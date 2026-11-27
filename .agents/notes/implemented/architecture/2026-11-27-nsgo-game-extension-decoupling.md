# Agent Note: Decoupling `nsgo game` from NSGo via `cmd/nsgo-game` and `internal/gamecli`

Status: implemented

## Problem

NSGo previously shipped a hardcoded built-in `nsgo game` command (`cmd/nsgo/gamecommand.go`) that knew `github.com/nsgo-dev/game/packages/game/cmd/game`, pinned a game release version, and read `NSGO_GAME_VERSION` when invoked outside a Go module. That coupled the core `nsgo` CLI to `nsgo-dev/game`'s repository layout and release schedule.

Removing that built-in leaves `nsgo game` to NSGo's generic extension mechanism:

1. Inside a module whose `go.mod` declares `tool github.com/nsgo-dev/game/packages/game/cmd/game`, `nsgo game <args>` automatically runs `go tool github.com/nsgo-dev/game/packages/game/cmd/game <args>`.
2. Outside a module (such as before scaffolding a project with `nsgo game new`), `nsgo game <args>` looks up an executable named `nsgo-game` on `PATH`.

`packages/game/cmd/game` installs a binary named `game`, not `nsgo-game`, so outside-a-module `nsgo game` could not find an installed extension binary without a `cmd/nsgo-game` package.

## Decision

Extract the entire CLI implementation (including `_templates/` and `testdata/`) from `packages/game/cmd/game` into `packages/game/internal/gamecli`, exposing `gamecli.Main()` and `gamecli.Run(args []string) int`, and wire two tiny `main` packages to it:

- `packages/game/cmd/game`: thin wrapper calling `gamecli.Main()`, preserving the module `tool` path (`github.com/nsgo-dev/game/packages/game/cmd/game`) for `go tool game`, in-module `nsgo game`, and zero-install `go run github.com/nsgo-dev/game/packages/game/cmd/game@latest`.
- `packages/game/cmd/nsgo-game`: thin wrapper calling `gamecli.Main()`, providing `go install github.com/nsgo-dev/game/packages/game/cmd/nsgo-game@latest` so `nsgo game` works outside a Go module via `PATH` discovery.

`packages/game/internal/gamecli` sits at the exact same directory depth below the repository root (4 levels) as `packages/game/cmd/game`, so all relative repository paths in tests (`reference_test.go`, `templates_test.go`, `library_test.go`, `skill_e2e_test.go`, `navmesh_test.go`, `playtest_test.go`, `replay_test.go`) remain unchanged. `packages/game/conformance_test.go`'s `notFormats` allowlist moves from `cmd/game` to `internal/gamecli`, and `docs/{en,zh}/stability.md` plus `skills/nsgo-game/references/packages.md` register `game/cmd/nsgo-game` as `Experimental`.

This partly supersedes the built-in dispatch mechanism recorded in [nsgo game as the documented form](../../implemented/process/2026-11-25-nsgo-game-main-form.md): `nsgo game` stays the long-term extension form once an NSGo release ships without the hardcoded built-in, while [Document `go run .../cmd/game@latest` and `go tool game` while templates pin NSGo v0.3.4](../../implemented/process/2026-11-28-cli-docs-go-tool-game-until-nsgo-release.md) switches user-facing docs to `go run .../cmd/game@latest` and `go tool game` until templates move past NSGo v0.3.4.

## Alternatives considered

- **Keep the hardcoded `nsgo game` built-in in `nsgo-dev/nsgo`** — Avoids a one-time `go install .../cmd/nsgo-game@latest` for outside-a-module `nsgo game`, which was why the earlier note ([nsgo game as the documented form](../../implemented/process/2026-11-25-nsgo-game-main-form.md)) accepted it. Rejected because it permanently couples `nsgo` to `nsgo-dev/game`'s module path, version pinning, and `NSGO_GAME_VERSION` override.
- **Rename `packages/game/cmd/game` to `packages/game/cmd/nsgo-game`** — Avoids adding a second `cmd/` package and an `internal/gamecli` package. Rejected because it breaks every existing project's `tool github.com/nsgo-dev/game/packages/game/cmd/game`, `go tool game` (used by the editor, workflows, MCP configs, and Git merge drivers), and `go run .../cmd/game@latest`.
- **Tell users outside a module to only use `go run .../cmd/game@latest` with no `cmd/nsgo-game`** — Avoids maintaining a second binary entrypoint. Rejected because users who already run `nsgo game` inside projects expect `nsgo game new` to work outside a project too once installed, matching standard `git-<subcommand>` / `nsgo-<subcommand>` PATH extensions.

## Consequences

- **Benefits**: `nsgo` has zero compile-time or runtime coupling to `nsgo-dev/game`; `cmd/game` and `cmd/nsgo-game` share 100% of CLI code via `internal/gamecli`; existing `go tool game`, `go.mod` `tool` directives, and `go run .../cmd/game@latest` remain completely compatible.
- **Costs**: Running `nsgo game` outside a Go module requires a one-time `go install github.com/nsgo-dev/game/packages/game/cmd/nsgo-game@latest` (or using `go run github.com/nsgo-dev/game/packages/game/cmd/game@latest` directly); contributors updating embedded project templates now copy into `packages/game/internal/gamecli/_templates/projects/<name>` instead of `packages/game/cmd/game/_templates/projects/<name>`.
