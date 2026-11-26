# Agent Note: nsgo game as the documented form of the game command

Status: implemented

## Problem

The game layer's command was documented only as `go tool game`, a second entry point beside `go tool nsgo` for one workflow, and creating a project needed `go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new`. nsgo-dev/nsgo#2366 makes `nsgo game` a built-in command of NSGo's `nsgo`: in a module that declares `github.com/nsgo-dev/game/packages/game/cmd/game` as a tool it runs `go tool` with it, at the module's version, and elsewhere `go run` of the game layer's release made for that `nsgo` (`NSGO_GAME_VERSION` replaces it).

## Decision

The docs (`docs/en`, `docs/zh`), the agent skill (`skills/nsgo-game`) and the READMEs that show the command write it as `nsgo game <command>`. `docs/en/cli.md#install` says it runs through `nsgo` since v0.3.3, that a project's own `nsgo` tool needs v0.3.3 or later as well since an `nsgo` of another version hands a project's commands to the project's, and that `go tool game` runs the same command in a project. `packages/game`, every template's `go.mod.tmpl` (and its copy under `internal/gamecli/_templates`), the examples and the agent-eval fixtures require NSGo v0.3.4 for that reason (#1126), rather than v0.3.3, which skips depth-tested draws into a window's own depth buffer (nsgo-dev/nsgo#2395), so a project made from any template runs `nsgo game` with no extra step. The generated command reference (`internal/gamecli/reference.go`) prints each usage line as `nsgo game …`; the command's own help and errors keep `game …`, true under either name.

What runs the command itself stays `go tool game`: the editor's Run button (`editor/run_game.go`), the workflows and the benchmark baseline's note, as they must work without an `nsgo` on `PATH`. The built-in `nsgo game` dispatch and "no extra command" part of this decision were later superseded by [decoupling `nsgo game` via `cmd/nsgo-game` and `internal/gamecli`](../../implemented/architecture/2026-11-27-nsgo-game-extension-decoupling.md), while `nsgo game` remains the documented form.

## Alternatives considered

- **A `cmd/nsgo-game` on `PATH`, found by #2366's extension lookup** — worked with any `nsgo` that has extensions, but needed a second install step, and the command's code would have moved into an internal package so both mains could share it; NSGo made `game` a built-in command instead, so nothing is installed.
- **Keeping `go tool game` as the documented form** — works with every released `nsgo`, but keeps two entry points for one workflow.

## Consequences

- **Benefits**: one entry point, `nsgo game`, for creating and working on a game, with the module's pinned version inside a project.
- **Costs and known limits**: a project that pins an NSGo before v0.3.3 has no `nsgo game`, because a newer `nsgo` hands its commands to the pinned one; `go get -tool github.com/nsgo-dev/nsgo/cmd/nsgo@v0.3.4` fixes it. Outside a project `nsgo game` runs `go run` of the game command at nsgo's `gameVersion`, which v0.3.4 still leaves at `latest` until nsgo-dev/nsgo pins it to the first game release made for an NSGo with `nsgo game`. Shell completion completes `game` and `nsgo game`, not `go tool game` ([completion for nsgo game](../feature/2026-11-25-completion-nsgo-game.md)).
