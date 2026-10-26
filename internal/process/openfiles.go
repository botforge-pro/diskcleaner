package process

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func OpenFiles(seconds int, lsof string, files []string) (map[string]bool, error) {
	result := RunGroup(seconds, lsof, append([]string{"-Fn", "--"}, files...)...)
	var exit *exec.ExitError
	switch {
	case result.TimedOut:
		return nil, fmt.Errorf("no answer within %ds", seconds)
	case result.Leftover:
		return nil, fmt.Errorf("exited but left a process running, which was stopped: %v", result.Err)
	case result.Err == nil && strings.TrimSpace(result.Stderr) != "":
		return nil, fmt.Errorf("warned: %s", strings.TrimSpace(result.Stderr))
	case result.Err == nil:
		return openAmong(result.Stdout, files), nil
	case errors.As(result.Err, &exit) && exit.ExitCode() == 1 && result.Output() == "":
		return map[string]bool{}, nil
	case errors.As(result.Err, &exit) && exit.ExitCode() == 1 && strings.TrimSpace(result.Stderr) == "":
		return openAmong(result.Stdout, files), nil
	case errors.As(result.Err, &exit):
		return nil, fmt.Errorf("exit %d: %s", exit.ExitCode(), result.Output())
	default:
		return nil, result.Err
	}
}

func openAmong(listing string, files []string) map[string]bool {
	asked := map[string]bool{}
	for _, file := range files {
		asked[file] = true
	}
	open := map[string]bool{}
	for _, line := range strings.Split(listing, "\n") {
		if name, ok := strings.CutPrefix(line, "n"); ok && asked[name] {
			open[name] = true
		}
	}
	if len(open) == 0 {
		return asked
	}
	return open
}
