package rule

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/inuse"
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

func TestAFolderRemovedDuringTheInspectionIsNotAFailure(t *testing.T) {
	fsys := vanishing{
		MapFS: fstest.MapFS{
			"b001/a.out":     {Data: []byte("a")},
			"b002/_pkg_.a":   {Data: []byte("b")},
			"b002/importcfg": {Data: []byte("c")},
		},
		gone: "b002",
	}

	_, err := inspectFS(fsys, "/tmp/go-build1", 1)

	require.NoError(t, err, "a go command removing its own folder mid-walk leaves nothing to remove")
	_, err = inspect(filepath.Join(t.TempDir(), "gone"), 1)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestAMountedDiskImageIsNotSpaceToFree(t *testing.T) {
	image := &syscall.Stat_t{Dev: 7, Blocks: 2 << 12}
	fsys := fstest.MapFS{
		"Restore":               {Mode: fs.ModeDir, Sys: image},
		"Restore/BuildManifest": {Data: make([]byte, 8192), Sys: image},
	}

	found, err := inspectFS(fsys, "/private/tmp/dmg.cPCLVT", 1)

	require.NoError(t, err)
	assert.Equal(t, "/private/tmp/dmg.cPCLVT/Restore", found.mounted, "removing it would empty the image or fail on a read-only one")
}

func leftovers(t *testing.T, temp, lsof string) Rule {
	env := environment(idle)
	env.Duration = func(string) time.Duration { return time.Hour }
	env.Lsof = inuse.Lsof{Command: lsof, TimeoutSeconds: 5}
	body := fmt.Sprintf(`order: 1
describes: x
root: %s
remove: {folders: ['go-build*']}
keep: {mounts: true, sockets: true, age: {of: newest-write, within: min-age}, open: true}
`, temp)
	return parse(t, "temp-leftovers.yaml", body).Rule(env)
}

func oldFolder(t *testing.T, path string) {
	require.NoError(t, os.MkdirAll(path, 0o755))
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
}

func TestAFolderHoldingASocketStays(t *testing.T) {
	temp, err := os.MkdirTemp("/tmp", "dc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(temp) })
	build := filepath.Join(temp, "go-build1")
	oldFolder(t, build)
	listener, err := net.Listen("unix", filepath.Join(build, "s"))
	require.NoError(t, err)
	defer listener.Close()
	socket := filepath.Join(build, "s")
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(build, old, old))

	r, _, stderr := run(leftovers(t, temp, "../process/testdata/lsof-none-open"), false)

	assert.DirExists(t, build)
	assert.Zero(t, r.Count)
	real, err := filepath.EvalSymlinks(socket)
	require.NoError(t, err)
	assert.Contains(t, stderr, "holds the socket")
	assert.Contains(t, stderr, filepath.Base(real))
}

func TestAFailedLsofKeepsTheFolderAndFails(t *testing.T) {
	temp, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	build := filepath.Join(temp, "go-build1")
	oldFolder(t, build)

	r, _, _ := run(leftovers(t, temp, fakeLsof(t, "echo 'lsof: broken' >&2\nexit 2\n")), false)

	assert.DirExists(t, build)
	assert.Zero(t, r.Count)
	assert.Len(t, r.Failures, 1)
}

func TestRealLsofSeesAnOpenProgramInACopy(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	clone := filepath.Join(dir, "com.google.Chrome.code_sign_clone", "code_sign_clone.open")
	program := filepath.Join(clone, "Google Chrome.app.bundle/Contents/MacOS/Google Chrome")
	require.NoError(t, os.MkdirAll(filepath.Dir(program), 0o755))
	require.NoError(t, os.WriteFile(program, make([]byte, 8192), 0o755))
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(clone, old, old))
	file, err := os.Open(program)
	require.NoError(t, err)
	defer file.Close()
	env := environment(idle)
	env.Duration = func(string) time.Duration { return time.Hour }
	env.Lsof = inuse.DefaultLsof()
	s := parse(t, "code-sign-clones.yaml", fmt.Sprintf(`order: 1
describes: x
root: %s
remove: {folders: ['*.code_sign_clone/code_sign_clone.*']}
keep: {age: {of: mtime, within: min-age}, open: ['*/Contents/MacOS/*']}
`, dir)).Rule(env)

	r, _, stderr := run(s, false)

	assert.DirExists(t, clone)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+clone+": a running program was started from it")
}
