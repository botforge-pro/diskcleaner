package rule

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const openAnswersPerListing = 50

type Environment struct {
	Home      string
	Under     string
	Getenv    func(string) string
	Processes Processes
	Commands  Commands
	Duration  func(key string) time.Duration
	Count     func(key string) int
	Dirs      func(key string) []string
	Now       func() time.Time
	Lsof      inuse.Lsof
	Mounts    func() (map[string]bool, error)
}

type Processes struct {
	Running   func(pattern string) (bool, error)
	Pids      func(pattern string) ([]string, error)
	CPUTimes  func(pids []string) (string, error)
	Terminate func(pattern string) error
	Sleep     func(time.Duration)
}

func SystemProcesses() Processes {
	return Processes{
		Running:   process.Running,
		Pids:      process.Pids,
		CPUTimes:  process.CPUTimes,
		Terminate: process.Terminate,
		Sleep:     time.Sleep,
	}
}

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

func MountPoints() (map[string]bool, error) {
	return mountPoints()
}

type Rule struct {
	file File
	env  Environment
}

func (f File) Rule(env Environment) Rule {
	return Rule{file: f, env: env}
}

type headed struct {
	w     io.Writer
	start func()
}

func (h headed) Write(p []byte) (int, error) {
	h.start()
	return h.w.Write(p)
}

type header struct {
	out     io.Writer
	label   string
	where   string
	started bool
}

func (h *header) start() {
	if !h.started {
		h.started = true
		fmt.Fprintf(h.out, "%-8s %s\n", h.label, h.where)
	}
}

func (h *header) at(where string) {
	if h.started && where != h.where {
		fmt.Fprintln(h.out)
		h.started = false
	}
	h.where = where
}

type shared struct {
	r      *report.Report
	env    Environment
	file   *File
	h      *header
	open   *inuse.Listing
	mounts map[string]bool
	home   string
	broken bool
}

type guard struct {
	c       *ctx
	node    *Node
	stopped bool
	fresh   bool
}

type ctx struct {
	*shared
	vars     map[string][]string
	folder   string
	written  string
	fallback bool
	guards   []*guard
}

func (s Rule) Run(r *report.Report) {
	h := &header{out: r.Out, label: s.file.Label(), where: s.file.Describes}
	out, errs := r.Out, r.Err
	r.Out, r.Err = headed{out, h.start}, headed{errs, h.start}
	defer func() {
		r.Out, r.Err = out, errs
		if h.started {
			fmt.Fprintln(out)
		}
	}()
	home, err := settings.Canonical(s.env.Home)
	if err != nil {
		home = filepath.Clean(s.env.Home)
	}
	c := &ctx{
		shared: &shared{r: r, env: s.env, file: &s.file, h: h, open: s.env.Lsof.Every(openAnswersPerListing), home: home},
		vars:   map[string][]string{},
	}
	c.node(&s.file.Node, true, nil)
}

func (c *ctx) child() *ctx {
	vars := make(map[string][]string, len(c.vars))
	for name, values := range c.vars {
		vars[name] = values
	}
	return &ctx{shared: c.shared, vars: vars, folder: c.folder, guards: c.guards}
}

func (c *ctx) halted() bool {
	if c.broken {
		return true
	}
	for _, g := range c.guards {
		if g.stopped {
			return true
		}
	}
	return false
}

func (c *ctx) allowed() bool {
	for _, g := range c.guards {
		if g.fresh {
			g.fresh = false
			continue
		}
		if g.stopped || !g.c.passes(g.node) {
			g.stopped = true
			return false
		}
	}
	return !c.broken
}

func (c *ctx) node(n *Node, heads bool, roots []string) {
	named := ""
	if n.Root != nil && roots == nil {
		var ok bool
		if roots, ok = c.eval(*n.Root, c.folder); !ok {
			c.broken = true
			return
		}
		named = c.namesFolders(*n.Root)
	}
	if n.Root == nil {
		roots = []string{c.folder}
	}
	seen := map[string]bool{}
	for _, root := range roots {
		if c.halted() {
			return
		}
		inner := c.child()
		if n.Root != nil {
			folder, ok := c.place(root, named)
			if !ok || seen[folder] {
				continue
			}
			seen[folder] = true
			inner.folder, inner.written = folder, root
			if heads {
				c.h.at(folder)
			}
		}
		if inner.folder != "" {
			inner.vars["path"] = []string{inner.folder}
			inner.vars["name"] = []string{filepath.Base(inner.folder)}
		}
		inner.run(n, heads)
	}
}

func (c *ctx) run(n *Node, heads bool) {
	for _, b := range n.Let {
		values, ok := c.eval(b.Value, c.folder)
		if !ok {
			c.broken = true
			return
		}
		c.vars[b.Name] = values
	}
	g := &guard{c: c, node: n}
	c.guards = append(c.guards[:len(c.guards):len(c.guards)], g)
	searchesFirst := n.Remove != nil && !n.Remove.Itself && n.Stop == nil && len(n.Do) == 0 && len(n.First) == 0
	if !searchesFirst {
		if !c.passes(n) {
			return
		}
		g.fresh = true
	}
	if n.Stop != nil && !c.stopIdle(n.Stop) {
		g.stopped = true
		return
	}
	if n.Remove != nil {
		c.removeIn(n)
	}
	if len(n.Run) > 0 && !c.halted() {
		c.action(n)
	}
	for i := range n.Do {
		if c.halted() {
			return
		}
		c.child().node(&n.Do[i], false, nil)
	}
	c.first(n.First, heads)
}

func (c *ctx) first(children []Node, heads bool) {
	for i := range children {
		ch := &children[i]
		inner := c.child()
		inner.fallback = i < len(children)-1
		roots, ok := inner.eval(*ch.Root, c.folder)
		if !ok {
			c.broken = true
			return
		}
		if len(roots) > 0 {
			c.child().node(ch, heads, roots)
			return
		}
	}
}

func (c *ctx) namesFolders(v Value) string {
	match := placeholder.FindStringSubmatch(v.Text)
	if match == nil || match[0] != v.Text {
		return ""
	}
	if _, let := c.vars[match[1]]; let {
		return ""
	}
	return c.file.configKey(match[1])
}

func (c *ctx) place(root, named string) (string, bool) {
	stat := os.Stat
	if c.folder != "" && strings.HasPrefix(root, c.folder+"/") {
		stat = os.Lstat
	}
	info, err := stat(root)
	switch {
	case err == nil && info.IsDir():
	case named != "" && (err == nil || errors.Is(err, fs.ErrNotExist)):
		c.r.Fail("%s: not a folder; it is named by %s", root, named)
		return "", false
	case err == nil, errors.Is(err, fs.ErrNotExist):
		return "", false
	default:
		c.r.Fail("stat %s: %v", root, err)
		return "", false
	}
	folder, err := settings.Canonical(root)
	if err != nil {
		c.r.Fail("resolve %s: %v", root, err)
		return "", false
	}
	if folder == "/" || folder == c.home || strings.HasPrefix(c.home, folder+"/") {
		c.r.Fail("root %s: refused, as removing in it would reach /, the home folder or a folder above it", root)
		c.broken = true
		return "", false
	}
	return folder, true
}

func (c *ctx) passes(n *Node) bool {
	for _, cond := range n.When {
		held, ok := c.holds(cond, c.folder, c.folder)
		if !ok {
			c.broken = true
			return false
		}
		if !held {
			c.refuse(cond)
			return false
		}
	}
	for _, cond := range n.Unless {
		held, ok := c.holds(cond, c.folder, c.folder)
		if !ok {
			c.broken = true
			return false
		}
		if held {
			c.refuse(cond)
			return false
		}
	}
	return true
}

func (c *ctx) refuse(cond Cond) {
	switch {
	case cond.Says != "":
		c.r.Skip("%s: %s", c.subject(), cond.Says)
	case cond.Fails != "":
		c.r.Fail("%s: %s", c.subject(), cond.Fails)
	}
}

func (c *ctx) subject() string {
	if c.folder == "" {
		return c.file.Name
	}
	return c.folder
}

func (c *ctx) duration(a Amount) time.Duration {
	if a == "" {
		return 0
	}
	if key, ok := a.setting(); ok {
		return c.env.Duration(c.file.configKey(key))
	}
	value, _ := literal(a, "duration")
	return value.(time.Duration)
}

func (c *ctx) count(a Amount) int {
	if key, ok := a.setting(); ok {
		return c.env.Count(c.file.configKey(key))
	}
	value, _ := literal(a, "count")
	return value.(int)
}

func (c *ctx) within(a Amount) string {
	limit := report.FormatDuration(c.duration(a))
	if key, ok := a.setting(); ok {
		return fmt.Sprintf("%s (%s)", limit, c.file.configKey(key))
	}
	return limit
}
