// Package lights2d implements 2D lighting for NSGo games: point and spot lights
// composited over ambient darkness, exact 2D visibility-polygon shadows from
// occluders, and tangent-space normal-mapped sprite shading.
package lights2d

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	defaultFalloff = 2.0
	twoPi          = 2 * math.Pi
	// spotlightBands is the number of equal-step smoothstep bands used to
	// approximate angular cone falloff in Scene.Draw (max angular error <= 1/(2*spotlightBands)).
	spotlightBands = 16
)

var (
	// spotlightAdditiveFractions holds the normalized half-angle u in (0, 1)
	// for each concentric wedge, solving smoothstepCone(u) = (i+0.5)/N.
	spotlightAdditiveFractions [spotlightBands]float32
)

func init() {
	for i := range spotlightBands {
		w := (float64(i) + 0.5) / float64(spotlightBands)
		spotlightAdditiveFractions[i] = float32(invSmoothstepCone(w))
	}
}

// smoothstepCone returns the spotlight angular falloff factor in [0, 1] for
// normalized off-axis angle u = |angle - ConeAngle| / (ConeSpread / 2).
func smoothstepCone(u float64) float64 {
	if u <= 0 {
		return 1
	}
	if u >= 1 {
		return 0
	}
	return 1 - u*u*(3-2*u)
}

// invSmoothstepCone returns u in [0, 1] satisfying smoothstepCone(u) == w.
// Writing u = 0.5 - sin(theta) turns 1 - u^2*(3 - 2*u) into (1 + sin(3*theta))/2.
func invSmoothstepCone(w float64) float64 {
	if w <= 0 {
		return 1
	}
	if w >= 1 {
		return 0
	}
	return 0.5 - math.Sin(math.Asin(2*w-1)/3)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Light is a 2D point light or directional spotlight.
type Light struct {
	// X and Y are the light's position in world coordinates.
	X, Y float32
	// Radius is the maximum reach of the light. Non-positive values emit nothing.
	Radius float32
	// Color is the light's color (its alpha further scales brightness).
	Color core.Color
	// Intensity scales the light's brightness. Non-positive values emit nothing.
	Intensity float32
	// Falloff is the distance attenuation exponent in (1 - d/Radius)^Falloff.
	// Values <= 0 default to 2 (smooth quadratic falloff).
	Falloff float32
	// ConeAngle is the spotlight direction in radians (0 = +X, pi/2 = +Y).
	ConeAngle float32
	// ConeSpread is the total angular width of the spotlight cone in radians.
	// When ConeSpread <= 0 or >= 2*pi, the light is omnidirectional.
	ConeSpread float32
	// CastShadows clips the light against the scene's occluders.
	CastShadows bool
	// Enabled turns the light on or off.
	Enabled bool
}

// NewLight returns an enabled omnidirectional light with Intensity 1 and
// quadratic Falloff (2).
func NewLight(x, y, radius float32, color core.Color) *Light {
	return &Light{
		X:         x,
		Y:         y,
		Radius:    radius,
		Color:     color,
		Intensity: 1,
		Falloff:   defaultFalloff,
		Enabled:   true,
	}
}

func (l *Light) isSpotlight() bool {
	return l.ConeSpread > 0 && l.ConeSpread < twoPi
}

func (l *Light) effectiveFalloff() float64 {
	if l.Falloff > 0 {
		return float64(l.Falloff)
	}
	return defaultFalloff
}

// AttenuationAt returns the combined distance and spotlight-cone attenuation
// factor in [0, 1] at world point (x, y). It returns 0 when the light is
// disabled, has non-positive Radius or Intensity, or (x, y) lies outside the
// light's radius or spotlight cone.
func (l *Light) AttenuationAt(x, y float32) float32 {
	if l == nil || !l.Enabled || l.Radius <= 0 || l.Intensity <= 0 {
		return 0
	}
	dx := float64(x - l.X)
	dy := float64(y - l.Y)
	r := float64(l.Radius)
	distSq := dx*dx + dy*dy
	if distSq >= r*r {
		return 0
	}
	dist := math.Sqrt(distSq)
	t := dist / r
	atten := math.Pow(1-t, l.effectiveFalloff())

	if l.isSpotlight() && dist > 1e-6 {
		half := float64(l.ConeSpread) * 0.5
		angle := math.Atan2(dy, dx)
		diff := math.Abs(wrapAngle64(angle - float64(l.ConeAngle)))
		if diff >= half {
			return 0
		}
		u := diff / half
		// Smoothstep angular falloff: 1 at center of beam, 0 at cone edge.
		atten *= smoothstepCone(u)
	}
	return float32(atten)
}

// Scene holds the ambient darkness, lights, and shadow-casting occluders for a
// 2D lighting pass.
type Scene struct {
	// Ambient is the ambient darkness tint and alpha drawn over the scene bounds,
	// and the baseline ambient illumination in EvaluateAt and normal-map shading.
	Ambient core.Color
	// Lights are the point and spot lights in the scene.
	Lights []*Light
	// Occluders block light from lights with CastShadows enabled.
	Occluders []Occluder
	// LightComposite sets how light glows composite onto the drawing.
	// Zero defaults to core.CompositeLighter (additive) unless set explicitly
	// with SetLightComposite. Assign core.CompositeScreen for softer
	// screen-blended lights, or call SetLightComposite(core.CompositeSourceOver)
	// for alpha-over glows.
	LightComposite core.CompositeOp
	// DisableShadows skips occlusion for every light, as though none had CastShadows, without changing the lights; a low quality preset sets it (quality.Preset.Shadows false).
	DisableShadows bool

	lightCompositeSet bool
}

// SetLightComposite sets how light glows composite onto the drawing, including
// core.CompositeSourceOver (whose zero value is otherwise treated as unset and
// defaults to core.CompositeLighter).
func (s *Scene) SetLightComposite(op core.CompositeOp) {
	if s == nil {
		return
	}
	s.LightComposite = op
	s.lightCompositeSet = true
}

// EffectiveLightComposite returns the composite operation Draw uses for light
// glows: s.LightComposite when non-zero or set with SetLightComposite, and
// core.CompositeLighter otherwise.
func (s *Scene) EffectiveLightComposite() core.CompositeOp {
	if s == nil {
		return core.CompositeLighter
	}
	if s.LightComposite == 0 && !s.lightCompositeSet {
		return core.CompositeLighter
	}
	return s.LightComposite
}

// shadowOccluders returns the occluders shadow-casting lights test against: none when DisableShadows is set.
func (s *Scene) shadowOccluders() []Occluder {
	if s.DisableShadows {
		return nil
	}
	return s.Occluders
}

// Draw renders the ambient darkness overlay followed by all active light glows
// into d, clipped to bounds. Lights with CastShadows enabled are clipped to
// their exact 2D visibility polygon against s.Occluders.
//
// Draw leaves d's graphics state as it found it.
func (s *Scene) Draw(d *core.Drawing, bounds core.Bounds) {
	if s == nil || d == nil {
		return
	}
	bx := float32(bounds.Origin.X)
	by := float32(bounds.Origin.Y)
	bw := float32(bounds.Size.Width)
	bh := float32(bounds.Size.Height)
	if bw <= 0 || bh <= 0 {
		return
	}

	d.Save()
	d.BeginPath()
	d.Rect(bx, by, bw, bh)
	d.Clip()

	if _, _, _, aa := s.Ambient.Components(); aa > 0 {
		d.SetComposite(core.CompositeSourceOver)
		d.SetFillColor(s.Ambient)
		d.FillRect(bx, by, bw, bh)
	}

	comp := s.EffectiveLightComposite()

	bMaxX := bx + bw
	bMaxY := by + bh

	for _, l := range s.Lights {
		if l == nil || !l.Enabled || l.Radius <= 0 || l.Intensity <= 0 {
			continue
		}
		// Cull lights whose bounding box does not overlap bounds.
		if l.X+l.Radius < bx || l.X-l.Radius > bMaxX || l.Y+l.Radius < by || l.Y-l.Radius > bMaxY {
			continue
		}
		_, _, _, la := l.Color.Components()
		if la <= 0 {
			continue
		}

		d.Save()
		d.SetComposite(comp)
		if l.CastShadows && len(s.shadowOccluders()) > 0 {
			origin := core.Point{X: core.Points(l.X), Y: core.Points(l.Y)}
			poly := VisibilityPolygon(origin, l.Radius, s.Occluders)
			if len(poly) < 3 {
				d.Restore()
				continue
			}
			d.BeginPath()
			d.MoveTo(float32(poly[0].X), float32(poly[0].Y))
			for _, pt := range poly[1:] {
				d.LineTo(float32(pt.X), float32(pt.Y))
			}
			d.ClosePath()
			d.Clip()
		}

		if l.isSpotlight() {
			drawSpotlight(d, l, comp)
		} else if grad := lightGradient(l); grad != nil {
			d.SetFillGradient(grad)
			d.BeginPath()
			d.Arc(l.X, l.Y, l.Radius, 0, twoPi, false)
			d.ClosePath()
			d.Fill()
		}
		d.Restore()
	}

	d.Restore()
}

// EvaluateAt computes the lit RGB intensity at world point (x, y), combining
// ambient illumination with every active light's color, intensity, distance
// falloff, spotlight cone, and shadow occlusion.
func (s *Scene) EvaluateAt(x, y float32) (r, g, b float32) {
	if s == nil {
		return 0, 0, 0
	}
	ar, ag, ab, aa := s.Ambient.Components()
	r = float32(ar * aa)
	g = float32(ag * aa)
	b = float32(ab * aa)

	var occSegs []seg64
	occReady := false

	for _, l := range s.Lights {
		if l == nil || !l.Enabled || l.Radius <= 0 || l.Intensity <= 0 {
			continue
		}
		atten := l.AttenuationAt(x, y)
		if atten <= 0 {
			continue
		}
		if l.CastShadows && len(s.shadowOccluders()) > 0 {
			if !occReady {
				occSegs = appendOccluderSegments(nil, s.Occluders)
				occReady = true
			}
			if !visibleSegments(float64(l.X), float64(l.Y), float64(x), float64(y), occSegs) {
				continue
			}
		}
		lr, lg, lb, la := l.Color.Components()
		if la <= 0 {
			continue
		}
		scale := float32(la) * l.Intensity * atten
		r += float32(lr) * scale
		g += float32(lg) * scale
		b += float32(lb) * scale
	}
	return r, g, b
}

var (
	gradientOffsets        = [6]float32{0.0, 0.2, 0.4, 0.6, 0.8, 1.0}
	spotlightRadialOffsets = [9]float32{0.0, 0.05, 0.1, 0.2, 0.35, 0.5, 0.65, 0.8, 1.0}
)

func lightGradient(l *Light) *core.CanvasGradient {
	_, _, _, la := l.Color.Components()
	if la <= 0 {
		return nil
	}
	rgb := uint32(l.Color) >> 8
	falloff := l.effectiveFalloff()
	stops := make([]core.GradientStop, len(gradientOffsets))
	for i, off := range gradientOffsets {
		var alpha float64
		if off < 1.0 {
			atten := math.Pow(1-float64(off), falloff)
			alpha = clamp01(la * float64(l.Intensity) * atten)
		}
		stops[i] = core.GradientStop{
			Offset: off,
			Color:  core.RGBA(rgb, float32(alpha)),
		}
	}
	return core.CanvasRadial(l.X, l.Y, 0, l.X, l.Y, l.Radius, stops...)
}

// spotlightDeltaGradient builds the radial gradient for one concentric additive
// spotlight wedge transitioning the cumulative angular weight from wPrev to wNext.
// Stop alphas are quantized cumulatively to 8-bit steps so summing any prefix of
// wedges stays within 0.5/255 of the target smoothstep intensity without losing
// dim tails or prematurely clamping intensities above 1.
func spotlightDeltaGradient(l *Light, wPrev, wNext float64) *core.CanvasGradient {
	_, _, _, la := l.Color.Components()
	if la <= 0 || wNext <= wPrev {
		return nil
	}
	rgb := uint32(l.Color) >> 8
	falloff := l.effectiveFalloff()
	stops := make([]core.GradientStop, len(spotlightRadialOffsets))
	anyNonZero := false
	for i, off := range spotlightRadialOffsets {
		var qDelta uint32
		if off < 1.0 {
			radial := la * float64(l.Intensity) * math.Pow(1-float64(off), falloff)
			qPrev := uint32(clamp01(radial*wPrev)*255 + 0.5)
			qNext := uint32(clamp01(radial*wNext)*255 + 0.5)
			if qNext > qPrev {
				qDelta = qNext - qPrev
				anyNonZero = true
			}
		}
		stops[i] = core.GradientStop{
			Offset: off,
			Color:  core.Color(rgb<<8 | qDelta),
		}
	}
	if !anyNonZero {
		return nil
	}
	return core.CanvasRadial(l.X, l.Y, 0, l.X, l.Y, l.Radius, stops...)
}

// spotlightOverGradient builds one concentric wedge gradient whose telescoping
// source-over composition transitions cumulative alpha from T(wPrev) to T(wNext):
// a = (T_next - T_prev) / (1 - T_prev).
func spotlightOverGradient(l *Light, wPrev, wNext float64) *core.CanvasGradient {
	_, _, _, la := l.Color.Components()
	if la <= 0 || wNext <= wPrev {
		return nil
	}
	rgb := uint32(l.Color) >> 8
	falloff := l.effectiveFalloff()
	stops := make([]core.GradientStop, len(spotlightRadialOffsets))
	anyNonZero := false
	for i, off := range spotlightRadialOffsets {
		var alpha float64
		if off < 1.0 {
			radial := la * float64(l.Intensity) * math.Pow(1-float64(off), falloff)
			tPrev := clamp01(radial * wPrev)
			tNext := clamp01(radial * wNext)
			if tNext > tPrev && tPrev < 1.0 {
				alpha = clamp01((tNext - tPrev) / (1.0 - tPrev))
			}
		}
		c := core.RGBA(rgb, float32(alpha))
		if c&0xff != 0 {
			anyNonZero = true
		}
		stops[i] = core.GradientStop{
			Offset: off,
			Color:  c,
		}
	}
	if !anyNonZero {
		return nil
	}
	return core.CanvasRadial(l.X, l.Y, 0, l.X, l.Y, l.Radius, stops...)
}

// spotlightScreenGradient builds one concentric wedge gradient whose telescoping
// screen composition transitions each channel from L_c*T(wPrev) to L_c*T(wNext):
// src_c = L_c*(T_next - T_prev) / (1 - L_c*T_prev).
func spotlightScreenGradient(l *Light, wPrev, wNext float64) *core.CanvasGradient {
	lr, lg, lb, la := l.Color.Components()
	if la <= 0 || wNext <= wPrev {
		return nil
	}
	falloff := l.effectiveFalloff()
	stops := make([]core.GradientStop, len(spotlightRadialOffsets))
	anyNonZero := false
	for i, off := range spotlightRadialOffsets {
		c := core.Color(uint32(l.Color) &^ 0xff)
		if off < 1.0 {
			radial := la * float64(l.Intensity) * math.Pow(1-float64(off), falloff)
			tPrev := clamp01(radial * wPrev)
			tNext := clamp01(radial * wNext)
			if tNext > tPrev {
				step := func(ch float64) float64 {
					p := clamp01(ch * tPrev)
					n := clamp01(ch * tNext)
					if n <= p || p >= 1.0 {
						return 0
					}
					return clamp01((n - p) / (1.0 - p))
				}
				sr, sg, sb := step(lr), step(lg), step(lb)
				alpha := max(sr, max(sg, sb))
				if alpha > 0 {
					ur := uint32(clamp01(sr/alpha)*255 + 0.5)
					ug := uint32(clamp01(sg/alpha)*255 + 0.5)
					ub := uint32(clamp01(sb/alpha)*255 + 0.5)
					ua := uint32(clamp01(alpha)*255 + 0.5)
					if ua > 0 {
						anyNonZero = true
					}
					c = core.Color(ur<<24 | ug<<16 | ub<<8 | ua)
				}
			}
		}
		stops[i] = core.GradientStop{
			Offset: off,
			Color:  c,
		}
	}
	if !anyNonZero {
		return nil
	}
	return core.CanvasRadial(l.X, l.Y, 0, l.X, l.Y, l.Radius, stops...)
}

// drawSpotlight renders a directional spotlight as spotlightBands concentric
// nested wedges whose angular widths invert smoothstepCone into equal-height
// steps. Concentric wedges avoid radial anti-aliasing seams, and each composite
// mode uses its telescoping per-wedge gradient so the combined result matches
// a single smoothstep-modulated light pass.
func drawSpotlight(d *core.Drawing, l *Light, comp core.CompositeOp) {
	half := l.ConeSpread * 0.5
	invN := 1.0 / float64(spotlightBands)
	for i := range spotlightBands {
		wPrev := float64(i) * invN
		wNext := float64(i+1) * invN
		var grad *core.CanvasGradient
		switch comp {
		case core.CompositeLighter:
			grad = spotlightDeltaGradient(l, wPrev, wNext)
		case core.CompositeScreen:
			grad = spotlightScreenGradient(l, wPrev, wNext)
		default:
			grad = spotlightOverGradient(l, wPrev, wNext)
		}
		if grad == nil {
			continue
		}
		halfW := half * spotlightAdditiveFractions[i]
		d.SetFillGradient(grad)
		d.BeginPath()
		d.MoveTo(l.X, l.Y)
		d.Arc(l.X, l.Y, l.Radius, l.ConeAngle-halfW, l.ConeAngle+halfW, false)
		d.ClosePath()
		d.Fill()
	}
}
