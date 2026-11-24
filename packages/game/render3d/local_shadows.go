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

// ErrInvalidLocalShadow is wrapped by every validation error returned for spot or point light shadow maps and their configurations.
var ErrInvalidLocalShadow = errors.New("render3d: invalid local shadow")

// Spot and point light shadow constants and defaults (#255).
const (
	// LocalShadowGroup is the bind group index [SpotShadowWGSL], [PointShadowWGSL], [LitPBRSpotShadowWGSL] and [LitPBRPointShadowWGSL] reserve for a spot or point shadow map (uniform buffer at binding 0, depth texture at binding 1, comparison sampler at binding 2). It is [ShadowGroup], the group after [CameraGroup] (0), [LightGroup] (1) and [PBRDrawGroup] (2), so a lit PBR pipeline takes one shadow map, fog, IBL or skin palette there. Custom shaders that need group 3 for something else, or that combine a spot and a point shadow, place the bindings elsewhere with [SpotShadowWGSLForGroup] and [PointShadowWGSLForGroup].
	LocalShadowGroup = ShadowGroup

	// PointShadowFaceCount is the number of cube faces rendered for a point light.
	PointShadowFaceCount = 6
	// PointShadowAtlasCols is the horizontal tile count of the 3x2 point shadow atlas.
	PointShadowAtlasCols = 3
	// PointShadowAtlasRows is the vertical tile count of the 3x2 point shadow atlas.
	PointShadowAtlasRows = 2

	// SpotShadowUniformSize is the byte size of [SpotShadowUniform.Encode] and the WGSL SpotShadow block: one mat4x4<f32> and one vec4<f32> (80 bytes).
	SpotShadowUniformSize = 64 + 16

	// PointShadowUniformSize is the byte size of [PointShadowUniform.Encode] and the WGSL PointShadow block: three vec4<f32> (48 bytes).
	PointShadowUniformSize = 3 * 16

	// DefaultLocalShadowNear is the default near plane distance of spot and point shadow projections.
	DefaultLocalShadowNear float32 = 0.1
	// DefaultLocalShadowFar is the default far plane distance used for a light whose Range is 0 (unbounded inverse-square falloff). Lit surfaces farther than this from the light are left unshadowed.
	DefaultLocalShadowFar float32 = 50

	// DefaultLocalShadowMaxCasters is the default number of caster draws per spot shadow pass, and per cube face of a point shadow, when MaxCasters is zero.
	DefaultLocalShadowMaxCasters uint32 = 256
	// MaxLocalShadowCasters is the largest MaxCasters [SpotShadowConfig.Validate] and [PointShadowConfig.Validate] accept. Each caster takes a 256-byte uniform slot, so a spot shadow at this limit holds a 1 MiB caster buffer and a point shadow six times that.
	MaxLocalShadowCasters uint32 = 4096

	// MaxSpotShadowConeAngle is the widest spot light OuterConeAngle (in radians, just under 90 degrees) a [SpotShadowMap] can project: its perspective field of view is twice the cone angle. Use a [PointShadowMap] for wider cones.
	MaxSpotShadowConeAngle float32 = math.Pi/2 - 0.01

	localShadowCasterSlotStride = 256 // WebGPU / Metal / D3D11 MinUniformBufferAlignment
	localShadowCasterDataSize   = 128 // light_view_proj (64) + model (64)

	defaultSpotShadowResolution  = 1024
	defaultPointShadowResolution = 512
	defaultLocalShadowDepthBias  = 0.0015
	defaultLocalShadowNormalBias = 0.02
)

// LocalShadowFilter selects how a local shadow map samples its depth texture.
type LocalShadowFilter uint8

const (
	// LocalShadowFilterHard performs a single nearest-neighbor depth comparison tap.
	LocalShadowFilterHard LocalShadowFilter = iota
	// LocalShadowFilterPCF3x3 performs a 3x3 percentage-closer filter using linear comparison taps clamped inside the shadow map or cube face tile.
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

// localShadowDepthWGSL renders shadow caster geometry into a Depth32Float shadow map or atlas tile without a fragment stage.
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

// spotShadowStructWGSL declares the uniform block shared by [SpotShadowWGSL] and [SpotShadowWGSLForGroup].
const spotShadowStructWGSL = `struct SpotShadow {
	view_proj: mat4x4<f32>,
	params: vec4<f32>,
}
`

// spotShadowSampleFnWGSL is the sample_spot_shadow helper shared by [SpotShadowWGSL] and [SpotShadowWGSLForGroup].
const spotShadowSampleFnWGSL = `
fn sample_spot_shadow(world_pos: vec3<f32>, world_normal: vec3<f32>) -> f32 {
	var n = vec3<f32>(0.0, 0.0, 0.0);
	let n_len = length(world_normal);
	if (n_len > 0.000001) {
		n = world_normal / n_len;
	}
	let biased_pos = world_pos + n * spot_shadow.params.y;
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
	for (var dy: i32 = -1; dy <= 1; dy = dy + 1) {
		for (var dx: i32 = -1; dx <= 1; dx = dx + 1) {
			let off = vec2<f32>(f32(dx) * inv_res, f32(dy) * inv_res);
			sum = sum + textureSampleCompareLevel(spot_shadow_map, spot_shadow_sampler, clamp(uv + off, uv_min, uv_max), ref_depth);
		}
	}
	return sum / 9.0;
}
`

// SpotShadowWGSL declares the spot shadow uniform block, depth map and comparison sampler at [LocalShadowGroup] (@group(3)) and the portable sample_spot_shadow(world_pos, world_normal) helper returning the spot light's visibility in [0, 1]. It equals SpotShadowWGSLForGroup(LocalShadowGroup). [LitPBRSpotShadowWGSL] is the ready-made PBR shader built on it; a custom lit shader multiplies its spot light's contribution by the helper's result, for example through shade_pbr_visibility in [LightWGSL]. It uses only textureSampleCompareLevel, so it runs on every NSGo backend, the WebGL 2 / GLES 3 profile and the software renderer included.
const SpotShadowWGSL = spotShadowStructWGSL + `
@group(3) @binding(0) var<uniform> spot_shadow: SpotShadow;
@group(3) @binding(1) var spot_shadow_map: texture_depth_2d;
@group(3) @binding(2) var spot_shadow_sampler: sampler_comparison;
` + spotShadowSampleFnWGSL

// SpotShadowWGSLForGroup returns [SpotShadowWGSL] with its bindings placed at @group(group) instead of [LocalShadowGroup].
func SpotShadowWGSLForGroup(group uint32) string {
	return fmt.Sprintf(`%s
@group(%[2]d) @binding(0) var<uniform> spot_shadow: SpotShadow;
@group(%[2]d) @binding(1) var spot_shadow_map: texture_depth_2d;
@group(%[2]d) @binding(2) var spot_shadow_sampler: sampler_comparison;
%[3]s`, spotShadowStructWGSL, group, spotShadowSampleFnWGSL)
}

// pointShadowStructWGSL declares the uniform block shared by [PointShadowWGSL] and [PointShadowWGSLForGroup].
const pointShadowStructWGSL = `struct PointShadow {
	position_far: vec4<f32>,
	params: vec4<f32>,
	near_pad: vec4<f32>,
}
`

// pointShadowSampleFnWGSL holds the point_shadow_tile_tap and sample_point_shadow helpers shared by [PointShadowWGSL] and [PointShadowWGSLForGroup].
const pointShadowSampleFnWGSL = `
fn point_shadow_tile_tap(col: f32, row: f32, local_uv: vec2<f32>, offset: vec2<f32>, pad: f32, ref_depth: f32) -> f32 {
	let clamped = clamp(local_uv + offset, vec2<f32>(pad, pad), vec2<f32>(1.0 - pad, 1.0 - pad));
	let atlas_uv = vec2<f32>((col + clamped.x) / 3.0, (row + clamped.y) / 2.0);
	return textureSampleCompareLevel(point_shadow_map, point_shadow_sampler, atlas_uv, ref_depth);
}

fn sample_point_shadow(world_pos: vec3<f32>, world_normal: vec3<f32>) -> f32 {
	var n = vec3<f32>(0.0, 0.0, 0.0);
	let n_len = length(world_normal);
	if (n_len > 0.000001) {
		n = world_normal / n_len;
	}
	let biased_pos = world_pos + n * point_shadow.params.y;
	let v = biased_pos - point_shadow.position_far.xyz;
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
	let near_p = point_shadow.near_pad.x;
	let far_p = point_shadow.position_far.w;
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
	for (var dy: i32 = -1; dy <= 1; dy = dy + 1) {
		for (var dx: i32 = -1; dx <= 1; dx = dx + 1) {
			sum = sum + point_shadow_tile_tap(col, row, local_uv, vec2<f32>(f32(dx) * inv_res, f32(dy) * inv_res), pad, ref_depth);
		}
	}
	return sum / 9.0;
}
`

// PointShadowWGSL declares the point shadow uniform block, 3x2 depth atlas and comparison sampler at [LocalShadowGroup] (@group(3)) and the portable sample_point_shadow(world_pos, world_normal) helper returning the point light's visibility in [0, 1]. It equals PointShadowWGSLForGroup(LocalShadowGroup). All six cube faces live in one texture_depth_2d atlas with per-tile UV clamping, so sampling and 3x3 PCF work on every NSGo backend without depth cube maps. [LitPBRPointShadowWGSL] is the ready-made PBR shader built on it.
const PointShadowWGSL = pointShadowStructWGSL + `
@group(3) @binding(0) var<uniform> point_shadow: PointShadow;
@group(3) @binding(1) var point_shadow_map: texture_depth_2d;
@group(3) @binding(2) var point_shadow_sampler: sampler_comparison;
` + pointShadowSampleFnWGSL

// PointShadowWGSLForGroup returns [PointShadowWGSL] with its bindings placed at @group(group) instead of [LocalShadowGroup].
func PointShadowWGSLForGroup(group uint32) string {
	return fmt.Sprintf(`%s
@group(%[2]d) @binding(0) var<uniform> point_shadow: PointShadow;
@group(%[2]d) @binding(1) var point_shadow_map: texture_depth_2d;
@group(%[2]d) @binding(2) var point_shadow_sampler: sampler_comparison;
%[3]s`, pointShadowStructWGSL, group, pointShadowSampleFnWGSL)
}

// LitPBRSpotShadowWGSL is [LitPBRWGSL] with a spot light shadow: the same vertex stage (per-draw model and normal matrices from [PBRDrawUniform]), bind groups [CameraGroup], [LightGroup] and [PBRDrawGroup], and lighting, plus [SpotShadowWGSL] at [LocalShadowGroup]. The fragment stage multiplies the first spot light of the [LightRig] (lights.spot[0]) by sample_spot_shadow; every other light, the ambient term and emission are unshadowed. [ShadePointVisibility] with Spot0 set is its CPU reference. Use [NewLitPBRSpotShadowPipeline] or [LitPBRPipelineOptions].SpotShadowLayout to create a pipeline from it.
const LitPBRSpotShadowWGSL = litPBRPreludeWGSL + SpotShadowWGSL + litPBRSpotShadowFragmentHeadWGSL + `	return vec4<f32>(linear_to_srgb(lit), pbr_draw.base_color.a);
}
`

// litPBRSpotShadowLinearWGSL is [LitPBRSpotShadowWGSL] writing the lit linear color without sRGB encoding.
const litPBRSpotShadowLinearWGSL = litPBRPreludeWGSL + SpotShadowWGSL + litPBRSpotShadowFragmentHeadWGSL + `	return vec4<f32>(lit, pbr_draw.base_color.a);
}
`

const litPBRSpotShadowFragmentHeadWGSL = `
@fragment
fn fs_main(in: PBRVertexOut) -> @location(0) vec4<f32> {
	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let spot_vis = sample_spot_shadow(in.world_pos, in.world_normal);
	let lit = shade_pbr_visibility(
		in.world_pos,
		in.world_normal,
		view_dir,
		pbr_draw.base_color.rgb,
		pbr_draw.pbr_params.x,
		pbr_draw.pbr_params.y,
		1.0,
		1.0,
		spot_vis,
	) + pbr_draw.emissive.rgb;
`

// LitPBRPointShadowWGSL is [LitPBRWGSL] with a point light shadow: the same vertex stage, bind groups and lighting, plus [PointShadowWGSL] at [LocalShadowGroup]. The fragment stage multiplies the first point light of the [LightRig] (lights.point[0]) by sample_point_shadow; every other light, the ambient term and emission are unshadowed. [ShadePointVisibility] with Point0 set is its CPU reference. Use [NewLitPBRPointShadowPipeline] or [LitPBRPipelineOptions].PointShadowLayout to create a pipeline from it.
const LitPBRPointShadowWGSL = litPBRPreludeWGSL + PointShadowWGSL + litPBRPointShadowFragmentHeadWGSL + `	return vec4<f32>(linear_to_srgb(lit), pbr_draw.base_color.a);
}
`

// litPBRPointShadowLinearWGSL is [LitPBRPointShadowWGSL] writing the lit linear color without sRGB encoding.
const litPBRPointShadowLinearWGSL = litPBRPreludeWGSL + PointShadowWGSL + litPBRPointShadowFragmentHeadWGSL + `	return vec4<f32>(lit, pbr_draw.base_color.a);
}
`

const litPBRPointShadowFragmentHeadWGSL = `
@fragment
fn fs_main(in: PBRVertexOut) -> @location(0) vec4<f32> {
	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let point_vis = sample_point_shadow(in.world_pos, in.world_normal);
	let lit = shade_pbr_visibility(
		in.world_pos,
		in.world_normal,
		view_dir,
		pbr_draw.base_color.rgb,
		pbr_draw.pbr_params.x,
		pbr_draw.pbr_params.y,
		1.0,
		point_vis,
		1.0,
	) + pbr_draw.emissive.rgb;
`

// NewLitPBRSpotShadowPipeline compiles [LitPBRSpotShadowWGSL] and creates a pipeline configured like [NewLitPBRPipeline] with spotLayout ([SpotShadowMap.Layout]) added at [LocalShadowGroup].
func NewLitPBRSpotShadowPipeline(cx *core.Scope, dev *gpu.Device, camLayout, lightLayout, drawLayout, spotLayout *gpu.BindGroupLayout, depthFormat gpu.TextureFormat) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if spotLayout == nil {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: NewLitPBRSpotShadowPipeline requires a spot shadow bind group layout"}
	}
	return NewLitPBRPipelineWithOptions(cx, dev, camLayout, lightLayout, drawLayout, LitPBRPipelineOptions{SpotShadowLayout: spotLayout, DepthFormat: depthFormat})
}

// NewLitPBRPointShadowPipeline compiles [LitPBRPointShadowWGSL] and creates a pipeline configured like [NewLitPBRPipeline] with pointLayout ([PointShadowMap.Layout]) added at [LocalShadowGroup].
func NewLitPBRPointShadowPipeline(cx *core.Scope, dev *gpu.Device, camLayout, lightLayout, drawLayout, pointLayout *gpu.BindGroupLayout, depthFormat gpu.TextureFormat) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if pointLayout == nil {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: NewLitPBRPointShadowPipeline requires a point shadow bind group layout"}
	}
	return NewLitPBRPipelineWithOptions(cx, dev, camLayout, lightLayout, drawLayout, LitPBRPipelineOptions{PointShadowLayout: pointLayout, DepthFormat: depthFormat})
}

// localShadowPlanes validates the near and far planes both configs carry.
func localShadowPlanes(kind string, near, far float32) error {
	if !finite(near) || near <= 0 {
		return fmt.Errorf("%w: %s Near %v must be finite and positive", ErrInvalidLocalShadow, kind, near)
	}
	if !finite(far) || far <= near {
		return fmt.Errorf("%w: %s Far %v must be finite and greater than Near %v", ErrInvalidLocalShadow, kind, far, near)
	}
	return nil
}

func validateLocalShadowCommon(kind string, depthBias, normalBias float32, filter LocalShadowFilter, cull gpu.CullMode, maxCasters uint32) error {
	if !finite(depthBias) || depthBias < 0 {
		return fmt.Errorf("%w: %s DepthBias %v must be finite and non-negative", ErrInvalidLocalShadow, kind, depthBias)
	}
	if !finite(normalBias) || normalBias < 0 {
		return fmt.Errorf("%w: %s NormalBias %v must be finite and non-negative", ErrInvalidLocalShadow, kind, normalBias)
	}
	if !localShadowValidFilter(filter) {
		return fmt.Errorf("%w: invalid %s Filter %d", ErrInvalidLocalShadow, kind, filter)
	}
	if !localShadowValidCull(cull) {
		return fmt.Errorf("%w: invalid %s CullMode %d", ErrInvalidLocalShadow, kind, cull)
	}
	if maxCasters > MaxLocalShadowCasters {
		return fmt.Errorf("%w: %s MaxCasters %d exceeds MaxLocalShadowCasters (%d)", ErrInvalidLocalShadow, kind, maxCasters, MaxLocalShadowCasters)
	}
	return nil
}

// localShadowFar returns the far plane for a light with the given Range: the Range itself when it bounds the light, otherwise the configured far.
func localShadowFar(kind string, lightRange, near, cfgFar float32) (float32, error) {
	far := cfgFar
	if lightRange > 0 {
		far = lightRange
	}
	if far <= near {
		return 0, fmt.Errorf("%w: %s light Range %v must be greater than the shadow Near %v", ErrInvalidLocalShadow, kind, far, near)
	}
	return far, nil
}

func localShadowFilterMode(f LocalShadowFilter) float32 {
	if f == LocalShadowFilterPCF3x3 {
		return 1
	}
	return 0
}

// SpotShadowConfig configures a perspective 2D [SpotShadowMap].
type SpotShadowConfig struct {
	// Resolution is the width and height of the square depth shadow map in texels. Zero defaults to 1024.
	Resolution uint32
	// Near is the near plane distance of the shadow projection. Zero defaults to [DefaultLocalShadowNear].
	Near float32
	// Far is the far plane distance used when the spot light's Range is 0; a light with a Range uses that instead. Zero defaults to [DefaultLocalShadowFar].
	Far float32
	// DepthBias subtracts a constant NDC [0, 1] bias during depth comparison to avoid shadow acne. [DefaultSpotShadowConfig] sets 0.0015; zero disables it.
	DepthBias float32
	// NormalBias offsets the shaded world position along its surface normal before projecting into light space.
	NormalBias float32
	// Filter selects hard single-tap or 3x3 PCF comparison sampling.
	Filter LocalShadowFilter
	// CullMode sets face culling for the shadow caster pass (default gpu.CullNone).
	CullMode gpu.CullMode
	// MaxCasters is the maximum number of [SpotShadowMap.DrawCaster] calls per shadow pass, at most [MaxLocalShadowCasters]. Zero defaults to [DefaultLocalShadowMaxCasters].
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
		Near:       DefaultLocalShadowNear,
		Far:        DefaultLocalShadowFar,
		DepthBias:  defaultLocalShadowDepthBias,
		NormalBias: defaultLocalShadowNormalBias,
		Filter:     LocalShadowFilterPCF3x3,
		CullMode:   gpu.CullNone,
		MaxCasters: DefaultLocalShadowMaxCasters,
	}
}

// Normalized returns a copy of c with zero Resolution, Near, Far and MaxCasters filled with defaults.
func (c SpotShadowConfig) Normalized() SpotShadowConfig {
	if c.Resolution == 0 {
		c.Resolution = defaultSpotShadowResolution
	}
	if c.Near == 0 {
		c.Near = DefaultLocalShadowNear
	}
	if c.Far == 0 {
		c.Far = DefaultLocalShadowFar
	}
	if c.MaxCasters == 0 {
		c.MaxCasters = DefaultLocalShadowMaxCasters
	}
	return c
}

// Validate checks that c's planes, biases, filter, cull mode and caster count are valid. Every error wraps [ErrInvalidLocalShadow].
func (c SpotShadowConfig) Validate() error {
	n := c.Normalized()
	if err := localShadowPlanes("spot", n.Near, n.Far); err != nil {
		return err
	}
	return validateLocalShadowCommon("spot", n.DepthBias, n.NormalBias, n.Filter, n.CullMode, n.MaxCasters)
}

// SpotShadowUniform is the packed uniform data matching the WGSL SpotShadow block declared by [SpotShadowWGSL].
type SpotShadowUniform struct {
	// ViewProj is the spot light's [0, 1]-depth perspective view-projection matrix.
	ViewProj      math3d.Mat4
	DepthBias     float32
	NormalBias    float32
	InvResolution float32
	// FilterMode is 0 for [LocalShadowFilterHard] and 1 for [LocalShadowFilterPCF3x3].
	FilterMode float32
}

// SpotShadowViewProj returns the [0, 1]-depth perspective view-projection matrix a [SpotShadowMap] renders light with: looking from light.Position along light.Direction with a square field of view of 2 * light.OuterConeAngle, from near to far.
func SpotShadowViewProj(light SpotLight, near, far float32) math3d.Mat4 {
	dir, ok := spotUnitDirection(light)
	if !ok {
		dir = math3d.Vec3{Y: -1}
	}
	up := math3d.Vec3Up()
	if localShadowAbs(dir.Y) > 0.99 {
		up = math3d.Vec3{Z: 1}
	}
	view := math3d.LookAt(light.Position, light.Position.Add(dir), up)
	return math3d.Perspective(2*light.OuterConeAngle, 1.0, near, far).Mul(view)
}

// NewSpotShadowUniform computes the shadow uniform data for the [LightRig] spot light light under cfg. The projection's far plane is light.Range, or cfg.Far when Range is 0. It returns an error wrapping [ErrInvalidLight] when light is invalid, and one wrapping [ErrInvalidLocalShadow] when cfg is invalid, the cone is wider than [MaxSpotShadowConeAngle], or the far plane is not beyond the near plane.
func NewSpotShadowUniform(light SpotLight, cfg SpotShadowConfig) (SpotShadowUniform, error) {
	if err := light.Validate(); err != nil {
		return SpotShadowUniform{}, err
	}
	if err := cfg.Validate(); err != nil {
		return SpotShadowUniform{}, err
	}
	if light.OuterConeAngle > MaxSpotShadowConeAngle {
		return SpotShadowUniform{}, fmt.Errorf("%w: spot OuterConeAngle %v exceeds MaxSpotShadowConeAngle %v", ErrInvalidLocalShadow, light.OuterConeAngle, MaxSpotShadowConeAngle)
	}
	n := cfg.Normalized()
	far, err := localShadowFar("spot", light.Range, n.Near, n.Far)
	if err != nil {
		return SpotShadowUniform{}, err
	}
	return SpotShadowUniform{
		ViewProj:      SpotShadowViewProj(light, n.Near, far),
		DepthBias:     n.DepthBias,
		NormalBias:    n.NormalBias,
		InvResolution: 1.0 / float32(n.Resolution),
		FilterMode:    localShadowFilterMode(n.Filter),
	}, nil
}

// Encode writes u into dst in the little-endian layout declared by [SpotShadowWGSL] and returns dst[:SpotShadowUniformSize]. It allocates nothing and panics if len(dst) < SpotShadowUniformSize.
func (u *SpotShadowUniform) Encode(dst []byte) []byte {
	if len(dst) < SpotShadowUniformSize {
		panic("render3d: SpotShadowUniform.Encode: dst shorter than SpotShadowUniformSize")
	}
	dst = dst[:SpotShadowUniformSize]
	for i, f := range u.ViewProj {
		binary.LittleEndian.PutUint32(dst[4*i:], math.Float32bits(f))
	}
	putVec4(dst[64:], math3d.Vec4{X: u.DepthBias, Y: u.NormalBias, Z: u.InvResolution, W: u.FilterMode})
	return dst
}

// PointShadowConfig configures an omnidirectional 3x2 depth-atlas [PointShadowMap].
type PointShadowConfig struct {
	// FaceResolution is the width and height of one cube face tile in texels. The 3x2 depth atlas has size (3 * FaceResolution) x (2 * FaceResolution). Zero defaults to 512.
	FaceResolution uint32
	// Near is the near plane distance of the six face projections. Zero defaults to [DefaultLocalShadowNear].
	Near float32
	// Far is the far plane distance used when the point light's Range is 0; a light with a Range uses that instead. Zero defaults to [DefaultLocalShadowFar].
	Far float32
	// DepthBias subtracts a constant NDC [0, 1] bias during depth comparison.
	DepthBias float32
	// NormalBias offsets the shaded world position along its surface normal before selecting the cube face and comparing depth.
	NormalBias float32
	// Filter selects hard single-tap or 3x3 PCF comparison sampling.
	Filter LocalShadowFilter
	// CullMode sets face culling for the shadow caster passes (default gpu.CullNone).
	CullMode gpu.CullMode
	// MaxCasters is the maximum number of [PointShadowMap.DrawCaster] calls per cube face pass, at most [MaxLocalShadowCasters]. Each face has its own slots, so faces can be recorded in any order or skipped. Zero defaults to [DefaultLocalShadowMaxCasters].
	MaxCasters uint32
	// Label names GPU resources in capture tools.
	Label string
	// Retained keeps resources across GPU device loss and restore.
	Retained bool
}

// DefaultPointShadowConfig returns a ready-to-use 512x512-per-face PCF 3x3 point shadow configuration.
func DefaultPointShadowConfig() PointShadowConfig {
	return PointShadowConfig{
		FaceResolution: defaultPointShadowResolution,
		Near:           DefaultLocalShadowNear,
		Far:            DefaultLocalShadowFar,
		DepthBias:      defaultLocalShadowDepthBias,
		NormalBias:     defaultLocalShadowNormalBias,
		Filter:         LocalShadowFilterPCF3x3,
		CullMode:       gpu.CullNone,
		MaxCasters:     DefaultLocalShadowMaxCasters,
	}
}

// Normalized returns a copy of c with zero FaceResolution, Near, Far and MaxCasters filled with defaults.
func (c PointShadowConfig) Normalized() PointShadowConfig {
	if c.FaceResolution == 0 {
		c.FaceResolution = defaultPointShadowResolution
	}
	if c.Near == 0 {
		c.Near = DefaultLocalShadowNear
	}
	if c.Far == 0 {
		c.Far = DefaultLocalShadowFar
	}
	if c.MaxCasters == 0 {
		c.MaxCasters = DefaultLocalShadowMaxCasters
	}
	return c
}

// AtlasSize returns the width and height in texels of the 3x2 2D depth atlas.
func (c PointShadowConfig) AtlasSize() (width, height uint32) {
	n := c.Normalized()
	return n.FaceResolution * PointShadowAtlasCols, n.FaceResolution * PointShadowAtlasRows
}

// Validate checks that c's resolution, planes, biases, filter, cull mode and caster count are valid. Every error wraps [ErrInvalidLocalShadow].
func (c PointShadowConfig) Validate() error {
	n := c.Normalized()
	if uint64(n.FaceResolution)*PointShadowAtlasCols > math.MaxUint32 {
		return fmt.Errorf("%w: point FaceResolution %d overflows atlas dimensions", ErrInvalidLocalShadow, n.FaceResolution)
	}
	if err := localShadowPlanes("point", n.Near, n.Far); err != nil {
		return err
	}
	return validateLocalShadowCommon("point", n.DepthBias, n.NormalBias, n.Filter, n.CullMode, n.MaxCasters)
}

// PointShadowUniform is the packed uniform data matching the WGSL PointShadow block declared by [PointShadowWGSL].
type PointShadowUniform struct {
	Position          math3d.Vec3
	Near              float32
	Far               float32
	DepthBias         float32
	NormalBias        float32
	InvFaceResolution float32
	// FilterMode is 0 for [LocalShadowFilterHard] and 1 for [LocalShadowFilterPCF3x3].
	FilterMode float32
}

// NewPointShadowUniform computes the shadow uniform data for the [LightRig] point light light under cfg. The far plane is light.Range, or cfg.Far when Range is 0. It returns an error wrapping [ErrInvalidLight] when light is invalid, and one wrapping [ErrInvalidLocalShadow] when cfg is invalid or the far plane is not beyond the near plane.
func NewPointShadowUniform(light PointLight, cfg PointShadowConfig) (PointShadowUniform, error) {
	if err := light.Validate(); err != nil {
		return PointShadowUniform{}, err
	}
	if err := cfg.Validate(); err != nil {
		return PointShadowUniform{}, err
	}
	n := cfg.Normalized()
	far, err := localShadowFar("point", light.Range, n.Near, n.Far)
	if err != nil {
		return PointShadowUniform{}, err
	}
	return PointShadowUniform{
		Position:          light.Position,
		Near:              n.Near,
		Far:               far,
		DepthBias:         n.DepthBias,
		NormalBias:        n.NormalBias,
		InvFaceResolution: 1.0 / float32(n.FaceResolution),
		FilterMode:        localShadowFilterMode(n.Filter),
	}, nil
}

// pointFaceBasis holds the canonical right-handed forward and up vectors for cube faces 0..5 matching [gpu.CubeFacePosX]..[gpu.CubeFaceNegZ] and [PointShadowWGSL].
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

// FaceViewProj returns the 90-degree [0, 1]-depth perspective view-projection matrix of cube face (0..5) that [PointShadowMap] renders with. Out-of-range faces clamp to [gpu.CubeFacePosX].
func (u PointShadowUniform) FaceViewProj(face gpu.CubeFace) math3d.Mat4 {
	if face >= PointShadowFaceCount {
		face = gpu.CubeFacePosX
	}
	b := pointFaceBasis[face]
	view := math3d.LookAt(u.Position, u.Position.Add(b.forward), b.up)
	return math3d.Perspective(math.Pi/2, 1.0, u.Near, u.Far).Mul(view)
}

// Encode writes u into dst in the little-endian layout declared by [PointShadowWGSL] and returns dst[:PointShadowUniformSize]. It allocates nothing and panics if len(dst) < PointShadowUniformSize.
func (u *PointShadowUniform) Encode(dst []byte) []byte {
	if len(dst) < PointShadowUniformSize {
		panic("render3d: PointShadowUniform.Encode: dst shorter than PointShadowUniformSize")
	}
	dst = dst[:PointShadowUniformSize]
	putVec4(dst[0:], math3d.Vec4{X: u.Position.X, Y: u.Position.Y, Z: u.Position.Z, W: u.Far})
	putVec4(dst[16:], math3d.Vec4{X: u.DepthBias, Y: u.NormalBias, Z: u.InvFaceResolution, W: u.FilterMode})
	putVec4(dst[32:], math3d.Vec4{X: u.Near})
	return dst
}

// localShadowResources holds the GPU objects a spot or point shadow map owns.
type localShadowResources struct {
	rt           *gpu.RenderTarget
	sampler      *gpu.Sampler
	uniformBuf   *gpu.Buffer
	casterBuf    *gpu.Buffer
	layout       *gpu.BindGroupLayout
	bindGroup    *gpu.BindGroup
	casterLayout *gpu.BindGroupLayout
	casterGroup  *gpu.BindGroup
	depthPipe    *gpu.Pipeline
}

// newLocalShadowResources creates a width x height Depth32Float target, its comparison sampler and sampling bind group, a uniformSize uniform buffer, a casterSlots-slot dynamic-offset caster buffer and the depth-only caster pipeline.
func newLocalShadowResources(cx *core.Scope, dev *gpu.Device, label string, width, height uint32, uniformSize, casterSlots uint64, filter LocalShadowFilter, cull gpu.CullMode, retained bool) (localShadowResources, error) {
	var r localShadowResources
	casterBufSize := casterSlots * localShadowCasterSlotStride
	if maxBuf := dev.Limits().MaxBufferSize; maxBuf > 0 && casterBufSize > maxBuf {
		return r, fmt.Errorf("%w: %s caster buffer of %d bytes exceeds MaxBufferSize %d; lower MaxCasters", ErrInvalidLocalShadow, label, casterBufSize, maxBuf)
	}
	var err error
	fail := func(e error) (localShadowResources, error) {
		_ = r.close()
		return localShadowResources{}, e
	}
	if r.rt, err = dev.CreateRenderTarget(cx, gpu.RenderTargetDesc{
		Label:       label + ".depth",
		Width:       width,
		Height:      height,
		DepthFormat: gpu.FormatDepth32Float,
		Retained:    retained,
	}); err != nil {
		return fail(err)
	}
	filterMode := gpu.FilterNearest
	if filter == LocalShadowFilterPCF3x3 {
		filterMode = gpu.FilterLinear
	}
	if r.sampler, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:     label + ".sampler",
		MinFilter: filterMode,
		MagFilter: filterMode,
		AddressU:  gpu.AddressClampToEdge,
		AddressV:  gpu.AddressClampToEdge,
		AddressW:  gpu.AddressClampToEdge,
		Compare:   gpu.CompareLessEqual,
		Retained:  retained,
	}); err != nil {
		return fail(err)
	}
	if r.uniformBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".uniform",
		Size:     uniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: retained,
	}); err != nil {
		return fail(err)
	}
	if r.casterBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".casters",
		Size:     casterBufSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: retained,
	}); err != nil {
		return fail(err)
	}
	if r.layout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingComparisonSampler},
		},
	}); err != nil {
		return fail(err)
	}
	if r.bindGroup, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + ".bind_group",
		Layout: r.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: r.uniformBuf, Size: uniformSize},
			{Binding: 1, Texture: r.rt.Depth},
			{Binding: 2, Sampler: r.sampler},
		},
	}); err != nil {
		return fail(err)
	}
	if r.casterLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".caster_layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex, Type: gpu.BindingUniform, HasDynamicOffset: true},
		},
	}); err != nil {
		return fail(err)
	}
	if r.casterGroup, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + ".caster_group",
		Layout: r.casterLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: r.casterBuf, Offset: 0, Size: localShadowCasterDataSize},
		},
	}); err != nil {
		return fail(err)
	}
	depthMod, err := gpu.CompileWGSL(localShadowDepthWGSL)
	if err != nil {
		return fail(err)
	}
	if r.depthPipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
		Label:         label + ".depth_pipe",
		Vertex:        gpu.ShaderStage{Module: depthMod, EntryPoint: "vs_local_shadow_depth"},
		Layouts:       []*gpu.BindGroupLayout{r.casterLayout},
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  cull,
		},
		DepthStencil: &gpu.DepthStencilState{
			Format:       gpu.FormatDepth32Float,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		},
		Retained: retained,
	}); err != nil {
		return fail(err)
	}
	return r, nil
}

func (r *localShadowResources) valid() bool {
	return r.rt != nil && r.rt.Valid() &&
		r.sampler != nil && r.sampler.Valid() &&
		r.uniformBuf != nil && r.uniformBuf.Valid() &&
		r.casterBuf != nil && r.casterBuf.Valid() &&
		r.layout != nil && r.layout.Valid() &&
		r.bindGroup != nil && r.bindGroup.Valid() &&
		r.casterLayout != nil && r.casterLayout.Valid() &&
		r.casterGroup != nil && r.casterGroup.Valid() &&
		r.depthPipe != nil && r.depthPipe.Valid()
}

func (r *localShadowResources) close() error {
	var err error
	closeOne := func(c interface{ Close() error }) {
		err = errors.Join(err, c.Close())
	}
	if r.depthPipe != nil {
		closeOne(r.depthPipe)
	}
	if r.casterGroup != nil {
		closeOne(r.casterGroup)
	}
	if r.casterLayout != nil {
		closeOne(r.casterLayout)
	}
	if r.bindGroup != nil {
		closeOne(r.bindGroup)
	}
	if r.layout != nil {
		closeOne(r.layout)
	}
	if r.casterBuf != nil {
		closeOne(r.casterBuf)
	}
	if r.uniformBuf != nil {
		closeOne(r.uniformBuf)
	}
	if r.sampler != nil {
		closeOne(r.sampler)
	}
	if r.rt != nil {
		closeOne(r.rt)
	}
	return err
}

// drawCaster writes lightVP and model into caster slot and records mesh with that slot bound.
func (r *localShadowResources) drawCaster(dev *gpu.Device, pass *gpu.RenderPass, scratch []byte, slot uint32, lightVP, model math3d.Mat4, mesh *GPUMesh) {
	offset := uint64(slot) * localShadowCasterSlotStride
	encodeLocalShadowCaster(scratch, lightVP, model)
	dev.WriteBuffer(r.casterBuf, offset, scratch)
	pass.SetBindGroup(0, r.casterGroup, uint32(offset))
	mesh.Draw(pass)
}

// SpotShadowMap manages a depth-only perspective 2D shadow map, its depth caster pipeline and its sampling bind group for the first spot light of a [LightRig].
//
// Create it once when the GPU device is ready (Retained: true survives device loss), call [SpotShadowMap.Update] with the rig's spot light, record casters between [SpotShadowMap.BeginShadowPass] and pass.End(), and bind [SpotShadowMap.BindGroup] at [LocalShadowGroup] when drawing with [NewLitPBRSpotShadowPipeline]. Its resources belong to the [*core.Scope] passed to [NewSpotShadowMap].
type SpotShadowMap struct {
	cfg           SpotShadowConfig
	res           localShadowResources
	uniforms      SpotShadowUniform
	updated       bool
	passDev       *gpu.Device
	casterSlot    uint32
	uniformBytes  [SpotShadowUniformSize]byte
	casterScratch [localShadowCasterDataSize]byte
}

// NewSpotShadowMap creates a depth-only perspective spot shadow map and its GPU resources scoped to cx. Invalid configuration, a resolution above the device's MaxTextureDimension2D, or a caster buffer above its MaxBufferSize returns an error wrapping [ErrInvalidLocalShadow]; device errors are returned as [gpu.Error].
func NewSpotShadowMap(cx *core.Scope, dev *gpu.Device, cfg SpotShadowConfig) (*SpotShadowMap, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	n := cfg.Normalized()
	if maxDim := dev.Limits().MaxTextureDimension2D; maxDim > 0 && n.Resolution > maxDim {
		return nil, fmt.Errorf("%w: spot resolution %d exceeds MaxTextureDimension2D %d", ErrInvalidLocalShadow, n.Resolution, maxDim)
	}
	if n.Label == "" {
		n.Label = "render3d.spot_shadow"
	}
	res, err := newLocalShadowResources(cx, dev, n.Label, n.Resolution, n.Resolution, SpotShadowUniformSize, uint64(n.MaxCasters), n.Filter, n.CullMode, n.Retained)
	if err != nil {
		return nil, err
	}
	return &SpotShadowMap{cfg: n, res: res}, nil
}

// Update computes the shadow projection for the [LightRig] spot light light (see [NewSpotShadowUniform]) and writes the uniform buffer on dev without heap allocations.
func (s *SpotShadowMap) Update(dev *gpu.Device, light SpotLight) error {
	if s == nil || !s.Valid() {
		return fmt.Errorf("%w: spot shadow map is not valid", ErrInvalidLocalShadow)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	u, err := NewSpotShadowUniform(light, s.cfg)
	if err != nil {
		return err
	}
	s.uniforms = u
	s.updated = true
	dev.WriteBuffer(s.res.uniformBuf, 0, s.uniforms.Encode(s.uniformBytes[:]))
	return nil
}

// Uniforms returns the last spot shadow uniform data written via [SpotShadowMap.Update].
func (s *SpotShadowMap) Uniforms() SpotShadowUniform {
	if s == nil {
		return SpotShadowUniform{}
	}
	return s.uniforms
}

// Config returns the normalized [SpotShadowConfig].
func (s *SpotShadowMap) Config() SpotShadowConfig {
	if s == nil {
		return SpotShadowConfig{}
	}
	return s.cfg
}

// MaxCasters returns the number of [SpotShadowMap.DrawCaster] calls one shadow pass accepts.
func (s *SpotShadowMap) MaxCasters() uint32 {
	if s == nil {
		return 0
	}
	return s.cfg.MaxCasters
}

// RenderTarget returns the underlying depth-only [gpu.RenderTarget].
func (s *SpotShadowMap) RenderTarget() *gpu.RenderTarget {
	if s == nil {
		return nil
	}
	return s.res.rt
}

// DepthTexture returns the underlying Depth32Float [gpu.Texture].
func (s *SpotShadowMap) DepthTexture() *gpu.Texture {
	if s == nil || s.res.rt == nil {
		return nil
	}
	return s.res.rt.Depth
}

// Sampler returns the comparison [gpu.Sampler] bound in [SpotShadowMap.BindGroup].
func (s *SpotShadowMap) Sampler() *gpu.Sampler {
	if s == nil {
		return nil
	}
	return s.res.sampler
}

// Layout returns the bind group layout for sampling this spot shadow map (binding 0: SpotShadow uniform, binding 1: texture_depth_2d, binding 2: sampler_comparison).
func (s *SpotShadowMap) Layout() *gpu.BindGroupLayout {
	if s == nil {
		return nil
	}
	return s.res.layout
}

// BindGroup returns the bind group to set at [LocalShadowGroup], or at the group passed to [SpotShadowWGSLForGroup], during the lit render pass.
func (s *SpotShadowMap) BindGroup() *gpu.BindGroup {
	if s == nil {
		return nil
	}
	return s.res.bindGroup
}

// ByteSize returns the GPU memory held by the depth texture and uniform buffers.
func (s *SpotShadowMap) ByteSize() uint64 {
	if s == nil || s.res.uniformBuf == nil || s.res.casterBuf == nil {
		return 0
	}
	depthBytes := uint64(s.cfg.Resolution) * uint64(s.cfg.Resolution) * 4
	return depthBytes + s.res.uniformBuf.Size() + s.res.casterBuf.Size()
}

// Valid reports whether all underlying GPU resources are open and live.
func (s *SpotShadowMap) Valid() bool {
	return s != nil && s.res.valid()
}

// BeginShadowPass resets the caster slot cursor, begins a depth-only render pass on target (a [*gpu.Encoder] or [*gpu.Frame]) clearing the spot shadow map to 1.0, sets the full viewport and scissor, and binds the depth pipeline. Record at most one shadow pass per submission. It returns nil when s, dev or target is nil or s is invalid, so check the result before drawing casters and calling End.
func (s *SpotShadowMap) BeginShadowPass(dev *gpu.Device, target ShadowPassTarget) *gpu.RenderPass {
	if s == nil || dev == nil || target == nil || !s.Valid() {
		return nil
	}
	pass := target.BeginRenderPass(gpu.RenderPassDesc{
		Label: s.cfg.Label + ".pass",
		DepthStencil: &gpu.DepthStencilAttachment{
			Texture:      s.res.rt.Depth,
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   1.0,
		},
	})
	if pass == nil {
		return nil
	}
	s.passDev = dev
	s.casterSlot = 0
	r := s.cfg.Resolution
	pass.SetViewport(0, 0, float32(r), float32(r), 0, 1)
	pass.SetScissor(0, 0, int32(r), int32(r))
	pass.SetPipeline(s.res.depthPipe)
	return pass
}

// DrawCaster records a shadow caster draw for mesh transformed by model inside the pass started by [SpotShadowMap.BeginShadowPass]. Each call uses its own 256-byte dynamic-offset uniform slot, so casters drawn in one pass never overwrite each other's transforms. It returns false and draws nothing when an argument is invalid, [SpotShadowMap.Update] has not been called yet, or [SpotShadowMap.MaxCasters] casters were already drawn in this pass.
func (s *SpotShadowMap) DrawCaster(pass *gpu.RenderPass, mesh *GPUMesh, model math3d.Mat4) bool {
	if s == nil || pass == nil || s.passDev == nil || !s.updated || !s.Valid() || !mesh.Valid() {
		return false
	}
	if s.casterSlot >= s.cfg.MaxCasters {
		return false
	}
	s.res.drawCaster(s.passDev, pass, s.casterScratch[:], s.casterSlot, s.uniforms.ViewProj, model, mesh)
	s.casterSlot++
	return true
}

// Close releases all GPU resources owned by s. It is safe to call on a nil receiver or more than once.
func (s *SpotShadowMap) Close() error {
	if s == nil {
		return nil
	}
	s.passDev = nil
	return s.res.close()
}

// PointShadowMap manages an omnidirectional shadow for the first point light of a [LightRig]: the six 90-degree cube faces packed into one 3x2 Depth32Float atlas, its caster pipeline and its sampling bind group.
//
// Call [PointShadowMap.Update] with the rig's point light, record casters for each face to refresh between [PointShadowMap.BeginFacePass] and pass.End(), and bind [PointShadowMap.BindGroup] at [LocalShadowGroup] when drawing with [NewLitPBRPointShadowPipeline].
type PointShadowMap struct {
	cfg           PointShadowConfig
	atlasW        uint32
	atlasH        uint32
	res           localShadowResources
	uniforms      PointShadowUniform
	updated       bool
	passDev       *gpu.Device
	faceViewProj  [PointShadowFaceCount]math3d.Mat4
	activeFace    gpu.CubeFace
	faceSlot      [PointShadowFaceCount]uint32
	uniformBytes  [PointShadowUniformSize]byte
	casterScratch [localShadowCasterDataSize]byte
}

// NewPointShadowMap creates a 3x2 Depth32Float atlas point shadow map and its GPU resources scoped to cx. Invalid configuration, an atlas above the device's MaxTextureDimension2D, or a caster buffer above its MaxBufferSize returns an error wrapping [ErrInvalidLocalShadow]; device errors are returned as [gpu.Error].
func NewPointShadowMap(cx *core.Scope, dev *gpu.Device, cfg PointShadowConfig) (*PointShadowMap, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	n := cfg.Normalized()
	atlasW, atlasH := n.AtlasSize()
	if maxDim := dev.Limits().MaxTextureDimension2D; maxDim > 0 && (atlasW > maxDim || atlasH > maxDim) {
		return nil, fmt.Errorf("%w: point shadow atlas %dx%d exceeds MaxTextureDimension2D %d", ErrInvalidLocalShadow, atlasW, atlasH, maxDim)
	}
	if n.Label == "" {
		n.Label = "render3d.point_shadow"
	}
	res, err := newLocalShadowResources(cx, dev, n.Label, atlasW, atlasH, PointShadowUniformSize, uint64(n.MaxCasters)*PointShadowFaceCount, n.Filter, n.CullMode, n.Retained)
	if err != nil {
		return nil, err
	}
	return &PointShadowMap{cfg: n, atlasW: atlasW, atlasH: atlasH, res: res}, nil
}

// Update computes the six cube-face projections for the [LightRig] point light light (see [NewPointShadowUniform]) and writes the uniform buffer on dev without heap allocations.
func (p *PointShadowMap) Update(dev *gpu.Device, light PointLight) error {
	if p == nil || !p.Valid() {
		return fmt.Errorf("%w: point shadow map is not valid", ErrInvalidLocalShadow)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	u, err := NewPointShadowUniform(light, p.cfg)
	if err != nil {
		return err
	}
	p.uniforms = u
	for face := range gpu.CubeFace(PointShadowFaceCount) {
		p.faceViewProj[face] = u.FaceViewProj(face)
	}
	p.updated = true
	dev.WriteBuffer(p.res.uniformBuf, 0, p.uniforms.Encode(p.uniformBytes[:]))
	return nil
}

// Uniforms returns the last point shadow uniform data written via [PointShadowMap.Update].
func (p *PointShadowMap) Uniforms() PointShadowUniform {
	if p == nil {
		return PointShadowUniform{}
	}
	return p.uniforms
}

// FaceViewProj returns the view-projection matrix of cube face (0..5) from the last [PointShadowMap.Update]. It returns identity for a nil receiver or an out-of-range face.
func (p *PointShadowMap) FaceViewProj(face gpu.CubeFace) math3d.Mat4 {
	if p == nil || face >= PointShadowFaceCount {
		return math3d.Mat4Identity()
	}
	return p.faceViewProj[face]
}

// FaceViewport returns the pixel rectangle (x, y, width, height) inside the 3x2 depth atlas assigned to cube face (0..5).
func (p *PointShadowMap) FaceViewport(face gpu.CubeFace) (x, y, width, height uint32) {
	if p == nil || face >= PointShadowFaceCount {
		return 0, 0, 0, 0
	}
	col := face % PointShadowAtlasCols
	row := face / PointShadowAtlasCols
	r := p.cfg.FaceResolution
	return col * r, row * r, r, r
}

// Config returns the normalized [PointShadowConfig].
func (p *PointShadowMap) Config() PointShadowConfig {
	if p == nil {
		return PointShadowConfig{}
	}
	return p.cfg
}

// MaxCasters returns the number of [PointShadowMap.DrawCaster] calls one face pass accepts.
func (p *PointShadowMap) MaxCasters() uint32 {
	if p == nil {
		return 0
	}
	return p.cfg.MaxCasters
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
	return p.res.rt
}

// DepthTexture returns the underlying Depth32Float 3x2 atlas [gpu.Texture].
func (p *PointShadowMap) DepthTexture() *gpu.Texture {
	if p == nil || p.res.rt == nil {
		return nil
	}
	return p.res.rt.Depth
}

// Sampler returns the comparison [gpu.Sampler] bound in [PointShadowMap.BindGroup].
func (p *PointShadowMap) Sampler() *gpu.Sampler {
	if p == nil {
		return nil
	}
	return p.res.sampler
}

// Layout returns the bind group layout for sampling this point shadow map (binding 0: PointShadow uniform, binding 1: texture_depth_2d, binding 2: sampler_comparison).
func (p *PointShadowMap) Layout() *gpu.BindGroupLayout {
	if p == nil {
		return nil
	}
	return p.res.layout
}

// BindGroup returns the bind group to set at [LocalShadowGroup], or at the group passed to [PointShadowWGSLForGroup], during the lit render pass.
func (p *PointShadowMap) BindGroup() *gpu.BindGroup {
	if p == nil {
		return nil
	}
	return p.res.bindGroup
}

// ByteSize returns the GPU memory held by the 3x2 depth atlas and uniform buffers.
func (p *PointShadowMap) ByteSize() uint64 {
	if p == nil || p.res.uniformBuf == nil || p.res.casterBuf == nil {
		return 0
	}
	depthBytes := uint64(p.atlasW) * uint64(p.atlasH) * 4
	return depthBytes + p.res.uniformBuf.Size() + p.res.casterBuf.Size()
}

// Valid reports whether all underlying GPU resources are open and live.
func (p *PointShadowMap) Valid() bool {
	return p != nil && p.res.valid()
}

// BeginFacePass begins a depth-only render pass on target (a [*gpu.Encoder] or [*gpu.Frame]) for cube face (0..5) of the 3x2 depth atlas, resets that face's caster slots, binds the depth pipeline, and locks viewport and scissor to the face's tile so geometry cannot bleed into neighboring faces.
//
// Face 0 clears the whole atlas to 1.0 and faces 1..5 keep what is already there ([gpu.LoadLoad]). Refreshing only some faces in a frame (for example skipping faces whose casters did not move) therefore works as long as face 0 is either refreshed together with every other face or not refreshed at all. Each face has its own [PointShadowMap.MaxCasters] slots, so faces may be recorded in any order; record each face at most once per submission. It returns nil when p, dev or target is nil, p is invalid, or face is out of range.
func (p *PointShadowMap) BeginFacePass(dev *gpu.Device, target ShadowPassTarget, face gpu.CubeFace) *gpu.RenderPass {
	if p == nil || dev == nil || target == nil || !p.Valid() || face >= PointShadowFaceCount {
		return nil
	}
	loadOp := gpu.LoadLoad
	if face == 0 {
		loadOp = gpu.LoadClear
	}
	pass := target.BeginRenderPass(gpu.RenderPassDesc{
		Label: fmt.Sprintf("%s.face%d", p.cfg.Label, face),
		DepthStencil: &gpu.DepthStencilAttachment{
			Texture:      p.res.rt.Depth,
			DepthLoadOp:  loadOp,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   1.0,
		},
	})
	if pass == nil {
		return nil
	}
	p.passDev = dev
	p.activeFace = face
	p.faceSlot[face] = 0
	vx, vy, vw, vh := p.FaceViewport(face)
	pass.SetViewport(float32(vx), float32(vy), float32(vw), float32(vh), 0, 1)
	pass.SetScissor(int32(vx), int32(vy), int32(vw), int32(vh))
	pass.SetPipeline(p.res.depthPipe)
	return pass
}

// DrawCaster records a shadow caster draw for mesh transformed by model into the face pass most recently started by [PointShadowMap.BeginFacePass]. It returns false and draws nothing when an argument is invalid, [PointShadowMap.Update] has not been called yet, or [PointShadowMap.MaxCasters] casters were already drawn into this face.
func (p *PointShadowMap) DrawCaster(pass *gpu.RenderPass, mesh *GPUMesh, model math3d.Mat4) bool {
	if p == nil || pass == nil || p.passDev == nil || !p.updated || !p.Valid() || !mesh.Valid() {
		return false
	}
	face := p.activeFace
	if p.faceSlot[face] >= p.cfg.MaxCasters {
		return false
	}
	slot := uint32(face)*p.cfg.MaxCasters + p.faceSlot[face]
	p.res.drawCaster(p.passDev, pass, p.casterScratch[:], slot, p.faceViewProj[face], model, mesh)
	p.faceSlot[face]++
	return true
}

// Close releases all GPU resources owned by p. It is safe to call on a nil receiver or more than once.
func (p *PointShadowMap) Close() error {
	if p == nil {
		return nil
	}
	p.passDev = nil
	return p.res.close()
}

// DecodeDepth32Float decodes tightly packed little-endian float32 depth texels returned by [gpu.Device.ReadTexture] on a [gpu.FormatDepth32Float] texture.
func DecodeDepth32Float(raw []byte) []float32 {
	n := len(raw) / 4
	out := make([]float32, n)
	for i := range n {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return out
}

// EvaluateSpotShadow is the CPU reference of sample_spot_shadow in [SpotShadowWGSL]: the spot light's visibility in [0, 1] at worldPos against a row-major res x res Depth32Float buffer, such as [DecodeDepth32Float] of the [SpotShadowMap.DepthTexture] readback.
func EvaluateSpotShadow(u SpotShadowUniform, depth []float32, res uint32, worldPos, worldNormal math3d.Vec3) float32 {
	if res == 0 || len(depth) < int(res)*int(res) {
		return 1
	}
	n := math3d.Vec3{}
	if worldNormal.LenSq() > 1e-12 {
		n = worldNormal.Norm()
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

// PointShadowSelectFace projects light-to-fragment vector v onto the dominant cube face (0..5) and returns the face index, face-local UV in [0, 1]^2, and positive major-axis distance matching [PointShadowWGSL].
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

// EvaluatePointShadow is the CPU reference of sample_point_shadow in [PointShadowWGSL]: the point light's visibility in [0, 1] at worldPos against a row-major (3*faceRes) x (2*faceRes) Depth32Float atlas buffer, such as [DecodeDepth32Float] of the [PointShadowMap.DepthTexture] readback.
func EvaluatePointShadow(u PointShadowUniform, atlasDepth []float32, faceRes uint32, worldPos, worldNormal math3d.Vec3) float32 {
	atlasW := int(faceRes * PointShadowAtlasCols)
	atlasH := int(faceRes * PointShadowAtlasRows)
	if faceRes == 0 || len(atlasDepth) < atlasW*atlasH {
		return 1
	}
	n := math3d.Vec3{}
	if worldNormal.LenSq() > 1e-12 {
		n = worldNormal.Norm()
	}
	biased := worldPos.Add(n.Scale(u.NormalBias))
	v := biased.Sub(u.Position)
	face, localU, localV, axisDist := PointShadowSelectFace(v)
	if axisDist <= u.Near || axisDist >= u.Far {
		return 1
	}
	ndcZ := (u.Far / (u.Far - u.Near)) * (1.0 - u.Near/axisDist)
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
