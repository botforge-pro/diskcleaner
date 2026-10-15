package report

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecimalSizeKB(t *testing.T) {
	cases := []struct {
		size string
		kb   int64
		ok   bool
	}{
		{"1.5GB", 1464843, true},
		{"0B", 0, true},
		{"100B", 0, true},
		{"2048B", 2, true},
		{"512kB", 500, true},
		{"2.3 MB", 2246, true},
		{"1TB", 976562500, true},
		{"1.2.3GB", 0, false},
		{"GB", 0, false},
		{"12", 0, false},
	}
	for _, tc := range cases {
		kb, ok := DecimalSizeKB(tc.size)
		assert.Equal(t, tc.ok, ok, tc.size)
		assert.Equal(t, tc.kb, kb, tc.size)
	}
}
