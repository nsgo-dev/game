package navbake3d

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// detailPoly is one polygon of a baked mesh with its height detail: the extra vertices sampled on and inside it and the triangles over its own vertices (indices from 0) and the extra ones (indices from its vertex count on).
type detailPoly struct {
	extra []math3d.Vec3
	tris  [][3]int
}

// detailBuilder samples the walkable surface's heights to add height detail to the polygons of a mesh.
type detailBuilder struct {
	chf       *compactHeightfield
	verts     []math3d.Vec3
	sampleDst float32
	maxErr    float32
	// maxDy is how far from the height it expects a sample may be found, half the agent's height.
	maxDy float32
	edges map[[2]int32][]math3d.Vec3
}

// world converts a mesh vertex in grid units to world space.
//
// Every product the bake adds to or subtracts from is wrapped in an explicit conversion, as here. The Go spec lets a compiler fuse x*y + z into one rounding (arm64 does, amd64 does not), and an explicit conversion is what forbids it, so the same level bakes to the same bytes on every architecture.
func (chf *compactHeightfield) world(v [3]int32) math3d.Vec3 {
	return math3d.Vec3{
		X: chf.bmin[0] + float32(float32(v[0])*chf.cs),
		Y: chf.bmin[1] + float32(float32(v[1])*chf.ch),
		Z: chf.bmin[2] + float32(float32(v[2])*chf.cs),
	}
}

// sampleHeight returns the floor height near (x, z) closest to refY and within maxDy of it: of a span of region reg (any region when reg is 0) within two cells when there is one, or else of any span within one cell. maxDy keeps a sample on its own level, never on a floor under a bridge or a roof over it.
func (chf *compactHeightfield) sampleHeight(x, z, refY float32, reg int32, maxDy float32) (float32, bool) {
	ix := int(math.Floor(float64((x - chf.bmin[0]) / chf.cs)))
	iz := int(math.Floor(float64((z - chf.bmin[2]) / chf.cs)))
	pass := func(rings int, accept func(i int32) bool) (float32, bool) {
		for r := 0; r <= rings; r++ {
			best, bestD := float32(0), maxDy
			found := false
			for dz := -r; dz <= r; dz++ {
				for dx := -r; dx <= r; dx++ {
					if max(absInt(dx), absInt(dz)) != r {
						continue
					}
					cx, cz := ix+dx, iz+dz
					if cx < 0 || cz < 0 || cx >= chf.width || cz >= chf.depth {
						continue
					}
					col := cx + cz*chf.width
					for i := chf.cellIndex[col]; i < chf.cellIndex[col]+chf.cellCount[col]; i++ {
						if !accept(i) {
							continue
						}
						y := chf.bmin[1] + float32(float32(chf.spans[i].y)*chf.ch)
						if d := float32(math.Abs(float64(y - refY))); d <= bestD && (!found || d < bestD) {
							best, bestD, found = y, d, true
						}
					}
				}
			}
			if found {
				return best, true
			}
		}
		return 0, false
	}
	if y, ok := pass(2, func(i int32) bool {
		r := chf.spans[i].reg
		return r != 0 && (reg == 0 || r == reg)
	}); ok {
		return y, true
	}
	return pass(1, func(int32) bool { return true })
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// edgeSamples returns the height samples strictly between mesh vertices a and b that the edge needs to stay within maxErr of the surface, ordered from the lower-numbered vertex to the higher, computed once per edge, from the region of the first polygon that asks, so both polygons on it share them.
func (db *detailBuilder) edgeSamples(a, b, reg int32) []math3d.Vec3 {
	k := [2]int32{min(a, b), max(a, b)}
	if s, ok := db.edges[k]; ok {
		return s
	}
	va, vb := db.verts[k[0]], db.verts[k[1]]
	dx, dz := vb.X-va.X, vb.Z-va.Z
	length := float32(math.Sqrt(float64(float32(dx*dx) + float32(dz*dz))))
	nn := int(length / db.sampleDst)
	nn = clampInt(nn, 1, maxEdgeSamples)
	pts := make([]math3d.Vec3, nn+1)
	for i := 0; i <= nn; i++ {
		t := float32(i) / float32(nn)
		p := math3d.Vec3{X: va.X + float32(dx*t), Y: va.Y + float32((vb.Y-va.Y)*t), Z: va.Z + float32(dz*t)}
		switch i {
		case 0:
			p = va
		case nn:
			p = vb
		default:
			if y, ok := db.chf.sampleHeight(p.X, p.Z, p.Y, reg, db.maxDy); ok {
				p.Y = y
			}
		}
		pts[i] = p
	}
	keep := []int{0, nn}
	for {
		added := false
		for j := 0; j+1 < len(keep); j++ {
			i0, i1 := keep[j], keep[j+1]
			maxd, maxi := float32(0), -1
			for i := i0 + 1; i < i1; i++ {
				t := float32(i-i0) / float32(i1-i0)
				y := pts[i0].Y + float32((pts[i1].Y-pts[i0].Y)*t)
				if d := float32(math.Abs(float64(pts[i].Y - y))); d > maxd {
					maxd, maxi = d, i
				}
			}
			if maxi >= 0 && maxd > db.maxErr {
				keep = append(keep, 0)
				copy(keep[j+2:], keep[j+1:])
				keep[j+1] = maxi
				added = true
				j++
			}
		}
		if !added {
			break
		}
	}
	var out []math3d.Vec3
	for _, i := range keep[1 : len(keep)-1] {
		out = append(out, pts[i])
	}
	db.edges[k] = out
	return out
}

// orientXZ returns twice the signed XZ area of (a, b, c): negative when the triangle winds counter-clockwise seen from above (+Y).
func orientXZ(a, b, c math3d.Vec3) float64 {
	return float64(float64(b.X-a.X)*float64(c.Z-a.Z)) - float64(float64(b.Z-a.Z)*float64(c.X-a.X))
}

// build returns the height detail of the mesh polygon whose vertex indices are poly and whose region is reg.
func (db *detailBuilder) build(poly []int32, reg int32) detailPoly {
	n := len(poly)
	local := make([]math3d.Vec3, 0, n*2)
	for _, vi := range poly {
		local = append(local, db.verts[vi])
	}
	var hull []int
	for i := range n {
		hull = append(hull, i)
		a, b := poly[i], poly[(i+1)%n]
		samples := db.edgeSamples(a, b, reg)
		if a < b {
			for _, s := range samples {
				hull = append(hull, len(local))
				local = append(local, s)
			}
		} else {
			for j := len(samples) - 1; j >= 0; j-- {
				hull = append(hull, len(local))
				local = append(local, samples[j])
			}
		}
	}
	var area float64
	for i := range hull {
		area += orientXZ(local[hull[0]], local[hull[i]], local[hull[(i+1)%len(hull)]])
	}
	if math.Abs(area) < 1e-9 {
		return detailPoly{}
	}
	if area > 0 {
		for i, j := 0, len(hull)-1; i < j; i, j = i+1, j-1 {
			hull[i], hull[j] = hull[j], hull[i]
		}
	}
	tris := clipConvexHull(local, hull)
	if len(tris) == 0 {
		return detailPoly{}
	}
	tris = db.addInteriorSamples(local, hull, tris, reg, &local)
	return detailPoly{extra: local[n:], tris: tris}
}

// clipConvexHull triangulates the convex outline hull, which winds counter-clockwise seen from above and may hold collinear points, by cutting the ear with the shortest diagonal among its strictly convex corners.
func clipConvexHull(v []math3d.Vec3, hull []int) [][3]int {
	h := append([]int(nil), hull...)
	var tris [][3]int
	for len(h) > 3 {
		best, bestLen := -1, float32(0)
		for i := range h {
			p, c, nx := h[(i+len(h)-1)%len(h)], h[i], h[(i+1)%len(h)]
			if orientXZ(v[p], v[c], v[nx]) >= -1e-9 {
				continue
			}
			if l := lenSq(v[p].Sub(v[nx])); best < 0 || l < bestLen {
				best, bestLen = i, l
			}
		}
		if best < 0 {
			return tris
		}
		p, c, nx := h[(best+len(h)-1)%len(h)], h[best], h[(best+1)%len(h)]
		tris = append(tris, [3]int{p, c, nx})
		h = append(h[:best], h[best+1:]...)
	}
	if orientXZ(v[h[0]], v[h[1]], v[h[2]]) < -1e-9 {
		tris = append(tris, [3]int{h[0], h[1], h[2]})
	}
	return tris
}

// triWeights returns the barycentric weights of (x, z) in the XZ projection of triangle tri of v.
func triWeights(v []math3d.Vec3, tri [3]int, x, z float32) [3]float64 {
	p := math3d.Vec3{X: x, Z: z}
	a, b, c := v[tri[0]], v[tri[1]], v[tri[2]]
	d := orientXZ(a, b, c)
	return [3]float64{orientXZ(p, b, c) / d, orientXZ(a, p, c) / d, orientXZ(a, b, p) / d}
}

const baryEps = 1e-6

// heightAt returns the height of the triangulation at (x, z) and the triangle holding it, or -1.
func heightAt(v []math3d.Vec3, tris [][3]int, x, z float32) (float32, int) {
	for ti, t := range tris {
		w := triWeights(v, t, x, z)
		if w[0] >= -baryEps && w[1] >= -baryEps && w[2] >= -baryEps {
			y := float64(w[0]*float64(v[t[0]].Y)) + float64(w[1]*float64(v[t[1]].Y)) + float64(w[2]*float64(v[t[2]].Y))
			return float32(y), ti
		}
	}
	return 0, -1
}

// addInteriorSamples adds height samples on a grid inside the polygon, the worst-fitting first, until every sample lies within maxErr of the triangulation or the polygon holds maxDetailVerts extra vertices, keeping the triangulation Delaunay.
func (db *detailBuilder) addInteriorSamples(local []math3d.Vec3, hull []int, tris [][3]int, reg int32, out *[]math3d.Vec3) [][3]int {
	minX, maxX := local[hull[0]].X, local[hull[0]].X
	minZ, maxZ := local[hull[0]].Z, local[hull[0]].Z
	for _, i := range hull {
		minX, maxX = min(minX, local[i].X), max(maxX, local[i].X)
		minZ, maxZ = min(minZ, local[i].Z), max(maxZ, local[i].Z)
	}
	sd := db.sampleDst
	inset := sd * 0.5
	var samples []math3d.Vec3
	x0 := int(math.Ceil(float64(minX / sd)))
	x1 := int(math.Floor(float64(maxX / sd)))
	z0 := int(math.Ceil(float64(minZ / sd)))
	z1 := int(math.Floor(float64(maxZ / sd)))
	for iz := z0; iz <= z1; iz++ {
		for ix := x0; ix <= x1; ix++ {
			x, z := float32(float32(ix)*sd), float32(float32(iz)*sd)
			if distInsideHull(local, hull, x, z) < inset {
				continue
			}
			ref, ti := heightAt(local, tris, x, z)
			if ti < 0 {
				continue
			}
			y, ok := db.chf.sampleHeight(x, z, ref, reg, db.maxDy)
			if !ok {
				continue
			}
			samples = append(samples, math3d.Vec3{X: x, Y: y, Z: z})
		}
	}
	used := make([]bool, len(samples))
	for added := 0; added < maxDetailVerts; added++ {
		maxd, maxi := float32(0), -1
		for si, s := range samples {
			if used[si] {
				continue
			}
			y, ti := heightAt(local, tris, s.X, s.Z)
			if ti < 0 {
				continue
			}
			if d := float32(math.Abs(float64(s.Y - y))); d > maxd {
				maxd, maxi = d, si
			}
		}
		if maxi < 0 || maxd <= db.maxErr {
			break
		}
		used[maxi] = true
		local = append(local, samples[maxi])
		tris = insertPoint(local, tris, len(local)-1)
	}
	*out = local
	return tris
}

// distInsideHull returns how far (x, z) lies inside the counter-clockwise convex outline hull, negative outside.
func distInsideHull(v []math3d.Vec3, hull []int, x, z float32) float32 {
	d := float32(math.Inf(1))
	for i := range hull {
		a, b := v[hull[i]], v[hull[(i+1)%len(hull)]]
		ex, ez := b.X-a.X, b.Z-a.Z
		l := float32(math.Sqrt(float64(float32(ex*ex) + float32(ez*ez))))
		if l == 0 {
			continue
		}
		// Inside a counter-clockwise (seen from above) outline lies on the side where orientXZ(a, b, p) is negative.
		s := -float32(orientXZ(a, b, math3d.Vec3{X: x, Z: z})) / l
		d = min(d, s)
	}
	return d
}

// insertPoint adds vertex p to the triangulation, splitting the triangle that holds it into three, or the two triangles beside the edge it lies on into four, and flips edges until the triangulation is Delaunay again. Outline edges, which only one triangle holds, are never flipped.
func insertPoint(v []math3d.Vec3, tris [][3]int, p int) [][3]int {
	var stack [][2]int
	for ti, t := range tris {
		w := triWeights(v, t, v[p].X, v[p].Z)
		if w[0] < -baryEps || w[1] < -baryEps || w[2] < -baryEps {
			continue
		}
		onEdge := -1
		for k := range 3 {
			if w[k] < baryEps {
				onEdge = k
			}
		}
		if onEdge < 0 {
			a, b, c := t[0], t[1], t[2]
			tris[ti] = [3]int{a, b, p}
			tris = append(tris, [3]int{b, c, p}, [3]int{c, a, p})
			stack = append(stack, [2]int{a, b}, [2]int{b, c}, [2]int{c, a})
			break
		}
		// The point lies on the edge opposite vertex onEdge.
		a, b, c := t[onEdge], t[(onEdge+1)%3], t[(onEdge+2)%3]
		tris[ti] = [3]int{a, b, p}
		tris = append(tris, [3]int{c, a, p})
		stack = append(stack, [2]int{a, b}, [2]int{c, a})
		if oi, ok := findEdgeTri(tris, c, b); ok {
			o := tris[oi]
			d := thirdVertex(o, c, b)
			tris[oi] = [3]int{d, c, p}
			tris = append(tris, [3]int{b, d, p})
			stack = append(stack, [2]int{d, c}, [2]int{b, d})
		}
		break
	}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		a, b := e[0], e[1]
		ti, ok := findEdgeTri(tris, a, b)
		if !ok {
			continue
		}
		oi, ok := findEdgeTri(tris, b, a)
		if !ok {
			continue
		}
		pp := thirdVertex(tris[ti], a, b)
		q := thirdVertex(tris[oi], b, a)
		if !inCircumcircle(v[a], v[b], v[pp], v[q]) {
			continue
		}
		// Flip only when the quad (a, q, b, pp) is strictly convex, so both new triangles keep their winding.
		if orientXZ(v[pp], v[a], v[q]) >= -1e-12 || orientXZ(v[pp], v[q], v[b]) >= -1e-12 {
			continue
		}
		tris[ti] = [3]int{pp, a, q}
		tris[oi] = [3]int{pp, q, b}
		stack = append(stack, [2]int{a, q}, [2]int{q, b})
	}
	return tris
}

// findEdgeTri returns the triangle that holds the directed edge from a to b.
func findEdgeTri(tris [][3]int, a, b int) (int, bool) {
	for i, t := range tris {
		for k := range 3 {
			if t[k] == a && t[(k+1)%3] == b {
				return i, true
			}
		}
	}
	return -1, false
}

// thirdVertex returns the vertex of t that is neither a nor b.
func thirdVertex(t [3]int, a, b int) int {
	for _, x := range t {
		if x != a && x != b {
			return x
		}
	}
	return t[0]
}

// inCircumcircle reports whether d lies strictly inside the XZ circumcircle of the counter-clockwise (seen from above) triangle (a, b, c).
func inCircumcircle(a, b, c, d math3d.Vec3) bool {
	adx, adz := float64(a.X-d.X), float64(a.Z-d.Z)
	bdx, bdz := float64(b.X-d.X), float64(b.Z-d.Z)
	cdx, cdz := float64(c.X-d.X), float64(c.Z-d.Z)
	m := func(a, b float64) float64 { return float64(a * b) }
	det := m(m(adx, adx)+m(adz, adz), m(bdx, cdz)-m(cdx, bdz)) - m(m(bdx, bdx)+m(bdz, bdz), m(adx, cdz)-m(cdx, adz)) + m(m(cdx, cdx)+m(cdz, cdz), m(adx, bdz)-m(bdx, adz))
	// orientXZ is negative for these triangles, so the usual sign of the test flips.
	return det < -1e-12
}
