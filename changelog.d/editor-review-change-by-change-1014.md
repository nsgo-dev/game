### Added

- `game/editor`: full-window change-by-change agent turn review screen (`ReviewScreenView`, `Editor.StartTurnReview`) grouped by file and kind with filter chips, search, seen progress, `ScopeReview` keyboard shortcuts (`J`/`K`, `X`, hold `B`, `⌘↵`, `⌘⌫`), adaptive scene/prefab/data-table previews, Why + line-numbered YAML diff, `Esc`/Resume pending state with viewport overlays and Agent panel card, Revert all confirmation dialog, and single-step `History` Undo (#1014).
- `game/editor/editormcp`: read-only `review_status` MCP tool reporting seen and reverted changes, active blockers, and turn review outcomes (#1014).
