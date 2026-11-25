package lights2d

import (
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// Segment is a 2D line segment from A to B that blocks light.
type Segment struct {
	A, B core.Point
}

// Occluder blocks light along its edges.
//
// Vertices form a closed polygon (each consecutive pair of vertices, plus the
// edge from the last vertex back to the first when there are at least three
// vertices). Segments are additional explicit line segments. An Occluder may
// populate Vertices, Segments, or both.
type Occluder struct {
	Vertices []core.Point
	Segments []Segment
}

// RectOccluder returns a closed rectangular Occluder for b.
func RectOccluder(b core.Bounds) Occluder {
	x0 := b.Origin.X
	y0 := b.Origin.Y
	x1 := x0 + b.Size.Width
	y1 := y0 + b.Size.Height
	return Occluder{
		Vertices: []core.Point{
			{X: x0, Y: y0},
			{X: x1, Y: y0},
			{X: x1, Y: y1},
			{X: x0, Y: y1},
		},
	}
}

// PolygonOccluder returns a closed polygon Occluder from vertices.
func PolygonOccluder(vertices ...core.Point) Occluder {
	cp := make([]core.Point, len(vertices))
	copy(cp, vertices)
	return Occluder{Vertices: cp}
}

// SegmentOccluder returns an Occluder built from explicit line segments.
func SegmentOccluder(segments ...Segment) Occluder {
	cp := make([]Segment, len(segments))
	copy(cp, segments)
	return Occluder{Segments: cp}
}

type seg64 struct {
	ax, ay float64
	bx, by float64
	minX   float64
	maxX   float64
	minY   float64
	maxY   float64
}

func makeSeg64(ax, ay, bx, by float64) (seg64, bool) {
	dx, dy := bx-ax, by-ay
	if dx*dx+dy*dy < 1e-18 {
		return seg64{}, false
	}
	return seg64{
		ax: ax, ay: ay,
		bx: bx, by: by,
		minX: math.Min(ax, bx),
		maxX: math.Max(ax, bx),
		minY: math.Min(ay, by),
		maxY: math.Max(ay, by),
	}, true
}

func appendOccluderSegments(dst []seg64, occluders []Occluder) []seg64 {
	for _, occ := range occluders {
		n := len(occ.Vertices)
		if n == 2 {
			a, b := occ.Vertices[0], occ.Vertices[1]
			if s, ok := makeSeg64(float64(a.X), float64(a.Y), float64(b.X), float64(b.Y)); ok {
				dst = append(dst, s)
			}
		} else if n >= 3 {
			for i := range n {
				a := occ.Vertices[i]
				b := occ.Vertices[(i+1)%n]
				if s, ok := makeSeg64(float64(a.X), float64(a.Y), float64(b.X), float64(b.Y)); ok {
					dst = append(dst, s)
				}
			}
		}
		for _, seg := range occ.Segments {
			if s, ok := makeSeg64(float64(seg.A.X), float64(seg.A.Y), float64(seg.B.X), float64(seg.B.Y)); ok {
				dst = append(dst, s)
			}
		}
	}
	return dst
}

// VisibilityPolygon computes the exact 2D visibility polygon from origin within
// the bounding square [origin.X-radius, origin.X+radius] x [origin.Y-radius, origin.Y+radius]
// against occluders using radial ray casting.
//
// It returns nil when radius <= 0.
func VisibilityPolygon(origin core.Point, radius float32, occluders []Occluder) []core.Point {
	if radius <= 0 {
		return nil
	}
	ox, oy := float64(origin.X), float64(origin.Y)
	r := float64(radius)
	minX, maxX := ox-r, ox+r
	minY, maxY := oy-r, oy+r

	// Start with the four bounding-box boundary edges around origin ± radius.
	var segs []seg64
	bboxSegs := [4][4]float64{
		{minX, minY, maxX, minY},
		{maxX, minY, maxX, maxY},
		{maxX, maxY, minX, maxY},
		{minX, maxY, minX, minY},
	}
	for _, bs := range bboxSegs {
		if s, ok := makeSeg64(bs[0], bs[1], bs[2], bs[3]); ok {
			segs = append(segs, s)
		}
	}

	// Collect occluder segments that overlap the light's bounding box.
	allOcc := appendOccluderSegments(nil, occluders)
	occStart := len(segs)
	for _, s := range allOcc {
		if s.maxX < minX || s.minX > maxX || s.maxY < minY || s.minY > maxY {
			continue
		}
		segs = append(segs, s)
	}

	// Gather critical ray angles to bounding-box corners, occluder endpoints
	// inside the bounding box, and segment intersections.
	const rayEps = 1e-5
	angles := make([]float64, 0, len(segs)*6+12)
	addPoint := func(px, py float64) {
		dx, dy := px-ox, py-oy
		if dx*dx+dy*dy < 1e-18 {
			return
		}
		a := math.Atan2(dy, dx)
		angles = append(angles, wrapAngle64(a-rayEps), wrapAngle64(a), wrapAngle64(a+rayEps))
	}

	// Bounding-box corners.
	addPoint(minX, minY)
	addPoint(maxX, minY)
	addPoint(maxX, maxY)
	addPoint(minX, maxY)

	// Occluder endpoints and intersections (with bbox and with other occluder segments).
	for i := occStart; i < len(segs); i++ {
		s := segs[i]
		if s.ax >= minX && s.ax <= maxX && s.ay >= minY && s.ay <= maxY {
			addPoint(s.ax, s.ay)
		}
		if s.bx >= minX && s.bx <= maxX && s.by >= minY && s.by <= maxY {
			addPoint(s.bx, s.by)
		}
		// Intersections with the four bounding-box sides and prior occluder segments.
		for j := range i {
			o := segs[j]
			if s.maxX < o.minX || s.minX > o.maxX || s.maxY < o.minY || s.minY > o.maxY {
				continue
			}
			if ix, iy, ok := segmentIntersection(s, o); ok {
				if ix >= minX-1e-9 && ix <= maxX+1e-9 && iy >= minY-1e-9 && iy <= maxY+1e-9 {
					addPoint(ix, iy)
				}
			}
		}
	}

	slices.Sort(angles)

	// Deduplicate nearly identical ray angles.
	uniqueAngles := angles[:0]
	for _, a := range angles {
		if len(uniqueAngles) == 0 || a-uniqueAngles[len(uniqueAngles)-1] > 1e-9 {
			uniqueAngles = append(uniqueAngles, a)
		}
	}

	type pt64 struct{ x, y float64 }
	hits := make([]pt64, 0, len(uniqueAngles))
	for _, a := range uniqueAngles {
		dy, dx := math.Sincos(a)
		bestT := math.Inf(1)
		for _, s := range segs {
			if t, ok := raySegmentIntersect(ox, oy, dx, dy, s); ok && t < bestT {
				bestT = t
			}
		}
		if !math.IsInf(bestT, 1) {
			hits = append(hits, pt64{x: ox + bestT*dx, y: oy + bestT*dy})
		}
	}

	if len(hits) < 3 {
		return nil
	}

	scale := math.Max(1.0, r)
	dupEps := scale * 1e-8
	colEps := scale * 1e-5

	// Step 1: Drop exact/near-identical consecutive points.
	dedup := make([]pt64, 0, len(hits))
	for _, p := range hits {
		if len(dedup) == 0 || math.Hypot(p.x-dedup[len(dedup)-1].x, p.y-dedup[len(dedup)-1].y) > dupEps {
			dedup = append(dedup, p)
		}
	}
	for len(dedup) > 1 && math.Hypot(dedup[0].x-dedup[len(dedup)-1].x, dedup[0].y-dedup[len(dedup)-1].y) <= dupEps {
		dedup = dedup[:len(dedup)-1]
	}

	// Step 2: Prune collinear intermediate vertices cyclically so exact corner
	// vertices are preserved and grazing rays along flat walls collapse cleanly.
	pruned := dedup
	changed := true
	for changed && len(pruned) > 3 {
		changed = false
		next := make([]pt64, 0, len(pruned))
		n := len(pruned)
		for i := range n {
			var prev pt64
			if len(next) > 0 {
				prev = next[len(next)-1]
			} else {
				prev = pruned[(i-1+n)%n]
			}
			curr := pruned[i]
			nxt := pruned[(i+1)%n]
			if isCollinearBetween(prev.x, prev.y, curr.x, curr.y, nxt.x, nxt.y, colEps) && len(pruned)-1 >= 3 {
				changed = true
				continue
			}
			next = append(next, curr)
		}
		// Check wrap-around for first vertex against final kept last vertex.
		if len(next) > 3 && isCollinearBetween(next[len(next)-1].x, next[len(next)-1].y, next[0].x, next[0].y, next[1].x, next[1].y, colEps) {
			next = next[1:]
			changed = true
		}
		if len(next) < 3 {
			break
		}
		pruned = next
	}

	// Step 3: Drop any remaining tightly coincident vertices.
	mergeEps := scale * 1e-6
	out := make([]core.Point, 0, len(pruned))
	for _, p := range pruned {
		pt := core.Point{X: core.Points(p.x), Y: core.Points(p.y)}
		if len(out) > 0 {
			last := out[len(out)-1]
			if math.Hypot(float64(pt.X-last.X), float64(pt.Y-last.Y)) <= mergeEps {
				continue
			}
		}
		out = append(out, pt)
	}
	if len(out) > 1 {
		first, last := out[0], out[len(out)-1]
		if math.Hypot(float64(first.X-last.X), float64(first.Y-last.Y)) <= mergeEps {
			out = out[:len(out)-1]
		}
	}
	if len(out) < 3 {
		return nil
	}
	return out
}

// Visible reports whether the line of sight from origin to target is
// unobstructed by occluders.
func Visible(origin, target core.Point, occluders []Occluder) bool {
	if len(occluders) == 0 {
		return true
	}
	segs := appendOccluderSegments(nil, occluders)
	return visibleSegments(float64(origin.X), float64(origin.Y), float64(target.X), float64(target.Y), segs)
}

// LineOfSight returns a function that reports Visible against *occluders, reading the slice on every call so a game that rebuilds its occluders keeps one function. It has the shape of ai.LineOfSight, so the occluders that cast shadows also block an agent's sight.
func LineOfSight(occluders *[]Occluder) func(from, to core.Point) bool {
	return func(from, to core.Point) bool {
		return Visible(from, to, *occluders)
	}
}

func visibleSegments(ox, oy, tx, ty float64, segs []seg64) bool {
	dx, dy := tx-ox, ty-oy
	if dx*dx+dy*dy < 1e-18 {
		return true
	}
	minX, maxX := math.Min(ox, tx), math.Max(ox, tx)
	minY, maxY := math.Min(oy, ty), math.Max(oy, ty)

	for _, s := range segs {
		if s.maxX < minX || s.minX > maxX || s.maxY < minY || s.minY > maxY {
			continue
		}
		sx, sy := s.bx-s.ax, s.by-s.ay
		vx, vy := s.ax-ox, s.ay-oy
		denom := dx*sy - dy*sx
		if math.Abs(denom) <= 1e-12 {
			continue
		}
		t := (vx*sy - vy*sx) / denom
		u := (vx*dy - vy*dx) / denom
		if t > 1e-6 && t < 1-1e-6 && u >= -1e-6 && u <= 1+1e-6 {
			return false
		}
	}
	return true
}

// PointInPolygon reports whether p lies inside the closed polygon poly using
// the even-odd ray-casting rule.
func PointInPolygon(p core.Point, poly []core.Point) bool {
	n := len(poly)
	if n < 3 {
		return false
	}
	px, py := float64(p.X), float64(p.Y)
	inside := false
	j := n - 1
	for i := range n {
		xi, yi := float64(poly[i].X), float64(poly[i].Y)
		xj, yj := float64(poly[j].X), float64(poly[j].Y)
		if (yi > py) != (yj > py) {
			xIntersect := (xj-xi)*(py-yi)/(yj-yi) + xi
			if px < xIntersect {
				inside = !inside
			}
		}
		j = i
	}
	return inside
}

func raySegmentIntersect(ox, oy, dx, dy float64, s seg64) (float64, bool) {
	sx, sy := s.bx-s.ax, s.by-s.ay
	vx, vy := s.ax-ox, s.ay-oy
	denom := dx*sy - dy*sx
	if math.Abs(denom) > 1e-12 {
		t := (vx*sy - vy*sx) / denom
		u := (vx*dy - vy*dx) / denom
		if t >= -1e-9 && u >= -1e-11 && u <= 1+1e-11 {
			if t < 0 {
				t = 0
			}
			return t, true
		}
		return 0, false
	}
	// Ray is parallel to segment: check collinearity (dx, dy is unit length).
	if math.Abs(vx*dy-vy*dx) <= 1e-9 {
		tA := vx*dx + vy*dy
		tB := (s.bx-ox)*dx + (s.by-oy)*dy
		if tA >= 0 && tB >= 0 {
			return math.Min(tA, tB), true
		}
		if tA >= 0 || tB >= 0 {
			return 0, true
		}
	}
	return 0, false
}

func segmentIntersection(a, b seg64) (float64, float64, bool) {
	dax, day := a.bx-a.ax, a.by-a.ay
	dbx, dby := b.bx-b.ax, b.by-b.ay
	denom := dax*dby - day*dbx
	if math.Abs(denom) <= 1e-12 {
		return 0, 0, false
	}
	vx, vy := b.ax-a.ax, b.ay-a.ay
	t := (vx*dby - vy*dbx) / denom
	u := (vx*day - vy*dax) / denom
	if t >= -1e-9 && t <= 1+1e-9 && u >= -1e-9 && u <= 1+1e-9 {
		return a.ax + t*dax, a.ay + t*day, true
	}
	return 0, 0, false
}

func isCollinearBetween(ax, ay, bx, by, cx, cy, eps float64) bool {
	acx, acy := cx-ax, cy-ay
	acLen := math.Hypot(acx, acy)
	if acLen <= eps {
		return true
	}
	// Perpendicular distance from B to line AC.
	cross := math.Abs((bx-ax)*acy - (by-ay)*acx)
	if cross/acLen > eps {
		return false
	}
	// Ensure B lies between A and C (not a reversal/spike).
	dot := (bx-ax)*(cx-bx) + (by-ay)*(cy-by)
	return dot >= -eps*acLen
}

// wrapAngle64 wraps angle a into (-pi, pi].
func wrapAngle64(a float64) float64 {
	a = math.Mod(a+math.Pi, 2*math.Pi)
	if a <= 0 {
		a += 2 * math.Pi
	}
	return a - math.Pi
}
