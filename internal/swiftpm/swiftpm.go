package swiftpm

import (
	"fmt"
	"path/filepath"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

type Step struct {
	Cache     string
	Processes string
	Running   func(pattern string) (bool, error)
}

func Default(home string) Step {
	return Step{
		Cache:     settings.Env("DISKCLEANER_SWIFTPM_CACHE", filepath.Join(home, "Library/Caches/org.swift.swiftpm")),
		Processes: settings.Env("DISKCLEANER_SWIFT_PROCESSES", "swift-build|swift-package|swift-test|swift-run|xcodebuild"),
		Running:   process.Running,
	}
}

func (s Step) Run(r *report.Report) {
	if !r.Directory(s.Cache) {
		return
	}
	fmt.Fprintf(r.Out, "SWIFTPM  %s\n", s.Cache)
	defer fmt.Fprintln(r.Out)

	if r.Busy(s.Running, s.Processes) {
		r.Skip("%s: a Swift package build or xcodebuild is running", s.Cache)
		return
	}
	r.Remove(s.Cache)
}
