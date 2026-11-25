package rule

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

type EachWhen struct {
	Folders []string      `yaml:"folders"`
	Has     []string      `yaml:"has"`
	Value   *CommandValue `yaml:"value"`
	Busy    BusyWhen      `yaml:"busy"`
}

type CommandValue struct {
	Command []string `yaml:"command"`
	Is      string   `yaml:"is"`
	Says    string   `yaml:"says"`
}

func (e EachWhen) given() bool {
	return len(e.Folders) > 0 || len(e.Has) > 0 || e.Value != nil || e.Busy.Says != "" || len(e.Busy.Processes) > 0
}

func (f File) checkEach() error {
	e := f.Each
	if !e.given() {
		return nil
	}
	switch {
	case len(e.Folders) == 0:
		return errors.New("each: want folders, the patterns of the folders handled one by one")
	case len(f.Remove.Folders) == 0:
		return errors.New("each: want remove: folders, looked for in each of them")
	case e.Value != nil && (len(e.Value.Command) == 0 || e.Value.Says == ""):
		return errors.New("each: value: want command, whose output is the value, is and says")
	case (len(e.Busy.Processes) > 0) != (e.Busy.Says != ""):
		return errors.New("each: busy: want processes together with says")
	}
	for _, pattern := range append(slices.Clone(e.Folders), e.Has...) {
		if !inside(strings.TrimSuffix(pattern, "/")) {
			return fmt.Errorf("each: %q: want a pattern inside the root", pattern)
		}
	}
	for _, pattern := range e.Busy.Processes {
		if _, err := regexp.CompilePOSIX(strings.ReplaceAll(pattern, "{path}", "/x")); err != nil {
			return fmt.Errorf("each: busy: processes %q: %w", pattern, err)
		}
	}
	return nil
}

func (s Rule) removeInEach(r *report.Report) {
	e := s.file.Each
	var folders []string
	for _, pattern := range e.Folders {
		matches, err := filepath.Glob(filepath.Join(s.root, pattern))
		if err != nil {
			r.Fail("find %s: %v", s.root, err)
			continue
		}
		folders = append(folders, matches...)
	}
	slices.Sort(folders)
	for _, folder := range slices.Compact(folders) {
		if info, err := os.Lstat(folder); err != nil || !info.IsDir() || !s.holds(r, folder, e.Has) {
			continue
		}
		s.base = folder
		for _, found := range s.olderThanTheNewest(r, s.folders(r)) {
			if !s.eachAllows(r, folder) {
				break
			}
			s.removeFolders(r, []string{found})
		}
	}
}

func (s Rule) holds(r *report.Report, folder string, paths []string) bool {
	for _, p := range paths {
		full := filepath.Join(folder, strings.TrimSuffix(p, "/"))
		if strings.HasSuffix(p, "/") {
			if !r.Directory(full) {
				return false
			}
			continue
		}
		if _, err := os.Stat(full); err != nil {
			return false
		}
	}
	return true
}

func (s Rule) eachAllows(r *report.Report, folder string) bool {
	e := s.file.Each
	if v := e.Value; v != nil {
		args := make([]string, len(v.Command))
		for i, arg := range v.Command {
			args[i] = strings.ReplaceAll(arg, "{path}", folder)
		}
		result := s.env.Commands.Run(0, args[0], args[1:]...)
		if result.Err != nil {
			r.Fail("%s: %v: %s", strings.Join(args, " "), result.Err, result.Output())
			return false
		}
		if value := strings.TrimSpace(result.Stdout); value != v.Is {
			r.Skip("%s: %s", folder, strings.ReplaceAll(v.Says, "{value}", value))
			return false
		}
	}
	if len(e.Busy.Processes) > 0 {
		pattern := strings.ReplaceAll(strings.Join(e.Busy.Processes, "|"), "{path}", folder)
		if r.Busy(s.env.Processes.Running, pattern) {
			r.Skip("%s: %s", folder, e.Busy.Says)
			return false
		}
	}
	return true
}

func (s Rule) deepFolders(base, pattern string) ([]string, error) {
	root, err := filepath.EvalSymlinks(base)
	if err != nil {
		return nil, err
	}
	var found []string
	var failures []error
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			failures = append(failures, err)
			return nil
		}
		if !entry.IsDir() || current == root {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if matchDeep(strings.Split(pattern, "/"), strings.Split(rel, string(filepath.Separator))) {
			found = append(found, filepath.Join(base, rel))
			return fs.SkipDir
		}
		return nil
	})
	return found, errors.Join(append(failures, err)...)
}

func matchDeep(pattern, parts []string) bool {
	if len(pattern) == 0 {
		return len(parts) == 0
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(parts); skip++ {
			if matchDeep(pattern[1:], parts[skip:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	matched, _ := path.Match(pattern[0], parts[0])
	return matched && matchDeep(pattern[1:], parts[1:])
}
