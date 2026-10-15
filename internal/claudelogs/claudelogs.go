package claudelogs

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

type Step struct {
	Projects string
	Days     int
	Now      func() time.Time
}

func Default(home string) (Step, error) {
	days, err := settings.PositiveInt("DISKCLEANER_CLAUDE_LOG_DAYS", 30)
	if err != nil {
		return Step{}, err
	}
	return Step{
		Projects: settings.Env("DISKCLEANER_CLAUDE_PROJECTS", filepath.Join(home, ".claude", "projects")),
		Days:     days,
		Now:      time.Now,
	}, nil
}

func (s Step) Run(r *report.Report) {
	if !r.Directory(s.Projects) {
		return
	}
	fmt.Fprintf(r.Out, "CLAUDE   %s\n", s.Projects)
	defer fmt.Fprintln(r.Out)

	now := s.Now()
	var set candidates.Set
	var walkErrors []error
	err := filepath.WalkDir(s.Projects, func(path string, entry fs.DirEntry, err error) error {
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
		if int(now.Sub(info.ModTime())/(24*time.Hour)) > s.Days {
			set.Add(r, path)
		}
		return nil
	})
	if err != nil {
		walkErrors = append(walkErrors, err)
	}
	if len(walkErrors) > 0 {
		r.Fail("scan %s: %v", s.Projects, errors.Join(walkErrors...))
	}
	set.Remove(r)
}
