package projects

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

var gradleFiles = []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"}

type Step struct {
	Repos   string
	Garbage GitGarbage
	LFS     LFS
}

func Default(home string) (Step, error) {
	repos := settings.Env("DISKCLEANER_REPOS_DIR", filepath.Join(home, "Developer"))
	resolved, err := settings.Canonical(repos)
	if err != nil {
		return Step{}, fmt.Errorf("repositories directory not found: %s (set DISKCLEANER_REPOS_DIR): %w", repos, err)
	}
	if !isDir(resolved) {
		return Step{}, fmt.Errorf("repositories directory not found: %s (set DISKCLEANER_REPOS_DIR)", repos)
	}
	garbage, err := DefaultGitGarbage()
	if err != nil {
		return Step{}, err
	}
	return Step{Repos: resolved, Garbage: garbage, LFS: DefaultLFS()}, nil
}

func (s Step) Run(r *report.Report) {
	repos, err := Repositories(s.Repos)
	if err != nil {
		r.Fail("scan %s: %v", s.Repos, err)
	}
	for _, repo := range repos {
		name, err := filepath.Rel(s.Repos, repo)
		if err != nil {
			name = repo
		}
		fmt.Fprintf(r.Out, "PROJECT  %s\n", name)
		var set candidates.Set
		s.collectBuilds(r, &set, repo)
		s.Garbage.Collect(r, &set, repo)
		set.Remove(r)
		s.LFS.Prune(r, repo)
		fmt.Fprintln(r.Out)
	}
}

func Repositories(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var repos []string
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repo := filepath.Join(root, entry.Name())
		info, err := os.Lstat(filepath.Join(repo, ".git"))
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			errs = append(errs, err)
		case info.IsDir():
			repos = append(repos, repo)
		}
	}
	slices.Sort(repos)
	return repos, errors.Join(errs...)
}

func (s Step) collectBuilds(r *report.Report, set *candidates.Set, repo string) {
	var walkErrors []error
	walk := func(skipped []string, visit func(path string, entry fs.DirEntry) error) {
		err := filepath.WalkDir(repo, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				walkErrors = append(walkErrors, err)
				return nil
			}
			if entry.IsDir() && (path == filepath.Join(repo, ".git") || slices.Contains(skipped, entry.Name())) {
				return filepath.SkipDir
			}
			return visit(path, entry)
		})
		if err != nil {
			walkErrors = append(walkErrors, err)
		}
	}

	walk([]string{"node_modules"}, func(path string, entry fs.DirEntry) error {
		if !entry.IsDir() || entry.Name() != ".build" {
			return nil
		}
		if isFile(filepath.Join(filepath.Dir(path), "Package.swift")) {
			set.Add(r, path)
		} else {
			r.Skip("%s: no Package.swift next to it", path)
		}
		return filepath.SkipDir
	})
	walk([]string{"node_modules", ".build"}, func(path string, entry fs.DirEntry) error {
		if !entry.IsDir() || entry.Name() != "build" {
			return nil
		}
		module := filepath.Dir(path)
		if slices.ContainsFunc(gradleFiles, func(file string) bool { return isFile(filepath.Join(module, file)) }) {
			set.Add(r, path)
		}
		return filepath.SkipDir
	})
	if isFile(filepath.Join(repo, "gradlew")) {
		walk([]string{"node_modules"}, func(path string, entry fs.DirEntry) error {
			if entry.Type().IsRegular() && isPackage(entry.Name()) {
				set.Add(r, path)
			}
			return nil
		})
	}
	if len(walkErrors) > 0 {
		r.Fail("scan %s: %v", repo, errors.Join(walkErrors...))
	}
}

func isPackage(name string) bool {
	return strings.HasSuffix(name, ".apk") || strings.HasSuffix(name, ".aab")
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
