package report

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newReport(dryRun bool) (*Report, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return &Report{Out: &stdout, Err: &stderr, DryRun: dryRun}, &stdout, &stderr
}

func TestRemoveEntryCountsWhatItRemoved(t *testing.T) {
	file := filepath.Join(t.TempDir(), "entry")
	require.NoError(t, os.WriteFile(file, make([]byte, 8192), 0o644))
	r, stdout, stderr := newReport(false)

	r.RemoveEntry(file, 8192, func() error { return os.Remove(file) })

	assert.NoFileExists(t, file)
	assert.Equal(t, 1, r.Count)
	assert.EqualValues(t, 8, r.KB)
	assert.Equal(t, "REMOVE        8.0 KiB  "+file+"\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestADryRunCountsAPathInsideACountedOneOnce(t *testing.T) {
	r, stdout, _ := newReport(true)
	removes := func() error {
		t.Fatal("a dry run removed")
		return nil
	}

	r.RemoveEntry("/a/.build", 4096, removes)
	r.RemoveEntry("/a/.build/pkg/node_modules", 1024, removes)
	r.RemoveEntry("/a/.build-other", 1024, removes)

	assert.Equal(t, 2, r.Count)
	assert.EqualValues(t, 5, r.KB)
	assert.Equal(t, 2, bytes.Count(stdout.Bytes(), []byte("WOULD REMOVE")))
}

func TestAPathThatFailedToGoIsNotTakenAsGone(t *testing.T) {
	r, _, stderr := newReport(false)
	dir := t.TempDir()
	inner := filepath.Join(dir, "inner")
	require.NoError(t, os.WriteFile(inner, []byte("x"), 0o644))

	r.RemoveEntry(dir, 4096, func() error { return errors.New("permission denied") })
	r.RemoveEntry(inner, 1024, func() error { return os.Remove(inner) })

	assert.NoFileExists(t, inner, "what lies inside a folder that did not go is still removed")
	assert.Equal(t, 1, r.Count)
	assert.Equal(t, []string{"rm " + dir + ": permission denied"}, r.Failures)
	assert.Contains(t, stderr.String(), "FAILED        rm "+dir)
}
