# mempool

`game/mempool` provides deterministic object pools for hot game loops (`Pool`, `Arena`, `SlicePool`) that allocate nothing once warm. The spatial indices and culling helpers are in [spatial](spatial.md).

```go
import "github.com/nsgo-dev/game/pkg/mempool"
```

## Deterministic object pools (`Pool`, `Arena`, `SlicePool`)

Unlike `sync.Pool`, `mempool` pools never drop entries on garbage collection, keeping frame times predictable across desktop, mobile, and WebAssembly targets:

```go
// Pointer pool with explicit reset callback:
bullets := mempool.NewPool(128, func(b *Bullet) { *b = Bullet{} })
b := bullets.Acquire()
b.X, b.Y = playerX, playerY
bullets.Release(b)

// Dense, cache-friendly Arena with generational Handles (stale handles safely return false):
enemies := mempool.NewArena[Enemy](64, nil)
h, e := enemies.Alloc()
e.HP = 100
for handle, item := range enemies.All() {
	stepEnemy(handle, item, dt)
}
enemies.Remove(h)

// Reusable scratch slices:
scratch := mempool.NewSlicePool[uint32](16)
ids := scratch.Acquire(64)
defer scratch.Release(ids)
```

| Type | Best for | Key methods |
| --- | --- | --- |
| `Pool[T]` | Stable pointers to pooled structs (particles, projectiles) | `Acquire`, `Release`, `Prewarm`, `Active`, `Free`, `Cap`, `Reset` |
| `Arena[T]` | Dense iteration + O(1) swap-remove with generational `Handle`s | `Add`, `Alloc`, `Get`, `Has`, `Remove`, `Slice`, `ForEach`, `All`, `Reset` |
| `SlicePool[T]` | Per-frame temporary query or sorting buffers | `Acquire(minCap)`, `Release(s)`, `Free`, `Reset` |
