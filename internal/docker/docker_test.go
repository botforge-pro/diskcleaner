package docker

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fakeDocker = `#!/bin/sh
state="$DISKCLEANER_TEST_DOCKER_STATE"
case "$1" in
info)
	[ -e "$state/info-hung" ] && exec sleep 30
	if [ -e "$state/info-hung-in-child" ]; then
		sleep 30 &
		echo $! >"$state/child.pid"
		wait
	fi
	if [ -e "$state/info-child-holds-output" ]; then
		sleep 30 &
		echo $! >"$state/child.pid"
		exit "$(cat "$state/info-child-holds-output")"
	fi
	if [ -e "$state/daemon-down" ]; then
		echo "failed to connect to the docker API at unix:///var/run/docker.sock" >&2
		exit 1
	fi
	;;
system)
	[ -e "$state/df-hung" ] && exec sleep 30
	if [ -e "$state/df-leaves-detached-child" ]; then
		sleep 30 >/dev/null 2>&1 &
		echo $! >"$state/child.pid"
	fi
	printf 'Images\t%s (99%%)\nLocal Volumes\t0B (0%%)\n' "$(cat "$state/reclaimable")"
	;;
image)
	[ -e "$state/prune-hung" ] && exec sleep 30
	if [ -e "$state/prune-fails" ]; then
		echo "Error response from daemon: a prune operation is already running" >&2
		exit 1
	fi
	if [ -e "$state/prune-fails-midway" ]; then
		rm -f "$state/data/images/layer"
		echo "Error response from daemon: conflict: unable to delete image" >&2
		exit 1
	fi
	rm -rf "$state/data/images"
	echo "Deleted Images:"
	echo "Total reclaimed space: $(cat "$state/reclaimable")"
	;;
*) exit 64 ;;
esac
`

type fixture struct {
	state string
	layer string
	step  Step
}

func idle(string) (bool, error) { return false, nil }

func newFixture(t *testing.T, reclaimable string) fixture {
	root := t.TempDir()
	f := fixture{
		state: filepath.Join(root, "state"),
		layer: filepath.Join(root, "state", "data", "images", "layer"),
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(f.layer), 0o755))
	require.NoError(t, os.WriteFile(f.layer, make([]byte, 65536), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.state, "data", "Docker.raw"), make([]byte, 65536), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.state, "reclaimable"), []byte(reclaimable), 0o644))
	docker := filepath.Join(root, "docker")
	require.NoError(t, os.WriteFile(docker, []byte(fakeDocker), 0o755))
	t.Setenv("DISKCLEANER_TEST_DOCKER_STATE", f.state)
	f.step = Step{
		Docker:              docker,
		Data:                filepath.Join(f.state, "data"),
		TimeoutSeconds:      1,
		PruneTimeoutSeconds: 1,
		Backend:             "docker",
		Running:             idle,
	}
	return f
}

func (f fixture) mark(t *testing.T, name string) {
	require.NoError(t, os.WriteFile(filepath.Join(f.state, name), nil, 0o644))
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestPrunesUnusedImages(t *testing.T) {
	f := newFixture(t, "1.5GB")

	r, stdout, stderr := run(f.step, false)

	assert.Empty(t, stderr)
	assert.NoFileExists(t, f.layer)
	assert.Equal(t, 1, r.Count)
	assert.Positive(t, r.KB)
	assert.Contains(t, stdout, "DOCKER   images not used by any container\n")
	assert.Contains(t, stdout, "RUN           docker image prune --all --force  (1.5GB reclaimable)\n")
	assert.Contains(t, stdout, "Total reclaimed space: 1.5GB\n")
	assert.NotContains(t, stdout, "Deleted Images")
}

func TestDryRunPrunesNothing(t *testing.T) {
	f := newFixture(t, "1.5GB")

	r, stdout, _ := run(f.step, true)

	assert.FileExists(t, f.layer)
	assert.Equal(t, 1, r.Count)
	assert.Equal(t, int64(1464843), r.KB)
	assert.Contains(t, stdout, "WOULD RUN     docker image prune --all --force  (1.5GB reclaimable)\n")
}

func TestNothingReclaimableRunsNothing(t *testing.T) {
	f := newFixture(t, "0B")

	r, stdout, stderr := run(f.step, false)

	assert.FileExists(t, f.layer)
	assert.Zero(t, r.Count)
	assert.Empty(t, stderr)
	assert.NotContains(t, stdout, "RUN")
}

func TestSkipsWhileDockerDesktopIsOff(t *testing.T) {
	f := newFixture(t, "1.5GB")
	f.mark(t, "daemon-down")

	r, _, stderr := run(f.step, false)

	assert.FileExists(t, f.layer)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stderr, "SKIPPED       docker images: Docker Desktop is not running\n")
}

func TestUnansweredInfoWhileDockerDesktopRunsFails(t *testing.T) {
	f := newFixture(t, "1.5GB")
	f.mark(t, "daemon-down")
	f.step.Running = func(string) (bool, error) { return true, nil }

	r, _, _ := run(f.step, false)

	assert.FileExists(t, f.layer)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "docker info: exit status 1: failed to connect to the docker API")
}

func TestProcessCheckErrorFails(t *testing.T) {
	f := newFixture(t, "1.5GB")
	f.mark(t, "daemon-down")
	f.step.Running = func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := run(f.step, false)

	assert.FileExists(t, f.layer)
	assert.Len(t, r.Failures, 2)
}

func TestHungCommandsTimeOut(t *testing.T) {
	cases := []struct {
		marker string
		want   string
	}{
		{"info-hung", "docker info did not answer within 1s: the Docker daemon is hung, restart Docker Desktop"},
		{"df-hung", "docker system df did not answer within 1s"},
		{"prune-hung", "docker image prune did not finish within 1s"},
	}
	for _, tc := range cases {
		t.Run(tc.marker, func(t *testing.T) {
			f := newFixture(t, "1.5GB")
			f.mark(t, tc.marker)
			started := time.Now()

			r, _, _ := run(f.step, false)

			assert.Less(t, time.Since(started), 10*time.Second)
			assert.Zero(t, r.Count)
			require.Len(t, r.Failures, 1)
			assert.Equal(t, tc.want, r.Failures[0])
		})
	}
}

func TestFailedPruneFailsAndCountsNothing(t *testing.T) {
	f := newFixture(t, "1.5GB")
	f.mark(t, "prune-fails")

	r, _, _ := run(f.step, false)

	assert.FileExists(t, f.layer)
	assert.Zero(t, r.Count)
	assert.Zero(t, r.KB)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "docker image prune: exit status 1: Error response from daemon")
}

func TestPruneFailingMidwayCountsOnlyFreedSpace(t *testing.T) {
	f := newFixture(t, "1.5GB")
	f.mark(t, "prune-fails-midway")

	r, _, _ := run(f.step, false)

	assert.NoFileExists(t, f.layer)
	assert.Zero(t, r.Count)
	assert.Positive(t, r.KB)
	assert.Less(t, r.KB, int64(1464843))
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "docker image prune: exit status 1: Error response from daemon: conflict")
}

func TestTimeoutStopsTheChildrenOfDocker(t *testing.T) {
	f := newFixture(t, "1.5GB")
	f.mark(t, "info-hung-in-child")

	r, _, _ := run(f.step, false)

	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "docker info did not answer within 1s")
	f.assertChildStopped(t)
}

func TestChildHoldingOutputIsStoppedAndFails(t *testing.T) {
	for _, exitStatus := range []string{"0", "1"} {
		t.Run("exit "+exitStatus, func(t *testing.T) {
			f := newFixture(t, "1.5GB")
			f.step.TimeoutSeconds = 5
			require.NoError(t, os.WriteFile(filepath.Join(f.state, "info-child-holds-output"), []byte(exitStatus), 0o644))
			started := time.Now()

			r, _, stderr := run(f.step, false)

			assert.Less(t, time.Since(started), 4*time.Second)
			assert.NotContains(t, stderr, "SKIPPED")
			require.Len(t, r.Failures, 1)
			assert.Contains(t, r.Failures[0], "docker info: exited but left a process running, which was stopped")
			f.assertChildStopped(t)
		})
	}
}

func TestDetachedChildOfASuccessfulCommandFails(t *testing.T) {
	f := newFixture(t, "1.5GB")
	f.mark(t, "df-leaves-detached-child")

	r, _, _ := run(f.step, false)

	assert.FileExists(t, f.layer)
	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "docker system df: it left a process running")
	f.assertChildStopped(t)
}

func (f fixture) assertChildStopped(t *testing.T) {
	pidText, err := os.ReadFile(filepath.Join(f.state, "child.pid"))
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
	require.NoError(t, err)
	assert.Eventually(t, func() bool {
		return syscall.Kill(pid, 0) != nil
	}, 5*time.Second, 50*time.Millisecond, "the child of docker is still running")
}

func TestUnreadableReclaimableSizeFails(t *testing.T) {
	f := newFixture(t, "lots")

	r, _, _ := run(f.step, false)

	assert.FileExists(t, f.layer)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "docker system df: cannot read the reclaimable size from: Images\tlots (99%)")
}

func TestWithoutDockerDoesNothing(t *testing.T) {
	f := newFixture(t, "1.5GB")
	f.step.Docker = "diskcleaner-test-absent-docker"

	r, stdout, stderr := run(f.step, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestDefaultRejectsFractionalTimeout(t *testing.T) {
	t.Setenv("DISKCLEANER_DOCKER_TIMEOUT", "1.5")

	_, err := Default(t.TempDir())

	assert.EqualError(t, err, "DISKCLEANER_DOCKER_TIMEOUT must be a whole number above zero, got: 1.5")
}

const maxTimeoutSeconds = math.MaxInt64 / int64(time.Second)

func TestDefaultRejectsTimeoutBeyondDuration(t *testing.T) {
	t.Setenv("DISKCLEANER_DOCKER_PRUNE_TIMEOUT", strconv.FormatInt(maxTimeoutSeconds+1, 10))

	_, err := Default(t.TempDir())

	assert.EqualError(t, err, fmt.Sprintf("DISKCLEANER_DOCKER_PRUNE_TIMEOUT must be at most %d seconds, got: %d", maxTimeoutSeconds, maxTimeoutSeconds+1))
}

func TestDefaultAcceptsLongestTimeout(t *testing.T) {
	t.Setenv("DISKCLEANER_DOCKER_PRUNE_TIMEOUT", strconv.FormatInt(maxTimeoutSeconds, 10))

	s, err := Default(t.TempDir())

	require.NoError(t, err)
	assert.Equal(t, maxTimeoutSeconds, int64(s.PruneTimeoutSeconds))
}
