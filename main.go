package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/clones"
	"github.com/botforge-pro/diskcleaner/internal/docker"
	"github.com/botforge-pro/diskcleaner/internal/gocache"
	"github.com/botforge-pro/diskcleaner/internal/gradle"
	"github.com/botforge-pro/diskcleaner/internal/homebrew"
	"github.com/botforge-pro/diskcleaner/internal/ide"
	"github.com/botforge-pro/diskcleaner/internal/leftovers"
	"github.com/botforge-pro/diskcleaner/internal/nodemodules"
	"github.com/botforge-pro/diskcleaner/internal/projects"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/scratchpads"
	"github.com/botforge-pro/diskcleaner/internal/sessionlogs"
	"github.com/botforge-pro/diskcleaner/internal/simulator"
	"github.com/botforge-pro/diskcleaner/internal/swiftpm"
	"github.com/botforge-pro/diskcleaner/internal/xcode"
)

type step interface {
	Run(r *report.Report)
}

type stepInfo struct {
	name      string
	describes string
	construct func(home string) (step, error)
}

var catalog = []stepInfo{
	{"projects", "build output and Git leftovers in your repositories", func(home string) (step, error) {
		s, err := projects.Default(home)
		return s, err
	}},
	{"gradle-caches", "the Gradle caches", func(home string) (step, error) { return gradle.Default(home), nil }},
	{"docker-images", "Docker images not used by any container", func(home string) (step, error) {
		s, err := docker.Default(home)
		return s, err
	}},
	{"go-cache", "the Go module and build caches", func(string) (step, error) { return gocache.Default(), nil }},
	{"homebrew", "old Homebrew versions and downloads", func(string) (step, error) {
		s, err := homebrew.Default()
		return s, err
	}},
	{"swiftpm-cache", "the Swift Package Manager cache", func(home string) (step, error) { return swiftpm.Default(home), nil }},
	{"xcode-derived-data", "Xcode DerivedData", func(home string) (step, error) { return xcode.Default(home), nil }},
	{"temp-leftovers", "temporary folders of killed Go builds and of Xcode exports", func(string) (step, error) {
		s, err := leftovers.Default()
		return s, err
	}},
	{"system-temp", "your files and folders left in /private/tmp", func(string) (step, error) {
		s, err := leftovers.SystemTemp()
		return s, err
	}},
	{"code-sign-clones", "copies of Chrome and other apps no running app uses", func(string) (step, error) {
		s, err := clones.Default()
		return s, err
	}},
	{"ide-old-versions", "settings, caches and logs of IDE versions older than the installed one", func(home string) (step, error) { return ide.Default(home), nil }},
	{"simulator-data", "data of iOS simulators not used for days", func(home string) (step, error) {
		s, err := simulator.DefaultData(home)
		return s, err
	}},
	{"simulator-caches", "Core ML caches of shut-down iOS simulators", func(home string) (step, error) { return simulator.Default(home), nil }},
	{"claude-logs", "old Claude Code session logs", func(home string) (step, error) {
		s, err := sessionlogs.Claude(home)
		return s, err
	}},
	{"codex-logs", "old Codex session logs", func(home string) (step, error) {
		s, err := sessionlogs.Codex(home)
		return s, err
	}},
	{"claude-scratchpads", "build output in Claude Code scratchpads", func(string) (step, error) { return scratchpads.Default(), nil }},
	{"node-modules", "node_modules of Node projects that have a lockfile", func(home string) (step, error) {
		s, err := nodemodules.Default(home)
		return s, err
	}},
}

var usage = buildUsage()

func buildUsage() string {
	var b strings.Builder
	b.WriteString(`Usage: diskcleaner <command>

Commands:
  version                    print the installed version
  clean [--dry-run]          run every step below, one cleanup at a time
  run STEP [--dry-run] [--report FILE]
                             run one cleanup step
  check STEP...              check the settings of these steps, change nothing

Steps, in the order clean runs them:
`)
	for _, info := range catalog {
		fmt.Fprintf(&b, "  %-26s %s\n", info.name, info.describes)
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
	find  func(name string) (step, error)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, registry{names: stepNames(), find: steps}))
}

func stepNames() []string {
	names := make([]string, len(catalog))
	for i, info := range catalog {
		names[i] = info.name
	}
	return names
}

func steps(name string) (step, error) {
	for _, info := range catalog {
		if info.name != name {
			continue
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("%s: no home directory: %w", name, err)
		}
		return info.construct(home)
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
	if len(args) >= 2 && args[0] == "run" {
		return runStep(args[1], args[2:], stdout, stderr, steps.find)
	}
	if len(args) >= 2 && args[0] == "check" {
		return checkSteps(args[1:], stderr, steps.find)
	}
	fmt.Fprint(stderr, usage)
	return 2
}

func findStep(name string, stderr io.Writer, find func(string) (step, error)) (step, bool) {
	s, err := find(name)
	if err == nil {
		return s, true
	}
	fmt.Fprintln(stderr, err)
	if errors.As(err, &unknownStepError{}) {
		fmt.Fprint(stderr, usage)
	}
	return nil, false
}

func checkSteps(names []string, stderr io.Writer, find func(string) (step, error)) int {
	for _, name := range names {
		if _, ok := findStep(name, stderr, find); !ok {
			return 2
		}
	}
	return 0
}

func runStep(name string, args []string, stdout, stderr io.Writer, find func(string) (step, error)) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "list what would be removed, remove nothing")
	summaryPath := flags.String("report", "", "write the count, size and failures to this file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	s, ok := findStep(name, stderr, find)
	if !ok {
		return 2
	}
	r := &report.Report{Out: stdout, Err: stderr, DryRun: *dryRun}
	s.Run(r)
	if *summaryPath != "" {
		if err := writeSummary(r, *summaryPath); err != nil {
			fmt.Fprintf(stderr, "FAILED        report %s: %v\n", *summaryPath, err)
			return 1
		}
	}
	if len(r.Failures) > 0 {
		return 1
	}
	return 0
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
