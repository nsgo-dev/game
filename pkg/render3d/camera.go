package render3d

import (
	"encoding/binary"
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// CameraGroup is the bind group index the 3D shaders in this repository reserve for the camera: [CameraBuffer.BindGroup] goes in this slot of every 3D pipeline's layouts, and [CameraWGSL] declares it there.
const CameraGroup = 0

// CameraUniformSize is the byte size of the camera uniform block that [CameraUniform.Encode] writes and [CameraWGSL] declares.
const CameraUniformSize = 3*64 + 16

// CameraWGSL declares the camera uniform block at @group(0) @binding(0) as the variable camera. Prepend it to a WGSL module whose vertex stage places points with camera.view_proj; the block's layout matches [CameraUniform.Encode] byte for byte.
//
// The eye field is the homogeneous eye point from [scene3d.Camera.Eye], so normalize(camera.eye.xyz - p * camera.eye.w) is the unit vector from a world point p toward the viewer under both perspective and orthographic cameras.
const CameraWGSL = `struct Camera {
	view: mat4x4<f32>,
	proj: mat4x4<f32>,
	view_proj: mat4x4<f32>,
	eye: vec4<f32>,
}

@group(0) @binding(0) var<uniform> camera: Camera;
`

// CameraUniform is the per-view camera data a 3D shader reads: the view, projection and combined matrices in math3d's column-major layout with [0, 1] clip depth, and the homogeneous eye point.
type CameraUniform struct {
	View     math3d.Mat4
	Proj     math3d.Mat4
	ViewProj math3d.Mat4
	// Eye is (position, 1) for a perspective camera and (direction toward the viewer, 0) for an orthographic one; see [scene3d.Camera.Eye].
	Eye math3d.Vec4
}

// NewCameraUniform returns the uniform data for cam's current pose and projection.
func NewCameraUniform(cam *scene3d.Camera) CameraUniform {
	view := cam.ViewMatrix()
	proj := cam.ProjMatrix()
	return CameraUniform{
		View:     view,
		Proj:     proj,
		ViewProj: proj.Mul(view),
		Eye:      cam.Eye(),
	}
}

// Encode writes u into dst as the little-endian float32 block [CameraWGSL] declares and returns dst[:CameraUniformSize]. It panics if len(dst) is less than CameraUniformSize, whatever dst's capacity.
func (u *CameraUniform) Encode(dst []byte) []byte {
	if len(dst) < CameraUniformSize {
		panic("render3d: CameraUniform.Encode: dst shorter than CameraUniformSize")
	}
	dst = dst[:CameraUniformSize]
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
		off += 4
	}
	for _, m := range [...]*math3d.Mat4{&u.View, &u.Proj, &u.ViewProj} {
		for _, f := range m {
			put(f)
		}
	}
	put(u.Eye.X)
	put(u.Eye.Y)
	put(u.Eye.Z)
	put(u.Eye.W)
	return dst
}

// CameraBuffer holds one camera's uniform buffer on a GPU device together with the bind group layout and bind group that expose it to vertex and fragment shaders at [CameraGroup].
//
// Create it once when the device is ready, call Write with the frame's camera before recording draws, and bind BindGroup at CameraGroup; pipelines list Layout at that index of their PipelineDesc.Layouts. Its resources belong to the scope it was created with and are released with it, or earlier by Close.
type CameraBuffer struct {
	dev     *gpu.Device
	buf     *gpu.Buffer
	layout  *gpu.BindGroupLayout
	group   *gpu.BindGroup
	scratch [CameraUniformSize]byte
}

// NewCameraBuffer creates a camera uniform buffer, its bind group layout and its bind group on dev, owned by cx (nil for resources the caller closes itself, as in headless tests).
func NewCameraBuffer(cx *core.Scope, dev *gpu.Device) (*CameraBuffer, error) {
	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label: "render3d.camera",
		Size:  CameraUniformSize,
		Usage: gpu.UsageUniform | gpu.UsageCopyDst,
	})
	if err != nil {
		return nil, err
	}
	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.camera",
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
		Label:   "render3d.camera",
		Layout:  layout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: buf, Size: CameraUniformSize}},
	})
	if err != nil {
		_ = layout.Close()
		_ = buf.Close()
		return nil, err
	}
	return &CameraBuffer{dev: dev, buf: buf, layout: layout, group: group}, nil
}

// Write uploads u to the buffer; draws recorded after it read the new values. It encodes into storage the CameraBuffer owns, so the only copy made is the one gpu.Device.WriteBuffer takes of the bytes it queues.
func (b *CameraBuffer) Write(u CameraUniform) {
	b.dev.WriteBuffer(b.buf, 0, u.Encode(b.scratch[:]))
}

// WriteCamera uploads cam's current pose and projection; it is Write(NewCameraUniform(cam)).
func (b *CameraBuffer) WriteCamera(cam *scene3d.Camera) {
	b.Write(NewCameraUniform(cam))
}

// Buffer returns the underlying uniform buffer.
func (b *CameraBuffer) Buffer() *gpu.Buffer { return b.buf }

// Layout returns the bind group layout a pipeline lists at [CameraGroup].
func (b *CameraBuffer) Layout() *gpu.BindGroupLayout { return b.layout }

// BindGroup returns the bind group a render pass sets at [CameraGroup].
func (b *CameraBuffer) BindGroup() *gpu.BindGroup { return b.group }

// Close releases the bind group, layout and buffer now rather than with the owning scope. It returns the first error and is safe to call more than once.
func (b *CameraBuffer) Close() error {
	var first error
	for _, c := range []interface{ Close() error }{b.group, b.layout, b.buf} {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
