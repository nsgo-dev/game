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

An effect does not declare `Input`, `InputSize` or `Fragment`, imports nothing, reads the frame only through `Input` (not `image0At`), and uses no name that starts with `p` or `P`, digits and `_`, which the chain gives its declarations.

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

## Reference pictures

Headless tests have no GPU, so `Chain.Reference(frame, width, height)` draws the chain on the CPU with each effect's `Reference` function, mapping each output pixel's center onto the frame as the GPU does. Compare the result with a golden PNG from `gametest`:

```go
// The invert at Amount 1.
invertRef := func(in effects.Input, x, y float64) [4]float64 {
	c := in.At(x, y)
	return [4]float64{c[3] - c[0], c[3] - c[1], c[3] - c[2], c[3]}
}
chain := effects.MustCompile(effects.Effect{Source: invert, Reference: invertRef}, ...)
got, err := chain.Reference(frame, 640, 360)
```

`Reference` returns an error when an effect has no `Reference`. A PNG stores straight colors, so convert the premultiplied picture to `*image.NRGBA` before `gametest.AssertGolden` when it has translucent pixels.
