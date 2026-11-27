# Agent Note: Preserve entity slot generations across World.Reset and World.Restore

Status: implemented

## Problem

`Entity` promises that destroying or replacing entities bumps slot generations so stale handles never alias newly spawned entities (`pkg/entities/world.go`). Two paths broke that guarantee (#1578):

1. `World.Restore` (`pkg/entities/snapshot.go`) sized the new slot table to `maxIdx + 1` and skipped generation preservation altogether for an empty snapshot, dropping generations for every slot above `maxIdx` (or all slots on an empty snapshot) so subsequent `Spawn` calls restarted them at generation 1. In addition, dead-slot generation increments did raw `+ 1` without wrapping `math.MaxUint32` past `0`, and rolling a live slot back to an earlier generation allowed destroying that restored entity to re-issue intermediate generations held by handles from before the rollback.
2. `World.Reset` (`pkg/entities/world.go`) set `w.gens = nil`, so any handle from before `Reset` aliased the next entity spawned into the same slot.

## Decision

`World` now maintains the invariant `len(w.alive) <= len(w.gens)` with non-zero generations in `w.gens`, plus a lazily allocated `floorGens []uint32` slice for rollback restores:

- **`World.Spawn`**: pops from `w.free` as before; when `w.free` is empty, it claims `idx := uint32(len(w.alive))` and reuses `w.gens[idx]` when `int(idx) < len(w.gens)` instead of appending generation 1. Steady-state recycling remains zero-allocation.
- **`World.Reset`**: releases hierarchy storage, component store slices, `w.alive`, `w.free`, and `w.floorGens`, while advancing generations for previously live slots and trimming excess capacity from `w.gens`. `World.Dispose` still drops `w.gens = nil` because a disposed world can never spawn again.
- **`World.Restore`**: computes `gensSize := max(aliveSize, len(w.gens))` for both non-empty and empty snapshots, populating every slot from `w.nextSlotGen(i)` (which advances live slots via `nextGen` wrapping `0 -> 1` and preserves already-bumped dead slots without burning generations on repeated restores). Only `aliveSize` entries are allocated for `tmp.alive` and `tmp.free`, keeping `Each` and `Snapshot` proportional to the restored snapshot rather than historical peak slot count.
- **Rollback generation floor (`floorGens`)**: when `Restore` places a live entity at `es.Generation` where `prevNext := tmp.gens[es.Index]` satisfies `prevNext > es.Generation && prevNext > nextGen(es.Generation)`, `tmp.floorGens[es.Index] = prevNext` is recorded. When that slot next dies (`Destroy`, `Clear`, `Reset`, or another `Restore`), `advanceGen` / `nextSlotGen` advances to at least `floorGens[idx]` and clears the floor. In the common non-rollback case `floorGens` stays `nil`.
- **Snapshot format**: unchanged; this is purely in-memory slot-table state.

## Alternatives considered

- **Document that `World.Reset` invalidates generational handle safety so `Reset` can nil `w.gens`**: rejected because `Reset` is routinely called on level transitions while systems, cameras, or UI still hold stale handles, and silently aliasing new entities causes hard-to-diagnose corruption. Keeping `w.gens` (4 bytes per historical slot) while releasing component stores, hierarchy buffers, `w.alive`, and `w.free` releases almost all memory while preserving the safety contract.
- **Size `tmp.alive` and `tmp.free` to `max(aliveSize, len(w.gens))` in `Restore` and `Reset`**: rejected because after restoring a small snapshot or calling `Reset` on a previously large world, `Each` and `Snapshot` would iterate over all dead tail slots and `Reset` would still retain `alive` and `free` arrays. Allowing `len(w.alive) <= len(w.gens)` gives identical ascending slot-reuse order with smaller allocations and faster scans.
- **Ignore rollback-then-destroy aliasing (`floorGens`)**: rejected because loading an earlier save checkpoint into a running world and then destroying a restored entity is a normal gameplay sequence; without a floor, `Destroy` + `Spawn` would immediately re-issue handles that existed before the checkpoint load.

## Consequences

- **Benefits**: Stale `Entity` handles stay dead across `Reset`, partial `Restore`, empty `Restore`, and rollback-then-destroy flows; generation wraparound at `math.MaxUint32` never emits generation `0`; zero-allocation steady-state `Spawn`/`Destroy` and snapshot JSON encoding are unaffected.
- **Costs**: `World.Reset` retains 4 bytes per previously used slot in `w.gens` until the `World` is garbage-collected or disposed (`Dispose` sets `w.gens = nil`). A rollback `Restore` that lowers a live slot's generation by more than 1 allocates `floorGens` of length `maxIdx + 1` until cleared.
