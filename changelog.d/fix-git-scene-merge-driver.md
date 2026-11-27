### Fixed

- `game/cmd/game`, `game/pkg/editor`: detect parent Git repositories in `game new` via `git rev-parse --show-toplevel` (printing the `go tool game merge -setup` command when outside Git), add `game merge -check [dir]` to fail when `.gitattributes` names `nsgo-scene` without a configured driver, and show a one-click setup banner when the scene editor opens a Git project missing the driver (#1532).
