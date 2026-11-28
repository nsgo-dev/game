# text

`game/text` renders AngelCode BMFont bitmap fonts through `sprite.Batch` (with kerning, word wrapping, and alignment), signed-distance-field fonts that stay sharp at any size, on the screen or in 3D world space, and platform-rendered Unicode/CJK text through `core.Drawing.FillText`.

```go
import "github.com/nsgo-dev/game/pkg/text"
```

## Bitmap fonts (AngelCode BMFont)

`LoadBMFont` and `LoadBMFontImage` parse AngelCode BMFont `.fnt` files in both **plain-text** and **XML** formats, building one `*sprite.Atlas` per texture page and indexing glyph metrics and kerning pairs. `game assets` bakes such a font from a TrueType or OpenType file, or from a folder of PNG glyph images such as score digits or damage numbers, with a font target ([Command Line](cli.md#assets)):

```go
//go:embed resources/pixel.fnt
var pixelFNT []byte

// Single-page font shortcut:
font, err := text.LoadBMFontImage(core.Src("asset:pixel_0.png"), pixelFNT)
if err != nil {
	return err
}

// Multi-page font:
multiFont, err := text.LoadBMFont(multiFNT, func(id int, file string) core.ImageSource {
	return core.Src("asset:fonts/" + file)
})
```

### Drawing, wrapping, and alignment

Draw bitmap text inside `core.Canvas.Draw` with `font.Draw(d, s, x, y, opts)`:

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

`Options` fields (its zero value draws left-aligned at scale `1`, full opacity, no wrapping):

| Field | Behavior |
| --- | --- |
| `MaxWidth` | When `> 0`, wraps lines to fit within `MaxWidth` points and aligns `AlignCenter`/`AlignRight` inside `[x, x+MaxWidth]`. When `<= 0`, aligns relative to `x` directly (centered at `x`, or right edge at `x`). |
| `Align` | `AlignLeft` (default), `AlignCenter`, or `AlignRight`. |
| `Scale` | Multiplies glyph positions and sizes (`0` means `1`; negative draws nothing). |
| `Alpha` | Opacity in `[0, 1]` (`0` means `1`; negative draws nothing). |
| `LineSpacing` | Extra vertical spacing between lines, in points. |
| `Z` | Passed to each glyph sprite in the underlying `sprite.Batch`. |

### Measuring and custom glyph effects

Use `font.Measure(s, opts)` to compute bounding `core.Size` for speech bubbles or layout boxes, `font.Kerning(a, b)` for kerning offsets, and `font.Layout(s, x, y, opts)` to get exact `[]GlyphPlacement` positions for custom effects (such as typewriter reveals or wavy text):

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

### Reporting missing glyphs

When `Draw`, `Layout`, `Measure`, or rich-text layout encounters a rune the font has no glyph for, it skips that rune. In debug builds (everything except `-tags release`), call `text.SetLogMissingGlyphs(true)` to log each skipped rune once per rune and font, and optionally install a custom callback with `text.SetMissingGlyphLogger(fn)`. Both functions compile to no-ops under `-tags release` (`text.LogMissingGlyphsEnabled()` returns `false`). To inspect coverage directly, call `font.MissingRunes(s)` (or `sdfFont.MissingRunes(s)`), which returns the sorted, deduplicated non-control runes in `s` that the font lacks.

For localized bitmap fonts, configure `charsFrom` and `charsFile` on the font target in `game.yaml` so `go tool game assets` bakes the characters used by your string tables and `go tool game check` flags any undrawable localized string ([Command Line](cli.md#assets)).

## Signed-distance-field fonts

A bitmap font is sharp only at the size it was drawn at; stretched, its edges blur. An `SDFFont` keeps a distance field instead (how far each atlas pixel is from the glyph's outline), and each glyph is resolved from it at the size it is drawn at, so its edge is one pixel wide at any size: right for text that zooms, labels in a 3D world and damage numbers that fly toward the camera.

`LoadSDFFont` and `LoadSDFFontImage` read the forms the common generators write, as they write them:

- [msdf-atlas-gen](https://github.com/Chlumsky/msdf-atlas-gen)'s JSON (`-json`, with `-charset` so glyphs are named by code point);
- AngelCode BMFont in plain text, XML or JSON with a `distanceField` record, as [msdf-bmfont-xml](https://github.com/soimy/msdf-bmfont-xml) writes.

The field type (`sdf`, `psdf`, `msdf` or `mtsdf`), the distance range and, for msdf-atlas-gen, the range's middle (`distanceRangeMiddle`, for an asymmetric `-apxrange`) and the atlas's `yOrigin` (either `bottom` or `top`) come from the file. A multi-channel field keeps corners sharp; an `mtsdf` atlas also gives outlines round corners. The loader reads the distance out of the decoded page images, so it takes `image.Image`s rather than `core.ImageSource`s:

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

`font.Draw(d, s, x, y, opts)`, `font.Measure` and `font.Layout` work as a bitmap font's do, with `SDFOptions`:

| Field | Behavior |
| --- | --- |
| `Size` | Em size in points (`0` means the font's own `Size`, the atlas's pixels an em). |
| `MaxWidth`, `Align` | Wrapping and alignment, as `Options`'s. |
| `LineSpacing` | Extra space between lines, in ems. |
| `Color` | Fill color (`0` means opaque white). |
| `Alpha` | Opacity (`0` means `1`, negative draws nothing). It is applied when drawing, so a fading label reuses its resolved glyphs. |
| `Outline`, `OutlineColor` | An outline around each glyph, its width in ems, up to the field's reach (`DistanceRange/2 - DistanceRangeMiddle` atlas pixels). |
| `PixelScale` | Device pixels a point covers, so glyphs are resolved at the screen's density (`0` means `1`). |

NSGo's `core.Drawing` has no per-draw shader, so the threshold runs on the CPU: a glyph is resolved once per size step (sizes 2% apart share one) and color, kept in a cache bounded by `SDFFont.CacheBytes` (16 MiB by default; `CachedGlyphs` and `ClearCache` inspect and empty it), and drawn as an image. A glyph image is at most 1024 pixels a side. The font's exported fields (metrics, glyphs, kernings and field settings) are read on every call, so they can be changed after loading; a changed glyph or field setting resolves its glyphs afresh. An `SDFFont` is not safe for concurrent use; draw with it from the canvas's `Draw`.

## Text in 3D world space

`font.DrawWorld(d, cam, viewport, t)` draws a `WorldText` through a `scene3d.Camera` into a canvas, with `viewport` the camera's view in the canvas's points (as `render3d.ProjectSegment`'s):

```go
// A damage number that faces the camera and shrinks with distance.
font.DrawWorld(d, cam, viewport, text.WorldText{
	Text:    "128",
	Pos:     enemy.Pos.Add(math3d.Vec3{Y: 2}),
	Size:    0.5, // world units an em
	AnchorX: 0.5, AnchorY: 1, // bottom middle at Pos
	Options: text.SDFOptions{Color: core.RGB(0xffd23f), Outline: 0.08, OutlineColor: core.RGB(0x000000), Alpha: fade, PixelScale: 2},
})

// A sign fixed on a wall, read from the wall's front only.
font.DrawWorld(d, cam, viewport, text.WorldText{
	Text:         "ARMORY",
	Pos:          math3d.Vec3{X: 4, Y: 3},
	Facing:       text.FacingFixed,
	Rot:          math3d.QuatFromAxisAngle(math3d.Vec3Up(), -math.Pi/2),
	Size:         0.6,
	CullBackFace: true,
})
```

| Field | Behavior |
| --- | --- |
| `Facing` | `FacingBillboard` (default) turns the text to face the camera, upright on the screen. `FacingFixed` lays it in the plane `Rot` sets: lines along `Rot`'s +X, glyphs up its +Y, read from +Z (the zero `Quat` is the identity). |
| `Size` | Em size in world units, so the text shrinks with distance. |
| `ScreenSize` | For a billboard, an em of this many points at any distance, in place of `Size`. |
| `AnchorX`, `AnchorY` | The point of the text's box at `Pos`, as fractions from its top left. |
| `CullBackFace` | Hides fixed text seen from behind; without it, it is drawn mirrored. |
| `Options` | Color, outline, alignment, opacity and pixel density; `MaxWidth` is in world units (in points with `ScreenSize`), and `Size` is ignored. |

A billboard is hidden when its anchor is behind the camera or past the far plane. Fixed text is hidden glyph by glyph, so a long sign the camera stands beside keeps the glyphs in view. Each fixed glyph is drawn with the affine transform closest to the perspective at its center, and every glyph is resolved at the scale it covers on the screen, so text stays sharp from up close to far away.

To draw overlapping labels back to front, lay each out with `font.LayoutWorld`, sort the `WorldLayout`s by `Depth` (highest first) and draw them with `font.DrawLayout`. `Depth` is the anchor's, or, for fixed text whose anchor is behind the camera or past the far plane, the mean of its visible glyphs' centers.

## Platform text with DrawPlatform

For localized prose, CJK/RTL scripts, or crisp vector labels drawn inside the game canvas, use `DrawPlatform` and `MeasurePlatform`. They draw via `core.Drawing.FillText` under the canvas's active transform (so they scale cleanly with `screen.Viewport` or `tilemap.Camera`) and wrap paint state in `Save`/`Restore`:

```go
style := text.PlatformStyle{
	Font:        core.Font{Size: 14, Weight: core.FontWeightBold},
	Color:       core.RGB(0xf8fafc),
	Align:       text.AlignCenter,
	MaxWidth:    240,
	LineSpacing: 4,
}

boxSize := text.MeasurePlatform(d, "Victory! 胜利！", style)
text.DrawPlatform(d, "Victory! 胜利！", 40, 24, style)
_ = boxSize
```

`PlatformStyle` supports `Font`, `Color` (`0` keeps the drawing's current fill color), `Alpha` (`0` means `1`), `Align`, `Baseline` (defaults to `core.BaselineAlphabetic`), `MaxWidth`, `LineHeight` (`<= 0` uses `d.TextMetrics`), and `LineSpacing`.
