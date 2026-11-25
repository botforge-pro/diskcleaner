package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func Canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs, err
	}
	return diskCase(resolved)
}

func diskCase(path string) (string, error) {
	parent := filepath.Dir(path)
	if parent == path {
		return path, nil
	}
	parent, err := diskCase(parent)
	if err != nil {
		return "", err
	}
	name := filepath.Base(path)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", fmt.Errorf("the case of %s on disk: %w", path, err)
	}
	folded := ""
	for _, entry := range entries {
		if entry.Name() == name {
			return filepath.Join(parent, name), nil
		}
		if folded == "" && strings.EqualFold(entry.Name(), name) {
			folded = entry.Name()
		}
	}
	if folded != "" {
		return filepath.Join(parent, folded), nil
	}
	return filepath.Join(parent, name), nil
}
