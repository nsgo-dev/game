### Changed

- Move the Go module root from `packages/game` (`github.com/nsgo-dev/game/packages/game`) to the repository root (`github.com/nsgo-dev/game`), keeping `cmd/` and `internal/` at the root and organizing all library subpackages under `pkg/` (`github.com/nsgo-dev/game/pkg/<name>`) so releases use clean root tags (`v0.1.0`) while keeping the repository root tidy.
