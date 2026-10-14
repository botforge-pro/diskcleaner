package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRun(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantStatus int
		wantStdout string
		wantStderr string
	}{
		{"version", []string{"version"}, 0, "(devel)\n", ""},
		{"no command", nil, 2, "", usage},
		{"unknown command", []string{"clean"}, 2, "", usage},
		{"extra argument", []string{"version", "now"}, 2, "", usage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := run(tc.args, &stdout, &stderr)
			assert.Equal(t, tc.wantStatus, status)
			assert.Equal(t, tc.wantStdout, stdout.String())
			assert.Equal(t, tc.wantStderr, stderr.String())
		})
	}
}
