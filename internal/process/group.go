package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type Result struct {
	Stdout   string
	Stderr   string
	TimedOut bool
	Leftover bool
	Err      error
}

func (r Result) Output() string {
	return strings.TrimSpace(strings.TrimSpace(r.Stdout) + "\n" + strings.TrimSpace(r.Stderr))
}

const killGrace = 5 * time.Second

func RunGroup(seconds int, name string, args ...string) Result {
	return awaitGroup(time.Duration(seconds)*time.Second, killGrace, func(ctx context.Context) Result {
		return runGroup(ctx, name, args...)
	})
}

func awaitGroup(limit, grace time.Duration, run func(context.Context) Result) Result {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	finished := make(chan Result, 1)
	go func() { finished <- run(ctx) }()
	select {
	case result := <-finished:
		return result
	case <-time.After(limit + grace):
		return Result{
			TimedOut: true,
			Leftover: true,
			Err:      fmt.Errorf("still running %s past its %s limit, left behind", grace, limit),
		}
	}
}

func runGroup(ctx context.Context, name string, args ...string) Result {
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	err := command.Run()
	result := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    err,
	}
	if command.Process != nil {
		group := -command.Process.Pid
		probe := syscall.Kill(group, 0)
		result.Leftover = probe == nil || errors.Is(probe, syscall.EPERM)
		if result.Leftover {
			result.Err = errors.Join(err, errors.New("it left a process running"))
			if killErr := syscall.Kill(group, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
				result.Err = errors.Join(result.Err, fmt.Errorf("stop its leftover processes: %w", killErr))
			}
		}
	}
	result.TimedOut = err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)
	return result
}
