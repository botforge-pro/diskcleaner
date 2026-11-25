package emulator

import (
	"path/filepath"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/rule"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

func Default(home string) rule.Rule {
	return rule.Rule{
		Label: "EMULATOR",
		Root:  devices(home),
		Busy: rule.Busy{
			Processes: `qemu-system-|/emulator/emulator( |$)`,
			Running:   process.Running,
			Says:      "an Android emulator is running",
		},
		Targets: []string{"*.avd/snapshots/*"},
	}
}

func devices(home string) string {
	if dir := settings.Env("ANDROID_AVD_HOME", ""); dir != "" {
		return dir
	}
	if dir := settings.Env("ANDROID_USER_HOME", ""); dir != "" {
		return filepath.Join(dir, "avd")
	}
	return filepath.Join(home, ".android/avd")
}
