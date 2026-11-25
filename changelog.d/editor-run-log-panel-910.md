### Added

- `game/editor`: add a Run / Stop Game toolbar command (and `F5` / `Shift+F5` shortcuts) that launches `go tool game dev` in the open project directory on desktop—optionally starting at the currently open scene—and cleanly terminates its entire process tree on Stop, `OpenProject`, `Close`, or view unmount, leaving in-editor Play preview untouched (#910).
- `game/editor`: add a docked Output / Log panel (`OutputPanelView`) below the viewport that streams the running game's stdout and stderr live off the UI goroutine, preserves a timestamped history of editor status and error messages, supports Clear and Errors-only filtering, and opens clicked `file:line` scene lines (or selects non-scene files in the file browser) (#910).
- `game/cmd/game`, `game/tooling/hotreload`: add `-scene` to `game dev` and `hotreload.SceneEnv` / `hotreload.DevScene()` so a game launched under `game dev` or from the editor can start at a chosen scene (#910).
