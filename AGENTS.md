# diskcleaner

Before the first action read the whole wiki «Как мы пишем код»:
wikilayer://wikilayer.org/smee-again/codestyle

## Who it is for

Developers and the agents working for them: it removes what builds and
development tools leave behind and recreate on demand. Agents run it most, so
it never asks in the terminal, and `--help` and `config` tell everything needed
to use it.

## Configuration is tuning

- It works out of the box, on any developer's Mac, with no configuration: where
  a tool keeps its data is asked of the tool, not written in.
- Every setting has one entry that gives its config key, its flag and its help
  line: a step's own settings in its `builtin/<step>.yaml`, the settings of all
  steps in `internal/config`. The config file and the flags set the same keys.
- Each step runs from `builtin/<step>.yaml`: the names, paths, patterns and
  commands are in the file, and Go holds only the ways of applying them. Its
  cases are in `builtin/<step>_test.yaml`, and the README's table of steps is
  made from these files by `make readme`.
- The config selects which steps run, where projects are and the thresholds.
- Every step runs unless turned off, a step that deletes what cannot come back
  included; such a step is marked as one in `--help` and in the README.
