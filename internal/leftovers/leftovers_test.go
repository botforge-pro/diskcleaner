package leftovers

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 26, 17, 0, 0, 0, time.UTC)

func newTemp(t *testing.T) string {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func makeDir(t *testing.T, temp, name string, age time.Duration) string {
	path := filepath.Join(temp, name)
	require.NoError(t, os.MkdirAll(filepath.Join(path, "b001"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "b001", "a.out"), make([]byte, 8192), 0o644))
	require.NoError(t, os.Chtimes(path, now.Add(-age), now.Add(-age)))
	return path
}

func step(temp string, running func(string) (bool, error)) Step {
	return Step{
		Dir:     temp,
		Minutes: 60,
		Kinds: []Kind{
			{Patterns: []string{"go-build*", "go-link-*"}, Processes: "go", Busy: "a go command is running"},
			{Patterns: []string{"XcodeDistPipeline.*"}, Processes: "xcode", Busy: "Xcode or an Xcode build is running"},
		},
		Now:     func() time.Time { return now },
		Running: running,
	}
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func idle(string) (bool, error) { return false, nil }

func TestRemovesOldFoldersOfEveryKind(t *testing.T) {
	temp := newTemp(t)
	build := makeDir(t, temp, "go-build2557847130", 2*time.Hour)
	link := makeDir(t, temp, "go-link-962278169", 3*24*time.Hour)
	export := makeDir(t, temp, "XcodeDistPipeline.~~~NmSxWB", 2*time.Hour)
	other := makeDir(t, temp, "TemporaryDirectory.abc", 30*24*time.Hour)

	r, stdout, stderr := run(step(temp, idle), false)

	assert.Empty(t, stderr)
	assert.NoDirExists(t, build)
	assert.NoDirExists(t, link)
	assert.NoDirExists(t, export)
	assert.DirExists(t, other)
	assert.Equal(t, 3, r.Count)
	assert.Positive(t, r.KB)
	assert.Contains(t, stdout, "TEMP     "+temp+"\n")
}

func TestDryRunRemovesNothing(t *testing.T) {
	temp := newTemp(t)
	build := makeDir(t, temp, "go-build1", 2*time.Hour)

	r, stdout, _ := run(step(temp, idle), true)

	assert.DirExists(t, build)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "WOULD REMOVE  ")
}

func TestKeepsAFolderWrittenWithinTheLimit(t *testing.T) {
	temp := newTemp(t)
	fresh := makeDir(t, temp, "go-build1", 59*time.Minute)

	r, _, stderr := run(step(temp, idle), false)

	assert.DirExists(t, fresh)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+fresh+": written within 60 minutes")
}

func TestSkipsOnlyTheKindWhoseToolRuns(t *testing.T) {
	temp := newTemp(t)
	build := makeDir(t, temp, "go-build1", 2*time.Hour)
	export := makeDir(t, temp, "XcodeDistPipeline.~~~abc", 2*time.Hour)
	goRuns := func(pattern string) (bool, error) { return pattern == "go", nil }

	r, _, stderr := run(step(temp, goRuns), false)

	assert.DirExists(t, build)
	assert.NoDirExists(t, export)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+filepath.Join(temp, "go-build*")+": a go command is running")
	assert.Contains(t, stderr, "SKIPPED       "+filepath.Join(temp, "go-link-*")+": a go command is running")
}

func TestProcessCheckErrorKeepsTheKindAndFails(t *testing.T) {
	temp := newTemp(t)
	build := makeDir(t, temp, "go-build1", 2*time.Hour)
	broken := func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := run(step(temp, broken), false)

	assert.DirExists(t, build)
	assert.Len(t, r.Failures, 1)
}

func TestDoesNotAskAboutProcessesWhenThereIsNothingToRemove(t *testing.T) {
	temp := newTemp(t)
	asked := false
	running := func(string) (bool, error) { asked = true; return false, nil }

	r, _, stderr := run(step(temp, running), false)

	assert.False(t, asked)
	assert.Zero(t, r.Count)
	assert.Empty(t, stderr)
}

func TestKeepsFilesAndSymlinksWithMatchingNames(t *testing.T) {
	temp := newTemp(t)
	target := makeDir(t, newTemp(t), "work", 2*time.Hour)
	require.NoError(t, os.WriteFile(filepath.Join(temp, "go-build.log"), []byte("x"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(temp, "go-build-link")))

	r, _, _ := run(step(temp, idle), false)

	assert.FileExists(t, filepath.Join(temp, "go-build.log"))
	assert.DirExists(t, target)
	_, err := os.Lstat(filepath.Join(temp, "go-build-link"))
	assert.NoError(t, err)
	assert.Zero(t, r.Count)
}

func TestMissingFolderIsQuiet(t *testing.T) {
	r, stdout, stderr := run(step(filepath.Join(t.TempDir(), "absent"), idle), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
