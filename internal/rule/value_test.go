package rule

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const archivePlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>ApplicationProperties</key>
	<dict>
		<key>Architectures</key>
		<array>
			<string>arm64</string>
		</array>
		<key>CFBundleIdentifier</key>
		<string>com.example.app</string>
	</dict>
	<key>ArchiveVersion</key>
	<integer>2</integer>
	<key>CreationDate</key>
	<date>2026-11-02T12:00:00Z</date>
	<key>Distributions</key>
	<array>
		<dict>
			<key>CreationDate</key>
			<date>2020-01-01T00:00:00Z</date>
			<key>Name</key>
			<string>Other</string>
		</dict>
	</array>
	<key>Name</key>
	<string>App</string>
</dict>
</plist>
`

func TestADictInAListNeverStandsInForTheFileItself(t *testing.T) {
	folder := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(folder, "Info.plist"), []byte(archivePlist), 0o644))

	app, err := Value{File: "Info.plist", Key: []string{"ApplicationProperties.CFBundleIdentifier", "Name"}}.read(folder)
	require.NoError(t, err)
	made, err := Value{File: "Info.plist", Key: []string{"CreationDate"}}.time(folder)
	require.NoError(t, err)
	name, err := Value{File: "Info.plist", Key: []string{"Name"}}.read(folder)
	require.NoError(t, err)
	version, err := Value{File: "Info.plist", Key: []string{"ArchiveVersion"}}.read(folder)
	require.NoError(t, err)

	assert.Equal(t, "com.example.app", app)
	assert.Equal(t, time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), made)
	assert.Equal(t, "App", name)
	assert.Equal(t, "2", version)
}

func TestAValueNoKeyGivesIsAnError(t *testing.T) {
	folder := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(folder, "Info.plist"), []byte(archivePlist), 0o644))

	_, err := Value{File: "Info.plist", Key: []string{"Missing", "AlsoMissing"}}.read(folder)

	assert.EqualError(t, err, "no Missing and no AlsoMissing")
}
