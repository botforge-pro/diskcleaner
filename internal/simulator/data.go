package simulator

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

var errNoValue = errors.New("no value")

type DataStep struct {
	Devices   string
	Processes string
	Idle      time.Duration
	Xcrun     string
	Running   func(pattern string) (bool, error)
	Value     func(plist, key string) (string, error)
	Now       func() time.Time
}

func DefaultData(home string, idle time.Duration) (DataStep, error) {
	caches := Default(home)
	return DataStep{
		Devices:   caches.Devices,
		Processes: caches.Processes,
		Idle:      idle,
		Xcrun:     "xcrun",
		Running:   process.Running,
		Value:     plutilValue,
		Now:       time.Now,
	}, nil
}

func (s DataStep) Run(r *report.Report) {
	if !r.Directory(s.Devices) {
		return
	}
	devices, err := filepath.EvalSymlinks(s.Devices)
	if err != nil {
		r.Fail("resolve %s: %v", s.Devices, err)
		return
	}
	fmt.Fprintf(r.Out, "SIMULATOR DATA %s\n", devices)
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
		data := filepath.Join(device, "data")
		if !r.Directory(data) {
			continue
		}
		udid, ok := s.idleSince(r, device)
		if ok {
			r.RunCleanup(data, s.Xcrun, "simctl", "erase", udid)
		}
	}
}

func (s DataStep) idleSince(r *report.Report, device string) (string, bool) {
	plist := filepath.Join(device, "device.plist")
	udid, err := s.Value(plist, "UDID")
	if err != nil {
		r.Fail("plutil %s: %v", plist, err)
		return "", false
	}
	state, err := s.Value(plist, "state")
	if err != nil {
		r.Fail("plutil %s: %v", plist, err)
		return "", false
	}
	if state != shutdownState {
		r.Skip("%s: the simulator is not shut down (state %s)", device, state)
		return "", false
	}
	containers := filepath.Join(device, "data", "Containers")
	_, err = os.Stat(containers)
	if errors.Is(err, fs.ErrNotExist) {
		r.Skip("%s: not booted since it was created or erased, erasing frees nothing", device)
		return "", false
	}
	if err != nil {
		r.Fail("stat %s: %v", containers, err)
		return "", false
	}
	lastUsed, err := s.Value(plist, "lastUsedAt")
	switch {
	case errors.Is(err, errNoValue):
		r.Skip("%s: the simulator was never booted, erasing frees nothing", device)
		return "", false
	case err != nil:
		r.Fail("plutil %s: %v", plist, err)
		return "", false
	default:
		used, err := time.Parse(time.RFC3339, lastUsed)
		if err != nil {
			r.Fail("%s: cannot read lastUsedAt %q: %v", plist, lastUsed, err)
			return "", false
		}
		if s.Now().Sub(used) <= s.Idle {
			r.Skip("%s: the simulator was used within %s (%s)", device, report.FormatDuration(s.Idle), lastUsed)
			return "", false
		}
	}
	if r.Busy(s.Running, device+"/") {
		r.Skip("%s: a process of this simulator is running", device)
		return "", false
	}
	return udid, true
}

func plutilValue(plist, key string) (string, error) {
	output, err := exec.Command("plutil", "-extract", key, "raw", "-o", "-", plist).CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil && strings.Contains(text, "No value at that key path") {
		return "", errNoValue
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, text)
	}
	return text, nil
}
