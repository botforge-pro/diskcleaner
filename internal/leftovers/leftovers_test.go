package leftovers

import (
	"bytes"
	"io/fs"
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
	for _, file := range []string{"b001/a.out", "b001/importcfg", "b002/_pkg_.a"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(path, file)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(path, file), make([]byte, 8192), 0o644))
	}
	backdate(t, path, age)
	return path
}

func backdate(t *testing.T, root string, age time.Duration) {
	require.NoError(t, filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		require.NoError(t, err)
		return os.Chtimes(path, now.Add(-age), now.Add(-age))
	}))
}

func step(temp, lsof string) Step {
	return Step{
		Dir:                temp,
		Patterns:           patterns,
		Minutes:            60,
		Now:                func() time.Time { return now },
		Lsof:               lsof,
		LsofTimeoutSeconds: 5,
		Batch:              lsofBatch,
	}
}

func fakeLsof(t *testing.T, body string) string {
	path := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return path
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

const noneOpen = "/usr/bin/false"

func TestRemovesOldFoldersOfEveryKind(t *testing.T) {
	temp := newTemp(t)
	build := makeDir(t, temp, "go-build2557847130", 2*time.Hour)
	link := makeDir(t, temp, "go-link-962278169", 3*24*time.Hour)
	export := makeDir(t, temp, "XcodeDistPipeline.~~~NmSxWB", 2*time.Hour)
	other := makeDir(t, temp, "TemporaryDirectory.abc", 30*24*time.Hour)

	r, stdout, stderr := run(step(temp, noneOpen), false)

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

	r, stdout, _ := run(step(temp, noneOpen), true)

	assert.DirExists(t, build)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "WOULD REMOVE  ")
}

func TestKeepsAFolderWrittenWithinTheLimit(t *testing.T) {
	temp := newTemp(t)
	fresh := makeDir(t, temp, "go-build1", 59*time.Minute)

	r, _, stderr := run(step(temp, noneOpen), false)

	assert.DirExists(t, fresh)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+fresh+": written within 60 minutes")
}

func TestAFreshFileDeepInsideKeepsAnOldFolder(t *testing.T) {
	temp := newTemp(t)
	build := makeDir(t, temp, "go-build1", 3*time.Hour)
	inner := filepath.Join(build, "b002", "_pkg_.a")
	require.NoError(t, os.Chtimes(inner, now.Add(-time.Minute), now.Add(-time.Minute)))

	r, _, stderr := run(step(temp, noneOpen), false)

	assert.FileExists(t, inner)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+build+": written within 60 minutes")
}

func TestKeepsAFolderAProcessHasAFileOpenIn(t *testing.T) {
	temp := newTemp(t)
	build := makeDir(t, temp, "go-build1", 2*time.Hour)
	export := makeDir(t, temp, "XcodeDistPipeline.~~~abc", 2*time.Hour)
	file, err := os.Open(filepath.Join(build, "b002", "_pkg_.a"))
	require.NoError(t, err)
	defer file.Close()
	s := step(temp, "lsof")
	s.Batch = 2

	r, _, stderr := run(s, false)

	assert.DirExists(t, build)
	assert.NoDirExists(t, export)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+build+": a process still has something in it open")
}

func TestLsofFailureKeepsTheFolderAndFails(t *testing.T) {
	temp := newTemp(t)
	build := makeDir(t, temp, "go-build1", 2*time.Hour)
	lsof := fakeLsof(t, "echo 'lsof: broken' >&2\nexit 2\n")

	r, _, _ := run(step(temp, lsof), false)

	assert.DirExists(t, build)
	assert.Zero(t, r.Count)
	assert.Len(t, r.Failures, 1)
}

func TestKeepsFilesAndSymlinksWithMatchingNames(t *testing.T) {
	temp := newTemp(t)
	target := makeDir(t, newTemp(t), "work", 2*time.Hour)
	require.NoError(t, os.WriteFile(filepath.Join(temp, "go-build.log"), []byte("x"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(temp, "go-build-link")))

	r, _, _ := run(step(temp, noneOpen), false)

	assert.FileExists(t, filepath.Join(temp, "go-build.log"))
	assert.DirExists(t, target)
	_, err := os.Lstat(filepath.Join(temp, "go-build-link"))
	assert.NoError(t, err)
	assert.Zero(t, r.Count)
}

func TestNothingToLookAtIsQuiet(t *testing.T) {
	for name, dir := range map[string]string{
		"missing folder": filepath.Join(t.TempDir(), "absent"),
		"no leftovers":   newTemp(t),
	} {
		t.Run(name, func(t *testing.T) {
			r, stdout, stderr := run(step(dir, noneOpen), false)

			assert.Zero(t, r.Count)
			assert.Empty(t, stdout)
			assert.Empty(t, stderr)
		})
	}
}
