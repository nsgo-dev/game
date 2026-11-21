# text

`game/text` renders AngelCode BMFont bitmap fonts through `sprite.Batch` (with kerning, word wrapping, and alignment) and platform-rendered Unicode/CJK text through `core.Drawing.FillText`.

```go
import "github.com/nsgo-dev/game/packages/game/text"
```

## Bitmap fonts (AngelCode BMFont)

`LoadBMFont` and `LoadBMFontImage` parse AngelCode BMFont `.fnt` files in both **plain-text** and **XML** formats, building one `*sprite.Atlas` per texture page and indexing glyph metrics and kerning pairs:

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

## Platform text with DrawPlatform

For localized prose, CJK/RTL scripts, or crisp vector labels drawn inside the game canvas, use `DrawPlatform` and `MeasurePlatform`. They draw via `core.Drawing.FillText` under the canvas's active transform (so they scale cleanly with `screen.Viewport` or `tilemap.Camera`) and wrap paint state in `Save`/`Restore`:

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

`PlatformStyle` supports `Font`, `Color` (`0` keeps the drawing's current fill color), `Alpha` (`0` means `1`), `Align`, `Baseline` (defaults to `core.BaselineAlphabetic`), `MaxWidth`, `LineHeight` (`<= 0` uses `d.TextMetrics`), and `LineSpacing`.
