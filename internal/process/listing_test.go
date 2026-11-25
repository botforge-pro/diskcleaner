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

func names(files []Open) []string {
	var names []string
	for _, file := range files {
		names = append(names, file.Name)
	}
	return names
}

func TestOpenFilesListsEveryNameLsofPrints(t *testing.T) {
	usePs(t, "exit 0\n")
	lsof := fakeLsof(t, "printf 'p42\\nfcwd\\nn/work\\nf3\\nn/work/a.o\\np43\\nf4\\nn->0x1234\\n'\nexit 0\n")

	files, err := OpenFiles(5, lsof)

	require.NoError(t, err)
	assert.Equal(t, []string{"/work", "/work/a.o", "->0x1234"}, names(files))
}

func TestOpenFilesKeepsTheDeviceAndInodeOfEachFile(t *testing.T) {
	usePs(t, "exit 0\n")
	lsof := fakeLsof(t, "printf 'p42\\nftxt\\nD0x100000f\\ni692842124\\nn/bin/zsh\\nf4\\nn->0x1234\\n'\n")

	files, err := OpenFiles(5, lsof)

	require.NoError(t, err)
	assert.Equal(t, []Open{{Name: "/bin/zsh", Device: 0x100000f, Inode: 692842124}, {Name: "->0x1234"}}, files)
}

func TestOpenFilesFailsOnADeviceItCannotRead(t *testing.T) {
	usePs(t, "exit 0\n")

	files, err := OpenFiles(5, fakeLsof(t, "printf 'p42\\nftxt\\nDsomewhere\\nn/bin/zsh\\n'\n"))

	assert.Nil(t, files)
	require.ErrorContains(t, err, `cannot read "Dsomewhere"`)
}

func usePs(t *testing.T, body string) {
	previous := ps
	ps = fakeLsof(t, body)
	t.Cleanup(func() { ps = previous })
}

func TestOpenFilesLeavesOutProcessesThatAreExiting(t *testing.T) {
	usePs(t, "printf '    1 Ss\\n34891 ?E\\n  700 Z+\\n  701 U\\n  702 Zl\\n'\n")
	lsof := fakeLsof(t, "for arg in \"$@\"; do printf 'n%s\\n' \"$arg\"; done\n")

	files, err := OpenFiles(5, lsof)

	require.NoError(t, err)
	assert.Equal(t, []string{"-n", "-P", "-FDin", "-p", "^34891,^700"}, names(files),
		"an exiting process or a zombie can no longer use a file, and asking lsof about one stuck while exiting hangs lsof for good; "+
			"a zombie leader whose threads still run (Zl on Linux) still holds its files")
}

func TestOpenFilesWithRealLsofLeavesOutARealZombie(t *testing.T) {
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

	files, err := OpenFiles(30, "lsof")

	require.NoError(t, err, "lsof accepts an excluded process, here a zombie")
	assert.True(t, slices.Contains(names(files), resolved))
}

func TestOpenFilesAsksAboutEveryProcessWhenNoneIsExiting(t *testing.T) {
	usePs(t, "printf '    1 Ss\\n  701 R+\\n'\n")
	lsof := fakeLsof(t, "for arg in \"$@\"; do printf 'n%s\\n' \"$arg\"; done\n")

	files, err := OpenFiles(5, lsof)

	require.NoError(t, err)
	assert.Equal(t, []string{"-n", "-P", "-FDin"}, names(files))
}

func TestOpenFilesFailsWhenItCannotListProcesses(t *testing.T) {
	usePs(t, "echo 'ps: broken' >&2\nexit 1\n")

	files, err := OpenFiles(5, fakeLsof(t, "printf 'n/a\\n'\n"))

	assert.Nil(t, files)
	require.ErrorContains(t, err, "ps: exit 1: ps: broken")
}

func TestOpenFilesWithRealLsofSeesAnOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "open")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	files, err := OpenFiles(30, "lsof")

	require.NoError(t, err)
	assert.True(t, slices.Contains(names(files), resolved))
}

func TestOpenFilesFails(t *testing.T) {
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
			files, err := OpenFiles(tc.timeout, fakeLsof(t, tc.body))

			assert.Nil(t, files)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.fail)
		})
	}
}
