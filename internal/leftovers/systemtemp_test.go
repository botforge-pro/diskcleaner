package leftovers

import (
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const bigEnough = 2 << 20

func systemStep(t *testing.T, temp, lsof string) Step {
	s, err := SystemTemp()
	require.NoError(t, err)
	s.Dir, s.Now, s.Lsof, s.LsofTimeoutSeconds = temp, func() time.Time { return now }, lsof, 5
	return s
}

func makeFile(t *testing.T, temp, name string, size int, age time.Duration) string {
	path := filepath.Join(temp, name)
	require.NoError(t, os.WriteFile(path, make([]byte, size), 0o644))
	require.NoError(t, os.Chtimes(path, now.Add(-age), now.Add(-age)))
	return path
}

func makeBigDir(t *testing.T, temp, name string, age time.Duration) string {
	path := filepath.Join(temp, name)
	require.NoError(t, os.MkdirAll(path, 0o755))
	makeFile(t, path, "copy.db", bigEnough, age)
	backdate(t, path, age)
	return path
}

func TestSystemTempKeepsTheLockFilesOfRunningServices(t *testing.T) {
	temp := newTemp(t)
	postgres := makeFile(t, temp, ".s.PGSQL.5432.lock", bigEnough, 2*time.Hour)
	firefox := makeFile(t, temp, "MozillaUpdateLock-2656FF1E876E9973", 0, 2*time.Hour)

	r, _, stderr := run(systemStep(t, temp, noneOpen), false)

	assert.FileExists(t, postgres, "PostgreSQL holds this lock without keeping it open and shuts down when it disappears")
	assert.FileExists(t, firefox, "a lock nobody holds open frees nothing when removed")
	assert.Zero(t, r.Count)
	assert.Empty(t, stderr)
}

func TestSystemTempRemovesWhatAgentsLeftInIt(t *testing.T) {
	temp := newTemp(t)
	dump := makeFile(t, temp, "lexling-marks-inventory.json", bigEnough, 2*time.Hour)
	copied := makeBigDir(t, temp, "lexling-phone-1957", 3*time.Hour)

	r, stdout, stderr := run(systemStep(t, temp, noneOpen), false)

	assert.Empty(t, stderr)
	assert.NoFileExists(t, dump)
	assert.NoDirExists(t, copied)
	assert.Equal(t, 2, r.Count)
	assert.Contains(t, stdout, "TEMP     "+temp+"\n")
}

func TestSystemTempKeepsWhatIsFreshOpenExcludedOrAnotherUsers(t *testing.T) {
	temp := newTemp(t)
	fresh := makeFile(t, temp, "notes.txt", bigEnough, 10*time.Minute)
	sessions := makeBigDir(t, temp, "claude-501", 30*24*time.Hour)
	open := makeFile(t, temp, "server.log", bigEnough, 2*time.Hour)
	file, err := os.Open(open)
	require.NoError(t, err)
	defer file.Close()
	target := makeBigDir(t, newTemp(t), "work", 2*time.Hour)
	link := filepath.Join(temp, "work-link")
	require.NoError(t, os.Symlink(target, link))

	r, _, stderr := run(systemStep(t, temp, "lsof"), false)

	assert.FileExists(t, fresh)
	assert.DirExists(t, sessions, "Claude Code sessions have a step of their own")
	assert.FileExists(t, open)
	_, err = os.Lstat(link)
	assert.NoError(t, err)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+fresh+": written within 60 minutes")
	assert.Contains(t, stderr, "SKIPPED       "+open+": a process still has something in it open")

	s := systemStep(t, temp, noneOpen)
	s.Owner = new(os.Getuid() + 1)
	r, _, _ = run(s, false)

	assert.FileExists(t, open, "a file of another user stays")
	assert.Zero(t, r.Count)
}

func TestSystemTempKeepsAFolderWithASocketInIt(t *testing.T) {
	temp, err := os.MkdirTemp("", "dc")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(temp)) })
	temp, err = filepath.EvalSymlinks(temp)
	require.NoError(t, err)
	sockets := makeBigDir(t, temp, "cc-socks", 2*time.Hour)
	listener, err := net.Listen("unix", filepath.Join(sockets, "1.sock"))
	require.NoError(t, err)
	defer listener.Close()
	backdate(t, sockets, 2*time.Hour)

	r, _, stderr := run(systemStep(t, temp, noneOpen), false)

	assert.DirExists(t, sockets, "a session listens on a socket lsof may not find under another spelling of the path")
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+sockets+": holds the socket "+filepath.Join(sockets, "1.sock"))
}

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

	found, err := walkFS(fsys, "/tmp/go-build1")

	require.NoError(t, err, "a go command removing its own folder mid-walk leaves nothing to remove")
	assert.Contains(t, found.entries, "/tmp/go-build1/b001/a.out")
	assert.NotContains(t, found.entries, "/tmp/go-build1/b002/importcfg")

	_, err = walk(filepath.Join(t.TempDir(), "gone"))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestSystemTempLooksInTheSharedTemporaryFolder(t *testing.T) {
	t.Setenv("DISKCLEANER_SYSTEM_TMP", "")

	s, err := SystemTemp()

	require.NoError(t, err)
	assert.Equal(t, "/private/tmp", s.Dir)
	require.NotNil(t, s.Owner)
	assert.Equal(t, os.Getuid(), *s.Owner)
	assert.True(t, s.Files)
	assert.Equal(t, int64(1024), s.MinKB)
}
