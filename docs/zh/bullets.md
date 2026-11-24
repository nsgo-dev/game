# bullets

`game/bullets` 为射击游戏生成、移动并绘制弹幕：用 YAML 定义的环形、螺旋和瞄准连发，子弹取自 `perf.Pool`，通过一个 `sprite.Batch` 绘制。

```go
import "github.com/nsgo-dev/game/packages/game/bullets"
```

## 弹幕文件

弹幕文件是带有 `version` 和具名 `patterns` 列表的 YAML：

```yaml
version: 1
patterns:
  - name: bloom
    kind: ring
    frame: orb
    count: 24
    speed: 90
    spin: 7.5
    shots: 6
    interval: 400ms
    lifetime: 6s
    radius: 4
  - name: vortex
    kind: spiral
    frame: orb
    count: 5
    speed: 140
    speedJitter: 8
    angleJitter: 2.5
    spin: 11
    shots: 0
    interval: 50ms
    accel: -20
    turn: 15
    radius: 3
  - name: volley
    kind: aimed
    frame: dart
    count: 7
    speed: 220
    spread: 45
    shots: 3
    delay: 1s
    interval: 150ms
    lifetime: 3s
    radius: 2.5
    faceHeading: true
```

`Parse` 读取文件，拒绝未知字段、不等于 `FormatVersion`（1）的 `version`、多余的 YAML 文档和无效的弹幕。`Encode` 以 2 空格缩进、固定的字段顺序写回文件，并省略为零的可选字段，因此同一文件总是编码为相同的字节，加载再保存不会改变任何内容。`File.Pattern(name)` 按名称查找弹幕。

角度以度为单位，从屏幕 +X 方向顺时针计算；速度单位是点每秒；时长是 Go 的时长写法（`50ms`、`2s`）。

| 字段 | 含义 |
| --- | --- |
| `name` | 唯一且不能为空 |
| `kind` | `ring`、`spiral` 或 `aimed` |
| `frame` | 每颗子弹绘制的图集帧；为空时子弹照常移动和碰撞，但不绘制 |
| `count` | 每次射击发出的子弹数，1 到 `MaxCount`（4096） |
| `speed`、`speedJitter` | 初始速度，每颗子弹随机增减 `[-speedJitter, speedJitter)`，不低于 0 |
| `angle`、`angleJitter` | 一次射击第一颗子弹的方向（`aimed` 中为相对目标方向的偏转），每颗子弹随机偏转 `[-angleJitter, angleJitter)` |
| `spread` | `aimed` 连发从第一颗到最后一颗覆盖的弧度；仅用于 `aimed`，`count` 大于 1 时必须大于 0 |
| `spin` | 每次射击比上一次多转的度数；`spiral` 必须不为 0 |
| `shots`、`delay`、`interval` | 射击次数（0 表示一直射击直到停止）、首次射击前的时间、两次射击的间隔（至少 `1ms`，`shots` 为 1 时可为 0） |
| `lifetime` | 每颗子弹的存活时间；0 表示存活到离开场地边界为止 |
| `accel`、`turn` | 每秒的速度变化（点每秒，减速到 0 的子弹停住），以及每秒的路径弯曲度数 |
| `radius` | 命中半径，也用来判断子弹是否已离开边界 |
| `faceHeading` | 让精灵朝向子弹移动的方向 |
| `z` | 在场地批次中的精灵顺序 |

- `ring` 将 `count` 颗子弹均匀分布在一整圈上，第一颗朝向 `angle`，每次射击多转 `spin`。
- `spiral` 是带 `spin` 的环形：`interval` 较短时，`count` 颗子弹各自画出一条旋臂。
- `aimed` 将 `count` 颗子弹均匀分布在 `spread` 内，以发射器指向目标的方向为中心，并按 `angle` 和 `spin` 偏转。`count` 为奇数且没有 `angle` 和 `spin` 时，中间那颗子弹直接射向目标；只有一颗子弹时，它射向按 `angle` 偏转后的目标方向。

## Field 与 Emitter

`Field` 持有存活的子弹。`NewField(atlas, capacity)` 在池中预留 `capacity` 颗子弹，因此生成不超过这个数量的子弹时不会分配内存。设置 `Bounds` 可移除命中圆已完全离开场地的子弹，设置 `Limit` 可限制场地持有的子弹数量。

`NewEmitter(field, pattern, seed)` 把弹幕的副本发射到场地中，并检查场地的图集中有弹幕的帧。用 `X`、`Y` 移动它，用 `TargetX`、`TargetY` 瞄准，用 `Stopped` 暂停，用 `Reset` 从头开始。停止的发射器的时钟也会停下，因此恢复后它从暂停处继续弹幕，而不会补发错过的射击。

```go
file, err := bullets.Parse(data)
field := bullets.NewField(atlas, 6000)
field.Bounds = math2d.Rect{W: 480, H: 640}
volley, _ := file.Pattern("volley")
boss, err := bullets.NewEmitter(field, volley, seed)

loop := &game.Loop{
	Update: func(dt time.Duration) {
		boss.X, boss.Y = bossX, bossY
		boss.TargetX, boss.TargetY = playerX, playerY
		field.Update(dt) // 先移动已在飞行的子弹
		boss.Update(dt)  // 再发出本步内到期的射击
		field.RemoveFunc(func(b *bullets.Bullet) bool {
			return b.Circle().Overlaps(playerHitbox)
		})
	},
}
// 在 core.Canvas 的 Draw 中：
field.Draw(d)
```

- 每一步先更新场地，再更新其发射器。在一步中途到期的射击，会把子弹放在它们在这一步结束时所处的位置，因此间隔不是整数步时子弹也不会挤在一起。
- 一次 `Update` 最多发出 4096 颗子弹的射击（至少一次射击）。到期的射击更多时（例如卡顿后的一个长步），它发出最晚的那些并跳过较早的，跳过的射击计为已发射，但不生成子弹，也不抽取随机数。
- `Field.Bullets()` 按从旧到新列出存活的子弹，每颗带有位置、`Speed`、`Heading`（弧度）、`Radius`、`Age`、`Lifetime`、`Pattern` 名称、`Shot` 序号和 `Sprite`。子弹归场地所有：不要在下一次 `Update`、`RemoveFunc` 或 `Clear` 之后继续持有指针，因为场地会复用它。
- `Field.Update` 保持留下的子弹的顺序，`Field.Draw` 按 `Z` 从低到高绘制，`Z` 相同时先画较旧的。
- 超过场地 `Limit` 的射击只生成放得下的子弹，但仍会抽取相同的随机数。

## 确定性

每个发射器从自己的 `random.Stream` 抽取随机偏差。两个弹幕和种子相同的发射器，以相同的 `dt` 和目标移动与步进时，会在相同位置生成相同的子弹：在同一构建目标上结果逐位相同，包内测试用 `random.AssertDeterministic` 检查这一点。浮点结果在不同 CPU 架构之间可能不同，因此跨平台的帧同步游戏应比较输入，而不是子弹位置。

## 性能

`BenchmarkFrame5000` 运行一个稳定 5000 颗子弹场景中的一帧（60 fps）：十个带随机偏差和弯曲路径的螺旋发射器，其中一半的精灵会旋转，子弹在这一帧中移动、过期、生成并绘制到 `core.Drawing`。在桌面 Ryzen 9 3900X 上约需 0.63 ms，而一帧有 16.7 ms；单独移动 5000 颗子弹（`BenchmarkUpdate5000`）约需 0.14 ms。CI 的性能基线（`benchmarks/baseline.yaml`）会让在 CI 运行器上一帧超过 6.25 ms 的改动失败。

这测量的是一帧的 CPU 工作：模拟子弹并记录它们的绘制操作。它不测量平台渲染器把这些操作画到屏幕上所需的时间，那部分属于 NSGo，并取决于 GPU。

- 池预热后，移动和发射不分配内存。
- `sprite.Batch` 通过 NSGo 的 `Drawing.DrawAtlas` 按段绘制子弹，它会复制每一段，并且每段分配三个值，与段中子弹数量无关。
