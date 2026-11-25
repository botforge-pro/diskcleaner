package main

import (
	"errors"
	"fmt"
	"strings"
)

const stepsHeader = "| Step | Removes | Keeps | Can lose |\n|---|---|---|---|\n"

func stepsTable() string {
	var b strings.Builder
	b.WriteString(stepsHeader)
	for _, info := range catalog {
		name := "`" + info.Name + "`"
		if !info.Reversible {
			name = "**" + name + "**"
		}
		f := info.file
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", name, f.Removes(), f.Explain(f.Readme.Keeps), f.Explain(f.Readme.CanLose))
	}
	return b.String()
}

func withStepsTable(readme string) (string, error) {
	start := strings.Index(readme, stepsHeader)
	if start < 0 {
		return "", errors.New("README.md: no table of steps starting with " + strings.TrimSpace(strings.SplitN(stepsHeader, "\n", 2)[0]))
	}
	end := strings.Index(readme[start:], "\n\n")
	if end < 0 {
		return "", errors.New("README.md: the table of steps has no blank line after it")
	}
	return readme[:start] + stepsTable() + readme[start+end+1:], nil
}
