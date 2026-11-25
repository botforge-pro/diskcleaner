package main

import (
	"os"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/rule"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheREADMEListsTheStepsAsTheirFilesSay(t *testing.T) {
	current, err := os.ReadFile("README.md")
	require.NoError(t, err)

	wanted, err := withStepsTable(string(current), catalog)
	require.NoError(t, err)

	if os.Getenv("UPDATE_README") != "" {
		require.NoError(t, os.WriteFile("README.md", []byte(wanted), 0o644))
		return
	}
	assert.Equal(t, wanted, string(current), "run make readme")
}

func parsedStep(t *testing.T, name, body string) stepInfo {
	f, err := rule.Parse(name+".yaml", []byte(body))
	require.NoError(t, err)
	return stepInfo{f.Step(), nil, f}
}

func TestEachStepIsARowInBoldWhenItCannotBeUndone(t *testing.T) {
	steps := []stepInfo{
		parsedStep(t, "some-cache", "order: 1\ndescribes: the cache of some tool\nreversible: true\nreadme: {keeps: all while it runs}\nrun: code\n"),
		parsedStep(t, "old-logs", "order: 2\ndescribes: logs older than {min-age}\nreadme: {can-lose: a pipe | in the text}\nsettings: [{key: min-age, kind: duration, default: 20d, help: x}]\nrun: code\n"),
	}

	assert.Equal(t, stepsHeader+
		"| `some-cache` | the cache of some tool | all while it runs |  |\n"+
		"| **`old-logs`** | logs older than `old-logs.min-age` (`20d`) |  | a pipe \\| in the text |\n",
		stepsTable(steps))
}

func TestOnlyTheTableOfStepsIsReplaced(t *testing.T) {
	steps := []stepInfo{parsedStep(t, "some-cache", "order: 1\ndescribes: the cache\nreversible: true\nrun: code\n")}
	readme := "# Tool\n\n" + stepsHeader + "| `old` | x |  |  |\n\n## Next\n"

	replaced, err := withStepsTable(readme, steps)

	require.NoError(t, err)
	assert.Equal(t, "# Tool\n\n"+stepsHeader+"| `some-cache` | the cache |  |  |\n\n## Next\n", replaced)
	_, err = withStepsTable(readme+readme, steps)
	assert.EqualError(t, err, "README.md: more than one table of steps; want one")
}
