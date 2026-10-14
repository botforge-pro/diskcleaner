package simulator

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const (
	cacheName     = "com.apple.e5rt.e5bundlecache"
	shutdownState = "1"
)

type Step struct {
	Devices   string
	Processes string
	Running   func(pattern string) (bool, error)
	State     func(plist string) (string, error)
}

func Default(home string) Step {
	return Step{
		Devices:   settings.Env("DISKCLEANER_SIMULATOR_DEVICES", filepath.Join(home, "Library/Developer/CoreSimulator/Devices")),
		Processes: settings.Env("DISKCLEANER_SIMULATOR_PROCESSES", `xcodebuild|simctl|/Simulator\.app/|/DeviceHub\.app/`),
		Running:   process.Running,
		State:     plutilState,
	}
}

func (s Step) Run(r *report.Report) {
	if !r.Directory(s.Devices) {
		return
	}
	devices, err := filepath.EvalSymlinks(s.Devices)
	if err != nil {
		r.Fail("resolve %s: %v", s.Devices, err)
		return
	}
	fmt.Fprintf(r.Out, "SIMULATOR %s\n", devices)
	defer fmt.Fprintln(r.Out)

	if r.Busy(s.Running, s.Processes) {
		r.Skip("%s: Simulator, DeviceHub, simctl or an Xcode build is running", devices)
		return
	}
	entries, err := os.ReadDir(devices)
	if err != nil {
		r.Fail("find %s: %v", devices, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		device := filepath.Join(devices, entry.Name())
		if !r.Directory(filepath.Join(device, "data")) {
			continue
		}
		for _, cache := range s.caches(r, device) {
			if !s.idle(r, device) {
				break
			}
			r.Remove(cache)
		}
	}
}

func (s Step) caches(r *report.Report, device string) []string {
	data := filepath.Join(device, "data")
	root, err := filepath.EvalSymlinks(data)
	if err != nil {
		r.Fail("find %s: %v", data, err)
		return nil
	}
	var caches []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			r.Fail("find %s: %v", path, err)
			return nil
		}
		if !entry.IsDir() || entry.Name() != cacheName {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(rel, "Library/Caches/") && !strings.Contains(rel, "/Library/Caches/") {
			return nil
		}
		caches = append(caches, filepath.Join(data, rel))
		return fs.SkipDir
	})
	if err != nil {
		r.Fail("find %s: %v", data, err)
	}
	sort.Strings(caches)
	return caches
}

func (s Step) idle(r *report.Report, device string) bool {
	plist := filepath.Join(device, "device.plist")
	state, err := s.State(plist)
	if err != nil {
		r.Fail("plutil %s: %v", plist, err)
		return false
	}
	if state != shutdownState {
		r.Skip("%s: the simulator is not shut down (state %s)", device, state)
		return false
	}
	if r.Busy(s.Running, device+"/") {
		r.Skip("%s: a process of this simulator is running", device)
		return false
	}
	return true
}

func plutilState(plist string) (string, error) {
	output, err := exec.Command("plutil", "-extract", "state", "raw", "-o", "-", plist).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
