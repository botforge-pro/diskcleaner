package rule

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValuesCompareAsVersionsDatesOrText(t *testing.T) {
	assert.Positive(t, compareValues("17.10", "17.9"))
	assert.Positive(t, compareValues("2026.2", "2026.1.3"))
	assert.Positive(t, compareValues("17.5.1", "17.5"))
	assert.Positive(t, compareValues("2026-03-01T00:00:00Z", "2025-12-31T23:59:59Z"))
	assert.Negative(t, compareValues("alpha", "beta"))
	assert.Zero(t, compareValues("1.0", "1.0"))
}

func folders(t *testing.T, root string, names ...string) {
	for _, name := range names {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name), 0o755))
		write(t, filepath.Join(root, name, "x"))
	}
}

const versions = "remove: [{folders: '*'}]\nrank:\n  per: {name: '^([A-Za-z]+)[0-9]'}\n  order: {name: '^[A-Za-z]+([0-9][0-9.]*)$'}\n"

func TestTheNewestOfEachGroupIsSparedAndTheRestGo(t *testing.T) {
	root := temp(t)
	folders(t, root, "GoLand2025.3", "GoLand2026.2", "GoLand2026.10", "PyCharm2024.1", "Toolbox")

	run(step(t, "/nowhere", "root: "+root+"\n"+versions+"  spare: {newest: 1}\n"), false)

	assert.DirExists(t, filepath.Join(root, "GoLand2026.10"))
	assert.NoDirExists(t, filepath.Join(root, "GoLand2026.2"))
	assert.NoDirExists(t, filepath.Join(root, "GoLand2025.3"))
	assert.DirExists(t, filepath.Join(root, "PyCharm2024.1"), "the only one of its group is the newest")
	assert.DirExists(t, filepath.Join(root, "Toolbox"), "a folder without a group stays")
}

func TestWhatSortsBelowTheNewestSparedOneGoesAndWhatIsNewerStays(t *testing.T) {
	root := temp(t)
	folders(t, root, "GoLand2025.3", "GoLand2026.2", "GoLand2027.1")

	run(step(t, "/nowhere", "root: "+root+"\n"+versions+"  spare: {in: [GoLand2026.2]}\n"), false)

	assert.NoDirExists(t, filepath.Join(root, "GoLand2025.3"))
	assert.DirExists(t, filepath.Join(root, "GoLand2026.2"))
	assert.DirExists(t, filepath.Join(root, "GoLand2027.1"))
}

func TestAGroupStaysWholeUntilItsNewestSparedOneIsMade(t *testing.T) {
	root := temp(t)
	folders(t, root, "GoLand2025.3")

	_, _, stderr := run(step(t, "/nowhere", "root: "+root+"\n"+versions+"  spare: {in: [GoLand2026.2]}\n"), false)

	assert.DirExists(t, filepath.Join(root, "GoLand2025.3"))
	assert.Contains(t, stderr, root+": nothing here goes until GoLand2026.2 is made")
}

func TestAGroupNothingIsSparedInStaysWhole(t *testing.T) {
	root := temp(t)
	folders(t, root, "aaaa", "bbbb")

	r, _, stderr := run(step(t, "/nowhere", "root: "+root+"\nremove: [{folders: '*'}]\nrank: {spare: {in: []}}\n"), false)

	assert.DirExists(t, filepath.Join(root, "aaaa"))
	assert.DirExists(t, filepath.Join(root, "bbbb"))
	assert.Zero(t, r.Count)
	assert.Empty(t, stderr)
}

func TestWithoutAnOrderEverySparedOneMustBeMadeBeforeTheRestGo(t *testing.T) {
	root := temp(t)
	folders(t, root, "aaaa", "bbbb", "cccc")
	body := "root: " + root + "\nremove: [{folders: '*'}]\nrank: {spare: {in: [aaaa, bbbb]}}\n"

	run(step(t, "/nowhere", body), false)

	assert.DirExists(t, filepath.Join(root, "aaaa"))
	assert.DirExists(t, filepath.Join(root, "bbbb"))
	assert.NoDirExists(t, filepath.Join(root, "cccc"))

	folders(t, root, "cccc")
	require.NoError(t, os.RemoveAll(filepath.Join(root, "bbbb")))

	_, _, stderr := run(step(t, "/nowhere", body), false)

	assert.DirExists(t, filepath.Join(root, "cccc"))
	assert.Contains(t, stderr, "nothing here goes until bbbb is made")
}
