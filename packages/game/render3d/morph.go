package render3d

import (
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// MorphTarget holds per-vertex displacement deltas for a single morph target.
// Positions and Normals are optional: each is either empty or holds one delta
// per base vertex. A [github.com/nsgo-dev/game/packages/game/gltf.MorphTarget]
// maps directly to these fields.
type MorphTarget struct {
	Positions []math3d.Vec3
	Normals   []math3d.Vec3
}

// MorphMesh pairs a base [Mesh] with zero or more [MorphTarget] displacement
// deltas and optional initial Weights, ready to upload with [UploadMorphMesh].
//
// Each target's Positions and Normals must be empty or match len(Base.Positions)
// with finite values. When Weights is non-empty, its length must match
// len(Targets) and every weight must be finite.
type MorphMesh struct {
	Base    Mesh
	Targets []MorphTarget
	Weights []float32
}

// Validate reports whether m can be uploaded: a valid base [Mesh], morph target
// delta slices that are empty or match the base vertex count with finite values,
// and initial weights that are either empty or match the target count with
// finite values. Every error wraps [ErrInvalidMesh].
func (m MorphMesh) Validate() error {
	if err := m.Base.Validate(); err != nil {
		return err
	}
	n := len(m.Base.Positions)
	for ti, target := range m.Targets {
		if len(target.Positions) != 0 && len(target.Positions) != n {
			return fmt.Errorf("%w: morph target %d has %d position deltas for %d vertices", ErrInvalidMesh, ti, len(target.Positions), n)
		}
		if len(target.Normals) != 0 && len(target.Normals) != n {
			return fmt.Errorf("%w: morph target %d has %d normal deltas for %d vertices", ErrInvalidMesh, ti, len(target.Normals), n)
		}
		for i, p := range target.Positions {
			if !finite3(p) {
				return fmt.Errorf("%w: morph target %d position delta %d is not finite", ErrInvalidMesh, ti, i)
			}
		}
		for i, v := range target.Normals {
			if !finite3(v) {
				return fmt.Errorf("%w: morph target %d normal delta %d is not finite", ErrInvalidMesh, ti, i)
			}
		}
	}
	if len(m.Weights) != 0 {
		if len(m.Weights) != len(m.Targets) {
			return fmt.Errorf("%w: %d initial weights for %d morph targets", ErrInvalidMesh, len(m.Weights), len(m.Targets))
		}
		for i, w := range m.Weights {
			if !finite(w) {
				return fmt.Errorf("%w: initial weight %d is not finite", ErrInvalidMesh, i)
			}
		}
	}
	return nil
}

// GPUMorphMesh is a [GPUMesh] whose vertex buffer is updated from a base mesh
// and morph target deltas whenever its weights change.
//
// It writes blended positions and renormalized normals into the same interleaved
// 32-byte [MeshVertexLayout] buffer that [GPUMesh] uses, so any pipeline built
// for [GPUMesh] draws a GPUMorphMesh without shader changes across every NSGo
// GPU backend (including WebGL 2 and OpenGL ES 3.0 without compute shaders).
//
// Call [GPUMorphMesh.SetWeights] when animation or gameplay updates morph
// weights, then [GPUMorphMesh.Update] on the engine goroutine before recording
// draws; Update is a no-op when weights have not changed. Use it from the
// engine goroutine, like the [gpu.Device] it came from.
type GPUMorphMesh struct {
	mesh          *GPUMesh
	basePositions []math3d.Vec3
	baseNormals   []math3d.Vec3
	baseUVs       []math3d.Vec2
	targets       []MorphTarget
	weights       []float32
	blendedPos    []math3d.Vec3
	blendedNorm   []math3d.Vec3
	vertexBytes   []byte
	bounds        math3d.AABB
	cpuDirty      bool
	gpuDirty      bool
}

// UploadMorphMesh validates m, blends its initial weights, and uploads its
// vertex buffer and optional index buffer onto dev, scoped to cx.
//
// A malformed mesh returns an error wrapping [ErrInvalidMesh] before any GPU
// resource is created; a device error is returned as reported by NSGo with no
// buffer left behind. Setting [MeshUpload.Retained] preserves the latest
// uploaded morphed vertex bytes across GPU device loss.
func UploadMorphMesh(cx *core.Scope, dev *gpu.Device, m MorphMesh, opts MeshUpload) (*GPUMorphMesh, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}

	n := len(m.Base.Positions)
	targets := make([]MorphTarget, len(m.Targets))
	for i, t := range m.Targets {
		targets[i] = MorphTarget{
			Positions: slices.Clone(t.Positions),
			Normals:   slices.Clone(t.Normals),
		}
	}
	weights := make([]float32, len(m.Targets))
	copy(weights, m.Weights)

	gm := &GPUMorphMesh{
		basePositions: slices.Clone(m.Base.Positions),
		baseNormals:   slices.Clone(m.Base.Normals),
		baseUVs:       slices.Clone(m.Base.UVs),
		targets:       targets,
		weights:       weights,
		blendedPos:    make([]math3d.Vec3, n),
		blendedNorm:   make([]math3d.Vec3, len(m.Base.Normals)),
		vertexBytes:   make([]byte, 0, n*MeshVertexStride),
		cpuDirty:      true,
	}
	gm.blendCPU()

	initial := Mesh{
		Positions: gm.blendedPos,
		Normals:   gm.blendedNorm,
		UVs:       gm.baseUVs,
		Indices:   m.Base.Indices,
	}
	uploaded, err := uploadMesh(initial, opts, func(desc gpu.BufferDesc) (*gpu.Buffer, error) {
		return dev.CreateBuffer(cx, desc)
	})
	if err != nil {
		return nil, err
	}
	gm.mesh = uploaded
	gm.bounds = uploaded.bounds
	return gm, nil
}

// SetWeights updates the active morph target weights and marks the mesh dirty
// when any weight changes. Passing fewer weights than [GPUMorphMesh.TargetCount]
// (including nil) sets the remaining trailing weights to 0; passing more weights
// than targets, any non-finite weight, or calling on a nil receiver returns an
// error wrapping [ErrInvalidMesh] without modifying the current weights.
func (m *GPUMorphMesh) SetWeights(weights []float32) error {
	if m == nil {
		return fmt.Errorf("%w: nil morph mesh", ErrInvalidMesh)
	}
	if len(weights) > len(m.targets) {
		return fmt.Errorf("%w: %d weights for %d morph targets", ErrInvalidMesh, len(weights), len(m.targets))
	}
	for i, w := range weights {
		if !finite(w) {
			return fmt.Errorf("%w: morph weight %d is not finite", ErrInvalidMesh, i)
		}
	}

	changed := false
	for i := range m.weights {
		var next float32
		if i < len(weights) {
			next = weights[i]
		}
		if next != m.weights[i] {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}

	for i := range m.weights {
		if i < len(weights) {
			m.weights[i] = weights[i]
		} else {
			m.weights[i] = 0
		}
	}
	m.cpuDirty = true
	m.gpuDirty = true
	return nil
}

// TargetCount returns the number of morph targets on m, or 0 for a nil mesh.
func (m *GPUMorphMesh) TargetCount() int {
	if m == nil {
		return 0
	}
	return len(m.targets)
}

// Weights copies m's active morph weights into dst[:0] and returns the slice,
// reusing dst's backing array when it has enough capacity.
func (m *GPUMorphMesh) Weights(dst []float32) []float32 {
	if m == nil {
		return dst[:0]
	}
	return append(dst[:0], m.weights...)
}

// Dirty reports whether m has weight changes that have not yet been uploaded to
// the GPU vertex buffer via [GPUMorphMesh.Update].
func (m *GPUMorphMesh) Dirty() bool {
	return m != nil && m.gpuDirty
}

// Update blends any pending weight changes on the CPU and writes the updated
// interleaved vertex buffer onto dev, returning true when a GPU upload took
// place. It returns false without touching the GPU when weights have not
// changed or when dev or m is invalid; if the device is temporarily lost, m
// remains dirty so the next Update after restore uploads the pending weights.
func (m *GPUMorphMesh) Update(dev *gpu.Device) bool {
	if m == nil || !m.gpuDirty || dev == nil || !m.mesh.Valid() {
		return false
	}
	m.ensureBlended()
	blended := Mesh{
		Positions: m.blendedPos,
		Normals:   m.blendedNorm,
		UVs:       m.baseUVs,
	}
	m.vertexBytes = blended.AppendVertexData(m.vertexBytes[:0])
	dev.WriteBuffer(m.mesh.VertexBuffer(), 0, m.vertexBytes)
	m.gpuDirty = false
	return true
}

// Mesh returns the underlying [GPUMesh], or nil if m is nil.
func (m *GPUMorphMesh) Mesh() *GPUMesh {
	if m == nil {
		return nil
	}
	return m.mesh
}

// VertexBuffer returns the morphed interleaved vertex buffer, laid out as [MeshVertexLayout] describes.
func (m *GPUMorphMesh) VertexBuffer() *gpu.Buffer {
	if m == nil || m.mesh == nil {
		return nil
	}
	return m.mesh.VertexBuffer()
}

// IndexBuffer returns the index buffer, or nil for a non-indexed mesh.
func (m *GPUMorphMesh) IndexBuffer() *gpu.Buffer {
	if m == nil || m.mesh == nil {
		return nil
	}
	return m.mesh.IndexBuffer()
}

// IndexFormat returns the width of the index buffer's entries.
func (m *GPUMorphMesh) IndexFormat() gpu.IndexFormat {
	if m == nil || m.mesh == nil {
		return 0
	}
	return m.mesh.IndexFormat()
}

// Indexed reports whether the mesh draws through an index buffer.
func (m *GPUMorphMesh) Indexed() bool {
	return m != nil && m.mesh != nil && m.mesh.Indexed()
}

// VertexCount returns the number of vertices in the vertex buffer.
func (m *GPUMorphMesh) VertexCount() uint32 {
	if m == nil || m.mesh == nil {
		return 0
	}
	return m.mesh.VertexCount()
}

// IndexCount returns the number of indices, or zero for a non-indexed mesh.
func (m *GPUMorphMesh) IndexCount() uint32 {
	if m == nil || m.mesh == nil {
		return 0
	}
	return m.mesh.IndexCount()
}

// Triangles returns the number of triangles one draw of the mesh rasterizes.
func (m *GPUMorphMesh) Triangles() uint32 {
	if m == nil || m.mesh == nil {
		return 0
	}
	return m.mesh.Triangles()
}

// Bounds returns the object-space bounding box of the mesh for its current
// morph weights.
func (m *GPUMorphMesh) Bounds() math3d.AABB {
	if m == nil {
		return math3d.AABBEmpty()
	}
	m.ensureBlended()
	return m.bounds
}

// ByteSize returns the GPU memory the mesh's buffers hold, for [RenderStats] and [Budget] accounting.
func (m *GPUMorphMesh) ByteSize() uint64 {
	if m == nil {
		return 0
	}
	return m.mesh.ByteSize()
}

// Valid reports whether the mesh's GPU buffers are open and belong to a live device.
func (m *GPUMorphMesh) Valid() bool {
	return m != nil && m.mesh.Valid()
}

// Bind binds the vertex buffer to slot 0 and, for an indexed mesh, the index buffer to pass.
func (m *GPUMorphMesh) Bind(pass *gpu.RenderPass) {
	if m == nil {
		return
	}
	m.mesh.Bind(pass)
}

// Draw binds the mesh to pass and draws all of its triangles once.
func (m *GPUMorphMesh) Draw(pass *gpu.RenderPass) {
	if m == nil {
		return
	}
	m.mesh.Draw(pass)
}

// Close releases the mesh's GPU buffers now instead of when their scope is disposed. It is safe to call more than once.
func (m *GPUMorphMesh) Close() error {
	if m == nil {
		return nil
	}
	return m.mesh.Close()
}

func (m *GPUMorphMesh) ensureBlended() {
	if !m.cpuDirty {
		return
	}
	m.blendCPU()
}

func (m *GPUMorphMesh) blendCPU() {
	copy(m.blendedPos, m.basePositions)
	hasNormals := len(m.baseNormals) > 0
	if hasNormals {
		copy(m.blendedNorm, m.baseNormals)
	}

	morphedNormals := false
	for k, w := range m.weights {
		if w == 0 {
			continue
		}
		target := m.targets[k]
		if len(target.Positions) > 0 {
			for i, d := range target.Positions {
				m.blendedPos[i].X += d.X * w
				m.blendedPos[i].Y += d.Y * w
				m.blendedPos[i].Z += d.Z * w
			}
		}
		if hasNormals && len(target.Normals) > 0 {
			morphedNormals = true
			for i, d := range target.Normals {
				m.blendedNorm[i].X += d.X * w
				m.blendedNorm[i].Y += d.Y * w
				m.blendedNorm[i].Z += d.Z * w
			}
		}
	}

	if morphedNormals {
		for i := range m.blendedNorm {
			m.blendedNorm[i] = m.blendedNorm[i].Norm()
		}
	}

	box := math3d.AABBEmpty()
	for _, p := range m.blendedPos {
		box = box.ExpandPoint(p)
	}
	m.bounds = box
	if m.mesh != nil {
		m.mesh.bounds = box
	}
	m.cpuDirty = false
}
