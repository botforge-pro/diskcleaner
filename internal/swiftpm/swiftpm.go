package swiftpm

import (
	"path/filepath"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/rule"
)

func Default(home string) rule.Rule {
	return rule.Rule{
		Label: "SWIFTPM",
		Root:  filepath.Join(home, "Library/Caches/org.swift.swiftpm"),
		Busy: rule.Busy{
			Processes: "swift-build|swift-package|swift-test|swift-run|" + process.Xcode,
			Running:   process.Running,
			Says:      "a Swift package build, xcodebuild or Xcode is running",
		},
	}
}
