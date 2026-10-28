# diskcleaner

[![CI](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml/badge.svg)](https://github.com/botforge-pro/diskcleaner/actions/workflows/ci.yml)

Frees disk space on a developer's Mac. Most of what it deletes, builds and
tools make again on demand; a few steps delete what cannot be brought back,
and the table says which.

## What gets deleted

| Step | Deletes | Can it come back |
|---|---|---|
| `projects` | build output, `.apk`/`.aab`, dead Git temporary files and old Git LFS objects in your repositories | yes: the next build, the LFS remote |
| `gradle-caches` | `~/.gradle/caches` | yes: the next Gradle build |
| `docker-images` | Docker images no container uses | when a registry or a Dockerfile still has it |
| `go-cache` | Go module and build caches | yes: the next Go build |
| `homebrew` | old Homebrew versions and all its downloads | **no** for the old versions; downloads come again |
| `swiftpm-cache` | the SwiftPM package cache | yes: the next package resolution |
| `xcode-derived-data` | Xcode DerivedData | yes: the next build |
| `temp-leftovers` | temporary folders of killed Go builds and of Xcode exports | nothing reads them again |
| `system-temp` | your files and folders in `/private/tmp` that nothing wrote or opened for an hour | **no**, and macOS deletes them after three days anyway |
| `code-sign-clones` | copies of Chrome and other apps that no running app was started from | nothing reads them again |
| `ide-old-versions` | data of IDE versions older than the installed one: JetBrains IDEs, Android Studio, VS Code and its forks | **no** for old IntelliJ-platform settings and logs; the VS Code code cache comes again |
| `simulator-data` | apps and data of iOS simulators not used for 3 days | **no**: the simulator is reset to new |
| `simulator-caches` | Core ML caches of iOS simulators | yes: the app compiles its models again |
| `claude-logs` | Claude Code session logs older than 20 days | **no**: such a session can no longer be resumed |
| `codex-logs` | Codex session logs older than 20 days | **no**: such a session can no longer be resumed |
| `claude-scratchpads` | build output in Claude Code scratchpads | yes: the next build |
| `node-modules` | `node_modules` that have a lockfile, and dead Git temporary files in Node projects | usually, by `npm install` and the like; exceptions under the step |

`diskcleaner clean` runs them all in this order. Run it with `--dry-run` first
to see the list. After a cleanup the first build is slower.

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

### Output

```text
XCODE    /Users/me/Library/Developer/Xcode/DerivedData
REMOVE        2.1 GiB  /Users/me/Library/Developer/Xcode/DerivedData/App-abc

TEMP     /private/var/folders/p2/.../T
REMOVE        124 MiB  /private/var/folders/p2/.../T/go-build1218894811
SKIPPED       /private/var/folders/p2/.../T/XcodeDistPipeline.~~~17otKp: written within 60 minutes

Removed 2 paths; freed approximately 2.2 GiB.
Free on the disk of /Users/me: 14 GiB.
```

- `REMOVE` / `WOULD REMOVE`: a path and its size;
- `RUN` / `WOULD RUN`: a cleanup command and the size it works on;
- `SKIPPED`: kept on purpose, with the reason;
- `FAILED`: a failure; `clean` repeats every failure at the end.

`--report FILE` writes `count N` (paths removed or cleaned), `kb N` (KiB freed)
and a `failure ...` line per failure.

### Exit status

| Status | Means |
|---|---|
| 0 | done, no failures |
| 1 | done, with at least one `FAILED` line |
| 2 | nothing touched: a wrong setting, a missing repositories folder or home directory, or a wrong command |
| 3 | nothing touched: another `clean` is running |

`clean` takes the lock `~/.diskcleaner.lock` (`DISKCLEANER_LOCK`). The file
stays and is free once the run ends, however it ends. On a disk too full to
create it, `clean` runs without it and says so. `run` takes no lock.

### Safety rules all steps share

- Settings are checked before anything is touched. A wrong number (a timeout,
  a day or minute count) stops `clean`, `run` and `check` with status 2;
  other settings, such as `pgrep -f` patterns, fail only when their step runs.
- Steps that look inside projects (`projects`, `claude-logs`, `codex-logs`,
  `claude-scratchpads`, `node-modules`) keep a path holding a file Git tracks
  (`SKIPPED`), and a path Git cannot answer for (`SKIPPED` and `FAILED`). The
  cache steps do not ask Git.
- A step that checks for running tools does not see a build started after the
  check.

## Steps in detail

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

`node_modules` folders are not looked into.

Settings: `DISKCLEANER_REPOS_DIR`, `DISKCLEANER_GIT_TMP_MINUTES`,
`DISKCLEANER_GIT_LSOF_TIMEOUT`.

### gradle-caches

- Removes `$GRADLE_USER_HOME/caches` (default `~/.gradle/caches`).
- Skips while `gradlew`, `gradle`, Android Studio or IntelliJ IDEA run, or while
  a Gradle or Kotlin daemon used CPU during a two-second check.
- Stops idle daemons with `SIGTERM` first; daemons alive ten seconds later keep
  the cache and are a failure.
- Settings: `DISKCLEANER_GRADLE_DAEMONS`, `DISKCLEANER_GRADLE_CLIENTS`.

### docker-images

- Runs `docker image prune --all --force` when `docker system df` reports
  images to free. Volumes are never touched.
- Does nothing without `docker` on the `PATH`; skips when `docker info` fails
  and Docker Desktop is not running.
- Docker that does not answer within `DISKCLEANER_DOCKER_TIMEOUT` seconds (30),
  or a prune longer than `DISKCLEANER_DOCKER_PRUNE_TIMEOUT` (900), is a
  failure: the `docker` command and any process it started are killed; Docker
  Desktop itself is left alone.
- The space counted is how much Docker Desktop's data directory shrank, which
  can lag behind what Docker reports.
- Settings: `DISKCLEANER_DOCKER_DATA`, `DISKCLEANER_DOCKER_PROCESS`.

### go-cache

- Runs `go clean -modcache` and `go clean -cache`.
- Does nothing without `go` on the `PATH`.
- Skips while `go build`, `test`, `run`, `install`, `get`, `mod`, `generate`,
  `vet` or `list` run.
- Settings: `DISKCLEANER_GO_PROCESSES`.

### homebrew

- Runs `brew cleanup --prune=all`: removes versions of formulae and casks
  older than the installed ones and every cached download, whatever its age.
- Does nothing without `brew` on the `PATH`; skips while another `brew` runs.
- The space counted is what `brew` reports it freed. A cleanup longer than
  `DISKCLEANER_BREW_TIMEOUT` seconds (900) is a failure, and `brew` with every
  process it started is killed.
- Settings: `DISKCLEANER_BREW_PROCESSES`, `DISKCLEANER_BREW_TIMEOUT`.

### swiftpm-cache

- Removes `~/Library/Caches/org.swift.swiftpm`, the downloaded package
  dependencies; if it is a symbolic link, only the link.
- Skips while `swift build`, `package`, `test`, `run`, `xcodebuild`, Xcode or
  `XCBBuildService` run.
- Settings: `DISKCLEANER_SWIFTPM_CACHE`, `DISKCLEANER_SWIFT_PROCESSES`.

### xcode-derived-data

- Removes every directory in `~/Library/Developer/Xcode/DerivedData`. Files and
  symbolic links in it stay; if DerivedData is itself a link, its target's
  directories go and the link stays.
- Skips while Xcode (any `Xcode*.app`), `xcodebuild` or `XCBBuildService` run.
- Settings: `DISKCLEANER_XCODE_DERIVED_DATA`, `DISKCLEANER_XCODE_PROCESSES`.

### temp-leftovers

- Looks in your temporary folder, `getconf DARWIN_USER_TEMP_DIR` (the one in
  `/var/folders`, whatever `TMPDIR` says), for:
  - `go-build*` and `go-link-*`: the work folders of `go build`, `test` and
    `run`. Go removes them when it exits, so the ones left behind belong to
    commands that were killed;
  - `XcodeDistPipeline.*`: what an archive export for upload leaves behind.
- Removes such a folder when both hold:
  - nothing in it was written within `DISKCLEANER_TMP_MINUTES` (60) minutes;
  - `lsof` sees no process with a file or folder in it open, so a running
    test binary or `go run` server keeps its folder (`SKIPPED`).
- Keeps a folder with a socket or a mounted volume in it (`SKIPPED`).
- When `lsof` fails, warns or does not answer within 30 seconds, the folder
  stays and the step fails.
- Settings: `DISKCLEANER_USER_TMP`, `DISKCLEANER_TMP_MINUTES`.

### system-temp

- Looks in the shared temporary folder `/private/tmp`, where programs and
  agents put copies, dumps and downloads that outlive the work they were for.
- Removes a file or folder of yours there, with everything in it, when all
  of these hold:
  - with everything in it, it takes at least `DISKCLEANER_SYSTEM_TMP_MIN_KB`
    (1024) KB on disk;
  - nothing in it was written within `DISKCLEANER_TMP_MINUTES` (60) minutes;
  - it neither is nor holds a socket (`SKIPPED`), however old;
  - it is not a mounted volume and holds none (`SKIPPED`), such as the
    `dmg.*` folders where disk images are attached: removing it would empty
    the image or fail on a read-only one, and free nothing on this disk;
  - `lsof` sees no process with it, or anything in it, open.
- Keeps what belongs to another user, symbolic links, every name that starts
  with a dot, and the `claude-*` folders of Claude Code sessions, which
  `claude-scratchpads` handles. These rules apply to what sits directly in
  `/private/tmp`.
- These rules keep what running programs need without holding it open.
  Services name their lock files with a dot: PostgreSQL shuts down when
  `.s.PGSQL.5432.lock` disappears. Other lock and pid files weigh a few bytes
  and fall under the size limit. A socket is kept whatever `lsof` says:
  `lsof` finds an open file by either path, `/tmp` or `/private/tmp`, but a
  socket only by the path it was created under.
- What the step removes cannot be recovered. macOS itself removes files in
  `/tmp` that nobody opened or changed for three days, so nothing there was
  meant to last.
- A Chrome profile kept in `/private/tmp` goes on the first `clean` that runs
  more than an hour after Chrome closed. Keep a profile you sign in with in
  your home folder instead.
- When `lsof` fails, warns or does not answer within 30 seconds, the entry
  stays and the step fails.
- Settings: `DISKCLEANER_SYSTEM_TMP`, `DISKCLEANER_SYSTEM_TMP_MIN_KB`,
  `DISKCLEANER_TMP_MINUTES`.

### code-sign-clones

- Chrome copies itself into
  `/var/folders/.../X/com.google.Chrome.code_sign_clone/code_sign_clone.*`
  each time it starts and removes the copy when it quits. A copy outlives a
  Chrome that crashed, was killed or was restarted by an update. Other apps
  that do the same have their own `*.code_sign_clone` folder next to it.
- Removes each copy made more than `DISKCLEANER_TMP_MINUTES` (60) minutes ago
  that no running process was started from: `lsof` finds none of the
  programs in its `Contents/MacOS` and frameworks open. A Chrome running from
  a copy, including one started for remote debugging, keeps it (`SKIPPED`).
- Keeps a copy with no such program inside (`SKIPPED`): without one, `lsof`
  cannot tell whether an app runs from it.
- When `lsof` fails, warns or does not answer within 30 seconds, the copy
  stays and the step fails.
- Settings: `DISKCLEANER_CODE_SIGN_CLONES` (the `X` folder),
  `DISKCLEANER_TMP_MINUTES`.

### ide-old-versions

An upgrade of an IDE leaves the data of the old version behind. The step
counts as installed every app it finds in `/Applications` and
`~/Applications` (one folder deep), under the JetBrains Toolbox `apps` folder
(any depth), and every app Spotlight knows of, such as one in `~/Downloads`
or on another disk. It handles two families:

| Family | How it is recognised | What it removes |
|---|---|---|
| IntelliJ platform: every JetBrains IDE, Android Studio and other IDEs built on it | `Contents/Resources/product-info.json` names the vendor and the data folder, such as `JetBrains` and `GoLand2026.2` | folders of older versions of the same product, such as `GoLand2025.3`, in `~/Library/Application Support`, `Caches` and `Logs` under the vendor's name |
| VS Code and its forks: Cursor, Windsurf, VSCodium and others | `Contents/Resources/app/product.json` names the data folder and the build's commit | `~/Library/Application Support/<name>/CachedData/<commit>` of builds no longer installed |

- A folder of an older version goes only when the same product has a newer
  installed version and the older version is no longer installed. Folders of
  products not installed, of newer versions and of every installed version
  stay; so does the cache of every installed build of a VS Code-family
  editor, even when two builds share a folder.
- An app Spotlight does not index, on a disk with indexing off, is not seen:
  its data counts as that of a version no longer installed.
- When `mdfind` fails or lists no apps at all, the step removes nothing
  (`FAILED`).
- Nothing goes until the installed version has run and made its own folder
  next to the old one, so it had the chance to copy the settings it wants
  (`SKIPPED` until then).
- An app whose description cannot be read, or names a folder oddly, stops the
  step before anything is removed (`FAILED`).
- Downloaded extensions (`CachedExtensionVSIXs`) and the extensions
  themselves stay.

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
  compiled for its apps. App data is not touched.
- Skips all simulators while Simulator, DeviceHub, `simctl` or `xcodebuild` run.
- Skips a simulator that is not Shutdown or has a running process; one whose
  state `plutil` cannot read is a failure.
- Settings: `DISKCLEANER_SIMULATOR_DEVICES`, `DISKCLEANER_SIMULATOR_PROCESSES`.

### claude-logs

- Removes `*.jsonl` session logs, agents' included, under `~/.claude/projects`
  last modified more than `DISKCLEANER_CLAUDE_LOG_DAYS` whole days ago (20, so
  21 days or more).
- Keeps a log that was written again after the scan found it old, and a log
  that `lsof` finds open (`SKIPPED`). `lsof` looks at the logs 50 at a time,
  right before those 50 are removed, so a session that opens an old log in
  that moment and has not yet written to it can still lose it.
- When `lsof` does not answer within 30 seconds, or warns, no further log is
  removed (`FAILED`).
- Settings: `DISKCLEANER_CLAUDE_PROJECTS`, `DISKCLEANER_CLAUDE_LOG_DAYS`.

### codex-logs

- Removes `*.jsonl` session logs under `~/.codex/sessions` last modified more
  than `DISKCLEANER_CODEX_LOG_DAYS` whole days ago (20, so 21 days or more).
  `~/.codex/history.jsonl`, `session_index.jsonl` and the databases next to
  `sessions` stay.
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

What it keeps and cannot undo:

- keeps a `node_modules` with no lockfile (`SKIPPED`): a reinstall would not
  bring back the same versions;
- a reinstall cannot bring back packages installed with `--no-save`,
  unpublished, or from a registry you can no longer reach;
- it does not check whether `npm install` is running.

Settings: `DISKCLEANER_NODE_DIR`, `DISKCLEANER_REPOS_DIR`,
`DISKCLEANER_GIT_TMP_MINUTES`, `DISKCLEANER_GIT_LSOF_TIMEOUT`.

## Lines of Code

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
  <img alt="Lines of Code graph" src="https://raw.githubusercontent.com/botforge-pro/diskcleaner/main/.github/loc-history-light.svg">
</picture>
