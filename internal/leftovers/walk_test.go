package leftovers

import (
	"io/fs"
	"path/filepath"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type vanishing struct {
	fstest.MapFS
	gone string
}

func (v vanishing) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == v.gone {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return v.MapFS.ReadDir(name)
}

func TestAFolderRemovedDuringTheWalkIsNotAFailure(t *testing.T) {
	fsys := vanishing{
		MapFS: fstest.MapFS{
			"b001/a.out":     {Data: []byte("a")},
			"b002/_pkg_.a":   {Data: []byte("b")},
			"b002/importcfg": {Data: []byte("c")},
		},
		gone: "b002",
	}

	_, err := walkFS(fsys, "/tmp/go-build1", 1)

	require.NoError(t, err, "a go command removing its own folder mid-walk leaves nothing to remove")

	_, err = walk(filepath.Join(t.TempDir(), "gone"), 1)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestAMountedDiskImageIsNotSpaceToFree(t *testing.T) {
	image := &syscall.Stat_t{Dev: 7, Blocks: 2 << 12}
	fsys := fstest.MapFS{
		"Restore":               {Mode: fs.ModeDir, Sys: image},
		"Restore/BuildManifest": {Data: make([]byte, 8192), Sys: image},
	}

	found, err := walkFS(fsys, "/private/tmp/dmg.cPCLVT", 1)

	require.NoError(t, err)
	assert.Equal(t, "/private/tmp/dmg.cPCLVT/Restore", found.mounted, "removing it would empty the image or fail on a read-only one")
}
