# spatial

`game/spatial` 无需逐个检测即可找出某个区域附近或视口内的 2D 对象：2D 宽相空间索引（`Hash`、`Quadtree`）以及视口精灵裁剪（`CullBatch`、`BatchCuller`、`Cull`），预热后零分配。对象池见 [mempool](mempool.md)。

```go
import "github.com/nsgo-dev/game/pkg/spatial"
```

## 2D 空间索引（`Hash`、`Quadtree`）

用 O(k) 的区域查询、点查询与重叠配对替代 O(n²) 暴力扫描。两种索引都会复用内部桶/节点，并把结果写入调用方传入的切片，实现稳态查询零分配：

```go
grid := spatial.NewHash[uint32](64) // 单元格尺寸通常设为常见实体尺寸的 1~2 倍
grid.Insert(id, entityBounds)
grid.Update(id, movedBounds)

// 复用切片进行零分配区域查询：
hits = grid.Query(cameraView, hits[:0])

// 去重后的宽相重叠实体对：
grid.QueryPairs(func(a, b uint32) {
	resolveNarrowPhase(a, b)
})
```

实体尺寸相近或世界无固定边界时优先使用 `Hash[ID]`；实体尺寸差异较大或集中在已知世界矩形内时使用 `NewQuadtree[ID](worldBounds, maxItems, maxDepth)`。两者均提供 `Insert`、`Update`、`Remove`、`Clear`、`Len`、`Query` 与 `QueryPoint`。

## 视口裁剪（`CullBatch`、`BatchCuller`、`Cull`）

在向 `core.Drawing` 提交绘制命令前先剔除屏幕外的精灵：

```go
var culler spatial.BatchCuller

// 在 Canvas.Draw 中：
culler.Draw(spriteBatch, cameraViewBounds, d)
```

- `BatchCuller.Draw(batch, view, d)` 复用内部临时 `sprite.Batch`，做到每帧零分配；若只需一次性便捷调用，可使用 `spatial.CullBatch(batch, view, d)`。
- 使用 `spatial.CullSprites(view, src, dst[:0])`、`spatial.SpriteBounds(s)`、`spatial.Intersects(a, b)` 或泛型 `spatial.Cull(view, items, boundsOf, dst[:0])` 裁剪任意 2D 世界对象。
