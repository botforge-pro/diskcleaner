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
| `folders` | a list of folders | `~/Developer` |

Global settings every step may use: `min-age` (duration) and `project-dirs`
(folders). A step may declare a setting of the same key as a global one; in
that step's file the key then names its own. Every setting a step declares is
used by its file, and `{key}` in a README cell is replaced by the setting's key
and default.

## The seven concepts

### 1. Value

A value is read when the step runs, and is unset, one string, or a set of
strings. Where a key wants a value, any form below is accepted.

| Form | Value |
|---|---|
| `~/x`, `/x` | a path |
| `x` | a path relative to the parent node's folder |
| `$VAR/x` | a path under the variable; unset when the variable is |
| `/x/*/y` | a path with a glob: the set of what exists |
| `a or b or c` | the first of them that is set |
| `[a, b]` | the set of all of them |
| `'{x}'` | a placeholder (below) |
| `{run: [cmd, args], timeout}` | the command's single line of output; unset when it fails or prints nothing |
| `{run: …, match: regex}` | group 1 of the last output line the regex matches |
| `{run: …, lines: true}` | the set of non-empty output lines |
| `{file: f.plist \| f.json, key: [a.b, c]}` | the first key present in the file; dotted keys reach inside |
| `{each: value, file, key, where: {k: v}}` | the key read in each path of a set, from those whose other keys equal the given values |
| `{name: regex}` | group 1 of the name of the thing read at |

`run` and `file` may add `then: rel` (join a relative path to the answer) and
`absolute: true` (an answer that is not an absolute path is unset). Where a key
wants a duration, a timeout or a count, it takes a literal (`10s`, `2`) or a
placeholder.

A value is read *at* a thing: `file` and `name` read there, and a relative
path starts there. Values in a node's keys are read at the node's folder;
values in a `remove` entry, in `rank` and in `keep` are read at each candidate.

Placeholders name values by key:

- `{path}` and `{name}`: the node's folder and the last part of it;
- `{x}`: a value bound by `let: {x: …}` in this node or one above it, read in
  order, so a later key may use an earlier one;
- `{key}`: a setting, the step's own first and then a global one;
- `{uid}`: the user's numeric id.

A placeholder that is the whole value keeps its set; inside a longer string
it is substituted, and a set there is refused when the file is loaded. Inside
a process pattern it is substituted and escaped, so `'{path}/'` matches that
folder literally.

### 2. Condition

Nodes and `remove` entries list conditions under `when:` (all must hold) and
`unless:` (none may hold).

| Atom | Holds when |
|---|---|
| `exists: p` | `p` exists; `../p` is beside what is asked about, `p/` must be a folder, a glob holds if anything matches |
| `above: [condition]` | a folder from the asked thing's parent up to the node's folder meets all of them |
| `value: v, is: x` | the value is `x`; `is: set` holds for any set value |
| `running: [regex \| {group: name}]` | a process's command line matches one of them |
| `succeeds: {run, timeout}` | the command exits 0 within the timeout |
| `alive: {name: regex}` | the process whose id is group 1 of the asked thing's name runs |
| `program: name` | the program is on `PATH` |
| `any: [condition]` | one of them holds |

A node's conditions ask about its folder, an entry's about the thing found.

What stops a node or an entry:

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
| `first: [node]` | the first child whose `root` is set is the node |
| `let: {x: value}` | values usable as `{x}` here and below |
| `when`, `unless` | conditions (concept 2) |
| `stop` | an action on processes before anything is removed (concept 6) |
| `remove` | candidates (concept 4) |
| `rank` | which candidates of a group stay (concept 4) |
| `run` | a command (concept 6) |
| `keep` | protections (concept 5) |
| `do: [node]` | child nodes, run in order |

A node without `root` or `first` works in its parent's folder. A root that is
unset, or set and missing on disk, makes the node pass quietly; a folder of a
`folders` setting that is missing fails, since the user named it.

### 4. Candidates

`remove: .` is the node's folder itself. Otherwise `remove` is a list of
entries, read in order:

```yaml
remove:
  - skip: pattern        # not taken
  - folders: pattern     # folders
  - files: pattern       # regular files
  - empty: pattern       # folders that are empty
  - {folders: pattern, match: regex, when, unless, says}
```

A pattern is a glob relative to the node's folder: `*` also matches names
starting with a dot, `**` as a whole part matches any depth, zero included,
and a trailing `/` names folders only. `match` adds a regex the found name must
match. An entry names a thing when its pattern and `match` fit it and its kind
fits the key.

The search walks down from the node's folder. Each thing found goes to the
entries that name it, in order. The first one whose conditions hold decides:
`skip` leaves it, the others take it, and in both cases the search does not go
inside. A thing no entry decides is not taken, and the search enters it if it
is a folder. When it ends untaken, the `says` of an entry it failed, or of the
`skip` that decided it, is printed. An empty folder is judged after what was
inside it.

`rank` keeps some candidates of each group and lets the rest go:

- `per: value` is the group; without `per` all candidates are one group;
- `order: value` sorts a group; versions compare part by part as numbers
  (`17.10` after `17.9`), dates as dates, the rest as text, and equal ones
  keep their names' order;
- `spare: {newest: count}` spares the newest of each group;
  `spare: {in: value}` spares the candidates whose name is in the set.

A candidate with `per` or `order` unset stays. A candidate not spared goes when
it sorts below the newest spared one; with no `order`, every candidate not
spared goes. A group stays whole until the candidate that decides the cut
exists: the newest spared one, or with no `order` every spared one. So an
app's data stays until the version that replaces it has made its own.
Protections (concept 5) apply after the ranking: a candidate kept for its age
still counts among the newest.

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
newest of access, change and status change), or a value read at the
candidate, such as `{file: Info.plist, key: [CreationDate]}`. What is kept for
its age is kept quietly; the other protections say why.

### 6. Action

A command that does the removal for its tool:

```yaml
run: [brew, cleanup, --prune=all]
dry-run: [--dry-run]           # appended in a dry run
timeout: '{timeout}'
estimate: value                # the size it would free, read before running; units: binary
size: path                     # measured before and after; what it freed counts
shows: regex                   # its output lines worth printing
```

An `estimate` without `run` reads the output of the dry run. When the estimate
is unset or zero, the command is not run, quietly. A step with `run` needs
`dry-run`, `estimate` or `size`, so that a dry run says what it would free;
without `dry-run`, a dry run reports the estimate and runs nothing.
`units: binary` reads KB, MB and GB as KiB, MiB and GiB.

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

- A root is resolved through its links first; a resolved root at `/`, at the
  home folder or above it is refused, and the step fails.
- Every candidate lies inside its node's folder: a `remove` pattern holds no
  `..` and no absolute path.
- Searches start from the resolved root in the case the disk has and never
  follow a link inside it; a link found is removed as a link.
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

The header keys of every step stay as they are and are left out below, except
that a README cell names a root in its own words, not with `{root}`.

### projects

```yaml
root: '{project-dirs}'
do:
  - root: '*'
    when: [{exists: .git/}]
    do:
      - remove:
          - {folders: '**/.build', when: [{exists: ../Package.swift}], says: no Package.swift next to it}
          - skip: '**/.build'
          - skip: '**/.git'
          - skip: '**/node_modules'
          - folders: '**/build'
            when: [{any: [{exists: ../build.gradle}, {exists: ../build.gradle.kts}, {exists: ../settings.gradle}, {exists: ../settings.gradle.kts}]}]
          - folders: '**/__pycache__'
        keep: {age: {of: newest-write, within: '{min-age}'}, open: true, tracked: true}
      - when: [{exists: gradlew}]
        remove: [{skip: '**/.git'}, {skip: '**/node_modules'}, {files: '**/*.apk'}, {files: '**/*.aab'}]
        keep: {age: {of: mtime, within: '{min-age}'}, open: true, tracked: true}
      - remove:
          - files: .git/objects/tmp_*
          - files: .git/objects/*/tmp_*
          - {files: .git/objects/pack/.tmp-*, unless: [{alive: {name: '^\.tmp-([0-9]+)-'}}], says: the git repack that writes it is still running}
        keep: {age: {of: mtime, within: '{git-tmp-min-age}'}, open: true}
      - when: [{exists: .git/lfs/objects/*/*/*}]
        run: [git, -C, '{path}', lfs, prune, --verify-remote]
        timeout: '{lfs-timeout}'
        estimate: {run: [git, -C, '{path}', lfs, prune, --dry-run, --verify-remote], timeout: '{lfs-timeout}', match: 'would be pruned \(([^)]*)\)'}
        size: .git/lfs
  - remove:
      - folders: '**/node_modules'
        when:
          - any:
              - {exists: ../package-lock.json}
              - {exists: ../npm-shrinkwrap.json}
              - {exists: ../yarn.lock}
              - {exists: ../pnpm-lock.yaml}
              - above:
                  - {any: [{exists: package-lock.json}, {exists: npm-shrinkwrap.json}, {exists: yarn.lock}, {exists: pnpm-lock.yaml}]}
                  - {any: [{exists: pnpm-workspace.yaml}, {value: {file: package.json, key: [workspaces]}, is: set}]}
        says: no lockfile, reinstall would not restore the same versions
    keep: {age: {of: newest-write, within: '{min-age}'}, open: true, tracked: true}
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
  - {succeeds: {run: [docker, info], timeout: '{timeout}'}, fails: the Docker daemon is hung, restart Docker Desktop}
run: [docker, image, prune, --all, --force]
timeout: '{prune-timeout}'
estimate: {run: [docker, system, df, --format, "{{.Type}}\t{{.Reclaimable}}"], timeout: '{timeout}', match: '^Images\t([^ ]+)'}
size: ~/Library/Containers/com.docker.docker
shows: '^Total reclaimed space'
```

`{{.Type}}` is Docker's own template: a placeholder is one name in single
braces.

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
timeout: '{timeout}'
estimate: {match: '(?:would free|has freed) approximately (.+?) of disk space', units: binary}
```

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
      - folders: '*.noindex'
      - folders: SDKExplicitPrecompiledModules
      - {folders: '*', match: '^.+-[a-z]{28}$', when: [{exists: info.plist}]}
      - {skip: '*/', says: not made by Xcode}
  - root: ~/Library/Developer/Xcode/DerivedData
    remove: [{folders: '*'}]
```

### xcode-archives

```yaml
root: ~/Library/Developer/Xcode/Archives
unless: [{running: [{group: xcode}], says: Xcode or an Xcode build is running}]
remove: [{folders: '*.xcarchive'}, {folders: '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]/*.xcarchive'}]
rank:
  per: {file: Info.plist, key: [ApplicationProperties.CFBundleIdentifier, Name]}
  order: {file: Info.plist, key: [CreationDate]}
  spare: {newest: '{keep}'}
keep: {age: {of: {file: Info.plist, key: [CreationDate]}, within: '{min-age}'}}
```

### temp-leftovers

```yaml
root: {run: [getconf, DARWIN_USER_TEMP_DIR]}
remove: [{folders: 'go-build*'}, {folders: 'go-link-*'}, {folders: 'XcodeDistPipeline.*'}]
keep: {age: {of: newest-write, within: '{min-age}'}, open: true}
```

### system-temp

```yaml
root: /private/tmp
remove:
  - skip: '.*'
  - skip: 'claude-*'
  - skip: '**/.X*-lock'
  - skip: '**/.X11-unix'
  - skip: '**/.ICE-unix'
  - skip: '**/.font-unix'
  - skip: '**/.XIM-unix'
  - skip: '**/quota.user'
  - skip: '**/quota.group'
  - skip: '**/.vfs_rsrc_streams_*'
  - files: '**/*'
  - empty: '**/*'
keep: {age: {of: any-time, within: 3d}, open: true}
```

### code-sign-clones

```yaml
root: {run: [getconf, DARWIN_USER_TEMP_DIR], then: ../X}
remove: [{folders: '*.code_sign_clone/code_sign_clone.*'}]
keep:
  age: {of: mtime, within: '{min-age}'}
  open: ['*/Contents/MacOS/*', '*/Contents/Frameworks/*.framework/Versions/*/*']
```

### ide-old-versions

```yaml
let:
  spotlight: {run: [mdfind, 'kMDItemContentType == "com.apple.application-bundle"'], lines: true}
  apps:
    - '{spotlight}'
    - /Applications/*.app
    - /Applications/*/*.app
    - ~/Applications/*.app
    - ~/Applications/*/*.app
    - ~/Library/Application Support/JetBrains/Toolbox/apps/**/*.app
when: [{value: '{spotlight}', is: set, fails: Spotlight lists no apps, so IDEs outside the Applications folders and JetBrains Toolbox cannot be found; is its indexing off?}]
do:
  - root: [~/Library/Application Support/*, ~/Library/Caches/*, ~/Library/Logs/*]
    remove: [{folders: '*'}]
    rank:
      per: {name: '^([A-Za-z]+)[0-9]'}
      order: {name: '^[A-Za-z]+([0-9][0-9.]*)$'}
      spare: {in: {each: '{apps}', file: Contents/Resources/product-info.json, key: [dataDirectoryName], where: {productVendor: '{name}'}}}
  - root: ~/Library/Application Support/*
    let: {app: '{name}'}
    do:
      - root: CachedData
        remove: [{folders: '*'}]
        rank:
          spare: {in: {each: '{apps}', file: Contents/Resources/app/product.json, key: [commit], where: {nameShort: '{app}'}}}
```

### simulator-caches

```yaml
root: ~/Library/Developer/CoreSimulator/Devices
unless: [{running: [xcodebuild, simctl, '/Simulator\.app/', '/DeviceHub\.app/'], says: Simulator, DeviceHub, simctl or an Xcode build is running}]
do:
  - root: '*'
    when:
      - {exists: data/}
      - {value: {run: [plutil, -extract, state, raw, -o, -, '{path}/device.plist']}, is: '1', says: the simulator is not shut down}
    unless: [{running: ['{path}/'], says: a process of this simulator is running}]
    remove:
      - folders: data/**/Library/Caches/**/com.apple.e5rt.e5bundlecache
      - folders: data/Library/Application Support/PRBPosterExtensionDataStore
```

### emulator-snapshots

```yaml
root: $ANDROID_AVD_HOME or $ANDROID_USER_HOME/avd or ~/.android/avd
unless: [{running: ['qemu-system-', '/emulator/emulator( |$)'], says: an Android emulator is running}]
remove: [{folders: '*.avd/snapshots/*'}]
```

### claude-logs, codex-logs

```yaml
root: $CLAUDE_CONFIG_DIR/projects or ~/.claude/projects     # codex-logs: $CODEX_HOME/sessions or ~/.codex/sessions
remove: [{files: '**/*.jsonl'}]
keep: {age: {of: mtime, within: '{min-age}'}, open: true, tracked: true}
```

### claude-scratchpads

```yaml
root: /private/tmp/claude-{uid}
unless: [{running: [{group: swiftpm}, xcodebuild], says: a Swift package build or xcodebuild is running}]
remove:
  - {folders: '**/scratchpad/**/Build', when: [{exists: Intermediates.noindex/}]}
  - {folders: '**/scratchpad/**/Index.noindex', when: [{exists: ../Build/Intermediates.noindex/}]}
  - {folders: '**/scratchpad/**/ModuleCache.noindex', when: [{exists: ../Build/Intermediates.noindex/}]}
  - {folders: '**/scratchpad/**/SourcePackages', when: [{exists: ../Build/Intermediates.noindex/}]}
  - {folders: '**/scratchpad/**/checkouts', when: [{exists: ../workspace-state.json}]}
  - {folders: '**/scratchpad/**/repositories', when: [{exists: ../workspace-state.json}, {exists: ../checkouts/}]}
  - {folders: '**/scratchpad/**/.build', when: [{exists: ../Package.swift}], says: no Package.swift next to it}
keep: {tracked: true}
```

## Open point

`system-temp` printed one line for the files kept for their age; with age
refusals quiet everywhere, it prints none, as the other steps.

## Appendix: from the current files

| Current key | In this language |
|---|---|
| `needs: [x]` | `when: [{program: x}]` |
| `busy: {processes, groups, says}` | `unless: [{running: […], says}]` |
| `ready: {run, timeout, hung, down}` | `when: [{running}, {succeeds, fails}]` |
| `each: {folders, has, value, busy}` | a node with a relative `root`, `when`, `unless` |
| `has`, `beside`, `beside-any`, `or-above` | `exists`, `../`, `any`, `above` |
| `parts` | `do` |
| a root choice with its own `remove` | `first` |
| `remove: root`, `size: {measure: root}` | `remove: .`, `size: '{path}'` |
| `except`, `outside`, `others`, `except-at-top` | `skip` entries in `remove` |
| `estimate: {none}` | an estimate that is unset or zero |
| a setting by its bare name | `'{key}'` |
| `{root}` in README cells | the root in the cell's own words |
| `newest`, `superseded`, `apps` | `rank` with `spare: {newest}` or `{in}`, `let`, `{each, file, key, where}` |
| `untouched` | invariants, `files`, `empty`, `skip`, `any-time` |
| `keep: {alive}`, `{sockets}`, `{mounts}` | the condition `alive`; invariants |
| groups written in Go | `builtin/processes.yaml` |
