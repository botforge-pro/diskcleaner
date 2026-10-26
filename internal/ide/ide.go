package ide

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

var versioned = regexp.MustCompile(`^([A-Za-z]+)([0-9]{4}(?:\.[0-9]+)*)$`)

type Step struct {
	Applications []string
	Folders      []string
}

func Default(home string) Step {
	library := filepath.Join(home, "Library")
	var folders []string
	for _, vendor := range []string{"JetBrains", "Google"} {
		for _, kind := range []string{"Application Support", "Caches", "Logs"} {
			folders = append(folders, filepath.Join(library, kind, vendor))
		}
	}
	return Step{
		Applications: []string{"/Applications", filepath.Join(home, "Applications")},
		Folders:      folders,
	}
}

type release struct {
	product string
	version []int
}

func (s Step) Run(r *report.Report) {
	installed, err := s.installed()
	if err != nil {
		r.Fail("read installed IDEs: %v", err)
		return
	}
	if len(installed) == 0 {
		return
	}
	fmt.Fprintln(r.Out, "IDE      data of older versions of installed IDEs")
	defer fmt.Fprintln(r.Out)

	for _, folder := range s.Folders {
		entries, err := os.ReadDir(folder)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			r.Fail("read %s: %v", folder, err)
			continue
		}
		for _, entry := range entries {
			old, ok := parse(entry.Name())
			if !ok || !entry.IsDir() {
				continue
			}
			if superseded(old, installed[old.product]) {
				r.Remove(filepath.Join(folder, entry.Name()))
			}
		}
	}
}

func superseded(old release, installed [][]int) bool {
	if len(installed) == 0 || slices.ContainsFunc(installed, func(version []int) bool { return slices.Equal(version, old.version) }) {
		return false
	}
	return slices.Compare(old.version, slices.MaxFunc(installed, slices.Compare[[]int])) < 0
}

func (s Step) installed() (map[string][][]int, error) {
	installed := map[string][][]int{}
	for _, dir := range s.Applications {
		var apps []string
		for _, pattern := range []string{"*.app", "*/*.app"} {
			matches, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil {
				return nil, err
			}
			apps = append(apps, matches...)
		}
		for _, app := range apps {
			name, err := dataDirectoryName(filepath.Join(app, "Contents", "Resources", "product-info.json"))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("%s: %w", app, err)
			}
			current, ok := parse(name)
			if !ok {
				return nil, fmt.Errorf("%s: unexpected dataDirectoryName %q", app, name)
			}
			installed[current.product] = append(installed[current.product], current.version)
		}
	}
	return installed, nil
}

func dataDirectoryName(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var info struct {
		DataDirectoryName string `json:"dataDirectoryName"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return "", err
	}
	return info.DataDirectoryName, nil
}

func parse(name string) (release, bool) {
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
