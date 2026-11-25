package main

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/rule"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func settingsDefault(t *testing.T, key string) time.Duration {
	s, ok := settingsTable.Find(key)
	require.True(t, ok, key)
	value, err := config.Parse(s, s.Default, "/")
	require.NoError(t, err)
	return value.(time.Duration)
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

func TestACatalogThatCannotRunAsWrittenIsRefused(t *testing.T) {
	code := map[string]construct{"a": nil, "unused": nil}
	files := []rule.File{
		{Name: "a", Order: 10, Run: "code"},
		{Name: "b", Order: 10, Run: "code"},
	}

	err := checkCatalog(files, code)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "b.yaml: order 10: a.yaml has it too")
	assert.Contains(t, err.Error(), "b.yaml: run: code, but no step of this name is written in Go")
	assert.Contains(t, err.Error(), "the step unused is written in code, but no file runs it")
}
