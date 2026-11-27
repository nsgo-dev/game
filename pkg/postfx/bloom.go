package postfx

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// bloomSource is Bloom's Kage program. Its taps, weights and luminance are bloomReference's, which draws it on the CPU.
const bloomSource = `package main

// Threshold is the luminance, from 0 to 1, of a premultiplied color above which a pixel glows: how far its luminance rises from Threshold toward 1 is how much of its color spreads. It is clamped to 0 to 1, and NaN acts as 0.
var Threshold float

// Intensity scales the glow added over the frame: 0 or less, or NaN, draws the frame as it is, and above 1000 it acts as 1000.
var Intensity float

// Radius is how far, in the frame's pixels, the glow reaches from a highlight.
var Radius float

// bright returns the part of c that glows above the threshold t.
func bright(c vec4, t float) vec4 {
	l := dot(c.rgb, vec3(0.2126, 0.7152, 0.0722))
	return c * clamp((l-t)/max(1-t, 0.0001), 0, 1)
}

func Effect(pos vec2) vec4 {
	c := Input(pos)
	if !(Intensity > 0) {
		return c
	}
	t := Threshold
	if !(t > 0) {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	glow := bright(c, t) * 4
	for i := 0; i < 8; i++ {
		a := float(i) * 0.7853981633974483
		glow += bright(Input(pos+vec2(cos(a), sin(a))*(Radius*0.5)), t) * 2
		b := a + 0.39269908169872414
		glow += bright(Input(pos+vec2(cos(b), sin(b))*Radius), t)
	}
	glow *= min(Intensity, 1000) / 28
	alpha := clamp(c.a+glow.a, 0, 1)
	return vec4(min(c.rgb+glow.rgb, vec3(alpha)), alpha)
}
`

// Bloom returns an effect that makes the frame's highlights glow in one pass: it adds, over each point, the bright part of the input at the point and at 16 points around it, an inner ring of 8 at half the radius and an outer ring of 8 at the radius turned half a step, weighted 4 at the center, 2 on the inner ring and 1 on the outer. It approximates a blurred bright pass without a texture between passes, so its glow is a soft star rather than a smooth disc at large radii.
//
// Its uniforms, by name:
//
//   - Threshold, from 0 to 1: the luminance (Rec. 709) above which a pixel glows, as much as its luminance rises from Threshold toward 1. It is the luminance of the premultiplied color, so a translucent pixel glows in proportion to its alpha: white at half alpha has a luminance of 0.5. Values outside 0 to 1 are clamped, and NaN acts as 0.
//   - Intensity: how much of the glow is added; 0 or less, the default being 0, or NaN returns the input unchanged, 1 to 2 is a strong bloom, and above 1000 it acts as 1000.
//   - Radius: how far, in the frame's pixels, the glow reaches.
//
// The glow adds light: a translucent pixel near a highlight becomes more opaque, and colors stay premultiplied. Each output point reads its input 17 times, and a chain runs the effects before it at each of those points, so put Bloom first in a chain or after effects that read their input once.
func Bloom() Effect {
	return Effect{Name: "bloom", Source: bloomSource, Reference: bloomReference}
}

// bloomTap is one of Bloom's taps: its offset as a fraction of the radius, and its weight.
type bloomTap struct {
	dx, dy, weight float64
}

// bloomTaps are Bloom's taps around the center, in its source's order.
var bloomTaps = func() []bloomTap {
	taps := make([]bloomTap, 0, 16)
	for i := range 8 {
		a := float64(i) * (math.Pi / 4)
		b := a + math.Pi/8
		taps = append(taps, bloomTap{math.Cos(a) * 0.5, math.Sin(a) * 0.5, 2}, bloomTap{math.Cos(b), math.Sin(b), 1})
	}
	return taps
}()

// bloomCenterWeight and bloomWeights are the center tap's weight and all taps' weights summed.
const (
	bloomCenterWeight = 4
	bloomWeights      = bloomCenterWeight + 8*2 + 8*1
)

// bloomReference draws Bloom on the CPU, as its source draws it on the GPU, with the same explicit handling of NaN and infinite uniforms.
func bloomReference(in Input, u core.Uniforms, x, y float64) [4]float64 {
	threshold, intensity, radius := Float(u, "Threshold"), Float(u, "Intensity"), Float(u, "Radius")
	c := in.At(x, y)
	if !(intensity > 0) {
		return c
	}
	if !(threshold > 0) {
		threshold = 0
	}
	threshold = math.Min(threshold, 1)
	bright := func(c [4]float64) [4]float64 {
		l := 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
		k := clamp01((l - threshold) / math.Max(1-threshold, 0.0001))
		for i := range c {
			c[i] *= k
		}
		return c
	}
	var glow [4]float64
	add := func(g [4]float64, w float64) {
		for i := range glow {
			glow[i] += g[i] * w
		}
	}
	add(bright(c), bloomCenterWeight)
	for _, t := range bloomTaps {
		add(bright(in.At(x+t.dx*radius, y+t.dy*radius)), t.weight)
	}
	scale := math.Min(intensity, 1000) / bloomWeights
	alpha := clamp01(c[3] + glow[3]*scale)
	out := [4]float64{3: alpha}
	for i := range 3 {
		out[i] = math.Min(c[i]+glow[i]*scale, alpha)
	}
	return out
}

// clamp01 clamps v to 0 to 1, as Kage's clamp(v, 0, 1); NaN stays NaN, which a picture draws as 0.
func clamp01(v float64) float64 {
	return math.Min(math.Max(v, 0), 1)
}
