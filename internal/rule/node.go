package rule

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Node struct {
	Root     *Value    `yaml:"root"`
	First    []Node    `yaml:"first"`
	Let      Bindings  `yaml:"let"`
	When     []Cond    `yaml:"when"`
	Unless   []Cond    `yaml:"unless"`
	Stop     *Stop     `yaml:"stop"`
	Remove   *Remove   `yaml:"remove"`
	Rank     *Rank     `yaml:"rank"`
	Run      []string  `yaml:"run"`
	DryRun   []string  `yaml:"dry-run"`
	Timeout  Amount    `yaml:"timeout"`
	Estimate *Estimate `yaml:"estimate"`
	Size     *Value    `yaml:"size"`
	Shows    string    `yaml:"shows"`
	Keep     *Keep     `yaml:"keep"`
	Do       []Node    `yaml:"do"`
	shows    *regexp.Regexp
}

type Binding struct {
	Name  string
	Value Value
}

type Bindings []Binding

func (b *Bindings) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: let: want names and their values, like {apps: /Applications/*.app}", node.Line)
	}
	for i := 0; i < len(node.Content); i += 2 {
		var v Value
		if err := v.UnmarshalYAML(node.Content[i+1]); err != nil {
			return err
		}
		*b = append(*b, Binding{Name: node.Content[i].Value, Value: v})
	}
	return nil
}

type Remove struct {
	Itself  bool
	Entries []Entry
}

func (m *Remove) UnmarshalYAML(node *yaml.Node) error {
	switch {
	case node.Kind == yaml.ScalarNode && node.Value == ".":
		m.Itself = true
		return nil
	case node.Kind == yaml.SequenceNode:
		for _, item := range node.Content {
			var e Entry
			if err := strict(item, &e); err != nil {
				return fmt.Errorf("remove: %w", err)
			}
			e.line = item.Line
			m.Entries = append(m.Entries, e)
		}
		return nil
	}
	return fmt.Errorf("line %d: remove: want . for the node's folder itself, or a list of entries like - folders: '*'", node.Line)
}

type Kind int

const (
	Skip Kind = iota
	Folders
	Files
	Empty
)

var kindNames = map[Kind]string{Skip: "skip", Folders: "folders", Files: "files", Empty: "empty"}

type Entry struct {
	SkipGlob    string `yaml:"skip"`
	FoldersGlob string `yaml:"folders"`
	FilesGlob   string `yaml:"files"`
	EmptyGlob   string `yaml:"empty"`
	Match       string `yaml:"match"`
	When        []Cond `yaml:"when"`
	Unless      []Cond `yaml:"unless"`
	Says        string `yaml:"says"`
	kind        Kind
	parts       []string
	folderOnly  bool
	name        *regexp.Regexp
	line        int
}

type Rank struct {
	Per   *Value `yaml:"per"`
	Order *Value `yaml:"order"`
	Spare Spare  `yaml:"spare"`
}

type Spare struct {
	Newest Amount `yaml:"newest"`
	In     *Value `yaml:"in"`
}

type Keep struct {
	Age     *Age     `yaml:"age"`
	Open    OpenKeep `yaml:"open"`
	Tracked bool     `yaml:"tracked"`
}

type Age struct {
	Of     Clock  `yaml:"of"`
	Within Amount `yaml:"within"`
}

type Clock struct {
	Name  string
	Value *Value
}

const (
	mtime       = "mtime"
	newestWrite = "newest-write"
	anyTime     = "any-time"
)

var clocks = []string{mtime, newestWrite, anyTime}

func (c *Clock) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		c.Name = node.Value
		return nil
	}
	c.Value = &Value{}
	return c.Value.UnmarshalYAML(node)
}

type OpenKeep struct {
	Anything bool
	Inside   []string
}

func (o *OpenKeep) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		return node.Decode(&o.Anything)
	}
	return node.Decode(&o.Inside)
}

type Stop struct {
	Running []Proc `yaml:"running"`
	IdleFor Amount `yaml:"idle-for"`
	Timeout Amount `yaml:"timeout"`
	Names   string `yaml:"names"`
	Says    string `yaml:"says"`
}

type Estimate struct {
	Run     []string `yaml:"run"`
	Timeout Amount   `yaml:"timeout"`
	Match   string   `yaml:"match"`
	Units   string   `yaml:"units"`
	match   *regexp.Regexp
}

type Proc struct {
	Pattern string
	Group   string
}

func (p *Proc) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		p.Pattern = node.Value
		return nil
	}
	var g struct {
		Group string `yaml:"group"`
	}
	if err := strict(node, &g); err != nil || g.Group == "" {
		return fmt.Errorf("line %d: want a process pattern or {group: name}", node.Line)
	}
	p.Group = g.Group
	return nil
}

type Amount string

var placeholder = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_-]*)\}`)

func (a Amount) setting() (string, bool) {
	match := placeholder.FindStringSubmatch(string(a))
	if match == nil || match[0] != string(a) {
		return "", false
	}
	return match[1], true
}

type checker struct {
	file  *File
	used  map[string]bool
	names map[string]bool
}

func (c checker) scoped() checker {
	names := make(map[string]bool, len(c.names))
	for name := range c.names {
		names[name] = true
	}
	return checker{file: c.file, used: c.used, names: names}
}

func (c checker) node(n *Node, depth int) error {
	if n.Root != nil {
		if err := c.value(*n.Root, "root"); err != nil {
			return err
		}
	}
	c = c.scoped()
	for _, b := range n.Let {
		if !stepName.MatchString(b.Name) || c.names[b.Name] {
			return fmt.Errorf("let: %s: want a new name of lowercase words joined by -", b.Name)
		}
		if err := c.value(b.Value, "let: "+b.Name); err != nil {
			return err
		}
		c.names[b.Name] = true
	}
	for _, cond := range slices.Concat(n.When, n.Unless) {
		if err := c.cond(cond); err != nil {
			return err
		}
	}
	if err := c.stop(n.Stop); err != nil {
		return err
	}
	if err := c.action(n); err != nil {
		return err
	}
	if err := c.remove(n); err != nil {
		return err
	}
	if len(n.First) > 0 && (n.Root != nil || len(n.Do) > 0 || n.Remove != nil || len(n.Run) > 0) {
		return errors.New("first: want it alone, the chosen child being the node")
	}
	for i := range n.First {
		if n.First[i].Root == nil {
			return fmt.Errorf("first: %d: want root, by which the child is chosen", i+1)
		}
		if err := c.node(&n.First[i], depth+1); err != nil {
			return fmt.Errorf("first: %d: %w", i+1, err)
		}
	}
	for i := range n.Do {
		if err := c.node(&n.Do[i], depth+1); err != nil {
			return fmt.Errorf("do: %d: %w", i+1, err)
		}
	}
	if depth == 0 && n.Remove == nil && len(n.Run) == 0 && len(n.Do) == 0 && len(n.First) == 0 {
		return errors.New("want remove, run, do or first: a step that removes nothing")
	}
	return nil
}

func (c checker) remove(n *Node) error {
	if n.Remove == nil {
		if n.Rank != nil || n.Keep != nil {
			return errors.New("rank and keep: want remove, whose candidates they judge")
		}
		return nil
	}
	if len(n.Run) > 0 {
		return errors.New("remove and run: want one of them in a node")
	}
	if n.Remove.Itself && n.Rank != nil {
		return errors.New("rank: want remove entries, the candidates it ranks")
	}
	for i := range n.Remove.Entries {
		if err := c.entry(&n.Remove.Entries[i]); err != nil {
			return fmt.Errorf("remove: line %d: %w", n.Remove.Entries[i].line, err)
		}
	}
	if !n.Remove.Itself && !slices.ContainsFunc(n.Remove.Entries, func(e Entry) bool { return e.kind != Skip }) {
		return errors.New("remove: want an entry that takes something: folders, files or empty")
	}
	if err := c.rank(n.Rank); err != nil {
		return err
	}
	return c.keep(n.Keep)
}

func (c checker) entry(e *Entry) error {
	set := 0
	for kind, glob := range map[Kind]string{Skip: e.SkipGlob, Folders: e.FoldersGlob, Files: e.FilesGlob, Empty: e.EmptyGlob} {
		if glob != "" {
			set++
			e.kind = kind
		}
	}
	if set != 1 {
		return errors.New("want one of skip, folders, files and empty, with a pattern")
	}
	glob := map[Kind]string{Skip: e.SkipGlob, Folders: e.FoldersGlob, Files: e.FilesGlob, Empty: e.EmptyGlob}[e.kind]
	e.folderOnly = strings.HasSuffix(glob, "/")
	glob = strings.TrimSuffix(glob, "/")
	e.parts = strings.Split(glob, "/")
	for _, part := range e.parts {
		if _, err := filepath.Match(part, ""); err != nil || part == "" || part == "." || part == ".." || part != "**" && strings.Contains(part, "**") {
			return fmt.Errorf("%s %q: want a pattern inside the node's folder, with ** only as a whole part, like data/**/Caches", kindNames[e.kind], glob)
		}
	}
	if filepath.IsAbs(glob) {
		return fmt.Errorf("%s %q: want a pattern inside the node's folder", kindNames[e.kind], glob)
	}
	if e.Match != "" {
		re, err := regexp.Compile(e.Match)
		if err != nil {
			return fmt.Errorf("match %q: %w", e.Match, err)
		}
		e.name = re
	}
	for _, cond := range slices.Concat(e.When, e.Unless) {
		if err := c.condAt(cond, false); err != nil {
			return err
		}
	}
	if e.Says != "" && e.kind != Skip && len(e.When) == 0 && len(e.Unless) == 0 {
		return errors.New("says: want when or unless, whose failing it explains")
	}
	return nil
}

func (c checker) rank(r *Rank) error {
	if r == nil {
		return nil
	}
	for name, v := range map[string]*Value{"per": r.Per, "order": r.Order, "spare: in": r.Spare.In} {
		if v != nil {
			if err := c.value(*v, "rank: "+name); err != nil {
				return err
			}
		}
	}
	if (r.Spare.Newest == "") == (r.Spare.In == nil) {
		return errors.New("rank: spare: want one of newest: count and in: a set of names")
	}
	if r.Spare.Newest != "" {
		if r.Order == nil {
			return errors.New("rank: spare: newest: want order, which says what is newest")
		}
		return c.amount(r.Spare.Newest, "rank: spare: newest", "count")
	}
	return nil
}

func (c checker) keep(k *Keep) error {
	if k == nil {
		return nil
	}
	if a := k.Age; a != nil {
		switch {
		case a.Of.Value != nil:
			if err := c.value(*a.Of.Value, "keep: age: of"); err != nil {
				return err
			}
		case !slices.Contains(clocks, a.Of.Name):
			return fmt.Errorf("keep: age: of %q: want one of %s, or a value read at the candidate", a.Of.Name, strings.Join(clocks, ", "))
		}
		if err := c.amount(a.Within, "keep: age: within", "duration"); err != nil {
			return err
		}
	}
	for _, pattern := range k.Open.Inside {
		if _, err := filepath.Match(pattern, ""); err != nil || pattern == "" || filepath.IsAbs(pattern) || slices.Contains(strings.Split(pattern, "/"), "..") {
			return fmt.Errorf("keep: open: %q: want a pattern inside the candidate, like */Contents/MacOS/*", pattern)
		}
	}
	return nil
}

func (c checker) stop(s *Stop) error {
	if s == nil {
		return nil
	}
	if len(s.Running) == 0 || s.Names == "" || s.Says == "" {
		return errors.New("stop: want running, names and says")
	}
	if err := c.processes(s.Running, "stop: running"); err != nil {
		return err
	}
	if err := c.amount(s.IdleFor, "stop: idle-for", "duration"); err != nil {
		return err
	}
	return c.amount(s.Timeout, "stop: timeout", "timeout")
}

func (c checker) action(n *Node) error {
	if len(n.Run) == 0 {
		if len(n.DryRun) > 0 || n.Timeout != "" || n.Estimate != nil || n.Size != nil || n.Shows != "" {
			return errors.New("dry-run, timeout, estimate, size and shows: want run, the command they belong to")
		}
		return nil
	}
	if len(n.DryRun) == 0 && n.Estimate == nil && n.Size == nil {
		return fmt.Errorf("run %s: want dry-run, estimate or size, so that a dry run can tell what it would free", strings.Join(n.Run, " "))
	}
	if err := c.words(n.Run, "run"); err != nil {
		return err
	}
	if n.Timeout != "" {
		if err := c.amount(n.Timeout, "timeout", "timeout"); err != nil {
			return err
		}
	}
	if n.Size != nil {
		if err := c.value(*n.Size, "size"); err != nil {
			return err
		}
	}
	if n.Shows != "" {
		re, err := regexp.Compile(n.Shows)
		if err != nil {
			return fmt.Errorf("shows %q: %w", n.Shows, err)
		}
		n.shows = re
	}
	if e := n.Estimate; e != nil {
		re, err := regexp.Compile(e.Match)
		switch {
		case err != nil:
			return fmt.Errorf("estimate: match %q: %w", e.Match, err)
		case re.NumSubexp() < 1:
			return fmt.Errorf("estimate: match %q: want a group, the size", e.Match)
		case e.Units != "" && e.Units != "binary":
			return fmt.Errorf("estimate: units %q: want binary, for sizes printed in KB, MB or GB that mean KiB, MiB or GiB", e.Units)
		case len(e.Run) == 0 && e.Timeout != "":
			return errors.New("estimate: timeout: want run, the command it limits")
		case len(e.Run) == 0 && len(n.DryRun) == 0:
			return errors.New("estimate: want run, or dry-run on the command, whose output it reads")
		}
		e.match = re
		if err := c.words(e.Run, "estimate: run"); err != nil {
			return err
		}
		if e.Timeout != "" {
			if err := c.amount(e.Timeout, "estimate: timeout", "timeout"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c checker) amount(a Amount, where, kind string) error {
	if key, ok := a.setting(); ok {
		got, found := c.file.setting(key)
		if !found || got != kinds[kind] && !(kind == "timeout" && got == kinds["duration"]) {
			return fmt.Errorf("%s: {%s}: want a %s setting of this step or a global one", where, key, kind)
		}
		c.used[key] = true
		return nil
	}
	if _, err := literal(a, kind); err != nil {
		return fmt.Errorf("%s %q: want a %s like %s, or {setting}: %w", where, a, kind, map[string]string{"count": "2", "duration": "1h", "timeout": "30s"}[kind], err)
	}
	return nil
}

func (c checker) words(words []string, where string) error {
	for _, word := range words {
		if err := c.text(word, where, false); err != nil {
			return err
		}
	}
	return nil
}

func (c checker) text(text, where string, whole bool) error {
	for _, found := range placeholder.FindAllStringSubmatch(text, -1) {
		name := found[1]
		if c.names[name] {
			continue
		}
		kind, ok := c.file.setting(name)
		if !ok {
			return fmt.Errorf("%s: {%s}: want path, name, uid, a let above or a setting", where, name)
		}
		if kind != kinds["folders"] {
			return fmt.Errorf("%s: {%s}: a %s setting belongs where a duration, timeout or count is wanted", where, name, settingKindName(kind))
		}
		if !whole {
			return fmt.Errorf("%s: {%s}: a set of folders belongs in a path, which it makes one per folder", where, name)
		}
		c.used[name] = true
	}
	return nil
}

func settingKindName(kind any) string {
	for name, k := range kinds {
		if k == kind {
			return name
		}
	}
	return "other"
}

func (c checker) processes(procs []Proc, where string) error {
	for _, p := range procs {
		if p.Group != "" {
			if _, ok := c.file.groups[p.Group]; !ok {
				return fmt.Errorf("%s: group %q: want one of the groups in %s", where, p.Group, GroupsFile)
			}
			continue
		}
		if err := c.text(p.Pattern, where, false); err != nil {
			return err
		}
		if _, err := regexp.CompilePOSIX(placeholder.ReplaceAllString(p.Pattern, "x")); err != nil {
			return fmt.Errorf("%s %q: %w", where, p.Pattern, err)
		}
	}
	return nil
}

func strict(node *yaml.Node, into any) error {
	data, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	return decoder.Decode(into)
}
