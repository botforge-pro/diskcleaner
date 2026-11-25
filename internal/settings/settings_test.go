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

func TestEachPartTakesTheCaseItHasOnDisk(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "Developer", "App"), 0o755))

	cased, err := diskCase(filepath.Join(dir, "developer", "APP"))

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "Developer", "App"), cased, "lsof prints the case on disk, so paths compared with it need it too")
}

func TestAnExactNameWinsOverOneThatDiffersOnlyInCase(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "Logs"), 0o755))
	if err := os.Mkdir(filepath.Join(dir, "logs"), 0o755); os.IsExist(err) {
		cased, err := diskCase(filepath.Join(dir, "LOGS"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, "Logs"), cased, "on a case-insensitive disk the one folder answers to both")
		return
	}

	cased, err := diskCase(filepath.Join(dir, "logs"))

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "logs"), cased)
}

func TestCanonicalOfAMissingPathIsAbsolute(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Chdir(dir)

	resolved, err := Canonical("absent")

	assert.Error(t, err)
	assert.Equal(t, filepath.Join(dir, "absent"), resolved)
}
