# Changelog

All notable changes to this project are documented in this file. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/); while the version
is 0.x, commands and output may still change.

## [0.14.0] - 2026-10-26

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.14.0`.

After upgrading, `diskcleaner clean` also deletes old work folders of Go and
Xcode in your temporary folder and copies of Chrome no running Chrome uses.
Run `diskcleaner clean --dry-run` first to see the list.

### Added

- A new step, `temp-leftovers`, removes `go-build*`, `go-link-*` and
  `XcodeDistPipeline.*` folders in the temporary folder
  `getconf DARWIN_USER_TEMP_DIR` names. Go leaves the first two when a
  `go build`, `test` or `run` is killed; an archive export leaves the last.
  A folder stays while anything in it was
  written within `DISKCLEANER_TMP_MINUTES` (60) minutes or `lsof` shows a
  process with something in it open, such as a running test binary.
  `DISKCLEANER_USER_TMP` sets the folder.
- A new step, `code-sign-clones`, removes the copies Chrome makes of itself at
  each start under `/var/folders/.../X/*.code_sign_clone` and leaves behind
  when it crashes, is killed or restarts for an update, gigabytes each. A
  copy that `lsof` shows a running program was started from is kept, and so
  is one made within `DISKCLEANER_TMP_MINUTES`, and one with no program
  inside where it expects one. `DISKCLEANER_CODE_SIGN_CLONES` sets the
  folder.

`clean` runs both right after `xcode-derived-data`.

### Fixed

- `claude-logs` and `codex-logs` no longer stop with `FAILED` when only some
  of the 50 old logs they check at once are open. `lsof` exits with status 1
  in that case while listing the open ones, which was taken for a failure;
  now the open logs are kept and the rest are removed.

## [0.13.1] - 2026-10-22

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.13.1`.

### Fixed

- `simulator-data` no longer erases the same simulator on every run. Erasing
  keeps the simulator's old last-boot date, so 0.13.0 erased it again each
  time, and `--dry-run` listed it as about 17 MiB to free. A simulator not
  booted since it was created or erased is now left alone with a `SKIPPED`
  line; once booted again, it is erased three days after its last boot as
  before.

## [0.13.0] - 2026-10-22

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.13.0`.

After upgrading, `diskcleaner clean` deletes more than before: old Homebrew
versions with every Homebrew download, and iOS simulators not booted for
three days are reset to new, losing their apps and app data. Run
`diskcleaner clean --dry-run` first to see the list. Neither can be undone.

### Added

- A new step, `homebrew`, runs `brew cleanup --prune=all`. Besides the old
  versions of formulae and casks that a plain `brew cleanup` removes, it
  deletes every cached download, however recent. After it an older version of
  a formula is no longer on the disk to go back to, and the next install
  downloads its bottle again. `clean` runs it right after `go-cache`; run it
  alone with `diskcleaner run homebrew`. It does nothing without `brew` on the
  `PATH`. While another `brew` runs it is skipped with a `SKIPPED` line, which
  does not change the exit status. `DISKCLEANER_BREW_TIMEOUT` (default 900) is
  how many seconds the cleanup may take; past that `brew` is stopped and the
  step is a failure, so `clean` exits 1 after running the remaining steps. It
  must be a whole number above zero; any other value stops `clean` and `run`
  before anything is deleted.
- A new step, `simulator-data`, runs `xcrun simctl erase` on each shut-down
  iOS simulator whose last boot (`lastUsedAt` in its `device.plist`) is more
  than three days old: 3 days and 1 second is erased, exactly 3 days is kept.
  The simulator stays in Xcode's list, but every app installed on it, its
  data and settings are gone, as on a new simulator. Booted simulators,
  simulators never booted, and all simulators while Simulator, `simctl` or an
  Xcode build run are left alone with a `SKIPPED` line. `clean` runs it right
  before `simulator-caches`. `DISKCLEANER_SIMULATOR_IDLE_DAYS` (default 3)
  sets the number of days. It must be a whole number above zero; any other
  value stops `clean` and `run` before anything is deleted. There is no switch
  to turn the step off: to keep simulators much longer, set
  `DISKCLEANER_SIMULATOR_IDLE_DAYS=3650`.

## [0.12.0] - 2026-10-20

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.12.0`.

After upgrading, `diskcleaner clean` deletes more than before: Codex session
logs, and Claude Code session logs 21 to 30 days old. Run
`diskcleaner clean --dry-run` first to see the list.

### Added

- A new step, `codex-logs`, deletes Codex session logs: the `*.jsonl` files
  under `~/.codex/sessions` not modified for 21 days or more. A file 20 days
  and 23 hours old stays. `clean` runs it right after `claude-logs`; run it
  alone with `diskcleaner run codex-logs`. The files are deleted, not moved to
  the Trash, and Codex can no longer resume those sessions.
- Environment variables set the step up. `DISKCLEANER_CODEX_LOG_DAYS`
  (default 20) is the age in whole days that a log may reach and still stay,
  so `30` deletes logs 31 days old or more. It must be a whole number above
  zero; any other value stops `clean` and `run` before anything is deleted.
  `DISKCLEANER_CODEX_SESSIONS` replaces `~/.codex/sessions` as the folder
  searched, subfolders included. There is no switch to turn the step off: to
  keep the logs much longer, set `DISKCLEANER_CODEX_LOG_DAYS=3650`.

### Changed

- `claude-logs` keeps 20 whole days of Claude Code session logs instead of
  30, so the next `clean` or `diskcleaner run claude-logs` also deletes the
  logs 21 to 30 days old. To keep 30 days, set the environment variable
  `DISKCLEANER_CLAUDE_LOG_DAYS=30`.

### Fixed

- `claude-logs`, and the new `codex-logs`, now check each old log again just
  before deleting it, so a session resumed while the cleanup runs is less
  likely to lose its log, though not fully protected. A log that has been
  written to since the step listed it stays, and so does one that `lsof`
  shows open at that check; both are listed as `SKIPPED`. A session that
  opens an old log after that check and before the deletion, without writing
  to it, can still lose it. When `lsof` does not answer within 30 seconds, or
  prints a warning, the step deletes no further logs, keeps the ones already
  deleted, and reports `FAILED`, which makes the exit status 1; the other
  steps of `clean` still run.

## [0.11.0] - 2026-10-15

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.11.0`. What
each step deletes and keeps is as in 0.10.0, whether run alone or by `clean`.

### Added

- `diskcleaner clean [--dry-run]` runs every step, in this order: `projects`,
  `gradle-caches`, `docker-images`, `go-cache`, `swiftpm-cache`,
  `xcode-derived-data`, `simulator-caches`, `claude-logs`,
  `claude-scratchpads`, `node-modules`. It asks for no confirmation, so run
  it with `--dry-run` first. Any other argument, `--report` included, prints
  the usage and exits with status 2.
  - Before anything else it checks the settings of every step, the same
    check as `diskcleaner check`: a wrong one, such as
    `DISKCLEANER_GIT_TMP_MINUTES=1.5` or a missing `DISKCLEANER_REPOS_DIR`,
    or no home folder, prints the problem and exits with status 2; nothing is
    deleted.
  - One `clean` at a time: while one runs, a second exits with status 3 and
    `another cleanup is already running`. The lock is the file
    `~/.diskcleaner.lock` (`DISKCLEANER_LOCK` names another). The file is
    never deleted, and a run that crashes or is killed frees it on its own,
    so there is no stale lock to remove. On a disk too full to create the
    file, `clean` prints `The disk is full, so the lock
    /Users/you/.diskcleaner.lock cannot be made; running without it.` and
    runs; do not start a second `clean` then, as nothing stops it.
  - A path that an earlier step removed, or with `--dry-run` listed as
    `WOULD REMOVE`, is skipped silently by later steps, with everything inside
    it: a `node_modules` inside a Swift `.build` that `projects` removes is
    neither removed again nor counted twice.
  - At the end it prints `Removed N paths; freed approximately X.` and
    `Free on the disk of <home folder>: X.`; with `--dry-run`, `Would remove
    N paths and free approximately X.` and `Nothing was removed.` instead.
  - Exit status: 0 when nothing failed; 1 when anything failed, after every
    `FAILED` line is printed once more under `N failure(s):`; 2 and 3 as
    above. A `SKIPPED` line is not a failure.
- `diskcleaner run STEP` takes no lock. Running one while `clean` runs can
  make both work on the same files: one may fail on what the other already
  deleted, and the totals of each count it.

### Changed

- `diskcleaner` with no arguments lists the steps in the order `clean` runs
  them.

## [0.10.0] - 2026-10-15

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.10.0`; the
steps added in earlier versions work as in 0.9.0.

### Added

- `diskcleaner run node-modules` cleans Node projects at any depth below
  `~/GolandProjects` (set `DISKCLEANER_NODE_DIR` if yours live elsewhere). It
  first prints `NODE` and the folder it cleans, then:
  - deletes a `node_modules` folder, with everything in it (including any
    `node_modules` nested inside), when a lockfile can rebuild it: a
    `package-lock.json`, `npm-shrinkwrap.json`, `yarn.lock` or
    `pnpm-lock.yaml` in the folder that holds it, or in any folder above it,
    up to and including `~/GolandProjects`, that is a workspace root (has a
    `pnpm-workspace.yaml`, or `"workspaces"` in its `package.json`);
  - deletes temporary files that an interrupted `git fetch` or `git repack`
    left in `.git/objects` of each Git repository directly inside
    `~/GolandProjects` (a repository deeper down keeps them), when the file was
    last modified more than 60 minutes ago, the `git repack` that writes it
    has ended, and `lsof` sees none of your processes holding it within 30
    seconds;
  - when you point `DISKCLEANER_REPOS_DIR` (the folder of the `projects`
    step, `~/Developer` by default) at the same folder, this step skips the
    Git temporary files silently, since `projects` cleans the same ones;
  - cost: each project needs `npm install` (or yarn, pnpm) again. A reinstall
    cannot bring back packages you installed with `--no-save`, packages
    removed from their registry since, or packages from a registry you can no
    longer reach. It does not check whether `npm install` is running, so do
    not run it during one;
  - keeps, with a `SKIPPED` line and exit status 0: a `node_modules` with no
    lockfile, a `node_modules` holding a file Git tracks, and a Git temporary
    file that is too young, still written or still open;
  - keeps, with a `SKIPPED` and a `FAILED` line and exit status 1: a path for
    which `git` fails to say whether it tracks files, for example because it
    refuses a repository you do not own; the `FAILED` line quotes `git`, and
    fixing what it says lets the next run delete the path;
  - a missing `~/GolandProjects` is passed over silently;
  - failures, exit status 1, after which everything else is still cleaned: a
    folder it cannot read, `lsof` exiting with an error, `lsof` giving no
    answer within 30 seconds;
  - `--dry-run`: prints `WOULD REMOVE` lines and deletes nothing; it still
    asks `git` and `lsof`;
  - `--report FILE`: `count` is the folders and files deleted, `kb` the KiB
    freed; with `--dry-run`, what would be;
  - settings: `DISKCLEANER_NODE_DIR` replaces `~/GolandProjects`,
    `DISKCLEANER_GIT_TMP_MINUTES` the 60 minutes,
    `DISKCLEANER_GIT_LSOF_TIMEOUT` the 30 seconds; a value that is not a
    whole number above zero gives exit status 2 before anything is deleted.

### Changed

- `projects` with a relative `DISKCLEANER_REPOS_DIR` now prints absolute
  paths; what it deletes is the same.

## [0.9.0] - 2026-10-15

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.9.0`; the
steps added in earlier versions work as in 0.8.0.

### Added

- `diskcleaner run projects` cleans each Git repository directly inside
  `~/Developer` (not deeper; other folders there are passed over silently),
  printing a `PROJECT` line for each:
  - deletes `.build` next to a `Package.swift`, Gradle `build` next to a
    `build.gradle(.kts)` or `settings.gradle(.kts)`, and `.apk` and `.aab`
    files in a repository with a `gradlew`;
  - deletes temporary files a dead `git fetch` or `git repack` left in
    `.git/objects`: last modified more than 60 minutes ago, not written by a
    `git repack` that is still running, and not held open by any of your own
    processes according to `lsof` (other users' processes are not seen);
  - prunes Git LFS objects with `git lfs prune --verify-remote`, so only
    objects the remote has go;
  - cost: the next build of each project starts from scratch. It does not
    check whether a build is running, so do not run it during one;
  - keeps with a `SKIPPED` line and exit status 0: a `.build` with no
    `Package.swift`, a younger or still open temporary file, a folder or file
    holding something Git tracks; a `build` with no Gradle file next to it is
    kept silently;
  - keeps with a `SKIPPED` and a `FAILED` line, exit status 1: a path Git
    cannot answer for;
  - failures, exit status 1, after which the other repositories are still
    cleaned: `lsof` that errs or gives no answer within 30 seconds, `git-lfs`
    missing while a repository uses LFS, a folder it cannot read;
  - `--dry-run`: `WOULD REMOVE` and `WOULD PRUNE` lines with the size Git LFS
    reports, deletes nothing (it still asks `lsof` and `git lfs`);
  - `--report FILE`: `count` is the folders and files deleted plus one per
    repository whose prune succeeded, `kb` the space freed; with `--dry-run`,
    what would be;
  - settings: `DISKCLEANER_REPOS_DIR` replaces `~/Developer` (a missing folder
    gives exit status 2), `DISKCLEANER_GIT_TMP_MINUTES` the 60 minutes,
    `DISKCLEANER_GIT_LSOF_TIMEOUT` the 30 seconds.

### Fixed

- Wording only: the 0.7.0 and 0.8.0 entries below now say that a path Git
  cannot answer for is kept as a failure with exit status 1; before this
  release they said exit status 0. The program always did the former.

## [0.8.0] - 2026-10-15

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.8.0`; the
steps added in earlier versions work as in 0.7.0.

### Added

- `diskcleaner run claude-scratchpads` deletes build output that Claude Code
  agents leave in their session scratchpads:
  - where: `scratchpad` folders under `/private/tmp/claude-<uid>`;
  - which: Xcode build output (`Build`, `Index.noindex`, `ModuleCache.noindex`,
    `SourcePackages`) next to a `Build/Intermediates.noindex`; the `checkouts`
    and `repositories` of a Swift package next to a `workspace-state.json`;
    `.build` next to a `Package.swift`;
  - prints a `REMOVE` line for each folder it deletes;
  - keeps silently: the folders these sit in and every other file;
  - keeps with a `SKIPPED` line and exit status 0:
    - a `.build` with no `Package.swift` next to it (any `.build` inside that
      one stays too, silently); Xcode and package caches inside such a
      `.build` are still deleted;
    - a path holding any file Git tracks;
  - keeps a path where Git cannot say whether it tracks a file, with a
    `SKIPPED` and a `FAILED` line and exit status 1;
  - cost: the next build in that scratchpad starts from scratch, also in
    Claude Code sessions that are still open;
  - skips the whole step with a `SKIPPED` line and exit status 0 while
    `swift build`, `swift package`, `swift test`, `swift run` or `xcodebuild`
    runs; a build started after that check, or one in the Xcode app, is not
    noticed;
  - failure, exit status 1: a folder it cannot read; what it could read is
    still cleaned;
  - `--dry-run`: `WOULD REMOVE` lines, deletes nothing; `--report FILE`:
    `count` is the number of folders deleted (would be, with `--dry-run`), `kb`
    the space freed;
  - settings: `DISKCLEANER_CLAUDE_TMP` replaces `/private/tmp/claude-<uid>`,
    `DISKCLEANER_SWIFT_PROCESSES` the programs that make it skip.

## [0.7.0] - 2026-10-15

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.7.0`; the
steps added in earlier versions work as in 0.6.0.

### Added

- `diskcleaner run claude-logs` deletes old Claude Code session logs:
  - which: `*.jsonl` files anywhere under `~/.claude/projects`, agents' logs in
    subfolders included, last modified 31 or more days ago;
  - cost: such a session can no longer be resumed;
  - keeps, with a `SKIPPED` line and exit status 0, a log inside a Git
    repository that tracks it; when Git cannot say whether it does, keeps it
    with a `SKIPPED` and a `FAILED` line and exit status 1;
  - failure, exit status 1: a folder it cannot read; the logs it could read
    are still deleted;
  - `--dry-run`: prints `WOULD REMOVE` lines, deletes nothing;
  - `--report FILE`: `count` is the number of logs deleted, `kb` the space
    freed;
  - settings: `DISKCLEANER_CLAUDE_PROJECTS` replaces the folder,
    `DISKCLEANER_CLAUDE_LOG_DAYS` the 30 days; a day count that is not a whole
    number above zero gives exit status 2 before anything is deleted.

## [0.6.0] - 2026-10-15

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.6.0`; the
steps added in earlier versions work as in 0.5.0.

### Added

- `diskcleaner run gradle-caches` deletes the Gradle caches folder:
  - which: `~/.gradle/caches`, or `caches` inside `GRADLE_USER_HOME`;
  - cost: the next Gradle build downloads and builds them again, slower and
    online;
  - skips with a `SKIPPED` line and exit status 0: while a Gradle build,
    Android Studio or IntelliJ IDEA runs, or while a Gradle or Kotlin background
    process (daemon) used any CPU time in a two-second check;
  - otherwise stops the idle daemons with `SIGTERM` (a `STOP` line); Gradle
    starts them again on the next build;
  - failure, exit status 1: daemons still running ten seconds later; the
    folder is kept;
  - `--dry-run`: prints `WOULD STOP` and `WOULD REMOVE`, changes nothing;
  - `--report FILE`: `count` is 1 when the folder was deleted, `kb` the space
    freed;
  - settings: `DISKCLEANER_GRADLE_CLIENTS` and `DISKCLEANER_GRADLE_DAEMONS`
    replace the patterns of the programs it looks for.

## [0.5.0] - 2026-10-15

Upgrade with `go install github.com/botforge-pro/diskcleaner@v0.5.0`. The steps
added in earlier versions remove the same things as in 0.4.0; the only change
for them is the home folder message under Changed.

### Added

- `diskcleaner run docker-images` deletes Docker images that no container uses,
  by running `docker image prune --all --force`; volumes, which hold data, are
  never touched. It runs only when `docker system df` reports space such images
  could free, and prints a `RUN` line with that size and Docker's own
  `Total reclaimed space` line; when they could free nothing, it prints only its
  `DOCKER` heading and exits with status 0. A removed image is pulled or built again the
  next time something needs it. If Docker Desktop is not running, it changes
  nothing and prints a `SKIPPED` line with exit status 0; if `docker` is not
  installed, it prints nothing and exits with status 0. A Docker that does not
  answer within `DISKCLEANER_DOCKER_TIMEOUT` seconds (default 30), or a prune
  that takes longer than `DISKCLEANER_DOCKER_PRUNE_TIMEOUT` seconds (default
  900), is stopped and reported as a failure with exit status 1. `--dry-run`
  prints a `WOULD RUN` line and counts the size Docker reports. With
  `--report FILE`, `count` is 1 when the prune succeeded and `kb` is how much
  the Docker Desktop data folder shrank. Right after a run that can be less
  than Docker reports, even 0, because Docker Desktop hands the space back to
  macOS gradually. `diskcleaner run docker-images` exits with status 2 and
  touches nothing when one of its two timeouts is not a whole number of seconds
  above zero or is more than 9223372036 seconds.
- `diskcleaner check STEP...`, for example
  `diskcleaner check go-cache docker-images`, takes one or more step names and
  changes nothing. It exits with status 0 when the timeouts of those steps are
  valid (only `docker-images` has any) and the home folder can be found;
  otherwise, and for a step name it does not know, it prints what is wrong, the
  same message `run` would, and exits with status 2. A script that runs several steps
  can call it first, so that a mistyped timeout stops it before anything is
  deleted. Other settings, such as the `DISKCLEANER_*_PROCESSES` patterns that
  tell which running programs make a step skip, are not checked ahead.

### Changed

- When the home folder cannot be determined, `diskcleaner run` now prints
  `STEP: no home directory:` and the reason, and exits with status 2, instead of
  reporting an unknown step.

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

[0.13.1]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.13.1
[0.13.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.13.0
[0.12.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.12.0
[0.11.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.11.0
[0.10.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.10.0
[0.9.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.9.0
[0.8.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.8.0
[0.7.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.7.0
[0.6.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.6.0
[0.5.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.5.0
[0.4.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.4.0
[0.3.1]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.3.1
[0.3.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.3.0
[0.2.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.2.0
[0.1.0]: https://github.com/botforge-pro/diskcleaner/releases/tag/v0.1.0
