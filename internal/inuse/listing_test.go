package inuse

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
