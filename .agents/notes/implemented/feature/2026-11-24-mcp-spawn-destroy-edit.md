# Agent Note: MCP object spawning, destruction and component editing

Status: implemented

## Problem

With [MCP scene, layer, entity and component inspection](2026-11-24-mcp-scene-entity-inspection.md) in place, coding agents running a game under `nsgo dev -automation` can read scenes, layers, live entities, and components, but cannot mutate runtime world state—they cannot spawn prefabs to reproduce bugs, destroy entities, or tweak component values to test gameplay hypotheses on the next simulation step.

## Decision

Extend `packages/game/mcp` with a dedicated mutation tool group in `edit.go` (`game_prefabs_list`, `game_entity_spawn`, `game_entity_destroy`, `game_component_set`, `game_component_remove`) hooked into `Bridge.Tools()`, backed by prefab registration APIs (`Config.Prefabs`, `Bridge.RegisterPrefab`, `Bridge.RegisterEntityPrefab`, `Bridge.RegisterScenePrefab`), typed component editor APIs (`RegisterMutableComponent`, `RegisterComponentEditor`), and strict codec mutation helpers (`World.DecodeComponent`, `World.PatchComponent`, `World.RemoveComponent`) in `packages/game/entities`.

## Architecture and mutation contract

- **Minimal hookup in `mcp.go`**: All mutation types, schemas, registration APIs, and handlers live in `packages/game/mcp/edit.go`; `mcp.go` adds only `Config.Prefabs`, `write`/`remove` hooks on `componentEntry`, prefab storage fields on `Bridge`, and `b.editTools()` in `Bridge.Tools()`. This isolates parallel MCP PRs (#462–#469) from merge conflicts.
- **Prefab sources**: `Bridge` supports three prefab kinds (`"custom"`, `"entity_prefab"`, `"scene"`): custom `PrefabSpawner` callbacks with optional `overrides`, `*entities.Prefab` templates via `RegisterEntityPrefab`, and validated self-contained `*sceneformat.SceneDoc` templates via `RegisterScenePrefab` (instantiated through `sceneformat.SpawnEntities`). `game_prefabs_list` enumerates all registered prefabs in deterministic sorted order.
- **Atomic spawning and rollback**: `game_entity_spawn` validates `x`/`y` finite float32 bounds, parent ref liveness, and prefab existence before spawning. If applying `parent` or any entry in `components` fails after spawn, every newly created entity (including scene-prefab children) is destroyed before returning the error so failed calls never leak half-initialized entities.
- **Patch-by-default strict component edits**: `game_component_set` defaults to patching existing component fields (`replace: false`) so editing a single field (such as `transform2d.x` or `health.hp`) preserves omitted fields like `scaleX`/`scaleY` or `max`, while `replace: true` decodes into a fresh zero value. Both codec helpers (`World.PatchComponent` / `World.DecodeComponent`) and bridge editors decode JSON with `DisallowUnknownFields()`, reject trailing tokens, reject `null` for non-nullable types, and reject non-finite floats (`NaN`, `±Inf`). Patching (`internal/jsonedit.Patch`) decodes onto a clone whose exported pointers, maps, slices and interfaces are freshly allocated, so a partial decode error or a rejecting editor never reaches the live entity, and then takes from the clone only the fields the patch changed, so unexported fields, `json:"-"` fields (callbacks, handles, caches) and the identity of untouched pointers survive. A JSON round-trip copy was the first approach; it was replaced because it zeroed exactly those runtime fields on `RegisterMutableComponent` types, and its shallow-copy fallback for values that cannot be marshalled shared live maps with the decoder. Codec edits are also refused when the decoded value cannot be encoded back, so a custom `UnmarshalJSON` that yields `NaN` cannot break later snapshots.
- **Hierarchy invariants**: Built-in `"transform2d"` writes go through `entities.Set`, which delegates to `Hierarchy2D.SetLocal` whenever a hierarchy is attached so `WorldTransform2D` dirty flags propagate to descendants automatically. Built-in `"world_transform2d"` is explicitly read-only because it is a derived cache overwritten on `Hierarchy2D.Sync()`. `game_entity_destroy` accepts `"cascade"`, `"orphan"`, `"keep_world"`, and `"reparent"` policies without lazily instantiating `Hierarchy2D` when an entity has no children; `"reparent"` keeps the children's world transforms, and an omitted policy reports the hierarchy's actual default (or `"none"` for a childless entity). Scene prefabs must have exactly one top-level object so the spawn result names every spawned entity through the root's subtree.
- **Read-only vs mutable bridge components**: `RegisterComponent[T]` and `RegisterComponentFunc` remain read-only so inspection-only registrations (such as components carrying callbacks) do not silently become writable. Callers opt in with `RegisterMutableComponent[T]` or upgrade an existing registration with `RegisterComponentEditor[T]` to enforce domain invariants before writing.

## Alternatives considered

### Making `RegisterComponent[T]` automatically writable

Allowing `game_component_set` to write to every `RegisterComponent[T]` registration would save one registration call for transient structs. It was rejected because games intentionally use `RegisterComponent` for read-only debug views or structs with runtime callbacks/handles (`OnTick func()`, pointers), where unconstrained JSON writes could corrupt invariants or zero unexported fields; explicit `RegisterMutableComponent` and `RegisterComponentEditor` keep the safety boundary clear while `entities.RegisterCodec` components remain automatically editable.

### Replace-only `game_component_set` (no default patch mode)

Decoding every `game_component_set` payload into a zero-valued struct would be simpler to implement. It was rejected because Go's zero value for `entities.Transform2D` has `ScaleX: 0, ScaleY: 0` (collapsed scale), so an agent sending `{"x": 100, "y": 50}` would silently make the object invisible, and tweaking one field on a gameplay struct (like `health.hp`) would wipe unrelated fields (`health.max`). Patching by default with an explicit `"replace": true` flag supports both workflows safely.

### Handling codec decoding purely inside `packages/game/mcp` via `World.Snapshot` / `Restore`

Avoiding changes to `packages/game/entities` by snapshotting, editing JSON, and calling `World.Restore` would keep `entities` untouched. It was rejected because `Snapshot`/`Restore` is O(world size), resets un-serialized transient component stores, and does not support strict `DisallowUnknownFields` decoding for a single entity's component. Adding `DecodeComponent`, `PatchComponent`, and `RemoveComponent` methods to `entities.World` keeps single-entity edits O(1) and preserves transient components.

## Consequences

- **Benefits**: Coding agents can discover prefabs, spawn entities (with hierarchy and initial components), edit codec/built-in/custom components with immediate effect on the next simulation step, remove components, and destroy subtrees with explicit hierarchy policies—all with strict validation and atomic rollback on error.
- **Costs**: `packages/game/entities` gains three public codec mutation methods (`DecodeComponent`, `PatchComponent`, `RemoveComponent`) to maintain alongside `EncodeComponent`. Patch mode clones the existing component value with reflection and compares fields to merge the result, which is negligible for interactive MCP edits but not intended for per-frame inner loops.

## Verification

Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./mcp`, `go tool nsgo test ./entities`, `go tool nsgo test -run TestStabilityTable .`, `go run ./tools/changelog check`, and `bun run verify-notes`.
