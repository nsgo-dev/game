// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/fx.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import "math"

// Frame is W x H pixels whose top-left sits at (OX, OY) in world space. Color holds opaque (or, from Painter.Blend, translucent) pixels and Light additive glow, both as colors; Composite puts them together. A frame is the same shape as Sprite Fusion's VFX frames.
type Frame struct {
	W, H   int
	OX, OY int
	Color  []uint32
	Light  []uint32

	painter *Painter
}

// NewFrame returns a clear w x h frame with its top-left at (ox, oy).
func NewFrame(w, h, ox, oy int) *Frame {
	return &Frame{W: w, H: h, OX: ox, OY: oy, Color: make([]uint32, w*h), Light: make([]uint32, w*h)}
}

// Clear empties the frame's color and light.
func (f *Frame) Clear() *Frame {
	clear(f.Color)
	clear(f.Light)
	return f
}

// Painter returns the frame's painter, reset: one per frame, reused, as pixelfx's painterFor gives it.
func (f *Frame) Painter() *Painter {
	if f.painter == nil {
		f.painter = &Painter{Frame: f}
	}
	return f.painter.Reset()
}

// Composite writes the frame's color over a background, plus its light scaled by lightK (1 is as drawn), into out as RGBA bytes, w * h * 4 of them, and returns it; a nil or short out is replaced by a new slice. bg is nil for transparency, one color for a flat background, or w * h colors. Over transparency, light becomes color with alpha (its brightest channel), which reads as additive over dark backgrounds. The bytes are straight (not premultiplied) alpha, as PNG stores them.
func Composite(f *Frame, out []byte, bg []uint32, lightK float64) []byte {
	n := f.W * f.H
	if len(out) < n*4 {
		out = make([]byte, n*4)
	}
	flat := len(bg) == 1
	for i := range n {
		c, l := f.Color[i], f.Light[i]&0xffffff
		var r, g, b, a float64
		ca := c >> 24
		switch {
		case ca == 255:
			r, g, b, a = float64(c&255), float64((c>>8)&255), float64((c>>16)&255), 255
		case bg == nil:
			// translucent color over nothing keeps its alpha
			r, g, b, a = float64(c&255), float64((c>>8)&255), float64((c>>16)&255), float64(ca)
		default:
			k := bg[0]
			if !flat {
				k = bg[i]
			}
			fa := float64(ca) / 255
			r = float64(c&255)*fa + float64(k&255)*(1-fa)
			g = float64((c>>8)&255)*fa + float64((k>>8)&255)*(1-fa)
			b = float64((c>>16)&255)*fa + float64((k>>16)&255)*(1-fa)
			a = 255
		}
		if l != 0 {
			lr, lg, lb := float64(l&255)*lightK, float64((l>>8)&255)*lightK, float64((l>>16)&255)*lightK
			if a < 255 {
				// over transparency: premultiply what's there, add the light, divide by the new alpha
				pa := a / 255
				pr, pg, pb := r*pa+lr, g*pa+lg, b*pa+lb
				na := math.Min(1, math.Max(pa, math.Max(lr, math.Max(lg, lb))/255))
				if na > 0 {
					r, g, b, a = math.Min(255, pr/na), math.Min(255, pg/na), math.Min(255, pb/na), na*255
				}
			} else {
				r, g, b = math.Min(255, r+lr), math.Min(255, g+lg), math.Min(255, b+lb)
			}
		}
		j := i * 4
		out[j], out[j+1], out[j+2], out[j+3] = clampByte(r), clampByte(g), clampByte(b), clampByte(a)
	}
	return out
}

// clampByte stores x as a Uint8ClampedArray does: held to 0..255 and rounded half to even, NaN as 0.
func clampByte(x float64) byte {
	if !(x > 0) {
		return 0
	}
	if x >= 255 {
		return 255
	}
	return byte(math.RoundToEven(x))
}

// EdgeFade eases the frame's light out over the last e pixels of each edge, in place, leaving color alone: a glow cut off by the canvas edge shows as a box once a frame sits in a game. pixelfx's sprite sheets fade 8 pixels.
func EdgeFade(f *Frame, e int) {
	w, h, L := f.W, f.H, f.Light
	E := float64(e)
	for y := 0; y < h; y++ {
		ey := min(y, h-1-y)
		for x := 0; x < w; x++ {
			d := min(ey, x, w-1-x)
			if d >= e {
				x = w - 1 - e
				continue
			}
			i := y*w + x
			l := L[i]
			if l&0xffffff == 0 {
				continue
			}
			u := (float64(d) + 0.5) / E
			k := u * u * (3 - 2*u)
			L[i] = l&0xff000000 | uint32(ToInt32(Round(float64(l&255)*k))) | uint32(ToInt32(Round(float64((l>>8)&255)*k)))<<8 | uint32(ToInt32(Round(float64((l>>16)&255)*k)))<<16
		}
	}
}
