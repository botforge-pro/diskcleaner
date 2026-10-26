package ide

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type machine struct {
	root      string
	apps      string
	library   string
	toolbox   string
	spotlight []string
}

func newMachine(t *testing.T) *machine {
	root := t.TempDir()
	m := &machine{
		root:      root,
		apps:      filepath.Join(root, "Applications"),
		library:   filepath.Join(root, "Library"),
		toolbox:   filepath.Join(root, "Library", "Application Support", "JetBrains", "Toolbox", "apps"),
		spotlight: []string{"/System/Applications/Calculator.app"},
	}
	require.NoError(t, os.MkdirAll(m.apps, 0o755))
	return m
}

func (m *machine) folder(kind, vendor string) string {
	return filepath.Join(m.library, kind, vendor)
}

func writeFile(t *testing.T, path, text string) {
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
}

func installPlatform(t *testing.T, apps, app, vendor, dataDirectoryName string) {
	writeFile(t, filepath.Join(apps, app, "Contents", "Resources", "product-info.json"),
		`{"name": "IDE", "productVendor": "`+vendor+`", "dataDirectoryName": "`+dataDirectoryName+`"}`)
}

func installEditor(t *testing.T, apps, app, nameShort, commit string) {
	writeFile(t, filepath.Join(apps, app, "Contents", "Resources", "app", "product.json"),
		`{"nameShort": "`+nameShort+`", "commit": "`+commit+`", "version": "1.0"}`)
}

func data(t *testing.T, folder, name string) string {
	path := filepath.Join(folder, name)
	writeFile(t, filepath.Join(path, "options", "ide.xml"), string(make([]byte, 8192)))
	return path
}

func (m *machine) step() Step {
	return Step{
		Applications: []string{m.apps},
		Toolbox:      m.toolbox,
		Spotlight:    func() ([]string, error) { return m.spotlight, nil },
		Library:      m.library,
	}
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestRemovesOlderVersionsOfAnInstalledIDEOfAnyVendor(t *testing.T) {
	m := newMachine(t)
	installPlatform(t, m.apps, "GoLand.app", "JetBrains", "GoLand2026.2")
	installPlatform(t, m.apps, "Android Studio.app", "Google", "AndroidStudio2025.3.2")
	installPlatform(t, m.apps, "DevEco-Studio.app", "Huawei", "DevEcoStudio5.1")
	support := m.folder("Application Support", "JetBrains")
	logs := m.folder("Logs", "Google")
	huawei := m.folder("Caches", "Huawei")
	old := data(t, support, "GoLand2025.3")
	current := data(t, support, "GoLand2026.2")
	oldLogs := data(t, logs, "AndroidStudio2025.3.1")
	currentLogs := data(t, logs, "AndroidStudio2025.3.2")
	oldHuawei := data(t, huawei, "DevEcoStudio5.0")
	data(t, huawei, "DevEcoStudio5.1")

	r, stdout, stderr := run(m.step(), false)

	assert.Empty(t, stderr)
	assert.NoDirExists(t, old)
	assert.NoDirExists(t, oldLogs)
	assert.NoDirExists(t, oldHuawei)
	assert.DirExists(t, current)
	assert.DirExists(t, currentLogs)
	assert.Equal(t, 3, r.Count)
	assert.Contains(t, stdout, "IDE      data of older versions of installed IDEs\n")
}

func TestKeepsEveryInstalledVersion(t *testing.T) {
	m := newMachine(t)
	installPlatform(t, m.apps, "GoLand.app", "JetBrains", "GoLand2026.2")
	installPlatform(t, m.apps, "JetBrains Toolbox/GoLand 2025.3.app", "JetBrains", "GoLand2025.3")
	support := m.folder("Application Support", "JetBrains")
	older := data(t, support, "GoLand2025.1")
	installed := data(t, support, "GoLand2025.3")
	data(t, support, "GoLand2026.2")

	r, _, _ := run(m.step(), false)

	assert.NoDirExists(t, older)
	assert.DirExists(t, installed)
	assert.Equal(t, 1, r.Count)
}

func TestKeepsAVersionInstalledAnywhereSpotlightOrToolboxSees(t *testing.T) {
	m := newMachine(t)
	installPlatform(t, m.apps, "GoLand.app", "JetBrains", "GoLand2026.2")
	downloads := filepath.Join(m.root, "Downloads")
	installPlatform(t, downloads, "GoLand 2025.1.app", "JetBrains", "GoLand2025.1")
	m.spotlight = append(m.spotlight, filepath.Join(downloads, "GoLand 2025.1.app"))
	installPlatform(t, filepath.Join(m.toolbox, "GoLand", "ch-0", "252.1"), "GoLand.app", "JetBrains", "GoLand2025.2")
	support := m.folder("Application Support", "JetBrains")
	inDownloads := data(t, support, "GoLand2025.1")
	inToolbox := data(t, support, "GoLand2025.2")
	nowhere := data(t, support, "GoLand2024.3")
	data(t, support, "GoLand2026.2")

	r, _, stderr := run(m.step(), false)

	assert.Empty(t, stderr)
	assert.DirExists(t, inDownloads)
	assert.DirExists(t, inToolbox)
	assert.NoDirExists(t, nowhere)
	assert.Equal(t, 1, r.Count)
}

func TestSpotlightFailureRemovesNothingAndFails(t *testing.T) {
	m := newMachine(t)
	installPlatform(t, m.apps, "GoLand.app", "JetBrains", "GoLand2026.2")
	support := m.folder("Application Support", "JetBrains")
	old := data(t, support, "GoLand2025.3")
	data(t, support, "GoLand2026.2")
	s := m.step()
	s.Spotlight = func() ([]string, error) { return nil, errors.New("mdfind: exit status 1") }

	r, _, _ := run(s, false)

	assert.DirExists(t, old)
	assert.Zero(t, r.Count)
	assert.Len(t, r.Failures, 1)
}

func TestToolboxAndOtherAppsWithoutIDEFieldsAreNotIDEs(t *testing.T) {
	m := newMachine(t)
	installPlatform(t, m.apps, "GoLand.app", "JetBrains", "GoLand2026.2")
	writeFile(t, filepath.Join(m.apps, "JetBrains Toolbox.app", "Contents", "Resources", "product-info.json"), `{"name": "Toolbox"}`)
	support := m.folder("Application Support", "JetBrains")
	old := data(t, support, "GoLand2025.3")
	data(t, support, "GoLand2026.2")

	r, _, stderr := run(m.step(), false)

	assert.Empty(t, stderr)
	assert.NoDirExists(t, old)
	assert.Equal(t, 1, r.Count)
}

func TestKeepsTheOldFolderUntilTheNewVersionHasMadeItsOwn(t *testing.T) {
	m := newMachine(t)
	installPlatform(t, m.apps, "GoLand.app", "JetBrains", "GoLand2026.2")
	old := data(t, m.folder("Application Support", "JetBrains"), "GoLand2025.3")

	r, _, stderr := run(m.step(), false)

	assert.DirExists(t, old)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+old+": GoLand2026.2 has not made its own folder here yet")
}

func TestKeepsNewerVersionsAndProductsNotInstalled(t *testing.T) {
	m := newMachine(t)
	installPlatform(t, m.apps, "PyCharm.app", "JetBrains", "PyCharm2025.3")
	support := m.folder("Application Support", "JetBrains")
	data(t, support, "PyCharm2025.3")
	newer := data(t, support, "PyCharm2026.1")
	other := data(t, support, "IntelliJIdea2024.1")
	unversioned := data(t, support, "Pycharm")

	r, _, stderr := run(m.step(), false)

	assert.DirExists(t, newer)
	assert.DirExists(t, other)
	assert.DirExists(t, unversioned)
	assert.Zero(t, r.Count)
	assert.Empty(t, stderr)
}

func TestRemovesTheCacheOfEditorBuildsNoLongerInstalled(t *testing.T) {
	m := newMachine(t)
	installEditor(t, m.apps, "Visual Studio Code.app", "Code", "6d62b1d5")
	installEditor(t, m.apps, "Cursor.app", "Cursor", "c0ffee01")
	cached := filepath.Join(m.library, "Application Support", "Code", "CachedData")
	old := data(t, cached, "08d4889f")
	current := data(t, cached, "6d62b1d5")
	cursorCached := filepath.Join(m.library, "Application Support", "Cursor", "CachedData")
	cursorOld := data(t, cursorCached, "deadbeef")
	data(t, cursorCached, "c0ffee01")
	vsix := data(t, filepath.Join(m.library, "Application Support", "Code"), "CachedExtensionVSIXs")

	r, _, stderr := run(m.step(), false)

	assert.Empty(t, stderr)
	assert.NoDirExists(t, old)
	assert.NoDirExists(t, cursorOld)
	assert.DirExists(t, current)
	assert.DirExists(t, vsix)
	assert.Equal(t, 2, r.Count)
}

func TestKeepsTheCacheOfEveryInstalledBuildOfOneEditor(t *testing.T) {
	m := newMachine(t)
	installEditor(t, m.apps, "Visual Studio Code.app", "Code", "aaaa")
	installEditor(t, m.apps, "Old/Visual Studio Code.app", "Code", "bbbb")
	cached := filepath.Join(m.library, "Application Support", "Code", "CachedData")
	first := data(t, cached, "aaaa")
	second := data(t, cached, "bbbb")
	gone := data(t, cached, "cccc")

	r, _, stderr := run(m.step(), false)

	assert.Empty(t, stderr)
	assert.DirExists(t, first)
	assert.DirExists(t, second)
	assert.NoDirExists(t, gone)
	assert.Equal(t, 1, r.Count)
}

func TestKeepsEditorCacheUntilTheInstalledBuildHasMadeItsOwn(t *testing.T) {
	m := newMachine(t)
	installEditor(t, m.apps, "Visual Studio Code.app", "Code", "6d62b1d5")
	cached := filepath.Join(m.library, "Application Support", "Code", "CachedData")
	old := data(t, cached, "08d4889f")

	r, _, stderr := run(m.step(), false)

	assert.DirExists(t, old)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+cached+": Visual Studio Code.app has not made its own cache here yet")
}

func TestOtherElectronAppsAreNotEditors(t *testing.T) {
	m := newMachine(t)
	writeFile(t, filepath.Join(m.apps, "Chat.app", "Contents", "Resources", "app", "product.json"), `{"name": "chat"}`)
	cached := filepath.Join(m.library, "Application Support", "Chat", "CachedData")
	kept := data(t, cached, "abc")

	r, stdout, stderr := run(m.step(), false)

	assert.DirExists(t, kept)
	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestDryRunRemovesNothing(t *testing.T) {
	m := newMachine(t)
	installPlatform(t, m.apps, "GoLand.app", "JetBrains", "GoLand2026.2")
	support := m.folder("Application Support", "JetBrains")
	old := data(t, support, "GoLand2025.3")
	data(t, support, "GoLand2026.2")

	r, _, _ := run(m.step(), true)

	assert.DirExists(t, old)
	assert.Equal(t, 1, r.Count)
}

func TestUnreadableOrOddDescriptionRemovesNothingAndFails(t *testing.T) {
	for name, info := range map[string]string{
		"broken JSON":          `{`,
		"vendor with a slash":  `{"productVendor": "../x", "dataDirectoryName": "GoLand2026.2"}`,
		"unversioned data dir": `{"productVendor": "JetBrains", "dataDirectoryName": "GoLand"}`,
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			installPlatform(t, m.apps, "GoLand.app", "JetBrains", "GoLand2026.2")
			writeFile(t, filepath.Join(m.apps, "Odd.app", "Contents", "Resources", "product-info.json"), info)
			support := m.folder("Application Support", "JetBrains")
			old := data(t, support, "GoLand2025.3")
			data(t, support, "GoLand2026.2")

			r, _, _ := run(m.step(), false)

			assert.DirExists(t, old)
			assert.Zero(t, r.Count)
			assert.Len(t, r.Failures, 1)
		})
	}
}

func TestNoIDEIsQuiet(t *testing.T) {
	m := newMachine(t)
	data(t, m.folder("Application Support", "JetBrains"), "GoLand2025.3")

	r, stdout, stderr := run(m.step(), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}
