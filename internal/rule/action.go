package rule

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

const stopPoll = time.Second

type sizing struct {
	text  string
	kb    int64
	known bool
}

func (c *ctx) action(n *Node) {
	if !c.allowed() {
		return
	}
	run, ok := c.fillAll(n.Run)
	if !ok {
		c.broken = true
		return
	}
	words := strings.Join(run, " ")
	measured := ""
	if n.Size != nil {
		paths, ok := c.eval(*n.Size, c.folder)
		if !ok || len(paths) != 1 {
			c.broken = true
			return
		}
		measured = paths[0]
	}
	var size sizing
	if e := n.Estimate; e != nil && len(e.Run) > 0 {
		args, ok := c.fillAll(e.Run)
		if !ok {
			c.broken = true
			return
		}
		output, ok := c.command(args, e.Timeout)
		if !ok {
			return
		}
		if size, ok = c.sized(e, words, output); !ok || !size.known || size.kb == 0 {
			return
		}
	}
	beforeKB := int64(0)
	if _, err := os.Lstat(measured); measured != "" && errors.Is(err, fs.ErrNotExist) {
		if !size.known {
			size = sizing{text: report.FormatKB(0) + " in " + measured, known: true}
		}
		measured = ""
	}
	if measured != "" {
		var err error
		if beforeKB, err = report.DiskUsageKB(measured); err != nil {
			c.r.Fail("du %s: %v", measured, err)
		}
		if !size.known {
			size = sizing{text: report.FormatKB(beforeKB) + " in " + measured, kb: beforeKB, known: true}
		}
	}
	if c.r.DryRun {
		if !size.known {
			dry, ok := c.fillAll(n.DryRun)
			if !ok {
				return
			}
			output, ok := c.command(append(run, dry...), n.Timeout)
			if !ok {
				return
			}
			if size, ok = c.sized(n.Estimate, words, output); !ok || !size.known {
				return
			}
		}
		fmt.Fprintf(c.r.Out, "WOULD RUN     %s  (%s)\n", words, size.text)
		c.r.Count++
		c.r.KB += size.kb
		return
	}
	result := c.env.Commands.Run(c.duration(n.Timeout), run[0], run[1:]...)
	if !size.known && result.Err == nil && n.Estimate != nil {
		var ok bool
		if size, ok = c.sized(n.Estimate, words, result.Output()); !ok || !size.known {
			return
		}
	}
	if size.known {
		fmt.Fprintf(c.r.Out, "RUN           %s  (%s)\n", words, size.text)
	} else {
		fmt.Fprintf(c.r.Out, "RUN           %s\n", words)
	}
	if n.shows != nil {
		for _, line := range strings.Split(result.Stdout, "\n") {
			if n.shows.MatchString(line) {
				fmt.Fprintln(c.r.Out, line)
			}
		}
	}
	switch {
	case result.TimedOut:
		c.r.Fail("%s did not finish within %s", words, c.within(n.Timeout))
	case result.Err != nil:
		c.r.Fail("%s: %v: %s", words, result.Err, result.Output())
	default:
		c.r.Count++
		if measured == "" {
			c.r.KB += size.kb
		}
	}
	if measured != "" {
		c.r.CountFreed(measured, beforeKB, words)
	}
}

func (c *ctx) command(args []string, timeout Amount) (string, bool) {
	words := strings.Join(args, " ")
	result := c.env.Commands.Run(c.duration(timeout), args[0], args[1:]...)
	switch {
	case result.TimedOut:
		c.r.Fail("%s did not answer within %s", words, c.within(timeout))
		return "", false
	case result.Err != nil:
		c.r.Fail("%s: %v: %s", words, result.Err, result.Output())
		return "", false
	}
	return result.Output(), true
}

func (c *ctx) sized(e *Estimate, words, output string) (sizing, bool) {
	if e == nil {
		return sizing{}, true
	}
	line := ""
	var found []string
	for _, candidate := range strings.Split(strings.ReplaceAll(output, "\r", "\n"), "\n") {
		if match := e.match.FindStringSubmatch(candidate); match != nil {
			line, found = strings.TrimSpace(candidate), match
		}
	}
	if found == nil {
		return sizing{}, true
	}
	text := found[1]
	size := text
	if e.Units == "binary" {
		if unit := strings.TrimLeft(size, "0123456789., "); unit != "B" {
			size = strings.TrimSuffix(size, "B") + "iB"
		}
	}
	kb, ok := report.SizeKB(size)
	if !ok {
		c.r.Fail("%s: cannot read the size from: %s", words, line)
		return sizing{}, false
	}
	return sizing{text: text, kb: kb, known: true}, true
}

func (c *ctx) stopIdle(s *Stop) bool {
	pattern, ok := c.pattern(s.Running)
	if !ok {
		return false
	}
	times, ok := c.cpuTimes(pattern, s.Names)
	if !ok {
		return false
	}
	if times == "" {
		return true
	}
	c.env.Processes.Sleep(c.duration(s.IdleFor))
	again, ok := c.cpuTimes(pattern, s.Names)
	if !ok {
		return false
	}
	if again != times {
		c.r.Skip("%s: %s", c.subject(), s.Says)
		return false
	}
	if c.r.DryRun {
		fmt.Fprintf(c.r.Out, "WOULD STOP    %s\n", s.Names)
		return true
	}
	fmt.Fprintf(c.r.Out, "STOP          %s\n", s.Names)
	if err := c.env.Processes.Terminate(pattern); err != nil {
		c.r.Fail("pkill -TERM -f '%s': %v", pattern, err)
		return false
	}
	limit := c.duration(s.Timeout)
	for waited := time.Duration(0); ; waited += stopPoll {
		pids, err := c.env.Processes.Pids(pattern)
		if err != nil {
			c.r.Fail("pgrep -f '%s': %v", pattern, err)
			return false
		}
		if len(pids) == 0 {
			return true
		}
		if waited >= limit {
			c.r.Fail("%s: %s did not stop within %s", c.subject(), s.Names, limit)
			return false
		}
		c.env.Processes.Sleep(stopPoll)
	}
}

func (c *ctx) cpuTimes(pattern, names string) (string, bool) {
	pids, err := c.env.Processes.Pids(pattern)
	if err != nil {
		c.r.Fail("pgrep -f '%s': %v", pattern, err)
		return "", false
	}
	times, err := c.env.Processes.CPUTimes(pids)
	if err != nil {
		c.r.Fail("ps of %s: %v", names, err)
		return "", false
	}
	return times, true
}
