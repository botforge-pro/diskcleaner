package main

import (
	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/ide"
	"github.com/botforge-pro/diskcleaner/internal/projects"
	"github.com/botforge-pro/diskcleaner/internal/scratchpads"
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
	"xcode-derived-data": func(home string, _ *config.Config) (step, error) {
		return xcode.Default(home), nil
	},
	"xcode-archives": func(home string, c *config.Config) (step, error) {
		return xcode.DefaultArchives(home, c.Duration("xcode-archives.min-age"), c.Count("xcode-archives.keep")), nil
	},
	"system-temp": func(string, *config.Config) (step, error) {
		return systemtemp.Default(), nil
	},
	"ide-old-versions": func(home string, _ *config.Config) (step, error) {
		return ide.Default(home), nil
	},
	"simulator-caches": func(home string, _ *config.Config) (step, error) {
		return simulator.Default(home), nil
	},
	"claude-scratchpads": func(string, *config.Config) (step, error) {
		return scratchpads.Default(), nil
	},
}
