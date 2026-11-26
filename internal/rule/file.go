package rule

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/botforge-pro/diskcleaner/internal/config"
)

const (
	TestSuffix  = "_test.yaml"
	GroupsFile  = "processes.yaml"
	fileSuffix  = ".yaml"
	enabledName = "enabled"
)

var (
	stepName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	stepFile = regexp.MustCompile(`^[0-9]{3}-(.*)$`)
)

type File struct {
	Name       string    `yaml:"-"`
	Base       string    `yaml:"-"`
	Describes  string    `yaml:"describes"`
	Reversible bool      `yaml:"reversible"`
	Readme     Readme    `yaml:"readme"`
	Settings   []Setting `yaml:"settings"`
	Node       `yaml:",inline"`
	groups     Groups
}

type Readme struct {
	Removes string `yaml:"removes"`
	Keeps   string `yaml:"keeps"`
	CanLose string `yaml:"can-lose"`
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
	"folders":  config.Dirs,
}

var globals = config.NewTable(nil)

func Load(fsys fs.FS) ([]File, error) {
	data, err := fs.ReadFile(fsys, GroupsFile)
	if err != nil {
		return nil, err
	}
	groups, err := ParseGroups(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", GroupsFile, err)
	}
	names, err := fs.Glob(fsys, "*"+fileSuffix)
	if err != nil {
		return nil, err
	}
	var files []File
	var errs []error
	for _, name := range names {
		if strings.HasSuffix(name, TestSuffix) || name == GroupsFile {
			continue
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		file, err := Parse(name, data, groups)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		files = append(files, file)
	}
	return files, errors.Join(errs...)
}

func Parse(name string, data []byte, groups Groups) (File, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	f := File{groups: groups}
	if err := decoder.Decode(&f); err != nil {
		return File{}, fmt.Errorf("%s: %w", name, err)
	}
	f.Base = strings.TrimSuffix(path.Base(name), fileSuffix)
	match := stepFile.FindStringSubmatch(f.Base)
	if match == nil {
		return File{}, fmt.Errorf("%s: the name of the file: want three digits, the step's place in a run, a dash and the step's name, like 070-xcode-derived-data.yaml", name)
	}
	f.Name = match[1]
	if err := f.check(); err != nil {
		return File{}, fmt.Errorf("%s: %w", name, err)
	}
	return f, nil
}

func (f File) Label() string {
	first, _, _ := strings.Cut(f.Name, "-")
	return strings.ToUpper(first)
}

func (f File) Step() config.Step {
	settings := make([]config.Setting, len(f.Settings))
	for i, s := range f.Settings {
		settings[i] = config.Setting{Key: s.Key, Kind: kinds[s.Kind], Default: s.Default, Help: s.Help}
	}
	return config.Step{Name: f.Name, Reversible: f.Reversible, Settings: settings}
}

var mention = regexp.MustCompile(`\{([^{}]*)\}`)

func (f File) Explain(text string) string {
	return mention.ReplaceAllStringFunc(text, func(found string) string {
		words, _ := f.mentioned(found[1 : len(found)-1])
		return words
	})
}

func (f File) Removes() string {
	if f.Readme.Removes == "" {
		return f.Explain(f.Describes)
	}
	return f.Explain(f.Readme.Removes)
}

func (f File) mentioned(key string) (string, bool) {
	if s, ok := f.own(key); ok {
		return fmt.Sprintf("`%s.%s` (`%s`)", f.Name, key, s.Default), true
	}
	if global, ok := globals.Find(key); ok && global.Kind == config.Duration {
		return fmt.Sprintf("`%s` (`%s`)", key, global.Default), true
	}
	return "", false
}

func (f File) own(key string) (Setting, bool) {
	i := slices.IndexFunc(f.Settings, func(s Setting) bool { return s.Key == key })
	if i < 0 {
		return Setting{}, false
	}
	return f.Settings[i], true
}

func (f File) setting(key string) (config.Kind, bool) {
	if s, ok := f.own(key); ok {
		return kinds[s.Kind], true
	}
	if global, ok := globals.Find(key); ok {
		return global.Kind, true
	}
	return 0, false
}

func (f File) configKey(key string) string {
	if _, ok := f.own(key); ok {
		return f.Name + "." + key
	}
	return key
}

func (f *File) check() error {
	switch {
	case !stepName.MatchString(f.Name):
		return errors.New("the name of the file: want lowercase words joined by - after the digits, like 070-xcode-derived-data.yaml")
	case f.Describes == "":
		return errors.New("describes: want what the step removes, for --help")
	case f.Readme.Removes == f.Describes:
		return errors.New("readme: removes: it says what describes says; leave it out and the README takes describes")
	}
	for _, s := range f.Settings {
		if err := s.check(); err != nil {
			return err
		}
	}
	used := map[string]bool{}
	c := checker{file: f, used: used, names: map[string]bool{"path": true, "name": true, "uid": true}}
	if err := c.node(&f.Node, 0); err != nil {
		return err
	}
	return f.checkReadme(used)
}

func (f File) checkReadme(used map[string]bool) error {
	mentioned := map[string]bool{}
	for _, text := range []string{f.Describes, f.Readme.Removes, f.Readme.Keeps, f.Readme.CanLose} {
		for _, found := range mention.FindAllStringSubmatch(text, -1) {
			if _, ok := f.mentioned(found[1]); !ok {
				return fmt.Errorf("readme: {%s}: want the key of one of this step's settings or of a global duration, like min-age", found[1])
			}
			if !used[found[1]] {
				return fmt.Errorf("readme: {%s}: the step does not use it, so the README would promise what the step does not do", found[1])
			}
			mentioned[found[1]] = true
		}
	}
	for _, s := range f.Settings {
		switch {
		case !used[s.Key]:
			return fmt.Errorf("settings: %s: nothing in the file uses it, so the README would promise what the step does not do", s.Key)
		case !mentioned[s.Key]:
			return fmt.Errorf("readme: want {%s} in its text, so that the README names every setting of the step", s.Key)
		}
	}
	return nil
}

func (s Setting) check() error {
	kind, ok := kinds[s.Kind]
	switch {
	case !stepName.MatchString(s.Key) || s.Key == enabledName:
		return fmt.Errorf("settings: key %q: want lowercase words joined by -, other than %s", s.Key, enabledName)
	case !ok || kind == config.Dirs:
		return fmt.Errorf("settings: %s: kind %q: want one of duration, timeout, count", s.Key, s.Kind)
	case s.Help == "":
		return fmt.Errorf("settings: %s: help: want what it changes, for --help", s.Key)
	}
	if _, err := config.Parse(config.Setting{Key: s.Key, Kind: kind}, s.Default, "/"); err != nil {
		return fmt.Errorf("settings: %s: default: %w", s.Key, err)
	}
	return nil
}

type Groups map[string][]string

func ParseGroups(data []byte) (Groups, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var groups Groups
	if err := decoder.Decode(&groups); err != nil {
		return nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(groups)) {
		if !stepName.MatchString(name) || len(groups[name]) == 0 {
			return nil, fmt.Errorf("%s: want lowercase words joined by - and a list of process patterns", name)
		}
		for _, pattern := range groups[name] {
			if _, err := regexp.CompilePOSIX(pattern); err != nil {
				return nil, fmt.Errorf("%s: %q: %w", name, pattern, err)
			}
		}
	}
	return groups, nil
}
