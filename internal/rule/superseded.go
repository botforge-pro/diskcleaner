package rule

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

var (
	placeholder = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_-]*)\}`)
	versioned   = regexp.MustCompile(`^([A-Za-z]+)([0-9]+(?:\.[0-9]+)*)$`)
)

type Apps struct {
	Run  []string `yaml:"run"`
	Says string   `yaml:"says"`
	In   []string `yaml:"in"`
}

func (a *Apps) given() bool {
	return a != nil && (len(a.Run) > 0 || len(a.In) > 0)
}

type Superseded struct {
	Read    string   `yaml:"read"`
	Folders []string `yaml:"folders"`
	By      string   `yaml:"by"`
}

func (f File) checkSuperseded() error {
	switch {
	case len(f.Remove.Superseded) == 0:
		return nil
	case !f.Apps.given():
		return errors.New("remove: superseded: want apps: the run that lists installed apps or the patterns in which they lie")
	case (len(f.Apps.Run) > 0) != (f.Apps.Says != ""):
		return errors.New("apps: want run together with says, which explains a run that lists no app")
	case !f.Root.empty() || f.Keep.given() || f.Each.given():
		return errors.New("remove: superseded: its folders say where they are and the installed apps keep theirs, so want no root, keep or each beside it")
	}
	for _, pattern := range f.Apps.In {
		if !strings.HasPrefix(pattern, "~/") && !filepath.IsAbs(pattern) {
			return fmt.Errorf("apps: in %q: want a pattern under ~/ or /", pattern)
		}
	}
	for _, s := range f.Remove.Superseded {
		switch {
		case !inside(s.Read) || filepath.Ext(s.Read) != ".json":
			return fmt.Errorf("remove: superseded: read %q: want a .json inside the app, like Contents/Resources/product-info.json", s.Read)
		case s.By != "version" && s.By != "name":
			return fmt.Errorf("remove: superseded: by %q: want version, to remove only older ones, or name, to remove any other", s.By)
		case len(s.Folders) == 0:
			return errors.New("remove: superseded: folders: want the folder of an installed app, the keys of the file read in {}")
		}
		for _, folder := range s.Folders {
			last := filepath.Base(folder)
			if !strings.HasPrefix(folder, "~/") && !filepath.IsAbs(folder) || placeholder.FindString(last) != last {
				return fmt.Errorf("remove: superseded: folders %q: want a path under ~/ or / whose last part is one {key} of the file read", folder)
			}
		}
	}
	return nil
}

type owned struct {
	name string
	app  string
}

func (s Rule) removeSuperseded(r *report.Report) {
	apps, err := s.installedApps()
	if err != nil {
		r.Fail("find installed apps: %v", err)
		return
	}
	owners := make([]map[string][]owned, len(s.remove.Superseded))
	for i, sup := range s.remove.Superseded {
		owners[i], err = s.ownFolders(sup, apps)
		if err != nil {
			r.Fail("%v", err)
			return
		}
	}
	for i, sup := range s.remove.Superseded {
		for _, parent := range slices.Sorted(maps.Keys(owners[i])) {
			if sup.By == "version" {
				removeOlder(r, parent, owners[i][parent])
			} else {
				removeOthers(r, parent, owners[i][parent])
			}
		}
	}
}

func (s Rule) ownFolders(sup Superseded, apps []string) (map[string][]owned, error) {
	parents := map[string][]owned{}
	for _, app := range apps {
		values, found, err := readValues(filepath.Join(app, sup.Read))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", app, err)
		}
		if !found {
			continue
		}
		for _, folder := range sup.Folders {
			path, ok, err := fill(folder, values)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", app, err)
			}
			if !ok {
				continue
			}
			name := filepath.Base(path)
			if _, isRelease := parseRelease(name); sup.By == "version" && !isRelease {
				return nil, fmt.Errorf("%s: %s: want a name followed by a version, like GoLand2026.2", app, name)
			}
			parent := s.local(filepath.Dir(path))
			parents[parent] = append(parents[parent], owned{name: name, app: filepath.Base(app)})
		}
	}
	return parents, nil
}

func (s Rule) installedApps() ([]string, error) {
	var found []string
	if apps := s.file.Apps; len(apps.Run) > 0 {
		result := s.env.Commands.Run(0, apps.Run[0], apps.Run[1:]...)
		if result.Err != nil {
			return nil, fmt.Errorf("%s: %v: %s", strings.Join(apps.Run, " "), result.Err, result.Output())
		}
		for _, line := range strings.Split(result.Stdout, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				found = append(found, line)
			}
		}
		if len(found) == 0 {
			return nil, errors.New(apps.Says)
		}
	}
	for _, pattern := range s.file.Apps.In {
		matches, err := s.matchApps(s.local(pattern))
		if err != nil {
			return nil, err
		}
		found = append(found, matches...)
	}
	slices.Sort(found)
	return slices.Compact(found), nil
}

func (s Rule) matchApps(pattern string) ([]string, error) {
	base, deep, isDeep := strings.Cut(pattern, "/**/")
	if !isDeep {
		return filepath.Glob(pattern)
	}
	matches, err := s.deepFolders(base, "**/"+deep, nil)
	if errors.Is(err, fs.ErrNotExist) && len(matches) == 0 {
		return nil, nil
	}
	return matches, err
}

func readValues(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	return values, true, nil
}

func fill(folder string, values map[string]any) (string, bool, error) {
	complete := true
	var failure error
	path := placeholder.ReplaceAllStringFunc(folder, func(found string) string {
		key := placeholder.FindStringSubmatch(found)[1]
		value, _ := values[key].(string)
		switch {
		case value == "":
			complete = false
		case value == "." || value == ".." || strings.ContainsRune(value, filepath.Separator):
			failure = errors.Join(failure, fmt.Errorf("%s %q: want a name, not a path", key, value))
		}
		return value
	})
	return path, complete && failure == nil, failure
}

type release struct {
	product string
	version []int
}

func parseRelease(name string) (release, bool) {
	match := versioned.FindStringSubmatch(name)
	if match == nil {
		return release{}, false
	}
	var version []int
	for _, part := range strings.Split(match[2], ".") {
		number, err := strconv.Atoi(part)
		if err != nil {
			return release{}, false
		}
		version = append(version, number)
	}
	return release{product: match[1], version: version}, true
}

func folderNames(r *report.Report, parent string) ([]string, bool) {
	entries, err := os.ReadDir(parent)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		r.Fail("read %s: %v", parent, err)
		return nil, false
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, true
}

func removeOlder(r *report.Report, parent string, installed []owned) {
	names, ok := folderNames(r, parent)
	if !ok {
		return
	}
	for _, name := range names {
		old, ok := parseRelease(name)
		if !ok {
			continue
		}
		var newest owned
		var newestVersion []int
		superseded, same := false, false
		for _, current := range installed {
			release, _ := parseRelease(current.name)
			if release.product != old.product {
				continue
			}
			switch compared := slices.Compare(old.version, release.version); {
			case compared == 0:
				same = true
			case compared < 0:
				superseded = true
			}
			if newestVersion == nil || slices.Compare(release.version, newestVersion) > 0 {
				newest, newestVersion = current, release.version
			}
		}
		if same || !superseded {
			continue
		}
		path := filepath.Join(parent, name)
		if !slices.Contains(names, newest.name) {
			r.Skip("%s: %s has not made its own folder here yet", path, newest.app)
			continue
		}
		r.Remove(path)
	}
}

func removeOthers(r *report.Report, parent string, installed []owned) {
	names, ok := folderNames(r, parent)
	if !ok {
		return
	}
	for _, current := range installed {
		if !slices.Contains(names, current.name) {
			r.Skip("%s: %s has not made its own folder here yet", parent, current.app)
			return
		}
	}
	for _, name := range names {
		if !slices.ContainsFunc(installed, func(current owned) bool { return current.name == name }) {
			r.Remove(filepath.Join(parent, name))
		}
	}
}
