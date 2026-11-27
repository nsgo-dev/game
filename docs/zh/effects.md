# effects

`game/effects` 让游戏画面依次经过一串着色器通道（pass）绘制，uniform 可随平台帧时钟播放动画。画面通常是游戏每帧写入的像素缓冲（`core.ImageSource.Pixels`），自 [nsgo#1052](https://github.com/nsgo-dev/nsgo/issues/1052) 起 NSGo 着色器可以读取它。

```go
import "github.com/nsgo-dev/game/pkg/effects"
```

## 编写效果

一个效果是用 Kage（Go 语法的着色器语言，见 `core.CompileShader`）写的片元程序。它定义 `Effect`，返回画面上某一点的预乘颜色，坐标以画面像素为单位、从左上角起算；用 `Input(pos)` 读取上一个通道，用 `InputSize()` 读取画面尺寸。第一个效果的 `Input` 读的就是画面本身：该点所在的像素，画面之外为透明。包级导出变量即效果的 uniform。

```go
const invert = `package main

// Amount is how far the colors turn, 0 to 1.
var Amount float

func Effect(pos vec2) vec4 {
	c := Input(pos)
	turned := vec4(c.a-c.r, c.a-c.g, c.a-c.b, c.a)
	return c + (turned-c)*Amount
}
`

const smear = `package main

// Offset is how far to the right, in the frame's pixels, the second tap reads.
var Offset float

func Effect(pos vec2) vec4 {
	p := vec2(min(pos.x+Offset, InputSize().x-0.5), pos.y)
	return (Input(pos) + Input(p)) * 0.5
}
`
```

效果不能声明 `Input`、`InputSize`、`Texture`、`TextureSize` 或 `Fragment`，不能 import，只能通过 `Input` 读取画面（不能用 `image0At`），通过 `Texture(pos)` 和 `TextureSize()` 读取可选的附加纹理（每条效果链最多 3 个带纹理的效果，依次绑定到 `image1` 至 `image3`），以画面的像素为单位（不能用视图的 `rectSize` 或 `pixelScale`），也不能使用以 `__` 开头的名字（留给着色器编译器）或以 `p` 或 `P` 加数字和 `_` 开头的名字，后者留给效果链重命名各效果的声明。

## 串联效果

`Compile` 把多个效果融合成一个程序：第一个效果画在画面上，之后每个效果画在前一个之上。每个效果的 `Input` 调用前一个效果，因此所有通道在一次绘制中完成，中间不经过纹理。每段源码会先单独编译，所以报错会指明是哪个效果，并给出其源码中的行号和列号。

```go
var post = effects.MustCompile(
	effects.Effect{Name: "invert", Source: invert},
	effects.Effect{Name: "smear", Source: smear},
)
```

- 每个效果保留自己的名字：两个效果可以都声明 `Amount`、辅助函数 `func blend` 或 `const half`，uniform 按效果分别设置。
- 在多个点读取输入的通道，会在每个点上重新运行它之前的所有通道：两次采样的通道叠在两次采样的通道之上，会读 4 次画面。把采样点多的通道放在前面，或保持效果链简短。
- `Compile()` 不传效果时按原样绘制画面。`Chain.Source` 返回融合后的程序，相同效果得到相同源码。

## 绘制游戏视图

`effects.View` 是一个以画面为图像、拉伸填满自身方框的 `core.Shader`。要得到方形像素，让方框与画面宽高比一致，就像 `screen.Viewport` 摆放虚拟画布那样。`Uniforms` 按效果链顺序存放每个效果的取值；`FrameFunc` 与 `UniformsFunc` 跟随信号变化。

```go
frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
version := core.NewSignal(cx, uint64(0))
// 每帧：先画进 frame，再 version.Set(version.GetUntracked() + 1)。

return effects.View{
	Chain:     post,
	FrameFunc: func() core.ImageSource { return core.ImageSource{Pixels: frame, Version: version.Get()} },
	Uniforms:  []core.Uniforms{{"Amount": 0.6}, {"Offset": 1}},
	Style:     core.Style{Width: core.Pt(640), Height: core.Pt(360)},
}
```

`Chain` 为 nil 时按原样绘制画面。自己构建 `core.Shader` 时，可用 `Chain.Uniforms` 把各效果的 uniform 映射到程序中的名字；效果未声明的名字会导致 panic。

## 随帧时钟播放动画

设为 `core.AnimatedNode` 的 uniform 会跟随平台帧时钟变化，每帧无需提交。`Clock(cx, period)` 返回一个以秒计数的值，从 0 增长到 `period` 后再从 0 开始；由于值会跳回 0，请选择让效果恢复初始外观的周期。

```go
pulse := effects.Clock(cx, 2*time.Second)
effects.View{Chain: post, Frame: src, Uniforms: []core.Uniforms{{"Amount": pulse}, {"Offset": 1}}}
```

其他节点同样可用，例如玩家受击时启动的 `core.Timing`。偶尔才变化的值可以来自 `UniformsFunc`，代价是每次变化一次提交。

## 泛光

`Bloom()` 返回一个现成的效果，在一个通道内让画面的高光发光，通道之间不经过纹理：它在每个点上叠加该点及其周围 16 个点处输入的高亮部分，这 16 个点分布在半径一半处的内环和半径处的外环上。它的 uniform 有：`Threshold`，0 到 1 的亮度，高于它的像素才发光，超出这个范围时取范围内最接近的值；`Intensity`，叠加多少光晕，0 或更小（默认 0）时原样输出输入，1 到 2 是很强的泛光，大于 1000 时按 1000 处理；`Radius`，光晕以画面像素计能扩散多远。

```go
post := effects.MustCompile(effects.Bloom(), effects.Effect{Name: "invert", Source: invert})
effects.View{Chain: post, Frame: src, Uniforms: []core.Uniforms{{"Threshold": 0.6, "Intensity": 1.5, "Radius": 4}}}
```

- 光晕是叠加的光：颜色保持预乘，高光附近的半透明像素会变得更不透明。
- `Threshold` 比较的是预乘颜色的亮度，因此半透明像素的发光程度与其 alpha 成正比：alpha 为一半的白色亮度为 0.5，达不到 0.6 的阈值。
- 每个点会读取输入 17 次，而效果链会在每个读取点上重新运行它之前的效果，因此把 `Bloom` 放在最前面，或放在只读取一次输入的效果之后。
- 它近似的是模糊后的高亮通道：半径较大时光晕呈柔和的星形，而不是平滑的圆盘。`Radius` 保持在画面的几个像素以内，想要脉动效果就给 `Intensity` 做动画。
- `Bloom` 自带 `Reference`，因此包含它的效果链可以在无头环境下绘制参考图。

## CRT 与扫描线

`CRT()` 返回一个现成的效果，在一个通道内将画面弯曲到显像管曲面上，并沿曲面绘制水平扫描线。它的 uniform 有：`Curvature`，桶形畸变强度，0 或更小（默认 0）或 NaN 保持画面平直，0.1 到 0.3 是常见的 CRT 弯曲度，大于 4 时按 4 处理；`Scanlines`，0 到 1，扫描线之间暗带的变暗程度，0 不改变亮度，1 使暗带全黑，超出 0 到 1 时取范围内最接近的值；`Lines`，画面高度上的扫描线条数，0 或更小（默认 0）时按画面的像素行数每行画一条扫描线，大于 16384 时按 16384 处理。

```go
post := effects.MustCompile(effects.Bloom(), effects.CRT(), effects.VignetteEffect())
effects.View{
	Chain:    post,
	Frame:    src,
	Uniforms: []core.Uniforms{{"Threshold": 0.6, "Intensity": 1.2, "Radius": 3}, {"Curvature": 0.25, "Scanlines": 0.35}},
	Style:    core.Style{Width: core.Pt(640), Height: core.Pt(360)},
}
```

- 只设 `Curvature` 不设 `Scanlines` 时只弯曲画面；只设 `Scanlines` 不设 `Curvature` 时绘制平直扫描线；两者都为 0 或更小时原样输出输入。
- 四条边的中点仍紧贴画面边界，四角向内收拢到不透明的黑色边框中。屏幕内部扫描线只压暗 RGB 并保留每个像素的 alpha，颜色保持预乘。
- `Lines` 为 0 时每像素行一条扫描线，在视图放大画面（2 倍或更高）时呈明暗交替的条纹；以 1:1 尺寸绘制画面时，把 `Lines` 设为画面高度的一半或更小。
- 每个点最多读取输入一次，因此 `CRT` 可以放在 `Bloom` 等多次采样通道之后而不会放大其采样次数；需要四角压暗时可以再串联 `VignetteEffect`。
- `CRT` 自带 `Reference`，因此包含它的效果链可以在无头环境下绘制参考图。

## 暗角、闪屏与震屏

本包自带三个效果及其 `Reference` 函数，因此由它们组成的效果链可以绘制参考图。每个函数都返回一个新的 `Effect`，可直接放进效果链。

| 效果 | Uniform | 绘制内容 |
| --- | --- | --- |
| `VignetteEffect()` | `Strength`、`Radius`、`Softness`、`Color` | 让画面边缘偏向 `Color`（未给出时为黑色），保留每个像素的 alpha。距离从中心的 0 到四角的 1，在画面像素中呈圆形；边缘从 `Radius` 处开始变色，在 `Softness` 的距离内平滑过渡到底，程度由 `Strength`（0 到 1）决定。 |
| `FlashEffect()` | `Amount`、`Color` | 按 `Amount`（0 到 1）让整个画面偏向不透明的 `Color`。 |
| `ShakeEffect()` | `Amplitude`、`Time`、`Frequency`、`Phase` | 让画面在每个方向上最多移动 `Amplitude` 像素，按 `Time`（秒）每秒摆动 `Frequency` 次，起点由四个 `Phase` 角度决定；移出画面的地方读取画面边缘。偏移不是有限值或达到 1e30 像素时，画面保持不动。uniform 为普通数值时，`ShakeOffset` 在 Go 中给出同样的偏移。 |

未给出的 uniform 为 0，因此每个效果在设置前都按原样绘制画面。把震屏放在最前面，让后面的通道画在移动后的画面上，闪屏放在最后：

```go
post := effects.MustCompile(effects.ShakeEffect(), effects.VignetteEffect(), effects.FlashEffect())
```

`NewFlash` 和 `NewShake` 让闪屏和震屏的 uniform 随帧时钟变化，每帧无需提交。`Trigger` 启动一次效果，并替换仍在进行的那次；`Stop` 立即结束它：

- `Flash.Trigger(peak, fade)` 把 `Amount` 设为 `peak`，并在 `fade` 时间内缓出淡到 0。
- `Shake.Trigger(amplitude, d)` 设置 `Amplitude`，并在 `d` 时间内缓出衰减到 0，同时 `Time` 从 0 开始计时。每次触发从 `ShakeConfig.Seed` 取下一组四个相位，因此用同一个种子回放时震动完全相同。`ShakeConfig.Frequency` 为 0 时取 12。

```go
assist := a11y.NewAssist(cx, "")
flash := effects.NewFlash(cx, effects.FlashConfig{Color: core.RGB(0xffffff), ReduceMotion: assist.ReduceMotion})
shake := effects.NewShake(cx, effects.ShakeConfig{Seed: 42, ReduceMotion: assist.ReduceMotion})
vignette := core.Uniforms{"Strength": 0.8, "Radius": 0.4, "Softness": 0.6}

onHit := func() {
	flash.Trigger(0.7, 300*time.Millisecond)
	shake.Trigger(6, 400*time.Millisecond)
}

return effects.View{
	Chain:     post,
	FrameFunc: func() core.ImageSource { return core.ImageSource{Pixels: frame, Version: version.Get()} },
	Uniforms:  []core.Uniforms{shake.Uniforms(), vignette, flash.Uniforms()},
	Style:     core.Style{Width: core.Pt(640), Height: core.Pt(360)},
}
```

闪屏和震屏遵循减弱动态效果设置：`ReduceMotion` 返回 true 时，`Trigger` 什么也不做并返回 false。可传入 `a11y.Assist` 的 `ReduceMotion`，它优先采用玩家的选择，否则跟随平台设置；`ReduceMotion` 为 nil 时读取平台设置。峰值、幅度或时长不大于 0 时同样不会启动，也不影响正在进行的那次：要结束它请用 `Stop`，这与 `tilemap.Camera.Shake` 不同，后者的强度为 0 时会取消震动。

## 调色板替换

本包提供两个通过调色板纹理映射颜色的现成效果，调色板绑定在 `Effect.Texture` 上，由着色器通过 `Texture(pos)` 和 `TextureSize()` 读取。两者都自带 `Reference`，只要调色板带有 `Pixels` 或 PNG `Data`，包含它们的效果链即可在无头环境下绘制参考图。

- `PaletteSwap(palette)` 按颜色键替换：第 `0` 行存放最多 256 个源颜色（透明格会被跳过），从第 `0` 行到第 `height - 1` 行的每一行都是一套目标调色板。它的 uniform 有：`Amount`，0 到 1（0 或更小、即默认值时原样透传输入）；`Row`，从 `0` 到 `height - 1`，小数会在相邻两行之间线性混合；`Tolerance`，非预乘 RGB 各通道允许匹配第 `0` 行源颜色的最大差值，不大于 `0.002/alpha`（含默认值 0）时按精确的 8 位颜色匹配，更大的值会按欧氏 RGB 距离选取最近的匹配键，设为 `1` 则把所有非透明颜色映射到最近的键。未匹配的颜色和透明像素保持不变。
- `PaletteRamp(palette)` 按像素非预乘颜色的 Rec. 709 亮度映射到任意宽度的由暗到亮调色板行（黑对应第 `0` 列，白对应第 `width - 1` 列），uniform 为 `Amount`（0 到 1）和 `Row`（0 到 `height - 1`，小数在相邻行之间混合）。

`PaletteImage(rows...)` 可用多行 `core.Color` 构造预乘 `*image.RGBA` 调色板纹理：

```go
pal := effects.PaletteImage(
	[]core.Color{core.RGB(0xd03030), core.RGB(0xf0c030), core.RGB(0x3060d0)}, // 第 0 行：源颜色键
	[]core.Color{core.RGB(0x40c8f0), core.RGB(0xe0f8ff), core.RGB(0x205090)}, // 第 1 行：冰霜配色
	[]core.Color{core.RGB(0x8838c8), core.RGB(0xb8f040), core.RGB(0x386030)}, // 第 2 行：剧毒配色
)
post := effects.MustCompile(effects.PaletteSwap(core.ImageSource{Pixels: pal, Version: 1}))

return effects.View{
	Chain:    post,
	Frame:    src,
	Uniforms: []core.Uniforms{{"Amount": 1, "Row": 1}},
}
```

- 颜色保持预乘：命中像素的 alpha 会乘到目标调色板条目的预乘颜色上，因此半透明精灵会保留自身 alpha，调色板条目本身也可以是半透明或透明的。
- 把 `Amount` 或 `Row` 设为 `core.AnimatedNode`，即可随平台帧时钟平滑淡入或在调色板之间过渡，无需重新上传纹理。
- 如需在视图上动态更换效果纹理而不重新编译效果链，可设置 `View.Textures` 或 `View.TexturesFunc`（自建 `core.Shader` 时用 `Chain.Images`，无头测试中用 `Chain.ReferenceWithTextures`）。

## 参考图

无头测试没有 GPU，因此 `Chain.Reference(frame, width, height, uniforms)` 用各效果的 `Reference` 函数在 CPU 上绘制效果链，使用 `View` 会设置的各效果 uniform，并像 GPU 一样把每个输出像素的中心映射到画面上。`Float` 读取数值 uniform，未给出的 uniform 为 0，与 GPU 一致。将结果与 `gametest` 的 golden PNG 比较：

```go
invertRef := func(in effects.Input, u core.Uniforms, x, y float64) [4]float64 {
	c, amount := in.At(x, y), effects.Float(u, "Amount")
	for k := range 3 {
		c[k] += (c[3] - 2*c[k]) * amount
	}
	return c
}
chain := effects.MustCompile(effects.Effect{Source: invert, Reference: invertRef}, ...)
got, err := chain.Reference(frame, 640, 360, []core.Uniforms{{"Amount": 0.6}, {"Offset": 1}})
```

有效果缺少 `Reference`、uniform 不是该效果声明的、或 uniform 设为动画节点（例如 `Clock` 的返回值，其值只有平台知道）时，`Reference` 返回错误；这时请传入要绘制的具体数值。
