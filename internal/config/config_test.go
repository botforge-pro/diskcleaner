package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var steps = []Step{
	{Name: "go-cache", Undoable: true},
	{Name: "xcode-archives", Settings: []Setting{
		{"min-age", Duration, "15d", "remove archives older than this"},
		{"keep", Count, "1", "keep this many newest archives of each app"},
		{"timeout", Timeout, "30s", "give up after this"},
	}},
}

func load(t *testing.T, home string, args ...string) (*Config, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", "")
	table := NewTable(steps)
	set := pflag.NewFlagSet("clean", pflag.ContinueOnError)
	table.Flags(set)
	require.NoError(t, set.Parse(args))
	return Load(table, set, home)
}

func writeConfig(t *testing.T, home, text string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "diskcleaner.toml"), []byte(text), 0o644))
}

func TestEverySettingHasAFlagAndHelp(t *testing.T) {
	table := NewTable(steps)
	set := pflag.NewFlagSet("clean", pflag.ContinueOnError)
	table.Flags(set)
	for _, s := range table {
		flag := set.Lookup(s.Key)
		require.NotNil(t, flag, s.Key)
		assert.NotEmpty(t, flag.Usage, s.Key)
	}
}

func TestWithoutAFileEverySettingIsItsDefault(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, "Developer"), 0o755))

	c, err := load(t, home)

	require.NoError(t, err)
	assert.Equal(t, time.Hour, c.Duration("min-age"))
	assert.Equal(t, int64(0), c.Size("min-free"))
	assert.True(t, c.Bool("go-cache.enabled"))
	assert.False(t, c.Bool("xcode-archives.enabled"), "a step whose deletions cannot be undone is off")
	assert.Equal(t, 15*24*time.Hour, c.Duration("xcode-archives.min-age"))
	assert.Equal(t, []string{filepath.Join(home, "Developer")}, c.Dirs("project-dirs"), "only the folders that exist")
	assert.Equal(t, FromDefault, c.Source("min-age"))
}

func TestTheFileOverridesTheDefaultAndAFlagTheFile(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, "min-age = \"2h\"\nmin-free = \"5GB\"\nproject-dirs = [\"~/work\"]\n\n"+
		"[xcode-archives]\nenabled = true\nkeep = 3\n")

	c, err := load(t, home, "--min-age=0", "--xcode-archives.keep", "2")

	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), c.Duration("min-age"))
	assert.Equal(t, FromFlag, c.Source("min-age"))
	assert.Equal(t, int64(5<<30), c.Size("min-free"))
	assert.Equal(t, FromFile, c.Source("min-free"))
	assert.True(t, c.Bool("xcode-archives.enabled"))
	assert.Equal(t, 2, c.Count("xcode-archives.keep"))
	assert.Equal(t, []string{filepath.Join(home, "work")}, c.Dirs("project-dirs"))
}

func TestABoolFlagWithoutAValueTurnsTheStepOn(t *testing.T) {
	c, err := load(t, t.TempDir(), "--xcode-archives.enabled")

	require.NoError(t, err)
	assert.True(t, c.Bool("xcode-archives.enabled"))
}

func TestFlagsTakeAListOfFolders(t *testing.T) {
	home := t.TempDir()

	c, err := load(t, home, "--project-dirs=~/a, /b")

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(home, "a"), "/b"}, c.Dirs("project-dirs"))
}

func TestConfigFlagNamesAnotherFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.toml")
	require.NoError(t, os.WriteFile(path, []byte("min-age = \"3d\"\n"), 0o644))

	c, err := load(t, t.TempDir(), "--config", path)

	require.NoError(t, err)
	assert.Equal(t, 3*24*time.Hour, c.Duration("min-age"))
	assert.Equal(t, path, c.File)
}

func TestXDGConfigHomeIsWhereTheFileIs(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "diskcleaner.toml"), []byte("min-age = \"5m\"\n"), 0o644))
	table := NewTable(steps)
	set := pflag.NewFlagSet("clean", pflag.ContinueOnError)
	table.Flags(set)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	c, err := Load(table, set, home)

	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, c.Duration("min-age"))
}

func TestAnUnknownKeyInTheFileIsRefused(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, "[xcode-archives]\nmin-aeg = \"1d\"\n")

	_, err := load(t, home)

	assert.ErrorContains(t, err, `unknown setting "xcode-archives.min-aeg"`)
}

func TestAWrongValueNamesItsSetting(t *testing.T) {
	for args, want := range map[string]string{
		"--min-age=1x":               `min-age: want a duration like 1h or 15d, got "1x"`,
		"--min-free=lots":            `min-free: want a size like 500MB or 5GB, got "lots"`,
		"--xcode-archives.keep=-1":   `xcode-archives.keep: want a whole number, got "-1"`,
		"--go-cache.enabled=yes":     `go-cache.enabled: want true or false, got "yes"`,
		"--xcode-archives.timeout=0": `xcode-archives.timeout: want a timeout above 0, got "0"`,
	} {
		_, err := load(t, t.TempDir(), args)

		assert.EqualError(t, err, want, args)
	}
}
