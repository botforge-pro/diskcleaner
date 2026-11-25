package main

import (
	"cmp"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
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
	construct construct
	file      rule.File
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
		err = checkCatalog(files, written)
	}
	if err != nil {
		panic(fmt.Sprintf("the built-in steps do not load: %v", err))
	}
	slices.SortFunc(files, func(a, b rule.File) int { return cmp.Compare(a.Order, b.Order) })
	steps := make([]stepInfo, len(files))
	for i, f := range files {
		steps[i] = stepInfo{f.Step(), constructor(f), f}
	}
	return steps
}

func checkCatalog(files []rule.File, code map[string]construct) error {
	var errs []error
	orders := map[int]string{}
	used := map[string]bool{}
	for _, f := range files {
		if other, taken := orders[f.Order]; taken {
			errs = append(errs, fmt.Errorf("%s.yaml: order %d: %s.yaml has it too", f.Name, f.Order, other))
		}
		orders[f.Order] = f.Name
		if f.Run == "" {
			continue
		}
		if _, ok := code[f.Name]; !ok {
			errs = append(errs, fmt.Errorf("%s.yaml: run: code, but no step of this name is written in Go", f.Name))
		}
		used[f.Name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(code)) {
		if !used[name] {
			errs = append(errs, fmt.Errorf("the step %s is written in code, but no file runs it", name))
		}
	}
	return errors.Join(errs...)
}

func constructor(f rule.File) construct {
	if f.Run != "" {
		return written[f.Name]
	}
	return func(home string, c *config.Config) (step, error) {
		return f.Rule(rule.Environment{
			Home:      home,
			Getenv:    os.Getenv,
			Processes: rule.SystemProcesses(),
			Commands:  rule.SystemCommands(),
			Duration:  func(key string) time.Duration { return c.Duration(f.Name + "." + key) },
			Now:       time.Now,
			Lsof:      inuse.DefaultLsof(),
		}), nil
	}
}

func configSteps() []config.Step {
	steps := make([]config.Step, len(catalog))
	for i, info := range catalog {
		steps[i] = info.Step
	}
	return steps
}
