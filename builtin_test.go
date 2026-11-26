package main

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/rule"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func settingsDefault(t *testing.T, key string) any {
	s, ok := settingsTable.Find(key)
	require.True(t, ok, key)
	value, err := config.Parse(s, s.Default, "/")
	require.NoError(t, err)
	return value
}

func TestEveryBuiltinFileLoadsAndIsInTheCatalog(t *testing.T) {
	names, err := fs.Glob(builtinFiles, "builtin/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, names)
	for _, name := range names {
		if strings.HasSuffix(name, rule.TestSuffix) || filepath.Base(name) == rule.GroupsFile {
			continue
		}
		base := filepath.Base(name[:len(name)-len(".yaml")])
		assert.True(t, slices.ContainsFunc(catalog, func(info stepInfo) bool { return info.file.Base == base }), "%s is loaded but no step runs it", name)
	}
}

func TestTwoFilesOfOneStepAreRefused(t *testing.T) {
	files := []rule.File{{Name: "a", Base: "010-a"}, {Name: "a", Base: "020-a"}}

	err := checkCatalog(files)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "020-a.yaml: the step a is 010-a.yaml already")
}
