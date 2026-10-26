package ide

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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
	Toolbox      string
	Spotlight    func() ([]string, error)
	Library      string
}

func Default(home string) Step {
	library := filepath.Join(home, "Library")
	return Step{
		Applications: []string{"/Applications", filepath.Join(home, "Applications")},
		Toolbox:      filepath.Join(library, "Application Support", "JetBrains", "Toolbox", "apps"),
		Spotlight:    spotlightApps,
		Library:      library,
	}
}

type release struct {
	name    string
	vendor  string
	product string
	version []int
}

type editorBuilds struct {
	apps    []string
	commits []string
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
	folders := make([]string, 0, len(editors))
	for folder := range editors {
		folders = append(folders, folder)
	}
	slices.Sort(folders)
	for _, folder := range folders {
		s.cleanEditor(r, folder, editors[folder])
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

func (s Step) cleanEditor(r *report.Report, folder string, builds editorBuilds) {
	cached := filepath.Join(s.Library, "Application Support", folder, "CachedData")
	entries, err := os.ReadDir(cached)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		r.Fail("read %s: %v", cached, err)
		return
	}
	for i, commit := range builds.commits {
		if !slices.ContainsFunc(entries, func(entry fs.DirEntry) bool { return entry.Name() == commit && entry.IsDir() }) {
			r.Skip("%s: %s has not made its own cache here yet", cached, builds.apps[i])
			return
		}
	}
	for _, entry := range entries {
		if entry.IsDir() && !slices.Contains(builds.commits, entry.Name()) {
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

func (s Step) installed() (map[string][]release, map[string]editorBuilds, error) {
	apps, err := s.apps()
	if err != nil {
		return nil, nil, err
	}
	platform := map[string][]release{}
	editors := map[string]editorBuilds{}
	for _, app := range apps {
		current, ok, err := platformRelease(app)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", app, err)
		}
		if ok {
			platform[current.product] = append(platform[current.product], current)
		}
		folder, commit, ok, err := editorBuild(app)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", app, err)
		}
		if ok {
			builds := editors[folder]
			builds.apps = append(builds.apps, filepath.Base(app))
			builds.commits = append(builds.commits, commit)
			editors[folder] = builds
		}
	}
	return platform, editors, nil
}

func (s Step) apps() ([]string, error) {
	found, err := s.Spotlight()
	if err != nil {
		return nil, err
	}
	for _, dir := range s.Applications {
		for _, pattern := range appPattern {
			matches, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil {
				return nil, err
			}
			found = append(found, matches...)
		}
	}
	err = filepath.WalkDir(s.Toolbox, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == s.Toolbox {
			return filepath.SkipAll
		}
		if err != nil {
			return err
		}
		if entry.IsDir() && strings.HasSuffix(path, ".app") {
			found = append(found, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(found)
	return slices.Compact(found), nil
}

func spotlightApps() ([]string, error) {
	var stdout, stderr bytes.Buffer
	command := exec.Command("mdfind", `kMDItemContentType == "com.apple.application-bundle"`)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("mdfind: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var apps []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		if line != "" {
			apps = append(apps, line)
		}
	}
	if len(apps) == 0 {
		return nil, errors.New("mdfind lists no apps, so installed IDEs outside /Applications cannot be found; is Spotlight indexing off?")
	}
	return apps, nil
}

func platformRelease(app string) (release, bool, error) {
	var info struct {
		DataDirectoryName string `json:"dataDirectoryName"`
		ProductVendor     string `json:"productVendor"`
	}
	found, err := readJSON(filepath.Join(app, "Contents", "Resources", "product-info.json"), &info)
	if err != nil || !found || info.DataDirectoryName == "" || info.ProductVendor == "" {
		return release{}, false, err
	}
	current, ok := parse(info.DataDirectoryName)
	if !ok || !safeName(info.ProductVendor) {
		return release{}, false, fmt.Errorf("unexpected dataDirectoryName %q or productVendor %q", info.DataDirectoryName, info.ProductVendor)
	}
	current.vendor = info.ProductVendor
	return current, true, nil
}

func editorBuild(app string) (string, string, bool, error) {
	var info struct {
		NameShort string `json:"nameShort"`
		Commit    string `json:"commit"`
	}
	found, err := readJSON(filepath.Join(app, "Contents", "Resources", "app", "product.json"), &info)
	if err != nil || !found || info.NameShort == "" || info.Commit == "" {
		return "", "", false, err
	}
	if !safeName(info.NameShort) || !safeName(info.Commit) {
		return "", "", false, fmt.Errorf("unexpected nameShort %q or commit %q", info.NameShort, info.Commit)
	}
	return info.NameShort, info.Commit, true, nil
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
