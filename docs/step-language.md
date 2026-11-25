# The step language

Every cleanup step of diskcleaner is one YAML file in `builtin/`, and its cases
are in the file beside it, `builtin/<step>_test.yaml`. This document is the
language those files are written in: seven concepts, the invariants the
engine keeps whatever a file says, and all 17 steps written in it.

A step says *what* to look at, *when* to leave it alone and *what* to remove.
*How* to walk a folder safely, ask `lsof`, compare versions or measure freed
space is the engine's, and no file can switch it off.

## The step header

Unchanged from the user's point of view: these keys name the step, its
settings and its README row.

| Key | Meaning |
|---|---|
| `order` | the step's place in a run, lowest first |
| `describes` | one line for `--help` and the README |
| `reversible` | whether what it removes comes back by itself |
| `readme` | `removes`, `keeps`, `can-lose`: README cells, with `{setting}` placeholders |
| `settings` | the step's own settings: `key`, `kind` (`duration`, `timeout`, `count`), `default`, `help` |

The step's name is its file's name; `min-age` and `project-dirs` are global
settings every step may use.

## The seven concepts

### 1. Value

A value is something read when the step runs. Wherever a key wants a value,
any of these forms is accepted:

| Form | Value |
|---|---|
| `~/x`, `/x`, `$VAR/x` | a path; `$VAR` unset makes the value unset |
| `a or b or c` | the first of several that is set |
| `{run: [cmd, args], timeout}` | the command's output, one line; failing or empty, the value is unset |
| `{run: …, match: regex}` | the first group of the last line that matches |
| `{run: …, lines: true}` | every non-empty line: a set |
| `{file: x.plist \| x.json, key: [a.b, c]}` | the first key present in a file of the current folder |
| `{setting: key}` | a setting; a setting of folders is a set |
| `{name: regex}` | the first group of the current folder's name |

A value of paths may end in `then: rel` to join a path to it, and
`absolute: true` makes a relative answer unset. `{uid}` in a path is the
user's id. Inside a node, `{path}` is its folder and `{x}` is the value bound
by `let: {x: …}` (see Scope).

### 2. Condition

A condition is asked of a folder or a file. A node or a pattern lists them
under `when:` (all must hold) and `unless:` (none may hold).

| Atom | Holds when |
|---|---|
| `exists: p` | the path `p` exists, relative to the thing asked about; `../p` is beside it, `p/` must be a folder; a glob holds if anything matches |
| `exists-any: [p, q]` | one of them exists |
| `above: [condition]` | some folder above, up to the node's folder, meets all of them |
| `value: v` with `is: x` | the value is `x` (`is: set` holds for any set value) |
| `running: [regex \| {group: name}]` | a process's command line matches one of them |
| `succeeds: {run, timeout}` | the command exits 0 within its timeout |
| `alive: {name: regex}` | the process whose id is the first group of the name runs |
| `program: name` | the program is on `PATH` |
| `any: [condition]` | one of them holds |

A condition that stops a node or a pattern may carry `says: text`, printed as
`SKIPPED`; without it the node passes quietly. `fails: text` reports a
failure instead, and `succeeds` that times out always fails. Conditions of a
node are asked once when it is entered and again before each removal inside
it, so a simulator booted mid-run keeps the rest of its caches.

Process patterns are POSIX extended expressions, as `pgrep` reads them.
Groups are named lists of patterns in `builtin/processes.yaml`, such as
`xcode` and `swiftpm`, so a tool's process names are written once.

### 3. Scope

A step is a tree of nodes, and every node has one schema:

```yaml
root: value        # where this node works; a set makes one node per folder
each: glob         # or: one node for each folder under the parent's matching
first: [node]      # or: the first child whose root is set
let: {x: value}    # values read in the node's folder, usable as {x}
when: [condition]
unless: [condition]
stop: ...          # an action on processes, before anything is removed
remove: ...        # candidates (concept 4)
run: ...           # a command (concept 6)
keep: ...          # protections (concept 5)
do: [node]         # child nodes, in order
```

The step file itself is the root node. A node without `root`, `each` or
`first` works in its parent's folder.

### 4. Candidates

`remove:` names what may go.

- `remove: .` is the node's folder itself.
- `remove: {folders: [pattern], files: [pattern], empty: [pattern]}` are the
  folders, the regular files and the empty folders matching the patterns,
  relative to the node's folder. One step may name all three; what lies
  inside a candidate is not a candidate of its own.

A pattern is a glob, `**` standing for any depth as a whole part, or a
mapping `{glob | match: regex, when, unless, says}` that adds conditions on
the thing found.

| Key beside the patterns | Meaning |
|---|---|
| `outside: [name]` | folders a search does not enter, whatever their depth |
| `except: [pattern]` | paths never taken, `.*` at the top, `**/x` anywhere |
| `rank: {group, order, keep}` | of the candidates of one group, keeps some and removes the rest (below) |
| `others: text` | what to say of the folders right in the node's folder that no pattern took (open point 2) |

`rank` turns several candidates into one decision. `group` and `order` are
values read for each candidate. `keep` is either `{newest: count}` or
`{in: value}`, a set of names that stay. A candidate goes when it is not
kept and, with an `order`, sorts below the newest kept one; without an
`order`, every candidate not kept goes. A group is left whole until the
candidate that decides the cut exists: the newest kept one, or with no
order every kept one, so data an app has not yet copied stays.

### 5. Keep

`keep:` lists protections beyond the engine's invariants.

| Key | Keeps |
|---|---|
| `age: {of: clock, within: setting}` | what is not older than the setting; `of` is `mtime`, `newest-write` (inside a folder), `any-time` (access, change or write) or a value read from the candidate |
| `open: true` | what a process has open, anything inside a folder included |
| `open: [pattern]` | a folder while a process has a file matching a pattern inside it open |
| `tracked: true` | what holds a file Git tracks |

Age refusals are quiet; the others say why.

### 6. Action

`run:` is a command that removes for the tool, such as `brew cleanup`:

```yaml
run: [cmd, args]
dry-run: [--dry-run]       # appended in a dry run
timeout: setting or 10s
estimate: value            # the size it would free, read before running
size: path                 # measured before and after to count what it freed
shows: regex               # its output lines worth printing
```

A dry run needs `dry-run`, `estimate` or `size`, so it can say what it would
free. `stop:` acts on processes before removing:

```yaml
stop: {running: [pattern], idle-for: 2s, timeout: 10s, names: text, says: text}
```

It samples the CPU time of the processes for `idle-for`; one that used the
CPU keeps everything and `says` why; the others are stopped with TERM and
waited for, and one alive after `timeout` fails the step.

### 7. Process groups

`builtin/processes.yaml` names lists of process patterns. A step refers to
them as `{group: xcode}` in `running`, so the patterns of Xcode, SwiftPM or
Gradle are written once for every step that waits for them.

## Invariants of the engine

No file can turn these off, and no file needs to ask for them.

- A root at `/`, at the home folder or above it is refused.
- Searches start from the root with links resolved and in the case the disk
  has, and never follow a link inside it; a link found is removed as a link.
- A mounted volume is never entered, and a folder holding one is kept.
- A folder holding a socket is kept.
- Only the running user's files are removed.
- Removal opens each folder on the way without following links, checks it is
  still on the same volume and removes the entry in the opened folder, so a
  folder swapped for a link between the scan and the removal stays.
- A file is removed only while it is still a regular file, a folder only
  while it is still a folder, and an empty folder only while still empty.
- In a dry run nothing is removed, and what lies inside something already
  counted is not counted again.
- `lsof` is asked by file identity as well as by name, so a file with several
  names is open whichever name it was opened by.

## The 17 steps

### projects

```yaml
order: 10
describes: build output, Python caches, node_modules and Git leftovers in your projects
reversible: true
readme: {removes: ..., keeps: ..., can-lose: ...}   # unchanged
settings: [git-tmp-min-age, lfs-timeout]            # unchanged

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
        size: .git/lfs
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

The four `each` parts of today's file become one `each` with four children:
the repository test is written once.

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
    group: {file: Info.plist, key: [ApplicationProperties.CFBundleIdentifier, Name]}
    order: {file: Info.plist, key: [CreationDate]}
    keep: {newest: keep}
keep: {age: {of: {file: Info.plist, key: [CreationDate]}, within: min-age}}
```

### temp-leftovers

```yaml
root: {run: [getconf, DARWIN_USER_TEMP_DIR]}
remove: {folders: ['go-build*', 'go-link-*', 'XcodeDistPipeline.*']}
keep: {age: {of: newest-write, within: min-age}, open: true}
```

Sockets and mounted volumes are invariants now, so the file no longer asks.

### system-temp

```yaml
root: /private/tmp
remove:
  files: ['**/*']
  empty: ['**/*']
  except: ['.*', 'claude-*', '**/.X*-lock', '**/.X11-unix', '**/.ICE-unix', '**/.font-unix', '**/.XIM-unix', '**/quota.user', '**/quota.group', '**/.vfs_rsrc_streams_*']
keep: {age: {of: any-time, within: 3d}, open: true}
```

What `untouched` did by itself is now the engine's for every step (only the
user's files, never through a link, removal in the opened folder, mounted
volumes and sockets) or a general key (`any-time`, `empty`, `except`).

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
    any:
      - {run: [mdfind, 'kMDItemContentType == "com.apple.application-bundle"'], lines: true, fails: Spotlight lists no apps, so IDEs outside the Applications folders and JetBrains Toolbox cannot be found; is its indexing off?}
      - /Applications/*.app
      - /Applications/*/*.app
      - ~/Applications/*.app
      - ~/Applications/*/*.app
      - ~/Library/Application Support/JetBrains/Toolbox/apps/**/*.app
do:
  - root: ~/Library/Application Support/*, ~/Library/Caches/*, ~/Library/Logs/*
    remove:
      folders: ['*']
      rank:
        group: {name: '^([A-Za-z]+)[0-9]'}
        order: {name: '^[A-Za-z]+([0-9.]+)$'}
        keep: {in: {apps: '{apps}', file: Contents/Resources/product-info.json, key: [dataDirectoryName], where: {productVendor: '{parent}'}}}
  - root: ~/Library/Application Support/*/CachedData
    remove:
      folders: ['*']
      rank:
        keep: {in: {apps: '{apps}', file: Contents/Resources/app/product.json, key: [commit], where: {nameShort: '{parent}'}}}
```

Open point, see below.

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

### claude-logs and codex-logs

```yaml
root: $CLAUDE_CONFIG_DIR/projects or ~/.claude/projects    # codex: $CODEX_HOME/sessions or ~/.codex/sessions
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

## What goes away

| Today | Becomes |
|---|---|
| `needs`, `ready`, `busy`, `each.busy`, `each.value`, `each.has`, `remove.has` | `when` and `unless` with condition atoms |
| `has`, `beside`, `beside-any`, `or-above`, `Mark`, `Group` | `exists`, `exists-any`, `../`, `above`, `any`, `value` |
| `parts`, `each`, a root choice with its own remove, `Command.Root` | nodes: `each`, `first`, `do`, `root` |
| `newest`, `superseded`, `apps` | `rank` with `{newest}` or `{in}` and a value set |
| `untouched`, `except-at-top` | invariants, `files`/`empty`, `except`, `any-time` |
| `keep: alive`, `keep: sockets`, `keep: mounts` | the condition `alive`; invariants |
| `groups` written in Go | `builtin/processes.yaml` |
| `size: {measure: root}`, `remove: root` | `size: '{path}'`, `remove: .` |

## Open points

1. `ide-old-versions` needs a set of values read from many files with a
   filter (`where`) and a root that is a glob of several folders. That is the
   one place the language reaches for a query, and it is the place to judge
   whether `rank: {keep: {in: …}}` is the right concept or the app-owned
   folders should be a value of their own.
2. Today's `others:` (say what in the root no pattern took) has one user,
   `xcode-derived-data`; it may be a `says` on a catch-all pattern instead.
3. `system-temp` printed one line for every file kept for its age; with age
   refusals quiet everywhere it prints none, as the other steps.
