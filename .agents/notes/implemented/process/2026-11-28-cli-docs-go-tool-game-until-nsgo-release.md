# Agent Note: Document `go run .../cmd/game@latest` and `go tool game` while templates pin NSGo v0.3.4

Status: implemented

## Problem

NSGo `v0.3.4`—the latest published NSGo release and the version pinned in every template's `go.mod.tmpl`—still ships a built-in `nsgo game` command (`cmd/nsgo/gamecommand.go`) that shadows NSGo's generic extension lookup and hard-codes the pre-migration module path `github.com/nsgo-dev/game/packages/game/cmd/game`. Projects created by the root module declare `tool github.com/nsgo-dev/game/cmd/game`, so `nsgo game ...` never matches the project's pinned tool and instead runs `go run github.com/nsgo-dev/game/packages/game/cmd/game@latest`, creating or checking projects with the wrong module.

NSGo removed that built-in on its `main` branch (`nsgo-dev/nsgo` `239a1b80f`), and [decoupling `nsgo game` via `cmd/nsgo-game` and `internal/gamecli`](../architecture/2026-11-27-nsgo-game-extension-decoupling.md) prepared the generic extension layout, but no NSGo release contains the removal yet. Meanwhile `docs/en`, `docs/zh`, the generated `cli-reference.md`, and `skills/nsgo-game` led with `nsgo game`, contradicting `README.md` and `apps/website/src/pages/Home.tsx` (`go run github.com/nsgo-dev/game/cmd/game@latest new mygame`).

## Decision

While project templates pin `github.com/nsgo-dev/nsgo` at `v0.3.4` or older, every user-facing entry point (`README.md`, `docs/en`, `docs/zh`, `docs/{en,zh}/cli-reference.md` via `internal/gamecli/reference.go`, `skills/nsgo-game`, and `apps/website/src/pages/Home.tsx`) leads with:

- `go run github.com/nsgo-dev/game/cmd/game@latest new <dir>` outside a project, and
- `go tool game <command>` inside a project.

Only `docs/en/cli.md` (`## Install`, `### completion`) and `docs/zh/cli.md` (`## 安装`, `### completion`) retain a short prose note explaining that `nsgo game` via NSGo's generic extension mechanism requires an NSGo release newer than `v0.3.4`.

Two mechanical guards enforce this contract:

1. **`internal/repocheck/cli_docs_test.go` (`TestCLIDocsAvoidLegacyNSGoGame`)**: checks all `template/**/go.mod.tmpl` pins. While any template pins NSGo `<= v0.3.4`, it fails if `"nsgo game"` appears inside any fenced code block anywhere in `README.md`, `docs/{en,zh}/`, `skills/nsgo-game/`, or `apps/website/src/`, or in prose outside the allowlisted compatibility sections of `docs/{en,zh}/cli.md`, and verifies that every newcomer entry point contains `go run github.com/nsgo-dev/game/cmd/game@latest new`. Once a newer NSGo release ships and templates bump past `v0.3.4`, the guard yields so the same PR can switch the docs back.
2. **`internal/gamecli/main.go` (`cliModulePath` / `warnUnexpectedModule`)**: inspects `runtime/debug.ReadBuildInfo()` at CLI startup, resolving the owning module of `info.Path` by longest module-path prefix across `info.Main` and `info.Deps`, and prints a single-line warning to `stderr` whenever the binary was built from a module other than `github.com/nsgo-dev/game`.

This temporarily supersedes the documented-form rule in [nsgo game as the documented form](2026-11-25-nsgo-game-main-form.md) and complements [decoupling `nsgo game` via `cmd/nsgo-game` and `internal/gamecli`](../architecture/2026-11-27-nsgo-game-extension-decoupling.md) until an NSGo release without the hardcoded built-in ships.

## Alternatives considered

- **Wait for an NSGo release and keep `nsgo game` in the docs** — Avoids churning docs twice. Rejected because every newcomer and coding agent following `getting-started.md`, `cli.md`, or `SKILL.md` today silently downloads and executes the pre-migration `packages/game` module under NSGo `v0.3.4`.
- **Check only `info.Main.Path != "github.com/nsgo-dev/game"` in the CLI warning** — Simpler than matching `info.Path` against `info.Main` and `info.Deps`. Rejected because `go tool game` inside a user's game project sets `info.Main.Path` to the user's module (`example.com/mygame`) while `github.com/nsgo-dev/game` lives in `info.Deps`, which would falsely warn every `go tool game` user.

## Consequences

- **Benefits**: Every documented command in README, docs, website, and the Agent Skill runs the root `github.com/nsgo-dev/game` CLI cleanly on NSGo `v0.3.4`; CI prevents `nsgo game` regressions while `v0.3.4` is pinned; any user accidentally running the CLI from another module path gets an immediate one-line stderr warning without corrupting stdout.
- **Costs and known limits**: Developers type `go tool game` inside projects and `go run github.com/nsgo-dev/game/cmd/game@latest` outside until a newer NSGo release ships and templates raise their pin. The warning in `internal/gamecli` only appears in builds containing this change (already-published `packages/game v0.1.0` cannot be altered retroactively, though any future build from a non-root module path will emit it).
