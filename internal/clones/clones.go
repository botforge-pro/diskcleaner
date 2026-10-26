package clones

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const lsofTimeoutSeconds = 30

var binaries = []string{
	"*/Contents/MacOS/*",
	"*/Contents/Frameworks/*.framework/Versions/*/*",
}

type Step struct {
	Dir                string
	Minutes            int
	Now                func() time.Time
	Lsof               string
	LsofTimeoutSeconds int
}

func Default() (Step, error) {
	dir := settings.Env("DISKCLEANER_CODE_SIGN_CLONES", "")
	if dir == "" {
		temp, err := settings.UserTemp()
		if err != nil {
			return Step{}, fmt.Errorf("%w; set DISKCLEANER_CODE_SIGN_CLONES instead", err)
		}
		dir = filepath.Join(filepath.Dir(temp), "X")
	}
	minutes, err := settings.PositiveDuration("DISKCLEANER_TMP_MINUTES", 60, time.Minute, "minutes")
	if err != nil {
		return Step{}, err
	}
	return Step{
		Dir:                dir,
		Minutes:            minutes,
		Now:                time.Now,
		Lsof:               "lsof",
		LsofTimeoutSeconds: lsofTimeoutSeconds,
	}, nil
}

func (s Step) Run(r *report.Report) {
	if !r.Directory(s.Dir) {
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
	for _, clone := range clones {
		s.check(r, clone)
	}
}

func (s Step) check(r *report.Report, clone string) {
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
	if s.Now().Sub(info.ModTime()) <= time.Duration(s.Minutes)*time.Minute {
		r.Skip("%s: made within %d minutes", clone, s.Minutes)
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
	open, err := process.OpenFiles(s.LsofTimeoutSeconds, s.Lsof, files)
	if err != nil {
		r.Fail("lsof in %s: %v", clone, err)
		return
	}
	if len(open) > 0 {
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
