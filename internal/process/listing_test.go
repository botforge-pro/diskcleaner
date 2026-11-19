package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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
	usePs(t, "exit 0\n")
	lsof := fakeLsof(t, "printf 'p42\\nfcwd\\nn/work\\nf3\\nn/work/a.o\\np43\\nf4\\nn->0x1234\\n'\nexit 0\n")

	names, err := OpenNames(5, lsof)

	require.NoError(t, err)
	assert.Equal(t, []string{"/work", "/work/a.o", "->0x1234"}, names)
}

func usePs(t *testing.T, body string) {
	previous := ps
	ps = fakeLsof(t, body)
	t.Cleanup(func() { ps = previous })
}

func TestOpenNamesLeavesOutProcessesThatAreExiting(t *testing.T) {
	usePs(t, "printf '    1 Ss\\n34891 ?E\\n  700 Z+\\n  701 U\\n  702 Zl\\n'\n")
	lsof := fakeLsof(t, "for arg in \"$@\"; do printf 'n%s\\n' \"$arg\"; done\n")

	names, err := OpenNames(5, lsof)

	require.NoError(t, err)
	assert.Equal(t, []string{"-n", "-P", "-Fn", "-p", "^34891,^700"}, names,
		"an exiting process or a zombie can no longer use a file, and asking lsof about one stuck while exiting hangs lsof for good; "+
			"a zombie leader whose threads still run (Zl on Linux) still holds its files")
}

func TestOpenNamesWithRealLsofLeavesOutARealZombie(t *testing.T) {
	zombie := exec.Command("/bin/sh", "-c", "exit 0")
	require.NoError(t, zombie.Start())
	t.Cleanup(func() { _ = zombie.Wait() })
	require.Eventually(t, func() bool {
		out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(zombie.Process.Pid)).Output()
		return err == nil && strings.Contains(string(out), "Z")
	}, 5*time.Second, 10*time.Millisecond)
	path := filepath.Join(t.TempDir(), "open")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	names, err := OpenNames(30, "lsof")

	require.NoError(t, err, "lsof accepts an excluded process, here a zombie")
	assert.True(t, slices.Contains(names, resolved))
}

func TestOpenNamesAsksAboutEveryProcessWhenNoneIsExiting(t *testing.T) {
	usePs(t, "printf '    1 Ss\\n  701 R+\\n'\n")
	lsof := fakeLsof(t, "for arg in \"$@\"; do printf 'n%s\\n' \"$arg\"; done\n")

	names, err := OpenNames(5, lsof)

	require.NoError(t, err)
	assert.Equal(t, []string{"-n", "-P", "-Fn"}, names)
}

func TestOpenNamesFailsWhenItCannotListProcesses(t *testing.T) {
	usePs(t, "echo 'ps: broken' >&2\nexit 1\n")

	names, err := OpenNames(5, fakeLsof(t, "printf 'n/a\\n'\n"))

	assert.Nil(t, names)
	require.ErrorContains(t, err, "ps: exit 1: ps: broken")
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
	usePs(t, "exit 0\n")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names, err := OpenNames(tc.timeout, fakeLsof(t, tc.body))

			assert.Nil(t, names)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.fail)
		})
	}
}
