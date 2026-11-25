package swiftpm

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheCacheIsSwiftPMsAndGoesWhole(t *testing.T) {
	home := t.TempDir()

	s := Default(home)

	assert.Equal(t, filepath.Join(home, "Library/Caches/org.swift.swiftpm"), s.Root)
	assert.Empty(t, s.Targets)
}

func TestDefaultPatternMatchesSwiftAndXcodeBuilds(t *testing.T) {
	pattern := regexp.MustCompile(Default(t.TempDir()).Busy.Processes)

	for _, command := range []string{
		"/usr/bin/swift-build --package-path app",
		"/usr/bin/swift-run app",
		"/Applications/Xcode-beta.app/Contents/MacOS/Xcode",
	} {
		assert.True(t, pattern.MatchString(command), command)
	}
	assert.False(t, pattern.MatchString("/Applications/Xcode.app/Contents/Developer/usr/bin/git status"))
}
