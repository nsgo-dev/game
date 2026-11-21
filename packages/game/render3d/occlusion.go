package render3d

import (
	"math"
	"math/bits"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

const (
	// DefaultOcclusionWidth is the default horizontal resolution of an
	// [OcclusionBuffer] when a non-positive width is passed to [NewOcclusionBuffer].
	DefaultOcclusionWidth = 128
	// DefaultOcclusionHeight is the default vertical resolution of an
	// [OcclusionBuffer] when a non-positive height is passed to [NewOcclusionBuffer].
	DefaultOcclusionHeight = 72

	occlusionMinW      = float32(1e-5)
	occlusionDepthBias = float32(1e-4)
)

// Occluder holds static opaque triangle geometry rasterized into an
// [OcclusionBuffer] to cull hidden objects behind walls, floors, and buildings
// (#380).
type Occluder struct {
	Vertices    []math3d.Vec3
	Indices     []uint32
	Bounds      math3d.AABB
	Transform   math3d.Mat4
	DoubleSided bool

	// edgeFlags stores a 3-bit mask per triangle indicating which edges are
	// internal coplanar shared edges (bit 0: v0->v1, bit 1: v1->v2, bit 2: v2->v0).
	// Coplanar shared edges are rasterized watertightly to prevent diagonal cracks,
	// while silhouette/boundary edges use strict inner-conservative coverage so
	// occluders never falsely cull visible edges.
	edgeFlags []uint8
}

// NewBoxOccluder builds a closed solid box [Occluder] from local-space bounds
// and a world transform. All six faces use outward CCW winding so back faces
// are skipped automatically.
func NewBoxOccluder(bounds math3d.AABB, transform math3d.Mat4) Occluder {
	if transform == (math3d.Mat4{}) {
		transform = math3d.Mat4Identity()
	}
	if bounds.Empty() {
		return Occluder{
			Bounds:    math3d.AABBEmpty(),
			Transform: transform,
		}
	}
	lo, hi := bounds.Min, bounds.Max
	verts := []math3d.Vec3{
		{X: lo.X, Y: lo.Y, Z: lo.Z}, // 0
		{X: hi.X, Y: lo.Y, Z: lo.Z}, // 1
		{X: hi.X, Y: hi.Y, Z: lo.Z}, // 2
		{X: lo.X, Y: hi.Y, Z: lo.Z}, // 3
		{X: lo.X, Y: lo.Y, Z: hi.Z}, // 4
		{X: hi.X, Y: lo.Y, Z: hi.Z}, // 5
		{X: hi.X, Y: hi.Y, Z: hi.Z}, // 6
		{X: lo.X, Y: hi.Y, Z: hi.Z}, // 7
	}
	// Six outward CCW faces, each split along (v0, v2) so tri0 edge 2 and
	// tri1 edge 0 form the coplanar face diagonal.
	indices := []uint32{
		4, 5, 6, 4, 6, 7, // +Z front
		1, 0, 3, 1, 3, 2, // -Z back
		5, 1, 2, 5, 2, 6, // +X right
		0, 4, 7, 0, 7, 3, // -X left
		7, 6, 2, 7, 2, 3, // +Y top
		0, 1, 5, 0, 5, 4, // -Y bottom
	}
	edgeFlags := []uint8{
		0b100, 0b001,
		0b100, 0b001,
		0b100, 0b001,
		0b100, 0b001,
		0b100, 0b001,
		0b100, 0b001,
	}
	return Occluder{
		Vertices:    verts,
		Indices:     indices,
		Bounds:      bounds.Transform(transform),
		Transform:   transform,
		DoubleSided: false,
		edgeFlags:   edgeFlags,
	}
}

// NewQuadOccluder builds a planar quad [Occluder] in world space from four CCW
// corners (a, b, c, d).
func NewQuadOccluder(a, b, c, d math3d.Vec3, doubleSided bool) Occluder {
	bounds := math3d.AABBEmpty().
		ExpandPoint(a).
		ExpandPoint(b).
		ExpandPoint(c).
		ExpandPoint(d)
	return Occluder{
		Vertices:    []math3d.Vec3{a, b, c, d},
		Indices:     []uint32{0, 1, 2, 0, 2, 3},
		Bounds:      bounds,
		Transform:   math3d.Mat4Identity(),
		DoubleSided: doubleSided,
		edgeFlags:   []uint8{0b100, 0b001},
	}
}

type quantPos struct {
	x, y, z int64
}

func quantizeVec3(v math3d.Vec3) quantPos {
	return quantPos{
		x: int64(math.Round(float64(v.X) * 1e4)),
		y: int64(math.Round(float64(v.Y) * 1e4)),
		z: int64(math.Round(float64(v.Z) * 1e4)),
	}
}

func quantPosLess(a, b quantPos) bool {
	if a.x != b.x {
		return a.x < b.x
	}
	if a.y != b.y {
		return a.y < b.y
	}
	return a.z < b.z
}

type edgeKey struct {
	a, b quantPos
}

type halfEdgeInfo struct {
	tri    int
	edge   uint8
	dir    int8
	normal math3d.Vec3
}

// OccluderFromMesh derives a conservative [Occluder] directly from a static
// triangle mesh and its world transform. Degenerate or out-of-range triangles
// are discarded, non-indexed triangle soups (len(indices) == 0) are accepted,
// and coplanar shared edges are detected so flat walls rasterize without
// internal diagonal cracks while boundary and silhouette edges stay strictly
// inner-conservative.
func OccluderFromMesh(vertices []math3d.Vec3, indices []uint32, transform math3d.Mat4) Occluder {
	if transform == (math3d.Mat4{}) {
		transform = math3d.Mat4Identity()
	}
	if len(vertices) < 3 {
		return Occluder{
			Bounds:    math3d.AABBEmpty(),
			Transform: transform,
		}
	}

	var srcIndices []uint32
	if len(indices) == 0 {
		triCount := len(vertices) / 3
		srcIndices = make([]uint32, triCount*3)
		for i := range srcIndices {
			srcIndices[i] = uint32(i)
		}
	} else {
		srcIndices = indices
	}

	vertsCopy := make([]math3d.Vec3, len(vertices))
	copy(vertsCopy, vertices)

	cleanIndices := make([]uint32, 0, len(srcIndices))
	triNormals := make([]math3d.Vec3, 0, len(srcIndices)/3)
	localBounds := math3d.AABBEmpty()

	vCount := uint32(len(vertsCopy))
	for i := 0; i+2 < len(srcIndices); i += 3 {
		i0, i1, i2 := srcIndices[i], srcIndices[i+1], srcIndices[i+2]
		if i0 >= vCount || i1 >= vCount || i2 >= vCount {
			continue
		}
		v0, v1, v2 := vertsCopy[i0], vertsCopy[i1], vertsCopy[i2]
		cross := v1.Sub(v0).Cross(v2.Sub(v0))
		if cross.LenSq() <= 1e-12 {
			continue
		}
		cleanIndices = append(cleanIndices, i0, i1, i2)
		triNormals = append(triNormals, cross.Norm())
		localBounds = localBounds.ExpandPoint(v0).ExpandPoint(v1).ExpandPoint(v2)
	}

	triCount := len(triNormals)
	if triCount == 0 {
		return Occluder{
			Bounds:    math3d.AABBEmpty(),
			Transform: transform,
		}
	}

	edgeFlags := make([]uint8, triCount)
	edgeMap := make(map[edgeKey]halfEdgeInfo, triCount*2)
	for t := range triCount {
		triIdx := [3]uint32{cleanIndices[t*3], cleanIndices[t*3+1], cleanIndices[t*3+2]}
		q := [3]quantPos{
			quantizeVec3(vertsCopy[triIdx[0]]),
			quantizeVec3(vertsCopy[triIdx[1]]),
			quantizeVec3(vertsCopy[triIdx[2]]),
		}
		n := triNormals[t]
		for e := range 3 {
			qa, qb := q[e], q[(e+1)%3]
			var key edgeKey
			var dir int8
			if quantPosLess(qa, qb) {
				key = edgeKey{a: qa, b: qb}
				dir = 1
			} else {
				key = edgeKey{a: qb, b: qa}
				dir = -1
			}
			if prev, exists := edgeMap[key]; exists {
				if prev.dir != dir && prev.normal.Dot(n) > 0.999 {
					edgeFlags[prev.tri] |= 1 << prev.edge
					edgeFlags[t] |= 1 << uint8(e)
				}
			} else {
				edgeMap[key] = halfEdgeInfo{
					tri:    t,
					edge:   uint8(e),
					dir:    dir,
					normal: n,
				}
			}
		}
	}

	return Occluder{
		Vertices:    vertsCopy,
		Indices:     cleanIndices,
		Bounds:      localBounds.Transform(transform),
		Transform:   transform,
		DoubleSided: false,
		edgeFlags:   edgeFlags,
	}
}

type hiZLevel struct {
	width  int
	height int
	data   []float32
}

// OcclusionBuffer is a conservative CPU software depth buffer and max-Z
// hierarchical-Z (Hi-Z) pyramid for zero-false-negative volume occlusion
// culling (#380).
//
// Typical per-frame usage (0 heap allocations in steady state):
//
//	occ.Clear()
//	occ.Rasterize(viewProj, walls...)
//	occ.BuildPyramid()
//	plan := queue.BuildWithOcclusion(viewProj, camPos, items, occ)
type OcclusionBuffer struct {
	// DepthNO selects OpenGL [-1, 1] NDC depth clipping (clip.Z >= -clip.W)
	// when true. By default (false), WebGPU/Metal [0, 1] NDC depth
	// (clip.Z >= 0) is used, matching [math3d.Perspective] and [Queue.FrustumNO].
	DepthNO bool
	// DepthBias is a small conservative depth margin added to Hi-Z max depth
	// during [OcclusionBuffer.TestAABB] to guard against floating-point edge
	// popping. Defaults to 1e-4 when <= 0.
	DepthBias float32

	width, height int
	gbW, gbH      int // guard-band raster dimensions (width+2, height+2)
	guardClipX    float32
	guardClipY    float32

	rawDepth    []float32 // (width+2)*(height+2) rasterization target
	tmpHoriz    []float32 // width*(height+2) scratch for separable 3x3 max filter
	pyramidData []float32 // contiguous backing storage for all Hi-Z mip levels
	levels      []hiZLevel

	hasGeometry  bool
	pyramidBuilt bool
}

// NewOcclusionBuffer allocates a reusable [OcclusionBuffer] of size
// width x height. Non-positive dimensions default to [DefaultOcclusionWidth] x
// [DefaultOcclusionHeight] (128x72).
func NewOcclusionBuffer(width, height int) *OcclusionBuffer {
	if width <= 0 {
		width = DefaultOcclusionWidth
	}
	if height <= 0 {
		height = DefaultOcclusionHeight
	}

	gbW := width + 2
	gbH := height + 2

	// Precompute mip dimensions and total backing slice length.
	totalCells := 0
	lw, lh := width, height
	levelCount := 0
	for {
		totalCells += lw * lh
		levelCount++
		if lw == 1 && lh == 1 {
			break
		}
		if lw > 1 {
			lw = (lw + 1) >> 1
		}
		if lh > 1 {
			lh = (lh + 1) >> 1
		}
	}

	pyramidData := make([]float32, totalCells)
	levels := make([]hiZLevel, levelCount)
	offset := 0
	lw, lh = width, height
	for i := range levelCount {
		n := lw * lh
		levels[i] = hiZLevel{
			width:  lw,
			height: lh,
			data:   pyramidData[offset : offset+n : offset+n],
		}
		offset += n
		if lw > 1 {
			lw = (lw + 1) >> 1
		}
		if lh > 1 {
			lh = (lh + 1) >> 1
		}
	}

	guardClipX := max(float32(1.5), 1.0+4.0/float32(width))
	guardClipY := max(float32(1.5), 1.0+4.0/float32(height))

	b := &OcclusionBuffer{
		width:       width,
		height:      height,
		gbW:         gbW,
		gbH:         gbH,
		guardClipX:  guardClipX,
		guardClipY:  guardClipY,
		rawDepth:    make([]float32, gbW*gbH),
		tmpHoriz:    make([]float32, width*gbH),
		pyramidData: pyramidData,
		levels:      levels,
	}
	b.Clear()
	return b
}

// Width returns the horizontal resolution of the occlusion buffer.
func (b *OcclusionBuffer) Width() int { return b.width }

// Height returns the vertical resolution of the occlusion buffer.
func (b *OcclusionBuffer) Height() int { return b.height }

// Levels returns the number of mip levels in the Hi-Z pyramid.
func (b *OcclusionBuffer) Levels() int { return len(b.levels) }

// DepthAt returns the conservative level-0 depth value at pixel (x, y), or 1.0
// if out of range or if no geometry has been rasterized since [Clear].
func (b *OcclusionBuffer) DepthAt(x, y int) float32 {
	if b == nil || x < 0 || x >= b.width || y < 0 || y >= b.height || !b.hasGeometry {
		return 1.0
	}
	if !b.pyramidBuilt {
		b.BuildPyramid()
	}
	return b.levels[0].data[y*b.width+x]
}

// Clear resets the depth buffer to far (1.0) without allocating.
func (b *OcclusionBuffer) Clear() {
	raw := b.rawDepth
	for i := range raw {
		raw[i] = 1.0
	}
	b.hasGeometry = false
	b.pyramidBuilt = false
}

type clipVertex struct {
	pos      math3d.Vec4
	internal bool
}

// Rasterize projects, near-plane clips, and conservatively rasterizes one or
// more [Occluder] meshes into the depth buffer. Call [OcclusionBuffer.BuildPyramid]
// after rasterizing all occluders for the frame.
func (b *OcclusionBuffer) Rasterize(viewProj math3d.Mat4, occluders ...Occluder) {
	if b == nil || len(occluders) == 0 {
		return
	}

	var frustum math3d.Frustum
	if b.DepthNO {
		frustum = math3d.FrustumFromMat4NO(viewProj)
	} else {
		frustum = math3d.FrustumFromMat4(viewProj)
	}

	ident := math3d.Mat4Identity()
	zeroMat := math3d.Mat4{}

	for oi := range occluders {
		occ := &occluders[oi]
		vCount := uint32(len(occ.Vertices))
		if vCount < 3 || len(occ.Indices) < 3 {
			continue
		}
		if !occ.Bounds.Empty() && occ.Bounds.Min != occ.Bounds.Max {
			if !frustum.IntersectsAABB(occ.Bounds) {
				continue
			}
		}

		var mvp math3d.Mat4
		flipWinding := false
		if occ.Transform == zeroMat || occ.Transform == ident {
			mvp = viewProj
		} else {
			mvp = viewProj.Mul(occ.Transform)
			flipWinding = occ.Transform.Mat3().Det() < 0
		}

		triCount := len(occ.Indices) / 3
		hasFlags := len(occ.edgeFlags) == triCount

		for t := range triCount {
			base := t * 3
			i0, i1, i2 := occ.Indices[base], occ.Indices[base+1], occ.Indices[base+2]
			if i0 >= vCount || i1 >= vCount || i2 >= vCount {
				continue
			}

			c0 := mulMat4Point(mvp, occ.Vertices[i0])
			c1 := mulMat4Point(mvp, occ.Vertices[i1])
			c2 := mulMat4Point(mvp, occ.Vertices[i2])

			var flags uint8
			if hasFlags {
				flags = occ.edgeFlags[t]
			}
			if flipWinding {
				c1, c2 = c2, c1
				// Swapping v1 <-> v2 swaps edge 0 (v0->v1) and edge 2 (v2->v0).
				f0 := (flags & 1) << 2
				f1 := flags & 2
				f2 := (flags & 4) >> 2
				flags = f0 | f1 | f2
			}

			b.rasterizeClipTri(c0, c1, c2, flags, occ.DoubleSided)
		}
	}
}

func mulMat4Point(m math3d.Mat4, p math3d.Vec3) math3d.Vec4 {
	return math3d.Vec4{
		X: m[0]*p.X + m[4]*p.Y + m[8]*p.Z + m[12],
		Y: m[1]*p.X + m[5]*p.Y + m[9]*p.Z + m[13],
		Z: m[2]*p.X + m[6]*p.Y + m[10]*p.Z + m[14],
		W: m[3]*p.X + m[7]*p.Y + m[11]*p.Z + m[15],
	}
}

func (b *OcclusionBuffer) rasterizeClipTri(c0, c1, c2 math3d.Vec4, flags uint8, doubleSided bool) {
	// Trivial reject if all 3 vertices lie behind W <= 0 or beyond far plane.
	if (c0.W <= 0 && c1.W <= 0 && c2.W <= 0) ||
		(c0.Z > c0.W && c1.Z > c1.W && c2.Z > c2.W) {
		return
	}

	// Trivial reject against near plane.
	var near0, near1, near2 float32
	if b.DepthNO {
		near0, near1, near2 = c0.Z+c0.W, c1.Z+c1.W, c2.Z+c2.W
	} else {
		near0, near1, near2 = c0.Z, c1.Z, c2.Z
	}
	if near0 < 0 && near1 < 0 && near2 < 0 {
		return
	}

	gx := b.guardClipX
	gy := b.guardClipY

	// Trivial reject against wide guard-band X/Y planes.
	if (c0.X < -gx*c0.W && c1.X < -gx*c1.W && c2.X < -gx*c2.W) ||
		(c0.X > gx*c0.W && c1.X > gx*c1.W && c2.X > gx*c2.W) ||
		(c0.Y < -gy*c0.W && c1.Y < -gy*c1.W && c2.Y < -gy*c2.W) ||
		(c0.Y > gy*c0.W && c1.Y > gy*c1.W && c2.Y > gy*c2.W) {
		return
	}

	e0Int := (flags & 1) != 0
	e1Int := (flags & 2) != 0
	e2Int := (flags & 4) != 0

	// Fast path: all 3 vertices strictly inside near plane, W >= minW, and
	// within wide X/Y guard bounds -> zero clipping overhead.
	if near0 >= 0 && near1 >= 0 && near2 >= 0 &&
		c0.W >= occlusionMinW && c1.W >= occlusionMinW && c2.W >= occlusionMinW &&
		c0.X >= -gx*c0.W && c0.X <= gx*c0.W && c0.Y >= -gy*c0.W && c0.Y <= gy*c0.W &&
		c1.X >= -gx*c1.W && c1.X <= gx*c1.W && c1.Y >= -gy*c1.W && c1.Y <= gy*c1.W &&
		c2.X >= -gx*c2.W && c2.X <= gx*c2.W && c2.Y >= -gy*c2.W && c2.Y <= gy*c2.W {
		b.rasterizeScreenTri(c0, c1, c2, e0Int, e1Int, e2Int, doubleSided)
		return
	}

	// Stack-allocated Sutherland-Hodgman polygon clipping (0 heap allocs).
	var bufA, bufB [10]clipVertex
	bufA[0] = clipVertex{pos: c0, internal: e0Int}
	bufA[1] = clipVertex{pos: c1, internal: e1Int}
	bufA[2] = clipVertex{pos: c2, internal: e2Int}
	n := 3

	// 1. Clip against near plane (clip.Z >= 0 for ZO, clip.Z + clip.W >= 0 for NO).
	if b.DepthNO {
		n = clipPolygonPlane(&bufA, n, &bufB, clipPlaneNearNO, 0)
	} else {
		n = clipPolygonPlane(&bufA, n, &bufB, clipPlaneNearZO, 0)
	}
	if n < 3 {
		return
	}

	// 2. Guard W >= occlusionMinW.
	n = clipPolygonPlane(&bufB, n, &bufA, clipPlaneMinW, occlusionMinW)
	if n < 3 {
		return
	}

	// 3. Clip against wide guard-band X/Y planes so huge close walls stay
	// bounded in screen space without losing 1-pixel guard-band coverage.
	n = clipPolygonPlane(&bufA, n, &bufB, clipPlaneMinX, gx)
	if n < 3 {
		return
	}
	n = clipPolygonPlane(&bufB, n, &bufA, clipPlaneMaxX, gx)
	if n < 3 {
		return
	}
	n = clipPolygonPlane(&bufA, n, &bufB, clipPlaneMinY, gy)
	if n < 3 {
		return
	}
	n = clipPolygonPlane(&bufB, n, &bufA, clipPlaneMaxY, gy)
	if n < 3 {
		return
	}

	// Fan-triangulate clipped convex polygon; internal fan diagonals are
	// coplanar so they rasterize watertightly with zero seams.
	p0 := bufA[0]
	for k := 1; k < n-1; k++ {
		pk := bufA[k]
		pk1 := bufA[k+1]
		triE0 := k > 1 || p0.internal
		triE1 := pk.internal
		triE2 := k < n-2 || pk1.internal
		b.rasterizeScreenTri(p0.pos, pk.pos, pk1.pos, triE0, triE1, triE2, doubleSided)
	}
}

type clipPlaneKind uint8

const (
	clipPlaneNearZO clipPlaneKind = iota
	clipPlaneNearNO
	clipPlaneMinW
	clipPlaneMinX
	clipPlaneMaxX
	clipPlaneMinY
	clipPlaneMaxY
)

func evalClipPlane(v math3d.Vec4, kind clipPlaneKind, k float32) float32 {
	switch kind {
	case clipPlaneNearZO:
		return v.Z
	case clipPlaneNearNO:
		return v.Z + v.W
	case clipPlaneMinW:
		return v.W - k
	case clipPlaneMinX:
		return v.X + k*v.W
	case clipPlaneMaxX:
		return k*v.W - v.X
	case clipPlaneMinY:
		return v.Y + k*v.W
	default:
		return k*v.W - v.Y
	}
}

func clipPolygonPlane(in *[10]clipVertex, n int, out *[10]clipVertex, kind clipPlaneKind, k float32) int {
	outN := 0
	for i := range n {
		a := in[i]
		next := i + 1
		if next == n {
			next = 0
		}
		b := in[next]
		da := evalClipPlane(a.pos, kind, k)
		db := evalClipPlane(b.pos, kind, k)

		if da >= 0 {
			if outN < len(out) {
				out[outN] = a
				outN++
			}
			if db < 0 {
				t := da / (da - db)
				if outN < len(out) {
					out[outN] = clipVertex{
						pos:      a.pos.Lerp(b.pos, t),
						internal: false, // clip boundary edge
					}
					outN++
				}
			}
		} else if db >= 0 {
			t := da / (da - db)
			if outN < len(out) {
				out[outN] = clipVertex{
					pos:      a.pos.Lerp(b.pos, t),
					internal: a.internal,
				}
				outN++
			}
		}
	}
	return outN
}

func (b *OcclusionBuffer) rasterizeScreenTri(c0, c1, c2 math3d.Vec4, e0Int, e1Int, e2Int, doubleSided bool) {
	wf := float32(b.width)
	hf := float32(b.height)

	// Map NDC [-1, 1] x [-1, 1] into guard-band pixel coordinates [1, W+1] x [1, H+1],
	// leaving a 1-pixel guard ring [0..W+1] x [0..H+1] around the viewport.
	invW0 := 1.0 / c0.W
	x0 := (c0.X*invW0*0.5+0.5)*wf + 1.0
	y0 := (c0.Y*invW0*0.5+0.5)*hf + 1.0
	z0 := c0.Z * invW0

	invW1 := 1.0 / c1.W
	x1 := (c1.X*invW1*0.5+0.5)*wf + 1.0
	y1 := (c1.Y*invW1*0.5+0.5)*hf + 1.0
	z1 := c1.Z * invW1

	invW2 := 1.0 / c2.W
	x2 := (c2.X*invW2*0.5+0.5)*wf + 1.0
	y2 := (c2.Y*invW2*0.5+0.5)*hf + 1.0
	z2 := c2.Z * invW2

	area2 := (x1-x0)*(y2-y0) - (y1-y0)*(x2-x0)
	if !doubleSided {
		if area2 <= 1e-6 {
			return
		}
	} else {
		if area2 < -1e-6 {
			x1, x2 = x2, x1
			y1, y2 = y2, y1
			z1, z2 = z2, z1
			e0Int, e2Int = e2Int, e0Int
			area2 = -area2
		} else if area2 <= 1e-6 {
			return
		}
	}

	minX := clampInt(floorInt(min(x0, min(x1, x2))), 0, b.gbW-1)
	maxX := clampInt(floorInt(max(x0, max(x1, x2))), 0, b.gbW-1)
	minY := clampInt(floorInt(min(y0, min(y1, y2))), 0, b.gbH-1)
	maxY := clampInt(floorInt(max(y0, max(y1, y2))), 0, b.gbH-1)
	if minX > maxX || minY > maxY {
		return
	}

	// Edge equations E_i(x, y) = (x - vx)*a_i + (y - vy)*b_i >= 0 inside CCW triangle.
	a0, b0 := y0-y1, x1-x0
	a1, b1 := y1-y2, x2-x1
	a2, b2 := y2-y0, x0-x2

	invArea2 := 1.0 / area2
	dzdx := (a1*z0 + a2*z1 + a0*z2) * invArea2
	dzdy := (b1*z0 + b2*z1 + b0*z2) * invArea2
	// Maximum (farthest) plane depth across a 1x1 pixel cell guarantees
	// conservative occlusion for sloped and grazing triangles.
	zBias := 0.5 * (absf32(dzdx) + absf32(dzdy))

	sx := float32(minX) + 0.5
	sy := float32(minY) + 0.5

	e0Row := (sx-x0)*a0 + (sy-y0)*b0
	e1Row := (sx-x1)*a1 + (sy-y1)*b1
	e2Row := (sx-x2)*a2 + (sy-y2)*b2
	zRow := (e1Row*z0+e2Row*z1+e0Row*z2)*invArea2 + zBias

	// Inner-conservative offset on boundary/silhouette edges (require all 4 pixel
	// corners inside the edge); tiny negative bias on internal coplanar edges so
	// shared diagonals are watertight with zero cracks.
	e0Row -= edgeBias(a0, b0, e0Int)
	e1Row -= edgeBias(a1, b1, e1Int)
	e2Row -= edgeBias(a2, b2, e2Int)

	raw := b.rawDepth
	gbW := b.gbW
	wrote := false

	for y := minY; y <= maxY; y++ {
		e0, e1, e2 := e0Row, e1Row, e2Row
		z := zRow
		row := raw[y*gbW+minX : y*gbW+maxX+1]
		for x := range row {
			if e0 >= 0 && e1 >= 0 && e2 >= 0 {
				zv := min(z, float32(1.0))
				if zv < row[x] {
					row[x] = zv
					wrote = true
				}
			}
			e0 += a0
			e1 += a1
			e2 += a2
			z += dzdx
		}
		e0Row += b0
		e1Row += b1
		e2Row += b2
		zRow += dzdy
	}

	if wrote {
		b.hasGeometry = true
		b.pyramidBuilt = false
	}
}

func edgeBias(a, b float32, internal bool) float32 {
	manh := absf32(a) + absf32(b)
	if internal {
		return -1e-5 * manh
	}
	return 0.5 * manh
}

// BuildPyramid constructs the conservative max-Z hierarchical-Z (Hi-Z) mip
// pyramid from the rasterized depth buffer. Level 0 applies a separable 3x3
// max erosion over the guard-banded depth buffer so partial pixel silhouettes
// and skewed quad corners never falsely occlude visible objects, and each
// coarser mip cell stores the maximum depth of its 2x2 children.
func (b *OcclusionBuffer) BuildPyramid() {
	if b == nil || len(b.levels) == 0 {
		return
	}

	w, h := b.width, b.height
	gbW, gbH := b.gbW, b.gbH
	raw := b.rawDepth
	tmp := b.tmpHoriz

	// Separable 3x3 max filter: horizontal pass over (W+2)x(H+2) -> Wx(H+2).
	for y := range gbH {
		src := raw[y*gbW : y*gbW+gbW]
		dst := tmp[y*w : y*w+w]
		_ = src[w+1]
		_ = dst[w-1]
		for x := range w {
			dst[x] = max(src[x], max(src[x+1], src[x+2]))
		}
	}

	// Vertical pass over Wx(H+2) -> level 0 (WxH).
	l0 := b.levels[0].data
	for y := range h {
		r0 := tmp[y*w : y*w+w]
		r1 := tmp[(y+1)*w : (y+1)*w+w]
		r2 := tmp[(y+2)*w : (y+2)*w+w]
		dst := l0[y*w : y*w+w]
		_ = r0[w-1]
		_ = r1[w-1]
		_ = r2[w-1]
		_ = dst[w-1]
		for x := range w {
			dst[x] = max(r0[x], max(r1[x], r2[x]))
		}
	}

	// Downsample conservative max-Z mip chain.
	for lvl := 1; lvl < len(b.levels); lvl++ {
		prev := &b.levels[lvl-1]
		curr := &b.levels[lvl]
		pw, ph := prev.width, prev.height
		cw, ch := curr.width, curr.height
		pData := prev.data
		cData := curr.data
		evenCols := pw >> 1

		for cy := range ch {
			py0 := cy << 1
			py1 := py0 + 1
			if py1 >= ph {
				py1 = ph - 1
			}
			row0 := pData[py0*pw : py0*pw+pw]
			row1 := pData[py1*pw : py1*pw+pw]
			dst := cData[cy*cw : cy*cw+cw]
			_ = dst[cw-1]
			for cx := range evenCols {
				px0 := cx << 1
				dst[cx] = max(max(row0[px0], row0[px0+1]), max(row1[px0], row1[px0+1]))
			}
			if evenCols < cw {
				lastX := pw - 1
				dst[evenCols] = max(row0[lastX], row1[lastX])
			}
		}
	}

	b.pyramidBuilt = true
}

// TestAABB reports whether bounds is potentially visible against the Hi-Z
// pyramid. It returns false ONLY when the projected AABB is strictly occluded
// behind the depth buffer (or completely outside the view frustum), guaranteeing
// zero false negatives (no popping).
//
// If any corner of bounds crosses the near plane (camera inside or straddling
// the AABB), TestAABB always returns true.
func (b *OcclusionBuffer) TestAABB(bounds math3d.AABB, viewProj math3d.Mat4) bool {
	if bounds.Empty() {
		return false
	}
	if b == nil || !b.hasGeometry {
		return true
	}
	if !b.pyramidBuilt {
		b.BuildPyramid()
	}

	// Transform all 8 AABB corners incrementally using base + axis deltas.
	base := mulMat4Point(viewProj, bounds.Min)
	size := bounds.Max.Sub(bounds.Min)
	dx := viewProj.Col(0).Scale(size.X)
	dy := viewProj.Col(1).Scale(size.Y)
	dz := viewProj.Col(2).Scale(size.Z)

	c0 := base
	c1 := base.Add(dx)
	c2 := base.Add(dy)
	c3 := c1.Add(dy)
	corners := [8]math3d.Vec4{
		c0,
		c1,
		c2,
		c3,
		c0.Add(dz),
		c1.Add(dz),
		c2.Add(dz),
		c3.Add(dz),
	}

	minNX := float32(math.Inf(1))
	maxNX := float32(math.Inf(-1))
	minNY := float32(math.Inf(1))
	maxNY := float32(math.Inf(-1))
	minZ := float32(math.Inf(1))

	behindNear := 0
	depthNO := b.DepthNO
	for i := range 8 {
		c := corners[i]
		behind := c.W <= occlusionMinW
		if !behind {
			if depthNO {
				behind = c.Z < -c.W
			} else {
				behind = c.Z < 0
			}
		}
		if behind {
			behindNear++
			continue
		}

		invW := 1.0 / c.W
		nx := c.X * invW
		ny := c.Y * invW
		nz := c.Z * invW

		if nx < minNX {
			minNX = nx
		}
		if nx > maxNX {
			maxNX = nx
		}
		if ny < minNY {
			minNY = ny
		}
		if ny > maxNY {
			maxNY = ny
		}
		if nz < minZ {
			minZ = nz
		}
	}

	if behindNear == 8 {
		return false
	}
	if behindNear > 0 {
		return true
	}
	if maxNX < -1 || minNX > 1 || maxNY < -1 || minNY > 1 || minZ > 1 {
		return false
	}

	w, h := b.width, b.height
	x0 := clampInt(floorInt((minNX*0.5+0.5)*float32(w)), 0, w-1)
	x1 := clampInt(floorInt((maxNX*0.5+0.5)*float32(w)), 0, w-1)
	y0 := clampInt(floorInt((minNY*0.5+0.5)*float32(h)), 0, h-1)
	y1 := clampInt(floorInt((maxNY*0.5+0.5)*float32(h)), 0, h-1)

	span := max(x1-x0+1, y1-y0+1)
	lvl := 0
	if span > 1 {
		lvl = bits.Len(uint(span - 1))
	}
	if maxLvl := len(b.levels) - 1; lvl > maxLvl {
		lvl = maxLvl
	}
	if lvl > 0 && ((x1>>(lvl-1))-(x0>>(lvl-1))) <= 1 && ((y1>>(lvl-1))-(y0>>(lvl-1))) <= 1 {
		lvl--
	}

	mip := &b.levels[lvl]
	cx0, cx1 := x0>>lvl, x1>>lvl
	cy0, cy1 := y0>>lvl, y1>>lvl
	if cx1 >= mip.width {
		cx1 = mip.width - 1
	}
	if cy1 >= mip.height {
		cy1 = mip.height - 1
	}

	maxZ := float32(-math.MaxFloat32)
	mw := mip.width
	data := mip.data
	for cy := cy0; cy <= cy1; cy++ {
		row := data[cy*mw : cy*mw+mw]
		for cx := cx0; cx <= cx1; cx++ {
			if v := row[cx]; v > maxZ {
				maxZ = v
			}
		}
	}

	bias := b.DepthBias
	if bias <= 0 {
		bias = occlusionDepthBias
	}
	return minZ <= maxZ+bias
}

func absf32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func floorInt(v float32) int {
	i := int(v)
	if float32(i) > v {
		i--
	}
	return i
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
