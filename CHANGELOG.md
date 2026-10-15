# Changelog

All notable changes to this project are documented in this file. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/); while the version
is 0.x, commands and output may still change.

## [0.5.0] - 2026-10-15

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.5.0`; the
commands added in earlier versions work as in 0.4.0.

### Added

- `diskcleaner run docker-images` deletes Docker images that no container uses,
  by running `docker image prune --all --force`; volumes, which hold data, are
  never touched. It runs only when `docker system df` reports space such images
  could free, and prints a `RUN` line with that size and Docker's own
  `Total reclaimed space` line. A removed image is pulled or built again the
  next time something needs it. If Docker Desktop is not running, it changes
  nothing and prints a `SKIPPED` line with exit status 0; if `docker` is not
  installed, it prints nothing and exits with status 0. A Docker that does not
  answer within `DISKCLEANER_DOCKER_TIMEOUT` seconds (default 30), or a prune
  that takes longer than `DISKCLEANER_DOCKER_PRUNE_TIMEOUT` seconds (default
  900), is stopped and reported as a failure with exit status 1. `--dry-run`
  prints a `WOULD RUN` line and counts the size Docker reports. With
  `--report FILE`, `count` is 1 when the prune succeeded and `kb` is how much
  the Docker Desktop data folder shrank, which can be less than Docker reports
  because Docker Desktop returns space to macOS gradually.
- `diskcleaner check STEP...` checks the timeouts of the named steps and that
  the home folder can be found, and changes nothing: it exits with status 0
  when they are valid, and otherwise prints what is wrong and exits with status
  2. A script that runs several steps can call it first, so that a mistyped
  timeout stops it before anything is deleted. Other settings, such as the
  process patterns, are not checked ahead.

### Changed

- `diskcleaner run` now exits with status 2, printing the reason, when a
  timeout of the step is not a whole number of seconds above zero or is too
  large to wait for; nothing is touched then. When the home folder cannot be
  determined, it now says so instead of reporting an unknown step.

## [0.4.0] - 2026-10-15

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.4.0`; the
commands added in earlier versions work as in 0.3.1.

### Added

- `diskcleaner run go-cache` empties the two caches where Go keeps downloaded
  modules and compiled packages (the module cache and the build cache). It
  does so by running `go clean -modcache` and `go clean -cache`, each printed as
  a `RUN` line with the size of the folder it empties. The next Go build
  downloads and compiles them again, which makes it slower and needs a network
  connection. If `go build`, `go test` or another Go command that uses these
  caches is running when the command starts, it changes nothing and prints the
  reason as a `SKIPPED` line; a skip is not a failure and the exit status stays
  0. If `go` is not installed, the command prints nothing and exits with status
  0. `--dry-run` prints `WOULD RUN` lines instead and runs nothing. With
  `--report FILE`, `count` is the number of caches emptied (at most 2) and `kb`
  the space freed; if a `go clean` fails, its cache is not counted, only the
  space it did free is, and the failure makes the exit status 1.

## [0.3.1] - 2026-10-15

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.3.1`.

### Fixed

- `diskcleaner run swiftpm-cache` no longer deletes the Swift Package Manager
  cache while the Xcode app is open: 0.3.0 noticed only command-line builds, so
  a build started in Xcode could lose the packages it was reading. Now the
  command also skips while Xcode or its build service (`XCBBuildService`) is
  running, so quit Xcode before running it. The `SKIPPED` line now reads
  `a Swift package build, xcodebuild or Xcode is running`.
- `diskcleaner run swiftpm-cache` and `diskcleaner run xcode-derived-data` now
  recognise Xcode installed under another name, such as `Xcode-beta.app` or
  `Xcode_26.1.app`; 0.3.0 recognised only `Xcode.app`.

## [0.3.0] - 2026-10-14

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.3.0`; nothing
else changes for existing users.

### Added

- `diskcleaner run swiftpm-cache` deletes the Swift Package Manager cache
  (`~/Library/Caches/org.swift.swiftpm`), where Swift keeps downloaded copies of
  the packages your projects depend on. The next build downloads the packages it
  needs again, which makes it slower and needs a network connection. If a Swift
  package command (`swift build`, `swift package`, `swift test`, `swift run`) or
  `xcodebuild` is running when the command starts, it deletes nothing and prints
  the reason as a `SKIPPED` line; a skip is not a failure and the exit status
  stays 0. A build started while the command is deleting is not noticed, and
  neither is a build run from the Xcode app itself, so close Xcode first. If the
  cache folder is a symbolic link, only the link is deleted, not the folder it
  points to. `--dry-run` and `--report FILE` work as described under 0.1.0; in
  the report, `count` is 1 when the cache was deleted and 0 otherwise.

## [0.2.0] - 2026-10-14

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.2.0`; nothing
else changes for existing users.

### Added

- `diskcleaner run xcode-derived-data` deletes every folder in Xcode's
  DerivedData (`~/Library/Developer/Xcode/DerivedData`), where Xcode keeps the
  results of building and analysing projects. Files directly in that folder are
  kept. The next build in Xcode creates the folders again, which makes that build
  slower. If Xcode or a command-line `xcodebuild` is running when the command
  starts, it deletes nothing and prints the reason as a `SKIPPED` line; a skip is
  not a failure and the exit status stays 0. Quit Xcode first: a build started
  while the command is deleting is not noticed. If DerivedData is a symbolic
  link, the folders in the directory it points to are deleted and the link
  stays. `--dry-run` and `--report FILE` work as for `simulator-caches`;
  `count N` is the number of deleted folders.

## [0.1.0] - 2026-10-14

The first release of diskcleaner, a macOS command-line tool for freeing disk
space by removing files that development tools create and recreate on their own;
this release removes one kind of them. Install it with a Go toolchain:
`go install github.com/botforge-pro/diskcleaner@v0.1.0`.

### Added

- `diskcleaner run simulator-caches` frees space taken by iOS simulators in
  Xcode. When an app that uses machine-learning models (Apple's Core ML) runs in
  a simulator, the system compiles those models and keeps the compiled copies,
  which can grow to many gigabytes. The command deletes these compiled copies,
  one folder per app or service in each simulator, and nothing else. It is
  safe: the app compiles its models again on its next launch, which only makes
  that launch slower. A simulator that is running is
  left alone, and so are all simulators while Xcode is building or the Simulator
  app is open; each skip is printed with its reason.
- `diskcleaner run simulator-caches --dry-run` prints what would be deleted
  without deleting anything.
- `diskcleaner run simulator-caches --report FILE` also writes the totals to
  `FILE`, replacing its contents, as plain text lines: `count N` for the number
  of deleted folders, `kb N` for the space freed in units of 1024 bytes, and
  `failure` followed by the error message for each problem, such as a folder
  that could not be deleted or a simulator whose state could not be read. With
  `--dry-run` the totals are what would be deleted. Any failure makes the exit
  status 1.
- `diskcleaner version` prints the installed version.

[0.5.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.5.0
[0.4.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.4.0
[0.3.1]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.3.1
[0.3.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.3.0
[0.2.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.2.0
[0.1.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.1.0
