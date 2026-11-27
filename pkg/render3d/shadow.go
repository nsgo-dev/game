package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Directional shadow mapping constants and defaults (#117).
const (
	// ShadowGroup is the bind group index [ShadowWGSL] and [LitPBRShadowWGSL] reserve for directional shadow data (uniform buffer at binding 0, depth texture at binding 1, comparison sampler at binding 2). It is the last of the four bind groups every NSGo backend offers, after [CameraGroup] (0), [LightGroup] (1) and [PBRDrawGroup] (2), so it is the same slot [FogGroup], [IBLGroup] and [LitPBRSkinGroup] use and a lit PBR pipeline takes only one of them. Custom shaders that need group 3 for something else can place the shadow bindings elsewhere with [ShadowWGSLForGroup].
	ShadowGroup = 3

	// ShadowUniformSize is the byte size of the [ShadowUniforms] block written by [AppendShadowUniforms] and declared by [ShadowWGSL]: one mat4x4<f32> plus two vec4<f32> vectors (96 bytes).
	ShadowUniformSize = 64 + 16 + 16

	// ShadowCasterUniformSize is the byte size of one caster's light-space MVP matrix in [ShadowDepthWGSL].
	ShadowCasterUniformSize = 64

	// DefaultShadowResolution is the default width and height in texels of a [DirectionalShadowMap].
	DefaultShadowResolution uint32 = 1024
	// MinShadowResolution is the smallest shadow map resolution accepted by [DirectionalShadowConfig.Validate] and [ShadowMapOptions.Validate].
	MinShadowResolution uint32 = 16
	// MaxShadowResolution is the largest shadow map resolution accepted by [DirectionalShadowConfig.Validate] and [ShadowMapOptions.Validate].
	MaxShadowResolution uint32 = 8192

	// DefaultShadowMaxCasters is the default number of caster draws supported per shadow pass when [ShadowMapOptions.MaxCasters] is zero.
	DefaultShadowMaxCasters uint32 = 256

	// DefaultShadowDepthBias is the default receiver-side clip-space depth bias applied in [ShadowWGSL] and [EvaluateDirectionalShadow].
	DefaultShadowDepthBias float32 = 0.0015
	// DefaultShadowNormalBias is the default world-space surface-normal offset applied before light-space projection to suppress shadow acne on sloped surfaces.
	DefaultShadowNormalBias float32 = 0.02
	// DefaultShadowStrength is the default shadow intensity in [0, 1] (1 = full occlusion of direct directional light).
	DefaultShadowStrength float32 = 1.0
)

// ErrInvalidShadow is wrapped by validation errors returned by [DirectionalShadowConfig], [ShadowMapOptions], [NewDirectionalShadowMap], and [ShadowDepthGridFromBytes].
var ErrInvalidShadow = errors.New("render3d: invalid directional shadow")

// ShadowFilter selects the shadow-map sampling kernel.
type ShadowFilter uint8

const (
	// ShadowFilterHard evaluates a single nearest-neighbor depth comparison tap (crisp texel-aligned shadow edges).
	ShadowFilterHard ShadowFilter = iota
	// ShadowFilterPCF3x3 averages a 3x3 grid of 9 percentage-closer comparison taps (using a linear comparison sampler for 2x2 hardware bilinear PCF per tap) for smooth penumbra transitions.
	ShadowFilterPCF3x3
)

// String returns a human-readable name for f.
func (f ShadowFilter) String() string {
	switch f {
	case ShadowFilterHard:
		return "hard"
	case ShadowFilterPCF3x3:
		return "pcf3x3"
	default:
		return fmt.Sprintf("ShadowFilter(%d)", f)
	}
}

func validShadowFilter(f ShadowFilter) bool {
	return f == ShadowFilterHard || f == ShadowFilterPCF3x3
}

func isPowerOfTwoU32(v uint32) bool {
	return v != 0 && (v&(v-1)) == 0
}

func validateShadowResolution(res uint32) error {
	if res < MinShadowResolution || res > MaxShadowResolution {
		return fmt.Errorf("%w: resolution %d out of range [%d, %d]", ErrInvalidShadow, res, MinShadowResolution, MaxShadowResolution)
	}
	if !isPowerOfTwoU32(res) {
		return fmt.Errorf("%w: resolution %d must be a power of two", ErrInvalidShadow, res)
	}
	return nil
}

// DirectionalShadowConfig holds the mathematical and shading parameters for a directional light's orthographic shadow frustum.
type DirectionalShadowConfig struct {
	// Direction is the world-space direction light rays travel (from the sun toward the scene). It need not be unit length, but must be finite and non-zero. With [LitPBRShadowWGSL] set it to the Direction of the [LightRig]'s first directional light, the one the shadow attenuates.
	Direction math3d.Vec3
	// Target is the world-space center of the orthographic shadow volume used by [DirectionalShadowConfig.LightViewProj] (ignored by [DirectionalShadowConfig.FitBounds]).
	Target math3d.Vec3
	// OrthoSize is the full width and height (in world units) of the square orthographic frustum used by [DirectionalShadowConfig.LightViewProj]. When zero, defaults to 20.
	OrthoSize float32
	// DepthRange is the full depth of the orthographic volume of [DirectionalShadowConfig.LightViewProj] along Direction, centred on Target: points DepthRange/2 toward the light from Target map to depth 0 and points DepthRange/2 past it map to depth 1, so casters and receivers must lie within that slab. Zero defaults to max(OrthoSize*2, 100). [DirectionalShadowConfig.FitBounds] ignores it and derives the depth range from the bounds.
	DepthRange float32
	// Resolution is the shadow map width and height in texels used to compute TexelSize and texel snapping. Zero defaults to [DefaultShadowResolution] (1024).
	Resolution uint32
	// DepthBias is the receiver-side depth bias in [0, 1] clip depth units (non-negative).
	DepthBias float32
	// NormalBias is the world-space offset applied along the receiver's surface normal before projecting into light space (non-negative).
	NormalBias float32
	// PCFKernel selects [ShadowFilterHard] (1-tap) or [ShadowFilterPCF3x3] (9-tap 3x3 PCF).
	PCFKernel ShadowFilter
	// Strength scales shadow darkness in [0, 1] (0 disables shadows; 1 fully occludes the directional light in shadow).
	Strength float32
	// TexelSnap keeps the shadow map's texel grid fixed in world space so shadow edges do not shimmer as the framing moves: the light-space XY center snaps to whole shadow-map texels, and [DirectionalShadowConfig.FitBounds] sizes the frustum from the bounds' bounding sphere, rounded up to a coarse step, instead of their exact light-space extent, so the texel size stays constant while the bounds move or the light turns.
	TexelSnap bool
	// CasterPadding extends the near plane of [DirectionalShadowConfig.FitBounds] backward toward the light (in world units, non-negative) so occluders slightly behind the fitted bounds still cast shadows.
	CasterPadding float32
}

// DefaultDirectionalShadowConfig returns a ready-to-use directional shadow configuration with 1024x1024 3x3 PCF shadows, texel snapping, and balanced depth/normal bias.
func DefaultDirectionalShadowConfig() DirectionalShadowConfig {
	return DirectionalShadowConfig{
		Direction:  math3d.Vec3{X: -0.4, Y: -1.0, Z: -0.35}.Norm(),
		OrthoSize:  20,
		Resolution: DefaultShadowResolution,
		DepthBias:  DefaultShadowDepthBias,
		NormalBias: DefaultShadowNormalBias,
		PCFKernel:  ShadowFilterPCF3x3,
		Strength:   DefaultShadowStrength,
		TexelSnap:  true,
	}
}

func (c DirectionalShadowConfig) effectiveResolution() uint32 {
	if c.Resolution == 0 {
		return DefaultShadowResolution
	}
	return c.Resolution
}

// Validate checks that every field of c is finite and within valid ranges. Every error wraps [ErrInvalidShadow].
func (c DirectionalShadowConfig) Validate() error {
	if !finite3(c.Direction) || c.Direction.LenSq() < 1e-12 {
		return fmt.Errorf("%w: light direction %v must be finite and non-zero", ErrInvalidShadow, c.Direction)
	}
	if !finite3(c.Target) {
		return fmt.Errorf("%w: target %v must be finite", ErrInvalidShadow, c.Target)
	}
	if !finite(c.OrthoSize) || c.OrthoSize < 0 {
		return fmt.Errorf("%w: OrthoSize %v must be finite and non-negative", ErrInvalidShadow, c.OrthoSize)
	}
	if !finite(c.DepthRange) || c.DepthRange < 0 {
		return fmt.Errorf("%w: DepthRange %v must be finite and non-negative", ErrInvalidShadow, c.DepthRange)
	}
	if err := validateShadowResolution(c.effectiveResolution()); err != nil {
		return err
	}
	if !finite(c.DepthBias) || c.DepthBias < 0 {
		return fmt.Errorf("%w: DepthBias %v must be finite and non-negative", ErrInvalidShadow, c.DepthBias)
	}
	if !finite(c.NormalBias) || c.NormalBias < 0 {
		return fmt.Errorf("%w: NormalBias %v must be finite and non-negative", ErrInvalidShadow, c.NormalBias)
	}
	if !validShadowFilter(c.PCFKernel) {
		return fmt.Errorf("%w: invalid PCFKernel %d", ErrInvalidShadow, c.PCFKernel)
	}
	if !finite(c.Strength) || c.Strength < 0 || c.Strength > 1 {
		return fmt.Errorf("%w: Strength %v must be finite and in [0, 1]", ErrInvalidShadow, c.Strength)
	}
	if !finite(c.CasterPadding) || c.CasterPadding < 0 {
		return fmt.Errorf("%w: CasterPadding %v must be finite and non-negative", ErrInvalidShadow, c.CasterPadding)
	}
	return nil
}

// lightBasisView builds a deterministic rotation-only right-handed light view matrix looking along unit direction dir from the world origin. Using a translation-free light basis ensures texel-snapped orthographic projections align to a world-locked texel grid independent of camera or target motion.
func lightBasisView(dir math3d.Vec3) math3d.Mat4 {
	up := math3d.Vec3{Y: 1}
	if math.Abs(float64(dir.Y)) > 0.99 {
		up = math3d.Vec3{Z: 1}
	}
	return math3d.LookAt(math3d.Vec3Zero(), dir, up)
}

func snapToTexel(v, texel float32) float32 {
	if texel <= 0 {
		return v
	}
	return float32(math.Floor(float64(v/texel)+0.5)) * texel
}

// LightViewProj computes the orthographic light-space view-projection matrix (with [0, 1] NDC depth) centered on c.Target, c.OrthoSize wide and high and c.DepthRange deep, with c.Target at depth 0.5. When c.TexelSnap is true, the light-space XY center is snapped to exact texel increments of c.Resolution to eliminate shadow swimming.
func (c DirectionalShadowConfig) LightViewProj() (math3d.Mat4, error) {
	if err := c.Validate(); err != nil {
		return math3d.Mat4{}, err
	}
	dir := c.Direction.Norm()
	size := c.OrthoSize
	if size == 0 {
		size = 20
	}
	depthRange := c.DepthRange
	if depthRange == 0 {
		depthRange = max(size*2, 100)
	}
	res := c.effectiveResolution()
	view := lightBasisView(dir)
	centerLS := view.TransformPoint(c.Target)
	cx, cy := centerLS.X, centerLS.Y
	if c.TexelSnap {
		texel := size / float32(res)
		cx = snapToTexel(cx, texel)
		cy = snapToTexel(cy, texel)
	}
	half := size * 0.5
	depthHalf := depthRange * 0.5
	// Map the target's light-space Z to the middle of the depth range.
	zNear := -centerLS.Z - depthHalf
	zFar := -centerLS.Z + depthHalf
	proj := math3d.OrthoZO(cx-half, cx+half, cy-half, cy+half, zNear, zFar)
	return proj.Mul(view), nil
}

// FitBounds computes an orthographic light-space view-projection matrix (with [0, 1] NDC depth) framing sceneBounds along lightDir (falling back to c.Direction when lightDir is zero).
//
// Without c.TexelSnap the frustum hugs the bounds' light-space extent, which gives the most shadow-map texels per world unit but changes the texel size whenever the bounds or the light move. With c.TexelSnap the frustum width is the diameter of the bounds' bounding sphere plus a margin, rounded up to a multiple of a sixteenth of the next power of two: that width does not change as the light turns and changes only in coarse steps as the bounds grow or shrink, and the light-space XY center then snaps to whole texels, so between two fits with the same width every static world point moves by a whole number of texels and shadow edges do not shimmer. Every corner of sceneBounds stays inside [0, 1]^2 either way.
func (c DirectionalShadowConfig) FitBounds(lightDir math3d.Vec3, sceneBounds math3d.AABB) (math3d.Mat4, error) {
	if lightDir != (math3d.Vec3{}) {
		c.Direction = lightDir
	}
	if err := c.Validate(); err != nil {
		return math3d.Mat4{}, err
	}
	if sceneBounds.Empty() || !sceneBounds.IsFinite() {
		return math3d.Mat4{}, fmt.Errorf("%w: scene bounds %+v must be non-empty and finite", ErrInvalidShadow, sceneBounds)
	}
	dir := c.Direction.Norm()
	res := c.effectiveResolution()
	view := lightBasisView(dir)
	ls := sceneBounds.Transform(view)

	var extent, cx, cy float32
	if c.TexelSnap {
		// The bounding sphere's diameter does not depend on the light direction, and quantizing it keeps the texel size, and so the texel grid, fixed while the bounds move. Every corner lies within diameter/2 of the center in light space; a margin of at least one texel per side covers the half-texel shift of the snapped center, since extent*(1 - 1/res) >= diameter*(1 + 2/res)*(1 - 1/res) >= diameter.
		diameter := max(sceneBounds.Max.Sub(sceneBounds.Min).Len(), 1e-3)
		extent = quantizeShadowExtent(diameter * max(1.02, 1+2/float32(res)))
		center := view.TransformPoint(sceneBounds.Center())
		texel := extent / float32(res)
		cx = snapToTexel(center.X, texel)
		cy = snapToTexel(center.Y, texel)
	} else {
		// Keep a small border margin so casters on the AABB boundary do not touch the clamped shadow edge.
		extent = max(ls.Max.X-ls.Min.X, ls.Max.Y-ls.Min.Y, 1e-3) * 1.02
		cx = (ls.Min.X + ls.Max.X) * 0.5
		cy = (ls.Min.Y + ls.Max.Y) * 0.5
	}
	half := extent * 0.5

	depthSpan := ls.Max.Z - ls.Min.Z
	if depthSpan < 1e-3 {
		depthSpan = 1e-3
	}
	zMargin := max(depthSpan*0.02, 1e-3)
	// In light view space, larger Z is closer to the light and smaller (more negative) Z is farther along lightDir.
	zNear := -ls.Max.Z - c.CasterPadding - zMargin
	zFar := -ls.Min.Z + zMargin

	proj := math3d.OrthoZO(cx-half, cx+half, cy-half, cy+half, zNear, zFar)
	return proj.Mul(view), nil
}

// quantizeShadowExtent rounds v up to a multiple of a sixteenth of the next power of two at or above v, so a slowly growing or shrinking extent keeps the same value (and texel size) until it crosses a step, and the result is less than an eighth larger than v.
func quantizeShadowExtent(v float32) float32 {
	pow2 := float32(math.Exp2(math.Ceil(math.Log2(float64(v)))))
	step := pow2 / 16
	return float32(math.Ceil(float64(v/step))) * step
}

// Uniforms packages c and lightViewProj into a [ShadowUniforms] ready to upload with [AppendShadowUniforms] or [DirectionalShadowMap.WriteUniforms].
func (c DirectionalShadowConfig) Uniforms(lightViewProj math3d.Mat4) ShadowUniforms {
	dir := c.Direction
	if dir.LenSq() > 1e-12 {
		dir = dir.Norm()
	} else {
		dir = math3d.Vec3{Y: -1}
	}
	res := c.effectiveResolution()
	return ShadowUniforms{
		LightViewProj:  lightViewProj,
		LightDirection: dir,
		Strength:       clamp01f(c.Strength),
		DepthBias:      max(c.DepthBias, 0),
		NormalBias:     max(c.NormalBias, 0),
		TexelSize:      1.0 / float32(res),
		PCFKernel:      c.PCFKernel,
	}
}

// UniformsForBounds fits c to sceneBounds via [DirectionalShadowConfig.FitBounds] and returns the resulting [ShadowUniforms].
func (c DirectionalShadowConfig) UniformsForBounds(sceneBounds math3d.AABB) (ShadowUniforms, error) {
	lvp, err := c.FitBounds(c.Direction, sceneBounds)
	if err != nil {
		return ShadowUniforms{}, err
	}
	return c.Uniforms(lvp), nil
}

// ShadowUniforms is the per-frame directional shadow uniform block matching [ShadowWGSL] byte for byte ([ShadowUniformSize] = 96 bytes).
type ShadowUniforms struct {
	// LightViewProj transforms world-space points into light clip space with [0, 1] depth.
	LightViewProj math3d.Mat4
	// LightDirection is the unit direction light rays travel (from the light toward the scene). Encoded into the WGSL uniform as to_light = -LightDirection so shaders compute dot(N, shadow.to_light.xyz) directly.
	LightDirection math3d.Vec3
	// Strength scales shadow darkness in [0, 1].
	Strength float32
	// DepthBias is the receiver clip-space depth bias.
	DepthBias float32
	// NormalBias is the world-space normal offset applied before projecting into light space.
	NormalBias float32
	// TexelSize is 1 / resolution in UV units, used for 3x3 PCF tap offsets.
	TexelSize float32
	// PCFKernel selects [ShadowFilterHard] or [ShadowFilterPCF3x3].
	PCFKernel ShadowFilter
}

// AppendShadowUniforms appends u to dst in the little-endian layout declared by [ShadowWGSL] ([ShadowUniformSize] bytes) and returns the extended slice. It allocates only when dst lacks capacity for [ShadowUniformSize] more bytes.
func AppendShadowUniforms(dst []byte, u ShadowUniforms) []byte {
	start := len(dst)
	dst = grow(dst, ShadowUniformSize)
	encodeShadowUniforms(dst[start:], u)
	return dst
}

// Encode writes u into dst as the little-endian [ShadowUniformSize]-byte block declared by [ShadowWGSL] and returns dst[:ShadowUniformSize]. It panics if len(dst) is less than [ShadowUniformSize].
func (u ShadowUniforms) Encode(dst []byte) []byte {
	if len(dst) < ShadowUniformSize {
		panic("render3d: ShadowUniforms.Encode: dst shorter than ShadowUniformSize")
	}
	encodeShadowUniforms(dst[:ShadowUniformSize], u)
	return dst[:ShadowUniformSize]
}

// sanitizeShadowUniforms returns u with every value the shader and [EvaluateDirectionalShadow] read made safe: a non-finite LightViewProj or Strength disables the shadow (Strength 0), Strength is clamped to [0, 1], non-finite or negative biases and TexelSize become 0, a non-finite LightDirection becomes zero (encoded as straight down), and a PCFKernel other than [ShadowFilterPCF3x3] becomes [ShadowFilterHard]. Encoding and the CPU reference both go through it, so they agree on any input.
func sanitizeShadowUniforms(u ShadowUniforms) ShadowUniforms {
	matOK := true
	for _, f := range u.LightViewProj {
		if !finite(f) {
			matOK = false
			break
		}
	}
	if !matOK || !finite(u.Strength) {
		u.Strength = 0
	}
	u.Strength = clamp01f(u.Strength)
	u.DepthBias = max(finiteOrZero(u.DepthBias), 0)
	u.NormalBias = max(finiteOrZero(u.NormalBias), 0)
	u.TexelSize = max(finiteOrZero(u.TexelSize), 0)
	if !finite3(u.LightDirection) {
		u.LightDirection = math3d.Vec3{}
	}
	if u.PCFKernel != ShadowFilterPCF3x3 {
		u.PCFKernel = ShadowFilterHard
	}
	return u
}

func encodeShadowUniforms(out []byte, u ShadowUniforms) {
	u = sanitizeShadowUniforms(u)
	putMat4(out[0:64], &u.LightViewProj)
	toLight := u.LightDirection.Neg()
	if toLight.LenSq() > 1e-12 {
		toLight = toLight.Norm()
	} else {
		toLight = math3d.Vec3{Y: 1}
	}
	putF32(out[64:68], toLight.X)
	putF32(out[68:72], toLight.Y)
	putF32(out[72:76], toLight.Z)
	putF32(out[76:80], u.Strength)
	putF32(out[80:84], u.DepthBias)
	putF32(out[84:88], u.NormalBias)
	putF32(out[88:92], u.TexelSize)
	filterVal := float32(0.0)
	if u.PCFKernel == ShadowFilterPCF3x3 {
		filterVal = 1.0
	}
	putF32(out[92:96], filterVal)
}

func putF32(b []byte, v float32) {
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
}

func clamp01f(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ShadowDepthWGSL is the depth-only caster vertex shader used by [DirectionalShadowMap]. It transforms [MeshVertexLayout] position attributes (@location(0)) by shadow_caster.light_mvp at @group(0) @binding(0). Entry point: "vs_shadow".
const ShadowDepthWGSL = `struct ShadowCasterUniform {
	light_mvp: mat4x4<f32>,
}

@group(0) @binding(0) var<uniform> shadow_caster: ShadowCasterUniform;

struct ShadowVertexIn {
	@location(0) pos: vec3<f32>,
}

@vertex
fn vs_shadow(in: ShadowVertexIn) -> @builtin(position) vec4<f32> {
	return shadow_caster.light_mvp * vec4<f32>(in.pos, 1.0);
}
`

// shadowStructWGSL declares the uniform block shared by [ShadowWGSL] and [ShadowWGSLForGroup].
const shadowStructWGSL = `struct ShadowUniforms {
	light_view_proj: mat4x4<f32>,
	to_light: vec4<f32>,
	bias_params: vec4<f32>,
}
`

// shadowSampleFnWGSL is the sample_directional_shadow helper shared by [ShadowWGSL] and [ShadowWGSLForGroup].
const shadowSampleFnWGSL = `
fn sample_directional_shadow(world_pos: vec3<f32>, world_normal: vec3<f32>) -> f32 {
	let strength = clamp(shadow.to_light.w, 0.0, 1.0);
	if (strength <= 0.0) {
		return 1.0;
	}
	var n = vec3<f32>(0.0, 1.0, 0.0);
	let n_len = length(world_normal);
	if (n_len > 0.000001) {
		n = world_normal / n_len;
	}
	var l = vec3<f32>(0.0, 1.0, 0.0);
	let l_len = length(shadow.to_light.xyz);
	if (l_len > 0.000001) {
		l = shadow.to_light.xyz / l_len;
	}
	let ndl = max(dot(n, l), 0.0);
	let normal_bias = shadow.bias_params.y;
	let biased_pos = world_pos + n * normal_bias;
	let light_clip = shadow.light_view_proj * vec4<f32>(biased_pos, 1.0);
	let ndc = light_clip.xyz / light_clip.w;
	let uv = vec2<f32>(ndc.x * 0.5 + 0.5, 0.5 - ndc.y * 0.5);
	if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0 || ndc.z < 0.0 || ndc.z > 1.0) {
		return 1.0;
	}
	let slope_bias = shadow.bias_params.x * (1.0 + (1.0 - ndl) * 2.0);
	let depth_ref = clamp(ndc.z - slope_bias, 0.0, 1.0);
	var vis: f32 = 1.0;
	if (shadow.bias_params.w < 0.5) {
		vis = textureSampleCompareLevel(shadow_map, shadow_sampler, uv, depth_ref);
	} else {
		let texel = shadow.bias_params.z;
		var sum: f32 = 0.0;
		for (var dy: i32 = -1; dy <= 1; dy = dy + 1) {
			for (var dx: i32 = -1; dx <= 1; dx = dx + 1) {
				let off = vec2<f32>(f32(dx) * texel, f32(dy) * texel);
				sum = sum + textureSampleCompareLevel(shadow_map, shadow_sampler, uv + off, depth_ref);
			}
		}
		vis = sum / 9.0;
	}
	return mix(1.0, vis, strength);
}
`

// ShadowWGSL declares the directional shadow uniform block, depth map and comparison sampler at [ShadowGroup] (@group(3)), and the portable sample_directional_shadow(world_pos, world_normal) helper returning directional light visibility in [0, 1]. It equals ShadowWGSLForGroup(ShadowGroup). Prepend [CameraWGSL] and ShadowWGSL to a custom 3D lit shader to add directional shadows without compute shaders or backend-specific intrinsics; [LitPBRShadowWGSL] is the ready-made PBR shader built on it.
const ShadowWGSL = shadowStructWGSL + `
@group(3) @binding(0) var<uniform> shadow: ShadowUniforms;
@group(3) @binding(1) var shadow_map: texture_depth_2d;
@group(3) @binding(2) var shadow_sampler: sampler_comparison;
` + shadowSampleFnWGSL

// ShadowWGSLForGroup returns [ShadowWGSL] with its shadow bindings placed at @group(group) instead of [ShadowGroup], for custom shaders whose group 3 already holds something else.
func ShadowWGSLForGroup(group uint32) string {
	return fmt.Sprintf(`%s
@group(%[2]d) @binding(0) var<uniform> shadow: ShadowUniforms;
@group(%[2]d) @binding(1) var shadow_map: texture_depth_2d;
@group(%[2]d) @binding(2) var shadow_sampler: sampler_comparison;
%[3]s`, shadowStructWGSL, group, shadowSampleFnWGSL)
}

// ShadowMapOptions configures [NewDirectionalShadowMap].
type ShadowMapOptions struct {
	// Label prefixes GPU resources in capture tools.
	Label string
	// Resolution is the shadow map width and height in texels (power of two in [MinShadowResolution, MaxShadowResolution]). Zero defaults to [DefaultShadowResolution] (1024).
	Resolution uint32
	// PCFKernel selects the initial filtering mode ([ShadowFilterHard] or [ShadowFilterPCF3x3]); it can also be switched dynamically per frame via [DirectionalShadowMap.WriteUniforms].
	PCFKernel ShadowFilter
	// DepthBias is the hardware rasterizer constant depth bias applied during the shadow depth pass.
	DepthBias int32
	// DepthBiasSlopeScale is the hardware rasterizer slope-scaled depth bias applied during the shadow depth pass.
	DepthBiasSlopeScale float32
	// CullMode selects face culling during the shadow depth pass (gpu.CullNone, gpu.CullBack, or gpu.CullFront).
	CullMode gpu.CullMode
	// MaxCasters is the maximum number of [DirectionalShadowMap.DrawCaster] calls per shadow pass. Zero defaults to [DefaultShadowMaxCasters] (256).
	MaxCasters uint32
	// Retained keeps the offscreen shadow render target, uniform buffers, samplers, bind groups, and depth pipeline alive across GPU device loss.
	Retained bool
}

// DefaultShadowMapOptions returns recommended options for [NewDirectionalShadowMap]: 1024x1024 depth map, 3x3 PCF filtering, hardware depth bias (2, 2.0), back-face culling, and 256 caster slots.
func DefaultShadowMapOptions() ShadowMapOptions {
	return ShadowMapOptions{
		Resolution:          DefaultShadowResolution,
		PCFKernel:           ShadowFilterPCF3x3,
		DepthBias:           2,
		DepthBiasSlopeScale: 2.0,
		CullMode:            gpu.CullBack,
		MaxCasters:          DefaultShadowMaxCasters,
	}
}

func (o ShadowMapOptions) effectiveResolution() uint32 {
	if o.Resolution == 0 {
		return DefaultShadowResolution
	}
	return o.Resolution
}

func (o ShadowMapOptions) effectiveMaxCasters() uint32 {
	if o.MaxCasters == 0 {
		return DefaultShadowMaxCasters
	}
	return o.MaxCasters
}

// Validate checks that every field of o is valid across all NSGo GPU backends. Every error wraps [ErrInvalidShadow].
func (o ShadowMapOptions) Validate() error {
	if err := validateShadowResolution(o.effectiveResolution()); err != nil {
		return err
	}
	if !validShadowFilter(o.PCFKernel) {
		return fmt.Errorf("%w: invalid PCFKernel %d", ErrInvalidShadow, o.PCFKernel)
	}
	if !finite(o.DepthBiasSlopeScale) {
		return fmt.Errorf("%w: DepthBiasSlopeScale %v must be finite", ErrInvalidShadow, o.DepthBiasSlopeScale)
	}
	if o.CullMode != gpu.CullNone && o.CullMode != gpu.CullFront && o.CullMode != gpu.CullBack {
		return fmt.Errorf("%w: invalid CullMode %d", ErrInvalidShadow, o.CullMode)
	}
	return nil
}

// ShadowPassTarget is implemented by both [*gpu.Encoder] and [*gpu.Frame], allowing [DirectionalShadowMap.BeginShadowPass] to record a shadow depth pass on either headless encoders or live gpu.View frames.
type ShadowPassTarget interface {
	BeginRenderPass(desc gpu.RenderPassDesc) *gpu.RenderPass
}

// DirectionalShadowMap manages an offscreen depth-only shadow map [*gpu.RenderTarget], a depth-only caster pipeline ([ShadowDepthWGSL]), a dynamic-offset caster transform buffer, the directional [ShadowUniforms] buffer, and both nearest (hard) and linear (PCF) comparison samplers/bind groups.
//
// Create it once when the GPU device is ready (with Retained: true to survive device loss automatically), call [DirectionalShadowMap.Update] or [DirectionalShadowMap.UpdateBounds] each frame, record casters between [DirectionalShadowMap.BeginShadowPass] and pass.End(), and bind [DirectionalShadowMap.BindGroup] at [ShadowGroup] during the main lit render pass. Its resources belong to the [*core.Scope] passed to [NewDirectionalShadowMap] and are released when that scope is disposed or when [DirectionalShadowMap.Close] is called.
type DirectionalShadowMap struct {
	label       string
	resolution  uint32
	maxCasters  uint32
	casterAlign uint64

	rt            *gpu.RenderTarget
	depthPipe     *gpu.Pipeline
	casterBuf     *gpu.Buffer
	casterLayout  *gpu.BindGroupLayout
	casterBG      *gpu.BindGroup
	uniformBuf    *gpu.Buffer
	sampleLayout  *gpu.BindGroupLayout
	nearestSamp   *gpu.Sampler
	linearSamp    *gpu.Sampler
	hardBG        *gpu.BindGroup
	pcfBG         *gpu.BindGroup
	activeFilter  ShadowFilter
	uniforms      ShadowUniforms
	passDev       *gpu.Device
	casterSlot    uint32
	uniformBytes  [ShadowUniformSize]byte
	casterScratch [ShadowCasterUniformSize]byte
}

// NewDirectionalShadowMap validates opts and allocates all GPU resources for directional shadow mapping on dev, scoped to cx. Invalid options return an error wrapping [ErrInvalidShadow] before creating any GPU object; a nil or lost device returns [gpu.ErrDeviceLost].
func NewDirectionalShadowMap(cx *core.Scope, dev *gpu.Device, opts ShadowMapOptions) (*DirectionalShadowMap, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	res := opts.effectiveResolution()
	if lim := dev.Limits().MaxTextureDimension2D; lim > 0 && res > lim {
		return nil, fmt.Errorf("%w: resolution %d exceeds device MaxTextureDimension2D %d", ErrInvalidShadow, res, lim)
	}
	label := opts.Label
	if label == "" {
		label = "render3d.shadow"
	}
	maxCasters := opts.effectiveMaxCasters()
	align := uint64(dev.Limits().MinUniformBufferAlignment)
	if align < ShadowCasterUniformSize {
		align = 256
	}
	casterBufSize := uint64(maxCasters) * align
	if lim := dev.Limits().MaxBufferSize; lim > 0 && casterBufSize > lim {
		return nil, fmt.Errorf("%w: MaxCasters %d requires %d bytes, exceeding MaxBufferSize %d", ErrInvalidShadow, maxCasters, casterBufSize, lim)
	}

	sm := &DirectionalShadowMap{
		label:        label,
		resolution:   res,
		maxCasters:   maxCasters,
		casterAlign:  align,
		activeFilter: opts.PCFKernel,
	}
	ok := false
	defer func() {
		if !ok {
			_ = sm.Close()
		}
	}()

	var err error
	sm.rt, err = dev.CreateRenderTarget(cx, gpu.RenderTargetDesc{
		Label:       label + " target",
		Width:       res,
		Height:      res,
		DepthFormat: gpu.FormatDepth32Float,
		Retained:    opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	depthMod, err := gpu.CompileWGSL(ShadowDepthWGSL)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile shadow depth shader: %w", err)
	}

	sm.casterBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " casters",
		Size:     casterBufSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	sm.casterLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " caster layout",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:          0,
			Visibility:       gpu.StageVertex,
			Type:             gpu.BindingUniform,
			HasDynamicOffset: true,
		}},
	})
	if err != nil {
		return nil, err
	}

	sm.casterBG, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + " caster group",
		Layout: sm.casterLayout,
		Entries: []gpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  sm.casterBuf,
			Size:    ShadowCasterUniformSize,
		}},
	})
	if err != nil {
		return nil, err
	}

	sm.depthPipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
		Label:   label + " depth pipeline",
		Vertex:  gpu.ShaderStage{Module: depthMod, EntryPoint: "vs_shadow"},
		Layouts: []*gpu.BindGroupLayout{sm.casterLayout},
		VertexBuffers: []gpu.VertexBufferLayout{{
			Stride:   MeshVertexStride,
			StepMode: gpu.StepVertex,
			Attributes: []gpu.VertexAttribute{{
				Format:   gpu.VertexFloat32x3,
				Offset:   MeshPositionOffset,
				Location: MeshPositionLocation,
			}},
		}},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  opts.CullMode,
		},
		DepthStencil: &gpu.DepthStencilState{
			Format:              gpu.FormatDepth32Float,
			DepthWrite:          true,
			DepthCompare:        gpu.CompareLess,
			DepthBias:           opts.DepthBias,
			DepthBiasSlopeScale: opts.DepthBiasSlopeScale,
		},
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	sm.uniformBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " uniforms",
		Size:     ShadowUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	sm.nearestSamp, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + " nearest cmp",
		MinFilter:    gpu.FilterNearest,
		MagFilter:    gpu.FilterNearest,
		MipmapFilter: gpu.FilterNearest,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Compare:      gpu.CompareLessEqual,
		Retained:     opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	sm.linearSamp, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + " linear cmp",
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterNearest,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Compare:      gpu.CompareLessEqual,
		Retained:     opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	sm.sampleLayout, err = ShadowBindingLayout(cx, dev)
	if err != nil {
		return nil, err
	}

	sm.hardBG, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + " sample hard",
		Layout: sm.sampleLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: sm.uniformBuf, Size: ShadowUniformSize},
			{Binding: 1, Texture: sm.rt.Depth},
			{Binding: 2, Sampler: sm.nearestSamp},
		},
	})
	if err != nil {
		return nil, err
	}

	sm.pcfBG, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + " sample pcf",
		Layout: sm.sampleLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: sm.uniformBuf, Size: ShadowUniformSize},
			{Binding: 1, Texture: sm.rt.Depth},
			{Binding: 2, Sampler: sm.linearSamp},
		},
	})
	if err != nil {
		return nil, err
	}

	// Initialize uniforms to a sensible default so sampling works even before the first Update call.
	defCfg := DefaultDirectionalShadowConfig()
	defCfg.Resolution = res
	defCfg.PCFKernel = opts.PCFKernel
	if lvp, lvpErr := defCfg.LightViewProj(); lvpErr == nil {
		sm.WriteUniforms(dev, defCfg.Uniforms(lvp))
	}

	ok = true
	return sm, nil
}

// ShadowBindingLayout creates the bind group layout declared by [ShadowWGSL]: binding 0 = vertex/fragment uniform buffer ([ShadowUniforms]), binding 1 = fragment depth texture (texture_depth_2d), binding 2 = fragment comparison sampler (sampler_comparison). Place it at [ShadowGroup] (2) in a pipeline's Layouts (or at whichever group [ShadowWGSLForGroup] uses).
func ShadowBindingLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.shadow.sample",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingComparisonSampler},
		},
	})
}

// Resolution returns the shadow map width and height in texels, or zero for a nil receiver.
func (s *DirectionalShadowMap) Resolution() uint32 {
	if s == nil {
		return 0
	}
	return s.resolution
}

// MaxCasters returns the maximum number of [DirectionalShadowMap.DrawCaster] calls supported per shadow pass, or zero for a nil receiver.
func (s *DirectionalShadowMap) MaxCasters() uint32 {
	if s == nil {
		return 0
	}
	return s.maxCasters
}

// RenderTarget returns the underlying depth-only [*gpu.RenderTarget], or nil for a nil receiver.
func (s *DirectionalShadowMap) RenderTarget() *gpu.RenderTarget {
	if s == nil {
		return nil
	}
	return s.rt
}

// DepthTexture returns the depth [*gpu.Texture] of the shadow map, or nil for a nil receiver.
func (s *DirectionalShadowMap) DepthTexture() *gpu.Texture {
	if s == nil || s.rt == nil {
		return nil
	}
	return s.rt.Depth
}

// DepthPipeline returns the depth-only shadow caster [*gpu.Pipeline], or nil for a nil receiver.
func (s *DirectionalShadowMap) DepthPipeline() *gpu.Pipeline {
	if s == nil {
		return nil
	}
	return s.depthPipe
}

// UniformBuffer returns the [*gpu.Buffer] holding [ShadowUniforms], or nil for a nil receiver.
func (s *DirectionalShadowMap) UniformBuffer() *gpu.Buffer {
	if s == nil {
		return nil
	}
	return s.uniformBuf
}

// Layout returns the bind group layout for sampling the shadow map (to place at [ShadowGroup] in lit pipelines), or nil for a nil receiver.
func (s *DirectionalShadowMap) Layout() *gpu.BindGroupLayout {
	if s == nil {
		return nil
	}
	return s.sampleLayout
}

// BindGroup returns the bind group for sampling the shadow map at [ShadowGroup], automatically selecting the nearest comparison sampler for [ShadowFilterHard] and the linear comparison sampler for [ShadowFilterPCF3x3].
func (s *DirectionalShadowMap) BindGroup() *gpu.BindGroup {
	if s == nil {
		return nil
	}
	if s.activeFilter == ShadowFilterPCF3x3 {
		return s.pcfBG
	}
	return s.hardBG
}

// Uniforms returns the most recently written [ShadowUniforms].
func (s *DirectionalShadowMap) Uniforms() ShadowUniforms {
	if s == nil {
		return ShadowUniforms{}
	}
	return s.uniforms
}

// ByteSize returns the GPU memory held by the shadow map depth texture (4 bytes per texel for Depth32Float) and uniform buffers, for [RenderStats] and [Budget] accounting.
func (s *DirectionalShadowMap) ByteSize() uint64 {
	if s == nil {
		return 0
	}
	var total uint64
	if s.resolution > 0 {
		total += uint64(s.resolution) * uint64(s.resolution) * 4
	}
	if s.casterBuf != nil {
		total += s.casterBuf.Size()
	}
	if s.uniformBuf != nil {
		total += s.uniformBuf.Size()
	}
	return total
}

// Valid reports whether all GPU resources owned by s are open and belong to a live device.
func (s *DirectionalShadowMap) Valid() bool {
	if s == nil {
		return false
	}
	return s.rt != nil && s.rt.Valid() &&
		s.depthPipe != nil && s.depthPipe.Valid() &&
		s.casterBuf != nil && s.casterBuf.Valid() &&
		s.casterBG != nil && s.casterBG.Valid() &&
		s.uniformBuf != nil && s.uniformBuf.Valid() &&
		s.hardBG != nil && s.hardBG.Valid() &&
		s.pcfBG != nil && s.pcfBG.Valid()
}

// WriteUniforms encodes u into the shadow uniform buffer on dev without heap allocation and selects the matching hard or PCF comparison sampler bind group. Values the shader cannot use are made safe first: a NaN or infinite matrix or Strength disables the shadow, non-finite biases become 0, a missing TexelSize becomes 1/[DirectionalShadowMap.Resolution], and an unknown PCFKernel falls back to [ShadowFilterHard]. [DirectionalShadowMap.Uniforms] returns the values as written.
func (s *DirectionalShadowMap) WriteUniforms(dev *gpu.Device, u ShadowUniforms) {
	if s == nil || dev == nil || !s.Valid() {
		return
	}
	u = sanitizeShadowUniforms(u)
	if u.TexelSize <= 0 && s.resolution > 0 {
		u.TexelSize = 1.0 / float32(s.resolution)
	}
	s.uniforms = u
	s.activeFilter = u.PCFKernel
	encodeShadowUniforms(s.uniformBytes[:], u)
	dev.WriteBuffer(s.uniformBuf, 0, s.uniformBytes[:])
}

// Update computes the orthographic light matrix from cfg (using s.Resolution() if cfg.Resolution is zero), uploads the resulting [ShadowUniforms] on dev, and returns them.
func (s *DirectionalShadowMap) Update(dev *gpu.Device, cfg DirectionalShadowConfig) (ShadowUniforms, error) {
	if s == nil || !s.Valid() {
		return ShadowUniforms{}, fmt.Errorf("%w: shadow map is not valid", ErrInvalidShadow)
	}
	if dev == nil {
		return ShadowUniforms{}, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if cfg.Resolution == 0 {
		cfg.Resolution = s.resolution
	}
	lvp, err := cfg.LightViewProj()
	if err != nil {
		return ShadowUniforms{}, err
	}
	u := cfg.Uniforms(lvp)
	u.TexelSize = 1.0 / float32(s.resolution)
	s.WriteUniforms(dev, u)
	return u, nil
}

// UpdateBounds fits the orthographic light frustum to sceneBounds via [DirectionalShadowConfig.FitBounds] (using s.Resolution() if cfg.Resolution is zero), uploads the resulting [ShadowUniforms] on dev, and returns them.
func (s *DirectionalShadowMap) UpdateBounds(dev *gpu.Device, cfg DirectionalShadowConfig, sceneBounds math3d.AABB) (ShadowUniforms, error) {
	if s == nil || !s.Valid() {
		return ShadowUniforms{}, fmt.Errorf("%w: shadow map is not valid", ErrInvalidShadow)
	}
	if dev == nil {
		return ShadowUniforms{}, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if cfg.Resolution == 0 {
		cfg.Resolution = s.resolution
	}
	lvp, err := cfg.FitBounds(cfg.Direction, sceneBounds)
	if err != nil {
		return ShadowUniforms{}, err
	}
	u := cfg.Uniforms(lvp)
	u.TexelSize = 1.0 / float32(s.resolution)
	s.WriteUniforms(dev, u)
	return u, nil
}

// BeginShadowPass resets the per-pass caster slot cursor, opens a depth-only render pass clearing the shadow map depth to 1.0, and binds the shadow depth pipeline. Pass the live [*gpu.Device] and either a [*gpu.Encoder] or [*gpu.Frame] as target. Returns nil when s, dev, or target is nil or s is invalid (after [DirectionalShadowMap.Close], or while the device is lost for a map that is not Retained), so check the result before drawing casters and calling End.
func (s *DirectionalShadowMap) BeginShadowPass(dev *gpu.Device, target ShadowPassTarget) *gpu.RenderPass {
	if s == nil || dev == nil || target == nil || !s.Valid() {
		return nil
	}
	s.passDev = dev
	s.casterSlot = 0
	pass := target.BeginRenderPass(gpu.RenderPassDesc{
		Label: s.label + " pass",
		DepthStencil: &gpu.DepthStencilAttachment{
			Texture:      s.rt.Depth,
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   1.0,
		},
	})
	if pass == nil {
		return nil
	}
	pass.SetPipeline(s.depthPipe)
	return pass
}

// DrawCaster records one shadow-casting [GPUMesh] with world transform model inside the shadow pass started by [DirectionalShadowMap.BeginShadowPass]. Each call writes lightViewProj * model into its own dynamic-offset uniform slot so multiple casters can be drawn in one pass without overwriting earlier draws. Returns false if any argument is invalid or [DirectionalShadowMap.MaxCasters] slots have been exhausted.
func (s *DirectionalShadowMap) DrawCaster(pass *gpu.RenderPass, mesh *GPUMesh, model math3d.Mat4) bool {
	if s == nil || pass == nil || s.passDev == nil || !s.Valid() || !mesh.Valid() {
		return false
	}
	if s.casterSlot >= s.maxCasters {
		return false
	}
	offset := uint64(s.casterSlot) * s.casterAlign
	s.casterSlot++

	mvp := s.uniforms.LightViewProj.Mul(model)
	putMat4(s.casterScratch[:], &mvp)
	s.passDev.WriteBuffer(s.casterBuf, offset, s.casterScratch[:])
	pass.SetBindGroup(0, s.casterBG, uint32(offset))
	mesh.Draw(pass)
	return true
}

// Close releases all GPU resources owned by s now rather than waiting for its scope to be disposed. Safe to call on a nil receiver or more than once.
func (s *DirectionalShadowMap) Close() error {
	if s == nil {
		return nil
	}
	s.passDev = nil
	var err error
	closeOne := func(c interface{ Close() error }) {
		if c != nil {
			err = errors.Join(err, c.Close())
		}
	}
	if s.pcfBG != nil {
		closeOne(s.pcfBG)
	}
	if s.hardBG != nil {
		closeOne(s.hardBG)
	}
	if s.sampleLayout != nil {
		closeOne(s.sampleLayout)
	}
	if s.linearSamp != nil {
		closeOne(s.linearSamp)
	}
	if s.nearestSamp != nil {
		closeOne(s.nearestSamp)
	}
	if s.uniformBuf != nil {
		closeOne(s.uniformBuf)
	}
	if s.depthPipe != nil {
		closeOne(s.depthPipe)
	}
	if s.casterBG != nil {
		closeOne(s.casterBG)
	}
	if s.casterLayout != nil {
		closeOne(s.casterLayout)
	}
	if s.casterBuf != nil {
		closeOne(s.casterBuf)
	}
	if s.rt != nil {
		closeOne(s.rt)
	}
	return err
}

// LitPBRShadowWGSL is [LitPBRWGSL] with a directional shadow: the same vertex stage, bind groups [CameraGroup], [LightGroup] and [PBRDrawGroup], and lighting, plus [ShadowWGSL] at [ShadowGroup]. The fragment stage multiplies the first directional light of the [LightRig] (lights.directional[0]) by sample_directional_shadow before encoding to sRGB; every other light, the ambient term and emission are unshadowed. [ShadePointShadowed] is its CPU reference. Use [NewLitPBRShadowPipeline] or [LitPBRPipelineOptions].ShadowLayout to create a pipeline from it.
const LitPBRShadowWGSL = litPBRPreludeWGSL + ShadowWGSL + litPBRShadowFragmentHeadWGSL + `	return vec4<f32>(linear_to_srgb(lit), pbr_draw.base_color.a);
}
`

// litPBRShadowLinearWGSL is [LitPBRShadowWGSL] writing the lit linear color without sRGB encoding, compiled by [NewLitPBRPipelineWithOptions] when [LitPBRPipelineOptions].LinearOutput and ShadowLayout are set.
const litPBRShadowLinearWGSL = litPBRPreludeWGSL + ShadowWGSL + litPBRShadowFragmentHeadWGSL + `	return vec4<f32>(lit, pbr_draw.base_color.a);
}
`

// litPBRShadowFragmentHeadWGSL opens fs_main for the shadowed lit PBR shaders and computes the lit linear color lit with directional light 0 attenuated by the shadow map.
const litPBRShadowFragmentHeadWGSL = `
@fragment
fn fs_main(in: PBRVertexOut) -> @location(0) vec4<f32> {
	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let shadow_vis = sample_directional_shadow(in.world_pos, in.world_normal);
	let lit = shade_pbr_shadowed(
		in.world_pos,
		in.world_normal,
		view_dir,
		pbr_draw.base_color.rgb,
		pbr_draw.pbr_params.x,
		pbr_draw.pbr_params.y,
		shadow_vis,
	) + pbr_draw.emissive.rgb;
`

// NewLitPBRShadowPipeline compiles [LitPBRShadowWGSL] and creates a pipeline configured like [NewLitPBRPipeline] with shadowLayout ([DirectionalShadowMap.Layout]) added at [ShadowGroup].
func NewLitPBRShadowPipeline(cx *core.Scope, dev *gpu.Device, camLayout, lightLayout, drawLayout, shadowLayout *gpu.BindGroupLayout, depthFormat gpu.TextureFormat) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if shadowLayout == nil {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: NewLitPBRShadowPipeline requires a shadow bind group layout"}
	}
	return NewLitPBRPipelineWithOptions(cx, dev, camLayout, lightLayout, drawLayout, LitPBRPipelineOptions{ShadowLayout: shadowLayout, DepthFormat: depthFormat})
}

// ShadowDepthGrid is a CPU-readable row-major [0, 1] depth map (top-to-bottom, index y*Width + x) matching the layout returned by [gpu.Device.ReadTexture] on a [gpu.FormatDepth32Float] shadow map.
type ShadowDepthGrid struct {
	Width  int
	Height int
	Depths []float32
}

// ShadowDepthGridFromBytes decodes raw little-endian float32 depth readback bytes into a [ShadowDepthGrid]. Every error wraps [ErrInvalidShadow].
func ShadowDepthGridFromBytes(width, height int, raw []byte) (ShadowDepthGrid, error) {
	if width <= 0 || height <= 0 {
		return ShadowDepthGrid{}, fmt.Errorf("%w: depth grid dimensions %dx%d must be positive", ErrInvalidShadow, width, height)
	}
	need := width * height * 4
	if len(raw) < need {
		return ShadowDepthGrid{}, fmt.Errorf("%w: depth grid bytes %d < %d for %dx%d", ErrInvalidShadow, len(raw), need, width, height)
	}
	depths := make([]float32, width*height)
	for i := range depths {
		depths[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return ShadowDepthGrid{Width: width, Height: height, Depths: depths}, nil
}

// EvaluateDirectionalShadow is the pure-Go CPU reference matching sample_directional_shadow in [ShadowWGSL] for the uniforms [AppendShadowUniforms] encodes from u (including its handling of non-finite values and unknown filters). It returns directional light visibility in [0, 1]: 1.0 when unoccluded, outside the light frustum, or when u.Strength <= 0, and (1 - u.Strength) when fully occluded.
func EvaluateDirectionalShadow(worldPos, worldNormal math3d.Vec3, u ShadowUniforms, grid ShadowDepthGrid) float32 {
	u = sanitizeShadowUniforms(u)
	strength := u.Strength
	if strength <= 0 || grid.Width <= 0 || grid.Height <= 0 || len(grid.Depths) < grid.Width*grid.Height {
		return 1.0
	}

	n := math3d.Vec3{Y: 1}
	if nLen := worldNormal.Len(); nLen > 1e-6 {
		n = worldNormal.Scale(1.0 / nLen)
	}
	toLight := u.LightDirection.Neg()
	l := math3d.Vec3{Y: 1}
	if lLen := toLight.Len(); lLen > 1e-6 {
		l = toLight.Scale(1.0 / lLen)
	}

	ndl := max(n.Dot(l), 0)
	normalBias := u.NormalBias
	biasedPos := worldPos.Add(n.Scale(normalBias))
	lightClip := u.LightViewProj.MulVec4(math3d.Vec4{X: biasedPos.X, Y: biasedPos.Y, Z: biasedPos.Z, W: 1})
	if lightClip.W == 0 {
		return 1.0
	}
	invW := 1.0 / lightClip.W
	ndcX := lightClip.X * invW
	ndcY := lightClip.Y * invW
	ndcZ := lightClip.Z * invW

	uCoord := ndcX*0.5 + 0.5
	vCoord := 0.5 - ndcY*0.5
	if uCoord < 0 || uCoord > 1 || vCoord < 0 || vCoord > 1 || ndcZ < 0 || ndcZ > 1 {
		return 1.0
	}

	depthBias := u.DepthBias
	slopeBias := depthBias * (1.0 + (1.0-ndl)*2.0)
	depthRef := clamp01f(ndcZ - slopeBias)

	var vis float32
	if u.PCFKernel != ShadowFilterPCF3x3 {
		vis = sampleShadowGridCompareNearest(grid, uCoord, vCoord, depthRef)
	} else {
		texel := u.TexelSize
		if texel <= 0 {
			texel = 1.0 / float32(grid.Width)
		}
		var sum float32
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				sum += sampleShadowGridCompareLinear(grid, uCoord+float32(dx)*texel, vCoord+float32(dy)*texel, depthRef)
			}
		}
		vis = sum / 9.0
	}
	return 1.0 + (vis-1.0)*strength
}

func sampleShadowGridTexel(grid ShadowDepthGrid, x, y int, depthRef float32) float32 {
	x = min(max(x, 0), grid.Width-1)
	y = min(max(y, 0), grid.Height-1)
	if depthRef <= grid.Depths[y*grid.Width+x] {
		return 1.0
	}
	return 0.0
}

func sampleShadowGridCompareNearest(grid ShadowDepthGrid, u, v, depthRef float32) float32 {
	u = clamp01f(u)
	v = clamp01f(v)
	ix := int(math.Floor(float64(u * float32(grid.Width))))
	iy := int(math.Floor(float64(v * float32(grid.Height))))
	return sampleShadowGridTexel(grid, ix, iy, depthRef)
}

func sampleShadowGridCompareLinear(grid ShadowDepthGrid, u, v, depthRef float32) float32 {
	u = clamp01f(u)
	v = clamp01f(v)
	fx := u*float32(grid.Width) - 0.5
	fy := v*float32(grid.Height) - 0.5
	x0 := int(math.Floor(float64(fx)))
	y0 := int(math.Floor(float64(fy)))
	tx := fx - float32(x0)
	ty := fy - float32(y0)
	c00 := sampleShadowGridTexel(grid, x0, y0, depthRef)
	c10 := sampleShadowGridTexel(grid, x0+1, y0, depthRef)
	c01 := sampleShadowGridTexel(grid, x0, y0+1, depthRef)
	c11 := sampleShadowGridTexel(grid, x0+1, y0+1, depthRef)
	top := c00*(1-tx) + c10*tx
	bot := c01*(1-tx) + c11*tx
	return top*(1-ty) + bot*ty
}
