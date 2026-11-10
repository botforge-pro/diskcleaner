package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalResolvesRelativePathsAndLinks(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "real"), 0o755))
	require.NoError(t, os.Symlink("real", filepath.Join(dir, "link")))
	t.Chdir(dir)

	for _, path := range []string{"real", "./link", filepath.Join(dir, "link")} {
		resolved, err := Canonical(path)
		require.NoError(t, err, path)
		assert.Equal(t, filepath.Join(dir, "real"), resolved, path)
	}
}

func TestCanonicalOfAMissingPathIsAbsolute(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Chdir(dir)

	resolved, err := Canonical("absent")

	assert.Error(t, err)
	assert.Equal(t, filepath.Join(dir, "absent"), resolved)
}
