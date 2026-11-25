package rule

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

type Removal struct {
	Root     bool
	Folders  []FolderPattern
	Files    []string
	Others   string
	Newest   *Newest
	Commands []Command
	given    bool
}

type Newest struct {
	Keep   string `yaml:"keep"`
	OfEach Value  `yaml:"of-each"`
	By     Value  `yaml:"by"`
}

type FolderPattern struct {
	Glob  string
	Match string   `yaml:"match"`
	Has   []string `yaml:"has"`
	name  *regexp.Regexp
}

func (p *FolderPattern) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		p.Glob = node.Value
		return nil
	}
	type plain FolderPattern
	return strict(node, (*plain)(p))
}

type patterns struct {
	Folders []FolderPattern `yaml:"folders"`
	Files   []string        `yaml:"files"`
	Others  string          `yaml:"others"`
	Newest  *Newest         `yaml:"newest"`
}

func (m *Removal) UnmarshalYAML(node *yaml.Node) error {
	m.given = true
	want := fmt.Errorf("line %d: remove: want root, one of folders: [patterns] and files: [patterns], or run: [command] or a list of these", node.Line)
	switch {
	case node.Kind == yaml.ScalarNode && node.Value == "root":
		m.Root = true
		return nil
	case node.Kind == yaml.SequenceNode:
		for _, item := range node.Content {
			var c Command
			if err := strict(item, &c); err != nil {
				return fmt.Errorf("remove: %w", err)
			}
			m.Commands = append(m.Commands, c)
		}
		return nil
	case node.Kind != yaml.MappingNode:
		return want
	case slices.ContainsFunc(node.Content, func(key *yaml.Node) bool { return key.Value == "run" }):
		var c Command
		if err := strict(node, &c); err != nil {
			return fmt.Errorf("remove: %w", err)
		}
		m.Commands = []Command{c}
		return nil
	}
	var p patterns
	if err := strict(node, &p); err != nil || (p.Folders == nil) == (p.Files == nil) {
		return want
	}
	m.Folders, m.Files, m.Others, m.Newest = p.Folders, p.Files, p.Others, p.Newest
	return nil
}

func (f File) checkRemoval(m Removal) error {
	switch {
	case !m.given:
		return errors.New("remove: want root, or one of folders: [patterns] and files: [patterns] inside the root")
	case !m.Root && len(m.Folders) == 0 && len(m.Files) == 0 && len(m.Commands) == 0:
		return errors.New("remove: want at least one pattern")
	case m.Others != "" && len(m.Folders) == 0:
		return errors.New("remove: others: want folders, whose others it speaks of")
	case m.Newest != nil && len(m.Folders) == 0:
		return errors.New("remove: newest: want folders, the newest of which it keeps")
	}
	if n := m.Newest; n != nil {
		if !hasSetting(f.Settings, n.Keep, "count") {
			return fmt.Errorf("remove: newest: keep %q: want the key of one of this step's count settings", n.Keep)
		}
		for name, v := range map[string]Value{"of-each": n.OfEach, "by": n.By} {
			if err := v.check(); err != nil {
				return fmt.Errorf("remove: newest: %s: %w", name, err)
			}
		}
	}
	for _, c := range m.Commands {
		if err := f.checkCommand(c); err != nil {
			return err
		}
	}
	for i := range m.Folders {
		p := &m.Folders[i]
		switch {
		case p.Glob != "" && p.Match != "":
			return fmt.Errorf("remove: folders: %q: want a pattern or match, not both", p.Glob)
		case p.Glob == "" && p.Match == "":
			return errors.New("remove: folders: want a pattern, or {match: regex, has: [paths]}")
		case p.Glob != "" && !inside(strings.ReplaceAll(p.Glob, "**", "*")):
			return fmt.Errorf("remove: folders: %q: want a pattern inside the root, like *.avd/snapshots/*", p.Glob)
		case strings.Contains(p.Glob, "**") && slices.ContainsFunc(strings.Split(p.Glob, "/"), func(part string) bool { return part != "**" && strings.Contains(part, "**") }):
			return fmt.Errorf("remove: folders: %q: want ** as a whole part of the path, like data/**/Caches", p.Glob)
		case m.Others != "" && strings.Contains(p.Glob, "/"):
			return fmt.Errorf("remove: folders: %q: others speaks of the folders right in the root, so want patterns of their names", p.Glob)
		}
		if p.Match != "" {
			re, err := regexp.Compile(p.Match)
			if err != nil {
				return fmt.Errorf("remove: folders: match %q: %w", p.Match, err)
			}
			p.name = re
		}
		for _, path := range p.Has {
			if !inside(path) {
				return fmt.Errorf("remove: folders: has %q: want a path inside the folder", path)
			}
		}
	}
	for _, pattern := range m.Files {
		rest, anywhere := strings.CutPrefix(pattern, "**/")
		if !inside(rest) || strings.Contains(rest, "**") || anywhere && strings.Contains(rest, "/") {
			return fmt.Errorf("remove: files: %q: want a pattern inside the root, like logs/*.log, or **/ and a name pattern, like **/*.jsonl", pattern)
		}
	}
	return nil
}

func (s Rule) folders(r *report.Report) []string {
	var matches []string
	for _, p := range s.remove.Folders {
		found, err := s.matchFolders(p)
		if err != nil {
			r.Fail("find %s: %v", s.root, err)
			continue
		}
		matches = append(matches, found...)
	}
	slices.Sort(matches)
	matches = slices.Compact(matches)
	var folders []string
	for _, match := range matches {
		info, err := os.Lstat(match)
		if err != nil {
			r.Fail("stat %s: %v", match, err)
			continue
		}
		if info.IsDir() {
			folders = append(folders, match)
		}
	}
	if s.remove.Others != "" {
		entries, err := os.ReadDir(s.root)
		if err != nil {
			r.Fail("find %s: %v", s.root, err)
		}
		for _, entry := range entries {
			path := filepath.Join(s.root, entry.Name())
			if entry.IsDir() && !slices.Contains(folders, path) {
				r.Skip("%s: %s", path, s.remove.Others)
			}
		}
	}
	return folders
}

func (s Rule) olderThanTheNewest(r *report.Report, folders []string) []string {
	n := s.remove.Newest
	if n == nil {
		return folders
	}
	type made struct {
		path string
		when time.Time
	}
	byGroup := map[string][]made{}
	for _, folder := range folders {
		group, err := n.OfEach.read(folder)
		var when time.Time
		if err == nil {
			when, err = n.By.time(folder)
		}
		if err != nil {
			r.Skip("%s: %s does not say what it is and when it was made", folder, n.OfEach.File)
			r.Fail("read %s: %v", filepath.Join(folder, n.OfEach.File), err)
			continue
		}
		byGroup[group] = append(byGroup[group], made{folder, when})
	}
	var older []string
	for _, group := range slices.Sorted(maps.Keys(byGroup)) {
		all := byGroup[group]
		slices.SortFunc(all, func(a, b made) int { return b.when.Compare(a.when) })
		for _, m := range all[min(s.env.Count(s.file.Name+"."+n.Keep), len(all)):] {
			older = append(older, m.path)
		}
	}
	return older
}

func (s Rule) matchFolders(p FolderPattern) ([]string, error) {
	var found []string
	switch {
	case strings.Contains(p.Glob, "**"):
		matches, err := s.deepFolders(s.base, p.Glob)
		if err != nil {
			return nil, err
		}
		found = matches
	case p.Glob != "":
		matches, err := filepath.Glob(filepath.Join(s.base, p.Glob))
		if err != nil {
			return nil, err
		}
		found = matches
	default:
		entries, err := os.ReadDir(s.base)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if p.name.MatchString(entry.Name()) {
				found = append(found, filepath.Join(s.base, entry.Name()))
			}
		}
	}
	return slices.DeleteFunc(found, func(folder string) bool {
		return slices.ContainsFunc(p.Has, func(path string) bool {
			_, err := os.Stat(filepath.Join(folder, path))
			return err != nil
		})
	}), nil
}
