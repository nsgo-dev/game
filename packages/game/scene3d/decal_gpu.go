package scene3d

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// DecalCameraUniformSize is the byte size of the camera uniform block declared
// in [DecalWGSL] (three column-major mat4x4 matrices plus a vec4 eye point),
// matching render3d.CameraUniformSize byte-for-byte.
const DecalCameraUniformSize = 3*64 + 16

// DecalWGSL is the mobile-compatible forward-rendering WGSL shader for
// box-projected decals (#385). It uses only standard vertex/fragment stages,
// a camera uniform at @group(0) @binding(0) (layout-compatible with
// render3d.CameraWGSL), and an albedo texture + sampler at @group(1). No
// compute shaders, storage buffers, or depth-buffer readback are required, so
// it runs identically on WebGPU, Metal, Vulkan, OpenGL ES 3.0, WebGL 2, and
// NSGo's software renderer.
const DecalWGSL = `struct DecalCamera {
	view: mat4x4<f32>,
	proj: mat4x4<f32>,
	view_proj: mat4x4<f32>,
	eye: vec4<f32>,
}

@group(0) @binding(0) var<uniform> camera: DecalCamera;
@group(1) @binding(0) var albedo_tex: texture_2d<f32>;
@group(1) @binding(1) var albedo_samp: sampler;

struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
	@location(3) color: vec4<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) uv: vec2<f32>,
	@location(1) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.uv = in.uv;
	out.color = in.color;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let tex = textureSample(albedo_tex, albedo_samp, in.uv);
	let out_color = tex * in.color;
	if (out_color.a <= 0.001) {
		discard;
	}
	return out_color;
}
`

// DecalVertexLayout returns the vertex buffer layout matching
// [DecalGeometry.AppendVertexData] and [DecalWGSL]. Each call returns a fresh
// Attributes slice.
func DecalVertexLayout() gpu.VertexBufferLayout {
	return gpu.VertexBufferLayout{
		Stride:   DecalVertexStride,
		StepMode: gpu.StepVertex,
		Attributes: []gpu.VertexAttribute{
			{Format: gpu.VertexFloat32x3, Offset: DecalPositionOffset, Location: DecalPositionLocation},
			{Format: gpu.VertexFloat32x3, Offset: DecalNormalOffset, Location: DecalNormalLocation},
			{Format: gpu.VertexFloat32x2, Offset: DecalUVOffset, Location: DecalUVLocation},
			{Format: gpu.VertexFloat32x4, Offset: DecalColorOffset, Location: DecalColorLocation},
		},
	}
}

// GPUDecalOptions configures [NewGPUDecalRenderer].
type GPUDecalOptions struct {
	// Label prefixes GPU resource labels in capture tools.
	Label string
	// DepthFormat is the depth attachment format of the forward render pass, or
	// gpu.FormatUndefined when drawing without a depth buffer.
	DepthFormat gpu.TextureFormat
	// CameraLayout is an optional bind group layout for @group(0) matching
	// [DecalWGSL] (such as render3d.CameraBuffer.Layout()). When nil, the
	// renderer owns its own camera uniform buffer updated via
	// [GPUDecalRenderer.WriteCamera].
	CameraLayout *gpu.BindGroupLayout
	// Retained opts owned GPU buffers, sampler, and textures into automatic
	// recreation across GPU device loss.
	Retained bool
	// DepthBias and DepthBiasSlopeScale push decal fragments slightly closer in
	// the depth buffer in addition to Decal.Bias along the surface normal. When
	// both are zero and DepthFormat is set, defaults of -1 and -1.0 are used.
	DepthBias           int32
	DepthBiasSlopeScale float32
	// DoubleSided disables back-face culling (gpu.CullNone) when true; the
	// default false uses gpu.CullBack.
	DoubleSided bool
}

type decalTexEntry struct {
	group *gpu.BindGroup
	owned *gpu.Texture
}

// GPUDecalRenderer draws [DecalGeometry] on the forward rendering path with
// alpha blending, depth testing without depth writes, and optional per-TextureID
// albedo textures (#385).
//
// Create it once when the GPU device is ready (in gpu.View OnReady or on a
// software device), upload projected decals with [GPUDecalRenderer.Upload], and
// call [GPUDecalRenderer.Draw] (or [GPUDecalRenderer.DrawWithCamera] when
// sharing a render3d.CameraBuffer) inside the opaque/forward render pass after
// drawing scene geometry.
type GPUDecalRenderer struct {
	cx       *core.Scope
	dev      *gpu.Device
	label    string
	retained bool

	pipeline *gpu.Pipeline

	ownCamera    bool
	camBuf       *gpu.Buffer
	camLayout    *gpu.BindGroupLayout
	camGroup     *gpu.BindGroup
	camScratch   [DecalCameraUniformSize]byte
	texLayout    *gpu.BindGroupLayout
	defaultTex   *gpu.Texture
	defaultSamp  *gpu.Sampler
	defaultGroup *gpu.BindGroup
	textures     map[uint32]decalTexEntry

	vb         *gpu.Buffer
	ib         *gpu.Buffer
	vbCap      uint64
	ibCap      uint64
	vertBytes  []byte
	idxBytes   []byte
	idxFormat  gpu.IndexFormat
	indexCount uint32
	batches    []DecalBatch
}

// NewGPUDecalRenderer compiles [DecalWGSL] and creates the forward decal
// pipeline, default white albedo texture, sampler, and (when opts.CameraLayout
// is nil) an internal camera uniform buffer on dev, scoped to cx.
func NewGPUDecalRenderer(cx *core.Scope, dev *gpu.Device, opts GPUDecalOptions) (*GPUDecalRenderer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "scene3d: nil device"}
	}
	label := opts.Label
	if label == "" {
		label = "scene3d.decal"
	}

	r := &GPUDecalRenderer{
		cx:       cx,
		dev:      dev,
		label:    label,
		retained: opts.Retained,
		textures: make(map[uint32]decalTexEntry),
	}
	cleanupOnErr := true
	defer func() {
		if cleanupOnErr {
			_ = r.Close()
		}
	}()

	mod, err := gpu.CompileWGSL(DecalWGSL)
	if err != nil {
		return nil, fmt.Errorf("scene3d: compile decal shader: %w", err)
	}

	if opts.CameraLayout != nil {
		r.camLayout = opts.CameraLayout
	} else {
		r.ownCamera = true
		r.camBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
			Label:    label + " camera",
			Size:     DecalCameraUniformSize,
			Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
			Retained: opts.Retained,
		})
		if err != nil {
			return nil, err
		}
		r.camLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
			Label: label + " camera layout",
			Entries: []gpu.BindGroupLayoutEntry{{
				Binding:    0,
				Visibility: gpu.StageVertex | gpu.StageFragment,
				Type:       gpu.BindingUniform,
			}},
		})
		if err != nil {
			return nil, err
		}
		r.camGroup, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
			Label:  label + " camera group",
			Layout: r.camLayout,
			Entries: []gpu.BindGroupEntry{{
				Binding: 0,
				Buffer:  r.camBuf,
				Size:    DecalCameraUniformSize,
			}},
		})
		if err != nil {
			return nil, err
		}
	}

	r.texLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " albedo layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		return nil, err
	}

	r.defaultTex, err = dev.CreateTexture(cx, gpu.TextureDesc{
		Label:    label + " white",
		Width:    1,
		Height:   1,
		Format:   gpu.FormatRGBA8Unorm,
		Usage:    gpu.TextureSampled | gpu.TextureCopyDst,
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	dev.WriteTexture(
		r.defaultTex,
		gpu.TextureCopy{Texture: r.defaultTex},
		[]byte{255, 255, 255, 255},
		gpu.DataLayout{BytesPerRow: 4, RowsPerImage: 1},
	)

	r.defaultSamp, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + " sampler",
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterLinear,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	r.defaultGroup, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + " default albedo",
		Layout: r.texLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Texture: r.defaultTex},
			{Binding: 1, Sampler: r.defaultSamp},
		},
	})
	if err != nil {
		return nil, err
	}

	cull := gpu.CullBack
	if opts.DoubleSided {
		cull = gpu.CullNone
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{r.camLayout, r.texLayout},
		VertexBuffers: []gpu.VertexBufferLayout{DecalVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  cull,
		},
		Targets: []gpu.ColorTargetState{{
			Format: dev.SurfaceFormat(),
			Blend:  gpu.BlendAlpha(),
		}},
	}
	if opts.DepthFormat != gpu.FormatUndefined {
		depthBias := opts.DepthBias
		slopeScale := opts.DepthBiasSlopeScale
		if depthBias == 0 && slopeScale == 0 {
			depthBias = -1
			slopeScale = -1
		}
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:              opts.DepthFormat,
			DepthWrite:          false,
			DepthCompare:        gpu.CompareLessEqual,
			DepthBias:           depthBias,
			DepthBiasSlopeScale: slopeScale,
		}
	}

	r.pipeline, err = dev.CreatePipeline(cx, desc)
	if err != nil {
		return nil, err
	}

	cleanupOnErr = false
	return r, nil
}

// Pipeline returns the renderer's forward decal pipeline.
func (r *GPUDecalRenderer) Pipeline() *gpu.Pipeline {
	if r == nil {
		return nil
	}
	return r.pipeline
}

// CameraLayout returns the bind group layout used at @group(0).
func (r *GPUDecalRenderer) CameraLayout() *gpu.BindGroupLayout {
	if r == nil {
		return nil
	}
	return r.camLayout
}

// TextureLayout returns the bind group layout used at @group(1).
func (r *GPUDecalRenderer) TextureLayout() *gpu.BindGroupLayout {
	if r == nil {
		return nil
	}
	return r.texLayout
}

// IndexCount returns the number of decal triangle indices currently uploaded.
func (r *GPUDecalRenderer) IndexCount() uint32 {
	if r == nil {
		return 0
	}
	return r.indexCount
}

// WriteCamera uploads cam's view, projection, view-projection, and eye point
// to the renderer's internal camera uniform buffer. It is a no-op if the
// renderer was constructed with an external CameraLayout or if cam is nil.
func (r *GPUDecalRenderer) WriteCamera(cam *Camera) {
	if r == nil || !r.ownCamera || cam == nil || r.camBuf == nil || !r.camBuf.Valid() {
		return
	}
	view := cam.ViewMatrix()
	proj := cam.ProjMatrix()
	r.WriteCameraMatrices(view, proj, cam.Eye())
}

// WriteCameraMatrices uploads explicit view, projection, and homogeneous eye
// values to the renderer's internal camera uniform buffer.
func (r *GPUDecalRenderer) WriteCameraMatrices(view, proj math3d.Mat4, eye math3d.Vec4) {
	if r == nil || !r.ownCamera || r.camBuf == nil || !r.camBuf.Valid() {
		return
	}
	vp := proj.Mul(view)
	off := 0
	for _, m := range [...]*math3d.Mat4{&view, &proj, &vp} {
		for _, f := range m {
			putF32(r.camScratch[off:], f)
			off += 4
		}
	}
	putF32(r.camScratch[off+0:], eye.X)
	putF32(r.camScratch[off+4:], eye.Y)
	putF32(r.camScratch[off+8:], eye.Z)
	putF32(r.camScratch[off+12:], eye.W)
	r.dev.WriteBuffer(r.camBuf, 0, r.camScratch[:])
}

// SetTexture associates a caller-managed GPU texture with id so decals with
// Decal.TextureID == id sample tex. Passing nil removes the binding and reverts
// id to the default 1x1 white texture.
func (r *GPUDecalRenderer) SetTexture(id uint32, tex *gpu.Texture) error {
	if r == nil || r.dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "scene3d: nil decal renderer"}
	}
	if tex == nil {
		r.releaseTexEntry(id)
		return nil
	}
	grp, err := r.dev.CreateBindGroupDesc(r.cx, gpu.BindGroupDesc{
		Label:  fmt.Sprintf("%s albedo %d", r.label, id),
		Layout: r.texLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Texture: tex},
			{Binding: 1, Sampler: r.defaultSamp},
		},
	})
	if err != nil {
		return err
	}
	r.releaseTexEntry(id)
	r.textures[id] = decalTexEntry{group: grp}
	return nil
}

// UploadTextureRGBA creates a FormatRGBA8Unorm texture of dimensions
// width x height, uploads rgba (4 bytes per pixel, tightly packed), and binds
// it to id. The renderer owns and closes the created texture on replacement or
// Close.
func (r *GPUDecalRenderer) UploadTextureRGBA(id uint32, width, height uint32, rgba []byte) error {
	if r == nil || r.dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "scene3d: nil decal renderer"}
	}
	if width == 0 || height == 0 {
		return errors.New("scene3d: decal texture dimensions must be positive")
	}
	wantBytes := uint64(width) * uint64(height) * 4
	if uint64(len(rgba)) != wantBytes {
		return fmt.Errorf("scene3d: decal texture data length %d does not match %dx%d RGBA (%d bytes)", len(rgba), width, height, wantBytes)
	}
	tex, err := r.dev.CreateTexture(r.cx, gpu.TextureDesc{
		Label:    fmt.Sprintf("%s tex %d", r.label, id),
		Width:    width,
		Height:   height,
		Format:   gpu.FormatRGBA8Unorm,
		Usage:    gpu.TextureSampled | gpu.TextureCopyDst,
		Retained: r.retained,
	})
	if err != nil {
		return err
	}
	r.dev.WriteTexture(
		tex,
		gpu.TextureCopy{Texture: tex},
		rgba,
		gpu.DataLayout{BytesPerRow: width * 4, RowsPerImage: height},
	)
	grp, err := r.dev.CreateBindGroupDesc(r.cx, gpu.BindGroupDesc{
		Label:  fmt.Sprintf("%s albedo %d", r.label, id),
		Layout: r.texLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Texture: tex},
			{Binding: 1, Sampler: r.defaultSamp},
		},
	})
	if err != nil {
		_ = tex.Close()
		return err
	}
	r.releaseTexEntry(id)
	r.textures[id] = decalTexEntry{group: grp, owned: tex}
	return nil
}

func (r *GPUDecalRenderer) releaseTexEntry(id uint32) {
	if prev, ok := r.textures[id]; ok {
		if prev.group != nil {
			_ = prev.group.Close()
		}
		if prev.owned != nil {
			_ = prev.owned.Close()
		}
		delete(r.textures, id)
	}
}

// Upload copies geom's vertices, indices, and batch ranges to GPU buffers,
// reusing previously allocated buffers whenever they are large enough.
func (r *GPUDecalRenderer) Upload(geom *DecalGeometry) error {
	if r == nil || r.dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "scene3d: nil decal renderer"}
	}
	if geom == nil || len(geom.Vertices) == 0 || len(geom.Indices) == 0 {
		r.indexCount = 0
		r.batches = r.batches[:0]
		return nil
	}

	r.vertBytes = geom.AppendVertexData(r.vertBytes[:0])
	r.idxBytes = geom.AppendIndexData(r.idxBytes[:0])
	r.idxFormat = geom.IndexFormat()

	vSize := uint64(len(r.vertBytes))
	if r.vb == nil || !r.vb.Valid() || vSize > r.vbCap {
		newCap := nextBufferCap(r.vbCap, vSize)
		buf, err := r.dev.CreateBuffer(r.cx, gpu.BufferDesc{
			Label:    r.label + " vertices",
			Size:     newCap,
			Usage:    gpu.UsageVertex | gpu.UsageCopyDst,
			Retained: r.retained,
		})
		if err != nil {
			return err
		}
		if r.vb != nil {
			_ = r.vb.Close()
		}
		r.vb = buf
		r.vbCap = newCap
	}

	iSize := uint64(len(r.idxBytes))
	if r.ib == nil || !r.ib.Valid() || iSize > r.ibCap {
		newCap := nextBufferCap(r.ibCap, iSize)
		buf, err := r.dev.CreateBuffer(r.cx, gpu.BufferDesc{
			Label:    r.label + " indices",
			Size:     newCap,
			Usage:    gpu.UsageIndex | gpu.UsageCopyDst,
			Retained: r.retained,
		})
		if err != nil {
			return err
		}
		if r.ib != nil {
			_ = r.ib.Close()
		}
		r.ib = buf
		r.ibCap = newCap
	}

	r.dev.WriteBuffer(r.vb, 0, r.vertBytes)
	r.dev.WriteBuffer(r.ib, 0, r.idxBytes)
	r.indexCount = uint32(len(geom.Indices))
	r.batches = append(r.batches[:0], geom.Batches...)
	if len(r.batches) == 0 {
		r.batches = append(r.batches, DecalBatch{
			FirstIndex: 0,
			IndexCount: r.indexCount,
		})
	}
	return nil
}

// Draw records the uploaded decal draws into pass using the renderer's internal
// camera bind group (updated via [GPUDecalRenderer.WriteCamera]). It is a no-op
// if no decals are uploaded or if the renderer was configured with an external
// CameraLayout (use [GPUDecalRenderer.DrawWithCamera] instead).
func (r *GPUDecalRenderer) Draw(pass *gpu.RenderPass) {
	if r == nil || !r.ownCamera {
		return
	}
	r.DrawWithCamera(pass, r.camGroup)
}

// DrawWithCamera records the uploaded decal draws into pass using cameraGroup
// at @group(0) (for example, render3d.CameraBuffer.BindGroup()). Consecutive
// batches that share the same TextureID are merged into a single DrawIndexed
// call.
func (r *GPUDecalRenderer) DrawWithCamera(pass *gpu.RenderPass, cameraGroup *gpu.BindGroup) {
	if r == nil || pass == nil || r.indexCount == 0 || cameraGroup == nil || !cameraGroup.Valid() {
		return
	}
	if r.pipeline == nil || !r.pipeline.Valid() || r.vb == nil || !r.vb.Valid() || r.ib == nil || !r.ib.Valid() {
		return
	}

	pass.SetPipeline(r.pipeline)
	pass.SetBindGroup(0, cameraGroup)
	pass.SetVertexBuffer(0, r.vb, 0)
	pass.SetIndexBuffer(r.ib, r.idxFormat, 0)

	var boundGroup *gpu.BindGroup
	n := len(r.batches)
	for i := 0; i < n; {
		b := r.batches[i]
		first := b.FirstIndex
		count := b.IndexCount
		texID := b.TextureID
		j := i + 1
		for j < n && r.batches[j].TextureID == texID && r.batches[j].FirstIndex == first+count {
			count += r.batches[j].IndexCount
			j++
		}
		if count > 0 {
			grp := r.textureGroup(texID)
			if grp != nil && grp.Valid() {
				if grp != boundGroup {
					pass.SetBindGroup(1, grp)
					boundGroup = grp
				}
				pass.DrawIndexed(count, first, 0)
			}
		}
		i = j
	}
}

func (r *GPUDecalRenderer) textureGroup(id uint32) *gpu.BindGroup {
	if entry, ok := r.textures[id]; ok && entry.group != nil && entry.group.Valid() {
		return entry.group
	}
	return r.defaultGroup
}

// Close releases all GPU resources owned by r. It is safe to call more than
// once.
func (r *GPUDecalRenderer) Close() error {
	if r == nil {
		return nil
	}
	var err error
	for id, entry := range r.textures {
		if entry.group != nil {
			err = errors.Join(err, entry.group.Close())
		}
		if entry.owned != nil {
			err = errors.Join(err, entry.owned.Close())
		}
		delete(r.textures, id)
	}
	closeIf := func(c interface{ Close() error }) {
		if c != nil {
			err = errors.Join(err, c.Close())
		}
	}
	closeIf(r.ib)
	closeIf(r.vb)
	closeIf(r.pipeline)
	closeIf(r.defaultGroup)
	closeIf(r.defaultSamp)
	closeIf(r.defaultTex)
	closeIf(r.texLayout)
	if r.ownCamera {
		closeIf(r.camGroup)
		closeIf(r.camLayout)
		closeIf(r.camBuf)
	}
	r.ib, r.vb, r.pipeline = nil, nil, nil
	r.defaultGroup, r.defaultSamp, r.defaultTex, r.texLayout = nil, nil, nil, nil
	if r.ownCamera {
		r.camGroup, r.camLayout, r.camBuf = nil, nil, nil
	}
	r.indexCount = 0
	r.batches = r.batches[:0]
	return err
}

func nextBufferCap(cur, need uint64) uint64 {
	c := cur
	if c < 64 {
		c = 64
	}
	for c < need {
		c *= 2
	}
	return (c + 3) &^ 3
}
