package sessionlogs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

func writeLog(t *testing.T, path string, age time.Duration) {
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
	require.NoError(t, os.Chtimes(path, now.Add(-age), now.Add(-age)))
}

func newFolder(t *testing.T) string {
	folder, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	writeLog(t, filepath.Join(folder, "-dev-app", "old.jsonl"), 40*day)
	writeLog(t, filepath.Join(folder, "-dev-app", "session", "subagents", "agent-old.jsonl"), 40*day)
	writeLog(t, filepath.Join(folder, "-dev-app", "recent.jsonl"), 2*day)
	writeLog(t, filepath.Join(folder, "-dev-app", "thirty.jsonl"), 30*day)
	writeLog(t, filepath.Join(folder, "-dev-app", "edge.jsonl"), 31*day-time.Second)
	writeLog(t, filepath.Join(folder, "-dev-app", "thirty-one.jsonl"), 31*day)
	writeLog(t, filepath.Join(folder, "-dev-app", "old.txt"), 40*day)
	return folder
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func fakeLsof(t *testing.T, body string) string {
	path := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return path
}

func step(folder string) Step {
	return Step{
		Label:              "CLAUDE",
		Folder:             folder,
		Days:               30,
		Now:                func() time.Time { return now },
		Lsof:               "../process/testdata/lsof-none-open",
		LsofTimeoutSeconds: 5,
		Batch:              50,
	}
}

func TestALogOpenInAProcessStays(t *testing.T) {
	folder := newFolder(t)
	open := filepath.Join(folder, "-dev-app", "old.jsonl")
	s := step(folder)
	s.Lsof = fakeLsof(t, "printf 'p42\\nf3\\nn"+open+"\\n'\nexit 0\n")

	r, _, stderr := run(s, false)

	assert.FileExists(t, open)
	assert.NoFileExists(t, filepath.Join(folder, "-dev-app", "thirty-one.jsonl"))
	assert.Equal(t, 2, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+open+": a process still has it open\n")
}

func TestAnOpenLogStaysWhenTheFolderIsReachedThroughALink(t *testing.T) {
	folder := newFolder(t)
	link := filepath.Join(t.TempDir(), "projects")
	require.NoError(t, os.Symlink(folder, link))
	open := filepath.Join(folder, "-dev-app", "old.jsonl")
	file, err := os.Open(open)
	require.NoError(t, err)
	defer file.Close()
	s := step(link)
	s.Lsof = "lsof"

	_, _, stderr := run(s, false)

	assert.FileExists(t, open, "lsof prints the resolved path, so the walk must use it too")
	assert.Contains(t, stderr, "SKIPPED       "+open+": a process still has it open\n")
}

func TestALogWrittenAgainAfterTheScanStays(t *testing.T) {
	folder := newFolder(t)
	s := step(folder)
	s.Lsof = fakeLsof(t, "find '"+folder+"' -name '*.jsonl' -exec touch {} +\nprintf 'n/\\n'\n")

	r, _, stderr := run(s, false)

	assert.FileExists(t, filepath.Join(folder, "-dev-app", "old.jsonl"))
	assert.FileExists(t, filepath.Join(folder, "-dev-app", "thirty-one.jsonl"))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+filepath.Join(folder, "-dev-app", "old.jsonl")+": written again since the scan\n")
}

func TestLogsAreCheckedForOpenWritersBatchByBatchRightBeforeRemoval(t *testing.T) {
	folder, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for i := range 3 {
		writeLog(t, filepath.Join(folder, fmt.Sprintf("log-%d.jsonl", i)), 40*day)
	}
	calls := filepath.Join(t.TempDir(), "calls")
	s := step(folder)
	s.Batch = 2
	s.Lsof = fakeLsof(t, "echo listed >>"+calls+"\nprintf 'n/\\n'\n")

	r, _, _ := run(s, false)

	assert.Equal(t, 3, r.Count)
	recorded, err := os.ReadFile(calls)
	require.NoError(t, err)
	assert.Equal(t, "listed\nlisted\n", string(recorded), "one fresh listing for each batch of two")
}

func TestABatchStartsAtTheLogBeingRemovedWhenEarlierOnesVanished(t *testing.T) {
	folder, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	var logs []string
	for i := range 3 {
		path := filepath.Join(folder, fmt.Sprintf("log-%d.jsonl", i))
		writeLog(t, path, 40*day)
		logs = append(logs, path)
	}
	s := step(folder)
	s.Batch = 2
	s.Lsof = fakeLsof(t, "printf 'p42\\nf3\\nn"+logs[2]+"\\n'\n")
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr}
	checks := openChecks{step: s, report: r, pending: logs}

	kept := checks.inUse(logs[2])

	assert.True(t, kept, "the log was judged by a check that never looked at it")
	assert.Contains(t, stderr.String(), "SKIPPED       "+logs[2]+": a process still has it open\n")
}

func TestAFailedOpenCheckRemovesNothing(t *testing.T) {
	folder := newFolder(t)
	s := step(folder)
	s.Lsof = fakeLsof(t, "echo 'lsof: status error' >&2\nexit 1\n")

	r, _, _ := run(s, false)

	assert.FileExists(t, filepath.Join(folder, "-dev-app", "old.jsonl"))
	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "lsof "+folder+": exit 1: lsof: status error")
}

func TestRemovesLogsOlderThanTheDays(t *testing.T) {
	folder := newFolder(t)

	r, stdout, stderr := run(step(folder), false)

	assert.Empty(t, stderr)
	assert.NoFileExists(t, filepath.Join(folder, "-dev-app", "old.jsonl"))
	assert.NoFileExists(t, filepath.Join(folder, "-dev-app", "session", "subagents", "agent-old.jsonl"))
	assert.FileExists(t, filepath.Join(folder, "-dev-app", "recent.jsonl"))
	assert.FileExists(t, filepath.Join(folder, "-dev-app", "thirty.jsonl"))
	assert.FileExists(t, filepath.Join(folder, "-dev-app", "edge.jsonl"))
	assert.NoFileExists(t, filepath.Join(folder, "-dev-app", "thirty-one.jsonl"))
	assert.FileExists(t, filepath.Join(folder, "-dev-app", "old.txt"))
	assert.Equal(t, 3, r.Count)
	assert.Contains(t, stdout, "CLAUDE   "+folder+"\n")
}

func TestTheLabelNamesTheTool(t *testing.T) {
	folder := newFolder(t)
	s := step(folder)
	s.Label = "CODEX"

	_, stdout, _ := run(s, true)

	assert.Contains(t, stdout, "CODEX    "+folder+"\n")
}

func TestARelativeFolderKeepsALogGitTracks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	tracked := filepath.Join(root, "logs", "old.jsonl")
	writeLog(t, tracked, 40*day)
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "logs/old.jsonl"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "fixture"},
	} {
		output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		require.NoError(t, err, string(output))
	}
	t.Chdir(root)

	r, _, stderr := run(step("logs"), false)

	assert.FileExists(t, tracked)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "contains files tracked by git")
}

func TestDryRunRemovesNothing(t *testing.T) {
	folder := newFolder(t)

	r, stdout, _ := run(step(folder), true)

	assert.FileExists(t, filepath.Join(folder, "-dev-app", "old.jsonl"))
	assert.Equal(t, 3, r.Count)
	assert.Contains(t, stdout, "WOULD REMOVE")
}

func TestAnUnreadableFolderFails(t *testing.T) {
	folder := newFolder(t)
	locked := filepath.Join(folder, "-locked")
	writeLog(t, filepath.Join(locked, "old.jsonl"), 40*day)
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	r, _, _ := run(step(folder), false)

	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "scan "+folder)
	assert.Equal(t, 3, r.Count)
}

func TestAMissingFolderIsQuiet(t *testing.T) {
	r, stdout, stderr := run(step(filepath.Join(t.TempDir(), "absent")), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestClaudeKeepsTwentyDaysOfProjectLogsByDefault(t *testing.T) {
	home := t.TempDir()

	s, err := Claude(home)

	require.NoError(t, err)
	assert.Equal(t, "CLAUDE", s.Label)
	assert.Equal(t, filepath.Join(home, ".claude", "projects"), s.Folder)
	assert.Equal(t, 20, s.Days)
}

func TestCodexKeepsTwentyDaysOfSessionLogsByDefault(t *testing.T) {
	home := t.TempDir()

	s, err := Codex(home)

	require.NoError(t, err)
	assert.Equal(t, "CODEX", s.Label)
	assert.Equal(t, filepath.Join(home, ".codex", "sessions"), s.Folder)
	assert.Equal(t, 20, s.Days)
}

func TestSettingsChooseTheFolderAndTheDays(t *testing.T) {
	t.Setenv("DISKCLEANER_CLAUDE_PROJECTS", "/claude")
	t.Setenv("DISKCLEANER_CLAUDE_LOG_DAYS", "7")
	t.Setenv("DISKCLEANER_CODEX_SESSIONS", "/codex")
	t.Setenv("DISKCLEANER_CODEX_LOG_DAYS", "9")

	claude, err := Claude(t.TempDir())
	require.NoError(t, err)
	codex, err := Codex(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, "/claude", claude.Folder)
	assert.Equal(t, 7, claude.Days)
	assert.Equal(t, "/codex", codex.Folder)
	assert.Equal(t, 9, codex.Days)
}

func TestAFractionalDayCountIsRejected(t *testing.T) {
	t.Setenv("DISKCLEANER_CLAUDE_LOG_DAYS", "1.5")
	t.Setenv("DISKCLEANER_CODEX_LOG_DAYS", "2.5")

	_, claudeErr := Claude(t.TempDir())
	_, codexErr := Codex(t.TempDir())

	assert.EqualError(t, claudeErr, "DISKCLEANER_CLAUDE_LOG_DAYS must be a whole number above zero, got: 1.5")
	assert.EqualError(t, codexErr, "DISKCLEANER_CODEX_LOG_DAYS must be a whole number above zero, got: 2.5")
}
