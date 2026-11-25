package candidates

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newReport() *report.Report {
	var stdout, stderr bytes.Buffer
	return &report.Report{Out: &stdout, Err: &stderr}
}

func write(t *testing.T, path string) {
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
}

func gitRun(t *testing.T, dir string, args ...string) {
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}

func newRepo(t *testing.T) string {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repo := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	gitRun(t, repo, "init", "-q")
	write(t, filepath.Join(repo, "app", "build", "tracked.txt"))
	gitRun(t, repo, "add", "app/build/tracked.txt")
	gitRun(t, repo, "commit", "-q", "-m", "fixture")
	write(t, filepath.Join(repo, "lib", "build", "output.o"))
	return repo
}

func TestGitTracking(t *testing.T) {
	repo := newRepo(t)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	write(t, filepath.Join(outside, "build", "out.o"))
	write(t, filepath.Join(repo, ".git", "objects", "pack", "tmp_pack_x"))

	cases := []struct {
		path string
		want Tracking
	}{
		{filepath.Join(repo, "app", "build"), Tracked},
		{filepath.Join(repo, "lib", "build"), Untracked},
		{filepath.Join(repo, ".git", "objects", "pack", "tmp_pack_x"), Untracked},
		{filepath.Join(outside, "build"), Untracked},
	}
	for _, tc := range cases {
		r := newReport()
		assert.Equal(t, tc.want, GitTracking(r, tc.path), tc.path)
		assert.Empty(t, r.Failures, tc.path)
	}
}

func TestARelativePathIsCheckedAgainstTheRightFile(t *testing.T) {
	repo := newRepo(t)
	t.Chdir(repo)
	r := newReport()

	assert.Equal(t, Tracked, GitTracking(r, filepath.Join("app", "build", "tracked.txt")))
	assert.Equal(t, Untracked, GitTracking(r, filepath.Join("lib", "build")))
}

func TestAPathThroughALinkedFolderNeverCountsAsUntracked(t *testing.T) {
	repo := newRepo(t)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	alias := filepath.Join(outside, "alias")
	require.NoError(t, os.Symlink(filepath.Join(repo, "app", "build"), alias))

	assert.NotEqual(t, Untracked, GitTracking(newReport(), filepath.Join(alias, "tracked.txt")))
}

func TestBareRepositoryIsUnknown(t *testing.T) {
	repo := newRepo(t)
	gitRun(t, repo, "config", "core.bare", "true")
	r := newReport()

	assert.Equal(t, Unknown, GitTracking(r, filepath.Join(repo, "lib", "build")))
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "neither a work tree nor inside a git directory")
}

func TestBrokenRepositoryBelowAGitDirectoryIsUnknown(t *testing.T) {
	repo := newRepo(t)
	require.NoError(t, os.RemoveAll(filepath.Join(repo, ".git")))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: /nowhere\n"), 0o644))
	r := newReport()

	assert.Equal(t, Unknown, GitTracking(r, filepath.Join(repo, "lib", "build")))
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "git rev-parse in "+filepath.Join(repo, "lib"))
}
