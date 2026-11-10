package xcode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

type Step struct {
	DerivedData string
	Processes   string
	Running     func(pattern string) (bool, error)
}

func Default(home string) Step {
	return Step{
		DerivedData: DerivedData(home, xcodeSetting),
		Processes:   process.Xcode,
		Running:     process.Running,
	}
}

func DerivedData(home string, setting func(key string) string) string {
	if custom := setting("IDECustomDerivedDataLocation"); filepath.IsAbs(custom) {
		return custom
	}
	return filepath.Join(home, "Library/Developer/Xcode/DerivedData")
}

func xcodeSetting(key string) string {
	output, err := exec.Command("defaults", "read", "com.apple.dt.Xcode", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (s Step) Run(r *report.Report) {
	if !r.Directory(s.DerivedData) {
		return
	}
	derivedData, err := filepath.EvalSymlinks(s.DerivedData)
	if err != nil {
		r.Fail("resolve %s: %v", s.DerivedData, err)
		return
	}
	fmt.Fprintf(r.Out, "XCODE    %s\n", derivedData)
	defer fmt.Fprintln(r.Out)

	if r.Busy(s.Running, s.Processes) {
		r.Skip("%s: Xcode or an Xcode build is running", derivedData)
		return
	}
	entries, err := os.ReadDir(derivedData)
	if err != nil {
		r.Fail("find %s: %v", derivedData, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			r.Remove(filepath.Join(derivedData, entry.Name()))
		}
	}
}
