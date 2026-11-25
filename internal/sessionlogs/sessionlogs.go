package sessionlogs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const lsofBatch = 50

type Step struct {
	Label  string
	Folder string
	MinAge time.Duration
	Now    func() time.Time
	Lsof   inuse.Lsof
	Batch  int
}

func Claude(home string, minAge time.Duration) Step {
	return logs("CLAUDE", filepath.Join(settings.Env("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "projects"), minAge)
}

func Codex(home string, minAge time.Duration) Step {
	return logs("CODEX", filepath.Join(settings.Env("CODEX_HOME", filepath.Join(home, ".codex")), "sessions"), minAge)
}

func logs(label, folder string, minAge time.Duration) Step {
	return Step{
		Label:  label,
		Folder: folder,
		MinAge: minAge,
		Now:    time.Now,
		Lsof:   inuse.DefaultLsof(),
		Batch:  lsofBatch,
	}
}

func (s Step) old(modified time.Time) bool {
	return s.Now().Sub(modified) >= s.MinAge
}

func (s Step) Run(r *report.Report) {
	if !r.Directory(s.Folder) {
		return
	}
	fmt.Fprintf(r.Out, "%-8s %s\n", s.Label, s.Folder)
	defer fmt.Fprintln(r.Out)
	folder, err := filepath.EvalSymlinks(s.Folder)
	if err != nil {
		r.Fail("resolve %s: %v", s.Folder, err)
		return
	}

	var set candidates.Set
	var walkErrors []error
	err = filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			walkErrors = append(walkErrors, err)
			return nil
		}
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			walkErrors = append(walkErrors, err)
			return nil
		}
		if s.old(info.ModTime()) {
			set.Add(r, path)
		}
		return nil
	})
	if err != nil {
		walkErrors = append(walkErrors, err)
	}
	if len(walkErrors) > 0 {
		r.Fail("scan %s: %v", s.Folder, errors.Join(walkErrors...))
	}
	open := s.Lsof.Every(s.Batch)
	failed := false
	set.RemoveUnless(r, func(path string) bool {
		if failed {
			return true
		}
		held, err := open.HoldsAny(path)
		if err != nil {
			r.Fail("lsof %s: %v", s.Folder, err)
			failed = true
			return true
		}
		return s.inUse(r, path, held)
	})
}

func (s Step) inUse(r *report.Report, path string, held bool) bool {
	if held {
		r.Skip("%s: a process still has it open", path)
		return true
	}
	info, err := os.Lstat(path)
	if err != nil {
		r.Fail("stat %s: %v", path, err)
		return true
	}
	if !s.old(info.ModTime()) {
		r.Skip("%s: written again since the scan", path)
		return true
	}
	return false
}
