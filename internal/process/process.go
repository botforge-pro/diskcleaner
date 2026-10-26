package process

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

const (
	Xcode = `xcodebuild|XCBBuildService|/Xcode[^/]*\.app/Contents/MacOS/Xcode`
	Go    = "(^|/)go (build|test|run|install|get|mod|generate|vet|list)( |$)"
)

func Running(pattern string) (bool, error) {
	pids, err := Pids(pattern)
	return len(pids) > 0, err
}

func Pids(pattern string) ([]string, error) {
	stdout, err := run("pgrep", "-f", pattern)
	if err != nil {
		return nil, err
	}
	return strings.Fields(stdout), nil
}

func CPUTimes(pids []string) (string, error) {
	if len(pids) == 0 {
		return "", nil
	}
	stdout, err := run("ps", "-o", "pid=,time=", "-p", strings.Join(pids, ","))
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n"), nil
}

func Terminate(pattern string) error {
	_, err := run("pkill", "-TERM", "-f", pattern)
	return err
}

func run(name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	command := exec.Command(name, args...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && stderr.Len() == 0 {
		return stdout.String(), nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
