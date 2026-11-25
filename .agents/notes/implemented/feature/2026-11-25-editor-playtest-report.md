# Agent Note: Playtest Report screen in the editor

Status: implemented

## Problem

After [playtest.yaml headless runner](2026-11-25-playtest-yaml-headless.md) (#1043) and [playtest bot profiles](2026-11-25-playtest-bot-profiles.md) (#1008) produced deterministic `playtest.report.yaml` files and per-run `.replay.yaml` recordings, authors still had no visual editor screen matching `docs/design/editor/Playtest.dc.html` to inspect goals met or missed with their source files, read spatial/series/matrix charts, review pending `playtest_define` metric proposals, send tuning requests to the agent, or jump directly into Play mode to watch a failing run's saved recording.

## Decision

Add a dedicated full-window **Playtest Report** screen (`PlaytestReportView`) and session controller (`packages/game/editor/playtest_report.go`, `playtest_report_ui.go`) integrated with Play-mode replay playback (`packages/game/editor/play.go`), editor menus/commands/file browser (`commands.go`, `menubar.go`, `files.go`, `ui.go`), and `editormcp` (`packages/game/editor/editormcp/playtest.go`).

## Architecture and contracts

- **Single chart selected automatically from report data**: `SelectPlaytestChart` picks one of four `core.Canvas` visualizations (`PlaytestChartHeatMap`, `PlaytestChartBars`, `PlaytestChartLine`, `PlaytestChartMatrix`) based on the report's populated fields: `Matrices` selects the win-rate matrix (flagging cells above `FlagAbove`, defaulting to 60%), `HeatMaps` selects the spatial level heat map (deaths or time per tile), `Series` with `TargetMax` selects wave bars against a dashed goal line, and per-level `Series` without `TargetMax` (such as `shortest_solution_per_level` or `puzzle-solver` curves) selects the line-per-level chart. Every chart renderer uses exclusively `docs/design/editor/Spec.dc.html` tokens and non-color-only cues (icons, labels, exclamation suffixes, dashed target lines, cluster callouts, and cell notes).
- **Spec-strict contrast and compact toolbar**: `PlaytestReportView` uses `#101216` text on `#3B7BFF` primary accent buttons (`4.88:1` AA per `Spec.dc.html`), never `#FFFFFF` on accent. Entry points live in the Project menu (`CmdPlaytestReport`, `mod+shift+y`), file browser (`*.report.yaml`), and a conditional status-bar badge rather than `editor-toolbar`, keeping the 2D toolbar within two rows at `1120 × 680`.
- **Watch run N opens recordings in Play mode**: `WatchPlaytestRun` / `WatchPlaytestRunIn` resolves the run's `Recording` path safely within the project or report base directory (refusing path traversal and escaping symlinks), validates it with `replay.LoadFile`, and launches `PlayReplayIn` so `PlayIn` advances a live `*replay.Player` on every fixed update step (invoking optional `Editor.OnPlayReplayStep`) while showing a compact replay banner with **Back to Playtest**.
- **Bidirectional `editormcp` sync**: `game_playtest` automatically loads freshly written reports into the editor via `LoadPlaytestReport`; `playtest_define` registers its pending review on the editor via `AddPlaytestProposal` with `ApplyFunc`/`DismissFunc` delegated to `Server.ApplyPlaytestReview`/`DismissPlaytestReview` so reviewing from the Playtest Report banner keeps disk and MCP server state identical; and `playtest_report` lets agents open, inspect, update `Agent's read` and suggestions, or trigger `WatchPlaytestRun`.

## Alternatives considered

### Add a Playtest toggle button directly to `editor-toolbar`

Putting a Playtest button in `editor-toolbar` alongside HUD and Profiler would make it visible at all times. This was rejected because adding another button to the 2D toolbar risks wrapping it past two rows at `1120 × 680`, breaking existing viewport-height layout invariants (`TestOpenFromTheFileBrowserAsksFirst`, `TestDiscardFollowsTheProjectBack`, `TestLightingHandlesAlignOnParallaxLayer`); the Project menu, Command Palette (`mod+shift+y`), file browser (`*.report.yaml`), and conditional status-bar pill provide direct access without growing the toolbar.

### Render the win-rate matrix and bar chart as nested UI flex/grid views instead of `core.Canvas`

Building bars and matrices out of nested `core.View` boxes would mirror `Playtest.dc.html`'s DOM structure. This was rejected because `core.Canvas` provides uniform deterministic rendering and pixel-accurate geometry across all four chart kinds (heat-map radial clusters, dashed goal lines, multi-point level curves with area fill, and centered matchup matrices) and allows direct `core.Drawing` unit verification in headless tests as well as crisp native macOS Metal rendering.

### Add a separate chart-type enum field to `playtest.Report` YAML

Adding an explicit `chartKind` field to `playtest.Report` would make chart selection trivial. This was rejected because existing `playtest.report.yaml` files and `playtest.ParseReport`'s `KnownFields(true)` schema already carry `heatMaps`, `series` (with `targetMax`), and `matrices`; inferring the chart kind directly from those existing structures keeps the on-disk YAML schema untouched and works with every existing report fixture.

## Consequences

- **Benefits**: Authors can inspect playtest goals met/missed with source files, visualize all four chart kinds, review and apply `playtest_define` proposals safely from a banner drawer, trigger tuning/suggestion reviews, and jump straight into Play mode to watch a run's `.replay.yaml` recording.
- **Costs**: `PlaytestReportView` is another full-window mode guarded alongside `StartScreenView`, `MergeConflictView`, and `HUDLayoutView` in `commands.go` and `ui.go`, and `Editor.PlayIn` now manages optional replay playback state cleared on `Stop`.

## Verification

Verified with `env -u GOROOT go tool nsgo check` (desktop and `-target ios`) on `./editor`, `./editor/editorcmd`, and `./editor/editormcp`, `TMPDIR=/tmp env -u GOROOT go tool nsgo test` on `./editor`, `./editor/editorcmd`, `./editor/editormcp`, and `.` (`TestFormatsPage|TestStabilityTable|TestAgentSkillValid`), real native macOS screenshots via `go tool nsgo test -target macos -screenshots ... ./editor/editorcmd -- -run TestPlaytestReportMacOSScreenshots` visually inspected beside `Playtest.dc.html` and `Spec.dc.html`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, `gofmt`, and `bun run verify-notes`.
