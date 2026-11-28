# Agent Skill

The repository publishes an Agent Skill at [`skills/nsgo-game`](../../skills/nsgo-game/SKILL.md) so coding agents know how to scaffold, build, test, inspect and drive NSGo games using the `go tool game` CLI, plain-text YAML formats and the `game/mcp` tool suite.

## Install

Install the skill with `bunx skills add`:

```sh
bunx skills add nsgo-dev/game
```

Pair it with NSGo's own `nsgo` skill for core UI primitives, signals, `nsgotest` and `go tool nsgo`:

```sh
bunx skills add nsgo-dev/nsgo
```

To let an agent inspect and drive a running game live, also register NSGo's MCP server:

```sh
claude mcp add nsgo -- go tool nsgo mcp
```

## What the skill covers

- **[`SKILL.md`](../../skills/nsgo-game/SKILL.md)**: project rules (Go only, plain-text versioned YAML formats, `go tool nsgo check`/`test`, stability tiers), quick-start CLI workflow, minimal `game.Loop` + `mcp.Attach` wiring, and the deterministic MCP verification loop.
- **[`references/packages.md`](../../skills/nsgo-game/references/packages.md)**: complete grouped catalog of every package in `github.com/nsgo-dev/game`, with import paths and stability status matching [API Stability](stability.md).
- **[`references/cli-and-formats.md`](../../skills/nsgo-game/references/cli-and-formats.md)**: all `go tool game` subcommands ([Command Reference](cli-reference.md)), companion module tools, the YAML format contract ([File Formats](formats.md)), JSON Schemas ([Schemas](schemas.md)) and native external formats (Tiled, LDtk, Aseprite, TexturePacker, BMFont, glTF, Yarn).
- **[`references/mcp-tools.md`](../../skills/nsgo-game/references/mcp-tools.md)**: `mcp.Bridge` setup ([mcp](mcp.md)) and the full tool catalog covering scene/entity inspection, fixed-step loop control, game-coordinate input, live spawning and component editing, screenshots with diagnostic overlays, debug console tunables, input recording/replay, performance telemetry and live asset hot reload.

## Typical agent workflow

1. Scaffold with `go run github.com/nsgo-dev/game/cmd/game@latest new` and add building blocks with `go tool game generate`, `go tool game assets` and `go tool game check`.
2. Attach `mcp.Bridge` on the game's `*core.Scope` and run `go tool nsgo dev -automation` (or `nsgotest.Start` in tests).
3. Drive reproducible checks through MCP: pause with `game_loop_pause`, seed with `game_loop_seed`, inject inputs or spawn entities, advance synchronously with `game_loop_step`, inspect entities with `game_entities_list` / `game_entity_inspect` / `game_component_get`, and capture overlays with `game_screenshot`.

For store media, keep `store/capture.yaml` ([Store screenshot plan](shipping.md#store-screenshot-plan)) up to date with the scene, recording and frame of each screenshot, capture each image at the size the plan names with `game_screenshot`, and run `go tool game capture` until it reports no missing or wrongly sized file. Draft listing text only from facts in `game.yaml`, the input bindings and the `changelog.d` fragments.

The editor's MCP server and the `go tool game mcp` proxy also serve the skill built into the project's pinned `go tool game` as `skill://nsgo-game/SKILL.md` and `skill://nsgo-game/references/<file>` resources, and clients that cannot read resources get one section at a time from the `skill_get` tool ([Versioned agent skill resources](mcp.md#versioned-agent-skill-resources)). That copy describes the tools of the version that answers the calls, so it wins over an older copy installed with `bunx skills add`.
