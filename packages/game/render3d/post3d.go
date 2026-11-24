package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ErrInvalidPostProcess is wrapped by every validation error returned by 3D
// post-processing configurations and constructors ([BloomConfig.Validate],
// [SSAOConfig.Validate], [DepthOfFieldConfig.Validate], [NewBloomPass],
// [NewSSAOPass], [NewDepthOfFieldPass], and [NewPostStack3D]).
var ErrInvalidPostProcess = errors.New("render3d: invalid post-processing configuration")

const (
	postMaxBloomBlurPasses = 8
	postMaxSSAOSamples     = 16
	postMinSSAOSamples     = 4
	postMaxSSAOBlurRadius  = 4
	postDoFTapCount        = 18

	postBloomExtractUniformSize   = 16
	postBloomBlurUniformSize      = 16
	postBloomCompositeUniformSize = 16
	postSSAOUniformSize           = 64 + 64 + 16 + 16 + postMaxSSAOSamples*16 // 416 bytes
	postSSAOBlurUniformSize       = 64 + 16                                   // 80 bytes
	postDoFCoCUniformSize         = 32
	postDoFBokehUniformSize       = 16 + postDoFTapCount*16 // 304 bytes
)

// Shared fullscreen triangle vertex stage prepended to every post-processing WGSL module.
// Z is 0.0 (never 1.0) so fullscreen triangles never clip at the far depth plane.
const postFullscreenVertexWGSL = `
struct PostVertexIn {
	@location(0) pos: vec2<f32>,
	@location(1) uv: vec2<f32>,
}

struct PostVertexOut {
	@builtin(position) pos: vec4<f32>,
	@location(0) uv: vec2<f32>,
}

@vertex
fn vs_post(in: PostVertexIn) -> PostVertexOut {
	var out: PostVertexOut;
	out.pos = vec4<f32>(in.pos, 0.0, 1.0);
	out.uv = in.uv;
	return out;
}
`

const postBlitWGSL = postFullscreenVertexWGSL + `
@group(0) @binding(0) var src_tex: texture_2d<f32>;
@group(0) @binding(1) var src_samp: sampler;

@fragment
fn fs_blit(in: PostVertexOut) -> @location(0) vec4<f32> {
	return textureSampleLevel(src_tex, src_samp, in.uv, 0.0);
}
`

const postBloomExtractWGSL = postFullscreenVertexWGSL + `
struct BloomExtractUniform {
	params: vec4<f32>, // x=threshold, y=soft_knee, z=0, w=0
}

@group(0) @binding(0) var<uniform> u: BloomExtractUniform;
@group(0) @binding(1) var src_tex: texture_2d<f32>;
@group(0) @binding(2) var src_samp: sampler;

@fragment
fn fs_bloom_extract(in: PostVertexOut) -> @location(0) vec4<f32> {
	let c = textureSampleLevel(src_tex, src_samp, in.uv, 0.0);
	let threshold = u.params.x;
	let soft_knee = u.params.y;
	let br = max(c.r, max(c.g, c.b));
	let knee = threshold * soft_knee;
	var contrib: f32 = 0.0;
	if (knee > 0.0001) {
		let soft = clamp(br - threshold + knee, 0.0, 2.0 * knee);
		let soft_c = (soft * soft) / (4.0 * knee);
		contrib = max(soft_c, br - threshold) / max(br, 0.0001);
	} else if (br > threshold) {
		contrib = (br - threshold) / max(br, 0.0001);
	}
	let bright = c.rgb * max(contrib, 0.0);
	return vec4<f32>(bright, 1.0);
}
`

// 9-tap separable Gaussian blur with precomputed weights (sum = 1.0, no exp/exp2 calls).
const postBloomBlurWGSL = postFullscreenVertexWGSL + `
struct BloomBlurUniform {
	step_dir: vec4<f32>, // x=dx, y=dy, z=0, w=0
}

@group(0) @binding(0) var<uniform> u: BloomBlurUniform;
@group(0) @binding(1) var src_tex: texture_2d<f32>;
@group(0) @binding(2) var src_samp: sampler;

@fragment
fn fs_bloom_blur(in: PostVertexOut) -> @location(0) vec4<f32> {
	let d = u.step_dir.xy;
	var sum = textureSampleLevel(src_tex, src_samp, in.uv, 0.0).rgb * 0.22702703;
	sum = sum + textureSampleLevel(src_tex, src_samp, in.uv + d * 1.0, 0.0).rgb * 0.19459459;
	sum = sum + textureSampleLevel(src_tex, src_samp, in.uv - d * 1.0, 0.0).rgb * 0.19459459;
	sum = sum + textureSampleLevel(src_tex, src_samp, in.uv + d * 2.0, 0.0).rgb * 0.12162162;
	sum = sum + textureSampleLevel(src_tex, src_samp, in.uv - d * 2.0, 0.0).rgb * 0.12162162;
	sum = sum + textureSampleLevel(src_tex, src_samp, in.uv + d * 3.0, 0.0).rgb * 0.05405405;
	sum = sum + textureSampleLevel(src_tex, src_samp, in.uv - d * 3.0, 0.0).rgb * 0.05405405;
	sum = sum + textureSampleLevel(src_tex, src_samp, in.uv + d * 4.0, 0.0).rgb * 0.01621622;
	sum = sum + textureSampleLevel(src_tex, src_samp, in.uv - d * 4.0, 0.0).rgb * 0.01621622;
	return vec4<f32>(sum, 1.0);
}
`

const postBloomCompositeWGSL = postFullscreenVertexWGSL + `
struct BloomCompositeUniform {
	params: vec4<f32>, // x=intensity, y=0, z=0, w=0
}

@group(0) @binding(0) var<uniform> u: BloomCompositeUniform;
@group(0) @binding(1) var scene_tex: texture_2d<f32>;
@group(0) @binding(2) var bloom_tex: texture_2d<f32>;
@group(0) @binding(3) var samp: sampler;

@fragment
fn fs_bloom_composite(in: PostVertexOut) -> @location(0) vec4<f32> {
	let base = textureSampleLevel(scene_tex, samp, in.uv, 0.0);
	let glow = textureSampleLevel(bloom_tex, samp, in.uv, 0.0).rgb;
	return vec4<f32>(base.rgb + glow * u.params.x, base.a);
}
`

const postSSAOWGSL = postFullscreenVertexWGSL + `
struct SSAOUniform {
	proj: mat4x4<f32>,
	inv_proj: mat4x4<f32>,
	params: vec4<f32>, // x=radius, y=bias, z=intensity, w=power
	screen: vec4<f32>, // x=1/w, y=1/h, z=sample_count, w=0
	kernel: array<vec4<f32>, 16>,
}

@group(0) @binding(0) var<uniform> u: SSAOUniform;
@group(0) @binding(1) var depth_tex: texture_depth_2d;
@group(0) @binding(2) var depth_samp: sampler;

fn ssao_view_pos(uv: vec2<f32>, d: f32) -> vec3<f32> {
	let ndc = vec4<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, d, 1.0);
	let vh = u.inv_proj * ndc;
	return vh.xyz / vh.w;
}

@fragment
fn fs_ssao(in: PostVertexOut) -> @location(0) vec4<f32> {
	let dC = textureSampleLevel(depth_tex, depth_samp, in.uv, 0);
	if (dC >= 0.9999) {
		return vec4<f32>(1.0, 1.0, 1.0, 1.0);
	}
	let intensity = u.params.z;
	if (intensity <= 0.0) {
		return vec4<f32>(1.0, 1.0, 1.0, 1.0);
	}

	let tx = u.screen.x;
	let ty = u.screen.y;
	let pC = ssao_view_pos(in.uv, dC);

	let uvR = vec2<f32>(min(in.uv.x + tx, 1.0), in.uv.y);
	let uvL = vec2<f32>(max(in.uv.x - tx, 0.0), in.uv.y);
	let uvU = vec2<f32>(in.uv.x, max(in.uv.y - ty, 0.0));
	let uvD = vec2<f32>(in.uv.x, min(in.uv.y + ty, 1.0));

	let pR = ssao_view_pos(uvR, textureSampleLevel(depth_tex, depth_samp, uvR, 0));
	let pL = ssao_view_pos(uvL, textureSampleLevel(depth_tex, depth_samp, uvL, 0));
	let pU = ssao_view_pos(uvU, textureSampleLevel(depth_tex, depth_samp, uvU, 0));
	let pD = ssao_view_pos(uvD, textureSampleLevel(depth_tex, depth_samp, uvD, 0));

	var dx = pR - pC;
	if (abs(pC.z - pL.z) < abs(pR.z - pC.z)) {
		dx = pC - pL;
	}
	var dy = pU - pC;
	if (abs(pC.z - pD.z) < abs(pU.z - pC.z)) {
		dy = pC - pD;
	}
	let cross_n = cross(dx, dy);
	var n = vec3<f32>(0.0, 0.0, 1.0);
	if (dot(cross_n, cross_n) > 1e-12) {
		n = normalize(cross_n);
	}

	// Deterministic 4x4 stratified rotation angle in [0, 2*pi).
	let ix = floor(in.pos.x);
	let iy = floor(in.pos.y);
	let cx = ix - floor(ix * 0.25) * 4.0;
	let cy = iy - floor(iy * 0.25) * 4.0;
	let cell = cy * 4.0 + cx;
	let ridx = cell * 7.0 - floor(cell * 7.0 * 0.0625) * 16.0;
	let ang = ridx * 0.39269908;
	var rvec = vec3<f32>(cos(ang), sin(ang), 0.0);
	if (abs(dot(rvec, n)) > 0.95) {
		rvec = vec3<f32>(0.0, 0.0, 1.0);
		if (abs(dot(rvec, n)) > 0.95) {
			rvec = vec3<f32>(1.0, 0.0, 0.0);
		}
	}
	let tangent = normalize(rvec - n * dot(rvec, n));
	let bitangent = cross(n, tangent);

	let radius = u.params.x;
	let bias = u.params.y;
	let power = u.params.w;
	let count = i32(u.screen.z + 0.5);

	var occlusion: f32 = 0.0;
	for (var i: i32 = 0; i < 16; i = i + 1) {
		if (i >= count) {
			break;
		}
		let k = u.kernel[i].xyz;
		let offset = tangent * k.x + bitangent * k.y + n * k.z;
		let spos = pC + offset * radius;

		let clip = u.proj * vec4<f32>(spos, 1.0);
		if (clip.w <= 0.0001) {
			continue;
		}
		let ndc = clip.xyz / clip.w;
		let suv = vec2<f32>(ndc.x * 0.5 + 0.5, 0.5 - ndc.y * 0.5);
		if (suv.x < 0.0 || suv.x > 1.0 || suv.y < 0.0 || suv.y > 1.0) {
			continue;
		}
		let occ_d = textureSampleLevel(depth_tex, depth_samp, suv, 0);
		if (occ_d >= 0.9999) {
			continue;
		}
		let occ_pos = ssao_view_pos(suv, occ_d);
		let dz = abs(pC.z - occ_pos.z);
		let range_check = 1.0 - smoothstep(radius * 0.5, radius * 1.5, dz);
		if (occ_pos.z >= spos.z + bias) {
			occlusion = occlusion + range_check;
		}
	}

	let raw_ao = 1.0 - clamp((occlusion / max(u.screen.z, 1.0)) * intensity, 0.0, 1.0);
	var ao = 1.0;
	if (raw_ao < 0.9999) {
		ao = pow(max(raw_ao, 0.0001), power);
	}
	return vec4<f32>(ao, ao, ao, 1.0);
}
`

const postSSAOBlurWGSL = postFullscreenVertexWGSL + `
struct SSAOBlurUniform {
	inv_proj: mat4x4<f32>,
	params: vec4<f32>, // x=1/w, y=1/h, z=blur_radius, w=depth_threshold
}

@group(0) @binding(0) var<uniform> u: SSAOBlurUniform;
@group(0) @binding(1) var ao_tex: texture_2d<f32>;
@group(0) @binding(2) var depth_tex: texture_depth_2d;
@group(0) @binding(3) var samp: sampler;

fn blur_view_z(uv: vec2<f32>, d: f32) -> f32 {
	let ndc = vec4<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, d, 1.0);
	let vh = u.inv_proj * ndc;
	return vh.z / vh.w;
}

@fragment
fn fs_ssao_blur(in: PostVertexOut) -> @location(0) vec4<f32> {
	let center_ao = textureSampleLevel(ao_tex, samp, in.uv, 0.0).r;
	let r = i32(u.params.z + 0.5);
	if (r <= 0) {
		return vec4<f32>(center_ao, center_ao, center_ao, 1.0);
	}
	let center_d = textureSampleLevel(depth_tex, samp, in.uv, 0);
	if (center_d >= 0.9999) {
		return vec4<f32>(1.0, 1.0, 1.0, 1.0);
	}
	let center_z = blur_view_z(in.uv, center_d);
	let thresh = max(u.params.w, 0.0001);
	let inv_r2 = 1.0 / f32(r * r + 1);

	var acc: f32 = 0.0;
	var wsum: f32 = 0.0;
	for (var dy: i32 = -r; dy <= r; dy = dy + 1) {
		for (var dx: i32 = -r; dx <= r; dx = dx + 1) {
			let suv = clamp(in.uv + vec2<f32>(f32(dx) * u.params.x, f32(dy) * u.params.y), vec2<f32>(0.0), vec2<f32>(1.0));
			let sd = textureSampleLevel(depth_tex, samp, suv, 0);
			if (sd >= 0.9999) {
				continue;
			}
			let sz = blur_view_z(suv, sd);
			let dz = abs(center_z - sz) / thresh;
			let depth_w = 1.0 / (1.0 + dz * dz);
			let dist2 = f32(dx * dx + dy * dy);
			let spatial_w = 1.0 / (1.0 + dist2 * inv_r2);
			let w = depth_w * spatial_w;
			let sao = textureSampleLevel(ao_tex, samp, suv, 0.0).r;
			acc = acc + sao * w;
			wsum = wsum + w;
		}
	}
	let out_ao = acc / max(wsum, 0.0001);
	return vec4<f32>(out_ao, out_ao, out_ao, 1.0);
}
`

const postSSAOCompositeWGSL = postFullscreenVertexWGSL + `
@group(0) @binding(0) var scene_tex: texture_2d<f32>;
@group(0) @binding(1) var ao_tex: texture_2d<f32>;
@group(0) @binding(2) var samp: sampler;

@fragment
fn fs_ssao_composite(in: PostVertexOut) -> @location(0) vec4<f32> {
	let scene = textureSampleLevel(scene_tex, samp, in.uv, 0.0);
	let ao = textureSampleLevel(ao_tex, samp, in.uv, 0.0).r;
	return vec4<f32>(scene.rgb * ao, scene.a);
}
`

const postDoFCoCWGSL = postFullscreenVertexWGSL + `
struct DoFCoCUniform {
	params: vec4<f32>, // x=focus_distance, y=focus_range, z=near, w=far
	flags: vec4<f32>,  // x=ortho (0 or 1), y=0, z=0, w=0
}

@group(0) @binding(0) var<uniform> u: DoFCoCUniform;
@group(0) @binding(1) var scene_tex: texture_2d<f32>;
@group(0) @binding(2) var depth_tex: texture_depth_2d;
@group(0) @binding(3) var color_samp: sampler;
@group(0) @binding(4) var depth_samp: sampler;

@fragment
fn fs_dof_coc(in: PostVertexOut) -> @location(0) vec4<f32> {
	let scene = textureSampleLevel(scene_tex, color_samp, in.uv, 0.0);
	let d = textureSampleLevel(depth_tex, depth_samp, in.uv, 0);
	let near = u.params.z;
	let far = u.params.w;
	var lin_d: f32 = far;
	if (u.flags.x > 0.5) {
		lin_d = near + d * (far - near);
	} else {
		lin_d = (near * far) / max(far - d * (far - near), 0.0001);
	}
	let signed_coc = clamp((lin_d - u.params.x) / max(u.params.y, 0.0001), -1.0, 1.0);
	let enc_coc = signed_coc * 0.5 + 0.5;
	return vec4<f32>(scene.rgb, enc_coc);
}
`

const postDoFBokehWGSL = postFullscreenVertexWGSL + `
struct DoFBokehUniform {
	params: vec4<f32>, // x=1/w, y=1/h, z=max_blur_radius, w=0
	taps: array<vec4<f32>, 18>, // x=dx, y=dy, z=ring_radius, w=0
}

@group(0) @binding(0) var<uniform> u: DoFBokehUniform;
@group(0) @binding(1) var scene_tex: texture_2d<f32>;
@group(0) @binding(2) var coc_tex: texture_2d<f32>;
@group(0) @binding(3) var samp: sampler;

@fragment
fn fs_dof_bokeh(in: PostVertexOut) -> @location(0) vec4<f32> {
	let base = textureSampleLevel(scene_tex, samp, in.uv, 0.0);
	let max_radius = u.params.z;
	if (max_radius <= 0.0001) {
		return base;
	}
	let center_coc_sample = textureSampleLevel(coc_tex, samp, in.uv, 0.0);
	let center_signed = center_coc_sample.a * 2.0 - 1.0;
	let center_coc = abs(center_signed);
	let texel = u.params.xy;

	var acc_rgb = base.rgb;
	var acc_w: f32 = 1.0;

	for (var i: i32 = 0; i < 18; i = i + 1) {
		let tap_info = u.taps[i];
		let r = tap_info.z;
		let suv = clamp(in.uv + tap_info.xy * max_radius * texel, vec2<f32>(0.0), vec2<f32>(1.0));
		let tap = textureSampleLevel(coc_tex, samp, suv, 0.0);
		let tap_signed = tap.a * 2.0 - 1.0;
		let tap_coc = abs(tap_signed);
		var eff_coc: f32 = min(tap_coc, center_coc);
		if (tap_signed < 0.0) {
			eff_coc = tap_coc;
		}
		let w = smoothstep(r - 0.25, r + 0.1, eff_coc);
		acc_rgb = acc_rgb + tap.rgb * w;
		acc_w = acc_w + w;
	}

	return vec4<f32>(acc_rgb / acc_w, base.a);
}
`

type postShaderCache struct {
	once           sync.Once
	err            error
	blit           *gpu.ShaderModule
	bloomExtract   *gpu.ShaderModule
	bloomBlur      *gpu.ShaderModule
	bloomComposite *gpu.ShaderModule
	ssao           *gpu.ShaderModule
	ssaoBlur       *gpu.ShaderModule
	ssaoComposite  *gpu.ShaderModule
	dofCoC         *gpu.ShaderModule
	dofBokeh       *gpu.ShaderModule
}

var postShaders postShaderCache

func getPostShaders() (*postShaderCache, error) {
	postShaders.once.Do(func() {
		compile := func(label, src string) *gpu.ShaderModule {
			if postShaders.err != nil {
				return nil
			}
			m, err := gpu.CompileShader(gpu.ShaderDesc{Label: label, Source: src})
			if err != nil {
				postShaders.err = fmt.Errorf("render3d: compile %s shader: %w", label, err)
				return nil
			}
			return m
		}
		postShaders.blit = compile("render3d.post.blit", postBlitWGSL)
		postShaders.bloomExtract = compile("render3d.post.bloom.extract", postBloomExtractWGSL)
		postShaders.bloomBlur = compile("render3d.post.bloom.blur", postBloomBlurWGSL)
		postShaders.bloomComposite = compile("render3d.post.bloom.composite", postBloomCompositeWGSL)
		postShaders.ssao = compile("render3d.post.ssao", postSSAOWGSL)
		postShaders.ssaoBlur = compile("render3d.post.ssao.blur", postSSAOBlurWGSL)
		postShaders.ssaoComposite = compile("render3d.post.ssao.composite", postSSAOCompositeWGSL)
		postShaders.dofCoC = compile("render3d.post.dof.coc", postDoFCoCWGSL)
		postShaders.dofBokeh = compile("render3d.post.dof.bokeh", postDoFBokehWGSL)
	})
	if postShaders.err != nil {
		return nil, postShaders.err
	}
	return &postShaders, nil
}

func postFullscreenVertexLayout() gpu.VertexBufferLayout {
	return gpu.VertexBufferLayout{
		Stride: 4 * 4,
		Attributes: []gpu.VertexAttribute{
			{Location: 0, Offset: 0, Format: gpu.VertexFloat32x2},
			{Location: 1, Offset: 2 * 4, Format: gpu.VertexFloat32x2},
		},
	}
}

func createPostFullscreenVB(cx *core.Scope, dev *gpu.Device, retained bool) (*gpu.Buffer, error) {
	verts := [12]float32{
		-1, -1, 0, 1,
		3, -1, 2, 1,
		-1, 3, 0, -1,
	}
	var data [48]byte
	for i, f := range verts {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(f))
	}
	return dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    "render3d.post.fullscreen",
		Size:     uint64(len(data)),
		Usage:    gpu.UsageVertex,
		Data:     data[:],
		Retained: retained,
	})
}

func createPostPipeline(
	cx *core.Scope,
	dev *gpu.Device,
	label string,
	mod *gpu.ShaderModule,
	fragEntry string,
	layout *gpu.BindGroupLayout,
	targetFormat gpu.TextureFormat,
) (*gpu.Pipeline, error) {
	return dev.CreatePipeline(cx, gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_post"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: fragEntry},
		Layouts:       []*gpu.BindGroupLayout{layout},
		VertexBuffers: []gpu.VertexBufferLayout{postFullscreenVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
		Targets:       []gpu.ColorTargetState{{Format: targetFormat, WriteMask: gpu.ColorWriteAll}},
	})
}

func drawPostFullscreen(rec RenderPassRecorder, label string, dst *gpu.Texture, pipe *gpu.Pipeline, bg *gpu.BindGroup, vb *gpu.Buffer) {
	pass := rec.BeginRenderPass(gpu.RenderPassDesc{
		Label: label,
		ColorAttachments: []gpu.ColorAttachment{{
			Texture: dst,
			LoadOp:  gpu.LoadClear,
			StoreOp: gpu.StoreStore,
		}},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(0, bg)
	pass.SetVertexBuffer(0, vb, 0)
	pass.Draw(3, 0)
	pass.End()
}

func postPutFloat32(dst []byte, off int, v float32) {
	binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(v))
}

func postPutMat4(dst []byte, off int, m math3d.Mat4) {
	for i, f := range m {
		binary.LittleEndian.PutUint32(dst[off+i*4:], math.Float32bits(f))
	}
}

func postResolveColorFormat(f gpu.TextureFormat) (gpu.TextureFormat, error) {
	if f == gpu.FormatUndefined {
		return gpu.FormatRGBA16Float, nil
	}
	if f.IsDepthStencil() || f.IsCompressed() {
		return gpu.FormatUndefined, fmt.Errorf("%w: unsupported color target format %s", ErrInvalidPostProcess, f)
	}
	return f, nil
}

func postResolveOutputFormat(dev *gpu.Device, f gpu.TextureFormat) (gpu.TextureFormat, error) {
	if f == gpu.FormatUndefined {
		if dev != nil && dev.SurfaceFormat() != gpu.FormatUndefined {
			return dev.SurfaceFormat(), nil
		}
		return gpu.FormatRGBA8Unorm, nil
	}
	if f.IsDepthStencil() || f.IsCompressed() {
		return gpu.FormatUndefined, fmt.Errorf("%w: unsupported output format %s", ErrInvalidPostProcess, f)
	}
	return f, nil
}

func postResolveDepthFormat(f gpu.TextureFormat) (gpu.TextureFormat, error) {
	if f == gpu.FormatUndefined {
		return gpu.FormatDepth32Float, nil
	}
	if !f.IsDepth() {
		return gpu.FormatUndefined, fmt.Errorf("%w: depth format %s has no depth aspect", ErrInvalidPostProcess, f)
	}
	return f, nil
}

func postValidateSize(w, h uint32) error {
	if w == 0 || h == 0 {
		return fmt.Errorf("%w: dimensions %dx%d must be positive", ErrInvalidPostProcess, w, h)
	}
	return nil
}

// postDestFormat returns the format the final draw of a pass writes: OutputFormat for the swapchain (nil dst), otherwise dst's own format, which must be the pass's ColorFormat or OutputFormat because those are the only formats it has pipelines for.
func postDestFormat(dst *gpu.Texture, colorFmt, outFmt gpu.TextureFormat) (gpu.TextureFormat, error) {
	if dst == nil {
		return outFmt, nil
	}
	f := dst.Desc().Format
	if f != colorFmt && f != outFmt {
		return gpu.FormatUndefined, fmt.Errorf("%w: destination format %s is neither ColorFormat %s nor OutputFormat %s", ErrInvalidPostProcess, f, colorFmt, outFmt)
	}
	return f, nil
}

// postPickPipeline returns the pipeline built for dstFmt: inter targets colorFmt, out (nil when OutputFormat equals ColorFormat) targets OutputFormat.
func postPickPipeline(dstFmt, colorFmt gpu.TextureFormat, inter, out *gpu.Pipeline) *gpu.Pipeline {
	if dstFmt == colorFmt || out == nil {
		return inter
	}
	return out
}

// postCheckInputs rejects textures whose size differs from the pass size (texel steps and kernel radii are derived from it) and a destination that is also an input, which would read and write one texture in the same draw.
func postCheckInputs(w, h uint32, src, depth, dst *gpu.Texture) error {
	for _, in := range [...]struct {
		name string
		tex  *gpu.Texture
	}{{"source color", src}, {"depth", depth}, {"destination", dst}} {
		if in.tex == nil {
			continue
		}
		if d := in.tex.Desc(); d.Width != w || d.Height != h {
			return fmt.Errorf("%w: %s texture is %dx%d, pass is %dx%d", ErrInvalidPostProcess, in.name, d.Width, d.Height, w, h)
		}
	}
	if dst != nil && (dst == src || dst == depth) {
		return fmt.Errorf("%w: destination texture is also an input", ErrInvalidPostProcess)
	}
	return nil
}

func postCloseAll(closers ...interface{ Close() error }) error {
	var first error
	for _, c := range closers {
		if c == nil {
			continue
		}
		// Guard against typed nil pointers stored in the interface.
		switch v := c.(type) {
		case *gpu.Buffer:
			if v == nil {
				continue
			}
		case *gpu.Sampler:
			if v == nil {
				continue
			}
		case *gpu.RenderTarget:
			if v == nil {
				continue
			}
		case *gpu.BindGroupLayout:
			if v == nil {
				continue
			}
		case *gpu.BindGroup:
			if v == nil {
				continue
			}
		case *gpu.Pipeline:
			if v == nil {
				continue
			}
		case *BloomPass:
			if v == nil {
				continue
			}
		case *SSAOPass:
			if v == nil {
				continue
			}
		case *DepthOfFieldPass:
			if v == nil {
				continue
			}
		}
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// ============================================================================
// Bloom
// ============================================================================

// BloomConfig tunes the 3D bloom post-processing pass.
type BloomConfig struct {
	// Threshold is the minimum max-channel brightness that contributes to bloom (>= 0).
	// Values above 1.0 target HDR emissive pixels in floating-point color targets.
	Threshold float32
	// SoftKnee controls the smooth transition width around Threshold in [0, 1].
	// 0 produces a hard cutoff; 0.5 produces a gradual shoulder.
	SoftKnee float32
	// Intensity scales the blurred glow added onto the scene color (>= 0).
	// 0 leaves the scene unchanged.
	Intensity float32
	// Radius scales the separable Gaussian blur tap stride in texels (>= 0).
	Radius float32
	// BlurPasses is the number of horizontal+vertical separable Gaussian iterations in [1, 8].
	// 0 defaults to 2.
	BlurPasses int
	// HalfRes runs bright extraction and Gaussian blur at half resolution (max(1, w/2) x max(1, h/2))
	// for wider soft halos at a quarter of the pixel cost.
	HalfRes bool
}

// DefaultBloomConfig returns balanced defaults for 3D bloom.
func DefaultBloomConfig() BloomConfig {
	return BloomConfig{
		Threshold:  0.8,
		SoftKnee:   0.5,
		Intensity:  0.6,
		Radius:     1.5,
		BlurPasses: 2,
		HalfRes:    true,
	}
}

// Validate checks that every field of c is finite and within its valid range.
// Every error wraps [ErrInvalidPostProcess].
func (c BloomConfig) Validate() error {
	if !finite(c.Threshold) || c.Threshold < 0 {
		return fmt.Errorf("%w: bloom Threshold %v must be finite and >= 0", ErrInvalidPostProcess, c.Threshold)
	}
	if !finite(c.SoftKnee) || c.SoftKnee < 0 || c.SoftKnee > 1 {
		return fmt.Errorf("%w: bloom SoftKnee %v must be finite and in [0, 1]", ErrInvalidPostProcess, c.SoftKnee)
	}
	if !finite(c.Intensity) || c.Intensity < 0 {
		return fmt.Errorf("%w: bloom Intensity %v must be finite and >= 0", ErrInvalidPostProcess, c.Intensity)
	}
	if !finite(c.Radius) || c.Radius < 0 {
		return fmt.Errorf("%w: bloom Radius %v must be finite and >= 0", ErrInvalidPostProcess, c.Radius)
	}
	if c.BlurPasses < 0 || c.BlurPasses > postMaxBloomBlurPasses {
		return fmt.Errorf("%w: bloom BlurPasses %d must be in [0, %d]", ErrInvalidPostProcess, c.BlurPasses, postMaxBloomBlurPasses)
	}
	return nil
}

func (c BloomConfig) normalized() BloomConfig {
	if c.BlurPasses == 0 {
		c.BlurPasses = 2
	}
	return c
}

// BloomPassOptions configures [NewBloomPass].
type BloomPassOptions struct {
	Label        string
	Width        uint32
	Height       uint32
	ColorFormat  gpu.TextureFormat // internal blur target format; defaults to FormatRGBA16Float
	OutputFormat gpu.TextureFormat // destination format; defaults to dev.SurfaceFormat() or FormatRGBA8Unorm
	Retained     bool
	Config       BloomConfig
}

// BloomPass executes bright-pixel extraction, separable 9-tap Gaussian blur
// across offscreen ping-pong render targets, and additive composition onto a
// destination target or swapchain.
type BloomPass struct {
	cx           *core.Scope
	label        string
	width        uint32
	height       uint32
	blurW        uint32
	blurH        uint32
	colorFormat  gpu.TextureFormat
	outputFormat gpu.TextureFormat
	retained     bool
	cfg          BloomConfig

	vb           *gpu.Buffer
	linearSamp   *gpu.Sampler
	extractBuf   *gpu.Buffer
	horizBlurBuf *gpu.Buffer
	vertBlurBuf  *gpu.Buffer
	compBuf      *gpu.Buffer

	singleLayout *gpu.BindGroupLayout // uniform + 1 tex + 1 samp
	compLayout   *gpu.BindGroupLayout // uniform + 2 tex + 1 samp

	extractPipe *gpu.Pipeline
	blurPipe    *gpu.Pipeline
	compPipe    *gpu.Pipeline
	compOutPipe *gpu.Pipeline // nil when outputFormat == colorFormat

	pingRT *gpu.RenderTarget
	pongRT *gpu.RenderTarget

	horizBlurBG *gpu.BindGroup // reads pingRT
	vertBlurBG  *gpu.BindGroup // reads pongRT

	cachedExtractTex *gpu.Texture
	cachedExtractBG  *gpu.BindGroup
	cachedCompTex    *gpu.Texture
	cachedCompBG     *gpu.BindGroup

	closed bool
}

// NewBloomPass creates a scope-owned [BloomPass] on dev.
func NewBloomPass(cx *core.Scope, dev *gpu.Device, opts BloomPassOptions) (*BloomPass, error) {
	if err := postValidateSize(opts.Width, opts.Height); err != nil {
		return nil, err
	}
	if err := opts.Config.Validate(); err != nil {
		return nil, err
	}
	colorFmt, err := postResolveColorFormat(opts.ColorFormat)
	if err != nil {
		return nil, err
	}
	outFmt, err := postResolveOutputFormat(dev, opts.OutputFormat)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	shaders, err := getPostShaders()
	if err != nil {
		return nil, err
	}

	label := opts.Label
	if label == "" {
		label = "render3d.bloom"
	}

	p := &BloomPass{
		cx:           cx,
		label:        label,
		width:        opts.Width,
		height:       opts.Height,
		colorFormat:  colorFmt,
		outputFormat: outFmt,
		retained:     opts.Retained,
		cfg:          opts.Config.normalized(),
	}

	if p.vb, err = createPostFullscreenVB(cx, dev, opts.Retained); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.linearSamp, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + ".linear",
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterLinear,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     opts.Retained,
	}); err != nil {
		_ = p.Close()
		return nil, err
	}
	mkUniform := func(name string, size uint64) (*gpu.Buffer, error) {
		return dev.CreateBuffer(cx, gpu.BufferDesc{
			Label:    label + "." + name,
			Size:     size,
			Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
			Retained: opts.Retained,
		})
	}
	if p.extractBuf, err = mkUniform("extract.u", postBloomExtractUniformSize); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.horizBlurBuf, err = mkUniform("blur.h.u", postBloomBlurUniformSize); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.vertBlurBuf, err = mkUniform("blur.v.u", postBloomBlurUniformSize); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.compBuf, err = mkUniform("comp.u", postBloomCompositeUniformSize); err != nil {
		_ = p.Close()
		return nil, err
	}

	if err := p.buildPipelines(dev, shaders); err != nil {
		_ = p.Close()
		return nil, err
	}
	if err := p.allocTargets(dev); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

func (p *BloomPass) buildPipelines(dev *gpu.Device, shaders *postShaderCache) error {
	singleLayout, err := dev.CreateBindGroupLayoutDesc(p.cx, gpu.BindGroupLayoutDesc{
		Label: p.label + ".single",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		return err
	}
	compLayout, err := dev.CreateBindGroupLayoutDesc(p.cx, gpu.BindGroupLayoutDesc{
		Label: p.label + ".comp",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		_ = singleLayout.Close()
		return err
	}
	extractPipe, err := createPostPipeline(p.cx, dev, p.label+".extract", shaders.bloomExtract, "fs_bloom_extract", singleLayout, p.colorFormat)
	if err != nil {
		_ = postCloseAll(singleLayout, compLayout)
		return err
	}
	blurPipe, err := createPostPipeline(p.cx, dev, p.label+".blur", shaders.bloomBlur, "fs_bloom_blur", singleLayout, p.colorFormat)
	if err != nil {
		_ = postCloseAll(extractPipe, singleLayout, compLayout)
		return err
	}
	compPipe, err := createPostPipeline(p.cx, dev, p.label+".comp", shaders.bloomComposite, "fs_bloom_composite", compLayout, p.colorFormat)
	if err != nil {
		_ = postCloseAll(extractPipe, blurPipe, singleLayout, compLayout)
		return err
	}
	var compOutPipe *gpu.Pipeline
	if p.outputFormat != p.colorFormat {
		compOutPipe, err = createPostPipeline(p.cx, dev, p.label+".comp.out", shaders.bloomComposite, "fs_bloom_composite", compLayout, p.outputFormat)
		if err != nil {
			_ = postCloseAll(extractPipe, blurPipe, compPipe, singleLayout, compLayout)
			return err
		}
	}
	_ = postCloseAll(p.cachedExtractBG, p.cachedCompBG, p.horizBlurBG, p.vertBlurBG,
		p.extractPipe, p.blurPipe, p.compPipe, p.compOutPipe, p.singleLayout, p.compLayout)
	p.singleLayout = singleLayout
	p.compLayout = compLayout
	p.extractPipe = extractPipe
	p.blurPipe = blurPipe
	p.compPipe = compPipe
	p.compOutPipe = compOutPipe
	p.horizBlurBG = nil
	p.vertBlurBG = nil
	p.cachedExtractTex = nil
	p.cachedExtractBG = nil
	p.cachedCompTex = nil
	p.cachedCompBG = nil
	return nil
}

func (p *BloomPass) rebuildBlurBindGroups(dev *gpu.Device) error {
	hBG, err := dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
		Label:  p.label + ".blur.h",
		Layout: p.singleLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: p.horizBlurBuf, Size: postBloomBlurUniformSize},
			{Binding: 1, Texture: p.pingRT.Color},
			{Binding: 2, Sampler: p.linearSamp},
		},
	})
	if err != nil {
		return err
	}
	vBG, err := dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
		Label:  p.label + ".blur.v",
		Layout: p.singleLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: p.vertBlurBuf, Size: postBloomBlurUniformSize},
			{Binding: 1, Texture: p.pongRT.Color},
			{Binding: 2, Sampler: p.linearSamp},
		},
	})
	if err != nil {
		_ = hBG.Close()
		return err
	}
	_ = postCloseAll(p.horizBlurBG, p.vertBlurBG)
	p.horizBlurBG = hBG
	p.vertBlurBG = vBG
	return nil
}

func (p *BloomPass) allocTargets(dev *gpu.Device) error {
	bw, bh := p.width, p.height
	if p.cfg.HalfRes {
		bw = max(1, bw/2)
		bh = max(1, bh/2)
	}
	p.blurW, p.blurH = bw, bh

	ping, err := dev.CreateRenderTarget(p.cx, gpu.RenderTargetDesc{
		Label:       p.label + ".ping",
		Width:       bw,
		Height:      bh,
		ColorFormat: p.colorFormat,
		Retained:    p.retained,
	})
	if err != nil {
		return err
	}
	pong, err := dev.CreateRenderTarget(p.cx, gpu.RenderTargetDesc{
		Label:       p.label + ".pong",
		Width:       bw,
		Height:      bh,
		ColorFormat: p.colorFormat,
		Retained:    p.retained,
	})
	if err != nil {
		_ = ping.Close()
		return err
	}
	_ = postCloseAll(p.horizBlurBG, p.vertBlurBG, p.cachedCompBG, p.pingRT, p.pongRT)
	p.pingRT = ping
	p.pongRT = pong
	p.horizBlurBG = nil
	p.vertBlurBG = nil
	p.cachedCompTex = nil
	p.cachedCompBG = nil
	return p.rebuildBlurBindGroups(dev)
}

// Config returns the pass's current normalized [BloomConfig].
func (p *BloomPass) Config() BloomConfig {
	if p == nil {
		return BloomConfig{}
	}
	return p.cfg
}

// SetConfig validates and updates the bloom parameters. Changing [BloomConfig.HalfRes]
// takes effect on the next [BloomPass.Resize] or [BloomPass.Apply].
func (p *BloomPass) SetConfig(cfg BloomConfig) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: bloom pass is not valid", ErrInvalidPostProcess)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	p.cfg = cfg.normalized()
	return nil
}

// Width returns the configured scene width in pixels.
func (p *BloomPass) Width() uint32 {
	if p == nil {
		return 0
	}
	return p.width
}

// Height returns the configured scene height in pixels.
func (p *BloomPass) Height() uint32 {
	if p == nil {
		return 0
	}
	return p.height
}

// Valid reports whether the pass is open and its GPU resources are live.
func (p *BloomPass) Valid() bool {
	return p != nil && !p.closed &&
		p.vb.Valid() && p.pingRT.Valid() && p.pongRT.Valid() &&
		p.extractPipe.Valid() && p.blurPipe.Valid() && p.compPipe.Valid() &&
		(p.compOutPipe == nil || p.compOutPipe.Valid())
}

func (p *BloomPass) ensureLive(dev *gpu.Device) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: bloom pass is not valid", ErrInvalidPostProcess)
	}
	if p.Valid() && p.horizBlurBG.Valid() && p.vertBlurBG.Valid() {
		return nil
	}
	if !p.retained || !p.vb.Valid() || !p.pingRT.Valid() || !p.pongRT.Valid() {
		return fmt.Errorf("%w: bloom pass is not valid", ErrInvalidPostProcess)
	}
	shaders, err := getPostShaders()
	if err != nil {
		return err
	}
	if err := p.buildPipelines(dev, shaders); err != nil {
		return err
	}
	return p.rebuildBlurBindGroups(dev)
}

// Resize recreates the pass's offscreen blur render targets for a new scene size.
func (p *BloomPass) Resize(dev *gpu.Device, width, height uint32) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: bloom pass is not valid", ErrInvalidPostProcess)
	}
	if err := postValidateSize(width, height); err != nil {
		return err
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	wantBW, wantBH := width, height
	if p.cfg.HalfRes {
		wantBW = max(1, width/2)
		wantBH = max(1, height/2)
	}
	if p.width == width && p.height == height && p.blurW == wantBW && p.blurH == wantBH && p.Valid() {
		return nil
	}
	if !p.singleLayout.Valid() && p.retained {
		shaders, err := getPostShaders()
		if err != nil {
			return err
		}
		if err := p.buildPipelines(dev, shaders); err != nil {
			return err
		}
	}
	p.width, p.height = width, height
	return p.allocTargets(dev)
}

// Apply runs bright extraction, separable blur, and additive composite from srcColor into dstColor. A nil dstColor writes to the swapchain in OutputFormat, and a texture dstColor must be in ColorFormat or OutputFormat. Every texture must have the pass size and dstColor cannot also be an input; otherwise Apply returns an error wrapping [ErrInvalidPostProcess].
func (p *BloomPass) Apply(dev *gpu.Device, rec RenderPassRecorder, srcColor, dstColor *gpu.Texture) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: bloom pass is not valid", ErrInvalidPostProcess)
	}
	dstFmt, err := postDestFormat(dstColor, p.colorFormat, p.outputFormat)
	if err != nil {
		return err
	}
	return p.applyTo(dev, rec, srcColor, dstColor, dstFmt)
}

func (p *BloomPass) applyTo(dev *gpu.Device, rec RenderPassRecorder, srcColor, dstColor *gpu.Texture, dstFmt gpu.TextureFormat) error {
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if err := p.ensureLive(dev); err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("%w: nil render pass recorder", ErrInvalidPostProcess)
	}
	if srcColor == nil || !srcColor.Valid() {
		return fmt.Errorf("%w: invalid source color texture", ErrInvalidPostProcess)
	}
	if dstColor != nil && !dstColor.Valid() {
		return fmt.Errorf("%w: invalid destination texture", ErrInvalidPostProcess)
	}
	if err := postCheckInputs(p.width, p.height, srcColor, nil, dstColor); err != nil {
		return err
	}
	// Ensure half-res/full-res internal targets match current config.
	wantBW, wantBH := p.width, p.height
	if p.cfg.HalfRes {
		wantBW = max(1, p.width/2)
		wantBH = max(1, p.height/2)
	}
	if p.blurW != wantBW || p.blurH != wantBH {
		if err := p.allocTargets(dev); err != nil {
			return err
		}
	}

	// Upload uniforms (each pass reads its own dedicated uniform buffer).
	var extractData [postBloomExtractUniformSize]byte
	postPutFloat32(extractData[:], 0, p.cfg.Threshold)
	postPutFloat32(extractData[:], 4, p.cfg.SoftKnee)
	dev.WriteBuffer(p.extractBuf, 0, extractData[:])

	stepX := p.cfg.Radius / float32(p.blurW)
	stepY := p.cfg.Radius / float32(p.blurH)
	var hData [postBloomBlurUniformSize]byte
	postPutFloat32(hData[:], 0, stepX)
	postPutFloat32(hData[:], 4, 0)
	dev.WriteBuffer(p.horizBlurBuf, 0, hData[:])

	var vData [postBloomBlurUniformSize]byte
	postPutFloat32(vData[:], 0, 0)
	postPutFloat32(vData[:], 4, stepY)
	dev.WriteBuffer(p.vertBlurBuf, 0, vData[:])

	var compData [postBloomCompositeUniformSize]byte
	postPutFloat32(compData[:], 0, p.cfg.Intensity)
	dev.WriteBuffer(p.compBuf, 0, compData[:])

	if p.cachedExtractTex != srcColor || !p.cachedExtractBG.Valid() {
		bg, err := dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
			Label:  p.label + ".extract.bg",
			Layout: p.singleLayout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Buffer: p.extractBuf, Size: postBloomExtractUniformSize},
				{Binding: 1, Texture: srcColor},
				{Binding: 2, Sampler: p.linearSamp},
			},
		})
		if err != nil {
			return err
		}
		_ = postCloseAll(p.cachedExtractBG)
		p.cachedExtractTex = srcColor
		p.cachedExtractBG = bg
	}
	if p.cachedCompTex != srcColor || !p.cachedCompBG.Valid() {
		bg, err := dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
			Label:  p.label + ".comp.bg",
			Layout: p.compLayout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Buffer: p.compBuf, Size: postBloomCompositeUniformSize},
				{Binding: 1, Texture: srcColor},
				{Binding: 2, Texture: p.pingRT.Color},
				{Binding: 3, Sampler: p.linearSamp},
			},
		})
		if err != nil {
			return err
		}
		_ = postCloseAll(p.cachedCompBG)
		p.cachedCompTex = srcColor
		p.cachedCompBG = bg
	}

	// 1. Bright extract -> pingRT.
	drawPostFullscreen(rec, p.label+".extract", p.pingRT.Color, p.extractPipe, p.cachedExtractBG, p.vb)

	// 2. Separable Gaussian blur iterations: pingRT -> pongRT -> pingRT.
	if p.cfg.Intensity > 0 && p.cfg.Radius > 0 {
		for range p.cfg.BlurPasses {
			drawPostFullscreen(rec, p.label+".blur.h", p.pongRT.Color, p.blurPipe, p.horizBlurBG, p.vb)
			drawPostFullscreen(rec, p.label+".blur.v", p.pingRT.Color, p.blurPipe, p.vertBlurBG, p.vb)
		}
	}

	// 3. Additive composite -> dstColor (or swapchain).
	pipe := postPickPipeline(dstFmt, p.colorFormat, p.compPipe, p.compOutPipe)
	drawPostFullscreen(rec, p.label+".composite", dstColor, pipe, p.cachedCompBG, p.vb)
	return nil
}

// Close releases all GPU resources owned by p. It is safe to call on a nil receiver or more than once.
func (p *BloomPass) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	p.cachedExtractTex = nil
	p.cachedCompTex = nil
	return postCloseAll(
		p.cachedExtractBG, p.cachedCompBG, p.horizBlurBG, p.vertBlurBG,
		p.pingRT, p.pongRT,
		p.extractPipe, p.blurPipe, p.compPipe, p.compOutPipe,
		p.singleLayout, p.compLayout,
		p.extractBuf, p.horizBlurBuf, p.vertBlurBuf, p.compBuf,
		p.linearSamp, p.vb,
	)
}

// ============================================================================
// SSAO
// ============================================================================

// SSAOConfig tunes screen-space ambient occlusion from a 3D depth buffer.
type SSAOConfig struct {
	// Radius is the view-space sampling hemisphere radius in world units (> 0). Default: 0.6.
	Radius float32
	// Bias offsets depth comparisons in view-space units to suppress self-occlusion acne (>= 0). Default: 0.025.
	Bias float32
	// Intensity scales the occlusion darkening strength in [0, 4]. 0 leaves the scene unchanged. Default: 1.2.
	Intensity float32
	// Power raises ambient visibility (ao^Power) to tune crevice contrast (> 0). Default: 1.5.
	Power float32
	// SampleCount is the number of deterministic hemisphere kernel samples in [4, 16]. 0 defaults to 12.
	SampleCount int
	// BlurRadius is the depth-aware bilateral blur half-width in texels in [0, 4]. 0 disables blur.
	BlurRadius int
	// DepthThreshold is the view-space depth tolerance for bilateral blur edge preservation (> 0). Default: 0.2.
	DepthThreshold float32
	// Near and Far are fallback perspective clip planes used only when ProjMatrix is zero.
	Near float32
	Far  float32
	// ProjMatrix is the camera's [0, 1] NDC depth projection matrix (e.g. cam.ProjMatrix()).
	ProjMatrix math3d.Mat4
	// InvProjMatrix optionally supplies ProjMatrix.Inverse(); when zero, it is computed from ProjMatrix.
	InvProjMatrix math3d.Mat4
}

// DefaultSSAOConfig returns balanced defaults for 3D SSAO.
func DefaultSSAOConfig() SSAOConfig {
	return SSAOConfig{
		Radius:         0.6,
		Bias:           0.025,
		Intensity:      1.2,
		Power:          1.5,
		SampleCount:    12,
		BlurRadius:     2,
		DepthThreshold: 0.2,
		Near:           0.1,
		Far:            100.0,
	}
}

// SSAOConfigFromCamera initializes an [SSAOConfig] using cam's projection matrix
// and clip planes combined with [DefaultSSAOConfig] quality settings.
func SSAOConfigFromCamera(cam *scene3d.Camera) SSAOConfig {
	cfg := DefaultSSAOConfig()
	if cam != nil {
		cfg.ProjMatrix = cam.ProjMatrix()
		cfg.Near = cam.Projection.Near
		cfg.Far = cam.Projection.Far
	}
	return cfg
}

// Validate checks that every field of c is finite and within its valid range.
// Every error wraps [ErrInvalidPostProcess].
func (c SSAOConfig) Validate() error {
	if !finite(c.Radius) || c.Radius <= 0 {
		return fmt.Errorf("%w: ssao Radius %v must be finite and > 0", ErrInvalidPostProcess, c.Radius)
	}
	if !finite(c.Bias) || c.Bias < 0 {
		return fmt.Errorf("%w: ssao Bias %v must be finite and >= 0", ErrInvalidPostProcess, c.Bias)
	}
	if !finite(c.Intensity) || c.Intensity < 0 || c.Intensity > 4 {
		return fmt.Errorf("%w: ssao Intensity %v must be finite and in [0, 4]", ErrInvalidPostProcess, c.Intensity)
	}
	if !finite(c.Power) || c.Power <= 0 {
		return fmt.Errorf("%w: ssao Power %v must be finite and > 0", ErrInvalidPostProcess, c.Power)
	}
	if c.SampleCount != 0 && (c.SampleCount < postMinSSAOSamples || c.SampleCount > postMaxSSAOSamples) {
		return fmt.Errorf("%w: ssao SampleCount %d must be 0 or in [%d, %d]", ErrInvalidPostProcess, c.SampleCount, postMinSSAOSamples, postMaxSSAOSamples)
	}
	if c.BlurRadius < 0 || c.BlurRadius > postMaxSSAOBlurRadius {
		return fmt.Errorf("%w: ssao BlurRadius %d must be in [0, %d]", ErrInvalidPostProcess, c.BlurRadius, postMaxSSAOBlurRadius)
	}
	if !finite(c.DepthThreshold) || c.DepthThreshold < 0 {
		return fmt.Errorf("%w: ssao DepthThreshold %v must be finite and >= 0", ErrInvalidPostProcess, c.DepthThreshold)
	}
	if c.ProjMatrix != (math3d.Mat4{}) {
		for _, f := range c.ProjMatrix {
			if !finite(f) {
				return fmt.Errorf("%w: ssao ProjMatrix contains non-finite value", ErrInvalidPostProcess)
			}
		}
		if c.InvProjMatrix == (math3d.Mat4{}) {
			if _, ok := c.ProjMatrix.Inverse(); !ok {
				return fmt.Errorf("%w: ssao ProjMatrix is singular", ErrInvalidPostProcess)
			}
		}
	} else {
		if !finite(c.Near) || !finite(c.Far) || c.Near <= 0 || c.Far <= c.Near {
			return fmt.Errorf("%w: ssao requires invertible ProjMatrix or 0 < Near < Far (got Near=%v, Far=%v)", ErrInvalidPostProcess, c.Near, c.Far)
		}
	}
	if c.InvProjMatrix != (math3d.Mat4{}) {
		for _, f := range c.InvProjMatrix {
			if !finite(f) {
				return fmt.Errorf("%w: ssao InvProjMatrix contains non-finite value", ErrInvalidPostProcess)
			}
		}
	}
	return nil
}

func (c SSAOConfig) normalized() SSAOConfig {
	if c.SampleCount == 0 {
		c.SampleCount = 12
	}
	if c.DepthThreshold == 0 {
		c.DepthThreshold = 0.2
	}
	return c
}

func (c SSAOConfig) resolveMatrices(w, h uint32) (proj, invProj math3d.Mat4) {
	if c.ProjMatrix != (math3d.Mat4{}) {
		proj = c.ProjMatrix
	} else {
		aspect := float32(w) / float32(max(h, 1))
		proj = math3d.Perspective(math3d.DegToRad(60), aspect, c.Near, c.Far)
	}
	if c.InvProjMatrix != (math3d.Mat4{}) {
		invProj = c.InvProjMatrix
	} else if inv, ok := proj.Inverse(); ok {
		invProj = inv
	} else {
		invProj = math3d.Mat4Identity()
	}
	return proj, invProj
}

// buildSSAOKernel generates a deterministic low-discrepancy hemisphere sample kernel
// in tangent space (+Z along surface normal) with samples concentrated closer to the origin.
func buildSSAOKernel(count int) [postMaxSSAOSamples][4]float32 {
	var out [postMaxSSAOSamples][4]float32
	if count < 1 {
		count = 1
	}
	if count > postMaxSSAOSamples {
		count = postMaxSSAOSamples
	}
	const goldenAngle = 2.39996322972865332 // pi * (3 - sqrt(5))
	for i := range count {
		t := (float64(i) + 0.5) / float64(count)
		// Cosine-weighted elevation in [0.15, 0.98] avoids grazing-angle self-occlusion.
		z := 0.15 + 0.83*t
		rXY := math.Sqrt(max(0, 1.0-z*z))
		theta := float64(i) * goldenAngle
		x := math.Cos(theta) * rXY
		y := math.Sin(theta) * rXY
		// Accelerating radial distribution puts more taps close to contact crevices.
		frac := float64(i) / float64(count)
		scale := 0.12 + 0.88*(frac*frac)
		out[i] = [4]float32{
			float32(x * scale),
			float32(y * scale),
			float32(z * scale),
			0,
		}
	}
	return out
}

// SSAOPassOptions configures [NewSSAOPass].
type SSAOPassOptions struct {
	Label        string
	Width        uint32
	Height       uint32
	ColorFormat  gpu.TextureFormat // intermediate color format; defaults to FormatRGBA16Float
	OutputFormat gpu.TextureFormat // destination format; defaults to dev.SurfaceFormat() or FormatRGBA8Unorm
	Retained     bool
	Config       SSAOConfig
}

// SSAOPass computes screen-space ambient occlusion from a 3D depth texture,
// optionally filters it with a depth-aware bilateral blur, and multiplies the
// resulting occlusion factor into a scene color texture.
type SSAOPass struct {
	cx           *core.Scope
	label        string
	width        uint32
	height       uint32
	colorFormat  gpu.TextureFormat
	outputFormat gpu.TextureFormat
	retained     bool
	cfg          SSAOConfig

	vb          *gpu.Buffer
	nearestSamp *gpu.Sampler
	linearSamp  *gpu.Sampler
	ssaoBuf     *gpu.Buffer
	blurBuf     *gpu.Buffer

	ssaoLayout *gpu.BindGroupLayout
	blurLayout *gpu.BindGroupLayout
	compLayout *gpu.BindGroupLayout

	ssaoPipe    *gpu.Pipeline
	blurPipe    *gpu.Pipeline
	compPipe    *gpu.Pipeline
	compOutPipe *gpu.Pipeline

	aoRawRT  *gpu.RenderTarget
	aoBlurRT *gpu.RenderTarget

	cachedSSAODepth *gpu.Texture
	cachedSSAOBG    *gpu.BindGroup
	cachedBlurDepth *gpu.Texture
	cachedBlurBG    *gpu.BindGroup
	cachedCompSrc   *gpu.Texture
	cachedCompAO    *gpu.Texture
	cachedCompBG    *gpu.BindGroup

	lastAO *gpu.Texture
	closed bool
}

// NewSSAOPass creates a scope-owned [SSAOPass] on dev.
func NewSSAOPass(cx *core.Scope, dev *gpu.Device, opts SSAOPassOptions) (*SSAOPass, error) {
	if err := postValidateSize(opts.Width, opts.Height); err != nil {
		return nil, err
	}
	if err := opts.Config.Validate(); err != nil {
		return nil, err
	}
	colorFmt, err := postResolveColorFormat(opts.ColorFormat)
	if err != nil {
		return nil, err
	}
	outFmt, err := postResolveOutputFormat(dev, opts.OutputFormat)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	shaders, err := getPostShaders()
	if err != nil {
		return nil, err
	}

	label := opts.Label
	if label == "" {
		label = "render3d.ssao"
	}

	p := &SSAOPass{
		cx:           cx,
		label:        label,
		width:        opts.Width,
		height:       opts.Height,
		colorFormat:  colorFmt,
		outputFormat: outFmt,
		retained:     opts.Retained,
		cfg:          opts.Config.normalized(),
	}

	if p.vb, err = createPostFullscreenVB(cx, dev, opts.Retained); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.nearestSamp, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + ".nearest",
		MinFilter:    gpu.FilterNearest,
		MagFilter:    gpu.FilterNearest,
		MipmapFilter: gpu.FilterNearest,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     opts.Retained,
	}); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.linearSamp, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + ".linear",
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterLinear,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     opts.Retained,
	}); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.ssaoBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".u",
		Size:     postSSAOUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: opts.Retained,
	}); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.blurBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".blur.u",
		Size:     postSSAOBlurUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: opts.Retained,
	}); err != nil {
		_ = p.Close()
		return nil, err
	}

	if err := p.buildPipelines(dev, shaders); err != nil {
		_ = p.Close()
		return nil, err
	}
	if err := p.allocTargets(dev); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

func (p *SSAOPass) buildPipelines(dev *gpu.Device, shaders *postShaderCache) error {
	ssaoLayout, err := dev.CreateBindGroupLayoutDesc(p.cx, gpu.BindGroupLayoutDesc{
		Label: p.label + ".layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		return err
	}
	blurLayout, err := dev.CreateBindGroupLayoutDesc(p.cx, gpu.BindGroupLayoutDesc{
		Label: p.label + ".blur.layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		_ = ssaoLayout.Close()
		return err
	}
	compLayout, err := dev.CreateBindGroupLayoutDesc(p.cx, gpu.BindGroupLayoutDesc{
		Label: p.label + ".comp.layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		_ = postCloseAll(ssaoLayout, blurLayout)
		return err
	}
	ssaoPipe, err := createPostPipeline(p.cx, dev, p.label+".pipe", shaders.ssao, "fs_ssao", ssaoLayout, gpu.FormatRGBA8Unorm)
	if err != nil {
		_ = postCloseAll(ssaoLayout, blurLayout, compLayout)
		return err
	}
	blurPipe, err := createPostPipeline(p.cx, dev, p.label+".blur.pipe", shaders.ssaoBlur, "fs_ssao_blur", blurLayout, gpu.FormatRGBA8Unorm)
	if err != nil {
		_ = postCloseAll(ssaoPipe, ssaoLayout, blurLayout, compLayout)
		return err
	}
	compPipe, err := createPostPipeline(p.cx, dev, p.label+".comp.pipe", shaders.ssaoComposite, "fs_ssao_composite", compLayout, p.colorFormat)
	if err != nil {
		_ = postCloseAll(ssaoPipe, blurPipe, ssaoLayout, blurLayout, compLayout)
		return err
	}
	var compOutPipe *gpu.Pipeline
	if p.outputFormat != p.colorFormat {
		compOutPipe, err = createPostPipeline(p.cx, dev, p.label+".comp.out", shaders.ssaoComposite, "fs_ssao_composite", compLayout, p.outputFormat)
		if err != nil {
			_ = postCloseAll(ssaoPipe, blurPipe, compPipe, ssaoLayout, blurLayout, compLayout)
			return err
		}
	}
	_ = postCloseAll(p.cachedSSAOBG, p.cachedBlurBG, p.cachedCompBG,
		p.ssaoPipe, p.blurPipe, p.compPipe, p.compOutPipe,
		p.ssaoLayout, p.blurLayout, p.compLayout)
	p.ssaoLayout = ssaoLayout
	p.blurLayout = blurLayout
	p.compLayout = compLayout
	p.ssaoPipe = ssaoPipe
	p.blurPipe = blurPipe
	p.compPipe = compPipe
	p.compOutPipe = compOutPipe
	p.cachedSSAODepth = nil
	p.cachedSSAOBG = nil
	p.cachedBlurDepth = nil
	p.cachedBlurBG = nil
	p.cachedCompSrc = nil
	p.cachedCompAO = nil
	p.cachedCompBG = nil
	return nil
}

func (p *SSAOPass) allocTargets(dev *gpu.Device) error {
	rawRT, err := dev.CreateRenderTarget(p.cx, gpu.RenderTargetDesc{
		Label:       p.label + ".raw",
		Width:       p.width,
		Height:      p.height,
		ColorFormat: gpu.FormatRGBA8Unorm,
		Retained:    p.retained,
	})
	if err != nil {
		return err
	}
	blurRT, err := dev.CreateRenderTarget(p.cx, gpu.RenderTargetDesc{
		Label:       p.label + ".blur",
		Width:       p.width,
		Height:      p.height,
		ColorFormat: gpu.FormatRGBA8Unorm,
		Retained:    p.retained,
	})
	if err != nil {
		_ = rawRT.Close()
		return err
	}
	_ = postCloseAll(p.cachedBlurBG, p.cachedCompBG, p.aoRawRT, p.aoBlurRT)
	p.aoRawRT = rawRT
	p.aoBlurRT = blurRT
	p.cachedBlurDepth = nil
	p.cachedBlurBG = nil
	p.cachedCompSrc = nil
	p.cachedCompAO = nil
	p.cachedCompBG = nil
	p.lastAO = rawRT.Color
	return nil
}

// Config returns the pass's current normalized [SSAOConfig].
func (p *SSAOPass) Config() SSAOConfig {
	if p == nil {
		return SSAOConfig{}
	}
	return p.cfg
}

// SetConfig validates and updates the SSAO parameters.
func (p *SSAOPass) SetConfig(cfg SSAOConfig) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: ssao pass is not valid", ErrInvalidPostProcess)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	p.cfg = cfg.normalized()
	return nil
}

// Width returns the configured target width in pixels.
func (p *SSAOPass) Width() uint32 {
	if p == nil {
		return 0
	}
	return p.width
}

// Height returns the configured target height in pixels.
func (p *SSAOPass) Height() uint32 {
	if p == nil {
		return 0
	}
	return p.height
}

// AOTexture returns the most recently computed ambient occlusion texture
// (blurred when BlurRadius > 0, raw otherwise).
func (p *SSAOPass) AOTexture() *gpu.Texture {
	if p == nil || p.closed {
		return nil
	}
	return p.lastAO
}

// Valid reports whether the pass is open and its GPU resources are live.
func (p *SSAOPass) Valid() bool {
	return p != nil && !p.closed &&
		p.vb.Valid() && p.aoRawRT.Valid() && p.aoBlurRT.Valid() &&
		p.ssaoPipe.Valid() && p.blurPipe.Valid() && p.compPipe.Valid() &&
		(p.compOutPipe == nil || p.compOutPipe.Valid())
}

func (p *SSAOPass) ensureLive(dev *gpu.Device) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: ssao pass is not valid", ErrInvalidPostProcess)
	}
	if p.Valid() {
		return nil
	}
	if !p.retained || !p.vb.Valid() || !p.aoRawRT.Valid() || !p.aoBlurRT.Valid() {
		return fmt.Errorf("%w: ssao pass is not valid", ErrInvalidPostProcess)
	}
	shaders, err := getPostShaders()
	if err != nil {
		return err
	}
	return p.buildPipelines(dev, shaders)
}

// Resize recreates the pass's offscreen AO targets for a new viewport size.
func (p *SSAOPass) Resize(dev *gpu.Device, width, height uint32) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: ssao pass is not valid", ErrInvalidPostProcess)
	}
	if err := postValidateSize(width, height); err != nil {
		return err
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if p.width == width && p.height == height && p.aoRawRT.Valid() && p.aoBlurRT.Valid() {
		return nil
	}
	p.width, p.height = width, height
	return p.allocTargets(dev)
}

// ComputeAO evaluates the SSAO hemisphere pass (and optional bilateral blur) from
// depthTex and returns the resulting AO texture for inspection or custom compositing.
func (p *SSAOPass) ComputeAO(dev *gpu.Device, rec RenderPassRecorder, depthTex *gpu.Texture) (*gpu.Texture, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if err := p.ensureLive(dev); err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("%w: nil render pass recorder", ErrInvalidPostProcess)
	}
	if depthTex == nil || !depthTex.Valid() {
		return nil, fmt.Errorf("%w: invalid depth texture", ErrInvalidPostProcess)
	}
	if err := postCheckInputs(p.width, p.height, nil, depthTex, nil); err != nil {
		return nil, err
	}

	proj, invProj := p.cfg.resolveMatrices(p.width, p.height)
	kernel := buildSSAOKernel(p.cfg.SampleCount)

	var uData [postSSAOUniformSize]byte
	postPutMat4(uData[:], 0, proj)
	postPutMat4(uData[:], 64, invProj)
	postPutFloat32(uData[:], 128, p.cfg.Radius)
	postPutFloat32(uData[:], 132, p.cfg.Bias)
	postPutFloat32(uData[:], 136, p.cfg.Intensity)
	postPutFloat32(uData[:], 140, p.cfg.Power)
	postPutFloat32(uData[:], 144, 1.0/float32(p.width))
	postPutFloat32(uData[:], 148, 1.0/float32(p.height))
	postPutFloat32(uData[:], 152, float32(p.cfg.SampleCount))
	postPutFloat32(uData[:], 156, 0)
	for i := range postMaxSSAOSamples {
		base := 160 + i*16
		postPutFloat32(uData[:], base+0, kernel[i][0])
		postPutFloat32(uData[:], base+4, kernel[i][1])
		postPutFloat32(uData[:], base+8, kernel[i][2])
		postPutFloat32(uData[:], base+12, kernel[i][3])
	}
	dev.WriteBuffer(p.ssaoBuf, 0, uData[:])

	if p.cachedSSAODepth != depthTex || !p.cachedSSAOBG.Valid() {
		bg, err := dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
			Label:  p.label + ".bg",
			Layout: p.ssaoLayout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Buffer: p.ssaoBuf, Size: postSSAOUniformSize},
				{Binding: 1, Texture: depthTex},
				{Binding: 2, Sampler: p.nearestSamp},
			},
		})
		if err != nil {
			return nil, err
		}
		_ = postCloseAll(p.cachedSSAOBG)
		p.cachedSSAODepth = depthTex
		p.cachedSSAOBG = bg
	}

	drawPostFullscreen(rec, p.label+".ao", p.aoRawRT.Color, p.ssaoPipe, p.cachedSSAOBG, p.vb)
	aoTex := p.aoRawRT.Color

	if p.cfg.BlurRadius > 0 && p.cfg.Intensity > 0 {
		var bData [postSSAOBlurUniformSize]byte
		postPutMat4(bData[:], 0, invProj)
		postPutFloat32(bData[:], 64, 1.0/float32(p.width))
		postPutFloat32(bData[:], 68, 1.0/float32(p.height))
		postPutFloat32(bData[:], 72, float32(p.cfg.BlurRadius))
		postPutFloat32(bData[:], 76, p.cfg.DepthThreshold)
		dev.WriteBuffer(p.blurBuf, 0, bData[:])

		if p.cachedBlurDepth != depthTex || !p.cachedBlurBG.Valid() {
			bg, err := dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
				Label:  p.label + ".blur.bg",
				Layout: p.blurLayout,
				Entries: []gpu.BindGroupEntry{
					{Binding: 0, Buffer: p.blurBuf, Size: postSSAOBlurUniformSize},
					{Binding: 1, Texture: p.aoRawRT.Color},
					{Binding: 2, Texture: depthTex},
					{Binding: 3, Sampler: p.nearestSamp},
				},
			})
			if err != nil {
				return nil, err
			}
			_ = postCloseAll(p.cachedBlurBG)
			p.cachedBlurDepth = depthTex
			p.cachedBlurBG = bg
		}
		drawPostFullscreen(rec, p.label+".blur", p.aoBlurRT.Color, p.blurPipe, p.cachedBlurBG, p.vb)
		aoTex = p.aoBlurRT.Color
	}

	p.lastAO = aoTex
	return aoTex, nil
}

// Apply computes SSAO from depthTex and multiplies it onto srcColor, writing to dstColor. A nil dstColor writes to the swapchain in OutputFormat, and a texture dstColor must be in ColorFormat or OutputFormat. Every texture must have the pass size and dstColor cannot also be an input; otherwise Apply returns an error wrapping [ErrInvalidPostProcess].
func (p *SSAOPass) Apply(dev *gpu.Device, rec RenderPassRecorder, srcColor, depthTex, dstColor *gpu.Texture) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: ssao pass is not valid", ErrInvalidPostProcess)
	}
	dstFmt, err := postDestFormat(dstColor, p.colorFormat, p.outputFormat)
	if err != nil {
		return err
	}
	return p.applyTo(dev, rec, srcColor, depthTex, dstColor, dstFmt)
}

func (p *SSAOPass) applyTo(dev *gpu.Device, rec RenderPassRecorder, srcColor, depthTex, dstColor *gpu.Texture, dstFmt gpu.TextureFormat) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: ssao pass is not valid", ErrInvalidPostProcess)
	}
	if srcColor == nil || !srcColor.Valid() {
		return fmt.Errorf("%w: invalid source color texture", ErrInvalidPostProcess)
	}
	if dstColor != nil && !dstColor.Valid() {
		return fmt.Errorf("%w: invalid destination texture", ErrInvalidPostProcess)
	}
	if err := postCheckInputs(p.width, p.height, srcColor, depthTex, dstColor); err != nil {
		return err
	}
	aoTex, err := p.ComputeAO(dev, rec, depthTex)
	if err != nil {
		return err
	}
	if p.cachedCompSrc != srcColor || p.cachedCompAO != aoTex || !p.cachedCompBG.Valid() {
		bg, err := dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
			Label:  p.label + ".comp.bg",
			Layout: p.compLayout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Texture: srcColor},
				{Binding: 1, Texture: aoTex},
				{Binding: 2, Sampler: p.linearSamp},
			},
		})
		if err != nil {
			return err
		}
		_ = postCloseAll(p.cachedCompBG)
		p.cachedCompSrc = srcColor
		p.cachedCompAO = aoTex
		p.cachedCompBG = bg
	}
	pipe := postPickPipeline(dstFmt, p.colorFormat, p.compPipe, p.compOutPipe)
	drawPostFullscreen(rec, p.label+".composite", dstColor, pipe, p.cachedCompBG, p.vb)
	return nil
}

// Close releases all GPU resources owned by p. It is safe to call on a nil receiver or more than once.
func (p *SSAOPass) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	p.cachedSSAODepth = nil
	p.cachedBlurDepth = nil
	p.cachedCompSrc = nil
	p.cachedCompAO = nil
	p.lastAO = nil
	return postCloseAll(
		p.cachedSSAOBG, p.cachedBlurBG, p.cachedCompBG,
		p.aoRawRT, p.aoBlurRT,
		p.ssaoPipe, p.blurPipe, p.compPipe, p.compOutPipe,
		p.ssaoLayout, p.blurLayout, p.compLayout,
		p.ssaoBuf, p.blurBuf,
		p.nearestSamp, p.linearSamp, p.vb,
	)
}

// ============================================================================
// Depth of Field
// ============================================================================

// DepthOfFieldConfig tunes depth-aware disc bokeh Depth of Field.
type DepthOfFieldConfig struct {
	// FocusDistance is the view-space distance from the camera that renders sharpest (> 0). Default: 5.0.
	FocusDistance float32
	// FocusRange is the half-width around FocusDistance over which the Circle of Confusion
	// ramps from 0 (in focus) to 1 (maximum blur). Must be > 0. Default: 2.0.
	FocusRange float32
	// MaxBlurRadius is the maximum bokeh disc radius in pixels at CoC = 1 (>= 0).
	// 0 leaves the scene unchanged. Default: 6.0.
	MaxBlurRadius float32
	// Near and Far are the camera clip planes used to linearize [0, 1] depth values (0 < Near < Far).
	Near float32
	Far  float32
	// Orthographic selects linear orthographic depth reconstruction instead of perspective.
	Orthographic bool
}

// DefaultDepthOfFieldConfig returns balanced defaults for 3D Depth of Field.
func DefaultDepthOfFieldConfig() DepthOfFieldConfig {
	return DepthOfFieldConfig{
		FocusDistance: 5.0,
		FocusRange:    2.0,
		MaxBlurRadius: 6.0,
		Near:          0.1,
		Far:           100.0,
	}
}

// DepthOfFieldConfigFromCamera initializes a [DepthOfFieldConfig] with cam's clip
// planes and projection kind.
func DepthOfFieldConfigFromCamera(cam *scene3d.Camera, focusDistance, focusRange, maxBlurRadius float32) DepthOfFieldConfig {
	cfg := DefaultDepthOfFieldConfig()
	cfg.FocusDistance = focusDistance
	cfg.FocusRange = focusRange
	cfg.MaxBlurRadius = maxBlurRadius
	if cam != nil {
		if cam.Projection.Near > 0 {
			cfg.Near = cam.Projection.Near
		}
		if cam.Projection.Far > cfg.Near {
			cfg.Far = cam.Projection.Far
		}
		cfg.Orthographic = cam.Projection.Kind == scene3d.ProjectionOrthographic
	}
	return cfg
}

// Validate checks that every field of c is finite and within its valid range.
// Every error wraps [ErrInvalidPostProcess].
func (c DepthOfFieldConfig) Validate() error {
	if !finite(c.FocusDistance) || c.FocusDistance <= 0 {
		return fmt.Errorf("%w: dof FocusDistance %v must be finite and > 0", ErrInvalidPostProcess, c.FocusDistance)
	}
	if !finite(c.FocusRange) || c.FocusRange <= 0 {
		return fmt.Errorf("%w: dof FocusRange %v must be finite and > 0", ErrInvalidPostProcess, c.FocusRange)
	}
	if !finite(c.MaxBlurRadius) || c.MaxBlurRadius < 0 {
		return fmt.Errorf("%w: dof MaxBlurRadius %v must be finite and >= 0", ErrInvalidPostProcess, c.MaxBlurRadius)
	}
	if !finite(c.Near) || !finite(c.Far) || c.Near <= 0 || c.Far <= c.Near {
		return fmt.Errorf("%w: dof requires 0 < Near < Far (got Near=%v, Far=%v)", ErrInvalidPostProcess, c.Near, c.Far)
	}
	return nil
}

// buildDoFDiscTaps returns 18 deterministic 2-ring disc bokeh offsets:
// 6 taps on the inner ring (r = 0.45) and 12 taps on the outer ring (r = 0.95).
func buildDoFDiscTaps() [postDoFTapCount][4]float32 {
	var taps [postDoFTapCount][4]float32
	idx := 0
	const innerR = 0.45
	for i := range 6 {
		a := float64(i) * (2.0 * math.Pi / 6.0)
		taps[idx] = [4]float32{
			float32(math.Cos(a) * innerR),
			float32(math.Sin(a) * innerR),
			innerR,
			0,
		}
		idx++
	}
	const outerR = 0.95
	for i := range 12 {
		a := (float64(i) + 0.5) * (2.0 * math.Pi / 12.0)
		taps[idx] = [4]float32{
			float32(math.Cos(a) * outerR),
			float32(math.Sin(a) * outerR),
			outerR,
			0,
		}
		idx++
	}
	return taps
}

// DepthOfFieldPassOptions configures [NewDepthOfFieldPass].
type DepthOfFieldPassOptions struct {
	Label        string
	Width        uint32
	Height       uint32
	ColorFormat  gpu.TextureFormat // intermediate color/CoC format; defaults to FormatRGBA16Float
	OutputFormat gpu.TextureFormat // destination format; defaults to dev.SurfaceFormat() or FormatRGBA8Unorm
	Retained     bool
	Config       DepthOfFieldConfig
}

// DepthOfFieldPass computes per-pixel signed Circle of Confusion from depth and
// gathers a depth-aware disc bokeh blur that keeps in-focus silhouettes sharp.
type DepthOfFieldPass struct {
	cx           *core.Scope
	label        string
	width        uint32
	height       uint32
	colorFormat  gpu.TextureFormat
	outputFormat gpu.TextureFormat
	retained     bool
	cfg          DepthOfFieldConfig

	vb          *gpu.Buffer
	linearSamp  *gpu.Sampler
	nearestSamp *gpu.Sampler
	cocBuf      *gpu.Buffer
	bokehBuf    *gpu.Buffer

	cocLayout   *gpu.BindGroupLayout
	bokehLayout *gpu.BindGroupLayout

	cocPipe      *gpu.Pipeline
	bokehPipe    *gpu.Pipeline
	bokehOutPipe *gpu.Pipeline

	cocRT *gpu.RenderTarget

	cachedCoCSrc   *gpu.Texture
	cachedCoCDepth *gpu.Texture
	cachedCoCBG    *gpu.BindGroup
	cachedBokehSrc *gpu.Texture
	cachedBokehBG  *gpu.BindGroup

	closed bool
}

// NewDepthOfFieldPass creates a scope-owned [DepthOfFieldPass] on dev.
func NewDepthOfFieldPass(cx *core.Scope, dev *gpu.Device, opts DepthOfFieldPassOptions) (*DepthOfFieldPass, error) {
	if err := postValidateSize(opts.Width, opts.Height); err != nil {
		return nil, err
	}
	if err := opts.Config.Validate(); err != nil {
		return nil, err
	}
	colorFmt, err := postResolveColorFormat(opts.ColorFormat)
	if err != nil {
		return nil, err
	}
	outFmt, err := postResolveOutputFormat(dev, opts.OutputFormat)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	shaders, err := getPostShaders()
	if err != nil {
		return nil, err
	}

	label := opts.Label
	if label == "" {
		label = "render3d.dof"
	}

	p := &DepthOfFieldPass{
		cx:           cx,
		label:        label,
		width:        opts.Width,
		height:       opts.Height,
		colorFormat:  colorFmt,
		outputFormat: outFmt,
		retained:     opts.Retained,
		cfg:          opts.Config,
	}

	if p.vb, err = createPostFullscreenVB(cx, dev, opts.Retained); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.linearSamp, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + ".linear",
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterLinear,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     opts.Retained,
	}); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.nearestSamp, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + ".nearest",
		MinFilter:    gpu.FilterNearest,
		MagFilter:    gpu.FilterNearest,
		MipmapFilter: gpu.FilterNearest,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     opts.Retained,
	}); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.cocBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".coc.u",
		Size:     postDoFCoCUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: opts.Retained,
	}); err != nil {
		_ = p.Close()
		return nil, err
	}
	if p.bokehBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".bokeh.u",
		Size:     postDoFBokehUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: opts.Retained,
	}); err != nil {
		_ = p.Close()
		return nil, err
	}

	if err := p.buildPipelines(dev, shaders); err != nil {
		_ = p.Close()
		return nil, err
	}
	if err := p.allocTargets(dev); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

func (p *DepthOfFieldPass) buildPipelines(dev *gpu.Device, shaders *postShaderCache) error {
	cocLayout, err := dev.CreateBindGroupLayoutDesc(p.cx, gpu.BindGroupLayoutDesc{
		Label: p.label + ".coc.layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: 4, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		return err
	}
	bokehLayout, err := dev.CreateBindGroupLayoutDesc(p.cx, gpu.BindGroupLayoutDesc{
		Label: p.label + ".bokeh.layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		_ = cocLayout.Close()
		return err
	}
	cocPipe, err := createPostPipeline(p.cx, dev, p.label+".coc.pipe", shaders.dofCoC, "fs_dof_coc", cocLayout, p.colorFormat)
	if err != nil {
		_ = postCloseAll(cocLayout, bokehLayout)
		return err
	}
	bokehPipe, err := createPostPipeline(p.cx, dev, p.label+".bokeh.pipe", shaders.dofBokeh, "fs_dof_bokeh", bokehLayout, p.colorFormat)
	if err != nil {
		_ = postCloseAll(cocPipe, cocLayout, bokehLayout)
		return err
	}
	var bokehOutPipe *gpu.Pipeline
	if p.outputFormat != p.colorFormat {
		bokehOutPipe, err = createPostPipeline(p.cx, dev, p.label+".bokeh.out", shaders.dofBokeh, "fs_dof_bokeh", bokehLayout, p.outputFormat)
		if err != nil {
			_ = postCloseAll(cocPipe, bokehPipe, cocLayout, bokehLayout)
			return err
		}
	}
	_ = postCloseAll(p.cachedCoCBG, p.cachedBokehBG,
		p.cocPipe, p.bokehPipe, p.bokehOutPipe,
		p.cocLayout, p.bokehLayout)
	p.cocLayout = cocLayout
	p.bokehLayout = bokehLayout
	p.cocPipe = cocPipe
	p.bokehPipe = bokehPipe
	p.bokehOutPipe = bokehOutPipe
	p.cachedCoCSrc = nil
	p.cachedCoCDepth = nil
	p.cachedCoCBG = nil
	p.cachedBokehSrc = nil
	p.cachedBokehBG = nil
	return nil
}

func (p *DepthOfFieldPass) allocTargets(dev *gpu.Device) error {
	rt, err := dev.CreateRenderTarget(p.cx, gpu.RenderTargetDesc{
		Label:       p.label + ".coc",
		Width:       p.width,
		Height:      p.height,
		ColorFormat: p.colorFormat,
		Retained:    p.retained,
	})
	if err != nil {
		return err
	}
	_ = postCloseAll(p.cachedBokehBG, p.cocRT)
	p.cocRT = rt
	p.cachedBokehSrc = nil
	p.cachedBokehBG = nil
	return nil
}

// Config returns the pass's current [DepthOfFieldConfig].
func (p *DepthOfFieldPass) Config() DepthOfFieldConfig {
	if p == nil {
		return DepthOfFieldConfig{}
	}
	return p.cfg
}

// SetConfig validates and updates the Depth of Field parameters.
func (p *DepthOfFieldPass) SetConfig(cfg DepthOfFieldConfig) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: dof pass is not valid", ErrInvalidPostProcess)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	p.cfg = cfg
	return nil
}

// Width returns the configured target width in pixels.
func (p *DepthOfFieldPass) Width() uint32 {
	if p == nil {
		return 0
	}
	return p.width
}

// Height returns the configured target height in pixels.
func (p *DepthOfFieldPass) Height() uint32 {
	if p == nil {
		return 0
	}
	return p.height
}

// Valid reports whether the pass is open and its GPU resources are live.
func (p *DepthOfFieldPass) Valid() bool {
	return p != nil && !p.closed &&
		p.vb.Valid() && p.cocRT.Valid() &&
		p.cocPipe.Valid() && p.bokehPipe.Valid() &&
		(p.bokehOutPipe == nil || p.bokehOutPipe.Valid())
}

func (p *DepthOfFieldPass) ensureLive(dev *gpu.Device) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: dof pass is not valid", ErrInvalidPostProcess)
	}
	if p.Valid() {
		return nil
	}
	if !p.retained || !p.vb.Valid() || !p.cocRT.Valid() {
		return fmt.Errorf("%w: dof pass is not valid", ErrInvalidPostProcess)
	}
	shaders, err := getPostShaders()
	if err != nil {
		return err
	}
	return p.buildPipelines(dev, shaders)
}

// Resize recreates the pass's offscreen CoC target for a new viewport size.
func (p *DepthOfFieldPass) Resize(dev *gpu.Device, width, height uint32) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: dof pass is not valid", ErrInvalidPostProcess)
	}
	if err := postValidateSize(width, height); err != nil {
		return err
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if p.width == width && p.height == height && p.cocRT.Valid() {
		return nil
	}
	p.width, p.height = width, height
	return p.allocTargets(dev)
}

// Apply runs the CoC prepass and depth-aware disc bokeh gather from srcColor and depthTex into dstColor. A nil dstColor writes to the swapchain in OutputFormat, and a texture dstColor must be in ColorFormat or OutputFormat. Every texture must have the pass size and dstColor cannot also be an input; otherwise Apply returns an error wrapping [ErrInvalidPostProcess].
func (p *DepthOfFieldPass) Apply(dev *gpu.Device, rec RenderPassRecorder, srcColor, depthTex, dstColor *gpu.Texture) error {
	if p == nil || p.closed {
		return fmt.Errorf("%w: dof pass is not valid", ErrInvalidPostProcess)
	}
	dstFmt, err := postDestFormat(dstColor, p.colorFormat, p.outputFormat)
	if err != nil {
		return err
	}
	return p.applyTo(dev, rec, srcColor, depthTex, dstColor, dstFmt)
}

func (p *DepthOfFieldPass) applyTo(dev *gpu.Device, rec RenderPassRecorder, srcColor, depthTex, dstColor *gpu.Texture, dstFmt gpu.TextureFormat) error {
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if err := p.ensureLive(dev); err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("%w: nil render pass recorder", ErrInvalidPostProcess)
	}
	if srcColor == nil || !srcColor.Valid() {
		return fmt.Errorf("%w: invalid source color texture", ErrInvalidPostProcess)
	}
	if depthTex == nil || !depthTex.Valid() {
		return fmt.Errorf("%w: invalid depth texture", ErrInvalidPostProcess)
	}
	if dstColor != nil && !dstColor.Valid() {
		return fmt.Errorf("%w: invalid destination texture", ErrInvalidPostProcess)
	}
	if err := postCheckInputs(p.width, p.height, srcColor, depthTex, dstColor); err != nil {
		return err
	}

	var cocData [postDoFCoCUniformSize]byte
	postPutFloat32(cocData[:], 0, p.cfg.FocusDistance)
	postPutFloat32(cocData[:], 4, p.cfg.FocusRange)
	postPutFloat32(cocData[:], 8, p.cfg.Near)
	postPutFloat32(cocData[:], 12, p.cfg.Far)
	if p.cfg.Orthographic {
		postPutFloat32(cocData[:], 16, 1)
	}
	dev.WriteBuffer(p.cocBuf, 0, cocData[:])

	var bokehData [postDoFBokehUniformSize]byte
	postPutFloat32(bokehData[:], 0, 1.0/float32(p.width))
	postPutFloat32(bokehData[:], 4, 1.0/float32(p.height))
	postPutFloat32(bokehData[:], 8, p.cfg.MaxBlurRadius)
	postPutFloat32(bokehData[:], 12, 0)
	taps := buildDoFDiscTaps()
	for i := range postDoFTapCount {
		base := 16 + i*16
		postPutFloat32(bokehData[:], base+0, taps[i][0])
		postPutFloat32(bokehData[:], base+4, taps[i][1])
		postPutFloat32(bokehData[:], base+8, taps[i][2])
		postPutFloat32(bokehData[:], base+12, taps[i][3])
	}
	dev.WriteBuffer(p.bokehBuf, 0, bokehData[:])

	if p.cachedCoCSrc != srcColor || p.cachedCoCDepth != depthTex || !p.cachedCoCBG.Valid() {
		bg, err := dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
			Label:  p.label + ".coc.bg",
			Layout: p.cocLayout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Buffer: p.cocBuf, Size: postDoFCoCUniformSize},
				{Binding: 1, Texture: srcColor},
				{Binding: 2, Texture: depthTex},
				{Binding: 3, Sampler: p.linearSamp},
				{Binding: 4, Sampler: p.nearestSamp},
			},
		})
		if err != nil {
			return err
		}
		_ = postCloseAll(p.cachedCoCBG)
		p.cachedCoCSrc = srcColor
		p.cachedCoCDepth = depthTex
		p.cachedCoCBG = bg
	}
	if p.cachedBokehSrc != srcColor || !p.cachedBokehBG.Valid() {
		bg, err := dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
			Label:  p.label + ".bokeh.bg",
			Layout: p.bokehLayout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Buffer: p.bokehBuf, Size: postDoFBokehUniformSize},
				{Binding: 1, Texture: srcColor},
				{Binding: 2, Texture: p.cocRT.Color},
				{Binding: 3, Sampler: p.linearSamp},
			},
		})
		if err != nil {
			return err
		}
		_ = postCloseAll(p.cachedBokehBG)
		p.cachedBokehSrc = srcColor
		p.cachedBokehBG = bg
	}

	// 1. CoC prepass -> cocRT.
	drawPostFullscreen(rec, p.label+".coc", p.cocRT.Color, p.cocPipe, p.cachedCoCBG, p.vb)

	// 2. Depth-aware disc bokeh gather -> dstColor (or swapchain).
	pipe := postPickPipeline(dstFmt, p.colorFormat, p.bokehPipe, p.bokehOutPipe)
	drawPostFullscreen(rec, p.label+".bokeh", dstColor, pipe, p.cachedBokehBG, p.vb)
	return nil
}

// Close releases all GPU resources owned by p. It is safe to call on a nil receiver or more than once.
func (p *DepthOfFieldPass) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	p.cachedCoCSrc = nil
	p.cachedCoCDepth = nil
	p.cachedBokehSrc = nil
	return postCloseAll(
		p.cachedCoCBG, p.cachedBokehBG,
		p.cocRT,
		p.cocPipe, p.bokehPipe, p.bokehOutPipe,
		p.cocLayout, p.bokehLayout,
		p.cocBuf, p.bokehBuf,
		p.linearSamp, p.nearestSamp, p.vb,
	)
}

// ============================================================================
// Composable PostStack3D
// ============================================================================

// PostStack3DOptions configures [NewPostStack3D].
type PostStack3DOptions struct {
	Label string
	// Width and Height of the offscreen 3D scene and post-processing targets (must be > 0).
	Width  uint32
	Height uint32
	// ColorFormat is the scene and intermediate ping-pong color format.
	// Defaults to [gpu.FormatRGBA16Float] when zero, preserving HDR highlights for Bloom.
	ColorFormat gpu.TextureFormat
	// DepthFormat is the scene render target's depth format. Defaults to [gpu.FormatDepth32Float] when zero.
	DepthFormat gpu.TextureFormat
	// OutputFormat is the swapchain format the final stage writes when dstColor is nil. A non-nil dstColor may be in OutputFormat or ColorFormat. Defaults to dev.SurfaceFormat() (or [gpu.FormatRGBA8Unorm]) when zero.
	OutputFormat gpu.TextureFormat
	// Retained opts offscreen targets, buffers, and samplers into automatic recreation after device loss.
	Retained bool

	// EnableSSAO enables the SSAO pass (stage 1). When false and SSAO is zero, DefaultSSAOConfig is prepared for later enabling.
	EnableSSAO bool
	SSAO       SSAOConfig
	// EnableDoF enables the Depth of Field pass (stage 2).
	EnableDoF bool
	DoF       DepthOfFieldConfig
	// EnableBloom enables the Bloom pass (stage 3).
	EnableBloom bool
	Bloom       BloomConfig
}

// PostStack3D composes SSAO, Depth of Field, and Bloom over an offscreen 3D
// color+depth render target, routing enabled passes cleanly through ping-pong
// offscreen targets and writing the final result to the swapchain or a chosen
// destination texture.
//
// Execution order is fixed to SSAO -> Depth of Field -> Bloom so ambient
// occlusion darkens crevices before bokeh blurring and highlights bloom after
// optical blur. All passes use portable WGSL compatible with both full and
// compute-free (WebGL 2 / OpenGL ES 3.0) NSGo GPU profiles.
type PostStack3D struct {
	cx           *core.Scope
	label        string
	width        uint32
	height       uint32
	colorFormat  gpu.TextureFormat
	depthFormat  gpu.TextureFormat
	outputFormat gpu.TextureFormat
	retained     bool

	enableSSAO  bool
	enableDoF   bool
	enableBloom bool

	ssao  *SSAOPass
	dof   *DepthOfFieldPass
	bloom *BloomPass

	sceneRT *gpu.RenderTarget
	pingRT  *gpu.RenderTarget
	pongRT  *gpu.RenderTarget

	vb          *gpu.Buffer
	linearSamp  *gpu.Sampler
	blitLayout  *gpu.BindGroupLayout
	blitPipe    *gpu.Pipeline
	blitOutPipe *gpu.Pipeline

	cachedBlitSrc *gpu.Texture
	cachedBlitBG  *gpu.BindGroup

	closed bool
}

// NewPostStack3D creates a scope-owned composable 3D post-processing stack on dev.
func NewPostStack3D(cx *core.Scope, dev *gpu.Device, opts PostStack3DOptions) (*PostStack3D, error) {
	if err := postValidateSize(opts.Width, opts.Height); err != nil {
		return nil, err
	}
	colorFmt, err := postResolveColorFormat(opts.ColorFormat)
	if err != nil {
		return nil, err
	}
	depthFmt, err := postResolveDepthFormat(opts.DepthFormat)
	if err != nil {
		return nil, err
	}
	outFmt, err := postResolveOutputFormat(dev, opts.OutputFormat)
	if err != nil {
		return nil, err
	}

	ssaoCfg := opts.SSAO
	if !opts.EnableSSAO && ssaoCfg == (SSAOConfig{}) {
		ssaoCfg = DefaultSSAOConfig()
	}
	if err := ssaoCfg.Validate(); err != nil {
		return nil, err
	}
	dofCfg := opts.DoF
	if !opts.EnableDoF && dofCfg == (DepthOfFieldConfig{}) {
		dofCfg = DefaultDepthOfFieldConfig()
	}
	if err := dofCfg.Validate(); err != nil {
		return nil, err
	}
	bloomCfg := opts.Bloom
	if !opts.EnableBloom && bloomCfg == (BloomConfig{}) {
		bloomCfg = DefaultBloomConfig()
	}
	if err := bloomCfg.Validate(); err != nil {
		return nil, err
	}

	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	shaders, err := getPostShaders()
	if err != nil {
		return nil, err
	}

	label := opts.Label
	if label == "" {
		label = "render3d.post"
	}

	s := &PostStack3D{
		cx:           cx,
		label:        label,
		width:        opts.Width,
		height:       opts.Height,
		colorFormat:  colorFmt,
		depthFormat:  depthFmt,
		outputFormat: outFmt,
		retained:     opts.Retained,
		enableSSAO:   opts.EnableSSAO,
		enableDoF:    opts.EnableDoF,
		enableBloom:  opts.EnableBloom,
	}

	if s.ssao, err = NewSSAOPass(cx, dev, SSAOPassOptions{
		Label:        label + ".ssao",
		Width:        opts.Width,
		Height:       opts.Height,
		ColorFormat:  colorFmt,
		OutputFormat: outFmt,
		Retained:     opts.Retained,
		Config:       ssaoCfg,
	}); err != nil {
		_ = s.Close()
		return nil, err
	}
	if s.dof, err = NewDepthOfFieldPass(cx, dev, DepthOfFieldPassOptions{
		Label:        label + ".dof",
		Width:        opts.Width,
		Height:       opts.Height,
		ColorFormat:  colorFmt,
		OutputFormat: outFmt,
		Retained:     opts.Retained,
		Config:       dofCfg,
	}); err != nil {
		_ = s.Close()
		return nil, err
	}
	if s.bloom, err = NewBloomPass(cx, dev, BloomPassOptions{
		Label:        label + ".bloom",
		Width:        opts.Width,
		Height:       opts.Height,
		ColorFormat:  colorFmt,
		OutputFormat: outFmt,
		Retained:     opts.Retained,
		Config:       bloomCfg,
	}); err != nil {
		_ = s.Close()
		return nil, err
	}

	if s.vb, err = createPostFullscreenVB(cx, dev, opts.Retained); err != nil {
		_ = s.Close()
		return nil, err
	}
	if s.linearSamp, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + ".blit.samp",
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterLinear,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     opts.Retained,
	}); err != nil {
		_ = s.Close()
		return nil, err
	}
	if err := s.buildPipelines(dev, shaders); err != nil {
		_ = s.Close()
		return nil, err
	}
	if err := s.allocTargets(dev); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostStack3D) buildPipelines(dev *gpu.Device, shaders *postShaderCache) error {
	blitLayout, err := dev.CreateBindGroupLayoutDesc(s.cx, gpu.BindGroupLayoutDesc{
		Label: s.label + ".blit.layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		return err
	}
	blitPipe, err := createPostPipeline(s.cx, dev, s.label+".blit", shaders.blit, "fs_blit", blitLayout, s.colorFormat)
	if err != nil {
		_ = blitLayout.Close()
		return err
	}
	var blitOutPipe *gpu.Pipeline
	if s.outputFormat != s.colorFormat {
		blitOutPipe, err = createPostPipeline(s.cx, dev, s.label+".blit.out", shaders.blit, "fs_blit", blitLayout, s.outputFormat)
		if err != nil {
			_ = postCloseAll(blitPipe, blitLayout)
			return err
		}
	}
	_ = postCloseAll(s.cachedBlitBG, s.blitPipe, s.blitOutPipe, s.blitLayout)
	s.blitLayout = blitLayout
	s.blitPipe = blitPipe
	s.blitOutPipe = blitOutPipe
	s.cachedBlitSrc = nil
	s.cachedBlitBG = nil
	return nil
}

func (s *PostStack3D) allocTargets(dev *gpu.Device) error {
	sceneRT, err := dev.CreateRenderTarget(s.cx, gpu.RenderTargetDesc{
		Label:       s.label + ".scene",
		Width:       s.width,
		Height:      s.height,
		ColorFormat: s.colorFormat,
		DepthFormat: s.depthFormat,
		Retained:    s.retained,
	})
	if err != nil {
		return err
	}
	pingRT, err := dev.CreateRenderTarget(s.cx, gpu.RenderTargetDesc{
		Label:       s.label + ".ping",
		Width:       s.width,
		Height:      s.height,
		ColorFormat: s.colorFormat,
		Retained:    s.retained,
	})
	if err != nil {
		_ = sceneRT.Close()
		return err
	}
	pongRT, err := dev.CreateRenderTarget(s.cx, gpu.RenderTargetDesc{
		Label:       s.label + ".pong",
		Width:       s.width,
		Height:      s.height,
		ColorFormat: s.colorFormat,
		Retained:    s.retained,
	})
	if err != nil {
		_ = sceneRT.Close()
		_ = pingRT.Close()
		return err
	}
	_ = postCloseAll(s.cachedBlitBG, s.sceneRT, s.pingRT, s.pongRT)
	s.sceneRT = sceneRT
	s.pingRT = pingRT
	s.pongRT = pongRT
	s.cachedBlitSrc = nil
	s.cachedBlitBG = nil
	return nil
}

// SceneTarget returns the stack-managed offscreen 3D color+depth [gpu.RenderTarget].
func (s *PostStack3D) SceneTarget() *gpu.RenderTarget {
	if s == nil || s.closed {
		return nil
	}
	return s.sceneRT
}

// BeginScenePass begins a render pass targeting [PostStack3D.SceneTarget], clearing
// color to clearColor and depth to 1.0 and storing both attachments for post-processing.
func (s *PostStack3D) BeginScenePass(rec RenderPassRecorder, clearColor core.Color) *gpu.RenderPass {
	if s == nil || s.closed || rec == nil || !s.sceneRT.Valid() {
		return nil
	}
	return rec.BeginRenderPass(gpu.RenderPassDesc{
		Label: s.label + ".scene",
		ColorAttachments: []gpu.ColorAttachment{{
			Texture:    s.sceneRT.Color,
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: clearColor,
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			Texture:      s.sceneRT.Depth,
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   1.0,
		},
	})
}

// SSAO returns the underlying [SSAOPass].
func (s *PostStack3D) SSAO() *SSAOPass {
	if s == nil {
		return nil
	}
	return s.ssao
}

// DepthOfField returns the underlying [DepthOfFieldPass].
func (s *PostStack3D) DepthOfField() *DepthOfFieldPass {
	if s == nil {
		return nil
	}
	return s.dof
}

// Bloom returns the underlying [BloomPass].
func (s *PostStack3D) Bloom() *BloomPass {
	if s == nil {
		return nil
	}
	return s.bloom
}

// SetSSAO enables or disables SSAO and updates its configuration.
func (s *PostStack3D) SetSSAO(enabled bool, cfg SSAOConfig) error {
	if s == nil || s.closed {
		return fmt.Errorf("%w: post stack is not valid", ErrInvalidPostProcess)
	}
	if err := s.ssao.SetConfig(cfg); err != nil {
		return err
	}
	s.enableSSAO = enabled
	return nil
}

// SetDepthOfField enables or disables Depth of Field and updates its configuration.
func (s *PostStack3D) SetDepthOfField(enabled bool, cfg DepthOfFieldConfig) error {
	if s == nil || s.closed {
		return fmt.Errorf("%w: post stack is not valid", ErrInvalidPostProcess)
	}
	if err := s.dof.SetConfig(cfg); err != nil {
		return err
	}
	s.enableDoF = enabled
	return nil
}

// SetBloom enables or disables Bloom and updates its configuration.
func (s *PostStack3D) SetBloom(enabled bool, cfg BloomConfig) error {
	if s == nil || s.closed {
		return fmt.Errorf("%w: post stack is not valid", ErrInvalidPostProcess)
	}
	if err := s.bloom.SetConfig(cfg); err != nil {
		return err
	}
	s.enableBloom = enabled
	return nil
}

// Width returns the stack's offscreen target width in pixels.
func (s *PostStack3D) Width() uint32 {
	if s == nil {
		return 0
	}
	return s.width
}

// Height returns the stack's offscreen target height in pixels.
func (s *PostStack3D) Height() uint32 {
	if s == nil {
		return 0
	}
	return s.height
}

// ColorFormat returns the offscreen scene and intermediate color target format.
func (s *PostStack3D) ColorFormat() gpu.TextureFormat {
	if s == nil {
		return gpu.FormatUndefined
	}
	return s.colorFormat
}

// DepthFormat returns the offscreen scene depth target format.
func (s *PostStack3D) DepthFormat() gpu.TextureFormat {
	if s == nil {
		return gpu.FormatUndefined
	}
	return s.depthFormat
}

// OutputFormat returns the final destination format (swapchain or dstColor).
func (s *PostStack3D) OutputFormat() gpu.TextureFormat {
	if s == nil {
		return gpu.FormatUndefined
	}
	return s.outputFormat
}

// Valid reports whether the stack and every internal pass and target are live.
func (s *PostStack3D) Valid() bool {
	return s != nil && !s.closed &&
		s.sceneRT.Valid() && s.pingRT.Valid() && s.pongRT.Valid() &&
		s.vb.Valid() && s.blitPipe.Valid() && (s.blitOutPipe == nil || s.blitOutPipe.Valid()) &&
		s.ssao.Valid() && s.dof.Valid() && s.bloom.Valid()
}

func (s *PostStack3D) ensureLive(dev *gpu.Device) error {
	if s == nil || s.closed {
		return fmt.Errorf("%w: post stack is not valid", ErrInvalidPostProcess)
	}
	if s.Valid() {
		return nil
	}
	if !s.retained || !s.sceneRT.Valid() || !s.pingRT.Valid() || !s.pongRT.Valid() || !s.vb.Valid() {
		return fmt.Errorf("%w: post stack is not valid", ErrInvalidPostProcess)
	}
	shaders, err := getPostShaders()
	if err != nil {
		return err
	}
	if !s.blitPipe.Valid() || (s.blitOutPipe != nil && !s.blitOutPipe.Valid()) {
		if err := s.buildPipelines(dev, shaders); err != nil {
			return err
		}
	}
	if err := s.ssao.ensureLive(dev); err != nil {
		return err
	}
	if err := s.dof.ensureLive(dev); err != nil {
		return err
	}
	return s.bloom.ensureLive(dev)
}

// Resize recreates the stack's scene, ping-pong, and per-pass offscreen targets.
func (s *PostStack3D) Resize(dev *gpu.Device, width, height uint32) error {
	if s == nil || s.closed {
		return fmt.Errorf("%w: post stack is not valid", ErrInvalidPostProcess)
	}
	if err := postValidateSize(width, height); err != nil {
		return err
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if s.width == width && s.height == height && s.Valid() {
		return nil
	}
	s.width, s.height = width, height
	if err := s.allocTargets(dev); err != nil {
		return err
	}
	if err := s.ssao.Resize(dev, width, height); err != nil {
		return err
	}
	if err := s.dof.Resize(dev, width, height); err != nil {
		return err
	}
	if err := s.bloom.Resize(dev, width, height); err != nil {
		return err
	}
	return nil
}

// ApplyScene executes all enabled passes on [PostStack3D.SceneTarget]'s color and depth attachments, writing the result into dstColor as [PostStack3D.Apply] does.
func (s *PostStack3D) ApplyScene(dev *gpu.Device, rec RenderPassRecorder, dstColor *gpu.Texture) error {
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if err := s.ensureLive(dev); err != nil {
		return err
	}
	return s.Apply(dev, rec, s.sceneRT.Color, s.sceneRT.Depth, dstColor)
}

// Apply executes all enabled passes in order (SSAO -> Depth of Field -> Bloom) on caller-provided srcColor and depthTex textures, writing the final result into dstColor. A nil dstColor writes to the swapchain in OutputFormat; a texture must have the stack size and either ColorFormat or OutputFormat. srcColor and depthTex must have the stack size too, and dstColor cannot be one of them; a mismatch returns an error wrapping [ErrInvalidPostProcess]. If depthTex is nil, depth-dependent passes (SSAO and Depth of Field) are skipped; if no pass runs, srcColor is blitted to dstColor.
func (s *PostStack3D) Apply(dev *gpu.Device, rec RenderPassRecorder, srcColor, depthTex, dstColor *gpu.Texture) error {
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if err := s.ensureLive(dev); err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("%w: nil render pass recorder", ErrInvalidPostProcess)
	}
	if srcColor == nil || !srcColor.Valid() {
		return fmt.Errorf("%w: invalid source color texture", ErrInvalidPostProcess)
	}
	if depthTex != nil && !depthTex.Valid() {
		return fmt.Errorf("%w: invalid depth texture", ErrInvalidPostProcess)
	}
	if dstColor != nil && !dstColor.Valid() {
		return fmt.Errorf("%w: invalid destination texture", ErrInvalidPostProcess)
	}
	if err := postCheckInputs(s.width, s.height, srcColor, depthTex, dstColor); err != nil {
		return err
	}
	dstFmt, err := postDestFormat(dstColor, s.colorFormat, s.outputFormat)
	if err != nil {
		return err
	}

	type stage uint8
	const (
		stageSSAO stage = iota + 1
		stageDoF
		stageBloom
	)
	var active [3]stage
	n := 0
	if s.enableSSAO && depthTex != nil {
		active[n] = stageSSAO
		n++
	}
	if s.enableDoF && depthTex != nil {
		active[n] = stageDoF
		n++
	}
	if s.enableBloom {
		active[n] = stageBloom
		n++
	}

	if n == 0 {
		if s.cachedBlitSrc != srcColor || !s.cachedBlitBG.Valid() {
			bg, err := dev.CreateBindGroupDesc(s.cx, gpu.BindGroupDesc{
				Label:  s.label + ".blit.bg",
				Layout: s.blitLayout,
				Entries: []gpu.BindGroupEntry{
					{Binding: 0, Texture: srcColor},
					{Binding: 1, Sampler: s.linearSamp},
				},
			})
			if err != nil {
				return err
			}
			_ = postCloseAll(s.cachedBlitBG)
			s.cachedBlitSrc = srcColor
			s.cachedBlitBG = bg
		}
		pipe := postPickPipeline(dstFmt, s.colorFormat, s.blitPipe, s.blitOutPipe)
		drawPostFullscreen(rec, s.label+".blit", dstColor, pipe, s.cachedBlitBG, s.vb)
		return nil
	}

	cur := srcColor
	pingNext := true
	for i := range n {
		last := i == n-1
		var target *gpu.Texture
		targetFmt := s.colorFormat
		if last {
			target = dstColor
			targetFmt = dstFmt
		} else {
			if pingNext {
				target = s.pingRT.Color
			} else {
				target = s.pongRT.Color
			}
			pingNext = !pingNext
		}

		var err error
		switch active[i] {
		case stageSSAO:
			err = s.ssao.applyTo(dev, rec, cur, depthTex, target, targetFmt)
		case stageDoF:
			err = s.dof.applyTo(dev, rec, cur, depthTex, target, targetFmt)
		case stageBloom:
			err = s.bloom.applyTo(dev, rec, cur, target, targetFmt)
		}
		if err != nil {
			return err
		}
		cur = target
	}
	return nil
}

// Close releases all GPU resources owned by s and its passes. It is safe to call on a nil receiver or more than once.
func (s *PostStack3D) Close() error {
	if s == nil || s.closed {
		return nil
	}
	s.closed = true
	s.cachedBlitSrc = nil
	return postCloseAll(
		s.cachedBlitBG,
		s.sceneRT, s.pingRT, s.pongRT,
		s.ssao, s.dof, s.bloom,
		s.blitPipe, s.blitOutPipe, s.blitLayout,
		s.linearSamp, s.vb,
	)
}
