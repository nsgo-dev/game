# sprite

`game/sprite` 负责加载打包好的精灵图集、按固定步长播放帧动画，并按 Z 顺序将精灵批次绘制到 NSGo 的 `core.Canvas` 中。

```go
import "github.com/nsgo-dev/game/pkg/sprite"
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
| `Effect` | 可选的 `*Effect`，只对当前精灵自身的像素应用纯色闪白/剪影、透明度描边、溶解、调色板替换或自定义像素钩子。为 `nil` 或未激活时仍走 `DrawAtlas` 快速路径。 |
| `Z` | 批次中的前后顺序；`Z` 越大越靠前绘制。 |

着色只改变精灵自身的像素：它通过 NSGo 的 `DrawTintedImageFrom` 绘制，先把图像各通道与着色相乘，再以默认的 source-over 模式合成，因此透明像素不会改变精灵下方已有的内容。`Tint` 可以与 `Alpha`、`Blend` 和 `Mask` 在同一次绘制中组合使用：

```go
hit := sprite.NewSprite(atlas.Frames["slime.png"])
hit.Tint = core.RGB(0xff6060) // 受伤时闪红
ghost := hit
ghost.Tint = core.RGBA(0x80c0ff, 0.5) // 淡蓝色，不透明度减半
```

在 Web 上，跨域的图集图片必须允许匿名 CORS 请求，NSGo 才能为它的像素着色。在 HarmonyOS 上，NSGo 可以为从字节或应用资源加载的图集着色，但着色尚未在真机上验证，而由像素缓冲区或离屏图像构建的图集目前在那里不会绘制。

## 单个精灵特效

`Sprite.Tint` 是乘法着色（深色像素在白色着色下仍保持深色），而 `game/effects` 作用于整个游戏视图。要为单个精灵添加受击闪白、剪影、描边、溶解或调色板替换，同时不改变相邻精灵或精灵下方已绘制的内容，请设置 `Sprite.Effect`：

```go
fx := &sprite.Effect{
	Flash:   sprite.Flash{Color: core.RGB(0xffffff), Strength: 0.8}, // 或 sprite.Silhouette(core.RGB(0xffffff))
	Outline: sprite.Outline{Color: core.RGB(0xffdd00), Thickness: 1},
}
hero.Effect = fx

// 参数可以每帧修改；清零后精灵会自动回到 DrawAtlas 合批路径。
fx.Flash.Strength = 0
```

`Effect` 上的各个阶段按固定顺序组合执行：

1. **`PaletteSwap`**：在非预乘 8 位 sRGB 空间中按顺序匹配 `Entries`（允许 `Tolerance` 容差，先匹配者生效），并按 `To` 的不透明度缩放像素 alpha。
2. **`Sprite.Tint`**：若同时设置了 `Tint`，在基础精灵颜色上执行乘法着色（因此闪白和描边不会被基础着色压暗）。
3. **`Dissolve`**：丢弃噪声值低于 `Threshold`（`[0, 1]`）的像素，并可将 `[Threshold, Threshold+EdgeWidth)` 范围内的存活像素向 `EdgeColor` 混合。默认使用由 `Seed` 与单元大小 `Scale` 控制的确定性哈希/值噪声，也可采样自定义灰度 `Noise` 图像。
4. **`Flash`**：按 `Strength`（`[0, 1]`）把非透明像素的 RGB 向 `Flash.Color` 插值并保留每个像素的透明度。`Silhouette(color)` 等价于 `Strength: 1`。
5. **`Outline`**：在不透明度达到 `AlphaThreshold`（默认任意非零透明像素）的区域外围绘制宽度为 `Thickness`（源像素单位）的纯色描边。缓冲区会自动在四边扩展 `ceil(Thickness)` 像素的透明边距，因此紧裁剪帧不会被截断，图集中紧邻的其他帧也不会渗色到描边里。
6. **`Custom`**：可选的逐像素回调 `func(x, y, w, h int, r, g, b, a float32) (nr, ng, nb, na float32)`，在所有内置阶段之后运行于预乘 RGBA 通道上。

### GPU 着色器视图（`EffectShader`）

当单个精灵需要作为独立 GPU 着色器节点渲染时（例如用 `core.AnimatedNode` 在平台帧时钟上零提交驱动 Boss/立绘特效，或在支持 GPU 的平台上直接对 `asset:` URI 图集运行全部特效），可以使用 `EffectProgram`、`Effect.Uniforms` 和 `EffectShader` 通过 Kage 片段着色器（`core.Shader`）运行相同的内置阶段（GPU 调色板替换支持最多 `MaxShaderPaletteEntries` = 8 项）：

```go
sh, err := sprite.EffectShader(atlas, hero)
if err != nil {
	return err
}
sh.Uniforms["FlashStrength"] = flashNode // 跟随平台帧时钟动画，无需每帧提交
```

`EffectProgram` 仅在首次调用时延迟编译，因此只使用 `Batch.Draw` 的游戏不会链接 NSGo 的着色器编译器。

### 单精灵开销

- **未激活 `Effect`**：`nil` 或全零/未激活的 `*Effect` 只增加一次空指针/布尔检查，继续走 `Batch` 的 `DrawAtlas` 合批路径，无额外绘制调用或像素开销。
- **`Batch.Draw` 中的静态特效**：每个带特效的精灵占用 `Batch` 中一个复用的槽位缓冲区。当图集版本、帧矩形、着色与特效参数与上一帧相同时，`Batch.Draw` 跳过像素重算并复用相同的 `ImageSource.Version`，NSGo 不会重新拷贝或上传纹理（32×32 精灵约 `0.21 µs`）。
- **`Batch.Draw` 中每帧变化的参数**：`Batch` 原地复用每个槽位的 `*image.RGBA` 和 alpha 临时切片（仅在尺寸超出容量时扩容，并在空闲时收缩多余尾槽），每帧修改 `Flash.Strength`、`Outline.Thickness`、`Dissolve.Threshold` 或 `PaletteSwap.Entries` 均不会产生新的像素缓冲区分配，并单调递增 `ImageSource.Version`。开销与帧像素面积线性相关（`W × H`，开启 `Outline` 时对非完全不透明边缘像素额外检查约 `πr²` 邻域）。Apple Silicon（`-cpu 1`）上 `BenchmarkSpriteEffects` 参考读数：

| 特效（32×32 精灵，每帧修改参数） | 单精灵 CPU 耗时 | 每帧堆分配 |
| --- | ---: | ---: |
| `Flash`（16×16） | 0.84 µs | 1（`96 B`，`DrawOp` 图像值） |
| `Flash`（32×32） | 3.3 µs | 1（`96 B`） |
| `Outline`（1 px，32×32） | 6.2 µs | 1（`96 B`） |
| `Outline`（2 px，32×32） | 13.1 µs | 1（`96 B`） |
| `Dissolve` + 边缘发光（32×32） | 7.1 µs | 1（`96 B`） |
| 静态 `PaletteSwap`（32×32，命中缓存） | 0.21 µs | 1（`96 B`） |

- **GPU `EffectShader`**：每个精灵视图占用一个 `core.Shader` 表面（图集纹理上传到 GPU 后无需每帧 CPU 像素写入或上传；适合少量主角/Boss/UI 精灵，不适合数百个合批实体）。

### 各目标平台行为与降级方案

只要图集提供可读像素（`Atlas.Image.Pixels != nil`，或提供 `Atlas.Image.Data` 并由应用导入如 `_ "image/png"` 等解码器供 `image.Decode` 延迟解码），`Batch.Draw` 就会输出完全一致的逐像素特效。若图集仅通过 URI 指定（`core.Src("asset:...")`，既无 `Pixels` 也无 `Data`），`Batch.Draw` 会安全降级且绝不修改画布混合模式或影响邻居：完全溶解（`Threshold >= 1`）跳过绘制；`Flash` 降级为 NSGo 逐次绘制乘法着色（`DrawTintedImageFrom`，对变暗闪光和黑色剪影完全精确，但无法将深色像素提亮为白色）；`Outline`、部分 `Dissolve`、`PaletteSwap` 和 `Custom` 则按原样绘制精灵。

| 目标平台 | `Batch.Draw`（`Pixels` 或 `Data` 图集） | `Batch.Draw`（仅 URI 图集） | `EffectShader`（`core.Shader` GPU） |
| --- | --- | --- | --- |
| macOS、iOS | 经 `CGImage` 像素缓冲区精确渲染全部特效 | `DrawTintedImageFrom` 闪光降级；完全溶解跳过 | 完整 GPU 着色器（Metal） |
| Windows | 经 Direct2D 预乘 BGRA 位图精确渲染全部特效 | `DrawTintedImageFrom` 闪光降级；完全溶解跳过 | 完整 GPU 着色器（Direct3D 11） |
| Linux | 经 Cairo ARGB32 表面精确渲染全部特效 | `DrawTintedImageFrom` 闪光降级；完全溶解跳过 | 完整 GPU 着色器（OpenGL 3.3 / ES 3.0） |
| Android | 经管道 `Bitmap` 传输精确渲染全部特效 | `DrawTintedImageFrom` 闪光降级；完全溶解跳过 | 完整 GPU 着色器（GLES 3.0） |
| Web（Wasm） | 经缓存离屏 `<canvas>` 精确渲染全部特效 | `DrawTintedImageFrom` 闪光降级（跨域需 CORS）；完全溶解跳过 | 完整 GPU 着色器（WebGL 2） |
| HarmonyOS | 经 `PixelMap` 像素缓冲区精确渲染全部特效（`Tint` 合并进像素） | `DrawTintedImageFrom` 闪光降级（尚未真机验证）；完全溶解跳过 | 不可用（着色器视图空白；请使用 `Batch.Draw`） |
| 小程序 | 经离屏 2D 画布 `putImageData` 精确渲染全部特效 | `DrawTintedImageFrom` 闪光降级；完全溶解跳过 | 不可用（`nsgo_noshader`；请使用 `Batch.Draw`） |
| 无头测试（`gametest.RenderOps`） | 完整逐像素输出，可直接做像素比对测试 | 记录 `DrawTintedImageFrom` / `DrawImageFrom` 指令 | 在 `nsgotest` 中建模 uniform 通道与绑定 |

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
- **按深度排序的场景层**：`game/scene` 中按 Y（`SortY`）或自定义键排序的层逐个绘制精灵，每个精灵都是只有一个精灵的连续段，仍然各自调用 `DrawImageFrom`。只有不按精灵排序的层和直接绘制的批次才使用 `DrawAtlas`。
- **跳过的精灵**：位置、原点、旋转、缩放或透明度为 NaN 或无穷大的精灵不绘制任何内容，没有 `Atlas` 的批次同样不绘制。

绘制结果与每个精灵一次 `DrawImageFrom` 相同：测试回放两种记录并逐一比较绘制出的每个矩形，模糊测试对随机批次做同样的比较，`gametest.RenderOps` 渲染两者得到完全相同的像素。

### 使用 `DrawAtlas` 前后的记录开销

`BenchmarkBatchRecord` 记录一帧 1,000 个和 5,000 个移动中的精灵（其中五分之一带旋转和缩放），分别用每个精灵一次 `DrawImageFrom`（`per-sprite`）和 `DrawAtlas`（`atlas`）。它只计算 CPU 上的记录时间，不包括平台的绘制。`sprites/60Hz` 是 16.7 ms 除以每个精灵的记录时间，是任何一帧都达不到的上限，而不是实测的每秒 60 帧下的精灵数：

```sh
go tool nsgo test ./pkg/sprite -- -run '^$' -bench BatchRecord -benchmem -cpu 1 -benchtime 2s
```

2026-11-23 在一台共用的 24 核 x86-64 Linux 机器上的读数；耗时随机器负载变化，分配次数不变：

| 精灵数 | 每帧记录耗时 | 每帧分配次数 | 16.7 ms 内可记录的精灵数 |
| ---: | ---: | ---: | ---: |
| 1,000 | 219 µs → 81 µs | 1,000 → 3 | 76,000 → 206,400 |
| 5,000 | 926 µs → 586 µs | 5,000 → 3 | 90,000 → 142,300 |

记录只是一帧中较小的部分，平台仍要逐个绘制精灵。本仓库的 `Batch` 在设备上保持每秒 60 帧时最多能画多少个精灵，改动前后都还没有测量（[#28](https://github.com/nsgo-dev/game/issues/28)）。

NSGo 自己的[大量精灵对比](https://github.com/nsgo-dev/nsgo/blob/main/docs/architecture/performance.md#canvas-atlas-batching)把 `DrawAtlas` 与每个精灵一次 `DrawImageFrom` 相比，用的是它自己的 32 × 32 裁剪图样例而不是这里的 `Batch`，测的是固定精灵数下的帧率：5,000 个精灵在 Pixel 3 XL 上从每秒 29.3 帧升到 39.9 帧，在无头 Chromium 中从 55.5 帧升到 60.0 帧，在 macOS 上从 6.6 帧升到 7.8 帧；1,000 个精灵在 Android 和 Chromium 上本来就能达到每秒 60 帧。Linux 上还没有可靠的读数：在共用的 Ubuntu 虚拟机中，5,000 个精灵单独回放 Cairo 从 20.86 ms 降到 15.81 ms，但端到端帧率随宿主机负载变化，第一次 atlas 运行反而更低（1,000 个精灵从 25.54 降到 10.83，5,000 个从 10.37 降到 1.98），NSGo 认为需要在专用机器上成对测量。

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

批次既会记录 `DrawImageFrom`，也会把精灵记录在 `DrawAtlas` 中，所以应当用 `gametest.ImageDraws(ops)` 检查绘制了什么，而不是统计 `DrawImage` 指令的数量。它返回每个绘制出的图片矩形，包括源矩形、在画布上的四个角、透明度、混合方式、着色、采样方式和裁剪，与记录方式无关。`gametest.AssertSameImages(t, got, want)` 用同样的方式比较两份记录，只比较图片（填充、描边和文字需要另外检查），例如把批次与手写的 `DrawImageFrom` 调用相比；`gametest.RenderOps` 也能渲染 `DrawAtlas`。
