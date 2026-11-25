package candidates

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

type Tracking int

const (
	Untracked Tracking = iota
	Tracked
	Unknown
)

type Set struct {
	paths []string
}

func (s *Set) Add(r *report.Report, path string) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		r.Fail("%s: %v", path, err)
		r.Skip("%s: git cannot tell whether it holds tracked files", path)
		return
	}
	path = absolute
	switch GitTracking(r, path) {
	case Untracked:
		s.paths = append(s.paths, path)
	case Tracked:
		r.Skip("%s: contains files tracked by git", path)
	default:
		r.Skip("%s: git cannot tell whether it holds tracked files", path)
	}
}

func (s *Set) Paths() []string {
	return slices.Clone(s.paths)
}

func (s *Set) Remove(r *report.Report) {
	s.RemoveUnless(r, func(string) bool { return false })
}

func (s *Set) RemoveUnless(r *report.Report, keep func(path string) bool) {
	paths := slices.Clone(s.paths)
	s.paths = nil
	slices.Sort(paths)
	var handled []string
	for _, path := range paths {
		if slices.ContainsFunc(handled, func(parent string) bool { return within(path, parent) }) {
			continue
		}
		handled = append(handled, path)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			r.Skip("%s: no longer exists", path)
			continue
		}
		if keep(path) {
			continue
		}
		r.Remove(path)
	}
}

func within(path, parent string) bool {
	return path == parent || strings.HasPrefix(path, parent+string(filepath.Separator))
}

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
