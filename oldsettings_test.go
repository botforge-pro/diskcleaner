package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnOldVariableStopsTheRunAndNamesItsReplacement(t *testing.T) {
	cleanLock(t)
	t.Setenv("DISKCLEANER_TMP_MINUTES", "5")
	t.Setenv("DISKCLEANER_SWIFTPM_CACHE", "/cache")
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run"}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 2, status)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "unset these variables:\n"+
		"DISKCLEANER_SWIFTPM_CACHE is no longer read: diskcleaner finds that itself\n"+
		"DISKCLEANER_TMP_MINUTES is now min-age in ~/.config/diskcleaner.toml or --min-age\n", stderr.String())
}

func TestEveryReplacementIsASetting(t *testing.T) {
	for name, key := range replacedSettings {
		_, ok := settingsTable.Find(key)
		assert.True(t, ok, name)
	}
}
