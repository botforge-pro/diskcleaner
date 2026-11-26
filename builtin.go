package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/rule"
)

//go:embed builtin/*.yaml
var builtinFiles embed.FS

type stepInfo struct {
	config.Step
	file rule.File
}

var builtinGroups = mustGroups()

var catalog = mustCatalog()

var settingsTable = config.NewTable(configSteps())

func builtinDir() fs.FS {
	dir, err := fs.Sub(builtinFiles, "builtin")
	if err != nil {
		panic(err)
	}
	return dir
}

func mustGroups() rule.Groups {
	data, err := fs.ReadFile(builtinDir(), rule.GroupsFile)
	if err != nil {
		panic(err)
	}
	groups, err := rule.ParseGroups(data)
	if err != nil {
		panic(fmt.Sprintf("%s does not load: %v", rule.GroupsFile, err))
	}
	return groups
}

func mustCatalog() []stepInfo {
	files, err := rule.Load(builtinDir())
	if err == nil {
		err = checkCatalog(files)
	}
	if err != nil {
		panic(fmt.Sprintf("the built-in steps do not load: %v", err))
	}
	steps := make([]stepInfo, len(files))
	for i, f := range files {
		steps[i] = stepInfo{f.Step(), f}
	}
	return steps
}

func checkCatalog(files []rule.File) error {
	var errs []error
	bases := map[string]string{}
	for _, f := range files {
		if other, taken := bases[f.Name]; taken {
			errs = append(errs, fmt.Errorf("%s.yaml: the step %s is %s.yaml already", f.Base, f.Name, other))
		}
		bases[f.Name] = f.Base
	}
	return errors.Join(errs...)
}

func (info stepInfo) step(home string, c *config.Config) step {
	return info.file.Rule(rule.Environment{
		Home:      home,
		Getenv:    os.Getenv,
		Processes: rule.SystemProcesses(),
		Commands:  rule.SystemCommands(),
		Duration:  c.Duration,
		Dirs:      c.Dirs,
		Count:     c.Count,
		Now:       time.Now,
		Lsof:      inuse.DefaultLsof(),
		Mounts:    rule.MountPoints,
	})
}

func configSteps() []config.Step {
	steps := make([]config.Step, len(catalog))
	for i, info := range catalog {
		steps[i] = info.Step
	}
	return steps
}
