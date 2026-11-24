package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Portable GPU skinning limits, uniform layout, bind group slot, and
// interleaved vertex layout.
const (
	// MaxSkinJoints is the maximum number of joint matrices stored in one
	// [GPUSkinPalette] uniform buffer. 128 mat4x4<f32> entries occupy 8192 bytes
	// ([SkinUniformSize]), staying comfortably below WebGL 2 and OpenGL ES 3.0's
	// 16 KiB minimum MAX_UNIFORM_BLOCK_SIZE while covering full character rigs.
	MaxSkinJoints = 128

	// SkinUniformSize is the byte size of the skin uniform block that
	// [AppendJointMatrices] writes and [SkinWGSL] declares: MaxSkinJoints * 64 = 8192 bytes.
	SkinUniformSize = MaxSkinJoints * 64

	// SkinGroup is the bind group index reserved for the skin joint palette
	// ([GPUSkinPalette.BindGroup]) in [SkinWGSL] and [NewSkinnedMeshPipeline].
	SkinGroup = 1

	// SkinnedVertexStride is the byte size of one interleaved skinned vertex:
	// position (12 bytes), normal (12 bytes), UV (8 bytes), four joint indices
	// (16 bytes), and four normalized weights (16 bytes).
	SkinnedVertexStride = 64

	// SkinnedPositionOffset is the byte offset of the float32x3 position inside a skinned vertex.
	SkinnedPositionOffset = 0
	// SkinnedNormalOffset is the byte offset of the float32x3 normal inside a skinned vertex.
	SkinnedNormalOffset = 12
	// SkinnedUVOffset is the byte offset of the float32x2 texture coordinate inside a skinned vertex.
	SkinnedUVOffset = 24
	// SkinnedJointsOffset is the byte offset of the uint32x4 joint indices inside a skinned vertex.
	SkinnedJointsOffset = 32
	// SkinnedWeightsOffset is the byte offset of the float32x4 joint weights inside a skinned vertex.
	SkinnedWeightsOffset = 48

	// SkinnedPositionLocation is the shader @location of the position attribute.
	SkinnedPositionLocation = MeshPositionLocation
	// SkinnedNormalLocation is the shader @location of the normal attribute.
	SkinnedNormalLocation = MeshNormalLocation
	// SkinnedUVLocation is the shader @location of the texture coordinate attribute.
	SkinnedUVLocation = MeshUVLocation
	// SkinnedJointsLocation is the shader @location of the uint32x4 joint indices attribute.
	SkinnedJointsLocation = 3
	// SkinnedWeightsLocation is the shader @location of the float32x4 joint weights attribute.
	SkinnedWeightsLocation = 4
)

// ErrInvalidSkin is wrapped by errors returned for malformed skinning attributes,
// out-of-range joint indices, invalid glTF skin definitions, or oversized joint palettes.
var ErrInvalidSkin = errors.New("render3d: invalid skin")

// SkinWGSL declares the portable joint palette uniform block at @group(1) @binding(0)
// and linear blend skinning helpers matching CPU [SkinVertex]. Prepend [CameraWGSL]
// and [SkinWGSL] to a custom skinned vertex shader, or use [SkinnedMeshWGSL] via
// [NewSkinnedMeshPipeline].
//
// Blending is evaluated per-influence as sum_i (skin.joints[j_i] * v) * w_i rather
// than summing scaled 4x4 matrices first, which is both fewer scalar operations on
// GPU hardware and exact across every NSGo backend (Metal, D3D11, OpenGL 3.3,
// WebGPU, WebGL 2, OpenGL ES 3.0, and the software rasterizer).
const SkinWGSL = `struct SkinPalette {
	joints: array<mat4x4<f32>, 128>,
}

@group(1) @binding(0) var<uniform> skin: SkinPalette;

fn skin_position(pos: vec3<f32>, joints: vec4<u32>, weights: vec4<f32>) -> vec3<f32> {
	let p = vec4<f32>(pos, 1.0);
	let blended = (skin.joints[joints.x] * p) * weights.x
		+ (skin.joints[joints.y] * p) * weights.y
		+ (skin.joints[joints.z] * p) * weights.z
		+ (skin.joints[joints.w] * p) * weights.w;
	return blended.xyz;
}

fn skin_normal(normal: vec3<f32>, joints: vec4<u32>, weights: vec4<f32>) -> vec3<f32> {
	let n = vec4<f32>(normal, 0.0);
	let blended = (skin.joints[joints.x] * n).xyz * weights.x
		+ (skin.joints[joints.y] * n).xyz * weights.y
		+ (skin.joints[joints.z] * n).xyz * weights.z
		+ (skin.joints[joints.w] * n).xyz * weights.w;
	let len_sq = dot(blended, blended);
	if (len_sq > 1e-12) {
		return normalize(blended);
	}
	return blended;
}
`

// SkinnedMeshWGSL is the default ready-to-use WGSL module compiled by
// [NewSkinnedMeshPipeline]. It binds [CameraBuffer] at [CameraGroup] (group 0)
// and [GPUSkinPalette] at [SkinGroup] (group 1), skins positions and normals
// with [SkinWGSL], and applies view-aligned shading with normal-based coloring.
const SkinnedMeshWGSL = CameraWGSL + "\n" + SkinWGSL + `
struct SkinnedVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
	@location(3) joints: vec4<u32>,
	@location(4) weights: vec4<f32>,
}

struct SkinnedVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

@vertex
fn vs_main(in: SkinnedVertexIn) -> SkinnedVertexOut {
	var out: SkinnedVertexOut;
	let world_pos = skin_position(in.pos, in.joints, in.weights);
	let world_nrm = skin_normal(in.normal, in.joints, in.weights);
	out.clip = camera.view_proj * vec4<f32>(world_pos, 1.0);
	out.world = world_pos;
	out.normal = world_nrm;
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: SkinnedVertexOut) -> @location(0) vec4<f32> {
	var n = vec3<f32>(0.0, 1.0, 0.0);
	if (dot(in.normal, in.normal) > 1e-12) {
		n = normalize(in.normal);
	}
	let v = normalize(camera.eye.xyz - in.world * camera.eye.w);
	let ndotl = max(dot(n, v), 0.0);
	let shade = 0.25 + 0.75 * ndotl;
	let base = vec3<f32>(0.55 + 0.25 * n.x, 0.55 + 0.25 * n.y, 0.6 + 0.25 * n.z);
	return vec4<f32>(base * shade, 1.0);
}
`

// SkinnedMesh is indexed or non-indexed triangle-list geometry with up to four
// joint influences per vertex, ready to upload with [UploadSkinnedMesh].
//
// Positions, Joints, and Weights are required and must have equal length.
// Normals and UVs are optional (empty or one entry per position). Indices is
// optional (empty for non-indexed triangle lists). JointCount optionally records
// the skin's joint count; when > 0, [SkinnedMesh.Validate] enforces that every
// joint index is strictly less than JointCount (and JointCount <= [MaxSkinJoints]).
// When JointCount == 0, every joint index must be strictly less than [MaxSkinJoints].
type SkinnedMesh struct {
	Positions  []math3d.Vec3
	Normals    []math3d.Vec3
	UVs        []math3d.Vec2
	Joints     [][4]uint16
	Weights    []math3d.Vec4
	Indices    []uint32
	JointCount int
}

// NormalizeSkinWeights normalizes non-negative joint weights so their sum equals 1.
// If the sum is zero or non-positive, it falls back to (1, 0, 0, 0) so vertices stay
// rigidly attached to their first joint instead of collapsing to the origin.
func NormalizeSkinWeights(w math3d.Vec4) math3d.Vec4 {
	sum := w.X + w.Y + w.Z + w.W
	if sum <= 0 || !finite(sum) {
		return math3d.Vec4{X: 1}
	}
	inv := 1 / sum
	return math3d.Vec4{
		X: w.X * inv,
		Y: w.Y * inv,
		Z: w.Z * inv,
		W: w.W * inv,
	}
}

// SkinnedMeshFromGLTF extracts and validates a [SkinnedMesh] from a glTF 2.0
// triangle primitive and its associated skin, normalizing vertex weights so their
// four components sum to 1. Geometry errors wrap [ErrInvalidMesh] and skinning
// errors wrap [ErrInvalidSkin].
func SkinnedMeshFromGLTF(prim gltf.Primitive, skin gltf.Skin) (SkinnedMesh, error) {
	if prim.Mode != gltf.ModeTriangles {
		return SkinnedMesh{}, fmt.Errorf("%w: glTF primitive mode %d is not triangles", ErrInvalidMesh, prim.Mode)
	}
	if len(skin.Joints) == 0 {
		return SkinnedMesh{}, fmt.Errorf("%w: glTF skin has no joints", ErrInvalidSkin)
	}
	if len(skin.Joints) > MaxSkinJoints {
		return SkinnedMesh{}, fmt.Errorf("%w: glTF skin has %d joints, exceeding MaxSkinJoints (%d)", ErrInvalidSkin, len(skin.Joints), MaxSkinJoints)
	}
	n := len(prim.Positions)
	if len(prim.Weights0) != n {
		return SkinnedMesh{}, fmt.Errorf("%w: %d WEIGHTS_0 entries for %d positions", ErrInvalidSkin, len(prim.Weights0), n)
	}
	normWeights := make([]math3d.Vec4, n)
	for i, w := range prim.Weights0 {
		if err := validateWeights(i, w); err != nil {
			return SkinnedMesh{}, err
		}
		normWeights[i] = NormalizeSkinWeights(w)
	}
	sm := SkinnedMesh{
		Positions:  prim.Positions,
		Normals:    prim.Normals,
		UVs:        prim.TexCoords0,
		Joints:     prim.Joints0,
		Weights:    normWeights,
		Indices:    prim.Indices,
		JointCount: len(skin.Joints),
	}
	if err := sm.Validate(); err != nil {
		return SkinnedMesh{}, err
	}
	return sm, nil
}

// SkinnedMeshesFromGLTFNode extracts every triangle primitive of doc's skinned
// mesh node at nodeIndex against its referenced skin.
func SkinnedMeshesFromGLTFNode(doc *gltf.Document, nodeIndex int) ([]SkinnedMesh, error) {
	if doc == nil {
		return nil, fmt.Errorf("%w: nil glTF document", ErrInvalidSkin)
	}
	if nodeIndex < 0 || nodeIndex >= len(doc.Nodes) {
		return nil, fmt.Errorf("%w: node index %d out of range for %d nodes", ErrInvalidSkin, nodeIndex, len(doc.Nodes))
	}
	node := doc.Nodes[nodeIndex]
	if node.Mesh < 0 || node.Mesh >= len(doc.Meshes) {
		return nil, fmt.Errorf("%w: node %d has no valid mesh (%d)", ErrInvalidSkin, nodeIndex, node.Mesh)
	}
	if node.Skin < 0 || node.Skin >= len(doc.Skins) {
		return nil, fmt.Errorf("%w: node %d has no valid skin (%d)", ErrInvalidSkin, nodeIndex, node.Skin)
	}
	mesh := doc.Meshes[node.Mesh]
	skin := doc.Skins[node.Skin]
	if len(mesh.Primitives) == 0 {
		return nil, fmt.Errorf("%w: mesh %d has no primitives", ErrInvalidMesh, node.Mesh)
	}
	out := make([]SkinnedMesh, 0, len(mesh.Primitives))
	for i, prim := range mesh.Primitives {
		sm, err := SkinnedMeshFromGLTF(prim, skin)
		if err != nil {
			return nil, fmt.Errorf("primitive %d: %w", i, err)
		}
		out = append(out, sm)
	}
	return out, nil
}

// Validate checks both the triangle geometry (wrapping [ErrInvalidMesh]) and the
// skinning attributes (wrapping [ErrInvalidSkin]): matching Joints and Weights
// counts, JointCount in [0, MaxSkinJoints], finite non-negative weights, and every
// joint index strictly below JointCount (or [MaxSkinJoints] when JointCount == 0).
func (m SkinnedMesh) Validate() error {
	base := Mesh{
		Positions: m.Positions,
		Normals:   m.Normals,
		UVs:       m.UVs,
		Indices:   m.Indices,
	}
	if err := base.Validate(); err != nil {
		return err
	}
	if m.JointCount < 0 {
		return fmt.Errorf("%w: negative joint count %d", ErrInvalidSkin, m.JointCount)
	}
	if m.JointCount > MaxSkinJoints {
		return fmt.Errorf("%w: joint count %d exceeds MaxSkinJoints (%d)", ErrInvalidSkin, m.JointCount, MaxSkinJoints)
	}
	n := len(m.Positions)
	if len(m.Joints) != n {
		return fmt.Errorf("%w: %d joint entries for %d positions", ErrInvalidSkin, len(m.Joints), n)
	}
	if len(m.Weights) != n {
		return fmt.Errorf("%w: %d weight entries for %d positions", ErrInvalidSkin, len(m.Weights), n)
	}
	limit := MaxSkinJoints
	if m.JointCount > 0 {
		limit = m.JointCount
	}
	for i, js := range m.Joints {
		for k, j := range js {
			if int(j) >= limit {
				return fmt.Errorf("%w: vertex %d joint[%d] = %d out of range [0, %d)", ErrInvalidSkin, i, k, j, limit)
			}
		}
	}
	for i, w := range m.Weights {
		if err := validateWeights(i, w); err != nil {
			return err
		}
	}
	return nil
}

func validateWeights(vertex int, w math3d.Vec4) error {
	if !finite(w.X) || !finite(w.Y) || !finite(w.Z) || !finite(w.W) {
		return fmt.Errorf("%w: vertex %d weights are not finite", ErrInvalidSkin, vertex)
	}
	if w.X < 0 || w.Y < 0 || w.Z < 0 || w.W < 0 {
		return fmt.Errorf("%w: vertex %d has negative weight %v", ErrInvalidSkin, vertex, w)
	}
	return nil
}

// VertexCount returns the number of vertices, len(m.Positions).
func (m SkinnedMesh) VertexCount() int { return len(m.Positions) }

// Indexed reports whether m has an index list.
func (m SkinnedMesh) Indexed() bool { return len(m.Indices) != 0 }

// IndexFormat returns [gpu.IndexUint16] when every vertex fits below 0xFFFF
// (avoiding WebGL 2's fixed primitive restart index), otherwise [gpu.IndexUint32].
func (m SkinnedMesh) IndexFormat() gpu.IndexFormat {
	if len(m.Positions) <= maxUint16Vertices {
		return gpu.IndexUint16
	}
	return gpu.IndexUint32
}

// Bounds returns the object-space bind-pose axis-aligned box around m.Positions,
// or [math3d.AABBEmpty] when m has none.
func (m SkinnedMesh) Bounds() math3d.AABB {
	b := math3d.AABBEmpty()
	for _, p := range m.Positions {
		b = b.ExpandPoint(p)
	}
	return b
}

// VertexDataSize returns the byte length [SkinnedMesh.AppendVertexData] appends.
func (m SkinnedMesh) VertexDataSize() int { return len(m.Positions) * SkinnedVertexStride }

// IndexDataSize returns the 4-byte-padded byte length [SkinnedMesh.AppendIndexData]
// appends, or zero for a non-indexed mesh.
func (m SkinnedMesh) IndexDataSize() int {
	n := len(m.Indices)
	if m.IndexFormat() == gpu.IndexUint32 {
		return n * 4
	}
	return (n*2 + 3) &^ 3
}

// AppendVertexData appends m's skinned vertices to dst in the little-endian
// interleaved layout of [SkinnedVertexLayout] and returns the extended slice.
// Missing normals and UVs are written as zeros; joint weights are normalized
// via [NormalizeSkinWeights] so every vertex sums to 1. It allocates only when
// dst lacks capacity for [SkinnedMesh.VertexDataSize] more bytes. Call
// [SkinnedMesh.Validate] before encoding untrusted data.
func (m SkinnedMesh) AppendVertexData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, m.VertexDataSize())
	out := dst[start:]
	hasNormals := len(m.Normals) == len(m.Positions)
	hasUVs := len(m.UVs) == len(m.Positions)
	hasJoints := len(m.Joints) == len(m.Positions)
	hasWeights := len(m.Weights) == len(m.Positions)
	for i, p := range m.Positions {
		v := out[i*SkinnedVertexStride : (i+1)*SkinnedVertexStride]
		putVec3(v[SkinnedPositionOffset:], p)
		if hasNormals {
			putVec3(v[SkinnedNormalOffset:], m.Normals[i])
		} else {
			clear(v[SkinnedNormalOffset:SkinnedUVOffset])
		}
		if hasUVs {
			binary.LittleEndian.PutUint32(v[SkinnedUVOffset:], math.Float32bits(m.UVs[i].X))
			binary.LittleEndian.PutUint32(v[SkinnedUVOffset+4:], math.Float32bits(m.UVs[i].Y))
		} else {
			clear(v[SkinnedUVOffset:SkinnedJointsOffset])
		}
		if hasJoints {
			j := m.Joints[i]
			binary.LittleEndian.PutUint32(v[SkinnedJointsOffset+0:], uint32(j[0]))
			binary.LittleEndian.PutUint32(v[SkinnedJointsOffset+4:], uint32(j[1]))
			binary.LittleEndian.PutUint32(v[SkinnedJointsOffset+8:], uint32(j[2]))
			binary.LittleEndian.PutUint32(v[SkinnedJointsOffset+12:], uint32(j[3]))
		} else {
			clear(v[SkinnedJointsOffset:SkinnedWeightsOffset])
		}
		w := math3d.Vec4{X: 1}
		if hasWeights {
			w = NormalizeSkinWeights(m.Weights[i])
		}
		binary.LittleEndian.PutUint32(v[SkinnedWeightsOffset+0:], math.Float32bits(w.X))
		binary.LittleEndian.PutUint32(v[SkinnedWeightsOffset+4:], math.Float32bits(w.Y))
		binary.LittleEndian.PutUint32(v[SkinnedWeightsOffset+8:], math.Float32bits(w.Z))
		binary.LittleEndian.PutUint32(v[SkinnedWeightsOffset+12:], math.Float32bits(w.W))
	}
	return dst
}

// AppendIndexData appends m's indices to dst at [SkinnedMesh.IndexFormat] width,
// little-endian and zero-padded to a 4-byte multiple, allocating only when dst
// lacks capacity.
func (m SkinnedMesh) AppendIndexData(dst []byte) []byte {
	return Mesh{Positions: m.Positions, Indices: m.Indices}.AppendIndexData(dst)
}

// SkinnedVertexLayout returns the vertex buffer layout of every [GPUSkinnedMesh]
// for [gpu.PipelineDesc].VertexBuffers: one per-vertex buffer of
// [SkinnedVertexStride] (64) bytes with position (@location(0)), normal
// (@location(1)), UV (@location(2)), joint indices (@location(3), uint32x4),
// and normalized weights (@location(4), float32x4). Each call returns a fresh
// Attributes slice.
func SkinnedVertexLayout() gpu.VertexBufferLayout {
	return gpu.VertexBufferLayout{
		Stride:   SkinnedVertexStride,
		StepMode: gpu.StepVertex,
		Attributes: []gpu.VertexAttribute{
			{Format: gpu.VertexFloat32x3, Offset: SkinnedPositionOffset, Location: SkinnedPositionLocation},
			{Format: gpu.VertexFloat32x3, Offset: SkinnedNormalOffset, Location: SkinnedNormalLocation},
			{Format: gpu.VertexFloat32x2, Offset: SkinnedUVOffset, Location: SkinnedUVLocation},
			{Format: gpu.VertexUint32x4, Offset: SkinnedJointsOffset, Location: SkinnedJointsLocation},
			{Format: gpu.VertexFloat32x4, Offset: SkinnedWeightsOffset, Location: SkinnedWeightsLocation},
		},
	}
}

// GPUSkinnedMesh is a [SkinnedMesh] uploaded to GPU vertex and optional index
// buffers scoped to the [*core.Scope] passed to [UploadSkinnedMesh].
type GPUSkinnedMesh struct {
	vertices    *gpu.Buffer
	indices     *gpu.Buffer
	indexFormat gpu.IndexFormat
	vertexCount uint32
	indexCount  uint32
	jointCount  int
	bounds      math3d.AABB
}

// UploadSkinnedMesh validates m and uploads its interleaved skinned vertex buffer
// and optional index buffer to dev, owned by cx. Set opts.Retained to keep a CPU
// copy so NSGo recreates the buffers automatically after a device loss.
func UploadSkinnedMesh(cx *core.Scope, dev *gpu.Device, m SkinnedMesh, opts MeshUpload) (*GPUSkinnedMesh, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return uploadSkinnedMesh(m, opts, func(desc gpu.BufferDesc) (*gpu.Buffer, error) {
		return dev.CreateBuffer(cx, desc)
	})
}

func uploadSkinnedMesh(m SkinnedMesh, opts MeshUpload, create func(gpu.BufferDesc) (*gpu.Buffer, error)) (*GPUSkinnedMesh, error) {
	g := &GPUSkinnedMesh{
		vertexCount: uint32(len(m.Positions)),
		indexCount:  uint32(len(m.Indices)),
		indexFormat: m.IndexFormat(),
		jointCount:  m.JointCount,
		bounds:      m.Bounds(),
	}
	label := opts.Label
	if label == "" {
		label = "skinned mesh"
	}
	var err error
	g.vertices, err = create(gpu.BufferDesc{
		Label:    label + " vertices",
		Usage:    gpu.UsageVertex | gpu.UsageCopyDst,
		Data:     m.AppendVertexData(nil),
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	if m.Indexed() {
		g.indices, err = create(gpu.BufferDesc{
			Label:    label + " indices",
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

// VertexBuffer returns the interleaved skinned vertex buffer described by [SkinnedVertexLayout].
func (g *GPUSkinnedMesh) VertexBuffer() *gpu.Buffer { return g.vertices }

// IndexBuffer returns the index buffer, or nil for a non-indexed skinned mesh.
func (g *GPUSkinnedMesh) IndexBuffer() *gpu.Buffer { return g.indices }

// IndexFormat returns the width of the index buffer's entries.
func (g *GPUSkinnedMesh) IndexFormat() gpu.IndexFormat { return g.indexFormat }

// Indexed reports whether the skinned mesh draws through an index buffer.
func (g *GPUSkinnedMesh) Indexed() bool { return g.indices != nil }

// VertexCount returns the number of vertices in the vertex buffer.
func (g *GPUSkinnedMesh) VertexCount() uint32 { return g.vertexCount }

// IndexCount returns the number of indices, or zero for a non-indexed mesh.
func (g *GPUSkinnedMesh) IndexCount() uint32 { return g.indexCount }

// JointCount returns the joint count recorded on the source [SkinnedMesh] (0 if unspecified).
func (g *GPUSkinnedMesh) JointCount() int { return g.jointCount }

// Triangles returns the number of triangles one draw of the mesh rasterizes.
func (g *GPUSkinnedMesh) Triangles() uint32 {
	if g.indices != nil {
		return g.indexCount / 3
	}
	return g.vertexCount / 3
}

// Bounds returns the bind-pose object-space box computed at upload.
func (g *GPUSkinnedMesh) Bounds() math3d.AABB { return g.bounds }

// ByteSize returns the GPU memory held by the mesh's buffers, or 0 for a nil mesh.
func (g *GPUSkinnedMesh) ByteSize() uint64 {
	if g == nil {
		return 0
	}
	n := g.vertices.Size()
	if g.indices != nil {
		n += g.indices.Size()
	}
	return n
}

// Valid reports whether the skinned mesh's buffers are open and belong to a live device.
func (g *GPUSkinnedMesh) Valid() bool {
	if g == nil || !g.vertices.Valid() {
		return false
	}
	return g.indices == nil || g.indices.Valid()
}

// Bind binds the vertex buffer to slot 0 and, for an indexed mesh, the index
// buffer to pass. It binds nothing when the mesh is not [GPUSkinnedMesh.Valid].
func (g *GPUSkinnedMesh) Bind(pass *gpu.RenderPass) {
	if !g.Valid() {
		return
	}
	pass.SetVertexBuffer(0, g.vertices, 0)
	if g.indices != nil {
		pass.SetIndexBuffer(g.indices, g.indexFormat, 0)
	}
}

// Draw binds the skinned mesh to pass and issues one draw call. The pass must
// already have a pipeline whose vertex buffer 0 is [SkinnedVertexLayout] and a
// [GPUSkinPalette] bound at [SkinGroup]. When the mesh is not valid it records nothing.
func (g *GPUSkinnedMesh) Draw(pass *gpu.RenderPass) {
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

// Close releases the mesh's GPU buffers immediately. It is safe to call more than once.
func (g *GPUSkinnedMesh) Close() error {
	if g == nil {
		return nil
	}
	err := g.vertices.Close()
	if g.indices != nil {
		err = errors.Join(err, g.indices.Close())
	}
	return err
}

// SkinVertex evaluates linear blend skinning on the CPU, matching [SkinWGSL]'s
// skin_position and skin_normal helpers. Weights are normalized via
// [NormalizeSkinWeights]; any joint index at or beyond len(matrices) evaluates
// as the identity matrix, matching how [GPUSkinPalette] pads unused slots.
func SkinVertex(pos, normal math3d.Vec3, joints [4]uint16, weights math3d.Vec4, matrices []math3d.Mat4) (skinnedPos, skinnedNormal math3d.Vec3) {
	w := NormalizeSkinWeights(weights)
	ws := [4]float32{w.X, w.Y, w.Z, w.W}
	p4 := pos.Vec4(1)
	n4 := normal.Vec4(0)
	var outP math3d.Vec4
	var outN math3d.Vec3
	for k := range 4 {
		wk := ws[k]
		var m math3d.Mat4
		if idx := int(joints[k]); idx < len(matrices) {
			m = matrices[idx]
		} else {
			m = math3d.Mat4Identity()
		}
		tp := m.MulVec4(p4)
		outP.X += tp.X * wk
		outP.Y += tp.Y * wk
		outP.Z += tp.Z * wk
		tn := m.MulVec4(n4)
		outN.X += tn.X * wk
		outN.Y += tn.Y * wk
		outN.Z += tn.Z * wk
	}
	if outN.LenSq() > 1e-12 {
		outN = outN.Norm()
	}
	return math3d.Vec3{X: outP.X, Y: outP.Y, Z: outP.Z}, outN
}

// SkinJointMatrices computes glTF 2.0 world-space joint skinning matrices for
// doc.Skins[skinIndex] as jointNode.WorldMatrix * InverseBindMatrices[i] (with
// identity when InverseBindMatrices is omitted). Per glTF 2.0 section 3.7.3.2,
// only joint transforms apply to a skinned mesh and the mesh node's own
// transform is ignored. Call [gltf.Document.ComputeWorldMatrices] before calling
// SkinJointMatrices after modifying node transforms or applying an animation.
//
// Results are appended to dst[:0]; when cap(dst) >= len(skin.Joints), this
// function performs zero heap allocations.
func SkinJointMatrices(doc *gltf.Document, skinIndex int, dst []math3d.Mat4) ([]math3d.Mat4, error) {
	return SkinJointMatricesWithTransform(doc, skinIndex, math3d.Mat4Identity(), dst)
}

// SkinJointMatricesWithTransform is like [SkinJointMatrices] but pre-multiplies
// each joint matrix by worldTransform so the skinned character can be placed
// anywhere in the scene. It performs zero heap allocations when cap(dst) >= len(skin.Joints).
func SkinJointMatricesWithTransform(doc *gltf.Document, skinIndex int, worldTransform math3d.Mat4, dst []math3d.Mat4) ([]math3d.Mat4, error) {
	if doc == nil {
		return dst, fmt.Errorf("%w: nil glTF document", ErrInvalidSkin)
	}
	if skinIndex < 0 || skinIndex >= len(doc.Skins) {
		return dst, fmt.Errorf("%w: skin index %d out of range for %d skins", ErrInvalidSkin, skinIndex, len(doc.Skins))
	}
	skin := &doc.Skins[skinIndex]
	n := len(skin.Joints)
	if n == 0 {
		return dst, fmt.Errorf("%w: skin %d has no joints", ErrInvalidSkin, skinIndex)
	}
	if n > MaxSkinJoints {
		return dst, fmt.Errorf("%w: skin %d has %d joints, exceeding MaxSkinJoints (%d)", ErrInvalidSkin, skinIndex, n, MaxSkinJoints)
	}
	for i, nodeIdx := range skin.Joints {
		if nodeIdx < 0 || nodeIdx >= len(doc.Nodes) {
			return dst, fmt.Errorf("%w: skin %d joint %d references out-of-range node %d", ErrInvalidSkin, skinIndex, i, nodeIdx)
		}
	}
	if cap(dst) < n {
		dst = make([]math3d.Mat4, n)
	} else {
		dst = dst[:n]
	}
	ident := math3d.Mat4Identity()
	hasWorld := worldTransform != ident
	for i, nodeIdx := range skin.Joints {
		ibm := ident
		if i < len(skin.InverseBindMatrices) {
			ibm = skin.InverseBindMatrices[i]
		}
		m := doc.Nodes[nodeIdx].WorldMatrix.Mul(ibm)
		if hasWorld {
			m = worldTransform.Mul(m)
		}
		dst[i] = m
	}
	return dst, nil
}

// AppendJointMatrices encodes joints into dst as the fixed [SkinUniformSize]
// (8192-byte) little-endian uniform block declared by [SkinWGSL], padding slots
// [len(joints), MaxSkinJoints) with identity matrices. It returns an error
// wrapping [ErrInvalidSkin] when len(joints) > [MaxSkinJoints] or any matrix
// entry is non-finite. It performs zero heap allocations when cap(dst)-len(dst)
// is at least [SkinUniformSize].
func AppendJointMatrices(dst []byte, joints []math3d.Mat4) ([]byte, error) {
	if len(joints) > MaxSkinJoints {
		return dst, fmt.Errorf("%w: %d joints exceed MaxSkinJoints (%d)", ErrInvalidSkin, len(joints), MaxSkinJoints)
	}
	for i := range joints {
		for k, f := range joints[i] {
			if !finite(f) {
				return dst, fmt.Errorf("%w: joint %d matrix element %d is not finite", ErrInvalidSkin, i, k)
			}
		}
	}
	start := len(dst)
	dst = grow(dst, SkinUniformSize)
	out := dst[start:]
	for i := range joints {
		putMat4(out[i*64:], &joints[i])
	}
	ident := math3d.Mat4Identity()
	for i := len(joints); i < MaxSkinJoints; i++ {
		putMat4(out[i*64:], &ident)
	}
	return dst, nil
}

func putMat4(dst []byte, m *math3d.Mat4) {
	for i, f := range m {
		binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(f))
	}
}

// SkinPaletteOptions configures [NewGPUSkinPalette].
type SkinPaletteOptions struct {
	// Label names the uniform buffer, bind group layout, and bind group in GPU capture tools.
	Label string
	// Retained keeps a CPU shadow copy of the palette uniform buffer so NSGo
	// restores it with the latest joint matrices after a device loss.
	Retained bool
}

// GPUSkinPalette holds up to [MaxSkinJoints] joint matrices in a portable
// uniform buffer ([SkinUniformSize] = 8192 bytes, fitting WebGL 2 and OpenGL ES
// 3.0's 16 KiB uniform block limit) together with the bind group layout and bind
// group exposed at [SkinGroup].
//
// Create it once on device readiness, call [GPUSkinPalette.Update] or
// [GPUSkinPalette.UpdateFromGLTF] when joint poses change, and bind
// [GPUSkinPalette.BindGroup] at [SkinGroup]. Resources belong to the scope
// passed to [NewGPUSkinPalette] and are released on scope disposal or Close.
type GPUSkinPalette struct {
	dev        *gpu.Device
	buf        *gpu.Buffer
	layout     *gpu.BindGroupLayout
	group      *gpu.BindGroup
	joints     int
	scratch    [SkinUniformSize]byte
	matScratch [MaxSkinJoints]math3d.Mat4
}

// NewGPUSkinPalette creates a [GPUSkinPalette] initialized to identity matrices
// for all [MaxSkinJoints] slots on dev, owned by cx.
func NewGPUSkinPalette(cx *core.Scope, dev *gpu.Device, opts SkinPaletteOptions) (*GPUSkinPalette, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.skin"
	}
	p := &GPUSkinPalette{dev: dev}
	if _, err := AppendJointMatrices(p.scratch[:0], nil); err != nil {
		return nil, err
	}
	var err error
	p.buf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label,
		Size:     SkinUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     p.scratch[:],
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	p.layout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label,
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageVertex,
			Type:       gpu.BindingUniform,
		}},
	})
	if err != nil {
		_ = p.buf.Close()
		return nil, err
	}
	p.group, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label,
		Layout: p.layout,
		Entries: []gpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  p.buf,
			Size:    SkinUniformSize,
		}},
	})
	if err != nil {
		_ = p.layout.Close()
		_ = p.buf.Close()
		return nil, err
	}
	return p, nil
}

// Update validates and uploads joints into the palette's uniform buffer, filling
// remaining slots up to [MaxSkinJoints] with identity matrices. If dev is nil,
// the device passed to [NewGPUSkinPalette] is used. It encodes into internal
// storage without heap allocations.
func (p *GPUSkinPalette) Update(dev *gpu.Device, joints []math3d.Mat4) error {
	if !p.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: invalid skin palette"}
	}
	if dev == nil {
		dev = p.dev
	} else {
		p.dev = dev
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if _, err := AppendJointMatrices(p.scratch[:0], joints); err != nil {
		return err
	}
	dev.WriteBuffer(p.buf, 0, p.scratch[:])
	p.joints = len(joints)
	return nil
}

// UpdateFromGLTF computes joint matrices for doc.Skins[skinIndex] into internal
// scratch storage and uploads them with [GPUSkinPalette.Update], with zero
// steady-state heap allocations. Call [gltf.Document.ComputeWorldMatrices] first
// whenever node transforms or animations change.
func (p *GPUSkinPalette) UpdateFromGLTF(dev *gpu.Device, doc *gltf.Document, skinIndex int) error {
	return p.UpdateFromGLTFWithTransform(dev, doc, skinIndex, math3d.Mat4Identity())
}

// UpdateFromGLTFWithTransform computes joint matrices for doc.Skins[skinIndex]
// pre-multiplied by worldTransform and uploads them into the palette.
func (p *GPUSkinPalette) UpdateFromGLTFWithTransform(dev *gpu.Device, doc *gltf.Document, skinIndex int, worldTransform math3d.Mat4) error {
	if !p.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: invalid skin palette"}
	}
	mats, err := SkinJointMatricesWithTransform(doc, skinIndex, worldTransform, p.matScratch[:0])
	if err != nil {
		return err
	}
	return p.Update(dev, mats)
}

// Buffer returns the underlying uniform buffer ([SkinUniformSize] bytes).
func (p *GPUSkinPalette) Buffer() *gpu.Buffer {
	if p == nil {
		return nil
	}
	return p.buf
}

// Layout returns the bind group layout a pipeline lists at [SkinGroup].
func (p *GPUSkinPalette) Layout() *gpu.BindGroupLayout {
	if p == nil {
		return nil
	}
	return p.layout
}

// BindGroup returns the bind group a render pass sets at [SkinGroup].
func (p *GPUSkinPalette) BindGroup() *gpu.BindGroup {
	if p == nil {
		return nil
	}
	return p.group
}

// JointCount returns the number of active joints uploaded by the last Update.
func (p *GPUSkinPalette) JointCount() int {
	if p == nil {
		return 0
	}
	return p.joints
}

// ByteSize returns the GPU memory held by the uniform buffer, or 0 for a nil palette.
func (p *GPUSkinPalette) ByteSize() uint64 {
	if p == nil || p.buf == nil {
		return 0
	}
	return p.buf.Size()
}

// Valid reports whether the palette's GPU resources are open and live.
func (p *GPUSkinPalette) Valid() bool {
	return p != nil && p.buf.Valid() && p.layout.Valid() && p.group.Valid()
}

// Close releases the bind group, layout, and uniform buffer immediately. Safe to call more than once.
func (p *GPUSkinPalette) Close() error {
	if p == nil {
		return nil
	}
	var err error
	if p.group != nil {
		err = errors.Join(err, p.group.Close())
	}
	if p.layout != nil {
		err = errors.Join(err, p.layout.Close())
	}
	if p.buf != nil {
		err = errors.Join(err, p.buf.Close())
	}
	return err
}

// SkinnedMeshPipelineOptions configures [NewSkinnedMeshPipeline].
type SkinnedMeshPipelineOptions struct {
	// Label names the render pipeline in GPU capture tools.
	Label string
	// ColorFormat overrides the color attachment format; when FormatUndefined,
	// dev.SurfaceFormat() is used.
	ColorFormat gpu.TextureFormat
	// DepthFormat enables depth testing and depth writes when not FormatUndefined.
	DepthFormat gpu.TextureFormat
	// DoubleSided disables back-face culling when true; by default CCW front faces
	// are kept and back faces are culled.
	DoubleSided bool
	// SampleCount sets the MSAA sample count (0 defaults to 1).
	SampleCount int
	// Retained keeps the pipeline descriptor so NSGo recreates it after device loss.
	Retained bool
}

// NewSkinnedMeshPipeline compiles [SkinnedMeshWGSL] and creates a render pipeline
// using [SkinnedVertexLayout], camLayout at [CameraGroup] (group 0), and
// skinLayout at [SkinGroup] (group 1).
func NewSkinnedMeshPipeline(cx *core.Scope, dev *gpu.Device, camLayout, skinLayout *gpu.BindGroupLayout, opts SkinnedMeshPipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	if skinLayout == nil || !skinLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid skin bind group layout"}
	}
	mod, err := gpu.CompileWGSL(SkinnedMeshWGSL)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile skinned mesh shader: %w", err)
	}
	colorFormat := opts.ColorFormat
	if colorFormat == gpu.FormatUndefined {
		colorFormat = dev.SurfaceFormat()
	}
	cull := gpu.CullBack
	if opts.DoubleSided {
		cull = gpu.CullNone
	}
	samples := opts.SampleCount
	if samples <= 0 {
		samples = 1
	}
	label := opts.Label
	if label == "" {
		label = "render3d.skinned_mesh"
	}
	layouts := make([]*gpu.BindGroupLayout, max(CameraGroup, SkinGroup)+1)
	layouts[CameraGroup] = camLayout
	layouts[SkinGroup] = skinLayout

	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{SkinnedVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  cull,
		},
		Targets:     []gpu.ColorTargetState{{Format: colorFormat, WriteMask: gpu.ColorWriteAll}},
		SampleCount: samples,
		Retained:    opts.Retained,
	}
	if opts.DepthFormat != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:       opts.DepthFormat,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		}
	}
	return dev.CreatePipeline(cx, desc)
}
