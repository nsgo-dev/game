# Agent Note: Built-in agent runner with `cx.Secrets`, task routing, limit queueing, and model-error failover

Status: implemented

## Problem

Issue #1030 layers the live built-in agent runner on top of the [Agent Settings pages](../../implemented/feature/2026-11-26-editor-settings-agent-pages.md) (#1028), [connecting external agents](../../implemented/architecture/2026-11-26-editor-agent-connect.md) (#1009), and the [You-only boundary](../../implemented/feature/2026-11-26-editor-you-only-and-proposed-deletions.md) (#1015). Authors need to run requests directly through their own API key without an external MCP CLI, route each **WHO DOES WHAT** task kind to either a connected local MCP agent or a built-in Anthropic model, queue overflow prompts when the person-only weekly request limit is reached, and recover cleanly from transient HTTP `529` / `429` / `5xx` errors by retrying with backoff or failing over to another connected agent.

Four cross-module contracts needed to be reconciled cleanly with #1028's settings schema and MCP server enforcement:

1. **Unified secret storage (`cx.Secrets` + `Editor.Keychain`)**: #1030 requires reading/writing API keys via NSGo's `cx.Secrets` (backed by the OS keychain in apps and `nsgotest.Secrets` in tests), while #1028 also supports explicit `Editor.Keychain` injection in tests. Both must share `You only` enforcement (`ErrAgentSettingsYouOnly`) and never write keys to `editor.yaml`, project files, or logs.
2. **Inverted MCP tool bridge enforcing #1028 policies**: `packages/game/editor` cannot import `packages/game/editor/editormcp`, yet every built-in agent tool call must pass through `editormcp.Server`'s enabled-tool filter, permission rules, `WithAgentActor` isolation, and symlink-aware outbound privacy checks.
3. **Hardened HTTP transport and non-deadlocking backoff**: the built-in runner must validate endpoint URLs (refusing plaintext `http://` to remote hosts, embedded userinfo, and cross-host/downgrade redirects), bound response bodies and tool outputs, redact secrets from all error paths, and never deadlock if the UI scope is disposed while waiting on a retry card update.
4. **Single source of truth for WHO DOES WHAT and weekly limits**: task routing and weekly usage counts must reuse #1028's `settingsMu`-guarded `EditorSettings.Agent` (`routes`, `weeklyRequestLimit`, `usage`) rather than duplicating state or forking `editor-settings.v1.json`.

## Decision

### 1. Unified API key lookup (`core.Secrets` with `Editor.Keychain` compatibility)

- `agent_builtin.go` defines `AgentAPIKeySecretAccount = "agent.anthropic.api_key"` and `AgentSecretStore` matching NSGo's `core.Secrets` interface (`Get`/`Set`/`Delete`), plus `MemorySecretStore` (whose `String()`/`GoString()` redact values).
- `agent_settings.go`'s `StoreAgentAPIKey`, `DeleteAgentAPIKey`, `AgentAPIKey`, `HasAgentAPIKey`, and `RefreshAgentAPIKeyStatus` resolve secret storage in deterministic precedence: `Editor.AgentSecretStore` (if explicitly set) -> `Editor.Keychain` (if explicitly set, keeping #1028 tests intact) -> `cx.Secrets()` when a `core.Scope` is attached -> `activeKeychain()` fallback.
- Key presence is cached under `e.settingsMu` and invalidated when a new `core.Scope` attaches (`AttachScope`) or settings reload (`ApplySettings`, which now also refuses `AgentActorActive()` with `ErrAgentSettingsYouOnly`).

### 2. Inverted MCP tool bridge (`editor.AgentToolExecutor`) with full policy enforcement

- `packages/game/editor` declares `AgentToolExecutor` (`ListTools` / `CallTool`) and `Editor.AgentTools`.
- `editormcp.New` wires `serverToolExecutor` onto `ed.AgentTools` (and clears it on `Server.Close()`). `ListTools` filters out any tool disabled in **Settings › Tools** (such as default-off `game_delete_file`) so disabled tools are never advertised to the remote model. `CallTool` delegates to `Server.call(...)`, inheriting tool-disabled checks, permission rules (#1010), symlink-aware pre/post privacy checks (#1028), `WithAgentActor` isolation (#1015), and save-failure warnings without creating a synthetic external MCP session.
- `builtInAgentRunner.executeToolWithPrivacy` adds defense-in-depth live checks against `Editor.IsMCPToolEnabled` and `Editor.AgentPrivacy()` (including `ClassifyProjectFilePrivacy` on `scene_read` and stripping PNGs when `ViewportImages` is off).

### 3. Reusing #1028's WHO DOES WHAT routes and weekly usage limits

- `AgentTaskKind` uses the exact canonical keys from `AgentTaskRoutesDefs()` (`scene_edits`, `playtest_analysis`, `problems_fixes`, `ask_in_place`, `art_and_sound`). `ResolveAgentModelRoute` maps the persisted route label (`Claude Code · default`, `Anthropic API · default`, `Anthropic API · fast`, `Not connected`) to `AgentModelRoute`.
- `SwitchAgentTurnToAlternative` (refused for agent actors with `ErrAgentSettingsYouOnly`) cancels the active backoff run, persists the new label via `SetAgentTaskRoute` so **Settings › Models and connections** stays in sync, and re-runs on the alternative connected agent without double-charging `WeeklyRequests`.
- Reaching `WeeklyRequestLimit()` transitions status to `AgentStatusLimitReached` (`Limit reached · resets 00:00` + **Usage** link) and appends subsequent prompts to a bounded queue (`maxQueuedAgentRequests = 64`) until `CheckAgentUsageReset()` crosses Monday `00:00` or the person raises the limit and clicks **Send**.

### 4. Hardened built-in Anthropic Messages API loop

- `validateAndResolveBuiltInEndpoint` permits only `https://` (or loopback `http://127.0.0.1` / `::1` / `localhost` for local tests), refuses embedded URL credentials, and default `http.Client` installs `builtInCheckRedirect` refusing cross-host or HTTPS-to-HTTP redirects.
- Response reads are capped via `io.LimitReader(resp.Body, builtInMaxResponseBytes+1)` (8 MiB), failing immediately as non-retryable on oversize or invalid JSON; tool results and error messages are bounded and passed through `redactSecret`.
- Transient errors (`429`, `5xx`, `529` overloaded) retry on `AgentModelRetryDelays = [4s, 8s, 16s, 32s]`. `postModelError` updates `AgentTurn.ModelError` and schedules the backoff timer atomically on the editor goroutine while selecting on `ctx.Done()` so scope disposal never deadlocks. `AttachScope(nil)` and `resetAgentForProject()` cancel active runs and retry timers cleanly.

## Alternatives considered

### Duplicate Settings pages and `AgentSettings` fields from pre-#1028 drafts

Keeping a separate `AgentSettings` layout (`weeklyLimit` vs `weeklyRequestLimit`, `scene_edit` vs `scene_edits`) would have fractured `editor-settings.v1.json` and duplicated #1028's Settings UI. Rejected in favor of layering directly onto #1028's schema, `settingsMu` locking, and Settings pages.

### Use an in-memory MCP client session for the built-in agent

Connecting the built-in agent over `mcp.NewInMemoryTransports` would register it in `Server.sessions` / `Editor.AgentClients()` and conflate the built-in API connection with local MCP clients (Claude Code / Codex) in the Connect dialog, status chip, and failover picker. Rejected in favor of the `AgentToolExecutor` bridge directly over `Server.Tools()` (filtered by `isToolEnabled`) and `Server.call(...)`.

### Auto-send queued requests immediately when the limit rises

Automatically firing queued prompts as soon as the limit increases or resets could start unexpected model spend. Rejected to match `docs/design/editor/AgentStates.dc.html`, which keeps queued prompts visible until the person explicitly presses **Send** or removes them.

## Consequences

- **Benefits**:
  - Zero duplication with #1028's Agent Settings pages, privacy enforcement, or `editor-settings.v1.json` schema.
  - Built-in agent runs end-to-end with `cx.Secrets()` and real `editormcp` tools under the exact same security, permission, and privacy rules as external agents.
  - Endpoint validation, redirect guards, bounded reads, secret redaction, and `ctx`-aware UI posting prevent secret leakage, memory exhaustion, and goroutine leaks.
- **Costs and known limits**:
  - The built-in HTTP runner currently targets the Anthropic Messages API wire format; additional providers can plug behind `builtInAgentRunner` later without changing Settings or UI contracts.

## Testing

- `packages/game/editor/agent_builtin_test.go`: tests weekly limit queueing, `ErrAgentSettingsYouOnly` refusals across every mutator and `AgentAPIKey`, `529` backoff schedule (`4s, 8s, 16s`), **Retry now**, **Use Claude Code** failover without double-charging usage, insecure endpoint refusal, secret redaction on 400 error bodies, and oversized response rejection.
- `packages/game/editor/editormcp/builtin_agent_test.go`: tests end-to-end built-in agent execution with `nsgotest.Secrets` and real `editormcp.Server` (`scene_edit`, single-step Undo, zero key leakage across project and settings files) plus enforcement of default-off `game_delete_file` and `GoSource=false` privacy.
- `packages/game/editor/editorcmd/agent_builtin_screenshot_test.go`: real macOS `nsgotest` screenshots (`-target macos`) of Connect API key step, 529 Model error card (Dark & Light), and Limit reached + queue (Dark & Light).
