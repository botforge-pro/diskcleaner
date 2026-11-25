package rule

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

const noneOpen = "../process/testdata/lsof-none-open"

var testGroups = Groups{"tools": {"tool-build", "tool-test"}}

func parse(t *testing.T, name, body string) File {
	f, err := Parse(name, []byte(body), testGroups)
	require.NoError(t, err)
	return f
}

func idle(string) (bool, error) { return false, nil }

func environment(home string) Environment {
	return Environment{
		Home:      home,
		Getenv:    func(string) string { return "" },
		Processes: Processes{Running: idle},
		Commands: Commands{
			LookPath: func(string) (string, error) { return "", errors.New("not expected") },
			Run: func(_ time.Duration, name string, args ...string) process.Result {
				return process.Result{Err: fmt.Errorf("not expected: %s %s", name, strings.Join(args, " "))}
			},
		},
		Duration: func(string) time.Duration { return 0 },
		Count:    func(string) int { return 1 },
		Dirs:     func(string) []string { return nil },
		Now:      time.Now,
		Lsof:     inuse.Lsof{Command: noneOpen, TimeoutSeconds: 5},
		Mounts:   MountPoints,
	}
}

func run(s Rule, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func temp(t *testing.T) string {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func write(t *testing.T, path string) string {
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
	return path
}

func fakeLsof(t *testing.T, script string) string {
	path := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755))
	return path
}

func step(t *testing.T, home, body string) Rule {
	return parse(t, "tool-cache.yaml", "order: 1\ndescribes: x\n"+body).Rule(environment(home))
}

func TestRemoveDotTakesTheFolderWhole(t *testing.T) {
	root := filepath.Join(temp(t), "cache")
	write(t, filepath.Join(root, "repositories", "pkg.git"))

	r, stdout, _ := run(step(t, "/nowhere", "root: "+root+"\nremove: .\n"), false)

	assert.NoDirExists(t, root)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "TOOL     "+root+"\n")
}

func TestALinkedRootLosesOnlyTheLink(t *testing.T) {
	target := filepath.Join(temp(t), "cache")
	write(t, filepath.Join(target, "repositories", "pkg.git"))
	link := filepath.Join(temp(t), "cache")
	require.NoError(t, os.Symlink(target, link))

	r, _, _ := run(step(t, "/nowhere", "root: "+link+"\nremove: .\n"), false)

	_, err := os.Lstat(link)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.FileExists(t, filepath.Join(target, "repositories", "pkg.git"))
	assert.Equal(t, 1, r.Count)
}

func TestARootAtOrAboveHomeIsRefusedAfterItsLinksAreResolved(t *testing.T) {
	home := temp(t)
	linkToHome := filepath.Join(temp(t), "home")
	require.NoError(t, os.Symlink(home, linkToHome))
	for _, root := range []string{"/", home, filepath.Dir(home), linkToHome} {
		t.Run(root, func(t *testing.T) {
			r, _, _ := run(step(t, home, "root: "+root+"\nremove: [{folders: '*'}]\n"), false)

			assert.DirExists(t, home)
			assert.Zero(t, r.Count)
			require.Len(t, r.Failures, 1)
			assert.Contains(t, r.Failures[0], "refused")
		})
	}
}

func TestAMissingRootIsQuiet(t *testing.T) {
	r, stdout, stderr := run(step(t, "/nowhere", "root: "+filepath.Join(temp(t), "absent")+"\nremove: .\n"), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestARunningToolKeepsEverythingAndSaysWhy(t *testing.T) {
	root := filepath.Join(temp(t), "cache")
	write(t, filepath.Join(root, "x"))
	s := parse(t, "tool-cache.yaml", "order: 1\ndescribes: x\nroot: "+root+"\nunless: [{running: [{group: tools}], says: a tool build is running}]\nremove: .\n")
	env := environment("/nowhere")
	var asked []string
	env.Processes.Running = func(pattern string) (bool, error) {
		asked = append(asked, pattern)
		return true, nil
	}

	r, _, stderr := run(s.Rule(env), false)

	assert.DirExists(t, root)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+root+": a tool build is running")
	assert.Equal(t, []string{"tool-build|tool-test"}, asked)
}

func TestAFailedProcessCheckKeepsEverythingAndFails(t *testing.T) {
	root := filepath.Join(temp(t), "cache")
	write(t, filepath.Join(root, "x"))
	s := parse(t, "tool-cache.yaml", "order: 1\ndescribes: x\nroot: "+root+"\nunless: [{running: [tool]}]\nremove: .\n")
	env := environment("/nowhere")
	env.Processes.Running = func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := run(s.Rule(env), false)

	assert.DirExists(t, root)
	assert.Equal(t, []string{"pgrep -f 'tool': exit status 2"}, r.Failures)
}

func TestNeverRemovesOutsideWhenAFolderBecomesALink(t *testing.T) {
	root := temp(t)
	sub := filepath.Join(root, "sub")
	write(t, filepath.Join(sub, "data.json"))
	outside := write(t, filepath.Join(temp(t), "data.json"))
	s := parse(t, "tool-cache.yaml", "order: 1\ndescribes: x\nroot: "+root+"\nremove: [{files: '**/*.json'}]\nkeep: {open: true}\n")
	env := environment("/nowhere")
	env.Lsof = inuse.Lsof{Command: fakeLsof(t, "mv '"+sub+"' '"+sub+".moved'\nln -s '"+filepath.Dir(outside)+"' '"+sub+"'\nprintf 'p1\\nn/elsewhere\\n'\n"), TimeoutSeconds: 5}

	r, _, stderr := run(s.Rule(env), false)

	assert.FileExists(t, outside, "a file reached through a link that replaced a folder is not under the root")
	assert.FileExists(t, filepath.Join(sub+".moved", "data.json"))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, filepath.Join(sub, "data.json")+": changed since it was found")
}

func TestASearchDoesNotPassThroughALink(t *testing.T) {
	root := temp(t)
	outside := temp(t)
	write(t, filepath.Join(outside, "build", "x"))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "linked")))
	inside := write(t, filepath.Join(root, "app", "build", "x"))

	r, _, _ := run(step(t, "/nowhere", "root: "+root+"\nremove: [{folders: '**/build'}]\n"), false)

	assert.DirExists(t, filepath.Join(outside, "build"))
	assert.NoFileExists(t, inside)
	assert.Equal(t, 1, r.Count)
}

func TestRemovingAFolderRemovesALinkInsideItAsALink(t *testing.T) {
	root := temp(t)
	outside := write(t, filepath.Join(temp(t), "keep.json"))
	build := filepath.Join(root, "build")
	write(t, filepath.Join(build, "x"))
	require.NoError(t, os.Symlink(filepath.Dir(outside), filepath.Join(build, "linked")))

	r, _, _ := run(step(t, "/nowhere", "root: "+root+"\nremove: [{folders: build}]\n"), false)

	assert.NoDirExists(t, build)
	assert.FileExists(t, outside)
	assert.Equal(t, 1, r.Count)
}

func TestAFolderHoldingASocketStays(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "sock")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	require.NoError(t, err)
	held := filepath.Join(root, "go-build1")
	require.NoError(t, os.MkdirAll(held, 0o755))
	listener, err := net.Listen("unix", filepath.Join(held, "s"))
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	write(t, filepath.Join(root, "go-build2", "x"))

	r, _, stderr := run(step(t, "/nowhere", "root: "+root+"\nremove: [{folders: 'go-build*'}]\n"), false)

	assert.DirExists(t, held)
	assert.NoDirExists(t, filepath.Join(root, "go-build2"))
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stderr, held+": holds the socket")
}

func TestASocketThatAppearsInAFolderBeingRemovedStopsTheRemoval(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "sock")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	folder := filepath.Join(root, "build")
	write(t, filepath.Join(folder, "x"))
	listener, err := net.Listen("unix", filepath.Join(folder, "s"))
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	parent, err := unix.Open(root, openFolder, 0)
	require.NoError(t, err)
	t.Cleanup(func() { unix.Close(parent) })
	var stat unix.Stat_t
	require.NoError(t, unix.Stat(root, &stat))

	err = removeTree(parent, "build", uint64(stat.Dev))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "the socket s appeared in it")
	assert.DirExists(t, folder)
}

func TestAFolderHoldingAnotherUsersFileIsKept(t *testing.T) {
	found := tree{volume: 1}
	mine := fakeInfo{stat: syscall.Stat_t{Dev: 1, Uid: uint32(os.Getuid())}}
	theirs := fakeInfo{stat: syscall.Stat_t{Dev: 1, Uid: uint32(os.Getuid()) + 1}}

	found.add("/a/x", mine)
	found.add("/a/root-owned", theirs)

	assert.Equal(t, "/a/root-owned", found.foreign)
}

type fakeInfo struct {
	os.FileInfo
	stat syscall.Stat_t
}

func (f fakeInfo) Sys() any           { return &f.stat }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) Mode() os.FileMode  { return 0 }

func TestAFailedLsofKeepsEverythingAndFails(t *testing.T) {
	root := temp(t)
	first := write(t, filepath.Join(root, "a.log"))
	second := write(t, filepath.Join(root, "b.log"))
	s := parse(t, "tool-cache.yaml", "order: 1\ndescribes: x\nroot: "+root+"\nremove: [{files: '*.log'}]\nkeep: {open: true}\n")
	env := environment("/nowhere")
	env.Lsof = inuse.Lsof{Command: fakeLsof(t, "echo broken >&2\nexit 2\n"), TimeoutSeconds: 5}

	r, _, _ := run(s.Rule(env), false)

	assert.FileExists(t, first)
	assert.FileExists(t, second)
	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "lsof")
}

func TestAFileWrittenAgainAfterTheScanStays(t *testing.T) {
	root := temp(t)
	file := write(t, filepath.Join(root, "a.log"))
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(file, old, old))
	s := parse(t, "tool-cache.yaml", "order: 1\ndescribes: x\nroot: "+root+"\nremove: [{files: '*.log'}]\nkeep: {age: {of: mtime, within: 1d}, open: true}\n")
	env := environment("/nowhere")
	env.Lsof = inuse.Lsof{Command: fakeLsof(t, "touch '"+file+"'\nprintf 'p1\\nn/elsewhere\\n'\n"), TimeoutSeconds: 5}

	r, _, stderr := run(s.Rule(env), false)

	assert.FileExists(t, file)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, file+": used since it was found")
}

func TestADryRunRemovesNothingAndCountsWhatIsInsideACountedFolderOnce(t *testing.T) {
	root := temp(t)
	write(t, filepath.Join(root, "app", ".build", "pkg", "__pycache__", "x"))
	body := "root: " + root + "\ndo:\n  - remove: [{folders: '**/.build'}]\n  - remove: [{folders: '**/__pycache__'}]\n"

	r, stdout, _ := run(step(t, "/nowhere", body), true)

	assert.DirExists(t, filepath.Join(root, "app", ".build"))
	assert.Equal(t, 1, r.Count)
	assert.Equal(t, 1, strings.Count(stdout, "WOULD REMOVE"))
}

func TestADeepPatternMatchesAnyDepthAndNoMore(t *testing.T) {
	root := temp(t)
	top := write(t, filepath.Join(root, "cache", "x"))
	deep := write(t, filepath.Join(root, "a", "b", "cache", "x"))
	other := write(t, filepath.Join(root, "a", "caches", "x"))

	run(step(t, "/nowhere", "root: "+root+"\nremove: [{folders: '**/cache'}]\n"), false)

	assert.NoFileExists(t, top)
	assert.NoFileExists(t, deep)
	assert.FileExists(t, other)
}

func TestAnUnreadableFolderFailsAndTheRestGoes(t *testing.T) {
	root := temp(t)
	locked := filepath.Join(root, "locked")
	write(t, filepath.Join(locked, "cache", "x"))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	open := write(t, filepath.Join(root, "open", "cache", "x"))

	r, _, _ := run(step(t, "/nowhere", "root: "+root+"\nremove: [{folders: '**/cache'}]\n"), false)

	assert.NoFileExists(t, open)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], locked)
}

func TestAnEntryDoesNotLookInsideWhatItNamesWhileTheOthersDo(t *testing.T) {
	root := temp(t)
	write(t, filepath.Join(root, "Build", "x"))
	inner := write(t, filepath.Join(root, "Build", "deps", "checkouts", "x"))
	nested := write(t, filepath.Join(root, "Build", "Build", "x"))
	body := "root: " + root + "\nremove:\n  - {folders: '**/Build', when: [{exists: missing}]}\n  - folders: '**/checkouts'\n"

	run(step(t, "/nowhere", body), false)

	assert.NoFileExists(t, inner, "the checkouts entry searched inside the Build the first entry named")
	assert.FileExists(t, nested, "the Build entry did not look inside the Build it named")
}

func TestASkipBeforeAnEntryExcludesAndASkipAfterOnlyKeepsItsSearchOut(t *testing.T) {
	root := temp(t)
	named := write(t, filepath.Join(root, "a", ".build", "x"))
	inside := write(t, filepath.Join(root, "a", ".build", "build", "x"))
	kept := write(t, filepath.Join(root, ".X11-unix", "x"))
	body := "root: " + root + "\nremove:\n  - skip: '**/.X11-unix'\n  - folders: '**/.build'\n  - skip: '**/.build'\n  - folders: '**/build'\n  - folders: '**/.X11-unix'\n"

	run(step(t, "/nowhere", body), false)

	assert.NoFileExists(t, named, "the skip after the .build entry did not stop it from taking what it names")
	assert.NoFileExists(t, inside, "what lies inside a removed folder goes with it")
	assert.FileExists(t, kept, "a skip before an entry keeps what it names from that entry")
}

func TestASkipKeepsALaterEntryOutOfWhatItNames(t *testing.T) {
	root := temp(t)
	kept := write(t, filepath.Join(root, "node_modules", "pkg", "build", "x"))
	gone := write(t, filepath.Join(root, "app", "build", "x"))
	body := "root: " + root + "\nremove:\n  - skip: '**/node_modules'\n  - folders: '**/build'\n"

	run(step(t, "/nowhere", body), false)

	assert.FileExists(t, kept)
	assert.NoFileExists(t, gone)
}

func TestANodeThatFindsNothingAsksNothing(t *testing.T) {
	root := temp(t)
	write(t, filepath.Join(root, "x"))
	s := parse(t, "tool-cache.yaml", "order: 1\ndescribes: x\nroot: "+root+"\nunless: [{running: [tool], says: a tool runs}]\nremove: [{folders: '*.cache'}]\n")
	env := environment("/nowhere")
	env.Processes.Running = func(string) (bool, error) {
		t.Fatal("asked although nothing was found")
		return false, nil
	}

	r, _, stderr := run(s.Rule(env), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stderr)
}

func TestARelativeRootThatIsALinkIsNotEntered(t *testing.T) {
	root := temp(t)
	outside := temp(t)
	write(t, filepath.Join(outside, "cache", "x"))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "linked")))
	inside := write(t, filepath.Join(root, "real", "cache", "x"))
	body := "root: " + root + "\ndo:\n  - root: '*'\n    remove: [{folders: cache}]\n"

	r, _, _ := run(step(t, "/nowhere", body), false)

	assert.DirExists(t, filepath.Join(outside, "cache"))
	assert.NoFileExists(t, inside)
	assert.Equal(t, 1, r.Count)
}
