package sceneformat

import (
	"fmt"
	"strings"
)

const (
	// DefaultReflectionProbeResolution is the cubemap face resolution used when ReflectionProbeDoc.Resolution is 0.
	DefaultReflectionProbeResolution = 64

	// MaxReflectionProbeResolution is the largest cubemap face resolution accepted in a scene file.
	MaxReflectionProbeResolution = 1024

	// MaxLightProbesPerGrid caps the total number of probes (nx * ny * nz) in a LightProbeGridDoc.
	MaxLightProbesPerGrid = 4096
)

// ReflectionProbeDoc configures a local 3D reflection probe attached to an object (ObjectDoc.ReflectionProbe).
// Its cubemap is captured at the object's 3D world position and blended across the axis-aligned influence box
// defined by Center and Size in local space. Version 4.
type ReflectionProbeDoc struct {
	// Center is the optional local-space center [x, y, z] of the influence box relative to the object's origin; nil centers it at [0, 0, 0].
	Center *[3]float32 `yaml:"center,omitempty,flow"`
	// Size is the local-space [width, height, depth] extents of the influence box; nil defaults to [10, 10, 10]. When set, every axis must be finite and positive.
	Size *[3]float32 `yaml:"size,omitempty,flow"`
	// BlendDistance is the inward fade width in world units from the influence box faces; 0 gives a sharp boundary. Must be finite and non-negative.
	BlendDistance float32 `yaml:"blendDistance,omitempty" schema:"minimum=0"`
	// Intensity scales the probe's reflected radiance; nil defaults to 1. Must be finite and non-negative.
	Intensity *float32 `yaml:"intensity,omitempty" schema:"minimum=0"`
	// Priority breaks ties when reflection probes overlap; higher values take precedence.
	Priority int `yaml:"priority,omitempty"`
	// BoxProjection enables parallax-corrected ray-AABB reflection lookup when true; nil defaults to true.
	BoxProjection *bool `yaml:"boxProjection,omitempty"`
	// Resolution is the cubemap face edge length in texels used when baking; 0 defaults to 64. Must be in [0, 1024].
	Resolution int `yaml:"resolution,omitempty" schema:"minimum=0,maximum=1024"`
	// Cubemap optionally names the baked cubemap image file by its slash-separated path from the project root, such as "probes/room_refl.hdr" (Radiance .hdr or .pic).
	Cubemap string `yaml:"cubemap,omitempty"`
}

// EffectiveCenter returns p.Center, defaulting to [0, 0, 0] when nil.
func (p *ReflectionProbeDoc) EffectiveCenter() [3]float32 {
	if p == nil || p.Center == nil {
		return [3]float32{}
	}
	return *p.Center
}

// EffectiveSize returns p.Size, defaulting to [10, 10, 10] when nil or all zero.
func (p *ReflectionProbeDoc) EffectiveSize() [3]float32 {
	if p == nil || p.Size == nil || *p.Size == ([3]float32{}) {
		return [3]float32{10, 10, 10}
	}
	return *p.Size
}

// LocalBounds returns the local-space min and max corners of the probe's influence box.
func (p *ReflectionProbeDoc) LocalBounds() (lo, hi [3]float32) {
	if p == nil {
		return lo, hi
	}
	sz := p.EffectiveSize()
	c := p.EffectiveCenter()
	hx, hy, hz := sz[0]*0.5, sz[1]*0.5, sz[2]*0.5
	return [3]float32{c[0] - hx, c[1] - hy, c[2] - hz}, [3]float32{c[0] + hx, c[1] + hy, c[2] + hz}
}

// EffectiveIntensity returns p.Intensity, defaulting to 1 when nil.
func (p *ReflectionProbeDoc) EffectiveIntensity() float32 {
	if p == nil || p.Intensity == nil {
		return 1
	}
	return *p.Intensity
}

// EffectiveBoxProjection reports whether parallax-corrected box projection is enabled, defaulting to true when nil.
func (p *ReflectionProbeDoc) EffectiveBoxProjection() bool {
	if p == nil || p.BoxProjection == nil {
		return true
	}
	return *p.BoxProjection
}

// EffectiveResolution returns p.Resolution, defaulting to DefaultReflectionProbeResolution (64) when 0.
func (p *ReflectionProbeDoc) EffectiveResolution() int {
	if p == nil || p.Resolution <= 0 {
		return DefaultReflectionProbeResolution
	}
	return p.Resolution
}

// Clone returns a deep copy of p, or nil when p is nil.
func (p *ReflectionProbeDoc) Clone() *ReflectionProbeDoc {
	if p == nil {
		return nil
	}
	out := *p
	out.Center = clonePtr(p.Center)
	out.Size = clonePtr(p.Size)
	out.Intensity = clonePtr(p.Intensity)
	out.BoxProjection = clonePtr(p.BoxProjection)
	return &out
}

// LightProbeGridDoc configures a 3D light probe grid attached to an object (ObjectDoc.LightProbeGrid).
// The grid places Counts[0] x Counts[1] x Counts[2] diffuse L2 spherical harmonic irradiance probes
// uniformly across the axis-aligned volume defined by Center and Size in local space. Version 4.
type LightProbeGridDoc struct {
	// Center is the optional local-space center [x, y, z] of the grid volume relative to the object's origin; nil centers it at [0, 0, 0].
	Center *[3]float32 `yaml:"center,omitempty,flow"`
	// Size is the local-space [width, height, depth] extents of the grid volume; nil defaults to [10, 6, 10]. When set, every axis must be finite and positive.
	Size *[3]float32 `yaml:"size,omitempty,flow"`
	// Counts is the number of probes [nx, ny, nz] along X, Y and Z; nil defaults to [3, 2, 3]. Each axis must be positive and nx*ny*nz at most MaxLightProbesPerGrid (4096).
	Counts *[3]int `yaml:"counts,omitempty,flow"`
	// Intensity scales the grid's diffuse irradiance contribution; nil defaults to 1. Must be finite and non-negative.
	Intensity *float32 `yaml:"intensity,omitempty" schema:"minimum=0"`
	// Data optionally names the baked light probe YAML file by its slash-separated path from the project root, such as "probes/room.probes.yaml".
	Data string `yaml:"data,omitempty"`
}

// EffectiveCenter returns g.Center, defaulting to [0, 0, 0] when nil.
func (g *LightProbeGridDoc) EffectiveCenter() [3]float32 {
	if g == nil || g.Center == nil {
		return [3]float32{}
	}
	return *g.Center
}

// EffectiveSize returns g.Size, defaulting to [10, 6, 10] when nil or all zero.
func (g *LightProbeGridDoc) EffectiveSize() [3]float32 {
	if g == nil || g.Size == nil || *g.Size == ([3]float32{}) {
		return [3]float32{10, 6, 10}
	}
	return *g.Size
}

// LocalBounds returns the local-space min and max corners of the grid's volume.
func (g *LightProbeGridDoc) LocalBounds() (lo, hi [3]float32) {
	if g == nil {
		return lo, hi
	}
	sz := g.EffectiveSize()
	c := g.EffectiveCenter()
	hx, hy, hz := sz[0]*0.5, sz[1]*0.5, sz[2]*0.5
	return [3]float32{c[0] - hx, c[1] - hy, c[2] - hz}, [3]float32{c[0] + hx, c[1] + hy, c[2] + hz}
}

// EffectiveCounts returns g.Counts, defaulting to [3, 2, 3] when nil or all zero.
func (g *LightProbeGridDoc) EffectiveCounts() [3]int {
	if g == nil || g.Counts == nil || *g.Counts == ([3]int{}) {
		return [3]int{3, 2, 3}
	}
	return *g.Counts
}

// EffectiveIntensity returns g.Intensity, defaulting to 1 when nil.
func (g *LightProbeGridDoc) EffectiveIntensity() float32 {
	if g == nil || g.Intensity == nil {
		return 1
	}
	return *g.Intensity
}

// Clone returns a deep copy of g, or nil when g is nil.
func (g *LightProbeGridDoc) Clone() *LightProbeGridDoc {
	if g == nil {
		return nil
	}
	out := *g
	out.Center = clonePtr(g.Center)
	out.Size = clonePtr(g.Size)
	out.Counts = clonePtr(g.Counts)
	out.Intensity = clonePtr(g.Intensity)
	return &out
}

// IsLightProbeGridPath reports whether p names a YAML light probe grid file by its extension (.yaml or .yml in any case).
func IsLightProbeGridPath(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

// validateReflectionProbe checks an object's ReflectionProbe field at path p.
func validateReflectionProbe(version int, p []string, where string, rp *ReflectionProbeDoc) error {
	if rp == nil {
		return nil
	}
	pp := at(p, "reflectionProbe")
	if version < 4 {
		return fieldErr(pp, "sceneformat: %s: reflectionProbe needs version 4, not %d", where, version)
	}
	if rp.Center != nil {
		for _, v := range *rp.Center {
			if !isFinite(v) {
				return fieldErr(at(pp, "center"), "sceneformat: %s reflectionProbe center must be finite", where)
			}
		}
	}
	if rp.Size != nil {
		for _, v := range *rp.Size {
			if !isFinite(v) {
				return fieldErr(at(pp, "size"), "sceneformat: %s reflectionProbe size must be finite", where)
			}
		}
		if *rp.Size != ([3]float32{}) && (rp.Size[0] <= 0 || rp.Size[1] <= 0 || rp.Size[2] <= 0) {
			return fieldErr(at(pp, "size"), "sceneformat: %s reflectionProbe size %v must have all positive axes or all zero", where, *rp.Size)
		}
	}
	if !isFinite(rp.BlendDistance) || rp.BlendDistance < 0 {
		return fieldErr(at(pp, "blendDistance"), "sceneformat: %s reflectionProbe blendDistance %v must be finite and non-negative", where, rp.BlendDistance)
	}
	if rp.Intensity != nil && (!isFinite(*rp.Intensity) || *rp.Intensity < 0) {
		return fieldErr(at(pp, "intensity"), "sceneformat: %s reflectionProbe intensity %v must be finite and non-negative", where, *rp.Intensity)
	}
	if rp.Resolution < 0 || rp.Resolution > MaxReflectionProbeResolution {
		return fieldErr(at(pp, "resolution"), "sceneformat: %s reflectionProbe resolution %d must be in [0, %d]", where, rp.Resolution, MaxReflectionProbeResolution)
	}
	if rp.Cubemap != "" {
		if !validFilePath(rp.Cubemap) {
			return fieldErr(at(pp, "cubemap"), "sceneformat: %s reflectionProbe cubemap %q must be a slash-separated path relative to the project root", where, rp.Cubemap)
		}
		if !IsHDRPath(rp.Cubemap) {
			return fieldErr(at(pp, "cubemap"), "sceneformat: %s reflectionProbe cubemap %q must be a Radiance HDR image ending in .hdr or .pic", where, rp.Cubemap)
		}
	}
	return nil
}

// validateLightProbeGrid checks an object's LightProbeGrid field at path p.
func validateLightProbeGrid(version int, p []string, where string, g *LightProbeGridDoc) error {
	if g == nil {
		return nil
	}
	gp := at(p, "lightProbeGrid")
	if version < 4 {
		return fieldErr(gp, "sceneformat: %s: lightProbeGrid needs version 4, not %d", where, version)
	}
	if g.Center != nil {
		for _, v := range *g.Center {
			if !isFinite(v) {
				return fieldErr(at(gp, "center"), "sceneformat: %s lightProbeGrid center must be finite", where)
			}
		}
	}
	if g.Size != nil {
		for _, v := range *g.Size {
			if !isFinite(v) {
				return fieldErr(at(gp, "size"), "sceneformat: %s lightProbeGrid size must be finite", where)
			}
		}
		if *g.Size != ([3]float32{}) && (g.Size[0] <= 0 || g.Size[1] <= 0 || g.Size[2] <= 0) {
			return fieldErr(at(gp, "size"), "sceneformat: %s lightProbeGrid size %v must have all positive axes or all zero", where, *g.Size)
		}
	}
	if g.Counts != nil && *g.Counts != ([3]int{}) {
		c := *g.Counts
		if c[0] <= 0 || c[1] <= 0 || c[2] <= 0 {
			return fieldErr(at(gp, "counts"), "sceneformat: %s lightProbeGrid counts %v must have all positive axes", where, c)
		}
		if int64(c[0])*int64(c[1]) > MaxLightProbesPerGrid || int64(c[0])*int64(c[1])*int64(c[2]) > MaxLightProbesPerGrid {
			return fieldErr(at(gp, "counts"), "sceneformat: %s lightProbeGrid counts %v exceed maximum %d probes", where, c, MaxLightProbesPerGrid)
		}
	}
	if g.Intensity != nil && (!isFinite(*g.Intensity) || *g.Intensity < 0) {
		return fieldErr(at(gp, "intensity"), "sceneformat: %s lightProbeGrid intensity %v must be finite and non-negative", where, *g.Intensity)
	}
	if g.Data != "" {
		if !validFilePath(g.Data) {
			return fieldErr(at(gp, "data"), "sceneformat: %s lightProbeGrid data %q must be a slash-separated path relative to the project root", where, g.Data)
		}
		if !IsLightProbeGridPath(g.Data) {
			return fieldErr(at(gp, "data"), "sceneformat: %s lightProbeGrid data %q must be a YAML file ending in .yaml or .yml", where, g.Data)
		}
	}
	return nil
}

// CheckObjectReflectionProbe checks o.ReflectionProbe as Validate checks it in a version 4 scene.
func CheckObjectReflectionProbe(o *ObjectDoc) error {
	if o == nil {
		return nil
	}
	return validateReflectionProbe(CurrentVersion, nil, fmt.Sprintf("object %q", o.ID), o.ReflectionProbe)
}

// CheckObjectLightProbeGrid checks o.LightProbeGrid as Validate checks it in a version 4 scene.
func CheckObjectLightProbeGrid(o *ObjectDoc) error {
	if o == nil {
		return nil
	}
	return validateLightProbeGrid(CurrentVersion, nil, fmt.Sprintf("object %q", o.ID), o.LightProbeGrid)
}
