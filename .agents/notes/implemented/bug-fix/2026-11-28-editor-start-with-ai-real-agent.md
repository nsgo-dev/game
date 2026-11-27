# Agent Note: Real agent execution and scene-derived review for Start with AI

Status: implemented

## Problem

The editor's Start with AI first-run flow (`pkg/editor/first_run.go`) presented fixed design-artboard placeholders instead of talking to an agent or inspecting what was written (#1505):

1. `defaultFirstRunConnectAgent` only checked whether a `claude` or `codex` binary existed on `PATH` and returned `18, nil` without starting a process or querying the editor's MCP server; `defaultValidateAPIKey` accepted any string starting with `sk-` or `AIza` of length >= 12 without contacting the provider.
2. When `Editor.FirstRunPlanner` was nil (the production default), `OpenFirstRunPlan` fell back to `DefaultFirstRunPlan`, which always produced the artboard knight/slimes/coins plan regardless of the author's idea or template.
3. `defaultFirstRunBuilder` emitted `"19 of N objects"` and `"placing slime_03"`, wrote a hardcoded knight/slimes/coins scene for every idea and template, reported `"10 s playtest · passed · 0 errors"` without running anything, and returned `defaultFirstRunBuildResult` claiming 11 coins and 612 tiles regardless of disk contents.

## Decision

### 1. Real Connect and API key verification

- `Editor.defaultFirstRunConnectAgent` queries `Editor.AgentTools.ListTools(ctx)` for the live count of enabled editor MCP tools (failing visibly when no MCP server is attached or zero tools are enabled) and runs a read-only readiness probe through `Editor.AgentRunner` (or `defaultCLIAgentRunner()` / `CLIAgentRunner` on desktop). All hardcoded `18` tool fallbacks are removed from `FirstRunConnectedAgent`, `SetFirstRunAgentConnected`, `FirstRunConnectSteps`, and `FinishFirstRunConnect`.
- `Editor.defaultValidateAPIKey` sends one minimal `POST` request (`DefaultBuiltInFastModel`, `max_tokens: 1`) to `BuiltInAgentEndpoint` (default `DefaultBuiltInAgentEndpoint`, validated via `validateAndResolveBuiltInEndpoint`) using `BuiltInHTTPClient`, mapping 2xx to `FirstRunAPIKeyWorks`, 401/403 to `FirstRunAPIKeyNotAccepted`, 402/429 and billing/credit 400s to `FirstRunAPIKeyNoQuota`, and transport/5xx errors to `FirstRunAPIKeyNoNetwork`.
- `FinishFirstRunConnect` persists validated API keys to both the legacy `FirstRunAPIKeyKeychainAccount` and `StoreAgentAPIKey(DefaultAgentAPIKeyAccount, ...)` so `builtInAgentRunner` can immediately authenticate, with an in-memory session fallback when no keychain or `core.Secrets` store is configured.

### 2. Agent-driven planning via `plan_propose` without UI deadlock

- When `Editor.FirstRunPlanner` is nil, `OpenFirstRunPlan` resolves the active runner (`builtInAgentRunner` for API key mode, or `Editor.AgentRunner` / `CLIAgentRunner` for local agents) and sends a planning prompt containing the author's idea, project name, target path, and available templates.
- `editormcp`'s `plan_propose` handler checks `ed.IsFirstRunPlanning()`; when active, it captures the proposed `*AgentPlan` via `ed.CaptureFirstRunAgentPlan` (clearing the working-editor active plan slot) and forces `wait = false` so the tool returns immediately instead of blocking on `decisionCh`.
- When a UI `core.Scope` is mounted (`hasFirstRunScope()`), `OpenFirstRunPlan` runs the agent in a cancellable background goroutine so `editormcp.Options.Run` (`cx.Post`) never deadlocks the UI goroutine; headless callers execute synchronously and receive errors directly.
- When an agent replies without calling `plan_propose` (such as a CLI agent before any project folder exists), `parseFirstRunPlanFromText` parses a structured JSON plan or numbered step lines from the agent's own reply, and fails visibly when no plan is produced.
- `DefaultFirstRunPlan` retains its exported signature as a neutral, template-derived skeleton containing zero artboard strings.

### 3. Real build via `scene_edit`, real playtest or honest skip, and scene-derived review

- `defaultFirstRunBuilder` scaffolds the project (using `Editor.CreateProject` or a clean empty-scene scaffold with zero artboard objects), writes `.mcp.json` when `SyncMCPConfig` is wired, opens the target scene in the editor via `OpenProjectScene` on the editor goroutine, and invokes the connected agent to build the scene via `scene_edit`.
- Live progress events inspect `Editor.Doc` against the pre-agent baseline on every `AgentEventStep`, reporting real object counts, the latest placed object ID or step title, and schematic shapes computed from the actual object coordinates.
- After the agent completes, modified in-memory scenes are saved via `SaveFile` (or disk edits are reloaded into `Editor.Doc`), followed by `project.Check` and `assetcheck.CheckWith`.
- Stage 4 checks for `playtest.yaml` in the project directory: when absent, it marks the stage done with `"Skipped · template has no playtest.yaml"`; when present, it runs `Editor.FirstRunPlaytest` or `go tool game playtest` (`first_run_playtest_desktop.go`, skipped honestly on non-desktop targets) and records pass/failure and missed-goal deviations from the actual `playtest.Report`.
- `buildFirstRunResultFromProject` replaces `defaultFirstRunBuildResult` everywhere (normal completion, early stop, and check-fix paths), deriving review changes, `ObjectIDs`, schematic shapes, and count deviations strictly from the written scene and project files on disk.

## Alternatives considered

### Run `OpenFirstRunPlan` synchronously in all modes

Running the agent synchronously inside `OpenFirstRunPlan` works in headless tests where `editormcp.Options.Run` is nil, but deadlocks whenever a real window or `nsgotest` app mounts `editormcp.New(ed, editormcp.Options{Run: cx.Post})`: the button press runs on the UI goroutine, waits for the agent, which invokes `plan_propose`, which posts back to the blocked UI goroutine. Running planning off the UI goroutine whenever a scope is mounted (and synchronously when headless) prevents UI deadlocks while preserving synchronous error returns in headless unit tests.

### Keep a static fallback plan when the agent fails

Falling back to `DefaultFirstRunPlan` when the agent returns an error would hide broken credentials, CLI login issues, or malformed responses behind a plausible-looking synthetic plan—the exact dishonesty #1505 removes. Showing an explicit error state with **Try again** and **Back to Start** keeps failures visible and recoverable.

## Consequences

- **Benefits**:
  - No agent binary + no accepted API key can never reach `Connected`, and tool counts reflect the live `editormcp.Server` registry.
  - Plans, live build counts, playtest results, and first-review change lists reflect the connected agent's actual output and the exact objects written to disk.
  - Zero design-artboard strings (knight, slimes, coin counts, 612 tiles, 3 rooms, 10 s playtest) appear unless produced by the agent.
- **Costs and known limits**:
  - Unit tests that exercise first-run UI flows without testing real agent integration must inject a scripted `AgentRunner` or `FirstRunPlanner` so tests do not invoke a host machine's `claude`/`codex` binary.
  - Because no project directory exists before Approve, external CLI agents that do not have a pre-existing MCP session attached during Step 3 return their plan via the structured JSON prompt contract rather than calling `plan_propose`.

## Testing

- `pkg/editor/first_run_test.go`: verifies no-binary/no-key cannot reach Connected (including rejecting prefix-only `"sk-123456789012"` via real HTTP validation), verifies scripted agent Connect/Plan/Build/Review for `"a kart racer on a beach"` matches the exact written scene with zero artboard strings, and verifies Stage 4 runs `FirstRunPlaytest` when `playtest.yaml` exists (pass and fail) and skips honestly when absent.
- `pkg/editor/editormcp/first_run_test.go`: end-to-end UI + real `editormcp.Server` + built-in HTTP agent test covering HTTP API key ping, real enabled tool count, `plan_propose` (with `wait: true`) without UI deadlock, `scene_edit` object placement, and exact review/disk parity.
- `pkg/editor/editorcmd/first_run_screenshot_test.go`: native macOS screenshots of all five Start with AI stages driven by a scripted agent over `editormcp.Server`.
