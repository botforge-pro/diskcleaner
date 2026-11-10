package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/botforge-pro/diskcleaner/internal/lock"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
	"github.com/spf13/pflag"
)

func clean(args []string, stdout, stderr io.Writer, steps registry) int {
	var dryRun *bool
	c, set, ok := loadSettings("clean", args, stderr, steps.table, func(set *pflag.FlagSet) {
		dryRun = set.Bool("dry-run", false, "list what would be removed, remove nothing")
	})
	if !ok || set.NArg() != 0 {
		if ok {
			fmt.Fprint(stderr, usage)
		}
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "no home directory: %v\n", err)
		return 2
	}
	var all []step
	var off []string
	for _, name := range steps.names {
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
	for _, name := range off {
		fmt.Fprintf(stdout, "OFF           %s: turn on with --%s.enabled\n", name, name)
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
