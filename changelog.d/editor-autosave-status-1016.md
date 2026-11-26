### Added

- `game/editor`: autosave kept scene and project edits after 2 s (`General › Save automatically`, `EditorSettings.AutoSave`), pausing while any review is open (#1016).
- `game/editor`: status bar save indicator with `Saved` (hover time tooltip), 300 ms anti-flicker `Saving…`, persistent failed-save alert with `Retry` and `Save as…`, and problem-red unsaved dot while saving fails (#1016).
- `game/editor/editormcp`: append a save-failure warning to tool results while edits are not on disk until saving succeeds (#1016).
