package main

import (
	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/projects"
)

type construct func(home string, c *config.Config) (step, error)

var written = map[string]construct{
	"projects": func(_ string, c *config.Config) (step, error) {
		s, err := projects.Default(c.Dirs("project-dirs"), c.Duration("min-age"), c.Duration("projects.git-tmp-min-age"), c.Duration("projects.lfs-timeout"))
		return s, err
	},
}
