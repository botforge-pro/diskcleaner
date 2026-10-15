package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/botforge-pro/diskcleaner/internal/claudelogs"
	"github.com/botforge-pro/diskcleaner/internal/docker"
	"github.com/botforge-pro/diskcleaner/internal/gocache"
	"github.com/botforge-pro/diskcleaner/internal/gradle"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/simulator"
	"github.com/botforge-pro/diskcleaner/internal/swiftpm"
	"github.com/botforge-pro/diskcleaner/internal/xcode"
)

const usage = `Usage: diskcleaner <command>

Commands:
  version                    print the installed version
  run STEP [--dry-run] [--report FILE]
                             run one cleanup step
  check STEP...              check the settings of these steps, change nothing

Steps:
  simulator-caches           Core ML caches of shut-down iOS simulators
  xcode-derived-data         Xcode DerivedData
  swiftpm-cache              the Swift Package Manager cache
  go-cache                   the Go module and build caches
  docker-images              Docker images not used by any container
  gradle-caches              the Gradle caches
  claude-logs                old Claude Code session logs
`

type step interface {
	Run(r *report.Report)
}

type unknownStepError struct {
	name string
}

func (e unknownStepError) Error() string {
	return fmt.Sprintf("unknown step %q", e.name)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, steps))
}

var constructors = map[string]func(home string) (step, error){
	"simulator-caches":   func(home string) (step, error) { return simulator.Default(home), nil },
	"xcode-derived-data": func(home string) (step, error) { return xcode.Default(home), nil },
	"swiftpm-cache":      func(home string) (step, error) { return swiftpm.Default(home), nil },
	"go-cache":           func(string) (step, error) { return gocache.Default(), nil },
	"gradle-caches":      func(home string) (step, error) { return gradle.Default(home), nil },
	"claude-logs": func(home string) (step, error) {
		s, err := claudelogs.Default(home)
		return s, err
	},
	"docker-images": func(home string) (step, error) {
		s, err := docker.Default(home)
		return s, err
	},
}

func steps(name string) (step, error) {
	construct, ok := constructors[name]
	if !ok {
		return nil, unknownStepError{name: name}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("%s: no home directory: %w", name, err)
	}
	return construct(home)
}

func run(args []string, stdout, stderr io.Writer, find func(string) (step, error)) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(stdout, version())
		return 0
	}
	if len(args) >= 2 && args[0] == "run" {
		return runStep(args[1], args[2:], stdout, stderr, find)
	}
	if len(args) >= 2 && args[0] == "check" {
		return checkSteps(args[1:], stderr, find)
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
