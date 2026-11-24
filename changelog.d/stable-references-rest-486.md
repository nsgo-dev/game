### Added

- `game/editor`: a file browser below the asset browser (`FilesView`) lists the open project's files (`Editor.ProjectFiles`) and renames or moves the selected one with its Move button through `Editor.MoveFile`, so every reference to it follows (#486).
- Cave Run carries a project index (`game.index.yaml`), and a `game/refs` test fails when `game index` would change anything in the examples or the starter template (#486).

### Changed

- `game/editor`: `Editor.MoveFile` is now one undoable step instead of emptying the undo history: Undo moves the files back and rewrites the references again, removing the directories the move created when they are left empty, and Redo moves them once more. A step that something on disk now blocks stays where it was, and the status bar says why (#486).
