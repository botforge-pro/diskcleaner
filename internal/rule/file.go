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

var (
	stepName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	variable = regexp.MustCompile(`^\$([A-Z_][A-Z0-9_]*)(/.*)?$`)
)

type File struct {
	Name       string   `yaml:"name"`
	Label      string   `yaml:"label"`
	Describes  string   `yaml:"describes"`
	Reversible bool     `yaml:"reversible"`
	Root       string   `yaml:"root"`
	Busy       BusyWhen `yaml:"busy"`
	Remove     Removal  `yaml:"remove"`
	places     []place
}

type BusyWhen struct {
	Processes []string `yaml:"processes"`
	Groups    []string `yaml:"groups"`
	Says      string   `yaml:"says"`
}

type Removal struct {
	Root    bool
	Folders []string
	given   bool
}

func (m *Removal) UnmarshalYAML(node *yaml.Node) error {
	m.given = true
	if node.Kind == yaml.ScalarNode {
		if node.Value != "root" {
			return fmt.Errorf("line %d: remove: want root, or folders: [patterns]", node.Line)
		}
		m.Root = true
		return nil
	}
	var folders struct {
		Folders []string `yaml:"folders"`
	}
	if err := node.Decode(&folders); err != nil {
		return err
	}
	m.Folders = folders.Folders
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
	if err := f.check(strings.TrimSuffix(path.Base(name), ".yaml")); err != nil {
		return File{}, fmt.Errorf("%s: %w", name, err)
	}
	return f, nil
}

func (f *File) check(base string) error {
	switch {
	case !stepName.MatchString(f.Name):
		return fmt.Errorf("name %q: want lowercase words joined by -, like xcode-derived-data", f.Name)
	case f.Name != base:
		return fmt.Errorf("name %q: want the name of the file, %q", f.Name, base)
	case f.Label == "" || len(f.Label) > 8 || strings.ToUpper(f.Label) != f.Label:
		return fmt.Errorf("label %q: want up to 8 capital letters", f.Label)
	case f.Describes == "":
		return errors.New("describes: want what the step removes, for --help")
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
		return errors.New("remove: want root, or folders: [patterns inside the root]")
	case !f.Remove.Root && len(f.Remove.Folders) == 0:
		return errors.New("remove: folders: want at least one pattern")
	}
	for _, pattern := range f.Remove.Folders {
		if _, err := filepath.Match(pattern, ""); err != nil || filepath.IsAbs(pattern) || slices.Contains(strings.Split(pattern, "/"), "..") {
			return fmt.Errorf("remove: folders: %q: want a pattern inside the root, like *.avd/snapshots/*", pattern)
		}
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

func (f File) Rule(home string, getenv func(string) string, running func(string) (bool, error)) Rule {
	patterns := slices.Clone(f.Busy.Processes)
	for _, group := range f.Busy.Groups {
		patterns = append(patterns, groups[group])
	}
	return Rule{
		Label:   f.Label,
		Root:    f.root(home, getenv),
		Busy:    Busy{Processes: strings.Join(patterns, "|"), Running: running, Says: f.Busy.Says},
		Targets: f.Remove.Folders,
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
