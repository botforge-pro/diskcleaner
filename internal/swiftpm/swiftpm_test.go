package swiftpm

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

func newCache(t *testing.T) string {
	cache := filepath.Join(t.TempDir(), "org.swift.swiftpm")
	path := filepath.Join(cache, "repositories", "pkg.git")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
	return cache
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func idle(string) (bool, error) { return false, nil }

func TestRemovesCache(t *testing.T) {
	cache := newCache(t)

	r, stdout, _ := run(Step{Cache: cache, Processes: "swift", Running: idle}, false)

	assert.NoDirExists(t, cache)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "SWIFTPM  "+cache+"\n")
}

func TestDryRunRemovesNothing(t *testing.T) {
	cache := newCache(t)

	r, _, _ := run(Step{Cache: cache, Processes: "swift", Running: idle}, true)

	assert.DirExists(t, cache)
	assert.Equal(t, 1, r.Count)
}

func TestDefaultPatternMatchesSwiftAndXcodeBuilds(t *testing.T) {
	t.Setenv("DISKCLEANER_SWIFT_PROCESSES", "")
	pattern := regexp.MustCompile(Default(t.TempDir()).Processes)

	for _, command := range []string{
		"/usr/bin/swift-build --package-path app",
		"/usr/bin/swift-run app",
		"/Applications/Xcode-beta.app/Contents/MacOS/Xcode",
	} {
		assert.True(t, pattern.MatchString(command), command)
	}
	assert.False(t, pattern.MatchString("/Applications/Xcode.app/Contents/Developer/usr/bin/git status"))
}

func TestSkipsWhileSwiftBuilds(t *testing.T) {
	cache := newCache(t)
	busy := func(string) (bool, error) { return true, nil }

	r, _, stderr := run(Step{Cache: cache, Processes: "swift", Running: busy}, false)

	assert.DirExists(t, cache)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+cache+": a Swift package build, xcodebuild or Xcode is running")
}

func TestProcessCheckErrorSkipsAndFails(t *testing.T) {
	cache := newCache(t)
	broken := func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := run(Step{Cache: cache, Processes: "swift", Running: broken}, false)

	assert.DirExists(t, cache)
	assert.Len(t, r.Failures, 1)
}

func TestRemovesOnlyTheLinkOfALinkedCache(t *testing.T) {
	target := newCache(t)
	link := filepath.Join(t.TempDir(), "org.swift.swiftpm")
	require.NoError(t, os.Symlink(target, link))

	r, _, _ := run(Step{Cache: link, Processes: "swift", Running: idle}, false)

	_, err := os.Lstat(link)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.FileExists(t, filepath.Join(target, "repositories", "pkg.git"))
	assert.Equal(t, 1, r.Count)
}

func TestMissingCacheIsQuiet(t *testing.T) {
	r, stdout, stderr := run(Step{Cache: filepath.Join(t.TempDir(), "absent"), Processes: "swift", Running: idle}, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
