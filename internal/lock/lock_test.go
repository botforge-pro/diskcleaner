package lock

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestASecondLockWaitsForTheFirstToBeReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	first, err := Take(path, io.Discard)
	require.NoError(t, err)

	_, err = Take(path, io.Discard)
	assert.ErrorIs(t, err, ErrHeld)
	assert.ErrorContains(t, err, path)

	require.NoError(t, first.Release())
	second, err := Take(path, io.Discard)
	require.NoError(t, err)
	require.NoError(t, second.Release())
}

func TestALockFileLeftBehindDoesNotHoldTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	require.NoError(t, os.WriteFile(path, []byte("12345"), 0o644))

	l, err := Take(path, io.Discard)

	require.NoError(t, err)
	require.NoError(t, l.Release())
}

func TestTheShellLockLinkIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	require.NoError(t, os.Symlink("99999", path))

	_, err := Take(path, io.Discard)

	assert.ErrorContains(t, err, "is a link, as the shell version of diskcleaner left its lock")
	assert.NotErrorIs(t, err, ErrHeld)
	_, statErr := os.Lstat(filepath.Join(filepath.Dir(path), "99999"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestADirectoryAtTheLockPathIsRefused(t *testing.T) {
	path := t.TempDir()

	_, err := Take(path, io.Discard)

	assert.ErrorContains(t, err, "cannot open the lock "+path)
	assert.NotErrorIs(t, err, ErrHeld)
}

func TestAFullDiskRunsWithoutTheLock(t *testing.T) {
	openFile = func(string, int, os.FileMode) (*os.File, error) {
		return nil, &os.PathError{Op: "open", Path: "lock", Err: syscall.ENOSPC}
	}
	t.Cleanup(func() { openFile = os.OpenFile })
	var stderr bytes.Buffer

	l, err := Take("/full/lock", &stderr)

	require.NoError(t, err)
	assert.Equal(t, "The disk is full, so the lock /full/lock cannot be made; running without it.\n", stderr.String())
	assert.NoError(t, l.Release())
}
