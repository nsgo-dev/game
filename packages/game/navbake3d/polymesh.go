package navbake3d

// polyMesh is the convex polygon mesh of the walkable area in grid units: vertices shared between polygons, and each polygon's vertex indices in order.
type polyMesh struct {
	verts [][3]int32 // x and z in cells, y in cell heights
	polys [][]int32
	regs  []int32
}

// vertKey finds a vertex by its XZ cell so outlines of neighbouring regions share their corners.
type vertKey struct{ x, z int32 }

// addVertex returns the index of the mesh vertex at (x, y, z), reusing one in the same cell within two cell heights.
func (pm *polyMesh) addVertex(x, y, z int32, index map[vertKey][]int32) int32 {
	k := vertKey{x, z}
	for _, i := range index[k] {
		if abs32(pm.verts[i][1]-y) <= 2 {
			return i
		}
	}
	i := int32(len(pm.verts))
	pm.verts = append(pm.verts, [3]int32{x, y, z})
	index[k] = append(index[k], i)
	return i
}

// buildPolyMesh triangulates each region outline and merges its triangles into convex polygons of at most nvp vertices, preferring to merge across the longest shared edge.
func buildPolyMesh(contours []contour, nvp int) *polyMesh {
	pm := &polyMesh{}
	index := make(map[vertKey][]int32)
	var tris []int32
	var indices []int
	for ci := range contours {
		c := &contours[ci]
		n := len(c.verts)
		indices = indices[:0]
		for i := range n {
			indices = append(indices, i)
		}
		tris = triangulate(c.verts, indices, tris[:0])
		if len(tris) == 0 {
			continue
		}
		vi := make([]int32, n)
		for i, v := range c.verts {
			vi[i] = pm.addVertex(v.x, v.y, v.z, index)
		}
		var polys [][]int32
		for t := 0; t+2 < len(tris); t += 3 {
			a, b, cc := vi[tris[t]], vi[tris[t+1]], vi[tris[t+2]]
			if a == b || b == cc || a == cc {
				continue
			}
			polys = append(polys, []int32{a, b, cc})
		}
		if nvp > 3 {
			for {
				bestVal, bestA, bestB, bestEA, bestEB := 0, 0, 0, 0, 0
				for j := 0; j < len(polys)-1; j++ {
					for k := j + 1; k < len(polys); k++ {
						v, ea, eb := pm.mergeValue(polys[j], polys[k], nvp)
						if v > bestVal {
							bestVal, bestA, bestB, bestEA, bestEB = v, j, k, ea, eb
						}
					}
				}
				if bestVal <= 0 {
					break
				}
				polys[bestA] = mergePolys(polys[bestA], polys[bestB], bestEA, bestEB)
				polys[bestB] = polys[len(polys)-1]
				polys = polys[:len(polys)-1]
			}
		}
		for _, p := range polys {
			pm.polys = append(pm.polys, p)
			pm.regs = append(pm.regs, c.reg)
		}
	}
	return pm
}

// uleft reports whether c lies strictly to the left of the line from a to b in the grid's XZ orientation.
func uleft(a, b, c [3]int32) bool {
	return int64(b[0]-a[0])*int64(c[2]-a[2])-int64(c[0]-a[0])*int64(b[2]-a[2]) < 0
}

// mergeValue returns the squared length of the edge pa and pb share when merging them across it keeps the result convex and within nvp vertices, with the edge's position in each; otherwise -1.
func (pm *polyMesh) mergeValue(pa, pb []int32, nvp int) (int, int, int) {
	na, nb := len(pa), len(pb)
	if na+nb-2 > nvp {
		return -1, 0, 0
	}
	ea, eb := -1, -1
	for i := 0; i < na && ea < 0; i++ {
		va0, va1 := pa[i], pa[(i+1)%na]
		if va0 > va1 {
			va0, va1 = va1, va0
		}
		for j := range nb {
			vb0, vb1 := pb[j], pb[(j+1)%nb]
			if vb0 > vb1 {
				vb0, vb1 = vb1, vb0
			}
			if va0 == vb0 && va1 == vb1 {
				ea, eb = i, j
				break
			}
		}
	}
	if ea < 0 {
		return -1, 0, 0
	}
	v := pm.verts
	if !uleft(v[pa[(ea+na-1)%na]], v[pa[ea]], v[pb[(eb+2)%nb]]) {
		return -1, 0, 0
	}
	if !uleft(v[pb[(eb+nb-1)%nb]], v[pb[eb]], v[pa[(ea+2)%na]]) {
		return -1, 0, 0
	}
	a, b := v[pa[ea]], v[pa[(ea+1)%na]]
	dx := int(a[0] - b[0])
	dz := int(a[2] - b[2])
	return dx*dx + dz*dz, ea, eb
}

// mergePolys joins pa and pb across their shared edge, pa's edge ea and pb's edge eb.
func mergePolys(pa, pb []int32, ea, eb int) []int32 {
	na, nb := len(pa), len(pb)
	out := make([]int32, 0, na+nb-2)
	for i := 0; i < na-1; i++ {
		out = append(out, pa[(ea+1+i)%na])
	}
	for i := 0; i < nb-1; i++ {
		out = append(out, pb[(eb+1+i)%nb])
	}
	return out
}

// The triangulation helpers work on outline vertices in the XZ plane, as Recast's do.

func area2(a, b, c contourVert) int64 {
	return int64(b.x-a.x)*int64(c.z-a.z) - int64(c.x-a.x)*int64(b.z-a.z)
}

func left(a, b, c contourVert) bool      { return area2(a, b, c) < 0 }
func leftOn(a, b, c contourVert) bool    { return area2(a, b, c) <= 0 }
func collinear(a, b, c contourVert) bool { return area2(a, b, c) == 0 }

func intersectProp(a, b, c, d contourVert) bool {
	if collinear(a, b, c) || collinear(a, b, d) || collinear(c, d, a) || collinear(c, d, b) {
		return false
	}
	return (left(a, b, c) != left(a, b, d)) && (left(c, d, a) != left(c, d, b))
}

func between(a, b, c contourVert) bool {
	if !collinear(a, b, c) {
		return false
	}
	if a.x != b.x {
		return (a.x <= c.x && c.x <= b.x) || (a.x >= c.x && c.x >= b.x)
	}
	return (a.z <= c.z && c.z <= b.z) || (a.z >= c.z && c.z >= b.z)
}

func intersectSeg(a, b, c, d contourVert) bool {
	if intersectProp(a, b, c, d) {
		return true
	}
	return between(a, b, c) || between(a, b, d) || between(c, d, a) || between(c, d, b)
}

func sameXZ(a, b contourVert) bool { return a.x == b.x && a.z == b.z }

// diagonalie reports whether the segment between outline positions i and j crosses no edge of the remaining outline.
func diagonalie(i, j int, verts []contourVert, idx []int, loose bool) bool {
	n := len(idx)
	d0, d1 := verts[idx[i]], verts[idx[j]]
	for k := range n {
		k1 := (k + 1) % n
		if k == i || k1 == i || k == j || k1 == j {
			continue
		}
		p0, p1 := verts[idx[k]], verts[idx[k1]]
		if sameXZ(d0, p0) || sameXZ(d1, p0) || sameXZ(d0, p1) || sameXZ(d1, p1) {
			continue
		}
		if loose {
			if intersectProp(d0, d1, p0, p1) {
				return false
			}
		} else if intersectSeg(d0, d1, p0, p1) {
			return false
		}
	}
	return true
}

// inCone reports whether the diagonal from position i to j leaves i into the polygon's interior.
func inCone(i, j int, verts []contourVert, idx []int, loose bool) bool {
	n := len(idx)
	pi, pj := verts[idx[i]], verts[idx[j]]
	pi1 := verts[idx[(i+1)%n]]
	pin1 := verts[idx[(i+n-1)%n]]
	if leftOn(pin1, pi, pi1) {
		if loose {
			return leftOn(pi, pj, pin1) && leftOn(pj, pi, pi1)
		}
		return left(pi, pj, pin1) && left(pj, pi, pi1)
	}
	return !(leftOn(pi, pj, pi1) && leftOn(pj, pi, pin1))
}

func diagonal(i, j int, verts []contourVert, idx []int, loose bool) bool {
	return inCone(i, j, verts, idx, loose) && diagonalie(i, j, verts, idx, loose)
}

// triangulate ear-clips the outline whose vertex indices are idx, always cutting the ear with the shortest diagonal, and appends the triangles as outline vertex indices to tris. A badly self-touching outline returns the triangles cut so far.
func triangulate(verts []contourVert, idx []int, tris []int32) []int32 {
	n := len(idx)
	ear := make([]bool, n)
	for i := range n {
		i2 := (i + 2) % n
		ear[(i+1)%n] = diagonal(i, i2, verts, idx, false)
	}
	for n > 3 {
		minLen, mini := int64(-1), -1
		for i := range n {
			i1 := (i + 1) % n
			if ear[i1] {
				p0 := verts[idx[i]]
				p2 := verts[idx[(i1+1)%n]]
				dx, dz := int64(p2.x-p0.x), int64(p2.z-p0.z)
				if l := dx*dx + dz*dz; minLen < 0 || l < minLen {
					minLen, mini = l, i
				}
			}
		}
		if mini == -1 {
			// Overlapping outline segments can hide every ear; a looser test lets the clipping go on.
			for i := range n {
				i1 := (i + 1) % n
				i2 := (i1 + 1) % n
				if diagonal(i, i2, verts, idx, true) {
					p0 := verts[idx[i]]
					p2 := verts[idx[(i2+1)%n]]
					dx, dz := int64(p2.x-p0.x), int64(p2.z-p0.z)
					if l := dx*dx + dz*dz; minLen < 0 || l < minLen {
						minLen, mini = l, i
					}
				}
			}
			if mini == -1 {
				return tris
			}
		}
		i := mini
		i1 := (i + 1) % n
		i2 := (i1 + 1) % n
		tris = append(tris, int32(idx[i]), int32(idx[i1]), int32(idx[i2]))
		n--
		copy(idx[i1:], idx[i1+1:n+1])
		copy(ear[i1:], ear[i1+1:n+1])
		idx = idx[:n]
		ear = ear[:n]
		if i1 >= n {
			i1 = 0
		}
		i = (i1 + n - 1) % n
		ear[i] = diagonal((i+n-1)%n, i1, verts, idx, false)
		ear[i1] = diagonal(i, (i1+1)%n, verts, idx, false)
	}
	return append(tris, int32(idx[0]), int32(idx[1]), int32(idx[2]))
}
