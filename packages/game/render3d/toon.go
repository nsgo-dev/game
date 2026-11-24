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

// Bind group slots, uniform sizes, and defaults for cel/toon shading (#262).
const (
	// ToonMaterialGroup is the bind group index [ToonWGSL] and [ToonOutlineWGSL] reserve for [GPUToonMaterial] (uniform + base and ramp textures/samplers), alongside [CameraGroup] at group 0. It is the same slot as [MaterialGroup], where the other material pipelines bind their material.
	ToonMaterialGroup = 1
	// ToonSceneGroup is the bind group index [ToonWGSL] and [ToonOutlineWGSL] reserve for the per-draw transform and the key light ([ToonSceneUniform]). Toon shading bands the light of one key light, so it takes that light from a [ToonLight] in this per-draw block rather than binding a whole [LightRig] at [LightGroup].
	ToonSceneGroup = 2

	// ToonUniformSize is the little-endian byte size of the material uniform block [ToonMaterial.AppendUniformData] writes and [ToonWGSL] declares (7 vec4<f32> = 112 bytes).
	ToonUniformSize = 7 * 16
	// ToonSceneUniformSize is the little-endian byte size of the per-draw scene uniform block [ToonSceneUniform.AppendUniformData] writes and [ToonWGSL] declares (2 mat4x4<f32> + 3 vec4<f32> = 176 bytes).
	ToonSceneUniformSize = 2*64 + 3*16

	// DefaultToonBands is the discrete diffuse band count used when [ToonMaterial.Bands] is zero.
	DefaultToonBands = 3
	// MinToonBands is the smallest explicit diffuse band count [ToonMaterial.Validate] accepts.
	MinToonBands = 2
	// MaxToonBands is the largest diffuse band count [ToonMaterial.Validate] accepts.
	MaxToonBands = 8

	// DefaultToonSpecularPower is the Blinn-Phong exponent used when [ToonMaterial.SpecularPower] is zero.
	DefaultToonSpecularPower = 32

	// MaxToonDrawsPerFrame is the most draws [ToonRenderer.Draw] accepts between two calls of [ToonRenderer.Begin] (or two frames), which bounds the scene uniform buffer when a caller that submits with [gpu.Device.Submit] directly forgets to call Begin.
	MaxToonDrawsPerFrame = 1 << 16
)

// ErrInvalidToonMaterial is wrapped by every error [ToonMaterial.Validate], [NewGPUToonMaterial], and [GPUToonMaterial.Update] return for malformed material parameters. Light errors wrap [ErrInvalidLight].
var ErrInvalidToonMaterial = errors.New("render3d: invalid toon material")

// ToonWGSL is the portable cel/toon shading WGSL shader (#262). It uses [CameraWGSL] at @group(0), [GPUToonMaterial] at @group(1), and [ToonSceneUniform] at @group(2), with [MeshVertexLayout] on vertex buffer 0. It uses only standard vertex/fragment stages and arithmetic/pow builtins so it runs identically on WebGPU, Metal, Vulkan, Direct3D 11, OpenGL 3.3, OpenGL ES 3.0, WebGL 2, and NSGo's software renderer.
const ToonWGSL = CameraWGSL + `
struct ToonMaterialUniform {
	base_color: vec4<f32>,
	shadow_bands: vec4<f32>,
	spec_color_pow: vec4<f32>,
	thresholds: vec4<f32>,
	rim_outline: vec4<f32>,
	outline_color: vec4<f32>,
	flags: vec4<f32>,
}

struct ToonSceneUniform {
	model: mat4x4<f32>,
	normal_mat: mat4x4<f32>,
	to_light: vec4<f32>,
	light_color: vec4<f32>,
	ambient_color: vec4<f32>,
}

@group(1) @binding(0) var<uniform> material: ToonMaterialUniform;
@group(1) @binding(1) var base_tex: texture_2d<f32>;
@group(1) @binding(2) var base_samp: sampler;
@group(1) @binding(3) var ramp_tex: texture_2d<f32>;
@group(1) @binding(4) var ramp_samp: sampler;

@group(2) @binding(0) var<uniform> scene: ToonSceneUniform;

struct ToonVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct ToonVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

fn toon_smoothstep(edge0: f32, edge1: f32, x: f32) -> f32 {
	let denom = max(edge1 - edge0, 0.00001);
	let t = clamp((x - edge0) / denom, 0.0, 1.0);
	return t * t * (3.0 - 2.0 * t);
}

fn toon_quantize_band(ndl: f32, bands: f32, smoothness: f32) -> f32 {
	let c = clamp(ndl, 0.0, 1.0);
	let n = max(bands, 2.0);
	let x = c * n;
	if (smoothness <= 0.00001) {
		let idx = min(floor(x), n - 1.0);
		return idx / (n - 1.0);
	}
	let w = min(smoothness, 0.5);
	let k = clamp(floor(x + 0.5), 1.0, n - 1.0);
	let t = toon_smoothstep(k - w, k + w, x);
	return (k - 1.0 + t) / (n - 1.0);
}

@vertex
fn vs_main(in: ToonVertexIn) -> ToonVertexOut {
	var out: ToonVertexOut;
	let world = scene.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world;
	out.world_pos = world.xyz;
	out.world_normal = (scene.normal_mat * vec4<f32>(in.normal, 0.0)).xyz;
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: ToonVertexOut) -> @location(0) vec4<f32> {
	let texel = textureSample(base_tex, base_samp, in.uv);
	let albedo = material.base_color * texel;

	let n_len = length(in.world_normal);
	var n = vec3<f32>(0.0, 1.0, 0.0);
	if (n_len > 0.00001) {
		n = in.world_normal / n_len;
	}

	let l_len = length(scene.to_light.xyz);
	var l = vec3<f32>(0.0, 1.0, 0.0);
	if (l_len > 0.00001) {
		l = scene.to_light.xyz / l_len;
	}

	let to_eye = camera.eye.xyz - in.world_pos * camera.eye.w;
	let v_len = length(to_eye);
	var v = vec3<f32>(0.0, 0.0, 1.0);
	if (v_len > 0.00001) {
		v = to_eye / v_len;
	}

	let ndl = clamp(dot(n, l), 0.0, 1.0);
	let ramp_sample = textureSample(ramp_tex, ramp_samp, vec2<f32>(ndl, 0.5));
	var diffuse_tint: vec3<f32>;
	if (material.flags.x > 0.5) {
		diffuse_tint = ramp_sample.rgb;
	} else {
		let band = toon_quantize_band(ndl, material.shadow_bands.w, material.thresholds.x);
		diffuse_tint = mix(material.shadow_bands.xyz, vec3<f32>(1.0, 1.0, 1.0), band);
	}

	let lit_diffuse = albedo.rgb * (diffuse_tint * scene.light_color.xyz + scene.ambient_color.xyz);

	var spec_factor: f32 = 0.0;
	if (ndl > 0.0) {
		let half_sum = l + v;
		let half_len = length(half_sum);
		if (half_len > 0.00001) {
			let h = half_sum / half_len;
			let ndh = max(dot(n, h), 0.0);
			if (ndh > 0.0) {
				let spec_raw = pow(ndh, material.spec_color_pow.w);
				let spec_thresh = material.thresholds.y;
				let spec_smooth = material.thresholds.z;
				if (spec_smooth <= 0.00001) {
					if (spec_raw >= spec_thresh) {
						spec_factor = 1.0;
					}
				} else {
					spec_factor = toon_smoothstep(spec_thresh - spec_smooth, spec_thresh + spec_smooth, spec_raw);
				}
			}
		}
	}
	let lit_spec = material.spec_color_pow.xyz * scene.light_color.xyz * spec_factor;

	let ndv = clamp(dot(n, v), 0.0, 1.0);
	let fresnel = 1.0 - ndv;
	let rim_thresh = material.thresholds.w;
	let rim_smooth = material.flags.z;
	var rim_factor: f32 = 0.0;
	if (rim_smooth <= 0.00001) {
		if (fresnel >= rim_thresh) {
			rim_factor = 1.0;
		}
	} else {
		rim_factor = toon_smoothstep(rim_thresh - rim_smooth, rim_thresh + rim_smooth, fresnel);
	}
	let lit_rim = material.rim_outline.xyz * rim_factor;

	let rgb = clamp(lit_diffuse + lit_spec + lit_rim, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
	return vec4<f32>(rgb, albedo.a);
}
`

// ToonOutlineWGSL is the inverted-hull outline WGSL shader paired with [ToonWGSL] (#262). Built into a pipeline with [gpu.CullFront] via [NewToonOutlinePipeline], it extrudes each vertex outward along its world-space normal (or projected clip-space normal when [ToonMaterial.OutlineClipSpace] is set) by [ToonMaterial.OutlineWidth] and shades back faces with [ToonMaterial.OutlineColor]. It shares the exact bind group layouts of [ToonWGSL] so the same camera, material, and scene bind groups draw both passes.
const ToonOutlineWGSL = CameraWGSL + `
struct ToonMaterialUniform {
	base_color: vec4<f32>,
	shadow_bands: vec4<f32>,
	spec_color_pow: vec4<f32>,
	thresholds: vec4<f32>,
	rim_outline: vec4<f32>,
	outline_color: vec4<f32>,
	flags: vec4<f32>,
}

struct ToonSceneUniform {
	model: mat4x4<f32>,
	normal_mat: mat4x4<f32>,
	to_light: vec4<f32>,
	light_color: vec4<f32>,
	ambient_color: vec4<f32>,
}

@group(1) @binding(0) var<uniform> material: ToonMaterialUniform;
@group(2) @binding(0) var<uniform> scene: ToonSceneUniform;

struct ToonOutlineIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct ToonOutlineOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) color: vec4<f32>,
}

@vertex
fn vs_outline(in: ToonOutlineIn) -> ToonOutlineOut {
	var out: ToonOutlineOut;
	let world_pos = (scene.model * vec4<f32>(in.pos, 1.0)).xyz;
	let raw_n = (scene.normal_mat * vec4<f32>(in.normal, 0.0)).xyz;
	let n_len = length(raw_n);
	var world_n = vec3<f32>(0.0, 0.0, 0.0);
	if (n_len > 0.00001) {
		world_n = raw_n / n_len;
	}
	let width = material.rim_outline.w;
	if (material.flags.y > 0.5) {
		let base_clip = camera.view_proj * vec4<f32>(world_pos, 1.0);
		let clip_n = (camera.view_proj * vec4<f32>(world_n, 0.0)).xy;
		// The projection's width-to-height ratio. NDC x spans the viewport's width and y its height, so the offset is made isotropic in pixels by measuring it in units of NDC y.
		var aspect = 1.0;
		if (abs(camera.proj[0][0]) > 0.00001) {
			aspect = abs(camera.proj[1][1] / camera.proj[0][0]);
		}
		let screen_n = vec2<f32>(clip_n.x * aspect, clip_n.y);
		let sn_len = length(screen_n);
		var offset = vec2<f32>(0.0, 0.0);
		if (sn_len > 0.00001) {
			let dir = screen_n / sn_len;
			offset = vec2<f32>(dir.x / aspect, dir.y) * (width * base_clip.w);
		}
		out.clip = vec4<f32>(base_clip.xy + offset, base_clip.z, base_clip.w);
	} else {
		let extruded = world_pos + world_n * width;
		out.clip = camera.view_proj * vec4<f32>(extruded, 1.0);
	}
	out.color = material.outline_color;
	return out;
}

@fragment
fn fs_outline(in: ToonOutlineOut) -> @location(0) vec4<f32> {
	return in.color;
}
`

// ToonMaterial defines cel/toon shading parameters: banded diffuse lighting (or a 1D ramp texture), hard stepped specular highlights, Fresnel rim lighting, and inverted-hull outline styling (#262).
type ToonMaterial struct {
	// BaseColorFactor is the linear RGBA base surface color multiplier, as in [Material]. Its alpha is written to the output alpha, which only a pipeline built with [NewToonPipeline] and a [ToonPipelineOptions.Blend] blends with: [ToonRenderer] draws opaque.
	BaseColorFactor [4]float32
	// BaseColorTexture is an optional 2D albedo texture multiplied with BaseColorFactor. When nil, [MaterialDefaults.WhiteSRGB] is bound.
	BaseColorTexture *GPUTexture
	// BaseColorSampler is the sampler for BaseColorTexture. When nil, [MaterialDefaults.Sampler] (linear repeat) is bound.
	BaseColorSampler *GPUSampler

	// ShadowColor is the linear RGB tint applied to the darkest diffuse band.
	ShadowColor [3]float32
	// Bands is the number of discrete diffuse cel-shading bands in [MinToonBands, MaxToonBands] (2..8). Zero defaults to [DefaultToonBands] (3). Ignored when RampTexture is set.
	Bands int
	// BandSmoothness controls transition softness between diffuse bands in [0, 0.5]. Zero produces crisp hard cel steps; small positive values anti-alias band boundaries.
	BandSmoothness float32

	// RampTexture is an optional 1D horizontal cel-ramp texture (height >= 1) sampled at u = clamp(N·L, 0, 1). When non-nil, its RGB value replaces analytical band quantization.
	RampTexture *GPUTexture
	// RampSampler is the sampler for RampTexture. When nil and RampTexture is set, the material creates a linear-clamp sampler so the ramp's ends do not wrap into each other.
	RampSampler *GPUSampler

	// SpecularColor is the linear RGB color of the stepped cel specular highlight. Zero disables specular highlights.
	SpecularColor [3]float32
	// SpecularPower is the Blinn-Phong shininess exponent. Zero defaults to [DefaultToonSpecularPower] (32).
	SpecularPower float32
	// SpecularThreshold is the step cutoff in [0, 1] applied to pow(N·H, SpecularPower).
	SpecularThreshold float32
	// SpecularSmoothness controls specular edge softness in [0, 0.5]. Zero gives a hard cel highlight edge.
	SpecularSmoothness float32

	// RimColor is the linear RGB color of the Fresnel rim highlight. Zero disables rim highlights.
	RimColor [3]float32
	// RimThreshold is the Fresnel cutoff in [0, 1] applied to (1 - N·V); higher values produce a thinner silhouette rim.
	RimThreshold float32
	// RimSmoothness controls rim edge softness in [0, 0.5]. Zero gives a crisp rim step.
	RimSmoothness float32

	// OutlineColor is the linear RGBA color of the inverted-hull outline pass. An alpha of 0 turns the outline off; other alpha values only blend in a pipeline built with [NewToonOutlinePipeline] and a [ToonPipelineOptions.Blend], since [ToonRenderer] draws opaque.
	OutlineColor [4]float32
	// OutlineWidth is the normal-extrusion distance of the inverted-hull outline pass: in world units, or with OutlineClipSpace in NDC y units, so the outline is OutlineWidth × viewport height / 2 pixels thick. Zero disables the outline pass.
	OutlineWidth float32
	// OutlineClipSpace extrudes outline vertices along their projected normal on screen instead of in world space, giving the same thickness in pixels at every distance and in every direction. It reads the aspect ratio from the camera's projection, which must match the viewport's.
	OutlineClipSpace bool
}

// DefaultToonMaterial returns a ready-to-use cel-shaded material with 3 crisp bands, a cool shadow tint, subtle specular and rim highlights, and a dark silhouette outline.
func DefaultToonMaterial() ToonMaterial {
	return ToonMaterial{
		BaseColorFactor:    [4]float32{1, 1, 1, 1},
		ShadowColor:        [3]float32{0.25, 0.28, 0.38},
		Bands:              DefaultToonBands,
		BandSmoothness:     0,
		SpecularColor:      [3]float32{0.85, 0.85, 0.85},
		SpecularPower:      DefaultToonSpecularPower,
		SpecularThreshold:  0.5,
		SpecularSmoothness: 0.02,
		RimColor:           [3]float32{0.6, 0.75, 0.9},
		RimThreshold:       0.65,
		RimSmoothness:      0.04,
		OutlineColor:       [4]float32{0.05, 0.05, 0.08, 1},
		OutlineWidth:       0.035,
	}
}

// EffectiveBands returns m.Bands, or [DefaultToonBands] when m.Bands is zero.
func (m ToonMaterial) EffectiveBands() int {
	if m.Bands == 0 {
		return DefaultToonBands
	}
	return m.Bands
}

// EffectiveSpecularPower returns m.SpecularPower, or [DefaultToonSpecularPower] when m.SpecularPower is zero.
func (m ToonMaterial) EffectiveSpecularPower() float32 {
	if m.SpecularPower == 0 {
		return DefaultToonSpecularPower
	}
	return m.SpecularPower
}

// HasOutline reports whether m draws an inverted-hull outline (positive width and non-zero outline alpha).
func (m ToonMaterial) HasOutline() bool {
	return m.OutlineWidth > 0 && m.OutlineColor[3] > 0
}

// Validate checks every scalar, color, band count, threshold, and optional GPU handle on m. Every error wraps [ErrInvalidToonMaterial].
func (m ToonMaterial) Validate() error {
	for i, c := range m.BaseColorFactor[:3] {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: BaseColorFactor[%d] = %v must be finite and >= 0", ErrInvalidToonMaterial, i, c)
		}
	}
	if !finite(m.BaseColorFactor[3]) || m.BaseColorFactor[3] < 0 || m.BaseColorFactor[3] > 1 {
		return fmt.Errorf("%w: BaseColorFactor alpha %v must be in [0, 1]", ErrInvalidToonMaterial, m.BaseColorFactor[3])
	}
	for i, c := range m.ShadowColor {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: ShadowColor[%d] = %v must be finite and >= 0", ErrInvalidToonMaterial, i, c)
		}
	}
	if m.Bands != 0 && (m.Bands < MinToonBands || m.Bands > MaxToonBands) {
		return fmt.Errorf("%w: Bands %d must be 0 (default %d) or in [%d, %d]", ErrInvalidToonMaterial, m.Bands, DefaultToonBands, MinToonBands, MaxToonBands)
	}
	if !finite(m.BandSmoothness) || m.BandSmoothness < 0 || m.BandSmoothness > 0.5 {
		return fmt.Errorf("%w: BandSmoothness %v must be in [0, 0.5]", ErrInvalidToonMaterial, m.BandSmoothness)
	}
	for i, c := range m.SpecularColor {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: SpecularColor[%d] = %v must be finite and >= 0", ErrInvalidToonMaterial, i, c)
		}
	}
	if !finite(m.SpecularPower) || m.SpecularPower < 0 {
		return fmt.Errorf("%w: SpecularPower %v must be finite and >= 0", ErrInvalidToonMaterial, m.SpecularPower)
	}
	if !finite(m.SpecularThreshold) || m.SpecularThreshold < 0 || m.SpecularThreshold > 1 {
		return fmt.Errorf("%w: SpecularThreshold %v must be in [0, 1]", ErrInvalidToonMaterial, m.SpecularThreshold)
	}
	if !finite(m.SpecularSmoothness) || m.SpecularSmoothness < 0 || m.SpecularSmoothness > 0.5 {
		return fmt.Errorf("%w: SpecularSmoothness %v must be in [0, 0.5]", ErrInvalidToonMaterial, m.SpecularSmoothness)
	}
	for i, c := range m.RimColor {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: RimColor[%d] = %v must be finite and >= 0", ErrInvalidToonMaterial, i, c)
		}
	}
	if !finite(m.RimThreshold) || m.RimThreshold < 0 || m.RimThreshold > 1 {
		return fmt.Errorf("%w: RimThreshold %v must be in [0, 1]", ErrInvalidToonMaterial, m.RimThreshold)
	}
	if !finite(m.RimSmoothness) || m.RimSmoothness < 0 || m.RimSmoothness > 0.5 {
		return fmt.Errorf("%w: RimSmoothness %v must be in [0, 0.5]", ErrInvalidToonMaterial, m.RimSmoothness)
	}
	for i, c := range m.OutlineColor[:3] {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: OutlineColor[%d] = %v must be finite and >= 0", ErrInvalidToonMaterial, i, c)
		}
	}
	if !finite(m.OutlineColor[3]) || m.OutlineColor[3] < 0 || m.OutlineColor[3] > 1 {
		return fmt.Errorf("%w: OutlineColor alpha %v must be in [0, 1]", ErrInvalidToonMaterial, m.OutlineColor[3])
	}
	if !finite(m.OutlineWidth) || m.OutlineWidth < 0 {
		return fmt.Errorf("%w: OutlineWidth %v must be finite and >= 0", ErrInvalidToonMaterial, m.OutlineWidth)
	}
	if m.BaseColorTexture != nil && !m.BaseColorTexture.Valid() {
		return fmt.Errorf("%w: BaseColorTexture is not valid", ErrInvalidToonMaterial)
	}
	if m.BaseColorSampler != nil && !m.BaseColorSampler.Valid() {
		return fmt.Errorf("%w: BaseColorSampler is not valid", ErrInvalidToonMaterial)
	}
	if m.RampTexture != nil && !m.RampTexture.Valid() {
		return fmt.Errorf("%w: RampTexture is not valid", ErrInvalidToonMaterial)
	}
	if m.RampSampler != nil && !m.RampSampler.Valid() {
		return fmt.Errorf("%w: RampSampler is not valid", ErrInvalidToonMaterial)
	}
	return nil
}

// AppendUniformData appends m's [ToonUniformSize]-byte little-endian uniform block matching [ToonWGSL] and [ToonOutlineWGSL] to dst and returns the extended slice. It allocates only when dst lacks capacity for [ToonUniformSize] more bytes.
func (m ToonMaterial) AppendUniformData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, ToonUniformSize)
	out := dst[start:]

	useRamp := float32(0)
	if m.RampTexture != nil {
		useRamp = 1
	}
	clipSpace := float32(0)
	if m.OutlineClipSpace {
		clipSpace = 1
	}

	vals := [28]float32{
		// base_color
		m.BaseColorFactor[0], m.BaseColorFactor[1], m.BaseColorFactor[2], m.BaseColorFactor[3],
		// shadow_bands
		m.ShadowColor[0], m.ShadowColor[1], m.ShadowColor[2], float32(m.EffectiveBands()),
		// spec_color_pow
		m.SpecularColor[0], m.SpecularColor[1], m.SpecularColor[2], m.EffectiveSpecularPower(),
		// thresholds
		m.BandSmoothness, m.SpecularThreshold, m.SpecularSmoothness, m.RimThreshold,
		// rim_outline
		m.RimColor[0], m.RimColor[1], m.RimColor[2], m.OutlineWidth,
		// outline_color
		m.OutlineColor[0], m.OutlineColor[1], m.OutlineColor[2], m.OutlineColor[3],
		// flags
		useRamp, clipSpace, m.RimSmoothness, 0,
	}
	for i, v := range vals {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}
	return dst
}

// ToonLight is the light a toon-shaded draw receives: one directional key light, whose N·L is quantized into bands, plus an ambient fill added to every band. Both use the same types and conventions as a [LightRig]: Key.Direction is the direction the light's rays travel, and an Intensity of 0 turns a light off.
type ToonLight struct {
	Key     DirectionalLight
	Ambient AmbientLight
}

// DefaultToonLight returns a white key light shining down from the upper front right with a dim cool ambient fill.
func DefaultToonLight() ToonLight {
	return ToonLight{
		Key: DirectionalLight{
			Direction: math3d.Vec3{X: -0.45, Y: -0.8, Z: -0.4}.Norm(),
			Color:     math3d.Vec3{X: 1, Y: 1, Z: 1},
			Intensity: 1,
		},
		Ambient: AmbientLight{Color: math3d.Vec3{X: 0.05, Y: 0.06, Z: 0.08}, Intensity: 1},
	}
}

// ToonLightFromRig returns the toon light for a scene lit by rig: its first directional light as the key light and its ambient fill. Point lights and further directional lights are not used. It returns an error wrapping [ErrInvalidLight] when rig is invalid or has no directional light.
func ToonLightFromRig(rig LightRig) (ToonLight, error) {
	if err := rig.Validate(); err != nil {
		return ToonLight{}, err
	}
	if len(rig.Directional) == 0 {
		return ToonLight{}, fmt.Errorf("%w: toon shading needs a directional key light", ErrInvalidLight)
	}
	return ToonLight{Key: rig.Directional[0], Ambient: rig.Ambient}, nil
}

// Validate reports whether the key light has a finite non-zero direction and both lights have finite non-negative colors and intensities. Every error wraps [ErrInvalidLight].
func (l ToonLight) Validate() error {
	if err := l.Key.Validate(); err != nil {
		return fmt.Errorf("toon key light: %w", err)
	}
	if err := l.Ambient.Validate(); err != nil {
		return fmt.Errorf("toon ambient light: %w", err)
	}
	return nil
}

// resolved returns the unit vector toward the key light and the key and ambient radiance. A degenerate key direction yields zero key radiance, as [LightRig.AppendUniformData] does.
func (l ToonLight) resolved() (toLight, color, ambient math3d.Vec3) {
	ambient = l.Ambient.Color.Scale(l.Ambient.Intensity)
	d, ok := toonNormalizeVec3(l.Key.Direction)
	if !ok {
		return math3d.Vec3{}, math3d.Vec3{}, ambient
	}
	return d.Scale(-1), l.Key.Color.Scale(l.Key.Intensity), ambient
}

// ToonSceneUniform holds the per-draw model matrix, inverse-transpose normal matrix, and key and ambient light packed for @group(2) in [ToonWGSL] and [ToonOutlineWGSL].
type ToonSceneUniform struct {
	Model        math3d.Mat4
	NormalMatrix math3d.Mat4
	// ToLight is the unit vector from a surface toward the key light (the opposite of [DirectionalLight.Direction]).
	ToLight math3d.Vec3
	// LightColor is the key light's linear radiance (Color × Intensity).
	LightColor math3d.Vec3
	// AmbientColor is the ambient fill's linear radiance (Color × Intensity).
	AmbientColor math3d.Vec3
}

// NewToonSceneUniform builds a [ToonSceneUniform] from a world transform (zero value means identity) and a [ToonLight].
func NewToonSceneUniform(model math3d.Mat4, light ToonLight) ToonSceneUniform {
	if model == (math3d.Mat4{}) {
		model = math3d.Mat4Identity()
	}
	toLight, col, amb := light.resolved()
	return ToonSceneUniform{
		Model:        model,
		NormalMatrix: toonNormalMatrix(model),
		ToLight:      toLight,
		LightColor:   col,
		AmbientColor: amb,
	}
}

// AppendUniformData appends u's [ToonSceneUniformSize]-byte little-endian uniform block matching [ToonWGSL] and [ToonOutlineWGSL] to dst. It allocates only when dst lacks capacity for [ToonSceneUniformSize] more bytes.
func (u ToonSceneUniform) AppendUniformData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, ToonSceneUniformSize)
	out := dst[start:]
	off := 0
	for _, f := range u.Model {
		binary.LittleEndian.PutUint32(out[off:], math.Float32bits(f))
		off += 4
	}
	for _, f := range u.NormalMatrix {
		binary.LittleEndian.PutUint32(out[off:], math.Float32bits(f))
		off += 4
	}
	tail := [12]float32{
		u.ToLight.X, u.ToLight.Y, u.ToLight.Z, 0,
		u.LightColor.X, u.LightColor.Y, u.LightColor.Z, 0,
		u.AmbientColor.X, u.AmbientColor.Y, u.AmbientColor.Z, 0,
	}
	for _, f := range tail {
		binary.LittleEndian.PutUint32(out[off:], math.Float32bits(f))
		off += 4
	}
	return dst
}

// ToonQuantizeBand evaluates the discrete cel-shading band factor in [0, 1] for ndl = N·L, matching [ToonWGSL]'s toon_quantize_band function. Bands < [MinToonBands] default to [DefaultToonBands]. Smoothness 0 produces hard steps; values in (0, 0.5] anti-alias each band boundary smoothly.
func ToonQuantizeBand(ndl float32, bands int, smoothness float32) float32 {
	if bands < MinToonBands {
		bands = DefaultToonBands
	}
	c := clamp01(ndl)
	n := float32(bands)
	x := c * n
	if smoothness <= 1e-5 {
		idx := float32(math.Floor(float64(x)))
		if idx > n-1 {
			idx = n - 1
		}
		return idx / (n - 1)
	}
	w := smoothness
	if w > 0.5 {
		w = 0.5
	}
	k := float32(math.Floor(float64(x + 0.5)))
	if k < 1 {
		k = 1
	}
	if k > n-1 {
		k = n - 1
	}
	t := toonSmoothstep(k-w, k+w, x)
	return (k - 1 + t) / (n - 1)
}

// ToonEvalInput supplies the surface normal, the directions toward the key light and the viewer, the light radiance, and optional texture samples for [EvaluateToon].
type ToonEvalInput struct {
	Normal math3d.Vec3
	// ToLight points from the surface toward the key light, like [ToonSceneUniform.ToLight].
	ToLight      math3d.Vec3
	ViewDir      math3d.Vec3
	LightColor   math3d.Vec3
	AmbientColor math3d.Vec3
	// BaseSample is an optional linear RGBA sample from BaseColorTexture. Nil means opaque white {1, 1, 1, 1}.
	BaseSample *[4]float32
	// RampSample is an optional linear RGB sample from RampTexture. When non-nil, it replaces analytical band quantization.
	RampSample *[3]float32
}

// EvaluateToon is the pure-Go CPU reference implementation of [ToonWGSL] for deterministic unit testing and golden-render parity checks. It returns linear RGBA with RGB clamped to [0, 1].
func EvaluateToon(mat ToonMaterial, in ToonEvalInput) [4]float32 {
	tex := [4]float32{1, 1, 1, 1}
	if in.BaseSample != nil {
		tex = *in.BaseSample
	}
	albedo := [4]float32{
		mat.BaseColorFactor[0] * tex[0],
		mat.BaseColorFactor[1] * tex[1],
		mat.BaseColorFactor[2] * tex[2],
		mat.BaseColorFactor[3] * tex[3],
	}

	n, ok := toonNormalizeVec3(in.Normal)
	if !ok {
		n = math3d.Vec3{Y: 1}
	}
	l, ok := toonNormalizeVec3(in.ToLight)
	if !ok {
		l = math3d.Vec3{Y: 1}
	}
	v, ok := toonNormalizeVec3(in.ViewDir)
	if !ok {
		v = math3d.Vec3{Z: 1}
	}

	ndl := clamp01(n.Dot(l))
	var diffuseTint [3]float32
	if in.RampSample != nil {
		diffuseTint = *in.RampSample
	} else {
		band := ToonQuantizeBand(ndl, mat.EffectiveBands(), mat.BandSmoothness)
		diffuseTint = [3]float32{
			mat.ShadowColor[0] + (1-mat.ShadowColor[0])*band,
			mat.ShadowColor[1] + (1-mat.ShadowColor[1])*band,
			mat.ShadowColor[2] + (1-mat.ShadowColor[2])*band,
		}
	}

	var specFactor float32
	if ndl > 0 {
		if h, ok := toonNormalizeVec3(l.Add(v)); ok {
			ndh := n.Dot(h)
			if ndh > 0 {
				specRaw := float32(math.Pow(float64(ndh), float64(mat.EffectiveSpecularPower())))
				if mat.SpecularSmoothness <= 1e-5 {
					if specRaw >= mat.SpecularThreshold {
						specFactor = 1
					}
				} else {
					specFactor = toonSmoothstep(mat.SpecularThreshold-mat.SpecularSmoothness, mat.SpecularThreshold+mat.SpecularSmoothness, specRaw)
				}
			}
		}
	}

	ndv := clamp01(n.Dot(v))
	fresnel := 1 - ndv
	var rimFactor float32
	if mat.RimSmoothness <= 1e-5 {
		if fresnel >= mat.RimThreshold {
			rimFactor = 1
		}
	} else {
		rimFactor = toonSmoothstep(mat.RimThreshold-mat.RimSmoothness, mat.RimThreshold+mat.RimSmoothness, fresnel)
	}

	lightColor := [3]float32{in.LightColor.X, in.LightColor.Y, in.LightColor.Z}
	ambient := [3]float32{in.AmbientColor.X, in.AmbientColor.Y, in.AmbientColor.Z}
	var out [4]float32
	for c := range 3 {
		diff := albedo[c] * (diffuseTint[c]*lightColor[c] + ambient[c])
		spec := mat.SpecularColor[c] * lightColor[c] * specFactor
		rim := mat.RimColor[c] * rimFactor
		out[c] = clamp01(diff + spec + rim)
	}
	out[3] = albedo[3]
	return out
}

// SmoothOutlineNormals returns a copy of m whose normals are averaged across vertices sharing the same position (within 1e-5), producing watertight inverted-hull outlines for hard-edged meshes such as boxes and low-poly props without splitting corners apart. Positions, UVs, and Indices are copied without mutating m.
func SmoothOutlineNormals(m Mesh) Mesh {
	out := Mesh{
		Positions: append([]math3d.Vec3(nil), m.Positions...),
		UVs:       append([]math3d.Vec2(nil), m.UVs...),
		Indices:   append([]uint32(nil), m.Indices...),
	}
	n := len(m.Positions)
	if n == 0 {
		return out
	}

	type posKey struct{ x, y, z int64 }
	quantize := func(p math3d.Vec3) posKey {
		const scale = 1e5
		return posKey{
			x: int64(math.Round(float64(p.X) * scale)),
			y: int64(math.Round(float64(p.Y) * scale)),
			z: int64(math.Round(float64(p.Z) * scale)),
		}
	}

	sums := make(map[posKey]math3d.Vec3, n)
	hasNormals := len(m.Normals) == n
	if hasNormals {
		for i, p := range m.Positions {
			k := quantize(p)
			sums[k] = sums[k].Add(m.Normals[i])
		}
	} else {
		// Derive angle-weighted face normals from triangle winding when the source mesh lacks vertex normals.
		angleAt := func(e1, e2 math3d.Vec3) float32 {
			u1, ok1 := toonNormalizeVec3(e1)
			u2, ok2 := toonNormalizeVec3(e2)
			if !ok1 || !ok2 {
				return 0
			}
			d := float64(u1.Dot(u2))
			if d < -1 {
				d = -1
			} else if d > 1 {
				d = 1
			}
			return float32(math.Acos(d))
		}
		addTri := func(i0, i1, i2 int) {
			if i0 >= n || i1 >= n || i2 >= n {
				return
			}
			a, b, c := m.Positions[i0], m.Positions[i1], m.Positions[i2]
			ab, ac := b.Sub(a), c.Sub(a)
			fnUnit, ok := toonNormalizeVec3(ab.Cross(ac))
			if !ok {
				return
			}
			ka, kb, kc := quantize(a), quantize(b), quantize(c)
			sums[ka] = sums[ka].Add(fnUnit.Scale(angleAt(ab, ac)))
			sums[kb] = sums[kb].Add(fnUnit.Scale(angleAt(c.Sub(b), a.Sub(b))))
			sums[kc] = sums[kc].Add(fnUnit.Scale(angleAt(a.Sub(c), b.Sub(c))))
		}
		if len(m.Indices) >= 3 {
			for i := 0; i+2 < len(m.Indices); i += 3 {
				addTri(int(m.Indices[i]), int(m.Indices[i+1]), int(m.Indices[i+2]))
			}
		} else {
			for i := 0; i+2 < n; i += 3 {
				addTri(i, i+1, i+2)
			}
		}
	}

	out.Normals = make([]math3d.Vec3, n)
	for i, p := range m.Positions {
		k := quantize(p)
		if avg, ok := toonNormalizeVec3(sums[k]); ok {
			out.Normals[i] = avg
		} else if hasNormals {
			out.Normals[i] = m.Normals[i]
		} else {
			out.Normals[i] = math3d.Vec3{Y: 1}
		}
	}
	return out
}

// ToonMaterialBindingLayout creates the @group(1) bind group layout for [GPUToonMaterial]:
// binding 0 uniform buffer (vertex + fragment), binding 1 base color texture, binding 2 base sampler,
// binding 3 cel ramp texture, binding 4 ramp sampler (bindings 1..4 fragment-visible).
func ToonMaterialBindingLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.toon.material",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 4, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
}

// ToonSceneBindingLayout creates the @group(2) bind group layout for a single [ToonSceneUniform] buffer (non-dynamic offset). For multi-object drawing within one render pass, see [ToonSceneDynamicBindingLayout] and [ToonRenderer].
func ToonSceneBindingLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	return toonSceneBindingLayout(cx, dev, false)
}

// ToonSceneDynamicBindingLayout creates the @group(2) bind group layout with a dynamic uniform offset, allowing one uniform buffer to supply distinct [ToonSceneUniform] transforms for many draws in the same render pass.
func ToonSceneDynamicBindingLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	return toonSceneBindingLayout(cx, dev, true)
}

func toonSceneBindingLayout(cx *core.Scope, dev *gpu.Device, dynamic bool) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	label := "render3d.toon.scene"
	if dynamic {
		label = "render3d.toon.scene.dynamic"
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label,
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:          0,
			Visibility:       gpu.StageVertex | gpu.StageFragment,
			Type:             gpu.BindingUniform,
			HasDynamicOffset: dynamic,
		}},
	})
}

// ToonMaterialOptions configures [NewGPUToonMaterial].
type ToonMaterialOptions struct {
	// Label prefixes GPU resource labels in capture tools.
	Label string
	// Layout is an optional shared layout from [ToonMaterialBindingLayout] or [ToonRenderer.MaterialLayout]. When nil, the material creates and owns its own layout.
	Layout *gpu.BindGroupLayout
	// Retained keeps a CPU shadow copy of the uniform buffer (and of the ramp sampler the material creates) so NSGo restores them after a device loss. The [MaterialDefaults] and any caller-supplied textures and samplers must be retained too for the bind group to come back.
	Retained bool
}

// GPUToonMaterial holds a [ToonMaterial]'s uniform buffer and its bind group for @group([ToonMaterialGroup]). Omitted textures and samplers come from a shared [MaterialDefaults]. Resources belong to the [core.Scope] passed to [NewGPUToonMaterial] and are released when that scope is disposed or when [GPUToonMaterial.Close] is called.
type GPUToonMaterial struct {
	mat      ToonMaterial
	layout   *gpu.BindGroupLayout
	ownLay   bool
	uniform  *gpu.Buffer
	group    *gpu.BindGroup
	rampSamp *gpu.Sampler
	scratch  [ToonUniformSize]byte
}

// NewGPUToonMaterial validates mat and allocates its uniform buffer and @group(1) bind group on dev, scoped to cx, binding defaults' 1x1 white textures and sampler for omitted textures and samplers, as [NewGPUMaterial] does. Create one [MaterialDefaults] per device and share it. Invalid parameters or defaults return an error wrapping [ErrInvalidToonMaterial] before touching the device.
func NewGPUToonMaterial(cx *core.Scope, dev *gpu.Device, defaults *MaterialDefaults, mat ToonMaterial, opts ToonMaterialOptions) (*GPUToonMaterial, error) {
	if err := mat.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if !defaults.Valid() {
		return nil, fmt.Errorf("%w: material defaults are not valid", ErrInvalidToonMaterial)
	}
	if opts.Layout != nil && !opts.Layout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid toon material bind group layout"}
	}

	label := opts.Label
	if label == "" {
		label = "render3d.toon.material"
	}

	gm := &GPUToonMaterial{mat: mat}
	cleanupOnErr := true
	defer func() {
		if cleanupOnErr {
			_ = gm.Close()
		}
	}()

	var err error
	if opts.Layout != nil {
		gm.layout = opts.Layout
	} else {
		gm.layout, err = ToonMaterialBindingLayout(cx, dev)
		if err != nil {
			return nil, err
		}
		gm.ownLay = true
	}

	data := mat.AppendUniformData(gm.scratch[:0])
	gm.uniform, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " uniform",
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     data,
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	baseTex := defaults.WhiteSRGB().Texture()
	if mat.BaseColorTexture != nil {
		baseTex = mat.BaseColorTexture.Texture()
	}
	baseSamp := defaults.Sampler().Sampler()
	if mat.BaseColorSampler != nil {
		baseSamp = mat.BaseColorSampler.Sampler()
	}
	rampTex := defaults.WhiteLinear().Texture()
	rampSamp := defaults.Sampler().Sampler()
	if mat.RampTexture != nil {
		rampTex = mat.RampTexture.Texture()
		if mat.RampSampler == nil {
			rampOpts := SamplerLinearClamp()
			rampOpts.Label = label + " ramp sampler"
			rampOpts.Retained = opts.Retained
			gm.rampSamp, err = dev.CreateSampler(cx, rampOpts.Desc())
			if err != nil {
				return nil, err
			}
			rampSamp = gm.rampSamp
		}
	}
	if mat.RampSampler != nil {
		rampSamp = mat.RampSampler.Sampler()
	}

	gm.group, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label,
		Layout: gm.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: gm.uniform, Offset: 0, Size: ToonUniformSize},
			{Binding: 1, Texture: baseTex},
			{Binding: 2, Sampler: baseSamp},
			{Binding: 3, Texture: rampTex},
			{Binding: 4, Sampler: rampSamp},
		},
	})
	if err != nil {
		return nil, err
	}

	cleanupOnErr = false
	return gm, nil
}

// Update validates mat and uploads its updated uniform parameters into m's uniform buffer on dev without allocating. It takes dev explicitly so a retained material remains updatable after a GPU device loss and restore. Texture and sampler handles bound in the bind group cannot be changed in-place; create a new [GPUToonMaterial] to swap textures or samplers.
func (m *GPUToonMaterial) Update(dev *gpu.Device, mat ToonMaterial) error {
	if m == nil || !m.Valid() {
		return fmt.Errorf("%w: material is not valid", ErrInvalidToonMaterial)
	}
	if err := mat.Validate(); err != nil {
		return err
	}
	if mat.BaseColorTexture != m.mat.BaseColorTexture ||
		mat.BaseColorSampler != m.mat.BaseColorSampler ||
		mat.RampTexture != m.mat.RampTexture ||
		mat.RampSampler != m.mat.RampSampler {
		return fmt.Errorf("%w: cannot swap bound textures or samplers in Update; create a new GPUToonMaterial", ErrInvalidToonMaterial)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	m.mat = mat
	data := mat.AppendUniformData(m.scratch[:0])
	dev.WriteBuffer(m.uniform, 0, data)
	return nil
}

// Bind binds the material's bind group at [ToonMaterialGroup] (group 1) on pass. It does nothing when m is not [GPUToonMaterial.Valid].
func (m *GPUToonMaterial) Bind(pass *gpu.RenderPass) {
	m.BindAt(pass, ToonMaterialGroup)
}

// BindAt binds the material's bind group at the specified group index on pass. It does nothing when m is not [GPUToonMaterial.Valid].
func (m *GPUToonMaterial) BindAt(pass *gpu.RenderPass, group uint32) {
	if !m.Valid() || pass == nil {
		return
	}
	pass.SetBindGroup(group, m.group)
}

// Material returns the latest [ToonMaterial] uploaded to m.
func (m *GPUToonMaterial) Material() ToonMaterial {
	if m == nil {
		return ToonMaterial{}
	}
	return m.mat
}

// HasOutline reports whether m's current material enables the inverted-hull outline pass.
func (m *GPUToonMaterial) HasOutline() bool {
	return m != nil && m.mat.HasOutline()
}

// Layout returns the bind group layout for @group([ToonMaterialGroup]).
func (m *GPUToonMaterial) Layout() *gpu.BindGroupLayout {
	if m == nil {
		return nil
	}
	return m.layout
}

// BindGroup returns the underlying [gpu.BindGroup], or nil for a nil receiver.
func (m *GPUToonMaterial) BindGroup() *gpu.BindGroup {
	if m == nil {
		return nil
	}
	return m.group
}

// ByteSize returns the GPU buffer memory owned by m (excluding shared defaults and caller-owned textures).
func (m *GPUToonMaterial) ByteSize() uint64 {
	if m == nil || m.uniform == nil {
		return 0
	}
	return m.uniform.Size()
}

// Valid reports whether m's GPU resources are open and belong to a live device.
func (m *GPUToonMaterial) Valid() bool {
	if m == nil || m.uniform == nil || m.group == nil {
		return false
	}
	if !m.uniform.Valid() || !m.group.Valid() {
		return false
	}
	if m.rampSamp != nil && !m.rampSamp.Valid() {
		return false
	}
	return true
}

// Close releases all GPU resources owned by m (never closing the shared defaults, caller-owned textures and samplers, or shared layouts). It is safe to call on a nil receiver or more than once.
func (m *GPUToonMaterial) Close() error {
	if m == nil {
		return nil
	}
	var err error
	if m.group != nil {
		err = errors.Join(err, m.group.Close())
	}
	if m.uniform != nil {
		err = errors.Join(err, m.uniform.Close())
	}
	if m.rampSamp != nil {
		err = errors.Join(err, m.rampSamp.Close())
	}
	if m.ownLay && m.layout != nil {
		err = errors.Join(err, m.layout.Close())
	}
	return err
}

// ToonPipelineOptions configures [NewToonPipeline] and [NewToonOutlinePipeline].
type ToonPipelineOptions struct {
	// Label names the pipeline in GPU capture tools.
	Label string
	// CameraLayout is the @group(0) bind group layout (from [CameraBuffer.Layout]).
	CameraLayout *gpu.BindGroupLayout
	// MaterialLayout is the @group(1) bind group layout (from [ToonMaterialBindingLayout] or [GPUToonMaterial.Layout]).
	MaterialLayout *gpu.BindGroupLayout
	// SceneLayout is the @group(2) bind group layout (from [ToonSceneBindingLayout] or [ToonSceneDynamicBindingLayout]).
	SceneLayout *gpu.BindGroupLayout
	// ColorFormat is the color attachment format; zero defaults to dev.SurfaceFormat().
	ColorFormat gpu.TextureFormat
	// DepthFormat is the depth attachment format (e.g. [gpu.FormatDepth24Plus]), or [gpu.FormatUndefined] for no depth buffer.
	DepthFormat gpu.TextureFormat
	// DoubleSided disables back-face culling (uses [gpu.CullNone] instead of [gpu.CullBack]) on the main cel-shaded pass. Ignored by [NewToonOutlinePipeline], which always uses [gpu.CullFront].
	DoubleSided bool
	// Blend sets an optional color blend state on the main cel-shaded pass (nil means opaque).
	Blend *gpu.BlendState
	// Retained opts the pipeline into automatic recreation after a GPU device loss.
	Retained bool
}

// NewToonPipeline compiles [ToonWGSL] and creates the main cel-shaded render pipeline on dev, scoped to cx.
func NewToonPipeline(cx *core.Scope, dev *gpu.Device, opts ToonPipelineOptions) (*gpu.Pipeline, error) {
	if err := validateToonPipelineOpts(dev, opts); err != nil {
		return nil, err
	}
	mod, err := gpu.CompileWGSL(ToonWGSL)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile toon shader: %w", err)
	}
	label := opts.Label
	if label == "" {
		label = "render3d.toon"
	}
	cull := gpu.CullBack
	if opts.DoubleSided {
		cull = gpu.CullNone
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{opts.CameraLayout, opts.MaterialLayout, opts.SceneLayout},
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: cull},
		Targets:       []gpu.ColorTargetState{{Format: colorFmt, Blend: opts.Blend, WriteMask: gpu.ColorWriteAll}},
		Retained:      opts.Retained,
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

// NewToonOutlinePipeline compiles [ToonOutlineWGSL] and creates the inverted-hull outline render pipeline ([gpu.CullFront]) on dev, scoped to cx.
func NewToonOutlinePipeline(cx *core.Scope, dev *gpu.Device, opts ToonPipelineOptions) (*gpu.Pipeline, error) {
	if err := validateToonPipelineOpts(dev, opts); err != nil {
		return nil, err
	}
	mod, err := gpu.CompileWGSL(ToonOutlineWGSL)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile toon outline shader: %w", err)
	}
	label := opts.Label
	if label == "" {
		label = "render3d.toon.outline"
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_outline"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_outline"},
		Layouts:       []*gpu.BindGroupLayout{opts.CameraLayout, opts.MaterialLayout, opts.SceneLayout},
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullFront},
		Targets:       []gpu.ColorTargetState{{Format: colorFmt, Blend: opts.Blend, WriteMask: gpu.ColorWriteAll}},
		Retained:      opts.Retained,
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

func validateToonPipelineOpts(dev *gpu.Device, opts ToonPipelineOptions) error {
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if opts.CameraLayout == nil || !opts.CameraLayout.Valid() {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	if opts.MaterialLayout == nil || !opts.MaterialLayout.Valid() {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid toon material bind group layout"}
	}
	if opts.SceneLayout == nil || !opts.SceneLayout.Valid() {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid toon scene bind group layout"}
	}
	return nil
}

// ToonDraw describes one cel-shaded object submitted to [ToonRenderer.Draw].
type ToonDraw struct {
	// Mesh is the main surface geometry drawn in the cel-shaded pass (and in the outline pass when OutlineMesh is nil).
	Mesh *GPUMesh
	// OutlineMesh is an optional mesh (for example built with [SmoothOutlineNormals]) drawn in the inverted-hull outline pass. When nil, Mesh is used.
	OutlineMesh *GPUMesh
	// Material supplies the cel-shading and outline parameters.
	Material *GPUToonMaterial
	// Transform is the object's world-space model matrix. A zero matrix is treated as [math3d.Mat4Identity].
	Transform math3d.Mat4
}

// ToonRendererOptions configures [NewToonRenderer].
type ToonRendererOptions struct {
	// Label prefixes GPU resource labels in capture tools.
	Label string
	// CameraLayout is the @group(0) layout from [CameraBuffer.Layout] (required).
	CameraLayout *gpu.BindGroupLayout
	// ColorFormat is the color attachment format; zero defaults to dev.SurfaceFormat().
	ColorFormat gpu.TextureFormat
	// DepthFormat is the depth attachment format (e.g. [gpu.FormatDepth24Plus]), or [gpu.FormatUndefined] without depth.
	DepthFormat gpu.TextureFormat
	// DoubleSided disables back-face culling on the main cel-shaded pass.
	DoubleSided bool
	// InitialCapacity is the starting number of per-draw transform slots in the dynamic scene uniform buffer (defaults to 32). The buffer grows when the draws of one frame need more slots.
	InitialCapacity int
	// Retained opts owned pipelines, layouts, and uniform buffers into automatic restoration across GPU device loss.
	Retained bool
}

// ToonRenderer manages both the inverted-hull outline pipeline and main cel-shaded pipeline together with a dynamic-offset per-draw scene uniform buffer, drawing outlines first and cel-shaded surfaces second in a single render pass (#262). Both pipelines draw opaque, with depth writes; for blended toon surfaces build pipelines with [NewToonPipeline] and [NewToonOutlinePipeline] and a [ToonPipelineOptions.Blend].
type ToonRenderer struct {
	cx       *core.Scope
	label    string
	retained bool

	matLayout       *gpu.BindGroupLayout
	sceneLayout     *gpu.BindGroupLayout
	toonPipeline    *gpu.Pipeline
	outlinePipeline *gpu.Pipeline

	sceneStride uint32
	sceneCap    int
	sceneBuf    *gpu.Buffer
	sceneGroup  *gpu.BindGroup
	staging     []byte

	// cursor is the first scene slot of sceneBuf no Draw has used since the last Begin. Every Draw writes its slots after the previous one's, because buffer writes are applied before the submission's commands and a later write to the same slots would replace the transforms an earlier Draw recorded.
	cursor int
	// lastFrame is dev.FrameIndex() at the last Draw, so Draw can rewind on its own when a gpu.Frame has been submitted in between.
	lastFrame uint64
	drawn     bool
	// retired holds the scene buffers and bind groups a Draw outgrew while earlier draws of the same frame still reference them; Begin closes them.
	retired []toonSceneSlots
}

type toonSceneSlots struct {
	buf   *gpu.Buffer
	group *gpu.BindGroup
}

// NewToonRenderer creates the shared material/scene layouts, both toon and outline pipelines, and the dynamic scene uniform buffer on dev, scoped to cx.
func NewToonRenderer(cx *core.Scope, dev *gpu.Device, opts ToonRendererOptions) (*ToonRenderer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if opts.CameraLayout == nil || !opts.CameraLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.toon"
	}
	capSlots := opts.InitialCapacity
	if capSlots <= 0 {
		capSlots = 32
	}

	r := &ToonRenderer{
		cx:       cx,
		label:    label,
		retained: opts.Retained,
	}
	cleanupOnErr := true
	defer func() {
		if cleanupOnErr {
			_ = r.Close()
		}
	}()

	var err error
	r.matLayout, err = ToonMaterialBindingLayout(cx, dev)
	if err != nil {
		return nil, err
	}
	r.sceneLayout, err = ToonSceneDynamicBindingLayout(cx, dev)
	if err != nil {
		return nil, err
	}

	pipeOpts := ToonPipelineOptions{
		Label:          label,
		CameraLayout:   opts.CameraLayout,
		MaterialLayout: r.matLayout,
		SceneLayout:    r.sceneLayout,
		ColorFormat:    opts.ColorFormat,
		DepthFormat:    opts.DepthFormat,
		DoubleSided:    opts.DoubleSided,
		Retained:       opts.Retained,
	}
	r.toonPipeline, err = NewToonPipeline(cx, dev, pipeOpts)
	if err != nil {
		return nil, err
	}
	pipeOpts.Label = label + ".outline"
	r.outlinePipeline, err = NewToonOutlinePipeline(cx, dev, pipeOpts)
	if err != nil {
		return nil, err
	}

	align := dev.Limits().MinUniformBufferAlignment
	if align == 0 {
		align = 256
	}
	r.sceneStride = toonAlignUp(ToonSceneUniformSize, align)
	if err := r.ensureSceneCapacity(dev, capSlots); err != nil {
		return nil, err
	}

	cleanupOnErr = false
	return r, nil
}

// MaterialLayout returns the @group([ToonMaterialGroup]) layout to pass in [ToonMaterialOptions.Layout] when creating [GPUToonMaterial] instances for this renderer.
func (r *ToonRenderer) MaterialLayout() *gpu.BindGroupLayout {
	if r == nil {
		return nil
	}
	return r.matLayout
}

// NewMaterial is a convenience wrapper around [NewGPUToonMaterial] that binds mat to r's [ToonRenderer.MaterialLayout] and inherits r's Retained setting.
func (r *ToonRenderer) NewMaterial(cx *core.Scope, dev *gpu.Device, defaults *MaterialDefaults, mat ToonMaterial, label string) (*GPUToonMaterial, error) {
	if r == nil || !r.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: toon renderer is not valid"}
	}
	return NewGPUToonMaterial(cx, dev, defaults, mat, ToonMaterialOptions{
		Label:    label,
		Layout:   r.matLayout,
		Retained: r.retained,
	})
}

// ToonPipeline returns the main cel-shaded pipeline.
func (r *ToonRenderer) ToonPipeline() *gpu.Pipeline {
	if r == nil {
		return nil
	}
	return r.toonPipeline
}

// OutlinePipeline returns the inverted-hull outline pipeline.
func (r *ToonRenderer) OutlinePipeline() *gpu.Pipeline {
	if r == nil {
		return nil
	}
	return r.outlinePipeline
}

// Valid reports whether all GPU resources owned by r are open and belong to a live device.
func (r *ToonRenderer) Valid() bool {
	if r == nil {
		return false
	}
	return r.matLayout != nil && r.matLayout.Valid() &&
		r.sceneLayout != nil && r.sceneLayout.Valid() &&
		r.toonPipeline != nil && r.toonPipeline.Valid() &&
		r.outlinePipeline != nil && r.outlinePipeline.Valid() &&
		r.sceneBuf != nil && r.sceneBuf.Valid() &&
		r.sceneGroup != nil && r.sceneGroup.Valid()
}

// Begin starts a new frame: the next Draw reuses the scene uniform slots from the start of the buffer, and scene buffers that a Draw outgrew during the previous frame are released. Call it once per frame, after the previous frame's submission and before its first Draw, when submitting with [gpu.Device.Submit] directly. Inside a [gpu.Frame] (for example a [gpu.View] OnFrame callback) Draw rewinds on its own when [gpu.Device.FrameIndex] advances, so calling Begin there is optional.
func (r *ToonRenderer) Begin() {
	if r == nil {
		return
	}
	r.cursor = 0
	r.drawn = false
	for _, s := range r.retired {
		_ = s.group.Close()
		_ = s.buf.Close()
	}
	clear(r.retired)
	r.retired = r.retired[:0]
}

// Draw writes per-draw transform/light uniforms and records the inverted-hull outline pass followed by the main cel-shaded pass into pass. Draws with invalid meshes or materials are skipped. Draw may run any number of times between two submissions, into one pass or several: each call writes its uniforms to slots no earlier call of the frame uses (see [ToonRenderer.Begin]). It returns a [gpu.Error] when r, dev, pass, or camGroup is nil, closed, or lost, an error wrapping [ErrInvalidLight] when light is invalid, and the error from growing the uniform buffer.
func (r *ToonRenderer) Draw(dev *gpu.Device, pass *gpu.RenderPass, camGroup *gpu.BindGroup, light ToonLight, draws ...ToonDraw) error {
	if r == nil || !r.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: toon renderer is closed or its device is lost"}
	}
	if dev == nil || dev.IsLost() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil or lost device"}
	}
	if pass == nil {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: nil render pass"}
	}
	if camGroup == nil || !camGroup.Valid() {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group"}
	}
	if err := light.Validate(); err != nil {
		return err
	}
	if len(draws) == 0 {
		return nil
	}
	if r.drawn && dev.FrameIndex() != r.lastFrame {
		r.Begin()
	}
	r.lastFrame = dev.FrameIndex()
	r.drawn = true
	if err := r.ensureSceneCapacity(dev, r.cursor+len(draws)); err != nil {
		return err
	}
	base := r.cursor

	stride := int(r.sceneStride)
	totalBytes := len(draws) * stride
	if cap(r.staging) < totalBytes {
		r.staging = make([]byte, totalBytes)
	} else {
		r.staging = r.staging[:totalBytes]
	}

	anyValid := false
	anyOutline := false
	for i, d := range draws {
		if !d.Mesh.Valid() || !d.Material.Valid() {
			continue
		}
		anyValid = true
		if d.Material.HasOutline() {
			anyOutline = true
		}
		off := i * stride
		u := NewToonSceneUniform(d.Transform, light)
		u.AppendUniformData(r.staging[off:off])
	}
	if !anyValid {
		return nil
	}

	dev.WriteBuffer(r.sceneBuf, uint64(base)*uint64(r.sceneStride), r.staging)
	r.cursor = base + len(draws)

	if anyOutline {
		pass.SetPipeline(r.outlinePipeline)
		pass.SetBindGroup(CameraGroup, camGroup)
		for i, d := range draws {
			if !d.Mesh.Valid() || !d.Material.Valid() || !d.Material.HasOutline() {
				continue
			}
			outlineMesh := d.OutlineMesh
			if outlineMesh == nil {
				outlineMesh = d.Mesh
			}
			if !outlineMesh.Valid() {
				continue
			}
			d.Material.Bind(pass)
			pass.SetBindGroup(ToonSceneGroup, r.sceneGroup, uint32(base+i)*r.sceneStride)
			outlineMesh.Draw(pass)
		}
	}

	pass.SetPipeline(r.toonPipeline)
	pass.SetBindGroup(CameraGroup, camGroup)
	for i, d := range draws {
		if !d.Mesh.Valid() || !d.Material.Valid() {
			continue
		}
		d.Material.Bind(pass)
		pass.SetBindGroup(ToonSceneGroup, r.sceneGroup, uint32(base+i)*r.sceneStride)
		d.Mesh.Draw(pass)
	}
	return nil
}

// Close releases all GPU resources owned by r. It is safe to call on a nil receiver or more than once.
func (r *ToonRenderer) Close() error {
	if r == nil {
		return nil
	}
	var err error
	for _, s := range r.retired {
		err = errors.Join(err, s.group.Close(), s.buf.Close())
	}
	r.retired = nil
	if r.sceneGroup != nil {
		err = errors.Join(err, r.sceneGroup.Close())
	}
	if r.sceneBuf != nil {
		err = errors.Join(err, r.sceneBuf.Close())
	}
	if r.outlinePipeline != nil {
		err = errors.Join(err, r.outlinePipeline.Close())
	}
	if r.toonPipeline != nil {
		err = errors.Join(err, r.toonPipeline.Close())
	}
	if r.sceneLayout != nil {
		err = errors.Join(err, r.sceneLayout.Close())
	}
	if r.matLayout != nil {
		err = errors.Join(err, r.matLayout.Close())
	}
	return err
}

func (r *ToonRenderer) ensureSceneCapacity(dev *gpu.Device, need int) error {
	if need <= r.sceneCap && r.sceneBuf != nil && r.sceneBuf.Valid() && r.sceneGroup != nil && r.sceneGroup.Valid() {
		return nil
	}
	if need > MaxToonDrawsPerFrame {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: fmt.Sprintf("render3d: %d toon draws since the last ToonRenderer.Begin exceed MaxToonDrawsPerFrame (%d); call Begin once per frame", need, MaxToonDrawsPerFrame)}
	}
	nextCap := max(r.sceneCap*2, need)
	if nextCap < 32 {
		nextCap = 32
	}
	buf, err := dev.CreateBuffer(r.cx, gpu.BufferDesc{
		Label:    r.label + " scene",
		Size:     uint64(nextCap) * uint64(r.sceneStride),
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: r.retained,
	})
	if err != nil {
		return err
	}
	group, err := dev.CreateBindGroupDesc(r.cx, gpu.BindGroupDesc{
		Label:  r.label + " scene",
		Layout: r.sceneLayout,
		Entries: []gpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  buf,
			Offset:  0,
			Size:    ToonSceneUniformSize,
		}},
	})
	if err != nil {
		_ = buf.Close()
		return err
	}
	if r.sceneGroup != nil && r.sceneBuf != nil {
		if r.cursor > 0 {
			// Draws already recorded this frame still bind the old buffer, so it stays open until Begin.
			r.retired = append(r.retired, toonSceneSlots{buf: r.sceneBuf, group: r.sceneGroup})
		} else {
			_ = r.sceneGroup.Close()
			_ = r.sceneBuf.Close()
		}
	}
	r.sceneBuf = buf
	r.sceneGroup = group
	r.sceneCap = nextCap
	r.cursor = 0
	return nil
}

func toonAlignUp(n, align uint32) uint32 {
	if align <= 1 {
		return n
	}
	rem := n % align
	if rem == 0 {
		return n
	}
	return n + (align - rem)
}

func toonSmoothstep(edge0, edge1, x float32) float32 {
	denom := edge1 - edge0
	if denom < 1e-5 {
		denom = 1e-5
	}
	t := clamp01((x - edge0) / denom)
	return t * t * (3 - 2*t)
}

func toonNormalizeVec3(v math3d.Vec3) (math3d.Vec3, bool) {
	l := v.LenSq()
	if !(l > 1e-10) || isInf32(l) {
		return math3d.Vec3{}, false
	}
	return v.Scale(1 / float32(math.Sqrt(float64(l)))), true
}

func toonNormalMatrix(model math3d.Mat4) math3d.Mat4 {
	inv, ok := model.Inverse()
	if !ok {
		return math3d.Mat4Identity()
	}
	tr := inv.Transpose()
	return math3d.Mat4FromCols(
		math3d.Vec4{X: tr.At(0, 0), Y: tr.At(1, 0), Z: tr.At(2, 0), W: 0},
		math3d.Vec4{X: tr.At(0, 1), Y: tr.At(1, 1), Z: tr.At(2, 1), W: 0},
		math3d.Vec4{X: tr.At(0, 2), Y: tr.At(1, 2), Z: tr.At(2, 2), W: 0},
		math3d.Vec4{X: 0, Y: 0, Z: 0, W: 1},
	)
}
