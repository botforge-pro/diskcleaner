package rule

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

type Busy struct {
	Processes string
	Running   func(pattern string) (bool, error)
	Says      string
}

type Rule struct {
	Label   string
	Root    string
	Busy    Busy
	Targets []string
}

func (s Rule) Run(r *report.Report) {
	if !r.Directory(s.Root) {
		return
	}
	fmt.Fprintf(r.Out, "%-8s %s\n", s.Label, s.Root)
	defer fmt.Fprintln(r.Out)

	if s.Busy.Processes != "" && r.Busy(s.Busy.Running, s.Busy.Processes) {
		r.Skip("%s: %s", s.Root, s.Busy.Says)
		return
	}
	if len(s.Targets) == 0 {
		r.Remove(s.Root)
		return
	}
	for _, folder := range s.folders(r) {
		r.Remove(folder)
	}
}

func (s Rule) folders(r *report.Report) []string {
	var matches []string
	for _, target := range s.Targets {
		found, err := filepath.Glob(filepath.Join(s.Root, target))
		if err != nil {
			r.Fail("find %s: %v", s.Root, err)
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
