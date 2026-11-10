package xcode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

type Step struct {
	DerivedData    string
	SharedWithUser bool
	Processes      string
	Running        func(pattern string) (bool, error)
}

func Default(home string) Step {
	derivedData, picked := DerivedData(home, xcodeSetting)
	return Step{
		DerivedData:    derivedData,
		SharedWithUser: picked,
		Processes:      process.Xcode,
		Running:        process.Running,
	}
}

func DerivedData(home string, setting func(key string) string) (path string, pickedByUser bool) {
	if custom := setting("IDECustomDerivedDataLocation"); filepath.IsAbs(custom) {
		return custom, true
	}
	return filepath.Join(home, "Library/Developer/Xcode/DerivedData"), false
}

var nameDashTwentyEightLetters = regexp.MustCompile(`^.+-[a-z]{28}$`)

func madeByXcode(dir, name string) bool {
	if strings.HasSuffix(name, ".noindex") || name == "SDKExplicitPrecompiledModules" {
		return true
	}
	if !nameDashTwentyEightLetters.MatchString(name) {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, name, "info.plist"))
	return err == nil
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
		if !entry.IsDir() {
			continue
		}
		if s.SharedWithUser && !madeByXcode(derivedData, entry.Name()) {
			r.Skip("%s: not made by Xcode", filepath.Join(derivedData, entry.Name()))
			continue
		}
		r.Remove(filepath.Join(derivedData, entry.Name()))
	}
}
