# Agent Note: Connecting external agents from the editor composer and `go tool game mcp install`

Status: implemented

## Problem

Issue #1009 connects the NSGo Game Editor to an external coding agent (Claude Code, Codex, or any MCP client on the developer's machine) and adds the Connect dialog, the Agent tab's thread/composer panel, and the header status chip on top of the [editor MCP server](./2026-11-25-editor-mcp-server.md) (#1008). Two architectural questions needed a lasting contract that downstream issues (#1010 permissions, #1011 turns/steps, #1019 threads, #1020 context chips) can rely on:

1. How a request typed in the editor's Agent composer reaches an external agent, streams incremental steps back into the panel, supports Stop, and keeps a session per thread — while terminal-started MCP clients also connect and work.
2. How the editor discovers connected MCP clients' identities, tracks disconnects with deterministic exponential backoff (`1, 2, 4, 8, 16 s`), and reopens Connect at the step that failed rather than the start.

## Decision

### Composer-to-agent execution contract (`editor.AgentRunner` + `CLIAgentRunner`)

- **Pluggable interface**: `editor.Editor.AgentRunner` is an `AgentRunner` interface (`Run(ctx context.Context, req AgentRequest, emit func(AgentEvent)) error`). Submitting from the composer creates a new `AgentTurn`, transitions the status chip to `Working`, and runs the runner on a background goroutine; every emitted `AgentEvent` is marshaled back onto the editor's UI goroutine via `Editor.Post` (`cx.Post`).
- **Non-interactive external CLI per request**: On desktop (`agent_runner_desktop.go`), the default `CLIAgentRunner` runs the connected external agent's own CLI non-interactively in the project root with streamed JSONL output, under the launch policy in [Local agent launch policy](../bug-fix/2026-11-28-editor-local-agent-launch-policy.md):
  - **Claude Code**: `claude --print --output-format=stream-json --verbose <launch policy flags, including an inline --mcp-config> [--resume <sessionID>] --append-system-prompt=<notice and Always chips> -- <prompt>` (parsed by `ParseClaudeStreamLine`).
  - **Codex**: `codex exec --json <launch policy flags: --ignore-rules, sandbox and approval pins, an untrusted projects override, inline mcp_servers> -C <root> -- <notice and prompt>`, or `codex exec resume --json <same flags> -- <sessionID> <notice and prompt>` when continuing a thread (parsed by `ParseCodexStreamLine`).
  - Confirmed against both CLIs' `--help` output at implementation time. Canceling `ctx` stops the child process cleanly for the **Stop** action while keeping completed steps visible and undoable.
  - On mobile/web (`agent_runner_other.go`), the default runner returns an explicit desktop-only error so `-target ios` builds cleanly without `os/exec` subprocess assumptions.
- **Terminal-started MCP clients still work**: Any MCP client started in a terminal connects through `go tool game mcp` (`editormcp.Proxy`) to the editor's local Unix socket, can call every tool, and `editormcp.Server` records its tool calls via `Editor.RecordExternalAgentToolCall` as external agent turns when no composer run owns the active turn.

### Project-level MCP install (`go tool game mcp install`)

- `editormcp.Install(path)` resolves the project root (`ProjectRoot`) and writes two project-level configs atomically and idempotently:
  - `.mcp.json` (Claude Code): inserts or updates `mcpServers["nsgo-game-editor"]` with `{"command":"go","args":["tool","game","mcp"]}`, preserving unrelated top-level keys and other MCP servers, and refusing malformed JSON without overwriting.
  - `.codex/config.toml` (Codex): inserts or replaces `[mcp_servers.nsgo-game-editor]` (`command = "go"`, `args = ["tool", "game", "mcp"]`), preserving unrelated settings and sections.
- Running `go tool game mcp install` twice produces byte-for-byte identical files and prints `Wrote <file>` or `Unchanged <file>` per target.

### Client identity, reconnect schedule, and Troubleshoot targeting

- `editormcp.Proxy` installs a receiving middleware on its MCP server so that for both MCP `2025-11-25` (`initialize` / `notifications/initialized`) and `2026-07-28` (`server/discover`), it captures the downstream client's `Implementation` (`ClientInfo`), forwards that identity tagged with stdio transport metadata to the open editor's Unix socket right away, and refreshes tools.
- `editormcp.Server` tracks active `*mcp.ServerSession`s and posts `NotifyAgentMCPConnected` / `NotifyAgentMCPDisconnected` onto the editor's UI goroutine (through `Options.Run` or `Editor.Post`), and wires `Editor.AgentReconnectProbe`.
- When the last MCP client drops, the editor diagnoses which Connect step failed (`ConnectStepInstall` if neither `.mcp.json` nor `.codex/config.toml` exists in the project root, `ConnectStepAPIKey` in built-in key mode, otherwise `ConnectStepWaiting`), enters `AgentStatusReconnecting`, and runs `AgentReconnectDelays = [1s, 2s, 4s, 8s, 16s]` via the injectable `Editor.AgentScheduleAfter` hook. After five failed attempts it enters `AgentStatusDisconnected`; **Troubleshoot** calls `OpenConnectDialogAt(failedStep)` so Connect reopens directly on the failing step with a diagnostic banner.

## Alternatives considered

### Drive an external agent solely through MCP sampling (`sampling/createMessage`)

MCP sampling lets a server ask a connected client to run an LLM turn without spawning a CLI. It is rejected because neither Claude Code nor Codex exposes full autonomous agentic tool loops, session resumption, streamed step telemetry, or cancellation over MCP sampling; spawning the CLI non-interactively with `--output-format=stream-json` / `codex exec --json` gives real tool calls, Stop, session-per-thread (`--resume`), and token usage.

### Embed an interactive PTY wrapping `claude` or `codex` inside the editor

Running a PTY gives access to interactive slash commands and TUI prompts. It is rejected because ANSI terminal output cannot be reliably parsed into structured steps, permission cards, or diff reviews without brittle screen scraping, and it duplicates terminal UX instead of integrating with the editor's Spec-styled Agent panel.

### Open the Agent tab by default in place of the Inspector

`Main.dc.html` shows the Agent tab active in the right sidebar. Switching the default tab away from Inspector was rejected for this change because dozens of existing editor tests and workflows expect `InspectorView` at `272 pt` immediately on open; keeping `RightTabInspector` as default (and switching to `RightTabAgent` at `380 pt` on tab press or Connect Done, while keeping the Agent panel closed for "No agent") preserves full backward compatibility.

## Consequences

- **Benefits**: Requests typed in the composer stream structured steps into the Agent panel and remain testable deterministically with mock `AgentRunner`s and injectable `AgentScheduleAfter` clocks; `go tool game mcp install` sets up both Claude Code and Codex in one idempotent command; Connect's waiting indicator turns green the instant an MCP client attaches (for both 2025-11-25 and 2026-07-28 MCP protocols); manual tools stay usable in every connection state.
- **Costs**: `CLIAgentRunner` relies on `claude` or `codex` being installed on `PATH` and follows their JSONL output event schemas (`ParseClaudeStreamLine` / `ParseCodexStreamLine`), which must be updated if upstream CLIs rename fields. The four starting permission checkboxes are stored in memory on `Editor` (`AgentPermissions` / `AgentPermissionRules`) until #1010 defines the shared permissions file format.

## Testing

- `packages/game/editor/editormcp/install_test.go`: tests idempotent `Install`, preservation of unrelated `.mcp.json` / `.codex/config.toml` entries, invalid JSON refusal, and `Proxy` -> `Server` client identity & disconnect propagation.
- `packages/game/editor/agent_test.go`: tests Connect dialog choices/copy/waiting-to-connected/permissions/Later/Done/No-agent, all five status chip states with `LiveRegionPolite` and manual tools, deterministic `1, 2, 4, 8, 16 s` reconnect backoff and Troubleshoot step targeting, composer multiline input & streamed steps, Claude/Codex JSONL parsers, and macOS `nsgotest` screenshots.
- `packages/game/cmd/game/mcp_test.go`: end-to-end acceptance test (`TestGameMCPInstallConnectReconnectAndComposer`) running `game mcp install` twice, starting a client from the written `.mcp.json`, connecting to the open editor, streaming composer steps, dropping the connection through the 5-attempt schedule, and opening Troubleshoot at the failed step.
