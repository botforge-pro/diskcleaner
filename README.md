# diskcleaner

[![CI](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml/badge.svg)](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml)

Frees disk space on a Mac by deleting what builds and tools recreate on demand.

## What gets deleted

| Step | Deletes |
|---|---|
| `projects` | build output, `.apk`/`.aab`, dead Git temporary files and old Git LFS objects in your repositories |
| `gradle-caches` | `~/.gradle/caches` |
| `docker-images` | Docker images no container uses |
| `go-cache` | Go module and build caches |
| `homebrew` | old Homebrew versions and all its downloads |
| `swiftpm-cache` | the SwiftPM package cache |
| `xcode-derived-data` | Xcode DerivedData |
| `temp-leftovers` | temporary folders of killed Go builds and of Xcode exports |
| `code-sign-clones` | copies of Chrome and other apps that no running app was started from |
| `simulator-data` | apps and data of iOS simulators not used for 3 days |
| `simulator-caches` | Core ML caches of iOS simulators |
| `claude-logs` | Claude Code session logs older than 20 days |
| `codex-logs` | Codex session logs older than 20 days |
| `claude-scratchpads` | build output in Claude Code scratchpads |
| `node-modules` | `node_modules` that have a lockfile, and dead Git temporary files in Node projects |

`diskcleaner clean` runs them all in this order. What makes each step skip is
listed for it below. After a cleanup the first build is slower.

## Install

```sh
go install github.com/botforge-pro/diskcleaner@latest
```

The binary lands in `$(go env GOBIN)`, or in `$(go env GOPATH)/bin` (usually
`~/go/bin`) when `GOBIN` is not set; put that directory on your `PATH`.

## Usage

```sh
diskcleaner clean --dry-run            # list what every step would remove
diskcleaner clean                      # remove it, then say how much is free
diskcleaner run STEP --dry-run         # the same for one step
diskcleaner run STEP --report FILE     # also write the totals to FILE
diskcleaner check STEP...              # check the settings of these steps
diskcleaner version
```

`clean`:

- checks the settings of every step first; a wrong one stops it with exit
  status 2 before anything is touched;
- runs one at a time: a second `clean` while one runs exits 3. The lock is
  `~/.diskcleaner.lock` (`DISKCLEANER_LOCK`); the file stays, and is free once
  the run ends however it ends. On a disk too full to create it, `clean` runs
  without it and says so;
- ends with the paths and the space removed, then repeats every failure.

`run` takes no lock.

Output lines:

- `REMOVE` / `WOULD REMOVE`: a path and its size;
- `RUN` / `WOULD RUN`: a cleanup command and the size it works on;
- `SKIPPED`: kept on purpose, with the reason;
- `FAILED`: a failure; any failure makes the exit status 1.

The report file holds `count N` (paths removed or cleaned), `kb N` (KiB freed)
and a `failure ...` line per failure.

A wrong number setting (a timeout, a day or minute count), a missing
repositories folder or a missing home directory makes `clean`, `run` and
`check` exit 2 before anything is touched. Other settings, such as `pgrep -f` patterns, fail
only when the step runs.

Steps that look inside projects (`claude-logs`, `codex-logs`,
`claude-scratchpads`, `projects`, `node-modules`) keep a path holding a file Git tracks (`SKIPPED`), and keep a path
Git cannot answer for (`SKIPPED` and `FAILED`). The cache steps do not ask Git.

## Steps in detail

A build started after a step checked for running tools is not detected.

### simulator-data

- Runs `xcrun simctl erase` on each iOS simulator in
  `~/Library/Developer/CoreSimulator/Devices` whose `lastUsedAt` is more than
  `DISKCLEANER_SIMULATOR_IDLE_DAYS` whole days ago (3). The simulator stays in
  Xcode's list; every app installed on it and all their data are gone, as on a
  new one.
- Keeps a simulator not booted since it was created or erased (it has no
  `data/Containers` yet): erasing it again frees nothing.
- `--dry-run` shows the whole `data` folder of a simulator, while the erase
  leaves a fresh one of about 20 megabytes, so the size it shows is an
  upper bound; a real run counts what was actually freed.
- Skips all simulators while Simulator, DeviceHub, `simctl` or `xcodebuild` run.
- Skips a simulator that is not Shutdown or has a running process; one whose
  `device.plist` `plutil` cannot read is a failure.
- Settings: `DISKCLEANER_SIMULATOR_DEVICES`, `DISKCLEANER_SIMULATOR_PROCESSES`,
  `DISKCLEANER_SIMULATOR_IDLE_DAYS`.

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

### temp-leftovers

- Looks in your temporary folder, `getconf DARWIN_USER_TEMP_DIR` (the one in
  `/var/folders`, whatever `TMPDIR` says), and removes folders written more
  than `DISKCLEANER_TMP_MINUTES` (60) minutes ago:
  - `go-build*` and `go-link-*`: the work folders of `go build`, `test` and
    `run`. Go removes them when it exits, so the ones left behind belong to
    commands that were killed. One killed `go test` left 1.1 GB;
  - `XcodeDistPipeline.*`: what `xcodebuild -exportArchive` leaves after
    exporting an archive for upload, about 100 MB per upload.
- Nothing is lost: none of them is read again.
- Skips the Go folders while a `go` command runs (`DISKCLEANER_GO_PROCESSES`),
  and the Xcode ones while Xcode or `xcodebuild` runs
  (`DISKCLEANER_XCODE_PROCESSES`).
- Settings: `DISKCLEANER_USER_TMP`, `DISKCLEANER_TMP_MINUTES`,
  `DISKCLEANER_GO_PROCESSES`, `DISKCLEANER_XCODE_PROCESSES`.

### code-sign-clones

- Chrome copies itself into
  `/var/folders/.../X/com.google.Chrome.code_sign_clone/code_sign_clone.*`
  each time it starts, 1.4 to 2.8 GB a copy, and removes the copy when it
  quits.
  A copy outlives a Chrome that crashed, was killed or was restarted by an
  update. Other apps that do the same have their own `*.code_sign_clone`
  folder next to it.
- Removes each copy made more than `DISKCLEANER_TMP_MINUTES` (60) minutes ago
  that no running process was started from: `lsof` finds none of the
  programs in its `Contents/MacOS` and frameworks open. A Chrome running from
  a copy, including one started for remote debugging, keeps it (`SKIPPED`).
- When `lsof` fails, warns or does not answer within 30 seconds, the copy
  stays and the step fails.
- Settings: `DISKCLEANER_CODE_SIGN_CLONES` (the `X` folder),
  `DISKCLEANER_TMP_MINUTES`.

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

### homebrew

- Runs `brew cleanup --prune=all`: removes versions of formulae and casks
  older than the installed ones and every cached download, whatever its age.
- The next install or upgrade downloads its bottle again; an old version is no
  longer there to switch back to.
- Does nothing without `brew` on the `PATH`; skips while another `brew` runs.
- The space counted is what `brew` reports it freed. A cleanup longer than
  `DISKCLEANER_BREW_TIMEOUT` seconds (900) is a failure, and `brew` with every
  process it started is killed.
- Settings: `DISKCLEANER_BREW_PROCESSES`, `DISKCLEANER_BREW_TIMEOUT`.

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
  last modified more than `DISKCLEANER_CLAUDE_LOG_DAYS` whole days ago (20, so
  21 days or more).
- Such a session can no longer be resumed.
- Keeps a log that was written again after the scan found it old, and a log
  that `lsof` finds open (`SKIPPED`). `lsof` looks at the logs 50 at a time,
  right before those 50 are removed, so a session that opens an old log in
  that moment and has not yet written to it can still lose it. When `lsof`
  does not answer within 30 seconds, or warns, no further log is removed
  (`FAILED`).
- Settings: `DISKCLEANER_CLAUDE_PROJECTS`, `DISKCLEANER_CLAUDE_LOG_DAYS`.

### codex-logs

- Removes `*.jsonl` session logs under `~/.codex/sessions` last modified more
  than `DISKCLEANER_CODEX_LOG_DAYS` whole days ago (20, so 21 days or more).
  `~/.codex/history.jsonl`, `session_index.jsonl` and the databases next to
  `sessions` stay.
- Such a session can no longer be resumed.
- Keeps open and rewritten logs the same way as `claude-logs`.
- Settings: `DISKCLEANER_CODEX_SESSIONS`, `DISKCLEANER_CODEX_LOG_DAYS`.

### claude-scratchpads

- Looks in the `scratchpad` folders of Claude Code sessions under
  `/private/tmp/claude-<uid>` and removes:
  - Xcode derived data: `Build`, `Index.noindex`, `ModuleCache.noindex` and
    `SourcePackages` next to a `Build/Intermediates.noindex`;
  - `checkouts` and `repositories` next to a `workspace-state.json`;
  - `.build` next to a `Package.swift`.
- The folders these sit in stay; running sessions are cleaned too, including a
  `.build` an agent keeps as test data.
- Skips while `swift build`, `package`, `test`, `run` or `xcodebuild` run.
- Settings: `DISKCLEANER_CLAUDE_TMP`, `DISKCLEANER_SWIFT_PROCESSES`.

### projects

In each Git repository one level below `~/Developer`, removes:

- `.build` next to a `Package.swift`;
- Gradle `build` next to a `build.gradle(.kts)` or `settings.gradle(.kts)`;
- `.apk` and `.aab` files, when the repository has a `gradlew`;
- temporary files a dead `git fetch` or `git repack` left in `.git/objects`
  (`tmp_*`, `*/tmp_*`, `pack/.tmp-*`), once all of these hold:
  - older than `DISKCLEANER_GIT_TMP_MINUTES` (60);
  - the `git repack` named in it has ended;
  - `lsof` sees no process of yours holding it within
    `DISKCLEANER_GIT_LSOF_TIMEOUT` seconds (30); other users' processes, such
    as `sudo git`, are guarded only by the age;
- Git LFS objects `git lfs prune --verify-remote` would prune: only those the
  remote has.

`node_modules` folders are not looked into. Settings: `DISKCLEANER_REPOS_DIR`,
`DISKCLEANER_GIT_TMP_MINUTES`, `DISKCLEANER_GIT_LSOF_TIMEOUT`.

### node-modules

Below `DISKCLEANER_NODE_DIR` (`~/GolandProjects`), removes:

- each `node_modules` (not those inside it) whose folder has a lockfile:
  `package-lock.json`, `npm-shrinkwrap.json`, `yarn.lock` or
  `pnpm-lock.yaml`;
- or whose workspace root above it, up to `DISKCLEANER_NODE_DIR`, has one; a
  workspace root has a `pnpm-workspace.yaml` or `"workspaces"` in its
  `package.json`;
- temporary files of a dead `git fetch` or `git repack` in each Git repository
  one level below, by the same rules as in `projects`; when
  `DISKCLEANER_NODE_DIR` is the repositories folder, `projects` covers them.

- Keeps a `node_modules` with no lockfile (`SKIPPED`): a reinstall would not
  bring back the same versions.
- A reinstall cannot bring back packages installed with `--no-save`,
  unpublished, or from a registry you can no longer reach.
- It does not check whether `npm install` is running.

Settings:
`DISKCLEANER_NODE_DIR`, `DISKCLEANER_REPOS_DIR`, `DISKCLEANER_GIT_TMP_MINUTES`,
`DISKCLEANER_GIT_LSOF_TIMEOUT`.

## Lines of Code

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
  <img alt="Lines of Code graph" src="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
</picture>
