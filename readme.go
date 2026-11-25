package main

import (
	"errors"
	"fmt"
	"strings"
)

const stepsHeader = "| Step | Removes | Keeps | Can lose |\n|---|---|---|---|\n"

func stepsTable(steps []stepInfo) string {
	var b strings.Builder
	b.WriteString(stepsHeader)
	for _, info := range steps {
		name := "`" + info.Name + "`"
		if !info.Reversible {
			name = "**" + name + "**"
		}
		f := info.file
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", name, cell(f.Removes()), cell(f.Explain(f.Readme.Keeps)), cell(f.Explain(f.Readme.CanLose)))
	}
	return b.String()
}

func cell(text string) string {
	return strings.ReplaceAll(text, "|", `\|`)
}

func withStepsTable(readme string, steps []stepInfo) (string, error) {
	switch strings.Count(readme, stepsHeader) {
	case 0:
		return "", errors.New("README.md: no table of steps starting with " + strings.TrimSpace(strings.SplitN(stepsHeader, "\n", 2)[0]))
	case 1:
	default:
		return "", errors.New("README.md: more than one table of steps; want one")
	}
	start := strings.Index(readme, stepsHeader)
	end := strings.Index(readme[start:], "\n\n")
	if end < 0 {
		return "", errors.New("README.md: the table of steps has no blank line after it")
	}
	return readme[:start] + stepsTable(steps) + readme[start+end+1:], nil
}
