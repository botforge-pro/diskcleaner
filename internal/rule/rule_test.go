package rule

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/inuse"
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

func resolved(t *testing.T, path string) string {
	real, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return real
}

func environment(running func(string) (bool, error)) Environment {
	return Environment{
		Home:      "/nowhere",
		Getenv:    func(string) string { return "" },
		Processes: Processes{Running: running},
		Duration:  func(string) time.Duration { return 0 },
		Now:       time.Now,
		Lsof:      inuse.Lsof{Command: "../process/testdata/lsof-none-open", TimeoutSeconds: 5},
	}
}

func cache(t *testing.T, root string, running func(string) (bool, error)) Rule {
	body := fmt.Sprintf("order: 1\ndescribes: x\nroot: %s\nbusy: {processes: [swift], says: a Swift build is running}\nremove: root\n", root)
	return parse(t, "swiftpm-cache.yaml", body).Rule(environment(running))
}

func TestRemoveRootTakesItWhole(t *testing.T) {
	root := newCache(t)

	r, stdout, _ := run(cache(t, root, idle), false)

	assert.NoDirExists(t, root)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "SWIFTPM  "+resolved(t, filepath.Dir(root))+"/cache\n")
}

func TestFolderPatternsRemoveTheFoldersTheyMatchAndNothingElse(t *testing.T) {
	devices, avd := newDevices(t)
	body := fmt.Sprintf("order: 1\ndescribes: x\nroot: %s\nremove: {folders: ['*.avd/snapshots/*']}\n", devices)

	r, stdout, _ := run(parse(t, "emulator-snapshots.yaml", body).Rule(environment(idle)), false)

	assert.NoDirExists(t, filepath.Join(avd, "snapshots", "default_boot"))
	assert.FileExists(t, filepath.Join(avd, "snapshots", "snapshots.pb"), "a file a folder pattern matches stays")
	assert.FileExists(t, filepath.Join(avd, "userdata-qemu.img.qcow2"))
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "EMULATOR "+resolved(t, devices)+"\n")
}

func TestDryRunRemovesNothing(t *testing.T) {
	root := newCache(t)

	r, _, _ := run(cache(t, root, idle), true)

	assert.DirExists(t, root)
	assert.Equal(t, 1, r.Count)
}

func TestARunningToolKeepsEverythingAndSaysWhy(t *testing.T) {
	root := newCache(t)
	busy := func(string) (bool, error) { return true, nil }

	r, _, stderr := run(cache(t, root, busy), false)

	assert.DirExists(t, root)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+root+": a Swift build is running")
}

func TestAFailedProcessCheckKeepsEverythingAndFails(t *testing.T) {
	root := newCache(t)
	broken := func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := run(cache(t, root, broken), false)

	assert.DirExists(t, root)
	assert.Len(t, r.Failures, 1)
}

func TestALinkedRootLosesOnlyTheLink(t *testing.T) {
	target := newCache(t)
	link := filepath.Join(t.TempDir(), "cache")
	require.NoError(t, os.Symlink(target, link))

	r, _, _ := run(cache(t, link, idle), false)

	_, err := os.Lstat(link)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.FileExists(t, filepath.Join(target, "repositories", "pkg.git"))
	assert.Equal(t, 1, r.Count)
}

func TestAMissingRootIsQuiet(t *testing.T) {
	r, stdout, stderr := run(cache(t, filepath.Join(t.TempDir(), "absent"), idle), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
