package main

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/rule"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func idleProcesses(string) (bool, error) { return false, nil }

func builtinFile(t *testing.T, name string) rule.File {
	data, err := fs.ReadFile(builtinFiles, "builtin/"+name+".yaml")
	require.NoError(t, err)
	f, err := rule.Parse(name+".yaml", data)
	require.NoError(t, err)
	return f
}

func builtinRoot(t *testing.T, name, home string, env map[string]string) string {
	return builtinFile(t, name).Rule(rule.Environment{
		Home:     home,
		Getenv:   func(key string) string { return env[key] },
		Running:  idleProcesses,
		Duration: func(key string) time.Duration { return settingsDefault(t, name+"."+key) },
	}).Root()
}

func settingsDefault(t *testing.T, key string) time.Duration {
	s, ok := settingsTable.Find(key)
	require.True(t, ok, key)
	value, err := config.Parse(s, s.Default, "/")
	require.NoError(t, err)
	return value.(time.Duration)
}

func TestEveryBuiltinFileLoadsAndIsInTheCatalog(t *testing.T) {
	names, err := fs.Glob(builtinFiles, "builtin/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, names)
	for _, name := range names {
		step := filepath.Base(name[:len(name)-len(".yaml")])
		assert.Contains(t, stepNames(), step, "%s is loaded but no step runs it", name)
	}
}

func TestACatalogThatCannotRunAsWrittenIsRefused(t *testing.T) {
	code := map[string]construct{"a": nil, "unused": nil}
	files := []rule.File{
		{Name: "a", Order: 10, Run: "code"},
		{Name: "b", Order: 10, Run: "code"},
	}

	err := checkCatalog(files, code)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "b.yaml: order 10: a.yaml has it too")
	assert.Contains(t, err.Error(), "b.yaml: run: code, but no step of this name is written in Go")
	assert.Contains(t, err.Error(), "the step unused is written in code, but no file runs it")
}

func TestSwiftPMCacheGoesWholeWhileNoSwiftOrXcodeBuilds(t *testing.T) {
	home := t.TempDir()
	f := builtinFile(t, "swiftpm-cache")

	assert.Equal(t, filepath.Join(home, "Library/Caches/org.swift.swiftpm"), builtinRoot(t, "swiftpm-cache", home, nil))
	assert.True(t, f.Remove.Root)
	pattern := regexp.MustCompile(f.BusyPattern())
	for _, command := range []string{
		"/usr/bin/swift-build --package-path app",
		"/usr/bin/swift-run app",
		"/Applications/Xcode-beta.app/Contents/MacOS/Xcode",
	} {
		assert.True(t, pattern.MatchString(command), command)
	}
	assert.False(t, pattern.MatchString("/Applications/Xcode.app/Contents/Developer/usr/bin/git status"))
}

func TestSessionLogsAreLookedForWhereEachToolKeepsThem(t *testing.T) {
	home := t.TempDir()

	assert.Equal(t, filepath.Join(home, ".claude", "projects"), builtinRoot(t, "claude-logs", home, nil))
	assert.Equal(t, "CLAUDE", builtinFile(t, "claude-logs").Label())
	assert.Equal(t, "/opt/claude/projects", builtinRoot(t, "claude-logs", home, map[string]string{"CLAUDE_CONFIG_DIR": "/opt/claude"}))

	assert.Equal(t, filepath.Join(home, ".codex", "sessions"), builtinRoot(t, "codex-logs", home, nil))
	assert.Equal(t, "CODEX", builtinFile(t, "codex-logs").Label())
	assert.Equal(t, "/opt/codex/sessions", builtinRoot(t, "codex-logs", home, map[string]string{"CODEX_HOME": "/opt/codex"}))
}

func TestSessionLogsKeepWhatIsYoung(t *testing.T) {
	for _, name := range []string{"claude-logs", "codex-logs"} {
		f := builtinFile(t, name)

		assert.Equal(t, []string{"**/*.jsonl"}, f.Remove.Files, name)
		assert.Equal(t, "min-age", f.Keep.WrittenWithin, name)
		assert.Equal(t, 20*24*time.Hour, settingsDefault(t, name+".min-age"), name)
	}
}

func TestEmulatorSnapshotsAreLookedForWhereAndroidKeepsDevices(t *testing.T) {
	home := t.TempDir()

	assert.Equal(t, filepath.Join(home, ".android/avd"), builtinRoot(t, "emulator-snapshots", home, nil))
	assert.Equal(t, "/opt/android/avd", builtinRoot(t, "emulator-snapshots", home, map[string]string{"ANDROID_USER_HOME": "/opt/android"}))
	assert.Equal(t, "/opt/avd", builtinRoot(t, "emulator-snapshots", home, map[string]string{"ANDROID_AVD_HOME": "/opt/avd", "ANDROID_USER_HOME": "/opt/android"}))
}

func TestEmulatorSnapshotsWaitForTheEmulatorAndNotTheSDKTools(t *testing.T) {
	f := builtinFile(t, "emulator-snapshots")

	assert.Equal(t, []string{"*.avd/snapshots/*"}, f.Remove.Folders)
	pattern := regexp.MustCompile(f.BusyPattern())
	for _, command := range []string{
		"/Users/me/Library/Android/sdk/emulator/qemu/darwin-aarch64/qemu-system-aarch64 -avd Pixel",
		"/Users/me/Library/Android/sdk/emulator/emulator -avd Pixel -no-window",
	} {
		assert.True(t, pattern.MatchString(command), command)
	}
	assert.False(t, pattern.MatchString("/Users/me/Library/Android/sdk/platform-tools/adb devices"))
	assert.False(t, pattern.MatchString("/Users/me/Library/Android/sdk/emulator/crashpad_handler"))
}
