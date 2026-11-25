package main

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/rule"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func idleProcesses(string) (bool, error) { return false, nil }

func builtinRule(t *testing.T, name, home string, env map[string]string) rule.Rule {
	data, err := fs.ReadFile(builtinFiles, "builtin/"+name+".yaml")
	require.NoError(t, err)
	f, err := rule.Parse(name+".yaml", data)
	require.NoError(t, err)
	return f.Rule(home, func(key string) string { return env[key] }, idleProcesses)
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

func TestTheStepsRunInTheirOrder(t *testing.T) {
	assert.Equal(t, []string{
		"projects", "gradle-caches", "docker-images", "go-cache", "homebrew", "swiftpm-cache",
		"xcode-derived-data", "xcode-archives", "temp-leftovers", "system-temp", "code-sign-clones",
		"ide-old-versions", "simulator-caches", "emulator-snapshots", "claude-logs", "codex-logs",
		"claude-scratchpads",
	}, stepNames())
}

func TestACatalogThatCannotRunAsWrittenIsRefused(t *testing.T) {
	code := map[string]construct{"a": nil, "unused": nil}
	files := []rule.File{
		{Name: "a", Order: 10, Run: "a"},
		{Name: "b", Order: 10, Run: "missing"},
	}

	err := checkCatalog(files, code)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "b.yaml: order 10: a.yaml has it too")
	assert.Contains(t, err.Error(), `b.yaml: run "missing": no step written in code has this name`)
	assert.Contains(t, err.Error(), "the step unused is written in code, but no file runs it")
}

func TestSwiftPMCacheGoesWholeWhileNoSwiftOrXcodeBuilds(t *testing.T) {
	home := t.TempDir()
	s := builtinRule(t, "swiftpm-cache", home, nil)

	assert.Equal(t, filepath.Join(home, "Library/Caches/org.swift.swiftpm"), s.Root)
	assert.Empty(t, s.Targets)
	pattern := regexp.MustCompile(s.Busy.Processes)
	for _, command := range []string{
		"/usr/bin/swift-build --package-path app",
		"/usr/bin/swift-run app",
		"/Applications/Xcode-beta.app/Contents/MacOS/Xcode",
	} {
		assert.True(t, pattern.MatchString(command), command)
	}
	assert.False(t, pattern.MatchString("/Applications/Xcode.app/Contents/Developer/usr/bin/git status"))
}

func TestEmulatorSnapshotsAreLookedForWhereAndroidKeepsDevices(t *testing.T) {
	home := t.TempDir()

	assert.Equal(t, filepath.Join(home, ".android/avd"), builtinRule(t, "emulator-snapshots", home, nil).Root)
	assert.Equal(t, "/opt/android/avd", builtinRule(t, "emulator-snapshots", home, map[string]string{"ANDROID_USER_HOME": "/opt/android"}).Root)
	assert.Equal(t, "/opt/avd", builtinRule(t, "emulator-snapshots", home, map[string]string{"ANDROID_AVD_HOME": "/opt/avd", "ANDROID_USER_HOME": "/opt/android"}).Root)
}

func TestEmulatorSnapshotsWaitForTheEmulatorAndNotTheSDKTools(t *testing.T) {
	s := builtinRule(t, "emulator-snapshots", t.TempDir(), nil)

	assert.Equal(t, []string{"*.avd/snapshots/*"}, s.Targets)
	pattern := regexp.MustCompile(s.Busy.Processes)
	for _, command := range []string{
		"/Users/me/Library/Android/sdk/emulator/qemu/darwin-aarch64/qemu-system-aarch64 -avd Pixel",
		"/Users/me/Library/Android/sdk/emulator/emulator -avd Pixel -no-window",
	} {
		assert.True(t, pattern.MatchString(command), command)
	}
	assert.False(t, pattern.MatchString("/Users/me/Library/Android/sdk/platform-tools/adb devices"))
	assert.False(t, pattern.MatchString("/Users/me/Library/Android/sdk/emulator/crashpad_handler"))
}
