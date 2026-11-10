package main

import (
	"fmt"
	"io"
	"os"

	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/spf13/pflag"
)

func showConfig(args []string, stdout, stderr io.Writer) int {
	set := pflag.NewFlagSet("config", pflag.ContinueOnError)
	set.SetOutput(stderr)
	settingsTable.Flags(set)
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "no home directory: %v\n", err)
		return 2
	}
	c, err := config.Load(settingsTable, set, home)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if c.File == "" {
		fmt.Fprintln(stdout, "file: none")
	} else {
		fmt.Fprintf(stdout, "file: %s\n", c.File)
	}
	for _, s := range settingsTable {
		fmt.Fprintf(stdout, "%-30s %-24s %s\n", s.Key, c.Text(s.Key), c.Source(s.Key))
	}
	return 0
}
