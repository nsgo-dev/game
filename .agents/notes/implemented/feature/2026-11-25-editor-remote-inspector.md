# Agent Note: Editor remote scene tree and live inspector over MCP

Status: implemented

## Problem

The game layer (`packages/game/mcp`) already exposes a running game's scenes, layers, entities, components, fixed-step loop controls, tunables, and debug console over NSGo's automation bridge, and the editor (`packages/game/editor`) can launch the game via `RunGame` / `RunGameWith` ([editor run log panel](2026-11-25-editor-run-log-panel.md)). However, the editor had no MCP client, no Remote view of live runtime entities, no way to edit live components or tunables while the game runs, and no way to pause, step N fixed updates, scale time, run debug console commands, or promote tuned live values back into the open scene file as an undoable edit.

## Decision

Add a remote MCP client, live scene tree tab, live inspector, Play Bar, live-change promotion tray, tunables editor, and console runner to `packages/game/editor` (`remote.go`, `remote_ui.go`, `remote_transport_desktop.go`, `remote_transport_other.go`), plus `-automation` forwarding in `packages/game/cmd/game/dev.go`:

- **MCP client over official Go SDK**: `Editor.ConnectRemote(ctx, transport)` connects using `github.com/modelcontextprotocol/go-sdk/mcp` (already a direct dependency of `packages/game`, shared with [the editor's MCP server](../../implemented/architecture/2026-11-25-editor-mcp-server.md)). On desktop targets (`darwin`, `linux`, `freebsd`, `openbsd`, `netbsd`, `windows`, excluding `android`/`ios`), `ConnectRemoteCommand` spawns `go tool nsgo mcp` via `mcp.CommandTransport`; non-desktop targets compile against `remote_transport_other.go` returning `ErrRemoteUnsupported`. `Editor.RemoteTransport` injects custom transports (in-memory or `io.Pipe`), and `NewRemoteBridgeServer(bridge, run)` wraps a live `*gamemcp.Bridge` as an `*mcp.Server` using the same JSON text wire format as `nsgo mcp`.
- **Opt-in `-automation` launch handoff**: `RunGameOptions.Automation` (and `Editor.RunWithAutomation()` / `SetRunWithAutomation(bool)`, defaulting to `false` to preserve existing `go tool game dev` runner invocations) passes `-automation` to `go tool game dev` and auto-connects in the background with exponential backoff until the game's automation server answers or the game exits. The transport context is owned by the remote session (cancelled only on `DisconnectRemote` or failed dial attempts), never cancelled when the dial goroutine exits on success.
- **Change-gated polling and UI-goroutine safety**: background polling runs at `Editor.RemoteRefreshInterval` (default 250 ms) while unpaused, pauses polling while the game loop is paused (since paused state only changes on explicit editor actions such as Step, component edit, tunable set, or console exec, each of which refreshes synchronously), and computes a snapshot digest so `core.Scope.Post(e.notify)` only fires when live state actually changes. Tree selection sync effects guard against disconnected state and clear cursor/selection idempotently so disconnecting never triggers reactive effect cycles.
- **Remote tab, Live Inspector, and Live Changes**: `SceneTreeView` adds a **Scene** / **Remote** tab switcher without altering local `SceneTreeView` test IDs or behavior. The Remote tab lists active scenes (`game_scenes_list`), layers (`game_layers_list`), live entity hierarchy (`game_entities_list`), runtime-spawned entities without a scene `ObjectRef`, and dimmed taken scene entities. `InspectorView` switches to the memoized Remote Inspector when on the Remote tab, rendering read-only derived components (`world_transform2d`, `object_ref`) separately from editable components (`game_component_set`) with `"was X"` indicators, **Tunables** (`game_tunables_list` / `game_tunable_set`), and a focus-preserving **Console** (`game_console_exec`). Every live edit is tracked in `RemoteLiveChanges` (preserved across disconnect/stop), can be reverted per row or discarded in bulk, or promoted into `Editor.Doc` via `KeepRemoteLiveChangesInScene` inside a single `History.Group` step so one Undo restores every kept change.
- **Play Bar and shortcuts**: while connected or connecting, `remotePlayBarView` renders the live/paused pill, Pause/Resume (`mod+shift+p`), Step 1 (`.` while paused), Step N, Time Scale segmented toggles (`0.25×`, `0.5×`, `1×`, `2×`), Refresh, and Disconnect using only `ui.ColorsOf(cx)` tokens.

## Alternatives considered

- **Connect directly to NSGo's internal HTTP automation protocol instead of `go tool nsgo mcp`** — Avoids spawning the `nsgo mcp` stdio process. Rejected because NSGo's internal automation session file and HTTP protocol live under `github.com/nsgo-dev/nsgo/internal/automation`, which external modules cannot import, and issue #911 explicitly specifies connecting through `go tool nsgo mcp` as an MCP client.
- **Enable `-automation` unconditionally in `RunGame()`** — Always connects on F5 without opting in. Rejected because games without `-automation` support (and existing runner fixture tests that assert exact `go tool game dev` arguments) would break; making `-automation` opt-in via `RunGameOptions.Automation` and the Remote tab's **Auto-connect on Run** toggle keeps backward compatibility and avoids crowding the main toolbar on narrow windows.
- **Place the remote client in a separate subpackage under `editor/remote`** — Keeps `github.com/modelcontextprotocol/go-sdk/mcp` out of `packages/game/editor`. Rejected because the Remote tab, Live Play Bar, Live Inspector, and `KeepRemoteLiveChangesInScene` integrate directly into `Editor`, `SceneTreeView`, `InspectorView`, `RunGameWith`, and `EditorView`, and `editor` is already an experimental editor-only package not linked by games.

## Consequences

- **Benefits**:
  - Authors can inspect live scenes, layers, and entity hierarchies, pause and step exact counts of fixed updates, scale simulation time, edit live components and tunables, run console commands, and promote tuned values into the open scene with single-step Undo.
  - Both in-memory and `io.Pipe` MCP transports work identically to `go tool nsgo mcp`, enabling fast, deterministic headless and native macOS tests with zero subprocess flakiness.
- **Costs and known limits**:
  - `packages/game/editor` now imports `github.com/modelcontextprotocol/go-sdk/mcp` and `packages/game/mcp` (both experimental; `TestStabilityTable` remains clean).
  - Polling refreshes live entities at 250 ms while unpaused rather than streaming push diffs, because `game/mcp` exposes pull-based MCP tools without resource subscriptions.
  - `KeepRemoteLiveChangesInScene` maps live edits for entities that carry a `sceneformat.ObjectRef` (or matching scene object ID) into `Editor.Doc`; runtime-spawned entities without a scene object stay in the live-changes list until discarded.

## Verification

- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./cmd/game)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./mcp)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestFormatsPage')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target ios ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web ./cmd/game)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
