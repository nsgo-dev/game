### Changed

- `gameschema` and `gameapi`, the commands that write the published JSON Schemas and the API reference, move from `cmd/` to the tools module: run them as `go run ./tools/gameschema` and `go run ./tools/gameapi` from the repository root. They are contributor tooling and leave the stability table.
