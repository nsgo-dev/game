### Added

- `template/coop2d`: a template, `game new -template coop2d`, with the authoritative rules of a 2D co-op room for one to four players: room codes and invite links, a fixed tick, leaving and rejoining within a grace period, a teammate reviving a downed player, and a solo respawn, tuned by a versioned `coop.yaml` and covered by headless tests. It has no network transport yet (#1959).
