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

var keptInSystemTemp = []string{".*", "claude-*"}

type Step struct {
	Dir      string
	Patterns []string
	Exclude  []string
	Files    bool
	Owner    *int
	MinKB    int64
	InUse    inuse.Check
}

func SystemTemp() (Step, error) {
	check, err := inuse.Default()
	if err != nil {
		return Step{}, err
	}
	minKB, err := settings.PositiveInt("DISKCLEANER_SYSTEM_TMP_MIN_KB", 1024)
	if err != nil {
		return Step{}, err
	}
	return Step{
		Dir:      settings.Env("DISKCLEANER_SYSTEM_TMP", "/private/tmp"),
		Patterns: []string{"*"},
		Exclude:  keptInSystemTemp,
		Files:    true,
		Owner:    new(os.Getuid()),
		MinKB:    int64(minKB),
		InUse:    check,
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
	check, err := inuse.Default()
	if err != nil {
		return Step{}, err
	}
	return Step{Dir: dir, Patterns: patterns, InUse: check}, nil
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
	if !s.owns(info) {
		return
	}
	found := tree{volume: volume}
	switch {
	case info.IsDir():
		found, err = walk(path, volume)
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
	if found.mounted == path {
		r.Skip("%s: a mounted volume, not space on this disk", path)
		return
	}
	if found.mounted != "" {
		r.Skip("%s: %s in it is a mounted volume, not space on this disk", path, found.mounted)
		return
	}
	if found.kb < s.MinKB {
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
	volume  int64
	latest  time.Time
	kb      int64
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
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		t.kb += stat.Blocks * 512 / 1024
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
