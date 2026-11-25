package rule

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/age"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

func writeLog(t *testing.T, path string, old time.Duration) {
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
	require.NoError(t, os.Chtimes(path, now.Add(-old), now.Add(-old)))
}

func newLogs(t *testing.T) string {
	folder, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	writeLog(t, filepath.Join(folder, "-dev-app", "old.jsonl"), 40*day)
	writeLog(t, filepath.Join(folder, "-dev-app", "session", "subagents", "agent-old.jsonl"), 40*day)
	writeLog(t, filepath.Join(folder, "-dev-app", "recent.jsonl"), 2*day)
	writeLog(t, filepath.Join(folder, "-dev-app", "thirty.jsonl"), 30*day)
	writeLog(t, filepath.Join(folder, "-dev-app", "edge.jsonl"), 31*day)
	writeLog(t, filepath.Join(folder, "-dev-app", "thirty-one.jsonl"), 31*day+time.Second)
	writeLog(t, filepath.Join(folder, "-dev-app", "old.txt"), 40*day)
	return folder
}

func fakeLsof(t *testing.T, body string) string {
	path := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return path
}

func logs(folder string) Rule {
	return Rule{
		Label: "CLAUDE",
		Root:  folder,
		Files: []string{"**/*.jsonl"},
		Keep:  Keep{Age: age.Limit{Min: 31 * day, Now: func() time.Time { return now }}},
		Lsof:  inuse.Lsof{Command: "../process/testdata/lsof-none-open", TimeoutSeconds: 5},
	}
}

func TestFilesOlderThanTheAgeGoAndTheRestStay(t *testing.T) {
	folder := newLogs(t)

	r, stdout, stderr := run(logs(folder), false)

	assert.Empty(t, stderr)
	assert.NoFileExists(t, filepath.Join(folder, "-dev-app", "old.jsonl"))
	assert.NoFileExists(t, filepath.Join(folder, "-dev-app", "session", "subagents", "agent-old.jsonl"))
	assert.FileExists(t, filepath.Join(folder, "-dev-app", "recent.jsonl"))
	assert.FileExists(t, filepath.Join(folder, "-dev-app", "thirty.jsonl"))
	assert.FileExists(t, filepath.Join(folder, "-dev-app", "edge.jsonl"), "exactly the age is not older than it")
	assert.NoFileExists(t, filepath.Join(folder, "-dev-app", "thirty-one.jsonl"))
	assert.FileExists(t, filepath.Join(folder, "-dev-app", "old.txt"), "the pattern names only .jsonl")
	assert.Equal(t, 3, r.Count)
	assert.Contains(t, stdout, "CLAUDE   "+folder+"\n")
}

func TestAPatternWithoutStarsMatchesFromTheRoot(t *testing.T) {
	folder := newLogs(t)
	s := logs(folder)
	s.Files = []string{"-dev-app/*.jsonl"}

	r, _, _ := run(s, false)

	assert.FileExists(t, filepath.Join(folder, "-dev-app", "session", "subagents", "agent-old.jsonl"))
	assert.Equal(t, 2, r.Count)
}

func TestAFileOpenInAProcessStays(t *testing.T) {
	folder := newLogs(t)
	open := filepath.Join(folder, "-dev-app", "old.jsonl")
	s := logs(folder)
	s.Lsof.Command = fakeLsof(t, "printf 'p42\\nf3\\nn"+open+"\\n'\nexit 0\n")

	r, _, stderr := run(s, false)

	assert.FileExists(t, open)
	assert.Equal(t, 2, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+open+": a process still has it open\n")
}

func TestAnOpenFileStaysWhenTheRootIsReachedThroughALink(t *testing.T) {
	folder := newLogs(t)
	link := filepath.Join(t.TempDir(), "projects")
	require.NoError(t, os.Symlink(folder, link))
	open := filepath.Join(folder, "-dev-app", "old.jsonl")
	file, err := os.Open(open)
	require.NoError(t, err)
	defer file.Close()
	s := logs(link)
	s.Lsof.Command = "lsof"

	_, _, stderr := run(s, false)

	assert.FileExists(t, open, "lsof prints the resolved path, so the walk must use it too")
	assert.Contains(t, stderr, "SKIPPED       "+open+": a process still has it open\n")
}

func TestAFileWrittenAgainAfterTheScanStays(t *testing.T) {
	folder := newLogs(t)
	s := logs(folder)
	s.Lsof.Command = fakeLsof(t, "find '"+folder+"' -name '*.jsonl' -exec touch {} +\nprintf 'n/\\n'\n")
	s.Keep.Age.Now = time.Now
	for _, name := range []string{"old.jsonl", "thirty-one.jsonl"} {
		path := filepath.Join(folder, "-dev-app", name)
		require.NoError(t, os.Chtimes(path, time.Now().Add(-40*day), time.Now().Add(-40*day)))
	}

	r, _, stderr := run(s, false)

	assert.FileExists(t, filepath.Join(folder, "-dev-app", "old.jsonl"))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+filepath.Join(folder, "-dev-app", "old.jsonl")+": written again since the scan\n")
}

func TestOpenFilesAreAskedAboutWithAFreshListingForEachBatch(t *testing.T) {
	folder, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	var paths []string
	for i := range openAnswersPerListing + 1 {
		path := filepath.Join(folder, fmt.Sprintf("log-%02d.jsonl", i))
		writeLog(t, path, 40*day)
		paths = append(paths, path)
	}
	last := paths[openAnswersPerListing]
	calls := filepath.Join(t.TempDir(), "calls")
	s := logs(folder)
	s.Lsof.Command = fakeLsof(t, "echo listed >>"+calls+"\nprintf 'p42\\nf3\\nn"+last+"\\n'\n")

	r, _, stderr := run(s, false)

	assert.Equal(t, openAnswersPerListing, r.Count)
	assert.FileExists(t, last, "open in the second batch")
	assert.Contains(t, stderr, "SKIPPED       "+last+": a process still has it open\n")
	recorded, err := os.ReadFile(calls)
	require.NoError(t, err)
	assert.Equal(t, "listed\nlisted\n", string(recorded))
}

func TestAFailedOpenCheckRemovesNothing(t *testing.T) {
	folder := newLogs(t)
	s := logs(folder)
	s.Lsof.Command = fakeLsof(t, "echo 'lsof: status error' >&2\nexit 1\n")

	r, _, _ := run(s, false)

	assert.FileExists(t, filepath.Join(folder, "-dev-app", "old.jsonl"))
	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "lsof "+folder+": exit 1: lsof: status error")
}

func TestNoAgeRemovesEvenAFileStampedInTheFuture(t *testing.T) {
	folder, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	future := filepath.Join(folder, "future.jsonl")
	writeLog(t, future, -time.Hour)
	s := logs(folder)
	s.Keep.Age.Min = 0

	r, _, _ := run(s, false)

	assert.NoFileExists(t, future)
	assert.Equal(t, 1, r.Count)
}

func TestARelativeRootKeepsAFileGitTracks(t *testing.T) {
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

	r, _, stderr := run(logs("logs"), false)

	assert.FileExists(t, tracked)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "contains files tracked by git")
}

func TestFilesDryRunRemovesNothing(t *testing.T) {
	folder := newLogs(t)

	r, stdout, _ := run(logs(folder), true)

	assert.FileExists(t, filepath.Join(folder, "-dev-app", "old.jsonl"))
	assert.Equal(t, 3, r.Count)
	assert.Contains(t, stdout, "WOULD REMOVE")
}

func TestAnUnreadableFolderUnderTheRootFails(t *testing.T) {
	folder := newLogs(t)
	locked := filepath.Join(folder, "-locked")
	writeLog(t, filepath.Join(locked, "old.jsonl"), 40*day)
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	r, _, _ := run(logs(folder), false)

	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "scan "+folder)
	assert.Equal(t, 3, r.Count)
}

func TestFilesUnderAMissingRootAreQuiet(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr}

	logs(filepath.Join(t.TempDir(), "absent")).Run(r)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout.String())
	assert.Empty(t, stderr.String())
}
