package clones

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 26, 17, 0, 0, 0, time.UTC)

const executable = "Google Chrome.app.bundle/Contents/MacOS/Google Chrome"

func newDir(t *testing.T) string {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func makeClone(t *testing.T, dir, name string, age time.Duration) string {
	clone := filepath.Join(dir, "com.google.Chrome.code_sign_clone", name)
	for _, file := range []string{
		executable,
		"Google Chrome.app.bundle/Contents/Frameworks/Google Chrome Framework.framework/Versions/154.0.8037.93/Google Chrome Framework",
		"Google Chrome.app.bundle/Contents/Resources/app.icns",
	} {
		path := filepath.Join(clone, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, make([]byte, 8192), 0o755))
	}
	require.NoError(t, os.Chtimes(clone, now.Add(-age), now.Add(-age)))
	return clone
}

func fakeLsof(t *testing.T, body string) string {
	path := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return path
}

func step(dir, lsof string) Step {
	return Step{Dir: dir, Minutes: 60, Now: func() time.Time { return now }, Lsof: lsof, LsofTimeoutSeconds: 5}
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestRemovesACloneNoProcessHasOpen(t *testing.T) {
	dir := newDir(t)
	clone := makeClone(t, dir, "code_sign_clone.3qjzL5", 9*24*time.Hour)

	r, stdout, stderr := run(step(dir, "/usr/bin/false"), false)

	assert.Empty(t, stderr)
	assert.NoDirExists(t, clone)
	assert.Equal(t, 1, r.Count)
	assert.Positive(t, r.KB)
	assert.Contains(t, stdout, "CLONES   "+dir+"\n")
}

func TestKeepsTheCloneARunningAppWasStartedFrom(t *testing.T) {
	dir := newDir(t)
	used := makeClone(t, dir, "code_sign_clone.57JIhs", 23*24*time.Hour)
	dead := makeClone(t, dir, "code_sign_clone.QnXXha", 18*24*time.Hour)
	running := filepath.Join(used, executable)
	lsof := fakeLsof(t, "for file in \"$@\"; do\n"+
		"  if [ \"$file\" = '"+running+"' ]; then printf 'p1326\\nftxt\\nn%s\\n' \"$file\"; exit 1; fi\n"+
		"done\nexit 1\n")

	r, _, stderr := run(step(dir, lsof), false)

	assert.DirExists(t, used)
	assert.NoDirExists(t, dead)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+used+": a running app was started from it")
}

func TestRealLsofSeesAnOpenExecutable(t *testing.T) {
	dir := newDir(t)
	clone := makeClone(t, dir, "code_sign_clone.open", 2*time.Hour)
	file, err := os.Open(filepath.Join(clone, executable))
	require.NoError(t, err)
	defer file.Close()

	r, _, stderr := run(step(dir, "lsof"), false)

	assert.DirExists(t, clone)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+clone+": a running app was started from it")
}

func TestDryRunRemovesNothing(t *testing.T) {
	dir := newDir(t)
	clone := makeClone(t, dir, "code_sign_clone.a", 2*time.Hour)

	r, stdout, _ := run(step(dir, "/usr/bin/false"), true)

	assert.DirExists(t, clone)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "WOULD REMOVE  ")
}

func TestKeepsACloneMadeWithinTheLimit(t *testing.T) {
	dir := newDir(t)
	clone := makeClone(t, dir, "code_sign_clone.new", 10*time.Minute)

	r, _, stderr := run(step(dir, "/usr/bin/false"), false)

	assert.DirExists(t, clone)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+clone+": made within 60 minutes")
}

func TestLsofFailureKeepsTheCloneAndFails(t *testing.T) {
	dir := newDir(t)
	clone := makeClone(t, dir, "code_sign_clone.a", 2*time.Hour)
	lsof := fakeLsof(t, "echo 'lsof: broken' >&2\nexit 2\n")

	r, _, _ := run(step(dir, lsof), false)

	assert.DirExists(t, clone)
	assert.Zero(t, r.Count)
	assert.Len(t, r.Failures, 1)
}

func TestLeavesOtherFoldersAlone(t *testing.T) {
	dir := newDir(t)
	other := filepath.Join(dir, "com.example.cache", "code_sign_clone.a")
	require.NoError(t, os.MkdirAll(other, 0o755))
	require.NoError(t, os.Chtimes(other, now.Add(-48*time.Hour), now.Add(-48*time.Hour)))

	r, stdout, stderr := run(step(dir, "/usr/bin/false"), false)

	assert.DirExists(t, other)
	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestMissingFolderIsQuiet(t *testing.T) {
	r, stdout, stderr := run(step(filepath.Join(t.TempDir(), "absent"), "/usr/bin/false"), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
