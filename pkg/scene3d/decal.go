package scene3d

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// DecalReceiverAttachment is the [Node.Attachments] key checked by
// [Node.DecalReceiver] and [ProjectDecal] before falling back to [Node.Collider].
const DecalReceiverAttachment = "decal_receiver"

// Default decal projection parameters applied by [NewDecal] and
// [NewDecalOnSurface].
const (
	// DefaultDecalAngleFadeStart is the angle (in radians) between a surface
	// normal and the decal's opposite projection direction (+Z) where angle fade
	// begins.
	DefaultDecalAngleFadeStart float32 = 60 * (math.Pi / 180)
	// DefaultDecalAngleFadeEnd is the angle (in radians) at and beyond which a
	// surface receives zero decal opacity and is clipped away.
	DefaultDecalAngleFadeEnd float32 = 85 * (math.Pi / 180)
	// DefaultDecalDepthFade is the fraction of box half-depth at both ends of
	// the projection box over which opacity fades smoothly to zero.
	DefaultDecalDepthFade float32 = 0.1
	// DefaultDecalBias is the default world-space offset applied along receiver
	// surface normals to prevent z-fighting on the forward rendering path.
	DefaultDecalBias float32 = 0.002
)

// DecalVertexLayout constants describe the interleaved byte layout written by
// [DecalGeometry.AppendVertexData] and consumed by [DecalWGSL].
const (
	// DecalVertexStride is the byte size of one interleaved decal vertex:
	// position (12 B) + normal (12 B) + UV (8 B) + RGBA color (16 B).
	DecalVertexStride = 48

	DecalPositionOffset = 0
	DecalNormalOffset   = 12
	DecalUVOffset       = 24
	DecalColorOffset    = 32

	DecalPositionLocation = 0
	DecalNormalLocation   = 1
	DecalUVLocation       = 2
	DecalColorLocation    = 3
)

// maxDecalUint16Vertices leaves index 0xFFFF unused because WebGL 2 always
// treats it as a primitive restart sentinel.
const maxDecalUint16Vertices = math.MaxUint16

// Decal describes a box-projected 3D decal in world space (#385).
//
// Its projection volume is an oriented box centered at Transform.Pos, rotated
// by Transform.Rot (identity when zero), with half-extents along local X, Y, Z
// equal to HalfExtents * Transform.Scale (defaulting to (0.5, 0.5, 0.5) when
// HalfExtents is zero and (1, 1, 1) when Transform.Scale is zero).
//
// Following math3d conventions, the decal projects along its local forward
// direction (-Z) onto receiver surfaces whose outward normal faces local +Z.
// Local box coordinates in [-0.5, 0.5]^3 map to texture coordinates
// u = local.x + 0.5 and v = 0.5 - local.y (top-left origin for WebGPU/GLES).
type Decal struct {
	// Transform positions, orients, and optionally scales the projection box.
	Transform math3d.Transform
	// HalfExtents sets the local half-size of the projection box before
	// Transform.Scale. Zero defaults to (0.5, 0.5, 0.5) (a unit 1x1x1 box).
	HalfExtents math3d.Vec3

	// Albedo tints the decal (0xRRGGBBAA). Zero defaults to opaque white
	// (core.RGB(0xffffff)). Its alpha channel multiplies Opacity.
	Albedo core.Color
	// TextureID is an optional caller-defined texture or atlas identifier used
	// to group draw batches and select textures in [GPUDecalRenderer].
	TextureID uint32

	// Opacity scales overall visibility in [0, 1].
	Opacity float32
	// Age is the elapsed lifetime of the decal, advanced by [DecalPool.Step].
	Age time.Duration
	// Lifetime is the total duration before automatic expiration. Zero or
	// negative means the decal does not expire from age (though a full
	// [DecalPool] may still evict it).
	Lifetime time.Duration
	// FadeIn ramps opacity from 0 to 1 over the first FadeIn of Age.
	FadeIn time.Duration
	// FadeOut ramps opacity from 1 to 0 over the final FadeOut of Lifetime.
	FadeOut time.Duration

	// AngleFadeStart and AngleFadeEnd are angles in radians from the opposite
	// projection axis (+Z). Surfaces within AngleFadeStart get full opacity;
	// surfaces at or past AngleFadeEnd (and all back-facing surfaces) fade to 0
	// and are rejected. When both are zero, [DefaultDecalAngleFadeStart] and
	// [DefaultDecalAngleFadeEnd] are used.
	AngleFadeStart float32
	AngleFadeEnd   float32

	// DepthFade in [0, 1] fades opacity smoothly to zero near the front and back
	// ends (|local.z| -> 0.5) of the projection box. Zero disables depth fade.
	DepthFade float32

	// LayerMask selects which [Node.Layer] bitmasks receive this decal. Zero
	// matches no nodes; [LayerAll] matches every non-zero layer.
	LayerMask uint32

	// SortOrder controls both draw ordering (ascending) and pool eviction
	// priority (lowest SortOrder is evicted first when a [DecalPool] is full).
	SortOrder int32

	// Bias offsets projected vertices along receiver surface normals (in world
	// units) to prevent z-fighting on forward rendering paths.
	Bias float32
}

// NewDecal returns a Decal initialized with sensible defaults: identity
// transform, unit 1x1x1 box, opaque white albedo, full opacity, LayerAll,
// 60°–85° normal-angle fade, 0.1 depth fade, and 0.002 normal bias.
func NewDecal() Decal {
	return Decal{
		Transform:      math3d.TransformIdentity(),
		HalfExtents:    math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5},
		Albedo:         core.RGB(0xffffff),
		Opacity:        1,
		AngleFadeStart: DefaultDecalAngleFadeStart,
		AngleFadeEnd:   DefaultDecalAngleFadeEnd,
		DepthFade:      DefaultDecalDepthFade,
		LayerMask:      LayerAll,
		Bias:           DefaultDecalBias,
	}
}

// NewDecalOnSurface creates a Decal centered at pos, projecting along -normal
// onto a surface whose outward normal is normal, with local +Y aligned toward
// up and box half-extents halfExtents.
func NewDecalOnSurface(pos, normal, up, halfExtents math3d.Vec3) Decal {
	d := NewDecal()
	d.Transform.Pos = pos
	if normal.LenSq() > 1e-12 {
		if up.LenSq() < 1e-12 {
			up = math3d.Vec3Up()
		}
		d.Transform.Rot = math3d.LookRotation(normal.Neg(), up)
	}
	if halfExtents != (math3d.Vec3{}) {
		d.HalfExtents = halfExtents
	}
	return d
}

// Forward returns the decal's unit world-space projection direction (local -Z).
func (d Decal) Forward() math3d.Vec3 {
	return d.effectiveRot().RotateVec3(math3d.Vec3Forward()).Norm()
}

// Expired reports whether d has a positive Lifetime and Age >= Lifetime.
func (d Decal) Expired() bool {
	return d.Lifetime > 0 && d.Age >= d.Lifetime
}

// EffectiveOpacity returns d's combined scalar opacity in [0, 1] after applying
// Opacity, Albedo alpha, and age-based FadeIn / FadeOut.
func (d Decal) EffectiveOpacity() float32 {
	_, _, _, a := d.effectiveRGBA()
	return a
}

// AngleFade evaluates d's normal-angle attenuation in [0, 1] for a world-space
// receiver surface normal. Back-facing and perpendicular normals return 0.
func (d Decal) AngleFade(surfaceNormal math3d.Vec3) float32 {
	if surfaceNormal.LenSq() < 1e-12 || !surfaceNormal.IsFinite() {
		return 0
	}
	negForward := d.effectiveRot().RotateVec3(math3d.Vec3UnitZ()).Norm()
	cosStart, cosEnd := d.effectiveAngleCosines()
	return computeAngleFade(surfaceNormal.Norm().Dot(negForward), cosStart, cosEnd)
}

// WorldBounds returns the tight world-space AABB enclosing d's oriented
// projection box, or [math3d.AABBEmpty] if d has non-finite or non-positive
// dimensions.
func (d Decal) WorldBounds() math3d.AABB {
	hx, hy, hz, ok := d.effectiveHalfExtents()
	if !ok || !d.Transform.Pos.IsFinite() {
		return math3d.AABBEmpty()
	}
	rot := d.effectiveRot()
	ax := rot.RotateVec3(math3d.Vec3UnitX())
	ay := rot.RotateVec3(math3d.Vec3UnitY())
	az := rot.RotateVec3(math3d.Vec3UnitZ())
	ext := math3d.Vec3{
		X: abs32(ax.X)*hx + abs32(ay.X)*hy + abs32(az.X)*hz,
		Y: abs32(ax.Y)*hx + abs32(ay.Y)*hy + abs32(az.Y)*hz,
		Z: abs32(ax.Z)*hx + abs32(ay.Z)*hy + abs32(az.Z)*hz,
	}
	if !ext.IsFinite() {
		return math3d.AABBEmpty()
	}
	return math3d.AABB{
		Min: d.Transform.Pos.Sub(ext),
		Max: d.Transform.Pos.Add(ext),
	}
}

func (d Decal) effectiveRot() math3d.Quat {
	q := d.Transform.Rot
	if q.LenSq() < 1e-12 || !finite32(q.X) || !finite32(q.Y) || !finite32(q.Z) || !finite32(q.W) {
		return math3d.QuatIdentity()
	}
	return q.Norm()
}

func (d Decal) effectiveHalfExtents() (hx, hy, hz float32, ok bool) {
	he := d.HalfExtents
	if he == (math3d.Vec3{}) {
		he = math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}
	}
	sc := d.Transform.Scale
	if sc == (math3d.Vec3{}) {
		sc = math3d.Vec3One()
	}
	if !he.IsFinite() || !sc.IsFinite() {
		return 0, 0, 0, false
	}
	hx = he.X * sc.X
	hy = he.Y * sc.Y
	hz = he.Z * sc.Z
	if !(hx > 0) || !(hy > 0) || !(hz > 0) || !finite32(hx) || !finite32(hy) || !finite32(hz) {
		return 0, 0, 0, false
	}
	return hx, hy, hz, true
}

func (d Decal) effectiveAngleCosines() (cosStart, cosEnd float32) {
	startRad := d.AngleFadeStart
	endRad := d.AngleFadeEnd
	if startRad == 0 && endRad == 0 {
		startRad = DefaultDecalAngleFadeStart
		endRad = DefaultDecalAngleFadeEnd
	}
	if !finite32(startRad) {
		startRad = DefaultDecalAngleFadeStart
	}
	if !finite32(endRad) {
		endRad = DefaultDecalAngleFadeEnd
	}
	const halfPi = float32(math.Pi * 0.5)
	startRad = math3d.Clamp(startRad, 0, halfPi)
	if endRad < startRad {
		endRad = startRad
	}
	endRad = math3d.Clamp(endRad, 0, halfPi)
	cosStart = float32(math.Cos(float64(startRad)))
	cosEnd = float32(math.Cos(float64(endRad)))
	if cosEnd < 1e-5 {
		cosEnd = 1e-5
	}
	if cosStart < cosEnd {
		cosStart = cosEnd
	}
	return cosStart, cosEnd
}

func (d Decal) effectiveRGBA() (r, g, b, a float32) {
	if d.Expired() || !finite32(d.Opacity) || !(d.Opacity > 0) {
		return 0, 0, 0, 0
	}
	col := d.Albedo
	if col == 0 {
		col = core.RGB(0xffffff)
	}
	if col == core.Transparent {
		return 0, 0, 0, 0
	}
	r = float32((col>>24)&0xff) / 255.0
	g = float32((col>>16)&0xff) / 255.0
	b = float32((col>>8)&0xff) / 255.0
	albedoA := float32(col&0xff) / 255.0

	ageFade := float32(1)
	if d.FadeIn > 0 {
		if d.Age <= 0 {
			ageFade = 0
		} else if d.Age < d.FadeIn {
			ageFade *= float32(d.Age) / float32(d.FadeIn)
		}
	}
	if d.FadeOut > 0 && d.Lifetime > 0 {
		rem := d.Lifetime - d.Age
		if rem <= 0 {
			ageFade = 0
		} else if rem < d.FadeOut {
			ageFade *= float32(rem) / float32(d.FadeOut)
		}
	}
	a = clamp01(d.Opacity) * albedoA * clamp01(ageFade)
	return r, g, b, a
}

// DecalReceiver supplies local-space triangles for box-projected decals.
// [*MeshCollider] and [*DecalReceiverMesh] implement DecalReceiver.
type DecalReceiver interface {
	// DecalTriangles invokes emit for each local-space triangle (p0, p1, p2)
	// and its local-space vertex normals (n0, n1, n2). Passing zero normals
	// tells the projector to derive the flat geometric face normal from
	// (p1 - p0) x (p2 - p0). Returning false stops iteration early.
	DecalTriangles(emit func(p0, p1, p2, n0, n1, n2 math3d.Vec3) bool)
}

// DecalTriangles implements [DecalReceiver] for MeshCollider, emitting every
// complete triangle in m.Triangles with flat geometric face normals.
func (m *MeshCollider) DecalTriangles(emit func(p0, p1, p2, n0, n1, n2 math3d.Vec3) bool) {
	if m == nil || emit == nil {
		return
	}
	n := len(m.Triangles) - len(m.Triangles)%3
	var zero math3d.Vec3
	for i := 0; i < n; i += 3 {
		if !emit(m.Triangles[i], m.Triangles[i+1], m.Triangles[i+2], zero, zero, zero) {
			return
		}
	}
}

// DecalReceiverMesh is indexed or non-indexed local-space triangle geometry
// that implements [DecalReceiver]. Positions is required; Normals and Indices
// are optional.
type DecalReceiverMesh struct {
	Positions []math3d.Vec3
	Normals   []math3d.Vec3
	Indices   []uint32
}

// LocalBounds returns the tight local-space AABB enclosing m.Positions.
func (m *DecalReceiverMesh) LocalBounds() math3d.AABB {
	if m == nil || len(m.Positions) == 0 {
		return math3d.AABBEmpty()
	}
	b := math3d.AABBEmpty()
	for _, p := range m.Positions {
		if p.IsFinite() {
			b = b.ExpandPoint(p)
		}
	}
	return b
}

// DecalTriangles implements [DecalReceiver] for DecalReceiverMesh.
func (m *DecalReceiverMesh) DecalTriangles(emit func(p0, p1, p2, n0, n1, n2 math3d.Vec3) bool) {
	if m == nil || emit == nil || len(m.Positions) == 0 {
		return
	}
	hasNormals := len(m.Normals) == len(m.Positions)
	var zero math3d.Vec3
	if len(m.Indices) > 0 {
		n := len(m.Indices) - len(m.Indices)%3
		vCount := uint32(len(m.Positions))
		for i := 0; i < n; i += 3 {
			i0, i1, i2 := m.Indices[i], m.Indices[i+1], m.Indices[i+2]
			if i0 >= vCount || i1 >= vCount || i2 >= vCount {
				continue
			}
			n0, n1, n2 := zero, zero, zero
			if hasNormals {
				n0, n1, n2 = m.Normals[i0], m.Normals[i1], m.Normals[i2]
			}
			if !emit(m.Positions[i0], m.Positions[i1], m.Positions[i2], n0, n1, n2) {
				return
			}
		}
		return
	}
	n := len(m.Positions) - len(m.Positions)%3
	for i := 0; i < n; i += 3 {
		n0, n1, n2 := zero, zero, zero
		if hasNormals {
			n0, n1, n2 = m.Normals[i], m.Normals[i+1], m.Normals[i+2]
		}
		if !emit(m.Positions[i], m.Positions[i+1], m.Positions[i+2], n0, n1, n2) {
			return
		}
	}
}

// SetDecalReceiver attaches r to n under [DecalReceiverAttachment]. If n has
// empty LocalBounds and r implements LocalBounds() math3d.AABB, n's LocalBounds
// is populated so the scene BVH indexes n automatically. Passing nil removes
// the attachment.
func (n *Node) SetDecalReceiver(r DecalReceiver) {
	if n == nil {
		return
	}
	if r == nil {
		n.RemoveAttachment(DecalReceiverAttachment)
		return
	}
	n.SetAttachment(DecalReceiverAttachment, r)
	if n.LocalBounds.Empty() {
		if bounded, ok := r.(interface{ LocalBounds() math3d.AABB }); ok {
			if b := bounded.LocalBounds(); !b.Empty() {
				n.SetLocalBounds(b)
			}
		}
	}
}

// DecalReceiver returns n's [DecalReceiver] attachment if present, or
// n.Collider if it implements [DecalReceiver], or nil otherwise.
func (n *Node) DecalReceiver() DecalReceiver {
	if n == nil {
		return nil
	}
	if r, ok := AttachmentAs[DecalReceiver](n, DecalReceiverAttachment); ok && r != nil {
		return r
	}
	if r, ok := n.Collider.(DecalReceiver); ok && r != nil {
		return r
	}
	return nil
}

// DecalVertex is one clipped, biased vertex produced by decal projection.
type DecalVertex struct {
	// Pos is the world-space position offset along Normal by Decal.Bias.
	Pos math3d.Vec3
	// Normal is the unit world-space surface normal.
	Normal math3d.Vec3
	// UV is the projected texture coordinate in [0, 1] with top-left origin.
	UV math3d.Vec2
	// Color holds the linear RGBA tint (R, G, B in [0, 1]) with opacity,
	// age fade, normal-angle fade, and depth fade folded into W (alpha).
	Color math3d.Vec4
}

// DecalBatch describes a contiguous range of indices in a [DecalGeometry]
// produced by one decal.
type DecalBatch struct {
	DecalID    uint64
	TextureID  uint32
	SortOrder  int32
	FirstIndex uint32
	IndexCount uint32
}

// DecalGeometry holds forward-path clipped decal triangles ready for CPU
// inspection or upload through [GPUDecalRenderer]. Call Reset to reuse backing
// storage across frames without allocating.
type DecalGeometry struct {
	Vertices []DecalVertex
	Indices  []uint32
	Batches  []DecalBatch
}

// Reset clears g's slices while preserving their allocated capacity.
func (g *DecalGeometry) Reset() {
	if g == nil {
		return
	}
	g.Vertices = g.Vertices[:0]
	g.Indices = g.Indices[:0]
	g.Batches = g.Batches[:0]
}

// VertexCount returns the number of projected vertices in g.
func (g *DecalGeometry) VertexCount() int {
	if g == nil {
		return 0
	}
	return len(g.Vertices)
}

// IndexCount returns the number of triangle indices in g.
func (g *DecalGeometry) IndexCount() int {
	if g == nil {
		return 0
	}
	return len(g.Indices)
}

// TriangleCount returns the number of projected triangles in g.
func (g *DecalGeometry) TriangleCount() int {
	if g == nil {
		return 0
	}
	return len(g.Indices) / 3
}

// Bounds returns the world-space AABB enclosing all vertices in g, or
// [math3d.AABBEmpty] when g is empty.
func (g *DecalGeometry) Bounds() math3d.AABB {
	if g == nil || len(g.Vertices) == 0 {
		return math3d.AABBEmpty()
	}
	b := math3d.AABBEmpty()
	for i := range g.Vertices {
		b = b.ExpandPoint(g.Vertices[i].Pos)
	}
	return b
}

// IndexFormat returns [gpu.IndexUint16] when every vertex fits below 0xFFFF
// (leaving WebGL 2's primitive restart sentinel free), otherwise
// [gpu.IndexUint32].
func (g *DecalGeometry) IndexFormat() gpu.IndexFormat {
	if g == nil || len(g.Vertices) <= maxDecalUint16Vertices {
		return gpu.IndexUint16
	}
	return gpu.IndexUint32
}

// VertexDataSize returns the byte length [DecalGeometry.AppendVertexData] writes.
func (g *DecalGeometry) VertexDataSize() int {
	if g == nil {
		return 0
	}
	return len(g.Vertices) * DecalVertexStride
}

// IndexDataSize returns the byte length [DecalGeometry.AppendIndexData] writes,
// padded to a 4-byte multiple as WebGPU requires.
func (g *DecalGeometry) IndexDataSize() int {
	if g == nil {
		return 0
	}
	n := len(g.Indices)
	if g.IndexFormat() == gpu.IndexUint32 {
		return n * 4
	}
	return (n*2 + 3) &^ 3
}

// AppendVertexData appends g's interleaved vertices to dst in the little-endian
// layout of [DecalVertexLayout]. It allocates only when dst lacks capacity.
func (g *DecalGeometry) AppendVertexData(dst []byte) []byte {
	if g == nil || len(g.Vertices) == 0 {
		return dst
	}
	need := g.VertexDataSize()
	start := len(dst)
	dst = growBytes(dst, need)
	out := dst[start:]
	for i := range g.Vertices {
		v := &g.Vertices[i]
		slot := out[i*DecalVertexStride : (i+1)*DecalVertexStride]
		putF32(slot[0:], v.Pos.X)
		putF32(slot[4:], v.Pos.Y)
		putF32(slot[8:], v.Pos.Z)
		putF32(slot[12:], v.Normal.X)
		putF32(slot[16:], v.Normal.Y)
		putF32(slot[20:], v.Normal.Z)
		putF32(slot[24:], v.UV.X)
		putF32(slot[28:], v.UV.Y)
		putF32(slot[32:], v.Color.X)
		putF32(slot[36:], v.Color.Y)
		putF32(slot[40:], v.Color.Z)
		putF32(slot[44:], v.Color.W)
	}
	return dst
}

// AppendIndexData appends g's indices to dst at [DecalGeometry.IndexFormat]
// width, little-endian and zero-padded to a 4-byte multiple.
func (g *DecalGeometry) AppendIndexData(dst []byte) []byte {
	if g == nil || len(g.Indices) == 0 {
		return dst
	}
	need := g.IndexDataSize()
	start := len(dst)
	dst = growBytes(dst, need)
	out := dst[start:]
	if g.IndexFormat() == gpu.IndexUint32 {
		for i, idx := range g.Indices {
			binary.LittleEndian.PutUint32(out[i*4:], idx)
		}
		return dst
	}
	for i, idx := range g.Indices {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(idx))
	}
	clear(out[len(g.Indices)*2:])
	return dst
}

// ProjectDecal clips and projects d onto all visible, enabled nodes in scene
// whose world bounds overlap d's projection box and whose [Node.Layer] matches
// d.LayerMask. Projected triangles are appended to dst; the number of triangles
// added is returned.
func ProjectDecal(scene *Scene, d Decal, dst *DecalGeometry) int {
	if scene == nil || dst == nil || d.LayerMask == 0 {
		return 0
	}
	var scratch []BVHHandle
	tris, _ := projectDecalInScene(scene, d, 0, dst, scratch)
	return tris
}

// ProjectDecalOntoReceiver clips and projects d onto a single [DecalReceiver]
// transformed by worldMat, appending triangles to dst and returning the number
// of triangles added.
func ProjectDecalOntoReceiver(d Decal, receiver DecalReceiver, worldMat math3d.Mat4, dst *DecalGeometry) int {
	if receiver == nil || dst == nil {
		return 0
	}
	proj, ok := prepareDecalProjection(d)
	if !ok {
		return 0
	}
	firstIdx := uint32(len(dst.Indices))
	proj.projectReceiver(receiver, worldMat, dst)
	added := uint32(len(dst.Indices)) - firstIdx
	if added > 0 {
		dst.Batches = append(dst.Batches, DecalBatch{
			TextureID:  d.TextureID,
			SortOrder:  d.SortOrder,
			FirstIndex: firstIdx,
			IndexCount: added,
		})
	}
	return int(added / 3)
}

func projectDecalInScene(scene *Scene, d Decal, decalID uint64, dst *DecalGeometry, scratch []BVHHandle) (int, []BVHHandle) {
	if scene == nil || dst == nil || d.LayerMask == 0 {
		return 0, scratch
	}
	root := scene.Root()
	if !root.Visible || !root.Enabled {
		return 0, scratch
	}
	proj, ok := prepareDecalProjection(d)
	if !ok {
		return 0, scratch
	}
	scene.SyncBVH()
	bvh := scene.ensureBVH()
	scene.beginVisibilityQuery()
	handles := bvh.QueryAABB(proj.worldBounds, scratch[:0])
	if len(handles) == 0 {
		return 0, handles
	}
	firstIdx := uint32(len(dst.Indices))
	for _, h := range handles {
		n, ok := bvh.leaves[h.index-1].userData.(*Node)
		if !ok || n.Layer&d.LayerMask == 0 || !scene.activeVisible(n) {
			continue
		}
		recv := n.DecalReceiver()
		if recv == nil {
			continue
		}
		proj.projectReceiver(recv, n.WorldMatrix(), dst)
	}
	added := uint32(len(dst.Indices)) - firstIdx
	if added > 0 {
		dst.Batches = append(dst.Batches, DecalBatch{
			DecalID:    decalID,
			TextureID:  d.TextureID,
			SortOrder:  d.SortOrder,
			FirstIndex: firstIdx,
			IndexCount: added,
		})
	}
	return int(added / 3), handles
}

// decalProjection holds precomputed projection box parameters for one Decal.
type decalProjection struct {
	center      math3d.Vec3
	invRot      math3d.Quat
	invSize     math3d.Vec3 // 1 / (2 * halfExtents)
	negForward  math3d.Vec3 // unit local +Z in world space
	worldBounds math3d.AABB
	cosStart    float32
	cosEnd      float32
	depthFade   float32
	bias        float32
	color       math3d.Vec4
}

func prepareDecalProjection(d Decal) (decalProjection, bool) {
	r, g, b, a := d.effectiveRGBA()
	if !(a > 0) {
		return decalProjection{}, false
	}
	hx, hy, hz, ok := d.effectiveHalfExtents()
	if !ok || !d.Transform.Pos.IsFinite() {
		return decalProjection{}, false
	}
	rot := d.effectiveRot()
	wb := d.WorldBounds()
	if wb.Empty() {
		return decalProjection{}, false
	}
	cosStart, cosEnd := d.effectiveAngleCosines()
	bias := d.Bias
	if !finite32(bias) || bias < 0 {
		bias = 0
	}
	df := d.DepthFade
	if !finite32(df) {
		df = 0
	}
	df = clamp01(df)
	return decalProjection{
		center:      d.Transform.Pos,
		invRot:      rot.Conj(),
		invSize:     math3d.Vec3{X: 1 / (2 * hx), Y: 1 / (2 * hy), Z: 1 / (2 * hz)},
		negForward:  rot.RotateVec3(math3d.Vec3UnitZ()).Norm(),
		worldBounds: wb,
		cosStart:    cosStart,
		cosEnd:      cosEnd,
		depthFade:   df,
		bias:        bias,
		color:       math3d.Vec4{X: r, Y: g, Z: b, W: a},
	}, true
}

func (p *decalProjection) worldToLocal(w math3d.Vec3) math3d.Vec3 {
	u := p.invRot.RotateVec3(w.Sub(p.center))
	return math3d.Vec3{
		X: u.X * p.invSize.X,
		Y: u.Y * p.invSize.Y,
		Z: u.Z * p.invSize.Z,
	}
}

type decalClipVert struct {
	worldPos    math3d.Vec3
	worldNormal math3d.Vec3
	localPos    math3d.Vec3
}

func (p *decalProjection) projectReceiver(recv DecalReceiver, worldMat math3d.Mat4, dst *DecalGeometry) {
	normMat, hasNormMat := math3d.NormalMatrix(worldMat)
	mirrored := math3d.Mat3FromMat4(worldMat).Det() < 0
	var zero math3d.Vec3

	switch r := recv.(type) {
	case *MeshCollider:
		if r == nil {
			return
		}
		n := len(r.Triangles) - len(r.Triangles)%3
		for i := 0; i < n; i += 3 {
			p.projectTriangle(r.Triangles[i], r.Triangles[i+1], r.Triangles[i+2], zero, zero, zero, worldMat, normMat, hasNormMat, mirrored, dst)
		}
	case *DecalReceiverMesh:
		if r == nil || len(r.Positions) == 0 {
			return
		}
		hasNormals := len(r.Normals) == len(r.Positions)
		if len(r.Indices) > 0 {
			n := len(r.Indices) - len(r.Indices)%3
			vCount := uint32(len(r.Positions))
			for i := 0; i < n; i += 3 {
				i0, i1, i2 := r.Indices[i], r.Indices[i+1], r.Indices[i+2]
				if i0 >= vCount || i1 >= vCount || i2 >= vCount {
					continue
				}
				n0, n1, n2 := zero, zero, zero
				if hasNormals {
					n0, n1, n2 = r.Normals[i0], r.Normals[i1], r.Normals[i2]
				}
				p.projectTriangle(r.Positions[i0], r.Positions[i1], r.Positions[i2], n0, n1, n2, worldMat, normMat, hasNormMat, mirrored, dst)
			}
			return
		}
		n := len(r.Positions) - len(r.Positions)%3
		for i := 0; i < n; i += 3 {
			n0, n1, n2 := zero, zero, zero
			if hasNormals {
				n0, n1, n2 = r.Normals[i], r.Normals[i+1], r.Normals[i+2]
			}
			p.projectTriangle(r.Positions[i], r.Positions[i+1], r.Positions[i+2], n0, n1, n2, worldMat, normMat, hasNormMat, mirrored, dst)
		}
	default:
		projectCustomReceiver(*p, recv, worldMat, normMat, hasNormMat, mirrored, dst)
	}
}

func projectCustomReceiver(p decalProjection, recv DecalReceiver, worldMat math3d.Mat4, normMat math3d.Mat3, hasNormMat, mirrored bool, dst *DecalGeometry) {
	recv.DecalTriangles(func(p0, p1, p2, n0, n1, n2 math3d.Vec3) bool {
		p.projectTriangle(p0, p1, p2, n0, n1, n2, worldMat, normMat, hasNormMat, mirrored, dst)
		return true
	})
}

func transformDecalNormal(n, fallback math3d.Vec3, worldMat math3d.Mat4, normMat math3d.Mat3, hasNormMat bool) math3d.Vec3 {
	if n.LenSq() < 1e-12 || !n.IsFinite() {
		return fallback
	}
	var wn math3d.Vec3
	if hasNormMat {
		wn = normMat.MulVec3(n)
	} else {
		wn = worldMat.TransformDir(n)
	}
	if wn.LenSq() < 1e-12 || !wn.IsFinite() {
		return fallback
	}
	return wn.Norm()
}

func (p *decalProjection) projectTriangle(p0, p1, p2, n0, n1, n2 math3d.Vec3, worldMat math3d.Mat4, normMat math3d.Mat3, hasNormMat, mirrored bool, dst *DecalGeometry) {
	if !p0.IsFinite() || !p1.IsFinite() || !p2.IsFinite() {
		return
	}
	w0 := worldMat.TransformPoint(p0)
	w1 := worldMat.TransformPoint(p1)
	w2 := worldMat.TransformPoint(p2)
	if mirrored {
		w1, w2 = w2, w1
		n1, n2 = n2, n1
	}
	faceCross := w1.Sub(w0).Cross(w2.Sub(w0))
	if faceCross.LenSq() < 1e-12 || !faceCross.IsFinite() {
		return
	}
	faceN := faceCross.Norm()
	faceCos := faceN.Dot(p.negForward)
	// Reject triangles whose geometric face is perpendicular or back-facing
	// relative to the decal projection direction so decals never bleed onto
	// rear walls or back faces.
	if faceCos <= 1e-5 {
		return
	}

	wn0 := transformDecalNormal(n0, faceN, worldMat, normMat, hasNormMat)
	wn1 := transformDecalNormal(n1, faceN, worldMat, normMat, hasNormMat)
	wn2 := transformDecalNormal(n2, faceN, worldMat, normMat, hasNormMat)
	c0 := wn0.Dot(p.negForward)
	c1 := wn1.Dot(p.negForward)
	c2 := wn2.Dot(p.negForward)
	if c0 <= p.cosEnd && c1 <= p.cosEnd && c2 <= p.cosEnd {
		return
	}

	l0 := p.worldToLocal(w0)
	l1 := p.worldToLocal(w1)
	l2 := p.worldToLocal(w2)

	code0 := boxOutcode(l0)
	code1 := boxOutcode(l1)
	code2 := boxOutcode(l2)
	if (code0 & code1 & code2) != 0 {
		return
	}

	var bufA, bufB [12]decalClipVert
	bufA[0] = decalClipVert{worldPos: w0, worldNormal: wn0, localPos: l0}
	bufA[1] = decalClipVert{worldPos: w1, worldNormal: wn1, localPos: l1}
	bufA[2] = decalClipVert{worldPos: w2, worldNormal: wn2, localPos: l2}
	poly := bufA[:3]

	if (code0 | code1 | code2) != 0 {
		poly = clipPolygonToUnitBox(poly, bufA[:0], bufB[:0], faceN)
		if len(poly) < 3 {
			return
		}
	}

	// Emit vertices; skip polygon if every clipped vertex faded to 0.
	baseVert := uint32(len(dst.Vertices))
	anyVisible := false
	for i := range poly {
		v := &poly[i]
		n := v.worldNormal
		if n.LenSq() < 1e-12 {
			n = faceN
		} else {
			n = n.Norm()
		}
		aFade := computeAngleFade(n.Dot(p.negForward), p.cosStart, p.cosEnd)
		dFade := computeDepthFade(v.localPos.Z, p.depthFade)
		alpha := p.color.W * aFade * dFade
		if alpha > 0 {
			anyVisible = true
		}
		dst.Vertices = append(dst.Vertices, DecalVertex{
			Pos:    v.worldPos.Add(n.Scale(p.bias)),
			Normal: n,
			UV: math3d.Vec2{
				X: clamp01(v.localPos.X + 0.5),
				Y: clamp01(0.5 - v.localPos.Y),
			},
			Color: math3d.Vec4{
				X: p.color.X,
				Y: p.color.Y,
				Z: p.color.Z,
				W: alpha,
			},
		})
	}
	if !anyVisible {
		dst.Vertices = dst.Vertices[:baseVert]
		return
	}
	nVerts := uint32(len(poly))
	for i := uint32(1); i+1 < nVerts; i++ {
		dst.Indices = append(dst.Indices, baseVert, baseVert+i, baseVert+i+1)
	}
}

func boxOutcode(p math3d.Vec3) uint8 {
	var c uint8
	if p.X < -0.5 {
		c |= 1 << 0
	} else if p.X > 0.5 {
		c |= 1 << 1
	}
	if p.Y < -0.5 {
		c |= 1 << 2
	} else if p.Y > 0.5 {
		c |= 1 << 3
	}
	if p.Z < -0.5 {
		c |= 1 << 4
	} else if p.Z > 0.5 {
		c |= 1 << 5
	}
	return c
}

// clipPolygonToUnitBox runs 6-plane Sutherland-Hodgman clipping against
// [-0.5, +0.5]^3 using the caller's stack ping-pong buffers without allocating.
func clipPolygonToUnitBox(in, bufA, bufB []decalClipVert, faceN math3d.Vec3) []decalClipVert {
	src := in
	dst := bufB
	for plane := range 6 {
		dst = dst[:0]
		n := len(src)
		if n < 3 {
			return nil
		}
		prev := src[n-1]
		dPrev := boxPlaneDist(prev.localPos, plane)
		prevIn := dPrev >= 0
		for i := range n {
			curr := src[i]
			dCurr := boxPlaneDist(curr.localPos, plane)
			currIn := dCurr >= 0
			if currIn {
				if !prevIn {
					dst = append(dst, lerpClipVert(prev, curr, dPrev, dCurr, faceN))
				}
				dst = append(dst, curr)
			} else if prevIn {
				dst = append(dst, lerpClipVert(prev, curr, dPrev, dCurr, faceN))
			}
			prev = curr
			dPrev = dCurr
			prevIn = currIn
		}
		if len(dst) < 3 {
			return nil
		}
		// Swap ping-pong buffers: odd planes write into bufA, even into bufB.
		if plane%2 == 0 {
			src = dst
			dst = bufA
		} else {
			src = dst
			dst = bufB
		}
	}
	return src
}

func boxPlaneDist(p math3d.Vec3, plane int) float32 {
	switch plane {
	case 0:
		return p.X + 0.5
	case 1:
		return 0.5 - p.X
	case 2:
		return p.Y + 0.5
	case 3:
		return 0.5 - p.Y
	case 4:
		return p.Z + 0.5
	default:
		return 0.5 - p.Z
	}
}

func lerpClipVert(a, b decalClipVert, dA, dB float32, faceN math3d.Vec3) decalClipVert {
	denom := dA - dB
	var t float32
	if abs32(denom) > 1e-12 {
		t = clamp01(dA / denom)
	}
	n := a.worldNormal.Lerp(b.worldNormal, t)
	if n.LenSq() < 1e-12 {
		n = faceN
	} else {
		n = n.Norm()
	}
	return decalClipVert{
		worldPos:    a.worldPos.Lerp(b.worldPos, t),
		worldNormal: n,
		localPos:    a.localPos.Lerp(b.localPos, t),
	}
}

func computeAngleFade(cosTheta, cosStart, cosEnd float32) float32 {
	if cosTheta <= cosEnd || cosTheta <= 1e-5 {
		return 0
	}
	if cosTheta >= cosStart || cosStart <= cosEnd {
		return 1
	}
	t := clamp01((cosTheta - cosEnd) / (cosStart - cosEnd))
	return t * t * (3 - 2*t)
}

func computeDepthFade(localZ, depthFade float32) float32 {
	if depthFade <= 0 {
		return 1
	}
	edge := 1 - 2*abs32(localZ)
	if edge <= 0 {
		return 0
	}
	if edge >= depthFade {
		return 1
	}
	t := clamp01(edge / depthFade)
	return t * t * (3 - 2*t)
}

// DecalHandle uniquely identifies a live decal stored in a [DecalPool].
// The zero value is invalid.
type DecalHandle struct {
	index uint32 // 1-based slot index; 0 means invalid
	gen   uint32 // generation counter to reject stale handles
}

// Valid reports whether h is a non-zero handle.
func (h DecalHandle) Valid() bool {
	return h.index != 0
}

type decalSlot struct {
	decal Decal
	seq   uint64
	gen   uint32
	alive bool
}

// DecalPool is a fixed-capacity pool of live 3D decals (#385).
//
// When the pool reaches capacity, [DecalPool.Spawn] deterministically evicts
// the live decal with the lowest [Decal.SortOrder], breaking ties by oldest
// spawn order (FIFO ring behavior when priorities are equal). [DecalPool.Step]
// ages live decals and expires those whose Age >= Lifetime. All steady-state
// operations (Spawn, Remove, Step, BuildGeometry) perform zero heap
// allocations.
type DecalPool struct {
	slots        []decalSlot
	freeStack    []int
	activeOrder  []int
	queryScratch []BVHHandle
	liveCount    int
	nextSeq      uint64
}

// NewDecalPool returns a fixed-capacity DecalPool holding at most maxDecals
// live decals. A non-positive maxDecals produces a zero-capacity pool.
func NewDecalPool(maxDecals int) *DecalPool {
	if maxDecals <= 0 {
		return &DecalPool{}
	}
	slots := make([]decalSlot, maxDecals)
	freeStack := make([]int, maxDecals)
	for i := range maxDecals {
		slots[i].gen = 1
		freeStack[i] = maxDecals - 1 - i
	}
	return &DecalPool{
		slots:       slots,
		freeStack:   freeStack,
		activeOrder: make([]int, 0, maxDecals),
	}
}

// Cap returns the maximum number of live decals p can hold.
func (p *DecalPool) Cap() int {
	if p == nil {
		return 0
	}
	return len(p.slots)
}

// Len returns the number of live decals currently in p.
func (p *DecalPool) Len() int {
	if p == nil {
		return 0
	}
	return p.liveCount
}

func (p *DecalPool) lookup(h DecalHandle) (*decalSlot, int) {
	if p == nil || h.index == 0 {
		return nil, -1
	}
	idx := int(h.index - 1)
	if idx >= len(p.slots) {
		return nil, -1
	}
	s := &p.slots[idx]
	if !s.alive || s.gen != h.gen {
		return nil, -1
	}
	return s, idx
}

// Contains reports whether h refers to a currently live decal in p.
func (p *DecalPool) Contains(h DecalHandle) bool {
	s, _ := p.lookup(h)
	return s != nil
}

// Get returns a pointer to the live decal identified by h, or (nil, false) if
// h is invalid, expired, removed, or evicted.
func (p *DecalPool) Get(h DecalHandle) (*Decal, bool) {
	s, _ := p.lookup(h)
	if s == nil {
		return nil, false
	}
	return &s.decal, true
}

// Spawn inserts d into p and returns its handle. If p is at capacity, the live
// decal with the lowest SortOrder (and oldest spawn sequence among ties) is
// evicted deterministically. If p has zero capacity or d is already expired,
// Spawn returns an invalid zero DecalHandle.
func (p *DecalPool) Spawn(d Decal) DecalHandle {
	if p == nil || len(p.slots) == 0 || d.Expired() {
		return DecalHandle{}
	}
	var idx int
	if n := len(p.freeStack); n > 0 {
		idx = p.freeStack[n-1]
		p.freeStack = p.freeStack[:n-1]
		p.liveCount++
	} else {
		// Pool is full: evict lowest SortOrder, tie-breaking by oldest seq.
		victim := 0
		for i := 1; i < len(p.slots); i++ {
			a := &p.slots[i]
			b := &p.slots[victim]
			if a.decal.SortOrder < b.decal.SortOrder ||
				(a.decal.SortOrder == b.decal.SortOrder && a.seq < b.seq) {
				victim = i
			}
		}
		idx = victim
		p.bumpGen(&p.slots[idx])
	}
	p.nextSeq++
	slot := &p.slots[idx]
	slot.decal = d
	slot.seq = p.nextSeq
	slot.alive = true
	return DecalHandle{index: uint32(idx + 1), gen: slot.gen}
}

// Remove deletes the live decal identified by h and returns true if it was
// present.
func (p *DecalPool) Remove(h DecalHandle) bool {
	s, idx := p.lookup(h)
	if s == nil {
		return false
	}
	p.freeSlot(idx)
	return true
}

// Clear removes every live decal from p and invalidates all outstanding
// handles.
func (p *DecalPool) Clear() {
	if p == nil || len(p.slots) == 0 {
		return
	}
	p.freeStack = p.freeStack[:0]
	for i := len(p.slots) - 1; i >= 0; i-- {
		s := &p.slots[i]
		if s.alive {
			s.alive = false
			s.decal = Decal{}
			p.bumpGen(s)
		}
		p.freeStack = append(p.freeStack, i)
	}
	p.liveCount = 0
}

// Step advances Age on every live decal by dt and removes decals whose
// Lifetime > 0 and Age >= Lifetime. It returns the number of expired decals.
// Non-positive dt is ignored.
func (p *DecalPool) Step(dt time.Duration) int {
	if p == nil || dt <= 0 || p.liveCount == 0 {
		return 0
	}
	expired := 0
	for i := range p.slots {
		s := &p.slots[i]
		if !s.alive {
			continue
		}
		s.decal.Age += dt
		if s.decal.Expired() {
			p.freeSlot(i)
			expired++
		}
	}
	return expired
}

func (p *DecalPool) freeSlot(idx int) {
	s := &p.slots[idx]
	s.alive = false
	s.decal = Decal{}
	p.bumpGen(s)
	p.freeStack = append(p.freeStack, idx)
	p.liveCount--
}

func (p *DecalPool) bumpGen(s *decalSlot) {
	s.gen++
	if s.gen == 0 {
		s.gen = 1
	}
}

func (p *DecalPool) sortActiveOrder() []int {
	order := p.activeOrder[:0]
	for i := range p.slots {
		if p.slots[i].alive {
			order = append(order, i)
		}
	}
	// Insertion sort keeps steady-state ordering allocation-free and deterministic
	// (ascending SortOrder, then ascending spawn sequence oldest-to-newest).
	for i := 1; i < len(order); i++ {
		key := order[i]
		ks := &p.slots[key]
		j := i - 1
		for j >= 0 {
			js := &p.slots[order[j]]
			if js.decal.SortOrder < ks.decal.SortOrder ||
				(js.decal.SortOrder == ks.decal.SortOrder && js.seq <= ks.seq) {
				break
			}
			order[j+1] = order[j]
			j--
		}
		order[j+1] = key
	}
	p.activeOrder = order
	return order
}

// Active returns a newly allocated slice of all live decals in deterministic
// draw order (ascending SortOrder, then oldest-to-newest). Use [DecalPool.ActiveInto]
// or [DecalPool.ForEach] to avoid allocating per frame.
func (p *DecalPool) Active() []Decal {
	if p == nil || p.liveCount == 0 {
		return nil
	}
	return p.ActiveInto(make([]Decal, 0, p.liveCount))
}

// ActiveInto appends all live decals in deterministic draw order to dst and
// returns the extended slice. Pass dst[:0] for zero-allocation reuse.
func (p *DecalPool) ActiveInto(dst []Decal) []Decal {
	if p == nil || p.liveCount == 0 {
		return dst
	}
	for _, idx := range p.sortActiveOrder() {
		dst = append(dst, p.slots[idx].decal)
	}
	return dst
}

// ForEach invokes fn for every live decal in deterministic draw order
// (ascending SortOrder, then oldest-to-newest). Return false to stop early.
func (p *DecalPool) ForEach(fn func(DecalHandle, *Decal) bool) {
	if p == nil || fn == nil || p.liveCount == 0 {
		return
	}
	for _, idx := range p.sortActiveOrder() {
		s := &p.slots[idx]
		h := DecalHandle{index: uint32(idx + 1), gen: s.gen}
		if !fn(h, &s.decal) {
			return
		}
	}
}

// BuildGeometry resets dst and projects every live decal in p onto scene in
// deterministic draw order (ascending SortOrder, then oldest-to-newest). It
// returns the total number of projected triangles. After initial slice warmup,
// BuildGeometry performs zero heap allocations.
func (p *DecalPool) BuildGeometry(scene *Scene, dst *DecalGeometry) int {
	if dst == nil {
		return 0
	}
	dst.Reset()
	if p == nil || scene == nil || p.liveCount == 0 {
		return 0
	}
	total := 0
	for _, idx := range p.sortActiveOrder() {
		s := &p.slots[idx]
		var tris int
		tris, p.queryScratch = projectDecalInScene(scene, s.decal, s.seq, dst, p.queryScratch)
		total += tris
	}
	return total
}

// BuildReceiverGeometry resets dst and projects every live decal in p whose
// LayerMask matches layer onto receiver transformed by worldMat. It returns the
// total number of projected triangles.
func (p *DecalPool) BuildReceiverGeometry(receiver DecalReceiver, worldMat math3d.Mat4, layer uint32, dst *DecalGeometry) int {
	if dst == nil {
		return 0
	}
	dst.Reset()
	if p == nil || receiver == nil || layer == 0 || p.liveCount == 0 {
		return 0
	}
	total := 0
	for _, idx := range p.sortActiveOrder() {
		s := &p.slots[idx]
		if s.decal.LayerMask&layer == 0 {
			continue
		}
		proj, ok := prepareDecalProjection(s.decal)
		if !ok {
			continue
		}
		firstIdx := uint32(len(dst.Indices))
		proj.projectReceiver(receiver, worldMat, dst)
		added := uint32(len(dst.Indices)) - firstIdx
		if added > 0 {
			dst.Batches = append(dst.Batches, DecalBatch{
				DecalID:    s.seq,
				TextureID:  s.decal.TextureID,
				SortOrder:  s.decal.SortOrder,
				FirstIndex: firstIdx,
				IndexCount: added,
			})
			total += int(added / 3)
		}
	}
	return total
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func finite32(x float32) bool {
	return !math.IsNaN(float64(x)) && !math.IsInf(float64(x), 0)
}

func clamp01(v float32) float32 {
	if !(v > 0) {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

func growBytes(dst []byte, n int) []byte {
	if cap(dst)-len(dst) < n {
		next := make([]byte, len(dst), len(dst)+n)
		copy(next, dst)
		dst = next
	}
	return dst[:len(dst)+n]
}

func putF32(b []byte, v float32) {
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
}
