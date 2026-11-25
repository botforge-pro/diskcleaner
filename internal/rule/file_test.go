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
	f := parse(t, "some-cache.yaml", valid)

	assert.Equal(t, "/Users/me/Library/Caches/some", f.Rule(at("/Users/me", nil)).Root())
	assert.Equal(t, "some-build|"+groups["xcode"], f.BusyPattern())
	assert.Equal(t, "some build is running", f.Busy.Says)
	require.Len(t, f.Remove.Folders, 1)
	assert.Equal(t, "*/old", f.Remove.Folders[0].Glob)
}

func TestTheNameComesFromTheFileAndTheLabelFromItsFirstWord(t *testing.T) {
	f := parse(t, "builtin/some-cache.yaml", valid)

	assert.Equal(t, "some-cache", f.Name)
	assert.Equal(t, "SOME", f.Label())
}

func TestTheRootIsTheFirstChoiceWhoseVariableIsSet(t *testing.T) {
	f := parse(t, "some-cache.yaml", valid)

	assert.Equal(t, "/opt/some/cache", f.Rule(at("/Users/me", map[string]string{"SOME_HOME": "/opt/some"})).Root())
	assert.Equal(t, "/var/some", f.Rule(at("/Users/me", map[string]string{"SOME_CACHE": "/var/some", "SOME_HOME": "/opt/some"})).Root())
}

func TestAnAbsoluteRootIsTakenAsWritten(t *testing.T) {
	f := parse(t, "tmp.yaml", "order: 1\ndescribes: x\nroot: /private/tmp/tool\nremove: root\n")

	assert.Equal(t, "/private/tmp/tool", f.Rule(at("/Users/me", nil)).Root())
	assert.True(t, f.Remove.Root)
}

func TestAStepWrittenInCodeDeclaresItsSettings(t *testing.T) {
	f := parse(t, "xcode-archives.yaml", `order: 80
describes: old Xcode archives, the newest of each app kept
readme:
  removes: archives older than {min-age}
  keeps: the newest {keep} of each app
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
	assert.Equal(t, "archives older than `xcode-archives.min-age` (`15d`)", f.Explain(f.Readme.Removes))
}

func TestAStepsOwnSettingWinsOverTheGlobalOfTheSameKey(t *testing.T) {
	own := parse(t, "some-logs.yaml", "order: 1\ndescribes: logs older than {min-age}\nsettings: [{key: min-age, kind: duration, default: 20d, help: x}]\nrun: code\n")
	global := parse(t, "some-builds.yaml", "order: 1\ndescribes: builds\nreadme: {keeps: 'anything written within {min-age}'}\nrun: code\n")

	assert.Equal(t, "logs older than `some-logs.min-age` (`20d`)", own.Removes())
	assert.Equal(t, "anything written within `min-age` (`1h`)", global.Explain(global.Readme.Keeps))
}

func TestTheRootMentionedInTheREADMEReadsAsItsChoicesInOrder(t *testing.T) {
	f := parse(t, "some-cache.yaml", valid)

	assert.Equal(t, "the cache of some tool in `$SOME_CACHE`, else `$SOME_HOME/cache`, else `~/Library/Caches/some`", f.Explain(f.Readme.Removes))
}

func TestAMistakeInAFileIsNamed(t *testing.T) {
	head := "order: 1\ndescribes: x\n"
	code := "order: 1\ndescribes: x\nrun: code\n"
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
		{"run something else", "a.yaml", "order: 1\ndescribes: x\nrun: a\n", `run "a": want code`},
		{"code with a root", "a.yaml", code + "root: ~/a\n", "belong to it there"},
		{"code with keep", "a.yaml", bare + "readme: {removes: 'older than {min-age}'}\nsettings: [{key: min-age, kind: duration, default: 1h, help: x}]\nkeep: {age: {of: mtime, within: min-age}}\n", "belong to it there"},
		{"a setting the readme does not name", "a.yaml", code + "settings: [{key: min-age, kind: duration, default: 1h, help: x}]\n", "want {min-age} in its text"},
		{"setting of an unknown kind", "a.yaml", code + "settings: [{key: wait, kind: minutes, default: '1', help: x}]\n", "want one of count, duration, timeout"},
		{"setting with a bad default", "a.yaml", code + "settings: [{key: wait, kind: duration, default: soon, help: x}]\n", "settings: wait: default: want a duration"},
		{"setting named enabled", "a.yaml", code + "settings: [{key: enabled, kind: count, default: '1', help: x}]\n", "other than enabled"},
		{"setting without help", "a.yaml", code + "settings: [{key: wait, kind: duration, default: 1h}]\n", "settings: wait: help: want"},
		{"readme names a global that is not an age", "a.yaml", bare + "readme: {keeps: 'in {project-dirs}'}\n", "readme: {project-dirs}: want"},
		{"readme says what describes says", "a.yaml", bare + "readme: {removes: x}\n", "leave it out and the README takes describes"},
		{"readme mentions a setting it lacks", "a.yaml", bare + "readme: {removes: 'older than {max-age}'}\n", "readme: {max-age}: want"},
		{"readme mentions the root of code", "a.yaml", bare + "readme: {removes: 'in {root}'}\n", "readme: {root}: want"},
		{"no root", "a.yaml", head + "remove: root\n", "root: want a folder"},
		{"variable last", "a.yaml", head + "root: ~/a or $A_HOME\nremove: root\n", "the last choice must not be a variable"},
		{"relative path", "a.yaml", head + "root: Library/a\nremove: root\n", "want $VARIABLE"},
		{"no remove", "a.yaml", head + "root: ~/a\n", "remove: want root, or one of folders"},
		{"remove something else", "a.yaml", head + "root: ~/a\nremove: all\n", "remove: want root, one of folders"},
		{"remove folders and files", "a.yaml", head + "root: ~/a\nremove: {folders: [x], files: [y]}\n", "remove: want root, one of folders"},
		{"remove with an unknown key", "a.yaml", head + "root: ~/a\nremove: {folder: [x]}\n", "remove: want root, one of folders"},
		{"a command without a dry run", "a.yaml", head + "remove: {run: [tool, clean]}\n", "want dry-run, estimate: run or size: measure"},
		{"a command with an unknown key", "a.yaml", head + "remove: {run: [tool, clean], dry-run: [-n], wait: 1s}\n", "field wait not found"},
		{"a command's timeout", "a.yaml", head + "remove: {run: [tool, clean], dry-run: [-n], timeout: soon}\n", `timeout "soon": want the key`},
		{"a setting nothing uses", "a.yaml", "order: 1\ndescribes: logs older than {min-age}\nsettings: [{key: min-age, kind: duration, default: 1h, help: x}]\nroot: ~/a\nremove: {files: ['**/*.log']}\n", "settings: min-age: nothing in the file uses it"},
		{"a global it does not apply", "a.yaml", "order: 1\ndescribes: logs older than {min-age}\nroot: ~/a\nremove: {files: ['**/*.log']}\n", "readme: {min-age}: the step does not apply it"},
		{"a process pattern pgrep cannot read", "a.yaml", head + "root: ~/a\nbusy: {processes: ['(?i)tool'], says: a tool runs}\nremove: root\n", `busy: processes "(?i)tool"`},
		{"ready without down", "a.yaml", head + "ready: {run: [tool, info]}\nremove: {run: [tool, clean], dry-run: [-n]}\n", "ready: want run with hung"},
		{"a root command without arguments", "a.yaml", head + "root: {command: []}\nremove: root\n", "root: command: want the command"},
		{"no folders", "a.yaml", head + "root: ~/a\nremove: {folders: []}\n", "want at least one pattern"},
		{"folder leaves the root", "a.yaml", head + "root: ~/a\nremove: {folders: ['../b']}\n", "want a pattern inside the root"},
		{"a folder after any depth", "a.yaml", head + "root: ~/a\nremove: {files: ['**/logs/*.log']}\n", "or **/ and a name pattern"},
		{"files deep in the middle", "a.yaml", head + "root: ~/a\nremove: {files: ['a/**/b']}\n", "want a pattern inside the root, like logs/*.log"},
		{"keep without files", "a.yaml", "order: 1\ndescribes: x\nreadme: {removes: 'older than {min-age}'}\nsettings: [{key: min-age, kind: duration, default: 1h, help: x}]\nroot: ~/a\nremove: root\nkeep: {age: {of: mtime, within: min-age}}\n", "keep: want remove: folders or files"},
		{"keep open files outside what is removed", "a.yaml", head + "root: ~/a\nremove: {folders: ['*']}\nkeep: {open: ['../bin/*']}\n", "keep: open: \"../bin/*\""},
		{"keep by an unknown age", "a.yaml", head + "root: ~/a\nremove: {files: ['**/*.log']}\nkeep: {age: {of: mtime, within: max-age}}\n", `keep: age: within "max-age": want`},
		{"keep by an unknown time", "a.yaml", head + "root: ~/a\nremove: {folders: ['*']}\nkeep: {age: {of: ctime, within: min-age}}\n", `keep: age: of "ctime"`},
		{"keep of folders for files", "a.yaml", head + "root: ~/a\nremove: {files: ['**/*.log']}\nkeep: {sockets: true}\n", "keep: for files"},
		{"keep with an unknown key", "a.yaml", head + "root: ~/a\nremove: {files: ['**/*.log']}\nkeep: {opened: true}\n", "field opened not found"},
		{"says with nothing to miss", "a.yaml", head + "root: ~/a\nremove: {folders: [{glob: '*/.build', says: none}]}\n", "says: want has or beside"},
		{"beside outside the parent", "a.yaml", head + "root: ~/a\nremove: {folders: [{glob: '*/.build', beside: [../Package.swift]}]}\n", "want a path inside the folder or its parent"},
		{"untouched with keep", "a.yaml", head + "root: /tmp\nremove: {untouched: {for: 3d}}\nkeep: {open: true}\n", "want no keep or each beside it"},
		{"untouched for no time", "a.yaml", head + "root: /tmp\nremove: {untouched: {for: soon}}\n", "remove: untouched: for:"},
		{"untouched except a path", "a.yaml", head + "root: /tmp\nremove: {untouched: {for: 3d, except: [a/b]}}\n", "want a name pattern"},
		{"untouched beside folders", "a.yaml", head + "root: /tmp\nremove: {untouched: {for: 3d}, folders: ['*']}\n", "remove: want root"},
		{"superseded without apps", "a.yaml", head + "remove: {superseded: [{read: a.json, folders: ['~/x/{a}'], by: name}]}\n", "want apps:"},
		{"apps without superseded", "a.yaml", head + "apps: {in: [/Applications/*.app]}\nroot: ~/a\nremove: root\n", "apps: want remove: superseded"},
		{"apps run without says", "a.yaml", head + "apps: {run: [mdfind, x]}\nremove: {superseded: [{read: a.json, folders: ['~/x/{a}'], by: name}]}\n", "apps: want run together with says"},
		{"superseded by an unknown order", "a.yaml", head + "apps: {in: [/A/*.app]}\nremove: {superseded: [{read: a.json, folders: ['~/x/{a}'], by: date}]}\n", "want version, to remove only older ones"},
		{"superseded reading a plist", "a.yaml", head + "apps: {in: [/A/*.app]}\nremove: {superseded: [{read: Info.plist, folders: ['~/x/{a}'], by: name}]}\n", "want a .json inside the app"},
		{"superseded folder ending in a name", "a.yaml", head + "apps: {in: [/A/*.app]}\nremove: {superseded: [{read: a.json, folders: ['~/x/{a}/cache'], by: name}]}\n", "whose last part is one {key}"},
		{"superseded with a root", "a.yaml", head + "apps: {in: [/A/*.app]}\nroot: ~/a\nremove: {superseded: [{read: a.json, folders: ['~/x/{a}'], by: name}]}\n", "want no root, keep or each"},
		{"apps in a relative pattern", "a.yaml", head + "apps: {in: [A/*.app]}\nremove: {superseded: [{read: a.json, folders: ['~/x/{a}'], by: name}]}\n", "want a pattern under ~/ or /"},
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
		"a_test.yaml":     {Data: []byte("- case: tests of a step, not a step\n")},
	}

	files, err := Load(fsys)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "a.yaml: ")
	assert.Contains(t, err.Error(), "c.yaml: ")
	require.Len(t, files, 1)
	assert.Equal(t, "some-cache", files[0].Name)
}
