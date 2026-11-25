# Agent Note: Influence maps in game/ai

Status: implemented

## Problem

Strategy and tactics games built on `game/ai` and `game/rts` need spatial reasoning over grids: measuring where each side is strong, locating contested front lines, and steering units toward low-threat reachable cells. `game/ai` previously provided pathfinding (`AStar`, `NavMesh`), steering, behavior trees, and utility AI scoring, but no spatial influence map layer.

An influence map implementation in `game/ai` must satisfy five constraints simultaneously:

1. Stay strictly deterministic across runs, including incremental updates when sources move, change, or are removed.
2. Perform zero heap allocations per step after warm-up across incremental source moves, spreading, layer combinations, and reachability queries.
3. Respect `game/ai`'s **Stable** API status (`TestStabilityTable` forbids importing Experimental packages such as `game/rts` or `game/debug`).
4. Propagate around blocked `ai.Grid` cells without leaking through walls or cutting diagonal corners.
5. Render a camera-culled debug heatmap over `tilemap.Camera` while compiling out completely under `-tags release`.

## Decision

Implement influence maps in `packages/game/ai/influence.go` with build-tagged heatmap rendering in `packages/game/ai/influence_debug.go` (`//go:build !release`) and `packages/game/ai/influence_release.go` (`//go:build release`).

### 1. Stable-only dependencies and world-coordinate convention

`InfluenceLayer` imports only `game/math2d`, `game/tilemap` (both Stable), and `nsgo/core`. It stores `Origin math2d.Vec2` and `CellSize float32` using the exact same cell-to-world convention as `rts.FlowField` without importing Experimental `game/rts`. `NewInfluenceLayerForGrid(g Grid, origin, cellSize)` sizes the layer directly from any `ai.Grid` (including `rts.FlowField`'s grid or `ai.GridFunc` over a `tilemap.Tilemap`).

### 2. Bit-identical incremental updates via dirty-region re-stamping

Tracked sources registered with `AddSource` live in a generational slot table (`InfluenceSourceID = gen<<32 | idx`). Moving, editing, or removing a source records only the union of old and new cell bounding boxes (`footprintRect`) plus any explicit `MarkDirtyCell` / `MarkDirtyRect` boxes.

On `Update(g)`, `syncBase` stamps a monotonically incrementing generation mark (`dirtyGen`) across the dirty boxes, zeroes only those dirty cells in `base`, and re-stamps active sources whose bounding box overlaps the dirty region in **fixed slot order**. When `values` is synced with `base` (`valuesSynced`), only `dirtyCells` are copied into `values`; if `values` diverged (after `Spread`, `Fill`, `Scale`, etc.), `Update` refreshes `values` from `base` so `Update` always matches `Rebuild` bit-for-bit.

### 3. Signed obstacle-aware spreading with decay and momentum

`Spread(g, opts)` snapshots `g.Walkable` into a reusable `[]uint8` buffer, syncs any dirty tracked sources into `base` without clobbering previous `values`, and runs double-buffered passes (`values` / `scratch`) over walkable cells. Each pass takes `bestPos` and `bestNeg` independently across the anchor and walkable neighbors (4-way or 8-way with no corner-cutting across blocked cardinal neighbors, using `(1 - Decay)` for cardinal steps and `(1 - Decay)^sqrt(2)` for diagonal steps), sets `target = bestPos + bestNeg`, and blends `prev*Momentum + target*(1 - Momentum)`. Separating `bestPos` and `bestNeg` keeps propagation commutative and deterministic while handling both unsigned (threat/control) and signed layers.

### 4. Zero-allocation combinations, queries, and build-tagged heatmap

- Layer algebra (`Add`, `Subtract`, `Multiply`, `Combine`, `AddScaled`, `Scale`, `Abs`, `Clamp`, `Normalize`, `CopyFrom`) reuses pre-sized `values` and `scratch` slices and safely handles self-aliasing.
- `HighestInRadius` and `LowestInRadius` support walkability and BFS reachability (`InfluenceQuery{Grid, Reachable, Diagonal, ExcludeCenter}`) using reusable generation-stamped `bfsGen` and `bfsQueue` buffers, breaking ties deterministically by squared distance to center, then `Row`, then `Col`.
- `Sample` performs bilinear interpolation anchored on cell centers; `Gradient` evaluates central/one-sided finite differences over the bilinear field so it agrees exactly with `GradientAtCell` at every cell center.
- `DrawHeatmap` (`!release`) culls to `tilemap.Camera.VisibleBounds()` and emits `core.Drawing` fill/grid ops; under `-tags release` it compiles to an empty stub and `InfluenceHeatmapEnabled()` returns `false`.

## Alternatives considered

### Incremental subtract-old / add-new source delta updates

Subtracting a moved source's previous falloff contribution from `base` and adding its new contribution touches only the moved source instead of overlapping sources in the dirty region. Rejected because IEEE-754 `float32` addition is not associative: repeated `(a + b) - a` cycles accumulate rounding drift over thousands of ticks, breaking bit-identical parity between incremental `Update` and full `Rebuild` and making long-running replays diverge depending on move history. Re-stamping only overlapping sources inside the dirty bounding box in fixed slot order keeps incremental updates fast (`O(dirty cells * overlapping sources)`) with zero floating-point drift.

### Placing `DrawHeatmap` in `game/debug` instead of `game/ai`

Moving heatmap drawing into `game/debug` would keep all debug overlays in one package. Rejected because issue #844 specifically calls for a debug heatmap over `tilemap.Camera` compiled out under `-tags release` alongside the influence map API, and `game/ai` already depends only on Stable `game/tilemap` and `nsgo/core`, matching how `game/debug` uses `//go:build !release` / `//go:build release` stubs.

### Computing reachability in `LowestInRadius` via repeated `AStar.Find` calls

Running `AStar` from the center cell to candidate cells in sorted value order would reuse `AStar` directly, but could run dozens of searches when the lowest-value cells are behind walls and would require sorting candidates into a scratch buffer. A single bounded BFS from `center` over walkable cells within `radius` visits each reachable cell once in `O(pi * r^2)` time with zero heap allocations using reusable `bfsGen` / `bfsQueue` buffers.

## Consequences

- **Benefits**: `game/ai` stays Stable with no Experimental dependencies; incremental `MoveSource` + `Update`, `Spread`, layer combinations, and reachable radius queries perform `0 allocs/op` after warm-up; incremental updates match full `Rebuild` bit-for-bit across arbitrary move/edit/remove histories; debug heatmaps add zero runtime cost in `-tags release` builds.
- **Costs**: Layers with very high overlap density (many sources covering the same dirty box) re-evaluate overlapping sources inside that box during `Update`, and callers using `Spread` after moving a source still run a full-grid propagation pass over walkable cells (`O(cols * rows * iters)`).

## Verification

- `packages/game/ai/influence_test.go` verifies linear/exponential/custom falloffs, NaN/Inf sanitization, determinism, wall-blocked and diagonal no-corner-cutting spread, momentum decay with moved/removed tracked sources, 150-step randomized incremental `Update` vs `Rebuild` bit-for-bit parity (including post-`Spread` and post-`Fill` updates), tactical layer combinations (control, tension, vulnerability, overlap, weighted `Combine` with self-aliasing, `Normalize`, `Clamp`, `AddScaled`, `Scale`), least-threatened reachable cell query against `AStar`, bilinear `Sample` and `Gradient` / `GradientAtCell` agreement, `testing.AllocsPerRun == 0` after warm-up across all operations, and `DrawHeatmap` camera culling in default builds plus strict no-op behavior under `-tags release`.
- Verified via `gofmt -l packages/game/ai`, `go tool nsgo test ./ai/...`, `go tool nsgo test ./ai/... -- -tags release`, `go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
