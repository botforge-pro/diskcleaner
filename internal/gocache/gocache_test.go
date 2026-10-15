package gocache

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	module string
	build  string
}

func newCaches(t *testing.T) fixture {
	root := t.TempDir()
	c := fixture{
		module: filepath.Join(root, "pkg", "mod"),
		build:  filepath.Join(root, "go-build"),
	}
	for _, path := range []string{
		filepath.Join(c.module, "example.com", "module@v1.0.0", "module.go"),
		filepath.Join(c.build, "00", "entry-d"),
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, make([]byte, 8192), 0o644))
	}
	t.Setenv("GOMODCACHE", c.module)
	t.Setenv("GOCACHE", c.build)
	return c
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func idle(string) (bool, error) { return false, nil }

func TestCleansBothCaches(t *testing.T) {
	c := newCaches(t)

	r, stdout, stderr := run(Step{Go: "go", Processes: "go", Running: idle}, false)

	assert.Empty(t, stderr)
	assert.NoDirExists(t, c.module)
	assert.NoFileExists(t, filepath.Join(c.build, "00", "entry-d"))
	assert.Equal(t, 2, r.Count)
	assert.Positive(t, r.KB)
	assert.Contains(t, stdout, "GO       module and build caches\n")
	assert.Contains(t, stdout, "RUN           go clean -modcache  (")
	assert.Contains(t, stdout, "RUN           go clean -cache  (")
}

func TestDryRunCleansNothing(t *testing.T) {
	c := newCaches(t)

	r, stdout, _ := run(Step{Go: "go", Processes: "go", Running: idle}, true)

	assert.FileExists(t, filepath.Join(c.module, "example.com", "module@v1.0.0", "module.go"))
	assert.FileExists(t, filepath.Join(c.build, "00", "entry-d"))
	assert.Equal(t, 2, r.Count)
	assert.Positive(t, r.KB)
	assert.Contains(t, stdout, "WOULD RUN     go clean -modcache  (")
	assert.Contains(t, stdout, "WOULD RUN     go clean -cache  (")
}

func TestSkipsWhileAGoCommandRuns(t *testing.T) {
	c := newCaches(t)
	busy := func(string) (bool, error) { return true, nil }

	r, _, stderr := run(Step{Go: "go", Processes: "go", Running: busy}, false)

	assert.DirExists(t, c.module)
	assert.Zero(t, r.Count)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stderr, "SKIPPED       go caches: a go command is running")
}

func TestProcessCheckErrorSkipsAndFails(t *testing.T) {
	c := newCaches(t)
	broken := func(string) (bool, error) { return false, errors.New("exit status 2") }

	r, _, _ := run(Step{Go: "go", Processes: "go", Running: broken}, false)

	assert.DirExists(t, c.module)
	assert.Len(t, r.Failures, 1)
}

func TestWithoutGoDoesNothing(t *testing.T) {
	newCaches(t)

	r, stdout, stderr := run(Step{Go: "diskcleaner-test-absent-go", Processes: "go", Running: idle}, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestMissingCachesAreQuiet(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOMODCACHE", filepath.Join(root, "absent-mod"))
	t.Setenv("GOCACHE", filepath.Join(root, "absent-build"))

	r, _, stderr := run(Step{Go: "go", Processes: "go", Running: idle}, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stderr)
}

func TestGoEnvFailureFails(t *testing.T) {
	r, _, _ := run(Step{Go: "false", Processes: "go", Running: idle}, false)

	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 2)
	assert.Contains(t, r.Failures[0], "go env GOMODCACHE: exit status 1")
	assert.Contains(t, r.Failures[1], "go env GOCACHE: exit status 1")
}

func fakeGo(t *testing.T, script string) string {
	path := filepath.Join(t.TempDir(), "go")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755))
	return path
}

func TestCleanFailureFailsAndCountsNothing(t *testing.T) {
	c := newCaches(t)
	fake := fakeGo(t, "[ \"$1\" = env ] || { echo cannot clean >&2; exit 3; }\nprintenv \"$2\"\n")

	r, _, _ := run(Step{Go: fake, Processes: "go", Running: idle}, false)

	assert.DirExists(t, c.module)
	assert.Zero(t, r.Count)
	assert.Zero(t, r.KB)
	require.Len(t, r.Failures, 2)
	assert.Contains(t, r.Failures[0], "clean -modcache: exit status 3: cannot clean")
}

func TestPartialCleanCountsOnlyFreedSpaceAndGoesOn(t *testing.T) {
	c := newCaches(t)
	kept := filepath.Join(c.module, "example.com", "kept@v1.0.0", "kept.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
	require.NoError(t, os.WriteFile(kept, make([]byte, 8192), 0o644))
	moduleKB, err := report.DiskUsageKB(c.module)
	require.NoError(t, err)
	buildKB, err := report.DiskUsageKB(c.build)
	require.NoError(t, err)
	fake := fakeGo(t, `case "$1 $2" in
"env "*) printenv "$2" ;;
"clean -modcache") rm -rf "$GOMODCACHE/example.com/module@v1.0.0"; echo permission denied >&2; exit 3 ;;
"clean -cache") rm -rf "$GOCACHE"/* ;;
esac
`)

	r, _, _ := run(Step{Go: fake, Processes: "go", Running: idle}, false)

	assert.FileExists(t, kept)
	assert.NoDirExists(t, filepath.Join(c.build, "00"))
	assert.Equal(t, 1, r.Count)
	assert.Greater(t, r.KB, buildKB)
	assert.Less(t, r.KB, buildKB+moduleKB)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "clean -modcache: exit status 3: permission denied")
}

func TestGoEnvDiagnosticsStayOutOfThePath(t *testing.T) {
	c := newCaches(t)
	fake := fakeGo(t, `case "$1 $2" in
"env "*) echo "go: downloading go1.99.0 (darwin/arm64)" >&2; printenv "$2" ;;
"clean -modcache") rm -rf "$GOMODCACHE" ;;
"clean -cache") rm -rf "$GOCACHE"/* ;;
esac
`)

	r, _, stderr := run(Step{Go: fake, Processes: "go", Running: idle}, false)

	assert.Empty(t, r.Failures)
	assert.Equal(t, 2, r.Count)
	assert.NoDirExists(t, c.module)
	assert.NoDirExists(t, filepath.Join(c.build, "00"))
	assert.Contains(t, stderr, "go: downloading go1.99.0")
}

func TestUnexpectedGoEnvOutputFails(t *testing.T) {
	c := newCaches(t)
	fake := fakeGo(t, "[ \"$1\" = env ] || exit 64\necho first\nprintenv \"$2\"\n")

	r, _, _ := run(Step{Go: fake, Processes: "go", Running: idle}, false)

	assert.DirExists(t, c.module)
	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 2)
	assert.Contains(t, r.Failures[0], "go env GOMODCACHE: expected one line")
}
