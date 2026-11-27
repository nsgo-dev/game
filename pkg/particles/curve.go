package particles

import "github.com/nsgo-dev/nsgo/core"

// FloatStop is one control point on a FloatCurve at normalized particle age T in [0, 1].
type FloatStop struct {
	T     float32
	Value float32
}

// FloatCurve maps a particle's normalized age in [0, 1] to a float32 by linear interpolation between stops ordered by T.
// An empty curve returns 1 at any age.
type FloatCurve []FloatStop

// Curve is an alias for FloatCurve.
type Curve = FloatCurve

// At returns the curve's value at normalized age t, clamped to [0, 1].
func (c FloatCurve) At(t float32) float32 {
	if len(c) == 0 {
		return 1
	}
	t = clamp01(t)
	if len(c) == 1 || t <= c[0].T {
		return c[0].Value
	}
	last := c[len(c)-1]
	if t >= last.T {
		return last.Value
	}
	for i := 1; i < len(c); i++ {
		if t <= c[i].T {
			a, b := c[i-1], c[i]
			span := b.T - a.T
			if span <= 0 {
				return b.Value
			}
			return a.Value + (b.Value-a.Value)*((t-a.T)/span)
		}
	}
	return last.Value
}

// ColorStop is one control point on a ColorCurve at normalized particle age T in [0, 1].
type ColorStop struct {
	T     float32
	Color core.Color
}

// ColorCurve maps a particle's normalized age in [0, 1] to a core.Color by linear RGBA interpolation between stops ordered by T.
// An empty curve returns opaque white (0xffffffff).
type ColorCurve []ColorStop

// At returns the curve's color at normalized age t, clamped to [0, 1].
func (c ColorCurve) At(t float32) core.Color {
	if len(c) == 0 {
		return core.RGB(0xffffff)
	}
	t = clamp01(t)
	if len(c) == 1 || t <= c[0].T {
		return c[0].Color
	}
	last := c[len(c)-1]
	if t >= last.T {
		return last.Color
	}
	for i := 1; i < len(c); i++ {
		if t <= c[i].T {
			a, b := c[i-1], c[i]
			span := b.T - a.T
			if span <= 0 {
				return b.Color
			}
			return lerpColor(a.Color, b.Color, (t-a.T)/span)
		}
	}
	return last.Color
}

func lerpColor(a, b core.Color, t float32) core.Color {
	ar, ag, ab, aa := unpackColor(a)
	br, bg, bb, ba := unpackColor(b)
	r := clampByte(ar + (br-ar)*t)
	g := clampByte(ag + (bg-ag)*t)
	bl := clampByte(ab + (bb-ab)*t)
	al := clampByte(aa + (ba-aa)*t)
	return core.Color(r<<24 | g<<16 | bl<<8 | al)
}

func unpackColor(c core.Color) (r, g, b, a float32) {
	return float32(c >> 24 & 0xff),
		float32(c >> 16 & 0xff),
		float32(c >> 8 & 0xff),
		float32(c & 0xff)
}

func clampByte(v float32) uint32 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint32(v + 0.5)
}

func clamp01(v float32) float32 {
	if !(v > 0) {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}
