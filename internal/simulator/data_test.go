package simulator

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

const fakeXcrun = `#!/bin/sh
echo "$@" >>"$DISKCLEANER_TEST_DEVICES/../calls"
[ "$1 $2" = "simctl erase" ] || exit 64
[ -e "$DISKCLEANER_TEST_DEVICES/$3/erase-fails" ] && { echo "Unable to erase contents and settings" >&2; exit 1; }
rm -rf "$DISKCLEANER_TEST_DEVICES/$3/data/Containers"
`

var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

type dataFixture struct {
	devices string
	values  map[string]map[string]string
	running map[string]bool
	xcrun   string
}

func newDataFixture(t *testing.T) *dataFixture {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	f := &dataFixture{
		devices: filepath.Join(root, "Devices"),
		values:  map[string]map[string]string{},
		running: map[string]bool{},
		xcrun:   filepath.Join(root, "xcrun"),
	}
	require.NoError(t, os.WriteFile(f.xcrun, []byte(fakeXcrun), 0o755))
	t.Setenv("DISKCLEANER_TEST_DEVICES", f.devices)
	return f
}

func (f *dataFixture) device(t *testing.T, udid, state, lastUsed string) {
	path := filepath.Join(f.devices, udid, appData)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, 8192), 0o644))
	values := map[string]string{"UDID": udid, "state": state}
	if lastUsed != "" {
		values["lastUsedAt"] = lastUsed
	}
	f.values[filepath.Join(f.devices, udid, "device.plist")] = values
}

func (f *dataFixture) step() DataStep {
	return DataStep{
		Devices:   f.devices,
		Processes: "simulator-ui",
		IdleDays:  3,
		Xcrun:     f.xcrun,
		Running: func(pattern string) (bool, error) {
			return f.running[pattern], nil
		},
		Value: func(plist, key string) (string, error) {
			values, ok := f.values[plist]
			if !ok {
				return "", errors.New("Could not extract value")
			}
			value, ok := values[key]
			if !ok {
				return "", errNoValue
			}
			return value, nil
		},
		Now: func() time.Time { return now },
	}
}

func (f *dataFixture) run(s DataStep, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func (f *dataFixture) erased(udid string) bool {
	_, err := os.Stat(filepath.Join(f.devices, udid, appData))
	return errors.Is(err, os.ErrNotExist)
}

func (f *dataFixture) calls(t *testing.T) string {
	calls, err := os.ReadFile(filepath.Join(f.devices, "..", "calls"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)
	return string(calls)
}

func TestErasesSimulatorIdleLongerThanTheLimit(t *testing.T) {
	f := newDataFixture(t)
	f.device(t, "OLD", "1", "2026-10-18T11:59:59Z")
	f.device(t, "RECENT", "1", "2026-10-19T12:00:00Z")

	r, stdout, stderr := f.run(f.step(), false)

	assert.True(t, f.erased("OLD"))
	assert.False(t, f.erased("RECENT"))
	assert.Equal(t, "simctl erase OLD\n", f.calls(t))
	assert.Equal(t, 1, r.Count)
	assert.Positive(t, r.KB)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stdout, "RUN           "+f.xcrun+" simctl erase OLD")
	assert.Contains(t, stderr, "RECENT: the simulator was used within 3 days")
}

func TestKeepsSimulatorNeverBooted(t *testing.T) {
	f := newDataFixture(t)
	f.device(t, "NEVER", "1", "")

	r, _, stderr := f.run(f.step(), false)

	assert.False(t, f.erased("NEVER"))
	assert.Empty(t, f.calls(t))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "NEVER: the simulator was never booted")
}

func TestKeepsSimulatorAlreadyErased(t *testing.T) {
	f := newDataFixture(t)
	f.device(t, "ERASED", "1", "2026-09-01T00:00:00Z")
	require.NoError(t, os.RemoveAll(filepath.Join(f.devices, "ERASED", "data", "Containers")))

	r, _, stderr := f.run(f.step(), false)

	assert.Empty(t, f.calls(t))
	assert.Zero(t, r.Count)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stderr, "ERASED: not booted since it was created or erased")
}

func TestDataDryRunErasesNothing(t *testing.T) {
	f := newDataFixture(t)
	f.device(t, "OLD", "1", "2026-09-01T00:00:00Z")

	r, stdout, _ := f.run(f.step(), true)

	assert.False(t, f.erased("OLD"))
	assert.Empty(t, f.calls(t))
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "WOULD RUN     "+f.xcrun+" simctl erase OLD")
}

func TestKeepsBootedSimulator(t *testing.T) {
	f := newDataFixture(t)
	f.device(t, "BOOTED", "3", "2026-09-01T00:00:00Z")

	r, _, stderr := f.run(f.step(), false)

	assert.False(t, f.erased("BOOTED"))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "BOOTED: the simulator is not shut down (state 3)")
}

func TestKeepsAllWhileSimulatorRuns(t *testing.T) {
	f := newDataFixture(t)
	f.device(t, "OLD", "1", "2026-09-01T00:00:00Z")
	f.running["simulator-ui"] = true

	r, _, stderr := f.run(f.step(), false)

	assert.False(t, f.erased("OLD"))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "Simulator, DeviceHub, simctl or an Xcode build is running")
}

func TestKeepsSimulatorWithItsOwnProcess(t *testing.T) {
	f := newDataFixture(t)
	f.device(t, "OLD", "1", "2026-09-01T00:00:00Z")
	f.running[filepath.Join(f.devices, "OLD")+"/"] = true

	_, _, stderr := f.run(f.step(), false)

	assert.False(t, f.erased("OLD"))
	assert.Contains(t, stderr, "OLD: a process of this simulator is running")
}

func TestUnreadableLastUsedFails(t *testing.T) {
	f := newDataFixture(t)
	f.device(t, "OLD", "1", "last week")

	r, _, stderr := f.run(f.step(), false)

	assert.False(t, f.erased("OLD"))
	require.Len(t, r.Failures, 1)
	assert.Contains(t, stderr, `cannot read lastUsedAt "last week"`)
}

func TestUnreadablePlistFails(t *testing.T) {
	f := newDataFixture(t)
	f.device(t, "OLD", "1", "2026-09-01T00:00:00Z")
	delete(f.values, filepath.Join(f.devices, "OLD", "device.plist"))

	r, _, _ := f.run(f.step(), false)

	assert.False(t, f.erased("OLD"))
	require.Len(t, r.Failures, 1)
}

func TestFailedEraseIsAFailure(t *testing.T) {
	f := newDataFixture(t)
	f.device(t, "OLD", "1", "2026-09-01T00:00:00Z")
	require.NoError(t, os.WriteFile(filepath.Join(f.devices, "OLD", "erase-fails"), nil, 0o644))

	r, _, stderr := f.run(f.step(), false)

	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, stderr, "Unable to erase contents and settings")
}

func TestMissingDevicesIsQuietForData(t *testing.T) {
	f := newDataFixture(t)

	r, stdout, stderr := f.run(f.step(), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout+stderr)
}
