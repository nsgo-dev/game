### Added

- `game/editor`: add the full-workspace cutscene timeline editor (`CutscenePanelView`) with left-rail cutscenes and localized `loc` lines, live `cutscene.TimelinePlayer` scrubbing with a 24-frame thumbnail strip, track lanes with ±0.1 s Start/Duration and Easing inspection, and an interactive agent review mode (Before/After, hold `B` to peek, dashed/solid change markers, per-change Revert/Restore, review-joined manual edits with `⌘Z` undo, Revert all, and single-undo-step Keep N of M) (#1056).
- `game/editor/editormcp`: add `cutscene_write` (opening pending cutscene reviews without overwriting disk until kept) and `cutscene_scrub` MCP tools (#1056).
