package projects

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

var parenthesized = regexp.MustCompile(`\(([^)]*)\)`)

type LFS struct {
	Git    string
	GitLFS string
}

func DefaultLFS() LFS {
	return LFS{Git: "git", GitLFS: "git-lfs"}
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
	output, err := exec.Command(l.Git, "-C", repo, "lfs", "prune", "--dry-run", "--verify-remote").CombinedOutput()
	if err != nil {
		r.Fail("git lfs prune --dry-run in %s: %v: %s", repo, err, strings.TrimSpace(string(output)))
		return
	}
	summary := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r", "\n"), "\n") {
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
			kb, ok = report.DecimalSizeKB(match[1])
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
	output, err = exec.Command(l.Git, "-C", repo, "lfs", "prune", "--verify-remote").CombinedOutput()
	if err != nil {
		r.Fail("git lfs prune in %s: %v: %s", repo, err, strings.TrimSpace(string(output)))
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
