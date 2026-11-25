package rule

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const valid = `order: 10
describes: the cache of some tool
readme:
  removes: "the cache of some tool in {root}"
root: $SOME_CACHE or $SOME_HOME/cache or ~/Library/Caches/some
busy:
  processes: [some-build]
  groups: [xcode]
  says: some build is running
remove:
  folders: ['*/old']
`

func at(home string, variables map[string]string) Environment {
	return Environment{
		Home:     home,
		Getenv:   func(key string) string { return variables[key] },
		Duration: func(string) time.Duration { return 0 },
	}
}

func parse(t *testing.T, name, body string) File {
	f, err := Parse(name, []byte(body))
	require.NoError(t, err)
	return f
}

func TestAValidFileBecomesARule(t *testing.T) {
	r := parse(t, "some-cache.yaml", valid).Rule(at("/Users/me", nil))

	assert.Equal(t, "/Users/me/Library/Caches/some", r.Root)
	assert.Equal(t, "some-build|"+groups["xcode"], r.Busy.Processes)
	assert.Equal(t, "some build is running", r.Busy.Says)
	assert.Equal(t, []string{"*/old"}, r.Folders)
}

func TestTheNameComesFromTheFileAndTheLabelFromItsFirstWord(t *testing.T) {
	f := parse(t, "builtin/some-cache.yaml", valid)

	assert.Equal(t, "some-cache", f.Name)
	assert.Equal(t, "SOME", f.Label())
	assert.Equal(t, "SOME", f.Rule(at("/Users/me", nil)).Label)
}

func TestTheRootIsTheFirstChoiceWhoseVariableIsSet(t *testing.T) {
	f := parse(t, "some-cache.yaml", valid)

	assert.Equal(t, "/opt/some/cache", f.Rule(at("/Users/me", map[string]string{"SOME_HOME": "/opt/some"})).Root)
	assert.Equal(t, "/var/some", f.Rule(at("/Users/me", map[string]string{"SOME_CACHE": "/var/some", "SOME_HOME": "/opt/some"})).Root)
}

func TestRemoveRootTakesTheRootWhole(t *testing.T) {
	r := parse(t, "tmp.yaml", "order: 1\ndescribes: x\nreadme: {removes: x}\nroot: /private/tmp/tool\nremove: root\n").Rule(at("/Users/me", nil))

	assert.Equal(t, "/private/tmp/tool", r.Root)
	assert.Empty(t, r.Folders)
}

func TestAStepWrittenInCodeDeclaresItsSettings(t *testing.T) {
	f := parse(t, "xcode-archives.yaml", `order: 80
describes: old Xcode archives, the newest of each app kept
readme:
  removes: archives older than {min-age}
settings:
  - key: min-age
    kind: duration
    default: 15d
    help: remove archives older than this
  - key: keep
    kind: count
    default: "1"
    help: keep this many newest archives of each app
run: code
`)

	step := f.Step()

	assert.Equal(t, "code", f.Run)
	assert.Equal(t, "xcode-archives", step.Name)
	assert.False(t, step.Reversible)
	require.Len(t, step.Settings, 2)
	assert.Equal(t, config.Setting{Key: "min-age", Kind: config.Duration, Default: "15d", Help: "remove archives older than this"}, step.Settings[0])
	assert.Equal(t, config.Count, step.Settings[1].Kind)
	assert.Equal(t, "archives older than `min-age` (`15d`)", f.Explain(f.Readme.Removes))
}

func TestTheRootMentionedInTheREADMEReadsAsWritten(t *testing.T) {
	f := parse(t, "some-cache.yaml", valid)

	assert.Equal(t, "the cache of some tool in `$SOME_CACHE or $SOME_HOME/cache or ~/Library/Caches/some`", f.Explain(f.Readme.Removes))
}

func TestAMistakeInAFileIsNamed(t *testing.T) {
	head := "order: 1\ndescribes: x\nreadme: {removes: x}\n"
	code := "order: 1\ndescribes: x\nreadme: {removes: x}\nrun: code\n"
	bare := "order: 1\ndescribes: x\nrun: code\n"
	cases := []struct {
		name, file, body, want string
	}{
		{"unknown key", "a.yaml", head + "root: ~/a\nremove: root\ntargets: [x]\n", "field targets not found"},
		{"a name in the file", "a.yaml", "name: a\n" + head + "root: ~/a\nremove: root\n", "field name not found"},
		{"a label in the file", "a.yaml", "label: A\n" + head + "root: ~/a\nremove: root\n", "field label not found"},
		{"file name not in words", "A_b.yaml", head + "root: ~/a\nremove: root\n", "the name of the file: want lowercase words"},
		{"no describes", "a.yaml", "order: 1\nroot: ~/a\nremove: root\n", "describes: want"},
		{"no order", "a.yaml", "describes: x\nroot: ~/a\nremove: root\n", "order: want a whole number above 0"},
		{"run something else", "a.yaml", "order: 1\ndescribes: x\nreadme: {removes: x}\nrun: a\n", `run "a": want code`},
		{"code with a root", "a.yaml", code + "root: ~/a\n", "belong to it there"},
		{"code with keep", "a.yaml", code + "keep: {open: true}\n", "belong to it there"},
		{"setting of an unknown kind", "a.yaml", code + "settings: [{key: wait, kind: minutes, default: '1', help: x}]\n", "want one of count, duration, timeout"},
		{"setting with a bad default", "a.yaml", code + "settings: [{key: wait, kind: duration, default: soon, help: x}]\n", "settings: wait: default: want a duration"},
		{"setting named enabled", "a.yaml", code + "settings: [{key: enabled, kind: count, default: '1', help: x}]\n", "other than enabled"},
		{"setting without help", "a.yaml", code + "settings: [{key: wait, kind: duration, default: 1h}]\n", "settings: wait: help: want"},
		{"no readme", "a.yaml", bare, "readme: removes: want"},
		{"readme mentions a setting it lacks", "a.yaml", bare + "readme: {removes: 'older than {min-age}'}\n", "readme: {min-age}: want"},
		{"readme mentions the root of code", "a.yaml", bare + "readme: {removes: 'in {root}'}\n", "readme: {root}: want"},
		{"no root", "a.yaml", head + "remove: root\n", "root: want a folder"},
		{"variable last", "a.yaml", head + "root: ~/a or $A_HOME\nremove: root\n", "the last choice must be a path"},
		{"relative path", "a.yaml", head + "root: Library/a\nremove: root\n", "want $VARIABLE"},
		{"no remove", "a.yaml", head + "root: ~/a\n", "remove: want root, or one of folders"},
		{"remove something else", "a.yaml", head + "root: ~/a\nremove: all\n", "remove: want root, or one of folders"},
		{"remove folders and files", "a.yaml", head + "root: ~/a\nremove: {folders: [x], files: [y]}\n", "remove: want root, or one of folders"},
		{"remove with an unknown key", "a.yaml", head + "root: ~/a\nremove: {folder: [x]}\n", "remove: want root, or one of folders"},
		{"no folders", "a.yaml", head + "root: ~/a\nremove: {folders: []}\n", "want at least one pattern"},
		{"folder leaves the root", "a.yaml", head + "root: ~/a\nremove: {folders: ['../b']}\n", "want a pattern inside the root"},
		{"a folder after any depth", "a.yaml", head + "root: ~/a\nremove: {files: ['**/logs/*.log']}\n", "or **/ and a name pattern"},
		{"files deep in the middle", "a.yaml", head + "root: ~/a\nremove: {files: ['a/**/b']}\n", "want a pattern inside the root, like logs/*.log"},
		{"keep without files", "a.yaml", head + "root: ~/a\nremove: root\nkeep: {open: true}\n", "keep: want remove: files"},
		{"keep by an unknown age", "a.yaml", head + "root: ~/a\nremove: {files: ['**/*.log']}\nkeep: {written-within: min-age}\n", `keep: written-within "min-age": want`},
		{"keep with an unknown key", "a.yaml", head + "root: ~/a\nremove: {files: ['**/*.log']}\nkeep: {opened: true}\n", "field opened not found"},
		{"processes without says", "a.yaml", head + "root: ~/a\nbusy: {processes: [a]}\nremove: root\n", "together with says"},
		{"says without processes", "a.yaml", head + "root: ~/a\nbusy: {says: a runs}\nremove: root\n", "together with says"},
		{"bad pattern", "a.yaml", head + "root: ~/a\nbusy: {processes: ['('], says: a runs}\nremove: root\n", "missing closing )"},
		{"unknown group", "a.yaml", head + "root: ~/a\nbusy: {groups: [eclipse], says: a runs}\nremove: root\n", "want one of xcode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.file, []byte(tc.body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.file+": ")
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoadNamesEveryBadFileAndKeepsTheGoodOnes(t *testing.T) {
	fsys := fstest.MapFS{
		"some-cache.yaml": {Data: []byte(valid)},
		"a.yaml":          {Data: []byte("order: 1\n")},
		"c.yaml":          {Data: []byte("nope: 1\n")},
		"notes.txt":       {Data: []byte("not a rule")},
	}

	files, err := Load(fsys)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "a.yaml: ")
	assert.Contains(t, err.Error(), "c.yaml: ")
	require.Len(t, files, 1)
	assert.Equal(t, "some-cache", files[0].Name)
}
