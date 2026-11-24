// Package editor implements the NSGo Scene Editor over packages/game/sceneformat.
//
// It provides:
//   - Command-pattern undo/redo history (History, Command) covering every scene edit.
//   - Scene document loading, saving, object placement, selection, transform editing,
//     and deletion (Editor, PlaceObject, MoveObject, RotateObject, ScaleObject, DeleteObject).
//   - Inline tilemap editing with paint, erase, and BFS flood fill (PaintTile, EraseTile, FloodFillTile).
//   - Typed property and component inspection and editing (SetProperty, SetComponentField, InspectorView).
//   - Play-in-editor preview powered by game.Loop with exact pre-play YAML snapshot restoration (Play, PlayIn, Stop).
//   - An animation editor that keys any registered property on a timeline, previews the clip with animclip and saves it as an animation resource file (NewClip, KeyProperty, SeekClip, PlayClip, SaveClipFile, AnimationPanelView).
//   - Atlas asset browsing and drop-to-place sprite creation (Assets, DropAtlasFrame, AssetBrowserView).
//   - Editor plugins built into the editor binary and turned on by the project's game.yaml, which add docked panels, inspectors for their component types and viewport tools (Plugin, RegisterPlugin, SetPluginEnabled).
//   - Moving or renaming a project's files with every reference to them rewritten, on disk and in the open scene and project (MoveFile, through package refs).
//   - A native NSGo UI root view (EditorView) combining toolbar, asset browser, interactive canvas viewport, property inspector and the plugins' panels.
package editor
