package candidates

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

type Tracking int

const (
	Untracked Tracking = iota
	Tracked
	Unknown
)

func GitTracking(r *report.Report, path string) Tracking {
	absolute, err := filepath.Abs(path)
	if err != nil {
		r.Fail("%s: %v", path, err)
		return Unknown
	}
	path = absolute
	if strings.Contains(path, string(filepath.Separator)+".git"+string(filepath.Separator)) {
		return Untracked
	}
	dir := filepath.Dir(path)
	workTree, stderr, err := git(dir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		if !hasGitAncestor(dir) && strings.Contains(stderr, "not a git repository (or any") {
			return Untracked
		}
		r.Fail("git rev-parse in %s: %s", dir, firstLines(stderr, err))
		return Unknown
	}
	if workTree != "true" {
		gitDir, _, err := git(dir, "rev-parse", "--is-inside-git-dir")
		if err == nil && gitDir == "true" {
			return Untracked
		}
		r.Fail("git in %s: neither a work tree nor inside a git directory, such as a bare repository", dir)
		return Unknown
	}
	_, stderr, err = git(dir, "ls-files", "--error-unmatch", "--", path)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return Tracked
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return Untracked
	}
	r.Fail("git ls-files %s: %s", path, firstLines(stderr, err))
	return Unknown
}

func git(dir string, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(), "LC_ALL=C")
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return strings.TrimSpace(stdout.String()), stderr.String(), err
}

func hasGitAncestor(dir string) bool {
	for dir != "/" && dir != "." && dir != "" {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		dir = filepath.Dir(dir)
	}
	return false
}

func firstLines(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if len(lines) > 3 {
		lines = lines[:3]
	}
	text := strings.Join(lines, "; ")
	if text == "" {
		return err.Error()
	}
	return fmt.Sprintf("%v: %s", err, text)
}
