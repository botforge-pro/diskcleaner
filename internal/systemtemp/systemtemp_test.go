package systemtemp

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

const noneOpen = "../process/testdata/lsof-none-open"

var later = time.Now().Add(4 * 24 * time.Hour)

func newTemp(t *testing.T) string {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func step(temp, lsof string) Step {
	s := Default()
	s.Dir, s.Now, s.Lsof, s.LsofTimeoutSeconds = temp, func() time.Time { return later }, lsof, 5
	return s
}

func makeFile(t *testing.T, path string, touched time.Time) string {
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
	require.NoError(t, os.Chtimes(path, touched, touched))
	return path
}

func fakeLsof(t *testing.T, body string) string {
	path := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return path
}

func run(s Step) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestRemovesFilesMacOSWouldRemoveAtAnyDepth(t *testing.T) {
	temp := newTemp(t)
	top := makeFile(t, filepath.Join(temp, "lexling-inventory.json"), time.Now())
	deep := makeFile(t, filepath.Join(temp, "fm-check", "logs", "build.log"), time.Now())

	r, stdout, stderr := run(step(temp, noneOpen))

	assert.Empty(t, stderr)
	assert.NoFileExists(t, top)
	assert.NoFileExists(t, deep)
	assert.DirExists(t, filepath.Join(temp, "fm-check", "logs"), "removing its file made the folder recent, as macOS leaves it too")
	assert.Equal(t, 2, r.Count)
	assert.Contains(t, stdout, top)
}

func TestKeepsAFileAnyOfWhoseTimesIsRecent(t *testing.T) {
	temp := newTemp(t)
	read := makeFile(t, filepath.Join(temp, "read.json"), time.Now())
	require.NoError(t, os.Chtimes(read, later.Add(-24*time.Hour), time.Now()))
	written := makeFile(t, filepath.Join(temp, "written.json"), time.Now())
	require.NoError(t, os.Chtimes(written, time.Now(), later.Add(-24*time.Hour)))

	r, _, stderr := run(step(temp, noneOpen))

	assert.FileExists(t, read)
	assert.FileExists(t, written)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "2 files")
	assert.Contains(t, stderr, "younger than 3 days")
}

func TestKeepsAFileWhoseStatusChangedRecently(t *testing.T) {
	temp := newTemp(t)
	file := makeFile(t, filepath.Join(temp, "renamed.json"), time.Now().Add(-4*24*time.Hour))
	s := step(temp, noneOpen)
	s.Now = time.Now

	r, _, stderr := run(s)

	assert.FileExists(t, file, "read and written 4 days ago, but its status changed just now")
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "1 file younger than 3 days")
}

func TestKeepsEverythingWhileTheirTimesAreRecent(t *testing.T) {
	temp := newTemp(t)
	fresh := makeFile(t, filepath.Join(temp, "fresh.log"), time.Now())
	s := step(temp, noneOpen)
	s.Now = time.Now

	r, _, _ := run(s)

	assert.FileExists(t, fresh)
	assert.Zero(t, r.Count)
}

func TestRemovesAnEmptyFolderMacOSWouldRemove(t *testing.T) {
	temp := newTemp(t)
	empty := filepath.Join(temp, "fm-favorites-owner-hierarchy", "empty")
	require.NoError(t, os.MkdirAll(empty, 0o755))

	r, _, stderr := run(step(temp, noneOpen))

	assert.Empty(t, stderr)
	assert.NoDirExists(t, empty)
	assert.DirExists(t, filepath.Join(temp, "fm-favorites-owner-hierarchy"), "it was not empty when looked at")
	assert.Equal(t, 1, r.Count)
}

func TestKeepsDotEntriesClaudeFoldersAndWhatMacOSIgnores(t *testing.T) {
	temp := newTemp(t)
	kept := []string{
		makeFile(t, filepath.Join(temp, ".s.PGSQL.5432.lock"), time.Now()),
		makeFile(t, filepath.Join(temp, ".X11-unix", "X0"), time.Now()),
		makeFile(t, filepath.Join(temp, "claude-501", "tasks", "out"), time.Now()),
		makeFile(t, filepath.Join(temp, "share", "quota.user"), time.Now()),
		makeFile(t, filepath.Join(temp, "share", ".X0-lock"), time.Now()),
	}

	r, _, _ := run(step(temp, noneOpen))

	for _, path := range kept {
		assert.FileExists(t, path)
	}
	assert.Zero(t, r.Count)
}

func TestKeepsResourceStreamFoldersMacOSKeeps(t *testing.T) {
	temp := newTemp(t)
	streams := filepath.Join(temp, "normal", ".vfs_rsrc_streams_123")
	require.NoError(t, os.MkdirAll(streams, 0o755))

	r, _, _ := run(step(temp, noneOpen))

	assert.DirExists(t, streams, "tmp_cleaner never removes an empty .vfs_rsrc_streams_* folder")
	assert.Zero(t, r.Count)
}

func TestKeepsSocketsAndTheirFolders(t *testing.T) {
	temp, err := os.MkdirTemp("/tmp", "systemtemp")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(temp) })
	socket := filepath.Join(temp, "cc-socks", "s")
	require.NoError(t, os.MkdirAll(filepath.Dir(socket), 0o755))
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	defer listener.Close()

	r, _, _ := run(step(temp, noneOpen))

	_, err = os.Lstat(socket)
	assert.NoError(t, err)
	assert.Zero(t, r.Count)
}

func TestNeverLooksIntoAMountedVolume(t *testing.T) {
	temp := newTemp(t)
	volume := filepath.Join(temp, "dmg.cPCLVT")
	inside := makeFile(t, filepath.Join(volume, "BuildManifest.plist"), time.Now())
	s := step(temp, noneOpen)
	s.Mounts = func() (map[string]bool, error) { return map[string]bool{volume: true}, nil }

	r, _, stderr := run(s)

	assert.FileExists(t, inside, "a stalled file system would hang the walk, and its files are not space on this disk")
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, volume+": a mounted volume")
}

func TestKeepsWhatChangedWhileLsofRan(t *testing.T) {
	temp := newTemp(t)
	folder := filepath.Join(temp, "fm-check")
	require.NoError(t, os.MkdirAll(folder, 0o755))
	arrived := filepath.Join(folder, "new.log")
	used := makeFile(t, filepath.Join(temp, "used.json"), time.Now())
	stamp := later.Format("200601021504")
	lsof := fakeLsof(t, "echo x > '"+arrived+"'\ntouch -t "+stamp+" '"+used+"'\nprintf 'p1\\nn/elsewhere\\n'\n")
	s := step(temp, lsof)

	r, _, stderr := run(s)

	assert.FileExists(t, arrived, "a file written into the folder after it was found empty")
	assert.FileExists(t, used, "a file used after it was found old")
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, folder+": no longer empty")
	assert.Contains(t, stderr, used+": used since it was found")
}

func TestNeverRemovesOutsideWhenAFolderBecomesALink(t *testing.T) {
	temp := newTemp(t)
	sub := filepath.Join(temp, "sub")
	makeFile(t, filepath.Join(sub, "data.json"), time.Now())
	outside := makeFile(t, filepath.Join(newTemp(t), "data.json"), time.Now())
	lsof := fakeLsof(t, "mv '"+sub+"' '"+sub+".moved'\nln -s '"+filepath.Dir(outside)+"' '"+sub+"'\nprintf 'p1\\nn/elsewhere\\n'\n")

	r, _, _ := run(step(temp, lsof))

	assert.FileExists(t, outside, "a file reached through a link that replaced a folder is not in the temporary folder")
	assert.FileExists(t, filepath.Join(sub+".moved", "data.json"), "the moved file was not looked at under its new name")
	assert.Zero(t, r.Count)
}

func TestNeverRemovesFromAKeptFolderReachedThroughALink(t *testing.T) {
	temp := newTemp(t)
	sub := filepath.Join(temp, "sub")
	makeFile(t, filepath.Join(sub, "data.json"), time.Now())
	kept := makeFile(t, filepath.Join(temp, "claude-501", "data.json"), time.Now())
	lsof := fakeLsof(t, "mv '"+sub+"' '"+sub+".moved'\nln -s claude-501 '"+sub+"'\nprintf 'p1\\nn/elsewhere\\n'\n")

	r, _, stderr := run(step(temp, lsof))

	assert.FileExists(t, kept, "the step never removes from claude-* folders, however it is led there")
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, filepath.Join(sub, "data.json")+": changed since it was found")
}

func TestDryRunRemovesNothing(t *testing.T) {
	temp := newTemp(t)
	file := makeFile(t, filepath.Join(temp, "old.json"), time.Now())
	folder := filepath.Join(temp, "empty")
	require.NoError(t, os.MkdirAll(folder, 0o755))
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: true}

	step(temp, noneOpen).Run(r)

	assert.FileExists(t, file)
	assert.DirExists(t, folder)
	assert.Equal(t, 2, r.Count)
	assert.Contains(t, stdout.String(), "WOULD REMOVE")
}

func TestKeepsWhatAVolumeMountedOverWhileLsofRan(t *testing.T) {
	temp := newTemp(t)
	sub := filepath.Join(temp, "sub")
	file := makeFile(t, filepath.Join(sub, "data.json"), time.Now())
	s := step(temp, noneOpen)
	calls := 0
	s.Mounts = func() (map[string]bool, error) {
		calls++
		return map[string]bool{sub: calls > 1}, nil
	}

	r, _, stderr := run(s)

	assert.FileExists(t, file, "a volume mounted over its folder after the walk")
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, file+": changed since it was found: a mounted volume is on its way")
}

func TestKeepsAFileAProcessHoldsOpen(t *testing.T) {
	temp := newTemp(t)
	open := makeFile(t, filepath.Join(temp, "held.db"), time.Now())
	closed := makeFile(t, filepath.Join(temp, "done.db"), time.Now())

	r, _, stderr := run(step(temp, fakeLsof(t, "printf 'p42\\nn"+open+"\\n'\n")))

	assert.FileExists(t, open)
	assert.NoFileExists(t, closed)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stderr, open+": a process has it open")
}

func TestRemovesNothingWhenItCannotTellWhatIsOpen(t *testing.T) {
	temp := newTemp(t)
	file := makeFile(t, filepath.Join(temp, "old.json"), time.Now())

	r, _, stderr := run(step(temp, fakeLsof(t, "echo 'lsof: broken' >&2\nexit 1\n")))

	assert.FileExists(t, file)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "FAILED")
	assert.Contains(t, stderr, "lsof")
}

func TestKeepsAnotherUsersFiles(t *testing.T) {
	temp := newTemp(t)
	file := makeFile(t, filepath.Join(temp, "theirs.json"), time.Now())
	s := step(temp, noneOpen)
	s.Owner = os.Getuid() + 1

	r, _, _ := run(s)

	assert.FileExists(t, file)
	assert.Zero(t, r.Count)
}

func TestLooksInTheSharedTemporaryFolderAsTheRunningUser(t *testing.T) {
	s := Default()

	assert.Equal(t, "/private/tmp", s.Dir)
	assert.Equal(t, os.Getuid(), s.Owner)
	mounts, err := s.Mounts()
	require.NoError(t, err)
	assert.True(t, mounts["/"], "the mount table lists the root volume")
}
