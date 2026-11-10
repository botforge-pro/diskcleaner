package xcode

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

const archiveSuffix = ".xcarchive"

type Archives struct {
	Folder    string
	MinAge    time.Duration
	Kept      int
	Now       func() time.Time
	Processes string
	Running   func(pattern string) (bool, error)
}

func DefaultArchives(home string, minAge time.Duration, kept int) (Archives, error) {
	return Archives{
		Folder:    filepath.Join(home, "Library/Developer/Xcode/Archives"),
		MinAge:    minAge,
		Kept:      kept,
		Now:       time.Now,
		Processes: process.Xcode,
		Running:   process.Running,
	}, nil
}

type archive struct {
	path    string
	app     string
	created time.Time
}

func (s Archives) Run(r *report.Report) {
	if !r.Directory(s.Folder) {
		return
	}
	folder, err := filepath.EvalSymlinks(s.Folder)
	if err != nil {
		r.Fail("resolve %s: %v", s.Folder, err)
		return
	}
	fmt.Fprintf(r.Out, "ARCHIVES %s\n", folder)
	defer fmt.Fprintln(r.Out)

	if r.Busy(s.Running, s.Processes) {
		r.Skip("%s: Xcode or an Xcode build is running", folder)
		return
	}
	byApp := map[string][]archive{}
	for _, path := range s.find(r, folder) {
		found, err := readArchive(path)
		if err != nil {
			r.Skip("%s: its Info.plist does not say which app it holds and when it was made", path)
			r.Fail("read %s: %v", filepath.Join(path, "Info.plist"), err)
			continue
		}
		byApp[found.app] = append(byApp[found.app], found)
	}
	apps := make([]string, 0, len(byApp))
	for app := range byApp {
		apps = append(apps, app)
	}
	slices.Sort(apps)
	for _, app := range apps {
		made := byApp[app]
		slices.SortFunc(made, func(a, b archive) int { return b.created.Compare(a.created) })
		for _, old := range made[min(s.Kept, len(made)):] {
			if s.old(old.created) {
				r.Remove(old.path)
			}
		}
	}
}

func (s Archives) old(created time.Time) bool {
	return s.Now().Sub(created) >= s.MinAge
}

func (s Archives) find(r *report.Report, folder string) []string {
	entries, err := os.ReadDir(folder)
	if err != nil {
		r.Fail("find %s: %v", folder, err)
		return nil
	}
	var found []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(folder, entry.Name())
		if strings.HasSuffix(entry.Name(), archiveSuffix) {
			found = append(found, path)
			continue
		}
		if !isDateFolder(entry.Name()) {
			continue
		}
		inner, err := os.ReadDir(path)
		if err != nil {
			r.Fail("find %s: %v", path, err)
			continue
		}
		for _, item := range inner {
			if item.IsDir() && strings.HasSuffix(item.Name(), archiveSuffix) {
				found = append(found, filepath.Join(path, item.Name()))
			}
		}
	}
	return found
}

func isDateFolder(name string) bool {
	_, err := time.Parse(time.DateOnly, name)
	return err == nil
}

func readArchive(path string) (archive, error) {
	file, err := os.Open(filepath.Join(path, "Info.plist"))
	if err != nil {
		return archive{}, err
	}
	defer file.Close()
	values, err := plistStrings(file)
	if err != nil {
		return archive{}, err
	}
	app := values["ApplicationProperties.CFBundleIdentifier"]
	if app == "" {
		app = values["Name"]
	}
	if app == "" {
		return archive{}, errors.New("no CFBundleIdentifier and no Name")
	}
	created, err := time.Parse(time.RFC3339, values["CreationDate"])
	if err != nil {
		return archive{}, fmt.Errorf("CreationDate: %w", err)
	}
	return archive{path: path, app: app, created: created}, nil
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
		case "string", "date":
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
