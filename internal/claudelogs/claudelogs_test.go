package claudelogs

import (
	"bytes"
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

func writeLog(t *testing.T, path string, age time.Duration) {
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
	require.NoError(t, os.Chtimes(path, now.Add(-age), now.Add(-age)))
}

func newProjects(t *testing.T) string {
	projects, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	day := 24 * time.Hour
	writeLog(t, filepath.Join(projects, "-dev-app", "old.jsonl"), 40*day)
	writeLog(t, filepath.Join(projects, "-dev-app", "session", "subagents", "agent-old.jsonl"), 40*day)
	writeLog(t, filepath.Join(projects, "-dev-app", "recent.jsonl"), 2*day)
	writeLog(t, filepath.Join(projects, "-dev-app", "thirty.jsonl"), 30*day)
	writeLog(t, filepath.Join(projects, "-dev-app", "edge.jsonl"), 31*day-time.Second)
	writeLog(t, filepath.Join(projects, "-dev-app", "thirty-one.jsonl"), 31*day)
	writeLog(t, filepath.Join(projects, "-dev-app", "old.txt"), 40*day)
	return projects
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func step(projects string) Step {
	return Step{Projects: projects, Days: 30, Now: func() time.Time { return now }}
}

func TestRemovesLogsOlderThanTheDays(t *testing.T) {
	projects := newProjects(t)

	r, stdout, stderr := run(step(projects), false)

	assert.Empty(t, stderr)
	assert.NoFileExists(t, filepath.Join(projects, "-dev-app", "old.jsonl"))
	assert.NoFileExists(t, filepath.Join(projects, "-dev-app", "session", "subagents", "agent-old.jsonl"))
	assert.FileExists(t, filepath.Join(projects, "-dev-app", "recent.jsonl"))
	assert.FileExists(t, filepath.Join(projects, "-dev-app", "thirty.jsonl"))
	assert.FileExists(t, filepath.Join(projects, "-dev-app", "edge.jsonl"))
	assert.NoFileExists(t, filepath.Join(projects, "-dev-app", "thirty-one.jsonl"))
	assert.FileExists(t, filepath.Join(projects, "-dev-app", "old.txt"))
	assert.Equal(t, 3, r.Count)
	assert.Contains(t, stdout, "CLAUDE   "+projects+"\n")
}

func TestARelativeProjectsFolderKeepsALogGitTracks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	tracked := filepath.Join(root, "logs", "old.jsonl")
	writeLog(t, tracked, 40*24*time.Hour)
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
	projects := newProjects(t)

	r, stdout, _ := run(step(projects), true)

	assert.FileExists(t, filepath.Join(projects, "-dev-app", "old.jsonl"))
	assert.Equal(t, 3, r.Count)
	assert.Contains(t, stdout, "WOULD REMOVE")
}

func TestAnUnreadableFolderFails(t *testing.T) {
	projects := newProjects(t)
	locked := filepath.Join(projects, "-locked")
	writeLog(t, filepath.Join(locked, "old.jsonl"), 40*24*time.Hour)
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	r, _, _ := run(step(projects), false)

	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "scan "+projects)
	assert.Equal(t, 3, r.Count)
}

func TestMissingProjectsAreQuiet(t *testing.T) {
	r, stdout, stderr := run(step(filepath.Join(t.TempDir(), "absent")), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestDefaultRejectsAFractionalDayCount(t *testing.T) {
	t.Setenv("DISKCLEANER_CLAUDE_LOG_DAYS", "1.5")

	_, err := Default(t.TempDir())

	assert.EqualError(t, err, "DISKCLEANER_CLAUDE_LOG_DAYS must be a whole number above zero, got: 1.5")
}
