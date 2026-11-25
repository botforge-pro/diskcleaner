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
root:
  - env: SOME_HOME
    path: cache
  - path: ~/Library/Caches/some
busy:
  processes: [some-build]
  groups: [xcode]
  says: some build is running
targets: ['*/old']
`

func TestAValidFileBecomesARule(t *testing.T) {
	f, err := Parse("some-cache.yaml", []byte(valid))
	require.NoError(t, err)

	r := f.Rule("/Users/me", func(string) string { return "" }, nil)

	assert.Equal(t, "SOME", r.Label)
	assert.Equal(t, "/Users/me/Library/Caches/some", r.Root)
	assert.Equal(t, "some-build|"+groups["xcode"], r.Busy.Processes)
	assert.Equal(t, "some build is running", r.Busy.Says)
	assert.Equal(t, []string{"*/old"}, r.Targets)
}

func TestTheFirstSetVariableChoosesTheRoot(t *testing.T) {
	f, err := Parse("some-cache.yaml", []byte(valid))
	require.NoError(t, err)

	r := f.Rule("/Users/me", func(key string) string { return map[string]string{"SOME_HOME": "/opt/some"}[key] }, nil)

	assert.Equal(t, "/opt/some/cache", r.Root)
}

func TestAnAbsolutePathIsTakenAsItIs(t *testing.T) {
	f, err := Parse("tmp.yaml", []byte("name: tmp\nlabel: TMP\ndescribes: x\nroot:\n  - path: /private/tmp\n"))
	require.NoError(t, err)

	assert.Equal(t, "/private/tmp", f.Rule("/Users/me", func(string) string { return "" }, nil).Root)
}

func TestAMistakeInAFileIsNamed(t *testing.T) {
	cases := []struct {
		name, file, body, want string
	}{
		{"unknown key", "a.yaml", "name: a\nlabel: A\ndescribes: x\nroot: [{path: ~/a}]\nremove: all\n", "field remove not found"},
		{"name differs from the file", "a.yaml", "name: b\nlabel: A\ndescribes: x\nroot: [{path: ~/a}]\n", `want the name of the file, "a"`},
		{"name not in words", "A_b.yaml", "name: A_b\nlabel: A\ndescribes: x\nroot: [{path: ~/a}]\n", "want lowercase words"},
		{"long label", "a.yaml", "name: a\nlabel: TOOLONGLABEL\ndescribes: x\nroot: [{path: ~/a}]\n", "want up to 8 capital letters"},
		{"no describes", "a.yaml", "name: a\nlabel: A\nroot: [{path: ~/a}]\n", "describes: want"},
		{"no root", "a.yaml", "name: a\nlabel: A\ndescribes: x\n", "root: want at least one place"},
		{"variable last", "a.yaml", "name: a\nlabel: A\ndescribes: x\nroot: [{env: A_HOME}]\n", "the last place must be a path"},
		{"relative path", "a.yaml", "name: a\nlabel: A\ndescribes: x\nroot: [{path: Library/a}]\n", "want it to start with ~/ or /"},
		{"absolute under a variable", "a.yaml", "name: a\nlabel: A\ndescribes: x\nroot: [{env: A_HOME, path: /a}, {path: ~/a}]\n", "a subfolder of the variable's value"},
		{"processes without says", "a.yaml", "name: a\nlabel: A\ndescribes: x\nroot: [{path: ~/a}]\nbusy: {processes: [a]}\n", "together with says"},
		{"says without processes", "a.yaml", "name: a\nlabel: A\ndescribes: x\nroot: [{path: ~/a}]\nbusy: {says: a runs}\n", "together with says"},
		{"bad pattern", "a.yaml", "name: a\nlabel: A\ndescribes: x\nroot: [{path: ~/a}]\nbusy: {processes: ['('], says: a runs}\n", "missing closing )"},
		{"unknown group", "a.yaml", "name: a\nlabel: A\ndescribes: x\nroot: [{path: ~/a}]\nbusy: {groups: [eclipse], says: a runs}\n", "want one of xcode"},
		{"target leaves the root", "a.yaml", "name: a\nlabel: A\ndescribes: x\nroot: [{path: ~/a}]\ntargets: ['../b']\n", "want a pattern inside the root"},
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
