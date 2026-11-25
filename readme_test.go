package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheREADMEListsTheStepsAsTheirFilesSay(t *testing.T) {
	current, err := os.ReadFile("README.md")
	require.NoError(t, err)

	wanted, err := withStepsTable(string(current))
	require.NoError(t, err)

	if os.Getenv("UPDATE_README") != "" {
		require.NoError(t, os.WriteFile("README.md", []byte(wanted), 0o644))
		return
	}
	assert.Equal(t, wanted, string(current), "run make readme")
}

func TestASettingMentionedInTheREADMEReadsAsItsKeyAndDefault(t *testing.T) {
	table := stepsTable()

	assert.Contains(t, table, "| **`xcode-archives`** | archives older than `min-age` (`15d`) | the newest `keep` (`1`) of each app;")
	assert.Contains(t, table, "| `gradle-caches` | `$GRADLE_USER_HOME/caches` | all while Gradle, Android Studio or IntelliJ run |  |\n")
}
