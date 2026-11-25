package main

import (
	"github.com/botforge-pro/diskcleaner/internal/clones"
	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/docker"
	"github.com/botforge-pro/diskcleaner/internal/gocache"
	"github.com/botforge-pro/diskcleaner/internal/gradle"
	"github.com/botforge-pro/diskcleaner/internal/homebrew"
	"github.com/botforge-pro/diskcleaner/internal/ide"
	"github.com/botforge-pro/diskcleaner/internal/leftovers"
	"github.com/botforge-pro/diskcleaner/internal/projects"
	"github.com/botforge-pro/diskcleaner/internal/scratchpads"
	"github.com/botforge-pro/diskcleaner/internal/sessionlogs"
	"github.com/botforge-pro/diskcleaner/internal/simulator"
	"github.com/botforge-pro/diskcleaner/internal/systemtemp"
	"github.com/botforge-pro/diskcleaner/internal/xcode"
)

type construct func(home string, c *config.Config) (step, error)

var written = map[string]construct{
	"projects": func(_ string, c *config.Config) (step, error) {
		s, err := projects.Default(c.Dirs("project-dirs"), c.Duration("min-age"), c.Duration("projects.git-tmp-min-age"), c.Duration("projects.lfs-timeout"))
		return s, err
	},
	"gradle-caches": func(home string, _ *config.Config) (step, error) {
		return gradle.Default(home), nil
	},
	"docker-images": func(home string, c *config.Config) (step, error) {
		return docker.Default(home, c.Duration("docker-images.timeout"), c.Duration("docker-images.prune-timeout")), nil
	},
	"go-cache": func(string, *config.Config) (step, error) {
		return gocache.Default(), nil
	},
	"homebrew": func(_ string, c *config.Config) (step, error) {
		return homebrew.Default(c.Duration("homebrew.timeout")), nil
	},
	"xcode-derived-data": func(home string, _ *config.Config) (step, error) {
		return xcode.Default(home), nil
	},
	"xcode-archives": func(home string, c *config.Config) (step, error) {
		return xcode.DefaultArchives(home, c.Duration("xcode-archives.min-age"), c.Count("xcode-archives.keep")), nil
	},
	"temp-leftovers": func(_ string, c *config.Config) (step, error) {
		s, err := leftovers.Default(c.Duration("min-age"))
		return s, err
	},
	"system-temp": func(string, *config.Config) (step, error) {
		return systemtemp.Default(), nil
	},
	"code-sign-clones": func(_ string, c *config.Config) (step, error) {
		s, err := clones.Default(c.Duration("min-age"))
		return s, err
	},
	"ide-old-versions": func(home string, _ *config.Config) (step, error) {
		return ide.Default(home), nil
	},
	"simulator-caches": func(home string, _ *config.Config) (step, error) {
		return simulator.Default(home), nil
	},
	"claude-logs": func(home string, c *config.Config) (step, error) {
		return sessionlogs.Claude(home, c.Duration("claude-logs.min-age")), nil
	},
	"codex-logs": func(home string, c *config.Config) (step, error) {
		return sessionlogs.Codex(home, c.Duration("codex-logs.min-age")), nil
	},
	"claude-scratchpads": func(string, *config.Config) (step, error) {
		return scratchpads.Default(), nil
	},
}
