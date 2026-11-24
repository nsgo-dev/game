### Added

- `internal/formattest`: a shared conformance check that every package reading a YAML format runs on canonical testdata files, requiring a byte-identical load and save round trip, output that does not depend on map order, the time or randomness, and an error naming the key for an unknown top-level key, a missing `version`, `version: 0` and a version newer than the format reads; `go tool nsgo test` fails a package that decodes YAML without running it, and `docs/en/schemas.md` describes how to add a format to it (#479).
