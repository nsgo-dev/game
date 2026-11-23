### Added

- `game/leaderboard/selfhost`: a self-hosted cross-platform leaderboard service with boards declared in versioned YAML (sort order, all-time, daily or weekly resets in UTC that keep earlier periods readable, score bounds), score submission authenticated by the account service's tokens, optional game-server-signed score proofs, top-N, around-me and friends queries over an in-memory order-statistic index (microseconds on a million entries), a JSON Lines file store, an HTTP handler and the `leaderboardd` command (#558).
