package simulator

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/report"
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
		Devices:   env("DISKCLEANER_SIMULATOR_DEVICES", filepath.Join(home, "Library/Developer/CoreSimulator/Devices")),
		Processes: env("DISKCLEANER_SIMULATOR_PROCESSES", `xcodebuild|simctl|/Simulator\.app/|/DeviceHub\.app/`),
		Running:   pgrep,
		State:     plutilState,
	}
}

func (s Step) Run(r *report.Report) {
	if !s.directory(r, s.Devices) {
		return
	}
	devices, err := filepath.EvalSymlinks(s.Devices)
	if err != nil {
		r.Fail("resolve %s: %v", s.Devices, err)
		return
	}
	fmt.Fprintf(r.Out, "SIMULATOR %s\n", devices)
	defer fmt.Fprintln(r.Out)

	if s.running(r, s.Processes) {
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
		if !s.directory(r, filepath.Join(device, "data")) {
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

func (s Step) directory(r *report.Report, path string) bool {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		r.Fail("stat %s: %v", path, err)
		return false
	}
	if !info.IsDir() {
		r.Fail("%s: not a directory", path)
		return false
	}
	return true
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
	if s.running(r, device+"/") {
		r.Skip("%s: a process of this simulator is running", device)
		return false
	}
	return true
}

func (s Step) running(r *report.Report, pattern string) bool {
	running, err := s.Running(pattern)
	if err != nil {
		r.Fail("pgrep -f '%s': %v", pattern, err)
		return true
	}
	return running
}

func pgrep(pattern string) (bool, error) {
	output, err := exec.Command("pgrep", "-f", pattern).CombinedOutput()
	if err == nil {
		return true, nil
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
}

func plutilState(plist string) (string, error) {
	output, err := exec.Command("plutil", "-extract", "state", "raw", "-o", "-", plist).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
