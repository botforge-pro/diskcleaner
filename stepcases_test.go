package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/rule"
)

type stepCase struct {
	Case    string            `yaml:"case"`
	Env     map[string]string `yaml:"env"`
	Running []string          `yaml:"running"`
	Files   map[string]string `yaml:"files"`
	Open    []string          `yaml:"open"`
	Tracked []string          `yaml:"tracked"`
	Removes *[]string         `yaml:"removes"`
	Says    []string          `yaml:"says"`
}

func TestEveryDeclaredStepHasATestFile(t *testing.T) {
	for _, info := range catalog {
		if info.file.Run != "" {
			continue
		}
		_, err := fs.Stat(builtinFiles, "builtin/"+info.Name+rule.TestSuffix)
		assert.NoError(t, err, "%s runs from its file, so its cases belong in %s%s", info.Name, info.Name, rule.TestSuffix)
	}
}

func TestEachStepDoesWhatItsTestFileSays(t *testing.T) {
	names, err := fs.Glob(builtinFiles, "builtin/*"+rule.TestSuffix)
	require.NoError(t, err)
	require.NotEmpty(t, names)
	for _, name := range names {
		step := strings.TrimSuffix(filepath.Base(name), rule.TestSuffix)
		t.Run(step, func(t *testing.T) {
			data, err := fs.ReadFile(builtinFiles, name)
			require.NoError(t, err)
			decoder := yaml.NewDecoder(bytes.NewReader(data))
			decoder.KnownFields(true)
			var cases []stepCase
			require.NoError(t, decoder.Decode(&cases), name)
			require.NotEmpty(t, cases, name)
			f := builtinFile(t, step)
			for _, c := range cases {
				t.Run(c.Case, func(t *testing.T) { c.run(t, f) })
			}
		})
	}
}

func builtinFile(t *testing.T, name string) rule.File {
	data, err := fs.ReadFile(builtinFiles, "builtin/"+name+".yaml")
	require.NoError(t, err, "a test file for a step that has no file")
	f, err := rule.Parse(name+".yaml", data)
	require.NoError(t, err)
	return f
}

func (c stepCase) run(t *testing.T, f rule.File) {
	require.NotEmpty(t, c.Case, "every case is named")
	require.NotEmpty(t, c.Files, "%s: files", c.Case)
	require.NotNil(t, c.Removes, "%s: removes: name what goes, or [] for nothing", c.Case)
	for _, path := range append(slices.Clone(c.Open), c.Tracked...) {
		require.Contains(t, c.Files, path, "%s: open and tracked name paths under files", c.Case)
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	now := time.Now()
	for path, age := range c.Files {
		written := now.Add(-c.age(t, age))
		full := filepath.Join(home, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte("x"), 0o644))
		require.NoError(t, os.Chtimes(full, written, written))
	}
	if len(c.Tracked) > 0 {
		track(t, home, c.Tracked)
	}

	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr}
	f.Rule(rule.Environment{
		Home:     home,
		Getenv:   func(key string) string { return c.variable(home, key) },
		Running:  c.running,
		Duration: func(key string) time.Duration { return settingsDefault(t, f.Name+"."+key) },
		Now:      time.Now,
		Lsof:     inuse.Lsof{Command: c.lsof(t, home), TimeoutSeconds: 5},
	}).Run(r)

	assert.Empty(t, r.Failures, "%s", stderr.String())
	for path := range c.Files {
		full := filepath.Join(home, path)
		if c.removed(path) {
			assert.NoFileExists(t, full)
		} else {
			assert.FileExists(t, full, "only removes: may go")
		}
	}
	for _, path := range *c.Removes {
		_, err := os.Lstat(filepath.Join(home, path))
		assert.ErrorIs(t, err, os.ErrNotExist, "removes: %s", path)
	}
	for _, said := range c.Says {
		assert.Contains(t, stderr.String(), said)
	}
}

func (c stepCase) age(t *testing.T, text string) time.Duration {
	if text == "" {
		return 0
	}
	value, err := config.Parse(config.Setting{Key: "age", Kind: config.Duration}, text, "/")
	require.NoError(t, err, "%s: files: age %q", c.Case, text)
	return value.(time.Duration)
}

func (c stepCase) variable(home, key string) string {
	value := c.Env[key]
	if rest, ok := strings.CutPrefix(value, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return value
}

func (c stepCase) running(pattern string) (bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(c.Running, re.MatchString), nil
}

func (c stepCase) removed(path string) bool {
	return slices.ContainsFunc(*c.Removes, func(gone string) bool {
		return path == gone || strings.HasPrefix(path, gone+"/")
	})
}

func (c stepCase) lsof(t *testing.T, home string) string {
	listing := "n/\n"
	for _, path := range c.Open {
		listing += "n" + filepath.Join(home, path) + "\n"
	}
	script := filepath.Join(t.TempDir(), "lsof")
	body := fmt.Sprintf("#!/bin/sh\ncat <<'LISTING'\np1\n%sLISTING\n", listing)
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))
	return script
}

func track(t *testing.T, home string, paths []string) {
	for _, args := range [][]string{
		{"init", "-q"},
		append([]string{"add", "--"}, paths...),
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "fixture"},
	} {
		output, err := exec.Command("git", append([]string{"-C", home}, args...)...).CombinedOutput()
		require.NoError(t, err, string(output))
	}
}
