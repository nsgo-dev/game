# perf

`game/perf` 为高频游戏主循环提供确定性、稳态零分配的性能基础件：对象池（`Pool`、`Arena`、`SlicePool`）、2D 宽相空间索引（`SpatialHash`、`Quadtree`）以及视口精灵裁剪（`CullBatch`、`BatchCuller`、`Cull`）。

```go
import "github.com/nsgo-dev/game/packages/game/perf"
```

## 确定性对象池（`Pool`、`Arena`、`SlicePool`）

与 `sync.Pool` 不同，`perf` 的对象池不会在 GC 期间丢弃缓存对象，从而在桌面、移动端和 WebAssembly 上保持稳定的帧耗时：

```go
// 带重置回调的指针池：
bullets := perf.NewPool(128, func(b *Bullet) { *b = Bullet{} })
b := bullets.Acquire()
b.X, b.Y = playerX, playerY
bullets.Release(b)

// 缓存友好的紧凑数组 Arena，使用带代数校验的 Handle（过期句柄安全返回 false）：
enemies := perf.NewArena[Enemy](64, nil)
h, e := enemies.Alloc()
e.HP = 100
for handle, item := range enemies.All() {
	stepEnemy(handle, item, dt)
}
enemies.Remove(h)

// 可复用的临时切片池：
scratch := perf.NewSlicePool[uint32](16)
ids := scratch.Acquire(64)
defer scratch.Release(ids)
```

| 类型 | 适用场景 | 核心方法 |
| --- | --- | --- |
| `Pool[T]` | 需要稳定指针的池化结构体（弹幕、粒子） | `Acquire`、`Release`、`Prewarm`、`Active`、`Free`、`Cap`、`Reset` |
| `Arena[T]` | 紧凑连续遍历 + O(1) 交换删除 + 带代数 `Handle` | `Add`、`Alloc`、`Get`、`Has`、`Remove`、`Slice`、`ForEach`、`All`、`Reset` |
| `SlicePool[T]` | 每帧临时查询或排序缓冲区 | `Acquire(minCap)`、`Release(s)`、`Free`、`Reset` |

## 2D 空间索引（`SpatialHash`、`Quadtree`）

用 O(k) 的区域查询、点查询与重叠配对替代 O(n²) 暴力扫描。两种索引都会复用内部桶/节点，并把结果写入调用方传入的切片，实现稳态查询零分配：

```go
grid := perf.NewSpatialHash[uint32](64) // 单元格尺寸通常设为常见实体尺寸的 1~2 倍
grid.Insert(id, entityBounds)
grid.Update(id, movedBounds)

// 复用切片进行零分配区域查询：
hits = grid.Query(cameraView, hits[:0])

// 去重后的宽相重叠实体对：
grid.QueryPairs(func(a, b uint32) {
	resolveNarrowPhase(a, b)
})
```

实体尺寸相近或世界无固定边界时优先使用 `SpatialHash[ID]`；实体尺寸差异较大或集中在已知世界矩形内时使用 `NewQuadtree[ID](worldBounds, maxItems, maxDepth)`。两者均提供 `Insert`、`Update`、`Remove`、`Clear`、`Len`、`Query` 与 `QueryPoint`。

## 视口裁剪（`CullBatch`、`BatchCuller`、`Cull`）

在向 `core.Drawing` 提交绘制命令前先剔除屏幕外的精灵：

```go
var culler perf.BatchCuller

// 在 Canvas.Draw 中：
culler.Draw(spriteBatch, cameraViewBounds, d)
```

- `BatchCuller.Draw(batch, view, d)` 复用内部临时 `sprite.Batch`，做到每帧零分配；若只需一次性便捷调用，可使用 `perf.CullBatch(batch, view, d)`。
- 使用 `perf.CullSprites(view, src, dst[:0])`、`perf.SpriteBounds(s)`、`perf.Intersects(a, b)` 或泛型 `perf.Cull(view, items, boundsOf, dst[:0])` 裁剪任意 2D 世界对象。
