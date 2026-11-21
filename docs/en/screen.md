# screen

`game/screen` maps a fixed virtual canvas onto any window size and aspect ratio, converts pointer input between view and virtual coordinates, snaps pixel-art coordinates, and computes safe-area rectangles for HUD layers.

```go
import "github.com/nsgo-dev/game/packages/game/screen"
```

## Viewport and scaling modes

Define a `Viewport` with your game's virtual resolution (for example, `320×180`) and choose how it adapts to different window aspect ratios via `Mode`:

```go
vp := screen.Viewport{
	Width:    320,
	Height:   180,
	Mode:     screen.ModeFit,
	BarColor: core.RGB(0x0b0e14),
}
```

| Mode | Behavior |
| --- | --- |
| `ModeFit` | Scales uniformly so the entire virtual canvas fits inside the window, centered, with letterbox or pillarbox bars (default). |
| `ModeFill` | Scales uniformly so the virtual canvas covers the whole window, centered, clipping overflow. |
| `ModeStretch` | Scales X and Y independently to fill the window with no bars or clipping. |
| `ModeExpand` | Scales uniformly to match the tighter axis and expands `VirtualSize` along the longer axis so the window fills with no bars or cropping. |

Wrap your canvas drawing in `vp.Draw(d, size, fn)`:

```go
return core.Canvas{
	Style: core.Style{FlexGrow: 1, FlexShrink: 1},
	Draw: func(d *core.Drawing, window core.Size) {
		vp.Draw(d, window, func(d *core.Drawing, virtualSize core.Size) {
			gameScene.Draw(d)
			_ = virtualSize // equals (320, 180) except in ModeExpand
		})
	},
}
```

`vp.Draw` draws any letterbox/pillarbox bars (`BarColor` defaults to opaque black; use `core.Transparent` to skip drawing bars), clips overflow when needed, applies translation and scale, runs `fn`, and restores `d`'s state. Inspect the computed `Rect`, `VirtualSize`, `ScaleX`, `ScaleY`, and `Bars` directly with `vp.Layout(window)`.

## Pixel-art integer scaling and snapping

For crisp pixel art without shimmer or uneven texels, enable `IntegerScale` and `PixelSnap`:

```go
vp := screen.Viewport{
	Width:        320,
	Height:       180,
	IntegerScale: true, // uses largest integer scale >= 1 that fits; falls back to fractional Fit if window < 1x
	PixelSnap:    true, // aligns viewport offsets and rounds Snap/SnapPoint to whole virtual pixels
}

// Round interpolated sprite or camera coordinates when PixelSnap is enabled:
drawX, drawY := vp.Snap(lerpX, lerpY)
camPos := vp.SnapPoint(targetPos)
```

Unconditional helpers `screen.Snap(x, y)` and `screen.SnapPoint(p)` round to whole virtual pixels regardless of viewport flags.

## Converting pointer coordinates

Convert view-space pointer coordinates (from `core.PressEvent`, `core.TouchEvent`, or `core.MouseEvent`) into virtual game coordinates with `ViewToGame`, and back with `GameToView`:

```go
if pt, ok := vp.ViewToGame(windowSize, ev.Position); ok {
	// pt is inside the virtual canvas; ok is false on letterbox bars or outside the window
	handleClick(pt)
}
```

`ViewToGame` pairs directly with [`input.Actions.MapPoint`](input.md#mapping-view-coordinates-to-a-virtual-canvas).

## Safe area for mobile HUDs

Keep screen-space HUD buttons and readouts clear of notches, rounded corners, status bars, and home indicators using `SafeGameRectForEnv` (or `SafeGameRect` with explicit `core.Edges`):

```go
safe := vp.SafeGameRectForEnv(cx.Env())
// safe is clamped to the visible virtual canvas in virtual game coordinates
scoreX := safe.X + 8
scoreY := safe.Y + 8
```

Called inside a reactive computation or `core.Canvas.Draw`, `SafeGameRectForEnv` and `SafeRectForEnv` subscribe automatically to window resize and safe-area inset changes. For native NSGo overlay views in view coordinates, use `screen.SafeRect(window, insets)` or `screen.SafeRectForEnv(cx.Env())`.
