# Agent Note: nsgo game as the documented form of the game command

Status: implemented

## Problem

The game layer's command was documented only as `go tool game`, a second entry point beside `go tool nsgo` for one workflow, and creating a project needed `go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new`. nsgo-dev/nsgo#2366 makes `nsgo game` a built-in command of NSGo's `nsgo`: in a module that declares `github.com/nsgo-dev/game/packages/game/cmd/game` as a tool it runs `go tool` with it, at the module's version, and elsewhere `go run` of the game layer's release made for that `nsgo` (`NSGO_GAME_VERSION` replaces it).

## Decision

The docs (`docs/en`, `docs/zh`), the agent skill (`skills/nsgo-game`) and the READMEs that show the command write it as `nsgo game <command>`. `docs/en/cli.md#install` says it needs `nsgo` from main until the release after 0.3.0, that a project's own `nsgo` tool needs #2366 as well since an `nsgo` of another version hands a project's commands to the project's, and that `go tool game` runs the same command in a project. The generated command reference (`cmd/game/reference.go`) prints each usage line as `nsgo game …`; the command's own help and errors keep `game …`, true under either name.

What runs the command itself stays `go tool game`: the editor's Run button (`editor/run_game.go`), the workflows and the benchmark baseline's note, as they must work without an `nsgo` on `PATH`. The game module adds no command for `nsgo`: the project templates' `go.mod` already declare the `game` tool that `nsgo game` runs.

## Alternatives considered

- **A `cmd/nsgo-game` on `PATH`, found by #2366's extension lookup** — worked with any `nsgo` that has extensions, but needed a second install step, and the command's code would have moved into an internal package so both mains could share it; NSGo made `game` a built-in command instead, so nothing is installed.
- **Keeping `go tool game` as the documented form** — works with every released `nsgo`, but keeps two entry points for one workflow.

## Consequences

- **Benefits**: one entry point, `nsgo game`, for creating and working on a game, with the module's pinned version inside a project.
- **Costs and known limits**: until NSGo releases #2366, the docs' main form needs `nsgo` from main, in the project's `tool` line too. Shell completion completes a command named `game`, not `nsgo game`.
