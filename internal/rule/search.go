package rule

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
)

type found struct {
	path string
	kind Kind
}

type walk struct {
	c       *ctx
	entries []Entry
	volume  uint64
	owner   uint32
	mounts  map[string]bool
	found   []found
	failed  bool
}

func (c *ctx) search(entries []Entry) ([]found, bool) {
	var root unix.Stat_t
	if err := unix.Stat(c.folder, &root); err != nil {
		c.r.Fail("look into %s: %v", c.folder, err)
		return nil, false
	}
	mounts, ok := c.mountTable()
	if !ok {
		return nil, false
	}
	w := walk{c: c, entries: entries, volume: uint64(root.Dev), owner: uint32(os.Getuid()), mounts: mounts}
	var searching []int
	for i, e := range entries {
		if e.kind != Skip {
			searching = append(searching, i)
		}
	}
	w.dir(c.folder, nil, searching)
	return w.found, !w.failed
}

func (c *ctx) mountTable() (map[string]bool, bool) {
	if c.mounts == nil {
		mounts, err := c.env.Mounts()
		if err != nil {
			c.r.Fail("%s: removed nothing, as it cannot list mounted volumes: %v", c.subject(), err)
			return nil, false
		}
		c.mounts = mounts
	}
	return c.mounts, true
}

func (w *walk) dir(dir string, rel []string, searching []int) {
	children, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		w.c.r.Fail("look into %s: %v", dir, err)
		return
	}
	for _, child := range children {
		if w.failed || w.c.halted() {
			return
		}
		full := filepath.Join(dir, child.Name())
		parts := append(slices.Clone(rel), child.Name())
		var stat unix.Stat_t
		err := unix.Lstat(full, &stat)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			w.c.r.Fail("stat %s: %v", full, err)
			continue
		}
		isDir := stat.Mode&unix.S_IFMT == unix.S_IFDIR
		isFile := stat.Mode&unix.S_IFMT == unix.S_IFREG
		if isDir && (uint64(stat.Dev) != w.volume || w.mounts[full]) {
			w.c.r.Skip("%s: a mounted volume, not space on this disk", full)
			continue
		}
		if stat.Uid != w.owner {
			continue
		}
		deeper, ok := w.decide(full, parts, isDir, isFile, searching)
		if !ok {
			w.failed = true
			return
		}
		if isDir && len(deeper) > 0 {
			w.dir(full, parts, deeper)
		}
	}
}

func (w *walk) decide(full string, parts []string, isDir, isFile bool, searching []int) ([]int, bool) {
	var deeper []int
	taken := false
	says := ""
	skipped := make([]bool, len(w.entries))
	skipSays := ""
	for i, e := range w.entries {
		if e.kind != Skip || !e.names(full, parts, isDir, isFile) {
			continue
		}
		held, ok := w.c.entryHolds(e, full)
		if !ok {
			return nil, false
		}
		if held {
			skipped[i] = true
			if skipSays == "" {
				skipSays = e.Says
			}
		}
	}
	for _, i := range searching {
		e := w.entries[i]
		if slices.Contains(skipped[:i], true) {
			continue
		}
		if e.names(full, parts, isDir, isFile) {
			held, ok := w.c.entryHolds(e, full)
			if !ok {
				return nil, false
			}
			switch {
			case held && !taken:
				taken = true
				w.found = append(w.found, found{path: full, kind: e.kind})
			case !held && says == "":
				says = e.Says
			}
			continue
		}
		if slices.Contains(skipped[i:], true) {
			continue
		}
		if reachable(e.parts, parts) {
			deeper = append(deeper, i)
		}
	}
	if says == "" {
		says = skipSays
	}
	if !taken && says != "" {
		w.c.r.Skip("%s: %s", full, says)
	}
	return deeper, true
}

func (e Entry) names(full string, parts []string, isDir, isFile bool) bool {
	switch {
	case e.folderOnly && !isDir:
		return false
	case e.kind == Folders && !isDir, e.kind == Files && !isFile, e.kind == Empty && !isDir:
		return false
	case e.name != nil && !e.name.MatchString(filepath.Base(full)):
		return false
	case !matchDeep(e.parts, parts):
		return false
	case e.kind == Empty:
		children, err := os.ReadDir(full)
		return err == nil && len(children) == 0
	}
	return true
}

func (c *ctx) entryHolds(e Entry, about string) (bool, bool) {
	for _, cond := range e.When {
		held, ok := c.holds(cond, about, c.folder)
		if !ok || !held {
			return false, ok
		}
	}
	for _, cond := range e.Unless {
		held, ok := c.holds(cond, about, c.folder)
		if !ok || held {
			return false, ok
		}
	}
	return true, true
}

func matchDeep(pattern, parts []string) bool {
	if len(pattern) == 0 {
		return len(parts) == 0
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(parts); skip++ {
			if matchDeep(pattern[1:], parts[skip:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	matched, _ := path.Match(pattern[0], parts[0])
	return matched && matchDeep(pattern[1:], parts[1:])
}

func reachable(pattern, parts []string) bool {
	switch {
	case len(parts) == 0:
		return len(pattern) > 0
	case len(pattern) == 0:
		return false
	case pattern[0] == "**":
		return true
	}
	matched, _ := path.Match(pattern[0], parts[0])
	return matched && reachable(pattern[1:], parts[1:])
}
