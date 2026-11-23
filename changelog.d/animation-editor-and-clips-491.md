### Added

- `game/animclip`: animation clips that key any object field or registered component field (`component.field`), checked against the component registry and played on scene objects through `tween.Timeline` with play, seek, loop and events; `BindScene` keeps built sprites in step and a `Binder` binds a game's own state (#491).
- `game/editor`: an animation panel that keys the selected object's properties at the playhead, edits keys' time, value and interpolation, scrubs and plays a preview posed by the same `animclip` player the game uses, and opens and saves clips as `animation` resource files, with every edit undoable; `go tool game editor` takes a clip file as its second argument (#491).
- `game/resource`: `animation` files take `events`, named cues in time order that a player reports as playback crosses them (#491).
