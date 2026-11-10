package main

import "github.com/botforge-pro/diskcleaner/internal/config"

var configSteps = []config.Step{
	{Name: "projects", Undoable: true, Settings: []config.Setting{
		{Key: "git-tmp-min-age", Kind: config.Duration, Default: "1h", Help: "keep temporary files of git fetch and repack written more recently"},
	}},
	{Name: "gradle-caches", Undoable: true},
	{Name: "docker-images", Undoable: true, Settings: []config.Setting{
		{Key: "timeout", Kind: config.Duration, Default: "30s", Help: "give up on a Docker that does not answer within this"},
		{Key: "prune-timeout", Kind: config.Duration, Default: "15m", Help: "give up on a prune that runs longer"},
	}},
	{Name: "go-cache", Undoable: true},
	{Name: "homebrew", Settings: []config.Setting{
		{Key: "timeout", Kind: config.Duration, Default: "15m", Help: "give up on a cleanup that runs longer"},
	}},
	{Name: "swiftpm-cache", Undoable: true},
	{Name: "xcode-derived-data", Undoable: true},
	{Name: "xcode-archives", Settings: []config.Setting{
		{Key: "min-age", Kind: config.Duration, Default: "15d", Help: "remove archives older than this"},
		{Key: "keep", Kind: config.Count, Default: "1", Help: "keep this many newest archives of each app"},
	}},
	{Name: "temp-leftovers", Undoable: true},
	{Name: "system-temp", Settings: []config.Setting{
		{Key: "min-size", Kind: config.Size, Default: "1MB", Help: "leave smaller entries"},
	}},
	{Name: "code-sign-clones", Undoable: true},
	{Name: "ide-old-versions"},
	{Name: "simulator-data", Settings: []config.Setting{
		{Key: "idle", Kind: config.Duration, Default: "3d", Help: "erase simulators not used for this long"},
	}},
	{Name: "simulator-caches", Undoable: true},
	{Name: "claude-logs", Settings: []config.Setting{
		{Key: "min-age", Kind: config.Duration, Default: "20d", Help: "remove session logs older than this"},
	}},
	{Name: "codex-logs", Settings: []config.Setting{
		{Key: "min-age", Kind: config.Duration, Default: "20d", Help: "remove session logs older than this"},
	}},
	{Name: "claude-scratchpads", Undoable: true},
}

var settingsTable = config.NewTable(configSteps)
