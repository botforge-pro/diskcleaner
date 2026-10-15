package scratchpads

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func touch(t *testing.T, root string, paths ...string) {
	for _, path := range paths {
		full := filepath.Join(root, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, make([]byte, 4096), 0o644))
	}
}

func mkdir(t *testing.T, root string, paths ...string) {
	for _, path := range paths {
		require.NoError(t, os.MkdirAll(filepath.Join(root, path), 0o755))
	}
}

func newRoot(t *testing.T) string {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	scratch := "-dev-app/session/scratchpad"
	touch(t, root,
		scratch+"/dd-app/Build/Intermediates.noindex/App.o",
		scratch+"/dd-app/SourcePackages/workspace-state.json",
		scratch+"/spm/workspace-state.json",
		scratch+"/copy/Package.swift",
		scratch+"/copy/.build/debug/Core.o",
		scratch+"/tools/.build/deploy.sh",
		scratch+"/notes.txt",
		scratch+"/Build/Intermediates.noindex/Root.o",
		scratch+"/clone/wip.swift",
	)
	mkdir(t, root,
		scratch+"/dd-app/SourcePackages/checkouts",
		scratch+"/spm/checkouts/pkg",
		scratch+"/clone/Build/Intermediates.noindex",
		"-dev-app/session/tasks/Build/Intermediates.noindex",
	)
	return root
}

func idle(string) (bool, error) { return false, nil }

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestRemovesBuildOutputAndKeepsTheRest(t *testing.T) {
	root := newRoot(t)
	scratch := filepath.Join(root, "-dev-app", "session", "scratchpad")

	r, stdout, stderr := run(Step{Root: root, Processes: "swift", Running: idle}, false)

	for _, gone := range []string{"Build", "clone/Build", "copy/.build", "dd-app/Build", "dd-app/SourcePackages", "spm/checkouts"} {
		assert.NoDirExists(t, filepath.Join(scratch, gone), gone)
	}
	for _, kept := range []string{"notes.txt", "clone/wip.swift", "spm/workspace-state.json", "copy/Package.swift", "tools/.build/deploy.sh"} {
		assert.FileExists(t, filepath.Join(scratch, kept), kept)
	}
	assert.DirExists(t, filepath.Join(root, "-dev-app", "session", "tasks", "Build", "Intermediates.noindex"))
	assert.Equal(t, 6, r.Count)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stdout, "SCRATCH  "+root+"\n")
	assert.Contains(t, stderr, "SKIPPED       "+filepath.Join(scratch, "tools", ".build")+": no Package.swift next to it\n")
}

func TestAPackageBuildInsideABuildFolderWithoutPackageSwiftStays(t *testing.T) {
	root := newRoot(t)
	tools := filepath.Join(root, "-dev-app", "session", "scratchpad", "tools", ".build")
	touch(t, tools, "fixture/Package.swift", "fixture/.build/data")

	r, _, _ := run(Step{Root: root, Processes: "swift", Running: idle}, false)

	assert.FileExists(t, filepath.Join(tools, "fixture", ".build", "data"))
	assert.Equal(t, 6, r.Count)
}

func TestCachesInsideAGitProtectedBuildAreStillFound(t *testing.T) {
	root := newRoot(t)
	scratch := filepath.Join(root, "-dev-app", "session", "scratchpad")
	app := filepath.Join(scratch, "tracked-app")
	touch(t, app, "Build/Intermediates.noindex/kept.txt", "Build/Intermediates.noindex/deps/workspace-state.json")
	mkdir(t, app, "Build/Intermediates.noindex/deps/checkouts/pkg")
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "Build/Intermediates.noindex/kept.txt"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "fixture"},
	} {
		output, err := exec.Command("git", append([]string{"-C", app}, args...)...).CombinedOutput()
		require.NoError(t, err, string(output))
	}

	_, _, stderr := run(Step{Root: root, Processes: "swift", Running: idle}, false)

	assert.FileExists(t, filepath.Join(app, "Build", "Intermediates.noindex", "kept.txt"))
	assert.NoDirExists(t, filepath.Join(app, "Build", "Intermediates.noindex", "deps", "checkouts"))
	assert.Contains(t, stderr, filepath.Join(app, "Build")+": contains files tracked by git")
}

func TestDryRunRemovesNothing(t *testing.T) {
	root := newRoot(t)

	r, _, _ := run(Step{Root: root, Processes: "swift", Running: idle}, true)

	assert.DirExists(t, filepath.Join(root, "-dev-app", "session", "scratchpad", "dd-app", "Build"))
	assert.Equal(t, 6, r.Count)
}

func TestSkipsWhileASwiftBuildRuns(t *testing.T) {
	root := newRoot(t)
	busy := func(string) (bool, error) { return true, nil }

	r, _, stderr := run(Step{Root: root, Processes: "swift", Running: busy}, false)

	assert.DirExists(t, filepath.Join(root, "-dev-app", "session", "scratchpad", "dd-app", "Build"))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+root+": a Swift package build or xcodebuild is running\n")
}

func TestProcessCheckErrorSkipsAndFails(t *testing.T) {
	root := newRoot(t)
	broken := func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := run(Step{Root: root, Processes: "swift", Running: broken}, false)

	assert.DirExists(t, filepath.Join(root, "-dev-app", "session", "scratchpad", "dd-app", "Build"))
	assert.Len(t, r.Failures, 1)
}

func TestAnUnreadableFolderFailsAndTheRestGoes(t *testing.T) {
	root := newRoot(t)
	locked := filepath.Join(root, "-locked")
	touch(t, locked, "s/scratchpad/dd/Build/Intermediates.noindex/x.o")
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	r, _, _ := run(Step{Root: root, Processes: "swift", Running: idle}, false)

	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "scan "+root)
	assert.Equal(t, 6, r.Count)
}

func TestMissingRootIsQuiet(t *testing.T) {
	r, stdout, stderr := run(Step{Root: filepath.Join(t.TempDir(), "absent"), Processes: "swift", Running: idle}, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
