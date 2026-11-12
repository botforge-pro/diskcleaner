package main

import (
	"github.com/botforge-pro/diskcleaner/internal/clones"
	"github.com/botforge-pro/diskcleaner/internal/config"
	"github.com/botforge-pro/diskcleaner/internal/docker"
	"github.com/botforge-pro/diskcleaner/internal/gocache"
	"github.com/botforge-pro/diskcleaner/internal/gradle"
	"github.com/botforge-pro/diskcleaner/internal/homebrew"
	"github.com/botforge-pro/diskcleaner/internal/ide"
	"github.com/botforge-pro/diskcleaner/internal/leftovers"
	"github.com/botforge-pro/diskcleaner/internal/projects"
	"github.com/botforge-pro/diskcleaner/internal/scratchpads"
	"github.com/botforge-pro/diskcleaner/internal/sessionlogs"
	"github.com/botforge-pro/diskcleaner/internal/simulator"
	"github.com/botforge-pro/diskcleaner/internal/swiftpm"
	"github.com/botforge-pro/diskcleaner/internal/xcode"
)

type stepInfo struct {
	config.Step
	describes string
	construct func(home string, c *config.Config) (step, error)
}

var catalog = []stepInfo{
	{config.Step{Name: "projects", Reversible: true, Settings: []config.Setting{
		{Key: "git-tmp-min-age", Kind: config.Duration, Default: "1h", Help: "keep temporary files of git fetch and repack written more recently"},
		{Key: "lfs-timeout", Kind: config.Timeout, Default: "10m", Help: "give up on a git lfs prune of one repository that runs longer"},
	}}, "build output, Python caches, node_modules and Git leftovers in your projects", func(_ string, c *config.Config) (step, error) {
		s, err := projects.Default(c.Dirs("project-dirs"), c.Duration("min-age"), c.Duration("projects.git-tmp-min-age"), c.Duration("projects.lfs-timeout"))
		return s, err
	}},
	{config.Step{Name: "gradle-caches", Reversible: true}, "the Gradle caches", func(home string, _ *config.Config) (step, error) {
		return gradle.Default(home), nil
	}},
	{config.Step{Name: "docker-images", Reversible: true, Settings: []config.Setting{
		{Key: "timeout", Kind: config.Timeout, Default: "30s", Help: "give up on a Docker that does not answer within this"},
		{Key: "prune-timeout", Kind: config.Timeout, Default: "15m", Help: "give up on a prune that runs longer"},
	}}, "Docker images not used by any container", func(home string, c *config.Config) (step, error) {
		return docker.Default(home, c.Duration("docker-images.timeout"), c.Duration("docker-images.prune-timeout")), nil
	}},
	{config.Step{Name: "go-cache", Reversible: true}, "the Go module and build caches", func(string, *config.Config) (step, error) {
		return gocache.Default(), nil
	}},
	{config.Step{Name: "homebrew", Settings: []config.Setting{
		{Key: "timeout", Kind: config.Timeout, Default: "15m", Help: "give up on a cleanup that runs longer"},
	}}, "old Homebrew versions and downloads", func(_ string, c *config.Config) (step, error) {
		return homebrew.Default(c.Duration("homebrew.timeout")), nil
	}},
	{config.Step{Name: "swiftpm-cache", Reversible: true}, "the Swift Package Manager cache", func(home string, _ *config.Config) (step, error) {
		return swiftpm.Default(home), nil
	}},
	{config.Step{Name: "xcode-derived-data", Reversible: true}, "Xcode DerivedData", func(home string, _ *config.Config) (step, error) {
		return xcode.Default(home), nil
	}},
	{config.Step{Name: "xcode-archives", Settings: []config.Setting{
		{Key: "min-age", Kind: config.Duration, Default: "15d", Help: "remove archives older than this"},
		{Key: "keep", Kind: config.Count, Default: "1", Help: "keep this many newest archives of each app"},
	}}, "old Xcode archives, the newest of each app kept", func(home string, c *config.Config) (step, error) {
		return xcode.DefaultArchives(home, c.Duration("xcode-archives.min-age"), c.Count("xcode-archives.keep")), nil
	}},
	{config.Step{Name: "temp-leftovers", Reversible: true}, "temporary folders of killed Go builds and of Xcode exports", func(_ string, c *config.Config) (step, error) {
		s, err := leftovers.Default(c.Duration("min-age"))
		return s, err
	}},
	{config.Step{Name: "system-temp", Settings: []config.Setting{
		{Key: "min-size", Kind: config.Size, Default: "1MB", Help: "leave smaller entries"},
	}}, "your files and folders left in /private/tmp", func(_ string, c *config.Config) (step, error) {
		return leftovers.SystemTemp(c.Duration("min-age"), c.Size("system-temp.min-size")), nil
	}},
	{config.Step{Name: "code-sign-clones", Reversible: true}, "copies of Chrome and other apps no running app uses", func(_ string, c *config.Config) (step, error) {
		s, err := clones.Default(c.Duration("min-age"))
		return s, err
	}},
	{config.Step{Name: "ide-old-versions"}, "settings, caches and logs of IDE versions older than the installed one", func(home string, _ *config.Config) (step, error) {
		return ide.Default(home), nil
	}},
	{config.Step{Name: "simulator-caches", Reversible: true}, "Core ML caches of shut-down iOS simulators", func(home string, _ *config.Config) (step, error) {
		return simulator.Default(home), nil
	}},
	{config.Step{Name: "claude-logs", Settings: []config.Setting{
		{Key: "min-age", Kind: config.Duration, Default: "20d", Help: "remove session logs older than this"},
	}}, "old Claude Code session logs", func(home string, c *config.Config) (step, error) {
		return sessionlogs.Claude(home, c.Duration("claude-logs.min-age")), nil
	}},
	{config.Step{Name: "codex-logs", Settings: []config.Setting{
		{Key: "min-age", Kind: config.Duration, Default: "20d", Help: "remove session logs older than this"},
	}}, "old Codex session logs", func(home string, c *config.Config) (step, error) {
		return sessionlogs.Codex(home, c.Duration("codex-logs.min-age")), nil
	}},
	{config.Step{Name: "claude-scratchpads", Reversible: true}, "build output in Claude Code scratchpads", func(string, *config.Config) (step, error) {
		return scratchpads.Default(), nil
	}},
}

func configSteps() []config.Step {
	steps := make([]config.Step, len(catalog))
	for i, info := range catalog {
		steps[i] = info.Step
	}
	return steps
}

var settingsTable = config.NewTable(configSteps())
