package rule

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/botforge-pro/diskcleaner/internal/config"
)

var variable = regexp.MustCompile(`^\$([A-Z_][A-Z0-9_]*)(/.*)?$`)

type Value struct {
	Text string
	List []Value
	Run  *RunValue
	Read *ReadValue
	Each *EachValue
	Name *NameValue
	line int
}

type RunValue struct {
	Run      []string `yaml:"run"`
	Timeout  Amount   `yaml:"timeout"`
	Match    string   `yaml:"match"`
	Lines    bool     `yaml:"lines"`
	Then     string   `yaml:"then"`
	Absolute bool     `yaml:"absolute"`
	match    *regexp.Regexp
}

type ReadValue struct {
	File string   `yaml:"file"`
	Key  []string `yaml:"key"`
}

type EachValue struct {
	Each  Value             `yaml:"each"`
	File  string            `yaml:"file"`
	Key   []string          `yaml:"key"`
	Where map[string]string `yaml:"where"`
}

type NameValue struct {
	Name string `yaml:"name"`
	re   *regexp.Regexp
}

func (v *Value) UnmarshalYAML(node *yaml.Node) error {
	v.line = node.Line
	switch node.Kind {
	case yaml.ScalarNode:
		v.Text = node.Value
		return nil
	case yaml.SequenceNode:
		v.List = []Value{}
		for _, item := range node.Content {
			var each Value
			if err := each.UnmarshalYAML(item); err != nil {
				return err
			}
			v.List = append(v.List, each)
		}
		return nil
	case yaml.MappingNode:
		keys := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			keys[node.Content[i].Value] = true
		}
		switch {
		case keys["run"]:
			v.Run = &RunValue{}
			return strict(node, v.Run)
		case keys["each"]:
			v.Each = &EachValue{}
			return strict(node, v.Each)
		case keys["file"]:
			v.Read = &ReadValue{}
			return strict(node, v.Read)
		case keys["name"]:
			v.Name = &NameValue{}
			return strict(node, v.Name)
		}
	}
	return fmt.Errorf("line %d: want a path, a list, {run}, {file, key}, {each, file, key, where} or {name: regex}", node.Line)
}

func (c checker) value(v Value, where string) error {
	where = fmt.Sprintf("%s (line %d)", where, v.line)
	switch {
	case v.Text != "":
		for _, choice := range strings.Split(v.Text, " or ") {
			if err := c.text(strings.TrimSpace(choice), where, true); err != nil {
				return err
			}
		}
	case v.List != nil:
		for _, item := range v.List {
			if err := c.value(item, where); err != nil {
				return err
			}
		}
	case v.Run != nil:
		if len(v.Run.Run) == 0 {
			return fmt.Errorf("%s: run: want the command and its arguments", where)
		}
		if err := c.words(v.Run.Run, where); err != nil {
			return err
		}
		if v.Run.Timeout != "" {
			if err := c.amount(v.Run.Timeout, where+": timeout", "timeout"); err != nil {
				return err
			}
		}
		if v.Run.Match != "" {
			re, err := regexp.Compile(v.Run.Match)
			if err != nil || re.NumSubexp() < 1 {
				return fmt.Errorf("%s: match %q: want a regular expression with a group, the value", where, v.Run.Match)
			}
			v.Run.match = re
		}
		if v.Run.Lines && v.Run.Match != "" {
			return fmt.Errorf("%s: want lines or match, not both", where)
		}
	case v.Read != nil:
		return checkRead(where, v.Read.File, v.Read.Key)
	case v.Each != nil:
		if err := c.value(v.Each.Each, where+": each"); err != nil {
			return err
		}
		for key, want := range v.Each.Where {
			if err := c.text(want, where+": where: "+key, false); err != nil {
				return err
			}
		}
		return checkRead(where, v.Each.File, v.Each.Key)
	case v.Name != nil:
		re, err := regexp.Compile(v.Name.Name)
		if err != nil || re.NumSubexp() != 1 {
			return fmt.Errorf("%s: name %q: want a regular expression with one group, the value", where, v.Name.Name)
		}
		v.Name.re = re
	default:
		return fmt.Errorf("%s: want a value", where)
	}
	return nil
}

func checkRead(where, file string, keys []string) error {
	switch {
	case file == "" || filepath.IsAbs(file) || slices.Contains(strings.Split(file, "/"), ".."):
		return fmt.Errorf("%s: file %q: want a path inside the thing read at, like Info.plist", where, file)
	case filepath.Ext(file) != ".plist" && filepath.Ext(file) != ".json":
		return fmt.Errorf("%s: file %q: want a .plist or a .json", where, file)
	case len(keys) == 0:
		return fmt.Errorf("%s: key: want the keys to read, the first present winning, dotted for nested ones", where)
	}
	return nil
}

func literal(a Amount, kind string) (any, error) {
	if kind == "count" {
		return strconv.Atoi(string(a))
	}
	return config.Parse(config.Setting{Key: "duration", Kind: config.Duration}, string(a), "/")
}

func (c *ctx) eval(v Value, at string) ([]string, bool) {
	switch {
	case v.Text != "":
		choices := strings.Split(v.Text, " or ")
		for i, choice := range choices {
			found, ok := c.choice(strings.TrimSpace(choice), at)
			if !ok || len(found) > 0 || i == len(choices)-1 {
				return found, ok
			}
		}
	case v.List != nil:
		var all []string
		for _, item := range v.List {
			found, ok := c.eval(item, at)
			if !ok {
				return nil, false
			}
			all = append(all, found...)
		}
		slices.Sort(all)
		return slices.Compact(all), true
	case v.Run != nil:
		return c.runValue(v.Run)
	case v.Read != nil:
		return c.read(filepath.Join(at, v.Read.File), v.Read.Key)
	case v.Each != nil:
		return c.each(v.Each, at)
	case v.Name != nil:
		match := v.Name.re.FindStringSubmatch(filepath.Base(at))
		if match == nil || match[1] == "" {
			return nil, true
		}
		return []string{match[1]}, true
	}
	return nil, true
}

func (c *ctx) choice(text, at string) ([]string, bool) {
	if match := placeholder.FindStringSubmatch(text); match != nil && match[0] == text {
		return c.named(match[1])
	}
	text, ok := c.fill(text, false)
	if !ok {
		return nil, false
	}
	var path string
	switch match := variable.FindStringSubmatch(text); {
	case match != nil:
		base := c.env.Getenv(match[1])
		if base == "" {
			return nil, true
		}
		path = filepath.Join(base, strings.TrimPrefix(match[2], "/"))
	case strings.HasPrefix(text, "~/"):
		path = filepath.Join(c.env.Home, text[2:])
	case filepath.IsAbs(text):
		path = filepath.Join(c.env.Under, text)
	default:
		path = filepath.Join(at, text)
	}
	if !strings.ContainsAny(path, `*?[`) {
		return []string{path}, true
	}
	found, err := expand(path)
	if err != nil {
		c.r.Fail("%s: %v", text, err)
		return nil, false
	}
	return found, true
}

func expand(pattern string) ([]string, error) {
	parts := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	paths := []string{"/"}
	for i, part := range parts {
		var next []string
		for _, dir := range paths {
			switch {
			case part == "**":
				err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if entry.IsDir() {
						next = append(next, path)
					}
					return nil
				})
				if err != nil {
					return nil, err
				}
			case !strings.ContainsAny(part, `*?[`):
				if _, err := os.Lstat(filepath.Join(dir, part)); err == nil {
					next = append(next, filepath.Join(dir, part))
				}
			default:
				entries, err := os.ReadDir(dir)
				if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
					return nil, err
				}
				for _, entry := range entries {
					if matched, _ := filepath.Match(part, entry.Name()); matched {
						next = append(next, filepath.Join(dir, entry.Name()))
					}
				}
			}
		}
		slices.Sort(next)
		paths = slices.Compact(next)
		if i == len(parts)-1 {
			return paths, nil
		}
	}
	return paths, nil
}

func (c *ctx) named(name string) ([]string, bool) {
	if found, ok := c.vars[name]; ok {
		return found, true
	}
	switch name {
	case "uid":
		return []string{strconv.Itoa(os.Getuid())}, true
	}
	return c.env.Dirs(c.file.configKey(name)), true
}

func (c *ctx) fill(text string, escape bool) (string, bool) {
	ok := true
	filled := placeholder.ReplaceAllStringFunc(text, func(found string) string {
		values, _ := c.named(found[1 : len(found)-1])
		if len(values) != 1 {
			c.r.Fail("%s: %s is %d values here, where one is wanted", text, found, len(values))
			ok = false
			return found
		}
		if escape {
			return regexp.QuoteMeta(values[0])
		}
		return values[0]
	})
	return filled, ok
}

func (c *ctx) fillAll(words []string) ([]string, bool) {
	filled := make([]string, len(words))
	for i, word := range words {
		var ok bool
		if filled[i], ok = c.fill(word, false); !ok {
			return nil, false
		}
	}
	return filled, true
}

func (c *ctx) runValue(v *RunValue) ([]string, bool) {
	args, ok := c.fillAll(v.Run)
	if !ok {
		return nil, false
	}
	words := strings.Join(args, " ")
	result := c.env.Commands.Run(c.duration(v.Timeout), args[0], args[1:]...)
	switch {
	case result.TimedOut:
		c.r.Fail("%s did not answer within %s", words, c.within(v.Timeout))
		return nil, false
	case result.Err != nil && c.fallback:
		return nil, true
	case result.Err != nil:
		c.r.Fail("%s: %v: %s", words, result.Err, result.Output())
		return nil, false
	}
	if diagnostics := strings.TrimSpace(result.Stderr); diagnostics != "" && !c.fallback {
		fmt.Fprintln(c.r.Err, diagnostics)
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	var found []string
	switch {
	case v.Lines:
		for _, line := range lines {
			if line = strings.TrimSpace(line); line != "" {
				found = append(found, line)
			}
		}
		slices.Sort(found)
		return slices.Compact(found), true
	case v.match != nil:
		for _, line := range lines {
			if m := v.match.FindStringSubmatch(line); m != nil {
				found = []string{m[1]}
			}
		}
		return found, true
	case len(lines) > 1:
		c.r.Fail("%s: expected one line, got %q", words, strings.TrimSpace(result.Stdout))
		return nil, false
	}
	value := strings.TrimSpace(lines[0])
	switch {
	case value == "", v.Absolute && !filepath.IsAbs(value):
		return nil, true
	case v.Then != "":
		value = filepath.Join(value, v.Then)
	}
	return []string{value}, true
}

func (c *ctx) each(v *EachValue, at string) ([]string, bool) {
	paths, ok := c.eval(v.Each, at)
	if !ok {
		return nil, false
	}
	want := map[string]string{}
	for key, text := range v.Where {
		if want[key], ok = c.fill(text, false); !ok {
			return nil, false
		}
	}
	var found []string
	for _, path := range paths {
		values, present, err := readKeys(filepath.Join(path, v.File))
		if err != nil {
			c.r.Fail("read %s: %v", filepath.Join(path, v.File), err)
			return nil, false
		}
		if !present || slices.ContainsFunc(sortedKeys(want), func(key string) bool { return values[key] != want[key] }) {
			continue
		}
		if value := first(values, v.Key); value != "" {
			found = append(found, value)
		}
	}
	slices.Sort(found)
	return slices.Compact(found), true
}

func (c *ctx) read(path string, keys []string) ([]string, bool) {
	values, present, err := readKeys(path)
	if err != nil {
		c.r.Fail("read %s: %v", path, err)
		return nil, false
	}
	if value := first(values, keys); present && value != "" {
		return []string{value}, true
	}
	return nil, true
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func first(values map[string]string, keys []string) string {
	for _, key := range keys {
		if value := values[key]; value != "" {
			return value
		}
	}
	return ""
}

func readKeys(path string) (map[string]string, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	if filepath.Ext(path) == ".json" {
		values, err := jsonStrings(file)
		return values, err == nil, err
	}
	values, err := plistStrings(file)
	return values, err == nil, err
}

func jsonStrings(reader io.Reader) (map[string]string, error) {
	var data any
	if err := json.NewDecoder(reader).Decode(&data); err != nil {
		return nil, err
	}
	values := map[string]string{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch v := v.(type) {
		case map[string]any:
			if prefix != "" {
				values[prefix] = "{}"
			}
			for key, inner := range v {
				name := key
				if prefix != "" {
					name = prefix + "." + key
				}
				walk(name, inner)
			}
		case string:
			values[prefix] = v
		case nil:
		default:
			text, _ := json.Marshal(v)
			values[prefix] = string(text)
		}
	}
	walk("", data)
	return values, nil
}

type plistContainer struct {
	key     string
	inArray bool
}

func plistStrings(reader io.Reader) (map[string]string, error) {
	decoder := xml.NewDecoder(reader)
	values := map[string]string{}
	var open []plistContainer
	key := ""
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if len(open) != 0 {
				return nil, errors.New("the property list ends inside a dict or array")
			}
			return values, nil
		}
		if err != nil {
			return nil, err
		}
		element, ok := token.(xml.StartElement)
		if !ok {
			if end, ok := token.(xml.EndElement); ok && (end.Name.Local == "dict" || end.Name.Local == "array") && len(open) > 0 {
				open = open[:len(open)-1]
			}
			continue
		}
		switch element.Name.Local {
		case "dict", "array":
			open = append(open, plistContainer{
				key:     key,
				inArray: element.Name.Local == "array" || (len(open) > 0 && open[len(open)-1].inArray),
			})
			key = ""
		case "key":
			if err := decoder.DecodeElement(&key, &element); err != nil {
				return nil, err
			}
		case "string", "date", "integer":
			var text string
			if err := decoder.DecodeElement(&text, &element); err != nil {
				return nil, err
			}
			if len(open) > 0 && !open[len(open)-1].inArray {
				values[dottedKey(open, key)] = text
			}
			key = ""
		default:
			key = ""
		}
	}
}

func dottedKey(open []plistContainer, key string) string {
	var names []string
	for _, container := range open[1:] {
		names = append(names, container.key)
	}
	return strings.Join(append(names, key), ".")
}

func parseTime(text string) (time.Time, error) {
	return time.Parse(time.RFC3339, text)
}
