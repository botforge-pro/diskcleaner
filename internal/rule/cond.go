package rule

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type Cond struct {
	Exists   string     `yaml:"exists"`
	Above    []Cond     `yaml:"above"`
	Value    *Value     `yaml:"value"`
	Is       *string    `yaml:"is"`
	Running  []Proc     `yaml:"running"`
	Succeeds *Succeeds  `yaml:"succeeds"`
	Alive    *NameValue `yaml:"alive"`
	Program  string     `yaml:"program"`
	Any      []Cond     `yaml:"any"`
	Says     string     `yaml:"says"`
	Fails    string     `yaml:"fails"`
}

type Succeeds struct {
	Run     []string `yaml:"run"`
	Timeout Amount   `yaml:"timeout"`
}

const isSet = "set"

func (c checker) cond(cond Cond) error {
	return c.condAt(cond, true)
}

func (c checker) condAt(cond Cond, explains bool) error {
	atoms := 0
	for _, given := range []bool{cond.Exists != "", cond.Above != nil, cond.Value != nil, cond.Running != nil, cond.Succeeds != nil, cond.Alive != nil, cond.Program != "", cond.Any != nil} {
		if given {
			atoms++
		}
	}
	switch {
	case atoms != 1:
		return errors.New("a condition: want one of exists, above, value with is, running, succeeds, alive, program and any")
	case (cond.Value == nil) != (cond.Is == nil):
		return errors.New("a condition: want value and is together")
	case !explains && (cond.Says != "" || cond.Fails != "" && cond.Succeeds == nil):
		return errors.New("a condition inside above, any or a remove entry: says and fails belong to the node's own conditions or to the entry; only succeeds may say what its timeout fails with")
	case cond.Says != "" && cond.Fails != "":
		return errors.New("a condition: want says or fails, not both")
	case cond.Exists != "":
		if filepath.IsAbs(cond.Exists) {
			return fmt.Errorf("exists %q: want a path relative to what is asked about", cond.Exists)
		}
		return c.text(cond.Exists, "exists", false)
	case cond.Value != nil:
		return c.value(*cond.Value, "value")
	case cond.Running != nil:
		return c.processes(cond.Running, "running")
	case cond.Succeeds != nil:
		if len(cond.Succeeds.Run) == 0 {
			return errors.New("succeeds: want run, the command that must exit 0")
		}
		if cond.Succeeds.Timeout != "" {
			if err := c.amount(cond.Succeeds.Timeout, "succeeds: timeout", "timeout"); err != nil {
				return err
			}
		}
		return c.words(cond.Succeeds.Run, "succeeds")
	case cond.Alive != nil:
		v := Value{Name: cond.Alive}
		return c.value(v, "alive")
	}
	for _, inner := range append(cond.Above, cond.Any...) {
		if err := c.condAt(inner, false); err != nil {
			return err
		}
	}
	return nil
}

func (c *ctx) holds(cond Cond, about, node string) (bool, bool) {
	switch {
	case cond.Exists != "":
		path, ok := c.fill(cond.Exists, false)
		if !ok {
			return false, false
		}
		return present(about, path), true
	case cond.Above != nil:
		for dir := filepath.Dir(about); ; dir = filepath.Dir(dir) {
			all, ok := c.all(cond.Above, dir, node)
			if !ok || all {
				return all, ok
			}
			if dir == node || dir == filepath.Dir(dir) {
				return false, true
			}
		}
	case cond.Value != nil:
		found, ok := c.eval(*cond.Value, about)
		if !ok {
			return false, false
		}
		if *cond.Is == isSet {
			return len(found) > 0, true
		}
		return len(found) == 1 && found[0] == *cond.Is, true
	case cond.Running != nil:
		pattern, ok := c.pattern(cond.Running)
		if !ok {
			return false, false
		}
		busy, err := c.env.Processes.Running(pattern)
		if err != nil {
			c.r.Fail("pgrep -f '%s': %v", pattern, err)
			return false, false
		}
		return busy, true
	case cond.Succeeds != nil:
		return c.succeeds(cond)
	case cond.Alive != nil:
		return alive(cond.Alive, about), true
	case cond.Program != "":
		_, err := c.env.Commands.LookPath(cond.Program)
		if errors.Is(err, exec.ErrNotFound) {
			return false, true
		}
		if err != nil {
			c.r.Fail("look up %s: %v", cond.Program, err)
			return false, false
		}
		return true, true
	}
	for _, inner := range cond.Any {
		held, ok := c.holds(inner, about, node)
		if !ok || held {
			return held, ok
		}
	}
	return false, true
}

func (c *ctx) all(conds []Cond, about, node string) (bool, bool) {
	for _, cond := range conds {
		held, ok := c.holds(cond, about, node)
		if !ok || !held {
			return false, ok
		}
	}
	return true, true
}

func (c *ctx) succeeds(cond Cond) (bool, bool) {
	s := cond.Succeeds
	args, ok := c.fillAll(s.Run)
	if !ok {
		return false, false
	}
	words := strings.Join(args, " ")
	result := c.env.Commands.Run(c.duration(s.Timeout), args[0], args[1:]...)
	switch {
	case result.TimedOut:
		explained := ""
		if cond.Fails != "" {
			explained = ": " + cond.Fails
		}
		c.r.Fail("%s did not answer within %s%s", words, c.within(s.Timeout), explained)
		return false, false
	case result.Leftover:
		c.r.Fail("%s: exited but left a process running, which was stopped: %v", words, result.Err)
		return false, false
	}
	return result.Err == nil, true
}

func (c *ctx) pattern(procs []Proc) (string, bool) {
	var patterns []string
	for _, p := range procs {
		if p.Group != "" {
			patterns = append(patterns, c.file.groups[p.Group]...)
			continue
		}
		filled, ok := c.fill(p.Pattern, true)
		if !ok {
			return "", false
		}
		patterns = append(patterns, filled)
	}
	return strings.Join(patterns, "|"), true
}

func alive(name *NameValue, about string) bool {
	match := name.re.FindStringSubmatch(filepath.Base(about))
	if match == nil {
		return false
	}
	pid, err := strconv.Atoi(match[1])
	if err != nil {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func present(about, path string) bool {
	wantFolder := strings.HasSuffix(path, "/")
	full := filepath.Join(about, strings.TrimSuffix(path, "/"))
	matches := []string{full}
	if strings.ContainsAny(full, `*?[`) {
		var err error
		if matches, err = filepath.Glob(full); err != nil {
			return false
		}
	}
	for _, match := range matches {
		info, err := os.Lstat(match)
		if err == nil && (!wantFolder || info.IsDir()) {
			return true
		}
	}
	return false
}
