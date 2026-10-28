package sessionlogs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const (
	defaultDays        = 20
	lsofTimeoutSeconds = 30
	lsofBatch          = 50
)

type Step struct {
	Label              string
	Folder             string
	Days               int
	Now                func() time.Time
	Lsof               string
	LsofTimeoutSeconds int
	Batch              int
}

func Claude(home string) (Step, error) {
	return fromSettings("CLAUDE", "DISKCLEANER_CLAUDE_PROJECTS", filepath.Join(home, ".claude", "projects"), "DISKCLEANER_CLAUDE_LOG_DAYS")
}

func Codex(home string) (Step, error) {
	return fromSettings("CODEX", "DISKCLEANER_CODEX_SESSIONS", filepath.Join(home, ".codex", "sessions"), "DISKCLEANER_CODEX_LOG_DAYS")
}

func fromSettings(label, folderSetting, folder, daysSetting string) (Step, error) {
	days, err := settings.PositiveInt(daysSetting, defaultDays)
	if err != nil {
		return Step{}, err
	}
	return Step{
		Label:              label,
		Folder:             settings.Env(folderSetting, folder),
		Days:               days,
		Now:                time.Now,
		Lsof:               "lsof",
		LsofTimeoutSeconds: lsofTimeoutSeconds,
		Batch:              lsofBatch,
	}, nil
}

func (s Step) old(modified time.Time) bool {
	return int(s.Now().Sub(modified)/(24*time.Hour)) > s.Days
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
	paths := set.Paths()
	slices.Sort(paths)
	checks := openChecks{step: s, report: r, pending: paths}
	set.RemoveUnless(r, checks.inUse)
}

type openChecks struct {
	step    Step
	report  *report.Report
	pending []string
	open    map[string]bool
	checked map[string]bool
	failed  bool
}

func (c *openChecks) inUse(path string) bool {
	if c.failed {
		return true
	}
	if !c.checked[path] && !c.checkBatchFrom(path) {
		return true
	}
	return c.step.inUse(c.report, path, c.open)
}

func (c *openChecks) checkBatchFrom(path string) bool {
	start := slices.Index(c.pending, path)
	if start < 0 {
		c.report.Fail("lsof %s: %s was never queued for the open check", c.step.Folder, path)
		c.failed = true
		return false
	}
	c.pending = c.pending[start:]
	batch := c.pending[:min(c.step.Batch, len(c.pending))]
	c.pending = c.pending[len(batch):]
	names, err := process.OpenNames(c.step.LsofTimeoutSeconds, c.step.Lsof)
	if err != nil {
		c.report.Fail("lsof %s: %v", c.step.Folder, err)
		c.failed = true
		return false
	}
	c.open = map[string]bool{}
	c.checked = map[string]bool{}
	for _, path := range batch {
		c.checked[path] = true
		c.open[path] = process.Holds(names, path)
	}
	return true
}

func (s Step) inUse(r *report.Report, path string, open map[string]bool) bool {
	if open[path] {
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
