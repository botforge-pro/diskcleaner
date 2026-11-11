package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/spf13/pflag"
)

type step interface {
	Run(r *report.Report)
}

var usage = buildUsage()

func buildUsage() string {
	var b strings.Builder
	b.WriteString(`Usage: diskcleaner <command>

Commands:
  clean       remove what the steps below find, then say what is free
  config      print every setting, its value and where it came from
  version     print the installed version

Examples:
  diskcleaner clean --dry-run                      what would go; nothing is removed
  diskcleaner clean --only projects,go-cache       just these steps
  diskcleaner clean --projects.git-tmp-min-age=0   dead Git temporary files at once

clean takes --dry-run, --only STEPS, --skip STEPS, --report FILE and
--config FILE. Every setting that diskcleaner config prints is also a flag of
clean and config, and lives in diskcleaner.toml.

Steps, in the order clean runs them; each runs unless --STEP.enabled=false turns
it off, and the marked ones delete what cannot be undone:
`)
	for _, info := range catalog {
		state := ""
		if !info.Reversible {
			state = " (cannot be undone)"
		}
		fmt.Fprintf(&b, "  %-20s %s%s\n", info.Name, info.describes, state)
	}
	return b.String()
}

type unknownStepError struct {
	name string
}

func (e unknownStepError) Error() string {
	return fmt.Sprintf("unknown step %q", e.name)
}

type registry struct {
	names []string
	table config.Table
	find  func(name string, c *config.Config) (step, error)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, registry{names: stepNames(), table: settingsTable, find: steps}))
}

func loadSettings(name string, args []string, stderr io.Writer, table config.Table, extra func(*pflag.FlagSet)) (*config.Config, *pflag.FlagSet, int) {
	set := pflag.NewFlagSet(name, pflag.ContinueOnError)
	set.SetOutput(stderr)
	table.Flags(set)
	if extra != nil {
		extra(set)
	}
	if err := set.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil, nil, 0
		}
		fmt.Fprint(stderr, usage)
		return nil, nil, 2
	}
	if err := oldSettings(os.Environ()); err != nil {
		fmt.Fprintln(stderr, err)
		return nil, nil, 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "no home directory: %v\n", err)
		return nil, nil, 2
	}
	c, err := config.Load(table, set, home)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, nil, 2
	}
	return c, set, 0
}

func stepNames() []string {
	names := make([]string, len(catalog))
	for i, info := range catalog {
		names[i] = info.Name
	}
	return names
}

func steps(name string, c *config.Config) (step, error) {
	for _, info := range catalog {
		if info.Name != name {
			continue
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("%s: no home directory: %w", name, err)
		}
		return info.construct(home, c)
	}
	return nil, unknownStepError{name: name}
}

func run(args []string, stdout, stderr io.Writer, steps registry) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(stdout, version())
		return 0
	}
	if len(args) >= 1 && args[0] == "clean" {
		return clean(args[1:], stdout, stderr, steps)
	}
	if len(args) >= 1 && args[0] == "config" {
		return showConfig(args[1:], stdout, stderr, steps.table)
	}
	fmt.Fprint(stderr, usage)
	return 2
}

func findStep(name string, c *config.Config, stderr io.Writer, find func(string, *config.Config) (step, error)) (step, bool) {
	s, err := find(name, c)
	if err == nil {
		return s, true
	}
	fmt.Fprintln(stderr, err)
	if errors.As(err, &unknownStepError{}) {
		fmt.Fprint(stderr, usage)
	}
	return nil, false
}

func writeSummary(r *report.Report, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := r.WriteSummary(file); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}
