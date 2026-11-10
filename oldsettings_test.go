package main

import (
	"bytes"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnOldVariableStopsTheRunAndSaysWhatToWriteInstead(t *testing.T) {
	cleanLock(t)
	t.Setenv("DISKCLEANER_TMP_MINUTES", "5")
	t.Setenv("DISKCLEANER_CLAUDE_LOG_DAYS", "30")
	t.Setenv("DISKCLEANER_SWIFTPM_CACHE", "/cache")
	var stdout, stderr bytes.Buffer

	status := run([]string{"clean", "--dry-run"}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 2, status)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "diskcleaner no longer reads DISKCLEANER_* variables; unset these:\n"+
		"DISKCLEANER_CLAUDE_LOG_DAYS: write --claude-logs.min-age=31d, or the same in the config file\n"+
		"DISKCLEANER_SWIFTPM_CACHE: no longer a setting\n"+
		"DISKCLEANER_TMP_MINUTES: write --min-age=5m, or the same in the config file\n", stderr.String())
}

func TestEveryReplacementIsASettingThatTakesTheValueItSuggests(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	for name, r := range replacedSettings {
		old := "7"
		if r.key == "project-dirs" {
			old = home
		}
		set := pflag.NewFlagSet("test", pflag.ContinueOnError)
		settingsTable.Flags(set)
		require.NoError(t, set.Parse([]string{"--" + r.key + "=" + r.value(old)}), name)

		_, err := config.Load(settingsTable, set, home)

		assert.NoError(t, err, name)
	}
}
