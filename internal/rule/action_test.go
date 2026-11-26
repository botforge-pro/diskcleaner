package rule

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/botforge-pro/diskcleaner/internal/process"
)

type answers map[string]process.Result

func (a answers) commands(ran *[]string) Commands {
	return Commands{
		LookPath: func(name string) (string, error) {
			for line := range a {
				if strings.HasPrefix(line, name+" ") || line == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", exec.ErrNotFound
		},
		Run: func(_ time.Duration, name string, args ...string) process.Result {
			line := strings.Join(append([]string{name}, args...), " ")
			*ran = append(*ran, line)
			result, ok := a[line]
			if !ok {
				return process.Result{Err: fmt.Errorf("not expected: %s", line)}
			}
			return result
		},
	}
}

func commanding(t *testing.T, body string, a answers, ran *[]string) Rule {
	env := environment("/nowhere")
	env.Commands = a.commands(ran)
	return parse(t, "001-tool-cache.yaml", "describes: x\n"+body).Rule(env)
}

func TestAProcessStartingBetweenCommandsKeepsTheRest(t *testing.T) {
	first, second := temp(t), temp(t)
	write(t, filepath.Join(first, "x"))
	write(t, filepath.Join(second, "x"))
	var ran []string
	body := "unless: [{running: [tool], says: a tool runs}]\ndo:\n  - {root: " + first + ", run: [tool, clean, one], size: '{path}'}\n  - {root: " + second + ", run: [tool, clean, two], size: '{path}'}\n"
	s := commanding(t, body, answers{"tool clean one": {}, "tool clean two": {}}, &ran)
	env := s.env
	calls := 0
	env.Processes.Running = func(string) (bool, error) {
		calls++
		return calls > 1, nil
	}
	s.env = env

	_, _, stderr := run(s, false)

	assert.Equal(t, []string{"tool clean one"}, ran)
	assert.Contains(t, stderr, "a tool runs")
}

func TestAMissingProgramIsQuietAndAFailedLookupFails(t *testing.T) {
	var ran []string
	body := "when: [{program: tool}]\nrun: [tool, clean]\ndry-run: [-n]\n"
	r, stdout, stderr := run(commanding(t, body, answers{}, &ran), false)

	assert.Empty(t, ran)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Empty(t, r.Failures)

	s := commanding(t, body, answers{}, &ran)
	env := s.env
	env.Commands.LookPath = func(string) (string, error) { return "", errors.New("permission denied") }
	s.env = env

	r, _, _ = run(s, false)

	assert.Equal(t, []string{"look up tool: permission denied"}, r.Failures)
}

func TestAnEstimateOfNothingRunsNothing(t *testing.T) {
	var ran []string
	body := "run: [tool, prune]\nestimate: {run: [tool, df], match: '^Images\\t(\\S+)'}\n"
	for _, df := range []string{"Images\t0B (0%)", "Volumes\t2GB"} {
		ran = nil
		r, stdout, _ := run(commanding(t, body, answers{"tool df": {Stdout: df}, "tool prune": {}}, &ran), false)

		assert.Equal(t, []string{"tool df"}, ran, df)
		assert.Zero(t, r.Count)
		assert.Empty(t, stdout)
	}
}

func TestTheSizeOfAToolThatPrintsItIsReadFromItsOwnRunOrDryRun(t *testing.T) {
	var ran []string
	body := "run: [tool, cleanup]\ndry-run: [--dry-run]\nestimate: {match: 'would free (.+?) of disk space', units: binary}\n"
	a := answers{"tool cleanup --dry-run": {Stdout: "would free 1KB of disk space"}, "tool cleanup": {Stdout: "would free 2KB of disk space"}}

	r, stdout, _ := run(commanding(t, body, a, &ran), true)

	assert.Equal(t, []string{"tool cleanup --dry-run"}, ran)
	assert.Contains(t, stdout, "WOULD RUN     tool cleanup  (1KB)")
	assert.EqualValues(t, 1, r.KB)

	ran = nil
	r, stdout, _ = run(commanding(t, body, a, &ran), false)

	assert.Equal(t, []string{"tool cleanup"}, ran)
	assert.Contains(t, stdout, "RUN           tool cleanup  (2KB)")
	assert.EqualValues(t, 2, r.KB)
}

func TestACommandThatFailsMakesItsValueUnsetOnlyWhenAChoiceFollows(t *testing.T) {
	custom, standard := temp(t), temp(t)
	write(t, filepath.Join(standard, "Build", "x"))
	var ran []string
	body := "first:\n  - {root: {run: [tool, where]}, remove: [{folders: '*'}]}\n  - {root: " + standard + ", remove: [{folders: '*'}]}\n"

	r, _, _ := run(commanding(t, body, answers{"tool where": {Err: errors.New("exit status 1")}}, &ran), false)

	assert.Empty(t, r.Failures)
	assert.NoDirExists(t, filepath.Join(standard, "Build"))

	write(t, filepath.Join(custom, "Build", "x"))
	r, _, _ = run(commanding(t, "root: {run: [tool, where]}\nremove: [{folders: '*'}]\n", answers{"tool where": {Err: errors.New("exit status 1"), Stderr: "no such key"}}, &ran), false)

	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "tool where: exit status 1: no such key")
}

func TestACheckThatTimesOutFailsWithWhatToDo(t *testing.T) {
	var ran []string
	body := "when: [{succeeds: {run: [tool, info], timeout: 30s}, fails: restart the tool}]\nrun: [tool, prune]\ndry-run: [-n]\n"

	r, _, _ := run(commanding(t, body, answers{"tool info": {TimedOut: true, Err: errors.New("killed")}}, &ran), false)

	assert.Equal(t, []string{"tool info did not answer within 30s: restart the tool"}, r.Failures)
	assert.Equal(t, []string{"tool info"}, ran)
}

func TestAMeasuredFolderThatDoesNotExistCountsNothing(t *testing.T) {
	var ran []string
	body := "run: [tool, clean]\nsize: " + filepath.Join(temp(t), "absent") + "\n"

	r, stdout, _ := run(commanding(t, body, answers{"tool clean": {}}, &ran), false)

	assert.Equal(t, 1, r.Count)
	assert.Zero(t, r.KB)
	assert.Contains(t, stdout, "RUN           tool clean  (0 B in ")
}
