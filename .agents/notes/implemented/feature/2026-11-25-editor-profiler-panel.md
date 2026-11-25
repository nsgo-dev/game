# Agent Note: Editor profiler panel and Play-mode frame time strip

Status: implemented

## Problem

The game layer already collects frame timings, draw statistics, allocations per frame, GC hitches, asset memory, and named profiler timing zones in `packages/game/debug` and serves them over MCP (`game_perf_readout`, `game_perf_zones`, `game_perf_reset` in `packages/game/mcp`), and `docs/design/editor/PlayMode.dc.html` specifies a persistent `FRAME TIME · LAST 60 S` strip below the game view in Play mode that opens a full Profiler panel. Without an editor profiler client and UI, authors had no way to watch frame cost, memory, or named zones from the editor or export recorded frames as a Chrome/Perfetto trace.

## Decision

1. **Transport-agnostic MCP profiler client (`packages/game/editor/profiler.go`)**:
   - `ProfilerClient` abstracts calling `game_perf_readout`, `game_perf_zones`, and `game_perf_reset`.
   - `ConnectProfilerMCP` implements `ProfilerClient` over the official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk/mcp`) for any `mcp.Transport`, `ConnectProfilerCommand` launches `go tool nsgo mcp` over `mcp.CommandTransport`, `ConnectProfilerScope` calls `automation.Call` on a live `*core.Scope` (automatically dispatching background polls onto the bound scope's goroutine), and `NewBridgeProfilerClient` invokes `mcp.Bridge` handlers directly in-process.
   - `Editor.ConnectProfiler` polls on a background goroutine off the UI thread (default `250ms`, configurable via `Editor.ProfilerPollInterval`), merges newly arrived `game_perf_readout` history samples without duplicating overlapping ring-buffer entries, and posts notifications to the attached `core.Scope`.
   - Built-in Play mode (`packages/game/editor/play.go`) instruments `physics`, `update`, and `draw` with local `debug.Overlay`, `debug.Profiler`, and `debug.GCMonitor` instances whenever no external profiler client is connected, detaching cleanly on `Stop()`.

2. **Spike cause propagation (`packages/game/debug`, `packages/game/mcp`)**:
   - `debug.FrameSample.Cause` and `debug.Overlay.NoteFrameCause(cause string)` (with a zero-cost no-op stub in `release.go`) let a game attach a short explanation (such as shader compilation) to the next recorded frame with zero allocations on steady-state frames.
   - `mcp.PerfFrameSample.Cause` exposes that explanation with `json:"cause,omitempty"`, and the editor falls back to `"GC pause"` when `GCHitch` is true and `Cause` is empty.

3. **Chrome/Perfetto trace export through `debug.Profiler.WriteChromeTrace`**:
   - `Editor.ExportProfilerTrace(w)` and `Editor.ExportProfilerTraceFile(path)` replay the editor's recorded frames (`frame`, `update`, `draw`, and every named timing zone polled from `game_perf_zones`) into a deterministic `debug.Profiler` instance and delegate serialization directly to `debug.Profiler.WriteChromeTrace`, keeping one Chrome Trace Event Format emitter in the repository.

4. **Spec-token Play-mode strip and full Profiler panel (`packages/game/editor/profiler_ui.go`)**:
   - `ProfilerStripView` sits directly beneath `ViewportView` inside `editor-center-column` whenever `Playing()` or `ProfilerConnected()` is true, implementing `docs/design/editor/PlayMode.dc.html` using only tokens from `docs/design/editor/Spec.dc.html`: `FRAME TIME · LAST 60 S`, `avg`, `p99`, `allocs/frame`, `8.3 ms` / `16.7 ms` lines and legend, `−60 s` / `−30 s` / `now` axis, spike callout pill, right-edge playhead bar, and a **Profiler** button that opens the full panel.
   - `ProfilerPanelView` docks below the main row when toggled via the strip, toolbar **Profiler** button, `Project > Profiler`, Command Palette, or `mod+shift+m` (`CmdProfiler`). It renders four clickable KPI cards, switchable time-series charts (`Frame`, `FPS`, `Allocs & GC`, `Memory`), a sorted zones table from `game_perf_zones`, **Reset** (`game_perf_reset`), and **Export Trace**.

## Alternatives considered

- **Adding a new `game_perf_trace` MCP tool in `packages/game/mcp` to pull raw trace JSON from the game**: Would capture sub-frame nested call depths directly from the game's `debug.Profiler` ring buffer, but would expand the public MCP tool catalog (requiring updates to `skills/nsgo-game` and remote recording lifecycle management) while issue #912 specifically scopes polling `game_perf_readout` / `game_perf_zones`, calling `game_perf_reset`, and exporting the editor's recorded frames through the `debug` package's exporter. Replaying recorded frames into `debug.Profiler.WriteChromeTrace` satisfies both remote MCP games and in-editor Play sessions without adding a new MCP tool.
- **Re-implementing Chrome trace JSON serialization inside `packages/game/editor`**: Would avoid constructing a temporary `debug.Profiler`, but would duplicate Chrome Trace Event Format encoding and could drift from `debug.Profiler.WriteChromeTrace`. Rejected so `packages/game/debug` remains the single exporter.
- **Using `ui.ColorsOf(cx)` theme colors for the strip and profiler panel**: Would match older pre-spec panels in `editor`, but violates the design acceptance requirement in issue #912 and `docs/design/editor/Spec.dc.html` to use only Spec tokens and verified WCAG 2.2 contrast pairs.

## Consequences

- **Benefits**:
  - Authors see live frame-time spikes and their causes immediately under the viewport in Play mode or while connected to a running game, and can open the full Profiler panel in one click.
  - All profiler UI elements strictly use Spec color, spacing, radius, and focus-ring tokens and pass real macOS `nsgotest` screenshot verification alongside headless tests.
  - Trace exports share the exact Chrome/Perfetto JSON schema tested in `packages/game/debug`.
- **Costs and known limits**:
  - `packages/game/editor` now imports `encoding/json` for MCP RPC payloads (recorded in `notJSONFormats` in `packages/game/formats_test.go`).
  - Exported traces built from polled MCP snapshots represent per-frame zone durations rather than individual intra-frame call timestamps when a zone runs multiple times per frame; a future `game_perf_trace` MCP tool can complement this if sub-frame call-stack granularity is needed over MCP.
  - Because issue #911 (remote scene tree/inspector) has not landed yet, automatic connection when clicking **Run** awaits the shared `-automation` remote session lifecycle; callers currently connect via `Editor.ConnectProfiler` (or automatically in Play mode).

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo test ./debug && env -u GOROOT go tool nsgo test ./debug -- -tags release && env -u GOROOT go tool nsgo test ./mcp && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go tool nsgo test -target macos -run TestProfilerScreenshots ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web ./debug ./mcp ./editor && env -u GOROOT go tool nsgo check -target ios ./debug ./mcp ./editor)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
