package settings

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func EnvOrUserTemp(name string, below func(temp string) string) (string, error) {
	if value := Env(name, ""); value != "" {
		return value, nil
	}
	var stdout, stderr bytes.Buffer
	command := exec.Command("getconf", "DARWIN_USER_TEMP_DIR")
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("getconf DARWIN_USER_TEMP_DIR: %v: %s; set %s instead", err, strings.TrimSpace(stderr.String()), name)
	}
	temp := strings.TrimSpace(stdout.String())
	if !filepath.IsAbs(temp) {
		return "", fmt.Errorf("getconf DARWIN_USER_TEMP_DIR: expected an absolute path, got %q; set %s instead", temp, name)
	}
	return below(filepath.Clean(temp)), nil
}
