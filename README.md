# diskcleaner

[![CI](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml/badge.svg)](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml)

A tool for freeing disk space on a developer machine by removing what builds and
tools leave behind and recreate on demand. It runs on macOS.

## Install

```sh
go install github.com/botforge-pro/diskcleaner@latest
```

This puts the `diskcleaner` binary in `$(go env GOBIN)`, or in
`$(go env GOPATH)/bin` (usually `~/go/bin`) when `GOBIN` is not set; make sure
that directory is on your `PATH`.

## Usage

```sh
diskcleaner version                    # print the installed version
diskcleaner run STEP --dry-run         # list what STEP would remove
diskcleaner run STEP                   # remove it
diskcleaner run STEP --report FILE     # also write the totals to FILE
diskcleaner check STEP...              # check the settings of these steps
```

The steps are `simulator-caches`, `xcode-derived-data`, `swiftpm-cache`,
`go-cache` and `docker-images`, described below. A timeout that is not a whole
number of seconds above zero, or a home directory that cannot be found, makes
`run` and `check` print what is wrong and exit with status 2 before anything is
touched; `check` lets a script that runs several steps find that out before the
first one. Other settings, such as a `pgrep -f` pattern, are not checked ahead:
a wrong one shows up as a failure of its step.

Each removal is printed as `REMOVE` with its size (`WOULD REMOVE` in a dry run),
a cleanup command as `RUN` with the size of the directory it cleans
(`WOULD RUN`), a path kept on purpose as `SKIPPED` with the reason, and a
failure as `FAILED`; any failure makes the exit status 1. The report file holds
`count N` and `kb N` lines with the number of paths removed or cleaned and the
kilobytes freed, and a `failure ...` line for each failure.

### simulator-caches

Removes the Core ML models that iOS simulators compiled for their apps and
services: every `com.apple.e5rt.e5bundlecache` directory inside a
`Library/Caches` under `~/Library/Developer/CoreSimulator/Devices/*/data`, with
the bundles a compilation that died left in it. They can pile up: we have seen
one app collect 19 GB of them in a single simulator. The next launch of the
app compiles its model again. App data such as `Application Support`,
`Documents` and databases is not touched.

All simulators are skipped while Simulator, DeviceHub, `simctl` or `xcodebuild`
is running. Right before each cache goes, its simulator is checked again: one
whose `device.plist` does not give it the Shutdown state, or that has a process
with its directory in the command line, is skipped, and one whose state
`plutil` cannot read is kept and reported as `FAILED`. A simulator booted in the
seconds between that check and the removal is not detected, and Core ML calls in
it can then fail with an E5RT error.

`DISKCLEANER_SIMULATOR_DEVICES` replaces the devices directory and
`DISKCLEANER_SIMULATOR_PROCESSES` the `pgrep -f` pattern of the programs that
make it skip all simulators.

### xcode-derived-data

Removes every directory in `~/Library/Developer/Xcode/DerivedData`: the build
output, indexes and module caches Xcode keeps there. The next build recreates
them, including the incremental build cache, so it is slower than usual. Files
and symbolic links directly in DerivedData stay. If DerivedData itself is a
symbolic link, the directories in its target are removed and the link stays.

It is skipped while Xcode (any `Xcode*.app`, such as `Xcode-beta.app`),
`xcodebuild` or the build service `XCBBuildService` is running. A build started
after that check is not detected.

`DISKCLEANER_XCODE_DERIVED_DATA` replaces the DerivedData directory and
`DISKCLEANER_XCODE_PROCESSES` the `pgrep -f` pattern of the programs that make
it skip.

### swiftpm-cache

Removes the Swift Package Manager cache `~/Library/Caches/org.swift.swiftpm`:
the clones and downloads of package dependencies. The next build that resolves
packages downloads them again. If the cache is a symbolic link, only the link
is removed, not what it points to.

It is skipped while `swift build`, `swift package`, `swift test`, `swift run`,
`xcodebuild`, Xcode (any `Xcode*.app`) or its build service `XCBBuildService`
is running. A build started after that check is not detected.

`DISKCLEANER_SWIFTPM_CACHE` replaces the cache directory and
`DISKCLEANER_SWIFT_PROCESSES` the `pgrep -f` pattern of the programs that make
it skip.

### go-cache

Runs `go clean -modcache` and `go clean -cache`, which empty the Go module cache
(`go env GOMODCACHE`) and build cache (`go env GOCACHE`). The next build
downloads the modules it needs and compiles everything again. Without a `go`
command on the `PATH` the step does nothing.

It is skipped while `go build`, `go test`, `go run`, `go install`, `go get`,
`go mod`, `go generate`, `go vet` or `go list` is running. A command started
after that check is not detected.

`DISKCLEANER_GO_PROCESSES` replaces the `pgrep -f` pattern of the commands that
make it skip.

### docker-images

Runs `docker image prune --all --force` when `docker system df` reports space
that images not used by any container could free. Volumes are never touched:
they hold data. The next build or run that needs a removed image pulls or
builds it again. Docker Desktop returns the space to macOS gradually, so the
space counted, measured on its data directory, can be smaller than what Docker
reports as reclaimed. Without a `docker` command on the `PATH` the step does
nothing.

It assumes Docker Desktop: when `docker info` fails and no Docker Desktop
backend process is running, Docker is taken to be off and the step is skipped.
A failing `docker info` while the backend runs is a failure.
`docker info` and `docker system df` that do not answer within
`DISKCLEANER_DOCKER_TIMEOUT` seconds (default 30), or a prune that does not
finish within `DISKCLEANER_DOCKER_PRUNE_TIMEOUT` seconds (default 900), are
stopped and reported as failures.

`DISKCLEANER_DOCKER_DATA` replaces the Docker Desktop data directory
(`~/Library/Containers/com.docker.docker`) and `DISKCLEANER_DOCKER_PROCESS` the
`pgrep -f` pattern of its backend process.

## Lines of Code

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
  <img alt="Lines of Code graph" src="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
</picture>
