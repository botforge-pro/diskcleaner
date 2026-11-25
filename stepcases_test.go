package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/rule"
)

type stepCase struct {
	Case     string                 `yaml:"case"`
	Env      map[string]string      `yaml:"env"`
	Running  []fakeProcess          `yaml:"running"`
	Commands map[string]fakeCommand `yaml:"commands"`
	Files    map[string]fakeFile    `yaml:"files"`
	Links    map[string]string      `yaml:"links"`
	Settings map[string]string      `yaml:"settings"`
	Open     []string               `yaml:"open"`
	Tracked  []string               `yaml:"tracked"`
	Later    string                 `yaml:"later"`
	Removes  *[]string              `yaml:"removes"`
	Says     []string               `yaml:"says"`
	Shows    []string               `yaml:"shows"`
	Fails    []string               `yaml:"fails"`
	DryRun   bool                   `yaml:"dry-run"`
	Counts   *int                   `yaml:"counts"`
	Frees    string                 `yaml:"frees"`
}

func TestEveryDeclaredStepHasATestFile(t *testing.T) {
	for _, info := range catalog {
		_, err := fs.Stat(builtinFiles, "builtin/"+info.Name+rule.TestSuffix)
		assert.NoError(t, err, "%s: its cases belong in %s%s", info.Name, info.Name, rule.TestSuffix)
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
	c = c.withUID()
	require.NotNil(t, c.Removes, "%s: removes: name what goes, or [] for nothing", c.Case)
	if len(*c.Removes) == 0 {
		require.True(t, len(c.Says) > 0 || len(c.Fails) > 0 || len(c.Shows) > 0 || c.Counts != nil,
			"%s: removes: []: say what shows that the step ran and kept everything: says, fails, shows or counts", c.Case)
	}
	for _, gone := range *c.Removes {
		require.True(t, slices.ContainsFunc(slices.Collect(maps.Keys(c.Files)), func(path string) bool {
			return path == gone || strings.HasPrefix(path, gone+"/")
		}), "%s: removes: %s is not under files, so nothing would show that it went", c.Case, gone)
	}
	for key, value := range c.Env {
		require.True(t, value == "" || strings.HasPrefix(value, "~/"), "%s: env: %s: want a path under the test's home, starting with ~/", c.Case, key)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, path := range append(slices.Clone(c.Open), c.Tracked...) {
		require.Contains(t, c.Files, path, "%s: open and tracked name paths under files", c.Case)
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	now := time.Now()
	if c.Later != "" {
		now = now.Add(c.age(t, c.Later))
	}
	folders := map[string]time.Time{}
	for path, file := range c.Files {
		written := now.Add(-c.age(t, file.Age))
		full := filepath.Join(home, path)
		if strings.HasSuffix(path, "/") {
			require.NoError(t, os.MkdirAll(full, 0o755))
			folders[filepath.Clean(full)] = written
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		text := ago.ReplaceAllStringFunc(file.Text, func(found string) string {
			return now.Add(-c.age(t, ago.FindStringSubmatch(found)[1])).UTC().Format(time.RFC3339)
		})
		if text == "" {
			text = "x"
		}
		require.NoError(t, os.WriteFile(full, []byte(text), 0o644))
		require.NoError(t, os.Chtimes(full, written, written))
	}
	for link, target := range c.Links {
		full := filepath.Join(home, link)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.Symlink(filepath.Join(home, target), full))
	}
	repos := c.repositories()
	for _, repo := range repos {
		gitIn(t, filepath.Join(home, repo), "init", "-q")
	}
	if len(c.Tracked) > 0 {
		track(t, home, repos, c.Tracked)
	}
	ageFolders(t, home, folders)

	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: c.DryRun}
	f.Rule(rule.Environment{
		Home:      home,
		Under:     filepath.Join(home, "fs"),
		Getenv:    func(key string) string { return c.variable(home, key) },
		Processes: newProcessTable(home, c.Running).processes(),
		Commands:  c.commands(t, home),
		Duration:  func(key string) time.Duration { return c.setting(t, key).(time.Duration) },
		Count:     func(key string) int { return c.setting(t, key).(int) },
		Dirs:      func(key string) []string { return c.folders(t, key, home) },
		Now:       func() time.Time { return now },
		Lsof:      inuse.Lsof{Command: c.lsof(t, home), TimeoutSeconds: 5},
		Mounts:    rule.MountPoints,
	}).Run(r)

	if len(c.Fails) == 0 {
		assert.Empty(t, r.Failures, "%s", stderr.String())
	}
	for _, failed := range c.Fails {
		assert.True(t, slices.ContainsFunc(r.Failures, func(f string) bool { return strings.Contains(f, failed) }), "fails: %q not among %q", failed, r.Failures)
	}
	if c.Counts != nil {
		assert.Equal(t, *c.Counts, r.Count, "counts")
	}
	if c.Frees != "" {
		assert.Equal(t, c.Frees, report.FormatKB(r.KB), "frees")
	}
	for _, shown := range c.Shows {
		assert.Contains(t, stdout.String(), shown)
	}
	for path := range c.Files {
		full := filepath.Join(home, path)
		folder := strings.HasSuffix(path, "/")
		switch removed := c.removed(strings.TrimSuffix(path, "/")); {
		case removed && folder:
			assert.NoDirExists(t, full)
		case removed:
			assert.NoFileExists(t, full)
		case folder:
			assert.DirExists(t, full, "only removes: may go")
		default:
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

func ageFolders(t *testing.T, home string, explicit map[string]time.Time) {
	var dirs []string
	require.NoError(t, filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		if entry.IsDir() && path != home {
			dirs = append(dirs, path)
		}
		return nil
	}))
	slices.SortFunc(dirs, func(a, b string) int { return strings.Count(b, "/") - strings.Count(a, "/") })
	for _, dir := range dirs {
		written, ok := explicit[dir]
		if !ok {
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			for _, entry := range entries {
				info, err := entry.Info()
				require.NoError(t, err)
				if info.ModTime().After(written) {
					written = info.ModTime()
				}
			}
		}
		if !written.IsZero() {
			require.NoError(t, os.Chtimes(dir, written, written))
		}
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

func (c stepCase) withUID() stepCase {
	uid := strings.NewReplacer("{uid}", strconv.Itoa(os.Getuid()))
	files := map[string]fakeFile{}
	for path, file := range c.Files {
		files[uid.Replace(path)] = file
	}
	links := map[string]string{}
	for link, target := range c.Links {
		links[uid.Replace(link)] = uid.Replace(target)
	}
	each := func(paths []string) []string {
		out := make([]string, len(paths))
		for i, path := range paths {
			out[i] = uid.Replace(path)
		}
		return out
	}
	c.Files, c.Links, c.Open, c.Tracked = files, links, each(c.Open), each(c.Tracked)
	if c.Removes != nil {
		removes := each(*c.Removes)
		c.Removes = &removes
	}
	return c
}

func (c stepCase) setting(t *testing.T, key string) any {
	text, ok := c.Settings[key]
	if !ok {
		return settingsDefault(t, key)
	}
	s, found := settingsTable.Find(key)
	require.True(t, found, "%s: settings: %s is no setting", c.Case, key)
	value, err := config.Parse(s, text, "/")
	require.NoError(t, err, "%s: settings: %s", c.Case, key)
	return value
}

func (c stepCase) folders(t *testing.T, key, home string) []string {
	s, found := settingsTable.Find(key)
	require.True(t, found, "%s: settings: %s is no setting", c.Case, key)
	text := c.Settings[key]
	if text == "" {
		text = s.Default
	}
	value, err := config.Parse(s, text, home)
	require.NoError(t, err, "%s: settings: %s", c.Case, key)
	return value.([]string)
}

var ago = regexp.MustCompile(`\{ago ([^}]+)\}`)

type fakeFile struct {
	Age  string `yaml:"age"`
	Text string `yaml:"text"`
}

func (f *fakeFile) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		f.Age = node.Value
		return nil
	}
	type plain fakeFile
	return node.Decode((*plain)(f))
}

type fakeCommand struct {
	Stdout  string   `yaml:"stdout"`
	Stderr  string   `yaml:"stderr"`
	Exit    int      `yaml:"exit"`
	Hangs   bool     `yaml:"hangs"`
	Removes []string `yaml:"removes"`
}

func (c stepCase) commands(t *testing.T, home string) rule.Commands {
	inHome := strings.NewReplacer("~/", home+"/")
	return rule.Commands{
		LookPath: func(name string) (string, error) {
			for line := range c.Commands {
				if line == name || strings.HasPrefix(line, name+" ") {
					return "/usr/bin/" + name, nil
				}
			}
			return "", exec.ErrNotFound
		},
		Run: func(_ time.Duration, name string, args ...string) process.Result {
			line := strings.ReplaceAll(strings.Join(append([]string{name}, args...), " "), home+"/", "~/")
			fake, ok := c.Commands[line]
			if !ok {
				return process.Result{Err: fmt.Errorf("exit status 127"), Stderr: "not expected in this case: " + line}
			}
			for _, path := range fake.Removes {
				require.NoError(t, os.RemoveAll(filepath.Join(home, path)))
			}
			result := process.Result{Stdout: inHome.Replace(fake.Stdout), Stderr: inHome.Replace(fake.Stderr), TimedOut: fake.Hangs}
			if fake.Exit != 0 || fake.Hangs {
				result.Err = fmt.Errorf("exit status %d", fake.Exit)
			}
			return result
		},
	}
}

type fakeProcess struct {
	Command string `yaml:"command"`
	Busy    bool   `yaml:"busy"`
	Stays   bool   `yaml:"stays"`
}

func (p *fakeProcess) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		p.Command = node.Value
		return nil
	}
	type plain fakeProcess
	return node.Decode((*plain)(p))
}

type processTable struct {
	running []fakeProcess
	cpu     int
}

func newProcessTable(home string, running []fakeProcess) *processTable {
	table := &processTable{}
	for _, p := range running {
		p.Command = strings.ReplaceAll(p.Command, "~/", home+"/")
		table.running = append(table.running, p)
	}
	return table
}

func (t *processTable) matching(pattern string) ([]int, error) {
	re, err := regexp.CompilePOSIX(pattern)
	if err != nil {
		return nil, err
	}
	var found []int
	for i, p := range t.running {
		if re.MatchString(p.Command) {
			found = append(found, i)
		}
	}
	return found, nil
}

func (t *processTable) processes() rule.Processes {
	return rule.Processes{
		Running: func(pattern string) (bool, error) {
			found, err := t.matching(pattern)
			return len(found) > 0, err
		},
		Pids: func(pattern string) ([]string, error) {
			found, err := t.matching(pattern)
			var pids []string
			for _, i := range found {
				pids = append(pids, fmt.Sprint(i+1))
			}
			return pids, err
		},
		CPUTimes: func(pids []string) (string, error) {
			t.cpu++
			var lines []string
			for _, pid := range pids {
				var i int
				fmt.Sscan(pid, &i)
				used := 0
				if t.running[i-1].Busy {
					used = t.cpu
				}
				lines = append(lines, fmt.Sprintf("%s 0:%02d.00", pid, used))
			}
			return strings.Join(lines, "\n"), nil
		},
		Terminate: func(pattern string) error {
			found, err := t.matching(pattern)
			var left []fakeProcess
			for i, p := range t.running {
				if p.Stays || !slices.Contains(found, i) {
					left = append(left, p)
				}
			}
			t.running = left
			return err
		},
		Sleep: func(time.Duration) {},
	}
}

func (c stepCase) removed(path string) bool {
	return slices.ContainsFunc(*c.Removes, func(gone string) bool {
		return path == gone || strings.HasPrefix(path, gone+"/")
	})
}

func (c stepCase) lsof(t *testing.T, home string) string {
	listing := "fcwd\nn/\n"
	for _, path := range c.Open {
		full := filepath.Join(home, path)
		info, err := os.Lstat(full)
		require.NoError(t, err, "%s: open: %s", c.Case, path)
		stat := info.Sys().(*syscall.Stat_t)
		listing += fmt.Sprintf("f3\nD0x%x\ni%d\nn%s\n", uint64(stat.Dev), stat.Ino, full)
	}
	script := filepath.Join(t.TempDir(), "lsof")
	body := fmt.Sprintf("#!/bin/sh\ncat <<'LISTING'\np1\n%sLISTING\n", listing)
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))
	return script
}

func (c stepCase) repositories() []string {
	var repos []string
	for path := range c.Files {
		if repo, ok := strings.CutSuffix(path, ".git/"); ok {
			repos = append(repos, strings.TrimSuffix(repo, "/"))
		}
	}
	slices.Sort(repos)
	return repos
}

func track(t *testing.T, home string, repos, paths []string) {
	byRepo := map[string][]string{}
	for _, path := range paths {
		holder := ""
		for _, repo := range repos {
			if strings.HasPrefix(path, repo+"/") && len(repo) > len(holder) {
				holder = repo
			}
		}
		if holder == "" {
			gitIn(t, home, "init", "-q")
		}
		rel, err := filepath.Rel(filepath.Join(home, holder), filepath.Join(home, path))
		require.NoError(t, err)
		byRepo[holder] = append(byRepo[holder], rel)
	}
	for repo, rels := range byRepo {
		dir := filepath.Join(home, repo)
		gitIn(t, dir, append([]string{"add", "-f", "--"}, rels...)...)
		gitIn(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "fixture")
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(output))
}
