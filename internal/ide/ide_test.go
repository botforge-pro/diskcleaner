package ide

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type machine struct {
	apps    string
	support string
	logs    string
}

func newMachine(t *testing.T) machine {
	root := t.TempDir()
	m := machine{
		apps:    filepath.Join(root, "Applications"),
		support: filepath.Join(root, "Library", "Application Support", "JetBrains"),
		logs:    filepath.Join(root, "Library", "Logs", "Google"),
	}
	for _, dir := range []string{m.apps, m.support, m.logs} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	return m
}

func install(t *testing.T, apps, app, dataDirectoryName string) {
	resources := filepath.Join(apps, app, "Contents", "Resources")
	require.NoError(t, os.MkdirAll(resources, 0o755))
	info := `{"name": "IDE", "version": "x", "dataDirectoryName": "` + dataDirectoryName + `"}`
	require.NoError(t, os.WriteFile(filepath.Join(resources, "product-info.json"), []byte(info), 0o644))
}

func data(t *testing.T, folder, name string) string {
	path := filepath.Join(folder, name)
	require.NoError(t, os.MkdirAll(filepath.Join(path, "options"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "options", "ide.xml"), make([]byte, 8192), 0o644))
	return path
}

func (m machine) step() Step {
	return Step{Applications: []string{m.apps}, Folders: []string{m.support, m.logs}}
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestRemovesOlderVersionsOfAnInstalledIDE(t *testing.T) {
	m := newMachine(t)
	install(t, m.apps, "GoLand.app", "GoLand2026.2")
	install(t, m.apps, "Android Studio.app", "AndroidStudio2025.3.2")
	old := data(t, m.support, "GoLand2025.3")
	current := data(t, m.support, "GoLand2026.2")
	oldLogs := data(t, m.logs, "AndroidStudio2025.3.1")
	currentLogs := data(t, m.logs, "AndroidStudio2025.3.2")

	r, stdout, stderr := run(m.step(), false)

	assert.Empty(t, stderr)
	assert.NoDirExists(t, old)
	assert.NoDirExists(t, oldLogs)
	assert.DirExists(t, current)
	assert.DirExists(t, currentLogs)
	assert.Equal(t, 2, r.Count)
	assert.Contains(t, stdout, "IDE      data of older versions of installed IDEs\n")
}

func TestKeepsEveryInstalledVersion(t *testing.T) {
	m := newMachine(t)
	install(t, m.apps, "GoLand.app", "GoLand2026.2")
	install(t, m.apps, "JetBrains Toolbox/GoLand 2025.3.app", "GoLand2025.3")
	older := data(t, m.support, "GoLand2025.1")
	installed := data(t, m.support, "GoLand2025.3")

	r, _, _ := run(m.step(), false)

	assert.NoDirExists(t, older)
	assert.DirExists(t, installed)
	assert.Equal(t, 1, r.Count)
}

func TestKeepsNewerVersionsAndProductsNotInstalled(t *testing.T) {
	m := newMachine(t)
	install(t, m.apps, "PyCharm.app", "PyCharm2025.3")
	newer := data(t, m.support, "PyCharm2026.1")
	other := data(t, m.support, "IntelliJIdea2024.1")
	unversioned := data(t, m.support, "Pycharm")

	r, _, stderr := run(m.step(), false)

	assert.DirExists(t, newer)
	assert.DirExists(t, other)
	assert.DirExists(t, unversioned)
	assert.Zero(t, r.Count)
	assert.Empty(t, stderr)
}

func TestDryRunRemovesNothing(t *testing.T) {
	m := newMachine(t)
	install(t, m.apps, "GoLand.app", "GoLand2026.2")
	old := data(t, m.support, "GoLand2025.3")

	r, _, _ := run(m.step(), true)

	assert.DirExists(t, old)
	assert.Equal(t, 1, r.Count)
}

func TestUnreadableProductInfoRemovesNothingAndFails(t *testing.T) {
	m := newMachine(t)
	install(t, m.apps, "GoLand.app", "GoLand2026.2")
	broken := filepath.Join(m.apps, "Broken.app", "Contents", "Resources")
	require.NoError(t, os.MkdirAll(broken, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(broken, "product-info.json"), []byte("{"), 0o644))
	old := data(t, m.support, "GoLand2025.3")

	r, _, _ := run(m.step(), false)

	assert.DirExists(t, old)
	assert.Zero(t, r.Count)
	assert.Len(t, r.Failures, 1)
}

func TestNoIDEIsQuiet(t *testing.T) {
	m := newMachine(t)
	data(t, m.support, "GoLand2025.3")

	r, stdout, stderr := run(m.step(), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
