package main

import (
	"io/fs"
	"path/filepath"
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
		if strings.HasSuffix(name, rule.TestSuffix) {
			continue
		}
		step := filepath.Base(name[:len(name)-len(".yaml")])
		assert.Contains(t, stepNames(), step, "%s is loaded but no step runs it", name)
	}
}

func TestTwoStepsOfOneOrderAreRefused(t *testing.T) {
	files := []rule.File{{Name: "a", Order: 10}, {Name: "b", Order: 10}}

	err := checkCatalog(files)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "b.yaml: order 10: a.yaml has it too")
}
