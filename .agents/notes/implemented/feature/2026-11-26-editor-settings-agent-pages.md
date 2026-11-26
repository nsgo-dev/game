# Agent Note: Settings pages for the agent's models, tools, usage and privacy

Status: implemented

## Problem

The unified Settings screen (`SettingsView`, introduced in [2026-11-26-editor-settings-project-pages.md](2026-11-26-editor-settings-project-pages.md)) hosted only the `PROJECT · game.yaml` and `EDITOR · THIS COMPUTER` groups. Authors had no dedicated per-computer, You-only Settings pages matching `docs/design/editor/Settings.dc.html` to manage connected agents and keychain-backed API keys, toggle individual MCP server tools on or off (`game_delete_file` off by default), register additional MCP servers (starting with the running game via `nsgo dev`), track weekly usage and review retention, or restrict what kinds of project content (`Scene and data files`, `Viewport images`, `Go source code`, `Art and audio files`) may leave the computer across every connected agent.

## Decision

Extend `EditorSettings` (`editor.yaml`, version 1) with an optional `agent` section and add the **`AGENT`** group (**Models and connections**, **Tools**, and **Usage and privacy**) to `SettingsView`, backed by live enforcement in `packages/game/editor/editormcp`:

- **Per-computer YAML storage without secrets (`packages/game/editor/agent_settings.go`, `settings.go`)**: `EditorSettings.Agent` stores non-secret preferences (`builtInProvider`, `routes`, `tools`, `mcpServers`, `weeklyRequestLimit`, `privacy`, and local `usage` counters) with strict `KnownFields(true)` decoding, deterministic sorting of `mcpServers`, and lossless round-tripping. Built-in API keys are stored exclusively in the host OS keychain under `AgentKeychainServiceName` (`"dev.nsgo.game.agent"`) via `StoreAgentAPIKey` / `HasAgentAPIKey` / `DeleteAgentAPIKey` / `AgentAPIKey` (and cleared from the Connect dialog draft immediately on close or completion), so no API key is ever written to `editor.yaml` or any project file. Every mutator and `AgentAPIKey` checks `Editor.AgentActorActive()` and returns `ErrAgentSettingsYouOnly` if called by an agent, and `Editor.settingsMu` protects concurrent reads/writes from MCP server goroutines.
- **Live MCP tool switching and refusal (`packages/game/editor/editormcp/privacy.go`, `server.go`, `you_only.go`, `proxy.go`)**: `editormcp.Tool` adds `DefaultOff` and `Destructive`, marking `game_delete_file` (from #1015's proposed-deletion flow in `you_only.go`) with `DefaultOff: true` and unifying `Editor.ToolEnabled` with `Editor.IsMCPToolEnabled`. `Server` keeps all registered tools in `s.tools`, publishes them to `ed.SetRegisteredMCPTools` for **Settings › Tools**, and synchronizes `s.sdk` (`AddTool` / `RemoveTools`) whenever editor settings change, emitting `notifications/tools/list_changed` to connected clients. A receiving middleware on `Server` (and forwarding middleware on `Proxy`, plus a gate in `Server.callWithSession` for `RegisterAutomation`) intercepts `tools/call` for any disabled registered tool and returns a readable `CallToolResult{IsError: true}` naming the tool and **Settings › Tools** rather than a generic unknown-tool error.
- **Outbound privacy boundary for every agent (`packages/game/editor/editormcp/privacy.go`, `tools.go`, `stream.go`)**: `Editor.AgentPrivacy()` resolves the four **WHAT MAY LEAVE THIS COMPUTER** switches (defaults: `SceneAndData=true`, `ViewportImages=true`, `GoSource=false`, `ArtAndAudio=false`). `Server` enforces the policy for every connected session both pre-call (across `viewport_image`, `play_frozen_context`, `play_replay_step`, `agent_turn_get`, `scene_read`, `refs_find`, `refactor_rename`, `game_delete_file`, `project_context`, `history_list`, `game_playtest`, and all scene/data tools, resolving in-project symlinks so disguised symlinks to `.go` or `art/`/`audio/` files cannot bypass privacy) and post-call (refusing any `Result.PNG` when viewport images are off, and gating art/audio text files in `scene_read` on both requested and symlink-resolved paths right after binary-file checks so binary files still report `"not a text file"` while zero bytes leave the computer), as well as on all six resources (`editor://scene`, `editor://selection`, `editor://agent/turn`, `editor://brief`, `editor://rules`, `editor://context`).
- **External MCP server config sync (`packages/game/editor/editormcp/install.go`, `packages/game/cmd/game/mcp.go`)**: `DefaultAgentMCPServers()` seeds **Running game · nsgo dev** (`nsgo-game`: `go tool nsgo mcp`). `InstallWithServers` writes `nsgo-game-editor` plus all enabled configured servers into both `.mcp.json` (Claude Code) and `.codex/config.toml` (Codex) while removing disabled or deleted managed servers and preserving unrelated user entries. `editormcp.New` wires `Editor.SyncMCPConfig` automatically so toggling or adding a server in **Settings › Tools** updates project MCP configs immediately.
- **Spec-token accessible UI (`packages/game/editor/settings_agent_ui.go`, `settings_ui.go`)**: All three pages render strictly with `SpecPalette` tokens (Dark and Light), real `ui.Button` / `ui.Switch` (`core.RoleSwitch`) / `ui.Select` / `ui.Input` controls with `p.FocusRing(2)`, `You only` policy badges, and full English and Simplified Chinese catalogs in `locales/{en,zh}.yaml`.

## Alternatives considered

- **Storing agent settings in a separate `agent-settings.yaml` file** — Would keep `editor.yaml` smaller. Rejected because `docs/design/editor/Settings.dc.html` and issue #1028 specify storing all per-person editor and agent settings in the single per-person editor settings file (excluding secrets in the keychain), and keeping one schema (`editor-settings.v1.json`) avoids fragmenting user config paths.
- **Leaving disabled tools in `s.sdk`'s tool list and refusing only inside handlers** — Simpler because `RemoveTools` and middleware would not be needed. Rejected because issue #1028 explicitly requires turning a tool off to remove it from the server's tool list for connected clients AND return a readable refusal when called directly.
- **Checking art/audio privacy before binary-file detection in `scene_read`** — Slightly simpler pre-call gate. Rejected because existing `TestSceneRead` expects binary `art/actors.png` to report `"not a text file"`; checking art/audio privacy immediately after `readProjectFile` confirms a readable text file preserves `"not a text file"` for binaries while guaranteeing zero bytes of any art/audio metadata file (such as `.atlas.yaml` or `.import.yaml`) ever leave the computer when switched off.

## Consequences

- **Benefits**:
  - Authors can inspect and constrain connected models, keychain API keys, every MCP tool, external MCP servers, weekly usage, and outbound privacy boundaries from one Spec-styled `SettingsView` in Dark and Light appearances.
  - Tool disabling and privacy refusals are enforced uniformly at the MCP server layer across every connected agent and automation bridge.
  - No API key is ever serialized to `editor.yaml` or any project file.
- **Costs and known limits**:
  - `EditorSettings` v1 adds an optional `agent` block (`editor-settings.v1.json` updated in place; older files without `agent` continue to parse identically).
  - Usage counters track requests, review outcomes, and playtest runs locally in `editor.yaml` without storing prompts or per-model billing APIs.

## Verification

- `(cd packages/game && env -u GOROOT go run ./cmd/gameschema -check)`
- `(cd packages/game && env -u GOROOT go tool nsgo check ./editor && env -u GOROOT go tool nsgo check -target ios ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check ./editor/editormcp && env -u GOROOT go tool nsgo check -target ios ./editor/editormcp)`
- `(cd packages/game && env -u GOROOT go tool nsgo check ./cmd/game)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor/editormcp)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./cmd/game -run TestMCP)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test -target macos -run '^TestSettingsAgentPagesMacOSScreenshots$' ./editor/editorcmd)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
