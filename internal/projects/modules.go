package projects

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/botforge-pro/diskcleaner/internal/candidates"
	"github.com/botforge-pro/diskcleaner/internal/report"
)

var lockfiles = []string{"package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml"}

func (s Step) collectModules(r *report.Report, set *candidates.Set, root string) {
	var walkErrors []error
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			walkErrors = append(walkErrors, err)
			return nil
		}
		if !entry.IsDir() || entry.Name() != "node_modules" {
			return nil
		}
		if hasLockfile(root, filepath.Dir(path)) {
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
		r.Fail("scan %s: %v", root, errors.Join(walkErrors...))
	}
}

func hasLockfile(root, dir string) bool {
	if dirHasLockfile(dir) {
		return true
	}
	for dir != root && dir != "/" && dir != "." {
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
