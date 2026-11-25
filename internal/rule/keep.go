package rule

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/botforge-pro/diskcleaner/internal/age"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

type KeepWhen struct {
	Age     AgeWhen  `yaml:"age"`
	Open    OpenWhen `yaml:"open"`
	Sockets bool     `yaml:"sockets"`
	Mounts  bool     `yaml:"mounts"`
	Tracked bool     `yaml:"tracked"`
}

type AgeWhen struct {
	Of     AgeOf  `yaml:"of"`
	Within string `yaml:"within"`
}

func (a AgeWhen) given() bool {
	return a.Within != "" || a.Of.Clock != "" || a.Of.Value != nil
}

type AgeOf struct {
	Clock string
	Value *Value
}

func (a *AgeOf) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		a.Clock = node.Value
		return nil
	}
	a.Value = &Value{}
	return strict(node, a.Value)
}

type OpenWhen struct {
	Anything bool
	Files    []string
}

func (o *OpenWhen) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		return node.Decode(&o.Anything)
	}
	return node.Decode(&o.Files)
}

func (o OpenWhen) given() bool {
	return o.Anything || len(o.Files) > 0
}

func (k KeepWhen) given() bool {
	return k.Age.given() || k.Open.given() || k.Sockets || k.Mounts || k.Tracked
}

var ages = []string{"mtime", "newest-write"}

func (f File) checkKeep() error {
	k := f.Keep
	if !k.given() {
		return nil
	}
	if k.Age.given() {
		switch {
		case k.Age.Of.Value != nil:
			if err := k.Age.Of.Value.check(); err != nil {
				return fmt.Errorf("keep: age: of: %w", err)
			}
		case !slices.Contains(ages, k.Age.Of.Clock):
			return fmt.Errorf("keep: age: of %q: want one of %v, or {file, key}, a date read from the folder", k.Age.Of.Clock, ages)
		}
		if !hasSetting(f.Settings, k.Age.Within, "duration") && k.Age.Within != "min-age" {
			return fmt.Errorf("keep: age: within %q: want the key of one of this step's duration settings or min-age", k.Age.Within)
		}
	}
	for _, pattern := range k.Open.Files {
		if !inside(pattern) {
			return fmt.Errorf("keep: open: %q: want a pattern inside what is removed, like */Contents/MacOS/*", pattern)
		}
	}
	if f.Remove.Root || len(f.Remove.Commands) > 0 {
		return errors.New("keep: want remove: folders or files; a root or a command has nothing to keep")
	}
	if len(f.Remove.Files) > 0 && (k.Sockets || k.Mounts || len(k.Open.Files) > 0 || k.Age.Of.Clock != "mtime" && k.Age.given()) {
		return errors.New("keep: for files, want age of mtime, open: true and tracked; the rest is for folders")
	}
	return nil
}

func (s Rule) ageLimit() age.Limit {
	limit := age.Limit{Now: s.env.Now}
	if key := s.file.Keep.Age.Within; key != "" {
		limit.Min = s.duration(key)
	}
	return limit
}

func (s Rule) duration(key string) time.Duration {
	if hasSetting(s.file.Settings, key, "") {
		return s.env.Duration(s.file.Name + "." + key)
	}
	return s.env.Duration(key)
}

func (s Rule) removeFolders(r *report.Report, folders []string) {
	open := s.env.Lsof.Every(openAnswersPerListing)
	failed := false
	keep := func(path string) bool {
		if failed {
			return true
		}
		kept, err := s.keepFolder(r, path, open)
		if err != nil {
			r.Fail("lsof %s: %v", s.root, err)
			failed = true
			return true
		}
		return kept
	}
	s.removeAll(r, folders, keep)
}

func (s Rule) keepFolder(r *report.Report, path string, open *inuse.Listing) (bool, error) {
	k := s.file.Keep
	limit := s.ageLimit()
	if k.Mounts || k.Sockets || k.Age.Of.Clock == "newest-write" {
		volume, err := os.Stat(s.root)
		if err != nil {
			r.Fail("stat %s: %v", s.root, err)
			return true, nil
		}
		found, err := inspect(path, device(volume))
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		if err != nil {
			r.Fail("look into %s: %v", path, err)
			return true, nil
		}
		switch {
		case k.Mounts && found.mounted == path:
			r.Skip("%s: a mounted volume, not space on this disk", path)
			return true, nil
		case k.Mounts && found.mounted != "":
			r.Skip("%s: %s in it is a mounted volume, not space on this disk", path, found.mounted)
			return true, nil
		case k.Sockets && found.socket != "":
			r.Skip("%s: holds the socket %s, which a process may be listening on", path, found.socket)
			return true, nil
		case k.Age.Of.Clock == "newest-write" && limit.Recent(found.latest):
			return true, nil
		}
	}
	if k.Age.Of.Clock == "mtime" {
		info, err := os.Lstat(path)
		if err != nil {
			r.Fail("stat %s: %v", path, err)
			return true, nil
		}
		if limit.Recent(info.ModTime()) {
			return true, nil
		}
	}
	if v := k.Age.Of.Value; v != nil {
		made, err := v.time(path)
		if err != nil {
			r.Skip("%s: %s does not say when it was made", path, v.File)
			r.Fail("read %s: %v", filepath.Join(path, v.File), err)
			return true, nil
		}
		if limit.Recent(made) {
			return true, nil
		}
	}
	switch {
	case len(k.Open.Files) > 0:
		files, err := matchingFiles(path, k.Open.Files)
		if err != nil {
			r.Fail("look into %s: %v", path, err)
			return true, nil
		}
		if len(files) == 0 {
			r.Skip("%s: nothing in it matches %v to ask lsof about", path, k.Open.Files)
			return true, nil
		}
		held, err := open.HoldsAny(files...)
		if err != nil {
			return true, err
		}
		if held {
			r.Skip("%s: a running program was started from it", path)
			return true, nil
		}
	case k.Open.Anything:
		held, err := open.HoldsAny(path)
		if err != nil {
			return true, err
		}
		if held {
			r.Skip("%s: a process still has something in it open", path)
			return true, nil
		}
	}
	return false, nil
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
	return inspectFS(os.DirFS(root), root, volume)
}

func inspectFS(fsys fs.FS, root string, volume int64) (tree, error) {
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
