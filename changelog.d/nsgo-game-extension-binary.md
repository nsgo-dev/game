### Added

- `game/cmd/nsgo-game`: add the `nsgo-game` extension binary entrypoint (`go install github.com/nsgo-dev/game/cmd/nsgo-game@latest`) so `nsgo game` works outside a Go module through NSGo's generic extension mechanism.

### Changed

- `game/cmd/game`: move the shared CLI implementation into `game/internal/gamecli` so `cmd/game` and `cmd/nsgo-game` share one entrypoint, and update CLI, template, getting-started, stability and agent skill docs for NSGo's extension-based `nsgo game` dispatch without a built-in command in `nsgo`.
