# Agent Note: Particle Emitter Editor with Live Preview and Scene Placement

Status: implemented

## Problem

2D particle emitters (`packages/game/particles`) were only authored by hand-editing YAML files and restarting or hot-reloading a running game. There was no visual editor panel to inspect or tune emitter properties, curves, atlas frames, trails, collisions, or sub-emitters with live feedback, no scheduled burst definition in the YAML format, no published JSON Schema (`particles.v1.json`), and no way to place a particle emitter file on an object inside a `sceneformat` scene so both Play mode and the game run it.

## Decision

1. **Scheduled bursts and schema metadata in `packages/game/particles`**:
   - Added `Burst` (`Time`, `Count`, `Cycles`, `Interval`) and `Bursts []Burst` to `Emitter` and `Config` (YAML/JSON key `bursts`, omitted when empty so existing YAML files round-trip byte-for-byte unchanged). `Emitter.Update` advances burst schedules deterministically; `ReloadConfig` updates burst schedules without clearing live particles; `Clear`/`Reset` reset burst timing.
   - Added read-only `Shape` accessors (`Kind()`, `Radius()`, `Width()`, `Height()`) so editors and tools can inspect shapes without reaching into unexported fields.
   - Added `SchemaURL = "https://game.nsgo.dev/schemas/particles.v1.json"` and exported schema reflection types in `packages/game/particles/schema.go` registered in `packages/game/cmd/gameschema` to publish `apps/website/public/schemas/particles.v1.json`.

2. **Version 2 scene placement in `packages/game/sceneformat`, `refs`, and `assetcheck`**:
   - Added `Particles *ParticlesRef` (`{file, atlas}`) to `sceneformat.ObjectDoc` and `ObjectOverride` (gated to scene version 2, matching `model` and `scene`, and added `"Particles"` to `sceneV2Fields` in `cmd/gameschema`).
   - Added optional `ParticlesResolver` in `sceneformat/build.go` with automatic fallback to `FileResolver` + `particles.UnmarshalConfigYAML`, so `FSAssets` works with zero extra boilerplate.
   - Added `BuiltObject.Emitter *particles.Emitter` and `BuiltLayer.Emitters []*BuiltObject`. `BuiltLayer` groups sprites and emitters in Z order so particle emitters interleave cleanly with sprites while preserving `BuiltLayer.Batches` for callers reading sprite batches directly. `BuiltObject.Sync` updates emitter position via `Follow` and rotation; `BuiltScene.Update(dt)` advances all placed emitters; `BuiltScene.Draw(d)` renders them.
   - Wired `ObjectDoc.Particles.File` and `ObjectOverride.Particles.File` into `packages/game/refs` (`FieldParticles = "particles"`), `packages/game/editor/refs.go` (`MoveFile`), and `packages/game/tooling/assetcheck` (validating emitter YAML and atlas `frameNames` including sub-emitters).

3. **Live-preview Particle Emitter Editor panel in `packages/game/editor`**:
   - Added `packages/game/editor/particles.go` and `packages/game/editor/particles_ui.go` (`ParticleEditorPanelView`) docked below the viewport, supporting every `particles.Config` field (including recursive sub-emitter editing via `SetParticleTargetPath`), an interactive `core.Canvas` curve widget (drag/click float stops and color gradient stops), an atlas frame picker, full `Editor.History` Undo/Redo, and deterministic save via `particles.MarshalConfigYAML` (which also hot-reloads matching emitters in the open scene).
   - Every edit syncs the preview emitter via `Emitter.ReloadConfig` without calling `Reset`/`Clear`, so changing rate or curves updates the running preview live without a restart. When no atlas or frames are bound, the preview canvas renders colored fallback discs so particles stay visible while tuning.
   - Added `PlaceParticles`, `SetObjectParticles`, Inspector `PARTICLES` fields, file-browser open/double-click for `*.particles.yaml` / `*.emitter.yaml`, `CmdParticleEditor` in commands/menu bar, and live edit-viewport stepping (`StepSceneEmitters`).

## Alternatives considered

- **Representing placed emitters as an untyped entry in `ObjectDoc.Components`**: Rejected because `refs.Scan`, `assetcheck`, `sceneformat.Build`, JSON schemas (`scene.v2.json`), and `MoveFile` all treat first-class asset references (`sprite`, `material`, `model`, `scene`) as typed fields on `ObjectDoc` / `ObjectOverride`. A typed `ParticlesRef` keeps schema validation strict and lets `sceneformat.Build` instantiate and interleave emitters natively without requiring a custom component registry.
- **Keeping `particles.Emitter.Draw` drawing fallback circles globally when `Atlas == nil`**: Rejected because `particles/particles.go` explicitly documents and tests that `Emitter.Draw` is a safe no-op when `Atlas == nil`. Drawing fallback discs only inside the editor's preview canvas preserves `particles`'s runtime contract while keeping untextured authoring ergonomic.
- **Restarting the preview emitter on every config change**: Rejected because it violates the live-tuning acceptance criterion; `Emitter.ReloadConfig` already preserves live particles, PRNG state, and runtime colliders while re-evaluating curves immediately on live particles.

## Consequences

- Existing version 1 scenes and existing particle YAML files remain 100% compatible and round-trip unchanged.
- `packages/game/sceneformat` now imports `packages/game/particles` (both are `Experimental` in `docs/en/stability.md`, verified by `TestStabilityTable`).
- `apps/website/public/schemas/particles.v1.json` and `apps/website/public/schemas/scene.v2.json` are generated by `cmd/gameschema` and enforced by CI `-check`.
- Both the standalone game (`sceneformat.Build` + `Update` + `Draw`) and the editor's Play mode (`PlayIn`) run placed particle emitters identically.
