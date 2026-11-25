package physics3d

import (
	"cmp"
	"math"
	"slices"
)

// Triangle is a 3D triangle defined by vertices A, B, C (counter-clockwise
// winding defines the outward Normal).
type Triangle struct {
	A, B, C Vec3
}

// Normal returns the unit surface normal of t using counter-clockwise winding.
func (t Triangle) Normal() Vec3 {
	return t.B.Sub(t.A).Cross(t.C.Sub(t.A)).Norm()
}

// Centroid returns the geometric center of t.
func (t Triangle) Centroid() Vec3 {
	return t.A.Add(t.B).Add(t.C).Scale(1.0 / 3.0)
}

// Bounds returns the tight axis-aligned bounding Box enclosing t.
func (t Triangle) Bounds() Box {
	minV := Vec3{
		X: min(t.A.X, min(t.B.X, t.C.X)),
		Y: min(t.A.Y, min(t.B.Y, t.C.Y)),
		Z: min(t.A.Z, min(t.B.Z, t.C.Z)),
	}
	maxV := Vec3{
		X: max(t.A.X, max(t.B.X, t.C.X)),
		Y: max(t.A.Y, max(t.B.Y, t.C.Y)),
		Z: max(t.A.Z, max(t.B.Z, t.C.Z)),
	}
	return BoxFromMinMax(minV, maxV)
}

// RaycastTriangle tests ray against triangle tri using the Möller–Trumbore
// algorithm. It handles both front- and back-facing hits and orients the
// returned Normal to oppose the ray direction.
func RaycastTriangle(ray Ray, tri Triangle) (RaycastHit, bool) {
	if ray.MaxDist <= 0 {
		return RaycastHit{}, false
	}
	dir := ray.Dir.Norm()
	if dir.LenSq() == 0 {
		return RaycastHit{}, false
	}
	return raycastTriangleNorm(ray.Origin, dir, ray.MaxDist, tri)
}

func raycastTriangleNorm(origin, dir Vec3, maxDist float32, tri Triangle) (RaycastHit, bool) {
	const eps = 1e-7
	e1 := tri.B.Sub(tri.A)
	e2 := tri.C.Sub(tri.A)
	pvec := dir.Cross(e2)
	det := e1.Dot(pvec)
	if abs32(det) < eps {
		return RaycastHit{}, false
	}
	invDet := 1 / det
	tvec := origin.Sub(tri.A)
	u := tvec.Dot(pvec) * invDet
	if u < -1e-6 || u > 1+1e-6 {
		return RaycastHit{}, false
	}
	qvec := tvec.Cross(e1)
	v := float32(dir.Dot(qvec) * invDet)
	if v < -1e-6 || float32(u)+v > 1+1e-6 {
		return RaycastHit{}, false
	}
	dist := e2.Dot(qvec) * invDet
	if dist < 0 || dist > maxDist {
		return RaycastHit{}, false
	}
	n := e1.Cross(e2).Norm()
	if n.LenSq() == 0 {
		return RaycastHit{}, false
	}
	if n.Dot(dir) > 0 {
		n = n.Neg()
	}
	return RaycastHit{
		ChildIndex: -1,
		T:          clamp32(dist/maxDist, 0, 1),
		Distance:   dist,
		Point:      origin.Add(dir.Scale(dist)),
		Normal:     n,
	}, true
}

// closestPointOnTriangle returns the closest point on triangle (a, b, c) to p
// using Christer Ericson's Voronoi-region method.
func closestPointOnTriangle(p, a, b, c Vec3) Vec3 {
	ab := b.Sub(a)
	ac := c.Sub(a)
	ap := p.Sub(a)
	d1 := ab.Dot(ap)
	d2 := ac.Dot(ap)
	if d1 <= 0 && d2 <= 0 {
		return a
	}

	bp := p.Sub(b)
	d3 := ab.Dot(bp)
	d4 := ac.Dot(bp)
	if d3 >= 0 && d4 <= d3 {
		return b
	}

	vc := float32(d1*d4) - float32(d3*d2)
	if vc <= 0 && d1 >= 0 && d3 <= 0 {
		v := d1 / (d1 - d3)
		return a.Add(ab.Scale(v))
	}

	cp := p.Sub(c)
	d5 := ab.Dot(cp)
	d6 := ac.Dot(cp)
	if d6 >= 0 && d5 <= d6 {
		return c
	}

	vb := float32(d5*d2) - float32(d1*d6)
	if vb <= 0 && d2 >= 0 && d6 <= 0 {
		w := d2 / (d2 - d6)
		return a.Add(ac.Scale(w))
	}

	va := float32(d3*d6) - float32(d5*d4)
	if va <= 0 && (d4-d3) >= 0 && (d5-d6) >= 0 {
		w := (d4 - d3) / ((d4 - d3) + (d5 - d6))
		return b.Add(c.Sub(b).Scale(w))
	}

	denom := 1 / (va + vb + vc)
	v := vb * denom
	w := vc * denom
	return a.Add(ab.Scale(v)).Add(ac.Scale(w))
}

// TestSphereTriangle tests sphere s against triangle tri. The returned Contact
// normal points from tri toward s.
func TestSphereTriangle(s Sphere, tri Triangle) Contact {
	if s.Radius <= 0 {
		return Contact{}
	}
	q := closestPointOnTriangle(s.Center, tri.A, tri.B, tri.C)
	d := s.Center.Sub(q)
	distSq := d.LenSq()
	if distSq >= s.Radius*s.Radius {
		return Contact{}
	}
	dist := sqrt32(distSq)
	if dist > 1e-6 {
		return Contact{
			Hit:    true,
			Normal: d.Scale(1 / dist),
			Depth:  s.Radius - dist,
			Point:  q,
		}
	}
	n := tri.Normal()
	if n.LenSq() == 0 {
		n = Vec3{Y: 1}
	}
	return Contact{
		Hit:    true,
		Normal: n,
		Depth:  s.Radius,
		Point:  q,
	}
}

func pointInTrianglePlane(p Vec3, tri Triangle, triN Vec3) bool {
	c0 := tri.B.Sub(tri.A).Cross(p.Sub(tri.A)).Dot(triN)
	c1 := tri.C.Sub(tri.B).Cross(p.Sub(tri.B)).Dot(triN)
	c2 := tri.A.Sub(tri.C).Cross(p.Sub(tri.C)).Dot(triN)
	const tol = -1e-5
	return c0 >= tol && c1 >= tol && c2 >= tol
}

// TestCapsuleTriangle tests capsule cap against triangle tri. The returned
// Contact normal points from tri toward cap.
func TestCapsuleTriangle(cap Capsule, tri Triangle) Contact {
	if cap.Radius <= 0 {
		return Contact{}
	}
	triN := tri.Normal()
	if triN.LenSq() == 0 {
		return Contact{}
	}

	// Check if segment [cap.A, cap.B] intersects the triangle plane inside tri.
	dA := cap.A.Sub(tri.A).Dot(triN)
	dB := cap.B.Sub(tri.A).Dot(triN)
	if (dA <= 0 && dB >= 0) || (dA >= 0 && dB <= 0) {
		denom := dA - dB
		var hitPt Vec3
		if abs32(denom) > 1e-7 {
			t := clamp32(dA/denom, 0, 1)
			hitPt = lerpVec3(cap.A, cap.B, t)
		} else {
			hitPt = cap.A.Add(cap.B).Scale(0.5)
		}
		if pointInTrianglePlane(hitPt, tri, triN) {
			// Orient push normal toward the side containing the segment midpoint
			// so deep single-endpoint penetrations push back out cleanly.
			midDot := (dA + dB) * 0.5
			pushN := triN
			deepest := min(dA, dB)
			if midDot < 0 {
				pushN = triN.Neg()
				deepest = min(-dA, -dB)
			}
			depth := cap.Radius - deepest
			if depth < cap.Radius {
				depth = cap.Radius
			}
			return Contact{
				Hit:    true,
				Normal: pushN,
				Depth:  depth,
				Point:  hitPt,
			}
		}
	}

	// Segment does not pierce the triangle interior: minimum distance occurs at
	// one of the two capsule endpoints or along one of the three triangle edges.
	qA := closestPointOnTriangle(cap.A, tri.A, tri.B, tri.C)
	bestSeg := cap.A
	bestTri := qA
	bestDistSq := cap.A.DistSq(qA)

	qB := closestPointOnTriangle(cap.B, tri.A, tri.B, tri.C)
	if ds := cap.B.DistSq(qB); ds < bestDistSq {
		bestDistSq = ds
		bestSeg = cap.B
		bestTri = qB
	}

	edges := [3][2]Vec3{{tri.A, tri.B}, {tri.B, tri.C}, {tri.C, tri.A}}
	for _, e := range edges {
		pS, pE := closestPointsSegments(cap.A, cap.B, e[0], e[1])
		if ds := pS.DistSq(pE); ds < bestDistSq {
			bestDistSq = ds
			bestSeg = pS
			bestTri = pE
		}
	}

	if bestDistSq >= cap.Radius*cap.Radius {
		return Contact{}
	}
	dist := sqrt32(bestDistSq)
	if dist > 1e-6 {
		return Contact{
			Hit:    true,
			Normal: bestSeg.Sub(bestTri).Scale(1 / dist),
			Depth:  cap.Radius - dist,
			Point:  bestTri,
		}
	}
	pushN := triN
	if (dA+dB)*0.5 < 0 {
		pushN = triN.Neg()
	}
	return Contact{
		Hit:    true,
		Normal: pushN,
		Depth:  cap.Radius,
		Point:  bestTri,
	}
}

// TestBoxTriangle tests axis-aligned box b against triangle tri using the
// 13-axis Separating Axis Theorem. The returned Contact normal points from tri
// toward b.
func TestBoxTriangle(b Box, tri Triangle) Contact {
	v0 := tri.A.Sub(b.Center)
	v1 := tri.B.Sub(b.Center)
	v2 := tri.C.Sub(b.Center)
	e := b.HalfExtents

	bestDepth := float32(math.MaxFloat32)
	var bestAxis Vec3

	testAxis := func(axis Vec3, bias float32) bool {
		ls := axis.LenSq()
		if ls <= 1e-10 {
			return true
		}
		inv := 1 / sqrt32(ls)
		n := axis.Scale(inv)
		r := float32(float32(e.X*abs32(n.X))+float32(e.Y*abs32(n.Y))) + float32(e.Z*abs32(n.Z))
		p0 := v0.Dot(n)
		p1 := v1.Dot(n)
		p2 := v2.Dot(n)
		pMin := min(p0, min(p1, p2))
		pMax := max(p0, max(p1, p2))
		if pMax <= -r || pMin >= r {
			return false
		}
		// Separation distances to push box away from triangle along +n or -n.
		pushPos := pMax + r
		pushNeg := r - pMin
		depth := pushPos
		dir := n
		if pushNeg < depth {
			depth = pushNeg
			dir = n.Neg()
		}
		if depth*bias < bestDepth {
			bestDepth = depth
			bestAxis = dir
		}
		return true
	}

	// 1. Triangle face normal (prefer slightly over edge axes on ties).
	e0 := v1.Sub(v0)
	e1 := v2.Sub(v1)
	e2 := v0.Sub(v2)
	triN := e0.Cross(v2.Sub(v0))
	if !testAxis(triN, 0.999) {
		return Contact{}
	}

	// 2. Three AABB face normals.
	if !testAxis(Vec3{X: 1}, 1.0) ||
		!testAxis(Vec3{Y: 1}, 1.0) ||
		!testAxis(Vec3{Z: 1}, 1.0) {
		return Contact{}
	}

	// 3. Nine edge cross-product axes (u_i x e_j).
	triEdges := [3]Vec3{e0, e1, e2}
	for _, te := range triEdges {
		if !testAxis(Vec3{Y: -te.Z, Z: te.Y}, 1.001) || // (1,0,0) x te
			!testAxis(Vec3{X: te.Z, Z: -te.X}, 1.001) || // (0,1,0) x te
			!testAxis(Vec3{X: -te.Y, Y: te.X}, 1.001) { // (0,0,1) x te
			return Contact{}
		}
	}

	if bestDepth == float32(math.MaxFloat32) || bestAxis.LenSq() == 0 {
		return Contact{}
	}
	point := closestPointOnTriangle(b.Center, tri.A, tri.B, tri.C)
	return Contact{
		Hit:    true,
		Normal: bestAxis,
		Depth:  bestDepth,
		Point:  point,
	}
}

// TestShapeTriangle tests any Shape against a Triangle. The returned Contact
// normal points from tri toward s.
func TestShapeTriangle(s Shape, tri Triangle) Contact {
	switch s.Kind {
	case ShapeSphere:
		return TestSphereTriangle(s.Sphere, tri)
	case ShapeCapsule:
		return TestCapsuleTriangle(s.Capsule, tri)
	case ShapeBox:
		if r, ok := s.orientedBox(); ok {
			f := newBoxFrame(s.Box.Center, r)
			local := Triangle{A: f.toLocal(tri.A), B: f.toLocal(tri.B), C: f.toLocal(tri.C)}
			return f.contactToWorld(TestBoxTriangle(Box{HalfExtents: s.Box.HalfExtents}, local))
		}
		return TestBoxTriangle(s.Box, tri)
	case ShapeConvexHull, ShapeCylinder:
		return testConvexTriangle(toCvx(s), tri)
	case ShapeCompound:
		var best Contact
		for _, child := range s.Children {
			if c := TestShapeTriangle(child.LocalShape(), tri); c.Hit && (!best.Hit || c.Depth > best.Depth) {
				best = c
			}
		}
		return best
	default:
		return Contact{}
	}
}

// testShapeTriangleSolid is like TestShapeTriangle but skips nested Trigger children
// inside compound shapes.
func testShapeTriangleSolid(s Shape, tri Triangle) Contact {
	if s.Kind == ShapeCompound {
		var best Contact
		for _, child := range s.Children {
			if child.Trigger {
				continue
			}
			if c := testShapeTriangleSolid(child.LocalShape(), tri); c.Hit && (!best.Hit || c.Depth > best.Depth) {
				best = c
			}
		}
		return best
	}
	return TestShapeTriangle(s, tri)
}

// rayIntersectsBoxDist returns (tMin, tMax, hit) for a normalized ray against box.
func rayIntersectsBoxDist(origin, dir Vec3, maxDist float32, b Box) (float32, float32, bool) {
	bMin, bMax := b.Min(), b.Max()
	tMin := float32(0)
	tMax := maxDist

	slab := func(orig, d, lo, hi float32) bool {
		if abs32(d) < 1e-8 {
			return orig >= lo && orig <= hi
		}
		inv := 1 / d
		t1 := (lo - orig) * inv
		t2 := (hi - orig) * inv
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		if t1 > tMin {
			tMin = t1
		}
		if t2 < tMax {
			tMax = t2
		}
		return tMin <= tMax
	}

	if !slab(origin.X, dir.X, bMin.X, bMax.X) ||
		!slab(origin.Y, dir.Y, bMin.Y, bMax.Y) ||
		!slab(origin.Z, dir.Z, bMin.Z, bMax.Z) {
		return 0, 0, false
	}
	return tMin, tMax, true
}

const bvhLeafSize = 4

type bvhNode struct {
	bounds   Box
	left     int
	right    int
	firstTri int
	triCount int
}

// TriMeshCollider is a static triangle-mesh collider accelerated by a pure-Go
// AABB Bounding Volume Hierarchy (BVH).
type TriMeshCollider struct {
	ID          int
	Tag         string
	Layer       LayerMask
	Mask        LayerMask
	Friction    float32
	Restitution float32
	Triangles   []Triangle
	Bounds      Box

	nodes    []bvhNode
	triOrder []int
}

// NewTriMeshCollider builds a TriMeshCollider and its AABB BVH from indexed
// triangle soup (directly compatible with glTF mesh positions and indices).
func NewTriMeshCollider(vertices []Vec3, indices []uint32) *TriMeshCollider {
	triCount := len(indices) / 3
	tris := make([]Triangle, 0, triCount)
	for i := 0; i+2 < len(indices); i += 3 {
		ia, ib, ic := int(indices[i]), int(indices[i+1]), int(indices[i+2])
		if ia < 0 || ia >= len(vertices) || ib < 0 || ib >= len(vertices) || ic < 0 || ic >= len(vertices) {
			continue
		}
		tri := Triangle{A: vertices[ia], B: vertices[ib], C: vertices[ic]}
		if tri.B.Sub(tri.A).Cross(tri.C.Sub(tri.A)).LenSq() <= 1e-12 {
			continue
		}
		tris = append(tris, tri)
	}

	tm := &TriMeshCollider{
		Friction:  0.5,
		Triangles: tris,
	}
	if len(tris) == 0 {
		return tm
	}

	n := len(tris)
	triBounds := make([]Box, n)
	triCentroids := make([]Vec3, n)
	tm.triOrder = make([]int, n)
	for i, tri := range tris {
		triBounds[i] = tri.Bounds()
		triCentroids[i] = tri.Centroid()
		tm.triOrder[i] = i
	}

	tm.nodes = make([]bvhNode, 0, 2*n)
	tm.buildBVH(0, n, triBounds, triCentroids)
	tm.Bounds = tm.nodes[0].bounds
	return tm
}

func unionBounds(a, b Box) Box {
	aMin, aMax := a.Min(), a.Max()
	bMin, bMax := b.Min(), b.Max()
	return BoxFromMinMax(
		Vec3{X: min(aMin.X, bMin.X), Y: min(aMin.Y, bMin.Y), Z: min(aMin.Z, bMin.Z)},
		Vec3{X: max(aMax.X, bMax.X), Y: max(aMax.Y, bMax.Y), Z: max(aMax.Z, bMax.Z)},
	)
}

func (tm *TriMeshCollider) buildBVH(start, count int, triBounds []Box, triCentroids []Vec3) int {
	nodeIdx := len(tm.nodes)
	tm.nodes = append(tm.nodes, bvhNode{left: -1, right: -1})

	// Compute node AABB and centroid AABB.
	b := triBounds[tm.triOrder[start]]
	cMin := triCentroids[tm.triOrder[start]]
	cMax := cMin
	for i := start + 1; i < start+count; i++ {
		idx := tm.triOrder[i]
		b = unionBounds(b, triBounds[idx])
		c := triCentroids[idx]
		cMin = Vec3{X: min(cMin.X, c.X), Y: min(cMin.Y, c.Y), Z: min(cMin.Z, c.Z)}
		cMax = Vec3{X: max(cMax.X, c.X), Y: max(cMax.Y, c.Y), Z: max(cMax.Z, c.Z)}
	}
	tm.nodes[nodeIdx].bounds = b

	if count <= bvhLeafSize {
		tm.nodes[nodeIdx].firstTri = start
		tm.nodes[nodeIdx].triCount = count
		return nodeIdx
	}

	// Split along the longest centroid axis at its midpoint.
	ext := cMax.Sub(cMin)
	axis := 0
	if ext.Y > ext.X && ext.Y >= ext.Z {
		axis = 1
	} else if ext.Z > ext.X && ext.Z > ext.Y {
		axis = 2
	}

	coord := func(v Vec3) float32 {
		switch axis {
		case 1:
			return v.Y
		case 2:
			return v.Z
		default:
			return v.X
		}
	}
	midVal := (coord(cMin) + coord(cMax)) * 0.5

	// Partition triOrder[start : start+count].
	i := start
	j := start + count - 1
	for i <= j {
		if coord(triCentroids[tm.triOrder[i]]) < midVal {
			i++
		} else {
			tm.triOrder[i], tm.triOrder[j] = tm.triOrder[j], tm.triOrder[i]
			j--
		}
	}
	leftCount := i - start
	if leftCount < count/8 || leftCount > count-count/8 {
		// A lopsided midpoint split (clustered or exponentially spaced triangles) would make the tree as deep as the mesh is long, so split at the centroid median instead, which bounds the depth by log2 of the triangle count (#546).
		slices.SortStableFunc(tm.triOrder[start:start+count], func(x, y int) int {
			if c := cmp.Compare(coord(triCentroids[x]), coord(triCentroids[y])); c != 0 {
				return c
			}
			return cmp.Compare(x, y)
		})
		leftCount = count / 2
	}

	leftIdx := tm.buildBVH(start, leftCount, triBounds, triCentroids)
	rightIdx := tm.buildBVH(start+leftCount, count-leftCount, triBounds, triCentroids)
	tm.nodes[nodeIdx].left = leftIdx
	tm.nodes[nodeIdx].right = rightIdx
	return nodeIdx
}

// Raycast traverses the BVH to find the closest intersection between ray and tm.
func (tm *TriMeshCollider) Raycast(ray Ray) (RaycastHit, bool) {
	if len(tm.nodes) == 0 || ray.MaxDist <= 0 {
		return RaycastHit{}, false
	}
	dir := ray.Dir.Norm()
	if dir.LenSq() == 0 {
		return RaycastHit{}, false
	}
	if _, _, hit := rayIntersectsBoxDist(ray.Origin, dir, ray.MaxDist, tm.nodes[0].bounds); !hit {
		return RaycastHit{}, false
	}

	// The stack grows past its inline capacity instead of dropping subtrees, so a deep tree never loses triangles (#546).
	var buf [64]int
	stack := append(buf[:0], 0)

	var best RaycastHit
	bestDist := ray.MaxDist
	found := false

	for len(stack) > 0 {
		node := &tm.nodes[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		if node.triCount > 0 {
			for i := 0; i < node.triCount; i++ {
				tri := tm.Triangles[tm.triOrder[node.firstTri+i]]
				if h, ok := raycastTriangleNorm(ray.Origin, dir, bestDist, tri); ok {
					if !found || h.Distance < bestDist {
						bestDist = h.Distance
						h.T = clamp32(h.Distance/ray.MaxDist, 0, 1)
						best = h
						found = true
					}
				}
			}
			continue
		}

		tL, _, hitL := rayIntersectsBoxDist(ray.Origin, dir, bestDist, tm.nodes[node.left].bounds)
		tR, _, hitR := rayIntersectsBoxDist(ray.Origin, dir, bestDist, tm.nodes[node.right].bounds)

		if hitL && hitR {
			// Push farther child first so nearer child is popped first.
			if tL <= tR {
				stack = append(stack, node.right, node.left)
			} else {
				stack = append(stack, node.left, node.right)
			}
		} else if hitL {
			stack = append(stack, node.left)
		} else if hitR {
			stack = append(stack, node.right)
		}
	}

	return best, found
}

// QueryTriangles visits every triangle in tm whose leaf BVH node overlaps bounds.
func (tm *TriMeshCollider) QueryTriangles(bounds Box, visit func(tri Triangle)) {
	if len(tm.nodes) == 0 || !aabbOverlap(bounds, tm.nodes[0].bounds) {
		return
	}
	var buf [64]int
	stack := append(buf[:0], 0)
	for len(stack) > 0 {
		node := &tm.nodes[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		if node.triCount > 0 {
			for i := 0; i < node.triCount; i++ {
				visit(tm.Triangles[tm.triOrder[node.firstTri+i]])
			}
			continue
		}
		if aabbOverlap(bounds, tm.nodes[node.right].bounds) {
			stack = append(stack, node.right)
		}
		if aabbOverlap(bounds, tm.nodes[node.left].bounds) {
			stack = append(stack, node.left)
		}
	}
}

// TestShape returns the deepest Contact between s and tm (normal pointing from
// tm toward s).
func (tm *TriMeshCollider) TestShape(s Shape) Contact {
	var best Contact
	sb := s.Bounds()
	tm.QueryTriangles(sb, func(tri Triangle) {
		if !aabbOverlap(sb, tri.Bounds()) {
			return
		}
		if c := TestShapeTriangle(s, tri); c.Hit {
			if !best.Hit || c.Depth > best.Depth {
				best = c
			}
		}
	})
	return best
}

// CollectContacts appends all overlapping triangle contacts for s to dst.
func (tm *TriMeshCollider) CollectContacts(s Shape, dst []Contact) []Contact {
	sb := s.Bounds()
	tm.QueryTriangles(sb, func(tri Triangle) {
		if !aabbOverlap(sb, tri.Bounds()) {
			return
		}
		if c := TestShapeTriangle(s, tri); c.Hit {
			dst = append(dst, c)
		}
	})
	return dst
}

// HeightfieldCollider is a static terrain heightfield collider on a regular XZ
// grid with exact barycentric surface interpolation, DDA raycasting, and
// sphere/capsule/box collision.
//
// Vertex (cx, cz) sits at world position:
//
//	(Origin.X + cx*CellSize, Origin.Y + Heights[cz*Cols + cx], Origin.Z + cz*CellSize)
type HeightfieldCollider struct {
	ID          int
	Tag         string
	Layer       LayerMask
	Mask        LayerMask
	Friction    float32
	Restitution float32
	Cols        int
	Rows        int
	CellSize    float32
	Origin      Vec3
	Heights     []float32
	Bounds      Box
}

// NewHeightfieldCollider constructs a HeightfieldCollider matching world3d.Heightmap
// grid conventions (row-major heights[cz*cols + cx]).
func NewHeightfieldCollider(cols, rows int, cellSize float32, origin Vec3, heights []float32) *HeightfieldCollider {
	hf := &HeightfieldCollider{
		Friction: 0.5,
		Cols:     cols,
		Rows:     rows,
		CellSize: cellSize,
		Origin:   origin,
	}
	if cols < 2 || rows < 2 || cellSize <= 0 || len(heights) < cols*rows {
		return hf
	}
	hf.Heights = make([]float32, cols*rows)
	copy(hf.Heights, heights[:cols*rows])

	minY := hf.Heights[0]
	maxY := hf.Heights[0]
	for _, h := range hf.Heights[1:] {
		if h < minY {
			minY = h
		}
		if h > maxY {
			maxY = h
		}
	}
	hf.Bounds = BoxFromMinMax(
		Vec3{X: origin.X, Y: origin.Y + minY - 0.01, Z: origin.Z},
		Vec3{
			X: origin.X + float32(float32(cols-1)*cellSize),
			Y: origin.Y + maxY + 0.01,
			Z: origin.Z + float32(float32(rows-1)*cellSize),
		},
	)
	return hf
}

func (hf *HeightfieldCollider) valid() bool {
	return hf != nil && hf.Cols >= 2 && hf.Rows >= 2 && hf.CellSize > 0 && len(hf.Heights) >= hf.Cols*hf.Rows
}

func (hf *HeightfieldCollider) vertex(cx, cz int) Vec3 {
	return Vec3{
		X: hf.Origin.X + float32(float32(cx)*hf.CellSize),
		Y: hf.Origin.Y + hf.Heights[cz*hf.Cols+cx],
		Z: hf.Origin.Z + float32(float32(cz)*hf.CellSize),
	}
}

func (hf *HeightfieldCollider) cellTriangles(cx, cz int) (Triangle, Triangle) {
	p00 := hf.vertex(cx, cz)
	p10 := hf.vertex(cx+1, cz)
	p01 := hf.vertex(cx, cz+1)
	p11 := hf.vertex(cx+1, cz+1)
	// Shared diagonal p00 -> p11; both triangles have CCW +Y upward normals.
	return Triangle{A: p00, B: p11, C: p10}, Triangle{A: p00, B: p01, C: p11}
}

// HeightAt returns the exact barycentric terrain elevation at world (x, z) and
// whether (x, z) lies within the heightfield's XZ footprint.
func (hf *HeightfieldCollider) HeightAt(x, z float32) (float32, bool) {
	if !hf.valid() {
		return 0, false
	}
	gx := (x - hf.Origin.X) / hf.CellSize
	gz := (z - hf.Origin.Z) / hf.CellSize
	maxX := float32(hf.Cols - 1)
	maxZ := float32(hf.Rows - 1)
	if gx < 0 || gx > maxX || gz < 0 || gz > maxZ {
		return 0, false
	}
	cx := int(math.Floor(float64(gx)))
	cz := int(math.Floor(float64(gz)))
	if cx >= hf.Cols-1 {
		cx = hf.Cols - 2
	}
	if cz >= hf.Rows-1 {
		cz = hf.Rows - 2
	}
	u := gx - float32(cx)
	v := gz - float32(cz)

	h00 := hf.Heights[cz*hf.Cols+cx]
	h10 := hf.Heights[cz*hf.Cols+cx+1]
	h01 := hf.Heights[(cz+1)*hf.Cols+cx]
	h11 := hf.Heights[(cz+1)*hf.Cols+cx+1]

	var h float32
	if u >= v {
		// Triangle (p00, p11, p10)
		h = float32(h00+float32((h10-h00)*u)) + float32((h11-h10)*v)
	} else {
		// Triangle (p00, p01, p11)
		h = float32(h00+float32((h11-h01)*u)) + float32((h01-h00)*v)
	}
	return hf.Origin.Y + h, true
}

// NormalAt returns the outward unit surface normal of the terrain at world (x, z).
// Coordinates outside the footprint are clamped to the nearest boundary cell.
func (hf *HeightfieldCollider) NormalAt(x, z float32) Vec3 {
	if !hf.valid() {
		return Vec3{Y: 1}
	}
	maxX := float32(hf.Cols - 1)
	maxZ := float32(hf.Rows - 1)
	gx := clamp32((x-hf.Origin.X)/hf.CellSize, 0, maxX)
	gz := clamp32((z-hf.Origin.Z)/hf.CellSize, 0, maxZ)

	cx := int(math.Floor(float64(gx)))
	cz := int(math.Floor(float64(gz)))
	if cx >= hf.Cols-1 {
		cx = hf.Cols - 2
	}
	if cz >= hf.Rows-1 {
		cz = hf.Rows - 2
	}
	u := gx - float32(cx)
	v := gz - float32(cz)
	t0, t1 := hf.cellTriangles(cx, cz)
	if u >= v {
		return t0.Normal()
	}
	return t1.Normal()
}

// Raycast intersects ray with the heightfield using clipped 2D DDA grid
// traversal over only the cells crossed by the ray.
func (hf *HeightfieldCollider) Raycast(ray Ray) (RaycastHit, bool) {
	if !hf.valid() || ray.MaxDist <= 0 {
		return RaycastHit{}, false
	}
	dir := ray.Dir.Norm()
	if dir.LenSq() == 0 {
		return RaycastHit{}, false
	}
	tStart, tEnd, hitBox := rayIntersectsBoxDist(ray.Origin, dir, ray.MaxDist, hf.Bounds)
	if !hitBox {
		return RaycastHit{}, false
	}

	pStart := ray.Origin.Add(dir.Scale(tStart))
	gx := (pStart.X - hf.Origin.X) / hf.CellSize
	gz := (pStart.Z - hf.Origin.Z) / hf.CellSize
	cx := int(math.Floor(float64(gx)))
	cz := int(math.Floor(float64(gz)))
	if cx < 0 {
		cx = 0
	} else if cx >= hf.Cols-1 {
		cx = hf.Cols - 2
	}
	if cz < 0 {
		cz = 0
	} else if cz >= hf.Rows-1 {
		cz = hf.Rows - 2
	}

	var best RaycastHit
	bestDist := ray.MaxDist
	found := false

	testCell := func(x, z int) {
		t0, t1 := hf.cellTriangles(x, z)
		if h, ok := raycastTriangleNorm(ray.Origin, dir, bestDist, t0); ok {
			if !found || h.Distance < bestDist {
				bestDist = h.Distance
				h.T = clamp32(h.Distance/ray.MaxDist, 0, 1)
				best = h
				found = true
			}
		}
		if h, ok := raycastTriangleNorm(ray.Origin, dir, bestDist, t1); ok {
			if !found || h.Distance < bestDist {
				bestDist = h.Distance
				h.T = clamp32(h.Distance/ray.MaxDist, 0, 1)
				best = h
				found = true
			}
		}
	}

	// Vertical ray special case.
	if abs32(dir.X) < 1e-8 && abs32(dir.Z) < 1e-8 {
		testCell(cx, cz)
		return best, found
	}

	const bigT = float32(1e30)
	stepX, stepZ := 0, 0
	tMaxX, tMaxZ := bigT, bigT
	tDeltaX, tDeltaZ := bigT, bigT

	if abs32(dir.X) >= 1e-8 {
		if dir.X > 0 {
			stepX = 1
			nextX := hf.Origin.X + float32(float32(cx+1)*hf.CellSize)
			tMaxX = (nextX - ray.Origin.X) / dir.X
		} else {
			stepX = -1
			nextX := hf.Origin.X + float32(float32(cx)*hf.CellSize)
			tMaxX = (nextX - ray.Origin.X) / dir.X
		}
		tDeltaX = hf.CellSize / abs32(dir.X)
	}
	if abs32(dir.Z) >= 1e-8 {
		if dir.Z > 0 {
			stepZ = 1
			nextZ := hf.Origin.Z + float32(float32(cz+1)*hf.CellSize)
			tMaxZ = (nextZ - ray.Origin.Z) / dir.Z
		} else {
			stepZ = -1
			nextZ := hf.Origin.Z + float32(float32(cz)*hf.CellSize)
			tMaxZ = (nextZ - ray.Origin.Z) / dir.Z
		}
		tDeltaZ = hf.CellSize / abs32(dir.Z)
	}

	maxSteps := hf.Cols + hf.Rows + 4
	for step := 0; step < maxSteps; step++ {
		if cx < 0 || cx >= hf.Cols-1 || cz < 0 || cz >= hf.Rows-1 {
			break
		}
		testCell(cx, cz)
		tNext := min(tMaxX, tMaxZ)
		if found && bestDist <= tNext+1e-5 {
			break
		}
		if tNext > tEnd+1e-5 {
			break
		}
		if tMaxX < tMaxZ {
			cx += stepX
			tMaxX += tDeltaX
		} else {
			cz += stepZ
			tMaxZ += tDeltaZ
		}
	}

	return best, found
}

// QueryTriangles visits every terrain triangle overlapping the XZ footprint of bounds.
func (hf *HeightfieldCollider) QueryTriangles(bounds Box, visit func(tri Triangle)) {
	if !hf.valid() || !aabbOverlap(bounds, hf.Bounds) {
		return
	}
	bMin, bMax := bounds.Min(), bounds.Max()
	cx0 := int(math.Floor(float64((bMin.X - hf.Origin.X) / hf.CellSize)))
	cx1 := int(math.Floor(float64((bMax.X - hf.Origin.X) / hf.CellSize)))
	cz0 := int(math.Floor(float64((bMin.Z - hf.Origin.Z) / hf.CellSize)))
	cz1 := int(math.Floor(float64((bMax.Z - hf.Origin.Z) / hf.CellSize)))

	if cx0 < 0 {
		cx0 = 0
	}
	if cx1 >= hf.Cols-1 {
		cx1 = hf.Cols - 2
	}
	if cz0 < 0 {
		cz0 = 0
	}
	if cz1 >= hf.Rows-1 {
		cz1 = hf.Rows - 2
	}

	for cz := cz0; cz <= cz1; cz++ {
		for cx := cx0; cx <= cx1; cx++ {
			t0, t1 := hf.cellTriangles(cx, cz)
			visit(t0)
			visit(t1)
		}
	}
}

func testShapeHeightfieldTriangle(s Shape, tri Triangle) Contact {
	return testShapeHeightfieldTriangleOpt(s, tri, false)
}

func testShapeHeightfieldTriangleSolid(s Shape, tri Triangle) Contact {
	return testShapeHeightfieldTriangleOpt(s, tri, true)
}

func testShapeHeightfieldTriangleOpt(s Shape, tri Triangle, solidOnly bool) Contact {
	var c Contact
	if solidOnly {
		c = testShapeTriangleSolid(s, tri)
	} else {
		c = TestShapeTriangle(s, tri)
	}
	if !c.Hit {
		return c
	}
	// Heightfields are solid underneath; ensure separation normal always points upward (+Y) rather than pushing a penetrating shape down into terrain.
	if c.Normal.Y < 0 {
		n := tri.Normal()
		if n.Y <= 0 {
			n = c.Normal.Neg()
		}
		c.Normal = n
		// Depth belongs to the normal it was measured along. When the shape lies under this triangle, the new normal must lift it clear of the triangle's highest point: a sphere whose center is 0.3 below a flat cell needs radius+0.3 to get out upward, not the radius-0.3 that pushed it down (#546). A shape beside the triangle, such as one touching a plateau's edge from the foot of a cliff, keeps its measured depth: re-measuring there reported the whole step height and lifted it onto the plateau.
		if shapeUnderTriangle(s, tri, n, solidOnly) {
			depth := triangleMaxAlong(tri, n) - shapeMinAlongOpt(s, n, solidOnly)
			if !(depth > 0) {
				return Contact{}
			}
			c.Depth = depth
		}
	}
	return c
}

// shapeUnderTriangle reports whether a reference point of s (a sphere's or box's center, a hull's centroid, or a point of a capsule's or cylinder's axis) lies inside tri's XZ footprint and below its plane, whose upward unit normal is n.
func shapeUnderTriangle(s Shape, tri Triangle, n Vec3, solidOnly bool) bool {
	under := func(p Vec3) bool {
		return p.Sub(tri.A).Dot(n) < 0 && pointInTriangleXZ(p, tri)
	}
	switch s.Kind {
	case ShapeSphere:
		return under(s.Sphere.Center)
	case ShapeCapsule:
		a, b := s.Capsule.A, s.Capsule.B
		return under(a) || under(b) || under(a.Add(b).Scale(0.5))
	case ShapeBox:
		return under(s.Box.Center)
	case ShapeConvexHull:
		if s.Hull.geom == nil {
			return false
		}
		c := toCvx(s)
		return under(c.centroid().vec3())
	case ShapeCylinder:
		a, b := s.Cylinder.A, s.Cylinder.B
		return under(a) || under(b) || under(a.Add(b).Scale(0.5))
	case ShapeCompound:
		for _, child := range s.Children {
			if solidOnly && child.Trigger {
				continue
			}
			if shapeUnderTriangle(child.LocalShape(), tri, n, solidOnly) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// pointInTriangleXZ reports whether p's XZ projection lies inside or on the edge of tri's XZ projection.
func pointInTriangleXZ(p Vec3, tri Triangle) bool {
	edge := func(a, b Vec3) float32 {
		return float32((b.X-a.X)*(p.Z-a.Z)) - float32((b.Z-a.Z)*(p.X-a.X))
	}
	d0, d1, d2 := edge(tri.A, tri.B), edge(tri.B, tri.C), edge(tri.C, tri.A)
	const eps = 1e-6
	hasNeg := d0 < -eps || d1 < -eps || d2 < -eps
	hasPos := d0 > eps || d1 > eps || d2 > eps
	return !(hasNeg && hasPos)
}

// triangleMaxAlong returns the largest projection of tri's vertices onto n.
func triangleMaxAlong(tri Triangle, n Vec3) float32 {
	return max(tri.A.Dot(n), tri.B.Dot(n), tri.C.Dot(n))
}

// shapeMinAlong returns the smallest projection of s onto the unit direction n.
func shapeMinAlong(s Shape, n Vec3) float32 {
	return shapeMinAlongOpt(s, n, false)
}

func shapeMinAlongOpt(s Shape, n Vec3, solidOnly bool) float32 {
	switch s.Kind {
	case ShapeSphere:
		return s.Sphere.Center.Dot(n) - s.Sphere.Radius
	case ShapeCapsule:
		return min(s.Capsule.A.Dot(n), s.Capsule.B.Dot(n)) - s.Capsule.Radius
	case ShapeBox:
		e := s.Box.HalfExtents
		r := float32(float32(e.X*abs32(n.X))+float32(e.Y*abs32(n.Y))) + float32(e.Z*abs32(n.Z))
		if rot, ok := s.orientedBox(); ok {
			ax := boxAxes(rot)
			r = float32(float32(e.X*abs32(ax[0].Dot(n)))+float32(e.Y*abs32(ax[1].Dot(n)))) + float32(e.Z*abs32(ax[2].Dot(n)))
		}
		return s.Box.Center.Dot(n) - r
	case ShapeConvexHull, ShapeCylinder:
		c := toCvx(s)
		if !c.valid() {
			return 0
		}
		nd := toDV(n)
		return float32(c.support(nd.neg()).dot(nd))
	case ShapeCompound:
		hasAny := false
		var minV float32
		for _, child := range s.Children {
			if solidOnly && child.Trigger {
				continue
			}
			cs := child.LocalShape()
			if cs.Kind == ShapeCompound && len(cs.Children) == 0 {
				continue
			}
			v := shapeMinAlongOpt(cs, n, solidOnly)
			if !hasAny || v < minV {
				minV = v
				hasAny = true
			}
		}
		return minV
	default:
		return 0
	}
}

// TestShape returns the deepest Contact between s and hf (normal pointing from
// terrain upward toward s).
func (hf *HeightfieldCollider) TestShape(s Shape) Contact {
	var best Contact
	sb := s.Bounds()
	hf.QueryTriangles(sb, func(tri Triangle) {
		if !aabbOverlap(sb, tri.Bounds()) {
			return
		}
		if c := testShapeHeightfieldTriangle(s, tri); c.Hit {
			if !best.Hit || c.Depth > best.Depth {
				best = c
			}
		}
	})
	return best
}

// CollectContacts appends all overlapping heightfield triangle contacts for s to dst.
func (hf *HeightfieldCollider) CollectContacts(s Shape, dst []Contact) []Contact {
	sb := s.Bounds()
	hf.QueryTriangles(sb, func(tri Triangle) {
		if !aabbOverlap(sb, tri.Bounds()) {
			return
		}
		if c := testShapeHeightfieldTriangle(s, tri); c.Hit {
			dst = append(dst, c)
		}
	})
	return dst
}

// AddTriMesh registers a static TriMeshCollider in w and returns it.
func (w *World) AddTriMesh(tm *TriMeshCollider) *TriMeshCollider {
	if tm == nil {
		return nil
	}
	// An explicit ID already used by another mesh or heightfield is reassigned, as AddBody does for bodies, so contact ordering and removal by ID stay unambiguous (#546).
	if tm.ID <= 0 || w.colliderIDTaken(tm.ID) {
		w.nextColliderID++
		tm.ID = w.nextColliderID
	} else if tm.ID >= w.nextColliderID {
		w.nextColliderID = tm.ID
	}
	if tm.Friction < 0 {
		tm.Friction = 0
	}
	if tm.Restitution < 0 {
		tm.Restitution = 0
	}
	if tm.Restitution > 1 {
		tm.Restitution = 1
	}
	w.triMeshes = append(w.triMeshes, tm)
	w.bp.addTriMesh(w, tm)
	return tm
}

// RemoveTriMesh removes the triangle-mesh collider with the given ID from w.
func (w *World) RemoveTriMesh(id int) bool {
	for i, tm := range w.triMeshes {
		if tm.ID == id {
			w.triMeshes = append(w.triMeshes[:i], w.triMeshes[i+1:]...)
			w.bp.removeTriMesh(w, i)
			w.wakeAllDynamic()
			return true
		}
	}
	return false
}

// TriMeshes returns all static TriMeshColliders registered in w.
func (w *World) TriMeshes() []*TriMeshCollider {
	return w.triMeshes
}

// AddHeightfield registers a static HeightfieldCollider in w and returns it.
func (w *World) AddHeightfield(hf *HeightfieldCollider) *HeightfieldCollider {
	if hf == nil {
		return nil
	}
	// An explicit ID already used by another mesh or heightfield is reassigned, as AddBody does for bodies, so contact ordering and removal by ID stay unambiguous (#546).
	if hf.ID <= 0 || w.colliderIDTaken(hf.ID) {
		w.nextColliderID++
		hf.ID = w.nextColliderID
	} else if hf.ID >= w.nextColliderID {
		w.nextColliderID = hf.ID
	}
	if hf.Friction < 0 {
		hf.Friction = 0
	}
	if hf.Restitution < 0 {
		hf.Restitution = 0
	}
	if hf.Restitution > 1 {
		hf.Restitution = 1
	}
	w.heightfields = append(w.heightfields, hf)
	w.bp.addHeightfield(w, hf)
	return hf
}

// RemoveHeightfield removes the heightfield collider with the given ID from w.
func (w *World) RemoveHeightfield(id int) bool {
	for i, hf := range w.heightfields {
		if hf.ID == id {
			w.heightfields = append(w.heightfields[:i], w.heightfields[i+1:]...)
			w.bp.removeHeightfield(w, i)
			w.wakeAllDynamic()
			return true
		}
	}
	return false
}

// Heightfields returns all static HeightfieldColliders registered in w.
func (w *World) Heightfields() []*HeightfieldCollider {
	return w.heightfields
}

// colliderIDTaken reports whether a registered mesh or heightfield already uses id.
func (w *World) colliderIDTaken(id int) bool {
	for _, tm := range w.triMeshes {
		if tm.ID == id {
			return true
		}
	}
	for _, hf := range w.heightfields {
		if hf.ID == id {
			return true
		}
	}
	for _, vc := range w.voxels {
		if vc.ID == id {
			return true
		}
	}
	return false
}

func (w *World) wakeAllDynamic() {
	for _, b := range w.bodies {
		if !b.Static && !b.Kinematic {
			b.Wake()
		}
	}
}
