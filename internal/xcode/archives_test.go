package xcode

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

const archivePlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>ApplicationProperties</key>
	<dict>
		<key>ApplicationPath</key>
		<string>Applications/App.app</string>
		<key>Architectures</key>
		<array>
			<string>arm64</string>
		</array>
		<key>CFBundleIdentifier</key>
		<string>%s</string>
	</dict>
	<key>ArchiveVersion</key>
	<integer>2</integer>
	<key>CreationDate</key>
	<date>%s</date>
	<key>Name</key>
	<string>App</string>
</dict>
</plist>
`

const (
	oldArchive    = "2026-09-25/App 25.09.2026.xcarchive"
	olderArchive  = "App-20260905-120000.xcarchive"
	middleArchive = "2026-10-01/App 01.10.2026.xcarchive"
	recentArchive = "2026-11-01/App 01.11.2026.xcarchive"
	loneArchive   = "2026-08-06/Other 06.08.2026.xcarchive"
)

func writeArchive(t *testing.T, path, bundle string, age time.Duration) {
	writeArchivePlist(t, path, fmt.Sprintf(archivePlist, bundle, now.Add(-age).Format(time.RFC3339)))
}

func writeArchivePlist(t *testing.T, path, plist string) {
	require.NoError(t, os.MkdirAll(filepath.Join(path, "Products"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "Products", "App.ipa"), make([]byte, 4096), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(path, "Info.plist"), []byte(plist), 0o644))
}

func newArchives(t *testing.T) string {
	folder, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	writeArchive(t, filepath.Join(folder, oldArchive), "com.example.app", 40*day)
	writeArchive(t, filepath.Join(folder, olderArchive), "com.example.app", 60*day)
	writeArchive(t, filepath.Join(folder, middleArchive), "com.example.app", 34*day)
	writeArchive(t, filepath.Join(folder, recentArchive), "com.example.app", 3*day)
	writeArchive(t, filepath.Join(folder, loneArchive), "com.example.other", 90*day)
	return folder
}

func archives(folder string) Archives {
	return Archives{
		Folder: folder, Days: 14, Kept: 1, Now: func() time.Time { return now },
		Processes: "xcode", Running: idle,
	}
}

func runArchives(s Archives, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestRemovesOldArchivesAndKeepsTheNewestOfEachApp(t *testing.T) {
	folder := newArchives(t)

	r, stdout, _ := runArchives(archives(folder), false)

	assert.NoDirExists(t, filepath.Join(folder, oldArchive))
	assert.NoDirExists(t, filepath.Join(folder, olderArchive))
	assert.NoDirExists(t, filepath.Join(folder, middleArchive))
	assert.DirExists(t, filepath.Join(folder, recentArchive))
	assert.DirExists(t, filepath.Join(folder, loneArchive))
	assert.Equal(t, 3, r.Count)
	assert.Contains(t, stdout, "ARCHIVES "+folder+"\n")
}

func TestKeepsAsManyNewestArchivesAsSet(t *testing.T) {
	folder := newArchives(t)
	s := archives(folder)
	s.Kept = 3

	r, _, _ := runArchives(s, false)

	assert.DirExists(t, filepath.Join(folder, recentArchive))
	assert.DirExists(t, filepath.Join(folder, middleArchive))
	assert.DirExists(t, filepath.Join(folder, oldArchive))
	assert.NoDirExists(t, filepath.Join(folder, olderArchive))
	assert.Equal(t, 1, r.Count)
}

func TestKeepsArchivesYoungerThanTheSetDays(t *testing.T) {
	folder := newArchives(t)
	s := archives(folder)
	s.Days = 45

	r, _, _ := runArchives(s, false)

	assert.DirExists(t, filepath.Join(folder, oldArchive))
	assert.DirExists(t, filepath.Join(folder, middleArchive))
	assert.NoDirExists(t, filepath.Join(folder, olderArchive))
	assert.Equal(t, 1, r.Count)
}

func TestArchiveDryRunRemovesNothing(t *testing.T) {
	folder := newArchives(t)

	r, _, _ := runArchives(archives(folder), true)

	assert.DirExists(t, filepath.Join(folder, oldArchive))
	assert.Equal(t, 3, r.Count)
}

func TestKeepsArchivesWhileXcodeRuns(t *testing.T) {
	folder := newArchives(t)
	s := archives(folder)
	s.Running = func(string) (bool, error) { return true, nil }

	r, _, stderr := runArchives(s, false)

	assert.DirExists(t, filepath.Join(folder, oldArchive))
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+folder+": Xcode or an Xcode build is running")
}

func TestAnArchiveWithoutItsInformationStaysAndFails(t *testing.T) {
	folder := newArchives(t)
	broken := filepath.Join(folder, "2026-09-01", "Broken.xcarchive")
	require.NoError(t, os.MkdirAll(broken, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(broken, "Info.plist"), []byte("<plist><dict>"), 0o644))

	r, _, stderr := runArchives(archives(folder), false)

	assert.DirExists(t, broken)
	assert.Len(t, r.Failures, 1)
	assert.Contains(t, stderr, broken)
	assert.Equal(t, 3, r.Count)
}

func TestArchivesInFoldersNotNamedByADateStay(t *testing.T) {
	folder := newArchives(t)
	moved := []string{
		filepath.Join(folder, "Keep", "App 01.01.2026.xcarchive"),
		filepath.Join(folder, "Released", "App 02.01.2026.xcarchive"),
	}
	for _, path := range moved {
		writeArchive(t, path, "com.example.app", 300*day)
	}

	r, _, _ := runArchives(archives(folder), false)

	for _, path := range moved {
		assert.DirExists(t, path)
	}
	assert.Equal(t, 3, r.Count)
}

func TestADictInAListNeverStandsInForTheArchiveItself(t *testing.T) {
	folder := newArchives(t)
	path := filepath.Join(folder, "2026-11-02", "App 02.11.2026.xcarchive")
	plist := strings.Replace(
		fmt.Sprintf(archivePlist, "com.example.app", now.Add(-2*day).Format(time.RFC3339)),
		"\t<key>Name</key>",
		"\t<key>Distributions</key>\n\t<array>\n\t\t<dict>\n\t\t\t<key>CreationDate</key>\n"+
			"\t\t\t<date>2020-01-01T00:00:00Z</date>\n\t\t\t<key>Name</key>\n\t\t\t<string>Other</string>\n"+
			"\t\t</dict>\n\t</array>\n\t<key>Name</key>",
		1)
	writeArchivePlist(t, path, plist)

	found, err := readArchive(path)

	require.NoError(t, err)
	assert.Equal(t, "com.example.app", found.app)
	assert.Equal(t, now.Add(-2*day), found.created)
}

func TestMissingArchivesFolderIsQuiet(t *testing.T) {
	r, stdout, stderr := runArchives(archives(filepath.Join(t.TempDir(), "absent")), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestArchiveSettingsComeFromTheEnvironment(t *testing.T) {
	t.Setenv("DISKCLEANER_XCODE_ARCHIVE_DAYS", "30")
	t.Setenv("DISKCLEANER_XCODE_ARCHIVE_KEEP", "2")

	s, err := DefaultArchives("/home")

	require.NoError(t, err)
	assert.Equal(t, 30, s.Days)
	assert.Equal(t, 2, s.Kept)
	assert.Equal(t, "/home/Library/Developer/Xcode/Archives", s.Folder)
}

func TestArchiveSettingsDefaultToTwoWeeksAndOneArchive(t *testing.T) {
	s, err := DefaultArchives("/home")

	require.NoError(t, err)
	assert.Equal(t, 14, s.Days)
	assert.Equal(t, 1, s.Kept)
}

func TestAWrongArchiveSettingIsAnError(t *testing.T) {
	for _, name := range []string{"DISKCLEANER_XCODE_ARCHIVE_DAYS", "DISKCLEANER_XCODE_ARCHIVE_KEEP"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "0")

			_, err := DefaultArchives("/home")

			assert.ErrorContains(t, err, name)
		})
	}
}
