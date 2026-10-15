package scratchpads

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

var (
	derivedData   = []string{"Build", "Index.noindex", "ModuleCache.noindex", "SourcePackages"}
	packageClones = []string{"checkouts", "repositories"}
)

type Step struct {
	Root      string
	Processes string
	Running   func(pattern string) (bool, error)
}

func Default() Step {
	return Step{
		Root:      settings.Env("DISKCLEANER_CLAUDE_TMP", fmt.Sprintf("/private/tmp/claude-%d", os.Getuid())),
		Processes: settings.Env("DISKCLEANER_SWIFT_PROCESSES", "swift-build|swift-package|swift-test|swift-run|xcodebuild"),
		Running:   process.Running,
	}
}

func (s Step) Run(r *report.Report) {
	if !r.Directory(s.Root) {
		return
	}
	fmt.Fprintf(r.Out, "SCRATCH  %s\n", s.Root)
	defer fmt.Fprintln(r.Out)

	if r.Busy(s.Running, s.Processes) {
		r.Skip("%s: a Swift package build or xcodebuild is running", s.Root)
		return
	}
	var set candidates.Set
	var walkErrors []error
	walk := func(visit func(path string, entry fs.DirEntry) error) {
		err := filepath.WalkDir(s.Root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				walkErrors = append(walkErrors, err)
				return nil
			}
			if !strings.Contains(path, "/scratchpad/") {
				return nil
			}
			return visit(path, entry)
		})
		if err != nil {
			walkErrors = append(walkErrors, err)
		}
	}

	walk(func(path string, entry fs.DirEntry) error {
		if entry.IsDir() && entry.Name() == "Intermediates.noindex" && filepath.Base(filepath.Dir(path)) == "Build" {
			addExisting(r, &set, filepath.Dir(filepath.Dir(path)), derivedData)
			return filepath.SkipDir
		}
		return nil
	})
	walk(func(path string, entry fs.DirEntry) error {
		if entry.Type().IsRegular() && entry.Name() == "workspace-state.json" && isDir(filepath.Join(filepath.Dir(path), "checkouts")) {
			addExisting(r, &set, filepath.Dir(path), packageClones)
		}
		return nil
	})
	walk(func(path string, entry fs.DirEntry) error {
		if !entry.IsDir() || entry.Name() != ".build" {
			return nil
		}
		if isFile(filepath.Join(filepath.Dir(path), "Package.swift")) {
			set.Add(r, path)
		} else {
			r.Skip("%s: no Package.swift next to it", path)
		}
		return filepath.SkipDir
	})
	if len(walkErrors) > 0 {
		r.Fail("scan %s: %v", s.Root, errors.Join(walkErrors...))
	}
	set.Remove(r)
}

func addExisting(r *report.Report, set *candidates.Set, root string, names []string) {
	for _, name := range names {
		path := filepath.Join(root, name)
		if _, err := os.Lstat(path); err == nil {
			set.Add(r, path)
		}
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
