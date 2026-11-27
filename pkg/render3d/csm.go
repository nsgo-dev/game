package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

const (
	// MaxCascades is the maximum number of shadow cascades supported by [CascadedShadowMap], [CSMFrame], and [CSMWGSL].
	MaxCascades = 4

	// CSMGroup is the bind group index [CSMWGSL] and [LitPBRCSMWGSL] use for the cascaded shadow map (uniform buffer at binding 0, depth atlas at binding 1, comparison sampler at binding 2). It is [ShadowGroup] (3), after [CameraGroup], [LightGroup] and [PBRDrawGroup], so a lit PBR pipeline takes the cascaded shadow map instead of the single directional shadow map, fog, IBL or skinning. Custom shaders that need group 3 for something else place the bindings elsewhere with [CSMWGSLForGroup].
	CSMGroup = ShadowGroup

	// CSMUniformSize is the byte size of the CSMUniforms block encoded by [CSMFrame.EncodeUniforms] and declared by [CSMWGSL]: four mat4x4<f32> matrices (256 bytes) plus ten vec4<f32> vectors (160 bytes).
	CSMUniformSize = 4*64 + 10*16

	// DefaultCSMCascadeCount is the default number of cascades used when [CSMConfig.CascadeCount] is zero.
	DefaultCSMCascadeCount = 4

	// DefaultCSMResolution is the default per-cascade shadow-map resolution in texels used when [CSMConfig.Resolution] is zero.
	DefaultCSMResolution = 1024

	// DefaultCSMSplitLambda is the practical split blend factor returned by [DefaultCSMConfig], blending 75% logarithmic and 25% uniform splits.
	DefaultCSMSplitLambda float32 = 0.75

	// DefaultCSMDepthBias is the constant receiver depth bias returned by [DefaultCSMConfig].
	DefaultCSMDepthBias float32 = 0.0015

	// DefaultCSMNormalBias is the world-space normal offset bias (for cascade 0, scaled proportionally to texel size for farther cascades) returned by [DefaultCSMConfig].
	DefaultCSMNormalBias float32 = 0.02

	// DefaultCSMCascadeBlendRange is the fractional cascade transition zone ([0, 0.5] of each cascade's depth span) returned by [DefaultCSMConfig].
	DefaultCSMCascadeBlendRange float32 = 0.1

	// DefaultCSMCasterPadding is the distance in world units pulled back toward the light source when fitting cascade orthographic near planes, so off-frustum casters behind the camera still cast shadows into view.
	DefaultCSMCasterPadding float32 = 25.0

	// DefaultCSMMaxCasters is the default per-frame dynamic caster transform capacity allocated by [NewCascadedShadowMap] when [CSMConfig.MaxCasters] is 0.
	DefaultCSMMaxCasters = 256
)

// ErrInvalidCSM is wrapped by every validation or invalid-argument error returned by the cascaded shadow map API.
var ErrInvalidCSM = errors.New("render3d: invalid cascaded shadow map")

// CSMConfig configures [FitCascades] and [NewCascadedShadowMap].
//
// Use [DefaultCSMConfig] for balanced outdoor defaults (4 cascades, 1024x1024 per cascade, lambda 0.75, texel snapping enabled, 3x3 PCF, 10% cascade blend). Zero values for CascadeCount, Resolution, and MaxCasters fall back to [DefaultCSMCascadeCount], [DefaultCSMResolution], and [DefaultCSMMaxCasters].
type CSMConfig struct {
	// Label names GPU resources in capture tools.
	Label string
	// CascadeCount is the number of cascades in [1, MaxCascades]. Zero defaults to 4.
	CascadeCount int
	// Resolution is the width and height in texels of each cascade tile in the 2D depth atlas. Zero defaults to 1024.
	Resolution int
	// SplitLambda blends uniform (0.0) and logarithmic (1.0) cascade split distances using the practical split scheme. Must lie in [0, 1].
	SplitLambda float32
	// MaxShadowDistance caps the farthest view-space depth covered by shadows. When zero, the camera's far clip plane is used.
	MaxShadowDistance float32
	// DepthBias subtracts a small constant (modulated by surface slope) from receiver light-space depth to prevent shadow acne. Must be >= 0.
	DepthBias float32
	// NormalBias offsets receiver world positions along their surface normal (scaled by each cascade's texel size relative to cascade 0) before projecting into light space. Must be >= 0.
	NormalBias float32
	// CascadeBlendRange is the fraction in [0, 0.5] of each cascade's depth interval used to smoothly cross-fade into the next cascade (and fade out shadows smoothly approaching MaxShadowDistance). Zero disables blending.
	CascadeBlendRange float32
	// TexelSnap fits each cascade with a rotation-invariant bounding sphere and snaps its light-space projection origin to whole shadow-map texel increments, eliminating shadow-edge swimming as the camera moves.
	TexelSnap bool
	// Filter selects hard 1-tap shadows ([ShadowFilterHard]) or 3x3 PCF ([ShadowFilterPCF3x3]).
	Filter ShadowFilter
	// CasterPadding extends each cascade's orthographic near plane backward along -lightDir by this many world units so tall or off-frustum casters behind the camera still cast shadows into view. Must be >= 0.
	CasterPadding float32
	// DebugCascades tints lit surfaces by active cascade (0=red, 1=green, 2=blue, 3=yellow) in [LitPBRCSMWGSL] for inspecting cascade boundaries; [CSMFrame.DebugTint] is its CPU reference.
	DebugCascades bool
	// MaxCasters is the number of distinct non-identity caster model matrices [CascadedShadowMap.DrawCaster] can hold between two [CascadedShadowMap.Update] calls. A model drawn into several cascades takes one slot. Zero defaults to [DefaultCSMMaxCasters] (256).
	MaxCasters int
	// Retained opts every GPU buffer, depth atlas, sampler, and pipeline owned by [CascadedShadowMap] into automatic recreation across GPU device loss.
	Retained bool
}

// DefaultCSMConfig returns balanced defaults for cascaded directional shadows.
func DefaultCSMConfig() CSMConfig {
	return CSMConfig{
		CascadeCount:      DefaultCSMCascadeCount,
		Resolution:        DefaultCSMResolution,
		SplitLambda:       DefaultCSMSplitLambda,
		DepthBias:         DefaultCSMDepthBias,
		NormalBias:        DefaultCSMNormalBias,
		CascadeBlendRange: DefaultCSMCascadeBlendRange,
		TexelSnap:         true,
		Filter:            ShadowFilterPCF3x3,
		CasterPadding:     DefaultCSMCasterPadding,
		MaxCasters:        DefaultCSMMaxCasters,
	}
}

// Validate checks every field of c and returns an error wrapping [ErrInvalidCSM] if any value is out of range or non-finite.
func (c CSMConfig) Validate() error {
	if c.CascadeCount < 0 || c.CascadeCount > MaxCascades {
		return fmt.Errorf("%w: CascadeCount %d must be in [0, %d]", ErrInvalidCSM, c.CascadeCount, MaxCascades)
	}
	if c.Resolution < 0 {
		return fmt.Errorf("%w: Resolution %d must be non-negative", ErrInvalidCSM, c.Resolution)
	}
	if !finite(c.SplitLambda) || c.SplitLambda < 0 || c.SplitLambda > 1 {
		return fmt.Errorf("%w: SplitLambda %v must be in [0, 1]", ErrInvalidCSM, c.SplitLambda)
	}
	if !finite(c.MaxShadowDistance) || c.MaxShadowDistance < 0 {
		return fmt.Errorf("%w: MaxShadowDistance %v must be finite and >= 0", ErrInvalidCSM, c.MaxShadowDistance)
	}
	if !finite(c.DepthBias) || c.DepthBias < 0 {
		return fmt.Errorf("%w: DepthBias %v must be finite and >= 0", ErrInvalidCSM, c.DepthBias)
	}
	if !finite(c.NormalBias) || c.NormalBias < 0 {
		return fmt.Errorf("%w: NormalBias %v must be finite and >= 0", ErrInvalidCSM, c.NormalBias)
	}
	if !finite(c.CascadeBlendRange) || c.CascadeBlendRange < 0 || c.CascadeBlendRange > 0.5 {
		return fmt.Errorf("%w: CascadeBlendRange %v must be in [0, 0.5]", ErrInvalidCSM, c.CascadeBlendRange)
	}
	if !validShadowFilter(c.Filter) {
		return fmt.Errorf("%w: unknown filter mode %d", ErrInvalidCSM, c.Filter)
	}
	if !finite(c.CasterPadding) || c.CasterPadding < 0 {
		return fmt.Errorf("%w: CasterPadding %v must be finite and >= 0", ErrInvalidCSM, c.CasterPadding)
	}
	if c.MaxCasters < 0 {
		return fmt.Errorf("%w: MaxCasters %d must be non-negative", ErrInvalidCSM, c.MaxCasters)
	}
	return nil
}

func (c CSMConfig) normalize() CSMConfig {
	out := c
	if out.CascadeCount <= 0 {
		out.CascadeCount = DefaultCSMCascadeCount
	}
	if out.CascadeCount > MaxCascades {
		out.CascadeCount = MaxCascades
	}
	if out.Resolution <= 0 {
		out.Resolution = DefaultCSMResolution
	}
	if !finite(out.SplitLambda) {
		out.SplitLambda = DefaultCSMSplitLambda
	} else {
		out.SplitLambda = math3d.Clamp(out.SplitLambda, 0, 1)
	}
	if !finite(out.MaxShadowDistance) || out.MaxShadowDistance < 0 {
		out.MaxShadowDistance = 0
	}
	if !finite(out.DepthBias) || out.DepthBias < 0 {
		out.DepthBias = 0
	}
	if !finite(out.NormalBias) || out.NormalBias < 0 {
		out.NormalBias = 0
	}
	if !finite(out.CascadeBlendRange) {
		out.CascadeBlendRange = 0
	} else {
		out.CascadeBlendRange = math3d.Clamp(out.CascadeBlendRange, 0, 0.5)
	}
	if !finite(out.CasterPadding) || out.CasterPadding < 0 {
		out.CasterPadding = 0
	}
	if out.MaxCasters <= 0 {
		out.MaxCasters = DefaultCSMMaxCasters
	}
	return out
}

func csmAtlasGrid(count int) (cols, rows int) {
	switch {
	case count <= 1:
		return 1, 1
	case count == 2:
		return 2, 1
	default:
		return 2, 2
	}
}

// AtlasSize returns the pixel dimensions (width, height) of the 2D depth atlas that packs c's cascades: 1x1 for 1 cascade, 2x1 for 2 cascades, and 2x2 for 3 or 4 cascades.
func (c CSMConfig) AtlasSize() (width, height uint32) {
	n := c.normalize()
	cols, rows := csmAtlasGrid(n.CascadeCount)
	return uint32(n.Resolution * cols), uint32(n.Resolution * rows)
}

// ComputeCSMSplits computes count+1 view-space split depths [d_0, ..., d_count] spanning [near, far] using the practical split scheme (Zhang et al., 2006):
//
//	d_i = lambda * near * (far/near)^(i/count) + (1 - lambda) * (near + (far - near) * i / count)
//
// lambda=0 produces uniform splits; lambda=1 produces logarithmic splits. Returns nil when count <= 0, near <= 0, far <= near, or either bound is non-finite. Non-finite lambda falls back to [DefaultCSMSplitLambda]; out-of-range finite lambda is clamped to [0, 1].
func ComputeCSMSplits(near, far float32, count int, lambda float32) []float32 {
	if count <= 0 || !finite(near) || !finite(far) || near <= 0 || far <= near {
		return nil
	}
	if !finite(lambda) {
		lambda = DefaultCSMSplitLambda
	} else {
		lambda = math3d.Clamp(lambda, 0, 1)
	}
	splits := make([]float32, count+1)
	computeCSMSplitsInto(splits, near, far, count, lambda)
	return splits
}

func computeCSMSplitsInto(dst []float32, near, far float32, count int, lambda float32) {
	dst[0] = near
	dst[count] = far
	if count == 1 {
		return
	}
	n64 := float64(near)
	f64 := float64(far)
	ratio := f64 / n64
	span := f64 - n64
	lam := float64(lambda)
	invCount := 1.0 / float64(count)
	for i := 1; i < count; i++ {
		p := float64(i) * invCount
		logSplit := n64 * math.Pow(ratio, p)
		uniSplit := n64 + span*p
		dst[i] = float32(lam*logSplit + (1.0-lam)*uniSplit)
	}
}

// CSMCascade holds the fitted light-space projection, depth bounds, atlas tile placement, and bounding sphere of a single shadow cascade.
type CSMCascade struct {
	// Near and Far are positive view-space distances covered by this cascade.
	Near float32
	Far  float32
	// LightView transforms world space into light view space (origin at world 0).
	LightView math3d.Mat4
	// LightProj is the orthographic projection mapping light view space into [0, 1] NDC depth.
	LightProj math3d.Mat4
	// LightViewProj is LightProj * LightView.
	LightViewProj math3d.Mat4
	// AtlasRect is the normalized UV sub-rectangle (u0, v0, uScale, vScale) of this cascade's tile inside the 2D depth atlas.
	AtlasRect math3d.Vec4
	// Viewport is [x, y, width, height] in atlas pixels for SetViewport/SetScissor.
	Viewport [4]float32
	// TexelSize is the world-space width of one shadow-map texel.
	TexelSize float32
	// Center and Radius describe the cascade sub-frustum's bounding sphere in world space.
	Center math3d.Vec3
	Radius float32
}

// CSMFrame holds the complete per-frame cascade solution computed by [FitCascades] or [FitCascadesFromCamera], ready to upload to a [CascadedShadowMap] via [CascadedShadowMap.Update] or evaluate on the CPU with [EvaluateCSMShadow] and [CSMFrame.DebugTint].
type CSMFrame struct {
	CascadeCount      int
	Cascades          [MaxCascades]CSMCascade
	Splits            [MaxCascades + 1]float32
	LightDir          math3d.Vec3
	MaxShadowDistance float32
	AtlasWidth        uint32
	AtlasHeight       uint32
	Config            CSMConfig
}

// FitCascades fits directional shadow cascades to the camera frustum defined by camView, camProj (using [0, 1] NDC depth as produced by [math3d.Perspective] and [math3d.Ortho]), and positive view-space clip distances [near, far].
//
// lightDir is the world-space direction light rays travel (from the directional light toward the scene, e.g. {0.3, -1, -0.4}). It is normalized internally; a zero or non-finite vector falls back to (0, -1, 0).
//
// When cfg.TexelSnap is true, each cascade uses a rotation-invariant bounding sphere quantized to 1/16 world unit and snaps its light-space projection origin to whole shadow-map texel steps, preventing shadow-edge swimming under camera motion. FitCascades performs zero heap allocations.
func FitCascades(camView, camProj math3d.Mat4, near, far float32, lightDir math3d.Vec3, cfg CSMConfig) CSMFrame {
	nCfg := cfg.normalize()
	count := nCfg.CascadeCount
	atlasW, atlasH := nCfg.AtlasSize()
	cols, rows := csmAtlasGrid(count)

	// rayNear and rayFar are the view-space distances of camProj's own near and far planes, which parameterise the frustum corner rays below. They can differ from near and far, which only bound the range shadows cover: an orthographic projection may put its near plane at or behind the camera (Near <= 0), while shadows start at a positive depth.
	rayNear, rayFar, rayOK := projectionDepthRange(camProj)
	if !finite(near) || near <= 0 {
		near = 0.1
	}
	if !finite(far) || far <= near {
		far = near + 100
	}
	if !rayOK {
		rayNear, rayFar = near, far
	}
	shadowFar := far
	if nCfg.MaxShadowDistance > 0 && nCfg.MaxShadowDistance < shadowFar {
		shadowFar = nCfg.MaxShadowDistance
	}
	if shadowFar <= near {
		shadowFar = near + 1
	}

	if !finite3(lightDir) || lightDir.LenSq() < 1e-12 {
		lightDir = math3d.Vec3{Y: -1}
	} else {
		lightDir = lightDir.Norm()
	}

	frame := CSMFrame{
		CascadeCount:      count,
		LightDir:          lightDir,
		MaxShadowDistance: shadowFar,
		AtlasWidth:        atlasW,
		AtlasHeight:       atlasH,
		Config:            nCfg,
	}
	computeCSMSplitsInto(frame.Splits[:count+1], near, shadowFar, count, nCfg.SplitLambda)

	// Unproject the 8 corners of the full camera frustum (NDC z=0 near, z=1 far). For any perspective or orthographic frustum, every ray from a near corner to its matching far corner varies linearly with view-space distance d.
	invVP, ok := camProj.Mul(camView).Inverse()
	if !ok {
		invVP = math3d.Mat4Identity()
	}
	ndcXY := [4][2]float32{
		{-1, -1},
		{1, -1},
		{1, 1},
		{-1, 1},
	}
	var fullNear, fullFar [4]math3d.Vec3
	for k := range 4 {
		fullNear[k] = invVP.TransformPoint(math3d.Vec3{X: ndcXY[k][0], Y: ndcXY[k][1], Z: 0})
		fullFar[k] = invVP.TransformPoint(math3d.Vec3{X: ndcXY[k][0], Y: ndcXY[k][1], Z: 1})
	}

	// Build a world-anchored light view basis depending only on lightDir so the texel grid remains stable across camera translations and rotations.
	lightUp := math3d.Vec3Up()
	if absf32(lightDir.Y) > 0.99 {
		lightUp = math3d.Vec3{Z: 1}
	}
	lightBasis := math3d.LookAt(math3d.Vec3Zero(), lightDir, lightUp)

	fullSpan := rayFar - rayNear
	resF := float32(nCfg.Resolution)
	invCols := float32(1.0) / float32(cols)
	invRows := float32(1.0) / float32(rows)

	var corners [8]math3d.Vec3
	for i := range count {
		cNear := frame.Splits[i]
		cFar := frame.Splits[i+1]
		t0 := (cNear - rayNear) / fullSpan
		t1 := (cFar - rayNear) / fullSpan

		var center math3d.Vec3
		for k := range 4 {
			corners[k] = fullNear[k].Lerp(fullFar[k], t0)
			corners[4+k] = fullNear[k].Lerp(fullFar[k], t1)
			center = center.Add(corners[k]).Add(corners[4+k])
		}
		center = center.Scale(1.0 / 8.0)

		var maxDistSq float32
		for k := range 8 {
			dSq := corners[k].Sub(center).LenSq()
			if dSq > maxDistSq {
				maxDistSq = dSq
			}
		}
		radius := float32(math.Sqrt(float64(maxDistSq)))
		if radius < 1e-3 {
			radius = 1e-3
		}

		var left, right, bottom, top, minZ, maxZ, texelSize float32
		if nCfg.TexelSnap {
			// Reserve 0.5 texel margin so snapping centerLS by up to +/-0.5 texel still strictly encloses every sub-frustum corner, then quantize the half-extent to 1/16 world unit so float rounding never jitters texelSize.
			padded := radius
			if nCfg.Resolution > 1 {
				padded = radius * resF / (resF - 1.0)
			}
			halfExtent := float32(math.Ceil(float64(padded)*16.0) / 16.0)
			texelSize = (2.0 * halfExtent) / resF

			centerLS := lightBasis.TransformPoint(center)
			snapX := float32(math.Floor(float64(centerLS.X)/float64(texelSize)+0.5)) * texelSize
			snapY := float32(math.Floor(float64(centerLS.Y)/float64(texelSize)+0.5)) * texelSize
			snapZ := float32(math.Floor(float64(centerLS.Z)/float64(texelSize)+0.5)) * texelSize

			left = snapX - halfExtent
			right = snapX + halfExtent
			bottom = snapY - halfExtent
			top = snapY + halfExtent
			maxZ = snapZ + halfExtent + nCfg.CasterPadding
			minZ = snapZ - halfExtent
		} else {
			minX, maxX := float32(math.Inf(1)), float32(math.Inf(-1))
			minY, maxY := float32(math.Inf(1)), float32(math.Inf(-1))
			minZ, maxZ = float32(math.Inf(1)), float32(math.Inf(-1))
			for k := range 8 {
				p := lightBasis.TransformPoint(corners[k])
				if p.X < minX {
					minX = p.X
				}
				if p.X > maxX {
					maxX = p.X
				}
				if p.Y < minY {
					minY = p.Y
				}
				if p.Y > maxY {
					maxY = p.Y
				}
				if p.Z < minZ {
					minZ = p.Z
				}
				if p.Z > maxZ {
					maxZ = p.Z
				}
			}
			if maxX-minX < 1e-3 {
				mid := 0.5 * (minX + maxX)
				minX, maxX = mid-0.5e-3, mid+0.5e-3
			}
			if maxY-minY < 1e-3 {
				mid := 0.5 * (minY + maxY)
				minY, maxY = mid-0.5e-3, mid+0.5e-3
			}
			if maxZ-minZ < 1e-3 {
				mid := 0.5 * (minZ + maxZ)
				minZ, maxZ = mid-0.5e-3, mid+0.5e-3
			}
			left, right = minX, maxX
			bottom, top = minY, maxY
			maxZ += nCfg.CasterPadding
			texelSize = max(right-left, top-bottom) / resF
		}

		// In right-handed light view space, points closer to the light have larger +Z (since LookAt maps +lightDir to -Z). OrthoZO expects positive-forward near and far distances (-z_view), so near = -maxZ and far = -minZ maps z_view = maxZ (closest to light) -> NDC z = 0 and z_view = minZ -> NDC z = 1.
		lightProj := math3d.Ortho(left, right, bottom, top, -maxZ, -minZ)
		lightVP := lightProj.Mul(lightBasis)

		col := i % cols
		row := i / cols
		tileRect := math3d.Vec4{
			X: float32(col) * invCols,
			Y: float32(row) * invRows,
			Z: invCols,
			W: invRows,
		}
		vp := [4]float32{
			float32(col * nCfg.Resolution),
			float32(row * nCfg.Resolution),
			resF,
			resF,
		}

		frame.Cascades[i] = CSMCascade{
			Near:          cNear,
			Far:           cFar,
			LightView:     lightBasis,
			LightProj:     lightProj,
			LightViewProj: lightVP,
			AtlasRect:     tileRect,
			Viewport:      vp,
			TexelSize:     texelSize,
			Center:        center,
			Radius:        radius,
		}
	}

	return frame
}

// FitCascadesFromCamera fits cascades to a [scene3d.Camera] with the same near and far planes its [scene3d.Camera.ProjMatrix] uses (the defaults [scene3d.Projection.Matrix] fills in for zero or out-of-range fields included). Shadows cover view depths from max(near, 0.1) to the far plane or [CSMConfig.MaxShadowDistance], so an orthographic camera whose near plane is at or behind it (Near <= 0) is shadowed from 0.1 on. When cam is nil, a default perspective camera at the origin looking down -Z is used. lightDir is the world-space direction light rays travel; with [LitPBRCSMWGSL] pass the Direction of the [LightRig]'s first directional light, the one the cascades shadow.
func FitCascadesFromCamera(cam *scene3d.Camera, lightDir math3d.Vec3, cfg CSMConfig) CSMFrame {
	if cam == nil {
		cam = scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), 1, 0.1, 100)
	}
	near, far := cameraDepthRange(cam.Projection)
	return FitCascades(cam.ViewMatrix(), cam.ProjMatrix(), near, far, lightDir, cfg)
}

// cameraDepthRange returns the near and far planes [scene3d.Projection.Matrix] uses for p.
func cameraDepthRange(p scene3d.Projection) (near, far float32) {
	near, far = p.Near, p.Far
	if p.Kind == scene3d.ProjectionOrthographic {
		if near == 0 && far == 0 {
			return 0.1, 1000
		}
	} else if near <= 0 {
		near = 0.1
	}
	if far <= near {
		far = near + 1000
	}
	return near, far
}

// projectionDepthRange returns the positive-forward view-space distances of proj's near (NDC z = 0) and far (NDC z = 1) planes along the view axis, and false when proj is singular or the planes are not finite and ordered.
func projectionDepthRange(proj math3d.Mat4) (near, far float32, ok bool) {
	inv, ok := proj.Inverse()
	if !ok {
		return 0, 0, false
	}
	n := inv.TransformPoint(math3d.Vec3{})
	f := inv.TransformPoint(math3d.Vec3{Z: 1})
	near, far = -n.Z, -f.Z
	if !finite(near) || !finite(far) || far <= near {
		return 0, 0, false
	}
	return near, far, true
}

// SelectCascade returns the primary cascade index for a positive camera view-space depth, along with the next cascade index (or -1 if none / fading out past the final cascade) and blend factor in [0, 1].
//
// When viewDepth <= 0 or viewDepth >= f.MaxShadowDistance (or f.CascadeCount <= 0), SelectCascade returns (-1, -1, 0).
func (f *CSMFrame) SelectCascade(viewDepth float32) (index int, nextIndex int, blend float32) {
	if f == nil || f.CascadeCount <= 0 || viewDepth <= 0 || viewDepth >= f.MaxShadowDistance {
		return -1, -1, 0
	}
	count := min(f.CascadeCount, MaxCascades)
	idx := count - 1
	for i := range count {
		if viewDepth <= f.Cascades[i].Far {
			idx = i
			break
		}
	}
	blendFrac := f.Config.CascadeBlendRange
	if blendFrac <= 0 {
		return idx, -1, 0
	}
	cNear := f.Cascades[idx].Near
	cFar := f.Cascades[idx].Far
	span := max(cFar-cNear, float32(1e-4))
	blendDist := span * blendFrac
	blendStart := cFar - blendDist
	if viewDepth <= blendStart {
		return idx, -1, 0
	}
	t := math3d.Clamp((viewDepth-blendStart)/max(blendDist, float32(1e-4)), 0, 1)
	if idx+1 < count {
		return idx, idx + 1, t
	}
	// Last cascade smoothly fades out toward 1.0 (unshadowed) at MaxShadowDistance.
	return idx, -1, t
}

// EncodeUniforms writes f into dst as the little-endian [CSMUniformSize]-byte uniform block matching [CSMWGSL]'s CSMUniforms struct and returns dst[:CSMUniformSize]. It panics if len(dst) < [CSMUniformSize]. It makes zero heap allocations.
func (f *CSMFrame) EncodeUniforms(dst []byte) []byte {
	if len(dst) < CSMUniformSize {
		panic("render3d: CSMFrame.EncodeUniforms: dst shorter than CSMUniformSize")
	}
	dst = dst[:CSMUniformSize]
	off := 0
	put := func(v float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(v))
		off += 4
	}
	putMat4 := func(m *math3d.Mat4) {
		for _, v := range m {
			put(v)
		}
	}
	putVec4 := func(v math3d.Vec4) {
		put(v.X)
		put(v.Y)
		put(v.Z)
		put(v.W)
	}

	count := min(max(f.CascadeCount, 1), MaxCascades)
	baseTexel := f.Cascades[0].TexelSize
	if baseTexel < 1e-4 {
		baseTexel = 1e-4
	}

	// 4 x light_vp (256 bytes)
	for i := range MaxCascades {
		src := min(i, count-1)
		putMat4(&f.Cascades[src].LightViewProj)
	}
	// 4 x atlas_rect (64 bytes)
	for i := range MaxCascades {
		src := min(i, count-1)
		putVec4(f.Cascades[src].AtlasRect)
	}
	// splits (far depths, 16 bytes)
	for i := range MaxCascades {
		src := min(i, count-1)
		put(f.Cascades[src].Far)
	}
	// split_near (near depths, 16 bytes)
	for i := range MaxCascades {
		src := min(i, count-1)
		put(f.Cascades[src].Near)
	}
	// light_dir (xyz = unit travel dir, w = max_shadow_distance, 16 bytes)
	put(f.LightDir.X)
	put(f.LightDir.Y)
	put(f.LightDir.Z)
	put(f.MaxShadowDistance)
	// bias_params (x = depth_bias, y = normal_bias, z = blend_range, w = cascade_count, 16 bytes)
	put(f.Config.DepthBias)
	put(f.Config.NormalBias)
	put(f.Config.CascadeBlendRange)
	put(float32(count))
	// atlas_params (x = 1/atlasW, y = 1/atlasH, z = filter_mode, w = debug_mode, 16 bytes)
	invW := float32(0)
	invH := float32(0)
	if f.AtlasWidth > 0 {
		invW = 1.0 / float32(f.AtlasWidth)
	}
	if f.AtlasHeight > 0 {
		invH = 1.0 / float32(f.AtlasHeight)
	}
	filterF := float32(0)
	if f.Config.Filter == ShadowFilterPCF3x3 {
		filterF = 1
	}
	debugF := float32(0)
	if f.Config.DebugCascades {
		debugF = 1
	}
	put(invW)
	put(invH)
	put(filterF)
	put(debugF)
	// cascade_scales (relative texel size per cascade for normal bias scaling, 16 bytes)
	for i := range MaxCascades {
		src := min(i, count-1)
		scale := f.Cascades[src].TexelSize / baseTexel
		if scale < 1 {
			scale = 1
		}
		put(scale)
	}
	return dst
}

// CSMDepthWGSL is the depth-only vertex shader used by [CascadedShadowMap] to rasterize shadow casters into a cascade tile of the 2D depth atlas.
//
// Bind groups:
//   - @group(0) @binding(0): CSMCascadeUniform (light_view_proj: mat4x4<f32>)
//   - @group(1) @binding(0): CSMModelUniform (world: mat4x4<f32>)
//
// Vertex buffer 0 matches [MeshVertexLayout].
const CSMDepthWGSL = `struct CSMCascadeUniform {
	light_view_proj: mat4x4<f32>,
}

struct CSMModelUniform {
	world: mat4x4<f32>,
}

@group(0) @binding(0) var<uniform> csm_cascade: CSMCascadeUniform;
@group(1) @binding(0) var<uniform> csm_model: CSMModelUniform;

struct CSMDepthIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

@vertex
fn vs_csm_depth(in: CSMDepthIn) -> @builtin(position) vec4<f32> {
	let world_pos = csm_model.world * vec4<f32>(in.pos, 1.0);
	return csm_cascade.light_view_proj * world_pos;
}
`

// csmStructWGSL declares the CSMUniforms block shared by [CSMWGSL] and [CSMWGSLForGroup].
const csmStructWGSL = `struct CSMUniforms {
	light_vp0: mat4x4<f32>,
	light_vp1: mat4x4<f32>,
	light_vp2: mat4x4<f32>,
	light_vp3: mat4x4<f32>,
	atlas_rect0: vec4<f32>,
	atlas_rect1: vec4<f32>,
	atlas_rect2: vec4<f32>,
	atlas_rect3: vec4<f32>,
	splits: vec4<f32>,
	split_near: vec4<f32>,
	light_dir: vec4<f32>,
	bias_params: vec4<f32>,
	atlas_params: vec4<f32>,
	cascade_scales: vec4<f32>,
}
`

// csmSampleFnWGSL holds the cascade selection and sampling helpers shared by [CSMWGSL] and [CSMWGSLForGroup].
const csmSampleFnWGSL = `
fn csm_sample_cascade(idx: i32, world_pos: vec3<f32>, world_normal: vec3<f32>, ndotl: f32) -> f32 {
	var light_vp: mat4x4<f32>;
	var rect: vec4<f32>;
	var nscale: f32;
	if (idx == 0) {
		light_vp = csm.light_vp0;
		rect = csm.atlas_rect0;
		nscale = csm.cascade_scales.x;
	} else if (idx == 1) {
		light_vp = csm.light_vp1;
		rect = csm.atlas_rect1;
		nscale = csm.cascade_scales.y;
	} else if (idx == 2) {
		light_vp = csm.light_vp2;
		rect = csm.atlas_rect2;
		nscale = csm.cascade_scales.z;
	} else {
		light_vp = csm.light_vp3;
		rect = csm.atlas_rect3;
		nscale = csm.cascade_scales.w;
	}

	let biased_pos = world_pos + world_normal * (csm.bias_params.y * nscale);
	let clip = light_vp * vec4<f32>(biased_pos, 1.0);
	let ndc = clip.xyz / clip.w;
	let local_uv = vec2<f32>(ndc.x * 0.5 + 0.5, 0.5 - ndc.y * 0.5);

	if (local_uv.x < 0.0 || local_uv.x > 1.0 || local_uv.y < 0.0 || local_uv.y > 1.0 || ndc.z < 0.0 || ndc.z > 1.0) {
		return 1.0;
	}

	let bias = csm.bias_params.x * (2.0 - ndotl);
	let cmp_depth = clamp(ndc.z - bias, 0.0, 1.0);

	let texel = csm.atlas_params.xy;
	let uv_min = rect.xy + texel * 0.501;
	let uv_max = rect.xy + rect.zw - texel * 0.501;
	let base_uv = rect.xy + local_uv * rect.zw;

	if (csm.atlas_params.z < 0.5) {
		let suv = clamp(base_uv, uv_min, uv_max);
		return textureSampleCompareLevel(csm_shadow_map, csm_shadow_sampler, suv, cmp_depth);
	}

	var sum: f32 = 0.0;
	for (var dy: i32 = -1; dy <= 1; dy = dy + 1) {
		for (var dx: i32 = -1; dx <= 1; dx = dx + 1) {
			let off = vec2<f32>(f32(dx) * texel.x, f32(dy) * texel.y);
			let suv = clamp(base_uv + off, uv_min, uv_max);
			sum = sum + textureSampleCompareLevel(csm_shadow_map, csm_shadow_sampler, suv, cmp_depth);
		}
	}
	return sum * (1.0 / 9.0);
}

fn csm_select_cascade(view_depth: f32) -> i32 {
	let count = i32(csm.bias_params.w + 0.5);
	if (view_depth <= csm.splits.x || count <= 1) {
		return 0;
	}
	if (view_depth <= csm.splits.y || count == 2) {
		return 1;
	}
	if (view_depth <= csm.splits.z || count == 3) {
		return 2;
	}
	return 3;
}

fn csm_shadow_factor(world_pos: vec3<f32>, world_normal: vec3<f32>, view_depth: f32) -> f32 {
	let max_dist = csm.light_dir.w;
	if (view_depth <= 0.0 || view_depth >= max_dist) {
		return 1.0;
	}
	let count = i32(csm.bias_params.w + 0.5);
	let ndotl = clamp(dot(world_normal, -csm.light_dir.xyz), 0.0, 1.0);
	let idx = csm_select_cascade(view_depth);

	var c_near: f32 = csm.split_near.w;
	var c_far: f32 = csm.splits.w;
	if (idx == 0) {
		c_near = csm.split_near.x;
		c_far = csm.splits.x;
	} else if (idx == 1) {
		c_near = csm.split_near.y;
		c_far = csm.splits.y;
	} else if (idx == 2) {
		c_near = csm.split_near.z;
		c_far = csm.splits.z;
	}

	var vis = csm_sample_cascade(idx, world_pos, world_normal, ndotl);
	let blend_frac = csm.bias_params.z;
	if (blend_frac > 0.0) {
		let span = max(c_far - c_near, 0.0001);
		let blend_dist = span * blend_frac;
		let blend_start = c_far - blend_dist;
		if (view_depth > blend_start) {
			let t = clamp((view_depth - blend_start) / max(blend_dist, 0.0001), 0.0, 1.0);
			if (idx + 1 < count) {
				let next_vis = csm_sample_cascade(idx + 1, world_pos, world_normal, ndotl);
				vis = mix(vis, next_vis, t);
			} else {
				vis = mix(vis, 1.0, t);
			}
		}
	}
	return vis;
}

fn csm_cascade_tint(idx: i32) -> vec3<f32> {
	if (idx == 0) {
		return vec3<f32>(1.0, 0.35, 0.35);
	}
	if (idx == 1) {
		return vec3<f32>(0.35, 1.0, 0.35);
	}
	if (idx == 2) {
		return vec3<f32>(0.35, 0.55, 1.0);
	}
	return vec3<f32>(1.0, 0.9, 0.35);
}

fn csm_view_depth(world_pos: vec3<f32>) -> f32 {
	return -(camera.view * vec4<f32>(world_pos, 1.0)).z;
}

fn sample_csm_shadow(world_pos: vec3<f32>, world_normal: vec3<f32>) -> f32 {
	var n = vec3<f32>(0.0, 1.0, 0.0);
	let n_len = length(world_normal);
	if (n_len > 0.000001) {
		n = world_normal / n_len;
	}
	return csm_shadow_factor(world_pos, n, csm_view_depth(world_pos));
}

fn csm_debug_tint(world_pos: vec3<f32>) -> vec3<f32> {
	let view_depth = csm_view_depth(world_pos);
	if (csm.atlas_params.w < 0.5 || view_depth <= 0.0 || view_depth >= csm.light_dir.w) {
		return vec3<f32>(1.0, 1.0, 1.0);
	}
	return mix(vec3<f32>(1.0, 1.0, 1.0), csm_cascade_tint(csm_select_cascade(view_depth)), 0.55);
}
`

// CSMWGSL declares the cascaded shadow map uniform block, 2D depth atlas and comparison sampler at [CSMGroup] (@group(3)), and portable helpers for sampling them with optional 3x3 PCF, smooth inter-cascade blending, a fade toward [CSMConfig.MaxShadowDistance] and optional debug cascade tinting. It equals CSMWGSLForGroup(CSMGroup).
//
// The helpers read the camera's view matrix, so prepend [CameraWGSL] first. sample_csm_shadow(world_pos, world_normal) returns the visibility in [0, 1] of the light the cascades were fitted to, like sample_directional_shadow in [ShadowWGSL]: pass it as the shadow_vis of shade_pbr_shadowed in [LightWGSL] to shadow the [LightRig]'s first directional light. csm_shadow_factor(world_pos, unit_normal, view_depth) takes the view depth directly, and csm_debug_tint(world_pos) returns the per-cascade color multiplier [CSMConfig.DebugCascades] enables (white when it is off). [LitPBRCSMWGSL] is the ready-made PBR shader built on it.
//
// It uses only algebraic WGSL operations and textureSampleCompareLevel (no exp, exp2, log2 or texture_depth_2d_array), so it runs identically on Metal, Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0 and the software GPU renderer.
const CSMWGSL = csmStructWGSL + `
@group(3) @binding(0) var<uniform> csm: CSMUniforms;
@group(3) @binding(1) var csm_shadow_map: texture_depth_2d;
@group(3) @binding(2) var csm_shadow_sampler: sampler_comparison;
` + csmSampleFnWGSL

// CSMWGSLForGroup returns [CSMWGSL] with its bindings placed at @group(group) instead of [CSMGroup], for custom shaders whose group 3 already holds something else.
func CSMWGSLForGroup(group uint32) string {
	return fmt.Sprintf(`%s
@group(%[2]d) @binding(0) var<uniform> csm: CSMUniforms;
@group(%[2]d) @binding(1) var csm_shadow_map: texture_depth_2d;
@group(%[2]d) @binding(2) var csm_shadow_sampler: sampler_comparison;
%[3]s`, csmStructWGSL, group, csmSampleFnWGSL)
}

// LitPBRCSMWGSL is [LitPBRWGSL] with cascaded directional shadows: the same vertex stage, bind groups [CameraGroup], [LightGroup] and [PBRDrawGroup], and lighting, plus [CSMWGSL] at [CSMGroup]. The fragment stage multiplies the first directional light of the [LightRig] (lights.directional[0]) by sample_csm_shadow and the lit color by csm_debug_tint before encoding to sRGB; every other light, the ambient term and emission are unshadowed. [ShadePointShadowed] with [EvaluateCSMShadow] as the visibility, times [CSMFrame.DebugTint], is its CPU reference. Fit the cascades with that light's Direction. Use [NewLitPBRCSMPipeline] or [LitPBRPipelineOptions].CSMLayout to create a pipeline from it.
const LitPBRCSMWGSL = litPBRPreludeWGSL + CSMWGSL + litPBRCSMFragmentHeadWGSL + `	return vec4<f32>(linear_to_srgb(lit), pbr_draw.base_color.a);
}
`

// litPBRCSMLinearWGSL is [LitPBRCSMWGSL] writing the lit linear color without sRGB encoding, compiled by [NewLitPBRPipelineWithOptions] when [LitPBRPipelineOptions].LinearOutput and CSMLayout are set.
const litPBRCSMLinearWGSL = litPBRPreludeWGSL + CSMWGSL + litPBRCSMFragmentHeadWGSL + `	return vec4<f32>(lit, pbr_draw.base_color.a);
}
`

// litPBRCSMFragmentHeadWGSL opens fs_main for the cascaded-shadow lit PBR shaders and computes the lit linear color lit with directional light 0 attenuated by the cascades.
const litPBRCSMFragmentHeadWGSL = `
@fragment
fn fs_main(in: PBRVertexOut) -> @location(0) vec4<f32> {
	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let shadow_vis = sample_csm_shadow(in.world_pos, in.world_normal);
	let lit = (shade_pbr_shadowed(
		in.world_pos,
		in.world_normal,
		view_dir,
		pbr_draw.base_color.rgb,
		pbr_draw.pbr_params.x,
		pbr_draw.pbr_params.y,
		shadow_vis,
	) + pbr_draw.emissive.rgb) * csm_debug_tint(in.world_pos);
`

// NewLitPBRCSMPipeline compiles [LitPBRCSMWGSL] and creates a pipeline configured like [NewLitPBRPipeline] with csmLayout ([CascadedShadowMap.Layout]) added at [CSMGroup].
func NewLitPBRCSMPipeline(cx *core.Scope, dev *gpu.Device, camLayout, lightLayout, drawLayout, csmLayout *gpu.BindGroupLayout, depthFormat gpu.TextureFormat) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if csmLayout == nil {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: NewLitPBRCSMPipeline requires a cascaded shadow map bind group layout"}
	}
	return NewLitPBRPipelineWithOptions(cx, dev, camLayout, lightLayout, drawLayout, LitPBRPipelineOptions{CSMLayout: csmLayout, DepthFormat: depthFormat})
}

// CSMDepthAtlas holds a CPU copy of a 2D float32 shadow depth atlas (row-major, top-left origin, depth values in [0, 1]) for pure-Go reference evaluation via [EvaluateCSMShadow].
type CSMDepthAtlas struct {
	Width  int
	Height int
	Depth  []float32
}

func (a CSMDepthAtlas) sampleCompare(u, v, cmpDepth float32, linear bool) float32 {
	if a.Width <= 0 || a.Height <= 0 || len(a.Depth) < a.Width*a.Height {
		return 1.0
	}
	u = math3d.Clamp(u, 0, 1)
	v = math3d.Clamp(v, 0, 1)
	w, h := a.Width, a.Height
	cmpTexel := func(x, y int) float32 {
		x = min(max(x, 0), w-1)
		y = min(max(y, 0), h-1)
		if cmpDepth <= a.Depth[y*w+x] {
			return 1.0
		}
		return 0.0
	}
	if linear {
		fx := u*float32(w) - 0.5
		fy := v*float32(h) - 0.5
		x0 := int(math.Floor(float64(fx)))
		y0 := int(math.Floor(float64(fy)))
		tx := fx - float32(x0)
		ty := fy - float32(y0)
		c00 := cmpTexel(x0, y0)
		c10 := cmpTexel(x0+1, y0)
		c01 := cmpTexel(x0, y0+1)
		c11 := cmpTexel(x0+1, y0+1)
		top := c00*(1-tx) + c10*tx
		bot := c01*(1-tx) + c11*tx
		return top*(1-ty) + bot*ty
	}
	ix := int(math.Floor(float64(u * float32(w))))
	iy := int(math.Floor(float64(v * float32(h))))
	return cmpTexel(ix, iy)
}

func evaluateCSMCascade(frame *CSMFrame, atlas CSMDepthAtlas, idx int, worldPos, worldNormal math3d.Vec3, ndotl float32) float32 {
	c := &frame.Cascades[idx]
	baseTexel := frame.Cascades[0].TexelSize
	if baseTexel < 1e-4 {
		baseTexel = 1e-4
	}
	nscale := c.TexelSize / baseTexel
	if nscale < 1 {
		nscale = 1
	}

	biasedPos := worldPos.Add(worldNormal.Scale(frame.Config.NormalBias * nscale))
	clip := c.LightViewProj.MulVec4(biasedPos.Vec4(1))
	if clip.W == 0 {
		return 1.0
	}
	invW := 1.0 / clip.W
	ndcX := clip.X * invW
	ndcY := clip.Y * invW
	ndcZ := clip.Z * invW
	localU := ndcX*0.5 + 0.5
	localV := 0.5 - ndcY*0.5

	if localU < 0 || localU > 1 || localV < 0 || localV > 1 || ndcZ < 0 || ndcZ > 1 {
		return 1.0
	}

	bias := frame.Config.DepthBias * (2.0 - ndotl)
	cmpDepth := math3d.Clamp(ndcZ-bias, 0, 1)

	texelU := float32(1.0) / float32(max(int(frame.AtlasWidth), 1))
	texelV := float32(1.0) / float32(max(int(frame.AtlasHeight), 1))
	rect := c.AtlasRect
	uvMinU := rect.X + texelU*0.501
	uvMinV := rect.Y + texelV*0.501
	uvMaxU := rect.X + rect.Z - texelU*0.501
	uvMaxV := rect.Y + rect.W - texelV*0.501
	baseU := rect.X + localU*rect.Z
	baseV := rect.Y + localV*rect.W

	if frame.Config.Filter == ShadowFilterHard {
		su := math3d.Clamp(baseU, uvMinU, uvMaxU)
		sv := math3d.Clamp(baseV, uvMinV, uvMaxV)
		return atlas.sampleCompare(su, sv, cmpDepth, false)
	}

	var sum float32
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			su := math3d.Clamp(baseU+float32(dx)*texelU, uvMinU, uvMaxU)
			sv := math3d.Clamp(baseV+float32(dy)*texelV, uvMinV, uvMaxV)
			sum += atlas.sampleCompare(su, sv, cmpDepth, true)
		}
	}
	return sum * (1.0 / 9.0)
}

// EvaluateCSMShadow is the pure-Go CPU reference matching csm_shadow_factor in [CSMWGSL]. It returns the shadow visibility factor in [0, 1] (0 = fully in shadow, 1 = fully lit) for a world-space surface point, unit world normal, and positive camera view-space depth.
func EvaluateCSMShadow(frame CSMFrame, atlas CSMDepthAtlas, worldPos, worldNormal math3d.Vec3, viewDepth float32) float32 {
	if frame.CascadeCount <= 0 || viewDepth <= 0 || viewDepth >= frame.MaxShadowDistance {
		return 1.0
	}
	if worldNormal.LenSq() > 1e-12 {
		worldNormal = worldNormal.Norm()
	}
	ndotl := math3d.Clamp(worldNormal.Dot(frame.LightDir.Neg()), 0, 1)
	idx, nextIdx, blend := frame.SelectCascade(viewDepth)
	if idx < 0 {
		return 1.0
	}
	vis := evaluateCSMCascade(&frame, atlas, idx, worldPos, worldNormal, ndotl)
	if blend > 0 {
		if nextIdx >= 0 {
			nextVis := evaluateCSMCascade(&frame, atlas, nextIdx, worldPos, worldNormal, ndotl)
			vis = math3d.Lerp(vis, nextVis, blend)
		} else {
			vis = math3d.Lerp(vis, 1.0, blend)
		}
	}
	return vis
}

// DebugTint returns the color multiplier csm_debug_tint in [CSMWGSL] applies at a positive camera view depth: white (1, 1, 1) when [CSMConfig.DebugCascades] is off or viewDepth is outside the shadowed range, otherwise the active cascade's tint (0 red, 1 green, 2 blue, 3 yellow) mixed 55% into white.
func (f *CSMFrame) DebugTint(viewDepth float32) math3d.Vec3 {
	white := math3d.Vec3{X: 1, Y: 1, Z: 1}
	if f == nil || !f.Config.DebugCascades || viewDepth <= 0 || viewDepth >= f.MaxShadowDistance {
		return white
	}
	idx, _, _ := f.SelectCascade(viewDepth)
	if idx < 0 {
		return white
	}
	t := csmCascadeTint(idx)
	return math3d.Vec3{X: math3d.Lerp(1, t[0], 0.55), Y: math3d.Lerp(1, t[1], 0.55), Z: math3d.Lerp(1, t[2], 0.55)}
}

func csmCascadeTint(idx int) [3]float32 {
	switch idx {
	case 0:
		return [3]float32{1.0, 0.35, 0.35}
	case 1:
		return [3]float32{0.35, 1.0, 0.35}
	case 2:
		return [3]float32{0.35, 0.55, 1.0}
	default:
		return [3]float32{1.0, 0.9, 0.35}
	}
}

// RenderPassStarter is satisfied by both [*gpu.Encoder] and [*gpu.Frame], allowing [CascadedShadowMap.BeginCascadePass] to record depth passes both on a standalone [gpu.SoftwareRenderer] encoder and inside [gpu.ViewProps.OnFrame].
type RenderPassStarter interface {
	BeginRenderPass(desc gpu.RenderPassDesc) *gpu.RenderPass
}

// CascadedShadowMap owns the portable 2D [gpu.FormatDepth32Float] depth atlas, comparison sampler, uniform buffers, bind groups, and depth-only caster pipeline for directional cascaded shadow maps on an NSGo [gpu.Device].
//
// Typical per-frame usage:
//
//	frame := render3d.FitCascadesFromCamera(cam, rig.Directional[0].Direction, csm.Config())
//	_ = csm.Update(dev, frame)
//	for i := range csm.CascadeCount() {
//		if pass := csm.BeginCascadePass(enc, i); pass != nil {
//			csm.DrawCaster(pass, mesh, modelMat)
//			pass.End()
//		}
//	}
//	// Then in the main lit pass, with a pipeline from NewLitPBRCSMPipeline:
//	mainPass.SetBindGroup(render3d.CSMGroup, csm.BindGroup())
type CascadedShadowMap struct {
	dev *gpu.Device
	cfg CSMConfig

	rt      *gpu.RenderTarget
	sampler *gpu.Sampler

	uniformBuf *gpu.Buffer
	layout     *gpu.BindGroupLayout
	group      *gpu.BindGroup

	cascadeLayout *gpu.BindGroupLayout
	cascadeBufs   [MaxCascades]*gpu.Buffer
	cascadeGroups [MaxCascades]*gpu.BindGroup

	casterLayout *gpu.BindGroupLayout
	casterBuf    *gpu.Buffer
	casterGroup  *gpu.BindGroup
	casterStride uint64
	maxCasters   int
	casterSlot   int
	casterSlots  map[math3d.Mat4]uint32

	depthPipe *gpu.Pipeline

	frame        CSMFrame
	atlasCleared bool

	uniformScratch [CSMUniformSize]byte
	matScratch     [64]byte
}

// NewCascadedShadowMap validates cfg and allocates the 2D depth atlas, comparison sampler, uniform buffers, bind groups, and depth-only caster pipeline on dev, scoped to cx.
//
// Invalid configuration returns an error wrapping [ErrInvalidCSM] before any GPU resource is created. A nil or lost device returns [gpu.ErrDeviceLost].
func NewCascadedShadowMap(cx *core.Scope, dev *gpu.Device, cfg CSMConfig) (*CascadedShadowMap, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	nCfg := cfg.normalize()
	atlasW, atlasH := nCfg.AtlasSize()
	if lim := dev.Limits().MaxTextureDimension2D; lim > 0 && (atlasW > lim || atlasH > lim) {
		return nil, fmt.Errorf("%w: atlas size %dx%d exceeds device MaxTextureDimension2D %d", ErrInvalidCSM, atlasW, atlasH, lim)
	}

	label := nCfg.Label
	if label == "" {
		label = "render3d.csm"
	}

	c := &CascadedShadowMap{
		dev:         dev,
		cfg:         nCfg,
		maxCasters:  nCfg.MaxCasters,
		casterSlot:  1,
		casterSlots: make(map[math3d.Mat4]uint32),
	}
	cleanup := func() {
		_ = c.Close()
	}

	var err error
	c.rt, err = dev.CreateRenderTarget(cx, gpu.RenderTargetDesc{
		Label:       label + " depth atlas",
		Width:       atlasW,
		Height:      atlasH,
		DepthFormat: gpu.FormatDepth32Float,
		Retained:    nCfg.Retained,
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	filter := gpu.FilterNearest
	if nCfg.Filter == ShadowFilterPCF3x3 {
		filter = gpu.FilterLinear
	}
	c.sampler, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:     label + " compare sampler",
		MinFilter: filter,
		MagFilter: filter,
		AddressU:  gpu.AddressClampToEdge,
		AddressV:  gpu.AddressClampToEdge,
		AddressW:  gpu.AddressClampToEdge,
		Compare:   gpu.CompareLessEqual,
		Retained:  nCfg.Retained,
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	c.uniformBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " uniforms",
		Size:     CSMUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: nCfg.Retained,
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	c.layout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingComparisonSampler},
		},
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	c.group, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + " group",
		Layout: c.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: c.uniformBuf, Size: CSMUniformSize},
			{Binding: 1, Texture: c.rt.Depth},
			{Binding: 2, Sampler: c.sampler},
		},
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	c.cascadeLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " cascade layout",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageVertex,
			Type:       gpu.BindingUniform,
		}},
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	identBytes := make([]byte, 64)
	encodeMat4Into(identBytes, math3d.Mat4Identity())
	for i := range nCfg.CascadeCount {
		c.cascadeBufs[i], err = dev.CreateBuffer(cx, gpu.BufferDesc{
			Label:    fmt.Sprintf("%s cascade %d", label, i),
			Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
			Data:     identBytes,
			Retained: nCfg.Retained,
		})
		if err != nil {
			cleanup()
			return nil, err
		}
		c.cascadeGroups[i], err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
			Label:   fmt.Sprintf("%s cascade %d group", label, i),
			Layout:  c.cascadeLayout,
			Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: c.cascadeBufs[i], Size: 64}},
		})
		if err != nil {
			cleanup()
			return nil, err
		}
	}

	align := uint64(dev.Limits().MinUniformBufferAlignment)
	if align < 64 {
		align = 256
	}
	c.casterStride = ((64 + align - 1) / align) * align
	casterBytes := make([]byte, c.casterStride*uint64(c.maxCasters))
	// Slot 0 is permanently Identity so world-space casters need zero writes.
	encodeMat4Into(casterBytes[:64], math3d.Mat4Identity())

	c.casterBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " caster models",
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     casterBytes,
		Retained: nCfg.Retained,
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	c.casterLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " caster model layout",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:          0,
			Visibility:       gpu.StageVertex,
			Type:             gpu.BindingUniform,
			HasDynamicOffset: true,
		}},
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	c.casterGroup, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:   label + " caster model group",
		Layout:  c.casterLayout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: c.casterBuf, Size: 64}},
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	depthMod, err := gpu.CompileWGSL(CSMDepthWGSL)
	if err != nil {
		cleanup()
		return nil, err
	}
	c.depthPipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
		Label:         label + " depth pipeline",
		Layouts:       []*gpu.BindGroupLayout{c.cascadeLayout, c.casterLayout},
		Vertex:        gpu.ShaderStage{Module: depthMod, EntryPoint: "vs_csm_depth"},
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  gpu.CullNone,
		},
		DepthStencil: &gpu.DepthStencilState{
			Format:       gpu.FormatDepth32Float,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		},
		Retained: nCfg.Retained,
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	// Populate a valid initial frame so the uniform buffer starts well-defined.
	c.frame = FitCascades(math3d.Mat4Identity(), math3d.Perspective(math3d.DegToRad(60), 1, 0.1, 100), 0.1, 100, math3d.Vec3{Y: -1}, nCfg)
	dev.WriteBuffer(c.uniformBuf, 0, c.frame.EncodeUniforms(c.uniformScratch[:]))
	return c, nil
}

// Update uploads frame's cascade matrices, split distances, atlas rects and shading parameters onto dev with zero heap allocations, and frees every caster slot [CascadedShadowMap.DrawCaster] took since the last Update. Call it once per frame before [CascadedShadowMap.BeginCascadePass].
//
// frame must be fitted with c's cascade count, resolution and filter ([CascadedShadowMap.Config]), because its atlas tiles, viewports, texel size and filter mode index c's atlas and sampler; any other frame returns an error wrapping [ErrInvalidCSM] and leaves c unchanged. The other [CSMConfig] fields (splits, biases, blend range, padding, debug tint) may change from frame to frame.
func (c *CascadedShadowMap) Update(dev *gpu.Device, frame CSMFrame) error {
	if c == nil || !c.Valid() {
		return fmt.Errorf("%w: cascaded shadow map is not valid", ErrInvalidCSM)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if frame.CascadeCount != c.cfg.CascadeCount || frame.Config.CascadeCount != c.cfg.CascadeCount {
		return fmt.Errorf("%w: frame cascade count %d does not match the atlas cascade count %d", ErrInvalidCSM, frame.CascadeCount, c.cfg.CascadeCount)
	}
	if frame.Config.Resolution != c.cfg.Resolution || frame.AtlasWidth != c.rt.Width || frame.AtlasHeight != c.rt.Height {
		return fmt.Errorf("%w: frame resolution %d (atlas %dx%d) does not match the atlas resolution %d (%dx%d)", ErrInvalidCSM, frame.Config.Resolution, frame.AtlasWidth, frame.AtlasHeight, c.cfg.Resolution, c.rt.Width, c.rt.Height)
	}
	if frame.Config.Filter != c.cfg.Filter {
		return fmt.Errorf("%w: frame filter %v does not match the atlas sampler's filter %v", ErrInvalidCSM, frame.Config.Filter, c.cfg.Filter)
	}
	c.dev = dev
	c.frame = frame
	c.casterSlot = 1
	clear(c.casterSlots)
	c.atlasCleared = false

	dev.WriteBuffer(c.uniformBuf, 0, c.frame.EncodeUniforms(c.uniformScratch[:]))
	for i := range frame.CascadeCount {
		encodeMat4Into(c.matScratch[:], frame.Cascades[i].LightViewProj)
		dev.WriteBuffer(c.cascadeBufs[i], 0, c.matScratch[:])
	}
	return nil
}

// BeginCascadePass starts a depth-only render pass targeting cascadeIdx's tile in the 2D depth atlas, sets the tile viewport and scissor rectangle, binds the depth-only caster pipeline, and binds cascadeIdx's light view-projection at group 0.
//
// The first cascade pass after [CascadedShadowMap.Update], whichever cascade it is, clears the whole depth atlas to 1.0 (a clear is not limited by the scissor); every later pass keeps the tiles already drawn with [gpu.LoadLoad]. Render every cascade after each Update, in any order. Returns nil when c, enc, or cascadeIdx is invalid.
func (c *CascadedShadowMap) BeginCascadePass(enc RenderPassStarter, cascadeIdx int) *gpu.RenderPass {
	if c == nil || enc == nil || !c.Valid() || cascadeIdx < 0 || cascadeIdx >= c.frame.CascadeCount {
		return nil
	}
	loadOp := gpu.LoadLoad
	if !c.atlasCleared {
		loadOp = gpu.LoadClear
		c.atlasCleared = true
	}
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label: fmt.Sprintf("csm cascade %d", cascadeIdx),
		DepthStencil: &gpu.DepthStencilAttachment{
			Texture:      c.rt.Depth,
			DepthLoadOp:  loadOp,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   1.0,
		},
	})
	vp := c.frame.Cascades[cascadeIdx].Viewport
	pass.SetViewport(vp[0], vp[1], vp[2], vp[3], 0, 1)
	pass.SetScissor(int32(vp[0]), int32(vp[1]), int32(vp[2]), int32(vp[3]))
	pass.SetPipeline(c.depthPipe)
	pass.SetBindGroup(0, c.cascadeGroups[cascadeIdx])
	return pass
}

// DrawCaster renders mesh as a shadow caster inside a pass opened by [CascadedShadowMap.BeginCascadePass], transforming its vertices by model, and reports whether it drew.
//
// A zero or identity model uses a pre-filled identity slot. Every other model takes one slot of the caster transform buffer the first time it is drawn after [CascadedShadowMap.Update]; drawing the same model again, for example into the next cascade, reuses that slot, so [CSMConfig.MaxCasters] counts distinct models per frame, not draws. Once every slot is taken, DrawCaster draws nothing and returns false rather than overwrite a transform an earlier draw of the same command buffer still reads; raise MaxCasters, or bind caller-owned transforms with [CascadedShadowMap.DrawCasterGroup].
func (c *CascadedShadowMap) DrawCaster(pass *gpu.RenderPass, mesh *GPUMesh, model math3d.Mat4) bool {
	if c == nil || pass == nil || !c.Valid() || !mesh.Valid() {
		return false
	}
	var dynOffset uint32
	if model != (math3d.Mat4{}) && model != math3d.Mat4Identity() {
		off, ok := c.casterSlots[model]
		if !ok {
			if c.casterSlot >= c.maxCasters || c.dev == nil {
				return false
			}
			encodeMat4Into(c.matScratch[:], model)
			byteOff := uint64(c.casterSlot) * c.casterStride
			c.dev.WriteBuffer(c.casterBuf, byteOff, c.matScratch[:])
			off = uint32(byteOff)
			c.casterSlot++
			if finiteMat4(model) {
				c.casterSlots[model] = off
			}
		}
		dynOffset = off
	}
	pass.SetBindGroup(1, c.casterGroup, dynOffset)
	mesh.Draw(pass)
	return true
}

// DrawCasterGroup renders mesh inside a pass opened by [CascadedShadowMap.BeginCascadePass] using a caller-owned model bind group matching [CascadedShadowMap.CasterModelLayout] at group 1.
func (c *CascadedShadowMap) DrawCasterGroup(pass *gpu.RenderPass, mesh *GPUMesh, modelGroup *gpu.BindGroup, dynamicOffsets ...uint32) {
	if c == nil || pass == nil || !c.Valid() || !mesh.Valid() || modelGroup == nil || !modelGroup.Valid() {
		return
	}
	pass.SetBindGroup(1, modelGroup, dynamicOffsets...)
	mesh.Draw(pass)
}

// Config returns the normalized [CSMConfig] used to create c.
func (c *CascadedShadowMap) Config() CSMConfig {
	if c == nil {
		return CSMConfig{}
	}
	return c.cfg
}

// Frame returns the most recently uploaded [CSMFrame].
func (c *CascadedShadowMap) Frame() CSMFrame {
	if c == nil {
		return CSMFrame{}
	}
	return c.frame
}

// CascadeCount returns the number of cascades of the frame last passed to [CascadedShadowMap.Update], which [CascadedShadowMap.BeginCascadePass] accepts; it always equals [CSMConfig.CascadeCount] of [CascadedShadowMap.Config].
func (c *CascadedShadowMap) CascadeCount() int {
	if c == nil {
		return 0
	}
	return c.frame.CascadeCount
}

// AtlasWidth returns the depth atlas width in texels.
func (c *CascadedShadowMap) AtlasWidth() uint32 {
	if c == nil || c.rt == nil {
		return 0
	}
	return c.rt.Width
}

// AtlasHeight returns the depth atlas height in texels.
func (c *CascadedShadowMap) AtlasHeight() uint32 {
	if c == nil || c.rt == nil {
		return 0
	}
	return c.rt.Height
}

// DepthTexture returns the underlying 2D [gpu.FormatDepth32Float] atlas texture.
func (c *CascadedShadowMap) DepthTexture() *gpu.Texture {
	if c == nil || c.rt == nil {
		return nil
	}
	return c.rt.Depth
}

// Sampler returns the comparison sampler bound with the depth atlas.
func (c *CascadedShadowMap) Sampler() *gpu.Sampler {
	if c == nil {
		return nil
	}
	return c.sampler
}

// UniformBuffer returns the [CSMUniformSize]-byte uniform buffer.
func (c *CascadedShadowMap) UniformBuffer() *gpu.Buffer {
	if c == nil {
		return nil
	}
	return c.uniformBuf
}

// Layout returns the bind group layout a lit pipeline places at [CSMGroup].
func (c *CascadedShadowMap) Layout() *gpu.BindGroupLayout {
	if c == nil {
		return nil
	}
	return c.layout
}

// BindGroup returns the bind group a lit render pass sets at [CSMGroup].
func (c *CascadedShadowMap) BindGroup() *gpu.BindGroup {
	if c == nil {
		return nil
	}
	return c.group
}

// DepthPipeline returns the depth-only caster pipeline used by [BeginCascadePass].
func (c *CascadedShadowMap) DepthPipeline() *gpu.Pipeline {
	if c == nil {
		return nil
	}
	return c.depthPipe
}

// CascadeBindGroup returns the group-0 bind group holding cascade idx's light_view_proj uniform buffer, or nil if idx is out of range.
func (c *CascadedShadowMap) CascadeBindGroup(idx int) *gpu.BindGroup {
	if c == nil || idx < 0 || idx >= c.cfg.CascadeCount {
		return nil
	}
	return c.cascadeGroups[idx]
}

// CasterModelLayout returns the group-1 bind group layout expected by [CascadedShadowMap.DepthPipeline] and [CascadedShadowMap.DrawCasterGroup].
func (c *CascadedShadowMap) CasterModelLayout() *gpu.BindGroupLayout {
	if c == nil {
		return nil
	}
	return c.casterLayout
}

// ByteSize returns the GPU memory held by c's depth atlas and uniform buffers, for [RenderStats] and [Budget] accounting.
func (c *CascadedShadowMap) ByteSize() uint64 {
	if c == nil {
		return 0
	}
	var total uint64
	if c.rt != nil {
		total += gpu.FormatDepth32Float.ByteSize(c.rt.Width, c.rt.Height)
	}
	if c.uniformBuf != nil {
		total += c.uniformBuf.Size()
	}
	if c.casterBuf != nil {
		total += c.casterBuf.Size()
	}
	for i := range c.cfg.CascadeCount {
		if c.cascadeBufs[i] != nil {
			total += c.cascadeBufs[i].Size()
		}
	}
	return total
}

// Valid reports whether every GPU resource owned by c is open and belongs to a live device.
func (c *CascadedShadowMap) Valid() bool {
	if c == nil || c.rt == nil || !c.rt.Valid() ||
		c.sampler == nil || !c.sampler.Valid() ||
		c.uniformBuf == nil || !c.uniformBuf.Valid() ||
		c.layout == nil || !c.layout.Valid() ||
		c.group == nil || !c.group.Valid() ||
		c.cascadeLayout == nil || !c.cascadeLayout.Valid() ||
		c.casterLayout == nil || !c.casterLayout.Valid() ||
		c.casterBuf == nil || !c.casterBuf.Valid() ||
		c.casterGroup == nil || !c.casterGroup.Valid() ||
		c.depthPipe == nil || !c.depthPipe.Valid() {
		return false
	}
	for i := range c.cfg.CascadeCount {
		if c.cascadeBufs[i] == nil || !c.cascadeBufs[i].Valid() ||
			c.cascadeGroups[i] == nil || !c.cascadeGroups[i].Valid() {
			return false
		}
	}
	return true
}

// Close releases every GPU resource owned by c now rather than waiting for its owning scope. It is safe to call on a nil receiver or more than once.
func (c *CascadedShadowMap) Close() error {
	if c == nil {
		return nil
	}
	var errs []error
	closeIf := func(cl interface{ Close() error }) {
		if cl == nil {
			return
		}
		if err := cl.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	closeIf(c.depthPipe)
	closeIf(c.casterGroup)
	closeIf(c.casterLayout)
	closeIf(c.casterBuf)
	for i := range c.cascadeGroups {
		closeIf(c.cascadeGroups[i])
		closeIf(c.cascadeBufs[i])
	}
	closeIf(c.cascadeLayout)
	closeIf(c.group)
	closeIf(c.layout)
	closeIf(c.uniformBuf)
	closeIf(c.sampler)
	closeIf(c.rt)
	return errors.Join(errs...)
}

// ReadCSMDepthAtlasAsync asynchronously reads back c's [gpu.FormatDepth32Float] shadow atlas into a CPU [CSMDepthAtlas] and invokes fn on the engine goroutine (matching [gpu.Device.ReadTexture]).
func ReadCSMDepthAtlasAsync(dev *gpu.Device, csm *CascadedShadowMap, fn func(CSMDepthAtlas, error)) {
	if fn == nil {
		return
	}
	if dev == nil {
		fn(CSMDepthAtlas{}, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"})
		return
	}
	if csm == nil || !csm.Valid() {
		fn(CSMDepthAtlas{}, fmt.Errorf("%w: cascaded shadow map is not valid", ErrInvalidCSM))
		return
	}
	w := csm.AtlasWidth()
	h := csm.AtlasHeight()
	wantBytes := int(w) * int(h) * 4
	dev.ReadTexture(csm.DepthTexture(), gpu.TextureRegion{Width: w, Height: h}, func(data []byte, err error) {
		if err != nil {
			fn(CSMDepthAtlas{}, err)
			return
		}
		if len(data) < wantBytes {
			fn(CSMDepthAtlas{}, fmt.Errorf("%w: depth readback returned %d bytes, want %d", ErrInvalidCSM, len(data), wantBytes))
			return
		}
		depths := make([]float32, int(w)*int(h))
		for i := range depths {
			depths[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
		}
		fn(CSMDepthAtlas{
			Width:  int(w),
			Height: int(h),
			Depth:  depths,
		}, nil)
	})
}

// ReadCSMDepthAtlas reads back c's [gpu.FormatDepth32Float] shadow atlas synchronously (for [gpu.SoftwareRenderer] tests where [gpu.Device.ReadTexture] completes immediately). Inside a [gpu.View], use [ReadCSMDepthAtlasAsync].
func ReadCSMDepthAtlas(dev *gpu.Device, csm *CascadedShadowMap) (CSMDepthAtlas, error) {
	var (
		atlas  CSMDepthAtlas
		gotErr error
		called bool
	)
	ReadCSMDepthAtlasAsync(dev, csm, func(a CSMDepthAtlas, err error) {
		atlas = a
		gotErr = err
		called = true
	})
	if !called {
		return CSMDepthAtlas{}, fmt.Errorf("%w: depth readback is asynchronous on this device; use ReadCSMDepthAtlasAsync", ErrInvalidCSM)
	}
	return atlas, gotErr
}

func encodeMat4Into(dst []byte, m math3d.Mat4) {
	for i, v := range m {
		binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(v))
	}
}

// finiteMat4 reports whether every element of m is finite, so it can serve as a map key that compares equal to itself.
func finiteMat4(m math3d.Mat4) bool {
	for _, v := range m {
		if !finite(v) {
			return false
		}
	}
	return true
}
