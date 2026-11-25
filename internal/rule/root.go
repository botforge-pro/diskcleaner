package rule

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/botforge-pro/diskcleaner/internal/report"
)

type place struct {
	variable string
	path     string
	command  []string
	absolute bool
	then     string
}

type Root struct {
	places []place
	given  bool
}

type commandPlace struct {
	Command  []string `yaml:"command"`
	Absolute bool     `yaml:"absolute"`
	Then     string   `yaml:"then"`
}

const rootForms = "root: want a path, a variable choice like $TOOL_HOME/cache or ~/.tool/cache, {command: [...]}, or a list of these"

func (root *Root) UnmarshalYAML(node *yaml.Node) error {
	root.given = true
	switch node.Kind {
	case yaml.ScalarNode:
		for _, part := range strings.Split(node.Value, " or ") {
			p, err := pathPlace(strings.TrimSpace(part))
			if err != nil {
				return err
			}
			root.places = append(root.places, p)
		}
		return nil
	case yaml.MappingNode:
		p, err := decodeCommandPlace(node)
		root.places = []place{p}
		return err
	case yaml.SequenceNode:
		for _, item := range node.Content {
			var p place
			var err error
			if item.Kind == yaml.ScalarNode {
				p, err = pathPlace(item.Value)
			} else {
				p, err = decodeCommandPlace(item)
			}
			if err != nil {
				return err
			}
			root.places = append(root.places, p)
		}
		return nil
	}
	return fmt.Errorf("line %d: %s", node.Line, rootForms)
}

func decodeCommandPlace(node *yaml.Node) (place, error) {
	var c commandPlace
	if err := strict(node, &c); err != nil {
		return place{}, fmt.Errorf("root: %w", err)
	}
	if len(c.Command) == 0 {
		return place{}, errors.New("root: command: want the command and its arguments, whose one line of output is the folder")
	}
	return place{command: c.Command, absolute: c.Absolute, then: c.Then}, nil
}

func pathPlace(part string) (place, error) {
	if match := variable.FindStringSubmatch(part); match != nil {
		return place{variable: match[1], path: strings.TrimPrefix(match[2], "/")}, nil
	}
	if !strings.HasPrefix(part, "~/") && !filepath.IsAbs(part) {
		return place{}, fmt.Errorf("root: %q: want $VARIABLE, $VARIABLE/subfolder, a path starting with ~/ or /", part)
	}
	return place{path: part}, nil
}

func (root Root) empty() bool {
	return !root.given
}

func (root Root) check(required bool) error {
	if !root.given {
		if required {
			return errors.New("root: want a folder, like ~/Library/Caches/tool or $TOOL_HOME/cache or ~/.tool/cache")
		}
		return nil
	}
	if last := root.places[len(root.places)-1]; last.variable != "" {
		return fmt.Errorf("root: $%s: the last choice must not be a variable, so that a root is always found", last.variable)
	}
	return nil
}

func (root Root) words() string {
	words := make([]string, len(root.places))
	for i, p := range root.places {
		switch {
		case p.command != nil:
			words[i] = "the folder `" + strings.Join(p.command, " ") + "` names"
		case p.variable == "":
			words[i] = "`" + p.path + "`"
		case p.path == "":
			words[i] = "`$" + p.variable + "`"
		default:
			words[i] = "`$" + p.variable + "/" + p.path + "`"
		}
	}
	return strings.Join(words, ", else ")
}

func (s Rule) resolve(r *report.Report, root Root) (string, bool) {
	for i, p := range root.places {
		last := i == len(root.places)-1
		switch {
		case p.command != nil:
			folder, ok := s.commandFolder(r, p, last)
			if ok {
				return folder, true
			}
			if last {
				return "", false
			}
		case p.variable == "":
			return expand(s.env.Home, p.path), true
		default:
			if value := s.env.Getenv(p.variable); value != "" {
				return filepath.Join(value, p.path), true
			}
		}
	}
	return "", false
}

func (s Rule) commandFolder(r *report.Report, p place, last bool) (string, bool) {
	words := strings.Join(p.command, " ")
	result := s.env.Commands.Run(0, p.command[0], p.command[1:]...)
	if result.Err != nil {
		if last {
			r.Fail("%s: %v: %s", words, result.Err, result.Output())
		}
		return "", false
	}
	if diagnostics := strings.TrimSpace(result.Stderr); diagnostics != "" && last {
		fmt.Fprintln(r.Err, diagnostics)
	}
	value := strings.TrimSpace(result.Stdout)
	switch {
	case strings.Contains(value, "\n"):
		r.Fail("%s: expected one line, got %q", words, value)
		return "", false
	case value == "", p.absolute && !filepath.IsAbs(value):
		return "", false
	case p.then != "":
		return filepath.Clean(filepath.Join(value, p.then)), true
	}
	return value, true
}

func expand(home, place string) string {
	if under, ok := strings.CutPrefix(place, "~/"); ok {
		return filepath.Join(home, under)
	}
	return place
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
