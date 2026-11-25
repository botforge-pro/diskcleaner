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

	"golang.org/x/sys/unix"

	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

type Untouched struct {
	For         string   `yaml:"for"`
	Except      []string `yaml:"except"`
	ExceptAtTop []string `yaml:"except-at-top"`
	age         time.Duration
}

func (u *Untouched) check() error {
	age, err := literalDuration(u.For)
	if err != nil {
		return fmt.Errorf("remove: untouched: for: %w", err)
	}
	u.age = age
	for _, pattern := range append(slices.Clone(u.Except), u.ExceptAtTop...) {
		if _, err := filepath.Match(pattern, ""); err != nil || pattern == "" || strings.Contains(pattern, "/") {
			return fmt.Errorf("remove: untouched: except %q: want a name pattern", pattern)
		}
	}
	return nil
}

func MountPoints() (map[string]bool, error) {
	return mountPoints()
}

type sweeper struct {
	Dir       string
	Owner     int
	Now       func() time.Time
	Lsof      inuse.Lsof
	Mounts    func() (map[string]bool, error)
	age       time.Duration
	keep      []string
	keepAtTop []string
}

func (s Rule) sweeper() sweeper {
	u := s.remove.Untouched
	return sweeper{
		Dir:       s.root,
		Owner:     os.Getuid(),
		Now:       s.env.Now,
		Lsof:      s.env.Lsof,
		Mounts:    s.env.Mounts,
		age:       u.age,
		keep:      u.Except,
		keepAtTop: u.ExceptAtTop,
	}
}

type found struct {
	volume  int64
	files   []string
	folders []string
	empty   []string
	young   int
	mounted []string
}

func (f found) nothing() bool {
	return len(f.files) == 0 && len(f.empty) == 0 && f.young == 0 && len(f.mounted) == 0
}

func (s sweeper) run(r *report.Report) {
	dir, err := settings.Canonical(s.Dir)
	if err != nil {
		r.Fail("resolve %s: %v", s.Dir, err)
		return
	}
	cutoff := s.Now().Add(-s.age)
	f, err := s.find(r, dir, cutoff)
	if err != nil {
		r.Fail("look into %s: %v", dir, err)
		return
	}
	if f.nothing() {
		return
	}
	for _, path := range f.mounted {
		r.Skip("%s: a mounted volume, not space on this disk", path)
	}
	if f.young > 0 {
		r.Skip("%s: %s younger than %s by access, change or write time", dir, files(f.young), s.untouched())
	}
	removable := append(f.files, f.empty...)
	if len(removable) == 0 {
		return
	}
	open, err := s.Lsof.Take()
	if err != nil {
		r.Fail("%s: removed nothing, as it cannot tell which files are open: lsof: %v", dir, err)
		return
	}
	mounts, err := s.Mounts()
	if err != nil {
		r.Fail("%s: removed nothing, as it cannot list mounted volumes: %v", dir, err)
		return
	}
	for _, path := range removable {
		if open.Holds(path) {
			r.Skip("%s: a process has it open", path)
			continue
		}
		s.removeIfStillOld(r, dir, path, f.volume, mounts, cutoff)
	}
}

func (s sweeper) untouched() string {
	if s.age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d days", s.age/(24*time.Hour))
	}
	return s.age.String()
}

func (s sweeper) removeIfStillOld(r *report.Report, dir, path string, volume int64, mounts map[string]bool, cutoff time.Time) {
	entry, err := openEntry(dir, path, volume, mounts)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		r.Skip("%s: changed since it was found: %v", path, err)
		return
	}
	defer entry.close()
	stat := entry.stat
	isDir := stat.Mode&unix.S_IFMT == unix.S_IFDIR
	switch {
	case int(stat.Uid) != s.Owner:
		r.Skip("%s: changed owner since it was found", path)
	case isDir && moment(stat.Mtim).Before(cutoff), stat.Mode&unix.S_IFMT == unix.S_IFREG && old(&stat, cutoff):
		r.RemoveEntry(path, stat.Blocks*512, func() error { return entry.remove(isDir) })
	default:
		r.Skip("%s: used since it was found", path)
	}
}

func old(stat *unix.Stat_t, cutoff time.Time) bool {
	return !slices.ContainsFunc([]unix.Timespec{stat.Atim, stat.Mtim, stat.Ctim}, func(t unix.Timespec) bool {
		return !moment(t).Before(cutoff)
	})
}

func moment(t unix.Timespec) time.Time {
	return time.Unix(t.Unix())
}

func files(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

func (s sweeper) find(r *report.Report, dir string, cutoff time.Time) (found, error) {
	var root unix.Stat_t
	if err := unix.Stat(dir, &root); err != nil {
		return found{}, err
	}
	mounts, err := s.Mounts()
	if err != nil {
		return found{}, fmt.Errorf("list mounted volumes: %w", err)
	}
	f := found{volume: int64(root.Dev)}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if path == dir {
			return err
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			r.Fail("look into %s: %v", path, err)
			return nil
		}
		if s.kept(dir, path) {
			return skip(entry)
		}
		if mounts[path] {
			f.mounted = append(f.mounted, path)
			return skip(entry)
		}
		return s.visit(r, &f, path, entry, cutoff)
	})
	if err != nil {
		return found{}, err
	}
	f.empty = emptyOf(r, f.folders)
	slices.Sort(f.files)
	return f, nil
}

func (s sweeper) visit(r *report.Report, f *found, path string, entry fs.DirEntry, cutoff time.Time) error {
	var stat unix.Stat_t
	err := unix.Lstat(path, &stat)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		r.Fail("stat %s: %v", path, err)
		return skip(entry)
	}
	if int64(stat.Dev) != f.volume {
		f.mounted = append(f.mounted, path)
		return skip(entry)
	}
	if int(stat.Uid) != s.Owner {
		return skip(entry)
	}
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		if moment(stat.Mtim).Before(cutoff) {
			f.folders = append(f.folders, path)
		}
	case unix.S_IFREG:
		if old(&stat, cutoff) {
			f.files = append(f.files, path)
		} else {
			f.young++
		}
	}
	return nil
}

func emptyOf(r *report.Report, folders []string) []string {
	var empty []string
	for _, folder := range folders {
		entries, err := os.ReadDir(folder)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			r.Fail("look into %s: %v", folder, err)
		case len(entries) == 0:
			empty = append(empty, folder)
		}
	}
	return empty
}

func (s sweeper) kept(dir, path string) bool {
	name := filepath.Base(path)
	matches := func(pattern string) bool {
		matched, _ := filepath.Match(pattern, name)
		return matched
	}
	return slices.ContainsFunc(s.keep, matches) ||
		filepath.Dir(path) == dir && slices.ContainsFunc(s.keepAtTop, matches)
}

func skip(entry fs.DirEntry) error {
	if entry != nil && entry.IsDir() {
		return fs.SkipDir
	}
	return nil
}
