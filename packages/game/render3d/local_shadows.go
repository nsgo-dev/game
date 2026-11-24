package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ErrInvalidLocalShadow is wrapped by every validation error returned for spot
// or point light shadow maps and light configurations.
var ErrInvalidLocalShadow = errors.New("render3d: invalid local shadow")

// LocalShadowGroup is the bind group index used by [SpotShadowWGSL],
// [PointShadowWGSL], [NewSpotShadowLitPipeline], and [NewPointShadowLitPipeline]
// (paired with [CameraGroup] at slot 0). Callers building custom pipelines may
// place [SpotShadowMap.Layout] or [PointShadowMap.Layout] at any group index.
const LocalShadowGroup = 1

// LocalShadowFilter selects how a local shadow map samples its depth texture.
type LocalShadowFilter uint8

const (
	// LocalShadowFilterHard performs a single nearest-neighbor depth comparison tap.
	LocalShadowFilterHard LocalShadowFilter = iota
	// LocalShadowFilterPCF3x3 performs a 3x3 percentage-closer filter using
	// linear comparison taps clamped inside the shadow map or cube face tile.
	LocalShadowFilterPCF3x3
)

// String returns the canonical filter name.
func (f LocalShadowFilter) String() string {
	switch f {
	case LocalShadowFilterHard:
		return "hard"
	case LocalShadowFilterPCF3x3:
		return "pcf3x3"
	default:
		return fmt.Sprintf("LocalShadowFilter(%d)", f)
	}
}

const (
	// PointShadowFaceCount is the number of cube faces rendered for a point light.
	PointShadowFaceCount = 6
	// PointShadowAtlasCols is the horizontal tile count of the 3x2 point shadow atlas.
	PointShadowAtlasCols = 3
	// PointShadowAtlasRows is the vertical tile count of the 3x2 point shadow atlas.
	PointShadowAtlasRows = 2

	// SpotShadowUniformSize is the byte size of [SpotShadowUniform.Encode] and
	// the WGSL SpotShadow uniform block (144 bytes, 16-byte aligned).
	SpotShadowUniformSize = 64 + 5*16

	// PointShadowUniformSize is the byte size of [PointShadowUniform.Encode] and
	// the WGSL PointShadow uniform block (64 bytes, 16-byte aligned).
	PointShadowUniformSize = 4 * 16

	localShadowCasterSlotStride = 256 // WebGPU / Metal / D3D11 MinUniformBufferAlignment
	localShadowCasterDataSize   = 128 // light_view_proj (64) + model (64)

	defaultSpotShadowResolution  = 1024
	defaultPointShadowResolution = 512
	defaultLocalShadowDepthBias  = 0.0015
	defaultLocalShadowNormalBias = 0.02
	defaultSpotMaxCasters        = 256
	defaultPointMaxCasters       = 1024
)

// localShadowDepthWGSL renders shadow caster geometry into a Depth32Float
// shadow map or atlas tile without a fragment stage.
const localShadowDepthWGSL = `struct LocalShadowCaster {
	light_view_proj: mat4x4<f32>,
	model: mat4x4<f32>,
}

@group(0) @binding(0) var<uniform> caster: LocalShadowCaster;

struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

@vertex
fn vs_local_shadow_depth(in: VertexIn) -> @builtin(position) vec4<f32> {
	let world = caster.model * vec4<f32>(in.pos, 1.0);
	return caster.light_view_proj * world;
}
`

// SpotShadowWGSL declares the spot-light shadow uniform block, depth map, and
// comparison sampler at @group(1) bindings 0..2 together with portable WGSL
// helpers sample_spot_shadow and evaluate_spot_lighting.
//
// Prepend [CameraWGSL] and SpotShadowWGSL to a 3D fragment shader. The shader
// avoids exp/exp2/log2 and uses textureSampleCompareLevel so it runs identically
// across Metal, Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and
// NSGo's software GPU renderer.
const SpotShadowWGSL = `struct SpotShadow {
	view_proj: mat4x4<f32>,
	position_range: vec4<f32>,
	direction_inner: vec4<f32>,
	color_outer: vec4<f32>,
	params: vec4<f32>,
	ambient_near: vec4<f32>,
}

@group(1) @binding(0) var<uniform> spot_shadow: SpotShadow;
@group(1) @binding(1) var spot_shadow_map: texture_depth_2d;
@group(1) @binding(2) var spot_shadow_sampler: sampler_comparison;

fn sample_spot_shadow(world_pos: vec3<f32>, world_normal: vec3<f32>) -> f32 {
	let biased_pos = world_pos + world_normal * spot_shadow.params.y;
	let clip = spot_shadow.view_proj * vec4<f32>(biased_pos, 1.0);
	if (clip.w <= 0.00001) {
		return 1.0;
	}
	let ndc = clip.xyz / clip.w;
	let uv = vec2<f32>(ndc.x * 0.5 + 0.5, 0.5 - ndc.y * 0.5);
	if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0 || ndc.z < 0.0 || ndc.z > 1.0) {
		return 1.0;
	}
	let ref_depth = clamp(ndc.z - spot_shadow.params.x, 0.0, 1.0);
	let inv_res = spot_shadow.params.z;
	let pad = 0.51 * inv_res;
	let uv_min = vec2<f32>(pad, pad);
	let uv_max = vec2<f32>(1.0 - pad, 1.0 - pad);
	if (spot_shadow.params.w < 0.5) {
		return textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv, uv_min, uv_max), ref_depth);
	}
	var sum: f32 = 0.0;
	sum = sum + textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv + vec2<f32>(-inv_res, -inv_res), uv_min, uv_max), ref_depth);
	sum = sum + textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv + vec2<f32>(0.0, -inv_res), uv_min, uv_max), ref_depth);
	sum = sum + textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv + vec2<f32>(inv_res, -inv_res), uv_min, uv_max), ref_depth);
	sum = sum + textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv + vec2<f32>(-inv_res, 0.0), uv_min, uv_max), ref_depth);
	sum = sum + textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv, uv_min, uv_max), ref_depth);
	sum = sum + textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv + vec2<f32>(inv_res, 0.0), uv_min, uv_max), ref_depth);
	sum = sum + textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv + vec2<f32>(-inv_res, inv_res), uv_min, uv_max), ref_depth);
	sum = sum + textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv + vec2<f32>(0.0, inv_res), uv_min, uv_max), ref_depth);
	sum = sum + textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv + vec2<f32>(inv_res, inv_res), uv_min, uv_max), ref_depth);
	return sum / 9.0;
}

fn evaluate_spot_lighting(world_pos: vec3<f32>, world_normal: vec3<f32>, albedo: vec3<f32>) -> vec3<f32> {
	let n = normalize(world_normal);
	let to_light = spot_shadow.position_range.xyz - world_pos;
	let dist = length(to_light);
	let light_range = spot_shadow.position_range.w;
	let ambient = albedo * spot_shadow.ambient_near.xyz;
	if (dist <= 0.0001 || dist >= light_range) {
		return ambient;
	}
	let l_dir = to_light / dist;
	let ndl = max(dot(n, l_dir), 0.0);
	if (ndl <= 0.0) {
		return ambient;
	}
	let cos_theta = dot(-l_dir, spot_shadow.direction_inner.xyz);
	let cos_outer = spot_shadow.color_outer.w;
	let cos_inner = spot_shadow.direction_inner.w;
	let cone_t = clamp((cos_theta - cos_outer) / max(cos_inner - cos_outer, 0.0001), 0.0, 1.0);
	let cone = cone_t * cone_t * (3.0 - 2.0 * cone_t);
	if (cone <= 0.0) {
		return ambient;
	}
	let ratio = dist / light_range;
	let r2 = ratio * ratio;
	let win = clamp(1.0 - r2 * r2, 0.0, 1.0);
	let atten = (win * win) / max(dist * dist, 0.01);
	let vis = sample_spot_shadow(world_pos, n);
	return ambient + albedo * spot_shadow.color_outer.xyz * (ndl * cone * atten * vis);
}
`

// PointShadowWGSL declares the omnidirectional point-light shadow uniform block,
// 3x2 2D depth atlas, and comparison sampler at @group(1) bindings 0..2 together
// with portable WGSL helpers sample_point_shadow and evaluate_point_lighting.
//
// Prepend [CameraWGSL] and PointShadowWGSL to a 3D fragment shader. Because all
// six 90-degree cube faces live in a single 3x2 texture_depth_2d atlas with
// per-tile UV clamping, sampling and 3x3 PCF work portably across Metal,
// Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and NSGo's software
// GPU renderer without cubemap depth-attachment limitations.
const PointShadowWGSL = `struct PointShadow {
	position_range: vec4<f32>,
	color_near: vec4<f32>,
	params: vec4<f32>,
	ambient_pad: vec4<f32>,
}

@group(1) @binding(0) var<uniform> point_shadow: PointShadow;
@group(1) @binding(1) var point_shadow_map: texture_depth_2d;
@group(1) @binding(2) var point_shadow_sampler: sampler_comparison;

fn point_shadow_tile_tap(col: f32, row: f32, local_uv: vec2<f32>, offset: vec2<f32>, pad: f32, ref_depth: f32) -> f32 {
	let clamped = clamp(local_uv + offset, vec2<f32>(pad, pad), vec2<f32>(1.0 - pad, 1.0 - pad));
	let atlas_uv = vec2<f32>((col + clamped.x) / 3.0, (row + clamped.y) / 2.0);
	return textureSampleCompareLevel(point_shadow_map, point_shadow_sampler, atlas_uv, ref_depth);
}

fn sample_point_shadow(world_pos: vec3<f32>, world_normal: vec3<f32>) -> f32 {
	let biased_pos = world_pos + world_normal * point_shadow.params.y;
	let v = biased_pos - point_shadow.position_range.xyz;
	let av = abs(v);
	var col: f32 = 0.0;
	var row: f32 = 0.0;
	var x_view: f32 = 0.0;
	var y_view: f32 = 0.0;
	var axis_dist: f32 = 0.0;
	if (av.x >= av.y && av.x >= av.z) {
		if (v.x >= 0.0) {
			col = 0.0; row = 0.0;
			x_view = v.z; y_view = v.y; axis_dist = v.x;
		} else {
			col = 1.0; row = 0.0;
			x_view = -v.z; y_view = v.y; axis_dist = -v.x;
		}
	} else if (av.y >= av.x && av.y >= av.z) {
		if (v.y >= 0.0) {
			col = 2.0; row = 0.0;
			x_view = v.x; y_view = v.z; axis_dist = v.y;
		} else {
			col = 0.0; row = 1.0;
			x_view = v.x; y_view = -v.z; axis_dist = -v.y;
		}
	} else {
		if (v.z >= 0.0) {
			col = 1.0; row = 1.0;
			x_view = -v.x; y_view = v.y; axis_dist = v.z;
		} else {
			col = 2.0; row = 1.0;
			x_view = v.x; y_view = v.y; axis_dist = -v.z;
		}
	}
	let near_p = point_shadow.color_near.w;
	let far_p = point_shadow.position_range.w;
	if (axis_dist <= near_p || axis_dist >= far_p) {
		return 1.0;
	}
	let local_uv = vec2<f32>((x_view / axis_dist) * 0.5 + 0.5, 0.5 - (y_view / axis_dist) * 0.5);
	let ndc_z = (far_p / (far_p - near_p)) * (1.0 - near_p / axis_dist);
	let ref_depth = clamp(ndc_z - point_shadow.params.x, 0.0, 1.0);
	let inv_res = point_shadow.params.z;
	let pad = 0.51 * inv_res;
	if (point_shadow.params.w < 0.5) {
		return point_shadow_tile_tap(col, row, local_uv, vec2<f32>(0.0, 0.0), pad, ref_depth);
	}
	var sum: f32 = 0.0;
	sum = sum + point_shadow_tile_tap(col, row, local_uv, vec2<f32>(-inv_res, -inv_res), pad, ref_depth);
	sum = sum + point_shadow_tile_tap(col, row, local_uv, vec2<f32>(0.0, -inv_res), pad, ref_depth);
	sum = sum + point_shadow_tile_tap(col, row, local_uv, vec2<f32>(inv_res, -inv_res), pad, ref_depth);
	sum = sum + point_shadow_tile_tap(col, row, local_uv, vec2<f32>(-inv_res, 0.0), pad, ref_depth);
	sum = sum + point_shadow_tile_tap(col, row, local_uv, vec2<f32>(0.0, 0.0), pad, ref_depth);
	sum = sum + point_shadow_tile_tap(col, row, local_uv, vec2<f32>(inv_res, 0.0), pad, ref_depth);
	sum = sum + point_shadow_tile_tap(col, row, local_uv, vec2<f32>(-inv_res, inv_res), pad, ref_depth);
	sum = sum + point_shadow_tile_tap(col, row, local_uv, vec2<f32>(0.0, inv_res), pad, ref_depth);
	sum = sum + point_shadow_tile_tap(col, row, local_uv, vec2<f32>(inv_res, inv_res), pad, ref_depth);
	return sum / 9.0;
}

fn evaluate_point_lighting(world_pos: vec3<f32>, world_normal: vec3<f32>, albedo: vec3<f32>) -> vec3<f32> {
	let n = normalize(world_normal);
	let to_light = point_shadow.position_range.xyz - world_pos;
	let dist = length(to_light);
	let light_range = point_shadow.position_range.w;
	let ambient = albedo * point_shadow.ambient_pad.xyz;
	if (dist <= 0.0001 || dist >= light_range) {
		return ambient;
	}
	let l_dir = to_light / dist;
	let ndl = max(dot(n, l_dir), 0.0);
	if (ndl <= 0.0) {
		return ambient;
	}
	let ratio = dist / light_range;
	let r2 = ratio * ratio;
	let win = clamp(1.0 - r2 * r2, 0.0, 1.0);
	let atten = (win * win) / max(dist * dist, 0.01);
	let vis = sample_point_shadow(world_pos, n);
	return ambient + albedo * point_shadow.color_near.xyz * (ndl * atten * vis);
}
`

const spotShadowLitShaderWGSL = CameraWGSL + "\n" + SpotShadowWGSL + `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
}

@vertex
fn vs_spot_lit(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.world_pos = in.pos;
	out.world_normal = in.normal;
	return out;
}

@fragment
fn fs_spot_lit(in: VertexOut) -> @location(0) vec4<f32> {
	let color = evaluate_spot_lighting(in.world_pos, in.world_normal, vec3<f32>(0.85, 0.85, 0.85));
	return vec4<f32>(clamp(color, vec3<f32>(0.0), vec3<f32>(1.0)), 1.0);
}
`

const pointShadowLitShaderWGSL = CameraWGSL + "\n" + PointShadowWGSL + `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
}

@vertex
fn vs_point_lit(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.world_pos = in.pos;
	out.world_normal = in.normal;
	return out;
}

@fragment
fn fs_point_lit(in: VertexOut) -> @location(0) vec4<f32> {
	let color = evaluate_point_lighting(in.world_pos, in.world_normal, vec3<f32>(0.85, 0.85, 0.85));
	return vec4<f32>(clamp(color, vec3<f32>(0.0), vec3<f32>(1.0)), 1.0);
}
`

// SpotShadowConfig configures a perspective 2D [SpotShadowMap].
type SpotShadowConfig struct {
	// Resolution is the width and height of the square depth shadow map in texels.
	// Zero defaults to 1024.
	Resolution uint32
	// DepthBias subtracts a constant NDC [0, 1] bias during depth comparison to
	// avoid shadow acne. Zero defaults to 0.0015 when using DefaultSpotShadowConfig;
	// explicit zero disables constant depth bias.
	DepthBias float32
	// NormalBias offsets the shaded world position along its surface normal before
	// projecting into light space.
	NormalBias float32
	// Filter selects hard single-tap or 3x3 PCF comparison sampling.
	Filter LocalShadowFilter
	// CullMode sets face culling for the shadow caster pass (default gpu.CullNone).
	CullMode gpu.CullMode
	// MaxCasters is the maximum number of DrawCaster calls per shadow pass.
	// Zero defaults to 256.
	MaxCasters uint32
	// Label names GPU resources in capture tools.
	Label string
	// Retained keeps resources across GPU device loss and restore.
	Retained bool
}

// DefaultSpotShadowConfig returns a ready-to-use 1024x1024 PCF 3x3 spot shadow configuration.
func DefaultSpotShadowConfig() SpotShadowConfig {
	return SpotShadowConfig{
		Resolution: defaultSpotShadowResolution,
		DepthBias:  defaultLocalShadowDepthBias,
		NormalBias: defaultLocalShadowNormalBias,
		Filter:     LocalShadowFilterPCF3x3,
		CullMode:   gpu.CullNone,
		MaxCasters: defaultSpotMaxCasters,
	}
}

// Normalized returns a copy of c with zero Resolution and MaxCasters filled with defaults.
func (c SpotShadowConfig) Normalized() SpotShadowConfig {
	if c.Resolution == 0 {
		c.Resolution = defaultSpotShadowResolution
	}
	if c.MaxCasters == 0 {
		c.MaxCasters = defaultSpotMaxCasters
	}
	return c
}

// Validate checks that c's resolution, biases, filter, and cull mode are valid.
// Every error wraps [ErrInvalidLocalShadow].
func (c SpotShadowConfig) Validate() error {
	n := c.Normalized()
	if !finite(n.DepthBias) || n.DepthBias < 0 {
		return fmt.Errorf("%w: spot DepthBias %v must be finite and non-negative", ErrInvalidLocalShadow, n.DepthBias)
	}
	if !finite(n.NormalBias) || n.NormalBias < 0 {
		return fmt.Errorf("%w: spot NormalBias %v must be finite and non-negative", ErrInvalidLocalShadow, n.NormalBias)
	}
	if !localShadowValidFilter(n.Filter) {
		return fmt.Errorf("%w: invalid spot Filter %d", ErrInvalidLocalShadow, n.Filter)
	}
	if !localShadowValidCull(n.CullMode) {
		return fmt.Errorf("%w: invalid spot CullMode %d", ErrInvalidLocalShadow, n.CullMode)
	}
	return nil
}

// SpotShadowLight defines the spot light parameters used to build the perspective
// shadow projection and evaluate direct shadowed spot lighting.
type SpotShadowLight struct {
	Position       math3d.Vec3
	Direction      math3d.Vec3
	Color          math3d.Vec3
	Intensity      float32
	Near           float32
	Range          float32
	InnerConeAngle float32 // half-angle in radians, 0 <= InnerConeAngle <= OuterConeAngle
	OuterConeAngle float32 // half-angle in radians, 0 < OuterConeAngle < pi/2
	Ambient        math3d.Vec3
}

// Validate checks that every vector and scalar is finite and within valid physical
// and perspective-projection bounds. Every error wraps [ErrInvalidLocalShadow].
func (l SpotShadowLight) Validate() error {
	if !finite3(l.Position) {
		return fmt.Errorf("%w: spot Position is not finite", ErrInvalidLocalShadow)
	}
	if !finite3(l.Direction) || l.Direction.LenSq() < 1e-12 {
		return fmt.Errorf("%w: spot Direction must be finite and non-zero", ErrInvalidLocalShadow)
	}
	if !finite3(l.Color) || l.Color.X < 0 || l.Color.Y < 0 || l.Color.Z < 0 {
		return fmt.Errorf("%w: spot Color must be finite and non-negative", ErrInvalidLocalShadow)
	}
	if !finite3(l.Ambient) || l.Ambient.X < 0 || l.Ambient.Y < 0 || l.Ambient.Z < 0 {
		return fmt.Errorf("%w: spot Ambient must be finite and non-negative", ErrInvalidLocalShadow)
	}
	if !finite(l.Intensity) || l.Intensity < 0 {
		return fmt.Errorf("%w: spot Intensity %v must be finite and non-negative", ErrInvalidLocalShadow, l.Intensity)
	}
	if !finite(l.Near) || l.Near <= 0 {
		return fmt.Errorf("%w: spot Near %v must be finite and positive", ErrInvalidLocalShadow, l.Near)
	}
	if !finite(l.Range) || l.Range <= l.Near {
		return fmt.Errorf("%w: spot Range %v must be finite and greater than Near %v", ErrInvalidLocalShadow, l.Range, l.Near)
	}
	const maxOuter = float32(math.Pi/2 - 1e-3)
	if !finite(l.OuterConeAngle) || l.OuterConeAngle <= 0 || l.OuterConeAngle >= maxOuter {
		return fmt.Errorf("%w: spot OuterConeAngle %v must be in (0, pi/2)", ErrInvalidLocalShadow, l.OuterConeAngle)
	}
	if !finite(l.InnerConeAngle) || l.InnerConeAngle < 0 || l.InnerConeAngle > l.OuterConeAngle {
		return fmt.Errorf("%w: spot InnerConeAngle %v must be in [0, OuterConeAngle %v]", ErrInvalidLocalShadow, l.InnerConeAngle, l.OuterConeAngle)
	}
	return nil
}

// ViewMatrix returns the right-handed shadow view matrix looking from l.Position
// along l.Direction.
func (l SpotShadowLight) ViewMatrix() math3d.Mat4 {
	dir := l.Direction.Norm()
	up := math3d.Vec3Up()
	if localShadowAbs(dir.Y) > 0.99 {
		up = math3d.Vec3{Z: 1}
	}
	return math3d.LookAt(l.Position, l.Position.Add(dir), up)
}

// ProjMatrix returns the [0, 1] NDC depth perspective projection matching
// fovY = 2 * l.OuterConeAngle, aspect = 1, near = l.Near, and far = l.Range.
func (l SpotShadowLight) ProjMatrix() math3d.Mat4 {
	return math3d.Perspective(2*l.OuterConeAngle, 1.0, l.Near, l.Range)
}

// ViewProj returns ProjMatrix().Mul(ViewMatrix()).
func (l SpotShadowLight) ViewProj() math3d.Mat4 {
	return l.ProjMatrix().Mul(l.ViewMatrix())
}

// SpotShadowUniform is the packed uniform data matching the WGSL SpotShadow block
// declared by [SpotShadowWGSL].
type SpotShadowUniform struct {
	ViewProj       math3d.Mat4
	Position       math3d.Vec3
	Range          float32
	Direction      math3d.Vec3
	CosInner       float32
	ColorIntensity math3d.Vec3
	CosOuter       float32
	DepthBias      float32
	NormalBias     float32
	InvResolution  float32
	FilterMode     float32
	Ambient        math3d.Vec3
	Near           float32
}

// NewSpotShadowUniform computes the uniform data for light and cfg.
func NewSpotShadowUniform(light SpotShadowLight, cfg SpotShadowConfig) SpotShadowUniform {
	n := cfg.Normalized()
	dir := light.Direction.Norm()
	filterMode := float32(0)
	if n.Filter == LocalShadowFilterPCF3x3 {
		filterMode = 1
	}
	return SpotShadowUniform{
		ViewProj:       light.ViewProj(),
		Position:       light.Position,
		Range:          light.Range,
		Direction:      dir,
		CosInner:       float32(math.Cos(float64(light.InnerConeAngle))),
		ColorIntensity: light.Color.Scale(light.Intensity),
		CosOuter:       float32(math.Cos(float64(light.OuterConeAngle))),
		DepthBias:      n.DepthBias,
		NormalBias:     n.NormalBias,
		InvResolution:  1.0 / float32(n.Resolution),
		FilterMode:     filterMode,
		Ambient:        light.Ambient,
		Near:           light.Near,
	}
}

// Encode writes u into dst in the little-endian layout declared by [SpotShadowWGSL]
// and returns dst[:SpotShadowUniformSize]. It allocates nothing and panics if
// len(dst) < SpotShadowUniformSize.
func (u *SpotShadowUniform) Encode(dst []byte) []byte {
	if len(dst) < SpotShadowUniformSize {
		panic("render3d: SpotShadowUniform.Encode: dst shorter than SpotShadowUniformSize")
	}
	dst = dst[:SpotShadowUniformSize]
	off := 0
	putF := func(v float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(v))
		off += 4
	}
	for _, f := range u.ViewProj {
		putF(f)
	}
	putF(u.Position.X)
	putF(u.Position.Y)
	putF(u.Position.Z)
	putF(u.Range)
	putF(u.Direction.X)
	putF(u.Direction.Y)
	putF(u.Direction.Z)
	putF(u.CosInner)
	putF(u.ColorIntensity.X)
	putF(u.ColorIntensity.Y)
	putF(u.ColorIntensity.Z)
	putF(u.CosOuter)
	putF(u.DepthBias)
	putF(u.NormalBias)
	putF(u.InvResolution)
	putF(u.FilterMode)
	putF(u.Ambient.X)
	putF(u.Ambient.Y)
	putF(u.Ambient.Z)
	putF(u.Near)
	return dst
}

// SpotShadowMap manages a depth-only perspective 2D shadow map, its depth caster
// pipeline, and its sampling bind group for a spot light on a [gpu.Device].
type SpotShadowMap struct {
	cfg           SpotShadowConfig
	rt            *gpu.RenderTarget
	sampler       *gpu.Sampler
	uniformBuf    *gpu.Buffer
	casterBuf     *gpu.Buffer
	layout        *gpu.BindGroupLayout
	bindGroup     *gpu.BindGroup
	casterLayout  *gpu.BindGroupLayout
	casterGroup   *gpu.BindGroup
	depthPipe     *gpu.Pipeline
	uniforms      SpotShadowUniform
	casterSlot    uint32
	uniformBytes  [SpotShadowUniformSize]byte
	casterScratch [localShadowCasterDataSize]byte
}

// NewSpotShadowMap creates a depth-only perspective spot shadow map and its GPU
// resources scoped to cx. Invalid configuration returns an error wrapping
// [ErrInvalidLocalShadow]; device errors are returned as [gpu.Error].
func NewSpotShadowMap(cx *core.Scope, dev *gpu.Device, cfg SpotShadowConfig) (*SpotShadowMap, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	n := cfg.Normalized()
	maxDim := dev.Limits().MaxTextureDimension2D
	if maxDim > 0 && n.Resolution > maxDim {
		return nil, fmt.Errorf("%w: spot resolution %d exceeds MaxTextureDimension2D %d", ErrInvalidLocalShadow, n.Resolution, maxDim)
	}

	label := n.Label
	if label == "" {
		label = "render3d.spot_shadow"
	}

	var closers []interface{ Close() error }
	fail := func(err error) (*SpotShadowMap, error) {
		for i := len(closers) - 1; i >= 0; i-- {
			if closers[i] != nil {
				_ = closers[i].Close()
			}
		}
		return nil, err
	}

	rt, err := dev.CreateRenderTarget(cx, gpu.RenderTargetDesc{
		Label:       label + ".depth",
		Width:       n.Resolution,
		Height:      n.Resolution,
		DepthFormat: gpu.FormatDepth32Float,
		Retained:    n.Retained,
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, rt)

	filterMode := gpu.FilterNearest
	if n.Filter == LocalShadowFilterPCF3x3 {
		filterMode = gpu.FilterLinear
	}
	samp, err := dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:     label + ".sampler",
		MinFilter: filterMode,
		MagFilter: filterMode,
		AddressU:  gpu.AddressClampToEdge,
		AddressV:  gpu.AddressClampToEdge,
		AddressW:  gpu.AddressClampToEdge,
		Compare:   gpu.CompareLessEqual,
		Retained:  n.Retained,
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, samp)

	uniformBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".uniform",
		Size:     SpotShadowUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: n.Retained,
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, uniformBuf)

	casterBufSize := uint64(n.MaxCasters) * localShadowCasterSlotStride
	casterBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".casters",
		Size:     casterBufSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: n.Retained,
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, casterBuf)

	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingComparisonSampler},
		},
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, layout)

	bindGroup, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + ".bind_group",
		Layout: layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: uniformBuf, Size: SpotShadowUniformSize},
			{Binding: 1, Texture: rt.Depth},
			{Binding: 2, Sampler: samp},
		},
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, bindGroup)

	casterLayout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".caster_layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex, Type: gpu.BindingUniform, HasDynamicOffset: true},
		},
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, casterLayout)

	casterGroup, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + ".caster_group",
		Layout: casterLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: casterBuf, Offset: 0, Size: localShadowCasterDataSize},
		},
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, casterGroup)

	depthMod, err := gpu.CompileWGSL(localShadowDepthWGSL)
	if err != nil {
		return fail(err)
	}
	depthPipe, err := dev.CreatePipeline(cx, gpu.PipelineDesc{
		Label:         label + ".depth_pipe",
		Vertex:        gpu.ShaderStage{Module: depthMod, EntryPoint: "vs_local_shadow_depth"},
		Layouts:       []*gpu.BindGroupLayout{casterLayout},
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  n.CullMode,
		},
		DepthStencil: &gpu.DepthStencilState{
			Format:       gpu.FormatDepth32Float,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		},
		Retained: n.Retained,
	})
	if err != nil {
		return fail(err)
	}

	return &SpotShadowMap{
		cfg:          n,
		rt:           rt,
		sampler:      samp,
		uniformBuf:   uniformBuf,
		casterBuf:    casterBuf,
		layout:       layout,
		bindGroup:    bindGroup,
		casterLayout: casterLayout,
		casterGroup:  casterGroup,
		depthPipe:    depthPipe,
	}, nil
}

// Update validates light, updates the spot shadow projection and uniforms, and
// writes the uniform buffer on dev without heap allocations.
func (s *SpotShadowMap) Update(dev *gpu.Device, light SpotShadowLight) error {
	if s == nil || !s.Valid() {
		return fmt.Errorf("%w: spot shadow map is not valid", ErrInvalidLocalShadow)
	}
	if err := light.Validate(); err != nil {
		return err
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	s.uniforms = NewSpotShadowUniform(light, s.cfg)
	dev.WriteBuffer(s.uniformBuf, 0, s.uniforms.Encode(s.uniformBytes[:]))
	return nil
}

// Uniforms returns the last spot shadow uniform data written via [SpotShadowMap.Update].
func (s *SpotShadowMap) Uniforms() SpotShadowUniform {
	if s == nil {
		return SpotShadowUniform{}
	}
	return s.uniforms
}

// ViewProj returns the spot light's perspective view-projection matrix.
func (s *SpotShadowMap) ViewProj() math3d.Mat4 {
	if s == nil {
		return math3d.Mat4Identity()
	}
	return s.uniforms.ViewProj
}

// Config returns the normalized [SpotShadowConfig].
func (s *SpotShadowMap) Config() SpotShadowConfig {
	if s == nil {
		return SpotShadowConfig{}
	}
	return s.cfg
}

// RenderTarget returns the underlying depth-only [gpu.RenderTarget].
func (s *SpotShadowMap) RenderTarget() *gpu.RenderTarget {
	if s == nil {
		return nil
	}
	return s.rt
}

// DepthTexture returns the underlying Depth32Float [gpu.Texture].
func (s *SpotShadowMap) DepthTexture() *gpu.Texture {
	if s == nil || s.rt == nil {
		return nil
	}
	return s.rt.Depth
}

// Sampler returns the comparison [gpu.Sampler] bound in [SpotShadowMap.BindGroup].
func (s *SpotShadowMap) Sampler() *gpu.Sampler {
	if s == nil {
		return nil
	}
	return s.sampler
}

// Layout returns the bind group layout for sampling this spot shadow map
// (binding 0: SpotShadow uniform, binding 1: texture_depth_2d, binding 2: sampler_comparison).
func (s *SpotShadowMap) Layout() *gpu.BindGroupLayout {
	if s == nil {
		return nil
	}
	return s.layout
}

// BindGroup returns the bind group to set at [LocalShadowGroup] (or any group slot
// matching [SpotShadowMap.Layout]) during the lit render pass.
func (s *SpotShadowMap) BindGroup() *gpu.BindGroup {
	if s == nil {
		return nil
	}
	return s.bindGroup
}

// ByteSize returns the GPU memory held by the depth texture and uniform buffers.
func (s *SpotShadowMap) ByteSize() uint64 {
	if s == nil {
		return 0
	}
	depthBytes := uint64(s.cfg.Resolution) * uint64(s.cfg.Resolution) * 4
	return depthBytes + s.uniformBuf.Size() + s.casterBuf.Size()
}

// Valid reports whether all underlying GPU resources are open and live.
func (s *SpotShadowMap) Valid() bool {
	return s != nil &&
		s.rt != nil && s.rt.Valid() &&
		s.sampler != nil && s.sampler.Valid() &&
		s.uniformBuf != nil && s.uniformBuf.Valid() &&
		s.casterBuf != nil && s.casterBuf.Valid() &&
		s.layout != nil && s.layout.Valid() &&
		s.bindGroup != nil && s.bindGroup.Valid() &&
		s.casterLayout != nil && s.casterLayout.Valid() &&
		s.casterGroup != nil && s.casterGroup.Valid() &&
		s.depthPipe != nil && s.depthPipe.Valid()
}

// LocalShadowPassRecorder is satisfied by both [*gpu.Encoder] and [*gpu.Frame],
// allowing [SpotShadowMap.BeginShadowPass] and [PointShadowMap.BeginFacePass]
// to record shadow passes both inside [gpu.ViewProps.OnFrame] and on standalone
// command encoders.
type LocalShadowPassRecorder interface {
	BeginRenderPass(desc gpu.RenderPassDesc) *gpu.RenderPass
}

// BeginShadowPass begins a depth-only render pass targeting the spot shadow map,
// clears depth to 1.0, sets the full viewport/scissor, binds the depth pipeline,
// and resets the caster dynamic-offset slot counter.
func (s *SpotShadowMap) BeginShadowPass(rec LocalShadowPassRecorder) *gpu.RenderPass {
	if s == nil || rec == nil || !s.Valid() {
		return nil
	}
	s.casterSlot = 0
	pass := rec.BeginRenderPass(gpu.RenderPassDesc{
		Label: s.cfg.Label + ".pass",
		DepthStencil: &gpu.DepthStencilAttachment{
			Texture:      s.rt.Depth,
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   1.0,
		},
	})
	res := s.cfg.Resolution
	pass.SetViewport(0, 0, float32(res), float32(res), 0, 1)
	pass.SetScissor(0, 0, int32(res), int32(res))
	pass.SetPipeline(s.depthPipe)
	return pass
}

// DrawCaster records a shadow caster draw for mesh transformed by model inside
// a pass started by [SpotShadowMap.BeginShadowPass]. Each call uses a distinct
// 256-byte aligned dynamic-offset uniform slot so multiple casters can be drawn
// in one pass without overwriting earlier transforms.
func (s *SpotShadowMap) DrawCaster(dev *gpu.Device, pass *gpu.RenderPass, mesh *GPUMesh, model math3d.Mat4) {
	if s == nil || dev == nil || pass == nil || !s.Valid() || !mesh.Valid() {
		return
	}
	if s.casterSlot >= s.cfg.MaxCasters {
		return
	}
	offset := uint64(s.casterSlot) * localShadowCasterSlotStride
	s.casterSlot++
	encodeLocalShadowCaster(s.casterScratch[:], s.uniforms.ViewProj, model)
	dev.WriteBuffer(s.casterBuf, offset, s.casterScratch[:])
	pass.SetBindGroup(0, s.casterGroup, uint32(offset))
	mesh.Draw(pass)
}

// Close releases all GPU resources owned by s. It is safe to call on a nil
// receiver or more than once.
func (s *SpotShadowMap) Close() error {
	if s == nil {
		return nil
	}
	var err error
	for _, c := range []interface{ Close() error }{
		s.depthPipe,
		s.casterGroup,
		s.casterLayout,
		s.bindGroup,
		s.layout,
		s.casterBuf,
		s.uniformBuf,
		s.sampler,
		s.rt,
	} {
		if c != nil {
			err = errors.Join(err, c.Close())
		}
	}
	return err
}

// PointShadowConfig configures an omnidirectional 3x2 depth-atlas [PointShadowMap].
type PointShadowConfig struct {
	// FaceResolution is the width and height of one cube face tile in texels.
	// The 3x2 depth atlas has size (3 * FaceResolution) x (2 * FaceResolution).
	// Zero defaults to 512.
	FaceResolution uint32
	// DepthBias subtracts a constant NDC [0, 1] bias during depth comparison.
	DepthBias float32
	// NormalBias offsets the shaded world position along its surface normal before
	// selecting the cube face and comparing depth.
	NormalBias float32
	// Filter selects hard single-tap or 3x3 PCF comparison sampling.
	Filter LocalShadowFilter
	// CullMode sets face culling for the shadow caster passes (default gpu.CullNone).
	CullMode gpu.CullMode
	// MaxCasters is the total maximum DrawCaster calls across all 6 faces per frame.
	// Zero defaults to 1024.
	MaxCasters uint32
	// Label names GPU resources in capture tools.
	Label string
	// Retained keeps resources across GPU device loss and restore.
	Retained bool
}

// DefaultPointShadowConfig returns a ready-to-use 512x512-per-face PCF 3x3 point
// shadow configuration.
func DefaultPointShadowConfig() PointShadowConfig {
	return PointShadowConfig{
		FaceResolution: defaultPointShadowResolution,
		DepthBias:      defaultLocalShadowDepthBias,
		NormalBias:     defaultLocalShadowNormalBias,
		Filter:         LocalShadowFilterPCF3x3,
		CullMode:       gpu.CullNone,
		MaxCasters:     defaultPointMaxCasters,
	}
}

// Normalized returns a copy of c with zero FaceResolution and MaxCasters filled
// with defaults.
func (c PointShadowConfig) Normalized() PointShadowConfig {
	if c.FaceResolution == 0 {
		c.FaceResolution = defaultPointShadowResolution
	}
	if c.MaxCasters == 0 {
		c.MaxCasters = defaultPointMaxCasters
	}
	return c
}

// AtlasSize returns the width and height in texels of the 3x2 2D depth atlas.
func (c PointShadowConfig) AtlasSize() (width, height uint32) {
	n := c.Normalized()
	return n.FaceResolution * PointShadowAtlasCols, n.FaceResolution * PointShadowAtlasRows
}

// Validate checks that c's resolution, biases, filter, and cull mode are valid.
// Every error wraps [ErrInvalidLocalShadow].
func (c PointShadowConfig) Validate() error {
	n := c.Normalized()
	if uint64(n.FaceResolution)*PointShadowAtlasCols > math.MaxUint32 {
		return fmt.Errorf("%w: point FaceResolution %d overflows atlas dimensions", ErrInvalidLocalShadow, n.FaceResolution)
	}
	if !finite(n.DepthBias) || n.DepthBias < 0 {
		return fmt.Errorf("%w: point DepthBias %v must be finite and non-negative", ErrInvalidLocalShadow, n.DepthBias)
	}
	if !finite(n.NormalBias) || n.NormalBias < 0 {
		return fmt.Errorf("%w: point NormalBias %v must be finite and non-negative", ErrInvalidLocalShadow, n.NormalBias)
	}
	if !localShadowValidFilter(n.Filter) {
		return fmt.Errorf("%w: invalid point Filter %d", ErrInvalidLocalShadow, n.Filter)
	}
	if !localShadowValidCull(n.CullMode) {
		return fmt.Errorf("%w: invalid point CullMode %d", ErrInvalidLocalShadow, n.CullMode)
	}
	return nil
}

// PointShadowLight defines the point light parameters used to build the six
// 90-degree cube-face shadow projections and evaluate omnidirectional lighting.
type PointShadowLight struct {
	Position  math3d.Vec3
	Color     math3d.Vec3
	Intensity float32
	Near      float32
	Range     float32
	Ambient   math3d.Vec3
}

// Validate checks that all fields are finite and within valid projection bounds.
// Every error wraps [ErrInvalidLocalShadow].
func (l PointShadowLight) Validate() error {
	if !finite3(l.Position) {
		return fmt.Errorf("%w: point Position is not finite", ErrInvalidLocalShadow)
	}
	if !finite3(l.Color) || l.Color.X < 0 || l.Color.Y < 0 || l.Color.Z < 0 {
		return fmt.Errorf("%w: point Color must be finite and non-negative", ErrInvalidLocalShadow)
	}
	if !finite3(l.Ambient) || l.Ambient.X < 0 || l.Ambient.Y < 0 || l.Ambient.Z < 0 {
		return fmt.Errorf("%w: point Ambient must be finite and non-negative", ErrInvalidLocalShadow)
	}
	if !finite(l.Intensity) || l.Intensity < 0 {
		return fmt.Errorf("%w: point Intensity %v must be finite and non-negative", ErrInvalidLocalShadow, l.Intensity)
	}
	if !finite(l.Near) || l.Near <= 0 {
		return fmt.Errorf("%w: point Near %v must be finite and positive", ErrInvalidLocalShadow, l.Near)
	}
	if !finite(l.Range) || l.Range <= l.Near {
		return fmt.Errorf("%w: point Range %v must be finite and greater than Near %v", ErrInvalidLocalShadow, l.Range, l.Near)
	}
	return nil
}

// pointFaceBasis holds the canonical right-handed forward and up vectors for
// cube faces 0..5 matching [gpu.CubeFacePosX]..[gpu.CubeFaceNegZ] and
// [PointShadowWGSL].
var pointFaceBasis = [PointShadowFaceCount]struct {
	forward math3d.Vec3
	up      math3d.Vec3
}{
	gpu.CubeFacePosX: {forward: math3d.Vec3{X: 1}, up: math3d.Vec3{Y: 1}},
	gpu.CubeFaceNegX: {forward: math3d.Vec3{X: -1}, up: math3d.Vec3{Y: 1}},
	gpu.CubeFacePosY: {forward: math3d.Vec3{Y: 1}, up: math3d.Vec3{Z: 1}},
	gpu.CubeFaceNegY: {forward: math3d.Vec3{Y: -1}, up: math3d.Vec3{Z: -1}},
	gpu.CubeFacePosZ: {forward: math3d.Vec3{Z: 1}, up: math3d.Vec3{Y: 1}},
	gpu.CubeFaceNegZ: {forward: math3d.Vec3{Z: -1}, up: math3d.Vec3{Y: 1}},
}

// FaceViewMatrix returns the right-handed view matrix for cube face (0..5).
// Out-of-range face indices clamp to [gpu.CubeFacePosX].
func (l PointShadowLight) FaceViewMatrix(face gpu.CubeFace) math3d.Mat4 {
	if face >= PointShadowFaceCount {
		face = gpu.CubeFacePosX
	}
	b := pointFaceBasis[face]
	return math3d.LookAt(l.Position, l.Position.Add(b.forward), b.up)
}

// FaceProjMatrix returns the 90-degree square [0, 1] depth perspective projection
// shared by all six cube faces of the point light.
func (l PointShadowLight) FaceProjMatrix() math3d.Mat4 {
	return math3d.Perspective(math.Pi/2, 1.0, l.Near, l.Range)
}

// FaceViewProj returns FaceProjMatrix().Mul(FaceViewMatrix(face)).
func (l PointShadowLight) FaceViewProj(face gpu.CubeFace) math3d.Mat4 {
	return l.FaceProjMatrix().Mul(l.FaceViewMatrix(face))
}

// PointShadowUniform is the packed uniform data matching the WGSL PointShadow
// block declared by [PointShadowWGSL].
type PointShadowUniform struct {
	Position          math3d.Vec3
	Range             float32
	ColorIntensity    math3d.Vec3
	Near              float32
	DepthBias         float32
	NormalBias        float32
	InvFaceResolution float32
	FilterMode        float32
	Ambient           math3d.Vec3
}

// NewPointShadowUniform computes the uniform data for light and cfg.
func NewPointShadowUniform(light PointShadowLight, cfg PointShadowConfig) PointShadowUniform {
	n := cfg.Normalized()
	filterMode := float32(0)
	if n.Filter == LocalShadowFilterPCF3x3 {
		filterMode = 1
	}
	return PointShadowUniform{
		Position:          light.Position,
		Range:             light.Range,
		ColorIntensity:    light.Color.Scale(light.Intensity),
		Near:              light.Near,
		DepthBias:         n.DepthBias,
		NormalBias:        n.NormalBias,
		InvFaceResolution: 1.0 / float32(n.FaceResolution),
		FilterMode:        filterMode,
		Ambient:           light.Ambient,
	}
}

// Encode writes u into dst in the little-endian layout declared by [PointShadowWGSL]
// and returns dst[:PointShadowUniformSize]. It allocates nothing and panics if
// len(dst) < PointShadowUniformSize.
func (u *PointShadowUniform) Encode(dst []byte) []byte {
	if len(dst) < PointShadowUniformSize {
		panic("render3d: PointShadowUniform.Encode: dst shorter than PointShadowUniformSize")
	}
	dst = dst[:PointShadowUniformSize]
	off := 0
	putF := func(v float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(v))
		off += 4
	}
	putF(u.Position.X)
	putF(u.Position.Y)
	putF(u.Position.Z)
	putF(u.Range)
	putF(u.ColorIntensity.X)
	putF(u.ColorIntensity.Y)
	putF(u.ColorIntensity.Z)
	putF(u.Near)
	putF(u.DepthBias)
	putF(u.NormalBias)
	putF(u.InvFaceResolution)
	putF(u.FilterMode)
	putF(u.Ambient.X)
	putF(u.Ambient.Y)
	putF(u.Ambient.Z)
	putF(0)
	return dst
}

// PointShadowMap manages a 3x2 2D depth-atlas omnidirectional shadow map, its
// caster pipeline, and its sampling bind group for a point light on a [gpu.Device].
type PointShadowMap struct {
	cfg           PointShadowConfig
	atlasW        uint32
	atlasH        uint32
	rt            *gpu.RenderTarget
	sampler       *gpu.Sampler
	uniformBuf    *gpu.Buffer
	casterBuf     *gpu.Buffer
	layout        *gpu.BindGroupLayout
	bindGroup     *gpu.BindGroup
	casterLayout  *gpu.BindGroupLayout
	casterGroup   *gpu.BindGroup
	depthPipe     *gpu.Pipeline
	uniforms      PointShadowUniform
	faceViewProj  [PointShadowFaceCount]math3d.Mat4
	activeFace    gpu.CubeFace
	casterSlot    uint32
	uniformBytes  [PointShadowUniformSize]byte
	casterScratch [localShadowCasterDataSize]byte
}

// NewPointShadowMap creates a 3x2 Depth32Float atlas point shadow map and its
// GPU resources scoped to cx. Invalid configuration returns an error wrapping
// [ErrInvalidLocalShadow]; device errors are returned as [gpu.Error].
func NewPointShadowMap(cx *core.Scope, dev *gpu.Device, cfg PointShadowConfig) (*PointShadowMap, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	n := cfg.Normalized()
	atlasW, atlasH := n.AtlasSize()
	maxDim := dev.Limits().MaxTextureDimension2D
	if maxDim > 0 && (atlasW > maxDim || atlasH > maxDim) {
		return nil, fmt.Errorf("%w: point shadow atlas %dx%d exceeds MaxTextureDimension2D %d", ErrInvalidLocalShadow, atlasW, atlasH, maxDim)
	}

	label := n.Label
	if label == "" {
		label = "render3d.point_shadow"
	}

	var closers []interface{ Close() error }
	fail := func(err error) (*PointShadowMap, error) {
		for i := len(closers) - 1; i >= 0; i-- {
			if closers[i] != nil {
				_ = closers[i].Close()
			}
		}
		return nil, err
	}

	rt, err := dev.CreateRenderTarget(cx, gpu.RenderTargetDesc{
		Label:       label + ".atlas",
		Width:       atlasW,
		Height:      atlasH,
		DepthFormat: gpu.FormatDepth32Float,
		Retained:    n.Retained,
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, rt)

	filterMode := gpu.FilterNearest
	if n.Filter == LocalShadowFilterPCF3x3 {
		filterMode = gpu.FilterLinear
	}
	samp, err := dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:     label + ".sampler",
		MinFilter: filterMode,
		MagFilter: filterMode,
		AddressU:  gpu.AddressClampToEdge,
		AddressV:  gpu.AddressClampToEdge,
		AddressW:  gpu.AddressClampToEdge,
		Compare:   gpu.CompareLessEqual,
		Retained:  n.Retained,
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, samp)

	uniformBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".uniform",
		Size:     PointShadowUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: n.Retained,
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, uniformBuf)

	casterBufSize := uint64(n.MaxCasters) * localShadowCasterSlotStride
	casterBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".casters",
		Size:     casterBufSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: n.Retained,
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, casterBuf)

	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingComparisonSampler},
		},
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, layout)

	bindGroup, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + ".bind_group",
		Layout: layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: uniformBuf, Size: PointShadowUniformSize},
			{Binding: 1, Texture: rt.Depth},
			{Binding: 2, Sampler: samp},
		},
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, bindGroup)

	casterLayout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".caster_layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex, Type: gpu.BindingUniform, HasDynamicOffset: true},
		},
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, casterLayout)

	casterGroup, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + ".caster_group",
		Layout: casterLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: casterBuf, Offset: 0, Size: localShadowCasterDataSize},
		},
	})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, casterGroup)

	depthMod, err := gpu.CompileWGSL(localShadowDepthWGSL)
	if err != nil {
		return fail(err)
	}
	depthPipe, err := dev.CreatePipeline(cx, gpu.PipelineDesc{
		Label:         label + ".depth_pipe",
		Vertex:        gpu.ShaderStage{Module: depthMod, EntryPoint: "vs_local_shadow_depth"},
		Layouts:       []*gpu.BindGroupLayout{casterLayout},
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  n.CullMode,
		},
		DepthStencil: &gpu.DepthStencilState{
			Format:       gpu.FormatDepth32Float,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		},
		Retained: n.Retained,
	})
	if err != nil {
		return fail(err)
	}

	return &PointShadowMap{
		cfg:          n,
		atlasW:       atlasW,
		atlasH:       atlasH,
		rt:           rt,
		sampler:      samp,
		uniformBuf:   uniformBuf,
		casterBuf:    casterBuf,
		layout:       layout,
		bindGroup:    bindGroup,
		casterLayout: casterLayout,
		casterGroup:  casterGroup,
		depthPipe:    depthPipe,
	}, nil
}

// Update validates light, computes all six cube-face view-projection matrices,
// and uploads the point shadow uniform buffer on dev without heap allocations.
func (p *PointShadowMap) Update(dev *gpu.Device, light PointShadowLight) error {
	if p == nil || !p.Valid() {
		return fmt.Errorf("%w: point shadow map is not valid", ErrInvalidLocalShadow)
	}
	if err := light.Validate(); err != nil {
		return err
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	proj := light.FaceProjMatrix()
	for face := range gpu.CubeFace(PointShadowFaceCount) {
		p.faceViewProj[face] = proj.Mul(light.FaceViewMatrix(face))
	}
	p.uniforms = NewPointShadowUniform(light, p.cfg)
	dev.WriteBuffer(p.uniformBuf, 0, p.uniforms.Encode(p.uniformBytes[:]))
	return nil
}

// Uniforms returns the last point shadow uniform data written via [PointShadowMap.Update].
func (p *PointShadowMap) Uniforms() PointShadowUniform {
	if p == nil {
		return PointShadowUniform{}
	}
	return p.uniforms
}

// FaceViewProj returns the 90-degree perspective view-projection matrix for cube
// face (0..5). Returns identity for a nil receiver or out-of-range face.
func (p *PointShadowMap) FaceViewProj(face gpu.CubeFace) math3d.Mat4 {
	if p == nil || face >= PointShadowFaceCount {
		return math3d.Mat4Identity()
	}
	return p.faceViewProj[face]
}

// FaceViewport returns the pixel rectangle (x, y, width, height) inside the 3x2
// depth atlas assigned to cube face (0..5).
func (p *PointShadowMap) FaceViewport(face gpu.CubeFace) (x, y, width, height uint32) {
	if p == nil || face >= PointShadowFaceCount {
		return 0, 0, 0, 0
	}
	col := face % PointShadowAtlasCols
	row := face / PointShadowAtlasCols
	res := p.cfg.FaceResolution
	return col * res, row * res, res, res
}

// Config returns the normalized [PointShadowConfig].
func (p *PointShadowMap) Config() PointShadowConfig {
	if p == nil {
		return PointShadowConfig{}
	}
	return p.cfg
}

// AtlasWidth returns the full width of the 3x2 depth atlas (3 * FaceResolution).
func (p *PointShadowMap) AtlasWidth() uint32 {
	if p == nil {
		return 0
	}
	return p.atlasW
}

// AtlasHeight returns the full height of the 3x2 depth atlas (2 * FaceResolution).
func (p *PointShadowMap) AtlasHeight() uint32 {
	if p == nil {
		return 0
	}
	return p.atlasH
}

// RenderTarget returns the underlying depth-only 3x2 atlas [gpu.RenderTarget].
func (p *PointShadowMap) RenderTarget() *gpu.RenderTarget {
	if p == nil {
		return nil
	}
	return p.rt
}

// DepthTexture returns the underlying Depth32Float 3x2 atlas [gpu.Texture].
func (p *PointShadowMap) DepthTexture() *gpu.Texture {
	if p == nil || p.rt == nil {
		return nil
	}
	return p.rt.Depth
}

// Sampler returns the comparison [gpu.Sampler] bound in [PointShadowMap.BindGroup].
func (p *PointShadowMap) Sampler() *gpu.Sampler {
	if p == nil {
		return nil
	}
	return p.sampler
}

// Layout returns the bind group layout for sampling this point shadow map
// (binding 0: PointShadow uniform, binding 1: texture_depth_2d, binding 2: sampler_comparison).
func (p *PointShadowMap) Layout() *gpu.BindGroupLayout {
	if p == nil {
		return nil
	}
	return p.layout
}

// BindGroup returns the bind group to set at [LocalShadowGroup] (or any group slot
// matching [PointShadowMap.Layout]) during the lit render pass.
func (p *PointShadowMap) BindGroup() *gpu.BindGroup {
	if p == nil {
		return nil
	}
	return p.bindGroup
}

// ByteSize returns the GPU memory held by the 3x2 depth atlas and uniform buffers.
func (p *PointShadowMap) ByteSize() uint64 {
	if p == nil {
		return 0
	}
	depthBytes := uint64(p.atlasW) * uint64(p.atlasH) * 4
	return depthBytes + p.uniformBuf.Size() + p.casterBuf.Size()
}

// Valid reports whether all underlying GPU resources are open and live.
func (p *PointShadowMap) Valid() bool {
	return p != nil &&
		p.rt != nil && p.rt.Valid() &&
		p.sampler != nil && p.sampler.Valid() &&
		p.uniformBuf != nil && p.uniformBuf.Valid() &&
		p.casterBuf != nil && p.casterBuf.Valid() &&
		p.layout != nil && p.layout.Valid() &&
		p.bindGroup != nil && p.bindGroup.Valid() &&
		p.casterLayout != nil && p.casterLayout.Valid() &&
		p.casterGroup != nil && p.casterGroup.Valid() &&
		p.depthPipe != nil && p.depthPipe.Valid()
}

// BeginFacePass begins a depth-only render pass targeting cube face (0..5) in the
// 3x2 depth atlas. Calling with face == 0 clears the entire atlas depth buffer to
// 1.0 and resets the caster dynamic-offset slot counter; faces 1..5 preserve
// previously rendered tiles using [gpu.LoadLoad]. Viewport and scissor are locked
// to the face's tile so geometry cannot bleed into neighboring faces.
func (p *PointShadowMap) BeginFacePass(rec LocalShadowPassRecorder, face gpu.CubeFace) *gpu.RenderPass {
	if p == nil || rec == nil || !p.Valid() || face >= PointShadowFaceCount {
		return nil
	}
	loadOp := gpu.LoadLoad
	if face == 0 {
		loadOp = gpu.LoadClear
		p.casterSlot = 0
	}
	p.activeFace = face
	pass := rec.BeginRenderPass(gpu.RenderPassDesc{
		Label: fmt.Sprintf("%s.face%d", p.cfg.Label, face),
		DepthStencil: &gpu.DepthStencilAttachment{
			Texture:      p.rt.Depth,
			DepthLoadOp:  loadOp,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   1.0,
		},
	})
	vx, vy, vw, vh := p.FaceViewport(face)
	pass.SetViewport(float32(vx), float32(vy), float32(vw), float32(vh), 0, 1)
	pass.SetScissor(int32(vx), int32(vy), int32(vw), int32(vh))
	pass.SetPipeline(p.depthPipe)
	return pass
}

// DrawCaster records a shadow caster draw for mesh transformed by model into the
// currently active cube face pass started by [PointShadowMap.BeginFacePass].
func (p *PointShadowMap) DrawCaster(dev *gpu.Device, pass *gpu.RenderPass, mesh *GPUMesh, model math3d.Mat4) {
	if p == nil || dev == nil || pass == nil || !p.Valid() || !mesh.Valid() {
		return
	}
	if p.casterSlot >= p.cfg.MaxCasters {
		return
	}
	offset := uint64(p.casterSlot) * localShadowCasterSlotStride
	p.casterSlot++
	encodeLocalShadowCaster(p.casterScratch[:], p.faceViewProj[p.activeFace], model)
	dev.WriteBuffer(p.casterBuf, offset, p.casterScratch[:])
	pass.SetBindGroup(0, p.casterGroup, uint32(offset))
	mesh.Draw(pass)
}

// Close releases all GPU resources owned by p. It is safe to call on a nil
// receiver or more than once.
func (p *PointShadowMap) Close() error {
	if p == nil {
		return nil
	}
	var err error
	for _, c := range []interface{ Close() error }{
		p.depthPipe,
		p.casterGroup,
		p.casterLayout,
		p.bindGroup,
		p.layout,
		p.casterBuf,
		p.uniformBuf,
		p.sampler,
		p.rt,
	} {
		if c != nil {
			err = errors.Join(err, c.Close())
		}
	}
	return err
}

// LocalShadowLitPipelineOptions configures [NewSpotShadowLitPipeline] and
// [NewPointShadowLitPipeline].
type LocalShadowLitPipelineOptions struct {
	Label       string
	ColorFormat gpu.TextureFormat // defaults to dev.SurfaceFormat()
	DepthFormat gpu.TextureFormat // optional; when non-zero enables depth testing with CompareLess
	CullMode    gpu.CullMode      // default gpu.CullNone
	Retained    bool
}

// NewSpotShadowLitPipeline compiles [SpotShadowWGSL] with [CameraWGSL] and
// builds a ready-to-use shadowed spot-lighting pipeline consuming
// [MeshVertexLayout] with cameraLayout at [CameraGroup] (0) and spotLayout at
// [LocalShadowGroup] (1).
func NewSpotShadowLitPipeline(cx *core.Scope, dev *gpu.Device, cameraLayout, spotLayout *gpu.BindGroupLayout, opts LocalShadowLitPipelineOptions) (*gpu.Pipeline, error) {
	return newLocalShadowLitPipeline(cx, dev, cameraLayout, spotLayout, opts, spotShadowLitShaderWGSL, "vs_spot_lit", "fs_spot_lit", "render3d.spot_shadow_lit")
}

// NewPointShadowLitPipeline compiles [PointShadowWGSL] with [CameraWGSL] and
// builds a ready-to-use shadowed omnidirectional point-lighting pipeline
// consuming [MeshVertexLayout] with cameraLayout at [CameraGroup] (0) and
// pointLayout at [LocalShadowGroup] (1).
func NewPointShadowLitPipeline(cx *core.Scope, dev *gpu.Device, cameraLayout, pointLayout *gpu.BindGroupLayout, opts LocalShadowLitPipelineOptions) (*gpu.Pipeline, error) {
	return newLocalShadowLitPipeline(cx, dev, cameraLayout, pointLayout, opts, pointShadowLitShaderWGSL, "vs_point_lit", "fs_point_lit", "render3d.point_shadow_lit")
}

func newLocalShadowLitPipeline(
	cx *core.Scope,
	dev *gpu.Device,
	cameraLayout, shadowLayout *gpu.BindGroupLayout,
	opts LocalShadowLitPipelineOptions,
	wgslSrc, vsEntry, fsEntry, defaultLabel string,
) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if cameraLayout == nil || !cameraLayout.Valid() || shadowLayout == nil || !shadowLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid bind group layout"}
	}
	if !localShadowValidCull(opts.CullMode) {
		return nil, fmt.Errorf("%w: invalid CullMode %d", ErrInvalidLocalShadow, opts.CullMode)
	}
	mod, err := gpu.CompileWGSL(wgslSrc)
	if err != nil {
		return nil, err
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	label := opts.Label
	if label == "" {
		label = defaultLabel
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: vsEntry},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: fsEntry},
		Layouts:       []*gpu.BindGroupLayout{cameraLayout, shadowLayout},
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  opts.CullMode,
		},
		Targets:  []gpu.ColorTargetState{{Format: colorFmt, WriteMask: gpu.ColorWriteAll}},
		Retained: opts.Retained,
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

// DecodeDepth32Float decodes tightly packed little-endian float32 depth texels
// returned by [gpu.Device.ReadTexture] on a [gpu.FormatDepth32Float] texture.
func DecodeDepth32Float(raw []byte) []float32 {
	n := len(raw) / 4
	out := make([]float32, n)
	for i := range n {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return out
}

// EvaluateSpotShadow evaluates the CPU reference shadow visibility in [0, 1]
// matching sample_spot_shadow in [SpotShadowWGSL] against a row-major
// res x res Depth32Float buffer.
func EvaluateSpotShadow(u SpotShadowUniform, depth []float32, res uint32, worldPos, worldNormal math3d.Vec3) float32 {
	if res == 0 || len(depth) < int(res)*int(res) {
		return 1
	}
	n := worldNormal
	if n.LenSq() > 1e-12 {
		n = n.Norm()
	}
	biased := worldPos.Add(n.Scale(u.NormalBias))
	clip := u.ViewProj.MulVec4(biased.Vec4(1))
	if clip.W <= 1e-5 {
		return 1
	}
	invW := 1 / clip.W
	ndcX, ndcY, ndcZ := clip.X*invW, clip.Y*invW, clip.Z*invW
	uvX := ndcX*0.5 + 0.5
	uvY := 0.5 - ndcY*0.5
	if uvX < 0 || uvX > 1 || uvY < 0 || uvY > 1 || ndcZ < 0 || ndcZ > 1 {
		return 1
	}
	refDepth := localShadowClamp(ndcZ-u.DepthBias, 0, 1)
	invRes := u.InvResolution
	if invRes <= 0 {
		invRes = 1 / float32(res)
	}
	pad := 0.51 * invRes
	w, h := int(res), int(res)
	if u.FilterMode < 0.5 {
		cu := localShadowClamp(uvX, pad, 1-pad)
		cv := localShadowClamp(uvY, pad, 1-pad)
		return localShadowCompareDepth2D(depth, w, h, cu, cv, refDepth, false)
	}
	var sum float32
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			cu := localShadowClamp(uvX+float32(dx)*invRes, pad, 1-pad)
			cv := localShadowClamp(uvY+float32(dy)*invRes, pad, 1-pad)
			sum += localShadowCompareDepth2D(depth, w, h, cu, cv, refDepth, true)
		}
	}
	return sum / 9.0
}

// EvaluateSpotLighting evaluates the CPU reference shadowed spot lighting color
// matching evaluate_spot_lighting in [SpotShadowWGSL].
func EvaluateSpotLighting(u SpotShadowUniform, depth []float32, res uint32, worldPos, worldNormal, albedo math3d.Vec3) math3d.Vec3 {
	n := worldNormal.Norm()
	toLight := u.Position.Sub(worldPos)
	dist := toLight.Len()
	ambient := math3d.Vec3{X: albedo.X * u.Ambient.X, Y: albedo.Y * u.Ambient.Y, Z: albedo.Z * u.Ambient.Z}
	if dist <= 1e-4 || dist >= u.Range {
		return ambient
	}
	lDir := toLight.Scale(1 / dist)
	ndl := max(n.Dot(lDir), float32(0))
	if ndl <= 0 {
		return ambient
	}
	cosTheta := lDir.Neg().Dot(u.Direction)
	coneT := localShadowClamp((cosTheta-u.CosOuter)/max(u.CosInner-u.CosOuter, float32(1e-4)), 0, 1)
	cone := coneT * coneT * (3 - 2*coneT)
	if cone <= 0 {
		return ambient
	}
	ratio := dist / u.Range
	r2 := ratio * ratio
	win := localShadowClamp(1-r2*r2, 0, 1)
	atten := (win * win) / max(dist*dist, float32(0.01))
	vis := EvaluateSpotShadow(u, depth, res, worldPos, n)
	k := ndl * cone * atten * vis
	return math3d.Vec3{
		X: ambient.X + albedo.X*u.ColorIntensity.X*k,
		Y: ambient.Y + albedo.Y*u.ColorIntensity.Y*k,
		Z: ambient.Z + albedo.Z*u.ColorIntensity.Z*k,
	}
}

// PointShadowSelectFace projects light-to-fragment vector v onto the dominant
// cube face (0..5) and returns the face index, face-local UV in [0, 1]^2, and
// positive major-axis distance matching [PointShadowWGSL].
func PointShadowSelectFace(v math3d.Vec3) (face gpu.CubeFace, localU, localV, axisDist float32) {
	ax, ay, az := localShadowAbs(v.X), localShadowAbs(v.Y), localShadowAbs(v.Z)
	var xView, yView float32
	switch {
	case ax >= ay && ax >= az:
		if v.X >= 0 {
			face = gpu.CubeFacePosX
			xView, yView, axisDist = v.Z, v.Y, v.X
		} else {
			face = gpu.CubeFaceNegX
			xView, yView, axisDist = -v.Z, v.Y, -v.X
		}
	case ay >= ax && ay >= az:
		if v.Y >= 0 {
			face = gpu.CubeFacePosY
			xView, yView, axisDist = v.X, v.Z, v.Y
		} else {
			face = gpu.CubeFaceNegY
			xView, yView, axisDist = v.X, -v.Z, -v.Y
		}
	default:
		if v.Z >= 0 {
			face = gpu.CubeFacePosZ
			xView, yView, axisDist = -v.X, v.Y, v.Z
		} else {
			face = gpu.CubeFaceNegZ
			xView, yView, axisDist = v.X, v.Y, -v.Z
		}
	}
	if axisDist <= 1e-12 {
		return face, 0.5, 0.5, 0
	}
	localU = (xView/axisDist)*0.5 + 0.5
	localV = 0.5 - (yView/axisDist)*0.5
	return face, localU, localV, axisDist
}

// EvaluatePointShadow evaluates the CPU reference omnidirectional shadow
// visibility in [0, 1] matching sample_point_shadow in [PointShadowWGSL] against
// a row-major (3*faceRes) x (2*faceRes) Depth32Float atlas buffer.
func EvaluatePointShadow(u PointShadowUniform, atlasDepth []float32, faceRes uint32, worldPos, worldNormal math3d.Vec3) float32 {
	atlasW := int(faceRes * PointShadowAtlasCols)
	atlasH := int(faceRes * PointShadowAtlasRows)
	if faceRes == 0 || len(atlasDepth) < atlasW*atlasH {
		return 1
	}
	n := worldNormal
	if n.LenSq() > 1e-12 {
		n = n.Norm()
	}
	biased := worldPos.Add(n.Scale(u.NormalBias))
	v := biased.Sub(u.Position)
	face, localU, localV, axisDist := PointShadowSelectFace(v)
	if axisDist <= u.Near || axisDist >= u.Range {
		return 1
	}
	ndcZ := (u.Range / (u.Range - u.Near)) * (1.0 - u.Near/axisDist)
	refDepth := localShadowClamp(ndcZ-u.DepthBias, 0, 1)
	invRes := u.InvFaceResolution
	if invRes <= 0 {
		invRes = 1 / float32(faceRes)
	}
	pad := 0.51 * invRes
	col := float32(face % PointShadowAtlasCols)
	row := float32(face / PointShadowAtlasCols)

	tap := func(du, dv float32, linear bool) float32 {
		cu := localShadowClamp(localU+du, pad, 1-pad)
		cv := localShadowClamp(localV+dv, pad, 1-pad)
		au := (col + cu) / float32(PointShadowAtlasCols)
		av := (row + cv) / float32(PointShadowAtlasRows)
		return localShadowCompareDepth2D(atlasDepth, atlasW, atlasH, au, av, refDepth, linear)
	}

	if u.FilterMode < 0.5 {
		return tap(0, 0, false)
	}
	var sum float32
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			sum += tap(float32(dx)*invRes, float32(dy)*invRes, true)
		}
	}
	return sum / 9.0
}

// EvaluatePointLighting evaluates the CPU reference shadowed point lighting color
// matching evaluate_point_lighting in [PointShadowWGSL].
func EvaluatePointLighting(u PointShadowUniform, atlasDepth []float32, faceRes uint32, worldPos, worldNormal, albedo math3d.Vec3) math3d.Vec3 {
	n := worldNormal.Norm()
	toLight := u.Position.Sub(worldPos)
	dist := toLight.Len()
	ambient := math3d.Vec3{X: albedo.X * u.Ambient.X, Y: albedo.Y * u.Ambient.Y, Z: albedo.Z * u.Ambient.Z}
	if dist <= 1e-4 || dist >= u.Range {
		return ambient
	}
	lDir := toLight.Scale(1 / dist)
	ndl := max(n.Dot(lDir), float32(0))
	if ndl <= 0 {
		return ambient
	}
	ratio := dist / u.Range
	r2 := ratio * ratio
	win := localShadowClamp(1-r2*r2, 0, 1)
	atten := (win * win) / max(dist*dist, float32(0.01))
	vis := EvaluatePointShadow(u, atlasDepth, faceRes, worldPos, n)
	k := ndl * atten * vis
	return math3d.Vec3{
		X: ambient.X + albedo.X*u.ColorIntensity.X*k,
		Y: ambient.Y + albedo.Y*u.ColorIntensity.Y*k,
		Z: ambient.Z + albedo.Z*u.ColorIntensity.Z*k,
	}
}

func localShadowCompareDepth2D(depth []float32, w, h int, u, v, refDepth float32, linear bool) float32 {
	u = localShadowClamp(u, 0, 1)
	v = localShadowClamp(v, 0, 1)
	cmpTexel := func(x, y int) float32 {
		x = min(max(x, 0), w-1)
		y = min(max(y, 0), h-1)
		if refDepth <= depth[y*w+x] {
			return 1
		}
		return 0
	}
	if !linear {
		ix := int(math.Floor(float64(u * float32(w))))
		iy := int(math.Floor(float64(v * float32(h))))
		return cmpTexel(ix, iy)
	}
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

func encodeLocalShadowCaster(dst []byte, lightVP, model math3d.Mat4) {
	off := 0
	for _, m := range [...]*math3d.Mat4{&lightVP, &model} {
		for _, f := range m {
			binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
			off += 4
		}
	}
}

func localShadowValidFilter(f LocalShadowFilter) bool {
	return f == LocalShadowFilterHard || f == LocalShadowFilterPCF3x3
}

func localShadowValidCull(c gpu.CullMode) bool {
	return c == gpu.CullNone || c == gpu.CullFront || c == gpu.CullBack
}

func localShadowAbs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func localShadowClamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
