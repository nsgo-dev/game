# mempool

`game/mempool` 为高频游戏主循环提供确定性的对象池（`Pool`、`Arena`、`SlicePool`），预热后零分配。空间索引与裁剪辅助见 [spatial](spatial.md)。

```go
import "github.com/nsgo-dev/game/pkg/mempool"
```

## 确定性对象池（`Pool`、`Arena`、`SlicePool`）

与 `sync.Pool` 不同，`mempool` 的对象池不会在 GC 期间丢弃缓存对象，从而在桌面、移动端和 WebAssembly 上保持稳定的帧耗时：

```go
// 带重置回调的指针池：
bullets := mempool.NewPool(128, func(b *Bullet) { *b = Bullet{} })
b := bullets.Acquire()
b.X, b.Y = playerX, playerY
bullets.Release(b)

// 缓存友好的紧凑数组 Arena，使用带代数校验的 Handle（过期句柄安全返回 false）：
enemies := mempool.NewArena[Enemy](64, nil)
h, e := enemies.Alloc()
e.HP = 100
for handle, item := range enemies.All() {
	stepEnemy(handle, item, dt)
}
enemies.Remove(h)

// 可复用的临时切片池：
scratch := mempool.NewSlicePool[uint32](16)
ids := scratch.Acquire(64)
defer scratch.Release(ids)
```

| 类型 | 适用场景 | 核心方法 |
| --- | --- | --- |
| `Pool[T]` | 需要稳定指针的池化结构体（弹幕、粒子） | `Acquire`、`Release`、`Prewarm`、`Active`、`Free`、`Cap`、`Reset` |
| `Arena[T]` | 紧凑连续遍历 + O(1) 交换删除 + 带代数 `Handle` | `Add`、`Alloc`、`Get`、`Has`、`Remove`、`Slice`、`ForEach`、`All`、`Reset` |
| `SlicePool[T]` | 每帧临时查询或排序缓冲区 | `Acquire(minCap)`、`Release(s)`、`Free`、`Reset` |
