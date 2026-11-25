package main

import (
	"cmp"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
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

var catalog = mustCatalog()

var settingsTable = config.NewTable(configSteps())

func mustCatalog() []stepInfo {
	dir, err := fs.Sub(builtinFiles, "builtin")
	if err != nil {
		panic(err)
	}
	files, err := rule.Load(dir)
	if err == nil {
		err = checkCatalog(files)
	}
	if err != nil {
		panic(fmt.Sprintf("the built-in steps do not load: %v", err))
	}
	slices.SortFunc(files, func(a, b rule.File) int { return cmp.Compare(a.Order, b.Order) })
	steps := make([]stepInfo, len(files))
	for i, f := range files {
		steps[i] = stepInfo{f.Step(), f}
	}
	return steps
}

func checkCatalog(files []rule.File) error {
	var errs []error
	orders := map[int]string{}
	for _, f := range files {
		if other, taken := orders[f.Order]; taken {
			errs = append(errs, fmt.Errorf("%s.yaml: order %d: %s.yaml has it too", f.Name, f.Order, other))
		}
		orders[f.Order] = f.Name
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
