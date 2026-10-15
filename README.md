# diskcleaner

[![CI](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml/badge.svg)](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml)

Frees disk space on a Mac by deleting what builds and tools recreate on demand.

## What gets deleted

| Step | Deletes |
|---|---|
| `simulator-caches` | Core ML caches of iOS simulators |
| `xcode-derived-data` | Xcode DerivedData |
| `swiftpm-cache` | the SwiftPM package cache |
| `go-cache` | Go module and build caches |
| `docker-images` | Docker images no container uses |
| `gradle-caches` | `~/.gradle/caches` |
| `claude-logs` | Claude Code session logs older than 30 days |

What makes each step skip is listed for it below. After a cleanup the first
build is slower.

## Install

```sh
go install github.com/botforge-pro/diskcleaner@latest
```

The binary lands in `$(go env GOBIN)`, or in `$(go env GOPATH)/bin` (usually
`~/go/bin`) when `GOBIN` is not set; put that directory on your `PATH`.

## Usage

```sh
diskcleaner run STEP --dry-run         # list what STEP would remove
diskcleaner run STEP                   # remove it
diskcleaner run STEP --report FILE     # also write the totals to FILE
diskcleaner check STEP...              # check the settings of these steps
diskcleaner version
```

Output lines:

- `REMOVE` / `WOULD REMOVE`: a path and its size;
- `RUN` / `WOULD RUN`: a cleanup command and the size it works on;
- `SKIPPED`: kept on purpose, with the reason;
- `FAILED`: a failure; any failure makes the exit status 1.

The report file holds `count N` (paths removed or cleaned), `kb N` (KiB freed)
and a `failure ...` line per failure.

A wrong number setting (a timeout, a day count) or a missing home directory
makes `run` and `check` exit 2 before anything is touched. Other settings, such
as `pgrep -f` patterns, fail
only when the step runs.

## Steps in detail

A build started after a step checked for running tools is not detected.

### simulator-caches

- Removes `com.apple.e5rt.e5bundlecache` under `Library/Caches` of each iOS
  simulator in `~/Library/Developer/CoreSimulator/Devices`: the Core ML models
  compiled for its apps. One app has piled up 19 GB of them.
- The app compiles its model again on its next launch. App data is not touched.
- Skips all simulators while Simulator, DeviceHub, `simctl` or `xcodebuild` run.
- Skips a simulator that is not Shutdown or has a running process; one whose
  state `plutil` cannot read is a failure.
- Settings: `DISKCLEANER_SIMULATOR_DEVICES`, `DISKCLEANER_SIMULATOR_PROCESSES`.

### xcode-derived-data

- Removes every directory in `~/Library/Developer/Xcode/DerivedData`. Files and
  symbolic links in it stay; if DerivedData is itself a link, its target's
  directories go and the link stays.
- The next build is slower.
- Skips while Xcode (any `Xcode*.app`), `xcodebuild` or `XCBBuildService` run.
- Settings: `DISKCLEANER_XCODE_DERIVED_DATA`, `DISKCLEANER_XCODE_PROCESSES`.

### swiftpm-cache

- Removes `~/Library/Caches/org.swift.swiftpm`, the downloaded package
  dependencies; if it is a symbolic link, only the link.
- The next package resolution downloads them again.
- Skips while `swift build`, `package`, `test`, `run`, `xcodebuild`, Xcode or
  `XCBBuildService` run.
- Settings: `DISKCLEANER_SWIFTPM_CACHE`, `DISKCLEANER_SWIFT_PROCESSES`.

### go-cache

- Runs `go clean -modcache` and `go clean -cache`.
- The next build downloads modules and compiles everything again.
- Does nothing without `go` on the `PATH`.
- Skips while `go build`, `test`, `run`, `install`, `get`, `mod`, `generate`,
  `vet` or `list` run.
- Settings: `DISKCLEANER_GO_PROCESSES`.

### docker-images

- Runs `docker image prune --all --force` when `docker system df` reports
  images to free. Volumes are never touched.
- A removed image is pulled or built again when needed.
- Does nothing without `docker` on the `PATH`; skips when `docker info` fails
  and Docker Desktop is not running.
- Docker that does not answer within `DISKCLEANER_DOCKER_TIMEOUT` seconds (30),
  or a prune longer than `DISKCLEANER_DOCKER_PRUNE_TIMEOUT` (900), is a
  failure: the `docker` command and any process it started are killed; Docker
  Desktop itself is left alone.
- The space counted is how much Docker Desktop's data directory shrank, which
  can lag behind what Docker reports.
- Settings: `DISKCLEANER_DOCKER_DATA`, `DISKCLEANER_DOCKER_PROCESS`.

### gradle-caches

- Removes `$GRADLE_USER_HOME/caches` (default `~/.gradle/caches`).
- The next Gradle build downloads and builds them again.
- Skips while `gradlew`, `gradle`, Android Studio or IntelliJ IDEA run, or while
  a Gradle or Kotlin daemon used CPU during a two-second check.
- Stops idle daemons with `SIGTERM` first; daemons alive ten seconds later keep
  the cache and are a failure.
- Settings: `DISKCLEANER_GRADLE_DAEMONS`, `DISKCLEANER_GRADLE_CLIENTS`.

### claude-logs

- Removes `*.jsonl` session logs, agents' included, under `~/.claude/projects`
  untouched for more than `DISKCLEANER_CLAUDE_LOG_DAYS` whole days (30).
- Such a session can no longer be resumed.
- A log Git tracks, or one Git cannot tell about, is kept.
- Settings: `DISKCLEANER_CLAUDE_PROJECTS`, `DISKCLEANER_CLAUDE_LOG_DAYS`.

## Lines of Code

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
  <img alt="Lines of Code graph" src="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
</picture>
