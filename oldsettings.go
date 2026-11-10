package main

import (
	"fmt"
	"slices"
	"strings"
)

var replacedSettings = map[string]string{
	"DISKCLEANER_REPOS_DIR":            "project-dirs",
	"DISKCLEANER_NODE_DIR":             "project-dirs",
	"DISKCLEANER_TMP_MINUTES":          "min-age",
	"DISKCLEANER_GIT_TMP_MINUTES":      "projects.git-tmp-min-age",
	"DISKCLEANER_XCODE_ARCHIVE_DAYS":   "xcode-archives.min-age",
	"DISKCLEANER_XCODE_ARCHIVE_KEEP":   "xcode-archives.keep",
	"DISKCLEANER_SIMULATOR_IDLE_DAYS":  "simulator-data.idle",
	"DISKCLEANER_CLAUDE_LOG_DAYS":      "claude-logs.min-age",
	"DISKCLEANER_CODEX_LOG_DAYS":       "codex-logs.min-age",
	"DISKCLEANER_SYSTEM_TMP_MIN_KB":    "system-temp.min-size",
	"DISKCLEANER_DOCKER_TIMEOUT":       "docker-images.timeout",
	"DISKCLEANER_DOCKER_PRUNE_TIMEOUT": "docker-images.prune-timeout",
	"DISKCLEANER_BREW_TIMEOUT":         "homebrew.timeout",
}

func oldSettings(environ []string) error {
	var lines []string
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, "DISKCLEANER_") {
			continue
		}
		if key, ok := replacedSettings[name]; ok {
			lines = append(lines, fmt.Sprintf("%s is now %s in ~/.config/diskcleaner.toml or --%s", name, key, key))
		} else {
			lines = append(lines, name+" is no longer read: diskcleaner finds that itself")
		}
	}
	if len(lines) == 0 {
		return nil
	}
	slices.Sort(lines)
	return fmt.Errorf("unset these variables:\n%s", strings.Join(lines, "\n"))
}
