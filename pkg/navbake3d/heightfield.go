package navbake3d

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// The bake follows Recast's pipeline (github.com/recastnavigation/recastnavigation, zlib license): rasterize the level's triangles into a heightfield of solid spans, keep the tops a walking agent fits on, erode them by the agent's radius, partition them into regions, trace and simplify the regions' outlines, and split the outlines into convex polygons with height detail.

// Grid directions, in Recast's order: -X, +Z, +X, -Z.
var (
	dirOffsetX = [4]int{-1, 0, 1, 0}
	dirOffsetZ = [4]int{0, 1, 0, -1}
)

// spanMaxHeight is the top of the heightfield in cell heights, which an open span reaches.
const spanMaxHeight = math.MaxInt32 / 4

// areaNull marks a span no agent walks on; areaWalkable one it may.
const (
	areaNull     uint8 = 0
	areaWalkable uint8 = 1
)

// span is a run of solid cells in one column of the heightfield, from smin to smax cell heights above its bottom, in a list sorted bottom to top.
type span struct {
	smin, smax int32
	area       uint8
	next       int32
}

// heightfield is the solid voxelization of the level: a grid of columns of spans.
type heightfield struct {
	width, depth int
	bmin, bmax   math3d.Vec3
	cs, ch       float32
	heads        []int32 // first span of each column, -1 when empty
	pool         []span
	free         int32 // head of the list of freed spans, -1 when none
}

func newHeightfield(width, depth int, bmin, bmax math3d.Vec3, cs, ch float32) *heightfield {
	hf := &heightfield{width: width, depth: depth, bmin: bmin, bmax: bmax, cs: cs, ch: ch, free: -1}
	hf.heads = make([]int32, width*depth)
	for i := range hf.heads {
		hf.heads[i] = -1
	}
	return hf
}

func (hf *heightfield) alloc(s span) int32 {
	if hf.free >= 0 {
		i := hf.free
		hf.free = hf.pool[i].next
		hf.pool[i] = s
		return i
	}
	hf.pool = append(hf.pool, s)
	return int32(len(hf.pool) - 1)
}

func (hf *heightfield) release(i int32) {
	hf.pool[i].next = hf.free
	hf.free = i
}

// addSpan merges the solid run [smin, smax] into column (x, z). Overlapping spans join, and the joined span takes the larger area of the spans whose tops lie within mergeThr of its top, so a walkable floor is not hidden by the wall it touches.
func (hf *heightfield) addSpan(x, z int, smin, smax int32, area uint8, mergeThr int32) {
	col := x + z*hf.width
	ns := span{smin: smin, smax: smax, area: area, next: -1}
	prev := int32(-1)
	cur := hf.heads[col]
	for cur >= 0 {
		c := hf.pool[cur]
		if c.smin > ns.smax {
			break
		}
		if c.smax < ns.smin {
			prev = cur
			cur = c.next
			continue
		}
		if c.smin < ns.smin {
			ns.smin = c.smin
		}
		if c.smax > ns.smax {
			ns.smax = c.smax
		}
		if abs32(ns.smax-c.smax) <= mergeThr && c.area > ns.area {
			ns.area = c.area
		}
		next := c.next
		hf.release(cur)
		if prev >= 0 {
			hf.pool[prev].next = next
		} else {
			hf.heads[col] = next
		}
		cur = next
	}
	ni := hf.alloc(ns)
	if prev >= 0 {
		hf.pool[ni].next = hf.pool[prev].next
		hf.pool[prev].next = ni
	} else {
		hf.pool[ni].next = hf.heads[col]
		hf.heads[col] = ni
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// clipPoly splits the convex polygon in at the plane where coordinate axis (0 for X, 2 for Z) equals x: below gets the part at or below x and above the part at or above it. Points on the plane go to both.
func clipPoly(in []math3d.Vec3, x float32, axis int, below, above []math3d.Vec3) ([]math3d.Vec3, []math3d.Vec3) {
	below, above = below[:0], above[:0]
	n := len(in)
	if n == 0 {
		return below, above
	}
	coord := func(v math3d.Vec3) float32 {
		if axis == 0 {
			return v.X
		}
		return v.Z
	}
	var d [16]float32
	for i := range in {
		d[i] = x - coord(in[i])
	}
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		ina := d[j] >= 0
		inb := d[i] >= 0
		if ina != inb {
			s := d[j] / (d[j] - d[i])
			p := math3d.Vec3{
				X: in[j].X + float32((in[i].X-in[j].X)*s),
				Y: in[j].Y + float32((in[i].Y-in[j].Y)*s),
				Z: in[j].Z + float32((in[i].Z-in[j].Z)*s),
			}
			below = append(below, p)
			above = append(above, p)
			if d[i] > 0 {
				below = append(below, in[i])
			} else if d[i] < 0 {
				above = append(above, in[i])
			}
			continue
		}
		if d[i] >= 0 {
			below = append(below, in[i])
			if d[i] != 0 {
				continue
			}
		}
		above = append(above, in[i])
	}
	return below, above
}

// rasterizeTriangle calls emit with the column and the solid range, in cell heights, that the triangle (a, b, c) covers in every cell it touches.
func (hf *heightfield) rasterizeTriangle(a, b, c math3d.Vec3, buf *rasterBuffers, emit func(x, z int, smin, smax int32)) {
	tmin := a.Min(b).Min(c)
	tmax := a.Max(b).Max(c)
	if tmin.X > hf.bmax.X || tmax.X < hf.bmin.X || tmin.Y > hf.bmax.Y || tmax.Y < hf.bmin.Y || tmin.Z > hf.bmax.Z || tmax.Z < hf.bmin.Z {
		return
	}
	ics := 1 / hf.cs
	ich := 1 / hf.ch
	by := hf.bmax.Y - hf.bmin.Y

	z0 := int(math.Floor(float64((tmin.Z - hf.bmin.Z) * ics)))
	z1 := int(math.Floor(float64((tmax.Z - hf.bmin.Z) * ics)))
	z0 = clampInt(z0, -1, hf.depth-1)
	z1 = clampInt(z1, 0, hf.depth-1)

	in := append(buf.in[:0], a, b, c)
	for z := z0; z <= z1; z++ {
		cz := hf.bmin.Z + float32(float32(z)*hf.cs)
		var row []math3d.Vec3
		row, buf.rest = clipPoly(in, cz+hf.cs, 2, buf.row, buf.rest)
		buf.row = row
		in, buf.rest = buf.rest, in
		buf.in = in
		if len(row) < 3 || z < 0 {
			continue
		}
		minX, maxX := row[0].X, row[0].X
		for _, v := range row[1:] {
			minX = min(minX, v.X)
			maxX = max(maxX, v.X)
		}
		x0 := int(math.Floor(float64((minX - hf.bmin.X) * ics)))
		x1 := int(math.Floor(float64((maxX - hf.bmin.X) * ics)))
		if x1 < 0 || x0 >= hf.width {
			continue
		}
		x0 = clampInt(x0, -1, hf.width-1)
		x1 = clampInt(x1, 0, hf.width-1)
		rowIn := row
		for x := x0; x <= x1; x++ {
			cx := hf.bmin.X + float32(float32(x)*hf.cs)
			var cell []math3d.Vec3
			cell, buf.cellRest = clipPoly(rowIn, cx+hf.cs, 0, buf.cell, buf.cellRest)
			buf.cell = cell
			rowIn, buf.cellRest = buf.cellRest, rowIn
			if len(cell) < 3 || x < 0 {
				continue
			}
			smin, smax := cell[0].Y, cell[0].Y
			for _, v := range cell[1:] {
				smin = min(smin, v.Y)
				smax = max(smax, v.Y)
			}
			smin -= hf.bmin.Y
			smax -= hf.bmin.Y
			if smax < 0 || smin > by {
				continue
			}
			smin = max(smin, 0)
			smax = min(smax, by)
			ismin := clampInt32(int32(math.Floor(float64(smin*ich))), 0, spanMaxHeight)
			ismax := clampInt32(int32(math.Ceil(float64(smax*ich))), ismin+1, spanMaxHeight)
			emit(x, z, ismin, ismax)
		}
		buf.row = rowIn
	}
}

// solidColumn is the range a closed convex solid fills in one column, and the area of its top.
type solidColumn struct {
	col        int
	smin, smax int32
	area       uint8
}

// rasterizeSolid adds a closed convex solid, given by its surface triangles and their areas: each column it touches is filled from the lowest to the highest point of its surface there, so the inside of a box is solid rather than a floor under a roof, and the span's area is that of the triangle at its top.
func (hf *heightfield) rasterizeSolid(verts []math3d.Vec3, areas []uint8, mergeThr int32, buf *rasterBuffers) {
	cols := make(map[int]int)
	var acc []solidColumn
	for t := range areas {
		area := areas[t]
		hf.rasterizeTriangle(verts[t*3], verts[t*3+1], verts[t*3+2], buf, func(x, z int, smin, smax int32) {
			col := x + z*hf.width
			i, ok := cols[col]
			if !ok {
				cols[col] = len(acc)
				acc = append(acc, solidColumn{col: col, smin: smin, smax: smax, area: area})
				return
			}
			c := &acc[i]
			c.smin = min(c.smin, smin)
			switch {
			case smax > c.smax:
				c.smax, c.area = smax, area
			case smax == c.smax && area > c.area:
				c.area = area
			}
		})
	}
	for _, c := range acc {
		hf.addSpan(c.col%hf.width, c.col/hf.width, c.smin, c.smax, c.area, mergeThr)
	}
}

// rasterBuffers are the scratch polygons rasterizeTriangle clips into, reused across triangles.
type rasterBuffers struct {
	in, row, rest, cell, cellRest []math3d.Vec3
}

func newRasterBuffers() *rasterBuffers {
	mk := func() []math3d.Vec3 { return make([]math3d.Vec3, 0, 16) }
	return &rasterBuffers{in: mk(), row: mk(), rest: mk(), cell: mk(), cellRest: mk()}
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

func clampInt32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// filterLowHangingObstacles lets an agent step onto a low obstacle, such as a curb or a stair's riser, whose top is within climb of the walkable span below it, by marking the obstacle walkable.
func (hf *heightfield) filterLowHangingObstacles(climb int32) {
	for col := range hf.heads {
		prevWalkable := false
		prevArea := areaNull
		prevTop := int32(0)
		for i := hf.heads[col]; i >= 0; i = hf.pool[i].next {
			s := &hf.pool[i]
			walkable := s.area != areaNull
			if !walkable && prevWalkable && abs32(s.smax-prevTop) <= climb {
				s.area = prevArea
			}
			prevWalkable = walkable
			prevArea = s.area
			prevTop = s.smax
		}
	}
}

// filterLedges removes walkable spans at a drop deeper than climb, and spans whose neighbours' tops differ by more than climb, so the mesh keeps off cliff edges.
func (hf *heightfield) filterLedges(height, climb int32) {
	for z := 0; z < hf.depth; z++ {
		for x := 0; x < hf.width; x++ {
			for i := hf.heads[x+z*hf.width]; i >= 0; i = hf.pool[i].next {
				s := &hf.pool[i]
				if s.area == areaNull {
					continue
				}
				bot := s.smax
				top := int32(spanMaxHeight)
				if s.next >= 0 {
					top = hf.pool[s.next].smin
				}
				minh := int32(spanMaxHeight)
				asmin, asmax := s.smax, s.smax
				for dir := range 4 {
					dx := x + dirOffsetX[dir]
					dz := z + dirOffsetZ[dir]
					if dx < 0 || dz < 0 || dx >= hf.width || dz >= hf.depth {
						minh = min(minh, -climb-bot)
						continue
					}
					ns := hf.heads[dx+dz*hf.width]
					nbot := -climb
					ntop := int32(spanMaxHeight)
					if ns >= 0 {
						ntop = hf.pool[ns].smin
					}
					if min(top, ntop)-max(bot, nbot) > height {
						minh = min(minh, nbot-bot)
					}
					for ; ns >= 0; ns = hf.pool[ns].next {
						n := hf.pool[ns]
						nbot = n.smax
						ntop = spanMaxHeight
						if n.next >= 0 {
							ntop = hf.pool[n.next].smin
						}
						if min(top, ntop)-max(bot, nbot) > height {
							minh = min(minh, nbot-bot)
							if abs32(nbot-bot) <= climb {
								asmin = min(asmin, nbot)
								asmax = max(asmax, nbot)
							}
						}
					}
				}
				if minh < -climb || asmax-asmin > climb {
					s.area = areaNull
				}
			}
		}
	}
}

// filterLowCeilings removes walkable spans with less than height cells of clearance above them.
func (hf *heightfield) filterLowCeilings(height int32) {
	for col := range hf.heads {
		for i := hf.heads[col]; i >= 0; i = hf.pool[i].next {
			s := &hf.pool[i]
			top := int32(spanMaxHeight)
			if s.next >= 0 {
				top = hf.pool[s.next].smin
			}
			if top-s.smax < height {
				s.area = areaNull
			}
		}
	}
}
