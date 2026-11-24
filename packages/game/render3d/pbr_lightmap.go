package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Bind group slots, binding indices, and uniform byte size for the baked-lightmap PBR pipeline ([PBRLightmapWGSL], [NewPBRLightmapPipeline], and [GPUPBRLightmap]).
const (
	// PBRLightmapMaterialGroup is the bind group index [PBRLightmapWGSL] reserves for a [GPUMaterial] (material uniform buffer plus base color, metallic-roughness, normal, occlusion, and emissive texture/sampler pairs) alongside [CameraGroup] (group 0) and [LightGroup] (group 1). It occupies group 2, matching [PBRDrawGroup] in [LitPBRWGSL].
	PBRLightmapMaterialGroup = 2

	// PBRLightmapGroup is the bind group index [PBRLightmapWGSL] and [PBRLightmapBindingsWGSL] reserve for [GPUPBRLightmap] (per-draw transform, atlas UV transform, lightmap encoding parameters, and baked lightmap texture/sampler), parallel to [FogGroup] and [IBLGroup] at group 3.
	PBRLightmapGroup = 3

	// PBRLightmapUniformBinding is the @binding index of the uniform block inside [PBRLightmapGroup].
	PBRLightmapUniformBinding uint32 = 0
	// PBRLightmapTextureBinding is the @binding index of the 2D baked lightmap texture inside [PBRLightmapGroup].
	PBRLightmapTextureBinding uint32 = 1
	// PBRLightmapSamplerBinding is the @binding index of the baked lightmap sampler inside [PBRLightmapGroup].
	PBRLightmapSamplerBinding uint32 = 2

	// PBRLightmapUniformSize is the byte size of struct PBRLightmapUniform in [PBRLightmapBindingsWGSL]: model (64) + normal_matrix (64) + uv_transform (16) + lm_params (16) + ambient_fallback (16) = 176 bytes.
	PBRLightmapUniformSize = 2*64 + 3*16
)

// MaterialBindingsWGSLForGroup returns [MaterialBindingsWGSL] with every @group(1) binding moved to @group(group), for pipelines whose group 1 already holds another bind group such as [LightGroup].
func MaterialBindingsWGSLForGroup(group uint32) string {
	return strings.ReplaceAll(MaterialBindingsWGSL, "@group(1)", fmt.Sprintf("@group(%d)", group))
}

// pbrLightmapUniformStructWGSL declares the uniform struct shared by [PBRLightmapBindingsWGSL] and [PBRLightmapBindingsWGSLForGroup].
const pbrLightmapUniformStructWGSL = `struct PBRLightmapUniform {
	model: mat4x4<f32>,
	normal_matrix: mat4x4<f32>,
	uv_transform: vec4<f32>,
	lm_params: vec4<f32>,
	ambient_fallback: vec4<f32>,
}

`

// pbrLightmapDecodeWGSL declares lightmap decoding and direct-only PBR shading helpers used by [PBRLightmapWGSL]. It uses only pow and algebraic math (no exp, exp2, log, or log2) so it executes identically on hardware backends and NSGo's software GPU VM (nsgo-dev/nsgo#2334).
const pbrLightmapDecodeWGSL = `fn pbr_srgb_to_linear_channel(c: f32) -> f32 {
	if (c <= 0.04045) {
		return c / 12.92;
	}
	return pow((c + 0.055) / 1.055, 2.4);
}

fn pbr_srgb_to_linear(c: vec3<f32>) -> vec3<f32> {
	return vec3<f32>(
		pbr_srgb_to_linear_channel(c.x),
		pbr_srgb_to_linear_channel(c.y),
		pbr_srgb_to_linear_channel(c.z),
	);
}

fn pbr_decode_lightmap(sample: vec4<f32>, encoding: f32, max_range: f32) -> vec3<f32> {
	if (encoding < 0.5) {
		return sample.rgb;
	}
	if (encoding < 1.5) {
		let clamped = clamp(sample.rgb, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
		return pbr_srgb_to_linear(clamped);
	}
	if (encoding < 2.5) {
		return sample.rgb * (sample.a * max_range);
	}
	let d = max(sample.a, 1.0 / 255.0);
	return sample.rgb / d;
}

fn shade_pbr_direct(world_pos: vec3<f32>, normal: vec3<f32>, view_dir: vec3<f32>, base_color: vec3<f32>, metallic: f32, roughness: f32) -> vec3<f32> {
	let n = normalize(normal);
	let v = normalize(view_dir);
	var lo = vec3<f32>(0.0, 0.0, 0.0);
	let dir_count = lights.counts.x;
	for (var di: u32 = 0u; di < 4u; di = di + 1u) {
		if (di >= dir_count) {
			break;
		}
		let dl = lights.directional[di];
		let l = dl.to_light.xyz;
		let ndotl = max(dot(n, l), 0.0);
		if (ndotl > 0.0) {
			lo = lo + pbr_brdf(n, v, l, base_color, metallic, roughness) * dl.color.xyz * ndotl;
		}
	}
	let pt_count = lights.counts.y;
	for (var pi_idx: u32 = 0u; pi_idx < 16u; pi_idx = pi_idx + 1u) {
		if (pi_idx >= pt_count) {
			break;
		}
		let pl = lights.point[pi_idx];
		let to_light = pl.position_range.xyz - world_pos;
		let dist_sq = dot(to_light, to_light);
		let atten = point_light_attenuation(dist_sq, pl.position_range.w);
		if (atten > 0.0 && dist_sq > 1e-12) {
			let l = to_light / sqrt(dist_sq);
			let ndotl = max(dot(n, l), 0.0);
			if (ndotl > 0.0) {
				lo = lo + pbr_brdf(n, v, l, base_color, metallic, roughness) * (pl.color.xyz * atten * ndotl);
			}
		}
	}
	let spot_count = lights.counts.z;
	for (var si: u32 = 0u; si < 8u; si = si + 1u) {
		if (si >= spot_count) {
			break;
		}
		let sl = lights.spot[si];
		let to_light = sl.position_range.xyz - world_pos;
		let dist_sq = dot(to_light, to_light);
		if (dist_sq > 1e-12) {
			let l = to_light / sqrt(dist_sq);
			let atten = point_light_attenuation(dist_sq, sl.position_range.w) * spot_cone_attenuation(sl.direction_scale.xyz, l, sl.direction_scale.w, sl.color_offset.w);
			let ndotl = max(dot(n, l), 0.0);
			if (atten > 0.0 && ndotl > 0.0) {
				lo = lo + pbr_brdf(n, v, l, base_color, metallic, roughness) * (sl.color_offset.xyz * atten * ndotl);
			}
		}
	}
	return lo;
}
`

// PBRLightmapBindingsWGSL declares the [PBRLightmapGroup] (@group(3)) uniform buffer, lightmap texture, and lightmap sampler together with pbr_decode_lightmap and shade_pbr_direct. Prepend [CameraWGSL], [LightWGSL], and [MaterialBindingsWGSLForGroup](2) before it.
const PBRLightmapBindingsWGSL = pbrLightmapUniformStructWGSL + `@group(3) @binding(0) var<uniform> pbr_lm: PBRLightmapUniform;
@group(3) @binding(1) var pbr_lightmap_tex: texture_2d<f32>;
@group(3) @binding(2) var pbr_lightmap_samp: sampler;

` + pbrLightmapDecodeWGSL

// PBRLightmapBindingsWGSLForGroup returns [PBRLightmapBindingsWGSL] with the lightmap uniform, texture, and sampler placed at @group(group).
func PBRLightmapBindingsWGSLForGroup(group uint32) string {
	return pbrLightmapUniformStructWGSL + fmt.Sprintf(`@group(%d) @binding(0) var<uniform> pbr_lm: PBRLightmapUniform;
@group(%d) @binding(1) var pbr_lightmap_tex: texture_2d<f32>;
@group(%d) @binding(2) var pbr_lightmap_samp: sampler;

`, group, group, group) + pbrLightmapDecodeWGSL
}

// pbrLightmapStagesHeadWGSL holds the UV1 vertex stage and fragment shading body up to linear radiance lit and out_alpha.
const pbrLightmapStagesHeadWGSL = `
struct PBRLightmapVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv0: vec2<f32>,
	@location(3) uv1: vec2<f32>,
}

struct PBRLightmapVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
	@location(2) uv0: vec2<f32>,
	@location(3) uv1: vec2<f32>,
}

@vertex
fn vs_main(in: PBRLightmapVertexIn) -> PBRLightmapVertexOut {
	var out: PBRLightmapVertexOut;
	let world = pbr_lm.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world;
	out.world_pos = world.xyz;
	out.world_normal = (pbr_lm.normal_matrix * vec4<f32>(in.normal, 0.0)).xyz;
	out.uv0 = in.uv0;
	out.uv1 = in.uv1;
	return out;
}

@fragment
fn fs_main(in: PBRLightmapVertexOut, @builtin(front_facing) front_facing: bool) -> @location(0) vec4<f32> {
	// Sample every map and compute screen-space derivatives in uniform control flow before any alpha-mask discard.
	let base_sample = textureSample(base_color_tex, base_color_samp, in.uv0);
	let mr_sample = textureSample(metallic_roughness_tex, metallic_roughness_samp, in.uv0);
	let norm_sample = textureSample(normal_tex, normal_samp, in.uv0);
	let occ_sample = textureSample(occlusion_tex, occlusion_samp, in.uv0);
	let emis_sample = textureSample(emissive_tex, emissive_samp, in.uv0);
	let lm_uv = in.uv1 * pbr_lm.uv_transform.xy + pbr_lm.uv_transform.zw;
	let lm_sample = textureSample(pbr_lightmap_tex, pbr_lightmap_samp, lm_uv);
	let dp1 = dpdx(in.world_pos);
	let dp2 = dpdy(in.world_pos);
	let duv1 = dpdx(in.uv0);
	let duv2 = dpdy(in.uv0);

	let base_color = material.base_color_factor * base_sample;
	if (material.flags.x > 0.5 && material.flags.x < 1.5 && base_color.a < material.params.w) {
		discard;
	}

	var geom_n = vec3<f32>(0.0, 1.0, 0.0);
	if (dot(in.world_normal, in.world_normal) > 1e-12) {
		geom_n = normalize(in.world_normal);
	}
	if (material.flags.y > 0.5 && !front_facing) {
		geom_n = -geom_n;
	}

	var n = geom_n;
	if (material.flags.z > 0.5) {
		n = material_perturb_normal(geom_n, dp1, dp2, duv1, duv2, norm_sample.xyz, material.emissive_factor.w);
	}

	let metallic = clamp(material.params.x * mr_sample.b, 0.0, 1.0);
	let roughness = clamp(material.params.y * mr_sample.g, 0.045, 1.0);
	let occlusion = mix(1.0, occ_sample.r, material.params.z);

	var emissive = material.emissive_factor.rgb;
	if (material.flags.w > 0.5) {
		emissive = emissive * emis_sample.rgb;
	}

	var indirect_irradiance = lights.ambient.xyz;
	if (pbr_lm.lm_params.w > 0.5) {
		indirect_irradiance = pbr_decode_lightmap(lm_sample, pbr_lm.lm_params.x, pbr_lm.lm_params.z) * pbr_lm.lm_params.y;
	} else if (pbr_lm.ambient_fallback.w > 0.5) {
		indirect_irradiance = pbr_lm.ambient_fallback.rgb;
	}
	let indirect = (1.0 - metallic) * base_color.rgb * indirect_irradiance * occlusion;

	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let direct = shade_pbr_direct(in.world_pos, n, view_dir, base_color.rgb, metallic, roughness);

	let lit = indirect + direct + emissive;
	var out_alpha = 1.0;
	if (material.flags.x > 1.5) {
		out_alpha = base_color.a;
	}
`

// PBRLightmapWGSL returns the complete portable WGSL shader module compiled by [NewPBRLightmapPipeline] for non-sRGB color targets. It combines:
//   - [CameraWGSL] at [CameraGroup] (group 0)
//   - [LightWGSL] at [LightGroup] (group 1) for dynamic directional, point, and spot lights
//   - [MaterialBindingsWGSL] shifted to [PBRLightmapMaterialGroup] (group 2) for [GPUMaterial] base color, metallic-roughness, normal, occlusion, and emissive maps on UV0
//   - [PBRLightmapBindingsWGSL] at [PBRLightmapGroup] (group 3) for the per-object transform and baked lightmap on UV1 * UVScale + UVOffset
//   - [LightmapVertexLayout] (40 bytes: position, normal, UV0, UV1)
//
// The decoded lightmap replaces the ambient / indirect diffuse term as (1 - metallic) * baseColor * decodedLightmap * intensity * occlusion while the [LightRig] dynamic lights supply the Cook-Torrance GGX direct term plus emissive, encoded with linear_to_srgb matching [LitPBRWGSL] and [LightmapWGSL].
func PBRLightmapWGSL() string {
	return CameraWGSL + LightWGSL + MaterialBindingsWGSLForGroup(PBRLightmapMaterialGroup) + PBRLightmapBindingsWGSL + pbrLightmapStagesHeadWGSL + `	return vec4<f32>(linear_to_srgb(lit), out_alpha);
}
`
}

// PBRLightmapLinearWGSL returns the linear-output variant of [PBRLightmapWGSL] that skips linear_to_srgb, compiled by [NewPBRLightmapPipeline] when [PBRLightmapPipelineOptions].LinearOutput is set or when ColorFormat is an sRGB target.
func PBRLightmapLinearWGSL() string {
	return CameraWGSL + LightWGSL + MaterialBindingsWGSLForGroup(PBRLightmapMaterialGroup) + PBRLightmapBindingsWGSL + pbrLightmapStagesHeadWGSL + `	return vec4<f32>(lit, out_alpha);
}
`
}

// PBRLightmap configures per-object transform, baked lightmap texture/sampler, encoding, intensity, RGBM range, shared-atlas UV transform, and fallback ambient irradiance for [NewGPUPBRLightmap] and [GPUPBRLightmap.Update].
type PBRLightmap struct {
	// Model transforms vertices from object space to world space; the zero matrix defaults to identity.
	Model math3d.Mat4
	// Lightmap is the baked irradiance texture sampled at UV1*UVTransform.Scale + UVTransform.Offset. When nil, a 1x1 fallback texture is bound and indirect irradiance falls back to AmbientFallback (if non-zero or UseAmbientFallback is true) or the scene [LightRig]'s ambient light.
	Lightmap *GPUTexture
	// LightmapSampler is an optional sampler for Lightmap; nil defaults to linear clamp-to-edge.
	LightmapSampler *GPUSampler
	// Encoding specifies how Lightmap texels are decoded to linear irradiance.
	Encoding LightmapEncoding
	// Intensity scales decoded lightmap irradiance. Zero means zero (no baked light); [DefaultPBRLightmap] sets 1.
	Intensity float32
	// RGBMMaxRange sets the HDR peak scale for [LightmapRGBM]. Zero defaults to [DefaultRGBMMaxRange] (8.0).
	RGBMMaxRange float32
	// UVTransform maps UV1 into a shared baked lightmap atlas (uv1 * Scale + Offset). Zero Scale defaults to {1, 1}.
	UVTransform UVTransform
	// AmbientFallback overrides the scene [LightRig]'s ambient irradiance when Lightmap is nil (used whenever non-zero or UseAmbientFallback is true).
	AmbientFallback [3]float32
	// UseAmbientFallback forces AmbientFallback (even when zero) instead of the [LightRig]'s ambient when Lightmap is nil.
	UseAmbientFallback bool
}

// DefaultPBRLightmap returns a [PBRLightmap] with an identity model matrix, [LightmapLinear] encoding, unit intensity, default RGBM range, and identity atlas UV transform.
func DefaultPBRLightmap() PBRLightmap {
	return PBRLightmap{
		Model:        math3d.Mat4Identity(),
		Encoding:     LightmapLinear,
		Intensity:    1,
		RGBMMaxRange: DefaultRGBMMaxRange,
		UVTransform:  IdentityUVTransform(),
	}
}

// Validate checks that every numeric field, encoding, UV transform, and non-nil GPU handle on m is valid. Every error wraps [ErrInvalidLightmap].
func (m PBRLightmap) Validate() error {
	for i, f := range m.Model {
		if !finite(f) {
			return fmt.Errorf("%w: Model[%d] = %v is not finite", ErrInvalidLightmap, i, f)
		}
	}
	if !m.Encoding.Valid() {
		return fmt.Errorf("%w: unknown encoding %d", ErrInvalidLightmap, m.Encoding)
	}
	if !finite(m.Intensity) || m.Intensity < 0 {
		return fmt.Errorf("%w: Intensity %v must be finite and non-negative", ErrInvalidLightmap, m.Intensity)
	}
	if !finite(m.RGBMMaxRange) || m.RGBMMaxRange < 0 {
		return fmt.Errorf("%w: RGBMMaxRange %v must be finite and non-negative", ErrInvalidLightmap, m.RGBMMaxRange)
	}
	if err := m.UVTransform.Validate(); err != nil {
		return err
	}
	for i, c := range m.AmbientFallback {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: AmbientFallback[%d] = %v must be finite and non-negative", ErrInvalidLightmap, i, c)
		}
	}
	if m.Lightmap != nil {
		if !m.Lightmap.Valid() {
			return fmt.Errorf("%w: Lightmap texture is not valid", ErrInvalidLightmap)
		}
		if (m.Encoding == LightmapRGBM || m.Encoding == LightmapRGBD) && m.Lightmap.SRGB() {
			return fmt.Errorf("%w: %s lightmap texture must be uploaded with SRGB=false", ErrInvalidLightmap, m.Encoding)
		}
	}
	if m.LightmapSampler != nil && !m.LightmapSampler.Valid() {
		return fmt.Errorf("%w: LightmapSampler is not valid", ErrInvalidLightmap)
	}
	return nil
}

// EncodeUniform writes m's [PBRLightmapUniformSize]-byte little-endian uniform block into dst, matching struct PBRLightmapUniform in [PBRLightmapBindingsWGSL], and returns dst[:PBRLightmapUniformSize]. It panics if len(dst) < PBRLightmapUniformSize.
func (m PBRLightmap) EncodeUniform(dst []byte) []byte {
	if len(dst) < PBRLightmapUniformSize {
		panic("render3d: PBRLightmap.EncodeUniform: dst shorter than PBRLightmapUniformSize")
	}
	dst = dst[:PBRLightmapUniformSize]
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
		off += 4
	}
	model := m.Model
	if model == (math3d.Mat4{}) {
		model = math3d.Mat4Identity()
	}
	for _, f := range model {
		put(f)
	}
	nm4 := math3d.Mat4Identity()
	if nm, ok := math3d.NormalMatrix(model); ok {
		nm4 = math3d.Mat4{
			nm[0], nm[1], nm[2], 0,
			nm[3], nm[4], nm[5], 0,
			nm[6], nm[7], nm[8], 0,
			0, 0, 0, 1,
		}
	}
	for _, f := range nm4 {
		put(f)
	}

	uvt := m.UVTransform.Normalized()
	put(uvt.Scale[0])
	put(uvt.Scale[1])
	put(uvt.Offset[0])
	put(uvt.Offset[1])

	encMode := float32(m.Encoding)
	if m.Encoding == LightmapSRGB && m.Lightmap != nil && m.Lightmap.SRGB() {
		// Hardware sampler already decodes FormatRGBA8UnormSRGB to linear.
		encMode = float32(LightmapLinear)
	}
	rgbmRange := m.RGBMMaxRange
	if rgbmRange == 0 {
		rgbmRange = DefaultRGBMMaxRange
	}
	var hasLM float32
	if m.Lightmap != nil {
		hasLM = 1
	}
	put(encMode)
	put(m.Intensity)
	put(rgbmRange)
	put(hasLM)

	var useFallback float32
	if m.UseAmbientFallback || m.AmbientFallback != [3]float32{} {
		useFallback = 1
	}
	put(m.AmbientFallback[0])
	put(m.AmbientFallback[1])
	put(m.AmbientFallback[2])
	put(useFallback)
	return dst
}

// PBRLightmapBindGroupLayout creates the [PBRLightmapGroup] (@group(3)) bind group layout shared by every [GPUPBRLightmap]: a vertex+fragment uniform buffer at binding 0, a sampled 2D lightmap texture at binding 1, and a sampler at binding 2.
func PBRLightmapBindGroupLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.pbr_lightmap",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: PBRLightmapUniformBinding, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: PBRLightmapTextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: PBRLightmapSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
}

// PBRLightmapOptions configures [NewGPUPBRLightmap].
type PBRLightmapOptions struct {
	// Label names the uniform buffer, bind group layout, and bind group in GPU capture tools.
	Label string
	// SharedLayout reuses an existing [PBRLightmapGroup] bind group layout (from [PBRLightmapBindGroupLayout], another [GPUPBRLightmap.Layout], or [GLTFPBRLightmapSet.Layout]) instead of creating one per object.
	SharedLayout *gpu.BindGroupLayout
	// FallbackTexture is an optional 1x1 linear texture bound when [PBRLightmap.Lightmap] is nil so callers sharing one fallback do not allocate a new 1x1 texture per instance.
	FallbackTexture *GPUTexture
	// FallbackSampler is an optional clamp-to-edge sampler bound when [PBRLightmap.LightmapSampler] is nil.
	FallbackSampler *GPUSampler
	// Retained keeps CPU shadow copies of the uniform buffer and any owned fallback texture/sampler so NSGo restores them across a GPU device loss.
	Retained bool
}

// GPUPBRLightmap holds a scope-owned [PBRLightmap] uniform buffer and [PBRLightmapGroup] bind group wiring the per-object transform, atlas UV transform, lightmap encoding parameters, and baked lightmap texture/sampler.
type GPUPBRLightmap struct {
	dev          *gpu.Device
	buf          *gpu.Buffer
	layout       *gpu.BindGroupLayout
	ownedLayout  bool
	ownedTex     *GPUTexture
	ownedSampler *GPUSampler
	group        *gpu.BindGroup
	config       PBRLightmap
	scratch      [PBRLightmapUniformSize]byte
}

// NewGPUPBRLightmap validates lm and creates its uniform buffer, optional bind group layout, and [PBRLightmapGroup] bind group on dev, owned by cx. When lm.Lightmap or lm.LightmapSampler is nil, FallbackTexture / FallbackSampler from opts is used or a retained-aware 1x1 linear fallback texture / linear-clamp sampler is created and owned by the returned handle.
func NewGPUPBRLightmap(cx *core.Scope, dev *gpu.Device, lm PBRLightmap, opts PBRLightmapOptions) (*GPUPBRLightmap, error) {
	if err := lm.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if opts.SharedLayout != nil && !opts.SharedLayout.Valid() {
		return nil, fmt.Errorf("%w: shared lightmap bind group layout is not valid", ErrInvalidLightmap)
	}
	if opts.FallbackTexture != nil && !opts.FallbackTexture.Valid() {
		return nil, fmt.Errorf("%w: fallback lightmap texture is not valid", ErrInvalidLightmap)
	}
	if opts.FallbackSampler != nil && !opts.FallbackSampler.Valid() {
		return nil, fmt.Errorf("%w: fallback lightmap sampler is not valid", ErrInvalidLightmap)
	}

	label := opts.Label
	if label == "" {
		label = "render3d.pbr_lightmap"
	}

	g := &GPUPBRLightmap{
		dev:    dev,
		config: lm,
	}
	cleanup := func(err error) (*GPUPBRLightmap, error) {
		_ = g.Close()
		return nil, err
	}

	lm.EncodeUniform(g.scratch[:])
	var err error
	g.buf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".uniforms",
		Size:     PBRLightmapUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     g.scratch[:],
		Retained: opts.Retained,
	})
	if err != nil {
		return cleanup(err)
	}

	if opts.SharedLayout != nil {
		g.layout = opts.SharedLayout
	} else {
		g.layout, err = PBRLightmapBindGroupLayout(cx, dev)
		if err != nil {
			return cleanup(err)
		}
		g.ownedLayout = true
	}

	tex := lm.Lightmap
	if tex == nil {
		if opts.FallbackTexture != nil {
			tex = opts.FallbackTexture
		} else {
			g.ownedTex, err = UploadTexturePixels(cx, dev, PixelBuffer{
				Width:  1,
				Height: 1,
				Format: gpu.FormatRGBA8Unorm,
				Pixels: []byte{255, 255, 255, 255},
			}, TextureUpload{
				Label:    label + ".fallback_white",
				Retained: opts.Retained,
			})
			if err != nil {
				return cleanup(err)
			}
			tex = g.ownedTex
		}
	}

	samp := lm.LightmapSampler
	if samp == nil {
		if opts.FallbackSampler != nil {
			samp = opts.FallbackSampler
		} else {
			sopts := SamplerLinearClamp()
			sopts.Label = label + ".fallback_sampler"
			sopts.Retained = opts.Retained
			g.ownedSampler, err = NewGPUSampler(cx, dev, sopts)
			if err != nil {
				return cleanup(err)
			}
			samp = g.ownedSampler
		}
	}

	g.group, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label,
		Layout: g.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: PBRLightmapUniformBinding, Buffer: g.buf, Size: PBRLightmapUniformSize},
			{Binding: PBRLightmapTextureBinding, Texture: tex.Texture()},
			{Binding: PBRLightmapSamplerBinding, Sampler: samp.Sampler()},
		},
	})
	if err != nil {
		return cleanup(err)
	}
	return g, nil
}

// Update validates lm and writes its updated uniform parameters (model matrix, UV transform, encoding, intensity, RGBM range, ambient fallback) into g's uniform buffer without reallocating the bind group. Lightmap and LightmapSampler pointers must match those g was created with. Pass the active [*gpu.Device] (or nil to reuse the stored device) so retained handles continue updating cleanly after a device loss and restore.
func (g *GPUPBRLightmap) Update(dev *gpu.Device, lm PBRLightmap) error {
	if !g.Valid() {
		return fmt.Errorf("%w: lightmap is not valid", ErrInvalidLightmap)
	}
	if err := lm.Validate(); err != nil {
		return err
	}
	if lm.Lightmap != g.config.Lightmap || lm.LightmapSampler != g.config.LightmapSampler {
		return fmt.Errorf("%w: Update cannot change Lightmap or LightmapSampler bindings; create a new GPUPBRLightmap", ErrInvalidLightmap)
	}
	target := dev
	if target == nil {
		target = g.dev
	} else {
		g.dev = dev
	}
	if target == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	target.WriteBuffer(g.buf, 0, lm.EncodeUniform(g.scratch[:]))
	g.config = lm
	return nil
}

// SetModel updates only the object-to-world Model matrix (and derived normal matrix) of g.
func (g *GPUPBRLightmap) SetModel(dev *gpu.Device, model math3d.Mat4) error {
	if g == nil {
		return fmt.Errorf("%w: lightmap is not valid", ErrInvalidLightmap)
	}
	next := g.config
	next.Model = model
	return g.Update(dev, next)
}

// Bind binds g at [PBRLightmapGroup] (group 3) on pass. It records nothing when g is not valid.
func (g *GPUPBRLightmap) Bind(pass *gpu.RenderPass) {
	g.BindAt(pass, PBRLightmapGroup)
}

// BindAt binds g at the specified bind group slot on pass. It records nothing when g is not valid.
func (g *GPUPBRLightmap) BindAt(pass *gpu.RenderPass, group uint32) {
	if !g.Valid() || pass == nil {
		return
	}
	pass.SetBindGroup(group, g.group)
}

// BindGroup returns the underlying [*gpu.BindGroup], or nil for a nil receiver.
func (g *GPUPBRLightmap) BindGroup() *gpu.BindGroup {
	if g == nil {
		return nil
	}
	return g.group
}

// Layout returns the [PBRLightmapGroup] [*gpu.BindGroupLayout], or nil for a nil receiver.
func (g *GPUPBRLightmap) Layout() *gpu.BindGroupLayout {
	if g == nil {
		return nil
	}
	return g.layout
}

// UniformBuffer returns the underlying uniform [*gpu.Buffer], or nil for a nil receiver.
func (g *GPUPBRLightmap) UniformBuffer() *gpu.Buffer {
	if g == nil {
		return nil
	}
	return g.buf
}

// Config returns the latest [PBRLightmap] parameters uploaded to g.
func (g *GPUPBRLightmap) Config() PBRLightmap {
	if g == nil {
		return PBRLightmap{}
	}
	return g.config
}

// ByteSize returns the GPU memory held by g's uniform buffer and any owned fallback texture.
func (g *GPUPBRLightmap) ByteSize() uint64 {
	if g == nil {
		return 0
	}
	var total uint64
	if g.buf != nil {
		total += g.buf.Size()
	}
	if g.ownedTex != nil {
		total += g.ownedTex.ByteSize()
	}
	return total
}

// Valid reports whether g's uniform buffer, layout, and bind group are open on a live device.
func (g *GPUPBRLightmap) Valid() bool {
	if g == nil || g.buf == nil || !g.buf.Valid() || g.layout == nil || !g.layout.Valid() || g.group == nil || !g.group.Valid() {
		return false
	}
	if g.ownedTex != nil && !g.ownedTex.Valid() {
		return false
	}
	if g.ownedSampler != nil && !g.ownedSampler.Valid() {
		return false
	}
	return true
}

// Close releases g's bind group, uniform buffer, and any owned fallback resources. It is nil-safe and safe to call more than once.
func (g *GPUPBRLightmap) Close() error {
	if g == nil {
		return nil
	}
	var err error
	if g.group != nil {
		err = errors.Join(err, g.group.Close())
	}
	if g.ownedSampler != nil {
		err = errors.Join(err, g.ownedSampler.Close())
	}
	if g.ownedTex != nil {
		err = errors.Join(err, g.ownedTex.Close())
	}
	if g.ownedLayout && g.layout != nil {
		err = errors.Join(err, g.layout.Close())
	}
	if g.buf != nil {
		err = errors.Join(err, g.buf.Close())
	}
	return err
}

// PBRLightmapPipelineOptions configures [NewPBRLightmapPipeline].
type PBRLightmapPipelineOptions struct {
	// Label names the pipeline in GPU capture tools.
	Label string
	// ColorFormat sets the color attachment format; [gpu.FormatUndefined] defaults to dev.SurfaceFormat() (or [gpu.FormatRGBA8Unorm] if undefined).
	ColorFormat gpu.TextureFormat
	// DepthFormat enables depth testing (and depth writes for opaque and alpha-mask modes) when non-zero.
	DepthFormat gpu.TextureFormat
	// AlphaMode selects blending and depth-write state: [AlphaOpaque] and [AlphaMask] write depth without color blending; [AlphaBlend] uses straight alpha blending ([gpu.BlendAlpha]) with depth writes disabled.
	AlphaMode AlphaMode
	// DoubleSided disables back-face culling ([gpu.CullNone]); when false, back faces are culled ([gpu.CullBack]).
	DoubleSided bool
	// LinearOutput skips the shader's final linear_to_srgb encode and writes unclamped linear radiance, for drawing into an [HDRTarget] resolved by [ToneMapPass]. When ColorFormat is an sRGB format, linear output is selected automatically so the hardware does not encode twice.
	LinearOutput bool
	// Retained keeps the compiled pipeline across a GPU device loss.
	Retained bool
}

// NewPBRLightmapPipeline compiles [PBRLightmapWGSL] (or [PBRLightmapLinearWGSL] when LinearOutput is true or ColorFormat is sRGB) and creates a render pipeline taking [LightmapVertexLayout] vertices at slot 0 with:
//   - camLayout at [CameraGroup] (group 0)
//   - lightLayout ([GPULightBuffer.Layout]) at [LightGroup] (group 1)
//   - matLayout ([MaterialDefaults.Layout]) at [PBRLightmapMaterialGroup] (group 2)
//   - lmLayout ([PBRLightmapBindGroupLayout] / [GPUPBRLightmap.Layout] / [GLTFPBRLightmapSet.Layout]) at [PBRLightmapGroup] (group 3)
func NewPBRLightmapPipeline(
	cx *core.Scope,
	dev *gpu.Device,
	camLayout, lightLayout, matLayout, lmLayout *gpu.BindGroupLayout,
	opts PBRLightmapPipelineOptions,
) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	if lightLayout == nil || !lightLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid light bind group layout"}
	}
	if matLayout == nil || !matLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid material bind group layout"}
	}
	if lmLayout == nil || !lmLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid PBR lightmap bind group layout"}
	}
	if !validAlphaMode(opts.AlphaMode) {
		return nil, fmt.Errorf("%w: invalid AlphaMode %d", ErrInvalidMaterial, opts.AlphaMode)
	}

	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
		if colorFmt == gpu.FormatUndefined {
			colorFmt = gpu.FormatRGBA8Unorm
		}
	}
	src := PBRLightmapWGSL()
	if opts.LinearOutput || colorFmt.IsSRGB() {
		src = PBRLightmapLinearWGSL()
	}
	mod, err := gpu.CompileWGSL(src)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile PBR lightmap shader: %w", err)
	}

	target := gpu.ColorTargetState{Format: colorFmt, WriteMask: gpu.ColorWriteAll}
	if opts.AlphaMode == AlphaBlend {
		target.Blend = gpu.BlendAlpha()
	}
	cull := gpu.CullBack
	if opts.DoubleSided {
		cull = gpu.CullNone
	}
	label := opts.Label
	if label == "" {
		label = "render3d.pbr_lightmap." + opts.AlphaMode.String()
		if opts.LinearOutput {
			label += ".linear"
		}
	}
	layouts := []*gpu.BindGroupLayout{
		CameraGroup:              camLayout,
		LightGroup:               lightLayout,
		PBRLightmapMaterialGroup: matLayout,
		PBRLightmapGroup:         lmLayout,
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{LightmapVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  cull,
		},
		Targets:  []gpu.ColorTargetState{target},
		Retained: opts.Retained,
	}
	if opts.DepthFormat != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:       opts.DepthFormat,
			DepthWrite:   opts.AlphaMode != AlphaBlend,
			DepthCompare: gpu.CompareLess,
		}
	}
	return dev.CreatePipeline(cx, desc)
}

// PBRLightmapEvalInput provides per-fragment inputs to [EvaluatePBRLightmap], the pure-Go CPU reference evaluator matching [PBRLightmapWGSL].
type PBRLightmapEvalInput struct {
	Material Material

	BaseColorSample            [4]float32
	HasBaseColorSample         bool
	MetallicSample             float32 // glTF metallic-roughness texture B channel
	RoughnessSample            float32 // glTF metallic-roughness texture G channel
	HasMetallicRoughnessSample bool
	NormalSample               [3]float32 // UNORM [0, 1] tangent-space normal sample
	HasNormalSample            bool
	OcclusionSample            float32 // glTF occlusion texture R channel
	HasOcclusionSample         bool
	EmissiveSample             [3]float32 // linear RGB emissive texture sample
	HasEmissiveSample          bool

	// Lightmap inputs. When HasLightmap is true, LightmapSample is decoded using Encoding, RGBMMaxRange, and Intensity and replaces Rig.Ambient.
	HasLightmap        bool
	LightmapSample     [4]float32
	LightmapSRGB       bool // true when the lightmap texture was uploaded as FormatRGBA8UnormSRGB (hardware already decoded sRGB)
	Encoding           LightmapEncoding
	Intensity          float32
	RGBMMaxRange       float32
	AmbientFallback    [3]float32
	UseAmbientFallback bool

	WorldPos    math3d.Vec3
	Normal      math3d.Vec3 // geometric world-space normal
	Tangent     math3d.Vec3 // optional world-space tangent (+U); derived from Normal when zero
	Bitangent   math3d.Vec3 // optional world-space bitangent (+Y normal map / -V); derived from Normal x Tangent when zero
	ViewDir     math3d.Vec3 // vector from surface toward camera
	FrontFacing bool        // true for front-facing fragments; flips Normal when false and Material.DoubleSided is true
	Rig         LightRig
}

// PBRLightmapEvalResult holds the CPU reference shading output produced by [EvaluatePBRLightmap].
type PBRLightmapEvalResult struct {
	Linear    math3d.Vec4 // unclamped linear RGBA radiance matching [PBRLightmapLinearWGSL]
	SRGB      math3d.Vec4 // IEC 61966-2-1 sRGB-encoded RGBA matching [PBRLightmapWGSL]
	Indirect  math3d.Vec3 // baked lightmap (or ambient fallback) diffuse contribution
	Direct    math3d.Vec3 // dynamic [LightRig] Cook-Torrance GGX direct contribution
	Emissive  math3d.Vec3 // emissive contribution
	Normal    math3d.Vec3 // final world-space shading normal after normal mapping
	Metallic  float32     // effective clamped metallic in [0, 1]
	Roughness float32     // effective clamped roughness in [0.045, 1]
	Discarded bool        // true when AlphaMode == AlphaMask and effective alpha < AlphaCutoff
}

// DecodeLightmapSample decodes a sampled RGBA lightmap texel into linear HDR RGB irradiance using the exact same rules as [LightmapWGSL] and [PBRLightmapWGSL]. Set hardwareSRGB true when encoding is [LightmapSRGB] and the GPU texture format already decoded sRGB on sample.
func DecodeLightmapSample(sample [4]float32, encoding LightmapEncoding, rgbmMaxRange float32, hardwareSRGB bool) math3d.Vec3 {
	if encoding == LightmapSRGB && hardwareSRGB {
		encoding = LightmapLinear
	}
	switch encoding {
	case LightmapLinear:
		return math3d.Vec3{X: sample[0], Y: sample[1], Z: sample[2]}
	case LightmapSRGB:
		return math3d.Vec3{
			X: SRGBToLinearChannel(sample[0]),
			Y: SRGBToLinearChannel(sample[1]),
			Z: SRGBToLinearChannel(sample[2]),
		}
	case LightmapRGBM:
		if rgbmMaxRange <= 0 || !finite(rgbmMaxRange) {
			rgbmMaxRange = DefaultRGBMMaxRange
		}
		scale := sample[3] * rgbmMaxRange
		return math3d.Vec3{
			X: sample[0] * scale,
			Y: sample[1] * scale,
			Z: sample[2] * scale,
		}
	case LightmapRGBD:
		d := sample[3]
		if d < 1.0/255.0 {
			d = 1.0 / 255.0
		}
		invD := 1.0 / d
		return math3d.Vec3{
			X: sample[0] * invD,
			Y: sample[1] * invD,
			Z: sample[2] * invD,
		}
	default:
		return math3d.Vec3{X: sample[0], Y: sample[1], Z: sample[2]}
	}
}

// EvaluatePBRLightmap evaluates the lightmapped glTF 2.0 metallic-roughness PBR shading model on the CPU, matching [PBRLightmapWGSL] and [PBRLightmapLinearWGSL].
func EvaluatePBRLightmap(in PBRLightmapEvalInput) PBRLightmapEvalResult {
	m := in.Material
	baseSample := [4]float32{1, 1, 1, 1}
	if in.HasBaseColorSample {
		baseSample = in.BaseColorSample
	}
	baseColor := math3d.Vec3{
		X: m.BaseColorFactor[0] * baseSample[0],
		Y: m.BaseColorFactor[1] * baseSample[1],
		Z: m.BaseColorFactor[2] * baseSample[2],
	}
	baseAlpha := m.BaseColorFactor[3] * baseSample[3]

	if m.AlphaMode == AlphaMask && baseAlpha < m.AlphaCutoff {
		return PBRLightmapEvalResult{Discarded: true}
	}

	geomN := math3d.Vec3{Y: 1}
	if in.Normal.LenSq() > 1e-12 {
		geomN = in.Normal.Norm()
	}
	if m.DoubleSided && !in.FrontFacing {
		geomN = geomN.Neg()
	}

	n := geomN
	if in.HasNormalSample || m.NormalTexture != nil {
		normSample := [3]float32{0.5, 0.5, 1}
		if in.HasNormalSample {
			normSample = in.NormalSample
		}
		tVec, bVec := resolveTangentBasis(geomN, in.Tangent, in.Bitangent)
		nx := (normSample[0]*2 - 1) * m.NormalScale
		ny := (normSample[1]*2 - 1) * m.NormalScale
		nz := normSample[2]*2 - 1
		nTS := math3d.Vec3{X: nx, Y: ny, Z: nz}.Norm()
		n = tVec.Scale(nTS.X).Add(bVec.Scale(nTS.Y)).Add(geomN.Scale(nTS.Z)).Norm()
	}

	metSample, roughSample := float32(1), float32(1)
	if in.HasMetallicRoughnessSample {
		metSample = in.MetallicSample
		roughSample = in.RoughnessSample
	}
	metallic := clampF32(m.MetallicFactor*metSample, 0, 1)
	roughness := clampF32(m.RoughnessFactor*roughSample, minPBRRoughness, 1)

	occSample := float32(1)
	if in.HasOcclusionSample {
		occSample = in.OcclusionSample
	}
	occlusion := 1*(1-m.OcclusionStrength) + occSample*m.OcclusionStrength

	emissive := math3d.Vec3{X: m.EmissiveFactor[0], Y: m.EmissiveFactor[1], Z: m.EmissiveFactor[2]}
	if in.HasEmissiveSample {
		emissive = math3d.Vec3{
			X: emissive.X * in.EmissiveSample[0],
			Y: emissive.Y * in.EmissiveSample[1],
			Z: emissive.Z * in.EmissiveSample[2],
		}
	}

	var indirectIrradiance math3d.Vec3
	if in.HasLightmap {
		decoded := DecodeLightmapSample(in.LightmapSample, in.Encoding, in.RGBMMaxRange, in.LightmapSRGB)
		indirectIrradiance = decoded.Scale(in.Intensity)
	} else if in.UseAmbientFallback || in.AmbientFallback != [3]float32{} {
		indirectIrradiance = math3d.Vec3{
			X: in.AmbientFallback[0],
			Y: in.AmbientFallback[1],
			Z: in.AmbientFallback[2],
		}
	} else {
		indirectIrradiance = in.Rig.Ambient.Color.Scale(in.Rig.Ambient.Intensity)
	}

	diffuseScale := (1 - metallic) * occlusion
	indirect := math3d.Vec3{
		X: baseColor.X * indirectIrradiance.X * diffuseScale,
		Y: baseColor.Y * indirectIrradiance.Y * diffuseScale,
		Z: baseColor.Z * indirectIrradiance.Z * diffuseScale,
	}

	directRig := in.Rig
	directRig.Ambient = AmbientLight{}
	direct := ShadePoint(in.WorldPos, n, in.ViewDir, PBRMaterial{
		BaseColor: baseColor,
		Metallic:  metallic,
		Roughness: roughness,
	}, directRig)

	linearRGB := indirect.Add(direct).Add(emissive)
	srgbRGB := LinearToSRGB(linearRGB)

	outAlpha := float32(1)
	if m.AlphaMode == AlphaBlend {
		outAlpha = baseAlpha
	}
	return PBRLightmapEvalResult{
		Linear:    math3d.Vec4{X: linearRGB.X, Y: linearRGB.Y, Z: linearRGB.Z, W: outAlpha},
		SRGB:      math3d.Vec4{X: srgbRGB.X, Y: srgbRGB.Y, Z: srgbRGB.Z, W: outAlpha},
		Indirect:  indirect,
		Direct:    direct,
		Emissive:  emissive,
		Normal:    n,
		Metallic:  metallic,
		Roughness: roughness,
	}
}

// LightmapMeshFromGLTF builds a 40-byte dual-UV [LightmapMesh] from a glTF primitive, extracting the material's lightmap UV channel (TEXCOORD_1 or explicit texCoord from MOZ_lightmap / EXT_lightmap / extras.lightmap, falling back cleanly to TEXCOORD_0 when TEXCOORD_1 is absent).
func LightmapMeshFromGLTF(doc *gltf.Document, prim gltf.Primitive) (LightmapMesh, error) {
	if doc == nil {
		return LightmapMesh{}, fmt.Errorf("%w: nil glTF document", ErrInvalidLightmap)
	}
	var lmRef *gltf.LightmapRef
	if prim.Material >= 0 && prim.Material < len(doc.Materials) {
		var err error
		lmRef, err = doc.MaterialLightmap(prim.Material)
		if err != nil {
			return LightmapMesh{}, fmt.Errorf("%w: %w", ErrInvalidLightmap, err)
		}
	}
	var uv1 []math3d.Vec2
	if lmRef != nil {
		var err error
		uv1, _, err = doc.LightmapTexCoords(prim, lmRef)
		if err != nil {
			return LightmapMesh{}, fmt.Errorf("%w: %w", ErrInvalidLightmap, err)
		}
	} else {
		uv1, _ = gltf.PrimitiveLightmapUVs(prim)
	}
	mesh := LightmapMeshFromAttributes(prim.Positions, prim.Normals, prim.TexCoords0, uv1, prim.Indices)
	if err := mesh.Validate(); err != nil {
		return LightmapMesh{}, err
	}
	return mesh, nil
}

// GLTFPBRLightmapUpload configures [UploadGLTFPBRLightmaps].
type GLTFPBRLightmapUpload struct {
	// Label prefixes debug labels on uploaded lightmap textures, samplers, buffers, and bind groups.
	Label string
	// SharedLayout reuses an existing [PBRLightmapGroup] bind group layout instead of creating one.
	SharedLayout *gpu.BindGroupLayout
	// DefaultEncoding selects the [LightmapEncoding] used when a glTF material's lightmap metadata omits an explicit encoding string (defaults to [LightmapLinear]).
	DefaultEncoding LightmapEncoding
	// DisableMipmaps disables mipmap generation on linear and sRGB lightmaps even when glTF samplers request mipmaps. Packed HDR formats ([LightmapRGBM], [LightmapRGBD]) never generate CPU box mipmaps because linear averaging corrupts shared-exponent channels.
	DisableMipmaps bool
	// MaxMipLevels caps generated mipmap chains when mipmaps are enabled; 0 builds the full chain.
	MaxMipLevels uint32
	// Retained keeps CPU shadow copies of every uploaded lightmap texture, sampler, and uniform buffer so the returned [GLTFPBRLightmapSet] recovers automatically after a GPU device loss.
	Retained bool
}

// GLTFPBRLightmapInfo holds the extracted glTF [gltf.LightmapRef], decoded [LightmapEncoding], uploaded GPU texture/sampler, and bound [GPUPBRLightmap] for one material entry in [GLTFPBRLightmapSet].
type GLTFPBRLightmapInfo struct {
	HasLightmap  bool
	Ref          *gltf.LightmapRef
	Encoding     LightmapEncoding
	TextureIndex int
	ImageIndex   int
	SamplerIndex int
	Texture      *GPUTexture
	Sampler      *GPUSampler
	Config       PBRLightmap
	GPU          *GPUPBRLightmap
}

// GLTFPBRLightmapSet owns every lightmap [GPUTexture], [GPUSampler], and [GPUPBRLightmap] uploaded from a [*gltf.Document] by [UploadGLTFPBRLightmaps]. Pair it with [GLTFMaterialSet] from [UploadGLTFMaterials] to draw glTF assets with both their metallic-roughness maps and their baked lightmaps in one [NewPBRLightmapPipeline].
type GLTFPBRLightmapSet struct {
	doc             *gltf.Document
	layout          *gpu.BindGroupLayout
	ownedLayout     bool
	fallbackTex     *GPUTexture
	defaultSampler  *GPUSampler
	defaultLightmap *GPUPBRLightmap
	lightmaps       []GLTFPBRLightmapInfo
	textures        []*GPUTexture
	textureByKey    map[gltfTextureKey]*GPUTexture
	samplers        []*GPUSampler
	samplerByIndex  map[int]*GPUSampler
}

// UploadGLTFPBRLightmaps inspects every material in doc via [gltf.Document.MaterialLightmap] (MOZ_lightmap, EXT_lightmap, extras.lightmap), deduplicates and uploads referenced lightmap images and samplers onto dev, and builds one [GPUPBRLightmap] per material (plus a default unlightmapped binding for primitives with Material == -1) sharing a single [PBRLightmapGroup] layout.
//
// If any material, lightmap extension, sampler, or image fails validation, decoding, or GPU upload, every GPU resource allocated during the call is closed before returning the error.
func UploadGLTFPBRLightmaps(cx *core.Scope, dev *gpu.Device, doc *gltf.Document, opts GLTFPBRLightmapUpload) (*GLTFPBRLightmapSet, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if doc == nil {
		return nil, fmt.Errorf("%w: nil glTF document", ErrInvalidLightmap)
	}
	if !opts.DefaultEncoding.Valid() {
		return nil, fmt.Errorf("%w: invalid DefaultEncoding %d", ErrInvalidLightmap, opts.DefaultEncoding)
	}
	if opts.SharedLayout != nil && !opts.SharedLayout.Valid() {
		return nil, fmt.Errorf("%w: shared lightmap bind group layout is not valid", ErrInvalidLightmap)
	}

	prefix := opts.Label
	if prefix == "" {
		prefix = "render3d.gltf.lightmap"
	}

	type parsedMatLM struct {
		ref      *gltf.LightmapRef
		encoding LightmapEncoding
		texIdx   int
		imgIdx   int
		sampIdx  int
		srgb     bool
		present  bool
	}
	type plannedSampler struct {
		index       int
		opts        SamplerOptions
		wantMipmaps bool
	}
	type plannedTexture struct {
		key     gltfTextureKey
		mipmaps bool
	}

	parsed := make([]parsedMatLM, len(doc.Materials))
	samplerOrder := make([]int, 0, len(doc.Samplers))
	samplerPlans := make(map[int]plannedSampler, len(doc.Samplers))
	texOrder := make([]gltfTextureKey, 0, len(doc.Images))
	texPlans := make(map[gltfTextureKey]*plannedTexture, len(doc.Images))

	// Pass 1: validate every material's lightmap extension, encoding, sampler, and image index before allocating any GPU resources.
	for i := range doc.Materials {
		ref, err := doc.MaterialLightmap(i)
		if err != nil {
			return nil, fmt.Errorf("%w: glTF material %d: %w", ErrInvalidLightmap, i, err)
		}
		if ref == nil {
			parsed[i] = parsedMatLM{texIdx: -1, imgIdx: -1, sampIdx: -1}
			continue
		}
		enc := opts.DefaultEncoding
		if ref.Encoding != "" {
			enc, err = ParseLightmapEncoding(ref.Encoding)
			if err != nil {
				return nil, fmt.Errorf("gltf material %d: %w", i, err)
			}
		}
		texIdx := ref.Texture.TextureIndex
		if texIdx < 0 || texIdx >= len(doc.Textures) {
			return nil, fmt.Errorf("%w: glTF material %d lightmap texture index %d out of range [0, %d)",
				ErrInvalidLightmap, i, texIdx, len(doc.Textures))
		}
		tex := doc.Textures[texIdx]
		if tex.Source == -1 {
			parsed[i] = parsedMatLM{ref: ref, encoding: enc, texIdx: texIdx, imgIdx: -1, sampIdx: tex.Sampler}
			continue
		}
		if tex.Source < -1 || tex.Source >= len(doc.Images) {
			return nil, fmt.Errorf("%w: glTF material %d lightmap texture %d image index %d out of range [0, %d)",
				ErrInvalidLightmap, i, texIdx, tex.Source, len(doc.Images))
		}
		if tex.Sampler < -1 || tex.Sampler >= len(doc.Samplers) {
			return nil, fmt.Errorf("%w: glTF material %d lightmap texture %d sampler index %d out of range [-1, %d)",
				ErrInvalidLightmap, i, texIdx, tex.Sampler, len(doc.Samplers))
		}

		wantMipmaps := false
		if tex.Sampler >= 0 {
			ps, ok := samplerPlans[tex.Sampler]
			if !ok {
				gs := doc.Samplers[tex.Sampler]
				so, wm, err := SamplerOptionsFromGLTF(gs)
				if err != nil {
					return nil, fmt.Errorf("gltf lightmap sampler %d: %w", tex.Sampler, err)
				}
				so.Retained = opts.Retained
				label := fmt.Sprintf("%s.sampler.%d", prefix, tex.Sampler)
				if so.Label != "" {
					label = fmt.Sprintf("%s.%s", label, so.Label)
				}
				so.Label = label
				ps = plannedSampler{index: tex.Sampler, opts: so, wantMipmaps: wm}
				samplerPlans[tex.Sampler] = ps
				samplerOrder = append(samplerOrder, tex.Sampler)
			}
			wantMipmaps = ps.wantMipmaps
		}

		srgb := enc == LightmapSRGB
		canMip := enc == LightmapLinear || enc == LightmapSRGB
		useMips := canMip && !opts.DisableMipmaps && wantMipmaps
		key := gltfTextureKey{imageIndex: tex.Source, srgb: srgb}
		if tp, ok := texPlans[key]; ok {
			if useMips {
				tp.mipmaps = true
			}
		} else {
			texPlans[key] = &plannedTexture{key: key, mipmaps: useMips}
			texOrder = append(texOrder, key)
		}

		parsed[i] = parsedMatLM{
			ref:      ref,
			encoding: enc,
			texIdx:   texIdx,
			imgIdx:   tex.Source,
			sampIdx:  tex.Sampler,
			srgb:     srgb,
			present:  true,
		}
	}

	set := &GLTFPBRLightmapSet{
		doc:            doc,
		lightmaps:      make([]GLTFPBRLightmapInfo, len(doc.Materials)),
		textureByKey:   make(map[gltfTextureKey]*GPUTexture, len(texOrder)),
		samplerByIndex: make(map[int]*GPUSampler, len(samplerOrder)),
	}
	cleanup := func(err error) (*GLTFPBRLightmapSet, error) {
		_ = set.Close()
		return nil, err
	}

	var err error
	if opts.SharedLayout != nil {
		set.layout = opts.SharedLayout
	} else {
		set.layout, err = PBRLightmapBindGroupLayout(cx, dev)
		if err != nil {
			return cleanup(err)
		}
		set.ownedLayout = true
	}

	set.fallbackTex, err = UploadTexturePixels(cx, dev, PixelBuffer{
		Width:  1,
		Height: 1,
		Format: gpu.FormatRGBA8Unorm,
		Pixels: []byte{255, 255, 255, 255},
	}, TextureUpload{
		Label:    prefix + ".fallback_white",
		Retained: opts.Retained,
	})
	if err != nil {
		return cleanup(err)
	}

	defSampOpts := SamplerLinearClamp()
	defSampOpts.Label = prefix + ".sampler.default_clamp"
	defSampOpts.Retained = opts.Retained
	set.defaultSampler, err = NewGPUSampler(cx, dev, defSampOpts)
	if err != nil {
		return cleanup(err)
	}

	// Pass 2: create deduplicated glTF samplers.
	for _, sampIdx := range samplerOrder {
		ps := samplerPlans[sampIdx]
		gs, err := NewGPUSampler(cx, dev, ps.opts)
		if err != nil {
			return cleanup(fmt.Errorf("gltf lightmap sampler %d: %w", sampIdx, err))
		}
		set.samplers = append(set.samplers, gs)
		set.samplerByIndex[sampIdx] = gs
	}

	// Pass 3: decode each referenced image once and upload unique (imageIndex, sRGB) textures.
	decoded := make(map[int]image.Image, len(texOrder))
	for _, key := range texOrder {
		tp := texPlans[key]
		img, ok := decoded[key.imageIndex]
		if !ok {
			img, err = doc.DecodeImage(key.imageIndex)
			if err != nil {
				return cleanup(fmt.Errorf("%w: glTF lightmap image %d: %w", ErrInvalidTexture, key.imageIndex, err))
			}
			decoded[key.imageIndex] = img
		}
		space := "linear"
		if key.srgb {
			space = "srgb"
		}
		texLabel := fmt.Sprintf("%s.image.%d.%s", prefix, key.imageIndex, space)
		if name := doc.Images[key.imageIndex].Name; name != "" {
			texLabel = fmt.Sprintf("%s.%s", texLabel, name)
		}
		gt, err := UploadTexture(cx, dev, img, TextureUpload{
			Label:        texLabel,
			SRGB:         key.srgb,
			Mipmaps:      tp.mipmaps,
			MaxMipLevels: opts.MaxMipLevels,
			Retained:     opts.Retained,
		})
		if err != nil {
			return cleanup(fmt.Errorf("gltf lightmap image %d (%s): %w", key.imageIndex, space, err))
		}
		set.textures = append(set.textures, gt)
		set.textureByKey[key] = gt
	}

	// Pass 4: create default unlightmapped binding and one GPUPBRLightmap per material.
	set.defaultLightmap, err = NewGPUPBRLightmap(cx, dev, DefaultPBRLightmap(), PBRLightmapOptions{
		Label:           prefix + ".default",
		SharedLayout:    set.layout,
		FallbackTexture: set.fallbackTex,
		FallbackSampler: set.defaultSampler,
		Retained:        opts.Retained,
	})
	if err != nil {
		return cleanup(err)
	}

	for i, pm := range parsed {
		cfg := DefaultPBRLightmap()
		var gt *GPUTexture
		var gs *GPUSampler
		if pm.present {
			gt = set.textureByKey[gltfTextureKey{imageIndex: pm.imgIdx, srgb: pm.srgb}]
			gs = set.defaultSampler
			if pm.sampIdx >= 0 {
				gs = set.samplerByIndex[pm.sampIdx]
			}
			cfg.Lightmap = gt
			cfg.LightmapSampler = gs
			cfg.Encoding = pm.encoding
			cfg.Intensity = pm.ref.Intensity
			cfg.RGBMMaxRange = pm.ref.RGBMMaxRange
			cfg.UVTransform = UVTransform{Scale: pm.ref.UVScale, Offset: pm.ref.UVOffset}.Normalized()
		}
		lmLabel := fmt.Sprintf("%s.material.%d", prefix, i)
		if name := doc.Materials[i].Name; name != "" {
			lmLabel = fmt.Sprintf("%s.%s", lmLabel, name)
		}
		gpuLM, err := NewGPUPBRLightmap(cx, dev, cfg, PBRLightmapOptions{
			Label:           lmLabel,
			SharedLayout:    set.layout,
			FallbackTexture: set.fallbackTex,
			FallbackSampler: set.defaultSampler,
			Retained:        opts.Retained,
		})
		if err != nil {
			return cleanup(fmt.Errorf("gltf material %d lightmap: %w", i, err))
		}
		var refCopy *gltf.LightmapRef
		if pm.ref != nil {
			cp := *pm.ref
			if cp.Texture.Transform != nil {
				tr := *cp.Texture.Transform
				if tr.TexCoord != nil {
					tc := *tr.TexCoord
					tr.TexCoord = &tc
				}
				cp.Texture.Transform = &tr
			}
			refCopy = &cp
		}
		set.lightmaps[i] = GLTFPBRLightmapInfo{
			HasLightmap:  pm.present,
			Ref:          refCopy,
			Encoding:     pm.encoding,
			TextureIndex: pm.texIdx,
			ImageIndex:   pm.imgIdx,
			SamplerIndex: pm.sampIdx,
			Texture:      gt,
			Sampler:      gs,
			Config:       cfg,
			GPU:          gpuLM,
		}
	}

	return set, nil
}

// Layout returns the shared [PBRLightmapGroup] bind group layout to pass to [NewPBRLightmapPipeline].
func (s *GLTFPBRLightmapSet) Layout() *gpu.BindGroupLayout {
	if s == nil {
		return nil
	}
	return s.layout
}

// Len returns the number of document materials tracked by s (excluding [GLTFPBRLightmapSet.DefaultLightmap]).
func (s *GLTFPBRLightmapSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.lightmaps)
}

// DefaultLightmap returns the fallback unlightmapped [*GPUPBRLightmap] used for primitives with Material == -1.
func (s *GLTFPBRLightmapSet) DefaultLightmap() *GPUPBRLightmap {
	if s == nil {
		return nil
	}
	return s.defaultLightmap
}

// Lightmap returns the [*GPUPBRLightmap] for glTF material index, or [GLTFPBRLightmapSet.DefaultLightmap] when index == -1. Out-of-range indices and nil receivers return nil.
func (s *GLTFPBRLightmapSet) Lightmap(index int) *GPUPBRLightmap {
	if s == nil {
		return nil
	}
	if index == -1 {
		return s.defaultLightmap
	}
	if index < 0 || index >= len(s.lightmaps) {
		return nil
	}
	return s.lightmaps[index].GPU
}

// Info returns the full [GLTFPBRLightmapInfo] for glTF material index.
func (s *GLTFPBRLightmapSet) Info(index int) (GLTFPBRLightmapInfo, bool) {
	if s == nil || index < 0 || index >= len(s.lightmaps) {
		return GLTFPBRLightmapInfo{}, false
	}
	return s.lightmaps[index], true
}

// LightmapForPrimitive resolves the active [*GPUPBRLightmap] for prim under the KHR_materials_variants variantIndex.
func (s *GLTFPBRLightmapSet) LightmapForPrimitive(prim gltf.Primitive, variantIndex int) *GPUPBRLightmap {
	if s == nil || s.doc == nil {
		return nil
	}
	return s.Lightmap(s.doc.MaterialForPrimitive(prim, variantIndex))
}

// LightmapForVariant resolves the active [*GPUPBRLightmap] for prim under the named KHR_materials_variants variant.
func (s *GLTFPBRLightmapSet) LightmapForVariant(prim gltf.Primitive, variantName string) *GPUPBRLightmap {
	if s == nil || s.doc == nil {
		return nil
	}
	return s.Lightmap(s.doc.MaterialForVariant(prim, variantName))
}

// NewInstance creates an independent [*GPUPBRLightmap] for materialIndex with its own Model matrix while reusing s's shared bind group layout, lightmap texture, and sampler. Useful when multiple scene nodes share one glTF material at different world transforms.
func (s *GLTFPBRLightmapSet) NewInstance(cx *core.Scope, dev *gpu.Device, materialIndex int, model math3d.Mat4, opts PBRLightmapOptions) (*GPUPBRLightmap, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("%w: glTF lightmap set is not valid", ErrInvalidLightmap)
	}
	cfg := DefaultPBRLightmap()
	if materialIndex != -1 {
		info, ok := s.Info(materialIndex)
		if !ok {
			return nil, fmt.Errorf("%w: glTF material index %d out of range [-1, %d)", ErrInvalidLightmap, materialIndex, len(s.lightmaps))
		}
		cfg = info.Config
	}
	cfg.Model = model
	if opts.SharedLayout == nil {
		opts.SharedLayout = s.layout
	}
	if opts.FallbackTexture == nil {
		opts.FallbackTexture = s.fallbackTex
	}
	if opts.FallbackSampler == nil {
		opts.FallbackSampler = s.defaultSampler
	}
	return NewGPUPBRLightmap(cx, dev, cfg, opts)
}

// TextureCount returns the number of unique baked lightmap textures uploaded from the document (excluding the internal 1x1 fallback).
func (s *GLTFPBRLightmapSet) TextureCount() int {
	if s == nil {
		return 0
	}
	return len(s.textures)
}

// SamplerCount returns the number of deduplicated glTF lightmap samplers uploaded from the document (excluding the internal clamp default sampler).
func (s *GLTFPBRLightmapSet) SamplerCount() int {
	if s == nil {
		return 0
	}
	return len(s.samplers)
}

// ByteSize returns the total GPU memory held by all uploaded lightmap textures, fallback texture, and uniform buffers in s.
func (s *GLTFPBRLightmapSet) ByteSize() uint64 {
	if s == nil {
		return 0
	}
	var total uint64
	if s.fallbackTex != nil {
		total += s.fallbackTex.ByteSize()
	}
	if s.defaultLightmap != nil {
		total += s.defaultLightmap.ByteSize()
	}
	for i := range s.lightmaps {
		total += s.lightmaps[i].GPU.ByteSize()
	}
	for _, t := range s.textures {
		total += t.ByteSize()
	}
	return total
}

// Valid reports whether every layout, texture, sampler, and lightmap binding owned by s is open on a live device.
func (s *GLTFPBRLightmapSet) Valid() bool {
	if s == nil || s.layout == nil || !s.layout.Valid() || !s.fallbackTex.Valid() || !s.defaultSampler.Valid() || !s.defaultLightmap.Valid() {
		return false
	}
	for i := range s.lightmaps {
		if !s.lightmaps[i].GPU.Valid() {
			return false
		}
	}
	for _, t := range s.textures {
		if !t.Valid() {
			return false
		}
	}
	for _, samp := range s.samplers {
		if !samp.Valid() {
			return false
		}
	}
	return true
}

// Close releases every GPU resource owned by s immediately instead of waiting for scope disposal. Safe to call on nil or more than once.
func (s *GLTFPBRLightmapSet) Close() error {
	if s == nil {
		return nil
	}
	var err error
	for i := range s.lightmaps {
		if s.lightmaps[i].GPU != nil {
			err = errors.Join(err, s.lightmaps[i].GPU.Close())
		}
	}
	if s.defaultLightmap != nil {
		err = errors.Join(err, s.defaultLightmap.Close())
	}
	for _, samp := range s.samplers {
		if samp != nil {
			err = errors.Join(err, samp.Close())
		}
	}
	if s.defaultSampler != nil {
		err = errors.Join(err, s.defaultSampler.Close())
	}
	for _, t := range s.textures {
		if t != nil {
			err = errors.Join(err, t.Close())
		}
	}
	if s.fallbackTex != nil {
		err = errors.Join(err, s.fallbackTex.Close())
	}
	if s.ownedLayout && s.layout != nil {
		err = errors.Join(err, s.layout.Close())
	}
	return err
}
