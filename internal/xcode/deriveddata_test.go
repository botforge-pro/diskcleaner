package xcode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDerivedDataFollowsXcodesOwnLocation(t *testing.T) {
	custom := func(string) string { return "/Volumes/Fast/DerivedData" }
	none := func(string) string { return "" }

	path, chosen := DerivedData("/home", custom)
	assert.Equal(t, "/Volumes/Fast/DerivedData", path)
	assert.True(t, chosen)
	path, chosen = DerivedData("/home", none)
	assert.Equal(t, "/home/Library/Developer/Xcode/DerivedData", path)
	assert.False(t, chosen)
}

func TestInAChosenFolderOnlyWhatXcodeMadeGoes(t *testing.T) {
	derivedData := newDerivedData(t)
	project := filepath.Join(derivedData, "App-abcdefghijklmnopqrstuvwxyzab")
	require.NoError(t, os.MkdirAll(project, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(project, "info.plist"), []byte("x"), 0o644))
	own := filepath.Join(derivedData, "notes-abcdefghijklmnopqrstuvwxyzab")
	require.NoError(t, os.MkdirAll(own, 0o755))

	r, _, stderr := run(Step{DerivedData: derivedData, SharedWithUser: true, Processes: "xcode", Running: idle}, false)

	assert.NoDirExists(t, project)
	assert.NoDirExists(t, filepath.Join(derivedData, "ModuleCache.noindex"))
	assert.DirExists(t, filepath.Join(derivedData, "App-123"))
	assert.DirExists(t, own)
	assert.Equal(t, 2, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+own+": not made by Xcode")
}
