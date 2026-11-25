# Agent Note: Scene Physics Bodies, Colliders, Triggers, and Named Layers

Status: implemented

## Problem

Authors previously had to wire up `physics.World`, `physics.RigidWorld`, and `physics3d.World` bodies, collider shapes, triggers, and raw numeric collision bitmasks manually in Go code, duplicating object transforms already declared in `*.scene.yaml` files (#817). Projects also had no canonical `game.yaml` place to configure 2D/3D gravity, solver iterations, or human-readable collision layer names, `physics3d.Body` lacked a `PhysicsMaterial` surface object matching 2D `physics.RigidBody`, and playing a scene in `game/editor` did not simulate physics or draw collider/trigger outlines.

## Decision

Split the feature cleanly along package stability and import-direction boundaries so `game/physics` (Stable) remains untouched and `game/sceneformat` never imports `game/project`:

- **`physics3d` & `resource`**: Add `physics3d.PhysicsMaterial` (`Friction`, `Restitution`, `LinearDamping`, `AngularDamping`) and `Body.Material` / `Body.ApplyMaterial` in `packages/game/physics3d/material.go`, routing contact resolution (`world.go`), area damping (`area.go`), and XPBD coupling (`xpbd.go`) through clamped effective accessors (`effectiveFriction`, `effectiveRestitution`, `effectiveLinearDamping`, `effectiveAngularDamping`). Extend `packages/game/resource` with `(*PhysicsMaterial).ApplyTo3D(*physics3d.Body)` and `(*Library).PhysicsMaterial(path)` implementing `sceneformat.PhysicsMaterialResolver`.
- **`project` (`game.yaml` `physics:` section)**: Add `Physics`, `Vec2`, `Vec3`, and `PhysicsLayer` in `packages/game/project/physics.go` with strict validation (`validatePhysics` refuses empty/whitespace/duplicate layer names, bits outside `[0, 31]`, duplicate bits, negative solver iterations, and non-finite gravity), regenerate `apps/website/public/schemas/game.v1.json`, wire `LoadAutoloads` to validate autoloaded scenes against `f.PhysicsLayerTable()`, and provide runtime helpers (`PhysicsLayerTable`, `PhysicsSettings`, `ConfigurePhysicsWorld`, `ConfigureRigidWorld`, `ConfigurePhysics3DWorld`, `LayerBit`, `LayerMask2D`, `LayerMask3D`).
- **`sceneformat` built-in physics components & builder**:
  - Extend `ComponentRegistry` (`components.go`, `resolve.go`) with `FieldStringList` (`[]string`) and `Field.Enum` while preserving zero-reflection validation and lossless YAML round-tripping.
  - Define six versioned built-in components (`PhysicsComponentVersion = 1`) in `packages/game/sceneformat/physics.go`: `body2d`, `collider2d`, `trigger2d`, `body3d`, `collider3d`, `trigger3d`, registered via `RegisterPhysicsComponents` / `PhysicsComponentRegistry`.
  - Add `ParseOptions.PhysicsLayers *PhysicsLayerTable` (`format.go`) and AST-backed `checkPhysicsLayerNodes` so misspelled or unknown layer names in `layer` or `mask` (including nested `children` and `overrides`) fail at parse time with a `*loaderr.Error` carrying exact file path, 1-based line, column, field path, and allowed layer names.
  - Add `BuildPhysics(scene, settings, resolver)` returning `*PhysicsScene` owning `World2D`, `Rigid2D`, and `World3D`. `Step(dt)` steps 2D kinematic bodies (X/Y axis-separated sweep against solid bodies), steps `Rigid2D`, mirrors dynamic poses into `World2D.Bodies`, evaluates `World2D.StepTriggers`, steps 3D kinematic bodies and `World3D`, syncs world transforms back to `BuiltObject.Doc`, and dispatches `TriggerEnter`/`TriggerStay`/`TriggerExit` to `OnTrigger` and connected `Signals` (`entered`/`stayed`/`exited`).
- **`editor` integration**:
  - Keep `Editor.ComponentRegistry()` defaulting to `nil` (preserving existing test invariants) while falling back to built-in physics component schemas in `lookupComponentType` (`inspector.go`), and add `ui.Select[string]` for `Enum` fields and comma-separated `ui.Input` for `FieldStringList`.
  - Add `ShowPhysicsDebug` (default `true`) and 2D/3D wireframe debug drawing (`physics_debug.go`, wired into `viewport.go` and `viewport3d.go`).
  - Build and step `PhysicsScene` automatically inside `Editor.PlayIn` (`play.go`), exposing `PlayPhysics()`, `PlayTriggerEvents()`, and `OnPlayTrigger`, and validate named physics layers on `OpenProject` (`project.go`).
  - Add the undoable `ProjectSectionPhysics` tab in Project Settings (`project_settings_physics.go`).

## Alternatives considered

### Putting `project.Physics` types directly in `sceneformat` or having `sceneformat` import `project`

Having `sceneformat` import `project` to read `game.yaml`'s layer table directly would create an import cycle because `packages/game/project` already imports `packages/game/sceneformat` for `LoadAutoloads` and `PathRefs`. Defining lightweight `PhysicsLayerTable` and `PhysicsSettings` in `sceneformat` and converting from `project.File` in `packages/game/project/physics.go` keeps the dependency graph acyclic and allows callers without a `game.yaml` file to construct a layer table in tests or tools.

### Adding `body`/`collider`/`trigger` as dedicated top-level fields on `ObjectDoc`

Promoting physics definitions to top-level `ObjectDoc` struct fields would require bumping the scene schema version (`v3`), duplicating validation across `ObjectDoc` and `ObjectOverrideDoc`, and breaking the existing `components` extension model. Defining them as versioned built-in components (`body2d`/`collider2d`/`trigger2d` and `body3d`/`collider3d`/`trigger3d`) works seamlessly in both `version: 1` and `version: 2` scenes (including `inherits`, `scene` instancing, `overrides`, and `connections`), round-trips losslessly with zero changes to `sceneformat.Encode`, and integrates immediately with the Inspector and `animclip`.

### Pre-populating `Editor.components` with `PhysicsComponentRegistry()` in `editor.New`

Setting `e.components` by default in `editor.New` would cause `ParseWith` to reject free-form custom components in existing scenes and break callers/tests that expect `ed.ComponentRegistry() == nil` until explicitly configured. Keeping `e.components == nil` by default and falling back to built-in physics schemas in `lookupComponentType` gives full typed Inspector support for physics components without constraining unschematized scenes.

## Consequences

- **Benefits**: Scenes with 2D or 3D physics bodies, colliders, triggers, materials, and named collision layers work out of the box in games and in `editor` Play mode without boilerplate Go collision wiring; misspelled layer names fail fast with exact file and line diagnostics; `game.yaml` and `*.scene.yaml` preserve strict validation and byte-for-byte round-tripping.
- **Trade-offs**: Because `game/physics` `RigidWorld` and `World` are separate types, `PhysicsScene.Step` synchronizes 2D rigid/kinematic/static body poses into a mirror `World2D` each tick so `World2D.StepTriggers` observes dynamic, kinematic, and static bodies uniformly.
- **Verification**: Covered by unit and integration tests in `packages/game/physics3d/material_test.go`, `packages/game/resource/physics3d_test.go`, `packages/game/project/physics_test.go`, `packages/game/sceneformat/physics_test.go`, and `packages/game/editor/physics_test.go`, plus `gameschema -check`, `TestFormatPackagesRunConformance`, `TestFormatsPage`, `TestStabilityTable`, and multi-target `nsgo check`.
