package report

import (
	"strings"

	"github.com/dustin/go-humanize"
)

func SizeKB(size string) (int64, bool) {
	if strings.TrimLeft(size, "0123456789., ") == "" {
		return 0, false
	}
	bytes, err := humanize.ParseBytes(size)
	if err != nil {
		return 0, false
	}
	return int64(bytes / 1024), true
}
