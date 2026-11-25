package rule

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnUnreadableFolderInADeepSearchFailsAndTheRestGoes(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	found := filepath.Join(root, "a", "scratchpad", "x", ".build")
	require.NoError(t, os.MkdirAll(found, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "scratchpad", "x", "Package.swift"), []byte("x"), 0o644))
	locked := filepath.Join(root, "locked")
	require.NoError(t, os.MkdirAll(filepath.Join(locked, "scratchpad", "y", ".build"), 0o755))
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	s := parse(t, "claude-scratchpads.yaml", fmt.Sprintf("order: 1\ndescribes: x\nroot: %s\nremove: {folders: [{glob: '**/scratchpad/**/.build', beside: [Package.swift]}]}\n", root)).Rule(environment(idle))

	r, _, _ := run(s, false)

	assert.NoDirExists(t, found)
	assert.Equal(t, 1, r.Count)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "find "+root)
}

func TestAPatternSearchNamesAnUnreadableFolderAndFindsTheRest(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for _, path := range []string{"a/.git/objects/tmp_obj", "b/.git/objects/pack/tmp_pack"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("x"), 0o644))
	}
	locked := filepath.Join(root, "b", ".git", "objects", "pack")
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	found, err := expandGlob(root, "*/.git/objects/*/tmp_*")
	foundTop, errTop := expandGlob(root, "*/.git/objects/tmp_*")

	assert.Empty(t, found)
	require.Error(t, err)
	assert.Contains(t, err.Error(), locked)
	assert.Equal(t, []string{filepath.Join(root, "a/.git/objects/tmp_obj")}, foundTop)
	assert.NoError(t, errTop)
}

func TestADeepPatternMatchesAnyDepthAndNoMore(t *testing.T) {
	pattern := []string{"data", "**", "Library", "Caches", "**", "cache"}

	assert.True(t, matchDeep(pattern, []string{"data", "Library", "Caches", "cache"}))
	assert.True(t, matchDeep(pattern, []string{"data", "Containers", "App", "Library", "Caches", "com.x", "cache"}))
	assert.False(t, matchDeep(pattern, []string{"data", "Documents", "cache"}))
	assert.False(t, matchDeep(pattern, []string{"other", "Library", "Caches", "cache"}))
}
