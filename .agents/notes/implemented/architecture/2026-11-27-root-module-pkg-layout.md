# Agent Note: Root Go module with pkg/ subpackages

Status: implemented

## Problem

Keeping the Go module in `packages/game` (`module github.com/nsgo-dev/game/packages/game`) forced Go submodule tags (`packages/game/v0.1.0`) on every release and long, repetitive import paths (`github.com/nsgo-dev/game/packages/game/sprite`). At the same time, flattening all ~83 library subpackages directly into the repository root would crowd the top-level directory alongside `apps/`, `docs/`, `examples/`, `template/`, and `tools/`.

## Decision

The Go module root lives at the repository root (`go.mod` declaring `module github.com/nsgo-dev/game`), keeping only the entry `package game` (`loop.go`, `doc.go`, root contract tests), `cmd/`, and `internal/` at the root while placing all library subpackages under `pkg/` (`github.com/nsgo-dev/game/pkg/<name>`).

### Layout invariants

- `go.mod` and `go.sum` stay at the repository root so release tags are standard root tags (`vX.Y.Z`) and `go tool nsgo test ./...` / `go tool nsgo check` run directly from the repository root.
- `internal/` stays at the repository root (not under `pkg/internal/`) so the root package, `cmd/*`, and `pkg/*` can all import internal helpers such as `internal/formattest` and `internal/gamecli`.
- All importable library subpackages live under `pkg/<name>` (`github.com/nsgo-dev/game/pkg/<name>`). Nested modules (`examples/*`, `template/*`, `tools/*`) retain their own `go.mod` files, keeping Go module boundaries isolated from the root module.
- Logical format IDs on `docs/{en,zh}/formats.md` (`<!-- format: quest -->`, etc.) omit the `pkg/` directory prefix; `internal/formattest`, `formats_test.go`, and `conformance_test.go` strip/resolve the `pkg/` prefix when matching packages to docs entries.
- `docs/{en,zh}/stability.md` and `skills/nsgo-game/references/packages.md` list packages relative to `github.com/nsgo-dev/` (`game`, `game/cmd/<name>`, `game/pkg/<name>`), and `apps/website` strips `pkg/` for display grouping in the package catalog and API sidebar.
- Website unit tests under `apps/website/` use the `*.spec.ts` suffix (rather than `*.test.ts`) so `bun test` discovers them while `go tool nsgo test ./...` at the module root does not mistake them for NSGo TSX tests.

## Alternatives considered

1. **Keep `packages/game` as a Go submodule**: Keeps the repository root minimal and avoids import rewrites, but permanently forces `packages/game/vX.Y.Z` submodule tags and redundant `packages/game` import paths.
2. **Flatten all ~83 subpackages directly into the repository root**: Gives shortest possible subpackage paths (`github.com/nsgo-dev/game/sprite`), but mixes ~83 library directories directly with `apps/`, `docs/`, `examples/`, `skills/`, `template/`, and `tools/` at the top level.
3. **Place subpackages under `packages/<name>` instead of `pkg/<name>`**: Keeps a single folder for subpackages, but `pkg/` is shorter and idiomatic in Go repositories.

## Consequences

- **Benefits**: Clean `vX.Y.Z` tags and GitHub Releases, shorter root module and CLI paths (`github.com/nsgo-dev/game`, `github.com/nsgo-dev/game/cmd/game`), tidy top-level repository layout, and simpler CI/developer commands run directly from the repository root.
- **Costs**: Existing references to `github.com/nsgo-dev/game/packages/game` (including templates, examples, website plugins, CI workflows, and `nsgo-dev/nsgo`'s extension test) must use `github.com/nsgo-dev/game` and `github.com/nsgo-dev/game/pkg/<name>`; library imports include the `/pkg/` segment.
- **Verification**: Root Go tests (`go tool nsgo test ./...`), cross-target checks (`go tool nsgo check`), `go run ./cmd/gameschema -check`, all examples and templates, website `bun run test && bun run build && bun run typecheck`, `go run ./tools/changelog check`, and `bun run verify-notes`.
