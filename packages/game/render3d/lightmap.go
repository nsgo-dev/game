package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Interleaved vertex layout written by [LightmapMesh.AppendVertexData] and
// described by [LightmapVertexLayout]. Every lightmapped mesh uses this 40-byte
// layout so one pipeline draws any [GPULightmapMesh] with distinct UV0 (albedo)
// and UV1 (baked lightmap) channels.
const (
	// LightmapVertexStride is the byte size of one interleaved lightmapped
	// vertex: position (12 bytes), normal (12 bytes), UV0 (8 bytes), UV1 (8 bytes).
	LightmapVertexStride = 40

	// LightmapPositionOffset is the byte offset of the float32x3 position inside a vertex.
	LightmapPositionOffset = 0
	// LightmapNormalOffset is the byte offset of the float32x3 normal inside a vertex.
	LightmapNormalOffset = 12
	// LightmapUV0Offset is the byte offset of the float32x2 primary (albedo) UV inside a vertex.
	LightmapUV0Offset = 24
	// LightmapUV1Offset is the byte offset of the float32x2 secondary (lightmap) UV inside a vertex.
	LightmapUV1Offset = 32

	// LightmapPositionLocation is the shader @location of the position attribute.
	LightmapPositionLocation = 0
	// LightmapNormalLocation is the shader @location of the normal attribute.
	LightmapNormalLocation = 1
	// LightmapUV0Location is the shader @location of the primary (albedo) UV attribute.
	LightmapUV0Location = 2
	// LightmapUV1Location is the shader @location of the secondary (lightmap) UV attribute.
	LightmapUV1Location = 3
)

// Bind group slots and uniform layout constants for [LightmapRenderer] and [LightmapWGSL].
const (
	// LightmapDrawGroup is the bind group slot carrying the per-draw dynamic uniform buffer.
	LightmapDrawGroup = 1
	// LightmapTextureGroup is the bind group slot carrying the base color and lightmap textures and samplers.
	LightmapTextureGroup = 2

	// LightmapDrawUniformSize is the byte size of the WGSL LightmapDraw uniform struct (160 bytes).
	LightmapDrawUniformSize = 64 + 6*16
	// LightmapDrawUniformStride is the byte stride between per-draw uniform slots
	// (256 bytes, matching WebGPU's minimum dynamic uniform buffer offset alignment).
	LightmapDrawUniformStride = 256

	// DefaultRGBMMaxRange is the default HDR multiplier for [LightmapRGBM] lightmaps.
	DefaultRGBMMaxRange float32 = 8.0
)

// ErrInvalidLightmap is wrapped by errors returned when lightmap encoding,
// material parameters, UV transforms, or renderer descriptors are invalid.
var ErrInvalidLightmap = errors.New("render3d: invalid lightmap")

// LightmapEncoding identifies how a baked lightmap texture encodes linear
// irradiance values.
type LightmapEncoding uint8

const (
	// LightmapLinear indicates the lightmap texture stores linear irradiance
	// directly (for example in FormatRGBA16Float HDR or linear FormatRGBA8Unorm).
	LightmapLinear LightmapEncoding = iota
	// LightmapSRGB indicates the lightmap texture stores sRGB-encoded LDR
	// irradiance. When uploaded as FormatRGBA8Unorm, the shader decodes sRGB to
	// linear; when uploaded as FormatRGBA8UnormSRGB, hardware sampling already
	// decodes to linear and the shader avoids double-decoding.
	LightmapSRGB
	// LightmapRGBM indicates an 8-bit linear RGBA8Unorm texture packed with a
	// shared multiplier in alpha: linearRGB = rgb * (a * RGBMMaxRange).
	// This allows HDR baked lightmaps in standard 8-bit PNGs across mobile and
	// WebGL 2 backends without float textures.
	LightmapRGBM
	// LightmapRGBD indicates an 8-bit linear RGBA8Unorm texture packed with a
	// shared divisor in alpha: linearRGB = rgb / max(a, 1/255), preserving full
	// 8-bit precision for LDR values <= 1 while representing HDR highlights.
	LightmapRGBD
)

// String returns a human-readable name for e.
func (e LightmapEncoding) String() string {
	switch e {
	case LightmapLinear:
		return "linear"
	case LightmapSRGB:
		return "srgb"
	case LightmapRGBM:
		return "rgbm"
	case LightmapRGBD:
		return "rgbd"
	default:
		return fmt.Sprintf("LightmapEncoding(%d)", e)
	}
}

// Valid reports whether e is a recognized [LightmapEncoding].
func (e LightmapEncoding) Valid() bool {
	return e <= LightmapRGBD
}

// ParseLightmapEncoding converts a case-insensitive encoding string ("",
// "linear", "srgb", "rgbm", "rgbd") into a [LightmapEncoding]. An empty string
// resolves to [LightmapLinear].
func ParseLightmapEncoding(s string) (LightmapEncoding, error) {
	switch s {
	case "", "linear", "LINEAR":
		return LightmapLinear, nil
	case "srgb", "SRGB", "sRGB":
		return LightmapSRGB, nil
	case "rgbm", "RGBM":
		return LightmapRGBM, nil
	case "rgbd", "RGBD":
		return LightmapRGBD, nil
	default:
		return LightmapLinear, fmt.Errorf("%w: unknown encoding %q", ErrInvalidLightmap, s)
	}
}

// UVTransform applies an affine scale and offset (uv * Scale + Offset) to
// lightmap coordinates so multiple meshes can share regions of a single baked
// lightmap atlas.
type UVTransform struct {
	Scale  [2]float32
	Offset [2]float32
}

// IdentityUVTransform returns the identity UV transform (Scale {1, 1}, Offset {0, 0}).
func IdentityUVTransform() UVTransform {
	return UVTransform{Scale: [2]float32{1, 1}}
}

// Normalized returns t with a zero Scale ({0, 0}) replaced by {1, 1}.
func (t UVTransform) Normalized() UVTransform {
	if t.Scale == [2]float32{0, 0} {
		t.Scale = [2]float32{1, 1}
	}
	return t
}

// Apply transforms uv by t.Normalized().
func (t UVTransform) Apply(uv math3d.Vec2) math3d.Vec2 {
	n := t.Normalized()
	return math3d.Vec2{
		X: uv.X*n.Scale[0] + n.Offset[0],
		Y: uv.Y*n.Scale[1] + n.Offset[1],
	}
}

// Validate checks that all scale and offset components are finite.
func (t UVTransform) Validate() error {
	if !finite(t.Scale[0]) || !finite(t.Scale[1]) {
		return fmt.Errorf("%w: UVTransform scale %v is not finite", ErrInvalidLightmap, t.Scale)
	}
	if !finite(t.Offset[0]) || !finite(t.Offset[1]) {
		return fmt.Errorf("%w: UVTransform offset %v is not finite", ErrInvalidLightmap, t.Offset)
	}
	return nil
}

// LightmapVertex is one interleaved vertex for lightmapped 3D geometry carrying
// both primary (albedo) UV0 and secondary (baked lightmap) UV1 coordinates.
type LightmapVertex struct {
	Position [3]float32
	Normal   [3]float32
	UV0      [2]float32
	UV1      [2]float32
}

// LightmapMesh is triangle-list geometry carrying two UV sets: UV0 for tiled
// material maps (matching glTF TEXCOORD_0) and UV1 for baked lightmaps
// (matching glTF TEXCOORD_1).
//
// Positions is required. Normals, UV0, and UV1 are optional: each is either
// empty or holds one entry per position. When UV1 is empty, [UploadLightmapMesh]
// and [LightmapMesh.AppendVertexData] fall back cleanly to UV0 (or zeros if UV0
// is also empty). A glTF primitive's Positions, Normals, TexCoords0, TexCoords1,
// and Indices fields map directly onto LightmapMesh.
type LightmapMesh struct {
	Positions []math3d.Vec3
	Normals   []math3d.Vec3
	UV0       []math3d.Vec2
	UV1       []math3d.Vec2
	Indices   []uint32
}

// LightmapMeshFromAttributes constructs a [LightmapMesh] from separate attribute
// slices (such as a glTF primitive's Positions, Normals, TexCoords0, TexCoords1,
// and Indices). When uv1 is empty, UV1 falls back cleanly to uv0 during upload.
func LightmapMeshFromAttributes(positions, normals []math3d.Vec3, uv0, uv1 []math3d.Vec2, indices []uint32) LightmapMesh {
	return LightmapMesh{
		Positions: positions,
		Normals:   normals,
		UV0:       uv0,
		UV1:       uv1,
		Indices:   indices,
	}
}

// LightmapMeshFromVertices converts interleaved [LightmapVertex] values and
// optional indices into a [LightmapMesh].
func LightmapMeshFromVertices(vertices []LightmapVertex, indices []uint32) LightmapMesh {
	n := len(vertices)
	m := LightmapMesh{
		Positions: make([]math3d.Vec3, n),
		Normals:   make([]math3d.Vec3, n),
		UV0:       make([]math3d.Vec2, n),
		UV1:       make([]math3d.Vec2, n),
		Indices:   indices,
	}
	for i, v := range vertices {
		m.Positions[i] = math3d.Vec3{X: v.Position[0], Y: v.Position[1], Z: v.Position[2]}
		m.Normals[i] = math3d.Vec3{X: v.Normal[0], Y: v.Normal[1], Z: v.Normal[2]}
		m.UV0[i] = math3d.Vec2{X: v.UV0[0], Y: v.UV0[1]}
		m.UV1[i] = math3d.Vec2{X: v.UV1[0], Y: v.UV1[1]}
	}
	return m
}

// Validate reports whether m can be uploaded: at least one whole triangle,
// optional attributes empty or matching len(Positions) with finite values, and
// every index in range. Every error wraps [ErrInvalidMesh].
func (m LightmapMesh) Validate() error {
	base := Mesh{
		Positions: m.Positions,
		Normals:   m.Normals,
		UVs:       m.UV0,
		Indices:   m.Indices,
	}
	if err := base.Validate(); err != nil {
		return err
	}
	n := len(m.Positions)
	if len(m.UV1) != 0 && len(m.UV1) != n {
		return fmt.Errorf("%w: %d UV1 coordinates for %d positions", ErrInvalidMesh, len(m.UV1), n)
	}
	for i, uv := range m.UV1 {
		if !finite(uv.X) || !finite(uv.Y) {
			return fmt.Errorf("%w: UV1 %d is not finite", ErrInvalidMesh, i)
		}
	}
	return nil
}

// HasUV1 reports whether m provides a dedicated secondary UV1 channel rather
// than falling back to UV0.
func (m LightmapMesh) HasUV1() bool {
	return len(m.Positions) > 0 && len(m.UV1) == len(m.Positions)
}

// VertexCount returns the number of vertices, len(m.Positions).
func (m LightmapMesh) VertexCount() int { return len(m.Positions) }

// Indexed reports whether m has an index list.
func (m LightmapMesh) Indexed() bool { return len(m.Indices) != 0 }

// IndexFormat returns [gpu.IndexUint16] while all vertex indices fit below
// 0xFFFF (WebGL 2 primitive restart), otherwise [gpu.IndexUint32].
func (m LightmapMesh) IndexFormat() gpu.IndexFormat {
	if len(m.Positions) <= maxUint16Vertices {
		return gpu.IndexUint16
	}
	return gpu.IndexUint32
}

// Bounds returns the object-space AABB enclosing m.Positions.
func (m LightmapMesh) Bounds() math3d.AABB {
	b := math3d.AABBEmpty()
	for _, p := range m.Positions {
		b = b.ExpandPoint(p)
	}
	return b
}

// VertexDataSize returns the byte length [LightmapMesh.AppendVertexData] appends.
func (m LightmapMesh) VertexDataSize() int { return len(m.Positions) * LightmapVertexStride }

// IndexDataSize returns the byte length [LightmapMesh.AppendIndexData] appends,
// padded to a multiple of four bytes as WebGPU requires.
func (m LightmapMesh) IndexDataSize() int {
	n := len(m.Indices)
	if m.IndexFormat() == gpu.IndexUint32 {
		return n * 4
	}
	return (n*2 + 3) &^ 3
}

// Vertices materializes m as a slice of interleaved [LightmapVertex] values,
// falling back from empty UV1 to UV0.
func (m LightmapMesh) Vertices() []LightmapVertex {
	n := len(m.Positions)
	out := make([]LightmapVertex, n)
	hasNormals := len(m.Normals) == n
	hasUV0 := len(m.UV0) == n
	hasUV1 := len(m.UV1) == n
	for i, p := range m.Positions {
		v := LightmapVertex{Position: [3]float32{p.X, p.Y, p.Z}}
		if hasNormals {
			norm := m.Normals[i]
			v.Normal = [3]float32{norm.X, norm.Y, norm.Z}
		}
		if hasUV0 {
			uv0 := m.UV0[i]
			v.UV0 = [2]float32{uv0.X, uv0.Y}
		}
		if hasUV1 {
			uv1 := m.UV1[i]
			v.UV1 = [2]float32{uv1.X, uv1.Y}
		} else if hasUV0 {
			uv0 := m.UV0[i]
			v.UV1 = [2]float32{uv0.X, uv0.Y}
		}
		out[i] = v
	}
	return out
}

// AppendVertexData appends m's vertices to dst in the little-endian 40-byte
// interleaved layout of [LightmapVertexLayout] and returns the extended slice.
// Missing normals are written as zeros; missing UV1 falls back cleanly to UV0
// (or zeros if UV0 is also absent). Call [LightmapMesh.Validate] first.
func (m LightmapMesh) AppendVertexData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, m.VertexDataSize())
	out := dst[start:]
	n := len(m.Positions)
	hasNormals := len(m.Normals) == n
	hasUV0 := len(m.UV0) == n
	hasUV1 := len(m.UV1) == n
	for i, p := range m.Positions {
		v := out[i*LightmapVertexStride : (i+1)*LightmapVertexStride]
		putVec3(v[LightmapPositionOffset:], p)
		if hasNormals {
			putVec3(v[LightmapNormalOffset:], m.Normals[i])
		} else {
			clear(v[LightmapNormalOffset:LightmapUV0Offset])
		}
		if hasUV0 {
			binary.LittleEndian.PutUint32(v[LightmapUV0Offset:], math.Float32bits(m.UV0[i].X))
			binary.LittleEndian.PutUint32(v[LightmapUV0Offset+4:], math.Float32bits(m.UV0[i].Y))
		} else {
			clear(v[LightmapUV0Offset:LightmapUV1Offset])
		}
		if hasUV1 {
			binary.LittleEndian.PutUint32(v[LightmapUV1Offset:], math.Float32bits(m.UV1[i].X))
			binary.LittleEndian.PutUint32(v[LightmapUV1Offset+4:], math.Float32bits(m.UV1[i].Y))
		} else if hasUV0 {
			binary.LittleEndian.PutUint32(v[LightmapUV1Offset:], math.Float32bits(m.UV0[i].X))
			binary.LittleEndian.PutUint32(v[LightmapUV1Offset+4:], math.Float32bits(m.UV0[i].Y))
		} else {
			clear(v[LightmapUV1Offset:LightmapVertexStride])
		}
	}
	return dst
}

// AppendIndexData appends m's indices to dst at [LightmapMesh.IndexFormat]
// width, little-endian and zero-padded to a 4-byte multiple.
func (m LightmapMesh) AppendIndexData(dst []byte) []byte {
	base := Mesh{Positions: m.Positions, Indices: m.Indices}
	return base.AppendIndexData(dst)
}

// LightmapVertexLayout returns the vertex buffer layout for [GPULightmapMesh]:
// one 40-byte per-vertex buffer with position at location 0, normal at 1,
// UV0 at 2, and UV1 at 3. Each call returns a fresh Attributes slice.
func LightmapVertexLayout() gpu.VertexBufferLayout {
	return gpu.VertexBufferLayout{
		Stride:   LightmapVertexStride,
		StepMode: gpu.StepVertex,
		Attributes: []gpu.VertexAttribute{
			{Format: gpu.VertexFloat32x3, Offset: LightmapPositionOffset, Location: LightmapPositionLocation},
			{Format: gpu.VertexFloat32x3, Offset: LightmapNormalOffset, Location: LightmapNormalLocation},
			{Format: gpu.VertexFloat32x2, Offset: LightmapUV0Offset, Location: LightmapUV0Location},
			{Format: gpu.VertexFloat32x2, Offset: LightmapUV1Offset, Location: LightmapUV1Location},
		},
	}
}

// GPULightmapMesh is a [LightmapMesh] uploaded to GPU vertex and optional index
// buffers using [LightmapVertexLayout].
type GPULightmapMesh struct {
	vertices        *gpu.Buffer
	indices         *gpu.Buffer
	indexFormat     gpu.IndexFormat
	vertexCount     uint32
	indexCount      uint32
	bounds          math3d.AABB
	usedUV0Fallback bool
}

// UploadLightmapMesh validates m and uploads its 40-byte interleaved vertex
// buffer (position, normal, UV0, UV1) and optional index buffer onto dev,
// scoped to cx. When m.UV1 is empty, UV0 is copied into the UV1 attribute slot
// and [GPULightmapMesh.UsedUV0Fallback] reports true.
func UploadLightmapMesh(cx *core.Scope, dev *gpu.Device, m LightmapMesh, opts MeshUpload) (*GPULightmapMesh, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	g := &GPULightmapMesh{
		vertexCount:     uint32(len(m.Positions)),
		indexCount:      uint32(len(m.Indices)),
		indexFormat:     m.IndexFormat(),
		bounds:          m.Bounds(),
		usedUV0Fallback: !m.HasUV1(),
	}
	var err error
	g.vertices, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    bufferLabel(opts.Label, "lightmap vertices"),
		Usage:    gpu.UsageVertex | gpu.UsageCopyDst,
		Data:     m.AppendVertexData(nil),
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	if m.Indexed() {
		g.indices, err = dev.CreateBuffer(cx, gpu.BufferDesc{
			Label:    bufferLabel(opts.Label, "lightmap indices"),
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

// UploadLightmapVertices uploads interleaved [LightmapVertex] data and optional
// triangle indices as a [GPULightmapMesh].
func UploadLightmapVertices(cx *core.Scope, dev *gpu.Device, vertices []LightmapVertex, indices []uint32, opts MeshUpload) (*GPULightmapMesh, error) {
	return UploadLightmapMesh(cx, dev, LightmapMeshFromVertices(vertices, indices), opts)
}

// VertexBuffer returns the interleaved 40-byte vertex buffer.
func (g *GPULightmapMesh) VertexBuffer() *gpu.Buffer {
	if g == nil {
		return nil
	}
	return g.vertices
}

// IndexBuffer returns the index buffer, or nil for a non-indexed mesh.
func (g *GPULightmapMesh) IndexBuffer() *gpu.Buffer {
	if g == nil {
		return nil
	}
	return g.indices
}

// IndexFormat returns the index element width.
func (g *GPULightmapMesh) IndexFormat() gpu.IndexFormat {
	if g == nil {
		return 0
	}
	return g.indexFormat
}

// Indexed reports whether g draws through an index buffer.
func (g *GPULightmapMesh) Indexed() bool { return g != nil && g.indices != nil }

// VertexCount returns the number of uploaded vertices.
func (g *GPULightmapMesh) VertexCount() uint32 {
	if g == nil {
		return 0
	}
	return g.vertexCount
}

// IndexCount returns the number of uploaded indices, or zero when non-indexed.
func (g *GPULightmapMesh) IndexCount() uint32 {
	if g == nil {
		return 0
	}
	return g.indexCount
}

// Triangles returns the triangle count rasterized by one draw of g.
func (g *GPULightmapMesh) Triangles() uint32 {
	if g == nil {
		return 0
	}
	if g.indices != nil {
		return g.indexCount / 3
	}
	return g.vertexCount / 3
}

// Bounds returns the object-space bounding box of g.
func (g *GPULightmapMesh) Bounds() math3d.AABB {
	if g == nil {
		return math3d.AABBEmpty()
	}
	return g.bounds
}

// UsedUV0Fallback reports whether g was uploaded without dedicated UV1
// coordinates and therefore reused UV0 in the lightmap UV slot.
func (g *GPULightmapMesh) UsedUV0Fallback() bool {
	return g != nil && g.usedUV0Fallback
}

// ByteSize returns the GPU buffer bytes held by g.
func (g *GPULightmapMesh) ByteSize() uint64 {
	if g == nil {
		return 0
	}
	n := g.vertices.Size()
	if g.indices != nil {
		n += g.indices.Size()
	}
	return n
}

// Valid reports whether g's buffers are open and belong to a live device.
func (g *GPULightmapMesh) Valid() bool {
	if g == nil || !g.vertices.Valid() {
		return false
	}
	return g.indices == nil || g.indices.Valid()
}

// Bind binds g's vertex buffer to slot 0 and optional index buffer to pass.
func (g *GPULightmapMesh) Bind(pass *gpu.RenderPass) {
	if !g.Valid() {
		return
	}
	pass.SetVertexBuffer(0, g.vertices, 0)
	if g.indices != nil {
		pass.SetIndexBuffer(g.indices, g.indexFormat, 0)
	}
}

// Draw binds g to pass and draws all of its triangles once.
func (g *GPULightmapMesh) Draw(pass *gpu.RenderPass) {
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

// Close releases g's GPU buffers. It is safe to call more than once.
func (g *GPULightmapMesh) Close() error {
	if g == nil {
		return nil
	}
	err := g.vertices.Close()
	if g.indices != nil {
		err = errors.Join(err, g.indices.Close())
	}
	return err
}

// EncodeRGBM packs a linear HDR RGB irradiance value into 8-bit RGBM bytes
// using maxRange (defaults to [DefaultRGBMMaxRange] when <= 0).
// Decoding is linearRGB = (rgb / 255) * ((a / 255) * maxRange).
func EncodeRGBM(rgb math3d.Vec3, maxRange float32) [4]uint8 {
	if maxRange <= 0 || !finite(maxRange) {
		maxRange = DefaultRGBMMaxRange
	}
	r := clampPositive(rgb.X) / maxRange
	g := clampPositive(rgb.Y) / maxRange
	b := clampPositive(rgb.Z) / maxRange
	m := max(r, max(g, b))
	if m <= 0 {
		return [4]uint8{0, 0, 0, 0}
	}
	if m > 1 {
		m = 1
	}
	mByte := int(math.Ceil(float64(m) * 255.0))
	if mByte < 1 {
		mByte = 1
	}
	if mByte > 255 {
		mByte = 255
	}
	mQuant := float32(mByte) / 255.0
	return [4]uint8{
		clampByte(int(math.Round(float64(clampUnit(r/mQuant)) * 255.0))),
		clampByte(int(math.Round(float64(clampUnit(g/mQuant)) * 255.0))),
		clampByte(int(math.Round(float64(clampUnit(b/mQuant)) * 255.0))),
		uint8(mByte),
	}
}

// DecodeRGBM unpacks an 8-bit RGBM texel back to linear HDR RGB irradiance
// using maxRange (defaults to [DefaultRGBMMaxRange] when <= 0).
func DecodeRGBM(rgba [4]uint8, maxRange float32) math3d.Vec3 {
	if maxRange <= 0 || !finite(maxRange) {
		maxRange = DefaultRGBMMaxRange
	}
	scale := (float32(rgba[3]) / 255.0) * maxRange
	return math3d.Vec3{
		X: (float32(rgba[0]) / 255.0) * scale,
		Y: (float32(rgba[1]) / 255.0) * scale,
		Z: (float32(rgba[2]) / 255.0) * scale,
	}
}

// EncodeRGBMImage encodes a row-major slice of width*height linear HDR RGB
// irradiance pixels into a straight-alpha [*image.NRGBA] suitable for upload
// with [UploadTexture] (SRGB: false) and [LightmapRGBM].
func EncodeRGBMImage(hdr []math3d.Vec3, width, height int, maxRange float32) (*image.NRGBA, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("%w: RGBM image dimensions %dx%d must be positive", ErrInvalidLightmap, width, height)
	}
	if len(hdr) < width*height {
		return nil, fmt.Errorf("%w: RGBM pixel slice has %d entries, need %d", ErrInvalidLightmap, len(hdr), width*height)
	}
	if maxRange < 0 || !finite(maxRange) {
		return nil, fmt.Errorf("%w: invalid RGBM maxRange %v", ErrInvalidLightmap, maxRange)
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			px := EncodeRGBM(hdr[y*width+x], maxRange)
			off := img.PixOffset(x, y)
			img.Pix[off+0] = px[0]
			img.Pix[off+1] = px[1]
			img.Pix[off+2] = px[2]
			img.Pix[off+3] = px[3]
		}
	}
	return img, nil
}

// EncodeRGBD packs a linear HDR RGB irradiance value into 8-bit RGBD bytes.
// LDR values (max channel <= 1) store D = 255 with full 8-bit precision; HDR
// values > 1 store D = floor(255 / maxChannel) and decode via rgb / max(d, 1/255).
func EncodeRGBD(rgb math3d.Vec3) [4]uint8 {
	r := clampPositive(rgb.X)
	g := clampPositive(rgb.Y)
	b := clampPositive(rgb.Z)
	peak := max(r, max(g, b))
	if peak <= 1 {
		return [4]uint8{
			clampByte(int(math.Round(float64(r) * 255.0))),
			clampByte(int(math.Round(float64(g) * 255.0))),
			clampByte(int(math.Round(float64(b) * 255.0))),
			255,
		}
	}
	dByte := int(math.Floor(255.0 / float64(peak)))
	if dByte < 1 {
		dByte = 1
	}
	if dByte > 255 {
		dByte = 255
	}
	dQuant := float32(dByte) / 255.0
	return [4]uint8{
		clampByte(int(math.Round(float64(clampUnit(r*dQuant)) * 255.0))),
		clampByte(int(math.Round(float64(clampUnit(g*dQuant)) * 255.0))),
		clampByte(int(math.Round(float64(clampUnit(b*dQuant)) * 255.0))),
		uint8(dByte),
	}
}

// DecodeRGBD unpacks an 8-bit RGBD texel back to linear HDR RGB irradiance.
func DecodeRGBD(rgba [4]uint8) math3d.Vec3 {
	d := float32(rgba[3]) / 255.0
	if d < 1.0/255.0 {
		d = 1.0 / 255.0
	}
	invD := 1.0 / d
	return math3d.Vec3{
		X: (float32(rgba[0]) / 255.0) * invD,
		Y: (float32(rgba[1]) / 255.0) * invD,
		Z: (float32(rgba[2]) / 255.0) * invD,
	}
}

// EncodeRGBDImage encodes a row-major slice of width*height linear HDR RGB
// irradiance pixels into a straight-alpha [*image.NRGBA] suitable for upload
// with [UploadTexture] (SRGB: false) and [LightmapRGBD].
func EncodeRGBDImage(hdr []math3d.Vec3, width, height int) (*image.NRGBA, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("%w: RGBD image dimensions %dx%d must be positive", ErrInvalidLightmap, width, height)
	}
	if len(hdr) < width*height {
		return nil, fmt.Errorf("%w: RGBD pixel slice has %d entries, need %d", ErrInvalidLightmap, len(hdr), width*height)
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			px := EncodeRGBD(hdr[y*width+x])
			off := img.PixOffset(x, y)
			img.Pix[off+0] = px[0]
			img.Pix[off+1] = px[1]
			img.Pix[off+2] = px[2]
			img.Pix[off+3] = px[3]
		}
	}
	return img, nil
}

// UploadLightmapRGBA16F packs linear HDR RGB irradiance pixels into an IEEE-754
// float16 RGBA16F texture ([gpu.FormatRGBA16Float]) and uploads it to dev,
// scoped to cx, for use with [LightmapLinear].
func UploadLightmapRGBA16F(cx *core.Scope, dev *gpu.Device, hdr []math3d.Vec3, width, height int, opts TextureUpload) (*GPUTexture, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("%w: RGBA16F lightmap dimensions %dx%d must be positive", ErrInvalidLightmap, width, height)
	}
	if uint64(width) > math.MaxUint32 || uint64(height) > math.MaxUint32 {
		return nil, fmt.Errorf("%w: RGBA16F lightmap dimensions %dx%d exceed uint32 range", ErrInvalidLightmap, width, height)
	}
	if len(hdr) < width*height {
		return nil, fmt.Errorf("%w: RGBA16F lightmap has %d pixels, need %d", ErrInvalidLightmap, len(hdr), width*height)
	}
	for i := range width * height {
		if !finite3(hdr[i]) {
			return nil, fmt.Errorf("%w: RGBA16F lightmap pixel %d is not finite", ErrInvalidLightmap, i)
		}
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	w, h := uint32(width), uint32(height)
	data := make([]byte, width*height*8)
	oneHalf := float32ToFloat16(1.0)
	for i := range width * height {
		c := hdr[i]
		off := i * 8
		binary.LittleEndian.PutUint16(data[off+0:], float32ToFloat16(clampPositive(c.X)))
		binary.LittleEndian.PutUint16(data[off+2:], float32ToFloat16(clampPositive(c.Y)))
		binary.LittleEndian.PutUint16(data[off+4:], float32ToFloat16(clampPositive(c.Z)))
		binary.LittleEndian.PutUint16(data[off+6:], oneHalf)
	}
	label := opts.Label
	if label == "" {
		label = "render3d.lightmap.rgba16f"
	}
	tex, err := dev.CreateTexture(cx, gpu.TextureDesc{
		Label:     label,
		Dimension: gpu.Dim2D,
		Width:     w,
		Height:    h,
		MipLevels: 1,
		Format:    gpu.FormatRGBA16Float,
		Usage:     gpu.TextureSampled | gpu.TextureCopyDst,
		Retained:  opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	dev.WriteTextureMip(tex, 0, 0, data, w*8)
	return &GPUTexture{
		tex:       tex,
		width:     w,
		height:    h,
		mipLevels: 1,
		format:    gpu.FormatRGBA16Float,
	}, nil
}

// LightmapDirectLight describes an optional dynamic directional light summed
// additively with baked lightmap irradiance (Lambertian N·L diffuse). Setting
// Intensity to 0 disables direct lighting.
type LightmapDirectLight struct {
	// Direction points from the shaded surface toward the light source.
	Direction math3d.Vec3
	// Color is the linear RGB light color.
	Color math3d.Vec3
	// Intensity scales Color; 0 disables direct lighting.
	Intensity float32
}

// LightmapMaterial configures albedo, baked lightmap sampling, encoding, atlas
// UV sub-rectangle transform, fallback ambient irradiance, and optional
// additive direct light for [LightmapRenderer.Draw].
type LightmapMaterial struct {
	// BaseColor is a linear RGBA multiplier on BaseColorTexture. An all-zero
	// value defaults to opaque white {1, 1, 1, 1}.
	BaseColor [4]float32
	// BaseColorTexture is an optional albedo texture sampled at UV0. When nil,
	// a 1x1 white texture is bound.
	BaseColorTexture *GPUTexture
	// BaseColorSampler is an optional sampler for BaseColorTexture (defaults to
	// linear repeat).
	BaseColorSampler *GPUSampler
	// Lightmap is the baked irradiance texture sampled at UV1*UVScale+UVOffset.
	// When nil, AmbientFallback provides the baked irradiance term.
	Lightmap *GPUTexture
	// LightmapSampler is an optional sampler for Lightmap (defaults to linear
	// clamp-to-edge).
	LightmapSampler *GPUSampler
	// Encoding specifies how Lightmap texels are decoded to linear irradiance.
	Encoding LightmapEncoding
	// Intensity scales decoded lightmap irradiance. Zero defaults to 1.0.
	Intensity float32
	// RGBMMaxRange sets the HDR peak scale for [LightmapRGBM]. Zero defaults to
	// [DefaultRGBMMaxRange] (8.0).
	RGBMMaxRange float32
	// UVScale scales UV1 before sampling Lightmap (defaults to {1, 1} when {0, 0}).
	UVScale [2]float32
	// UVOffset offsets scaled UV1 before sampling Lightmap, allowing multiple
	// objects to share a single baked lightmap atlas.
	UVOffset [2]float32
	// AmbientFallback is linear RGB irradiance used when Lightmap is nil.
	AmbientFallback [3]float32
	// DirectLight is an optional dynamic directional light added to the baked
	// irradiance so baked indirect lighting combines cleanly with dynamic sun
	// or key lights.
	DirectLight LightmapDirectLight
}

// DefaultLightmapMaterial returns a [LightmapMaterial] with opaque white base
// color, [LightmapLinear] encoding, unit intensity, default RGBM range, and
// identity atlas UV scale/offset.
func DefaultLightmapMaterial() LightmapMaterial {
	return LightmapMaterial{
		BaseColor:    [4]float32{1, 1, 1, 1},
		Encoding:     LightmapLinear,
		Intensity:    1,
		RGBMMaxRange: DefaultRGBMMaxRange,
		UVScale:      [2]float32{1, 1},
	}
}

// SetUVTransform assigns t.Scale and t.Offset onto m.
func (m *LightmapMaterial) SetUVTransform(t UVTransform) {
	n := t.Normalized()
	m.UVScale = n.Scale
	m.UVOffset = n.Offset
}

// UVTransform returns m's atlas lightmap [UVTransform].
func (m LightmapMaterial) UVTransform() UVTransform {
	return UVTransform{Scale: m.UVScale, Offset: m.UVOffset}.Normalized()
}

// Validate checks that m's numeric fields, encoding, and non-nil GPU handles
// are valid. Every error wraps [ErrInvalidLightmap].
func (m LightmapMaterial) Validate() error {
	for i, c := range m.BaseColor {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: BaseColor[%d] = %v must be finite and non-negative", ErrInvalidLightmap, i, c)
		}
	}
	if !m.Encoding.Valid() {
		return fmt.Errorf("%w: unknown encoding %d", ErrInvalidLightmap, m.Encoding)
	}
	if !finite(m.Intensity) || m.Intensity < 0 {
		return fmt.Errorf("%w: Intensity %v must be finite and non-negative", ErrInvalidLightmap, m.Intensity)
	}
	if !finite(m.RGBMMaxRange) || m.RGBMMaxRange < 0 {
		return fmt.Errorf("%w: RGBMMaxRange %v must be finite and non-negative", ErrInvalidLightmap, m.RGBMMaxRange)
	}
	if err := (UVTransform{Scale: m.UVScale, Offset: m.UVOffset}).Validate(); err != nil {
		return err
	}
	for i, c := range m.AmbientFallback {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: AmbientFallback[%d] = %v must be finite and non-negative", ErrInvalidLightmap, i, c)
		}
	}
	if !finite3(m.DirectLight.Direction) {
		return fmt.Errorf("%w: DirectLight.Direction is not finite", ErrInvalidLightmap)
	}
	if !finite3(m.DirectLight.Color) || m.DirectLight.Color.X < 0 || m.DirectLight.Color.Y < 0 || m.DirectLight.Color.Z < 0 {
		return fmt.Errorf("%w: DirectLight.Color must be finite and non-negative", ErrInvalidLightmap)
	}
	if !finite(m.DirectLight.Intensity) || m.DirectLight.Intensity < 0 {
		return fmt.Errorf("%w: DirectLight.Intensity %v must be finite and non-negative", ErrInvalidLightmap, m.DirectLight.Intensity)
	}
	if m.BaseColorTexture != nil && !m.BaseColorTexture.Valid() {
		return fmt.Errorf("%w: BaseColorTexture is not valid", ErrInvalidLightmap)
	}
	if m.BaseColorSampler != nil && !m.BaseColorSampler.Valid() {
		return fmt.Errorf("%w: BaseColorSampler is not valid", ErrInvalidLightmap)
	}
	if m.Lightmap != nil {
		if !m.Lightmap.Valid() {
			return fmt.Errorf("%w: Lightmap texture is not valid", ErrInvalidLightmap)
		}
		if (m.Encoding == LightmapRGBM || m.Encoding == LightmapRGBD) && m.Lightmap.SRGB() {
			return fmt.Errorf("%w: %s lightmap texture must be uploaded with SRGB=false", ErrInvalidLightmap, m.Encoding)
		}
	}
	if m.LightmapSampler != nil && !m.LightmapSampler.Valid() {
		return fmt.Errorf("%w: LightmapSampler is not valid", ErrInvalidLightmap)
	}
	return nil
}

// LightmapWGSL is the WGSL shader module compiled by [NewLightmapRenderer].
// It uses only pow and algebraic math (no exp/exp2/log2) so it executes
// identically across hardware GPU backends and NSGo's software GPU VM.
const LightmapWGSL = CameraWGSL + `
struct LightmapDraw {
	model: mat4x4<f32>,
	base_color: vec4<f32>,
	uv_transform: vec4<f32>,
	lm_params: vec4<f32>,
	ambient: vec4<f32>,
	light_dir: vec4<f32>,
	light_color: vec4<f32>,
}

@group(1) @binding(0) var<uniform> lm_draw: LightmapDraw;

@group(2) @binding(0) var base_tex: texture_2d<f32>;
@group(2) @binding(1) var base_samp: sampler;
@group(2) @binding(2) var lightmap_tex: texture_2d<f32>;
@group(2) @binding(3) var lightmap_samp: sampler;

struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv0: vec2<f32>,
	@location(3) uv1: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) normal: vec3<f32>,
	@location(1) uv0: vec2<f32>,
	@location(2) uv1: vec2<f32>,
}

fn srgb_to_linear_channel(c: f32) -> f32 {
	if (c <= 0.04045) {
		return c / 12.92;
	}
	return pow((c + 0.055) / 1.055, 2.4);
}

fn srgb_to_linear(c: vec3<f32>) -> vec3<f32> {
	return vec3<f32>(
		srgb_to_linear_channel(c.x),
		srgb_to_linear_channel(c.y),
		srgb_to_linear_channel(c.z),
	);
}

fn decode_lightmap(sample: vec4<f32>, encoding: f32, max_range: f32) -> vec3<f32> {
	if (encoding < 0.5) {
		return sample.rgb;
	}
	if (encoding < 1.5) {
		let clamped = clamp(sample.rgb, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
		return srgb_to_linear(clamped);
	}
	if (encoding < 2.5) {
		return sample.rgb * (sample.a * max_range);
	}
	let d = max(sample.a, 1.0 / 255.0);
	return sample.rgb / d;
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	let world_pos = lm_draw.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world_pos;
	out.normal = (lm_draw.model * vec4<f32>(in.normal, 0.0)).xyz;
	out.uv0 = in.uv0;
	out.uv1 = in.uv1;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let base_sample = textureSample(base_tex, base_samp, in.uv0);
	let albedo = base_sample * lm_draw.base_color;

	var baked = lm_draw.ambient.xyz;
	if (lm_draw.lm_params.w > 0.5) {
		let lm_uv = in.uv1 * lm_draw.uv_transform.xy + lm_draw.uv_transform.zw;
		let lm_sample = textureSample(lightmap_tex, lightmap_samp, lm_uv);
		baked = decode_lightmap(lm_sample, lm_draw.lm_params.x, lm_draw.lm_params.z) * lm_draw.lm_params.y;
	}

	var direct = vec3<f32>(0.0, 0.0, 0.0);
	if (lm_draw.light_dir.w > 0.0) {
		let l_len = length(lm_draw.light_dir.xyz);
		let n_len = length(in.normal);
		if (l_len > 0.0001 && n_len > 0.0001) {
			let l_dir = lm_draw.light_dir.xyz / l_len;
			let n_dir = in.normal / n_len;
			let ndl = max(dot(n_dir, l_dir), 0.0);
			direct = lm_draw.light_color.xyz * (lm_draw.light_dir.w * ndl);
		}
	}

	let lit = albedo.rgb * (baked + direct);
	return vec4<f32>(lit, albedo.a);
}
`

// LightmapRendererOptions configures [NewLightmapRenderer]. The zero value
// enables depth testing ([gpu.FormatDepth24Plus]), back-face culling
// ([gpu.CullBack] with [gpu.FrontCCW]), and targets the device surface format.
type LightmapRendererOptions struct {
	// ColorFormat overrides the color target format (defaults to
	// dev.SurfaceFormat(), or [gpu.FormatRGBA8Unorm] if undefined).
	ColorFormat gpu.TextureFormat
	// DepthFormat overrides the depth target format when DisableDepth is false
	// (defaults to [gpu.FormatDepth24Plus]).
	DepthFormat gpu.TextureFormat
	// DisableDepth creates the pipeline without a depth-stencil attachment.
	DisableDepth bool
	// DoubleSided disables back-face culling (uses [gpu.CullNone]).
	DoubleSided bool
	// Retained keeps internal GPU resources across device loss.
	Retained bool
	// InitialCapacity sets the initial per-frame dynamic uniform slot count
	// (defaults to 64).
	InitialCapacity int
	// Label prefixes GPU resource debug labels.
	Label string
}

type lightmapTexKey struct {
	baseTex *gpu.Texture
	baseSmp *gpu.Sampler
	lmTex   *gpu.Texture
	lmSmp   *gpu.Sampler
}

// LightmapRenderer draws [GPULightmapMesh] geometry shaded with baked lightmaps
// (sampling albedo at UV0 and baked irradiance at UV1 * UVScale + UVOffset),
// supporting linear/HDR RGBA16F, sRGB LDR, RGBM, and RGBD lightmaps as well as
// optional additive dynamic directional lighting.
//
// Call [LightmapRenderer.BeginFrame] at the start of each frame to reuse the
// dynamic uniform buffer without steady-state allocations.
type LightmapRenderer struct {
	cx       *core.Scope
	dev      *gpu.Device
	retained bool
	label    string

	pipeline   *gpu.Pipeline
	drawLayout *gpu.BindGroupLayout
	texLayout  *gpu.BindGroupLayout

	drawBuf     *gpu.Buffer
	drawGroup   *gpu.BindGroup
	capacity    int
	usedSlots   int
	retiredBufs []*gpu.Buffer
	retiredGrps []*gpu.BindGroup
	scratch     [LightmapDrawUniformSize]byte

	whiteTex   *GPUTexture
	albedoSamp *GPUSampler
	lmSamp     *GPUSampler
	texGroups  map[lightmapTexKey]*gpu.BindGroup
	closed     bool
}

// NewLightmapRenderer compiles [LightmapWGSL] and creates the pipeline,
// fallback 1x1 white texture, default samplers, and dynamic per-draw uniform
// ring buffer on dev, owned by cx.
func NewLightmapRenderer(cx *core.Scope, dev *gpu.Device, camLayout *gpu.BindGroupLayout, opts LightmapRendererOptions) (*LightmapRenderer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() {
		return nil, fmt.Errorf("%w: invalid camera bind group layout", ErrInvalidLightmap)
	}
	if opts.InitialCapacity < 0 {
		return nil, fmt.Errorf("%w: InitialCapacity %d must be non-negative", ErrInvalidLightmap, opts.InitialCapacity)
	}
	capacity := opts.InitialCapacity
	if capacity == 0 {
		capacity = 64
	}
	label := opts.Label
	if label == "" {
		label = "render3d.lightmap"
	}

	mod, err := gpu.CompileWGSL(LightmapWGSL)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile lightmap shader: %w", err)
	}

	drawLayout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".draw",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:          0,
			Visibility:       gpu.StageVertex | gpu.StageFragment,
			Type:             gpu.BindingUniform,
			HasDynamicOffset: true,
		}},
	})
	if err != nil {
		return nil, err
	}

	texLayout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".textures",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		_ = drawLayout.Close()
		return nil, err
	}

	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
		if colorFmt == gpu.FormatUndefined {
			colorFmt = gpu.FormatRGBA8Unorm
		}
	}
	cull := gpu.CullBack
	if opts.DoubleSided {
		cull = gpu.CullNone
	}
	pdesc := gpu.PipelineDesc{
		Label:         label + ".pipeline",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{camLayout, drawLayout, texLayout},
		VertexBuffers: []gpu.VertexBufferLayout{LightmapVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  cull,
		},
		Targets:  []gpu.ColorTargetState{{Format: colorFmt, WriteMask: gpu.ColorWriteAll}},
		Retained: opts.Retained,
	}
	if !opts.DisableDepth {
		depthFmt := opts.DepthFormat
		if depthFmt == gpu.FormatUndefined {
			depthFmt = gpu.FormatDepth24Plus
		}
		pdesc.DepthStencil = &gpu.DepthStencilState{
			Format:       depthFmt,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		}
	}
	pipe, err := dev.CreatePipeline(cx, pdesc)
	if err != nil {
		_ = texLayout.Close()
		_ = drawLayout.Close()
		return nil, err
	}

	whiteTex, err := UploadTexturePixels(cx, dev, PixelBuffer{
		Width:  1,
		Height: 1,
		Format: gpu.FormatRGBA8Unorm,
		Pixels: []byte{255, 255, 255, 255},
	}, TextureUpload{
		Label:    label + ".white",
		Retained: opts.Retained,
	})
	if err != nil {
		_ = pipe.Close()
		_ = texLayout.Close()
		_ = drawLayout.Close()
		return nil, err
	}

	albedoOpts := SamplerLinearRepeat()
	albedoOpts.Label = label + ".albedo_sampler"
	albedoOpts.Retained = opts.Retained
	albedoSamp, err := NewGPUSampler(cx, dev, albedoOpts)
	if err != nil {
		_ = whiteTex.Close()
		_ = pipe.Close()
		_ = texLayout.Close()
		_ = drawLayout.Close()
		return nil, err
	}

	lmOpts := SamplerLinearClamp()
	lmOpts.Label = label + ".lightmap_sampler"
	lmOpts.Retained = opts.Retained
	lmSamp, err := NewGPUSampler(cx, dev, lmOpts)
	if err != nil {
		_ = albedoSamp.Close()
		_ = whiteTex.Close()
		_ = pipe.Close()
		_ = texLayout.Close()
		_ = drawLayout.Close()
		return nil, err
	}

	r := &LightmapRenderer{
		cx:         cx,
		dev:        dev,
		retained:   opts.Retained,
		label:      label,
		pipeline:   pipe,
		drawLayout: drawLayout,
		texLayout:  texLayout,
		whiteTex:   whiteTex,
		albedoSamp: albedoSamp,
		lmSamp:     lmSamp,
		texGroups:  make(map[lightmapTexKey]*gpu.BindGroup),
	}
	if err := r.allocateDrawRing(capacity); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

func (r *LightmapRenderer) allocateDrawRing(capacity int) error {
	size := uint64(capacity) * LightmapDrawUniformStride
	buf, err := r.dev.CreateBuffer(r.cx, gpu.BufferDesc{
		Label:    r.label + ".draws",
		Size:     size,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: r.retained,
	})
	if err != nil {
		return err
	}
	grp, err := r.dev.CreateBindGroupDesc(r.cx, gpu.BindGroupDesc{
		Label:  r.label + ".draws",
		Layout: r.drawLayout,
		Entries: []gpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  buf,
			Offset:  0,
			Size:    LightmapDrawUniformSize,
		}},
	})
	if err != nil {
		_ = buf.Close()
		return err
	}
	if r.drawBuf != nil {
		r.retiredBufs = append(r.retiredBufs, r.drawBuf)
	}
	if r.drawGroup != nil {
		r.retiredGrps = append(r.retiredGrps, r.drawGroup)
	}
	r.drawBuf = buf
	r.drawGroup = grp
	r.capacity = capacity
	r.usedSlots = 0
	return nil
}

// BeginFrame resets the per-frame dynamic uniform ring offset and releases any
// older uniform buffers retired when the ring grew in a previous frame.
func (r *LightmapRenderer) BeginFrame() {
	if r == nil || r.closed {
		return
	}
	r.usedSlots = 0
	for _, g := range r.retiredGrps {
		_ = g.Close()
	}
	r.retiredGrps = r.retiredGrps[:0]
	for _, b := range r.retiredBufs {
		_ = b.Close()
	}
	r.retiredBufs = r.retiredBufs[:0]
}

// Pipeline returns the compiled lightmap [gpu.Pipeline].
func (r *LightmapRenderer) Pipeline() *gpu.Pipeline {
	if r == nil {
		return nil
	}
	return r.pipeline
}

// Valid reports whether r is open and its GPU resources belong to a live device.
func (r *LightmapRenderer) Valid() bool {
	return r != nil && !r.closed && r.pipeline.Valid() && r.drawBuf.Valid() && r.drawGroup.Valid()
}

// Draw encodes the model transform and mat into a dynamic uniform slot, binds
// camera, per-draw uniforms, and albedo/lightmap textures on pass, and draws
// mesh. Multiple objects with different transforms, materials, or shared atlas
// UV sub-rectangles may be drawn within the same render pass.
func (r *LightmapRenderer) Draw(pass *gpu.RenderPass, cam *CameraBuffer, mesh *GPULightmapMesh, model math3d.Mat4, mat LightmapMaterial) error {
	if !r.Valid() {
		return fmt.Errorf("%w: renderer is not valid", ErrInvalidLightmap)
	}
	if pass == nil {
		return fmt.Errorf("%w: nil render pass", ErrInvalidLightmap)
	}
	if cam == nil || !cam.BindGroup().Valid() {
		return fmt.Errorf("%w: invalid camera buffer", ErrInvalidLightmap)
	}
	if !mesh.Valid() {
		return fmt.Errorf("%w: mesh is not valid", ErrInvalidMesh)
	}
	if err := mat.Validate(); err != nil {
		return err
	}
	if r.usedSlots >= r.capacity {
		if err := r.allocateDrawRing(max(r.capacity*2, 64)); err != nil {
			return err
		}
	}

	slot := r.usedSlots
	r.usedSlots++
	offset := uint64(slot) * LightmapDrawUniformStride
	encodeLightmapDrawUniform(r.scratch[:], model, mat)
	r.dev.WriteBuffer(r.drawBuf, offset, r.scratch[:])

	texGroup, err := r.resolveTextureGroup(mat)
	if err != nil {
		return err
	}

	pass.SetPipeline(r.pipeline)
	pass.SetBindGroup(CameraGroup, cam.BindGroup())
	pass.SetBindGroup(LightmapDrawGroup, r.drawGroup, uint32(offset))
	pass.SetBindGroup(LightmapTextureGroup, texGroup)
	mesh.Draw(pass)
	return nil
}

func (r *LightmapRenderer) resolveTextureGroup(mat LightmapMaterial) (*gpu.BindGroup, error) {
	baseTex := r.whiteTex
	if mat.BaseColorTexture != nil {
		baseTex = mat.BaseColorTexture
	}
	baseSmp := r.albedoSamp
	if mat.BaseColorSampler != nil {
		baseSmp = mat.BaseColorSampler
	}
	lmTex := r.whiteTex
	if mat.Lightmap != nil {
		lmTex = mat.Lightmap
	}
	lmSmp := r.lmSamp
	if mat.LightmapSampler != nil {
		lmSmp = mat.LightmapSampler
	}

	key := lightmapTexKey{
		baseTex: baseTex.Texture(),
		baseSmp: baseSmp.Sampler(),
		lmTex:   lmTex.Texture(),
		lmSmp:   lmSmp.Sampler(),
	}
	if existing, ok := r.texGroups[key]; ok && existing.Valid() {
		return existing, nil
	}
	grp, err := r.dev.CreateBindGroupDesc(r.cx, gpu.BindGroupDesc{
		Label:  r.label + ".textures",
		Layout: r.texLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Texture: key.baseTex},
			{Binding: 1, Sampler: key.baseSmp},
			{Binding: 2, Texture: key.lmTex},
			{Binding: 3, Sampler: key.lmSmp},
		},
	})
	if err != nil {
		return nil, err
	}
	r.texGroups[key] = grp
	return grp, nil
}

// Close releases all GPU resources created by r. It is safe to call more than once.
func (r *LightmapRenderer) Close() error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	var first error
	record := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	for k, g := range r.texGroups {
		record(g.Close())
		delete(r.texGroups, k)
	}
	for _, g := range r.retiredGrps {
		record(g.Close())
	}
	r.retiredGrps = nil
	for _, b := range r.retiredBufs {
		record(b.Close())
	}
	r.retiredBufs = nil
	if r.drawGroup != nil {
		record(r.drawGroup.Close())
	}
	if r.drawBuf != nil {
		record(r.drawBuf.Close())
	}
	if r.lmSamp != nil {
		record(r.lmSamp.Close())
	}
	if r.albedoSamp != nil {
		record(r.albedoSamp.Close())
	}
	if r.whiteTex != nil {
		record(r.whiteTex.Close())
	}
	if r.pipeline != nil {
		record(r.pipeline.Close())
	}
	if r.texLayout != nil {
		record(r.texLayout.Close())
	}
	if r.drawLayout != nil {
		record(r.drawLayout.Close())
	}
	return first
}

func encodeLightmapDrawUniform(dst []byte, model math3d.Mat4, mat LightmapMaterial) {
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
		off += 4
	}
	for _, f := range model {
		put(f)
	}

	bc := mat.BaseColor
	if bc == [4]float32{0, 0, 0, 0} {
		bc = [4]float32{1, 1, 1, 1}
	}
	put(bc[0])
	put(bc[1])
	put(bc[2])
	put(bc[3])

	uvt := mat.UVTransform()
	put(uvt.Scale[0])
	put(uvt.Scale[1])
	put(uvt.Offset[0])
	put(uvt.Offset[1])

	encMode := float32(mat.Encoding)
	if mat.Encoding == LightmapSRGB && mat.Lightmap != nil && mat.Lightmap.SRGB() {
		// Hardware sampler already decodes FormatRGBA8UnormSRGB to linear.
		encMode = float32(LightmapLinear)
	}
	intensity := mat.Intensity
	if intensity == 0 {
		intensity = 1.0
	}
	rgbmRange := mat.RGBMMaxRange
	if rgbmRange == 0 {
		rgbmRange = DefaultRGBMMaxRange
	}
	var hasLM float32
	if mat.Lightmap != nil {
		hasLM = 1.0
	}
	put(encMode)
	put(intensity)
	put(rgbmRange)
	put(hasLM)

	put(mat.AmbientFallback[0])
	put(mat.AmbientFallback[1])
	put(mat.AmbientFallback[2])
	put(0)

	put(mat.DirectLight.Direction.X)
	put(mat.DirectLight.Direction.Y)
	put(mat.DirectLight.Direction.Z)
	put(mat.DirectLight.Intensity)

	put(mat.DirectLight.Color.X)
	put(mat.DirectLight.Color.Y)
	put(mat.DirectLight.Color.Z)
	put(0)
}

func clampPositive(v float32) float32 {
	if !finite(v) || v <= 0 {
		return 0
	}
	return v
}

func clampUnit(v float32) float32 {
	if !finite(v) || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

// float32ToFloat16 converts a finite float32 to IEEE-754 binary16 (round-to-nearest-even).
func float32ToFloat16(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16((bits >> 16) & 0x8000)
	exp := int((bits>>23)&0xFF) - 127 + 15
	mant := bits & 0x007FFFFF

	if exp <= 0 {
		if exp < -10 {
			return sign
		}
		mant = (mant | 0x00800000) >> uint(1-exp)
		if (mant&0x00001000) != 0 && ((mant & 0x00002FFF) != 0) {
			mant += 0x00002000
		}
		return sign | uint16(mant>>13)
	}
	if exp >= 31 {
		// Saturate finite large values to max finite half (65504.0).
		return sign | 0x7BFF
	}
	if (mant&0x00001000) != 0 && ((mant & 0x00002FFF) != 0) {
		mant += 0x00002000
		if (mant & 0x00800000) != 0 {
			mant = 0
			exp++
			if exp >= 31 {
				return sign | 0x7BFF
			}
		}
	}
	return sign | uint16(exp<<10) | uint16(mant>>13)
}
