package navbake

import (
	"cmp"
	"errors"
	"math"
	"slices"
)

// counts are the two region counters a bake tracks; crossing a segment from the right of a->b to its left adds the segment's counts to them.
type counts [2]int32

func (c counts) add(d counts) counts { return counts{c[0] + d[0], c[1] + d[1]} }
func (c counts) sub(d counts) counts { return counts{c[0] - d[0], c[1] - d[1]} }
func (c counts) neg() counts         { return counts{-c[0], -c[1]} }
func (c counts) zero() bool          { return c[0] == 0 && c[1] == 0 }

// seg is a constraint segment with a.less(b), and the counts crossing it from its right to its left adds.
type seg struct {
	a, b ipt
	d    counts
}

// addSeg appends the directed segment a->b with counts d to out in normal form, dropping a zero-length segment.
func addSeg(out []seg, a, b ipt, d counts) []seg {
	switch {
	case a == b:
		return out
	case b.less(a):
		return append(out, seg{a: b, b: a, d: d.neg()})
	}
	return append(out, seg{a: a, b: b, d: d})
}

// addLoop appends the closed polygon pts, edge by edge, with counts d on each edge.
func addLoop(out []seg, pts []ipt, d counts) []seg {
	for i, p := range pts {
		out = addSeg(out, p, pts[(i+1)%len(pts)], d)
	}
	return out
}

// maxArrangePasses bounds the passes of arrange; snapping crossings to the grid settles in a few passes on real geometry.
const maxArrangePasses = 64

// errUnsettled is returned when snapped crossings keep creating new ones.
var errUnsettled = errors.New("navbake: the geometry did not settle after snapping crossings to the grid; try a finer Precision")

// arrange splits the segments at every crossing, touching endpoint and collinear overlap, snapping crossings to the grid, and merges coincident segments by adding their counts, dropping those whose counts cancel. The result is sorted and has no two segments that cross or overlap, and no endpoint inside another segment.
func arrange(in []seg) ([]seg, error) {
	segs := canonical(in)
	for range maxArrangePasses {
		splits := findSplits(segs)
		if splits == nil {
			return segs, nil
		}
		next := make([]seg, 0, len(segs)+len(splits)*2)
		for i, s := range segs {
			ps := splits[i]
			if len(ps) == 0 {
				next = append(next, s)
				continue
			}
			ps = append(ps, s.a, s.b)
			dx, dy := float64(s.b.x-s.a.x), float64(s.b.y-s.a.y)
			proj := func(p ipt) float64 { return float64(p.x-s.a.x)*dx + float64(p.y-s.a.y)*dy }
			slices.SortFunc(ps, func(p, q ipt) int {
				if c := cmp.Compare(proj(p), proj(q)); c != 0 {
					return c
				}
				if p.less(q) {
					return -1
				}
				if q.less(p) {
					return 1
				}
				return 0
			})
			ps = slices.Compact(ps)
			for k := 0; k+1 < len(ps); k++ {
				next = addSeg(next, ps[k], ps[k+1], s.d)
			}
		}
		segs = canonical(next)
	}
	return nil, errUnsettled
}

// canonical sorts segs, merges coincident ones by adding their counts and drops those whose counts cancel.
func canonical(segs []seg) []seg {
	slices.SortFunc(segs, func(s, t seg) int {
		switch {
		case s.a != t.a:
			if s.a.less(t.a) {
				return -1
			}
			return 1
		case s.b != t.b:
			if s.b.less(t.b) {
				return -1
			}
			return 1
		}
		return 0
	})
	out := segs[:0]
	for i := 0; i < len(segs); {
		s := segs[i]
		j := i + 1
		for j < len(segs) && segs[j].a == s.a && segs[j].b == s.b {
			s.d = s.d.add(segs[j].d)
			j++
		}
		if !s.d.zero() {
			out = append(out, s)
		}
		i = j
	}
	return out
}

// box is an axis-aligned bounding box in grid steps.
type box struct {
	x0, y0, x1, y1 int64
}

func segBox(s seg) box {
	return box{min(s.a.x, s.b.x), min(s.a.y, s.b.y), max(s.a.x, s.b.x), max(s.a.y, s.b.y)}
}

// findSplits returns, for each segment, the points it must be split at, or nil when no segment needs splitting. Candidate pairs come from a uniform grid of buckets, each pair tested once, in the bucket holding the lower corner of the two boxes' overlap.
func findSplits(segs []seg) [][]ipt {
	if len(segs) < 2 {
		return nil
	}
	boxes := make([]box, len(segs))
	all := segBox(segs[0])
	var total float64
	for i, s := range segs {
		b := segBox(s)
		boxes[i] = b
		all = box{min(all.x0, b.x0), min(all.y0, b.y0), max(all.x1, b.x1), max(all.y1, b.y1)}
		total += math.Hypot(float64(b.x1-b.x0), float64(b.y1-b.y0))
	}
	cell := max(int64(total/float64(len(segs))), 1)
	cols := func() int64 { return (all.x1-all.x0)/cell + 1 }
	rows := func() int64 { return (all.y1-all.y0)/cell + 1 }
	for cols()*rows() > 4*int64(len(segs))+4096 {
		cell *= 2
	}
	gw, gh := cols(), rows()
	buckets := make([][]int32, gw*gh)
	for i, b := range boxes {
		for cy := (b.y0 - all.y0) / cell; cy <= (b.y1-all.y0)/cell; cy++ {
			for cx := (b.x0 - all.x0) / cell; cx <= (b.x1-all.x0)/cell; cx++ {
				buckets[cy*gw+cx] = append(buckets[cy*gw+cx], int32(i))
			}
		}
	}
	var splits [][]ipt
	split := func(i int32, p ipt) {
		if splits == nil {
			splits = make([][]ipt, len(segs))
		}
		splits[i] = append(splits[i], p)
	}
	for bi, list := range buckets {
		cx, cy := int64(bi)%gw, int64(bi)/gw
		for x := 0; x < len(list); x++ {
			i := list[x]
			bx := boxes[i]
			for y := x + 1; y < len(list); y++ {
				j := list[y]
				by := boxes[j]
				if bx.x1 < by.x0 || by.x1 < bx.x0 || bx.y1 < by.y0 || by.y1 < bx.y0 {
					continue
				}
				if (max(bx.x0, by.x0)-all.x0)/cell != cx || (max(bx.y0, by.y0)-all.y0)/cell != cy {
					continue
				}
				splitPair(segs[i], segs[j], func(p ipt) { split(i, p) }, func(p ipt) { split(j, p) })
			}
		}
	}
	return splits
}

// splitPair reports through si and sj the points where segment s and segment t must be split so that they neither cross, overlap nor touch except at shared endpoints.
func splitPair(s, t seg, si, sj func(ipt)) {
	a, b, c, d := s.a, s.b, t.a, t.b
	o1, o2 := orient(a, b, c), orient(a, b, d)
	o3, o4 := orient(c, d, a), orient(c, d, b)
	if o1 == 0 && strictlyBetween(a, b, c) {
		si(c)
	}
	if o2 == 0 && strictlyBetween(a, b, d) {
		si(d)
	}
	if o3 == 0 && strictlyBetween(c, d, a) {
		sj(a)
	}
	if o4 == 0 && strictlyBetween(c, d, b) {
		sj(b)
	}
	if sign(o1)*sign(o2) < 0 && sign(o3)*sign(o4) < 0 {
		p := crossPoint(a, b, c, d)
		if p != a && p != b {
			si(p)
		}
		if p != c && p != d {
			sj(p)
		}
	}
}
