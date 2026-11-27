package scenemodel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// BaseColorGroup is the bind group of the base color texture and sampler in ShaderWGSL, after render3d's camera (render3d.CameraGroup), scene lights and environment (render3d.LightGroup) and per-draw uniform (render3d.PBRDrawGroup).
const BaseColorGroup = render3d.PBRDrawGroup + 1

const (
	sceneEnvUniformSize     = 32
	skyboxUniformSize       = 80
	sceneClusterBaseBinding = 12
)

func rebindGroup1(src string, b0, b1, b2 int) string {
	s := strings.Replace(src, "@group(1) @binding(0)", fmt.Sprintf("@group(1) @binding(%d)", b0), 1)
	s = strings.Replace(s, "@group(1) @binding(1)", fmt.Sprintf("@group(1) @binding(%d)", b1), 1)
	s = strings.Replace(s, "@group(1) @binding(2)", fmt.Sprintf("@group(1) @binding(%d)", b2), 1)
	return s
}

func rebindProbeIBLDrawGroup(src string) string {
	g := fmt.Sprintf("@group(%d)", render3d.PBRDrawGroup)
	s := strings.Replace(src, g+" @binding(4)", g+" @binding(5)", 1)
	s = strings.Replace(s, g+" @binding(3)", g+" @binding(4)", 1)
	s = strings.Replace(s, g+" @binding(2)", g+" @binding(3)", 1)
	s = strings.Replace(s, g+" @binding(1)", g+" @binding(2)", 1)
	s = strings.Replace(s, g+" @binding(0)", g+" @binding(1)", 1)
	return s
}

// sceneTonemapWGSL provides portable exposure, tone mapping (None, Reinhard, ACES, Neutral, AgX) and sRGB encoding matching render3d.ApplyToneMap and render3d.ToneMapWGSL without exp or log2 builtins so NSGo's software GPU evaluates every operator identically to hardware backends.
const sceneTonemapWGSL = `
fn sm_tonemap_reinhard(c: vec3<f32>) -> vec3<f32> {
	return c / (vec3<f32>(1.0, 1.0, 1.0) + c);
}

fn sm_tonemap_aces(c: vec3<f32>) -> vec3<f32> {
	let num = c * (2.51 * c + vec3<f32>(0.03, 0.03, 0.03));
	let den = c * (2.43 * c + vec3<f32>(0.59, 0.59, 0.59)) + vec3<f32>(0.14, 0.14, 0.14);
	return clamp(num / den, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
}

fn sm_tonemap_neutral(color: vec3<f32>) -> vec3<f32> {
	let start_compression = 0.76;
	let desaturation = 0.15;
	let x = min(color.x, min(color.y, color.z));
	var offset: f32 = 0.04;
	if (x < 0.08) {
		offset = x - 6.25 * x * x;
	}
	var c = color - vec3<f32>(offset, offset, offset);
	let peak = max(c.x, max(c.y, c.z));
	if (peak < start_compression) {
		return clamp(c, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
	}
	let d = 1.0 - start_compression;
	let new_peak = 1.0 - (d * d) / (peak + d - start_compression);
	c = c * (new_peak / peak);
	let g = 1.0 - 1.0 / (desaturation * (peak - new_peak) + 1.0);
	return clamp(mix(c, vec3<f32>(new_peak, new_peak, new_peak), vec3<f32>(g, g, g)), vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
}

fn sm_fast_log2_agx(v: f32) -> f32 {
	var m = clamp(v, 0.0001757761, 16.291573);
	var e: f32 = 0.0;
	if (m >= 16.0) { m = m * 0.0625; e = e + 4.0; }
	if (m >= 4.0) { m = m * 0.25; e = e + 2.0; }
	if (m >= 2.0) { m = m * 0.5; e = e + 1.0; }
	if (m < 0.00390625) { m = m * 256.0; e = e - 8.0; }
	if (m < 0.0625) { m = m * 16.0; e = e - 4.0; }
	if (m < 0.25) { m = m * 4.0; e = e - 2.0; }
	if (m < 0.5) { m = m * 2.0; e = e - 1.0; }
	if (m < 1.0) { m = m * 2.0; e = e - 1.0; }
	if (m >= 1.41421356) { m = m * 0.5; e = e + 1.0; }
	let t = (m - 1.0) / (m + 1.0);
	let t2 = t * t;
	let poly = 1.0 + t2 * (0.33333334 + t2 * (0.2 + t2 * 0.14285715));
	return e + 2.88539008 * t * poly;
}

fn sm_agx_curve(x: vec3<f32>) -> vec3<f32> {
	let x2 = x * x;
	let x4 = x2 * x2;
	return 15.5 * x4 * x2
		- 40.14 * x4 * x
		+ 31.96 * x4
		- 6.868 * x2 * x
		+ 0.4298 * x2
		+ 0.1191 * x
		- vec3<f32>(0.00232, 0.00232, 0.00232);
}

fn sm_tonemap_agx(c: vec3<f32>) -> vec3<f32> {
	let inset = vec3<f32>(
		0.84247906 * c.x + 0.07843360 * c.y + 0.07922375 * c.z,
		0.04232824 * c.x + 0.87846864 * c.y + 0.07916613 * c.z,
		0.04237565 * c.x + 0.07843360 * c.y + 0.87914297 * c.z,
	);
	let min_ev = -12.47393;
	let inv_range = 0.06060606;
	let lx = clamp((sm_fast_log2_agx(inset.x) - min_ev) * inv_range, 0.0, 1.0);
	let ly = clamp((sm_fast_log2_agx(inset.y) - min_ev) * inv_range, 0.0, 1.0);
	let lz = clamp((sm_fast_log2_agx(inset.z) - min_ev) * inv_range, 0.0, 1.0);
	let curved = sm_agx_curve(vec3<f32>(lx, ly, lz));
	let outset = vec3<f32>(
		1.19687901 * curved.x - 0.09802088 * curved.y - 0.09902974 * curved.z,
		-0.05289685 * curved.x + 1.15190313 * curved.y - 0.09896118 * curved.z,
		-0.05297163 * curved.x - 0.09804345 * curved.y + 1.15107368 * curved.z,
	);
	let clamped = clamp(outset, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
	return vec3<f32>(
		pow(clamped.x, 2.2),
		pow(clamped.y, 2.2),
		pow(clamped.z, 2.2),
	);
}

fn scene_tonemap_srgb(rgb: vec3<f32>, exposure: f32, op: f32) -> vec3<f32> {
	let exposed = clamp(max(rgb * exposure, vec3<f32>(0.0, 0.0, 0.0)), vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(65504.0, 65504.0, 65504.0));
	var mapped = clamp(exposed, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
	if (op >= 0.5 && op < 1.5) {
		mapped = sm_tonemap_reinhard(exposed);
	} else if (op >= 1.5 && op < 2.5) {
		mapped = sm_tonemap_aces(exposed);
	} else if (op >= 2.5 && op < 3.5) {
		mapped = sm_tonemap_neutral(exposed);
	} else if (op >= 3.5) {
		mapped = sm_tonemap_agx(exposed);
	}
	return linear_to_srgb(mapped);
}
`

const sceneEnvDeclWGSL = `
struct SceneEnvUniform {
	params: vec4<f32>, // x=exposure, y=tonemap_op, z=has_dir_shadow, w=has_point_shadow
	extra: vec4<f32>,  // x=has_spot_shadow
}

@group(1) @binding(1) var<uniform> fog: Fog;
@group(1) @binding(2) var<uniform> scene_env: SceneEnvUniform;
`

func buildShaderPreludeWGSL(fallback bool) string {
	probeIBLWGSL := rebindProbeIBLDrawGroup(render3d.ProbeIBLWGSLForGroup(render3d.PBRDrawGroup))
	dirShadowWGSL := rebindGroup1(render3d.ShadowWGSLForGroup(render3d.LightGroup), 3, 4, 5)
	spotShadowWGSL := rebindGroup1(render3d.SpotShadowWGSLForGroup(render3d.LightGroup), 6, 7, 8)
	pointShadowWGSL := rebindGroup1(render3d.PointShadowWGSLForGroup(render3d.LightGroup), 9, 10, 11)
	localLightsWGSL := render3d.ClusteredLocalBindingsWGSL(render3d.LightGroup, sceneClusterBaseBinding)
	if fallback {
		localLightsWGSL = render3d.ClusteredFallbackLocalBindingsWGSL(render3d.LightGroup, sceneClusterBaseBinding)
	}

	return render3d.CameraWGSL +
		render3d.LightWGSL + "\n" +
		localLightsWGSL + "\n" +
		render3d.FogWGSL + "\n" +
		render3d.FogCameraWGSL + "\n" +
		sceneEnvDeclWGSL + "\n" +
		probeIBLWGSL + "\n" +
		dirShadowWGSL + "\n" +
		spotShadowWGSL + "\n" +
		pointShadowWGSL + "\n" +
		sceneTonemapWGSL + `
struct PBRDrawUniform {
	model: mat4x4<f32>,
	normal_matrix: mat4x4<f32>,
	base_color: vec4<f32>,
	pbr_params: vec4<f32>,
	emissive: vec4<f32>,
}

@group(2) @binding(0) var<uniform> pbr_draw: PBRDrawUniform;
@group(3) @binding(0) var base_tex: texture_2d<f32>;
@group(3) @binding(1) var base_samp: sampler;

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

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	let world = pbr_draw.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world;
	out.world_pos = world.xyz;
	out.world_normal = (pbr_draw.normal_matrix * vec4<f32>(in.normal, 0.0)).xyz;
	out.uv = in.uv;
	return out;
}
`
}

const litFragmentBodyWGSL = `
@fragment
fn fs_main(in: VertexOut, @builtin(front_facing) front_facing: bool) -> @location(0) vec4<f32> {
	// Sample in uniform control flow, before the cutout's discard.
	let texel = textureSample(base_tex, base_samp, in.uv);
	let base = pbr_draw.base_color * texel;
	if (pbr_draw.pbr_params.z > 0.0 && base.a < pbr_draw.pbr_params.z) {
		discard;
	}
	var n = normalize(in.world_normal);
	if (pbr_draw.pbr_params.w > 0.5 && !front_facing) {
		n = -n;
	}
	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	var dir_vis: f32 = 1.0;
	if (scene_env.params.z > 0.5) {
		dir_vis = sample_directional_shadow(in.world_pos, n);
	}
	var pt_vis: f32 = 1.0;
	if (scene_env.params.w > 0.5) {
		pt_vis = sample_point_shadow(in.world_pos, n);
	}
	var spot_vis: f32 = 1.0;
	if (scene_env.extra.x > 0.5) {
		spot_vis = sample_spot_shadow(in.world_pos, n);
	}
	var lit = shade_pbr_clustered_visibility(in.world_pos, n, view_dir, base.rgb, pbr_draw.pbr_params.x, pbr_draw.pbr_params.y, dir_vis, pt_vis, spot_vis, in.clip) + pbr_draw.emissive.rgb;
	if (probe_ibl.params.x > 0.0) {
		lit = lit + evaluate_probe_ibl(in.world_pos, n, view_dir, base.rgb, pbr_draw.pbr_params.x, pbr_draw.pbr_params.y, 1.0);
	}
	let fogged = apply_fog(lit, fog_camera_pos(in.world_pos), in.world_pos, fog);
`

const (
	litFragmentTailWGSL = `	let display = scene_tonemap_srgb(fogged, scene_env.params.x, scene_env.params.y);
	var alpha = 1.0;
	if (pbr_draw.emissive.w > 0.5) {
		alpha = base.a;
	}
	return vec4<f32>(display, alpha);
}
`
	linearFragmentTailWGSL = `	var alpha = 1.0;
	if (pbr_draw.emissive.w > 0.5) {
		alpha = base.a;
	}
	return vec4<f32>(fogged, alpha);
}
`
	unlitFragmentBodyWGSL = `
@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	// Sample in uniform control flow, before the cutout's discard.
	let texel = textureSample(base_tex, base_samp, in.uv);
	let base = pbr_draw.base_color * texel;
	if (pbr_draw.pbr_params.z > 0.0 && base.a < pbr_draw.pbr_params.z) {
		discard;
	}
	var alpha = 1.0;
	if (pbr_draw.emissive.w > 0.5) {
		alpha = base.a;
	}
	return vec4<f32>(linear_to_srgb(base.rgb + pbr_draw.emissive.rgb), alpha);
}
`
)

// ShaderWGSL is the clustered-lighting shader module Renderer draws with in ShadingLit on devices with storage buffers: render3d metallic-roughness PBR lighting with directional/point/spot lights, clustered local lights, optional directional/point/spot shadows, per-object reflection probes and light probe SH irradiance (falling back to scene sky IBL), distance and height fog, exposure, tone mapping, and a base color texture.
var ShaderWGSL = buildShaderPreludeWGSL(false) + litFragmentBodyWGSL + litFragmentTailWGSL

// linearShaderWGSL is ShaderWGSL writing unclamped linear HDR radiance without tone mapping or sRGB encoding, used when RendererOptions.LinearOutput is set (for example when baking reflection and light probes into an HDR target).
var linearShaderWGSL = buildShaderPreludeWGSL(false) + litFragmentBodyWGSL + linearFragmentTailWGSL

// unlitShaderWGSL is ShaderWGSL with a fragment stage that writes the textured base color plus the emissive color and ignores the lights, which ShadingUnlit draws with.
var unlitShaderWGSL = buildShaderPreludeWGSL(false) + unlitFragmentBodyWGSL

// fallbackShaderWGSL, fallbackLinearShaderWGSL and fallbackUnlitShaderWGSL are the uniform-buffer fallback variants of ShaderWGSL, linearShaderWGSL and unlitShaderWGSL for targets without storage buffers (WebGL 2, OpenGL ES 3.0, Linux GtkGLArea, nsgotest.NoGPUCompute).
var (
	fallbackShaderWGSL       = buildShaderPreludeWGSL(true) + litFragmentBodyWGSL + litFragmentTailWGSL
	fallbackLinearShaderWGSL = buildShaderPreludeWGSL(true) + litFragmentBodyWGSL + linearFragmentTailWGSL
	fallbackUnlitShaderWGSL  = buildShaderPreludeWGSL(true) + unlitFragmentBodyWGSL
)

// sceneSkyboxWGSL draws the scene's HDR/gradient skybox cubemap through the exact same exposure and tone mapping curve as ShaderWGSL.
const sceneSkyboxHeadWGSL = `struct SkyboxParams {
	inv_view_proj: mat4x4<f32>,
	exposure: f32,
	tonemap_op: f32,
	_pad0: f32,
	_pad1: f32,
}

@group(0) @binding(0) var<uniform> skybox: SkyboxParams;
@group(0) @binding(1) var skybox_tex: texture_cube<f32>;
@group(0) @binding(2) var skybox_sampler: sampler;

fn linear_to_srgb_channel(c: f32) -> f32 {
	let x = clamp(c, 0.0, 1.0);
	if (x <= 0.0031308) {
		return 12.92 * x;
	}
	return 1.055 * pow(x, 1.0 / 2.4) - 0.055;
}

fn linear_to_srgb(rgb: vec3<f32>) -> vec3<f32> {
	return vec3<f32>(
		linear_to_srgb_channel(rgb.x),
		linear_to_srgb_channel(rgb.y),
		linear_to_srgb_channel(rgb.z),
	);
}
` + sceneTonemapWGSL + `
struct SkyboxVertexOut {
	@builtin(position) pos: vec4<f32>,
	@location(0) dir: vec3<f32>,
}

@vertex
fn vs_skybox(@builtin(vertex_index) vid: u32) -> SkyboxVertexOut {
	var ndc = vec2<f32>(-1.0, -1.0);
	if (vid == 1u) {
		ndc = vec2<f32>(3.0, -1.0);
	} else if (vid == 2u) {
		ndc = vec2<f32>(-1.0, 3.0);
	}
	let world = skybox.inv_view_proj * vec4<f32>(ndc, 1.0, 1.0);
	var out: SkyboxVertexOut;
	out.pos = vec4<f32>(ndc, 1.0, 1.0);
	out.dir = world.xyz;
	return out;
}
`

const sceneSkyboxWGSL = sceneSkyboxHeadWGSL + `
@fragment
fn fs_skybox(in: SkyboxVertexOut) -> @location(0) vec4<f32> {
	let dir = normalize(in.dir);
	let sampled = textureSampleLevel(skybox_tex, skybox_sampler, dir, 0.0);
	return vec4<f32>(scene_tonemap_srgb(sampled.rgb, skybox.exposure, skybox.tonemap_op), 1.0);
}
`

const sceneSkyboxLinearWGSL = sceneSkyboxHeadWGSL + `
@fragment
fn fs_skybox(in: SkyboxVertexOut) -> @location(0) vec4<f32> {
	let dir = normalize(in.dir);
	let sampled = textureSampleLevel(skybox_tex, skybox_sampler, dir, 0.0);
	return vec4<f32>(sampled.rgb, 1.0);
}
`

// RendererOptions configures NewRenderer.
type RendererOptions struct {
	// Label prefixes the debug labels of the GPU resources; empty is "scenemodel".
	Label string
	// ColorFormat is the color attachment's format; gpu.FormatUndefined is the device's surface format.
	ColorFormat gpu.TextureFormat
	// DepthFormat is the depth attachment's format, which every pass the renderer draws in must have; gpu.FormatUndefined is gpu.FormatDepth24Plus.
	DepthFormat gpu.TextureFormat
	// Retained keeps CPU copies of the buffers and textures so NSGo recreates them after a device loss (gpu.BufferDesc.Retained).
	Retained bool
	// DisableOcclusion disables CPU software occlusion culling even when Built holds occluders.
	DisableOcclusion bool
	// OcclusionWidth and OcclusionHeight configure the software render3d.OcclusionBuffer resolution; non-positive values use render3d's defaults (128x72).
	OcclusionWidth, OcclusionHeight int
	// LODHysteresis overrides the relative deadband [0, 0.5] used by render3d.Queue for LOD selection; 0 uses Built.LODHysteresis.
	LODHysteresis float32
	// LinearOutput compiles linear HDR shaders (no tone mapping or sRGB encode) for rendering into a floating-point target such as FormatRGBA16Float when baking reflection or light probes.
	LinearOutput bool
	// DisableProbes excludes baked local reflection probes and light probe grids from shading (still keeping direct lights, shadows, sky and emissives), used when baking or rebaking probes.
	DisableProbes bool
	// ClusterConfig configures the internal render3d.ClusteredLightSystem used for unshadowed point and spot lights; the zero value uses render3d.DefaultClusterConfig() (and falls back automatically to the uniform-buffer fallback path if the device's MaxStorageBufferBindingSize cannot fit the default storage buffers).
	ClusterConfig render3d.ClusterConfig
}

// Shading says how Renderer shades the parts it draws (Renderer.SetShading).
type Shading uint8

const (
	// ShadingLit lights every part with the render3d.LightRig given to Prepare (or built.LightRig via PrepareScene), plus the scene's environment. It is a Renderer's shading until SetShading changes it.
	ShadingLit Shading = iota
	// ShadingUnlit draws every part at its textured base color plus its emissive color and ignores the lights, as an editor's unlit view shows a scene.
	ShadingUnlit
)

// String returns "lit" or "unlit", or "Shading(n)" for any other value.
func (s Shading) String() string {
	switch s {
	case ShadingLit:
		return "lit"
	case ShadingUnlit:
		return "unlit"
	}
	return fmt.Sprintf("Shading(%d)", uint8(s))
}

type partDraw struct {
	drawBuf  *gpu.Buffer
	probeBuf *gpu.Buffer
	group    *gpu.BindGroup
	tex0     *gpu.Texture
	tex1     *gpu.Texture
	texSky   *gpu.Texture
}

// Renderer draws a Built scene's drawables, lights, shadows, reflection probes, light probe grids and environment on a gpu.Device, culled and ordered by a render3d.Queue (frustum culling, software occlusion culling when Built has occluders, LOD selection with hysteresis, opaque surfaces front to back by pipeline, material and mesh, blended ones back to front). Each frame, call PrepareScene (or Prepare with custom lights), call RecordShadows before the main render pass when any light casts shadows, and call DrawSky and Draw inside the render pass. Its GPU resources belong to the scope given to NewRenderer, or are released by Close. Use it from the engine goroutine, like the device.
type Renderer struct {
	built *Built
	dev   *gpu.Device

	cam           *render3d.CameraBuffer
	clustered     *render3d.ClusteredLightSystem
	sceneLayout   *gpu.BindGroupLayout
	sceneGroup    *gpu.BindGroup
	fogBuf        *gpu.Buffer
	envBuf        *gpu.Buffer
	dirShadowBuf  *gpu.Buffer
	spotShadowBuf *gpu.Buffer
	ptShadowBuf   *gpu.Buffer
	dummyCube     *render3d.GPUCubemap
	iblCube       *render3d.GPUCubemap
	probeCubes    []*render3d.GPUCubemap
	dummyDepth    *gpu.Texture
	shadowSampler *gpu.Sampler

	dirShadow        *render3d.DirectionalShadowMap
	spotShadow       *render3d.SpotShadowMap
	pointShadow      *render3d.PointShadowMap
	dirShadowActive  bool
	spotShadowActive bool
	ptShadowActive   bool

	skyPipe    *gpu.Pipeline
	skyLayout  *gpu.BindGroupLayout
	skyUniform *gpu.Buffer
	skyCube    *render3d.GPUCubemap
	skyGroup   *gpu.BindGroup

	drawLayout *gpu.BindGroupLayout
	texLayout  *gpu.BindGroupLayout
	// pipes holds the pipeline of each DrawPipeline ID for each Shading, the unlit ones made on the first SetShading(ShadingUnlit).
	pipes   [2][4]*gpu.Pipeline
	shading Shading
	// cx, label and opts are what the pipelines SetShading makes need.
	cx    *core.Scope
	label string
	opts  RendererOptions

	white    *render3d.GPUTexture
	textures []*render3d.GPUTexture
	samplers []*render3d.GPUSampler
	// meshes holds each mesh by MeshID, and materials each material's texture bind group by MaterialID.
	meshes    []*render3d.GPUMesh
	materials []*gpu.BindGroup
	// draws holds the per-draw uniform and probe IBL bind group of each part, by drawable and part.
	draws [][]partDraw

	queue         render3d.Queue
	occ           *render3d.OcclusionBuffer
	occluders     []render3d.Occluder
	items         []render3d.DrawItem
	plan          render3d.RenderPlan
	clusterLights []render3d.ClusterLight
	viewportW     float32
	viewportH     float32

	fogScratch        []byte
	envScratch        [sceneEnvUniformSize]byte
	drawScratch       [render3d.PBRDrawUniformSize]byte
	probeScratch      [render3d.ProbeIBLUniformSize]byte
	dirShadowScratch  []byte
	spotShadowScratch [render3d.SpotShadowUniformSize]byte
	ptShadowScratch   [render3d.PointShadowUniformSize]byte
	skyScratch        [skyboxUniformSize]byte
}

// NewRenderer uploads every mesh, texture, material, sky cubemap, IBL prefilter and shadow map of b to dev, scoped to cx, and creates the buffers and pipelines that draw them. b's drawables are fixed from here on; their nodes may move, hide and show.
func NewRenderer(cx *core.Scope, dev *gpu.Device, b *Built, opts RendererOptions) (*Renderer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "scenemodel: nil device"}
	}
	if b == nil {
		return nil, errors.New("scenemodel: nil Built")
	}
	r := &Renderer{built: b, dev: dev, cx: cx, opts: opts}
	if err := r.setup(cx, opts); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

// setup creates r's resources.
func (r *Renderer) setup(cx *core.Scope, opts RendererOptions) error {
	dev := r.dev
	label := opts.Label
	if label == "" {
		label = "scenemodel"
	}
	r.label = label
	var err error
	if r.cam, err = render3d.NewCameraBuffer(cx, dev); err != nil {
		return fmt.Errorf("scenemodel: camera buffer: %w", err)
	}
	if err := r.setupSceneResources(cx, label, opts); err != nil {
		return err
	}
	if r.drawLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".draw",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 4, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 5, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	}); err != nil {
		return fmt.Errorf("scenemodel: draw layout: %w", err)
	}
	if r.texLayout, err = render3d.TextureBindingLayout(cx, dev); err != nil {
		return fmt.Errorf("scenemodel: texture layout: %w", err)
	}
	if err := r.uploadMaterials(cx, label, opts.Retained); err != nil {
		return err
	}
	if err := r.uploadMeshes(cx, label, opts.Retained); err != nil {
		return err
	}
	var zeroProbe render3d.ProbeIBLUniform
	zeroProbe.Encode(r.probeScratch[:])
	r.draws = make([][]partDraw, len(r.built.Drawables))
	for di, d := range r.built.Drawables {
		r.draws[di] = make([]partDraw, len(d.Parts))
		for pi := range d.Parts {
			u := drawUniform(d, &d.Parts[pi])
			drawBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
				Label:    fmt.Sprintf("%s.draw.%d.%d", label, di, pi),
				Size:     render3d.PBRDrawUniformSize,
				Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
				Data:     u.Encode(r.drawScratch[:]),
				Retained: opts.Retained,
			})
			if err != nil {
				return fmt.Errorf("scenemodel: object %q draw buffer: %w", d.Object, err)
			}
			probeBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
				Label:    fmt.Sprintf("%s.probe.%d.%d", label, di, pi),
				Size:     render3d.ProbeIBLUniformSize,
				Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
				Data:     r.probeScratch[:],
				Retained: opts.Retained,
			})
			if err != nil {
				_ = drawBuf.Close()
				return fmt.Errorf("scenemodel: object %q probe buffer: %w", d.Object, err)
			}
			pd := partDraw{drawBuf: drawBuf, probeBuf: probeBuf}
			skyTex := r.dummyCube.Texture()
			if r.iblCube != nil {
				skyTex = r.iblCube.Texture()
			}
			if err := r.rebindPartDraw(&pd, r.dummyCube.Texture(), r.dummyCube.Texture(), skyTex); err != nil {
				_ = drawBuf.Close()
				_ = probeBuf.Close()
				return fmt.Errorf("scenemodel: object %q draw bind group: %w", d.Object, err)
			}
			r.draws[di][pi] = pd
		}
	}
	if opts.LODHysteresis > 0 {
		r.queue.LODHysteresis = opts.LODHysteresis
	} else {
		r.queue.LODHysteresis = r.built.LODHysteresis
	}
	if !opts.DisableOcclusion && len(r.built.Occluders) > 0 {
		r.occ = render3d.NewOcclusionBuffer(opts.OcclusionWidth, opts.OcclusionHeight)
	}
	if err := r.setupSkybox(cx, label, opts); err != nil {
		return err
	}
	return r.createPipelines(ShadingLit)
}

func (r *Renderer) setupSceneResources(cx *core.Scope, label string, opts RendererOptions) error {
	dev := r.dev
	retained := opts.Retained
	var err error

	newBuf := func(name string, size uint64) (*gpu.Buffer, error) {
		return dev.CreateBuffer(cx, gpu.BufferDesc{
			Label:    label + "." + name,
			Size:     size,
			Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
			Retained: retained,
		})
	}
	clusterCfg := opts.ClusterConfig
	defaultClusterCfg := clusterCfg == (render3d.ClusterConfig{})
	if clusterCfg.Label == "" {
		clusterCfg.Label = label + ".clustered"
	}
	if retained {
		clusterCfg.Retained = true
	}
	if r.clustered, err = render3d.NewClusteredLightSystem(cx, dev, clusterCfg); err != nil {
		if defaultClusterCfg && errors.Is(err, render3d.ErrInvalidClusterConfig) && !clusterCfg.ForceFallback {
			clusterCfg.ForceFallback = true
			r.clustered, err = render3d.NewClusteredLightSystem(cx, dev, clusterCfg)
		}
		if err != nil {
			return fmt.Errorf("scenemodel: clustered lights: %w", err)
		}
	}
	if r.fogBuf, err = newBuf("fog", render3d.FogUniformSize); err != nil {
		return fmt.Errorf("scenemodel: fog buffer: %w", err)
	}
	if r.envBuf, err = newBuf("env", sceneEnvUniformSize); err != nil {
		return fmt.Errorf("scenemodel: env buffer: %w", err)
	}
	if r.dirShadowBuf, err = newBuf("dir_shadow", render3d.ShadowUniformSize); err != nil {
		return fmt.Errorf("scenemodel: directional shadow buffer: %w", err)
	}
	if r.spotShadowBuf, err = newBuf("spot_shadow", render3d.SpotShadowUniformSize); err != nil {
		return fmt.Errorf("scenemodel: spot shadow buffer: %w", err)
	}
	if r.ptShadowBuf, err = newBuf("point_shadow", render3d.PointShadowUniformSize); err != nil {
		return fmt.Errorf("scenemodel: point shadow buffer: %w", err)
	}

	// Always allocate a 1x1 black fallback cubemap for unbound reflection probe / sky IBL slots.
	blackCube, err := solidHDRCubemap(math3d.Vec3Zero(), 1)
	if err != nil {
		return fmt.Errorf("scenemodel: dummy ibl cubemap: %w", err)
	}
	if r.dummyCube, err = render3d.UploadCubemapHDR(cx, dev, blackCube, render3d.CubemapUpload{Label: label + ".ibl.dummy", Retained: retained}); err != nil {
		return fmt.Errorf("scenemodel: dummy ibl cubemap upload: %w", err)
	}
	if r.built.Environment.IBLPrefilter != nil && r.built.Environment.IBLIntensity > 0 {
		if r.iblCube, err = render3d.UploadCubemapHDR(cx, dev, r.built.Environment.IBLPrefilter, render3d.CubemapUpload{Label: label + ".ibl", Retained: retained}); err != nil {
			return fmt.Errorf("scenemodel: ibl cubemap: %w", err)
		}
	}
	if !opts.DisableProbes && len(r.built.ReflectionProbes) > 0 {
		r.probeCubes = make([]*render3d.GPUCubemap, len(r.built.ReflectionProbes))
		for i, rp := range r.built.ReflectionProbes {
			if rp == nil || rp.Prefiltered == nil {
				continue
			}
			gc, err := render3d.UploadCubemapHDR(cx, dev, rp.Prefiltered, render3d.CubemapUpload{
				Label:    fmt.Sprintf("%s.probe.%s", label, rp.Object),
				Retained: retained,
			})
			if err != nil {
				return fmt.Errorf("scenemodel: reflection probe %q cubemap: %w", rp.Object, err)
			}
			r.probeCubes[i] = gc
		}
	}

	// Dummy 1x1 depth texture & comparison sampler for inactive shadow slots.
	if r.dummyDepth, err = dev.CreateTexture(cx, gpu.TextureDesc{
		Label:    label + ".shadow.dummy",
		Width:    1,
		Height:   1,
		Format:   gpu.FormatDepth32Float,
		Usage:    gpu.TextureRenderAttachment | gpu.TextureSampled,
		Retained: retained,
	}); err != nil {
		return fmt.Errorf("scenemodel: dummy depth texture: %w", err)
	}
	if r.shadowSampler, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:     label + ".shadow.sampler",
		MinFilter: gpu.FilterLinear,
		MagFilter: gpu.FilterLinear,
		AddressU:  gpu.AddressClampToEdge,
		AddressV:  gpu.AddressClampToEdge,
		Compare:   gpu.CompareLessEqual,
		Retained:  retained,
	}); err != nil {
		return fmt.Errorf("scenemodel: shadow comparison sampler: %w", err)
	}

	// Allocate shadow maps only for light kinds that have at least one shadow-casting light in the scene.
	var wantDirShadow, wantSpotShadow, wantPtShadow bool
	for _, l := range r.built.Lights {
		if l == nil || !l.Doc.Shadows {
			continue
		}
		switch l.Doc.Kind {
		case sceneformat.LightDirectional:
			wantDirShadow = true
		case sceneformat.LightSpot:
			wantSpotShadow = true
		case sceneformat.LightPoint:
			wantPtShadow = true
		}
	}
	maxCasters := uint32(max(256, len(r.built.Drawables)*2))
	if wantDirShadow {
		smOpts := render3d.DefaultShadowMapOptions()
		smOpts.Label = label + ".dir_shadow"
		smOpts.Resolution = 512
		smOpts.MaxCasters = maxCasters
		smOpts.Retained = retained
		if r.dirShadow, err = render3d.NewDirectionalShadowMap(cx, dev, smOpts); err != nil {
			return fmt.Errorf("scenemodel: directional shadow map: %w", err)
		}
	}
	if wantSpotShadow {
		cfg := render3d.DefaultSpotShadowConfig()
		cfg.Label = label + ".spot_shadow"
		cfg.Resolution = 512
		cfg.MaxCasters = maxCasters
		cfg.Retained = retained
		if r.spotShadow, err = render3d.NewSpotShadowMap(cx, dev, cfg); err != nil {
			return fmt.Errorf("scenemodel: spot shadow map: %w", err)
		}
	}
	if wantPtShadow {
		cfg := render3d.DefaultPointShadowConfig()
		cfg.Label = label + ".point_shadow"
		cfg.FaceResolution = 256
		cfg.MaxCasters = maxCasters
		cfg.Retained = retained
		if r.pointShadow, err = render3d.NewPointShadowMap(cx, dev, cfg); err != nil {
			return fmt.Errorf("scenemodel: point shadow map: %w", err)
		}
	}

	dirTex := r.dummyDepth
	if r.dirShadow != nil {
		dirTex = r.dirShadow.DepthTexture()
	}
	spotTex, spotSamp := r.dummyDepth, r.shadowSampler
	if r.spotShadow != nil {
		spotTex, spotSamp = r.spotShadow.DepthTexture(), r.spotShadow.Sampler()
	}
	ptTex, ptSamp := r.dummyDepth, r.shadowSampler
	if r.pointShadow != nil {
		ptTex, ptSamp = r.pointShadow.DepthTexture(), r.pointShadow.Sampler()
	}

	sceneLayoutEntries := []gpu.BindGroupLayoutEntry{
		{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
		{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
		{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
		{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
		{Binding: 4, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
		{Binding: 5, Visibility: gpu.StageFragment, Type: gpu.BindingComparisonSampler},
		{Binding: 6, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
		{Binding: 7, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
		{Binding: 8, Visibility: gpu.StageFragment, Type: gpu.BindingComparisonSampler},
		{Binding: 9, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
		{Binding: 10, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
		{Binding: 11, Visibility: gpu.StageFragment, Type: gpu.BindingComparisonSampler},
	}
	sceneLayoutEntries = r.clustered.AppendLocalLayoutEntries(sceneLayoutEntries, sceneClusterBaseBinding)
	if r.sceneLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label:   label + ".scene",
		Entries: sceneLayoutEntries,
	}); err != nil {
		return fmt.Errorf("scenemodel: scene layout: %w", err)
	}
	sceneGroupEntries := []gpu.BindGroupEntry{
		{Binding: 0, Buffer: r.clustered.RigBuffer(), Size: render3d.LightUniformSize},
		{Binding: 1, Buffer: r.fogBuf, Size: render3d.FogUniformSize},
		{Binding: 2, Buffer: r.envBuf, Size: sceneEnvUniformSize},
		{Binding: 3, Buffer: r.dirShadowBuf, Size: render3d.ShadowUniformSize},
		{Binding: 4, Texture: dirTex},
		{Binding: 5, Sampler: r.shadowSampler},
		{Binding: 6, Buffer: r.spotShadowBuf, Size: render3d.SpotShadowUniformSize},
		{Binding: 7, Texture: spotTex},
		{Binding: 8, Sampler: spotSamp},
		{Binding: 9, Buffer: r.ptShadowBuf, Size: render3d.PointShadowUniformSize},
		{Binding: 10, Texture: ptTex},
		{Binding: 11, Sampler: ptSamp},
	}
	sceneGroupEntries = r.clustered.AppendLocalBindGroupEntries(sceneGroupEntries, sceneClusterBaseBinding)
	if r.sceneGroup, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:   label + ".scene",
		Layout:  r.sceneLayout,
		Entries: sceneGroupEntries,
	}); err != nil {
		return fmt.Errorf("scenemodel: scene bind group: %w", err)
	}

	// Upload initial fog & env uniforms.
	r.fogScratch = render3d.AppendFogUniforms(r.fogScratch[:0], r.built.Environment.Fog.Uniforms())
	dev.WriteBuffer(r.fogBuf, 0, r.fogScratch)
	r.writeEnvUniform(false, false, false)
	return nil
}

func (r *Renderer) rebindPartDraw(pd *partDraw, tex0, tex1, texSky *gpu.Texture) error {
	if pd.group != nil && pd.group.Valid() && pd.tex0 == tex0 && pd.tex1 == tex1 && pd.texSky == texSky {
		return nil
	}
	bg, err := r.dev.CreateBindGroupDesc(r.cx, gpu.BindGroupDesc{
		Label:  r.label + ".draw",
		Layout: r.drawLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: pd.drawBuf, Size: render3d.PBRDrawUniformSize},
			{Binding: 1, Buffer: pd.probeBuf, Size: render3d.ProbeIBLUniformSize},
			{Binding: 2, Texture: tex0},
			{Binding: 3, Texture: tex1},
			{Binding: 4, Texture: texSky},
			{Binding: 5, Sampler: r.dummyCube.Sampler()},
		},
	})
	if err != nil {
		return err
	}
	old := pd.group
	pd.group = bg
	pd.tex0, pd.tex1, pd.texSky = tex0, tex1, texSky
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (r *Renderer) setupSkybox(cx *core.Scope, label string, opts RendererOptions) error {
	if r.built.Environment.SkyCubemap == nil {
		return nil
	}
	dev := r.dev
	var err error
	if r.skyCube, err = render3d.UploadCubemapHDR(cx, dev, r.built.Environment.SkyCubemap, render3d.CubemapUpload{
		Label:    label + ".sky",
		Retained: opts.Retained,
	}); err != nil {
		return fmt.Errorf("scenemodel: sky cubemap: %w", err)
	}
	if r.skyUniform, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".sky.uniform",
		Size:     skyboxUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: opts.Retained,
	}); err != nil {
		return fmt.Errorf("scenemodel: sky uniform: %w", err)
	}
	if r.skyLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".sky",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	}); err != nil {
		return fmt.Errorf("scenemodel: sky layout: %w", err)
	}
	if r.skyGroup, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + ".sky",
		Layout: r.skyLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: r.skyUniform, Size: skyboxUniformSize},
			{Binding: 1, Texture: r.skyCube.Texture()},
			{Binding: 2, Sampler: r.skyCube.Sampler()},
		},
	}); err != nil {
		return fmt.Errorf("scenemodel: sky bind group: %w", err)
	}
	skySrc := sceneSkyboxWGSL
	if opts.LinearOutput {
		skySrc = sceneSkyboxLinearWGSL
	}
	mod, err := gpu.CompileWGSL(skySrc)
	if err != nil {
		return fmt.Errorf("scenemodel: compile sky shader: %w", err)
	}
	colorFormat := opts.ColorFormat
	if colorFormat == gpu.FormatUndefined {
		colorFormat = dev.SurfaceFormat()
	}
	depthFormat := opts.DepthFormat
	if depthFormat == gpu.FormatUndefined {
		depthFormat = gpu.FormatDepth24Plus
	}
	if r.skyPipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
		Label:     label + ".sky.pipeline",
		Layouts:   []*gpu.BindGroupLayout{r.skyLayout},
		Vertex:    gpu.ShaderStage{Module: mod, EntryPoint: "vs_skybox"},
		Fragment:  gpu.ShaderStage{Module: mod, EntryPoint: "fs_skybox"},
		Primitive: gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullNone},
		DepthStencil: &gpu.DepthStencilState{
			Format:       depthFormat,
			DepthWrite:   false,
			DepthCompare: gpu.CompareLessEqual,
		},
		Targets:  []gpu.ColorTargetState{{Format: colorFormat, WriteMask: gpu.ColorWriteAll}},
		Retained: opts.Retained,
	}); err != nil {
		return fmt.Errorf("scenemodel: sky pipeline: %w", err)
	}
	return nil
}

func (r *Renderer) writeEnvUniform(dirShadow, ptShadow, spotShadow bool) {
	tm := r.built.Environment.ToneMap.Normalized()
	put := func(off int, v float32) {
		binary.LittleEndian.PutUint32(r.envScratch[off:], math.Float32bits(v))
	}
	var ds, ps, ss float32
	if dirShadow {
		ds = 1
	}
	if ptShadow {
		ps = 1
	}
	if spotShadow {
		ss = 1
	}
	put(0, tm.Exposure)
	put(4, float32(tm.Operator))
	put(8, ds)
	put(12, ps)
	put(16, ss)
	put(20, 0)
	put(24, 0)
	put(28, 0)
	r.dev.WriteBuffer(r.envBuf, 0, r.envScratch[:])
}

func (r *Renderer) writeSkyUniform(cam *scene3d.Camera) {
	if r.skyUniform == nil || cam == nil {
		return
	}
	u := render3d.NewSkyboxUniform(cam)
	tm := r.built.Environment.ToneMap.Normalized()
	off := 0
	put := func(v float32) {
		binary.LittleEndian.PutUint32(r.skyScratch[off:], math.Float32bits(v))
		off += 4
	}
	for _, f := range u.InvViewProj {
		put(f)
	}
	put(tm.Exposure)
	put(float32(tm.Operator))
	put(0)
	put(0)
	r.dev.WriteBuffer(r.skyUniform, 0, r.skyScratch[:])
}

// uploadMaterials uploads every texture the parts use, once each, and a texture bind group per material.
func (r *Renderer) uploadMaterials(cx *core.Scope, label string, retained bool) error {
	dev := r.dev
	white := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	white.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	var err error
	if r.white, err = render3d.UploadTexture(cx, dev, white, render3d.TextureUpload{Label: label + ".white", SRGB: true, Retained: retained}); err != nil {
		return fmt.Errorf("scenemodel: white texture: %w", err)
	}
	r.textures = append(r.textures, r.white)
	uploaded := make(map[*Texture]*render3d.GPUTexture)
	samplers := make(map[render3d.SamplerOptions]*render3d.GPUSampler)
	sampler := func(o render3d.SamplerOptions) (*render3d.GPUSampler, error) {
		o.Retained = retained
		if s, ok := samplers[o]; ok {
			return s, nil
		}
		s, err := render3d.NewGPUSampler(cx, dev, o)
		if err != nil {
			return nil, err
		}
		samplers[o] = s
		r.samplers = append(r.samplers, s)
		return s, nil
	}
	defaultSampler, err := sampler(render3d.SamplerLinearRepeat())
	if err != nil {
		return fmt.Errorf("scenemodel: sampler: %w", err)
	}
	r.materials = make([]*gpu.BindGroup, r.built.Materials)
	for _, d := range r.built.Drawables {
		for pi := range d.Parts {
			p := &d.Parts[pi]
			if int(p.MaterialID) >= len(r.materials) {
				return fmt.Errorf("scenemodel: object %q material ID %d is out of range for %d materials", d.Object, p.MaterialID, len(r.materials))
			}
			if r.materials[p.MaterialID] != nil {
				continue
			}
			tex, samp := r.white, defaultSampler
			if t := p.Material.BaseColorTexture; t != nil {
				gt, ok := uploaded[t]
				if !ok {
					gt, err = render3d.UploadTexture(cx, dev, t.Image, render3d.TextureUpload{Label: label + " " + t.Name, SRGB: true, Mipmaps: t.Mipmaps, Retained: retained})
					if err != nil {
						return fmt.Errorf("scenemodel: object %q texture %s: %w", d.Object, t.Name, err)
					}
					uploaded[t] = gt
					r.textures = append(r.textures, gt)
				}
				if samp, err = sampler(t.Sampler); err != nil {
					return fmt.Errorf("scenemodel: object %q texture %s sampler: %w", d.Object, t.Name, err)
				}
				tex = gt
			}
			bg, err := render3d.BindTexture(cx, dev, r.texLayout, tex, samp)
			if err != nil {
				return fmt.Errorf("scenemodel: object %q material bind group: %w", d.Object, err)
			}
			r.materials[p.MaterialID] = bg
		}
	}
	return nil
}

// uploadMeshes uploads every mesh the parts and their LOD levels use, once each.
func (r *Renderer) uploadMeshes(cx *core.Scope, label string, retained bool) error {
	r.meshes = make([]*render3d.GPUMesh, r.built.Meshes)
	upload := func(obj string, id uint32, mesh render3d.Mesh) error {
		if int(id) >= len(r.meshes) {
			return fmt.Errorf("scenemodel: object %q mesh ID %d is out of range for %d meshes", obj, id, len(r.meshes))
		}
		if r.meshes[id] != nil {
			return nil
		}
		m, err := render3d.UploadMesh(cx, r.dev, mesh, render3d.MeshUpload{Label: fmt.Sprintf("%s.mesh.%d", label, id), Retained: retained})
		if err != nil {
			return fmt.Errorf("scenemodel: object %q mesh: %w", obj, err)
		}
		r.meshes[id] = m
		return nil
	}
	for _, d := range r.built.Drawables {
		for pi := range d.Parts {
			p := &d.Parts[pi]
			if err := upload(d.Object, p.MeshID, p.Mesh); err != nil {
				return err
			}
			for li := range p.LODs {
				if err := upload(d.Object, p.LODs[li].MeshID, p.LODs[li].Mesh); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// createPipelines creates the pipeline of each DrawPipeline ID for shading s.
func (r *Renderer) createPipelines(s Shading) error {
	cx, label, opts := r.cx, r.label, r.opts
	fallback := r.clustered != nil && r.clustered.IsFallback()
	src, suffix := ShaderWGSL, ""
	if fallback {
		src, suffix = fallbackShaderWGSL, ".fallback"
	}
	if opts.LinearOutput && s == ShadingLit {
		if fallback {
			src, suffix = fallbackLinearShaderWGSL, ".fallback.linear"
		} else {
			src, suffix = linearShaderWGSL, ".linear"
		}
	} else if s == ShadingUnlit {
		if fallback {
			src, suffix = fallbackUnlitShaderWGSL, ".fallback.unlit"
		} else {
			src, suffix = unlitShaderWGSL, ".unlit"
		}
	}
	mod, err := gpu.CompileWGSL(src)
	if err != nil {
		return fmt.Errorf("scenemodel: compile %s shader: %w", s, err)
	}
	colorFormat := opts.ColorFormat
	if colorFormat == gpu.FormatUndefined {
		colorFormat = r.dev.SurfaceFormat()
	}
	depthFormat := opts.DepthFormat
	if depthFormat == gpu.FormatUndefined {
		depthFormat = gpu.FormatDepth24Plus
	}
	layouts := make([]*gpu.BindGroupLayout, BaseColorGroup+1)
	layouts[render3d.CameraGroup] = r.cam.Layout()
	layouts[render3d.LightGroup] = r.sceneLayout
	layouts[render3d.PBRDrawGroup] = r.drawLayout
	layouts[BaseColorGroup] = r.texLayout
	for id := range r.pipes[s] {
		doubleSided, blend := id&1 != 0, id&2 != 0
		cull := gpu.CullBack
		if doubleSided {
			cull = gpu.CullNone
		}
		target := gpu.ColorTargetState{Format: colorFormat, WriteMask: gpu.ColorWriteAll}
		if blend {
			target.Blend = gpu.BlendAlpha()
		}
		pipe, err := r.dev.CreatePipeline(cx, gpu.PipelineDesc{
			Label:         fmt.Sprintf("%s.pipeline.%d%s", label, id, suffix),
			Layouts:       layouts,
			Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
			Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
			VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
			Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: cull},
			DepthStencil:  &gpu.DepthStencilState{Format: depthFormat, DepthWrite: !blend, DepthCompare: gpu.CompareLess},
			Targets:       []gpu.ColorTargetState{target},
			Retained:      opts.Retained,
		})
		if err != nil {
			return fmt.Errorf("scenemodel: %s pipeline %d: %w", s, id, err)
		}
		r.pipes[s][id] = pipe
	}
	return nil
}

// SetShading makes Draw shade the parts with s from the next frame on. The first change to ShadingUnlit creates its pipelines, under the scope given to NewRenderer, and returns an error when they cannot be created, leaving the shading as it was; so does an unknown Shading or a closed renderer.
func (r *Renderer) SetShading(s Shading) error {
	if r == nil || r.cam == nil {
		return errors.New("scenemodel: SetShading on a closed renderer")
	}
	if s != ShadingLit && s != ShadingUnlit {
		return fmt.Errorf("scenemodel: unknown shading %d", uint8(s))
	}
	if r.pipes[s][0] == nil {
		if err := r.createPipelines(s); err != nil {
			for id, p := range r.pipes[s] {
				if p != nil {
					_ = p.Close()
					r.pipes[s][id] = nil
				}
			}
			return err
		}
	}
	r.shading = s
	return nil
}

// Shading returns how Draw shades the parts (SetShading).
func (r *Renderer) Shading() Shading {
	if r == nil {
		return ShadingLit
	}
	return r.shading
}

// HasSky reports whether the built scene has a gradient or HDR skybox cubemap that DrawSky draws.
func (r *Renderer) HasSky() bool {
	return r != nil && r.skyPipe != nil && r.skyGroup != nil
}

// ClearColor returns the scene environment's tone-mapped solid sky color when SkyKind is SkyColor, or fallback otherwise.
func (r *Renderer) ClearColor(fallback core.Color) core.Color {
	if r != nil && r.built != nil && r.built.Environment.SkyKind == sceneformat.SkyColor {
		return r.built.Environment.SkyClearColor
	}
	return fallback
}

// drawUniform returns the per-draw uniform of part p of drawable d at its node's current world transform.
func drawUniform(d *Drawable, p *Part) render3d.PBRDrawUniform {
	m := &p.Material
	u := render3d.NewPBRDrawUniform(d.Node.WorldMatrix(), render3d.PBRMaterial{
		BaseColor: m.BaseColor.XYZ(),
		Metallic:  m.Metallic,
		Roughness: m.Roughness,
		Emissive:  m.Emissive,
	})
	u.BaseColor.W = min(max(m.BaseColor.W, 0), 1)
	if m.AlphaMode == render3d.AlphaMask {
		// A cutoff of 0 keeps every fragment, as glTF's comparison alpha < cutoff does.
		u.PBRParams.Z = max(m.AlphaCutoff, 0)
	}
	if m.DoubleSided {
		u.PBRParams.W = 1
	}
	if m.AlphaMode == render3d.AlphaBlend {
		u.Emissive.W = 1
	}
	return u
}

// SetViewport sets the render target size in pixels used to map fragment coordinates to clustered light tiles. Call it before PrepareScene when rendering into a target whose dimensions differ from dev.SurfaceSize(); non-positive or non-finite values reset to using dev.SurfaceSize().
func (r *Renderer) SetViewport(width, height float32) {
	if r == nil {
		return
	}
	if width > 0 && height > 0 && !math.IsNaN(float64(width)) && !math.IsNaN(float64(height)) && !math.IsInf(float64(width), 0) && !math.IsInf(float64(height), 0) {
		r.viewportW, r.viewportH = width, height
		return
	}
	r.viewportW, r.viewportH = 0, 0
}

// ClusteredLights returns the underlying render3d.ClusteredLightSystem owned by r, for inspecting whether the clustered or fallback path is active and reading per-frame cluster stats.
func (r *Renderer) ClusteredLights() *render3d.ClusteredLightSystem {
	if r == nil {
		return nil
	}
	return r.clustered
}

func (r *Renderer) effectiveViewport() (float32, float32) {
	if r.viewportW > 0 && r.viewportH > 0 {
		return r.viewportW, r.viewportH
	}
	if r.dev != nil {
		if w, h := r.dev.SurfaceSize(); w > 0 && h > 0 {
			return float32(w), float32(h)
		}
	}
	return 1280, 720
}

// Prepare writes cam and lights to their buffers and each visible part's world transform to its draw buffer, and plans the frame with the renderer's render3d.Queue: frustum culling and draw order. Call it once a frame before the render pass that Draw records into. It returns the plan's statistics, and an error when lights fails render3d's validation or the renderer is closed.
func (r *Renderer) Prepare(cam *scene3d.Camera, lights render3d.LightRig) (render3d.RenderStats, error) {
	return r.prepare(cam, lights, nil, false)
}

// PrepareScene prepares the frame using the built scene's own 3D lights (Built.SceneLights, lighting unshadowed point and spot lights through render3d.ClusteredLightSystem), shadow-casting lights (Built.ShadowLights) and environment (sky, IBL, fog, tone mapping, exposure). Call RecordShadows before the main render pass when any light casts shadows, then DrawSky and Draw inside the pass.
func (r *Renderer) PrepareScene(cam *scene3d.Camera) (render3d.RenderStats, error) {
	if r == nil || r.built == nil {
		return render3d.RenderStats{}, errors.New("scenemodel: PrepareScene on a nil renderer")
	}
	rig, local, err := r.built.appendSceneLights(r.clusterLights[:0])
	r.clusterLights = local
	if err != nil {
		return render3d.RenderStats{}, fmt.Errorf("scenemodel: lights: %w", err)
	}
	return r.prepare(cam, rig, local, true)
}

func (r *Renderer) prepare(cam *scene3d.Camera, lights render3d.LightRig, localLights []render3d.ClusterLight, useSceneShadows bool) (render3d.RenderStats, error) {
	if r == nil || cam == nil {
		return render3d.RenderStats{}, errors.New("scenemodel: Prepare needs a renderer and a camera")
	}
	if r.cam == nil || r.clustered == nil {
		return render3d.RenderStats{}, errors.New("scenemodel: Prepare on a closed renderer")
	}
	vpW, vpH := r.effectiveViewport()
	if _, err := r.clustered.UpdateCamera(r.dev, cam, vpW, vpH, lights, localLights); err != nil {
		return render3d.RenderStats{}, fmt.Errorf("scenemodel: lights: %w", err)
	}
	r.cam.WriteCamera(cam)
	r.writeSkyUniform(cam)

	r.dirShadowActive, r.ptShadowActive, r.spotShadowActive = false, false, false
	if useSceneShadows && r.shading != ShadingUnlit {
		dirL, ptL, spotL := r.built.ShadowLights()
		if dirL != nil && r.dirShadow != nil {
			if bounds, ok := r.built.WorldBounds(); ok {
				cfg := render3d.DefaultDirectionalShadowConfig()
				cfg.Resolution = r.dirShadow.Resolution()
				cfg.Direction = dirL.Direction
				if u, err := r.dirShadow.UpdateBounds(r.dev, cfg, bounds); err == nil {
					r.dirShadowScratch = render3d.AppendShadowUniforms(r.dirShadowScratch[:0], u)
					r.dev.WriteBuffer(r.dirShadowBuf, 0, r.dirShadowScratch)
					r.dirShadowActive = true
				}
			}
		}
		if spotL != nil && r.spotShadow != nil {
			sl := *spotL
			if sl.OuterConeAngle > render3d.MaxSpotShadowConeAngle {
				sl.OuterConeAngle = render3d.MaxSpotShadowConeAngle
				if sl.InnerConeAngle >= sl.OuterConeAngle {
					sl.InnerConeAngle = max(sl.OuterConeAngle-0.01, 0)
				}
			}
			if err := r.spotShadow.Update(r.dev, sl); err == nil {
				u := r.spotShadow.Uniforms()
				r.dev.WriteBuffer(r.spotShadowBuf, 0, u.Encode(r.spotShadowScratch[:]))
				r.spotShadowActive = true
			}
		}
		if ptL != nil && r.pointShadow != nil {
			if err := r.pointShadow.Update(r.dev, *ptL); err == nil {
				u := r.pointShadow.Uniforms()
				r.dev.WriteBuffer(r.ptShadowBuf, 0, u.Encode(r.ptShadowScratch[:]))
				r.ptShadowActive = true
			}
		}
	}
	r.writeEnvUniform(r.dirShadowActive, r.ptShadowActive, r.spotShadowActive)

	var probeSet render3d.ReflectionProbeSet
	if !r.opts.DisableProbes && len(r.built.ReflectionProbes) > 0 {
		probeSet.Probes = make([]*render3d.ReflectionProbe, 0, len(r.built.ReflectionProbes))
		for i, rp := range r.built.ReflectionProbes {
			if rp == nil || i >= len(r.probeCubes) || r.probeCubes[i] == nil {
				continue
			}
			probeSet.Probes = append(probeSet.Probes, rp.RuntimeProbe(r.probeCubes[i]))
		}
	}

	envIBL := r.built.Environment.IBLIntensity
	skyCube := r.iblCube
	if envIBL <= 0 {
		skyCube = nil
	}
	skyTex := r.dummyCube.Texture()
	if skyCube != nil && skyCube.Valid() {
		skyTex = skyCube.Texture()
	}

	r.items = r.built.AppendDrawItems(r.items[:0])
	for i := range r.items {
		id := r.items[i].ID
		di, pi := int(id>>32), int(uint32(id))
		d, p, _ := r.built.Part(id)
		pd := &r.draws[di][pi]
		u := drawUniform(d, p)
		r.dev.WriteBuffer(pd.drawBuf, 0, u.Encode(r.drawScratch[:]))

		worldCenter := d.Node.WorldMatrix().TransformPoint(p.Mesh.Bounds().Center())
		blend := render3d.ProbeBlendResult{SkyWeight: 1}
		var sh render3d.IrradianceSH
		hasLPG := false
		if !r.opts.DisableProbes {
			if len(probeSet.Probes) > 0 {
				blend = probeSet.SelectReflectionProbes(worldCenter)
			}
			sh, hasLPG = r.built.SampleLightProbes(worldCenter)
		}
		if !hasLPG {
			sh = r.built.Environment.IBLIrradiance.Scale(envIBL)
		}
		pu := render3d.NewProbeIBLUniform(sh, blend, skyCube, 1)
		pu.SkyWeight *= envIBL
		if !hasLPG && envIBL <= 0 && pu.Probe0Weight <= 0 && pu.Probe1Weight <= 0 {
			pu.Intensity = 0
		}
		tex0 := r.dummyCube.Texture()
		if blend.Primary != nil && blend.Primary.GPUCubemap != nil && blend.Primary.GPUCubemap.Valid() && pu.Probe0Weight > 0 {
			tex0 = blend.Primary.GPUCubemap.Texture()
		}
		tex1 := r.dummyCube.Texture()
		if blend.Secondary != nil && blend.Secondary.GPUCubemap != nil && blend.Secondary.GPUCubemap.Valid() && pu.Probe1Weight > 0 {
			tex1 = blend.Secondary.GPUCubemap.Texture()
		}
		if err := r.rebindPartDraw(pd, tex0, tex1, skyTex); err != nil {
			return render3d.RenderStats{}, fmt.Errorf("scenemodel: bind probes for %q: %w", d.Object, err)
		}
		r.dev.WriteBuffer(pd.probeBuf, 0, pu.Encode(r.probeScratch[:]))
	}
	viewProj := cam.ViewProj()
	var occ *render3d.OcclusionBuffer
	if r.occ != nil {
		r.occluders = r.built.AppendOccluders(r.occluders[:0])
		r.occ.Clear()
		if len(r.occluders) > 0 {
			r.occ.Rasterize(viewProj, r.occluders...)
			r.occ.BuildPyramid()
			occ = r.occ
		}
	}
	r.plan = r.queue.BuildWithOcclusion(viewProj, cam.Pos, r.items, occ)
	return r.plan.Stats, nil
}

// Plan returns the render3d.RenderPlan from the most recent Prepare call, allowing callers to inspect selected LOD indices and culling statistics.
func (r *Renderer) Plan() render3d.RenderPlan {
	if r == nil {
		return render3d.RenderPlan{}
	}
	return r.plan
}

// RecordShadows records the depth shadow passes for any active shadow-casting lights prepared by PrepareScene into target (a *gpu.Frame or *gpu.Encoder). Call it before BeginRenderPass for the main scene pass. It does nothing when no shadows are active or in ShadingUnlit.
func (r *Renderer) RecordShadows(target render3d.ShadowPassTarget) {
	if r == nil || target == nil || r.cam == nil || r.shading == ShadingUnlit {
		return
	}
	if r.dirShadowActive && r.dirShadow != nil {
		if pass := r.dirShadow.BeginShadowPass(r.dev, target); pass != nil {
			for i := range r.items {
				if r.items[i].Blend == render3d.BlendAlphaBlend {
					continue
				}
				r.dirShadow.DrawCaster(pass, r.meshes[r.items[i].MeshID], r.items[i].Transform)
			}
			pass.End()
		}
	}
	if r.spotShadowActive && r.spotShadow != nil {
		if pass := r.spotShadow.BeginShadowPass(r.dev, target); pass != nil {
			for i := range r.items {
				if r.items[i].Blend == render3d.BlendAlphaBlend {
					continue
				}
				r.spotShadow.DrawCaster(pass, r.meshes[r.items[i].MeshID], r.items[i].Transform)
			}
			pass.End()
		}
	}
	if r.ptShadowActive && r.pointShadow != nil {
		for face := range gpu.CubeFace(render3d.PointShadowFaceCount) {
			if pass := r.pointShadow.BeginFacePass(r.dev, target, face); pass != nil {
				for i := range r.items {
					if r.items[i].Blend == render3d.BlendAlphaBlend {
						continue
					}
					r.pointShadow.DrawCaster(pass, r.meshes[r.items[i].MeshID], r.items[i].Transform)
				}
				pass.End()
			}
		}
	}
}

// DrawSky draws the scene's gradient or HDR skybox cubemap into pass using the scene's exposure and tone mapping operator. Call it before Draw inside the render pass; it does nothing when the scene has no cubemap sky.
func (r *Renderer) DrawSky(pass *gpu.RenderPass) {
	if r == nil || pass == nil || r.skyPipe == nil || r.skyGroup == nil {
		return
	}
	pass.SetPipeline(r.skyPipe)
	pass.SetBindGroup(0, r.skyGroup)
	pass.Draw(3, 0)
}

// Draw records the parts Prepare planned into pass: the opaque and cutout ones first, then the blended ones back to front. pass must have a color attachment of RendererOptions.ColorFormat and a depth attachment of its DepthFormat. It records nothing once the renderer is closed.
func (r *Renderer) Draw(pass *gpu.RenderPass) {
	if r == nil || pass == nil || r.cam == nil {
		return
	}
	pass.SetBindGroup(render3d.CameraGroup, r.cam.BindGroup())
	pass.SetBindGroup(render3d.LightGroup, r.sceneGroup)
	current := -1
	draw := func(rd render3d.ResolvedDraw) {
		di, pi := int(rd.ID>>32), int(uint32(rd.ID))
		if int(rd.PipelineID) != current {
			current = int(rd.PipelineID)
			pass.SetPipeline(r.pipes[r.shading][current])
		}
		pass.SetBindGroup(render3d.PBRDrawGroup, r.draws[di][pi].group)
		pass.SetBindGroup(BaseColorGroup, r.materials[rd.MaterialID])
		r.meshes[rd.MeshID].Draw(pass)
	}
	for _, rd := range r.plan.Opaque {
		draw(rd)
	}
	for _, rd := range r.plan.Transparent {
		draw(rd)
	}
}

// Close releases every GPU resource of r now instead of when its scope is disposed. It is safe to call on nil and more than once; after it, Prepare returns an error and Draw records nothing.
func (r *Renderer) Close() error {
	if r == nil {
		return nil
	}
	var err error
	for _, row := range r.draws {
		for i := range row {
			if row[i].group != nil {
				err = errors.Join(err, row[i].group.Close())
			}
			if row[i].drawBuf != nil {
				err = errors.Join(err, row[i].drawBuf.Close())
			}
			if row[i].probeBuf != nil {
				err = errors.Join(err, row[i].probeBuf.Close())
			}
		}
	}
	for _, m := range r.meshes {
		if m != nil {
			err = errors.Join(err, m.Close())
		}
	}
	for _, bg := range r.materials {
		if bg != nil {
			err = errors.Join(err, bg.Close())
		}
	}
	for _, t := range r.textures {
		err = errors.Join(err, t.Close())
	}
	for _, s := range r.samplers {
		err = errors.Join(err, s.Close())
	}
	for _, set := range r.pipes {
		for _, p := range set {
			if p != nil {
				err = errors.Join(err, p.Close())
			}
		}
	}
	for _, p := range []*gpu.Pipeline{r.skyPipe} {
		if p != nil {
			err = errors.Join(err, p.Close())
		}
	}
	for _, bg := range []*gpu.BindGroup{r.skyGroup, r.sceneGroup} {
		if bg != nil {
			err = errors.Join(err, bg.Close())
		}
	}
	for _, l := range []*gpu.BindGroupLayout{r.drawLayout, r.texLayout, r.sceneLayout, r.skyLayout} {
		if l != nil {
			err = errors.Join(err, l.Close())
		}
	}
	for _, b := range []*gpu.Buffer{r.fogBuf, r.envBuf, r.dirShadowBuf, r.spotShadowBuf, r.ptShadowBuf, r.skyUniform} {
		if b != nil {
			err = errors.Join(err, b.Close())
		}
	}
	if r.clustered != nil {
		err = errors.Join(err, r.clustered.Close())
	}
	for _, c := range r.probeCubes {
		if c != nil {
			err = errors.Join(err, c.Close())
		}
	}
	for _, c := range []*render3d.GPUCubemap{r.skyCube, r.iblCube, r.dummyCube} {
		if c != nil {
			err = errors.Join(err, c.Close())
		}
	}
	if r.dirShadow != nil {
		err = errors.Join(err, r.dirShadow.Close())
	}
	if r.spotShadow != nil {
		err = errors.Join(err, r.spotShadow.Close())
	}
	if r.pointShadow != nil {
		err = errors.Join(err, r.pointShadow.Close())
	}
	if r.dummyDepth != nil {
		err = errors.Join(err, r.dummyDepth.Close())
	}
	if r.shadowSampler != nil {
		err = errors.Join(err, r.shadowSampler.Close())
	}
	if r.cam != nil {
		err = errors.Join(err, r.cam.Close())
	}
	*r = Renderer{}
	return err
}
