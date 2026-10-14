package report

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveCountsWhatItRemoved(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "entry"), make([]byte, 8192), 0o644))
	var stdout, stderr bytes.Buffer
	r := &Report{Out: &stdout, Err: &stderr}

	r.Remove(dir)

	assert.NoDirExists(t, dir)
	assert.Equal(t, 1, r.Count)
	assert.GreaterOrEqual(t, r.KB, int64(8))
	assert.Regexp(t, `^REMOVE {8}[0-9.]+ KiB  `, stdout.String())
	assert.Empty(t, stderr.String())
}

func TestRemoveReportsWhatItCouldNotRemove(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "locked")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "entry"), []byte("x"), 0o644))
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	var stdout, stderr bytes.Buffer
	r := &Report{Out: &stdout, Err: &stderr}

	r.Remove(dir)

	assert.DirExists(t, dir)
	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, stderr.String(), "FAILED        rm "+dir)
}
