package report

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatDurationWritesItAsASettingDoes(t *testing.T) {
	for d, want := range map[time.Duration]string{
		15 * 24 * time.Hour: "15d",
		time.Hour:           "1h",
		36 * time.Hour:      "36h",
		30 * time.Minute:    "30m",
		90 * time.Second:    "1m30s",
	} {
		assert.Equal(t, want, FormatDuration(d))
	}
}
