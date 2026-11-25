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

	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

var patterns = []string{"go-build*", "go-link-*", "XcodeDistPipeline.*"}

type Step struct {
	Dir      string
	Patterns []string
	InUse    inuse.Check
}

func Default(minAge time.Duration) (Step, error) {
	dir, err := settings.UserTemp()
	if err != nil {
		return Step{}, err
	}
	return Step{Dir: dir, Patterns: patterns, InUse: inuse.Default(minAge)}, nil
}

func (s Step) Run(r *report.Report) {
	if s.Dir == "" || !r.Directory(s.Dir) {
		return
	}
	dir, err := settings.Canonical(s.Dir)
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
	volume, err := os.Stat(dir)
	if err != nil {
		r.Fail("stat %s: %v", dir, err)
		return
	}
	fmt.Fprintf(r.Out, "TEMP     %s\n", dir)
	defer fmt.Fprintln(r.Out)

	slices.Sort(paths)
	for _, path := range paths {
		s.check(r, path, device(volume))
	}
}

func device(info fs.FileInfo) int64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return -1
	}
	return int64(stat.Dev)
}

func (s Step) check(r *report.Report, path string, volume int64) {
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
	found, err := walk(path, volume)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		r.Fail("look into %s: %v", path, err)
		return
	}
	if found.mounted == path {
		r.Skip("%s: a mounted volume, not space on this disk", path)
		return
	}
	if found.mounted != "" {
		r.Skip("%s: %s in it is a mounted volume, not space on this disk", path, found.mounted)
		return
	}
	if found.socket != "" {
		r.Skip("%s: holds the socket %s, which a process may be listening on", path, found.socket)
		return
	}
	reason, err := s.InUse.ReasonAfter(path, found.latest)
	if err != nil {
		r.Fail("%s: %v", path, err)
		return
	}
	if reason != "" {
		r.Skip("%s: %s", path, reason)
		return
	}
	r.Remove(path)
}

type tree struct {
	volume  int64
	latest  time.Time
	socket  string
	mounted string
}

func (t *tree) add(path string, info fs.FileInfo) {
	if on := device(info); on != -1 && on != t.volume {
		t.mounted = path
		return
	}
	if info.ModTime().After(t.latest) {
		t.latest = info.ModTime()
	}
	if info.Mode().Type() == fs.ModeSocket && t.socket == "" {
		t.socket = path
	}
}

func walk(root string, volume int64) (tree, error) {
	return walkFS(os.DirFS(root), root, volume)
}

func walkFS(fsys fs.FS, root string, volume int64) (tree, error) {
	found := tree{volume: volume}
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
		if found.mounted != "" {
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}
