### Added

- `game/editor`: editor plugins, as Godot's `EditorPlugin`: Go code built into the editor binary (`Editor.RegisterPlugin`) adds panels docked left, right or below the viewport, an inspector that draws its own component types in place of the generic fields, and 2D viewport tools (`ToolPlugin`, `SetPluginTool`), with every edit going through the undo history; `SetPluginEnabled`, `ActivePlugins` and `MissingPlugins` turn plugins on and report them, `ComponentField` reads a component field with its registered default, and `Report` shows a plugin's refused edit in the status bar (#492).
- `game/project`: an `editor` section in `game.yaml` whose `plugins` list turns editor plugins on for the project (#492).
