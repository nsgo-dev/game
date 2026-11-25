# Agent Note: 2D skeletal rig editor and built-in scene skeletal component

Status: implemented

## Problem

2D skeletal rigs (`.skel.yaml`, `packages/game/skeletal`) previously had to be written by hand or exported by external scripts, with no visual rig view in `packages/game/editor` to inspect bones and attachments, drag bone or attachment-origin handles, key bone/slot animation timelines, preview runtime playback and crossfade blending, or place a rig with a chosen skin and animation inside a `.scene.yaml` scene (#886).

## Decision

Split the capability cleanly across `sceneformat`, `refs`, and `editor` while driving every preview directly through the runtime `packages/game/skeletal` package:

- **Built-in `skeletal` scene component (`packages/game/sceneformat/skeletal.go`, `build.go`, `packages/game/refs/scan.go`)**:
  - Define a versioned built-in component `skeletal` (`SkeletalComponentVersion = 1`) with fields `version`, `rig`, `atlas`, `skin`, `animation`, and `loop`, plus an optional `SkeletonResolver` interface (falling back to `FileResolver` + `skeletal.Load`, matching how `TilemapResolver` / `FileResolver` work).
  - `sceneformat.Build` constructs a live `*skeletal.Skeleton` on `BuiltObject.Skeleton` with the chosen atlas, skin, and initial animation (using the animation's own `Loop` flag when `loop` is omitted), syncs its root transform, flips, alpha, and `Z` in `BuiltObject.Sync()`, interleaves sprites and skeletons by `Z` in `BuiltLayer.DrawObjects`, and advances all placed skeletons in `BuiltScene.Update(dt)`.
  - `refs.Scan` tracks `components.skeletal.rig` (`refs.FieldSkeletalRig`) so `MoveFile` rewrites rig paths across scenes and overrides automatically.
- **2D skeletal rig editor (`packages/game/editor/rig.go`, `rig_handles.go`, `rig_ui.go`)**:
  - Open `.skel.yaml` / `.skel.yml` files from `FilesView` (`OpenRigProjectFile`, `OpenRigFile`, `OpenRigYAML`) or create new rigs (`NewRig`) in `RigPanelView` below the viewport.
  - Retain the original file bytes (`raw`) and a `dirty` flag inside undoable `rigState` snapshots so opening and saving an untouched rig—or undoing every edit back to the initial state—returns the exact original bytes byte-for-byte; edited rigs encode deterministically via `skeletal.Encode` after validating with `SkeletonData.Compile` and `skeletal.Encode`.
  - Provide interactive canvas handles (`RigHandleBoneMove`, `RigHandleBoneRotate`, `RigHandleBoneLength`, `RigHandleAttachmentOrigin`) that edit in place during drags and commit one undoable `Editor.History` command on `EndRigDrag` (or restore the snapshot on `CancelRigDrag`). In Bind Pose, bone move/rotate handles edit bind-pose `BoneData`; when an animation is selected, they key `Translate`/`Rotate` offsets at the active playhead.
  - Drive preview rendering (`DrawRigViewport`), scrubbing (`SeekRig`), playback (`PlayRig`/`StepRig`), and crossfading (`CrossfadeRig`) directly through a live runtime `*skeletal.Skeleton` so editor preview matches game playback identically.
  - Expose `PlaceRig` and `PlaceOpenRig` (plus the panel's **Place in Scene** button) to place a rig with its chosen atlas, skin, and animation into the active scene layer, and register `SkeletalComponent` in the inspector's built-in fallback registry and `editorcmd`'s registry wrapper.

## Alternatives considered

### Adding dedicated `rig`/`skin`/`animation` fields to `sceneformat.ObjectDoc`

Adding top-level fields on `ObjectDoc` and `ObjectOverride` would require bumping `sceneformat.CurrentVersion` from 3 to 4, publishing a new `scene.v4.json`, updating all generated-scene version tests, and duplicating validation across `ObjectDoc` and `ObjectOverride`. Defining a built-in versioned `skeletal` component mirrors `sceneformat/physics.go` (#817): it works across scene versions 1–3, inherits and overrides cleanly, requires no scene schema version bump, and integrates immediately with `InspectorView`.

### Writing a separate editor-side FK/animation evaluator for rig preview

Evaluating bone transforms and crossfade interpolation in `packages/game/editor` would risk subtle drift in shortest-angle wrapping, Bezier evaluation, or skin fallback rules. Instantiating and stepping a live `*skeletal.Skeleton` guarantees bit-for-bit behavioral parity between the rig editor preview and `skeletal.Load` playback in games.

### Always re-encoding on `SaveRigYAML` without keeping original `raw` bytes

Canonical `.skel.yaml` files already round-trip byte-for-byte through `skeletal.Encode(skeletal.Load(data))`, but hand-edited `.skel.yaml` files with comments or custom spacing would be rewritten on a no-op save. Retaining `raw` while `!dirty` (and restoring `raw` when edits are undone) guarantees byte-for-byte identity for both canonical and hand-formatted rigs on open-and-save while still writing canonical `skeletal.Encode` output after edits.

## Consequences

- **Benefits**: Authors can build, adjust, skin, keyframe, crossfade-preview, and place 2D skeletal rigs visually in the editor with full Undo/Redo and deterministic plain-text YAML output, and scenes play placed rigs automatically via `BuiltScene.Update` and `BuiltScene.Draw`.
- **Costs**: `swapRigCmd` snapshots the cloned `*skeletal.SkeletonData` per edit on `Editor.History`, trading small per-command memory for exact undo/redo (including `raw`/`dirty` restoration) across structural bone/slot/skin/timeline edits.
- **Verification**: Covered by `packages/game/sceneformat/skeletal_test.go` and `packages/game/editor/rig_test.go` (proving runtime parity across playback and crossfade, byte-for-byte open/save on both canonical and commented rigs, handle drags/cancels, reparenting, scene placement, `refs.Scan`/`MoveFile`, and `nsgotest` UI flows), plus `formats_test.go`, `gameschema -check`, `changelog check`, `verify-notes`, and multi-target `nsgo check`.
