### Added

- `game/editor`: add a Problems panel (`ProblemsView`, `CheckProblems`, `Problems`, `OpenProblem`) that runs `assetcheck.CheckWith` (with the editor's component registry) and `project.Check` off the UI goroutine on project open, save and file move, navigates to scenes or selects files on press, and displays the live problem count in the status bar (#913).
