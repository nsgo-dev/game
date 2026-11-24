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

const (
	// LightGroup is the bind group index the 3D shaders in this repository reserve for scene lights:
	// [GPULightBuffer.BindGroup] goes in this slot of lit 3D pipelines' Layouts, and [LightWGSL] declares it there.
	LightGroup = 1

	// PBRDrawGroup is the bind group index [LitPBRWGSL] reserves for the per-draw transform and
	// [PBRMaterial] uniform block ([PBRDrawBuffer.BindGroup]).
	PBRDrawGroup = 2

	// MaxDirectionalLights is the maximum number of directional lights a [LightRig] encodes into the GPU uniform block.
	MaxDirectionalLights = 4

	// MaxPointLights is the maximum number of point lights a [LightRig] encodes into the GPU uniform block.
	MaxPointLights = 16

	// LightUniformSize is the byte size of the light uniform block that [LightRig.AppendUniformData] and
	// [LightUniforms.Encode] write and [LightWGSL] declares: ambient (16) + counts (16) +
	// MaxDirectionalLights*32 + MaxPointLights*32 = 672 bytes.
	LightUniformSize = 16 + 16 + MaxDirectionalLights*32 + MaxPointLights*32

	// PBRDrawUniformSize is the byte size of the per-draw PBR uniform block that [PBRDrawUniform.Encode]
	// writes and [LitPBRWGSL] declares: model (64) + normal_matrix (64) + base_color (16) +
	// pbr_params (16) + emissive (16) = 176 bytes.
	PBRDrawUniformSize = 64 + 64 + 16 + 16 + 16

	minPerceptualRoughness = 0.045
	dielectricF0           = 0.04
	minPointLightDistSq    = 1e-4
	minLightDirLenSq       = 1e-12
)

// ErrInvalidLight is wrapped by every error light validation or upload returns for malformed light definitions.
var ErrInvalidLight = errors.New("render3d: invalid light")

// LightWGSL declares the scene lighting uniform block at @group(1) @binding(0) as the variable lights,
// along with portable glTF 2.0 metallic-roughness PBR shading helpers:
//
//   - point_light_attenuation(dist_sq, light_range) -> f32
//   - pbr_brdf(n, v, l, base_color, metallic, roughness) -> vec3<f32>
//   - shade_pbr(world_pos, normal, view_dir, base_color, metallic, roughness) -> vec3<f32>
//   - shade_pbr_shadowed(world_pos, normal, view_dir, base_color, metallic, roughness, shadow_vis) -> vec3<f32>
//   - linear_to_srgb_channel(c) -> f32
//   - linear_to_srgb(rgb) -> vec3<f32>
//
// Prepend [CameraWGSL] and LightWGSL to any 3D shader that reads scene lights; the block's layout
// matches [LightUniforms.Encode] and [LightRig.AppendUniformData] byte for byte, and the shading math
// matches [EvaluatePBR], [ShadePoint], [ShadePointShadowed], [PointLightAttenuation], and [LinearToSRGB].
const LightWGSL = `struct DirectionalLightUniform {
	to_light: vec4<f32>,
	color: vec4<f32>,
}

struct PointLightUniform {
	position_range: vec4<f32>,
	color: vec4<f32>,
}

struct LightRigUniform {
	ambient: vec4<f32>,
	counts: vec4<u32>,
	directional: array<DirectionalLightUniform, 4>,
	point: array<PointLightUniform, 16>,
}

@group(1) @binding(0) var<uniform> lights: LightRigUniform;

fn point_light_attenuation(dist_sq: f32, light_range: f32) -> f32 {
	let inv_d2 = 1.0 / max(dist_sq, 1e-4);
	if (light_range > 0.0) {
		let ratio = sqrt(max(dist_sq, 0.0)) / light_range;
		let ratio2 = ratio * ratio;
		let window = clamp(1.0 - ratio2 * ratio2, 0.0, 1.0);
		return inv_d2 * window * window;
	}
	return inv_d2;
}

fn pbr_brdf(n: vec3<f32>, v: vec3<f32>, l: vec3<f32>, base_color: vec3<f32>, metallic: f32, roughness: f32) -> vec3<f32> {
	let ndotl = dot(n, l);
	let ndotv_raw = dot(n, v);
	if (ndotl <= 0.0 || ndotv_raw <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let ndotv = max(ndotv_raw, 1e-4);
	let m = clamp(metallic, 0.0, 1.0);
	let r = clamp(roughness, 0.045, 1.0);
	let a = r * r;
	let a2 = a * a;
	let pi = 3.14159265;
	let f0 = mix(vec3<f32>(0.04, 0.04, 0.04), base_color, m);
	let h_raw = v + l;
	let h_len2 = dot(h_raw, h_raw);
	if (h_len2 <= 1e-8) {
		return (vec3<f32>(1.0, 1.0, 1.0) - f0) * (1.0 - m) * base_color / pi;
	}
	let h = h_raw / sqrt(h_len2);
	let ndoth = max(dot(n, h), 0.0);
	let vdoth = max(dot(v, h), 0.0);

	let denom = ndoth * ndoth * (a2 - 1.0) + 1.0;
	let d = a2 / (pi * denom * denom);

	let gv = ndotl * sqrt(ndotv * ndotv * (1.0 - a2) + a2);
	let gl = ndotv * sqrt(ndotl * ndotl * (1.0 - a2) + a2);
	let vis = 0.5 / max(gv + gl, 1e-6);

	let t = clamp(1.0 - vdoth, 0.0, 1.0);
	let t2 = t * t;
	let f = f0 + (vec3<f32>(1.0, 1.0, 1.0) - f0) * (t2 * t2 * t);

	let spec = d * vis * f;
	let diff = (vec3<f32>(1.0, 1.0, 1.0) - f) * (1.0 - m) * base_color / pi;
	return diff + spec;
}

fn shade_pbr(world_pos: vec3<f32>, normal: vec3<f32>, view_dir: vec3<f32>, base_color: vec3<f32>, metallic: f32, roughness: f32) -> vec3<f32> {
	return shade_pbr_shadowed(world_pos, normal, view_dir, base_color, metallic, roughness, 1.0);
}

fn shade_pbr_shadowed(world_pos: vec3<f32>, normal: vec3<f32>, view_dir: vec3<f32>, base_color: vec3<f32>, metallic: f32, roughness: f32, shadow_vis: f32) -> vec3<f32> {
	let n = normalize(normal);
	let v = normalize(view_dir);
	var lo = lights.ambient.xyz * base_color;
	let dir_count = lights.counts.x;
	for (var di: u32 = 0u; di < 4u; di = di + 1u) {
		if (di >= dir_count) {
			break;
		}
		let dl = lights.directional[di];
		let l = dl.to_light.xyz;
		let ndotl = max(dot(n, l), 0.0);
		var vis = 1.0;
		if (di == 0u) {
			vis = shadow_vis;
		}
		if (ndotl > 0.0) {
			lo = lo + pbr_brdf(n, v, l, base_color, metallic, roughness) * dl.color.xyz * (ndotl * vis);
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
	return lo;
}

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
`

// LitPBRWGSL is a complete, portable metallic-roughness PBR WGSL module built on [CameraWGSL],
// [LightWGSL], [MeshVertexLayout], and a per-draw [PBRDrawUniform] at [PBRDrawGroup]. Entry points
// are vs_main and fs_main; use [NewLitPBRPipeline] to create a pipeline from it. [LitPBRFogWGSL] is
// the same module with fog.
const LitPBRWGSL = litPBRPreludeWGSL + litPBRFragmentHeadWGSL + `	return vec4<f32>(linear_to_srgb(lit), pbr_draw.base_color.a);
}
`

// litPBRPreludeWGSL holds the declarations and vertex stage [LitPBRWGSL] and [LitPBRFogWGSL] share.
const litPBRPreludeWGSL = litPBRDeclarationsWGSL + `
struct PBRVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

@vertex
fn vs_main(in: PBRVertexIn) -> PBRVertexOut {
	var out: PBRVertexOut;
	let world = pbr_draw.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world;
	out.world_pos = world.xyz;
	out.world_normal = (pbr_draw.normal_matrix * vec4<f32>(in.normal, 0.0)).xyz;
	return out;
}
`

// litPBRDeclarationsWGSL declares the camera, lights, per-draw uniform and vertex output that the plain and skinned lit PBR vertex stages share.
const litPBRDeclarationsWGSL = CameraWGSL + LightWGSL + `
struct PBRDrawUniform {
	model: mat4x4<f32>,
	normal_matrix: mat4x4<f32>,
	base_color: vec4<f32>,
	pbr_params: vec4<f32>,
	emissive: vec4<f32>,
}

@group(2) @binding(0) var<uniform> pbr_draw: PBRDrawUniform;

struct PBRVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
}
`

// litPBRFragmentHeadWGSL opens fs_main and computes the lit linear color lit; [LitPBRWGSL] and [LitPBRFogWGSL] each close it with their own return.
const litPBRFragmentHeadWGSL = `
@fragment
fn fs_main(in: PBRVertexOut) -> @location(0) vec4<f32> {
	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let lit = shade_pbr(
		in.world_pos,
		in.world_normal,
		view_dir,
		pbr_draw.base_color.rgb,
		pbr_draw.pbr_params.x,
		pbr_draw.pbr_params.y,
	) + pbr_draw.emissive.rgb;
`

// DirectionalLight is an infinitely distant light source whose parallel rays travel along Direction.
//
// Direction is the world-space travel direction of light rays (for example, (0, -1, 0) shines straight
// down onto a horizontal +Y surface); it does not need to be unit length and is normalized when shaded
// or packed for the GPU. Color is linear RGB tint (non-negative finite channels), and Intensity scales
// Color to produce linear illuminance.
type DirectionalLight struct {
	Direction math3d.Vec3
	Color     math3d.Vec3
	Intensity float32
}

// Validate reports whether l has a finite non-zero direction, finite non-negative color channels,
// and a finite non-negative intensity. Every error wraps [ErrInvalidLight].
func (l DirectionalLight) Validate() error {
	if !finite3(l.Direction) || l.Direction.LenSq() <= minLightDirLenSq {
		return fmt.Errorf("%w: directional light direction must be finite and non-zero", ErrInvalidLight)
	}
	if !validLinearColor(l.Color) {
		return fmt.Errorf("%w: directional light color must be finite and non-negative", ErrInvalidLight)
	}
	if !finite(l.Intensity) || l.Intensity < 0 {
		return fmt.Errorf("%w: directional light intensity must be finite and non-negative", ErrInvalidLight)
	}
	return nil
}

// PointLight is an omnidirectional punctual emitter at Position with inverse-square falloff and an
// optional glTF KHR_lights_punctual smooth cutoff Range.
//
// Color is linear RGB tint (non-negative finite channels) and Intensity scales Color to produce linear
// radiant intensity. When Range > 0, attenuation is windowed by max(min(1 - (d/Range)^4, 1), 0)^2 / d^2
// so contribution smoothly reaches zero at Range; when Range == 0, pure inverse-square falloff 1 / d^2
// is used.
type PointLight struct {
	Position  math3d.Vec3
	Color     math3d.Vec3
	Intensity float32
	Range     float32
}

// Validate reports whether l has a finite position, finite non-negative color channels, and finite
// non-negative intensity and range. Every error wraps [ErrInvalidLight].
func (l PointLight) Validate() error {
	if !finite3(l.Position) {
		return fmt.Errorf("%w: point light position must be finite", ErrInvalidLight)
	}
	if !validLinearColor(l.Color) {
		return fmt.Errorf("%w: point light color must be finite and non-negative", ErrInvalidLight)
	}
	if !finite(l.Intensity) || l.Intensity < 0 {
		return fmt.Errorf("%w: point light intensity must be finite and non-negative", ErrInvalidLight)
	}
	if !finite(l.Range) || l.Range < 0 {
		return fmt.Errorf("%w: point light range must be finite and non-negative", ErrInvalidLight)
	}
	return nil
}

// AmbientLight is a uniform isotropic fill light applied across surfaces in linear RGB space.
type AmbientLight struct {
	Color     math3d.Vec3
	Intensity float32
}

// Validate reports whether l has finite non-negative color channels and intensity. Every error wraps
// [ErrInvalidLight].
func (l AmbientLight) Validate() error {
	if !validLinearColor(l.Color) {
		return fmt.Errorf("%w: ambient light color must be finite and non-negative", ErrInvalidLight)
	}
	if !finite(l.Intensity) || l.Intensity < 0 {
		return fmt.Errorf("%w: ambient light intensity must be finite and non-negative", ErrInvalidLight)
	}
	return nil
}

// LightRig holds the active lights for a 3D scene: an ambient fill, up to [MaxDirectionalLights]
// directional lights, and up to [MaxPointLights] point lights.
type LightRig struct {
	Ambient     AmbientLight
	Directional []DirectionalLight
	Point       []PointLight
}

// Validate checks ambient, directional, and point lights and enforces [MaxDirectionalLights] and
// [MaxPointLights]. Every error wraps [ErrInvalidLight].
func (r LightRig) Validate() error {
	if err := r.Ambient.Validate(); err != nil {
		return err
	}
	if len(r.Directional) > MaxDirectionalLights {
		return fmt.Errorf("%w: %d directional lights exceed MaxDirectionalLights (%d)", ErrInvalidLight, len(r.Directional), MaxDirectionalLights)
	}
	for i, dl := range r.Directional {
		if err := dl.Validate(); err != nil {
			return fmt.Errorf("%w: directional[%d]: %w", ErrInvalidLight, i, err)
		}
	}
	if len(r.Point) > MaxPointLights {
		return fmt.Errorf("%w: %d point lights exceed MaxPointLights (%d)", ErrInvalidLight, len(r.Point), MaxPointLights)
	}
	for i, pl := range r.Point {
		if err := pl.Validate(); err != nil {
			return fmt.Errorf("%w: point[%d]: %w", ErrInvalidLight, i, err)
		}
	}
	return nil
}

// UniformDataSize returns the byte length [LightRig.AppendUniformData] appends ([LightUniformSize]).
func (r LightRig) UniformDataSize() int { return LightUniformSize }

// AppendUniformData appends r's lights to dst in the little-endian std140/WGSL layout of [LightWGSL]
// and returns the extended slice. It allocates only when dst lacks capacity for [LightUniformSize]
// more bytes. Call [LightRig.Validate] first: lights beyond [MaxDirectionalLights] or [MaxPointLights]
// are omitted, and a directional light with a degenerate direction writes zero radiance.
func (r LightRig) AppendUniformData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, LightUniformSize)
	u := packLightUniforms(r)
	u.Encode(dst[start:])
	return dst
}

// GPUDirectionalLight is the 32-byte GPU uniform representation of one [DirectionalLight].
type GPUDirectionalLight struct {
	// ToLight holds the unit vector pointing from the surface toward the light (-normalize(Direction)) in xyz.
	ToLight math3d.Vec4
	// Color holds premultiplied linear radiance (Color * Intensity) in rgb and Intensity in w.
	Color math3d.Vec4
}

// GPUPointLight is the 32-byte GPU uniform representation of one [PointLight].
type GPUPointLight struct {
	// PositionRange holds world-space Position in xyz and Range in w (0 for unbounded falloff).
	PositionRange math3d.Vec4
	// Color holds premultiplied linear radiant intensity (Color * Intensity) in rgb and Intensity in w.
	Color math3d.Vec4
}

// LightUniforms is the fixed-size, 16-byte-aligned GPU uniform payload matching [LightWGSL].
type LightUniforms struct {
	// Ambient holds premultiplied linear ambient radiance (Color * Intensity) in rgb and Intensity in w.
	Ambient          math3d.Vec4
	DirectionalCount uint32
	PointCount       uint32
	Directional      [MaxDirectionalLights]GPUDirectionalLight
	Point            [MaxPointLights]GPUPointLight
}

// NewLightUniforms validates rig and packs it into a GPU-ready [LightUniforms].
func NewLightUniforms(rig LightRig) (LightUniforms, error) {
	if err := rig.Validate(); err != nil {
		return LightUniforms{}, err
	}
	return packLightUniforms(rig), nil
}

func packLightUniforms(rig LightRig) LightUniforms {
	var u LightUniforms
	amb := rig.Ambient.Color.Scale(rig.Ambient.Intensity)
	u.Ambient = math3d.Vec4{X: amb.X, Y: amb.Y, Z: amb.Z, W: rig.Ambient.Intensity}

	nDir := min(len(rig.Directional), MaxDirectionalLights)
	u.DirectionalCount = uint32(nDir)
	for i := range nDir {
		dl := rig.Directional[i]
		lenSq := dl.Direction.LenSq()
		if lenSq <= minLightDirLenSq {
			continue
		}
		invLen := float32(1.0 / math.Sqrt(float64(lenSq)))
		toLight := dl.Direction.Scale(-invLen)
		col := dl.Color.Scale(dl.Intensity)
		u.Directional[i] = GPUDirectionalLight{
			ToLight: math3d.Vec4{X: toLight.X, Y: toLight.Y, Z: toLight.Z},
			Color:   math3d.Vec4{X: col.X, Y: col.Y, Z: col.Z, W: dl.Intensity},
		}
	}

	nPt := min(len(rig.Point), MaxPointLights)
	u.PointCount = uint32(nPt)
	for i := range nPt {
		pl := rig.Point[i]
		col := pl.Color.Scale(pl.Intensity)
		u.Point[i] = GPUPointLight{
			PositionRange: math3d.Vec4{X: pl.Position.X, Y: pl.Position.Y, Z: pl.Position.Z, W: pl.Range},
			Color:         math3d.Vec4{X: col.X, Y: col.Y, Z: col.Z, W: pl.Intensity},
		}
	}
	return u
}

// Encode writes u into dst as the little-endian std140/WGSL block [LightWGSL] declares and returns
// dst[:LightUniformSize] without allocating. It panics if len(dst) < LightUniformSize, whatever dst's
// capacity.
func (u *LightUniforms) Encode(dst []byte) []byte {
	if len(dst) < LightUniformSize {
		panic("render3d: LightUniforms.Encode: dst shorter than LightUniformSize")
	}
	dst = dst[:LightUniformSize]
	putVec4(dst[0:], u.Ambient)
	binary.LittleEndian.PutUint32(dst[16:], u.DirectionalCount)
	binary.LittleEndian.PutUint32(dst[20:], u.PointCount)
	binary.LittleEndian.PutUint32(dst[24:], 0)
	binary.LittleEndian.PutUint32(dst[28:], 0)
	off := 32
	for i := range MaxDirectionalLights {
		putVec4(dst[off:], u.Directional[i].ToLight)
		putVec4(dst[off+16:], u.Directional[i].Color)
		off += 32
	}
	for i := range MaxPointLights {
		putVec4(dst[off:], u.Point[i].PositionRange)
		putVec4(dst[off+16:], u.Point[i].Color)
		off += 32
	}
	return dst
}

// LightBufferOptions configures [NewLightBuffer].
type LightBufferOptions struct {
	// Label names the buffer, bind group layout and bind group in GPU capture tools.
	Label string
	// Retained keeps a CPU copy of the uniform buffer so NSGo automatically recreates the buffer,
	// its bind group layout, and its bind group after a device loss (see [gpu.BufferDesc].Retained).
	Retained bool
}

// GPULightBuffer holds a scene's [LightRig] uniform buffer on a GPU device together with the bind group
// layout and bind group that expose it to shaders at [LightGroup].
//
// Create it once when the device is ready, call [GPULightBuffer.Update] whenever the scene lights change,
// and bind [GPULightBuffer.BindGroup] at [LightGroup] (or call [GPULightBuffer.Bind]). Pipelines list
// [GPULightBuffer.Layout] at [LightGroup] in their PipelineDesc.Layouts. Its resources belong to the
// scope passed to [NewLightBuffer] and are released with it, or earlier by Close.
type GPULightBuffer struct {
	dev     *gpu.Device
	buf     *gpu.Buffer
	layout  *gpu.BindGroupLayout
	group   *gpu.BindGroup
	scratch [LightUniformSize]byte
}

// NewLightBuffer validates initial and creates a light uniform buffer, bind group layout, and bind group
// on dev, scoped to cx (nil for resources closed manually). A malformed rig returns an error wrapping
// [ErrInvalidLight] before any GPU resource is created.
func NewLightBuffer(cx *core.Scope, dev *gpu.Device, initial LightRig, opts LightBufferOptions) (*GPULightBuffer, error) {
	u, err := NewLightUniforms(initial)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.lights"
	}
	b := &GPULightBuffer{dev: dev}
	u.Encode(b.scratch[:])

	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label,
		Size:     LightUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     b.scratch[:],
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label,
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageVertex | gpu.StageFragment,
			Type:       gpu.BindingUniform,
		}},
	})
	if err != nil {
		_ = buf.Close()
		return nil, err
	}
	group, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:   label,
		Layout:  layout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: buf, Size: LightUniformSize}},
	})
	if err != nil {
		_ = layout.Close()
		_ = buf.Close()
		return nil, err
	}
	b.buf = buf
	b.layout = layout
	b.group = group
	return b, nil
}

// Update validates rig and uploads it into the uniform buffer without allocating into Go heap memory
// beyond the copy gpu.Device.WriteBuffer queues. Pass the current *gpu.Device (such as frame.Device())
// so updates continue working after a retained device-loss recovery, or pass nil to reuse the device
// from creation or the previous Update.
func (b *GPULightBuffer) Update(dev *gpu.Device, rig LightRig) error {
	u, err := NewLightUniforms(rig)
	if err != nil {
		return err
	}
	if !b.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: light buffer is not valid"}
	}
	target := dev
	if target == nil {
		target = b.dev
	} else {
		b.dev = dev
	}
	if target == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	target.WriteBuffer(b.buf, 0, u.Encode(b.scratch[:]))
	return nil
}

// Valid reports whether the buffer, bind group layout, and bind group are all open and live.
func (b *GPULightBuffer) Valid() bool {
	return b != nil && b.buf.Valid() && b.layout.Valid() && b.group.Valid()
}

// Buffer returns the underlying uniform buffer.
func (b *GPULightBuffer) Buffer() *gpu.Buffer {
	if b == nil {
		return nil
	}
	return b.buf
}

// Layout returns the bind group layout a pipeline lists at [LightGroup].
func (b *GPULightBuffer) Layout() *gpu.BindGroupLayout {
	if b == nil {
		return nil
	}
	return b.layout
}

// BindGroup returns the bind group a render pass sets at [LightGroup].
func (b *GPULightBuffer) BindGroup() *gpu.BindGroup {
	if b == nil {
		return nil
	}
	return b.group
}

// Bind sets the light bind group at [LightGroup] on pass when b is valid.
func (b *GPULightBuffer) Bind(pass *gpu.RenderPass) {
	if !b.Valid() {
		return
	}
	pass.SetBindGroup(LightGroup, b.group)
}

// ByteSize returns the GPU memory held by the light uniform buffer, or zero for a nil buffer.
func (b *GPULightBuffer) ByteSize() uint64 {
	if b == nil || b.buf == nil {
		return 0
	}
	return b.buf.Size()
}

// Close releases the bind group, layout, and uniform buffer now rather than with the owning scope.
// It is nil-safe and safe to call more than once.
func (b *GPULightBuffer) Close() error {
	if b == nil {
		return nil
	}
	var err error
	if b.group != nil {
		err = errors.Join(err, b.group.Close())
	}
	if b.layout != nil {
		err = errors.Join(err, b.layout.Close())
	}
	if b.buf != nil {
		err = errors.Join(err, b.buf.Close())
	}
	return err
}

// PBRMaterial holds glTF 2.0 metallic-roughness surface parameters in linear RGB space.
type PBRMaterial struct {
	// BaseColor is linear RGB albedo (dielectric diffuse reflectance or metallic F0 specular tint).
	BaseColor math3d.Vec3
	// Alpha is surface opacity in (0, 1]; zero defaults to 1 (opaque) in [NewPBRDrawUniform].
	Alpha float32
	// Metallic blends between dielectric (0) and conductor (1), clamped to [0, 1].
	Metallic float32
	// Roughness is perceptual roughness in [0, 1] (0 = smooth, 1 = fully rough). Internally clamped
	// to >= 0.045 before squaring to alpha = roughness^2 to prevent GGX singularities.
	Roughness float32
	// Emissive is optional linear RGB self-emission added after direct and ambient lighting.
	Emissive math3d.Vec3
}

// PBRDrawUniform holds the per-draw world transform, normal matrix, and [PBRMaterial] matching [LitPBRWGSL].
type PBRDrawUniform struct {
	Model        math3d.Mat4
	NormalMatrix math3d.Mat4
	BaseColor    math3d.Vec4
	PBRParams    math3d.Vec4
	Emissive     math3d.Vec4
}

// NewPBRDrawUniform constructs a [PBRDrawUniform] from an object-to-world model matrix and a [PBRMaterial],
// deriving the normal matrix via [math3d.NormalMatrix] (falling back to identity if model is singular).
func NewPBRDrawUniform(model math3d.Mat4, mat PBRMaterial) PBRDrawUniform {
	nm4 := math3d.Mat4Identity()
	if nm, ok := math3d.NormalMatrix(model); ok {
		nm4 = math3d.Mat4{
			nm[0], nm[1], nm[2], 0,
			nm[3], nm[4], nm[5], 0,
			nm[6], nm[7], nm[8], 0,
			0, 0, 0, 1,
		}
	}
	alpha := mat.Alpha
	if alpha == 0 {
		alpha = 1
	}
	return PBRDrawUniform{
		Model:        model,
		NormalMatrix: nm4,
		BaseColor:    math3d.Vec4{X: mat.BaseColor.X, Y: mat.BaseColor.Y, Z: mat.BaseColor.Z, W: clamp01(alpha)},
		PBRParams:    math3d.Vec4{X: clamp01(mat.Metallic), Y: clamp01(mat.Roughness)},
		Emissive:     math3d.Vec4{X: mat.Emissive.X, Y: mat.Emissive.Y, Z: mat.Emissive.Z},
	}
}

// Encode writes u into dst as the little-endian block [LitPBRWGSL] declares and returns
// dst[:PBRDrawUniformSize] without allocating. It panics if len(dst) < PBRDrawUniformSize.
func (u *PBRDrawUniform) Encode(dst []byte) []byte {
	if len(dst) < PBRDrawUniformSize {
		panic("render3d: PBRDrawUniform.Encode: dst shorter than PBRDrawUniformSize")
	}
	dst = dst[:PBRDrawUniformSize]
	off := 0
	for _, m := range [...]*math3d.Mat4{&u.Model, &u.NormalMatrix} {
		for _, f := range m {
			binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
			off += 4
		}
	}
	putVec4(dst[128:], u.BaseColor)
	putVec4(dst[144:], u.PBRParams)
	putVec4(dst[160:], u.Emissive)
	return dst
}

// PBRDrawBuffer holds a [PBRDrawUniform] buffer and bind group bound at [PBRDrawGroup] for [LitPBRWGSL].
type PBRDrawBuffer struct {
	dev         *gpu.Device
	buf         *gpu.Buffer
	layout      *gpu.BindGroupLayout
	ownedLayout bool
	group       *gpu.BindGroup
	scratch     [PBRDrawUniformSize]byte
}

// NewPBRDrawBuffer creates a per-draw PBR uniform buffer, bind group layout (or reuses sharedLayout if
// non-nil), and bind group on dev, scoped to cx.
func NewPBRDrawBuffer(cx *core.Scope, dev *gpu.Device, sharedLayout *gpu.BindGroupLayout, initial PBRDrawUniform, retained bool) (*PBRDrawBuffer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	b := &PBRDrawBuffer{dev: dev}
	initial.Encode(b.scratch[:])
	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    "render3d.pbr_draw",
		Size:     PBRDrawUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     b.scratch[:],
		Retained: retained,
	})
	if err != nil {
		return nil, err
	}
	layout := sharedLayout
	ownedLayout := false
	if layout == nil {
		layout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
			Label: "render3d.pbr_draw",
			Entries: []gpu.BindGroupLayoutEntry{{
				Binding:    0,
				Visibility: gpu.StageVertex | gpu.StageFragment,
				Type:       gpu.BindingUniform,
			}},
		})
		if err != nil {
			_ = buf.Close()
			return nil, err
		}
		ownedLayout = true
	}
	group, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:   "render3d.pbr_draw",
		Layout:  layout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: buf, Size: PBRDrawUniformSize}},
	})
	if err != nil {
		if ownedLayout {
			_ = layout.Close()
		}
		_ = buf.Close()
		return nil, err
	}
	b.buf = buf
	b.layout = layout
	b.ownedLayout = ownedLayout
	b.group = group
	return b, nil
}

// Write uploads u into the draw uniform buffer. Pass the current *gpu.Device or nil to reuse the stored one.
func (b *PBRDrawBuffer) Write(dev *gpu.Device, u PBRDrawUniform) {
	if b == nil || !b.buf.Valid() {
		return
	}
	target := dev
	if target == nil {
		target = b.dev
	} else {
		b.dev = dev
	}
	if target == nil {
		return
	}
	target.WriteBuffer(b.buf, 0, u.Encode(b.scratch[:]))
}

// Layout returns the bind group layout for [PBRDrawGroup].
func (b *PBRDrawBuffer) Layout() *gpu.BindGroupLayout { return b.layout }

// BindGroup returns the bind group to bind at [PBRDrawGroup].
func (b *PBRDrawBuffer) BindGroup() *gpu.BindGroup { return b.group }

// Valid reports whether the draw buffer and bind group are live.
func (b *PBRDrawBuffer) Valid() bool {
	return b != nil && b.buf.Valid() && b.group.Valid()
}

// Close releases the draw buffer's bind group, uniform buffer, and owned bind group layout (if created
// without a shared layout). It is nil-safe and safe to call more than once.
func (b *PBRDrawBuffer) Close() error {
	if b == nil {
		return nil
	}
	var err error
	if b.group != nil {
		err = errors.Join(err, b.group.Close())
	}
	if b.ownedLayout && b.layout != nil {
		err = errors.Join(err, b.layout.Close())
	}
	if b.buf != nil {
		err = errors.Join(err, b.buf.Close())
	}
	return err
}

// NewLitPBRPipeline compiles [LitPBRWGSL] and creates a back-face-culled triangle-list pipeline configured
// for [MeshVertexLayout] with bind groups [CameraGroup], [LightGroup], and [PBRDrawGroup].
// Pass depthFormat = [gpu.FormatUndefined] when rendering without a depth attachment.
func NewLitPBRPipeline(cx *core.Scope, dev *gpu.Device, camLayout, lightLayout, drawLayout *gpu.BindGroupLayout, depthFormat gpu.TextureFormat) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	layouts := make([]*gpu.BindGroupLayout, PBRDrawGroup+1)
	layouts[CameraGroup] = camLayout
	layouts[LightGroup] = lightLayout
	layouts[PBRDrawGroup] = drawLayout
	return newLitPBRPipeline(cx, dev, "render3d.lit_pbr", LitPBRWGSL, layouts, MeshVertexLayout(), dev.SurfaceFormat(), depthFormat)
}

// litPBRLinearWGSL is [LitPBRWGSL] writing the lit linear color without sRGB encoding, compiled by [NewLitPBRPipelineWithOptions] when [LitPBRPipelineOptions].LinearOutput is set.
const litPBRLinearWGSL = litPBRPreludeWGSL + litPBRFragmentHeadWGSL + `	return vec4<f32>(lit, pbr_draw.base_color.a);
}
`

// LitPBRPipelineOptions configures [NewLitPBRPipelineWithOptions].
type LitPBRPipelineOptions struct {
	// Label names the pipeline in GPU capture tools; empty picks render3d.lit_pbr, lit_pbr_fog, lit_pbr_ibl or lit_pbr_shadow with a .linear suffix for LinearOutput.
	Label string
	// ColorFormat sets the color attachment format; [gpu.FormatUndefined] defaults to dev.SurfaceFormat(). Use [HDRTarget.Format] when drawing into an [HDRTarget].
	ColorFormat gpu.TextureFormat
	// DepthFormat enables depth testing and writes when not [gpu.FormatUndefined].
	DepthFormat gpu.TextureFormat
	// FogLayout, when set, compiles [LitPBRFogWGSL] with this layout ([GPUFogBuffer.Layout]) at [FogGroup], as [NewLitPBRFogPipeline] does.
	FogLayout *gpu.BindGroupLayout
	// IBLLayout, when set, compiles [LitPBRIBLWGSL] with this layout ([GPUIBLBuffer.Layout]) at [IBLGroup], as [NewLitPBRIBLPipeline] does. Fog and IBL share bind group 3, so setting both is an error.
	IBLLayout *gpu.BindGroupLayout
	// SkinLayout, when set, compiles [LitPBRSkinnedWGSL] with this layout ([GPUSkinPalette.Layout]) at [LitPBRSkinGroup] and takes [SkinnedVertexLayout] vertices, so a [GPUSkinnedMesh] is drawn with scene lights. The vertex stage skins each vertex with the palette and then applies the per-draw model and normal matrices: use an identity model with [SkinJointMatrices] (world-space joints), or the mesh node's world matrix with mesh-local joints such as anim3d's Player.SkinMatricesForNode. Fog, IBL and skinning all use bind group 3, so SkinLayout cannot be combined with FogLayout or IBLLayout.
	SkinLayout *gpu.BindGroupLayout
	// ShadowLayout, when set, compiles [LitPBRShadowWGSL] with this layout ([DirectionalShadowMap.Layout]) at [ShadowGroup], as [NewLitPBRShadowPipeline] does, so the first directional light is attenuated by the shadow map. The shadow uses bind group 3 like fog, IBL and skinning, so ShadowLayout cannot be combined with FogLayout, IBLLayout or SkinLayout.
	ShadowLayout *gpu.BindGroupLayout
	// LinearOutput skips the final linear-to-sRGB encoding and its clamp to [0, 1] and writes the lit linear radiance ([ShadePoint], plus [EvaluateIBL] or [Fog.Apply] when enabled), for drawing into an [HDRTarget] that a [ToneMapPass] then resolves to the display.
	LinearOutput bool
}

// NewLitPBRPipelineWithOptions creates a lit PBR pipeline like [NewLitPBRPipeline], [NewLitPBRFogPipeline], [NewLitPBRIBLPipeline] or [NewLitPBRShadowPipeline], with a configurable color format, with [LitPBRPipelineOptions].LinearOutput linear HDR output for an [HDRTarget], and with [LitPBRPipelineOptions].SkinLayout GPU skinning.
func NewLitPBRPipelineWithOptions(cx *core.Scope, dev *gpu.Device, camLayout, lightLayout, drawLayout *gpu.BindGroupLayout, opts LitPBRPipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if opts.FogLayout != nil && opts.IBLLayout != nil {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: fog and IBL both use bind group 3; set FogLayout or IBLLayout, not both"}
	}
	if opts.SkinLayout != nil && (opts.FogLayout != nil || opts.IBLLayout != nil) {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: skinning shares bind group 3 with fog and IBL; set SkinLayout without FogLayout or IBLLayout"}
	}
	if opts.ShadowLayout != nil && (opts.FogLayout != nil || opts.IBLLayout != nil || opts.SkinLayout != nil) {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: the directional shadow shares bind group 3 with fog, IBL and skinning; set ShadowLayout without FogLayout, IBLLayout or SkinLayout"}
	}
	label, src, linearSrc := "render3d.lit_pbr", LitPBRWGSL, litPBRLinearWGSL
	vertexLayout := MeshVertexLayout()
	layouts := make([]*gpu.BindGroupLayout, PBRDrawGroup+1, FogGroup+1)
	switch {
	case opts.SkinLayout != nil:
		if !opts.SkinLayout.Valid() {
			return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid skin bind group layout"}
		}
		label, src, linearSrc = "render3d.lit_pbr_skinned", LitPBRSkinnedWGSL, litPBRSkinnedLinearWGSL
		vertexLayout = SkinnedVertexLayout()
		layouts = append(layouts, opts.SkinLayout)
	case opts.FogLayout != nil:
		label, src, linearSrc = "render3d.lit_pbr_fog", LitPBRFogWGSL, litPBRFogLinearWGSL
		layouts = append(layouts, opts.FogLayout)
	case opts.IBLLayout != nil:
		label, src, linearSrc = "render3d.lit_pbr_ibl", LitPBRIBLWGSL, litPBRIBLLinearWGSL
		layouts = append(layouts, opts.IBLLayout)
	case opts.ShadowLayout != nil:
		label, src, linearSrc = "render3d.lit_pbr_shadow", LitPBRShadowWGSL, litPBRShadowLinearWGSL
		layouts = append(layouts, opts.ShadowLayout)
	}
	layouts[CameraGroup] = camLayout
	layouts[LightGroup] = lightLayout
	layouts[PBRDrawGroup] = drawLayout
	if opts.LinearOutput {
		src = linearSrc
		label += ".linear"
	}
	if opts.Label != "" {
		label = opts.Label
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	return newLitPBRPipeline(cx, dev, label, src, layouts, vertexLayout, colorFmt, opts.DepthFormat)
}

// newLitPBRPipeline compiles src and creates the back-face-culled triangle-list pipeline [NewLitPBRPipeline] and [NewLitPBRFogPipeline] share.
func newLitPBRPipeline(cx *core.Scope, dev *gpu.Device, label, src string, layouts []*gpu.BindGroupLayout, vertexLayout gpu.VertexBufferLayout, colorFormat, depthFormat gpu.TextureFormat) (*gpu.Pipeline, error) {
	mod, err := gpu.CompileWGSL(src)
	if err != nil {
		return nil, err
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{vertexLayout},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  gpu.CullBack,
		},
		Targets: []gpu.ColorTargetState{{Format: colorFormat, WriteMask: gpu.ColorWriteAll}},
	}
	if depthFormat != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:       depthFormat,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		}
	}
	return dev.CreatePipeline(cx, desc)
}

// PointLightAttenuation computes the glTF KHR_lights_punctual inverse-square distance attenuation with
// smooth range windowing:
//
//	max(min(1 - (d / lightRange)^4, 1), 0)^2 / max(distSq, 1e-4)
//
// When lightRange <= 0, pure clamped inverse-square attenuation 1 / max(distSq, 1e-4) is returned.
// This matches point_light_attenuation in [LightWGSL].
func PointLightAttenuation(distSq, lightRange float32) float32 {
	if distSq < 0 {
		distSq = 0
	}
	invD2 := float32(1.0) / max(distSq, minPointLightDistSq)
	if lightRange > 0 {
		ratio := float32(math.Sqrt(float64(distSq))) / lightRange
		ratio2 := ratio * ratio
		window := clamp01(1 - ratio2*ratio2)
		return invD2 * window * window
	}
	return invD2
}

// EvaluatePBR evaluates the glTF 2.0 metallic-roughness Cook-Torrance GGX BRDF (diffuse + specular,
// before multiplying by light radiance and NdotL) for unit surface normal n, unit view direction v
// toward the eye, and unit light direction l toward the light.
//
// It uses dielectric F0 = 0.04, Schlick Fresnel, Trowbridge-Reitz GGX normal distribution,
// height-correlated Smith-GGX visibility, and energy-conserving Lambertian diffuse, matching
// pbr_brdf in [LightWGSL].
func EvaluatePBR(n, v, l math3d.Vec3, mat PBRMaterial) math3d.Vec3 {
	ndotl := n.Dot(l)
	ndotvRaw := n.Dot(v)
	if ndotl <= 0 || ndotvRaw <= 0 {
		return math3d.Vec3Zero()
	}
	ndotv := max(ndotvRaw, float32(1e-4))
	m := clamp01(mat.Metallic)
	r := clampRange(mat.Roughness, minPerceptualRoughness, 1)
	a := r * r
	a2 := a * a
	const pi = float32(3.14159265)

	f0 := math3d.Vec3{
		X: dielectricF0 + (mat.BaseColor.X-dielectricF0)*m,
		Y: dielectricF0 + (mat.BaseColor.Y-dielectricF0)*m,
		Z: dielectricF0 + (mat.BaseColor.Z-dielectricF0)*m,
	}

	hRaw := v.Add(l)
	hLen2 := hRaw.LenSq()
	if hLen2 <= 1e-8 {
		invPi := (1 - m) / pi
		return math3d.Vec3{
			X: (1 - f0.X) * mat.BaseColor.X * invPi,
			Y: (1 - f0.Y) * mat.BaseColor.Y * invPi,
			Z: (1 - f0.Z) * mat.BaseColor.Z * invPi,
		}
	}
	h := hRaw.Scale(float32(1.0 / math.Sqrt(float64(hLen2))))
	ndoth := max(n.Dot(h), float32(0))
	vdoth := max(v.Dot(h), float32(0))

	denom := ndoth*ndoth*(a2-1) + 1
	d := a2 / (pi * denom * denom)

	gv := ndotl * float32(math.Sqrt(float64(ndotv*ndotv*(1-a2)+a2)))
	gl := ndotv * float32(math.Sqrt(float64(ndotl*ndotl*(1-a2)+a2)))
	vis := float32(0.5) / max(gv+gl, float32(1e-6))

	t := clamp01(1 - vdoth)
	t2 := t * t
	t5 := t2 * t2 * t
	f := math3d.Vec3{
		X: f0.X + (1-f0.X)*t5,
		Y: f0.Y + (1-f0.Y)*t5,
		Z: f0.Z + (1-f0.Z)*t5,
	}

	dv := d * vis
	invPi := (1 - m) / pi
	return math3d.Vec3{
		X: (1-f.X)*mat.BaseColor.X*invPi + dv*f.X,
		Y: (1-f.Y)*mat.BaseColor.Y*invPi + dv*f.Y,
		Z: (1-f.Z)*mat.BaseColor.Z*invPi + dv*f.Z,
	}
}

// ShadePoint evaluates total outgoing linear RGB radiance at worldPos with surface normal and view
// direction viewDir (from surface toward the eye) under rig and mat, matching shade_pbr + emissive
// in [LitPBRWGSL].
//
// normal and viewDir do not need to be unit length; if either is degenerate, only ambient and emissive
// terms contribute. Call [LinearToSRGB] on the returned linear color to encode sRGB display output.
func ShadePoint(worldPos, normal, viewDir math3d.Vec3, mat PBRMaterial, rig LightRig) math3d.Vec3 {
	return ShadePointShadowed(worldPos, normal, viewDir, mat, rig, 1)
}

// ShadePointShadowed is [ShadePoint] with the first directional light (rig.Directional[0]) scaled by shadowVisibility, matching shade_pbr_shadowed in [LightWGSL] and the lighting of [LitPBRShadowWGSL]. Pass [EvaluateDirectionalShadow]'s result as shadowVisibility to reproduce a shadowed pixel on the CPU; 1 gives exactly [ShadePoint].
func ShadePointShadowed(worldPos, normal, viewDir math3d.Vec3, mat PBRMaterial, rig LightRig, shadowVisibility float32) math3d.Vec3 {
	amb := rig.Ambient.Color.Scale(rig.Ambient.Intensity)
	lo := math3d.Vec3{
		X: amb.X*mat.BaseColor.X + mat.Emissive.X,
		Y: amb.Y*mat.BaseColor.Y + mat.Emissive.Y,
		Z: amb.Z*mat.BaseColor.Z + mat.Emissive.Z,
	}
	nLenSq := normal.LenSq()
	vLenSq := viewDir.LenSq()
	if nLenSq <= minLightDirLenSq || vLenSq <= minLightDirLenSq {
		return lo
	}
	n := normal.Scale(float32(1.0 / math.Sqrt(float64(nLenSq))))
	v := viewDir.Scale(float32(1.0 / math.Sqrt(float64(vLenSq))))

	nDir := min(len(rig.Directional), MaxDirectionalLights)
	for i := range nDir {
		dl := rig.Directional[i]
		dLenSq := dl.Direction.LenSq()
		if dLenSq <= minLightDirLenSq {
			continue
		}
		l := dl.Direction.Scale(float32(-1.0 / math.Sqrt(float64(dLenSq))))
		ndotl := n.Dot(l)
		if ndotl <= 0 {
			continue
		}
		brdf := EvaluatePBR(n, v, l, mat)
		vis := float32(1)
		if i == 0 {
			vis = shadowVisibility
		}
		rad := dl.Color.Scale(dl.Intensity * (ndotl * vis))
		lo.X += brdf.X * rad.X
		lo.Y += brdf.Y * rad.Y
		lo.Z += brdf.Z * rad.Z
	}

	nPt := min(len(rig.Point), MaxPointLights)
	for i := range nPt {
		pl := rig.Point[i]
		toLight := pl.Position.Sub(worldPos)
		distSq := toLight.LenSq()
		atten := PointLightAttenuation(distSq, pl.Range)
		if atten <= 0 || distSq <= minLightDirLenSq {
			continue
		}
		l := toLight.Scale(float32(1.0 / math.Sqrt(float64(distSq))))
		ndotl := n.Dot(l)
		if ndotl <= 0 {
			continue
		}
		brdf := EvaluatePBR(n, v, l, mat)
		rad := pl.Color.Scale(pl.Intensity * atten * ndotl)
		lo.X += brdf.X * rad.X
		lo.Y += brdf.Y * rad.Y
		lo.Z += brdf.Z * rad.Z
	}
	return lo
}

// LinearToSRGBChannel converts a single linear light channel in [0, 1] (clamped) to sRGB using the
// IEC 61966-2-1 transfer function, matching linear_to_srgb_channel in [LightWGSL].
func LinearToSRGBChannel(c float32) float32 {
	x := clamp01(c)
	if x <= 0.0031308 {
		return 12.92 * x
	}
	return float32(1.055*math.Pow(float64(x), 1.0/2.4) - 0.055)
}

// LinearToSRGB converts a linear RGB triple (clamped to [0, 1] per channel) to sRGB, matching
// linear_to_srgb in [LightWGSL].
func LinearToSRGB(rgb math3d.Vec3) math3d.Vec3 {
	return math3d.Vec3{
		X: LinearToSRGBChannel(rgb.X),
		Y: LinearToSRGBChannel(rgb.Y),
		Z: LinearToSRGBChannel(rgb.Z),
	}
}

// SRGBToLinearChannel decodes a single sRGB-encoded channel in [0, 1] (clamped, NaN to 0) to linear light using the IEC 61966-2-1 transfer function; it is the inverse of [LinearToSRGBChannel].
func SRGBToLinearChannel(c float32) float32 {
	x := clamp01(c)
	if x <= 0.04045 {
		return x / 12.92
	}
	return float32(math.Pow(float64((x+0.055)/1.055), 2.4))
}

// SRGBToLinear decodes an sRGB triple (clamped to [0, 1] per channel) to linear RGB; it is the inverse of [LinearToSRGB].
func SRGBToLinear(rgb math3d.Vec3) math3d.Vec3 {
	return math3d.Vec3{
		X: SRGBToLinearChannel(rgb.X),
		Y: SRGBToLinearChannel(rgb.Y),
		Z: SRGBToLinearChannel(rgb.Z),
	}
}

func validLinearColor(c math3d.Vec3) bool {
	return finite3(c) && c.X >= 0 && c.Y >= 0 && c.Z >= 0
}

func clampRange(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func putVec4(b []byte, v math3d.Vec4) {
	binary.LittleEndian.PutUint32(b[0:], math.Float32bits(v.X))
	binary.LittleEndian.PutUint32(b[4:], math.Float32bits(v.Y))
	binary.LittleEndian.PutUint32(b[8:], math.Float32bits(v.Z))
	binary.LittleEndian.PutUint32(b[12:], math.Float32bits(v.W))
}
