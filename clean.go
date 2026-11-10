package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/lock"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/spf13/pflag"
)

func clean(args []string, stdout, stderr io.Writer, steps registry) int {
	var dryRun *bool
	var only, skip *[]string
	var summaryPath *string
	c, set, exit := loadSettings("clean", args, stderr, steps.table, func(set *pflag.FlagSet) {
		dryRun = set.Bool("dry-run", false, "list what would be removed, remove nothing")
		only = set.StringSlice("only", nil, "run only these steps, of those that are on")
		skip = set.StringSlice("skip", nil, "leave these steps out")
		summaryPath = set.String("report", "", "write the count, size and failures to this file")
	})
	if c == nil {
		return exit
	}
	if set.NArg() != 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	for _, name := range append(slices.Clone(*only), *skip...) {
		if !slices.Contains(steps.names, name) {
			fmt.Fprintln(stderr, unknownStepError{name: name})
			fmt.Fprint(stderr, usage)
			return 2
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "no home directory: %v\n", err)
		return 2
	}
	var all []step
	var off []string
	for _, name := range steps.names {
		if len(*only) > 0 && !slices.Contains(*only, name) || slices.Contains(*skip, name) {
			continue
		}
		if !c.Bool(name + ".enabled") {
			off = append(off, name)
			continue
		}
		s, ok := findStep(name, c, stderr, steps.find)
		if !ok {
			return 2
		}
		all = append(all, s)
	}

	held, err := takeLock(home, stderr)
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
	for _, name := range off {
		estimate := ""
		if *dryRun {
			estimate = wouldFree(name, c, steps)
		}
		fmt.Fprintf(stdout, "OFF           %s:%s turn on with --%s.enabled\n", name, estimate, name)
	}
	if err := held.Release(); err != nil {
		r.Fail("release the lock: %v", err)
	}
	if *summaryPath != "" {
		if err := writeSummary(r, *summaryPath); err != nil {
			r.Fail("report %s: %v", *summaryPath, err)
		}
	}

	freeKB := int64(-1)
	if *dryRun {
		fmt.Fprintf(stdout, "Would remove %d paths and free approximately %s.\nNothing was removed.\n", r.Count, report.FormatKB(r.KB))
	} else {
		fmt.Fprintf(stdout, "Removed %d paths; freed approximately %s.\n", r.Count, report.FormatKB(r.KB))
		freeKB = printFreeSpace(r, home)
	}
	if len(r.Failures) > 0 {
		fmt.Fprintf(stderr, "%d failure(s):\n", len(r.Failures))
		for _, failure := range r.Failures {
			fmt.Fprintf(stderr, "FAILED        %s\n", failure)
		}
		return 1
	}
	if minFree := c.Size("min-free"); freeKB >= 0 && minFree > 0 && freeKB*1024 < minFree {
		fmt.Fprintf(stderr, "Less than min-free (%s) is free.\n", report.FormatKB(minFree/1024))
		return 4
	}
	return 0
}

func wouldFree(name string, c *config.Config, steps registry) string {
	s, err := steps.find(name, c)
	if err != nil {
		return fmt.Sprintf(" cannot estimate (%v);", err)
	}
	r := &report.Report{Out: io.Discard, Err: io.Discard, DryRun: true}
	s.Run(r)
	if len(r.Failures) > 0 {
		first, _, _ := strings.Cut(r.Failures[0], "\n")
		return fmt.Sprintf(" cannot estimate (%s);", first)
	}
	return fmt.Sprintf(" would free about %s;", report.FormatKB(r.KB))
}

func takeLock(home string, stderr io.Writer) (*lock.Lock, error) {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(state, "diskcleaner")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if errors.Is(err, syscall.ENOSPC) {
			fmt.Fprintf(stderr, "The disk is full, so the lock folder %s cannot be made; running without it.\n", dir)
			return &lock.Lock{}, nil
		}
		return nil, fmt.Errorf("cannot make the folder of the lock: %w", err)
	}
	return lock.Take(filepath.Join(dir, "lock"), stderr)
}

func printFreeSpace(r *report.Report, home string) int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(home, &stat); err != nil {
		r.Fail("statfs %s: %v", home, err)
		return -1
	}
	freeKB := int64(stat.Bavail) * int64(stat.Bsize) / 1024
	fmt.Fprintf(r.Out, "Free on the disk of %s: %s.\n", home, report.FormatKB(freeKB))
	return freeKB
}
