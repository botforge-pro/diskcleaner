package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpIsNoError(t *testing.T) {
	for _, command := range []string{"clean", "config"} {
		var stdout, stderr bytes.Buffer

		status := run([]string{command, "--help"}, &stdout, &stderr, realSteps)

		assert.Equal(t, 0, status, command)
		assert.Contains(t, stderr.String(), "--min-age duration", command)
		assert.Contains(t, stderr.String(), "--homebrew.enabled ", command)
	}
}

func TestConfigPrintsEachSettingWithItsSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config"), 0o755))
	path := filepath.Join(home, ".config", "diskcleaner.toml")
	require.NoError(t, os.WriteFile(path, []byte("[xcode-archives]\nmin-age = \"30d\"\n"), 0o644))
	var stdout, stderr bytes.Buffer

	status := run([]string{"config", "--min-age=2h"}, &stdout, &stderr, realSteps)

	assert.Equal(t, 0, status, stderr.String())
	lines := strings.Split(stdout.String(), "\n")
	assert.Equal(t, "file: "+path, lines[0])
	assert.Equal(t, []string{"min-age", "2h", "flag"}, fieldsOf(lines, "min-age"))
	assert.Equal(t, []string{"xcode-archives.min-age", "30d", "file"}, fieldsOf(lines, "xcode-archives.min-age"))
	assert.Equal(t, []string{"homebrew.enabled", "false", "default"}, fieldsOf(lines, "homebrew.enabled"))
}

func TestConfigRefusesAWrongValue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	var stdout, stderr bytes.Buffer

	status := run([]string{"config", "--min-age=soon"}, &stdout, &stderr, realSteps)

	assert.Equal(t, 2, status)
	assert.Equal(t, "min-age: want a duration like 1h or 15d, got \"soon\"\n", stderr.String())
}

var realSteps = registry{names: stepNames(), table: settingsTable, find: steps}

func fieldsOf(lines []string, key string) []string {
	for _, line := range lines {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == key {
			return fields
		}
	}
	return nil
}
