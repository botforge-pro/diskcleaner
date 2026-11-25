package rule

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/age"
	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

type Busy struct {
	Processes string
	Running   func(pattern string) (bool, error)
	Says      string
}

const openAnswersPerListing = 50

type Keep struct {
	Age age.Limit
}

type Rule struct {
	Label   string
	Root    string
	Busy    Busy
	Folders []string
	Files   []string
	Keep    Keep
	Lsof    inuse.Lsof
}

func (s Rule) Run(r *report.Report) {
	if !r.Directory(s.Root) {
		return
	}
	fmt.Fprintf(r.Out, "%-8s %s\n", s.Label, s.Root)
	defer fmt.Fprintln(r.Out)

	if s.Busy.Processes != "" && r.Busy(s.Busy.Running, s.Busy.Processes) {
		r.Skip("%s: %s", s.Root, s.Busy.Says)
		return
	}
	switch {
	case len(s.Files) > 0:
		s.removeFiles(r)
	case len(s.Folders) > 0:
		for _, folder := range s.folders(r) {
			r.Remove(folder)
		}
	default:
		r.Remove(s.Root)
	}
}

func (s Rule) folders(r *report.Report) []string {
	var matches []string
	for _, pattern := range s.Folders {
		found, err := filepath.Glob(filepath.Join(s.Root, pattern))
		if err != nil {
			r.Fail("find %s: %v", s.Root, err)
			continue
		}
		matches = append(matches, found...)
	}
	slices.Sort(matches)
	var folders []string
	for _, match := range slices.Compact(matches) {
		info, err := os.Lstat(match)
		if err != nil {
			r.Fail("stat %s: %v", match, err)
			continue
		}
		if info.IsDir() {
			folders = append(folders, match)
		}
	}
	return folders
}

func (s Rule) removeFiles(r *report.Report) {
	root, err := settings.Canonical(s.Root)
	if err != nil {
		r.Fail("resolve %s: %v", s.Root, err)
		return
	}
	var set candidates.Set
	var scanErrors []error
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			scanErrors = append(scanErrors, err)
			return nil
		}
		if !entry.Type().IsRegular() || !s.wanted(root, path) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			scanErrors = append(scanErrors, err)
			return nil
		}
		if s.Keep.Age.Recent(info.ModTime()) {
			return nil
		}
		set.Add(r, path)
		return nil
	})
	if err != nil {
		scanErrors = append(scanErrors, err)
	}
	if len(scanErrors) > 0 {
		r.Fail("scan %s: %v", s.Root, errors.Join(scanErrors...))
	}
	set.RemoveUnless(r, s.keeper(r))
}

func (s Rule) wanted(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(s.Files, func(pattern string) bool {
		if anywhere, ok := strings.CutPrefix(pattern, "**/"); ok {
			matched, _ := filepath.Match(anywhere, filepath.Base(rel))
			return matched
		}
		matched, _ := filepath.Match(pattern, rel)
		return matched
	})
}

func (s Rule) keeper(r *report.Report) func(path string) bool {
	open := s.Lsof.Every(openAnswersPerListing)
	failed := false
	return func(path string) bool {
		if failed {
			return true
		}
		held, err := open.HoldsAny(path)
		if err != nil {
			r.Fail("lsof %s: %v", s.Root, err)
			failed = true
			return true
		}
		if held {
			r.Skip("%s: a process still has it open", path)
			return true
		}
		info, err := os.Lstat(path)
		if err != nil {
			r.Fail("stat %s: %v", path, err)
			return true
		}
		if s.Keep.Age.Recent(info.ModTime()) {
			r.Skip("%s: written again since the scan", path)
			return true
		}
		return false
	}
}
