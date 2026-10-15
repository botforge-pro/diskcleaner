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

func TestRemoveCountsAPathInsideARemovedOneOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".build")
	inner := filepath.Join(dir, "pkg", "node_modules")
	require.NoError(t, os.MkdirAll(inner, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(inner, "entry"), make([]byte, 8192), 0o644))
	sibling := dir + "-other"
	require.NoError(t, os.MkdirAll(sibling, 0o755))
	var stdout, stderr bytes.Buffer
	r := &Report{Out: &stdout, Err: &stderr, DryRun: true}

	r.Remove(dir)
	kb := r.KB
	r.Remove(inner)
	r.Remove(dir)
	r.Remove(sibling)

	siblingKB, err := DiskUsageKB(sibling)
	require.NoError(t, err)
	assert.Equal(t, 2, r.Count)
	assert.Equal(t, kb+siblingKB, r.KB)
	assert.Equal(t, 2, bytes.Count(stdout.Bytes(), []byte("WOULD REMOVE")))
	assert.Empty(t, stderr.String())
}

func TestRemoveKnowsARelativeSpellingOfARemovedPath(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "repos", "app", ".build", "cache"), 0o755))
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	r := &Report{Out: &stdout, Err: &stderr, DryRun: true}

	r.Remove(filepath.Join(dir, "repos", "app", ".build"))
	r.Remove("./repos/app/.build")
	r.Remove("repos/other/../app/.build/cache")

	assert.Equal(t, 1, r.Count)
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
