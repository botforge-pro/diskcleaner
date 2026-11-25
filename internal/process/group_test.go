package process

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAwaitGroupGivesUpOnARunThatOutlivesItsKill(t *testing.T) {
	limit := 50 * time.Millisecond
	grace := 50 * time.Millisecond
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := time.Now()

	result := awaitGroup(limit, grace, func(context.Context) Result {
		<-release
		return Result{}
	})

	assert.True(t, result.TimedOut)
	assert.True(t, result.Leftover)
	require.ErrorContains(t, result.Err, "still running 50ms past its 50ms limit")
	assert.Less(t, time.Since(started), time.Second)
}

func TestAwaitGroupReturnsWhatAFinishedRunGives(t *testing.T) {
	want := Result{Stdout: "n/a\n"}

	result := awaitGroup(time.Second, time.Second, func(context.Context) Result { return want })

	assert.Equal(t, want, result)
}

func TestAwaitGroupCancelsTheRunAtItsLimit(t *testing.T) {
	result := awaitGroup(50*time.Millisecond, time.Second, func(ctx context.Context) Result {
		<-ctx.Done()
		return Result{TimedOut: true, Err: ctx.Err()}
	})

	assert.True(t, result.TimedOut)
	assert.False(t, result.Leftover)
	require.ErrorIs(t, result.Err, context.DeadlineExceeded)
}

func childScript(t *testing.T, body string) (string, string) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "tool")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"+strings.ReplaceAll(body, "PIDFILE", pidFile)), 0o755))
	return script, pidFile
}

func assertChildStopped(t *testing.T, pidFile string) {
	text, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(text)))
	require.NoError(t, err)
	assert.Eventually(t, func() bool { return syscall.Kill(pid, 0) != nil }, 5*time.Second, 50*time.Millisecond, "the child is still running")
}

func TestRunGroupStopsTheChildrenOfACommandPastItsLimit(t *testing.T) {
	script, pidFile := childScript(t, "sleep 30 &\necho $! >PIDFILE\nwait\n")

	result := RunGroup(1, script)

	assert.True(t, result.TimedOut)
	assertChildStopped(t, pidFile)
}

func TestRunGroupStopsAChildHoldingTheOutputAfterItsParentExits(t *testing.T) {
	for _, status := range []string{"0", "1"} {
		t.Run("exit "+status, func(t *testing.T) {
			script, pidFile := childScript(t, "sleep 30 &\necho $! >PIDFILE\nexit "+status+"\n")
			started := time.Now()

			result := RunGroup(5, script)

			assert.Less(t, time.Since(started), 4*time.Second)
			assert.True(t, result.Leftover)
			assertChildStopped(t, pidFile)
		})
	}
}

func TestRunGroupStopsADetachedChildOfACommandThatSucceeded(t *testing.T) {
	script, pidFile := childScript(t, "sleep 30 >/dev/null 2>&1 &\necho $! >PIDFILE\n")

	result := RunGroup(5, script)

	require.ErrorContains(t, result.Err, "it left a process running")
	assertChildStopped(t, pidFile)
}
