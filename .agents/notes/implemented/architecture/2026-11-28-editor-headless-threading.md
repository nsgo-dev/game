# Agent Note: Unified headless executor and threading contract for Editor

Status: implemented

## Problem

When `Editor` runs in a window (`Editor.AttachScope(cx)` with a live `core.Scope`), every UI mutation happens on the scope's UI goroutine and background work returns via `cx.Post`. When no scope is attached (headless tests and scripts), features previously fell back to separate private mutexes (`agent.inlineMu`, `firstRun.headlessMu`, `tasks.inlineMu`, `editormcp.Server.inline`) or raw inline calls on worker goroutines (`start_screen.go`, `asset_build.go`, `export_panel.go`, `tasks.go`).

That split broke single-owner invariants in three ways (#1547, part of #1479):

1. Background callbacks from one feature raced with callbacks from other features on shared `Editor` state (`revision`, `listeners`, `autosave`, `agent`, `ProjectPath`).
2. A caller owning a scope-less `Editor` (tests and scripts) called `Editor` methods without holding any of those private mutexes, racing directly with inline automation, first-run, task, and MCP session callbacks (`go tool nsgo test ./pkg/editor/... -race` reported 26 races across `TestAutomationsAllFiveTriggers`, `TestAutomationsPauseAllForTodayUntilNextDay`, `TestFirstRunConnectStepWrapsInsideCard`, and `TestServerOpenFollowsTheProject`).
3. `AttachScope(cx)` only drained `agent.inlineMu`, leaving `firstRun.headlessMu`, `tasks.inlineMu`, and `editormcp.Server.inline` callbacks able to run `Editor.notify()` concurrently with `EditorView`'s `Editor.Subscribe(...)`.

## Decision

Give every scope-less `Editor` a single serialized owner (`editorHeadless` in `pkg/editor/headless.go`) shared by all headless background callbacks, `editormcp.Server` (when `Options.Run == nil`), and callers driving a scope-less `Editor`:

- **Single ownership model**:
  - When a live `core.Scope` is attached, the UI goroutine owns `Editor` and background callbacks route to `cx.Post`.
  - When no `core.Scope` is attached, `editorHeadless` is the sole owner: at most one `RunHeadless` call, `PostOrRunHeadless` call, `detachScope` teardown, or headless queue callback executes at a time, with full happens-before synchronization across ownership handoffs.
- **Async `Editor.Post` / `postOrRun`**:
  - When a live scope is attached, `Post` forwards directly to `cx.Post`.
  - When no scope is attached, `Post` appends to a FIFO slice queue and lazily spawns a self-terminating drain goroutine (zero goroutine leak when idle or when tests omit `Close`). Because `Post` never blocks waiting for ownership, calling `Post` from inside `RunHeadless` (such as `ExecuteCodeReviewCheckRun` progress callbacks) or from inside another posted callback never deadlocks, matching `core.Scope.Post` semantics.
  - All headless background callbacks (`postOrRun`, `firstRunPostOrRun`, `tasks.startTaskRunner`, `start_screen`, `asset_build`, `export_panel`) route through `e.postOrRun`; the separate `agent.inlineMu`, `firstRun.headlessMu`, `tasks.inlineMu`, `lockHeadlessAgentOp`, and `lockHeadlessTaskOp` mechanisms are removed.
- **Sync `Editor.RunHeadless(fn)` and `Editor.RunHeadlessContext(ctx, fn)`**:
  - Acquire exclusive headless ownership (honoring `ctx.Done()` in `RunHeadlessContext`) and run `fn` directly on the caller's goroutine (releasing via `defer`, so panics and `testing.T.Fatal`/`runtime.Goexit` work natively).
  - Tests and scripts driving a scope-less `Editor` while background work or an `editormcp.Server` (with nil `Options.Run`) is active must wrap direct `Editor` calls in `Editor.RunHeadless`. Do not nest `RunHeadless` inside `RunHeadless`; use `Editor.Post` for follow-up work.
- **Cancelable `Editor.PostOrRunHeadless(ctx, fn)` for `editormcp.Server`**:
  - Atomically checks under `headless.mu` whether a live scope is attached (posting asynchronously to `cx.Post` if so, including if a scope attaches while waiting) or acquires exclusive headless ownership while honoring `ctx.Done()`, then runs `fn` synchronously.
  - `editormcp.Server` removes `s.inline`, routes `Server.Editor` through `Editor.RunHeadlessContext` and `Server.notifyEditor` through `Editor.PostOrRunHeadless` when `Options.Run == nil`, and cancels a server-lifetime context in `Server.Close` before `s.wg.Wait()` so calling `Server.Close` inside `Editor.RunHeadless` never ABBA-deadlocks against an in-flight `serveConn`/`dropSession` notification. This updates [The editor's MCP server on a per-project local endpoint](2026-11-25-editor-mcp-server.md).
- **Atomic scope transitions in `AttachScope`**:
  - `AttachScope(cx)` with `cx != nil` publishes `cx` under `headless.mu`, waits for any in-flight headless callback to finish, and forwards any queued headless callbacks to `cx.Post` in FIFO order before writing `e.scope`, attaching subsystem scopes, or letting `EditorView` call `Subscribe`.
  - `AttachScope(nil)` runs scope teardown and timer/runner cancellation under `headless.detachScope` (holding exclusive ownership) so a concurrently disposing scope cannot let a background callback race with teardown.
- **Handshake and build completion ordering in `pkg/editor/first_run.go`**:
  - `StartWithAI` and `SelectFirstRunAgent` call `e.notify()` before spawning `startFirstRunConnectHandshake`, and `ApproveFirstRunPlan` invokes `run.finish()` inside the posted completion callback (and on early cancellation) so `WaitForFirstRunBuild` never returns before `finishFirstRunBuild` finishes.

## Alternatives considered

### Acquire a mutex inside every exported `Editor` method

Serializing inside every exported method would let headless callers omit `RunHeadless`. Rejected because `Editor` has hundreds of exported methods that call each other (a non-reentrant mutex would deadlock on internal method calls) and exposes exported fields (`Doc`, `ProjectPath`, `Status`, `SelectedID`, `History`) that callers and subscribers read directly without going through a method.

### Run `RunHeadless` callbacks on a dedicated persistent goroutine

Running every `RunHeadless` call on a dedicated background goroutine would isolate ownership to one OS/Go goroutine, but breaks `t.Fatal`/`t.Skip` inside `ed.RunHeadless(func() { ... })` (which invoke `runtime.Goexit` and must run on the test goroutine) and leaks a goroutine for every test that constructs an `Editor` without calling `Close`. Running `RunHeadless` synchronously on the caller's goroutine under a shared exclusive token while draining async `Post` callbacks on a self-terminating goroutine avoids both problems.

### Make `editormcp.Server.notifyEditor` asynchronous via `Editor.Post` when `Options.Run == nil`

Using async `Post` for `notifyEditor` would simplify `Server`, but `Server.handler` records tool calls (`RecordAgentToolCall`) right before returning the MCP tool result; making that asynchronous would let headless tests observe the MCP response and immediately mutate `Editor` while `RecordAgentToolCall` is still draining, introducing races across the `editormcp` test suite. `PostOrRunHeadless` keeps headless tool-call recording synchronous while still switching cleanly to `cx.Post` if a scope attaches.

## Consequences

- **Benefits**: One threading contract covers all headless callbacks and `editormcp.Server`; `AttachScope` deterministically drains in-flight work and preserves queued FIFO callbacks; zero goroutine leaks when idle; `pkg/editor` and `pkg/editor/editormcp` run cleanly under `-race`.
- **Costs and known limits**: Headless callers (tests and scripts) that drive an `Editor` concurrently with background work or a nil-`Run` `editormcp.Server` must wrap direct `Editor` calls in `Editor.RunHeadless`, and must not nest `RunHeadless` calls. `Editor` exports three new methods (`RunHeadless`, `RunHeadlessContext`, and `PostOrRunHeadless`), documented on `Editor`, `editormcp.Options.Run`, `docs/en/mcp.md`, and `docs/zh/mcp.md`.

## Testing

- `pkg/editor/headless_test.go`: `TestHeadlessExecutorSerializesAndSupportsReentrantPostAndCancel` (concurrent `RunHeadless`/`Post`/`PostOrRunHeadless`/`Subscribe`, reentrant `Post`, canceled context) and `TestAttachScopeDrainsInFlightAndForwardsQueuedHeadlessWork` (drain barrier and FIFO forwarding before `EditorView` subscribes).
- `pkg/editor/editormcp/server_test.go`: `TestHeadlessServerSerializesWithRunHeadlessAndClosesCleanly` (concurrent nil-`Run` MCP calls vs `RunHeadless` mutations, plus `Server.Close` inside `RunHeadless` verifying no ABBA deadlock).
- Updated `TestAutomationsAllFiveTriggers`, `TestAutomationsPauseAllForTodayUntilNextDay`, `TestFirstRunConnectStepWrapsInsideCard`, and `TestServerOpenFollowsTheProject` to drive headless mutations through `Editor.RunHeadless`.
- Verified with `-race -count=20` on all target and new tests, `-race` on `pkg/editor/editormcp` and `pkg/editor` background suites, `go tool nsgo check -target host -jobs 2`, `bun run verify-notes`, and `(cd tools && go run ./changelog check)`.
