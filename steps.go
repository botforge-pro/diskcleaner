package main

import (
	"github.com/botforge-pro/diskcleaner/internal/config"
)

type construct func(home string, c *config.Config) (step, error)

var written = map[string]construct{}
