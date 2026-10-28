package leftovers

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
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

var keptInSystemTemp = []string{".*", "claude-*"}

type Step struct {
	Dir                string
	Patterns           []string
	Exclude            []string
	Files              bool
	Owner              *int
	MinKB              int64
	Minutes            int
	Now                func() time.Time
	Lsof               string
	LsofTimeoutSeconds int
	Batch              int
}

func SystemTemp() (Step, error) {
	minutes, err := settings.PositiveDuration("DISKCLEANER_TMP_MINUTES", 60, time.Minute, "minutes")
	if err != nil {
		return Step{}, err
	}
	minKB, err := settings.PositiveInt("DISKCLEANER_SYSTEM_TMP_MIN_KB", 1024)
	if err != nil {
		return Step{}, err
	}
	return Step{
		Dir:                settings.Env("DISKCLEANER_SYSTEM_TMP", "/private/tmp"),
		Patterns:           []string{"*"},
		Exclude:            keptInSystemTemp,
		Files:              true,
		Owner:              new(os.Getuid()),
		MinKB:              int64(minKB),
		Minutes:            minutes,
		Now:                time.Now,
		Lsof:               "lsof",
		LsofTimeoutSeconds: lsofTimeoutSeconds,
		Batch:              lsofBatch,
	}, nil
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
		paths = append(paths, slices.DeleteFunc(matches, s.excluded)...)
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
	if !s.owns(info) {
		return
	}
	var found tree
	switch {
	case info.IsDir():
		found, err = walk(path)
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		if err != nil {
			r.Fail("look into %s: %v", path, err)
			return
		}
	case s.Files && info.Mode().IsRegular():
		found.add(path, info)
	default:
		return
	}
	if found.kb < s.MinKB {
		return
	}
	if found.socket != "" {
		r.Skip("%s: holds the socket %s, which a process may be listening on", path, found.socket)
		return
	}
	if s.Now().Sub(found.latest) <= time.Duration(s.Minutes)*time.Minute {
		r.Skip("%s: written within %d minutes", path, s.Minutes)
		return
	}
	for batch := range slices.Chunk(found.entries, s.Batch) {
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

func (s Step) excluded(path string) bool {
	return slices.ContainsFunc(s.Exclude, func(pattern string) bool {
		matched, _ := filepath.Match(pattern, filepath.Base(path))
		return matched
	})
}

func (s Step) owns(info fs.FileInfo) bool {
	if s.Owner == nil {
		return true
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == *s.Owner
}

type tree struct {
	entries []string
	latest  time.Time
	kb      int64
	socket  string
}

func (t *tree) add(path string, info fs.FileInfo) {
	t.entries = append(t.entries, path)
	if info.ModTime().After(t.latest) {
		t.latest = info.ModTime()
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		t.kb += stat.Blocks * 512 / 1024
	}
	if info.Mode().Type() == fs.ModeSocket && t.socket == "" {
		t.socket = path
	}
}

func walk(root string) (tree, error) {
	return walkFS(os.DirFS(root), root)
}

func walkFS(fsys fs.FS, root string) (tree, error) {
	var found tree
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && name != "." {
			return nil
		}
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		found.add(filepath.Join(root, name), info)
		return nil
	})
	return found, err
}
