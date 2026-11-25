package rule

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type calls struct {
	run   []string
	reply func(line string) process.Result
}

func (c *calls) commands() Commands {
	return Commands{
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		Run: func(_ time.Duration, name string, args ...string) process.Result {
			line := strings.Join(append([]string{name}, args...), " ")
			c.run = append(c.run, line)
			return c.reply(line)
		},
	}
}

func commandStep(t *testing.T, body string, env Environment) Rule {
	return parse(t, "tool-cache.yaml", "order: 1\ndescribes: the tool's caches\n"+body).Rule(env)
}

func TestAProcessStartingBetweenCommandsKeepsTheRest(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	busy := false
	c := &calls{reply: func(line string) process.Result {
		switch line {
		case "tool where first":
			return process.Result{Stdout: first}
		case "tool where second":
			return process.Result{Stdout: second}
		case "tool clean first":
			busy = true
		}
		return process.Result{}
	}}
	env := environment(func(string) (bool, error) { return busy, nil })
	env.Commands = c.commands()
	s := commandStep(t, `busy: {processes: [tool build], says: a build runs}
remove:
  - root: {command: [tool, where, first]}
    run: [tool, clean, first]
    size: {measure: root}
  - root: {command: [tool, where, second]}
    run: [tool, clean, second]
    size: {measure: root}
`, env)

	r, _, stderr := run(s, false)

	assert.NotContains(t, c.run, "tool clean second")
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stderr, "a build runs")
}

func readyStep(t *testing.T, reply process.Result, running func(string) (bool, error)) (*calls, Rule) {
	c := &calls{reply: func(line string) process.Result {
		if line == "tool info" {
			return reply
		}
		return process.Result{Stdout: "Images\t1GB"}
	}}
	env := environment(running)
	env.Commands = c.commands()
	return c, commandStep(t, `ready:
  run: [tool, info]
  timeout: 5s
  hung: restart the tool
  down: {processes: [tool-daemon], says: the tool is not running}
remove:
  run: [tool, prune]
  estimate: {run: [tool, df], match: '^Images\t([^ ]+)'}
`, env)
}

func TestAReadyCommandThatLeftAProcessFails(t *testing.T) {
	c, s := readyStep(t, process.Result{Leftover: true, Err: errors.New("it left a process running")}, idle)

	r, _, _ := run(s, false)

	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "tool info: exited but left a process running")
	assert.Equal(t, []string{"tool info"}, c.run)
}

func TestAFailedProcessCheckAfterAFailedReadyFails(t *testing.T) {
	broken := func(string) (bool, error) { return false, errors.New("process list unavailable") }
	c, s := readyStep(t, process.Result{Err: errors.New("exit status 1")}, broken)

	r, _, _ := run(s, false)

	assert.Len(t, r.Failures, 2)
	assert.Contains(t, r.Failures[0], "process list unavailable")
	assert.Equal(t, []string{"tool info"}, c.run)
}

func TestAProgramThatCannotBeLookedUpFails(t *testing.T) {
	env := environment(idle)
	env.Commands = Commands{LookPath: func(string) (string, error) {
		return "", &exec.Error{Name: "tool", Err: os.ErrPermission}
	}}
	s := commandStep(t, "needs: [tool]\nremove: {run: [tool, clean], dry-run: [-n]}\n", env)

	r, _, _ := run(s, false)

	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "look up tool")
}

func TestAMissingProgramIsQuiet(t *testing.T) {
	env := environment(idle)
	env.Commands = Commands{LookPath: func(string) (string, error) { return "", exec.ErrNotFound }}
	s := commandStep(t, "needs: [tool]\nremove: {run: [tool, clean], dry-run: [-n]}\n", env)

	r, stdout, stderr := run(s, false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestAMeasuredFolderThatDoesNotExistCountsNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	c := &calls{reply: func(string) process.Result { return process.Result{} }}
	env := environment(idle)
	env.Commands = c.commands()
	s := commandStep(t, fmt.Sprintf("remove: {run: [tool, clean], size: {measure: %s}}\n", missing), env)

	r, _, _ := run(s, false)

	assert.Empty(t, r.Failures)
	assert.Equal(t, 1, r.Count)
	assert.Zero(t, r.KB)
}
