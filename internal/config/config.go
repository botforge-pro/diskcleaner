package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

type Kind int

const (
	Bool Kind = iota
	Duration
	Timeout
	Size
	Count
	Dirs
)

type Setting struct {
	Key     string
	Kind    Kind
	Default string
	Help    string
}

type Step struct {
	Name       string
	Reversible bool
	Settings   []Setting
}

type Table []Setting

var ProjectDirCandidates = []string{
	"Developer", "Projects", "src", "code", "GolandProjects", "IdeaProjects", "AndroidStudioProjects",
}

func NewTable(steps []Step) Table {
	table := Table{
		{"project-dirs", Dirs, "", "folders whose subfolders are Git repositories " +
			"(default: those of ~/" + strings.Join(ProjectDirCandidates, ", ~/") + " that exist)"},
		{"min-age", Duration, "1h", "keep anything written more recently"},
		{"min-free", Size, "0", "exit 4 when less is free after clean; 0 is off"},
	}
	for _, step := range steps {
		enabled, help := "true", "run "+step.Name
		if !step.Reversible {
			enabled, help = "false", "run "+step.Name+", whose deletions cannot be undone"
		}
		table = append(table, Setting{step.Name + ".enabled", Bool, enabled, help})
		for _, setting := range step.Settings {
			setting.Key = step.Name + "." + setting.Key
			table = append(table, setting)
		}
	}
	return table
}

func (t Table) Find(key string) (Setting, bool) {
	i := slices.IndexFunc(t, func(s Setting) bool { return s.Key == key })
	if i < 0 {
		return Setting{}, false
	}
	return t[i], true
}

func (t Table) Flags(set *pflag.FlagSet) {
	set.String("config", "", "read settings from this file instead of diskcleaner.toml in $XDG_CONFIG_HOME or ~/.config")
	for _, s := range t {
		set.Var(&text{value: s.Default, kind: kindNames[s.Kind]}, s.Key, s.Help)
		if s.Kind == Bool {
			set.Lookup(s.Key).NoOptDefVal = "true"
		}
	}
}

var kindNames = map[Kind]string{
	Bool: "bool", Duration: "duration", Timeout: "duration", Size: "size", Count: "count", Dirs: "folders",
}

type text struct {
	value, kind string
}

func (t *text) String() string     { return t.value }
func (t *text) Set(s string) error { t.value = s; return nil }
func (t *text) Type() string       { return t.kind }

type Source string

const (
	FromDefault Source = "default"
	FromFile    Source = "file"
	FromFlag    Source = "flag"
)

type Config struct {
	values  map[string]any
	texts   map[string]string
	sources map[string]Source
	File    string
}

func Load(t Table, set *pflag.FlagSet, home string) (*Config, error) {
	c := &Config{values: map[string]any{}, texts: map[string]string{}, sources: map[string]Source{}}
	raw := map[string]any{}
	for _, s := range t {
		raw[s.Key] = s.Default
		c.sources[s.Key] = FromDefault
	}
	path, err := configPath(set, home)
	if err != nil {
		return nil, err
	}
	if path != "" {
		c.File = path
		k := koanf.New(".")
		if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for key, value := range k.All() {
			if emptySection, ok := value.(map[string]any); ok && len(emptySection) == 0 {
				continue
			}
			if _, known := t.Find(key); !known {
				return nil, fmt.Errorf("%s: unknown setting %q", path, key)
			}
			raw[key] = value
			c.sources[key] = FromFile
		}
	}
	set.Visit(func(f *pflag.Flag) {
		if _, known := t.Find(f.Name); known {
			raw[f.Name] = f.Value.String()
			c.sources[f.Name] = FromFlag
		}
	})
	for _, s := range t {
		value, err := parse(s, raw[s.Key], home)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.Key, err)
		}
		c.values[s.Key] = value
		if list, ok := value.([]string); ok {
			c.texts[s.Key] = strings.Join(list, ", ")
		} else {
			c.texts[s.Key] = strings.TrimSpace(fmt.Sprint(raw[s.Key]))
		}
	}
	return c, nil
}

func (c *Config) Text(key string) string { return c.texts[key] }

func configPath(set *pflag.FlagSet, home string) (string, error) {
	if f := set.Lookup("config"); f != nil && f.Changed {
		if f.Value.String() == "" {
			return "", errors.New("--config: want a file")
		}
		return f.Value.String(), nil
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".config")
	}
	path := filepath.Join(dir, "diskcleaner.toml")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return path, nil
}

func parse(s Setting, raw any, home string) (any, error) {
	if s.Kind == Dirs {
		return dirs(raw, home)
	}
	text := strings.TrimSpace(fmt.Sprint(raw))
	switch s.Kind {
	case Bool:
		value, err := strconv.ParseBool(text)
		if err != nil {
			return nil, fmt.Errorf("want true or false, got %q", text)
		}
		return value, nil
	case Duration:
		return duration(text)
	case Timeout:
		value, err := duration(text)
		if err == nil && value < time.Second {
			return nil, fmt.Errorf("want a timeout of 1s or more, got %q", text)
		}
		return value, err
	case Size:
		return size(text)
	case Count:
		value, err := strconv.Atoi(text)
		if err != nil || value < 0 {
			return nil, fmt.Errorf("want a whole number, got %q", text)
		}
		return value, nil
	}
	return nil, fmt.Errorf("no parser for kind %d", s.Kind)
}

func duration(text string) (time.Duration, error) {
	if text == "0" {
		return 0, nil
	}
	if days, ok := strings.CutSuffix(text, "d"); ok {
		n, err := strconv.ParseInt(days, 10, 64)
		if err != nil || n < 0 || n > math.MaxInt64/int64(24*time.Hour) {
			return 0, fmt.Errorf("want a duration like 1h or 15d, got %q", text)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	value, err := time.ParseDuration(text)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("want a duration like 1h or 15d, got %q", text)
	}
	return value, nil
}

var sizeUnits = []struct {
	suffix string
	bytes  int64
}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}}

func size(text string) (int64, error) {
	if text == "0" {
		return 0, nil
	}
	for _, unit := range sizeUnits {
		number, ok := strings.CutSuffix(strings.ToUpper(text), unit.suffix)
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(number), 10, 64)
		if err != nil || n < 0 || n > math.MaxInt64/unit.bytes {
			break
		}
		return n * unit.bytes, nil
	}
	return 0, fmt.Errorf("want a size like 500MB or 5GB, got %q", text)
}

func dirs(raw any, home string) ([]string, error) {
	var list []string
	switch value := raw.(type) {
	case []any:
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("want a list of folders, got %v", item)
			}
			list = append(list, text)
		}
	case string:
		if value == "" {
			return existing(home), nil
		}
		list = strings.Split(value, ",")
	default:
		return nil, fmt.Errorf("want a list of folders, got %v", raw)
	}
	for i, dir := range list {
		dir = strings.TrimSpace(dir)
		if dir == "~" || strings.HasPrefix(dir, "~/") {
			dir = filepath.Join(home, dir[1:])
		}
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("want folders starting with / or ~/, got %q", dir)
		}
		list[i] = dir
	}
	return list, nil
}

func existing(home string) []string {
	var found []string
	for _, name := range ProjectDirCandidates {
		dir := filepath.Join(home, name)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			found = append(found, dir)
		}
	}
	return found
}

func (c *Config) Source(key string) Source { return c.sources[key] }

func (c *Config) Bool(key string) bool              { return c.values[key].(bool) }
func (c *Config) Duration(key string) time.Duration { return c.values[key].(time.Duration) }
func (c *Config) Size(key string) int64             { return c.values[key].(int64) }
func (c *Config) Count(key string) int              { return c.values[key].(int) }
func (c *Config) Dirs(key string) []string          { return c.values[key].([]string) }
