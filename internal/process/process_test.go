package process

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXcodeMatchesXcodeAndItsBuilds(t *testing.T) {
	pattern := regexp.MustCompile(Xcode)

	for _, command := range []string{
		"/Applications/Xcode.app/Contents/MacOS/Xcode",
		"/Applications/Xcode-beta.app/Contents/MacOS/Xcode",
		"/Applications/Xcode_26.1.app/Contents/MacOS/Xcode -NSDocumentRevisionsDebugMode YES",
		"/Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild -scheme App",
		"/Applications/Xcode.app/Contents/SharedFrameworks/XCBuild.framework/Versions/A/PlugIns/XCBBuildService.bundle/Contents/MacOS/XCBBuildService",
	} {
		assert.True(t, pattern.MatchString(command), command)
	}
	for _, command := range []string{
		"/Applications/Xcode.app/Contents/Developer/usr/bin/git status",
		"/Applications/Xcode.app/Contents/Developer/Applications/Simulator.app/Contents/MacOS/Simulator",
	} {
		assert.False(t, pattern.MatchString(command), command)
	}
}

func TestPidsCPUTimesAndTerminateFollowARealProcess(t *testing.T) {
	seconds := "61.4" + strings.ReplaceAll(time.Now().Format("150405.000"), ".", "")
	sleeper := exec.Command("sleep", seconds)
	require.NoError(t, sleeper.Start())
	exited := make(chan struct{})
	go func() {
		_ = sleeper.Wait()
		close(exited)
	}()
	t.Cleanup(func() { _ = sleeper.Process.Kill() })
	pattern := "sleep " + regexp.QuoteMeta(seconds)

	pids, err := Pids(pattern)
	require.NoError(t, err)
	require.Len(t, pids, 1)
	times, err := CPUTimes(pids)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(times, pids[0]+" "), times)

	require.NoError(t, Terminate(pattern))
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the process did not stop after Terminate")
	}
	pids, err = Pids(pattern)
	require.NoError(t, err)
	assert.Empty(t, pids)
}

func startSleeper(t *testing.T) string {
	sleeper := exec.Command("sleep", "60")
	require.NoError(t, sleeper.Start())
	t.Cleanup(func() {
		_ = sleeper.Process.Kill()
		_ = sleeper.Wait()
	})
	return strconv.Itoa(sleeper.Process.Pid)
}

func exitedPid(t *testing.T) string {
	finished := exec.Command("true")
	require.NoError(t, finished.Run())
	return strconv.Itoa(finished.Process.Pid)
}

func TestCPUTimesLeavesOutProcessesThatAreGone(t *testing.T) {
	live := startSleeper(t)
	gone := exitedPid(t)

	times, err := CPUTimes([]string{gone})
	require.NoError(t, err)
	assert.Empty(t, times)

	times, err = CPUTimes([]string{gone, live})
	require.NoError(t, err)
	assert.Len(t, strings.Split(times, "\n"), 1)
	assert.True(t, strings.HasPrefix(times, live+" "), times)
}

func TestCPUTimesIsTheSameInAnyOrder(t *testing.T) {
	first, second := startSleeper(t), startSleeper(t)

	forward, err := CPUTimes([]string{first, second})
	require.NoError(t, err)
	backward, err := CPUTimes([]string{second, first})
	require.NoError(t, err)

	assert.Equal(t, forward, backward)
	assert.Len(t, strings.Split(forward, "\n"), 2)
}

func TestCPUTimesRefusesAPidPsCannotRead(t *testing.T) {
	_, err := CPUTimes([]string{"not-a-pid"})
	assert.Error(t, err)
}

func TestExit1IsAnAnswerOnlyWithoutAnError(t *testing.T) {
	stdout, err := run("sh", "-c", "echo found; exit 1")
	require.NoError(t, err)
	assert.Equal(t, "found\n", stdout)

	_, err = run("sh", "-c", "echo found; echo broken >&2; exit 1")
	assert.ErrorContains(t, err, "broken")
}

func TestNoMatchIsNotAnError(t *testing.T) {
	pids, err := Pids("diskcleaner-test-no-such-process-[0-9]{40}")
	require.NoError(t, err)
	assert.Empty(t, pids)
	assert.NoError(t, Terminate("diskcleaner-test-no-such-process-[0-9]{40}"))
	times, err := CPUTimes(nil)
	require.NoError(t, err)
	assert.Empty(t, times)
}

func TestBadPatternIsAnError(t *testing.T) {
	_, err := Pids("sleep(")
	assert.Error(t, err)
}
