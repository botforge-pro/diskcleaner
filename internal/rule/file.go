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

	"go.yaml.in/yaml/v3"

	"github.com/botforge-pro/diskcleaner/internal/process"
)

var groups = map[string]string{
	"xcode": process.Xcode,
}

var stepName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

type File struct {
	Name       string   `yaml:"name"`
	Label      string   `yaml:"label"`
	Describes  string   `yaml:"describes"`
	Reversible bool     `yaml:"reversible"`
	Root       []Place  `yaml:"root"`
	Busy       BusyWhen `yaml:"busy"`
	Targets    []string `yaml:"targets"`
}

type Place struct {
	Env  string `yaml:"env"`
	Path string `yaml:"path"`
}

type BusyWhen struct {
	Processes []string `yaml:"processes"`
	Groups    []string `yaml:"groups"`
	Says      string   `yaml:"says"`
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
	if err := f.check(strings.TrimSuffix(path.Base(name), ".yaml")); err != nil {
		return File{}, fmt.Errorf("%s: %w", name, err)
	}
	return f, nil
}

func (f File) check(base string) error {
	switch {
	case !stepName.MatchString(f.Name):
		return fmt.Errorf("name %q: want lowercase words joined by -, like xcode-derived-data", f.Name)
	case f.Name != base:
		return fmt.Errorf("name %q: want the name of the file, %q", f.Name, base)
	case f.Label == "" || len(f.Label) > 8 || strings.ToUpper(f.Label) != f.Label:
		return fmt.Errorf("label %q: want up to 8 capital letters", f.Label)
	case f.Describes == "":
		return errors.New("describes: want what the step removes, for --help")
	case len(f.Root) == 0:
		return errors.New("root: want at least one place")
	}
	for i, place := range f.Root {
		last := i == len(f.Root)-1
		switch {
		case place.Env == "" && place.Path == "":
			return fmt.Errorf("root %d: want env, path or both", i+1)
		case last && place.Env != "":
			return errors.New("root: the last place must be a path without env, so that a root is always found")
		case place.Env == "" && !strings.HasPrefix(place.Path, "~/") && !filepath.IsAbs(place.Path):
			return fmt.Errorf("root %d: path %q: want it to start with ~/ or /", i+1, place.Path)
		case place.Env != "" && (filepath.IsAbs(place.Path) || strings.HasPrefix(place.Path, "~")):
			return fmt.Errorf("root %d: path %q: under env it is a subfolder of the variable's value", i+1, place.Path)
		}
	}
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
	for _, target := range f.Targets {
		if _, err := filepath.Match(target, ""); err != nil || filepath.IsAbs(target) || slices.Contains(strings.Split(target, "/"), "..") {
			return fmt.Errorf("targets: %q: want a pattern inside the root, like *.avd/snapshots/*", target)
		}
	}
	return nil
}

func (f File) Rule(home string, getenv func(string) string, running func(string) (bool, error)) Rule {
	patterns := slices.Clone(f.Busy.Processes)
	for _, group := range f.Busy.Groups {
		patterns = append(patterns, groups[group])
	}
	return Rule{
		Label:   f.Label,
		Root:    f.root(home, getenv),
		Busy:    Busy{Processes: strings.Join(patterns, "|"), Running: running, Says: f.Busy.Says},
		Targets: f.Targets,
	}
}

func (f File) root(home string, getenv func(string) string) string {
	for _, place := range f.Root {
		if place.Env == "" {
			return expand(home, place.Path)
		}
		if value := getenv(place.Env); value != "" {
			return filepath.Join(value, place.Path)
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
