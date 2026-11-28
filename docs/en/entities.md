# Entities

A small, zero-reflection entity-component-system (ECS) for NSGo games: generational entities, typed sparse-set components, allocation-free queries, topologically ordered systems, prefabs and JSON world snapshots integrated with `game/save`.

```go
import "github.com/nsgo-dev/game/pkg/entities"
```

`go tool game generate component <name>`, `system <name>` and `prefab <name>` write a new component, system or prefab into a game with its registration and a test; see [Command Line](cli.md#generate).

## Architectural decision

Before implementing `entities` ([#136](https://github.com/nsgo-dev/game/issues/136)), three designs were compared for representing game objects in `github.com/nsgo-dev/game`:

1. **A small, zero-reflection, generational-index ECS with typed sparse-set storage** (chosen).
2. **An object-oriented scene tree** of heap-allocated nodes with virtual update methods.
3. **A reflection-driven archetype ECS** with dynamic component tables and query planners.

Option (1) was chosen for five reasons:

- **Zero-allocation hot loops**: entities are 64-bit generational handles (32-bit slot index + 32-bit generation) recycled through a free list, and each component type lives in a contiguous typed sparse set (`Store[T]`). Creating and destroying 100k entities in steady state, accessing components, and iterating queries perform zero heap allocations.
- **Zero reflection**: Go generics identify component stores through zero-sized type tags and keep all component access, queries and prefab builders statically typed with no `reflect` imports.
- **Safe handle lifetimes**: destroying an entity increments its slot generation before recycling it, so stale `Entity` handles never alias newly spawned entities.
- **Explicit, deterministic execution**: systems declare `Before` and `After` constraints resolved once by Kahn's topological sort with registration-order tie-breaking. Cycles and missing targets fail cleanly, and `Order()` exposes the exact execution sequence for debug overlays.
- **Clean serialization boundaries**: only component types registered with `RegisterCodec[T]` are written to deterministic `WorldSnapshot` JSON values and saved through `game/save`, keeping transient runtime handles (sprites, audio voices) out of save files while preserving entity IDs and cross-entity references.

## Entities and World

An `Entity` is a `uint64` carrying a 32-bit slot index and a 32-bit generation (`entities.Zero` is the invalid handle and is never alive). `World` owns entities and their component stores.

- `NewWorld()` returns a fresh world (the zero value of `World` is also ready to use).
- `Spawn()`, `Destroy(e)`, `Alive(e)` and `Count()` manage entity lifetimes with free-list recycling.
- `Clear()` removes all entities and components while bumping generations and keeping slice capacities for zero-allocation reuse. `Reset()` releases component and active entity backing storage while keeping bumped slot generations, so handles from before `Reset()` stay dead even after new entities are spawned.
- `Attach(cx)` binds the world to an NSGo `*core.Scope` so `Dispose()` runs automatically when the scope is disposed.

```go
world := entities.NewWorld()
world.Attach(cx)

hero := world.Spawn()
world.Alive(hero) // true
world.Destroy(hero)
world.Alive(hero) // false, even after the slot is recycled
```

## Components and queries

Each component type `T` is stored in a typed sparse set (`*Store[T]`). Top-level generic functions operate directly on a `*World`, and `Component[T](w)` returns the underlying `*Store[T]` for direct inner-loop access.

- `Set[T](w, e, v)`, `Get[T](w, e)`, `Has[T](w, e)` and `Remove[T](w, e)` attach, read, check and detach components in O(1) time.
- `Each1[A]`, `Each2[A, B]` and `Each3[A, B, C]` iterate over the smallest sparse set and check O(1) membership in the others, visiting exact matches in deterministic order with zero reflection and zero heap allocations. Returning `false` stops iteration early. The component pointers are valid only inside the callback and only until it adds a component of an iterated type: adding can grow the store and move its values, so collect entities and add components after the loop. A callback may destroy the entity it was handed, but not add or remove other entities of the iterated stores; collect those and act after the loop.

```go
type Position struct{ X, Y float64 }
type Velocity struct{ DX, DY float64 }

entities.Set(world, hero, Position{X: 10, Y: 20})
entities.Set(world, hero, Velocity{DX: 60, DY: 0})

entities.Each2[Position, Velocity](world, func(e entities.Entity, p *Position, v *Velocity) bool {
	p.X += v.DX * dt.Seconds()
	p.Y += v.DY * dt.Seconds()
	return true
})
```

## Systems and update order

`Scheduler` runs named systems in a deterministic topological order on each `game.Loop` fixed step.

- `AddSystem(name, fn, opts...)` registers a system with optional `Before(other)` and `After(other)` constraints. Targets may be registered in any order.
- `HasSystem(name)` reports whether a system with `name` is already registered on the scheduler.
- `Build()` validates constraints and resolves execution order with Kahn's algorithm, breaking ties by registration order. Unknown targets return `ErrUnknownSystem` and cycles return `ErrSystemCycle`.
- `Order()` returns the resolved names and `String()` formats them as `"a -> b -> c"` for debug overlays.
- `Step(w, dt)` resolves if needed and runs each system in order with zero steady-state allocations.
- For reusable plugins, `game/pkg/gameplugin` adds four standard stage anchor pairs (`StageInput` -> `StageUpdate` -> `StagePhysics` -> `StageLate`) once per scheduler so plugin systems can attach to a stage without knowing the game's system names; see [Plugins](plugins.md#standard-scheduler-stages).

```go
sched := entities.NewScheduler()
_ = sched.AddSystem("input", readInput)
_ = sched.AddSystem("physics", stepPhysics, entities.After("input"))
_ = sched.AddSystem("render", syncSprites, entities.After("physics"))

loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		_ = sched.Step(world, dt)
	},
}
```

## Prefabs

A `Prefab` is a named template that stamps a preset group of components onto newly spawned entities.

- `NewPrefab(name)` creates a template, and `Clone(name)` derives a variant without mutating the original.
- `With[T](p, v)` attaches a value component copied per entity.
- `WithFunc[T](p, make)` invokes `make()` on each spawn so slices, maps and pointers are never shared across entities.
- `Spawn(w)` and `SpawnN(w, n)` create independent entities from the template.

```go
goblin := entities.NewPrefab("goblin")
entities.With(goblin, Position{X: 10, Y: 20})
entities.WithFunc(goblin, func() Inventory {
	return Inventory{Items: []string{"dagger"}}
})

squad := goblin.SpawnN(world, 5)
```

## 2D transform hierarchy

`world.Hierarchy2D()` (or `entities.NewHierarchy2D(world)`) attaches a parent-child 2D transform hierarchy to a World. Local transforms live in the `Transform2D` component (`X`, `Y`, `Rotation`, `ScaleX`, `ScaleY`, `Hidden`; build one with `NewTransform2D` or `IdentityTransform2D`, since the zero value has zero scale), and `Sync()` composes them into cached `WorldTransform2D` components that `Each1`/`Each2`/`Each3` can iterate directly.

```go
h := world.Hierarchy2D()
ship, turret := world.Spawn(), world.Spawn()
h.SetLocal(ship, entities.NewTransform2D(100, 50))
h.SetLocal(turret, entities.NewTransform2D(0, -12))
_ = h.SetParent(turret, ship, false)
h.SetRotation(ship, 0.5)
h.Sync()
wt, _ := h.WorldTransform(turret)
_ = wt
```

- `SetLocal`, `SetPosition`, `SetRotation`, `SetScale`, `SetVisible` and `entities.Set` with a `Transform2D` mark the entity and its subtree dirty. After writing through a `*Transform2D` from `Get` or a store, call `h.MarkDirty(e)`.
- Visibility is inherited: `WorldTransform2D.WorldVisible` is false when the entity or any ancestor is hidden.
- `SetParent(child, parent, keepWorld)` and `Detach(child, keepWorld)` keep the child's local transform, or with `keepWorld` recompute it so the child stays where it is on screen. A keep-world reparent whose exact relative transform needs shear (a rotated child under a non-uniformly scaled parent) returns `ErrNotDecomposable` and changes nothing; cycles return `ErrCycle` and dead entities `ErrDeadEntity`.
- Destroying a parent applies the hierarchy's `RemovePolicy` (`SetRemovePolicy`): `RemoveDetach` (default) detaches the children keeping their local transforms, `RemoveDetachKeepWorld` detaches them keeping their world transforms, and `RemoveCascade` destroys the whole subtree. `DestroyWithPolicy(e, policy)` overrides it for one call.
- Snapshots only include registered components: call `RegisterCodec[entities.Transform2D](w, "transform2d")` to save local transforms. `WorldTransform2D` is derived, so it does not need a codec; the hierarchy reseeds from the restored local transforms.

## Spawning from YAML scenes

`game/sceneformat` spawns a self-contained YAML scene directly into a `*World` with `SpawnEntities` (transforms and `ObjectRef` only) or `SpawnEntitiesWith` (also attaching typed ECS components declared on a `ComponentRegistry` via `ComponentType.Spawn`):

```go
type Health struct{ HP, Max int }

reg := sceneformat.NewComponentRegistry()
reg.MustRegister(sceneformat.ComponentType{
	Name: "health",
	Fields: []sceneformat.Field{
		{Name: "hp", Kind: sceneformat.FieldInt, Default: 10},
		{Name: "max", Kind: sceneformat.FieldInt, Default: 10},
	},
	Spawn: func(w *entities.World, e entities.Entity, fields map[string]any) error {
		entities.Set(w, e, Health{HP: fields["hp"].(int), Max: fields["max"].(int)})
		return nil
	},
})

spawned, err := sceneformat.SpawnEntitiesWith(doc, world, sceneformat.SpawnOptions{Components: reg})
```

- **Pre-spawn validation & defaults**: when `SpawnOptions.Components` is non-nil, every component on every object is validated and resolved with its defaults (`ComponentRegistry.Resolve`) before the first entity is spawned. Registries that only check files may leave `ComponentType.Spawn` nil.
- **Deterministic order**: entities spawn in depth-first document order (parents before children, linked into `Hierarchy2D` with `Transform2D` and `ObjectRef`), and within each object components run in ascending component-name order. Hidden layers spawn top-level objects hidden so their subtrees are not world-visible while still running `Spawn`.
- **Rollback on error**: if any `ComponentType.Spawn` returns an error, every entity created during the call is destroyed in reverse order (leaving pre-existing entities in `w` untouched) and the error names the layer, object ID and component.
- **Connected scenes**: `sceneformat.SpawnConnected(doc, world, reg, ctx)` passes `reg` through `SpawnEntitiesWith`, so connection handlers can immediately read typed components with `entities.Get`.

## Saving and loading a world

Worlds serialize to deterministic `WorldSnapshot` JSON values and integrate directly with `game/save`.

- `RegisterCodec[T](w, typeName)` registers a JSON codec for component type `T`. Unregistered components are omitted from snapshots.
- `w.Snapshot()` emits live entities in ascending slot-index order and components in ascending codec-name order, preserving exact indices and generations so cross-entity `Entity` references stay valid after restore.
- `w.Restore(snap)` decodes into a temporary world first and commits atomically, leaving `w` untouched if any codec is missing or decoding fails. Slots omitted from `snap` (including an empty snapshot) keep bumped generations so stale handles from before `Restore` stay dead even after new entities are spawned, and rolling a slot back to an earlier generation keeps a generation floor so destroying the restored entity never re-issues a generation already used before the restore.
- `SaveWorld(store, slot, w)` and `LoadWorld(store, slot, w)` persist and restore worlds through `save.Store` (both `save.NewStorageStore` and `save.NewFileStore`, including schema versions and migrations). `LoadWorld` checks the snapshot while the slot decodes, so a snapshot it cannot restore (an unknown codec, a bad parent, a component that does not decode) falls back to the backup copy, and its error names the slot, the line and the field, such as `data.entities[2].parent`.

```go
_ = entities.RegisterCodec[Position](world, "position")
_ = entities.RegisterCodec[Velocity](world, "velocity")

store := save.NewStorageStore(cx.Storage(), "game")
if err := entities.SaveWorld(store, "slot1", world); err != nil {
	return err
}

fresh := entities.NewWorld()
_ = entities.RegisterCodec[Position](fresh, "position")
_ = entities.RegisterCodec[Velocity](fresh, "velocity")
if err := entities.LoadWorld(store, "slot1", fresh); err != nil {
	return err
}
```
# Wide queries

`Each4` visits the intersection of four component stores using the smallest store as driver, with deterministic order and zero allocations like `Each1`–`Each3`.
