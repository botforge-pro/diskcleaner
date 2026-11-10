package projects

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

var parenthesized = regexp.MustCompile(`\(([^)]*)\)`)

type LFS struct {
	Git            string
	GitLFS         string
	TimeoutSeconds int
}

func DefaultLFS(timeout time.Duration) LFS {
	return LFS{Git: "git", GitLFS: "git-lfs", TimeoutSeconds: int(timeout / time.Second)}
}

func (l LFS) prune(repo string, args ...string) (string, error) {
	command := append([]string{"-C", repo, "lfs", "prune"}, args...)
	result := process.RunGroup(l.TimeoutSeconds, l.Git, command...)
	output := strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
	if result.TimedOut {
		return output, fmt.Errorf("did not finish within %ds (projects.lfs-timeout)", l.TimeoutSeconds)
	}
	return output, result.Err
}

func (l LFS) Prune(r *report.Report, repo string) {
	lfs := filepath.Join(repo, ".git", "lfs")
	if !isDir(lfs) {
		return
	}
	if _, err := exec.LookPath(l.GitLFS); err != nil {
		r.Fail("%s: git-lfs is not installed", lfs)
		return
	}
	output, err := l.prune(repo, "--dry-run", "--verify-remote")
	if err != nil {
		r.Fail("git lfs prune --dry-run in %s: %v: %s", repo, err, output)
		return
	}
	summary := ""
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", "\n"), "\n") {
		if strings.Contains(line, "would be pruned") {
			summary = strings.TrimSpace(line)
		}
	}
	if summary == "" || strings.HasPrefix(summary, "0 ") {
		return
	}
	if r.DryRun {
		match := parenthesized.FindStringSubmatch(summary)
		kb, ok := int64(0), false
		if match != nil {
			kb, ok = report.SizeKB(match[1])
		}
		if !ok {
			r.Fail("git lfs prune --dry-run in %s: cannot read the size from: %s", repo, summary)
			return
		}
		fmt.Fprintf(r.Out, "WOULD PRUNE   %s: %s\n", lfs, summary)
		r.Count++
		r.KB += kb
		return
	}
	beforeKB, err := report.DiskUsageKB(lfs)
	if err != nil {
		r.Fail("du %s: %v", lfs, err)
	}
	fmt.Fprintf(r.Out, "LFS PRUNE     %s: %s\n", lfs, summary)
	output, err = l.prune(repo, "--verify-remote")
	if err != nil {
		r.Fail("git lfs prune in %s: %v: %s", repo, err, output)
	} else {
		r.Count++
	}
	afterKB, err := report.DiskUsageKB(lfs)
	if err != nil {
		r.Fail("du %s after git lfs prune, its freed space is not counted: %v", lfs, err)
		return
	}
	if afterKB < beforeKB {
		r.KB += beforeKB - afterKB
	}
}
