package rule

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

type Commands struct {
	LookPath func(name string) (string, error)
	Run      func(timeout time.Duration, name string, args ...string) process.Result
}

func SystemCommands() Commands {
	return Commands{LookPath: exec.LookPath, Run: runCommand}
}

func runCommand(timeout time.Duration, name string, args ...string) process.Result {
	if timeout > 0 {
		return process.RunGroup(int(timeout/time.Second), name, args...)
	}
	var stdout, stderr bytes.Buffer
	command := exec.Command(name, args...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return process.Result{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
}

type Command struct {
	Root     Root     `yaml:"root"`
	Has      []string `yaml:"has"`
	Run      []string `yaml:"run"`
	DryRun   []string `yaml:"dry-run"`
	Timeout  string   `yaml:"timeout"`
	Estimate Estimate `yaml:"estimate"`
	Size     Size     `yaml:"size"`
	Shows    string   `yaml:"shows"`
}

type Estimate struct {
	Run     []string `yaml:"run"`
	Timeout string   `yaml:"timeout"`
	Line    string   `yaml:"line"`
	Match   string   `yaml:"match"`
	Units   string   `yaml:"units"`
	None    string   `yaml:"none"`
}

type Size struct {
	Measure string `yaml:"measure"`
}

type Ready struct {
	Run     []string `yaml:"run"`
	Timeout string   `yaml:"timeout"`
	Hung    string   `yaml:"hung"`
	Down    BusyWhen `yaml:"down"`
}

func (c Command) words() string {
	return strings.Join(c.Run, " ")
}

func (c Command) in(folder string) Command {
	at := func(words []string) []string {
		if words == nil {
			return nil
		}
		out := make([]string, len(words))
		for i, word := range words {
			out[i] = strings.ReplaceAll(word, "{path}", folder)
		}
		return out
	}
	c.Run, c.DryRun, c.Estimate.Run = at(c.Run), at(c.DryRun), at(c.Estimate.Run)
	return c
}

func (f File) checkCommand(c Command) error {
	switch {
	case len(c.Run) == 0:
		return errors.New("remove: run: want the command and its arguments")
	case c.Estimate.Match == "" && (c.Estimate.Run != nil || c.Estimate.Line != ""):
		return fmt.Errorf("remove: %s: estimate: want match, the size in its first group", c.words())
	case c.Estimate.Run == nil && c.DryRun == nil && c.Size.Measure == "":
		return fmt.Errorf("remove: %s: want dry-run, estimate: run or size: measure, so that a dry run can tell what it would free", c.words())
	case c.Estimate.Units != "" && c.Estimate.Units != "binary":
		return fmt.Errorf("remove: %s: estimate: units %q: want binary, for a size printed in KB, MB or GB that means KiB, MiB or GiB", c.words(), c.Estimate.Units)
	}
	for _, pattern := range []string{c.Estimate.Line, c.Estimate.Match, c.Estimate.None, c.Shows} {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("remove: %s: %q: %w", c.words(), pattern, err)
		}
	}
	for _, timeout := range []string{c.Timeout, c.Estimate.Timeout} {
		if err := f.checkTimeout(timeout); err != nil {
			return fmt.Errorf("remove: %s: %w", c.words(), err)
		}
	}
	for _, path := range c.Has {
		if !inside(strings.TrimSuffix(path, "/")) {
			return fmt.Errorf("remove: %s: has %q: want a path inside the folder it runs for", c.words(), path)
		}
	}
	if strings.Contains(strings.Join(slices.Concat(c.Run, c.DryRun, c.Estimate.Run), " "), "{path}") && !f.Each.given() {
		return fmt.Errorf("remove: %s: {path} names the folder of each, so want each", c.words())
	}
	if !c.Root.empty() {
		return c.Root.check(true)
	}
	return nil
}

func (f File) checkTimeout(timeout string) error {
	if timeout == "" || hasSetting(f.Settings, timeout, "timeout") {
		return nil
	}
	if _, err := literalDuration(timeout); err != nil {
		return fmt.Errorf("timeout %q: want the key of one of this step's timeout settings or a duration like 10s", timeout)
	}
	return nil
}

func hasSetting(settings []Setting, key, kind string) bool {
	for _, s := range settings {
		if s.Key == key && (kind == "" || s.Kind == kind) {
			return true
		}
	}
	return false
}

func (s Rule) timeout(text string) time.Duration {
	if text == "" {
		return 0
	}
	if hasSetting(s.file.Settings, text, "timeout") {
		return s.duration(text)
	}
	value, _ := literalDuration(text)
	return value
}

func (s Rule) needed(r *report.Report) bool {
	for _, program := range s.file.Needs {
		_, err := s.env.Commands.LookPath(program)
		if errors.Is(err, exec.ErrNotFound) {
			return false
		}
		if err != nil {
			r.Fail("look up %s: %v", program, err)
			return false
		}
	}
	return true
}

func (s Rule) ready(r *report.Report) bool {
	ready := s.file.Ready
	if len(ready.Run) == 0 {
		return true
	}
	words := strings.Join(ready.Run, " ")
	limit := s.timeout(ready.Timeout)
	result := s.env.Commands.Run(limit, ready.Run[0], ready.Run[1:]...)
	switch {
	case result.TimedOut:
		r.Fail("%s did not answer within %s: %s", words, limit, ready.Hung)
	case result.Leftover:
		r.Fail("%s: exited but left a process running, which was stopped: %v", words, result.Err)
	case result.Err != nil && !r.Busy(s.env.Processes.Running, strings.Join(ready.Down.Processes, "|")):
		r.Skip("%s: %s", s.file.Name, ready.Down.Says)
	case result.Err != nil:
		r.Fail("%s: %v: %s", words, result.Err, result.Output())
	default:
		return true
	}
	return false
}

func (s Rule) runCommand(r *report.Report, c Command, dir string) bool {
	if !c.Root.empty() {
		root, ok := s.resolve(r, c.Root)
		if !ok || !r.Directory(root) {
			return true
		}
		dir = root
	}
	if slices.ContainsFunc(c.Has, func(path string) bool { return !present(dir, path) }) {
		return true
	}
	measured := s.measured(c, dir)
	estimate, kb, known := "", int64(0), false
	if c.Estimate.Run != nil {
		var ok bool
		estimate, kb, ok = s.estimate(r, c, c.Estimate.Run, s.timeout(c.Estimate.Timeout))
		if !ok {
			return true
		}
		known = true
	}
	beforeKB := int64(0)
	if _, err := os.Lstat(measured); measured != "" && errors.Is(err, fs.ErrNotExist) {
		if !known {
			estimate, known = report.FormatKB(0)+" in "+measured, true
		}
		measured = ""
	}
	if measured != "" {
		var err error
		beforeKB, err = report.DiskUsageKB(measured)
		if err != nil {
			r.Fail("du %s: %v", measured, err)
		}
		if !known {
			estimate, kb, known = report.FormatKB(beforeKB)+" in "+measured, beforeKB, true
		}
	}
	if s.busy(r) {
		return false
	}
	if r.DryRun {
		if !known {
			args := append(append([]string{}, c.Run[1:]...), c.DryRun...)
			var ok bool
			if estimate, kb, ok = s.estimate(r, c, append([]string{c.Run[0]}, args...), s.timeout(c.Timeout)); !ok {
				return true
			}
		}
		fmt.Fprintf(r.Out, "WOULD RUN     %s  (%s)\n", c.words(), estimate)
		r.Count++
		r.KB += kb
		return true
	}
	result := s.env.Commands.Run(s.timeout(c.Timeout), c.Run[0], c.Run[1:]...)
	if !known && result.Err == nil {
		var ok bool
		if estimate, kb, ok = s.sized(r, c, result.Output()); !ok {
			return true
		}
		known = true
	}
	if known {
		fmt.Fprintf(r.Out, "RUN           %s  (%s)\n", c.words(), estimate)
	} else {
		fmt.Fprintf(r.Out, "RUN           %s\n", c.words())
	}
	if c.Shows != "" {
		shows := regexp.MustCompile(c.Shows)
		for _, line := range strings.Split(result.Stdout, "\n") {
			if shows.MatchString(line) {
				fmt.Fprintln(r.Out, line)
			}
		}
	}
	switch {
	case result.TimedOut:
		r.Fail("%s did not finish within %s", c.words(), s.timeout(c.Timeout))
	case result.Err != nil:
		r.Fail("%s: %v: %s", c.words(), result.Err, result.Output())
	default:
		r.Count++
		if measured == "" {
			r.KB += kb
		}
	}
	if measured != "" {
		r.CountFreed(measured, beforeKB, c.words())
	}
	return true
}

func (s Rule) measured(c Command, dir string) string {
	switch measure := c.Size.Measure; {
	case measure == "":
		return ""
	case measure == "root":
		return dir
	case strings.HasPrefix(measure, "~/"):
		return expand(s.env.Home, measure)
	case filepath.IsAbs(measure):
		return measure
	default:
		return filepath.Join(dir, measure)
	}
}

func (s Rule) estimate(r *report.Report, c Command, run []string, limit time.Duration) (string, int64, bool) {
	words := strings.Join(run, " ")
	result := s.env.Commands.Run(limit, run[0], run[1:]...)
	switch {
	case result.TimedOut:
		r.Fail("%s did not answer within %s", words, limit)
		return "", 0, false
	case result.Err != nil:
		r.Fail("%s: %v: %s", words, result.Err, result.Output())
		return "", 0, false
	}
	return s.sized(r, c, result.Output())
}

func (s Rule) sized(r *report.Report, c Command, output string) (string, int64, bool) {
	e := c.Estimate
	line := ""
	match := regexp.MustCompile(e.Match)
	pick := match
	if e.Line != "" {
		pick = regexp.MustCompile(e.Line)
	}
	for _, candidate := range strings.Split(strings.ReplaceAll(output, "\r", "\n"), "\n") {
		if pick.MatchString(candidate) {
			line = strings.TrimSpace(candidate)
		}
	}
	if line == "" || e.None != "" && regexp.MustCompile(e.None).MatchString(line) {
		return "", 0, false
	}
	found := match.FindStringSubmatch(line)
	if len(found) < 2 {
		r.Fail("%s: cannot read the size from: %s", c.words(), line)
		return "", 0, false
	}
	size := found[1]
	if e.Units == "binary" {
		if unit := strings.TrimLeft(size, "0123456789., "); unit != "B" {
			size = strings.TrimSuffix(size, "B") + "iB"
		}
	}
	kb, ok := report.SizeKB(size)
	if !ok {
		r.Fail("%s: cannot read the size from: %s", c.words(), line)
		return "", 0, false
	}
	return found[1], kb, true
}
