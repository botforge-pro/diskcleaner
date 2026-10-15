package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/botforge-pro/diskcleaner/internal/lock"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

func clean(args []string, stdout, stderr io.Writer, steps registry) int {
	flags := flag.NewFlagSet("clean", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "list what would be removed, remove nothing")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "no home directory: %v\n", err)
		return 2
	}
	var all []step
	for _, name := range steps.names {
		s, ok := findStep(name, stderr, steps.find)
		if !ok {
			return 2
		}
		all = append(all, s)
	}

	held, err := lock.Take(settings.Env("DISKCLEANER_LOCK", filepath.Join(home, ".diskcleaner.lock")), stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, lock.ErrHeld) {
			return 3
		}
		return 2
	}

	r := &report.Report{Out: stdout, Err: stderr, DryRun: *dryRun}
	if *dryRun {
		fmt.Fprintf(stdout, "Mode: dry run\n\n")
	}
	for _, s := range all {
		s.Run(r)
	}
	if err := held.Release(); err != nil {
		r.Fail("release the lock: %v", err)
	}

	if *dryRun {
		fmt.Fprintf(stdout, "Would remove %d paths and free approximately %s.\nNothing was removed.\n", r.Count, report.FormatKB(r.KB))
	} else {
		fmt.Fprintf(stdout, "Removed %d paths; freed approximately %s.\n", r.Count, report.FormatKB(r.KB))
		printFreeSpace(r, home)
	}
	if len(r.Failures) == 0 {
		return 0
	}
	fmt.Fprintf(stderr, "%d failure(s):\n", len(r.Failures))
	for _, failure := range r.Failures {
		fmt.Fprintf(stderr, "FAILED        %s\n", failure)
	}
	return 1
}

func printFreeSpace(r *report.Report, home string) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(home, &stat); err != nil {
		r.Fail("statfs %s: %v", home, err)
		return
	}
	freeKB := int64(stat.Bavail) * int64(stat.Bsize) / 1024
	fmt.Fprintf(r.Out, "Free on the disk of %s: %s.\n", home, report.FormatKB(freeKB))
}
