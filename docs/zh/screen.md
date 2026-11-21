# screen

`game/screen` 将固定虚拟画布映射到任意窗口尺寸与宽高比，在视图坐标与虚拟坐标间转换指针输入，支持像素风整数缩放与像素对齐，并为 HUD 图层计算安全区矩形。

```go
import "github.com/nsgo-dev/game/packages/game/screen"
```

## Viewport 与缩放模式

使用游戏的虚拟分辨率（例如 `320×180`）定义 `Viewport`，并通过 `Mode` 选择适配不同窗口宽高比的方式：

```go
vp := screen.Viewport{
	Width:    320,
	Height:   180,
	Mode:     screen.ModeFit,
	BarColor: core.RGB(0x0b0e14),
}
```

| 模式 | 行为 |
| --- | --- |
| `ModeFit` | 等比缩放使整个虚拟画布完整落入窗口并居中，多余边缘填充黑边（默认）。 |
| `ModeFill` | 等比缩放铺满整个窗口并居中，超出窗口的部分会被裁剪。 |
| `ModeStretch` | 独立拉伸 X 与 Y 轴填满窗口，无黑边也不裁剪。 |
| `ModeExpand` | 按较紧的轴等比缩放，并沿另一条轴扩展可见的 `VirtualSize`，无黑边也无裁剪。 |

在 `Canvas.Draw` 中用 `vp.Draw(d, size, fn)` 包裹游戏绘制：

```go
return core.Canvas{
	Style: core.Style{FlexGrow: 1, FlexShrink: 1},
	Draw: func(d *core.Drawing, window core.Size) {
		vp.Draw(d, window, func(d *core.Drawing, virtualSize core.Size) {
			gameScene.Draw(d)
			_ = virtualSize // 除 ModeExpand 外始终等于 (320, 180)
		})
	},
}
```

`vp.Draw` 会自动绘制黑边（`BarColor` 为 0 时默认纯黑，设为 `core.Transparent` 可不绘制黑边）、在需要时裁剪溢出、应用平移与缩放、调用 `fn`，最后恢复 `d` 的状态。调用 `vp.Layout(window)` 可直接检查计算出的 `Rect`、`VirtualSize`、`ScaleX`、`ScaleY` 和 `Bars`。

## 像素风整数缩放与对齐

对于要求像素锐利、无抖动的像素游戏，开启 `IntegerScale` 与 `PixelSnap`：

```go
vp := screen.Viewport{
	Width:        320,
	Height:       180,
	IntegerScale: true, // 使用能放进窗口的最大 >= 1 整数倍；窗口小于 1x 时回退到浮点 Fit
	PixelSnap:    true, // 对齐视口偏移，并使 Snap/SnapPoint 取整到整数虚拟像素
}

// 当 PixelSnap 开启时，将插值后的精灵或相机坐标对齐到整数虚拟像素：
drawX, drawY := vp.Snap(lerpX, lerpY)
camPos := vp.SnapPoint(targetPos)
```

包级函数 `screen.Snap(x, y)` 与 `screen.SnapPoint(p)` 会无条件四舍五入到整数虚拟像素。

## 转换指针坐标

使用 `ViewToGame` 将 `core.PressEvent`、`core.TouchEvent` 或 `core.MouseEvent` 中的视图坐标转换为虚拟游戏坐标，使用 `GameToView` 执行反向转换：

```go
if pt, ok := vp.ViewToGame(windowSize, ev.Position); ok {
	// pt 位于虚拟画布内；落在黑边或窗口外时 ok 为 false
	handleClick(pt)
}
```

`ViewToGame` 可直接接到 [`input.Actions.MapPoint`](input.md#将视图坐标映射到虚拟画布)。

## 移动端 HUD 安全区

使用 `SafeGameRectForEnv`（或传入显式 `core.Edges` 的 `SafeGameRect`）让屏幕空间 HUD 按钮与读数避开刘海、圆角、状态栏和底部指示条：

```go
safe := vp.SafeGameRectForEnv(cx.Env())
// safe 已裁剪到可见虚拟画布内，并换算为虚拟游戏坐标
scoreX := safe.X + 8
scoreY := safe.Y + 8
```

在响应式计算或 `core.Canvas.Draw` 中调用 `SafeGameRectForEnv` / `SafeRectForEnv` 会自动订阅窗口尺寸与安全区边距的变化。若是在原生 NSGo 视图坐标中布局，请使用 `screen.SafeRect(window, insets)` 或 `screen.SafeRectForEnv(cx.Env())`。
