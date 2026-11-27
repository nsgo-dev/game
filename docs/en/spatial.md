# spatial

`game/spatial` finds the 2D objects near an area or inside the view without testing every one: broad-phase spatial indices (`Hash`, `Quadtree`) and viewport sprite culling (`CullBatch`, `BatchCuller`, `Cull`), allocation-free once warm. The object pools are in [mempool](mempool.md).

```go
import "github.com/nsgo-dev/game/pkg/spatial"
```

## 2D spatial indices (`Hash`, `Quadtree`)

Replace O(n²) scans with O(k) area queries, point queries, and collision pair generation. Both indices reuse internal buckets/nodes and accept caller-owned output slices so steady-state queries allocate zero bytes:

```go
grid := spatial.NewHash[uint32](64) // cell size ~1-2x typical entity size
grid.Insert(id, entityBounds)
grid.Update(id, movedBounds)

// Zero-alloc area query reusing caller slice:
hits = grid.Query(cameraView, hits[:0])

// Deduplicated overlapping broad-phase pairs:
grid.QueryPairs(func(a, b uint32) {
	resolveNarrowPhase(a, b)
})
```

Use `Hash[ID]` for uniform entity sizes and unbounded or scrolling worlds; use `NewQuadtree[ID](worldBounds, maxItems, maxDepth)` when entities vary widely in size or cluster densely inside a known world rectangle. Both expose `Insert`, `Update`, `Remove`, `Clear`, `Len`, `Query`, and `QueryPoint`.

## Viewport culling (`CullBatch`, `BatchCuller`, `Cull`)

Skip drawing off-screen sprites before submitting draw commands to `core.Drawing`:

```go
var culler spatial.BatchCuller

// Inside Canvas.Draw:
culler.Draw(spriteBatch, cameraViewBounds, d)
```

- `BatchCuller.Draw(batch, view, d)` reuses an internal scratch `sprite.Batch` for zero per-frame allocations (use the one-liner `spatial.CullBatch(batch, view, d)` when convenience matters more than reusing scratch memory).
- Use `spatial.CullSprites(view, src, dst[:0])`, `spatial.SpriteBounds(s)`, `spatial.Intersects(a, b)`, or generic `spatial.Cull(view, items, boundsOf, dst[:0])` to cull arbitrary world objects.
