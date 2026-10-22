package homebrew

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fakeBrew = `#!/bin/sh
state="$DISKCLEANER_TEST_BREW_STATE"
echo "$@" >>"$state/calls"
[ -e "$state/hung" ] && exec sleep 30
if [ -e "$state/fails" ]; then
	echo "Error: Permission denied @ apply2files" >&2
	exit 1
fi
[ -e "$state/nothing" ] && exit 0
case "$*" in
*--dry-run*) echo "==> This operation would free approximately $(cat "$state/size") of disk space." ;;
*) echo "==> This operation has freed approximately $(cat "$state/size") of disk space." ;;
esac
`

type fixture struct {
	state string
	step  Step
}

func newFixture(t *testing.T, size string) fixture {
	root := t.TempDir()
	f := fixture{state: filepath.Join(root, "state")}
	require.NoError(t, os.MkdirAll(f.state, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.state, "size"), []byte(size), 0o644))
	brew := filepath.Join(root, "brew")
	require.NoError(t, os.WriteFile(brew, []byte(fakeBrew), 0o755))
	t.Setenv("DISKCLEANER_TEST_BREW_STATE", f.state)
	f.step = Step{
		Brew:           brew,
		Processes:      "brew-running",
		TimeoutSeconds: 5,
		Running:        func(string) (bool, error) { return false, nil },
	}
	return f
}

func (f fixture) mark(t *testing.T, name string) {
	require.NoError(t, os.WriteFile(filepath.Join(f.state, name), nil, 0o644))
}

func (f fixture) calls(t *testing.T) string {
	calls, err := os.ReadFile(filepath.Join(f.state, "calls"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)
	return string(calls)
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestCleansAndCountsWhatBrewFreed(t *testing.T) {
	f := newFixture(t, "1.1GB")

	r, stdout, _ := run(f.step, false)

	assert.Equal(t, "cleanup --prune=all\n", f.calls(t))
	assert.Equal(t, 1, r.Count)
	assert.Equal(t, int64(1153433), r.KB)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stdout, "RUN           brew cleanup --prune=all  (1.1GB)")
}

func TestDryRunAsksBrewWhatItWouldFree(t *testing.T) {
	f := newFixture(t, "994.2MB")

	r, stdout, _ := run(f.step, true)

	assert.Equal(t, "cleanup --prune=all --dry-run\n", f.calls(t))
	assert.Equal(t, 1, r.Count)
	assert.Equal(t, int64(1018060), r.KB)
	assert.Contains(t, stdout, "WOULD RUN     brew cleanup --prune=all  (994.2MB)")
}

func TestNothingToFreeCountsNothing(t *testing.T) {
	f := newFixture(t, "")
	f.mark(t, "nothing")

	r, stdout, _ := run(f.step, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, r.Failures)
	assert.NotContains(t, stdout, "RUN")
}

func TestUnreadableSizeFails(t *testing.T) {
	f := newFixture(t, "lots")

	r, _, stderr := run(f.step, false)

	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, stderr, "cannot read the size")
}

func TestFailedCleanupIsAFailure(t *testing.T) {
	f := newFixture(t, "1GB")
	f.mark(t, "fails")

	r, _, stderr := run(f.step, false)

	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, stderr, "Permission denied")
}

func TestHungCleanupIsKilled(t *testing.T) {
	f := newFixture(t, "1GB")
	f.mark(t, "hung")
	f.step.TimeoutSeconds = 1

	r, _, stderr := run(f.step, false)

	require.Len(t, r.Failures, 1)
	assert.Contains(t, stderr, "did not finish within 1s")
}

func TestSkipsWhileBrewRuns(t *testing.T) {
	f := newFixture(t, "1GB")
	f.step.Running = func(pattern string) (bool, error) { return pattern == "brew-running", nil }

	r, _, stderr := run(f.step, false)

	assert.Empty(t, f.calls(t))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       homebrew: a brew command is running")
}

func TestQuietWithoutBrew(t *testing.T) {
	f := newFixture(t, "1GB")
	f.step.Brew = "diskcleaner-test-no-such-brew"

	r, stdout, stderr := run(f.step, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, strings.TrimSpace(stdout+stderr))
}
