# sprite

`game/sprite` 负责加载打包好的精灵图集、按固定步长播放帧动画，并按 Z 顺序将精灵批次绘制到 NSGo 的 `core.Canvas` 中。

```go
import "github.com/nsgo-dev/game/packages/game/sprite"
```

## 从 TexturePacker 和 Aseprite 加载图集

`Atlas` 把一张打包图片（`Image`）、按名称索引的帧（`Frames`）和可选的按名称索引的动画（`Animations`）组合在一起。

`LoadAtlas(img, data)` 读取以下工具导出的 JSON：

- **TexturePacker**：“JSON (Hash)” 和 “JSON (Array)”
- **Aseprite**：`json-hash` 和 `json-array`（开启 frame tags 导出后会自动填充 `Animations`）

把打包好的 PNG 放到 `resources/` 下供 NSGo 以 `"asset:..."` 加载，用 `//go:embed` 将 JSON 直接嵌入二进制：

```go
//go:embed resources/hero.json
var heroJSON []byte

atlas, err := sprite.LoadAtlas(core.Src("asset:hero.png"), heroJSON)
if err != nil {
	return err
}
```

### 导出设置与缩放

- **关闭旋转**：`LoadAtlas` 会拒绝被打包工具旋转过的帧并返回错误。
- **帧名称唯一**：重复的帧名称会返回错误。
- **高倍图（`meta.scale`）**：所有帧矩形、偏移量和原始尺寸都会除以 `meta.scale`（默认为 `1`）。以 `"scale": "2"` 导出的 2x 图集会换算为 1x 点坐标，与 NSGo 的点坐标系一致。

### 手写图集

对于简单的规则精灵表、原型或测试，可以直接手写 `Atlas` 字面量，不需要 JSON：

```go
sheet := &sprite.Atlas{
	Image: core.Src("asset:sheet.png"),
	Frames: map[string]sprite.Frame{
		"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		"coin": {Name: "coin", X: 16, Y: 0, W: 8, H: 10, OffsetX: 4, OffsetY: 3, Width: 16, Height: 16},
	},
}
```

## 裁剪帧（Trimmed frames）

打包工具通常会裁掉每帧四周的透明边缘以节省图集空间。`Frame` 同时记录图集里的裁剪矩形和未裁剪时的原始画框：

```go
type Frame struct {
	Name             string
	X, Y, W, H       float32 // 图集图片中的裁剪矩形，单位为点
	OffsetX, OffsetY float32 // 裁剪矩形在未裁剪画框内的偏移
	Width, Height    float32 // 未裁剪时的帧尺寸（未裁剪时等于 W、H）
}
```

`Sprite` 的 `OriginX`、`OriginY`、`FlipX`、`FlipY`、旋转和缩放都以未裁剪画框（`Width`、`Height`）为准。因此裁剪透明边缘绝不会让角色在切换动画帧时脚底漂移或错位。

## Animation

`Animation` 依次播放一组 `AnimationFrame`，每帧都有自己的 `Duration`，在 `game.Loop.Update` 中通过 `Update(dt)` 推进：

```go
type Animation struct {
	Frames []AnimationFrame
	Loop   bool
	// ...
}

type AnimationFrame struct {
	Frame    Frame
	Duration time.Duration
}
```

`Animation` 是一个保存播放状态（当前帧索引、已播放时长和完成标志）的值类型。从 `atlas.Animations` 复制一份给每个实体，就能各自独立播放：

```go
walk := atlas.Animations["walk"]
attack := atlas.Animations["attack"]
```

`Animation` 的方法：

- `Update(dt)`：向前推进 `dt`，跨过 `dt` 所覆盖的所有帧（乃至多个完整循环）。
- `Frame()`：返回当前显示的 `Frame`（无帧时返回零值 `Frame`）。
- `Index()`：返回当前帧在 `Frames` 中的索引。
- `Done()`：报告不循环的动画是否已经把最后一帧显示满该帧的时长。
- `Reset()`：重置到第 0 帧并清除 `Done()`。

### Aseprite 帧标签

加载 Aseprite 导出文件时，`meta.frameTags` 中的每个标签都会成为 `atlas.Animations` 中的一项：

- 每帧时长取自导出数据中的毫秒数 `duration`（标签覆盖的每一帧都必须有正的时长）。
- 支持的播放方向：`"forward"`（默认）、`"reverse"`、`"pingpong"` 和 `"pingpong_reverse"`。
- 未设置 `repeat`（或 `repeat: 0`）时 `Loop` 为 `true`；设置了 `repeat: N` 时展开为播放 `N` 趟的非循环动画（`Loop: false`）。往返（ping-pong）标签把每一趟计作一次重复，且折返端点的帧不会连续显示两次。

### 手写动画

对于不带帧标签的 TexturePacker 图集，可以从 `atlas.Frames` 手动组装 `Animation`：

```go
spin := sprite.Animation{
	Loop: true,
	Frames: []sprite.AnimationFrame{
		{Frame: atlas.Frames["coin_0.png"], Duration: 100 * time.Millisecond},
		{Frame: atlas.Frames["coin_1.png"], Duration: 100 * time.Millisecond},
		{Frame: atlas.Frames["coin_2.png"], Duration: 100 * time.Millisecond},
	},
}
```

## Sprite 摆放与变换

`Sprite` 把一帧放到 `core.Drawing` 中。它的零值 `ScaleX`、`ScaleY` 和 `Alpha` 都是 0，所以不会绘制任何内容；用 `sprite.NewSprite(frame)` 可以得到原始尺寸（`1, 1`）、完全不透明（`1`）的精灵：

```go
hero := sprite.NewSprite(walk.Frame())
hero.OriginX = hero.Frame.Width / 2
hero.OriginY = hero.Frame.Height // 把原点锚定在角色脚底
hero.X, hero.Y = 120, 200
```

`Sprite` 的字段：

| 字段 | 含义 |
| --- | --- |
| `Frame` | 当前要绘制的 `Frame`；零值帧（`W <= 0` 或 `H <= 0`）不绘制。 |
| `X`、`Y` | 精灵原点在绘图中的坐标，单位为点。 |
| `OriginX`、`OriginY` | 未裁剪帧内的锚点，从左上角起算。 |
| `Rotation` | 绕原点顺时针旋转的弧度。 |
| `ScaleX`、`ScaleY` | 绕原点的缩放比例；负值表示绕原点镜像；`0` 不绘制。 |
| `FlipX`、`FlipY` | 在摆放前将画面在未裁剪画框内左右/上下翻转，原点位置保持不变。 |
| `Alpha` | 不透明度，范围 `[0, 1]`；`<= 0` 不绘制。绘制该精灵时会替换当前绘图的透明度。 |
| `Z` | 批次中的前后顺序；`Z` 越大越靠前绘制。 |

精灵暂时还不支持着色（[#29](https://github.com/nsgo-dev/game/issues/29)）。

## Batch 与 Z 排序

`Batch` 把来自同一个 `Atlas` 的一组精灵绘制到 `*core.Drawing` 中：

```go
batch := &sprite.Batch{
	Atlas:   atlas,
	Sprites: []*sprite.Sprite{&shadow, &hero, &weapon},
}

// 在 core.Canvas.Draw 中：
batch.Draw(d)
```

- **排序**：`Draw` 在内部临时切片上按 `Z` 从小到大做稳定排序（`Z` 最小的先画，`Z` 相同的保持 `Sprites` 中的顺序）。`batch.Sprites` 本身的顺序不会被修改。
- **绘图状态**：`Draw` 会把 `d` 的变换矩阵和透明度恢复成绘制前的样子，仅对发生旋转、镜像或半透明的精灵使用 `Save`/`Restore`。无旋转、无镜像且完全不透明的精灵会直接绘制，不额外压入变换。
- **绘制调用**：目前每个可见精灵调用一次 `DrawImageFrom`；改用 NSGo 的批量 `DrawAtlas` 见 [#28](https://github.com/nsgo-dev/game/issues/28)。

## 使用 nsgotest 测试

因为 `Batch.Draw` 记录的是 `core.Canvas` 上普通的 `core.DrawOp` 指令，你可以用 `nsgotest` 无头检查批次实际绘制了什么：

```go
app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
	return core.Canvas{
		TestID: "stage",
		Style:  core.Style{Width: core.Pt(320), Height: core.Pt(240)},
		Draw: func(d *core.Drawing, _ core.Size) {
			batch.Draw(d)
		},
	}
})
ops := app.Find("stage").Ops
if len(ops) == 0 {
	t.Fatal("expected sprite draw operations")
}
```
