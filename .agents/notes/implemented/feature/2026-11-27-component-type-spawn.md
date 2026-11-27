# Agent Note: ComponentType.Spawn ordering and rollback contract

Status: implemented

## Problem

Scene components declared in YAML remained untyped `map[string]any` data when `sceneformat.SpawnEntities` or `sceneformat.SpawnConnected` spawned a scene into an `entities.World`. Every game and plugin that registered a `sceneformat.ComponentType` still had to write its own post-spawn walk over `ObjectDoc` values to resolve defaults and populate typed `entities.Store[T]` components (as Field Medic did with `health.Read`), duplicating glue that a plugin could not ship alongside its component schema.

## Decision

`sceneformat.ComponentType` now carries an optional factory `Spawn func(w *entities.World, e entities.Entity, fields map[string]any) error`, preserved by `ComponentRegistry.Register`, `Lookup`, and `Types`. `sceneformat.SpawnEntitiesWith(doc, w, SpawnOptions{Components: reg})` validates and resolves components before creating any entity, spawns entities and their components in a deterministic order, and rolls back created entities if any `Spawn` fails. `SpawnEntities(doc, w)` delegates directly to `SpawnEntitiesWith(doc, w, SpawnOptions{})`, and `SpawnConnected(doc, w, reg, ctx)` forwards `reg` to `SpawnEntitiesWith`.

### Spawn contract

- **Pre-spawn validation and resolution**: When `SpawnOptions.Components` is non-nil, `SpawnEntitiesWith` validates `doc`, refuses unresolved scene references and nil/disposed worlds, and then walks every layer and object in depth-first document order (and every component in ascending name order) via `planComponentSpawns` before spawning the first entity. Every component must match its registered type (`CheckComponent`, refusing unregistered types, non-mapping or null values, unknown fields, wrong kinds, and out-of-range values). For each component whose `ComponentType.Spawn` is non-nil, `ComponentRegistry.Resolve` produces a fresh `fields` map with normalized Go types and cloned defaults so mutating `fields` inside one `Spawn` cannot leak into other objects or the registry.
- **Deterministic spawn order**: Layers run in `doc.Layers` order; objects within each layer run in depth-first `WalkObjects` order (each parent before its children); and within each object, components with non-nil `Spawn` run in ascending component-name order. Before an object's `Spawn` factories run, the entity already has its local `entities.Transform2D` (with `Hidden: true` on top-level objects of hidden layers so subtrees are not world-visible), its `sceneformat.ObjectRef`, and its `Hierarchy2D` parent link (and its parent already has all of its spawned components). Hidden layers still spawn entities and invoke `Spawn`.
- **Atomic rollback on error**: `SpawnEntitiesWith` tracks the entities it creates in spawn order. If any `ComponentType.Spawn` (or `SetParent`) returns an error, spawning stops immediately—later objects and components are never spawned—and every entity created during this call is destroyed in reverse order (`created[len-1]` down to `created[0]`) so children are destroyed before parents regardless of `Hierarchy2D.RemovePolicy`. Pre-existing entities in `w` remain untouched. The returned error wraps the underlying cause with `%w` and includes layer, object ID, and component name (`sceneformat: layer %q object %q component %q: %w`).
- **Backwards compatibility and connected scenes**: Passing `SpawnOptions{Components: nil}` (including `SpawnEntities`) keeps free-form component behavior without validation or `Spawn` callbacks. Registries whose types leave `Spawn == nil` validate components and skip factories cleanly. `SpawnConnected` runs `Connect` before `SpawnEntitiesWith`, so connection errors never trigger `Spawn`, and a `Spawn` error rolls back all created entities before returning `(nil, nil, err)`.

## Alternatives considered

- **Two-pass spawn (create all entities first, then run all `ComponentType.Spawn` callbacks)**: Considered so a `Spawn` callback could look up sibling or later entities in the same scene. Rejected because cross-object references belong in `SceneDoc.Connections` or a post-spawn pass, and spawning entities on demand in depth-first order guarantees that an early `Spawn` error never spawns later entities and keeps parent-before-child component availability straightforward.
- **Skipping rollback and leaving partially spawned entities in `w`**: Rejected because a failed scene load would leak orphan entities and half-populated component stores into a shared `entities.World`, forcing every caller to clear or discard the world on error.
- **Running validation lazily per entity during the spawn loop**: Rejected because invalid YAML components on later objects would trigger side effects in earlier `ComponentType.Spawn` callbacks before failing; pre-spawn resolution guarantees zero `Spawn` calls and zero mutations to `w` when any component fails validation.

## Consequences

- **Benefits**: Plugins and game packages (including Field Medic's `health` component and future `plugin.Plugin.Components` wiring) can ship schema and ECS runtime attachment together in one `ComponentType`, with deterministic ordering, isolated resolved defaults, and rollback safety for both `SpawnEntitiesWith` and `SpawnConnected`.
- **Costs**: `SpawnEntitiesWith` allocates a pre-resolved slice of component tasks per object with `Spawn != nil` before creating entities, and rollback only undoes entities created by `SpawnEntitiesWith` itself (any external side effects or extra helper entities a `Spawn` callback creates outside the returned entity are the callback's responsibility to avoid or clean up on error).
