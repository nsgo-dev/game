### Added

- `game`: every package that reads a YAML format runs a shared conformance test on its testdata, which checks that the format refuses unknown fields, a missing version and versions it does not support, writes the same bytes each time it saves a value and loads and saves each file without changing a byte; CI fails a package that reads strict YAML without running it ([Checking formats in CI](docs/en/schemas.md#checking-formats-in-ci)) (#479).
