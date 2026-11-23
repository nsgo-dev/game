# effects

`game/effects` 让游戏画面依次经过一串着色器通道（pass）绘制，uniform 可随平台帧时钟播放动画。画面通常是游戏每帧写入的像素缓冲（`core.ImageSource.Pixels`），自 [nsgo#1052](https://github.com/nsgo-dev/nsgo/issues/1052) 起 NSGo 着色器可以读取它。

```go
import "github.com/nsgo-dev/game/packages/game/effects"
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

效果不能声明 `Input`、`InputSize` 或 `Fragment`，不能 import，只能通过 `Input` 读取画面（不能用 `image0At`），以画面的像素为单位（不能用视图的 `rectSize` 或 `pixelScale`），也不能使用以 `__` 开头的名字（留给着色器编译器）或以 `p` 或 `P` 加数字和 `_` 开头的名字，后者留给效果链重命名各效果的声明。

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

有效果缺少 `Reference`、uniform 不是该效果声明的、或 uniform 设为动画节点（例如 `Clock` 的返回值，其值只有平台知道）时，`Reference` 返回错误；这时请传入要绘制的具体数值。PNG 存储的是非预乘颜色，因此图中有半透明像素时，先把预乘的图转换为 `*image.NRGBA` 再交给 `gametest.AssertGolden`。
