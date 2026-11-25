package clones

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

var binaries = []string{
	"*/Contents/MacOS/*",
	"*/Contents/Frameworks/*.framework/Versions/*/*",
}

type Step struct {
	Dir    string
	MinAge time.Duration
	Now    func() time.Time
	Lsof   inuse.Lsof
}

func Default(minAge time.Duration) (Step, error) {
	temp, err := settings.UserTemp()
	if err != nil {
		return Step{}, err
	}
	dir := ""
	if temp != "" {
		dir = filepath.Join(filepath.Dir(temp), "X")
	}
	return Step{
		Dir:    dir,
		MinAge: minAge,
		Now:    time.Now,
		Lsof:   inuse.DefaultLsof(),
	}, nil
}

func (s Step) Run(r *report.Report) {
	if s.Dir == "" || !r.Directory(s.Dir) {
		return
	}
	dir, err := filepath.EvalSymlinks(s.Dir)
	if err != nil {
		r.Fail("resolve %s: %v", s.Dir, err)
		return
	}
	clones, err := filepath.Glob(filepath.Join(dir, "*.code_sign_clone", "code_sign_clone.*"))
	if err != nil {
		r.Fail("glob %s: %v", dir, err)
		return
	}
	if len(clones) == 0 {
		return
	}
	fmt.Fprintf(r.Out, "CLONES   %s\n", dir)
	defer fmt.Fprintln(r.Out)

	slices.Sort(clones)
	open := s.Lsof.Every(1)
	for _, clone := range clones {
		s.check(r, open, clone)
	}
}

func (s Step) check(r *report.Report, open *inuse.Listing, clone string) {
	info, err := os.Lstat(clone)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		r.Fail("stat %s: %v", clone, err)
		return
	}
	if !info.IsDir() {
		return
	}
	if s.Now().Sub(info.ModTime()) <= s.MinAge {
		r.Skip("%s: made within %s", clone, report.FormatDuration(s.MinAge))
		return
	}
	files, err := executables(clone)
	if err != nil {
		r.Fail("look into %s: %v", clone, err)
		return
	}
	if len(files) == 0 {
		r.Skip("%s: no app program found in it to ask lsof about", clone)
		return
	}
	started, err := open.HoldsAny(files...)
	if err != nil {
		r.Fail("lsof in %s: %v", clone, err)
		return
	}
	if started {
		r.Skip("%s: a running app was started from it", clone)
		return
	}
	r.Remove(clone)
}

func executables(clone string) ([]string, error) {
	var files []string
	for _, pattern := range binaries {
		matches, err := filepath.Glob(filepath.Join(clone, pattern))
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			info, err := os.Lstat(match)
			if err != nil {
				return nil, err
			}
			if info.Mode().IsRegular() {
				files = append(files, match)
			}
		}
	}
	slices.Sort(files)
	return files, nil
}
