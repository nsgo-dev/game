### Added

- `game/mcp`: add `game_perf_readout`, `game_perf_zones`, and `game_perf_reset` MCP tools exposing frame timings, percentiles, draw statistics, per-frame allocations, GC/heap telemetry, asset memory, and 3D render statistics matching the debug overlay (#468).
- `game/debug`: track per-frame heap allocations (`/gc/heap/allocs:objects` and `/gc/heap/allocs:bytes`) in `GCMonitor`, add `DrawStats` support to `Overlay`, and add `Reset` methods on `Profiler`, `GCMonitor`, and `Overlay.ResetStats` (#468).
