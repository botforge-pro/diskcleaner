package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/lock"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStep struct {
	fail bool
}

func (f fakeStep) Run(r *report.Report) {
	fmt.Fprintln(r.Out, "FAKE")
	r.Count += 2
	r.KB += 3072
	if f.fail {
		r.Fail("rm /tmp/x: permission denied\nsecond line")
	}
}

func findFake(fail bool, names ...string) registry {
	if len(names) == 0 {
		names = []string{"fake", "fake"}
	}
	return registry{
		names: names,
		find: func(name string) (step, error) {
			switch name {
			case "fake":
				return fakeStep{fail: fail}, nil
			case "misconfigured":
				return nil, errors.New("DISKCLEANER_FAKE must be a whole number above zero, got: 1.5")
			}
			return nil, unknownStepError{name: name}
		},
	}
}

func TestRun(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantStatus int
		wantStdout string
		wantStderr string
	}{
		{"version", []string{"version"}, 0, "(devel)\n", ""},
		{"no command", nil, 2, "", usage},
		{"unknown command", []string{"tidy"}, 2, "", usage},
		{"extra argument", []string{"version", "now"}, 2, "", usage},
		{"clean argument", []string{"clean", "now"}, 2, "", usage},
		{"unknown step", []string{"run", "nothing"}, 2, "", "unknown step \"nothing\"\n" + usage},
		{"step argument", []string{"run", "fake", "now"}, 2, "", usage},
		{"step", []string{"run", "fake", "--dry-run"}, 0, "FAKE\n", ""},
		{"misconfigured step", []string{"run", "misconfigured"}, 2, "", "DISKCLEANER_FAKE must be a whole number above zero, got: 1.5\n"},
		{"check", []string{"check", "fake", "fake"}, 0, "", ""},
		{"check without steps", []string{"check"}, 2, "", usage},
		{"check misconfigured", []string{"check", "fake", "misconfigured"}, 2, "", "DISKCLEANER_FAKE must be a whole number above zero, got: 1.5\n"},
		{"check unknown", []string{"check", "nothing"}, 2, "", "unknown step \"nothing\"\n" + usage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := run(tc.args, &stdout, &stderr, findFake(false))
			assert.Equal(t, tc.wantStatus, status)
			assert.Equal(t, tc.wantStdout, stdout.String())
			assert.Equal(t, tc.wantStderr, stderr.String())
		})
	}
}

func TestUsageListsEveryStepInCleanOrder(t *testing.T) {
	for _, name := range stepNames() {
		assert.Contains(t, usage, "\n  "+name+" ")
	}
	assert.Less(t, strings.Index(usage, "  projects "), strings.Index(usage, "  node-modules "))
}

func TestEveryStepIsFound(t *testing.T) {
	t.Setenv("DISKCLEANER_REPOS_DIR", t.TempDir())
	for _, name := range stepNames() {
		_, err := steps(name)
		assert.NoError(t, err, name)
	}
}

func TestRunWritesSummary(t *testing.T) {
	summary := filepath.Join(t.TempDir(), "summary")
	var stdout, stderr bytes.Buffer

	status := run([]string{"run", "fake", "--report", summary}, &stdout, &stderr, findFake(true))

	assert.Equal(t, 1, status)
	written, err := os.ReadFile(summary)
	require.NoError(t, err)
	assert.Equal(t, "count 2\nkb 3072\nfailure rm /tmp/x: permission denied; second line\n", string(written))
	assert.Equal(t, "FAILED        rm /tmp/x: permission denied; second line\n", stderr.String())
}

func TestRunReportsUnwritableSummary(t *testing.T) {
	summary := filepath.Join(t.TempDir(), "missing", "summary")
	var stdout, stderr bytes.Buffer

	status := run([]string{"run", "fake", "--report", summary}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 1, status)
	assert.Contains(t, stderr.String(), "FAILED        report "+summary)
}

func cleanLock(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "lock")
	t.Setenv("DISKCLEANER_LOCK", path)
	return path
}

func TestCleanDryRunAddsUpEveryStep(t *testing.T) {
	cleanLock(t)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run"}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 0, status)
	assert.Equal(t, "Mode: dry run\n\nFAKE\nFAKE\nWould remove 4 paths and free approximately 6.0 MiB.\nNothing was removed.\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestCleanSaysWhatItFreedAndWhatIsFree(t *testing.T) {
	cleanLock(t)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean"}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 0, status)
	assert.Contains(t, stdout.String(), "FAKE\nFAKE\nRemoved 4 paths; freed approximately 6.0 MiB.\nFree on the disk of ")
	assert.Empty(t, stderr.String())
}

func TestCleanRepeatsEveryFailureAtTheEnd(t *testing.T) {
	cleanLock(t)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run"}, &stdout, &stderr, findFake(true))

	assert.Equal(t, 1, status)
	failure := "FAILED        rm /tmp/x: permission denied; second line\n"
	assert.Equal(t, failure+failure+"2 failure(s):\n"+failure+failure, stderr.String())
}

func TestCleanChecksEveryStepBeforeRunningAny(t *testing.T) {
	path := cleanLock(t)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean"}, &stdout, &stderr, findFake(false, "fake", "misconfigured"))

	assert.Equal(t, 2, status)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "DISKCLEANER_FAKE must be a whole number above zero, got: 1.5\n", stderr.String())
	assert.NoFileExists(t, path)
}

func TestCleanDoesNotStartWhileAnotherRuns(t *testing.T) {
	path := cleanLock(t)
	held, err := lock.Take(path, io.Discard)
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Release() })
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean"}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 3, status)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "another cleanup is already running (lock "+path+"); not starting a second one\n", stderr.String())
}

func TestCleanReleasesTheLock(t *testing.T) {
	path := cleanLock(t)

	assert.Equal(t, 0, run([]string{"clean", "--dry-run"}, io.Discard, io.Discard, findFake(false)))

	held, err := lock.Take(path, io.Discard)
	require.NoError(t, err)
	require.NoError(t, held.Release())
}

func TestCleanCountsWhatTwoStepsBothFindOnce(t *testing.T) {
	cleanLock(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	app := filepath.Join(dir, "app")
	modules := filepath.Join(app, ".build", "pkg", "node_modules")
	require.NoError(t, os.MkdirAll(modules, 0o755))
	output, err := exec.Command("git", "init", "-q", app).CombinedOutput()
	require.NoError(t, err, string(output))
	for _, file := range []string{filepath.Join(app, "Package.swift"), filepath.Join(app, ".build", "pkg", "package-lock.json")} {
		require.NoError(t, os.WriteFile(file, nil, 0o644))
	}
	t.Setenv("DISKCLEANER_REPOS_DIR", dir)
	t.Setenv("DISKCLEANER_NODE_DIR", dir)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run"}, &stdout, &stderr, registry{names: []string{"projects", "node-modules"}, find: steps})

	assert.Equal(t, 0, status, stderr.String())
	assert.Equal(t, 1, strings.Count(stdout.String(), "WOULD REMOVE"), stdout.String())
	assert.Contains(t, stdout.String(), "Would remove 1 paths")
}

func TestCleanRefusesALockItCannotUse(t *testing.T) {
	path := cleanLock(t)
	require.NoError(t, os.Mkdir(path, 0o755))
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean"}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 2, status)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "cannot open the lock "+path)
}
