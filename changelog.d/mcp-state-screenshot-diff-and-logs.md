### Added

- `game/pkg/mcp`: added `game_state_capture` and `game_state_diff` tools backed by `statedump.Capture` and `statedump.Diff` with bounded in-memory capture storage, path filtering, changed entity references, and structured field diffs (#1744).
- `game/pkg/mcp`: extended `game_screenshot` with named in-memory `baseline` storage, `compare_to` diffing against baselines or confined project-relative PNGs, per-region 8-connected changed bounding boxes, `gametest`-compatible tolerance checks, and optional visual diff PNG output (#1744).
- `game/pkg/mcp`, `game/pkg/tooling/gamedebug`: added `game_log_read` with a concurrency-safe bounded `slog.Handler` ring buffer (`Config.Log`, `mcp.LogBuffer`), cursor/level/substring filtering, and `gamedebug.RecordPanic` / `gamedebug.Recover` / `gamedebug.RecoverFunc` / `gamedebug.LastPanic` panic reporting (#1744).
