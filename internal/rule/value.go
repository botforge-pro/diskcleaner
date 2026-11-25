package rule

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Value struct {
	File string   `yaml:"file"`
	Key  []string `yaml:"key"`
}

func (v Value) check() error {
	switch {
	case v.File == "" || !inside(v.File):
		return fmt.Errorf("want file, a path inside the folder, like Info.plist")
	case filepath.Ext(v.File) != ".plist":
		return fmt.Errorf("file %q: want a .plist", v.File)
	case len(v.Key) == 0:
		return errors.New("key: want the keys to read, the first that is set winning, dotted for nested ones")
	}
	return nil
}

func (v Value) read(folder string) (string, error) {
	file, err := os.Open(filepath.Join(folder, v.File))
	if err != nil {
		return "", err
	}
	defer file.Close()
	values, err := plistStrings(file)
	if err != nil {
		return "", err
	}
	for _, key := range v.Key {
		if value := values[key]; value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("no %s", strings.Join(v.Key, " and no "))
}

func (v Value) time(folder string) (time.Time, error) {
	text, err := v.read(folder)
	if err != nil {
		return time.Time{}, err
	}
	when, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", strings.Join(v.Key, " or "), err)
	}
	return when, nil
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
			if end, ok := token.(xml.EndElement); ok && isContainer(end.Name.Local) && len(open) > 0 {
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

func isContainer(name string) bool {
	return name == "dict" || name == "array"
}

func dottedKey(open []plistContainer, key string) string {
	var names []string
	for _, container := range open[1:] {
		names = append(names, container.key)
	}
	return strings.Join(append(names, key), ".")
}
