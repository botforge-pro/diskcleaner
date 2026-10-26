package leftovers

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

const (
	lsofTimeoutSeconds = 30
	lsofBatch          = 200
)

var patterns = []string{"go-build*", "go-link-*", "XcodeDistPipeline.*"}

type Step struct {
	Dir                string
	Patterns           []string
	Minutes            int
	Now                func() time.Time
	Lsof               string
	LsofTimeoutSeconds int
	Batch              int
}

func Default() (Step, error) {
	dir := settings.Env("DISKCLEANER_USER_TMP", "")
	if dir == "" {
		temp, err := settings.UserTemp()
		if err != nil {
			return Step{}, fmt.Errorf("%w; set DISKCLEANER_USER_TMP instead", err)
		}
		dir = temp
	}
	minutes, err := settings.PositiveDuration("DISKCLEANER_TMP_MINUTES", 60, time.Minute, "minutes")
	if err != nil {
		return Step{}, err
	}
	return Step{
		Dir:                dir,
		Patterns:           patterns,
		Minutes:            minutes,
		Now:                time.Now,
		Lsof:               "lsof",
		LsofTimeoutSeconds: lsofTimeoutSeconds,
		Batch:              lsofBatch,
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
	var paths []string
	for _, pattern := range s.Patterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			r.Fail("glob %s: %v", pattern, err)
			continue
		}
		paths = append(paths, matches...)
	}
	if len(paths) == 0 {
		return
	}
	fmt.Fprintf(r.Out, "TEMP     %s\n", dir)
	defer fmt.Fprintln(r.Out)

	slices.Sort(paths)
	for _, path := range paths {
		s.check(r, path)
	}
}

func (s Step) check(r *report.Report, path string) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		r.Fail("stat %s: %v", path, err)
		return
	}
	if !info.IsDir() {
		return
	}
	entries, latest, err := walk(path)
	if err != nil {
		r.Fail("look into %s: %v", path, err)
		return
	}
	if s.Now().Sub(latest) <= time.Duration(s.Minutes)*time.Minute {
		r.Skip("%s: written within %d minutes", path, s.Minutes)
		return
	}
	for batch := range slices.Chunk(entries, s.Batch) {
		open, err := process.OpenFiles(s.LsofTimeoutSeconds, s.Lsof, batch)
		if err != nil {
			r.Fail("lsof in %s: %v", path, err)
			return
		}
		if len(open) > 0 {
			r.Skip("%s: a process still has something in it open", path)
			return
		}
	}
	r.Remove(path)
}

func walk(root string) ([]string, time.Time, error) {
	var entries []string
	var latest time.Time
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		entries = append(entries, path)
		return nil
	})
	return entries, latest, err
}
