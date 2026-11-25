package rule

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

type Removal struct {
	Root       bool
	Folders    []FolderPattern
	Files      []string
	Others     string
	Newest     *Newest
	Untouched  *Untouched
	Superseded []Superseded
	Outside    []string
	Commands   []Command
	given      bool
}

type Newest struct {
	Keep   string `yaml:"keep"`
	OfEach Value  `yaml:"of-each"`
	By     Value  `yaml:"by"`
}

type FolderPattern struct {
	Glob      string   `yaml:"glob"`
	Match     string   `yaml:"match"`
	Has       []string `yaml:"has"`
	Beside    []string `yaml:"beside"`
	BesideAny []string `yaml:"beside-any"`
	OrAbove   []Group  `yaml:"or-above"`
	Says      string   `yaml:"says"`
	name      *regexp.Regexp
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
	Outside []string        `yaml:"outside"`
}

type alone struct {
	Untouched  *Untouched   `yaml:"untouched"`
	Superseded []Superseded `yaml:"superseded"`
}

func (m *Removal) UnmarshalYAML(node *yaml.Node) error {
	m.given = true
	want := fmt.Errorf("line %d: remove: want root, one of folders: [patterns] and files: [patterns], untouched: {for, except}, superseded: [{read, folders, by}], or run: [command] or a list of these", node.Line)
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
	case slices.ContainsFunc(node.Content, func(key *yaml.Node) bool { return key.Value == "untouched" || key.Value == "superseded" }):
		var a alone
		if err := strict(node, &a); err != nil || (a.Untouched == nil) == (a.Superseded == nil) {
			return want
		}
		m.Untouched, m.Superseded = a.Untouched, a.Superseded
		return nil
	}
	var p patterns
	if err := strict(node, &p); err != nil || (p.Folders == nil) == (p.Files == nil) {
		return want
	}
	m.Folders, m.Files, m.Others, m.Newest, m.Outside = p.Folders, p.Files, p.Others, p.Newest, p.Outside
	return nil
}

func (f File) checkRemoval(m Removal) error {
	switch {
	case !m.given:
		return errors.New("remove: want root, or one of folders: [patterns] and files: [patterns] inside the root")
	case !m.Root && len(m.Folders) == 0 && len(m.Files) == 0 && len(m.Commands) == 0 && m.Untouched == nil && len(m.Superseded) == 0:
		return errors.New("remove: want at least one pattern")
	case m.Untouched != nil && (f.Keep.given() || f.Each.given()):
		return errors.New("remove: untouched keeps what is open, a socket or on another volume by itself, so want no keep or each beside it")
	case m.Untouched != nil:
		return m.Untouched.check()
	case m.Others != "" && len(m.Folders) == 0:
		return errors.New("remove: others: want folders, whose others it speaks of")
	case m.Newest != nil && len(m.Folders) == 0:
		return errors.New("remove: newest: want folders, the newest of which it keeps")
	case slices.ContainsFunc(m.Outside, func(name string) bool { return name == "" || strings.ContainsAny(name, "/*?[") }):
		return errors.New("remove: outside: want names of folders that a search for ** does not enter, like node_modules")
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
		for _, path := range slices.Concat(p.Has, p.Beside, p.BesideAny) {
			if !inside(strings.TrimSuffix(path, "/")) {
				return fmt.Errorf("remove: folders: has or beside %q: want a path inside the folder or its parent", path)
			}
		}
		if len(p.OrAbove) > 0 && len(p.BesideAny) == 0 {
			return fmt.Errorf("remove: folders: %q: or-above: want beside-any, which it widens to the folders above", p.Glob)
		}
		for _, g := range p.OrAbove {
			if len(g.Any) == 0 {
				return fmt.Errorf("remove: folders: %q: or-above: want any: [marks] in each group", p.Glob)
			}
			for _, m := range g.Any {
				if err := m.check(); err != nil {
					return fmt.Errorf("remove: folders: %q: or-above: %w", p.Glob, err)
				}
			}
		}
		if p.Says != "" && len(p.Has) == 0 && len(p.Beside) == 0 && len(p.BesideAny) == 0 {
			return fmt.Errorf("remove: folders: %q: says: want has or beside, whose absence it explains", p.Glob)
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
		found, err := s.matchFolders(r, p)
		if err != nil {
			r.Fail("find %s: %v", s.base, err)
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
		entries, err := os.ReadDir(s.base)
		if err != nil {
			r.Fail("find %s: %v", s.base, err)
		}
		for _, entry := range entries {
			path := filepath.Join(s.base, entry.Name())
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

func (s Rule) matchFolders(r *report.Report, p FolderPattern) ([]string, error) {
	var found []string
	var walkErr error
	switch {
	case strings.Contains(p.Glob, "**"):
		found, walkErr = s.deepFolders(s.base, p.Glob, s.remove.Outside)
	case p.Glob != "":
		found, walkErr = expandGlob(s.base, p.Glob)
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
		missing := !s.qualifies(p, folder)
		if missing && p.Says != "" {
			r.Skip("%s: %s", folder, p.Says)
		}
		return missing
	}), walkErr
}

func (s Rule) qualifies(p FolderPattern, folder string) bool {
	parent := filepath.Dir(folder)
	if slices.ContainsFunc(p.Has, func(path string) bool { return !present(folder, path) }) ||
		slices.ContainsFunc(p.Beside, func(path string) bool { return !present(parent, path) }) {
		return false
	}
	if len(p.BesideAny) == 0 || slices.ContainsFunc(p.BesideAny, func(path string) bool { return present(parent, path) }) {
		return true
	}
	for above := parent; len(p.OrAbove) > 0; above = filepath.Dir(above) {
		if !slices.ContainsFunc(p.OrAbove, func(g Group) bool { return !g.in(above) }) {
			return true
		}
		if above == s.base || above == filepath.Dir(above) {
			return false
		}
	}
	return false
}

type Group struct {
	Any []Mark `yaml:"any"`
}

func (g Group) in(folder string) bool {
	return slices.ContainsFunc(g.Any, func(m Mark) bool { return m.in(folder) })
}

type Mark struct {
	Path string
	File string `yaml:"file"`
	Key  string `yaml:"key"`
}

func (m *Mark) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		m.Path = node.Value
		return nil
	}
	type plain Mark
	return strict(node, (*plain)(m))
}

func (m Mark) check() error {
	switch {
	case m.Path != "" && inside(strings.TrimSuffix(m.Path, "/")):
		return nil
	case m.Path == "" && inside(m.File) && filepath.Ext(m.File) == ".json" && m.Key != "":
		return nil
	}
	return fmt.Errorf("%q: want a path inside the folder, or {file: a .json inside it, key: one it has}", m.Path+m.File)
}

func (m Mark) in(folder string) bool {
	if m.Path != "" {
		return present(folder, m.Path)
	}
	values, found, err := readValues(filepath.Join(folder, m.File))
	if err != nil || !found {
		return false
	}
	_, has := values[m.Key]
	return has
}

func expandGlob(base, pattern string) ([]string, error) {
	paths := []string{base}
	var failures []error
	unreadable := func(err error) {
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			failures = append(failures, err)
		}
	}
	for _, part := range strings.Split(pattern, "/") {
		var next []string
		for _, dir := range paths {
			if !strings.ContainsAny(part, `*?[\`) {
				candidate := filepath.Join(dir, part)
				if _, err := os.Lstat(candidate); err != nil {
					unreadable(err)
				} else {
					next = append(next, candidate)
				}
				continue
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				unreadable(err)
				continue
			}
			for _, entry := range entries {
				if matched, _ := filepath.Match(part, entry.Name()); matched {
					next = append(next, filepath.Join(dir, entry.Name()))
				}
			}
		}
		paths = next
	}
	return paths, errors.Join(failures...)
}

func present(folder, path string) bool {
	wantFolder := strings.HasSuffix(path, "/")
	matches, err := filepath.Glob(filepath.Join(folder, strings.TrimSuffix(path, "/")))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(matches, func(match string) bool {
		info, err := os.Stat(match)
		return err == nil && (wantFolder && info.IsDir() || !wantFolder && info.Mode().IsRegular())
	})
}
