package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Interleaved vertex layout written by [ColoredMesh.AppendVertexData] and described by [ColoredVertexLayout].
// Locations 0..2 match [MeshVertexLayout] identically, extended with linear RGBA vertex color at @location(3).
// Missing normals and UVs are written as zeros; missing vertex colors are written as linear white (1, 1, 1, 1).
const (
	// ColoredVertexStride is the byte size of one interleaved colored vertex: position (12), normal (12), UV (8), and linear RGBA color (16).
	ColoredVertexStride = 48

	// ColoredPositionOffset is the byte offset of the float32x3 position inside a colored vertex.
	ColoredPositionOffset = MeshPositionOffset
	// ColoredNormalOffset is the byte offset of the float32x3 normal inside a colored vertex.
	ColoredNormalOffset = MeshNormalOffset
	// ColoredUVOffset is the byte offset of the float32x2 texture coordinate inside a colored vertex.
	ColoredUVOffset = MeshUVOffset
	// ColoredColorOffset is the byte offset of the float32x4 linear RGBA vertex color inside a colored vertex.
	ColoredColorOffset = 32

	// ColoredPositionLocation is the shader @location of the position attribute.
	ColoredPositionLocation = MeshPositionLocation
	// ColoredNormalLocation is the shader @location of the normal attribute.
	ColoredNormalLocation = MeshNormalLocation
	// ColoredUVLocation is the shader @location of the texture coordinate attribute.
	ColoredUVLocation = MeshUVLocation
	// ColoredColorLocation is the shader @location of the linear RGBA vertex color attribute (glTF COLOR_0).
	ColoredColorLocation = 3
)

const (
	// UnlitMaterialGroup is the bind group index reserved by [UnlitWGSL] and [GPUUnlitMaterial.Bind] for unlit material uniforms, base color texture, and sampler, placed alongside [CameraGroup] at group 0.
	UnlitMaterialGroup = 1

	// UnlitUniformSize is the byte size of the unlit material uniform block written by [AppendUnlitUniforms] and declared by [UnlitWGSL]:
	//   base_color_factor: vec4<f32> (16 bytes, offset 0)
	//   params:            vec4<f32> (16 bytes, offset 16) — (alpha_cutoff, alpha_mode, use_vertex_color, 0)
	UnlitUniformSize = 32
)

// ErrInvalidUnlitMaterial is wrapped by every error returned when validating, converting, creating, or updating an unlit material or unlit pipeline.
var ErrInvalidUnlitMaterial = errors.New("render3d: invalid unlit material")

// ColoredMesh is indexed or non-indexed triangle-list geometry with optional linear RGBA vertex colors (glTF COLOR_0), ready to upload to a [gpu.Device] via [UploadColoredMesh].
//
// Positions is required. Normals, UVs, and Colors are optional: each must be empty or have one entry per position. When Colors is empty, [ColoredMesh.AppendVertexData] fills every vertex with linear opaque white (1, 1, 1, 1) so meshes with and without vertex colors share one [ColoredVertexLayout] and pipeline.
type ColoredMesh struct {
	Positions []math3d.Vec3
	Normals   []math3d.Vec3
	UVs       []math3d.Vec2
	Colors    []math3d.Vec4
	Indices   []uint32
}

// Mesh returns the base [Mesh] geometry (positions, normals, UVs, and indices) without vertex colors.
func (m ColoredMesh) Mesh() Mesh {
	return Mesh{
		Positions: m.Positions,
		Normals:   m.Normals,
		UVs:       m.UVs,
		Indices:   m.Indices,
	}
}

// HasVertexColors reports whether m carries explicit per-vertex colors.
func (m ColoredMesh) HasVertexColors() bool { return len(m.Colors) > 0 }

// Validate reports whether m can be uploaded: all [Mesh.Validate] rules plus Colors empty or matching len(Positions) with finite components. Every error wraps [ErrInvalidMesh].
func (m ColoredMesh) Validate() error {
	if err := m.Mesh().Validate(); err != nil {
		return err
	}
	n := len(m.Positions)
	if len(m.Colors) != 0 && len(m.Colors) != n {
		return fmt.Errorf("%w: %d colors for %d positions", ErrInvalidMesh, len(m.Colors), n)
	}
	for i, c := range m.Colors {
		if !finite(c.X) || !finite(c.Y) || !finite(c.Z) || !finite(c.W) {
			return fmt.Errorf("%w: color %d is not finite", ErrInvalidMesh, i)
		}
	}
	return nil
}

// VertexCount returns the number of vertices, len(m.Positions).
func (m ColoredMesh) VertexCount() int { return len(m.Positions) }

// Indexed reports whether m has an index list.
func (m ColoredMesh) Indexed() bool { return len(m.Indices) != 0 }

// IndexFormat returns [gpu.IndexUint16] when every vertex fits below 0xFFFF (kept free for WebGL 2 primitive restart), otherwise [gpu.IndexUint32].
func (m ColoredMesh) IndexFormat() gpu.IndexFormat { return m.Mesh().IndexFormat() }

// Bounds returns the axis-aligned box around m's positions, or [math3d.AABBEmpty] when m has none.
func (m ColoredMesh) Bounds() math3d.AABB { return m.Mesh().Bounds() }

// VertexDataSize returns the byte length [ColoredMesh.AppendVertexData] appends.
func (m ColoredMesh) VertexDataSize() int { return len(m.Positions) * ColoredVertexStride }

// IndexDataSize returns the byte length [ColoredMesh.AppendIndexData] appends, zero-padded to a multiple of four bytes.
func (m ColoredMesh) IndexDataSize() int { return m.Mesh().IndexDataSize() }

// AppendVertexData appends m's vertices to dst in the little-endian interleaved layout of [ColoredVertexLayout] and returns the extended slice. Missing normals and UVs are written as zeros; missing vertex colors are written as linear white (1, 1, 1, 1). It allocates only when dst lacks capacity for [ColoredMesh.VertexDataSize] more bytes. Call [ColoredMesh.Validate] first.
func (m ColoredMesh) AppendVertexData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, m.VertexDataSize())
	out := dst[start:]
	n := len(m.Positions)
	hasNormals := len(m.Normals) == n
	hasUVs := len(m.UVs) == n
	hasColors := len(m.Colors) == n
	oneBits := math.Float32bits(1)
	for i, p := range m.Positions {
		v := out[i*ColoredVertexStride : (i+1)*ColoredVertexStride]
		putVec3(v[ColoredPositionOffset:], p)
		if hasNormals {
			putVec3(v[ColoredNormalOffset:], m.Normals[i])
		} else {
			clear(v[ColoredNormalOffset:ColoredUVOffset])
		}
		if hasUVs {
			binary.LittleEndian.PutUint32(v[ColoredUVOffset:], math.Float32bits(m.UVs[i].X))
			binary.LittleEndian.PutUint32(v[ColoredUVOffset+4:], math.Float32bits(m.UVs[i].Y))
		} else {
			clear(v[ColoredUVOffset:ColoredColorOffset])
		}
		if hasColors {
			c := m.Colors[i]
			binary.LittleEndian.PutUint32(v[ColoredColorOffset:], math.Float32bits(c.X))
			binary.LittleEndian.PutUint32(v[ColoredColorOffset+4:], math.Float32bits(c.Y))
			binary.LittleEndian.PutUint32(v[ColoredColorOffset+8:], math.Float32bits(c.Z))
			binary.LittleEndian.PutUint32(v[ColoredColorOffset+12:], math.Float32bits(c.W))
		} else {
			binary.LittleEndian.PutUint32(v[ColoredColorOffset:], oneBits)
			binary.LittleEndian.PutUint32(v[ColoredColorOffset+4:], oneBits)
			binary.LittleEndian.PutUint32(v[ColoredColorOffset+8:], oneBits)
			binary.LittleEndian.PutUint32(v[ColoredColorOffset+12:], oneBits)
		}
	}
	return dst
}

// AppendIndexData appends m's indices to dst at [ColoredMesh.IndexFormat] width, little-endian and zero-padded to [ColoredMesh.IndexDataSize]. It allocates only when dst lacks capacity. Call [ColoredMesh.Validate] first.
func (m ColoredMesh) AppendIndexData(dst []byte) []byte {
	return m.Mesh().AppendIndexData(dst)
}

// ColoredVertexLayout returns the vertex buffer layout of every [GPUColoredMesh] for [gpu.PipelineDesc].VertexBuffers: one per-vertex buffer of [ColoredVertexStride] bytes with position at location 0, normal at location 1, UV at location 2, and linear RGBA color at location 3. Each call returns a fresh Attributes slice.
func ColoredVertexLayout() gpu.VertexBufferLayout {
	return gpu.VertexBufferLayout{
		Stride:   ColoredVertexStride,
		StepMode: gpu.StepVertex,
		Attributes: []gpu.VertexAttribute{
			{Format: gpu.VertexFloat32x3, Offset: ColoredPositionOffset, Location: ColoredPositionLocation},
			{Format: gpu.VertexFloat32x3, Offset: ColoredNormalOffset, Location: ColoredNormalLocation},
			{Format: gpu.VertexFloat32x2, Offset: ColoredUVOffset, Location: ColoredUVLocation},
			{Format: gpu.VertexFloat32x4, Offset: ColoredColorOffset, Location: ColoredColorLocation},
		},
	}
}

// ColoredMeshFromGLTF extracts a triangle-list [ColoredMesh] from doc.Meshes[meshIndex].Primitives[primIndex], including decoded linear RGB/RGBA vertex colors from COLOR_0 (where glTF VEC3 colors have alpha 1.0) and optional KHR_texture_transform UVs when the primitive's material specifies a baseColorTexture transform or UV set.
//
// Returns an error wrapping [ErrInvalidMesh] if doc is nil, either index is out of range, the primitive topology is not [gltf.ModeTriangles], or the resulting mesh fails validation.
func ColoredMeshFromGLTF(doc *gltf.Document, meshIndex, primIndex int) (ColoredMesh, error) {
	if doc == nil {
		return ColoredMesh{}, fmt.Errorf("%w: nil glTF document", ErrInvalidMesh)
	}
	if meshIndex < 0 || meshIndex >= len(doc.Meshes) {
		return ColoredMesh{}, fmt.Errorf("%w: glTF mesh index %d out of range [0, %d)", ErrInvalidMesh, meshIndex, len(doc.Meshes))
	}
	mesh := doc.Meshes[meshIndex]
	if primIndex < 0 || primIndex >= len(mesh.Primitives) {
		return ColoredMesh{}, fmt.Errorf("%w: glTF mesh %d primitive index %d out of range [0, %d)", ErrInvalidMesh, meshIndex, primIndex, len(mesh.Primitives))
	}
	prim := mesh.Primitives[primIndex]
	if prim.Mode != gltf.ModeTriangles {
		return ColoredMesh{}, fmt.Errorf("%w: glTF mesh %d primitive %d mode %d is not triangles", ErrInvalidMesh, meshIndex, primIndex, prim.Mode)
	}

	uvs := slices.Clone(prim.TexCoords0)
	if prim.Material >= 0 && prim.Material < len(doc.Materials) {
		if ref := doc.Materials[prim.Material].BaseColorTexture; ref != nil {
			if ref.EffectiveTexCoord() != 0 || (ref.Transform != nil && !ref.Transform.IsIdentity()) {
				transformed, err := doc.TransformTexCoords(prim, ref, nil)
				if err != nil {
					return ColoredMesh{}, fmt.Errorf("%w: glTF mesh %d primitive %d baseColorTexture UVs: %v", ErrInvalidMesh, meshIndex, primIndex, err)
				}
				uvs = transformed
			}
		}
	}

	cm := ColoredMesh{
		Positions: slices.Clone(prim.Positions),
		Normals:   slices.Clone(prim.Normals),
		UVs:       uvs,
		Colors:    slices.Clone(prim.Colors0),
		Indices:   slices.Clone(prim.Indices),
	}
	if err := cm.Validate(); err != nil {
		return ColoredMesh{}, err
	}
	return cm, nil
}

// GPUColoredMesh is a [ColoredMesh] uploaded to a GPU device's vertex and optional index buffers using [ColoredVertexLayout]. Its buffers belong to the scope passed to [UploadColoredMesh] and are released when that scope is disposed or when [GPUColoredMesh.Close] is called.
type GPUColoredMesh struct {
	vertices        *gpu.Buffer
	indices         *gpu.Buffer
	indexFormat     gpu.IndexFormat
	vertexCount     uint32
	indexCount      uint32
	bounds          math3d.AABB
	hasVertexColors bool
}

// UploadColoredMesh validates m and creates its vertex buffer and, when m is indexed, its index buffer on dev, scoped to cx. A malformed mesh returns an error wrapping [ErrInvalidMesh] before anything is created; a nil or lost device returns [gpu.ErrDeviceLost].
func UploadColoredMesh(cx *core.Scope, dev *gpu.Device, m ColoredMesh, opts MeshUpload) (*GPUColoredMesh, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	g := &GPUColoredMesh{
		vertexCount:     uint32(len(m.Positions)),
		indexCount:      uint32(len(m.Indices)),
		indexFormat:     m.IndexFormat(),
		bounds:          m.Bounds(),
		hasVertexColors: m.HasVertexColors(),
	}
	var err error
	g.vertices, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    bufferLabel(opts.Label, "colored vertices"),
		Usage:    gpu.UsageVertex | gpu.UsageCopyDst,
		Data:     m.AppendVertexData(nil),
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	if m.Indexed() {
		g.indices, err = dev.CreateBuffer(cx, gpu.BufferDesc{
			Label:    bufferLabel(opts.Label, "colored indices"),
			Usage:    gpu.UsageIndex | gpu.UsageCopyDst,
			Data:     m.AppendIndexData(nil),
			Retained: opts.Retained,
		})
		if err != nil {
			_ = g.vertices.Close()
			return nil, err
		}
	}
	return g, nil
}

// VertexBuffer returns the interleaved vertex buffer laid out as [ColoredVertexLayout] describes, or nil for a nil receiver.
func (g *GPUColoredMesh) VertexBuffer() *gpu.Buffer {
	if g == nil {
		return nil
	}
	return g.vertices
}

// IndexBuffer returns the index buffer, or nil for a nil or non-indexed mesh.
func (g *GPUColoredMesh) IndexBuffer() *gpu.Buffer {
	if g == nil {
		return nil
	}
	return g.indices
}

// IndexFormat returns the width of the index buffer's entries.
func (g *GPUColoredMesh) IndexFormat() gpu.IndexFormat {
	if g == nil {
		return 0
	}
	return g.indexFormat
}

// Indexed reports whether the mesh draws through an index buffer.
func (g *GPUColoredMesh) Indexed() bool { return g != nil && g.indices != nil }

// VertexCount returns the number of vertices in the vertex buffer.
func (g *GPUColoredMesh) VertexCount() uint32 {
	if g == nil {
		return 0
	}
	return g.vertexCount
}

// IndexCount returns the number of indices, or zero for a non-indexed mesh.
func (g *GPUColoredMesh) IndexCount() uint32 {
	if g == nil {
		return 0
	}
	return g.indexCount
}

// Triangles returns the number of triangles one draw of the mesh rasterizes.
func (g *GPUColoredMesh) Triangles() uint32 {
	if g == nil {
		return 0
	}
	if g.indices != nil {
		return g.indexCount / 3
	}
	return g.vertexCount / 3
}

// Bounds returns the object-space box around the mesh's positions, computed at upload.
func (g *GPUColoredMesh) Bounds() math3d.AABB {
	if g == nil {
		return math3d.AABBEmpty()
	}
	return g.bounds
}

// HasVertexColors reports whether the uploaded [ColoredMesh] provided explicit vertex colors (as opposed to default white).
func (g *GPUColoredMesh) HasVertexColors() bool {
	return g != nil && g.hasVertexColors
}

// ByteSize returns the GPU memory held by the mesh's buffers for [RenderStats] and [Budget] accounting. It is zero for a nil mesh.
func (g *GPUColoredMesh) ByteSize() uint64 {
	if g == nil {
		return 0
	}
	n := g.vertices.Size()
	if g.indices != nil {
		n += g.indices.Size()
	}
	return n
}

// Valid reports whether the mesh's buffers are open and belong to a live device.
func (g *GPUColoredMesh) Valid() bool {
	if g == nil || !g.vertices.Valid() {
		return false
	}
	return g.indices == nil || g.indices.Valid()
}

// Bind binds the vertex buffer to slot 0 and, when indexed, the index buffer to pass. It records nothing when the mesh is not [GPUColoredMesh.Valid].
func (g *GPUColoredMesh) Bind(pass *gpu.RenderPass) {
	if !g.Valid() {
		return
	}
	pass.SetVertexBuffer(0, g.vertices, 0)
	if g.indices != nil {
		pass.SetIndexBuffer(g.indices, g.indexFormat, 0)
	}
}

// Draw binds the mesh to pass and draws all of its triangles once. It records nothing when the mesh is not [GPUColoredMesh.Valid].
func (g *GPUColoredMesh) Draw(pass *gpu.RenderPass) {
	if !g.Valid() {
		return
	}
	g.Bind(pass)
	if g.indices != nil {
		pass.DrawIndexed(g.indexCount, 0, 0)
		return
	}
	pass.Draw(g.vertexCount, 0)
}

// Close releases the mesh's buffers now rather than when their scope is disposed. It is safe to call on a nil receiver or more than once.
func (g *GPUColoredMesh) Close() error {
	if g == nil {
		return nil
	}
	err := g.vertices.Close()
	if g.indices != nil {
		err = errors.Join(err, g.indices.Close())
	}
	return err
}

// UnlitAlphaMode controls how an [UnlitMaterial] interprets its combined alpha channel, matching the glTF 2.0 alphaMode specification.
type UnlitAlphaMode uint8

const (
	// UnlitAlphaOpaque ignores alpha and outputs alpha = 1.0 with depth writes enabled.
	UnlitAlphaOpaque UnlitAlphaMode = iota
	// UnlitAlphaMask discards fragments whose combined alpha is strictly less than AlphaCutoff and outputs alpha = 1.0 for surviving fragments.
	UnlitAlphaMask
	// UnlitAlphaBlend blends fragments using straight-alpha blending ([gpu.BlendAlpha]) with depth writes disabled.
	UnlitAlphaBlend
)

// String returns a human-readable name for m.
func (m UnlitAlphaMode) String() string {
	switch m {
	case UnlitAlphaOpaque:
		return "opaque"
	case UnlitAlphaMask:
		return "mask"
	case UnlitAlphaBlend:
		return "blend"
	default:
		return "unknown"
	}
}

// BlendMode converts m to the matching [render3d.BlendMode] for [Queue] sorting and batching.
func (m UnlitAlphaMode) BlendMode() BlendMode {
	switch m {
	case UnlitAlphaMask:
		return BlendAlphaMask
	case UnlitAlphaBlend:
		return BlendAlphaBlend
	default:
		return BlendOpaque
	}
}

func validUnlitAlphaMode(m UnlitAlphaMode) bool {
	return m == UnlitAlphaOpaque || m == UnlitAlphaMask || m == UnlitAlphaBlend
}

// UnlitMaterial describes a glTF KHR_materials_unlit surface: flat color unaffected by scene lighting, computed in linear space as
//
//	baseColor = BaseColorFactor * baseColorTexture(uv) * vertexColor
//
// where BaseColorTexture defaults to a 1x1 white sRGB texture when nil and vertexColor is COLOR_0 (or linear white (1, 1, 1, 1) when UseVertexColor is false or the mesh has no vertex colors).
type UnlitMaterial struct {
	// BaseColorFactor is the linear RGBA multiplier applied to the texture and vertex color. Use [DefaultUnlitMaterial] for the glTF default [1, 1, 1, 1].
	BaseColorFactor [4]float32
	// BaseColorTexture is an optional base-color texture (typically uploaded with TextureUpload{SRGB: true}). When nil, [NewGPUUnlitMaterial] binds a 1x1 white sRGB fallback texture.
	BaseColorTexture *GPUTexture
	// BaseColorSampler is an optional sampler for BaseColorTexture. When nil, [NewGPUUnlitMaterial] binds a default linear-repeat sampler.
	BaseColorSampler *GPUSampler
	// AlphaMode selects opaque, alpha-masked cutout, or alpha-blended transparency.
	AlphaMode UnlitAlphaMode
	// AlphaCutoff is the discard threshold for [UnlitAlphaMask] (default 0.5). Fragments with combined alpha < AlphaCutoff are discarded.
	AlphaCutoff float32
	// DoubleSided disables back-face culling when building a pipeline for this material.
	DoubleSided bool
	// UseVertexColor enables multiplying the material color by the mesh's @location(3) linear RGBA vertex color (glTF COLOR_0).
	UseVertexColor bool
}

// DefaultUnlitMaterial returns the glTF 2.0 KHR_materials_unlit defaults: opaque white BaseColorFactor [1, 1, 1, 1], AlphaMode UnlitAlphaOpaque, AlphaCutoff 0.5, single-sided, and UseVertexColor enabled.
func DefaultUnlitMaterial() UnlitMaterial {
	return UnlitMaterial{
		BaseColorFactor: [4]float32{1, 1, 1, 1},
		AlphaMode:       UnlitAlphaOpaque,
		AlphaCutoff:     0.5,
		UseVertexColor:  true,
	}
}

// BlendMode returns the [render3d.BlendMode] corresponding to m.AlphaMode for [Queue] sorting.
func (m UnlitMaterial) BlendMode() BlendMode { return m.AlphaMode.BlendMode() }

// Validate checks that m's uniform parameters and any attached GPU texture/sampler handles are valid. Every error wraps [ErrInvalidUnlitMaterial].
func (m UnlitMaterial) Validate() error {
	if err := m.validateUniforms(); err != nil {
		return err
	}
	if m.BaseColorTexture != nil && !m.BaseColorTexture.Valid() {
		return fmt.Errorf("%w: base color texture is not valid", ErrInvalidUnlitMaterial)
	}
	if m.BaseColorSampler != nil && !m.BaseColorSampler.Valid() {
		return fmt.Errorf("%w: base color sampler is not valid", ErrInvalidUnlitMaterial)
	}
	return nil
}

func (m UnlitMaterial) validateUniforms() error {
	for i, c := range m.BaseColorFactor {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: BaseColorFactor[%d] = %v must be finite and non-negative", ErrInvalidUnlitMaterial, i, c)
		}
	}
	if !validUnlitAlphaMode(m.AlphaMode) {
		return fmt.Errorf("%w: invalid alpha mode %d", ErrInvalidUnlitMaterial, m.AlphaMode)
	}
	if !finite(m.AlphaCutoff) || m.AlphaCutoff < 0 {
		return fmt.Errorf("%w: AlphaCutoff = %v must be finite and non-negative", ErrInvalidUnlitMaterial, m.AlphaCutoff)
	}
	return nil
}

// AppendUnlitUniforms appends the 32-byte little-endian uniform block matching [UnlitWGSL]'s UnlitUniforms struct to dst and returns the extended slice. It allocates zero bytes when cap(dst)-len(dst) >= [UnlitUniformSize].
func AppendUnlitUniforms(dst []byte, m UnlitMaterial) []byte {
	start := len(dst)
	dst = grow(dst, UnlitUniformSize)
	out := dst[start:]
	binary.LittleEndian.PutUint32(out[0:], math.Float32bits(m.BaseColorFactor[0]))
	binary.LittleEndian.PutUint32(out[4:], math.Float32bits(m.BaseColorFactor[1]))
	binary.LittleEndian.PutUint32(out[8:], math.Float32bits(m.BaseColorFactor[2]))
	binary.LittleEndian.PutUint32(out[12:], math.Float32bits(m.BaseColorFactor[3]))
	binary.LittleEndian.PutUint32(out[16:], math.Float32bits(m.AlphaCutoff))
	binary.LittleEndian.PutUint32(out[20:], math.Float32bits(float32(m.AlphaMode)))
	var useVC float32
	if m.UseVertexColor {
		useVC = 1
	}
	binary.LittleEndian.PutUint32(out[24:], math.Float32bits(useVC))
	binary.LittleEndian.PutUint32(out[28:], 0)
	return dst
}

// UnlitMaterialFromGLTF extracts an [UnlitMaterial] from doc.Materials[matIndex], verifying that the material enables KHR_materials_unlit (mat.Unlit == true). Optional pre-uploaded tex and samp are attached as BaseColorTexture and BaseColorSampler.
//
// Returns an error wrapping [ErrInvalidUnlitMaterial] if doc is nil, matIndex is out of range, the material does not declare KHR_materials_unlit, or the resulting material fails validation.
func UnlitMaterialFromGLTF(doc *gltf.Document, matIndex int, tex *GPUTexture, samp *GPUSampler) (UnlitMaterial, error) {
	if doc == nil {
		return UnlitMaterial{}, fmt.Errorf("%w: nil glTF document", ErrInvalidUnlitMaterial)
	}
	if matIndex < 0 || matIndex >= len(doc.Materials) {
		return UnlitMaterial{}, fmt.Errorf("%w: glTF material index %d out of range [0, %d)", ErrInvalidUnlitMaterial, matIndex, len(doc.Materials))
	}
	gm := doc.Materials[matIndex]
	if !gm.Unlit {
		return UnlitMaterial{}, fmt.Errorf("%w: glTF material %d (%q) does not declare KHR_materials_unlit", ErrInvalidUnlitMaterial, matIndex, gm.Name)
	}
	alphaMode, err := unlitAlphaModeFromGLTF(gm.AlphaMode)
	if err != nil {
		return UnlitMaterial{}, err
	}
	mat := UnlitMaterial{
		BaseColorFactor: [4]float32{
			gm.BaseColorFactor.X,
			gm.BaseColorFactor.Y,
			gm.BaseColorFactor.Z,
			gm.BaseColorFactor.W,
		},
		BaseColorTexture: tex,
		BaseColorSampler: samp,
		AlphaMode:        alphaMode,
		AlphaCutoff:      gm.AlphaCutoff,
		DoubleSided:      gm.DoubleSided,
		UseVertexColor:   true,
	}
	if err := mat.Validate(); err != nil {
		return UnlitMaterial{}, err
	}
	return mat, nil
}

func unlitAlphaModeFromGLTF(mode gltf.AlphaMode) (UnlitAlphaMode, error) {
	switch mode {
	case "", gltf.AlphaOpaque:
		return UnlitAlphaOpaque, nil
	case gltf.AlphaMask:
		return UnlitAlphaMask, nil
	case gltf.AlphaBlend:
		return UnlitAlphaBlend, nil
	default:
		return 0, fmt.Errorf("%w: unsupported glTF alphaMode %q", ErrInvalidUnlitMaterial, mode)
	}
}

// UnlitSamplerFromGLTF converts a [gltf.Sampler] into [SamplerOptions] suitable for [NewGPUSampler], defaulting unset filter modes to linear and unset wrap modes to repeat per the glTF 2.0 specification.
func UnlitSamplerFromGLTF(s gltf.Sampler) SamplerOptions {
	opts := SamplerLinearRepeat()
	opts.Label = s.Name
	switch s.MagFilter {
	case gltf.FilterNearest:
		opts.MagFilter = gpu.FilterNearest
	case gltf.FilterLinear, 0:
		opts.MagFilter = gpu.FilterLinear
	}
	switch s.MinFilter {
	case gltf.FilterNearest, gltf.FilterNearestMipmapNearest:
		opts.MinFilter = gpu.FilterNearest
		opts.MipmapFilter = gpu.FilterNearest
	case gltf.FilterNearestMipmapLinear:
		opts.MinFilter = gpu.FilterNearest
		opts.MipmapFilter = gpu.FilterLinear
	case gltf.FilterLinearMipmapNearest:
		opts.MinFilter = gpu.FilterLinear
		opts.MipmapFilter = gpu.FilterNearest
	case gltf.FilterLinear, gltf.FilterLinearMipmapLinear, 0:
		opts.MinFilter = gpu.FilterLinear
		opts.MipmapFilter = gpu.FilterLinear
	}
	opts.AddressU = gltfWrapToGPU(s.WrapS)
	opts.AddressV = gltfWrapToGPU(s.WrapT)
	return opts
}

func gltfWrapToGPU(w gltf.WrapMode) gpu.AddressMode {
	switch w {
	case gltf.WrapClampToEdge:
		return gpu.AddressClampToEdge
	case gltf.WrapMirroredRepeat:
		return gpu.AddressMirrorRepeat
	default:
		return gpu.AddressRepeat
	}
}

// UnlitMaterialOptions configures [NewGPUUnlitMaterial] and [UploadGLTFUnlitMaterial].
type UnlitMaterialOptions struct {
	// Label names the material's uniform buffer, bind group, and any fallback resources in GPU capture tools.
	Label string
	// Retained keeps CPU shadow copies of the uniform buffer and any material-owned fallback or decoded texture/sampler so NSGo recreates the material and its bind group after a device loss. Caller-supplied BaseColorTexture and BaseColorSampler must also be created with Retained: true for the bind group to restore automatically.
	Retained bool
	// Mipmaps enables CPU linear-light mipmap generation when [UploadGLTFUnlitMaterial] uploads a decoded glTF baseColorTexture.
	Mipmaps bool
}

// UnlitMaterialBindingLayout creates the [gpu.BindGroupLayout] expected at [UnlitMaterialGroup] (@group(1)) by [UnlitWGSL]:
//   - binding 0: uniform buffer ([UnlitUniformSize] bytes, vertex & fragment visible)
//   - binding 1: sampled 2D base-color texture (fragment visible)
//   - binding 2: sampler (fragment visible)
func UnlitMaterialBindingLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.unlit.material",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
}

// GPUUnlitMaterial holds the GPU uniform buffer, bind group, and any fallback 1x1 white sRGB texture or default sampler for an [UnlitMaterial].
//
// Its resources belong to the [core.Scope] passed at creation and are released when that scope is disposed or when [GPUUnlitMaterial.Close] is called. It does not cache a [*gpu.Device] pointer, so a retained material remains updatable via [GPUUnlitMaterial.Update] after a device loss and restore.
type GPUUnlitMaterial struct {
	mat       UnlitMaterial
	boundTex  *GPUTexture // caller-provided texture (nil if fallback ownedTex is used)
	boundSamp *GPUSampler // caller-provided sampler (nil if fallback ownedSamp is used)
	ownedTex  *GPUTexture // fallback or glTF-decoded texture closed with this material
	ownedSamp *GPUSampler // fallback or glTF-created sampler closed with this material
	tex       *GPUTexture // effective texture bound in group
	samp      *GPUSampler // effective sampler bound in group
	buf       *gpu.Buffer
	group     *gpu.BindGroup
	scratch   [UnlitUniformSize]byte
}

// NewGPUUnlitMaterial validates mat, uploads its uniform block, allocates a 1x1 white sRGB fallback texture and/or default linear-repeat sampler whenever BaseColorTexture or BaseColorSampler is nil, and creates a bind group matching layout (from [UnlitMaterialBindingLayout]).
func NewGPUUnlitMaterial(cx *core.Scope, dev *gpu.Device, layout *gpu.BindGroupLayout, mat UnlitMaterial, opts UnlitMaterialOptions) (*GPUUnlitMaterial, error) {
	if err := mat.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if layout == nil || !layout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid unlit material bind group layout"}
	}
	return newGPUUnlitMaterialWithOwnership(cx, dev, layout, mat, opts, false, false)
}

func newGPUUnlitMaterialWithOwnership(cx *core.Scope, dev *gpu.Device, layout *gpu.BindGroupLayout, mat UnlitMaterial, opts UnlitMaterialOptions, ownTex, ownSamp bool) (*GPUUnlitMaterial, error) {
	label := opts.Label
	if label == "" {
		label = "render3d.unlit"
	}

	var (
		ownedTex  *GPUTexture
		ownedSamp *GPUSampler
		tex       = mat.BaseColorTexture
		samp      = mat.BaseColorSampler
		err       error
	)
	if ownTex {
		ownedTex = tex
	}
	if ownSamp {
		ownedSamp = samp
	}

	cleanup := func() {
		if ownedTex != nil {
			_ = ownedTex.Close()
		}
		if ownedSamp != nil {
			_ = ownedSamp.Close()
		}
	}

	if tex == nil {
		ownedTex, err = uploadUnlitWhiteTexture(cx, dev, label+" white", opts.Retained)
		if err != nil {
			cleanup()
			return nil, err
		}
		tex = ownedTex
	}
	if samp == nil {
		sampOpts := SamplerLinearRepeat()
		sampOpts.Label = label + " sampler"
		sampOpts.Retained = opts.Retained
		ownedSamp, err = NewGPUSampler(cx, dev, sampOpts)
		if err != nil {
			cleanup()
			return nil, err
		}
		samp = ownedSamp
	}

	var initial [UnlitUniformSize]byte
	data := AppendUnlitUniforms(initial[:0], mat)
	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " uniforms",
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     data,
		Retained: opts.Retained,
	})
	if err != nil {
		cleanup()
		return nil, err
	}

	group, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + " bindgroup",
		Layout: layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: buf, Size: UnlitUniformSize},
			{Binding: 1, Texture: tex.Texture()},
			{Binding: 2, Sampler: samp.Sampler()},
		},
	})
	if err != nil {
		_ = buf.Close()
		cleanup()
		return nil, err
	}

	m := &GPUUnlitMaterial{
		mat:       mat,
		boundTex:  mat.BaseColorTexture,
		boundSamp: mat.BaseColorSampler,
		ownedTex:  ownedTex,
		ownedSamp: ownedSamp,
		tex:       tex,
		samp:      samp,
		buf:       buf,
		group:     group,
	}
	return m, nil
}

func uploadUnlitWhiteTexture(cx *core.Scope, dev *gpu.Device, label string, retained bool) (*GPUTexture, error) {
	white := PixelBuffer{
		Width:  1,
		Height: 1,
		Stride: 4,
		Format: gpu.FormatRGBA8UnormSRGB,
		Pixels: []byte{255, 255, 255, 255},
	}
	return UploadTexturePixels(cx, dev, white, TextureUpload{
		Label:    label,
		SRGB:     true,
		Retained: retained,
	})
}

// UploadGLTFUnlitMaterial extracts a KHR_materials_unlit material from doc.Materials[matIndex], decodes and uploads its baseColorTexture as an sRGB [GPUTexture] and matching [GPUSampler] when present (or falls back to the 1x1 white sRGB texture), and creates a scope-owned [GPUUnlitMaterial].
//
// Any texture or sampler decoded by this helper is owned by the returned [GPUUnlitMaterial] and closed when [GPUUnlitMaterial.Close] is called.
func UploadGLTFUnlitMaterial(cx *core.Scope, dev *gpu.Device, layout *gpu.BindGroupLayout, doc *gltf.Document, matIndex int, opts UnlitMaterialOptions) (*GPUUnlitMaterial, error) {
	baseMat, err := UnlitMaterialFromGLTF(doc, matIndex, nil, nil)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if layout == nil || !layout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid unlit material bind group layout"}
	}

	gm := doc.Materials[matIndex]
	label := opts.Label
	if label == "" {
		if gm.Name != "" {
			label = "gltf.unlit." + gm.Name
		} else {
			label = fmt.Sprintf("gltf.unlit.%d", matIndex)
		}
		opts.Label = label
	}

	var (
		tex     *GPUTexture
		samp    *GPUSampler
		ownTex  bool
		ownSamp bool
	)
	if gm.BaseColorTexture != nil {
		texIdx := gm.BaseColorTexture.TextureIndex
		if texIdx < 0 || texIdx >= len(doc.Textures) {
			return nil, fmt.Errorf("%w: glTF texture index %d out of range [0, %d)", ErrInvalidUnlitMaterial, texIdx, len(doc.Textures))
		}
		gt := doc.Textures[texIdx]
		if gt.Source >= 0 {
			img, err := doc.DecodeImage(gt.Source)
			if err != nil {
				return nil, fmt.Errorf("%w: decode baseColorTexture image %d: %v", ErrInvalidUnlitMaterial, gt.Source, err)
			}
			tex, err = UploadTexture(cx, dev, img, TextureUpload{
				Label:    label + " baseColor",
				SRGB:     true,
				Mipmaps:  opts.Mipmaps,
				Retained: opts.Retained,
			})
			if err != nil {
				return nil, err
			}
			ownTex = true
		}
		if gt.Sampler >= 0 && gt.Sampler < len(doc.Samplers) {
			sampOpts := UnlitSamplerFromGLTF(doc.Samplers[gt.Sampler])
			if sampOpts.Label == "" {
				sampOpts.Label = label + " sampler"
			}
			sampOpts.Retained = opts.Retained
			samp, err = NewGPUSampler(cx, dev, sampOpts)
			if err != nil {
				if tex != nil {
					_ = tex.Close()
				}
				return nil, err
			}
			ownSamp = true
		}
	}

	baseMat.BaseColorTexture = tex
	baseMat.BaseColorSampler = samp
	return newGPUUnlitMaterialWithOwnership(cx, dev, layout, baseMat, opts, ownTex, ownSamp)
}

// Update writes updated uniform parameters (BaseColorFactor, AlphaMode, AlphaCutoff, DoubleSided, UseVertexColor) to the GPU uniform buffer with zero allocations.
//
// Texture and sampler bindings are immutable per [GPUUnlitMaterial]: pass either the same handles bound at creation or nil (to keep the existing bindings). Passing a different texture or sampler handle returns an error wrapping [ErrInvalidUnlitMaterial].
func (m *GPUUnlitMaterial) Update(dev *gpu.Device, mat UnlitMaterial) error {
	if !m.Valid() {
		return fmt.Errorf("%w: unlit material is not valid", ErrInvalidUnlitMaterial)
	}
	if err := mat.validateUniforms(); err != nil {
		return err
	}
	if mat.BaseColorTexture != nil && mat.BaseColorTexture != m.boundTex && mat.BaseColorTexture != m.tex {
		return fmt.Errorf("%w: cannot swap BaseColorTexture in Update; create a new GPUUnlitMaterial", ErrInvalidUnlitMaterial)
	}
	if mat.BaseColorSampler != nil && mat.BaseColorSampler != m.boundSamp && mat.BaseColorSampler != m.samp {
		return fmt.Errorf("%w: cannot swap BaseColorSampler in Update; create a new GPUUnlitMaterial", ErrInvalidUnlitMaterial)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	data := AppendUnlitUniforms(m.scratch[:0], mat)
	dev.WriteBuffer(m.buf, 0, data)
	m.mat.BaseColorFactor = mat.BaseColorFactor
	m.mat.AlphaMode = mat.AlphaMode
	m.mat.AlphaCutoff = mat.AlphaCutoff
	m.mat.DoubleSided = mat.DoubleSided
	m.mat.UseVertexColor = mat.UseVertexColor
	return nil
}

// Material returns a copy of the current [UnlitMaterial] settings stored in m, or the zero value for a nil receiver.
func (m *GPUUnlitMaterial) Material() UnlitMaterial {
	if m == nil {
		return UnlitMaterial{}
	}
	return m.mat
}

// UniformBuffer returns the underlying material uniform buffer, or nil for a nil receiver.
func (m *GPUUnlitMaterial) UniformBuffer() *gpu.Buffer {
	if m == nil {
		return nil
	}
	return m.buf
}

// BindGroup returns the underlying bind group, or nil for a nil receiver.
func (m *GPUUnlitMaterial) BindGroup() *gpu.BindGroup {
	if m == nil {
		return nil
	}
	return m.group
}

// ByteSize returns the GPU memory held by m's uniform buffer and any material-owned fallback or decoded texture. It is zero for a nil receiver.
func (m *GPUUnlitMaterial) ByteSize() uint64 {
	if m == nil {
		return 0
	}
	var n uint64
	if m.buf != nil {
		n += m.buf.Size()
	}
	if m.ownedTex != nil {
		n += m.ownedTex.ByteSize()
	}
	return n
}

// Valid reports whether the material's uniform buffer, bind group, and bound texture and sampler are open and belong to a live device.
func (m *GPUUnlitMaterial) Valid() bool {
	if m == nil || !m.buf.Valid() || !m.group.Valid() {
		return false
	}
	return m.tex.Valid() && m.samp.Valid()
}

// Bind binds m's bind group at [UnlitMaterialGroup] (group 1) on pass. It records nothing when m is not [GPUUnlitMaterial.Valid].
func (m *GPUUnlitMaterial) Bind(pass *gpu.RenderPass) {
	m.BindAt(pass, UnlitMaterialGroup)
}

// BindAt binds m's bind group at group on pass. It records nothing when m is not [GPUUnlitMaterial.Valid].
func (m *GPUUnlitMaterial) BindAt(pass *gpu.RenderPass, group uint32) {
	if !m.Valid() {
		return
	}
	pass.SetBindGroup(group, m.group)
}

// Close releases the material's bind group, uniform buffer, and any material-owned fallback or glTF-decoded texture and sampler (leaving caller-supplied textures and samplers untouched). Safe to call on a nil receiver or more than once.
func (m *GPUUnlitMaterial) Close() error {
	if m == nil {
		return nil
	}
	var err error
	if m.group != nil {
		err = errors.Join(err, m.group.Close())
	}
	if m.buf != nil {
		err = errors.Join(err, m.buf.Close())
	}
	if m.ownedTex != nil {
		err = errors.Join(err, m.ownedTex.Close())
	}
	if m.ownedSamp != nil {
		err = errors.Join(err, m.ownedSamp.Close())
	}
	return err
}

// UnlitWGSL is the built-in WGSL shader module for glTF KHR_materials_unlit surfaces with linear RGBA vertex colors (COLOR_0).
//
// Bind groups:
//   - @group(0) @binding(0): [CameraWGSL] camera uniform block ([CameraBuffer])
//   - @group(1) @binding(0..2): UnlitUniforms, base-color texture, and sampler ([UnlitMaterialBindingLayout])
//
// Vertex inputs match [ColoredVertexLayout] (@location(0) pos, @location(1) normal, @location(2) uv, @location(3) color).
// Entry points:
//   - "vs_main": transforms positions by camera.view_proj and forwards UV and linear vertex color
//   - "fs_main": evaluates baseColorFactor * baseColorTexture * vertexColor, applies alpha-mask discard and alpha mode, and encodes linear RGB to sRGB (for non-sRGB targets such as FormatRGBA8Unorm)
//   - "fs_linear": evaluates the same unlit color and outputs linear RGBA directly (for sRGB hardware swapchain targets where the GPU encodes linear -> sRGB on write)
const UnlitWGSL = CameraWGSL + `
struct UnlitUniforms {
	base_color_factor: vec4<f32>,
	params: vec4<f32>,
}

@group(1) @binding(0) var<uniform> unlit_mat: UnlitUniforms;
@group(1) @binding(1) var unlit_tex: texture_2d<f32>;
@group(1) @binding(2) var unlit_samp: sampler;

struct UnlitVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
	@location(3) color: vec4<f32>,
}

struct UnlitVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) uv: vec2<f32>,
	@location(1) color: vec4<f32>,
}

@vertex
fn vs_main(in: UnlitVertexIn) -> UnlitVertexOut {
	var out: UnlitVertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.uv = in.uv;
	out.color = in.color;
	return out;
}

fn unlit_linear_to_srgb_ch(c: f32) -> f32 {
	let v = clamp(c, 0.0, 1.0);
	if v <= 0.0031308 {
		return v * 12.92;
	}
	return 1.055 * pow(v, 1.0 / 2.4) - 0.055;
}

fn unlit_linear_to_srgb(rgb: vec3<f32>) -> vec3<f32> {
	return vec3<f32>(
		unlit_linear_to_srgb_ch(rgb.x),
		unlit_linear_to_srgb_ch(rgb.y),
		unlit_linear_to_srgb_ch(rgb.z),
	);
}

@fragment
fn fs_main(in: UnlitVertexOut) -> @location(0) vec4<f32> {
	let texel = textureSample(unlit_tex, unlit_samp, in.uv);
	var vcol = vec4<f32>(1.0, 1.0, 1.0, 1.0);
	if unlit_mat.params.z > 0.5 {
		vcol = in.color;
	}
	let base = unlit_mat.base_color_factor * texel * vcol;
	var out_alpha = 1.0;
	if unlit_mat.params.y > 0.5 && unlit_mat.params.y < 1.5 {
		if base.a < unlit_mat.params.x {
			discard;
		}
		out_alpha = 1.0;
	} else if unlit_mat.params.y >= 1.5 {
		out_alpha = clamp(base.a, 0.0, 1.0);
	}
	return vec4<f32>(unlit_linear_to_srgb(base.rgb), out_alpha);
}

@fragment
fn fs_linear(in: UnlitVertexOut) -> @location(0) vec4<f32> {
	let texel = textureSample(unlit_tex, unlit_samp, in.uv);
	var vcol = vec4<f32>(1.0, 1.0, 1.0, 1.0);
	if unlit_mat.params.z > 0.5 {
		vcol = in.color;
	}
	let base = unlit_mat.base_color_factor * texel * vcol;
	var out_alpha = 1.0;
	if unlit_mat.params.y > 0.5 && unlit_mat.params.y < 1.5 {
		if base.a < unlit_mat.params.x {
			discard;
		}
		out_alpha = 1.0;
	} else if unlit_mat.params.y >= 1.5 {
		out_alpha = clamp(base.a, 0.0, 1.0);
	}
	return vec4<f32>(base.rgb, out_alpha);
}
`

// UnlitPipelineOptions configures [NewUnlitPipeline].
type UnlitPipelineOptions struct {
	// Label names the pipeline in GPU capture tools.
	Label string
	// CameraLayout is the bind group layout for @group(0) (typically [CameraBuffer.Layout]). Required.
	CameraLayout *gpu.BindGroupLayout
	// MaterialLayout is the bind group layout for @group(1) (from [UnlitMaterialBindingLayout]). Required.
	MaterialLayout *gpu.BindGroupLayout
	// AlphaMode selects blend and depth-write state: UnlitAlphaOpaque and UnlitAlphaMask write depth without color blending; UnlitAlphaBlend enables [gpu.BlendAlpha] and disables depth writes.
	AlphaMode UnlitAlphaMode
	// DoubleSided uses [gpu.CullNone] when true; otherwise back faces are culled ([gpu.CullBack], counter-clockwise front faces).
	DoubleSided bool
	// TargetFormat is the color target format; [gpu.FormatUndefined] defaults to dev.SurfaceFormat(). Non-sRGB targets select fs_main (linear -> sRGB in shader); sRGB targets select fs_linear so the hardware encodes once.
	TargetFormat gpu.TextureFormat
	// DepthFormat is the depth attachment format, or [gpu.FormatUndefined] when rendering without depth testing.
	DepthFormat gpu.TextureFormat
	// DepthCompare overrides the depth comparison function when DepthFormat is set; zero defaults to [gpu.CompareLess].
	DepthCompare gpu.CompareFunc
	// SampleCount is the MSAA sample count (0 or 1 disables multisampling).
	SampleCount int
	// Retained opts the pipeline into automatic recreation after device loss.
	Retained bool
}

// NewUnlitPipeline compiles [UnlitWGSL] and creates a [gpu.Pipeline] configured for [ColoredVertexLayout], [CameraGroup], [UnlitMaterialGroup], and opts.
func NewUnlitPipeline(cx *core.Scope, dev *gpu.Device, opts UnlitPipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if !validUnlitAlphaMode(opts.AlphaMode) {
		return nil, fmt.Errorf("%w: invalid alpha mode %d", ErrInvalidUnlitMaterial, opts.AlphaMode)
	}
	if opts.CameraLayout == nil || !opts.CameraLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	if opts.MaterialLayout == nil || !opts.MaterialLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid unlit material bind group layout"}
	}
	mod, err := gpu.CompileWGSL(UnlitWGSL)
	if err != nil {
		return nil, err
	}

	targetFormat := opts.TargetFormat
	if targetFormat == gpu.FormatUndefined {
		targetFormat = dev.SurfaceFormat()
	}
	fragEntry := "fs_main"
	if targetFormat.IsSRGB() {
		fragEntry = "fs_linear"
	}

	target := gpu.ColorTargetState{Format: targetFormat}
	depthWrite := true
	if opts.AlphaMode == UnlitAlphaBlend {
		target.Blend = gpu.BlendAlpha()
		depthWrite = false
	}

	cull := gpu.CullBack
	if opts.DoubleSided {
		cull = gpu.CullNone
	}

	label := opts.Label
	if label == "" {
		label = "render3d.unlit." + opts.AlphaMode.String()
	}

	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: fragEntry},
		Layouts:       []*gpu.BindGroupLayout{opts.CameraLayout, opts.MaterialLayout},
		VertexBuffers: []gpu.VertexBufferLayout{ColoredVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  cull,
		},
		Targets:     []gpu.ColorTargetState{target},
		SampleCount: opts.SampleCount,
		Retained:    opts.Retained,
	}
	if opts.DepthFormat != gpu.FormatUndefined {
		cmp := opts.DepthCompare
		if cmp == 0 {
			cmp = gpu.CompareLess
		}
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:       opts.DepthFormat,
			DepthWrite:   depthWrite,
			DepthCompare: cmp,
		}
	}
	return dev.CreatePipeline(cx, desc)
}

// UnlitSample is the pure-Go CPU reference result produced by [EvaluateUnlit], matching [UnlitWGSL].
type UnlitSample struct {
	// Linear holds the evaluated linear RGBA color (with alpha 1.0 for UnlitAlphaOpaque and surviving UnlitAlphaMask fragments).
	Linear [4]float32
	// SRGB holds the IEC 61966-2-1 sRGB-encoded RGB and linear alpha in [0, 1], matching [UnlitWGSL]'s fs_main output.
	SRGB [4]float32
	// Discarded is true when AlphaMode is UnlitAlphaMask and the combined alpha is strictly less than AlphaCutoff.
	Discarded bool
}

// RGBA8 quantizes s.SRGB into an 8-bit [color.RGBA] matching an RGBA8Unorm framebuffer pixel (or zero when s.Discarded is true).
func (s UnlitSample) RGBA8() color.RGBA {
	if s.Discarded {
		return color.RGBA{}
	}
	return color.RGBA{
		R: unlitUnitToByte(s.SRGB[0]),
		G: unlitUnitToByte(s.SRGB[1]),
		B: unlitUnitToByte(s.SRGB[2]),
		A: unlitUnitToByte(s.SRGB[3]),
	}
}

// BlendOver composites s over an sRGB RGBA8 destination pixel using straight-alpha blending ([gpu.BlendAlpha]), matching [UnlitAlphaBlend] on a FormatRGBA8Unorm target. When s.Discarded is true, dst is returned unchanged.
func (s UnlitSample) BlendOver(dst color.RGBA) color.RGBA {
	if s.Discarded {
		return dst
	}
	a := unlitClamp01(s.SRGB[3])
	inv := 1 - a
	dr := float32(dst.R) / 255
	dg := float32(dst.G) / 255
	db := float32(dst.B) / 255
	da := float32(dst.A) / 255
	return color.RGBA{
		R: unlitUnitToByte(s.SRGB[0]*a + dr*inv),
		G: unlitUnitToByte(s.SRGB[1]*a + dg*inv),
		B: unlitUnitToByte(s.SRGB[2]*a + db*inv),
		A: unlitUnitToByte(a + da*inv),
	}
}

// EvaluateUnlit evaluates the exact glTF KHR_materials_unlit shading equation on the CPU:
//
//	base = mat.BaseColorFactor * texLinear * (mat.UseVertexColor ? vertexColor : [1,1,1,1])
//
// applying AlphaMode (forcing alpha to 1.0 for opaque, discarding when alpha < AlphaCutoff for mask, or clamping alpha to [0, 1] for blend) and encoding linear RGB to sRGB via the IEC 61966-2-1 transfer curve.
//
// Pass texLinear = [4]float32{1, 1, 1, 1} when no texture is bound (or decode an sRGB texel to linear RGB + linear alpha first), and vertexColor = [4]float32{1, 1, 1, 1} when COLOR_0 is omitted.
func EvaluateUnlit(mat UnlitMaterial, texLinear, vertexColor [4]float32) UnlitSample {
	vcol := [4]float32{1, 1, 1, 1}
	if mat.UseVertexColor {
		vcol = vertexColor
	}
	r := mat.BaseColorFactor[0] * texLinear[0] * vcol[0]
	g := mat.BaseColorFactor[1] * texLinear[1] * vcol[1]
	b := mat.BaseColorFactor[2] * texLinear[2] * vcol[2]
	a := mat.BaseColorFactor[3] * texLinear[3] * vcol[3]

	outAlpha := float32(1)
	switch mat.AlphaMode {
	case UnlitAlphaMask:
		if a < mat.AlphaCutoff {
			return UnlitSample{Discarded: true}
		}
		outAlpha = 1
	case UnlitAlphaBlend:
		outAlpha = unlitClamp01(a)
	default:
		outAlpha = 1
	}

	lin := [4]float32{r, g, b, outAlpha}
	srgb := [4]float32{
		unlitLinearToSRGB(r),
		unlitLinearToSRGB(g),
		unlitLinearToSRGB(b),
		outAlpha,
	}
	return UnlitSample{
		Linear: lin,
		SRGB:   srgb,
	}
}

func unlitClamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func unlitLinearToSRGB(c float32) float32 {
	v := unlitClamp01(c)
	if v <= 0.0031308 {
		return v * 12.92
	}
	return float32(1.055*math.Pow(float64(v), 1.0/2.4) - 0.055)
}

func unlitUnitToByte(v float32) uint8 {
	return clampByte(int(math.Round(float64(unlitClamp01(v)) * 255.0)))
}
