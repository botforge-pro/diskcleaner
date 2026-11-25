package age

import (
	"time"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

type Limit struct {
	Min time.Duration
	Now func() time.Time
}

func Of(min time.Duration) Limit {
	return Limit{Min: min, Now: time.Now}
}

func (l Limit) Recent(written time.Time) bool {
	return l.Min > 0 && l.Now().Sub(written) <= l.Min
}

func (l Limit) String() string {
	return report.FormatDuration(l.Min)
}
