package xcode

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

func newDerivedData(t *testing.T) string {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	derivedData := filepath.Join(root, "DerivedData")
	for _, file := range []string{"App-123/Build/App.o", "ModuleCache.noindex/Foundation.pcm"} {
		path := filepath.Join(derivedData, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(derivedData, "info.plist"), []byte("x"), 0o644))
	return derivedData
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func idle(string) (bool, error) { return false, nil }

func TestRemovesEachDirectory(t *testing.T) {
	derivedData := newDerivedData(t)

	r, stdout, _ := run(Step{DerivedData: derivedData, Processes: "xcode", Running: idle}, false)

	assert.NoDirExists(t, filepath.Join(derivedData, "App-123"))
	assert.NoDirExists(t, filepath.Join(derivedData, "ModuleCache.noindex"))
	assert.FileExists(t, filepath.Join(derivedData, "info.plist"))
	assert.Equal(t, 2, r.Count)
	assert.Contains(t, stdout, "XCODE    "+derivedData+"\n")
}

func TestDryRunRemovesNothing(t *testing.T) {
	derivedData := newDerivedData(t)

	r, _, _ := run(Step{DerivedData: derivedData, Processes: "xcode", Running: idle}, true)

	assert.DirExists(t, filepath.Join(derivedData, "App-123"))
	assert.Equal(t, 2, r.Count)
}

func TestSkipsWhileXcodeRuns(t *testing.T) {
	derivedData := newDerivedData(t)
	busy := func(string) (bool, error) { return true, nil }

	r, _, stderr := run(Step{DerivedData: derivedData, Processes: "xcode", Running: busy}, false)

	assert.DirExists(t, filepath.Join(derivedData, "App-123"))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+derivedData+": Xcode or an Xcode build is running")
}

func TestProcessCheckErrorSkipsAndFails(t *testing.T) {
	derivedData := newDerivedData(t)
	broken := func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := run(Step{DerivedData: derivedData, Processes: "xcode", Running: broken}, false)

	assert.DirExists(t, filepath.Join(derivedData, "App-123"))
	assert.Len(t, r.Failures, 1)
}

func TestFollowsSymlinkedDerivedData(t *testing.T) {
	derivedData := newDerivedData(t)
	link := filepath.Join(t.TempDir(), "DerivedData")
	require.NoError(t, os.Symlink(derivedData, link))

	r, _, _ := run(Step{DerivedData: link, Processes: "xcode", Running: idle}, true)

	assert.Equal(t, 2, r.Count)
}

func TestKeepsSymlinksInsideDerivedData(t *testing.T) {
	derivedData := newDerivedData(t)
	elsewhere := filepath.Join(t.TempDir(), "kept")
	require.NoError(t, os.MkdirAll(elsewhere, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "work"), []byte("x"), 0o644))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(derivedData, "Linked")))

	r, _, _ := run(Step{DerivedData: derivedData, Processes: "xcode", Running: idle}, false)

	assert.FileExists(t, filepath.Join(elsewhere, "work"))
	_, err := os.Lstat(filepath.Join(derivedData, "Linked"))
	assert.NoError(t, err)
	assert.Equal(t, 2, r.Count)
}

func TestMissingDerivedDataIsQuiet(t *testing.T) {
	r, stdout, stderr := run(Step{DerivedData: filepath.Join(t.TempDir(), "absent"), Processes: "xcode", Running: idle}, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
