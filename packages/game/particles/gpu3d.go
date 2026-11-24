package particles

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Interleaved vertex and per-instance layouts for instanced 3D particle drawing on [gpu.View].
//
// Slot 0 holds the shared 4-vertex unit quad ([StepVertex]); slot 1 holds one 40-byte instance per live particle ([StepInstance]) written by [Emitter3D.AppendInstances].
const (
	// ParticleQuadVertexStride is the byte size of one unit-quad vertex: corner (float32x2) + UV (float32x2).
	ParticleQuadVertexStride = 16
	// ParticleQuadCornerOffset is the byte offset of the float32x2 corner in [-0.5, 0.5]².
	ParticleQuadCornerOffset = 0
	// ParticleQuadUVOffset is the byte offset of the float32x2 texture coordinate in [0, 1]².
	ParticleQuadUVOffset = 8

	// ParticleInstanceStride is the byte size of one packed 3D particle instance: center (12), right (12), up (12), RGBA8 color (4).
	ParticleInstanceStride = 40
	// ParticleInstanceCenterOffset is the byte offset of the float32x3 world-space quad center.
	ParticleInstanceCenterOffset = 0
	// ParticleInstanceRightOffset is the byte offset of the float32x3 world-space quad right vector (scaled by width).
	ParticleInstanceRightOffset = 12
	// ParticleInstanceUpOffset is the byte offset of the float32x3 world-space quad up vector (scaled by height).
	ParticleInstanceUpOffset = 24
	// ParticleInstanceColorOffset is the byte offset of the unorm8x4 RGBA particle color.
	ParticleInstanceColorOffset = 36

	// Shader @location indices for [ParticleVertexLayouts].
	ParticleCornerLocation = 0
	ParticleUVLocation     = 1
	ParticleCenterLocation = 2
	ParticleRightLocation  = 3
	ParticleUpLocation     = 4
	ParticleColorLocation  = 5
)

// ParticleWGSL is a WGSL shader module for solid-colored instanced 3D billboard particles.
//
// Its @group(0) @binding(0) camera uniform block matches render3d.CameraWGSL (view, proj, view_proj, eye) byte for byte, so render3d.CameraBuffer binds directly to it. Entry points are "vs_main" and "fs_main".
const ParticleWGSL = `struct Camera {
	view: mat4x4<f32>,
	proj: mat4x4<f32>,
	view_proj: mat4x4<f32>,
	eye: vec4<f32>,
}

@group(0) @binding(0) var<uniform> camera: Camera;

struct VertexIn {
	@location(0) corner: vec2<f32>,
	@location(1) uv: vec2<f32>,
	@location(2) center: vec3<f32>,
	@location(3) right: vec3<f32>,
	@location(4) up: vec3<f32>,
	@location(5) color: vec4<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) uv: vec2<f32>,
	@location(1) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	let world_pos = in.center + in.right * in.corner.x + in.up * in.corner.y;
	out.clip = camera.view_proj * vec4<f32>(world_pos, 1.0);
	out.uv = in.uv;
	out.color = in.color;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return in.color;
}
`

// ParticleSoftCircleWGSL is a WGSL shader module for soft radial-falloff instanced 3D billboard particles without requiring a texture.
//
// It binds the standard camera uniform at @group(0) @binding(0). Entry points are "vs_main" and "fs_main".
const ParticleSoftCircleWGSL = `struct Camera {
	view: mat4x4<f32>,
	proj: mat4x4<f32>,
	view_proj: mat4x4<f32>,
	eye: vec4<f32>,
}

@group(0) @binding(0) var<uniform> camera: Camera;

struct VertexIn {
	@location(0) corner: vec2<f32>,
	@location(1) uv: vec2<f32>,
	@location(2) center: vec3<f32>,
	@location(3) right: vec3<f32>,
	@location(4) up: vec3<f32>,
	@location(5) color: vec4<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) uv: vec2<f32>,
	@location(1) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	let world_pos = in.center + in.right * in.corner.x + in.up * in.corner.y;
	out.clip = camera.view_proj * vec4<f32>(world_pos, 1.0);
	out.uv = in.uv;
	out.color = in.color;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let d = in.uv * 2.0 - vec2<f32>(1.0, 1.0);
	let mask = clamp(1.0 - dot(d, d), 0.0, 1.0);
	return vec4<f32>(in.color.rgb, in.color.a * mask * mask);
}
`

// ParticleTexturedWGSL is a WGSL shader module for textured instanced 3D billboard particles.
//
// It binds the standard camera uniform at @group(0) @binding(0), the particle texture at @group(1) @binding(0), and the sampler at @group(1) @binding(1). Entry points are "vs_main" and "fs_textured".
const ParticleTexturedWGSL = `struct Camera {
	view: mat4x4<f32>,
	proj: mat4x4<f32>,
	view_proj: mat4x4<f32>,
	eye: vec4<f32>,
}

@group(0) @binding(0) var<uniform> camera: Camera;
@group(1) @binding(0) var particle_tex: texture_2d<f32>;
@group(1) @binding(1) var particle_sampler: sampler;

struct VertexIn {
	@location(0) corner: vec2<f32>,
	@location(1) uv: vec2<f32>,
	@location(2) center: vec3<f32>,
	@location(3) right: vec3<f32>,
	@location(4) up: vec3<f32>,
	@location(5) color: vec4<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) uv: vec2<f32>,
	@location(1) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	let world_pos = in.center + in.right * in.corner.x + in.up * in.corner.y;
	out.clip = camera.view_proj * vec4<f32>(world_pos, 1.0);
	out.uv = in.uv;
	out.color = in.color;
	return out;
}

@fragment
fn fs_textured(in: VertexOut) -> @location(0) vec4<f32> {
	return textureSample(particle_tex, particle_sampler, in.uv) * in.color;
}
`

// ParticleVertexLayouts returns the two vertex buffer layouts (slot 0 = unit quad per-vertex, slot 1 = packed particle per-instance) expected by [ParticleWGSL], [ParticleTexturedWGSL] and [GPUParticles3D.Draw].
func ParticleVertexLayouts() []gpu.VertexBufferLayout {
	return []gpu.VertexBufferLayout{
		{
			Stride:   ParticleQuadVertexStride,
			StepMode: gpu.StepVertex,
			Attributes: []gpu.VertexAttribute{
				{Format: gpu.VertexFloat32x2, Offset: ParticleQuadCornerOffset, Location: ParticleCornerLocation},
				{Format: gpu.VertexFloat32x2, Offset: ParticleQuadUVOffset, Location: ParticleUVLocation},
			},
		},
		{
			Stride:   ParticleInstanceStride,
			StepMode: gpu.StepInstance,
			Attributes: []gpu.VertexAttribute{
				{Format: gpu.VertexFloat32x3, Offset: ParticleInstanceCenterOffset, Location: ParticleCenterLocation},
				{Format: gpu.VertexFloat32x3, Offset: ParticleInstanceRightOffset, Location: ParticleRightLocation},
				{Format: gpu.VertexFloat32x3, Offset: ParticleInstanceUpOffset, Location: ParticleUpLocation},
				{Format: gpu.VertexUnorm8x4, Offset: ParticleInstanceColorOffset, Location: ParticleColorLocation},
			},
		},
	}
}

// BlendState returns the [gpu.BlendState] corresponding to m.
func (m ParticleBlendMode) BlendState() *gpu.BlendState {
	switch m {
	case BlendAdditive:
		return gpu.BlendAdditive()
	case BlendPremultiplied:
		return gpu.BlendPremultipliedAlpha()
	default:
		return gpu.BlendAlpha()
	}
}

// ParticlePipelineOptions configures [NewParticlePipeline].
type ParticlePipelineOptions struct {
	// Label names the pipeline in GPU capture tools.
	Label string
	// CameraLayout is the bind group layout for @group(0) (required; matches render3d.CameraBuffer.Layout()).
	CameraLayout *gpu.BindGroupLayout
	// TextureLayout is an optional bind group layout for @group(1) (texture at binding 0, sampler at binding 1). When non-nil, NewParticlePipeline compiles ParticleTexturedWGSL instead of ParticleWGSL.
	TextureLayout *gpu.BindGroupLayout
	// ColorFormat is the color target format; FormatUndefined defaults to dev.SurfaceFormat().
	ColorFormat gpu.TextureFormat
	// DepthFormat enables depth testing when not FormatUndefined.
	DepthFormat gpu.TextureFormat
	// DepthWrite controls whether particles write to the depth buffer; false (the default) is standard for translucent particles.
	DepthWrite bool
	// Blend selects alpha, additive, or premultiplied blending; the zero value is BlendAlpha.
	Blend ParticleBlendMode
	// SoftCircle selects [ParticleSoftCircleWGSL] (smooth radial falloff) when TextureLayout is nil.
	SoftCircle bool
	// SampleCount sets the MSAA sample count (0 or 1 for non-MSAA).
	SampleCount int
	// Retained opts into automatic pipeline recreation across GPU device loss.
	Retained bool
}

// NewParticlePipeline compiles the built-in 3D particle WGSL module and creates a [gpu.Pipeline] configured for [GPUParticles3D.Draw], scoped to cx.
func NewParticlePipeline(cx *core.Scope, dev *gpu.Device, opts ParticlePipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "particles: nil device"}
	}
	if opts.CameraLayout == nil {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "particles: nil CameraLayout"}
	}

	src := ParticleWGSL
	fragEntry := "fs_main"
	layouts := []*gpu.BindGroupLayout{opts.CameraLayout}
	if opts.TextureLayout != nil {
		src = ParticleTexturedWGSL
		fragEntry = "fs_textured"
		layouts = append(layouts, opts.TextureLayout)
	} else if opts.SoftCircle {
		src = ParticleSoftCircleWGSL
	}

	mod, err := gpu.CompileWGSL(src)
	if err != nil {
		return nil, fmt.Errorf("particles: compile shader: %w", err)
	}

	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}

	label := opts.Label
	if label == "" {
		label = "particles3d"
	}

	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: fragEntry},
		Layouts:       layouts,
		VertexBuffers: ParticleVertexLayouts(),
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  gpu.CullNone,
		},
		Targets: []gpu.ColorTargetState{{
			Format:    colorFmt,
			Blend:     opts.Blend.BlendState(),
			WriteMask: gpu.ColorWriteAll,
		}},
		SampleCount: opts.SampleCount,
		Retained:    opts.Retained,
	}
	if opts.DepthFormat != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:       opts.DepthFormat,
			DepthWrite:   opts.DepthWrite,
			DepthCompare: gpu.CompareLessEqual,
		}
	}
	return dev.CreatePipeline(cx, desc)
}

// GPUParticles3DOptions configures [NewGPUParticles3D].
type GPUParticles3DOptions struct {
	// Label names the GPU buffers in capture tools.
	Label string
	// Capacity is the initial maximum number of particle instances the GPU buffer holds before growing; defaults to 256 when <= 0.
	Capacity int
	// Retained keeps the buffers retained across GPU device loss.
	Retained bool
}

// GPUParticles3D owns the static unit-quad geometry and dynamic per-instance vertex buffer needed to draw an [Emitter3D] in a single [gpu.RenderPass.DrawIndexedInstanced] call.
//
// Its buffers belong to the [*core.Scope] passed to [NewGPUParticles3D] and are released automatically when that scope is disposed, or earlier via [GPUParticles3D.Close]. Use it on the engine goroutine, like the [gpu.Device] it came from.
type GPUParticles3D struct {
	cx       *core.Scope
	dev      *gpu.Device
	quadVB   *gpu.Buffer
	quadIB   *gpu.Buffer
	instVB   *gpu.Buffer
	capacity int
	count    uint32
	label    string
	retained bool
	scratch  []byte
}

// NewGPUParticles3D creates the static quad vertex/index buffers and an initial instance vertex buffer on dev, scoped to cx.
func NewGPUParticles3D(cx *core.Scope, dev *gpu.Device, opts GPUParticles3DOptions) (*GPUParticles3D, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "particles: nil device"}
	}
	capWant := opts.Capacity
	if capWant <= 0 {
		capWant = defaultMaxParticles
	}
	label := opts.Label
	if label == "" {
		label = "particles3d"
	}

	quadVB, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " quad vertices",
		Usage:    gpu.UsageVertex | gpu.UsageCopyDst,
		Data:     unitQuadVertexBytes(),
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	quadIB, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " quad indices",
		Usage:    gpu.UsageIndex | gpu.UsageCopyDst,
		Data:     unitQuadIndexBytes(),
		Retained: opts.Retained,
	})
	if err != nil {
		_ = quadVB.Close()
		return nil, err
	}

	instVB, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " instances",
		Size:     uint64(capWant) * ParticleInstanceStride,
		Usage:    gpu.UsageVertex | gpu.UsageCopyDst,
		Retained: opts.Retained,
	})
	if err != nil {
		_ = quadIB.Close()
		_ = quadVB.Close()
		return nil, err
	}

	return &GPUParticles3D{
		cx:       cx,
		dev:      dev,
		quadVB:   quadVB,
		quadIB:   quadIB,
		instVB:   instVB,
		capacity: capWant,
		label:    label,
		retained: opts.Retained,
		scratch:  make([]byte, 0, capWant*ParticleInstanceStride),
	}, nil
}

// Upload packs e's live particles facing (camRight, camUp) into reusable scratch storage and uploads them to the GPU instance buffer.
// If e has more live particles than the current GPU buffer capacity, the instance buffer grows automatically.
func (g *GPUParticles3D) Upload(e *Emitter3D, camRight, camUp math3d.Vec3) error {
	if !g.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "particles: GPUParticles3D buffer is not valid"}
	}
	if e == nil || e.Count() == 0 {
		g.count = 0
		return nil
	}
	g.scratch = e.AppendInstances(g.scratch[:0], camRight, camUp)
	return g.UploadBytes(g.scratch)
}

// UploadBytes uploads pre-packed instance bytes (whose length must be a multiple of [ParticleInstanceStride]) to the GPU instance buffer.
// This allows packing several emitters into one byte slice and drawing them in a single instanced draw call.
func (g *GPUParticles3D) UploadBytes(data []byte) error {
	if !g.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "particles: GPUParticles3D buffer is not valid"}
	}
	if len(data)%ParticleInstanceStride != 0 {
		return fmt.Errorf("particles: instance byte length %d is not a multiple of %d", len(data), ParticleInstanceStride)
	}
	n := len(data) / ParticleInstanceStride
	if n == 0 {
		g.count = 0
		return nil
	}
	if n > g.capacity {
		newCap := max(n, g.capacity*2)
		next, err := g.dev.CreateBuffer(g.cx, gpu.BufferDesc{
			Label:    g.label + " instances",
			Size:     uint64(newCap) * ParticleInstanceStride,
			Usage:    gpu.UsageVertex | gpu.UsageCopyDst,
			Retained: g.retained,
		})
		if err != nil {
			return err
		}
		_ = g.instVB.Close()
		g.instVB = next
		g.capacity = newCap
	}
	g.dev.WriteBuffer(g.instVB, 0, data)
	g.count = uint32(n)
	return nil
}

// Draw binds the unit quad and instance buffers to pass and records one [gpu.RenderPass.DrawIndexedInstanced] call for all uploaded particles.
// The pass must already have a pipeline built with [ParticleVertexLayouts] and its bind groups set. When g is invalid or has 0 uploaded particles, Draw records nothing.
func (g *GPUParticles3D) Draw(pass *gpu.RenderPass) {
	if pass == nil || !g.Valid() || g.count == 0 {
		return
	}
	pass.SetVertexBuffer(0, g.quadVB, 0)
	pass.SetVertexBuffer(1, g.instVB, 0)
	pass.SetIndexBuffer(g.quadIB, gpu.IndexUint16, 0)
	pass.DrawIndexedInstanced(6, g.count, 0, 0, 0)
}

// Valid reports whether all underlying GPU buffers are open and belong to a live device.
func (g *GPUParticles3D) Valid() bool {
	return g != nil && g.quadVB.Valid() && g.quadIB.Valid() && g.instVB.Valid()
}

// Count returns the number of particle instances currently uploaded for drawing.
func (g *GPUParticles3D) Count() uint32 {
	if g == nil {
		return 0
	}
	return g.count
}

// Capacity returns the maximum number of particle instances the current GPU buffer holds without growing.
func (g *GPUParticles3D) Capacity() int {
	if g == nil {
		return 0
	}
	return g.capacity
}

// ByteSize returns the total GPU memory held by the quad and instance buffers.
func (g *GPUParticles3D) ByteSize() uint64 {
	if g == nil {
		return 0
	}
	return g.quadVB.Size() + g.quadIB.Size() + g.instVB.Size()
}

// QuadVertexBuffer returns the static unit-quad vertex buffer (slot 0).
func (g *GPUParticles3D) QuadVertexBuffer() *gpu.Buffer { return g.quadVB }

// QuadIndexBuffer returns the static unit-quad uint16 index buffer.
func (g *GPUParticles3D) QuadIndexBuffer() *gpu.Buffer { return g.quadIB }

// InstanceBuffer returns the dynamic per-instance vertex buffer (slot 1).
func (g *GPUParticles3D) InstanceBuffer() *gpu.Buffer { return g.instVB }

// Close releases all GPU buffers now rather than waiting for scope disposal. Safe to call more than once.
func (g *GPUParticles3D) Close() error {
	if g == nil {
		return nil
	}
	g.count = 0
	return errors.Join(g.instVB.Close(), g.quadIB.Close(), g.quadVB.Close())
}

func unitQuadVertexBytes() []byte {
	// Counter-clockwise corners: BL, BR, TR, TL; UV (0,0) top-left, (0,1) bottom-left.
	verts := [4][4]float32{
		{-0.5, -0.5, 0, 1},
		{0.5, -0.5, 1, 1},
		{0.5, 0.5, 1, 0},
		{-0.5, 0.5, 0, 0},
	}
	out := make([]byte, 4*ParticleQuadVertexStride)
	off := 0
	for _, v := range verts {
		for _, f := range v {
			binary.LittleEndian.PutUint32(out[off:], math.Float32bits(f))
			off += 4
		}
	}
	return out
}

func unitQuadIndexBytes() []byte {
	idx := [6]uint16{0, 1, 2, 0, 2, 3}
	out := make([]byte, len(idx)*2)
	for i, v := range idx {
		binary.LittleEndian.PutUint16(out[i*2:], v)
	}
	return out
}
