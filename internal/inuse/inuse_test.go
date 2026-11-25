package inuse

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/age"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var old = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

func folder(t *testing.T) string {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	dir := filepath.Join(root, "out")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "debug"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "debug", "a.o"), []byte("x"), 0o644))
	for _, path := range []string{filepath.Join(dir, "debug", "a.o"), filepath.Join(dir, "debug"), dir} {
		require.NoError(t, os.Chtimes(path, old, old))
	}
	return dir
}

func listing(t *testing.T, names ...string) string {
	body := "#!/bin/sh\nprintf 'p1\\n"
	for _, name := range names {
		body += "f3\\nn" + name + "\\n"
	}
	path := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(path, []byte(body+"'\n"), 0o755))
	return path
}

func check(lsof string) Check {
	return Check{Age: age.Of(time.Hour), Open: Lsof{Command: lsof, TimeoutSeconds: 5}.Every(1)}
}

func counting(t *testing.T) (Lsof, func() int) {
	calls := filepath.Join(t.TempDir(), "calls")
	path := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\necho x >>"+calls+"\nprintf 'p1\\nn/held/file\\n'\n"), 0o755))
	return Lsof{Command: path, TimeoutSeconds: 5}, func() int {
		recorded, err := os.ReadFile(calls)
		if errors.Is(err, fs.ErrNotExist) {
			return 0
		}
		require.NoError(t, err)
		return bytes.Count(recorded, []byte("x"))
	}
}

func TestNothingIsListedBeforeTheFirstQuestion(t *testing.T) {
	lsof, calls := counting(t)

	lsof.Every(2)
	lsof.Once()

	assert.Zero(t, calls())
}

func TestEveryAnswerHasAFreshListingAtOneOrLess(t *testing.T) {
	for _, answers := range []int{1, 0, -3} {
		lsof, calls := counting(t)
		listing := lsof.Every(answers)

		for range 3 {
			_, err := listing.HoldsAny("/a")
			require.NoError(t, err)
		}

		assert.Equal(t, 3, calls(), "Every(%d)", answers)
	}
}

func TestAFailedListingIsTakenAgainOnTheNextQuestion(t *testing.T) {
	failedOnce := filepath.Join(t.TempDir(), "failed")
	path := filepath.Join(t.TempDir(), "lsof")
	body := "#!/bin/sh\nif [ ! -f " + failedOnce + " ]; then touch " + failedOnce + "; echo 'lsof: broken' >&2; exit 2; fi\nprintf 'p1\\nn/held/file\\n'\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o755))
	listing := Lsof{Command: path, TimeoutSeconds: 5}.Once()

	_, err := listing.HoldsAny("/held")
	require.EqualError(t, err, "exit 2: lsof: broken")

	held, err := listing.HoldsAny("/held")
	require.NoError(t, err)
	assert.True(t, held)
}

func TestAListingIsTakenAgainAfterItsNumberOfAnswers(t *testing.T) {
	lsof, calls := counting(t)
	listing := lsof.Every(2)

	for _, path := range []string{"/a", "/held", "/c"} {
		_, err := listing.HoldsAny(path)
		require.NoError(t, err)
	}

	assert.Equal(t, 2, calls())
}

func TestAListingTakenOnceAnswersEveryPathFromIt(t *testing.T) {
	lsof, calls := counting(t)
	listing := lsof.Once()

	held, err := listing.HoldsAny("/a", "/held")
	require.NoError(t, err)
	assert.True(t, held)
	held, err = listing.HoldsAny("/c")
	require.NoError(t, err)
	assert.False(t, held)

	assert.Equal(t, 1, calls())
}

func TestAnOldClosedFolderIsNotInUse(t *testing.T) {
	dir := folder(t)

	reason, err := check(listing(t, "/", dir+"-other/a.o", filepath.Dir(dir))).Reason(dir)

	require.NoError(t, err)
	assert.Empty(t, reason)
}

func TestARecentWriteAnywhereInsideKeepsTheFolder(t *testing.T) {
	dir := folder(t)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "debug", "a.o"), time.Now(), time.Now()))

	reason, err := check(listing(t, "/")).Reason(dir)

	require.NoError(t, err)
	assert.Equal(t, "written within 1h", reason)
}

func TestAnOpenFileOrWorkingFolderInsideKeepsTheFolder(t *testing.T) {
	dir := folder(t)
	for _, open := range []string{dir, filepath.Join(dir, "debug"), filepath.Join(dir, "debug", "a.o")} {
		reason, err := check(listing(t, "/", open)).Reason(dir)

		require.NoError(t, err)
		assert.Equal(t, "a process still has something in it open", reason, open)
	}
}

func TestRealLsofSeesAnOpenFile(t *testing.T) {
	dir := folder(t)
	file, err := os.Open(filepath.Join(dir, "debug", "a.o"))
	require.NoError(t, err)
	defer file.Close()

	reason, err := check("lsof").Reason(dir)

	require.NoError(t, err)
	assert.Equal(t, "a process still has something in it open", reason)
}

func TestReasonAfterTakesTheLatestWriteItIsGiven(t *testing.T) {
	dir := folder(t)
	c := check(listing(t, "/"))

	reason, err := c.ReasonAfter(dir, time.Now().Add(-59*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, "written within 1h", reason)

	reason, err = c.ReasonAfter(dir, time.Now().Add(-61*time.Minute))
	require.NoError(t, err)
	assert.Empty(t, reason)
}

func TestKeepReportsWhyAndFailsWhenItCannotTell(t *testing.T) {
	dir := folder(t)
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr}

	assert.True(t, check(listing(t, "/", dir)).Keep(r, dir))
	assert.Contains(t, stderr.String(), "SKIPPED       "+dir+": a process still has something in it open")

	broken := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(broken, []byte("#!/bin/sh\necho 'lsof: broken' >&2\nexit 2\n"), 0o755))
	assert.True(t, check(broken).Keep(r, dir))
	assert.Equal(t, []string{dir + ": lsof: exit 2: lsof: broken"}, r.Failures)

	assert.False(t, check(listing(t, "/")).Keep(r, dir))
}
