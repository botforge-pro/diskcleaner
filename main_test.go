package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStep struct {
	fail bool
}

func (f fakeStep) Run(r *report.Report) {
	r.Count = 2
	r.KB = 3072
	if f.fail {
		r.Fail("rm /tmp/x: permission denied\nsecond line")
	}
}

func findFake(fail bool) func(string) (step, error) {
	return func(name string) (step, error) {
		switch name {
		case "fake":
			return fakeStep{fail: fail}, nil
		case "misconfigured":
			return nil, errors.New("DISKCLEANER_FAKE must be a whole number above zero, got: 1.5")
		}
		return nil, unknownStepError{name: name}
	}
}

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
		{"unknown step", []string{"run", "nothing"}, 2, "", "unknown step \"nothing\"\n" + usage},
		{"step argument", []string{"run", "fake", "now"}, 2, "", usage},
		{"step", []string{"run", "fake", "--dry-run"}, 0, "", ""},
		{"misconfigured step", []string{"run", "misconfigured"}, 2, "", "DISKCLEANER_FAKE must be a whole number above zero, got: 1.5\n"},
		{"check", []string{"check", "fake", "fake"}, 0, "", ""},
		{"check without steps", []string{"check"}, 2, "", usage},
		{"check misconfigured", []string{"check", "fake", "misconfigured"}, 2, "", "DISKCLEANER_FAKE must be a whole number above zero, got: 1.5\n"},
		{"check unknown", []string{"check", "nothing"}, 2, "", "unknown step \"nothing\"\n" + usage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := run(tc.args, &stdout, &stderr, findFake(false))
			assert.Equal(t, tc.wantStatus, status)
			assert.Equal(t, tc.wantStdout, stdout.String())
			assert.Equal(t, tc.wantStderr, stderr.String())
		})
	}
}

func TestRunWritesSummary(t *testing.T) {
	summary := filepath.Join(t.TempDir(), "summary")
	var stdout, stderr bytes.Buffer

	status := run([]string{"run", "fake", "--report", summary}, &stdout, &stderr, findFake(true))

	assert.Equal(t, 1, status)
	written, err := os.ReadFile(summary)
	require.NoError(t, err)
	assert.Equal(t, "count 2\nkb 3072\nfailure rm /tmp/x: permission denied; second line\n", string(written))
	assert.Equal(t, "FAILED        rm /tmp/x: permission denied; second line\n", stderr.String())
}

func TestRunReportsUnwritableSummary(t *testing.T) {
	summary := filepath.Join(t.TempDir(), "missing", "summary")
	var stdout, stderr bytes.Buffer

	status := run([]string{"run", "fake", "--report", summary}, &stdout, &stderr, findFake(false))

	assert.Equal(t, 1, status)
	assert.Contains(t, stderr.String(), "FAILED        report "+summary)
}
