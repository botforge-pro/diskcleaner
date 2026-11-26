package rule

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheNameComesFromTheFileAfterItsPlaceAndTheLabelFromItsFirstWord(t *testing.T) {
	f := parse(t, "070-xcode-derived-data.yaml", "describes: x\nroot: ~/a\nremove: .\n")

	assert.Equal(t, "xcode-derived-data", f.Name)
	assert.Equal(t, "070-xcode-derived-data", f.Base)
	assert.Equal(t, "XCODE", f.Label())
}

func TestAStepsOwnSettingWinsOverTheGlobalOfTheSameKey(t *testing.T) {
	f := parse(t, "150-old-logs.yaml", "describes: logs older than {min-age}\nsettings: [{key: min-age, kind: duration, default: 20d, help: x}]\nroot: ~/a\nremove: [{files: '*.log'}]\nkeep: {age: {of: mtime, within: '{min-age}'}}\n")
	var asked []string
	env := environment("/nowhere")
	env.Duration = func(key string) time.Duration {
		asked = append(asked, key)
		return 0
	}

	run(f.Rule(env), false)

	assert.Equal(t, "logs older than `old-logs.min-age` (`20d`)", f.Removes())
	assert.Empty(t, asked, "the root is missing, so no age was asked")
	assert.Equal(t, "old-logs.min-age", f.configKey("min-age"))
	assert.Equal(t, "project-dirs", f.configKey("project-dirs"))
}

func TestAMistakeInAFileIsNamed(t *testing.T) {
	const file = "001-a.yaml"
	head := "describes: x\n"
	code := head + "root: ~/a\nremove: .\n"
	cases := []struct {
		name, file, body, want string
	}{
		{"unknown key", file, code + "targets: [x]\n", "field targets not found"},
		{"an order in the file", file, "order: 1\n" + code, "field order not found"},
		{"a file without its place", "a.yaml", code, "want three digits, the step's place in a run"},
		{"a place of two digits", "01-a.yaml", code, "want three digits, the step's place in a run"},
		{"file name not in words", "001-A_b.yaml", code, "want lowercase words joined by - after the digits"},
		{"no describes", file, "root: ~/a\nremove: .\n", "describes: want"},
		{"nothing to do", file, head + "root: ~/a\n", "want remove, run, do or first"},
		{"a setting the readme does not name", file, head + "settings: [{key: wait, kind: duration, default: 1h, help: x}]\nroot: ~/a\nremove: [{files: '*'}]\nkeep: {age: {of: mtime, within: '{wait}'}}\n", "want {wait} in its text"},
		{"a setting nothing uses", file, code + "settings: [{key: wait, kind: duration, default: 1h, help: x}]\n", "settings: wait: nothing in the file uses it"},
		{"a setting the readme names and nothing uses", file, "describes: older than {wait}\nsettings: [{key: wait, kind: duration, default: 1h, help: x}]\nroot: ~/a\nremove: .\n", "readme: {wait}: the step does not use it"},
		{"a global it does not apply", file, "describes: logs older than {min-age}\nroot: ~/a\nremove: .\n", "readme: {min-age}: the step does not use it"},
		{"setting of an unknown kind", file, code + "settings: [{key: wait, kind: minutes, default: '1', help: x}]\n", "want one of duration, timeout, count"},
		{"setting with a bad default", file, code + "settings: [{key: wait, kind: duration, default: soon, help: x}]\n", "settings: wait: default:"},
		{"setting named enabled", file, code + "settings: [{key: enabled, kind: count, default: '1', help: x}]\n", "other than enabled"},
		{"setting without help", file, code + "settings: [{key: wait, kind: duration, default: 1h}]\n", "settings: wait: help: want"},
		{"readme says what describes says", file, code + "readme: {removes: x}\n", "leave it out and the README takes describes"},
		{"readme mentions an unknown setting", file, code + "readme: {removes: 'older than {max-age}'}\n", "readme: {max-age}: want"},
		{"remove something else", file, head + "root: ~/a\nremove: all\n", "remove: want . for the node's folder itself"},
		{"an entry of two kinds", file, head + "root: ~/a\nremove: [{folders: x, files: y}]\n", "want one of skip, folders, files and empty"},
		{"an entry with an unknown key", file, head + "root: ~/a\nremove: [{folder: x}]\n", "field folder not found"},
		{"only skips", file, head + "root: ~/a\nremove: [{skip: x}]\n", "want an entry that takes something"},
		{"an entry leaving the folder", file, head + "root: ~/a\nremove: [{folders: '../b'}]\n", "want a pattern inside the node's folder"},
		{"an absolute entry", file, head + "root: ~/a\nremove: [{folders: /b}]\n", "want a pattern inside the node's folder"},
		{"stars inside a part", file, head + "root: ~/a\nremove: [{files: 'a**/b'}]\n", "with ** only as a whole part"},
		{"says with nothing to explain", file, head + "root: ~/a\nremove: [{folders: '*', says: none}]\n", "says: want when or unless"},
		{"says inside an entry's condition", file, head + "root: ~/a\nremove: [{folders: '*', when: [{exists: x, says: y}]}]\n", "says and fails belong to the node's own conditions"},
		{"a condition of two atoms", file, head + "root: ~/a\nwhen: [{exists: x, program: y}]\nremove: .\n", "a condition: want one of"},
		{"value without is", file, head + "root: ~/a\nwhen: [{value: x}]\nremove: .\n", "want value and is together"},
		{"a process pattern pgrep cannot read", file, head + "root: ~/a\nunless: [{running: ['(']}]\nremove: .\n", `running "("`},
		{"an unknown group", file, head + "root: ~/a\nunless: [{running: [{group: eclipse}]}]\nremove: .\n", "group \"eclipse\": want one of the groups"},
		{"an unknown placeholder", file, head + "root: ~/a/{nothing}\nremove: .\n", "{nothing}: want path, name, uid, a let above or a setting"},
		{"a duration where a path is wanted", file, head + "root: ~/a/{min-age}\nremove: .\n", "{min-age}: a duration setting belongs where"},
		{"a set of folders in a command", file, head + "run: [tool, '{project-dirs}']\ndry-run: [-n]\n", "a set of folders belongs in a path"},
		{"a let used above it", file, head + "root: '{apps}'\nlet: {apps: /Applications}\nremove: .\n", "{apps}: want path, name, uid, a let above"},
		{"rank without remove entries", file, head + "root: ~/a\nremove: .\nrank: {spare: {newest: 1}}\n", "rank: want remove entries"},
		{"rank sparing two ways", file, head + "root: ~/a\nremove: [{folders: '*'}]\nrank: {spare: {newest: 1, in: x}}\n", "rank: spare: want one of"},
		{"the newest without an order", file, head + "root: ~/a\nremove: [{folders: '*'}]\nrank: {spare: {newest: 1}}\n", "rank: spare: newest: want order"},
		{"a count that is no number", file, head + "root: ~/a\nremove: [{folders: '*'}]\nrank: {order: {name: '(.*)'}, spare: {newest: few}}\n", "rank: spare: newest \"few\""},
		{"keep without remove", file, head + "root: ~/a\nrun: [tool, clean]\ndry-run: [-n]\nkeep: {open: true}\n", "rank and keep: want remove"},
		{"keep by an unknown clock", file, head + "root: ~/a\nremove: [{folders: '*'}]\nkeep: {age: {of: ctime, within: 1d}}\n", `keep: age: of "ctime"`},
		{"keep open files outside the candidate", file, head + "root: ~/a\nremove: [{folders: '*'}]\nkeep: {open: ['../bin/*']}\n", `keep: open: "../bin/*"`},
		{"a command without a dry run", file, head + "run: [tool, clean]\n", "want dry-run, estimate or size"},
		{"a command beside remove", file, head + "root: ~/a\nremove: .\nrun: [tool, clean]\ndry-run: [-n]\n", "remove and run: want one of them"},
		{"a timeout that is no duration", file, head + "run: [tool, clean]\ndry-run: [-n]\ntimeout: soon\n", `timeout "soon"`},
		{"an estimate without a group", file, head + "run: [tool, clean]\nestimate: {run: [tool, df], match: 'freed'}\n", "want a group, the size"},
		{"an estimate reading no output", file, head + "run: [tool, clean]\nsize: ~/a\nestimate: {match: '(.*)'}\n", "estimate: want run, or dry-run"},
		{"first beside root", file, head + "root: ~/a\nfirst: [{root: ~/b, remove: .}]\n", "first: want it alone"},
		{"a mistake in a child", file, head + "do: [{root: ~/a, remove: .}, {root: ~/b, remove: [{files: 'a**'}]}]\n", "do: 2: remove:"},
		{"stop without says", file, head + "root: ~/a\nstop: {running: [d], names: daemons}\nremove: .\n", "stop: want running, names and says"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.file, []byte(tc.body), testGroups)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoadNamesEveryBadFileAndKeepsTheGoodOnesInTheOrderOfTheirFiles(t *testing.T) {
	fsys := fstest.MapFS{
		GroupsFile:              {Data: []byte("tools: [tool]\n")},
		"020-good.yaml":         {Data: []byte("describes: x\nroot: ~/a\nremove: .\n")},
		"020-good_test.yaml":    {Data: []byte("- case: x\n")},
		"030-bad-one.yaml":      {Data: []byte("describes: x\nroot: ~/a\nremove: all\n")},
		"040-bad-another.yaml":  {Data: []byte("root: ~/a\nremove: .\n")},
		"010-uses-a-group.yaml": {Data: []byte("describes: x\nroot: ~/a\nunless: [{running: [{group: tools}]}]\nremove: .\n")},
	}

	files, err := Load(fsys)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "030-bad-one.yaml")
	assert.Contains(t, err.Error(), "040-bad-another.yaml")
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	assert.Equal(t, []string{"uses-a-group", "good"}, names)
}

func TestAGroupOfProcessesIsCheckedAsItLoads(t *testing.T) {
	for body, want := range map[string]string{
		"Tools: [x]\n":   "want lowercase words",
		"tools: []\n":    "a list of process patterns",
		"tools: ['(']\n": "tools: \"(\"",
	} {
		_, err := ParseGroups([]byte(body))

		require.Error(t, err, body)
		assert.Contains(t, err.Error(), want)
	}
}
