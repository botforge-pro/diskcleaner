package settings

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalResolvesRelativePathsAndLinks(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "real"), 0o755))
	require.NoError(t, os.Symlink("real", filepath.Join(dir, "link")))
	t.Chdir(dir)

	for _, path := range []string{"real", "./link", filepath.Join(dir, "link")} {
		resolved, err := Canonical(path)
		require.NoError(t, err, path)
		assert.Equal(t, filepath.Join(dir, "real"), resolved, path)
	}
}

func TestCanonicalOfAMissingPathIsAbsolute(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Chdir(dir)

	resolved, err := Canonical("absent")

	assert.Error(t, err)
	assert.Equal(t, filepath.Join(dir, "absent"), resolved)
}

func TestPositiveIntFallsBackWhenUnset(t *testing.T) {
	t.Setenv("DISKCLEANER_TEST_NUMBER", "")

	number, err := PositiveInt("DISKCLEANER_TEST_NUMBER", 30)

	require.NoError(t, err)
	assert.Equal(t, 30, number)
}

func TestPositiveIntReadsWholeNumber(t *testing.T) {
	t.Setenv("DISKCLEANER_TEST_NUMBER", "900")

	number, err := PositiveInt("DISKCLEANER_TEST_NUMBER", 30)

	require.NoError(t, err)
	assert.Equal(t, 900, number)
}

func TestPositiveDurationStopsWhereTheDurationWouldOverflow(t *testing.T) {
	most := int64(math.MaxInt64 / int64(time.Minute))

	t.Setenv("DISKCLEANER_TEST_NUMBER", strconv.FormatInt(most, 10))
	number, err := PositiveDuration("DISKCLEANER_TEST_NUMBER", 60, time.Minute, "minutes")
	require.NoError(t, err)
	assert.Equal(t, most, int64(number))

	t.Setenv("DISKCLEANER_TEST_NUMBER", strconv.FormatInt(most+1, 10))
	_, err = PositiveDuration("DISKCLEANER_TEST_NUMBER", 60, time.Minute, "minutes")
	assert.EqualError(t, err, fmt.Sprintf("DISKCLEANER_TEST_NUMBER must be at most %d minutes, got: %d", most, most+1))
}

func TestPositiveIntRejectsOtherValues(t *testing.T) {
	for _, value := range []string{"0", "-1", "1.5", "07", "30s", " 30", "99999999999999999999"} {
		t.Setenv("DISKCLEANER_TEST_NUMBER", value)

		_, err := PositiveInt("DISKCLEANER_TEST_NUMBER", 30)

		assert.EqualError(t, err, "DISKCLEANER_TEST_NUMBER must be a whole number above zero, got: "+value, value)
	}
}
