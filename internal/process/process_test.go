package process

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestXcodeMatchesXcodeAndItsBuilds(t *testing.T) {
	pattern := regexp.MustCompile(Xcode)

	for _, command := range []string{
		"/Applications/Xcode.app/Contents/MacOS/Xcode",
		"/Applications/Xcode-beta.app/Contents/MacOS/Xcode",
		"/Applications/Xcode_26.1.app/Contents/MacOS/Xcode -NSDocumentRevisionsDebugMode YES",
		"/Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild -scheme App",
		"/Applications/Xcode.app/Contents/SharedFrameworks/XCBuild.framework/Versions/A/PlugIns/XCBBuildService.bundle/Contents/MacOS/XCBBuildService",
	} {
		assert.True(t, pattern.MatchString(command), command)
	}
	for _, command := range []string{
		"/Applications/Xcode.app/Contents/Developer/usr/bin/git status",
		"/Applications/Xcode.app/Contents/Developer/Applications/Simulator.app/Contents/MacOS/Simulator",
	} {
		assert.False(t, pattern.MatchString(command), command)
	}
}
