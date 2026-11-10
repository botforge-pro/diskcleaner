package projects

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/inuse"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

var gradleFiles = []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"}

type Step struct {
	Dirs    []string
	InUse   inuse.Check
	Garbage GitGarbage
	LFS     LFS
}

func Default(dirs []string, gitTmpMinAge time.Duration) (Step, error) {
	var resolved []string
	for _, dir := range dirs {
		canonical, err := settings.Canonical(dir)
		if err != nil || !isDir(canonical) {
			return Step{}, fmt.Errorf("project folder not found: %s (see project-dirs)", dir)
		}
		if !slices.Contains(resolved, canonical) {
			resolved = append(resolved, canonical)
		}
	}
	check, err := inuse.Default()
	if err != nil {
		return Step{}, err
	}
	garbage, err := DefaultGitGarbage(gitTmpMinAge)
	if err != nil {
		return Step{}, err
	}
	return Step{Dirs: resolved, InUse: check, Garbage: garbage, LFS: DefaultLFS()}, nil
}

func (s Step) Run(r *report.Report) {
	for _, dir := range s.Dirs {
		if !r.Directory(dir) {
			continue
		}
		repos, err := Repositories(dir)
		if err != nil {
			r.Fail("scan %s: %v", dir, err)
		}
		for _, repo := range repos {
			fmt.Fprintf(r.Out, "PROJECT  %s\n", repo)
			var builds, garbage candidates.Set
			s.collectBuilds(r, &builds, repo)
			builds.RemoveUnless(r, func(path string) bool { return s.InUse.Keep(r, path) })
			s.Garbage.Collect(r, &garbage, repo)
			garbage.Remove(r)
			s.LFS.Prune(r, repo)
			fmt.Fprintln(r.Out)
		}
		var modules candidates.Set
		s.collectModules(r, &modules, dir)
		modules.RemoveUnless(r, func(path string) bool { return s.InUse.Keep(r, path) })
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
