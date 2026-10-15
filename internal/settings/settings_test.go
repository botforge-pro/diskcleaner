package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestPositiveIntRejectsOtherValues(t *testing.T) {
	for _, value := range []string{"0", "-1", "1.5", "07", "30s", " 30", "99999999999999999999"} {
		t.Setenv("DISKCLEANER_TEST_NUMBER", value)

		_, err := PositiveInt("DISKCLEANER_TEST_NUMBER", 30)

		assert.EqualError(t, err, "DISKCLEANER_TEST_NUMBER must be a whole number above zero, got: "+value, value)
	}
}
