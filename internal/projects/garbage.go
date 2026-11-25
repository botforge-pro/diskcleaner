package projects

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/age"
	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

var repackPid = regexp.MustCompile(`^\.tmp-([0-9]+)-`)

type GitGarbage struct {
	Age  age.Limit
	Lsof inuse.Lsof
}

func DefaultGitGarbage(minAge time.Duration) GitGarbage {
	return GitGarbage{Age: age.Of(minAge), Lsof: inuse.DefaultLsof()}
}

func (g GitGarbage) Collect(r *report.Report, set *candidates.Set, repo string) {
	objects := filepath.Join(repo, ".git", "objects")
	entries, err := os.ReadDir(objects)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		r.Fail("read %s: %v", objects, err)
		return
	}
	var files []string
	for _, entry := range entries {
		path := filepath.Join(objects, entry.Name())
		if strings.HasPrefix(entry.Name(), "tmp_") {
			files = append(files, path)
		}
		if !entry.IsDir() {
			continue
		}
		inner, err := os.ReadDir(path)
		if err != nil {
			r.Fail("read %s: %v", path, err)
			continue
		}
		for _, file := range inner {
			name := file.Name()
			if strings.HasPrefix(name, "tmp_") || entry.Name() == "pack" && strings.HasPrefix(name, ".tmp-") {
				files = append(files, filepath.Join(path, name))
			}
		}
	}
	slices.Sort(files)
	open := g.Lsof.Once()
	for _, file := range files {
		g.check(r, set, open, file)
	}
}

func (g GitGarbage) check(r *report.Report, set *candidates.Set, open *inuse.Listing, file string) {
	info, err := os.Lstat(file)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		r.Fail("stat %s: %v", file, err)
		return
	}
	if !info.Mode().IsRegular() {
		return
	}
	if g.Age.Recent(info.ModTime()) {
		r.Skip("%s: written within %s", file, g.Age)
		return
	}
	if match := repackPid.FindStringSubmatch(filepath.Base(file)); match != nil {
		if pid, err := strconv.Atoi(match[1]); err == nil && alive(pid) {
			r.Skip("%s: the git repack that writes it is still running", file)
			return
		}
	}
	held, err := open.HoldsAny(file)
	switch {
	case err != nil:
		r.Fail("lsof %s: %v", file, err)
	case held:
		r.Skip("%s: a process still has it open", file)
	default:
		set.Add(r, file)
	}
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
