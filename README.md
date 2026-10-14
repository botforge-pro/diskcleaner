# diskcleaner

[![CI](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml/badge.svg)](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml)

Frees disk space on a developer machine by removing what builds and tools leave
behind and recreate on demand: build output, package caches, compiled models in
simulators.

## Install

```sh
go install github.com/botforge-pro/diskcleaner@latest
```

This puts the `diskcleaner` binary in `$(go env GOPATH)/bin` (usually `~/go/bin`);
make sure it is on your `PATH`. Install it before the disk fills up: building it
needs free space, and running it does not.

## Usage

```sh
diskcleaner version  # print the installed version
```

## Lines of Code

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
  <img alt="Lines of Code graph" src="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
</picture>
