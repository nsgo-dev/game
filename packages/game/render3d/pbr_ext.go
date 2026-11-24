package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Bind group slots and uniform block sizes for [ExtendedPBRRenderer] and [PBRExtWGSL].
const (
	// PBRExtSceneGroup is the bind group slot for the per-object transform and lighting uniform block (alongside [CameraGroup] at 0).
	PBRExtSceneGroup = 1
	// PBRExtMaterialGroup is the bind group slot for the extended PBR material uniform, samplers, and textures.
	PBRExtMaterialGroup = 2

	// PBRExtSceneUniformSize is the byte size of the scene/object uniform block encoded by [PBRExtLighting.EncodeSceneUniform].
	PBRExtSceneUniformSize = 176
	// PBRExtMaterialUniformSize is the byte size of the material uniform block encoded by [PBRMaterialExt.EncodeUniform].
	PBRExtMaterialUniformSize = 80
)

// ErrInvalidPBRExtMaterial is wrapped by validation errors returned by [PBRMaterialExt.Validate] and [ExtendedPBRRenderer.CreateMaterial].
var ErrInvalidPBRExtMaterial = errors.New("render3d: invalid extended PBR material")

// PBRToneMap selects how [ExtendedPBRRenderer] and [EvaluatePBRMaterialExt] encode linear HDR radiance to the render target.
type PBRToneMap uint32

const (
	// PBRToneMapNone outputs raw linear HDR radiance without tone mapping or gamma encoding (preserves values > 1.0 on float targets such as FormatRGBA16Float).
	PBRToneMapNone PBRToneMap = iota
	// PBRToneMapLinearSRGB clamps linear radiance to [0, 1] and applies IEC 61966-2-1 sRGB encoding.
	PBRToneMapLinearSRGB
	// PBRToneMapACES applies Narkowicz ACES Filmic HDR tone mapping followed by IEC 61966-2-1 sRGB encoding.
	PBRToneMapACES
	// PBRToneMapReinhard applies Reinhard tone mapping (c / (1 + c)) followed by IEC 61966-2-1 sRGB encoding.
	PBRToneMapReinhard
)

// PBRExtWGSL is the WGSL shader module for [ExtendedPBRRenderer]. It implements
// Khronos glTF 2.0 metallic-roughness PBR together with:
//   - KHR_materials_emissive_strength (unbounded linear HDR emissive radiance)
//   - KHR_materials_clearcoat (dual-lobe GGX dielectric coat, IOR 1.5 / F0 = 0.04, Fresnel base attenuation, optional coat factor/roughness/normal maps)
//   - KHR_materials_transmission (dielectric diffuse-to-transmission blending over a screen-space background texture with roughness-modulated filtering, preserving specular and clearcoat reflections)
//
// The shader avoids exp/exp2/log2 and screen-space derivatives so it executes
// identically across hardware GPU backends and NSGo's software GPU renderer.
const PBRExtWGSL = CameraWGSL + `
struct PBRExtScene {
	model: mat4x4<f32>,
	normal_col0: vec4<f32>,
	normal_col1: vec4<f32>,
	normal_col2: vec4<f32>,
	light_dir_exposure: vec4<f32>,
	light_color_tonemap: vec4<f32>,
	ambient_sky_vw: vec4<f32>,
	ambient_ground_vh: vec4<f32>,
}

struct PBRExtMaterial {
	base_color: vec4<f32>,
	mr_emissive_str: vec4<f32>,
	emissive_cc: vec4<f32>,
	cc_trans: vec4<f32>,
	flags: vec4<f32>,
}

@group(1) @binding(0) var<uniform> pbr_scene: PBRExtScene;

@group(2) @binding(0) var<uniform> pbr_mat: PBRExtMaterial;
@group(2) @binding(1) var pbr_mat_samp: sampler;
@group(2) @binding(2) var pbr_bg_samp: sampler;
@group(2) @binding(3) var pbr_base_color_tex: texture_2d<f32>;
@group(2) @binding(4) var pbr_mr_tex: texture_2d<f32>;
@group(2) @binding(5) var pbr_emissive_tex: texture_2d<f32>;
@group(2) @binding(6) var pbr_cc_tex: texture_2d<f32>;
@group(2) @binding(7) var pbr_cc_rough_tex: texture_2d<f32>;
@group(2) @binding(8) var pbr_cc_normal_tex: texture_2d<f32>;
@group(2) @binding(9) var pbr_trans_tex: texture_2d<f32>;
@group(2) @binding(10) var pbr_trans_bg_tex: texture_2d<f32>;

struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

const PBR_PI: f32 = 3.14159265359;

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	let world = pbr_scene.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world;
	out.world_pos = world.xyz;
	let wn = pbr_scene.normal_col0.xyz * in.normal.x +
		pbr_scene.normal_col1.xyz * in.normal.y +
		pbr_scene.normal_col2.xyz * in.normal.z;
	out.world_normal = wn;
	out.uv = in.uv;
	return out;
}

fn pbr_fresnel_schlick(f0: vec3<f32>, cos_theta: f32) -> vec3<f32> {
	let f = clamp(1.0 - cos_theta, 0.0, 1.0);
	let f2 = f * f;
	let f5 = f2 * f2 * f;
	return f0 + (vec3<f32>(1.0, 1.0, 1.0) - f0) * f5;
}

fn pbr_d_ggx(ndh: f32, alpha: f32) -> f32 {
	let a2 = alpha * alpha;
	let d = ndh * ndh * (a2 - 1.0) + 1.0;
	return a2 / max(PBR_PI * d * d, 1e-6);
}

fn pbr_v_ggx(ndl: f32, ndv: f32, alpha: f32) -> f32 {
	let a2 = alpha * alpha;
	let ggxv = ndl * sqrt(ndv * ndv * (1.0 - a2) + a2);
	let ggxl = ndv * sqrt(ndl * ndl * (1.0 - a2) + a2);
	return 0.5 / max(ggxv + ggxl, 1e-5);
}

fn pbr_perturb_clearcoat_normal(n: vec3<f32>, uv: vec2<f32>, scale: f32) -> vec3<f32> {
	let sample_rgb = textureSampleLevel(pbr_cc_normal_tex, pbr_mat_samp, uv, 0.0).rgb;
	let tn = normalize(vec3<f32>(
		(sample_rgb.x * 2.0 - 1.0) * scale,
		(sample_rgb.y * 2.0 - 1.0) * scale,
		max(sample_rgb.z * 2.0 - 1.0, 1e-4)
	));
	var up = vec3<f32>(0.0, 1.0, 0.0);
	if (abs(n.y) > 0.99) {
		up = vec3<f32>(1.0, 0.0, 0.0);
	}
	let t = normalize(cross(up, n));
	let b = cross(n, t);
	return normalize(t * tn.x + b * tn.y + n * tn.z);
}

fn pbr_sample_transmission_bg(screen_uv: vec2<f32>, roughness: f32, blur_radius: f32) -> vec3<f32> {
	let r = clamp(roughness * roughness * blur_radius, 0.0, 0.08);
	let lo = vec2<f32>(0.0, 0.0);
	let hi = vec2<f32>(1.0, 1.0);
	let c0 = textureSampleLevel(pbr_trans_bg_tex, pbr_bg_samp, clamp(screen_uv, lo, hi), 0.0).rgb;
	let c1 = textureSampleLevel(pbr_trans_bg_tex, pbr_bg_samp, clamp(screen_uv + vec2<f32>(r, r), lo, hi), 0.0).rgb;
	let c2 = textureSampleLevel(pbr_trans_bg_tex, pbr_bg_samp, clamp(screen_uv + vec2<f32>(-r, r), lo, hi), 0.0).rgb;
	let c3 = textureSampleLevel(pbr_trans_bg_tex, pbr_bg_samp, clamp(screen_uv + vec2<f32>(r, -r), lo, hi), 0.0).rgb;
	let c4 = textureSampleLevel(pbr_trans_bg_tex, pbr_bg_samp, clamp(screen_uv + vec2<f32>(-r, -r), lo, hi), 0.0).rgb;
	return c0 * 0.4 + (c1 + c2 + c3 + c4) * 0.15;
}

fn pbr_linear_to_srgb_ch(c: f32) -> f32 {
	let x = clamp(c, 0.0, 1.0);
	if (x <= 0.0031308) {
		return x * 12.92;
	}
	return 1.055 * pow(x, 1.0 / 2.4) - 0.055;
}

fn pbr_linear_to_srgb(rgb: vec3<f32>) -> vec3<f32> {
	return vec3<f32>(
		pbr_linear_to_srgb_ch(rgb.x),
		pbr_linear_to_srgb_ch(rgb.y),
		pbr_linear_to_srgb_ch(rgb.z)
	);
}

fn pbr_encode_output(hdr: vec3<f32>, exposure: f32, tonemap: f32) -> vec3<f32> {
	let exposed = max(hdr * exposure, vec3<f32>(0.0, 0.0, 0.0));
	if (tonemap < 0.5) {
		return exposed;
	}
	if (tonemap < 1.5) {
		return pbr_linear_to_srgb(exposed);
	}
	if (tonemap < 2.5) {
		let a = exposed * (2.51 * exposed + vec3<f32>(0.03, 0.03, 0.03));
		let b = exposed * (2.43 * exposed + vec3<f32>(0.59, 0.59, 0.59)) + vec3<f32>(0.14, 0.14, 0.14);
		return pbr_linear_to_srgb(clamp(a / b, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0)));
	}
	let reinhard = exposed / (vec3<f32>(1.0, 1.0, 1.0) + exposed);
	return pbr_linear_to_srgb(reinhard);
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let n = normalize(in.world_normal);
	let v = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let l = normalize(pbr_scene.light_dir_exposure.xyz);
	let h = normalize(v + l);

	let ndv = max(dot(n, v), 1e-4);
	let ndl = max(dot(n, l), 0.0);
	let ndh = max(dot(n, h), 0.0);
	let vdh = max(dot(v, h), 0.0);

	// Sample material maps (all textureSampleLevel at mip 0 for uniform-safe control flow).
	let base_tex = textureSampleLevel(pbr_base_color_tex, pbr_mat_samp, in.uv, 0.0);
	let base_color = pbr_mat.base_color.rgb * base_tex.rgb;
	let alpha_out = pbr_mat.base_color.a * base_tex.a;

	let mr_tex = textureSampleLevel(pbr_mr_tex, pbr_mat_samp, in.uv, 0.0);
	let metallic = clamp(pbr_mat.mr_emissive_str.x * mr_tex.b, 0.0, 1.0);
	let roughness = clamp(pbr_mat.mr_emissive_str.y * mr_tex.g, 0.04, 1.0);
	let alpha = roughness * roughness;

	let emissive_tex = textureSampleLevel(pbr_emissive_tex, pbr_mat_samp, in.uv, 0.0).rgb;
	let emissive_strength = max(pbr_mat.mr_emissive_str.z, 0.0);
	let emissive = pbr_mat.emissive_cc.xyz * emissive_tex * emissive_strength;

	let cc_tex = textureSampleLevel(pbr_cc_tex, pbr_mat_samp, in.uv, 0.0).r;
	let cc_rough_tex = textureSampleLevel(pbr_cc_rough_tex, pbr_mat_samp, in.uv, 0.0).g;
	let clearcoat = clamp(pbr_mat.emissive_cc.w * cc_tex, 0.0, 1.0);
	let cc_roughness = clamp(pbr_mat.cc_trans.x * cc_rough_tex, 0.04, 1.0);
	let cc_alpha = cc_roughness * cc_roughness;

	let trans_tex = textureSampleLevel(pbr_trans_tex, pbr_mat_samp, in.uv, 0.0).r;
	let transmission = clamp(pbr_mat.cc_trans.z * trans_tex, 0.0, 1.0);

	let light_rad = pbr_scene.light_color_tonemap.xyz;
	let hemi_t = clamp(n.y * 0.5 + 0.5, 0.0, 1.0);
	let ambient_env = mix(pbr_scene.ambient_ground_vh.xyz, pbr_scene.ambient_sky_vw.xyz, hemi_t);

	// Base glTF 2.0 metallic-roughness BRDF.
	let f0_dielectric = vec3<f32>(0.04, 0.04, 0.04);
	let f_diel_l = pbr_fresnel_schlick(f0_dielectric, vdh);
	let f_diel_v = pbr_fresnel_schlick(f0_dielectric, ndv);
	let f_metal_l = pbr_fresnel_schlick(base_color, vdh);
	let f_metal_v = pbr_fresnel_schlick(base_color, ndv);

	let d_base = pbr_d_ggx(ndh, alpha);
	let v_base = pbr_v_ggx(ndl, ndv, alpha);
	let dv_base = d_base * v_base;

	// Dielectric opaque diffuse vs transmitted background (KHR_materials_transmission).
	let opaque_diff = ((vec3<f32>(1.0, 1.0, 1.0) - f_diel_l) * (base_color / PBR_PI) * light_rad * ndl) +
		((vec3<f32>(1.0, 1.0, 1.0) - f_diel_v) * base_color * ambient_env);

	let viewport = max(vec2<f32>(pbr_scene.ambient_sky_vw.w, pbr_scene.ambient_ground_vh.w), vec2<f32>(1.0, 1.0));
	let screen_uv = in.clip.xy / viewport;
	let bg_color = pbr_sample_transmission_bg(screen_uv, roughness, pbr_mat.cc_trans.w);
	let transmitted = (vec3<f32>(1.0, 1.0, 1.0) - f_diel_v) * base_color * bg_color;

	let diel_diffuse_or_btdf = mix(opaque_diff, transmitted, transmission);
	let diel_spec_direct = f_diel_l * dv_base * light_rad * ndl;
	let diel_spec_ambient = f_diel_v * ambient_env * (1.0 - roughness * 0.5);
	let dielectric_total = diel_diffuse_or_btdf + diel_spec_direct + diel_spec_ambient;

	let metal_spec_direct = f_metal_l * dv_base * light_rad * ndl;
	let metal_spec_ambient = f_metal_v * ambient_env * (1.0 - roughness * 0.5);
	let metal_total = metal_spec_direct + metal_spec_ambient;

	var color = mix(dielectric_total, metal_total, metallic) + emissive;

	// KHR_materials_clearcoat: dielectric coat (IOR 1.5, F0 = 0.04) layered over base.
	var nc = n;
	if (pbr_mat.mr_emissive_str.w > 0.5) {
		nc = pbr_perturb_clearcoat_normal(n, in.uv, pbr_mat.cc_trans.y);
	}
	let ncdv = max(dot(nc, v), 1e-4);
	let ncdl = max(dot(nc, l), 0.0);
	let ncdh = max(dot(nc, h), 0.0);

	let f_coat_l = pbr_fresnel_schlick(f0_dielectric, vdh);
	let f_coat_v = pbr_fresnel_schlick(f0_dielectric, ncdv);
	let cc_spec_direct = f_coat_l * pbr_d_ggx(ncdh, cc_alpha) * pbr_v_ggx(ncdl, ncdv, cc_alpha) * light_rad * ncdl;
	let cc_spec_ambient = f_coat_v * ambient_env * (1.0 - cc_roughness * 0.5);

	// Energy-conserving Fresnel attenuation of base layer under clearcoat.
	let coat_atten = 1.0 - clearcoat * f_coat_v.x;
	color = color * coat_atten + clearcoat * (cc_spec_direct + cc_spec_ambient);

	let out_rgb = pbr_encode_output(color, pbr_scene.light_dir_exposure.w, pbr_scene.light_color_tonemap.w);
	return vec4<f32>(out_rgb, alpha_out);
}
`

// PBRMaterialExt holds glTF 2.0 metallic-roughness parameters extended with
// KHR_materials_emissive_strength, KHR_materials_clearcoat, and
// KHR_materials_transmission, plus optional GPU textures.
type PBRMaterialExt struct {
	// BaseColorFactor is the linear RGBA base color multiplier (default (1, 1, 1, 1)).
	BaseColorFactor math3d.Vec4
	// MetallicFactor is in [0, 1] (default 0).
	MetallicFactor float32
	// RoughnessFactor is in [0, 1] (default 1).
	RoughnessFactor float32
	// EmissiveFactor is the linear RGB emissive multiplier (default (0, 0, 0)).
	EmissiveFactor math3d.Vec3

	// EmissiveStrength is the KHR_materials_emissive_strength multiplier (>= 0, default 1).
	EmissiveStrength float32

	// ClearcoatFactor is KHR_materials_clearcoat.clearcoatFactor in [0, 1] (default 0).
	ClearcoatFactor float32
	// ClearcoatRoughnessFactor is KHR_materials_clearcoat.clearcoatRoughnessFactor in [0, 1] (default 0).
	ClearcoatRoughnessFactor float32
	// ClearcoatNormalScale scales the tangent-space X/Y components of ClearcoatNormalTexture (default 1).
	ClearcoatNormalScale float32

	// TransmissionFactor is KHR_materials_transmission.transmissionFactor in [0, 1] (default 0).
	TransmissionFactor float32
	// TransmissionBlur controls the screen-space UV blur radius at roughness=1 when sampling TransmissionBackground (default 0.015).
	TransmissionBlur float32

	// Optional GPU textures. Any nil texture falls back to a neutral 1x1 default.
	BaseColorTexture          *GPUTexture
	MetallicRoughnessTexture  *GPUTexture // G = roughness, B = metallic
	EmissiveTexture           *GPUTexture
	ClearcoatTexture          *GPUTexture // R = clearcoatFactor multiplier
	ClearcoatRoughnessTexture *GPUTexture // G = clearcoatRoughnessFactor multiplier
	ClearcoatNormalTexture    *GPUTexture // tangent-space normal map
	TransmissionTexture       *GPUTexture // R = transmissionFactor multiplier
	// TransmissionBackground is the screen-space opaque scene color texture sampled when TransmissionFactor > 0.
	// Pass a gpu.RenderTarget.Color or a GPUTexture.Texture() here; nil falls back to black.
	TransmissionBackground *gpu.Texture
}

// DefaultPBRMaterialExt returns a physically valid opaque dielectric material
// with neutral defaults (white base color, roughness 1, emissive strength 1,
// clearcoat 0, transmission 0).
func DefaultPBRMaterialExt() PBRMaterialExt {
	return PBRMaterialExt{
		BaseColorFactor:      math3d.Vec4One(),
		RoughnessFactor:      1,
		EmissiveStrength:     1,
		ClearcoatNormalScale: 1,
		TransmissionBlur:     0.015,
	}
}

// PBRMaterialExtFromGLTF converts a parsed [gltf.Material] (including its
// KHR_materials_emissive_strength, KHR_materials_clearcoat, and
// KHR_materials_transmission fields) into a [PBRMaterialExt]. Callers attach
// uploaded GPU textures to the returned struct before calling
// [ExtendedPBRRenderer.CreateMaterial].
func PBRMaterialExtFromGLTF(m gltf.Material) PBRMaterialExt {
	out := DefaultPBRMaterialExt()
	out.BaseColorFactor = m.BaseColorFactor
	out.MetallicFactor = m.MetallicFactor
	out.RoughnessFactor = m.RoughnessFactor
	out.EmissiveFactor = m.EmissiveFactor
	out.EmissiveStrength = m.EmissiveStrength
	if m.Clearcoat != nil {
		out.ClearcoatFactor = m.Clearcoat.Factor
		out.ClearcoatRoughnessFactor = m.Clearcoat.RoughnessFactor
		if m.Clearcoat.NormalTexture != nil {
			out.ClearcoatNormalScale = m.Clearcoat.NormalTexture.Scale
		}
	}
	if m.Transmission != nil {
		out.TransmissionFactor = m.Transmission.Factor
	}
	return out
}

// Validate checks that every scalar factor is finite and within its Khronos
// specification range, and that any non-nil GPU texture handles are valid.
// Errors wrap [ErrInvalidPBRExtMaterial].
func (m PBRMaterialExt) Validate() error {
	if !finite(m.BaseColorFactor.X) || !finite(m.BaseColorFactor.Y) || !finite(m.BaseColorFactor.Z) || !finite(m.BaseColorFactor.W) {
		return fmt.Errorf("%w: base color factor is not finite", ErrInvalidPBRExtMaterial)
	}
	if !finite(m.MetallicFactor) || m.MetallicFactor < 0 || m.MetallicFactor > 1 {
		return fmt.Errorf("%w: metallic factor %v must be in [0, 1]", ErrInvalidPBRExtMaterial, m.MetallicFactor)
	}
	if !finite(m.RoughnessFactor) || m.RoughnessFactor < 0 || m.RoughnessFactor > 1 {
		return fmt.Errorf("%w: roughness factor %v must be in [0, 1]", ErrInvalidPBRExtMaterial, m.RoughnessFactor)
	}
	if !finite3(m.EmissiveFactor) {
		return fmt.Errorf("%w: emissive factor is not finite", ErrInvalidPBRExtMaterial)
	}
	if !finite(m.EmissiveStrength) || m.EmissiveStrength < 0 {
		return fmt.Errorf("%w: emissive strength %v must be finite and >= 0", ErrInvalidPBRExtMaterial, m.EmissiveStrength)
	}
	if !finite(m.ClearcoatFactor) || m.ClearcoatFactor < 0 || m.ClearcoatFactor > 1 {
		return fmt.Errorf("%w: clearcoat factor %v must be in [0, 1]", ErrInvalidPBRExtMaterial, m.ClearcoatFactor)
	}
	if !finite(m.ClearcoatRoughnessFactor) || m.ClearcoatRoughnessFactor < 0 || m.ClearcoatRoughnessFactor > 1 {
		return fmt.Errorf("%w: clearcoat roughness factor %v must be in [0, 1]", ErrInvalidPBRExtMaterial, m.ClearcoatRoughnessFactor)
	}
	if !finite(m.ClearcoatNormalScale) {
		return fmt.Errorf("%w: clearcoat normal scale is not finite", ErrInvalidPBRExtMaterial)
	}
	if !finite(m.TransmissionFactor) || m.TransmissionFactor < 0 || m.TransmissionFactor > 1 {
		return fmt.Errorf("%w: transmission factor %v must be in [0, 1]", ErrInvalidPBRExtMaterial, m.TransmissionFactor)
	}
	if !finite(m.TransmissionBlur) || m.TransmissionBlur < 0 {
		return fmt.Errorf("%w: transmission blur %v must be finite and >= 0", ErrInvalidPBRExtMaterial, m.TransmissionBlur)
	}
	for _, tc := range []struct {
		name string
		tex  *GPUTexture
	}{
		{"base color", m.BaseColorTexture},
		{"metallic-roughness", m.MetallicRoughnessTexture},
		{"emissive", m.EmissiveTexture},
		{"clearcoat", m.ClearcoatTexture},
		{"clearcoat roughness", m.ClearcoatRoughnessTexture},
		{"clearcoat normal", m.ClearcoatNormalTexture},
		{"transmission", m.TransmissionTexture},
	} {
		if tc.tex != nil && !tc.tex.Valid() {
			return fmt.Errorf("%w: %s texture is not valid", ErrInvalidPBRExtMaterial, tc.name)
		}
	}
	if m.TransmissionBackground != nil && !m.TransmissionBackground.Valid() {
		return fmt.Errorf("%w: transmission background texture is not valid", ErrInvalidPBRExtMaterial)
	}
	return nil
}

// EncodeUniform writes m's uniform block into dst as [PBRExtMaterialUniformSize]
// little-endian bytes matching [PBRExtWGSL]. It panics if len(dst) is smaller
// than [PBRExtMaterialUniformSize].
func (m PBRMaterialExt) EncodeUniform(dst []byte) []byte {
	if len(dst) < PBRExtMaterialUniformSize {
		panic("render3d: PBRMaterialExt.EncodeUniform: dst shorter than PBRExtMaterialUniformSize")
	}
	dst = dst[:PBRExtMaterialUniformSize]
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
		off += 4
	}
	var hasCCNormal float32
	if m.ClearcoatNormalTexture != nil {
		hasCCNormal = 1
	}
	// base_color
	put(m.BaseColorFactor.X)
	put(m.BaseColorFactor.Y)
	put(m.BaseColorFactor.Z)
	put(m.BaseColorFactor.W)
	// mr_emissive_str
	put(m.MetallicFactor)
	put(m.RoughnessFactor)
	put(m.EmissiveStrength)
	put(hasCCNormal)
	// emissive_cc
	put(m.EmissiveFactor.X)
	put(m.EmissiveFactor.Y)
	put(m.EmissiveFactor.Z)
	put(m.ClearcoatFactor)
	// cc_trans
	put(m.ClearcoatRoughnessFactor)
	put(m.ClearcoatNormalScale)
	put(m.TransmissionFactor)
	put(m.TransmissionBlur)
	// flags (reserved)
	put(0)
	put(0)
	put(0)
	put(0)
	return dst
}

// PBRExtLighting configures directional and hemisphere ambient illumination,
// exposure, tone mapping, and viewport dimensions for [ExtendedPBRRenderer] and
// [EvaluatePBRMaterialExt].
type PBRExtLighting struct {
	// LightDir is the unit direction from the surface toward the directional light.
	LightDir math3d.Vec3
	// LightColor is the linear HDR directional light radiance.
	LightColor math3d.Vec3
	// AmbientSky is the linear upper-hemisphere ambient radiance.
	AmbientSky math3d.Vec3
	// AmbientGround is the linear lower-hemisphere ambient radiance.
	AmbientGround math3d.Vec3
	// Exposure multiplies linear radiance before tone mapping (<= 0 defaults to 1).
	Exposure float32
	// ToneMap controls HDR-to-output encoding.
	ToneMap PBRToneMap
	// ViewportWidth and ViewportHeight convert fragment positions to [0, 1] UVs for transmission background sampling.
	ViewportWidth  float32
	ViewportHeight float32
}

// DefaultPBRExtLighting returns balanced studio lighting for a viewport of
// width x height pixels.
func DefaultPBRExtLighting(width, height int) PBRExtLighting {
	return PBRExtLighting{
		LightDir:       math3d.Vec3{X: 0.4, Y: 0.7, Z: 0.6}.Norm(),
		LightColor:     math3d.Vec3{X: 2.5, Y: 2.5, Z: 2.5},
		AmbientSky:     math3d.Vec3{X: 0.25, Y: 0.28, Z: 0.32},
		AmbientGround:  math3d.Vec3{X: 0.1, Y: 0.1, Z: 0.12},
		Exposure:       1,
		ToneMap:        PBRToneMapACES,
		ViewportWidth:  float32(max(width, 1)),
		ViewportHeight: float32(max(height, 1)),
	}
}

// EncodeSceneUniform writes model, its upper-3x3 normal matrix, and l into dst
// as [PBRExtSceneUniformSize] little-endian bytes matching [PBRExtWGSL]. It
// panics if len(dst) is smaller than [PBRExtSceneUniformSize].
func (l PBRExtLighting) EncodeSceneUniform(model math3d.Mat4, dst []byte) []byte {
	if len(dst) < PBRExtSceneUniformSize {
		panic("render3d: PBRExtLighting.EncodeSceneUniform: dst shorter than PBRExtSceneUniformSize")
	}
	dst = dst[:PBRExtSceneUniformSize]
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
		off += 4
	}
	for _, f := range model {
		put(f)
	}
	nmat := pbrExtNormalMatrix(model)
	// normal_col0, normal_col1, normal_col2 (padded to vec4)
	put(nmat[0])
	put(nmat[1])
	put(nmat[2])
	put(0)
	put(nmat[3])
	put(nmat[4])
	put(nmat[5])
	put(0)
	put(nmat[6])
	put(nmat[7])
	put(nmat[8])
	put(0)

	dir := l.LightDir
	if dir.LenSq() > 1e-12 {
		dir = dir.Norm()
	} else {
		dir = math3d.Vec3{Y: 1}
	}
	exp := l.Exposure
	if exp <= 0 {
		exp = 1
	}
	vw := l.ViewportWidth
	if vw < 1 {
		vw = 1
	}
	vh := l.ViewportHeight
	if vh < 1 {
		vh = 1
	}
	put(dir.X)
	put(dir.Y)
	put(dir.Z)
	put(exp)

	put(l.LightColor.X)
	put(l.LightColor.Y)
	put(l.LightColor.Z)
	put(float32(l.ToneMap))

	put(l.AmbientSky.X)
	put(l.AmbientSky.Y)
	put(l.AmbientSky.Z)
	put(vw)

	put(l.AmbientGround.X)
	put(l.AmbientGround.Y)
	put(l.AmbientGround.Z)
	put(vh)

	return dst
}

// pbrExtNormalMatrix computes the column-major 3x3 inverse-transpose of model's
// upper-left 3x3 submatrix, falling back to the upper-left 3x3 if singular.
func pbrExtNormalMatrix(m math3d.Mat4) [9]float32 {
	a00, a10, a20 := m[0], m[1], m[2]
	a01, a11, a21 := m[4], m[5], m[6]
	a02, a12, a22 := m[8], m[9], m[10]

	c00 := a11*a22 - a12*a21
	c10 := -(a01*a22 - a02*a21)
	c20 := a01*a12 - a02*a11

	c01 := -(a10*a22 - a12*a20)
	c11 := a00*a22 - a02*a20
	c21 := -(a00*a12 - a02*a10)

	c02 := a10*a21 - a11*a20
	c12 := -(a00*a21 - a01*a20)
	c22 := a00*a11 - a01*a10

	det := a00*c00 + a01*c01 + a02*c02
	if math.Abs(float64(det)) < 1e-12 {
		return [9]float32{a00, a10, a20, a01, a11, a21, a02, a12, a22}
	}
	inv := 1.0 / det
	// Transpose of inverse = cofactor matrix / det, stored column-major.
	return [9]float32{
		c00 * inv, c01 * inv, c02 * inv,
		c10 * inv, c11 * inv, c12 * inv,
		c20 * inv, c21 * inv, c22 * inv,
	}
}

// PBRExtSample holds the per-fragment surface inputs for [EvaluatePBRMaterialExt].
type PBRExtSample struct {
	BaseColor          math3d.Vec3
	Metallic           float32
	Roughness          float32
	Emissive           math3d.Vec3
	EmissiveStrength   float32
	Clearcoat          float32
	ClearcoatRoughness float32
	Transmission       float32
	BackgroundColor    math3d.Vec3
	Normal             math3d.Vec3
	ClearcoatNormal    math3d.Vec3 // defaults to Normal when zero
	ViewDir            math3d.Vec3
}

// EvaluatePBRMaterialExt computes the linear HDR radiance and tone-mapped output
// for one shaded point using the exact Khronos glTF 2.0 metallic-roughness +
// KHR_materials_emissive_strength + KHR_materials_clearcoat +
// KHR_materials_transmission equations implemented in [PBRExtWGSL].
func EvaluatePBRMaterialExt(s PBRExtSample, light PBRExtLighting) (linearHDR, encoded math3d.Vec3) {
	const pi = float32(math.Pi)

	n := s.Normal.Norm()
	v := s.ViewDir.Norm()
	l := light.LightDir
	if l.LenSq() > 1e-12 {
		l = l.Norm()
	} else {
		l = math3d.Vec3{Y: 1}
	}
	h := v.Add(l).Norm()

	ndv := max(n.Dot(v), float32(1e-4))
	ndl := max(n.Dot(l), float32(0))
	ndh := max(n.Dot(h), float32(0))
	vdh := max(v.Dot(h), float32(0))

	metallic := math3d.Clamp(s.Metallic, 0, 1)
	roughness := math3d.Clamp(s.Roughness, 0.04, 1)
	alpha := roughness * roughness

	emissive := s.Emissive.Scale(max(s.EmissiveStrength, 0))
	clearcoat := math3d.Clamp(s.Clearcoat, 0, 1)
	ccRough := math3d.Clamp(s.ClearcoatRoughness, 0.04, 1)
	ccAlpha := ccRough * ccRough
	transmission := math3d.Clamp(s.Transmission, 0, 1)

	hemiT := math3d.Clamp(n.Y*0.5+0.5, 0, 1)
	ambientEnv := light.AmbientGround.Lerp(light.AmbientSky, hemiT)

	f0Diel := math3d.Vec3{X: 0.04, Y: 0.04, Z: 0.04}
	fDielL := pbrExtFresnel(f0Diel, vdh)
	fDielV := pbrExtFresnel(f0Diel, ndv)
	fMetalL := pbrExtFresnel(s.BaseColor, vdh)
	fMetalV := pbrExtFresnel(s.BaseColor, ndv)

	dvBase := pbrExtDGGX(ndh, alpha) * pbrExtVGGX(ndl, ndv, alpha)
	one := math3d.Vec3One()

	opaqueDiff := one.Sub(fDielL).Mul(s.BaseColor.Scale(1 / pi)).Mul(light.LightColor).Scale(ndl).Add(
		one.Sub(fDielV).Mul(s.BaseColor).Mul(ambientEnv),
	)
	transmitted := one.Sub(fDielV).Mul(s.BaseColor).Mul(s.BackgroundColor)
	dielDiffOrBTDF := opaqueDiff.Lerp(transmitted, transmission)

	dielSpecDirect := fDielL.Scale(dvBase * ndl).Mul(light.LightColor)
	dielSpecAmbient := fDielV.Mul(ambientEnv).Scale(1 - roughness*0.5)
	dielTotal := dielDiffOrBTDF.Add(dielSpecDirect).Add(dielSpecAmbient)

	metalSpecDirect := fMetalL.Scale(dvBase * ndl).Mul(light.LightColor)
	metalSpecAmbient := fMetalV.Mul(ambientEnv).Scale(1 - roughness*0.5)
	metalTotal := metalSpecDirect.Add(metalSpecAmbient)

	color := dielTotal.Lerp(metalTotal, metallic).Add(emissive)

	nc := n
	if s.ClearcoatNormal.LenSq() > 1e-12 {
		nc = s.ClearcoatNormal.Norm()
	}
	ncdv := max(nc.Dot(v), float32(1e-4))
	ncdl := max(nc.Dot(l), float32(0))
	ncdh := max(nc.Dot(h), float32(0))

	fCoatL := pbrExtFresnel(f0Diel, vdh)
	fCoatV := pbrExtFresnel(f0Diel, ncdv)
	ccSpecDirect := fCoatL.Scale(pbrExtDGGX(ncdh, ccAlpha) * pbrExtVGGX(ncdl, ncdv, ccAlpha) * ncdl).Mul(light.LightColor)
	ccSpecAmbient := fCoatV.Mul(ambientEnv).Scale(1 - ccRough*0.5)

	coatAtten := 1 - clearcoat*fCoatV.X
	color = color.Scale(coatAtten).Add(ccSpecDirect.Add(ccSpecAmbient).Scale(clearcoat))

	exp := light.Exposure
	if exp <= 0 {
		exp = 1
	}
	linearHDR = math3d.Vec3{
		X: max(color.X*exp, 0),
		Y: max(color.Y*exp, 0),
		Z: max(color.Z*exp, 0),
	}
	encoded = pbrExtEncodeOutput(linearHDR, light.ToneMap)
	return linearHDR, encoded
}

func pbrExtFresnel(f0 math3d.Vec3, cosTheta float32) math3d.Vec3 {
	f := math3d.Clamp(1-cosTheta, 0, 1)
	f2 := f * f
	f5 := f2 * f2 * f
	return math3d.Vec3{
		X: f0.X + (1-f0.X)*f5,
		Y: f0.Y + (1-f0.Y)*f5,
		Z: f0.Z + (1-f0.Z)*f5,
	}
}

func pbrExtDGGX(ndh, alpha float32) float32 {
	a2 := alpha * alpha
	d := ndh*ndh*(a2-1) + 1
	return a2 / max(float32(math.Pi)*d*d, float32(1e-6))
}

func pbrExtVGGX(ndl, ndv, alpha float32) float32 {
	a2 := alpha * alpha
	ggxv := ndl * float32(math.Sqrt(float64(ndv*ndv*(1-a2)+a2)))
	ggxl := ndv * float32(math.Sqrt(float64(ndl*ndl*(1-a2)+a2)))
	return 0.5 / max(ggxv+ggxl, float32(1e-5))
}

func pbrExtLinearToSRGBCh(c float32) float32 {
	x := math3d.Clamp(c, 0, 1)
	if x <= 0.0031308 {
		return x * 12.92
	}
	return float32(1.055*math.Pow(float64(x), 1.0/2.4) - 0.055)
}

func pbrExtEncodeOutput(exposed math3d.Vec3, mode PBRToneMap) math3d.Vec3 {
	switch mode {
	case PBRToneMapNone:
		return exposed
	case PBRToneMapLinearSRGB:
		return math3d.Vec3{
			X: pbrExtLinearToSRGBCh(exposed.X),
			Y: pbrExtLinearToSRGBCh(exposed.Y),
			Z: pbrExtLinearToSRGBCh(exposed.Z),
		}
	case PBRToneMapACES:
		aces := func(x float32) float32 {
			a := x * (2.51*x + 0.03)
			b := x*(2.43*x+0.59) + 0.14
			return pbrExtLinearToSRGBCh(math3d.Clamp(a/b, 0, 1))
		}
		return math3d.Vec3{X: aces(exposed.X), Y: aces(exposed.Y), Z: aces(exposed.Z)}
	default:
		reinhard := func(x float32) float32 {
			return pbrExtLinearToSRGBCh(x / (1 + x))
		}
		return math3d.Vec3{X: reinhard(exposed.X), Y: reinhard(exposed.Y), Z: reinhard(exposed.Z)}
	}
}

// ExtendedPBRRendererOptions configures [NewExtendedPBRRenderer].
type ExtendedPBRRendererOptions struct {
	// ColorFormat is the render pass color attachment format (defaults to dev.SurfaceFormat()).
	ColorFormat gpu.TextureFormat
	// DepthFormat is the render pass depth format (FormatUndefined disables depth testing).
	DepthFormat gpu.TextureFormat
	// CullMode sets triangle face culling (defaults to gpu.CullBack when left at zero? Note: gpu.CullNone is 0, so leave CullMode explicit).
	CullMode gpu.CullMode
	// Blend optionally enables color blending on the pipeline target.
	Blend *gpu.BlendState
	// Label names GPU resources in capture tools.
	Label string
}

// ExtendedPBRScene holds one per-object transform and lighting uniform binding
// at [PBRExtSceneGroup], allowing multiple objects with different transforms to
// be drawn inside a single render pass without overwriting one another.
type ExtendedPBRScene struct {
	dev     *gpu.Device
	buf     *gpu.Buffer
	group   *gpu.BindGroup
	scratch [PBRExtSceneUniformSize]byte
}

// Write updates the object transform and lighting uniforms for subsequent draws.
func (s *ExtendedPBRScene) Write(model math3d.Mat4, light PBRExtLighting) {
	if s == nil || !s.Valid() {
		return
	}
	s.dev.WriteBuffer(s.buf, 0, light.EncodeSceneUniform(model, s.scratch[:]))
}

// BindGroup returns the GPU bind group to bind at [PBRExtSceneGroup].
func (s *ExtendedPBRScene) BindGroup() *gpu.BindGroup {
	if s == nil {
		return nil
	}
	return s.group
}

// Valid reports whether the scene uniform buffer and bind group are live.
func (s *ExtendedPBRScene) Valid() bool {
	return s != nil && s.buf.Valid() && s.group.Valid()
}

// Close releases the scene uniform buffer and bind group.
func (s *ExtendedPBRScene) Close() error {
	if s == nil {
		return nil
	}
	var first error
	for _, c := range []interface{ Close() error }{s.group, s.buf} {
		if c != nil {
			if err := c.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// ExtendedPBRMaterial is a GPU-bound [PBRMaterialExt] ready to draw with
// [ExtendedPBRRenderer].
type ExtendedPBRMaterial struct {
	dev     *gpu.Device
	buf     *gpu.Buffer
	group   *gpu.BindGroup
	scratch [PBRExtMaterialUniformSize]byte
}

// Update validates mat and uploads its scalar/vector factors into the material
// uniform buffer (texture bindings stay as created; create a new material to
// rebind textures).
func (m *ExtendedPBRMaterial) Update(mat PBRMaterialExt) error {
	if m == nil || !m.Valid() {
		return fmt.Errorf("%w: material is not valid", ErrInvalidPBRExtMaterial)
	}
	if err := mat.Validate(); err != nil {
		return err
	}
	m.dev.WriteBuffer(m.buf, 0, mat.EncodeUniform(m.scratch[:]))
	return nil
}

// BindGroup returns the GPU bind group to bind at [PBRExtMaterialGroup].
func (m *ExtendedPBRMaterial) BindGroup() *gpu.BindGroup {
	if m == nil {
		return nil
	}
	return m.group
}

// Valid reports whether the material uniform buffer and bind group are live.
func (m *ExtendedPBRMaterial) Valid() bool {
	return m != nil && m.buf.Valid() && m.group.Valid()
}

// Close releases the material uniform buffer and bind group.
func (m *ExtendedPBRMaterial) Close() error {
	if m == nil {
		return nil
	}
	var first error
	for _, c := range []interface{ Close() error }{m.group, m.buf} {
		if c != nil {
			if err := c.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// ExtendedPBRRenderer compiles and owns the GPU pipeline, bind group layouts,
// samplers, and neutral 1x1 fallback textures for rendering glTF 2.0 PBR
// materials with KHR_materials_emissive_strength, KHR_materials_clearcoat, and
// KHR_materials_transmission on an NSGo [gpu.Device].
type ExtendedPBRRenderer struct {
	dev          *gpu.Device
	pipeline     *gpu.Pipeline
	sceneLayout  *gpu.BindGroupLayout
	matLayout    *gpu.BindGroupLayout
	defaultScene *ExtendedPBRScene
	matSampler   *GPUSampler
	bgSampler    *GPUSampler
	whiteTex     *GPUTexture
	blackTex     *GPUTexture
	flatNormTex  *GPUTexture
}

// NewExtendedPBRRenderer creates an [ExtendedPBRRenderer] on dev, scoped to cx,
// compatible with camLayout at [CameraGroup] (0) and [MeshVertexLayout].
func NewExtendedPBRRenderer(cx *core.Scope, dev *gpu.Device, camLayout *gpu.BindGroupLayout, opts ExtendedPBRRendererOptions) (*ExtendedPBRRenderer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}

	label := opts.Label
	if label == "" {
		label = "render3d.pbr_ext"
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}

	mod, err := gpu.CompileWGSL(PBRExtWGSL)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile extended PBR WGSL: %w", err)
	}

	r := &ExtendedPBRRenderer{dev: dev}
	cleanup := func(cause error) (*ExtendedPBRRenderer, error) {
		_ = r.Close()
		return nil, cause
	}

	r.sceneLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".scene",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageVertex | gpu.StageFragment,
			Type:       gpu.BindingUniform,
		}},
	})
	if err != nil {
		return cleanup(err)
	}

	r.matLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".material",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 4, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 5, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 6, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 7, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 8, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 9, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 10, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
		},
	})
	if err != nil {
		return cleanup(err)
	}

	r.matSampler, err = NewGPUSampler(cx, dev, SamplerLinearRepeat())
	if err != nil {
		return cleanup(err)
	}
	r.bgSampler, err = NewGPUSampler(cx, dev, SamplerLinearClamp())
	if err != nil {
		return cleanup(err)
	}

	r.whiteTex, err = UploadTexturePixels(cx, dev, PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{255, 255, 255, 255},
	}, TextureUpload{Label: label + ".white"})
	if err != nil {
		return cleanup(err)
	}
	r.blackTex, err = UploadTexturePixels(cx, dev, PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{0, 0, 0, 255},
	}, TextureUpload{Label: label + ".black"})
	if err != nil {
		return cleanup(err)
	}
	r.flatNormTex, err = UploadTexturePixels(cx, dev, PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{128, 128, 255, 255},
	}, TextureUpload{Label: label + ".flat_normal"})
	if err != nil {
		return cleanup(err)
	}

	r.defaultScene, err = r.CreateScene(cx, math3d.Mat4Identity(), DefaultPBRExtLighting(1, 1))
	if err != nil {
		return cleanup(err)
	}

	layouts := make([]*gpu.BindGroupLayout, PBRExtMaterialGroup+1)
	layouts[CameraGroup] = camLayout
	layouts[PBRExtSceneGroup] = r.sceneLayout
	layouts[PBRExtMaterialGroup] = r.matLayout

	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  opts.CullMode,
		},
		Targets: []gpu.ColorTargetState{{
			Format:    colorFmt,
			Blend:     opts.Blend,
			WriteMask: gpu.ColorWriteAll,
		}},
	}
	if opts.DepthFormat != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:       opts.DepthFormat,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		}
	}
	r.pipeline, err = dev.CreatePipeline(cx, desc)
	if err != nil {
		return cleanup(err)
	}

	return r, nil
}

// CreateScene allocates an independent per-object scene uniform binding for
// drawing multiple transforms in a single render pass via [ExtendedPBRRenderer.DrawWithScene].
func (r *ExtendedPBRRenderer) CreateScene(cx *core.Scope, model math3d.Mat4, light PBRExtLighting) (*ExtendedPBRScene, error) {
	if r == nil || r.dev == nil || !r.sceneLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: extended PBR renderer is not valid"}
	}
	var initData [PBRExtSceneUniformSize]byte
	light.EncodeSceneUniform(model, initData[:])
	buf, err := r.dev.CreateBuffer(cx, gpu.BufferDesc{
		Label: "render3d.pbr_ext.scene",
		Size:  PBRExtSceneUniformSize,
		Usage: gpu.UsageUniform | gpu.UsageCopyDst,
		Data:  initData[:],
	})
	if err != nil {
		return nil, err
	}
	group, err := r.dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:   "render3d.pbr_ext.scene",
		Layout:  r.sceneLayout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: buf, Size: PBRExtSceneUniformSize}},
	})
	if err != nil {
		_ = buf.Close()
		return nil, err
	}
	return &ExtendedPBRScene{dev: r.dev, buf: buf, group: group}, nil
}

// CreateMaterial validates mat, allocates its uniform buffer, and builds a GPU
// bind group substituting neutral 1x1 defaults for any nil textures.
func (r *ExtendedPBRRenderer) CreateMaterial(cx *core.Scope, mat PBRMaterialExt) (*ExtendedPBRMaterial, error) {
	if err := mat.Validate(); err != nil {
		return nil, err
	}
	if r == nil || r.dev == nil || !r.matLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: extended PBR renderer is not valid"}
	}
	var initData [PBRExtMaterialUniformSize]byte
	mat.EncodeUniform(initData[:])
	buf, err := r.dev.CreateBuffer(cx, gpu.BufferDesc{
		Label: "render3d.pbr_ext.material",
		Size:  PBRExtMaterialUniformSize,
		Usage: gpu.UsageUniform | gpu.UsageCopyDst,
		Data:  initData[:],
	})
	if err != nil {
		return nil, err
	}

	pickTex := func(t *GPUTexture, fallback *GPUTexture) *gpu.Texture {
		if t != nil {
			return t.Texture()
		}
		return fallback.Texture()
	}
	bgTex := mat.TransmissionBackground
	if bgTex == nil {
		bgTex = r.blackTex.Texture()
	}

	group, err := r.dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  "render3d.pbr_ext.material",
		Layout: r.matLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: buf, Size: PBRExtMaterialUniformSize},
			{Binding: 1, Sampler: r.matSampler.Sampler()},
			{Binding: 2, Sampler: r.bgSampler.Sampler()},
			{Binding: 3, Texture: pickTex(mat.BaseColorTexture, r.whiteTex)},
			{Binding: 4, Texture: pickTex(mat.MetallicRoughnessTexture, r.whiteTex)},
			{Binding: 5, Texture: pickTex(mat.EmissiveTexture, r.whiteTex)},
			{Binding: 6, Texture: pickTex(mat.ClearcoatTexture, r.whiteTex)},
			{Binding: 7, Texture: pickTex(mat.ClearcoatRoughnessTexture, r.whiteTex)},
			{Binding: 8, Texture: pickTex(mat.ClearcoatNormalTexture, r.flatNormTex)},
			{Binding: 9, Texture: pickTex(mat.TransmissionTexture, r.whiteTex)},
			{Binding: 10, Texture: bgTex},
		},
	})
	if err != nil {
		_ = buf.Close()
		return nil, err
	}
	return &ExtendedPBRMaterial{dev: r.dev, buf: buf, group: group}, nil
}

// SetScene uploads model and light into the renderer's default scene uniform
// buffer used by [ExtendedPBRRenderer.Draw].
func (r *ExtendedPBRRenderer) SetScene(model math3d.Mat4, light PBRExtLighting) {
	if r == nil || r.defaultScene == nil {
		return
	}
	r.defaultScene.Write(model, light)
}

// Draw binds the renderer pipeline, camera, default scene binding, and mat, then
// draws mesh into pass.
func (r *ExtendedPBRRenderer) Draw(pass *gpu.RenderPass, cam *CameraBuffer, mat *ExtendedPBRMaterial, mesh *GPUMesh) {
	r.DrawWithScene(pass, cam, r.defaultScene, mat, mesh)
}

// DrawWithScene binds the renderer pipeline, camera, an explicit per-object
// scene binding, and mat, then draws mesh into pass.
func (r *ExtendedPBRRenderer) DrawWithScene(pass *gpu.RenderPass, cam *CameraBuffer, scene *ExtendedPBRScene, mat *ExtendedPBRMaterial, mesh *GPUMesh) {
	if r == nil || pass == nil || cam == nil || !r.pipeline.Valid() || !scene.Valid() || !mat.Valid() || !mesh.Valid() {
		return
	}
	pass.SetPipeline(r.pipeline)
	pass.SetBindGroup(CameraGroup, cam.BindGroup())
	pass.SetBindGroup(PBRExtSceneGroup, scene.BindGroup())
	pass.SetBindGroup(PBRExtMaterialGroup, mat.BindGroup())
	mesh.Draw(pass)
}

// Pipeline returns the compiled extended PBR GPU pipeline.
func (r *ExtendedPBRRenderer) Pipeline() *gpu.Pipeline {
	if r == nil {
		return nil
	}
	return r.pipeline
}

// SceneLayout returns the bind group layout at [PBRExtSceneGroup].
func (r *ExtendedPBRRenderer) SceneLayout() *gpu.BindGroupLayout {
	if r == nil {
		return nil
	}
	return r.sceneLayout
}

// MaterialLayout returns the bind group layout at [PBRExtMaterialGroup].
func (r *ExtendedPBRRenderer) MaterialLayout() *gpu.BindGroupLayout {
	if r == nil {
		return nil
	}
	return r.matLayout
}

// Close releases all GPU resources owned by r.
func (r *ExtendedPBRRenderer) Close() error {
	if r == nil {
		return nil
	}
	var first error
	for _, c := range []interface{ Close() error }{
		r.pipeline,
		r.defaultScene,
		r.flatNormTex,
		r.blackTex,
		r.whiteTex,
		r.bgSampler,
		r.matSampler,
		r.matLayout,
		r.sceneLayout,
	} {
		if c != nil {
			if err := c.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}
