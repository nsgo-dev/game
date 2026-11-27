### Changed

- `wasmprobe`, the program CI builds to measure release package sizes, moves from `cmd/wasmprobe` to `internal/sizeprobe` and is no longer a public command: build it as `./internal/sizeprobe` from the repository root. It leaves the stability table.
