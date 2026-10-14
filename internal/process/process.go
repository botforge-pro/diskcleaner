package process

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const Xcode = `xcodebuild|XCBBuildService|/Xcode[^/]*\.app/Contents/MacOS/Xcode`

func Running(pattern string) (bool, error) {
	output, err := exec.Command("pgrep", "-f", pattern).CombinedOutput()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
}
