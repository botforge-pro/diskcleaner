# Changelog

All notable changes to this project are documented in this file. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/); while the version
is 0.x, commands and output may still change.

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
  one folder per app or service in each simulator, and nothing else. It is safe: the app compiles its models again on its next
  launch, which only makes that launch slower. A simulator that is running is
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

[0.1.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.1.0
