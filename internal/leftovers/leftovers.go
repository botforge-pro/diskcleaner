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

type Kind struct {
	Patterns  []string
	Processes string
	Busy      string
}

type Step struct {
	Dir     string
	Minutes int
	Kinds   []Kind
	Now     func() time.Time
	Running func(pattern string) (bool, error)
}

func Default() (Step, error) {
	dir, err := settings.EnvOrUserTemp("DISKCLEANER_USER_TMP", func(temp string) string { return temp })
	if err != nil {
		return Step{}, err
	}
	minutes, err := settings.PositiveDuration("DISKCLEANER_TMP_MINUTES", 60, time.Minute, "minutes")
	if err != nil {
		return Step{}, err
	}
	return Step{
		Dir:     dir,
		Minutes: minutes,
		Kinds: []Kind{
			{
				Patterns:  []string{"go-build*", "go-link-*"},
				Processes: settings.Env("DISKCLEANER_GO_PROCESSES", process.Go),
				Busy:      "a go command is running",
			},
			{
				Patterns:  []string{"XcodeDistPipeline.*"},
				Processes: settings.Env("DISKCLEANER_XCODE_PROCESSES", process.Xcode),
				Busy:      "Xcode or an Xcode build is running",
			},
		},
		Now:     time.Now,
		Running: process.Running,
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
	fmt.Fprintf(r.Out, "TEMP     %s\n", dir)
	defer fmt.Fprintln(r.Out)

	for _, kind := range s.Kinds {
		s.clean(r, dir, kind)
	}
}

func (s Step) clean(r *report.Report, dir string, kind Kind) {
	var paths []string
	for _, pattern := range kind.Patterns {
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
	slices.Sort(paths)
	if r.Busy(s.Running, kind.Processes) {
		for _, pattern := range kind.Patterns {
			r.Skip("%s: %s", filepath.Join(dir, pattern), kind.Busy)
		}
		return
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			r.Fail("stat %s: %v", path, err)
			continue
		}
		if !info.IsDir() {
			continue
		}
		if s.Now().Sub(info.ModTime()) <= time.Duration(s.Minutes)*time.Minute {
			r.Skip("%s: written within %d minutes", path, s.Minutes)
			continue
		}
		r.Remove(path)
	}
}
