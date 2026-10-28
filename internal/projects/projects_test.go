package projects

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var old = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

func touch(t *testing.T, root string, paths ...string) {
	for _, path := range paths {
		full := filepath.Join(root, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, make([]byte, 4096), 0o644))
	}
}

func age(t *testing.T, root string, paths ...string) {
	for _, path := range paths {
		require.NoError(t, os.Chtimes(filepath.Join(root, path), old, old))
	}
}

func git(t *testing.T, dir string, args ...string) {
	output, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	require.NoError(t, err, string(output))
}

func script(t *testing.T, name, body string) string {
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return path
}

func newRepos(t *testing.T) string {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	swift := filepath.Join(root, "swiftapp")
	require.NoError(t, os.MkdirAll(swift, 0o755))
	git(t, swift, "init", "-q")
	touch(t, swift,
		"Packages/Core/Package.swift",
		"Packages/Core/.build/debug/Core.o",
		"tools/.build/deploy.sh",
		".git/objects/pack/tmp_pack_died",
		".git/objects/tmp_obj_died",
		".git/objects/pack/tmp_pack_fetching",
		".git/objects/pack/pack-kept.pack",
		".git/objects/pack/.tmp-99999999-pack-died.pack",
		fmt.Sprintf(".git/objects/pack/.tmp-%d-pack-repacking.pack", os.Getpid()),
	)
	age(t, swift,
		".git/objects/pack/tmp_pack_died",
		".git/objects/tmp_obj_died",
		".git/objects/pack/pack-kept.pack",
		".git/objects/pack/.tmp-99999999-pack-died.pack",
		fmt.Sprintf(".git/objects/pack/.tmp-%d-pack-repacking.pack", os.Getpid()),
	)

	android := filepath.Join(root, "androidapp")
	require.NoError(t, os.MkdirAll(android, 0o755))
	git(t, android, "init", "-q")
	touch(t, android, "gradlew", "settings.gradle.kts", "app/build.gradle.kts", "lib/build.gradle.kts", "lib/build/generated-source.kt")
	git(t, android, "add", ".")
	git(t, android, "commit", "-q", "-m", "fixture")
	touch(t, android,
		"app/build/outputs/apk/app-debug.apk",
		"app/build/tmp/pkg/Package.swift",
		"app/build/tmp/pkg/.build/cache",
		"app/release/app-release.aab",
		"docs/build/index.html",
		"docs/build/site/Package.swift",
		"docs/build/site/.build/cache",
	)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "not-a-repo", "build"), 0o755))
	return root
}

func step(t *testing.T, repos string) Step {
	return Step{
		Repos: repos,
		InUse: inuse.Check{
			Minutes: 60, Now: func() time.Time { return time.Now().Add(2 * time.Hour) },
			Lsof: script(t, "lsof", "printf 'p1\\nfcwd\\nn/\\n'\n"), LsofTimeoutSeconds: 5,
		},
		Garbage: GitGarbage{Minutes: 60, LsofTimeoutSeconds: 5, Now: time.Now, Lsof: script(t, "lsof", "exit 1\n")},
		LFS:     LFS{Git: "git", GitLFS: "diskcleaner-test-absent-git-lfs"},
	}
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestRemovesBuildOutputAndGitGarbage(t *testing.T) {
	repos := newRepos(t)

	r, stdout, stderr := run(step(t, repos), false)

	for _, gone := range []string{
		"swiftapp/Packages/Core/.build",
		"swiftapp/.git/objects/pack/tmp_pack_died",
		"swiftapp/.git/objects/tmp_obj_died",
		"swiftapp/.git/objects/pack/.tmp-99999999-pack-died.pack",
		"androidapp/app/build",
		"androidapp/app/release/app-release.aab",
		"androidapp/docs/build/site/.build",
	} {
		_, err := os.Lstat(filepath.Join(repos, gone))
		assert.ErrorIs(t, err, os.ErrNotExist, gone)
	}
	for _, kept := range []string{
		"swiftapp/Packages/Core/Package.swift",
		"swiftapp/tools/.build/deploy.sh",
		"swiftapp/.git/objects/pack/tmp_pack_fetching",
		"swiftapp/.git/objects/pack/pack-kept.pack",
		fmt.Sprintf("swiftapp/.git/objects/pack/.tmp-%d-pack-repacking.pack", os.Getpid()),
		"androidapp/app/build.gradle.kts",
		"androidapp/docs/build/index.html",
		"androidapp/lib/build/generated-source.kt",
	} {
		assert.FileExists(t, filepath.Join(repos, kept), kept)
	}
	assert.Empty(t, r.Failures)
	assert.Equal(t, 7, r.Count)
	assert.Contains(t, stdout, "PROJECT  androidapp\n")
	assert.Contains(t, stdout, "PROJECT  swiftapp\n")
	assert.NotContains(t, stdout, "not-a-repo")
	for _, line := range []string{
		"swiftapp/tools/.build: no Package.swift next to it",
		"androidapp/lib/build: contains files tracked by git",
		"tmp_pack_fetching: written within 60 minutes",
		"pack-repacking.pack: the git repack that writes it is still running",
	} {
		assert.Contains(t, stderr, line)
	}
}

func TestKeepsBuildOutputThatABuildStillUses(t *testing.T) {
	repos := newRepos(t)
	swift := filepath.Join(repos, "swiftapp")
	touch(t, swift, "Packages/Running/Package.swift", "Packages/Running/.build/debug/Running.o",
		"Packages/Linking/Package.swift", "Packages/Linking/.build/debug/Linking.o",
		"Packages/Testing/Package.swift", "Packages/Testing/.build/debug/Testing.o")
	age(t, swift, "Packages/Core/.build/debug/Core.o", "Packages/Core/.build/debug", "Packages/Core/.build",
		"Packages/Linking/.build/debug/Linking.o", "Packages/Linking/.build/debug", "Packages/Linking/.build",
		"Packages/Testing/.build/debug/Testing.o", "Packages/Testing/.build/debug", "Packages/Testing/.build")
	linking := filepath.Join(swift, "Packages/Linking/.build")
	file, err := os.Open(filepath.Join(linking, "debug", "Linking.o"))
	require.NoError(t, err)
	defer file.Close()
	working := filepath.Join(swift, "Packages/Testing/.build")
	sleeper := exec.Command("sleep", "60")
	sleeper.Dir = filepath.Join(working, "debug")
	require.NoError(t, sleeper.Start())
	t.Cleanup(func() {
		assert.NoError(t, sleeper.Process.Kill())
		_ = sleeper.Wait()
	})
	s := step(t, repos)
	s.InUse.Now, s.InUse.Lsof = time.Now, "lsof"

	r, _, stderr := run(s, false)

	assert.Empty(t, r.Failures)
	assert.NoDirExists(t, filepath.Join(swift, "Packages/Core/.build"), "nothing uses it")
	running := filepath.Join(swift, "Packages/Running/.build")
	assert.DirExists(t, running)
	assert.Contains(t, stderr, "SKIPPED       "+running+": written within 60 minutes")
	assert.DirExists(t, linking)
	assert.Contains(t, stderr, "SKIPPED       "+linking+": a process still has something in it open")
	assert.DirExists(t, working, "a process works in it")
	assert.Contains(t, stderr, "SKIPPED       "+working+": a process still has something in it open")
}

func TestLsofFailureKeepsTheBuildOutputAndFails(t *testing.T) {
	repos := newRepos(t)
	s := step(t, repos)
	s.InUse.Lsof = script(t, "lsof", "echo 'lsof: broken' >&2\nexit 2\n")

	r, _, _ := run(s, false)

	assert.DirExists(t, filepath.Join(repos, "androidapp", "app", "build"))
	assert.DirExists(t, filepath.Join(repos, "swiftapp", "Packages", "Core", ".build"))
	assert.True(t, strings.Contains(strings.Join(r.Failures, "\n"), "lsof: exit 2: lsof: broken"), r.Failures)
	assert.NoFileExists(t, filepath.Join(repos, "swiftapp", ".git", "objects", "tmp_obj_died"), "git garbage has checks of its own")
}

func TestLinkedRepositoriesAreNotEntered(t *testing.T) {
	repos := newRepos(t)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	external := filepath.Join(outside, "external")
	require.NoError(t, os.MkdirAll(external, 0o755))
	git(t, external, "init", "-q")
	touch(t, external, ".git/objects/pack/tmp_pack_died", ".build/x.o", "Package.swift")
	age(t, external, ".git/objects/pack/tmp_pack_died")
	require.NoError(t, os.Symlink(external, filepath.Join(repos, "alias")))
	linkedGit := filepath.Join(repos, "linked-git")
	touch(t, linkedGit, ".build/x.o", "Package.swift")
	require.NoError(t, os.Symlink(filepath.Join(external, ".git"), filepath.Join(linkedGit, ".git")))

	_, stdout, _ := run(step(t, repos), false)

	assert.FileExists(t, filepath.Join(external, ".git", "objects", "pack", "tmp_pack_died"))
	assert.FileExists(t, filepath.Join(external, ".build", "x.o"))
	assert.FileExists(t, filepath.Join(linkedGit, ".build", "x.o"))
	assert.NotContains(t, stdout, "PROJECT  alias")
	assert.NotContains(t, stdout, "PROJECT  linked-git")
}

func TestUnreadableGitObjectsFailAndOtherRepositoriesGoOn(t *testing.T) {
	repos := newRepos(t)
	pack := filepath.Join(repos, "swiftapp", ".git", "objects", "pack")
	require.NoError(t, os.Chmod(pack, 0))
	t.Cleanup(func() { _ = os.Chmod(pack, 0o755) })

	r, _, _ := run(step(t, repos), false)

	assert.True(t, strings.Contains(strings.Join(r.Failures, "\n"), "read "+pack), r.Failures)
	assert.NoDirExists(t, filepath.Join(repos, "androidapp", "app", "build"))
	assert.NoFileExists(t, filepath.Join(repos, "swiftapp", ".git", "objects", "tmp_obj_died"))
}

func TestDefaultRefusesAMinuteCountThatWouldOverflow(t *testing.T) {
	t.Setenv("DISKCLEANER_REPOS_DIR", t.TempDir())
	t.Setenv("DISKCLEANER_GIT_TMP_MINUTES", "153722868")

	_, err := Default(t.TempDir())

	assert.ErrorContains(t, err, "DISKCLEANER_GIT_TMP_MINUTES must be at most 153722867 minutes")
}

func TestDryRunRemovesNothing(t *testing.T) {
	repos := newRepos(t)

	r, stdout, _ := run(step(t, repos), true)

	assert.DirExists(t, filepath.Join(repos, "androidapp", "app", "build"))
	assert.FileExists(t, filepath.Join(repos, "swiftapp", ".git", "objects", "pack", "tmp_pack_died"))
	assert.Equal(t, 7, r.Count)
	assert.Contains(t, stdout, "WOULD REMOVE")
}

func TestLsofDecidesTheGitGarbage(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		timeout int
		skip    string
		fail    string
	}{
		{"open", "exit 0\n", 5, "tmp_pack_died: a process still has it open", ""},
		{"error", "echo \"lsof: status error on $2: Permission denied\" >&2\nexit 1\n", 5, "", "tmp_pack_died: exit 1: lsof: status error on"},
		{"hung", "exec sleep 30\n", 1, "", "tmp_pack_died: no answer within 1s"},
		{"hung in a child", "sleep 30 &\necho $! >\"$0.pid\"\nwait\n", 1, "", "tmp_pack_died: no answer within 1s"},
		{"child left behind", "sleep 30 >/dev/null 2>&1 &\necho $! >\"$0.pid\"\nexit 1\n", 5, "", "tmp_pack_died: exited but left a process running"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repos := newRepos(t)
			s := step(t, repos)
			s.Garbage.Lsof = script(t, "lsof", tc.body)
			s.Garbage.LsofTimeoutSeconds = tc.timeout
			started := time.Now()

			r, _, stderr := run(s, false)

			assert.Less(t, time.Since(started), 30*time.Second)
			assert.FileExists(t, filepath.Join(repos, "swiftapp", ".git", "objects", "pack", "tmp_pack_died"))
			if pidText, err := os.ReadFile(s.Garbage.Lsof + ".pid"); err == nil {
				pid, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
				require.NoError(t, err)
				assert.Eventually(t, func() bool { return syscall.Kill(pid, 0) != nil },
					5*time.Second, 50*time.Millisecond, "the child of lsof is still running")
			}
			if tc.skip != "" {
				assert.Contains(t, stderr, tc.skip)
			}
			if tc.fail != "" {
				assert.True(t, strings.Contains(strings.Join(r.Failures, "\n"), tc.fail), r.Failures)
			}
		})
	}
}

func lfsRepo(t *testing.T) (string, string) {
	repos, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repo := filepath.Join(repos, "media")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	git(t, repo, "init", "-q")
	touch(t, repo, ".git/lfs/objects/ab/cd/old", ".git/lfs/objects/ef/gh/kept")
	return repos, repo
}

func fakeGitLFS(t *testing.T, body string) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git-lfs"), []byte("#!/bin/sh\n"+body), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const pruneScript = `case "$*" in
*--dry-run*) printf 'prune: 2 local objects, 1 retained\rprune: 1 file would be pruned (1.2 MB)\n' ;;
*) rm -rf .git/lfs/objects/ab; echo "prune: Deleting objects: 100%% (1/1), done." ;;
esac
`

func TestLFSPrunesWhatGitLFSWouldPrune(t *testing.T) {
	repos, repo := lfsRepo(t)
	fakeGitLFS(t, pruneScript)
	s := step(t, repos)
	s.LFS = DefaultLFS()

	r, stdout, _ := run(s, false)

	assert.Empty(t, r.Failures)
	assert.NoFileExists(t, filepath.Join(repo, ".git", "lfs", "objects", "ab", "cd", "old"))
	assert.FileExists(t, filepath.Join(repo, ".git", "lfs", "objects", "ef", "gh", "kept"))
	assert.Equal(t, 1, r.Count)
	assert.Positive(t, r.KB)
	assert.Contains(t, stdout, "LFS PRUNE     "+filepath.Join(repo, ".git", "lfs")+": prune: 1 file would be pruned (1.2 MB)\n")
}

func TestLFSPruneFailingMidwayCountsOnlyFreedSpace(t *testing.T) {
	repos, repo := lfsRepo(t)
	fakeGitLFS(t, `case "$*" in
*--dry-run*) echo "prune: 2 files would be pruned (8.2 kB)" ;;
*) rm -rf .git/lfs/objects/ab; echo "prune: unable to delete ef/gh/kept" >&2; exit 1 ;;
esac
`)
	s := step(t, repos)
	s.LFS = DefaultLFS()

	r, _, _ := run(s, false)

	assert.NoFileExists(t, filepath.Join(repo, ".git", "lfs", "objects", "ab", "cd", "old"))
	assert.Zero(t, r.Count)
	assert.Positive(t, r.KB)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "git lfs prune in "+repo+": exit status 1: prune: unable to delete")
}

func TestLFSDryRunCountsTheSizeGitLFSReports(t *testing.T) {
	repos, repo := lfsRepo(t)
	fakeGitLFS(t, pruneScript)
	s := step(t, repos)
	s.LFS = DefaultLFS()

	r, stdout, _ := run(s, true)

	assert.FileExists(t, filepath.Join(repo, ".git", "lfs", "objects", "ab", "cd", "old"))
	assert.Equal(t, 1, r.Count)
	assert.Equal(t, int64(1171), r.KB)
	assert.Contains(t, stdout, "WOULD PRUNE   ")
}

func TestLFSWithoutGitLFSFails(t *testing.T) {
	repos, repo := lfsRepo(t)

	r, _, _ := run(step(t, repos), false)

	assert.Equal(t, []string{filepath.Join(repo, ".git", "lfs") + ": git-lfs is not installed"}, r.Failures)
}

func TestDefaultRefusesAMissingReposFolder(t *testing.T) {
	t.Setenv("DISKCLEANER_REPOS_DIR", filepath.Join(t.TempDir(), "absent"))

	_, err := Default(t.TempDir())

	assert.ErrorContains(t, err, "repositories directory not found")
}

func TestDefaultRefusesAFractionalMinuteCount(t *testing.T) {
	t.Setenv("DISKCLEANER_REPOS_DIR", t.TempDir())
	t.Setenv("DISKCLEANER_GIT_TMP_MINUTES", "1.5")

	_, err := Default(t.TempDir())

	assert.EqualError(t, err, "DISKCLEANER_GIT_TMP_MINUTES must be a whole number above zero, got: 1.5")
}
