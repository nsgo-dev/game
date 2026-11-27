# perf

`game/perf` provides deterministic, zero-steady-state-allocation primitives for hot game loops: reusable object pools (`Pool`, `Arena`, `SlicePool`), 2D broad-phase spatial indices (`SpatialHash`, `Quadtree`), and viewport sprite culling (`CullBatch`, `BatchCuller`, `Cull`).

```go
import "github.com/nsgo-dev/game/pkg/perf"
```

## Deterministic object pools (`Pool`, `Arena`, `SlicePool`)

Unlike `sync.Pool`, `perf` pools never drop entries on garbage collection, keeping frame times predictable across desktop, mobile, and WebAssembly targets:

```go
// Pointer pool with explicit reset callback:
bullets := perf.NewPool(128, func(b *Bullet) { *b = Bullet{} })
b := bullets.Acquire()
b.X, b.Y = playerX, playerY
bullets.Release(b)

// Dense, cache-friendly Arena with generational Handles (stale handles safely return false):
enemies := perf.NewArena[Enemy](64, nil)
h, e := enemies.Alloc()
e.HP = 100
for handle, item := range enemies.All() {
	stepEnemy(handle, item, dt)
}
enemies.Remove(h)

// Reusable scratch slices:
scratch := perf.NewSlicePool[uint32](16)
ids := scratch.Acquire(64)
defer scratch.Release(ids)
```

| Type | Best for | Key methods |
| --- | --- | --- |
| `Pool[T]` | Stable pointers to pooled structs (particles, projectiles) | `Acquire`, `Release`, `Prewarm`, `Active`, `Free`, `Cap`, `Reset` |
| `Arena[T]` | Dense iteration + O(1) swap-remove with generational `Handle`s | `Add`, `Alloc`, `Get`, `Has`, `Remove`, `Slice`, `ForEach`, `All`, `Reset` |
| `SlicePool[T]` | Per-frame temporary query or sorting buffers | `Acquire(minCap)`, `Release(s)`, `Free`, `Reset` |

## 2D spatial indices (`SpatialHash`, `Quadtree`)

Replace O(n²) scans with O(k) area queries, point queries, and collision pair generation. Both indices reuse internal buckets/nodes and accept caller-owned output slices so steady-state queries allocate zero bytes:

```go
grid := perf.NewSpatialHash[uint32](64) // cell size ~1-2x typical entity size
grid.Insert(id, entityBounds)
grid.Update(id, movedBounds)

// Zero-alloc area query reusing caller slice:
hits = grid.Query(cameraView, hits[:0])

// Deduplicated overlapping broad-phase pairs:
grid.QueryPairs(func(a, b uint32) {
	resolveNarrowPhase(a, b)
})
```

Use `SpatialHash[ID]` for uniform entity sizes and unbounded or scrolling worlds; use `NewQuadtree[ID](worldBounds, maxItems, maxDepth)` when entities vary widely in size or cluster densely inside a known world rectangle. Both expose `Insert`, `Update`, `Remove`, `Clear`, `Len`, `Query`, and `QueryPoint`.

## Viewport culling (`CullBatch`, `BatchCuller`, `Cull`)

Skip drawing off-screen sprites before submitting draw commands to `core.Drawing`:

```go
var culler perf.BatchCuller

// Inside Canvas.Draw:
culler.Draw(spriteBatch, cameraViewBounds, d)
```

- `BatchCuller.Draw(batch, view, d)` reuses an internal scratch `sprite.Batch` for zero per-frame allocations (use the one-liner `perf.CullBatch(batch, view, d)` when convenience matters more than reusing scratch memory).
- Use `perf.CullSprites(view, src, dst[:0])`, `perf.SpriteBounds(s)`, `perf.Intersects(a, b)`, or generic `perf.Cull(view, items, boundsOf, dst[:0])` to cull arbitrary world objects.
