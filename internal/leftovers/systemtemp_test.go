package leftovers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func systemStep(temp, lsof string) Step {
	s := step(temp, lsof)
	s.Patterns = []string{"*"}
	s.Files = true
	s.Exclude = []string{".*", "claude-*"}
	s.Owner = new(os.Getuid())
	return s
}

func TestSystemTempKeepsTheLockFilesOfRunningServices(t *testing.T) {
	temp := newTemp(t)
	lock := makeFile(t, temp, ".s.PGSQL.5432.lock", 2*time.Hour)

	s, err := SystemTemp()
	require.NoError(t, err)
	s.Dir, s.Now, s.Lsof = temp, func() time.Time { return now }, noneOpen
	r, _, _ := run(s, false)

	assert.FileExists(t, lock, "PostgreSQL holds this lock without keeping it open and shuts down when it disappears")
	assert.Zero(t, r.Count)
}

func makeFile(t *testing.T, temp, name string, age time.Duration) string {
	path := filepath.Join(temp, name)
	require.NoError(t, os.WriteFile(path, make([]byte, 8192), 0o644))
	require.NoError(t, os.Chtimes(path, now.Add(-age), now.Add(-age)))
	return path
}

func TestSystemTempRemovesWhatAgentsLeftInIt(t *testing.T) {
	temp := newTemp(t)
	dump := makeFile(t, temp, "lexling-marks-inventory.json", 2*time.Hour)
	copied := makeDir(t, temp, "lexling-phone-1957", 3*time.Hour)

	r, stdout, stderr := run(systemStep(temp, noneOpen), false)

	assert.Empty(t, stderr)
	assert.NoFileExists(t, dump)
	assert.NoDirExists(t, copied)
	assert.Equal(t, 2, r.Count)
	assert.Contains(t, stdout, "TEMP     "+temp+"\n")
}

func TestSystemTempKeepsWhatIsFreshOpenExcludedOrAnotherUsers(t *testing.T) {
	temp := newTemp(t)
	fresh := makeFile(t, temp, "notes.txt", 10*time.Minute)
	sessions := makeDir(t, temp, "claude-501", 30*24*time.Hour)
	open := makeFile(t, temp, "server.log", 2*time.Hour)
	file, err := os.Open(open)
	require.NoError(t, err)
	defer file.Close()
	target := makeDir(t, newTemp(t), "work", 2*time.Hour)
	link := filepath.Join(temp, "work-link")
	require.NoError(t, os.Symlink(target, link))

	r, _, stderr := run(systemStep(temp, "lsof"), false)

	assert.FileExists(t, fresh)
	assert.DirExists(t, sessions, "Claude Code sessions have a step of their own")
	assert.FileExists(t, open)
	_, err = os.Lstat(link)
	assert.NoError(t, err)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+fresh+": written within 60 minutes")
	assert.Contains(t, stderr, "SKIPPED       "+open+": a process still has something in it open")

	s := systemStep(temp, noneOpen)
	s.Owner = new(os.Getuid() + 1)
	r, _, _ = run(s, false)

	assert.FileExists(t, open, "a file of another user stays")
	assert.Zero(t, r.Count)
}

func TestSystemTempLooksInTheSharedTemporaryFolder(t *testing.T) {
	t.Setenv("DISKCLEANER_SYSTEM_TMP", "")

	s, err := SystemTemp()

	require.NoError(t, err)
	assert.Equal(t, "/private/tmp", s.Dir)
	require.NotNil(t, s.Owner)
	assert.Equal(t, os.Getuid(), *s.Owner)
	assert.True(t, s.Files)
}
