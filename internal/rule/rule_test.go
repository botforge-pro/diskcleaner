package rule

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCache(t *testing.T) string {
	cache := filepath.Join(t.TempDir(), "cache")
	path := filepath.Join(cache, "repositories", "pkg.git")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
	return cache
}

func newDevices(t *testing.T) (string, string) {
	devices := t.TempDir()
	avd := filepath.Join(devices, "Pixel.avd")
	snapshot := filepath.Join(avd, "snapshots", "default_boot")
	require.NoError(t, os.MkdirAll(snapshot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snapshot, "ram.bin"), make([]byte, 4096), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(avd, "snapshots", "snapshots.pb"), make([]byte, 4096), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(avd, "userdata-qemu.img.qcow2"), make([]byte, 4096), 0o644))
	return devices, avd
}

func run(s Rule, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func idle(string) (bool, error) { return false, nil }

func cache(root string, running func(string) (bool, error)) Rule {
	return Rule{Label: "SWIFTPM", Root: root, Busy: Busy{Processes: "swift", Running: running, Says: "a Swift build is running"}}
}

func TestWithoutTargetsTheRootGoesWhole(t *testing.T) {
	root := newCache(t)

	r, stdout, _ := run(cache(root, idle), false)

	assert.NoDirExists(t, root)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "SWIFTPM  "+root+"\n")
}

func TestTargetsRemoveTheFoldersTheyMatchAndNothingElse(t *testing.T) {
	devices, avd := newDevices(t)

	r, stdout, _ := run(Rule{Label: "EMULATOR", Root: devices, Targets: []string{"*.avd/snapshots/*"}}, false)

	assert.NoDirExists(t, filepath.Join(avd, "snapshots", "default_boot"))
	assert.FileExists(t, filepath.Join(avd, "snapshots", "snapshots.pb"), "a file a target matches stays")
	assert.FileExists(t, filepath.Join(avd, "userdata-qemu.img.qcow2"))
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "EMULATOR "+devices+"\n")
}

func TestDryRunRemovesNothing(t *testing.T) {
	root := newCache(t)

	r, _, _ := run(cache(root, idle), true)

	assert.DirExists(t, root)
	assert.Equal(t, 1, r.Count)
}

func TestARunningToolKeepsEverythingAndSaysWhy(t *testing.T) {
	root := newCache(t)
	busy := func(string) (bool, error) { return true, nil }

	r, _, stderr := run(cache(root, busy), false)

	assert.DirExists(t, root)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+root+": a Swift build is running")
}

func TestAFailedProcessCheckKeepsEverythingAndFails(t *testing.T) {
	root := newCache(t)
	broken := func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := run(cache(root, broken), false)

	assert.DirExists(t, root)
	assert.Len(t, r.Failures, 1)
}

func TestALinkedRootLosesOnlyTheLink(t *testing.T) {
	target := newCache(t)
	link := filepath.Join(t.TempDir(), "cache")
	require.NoError(t, os.Symlink(target, link))

	r, _, _ := run(cache(link, idle), false)

	_, err := os.Lstat(link)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.FileExists(t, filepath.Join(target, "repositories", "pkg.git"))
	assert.Equal(t, 1, r.Count)
}

func TestAMissingRootIsQuiet(t *testing.T) {
	r, stdout, stderr := run(cache(filepath.Join(t.TempDir(), "absent"), idle), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
