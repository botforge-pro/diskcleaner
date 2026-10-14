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
```

The steps are `simulator-caches`, `xcode-derived-data` and `swiftpm-cache`,
described below.

Each removal is printed as `REMOVE` with its size (`WOULD REMOVE` in a dry run),
a path kept on purpose as `SKIPPED` with the reason, and a failure as `FAILED`;
any failure makes the exit status 1. The report file holds `count N` and
`kb N` lines with the number of paths and kilobytes removed, and a
`failure ...` line for each failure.

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

It is skipped while Xcode, `xcodebuild` or the build service `XCBBuildService`
is running. A build started after that check is not detected.

`DISKCLEANER_XCODE_DERIVED_DATA` replaces the DerivedData directory and
`DISKCLEANER_XCODE_PROCESSES` the `pgrep -f` pattern of the programs that make
it skip.

### swiftpm-cache

Removes the Swift Package Manager cache `~/Library/Caches/org.swift.swiftpm`:
the clones and downloads of package dependencies. The next build that resolves
packages downloads them again. If the cache is a symbolic link, only the link
is removed, not what it points to.

It is skipped while `swift build`, `swift package`, `swift test`, `swift run` or
`xcodebuild` is running. A build started after that check is not detected, and
neither is a build run from the Xcode app itself.

`DISKCLEANER_SWIFTPM_CACHE` replaces the cache directory and
`DISKCLEANER_SWIFT_PROCESSES` the `pgrep -f` pattern of the programs that make
it skip.

## Lines of Code

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
  <img alt="Lines of Code graph" src="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
</picture>
