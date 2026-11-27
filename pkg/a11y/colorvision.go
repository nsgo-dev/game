package a11y

import (
	"math"

	"github.com/nsgo-dev/game/pkg/postfx"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
)

func toEffectsColorVisionMode(mode ColorVision) postfx.ColorVisionMode {
	switch mode {
	case VisionProtanopia:
		return postfx.ColorVisionProtanopia
	case VisionDeuteranopia:
		return postfx.ColorVisionDeuteranopia
	case VisionTritanopia:
		return postfx.ColorVisionTritanopia
	case VisionHighContrast:
		return postfx.ColorVisionHighContrast
	default:
		return postfx.ColorVisionNone
	}
}

// ColorVisionFilter returns the daltonization / color-vision correction frame effect
// configured from settings.ColorBlindMode and settings.EffectiveColorBlindSeverity().
// When ColorBlindMode is "none" (or unrecognized) or the effective severity is 0 or
// less, it returns a zero Effect and false so callers can omit the pass.
func ColorVisionFilter(settings save.AccessibilitySettings) (postfx.Effect, bool) {
	mode, ok := ParseColorVision(settings.ColorBlindMode)
	if !ok || mode == VisionNormal {
		return postfx.Effect{}, false
	}
	severity := settings.EffectiveColorBlindSeverity()
	if !(severity > 0) {
		return postfx.Effect{}, false
	}
	return postfx.ColorVisionEffect(toEffectsColorVisionMode(mode), severity, true), true
}

// ColorVisionSimulation returns a frame effect that simulates how a scene appears
// under mode at severity in [0, 1] (clamped, with NaN treated as 0). VisionNormal,
// VisionHighContrast, and severity <= 0 return a zero Effect and false.
func ColorVisionSimulation(mode ColorVision, severity float64) (postfx.Effect, bool) {
	if mode != VisionProtanopia && mode != VisionDeuteranopia && mode != VisionTritanopia {
		return postfx.Effect{}, false
	}
	if math.IsNaN(severity) || severity <= 0 {
		return postfx.Effect{}, false
	}
	if severity > 1 {
		severity = 1
	}
	return postfx.ColorVisionEffect(toEffectsColorVisionMode(mode), severity, false), true
}

// SimulateAt approximates how c appears under color-vision mode at severity in [0, 1]
// (clamped, NaN treated as 0). Alpha is preserved. VisionNormal, VisionHighContrast,
// and severity <= 0 return c unchanged. SimulateAt(c, mode, 1.0) matches Simulate(c, mode).
func SimulateAt(c core.Color, mode ColorVision, severity float64) core.Color {
	if math.IsNaN(severity) || severity <= 0 {
		return c
	}
	if mode != VisionProtanopia && mode != VisionDeuteranopia && mode != VisionTritanopia {
		return c
	}
	m := postfx.ColorVisionMatrix(toEffectsColorVisionMode(mode), severity, false)
	return applyLinearMatrixColor(c, m)
}

// Daltonize transforms c to shift lost chromatic contrast into channels visible under
// mode at severity in [0, 1] (clamped, NaN treated as 0). Alpha is preserved.
// For VisionHighContrast, it boosts sRGB contrast around mid-gray. VisionNormal and
// severity <= 0 return c unchanged.
func Daltonize(c core.Color, mode ColorVision, severity float64) core.Color {
	if math.IsNaN(severity) || severity <= 0 {
		return c
	}
	if severity > 1 {
		severity = 1
	}
	switch mode {
	case VisionProtanopia, VisionDeuteranopia, VisionTritanopia:
		m := postfx.ColorVisionMatrix(toEffectsColorVisionMode(mode), severity, true)
		return applyLinearMatrixColor(c, m)
	case VisionHighContrast:
		r, g, b, _ := c.Components()
		scale := 1.0 + 0.5*severity
		return packColor(
			clamp01((r-0.5)*scale+0.5),
			clamp01((g-0.5)*scale+0.5),
			clamp01((b-0.5)*scale+0.5),
			uint32(c&0xff),
		)
	default:
		return c
	}
}

func applyLinearMatrixColor(c core.Color, m [3][3]float64) core.Color {
	r, g, b, _ := c.Components()
	lr, lg, lb := srgbToLinear(r), srgbToLinear(g), srgbToLinear(b)
	sr := clamp01(m[0][0]*lr + m[0][1]*lg + m[0][2]*lb)
	sg := clamp01(m[1][0]*lr + m[1][1]*lg + m[1][2]*lb)
	sb := clamp01(m[2][0]*lr + m[2][1]*lg + m[2][2]*lb)
	return packColor(linearToSRGB(sr), linearToSRGB(sg), linearToSRGB(sb), uint32(c&0xff))
}

// ColorDistance returns the Euclidean sRGB distance between a and b in 8-bit channel
// units [0, 441.67], ignoring alpha.
func ColorDistance(a, b core.Color) float64 {
	ar, ag, ab, _ := a.Components()
	br, bg, bb, _ := b.Components()
	dr := (ar - br) * 255
	dg := (ag - bg) * 255
	db := (ab - bb) * 255
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// ColorBlindSeverity returns the color-vision filter strength in [0, 1], defaulting to 1.0.
func (a *Assist) ColorBlindSeverity() float64 {
	return a.load().EffectiveColorBlindSeverity()
}

// SetColorBlindSeverity clamps v to [0, 1] and persists it.
func (a *Assist) SetColorBlindSeverity(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		v = 1.0
	}
	v = clampFloat64(v, 0, 1)
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.SetColorBlindSeverity(v)
	})
}

// ColorVisionFilter returns the active color-vision correction frame effect for the
// player's current accessibility settings, or (zero, false) when ColorVision is
// VisionNormal or ColorBlindSeverity is 0.
func (a *Assist) ColorVisionFilter() (postfx.Effect, bool) {
	return ColorVisionFilter(a.load())
}
