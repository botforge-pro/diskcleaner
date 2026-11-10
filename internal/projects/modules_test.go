package projects

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newNodeProjects(t *testing.T) string {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	touch(t, root,
		"locked/package.json", "locked/package-lock.json", "locked/node_modules/left-pad/index.js",
		"workspace/package-lock.json", "workspace/packages/web/package.json",
		"workspace/packages/web/node_modules/left-pad/index.js",
		"plain/package.json", "plain/package-lock.json", "plain/nested/package.json",
		"plain/nested/node_modules/left-pad/index.js",
		"unlocked/package.json", "unlocked/node_modules/left-pad/index.js",
	)
	require.NoError(t, os.WriteFile(filepath.Join(root, "workspace", "package.json"), []byte(`{"workspaces": ["packages/*"]}`), 0o644))
	return root
}

func TestRemovesNodeModulesWithALockfile(t *testing.T) {
	root := newNodeProjects(t)

	r, _, stderr := run(step(t, root), false)

	assert.NoDirExists(t, filepath.Join(root, "locked", "node_modules"))
	assert.NoDirExists(t, filepath.Join(root, "workspace", "packages", "web", "node_modules"))
	assert.DirExists(t, filepath.Join(root, "unlocked", "node_modules"))
	assert.DirExists(t, filepath.Join(root, "plain", "nested", "node_modules"))
	assert.FileExists(t, filepath.Join(root, "locked", "package-lock.json"))
	assert.Equal(t, 2, r.Count)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stderr, filepath.Join(root, "unlocked", "node_modules")+": no lockfile, reinstall would not restore the same versions")
	assert.Contains(t, stderr, filepath.Join(root, "plain", "nested", "node_modules")+": no lockfile")
}

func TestPnpmWorkspaceCountsAsAWorkspaceRoot(t *testing.T) {
	root := newNodeProjects(t)
	touch(t, root, "pnpm/pnpm-workspace.yaml", "pnpm/pnpm-lock.yaml", "pnpm/apps/site/node_modules/x/index.js")

	run(step(t, root), false)

	assert.NoDirExists(t, filepath.Join(root, "pnpm", "apps", "site", "node_modules"))
}

func TestKeepsNodeModulesAnInstallIsWriting(t *testing.T) {
	root := newNodeProjects(t)
	s := step(t, root)
	s.InUse.Now = time.Now
	age(t, root, "workspace/packages/web/node_modules/left-pad/index.js",
		"workspace/packages/web/node_modules/left-pad", "workspace/packages/web/node_modules")

	r, _, stderr := run(s, false)

	assert.Empty(t, r.Failures)
	assert.NoDirExists(t, filepath.Join(root, "workspace", "packages", "web", "node_modules"))
	writing := filepath.Join(root, "locked", "node_modules")
	assert.DirExists(t, writing)
	assert.Contains(t, stderr, "SKIPPED       "+writing+": written within 1h")
}

func TestEveryProjectFolderIsCleaned(t *testing.T) {
	first, second := newNodeProjects(t), newNodeProjects(t)
	s := step(t, first)
	s.Dirs = append(s.Dirs, second)

	r, _, _ := run(s, false)

	assert.NoDirExists(t, filepath.Join(first, "locked", "node_modules"))
	assert.NoDirExists(t, filepath.Join(second, "locked", "node_modules"))
	assert.Equal(t, 4, r.Count)
}

func TestAMissingProjectFolderIsQuiet(t *testing.T) {
	r, stdout, stderr := run(step(t, filepath.Join(t.TempDir(), "absent")), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestDefaultTakesEachSpellingOfAFolderOnce(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "repos"), 0o755))
	require.NoError(t, os.Symlink("repos", filepath.Join(dir, "alias")))
	t.Chdir(dir)

	s, err := Default([]string{"./repos", filepath.Join(dir, "repos"), "alias"}, time.Hour, time.Hour)

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "repos")}, s.Dirs)
}

func TestDefaultRefusesAMissingProjectFolder(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")

	_, err := Default([]string{missing}, time.Hour, time.Hour)

	assert.EqualError(t, err, "project folder not found: "+missing+" (see project-dirs)")
}
