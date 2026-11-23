package emulator

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDevice(t *testing.T) (string, string) {
	devices := t.TempDir()
	avd := filepath.Join(devices, "Pixel.avd")
	snapshot := filepath.Join(avd, "snapshots", "default_boot")
	require.NoError(t, os.MkdirAll(snapshot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snapshot, "ram.bin"), make([]byte, 4096), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(avd, "userdata-qemu.img.qcow2"), make([]byte, 4096), 0o644))
	return devices, avd
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func idle(string) (bool, error) { return false, nil }

func TestRemovesSnapshotsAndKeepsTheDeviceData(t *testing.T) {
	devices, avd := newDevice(t)

	r, stdout, _ := run(Step{Devices: devices, Running: idle}, false)

	assert.NoDirExists(t, filepath.Join(avd, "snapshots", "default_boot"))
	assert.FileExists(t, filepath.Join(avd, "userdata-qemu.img.qcow2"))
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "EMULATOR "+devices+"\n")
}

func TestDryRunRemovesNothing(t *testing.T) {
	devices, avd := newDevice(t)

	r, _, _ := run(Step{Devices: devices, Running: idle}, true)

	assert.DirExists(t, filepath.Join(avd, "snapshots", "default_boot"))
	assert.Equal(t, 1, r.Count)
}

func TestSkipsWhileAnEmulatorRuns(t *testing.T) {
	devices, avd := newDevice(t)
	busy := func(string) (bool, error) { return true, nil }

	r, _, stderr := run(Step{Devices: devices, Running: busy}, false)

	assert.DirExists(t, filepath.Join(avd, "snapshots", "default_boot"))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+devices+": an Android emulator is running")
}

func TestProcessCheckErrorSkipsAndFails(t *testing.T) {
	devices, avd := newDevice(t)
	broken := func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := run(Step{Devices: devices, Running: broken}, false)

	assert.DirExists(t, filepath.Join(avd, "snapshots", "default_boot"))
	assert.Len(t, r.Failures, 1)
}

func TestMissingDevicesAreQuiet(t *testing.T) {
	r, stdout, stderr := run(Step{Devices: filepath.Join(t.TempDir(), "absent"), Running: idle}, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestDefaultPatternMatchesTheEmulatorAndNotTheSDKTools(t *testing.T) {
	pattern := regexp.MustCompile(Default(t.TempDir()).Processes)

	for _, command := range []string{
		"/Users/me/Library/Android/sdk/emulator/qemu/darwin-aarch64/qemu-system-aarch64 -avd Pixel",
		"/Users/me/Library/Android/sdk/emulator/emulator -avd Pixel -no-window",
	} {
		assert.True(t, pattern.MatchString(command), command)
	}
	assert.False(t, pattern.MatchString("/Users/me/Library/Android/sdk/platform-tools/adb devices"))
	assert.False(t, pattern.MatchString("/Users/me/Library/Android/sdk/emulator/crashpad_handler"))
}

func TestAvdHomeComesFromTheAndroidVariables(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANDROID_AVD_HOME", "")
	t.Setenv("ANDROID_USER_HOME", "")
	assert.Equal(t, filepath.Join(home, ".android/avd"), Default(home).Devices)

	t.Setenv("ANDROID_USER_HOME", "/opt/android")
	assert.Equal(t, "/opt/android/avd", Default(home).Devices)

	t.Setenv("ANDROID_AVD_HOME", "/opt/avd")
	assert.Equal(t, "/opt/avd", Default(home).Devices)
}
