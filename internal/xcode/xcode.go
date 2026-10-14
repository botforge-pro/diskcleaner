package xcode

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

type Step struct {
	DerivedData string
	Processes   string
	Running     func(pattern string) (bool, error)
}

func Default(home string) Step {
	return Step{
		DerivedData: settings.Env("DISKCLEANER_XCODE_DERIVED_DATA", filepath.Join(home, "Library/Developer/Xcode/DerivedData")),
		Processes:   settings.Env("DISKCLEANER_XCODE_PROCESSES", `xcodebuild|XCBBuildService|/Xcode\.app/Contents/MacOS/Xcode`),
		Running:     process.Running,
	}
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
