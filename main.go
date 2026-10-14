package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

const usage = `Usage: diskcleaner <command>

Commands:
  version  print the installed version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "version" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	fmt.Fprintln(stdout, version())
	return 0
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}
