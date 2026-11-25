package sceneformat

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// LightKind selects the kind of 3D light an object emits (LightDoc.Kind).
type LightKind string

const (
	// LightDirectional is an infinitely distant light whose parallel rays travel along the object's local -Z axis.
	LightDirectional LightKind = "directional"
	// LightPoint is an omnidirectional punctual light shining from the object's position.
	LightPoint LightKind = "point"
	// LightSpot is a cone-shaped punctual light at the object's position shining along its local -Z axis.
	LightSpot LightKind = "spot"
)

// LightDoc configures a 3D light attached to an object (ObjectDoc.Light). Its world position and forward direction (local -Z) come from the object's 3D transform. Version 4.
type LightDoc struct {
	// Kind selects LightDirectional ("directional"), LightPoint ("point") or LightSpot ("spot").
	Kind LightKind `yaml:"kind" schema:"required,enum=directional|point|spot"`
	// Color is an optional CSS color string parsed with core.ParseColor (sRGB, converted to linear light when shaded); empty defaults to white.
	Color string `yaml:"color,omitempty"`
	// Intensity scales the light's color; nil defaults to 1. Must be finite and non-negative.
	Intensity *float32 `yaml:"intensity,omitempty" schema:"minimum=0"`
	// Range is the smooth distance falloff cutoff in world units for point and spot lights; 0 gives unbounded inverse-square falloff. Must be finite and non-negative; refused on directional lights when non-zero.
	Range float32 `yaml:"range,omitempty" schema:"minimum=0"`
	// InnerCone is a spot light's inner cone half-angle in degrees, in [0, outerCone); defaults to 0. Refused on non-spot lights when non-zero.
	InnerCone float32 `yaml:"innerCone,omitempty" schema:"minimum=0,maximum=90"`
	// OuterCone is a spot light's outer cone half-angle in degrees, in (0, 90]; nil defaults to 45 for spot lights. Refused on non-spot lights.
	OuterCone *float32 `yaml:"outerCone,omitempty" schema:"exclusiveMinimum=0,maximum=90"`
	// Shadows enables shadow casting for this light.
	Shadows bool `yaml:"shadows,omitempty"`
}

// EffectiveIntensity returns l.Intensity, defaulting to 1 when nil.
func (l *LightDoc) EffectiveIntensity() float32 {
	if l == nil || l.Intensity == nil {
		return 1
	}
	return *l.Intensity
}

// EffectiveOuterCone returns a spot light's outer cone half-angle in degrees, defaulting to 45 when nil.
func (l *LightDoc) EffectiveOuterCone() float32 {
	if l == nil || l.OuterCone == nil {
		return 45
	}
	return *l.OuterCone
}

// EffectiveColor returns l.Color, defaulting to "#ffffff" when empty.
func (l *LightDoc) EffectiveColor() string {
	if l == nil || l.Color == "" {
		return "#ffffff"
	}
	return l.Color
}

// Clone returns a deep copy of l, or nil when l is nil.
func (l *LightDoc) Clone() *LightDoc {
	if l == nil {
		return nil
	}
	out := *l
	out.Intensity = clonePtr(l.Intensity)
	out.OuterCone = clonePtr(l.OuterCone)
	return &out
}

// ProjectionKind selects a 3D camera's projection (Camera3DDoc.Projection).
type ProjectionKind string

const (
	// ProjectionPerspective projects a perspective view frustum.
	ProjectionPerspective ProjectionKind = "perspective"
	// ProjectionOrthographic projects a parallel orthographic view volume.
	ProjectionOrthographic ProjectionKind = "orthographic"
)

// Camera3DDoc configures a 3D camera attached to an object (ObjectDoc.Camera). Its world position and orientation (looking down local -Z with +Y up) come from the object's 3D transform. Version 4.
type Camera3DDoc struct {
	// Projection selects ProjectionPerspective ("perspective") or ProjectionOrthographic ("orthographic"); empty defaults to perspective.
	Projection ProjectionKind `yaml:"projection,omitempty" schema:"enum=perspective|orthographic"`
	// FOV is a perspective camera's vertical field of view in degrees, in (0, 180); nil defaults to 60. Refused on orthographic cameras.
	FOV *float32 `yaml:"fov,omitempty" schema:"exclusiveMinimum=0,exclusiveMaximum=180"`
	// OrthoHeight is an orthographic camera's full vertical world height; nil defaults to 10. Must be finite and positive; refused on perspective cameras.
	OrthoHeight *float32 `yaml:"orthoHeight,omitempty" schema:"exclusiveMinimum=0"`
	// Near is the near clip plane distance; nil defaults to 0.1. Must be finite and positive for perspective cameras, and finite for orthographic cameras.
	Near *float32 `yaml:"near,omitempty"`
	// Far is the far clip plane distance; nil defaults to 1000. Must be finite and greater than near.
	Far *float32 `yaml:"far,omitempty"`
	// Current marks this camera as the scene's active 3D camera. At most one camera in a scene may be current; when none is marked, the first 3D camera in document order is used.
	Current bool `yaml:"current,omitempty"`
}

// EffectiveProjection returns c.Projection, defaulting to ProjectionPerspective when empty.
func (c *Camera3DDoc) EffectiveProjection() ProjectionKind {
	if c == nil || c.Projection == "" {
		return ProjectionPerspective
	}
	return c.Projection
}

// EffectiveFOV returns a perspective camera's vertical field of view in degrees, defaulting to 60 when nil.
func (c *Camera3DDoc) EffectiveFOV() float32 {
	if c == nil || c.FOV == nil {
		return 60
	}
	return *c.FOV
}

// EffectiveOrthoHeight returns an orthographic camera's full vertical world height, defaulting to 10 when nil.
func (c *Camera3DDoc) EffectiveOrthoHeight() float32 {
	if c == nil || c.OrthoHeight == nil {
		return 10
	}
	return *c.OrthoHeight
}

// EffectiveNear returns the near clip plane distance, defaulting to 0.1 when nil.
func (c *Camera3DDoc) EffectiveNear() float32 {
	if c == nil || c.Near == nil {
		return 0.1
	}
	return *c.Near
}

// EffectiveFar returns the far clip plane distance, defaulting to 1000 when nil.
func (c *Camera3DDoc) EffectiveFar() float32 {
	if c == nil || c.Far == nil {
		return 1000
	}
	return *c.Far
}

// Clone returns a deep copy of c, or nil when c is nil.
func (c *Camera3DDoc) Clone() *Camera3DDoc {
	if c == nil {
		return nil
	}
	out := *c
	out.FOV = clonePtr(c.FOV)
	out.OrthoHeight = clonePtr(c.OrthoHeight)
	out.Near = clonePtr(c.Near)
	out.Far = clonePtr(c.Far)
	return &out
}

// SkyKind selects how the 3D scene sky background and environment map are generated (SkyDoc.Kind).
type SkyKind string

const (
	// SkyColor fills the sky background with a single solid color (SkyDoc.Color).
	SkyColor SkyKind = "color"
	// SkyGradient draws a procedural three-band gradient sky from Zenith through Horizon to Ground.
	SkyGradient SkyKind = "gradient"
	// SkyHDR loads an equirectangular Radiance .hdr panorama from SkyDoc.HDR into an HDR cubemap for both skybox rendering and image-based lighting.
	SkyHDR SkyKind = "hdr"
)

// SkyDoc configures the 3D scene sky and IBL source (EnvironmentDoc.Sky). Version 4.
type SkyDoc struct {
	// Kind selects SkyColor ("color"), SkyGradient ("gradient") or SkyHDR ("hdr").
	Kind SkyKind `yaml:"kind" schema:"required,enum=color|gradient|hdr"`
	// Color is the CSS color of a SkyColor sky; empty defaults to "#141824". Refused on other sky kinds.
	Color string `yaml:"color,omitempty"`
	// Zenith is the upper-hemisphere CSS color of a SkyGradient sky; empty defaults to "#3d6194". Refused on other sky kinds.
	Zenith string `yaml:"zenith,omitempty"`
	// Horizon is the horizon-band CSS color of a SkyGradient sky; empty defaults to "#a8b8c7". Refused on other sky kinds.
	Horizon string `yaml:"horizon,omitempty"`
	// Ground is the lower-hemisphere CSS color of a SkyGradient sky; empty defaults to "#2b2b2e". Refused on other sky kinds.
	Ground string `yaml:"ground,omitempty"`
	// HDR is the slash-separated path from the project root to an equirectangular Radiance .hdr or .pic panorama for a SkyHDR sky, such as "env/studio.hdr". Required for SkyHDR and refused on other sky kinds.
	HDR string `yaml:"hdr,omitempty"`
}

// EffectiveColor returns s.Color, defaulting to "#141824" when empty.
func (s *SkyDoc) EffectiveColor() string {
	if s == nil || s.Color == "" {
		return "#141824"
	}
	return s.Color
}

// EffectiveZenith returns s.Zenith, defaulting to "#3d6194" when empty.
func (s *SkyDoc) EffectiveZenith() string {
	if s == nil || s.Zenith == "" {
		return "#3d6194"
	}
	return s.Zenith
}

// EffectiveHorizon returns s.Horizon, defaulting to "#a8b8c7" when empty.
func (s *SkyDoc) EffectiveHorizon() string {
	if s == nil || s.Horizon == "" {
		return "#a8b8c7"
	}
	return s.Horizon
}

// EffectiveGround returns s.Ground, defaulting to "#2b2b2e" when empty.
func (s *SkyDoc) EffectiveGround() string {
	if s == nil || s.Ground == "" {
		return "#2b2b2e"
	}
	return s.Ground
}

// Clone returns a deep copy of s, or nil when s is nil.
func (s *SkyDoc) Clone() *SkyDoc {
	if s == nil {
		return nil
	}
	out := *s
	return &out
}

// hdrExtensions are the file extensions SkyDoc.HDR accepts, compared without case.
var hdrExtensions = []string{".hdr", ".pic"}

// IsHDRPath reports whether p names a Radiance HDR image by its extension, .hdr or .pic in any case, as SkyDoc.HDR requires.
func IsHDRPath(p string) bool {
	return slices.Contains(hdrExtensions, strings.ToLower(path.Ext(p)))
}

// FogMode selects how 3D distance fog attenuates visibility (FogDoc.Mode).
type FogMode string

const (
	// FogOff disables distance fog (analytical height fog may still apply when HeightEnabled is true).
	FogOff FogMode = "off"
	// FogLinear ramps distance fog linearly from Start to End.
	FogLinear FogMode = "linear"
	// FogExp applies exponential distance fog using Density.
	FogExp FogMode = "exp"
	// FogExp2 applies exponential-squared distance fog using Density.
	FogExp2 FogMode = "exp2"
)

// FogDoc configures 3D distance and analytical exponential height fog (EnvironmentDoc.Fog). Version 4.
type FogDoc struct {
	// Mode selects FogOff ("off"), FogLinear ("linear"), FogExp ("exp") or FogExp2 ("exp2"); empty defaults to off.
	Mode FogMode `yaml:"mode,omitempty" schema:"enum=off|linear|exp|exp2"`
	// Color is the CSS color of the fog (sRGB, converted to linear light when shaded); empty defaults to "#8090a8".
	Color string `yaml:"color,omitempty"`
	// Start is the world distance where FogLinear fog begins; must be finite and non-negative.
	Start float32 `yaml:"start,omitempty" schema:"minimum=0"`
	// End is the world distance where FogLinear fog reaches full density; must be finite and greater than Start when Mode is FogLinear.
	End float32 `yaml:"end,omitempty" schema:"minimum=0"`
	// Density is the extinction density for FogExp and FogExp2; must be finite and non-negative.
	Density float32 `yaml:"density,omitempty" schema:"minimum=0"`
	// MaxOpacity caps the combined fog factor in (0, 1]; nil defaults to 1.
	MaxOpacity *float32 `yaml:"maxOpacity,omitempty" schema:"exclusiveMinimum=0,maximum=1"`
	// HeightEnabled enables analytical exponential height fog along the view ray.
	HeightEnabled bool `yaml:"heightEnabled,omitempty"`
	// HeightBase is the world Y reference height where height fog has full HeightDensity.
	HeightBase float32 `yaml:"heightBase,omitempty"`
	// HeightFalloff is the vertical exponential decay rate above HeightBase; must be finite and non-negative.
	HeightFalloff float32 `yaml:"heightFalloff,omitempty" schema:"minimum=0"`
	// HeightDensity is the base extinction density of height fog at HeightBase; must be finite and non-negative.
	HeightDensity float32 `yaml:"heightDensity,omitempty" schema:"minimum=0"`
}

// EffectiveMode returns f.Mode, defaulting to FogOff when empty.
func (f *FogDoc) EffectiveMode() FogMode {
	if f == nil || f.Mode == "" {
		return FogOff
	}
	return f.Mode
}

// EffectiveColor returns f.Color, defaulting to "#8090a8" when empty.
func (f *FogDoc) EffectiveColor() string {
	if f == nil || f.Color == "" {
		return "#8090a8"
	}
	return f.Color
}

// EffectiveMaxOpacity returns f.MaxOpacity, defaulting to 1 when nil.
func (f *FogDoc) EffectiveMaxOpacity() float32 {
	if f == nil || f.MaxOpacity == nil {
		return 1
	}
	return *f.MaxOpacity
}

// Clone returns a deep copy of f, or nil when f is nil.
func (f *FogDoc) Clone() *FogDoc {
	if f == nil {
		return nil
	}
	out := *f
	out.MaxOpacity = clonePtr(f.MaxOpacity)
	return &out
}

// TonemapMode selects the HDR-to-LDR tone mapping operator (EnvironmentDoc.Tonemapping).
type TonemapMode string

const (
	// TonemapNone clamps exposed linear radiance to [0, 1] without highlight roll-off.
	TonemapNone TonemapMode = "none"
	// TonemapReinhard applies per-channel Reinhard compression.
	TonemapReinhard TonemapMode = "reinhard"
	// TonemapACES applies the fitted ACES filmic curve.
	TonemapACES TonemapMode = "aces"
	// TonemapNeutral applies the Khronos PBR Neutral tone mapper.
	TonemapNeutral TonemapMode = "neutral"
	// TonemapAgX applies the AgX filmic tone mapper.
	TonemapAgX TonemapMode = "agx"
)

// EnvironmentDoc configures a 3D scene's sky, ambient fill, image-based lighting, fog, tone mapping and exposure (SceneDoc.Environment). Version 4.
type EnvironmentDoc struct {
	// Sky configures the background sky and IBL environment map; nil leaves the scene without a skybox.
	Sky *SkyDoc `yaml:"sky,omitempty"`
	// AmbientColor is the CSS color of the uniform ambient fill light (sRGB, converted to linear light); empty defaults to "#ffffff".
	AmbientColor string `yaml:"ambientColor,omitempty"`
	// AmbientIntensity scales the uniform ambient fill light; nil defaults to 0.25. Must be finite and non-negative.
	AmbientIntensity *float32 `yaml:"ambientIntensity,omitempty" schema:"minimum=0"`
	// IBLIntensity scales diffuse spherical-harmonic and specular prefiltered image-based lighting derived from Sky; nil defaults to 1 for SkyHDR and 0 otherwise. Must be finite and non-negative.
	IBLIntensity *float32 `yaml:"iblIntensity,omitempty" schema:"minimum=0"`
	// Fog configures 3D distance and analytical height fog; nil leaves fog disabled.
	Fog *FogDoc `yaml:"fog,omitempty"`
	// Tonemapping selects TonemapNone ("none"), TonemapReinhard ("reinhard"), TonemapACES ("aces"), TonemapNeutral ("neutral") or TonemapAgX ("agx"); empty defaults to none.
	Tonemapping TonemapMode `yaml:"tonemapping,omitempty" schema:"enum=none|reinhard|aces|neutral|agx"`
	// Exposure is the linear exposure multiplier applied before tone mapping; nil defaults to 1. Must be finite and non-negative.
	Exposure *float32 `yaml:"exposure,omitempty" schema:"minimum=0"`
}

// EffectiveAmbientColor returns e.AmbientColor, defaulting to "#ffffff" when empty.
func (e *EnvironmentDoc) EffectiveAmbientColor() string {
	if e == nil || e.AmbientColor == "" {
		return "#ffffff"
	}
	return e.AmbientColor
}

// EffectiveAmbientIntensity returns e.AmbientIntensity, defaulting to 0.25 when nil.
func (e *EnvironmentDoc) EffectiveAmbientIntensity() float32 {
	if e == nil || e.AmbientIntensity == nil {
		return 0.25
	}
	return *e.AmbientIntensity
}

// EffectiveIBLIntensity returns e.IBLIntensity when set, 1 when Sky is SkyHDR, and 0 otherwise.
func (e *EnvironmentDoc) EffectiveIBLIntensity() float32 {
	if e == nil {
		return 0
	}
	if e.IBLIntensity != nil {
		return *e.IBLIntensity
	}
	if e.Sky != nil && e.Sky.Kind == SkyHDR {
		return 1
	}
	return 0
}

// EffectiveTonemapping returns e.Tonemapping, defaulting to TonemapNone when empty.
func (e *EnvironmentDoc) EffectiveTonemapping() TonemapMode {
	if e == nil || e.Tonemapping == "" {
		return TonemapNone
	}
	return e.Tonemapping
}

// EffectiveExposure returns e.Exposure, defaulting to 1 when nil.
func (e *EnvironmentDoc) EffectiveExposure() float32 {
	if e == nil || e.Exposure == nil {
		return 1
	}
	return *e.Exposure
}

// Clone returns a deep copy of e, or nil when e is nil.
func (e *EnvironmentDoc) Clone() *EnvironmentDoc {
	if e == nil {
		return nil
	}
	out := *e
	out.Sky = e.Sky.Clone()
	out.AmbientIntensity = clonePtr(e.AmbientIntensity)
	out.IBLIntensity = clonePtr(e.IBLIntensity)
	out.Fog = e.Fog.Clone()
	out.Exposure = clonePtr(e.Exposure)
	return &out
}

// validateEnvironment checks doc.Environment at path ["environment"].
func validateEnvironment(version int, env *EnvironmentDoc) error {
	if env == nil {
		return nil
	}
	ep := []string{"environment"}
	if version < 4 {
		return fieldErr(ep, "sceneformat: environment needs version 4, not %d", version)
	}
	if env.Sky != nil {
		if err := validateSky(at(ep, "sky"), env.Sky); err != nil {
			return err
		}
	}
	if env.AmbientColor != "" {
		if _, err := core.ParseColor(env.AmbientColor); err != nil {
			return fieldErr(at(ep, "ambientColor"), "sceneformat: environment ambientColor %q: %w", env.AmbientColor, err)
		}
	}
	if env.AmbientIntensity != nil && (!isFinite(*env.AmbientIntensity) || *env.AmbientIntensity < 0) {
		return fieldErr(at(ep, "ambientIntensity"), "sceneformat: environment ambientIntensity %v must be finite and non-negative", *env.AmbientIntensity)
	}
	if env.IBLIntensity != nil && (!isFinite(*env.IBLIntensity) || *env.IBLIntensity < 0) {
		return fieldErr(at(ep, "iblIntensity"), "sceneformat: environment iblIntensity %v must be finite and non-negative", *env.IBLIntensity)
	}
	if env.Fog != nil {
		if err := validateFog(at(ep, "fog"), env.Fog); err != nil {
			return err
		}
	}
	switch env.Tonemapping {
	case "", TonemapNone, TonemapReinhard, TonemapACES, TonemapNeutral, TonemapAgX:
	default:
		return fieldErr(at(ep, "tonemapping"), "sceneformat: environment tonemapping %q is invalid (want none, reinhard, aces, neutral or agx)", env.Tonemapping)
	}
	if env.Exposure != nil && (!isFinite(*env.Exposure) || *env.Exposure < 0) {
		return fieldErr(at(ep, "exposure"), "sceneformat: environment exposure %v must be finite and non-negative", *env.Exposure)
	}
	return nil
}

func validateSky(p []string, s *SkyDoc) error {
	switch s.Kind {
	case SkyColor, SkyGradient, SkyHDR:
	default:
		return fieldErr(at(p, "kind"), "sceneformat: environment sky kind %q is invalid (want color, gradient or hdr)", s.Kind)
	}
	if s.Color != "" {
		if s.Kind != SkyColor {
			return fieldErr(at(p, "color"), "sceneformat: environment sky color is only valid for kind %q, not %q", SkyColor, s.Kind)
		}
		if _, err := core.ParseColor(s.Color); err != nil {
			return fieldErr(at(p, "color"), "sceneformat: environment sky color %q: %w", s.Color, err)
		}
	}
	for _, f := range []struct {
		name string
		val  string
	}{
		{"zenith", s.Zenith},
		{"horizon", s.Horizon},
		{"ground", s.Ground},
	} {
		if f.val == "" {
			continue
		}
		if s.Kind != SkyGradient {
			return fieldErr(at(p, f.name), "sceneformat: environment sky %s is only valid for kind %q, not %q", f.name, SkyGradient, s.Kind)
		}
		if _, err := core.ParseColor(f.val); err != nil {
			return fieldErr(at(p, f.name), "sceneformat: environment sky %s %q: %w", f.name, f.val, err)
		}
	}
	if s.Kind == SkyHDR {
		if s.HDR == "" {
			return fieldErr(at(p, "hdr"), "sceneformat: environment sky kind %q requires hdr path", SkyHDR)
		}
		if !validFilePath(s.HDR) {
			return fieldErr(at(p, "hdr"), "sceneformat: environment sky hdr %q must be a slash-separated path relative to the project root", s.HDR)
		}
		if !IsHDRPath(s.HDR) {
			return fieldErr(at(p, "hdr"), "sceneformat: environment sky hdr %q must be a Radiance HDR image ending in .hdr or .pic", s.HDR)
		}
	} else if s.HDR != "" {
		return fieldErr(at(p, "hdr"), "sceneformat: environment sky hdr is only valid for kind %q, not %q", SkyHDR, s.Kind)
	}
	return nil
}

func validateFog(p []string, f *FogDoc) error {
	switch f.Mode {
	case "", FogOff, FogLinear, FogExp, FogExp2:
	default:
		return fieldErr(at(p, "mode"), "sceneformat: environment fog mode %q is invalid (want off, linear, exp or exp2)", f.Mode)
	}
	if f.Color != "" {
		if _, err := core.ParseColor(f.Color); err != nil {
			return fieldErr(at(p, "color"), "sceneformat: environment fog color %q: %w", f.Color, err)
		}
	}
	if !isFinite(f.Start) || f.Start < 0 {
		return fieldErr(at(p, "start"), "sceneformat: environment fog start %v must be finite and non-negative", f.Start)
	}
	if !isFinite(f.End) || f.End < 0 {
		return fieldErr(at(p, "end"), "sceneformat: environment fog end %v must be finite and non-negative", f.End)
	}
	if f.Mode == FogLinear && f.End <= f.Start {
		return fieldErr(at(p, "end"), "sceneformat: environment linear fog end %v must be greater than start %v", f.End, f.Start)
	}
	if !isFinite(f.Density) || f.Density < 0 {
		return fieldErr(at(p, "density"), "sceneformat: environment fog density %v must be finite and non-negative", f.Density)
	}
	if f.MaxOpacity != nil && (!isFinite(*f.MaxOpacity) || *f.MaxOpacity <= 0 || *f.MaxOpacity > 1) {
		return fieldErr(at(p, "maxOpacity"), "sceneformat: environment fog maxOpacity %v must be finite and in (0, 1]", *f.MaxOpacity)
	}
	if !isFinite(f.HeightBase) {
		return fieldErr(at(p, "heightBase"), "sceneformat: environment fog heightBase must be finite")
	}
	if !isFinite(f.HeightFalloff) || f.HeightFalloff < 0 {
		return fieldErr(at(p, "heightFalloff"), "sceneformat: environment fog heightFalloff %v must be finite and non-negative", f.HeightFalloff)
	}
	if !isFinite(f.HeightDensity) || f.HeightDensity < 0 {
		return fieldErr(at(p, "heightDensity"), "sceneformat: environment fog heightDensity %v must be finite and non-negative", f.HeightDensity)
	}
	return nil
}

// validateLight checks an object's Light field at path p (pointing to the object or override).
func validateLight(version int, p []string, where string, l *LightDoc) error {
	if l == nil {
		return nil
	}
	lp := at(p, "light")
	if version < 4 {
		return fieldErr(lp, "sceneformat: %s: light needs version 4, not %d", where, version)
	}
	switch l.Kind {
	case LightDirectional, LightPoint, LightSpot:
	default:
		return fieldErr(at(lp, "kind"), "sceneformat: %s light kind %q is invalid (want directional, point or spot)", where, l.Kind)
	}
	if l.Color != "" {
		if _, err := core.ParseColor(l.Color); err != nil {
			return fieldErr(at(lp, "color"), "sceneformat: %s light color %q: %w", where, l.Color, err)
		}
	}
	if l.Intensity != nil && (!isFinite(*l.Intensity) || *l.Intensity < 0) {
		return fieldErr(at(lp, "intensity"), "sceneformat: %s light intensity %v must be finite and non-negative", where, *l.Intensity)
	}
	if !isFinite(l.Range) || l.Range < 0 {
		return fieldErr(at(lp, "range"), "sceneformat: %s light range %v must be finite and non-negative", where, l.Range)
	}
	if l.Kind == LightDirectional && l.Range != 0 {
		return fieldErr(at(lp, "range"), "sceneformat: %s directional light cannot set range", where)
	}
	if !isFinite(l.InnerCone) || l.InnerCone < 0 || l.InnerCone > 90 {
		return fieldErr(at(lp, "innerCone"), "sceneformat: %s light innerCone %v must be finite and in [0, 90]", where, l.InnerCone)
	}
	if l.OuterCone != nil && (!isFinite(*l.OuterCone) || *l.OuterCone <= 0 || *l.OuterCone > 90) {
		return fieldErr(at(lp, "outerCone"), "sceneformat: %s light outerCone %v must be finite and in (0, 90]", where, *l.OuterCone)
	}
	if l.Kind != LightSpot {
		if l.InnerCone != 0 {
			return fieldErr(at(lp, "innerCone"), "sceneformat: %s %s light cannot set innerCone", where, l.Kind)
		}
		if l.OuterCone != nil {
			return fieldErr(at(lp, "outerCone"), "sceneformat: %s %s light cannot set outerCone", where, l.Kind)
		}
	} else if l.InnerCone >= l.EffectiveOuterCone() {
		return fieldErr(at(lp, "innerCone"), "sceneformat: %s spot light innerCone %v must be less than outerCone %v", where, l.InnerCone, l.EffectiveOuterCone())
	}
	return nil
}

// validateCamera3D checks an object's Camera field at path p (pointing to the object or override).
func validateCamera3D(version int, p []string, where string, c *Camera3DDoc) error {
	if c == nil {
		return nil
	}
	cp := at(p, "camera")
	if version < 4 {
		return fieldErr(cp, "sceneformat: %s: camera needs version 4, not %d", where, version)
	}
	switch c.Projection {
	case "", ProjectionPerspective, ProjectionOrthographic:
	default:
		return fieldErr(at(cp, "projection"), "sceneformat: %s camera projection %q is invalid (want perspective or orthographic)", where, c.Projection)
	}
	proj := c.EffectiveProjection()
	if proj == ProjectionPerspective {
		if c.FOV != nil && (!isFinite(*c.FOV) || *c.FOV <= 0 || *c.FOV >= 180) {
			return fieldErr(at(cp, "fov"), "sceneformat: %s camera fov %v must be finite and in (0, 180)", where, *c.FOV)
		}
		if c.OrthoHeight != nil {
			return fieldErr(at(cp, "orthoHeight"), "sceneformat: %s perspective camera cannot set orthoHeight", where)
		}
		if c.Near != nil && (!isFinite(*c.Near) || *c.Near <= 0) {
			return fieldErr(at(cp, "near"), "sceneformat: %s perspective camera near %v must be finite and positive", where, *c.Near)
		}
	} else {
		if c.OrthoHeight != nil && (!isFinite(*c.OrthoHeight) || *c.OrthoHeight <= 0) {
			return fieldErr(at(cp, "orthoHeight"), "sceneformat: %s camera orthoHeight %v must be finite and positive", where, *c.OrthoHeight)
		}
		if c.FOV != nil {
			return fieldErr(at(cp, "fov"), "sceneformat: %s orthographic camera cannot set fov", where)
		}
		if c.Near != nil && !isFinite(*c.Near) {
			return fieldErr(at(cp, "near"), "sceneformat: %s camera near must be finite", where)
		}
	}
	if c.Far != nil && !isFinite(*c.Far) {
		return fieldErr(at(cp, "far"), "sceneformat: %s camera far must be finite", where)
	}
	if c.EffectiveFar() <= c.EffectiveNear() {
		return fieldErr(at(cp, "far"), "sceneformat: %s camera far %v must be greater than near %v", where, c.EffectiveFar(), c.EffectiveNear())
	}
	return nil
}

// CheckObjectLight checks o.Light as Validate checks it in a version 4 scene.
func CheckObjectLight(o *ObjectDoc) error {
	if o == nil {
		return nil
	}
	return validateLight(CurrentVersion, nil, fmt.Sprintf("object %q", o.ID), o.Light)
}

// CheckObjectCamera3D checks o.Camera as Validate checks it in a version 4 scene.
func CheckObjectCamera3D(o *ObjectDoc) error {
	if o == nil {
		return nil
	}
	return validateCamera3D(CurrentVersion, nil, fmt.Sprintf("object %q", o.ID), o.Camera)
}
