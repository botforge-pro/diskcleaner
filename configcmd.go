package main

import (
	"fmt"
	"io"

	"github.com/botforge-pro/diskcleaner/internal/config"
)

func showConfig(args []string, stdout, stderr io.Writer, table config.Table) int {
	c, set, ok := loadSettings("config", args, stderr, table, nil)
	if !ok || set.NArg() != 0 {
		if ok {
			fmt.Fprint(stderr, usage)
		}
		return 2
	}
	if c.File == "" {
		fmt.Fprintln(stdout, "file: none")
	} else {
		fmt.Fprintf(stdout, "file: %s\n", c.File)
	}
	for _, s := range table {
		fmt.Fprintf(stdout, "%-30s %-24s %s\n", s.Key, c.Text(s.Key), c.Source(s.Key))
	}
	return 0
}
