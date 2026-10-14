# diskcleaner

[![CI](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml/badge.svg)](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml)

A tool for freeing disk space on a developer machine by removing what builds and
tools leave behind and recreate on demand. It does not remove anything yet: the
only command prints its version.

## Install

```sh
go install github.com/botforge-pro/diskcleaner@latest
```

This puts the `diskcleaner` binary in `$(go env GOBIN)`, or in
`$(go env GOPATH)/bin` (usually `~/go/bin`) when `GOBIN` is not set; make sure
that directory is on your `PATH`.

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
