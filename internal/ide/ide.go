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

var (
	versioned  = regexp.MustCompile(`^([A-Za-z]+)([0-9]+(?:\.[0-9]+)*)$`)
	dataKinds  = []string{"Application Support", "Caches", "Logs"}
	appPattern = []string{"*.app", "*/*.app"}
)

type Step struct {
	Applications []string
	Library      string
}

func Default(home string) Step {
	return Step{
		Applications: []string{"/Applications", filepath.Join(home, "Applications")},
		Library:      filepath.Join(home, "Library"),
	}
}

type release struct {
	name    string
	vendor  string
	product string
	version []int
}

type editor struct {
	app    string
	folder string
	commit string
}

func (s Step) Run(r *report.Report) {
	platform, editors, err := s.installed()
	if err != nil {
		r.Fail("read installed IDEs: %v", err)
		return
	}
	if len(platform) == 0 && len(editors) == 0 {
		return
	}
	fmt.Fprintln(r.Out, "IDE      data of older versions of installed IDEs")
	defer fmt.Fprintln(r.Out)

	s.cleanPlatform(r, platform)
	for _, e := range editors {
		s.cleanEditor(r, e)
	}
}

func (s Step) cleanPlatform(r *report.Report, installed map[string][]release) {
	var vendors []string
	for _, releases := range installed {
		for _, current := range releases {
			if !slices.Contains(vendors, current.vendor) {
				vendors = append(vendors, current.vendor)
			}
		}
	}
	slices.Sort(vendors)
	for _, vendor := range vendors {
		for _, kind := range dataKinds {
			s.cleanVendorFolder(r, filepath.Join(s.Library, kind, vendor), vendor, installed)
		}
	}
}

func (s Step) cleanVendorFolder(r *report.Report, folder, vendor string, installed map[string][]release) {
	entries, err := os.ReadDir(folder)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		r.Fail("read %s: %v", folder, err)
		return
	}
	for _, entry := range entries {
		old, ok := parse(entry.Name())
		if !ok || !entry.IsDir() {
			continue
		}
		var versions []release
		for _, current := range installed[old.product] {
			if current.vendor == vendor {
				versions = append(versions, current)
			}
		}
		if !superseded(old, versions) {
			continue
		}
		path := filepath.Join(folder, entry.Name())
		newest := slices.MaxFunc(versions, func(a, b release) int { return slices.Compare(a.version, b.version) })
		if !r.Directory(filepath.Join(folder, newest.name)) {
			r.Skip("%s: %s has not made its own folder here yet", path, newest.name)
			continue
		}
		r.Remove(path)
	}
}

func (s Step) cleanEditor(r *report.Report, e editor) {
	cached := filepath.Join(s.Library, "Application Support", e.folder, "CachedData")
	entries, err := os.ReadDir(cached)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		r.Fail("read %s: %v", cached, err)
		return
	}
	if !slices.ContainsFunc(entries, func(entry fs.DirEntry) bool { return entry.Name() == e.commit && entry.IsDir() }) {
		r.Skip("%s: %s has not made its own cache here yet", cached, e.app)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != e.commit {
			r.Remove(filepath.Join(cached, entry.Name()))
		}
	}
}

func superseded(old release, installed []release) bool {
	if len(installed) == 0 || slices.ContainsFunc(installed, func(current release) bool { return slices.Equal(current.version, old.version) }) {
		return false
	}
	return slices.ContainsFunc(installed, func(current release) bool { return slices.Compare(old.version, current.version) < 0 })
}

func (s Step) installed() (map[string][]release, []editor, error) {
	platform := map[string][]release{}
	var editors []editor
	for _, dir := range s.Applications {
		var apps []string
		for _, pattern := range appPattern {
			matches, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil {
				return nil, nil, err
			}
			apps = append(apps, matches...)
		}
		for _, app := range apps {
			current, ok, err := platformRelease(app)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", app, err)
			}
			if ok {
				platform[current.product] = append(platform[current.product], current)
			}
			e, ok, err := editorBuild(app)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", app, err)
			}
			if ok {
				editors = append(editors, e)
			}
		}
	}
	return platform, editors, nil
}

func platformRelease(app string) (release, bool, error) {
	var info struct {
		DataDirectoryName string `json:"dataDirectoryName"`
		ProductVendor     string `json:"productVendor"`
	}
	found, err := readJSON(filepath.Join(app, "Contents", "Resources", "product-info.json"), &info)
	if err != nil || !found {
		return release{}, false, err
	}
	current, ok := parse(info.DataDirectoryName)
	if !ok || !safeName(info.ProductVendor) {
		return release{}, false, fmt.Errorf("unexpected dataDirectoryName %q or productVendor %q", info.DataDirectoryName, info.ProductVendor)
	}
	current.vendor = info.ProductVendor
	return current, true, nil
}

func editorBuild(app string) (editor, bool, error) {
	var info struct {
		NameShort string `json:"nameShort"`
		Commit    string `json:"commit"`
	}
	found, err := readJSON(filepath.Join(app, "Contents", "Resources", "app", "product.json"), &info)
	if err != nil || !found || info.NameShort == "" || info.Commit == "" {
		return editor{}, false, err
	}
	if !safeName(info.NameShort) || !safeName(info.Commit) {
		return editor{}, false, fmt.Errorf("unexpected nameShort %q or commit %q", info.NameShort, info.Commit)
	}
	return editor{app: filepath.Base(app), folder: info.NameShort, commit: info.Commit}, true, nil
}

func readJSON(path string, into any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(data, into)
}

func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsRune(name, filepath.Separator)
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
	return release{name: name, product: match[1], version: version}, true
}
