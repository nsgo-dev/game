# text

`game/text` 通过 `sprite.Batch` 绘制 AngelCode BMFont 位图字体（支持字距调整、自动换行和对齐），并通过 `core.Drawing.FillText` 绘制平台矢量文本。

```go
import "github.com/nsgo-dev/game/packages/game/text"
```

## 位图字体（AngelCode BMFont）

`LoadBMFont` 和 `LoadBMFontImage` 支持解析 **纯文本** 和 **XML** 两种格式的 AngelCode BMFont `.fnt` 文件，为每张纹理页构建一个 `*sprite.Atlas` 并索引字形度量与字距对：

```go
//go:embed resources/pixel.fnt
var pixelFNT []byte

// 单页字体快捷方式：
font, err := text.LoadBMFontImage(core.Src("asset:pixel_0.png"), pixelFNT)
if err != nil {
	return err
}

// 多页字体：
multiFont, err := text.LoadBMFont(multiFNT, func(id int, file string) core.ImageSource {
	return core.Src("asset:fonts/" + file)
})
```

### 绘制、换行与对齐

在 `core.Canvas.Draw` 中调用 `font.Draw(d, s, x, y, opts)` 绘制位图文字：

```go
font.Draw(d, "PRESS START TO CONTINUE", 160, 120, text.Options{
	MaxWidth:    200,
	Align:       text.AlignCenter,
	Scale:       1,
	Alpha:       0.95,
	LineSpacing: 2,
	Z:           10,
})
```

`Options` 字段（零值表示左对齐、缩放为 `1`、完全不透明且不换行）：

| 字段 | 行为 |
| --- | --- |
| `MaxWidth` | 大于 `0` 时按 `MaxWidth` 点自动换行，并将 `AlignCenter`/`AlignRight` 对齐在 `[x, x+MaxWidth]` 内；`<= 0` 时不换行，直接相对 `x` 对齐（以 `x` 为中心或右边缘）。 |
| `Align` | `AlignLeft`（默认）、`AlignCenter` 或 `AlignRight`。 |
| `Scale` | 缩放字形位置与尺寸（`0` 表示 `1`，负值不绘制）。 |
| `Alpha` | 不透明度 `[0, 1]`（`0` 表示 `1`，负值不绘制）。 |
| `LineSpacing` | 行间额外垂直间距，单位为点。 |
| `Z` | 传递给底层 `sprite.Batch` 中每个字形精灵的 Z 顺序。 |

### 测量与自定义字形效果

使用 `font.Measure(s, opts)` 计算对话框或布局包围盒尺寸，使用 `font.Kerning(a, b)` 查询字距微调，使用 `font.Layout(s, x, y, opts)` 获取精确的 `[]GlyphPlacement` 以实现打字机逐字显示或波浪文字等自定义效果：

```go
size := font.Measure(dialogue, text.Options{MaxWidth: 220})
placements := font.Layout(dialogue, boxX, boxY, text.Options{MaxWidth: 220})
for i, g := range placements {
	if i >= visibleChars {
		break
	}
	_ = g // g.Rune, g.Glyph, g.X, g.Y, g.W, g.H, g.Line
}
```

## 使用 DrawPlatform 绘制平台文本

需要在游戏画布内绘制多语言文案、中日韩文字或清晰矢量标签时，使用 `DrawPlatform` 和 `MeasurePlatform`。它们通过 `core.Drawing.FillText` 在当前画布变换栈下绘制（可随 `screen.Viewport` 或 `tilemap.Camera` 自动缩放），并用 `Save`/`Restore` 隔离画笔状态：

```go
style := text.PlatformStyle{
	Font:        core.Font{Size: 14, Weight: core.FontBold},
	Color:       core.RGB(0xf8fafc),
	Align:       text.AlignCenter,
	MaxWidth:    240,
	LineSpacing: 4,
}

boxSize := text.MeasurePlatform(d, "Victory! 胜利！", style)
text.DrawPlatform(d, "Victory! 胜利！", 40, 24, style)
_ = boxSize
```

`PlatformStyle` 支持 `Font`、`Color`（`0` 表示沿用画布当前填充色）、`Alpha`（`0` 表示 `1`）、`Align`、`Baseline`（默认 `core.BaselineAlphabetic`）、`MaxWidth`、`LineHeight`（`<= 0` 时使用 `d.TextMetrics` 测量的行高）以及 `LineSpacing`。
