package effects

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// vignetteSource is Vignette's program. Its distance runs from 0 at the frame's center to 1 at its corners, round in the frame's pixels.
const vignetteSource = `package main

// Strength is how far the corners turn to Color, 0 to 1.
var Strength float

// Radius is the distance from the center, 0 there and 1 at the corners, at which the edges start to turn.
var Radius float

// Softness is the distance over which they turn all the way.
var Softness float

// Color is the color the edges turn to, not premultiplied: black when left out.
var Color vec3

func Effect(pos vec2) vec4 {
	size := InputSize()
	d := length(pos-size*0.5) / max(length(size)*0.5, 0.0001)
	t := clamp((d-Radius)/max(Softness, 0.0001), 0.0, 1.0)
	k := t * t * (3.0 - 2.0*t) * clamp(Strength, 0.0, 1.0)
	c := Input(pos)
	return c + (vec4(clamp(Color, 0.0, 1.0)*c.a, c.a)-c)*k
}
`

// VignetteEffect returns an effect that turns the frame's edges toward a color, as an old lens or a hurt player's view does. Its uniforms are:
//
//   - Strength, how far the corners turn, from 0, which draws the frame as it is, to 1;
//   - Radius, the distance from the center at which the edges start to turn, 0 at the center and 1 at the corners, round in the frame's pixels;
//   - Softness, the distance over which they turn all the way, eased; 0 draws a hard edge;
//   - Color, a core.Color the edges turn to, black when left out; it keeps each pixel's alpha.
//
// Each is a number or an AnimatedNode, so a game can close the vignette in on the frame clock. The effect has a Reference, so a chain of it draws a reference picture.
func VignetteEffect() Effect {
	return Effect{Name: "vignette", Source: vignetteSource, Reference: vignetteReference}
}

// vignetteReference is vignetteSource on the CPU.
func vignetteReference(in Input, u core.Uniforms, x, y float64) [4]float64 {
	w, h := in.Size()
	d := math.Hypot(x-w*0.5, y-h*0.5) / math.Max(math.Hypot(w, h)*0.5, 0.0001)
	t := clamp01((d - Float(u, "Radius")) / math.Max(Float(u, "Softness"), 0.0001))
	k := t * t * (3 - 2*t) * clamp01(Float(u, "Strength"))
	color := vector(u, "Color", 3)
	c := in.At(x, y)
	for i := range 3 {
		c[i] += (clamp01(color[i])*c[3] - c[i]) * k
	}
	return c
}

// clamp01 is Kage's clamp(v, 0.0, 1.0), with NaN as 0.
func clamp01(v float64) float64 {
	if !(v > 0) {
		return 0
	}
	return math.Min(v, 1)
}
