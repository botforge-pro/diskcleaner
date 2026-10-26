package settings

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func UserTemp() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", nil
	}
	var stdout, stderr bytes.Buffer
	command := exec.Command("getconf", "DARWIN_USER_TEMP_DIR")
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("getconf DARWIN_USER_TEMP_DIR: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	temp := strings.TrimSpace(stdout.String())
	if !filepath.IsAbs(temp) {
		return "", fmt.Errorf("getconf DARWIN_USER_TEMP_DIR: expected an absolute path, got %q", temp)
	}
	return filepath.Clean(temp), nil
}
