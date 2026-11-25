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
	Getenv    func(string) string
	Processes Processes
	Commands  Commands
	Duration  func(key string) time.Duration
	Now       func() time.Time
	Lsof      inuse.Lsof
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
	file File
	root string
	env  Environment
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

func (s Rule) Run(r *report.Report) {
	out, errs := r.Out, r.Err
	started := false
	where := s.file.Describes
	start := func() {
		if !started {
			started = true
			fmt.Fprintf(out, "%-8s %s\n", s.file.Label(), where)
		}
	}
	r.Out, r.Err = headed{out, start}, headed{errs, start}
	defer func() {
		r.Out, r.Err = out, errs
		if started {
			fmt.Fprintln(out)
		}
	}()

	if !s.needed(r) {
		return
	}
	if !s.file.Root.empty() {
		root, ok := s.resolve(r, s.file.Root)
		if !ok || !r.Directory(root) {
			return
		}
		s.root = root
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			where = resolved
		} else {
			where = root
		}
	}
	if !s.ready(r) || s.busy(r) {
		return
	}
	if s.file.Stop.given() && !s.stopIdle(r) {
		return
	}
	switch remove := s.file.Remove; {
	case remove.Root:
		r.Remove(s.root)
	case len(remove.Folders) > 0:
		for _, folder := range s.folders(r) {
			r.Remove(folder)
		}
	case len(remove.Files) > 0:
		s.removeFiles(r)
	}
	for _, c := range s.file.Remove.Commands {
		if !s.runCommand(r, c, s.root) {
			return
		}
	}
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

func (s Rule) age() age.Limit {
	limit := age.Limit{Now: s.env.Now}
	if key := s.file.Keep.WrittenWithin; key != "" {
		limit.Min = s.env.Duration(key)
	}
	return limit
}

func (s Rule) folders(r *report.Report) []string {
	var matches []string
	for _, pattern := range s.file.Remove.Folders {
		found, err := filepath.Glob(filepath.Join(s.root, pattern))
		if err != nil {
			r.Fail("find %s: %v", s.root, err)
			continue
		}
		matches = append(matches, found...)
	}
	slices.Sort(matches)
	var folders []string
	for _, match := range slices.Compact(matches) {
		info, err := os.Lstat(match)
		if err != nil {
			r.Fail("stat %s: %v", match, err)
			continue
		}
		if info.IsDir() {
			folders = append(folders, match)
		}
	}
	return folders
}

func (s Rule) removeFiles(r *report.Report) {
	root, err := settings.Canonical(s.root)
	if err != nil {
		r.Fail("resolve %s: %v", s.root, err)
		return
	}
	limit := s.age()
	var set candidates.Set
	var scanErrors []error
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			scanErrors = append(scanErrors, err)
			return nil
		}
		if !entry.Type().IsRegular() || !s.wanted(root, path) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			scanErrors = append(scanErrors, err)
			return nil
		}
		if !limit.Recent(info.ModTime()) {
			set.Add(r, path)
		}
		return nil
	})
	if err != nil {
		scanErrors = append(scanErrors, err)
	}
	if len(scanErrors) > 0 {
		r.Fail("scan %s: %v", s.root, errors.Join(scanErrors...))
	}
	set.RemoveUnless(r, s.keeper(r, limit))
}

func (s Rule) wanted(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(s.file.Remove.Files, func(pattern string) bool {
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
		info, err := os.Lstat(path)
		if err != nil {
			r.Fail("stat %s: %v", path, err)
			return true
		}
		if limit.Recent(info.ModTime()) {
			r.Skip("%s: written again since the scan", path)
			return true
		}
		return false
	}
}
