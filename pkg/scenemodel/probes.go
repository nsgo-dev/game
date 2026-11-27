package scenemodel

import (
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ReflectionProbe is a 3D reflection probe built from a scene object (sceneformat.ObjectDoc.ReflectionProbe),
// linked to its scene3d.Node and holding any baked cubemap and GGX prefiltered specular mip chain loaded
// from Doc.Cubemap.
type ReflectionProbe struct {
	// Node is the scene3d node placing the reflection probe.
	Node *scene3d.Node
	// Object is the ID of the scene object the reflection probe belongs to.
	Object string
	// Doc is a copy of the source reflection probe definition.
	Doc sceneformat.ReflectionProbeDoc
	// Cubemap is the loaded base-mip linear HDR cubemap when Doc.Cubemap is set, or nil when unbaked.
	Cubemap *render3d.HDRCubemap
	// Prefiltered is the GGX roughness-prefiltered HDR cubemap built from Cubemap, or nil when unbaked.
	Prefiltered *render3d.HDRCubemap
}

// WorldBounds returns the probe's capture origin and world-space axis-aligned influence box [boxMin, boxMax].
func (p *ReflectionProbe) WorldBounds() (pos, boxMin, boxMax math3d.Vec3) {
	if p == nil {
		return pos, boxMin, boxMax
	}
	lo, hi := p.Doc.LocalBounds()
	localAABB := math3d.AABB{
		Min: math3d.Vec3{X: lo[0], Y: lo[1], Z: lo[2]},
		Max: math3d.Vec3{X: hi[0], Y: hi[1], Z: hi[2]},
	}
	if p.Node == nil {
		return math3d.Vec3Zero(), localAABB.Min, localAABB.Max
	}
	pos = p.Node.WorldTransform().Pos
	world := localAABB.Transform(p.Node.WorldMatrix())
	return pos, world.Min, world.Max
}

// RuntimeProbe builds a *render3d.ReflectionProbe reflecting p's current node transform, visibility,
// and baked state, optionally attaching gpuCube for GPU shading.
func (p *ReflectionProbe) RuntimeProbe(gpuCube *render3d.GPUCubemap) *render3d.ReflectionProbe {
	if p == nil {
		return nil
	}
	pos, bmin, bmax := p.WorldBounds()
	disabled := p.Node != nil && (!p.Node.EffectiveVisible() || !p.Node.EffectiveEnabled())
	baked := p.Prefiltered != nil && (gpuCube == nil || gpuCube.Valid())
	return &render3d.ReflectionProbe{
		ID:            p.Object,
		Position:      pos,
		BoxMin:        bmin,
		BoxMax:        bmax,
		BlendDistance: p.Doc.BlendDistance,
		Priority:      p.Doc.Priority,
		BoxProjection: p.Doc.EffectiveBoxProjection(),
		Intensity:     p.Doc.EffectiveIntensity(),
		GPUCubemap:    gpuCube,
		Baked:         baked,
		Disabled:      disabled,
	}
}

// LightProbeGrid is a 3D light probe grid built from a scene object (sceneformat.ObjectDoc.LightProbeGrid),
// linked to its scene3d.Node and holding any baked L2 spherical harmonic irradiance grid loaded from Doc.Data.
type LightProbeGrid struct {
	// Node is the scene3d node placing the light probe grid.
	Node *scene3d.Node
	// Object is the ID of the scene object the light probe grid belongs to.
	Object string
	// Doc is a copy of the source light probe grid definition.
	Doc sceneformat.LightProbeGridDoc
	// Grid is the loaded baked *render3d.LightProbeGrid when Doc.Data is set, or nil when unbaked.
	Grid *render3d.LightProbeGrid
}

// WorldBounds returns the world-space axis-aligned volume covering g's light probes.
func (g *LightProbeGrid) WorldBounds() math3d.AABB {
	if g == nil {
		return math3d.AABB{}
	}
	lo, hi := g.Doc.LocalBounds()
	localAABB := math3d.AABB{
		Min: math3d.Vec3{X: lo[0], Y: lo[1], Z: lo[2]},
		Max: math3d.Vec3{X: hi[0], Y: hi[1], Z: hi[2]},
	}
	if g.Node == nil {
		return localAABB
	}
	return localAABB.Transform(g.Node.WorldMatrix())
}

// HasBakedProbes reports whether b contains at least one reflection probe with a baked cubemap
// or light probe grid with baked irradiance data.
func (b *Built) HasBakedProbes() bool {
	if b == nil {
		return false
	}
	for _, rp := range b.ReflectionProbes {
		if rp != nil && rp.Prefiltered != nil {
			return true
		}
	}
	for _, lpg := range b.LightProbeGrids {
		if lpg != nil && lpg.Grid != nil {
			return true
		}
	}
	return false
}

// SampleLightProbes evaluates the scene's visible, enabled, baked light probe grids at worldPos
// and returns the trilinearly interpolated L2 spherical harmonic irradiance scaled by the chosen
// grid's intensity. When multiple baked grids contain worldPos, the smallest-volume grid wins
// (ties break in document order); when worldPos is outside every grid, the nearest grid is sampled
// (clamped to its boundary). Returns (zero, false) when no visible baked light probe grid exists.
func (b *Built) SampleLightProbes(worldPos math3d.Vec3) (render3d.IrradianceSH, bool) {
	if b == nil || len(b.LightProbeGrids) == 0 {
		return render3d.IrradianceSH{}, false
	}
	var (
		bestInside  *LightProbeGrid
		bestVol     = float32(math.Inf(1))
		bestOutside *LightProbeGrid
		bestDistSq  = float32(math.Inf(1))
	)
	for _, g := range b.LightProbeGrids {
		if g == nil || g.Grid == nil {
			continue
		}
		if g.Node != nil && (!g.Node.EffectiveVisible() || !g.Node.EffectiveEnabled()) {
			continue
		}
		bounds := g.Grid.Bounds
		if bounds.Contains(worldPos) {
			sz := bounds.Size()
			vol := sz.X * sz.Y * sz.Z
			if bestInside == nil || vol < bestVol {
				bestInside = g
				bestVol = vol
			}
			continue
		}
		if bestInside != nil {
			continue
		}
		clamped := math3d.Vec3{
			X: math3d.Clamp(worldPos.X, bounds.Min.X, bounds.Max.X),
			Y: math3d.Clamp(worldPos.Y, bounds.Min.Y, bounds.Max.Y),
			Z: math3d.Clamp(worldPos.Z, bounds.Min.Z, bounds.Max.Z),
		}
		d2 := worldPos.DistSq(clamped)
		if bestOutside == nil || d2 < bestDistSq {
			bestOutside = g
			bestDistSq = d2
		}
	}
	chosen := bestInside
	if chosen == nil {
		chosen = bestOutside
	}
	if chosen == nil {
		return render3d.IrradianceSH{}, false
	}
	return chosen.Grid.Sample(worldPos).Scale(chosen.Doc.EffectiveIntensity()), true
}

var errNoLightProbeResolver = errors.New("naming a lightProbeGrid data file requires a LightProbeGridResolver")

func (b *builder) buildReflectionProbe(o *sceneformat.ObjectDoc, n *scene3d.Node) error {
	if o.ReflectionProbe == nil {
		return nil
	}
	rp := &ReflectionProbe{
		Node:   n,
		Object: o.ID,
		Doc:    *o.ReflectionProbe.Clone(),
	}
	if rp.Doc.Cubemap != "" {
		hr, ok := b.models.(HDRResolver)
		if !ok {
			return fmt.Errorf("reflectionProbe cubemap %q: %w", rp.Doc.Cubemap, errNoHDRResolver)
		}
		img, err := hr.HDR(rp.Doc.Cubemap)
		if err != nil {
			return fmt.Errorf("reflectionProbe cubemap %q: %w", rp.Doc.Cubemap, err)
		}
		if img == nil {
			return fmt.Errorf("reflectionProbe cubemap %q: HDRResolver returned nil", rp.Doc.Cubemap)
		}
		cube, err := render3d.CubemapFromHDRImage(img)
		if err != nil {
			return fmt.Errorf("reflectionProbe cubemap %q: %w", rp.Doc.Cubemap, err)
		}
		baseSize := min(cube.Size, 32)
		maxMips := int(gpu.MaxMipLevels(uint32(baseSize), uint32(baseSize)))
		pref, err := render3d.PrefilterEnvironmentHDR(cube, render3d.PrefilterOptions{
			BaseSize:    baseSize,
			MipLevels:   min(maxMips, 5),
			SampleCount: 16,
		})
		if err != nil {
			return fmt.Errorf("reflectionProbe cubemap %q prefilter: %w", rp.Doc.Cubemap, err)
		}
		rp.Cubemap = cube
		rp.Prefiltered = pref
	}
	b.built.ReflectionProbes = append(b.built.ReflectionProbes, rp)
	return nil
}

func (b *builder) buildLightProbeGrid(o *sceneformat.ObjectDoc, n *scene3d.Node) error {
	if o.LightProbeGrid == nil {
		return nil
	}
	lpg := &LightProbeGrid{
		Node:   n,
		Object: o.ID,
		Doc:    *o.LightProbeGrid.Clone(),
	}
	if lpg.Doc.Data != "" {
		lr, ok := b.models.(LightProbeGridResolver)
		if !ok {
			return fmt.Errorf("lightProbeGrid data %q: %w", lpg.Doc.Data, errNoLightProbeResolver)
		}
		grid, err := lr.LightProbeGrid(lpg.Doc.Data)
		if err != nil {
			return fmt.Errorf("lightProbeGrid data %q: %w", lpg.Doc.Data, err)
		}
		if grid == nil {
			return fmt.Errorf("lightProbeGrid data %q: LightProbeGridResolver returned nil", lpg.Doc.Data)
		}
		lpg.Grid = grid
	}
	b.built.LightProbeGrids = append(b.built.LightProbeGrids, lpg)
	return nil
}
