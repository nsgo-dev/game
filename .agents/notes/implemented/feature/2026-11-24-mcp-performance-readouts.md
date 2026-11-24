# Agent Note: MCP performance readouts

Status: implemented

## Problem

Coding agents driving or inspecting a running NSGo game over `nsgo mcp` (see [MCP scene, layer, entity and component inspection](2026-11-24-mcp-scene-entity-inspection.md)) can query scenes, layers, entities and components, but cannot observe live frame timing, update and draw costs, draw statistics, per-frame heap allocations, GC pauses, or named profiler zones. Without structured readouts backed by the same counters as `game/debug`'s on-screen overlay, agents cannot verify frame-budget compliance or diagnose spikes and allocation regressions programmatically.

## Decision

Add a dedicated `perf` tool group (`packages/game/mcp/perf.go`) exposing `game_perf_readout`, `game_perf_zones`, and `game_perf_reset` via `Bridge.Tools()`, reading directly from `debug.Overlay`, `debug.Profiler`, `debug.GCMonitor`, `debug.MemoryTracker` and `debug.DrawStats`, plus 3D statistics a game supplies as `mcp.PerfRender3DStats`. Extend `packages/game/debug` minimally so `GCMonitor` also samples per-frame heap allocations with zero steady-state allocations, `Overlay` supports optional `DrawStats`, and `Overlay`/`Profiler`/`GCMonitor` expose clean reset helpers shared by both the HUD and MCP.

## Architecture and contract

- **Single source of truth with `debug.Overlay`**: `game_perf_readout` reads `FPS()`, `Average()`, `Latest()`, `Samples()`, `DrawStats()`, `Profiler()`, `GC()`, and `Memory()` directly from `Config.Overlay` (using the same `float64(d) / float64(time.Millisecond)` conversion as `debug/overlay.go`). Values returned by MCP match the on-screen debug overlay bit-for-bit, and `game_perf_reset` refreshes the visible overlay HUD via `Overlay.ResetStats()`.
- **Zero-alloc per-frame allocation tracking in `debug.GCMonitor`**: `GCMonitor` extends its pre-allocated `runtime/metrics` sample buffer from 3 to 5 entries to read `/gc/heap/allocs:objects` and `/gc/heap/allocs:bytes` alongside `/gc/pauses:seconds`, `/gc/cycles/total:gc-cycles`, and `/memory/classes/heap/objects:bytes`. Running window totals (`recentAllocs`, `recentAllocBytes`) are updated in O(1) as the ring buffer advances, preserving `debug.AssertZeroAllocs` in steady state while exposing `AllocsPerFrame()` and `AllocBytesPerFrame()` to both the overlay and MCP.
- **2D and 3D draw statistics**: `debug.DrawStats` (`DrawCalls`, `Sprites`, `Batches`, `Triangles`, `Vertices`) can be supplied via `debug.Options.DrawStats`, pushed with `Overlay.SetDrawStats`, or overridden via `mcp.Config.DrawStatsFunc`. Optional `mcp.Config.RenderStatsFunc` attaches full 3D telemetry under `render3d` as `mcp.PerfRender3DStats`, a struct with the same fields as `render3d.RenderStats` so a game converts with `mcp.PerfRender3DStats(stats)` while `game/mcp` never imports `game/render3d` (which would link `nsgo/gpu`, `scene3d`, `particles` and `ktx2` into every 2D game that attaches the bridge), and populates top-level `draw_calls`, `batches`, `triangles`, and `vertices` automatically when no 2D draw-stats source is configured.
- **Strict schemas and graceful degradation**: Every perf tool decodes arguments with `json.Decoder.DisallowUnknownFields()` and rejects negative limits. Calling `game_perf_readout` or `game_perf_reset` with no configured perf telemetry returns an explicit error rather than misleading zeros; configuring any subset of providers succeeds and reports active providers in `sources`. Calling `game_perf_zones` without a profiler returns an explicit error, while `game_perf_readout` with `include_zones: true` degrades cleanly when only an overlay is configured.
- **Release-build parity**: Every new method added to `packages/game/debug` (`DrawStats`, `SetDrawStats`, `HasDrawStats`, `ResetStats`, `Profiler.Reset`, `GCMonitor.AllocsPerFrame`, `GCMonitor.AllocBytesPerFrame`, `GCMonitor.Reset`) has a matching zero-cost no-op stub in the `//go:build release` files so `-tags release` builds remain unaffected.
- **Minimal merge footprint**: All tool schemas, response structs, and handlers live in `packages/game/mcp/perf.go`, with only additive `Config` fields and `b.perfTools()` hookup in `packages/game/mcp/mcp.go` so it conflicts with the other MCP tool groups (#462–#469) only where each appends to `Config`, `Tools()` and the tool lists.

## Alternatives considered

### Sampling `runtime.ReadMemStats` inside `mcp` on every `game_perf_readout` call

Reading `runtime.ReadMemStats` directly in `mcp` would avoid extending `debug.GCMonitor` and would report heap/allocation numbers even when `Config.GC` is nil. This was rejected because `runtime.ReadMemStats` triggers a stop-the-world pause (distorting the very frame times being measured), produces ad-hoc inter-call deltas rather than per-frame averages, and would diverge from the numbers displayed in `debug.Overlay`.

### Keeping `DrawStats` exclusively inside `packages/game/mcp`

Defining `DrawStats` only inside `mcp` would avoid touching `packages/game/debug`. This was rejected because issue #468 requires MCP performance readouts to match the debug overlay's values; placing `DrawStats` on `debug.Overlay` lets games report draw counts once and have both the on-screen HUD and `game_perf_readout` reflect identical numbers.

### Returning all-zero readouts when no performance providers are configured

Returning a zeroed `PerfReadoutResult` on an empty `mcp.Config{}` avoids errors in unconfigured games. This was rejected because zero `fps`, zero `draw_calls`, and zero `allocs_per_frame` are indistinguishable from a paused or idle game; returning an explicit error when no provider is configured matches `game_entities_list` and `game_layers_list` in `inspect.go`.

## Consequences

- **Benefits**: Agents can inspect live FPS, average/latest/percentile frame timings, update/draw splits, 2D and 3D draw counts, per-frame heap allocations, GC pauses/hitches, asset memory budgets, chronological frame history, and profiler zones with guaranteed parity against `debug.Overlay`. Resetting metrics via MCP keeps the visible HUD synchronized.
- **Costs**: `packages/game/debug` gains a small additive API surface (`DrawStats`, `GCMonitor` allocation averages, and `Reset` methods) that must remain mirrored in its `release` build stubs. `packages/game/mcp` now imports `packages/game/debug` (both are Experimental packages, so Stable packages remain unaffected); it does not import `packages/game/render3d`.

## Verification

Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./mcp`, `go tool nsgo test ./debug`, `go tool nsgo test -tags release ./debug`, `go tool nsgo test -run TestStabilityTable .`, `go run ./tools/changelog check`, and `bun run verify-notes`.
