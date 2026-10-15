package nodemodules

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/projects"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, root, path, body string) {
	full := filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

func newRoot(t *testing.T) string {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	write(t, root, "locked/package.json", "{}")
	write(t, root, "locked/package-lock.json", "{}")
	write(t, root, "locked/node_modules/left-pad/index.js", "x")
	write(t, root, "workspace/package.json", `{"workspaces": ["packages/*"]}`)
	write(t, root, "workspace/package-lock.json", "{}")
	write(t, root, "workspace/packages/web/package.json", "{}")
	write(t, root, "workspace/packages/web/node_modules/left-pad/index.js", "x")
	write(t, root, "plain/package.json", "{}")
	write(t, root, "plain/package-lock.json", "{}")
	write(t, root, "plain/nested/package.json", "{}")
	write(t, root, "plain/nested/node_modules/left-pad/index.js", "x")
	write(t, root, "unlocked/package.json", "{}")
	write(t, root, "unlocked/node_modules/left-pad/index.js", "x")

	octo := filepath.Join(root, "octo")
	require.NoError(t, os.MkdirAll(octo, 0o755))
	output, err := exec.Command("git", "-C", octo, "init", "-q").CombinedOutput()
	require.NoError(t, err, string(output))
	died := filepath.Join(octo, ".git", "objects", "pack", "tmp_pack_died")
	write(t, octo, ".git/objects/pack/tmp_pack_died", "x")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(died, old, old))
	return root
}

func step(t *testing.T, root string) Step {
	lsof := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(lsof, []byte("#!/bin/sh\nexit 1\n"), 0o755))
	return Step{Root: root, Garbage: projects.GitGarbage{Minutes: 60, LsofTimeoutSeconds: 5, Now: time.Now, Lsof: lsof}}
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestRemovesNodeModulesWithALockfileAndGitGarbage(t *testing.T) {
	root := newRoot(t)

	r, stdout, stderr := run(step(t, root), false)

	assert.NoDirExists(t, filepath.Join(root, "locked", "node_modules"))
	assert.NoDirExists(t, filepath.Join(root, "workspace", "packages", "web", "node_modules"))
	assert.NoFileExists(t, filepath.Join(root, "octo", ".git", "objects", "pack", "tmp_pack_died"))
	assert.DirExists(t, filepath.Join(root, "unlocked", "node_modules"))
	assert.DirExists(t, filepath.Join(root, "plain", "nested", "node_modules"))
	assert.FileExists(t, filepath.Join(root, "locked", "package-lock.json"))
	assert.Equal(t, 3, r.Count)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stdout, "NODE     "+root+"\n")
	assert.Contains(t, stderr, filepath.Join(root, "unlocked", "node_modules")+": no lockfile, reinstall would not restore the same versions")
	assert.Contains(t, stderr, filepath.Join(root, "plain", "nested", "node_modules")+": no lockfile")
}

func TestPnpmWorkspaceCountsAsAWorkspaceRoot(t *testing.T) {
	root := newRoot(t)
	write(t, root, "pnpm/pnpm-workspace.yaml", "packages: [apps/*]")
	write(t, root, "pnpm/pnpm-lock.yaml", "x")
	write(t, root, "pnpm/apps/site/node_modules/x/index.js", "x")

	run(step(t, root), false)

	assert.NoDirExists(t, filepath.Join(root, "pnpm", "apps", "site", "node_modules"))
}

func TestGitGarbageIsLeftToProjectsWhenTheFoldersAreTheSame(t *testing.T) {
	root := newRoot(t)
	s := step(t, root)
	s.ReposRoot = root

	r, _, _ := run(s, false)

	assert.FileExists(t, filepath.Join(root, "octo", ".git", "objects", "pack", "tmp_pack_died"))
	assert.Equal(t, 2, r.Count)
}

func TestDefaultSeesAnotherSpellingOfTheRepositoriesFolderAsIt(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "repos"), 0o755))
	require.NoError(t, os.Symlink("repos", filepath.Join(dir, "alias")))
	t.Chdir(dir)

	for _, pair := range [][2]string{
		{"./repos", filepath.Join(dir, "repos")},
		{filepath.Join(dir, "repos"), "repos"},
		{"alias", filepath.Join(dir, "repos")},
		{filepath.Join(dir, "repos"), filepath.Join(dir, "alias")},
	} {
		t.Setenv("DISKCLEANER_NODE_DIR", pair[0])
		t.Setenv("DISKCLEANER_REPOS_DIR", pair[1])

		s, err := Default(t.TempDir())

		require.NoError(t, err, pair)
		assert.Equal(t, filepath.Join(dir, "repos"), s.Root, pair)
		assert.Equal(t, s.Root, s.ReposRoot, pair)
	}
}

func TestDryRunRemovesNothing(t *testing.T) {
	root := newRoot(t)

	r, _, _ := run(step(t, root), true)

	assert.DirExists(t, filepath.Join(root, "locked", "node_modules"))
	assert.Equal(t, 3, r.Count)
}

func TestMissingRootIsQuiet(t *testing.T) {
	r, stdout, stderr := run(step(t, filepath.Join(t.TempDir(), "absent")), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
