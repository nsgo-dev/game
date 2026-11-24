package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Directional shadow mapping constants and defaults (#117).
const (
	// ShadowGroup is the bind group index reserved by [ShadowWGSL] and [ShadowLitWGSL] for directional shadow data (uniform buffer at binding 0, depth texture at binding 1, comparison sampler at binding 2), leaving [CameraGroup] (0) for the camera and group 1 for materials or per-object data.
	ShadowGroup = 2

	// ShadowUniformSize is the byte size of the [ShadowUniforms] block written by [AppendShadowUniforms] and declared by [ShadowWGSL]: one mat4x4<f32> plus two vec4<f32> vectors (96 bytes).
	ShadowUniformSize = 64 + 16 + 16

	// ShadowCasterUniformSize is the byte size of one caster's light-space MVP matrix in [ShadowDepthWGSL].
	ShadowCasterUniformSize = 64

	// ShadowLitObjectUniformSize is the byte size of [ShadowLitObjectUniform] declared in [ShadowLitWGSL]: two mat4x4<f32> matrices plus three vec4<f32> vectors (176 bytes).
	ShadowLitObjectUniformSize = 2*64 + 3*16

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
	// Direction is the world-space direction light rays travel (from the sun toward the scene). It need not be unit length, but must be finite and non-zero.
	Direction math3d.Vec3
	// Target is the world-space center of the orthographic shadow volume used by [DirectionalShadowConfig.LightViewProj] (ignored by [DirectionalShadowConfig.FitBounds]).
	Target math3d.Vec3
	// OrthoSize is the full width and height (in world units) of the square orthographic frustum used by [DirectionalShadowConfig.LightViewProj]. When zero, defaults to 20.
	OrthoSize float32
	// Near and Far are the depth clip distances along the light axis used by [DirectionalShadowConfig.LightViewProj]. When both are zero, Near defaults to 0.1 and Far defaults to max(OrthoSize*2, 100).
	Near float32
	Far  float32
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
	// TexelSnap snaps the orthographic light-space XY center to integer shadow-map texel increments to prevent shadow swimming as the target moves.
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
	if !finite(c.Near) || !finite(c.Far) {
		return fmt.Errorf("%w: Near/Far (%v, %v) must be finite", ErrInvalidShadow, c.Near, c.Far)
	}
	if (c.Near != 0 || c.Far != 0) && c.Far <= c.Near {
		return fmt.Errorf("%w: Far (%g) must be greater than Near (%g)", ErrInvalidShadow, c.Far, c.Near)
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

// LightViewProj computes the orthographic light-space view-projection matrix (with [0, 1] NDC depth) centered around c.Target with width and height c.OrthoSize. When c.TexelSnap is true, the light-space XY center is snapped to exact texel increments of c.Resolution to eliminate shadow swimming.
func (c DirectionalShadowConfig) LightViewProj() (math3d.Mat4, error) {
	if err := c.Validate(); err != nil {
		return math3d.Mat4{}, err
	}
	dir := c.Direction.Norm()
	size := c.OrthoSize
	if size == 0 {
		size = 20
	}
	near, far := c.Near, c.Far
	if near == 0 && far == 0 {
		near = 0.1
		far = max(size*2, 100)
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
	depthHalf := (far - near) * 0.5
	// Map target's light-space Z to the midpoint of [near, far].
	zNear := -centerLS.Z - depthHalf
	zFar := -centerLS.Z + depthHalf
	proj := math3d.OrthoZO(cx-half, cx+half, cy-half, cy+half, zNear, zFar)
	return proj.Mul(view), nil
}

// FitBounds computes an orthographic light-space view-projection matrix (with [0, 1] NDC depth) tightly framing sceneBounds along lightDir (falling back to c.Direction when lightDir is zero). When c.TexelSnap is true, the light-space XY center is snapped to exact shadow-map texel increments while expanding the frustum margin by one texel per side so every corner of sceneBounds remains inside [0, 1]^2.
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

	spanX := ls.Max.X - ls.Min.X
	spanY := ls.Max.Y - ls.Min.Y
	rawExtent := max(spanX, spanY)
	if rawExtent < 1e-3 {
		rawExtent = 1e-3
	}

	// Keep a small border margin so casters on the AABB boundary do not clip or touch the clamped shadow edge; under texel snapping, guarantee at least one full texel per side so +/-0.5 texel rounding still encloses every corner.
	var extent float32
	if c.TexelSnap {
		extent = rawExtent * (1.0 + 2.0/float32(res))
		if extent < rawExtent*1.02 {
			extent = rawExtent * 1.02
		}
	} else {
		extent = rawExtent * 1.02
	}

	cx := (ls.Min.X + ls.Max.X) * 0.5
	cy := (ls.Min.Y + ls.Max.Y) * 0.5
	if c.TexelSnap {
		texel := extent / float32(res)
		cx = snapToTexel(cx, texel)
		cy = snapToTexel(cy, texel)
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

func encodeShadowUniforms(out []byte, u ShadowUniforms) {
	putMat4(out[0:64], u.LightViewProj)
	toLight := u.LightDirection.Neg()
	if toLight.LenSq() > 1e-12 {
		toLight = toLight.Norm()
	} else {
		toLight = math3d.Vec3{Y: 1}
	}
	putF32(out[64:68], toLight.X)
	putF32(out[68:72], toLight.Y)
	putF32(out[72:76], toLight.Z)
	putF32(out[76:80], clamp01f(u.Strength))
	putF32(out[80:84], max(u.DepthBias, 0))
	putF32(out[84:88], max(u.NormalBias, 0))
	putF32(out[88:92], max(u.TexelSize, 0))
	filterVal := float32(0.0)
	if u.PCFKernel == ShadowFilterPCF3x3 {
		filterVal = 1.0
	}
	putF32(out[92:96], filterVal)
}

func putF32(b []byte, v float32) {
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
}

func putMat4(b []byte, m math3d.Mat4) {
	for i, f := range m {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
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

const shadowWGSLTemplate = `struct ShadowUniforms {
	light_view_proj: mat4x4<f32>,
	to_light: vec4<f32>,
	bias_params: vec4<f32>,
}

@group(__SHADOW_GROUP__) @binding(0) var<uniform> shadow: ShadowUniforms;
@group(__SHADOW_GROUP__) @binding(1) var shadow_map: texture_depth_2d;
@group(__SHADOW_GROUP__) @binding(2) var shadow_sampler: sampler_comparison;

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

// ShadowWGSL declares the directional shadow uniform block, depth map, comparison sampler at [ShadowGroup] (@group(2)), and the portable sample_directional_shadow(world_pos, world_normal) helper returning directional light visibility in [0, 1]. Prepend [CameraWGSL] and ShadowWGSL to any custom 3D lit shader to add directional shadows without compute shaders or backend-specific intrinsics.
const ShadowWGSL = `struct ShadowUniforms {
	light_view_proj: mat4x4<f32>,
	to_light: vec4<f32>,
	bias_params: vec4<f32>,
}

@group(2) @binding(0) var<uniform> shadow: ShadowUniforms;
@group(2) @binding(1) var shadow_map: texture_depth_2d;
@group(2) @binding(2) var shadow_sampler: sampler_comparison;

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

// ShadowWGSLForGroup returns [ShadowWGSL] with its shadow bindings placed at @group(group) instead of [ShadowGroup] (2).
func ShadowWGSLForGroup(group uint32) string {
	return strings.ReplaceAll(shadowWGSLTemplate, "__SHADOW_GROUP__", fmt.Sprintf("%d", group))
}

// ShadowLitWGSL is a complete portable forward-lit directional-shadow WGSL shader combining [CameraWGSL] at @group(0), a [ShadowLitObjectUniform] at @group(1) @binding(0), and [ShadowWGSL] at @group(2). It accepts [MeshVertexLayout] vertices and shades Lambertian directional diffuse light attenuated by sample_directional_shadow plus ambient light. Entry points: "vs_main" and "fs_main".
const ShadowLitWGSL = CameraWGSL + "\n" + ShadowWGSL + `
struct ShadowLitObject {
	model: mat4x4<f32>,
	normal_mat: mat4x4<f32>,
	base_color: vec4<f32>,
	light_color: vec4<f32>,
	ambient_color: vec4<f32>,
}

@group(1) @binding(0) var<uniform> obj: ShadowLitObject;

struct ShadowLitVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct ShadowLitVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
}

@vertex
fn vs_main(in: ShadowLitVertexIn) -> ShadowLitVertexOut {
	var out: ShadowLitVertexOut;
	let world = obj.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world;
	out.world_pos = world.xyz;
	out.world_normal = (obj.normal_mat * vec4<f32>(in.normal, 0.0)).xyz;
	return out;
}

@fragment
fn fs_main(in: ShadowLitVertexOut) -> @location(0) vec4<f32> {
	var n = vec3<f32>(0.0, 1.0, 0.0);
	let n_len = length(in.world_normal);
	if (n_len > 0.000001) {
		n = in.world_normal / n_len;
	}
	var l = vec3<f32>(0.0, 1.0, 0.0);
	let l_len = length(shadow.to_light.xyz);
	if (l_len > 0.000001) {
		l = shadow.to_light.xyz / l_len;
	}
	let ndl = max(dot(n, l), 0.0);
	let vis = sample_directional_shadow(in.world_pos, n);
	let lit = obj.base_color.rgb * (obj.ambient_color.rgb + obj.light_color.rgb * (ndl * vis));
	return vec4<f32>(clamp(lit, vec3<f32>(0.0), vec3<f32>(1.0)), obj.base_color.a);
}
`

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

// WriteUniforms encodes u into the shadow uniform buffer on dev without heap allocation and selects the matching hard or PCF comparison sampler bind group.
func (s *DirectionalShadowMap) WriteUniforms(dev *gpu.Device, u ShadowUniforms) {
	if s == nil || dev == nil || !s.Valid() {
		return
	}
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

// BeginShadowPass resets the per-pass caster slot cursor, opens a depth-only render pass clearing the shadow map depth to 1.0, and binds the shadow depth pipeline. Pass the live [*gpu.Device] and either a [*gpu.Encoder] or [*gpu.Frame] as target. Returns nil when s, dev, or target is nil or invalid.
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
	putMat4(s.casterScratch[:], mvp)
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

// ShadowLitObjectUniform holds per-object transform and shading parameters for [ShadowLitWGSL] ([ShadowLitObjectUniformSize] = 176 bytes).
type ShadowLitObjectUniform struct {
	Model        math3d.Mat4
	NormalMatrix math3d.Mat4
	BaseColor    math3d.Vec4
	LightColor   math3d.Vec3
	AmbientColor math3d.Vec3
}

// NewShadowLitObjectUniform constructs a [ShadowLitObjectUniform] from model, computing its inverse-transpose normal matrix via [math3d.NormalMatrix] (falling back to identity for a singular model).
func NewShadowLitObjectUniform(model math3d.Mat4, baseColor math3d.Vec4, lightColor, ambientColor math3d.Vec3) ShadowLitObjectUniform {
	nMat := math3d.Mat4Identity()
	if nm, ok := math3d.NormalMatrix(model); ok {
		nMat = math3d.Mat4{
			nm[0], nm[1], nm[2], 0,
			nm[3], nm[4], nm[5], 0,
			nm[6], nm[7], nm[8], 0,
			0, 0, 0, 1,
		}
	}
	return ShadowLitObjectUniform{
		Model:        model,
		NormalMatrix: nMat,
		BaseColor:    baseColor,
		LightColor:   lightColor,
		AmbientColor: ambientColor,
	}
}

// AppendShadowLitObjectUniform appends u to dst in the little-endian layout declared by [ShadowLitWGSL] ([ShadowLitObjectUniformSize] bytes) and returns the extended slice. It allocates only when dst lacks capacity.
func AppendShadowLitObjectUniform(dst []byte, u ShadowLitObjectUniform) []byte {
	start := len(dst)
	dst = grow(dst, ShadowLitObjectUniformSize)
	out := dst[start:]
	putMat4(out[0:64], u.Model)
	putMat4(out[64:128], u.NormalMatrix)
	putF32(out[128:132], u.BaseColor.X)
	putF32(out[132:136], u.BaseColor.Y)
	putF32(out[136:140], u.BaseColor.Z)
	putF32(out[140:144], u.BaseColor.W)
	putF32(out[144:148], u.LightColor.X)
	putF32(out[148:152], u.LightColor.Y)
	putF32(out[152:156], u.LightColor.Z)
	putF32(out[156:160], 0)
	putF32(out[160:164], u.AmbientColor.X)
	putF32(out[164:168], u.AmbientColor.Y)
	putF32(out[168:172], u.AmbientColor.Z)
	putF32(out[172:176], 0)
	return dst
}

// ShadowLitObjectLayout creates the @group(1) bind group layout expected by [ShadowLitWGSL] (binding 0 = vertex+fragment uniform buffer for [ShadowLitObjectUniform]). Set dynamicOffset to true to share one aligned uniform buffer across multiple lit meshes.
func ShadowLitObjectLayout(cx *core.Scope, dev *gpu.Device, dynamicOffset bool) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.shadow.lit.object",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:          0,
			Visibility:       gpu.StageVertex | gpu.StageFragment,
			Type:             gpu.BindingUniform,
			HasDynamicOffset: dynamicOffset,
		}},
	})
}

// ShadowLitPipelineOptions configures [NewShadowLitPipeline].
type ShadowLitPipelineOptions struct {
	Label        string
	CameraLayout *gpu.BindGroupLayout // @group(0), e.g. CameraBuffer.Layout()
	ObjectLayout *gpu.BindGroupLayout // @group(1), e.g. ShadowLitObjectLayout(...)
	ShadowLayout *gpu.BindGroupLayout // @group(2), e.g. DirectionalShadowMap.Layout()
	ColorFormat  gpu.TextureFormat    // FormatUndefined defaults to dev.SurfaceFormat()
	DepthFormat  gpu.TextureFormat    // FormatUndefined disables depth testing
	CullMode     gpu.CullMode         // CullNone, CullFront, or CullBack
	SampleCount  int
	Retained     bool
}

// NewShadowLitPipeline compiles [ShadowLitWGSL] and creates a forward-lit shadowed [*gpu.Pipeline] for [MeshVertexLayout] meshes, scoped to cx.
func NewShadowLitPipeline(cx *core.Scope, dev *gpu.Device, opts ShadowLitPipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if opts.CameraLayout == nil || opts.ObjectLayout == nil || opts.ShadowLayout == nil {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: ShadowLitPipelineOptions requires CameraLayout, ObjectLayout, and ShadowLayout"}
	}
	mod, err := gpu.CompileWGSL(ShadowLitWGSL)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile shadow lit shader: %w", err)
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	if colorFmt == gpu.FormatUndefined {
		colorFmt = gpu.FormatRGBA8Unorm
	}
	label := opts.Label
	if label == "" {
		label = "render3d.shadow.lit"
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{opts.CameraLayout, opts.ObjectLayout, opts.ShadowLayout},
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  opts.CullMode,
		},
		Targets:     []gpu.ColorTargetState{{Format: colorFmt, WriteMask: gpu.ColorWriteAll}},
		SampleCount: opts.SampleCount,
		Retained:    opts.Retained,
	}
	if opts.DepthFormat != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:       opts.DepthFormat,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		}
	}
	return dev.CreatePipeline(cx, desc)
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

// EvaluateDirectionalShadow is the pure-Go CPU reference matching sample_directional_shadow in [ShadowWGSL]. It returns directional light visibility in [0, 1]: 1.0 when unoccluded, outside the light frustum, or when u.Strength <= 0, and (1 - u.Strength) when fully occluded.
func EvaluateDirectionalShadow(worldPos, worldNormal math3d.Vec3, u ShadowUniforms, grid ShadowDepthGrid) float32 {
	strength := clamp01f(u.Strength)
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
	normalBias := max(u.NormalBias, 0)
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

	depthBias := max(u.DepthBias, 0)
	slopeBias := depthBias * (1.0 + (1.0-ndl)*2.0)
	depthRef := clamp01f(ndcZ - slopeBias)

	var vis float32
	if u.PCFKernel == ShadowFilterHard {
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
