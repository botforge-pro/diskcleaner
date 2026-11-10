package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type replacement struct {
	key   string
	value func(old string) string
}

func same(old string) string { return old }

func unit(suffix string) func(string) string {
	return func(old string) string { return old + suffix }
}

func wholeDaysOver(old string) string {
	n, err := strconv.Atoi(old)
	if err != nil {
		return old + "d"
	}
	return strconv.Itoa(n+1) + "d"
}

var replacedSettings = map[string]replacement{
	"DISKCLEANER_REPOS_DIR":            {"project-dirs", same},
	"DISKCLEANER_NODE_DIR":             {"project-dirs", same},
	"DISKCLEANER_TMP_MINUTES":          {"min-age", unit("m")},
	"DISKCLEANER_GIT_TMP_MINUTES":      {"projects.git-tmp-min-age", unit("m")},
	"DISKCLEANER_XCODE_ARCHIVE_DAYS":   {"xcode-archives.min-age", wholeDaysOver},
	"DISKCLEANER_XCODE_ARCHIVE_KEEP":   {"xcode-archives.keep", same},
	"DISKCLEANER_SIMULATOR_IDLE_DAYS":  {"simulator-data.idle", unit("d")},
	"DISKCLEANER_CLAUDE_LOG_DAYS":      {"claude-logs.min-age", wholeDaysOver},
	"DISKCLEANER_CODEX_LOG_DAYS":       {"codex-logs.min-age", wholeDaysOver},
	"DISKCLEANER_SYSTEM_TMP_MIN_KB":    {"system-temp.min-size", unit("KB")},
	"DISKCLEANER_DOCKER_TIMEOUT":       {"docker-images.timeout", unit("s")},
	"DISKCLEANER_DOCKER_PRUNE_TIMEOUT": {"docker-images.prune-timeout", unit("s")},
	"DISKCLEANER_BREW_TIMEOUT":         {"homebrew.timeout", unit("s")},
}

func oldSettings(environ []string) error {
	var lines []string
	for _, entry := range environ {
		name, value, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, "DISKCLEANER_") {
			continue
		}
		if r, ok := replacedSettings[name]; ok {
			lines = append(lines, fmt.Sprintf("%s: write --%s=%s, or the same in the config file", name, r.key, r.value(value)))
		} else {
			lines = append(lines, name+": no longer a setting")
		}
	}
	if len(lines) == 0 {
		return nil
	}
	slices.Sort(lines)
	return fmt.Errorf("diskcleaner no longer reads DISKCLEANER_* variables; unset these:\n%s", strings.Join(lines, "\n"))
}
