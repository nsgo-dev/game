### Added

- `game/editor`: add a live Profiler panel (`ProfilerPanelView`) and Play-mode `FRAME TIME · LAST 60 S` strip (`ProfilerStripView`) built with Spec tokens that connect to a running game over MCP (`ConnectProfiler`, `ConnectProfilerMCP`, `ConnectProfilerCommand`, `ConnectProfilerScope`), poll `game_perf_readout` and `game_perf_zones`, chart frame/update/draw time, FPS, allocations/GC, and asset memory with labelled spike causes, reset via `game_perf_reset`, and export recorded frames as Chrome/Perfetto trace JSON through `debug.Profiler.WriteChromeTrace` (#912).
- `game/debug`, `game/mcp`: add `FrameSample.Cause`, `Overlay.NoteFrameCause`, and `PerfFrameSample.Cause` so a game can explain known frame-time spikes in `game_perf_readout` history (#912).
