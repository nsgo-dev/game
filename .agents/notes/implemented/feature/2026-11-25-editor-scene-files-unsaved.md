# Agent Note: Editor scene files, unsaved-changes tracking and the prompt before losing edits

Status: implemented

## Problem

The editor opened one scene per process (`go tool game editor scene.yaml`), had no New, Open or Save As, and gave no sign of unsaved edits, so switching scenes or quitting silently lost work (#866). Adding scene switching makes losing edits easy, so the editor needs to know what is unsaved, show it, and ask before anything throws it away, on every desktop platform the editor runs on.

## Decision

- **Dirty means "encodes differently"**: `Editor.sceneSaved` holds the scene's canonical encoding when it was opened or last saved (`New`, `openYAML`, `SaveFile`, `Save` through `OnSave`, `RevertScene`, and `followMove` when a move rewrites the open scene's file). `SceneDirty` re-encodes the scene (the pre-Play snapshot while playing) and compares bytes, as `ProjectDirty` already does for `game.yaml`. `HasUnsavedChanges` is `SceneDirty || ProjectDirty`, and is what the marker, the window title and the prompt use.
- **`ScenePath` is derived, not set by hand**: `OpenFile`, `SaveFile` and `OpenProject` set it from `FilePath` when that lies under the project root and clear it otherwise (`syncScenePath`). `OpenScene`, `NewScene` and `SaveSceneAs` take project-relative paths and refuse anything outside the project or named `game.yaml`/`nsgo.yaml`; they never overwrite a file (`ErrFileExists`).
- **One open path clears `OnSave`**: `OnSave` persists the scene the app handed in, so every way of opening another project scene (`OpenScene`, used by the file browser, New Scene, `OpenProblem`, `OpenSearchResult` and `OpenReference`, and `SaveSceneAs`) clears it; otherwise Save would hand scene B's bytes to scene A's callback.
- **`Save` writes only what is edited**: an unedited project or scene file is left as written, comments included; only a scene whose `FilePath` names no file yet is written while clean.
- **`EditorView` owns the window title** (`WindowTitle`: label, `*`, then `Editor.AppName` or "NSGo Scene Editor"), so apps name themselves with `AppName` instead of calling `SetTitle`.
- **One prompt, three triggers**: `whenSaved(proceed, cancel)` asks through a `ui.AlertDialog` that `EditorView` installs (`askUnsaved`); without an `EditorView` the action is refused with `errUnsaved`. Triggers are closing a dirty scene tab (added in [editor-scene-tabs](2026-11-25-editor-scene-tabs.md), which superseded prompting on scene switches and New Scene because scenes now open in separate tabs), switching projects, `desktop.OnQuitRequest` (deferred answer; a forced quit never asks) and the window's `OnClosing`. Discard on project switch restores project-derived state (audio mixer selection and audition, export panel preset, plugin tool).
- **Window close is hooked separately**: in NSGo v0.3.0 `core.Window.requestClose` routes a close that ends the app through `requestQuit` only on Windows and Linux; on macOS the close button closes the window and the app ends without `OnQuitRequest` (documented on `nsgotest.App.CloseWindow`). `EditorView` therefore cancels `OnClosing` while edits are unsaved and closes the window itself, via `cx.Post(w.Close)`, once the author saved or discarded.
- **Dirty state is computed once per revision**: `unsavedMemo` encodes the scene and project once per editor revision for the marker, Revert's enabled state and the title, and while Play runs (a revision per frame) it keeps the value from Play's first revision, since nothing that is compared can change then.

## Alternatives considered

- **A saved point in `History` (dirty = undo index differs from the index at save)**: cheap and common in editors. Rejected because not every change goes through History: `MoveFile` rewrites references outside the history's view of the file, `OpenProject` clears the history, and a redo past the save point and back would need careful bookkeeping; comparing encodings is exact and matches how `ProjectDirty` already works.
- **Only `OnQuitRequest` for quitting**: covers Windows and Linux, where closing the last window asks it. Rejected because macOS, the main development platform, closes the window without asking it.
- **Separate New, Save As and Revert toolbar buttons**: more discoverable, but at the 1120-point default window they pushed Save off screen, so they live in a File menu.
- **Discard project edits before opening the scene, so `History.Clear`'s `afterMutate` sees the restored project**: also works, but if opening the scene then fails the author has lost project edits for nothing; discarding after a successful open and re-running `afterMutate` keeps the failure case harmless.

## Consequences

- An app that called `SetTitle` before `EditorView` has its title replaced; it sets `Editor.AppName` instead (Field Medic's editor does).
- An app that set `ScenePath` itself after `OpenFile` no longer needs to; a path outside the project now clears it.
- `HasUnsavedChanges` encodes both files; the UI calls it through the memo, but Go callers polling it in a loop pay that cost each call.
- The Open button and double press are offered only for scene names (`*.scene.yaml`, `*.scene.yml`, autoload scenes), as `OpenProblem` decides, so other YAML files are not mistaken for scenes.

## Verification

- `(cd packages/game && go tool nsgo test ./editor)`: `scenefile_test.go` covers the marker and title, Mod+S, Undo back to clean, Revert and its undo, the prompt from the file browser, Problems panel, New Scene, quit (Cancel, Save, Discard, forced) and macOS window close, Discard restoring project-derived audio state, Save leaving an unedited scene as written, and `assetcheck.Check` passing on scenes made by New Scene and Save As.
