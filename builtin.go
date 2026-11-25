package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/rule"
)

//go:embed builtin/*.yaml
var builtinFiles embed.FS

var builtins = mustLoadBuiltins()

func mustLoadBuiltins() map[string]rule.File {
	dir, err := fs.Sub(builtinFiles, "builtin")
	if err != nil {
		panic(err)
	}
	files, err := rule.Load(dir)
	if err != nil {
		panic(fmt.Sprintf("a built-in rule does not load: %v", err))
	}
	byName := map[string]rule.File{}
	for _, f := range files {
		byName[f.Name] = f
	}
	return byName
}

func declared(name string) stepInfo {
	f, ok := builtins[name]
	if !ok {
		panic(fmt.Sprintf("no built-in rule %s.yaml", name))
	}
	return stepInfo{config.Step{Name: f.Name, Reversible: f.Reversible}, f.Describes, func(home string, _ *config.Config) (step, error) {
		return f.Rule(home, os.Getenv, process.Running), nil
	}}
}
