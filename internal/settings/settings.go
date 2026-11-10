package settings

import (
	"os"
	"path/filepath"
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
	return resolved, nil
}
