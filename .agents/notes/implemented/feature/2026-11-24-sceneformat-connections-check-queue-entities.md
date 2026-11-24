# Agent Note: Cross-file connection checks, deferred signal queue, and entity binding

Status: implemented

## Problem

PR #689 (#490) added declarative `connections:` in scene YAML files and synchronous dispatch via `sceneformat.Connect` and `Signals.Emit`, but left three gaps tracked in #694:

1. `game check` (`packages/game/tooling/assetcheck`) parsed scene files individually without resolving `inherits:` or instanced `scene:` references, so a cross-file connection naming a missing object, component, event or handler passed `game check` and failed only at run time.
2. `Signals.Emit` ran handlers synchronously, so a handler that emitted a follow-up event re-entered mid-step instead of deferring until after the current `game.Loop` step or handler returned.
3. Spawning a scene into `entities.World` via `sceneformat.SpawnEntities` had no built-in way to bind `connections:` to those `entities.Entity` handles, validate connections before mutating the world, or skip despawned entities safely.

## Decision

Extend the existing `sceneformat` (`ComponentType.Events`/`Handlers`, `Connect`, `Signals`) and `tooling/assetcheck` APIs in place without introducing a separate event registry or bus:

- **Resolved connection checking in `tooling/assetcheck`**:
  - `Problem` gains `Column int` (formatted as `file:line:col: message` when non-zero, preserving `file:line: message` when zero for existing checks).
  - `CheckWith(fsys, Options{Components: reg})` accepts an optional `*sceneformat.ComponentRegistry`; `Check(fsys)` delegates to `CheckWith(fsys, Options{})`.
  - `checkScene` resolves scenes that refer to other scenes (`RefersToScenes()`) through a caching `sceneLoader` against the scene's project root, resolving a probe copy without the scene's own connections so `Resolve` yields all inherited and instanced objects even when the scene's own connections are broken.
  - Every connection in the scene is validated against the resolved scene so multiple broken connections are all reported with exact 1-based YAML line and column (`yamlConnectionPos` matching `sceneformat.ConnectionError`). Without a registry, missing `from`/`to` objects, absent components on qualified `comp.event`/`comp.handler` names, duplicate inherited/instanced connections, and scene cycles are caught; with `Options.Components`, unregistered components, unknown or ambiguous events/handlers, invalid arguments, and canonical duplicates are checked via `sceneformat.ValidateConnections` and `sceneformat.ValidateComponents`.
  - Referenced scene files that are missing return `errRefUnavailable` so `checkRefs` remains the single reporter (preserving moved-file hints); referenced scene files that are invalid (including non-`*.scene.yaml` files) are checked and reported once on their own file, line, and column, and sanitized copies are returned to referencing scenes to prevent cascading duplicate errors.
- **Deferred FIFO event queue on `sceneformat.Signals`**:
  - `Signals.Enqueue(from, event, payload)` validates `from` and `event` immediately (refusing unknown objects, undeclared events, and ambiguous bare names) and appends to an internal FIFO queue. `Signals.Queued()` reports the pending count.
  - `Signals.Flush()` drains queued events wave by wave in strict FIFO enqueue order without recursion: events enqueued by handlers during wave `N` are appended to wave `N+1` and execute only after wave `N` finishes.
  - `Flush()` refuses re-entrant calls from inside a running handler and caps consecutive waves at `MaxFlushWaves` (`= MaxEmitDepth = 64`), discarding any remaining queue and returning an error if handlers loop forever so `Signals` recovers cleanly.
- **Atomic entity binding on `sceneformat`**:
  - `Call` gains `World *entities.World`, `SourceEntity entities.Entity`, and `TargetEntity entities.Entity` (nil / `entities.Zero` when created with `Connect`).
  - `ConnectEntities(doc, reg, w, spawned, ctx)` binds a scene's connections to a 1-to-1 map of live entities in `w`, rejecting nil/disposed worlds and missing, dead, or duplicate entity mappings.
  - `SpawnEntitiesWithSignals(doc, reg, w, ctx)` runs full validation (`Connect`) before calling `SpawnEntities(doc, w)`, guaranteeing `w` is untouched on any validation or missing-`Func` error.
  - `Signals.Entity(id)`, `Signals.EmitEntity(entity, event, payload)`, and `Signals.EnqueueEntity(entity, event, payload)` address bound entities directly. Because `entities.Entity` handles are generational, despawned source or target entities (`!w.Alive(e)`) are skipped cleanly on `Emit`, `Enqueue`, `EmitEntity`, `EnqueueEntity`, and `Flush` (while still validating event names for typos and rejecting unbound or recycled-generation entities).

## Alternatives considered

### Separate `EventRegistry` / `EventBus` package for scene connections

Introducing a dedicated event bus type parallel to `ComponentRegistry` and `Signals` would decouple event registration from components. It was rejected because #689 already anchored declarative connections on `ComponentType.Events`/`Handlers`, `Connect`, and `Signals`, and splitting registration would force games to register the same component behavior twice and break editor/schema tooling alignment.

### Running `sceneformat.Resolve` directly on the full scene with connections included in `assetcheck`

Passing the unmodified `SceneDoc` to `sceneformat.Resolve` is shorter, but `Resolve` runs `Validate(out)` and stops at the first missing `from`/`to` object with a flattened index across inherited and instanced connections (which does not map directly to the file's YAML sequence index) and drops subsequent broken connections. Resolving a probe without the file's own connections yields the full resolved object table and inherited/instanced connections, allowing every connection in the file to be checked and mapped to its exact YAML line and column.

### Draining only a single wave per `Flush()` vs bounded multi-wave drain

Leaving handler-enqueued events for the next frame's `Flush()` is simpler, but breaks single-step chains where a button opens a door and the door's `opened` event triggers a lamp within the same step's flush. Bounded wave draining (`MaxFlushWaves`) preserves non-reentrant FIFO execution (each handler returns before its enqueued follow-up runs), completes finite chains in one step, and still bounds infinite self-enqueue loops deterministically.

## Consequences

- **Benefits**:
  - `game check` catches broken cross-file connections (and with `assetcheck.CheckWith`, broken events, handlers, arguments, and component overrides) in CI with exact `file:line:col` diagnostics.
  - Games can enqueue events during a `game.Loop` update step and call `Signals.Flush()` once at step end with deterministic FIFO ordering and zero handler recursion.
  - Scenes spawned into `entities.World` validate atomically before spawning and safely tolerate entity despawns during or between dispatches.
- **Costs & limits**:
  - The standalone `game check` CLI has no access to a game's compiled Go `*sceneformat.ComponentRegistry`; without `Options.Components`, it checks resolved `from`/`to` objects, qualified `component.name` membership on resolved objects, duplicates, and resolution rules, while full bare event/handler/arg validation requires calling `assetcheck.CheckWith` from a Go test or CI tool with the game's registry.
  - `Signals` remains single-goroutine and snapshots the scene's object/entity bindings at `Connect` / `SpawnEntitiesWithSignals` time.

## Verification

- `go tool nsgo test ./sceneformat` verifies FIFO `Enqueue`/`Flush`, zero handler recursion (`maxActive == 1`), `MaxFlushWaves` loop guard and recovery, re-entrant `Flush` refusal, `SpawnEntitiesWithSignals` atomic validation leaving `World` untouched on errors, `ConnectEntities`, `EmitEntity`/`EnqueueEntity`, despawned source/target skipping (before dispatch, mid-dispatch, and between enqueue and flush), and generational slot recycling.
- `go tool nsgo test ./tooling/assetcheck` and `go tool nsgo test ./cmd/game` verify cross-file connection checks (instanced, inherited, non-`*.scene.yaml` referenced scenes, qualified missing components, `Options.Components` events/handlers/args/overrides, multiple errors in one file, and scene cycles) with exact `file:line:col` positions, plus CLI `game check` failure output.
- `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, `gofmt`, and `bun run verify-notes` verify cross-target builds, schemas, changelog, formatting, and notes.
