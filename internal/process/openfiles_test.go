package process

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeLsof(t *testing.T, body string) string {
	path := filepath.Join(t.TempDir(), "lsof")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return path
}

func TestOpenFilesNamesTheOpenOnes(t *testing.T) {
	lsof := fakeLsof(t, "printf 'p42\\nf3\\nn/logs/b.jsonl\\n'\nexit 0\n")

	open, err := OpenFiles(5, lsof, []string{"/logs/a.jsonl", "/logs/b.jsonl"})

	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"/logs/b.jsonl": true}, open)
}

func TestOpenFilesWithNoneOpen(t *testing.T) {
	lsof := fakeLsof(t, "exit 1\n")

	open, err := OpenFiles(5, lsof, []string{"/logs/a.jsonl"})

	require.NoError(t, err)
	assert.Empty(t, open)
}

func TestOpenFilesThatListsNothingItWasAskedAboutCountsThemAllOpen(t *testing.T) {
	lsof := fakeLsof(t, "exit 0\n")

	open, err := OpenFiles(5, lsof, []string{"/logs/a.jsonl", "/logs/b.jsonl"})

	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"/logs/a.jsonl": true, "/logs/b.jsonl": true}, open)
}

func TestOpenFilesFails(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		timeout int
		fail    string
	}{
		{
			name:    "error",
			body:    "echo 'lsof: status error on /logs/a.jsonl: Permission denied' >&2\nexit 1\n",
			timeout: 5,
			fail:    "exit 1: lsof: status error on /logs/a.jsonl: Permission denied",
		},
		{
			name:    "warned",
			body:    "printf 'p42\\nf3\\nn/logs/b.jsonl\\n'\necho 'lsof: WARNING: can not stat() fuse file system' >&2\nexit 0\n",
			timeout: 5,
			fail:    "warned: lsof: WARNING: can not stat() fuse file system",
		},
		{
			name:    "hung",
			body:    "exec sleep 30\n",
			timeout: 1,
			fail:    "no answer within 1s",
		},
		{
			name:    "child left behind",
			body:    "sleep 30 >/dev/null 2>&1 &\nexit 1\n",
			timeout: 5,
			fail:    "exited but left a process running",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			open, err := OpenFiles(tc.timeout, fakeLsof(t, tc.body), []string{"/logs/a.jsonl"})

			assert.Nil(t, open)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.fail)
		})
	}
}
