package inuse

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

type Check struct {
	MinAge time.Duration
	Now    func() time.Time
	Open   *Listing
}

func Default(minAge time.Duration) Check {
	return Check{MinAge: minAge, Now: time.Now, Open: DefaultLsof().Every(1)}
}

func (c Check) WithOneListing() Check {
	c.Open = c.Open.Lsof().Once()
	return c
}

func (c Check) Keep(r *report.Report, path string) bool {
	reason, err := c.Reason(path)
	switch {
	case err != nil:
		r.Fail("%s: %v", path, err)
		return true
	case reason != "":
		r.Skip("%s: %s", path, reason)
		return true
	}
	return false
}

func (c Check) Reason(path string) (string, error) {
	fresh, err := c.writtenSince(path)
	if err != nil {
		return "", err
	}
	if fresh {
		return c.written(), nil
	}
	return c.open(path)
}

func (c Check) ReasonAfter(path string, latest time.Time) (string, error) {
	if !latest.Before(c.cutoff()) {
		return c.written(), nil
	}
	return c.open(path)
}

func (c Check) cutoff() time.Time {
	return c.Now().Add(-c.MinAge)
}

func (c Check) written() string {
	return fmt.Sprintf("written within %s", report.FormatDuration(c.MinAge))
}

func (c Check) writtenSince(root string) (bool, error) {
	cutoff := c.cutoff()
	fresh := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path != root {
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
		if !info.ModTime().Before(cutoff) {
			fresh = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("look into %s: %w", root, err)
	}
	return fresh, nil
}

func (c Check) open(path string) (string, error) {
	held, err := c.Open.HoldsAny(path)
	if err != nil {
		return "", fmt.Errorf("lsof: %w", err)
	}
	if held {
		return "a process still has something in it open", nil
	}
	return "", nil
}
