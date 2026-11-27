package navbake

import (
	"cmp"
	"slices"
)

// convexPolygons merges the walkable triangles of r into convex polygons, greedily removing the longest shared edges first (Hertel-Mehlhorn), and returns them with collinear vertices removed, each counter-clockwise from its lowest vertex, sorted.
func (r *region) convexPolygons() [][]ipt {
	c := r.cdt
	owner := make([]int32, len(c.tris))
	polys := make(map[int32][]int32)
	type cand struct {
		t, s   int32
		u, w   int32
		length int64
	}
	var cands []cand
	for t, tr := range c.tris {
		owner[t] = int32(t)
		if !r.walk[t] {
			continue
		}
		polys[int32(t)] = []int32{tr.v[0], tr.v[1], tr.v[2]}
		for i := range 3 {
			s := tr.n[i]
			if s <= int32(t) || !r.walk[s] {
				continue
			}
			u, w := tr.v[next3[i]], tr.v[prev3[i]]
			pu, pw := c.pts[u], c.pts[w]
			dx, dy := pu.x-pw.x, pu.y-pw.y
			// Lengths fit: differences are below 2^31, so each square is below 2^62.
			l := dx*dx/2 + dy*dy/2
			cands = append(cands, cand{t: int32(t), s: s, u: u, w: w, length: l})
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int { return cmp.Compare(b.length, a.length) })
	find := func(t int32) int32 {
		for owner[t] != t {
			owner[t] = owner[owner[t]]
			t = owner[t]
		}
		return t
	}
	for _, e := range cands {
		p, q := find(e.t), find(e.s)
		if p == q {
			continue
		}
		merged, ok := mergeConvex(c.pts, polys[p], polys[q], e.u, e.w)
		if !ok {
			continue
		}
		owner[q] = p
		polys[p] = merged
		delete(polys, q)
	}
	out := make([][]ipt, 0, len(polys))
	for _, poly := range polys {
		pts := make([]ipt, 0, len(poly))
		for k, v := range poly {
			a, b := c.pts[poly[(k+len(poly)-1)%len(poly)]], c.pts[poly[(k+1)%len(poly)]]
			if orient(a, c.pts[v], b) != 0 {
				pts = append(pts, c.pts[v])
			}
		}
		lowest := 0
		for k, p := range pts {
			if p.less(pts[lowest]) {
				lowest = k
			}
		}
		out = append(out, append(pts[lowest:], pts[:lowest]...))
	}
	slices.SortFunc(out, comparePolys)
	return out
}

// comparePolys orders polygons by their vertices, compared in turn.
func comparePolys(a, b []ipt) int {
	for k := range min(len(a), len(b)) {
		if a[k] != b[k] {
			if a[k].less(b[k]) {
				return -1
			}
			return 1
		}
	}
	return cmp.Compare(len(a), len(b))
}

// mergeConvex joins the counter-clockwise convex polygons p and q, where p holds the edge u->w and q the edge w->u, across their whole shared boundary, and reports whether the result is convex.
func mergeConvex(pts []ipt, p, q []int32, u, w int32) ([]int32, bool) {
	np, nq := len(p), len(q)
	i := -1
	for k := range np {
		if p[k] == u && p[(k+1)%np] == w {
			i = k
			break
		}
	}
	j := -1
	for k := range nq {
		if q[k] == w && q[(k+1)%nq] == u {
			j = k
			break
		}
	}
	if i < 0 || j < 0 {
		return nil, false
	}
	at := func(s []int32, k int) int32 { return s[((k%len(s))+len(s))%len(s)] }
	// The shared chain runs from p[s0] (u's side) to p[s1] (w's side) in p, and back from q[t0] to q[t1] in q.
	s0, s1 := i, i+1
	t0, t1 := j, j+1
	for shared := 1; shared < min(np, nq)-1 && at(p, s1+1) == at(q, t0-1); shared++ {
		s1++
		t0--
	}
	for shared := s1 - s0; shared < min(np, nq)-1 && at(p, s0-1) == at(q, t1+1); shared++ {
		s0--
		t1++
	}
	start, end := at(p, s0), at(p, s1)
	if orient(pts[at(p, s0-1)], pts[start], pts[at(q, t1+1)]) < 0 || orient(pts[at(q, t0-1)], pts[end], pts[at(p, s1+1)]) < 0 {
		return nil, false
	}
	out := make([]int32, 0, np+nq)
	for k := s1; k <= s0+np; k++ {
		out = append(out, at(p, k))
	}
	for k := t1 + 1; k < t0+nq; k++ {
		out = append(out, at(q, k))
	}
	return out, true
}
