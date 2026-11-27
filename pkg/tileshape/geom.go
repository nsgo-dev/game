package tileshape

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/lights2d"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// class is how a piece of geometry collides.
type class uint8

const (
	classSolid class = iota
	classOneWay
)

// source is where an edge came from.
type source uint8

const (
	sourceCell  source = iota // a whole cell
	sourceShape               // a closed tile shape
	sourceLine                // a polyline tile shape
)

type vec struct{ x, y float64 }

// poly is a closed tile shape, wound so that its signed area is positive (clockwise on screen).
type poly struct {
	pts  []vec
	rect bool // an axis-aligned rectangle
}

// tileGeom is the collision geometry of one gid, flip bits included, relative to the top-left corner of the cell it is placed in.
type tileGeom struct {
	full  bool // a closed shape fills the cell
	polys []poly
	lines [][]vec
}

// placed is a tile's partial geometry placed in a cell.
type placed struct {
	g      *tileGeom
	ox, oy float64
	cls    class
}

type edge struct {
	ax, ay, bx, by float64
	src            source
	cls            class
}

func (e edge) segment() Segment {
	return Segment{A: core.Point{X: core.Points(e.ax), Y: core.Points(e.ay)}, B: core.Point{X: core.Points(e.bx), Y: core.Points(e.by)}}
}

// chunk caches the cells and geometry of a chunkSize by chunkSize block of the map.
type chunk struct {
	cellsOK bool
	geomOK  bool

	flags  []physics.TileFlags // what TileFlagsAt reports, row-major
	solid  []bool              // whole solid cells, row-major
	start  []int32             // cell i's partial geometry is placed[start[i]:start[i+1]]
	placed []placed

	outline    []Segment
	occ        []lights2d.Segment
	slopes     []physics.Slope
	boxes      []physics.Box
	shapeBoxes []physics.Box
	shapePolys []poly
}

// cellChunk returns the chunk holding (col, row) with its cells built; the caller has checked the bounds.
func (v *View) cellChunk(col, row int) *chunk {
	cx, cy := col/chunkSize, row/chunkSize
	ch := &v.chunks[cy*v.ccols+cx]
	if !ch.cellsOK {
		v.buildCells(ch, cx, cy)
	}
	return ch
}

// geomChunk returns the chunk (cx, cy) with its geometry built.
func (v *View) geomChunk(cx, cy int) *chunk {
	ch := &v.chunks[cy*v.ccols+cx]
	if !ch.cellsOK {
		v.buildCells(ch, cx, cy)
	}
	if !ch.geomOK {
		v.buildGeom(ch, cx, cy)
	}
	return ch
}

// solidAt reports whether (col, row) is a whole solid cell, and returns its partial geometry.
func (v *View) solidAt(col, row int) (bool, []placed) {
	if col < 0 || row < 0 || col >= v.cols || row >= v.rows {
		return false, nil
	}
	ch := v.cellChunk(col, row)
	i := (row%chunkSize)*chunkSize + col%chunkSize
	return ch.solid[i], ch.placed[ch.start[i]:ch.start[i+1]]
}

func (v *View) buildCells(ch *chunk, cx, cy int) {
	if ch.flags == nil {
		ch.flags = make([]physics.TileFlags, chunkSize*chunkSize)
		ch.solid = make([]bool, chunkSize*chunkSize)
		ch.start = make([]int32, chunkSize*chunkSize+1)
	}
	clear(ch.flags)
	clear(ch.solid)
	ch.placed = ch.placed[:0]
	for r := range chunkSize {
		row := cy*chunkSize + r
		for c := range chunkSize {
			col := cx*chunkSize + c
			i := r*chunkSize + c
			ch.start[i] = int32(len(ch.placed))
			if row < v.rows && col < v.cols {
				ch.flags[i], ch.solid[i], ch.placed = v.cell(col, row, ch.placed)
			}
		}
	}
	ch.start[chunkSize*chunkSize] = int32(len(ch.placed))
	ch.cellsOK = true
	ch.geomOK = false
	v.aggOK = 0
}

// cell computes the flags and whole solidity of (col, row) from the map, appending its partial geometry to dst.
func (v *View) cell(col, row int, dst []placed) (physics.TileFlags, bool, []placed) {
	m := v.m
	if f, ok := m.CellFlags(col, row); ok {
		return f.PhysicsFlags(), f&tilemap.TileSolid != 0, dst
	}
	mark := len(dst)
	var flags, shaped tilemap.TileFlags
	n := len(v.layers)
	if v.all {
		n = len(m.Layers)
	}
	for k := range n {
		li := k
		if !v.all {
			li = v.layers[k]
		}
		gid := m.TileAt(li, col, row)
		if gid&tilemap.TileIDMask == 0 {
			continue
		}
		f := m.Flags(gid)
		g := v.tileGeom(gid)
		if g == nil {
			flags |= f
			continue
		}
		cls, bit := classSolid, tilemap.TileSolid
		if f&tilemap.TileOneWay != 0 && f&tilemap.TileSolid == 0 {
			cls, bit = classOneWay, tilemap.TileOneWay
		}
		if g.full {
			flags |= f | bit
			continue
		}
		flags |= f &^ (tilemap.TileSolid | tilemap.TileOneWay)
		if len(g.polys) > 0 {
			// A polyline has no inside, so it never makes its cell solid.
			shaped |= bit
		}
		dst = append(dst, placed{g: g, ox: v.ox + float64(col)*v.tw, oy: v.oy + float64(row)*v.th, cls: cls})
	}
	solid := flags&tilemap.TileSolid != 0
	if solid {
		// A whole solid cell hides any shape in it.
		dst = dst[:mark]
		shaped = 0
	}
	if v.shapedSolid {
		flags |= shaped
	}
	return flags.PhysicsFlags(), solid, dst
}

// tileGeom returns the cached geometry of gid's collision shapes, or nil when it has none.
func (v *View) tileGeom(gid uint32) *tileGeom {
	if g, ok := v.shapes[gid]; ok {
		return g
	}
	var g *tileGeom
	if shapes := v.m.TileShapes(gid); len(shapes) > 0 {
		g = v.makeTileGeom(gid, shapes)
	}
	if v.shapes == nil {
		v.shapes = make(map[uint32]*tileGeom)
	}
	v.shapes[gid] = g
	return g
}

func (v *View) makeTileGeom(gid uint32, shapes []tilemap.TileShape) *tileGeom {
	tw, th := v.m.TileBox(gid)
	box := float64(tw) == v.tw && float64(th) == v.th
	// A tile taller than the map's cells is drawn bottom-aligned in its cell, and so are its shapes.
	oy := v.th - float64(th)
	g := &tileGeom{}
	for _, s := range shapes {
		pts := make([]vec, 0, len(s.Points))
		for _, p := range s.Points {
			q := vec{float64(p.X), oy + float64(p.Y)}
			if math.IsNaN(q.x) || math.IsNaN(q.y) || math.IsInf(q.x, 0) || math.IsInf(q.y, 0) {
				pts = nil
				break
			}
			if n := len(pts); n > 0 && pts[n-1] == q {
				continue
			}
			pts = append(pts, q)
		}
		if !s.Closed() {
			if len(pts) >= 2 {
				g.lines = append(g.lines, pts)
			}
			continue
		}
		if len(pts) > 1 && pts[0] == pts[len(pts)-1] {
			pts = pts[:len(pts)-1]
		}
		p, ok := makePoly(pts)
		if !ok {
			continue
		}
		if box && fillsCell(p, v.tw, v.th) {
			return &tileGeom{full: true}
		}
		g.polys = append(g.polys, p)
	}
	if len(g.polys) == 0 && len(g.lines) == 0 {
		return nil
	}
	return g
}

// makePoly winds pts so that their signed area is positive, and reports false for a polygon with no area.
func makePoly(pts []vec) (poly, bool) {
	if len(pts) < 3 {
		return poly{}, false
	}
	area := signedArea(pts)
	if math.Abs(area) < 1e-9 || math.IsNaN(area) || math.IsInf(area, 0) {
		return poly{}, false
	}
	if area < 0 {
		slices.Reverse(pts)
	}
	return poly{pts: pts, rect: isRect(pts)}, true
}

func signedArea(pts []vec) float64 {
	var a float64
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		a += float64(p.x*q.y) - float64(q.x*p.y)
	}
	return a / 2
}

// isRect reports whether pts are the four corners of an axis-aligned rectangle.
func isRect(pts []vec) bool {
	if len(pts) != 4 {
		return false
	}
	for i, p := range pts {
		q := pts[(i+1)%4]
		if math.Abs(p.x-q.x) > 1e-6 && math.Abs(p.y-q.y) > 1e-6 {
			return false
		}
	}
	return true
}

// fillsCell reports whether p, relative to a cell's top-left corner, covers the whole w by h cell.
func fillsCell(p poly, w, h float64) bool {
	eps := 1e-4 * max(w, h)
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, q := range p.pts {
		minX, minY = min(minX, q.x), min(minY, q.y)
		maxX, maxY = max(maxX, q.x), max(maxY, q.y)
	}
	if math.Abs(minX) > eps || math.Abs(minY) > eps || math.Abs(maxX-w) > eps || math.Abs(maxY-h) > eps {
		return false
	}
	return math.Abs(signedArea(p.pts)-w*h) <= eps*max(w, h)
}

// appendCellEdges appends the four edges of the whole cell at (col, row), wound like a poly.
func (v *View) appendCellEdges(dst [][2]vec, col, row int) [][2]vec {
	x0, y0 := v.ox+float64(col)*v.tw, v.oy+float64(row)*v.th
	x1, y1 := x0+v.tw, y0+v.th
	return append(dst, [2]vec{{x0, y0}, {x1, y0}}, [2]vec{{x1, y0}, {x1, y1}}, [2]vec{{x1, y1}, {x0, y1}}, [2]vec{{x0, y1}, {x0, y0}})
}

// appendPlacedEdges appends the edges of every polygon of class cls in ps, except the one skip points to.
func appendPlacedEdges(dst [][2]vec, ps []placed, cls class, skip *poly) [][2]vec {
	for _, pl := range ps {
		if pl.cls != cls {
			continue
		}
		for i := range pl.g.polys {
			p := &pl.g.polys[i]
			if p == skip {
				continue
			}
			for k, a := range p.pts {
				b := p.pts[(k+1)%len(p.pts)]
				dst = append(dst, [2]vec{{pl.ox + a.x, pl.oy + a.y}, {pl.ox + b.x, pl.oy + b.y}})
			}
		}
	}
	return dst
}

// appendAround appends the edges of every polygon of class cls placed in the cells within mc columns and mr rows of (col, row), the cell itself excluded, and with cells set the edges of the whole solid cells there.
func (v *View) appendAround(dst [][2]vec, col, row, mc, mr int, cls class, cells bool) [][2]vec {
	for dr := -mr; dr <= mr; dr++ {
		for dc := -mc; dc <= mc; dc++ {
			if dc == 0 && dr == 0 {
				continue
			}
			solid, ps := v.solidAt(col+dc, row+dr)
			if solid && cells {
				dst = v.appendCellEdges(dst, col+dc, row+dr)
			}
			dst = appendPlacedEdges(dst, ps, cls, nil)
		}
	}
	return dst
}

func hasClass(ps []placed, cls class) bool {
	for _, pl := range ps {
		if pl.cls == cls && len(pl.g.polys) > 0 {
			return true
		}
	}
	return false
}

func (v *View) buildGeom(ch *chunk, cx, cy int) {
	ch.outline = ch.outline[:0]
	ch.occ = ch.occ[:0]
	ch.slopes = ch.slopes[:0]
	ch.boxes = ch.boxes[:0]
	ch.shapeBoxes = ch.shapeBoxes[:0]
	ch.shapePolys = ch.shapePolys[:0]
	eps := 1e-4 * max(v.tw, v.th)
	raw := v.rawBuf[:0]

	// cut emits the parts of the edge a->b that no opposite collinear cutter in v.cutters covers.
	cut := func(a, b vec, src source, cls class) {
		v.keep, v.cuts = subtract(v.keep[:0], v.cuts[:0], a, b, v.cutters, eps)
		dx, dy := b.x-a.x, b.y-a.y
		for _, k := range v.keep {
			raw = append(raw, edge{ax: a.x + dx*k[0], ay: a.y + dy*k[0], bx: a.x + dx*k[1], by: a.y + dy*k[1], src: src, cls: cls})
		}
	}

	for r := range chunkSize {
		row := cy*chunkSize + r
		if row >= v.rows {
			break
		}
		for c := range chunkSize {
			col := cx*chunkSize + c
			if col >= v.cols {
				break
			}
			i := r*chunkSize + c
			if ch.solid[i] {
				x0, y0 := v.ox+float64(col)*v.tw, v.oy+float64(row)*v.th
				x1, y1 := x0+v.tw, y0+v.th
				// Top, right, bottom and left edges, each toward the neighbour across it.
				for _, side := range [4]struct {
					a, b   vec
					dc, dr int
				}{{vec{x0, y0}, vec{x1, y0}, 0, -1}, {vec{x1, y0}, vec{x1, y1}, 1, 0}, {vec{x1, y1}, vec{x0, y1}, 0, 1}, {vec{x0, y1}, vec{x0, y0}, -1, 0}} {
					solid, ps := v.solidAt(col+side.dc, row+side.dr)
					if !solid && (v.reachC > 0 || v.reachR > 0) {
						// Oversized tiles placed further away can cover this side too.
						v.cutters = v.appendAround(v.cutters[:0], col, row, 1+v.reachC, 1+v.reachR, classSolid, false)
						cut(side.a, side.b, sourceCell, classSolid)
						continue
					}
					switch {
					case solid:
					case !hasClass(ps, classSolid):
						raw = append(raw, edge{ax: side.a.x, ay: side.a.y, bx: side.b.x, by: side.b.y, src: sourceCell, cls: classSolid})
					default:
						v.cutters = appendPlacedEdges(v.cutters[:0], ps, classSolid, nil)
						cut(side.a, side.b, sourceCell, classSolid)
					}
				}
				continue
			}
			own := ch.placed[ch.start[i]:ch.start[i+1]]
			for _, pl := range own {
				for pi := range pl.g.polys {
					p := &pl.g.polys[pi]
					// Gather the edges this polygon can share: other polygons of its class here and around it, and solid cells around it, reaching as far as oversized tiles do.
					v.cutters = appendPlacedEdges(v.cutters[:0], own, pl.cls, p)
					v.cutters = v.appendAround(v.cutters, col, row, 1+2*v.reachC, 1+2*v.reachR, pl.cls, pl.cls == classSolid)
					for k, a := range p.pts {
						b := p.pts[(k+1)%len(p.pts)]
						cut(vec{pl.ox + a.x, pl.oy + a.y}, vec{pl.ox + b.x, pl.oy + b.y}, sourceShape, pl.cls)
					}
					if pl.cls == classSolid {
						if p.rect {
							ch.shapeBoxes = append(ch.shapeBoxes, rectBox(p.pts, pl.ox, pl.oy))
						} else {
							worldPts := make([]vec, len(p.pts))
							for k, pt := range p.pts {
								worldPts[k] = vec{pl.ox + pt.x, pl.oy + pt.y}
							}
							ch.shapePolys = append(ch.shapePolys, poly{pts: worldPts})
						}
					}
				}
				for _, l := range pl.g.lines {
					for k := 1; k < len(l); k++ {
						raw = append(raw, edge{ax: pl.ox + l[k-1].x, ay: pl.oy + l[k-1].y, bx: pl.ox + l[k].x, by: pl.oy + l[k].y, src: sourceLine, cls: pl.cls})
					}
				}
			}
		}
	}

	v.keys, v.order = mergeEdges(v.keys[:0], v.order, raw, eps)
	v.rawBuf = raw
	for _, k := range v.keys {
		e := k.e
		s := e.segment()
		// A polyline has no solid side, so it is a slope but not part of the outline.
		if e.cls == classSolid && e.src != sourceLine {
			ch.outline = append(ch.outline, s)
			ch.occ = append(ch.occ, lights2d.Segment{A: s.A, B: s.B})
		}
		if e.src == sourceCell {
			continue
		}
		dx := e.bx - e.ax
		l := math.Hypot(dx, e.by-e.ay)
		switch {
		case e.src == sourceLine:
		case e.cls == classSolid && dx >= -1e-6*l:
		case e.cls == classOneWay && dx > 1e-6*l:
		default:
			continue
		}
		ch.slopes = append(ch.slopes, physics.Slope{A: s.A, B: s.B})
	}
	ch.boxes = v.mergeBoxes(ch.boxes, ch, cx, cy)
	if len(ch.shapeBoxes) > 1 {
		ch.shapeBoxes = mergeRects(ch.shapeBoxes, eps)
	}
	if len(ch.shapePolys) > 1 {
		ch.shapePolys = mergeConvexPolys(ch.shapePolys, eps)
	}
	ch.geomOK = true
	v.aggOK = 0
}

func rectBox(pts []vec, ox, oy float64) physics.Box {
	minX, minY, maxX, maxY := pts[0].x, pts[0].y, pts[0].x, pts[0].y
	for _, p := range pts[1:] {
		minX, minY = min(minX, p.x), min(minY, p.y)
		maxX, maxY = max(maxX, p.x), max(maxY, p.y)
	}
	return physics.Box{X: float32(ox + minX), Y: float32(oy + minY), W: float32(maxX - minX), H: float32(maxY - minY)}
}

// subtract appends to keep the parameter intervals of the edge a->b, within [0, 1], that no cutter running the opposite way along the same line covers, using cuts as scratch space.
func subtract(keep, cuts [][2]float64, a, b vec, cutters [][2]vec, eps float64) ([][2]float64, [][2]float64) {
	dx, dy := b.x-a.x, b.y-a.y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return keep, cuts
	}
	l := math.Sqrt(l2)
	for _, f := range cutters {
		fdx, fdy := f[1].x-f[0].x, f[1].y-f[0].y
		if dx*fdx+dy*fdy >= 0 {
			continue
		}
		// Both of f's ends must lie on a->b's line.
		if math.Abs(dx*(f[0].y-a.y)-dy*(f[0].x-a.x))/l > eps || math.Abs(dx*(f[1].y-a.y)-dy*(f[1].x-a.x))/l > eps {
			continue
		}
		t0 := ((f[0].x-a.x)*dx + (f[0].y-a.y)*dy) / l2
		t1 := ((f[1].x-a.x)*dx + (f[1].y-a.y)*dy) / l2
		lo, hi := max(min(t0, t1), 0), min(max(t0, t1), 1)
		if (hi-lo)*l > eps {
			cuts = append(cuts, [2]float64{lo, hi})
		}
	}
	if len(cuts) == 0 {
		return append(keep, [2]float64{0, 1}), cuts
	}
	slices.SortFunc(cuts, func(p, q [2]float64) int { return cmp.Compare(p[0], q[0]) })
	at := 0.0
	for _, c := range cuts {
		if (c[0]-at)*l > eps {
			keep = append(keep, [2]float64{at, c[0]})
		}
		at = max(at, c[1])
	}
	if (1-at)*l > eps {
		keep = append(keep, [2]float64{at, 1})
	}
	return keep, cuts
}

// keyedEdge is an edge with the line and direction it lies on, for merging.
type keyedEdge struct {
	e          edge
	ux, uy     float64
	dir, off   int64
	start, end float64
}

// mergeEdges appends to dst the edges of raw with collinear, same-direction edges of the same source and class that touch or overlap joined into one, in a deterministic order; order is scratch space for sorting.
func mergeEdges(dst []keyedEdge, order []int32, raw []edge, eps float64) ([]keyedEdge, []int32) {
	ks := dst
	for _, e := range raw {
		dx, dy := e.bx-e.ax, e.by-e.ay
		l := math.Hypot(dx, dy)
		if l <= eps {
			continue
		}
		ux, uy := dx/l, dy/l
		s := ux*e.ax + uy*e.ay
		ks = append(ks, keyedEdge{
			e: e, ux: ux, uy: uy,
			dir:   int64(math.Round(math.Atan2(uy, ux) * 1e6)),
			off:   int64(math.Round((ux*e.ay - uy*e.ax) / eps)),
			start: s, end: s + l,
		})
	}
	// Sort indices rather than the large keyed edges themselves.
	order = order[:0]
	for i := range ks {
		order = append(order, int32(i))
	}
	slices.SortFunc(order, func(i, j int32) int {
		p, q := &ks[i], &ks[j]
		return cmp.Or(
			cmp.Compare(p.e.src, q.e.src),
			cmp.Compare(p.e.cls, q.e.cls),
			cmp.Compare(p.dir, q.dir),
			cmp.Compare(p.off, q.off),
			cmp.Compare(p.start, q.start),
		)
	})
	n := len(ks)
	for i := 0; i < len(order); {
		cur := ks[order[i]]
		end := cur.end
		j := i + 1
		for ; j < len(order); j++ {
			n := &ks[order[j]]
			if n.e.src != cur.e.src || n.e.cls != cur.e.cls || n.dir != cur.dir || n.off != cur.off || n.start > end+eps {
				break
			}
			end = max(end, n.end)
		}
		if j > i+1 {
			cur.e.bx, cur.e.by = cur.e.ax+cur.ux*(end-cur.start), cur.e.ay+cur.uy*(end-cur.start)
		}
		ks = append(ks, cur)
		i = j
	}
	// The merged edges follow the keyed ones; move them to the front.
	return append(ks[:0], ks[n:]...), order
}

// mergeBoxes appends to dst the whole solid cells of ch merged greedily into rectangles: runs along each row, grown down while the rows below repeat the run.
func (v *View) mergeBoxes(dst []physics.Box, ch *chunk, cx, cy int) []physics.Box {
	w := min(chunkSize, v.cols-cx*chunkSize)
	h := min(chunkSize, v.rows-cy*chunkSize)
	var used [chunkSize * chunkSize]bool
	for r := range h {
		for c := 0; c < w; {
			i := r*chunkSize + c
			if !ch.solid[i] || used[i] {
				c++
				continue
			}
			c1 := c
			for c1 < w && ch.solid[r*chunkSize+c1] && !used[r*chunkSize+c1] {
				c1++
			}
			r1 := r + 1
		grow:
			for ; r1 < h; r1++ {
				for k := c; k < c1; k++ {
					if !ch.solid[r1*chunkSize+k] || used[r1*chunkSize+k] {
						break grow
					}
				}
			}
			for rr := r; rr < r1; rr++ {
				for k := c; k < c1; k++ {
					used[rr*chunkSize+k] = true
				}
			}
			col, row := cx*chunkSize+c, cy*chunkSize+r
			dst = append(dst, physics.Box{
				X: float32(v.ox + float64(col)*v.tw), Y: float32(v.oy + float64(row)*v.th),
				W: float32(float64(c1-c) * v.tw), H: float32(float64(r1-r) * v.th),
			})
			c = c1
		}
	}
	return dst
}

// mergeRects merges touching axis-aligned rectangles that share a full horizontal or vertical edge within eps,
// repeating horizontal and vertical passes until no further merges apply, and returns them in deterministic (Y, X, W, H) order.
func mergeRects(in []physics.Box, eps float64) []physics.Box {
	if len(in) <= 1 {
		return in
	}
	rects := slices.Clone(in)
	for {
		merged := false

		// Horizontal pass: equal (Y, H), touching X+W == next.X.
		slices.SortFunc(rects, func(a, b physics.Box) int {
			return cmp.Or(
				cmp.Compare(a.Y, b.Y),
				cmp.Compare(a.H, b.H),
				cmp.Compare(a.X, b.X),
				cmp.Compare(a.W, b.W),
			)
		})
		out := rects[:0]
		for _, r := range rects {
			if n := len(out); n > 0 {
				prev := &out[n-1]
				if math.Abs(float64(prev.Y)-float64(r.Y)) <= eps &&
					math.Abs(float64(prev.H)-float64(r.H)) <= eps &&
					math.Abs((float64(prev.X)+float64(prev.W))-float64(r.X)) <= eps {
					prev.W = float32((float64(r.X) + float64(r.W)) - float64(prev.X))
					merged = true
					continue
				}
			}
			out = append(out, r)
		}
		rects = out

		// Vertical pass: equal (X, W), touching Y+H == next.Y.
		slices.SortFunc(rects, func(a, b physics.Box) int {
			return cmp.Or(
				cmp.Compare(a.X, b.X),
				cmp.Compare(a.W, b.W),
				cmp.Compare(a.Y, b.Y),
				cmp.Compare(a.H, b.H),
			)
		})
		out = rects[:0]
		for _, r := range rects {
			if n := len(out); n > 0 {
				prev := &out[n-1]
				if math.Abs(float64(prev.X)-float64(r.X)) <= eps &&
					math.Abs(float64(prev.W)-float64(r.W)) <= eps &&
					math.Abs((float64(prev.Y)+float64(prev.H))-float64(r.Y)) <= eps {
					prev.H = float32((float64(r.Y) + float64(r.H)) - float64(prev.Y))
					merged = true
					continue
				}
			}
			out = append(out, r)
		}
		rects = out

		if !merged {
			break
		}
	}
	slices.SortFunc(rects, func(a, b physics.Box) int {
		return cmp.Or(
			cmp.Compare(a.Y, b.Y),
			cmp.Compare(a.X, b.X),
			cmp.Compare(a.W, b.W),
			cmp.Compare(a.H, b.H),
		)
	})
	return rects
}

func nearVec(a, b vec, eps float64) bool {
	return math.Abs(a.x-b.x) <= eps && math.Abs(a.y-b.y) <= eps
}

// isConvexPoly reports whether pts (wound clockwise on screen, positive signed area) form a simple convex polygon.
func isConvexPoly(pts []vec, eps float64) bool {
	n := len(pts)
	if n < 3 {
		return false
	}
	turning := 0.0
	for i := range n {
		a, b, c := pts[i], pts[(i+1)%n], pts[(i+2)%n]
		ux, uy := b.x-a.x, b.y-a.y
		vx, vy := c.x-b.x, c.y-b.y
		ul, vl := math.Hypot(ux, uy), math.Hypot(vx, vy)
		if ul <= eps || vl <= eps {
			return false
		}
		cross := float64(ux*vy) - float64(uy*vx)
		if cross < -eps*(ul+vl) {
			return false
		}
		dot := float64(ux*vx) + float64(uy*vy)
		turning += math.Atan2(cross, dot)
	}
	return math.Abs(turning-2*math.Pi) < 1e-4
}

// simplifyCollinear removes duplicate and collinear vertices from a closed polygon ring.
func simplifyCollinear(pts []vec, eps float64) []vec {
	cur := make([]vec, 0, len(pts))
	for _, p := range pts {
		if len(cur) > 0 && nearVec(cur[len(cur)-1], p, eps) {
			continue
		}
		cur = append(cur, p)
	}
	if len(cur) > 1 && nearVec(cur[0], cur[len(cur)-1], eps) {
		cur = cur[:len(cur)-1]
	}
	for len(cur) >= 3 {
		changed := false
		next := make([]vec, 0, len(cur))
		n := len(cur)
		for i := range n {
			a, b, c := cur[(i-1+n)%n], cur[i], cur[(i+1)%n]
			ux, uy := b.x-a.x, b.y-a.y
			vx, vy := c.x-b.x, c.y-b.y
			ul, vl := math.Hypot(ux, uy), math.Hypot(vx, vy)
			if ul <= eps || vl <= eps {
				changed = true
				continue
			}
			cross := float64(ux*vy) - float64(uy*vx)
			dot := float64(ux*vx) + float64(uy*vy)
			if math.Abs(cross) <= eps*(ul+vl) && dot >= 0 {
				changed = true
				continue
			}
			next = append(next, b)
		}
		cur = next
		if !changed {
			break
		}
	}
	return cur
}

// tryMergeConvexPolys attempts to merge two clockwise convex polygons sharing an exact opposite edge
// into a single convex polygon with at most physics.MaxPolygonVertices vertices.
func tryMergeConvexPolys(a, b []vec, eps float64) ([]vec, bool) {
	na, nb := len(a), len(b)
	for i := range na {
		a0, a1 := a[i], a[(i+1)%na]
		for j := range nb {
			b0, b1 := b[j], b[(j+1)%nb]
			if !nearVec(a0, b1, eps) || !nearVec(a1, b0, eps) {
				continue
			}
			merged := make([]vec, 0, na+nb-2)
			for k := 1; k <= na; k++ {
				merged = append(merged, a[(i+k)%na])
			}
			for k := 2; k < nb; k++ {
				merged = append(merged, b[(j+k)%nb])
			}
			merged = simplifyCollinear(merged, eps)
			if len(merged) < 3 || len(merged) > physics.MaxPolygonVertices {
				continue
			}
			if signedArea(merged) <= eps*eps || !isConvexPoly(merged, eps) {
				continue
			}
			return merged, true
		}
	}
	return nil, false
}

type polyBounds struct {
	minX, minY, maxX, maxY float64
}

func computePolyBounds(pts []vec) polyBounds {
	if len(pts) == 0 {
		return polyBounds{}
	}
	b := polyBounds{minX: pts[0].x, minY: pts[0].y, maxX: pts[0].x, maxY: pts[0].y}
	for _, p := range pts[1:] {
		b.minX = min(b.minX, p.x)
		b.minY = min(b.minY, p.y)
		b.maxX = max(b.maxX, p.x)
		b.maxY = max(b.maxY, p.y)
	}
	return b
}

func (a polyBounds) overlaps(b polyBounds, eps float64) bool {
	return a.minX <= b.maxX+eps && b.minX <= a.maxX+eps && a.minY <= b.maxY+eps && b.minY <= a.maxY+eps
}

// mergeConvexPolys greedily merges adjacent convex polygons that share an edge whenever their union
// remains convex with at most physics.MaxPolygonVertices vertices.
func mergeConvexPolys(in []poly, eps float64) []poly {
	if len(in) <= 1 {
		return in
	}
	out := make([]poly, len(in))
	convex := make([]bool, len(in))
	bounds := make([]polyBounds, len(in))
	for i, p := range in {
		out[i] = p
		convex[i] = len(p.pts) <= physics.MaxPolygonVertices && isConvexPoly(p.pts, eps)
		if convex[i] {
			bounds[i] = computePolyBounds(p.pts)
		}
	}
	for {
		merged := false
		for i := 0; i < len(out); i++ {
			if !convex[i] {
				continue
			}
			for j := i + 1; j < len(out); {
				if !convex[j] || !bounds[i].overlaps(bounds[j], eps) {
					j++
					continue
				}
				if m, ok := tryMergeConvexPolys(out[i].pts, out[j].pts, eps); ok {
					out[i] = poly{pts: m}
					bounds[i] = computePolyBounds(m)
					out = slices.Delete(out, j, j+1)
					convex = slices.Delete(convex, j, j+1)
					bounds = slices.Delete(bounds, j, j+1)
					merged = true
					continue
				}
				j++
			}
		}
		if !merged {
			break
		}
	}
	return out
}

// polyToPhysicsShape converts a clockwise world-space polygon into a convex physics.Polygon
// when convex with at most physics.MaxPolygonVertices vertices, or a closed physics.Chain otherwise.
func polyToPhysicsShape(p poly, eps float64) (physics.Shape, bool) {
	if len(p.pts) < 3 {
		return physics.Shape{}, false
	}
	pts := make([]core.Point, len(p.pts))
	for i, v := range p.pts {
		pts[i] = core.Point{X: core.Points(v.x), Y: core.Points(v.y)}
	}
	if len(pts) <= physics.MaxPolygonVertices && isConvexPoly(p.pts, eps) {
		if hp, ok := physics.NewPolygon(pts, 0); ok {
			return physics.PolygonShape(hp), true
		}
	}
	s := physics.ChainShape(pts, true)
	return s, !s.Empty()
}
