package report

import (
	"regexp"
	"strconv"
)

var humanSize = regexp.MustCompile(`^([0-9.]+) ?([kKMGT]?B)$`)

var bytesPerUnit = map[string]float64{
	"B":  1,
	"kB": 1e3,
	"KB": 1e3,
	"MB": 1e6,
	"GB": 1e9,
	"TB": 1e12,
}

func DecimalSizeKB(size string) (int64, bool) {
	match := humanSize.FindStringSubmatch(size)
	if match == nil {
		return 0, false
	}
	number, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	return int64(number * bytesPerUnit[match[2]] / 1024), true
}
