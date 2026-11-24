package render3d

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

const (
	// MaxSpotLights is the maximum number of spot lights a [LightRig] encodes into the GPU uniform block.
	MaxSpotLights = 8

	// maxSpotConeAngle is the largest outer cone half-angle glTF KHR_lights_punctual allows (pi/2).
	maxSpotConeAngle = math.Pi / 2

	// minSpotConeCosDelta bounds cos(inner) - cos(outer) from below before it is inverted into the angular scale, as the glTF KHR_lights_punctual reference implementation does, so a very thin falloff band stays finite.
	minSpotConeCosDelta = 1e-3
)

// SpotLight is a punctual cone-shaped emitter at Position shining along Direction, following glTF KHR_lights_punctual.
//
// Direction is the world-space direction the cone points in (the light travels along it); it does not need to be unit length and is normalized when shaded or packed for the GPU. Color is linear RGB tint (non-negative finite channels) and Intensity scales Color to produce linear radiant intensity. Range windows the distance falloff exactly as it does for a [PointLight] (0 for pure inverse-square falloff). InnerConeAngle and OuterConeAngle are half-angles in radians from Direction with 0 <= InnerConeAngle < OuterConeAngle <= pi/2: surfaces inside the inner cone receive the full intensity, which then falls off smoothly to zero at the outer cone (see [SpotConeAttenuation]).
type SpotLight struct {
	Position       math3d.Vec3
	Direction      math3d.Vec3
	Color          math3d.Vec3
	Intensity      float32
	Range          float32
	InnerConeAngle float32
	OuterConeAngle float32
}

// Validate reports whether l has a finite position, a finite non-zero direction, finite non-negative color channels, intensity and range, and cone half-angles with 0 <= InnerConeAngle < OuterConeAngle <= pi/2, the bounds glTF KHR_lights_punctual sets. Every error wraps [ErrInvalidLight].
func (l SpotLight) Validate() error {
	if !finite3(l.Position) {
		return fmt.Errorf("%w: spot light position must be finite", ErrInvalidLight)
	}
	if !finite3(l.Direction) || l.Direction.LenSq() <= minLightDirLenSq {
		return fmt.Errorf("%w: spot light direction must be finite and non-zero", ErrInvalidLight)
	}
	if !validLinearColor(l.Color) {
		return fmt.Errorf("%w: spot light color must be finite and non-negative", ErrInvalidLight)
	}
	if !finite(l.Intensity) || l.Intensity < 0 {
		return fmt.Errorf("%w: spot light intensity must be finite and non-negative", ErrInvalidLight)
	}
	if !finite(l.Range) || l.Range < 0 {
		return fmt.Errorf("%w: spot light range must be finite and non-negative", ErrInvalidLight)
	}
	if !finite(l.OuterConeAngle) || l.OuterConeAngle <= 0 || l.OuterConeAngle > maxSpotConeAngle {
		return fmt.Errorf("%w: spot light outer cone angle %v must be in (0, pi/2]", ErrInvalidLight, l.OuterConeAngle)
	}
	if !finite(l.InnerConeAngle) || l.InnerConeAngle < 0 || l.InnerConeAngle >= l.OuterConeAngle {
		return fmt.Errorf("%w: spot light inner cone angle %v must be in [0, outer cone angle %v)", ErrInvalidLight, l.InnerConeAngle, l.OuterConeAngle)
	}
	return nil
}

// ConeScaleOffset returns the angular attenuation terms glTF KHR_lights_punctual precomputes per light: angleScale = 1 / max(cos(InnerConeAngle) - cos(OuterConeAngle), 0.001) and angleOffset = -cos(OuterConeAngle) * angleScale. [SpotConeAttenuation] takes them, and the GPU light block stores them next to each spot light.
func (l SpotLight) ConeScaleOffset() (angleScale, angleOffset float32) {
	cosInner := math.Cos(float64(l.InnerConeAngle))
	cosOuter := math.Cos(float64(l.OuterConeAngle))
	scale := 1 / max(cosInner-cosOuter, minSpotConeCosDelta)
	return float32(scale), float32(-cosOuter * scale)
}

// SpotConeAttenuation returns the glTF KHR_lights_punctual angular falloff of a spot light in [0, 1]: t = clamp(dot(spotDir, -toLight) * angleScale + angleOffset, 0, 1), squared. spotDir is the light's unit cone direction, toLight is the unit vector from the surface toward the light, and angleScale and angleOffset come from [SpotLight.ConeScaleOffset]. It is 1 inside the inner cone and 0 outside the outer cone or behind the light, and it matches spot_cone_attenuation in [LightWGSL].
func SpotConeAttenuation(spotDir, toLight math3d.Vec3, angleScale, angleOffset float32) float32 {
	t := clamp01(-spotDir.Dot(toLight)*angleScale + angleOffset)
	return t * t
}

// Attenuation returns the combined distance and cone attenuation l applies at worldPos: [PointLightAttenuation] with l.Range times [SpotConeAttenuation]. It is 0 for a degenerate direction or when worldPos coincides with the light, and multiplying it by Color * Intensity gives the incident radiance [ShadePoint] uses for l.
func (l SpotLight) Attenuation(worldPos math3d.Vec3) float32 {
	dir, ok := spotUnitDirection(l)
	toLight := l.Position.Sub(worldPos)
	distSq := toLight.LenSq()
	if !ok || distSq <= minLightDirLenSq {
		return 0
	}
	lv := toLight.Scale(float32(1.0 / math.Sqrt(float64(distSq))))
	scale, offset := l.ConeScaleOffset()
	return PointLightAttenuation(distSq, l.Range) * SpotConeAttenuation(dir, lv, scale, offset)
}
