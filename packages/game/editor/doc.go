// Package editor implements the NSGo Scene Editor over packages/game/sceneformat.
//
// It provides:
//   - Command-pattern undo/redo history (History, Command) covering every scene edit.
//   - Scene document loading, saving, object placement, selection, transform editing,
//     and deletion (Editor, PlaceObject, MoveObject, RotateObject, ScaleObject, DeleteObject).
//   - Inline tilemap editing with paint, erase, and BFS flood fill (PaintTile, EraseTile, FloodFillTile).
//   - Typed property and component inspection and editing (SetProperty, SetComponentField, InspectorView).
//   - Play-in-editor preview powered by game.Loop with exact pre-play YAML snapshot restoration (Play, PlayIn, Stop).
//   - Atlas asset browsing and drop-to-place sprite creation (Assets, DropAtlasFrame, AssetBrowserView).
//   - A native NSGo UI root view (EditorView) combining toolbar, asset browser, interactive canvas viewport,
//     and property inspector.
package editor
