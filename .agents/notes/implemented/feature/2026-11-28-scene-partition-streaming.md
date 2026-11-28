# Agent Note: Scene partition cell math

Status: implemented

## Problem

A scene file loads whole, and #1728 asks for one large level to stream in grid cells around any number of sources, as Unreal Engine's World Partition does. Every later piece of that work (a `partition:` section in the scene file, `game partition` writing per-cell fragments, a runtime streamer, `game check` budgets) needs the same answer to two questions: which cells does an object belong to, and which cells should be loaded or kept around a set of sources. If the build step and the runtime streamer computed these separately they could disagree, and any map-ordered output would make generated cells differ between runs.

## Decision

`pkg/sceneformat/partition.go` adds the shared cell math only:

- `Partition{CellSize, LoadRadius, UnloadRadius}` with `Validate` requiring a positive finite cell size and `0 <= LoadRadius <= UnloadRadius`. The unload radius being at least the load radius is the hysteresis that stops a source on a cell boundary from loading and unloading the same cell every frame.
- `Cells(CellObject)` assigns an object to every cell its bounds rectangle touches, in row-major order. The far edge is moved down by one ulp (`math.Nextafter`) so an edge lying exactly on a boundary does not claim the next cell, and the upper corner is clamped to the lower one so a zero-size object on a boundary still gets the cell containing it. `AlwaysLoaded` objects, invalid partitions and non-finite or negative bounds return nil.
- `Needed(sources)` and `Retained(sources)` return the cells within the load and unload radius of any `StreamingSource`, each source's `RadiusScale` multiplying the radius, sorted by Y then X so the result does not depend on source order or map iteration. A cell in `Retained` but not `Needed` is kept if loaded and not loaded otherwise.

Non-finite inputs are rejected or skipped rather than converted, because `int(math.Floor(NaN))` is implementation-defined in Go and would produce arbitrary (often enormous) loop bounds.

## Alternatives considered

### Assign objects by origin

Strongest argument: one cell per object is cheaper and simpler to store.

Rejected: a long bridge or building would be loaded only from its origin cell and vanish while the player stands on its other end, which is exactly the failure the issue's design notes call out.

### Keep `Priority` and `Wait` on `StreamingSource` now

Strongest argument: the issue lists them as source properties, and adding them later grows the struct.

Rejected for this change: nothing here orders loads or reports a waiting source, so the fields would be accepted and silently ignored. They belong with the runtime streamer that gives them meaning.

## Consequences

- The build step and a future runtime streamer can share one definition of cell membership and of the load and keep sets.
- Still not delivered for #1728: the `partition:` scene section and per-object `grid`, coarser grid levels for objects larger than a cell, `game partition`, the runtime `Streamer` with budgets, load order by priority and distance and `wait` reporting, data layers, `game check` diagnostics and the 2D example.
