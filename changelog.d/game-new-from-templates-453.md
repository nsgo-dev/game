### Added

- `game new <dir>` creates a game project from the starter template, the one `template/template` holds, with `nsgo.yaml`'s `name` and `id` and the module path set from the directory or `-name` and `-module`, then runs `go get -tool` and `go mod tidy` so its `go.mod` requires the command's own version of the game layer (or `-version`) and has the `game` tool beside `nsgo`; `-template` chooses among templates, `starter` for now (#453).
