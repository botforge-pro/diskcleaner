package rule

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const valid = `name: some-cache
label: SOME
describes: the cache of some tool
root: $SOME_CACHE or $SOME_HOME/cache or ~/Library/Caches/some
busy:
  processes: [some-build]
  groups: [xcode]
  says: some build is running
remove:
  folders: ['*/old']
`

func noVariables(string) string { return "" }

func variables(set map[string]string) func(string) string {
	return func(key string) string { return set[key] }
}

func parse(t *testing.T, name, body string) File {
	f, err := Parse(name, []byte(body))
	require.NoError(t, err)
	return f
}

func TestAValidFileBecomesARule(t *testing.T) {
	r := parse(t, "some-cache.yaml", valid).Rule("/Users/me", noVariables, nil)

	assert.Equal(t, "SOME", r.Label)
	assert.Equal(t, "/Users/me/Library/Caches/some", r.Root)
	assert.Equal(t, "some-build|"+groups["xcode"], r.Busy.Processes)
	assert.Equal(t, "some build is running", r.Busy.Says)
	assert.Equal(t, []string{"*/old"}, r.Targets)
}

func TestTheRootIsTheFirstChoiceWhoseVariableIsSet(t *testing.T) {
	f := parse(t, "some-cache.yaml", valid)

	assert.Equal(t, "/opt/some/cache", f.Rule("/Users/me", variables(map[string]string{"SOME_HOME": "/opt/some"}), nil).Root)
	assert.Equal(t, "/var/some", f.Rule("/Users/me", variables(map[string]string{"SOME_CACHE": "/var/some", "SOME_HOME": "/opt/some"}), nil).Root)
}

func TestRemoveRootTakesTheRootWhole(t *testing.T) {
	r := parse(t, "tmp.yaml", "name: tmp\nlabel: TMP\ndescribes: x\nroot: /private/tmp/tool\nremove: root\n").Rule("/Users/me", noVariables, nil)

	assert.Equal(t, "/private/tmp/tool", r.Root)
	assert.Empty(t, r.Targets)
}

func TestAMistakeInAFileIsNamed(t *testing.T) {
	head := "label: A\ndescribes: x\n"
	cases := []struct {
		name, file, body, want string
	}{
		{"unknown key", "a.yaml", "name: a\n" + head + "root: ~/a\nremove: root\ntargets: [x]\n", "field targets not found"},
		{"name differs from the file", "a.yaml", "name: b\n" + head + "root: ~/a\nremove: root\n", `want the name of the file, "a"`},
		{"name not in words", "A_b.yaml", "name: A_b\n" + head + "root: ~/a\nremove: root\n", "want lowercase words"},
		{"long label", "a.yaml", "name: a\nlabel: TOOLONGLABEL\ndescribes: x\nroot: ~/a\nremove: root\n", "want up to 8 capital letters"},
		{"no describes", "a.yaml", "name: a\nlabel: A\nroot: ~/a\nremove: root\n", "describes: want"},
		{"no root", "a.yaml", "name: a\n" + head + "remove: root\n", "root: want a folder"},
		{"variable last", "a.yaml", "name: a\n" + head + "root: ~/a or $A_HOME\nremove: root\n", "the last choice must be a path"},
		{"relative path", "a.yaml", "name: a\n" + head + "root: Library/a\nremove: root\n", "want $VARIABLE"},
		{"no remove", "a.yaml", "name: a\n" + head + "root: ~/a\n", "remove: want root, or folders"},
		{"remove something else", "a.yaml", "name: a\n" + head + "root: ~/a\nremove: all\n", "remove: want root, or folders"},
		{"no folders", "a.yaml", "name: a\n" + head + "root: ~/a\nremove: {folders: []}\n", "want at least one pattern"},
		{"folder leaves the root", "a.yaml", "name: a\n" + head + "root: ~/a\nremove: {folders: ['../b']}\n", "want a pattern inside the root"},
		{"processes without says", "a.yaml", "name: a\n" + head + "root: ~/a\nbusy: {processes: [a]}\nremove: root\n", "together with says"},
		{"says without processes", "a.yaml", "name: a\n" + head + "root: ~/a\nbusy: {says: a runs}\nremove: root\n", "together with says"},
		{"bad pattern", "a.yaml", "name: a\n" + head + "root: ~/a\nbusy: {processes: ['('], says: a runs}\nremove: root\n", "missing closing )"},
		{"unknown group", "a.yaml", "name: a\n" + head + "root: ~/a\nbusy: {groups: [eclipse], says: a runs}\nremove: root\n", "want one of xcode"},
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
		"a.yaml":          {Data: []byte("name: b\n")},
		"c.yaml":          {Data: []byte("name: c\nnope: 1\n")},
		"notes.txt":       {Data: []byte("not a rule")},
	}

	files, err := Load(fsys)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "a.yaml: ")
	assert.Contains(t, err.Error(), "c.yaml: ")
	require.Len(t, files, 1)
	assert.Equal(t, "some-cache", files[0].Name)
}
