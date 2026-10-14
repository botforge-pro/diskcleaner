package simulator

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

const (
	appCache    = "data/Containers/Data/Application/APP/Library/Caches/com.example.app/" + cacheName
	systemCache = "data/Library/Caches/com.apple.icloudmailagent/" + cacheName
	appData     = "data/Containers/Data/Application/APP/Library/Application Support/default.store"
	documents   = "data/Containers/Data/Application/APP/Documents/" + cacheName + "/kept"
)

type fixture struct {
	devices string
	states  map[string]string
	running map[string]bool
}

func newFixture(t *testing.T) *fixture {
	devices, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	f := &fixture{
		devices: devices,
		states:  map[string]string{},
		running: map[string]bool{},
	}
	f.device(t, "shut", "1")
	f.device(t, "booted", "3")
	return f
}

func (f *fixture) device(t *testing.T, name, state string) {
	for _, file := range []string{appCache + "/24A434/model.bundle", systemCache + "/entry", appData, documents} {
		path := filepath.Join(f.devices, name, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
	}
	f.states[filepath.Join(f.devices, name, "device.plist")] = state
}

func (f *fixture) step() Step {
	return Step{
		Devices:   f.devices,
		Processes: "simulator-ui",
		Running: func(pattern string) (bool, error) {
			return f.running[pattern], nil
		},
		State: func(plist string) (string, error) {
			state, ok := f.states[plist]
			if !ok {
				return "", errors.New("No value at that key path")
			}
			return state, nil
		},
	}
}

func (f *fixture) run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func (f *fixture) exists(name, file string) bool {
	_, err := os.Stat(filepath.Join(f.devices, name, file))
	return err == nil
}

func TestRemovesCachesOfShutDownSimulator(t *testing.T) {
	f := newFixture(t)

	r, stdout, stderr := f.run(f.step(), false)

	assert.False(t, f.exists("shut", appCache))
	assert.False(t, f.exists("shut", systemCache))
	assert.True(t, f.exists("shut", appData))
	assert.True(t, f.exists("shut", documents))
	assert.True(t, f.exists("booted", appCache))
	assert.Equal(t, 2, r.Count)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stdout, "REMOVE        ")
	assert.Contains(t, stderr, "SKIPPED       "+filepath.Join(f.devices, "booted")+": the simulator is not shut down (state 3)")
}

func TestDryRunRemovesNothing(t *testing.T) {
	f := newFixture(t)

	r, stdout, _ := f.run(f.step(), true)

	assert.True(t, f.exists("shut", appCache))
	assert.Equal(t, 2, r.Count)
	assert.Equal(t, 2, strings.Count(stdout, "WOULD REMOVE  "))
}

func TestSkipsAllWhileSimulatorRuns(t *testing.T) {
	f := newFixture(t)
	f.running["simulator-ui"] = true

	r, _, stderr := f.run(f.step(), false)

	assert.True(t, f.exists("shut", appCache))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "Simulator, DeviceHub, simctl or an Xcode build is running")
}

func TestSkipsSimulatorWithOwnProcess(t *testing.T) {
	f := newFixture(t)
	f.running[filepath.Join(f.devices, "shut")+"/"] = true

	_, _, stderr := f.run(f.step(), false)

	assert.True(t, f.exists("shut", appCache))
	assert.Contains(t, stderr, "shut: a process of this simulator is running")
}

func TestKeepsSimulatorWhoseStateCannotBeRead(t *testing.T) {
	f := newFixture(t)
	delete(f.states, filepath.Join(f.devices, "shut", "device.plist"))

	r, _, stderr := f.run(f.step(), false)

	assert.True(t, f.exists("shut", appCache))
	require.Len(t, r.Failures, 1)
	assert.Contains(t, stderr, "FAILED        plutil ")
}

func TestProcessCheckErrorSkipsAndFails(t *testing.T) {
	f := newFixture(t)
	s := f.step()
	s.Running = func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := f.run(s, false)

	assert.True(t, f.exists("shut", appCache))
	assert.Len(t, r.Failures, 1)
}

func TestFollowsSymlinkedDevicesDirectory(t *testing.T) {
	f := newFixture(t)
	link := filepath.Join(t.TempDir(), "Devices")
	require.NoError(t, os.Symlink(f.devices, link))
	s := f.step()
	s.Devices = link

	r, _, _ := f.run(s, true)

	assert.Equal(t, 2, r.Count)
}

func TestKeepsDocumentsWhenDevicesLieUnderCaches(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	f := &fixture{
		devices: filepath.Join(root, "Library/Caches/sims/Devices"),
		states:  map[string]string{},
		running: map[string]bool{},
	}
	f.device(t, "shut", "1")

	r, _, _ := f.run(f.step(), false)

	assert.True(t, f.exists("shut", documents))
	assert.True(t, f.exists("shut", appData))
	assert.False(t, f.exists("shut", appCache))
	assert.Equal(t, 2, r.Count)
}

func TestUnreadableDevicesDirectoryFails(t *testing.T) {
	f := newFixture(t)
	parent := filepath.Dir(f.devices)
	s := f.step()
	s.Devices = filepath.Join(parent, "locked", "Devices")
	require.NoError(t, os.MkdirAll(s.Devices, 0o755))
	require.NoError(t, os.Chmod(filepath.Join(parent, "locked"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(parent, "locked"), 0o755) })

	r, _, stderr := f.run(s, false)

	require.Len(t, r.Failures, 1)
	assert.Contains(t, stderr, "FAILED        stat "+s.Devices)
}

func TestMissingDevicesDirectoryIsQuiet(t *testing.T) {
	f := newFixture(t)
	s := f.step()
	s.Devices = filepath.Join(t.TempDir(), "absent")

	r, stdout, stderr := f.run(s, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
