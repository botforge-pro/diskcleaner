package rule

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/age"
	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const openAnswersPerListing = 50

type Environment struct {
	Home     string
	Getenv   func(string) string
	Running  func(pattern string) (bool, error)
	Duration func(key string) time.Duration
	Now      func() time.Time
	Lsof     inuse.Lsof
}

type Rule struct {
	file File
	root string
	env  Environment
}

func (f File) Rule(env Environment) Rule {
	return Rule{file: f, root: f.root(env.Home, env.Getenv), env: env}
}

func (s Rule) Root() string {
	return s.root
}

func (s Rule) Run(r *report.Report) {
	if !r.Directory(s.root) {
		return
	}
	fmt.Fprintf(r.Out, "%-8s %s\n", s.file.Label(), s.root)
	defer fmt.Fprintln(r.Out)

	if pattern := s.file.BusyPattern(); pattern != "" && r.Busy(s.env.Running, pattern) {
		r.Skip("%s: %s", s.root, s.file.Busy.Says)
		return
	}
	switch remove := s.file.Remove; {
	case remove.Root:
		r.Remove(s.root)
	case len(remove.Folders) > 0:
		for _, folder := range s.folders(r) {
			r.Remove(folder)
		}
	case len(remove.Files) > 0:
		s.removeFiles(r)
	}
}

func (s Rule) age() age.Limit {
	limit := age.Limit{Now: s.env.Now}
	if key := s.file.Keep.WrittenWithin; key != "" {
		limit.Min = s.env.Duration(key)
	}
	return limit
}

func (s Rule) folders(r *report.Report) []string {
	var matches []string
	for _, pattern := range s.file.Remove.Folders {
		found, err := filepath.Glob(filepath.Join(s.root, pattern))
		if err != nil {
			r.Fail("find %s: %v", s.root, err)
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
	root, err := settings.Canonical(s.root)
	if err != nil {
		r.Fail("resolve %s: %v", s.root, err)
		return
	}
	limit := s.age()
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
		if !limit.Recent(info.ModTime()) {
			set.Add(r, path)
		}
		return nil
	})
	if err != nil {
		scanErrors = append(scanErrors, err)
	}
	if len(scanErrors) > 0 {
		r.Fail("scan %s: %v", s.root, errors.Join(scanErrors...))
	}
	set.RemoveUnless(r, s.keeper(r, limit))
}

func (s Rule) wanted(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(s.file.Remove.Files, func(pattern string) bool {
		if anywhere, ok := strings.CutPrefix(pattern, "**/"); ok {
			matched, _ := filepath.Match(anywhere, filepath.Base(rel))
			return matched
		}
		matched, _ := filepath.Match(pattern, rel)
		return matched
	})
}

func (s Rule) keeper(r *report.Report, limit age.Limit) func(path string) bool {
	open := s.env.Lsof.Every(openAnswersPerListing)
	failed := false
	return func(path string) bool {
		if failed {
			return true
		}
		held, err := open.HoldsAny(path)
		if err != nil {
			r.Fail("lsof %s: %v", s.root, err)
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
		if limit.Recent(info.ModTime()) {
			r.Skip("%s: written again since the scan", path)
			return true
		}
		return false
	}
}
