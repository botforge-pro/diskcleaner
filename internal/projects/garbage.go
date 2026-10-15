package projects

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

var repackPid = regexp.MustCompile(`^\.tmp-([0-9]+)-`)

type GitGarbage struct {
	Minutes            int
	LsofTimeoutSeconds int
	Now                func() time.Time
	Lsof               string
}

func DefaultGitGarbage() (GitGarbage, error) {
	minutes, err := settings.PositiveDuration("DISKCLEANER_GIT_TMP_MINUTES", 60, time.Minute, "minutes")
	if err != nil {
		return GitGarbage{}, err
	}
	timeout, err := settings.PositiveDuration("DISKCLEANER_GIT_LSOF_TIMEOUT", 30, time.Second, "seconds")
	if err != nil {
		return GitGarbage{}, err
	}
	return GitGarbage{Minutes: minutes, LsofTimeoutSeconds: timeout, Now: time.Now, Lsof: "lsof"}, nil
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
	for _, file := range files {
		g.check(r, set, file)
	}
}

func (g GitGarbage) check(r *report.Report, set *candidates.Set, file string) {
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
	if g.Now().Sub(info.ModTime()) <= time.Duration(g.Minutes)*time.Minute {
		r.Skip("%s: written within %d minutes", file, g.Minutes)
		return
	}
	if match := repackPid.FindStringSubmatch(filepath.Base(file)); match != nil {
		if pid, err := strconv.Atoi(match[1]); err == nil && alive(pid) {
			r.Skip("%s: the git repack that writes it is still running", file)
			return
		}
	}
	lsof := process.RunGroup(g.LsofTimeoutSeconds, g.Lsof, "--", file)
	var exit *exec.ExitError
	switch {
	case lsof.TimedOut:
		r.Fail("lsof %s: no answer within %ds", file, g.LsofTimeoutSeconds)
	case lsof.Leftover:
		r.Fail("lsof %s: exited but left a process running, which was stopped: %v", file, lsof.Err)
	case lsof.Err == nil:
		r.Skip("%s: a process still has it open", file)
	case !exists(file):
	case errors.As(lsof.Err, &exit) && exit.ExitCode() == 1 && lsof.Output() == "":
		set.Add(r, file)
	case errors.As(lsof.Err, &exit):
		r.Fail("lsof %s: exit %d: %s", file, exit.ExitCode(), lsof.Output())
	default:
		r.Fail("lsof %s: %v", file, lsof.Err)
	}
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
