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
//   - A Data Table editor that opens versioned datatable YAML files from the file browser as a sortable, filterable grid with inferred value kinds, undo/redo, lossless round-trip saves and optional per-pattern Go row-type validation (OpenDataTableFile, RegisterDataTableType, SetDataTableCellText, DataTablePanelView).
//   - Atlas asset browsing and drop-to-place sprite creation (Assets, DropAtlasFrame, AssetBrowserView).
//   - Editor plugins built into the editor binary and turned on by the project's game.yaml, which add docked panels, inspectors for their component types and viewport tools (Plugin, RegisterPlugin, SetPluginEnabled).
//   - A file browser that lists the open project's files and moves or renames them with every reference to them rewritten, on disk and in the open scene and project, as one undoable step (ProjectFiles, MoveFile through package refs, FilesView).
//   - A Project Settings panel over game.yaml editing general metadata, window, display, autoload scenes/resources and main scene, and editor plugins through EditProject with inline validation errors, canonical-save notice, undo/redo, and synced nsgo.yaml saving (ProjectSettingsPanelView, SetProjectSettingsOpen).
//   - An audio bus mixer over game.yaml's audio section with undoable bus and effect-chain editing and live audition through sound.Mixer (AudioMixerPanelView, AddAudioBus, AddAudioBusEffect, AuditionAudioBus).
//   - A Problems panel that runs assetcheck.CheckWith (with the editor's component registry) and project.Check over the open project off the UI goroutine on open, save and file move, shows one row per problem with file, line and message, navigates to scenes or selects files on press, and displays the live problem count in the status bar (CheckProblems, Problems, OpenProblem, ProblemsView).
//   - A native NSGo UI root view (EditorView) combining toolbar, asset browser, file browser, interactive canvas viewport, property inspector, problems panel and the plugins' panels.
package editor
