### Added

- `game/refs`: references between a project's files that survive renames and moves. `Scan` finds every path `game.yaml`, scenes, resource files and Tiled and LDtk files write to another file, with its file and line; the project index `game.index.yaml` records each scene and resource file's `uid` and each other referenced file's SHA-256, so `Project.Moved` finds a file moved without the tools; `PlanMove` and `PlanIndex` plan the in-place rewrites, keeping each file's comments and formatting, and `Plan.Apply` makes them (#486).
- `game/resource` and `game/sceneformat`: an optional `uid` field in the header of every resource file and scene, a stable ID a file keeps through renames and moves, made by `resource.NewUID` and checked by `resource.ValidUID`; `sceneformat.Resolve` keeps the scene's own uid (#486).
- `game mv` moves or renames a file or directory and rewrites every reference to it; `game index` gives scene and resource files a uid, points references at files moved without the tools and writes the project index; `editor.Editor.MoveFile` moves a file from the editor (#486).

### Changed

- `game check` (`tooling/assetcheck`) also fails on a missing scene that a scene inherits or instances or that `game.yaml` autoloads, on a missing autoloaded resource, on a project index that does not parse and on a uid two files share, and a reference to a file that is gone says where the file went when the project index finds it. A directory holding `game.yaml` is now a project root too (#486).
