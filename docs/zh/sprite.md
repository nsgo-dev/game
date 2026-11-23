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
| `Tint` | 一个 `core.Color`，在合成前与帧的红、绿、蓝和透明度通道相乘；零值和不透明白色按原样绘制，其他透明度为 `0` 的着色不绘制任何内容。要让精灵淡出，请降低 `Alpha`：着色渐变到透明黑色时就等于零值，会按原样绘制帧。 |
| `Z` | 批次中的前后顺序；`Z` 越大越靠前绘制。 |

着色只改变精灵自身的像素：它通过 NSGo 的 `DrawTintedImageFrom` 绘制，先把图像各通道与着色相乘，再以默认的 source-over 模式合成，因此透明像素不会改变精灵下方已有的内容。`Tint` 可以与 `Alpha`、`Blend` 和 `Mask` 在同一次绘制中组合使用：

```go
hit := sprite.NewSprite(atlas.Frames["slime.png"])
hit.Tint = core.RGB(0xff6060) // 受伤时闪红
ghost := hit
ghost.Tint = core.RGBA(0x80c0ff, 0.5) // 淡蓝色，不透明度减半
```

在 Web 上，跨域的图集图片必须允许匿名 CORS 请求，NSGo 才能为它的像素着色。在 HarmonyOS 上，NSGo 可以为从字节或应用资源加载的图集着色，但着色尚未在真机上验证，而由像素缓冲区或离屏图像构建的图集目前在那里不会绘制。

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
- **绘图状态**：`Draw` 会把 `d` 的变换矩阵、透明度、混合方式和裁剪区域原样恢复成绘制前的样子。
- **绘制调用**：按 Z 顺序相邻、只做了平移、旋转和等比缩放（`ScaleX == ScaleY`，或同时设置两个翻转）、使用默认 `Blend` 且没有自己的 `Mask` 的精灵，合并为一次 NSGo `DrawAtlas`。镜像、非等比缩放、设置了混合方式或遮罩的精灵，以及只有一个精灵的连续段，各自调用一次 `DrawImageFrom`；旋转、镜像、半透明、混合或遮罩时包在 `Save`/`Restore` 中。
- **连续段中的透明度**：完全不透明的连续段沿用当前绘图的透明度，与单个精灵相同。半透明的连续段在 `Save` 与 `Restore` 之间先把透明度设为 `1`，再使用每个精灵自己的 `Alpha`，因此 `Alpha` 仍然替换绘图的透明度。从不透明精灵切换到半透明精灵时开始新的连续段。

绘制结果与每个精灵一次 `DrawImageFrom` 相同：测试回放两种记录并逐一比较绘制出的每个矩形，模糊测试对随机批次做同样的比较，`gametest.RenderOps` 渲染两者得到完全相同的像素。

### 使用 `DrawAtlas` 前后的开销

`BenchmarkBatchRecord` 记录一帧 1,000 个和 5,000 个移动中的精灵（其中五分之一带旋转和缩放），分别用每个精灵一次 `DrawImageFrom`（`per-sprite`）和 `DrawAtlas`（`atlas`）。`sprites/60Hz` 表示仅记录这一步能在一帧 16.7 ms 内容纳多少个精灵：

```sh
cd packages/game
go tool nsgo test ./sprite -- -run '^$' -bench BatchRecord -benchmem -cpu 1 -benchtime 2s
```

2026-11-23 在一台共用的 24 核 x86-64 Linux 机器上的读数；耗时随机器负载变化，分配次数不变：

| 精灵数 | 每帧耗时 | 每帧分配次数 | 每个 60 Hz 帧可容纳的精灵数 |
| ---: | ---: | ---: | ---: |
| 1,000 | 219 µs → 81 µs | 1,000 → 3 | 76,000 → 206,400 |
| 5,000 | 926 µs → 586 µs | 5,000 → 3 | 90,000 → 142,300 |

记录只是一帧中较小的部分，平台仍要逐个绘制精灵。NSGo 自己的[大量精灵对比](https://github.com/nsgo-dev/nsgo/blob/main/docs/architecture/performance.md#canvas-atlas-batching)把 `DrawAtlas` 与每个精灵一次 `DrawImageFrom` 相比：5,000 个精灵在 Pixel 3 XL 上从每秒 29.3 帧升到 39.9 帧，在无头 Chromium 中从 55.5 帧升到 60.0 帧，在 macOS 上从 6.6 帧升到 7.8 帧；1,000 个精灵在 Android 和 Chromium 上本来就能达到每秒 60 帧。

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

批次既会记录 `DrawImageFrom`，也会把精灵记录在 `DrawAtlas` 中，所以应当用 `gametest.ImageDraws(ops)` 检查绘制了什么，而不是统计 `DrawImage` 指令的数量。它返回每个绘制出的图片矩形，包括源矩形、在画布上的四个角、透明度和混合方式，与记录方式无关。`gametest.AssertSameImages(t, got, want)` 用同样的方式比较两份记录，例如把批次与手写的 `DrawImageFrom` 调用相比；`gametest.RenderOps` 也能渲染 `DrawAtlas`。
