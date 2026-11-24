package effects

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// crtSource is CRT's Kage program. Its barrel warp, bezel cut and scanline wave are crtReference's, which draws it on the CPU.
const crtSource = `package main

// Curvature bends the frame outward like a curved tube: 0 or less, or NaN, leaves it flat, 0.1 to 0.3 is a typical CRT curve, and above 4 it acts as 4.
var Curvature float

// Scanlines darkens the gaps between horizontal scanlines, from 0, no scanlines, to 1, black gaps. Values outside 0 to 1 are clamped, and NaN acts as 0.
var Scanlines float

// Lines is how many scanlines span the frame's height: 0 or less, or NaN, draws one per pixel row of the frame, and above 16384 it acts as 16384.
var Lines float

func Effect(pos vec2) vec4 {
	k := Curvature
	if !(k > 0) {
		k = 0
	}
	if k > 4 {
		k = 4
	}
	s := Scanlines
	if !(s > 0) {
		s = 0
	}
	if s > 1 {
		s = 1
	}
	size := InputSize()
	if !(size.x > 0 && size.y > 0) {
		k = 0
		s = 0
	}
	samplePos := pos
	uv := vec2(0)
	if k > 0 || s > 0 {
		uv = pos / size
	}
	if k > 0 {
		p := uv*2.0 - vec2(1.0)
		p = vec2(p.x*(1.0+k*p.y*p.y), p.y*(1.0+k*p.x*p.x))
		uv = p*0.5 + vec2(0.5)
		samplePos = uv * size
		if !(samplePos.x >= 0 && samplePos.x < size.x && samplePos.y >= 0 && samplePos.y < size.y) {
			return vec4(0, 0, 0, 1)
		}
	}
	c := Input(samplePos)
	if s > 0 {
		lines := Lines
		if !(lines > 0) {
			lines = size.y
		}
		if lines > 16384 {
			lines = 16384
		}
		factor := 1.0 - s*(0.5-0.5*sin(6.283185307179586*uv.y*lines))
		c = vec4(c.rgb*factor, c.a)
	}
	return c
}
`

// CRT returns an effect that bends the frame onto a curved cathode-ray tube and draws horizontal scanlines along the curve in one pass.
//
// Its uniforms, by name:
//
//   - Curvature: barrel distortion strength. 0 or less, the default being 0, or NaN leaves the frame flat; 0.1 to 0.3 is a typical CRT curve; values above 4 act as 4. The four edge midpoints stay pinned to the frame border while the corners pull inward onto an opaque black bezel.
//   - Scanlines, from 0 to 1: how dark the gaps between horizontal scanlines turn, 0 leaving the brightness unchanged and 1 turning the troughs black. Values outside 0 to 1 are clamped, and NaN acts as 0.
//   - Lines: how many scanlines span the frame's height. 0 or less, the default being 0, or NaN draws one scanline per pixel row of the frame, which shows as alternating bright and dark rows when the view scales the frame up (at 2x or higher); set Lines to half the frame's height or less when drawing a frame at 1:1. Values above 16384 act as 16384.
//
// When both Curvature and Scanlines are 0 or less, CRT passes its input through unchanged. Inside the screen, scanlines darken RGB while keeping each pixel's alpha, so colors stay premultiplied; outside the curved tube, the bezel is opaque black. Each output point reads its input at most once, so CRT can follow multi-tap passes such as Bloom without multiplying their taps, and chains with VignetteEffect when darkened corners are also wanted. The effect has a Reference, so a chain of it draws a reference picture.
func CRT() Effect {
	return Effect{Name: "crt", Source: crtSource, Reference: crtReference}
}

const (
	crtMaxCurvature = 4
	crtMaxLines     = 16384
)

// crtReference draws CRT on the CPU, as its source draws it on the GPU, with the same explicit handling of NaN and infinite uniforms.
func crtReference(in Input, u core.Uniforms, x, y float64) [4]float64 {
	k := Float(u, "Curvature")
	if !(k > 0) {
		k = 0
	}
	if k > crtMaxCurvature {
		k = crtMaxCurvature
	}
	s := Float(u, "Scanlines")
	if !(s > 0) {
		s = 0
	}
	if s > 1 {
		s = 1
	}
	w, h := in.Size()
	if !(w > 0 && h > 0) {
		k = 0
		s = 0
	}
	sx, sy := x, y
	var uvx, uvy float64
	if k > 0 || s > 0 {
		uvx, uvy = x/w, y/h
	}
	if k > 0 {
		px, py := uvx*2-1, uvy*2-1
		wx, wy := px*(1+k*py*py), py*(1+k*px*px)
		uvx, uvy = wx*0.5+0.5, wy*0.5+0.5
		sx, sy = uvx*w, uvy*h
		if !(sx >= 0 && sx < w && sy >= 0 && sy < h) {
			return [4]float64{0, 0, 0, 1}
		}
	}
	c := in.At(sx, sy)
	if s > 0 {
		lines := Float(u, "Lines")
		if !(lines > 0) {
			lines = h
		}
		if lines > crtMaxLines {
			lines = crtMaxLines
		}
		factor := 1 - s*(0.5-0.5*math.Sin(2*math.Pi*uvy*lines))
		for i := range 3 {
			c[i] *= factor
		}
	}
	return c
}
