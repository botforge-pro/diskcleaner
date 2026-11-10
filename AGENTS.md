# diskcleaner

Before the first action read the whole wiki «Как мы пишем код»:
wikilayer://wikilayer.org/smee-again/codestyle

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
- Before 1.0, check how it behaves with no config at all.
