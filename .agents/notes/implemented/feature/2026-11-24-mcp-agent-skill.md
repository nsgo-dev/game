# Agent Note: Agent Skill for the NSGo game layer

Status: implemented

## Problem

NSGo ships an installable Agent Skill (`skills/nsgo`) so coding agents can build, test and drive NSGo apps via `go tool nsgo` and `nsgo mcp`, while the game layer (`github.com/nsgo-dev/game/packages/game`) adds 100+ packages, the `go tool game` CLI, strict versioned YAML formats, and the `game/mcp` tool suite. Without a dedicated, installable skill describing those conventions and MCP tools, agents guess package names, bypass `go tool nsgo check`/`test`, write non-canonical YAML, or miss the deterministic `game_loop_*` and inspection workflows.

## Decision

Publish an Agent Skill at `skills/nsgo-game/` (installable via `bunx skills add nsgo-dev/game`) with a concise `SKILL.md` entry point and three on-demand reference files (`references/packages.md`, `references/cli-and-formats.md`, `references/mcp-tools.md`), backed by automated Go tests that keep the skill mechanically aligned with the repository and verify the documented CLI-to-MCP workflow end to end.

## Skill layout and test invariants

- **Standard Agent Skills layout**: `skills/nsgo-game/SKILL.md` carries strict YAML frontmatter (`name: nsgo-game` and `description`) matching the directory name, mirroring NSGo's `skills/nsgo/` layout so `bunx skills add nsgo-dev/game` discovers and installs it cleanly.
- **Progressive disclosure via `references/`**: `SKILL.md` stays short and task-oriented (core rules, `game new`/`generate`/`assets`/`check`/`dev` workflow, minimal `game.Loop` + `mcp.Attach` pattern, and the deterministic MCP verification loop), delegating dense catalogs to `references/packages.md`, `references/cli-and-formats.md`, and `references/mcp-tools.md`.
- **Drift prevention tests (`packages/game/skill_test.go`)**: `TestAgentSkillValid` parses `SKILL.md` frontmatter with `KnownFields(true)`, verifies every relative Markdown link stays inside `skills/nsgo-game/` and resolves on disk, checks `references/packages.md` against `modulePackages(t)` (both directions) and each package's status against `docs/en/stability.md`, and requires the tool tables in `references/mcp-tools.md` to name exactly the tools `mcp.New(mcp.Config{}).Tools()` returns. `TestAgentSkillDocumentsEveryCommand` (`packages/game/cmd/game`) loops over the `commands` slice and requires each subcommand in `references/cli-and-formats.md`.
- **End-to-end CLI and MCP verification**: `TestAgentSkillWorkflowBuildAndVerifyThroughMCP` (`packages/game/skill_test.go`) and `TestAgentSkillCLIScaffoldGenerateCheckAndMCP` (`packages/game/cmd/game/skill_e2e_test.go`) scaffold/validate a project (`game new`, `game generate`, `game index`, `game check`), load the YAML scene into `entities.World`, attach `mcp.Bridge` inside `nsgotest.Start`, and verify deterministic simulation over MCP (`game_scenes_list`, `game_layers_list`, `game_loop_pause`, `game_loop_seed`, `game_loop_step`, `game_entities_list`, `game_entity_inspect`, `game_component_get`).
- **Additive rebase boundary**: Tests live in `packages/game/skill_test.go` and `packages/game/cmd/game/skill_e2e_test.go`, and user docs live in new `docs/{en,zh}/agent-skill.md` pages rather than heavily editing `packages/game/mcp/` or `docs/{en,zh}/mcp.md`, avoiding merge conflicts with sibling MCP tool PRs (#463–#469). `references/mcp-tools.md` documents the complete `#461`–`#469` tool catalog while Go tests compile strictly against the tools landed on `main` (`#461` and `#462`, see [MCP scene, layer, entity and component inspection](2026-11-24-mcp-scene-entity-inspection.md) and [MCP loop control and deterministic seeding](2026-11-24-mcp-loop-control.md)).

## Alternatives considered

### Putting everything into one monolithic `skills/nsgo-game/SKILL.md`

Strongest argument: a single file requires no secondary reads when an agent loads the skill.

Rejected because listing 111 packages, 19 CLI commands, dozens of YAML formats, and 38 MCP tools in one file bloats every agent prompt where the skill triggers. Splitting into a compact `SKILL.md` plus three focused `references/` files matches NSGo's `skills/nsgo/` structure and Agent Skills progressive-disclosure guidelines.

### Placing the skill under `.agents/skills/nsgo-game/` instead of `skills/nsgo-game/`

Strongest argument: `.agents/skills/` is where repository-local contributor skills live (such as `write-notes-like-deepseek`).

Rejected because `.agents/skills/` is for internal repository workflow skills, whereas `skills/nsgo-game/` matches NSGo's published `skills/nsgo/` location for downstream users installing via `bunx skills add nsgo-dev/game`.

### Relying on manual review to keep the skill synchronized with code

Strongest argument: markdown files do not normally require Go tests.

Rejected because new packages, CLI commands, and MCP tools land frequently; without `TestAgentSkillValid`, the skill's package list, relative links, and MCP tool names would silently drift from the codebase.

## Consequences

- **Benefits**: Downstream users can install the game-layer skill with `bunx skills add nsgo-dev/game`; agents have an accurate, concise map of packages, CLI commands, YAML rules, and MCP tools; CI automatically catches missing packages, broken links, or undocumented MCP tools.
- **Costs**: Adding a new package under `packages/game/` now requires updating `skills/nsgo-game/references/packages.md` alongside `docs/{en,zh}/stability.md`, and adding a new tool to `mcp.Bridge.Tools()` requires ensuring its name appears in `skills/nsgo-game/references/mcp-tools.md` (already pre-populated for `#461`–`#469`).

## Verification

Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./cmd/game`, `go tool nsgo test ./mcp`, `go tool nsgo test . -- -run '^Test(AgentSkill|ASecond|FormatsPage|FormatPackagesRunConformance|StabilityTable)'`, `go run ./tools/changelog check`, and `bun run verify-notes`.
