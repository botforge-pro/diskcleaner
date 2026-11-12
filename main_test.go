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
	"time"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/lock"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStep struct {
	fail   bool
	minAge time.Duration
}

func (f fakeStep) Run(r *report.Report) {
	if f.minAge == time.Hour {
		fmt.Fprintln(r.Out, "FAKE")
	} else {
		fmt.Fprintf(r.Out, "FAKE min-age %s\n", f.minAge)
	}
	r.Count += 2
	r.KB += 3072
	if f.fail {
		r.Fail("rm /tmp/x: permission denied\nsecond line")
	}
}

var fakeTable = config.NewTable([]config.Step{
	{Name: "fake", Reversible: true}, {Name: "misconfigured", Reversible: true}, {Name: "kept"},
})

func findFake(fail bool, names ...string) registry {
	if len(names) == 0 {
		names = []string{"fake", "fake"}
	}
	return registry{
		names: names,
		table: fakeTable,
		find: func(name string, c *config.Config) (step, error) {
			switch name {
			case "fake", "kept":
				return fakeStep{fail: fail, minAge: c.Duration("min-age")}, nil
			case "misconfigured":
				return nil, errors.New("misconfigured: no tool to run")
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
		{"wrong setting", []string{"clean", "--min-age=soon"}, 2, "", "min-age: want a duration like 1h or 15d, got \"soon\"\n"},
		{"run is gone", []string{"run", "fake"}, 2, "", usage},
		{"check is gone", []string{"check", "fake"}, 2, "", usage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanLock(t)
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
	assert.Less(t, strings.Index(usage, "  projects "), strings.Index(usage, "  claude-scratchpads "))
}

func TestEveryStepIsFound(t *testing.T) {
	set := pflag.NewFlagSet("clean", pflag.ContinueOnError)
	settingsTable.Flags(set)
	c, err := config.Load(settingsTable, set, t.TempDir())
	require.NoError(t, err)
	for _, name := range stepNames() {
		_, err := steps(name, c)
		assert.NoError(t, err, name)
	}
}

func TestNoStepErasesSimulators(t *testing.T) {
	set := pflag.NewFlagSet("clean", pflag.ContinueOnError)
	settingsTable.Flags(set)
	c, err := config.Load(settingsTable, set, t.TempDir())
	require.NoError(t, err)

	_, err = steps("simulator-data", c)

	assert.ErrorAs(t, err, &unknownStepError{})
}

func TestCleanWritesSummary(t *testing.T) {
	cleanLock(t)
	summary := filepath.Join(t.TempDir(), "summary")
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run", "--only=fake", "--report", summary}, &stdout, &stderr, findFake(true, "fake"))

	assert.Equal(t, 1, status)
	written, err := os.ReadFile(summary)
	require.NoError(t, err)
	assert.Equal(t, "count 2\nkb 3072\nfailure rm /tmp/x: permission denied; second line\n", string(written))
}

func TestCleanReportsUnwritableSummary(t *testing.T) {
	cleanLock(t)
	summary := filepath.Join(t.TempDir(), "missing", "summary")
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run", "--report", summary}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 1, status)
	assert.Contains(t, stderr.String(), "FAILED        report "+summary)
}
func TestMain(m *testing.M) {
	config, err := os.MkdirTemp("", "diskcleaner-config")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	status := m.Run()
	os.RemoveAll(config)
	os.Exit(status)
}

func cleanLock(t *testing.T) string {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	require.NoError(t, os.MkdirAll(filepath.Join(state, "diskcleaner"), 0o755))
	return filepath.Join(state, "diskcleaner", "lock")
}

func TestCleanDryRunAddsUpEveryStep(t *testing.T) {
	cleanLock(t)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run"}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 0, status)
	assert.Equal(t, "Mode: dry run\n\nFAKE\nFAKE\nWould remove 4 paths and free approximately 6.0 MiB.\nNothing was removed.\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestCleanPassesSettingsToEveryStep(t *testing.T) {
	cleanLock(t)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run", "--min-age=0"}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 0, status)
	assert.Contains(t, stdout.String(), "FAKE min-age 0s\nFAKE min-age 0s\n")
	assert.Empty(t, stderr.String())
}

func TestCleanOnlyAndSkipChooseAmongTheSteps(t *testing.T) {
	cleanLock(t)
	for args, want := range map[string]int{
		"--only=fake":                           1,
		"--skip=fake":                           1,
		"--skip=fake,kept":                      0,
		"--only=fake,kept":                      2,
		"--only=fake,kept --kept.enabled=false": 1,
		"--only=kept":                           1,
	} {
		var stdout, stderr bytes.Buffer

		status := run(append([]string{"clean", "--dry-run"}, strings.Fields(args)...), &stdout, &stderr, findFake(false, "fake", "kept"))

		assert.Equal(t, 0, status, args+": "+stderr.String())
		assert.Equal(t, want, strings.Count(stdout.String(), "FAKE\n"), args)
	}
}

func TestCleanRefusesAnUnknownStepInOnly(t *testing.T) {
	cleanLock(t)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--only=nothing"}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 2, status)
	assert.Equal(t, "unknown step \"nothing\"\n"+usage, stderr.String())
}

func TestCleanExitsWithFourWhenLessThanMinFreeIsFree(t *testing.T) {
	cleanLock(t)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--min-free=1000000GB"}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 4, status, stderr.String())
	assert.Contains(t, stderr.String(), "Less than min-free")
}

func TestCleanLeavesAStepThatIsOffAndSaysHowToTurnItOn(t *testing.T) {
	cleanLock(t)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run", "--kept.enabled=false"}, &stdout, &stderr, findFake(false, "fake", "kept"))

	assert.Equal(t, 0, status, stderr.String())
	assert.Equal(t, 1, strings.Count(stdout.String(), "FAKE\n"))
	assert.Contains(t, stdout.String(), "OFF           kept: would free about 3.0 MiB; turn on with --kept.enabled\n")

	stdout.Reset()
	status = run([]string{"clean", "--dry-run", "--kept.enabled"}, &stdout, &stderr, findFake(false, "fake", "kept"))

	assert.Equal(t, 0, status, stderr.String())
	assert.Equal(t, 2, strings.Count(stdout.String(), "FAKE\n"))
	assert.NotContains(t, stdout.String(), "OFF")
}

func TestOnlyADryRunEstimatesAStepThatIsOff(t *testing.T) {
	cleanLock(t)
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--kept.enabled=false"}, &stdout, &stderr, findFake(false, "fake", "kept"))

	assert.Equal(t, 0, status, stderr.String())
	assert.Equal(t, 1, strings.Count(stdout.String(), "FAKE\n"))
	assert.Contains(t, stdout.String(), "OFF           kept: turn on with --kept.enabled\n")
}

func TestAnEstimateThatFailsSaysSo(t *testing.T) {
	cleanLock(t)
	var stdout, stderr bytes.Buffer

	run([]string{"clean", "--dry-run", "--kept.enabled=false"}, &stdout, &stderr, findFake(true, "fake", "kept"))

	assert.Contains(t, stdout.String(), "OFF           kept: cannot estimate (")
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
	assert.Equal(t, "misconfigured: no tool to run\n", stderr.String())
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
	assert.True(t, strings.HasPrefix(stderr.String(), fmt.Sprintf("another cleanup is already running (pid %d since ", os.Getpid())), stderr.String())
	assert.True(t, strings.HasSuffix(stderr.String(), ", lock "+path+"); not starting a second one\n"), stderr.String())
}

func TestCleanReleasesTheLock(t *testing.T) {
	path := cleanLock(t)

	assert.Equal(t, 0, run([]string{"clean", "--dry-run"}, io.Discard, io.Discard, findFake(false)))

	held, err := lock.Take(path, io.Discard)
	require.NoError(t, err)
	require.NoError(t, held.Release())
}

func TestCleanCountsNodeModulesInsideBuildOutputOnce(t *testing.T) {
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
	old := time.Now().Add(-2 * time.Hour)
	for _, path := range []string{modules, filepath.Join(app, ".build", "pkg", "package-lock.json"), filepath.Dir(modules), filepath.Join(app, ".build")} {
		require.NoError(t, os.Chtimes(path, old, old))
	}
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run", "--project-dirs=" + dir}, &stdout, &stderr, registry{names: []string{"projects"}, table: settingsTable, find: steps})

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
