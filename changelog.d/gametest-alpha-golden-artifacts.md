### Fixed

- `pkg/gametest`: `CompareGolden` now round-trips the actual image through PNG before diffing so premultiplied frames with partial alpha match freshly written goldens, and `AssertGoldenAt` now writes `<name>.got.png` and `<name>.diff.png` beside the golden (or under `GOLDEN_DIFF_DIR`) so failure images outlive the test (#1603).
