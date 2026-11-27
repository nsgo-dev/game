# Agent Note: MCP State/Screenshot Diffing and Bounded Log Capture

Status: implemented

## Problem

Coding agents driving a running game through `game/pkg/mcp` previously had to inspect entities one at a time, visually eyeball separate PNG screenshots, and could not read game logs or recovered panics over MCP. Confirming whether an edit changed only what was intended required many tool calls and tokens and missed subtle state or pixel regressions (#1744).

## Decision

`pkg/mcp` exposes three cohesive verification capabilities while keeping runtime dependencies minimal and release builds zero-cost:

1. **Named state captures and diffs (`game_state_capture`, `game_state_diff`)**:
   - `game_state_capture` runs `statedump.Capture` over `World`, `Scenes`, `Loop` counters and `Config.StateValues` (optionally restricted to requested `values` keys) and stores canonical YAML in a bounded FIFO in-memory store (`MaxStateCaptures = 32`, `MaxStateCaptureBytes = 4 MiB`, `MaxStateCaptureTotalBytes = 16 MiB`).
   - `game_state_diff` compares two stored captures (or a stored capture against current live state when `to` is omitted), returning `statedump.Diff` unified YAML lines, `changed_entities` / `added_entities` / `removed_entities` / `modified_entities` refs, and structured field-level diffs. Optional `paths` filters prune both copies before encoding and diffing, clearing orphaned parent links on filtered entity subsets so `statedump.Validate` stays satisfied.

2. **Screenshot baselines and pixel/region diffing (`game_screenshot`)**:
   - `game_screenshot` adds `baseline` (bounded FIFO store: `MaxScreenshotBaselines = 16`, `MaxScreenshotBaselineBytes = 64 MiB` decoded RGBA) and `compare_to` (stored baseline name or relative `.png` read via `os.OpenRoot` under `Config.ProjectDir` or `Config.ScreenshotDir`).
   - Pixel comparison (`pkg/mcp/screenshot_diff.go`) implements the exact pixel and magenta-on-dimmed-grayscale semantics of `gametest.DiffImage` directly inside `pkg/mcp` without importing `pkg/gametest`, adds optional per-channel `tolerance`, `max_mismatched_pixels`, and `max_mismatched_percent` checks, detects 8-connected changed-region bounding boxes via iterative scanline row-run union-find (capped at 32 regions sorted by pixel count descending, then Y/X ascending), and writes optional diff PNGs through `os.OpenRoot(Config.ScreenshotDir)` or returns base64 inline.
   - Images are compared in PNG-decoded color space (matching `gametest.CompareGolden`'s PNG round-trip) so in-memory baselines and on-disk PNGs compare identically.

3. **Bounded `slog.Handler` ring buffer and panic recording (`game_log_read`, `pkg/tooling/gamedebug`)**:
   - `pkg/tooling/gamedebug` provides concurrency-safe `PanicRecord`, `RecordPanic`, `Recover`, `RecoverFunc`, `LastPanic`, and `ClearPanic` in default builds (`panic.go`) and zero-cost stubs in `-tags release` builds (`panic_release.go`).
   - `pkg/mcp` provides `LogBuffer` (a concurrency-safe bounded ring buffer implementing `slog.Handler` with `WithAttrs`, `WithGroup`, and `Tee`) and `Config.Log` (`*LogConfig`). When `Config.Log` is set without `NoDefault` or a pre-created `Buffer`, `Bridge.Attach` wraps `slog.Default()` with a tee handler and `Bridge.Detach` / scope disposal restores the previous default logger if still active.
   - `game_log_read` supports forward cursor pagination (`since > 0` returns the earliest `limit` matching entries and sets `has_more` without skipping unseen records, while reporting `missed` when old lines rolled off), tail reads (`since == 0` returns the most recent `limit` matching entries and sets `truncated`), minimum `level` filtering, case-insensitive `contains` filtering, and always surfaces `last_panic` when recorded.

## Alternatives considered

- **Importing `pkg/gametest` directly from `pkg/mcp`**: Rejected because `pkg/gametest` imports `"testing"` and registers a global `-update-golden` flag on `flag.CommandLine`. Importing it in a non-test library package would pull `testing`'s `-test.*` flags into every game binary that attaches `mcp.Bridge`. Instead, `pkg/mcp/screenshot_diff.go` implements the exact same pixel and diff-image rules with zero test-package dependency, and `pkg/mcp/screenshot_diff_test.go` cross-checks exact pixel equality against `gametest.DiffImage`.
- **Always returning diff PNG base64 on `compare_to`**: Rejected because inline base64 PNG strings bloat agent context windows when an agent only needs `changed_pixels` and `changed_regions` boxes. Diff image generation is opt-in via `diff_path` or `include_diff_image: true`.
- **Requiring `Config.Log` to read `gamedebug.LastPanic()`**: Rejected because returning `{configured: false, entries: [], last_panic: ...}` still gives agents crash diagnostics even in games that only wired `gamedebug.Recover` without `Config.Log`.

## Consequences

- **Benefits**: Agents can run a tight `capture -> act -> step -> capture -> diff` verification loop across state, pixels, and logs with bounded memory overhead, zero `testing` flag pollution in game binaries, and full `-race` safety.
- **Costs and limits**: `pkg/mcp/screenshot_diff.go` maintains its own copy of `gametest.DiffImage`'s dimmed-grayscale/magenta visualization rules (guarded by `TestScreenshotCompareIdenticalAndMovedSprite` in `pkg/mcp/screenshot_diff_test.go`). Auto-wrapping `slog.Default()` is process-global; concurrent tests or multi-bridge setups should pass an isolated `LogConfig{Buffer: mcp.NewLogBuffer(...)}` or `NoDefault: true`.
