package emulator

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOnlySnapshotsOfEachDeviceAreTargeted(t *testing.T) {
	assert.Equal(t, []string{"*.avd/snapshots/*"}, Default(t.TempDir()).Targets)
}

func TestDefaultPatternMatchesTheEmulatorAndNotTheSDKTools(t *testing.T) {
	pattern := regexp.MustCompile(Default(t.TempDir()).Busy.Processes)

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
	assert.Equal(t, filepath.Join(home, ".android/avd"), Default(home).Root)

	t.Setenv("ANDROID_USER_HOME", "/opt/android")
	assert.Equal(t, "/opt/android/avd", Default(home).Root)

	t.Setenv("ANDROID_AVD_HOME", "/opt/avd")
	assert.Equal(t, "/opt/avd", Default(home).Root)
}
