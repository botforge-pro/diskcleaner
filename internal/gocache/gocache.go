package gocache

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

type Step struct {
	Go        string
	Processes string
	Running   func(pattern string) (bool, error)
}

type cache struct {
	variable string
	flag     string
}

var caches = []cache{
	{variable: "GOMODCACHE", flag: "-modcache"},
	{variable: "GOCACHE", flag: "-cache"},
}

func Default() Step {
	return Step{
		Go:        "go",
		Processes: settings.Env("DISKCLEANER_GO_PROCESSES", "(^|/)go (build|test|run|install|get|mod|generate|vet|list)( |$)"),
		Running:   process.Running,
	}
}

func (s Step) Run(r *report.Report) {
	_, err := exec.LookPath(s.Go)
	if errors.Is(err, exec.ErrNotFound) {
		return
	}
	if err != nil {
		r.Fail("look up %s: %v", s.Go, err)
		return
	}
	fmt.Fprintln(r.Out, "GO       module and build caches")
	defer fmt.Fprintln(r.Out)

	if r.Busy(s.Running, s.Processes) {
		r.Skip("go caches: a go command is running")
		return
	}
	for _, c := range caches {
		dir, ok := s.env(r, c.variable)
		if ok && dir != "" && r.Directory(dir) {
			r.RunCleanup(dir, s.Go, "clean", c.flag)
		}
	}
}

func (s Step) env(r *report.Report, variable string) (string, bool) {
	var stdout, stderr bytes.Buffer
	command := exec.Command(s.Go, "env", variable)
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	diagnostics := strings.TrimSpace(stderr.String())
	if err != nil {
		r.Fail("go env %s: %v: %s", variable, err, diagnostics)
		return "", false
	}
	if diagnostics != "" {
		fmt.Fprintln(r.Err, diagnostics)
	}
	value := strings.TrimSpace(stdout.String())
	if strings.Contains(value, "\n") {
		r.Fail("go env %s: expected one line, got %q", variable, value)
		return "", false
	}
	return value, true
}
