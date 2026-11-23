# text

`game/text` 通过 `sprite.Batch` 绘制 AngelCode BMFont 位图字体（支持字距调整、自动换行和对齐），绘制在屏幕上或 3D 世界空间中任何尺寸都保持清晰的有向距离场字体，并通过 `core.Drawing.FillText` 绘制平台矢量文本。

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

## 有向距离场字体

位图字体只在生成时的尺寸下清晰，放大后边缘就会模糊。`SDFFont` 保存的是距离场（每个图集像素到字形轮廓的距离），每个字形都按绘制时的尺寸从距离场解析出来，所以任何尺寸下边缘都只有一个像素宽：适合会缩放的文字、3D 世界中的标签，以及飞向镜头的伤害数字。

`LoadSDFFont` 和 `LoadSDFFontImage` 按常用生成工具的原样输出读取，不需要转换：

- [msdf-atlas-gen](https://github.com/Chlumsky/msdf-atlas-gen) 的 JSON（`-json`，并用 `-charset` 让字形以码点命名）；
- 带 `distanceField` 记录的 AngelCode BMFont 纯文本、XML 或 JSON，即 [msdf-bmfont-xml](https://github.com/soimy/msdf-bmfont-xml) 的输出。

距离场类型（`sdf`、`psdf`、`msdf` 或 `mtsdf`）、距离范围，以及 msdf-atlas-gen 的范围中点（`distanceRangeMiddle`，用于非对称的 `-apxrange`）和图集的 `yOrigin`（`bottom` 或 `top` 均可）都取自文件。多通道距离场能保持拐角锐利；`mtsdf` 图集还能让描边拥有圆角。加载器从解码后的页面图像中读出距离，因此接收的是 `image.Image` 而不是 `core.ImageSource`：

```go
//go:embed fonts/title.json fonts/title.png
var fontFiles embed.FS

data, _ := fontFiles.ReadFile("fonts/title.json")
pngData, _ := fontFiles.ReadFile("fonts/title.png")
img, err := png.Decode(bytes.NewReader(pngData))
if err != nil {
	return err
}
font, err := text.LoadSDFFontImage(img, data)
```

`font.Draw(d, s, x, y, opts)`、`font.Measure` 和 `font.Layout` 的用法与位图字体相同，选项为 `SDFOptions`：

| 字段 | 行为 |
| --- | --- |
| `Size` | em 尺寸，单位为点（`0` 表示字体自身的 `Size`，即图集中一个 em 的像素数）。 |
| `MaxWidth`、`Align` | 换行与对齐，与 `Options` 相同。 |
| `LineSpacing` | 行间额外间距，单位为 em。 |
| `Color` | 填充颜色（`0` 表示不透明白色）。 |
| `Alpha` | 不透明度（`0` 表示 `1`，负值不绘制）。它在绘制时应用，所以渐隐的标签会复用已解析的字形。 |
| `Outline`、`OutlineColor` | 每个字形的描边，宽度单位为 em，最多到距离场覆盖的范围（`DistanceRange/2 - DistanceRangeMiddle` 个图集像素）。 |
| `PixelScale` | 每个点覆盖的设备像素数，使字形按屏幕密度解析（`0` 表示 `1`）。 |

NSGo 的 `core.Drawing` 没有逐次绘制的着色器，因此阈值处理在 CPU 上进行：每个字形按尺寸档位（相差 2% 以内的尺寸共用一档）和颜色解析一次，存入以 `SDFFont.CacheBytes` 为上限的缓存（默认 16 MiB，可用 `CachedGlyphs` 查看、`ClearCache` 清空），再作为图像绘制。单个字形图像每边最多 1024 像素。字体的导出字段（度量、字形、字距和距离场设置）每次调用都会重新读取，所以加载后可以修改；修改字形或距离场设置后，相关字形会重新解析。`SDFFont` 不能并发使用，请在画布的 `Draw` 中使用它绘制。

## 3D 世界空间中的文字

`font.DrawWorld(d, cam, viewport, t)` 通过 `scene3d.Camera` 把 `WorldText` 绘制到画布中，`viewport` 是相机视图在画布中的范围（单位为点，与 `render3d.ProjectSegment` 相同）：

```go
// 朝向镜头、随距离缩小的伤害数字。
font.DrawWorld(d, cam, viewport, text.WorldText{
	Text:    "128",
	Pos:     enemy.Pos.Add(math3d.Vec3{Y: 2}),
	Size:    0.5, // 每 em 的世界单位
	AnchorX: 0.5, AnchorY: 1, // 底边中点位于 Pos
	Options: text.SDFOptions{Color: core.RGB(0xffd23f), Outline: 0.08, OutlineColor: core.RGB(0x000000), Alpha: fade, PixelScale: 2},
})

// 固定在墙上、只能从正面阅读的标牌。
font.DrawWorld(d, cam, viewport, text.WorldText{
	Text:         "ARMORY",
	Pos:          math3d.Vec3{X: 4, Y: 3},
	Facing:       text.FacingFixed,
	Rot:          math3d.QuatFromAxisAngle(math3d.Vec3Up(), -math.Pi/2),
	Size:         0.6,
	CullBackFace: true,
})
```

| 字段 | 行为 |
| --- | --- |
| `Facing` | `FacingBillboard`（默认）让文字朝向镜头，并在屏幕上保持直立。`FacingFixed` 把文字放在 `Rot` 决定的平面内：行沿 `Rot` 的 +X，字形朝 +Y 竖立，从 +Z 一侧阅读（零值 `Quat` 视为单位旋转）。 |
| `Size` | em 尺寸，单位为世界单位，所以文字随距离缩小。 |
| `ScreenSize` | 对广告牌文字，在任何距离下一个 em 都是这么多点，取代 `Size`。 |
| `AnchorX`、`AnchorY` | 文字包围盒中位于 `Pos` 的点，以从左上角起的比例表示。 |
| `CullBackFace` | 从背面看固定文字时不绘制；不设置时绘制为镜像。 |
| `Options` | 颜色、描边、对齐、不透明度与像素密度；`MaxWidth` 以世界单位计（设置 `ScreenSize` 时以点计），`Size` 被忽略。 |

广告牌文字的锚点在镜头后方或远裁剪面之外时整体隐藏。固定文字按字形逐个隐藏，所以镜头站在长标牌旁边时，视野内的字形仍会绘制。每个固定字形都用在其中心最接近透视的仿射变换绘制，并且每个字形都按它在屏幕上覆盖的比例解析，所以文字从近到远都保持清晰。

要从后往前绘制互相重叠的标签，先用 `font.LayoutWorld` 布局每个标签，按 `Depth` 从大到小排序 `WorldLayout`，再用 `font.DrawLayout` 绘制。`Depth` 取锚点的深度；固定文字的锚点在镜头后方或远裁剪面之外时，取其可见字形中心深度的平均值。

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
