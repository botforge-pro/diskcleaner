package age

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func limit(min time.Duration) Limit {
	return Limit{Min: min, Now: func() time.Time { return now }}
}

func TestWrittenWithinTheLimitIsRecentAndOnlyOlderIsNot(t *testing.T) {
	l := limit(time.Hour)

	assert.True(t, l.Recent(now.Add(-59*time.Minute)))
	assert.True(t, l.Recent(now.Add(-time.Hour)), "exactly the limit is not older than it")
	assert.False(t, l.Recent(now.Add(-time.Hour-time.Nanosecond)))
	assert.True(t, l.Recent(now.Add(time.Minute)), "a write stamped in the future")
}

func TestNoLimitKeepsNothingForItsAge(t *testing.T) {
	l := limit(0)

	assert.False(t, l.Recent(now))
	assert.False(t, l.Recent(now.Add(time.Minute)))
}

func TestTheLimitReadsAsADuration(t *testing.T) {
	assert.Equal(t, "15d", limit(15*24*time.Hour).String())
}
