# diskcleaner

[![CI](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml/badge.svg)](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml)

Frees disk space on a developer's Mac: build output, caches and leftovers that
the tools make again. Steps that delete what cannot come back are off until you
turn them on.

## Install

```sh
go install github.com/botforge-pro/diskcleaner@latest
```

## Use

```sh
diskcleaner clean --dry-run                        # what would go, nothing removed
diskcleaner clean                                  # remove it, say what is free
diskcleaner clean --only projects,go-cache         # just these steps
diskcleaner clean --projects.git-tmp-min-age=0     # any setting, for this run
diskcleaner config                                 # every setting and where it came from
```

`clean` keeps whatever a running tool uses: it skips a cache while its tool
runs and anything a process holds open, written within `min-age`, or tracked
by Git. Each kept path is listed as `SKIPPED` with the reason; `--report FILE`
writes the totals.

| Exit | Means |
|---|---|
| 0 | done |
| 1 | done, some paths failed (`FAILED` lines) |
| 2 | nothing touched: a wrong setting or command |
| 3 | nothing touched: another `clean` runs; its PID and start are printed |
| 4 | done, but less than `min-free` is free |

## Configure

Everything works without a config. To change a setting, put it in
`~/.config/diskcleaner.toml` (or `$XDG_CONFIG_HOME/diskcleaner.toml`, or
`--config FILE`), or pass it as `--key=value` for one run; a flag wins over the
file.

```toml
project-dirs = ["~/Developer", "~/work"]
min-age = "1h"
min-free = "5GB"

[xcode-archives]
enabled = true
min-age = "30d"
```

| Setting | Default | |
|---|---|---|
| `project-dirs` | those of `~/Developer`, `~/Projects`, `~/src`, `~/code`, `~/GolandProjects`, `~/IdeaProjects`, `~/AndroidStudioProjects` that exist | where Git repositories sit, one level down |
| `min-age` | `1h` | anything written more recently stays |
| `min-free` | `0` (off) | exit 4 when less is free after `clean` |
| `<step>.enabled` | on, off for the steps marked below | |

Durations take `m`, `h` and `d`. Each step's own settings are under it below.

## Steps

`clean` runs them in this order. **Off by default:** delete what cannot come back.

| Step | Removes | Keeps |
|---|---|---|
| `projects` | in each project: `.build` of Swift packages, Gradle `build`, `.apk`/`.aab`, `node_modules` with a lockfile, temporary files of a dead `git fetch` or `git repack` (`git-tmp-min-age`, `1h`), Git LFS objects the remote has | `node_modules` without a lockfile (a reinstall would bring other versions) |
| `gradle-caches` | `$GRADLE_USER_HOME/caches` | everything while Gradle, Android Studio or IntelliJ run |
| `docker-images` | images no container uses (`timeout`, `30s`; `prune-timeout`, `15m`) | volumes |
| `go-cache` | the Go module and build caches | everything while `go` builds |
| `swiftpm-cache` | the SwiftPM package cache | everything while Swift or Xcode build |
| `xcode-derived-data` | Xcode DerivedData | everything while Xcode builds |
| `simulator-caches` | Core ML caches of shut-down simulators | app data |
| `temp-leftovers` | work folders of killed Go builds, Xcode export leftovers | |
| `code-sign-clones` | copies of Chrome and other apps no running app was started from | |
| `claude-scratchpads` | build output in Claude Code scratchpads | the scratchpads |
| **`homebrew`** | old versions of formulae and casks, every download (`timeout`, `15m`) | installed versions |
| **`xcode-archives`** | archives older than `min-age` (`15d`) | the newest `keep` (`1`) of each app; anything moved to a folder not named by a date |
| **`ide-old-versions`** | settings, caches and logs of IDE versions older than the installed one | data of every installed version |
| **`simulator-data`** | apps and data of simulators idle for `idle` (`3d`); the simulator stays, as new | |
| **`system-temp`** | your files in `/private/tmp` larger than `min-size` (`1MB`) | sockets, mounted volumes, dot files, what a process holds open |
| **`claude-logs`** | Claude Code session logs older than `min-age` (`20d`); such a session can no longer be resumed | open logs |
| **`codex-logs`** | Codex session logs older than `min-age` (`20d`) | open logs |

## Lines of Code

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
  <img alt="Lines of Code graph" src="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
</picture>
