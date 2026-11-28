### Added

- `leaderboard/selfhost/sqlitestore` and `leaderboard/selfhost/pgstore` persist self-hosted scores in SQLite or Postgres with transactional best-score writes. `leaderboardd serve` now accepts `-sqlite` and `-postgres` storage flags (#1680).
