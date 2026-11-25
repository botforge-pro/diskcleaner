package rule

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/botforge-pro/diskcleaner/internal/age"
	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

func (c *ctx) removeIn(n *Node) {
	var all []found
	if n.Remove.Itself {
		if info, err := os.Lstat(c.written); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			if c.allowed() {
				c.removeLink(c.written)
			}
			return
		}
		all = []found{{path: c.folder, kind: Folders}}
	} else {
		var ok bool
		if all, ok = c.search(n.Remove.Entries); !ok {
			c.broken = true
			return
		}
	}
	if n.Rank != nil {
		var ok bool
		if all, ok = c.rank(n.Rank, all); !ok {
			c.broken = true
			return
		}
	}
	for _, f := range all {
		if !c.allowed() {
			return
		}
		if _, err := os.Lstat(f.path); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if c.kept(n.Keep, f) {
			continue
		}
		c.remove(n.Keep, f)
	}
}

func (c *ctx) limit(k *Keep) age.Limit {
	limit := age.Limit{Now: c.env.Now}
	if k != nil && k.Age != nil {
		limit.Min = c.duration(k.Age.Within)
	}
	return limit
}

func (c *ctx) kept(k *Keep, f found) bool {
	isDir := f.kind != Files
	if k != nil && k.Tracked {
		switch candidates.GitTracking(c.r, f.path) {
		case candidates.Tracked:
			c.r.Skip("%s: contains files tracked by git", f.path)
			return true
		case candidates.Unknown:
			c.r.Skip("%s: git cannot tell whether it holds tracked files", f.path)
			return true
		}
	}
	var inside tree
	if isDir {
		volume, err := os.Stat(c.folder)
		if err != nil {
			c.r.Fail("stat %s: %v", c.folder, err)
			return true
		}
		inside, err = inspect(f.path, device(volume))
		if errors.Is(err, fs.ErrNotExist) {
			return true
		}
		if err != nil {
			c.r.Fail("look into %s: %v", f.path, err)
			return true
		}
		switch {
		case inside.mounted == f.path:
			c.r.Skip("%s: a mounted volume, not space on this disk", f.path)
			return true
		case inside.mounted != "":
			c.r.Skip("%s: %s in it is a mounted volume, not space on this disk", f.path, inside.mounted)
			return true
		case inside.socket != "":
			c.r.Skip("%s: holds the socket %s, which a process may be listening on", f.path, inside.socket)
			return true
		}
	}
	if k == nil {
		return false
	}
	if k.Age != nil {
		recent, ok := c.recent(k, f, inside)
		if !ok || recent {
			return true
		}
	}
	return c.held(k, f, isDir)
}

func (c *ctx) recent(k *Keep, f found, inside tree) (bool, bool) {
	limit := c.limit(k)
	if v := k.Age.Of.Value; v != nil {
		values, ok := c.eval(*v, f.path)
		if !ok {
			return false, false
		}
		if len(values) != 1 {
			c.r.Skip("%s: says nothing of when it was made", f.path)
			return false, false
		}
		made, err := parseTime(values[0])
		if err != nil {
			c.r.Skip("%s: says nothing of when it was made", f.path)
			c.r.Fail("%s: %q is not a date: %v", f.path, values[0], err)
			return false, false
		}
		return limit.Recent(made), true
	}
	if k.Age.Of.Name == newestWrite && f.kind != Files {
		return limit.Recent(inside.latest), true
	}
	var stat unix.Stat_t
	if err := unix.Lstat(f.path, &stat); err != nil {
		c.r.Fail("stat %s: %v", f.path, err)
		return false, false
	}
	return limit.Recent(clockOf(k.Age.Of.Name, &stat)), true
}

func clockOf(name string, stat *unix.Stat_t) time.Time {
	latest := moment(stat.Mtim)
	if name == anyTime && stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		for _, t := range []unix.Timespec{stat.Atim, stat.Ctim} {
			if moment(t).After(latest) {
				latest = moment(t)
			}
		}
	}
	return latest
}

func moment(t unix.Timespec) time.Time {
	return time.Unix(t.Unix())
}

func (c *ctx) held(k *Keep, f found, isDir bool) bool {
	if c.broken {
		return true
	}
	var paths []string
	switch {
	case len(k.Open.Inside) > 0:
		files, err := matchingFiles(f.path, k.Open.Inside)
		if err != nil {
			c.r.Fail("look into %s: %v", f.path, err)
			return true
		}
		if len(files) == 0 {
			c.r.Skip("%s: nothing in it matches %v to ask lsof about", f.path, k.Open.Inside)
			return true
		}
		paths = files
	case k.Open.Anything:
		paths = []string{f.path}
	default:
		return false
	}
	held, err := c.open.HoldsAny(paths...)
	if err != nil {
		c.r.Fail("lsof %s: %v", c.subject(), err)
		c.broken = true
		return true
	}
	switch {
	case !held:
		return false
	case len(k.Open.Inside) > 0:
		c.r.Skip("%s: a running program was started from it", f.path)
	case isDir:
		c.r.Skip("%s: a process still has something in it open", f.path)
	default:
		c.r.Skip("%s: a process still has it open", f.path)
	}
	return true
}

func (c *ctx) remove(k *Keep, f found) {
	base := c.folder
	if f.path == c.folder {
		base = filepath.Dir(c.folder)
	}
	volume, err := os.Stat(f.path)
	if err != nil {
		c.r.Fail("stat %s: %v", f.path, err)
		return
	}
	e, err := openEntry(base, f.path, device(volume))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		c.r.Skip("%s: changed since it was found: %v", f.path, err)
		return
	}
	defer e.close()
	kind := e.stat.Mode & unix.S_IFMT
	switch {
	case e.stat.Uid != uint32(os.Getuid()):
		c.r.Skip("%s: not yours", f.path)
		return
	case f.kind == Files && kind != unix.S_IFREG:
		c.r.Skip("%s: no longer a file since it was found", f.path)
		return
	case f.kind != Files && kind != unix.S_IFDIR:
		c.r.Skip("%s: no longer a folder since it was found", f.path)
		return
	case f.kind == Files && k != nil && k.Age != nil && k.Age.Of.Value == nil && c.limit(k).Recent(clockOf(k.Age.Of.Name, &e.stat)):
		c.r.Skip("%s: used since it was found", f.path)
		return
	}
	bytes := e.stat.Blocks * 512
	if f.kind == Folders {
		kb, err := report.DiskUsageKB(f.path)
		if err != nil {
			c.r.Fail("du %s: %v", f.path, err)
		}
		bytes = kb * 1024
	}
	volumeID := uint64(e.stat.Dev)
	c.r.RemoveEntry(f.path, bytes, func() error {
		switch f.kind {
		case Files:
			return unix.Unlinkat(e.parent, e.name, 0)
		case Empty:
			return unix.Unlinkat(e.parent, e.name, unix.AT_REMOVEDIR)
		}
		return removeTree(e.parent, e.name, volumeID)
	})
}

func (c *ctx) removeLink(link string) {
	parent := filepath.Dir(link)
	volume, err := os.Stat(parent)
	if err != nil {
		c.r.Fail("stat %s: %v", parent, err)
		return
	}
	e, err := openEntry(parent, link, device(volume))
	if err != nil {
		c.r.Skip("%s: changed since it was found: %v", link, err)
		return
	}
	defer e.close()
	if e.stat.Mode&unix.S_IFMT != unix.S_IFLNK {
		c.r.Skip("%s: no longer a link since it was found", link)
		return
	}
	c.r.RemoveEntry(link, 0, func() error { return unix.Unlinkat(e.parent, e.name, 0) })
}

func matchingFiles(dir string, patterns []string) ([]string, error) {
	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
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

func device(info fs.FileInfo) int64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return -1
	}
	return int64(stat.Dev)
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

func inspect(root string, volume int64) (tree, error) {
	found := tree{volume: volume}
	err := fs.WalkDir(os.DirFS(root), ".", func(name string, entry fs.DirEntry, err error) error {
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
