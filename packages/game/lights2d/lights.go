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
)

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
		cone := 1 - u*u*(3-2*u)
		atten *= cone
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
	// Zero defaults to core.CompositeLighter (additive). Set to
	// core.CompositeScreen for softer screen-blended lights.
	LightComposite core.CompositeOp
	// DisableShadows skips occlusion for every light, as though none had CastShadows, without changing the lights; a low quality preset sets it (quality.Preset.Shadows false).
	DisableShadows bool
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

	comp := s.LightComposite
	if comp == 0 {
		comp = core.CompositeLighter
	}

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
		grad := lightGradient(l)
		if grad == nil {
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

		d.SetFillGradient(grad)
		d.BeginPath()
		if l.isSpotlight() {
			half := l.ConeSpread * 0.5
			d.MoveTo(l.X, l.Y)
			d.Arc(l.X, l.Y, l.Radius, l.ConeAngle-half, l.ConeAngle+half, false)
			d.ClosePath()
		} else {
			d.Arc(l.X, l.Y, l.Radius, 0, twoPi, false)
			d.ClosePath()
		}
		d.Fill()
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

var gradientOffsets = [6]float32{0.0, 0.2, 0.4, 0.6, 0.8, 1.0}

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
			alpha = la * float64(l.Intensity) * atten
			if alpha > 1 {
				alpha = 1
			}
			if alpha < 0 {
				alpha = 0
			}
		}
		stops[i] = core.GradientStop{
			Offset: off,
			Color:  core.RGBA(rgb, float32(alpha)),
		}
	}
	return core.CanvasRadial(l.X, l.Y, 0, l.X, l.Y, l.Radius, stops...)
}
