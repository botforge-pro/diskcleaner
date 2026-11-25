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

	"github.com/botforge-pro/diskcleaner/internal/age"
	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
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
	Root       string    `yaml:"root"`
	Busy       BusyWhen  `yaml:"busy"`
	Remove     Removal   `yaml:"remove"`
	Keep       KeepWhen  `yaml:"keep"`
	places     []place
}

type Readme struct {
	Removes string `yaml:"removes"`
	Keeps   string `yaml:"keeps"`
	CanLose string `yaml:"can-lose"`
}

var mention = regexp.MustCompile(`\{([^{}]*)\}`)

func (f File) Explain(text string) string {
	return mention.ReplaceAllStringFunc(text, func(found string) string {
		key := found[1 : len(found)-1]
		if key == "root" {
			return f.rootInWords()
		}
		i := slices.IndexFunc(f.Settings, func(s Setting) bool { return s.Key == key })
		return fmt.Sprintf("`%s.%s` (`%s`)", f.Name, key, f.Settings[i].Default)
	})
}

func (f File) Removes() string {
	if f.Readme.Removes == "" {
		return f.Explain(f.Describes)
	}
	return f.Explain(f.Readme.Removes)
}

func (f File) rootInWords() string {
	words := make([]string, len(f.places))
	for i, p := range f.places {
		switch {
		case p.variable == "":
			words[i] = "`" + p.path + "`"
		case p.path == "":
			words[i] = "`$" + p.variable + "`"
		default:
			words[i] = "`$" + p.variable + "/" + p.path + "`"
		}
	}
	return strings.Join(words, ", else ")
}

func (f File) checkReadme() error {
	if f.Readme.Removes == f.Describes {
		return errors.New("readme: removes: it says what describes says; leave it out and the README takes describes")
	}
	mentioned := map[string]bool{}
	for _, text := range []string{f.Describes, f.Readme.Removes, f.Readme.Keeps, f.Readme.CanLose} {
		for _, found := range mention.FindAllStringSubmatch(text, -1) {
			if found[1] == "root" && f.Root != "" {
				continue
			}
			if !slices.ContainsFunc(f.Settings, func(s Setting) bool { return s.Key == found[1] }) {
				return fmt.Errorf("readme: {%s}: want {root} of a declared step or the key of one of this step's settings", found[1])
			}
			mentioned[found[1]] = true
		}
	}
	for _, s := range f.Settings {
		if !mentioned[s.Key] {
			return fmt.Errorf("readme: want {%s} in its text, so that the README names every setting of the step", s.Key)
		}
	}
	return nil
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

type KeepWhen struct {
	WrittenWithin string `yaml:"written-within"`
}

type Removal struct {
	Root    bool
	Folders []string
	Files   []string
	given   bool
}

func (m *Removal) UnmarshalYAML(node *yaml.Node) error {
	m.given = true
	want := fmt.Errorf("line %d: remove: want root, or one of folders: [patterns] and files: [patterns]", node.Line)
	if node.Kind == yaml.ScalarNode {
		if node.Value != "root" {
			return want
		}
		m.Root = true
		return nil
	}
	if node.Kind != yaml.MappingNode || len(node.Content) != 2 {
		return want
	}
	var patterns []string
	if err := node.Content[1].Decode(&patterns); err != nil {
		return err
	}
	switch node.Content[0].Value {
	case "folders":
		m.Folders = patterns
	case "files":
		m.Files = patterns
	default:
		return want
	}
	return nil
}

type place struct {
	variable string
	path     string
}

func Load(fsys fs.FS) ([]File, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	var files []File
	var errs []error
	for _, name := range names {
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
	case f.Run == "code" && (f.Root != "" || f.Busy.Says != "" || len(f.Busy.Processes) > 0 || len(f.Busy.Groups) > 0 || f.Remove.given || f.Keep != (KeepWhen{})):
		return errors.New("run: code: the step is written in Go, so root, busy, remove and keep belong to it there")
	case f.Run == "code":
		return nil
	}
	places, err := parseRoot(f.Root)
	if err != nil {
		return err
	}
	f.places = places
	busy := f.Busy
	if (len(busy.Processes) > 0 || len(busy.Groups) > 0) != (busy.Says != "") {
		return errors.New("busy: want processes or groups together with says")
	}
	for _, pattern := range busy.Processes {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("busy: processes %q: %w", pattern, err)
		}
	}
	for _, group := range busy.Groups {
		if _, ok := groups[group]; !ok {
			return fmt.Errorf("busy: groups %q: want one of %s", group, strings.Join(slices.Sorted(maps.Keys(groups)), ", "))
		}
	}
	switch {
	case !f.Remove.given:
		return errors.New("remove: want root, or one of folders: [patterns] and files: [patterns] inside the root")
	case !f.Remove.Root && len(f.Remove.Folders) == 0 && len(f.Remove.Files) == 0:
		return errors.New("remove: want at least one pattern")
	}
	for _, pattern := range f.Remove.Folders {
		if !inside(pattern) || strings.Contains(pattern, "**") {
			return fmt.Errorf("remove: folders: %q: want a pattern inside the root, like *.avd/snapshots/*", pattern)
		}
	}
	for _, pattern := range f.Remove.Files {
		rest, anywhere := strings.CutPrefix(pattern, "**/")
		if !inside(rest) || strings.Contains(rest, "**") || anywhere && strings.Contains(rest, "/") {
			return fmt.Errorf("remove: files: %q: want a pattern inside the root, like logs/*.log, or **/ and a name pattern, like **/*.jsonl", pattern)
		}
	}
	return f.checkKeep()
}

func inside(pattern string) bool {
	_, err := filepath.Match(pattern, "")
	return err == nil && pattern != "" && !filepath.IsAbs(pattern) && !slices.Contains(strings.Split(pattern, "/"), "..")
}

func (f File) checkKeep() error {
	k := f.Keep
	if k == (KeepWhen{}) {
		return nil
	}
	if len(f.Remove.Files) == 0 {
		return errors.New("keep: want remove: files; only files have keep yet")
	}
	if k.WrittenWithin != "" && !slices.ContainsFunc(f.Settings, func(s Setting) bool { return s.Key == k.WrittenWithin && s.Kind == "duration" }) {
		return fmt.Errorf("keep: written-within %q: want the key of one of this step's duration settings", k.WrittenWithin)
	}
	return nil
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

func parseRoot(root string) ([]place, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("root: want a folder, like ~/Library/Caches/tool or $TOOL_HOME/cache or ~/.tool/cache")
	}
	parts := strings.Split(root, " or ")
	places := make([]place, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		last := i == len(parts)-1
		if match := variable.FindStringSubmatch(part); match != nil {
			if last {
				return nil, fmt.Errorf("root: %q: the last choice must be a path, so that a root is always found", part)
			}
			places[i] = place{variable: match[1], path: strings.TrimPrefix(match[2], "/")}
			continue
		}
		if !strings.HasPrefix(part, "~/") && !filepath.IsAbs(part) {
			return nil, fmt.Errorf("root: %q: want $VARIABLE, $VARIABLE/subfolder, a path starting with ~/ or /", part)
		}
		places[i] = place{path: part}
	}
	return places, nil
}

type Environment struct {
	Home     string
	Getenv   func(string) string
	Running  func(string) (bool, error)
	Duration func(key string) time.Duration
}

func (f File) Rule(env Environment) Rule {
	patterns := slices.Clone(f.Busy.Processes)
	for _, group := range f.Busy.Groups {
		patterns = append(patterns, groups[group])
	}
	keep := Keep{Age: age.Of(0)}
	if f.Keep.WrittenWithin != "" {
		keep.Age = age.Of(env.Duration(f.Keep.WrittenWithin))
	}
	return Rule{
		Label:   f.Label(),
		Root:    f.root(env.Home, env.Getenv),
		Busy:    Busy{Processes: strings.Join(patterns, "|"), Running: env.Running, Says: f.Busy.Says},
		Folders: f.Remove.Folders,
		Files:   f.Remove.Files,
		Keep:    keep,
		Lsof:    inuse.DefaultLsof(),
	}
}

func (f File) root(home string, getenv func(string) string) string {
	for _, p := range f.places {
		if p.variable == "" {
			return expand(home, p.path)
		}
		if value := getenv(p.variable); value != "" {
			return filepath.Join(value, p.path)
		}
	}
	return ""
}

func expand(home, place string) string {
	if under, ok := strings.CutPrefix(place, "~/"); ok {
		return filepath.Join(home, under)
	}
	return place
}
