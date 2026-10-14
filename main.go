package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

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

Steps:
  simulator-caches           Core ML caches of shut-down iOS simulators
  xcode-derived-data         Xcode DerivedData
  swiftpm-cache              the Swift Package Manager cache
`

type step interface {
	Run(r *report.Report)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, steps))
}

func steps(name string) (step, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}
	switch name {
	case "simulator-caches":
		return simulator.Default(home), true
	case "xcode-derived-data":
		return xcode.Default(home), true
	case "swiftpm-cache":
		return swiftpm.Default(home), true
	}
	return nil, false
}

func run(args []string, stdout, stderr io.Writer, find func(string) (step, bool)) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(stdout, version())
		return 0
	}
	if len(args) >= 2 && args[0] == "run" {
		return runStep(args[1], args[2:], stdout, stderr, find)
	}
	fmt.Fprint(stderr, usage)
	return 2
}

func runStep(name string, args []string, stdout, stderr io.Writer, find func(string) (step, bool)) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "list what would be removed, remove nothing")
	summaryPath := flags.String("report", "", "write the count, size and failures to this file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	s, ok := find(name)
	if !ok {
		fmt.Fprintf(stderr, "unknown step %q\n", name)
		fmt.Fprint(stderr, usage)
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
