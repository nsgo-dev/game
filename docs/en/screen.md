# screen

`game/screen` maps a fixed virtual canvas onto any window size and aspect ratio, converts pointer input between view and virtual coordinates, snaps pixel-art coordinates, computes safe-area rectangles for HUD layers, and applies the player's video settings.

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

## Orientation on phones

A game declares the orientation it plays in by calling NSGo's `cx.RequestOrientation` in the scope that shows it, usually the app's root. A wide virtual canvas asks for landscape, a tall one for portrait:

```go
func Root(cx *core.Scope) core.Node {
	cx.RequestOrientation(core.OrientationLandscape) // either landscape direction
	// ...
}
```

On an iPhone or an Android phone the interface turns to one of the requested orientations and stays there while that scope lives; when the scope ends, the app returns to the system's usual policy. The newest live request wins, so a portrait menu or a landscape mini-game can request its own orientation in its scope. Use `core.OrientationLandscapeLeft` or `core.OrientationLandscapeRight` alone to pin one direction, and `core.OrientationPortrait` (optionally with `core.OrientationPortraitUpsideDown`) for a portrait game.

The request does nothing on desktops. Android may disregard it in multi-window or large-screen modes, and a browser locks orientation only in full screen, which a press must enter (`desktop.ToggleFullScreen(cx)`); a refusal goes to the browser console. In `nsgotest`, `App.Orientation` holds the current request. The [Cave Run example](https://github.com/nsgo-dev/game/tree/main/examples/caverun) locks landscape this way. NSGo documents the API under [Interface orientation](https://github.com/nsgo-dev/nsgo/blob/main/docs/public/en/services.md#interface-orientation).

## Video settings

`Video` applies the display part of the player's settings (`save.DisplaySettings`) to the window and a `game.Loop`: windowed or borderless full screen on a chosen display, a cap on how many frames the loop draws a second, and, with `AutoDropLowPower`, a lower cap while the system is in low-power mode or under serious or critical thermal pressure. It ships no UI: a settings screen edits `save.Settings`, saves it and applies it again, and the game applies the saved settings at start:

```go
loop := &game.Loop{Update: update, Draw: draw}
video := &screen.Video{Loop: loop}
store := save.NewStorageSettingsStore(cx.Storage(), "mygame")

loop.Run(cx)
video.Run(cx)
settings, err := store.LoadAndApply(save.SettingsTargets{ApplyDisplay: video.Apply})
```

| Field | Effect |
| --- | --- |
| `Fullscreen` | Full screen on the chosen display, or windowed. |
| `DisplayID` | The display, by its ID in `cx.Env().Displays()`; stays the same across launches and display changes, so a UI that offers displays sets it. |
| `DisplayIndex` | The display by position in `Displays()` when `DisplayID` is empty or not connected; out of range keeps the window's current display. |
| `FrameCap` | Most frames drawn a second (the loop's `DrawRate`), uncapped when 0. Updates keep to `Loop.Rate`. |
| `AutoDropLowPower` | Lowers the cap to `Video.LowPowerCap` (30 when 0) while `cx.Env().LowPowerMode()` is on or `ThermalState()` is serious or critical, keeping a lower `FrameCap`, and restores it once the condition clears. |

`Video` asks the window to change full screen or display only when the settings change them, or on the first `Apply` when they differ from the window, so applying other settings does not undo a player leaving full screen with the system's own controls. Settings applied before `Run` take effect when it starts. `Video.FrameCap()` returns the cap in effect and `Video.Dropped()` whether it is lowered; `screen.DisplayID` and `screen.Constrained` expose the display choice and the power rule for a settings screen. Full screen applies on macOS, Windows and Linux; platforms without desktop windows ignore it, and the frame cap applies everywhere.
