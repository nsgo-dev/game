package navbake

import (
	"errors"
	"fmt"
)

// tri is a counter-clockwise triangle of a cdt. Edge i is the edge opposite v[i], from v[(i+1)%3] to v[(i+2)%3].
type tri struct {
	v [3]int32
	// n is the triangle across edge i, or -1 on the hull.
	n [3]int32
	// c marks edge i as a constraint.
	c [3]bool
}

// cdt is a constrained Delaunay triangulation of points inside a rectangle whose four corners are its first vertices.
type cdt struct {
	pts  []ipt
	tris []tri
	// vt holds a triangle touching each vertex.
	vt []int32
	// hint is the triangle point location starts from.
	hint int32
}

var next3 = [3]int{1, 2, 0}
var prev3 = [3]int{2, 0, 1}

// errTriangulation reports a broken invariant of the triangulation, which exact predicates rule out on input that arrange produced.
var errTriangulation = errors.New("navbake: internal triangulation error")

// newCDT returns the triangulation of the rectangle lo-hi, split along its diagonal.
func newCDT(lo, hi ipt, capacity int) *cdt {
	c := &cdt{
		pts:  make([]ipt, 0, capacity+4),
		tris: make([]tri, 0, 2*capacity+2),
		vt:   make([]int32, 0, capacity+4),
	}
	c.pts = append(c.pts, lo, ipt{hi.x, lo.y}, hi, ipt{lo.x, hi.y})
	c.tris = append(c.tris,
		tri{v: [3]int32{0, 1, 2}, n: [3]int32{-1, 1, -1}},
		tri{v: [3]int32{0, 2, 3}, n: [3]int32{-1, -1, 0}},
	)
	c.vt = append(c.vt, 0, 0, 0, 1)
	return c
}

// index returns the slot of vertex v in triangle t.
func (c *cdt) index(t, v int32) int {
	tr := &c.tris[t]
	switch v {
	case tr.v[0]:
		return 0
	case tr.v[1]:
		return 1
	}
	return 2
}

// across returns the slot of t in its neighbour u.
func (c *cdt) across(u, t int32) int {
	tr := &c.tris[u]
	switch t {
	case tr.n[0]:
		return 0
	case tr.n[1]:
		return 1
	}
	return 2
}

// relink makes triangle x, if any, point at nu where it pointed at old.
func (c *cdt) relink(x, old, nu int32) {
	if x < 0 {
		return
	}
	tr := &c.tris[x]
	for k := range 3 {
		if tr.n[k] == old {
			tr.n[k] = nu
			return
		}
	}
}

func (c *cdt) touch(t int32) {
	for _, v := range c.tris[t].v {
		c.vt[v] = t
	}
}

// locate returns the triangle containing p, walking from the hint; p must lie strictly inside the rectangle.
func (c *cdt) locate(p ipt) int32 {
	t := c.hint
	limit := len(c.tris) + 16
	for step := 0; step < limit; step++ {
		tr := &c.tris[t]
		moved := false
		for k := range 3 {
			i := (k + step) % 3
			if orient(c.pts[tr.v[next3[i]]], c.pts[tr.v[prev3[i]]], p) < 0 {
				t = tr.n[i]
				moved = true
				break
			}
		}
		if !moved {
			return t
		}
	}
	// The walk only cycles on a broken triangulation; scan instead.
	for t := range c.tris {
		tr := &c.tris[t]
		if orient(c.pts[tr.v[0]], c.pts[tr.v[1]], p) >= 0 && orient(c.pts[tr.v[1]], c.pts[tr.v[2]], p) >= 0 && orient(c.pts[tr.v[2]], c.pts[tr.v[0]], p) >= 0 {
			return int32(t)
		}
	}
	return 0
}

// insert adds point p, which must be new and strictly inside the rectangle, and restores the Delaunay property around it.
func (c *cdt) insert(p ipt) {
	id := int32(len(c.pts))
	c.pts = append(c.pts, p)
	c.vt = append(c.vt, -1)
	t := c.locate(p)
	tr := c.tris[t]
	edge := -1
	for i := range 3 {
		if orient(c.pts[tr.v[next3[i]]], c.pts[tr.v[prev3[i]]], p) == 0 {
			edge = i
			break
		}
	}
	var stack [][2]int32
	if edge < 0 {
		stack = c.split3(t, id)
	} else {
		stack = c.splitEdge(t, edge, id)
	}
	c.hint = t
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		t, i := e[0], int(e[1])
		tr := &c.tris[t]
		u := tr.n[i]
		if u < 0 || tr.c[i] {
			continue
		}
		w := c.tris[u].v[c.across(u, t)]
		if !incircle(c.pts[tr.v[0]], c.pts[tr.v[1]], c.pts[tr.v[2]], c.pts[w]) {
			continue
		}
		c.flip(t, i)
		// After the flip t is (p, v1, w) and u is (w, v2, p): the edges opposite p are new candidates.
		stack = append(stack, [2]int32{t, 0}, [2]int32{u, 2})
	}
}

// split3 splits triangle t at interior point p and returns the edges opposite p, each as {triangle, slot}.
func (c *cdt) split3(t, p int32) [][2]int32 {
	o := c.tris[t]
	a, b, v := o.v[0], o.v[1], o.v[2]
	t1, t2 := int32(len(c.tris)), int32(len(c.tris)+1)
	c.tris[t] = tri{v: [3]int32{a, b, p}, n: [3]int32{t1, t2, o.n[2]}, c: [3]bool{false, false, o.c[2]}}
	c.tris = append(c.tris,
		tri{v: [3]int32{b, v, p}, n: [3]int32{t2, t, o.n[0]}, c: [3]bool{false, false, o.c[0]}},
		tri{v: [3]int32{v, a, p}, n: [3]int32{t, t1, o.n[1]}, c: [3]bool{false, false, o.c[1]}},
	)
	c.relink(o.n[0], t, t1)
	c.relink(o.n[1], t, t2)
	c.touch(t)
	c.touch(t1)
	c.touch(t2)
	return [][2]int32{{t, 2}, {t1, 2}, {t2, 2}}
}

// splitEdge splits edge e of triangle t, and the triangle across it, at point p on that edge, and returns the edges opposite p.
func (c *cdt) splitEdge(t int32, e int, p int32) [][2]int32 {
	o := c.tris[t]
	cv, a, b := o.v[e], o.v[next3[e]], o.v[prev3[e]]
	ncb, nca := o.n[next3[e]], o.n[prev3[e]]
	ccb, cca := o.c[next3[e]], o.c[prev3[e]]
	split := o.c[e]
	u := o.n[e]
	t1 := int32(len(c.tris))
	if u < 0 {
		c.tris[t] = tri{v: [3]int32{cv, a, p}, n: [3]int32{-1, t1, nca}, c: [3]bool{split, false, cca}}
		c.tris = append(c.tris, tri{v: [3]int32{cv, p, b}, n: [3]int32{-1, ncb, t}, c: [3]bool{split, ccb, false}})
		c.relink(ncb, t, t1)
		c.touch(t)
		c.touch(t1)
		return [][2]int32{{t, 2}, {t1, 1}}
	}
	ou := c.tris[u]
	j := c.across(u, t)
	d := ou.v[j]
	nad, ndb := ou.n[next3[j]], ou.n[prev3[j]]
	cad, cdb := ou.c[next3[j]], ou.c[prev3[j]]
	u1 := t1 + 1
	// t: (c, a, p), t1: (c, p, b), u: (d, b, p), u1: (d, p, a).
	c.tris[t] = tri{v: [3]int32{cv, a, p}, n: [3]int32{u1, t1, nca}, c: [3]bool{split, false, cca}}
	c.tris[u] = tri{v: [3]int32{d, b, p}, n: [3]int32{t1, u1, ndb}, c: [3]bool{split, false, cdb}}
	c.tris = append(c.tris,
		tri{v: [3]int32{cv, p, b}, n: [3]int32{u, ncb, t}, c: [3]bool{split, ccb, false}},
		tri{v: [3]int32{d, p, a}, n: [3]int32{t, nad, u}, c: [3]bool{split, cad, false}},
	)
	c.relink(ncb, t, t1)
	c.relink(nad, u, u1)
	c.touch(t)
	c.touch(u)
	c.touch(t1)
	c.touch(u1)
	return [][2]int32{{t, 2}, {t1, 1}, {u, 2}, {u1, 1}}
}

// flip replaces edge i of triangle t, shared with its neighbour u, by the other diagonal of their quadrilateral. With v0, v1, v2 = t's vertices from slot i and w = u's far vertex, t becomes (v0, v1, w) and u becomes (w, v2, v0).
func (c *cdt) flip(t int32, i int) {
	ot := c.tris[t]
	u := ot.n[i]
	ou := c.tris[u]
	j := c.across(u, t)
	v0, v1, v2 := ot.v[i], ot.v[next3[i]], ot.v[prev3[i]]
	w := ou.v[j]
	nu1, cu1 := ou.n[next3[j]], ou.c[next3[j]]
	nu2, cu2 := ou.n[prev3[j]], ou.c[prev3[j]]
	nt1, ct1 := ot.n[next3[i]], ot.c[next3[i]]
	nt2, ct2 := ot.n[prev3[i]], ot.c[prev3[i]]
	c.tris[t] = tri{v: [3]int32{v0, v1, w}, n: [3]int32{nu1, u, nt2}, c: [3]bool{cu1, false, ct2}}
	c.tris[u] = tri{v: [3]int32{w, v2, v0}, n: [3]int32{nt1, t, nu2}, c: [3]bool{ct1, false, cu2}}
	c.relink(nu1, u, t)
	c.relink(nt1, t, u)
	c.touch(t)
	c.touch(u)
}

// findEdge returns the triangle holding the directed edge x->y and the edge's slot, or -1 when x and y are not joined.
func (c *cdt) findEdge(x, y int32) (int32, int) {
	start := c.vt[x]
	t := start
	for {
		k := c.index(t, x)
		tr := &c.tris[t]
		if tr.v[next3[k]] == y {
			return t, prev3[k]
		}
		t = tr.n[next3[k]]
		if t < 0 || t == start {
			break
		}
	}
	if t == start {
		return -1, -1
	}
	// x is on the hull: turn the other way from start.
	t = start
	for {
		k := c.index(t, x)
		tr := &c.tris[t]
		if tr.v[next3[k]] == y {
			return t, prev3[k]
		}
		t = tr.n[prev3[k]]
		if t < 0 {
			return -1, -1
		}
	}
}

// constrain marks edge i of triangle t, on both sides, as a constraint.
func (c *cdt) constrain(t int32, i int) {
	c.tris[t].c[i] = true
	if u := c.tris[t].n[i]; u >= 0 {
		c.tris[u].c[c.across(u, t)] = true
	}
}

// crosses reports whether segments (a, b) and (p, q) cross at a point inside both.
func crosses(a, b, p, q ipt) bool {
	return sign(orient(a, b, p))*sign(orient(a, b, q)) < 0 && sign(orient(p, q, a))*sign(orient(p, q, b)) < 0
}

// insertConstraint makes the segment between vertices a and b an edge of the triangulation and marks it a constraint. No vertex may lie inside the segment and it may cross no other constraint, as arrange guarantees.
func (c *cdt) insertConstraint(a, b int32) error {
	if t, i := c.findEdge(a, b); t >= 0 {
		c.constrain(t, i)
		return nil
	}
	pa, pb := c.pts[a], c.pts[b]
	// Find the triangle at a whose angle holds the direction to b; its far edge is the first one the segment crosses.
	var x, y int32 = -1, -1
	start := c.vt[a]
	t := start
	for {
		k := c.index(t, a)
		tr := &c.tris[t]
		vx, vy := tr.v[next3[k]], tr.v[prev3[k]]
		if orient(pa, c.pts[vx], pb) > 0 && orient(pa, c.pts[vy], pb) < 0 {
			x, y = vx, vy
			break
		}
		t = tr.n[next3[k]]
		if t < 0 || t == start {
			break
		}
	}
	if x < 0 {
		return fmt.Errorf("%w: no triangle at a vertex faces a constraint", errTriangulation)
	}
	// Walk along the segment, collecting the edges it crosses, each with x on its right and y on its left.
	var cross [][2]int32
	for {
		cross = append(cross, [2]int32{x, y})
		u, i := c.findEdge(y, x)
		if u < 0 || c.tris[u].c[i] {
			return fmt.Errorf("%w: a constraint crosses another", errTriangulation)
		}
		w := c.tris[u].v[i]
		if w == b {
			break
		}
		switch o := orient(pa, pb, c.pts[w]); {
		case o < 0:
			x = w
		case o > 0:
			y = w
		default:
			return fmt.Errorf("%w: a vertex lies on a constraint", errTriangulation)
		}
	}
	// Flip the crossed edges away (Sloan's algorithm): flipping a convex quadrilateral's diagonal either clears the segment or leaves an edge that still crosses it, to be flipped again.
	var fresh [][2]int32
	limit := 64*len(cross)*len(cross) + 1024
	for len(cross) > 0 {
		if limit--; limit < 0 {
			return fmt.Errorf("%w: constraint insertion did not finish", errTriangulation)
		}
		e := cross[0]
		cross = cross[1:]
		t, i := c.findEdge(e[0], e[1])
		if t < 0 {
			return fmt.Errorf("%w: a crossed edge vanished", errTriangulation)
		}
		u := c.tris[t].n[i]
		p := c.tris[t].v[i]
		q := c.tris[u].v[c.across(u, t)]
		pp, pq := c.pts[p], c.pts[q]
		if orient(pp, c.pts[e[0]], pq) <= 0 || orient(pq, c.pts[e[1]], pp) <= 0 {
			cross = append(cross, e)
			continue
		}
		c.flip(t, i)
		if p != a && p != b && q != a && q != b && crosses(pa, pb, pp, pq) {
			cross = append(cross, [2]int32{p, q})
		} else {
			fresh = append(fresh, [2]int32{p, q})
		}
	}
	t, i := c.findEdge(a, b)
	if t < 0 {
		return fmt.Errorf("%w: a constraint was not recovered", errTriangulation)
	}
	c.constrain(t, i)
	// Restore the Delaunay property among the new edges.
	for changed := true; changed; {
		changed = false
		for k, e := range fresh {
			if (e[0] == a && e[1] == b) || (e[0] == b && e[1] == a) {
				continue
			}
			t, i := c.findEdge(e[0], e[1])
			if t < 0 || c.tris[t].c[i] {
				continue
			}
			u := c.tris[t].n[i]
			if u < 0 {
				continue
			}
			tr := &c.tris[t]
			w := c.tris[u].v[c.across(u, t)]
			if !incircle(c.pts[tr.v[0]], c.pts[tr.v[1]], c.pts[tr.v[2]], c.pts[w]) {
				continue
			}
			p := tr.v[i]
			c.flip(t, i)
			fresh[k] = [2]int32{p, w}
			changed = true
		}
	}
	return nil
}
