package process

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

func OpenNames(seconds int, lsof string) ([]string, error) {
	result := RunGroup(seconds, lsof, "-n", "-P", "-Fn")
	var exit *exec.ExitError
	switch {
	case result.TimedOut:
		return nil, fmt.Errorf("no answer within %ds", seconds)
	case result.Leftover:
		return nil, fmt.Errorf("exited but left a process running, which was stopped: %v", result.Err)
	case errors.As(result.Err, &exit):
		return nil, fmt.Errorf("exit %d: %s", exit.ExitCode(), result.Output())
	case result.Err != nil:
		return nil, result.Err
	case strings.TrimSpace(result.Stderr) != "":
		return nil, fmt.Errorf("warned: %s", strings.TrimSpace(result.Stderr))
	}
	var names []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		if name, ok := strings.CutPrefix(line, "n"); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("listed no open files at all, not even its own")
	}
	return names, nil
}

func Holds(names []string, path string) bool {
	inside := path + string(filepath.Separator)
	return slices.ContainsFunc(names, func(name string) bool { return name == path || strings.HasPrefix(name, inside) })
}
