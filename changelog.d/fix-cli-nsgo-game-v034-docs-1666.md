### Fixed

- `cmd/game` & docs: lead README, bilingual docs, generated CLI reference, and the `nsgo-game` skill with `go run github.com/nsgo-dev/game/cmd/game@latest new` and `go tool game` while templates pin NSGo v0.3.4 (whose built-in `nsgo game` command targets the legacy `packages/game` module), add a `repocheck` doc guard, and print a one-line stderr warning if the CLI runs from a module other than `github.com/nsgo-dev/game` (#1666).
