package process

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

var ps = "ps"

func OpenNames(seconds int, lsof string) ([]string, error) {
	args := []string{"-n", "-P", "-Fn"}
	exiting, err := exitingProcesses(seconds)
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	if len(exiting) > 0 {
		args = append(args, "-p", "^"+strings.Join(exiting, ",^"))
	}
	result := RunGroup(seconds, lsof, args...)
	var exit *exec.ExitError
	switch {
	case result.TimedOut && result.Leftover:
		return nil, fmt.Errorf("no answer within %ds: %v", seconds, result.Err)
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

func exitingProcesses(seconds int) ([]string, error) {
	result := RunGroup(seconds, ps, "-axo", "pid=,stat=")
	var exit *exec.ExitError
	switch {
	case result.TimedOut && result.Leftover:
		return nil, fmt.Errorf("no answer within %ds: %v", seconds, result.Err)
	case result.TimedOut:
		return nil, fmt.Errorf("no answer within %ds", seconds)
	case errors.As(result.Err, &exit):
		return nil, fmt.Errorf("exit %d: %s", exit.ExitCode(), result.Output())
	case result.Err != nil:
		return nil, result.Err
	}
	var pids []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.ContainsAny(fields[1], "EZ") && !strings.Contains(fields[1], "l") {
			pids = append(pids, fields[0])
		}
	}
	return pids, nil
}

func Holds(names []string, path string) bool {
	inside := path + string(filepath.Separator)
	return slices.ContainsFunc(names, func(name string) bool { return name == path || strings.HasPrefix(name, inside) })
}
