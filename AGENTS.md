# diskcleaner

Read Wikilayer wiki 1025 via MCP before the first action.

## Who it is for

Developers: it removes what builds and development tools leave behind and
recreate on demand.

## Configuration is tuning

- It works out of the box, as universally as possible, with no configuration.
  A user never has to start by writing a config.
- On the first run, unless `-y` is passed, it may ask about each kind of
  removal ("remove this? [y/n]"), write the answers to its config and read the
  config from then on.
- The config selects where and which steps run and their thresholds. What
  counts as removable stays in code with its checks; a config cannot add a
  removal rule.
- During development the maintainer's own config is kept in the private
  `diskcleaner` repository, which runs this binary. Before 1.0, check how it
  behaves with no config at all.

## Porting from the private shell tool

Steps move here one at a time from the private shell `diskcleaner`, which stays
the entry point and calls this binary for each ported step, so the cleanup
works at every moment. In the end the private repository holds only the call of
this binary, the config and a README.
