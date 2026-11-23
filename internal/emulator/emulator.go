package emulator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

type Step struct {
	Devices   string
	Processes string
	Running   func(pattern string) (bool, error)
}

func Default(home string) Step {
	return Step{
		Devices:   devices(home),
		Processes: `qemu-system-|/emulator/emulator( |$)`,
		Running:   process.Running,
	}
}

func devices(home string) string {
	if dir := settings.Env("ANDROID_AVD_HOME", ""); dir != "" {
		return dir
	}
	if dir := settings.Env("ANDROID_USER_HOME", ""); dir != "" {
		return filepath.Join(dir, "avd")
	}
	return filepath.Join(home, ".android/avd")
}

func (s Step) Run(r *report.Report) {
	if !r.Directory(s.Devices) {
		return
	}
	fmt.Fprintf(r.Out, "EMULATOR %s\n", s.Devices)
	defer fmt.Fprintln(r.Out)

	if r.Busy(s.Running, s.Processes) {
		r.Skip("%s: an Android emulator is running", s.Devices)
		return
	}
	snapshots, err := filepath.Glob(filepath.Join(s.Devices, "*.avd", "snapshots", "*"))
	if err != nil {
		r.Fail("find %s: %v", s.Devices, err)
		return
	}
	sort.Strings(snapshots)
	for _, snapshot := range snapshots {
		info, err := os.Lstat(snapshot)
		if err != nil {
			r.Fail("stat %s: %v", snapshot, err)
			continue
		}
		if info.IsDir() {
			r.Remove(snapshot)
		}
	}
}
