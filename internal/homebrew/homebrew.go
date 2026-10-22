package homebrew

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const cleanup = "brew cleanup --prune=all"

var freed = regexp.MustCompile(`(?:would free|has freed) approximately (.+?) of disk space`)

type Step struct {
	Brew           string
	Processes      string
	TimeoutSeconds int
	Running        func(pattern string) (bool, error)
}

func Default() (Step, error) {
	timeout, err := settings.PositiveDuration("DISKCLEANER_BREW_TIMEOUT", 900, time.Second, "seconds")
	if err != nil {
		return Step{}, err
	}
	return Step{
		Brew:           "brew",
		Processes:      settings.Env("DISKCLEANER_BREW_PROCESSES", `/Library/Homebrew/brew\.(sh|rb)`),
		TimeoutSeconds: timeout,
		Running:        process.Running,
	}, nil
}

func (s Step) Run(r *report.Report) {
	_, err := exec.LookPath(s.Brew)
	if errors.Is(err, exec.ErrNotFound) {
		return
	}
	if err != nil {
		r.Fail("look up %s: %v", s.Brew, err)
		return
	}
	fmt.Fprintln(r.Out, "HOMEBREW old versions and downloads")
	defer fmt.Fprintln(r.Out)

	if r.Busy(s.Running, s.Processes) {
		r.Skip("homebrew: a brew command is running")
		return
	}
	args := []string{"cleanup", "--prune=all"}
	if r.DryRun {
		args = append(args, "--dry-run")
	}
	result := process.RunGroup(s.TimeoutSeconds, s.Brew, args...)
	switch {
	case result.TimedOut:
		r.Fail("%s did not finish within %ds", cleanup, s.TimeoutSeconds)
		return
	case result.Err != nil:
		r.Fail("%s: %v: %s", cleanup, result.Err, result.Output())
		return
	}
	match := freed.FindStringSubmatch(result.Output())
	if match == nil {
		return
	}
	size := match[1]
	kb, ok := binarySizeKB(size)
	if !ok {
		r.Fail("%s: cannot read the size from: %s", cleanup, match[0])
		return
	}
	verb := "RUN          "
	if r.DryRun {
		verb = "WOULD RUN    "
	}
	fmt.Fprintf(r.Out, "%s %s  (%s)\n", verb, cleanup, size)
	r.Count++
	r.KB += kb
}

func binarySizeKB(size string) (int64, bool) {
	if unit := strings.TrimLeft(size, "0123456789., "); unit != "B" {
		size = strings.TrimSuffix(size, "B") + "iB"
	}
	return report.SizeKB(size)
}
