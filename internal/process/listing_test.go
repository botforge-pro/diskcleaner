package process

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeLsof(t *testing.T, body string) string {
	path := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return path
}

func TestHoldsMatchesThePathAndWhatIsInsideIt(t *testing.T) {
	names := []string{"/", "/work/.build-old/a.o", "/work"}

	assert.True(t, Holds(names, "/work"))
	assert.True(t, Holds([]string{"/work/.build/debug/a.o"}, "/work/.build"))
	assert.False(t, Holds(names, "/work/.build"), "a sibling sharing the prefix is not inside it")
	assert.False(t, Holds(names, "/work/x"))
}

func TestOpenNamesListsEveryNameLsofPrints(t *testing.T) {
	lsof := fakeLsof(t, "printf 'p42\\nfcwd\\nn/work\\nf3\\nn/work/a.o\\np43\\nf4\\nn->0x1234\\n'\nexit 0\n")

	names, err := OpenNames(5, lsof)

	require.NoError(t, err)
	assert.Equal(t, []string{"/work", "/work/a.o", "->0x1234"}, names)
}

func TestOpenNamesWithRealLsofSeesAnOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "open")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	names, err := OpenNames(30, "lsof")

	require.NoError(t, err)
	assert.True(t, slices.Contains(names, resolved))
}

func TestOpenNamesFails(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		timeout int
		fail    string
	}{
		{"error", "echo 'lsof: broken' >&2\nexit 1\n", 5, "exit 1: lsof: broken"},
		{"warned", "printf 'p42\\nf3\\nn/a\\n'\necho 'lsof: WARNING: can not stat() fuse file system' >&2\n", 5, "warned: lsof: WARNING: can not stat() fuse file system"},
		{"nothing listed", "exit 0\n", 5, "listed no open files at all"},
		{"hung", "exec sleep 30\n", 1, "no answer within 1s"},
		{"child left behind", "sleep 30 >/dev/null 2>&1 &\nprintf 'n/a\\n'\n", 5, "exited but left a process running"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names, err := OpenNames(tc.timeout, fakeLsof(t, tc.body))

			assert.Nil(t, names)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.fail)
		})
	}
}
