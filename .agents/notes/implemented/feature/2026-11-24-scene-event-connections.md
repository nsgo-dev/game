# Agent Note: Declarative scene event connections

Status: implemented

## Problem

Scene files in `packages/game/sceneformat` describe object hierarchies, components, instanced sub-scenes, and inheritance, but wiring gameplay interactions between placed objects (such as a button opening a specific door in a level) previously required Go glue code per level. That breaks the project goal of text-file scenes an author or AI can inspect and edit whole, and prevents instanced sub-scenes (such as a reusable button-and-door puzzle pair) from carrying self-contained internal wiring.

## Decision

Version 2 scene documents (`SceneDoc.Connections`, YAML key `connections`) declare event-to-handler connections as a list of `ConnectionDoc` values (`from`, `event`, `to`, `handler`, optional static `args` map, optional `once` bool), published in `apps/website/public/schemas/scene.v2.json` and refused in version 1 scenes (`scene.v1.json` remains unchanged).

### Format, validation, and resolution invariants

- `Validate` refuses `connections` in version 1 scenes and refuses empty `from`, `event`, `to`, or `handler` fields with `*FieldError` paths (`connections[i].<field>`). For self-contained scenes (`!doc.RefersToScenes()`), `Validate` requires both `from` and `to` to name existing object IDs in the scene.
- For scenes using `inherits` or `scene` instancing, `Resolve` carries every instanced scene's connections with both `From` and `To` prefixed by `<instanceID>/` (matching the prefixed object IDs so multiple instances stay strictly isolated), carries base connections ahead of derived connections on inheritance, caps total resolved connections at `MaxResolvedConnections` (`1 << 17`), and checks `doc.Connections` against the complete resolved object ID set so errors carry exact `*FieldError` indices into `doc`.
- `tooling/assetcheck` (`game check`) checks `inherits` and `scene` file references and resolves referencing scenes, reporting broken connection endpoints at the offending YAML line.

### Registry and runtime dispatch

- `EventRegistry` declares known `EventType` and `HandlerType` definitions explicitly in Go without reflection, mirroring `ComponentRegistry`. `HandlerType.Args` reuses `Field` (`FieldBool`, `FieldInt`, `FieldFloat`, `FieldString`, defaults, ranges, steps) via a shared `validateFields` helper. Optional `Component` names on `EventType` and `HandlerType` verify that source/target objects carry the expected component when present in the document.
- `ParseWith` with `ParseOptions.Events` (and `ValidateConnections`) returns `*loaderr.Error` values wrapping `*FieldError` with file, line, column, field path, expected names, and actionable fix. Unset handler arg defaults are never written back into `SceneDoc`, preserving byte-for-byte YAML round-trips.
- `HandlerType.Func` may be nil during registration for validation-only registries; binding refuses any connected handler with a nil `Func` before spawning entities or returning a bus, keeping `SpawnEntitiesWith` atomic.
- `EventRegistry.Bind`, `EventRegistry.BindEntities`, `BuildWith` / `BuiltScene.BindEvents`, and `SpawnEntitiesWith` wire connections into an `EventBus` that runs matching handlers in document order, disconnects `once: true` connections before invoking the handler (safe under re-entrant `Emit`), supports deferred `Enqueue` / `EnqueueEntity` + `Flush`, gives each call a fresh copy of resolved `Args`, and skips destroyed entities when bound to an `entities.World`.

## Alternatives considered

### Putting connections on each source `ObjectDoc` instead of top-level `SceneDoc.Connections`

Placing a `connections` list directly on each emitting `ObjectDoc` keeps outgoing edges beside the emitter, which looks compact for single-object triggers. However, it makes auditing all wiring in a scene harder (readers and AI tools have to scan every nested layer and child tree), complicates overrides on instanced scenes, and diverges from Godot's `.tscn` layout where `[connection ...]` entries live together at the bottom of the scene file. A top-level `connections` section keeps all wiring in one diff-friendly block and composes cleanly through `Resolve`.

### Bumping the scene schema to version 3

Introducing a new `scene.v3.json` would isolate event connections from instancing and inheritance. Because scene format version 2 is still unreleased under the same text-project milestone (`changelog.d/scene-instancing-and-inheritance-483.md`), adding `connections` to version 2 while keeping `scene.v1.json` frozen avoids fragmenting the schema before version 2 ships.

### Reusing `flow.Bus` directly for scene connections

`flow.Bus` dispatches strictly by Go struct type (`reflect.TypeFor[T]()`), which is ideal for typed inter-system Go events with compile-time checking. YAML scene connections are keyed by `(objectID, eventName)` strings loaded at runtime and carry bound YAML `Args` plus object/entity handles, so a dedicated `EventBus` backed by `EventRegistry` avoids reflection over user structs and gives exact `loaderr` diagnostics at load time.

## Consequences

### Benefits

- Level authors and AI tools can wire gameplay interactions (such as button-opens-door chains) entirely in deterministic YAML with zero per-level Go glue code.
- Instanced sub-scenes carry isolated internal connections automatically via `<instanceID>/` prefixing during `Resolve`.
- Misspelled object IDs, events, handlers, missing required components, and invalid static args fail early at parse/check time with file, line, column, field, and fix.

### Costs

- Scene files that declare connections must use `version: 2`; version 1 files cannot use `connections`.
- `EventRegistry` is separate from `ComponentRegistry`; callers validating both components and connections pass both in `ParseOptions`.
- Editor UI support for visually editing connections is not yet wired into `packages/game/editor` and remains follow-up work.
