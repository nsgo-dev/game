# Agent Note: Save part registry, slot layout, and per-scene object persistence

Status: implemented

## Problem

`save.Store` stores one versioned Go value per slot, leaving every game to author a monolithic save struct, manually wire snapshot/restore calls across subsystems (`inventory`, `quest`, `stats`, `ability`, `flow`, `entities`), and duplicate staging logic to avoid half-restored state on load failure. Games also have to invent their own per-level maps (such as `rpg2d`'s `defeated` and `taken`) to remember destroyed, moved or toggled scene objects across room transitions and reloads.

## Decision

`pkg/save` provides a registry (`save.Game`), a per-scene object persistence store (`save.Persistence`), and generic adapters (`ValuePart`, `FuncPart`, `StagedFuncPart`), while each snapshot-capable subsystem exposes a `SavePart() save.Part` method, `pkg/sceneformat` applies/captures `save.Persistence`, and `pkg/save/savetest` verifies full-game save round-trips.

### Slot layout

Inside `save.Store`'s outer `{"version": N, "data": ...}` envelope, `Game.Save` writes a strict JSON object with a single `"parts"` key mapping each registered part name to its own versioned envelope:

```json
{
  "version": 1,
  "data": {
    "parts": {
      "bag": { "version": 1, "data": { "slots": [] } },
      "persistence": { "version": 1, "data": { "scenes": {} } },
      "player": { "version": 2, "data": { "map": "town", "hp": 30 } }
    }
  }
}
```

- **Strict parsing**: unknown or duplicate keys on the game object or any part entry are refused as `save.ErrCorrupt` (which triggers `Store.LoadChecked` backup recovery), except that a registered part carrying a version higher than registered reports `save.ErrFutureVersion` even when extra fields are present, matching `Store`'s own envelope contract.
- **Deterministic output**: `Game.Save` calls `SaveState()` in registration order and encodes `"parts"` as a JSON object whose keys `encoding/json` writes in ascending order, so identical state always produces byte-identical slots.
- **Two-level versioning**: `Store.Version` versions the outer slot structure (for example, upgrading a legacy flat save struct from `v1` to `v2` parts via `save.EncodeParts` in a `Store` migration), while each registered part has its own integer version and one-step `v -> v+1` migrations (`WithPartMigration` / `RegisterMigration`).
- **Missing and removed parts**: when an older slot omits a registered part, `Game.Load` initializes it from `WithPartDefault` when configured, calls `ResetState()` when it implements `save.Resetter`, or leaves it untouched otherwise. Unregistered parts present in a slot are ignored so games can drop obsolete parts without breaking older saves.
- **All-or-nothing loading**: `Game.Load` executes inside `Store.LoadChecked`. Every `save.StagedPart` runs `PrepareLoad` before any part mutates live state; when all parts are staged (which holds for all built-in adapters and `ValuePart`/`StagedFuncPart`), commit cannot fail. If any plain `save.Part` is registered, `Game.Load` captures pre-load JSON snapshots before committing in registration order and rolls back in reverse order on failure.

### Package boundaries and adapters

Because `game/pkg/save` is a `Stable` package, it never imports `Experimental` packages (`inventory`, `quest`, `stats`, `ability`, `entities`, `sceneformat`), and `pkg/entities` already imports `pkg/save`. Instead, `pkg/save` exposes `StagedFuncPart[T]` which performs `json.Unmarshal` inside `pkg/save`, and each subsystem package provides a `SavePart() save.Part` method (`inventory.Container`, `inventory.Equipment`, `quest.Log`, `stats.Sheet`, `ability.Caster`, `flow.Machine[S]`, `dialogue.Runner`, `entities.World`) without importing `encoding/json`. `pkg/stats` also gains `Sheet.Snapshot()` and `Sheet.Restore()`. (The project-wide variables/switches of #1754 are not yet landed and will add their own `SavePart()` when implemented.)

### Scene persistence

`save.Persistence` stores per-scene `ObjectState` keyed by cleaned slash-separated scene path and object ID (`destroyed`, `x`, `y`, `posZ`, `rotation`, `components`, `switches`, `vars`). `PrepareLoad` decodes JSON with `UseNumber()` and converts integral `json.Number` values back to Go `int` so `sceneformat.FieldInt` validation continues to pass after reload. `pkg/sceneformat` provides `ApplyPersistence`, `BuildWithPersistence`, `CapturePersistence`, and `SpawnOptions.Persistence`/`ScenePath`; destroyed objects are omitted along with their descendants and any `Connections` referencing them, without mutating the caller's `SceneDoc`.

## Alternatives considered

1. **Put package adapters in `pkg/save`**: would give one import for all adapters (`save.InventoryPart(bag)`). Rejected because `pkg/save` is `Stable` and cannot import `Experimental` packages (enforced by `TestStabilityTable`), and `pkg/entities` already imports `pkg/save` (import cycle).
2. **Store parts as a JSON array `[{"name":..., "version":..., "data":...}]`**: preserves registration order in the JSON file. Rejected because keyed objects are easier to inspect, diff, and migrate by name (`PeekPart`, `jq`), while registration order is already owned by `save.Game` in Go code.
3. **Require every `Part` to implement `PrepareLoad`**: simplifies `Game.Load` by removing rollback code. Rejected because simple game parts should only need `SaveState() (any, error)` and `LoadState(json.RawMessage) error`; making `StagedPart` optional with snapshot rollback for plain `Part` keeps the base interface minimal while preserving the all-or-nothing guarantee.

## Consequences

- **Benefits**: games register saveable subsystems once and get deterministic slots, per-part migrations, backup recovery on part validation failure, all-or-nothing loads, per-scene object persistence across reloads/transitions, and automated round-trip verification via `savetest.RoundTrip`.
- **Costs**: `pkg/save/savetest` adds a new `Experimental` nested package listed in `docs/{en,zh}/stability.md` and `skills/nsgo-game/references/packages.md`. Plain `Part` implementations (without `StagedPart`) incur a pre-load snapshot cost during `Game.Load` to support rollback.
- **Open follow-ups**: adopting `save.Game` and `save.EncodeParts` migration in `template/rpg2d` waits for `v0.1.1` (because `template/rpg2d/template/go.mod.tmpl` pins published `github.com/nsgo-dev/game v0.1.0` and CI tests templates against their pinned modules with `GOWORK=off`); adding `SavePart()` for project-wide variables/switches waits for #1754.
- **Verification**: `go tool nsgo test` across `pkg/save/...` (including `savetest`), `pkg/inventory/...`, `pkg/quest/...`, `pkg/stats/...`, `pkg/ability/...`, `pkg/flow/...`, `pkg/entities/...`, `pkg/sceneformat/...`, and `internal/repocheck/...`; `go tool nsgo check -jobs 1`; `bun run verify-notes`; `(cd tools && go run ./changelog check)`.
