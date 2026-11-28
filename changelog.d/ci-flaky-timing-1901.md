### Fixed

- Unit tests no longer fail on wall-clock performance thresholds, and `game bench` retries a ns/op-only regression once when allocation metrics are unchanged, failing only when it reproduces (#1901).
