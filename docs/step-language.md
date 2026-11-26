# The step language

Every cleanup step of diskcleaner is one YAML file, `builtin/NNN-<step>.yaml`,
and its cases are in `builtin/NNN-<step>_test.yaml` beside it. This document is
the language of those files: seven concepts, the invariants the engine keeps
for every step and the format of the cases. The built-in steps in `builtin/`
are written in it.

A step says *where* to look, *when* to leave things alone and *what* may go.
*How* to walk a folder safely, ask `lsof` which files processes hold open,
compare versions or measure freed space is the engine's, and no file can switch
it off.

## A step file

```yaml
# builtin/060-swiftpm-cache.yaml
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

A file's name is three digits, a dash and the step's name. Steps run in the
order of their files' names, so the digits are a step's place in a run, and
two files may not name one step. A setting `keep` of the step
`xcode-archives` is the config key `xcode-archives.keep` and the flag
`--xcode-archives.keep`. Kinds: `duration` (`20d`, `1h`), `timeout` (`30s`)
and `count` (`1`).

Global settings every step may use: `min-age` (duration) and `project-dirs`
(folders). A step may declare a setting of the same key as a global one; in
that step's file the key then names its own. Every setting a step declares is
used by its file and named in its README cells, where `{key}` is replaced by
the setting's key and default.

## The seven concepts

### 1. Value

A value is read when the step runs, and is unset, one string, or a set of
strings. Where a key wants a value, any form below is accepted.

| Form | Value |
|---|---|
| `~/x`, `/x` | a path |
| `x` | a path relative to the thing read at |
| `$VAR/x` | a path under the variable; unset when the variable is |
| `/x/*/y`, `/x/**/y` | a path with a glob: the set of what exists; `**` is any depth |
| `a or b or c` | the first of them that is set |
| `[a, b]` | the set of all of them; `[]` is the empty set |
| `'{x}'` | a placeholder (below) |
| `{run: [cmd, args], timeout}` | the command's single line of output; unset when it prints nothing |
| `{run: …, match: regex}` | group 1 of the last output line the regex matches |
| `{run: …, lines: true}` | the set of non-empty output lines |
| `{file: f.plist \| f.json, key: [a.b, c]}` | the first key present in the file; dotted keys reach inside |
| `{each: value, file, key, where: {k: v}}` | the key read in each path of a set, from those whose other keys equal the given values |
| `{name: regex}` | group 1 of the name of the thing read at |

A `run` may add `then: rel` (join a relative path to the answer) and
`absolute: true` (an answer that is not an absolute path is unset). A command
that is not installed makes its value unset, so a root asked of a tool passes
quietly where the tool is missing. A command that fails makes its value unset
when a choice follows it, in an `or` or a `first`; otherwise the step fails
with the command's output. A file that does
not exist, or lacks every key, is unset; one that cannot be read is a failure,
which at a candidate keeps the candidate and elsewhere stops the step.

A value is read *at* a thing. Values in a node's keys are read at the node's
folder, a `root` at its parent's; values in a `remove` entry, in `rank` and in
`keep` are read at each candidate.

Where a key wants a duration, a timeout or a count, it takes a literal (`10s`,
`2`) or a placeholder of a setting.

Placeholders name values by key:

- `{path}` and `{name}`: the node's folder and the last part of it;
- `{x}`: a value bound by `let: {x: …}` in this node or one above it, read in
  order, so a later key may use an earlier one;
- `{key}`: a setting, the step's own first and then a global one;
- `{uid}`: the user's numeric id.

A placeholder that is the whole value keeps its set. In a path, a set makes one
path per value, and a placeholder past the start of a path stands for one name:
a value holding `/` or being `..` fails the step. In a command or a process
pattern a placeholder must be one value; in a process pattern it is matched
literally.

### 2. Condition

Nodes and `remove` entries list conditions under `when:` (all must hold) and
`unless:` (none may hold).

| Atom | Holds when |
|---|---|
| `exists: p` | `p` exists, a link as a link; `../p` is beside what is asked about, `p/` must be a folder, a glob holds if anything matches |
| `above: [condition]` | a folder from the asked thing's parent up to the node's folder meets all of them |
| `value: v, is: x` | the value is `x`; `is: set` holds for any set value |
| `running: [regex \| {group: name}]` | a process's command line matches one of them |
| `succeeds: {run, timeout}` | the command exits 0 within the timeout |
| `alive: {name: regex}` | the process whose id is group 1 of the asked thing's name runs |
| `program: name` | the program is on `PATH` |
| `any: [condition]` | one of them holds |

A node's conditions ask about its folder, an entry's about the thing found.

What stops a node:

- with `says: text`, prints `SKIPPED <path>: text`;
- with `fails: text`, prints a failure and makes `clean` exit 1;
- with neither, passes quietly.

An entry says why it took nothing with its own `says` (concept 4). A
`succeeds` that times out always fails the step; its `fails`, at any depth,
is what that failure says. Process patterns are POSIX extended expressions,
as `pgrep` reads them.

A node's conditions are asked before it removes or runs anything, and again
before each removal and run inside it, so a simulator booted mid-run keeps the
rest of its caches. A node that removes what it finds asks them once its
search has found something, so a node with nothing to remove asks nothing and
says nothing.

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
`folders` setting that is missing fails, since the user named it. A relative
root found inside its parent's folder is never a link.

### 4. Candidates

`remove: .` is the node's folder itself; when the root as written is a link,
only the link goes. Otherwise `remove` is a list of entries:

```yaml
remove:
  - skip: pattern        # not taken, not entered
  - folders: pattern     # folders
  - files: pattern       # regular files
  - empty: pattern       # folders empty when found
  - {folders: pattern, match: regex, when, unless, says}
```

A pattern is a glob relative to the node's folder: `*` also matches names
starting with a dot, `**` as a whole part matches any depth, zero included,
and a trailing `/` names folders only. `match` adds a regex the found name must
match. An entry names a thing when its pattern and `match` fit it and its kind
fits the key; a link is never named, entered or followed.

Each taking entry searches the node's folder on its own and takes what it names
whose conditions hold. It never looks inside what it names, while the other
entries still do, so a cache inside a folder one entry took and a protection
kept is found by the entry that names the cache. A `skip` keeps every search
out of what it names; listed before an entry, it also keeps that entry from
taking it. A thing taken by several entries is taken once.

A thing no entry took prints the `says` of the first entry it failed, or else
of a `skip` that names it, if there is one.

`rank` keeps some candidates of each group and lets the rest go:

- `per: value` is the group; without `per` all candidates are one group;
- `order: value` sorts a group; versions compare part by part as numbers
  (`17.10` after `17.9`), dates as dates, the rest as text, and equal ones
  keep their names' order;
- `spare: {newest: count}` spares the newest of each group, which `order`
  must say;
  `spare: {in: value}` spares the candidates whose name is the last part of a
  value in the set; a name not on disk joins the group its `per` reads from
  the name.

A candidate whose `per` or `order` is unset stays. A candidate not spared goes
when it sorts below the newest spared one; with no `order`, every candidate not
spared goes. A group with nothing spared stays whole, and so does a group whose
deciding candidate is not on disk yet: the newest spared one, or with no
`order` every spared one. It says so when something would have gone. So an
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
change time of anything inside a folder, the folder included), `any-time` (for
a file the newest of access, change and status change; for a folder its change
time, since a search reads it), or a value read at the candidate, such as
`{file: Info.plist, key: [CreationDate]}`. What is kept for its age is kept
quietly; the other protections say why.

### 6. Action

A command that does the removal for its tool:

```yaml
run: [brew, cleanup, --prune=all]
dry-run: [--dry-run]           # appended in a dry run
timeout: '{timeout}'
estimate: {run, timeout, match: regex, units: binary}   # the size it would free
size: path                     # measured before and after; what it freed counts
shows: regex                   # its output lines worth printing
```

An `estimate` with `run` is read before the command; when its `match` finds
nothing or a size of zero, the command is not run, quietly. An `estimate`
without `run` is read from the command's own output: the dry run's in a dry
run, the run's otherwise. A step with `run` needs `dry-run`, `estimate` or
`size`, so that a dry run says what it would free; without `dry-run`, a dry
run reports the estimate or the size and runs nothing. `units: binary` reads
KB, MB and GB as KiB, MiB and GiB.

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
```

A step refers to one as `{group: xcode}` inside `running`.

## Invariants of the engine

No file can turn these off, and no file needs to ask for them.

- A root is resolved through its links first; a resolved root at `/`, at the
  home folder or above it is refused, and the step fails.
- Every candidate lies inside its node's folder: a `remove` pattern holds no
  `..` and no absolute path.
- Searches start from the resolved root in the case the disk has and never
  follow a link inside it.
- A mounted volume is never entered, and a folder holding one is kept.
- A folder holding a socket is kept.
- Only the running user's files are taken: a folder of another user's is not
  entered, and a folder holding another user's file is kept.
- Removal opens each folder on the way without following links, checks it is
  still on the volume of the node's folder and removes the entry in the opened
  folder; a folder's contents are removed the same way, a link inside it as a
  link. So a folder swapped for a link, or a volume mounted on it, between the
  scan and the removal stays, and a socket or another user's file that appears
  inside it stops its removal there.
- A file goes only while it is a regular file and, kept for its age, still old;
  a folder only while it is a folder; an empty folder only while it is empty.
- A dry run removes nothing, and what lies inside something already counted is
  not counted again, whichever node found it.
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
  says: ['new: a process still has it open']   # substrings of the step's notices
  shows: []                               # substrings of its output
  fails: []                               # substrings of its failures
  counts: 1
```

Absolute roots such as `/private/tmp` are made under `fs/` in the temporary
home. A files key ending in `.git/` makes a Git repository there.
