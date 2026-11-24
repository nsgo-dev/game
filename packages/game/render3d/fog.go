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

// Bind group slots and byte sizes for GPU fog uniforms and the built-in fog pipeline.
const (
	// FogGroup is the bind group index reserved for [GPUFogBuffer.BindGroup] alongside [CameraGroup] (group 0) in [FogBindingWGSL] and [NewFogPipeline].
	FogGroup = 1
	// FogObjectGroup is the bind group index [FogPipeline] uses for per-object transforms, colors, and directional lighting ([GPUFogObject.Bind]).
	FogObjectGroup = 2

	// FogUniformSize is the byte size of [FogUniforms] as written by [AppendFogUniforms] and declared by [FogWGSL] (three vec4<f32> blocks).
	FogUniformSize = 48
	// FogObjectUniformSize is the byte size of the per-object uniform block used by [FogPipeline] (one mat4x4<f32> and two vec4<f32> blocks).
	FogObjectUniformSize = 96
)

// ErrInvalidFog is wrapped by every error [Fog.Validate], [FogObject.Validate], [NewGPUFogBuffer], [GPUFogBuffer.Update], [FogPipeline.NewObjectBuffer], and [GPUFogObject.Update] return for malformed parameters.
var ErrInvalidFog = errors.New("render3d: invalid fog")

// FogWGSL declares the portable WGSL Fog uniform struct and its evaluation helpers:
//   - fog_factor(cam_pos, world_pos, fog) -> f32
//   - apply_fog(color, cam_pos, world_pos, fog) -> vec3<f32>
//
// It intentionally declares no @group/@binding variable so any shader can embed Fog in its own uniform layout or prepend [FogBindingWGSL] to bind [GPUFogBuffer] at [FogGroup].
// Its struct layout matches [AppendFogUniforms] byte for byte, and its formulas match [Fog.Factor] and [Fog.Apply].
const FogWGSL = `struct Fog {
	color_opacity: vec4<f32>,
	distance_params: vec4<f32>,
	height_params: vec4<f32>,
}

fn fog_exp(x: f32) -> f32 {
	return pow(2.718281828, x);
}

fn fog_factor(cam_pos: vec3<f32>, world_pos: vec3<f32>, fog: Fog) -> f32 {
	let d = length(world_pos - cam_pos);
	var f_dist: f32 = 0.0;
	let mode = fog.distance_params.x;
	if mode > 0.5 && mode < 1.5 {
		let start = fog.distance_params.y;
		let end = fog.distance_params.z;
		if end <= start {
			if d >= start {
				f_dist = 1.0;
			}
		} else {
			f_dist = clamp((d - start) / (end - start), 0.0, 1.0);
		}
	} else if mode >= 1.5 && mode < 2.5 {
		let density = max(fog.distance_params.w, 0.0);
		if density > 0.0 {
			let x = max(d * density, 0.0);
			f_dist = clamp(1.0 - fog_exp(-min(x, 80.0)), 0.0, 1.0);
		}
	} else if mode >= 2.5 {
		let density = max(fog.distance_params.w, 0.0);
		if density > 0.0 {
			let x = max(d * density, 0.0);
			f_dist = clamp(1.0 - fog_exp(-min(x * x, 80.0)), 0.0, 1.0);
		}
	}

	var f_height: f32 = 0.0;
	if fog.height_params.x > 0.5 {
		let h_base = fog.height_params.y;
		let h_falloff = max(fog.height_params.z, 0.0);
		let h_density = max(fog.height_params.w, 0.0);
		if d > 0.0 && h_density > 0.0 {
			let h0 = cam_pos.y - h_base;
			let h1 = world_pos.y - h_base;
			let h_min = min(h0, h1);
			let delta = abs(h1 - h0);
			let u = h_falloff * delta;
			var integral: f32 = 1.0;
			if u < 0.05 {
				let u2 = u * u;
				integral = 1.0 - 0.5 * u + u2 / 6.0 - (u2 * u) / 24.0;
			} else {
				integral = (1.0 - fog_exp(-min(u, 80.0))) / u;
			}
			let exp_arg = clamp(-h_falloff * h_min, -80.0, 40.0);
			let tau = max(h_density * d * fog_exp(exp_arg) * integral, 0.0);
			f_height = clamp(1.0 - fog_exp(-min(tau, 80.0)), 0.0, 1.0);
		}
	}

	let combined = 1.0 - (1.0 - f_dist) * (1.0 - f_height);
	return clamp(combined, 0.0, clamp(fog.color_opacity.w, 0.0, 1.0));
}

fn apply_fog(color: vec3<f32>, cam_pos: vec3<f32>, world_pos: vec3<f32>, fog: Fog) -> vec3<f32> {
	let f = fog_factor(cam_pos, world_pos, fog);
	return mix(color, fog.color_opacity.xyz, vec3<f32>(f));
}
`

// FogBindingWGSL declares the Fog uniform binding at @group(1) @binding(0) matching [FogGroup] and [GPUFogBuffer].
const FogBindingWGSL = `@group(1) @binding(0) var<uniform> fog: Fog;
`

// FogLitWGSL is the complete WGSL module compiled by [NewFogPipeline]. It combines [CameraWGSL], [FogWGSL], [FogBindingWGSL], [MeshVertexLayout] inputs, directional Lambertian lighting with configurable ambient, and distance/height fog for both perspective and orthographic cameras.
const FogLitWGSL = CameraWGSL + FogWGSL + FogBindingWGSL + `struct FogObject {
	model: mat4x4<f32>,
	base_color: vec4<f32>,
	light_dir_ambient: vec4<f32>,
}

@group(2) @binding(0) var<uniform> fog_object: FogObject;

struct FogVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct FogVertexOut {
	@builtin(position) clip_pos: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
}

@vertex
fn vs_fog(in: FogVertexIn) -> FogVertexOut {
	var out: FogVertexOut;
	let world = fog_object.model * vec4<f32>(in.pos, 1.0);
	out.clip_pos = camera.view_proj * world;
	out.world_pos = world.xyz;
	out.world_normal = (fog_object.model * vec4<f32>(in.normal, 0.0)).xyz;
	return out;
}

@fragment
fn fs_fog(in: FogVertexOut) -> @location(0) vec4<f32> {
	let ambient = clamp(fog_object.light_dir_ambient.w, 0.0, 1.0);
	var shade: f32 = ambient;
	let l_dir = fog_object.light_dir_ambient.xyz;
	let l_len = length(l_dir);
	let n_len = length(in.world_normal);
	if l_len > 0.00001 && n_len > 0.00001 {
		let ndotl = max(dot(in.world_normal / n_len, l_dir / l_len), 0.0);
		shade = ambient + (1.0 - ambient) * ndotl;
	}
	let lit = fog_object.base_color.rgb * shade;
	var cam_pos = camera.eye.xyz;
	if camera.eye.w <= 0.5 {
		let view_pos = camera.view * vec4<f32>(in.world_pos, 1.0);
		cam_pos = in.world_pos + camera.eye.xyz * max(-view_pos.z, 0.0);
	}
	let fogged = apply_fog(lit, cam_pos, in.world_pos, fog);
	return vec4<f32>(fogged, fog_object.base_color.a);
}
`

// FogMode selects how distance fog attenuates visibility with camera distance.
type FogMode uint8

const (
	// FogOff disables distance fog attenuation. Analytical height fog may still apply when [Fog.HeightEnabled] is true.
	FogOff FogMode = iota
	// FogLinear ramps distance fog linearly from 0 at [Fog.Start] to 1 at [Fog.End].
	FogLinear
	// FogExp applies exponential distance fog: 1 - exp(-distance * Density).
	FogExp
	// FogExp2 applies exponential-squared distance fog: 1 - exp(-(distance * Density)^2).
	FogExp2
)

// String returns the mode's lowercase name ("off", "linear", "exp", "exp2") or "unknown" for out-of-range values.
func (m FogMode) String() string {
	switch m {
	case FogOff:
		return "off"
	case FogLinear:
		return "linear"
	case FogExp:
		return "exp"
	case FogExp2:
		return "exp2"
	default:
		return "unknown"
	}
}

// Fog configures distance fog, analytical exponential height fog along the view ray, or both combined.
//
// Color is in linear RGB. MaxOpacity caps the combined fog blend factor in (0, 1]; a zero value defaults to 1.0 so struct literals that omit MaxOpacity apply full fog.
//
// When both distance fog and height fog are enabled, their transmittances multiply:
//
//	factor = clamp(1 - (1 - f_dist) * (1 - f_height), 0, MaxOpacity)
//
// Height fog models exponential vertical extinction σ(y) = HeightDensity * exp(-HeightFalloff * (y - HeightBase)) analytically integrated along the segment from camPos to worldPos, so it is smooth at grazing angles and symmetric between endpoints.
type Fog struct {
	Mode       FogMode
	Color      [3]float32
	Start      float32
	End        float32
	Density    float32
	MaxOpacity float32

	HeightEnabled bool
	HeightBase    float32
	HeightFalloff float32
	HeightDensity float32
}

// Validate checks that every field of f is finite and in range: recognized [FogMode], non-negative linear RGB color, non-negative Start/End/Density/HeightFalloff/HeightDensity, MaxOpacity in [0, 1] (0 means default 1.0), and End > Start when Mode is [FogLinear]. Every error wraps [ErrInvalidFog].
func (f Fog) Validate() error {
	if f.Mode > FogExp2 {
		return fmt.Errorf("%w: unknown mode %d", ErrInvalidFog, f.Mode)
	}
	for i, c := range f.Color {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: color[%d] must be finite and >= 0, got %v", ErrInvalidFog, i, c)
		}
	}
	if !finite(f.Start) || f.Start < 0 {
		return fmt.Errorf("%w: start must be finite and >= 0, got %v", ErrInvalidFog, f.Start)
	}
	if !finite(f.End) || f.End < 0 {
		return fmt.Errorf("%w: end must be finite and >= 0, got %v", ErrInvalidFog, f.End)
	}
	if f.Mode == FogLinear && f.End <= f.Start {
		return fmt.Errorf("%w: linear fog requires end (%v) > start (%v)", ErrInvalidFog, f.End, f.Start)
	}
	if !finite(f.Density) || f.Density < 0 {
		return fmt.Errorf("%w: density must be finite and >= 0, got %v", ErrInvalidFog, f.Density)
	}
	if !finite(f.MaxOpacity) || f.MaxOpacity < 0 || f.MaxOpacity > 1 {
		return fmt.Errorf("%w: max opacity must be in [0, 1], got %v", ErrInvalidFog, f.MaxOpacity)
	}
	if !finite(f.HeightBase) {
		return fmt.Errorf("%w: height base must be finite, got %v", ErrInvalidFog, f.HeightBase)
	}
	if !finite(f.HeightFalloff) || f.HeightFalloff < 0 {
		return fmt.Errorf("%w: height falloff must be finite and >= 0, got %v", ErrInvalidFog, f.HeightFalloff)
	}
	if !finite(f.HeightDensity) || f.HeightDensity < 0 {
		return fmt.Errorf("%w: height density must be finite and >= 0, got %v", ErrInvalidFog, f.HeightDensity)
	}
	return nil
}

// Sanitized returns a copy of f with non-finite values replaced by safe defaults, negative magnitudes clamped to 0, unrecognized modes mapped to [FogOff], MaxOpacity defaulted to 1.0 when <= 0 and clamped to 1.0 when > 1, and linear End raised to at least Start.
func (f Fog) Sanitized() Fog {
	if f.Mode > FogExp2 {
		f.Mode = FogOff
	}
	for i := range f.Color {
		if !finite(f.Color[i]) || f.Color[i] < 0 {
			f.Color[i] = 0
		}
	}
	if !finite(f.Start) || f.Start < 0 {
		f.Start = 0
	}
	if !finite(f.End) || f.End < 0 {
		f.End = 0
	}
	if f.Mode == FogLinear && f.End < f.Start {
		f.End = f.Start
	}
	if !finite(f.Density) || f.Density < 0 {
		f.Density = 0
	}
	if !finite(f.MaxOpacity) || f.MaxOpacity <= 0 {
		f.MaxOpacity = 1
	} else if f.MaxOpacity > 1 {
		f.MaxOpacity = 1
	}
	if !finite(f.HeightBase) {
		f.HeightBase = 0
	}
	if !finite(f.HeightFalloff) || f.HeightFalloff < 0 {
		f.HeightFalloff = 0
	}
	if !finite(f.HeightDensity) || f.HeightDensity < 0 {
		f.HeightDensity = 0
	}
	return f
}

// Factor evaluates the CPU reference fog blend factor in [0, MaxOpacity] along the view ray from camPos to worldPos, matching fog_factor in [FogWGSL]. Non-finite coordinates return 0.
func (f Fog) Factor(camPos, worldPos math3d.Vec3) float32 {
	return fogFactorSanitized(f.Sanitized(), camPos, worldPos)
}

// Apply blends linear RGB color toward f.Color using [Fog.Factor](camPos, worldPos), matching apply_fog in [FogWGSL].
func (f Fog) Apply(color [3]float32, camPos, worldPos math3d.Vec3) [3]float32 {
	s := f.Sanitized()
	factor := fogFactorSanitized(s, camPos, worldPos)
	inv := 1 - factor
	return [3]float32{
		color[0]*inv + s.Color[0]*factor,
		color[1]*inv + s.Color[1]*factor,
		color[2]*inv + s.Color[2]*factor,
	}
}

func fogFactorSanitized(s Fog, camPos, worldPos math3d.Vec3) float32 {
	if !finite3(camPos) || !finite3(worldPos) {
		return 0
	}
	if s.Mode == FogOff && !s.HeightEnabled {
		return 0
	}
	d := worldPos.Sub(camPos).Len()
	if !finite(d) {
		return 0
	}

	var fDist float32
	switch s.Mode {
	case FogLinear:
		if s.End <= s.Start {
			if d >= s.Start {
				fDist = 1
			}
		} else {
			fDist = fogClamp((d-s.Start)/(s.End-s.Start), 0, 1)
		}
	case FogExp:
		if s.Density > 0 {
			x := d * s.Density
			if !finite(x) || x > 80 {
				x = 80
			} else if x < 0 {
				x = 0
			}
			fDist = fogClamp(1-float32(math.Exp(float64(-x))), 0, 1)
		}
	case FogExp2:
		if s.Density > 0 {
			x := d * s.Density
			x2 := x * x
			if !finite(x2) || x2 > 80 {
				x2 = 80
			} else if x2 < 0 {
				x2 = 0
			}
			fDist = fogClamp(1-float32(math.Exp(float64(-x2))), 0, 1)
		}
	}

	var fHeight float32
	if s.HeightEnabled && d > 0 && s.HeightDensity > 0 {
		h0 := camPos.Y - s.HeightBase
		h1 := worldPos.Y - s.HeightBase
		hMin := h0
		if h1 < hMin {
			hMin = h1
		}
		delta := float32(math.Abs(float64(h1 - h0)))
		u := s.HeightFalloff * delta
		var integral float32 = 1
		if u < 0.05 {
			u2 := u * u
			integral = 1 - 0.5*u + u2/6 - (u2*u)/24
		} else {
			uClamped := u
			if !finite(uClamped) || uClamped > 80 {
				uClamped = 80
			}
			integral = (1 - float32(math.Exp(float64(-uClamped)))) / u
		}
		expArg := fogClamp(-s.HeightFalloff*hMin, -80, 40)
		tau := s.HeightDensity * d * float32(math.Exp(float64(expArg))) * integral
		if !finite(tau) || tau > 80 {
			tau = 80
		} else if tau < 0 {
			tau = 0
		}
		fHeight = fogClamp(1-float32(math.Exp(float64(-tau))), 0, 1)
	}

	combined := 1 - (1-fDist)*(1-fHeight)
	return fogClamp(combined, 0, s.MaxOpacity)
}

// FogUniforms is the std140/WGSL-aligned uniform block declared by [FogWGSL] (three vec4<f32> vectors, [FogUniformSize] bytes).
type FogUniforms struct {
	// ColorOpacity holds linear RGB fog color in [0..2] and clamped MaxOpacity in [3].
	ColorOpacity [4]float32
	// DistanceParams holds mode (0=off, 1=linear, 2=exp, 3=exp2) in [0], Start in [1], End in [2], and Density in [3].
	DistanceParams [4]float32
	// HeightParams holds enabled (0 or 1) in [0], HeightBase in [1], HeightFalloff in [2], and HeightDensity in [3].
	HeightParams [4]float32
}

// Uniforms packs a sanitized copy of f into the GPU uniform layout declared by [FogWGSL].
func (f Fog) Uniforms() FogUniforms {
	s := f.Sanitized()
	var heightEnabled float32
	if s.HeightEnabled {
		heightEnabled = 1
	}
	return FogUniforms{
		ColorOpacity:   [4]float32{s.Color[0], s.Color[1], s.Color[2], s.MaxOpacity},
		DistanceParams: [4]float32{float32(s.Mode), s.Start, s.End, s.Density},
		HeightParams:   [4]float32{heightEnabled, s.HeightBase, s.HeightFalloff, s.HeightDensity},
	}
}

// AppendFogUniforms appends u to dst in the little-endian layout [FogWGSL] declares ([FogUniformSize] bytes) and returns the extended slice. It allocates only when dst lacks capacity for [FogUniformSize] more bytes.
func AppendFogUniforms(dst []byte, u FogUniforms) []byte {
	start := len(dst)
	dst = grow(dst, FogUniformSize)
	out := dst[start:]
	fogPutVec4(out[0:16], u.ColorOpacity)
	fogPutVec4(out[16:32], u.DistanceParams)
	fogPutVec4(out[32:48], u.HeightParams)
	return dst
}

// Encode writes u into dst[:FogUniformSize] as little-endian float32s and returns dst[:FogUniformSize]. It panics if len(dst) < FogUniformSize.
func (u FogUniforms) Encode(dst []byte) []byte {
	if len(dst) < FogUniformSize {
		panic("render3d: FogUniforms.Encode: dst shorter than FogUniformSize")
	}
	fogPutVec4(dst[0:16], u.ColorOpacity)
	fogPutVec4(dst[16:32], u.DistanceParams)
	fogPutVec4(dst[32:48], u.HeightParams)
	return dst[:FogUniformSize]
}

// FogBufferOptions configures [NewGPUFogBuffer] and [FogPipeline.NewObjectBuffer].
type FogBufferOptions struct {
	// Label names the buffer and bind group in GPU capture tools.
	Label string
	// Retained keeps a CPU shadow copy so NSGo recreates the buffer, layout, and bind group with their latest data after a device loss.
	Retained bool
}

// GPUFogBuffer holds a [Fog] uniform buffer, its bind group layout, and its bind group on an NSGo [gpu.Device], scoped to the [core.Scope] passed to [NewGPUFogBuffer].
//
// Bind it at [FogGroup] when using [FogBindingWGSL] or [NewFogPipeline], or at any group chosen by a custom shader that embeds [FogWGSL].
type GPUFogBuffer struct {
	dev     *gpu.Device
	buf     *gpu.Buffer
	layout  *gpu.BindGroupLayout
	group   *gpu.BindGroup
	scratch [FogUniformSize]byte
}

// NewGPUFogBuffer validates fog and creates its uniform buffer, bind group layout, and bind group on dev, owned by cx (nil for resources closed manually). An invalid fog returns an error wrapping [ErrInvalidFog] before creating any GPU resource.
func NewGPUFogBuffer(cx *core.Scope, dev *gpu.Device, fog Fog, opts FogBufferOptions) (*GPUFogBuffer, error) {
	if err := fog.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	var initData [FogUniformSize]byte
	AppendFogUniforms(initData[:0], fog.Uniforms())

	label := fogLabel(opts.Label, "render3d.fog")
	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label,
		Size:     FogUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     initData[:],
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
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: buf, Size: FogUniformSize}},
	})
	if err != nil {
		_ = layout.Close()
		_ = buf.Close()
		return nil, err
	}
	return &GPUFogBuffer{dev: dev, buf: buf, layout: layout, group: group}, nil
}

// Update validates fog and writes its packed uniforms to the GPU buffer without allocating into scratch storage. Pass the active [gpu.Device] (for example frame.Device()) so a retained buffer continues accepting updates after a device loss and restore; passing nil reuses the last device.
func (b *GPUFogBuffer) Update(dev *gpu.Device, fog Fog) error {
	if err := fog.Validate(); err != nil {
		return err
	}
	if !b.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: fog buffer is not valid"}
	}
	if dev != nil {
		b.dev = dev
	}
	if b.dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	data := AppendFogUniforms(b.scratch[:0], fog.Uniforms())
	b.dev.WriteBuffer(b.buf, 0, data)
	return nil
}

// Buffer returns the underlying uniform buffer, or nil when b is nil.
func (b *GPUFogBuffer) Buffer() *gpu.Buffer {
	if b == nil {
		return nil
	}
	return b.buf
}

// Layout returns the bind group layout for pipelines that bind b.
func (b *GPUFogBuffer) Layout() *gpu.BindGroupLayout {
	if b == nil {
		return nil
	}
	return b.layout
}

// BindGroup returns the bind group to pass to [gpu.RenderPass.SetBindGroup].
func (b *GPUFogBuffer) BindGroup() *gpu.BindGroup {
	if b == nil {
		return nil
	}
	return b.group
}

// Valid reports whether the buffer, layout, and bind group are open on a live device.
func (b *GPUFogBuffer) Valid() bool {
	return b != nil && b.buf.Valid() && b.layout.Valid() && b.group.Valid()
}

// Close releases the bind group, layout, and buffer now instead of waiting for their owning scope. It is safe to call on nil or more than once.
func (b *GPUFogBuffer) Close() error {
	if b == nil {
		return nil
	}
	return errors.Join(b.group.Close(), b.layout.Close(), b.buf.Close())
}

// FogObject holds per-draw transform, linear RGBA base color, and directional lighting parameters for [FogPipeline].
//
// A zero Model defaults to identity; a zero BaseColor defaults to opaque white {1, 1, 1, 1}. When LightDir is zero and Ambient is 0, shading defaults to unlit (Ambient = 1.0) so fragments emit exact BaseColor before fog is applied.
type FogObject struct {
	Model     math3d.Mat4
	BaseColor [4]float32
	LightDir  math3d.Vec3
	Ambient   float32
}

// Validate checks that every field of o is finite and Ambient is in [0, 1]. Every error wraps [ErrInvalidFog].
func (o FogObject) Validate() error {
	for i, v := range o.Model {
		if !finite(v) {
			return fmt.Errorf("%w: object model[%d] is not finite", ErrInvalidFog, i)
		}
	}
	for i, c := range o.BaseColor {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: object base_color[%d] must be finite and >= 0, got %v", ErrInvalidFog, i, c)
		}
	}
	if !finite3(o.LightDir) {
		return fmt.Errorf("%w: object light_dir is not finite", ErrInvalidFog)
	}
	if !finite(o.Ambient) || o.Ambient < 0 || o.Ambient > 1 {
		return fmt.Errorf("%w: object ambient must be in [0, 1], got %v", ErrInvalidFog, o.Ambient)
	}
	return nil
}

// FogPipelineOptions configures [NewFogPipeline].
type FogPipelineOptions struct {
	Label       string
	ColorFormat gpu.TextureFormat
	DepthFormat gpu.TextureFormat
	CullMode    gpu.CullMode
	SampleCount int
	Retained    bool
}

// FogPipeline wraps a ready-to-use lit and fogged [gpu.Pipeline] built from [FogLitWGSL] and [MeshVertexLayout], together with the per-object bind group layout at [FogObjectGroup].
type FogPipeline struct {
	pipe      *gpu.Pipeline
	objLayout *gpu.BindGroupLayout
}

// NewFogPipeline compiles [FogLitWGSL] and creates a render pipeline expecting [CameraBuffer.Layout] at [CameraGroup], [GPUFogBuffer.Layout] at [FogGroup], and per-object uniforms from [FogPipeline.NewObjectBuffer] at [FogObjectGroup].
func NewFogPipeline(cx *core.Scope, dev *gpu.Device, camLayout, fogLayout *gpu.BindGroupLayout, opts FogPipelineOptions) (*FogPipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() || fogLayout == nil || !fogLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: camera and fog bind group layouts must be valid"}
	}
	mod, err := gpu.CompileWGSL(FogLitWGSL)
	if err != nil {
		return nil, err
	}
	label := fogLabel(opts.Label, "render3d.fog-lit")
	objLayout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " object",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageVertex | gpu.StageFragment,
			Type:       gpu.BindingUniform,
		}},
	})
	if err != nil {
		return nil, err
	}
	colorFormat := opts.ColorFormat
	if colorFormat == gpu.FormatUndefined {
		colorFormat = dev.SurfaceFormat()
	}
	layouts := make([]*gpu.BindGroupLayout, FogObjectGroup+1)
	layouts[CameraGroup] = camLayout
	layouts[FogGroup] = fogLayout
	layouts[FogObjectGroup] = objLayout
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_fog"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_fog"},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  opts.CullMode,
		},
		Targets: []gpu.ColorTargetState{{
			Format:    colorFormat,
			WriteMask: gpu.ColorWriteAll,
		}},
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
	pipe, err := dev.CreatePipeline(cx, desc)
	if err != nil {
		_ = objLayout.Close()
		return nil, err
	}
	return &FogPipeline{pipe: pipe, objLayout: objLayout}, nil
}

// Pipeline returns the underlying [gpu.Pipeline].
func (p *FogPipeline) Pipeline() *gpu.Pipeline {
	if p == nil {
		return nil
	}
	return p.pipe
}

// ObjectLayout returns the bind group layout bound at [FogObjectGroup].
func (p *FogPipeline) ObjectLayout() *gpu.BindGroupLayout {
	if p == nil {
		return nil
	}
	return p.objLayout
}

// Bind sets the pipeline along with the camera and fog bind groups on pass. It does nothing when p is not [FogPipeline.Valid].
func (p *FogPipeline) Bind(pass *gpu.RenderPass, camGroup, fogGroup *gpu.BindGroup) {
	if !p.Valid() || pass == nil {
		return
	}
	pass.SetPipeline(p.pipe)
	if camGroup != nil && camGroup.Valid() {
		pass.SetBindGroup(CameraGroup, camGroup)
	}
	if fogGroup != nil && fogGroup.Valid() {
		pass.SetBindGroup(FogGroup, fogGroup)
	}
}

// NewObjectBuffer creates a per-object uniform buffer and bind group matching p's [FogObjectGroup] layout.
func (p *FogPipeline) NewObjectBuffer(cx *core.Scope, dev *gpu.Device, obj FogObject, opts FogBufferOptions) (*GPUFogObject, error) {
	if err := obj.Validate(); err != nil {
		return nil, err
	}
	if dev == nil || !p.Valid() {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: fog pipeline or device is not valid"}
	}
	var initData [FogObjectUniformSize]byte
	fogEncodeObject(initData[:], obj)
	label := fogLabel(opts.Label, "render3d.fog-object")
	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label,
		Size:     FogObjectUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     initData[:],
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	group, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:   label,
		Layout:  p.objLayout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: buf, Size: FogObjectUniformSize}},
	})
	if err != nil {
		_ = buf.Close()
		return nil, err
	}
	return &GPUFogObject{dev: dev, buf: buf, group: group}, nil
}

// Valid reports whether the pipeline and object layout are open on a live device.
func (p *FogPipeline) Valid() bool {
	return p != nil && p.pipe.Valid() && p.objLayout.Valid()
}

// Close releases the pipeline and object bind group layout. It is safe to call on nil or more than once.
func (p *FogPipeline) Close() error {
	if p == nil {
		return nil
	}
	return errors.Join(p.pipe.Close(), p.objLayout.Close())
}

// GPUFogObject holds per-object transform, color, and directional light uniforms for [FogPipeline].
type GPUFogObject struct {
	dev     *gpu.Device
	buf     *gpu.Buffer
	group   *gpu.BindGroup
	scratch [FogObjectUniformSize]byte
}

// Update validates obj and uploads its uniform bytes to the GPU buffer.
func (o *GPUFogObject) Update(dev *gpu.Device, obj FogObject) error {
	if err := obj.Validate(); err != nil {
		return err
	}
	if !o.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: fog object buffer is not valid"}
	}
	if dev != nil {
		o.dev = dev
	}
	if o.dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	fogEncodeObject(o.scratch[:], obj)
	o.dev.WriteBuffer(o.buf, 0, o.scratch[:])
	return nil
}

// Bind sets the object's bind group at [FogObjectGroup] on pass. It does nothing when o is not [GPUFogObject.Valid].
func (o *GPUFogObject) Bind(pass *gpu.RenderPass) {
	if !o.Valid() || pass == nil {
		return
	}
	pass.SetBindGroup(FogObjectGroup, o.group)
}

// Buffer returns the underlying uniform buffer.
func (o *GPUFogObject) Buffer() *gpu.Buffer {
	if o == nil {
		return nil
	}
	return o.buf
}

// BindGroup returns the per-object bind group.
func (o *GPUFogObject) BindGroup() *gpu.BindGroup {
	if o == nil {
		return nil
	}
	return o.group
}

// Valid reports whether the object's buffer and bind group are open on a live device.
func (o *GPUFogObject) Valid() bool {
	return o != nil && o.buf.Valid() && o.group.Valid()
}

// Close releases the bind group and uniform buffer. It is safe to call on nil or more than once.
func (o *GPUFogObject) Close() error {
	if o == nil {
		return nil
	}
	return errors.Join(o.group.Close(), o.buf.Close())
}

func fogEncodeObject(dst []byte, obj FogObject) {
	model := obj.Model
	if fogMat4IsZero(model) {
		model = math3d.Mat4Identity()
	}
	for i, f := range model {
		binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(f))
	}
	col := obj.BaseColor
	if col == ([4]float32{}) {
		col = [4]float32{1, 1, 1, 1}
	}
	fogPutVec4(dst[64:80], col)

	ambient := obj.Ambient
	lightDir := obj.LightDir
	if lightDir.LenSq() == 0 && ambient == 0 {
		ambient = 1
	}
	fogPutVec4(dst[80:96], [4]float32{lightDir.X, lightDir.Y, lightDir.Z, ambient})
}

func fogMat4IsZero(m math3d.Mat4) bool {
	for _, f := range m {
		if f != 0 {
			return false
		}
	}
	return true
}

func fogPutVec4(dst []byte, v [4]float32) {
	binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(v[0]))
	binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(v[1]))
	binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(v[2]))
	binary.LittleEndian.PutUint32(dst[12:], math.Float32bits(v[3]))
}

func fogClamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func fogLabel(label, fallback string) string {
	if label == "" {
		return fallback
	}
	return label
}
