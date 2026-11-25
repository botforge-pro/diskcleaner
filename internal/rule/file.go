package rule

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/process"
)

var groups = map[string]string{
	"xcode": process.Xcode,
}

var (
	stepName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	variable = regexp.MustCompile(`^\$([A-Z_][A-Z0-9_]*)(/.*)?$`)
)

type File struct {
	Name       string    `yaml:"-"`
	Order      int       `yaml:"order"`
	Describes  string    `yaml:"describes"`
	Reversible bool      `yaml:"reversible"`
	Readme     Readme    `yaml:"readme"`
	Settings   []Setting `yaml:"settings"`
	Run        string    `yaml:"run"`
	Needs      []string  `yaml:"needs"`
	Root       Root      `yaml:"root"`
	Ready      Ready     `yaml:"ready"`
	Busy       BusyWhen  `yaml:"busy"`
	Stop       StopWhen  `yaml:"stop"`
	Each       EachWhen  `yaml:"each"`
	Remove     Removal   `yaml:"remove"`
	Keep       KeepWhen  `yaml:"keep"`
}

type Readme struct {
	Removes string `yaml:"removes"`
	Keeps   string `yaml:"keeps"`
	CanLose string `yaml:"can-lose"`
}

var mention = regexp.MustCompile(`\{([^{}]*)\}`)

func (f File) Explain(text string) string {
	return mention.ReplaceAllStringFunc(text, func(found string) string {
		words, _ := f.mentioned(found[1 : len(found)-1])
		return words
	})
}

var globals = config.NewTable(nil)

func (f File) mentioned(key string) (string, bool) {
	if key == "root" && !f.Root.empty() {
		return f.Root.words(), true
	}
	if i := slices.IndexFunc(f.Settings, func(s Setting) bool { return s.Key == key }); i >= 0 {
		return fmt.Sprintf("`%s.%s` (`%s`)", f.Name, key, f.Settings[i].Default), true
	}
	if global, ok := globals.Find(key); ok && global.Kind == config.Duration {
		return fmt.Sprintf("`%s` (`%s`)", key, global.Default), true
	}
	return "", false
}

func (f File) Removes() string {
	if f.Readme.Removes == "" {
		return f.Explain(f.Describes)
	}
	return f.Explain(f.Readme.Removes)
}

func (f File) checkReadme() error {
	if f.Readme.Removes == f.Describes {
		return errors.New("readme: removes: it says what describes says; leave it out and the README takes describes")
	}
	mentioned := map[string]bool{}
	for _, text := range []string{f.Describes, f.Readme.Removes, f.Readme.Keeps, f.Readme.CanLose} {
		for _, found := range mention.FindAllStringSubmatch(text, -1) {
			if _, ok := f.mentioned(found[1]); !ok {
				return fmt.Errorf("readme: {%s}: want {root} of a declared step, the key of one of this step's settings or min-age", found[1])
			}
			mentioned[found[1]] = true
		}
	}
	for _, s := range f.Settings {
		if !mentioned[s.Key] {
			return fmt.Errorf("readme: want {%s} in its text, so that the README names every setting of the step", s.Key)
		}
	}
	if f.Run == "code" {
		return nil
	}
	used := f.usedSettings()
	for _, s := range f.Settings {
		if !used[s.Key] {
			return fmt.Errorf("settings: %s: nothing in the file uses it, so the README would promise what the step does not do", s.Key)
		}
	}
	for key := range mentioned {
		if _, own := f.mentioned(key); own && key != "root" && !hasSetting(f.Settings, key, "") && !used[key] {
			return fmt.Errorf("readme: {%s}: the step does not apply it, so the README would promise what the step does not do", key)
		}
	}
	return nil
}

func (f File) usedSettings() map[string]bool {
	used := map[string]bool{}
	add := func(key string) {
		if key != "" {
			used[key] = true
		}
	}
	add(f.Keep.Age.Within)
	add(f.Ready.Timeout)
	if f.Remove.Newest != nil {
		add(f.Remove.Newest.Keep)
	}
	for _, c := range f.Remove.Commands {
		add(c.Timeout)
		add(c.Estimate.Timeout)
	}
	return used
}

type Setting struct {
	Key     string `yaml:"key"`
	Kind    string `yaml:"kind"`
	Default string `yaml:"default"`
	Help    string `yaml:"help"`
}

var kinds = map[string]config.Kind{
	"duration": config.Duration,
	"timeout":  config.Timeout,
	"count":    config.Count,
}

func (f File) Step() config.Step {
	settings := make([]config.Setting, len(f.Settings))
	for i, s := range f.Settings {
		settings[i] = config.Setting{Key: s.Key, Kind: kinds[s.Kind], Default: s.Default, Help: s.Help}
	}
	return config.Step{Name: f.Name, Reversible: f.Reversible, Settings: settings}
}

type BusyWhen struct {
	Processes []string `yaml:"processes"`
	Groups    []string `yaml:"groups"`
	Says      string   `yaml:"says"`
}

type StopWhen struct {
	Processes     []string `yaml:"processes"`
	Names         string   `yaml:"names"`
	UnlessBusyFor string   `yaml:"unless-busy-for"`
	Timeout       string   `yaml:"timeout"`
	Says          string   `yaml:"says"`
	quiet         time.Duration
	limit         time.Duration
}

func (s StopWhen) given() bool {
	return len(s.Processes) > 0 || s.Names != "" || s.UnlessBusyFor != "" || s.Timeout != "" || s.Says != ""
}

func (s StopWhen) pattern() string {
	return strings.Join(s.Processes, "|")
}

func (s *StopWhen) check() error {
	if !s.given() {
		return nil
	}
	if len(s.Processes) == 0 || s.Names == "" || s.UnlessBusyFor == "" || s.Timeout == "" || s.Says == "" {
		return errors.New("stop: want processes, names, unless-busy-for, timeout and says together")
	}
	for _, pattern := range s.Processes {
		if _, err := regexp.CompilePOSIX(pattern); err != nil {
			return fmt.Errorf("stop: processes %q: %w", pattern, err)
		}
	}
	var err error
	if s.quiet, err = literalDuration(s.UnlessBusyFor); err != nil {
		return fmt.Errorf("stop: unless-busy-for: %w", err)
	}
	if s.limit, err = literalDuration(s.Timeout); err != nil {
		return fmt.Errorf("stop: timeout: %w", err)
	}
	return nil
}

func literalDuration(text string) (time.Duration, error) {
	value, err := config.Parse(config.Setting{Key: "duration", Kind: config.Duration}, text, "/")
	if err != nil {
		return 0, err
	}
	return value.(time.Duration), nil
}

const TestSuffix = "_test.yaml"

func Load(fsys fs.FS) ([]File, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	var files []File
	var errs []error
	for _, name := range names {
		if strings.HasSuffix(name, TestSuffix) {
			continue
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		file, err := Parse(name, data)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		files = append(files, file)
	}
	return files, errors.Join(errs...)
}

func Parse(name string, data []byte) (File, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var f File
	if err := decoder.Decode(&f); err != nil {
		return File{}, fmt.Errorf("%s: %w", name, err)
	}
	f.Name = strings.TrimSuffix(path.Base(name), ".yaml")
	if err := f.check(); err != nil {
		return File{}, fmt.Errorf("%s: %w", name, err)
	}
	return f, nil
}

func (f File) Label() string {
	first, _, _ := strings.Cut(f.Name, "-")
	return strings.ToUpper(first)
}

func (f *File) check() error {
	switch {
	case !stepName.MatchString(f.Name):
		return fmt.Errorf("the name of the file: want lowercase words joined by -, like xcode-derived-data.yaml")
	case f.Order <= 0:
		return errors.New("order: want a whole number above 0; steps run from the lowest")
	case f.Describes == "":
		return errors.New("describes: want what the step removes, for --help")
	}
	for _, s := range f.Settings {
		if err := s.check(); err != nil {
			return err
		}
	}
	if err := f.checkReadme(); err != nil {
		return err
	}
	switch {
	case f.Run != "" && f.Run != "code":
		return fmt.Errorf("run %q: want code, for a step whose work is written in Go", f.Run)
	case f.Run == "code" && (!f.Root.empty() || len(f.Needs) > 0 || len(f.Ready.Run) > 0 || f.Busy.Says != "" || len(f.Busy.Processes) > 0 || len(f.Busy.Groups) > 0 || f.Stop.given() || f.Each.given() || f.Remove.given || f.Keep.given()):
		return errors.New("run: code: the step is written in Go, so needs, root, ready, busy, stop, each, remove and keep belong to it there")
	case f.Run == "code":
		return nil
	}
	if err := f.Root.check(len(f.Remove.Commands) == 0); err != nil {
		return err
	}
	if err := f.checkReady(); err != nil {
		return err
	}
	busy := f.Busy
	if (len(busy.Processes) > 0 || len(busy.Groups) > 0) != (busy.Says != "") {
		return errors.New("busy: want processes or groups together with says")
	}
	for _, pattern := range busy.Processes {
		if _, err := regexp.CompilePOSIX(pattern); err != nil {
			return fmt.Errorf("busy: processes %q: %w", pattern, err)
		}
	}
	for _, group := range busy.Groups {
		if _, ok := groups[group]; !ok {
			return fmt.Errorf("busy: groups %q: want one of %s", group, strings.Join(slices.Sorted(maps.Keys(groups)), ", "))
		}
	}
	if err := f.Stop.check(); err != nil {
		return err
	}
	if err := f.checkRemoval(f.Remove); err != nil {
		return err
	}
	if err := f.checkEach(); err != nil {
		return err
	}
	for i := range f.Root.places {
		if choice := f.Root.places[i].remove; choice != nil {
			if err := f.checkRemoval(*choice); err != nil {
				return fmt.Errorf("root: %w", err)
			}
		}
	}
	return f.checkKeep()
}

func inside(pattern string) bool {
	_, err := filepath.Match(pattern, "")
	return err == nil && pattern != "" && !filepath.IsAbs(pattern) && !slices.Contains(strings.Split(pattern, "/"), "..")
}

func (s Setting) check() error {
	kind, ok := kinds[s.Kind]
	switch {
	case !stepName.MatchString(s.Key) || s.Key == "enabled":
		return fmt.Errorf("settings: key %q: want lowercase words joined by -, other than enabled", s.Key)
	case !ok:
		return fmt.Errorf("settings: %s: kind %q: want one of %s", s.Key, s.Kind, strings.Join(slices.Sorted(maps.Keys(kinds)), ", "))
	case s.Help == "":
		return fmt.Errorf("settings: %s: help: want what it changes, for --help", s.Key)
	}
	if _, err := config.Parse(config.Setting{Key: s.Key, Kind: kind}, s.Default, "/"); err != nil {
		return fmt.Errorf("settings: %s: default: %w", s.Key, err)
	}
	return nil
}

func (f File) checkReady() error {
	ready := f.Ready
	if len(ready.Run) == 0 {
		if ready.Timeout != "" || ready.Hung != "" || len(ready.Down.Processes) > 0 || ready.Down.Says != "" {
			return errors.New("ready: want run, the command that must answer first")
		}
		return nil
	}
	if ready.Hung == "" || len(ready.Down.Processes) == 0 || ready.Down.Says == "" {
		return errors.New("ready: want run with hung, what to do when it does not answer, and down: {processes, says}, what tells that it is not running")
	}
	for _, pattern := range ready.Down.Processes {
		if _, err := regexp.CompilePOSIX(pattern); err != nil {
			return fmt.Errorf("ready: down: processes %q: %w", pattern, err)
		}
	}
	return f.checkTimeout(ready.Timeout)
}

func (f File) BusyPattern() string {
	patterns := slices.Clone(f.Busy.Processes)
	for _, group := range f.Busy.Groups {
		patterns = append(patterns, groups[group])
	}
	return strings.Join(patterns, "|")
}
