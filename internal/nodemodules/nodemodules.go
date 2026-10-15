package nodemodules

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/projects"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

var lockfiles = []string{"package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml"}

type Step struct {
	Root      string
	ReposRoot string
	Garbage   projects.GitGarbage
}

func Default(home string) (Step, error) {
	garbage, err := projects.DefaultGitGarbage()
	if err != nil {
		return Step{}, err
	}
	return Step{
		Root:      resolve(settings.Env("DISKCLEANER_NODE_DIR", filepath.Join(home, "GolandProjects"))),
		ReposRoot: resolve(settings.Env("DISKCLEANER_REPOS_DIR", filepath.Join(home, "Developer"))),
		Garbage:   garbage,
	}, nil
}

func resolve(path string) string {
	resolved, _ := settings.Canonical(path)
	return resolved
}

func (s Step) Run(r *report.Report) {
	if !r.Directory(s.Root) {
		return
	}
	fmt.Fprintf(r.Out, "NODE     %s\n", s.Root)
	defer fmt.Fprintln(r.Out)

	var set candidates.Set
	var walkErrors []error
	err := filepath.WalkDir(s.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			walkErrors = append(walkErrors, err)
			return nil
		}
		if !entry.IsDir() || entry.Name() != "node_modules" {
			return nil
		}
		if s.hasLockfile(filepath.Dir(path)) {
			set.Add(r, path)
		} else {
			r.Skip("%s: no lockfile, reinstall would not restore the same versions", path)
		}
		return filepath.SkipDir
	})
	if err != nil {
		walkErrors = append(walkErrors, err)
	}
	if len(walkErrors) > 0 {
		r.Fail("scan %s: %v", s.Root, errors.Join(walkErrors...))
	}

	if s.Root != s.ReposRoot {
		repos, err := projects.Repositories(s.Root)
		if err != nil {
			r.Fail("scan %s: %v", s.Root, err)
		}
		for _, repo := range repos {
			s.Garbage.Collect(r, &set, repo)
		}
	}
	set.Remove(r)
}

func (s Step) hasLockfile(dir string) bool {
	if dirHasLockfile(dir) {
		return true
	}
	for dir != s.Root && dir != "/" && dir != "." {
		dir = filepath.Dir(dir)
		if isWorkspaceRoot(dir) && dirHasLockfile(dir) {
			return true
		}
	}
	return false
}

func dirHasLockfile(dir string) bool {
	return slices.ContainsFunc(lockfiles, func(name string) bool {
		info, err := os.Stat(filepath.Join(dir, name))
		return err == nil && info.Mode().IsRegular()
	})
}

func isWorkspaceRoot(dir string) bool {
	if info, err := os.Stat(filepath.Join(dir, "pnpm-workspace.yaml")); err == nil && info.Mode().IsRegular() {
		return true
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "package.json"))
	return err == nil && bytes.Contains(manifest, []byte(`"workspaces"`))
}
