package rule

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/age"
	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const (
	openAnswersPerListing = 50
	stopPoll              = time.Second
)

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

type Rule struct {
	file   File
	root   string
	base   string
	remove Removal
	env    Environment
}

func (f File) Rule(env Environment) Rule {
	return Rule{file: f, env: env}
}

func (s Rule) Root() string {
	root, _ := s.resolve(&report.Report{Out: io.Discard, Err: io.Discard}, s.file.Root)
	return root
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
	out, errs io.Writer
	label     string
	where     string
	started   bool
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

type target struct {
	root   string
	remove *Removal
}

func (s Rule) Run(r *report.Report) {
	h := &header{out: r.Out, errs: r.Err, label: s.file.Label(), where: s.file.Describes}
	r.Out, r.Err = headed{h.out, h.start}, headed{h.errs, h.start}
	defer func() {
		r.Out, r.Err = h.out, h.errs
		if h.started {
			fmt.Fprintln(h.out)
		}
	}()

	if !s.needed(r) {
		return
	}
	targets, ok := s.targets(r)
	if !ok {
		return
	}
	if len(targets) == 1 && targets[0].root != "" {
		s.root = targets[0].root
		h.at(shown(s.root))
	}
	if !s.ready(r) || s.busy(r) {
		return
	}
	if s.file.Stop.given() && !s.stopIdle(r) {
		return
	}
	for _, t := range targets {
		base := shown(t.root)
		if t.root != "" {
			h.at(base)
		}
		for _, v := range s.file.views() {
			part := s
			part.file, part.root, part.base, part.remove = v, t.root, base, v.Remove
			if t.remove != nil {
				part.remove = *t.remove
			}
			if !part.runPart(r) {
				return
			}
		}
	}
}

func shown(root string) string {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		return resolved
	}
	return root
}

func (s Rule) targets(r *report.Report) ([]target, bool) {
	switch {
	case s.file.Root.empty():
		return []target{{}}, true
	case s.file.Root.setting != "":
		return s.settingTargets(r)
	}
	root, chosen, ok := s.resolveChoice(r, s.file.Root)
	if !ok || !r.Directory(root) {
		return nil, false
	}
	return []target{{root: root, remove: chosen.remove}}, true
}

func (s Rule) settingTargets(r *report.Report) ([]target, bool) {
	key := s.file.Root.setting
	var targets []target
	for _, folder := range s.env.Dirs(key) {
		root, ok := s.allowed(r, folder)
		if !ok {
			continue
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			r.Fail("%s: not a folder; it is named by %s", folder, key)
			continue
		}
		if !slices.ContainsFunc(targets, func(t target) bool { return shown(t.root) == shown(root) }) {
			targets = append(targets, target{root: root})
		}
	}
	return targets, len(targets) > 0
}

func (s Rule) runPart(r *report.Report) bool {
	switch remove := s.remove; {
	case remove.Root:
		r.Remove(s.root)
	case s.file.Each.given():
		return s.removeInEach(r)
	case len(remove.Folders) > 0:
		s.removeFolders(r, s.olderThanTheNewest(r, s.folders(r)))
	case len(remove.Files) > 0:
		s.removeFiles(r)
	case remove.Untouched != nil:
		s.sweeper().run(r)
	case len(remove.Superseded) > 0:
		s.removeSuperseded(r)
	}
	if s.file.Each.given() {
		return true
	}
	for _, c := range s.remove.Commands {
		if !s.runCommand(r, c, s.root) {
			return false
		}
	}
	return true
}

func (s Rule) busy(r *report.Report) bool {
	pattern := s.file.BusyPattern()
	if pattern == "" || !r.Busy(s.env.Processes.Running, pattern) {
		return false
	}
	subject := s.root
	if subject == "" {
		subject = s.file.Name
	}
	r.Skip("%s: %s", subject, s.file.Busy.Says)
	return true
}

func (s Rule) stopIdle(r *report.Report) bool {
	stop := s.file.Stop
	times, ok := s.cpuTimes(r)
	if !ok {
		return false
	}
	if times == "" {
		return true
	}
	s.env.Processes.Sleep(stop.quiet)
	again, ok := s.cpuTimes(r)
	if !ok {
		return false
	}
	if again != times {
		r.Skip("%s: %s", s.root, stop.Says)
		return false
	}
	if r.DryRun {
		fmt.Fprintf(r.Out, "WOULD STOP    %s\n", stop.Names)
		return true
	}
	fmt.Fprintf(r.Out, "STOP          %s\n", stop.Names)
	if err := s.env.Processes.Terminate(stop.pattern()); err != nil {
		r.Fail("pkill -TERM -f '%s': %v", stop.pattern(), err)
		return false
	}
	for waited := time.Duration(0); ; waited += stopPoll {
		pids, err := s.env.Processes.Pids(stop.pattern())
		if err != nil {
			r.Fail("pgrep -f '%s': %v", stop.pattern(), err)
			return false
		}
		if len(pids) == 0 {
			return true
		}
		if waited >= stop.limit {
			r.Fail("%s: %s did not stop within %s", s.root, stop.Names, stop.limit)
			return false
		}
		s.env.Processes.Sleep(stopPoll)
	}
}

func (s Rule) cpuTimes(r *report.Report) (string, bool) {
	pattern := s.file.Stop.pattern()
	pids, err := s.env.Processes.Pids(pattern)
	if err != nil {
		r.Fail("pgrep -f '%s': %v", pattern, err)
		return "", false
	}
	times, err := s.env.Processes.CPUTimes(pids)
	if err != nil {
		r.Fail("ps of %s: %v", s.file.Stop.Names, err)
		return "", false
	}
	return times, true
}

func (s Rule) removeFiles(r *report.Report) {
	root, err := settings.Canonical(s.base)
	if err != nil {
		r.Fail("resolve %s: %v", s.base, err)
		return
	}
	limit := s.ageLimit()
	var found []string
	var scanErrors []error
	consider := func(path string, entry fs.DirEntry) {
		if !entry.Type().IsRegular() {
			return
		}
		info, err := entry.Info()
		if err != nil {
			scanErrors = append(scanErrors, err)
			return
		}
		if !limit.Recent(info.ModTime()) {
			found = append(found, path)
		}
	}
	if slices.ContainsFunc(s.remove.Files, func(pattern string) bool { return strings.HasPrefix(pattern, "**/") }) {
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			switch {
			case err != nil:
				scanErrors = append(scanErrors, err)
			case entry.IsDir() && path != root && slices.Contains(s.remove.Outside, entry.Name()):
				return fs.SkipDir
			case s.wanted(root, path):
				consider(path, entry)
			}
			return nil
		})
		if err != nil {
			scanErrors = append(scanErrors, err)
		}
	} else {
		for _, pattern := range s.remove.Files {
			matches, err := expandGlob(root, pattern)
			if err != nil {
				scanErrors = append(scanErrors, err)
			}
			for _, path := range matches {
				info, err := os.Lstat(path)
				if err != nil {
					scanErrors = append(scanErrors, err)
					continue
				}
				consider(path, fs.FileInfoToDirEntry(info))
			}
		}
	}
	if len(scanErrors) > 0 {
		r.Fail("scan %s: %v", s.base, errors.Join(scanErrors...))
	}
	slices.Sort(found)
	s.removeAll(r, slices.Compact(found), s.keeper(r, limit))
}

func (s Rule) removeAll(r *report.Report, paths []string, keep func(path string) bool) {
	if s.file.Keep.Tracked {
		var set candidates.Set
		for _, path := range paths {
			set.Add(r, path)
		}
		set.RemoveUnless(r, keep)
		return
	}
	for _, path := range paths {
		if !keep(path) {
			r.Remove(path)
		}
	}
}

func (s Rule) wanted(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(s.remove.Files, func(pattern string) bool {
		if anywhere, ok := strings.CutPrefix(pattern, "**/"); ok {
			matched, _ := filepath.Match(anywhere, filepath.Base(rel))
			return matched
		}
		matched, _ := filepath.Match(pattern, rel)
		return matched
	})
}

func (s Rule) keeper(r *report.Report, limit age.Limit) func(path string) bool {
	open := s.env.Lsof.Every(openAnswersPerListing)
	failed := false
	return func(path string) bool {
		if failed {
			return true
		}
		if alive := s.file.Keep.Alive; alive != nil && alive.writing(path) {
			r.Skip("%s: %s", path, alive.Says)
			return true
		}
		if s.file.Keep.Open.Anything {
			held, err := open.HoldsAny(path)
			if err != nil {
				r.Fail("lsof %s: %v", s.root, err)
				failed = true
				return true
			}
			if held {
				r.Skip("%s: a process still has it open", path)
				return true
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			r.Fail("stat %s: %v", path, err)
			return true
		}
		if !info.Mode().IsRegular() {
			r.Skip("%s: no longer a file since the scan", path)
			return true
		}
		if limit.Recent(info.ModTime()) {
			r.Skip("%s: written again since the scan", path)
			return true
		}
		return false
	}
}
