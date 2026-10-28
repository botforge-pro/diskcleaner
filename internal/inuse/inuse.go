package inuse

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const lsofTimeoutSeconds = 30

type Check struct {
	Minutes            int
	Now                func() time.Time
	Lsof               string
	LsofTimeoutSeconds int
}

func Default() (Check, error) {
	minutes, err := settings.PositiveDuration("DISKCLEANER_TMP_MINUTES", 60, time.Minute, "minutes")
	if err != nil {
		return Check{}, err
	}
	return Check{Minutes: minutes, Now: time.Now, Lsof: "lsof", LsofTimeoutSeconds: lsofTimeoutSeconds}, nil
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
	return c.Now().Add(-time.Duration(c.Minutes) * time.Minute)
}

func (c Check) written() string {
	return fmt.Sprintf("written within %d minutes", c.Minutes)
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
	names, err := process.OpenNames(c.LsofTimeoutSeconds, c.Lsof)
	if err != nil {
		return "", fmt.Errorf("lsof: %w", err)
	}
	inside := path + string(filepath.Separator)
	if slices.ContainsFunc(names, func(name string) bool { return name == path || strings.HasPrefix(name, inside) }) {
		return "a process still has something in it open", nil
	}
	return "", nil
}
