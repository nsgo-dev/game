# Agent Note: Single request-to-model contract for editor agent runners

Status: implemented

## Problem

`Editor.AssembleAgentRequest` (`pkg/editor/agent_composer.go`) resolves every active composer chip (Always chips `brief.yaml` / `AGENTS.md`, `@`-mentioned objects/files/prefabs/assets/problems/playtests/history/rules, the toggleable Viewport image PNG, and compacted/fresh-thread summaries) into `AgentRequest.Attachments`, `AgentRequest.ViewportImage`, and `AgentRequest.AssembledContext`. Hovering any chip previews that exact payload, and the context meter accounts for its token cost.

However, none of the three production runners consumed those fields (#1742):

1. `CLIAgentRunner` (`pkg/editor/agent_runner_desktop.go`) started from `req.Prompt` and only appended reference images (`req.Images`), dropping `AssembledContext`, `Attachments`, and `ViewportImage` for both Claude Code and Codex.
2. `builtInAgentRunner` (`pkg/editor/agent_builtin.go`) sent only `req.Prompt` and `req.Images` under a two-line system prompt, ignored `req.PriorTurns` and thread history (so every follow-up forgot the prior turn), and never received `editormcp.Server`'s `instructions` telling the model to read `project_context` first.
3. Composer tests verified `AssembleAgentRequest` in isolation while runner tests verified raw prompts, leaving the handoff between composer assembly and runner wire formatting untested.

## Decision

### 1. Canonical request-to-model renderer (`RenderAgentModelInput`)

`pkg/editor/agent_request_input.go` defines `AgentModelInput` and `RenderAgentModelInput(req AgentRequest) AgentModelInput` as the single bridge between `AgentRequest` and every runner:

- **Always vs. per-turn split**: `AlwaysContext` extracts formatted Always chips (`brief.yaml`, `AGENTS.md`) from `req.Attachments`. `TurnContext` holds everything else from `req.AssembledContext` (compacted/seed summary, explicitly attached chips, viewport header, and any caller-supplied context remainder) with exact Always blocks removed so runners with a system-prompt channel never duplicate project rules across turns. `Context` keeps the complete combined block for runners without a separate system-prompt channel.
- **Prompt formatting**: `TurnPrompt` and `FullPrompt` join the context block and `Prompt` with `"\n\n---\n\n"` when context is non-empty, and return `Prompt` verbatim when context is empty (preserving CLI flag-injection guarantees and empty-context tests).
- **Unified image list**: `Images` combines `req.Images`, the viewport PNG (`req.ViewportImage` or `ChipKindViewportImage` attachment, materialized as `viewport.png`), and attached image-asset chip PNGs (`ChipKindAsset`, deduplicated by project-relative path).

### 2. Per-runner delivery contract

- **Claude Code (`CLIAgentRunner`)**: passes `input.AlwaysContext` via `--append-system-prompt=<value>` (single `=`-joined arg so leading dashes in `brief.yaml` or `AGENTS.md` can never be parsed as flags), passes `input.TurnPrompt` after `--`, and materializes ephemeral images (including `viewport.png`) via `materializeEphemeralCLIImages(input.Images)` into a private `0o600` temp directory cleaned up on return.
- **Codex (`CLIAgentRunner`)**: passes `input.FullPrompt` after `--` (Codex has no `--append-system-prompt` flag), attaches validated project image paths and materialized ephemeral images (including `viewport.png`) via `-i <path>` on fresh `codex exec` runs, and lists them under `Reference images:` on `codex exec resume`.
- **Built-in Anthropic API runner (`builtInAgentRunner`)**:
  - Reads MCP server instructions through the optional `AgentToolInstructionsProvider` interface implemented by `editormcp`'s `serverToolExecutor` (preserving the `pkg/editor` -> `pkg/editor/editormcp` layering boundary established in [built-in agent runner](../../implemented/feature/2026-11-26-editor-builtin-agent-models-usage.md)).
  - Builds the `system` prompt from the base role line, server `instructions`, scene/selection metadata, and `input.AlwaysContext`.
  - Builds the user message from base64 `image` blocks for `input.Images` (skipping the viewport PNG when `Editor.AgentPrivacy().ViewportImages` is false) followed by `input.TurnPrompt`.

### 3. Bounded per-thread conversation history on the built-in runner

`Editor` stores per-thread built-in message history under `agentState.builtInHistoryMu` (`map[string]*builtInThreadHistory`), keyed by thread ID:

- Each completed turn is stored as an atomic `builtInTurnRecord` (`[user, (assistant tool_use, user tool_result)*, assistant final]`) so trimming oldest turns never breaks Anthropic `user`/`assistant` alternation or orphans `tool_use`/`tool_result` pairs.
- Archiving a turn strips raw base64 `image` blocks (`"[image omitted from earlier turn]"`) and caps archived `tool_result` text at `8 KiB`, with thread bounds of `16` turns, `256 KiB` serialized JSON per thread, and `32` tracked threads (LRU eviction).
- An epoch counter per thread prevents an in-flight background run from writing stale turns back after `CompactAgentThread`, `StartFreshAgentThread`, `RewindAgentTurn`, or `resetAgentForProject`.
- `CompactAgentThread` and `StartFreshAgentThread` drop the thread's built-in history (setting `Compacted = true` so stale `PriorTurns` are not re-hydrated) because the next request carries `# Compacted earlier requests` or `# Thread summary` in `TurnContext`. Both methods also fall back to live `e.agent.turns` when `e.agentComposer.turns` is empty.
- When a thread has no built-in history and no compacted/fresh summary, `snapshotBuiltInThreadHistory` seeds alternating `user`/`assistant` pairs from completed `req.PriorTurns` (supporting failover or route switches into the built-in runner mid-thread).
- `RewindAgentTurn` truncates dropped turn IDs from built-in history and records the snapshot on `agentRewindRecord` so `RedoAgentRewind` restores it.

## Alternatives considered

### Pass Always chips inside the user prompt for every runner

Putting `brief.yaml` and `AGENTS.md` inside the user prompt for the built-in runner and Claude Code would duplicate project rules in every stored turn of a multi-turn thread and consume conversation history budget on repeated boilerplate. Splitting `AlwaysContext` into the system prompt (and `--append-system-prompt=` for Claude Code) keeps project instructions authoritative and fresh on every turn while Codex still receives them via `FullPrompt`.

### Keep built-in history on `builtInAgentRunner` instead of `Editor`

`Editor.newBuiltInAgentRunner` allocates a fresh `*builtInAgentRunner` for each submitted request, so instance-local history would be lost immediately between turns and could not be cleared by `CompactAgentThread`, `StartFreshAgentThread`, or `RewindAgentTurn`. Storing mutex-guarded per-thread history on `Editor` with epoch invalidation coordinates all lifecycle transitions cleanly without deadlocking the UI goroutine.

### Reconstruct built-in follow-up history solely from `req.PriorTurns`

`AgentTurnSummary` only stores prompt, text summary, and changed filenames—not tool calls or tool results—and would not drop cleanly on `CompactAgentThread` without mutating `e.agent.turns`. Keeping structured per-turn message records for live built-in threads while falling back to `req.PriorTurns` only when no built-in history or compacted summary exists gives full tool-call continuity on follow-ups and graceful continuity after route failover.

## Consequences

- **Benefits**:
  - Every chip previewed on hover (Always chips, attached objects/files/prefabs/assets/problems/playtests/history/rules, viewport PNG, and compacted/seed summaries) reaches Claude Code, Codex, and the built-in runner through one shared contract.
  - Built-in follow-up turns preserve full prior turn context (including tool calls and results) without leaking multi-MiB images across turns, and honor Compact, Start fresh, Rewind/Redo, thread switching, and `PrivacyViewportImages`.
  - `editormcp.Server` instructions reach the built-in model automatically without inverting package dependencies.
- **Costs and known limits**:
  - Built-in message history lives in memory for the editor session; restarting the editor hydrates prior context from summaries or `PriorTurns` rather than replaying raw Anthropic tool-use blocks from disk.
  - CLI runners pass context via process arguments (`argv`), which remains bounded by `maxAssembledContextBytes` and host `ARG_MAX` limits.

## Testing

- `pkg/editor/agent_request_input_test.go`: unit tests for `RenderAgentModelInput` covering empty prompt preservation, manual `AssembledContext` + `ViewportImage` requests, Always/Turn splitting, compacted summaries, and deduplicated viewport/asset images.
- `pkg/editor/agent_runner_desktop_test.go`: fake-CLI subprocess contract tests for Claude Code (`--append-system-prompt`, prompt, temp viewport PNG creation and cleanup), Codex fresh (`-i` viewport/asset images and full context), and Codex resume.
- `pkg/editor/agent_builtin_test.go`: fake HTTP endpoint contract test verifying server instructions, Always chips, attached chips, base64 viewport PNG, multi-turn follow-up history with old-image stripping, Compact, Start fresh, Rewind, and `PrivacyViewportImages=false`.
- `pkg/editor/editormcp/builtin_agent_test.go`: verifies real `editormcp.New` wires server `instructions` (`project_context`) into the built-in runner's system prompt.
