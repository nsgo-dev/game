package navbake3d

// contourVert is one corner of a region's outline in grid units: x and z in cells, y in cell heights, and reg the region across the edge that starts at it (0 for a wall).
type contourVert struct {
	x, y, z int32
	reg     int32
	// raw is the index of the raw outline point a simplified vertex was taken from.
	raw int
}

// contour is the simplified outline of one region.
type contour struct {
	verts []contourVert
	reg   int32
}

// buildContours traces the outline of every region and simplifies it: edges between two regions keep only the corners where the neighbouring region changes, so both sides agree, and wall edges are refined until no outline point strays more than maxError cells and no edge is longer than maxEdgeLen cells.
func (chf *compactHeightfield) buildContours(maxError float32, maxEdgeLen int32) []contour {
	flags := make([]uint8, len(chf.spans))
	for z := 0; z < chf.depth; z++ {
		for x := 0; x < chf.width; x++ {
			col := x + z*chf.width
			for i := chf.cellIndex[col]; i < chf.cellIndex[col]+chf.cellCount[col]; i++ {
				reg := chf.spans[i].reg
				if reg == 0 {
					continue
				}
				var same uint8
				for dir := range 4 {
					if a := chf.spans[i].con[dir]; a != notConnected && chf.spans[a].reg == reg {
						same |= 1 << dir
					}
				}
				flags[i] = same ^ 0xf
			}
		}
	}
	var out []contour
	var raw []contourVert
	for z := 0; z < chf.depth; z++ {
		for x := 0; x < chf.width; x++ {
			col := x + z*chf.width
			for i := chf.cellIndex[col]; i < chf.cellIndex[col]+chf.cellCount[col]; i++ {
				if flags[i] == 0 || flags[i] == 0xf {
					flags[i] = 0
					continue
				}
				reg := chf.spans[i].reg
				if reg == 0 {
					continue
				}
				raw = chf.walkContour(x, z, i, flags, raw[:0])
				simplified := simplifyContour(raw, maxError, maxEdgeLen)
				simplified = removeDegenerateSegments(simplified)
				if len(simplified) < 3 {
					continue
				}
				// A region outline winds one way and a hole inside it the other; monotone regions have no holes, so a reversed outline is a degenerate trace and is dropped.
				if contourArea2(simplified) <= 0 {
					continue
				}
				out = append(out, contour{verts: simplified, reg: reg})
			}
		}
	}
	return out
}

// walkContour follows the boundary of span i's region from its first boundary edge, turning along walls and stepping into same-region neighbours, and appends the corner of each boundary edge it passes.
func (chf *compactHeightfield) walkContour(x, z int, i int32, flags []uint8, pts []contourVert) []contourVert {
	dir := 0
	for flags[i]&(1<<dir) == 0 {
		dir++
	}
	startDir := dir
	starti := i
	for iter := 0; iter < 1<<20; iter++ {
		if flags[i]&(1<<dir) != 0 {
			px := int32(x)
			py := chf.cornerHeight(x, z, i, dir)
			pz := int32(z)
			switch dir {
			case 0:
				pz++
			case 1:
				px++
				pz++
			case 2:
				px++
			}
			r := int32(0)
			if a := chf.spans[i].con[dir]; a != notConnected {
				r = chf.spans[a].reg
			}
			pts = append(pts, contourVert{x: px, y: py, z: pz, reg: r})
			flags[i] &^= 1 << dir
			dir = (dir + 1) & 3
		} else {
			ni := chf.spans[i].con[dir]
			if ni == notConnected {
				return pts
			}
			x += dirOffsetX[dir]
			z += dirOffsetZ[dir]
			i = ni
			dir = (dir + 3) & 3
		}
		if i == starti && dir == startDir {
			break
		}
	}
	return pts
}

// cornerHeight returns the highest floor of the up to four spans that meet at the corner of span i ahead and to the side of direction dir, so every region meeting there gives the corner one height.
func (chf *compactHeightfield) cornerHeight(x, z int, i int32, dir int) int32 {
	s := chf.spans[i]
	h := s.y
	dirp := (dir + 1) & 3
	if a := s.con[dir]; a != notConnected {
		h = max(h, chf.spans[a].y)
		if a2 := chf.spans[a].con[dirp]; a2 != notConnected {
			h = max(h, chf.spans[a2].y)
		}
	}
	if a := s.con[dirp]; a != notConnected {
		h = max(h, chf.spans[a].y)
		if a2 := chf.spans[a].con[dir]; a2 != notConnected {
			h = max(h, chf.spans[a2].y)
		}
	}
	return h
}

// distPtSegSq returns the squared XZ distance from (x, z) to the segment from (px, pz) to (qx, qz).
func distPtSegSq(x, z, px, pz, qx, qz int32) float32 {
	pqx := float32(qx - px)
	pqz := float32(qz - pz)
	dx := float32(x - px)
	dz := float32(z - pz)
	d := float32(pqx*pqx) + float32(pqz*pqz)
	t := float32(pqx*dx) + float32(pqz*dz)
	if d > 0 {
		t /= d
	}
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	dx = float32(px) + float32(t*pqx) - float32(x)
	dz = float32(pz) + float32(t*pqz) - float32(z)
	return float32(dx*dx) + float32(dz*dz)
}

// simplifyContour reduces the raw outline pts to the corners where the neighbouring region changes plus the wall corners needed to stay within maxError cells of it, then splits wall edges longer than maxEdgeLen cells. Each vertex's reg is that of the edge that starts at it.
func simplifyContour(pts []contourVert, maxError float32, maxEdgeLen int32) []contourVert {
	pn := len(pts)
	var simp []contourVert
	hasConnections := false
	for _, p := range pts {
		if p.reg != 0 {
			hasConnections = true
			break
		}
	}
	if hasConnections {
		for i := range pn {
			ii := (i + 1) % pn
			if pts[i].reg != pts[ii].reg {
				v := pts[i]
				v.raw = i
				simp = append(simp, v)
			}
		}
	}
	if len(simp) == 0 {
		ll, ur := 0, 0
		for i, p := range pts {
			if p.x < pts[ll].x || (p.x == pts[ll].x && p.z < pts[ll].z) {
				ll = i
			}
			if p.x > pts[ur].x || (p.x == pts[ur].x && p.z > pts[ur].z) {
				ur = i
			}
		}
		a, b := pts[ll], pts[ur]
		a.raw, b.raw = ll, ur
		simp = append(simp, a, b)
	}
	insert := func(at int, v contourVert) {
		simp = append(simp, contourVert{})
		copy(simp[at+1:], simp[at:])
		simp[at] = v
	}
	maxErrSq := maxError * maxError
	for i := 0; i < len(simp); {
		ii := (i + 1) % len(simp)
		ax, az, ai := simp[i].x, simp[i].z, simp[i].raw
		bx, bz, bi := simp[ii].x, simp[ii].z, simp[ii].raw
		maxd := float32(0)
		maxi := -1
		var ci, cinc, endi int
		// Walk the segment in a fixed order so both sides of an edge measure the same deviation.
		if bx > ax || (bx == ax && bz > az) {
			cinc = 1
			ci = (ai + cinc) % pn
			endi = bi
		} else {
			cinc = pn - 1
			ci = (bi + cinc) % pn
			endi = ai
			ax, bx = bx, ax
			az, bz = bz, az
		}
		if pts[ci].reg == 0 {
			for ci != endi {
				d := distPtSegSq(pts[ci].x, pts[ci].z, ax, az, bx, bz)
				if d > maxd {
					maxd = d
					maxi = ci
				}
				ci = (ci + cinc) % pn
			}
		}
		if maxi != -1 && maxd > maxErrSq {
			v := pts[maxi]
			v.raw = maxi
			insert(i+1, v)
		} else {
			i++
		}
	}
	if maxEdgeLen > 0 {
		for i := 0; i < len(simp); {
			ii := (i + 1) % len(simp)
			ax, az, ai := simp[i].x, simp[i].z, simp[i].raw
			bx, bz, bi := simp[ii].x, simp[ii].z, simp[ii].raw
			maxi := -1
			ci := (ai + 1) % pn
			if pts[ci].reg == 0 {
				dx, dz := bx-ax, bz-az
				if int64(dx)*int64(dx)+int64(dz)*int64(dz) > int64(maxEdgeLen)*int64(maxEdgeLen) {
					n := bi - ai
					if bi < ai {
						n = bi + pn - ai
					}
					if n > 1 {
						if bx > ax || (bx == ax && bz > az) {
							maxi = (ai + n/2) % pn
						} else {
							maxi = (ai + (n+1)/2) % pn
						}
					}
				}
			}
			if maxi != -1 {
				v := pts[maxi]
				v.raw = maxi
				insert(i+1, v)
			} else {
				i++
			}
		}
	}
	// The region a vertex's edge leads along is that of the raw point after it.
	for i := range simp {
		simp[i].reg = pts[(simp[i].raw+1)%pn].reg
	}
	return simp
}

// removeDegenerateSegments drops vertices that repeat the previous one's position in XZ.
func removeDegenerateSegments(v []contourVert) []contourVert {
	for i := 0; i < len(v) && len(v) > 0; {
		ni := (i + 1) % len(v)
		if ni != i && v[i].x == v[ni].x && v[i].z == v[ni].z {
			v = append(v[:i], v[i+1:]...)
			continue
		}
		i++
	}
	return v
}

// contourArea2 returns twice the signed XZ area of the outline, positive for a region's outer outline as walkContour traces it.
func contourArea2(v []contourVert) int64 {
	var area int64
	n := len(v)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		area += int64(v[i].x)*int64(v[j].z) - int64(v[j].x)*int64(v[i].z)
	}
	return area
}
