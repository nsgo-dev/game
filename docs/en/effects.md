# effects

`game/effects` draws a game's frame through a chain of shader passes, in order, with uniforms animated on the platform's frame clock. The frame is usually a pixel buffer the game writes each frame (`core.ImageSource.Pixels`), which NSGo shaders read since [nsgo#1052](https://github.com/nsgo-dev/nsgo/issues/1052).

```go
import "github.com/nsgo-dev/game/packages/game/effects"
```

## Write an effect

An effect is a fragment program in Kage, Go's syntax for shaders (`core.CompileShader`). It defines `Effect`, which returns the premultiplied color of a point of the frame, in the frame's pixels from its top left corner, and reads the pass before it with `Input(pos)` and the frame's size with `InputSize()`. The first effect's `Input` reads the frame itself: the pixel under the point, transparent outside the frame. Exported package-level variables are the effect's uniforms.

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

An effect does not declare `Input`, `InputSize` or `Fragment`, imports nothing, reads the frame only through `Input` (not `image0At`), works in the frame's pixels (not the view's `rectSize` or `pixelScale`), and uses no name that starts with `__`, which the shader compiler keeps, or with `p` or `P`, digits and `_`, which the chain gives its declarations.

## Chain effects

`Compile` fuses effects into one program, the first drawn over the frame and each next over the one before it. Each effect's `Input` calls the effect before it, so the passes run in one draw with no texture between them. Each source is compiled on its own first, so an error names the effect and gives the line and column in its source.

```go
var post = effects.MustCompile(
	effects.Effect{Name: "invert", Source: invert},
	effects.Effect{Name: "smear", Source: smear},
)
```

- Each effect keeps its own names: two effects may both declare `Amount`, a helper `func blend` or a `const half`, and their uniforms are set per effect.
- A pass that reads its input at several points runs the passes before it at each of them: a 2-tap pass over a 2-tap pass reads the frame 4 times. Put the passes that read many points first, or keep chains short.
- `Compile()` of no effects draws the frame as it is. `Chain.Source` returns the fused program, the same for the same effects.

## Draw the game view

`effects.View` is a `core.Shader` whose image is the frame, stretched over its box. Give the box the frame's aspect ratio for square pixels, as `screen.Viewport` places a virtual canvas. `Uniforms` holds each effect's values in the chain's order; `FrameFunc` and `UniformsFunc` follow signals.

```go
frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
version := core.NewSignal(cx, uint64(0))
// Each frame: draw into frame, then version.Set(version.GetUntracked() + 1).

return effects.View{
	Chain:     post,
	FrameFunc: func() core.ImageSource { return core.ImageSource{Pixels: frame, Version: version.Get()} },
	Uniforms:  []core.Uniforms{{"Amount": 0.6}, {"Offset": 1}},
	Style:     core.Style{Width: core.Pt(640), Height: core.Pt(360)},
}
```

A nil `Chain` draws the frame as it is. `Chain.Uniforms` maps per-effect uniforms onto the program's names when you build a `core.Shader` yourself, and panics on a name an effect does not declare.

## Animate on the frame clock

A uniform set to a `core.AnimatedNode` follows the platform's frame clock with no commit per frame. `Clock(cx, period)` returns a value counting seconds from 0 up to `period` and from 0 again; pick a period after which the effect looks as it started, as the value jumps back to 0.

```go
pulse := effects.Clock(cx, 2*time.Second)
effects.View{Chain: post, Frame: src, Uniforms: []core.Uniforms{{"Amount": pulse}, {"Offset": 1}}}
```

Any other node works too, such as a `core.Timing` a game starts when the player is hit. Values that change only now and then can come from `UniformsFunc`, at the cost of a commit each change.

## Bloom

`Bloom()` returns a ready effect that makes the frame's highlights glow in one pass, with no texture between passes: over each point it adds the bright part of its input at the point and at 16 points around it, on an inner ring at half the radius and an outer ring at the radius. Its uniforms are `Threshold`, the luminance from 0 to 1 above which a pixel glows, clamped to that range; `Intensity`, how much glow is added, where 0 or less, the default, passes its input through unchanged, 1 to 2 is a strong bloom and values above 1000 act as 1000; and `Radius`, how far in the frame's pixels the glow reaches.

```go
post := effects.MustCompile(effects.Bloom(), effects.Effect{Name: "invert", Source: invert})
effects.View{Chain: post, Frame: src, Uniforms: []core.Uniforms{{"Threshold": 0.6, "Intensity": 1.5, "Radius": 4}}}
```

- The glow adds light: colors stay premultiplied, and a translucent pixel near a highlight becomes more opaque.
- `Threshold` is compared with the luminance of the premultiplied color, so a translucent pixel glows in proportion to its alpha: white at half alpha has a luminance of 0.5 and stays below a threshold of 0.6.
- Each point reads its input 17 times, and a chain runs the effects before it at each of them, so put `Bloom` first or after effects that read their input once.
- It approximates a blurred bright pass: at a large radius the glow is a soft star rather than a smooth disc. Keep `Radius` to a few pixels of the frame, and animate `Intensity` for a pulse.
- `Bloom` carries its own `Reference`, so a chain with it draws a reference picture headless.

## CRT and scanlines

`CRT()` returns a ready effect that bends the frame onto a curved cathode-ray tube and draws horizontal scanlines along the curve in one pass. Its uniforms are `Curvature`, the barrel distortion strength, where 0 or less, the default, or NaN leaves the frame flat, 0.1 to 0.3 is a typical CRT curve and values above 4 act as 4; `Scanlines`, from 0 to 1, how dark the gaps between scanlines turn, where 0 leaves the brightness unchanged, 1 turns the troughs black and values outside 0 to 1 are clamped; and `Lines`, how many scanlines span the frame's height, where 0 or less, the default, draws one scanline per pixel row of the frame and values above 16384 act as 16384.

```go
post := effects.MustCompile(effects.Bloom(), effects.CRT(), effects.VignetteEffect())
effects.View{
	Chain:    post,
	Frame:    src,
	Uniforms: []core.Uniforms{{"Threshold": 0.6, "Intensity": 1.2, "Radius": 3}, {"Curvature": 0.25, "Scanlines": 0.35}},
	Style:    core.Style{Width: core.Pt(640), Height: core.Pt(360)},
}
```

- Setting `Curvature` without `Scanlines` curves the frame alone; setting `Scanlines` without `Curvature` draws flat scanlines; leaving both 0 or less passes the input through unchanged.
- The four edge midpoints stay pinned to the frame border while the corners pull inward onto an opaque black bezel. Inside the screen, scanlines darken RGB while keeping each pixel's alpha, so colors stay premultiplied.
- With `Lines` left 0, one scanline per pixel row shows as alternating bright and dark rows when the view scales the frame up (at 2x or higher); set `Lines` to half the frame's height or less when drawing a frame at 1:1.
- Each point reads its input at most once, so `CRT` can follow multi-tap passes such as `Bloom` without multiplying their taps, and chains with `VignetteEffect` when darkened corners are also wanted.
- `CRT` carries its own `Reference`, so a chain with it draws a reference picture headless.

## Vignette, flash and shake

The package ships three effects with their `Reference` functions, so a chain of them draws reference pictures. Each returns a fresh `Effect` to put in a chain.

| Effect | Uniforms | What it draws |
| --- | --- | --- |
| `VignetteEffect()` | `Strength`, `Radius`, `Softness`, `Color` | Turns the edges toward `Color` (black when left out), keeping each pixel's alpha. The distance runs from 0 at the center to 1 at the corners, round in the frame's pixels; the edges start to turn at `Radius` and turn all the way, eased, over `Softness`, as far as `Strength` (0 to 1). |
| `FlashEffect()` | `Amount`, `Color` | Turns the whole frame toward the opaque `Color` by `Amount`, 0 to 1. |
| `ShakeEffect()` | `Amplitude`, `Time`, `Frequency`, `Phase` | Moves the frame by up to `Amplitude` pixels along each direction, swaying `Frequency` times a second on `Time` (seconds), started by the four `Phase` angles, and reads the frame's edge where it moves off it. An offset that is not finite, or 1e30 pixels or more, leaves the frame in place. `ShakeOffset` gives the same offset in Go for uniforms set to plain numbers. |

A uniform left out is 0, so each draws the frame as it is until it is set. Put the shake first, so the passes after it draw over the moved frame, and the flash last:

```go
post := effects.MustCompile(effects.ShakeEffect(), effects.VignetteEffect(), effects.FlashEffect())
```

`NewFlash` and `NewShake` animate the flash's and the shake's uniforms on the frame clock, with no commit per frame. `Trigger` starts one, in place of one still running, and `Stop` ends it at once:

- `Flash.Trigger(peak, fade)` sets `Amount` to `peak` and fades it to 0 over `fade`, easing out.
- `Shake.Trigger(amplitude, d)` sets `Amplitude` and decays it to 0 over `d`, easing out, while `Time` runs from 0. Each trigger takes its next four phases from `ShakeConfig.Seed`, so a replay with the same seed shakes the same way. `ShakeConfig.Frequency` is 12 when left 0.

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

Flashes and shakes follow reduced motion: `Trigger` does nothing and returns false while `ReduceMotion` reports true. Pass `a11y.Assist`'s `ReduceMotion`, which follows the player's choice and else the platform's; when `ReduceMotion` is nil, the platform's setting is asked. A peak, amplitude or duration of 0 or less does not start one either, and leaves a running one alone: `Stop` ends it, unlike `tilemap.Camera.Shake`, which a zero intensity cancels.

## Reference pictures

Headless tests have no GPU, so `Chain.Reference(frame, width, height, uniforms)` draws the chain on the CPU with each effect's `Reference` function, at the per-effect uniforms a `View` would set, mapping each output pixel's center onto the frame as the GPU does. `Float` reads a number uniform, and a uniform left out is 0, as on the GPU. Compare the result with a golden PNG from `gametest`:

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

`Reference` returns an error when an effect has no `Reference`, for a uniform an effect does not declare, and for one set to an animated node such as `Clock`'s, whose value only the platform knows: pass the number to draw it at instead. A PNG stores straight colors, so convert the premultiplied picture to `*image.NRGBA` before `gametest.AssertGolden` when it has translucent pixels.
