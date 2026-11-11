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
- Every setting lives in one table in the code, which gives its config key, its
  flag and its help line; the config file and the flags set the same keys.
- The config selects which steps run, where projects are and the thresholds.
  What counts as removable, and what shows a tool is running, stays in code
  with its checks; a config cannot add a removal rule.
- Every step runs unless turned off, a step that deletes what cannot come back
  included: the owner decided on 2026-11-11, reversing 0.20.0, because with
  those steps off a full `clean` no longer freed the space a developer's Mac
  needs. Such a step is marked as one in `--help` and in the README.
