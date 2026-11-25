# The step language

Every cleanup step of diskcleaner is one YAML file, `builtin/<step>.yaml`, and
its cases are in `builtin/<step>_test.yaml` beside it. This document is the
language of those files: seven concepts, the invariants the engine keeps for
every step, the format of the cases, and all 17 built-in steps written in it.

A step says *where* to look, *when* to leave things alone and *what* may go.
*How* to walk a folder safely, ask `lsof` which files processes hold open,
compare versions or measure freed space is the engine's, and no file can switch
it off.

## A step file

```yaml
order: 60                         # place in a run, lowest first; unique
describes: the Swift Package Manager cache
reversible: true                  # what it removes is rebuilt or downloaded again on demand
readme:                           # cells of the README's table of steps
  removes: the SwiftPM package cache          # defaults to describes
  keeps: all while Swift or Xcode build
  can-lose: nothing                           # optional
settings:                         # the step's own settings, optional
  - {key: keep, kind: count, default: '1', help: keep this many newest archives of each app}

root: ~/Library/Caches/org.swift.swiftpm      # the rest is a node (concept 3)
unless: [{running: [{group: swiftpm}, {group: xcode}], says: a Swift package build or Xcode is running}]
remove: .
```

The step's name is the file's name. A setting `keep` of the step
`xcode-archives` is the config key `xcode-archives.keep` and the flag
`--xcode-archives.keep`. Kinds:

| Kind | Holds | Example |
|---|---|---|
| `duration` | an age | `20d`, `1h` |
| `timeout` | how long a command may run | `30s`, `15m` |
| `count` | a whole number | `1` |

Global settings every step may use: `min-age` (duration) and `project-dirs`
(folders). `{key}` in a README cell is replaced by the setting's key and
default, and every setting of a step must be used by its file.

Where a key wants a duration, a timeout or a count, it takes either a literal
(`10s`, `2`) or the name of a setting (`min-age`, `lfs-timeout`, `keep`);
anywhere else a setting is the value `{setting: key}`.

## The seven concepts

### 1. Value

A value is read when the step runs. Where a key wants a value, any form below
is accepted. A value is unset, one string, or a set of strings.

| Form | Value |
|---|---|
| `~/x`, `/x` | a path |
| `$VAR/x` | a path under the variable; unset when the variable is |
| `/x/*/y` | a path with a glob: the set of what exists |
| `a or b or c` | the first of them that is set |
| `[a, b]` | the set of all of them |
| `{run: [cmd, args], timeout}` | the command's single line of output; unset when it fails or prints nothing |
| `{run: …, match: regex}` | group 1 of the last output line the regex matches |
| `{run: …, lines: true}` | the set of non-empty output lines |
| `{file: f.plist \| f.json, key: [a.b, c]}` | the first key present in the file, read in the current folder; dotted keys reach inside |
| `{each: value, file, key, where: {k: v}}` | the key read in each path of a set, from those whose other keys equal the given values |
| `{name: regex}` | group 1 of the current folder's name |
| `{setting: key}` | a setting; `project-dirs` is a set |

`run` and `file` may add `then: rel` (join a relative path to the answer) and
`absolute: true` (an answer that is not an absolute path is unset). `{uid}` in
a path is the user's numeric id.

The *current folder* is the node's folder, or for a pattern's condition, the
thing found. Inside a node, `{path}` is its folder, `{name}` the last part of
it, and `{x}` a value bound by `let: {x: …}`. A placeholder inside a process
pattern is matched literally.

### 2. Condition

Nodes and patterns list conditions under `when:` (all must hold) and `unless:`
(none may hold).

| Atom | Holds when |
|---|---|
| `exists: p` | `p` exists, relative to the current folder; `../p` is beside it, `p/` must be a folder, a glob holds if anything matches |
| `exists-any: [p, q]` | one of them exists |
| `above: [condition]` | a folder from the current folder's parent up to the node's folder meets all of them |
| `value: v, is: x` | the value is `x`; `is: set` holds for any set value |
| `running: [regex \| {group: name}]` | a process's command line matches one of them |
| `succeeds: {run, timeout}` | the command exits 0 within the timeout |
| `alive: {name: regex}` | the process whose id is group 1 of the current folder's name runs |
| `program: name` | the program is on `PATH` |
| `any: [condition]` | one of them holds |

What stops a node or a pattern:

- with `says: text`, prints `SKIPPED <path>: text`;
- with `fails: text`, prints a failure and makes `clean` exit 1;
- with neither, passes quietly.

A `succeeds` that times out always fails. Process patterns are POSIX extended
expressions, as `pgrep` reads them. Conditions of a node are asked when it is
entered and again before each removal inside it, so a simulator booted mid-run
keeps the rest of its caches.

### 3. Scope

A step is a tree of nodes. Every node, the step file included, has this one
schema:

| Key | Meaning |
|---|---|
| `root: value` | the folder the node works in; a set makes one node per folder |
| `each: glob` | one node per folder under the parent's folder matching the glob |
| `first: [node]` | the first child whose `root` is set and exists is the node |
| `let: {x: value}` | values read in the node's folder, usable as `{x}` |
| `when`, `unless` | conditions (concept 2) |
| `stop` | an action on processes before anything is removed (concept 6) |
| `remove` | candidates (concept 4) |
| `run` | a command (concept 6) |
| `keep` | protections (concept 5) |
| `do: [node]` | child nodes, run in order |

A node without `root`, `each` or `first` works in its parent's folder. A root
that is not set, or set but missing on disk, makes the node pass quietly; a
folder from `project-dirs` that is missing fails, since the user named it.

### 4. Candidates

`remove:` names what may go: `remove: .` is the node's folder itself, and

```yaml
remove:
  folders: [pattern]   # folders
  files: [pattern]     # regular files
  empty: [pattern]     # folders that are empty
  outside: [name]      # folders no search enters, at any depth
  except: [pattern]    # paths never taken
  rank: {per, order, spare}
```

A pattern is relative to the node's folder: a glob, where `*` also matches
names starting with a dot and `**` as a whole part matches any depth, zero
included; or a mapping `{glob | match: regex, when, unless, says}` adding
conditions on the thing found. A search does not enter what it found, so what
lies inside a candidate is never a candidate of its own, and a name in
`outside` is still found when a pattern names it.

`rank` keeps some candidates of each group and lets the rest go:

- `per: value` is the group, read for each candidate; candidates without one
  stay;
- `order: value` sorts a group; versions compare part by part as numbers
  (`17.10` after `17.9`), dates as dates, the rest as text, and equal ones
  keep their names' order;
- `spare: {newest: count}` spares the newest of each group;
  `spare: {in: value}` spares the candidates whose name is in the set.

A candidate not spared goes when it sorts below the newest spared one; with
no `order`, every candidate not spared goes. A group stays whole until the
candidate that decides the cut exists, the newest spared one (or with no
`order`, every spared one), so an app's data stays until the version that
replaces it has made its own. Protections (concept 5) apply after the ranking:
a candidate kept for its age still counts among the newest.

### 5. Keep

`keep:` adds protections beyond the invariants.

| Key | Keeps |
|---|---|
| `age: {of: clock, within: duration}` | what is not older than the duration; the boundary itself stays |
| `open: true` | what a process has open, anything inside a folder included |
| `open: [pattern]` | a folder while a process has a file matching a pattern inside it open |
| `tracked: true` | what holds a file Git tracks |

Clocks: `mtime` (the thing's own change time), `newest-write` (the newest
change time of anything inside a folder, the folder included), `any-time` (the
newest of access, change and status change), or a value read from the
candidate, such as `{file: Info.plist, key: [CreationDate]}`. What is kept for
its age is kept quietly; the other protections say why.

### 6. Action

A command that does the removal for its tool:

```yaml
run: [brew, cleanup, --prune=all]
dry-run: [--dry-run]           # appended in a dry run; without it the dry run only estimates
timeout: timeout               # a timeout setting or a literal
estimate: value                # the size it would free, read before running: {run, match, units: binary}
size: path                     # measured before and after; what it freed counts
shows: regex                   # its output lines worth printing
```

A step with `run` needs `dry-run`, `estimate` or `size`, so that a dry run says
what it would free; `units: binary` reads KB, MB and GB as KiB, MiB and GiB.

Stopping processes before removal:

```yaml
stop: {running: [pattern], idle-for: 2s, timeout: 10s, names: idle Gradle daemons, says: a Gradle daemon is using the CPU}
```

The engine samples the CPU time of the processes over `idle-for`. If one used
the CPU, nothing is removed and `says` tells why; otherwise they get TERM, and
one still alive after `timeout` fails the step.

### 7. Process groups

`builtin/processes.yaml` names lists of process patterns, so the patterns of
one tool are written once:

```yaml
xcode: [xcodebuild, XCBBuildService, '/Xcode[^/]*\.app/Contents/MacOS/Xcode']
swiftpm: [swift-build, swift-package, swift-test, swift-run]
gradle: ['org\.gradle\.wrapper\.GradleWrapperMain', 'org\.gradle\.launcher\.GradleMain']
gradle-daemons: ['org\.gradle\.launcher\.daemon\.bootstrap\.GradleDaemon', 'org\.jetbrains\.kotlin\.daemon\.KotlinCompileDaemon']
jetbrains: [/Contents/MacOS/studio, /Contents/MacOS/idea]
```

A step refers to one as `{group: xcode}` inside `running`.

## Invariants of the engine

No file can turn these off, and no file needs to ask for them.

- A root at `/`, at the home folder or above it is refused, and the step fails.
- Searches start from the root with links resolved and in the case the disk
  has, and never follow a link inside it; a link found is removed as a link.
- A mounted volume is never entered, and a folder holding one is kept.
- A folder holding a socket is kept.
- Only the running user's files are removed.
- Removal opens each folder on the way without following links, checks it is
  still on the same volume and removes the entry in the opened folder, so a
  folder swapped for a link between the scan and the removal stays.
- A file goes only while it is a regular file, a folder only while it is a
  folder, an empty folder only while it is empty.
- A dry run removes nothing, and what lies inside something it already
  counted is not counted again.
- `lsof` is asked by file identity as well as by name, so a file with several
  names counts as open whichever name a process opened it by.

## Cases

`builtin/<step>_test.yaml` is a list of cases run against a temporary home.

```yaml
- case: a cache older than min-age goes and a young one stays
  files:                                  # paths under the temporary home
    Library/Caches/tool/old/data: 2d      # age of a file
    Library/Caches/tool/new/data:         # written now
    Library/Caches/tool/empty/: 3d        # a folder
    Library/Caches/tool/info.json: {age: 2d, text: '{"v": 1}'}
  links: {Library/link: Library/Caches/tool}
  env: {TOOL_HOME: ~/Library/Caches/tool}
  settings: {min-age: 1d}
  running: [/usr/bin/swift-build]          # or {command, busy, stays}
  commands:                               # command line → result; others are not installed
    tool --version: {stdout: '1.0'}
  open: [Library/Caches/tool/new/data]
  tracked: []
  later: 4d                               # run that much after the files were made
  dry-run: false
  removes: [Library/Caches/tool/old]      # required; [] with says, shows, fails or counts
  says: ['new: a process has it open']    # substrings of the step's notices
  shows: []                               # substrings of its output
  fails: []                               # substrings of its failures
  counts: 1
```

Absolute roots such as `/private/tmp` are made under `fs/` in the temporary
home. A files key ending in `.git/` makes a Git repository there.

## The 17 steps

The header keys (`order`, `describes`, `reversible`, `readme`, `settings`) of
every step stay as they are and are left out below.

### projects

```yaml
root: {setting: project-dirs}
do:
  - each: '*'
    when: [{exists: .git/}]
    do:
      - remove:
          folders:
            - {glob: '**/.build', when: [{exists: ../Package.swift}], says: no Package.swift next to it}
            - {glob: '**/build', when: [{exists-any: [../build.gradle, ../build.gradle.kts, ../settings.gradle, ../settings.gradle.kts]}]}
            - '**/__pycache__'
          outside: [.git, node_modules, .build]
        keep: {age: {of: newest-write, within: min-age}, open: true, tracked: true}
      - when: [{exists: gradlew}]
        remove: {files: ['**/*.apk', '**/*.aab'], outside: [.git, node_modules]}
        keep: {age: {of: mtime, within: min-age}, open: true, tracked: true}
      - remove:
          files:
            - .git/objects/tmp_*
            - .git/objects/*/tmp_*
            - {glob: .git/objects/pack/.tmp-*, unless: [{alive: {name: '^\.tmp-([0-9]+)-'}}], says: the git repack that writes it is still running}
        keep: {age: {of: mtime, within: git-tmp-min-age}, open: true}
      - when: [{exists: .git/lfs/objects/*/*/*}]
        run: [git, -C, '{path}', lfs, prune, --verify-remote]
        timeout: lfs-timeout
        estimate: {run: [git, -C, '{path}', lfs, prune, --dry-run, --verify-remote], timeout: lfs-timeout, match: 'would be pruned \(([^)]*)\)'}
        size: '{path}/.git/lfs'
  - remove:
      folders:
        - glob: '**/node_modules'
          when:
            - any:
                - {exists-any: [../package-lock.json, ../npm-shrinkwrap.json, ../yarn.lock, ../pnpm-lock.yaml]}
                - above:
                    - {exists-any: [package-lock.json, npm-shrinkwrap.json, yarn.lock, pnpm-lock.yaml]}
                    - any: [{exists: pnpm-workspace.yaml}, {value: {file: package.json, key: [workspaces]}, is: set}]
          says: no lockfile, reinstall would not restore the same versions
    keep: {age: {of: newest-write, within: min-age}, open: true, tracked: true}
```

### gradle-caches

```yaml
root: $GRADLE_USER_HOME/caches or ~/.gradle/caches
unless: [{running: [{group: gradle}, {group: jetbrains}], says: a Gradle build or Android Studio / IntelliJ IDEA is running}]
stop: {running: [{group: gradle-daemons}], idle-for: 2s, timeout: 10s, names: idle Gradle and Kotlin daemons, says: a Gradle or Kotlin daemon is using the CPU}
remove: .
```

### docker-images

```yaml
when:
  - {program: docker}
  - {running: ['com\.docker\.backend'], says: Docker Desktop is not running}
  - {succeeds: {run: [docker, info], timeout: timeout}, fails: the Docker daemon is hung, restart Docker Desktop}
run: [docker, image, prune, --all, --force]
timeout: prune-timeout
estimate: {run: [docker, system, df, --format, "{{.Type}}\t{{.Reclaimable}}"], timeout: timeout, match: '^Images\t([^ ]+)'}
size: ~/Library/Containers/com.docker.docker
shows: '^Total reclaimed space'
```

### go-cache

```yaml
when: [{program: go}]
unless: [{running: ['(^|/)go (build|test|run|install|get|mod|generate|vet|list)( |$)'], says: a go command is running}]
do:
  - {root: {run: [go, env, GOMODCACHE]}, run: [go, clean, -modcache], size: '{path}'}
  - {root: {run: [go, env, GOCACHE]}, run: [go, clean, -cache], size: '{path}'}
```

### homebrew

```yaml
when: [{program: brew}]
unless: [{running: ['/Library/Homebrew/brew\.(sh|rb)'], says: a brew command is running}]
run: [brew, cleanup, --prune=all]
dry-run: [--dry-run]
timeout: timeout
estimate: {match: '(?:would free|has freed) approximately (.+?) of disk space', units: binary}
```

With `dry-run` and no `estimate.run`, the estimate is read from the dry run's
own output.

### swiftpm-cache

```yaml
root: ~/Library/Caches/org.swift.swiftpm
unless: [{running: [{group: swiftpm}, {group: xcode}], says: a Swift package build, xcodebuild or Xcode is running}]
remove: .
```

### xcode-derived-data

```yaml
unless: [{running: [{group: xcode}], says: Xcode or an Xcode build is running}]
first:
  - root: {run: [defaults, read, com.apple.dt.Xcode, IDECustomDerivedDataLocation], absolute: true}
    remove:
      folders: ['*.noindex', SDKExplicitPrecompiledModules, {match: '^.+-[a-z]{28}$', when: [{exists: info.plist}]}]
      others: not made by Xcode
  - root: ~/Library/Developer/Xcode/DerivedData
    remove: {folders: ['*']}
```

### xcode-archives

```yaml
root: ~/Library/Developer/Xcode/Archives
unless: [{running: [{group: xcode}], says: Xcode or an Xcode build is running}]
remove:
  folders: ['*.xcarchive', '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]/*.xcarchive']
  rank:
    per: {file: Info.plist, key: [ApplicationProperties.CFBundleIdentifier, Name]}
    order: {file: Info.plist, key: [CreationDate]}
    spare: {newest: keep}
keep: {age: {of: {file: Info.plist, key: [CreationDate]}, within: min-age}}
```

### temp-leftovers

```yaml
root: {run: [getconf, DARWIN_USER_TEMP_DIR]}
remove: {folders: ['go-build*', 'go-link-*', 'XcodeDistPipeline.*']}
keep: {age: {of: newest-write, within: min-age}, open: true}
```

### system-temp

```yaml
root: /private/tmp
remove:
  files: ['**/*']
  empty: ['**/*']
  except: ['.*', 'claude-*', '**/.X*-lock', '**/.X11-unix', '**/.ICE-unix', '**/.font-unix', '**/.XIM-unix', '**/quota.user', '**/quota.group', '**/.vfs_rsrc_streams_*']
keep: {age: {of: any-time, within: 3d}, open: true}
```

### code-sign-clones

```yaml
root: {run: [getconf, DARWIN_USER_TEMP_DIR], then: ../X}
remove: {folders: ['*.code_sign_clone/code_sign_clone.*']}
keep:
  age: {of: mtime, within: min-age}
  open: ['*/Contents/MacOS/*', '*/Contents/Frameworks/*.framework/Versions/*/*']
```

### ide-old-versions

```yaml
let:
  apps:
    - {run: [mdfind, 'kMDItemContentType == "com.apple.application-bundle"'], lines: true, fails: Spotlight lists no apps, so IDEs outside the Applications folders and JetBrains Toolbox cannot be found; is its indexing off?}
    - /Applications/*.app
    - /Applications/*/*.app
    - ~/Applications/*.app
    - ~/Applications/*/*.app
    - ~/Library/Application Support/JetBrains/Toolbox/apps/**/*.app
do:
  - root: [~/Library/Application Support/*, ~/Library/Caches/*, ~/Library/Logs/*]
    remove:
      folders: ['*']
      rank:
        per: {name: '^([A-Za-z]+)[0-9]'}
        order: {name: '^[A-Za-z]+([0-9][0-9.]*)$'}
        spare: {in: {each: '{apps}', file: Contents/Resources/product-info.json, key: [dataDirectoryName], where: {productVendor: '{name}'}}}
  - root: ~/Library/Application Support/*/CachedData
    remove:
      folders: ['*']
      rank:
        spare: {in: {each: '{apps}', file: Contents/Resources/app/product.json, key: [commit], where: {nameShort: '{parent}'}}}
```

### simulator-caches

```yaml
root: ~/Library/Developer/CoreSimulator/Devices
unless: [{running: [{group: xcode}, simctl, '/Simulator\.app/', '/DeviceHub\.app/'], says: Simulator, DeviceHub, simctl or an Xcode build is running}]
do:
  - each: '*'
    when:
      - {exists: data/}
      - {value: {run: [plutil, -extract, state, raw, -o, -, '{path}/device.plist']}, is: '1', says: the simulator is not shut down}
    unless: [{running: ['{path}/'], says: a process of this simulator is running}]
    remove:
      folders:
        - data/**/Library/Caches/**/com.apple.e5rt.e5bundlecache
        - data/Library/Application Support/PRBPosterExtensionDataStore
```

### emulator-snapshots

```yaml
root: $ANDROID_AVD_HOME or $ANDROID_USER_HOME/avd or ~/.android/avd
unless: [{running: ['qemu-system-', '/emulator/emulator( |$)'], says: an Android emulator is running}]
remove: {folders: ['*.avd/snapshots/*']}
```

### claude-logs, codex-logs

```yaml
root: $CLAUDE_CONFIG_DIR/projects or ~/.claude/projects     # codex-logs: $CODEX_HOME/sessions or ~/.codex/sessions
remove: {files: ['**/*.jsonl']}
keep: {age: {of: mtime, within: min-age}, open: true, tracked: true}
```

### claude-scratchpads

```yaml
root: /private/tmp/claude-{uid}
unless: [{running: [{group: swiftpm}, xcodebuild], says: a Swift package build or xcodebuild is running}]
remove:
  folders:
    - {glob: '**/scratchpad/**/Build', when: [{exists: Intermediates.noindex/}]}
    - {glob: '**/scratchpad/**/Index.noindex', when: [{exists: ../Build/Intermediates.noindex/}]}
    - {glob: '**/scratchpad/**/ModuleCache.noindex', when: [{exists: ../Build/Intermediates.noindex/}]}
    - {glob: '**/scratchpad/**/SourcePackages', when: [{exists: ../Build/Intermediates.noindex/}]}
    - {glob: '**/scratchpad/**/checkouts', when: [{exists: ../workspace-state.json}]}
    - {glob: '**/scratchpad/**/repositories', when: [{exists: ../workspace-state.json}, {exists: ../checkouts/}]}
    - {glob: '**/scratchpad/**/.build', when: [{exists: ../Package.swift}], says: no Package.swift next to it}
keep: {tracked: true}
```

## Open points

1. `{parent}` in `ide-old-versions`: the name of the node folder's parent,
   used once. `{name}` of a node one level up would do the same if the root
   were the vendor folder and `each: CachedData`; to settle when the engine is
   built.
2. `others:` (what to say of the folders right in the node's folder that no
   pattern took) has one user, `xcode-derived-data`. It may become `says` on a
   catch-all pattern, `{glob: '*', unless: [...], says: not made by Xcode}`,
   once the cases show they read the same.
3. `system-temp` printed one line for the files kept for their age; with age
   refusals quiet everywhere, it prints none, as the other steps.

## Appendix: from the current files

| Current key | In this language |
|---|---|
| `needs: [x]` | `when: [{program: x}]` |
| `busy: {processes, groups, says}` | `unless: [{running: […], says}]` |
| `ready: {run, timeout, hung, down}` | `when: [{running}, {succeeds, fails}]` |
| `each: {folders, has, value, busy}` | a node with `each`, `when`, `unless` |
| `has`, `beside`, `beside-any`, `or-above` | `exists`, `../`, `exists-any`, `above`, `any` |
| `parts` | `do` |
| a root choice with its own `remove` | `first` |
| `remove: root`, `size: {measure: root}` | `remove: .`, `size: '{path}'` |
| `newest`, `superseded`, `apps` | `rank` with `spare: {newest}` or `{in}`, `let`, `{each, file, key, where}` |
| `untouched`, `except-at-top` | invariants, `files`, `empty`, `except`, `any-time` |
| `keep: {alive}`, `{sockets}`, `{mounts}` | the condition `alive`; invariants |
| groups written in Go | `builtin/processes.yaml` |
