package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Interleaved vertex layout written by [Mesh.AppendVertexData] and described by [MeshVertexLayout]. Every mesh uses the same layout, so one pipeline draws any [GPUMesh]; a missing normal or UV is written as zeros.
const (
	// MeshVertexStride is the byte size of one interleaved vertex: position (12 bytes), normal (12 bytes) and UV (8 bytes).
	MeshVertexStride = 32

	// MeshPositionOffset is the byte offset of the float32x3 position inside a vertex.
	MeshPositionOffset = 0
	// MeshNormalOffset is the byte offset of the float32x3 normal inside a vertex.
	MeshNormalOffset = 12
	// MeshUVOffset is the byte offset of the float32x2 texture coordinate inside a vertex.
	MeshUVOffset = 24

	// MeshPositionLocation is the shader @location of the position attribute.
	MeshPositionLocation = 0
	// MeshNormalLocation is the shader @location of the normal attribute.
	MeshNormalLocation = 1
	// MeshUVLocation is the shader @location of the texture coordinate attribute.
	MeshUVLocation = 2
)

// maxUint16Vertices is the largest vertex count whose indices are written as uint16. Index 0xFFFF is left unused because WebGL 2 always treats it as a primitive restart.
const maxUint16Vertices = math.MaxUint16

// ErrInvalidMesh is wrapped by every error [Mesh.Validate] and [UploadMesh] return for malformed geometry.
var ErrInvalidMesh = errors.New("render3d: invalid mesh")

// Mesh is indexed or non-indexed triangle-list geometry kept on the CPU, ready to upload to the GPU view's buffers with [UploadMesh].
//
// Positions is required. Normals and UVs are optional: each is either empty or holds one entry per position. Indices is optional: when empty, every three consecutive positions form a triangle; otherwise every three indices do, and each index must be less than len(Positions). Triangles are counter-clockwise when front-facing, matching glTF and [math3d]. A glTF triangle primitive maps directly: Positions, Normals, TexCoords0 and Indices of [github.com/nsgo-dev/game/pkg/gltf.Primitive] have these types.
type Mesh struct {
	Positions []math3d.Vec3
	Normals   []math3d.Vec3
	UVs       []math3d.Vec2
	Indices   []uint32
}

// Validate reports whether m can be uploaded: at least one triangle, normals and UVs that are empty or match the position count, finite attribute values, an index count (or, without indices, a vertex count) that is a multiple of three, and every index in range. Every error wraps [ErrInvalidMesh].
func (m Mesh) Validate() error {
	n := len(m.Positions)
	if n == 0 {
		return fmt.Errorf("%w: no positions", ErrInvalidMesh)
	}
	if uint64(n) > math.MaxUint32 {
		return fmt.Errorf("%w: %d positions exceed the uint32 index range", ErrInvalidMesh, n)
	}
	if len(m.Normals) != 0 && len(m.Normals) != n {
		return fmt.Errorf("%w: %d normals for %d positions", ErrInvalidMesh, len(m.Normals), n)
	}
	if len(m.UVs) != 0 && len(m.UVs) != n {
		return fmt.Errorf("%w: %d UVs for %d positions", ErrInvalidMesh, len(m.UVs), n)
	}
	for i, p := range m.Positions {
		if !finite3(p) {
			return fmt.Errorf("%w: position %d is not finite", ErrInvalidMesh, i)
		}
	}
	for i, v := range m.Normals {
		if !finite3(v) {
			return fmt.Errorf("%w: normal %d is not finite", ErrInvalidMesh, i)
		}
	}
	for i, uv := range m.UVs {
		if !finite(uv.X) || !finite(uv.Y) {
			return fmt.Errorf("%w: UV %d is not finite", ErrInvalidMesh, i)
		}
	}
	if len(m.Indices) == 0 {
		if n%3 != 0 {
			return fmt.Errorf("%w: %d non-indexed vertices are not whole triangles", ErrInvalidMesh, n)
		}
		return nil
	}
	if len(m.Indices)%3 != 0 {
		return fmt.Errorf("%w: %d indices are not whole triangles", ErrInvalidMesh, len(m.Indices))
	}
	if uint64(len(m.Indices)) > math.MaxUint32 {
		return fmt.Errorf("%w: %d indices exceed the uint32 draw range", ErrInvalidMesh, len(m.Indices))
	}
	for i, idx := range m.Indices {
		if uint64(idx) >= uint64(n) {
			return fmt.Errorf("%w: index %d is %d, out of range for %d positions", ErrInvalidMesh, i, idx, n)
		}
	}
	return nil
}

// VertexCount returns the number of vertices, len(m.Positions).
func (m Mesh) VertexCount() int { return len(m.Positions) }

// Indexed reports whether m has an index list.
func (m Mesh) Indexed() bool { return len(m.Indices) != 0 }

// IndexFormat returns the index width [Mesh.AppendIndexData] writes: [gpu.IndexUint16] when every vertex fits below 0xFFFF (kept free because WebGL 2 always restarts primitives on it), otherwise [gpu.IndexUint32].
func (m Mesh) IndexFormat() gpu.IndexFormat {
	if len(m.Positions) <= maxUint16Vertices {
		return gpu.IndexUint16
	}
	return gpu.IndexUint32
}

// Bounds returns the axis-aligned box around m's positions, or [math3d.AABBEmpty] when m has none.
func (m Mesh) Bounds() math3d.AABB {
	b := math3d.AABBEmpty()
	for _, p := range m.Positions {
		b = b.ExpandPoint(p)
	}
	return b
}

// VertexDataSize returns the byte length [Mesh.AppendVertexData] appends.
func (m Mesh) VertexDataSize() int { return len(m.Positions) * MeshVertexStride }

// IndexDataSize returns the byte length [Mesh.AppendIndexData] appends: the indices at [Mesh.IndexFormat] width, padded with zeros to a multiple of four bytes as WebGPU requires for buffer sizes. It is zero for a non-indexed mesh.
func (m Mesh) IndexDataSize() int {
	n := len(m.Indices)
	if m.IndexFormat() == gpu.IndexUint32 {
		return n * 4
	}
	return (n*2 + 3) &^ 3
}

// AppendVertexData appends m's vertices to dst in the little-endian interleaved layout of [MeshVertexLayout] and returns the extended slice. Missing normals and UVs are written as zeros. It allocates only when dst lacks capacity for [Mesh.VertexDataSize] more bytes. It does not validate m: call [Mesh.Validate] first, because for a mesh that fails it the bytes written are unspecified (normals or UVs whose count does not match the positions, for example, are written as zeros).
func (m Mesh) AppendVertexData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, m.VertexDataSize())
	out := dst[start:]
	hasNormals := len(m.Normals) == len(m.Positions)
	hasUVs := len(m.UVs) == len(m.Positions)
	for i, p := range m.Positions {
		v := out[i*MeshVertexStride : (i+1)*MeshVertexStride]
		putVec3(v[MeshPositionOffset:], p)
		if hasNormals {
			putVec3(v[MeshNormalOffset:], m.Normals[i])
		} else {
			clear(v[MeshNormalOffset:MeshUVOffset])
		}
		if hasUVs {
			binary.LittleEndian.PutUint32(v[MeshUVOffset:], math.Float32bits(m.UVs[i].X))
			binary.LittleEndian.PutUint32(v[MeshUVOffset+4:], math.Float32bits(m.UVs[i].Y))
		} else {
			clear(v[MeshUVOffset:MeshVertexStride])
		}
	}
	return dst
}

// AppendIndexData appends m's indices to dst at [Mesh.IndexFormat] width, little-endian and zero-padded to [Mesh.IndexDataSize], and returns the extended slice. It appends nothing for a non-indexed mesh and allocates only when dst lacks capacity. It does not validate m: call [Mesh.Validate] first, because for a mesh that fails it the bytes written are unspecified (an out-of-range index is truncated to the index width, for example).
func (m Mesh) AppendIndexData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, m.IndexDataSize())
	out := dst[start:]
	if m.IndexFormat() == gpu.IndexUint32 {
		for i, idx := range m.Indices {
			binary.LittleEndian.PutUint32(out[i*4:], idx)
		}
		return dst
	}
	for i, idx := range m.Indices {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(idx))
	}
	clear(out[len(m.Indices)*2:])
	return dst
}

// MeshVertexLayout returns the vertex buffer layout of every [GPUMesh] for [gpu.PipelineDesc].VertexBuffers: one per-vertex buffer of [MeshVertexStride] bytes with the position at [MeshPositionLocation], the normal at [MeshNormalLocation] and the UV at [MeshUVLocation]. Each call returns a fresh Attributes slice.
func MeshVertexLayout() gpu.VertexBufferLayout {
	return gpu.VertexBufferLayout{
		Stride:   MeshVertexStride,
		StepMode: gpu.StepVertex,
		Attributes: []gpu.VertexAttribute{
			{Format: gpu.VertexFloat32x3, Offset: MeshPositionOffset, Location: MeshPositionLocation},
			{Format: gpu.VertexFloat32x3, Offset: MeshNormalOffset, Location: MeshNormalLocation},
			{Format: gpu.VertexFloat32x2, Offset: MeshUVOffset, Location: MeshUVLocation},
		},
	}
}

// MeshUpload configures [UploadMesh].
type MeshUpload struct {
	// Label names the buffers in GPU capture tools; the vertex buffer gets the suffix " vertices" and the index buffer " indices".
	Label string
	// Retained keeps a CPU copy of the buffers so NSGo recreates them after a device loss (see [gpu.BufferDesc].Retained). Leave it false when the game re-uploads its meshes from OnReady instead.
	Retained bool
}

// GPUMesh is a [Mesh] uploaded to a GPU view's vertex and, when indexed, index buffers. Its buffers belong to the scope passed to [UploadMesh] and are released when that scope is disposed or when [GPUMesh.Close] is called. Use it from the engine goroutine, like the [gpu.Device] it came from.
type GPUMesh struct {
	vertices    *gpu.Buffer
	indices     *gpu.Buffer
	indexFormat gpu.IndexFormat
	vertexCount uint32
	indexCount  uint32
	bounds      math3d.AABB
}

// UploadMesh validates m and creates its vertex buffer and, when m is indexed, its index buffer on dev, scoped to cx. A malformed mesh returns an error wrapping [ErrInvalidMesh] before anything is created; a device error (such as [gpu.ErrDeviceLost] or a buffer size over [gpu.Limits].MaxBufferSize) is returned as the [gpu.Error] NSGo reports, with no buffer left behind.
func UploadMesh(cx *core.Scope, dev *gpu.Device, m Mesh, opts MeshUpload) (*GPUMesh, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return uploadMesh(m, opts, func(desc gpu.BufferDesc) (*gpu.Buffer, error) {
		return dev.CreateBuffer(cx, desc)
	})
}

// uploadMesh creates a validated mesh's buffers through create, closing the vertex buffer again when the index buffer cannot be created.
func uploadMesh(m Mesh, opts MeshUpload, create func(gpu.BufferDesc) (*gpu.Buffer, error)) (*GPUMesh, error) {
	g := &GPUMesh{
		vertexCount: uint32(len(m.Positions)),
		indexCount:  uint32(len(m.Indices)),
		indexFormat: m.IndexFormat(),
		bounds:      m.Bounds(),
	}
	var err error
	g.vertices, err = create(gpu.BufferDesc{
		Label:    bufferLabel(opts.Label, "vertices"),
		Usage:    gpu.UsageVertex | gpu.UsageCopyDst,
		Data:     m.AppendVertexData(nil),
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	if m.Indexed() {
		g.indices, err = create(gpu.BufferDesc{
			Label:    bufferLabel(opts.Label, "indices"),
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

// VertexBuffer returns the interleaved vertex buffer, laid out as [MeshVertexLayout] describes.
func (g *GPUMesh) VertexBuffer() *gpu.Buffer { return g.vertices }

// IndexBuffer returns the index buffer, or nil for a non-indexed mesh.
func (g *GPUMesh) IndexBuffer() *gpu.Buffer { return g.indices }

// IndexFormat returns the width of the index buffer's entries.
func (g *GPUMesh) IndexFormat() gpu.IndexFormat { return g.indexFormat }

// Indexed reports whether the mesh draws through an index buffer.
func (g *GPUMesh) Indexed() bool { return g.indices != nil }

// VertexCount returns the number of vertices in the vertex buffer.
func (g *GPUMesh) VertexCount() uint32 { return g.vertexCount }

// IndexCount returns the number of indices, or zero for a non-indexed mesh.
func (g *GPUMesh) IndexCount() uint32 { return g.indexCount }

// Triangles returns the number of triangles one draw of the mesh rasterizes.
func (g *GPUMesh) Triangles() uint32 {
	if g.indices != nil {
		return g.indexCount / 3
	}
	return g.vertexCount / 3
}

// Bounds returns the object-space box around the mesh's positions, as [Mesh.Bounds] computed it at upload.
func (g *GPUMesh) Bounds() math3d.AABB { return g.bounds }

// ByteSize returns the GPU memory the mesh's buffers hold, for [RenderStats] and [Budget] accounting. It is zero for a nil mesh.
func (g *GPUMesh) ByteSize() uint64 {
	if g == nil {
		return 0
	}
	n := g.vertices.Size()
	if g.indices != nil {
		n += g.indices.Size()
	}
	return n
}

// Valid reports whether the mesh's buffers are open and belong to a live device, so it can be drawn.
func (g *GPUMesh) Valid() bool {
	if g == nil || !g.vertices.Valid() {
		return false
	}
	return g.indices == nil || g.indices.Valid()
}

// Bind binds the vertex buffer to slot 0 and, for an indexed mesh, the index buffer to pass, so a caller can issue its own draw (for example an instanced one) after setting a pipeline built with [MeshVertexLayout]. It binds nothing when the mesh is not [GPUMesh.Valid] (nil, closed, its scope disposed, or not retained across a device loss), so check Valid before issuing a draw of your own.
func (g *GPUMesh) Bind(pass *gpu.RenderPass) {
	if !g.Valid() {
		return
	}
	pass.SetVertexBuffer(0, g.vertices, 0)
	if g.indices != nil {
		pass.SetIndexBuffer(g.indices, g.indexFormat, 0)
	}
}

// Draw binds the mesh to pass and draws all of its triangles once. The pass must already have a pipeline whose vertex buffer 0 is [MeshVertexLayout] and whose bind groups are set. When the mesh is not [GPUMesh.Valid] it records nothing, so it never draws with buffers an earlier mesh left bound: NSGo skips binding an invalid buffer but still records the draw (nsgo-dev/nsgo#2253).
func (g *GPUMesh) Draw(pass *gpu.RenderPass) {
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

// Close releases the mesh's buffers now instead of when their scope is disposed. It is safe to call more than once.
func (g *GPUMesh) Close() error {
	if g == nil {
		return nil
	}
	err := g.vertices.Close()
	if g.indices != nil {
		err = errors.Join(err, g.indices.Close())
	}
	return err
}

func bufferLabel(label, kind string) string {
	if label == "" {
		return "mesh " + kind
	}
	return label + " " + kind
}

func grow(dst []byte, n int) []byte {
	if cap(dst)-len(dst) < n {
		next := make([]byte, len(dst), len(dst)+n)
		copy(next, dst)
		dst = next
	}
	return dst[:len(dst)+n]
}

func putVec3(b []byte, v math3d.Vec3) {
	binary.LittleEndian.PutUint32(b[0:], math.Float32bits(v.X))
	binary.LittleEndian.PutUint32(b[4:], math.Float32bits(v.Y))
	binary.LittleEndian.PutUint32(b[8:], math.Float32bits(v.Z))
}

func finite(f float32) bool { return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0) }

func finite3(v math3d.Vec3) bool { return finite(v.X) && finite(v.Y) && finite(v.Z) }
