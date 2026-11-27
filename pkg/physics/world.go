package physics

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/internal/detmath"
	"github.com/nsgo-dev/nsgo/core"
)

// QueryMask selects which targets a query inspects. A zero QueryMask is treated as QueryAll.
type QueryMask uint32

const (
	// QueryBodies includes bodies in World.Bodies.
	QueryBodies QueryMask = 1 << iota
	// QueryTiles includes solid tiles (TileSolid) in World.Tiles.
	QueryTiles
	// QueryOneWay includes one-way platform tiles (TileOneWay) in World.Tiles.
	// During raycasts, a one-way tile only hits when the ray enters its top edge moving downward.
	QueryOneWay
	// QueryAll includes bodies, solid tiles and one-way tiles.
	QueryAll = QueryBodies | QueryTiles | QueryOneWay
)

// QuerySensors includes RigidBody sensors in RigidWorld queries, which QueryBodies and QueryAll leave out. World queries ignore it.
const QuerySensors QueryMask = 1 << 3

// OverlapHit is one body or tile overlapping a query shape.
type OverlapHit struct {
	// Body is non-nil when the hit is a body from World.Bodies.
	Body *Body
	// RigidBody is non-nil when the hit is a body from RigidWorld.Bodies.
	RigidBody *RigidBody
	// Tile is true when the hit is a tile from World.Tiles or RigidWorld.Tiles at (Col, Row).
	Tile      bool
	Col, Row  int
	TileFlags TileFlags
	// Contact holds the overlap normal (pointing from the hit toward the query shape), depth and contact point.
	Contact Contact
}

// RaycastHit is one intersection of a ray segment with a body or tile.
type RaycastHit struct {
	// T is the normalized distance along the ray from From (0) to To (1).
	T float32
	// Point is the world-space intersection point.
	Point core.Point
	// NormalX and NormalY form the unit surface normal at Point, or (0, 0) when the ray starts inside a shape. Segments and chains are hit from either side, with the normal facing the ray's start; a ray running along one misses it.
	NormalX, NormalY float32
	// Body is non-nil when the hit is a body from World.Bodies.
	Body *Body
	// RigidBody is non-nil when the hit is a body from RigidWorld.Bodies.
	RigidBody *RigidBody
	// Tile is true when the hit is a tile from World.Tiles or RigidWorld.Tiles at (Col, Row).
	Tile      bool
	Col, Row  int
	TileFlags TileFlags
}

// World holds bodies, triggers, an optional TileGrid and an optional RigidWorld for spatial queries and MoveAndSlide.
type World struct {
	Bodies   []*Body
	Triggers []*Trigger
	Tiles    TileGrid
	// RigidWorld is an optional RigidWorld whose static RigidBody colliders participate in World.MoveAndSlide.
	RigidWorld *RigidWorld
	// TileLayer and TileMask optionally filter tile collisions during World.MoveAndSlide (zero defaults to LayerAll).
	TileLayer LayerMask
	TileMask  LayerMask
	GravityX  float32
	GravityY  float32

	triggerActive map[*Trigger][]*Body
}

// Add appends non-nil bodies to w.Bodies.
func (w *World) Add(bodies ...*Body) {
	for _, b := range bodies {
		if b != nil {
			w.Bodies = append(w.Bodies, b)
		}
	}
}

// Remove deletes the first occurrence of b from w.Bodies, preserving the order of remaining bodies.
func (w *World) Remove(b *Body) bool {
	for i, existing := range w.Bodies {
		if existing == b {
			w.Bodies = append(w.Bodies[:i], w.Bodies[i+1:]...)
			return true
		}
	}
	return false
}

// QueryOverlap returns all bodies and tiles overlapping shape in deterministic order:
// matching bodies in World.Bodies order followed by matching tiles in row-major order.
func (w *World) QueryOverlap(shape Shape) []OverlapHit {
	return w.QueryOverlapMask(shape, QueryAll)
}

// QueryOverlapMask returns all bodies and tiles matching mask that overlap shape in deterministic order.
func (w *World) QueryOverlapMask(shape Shape, mask QueryMask) []OverlapHit {
	return w.QueryOverlapLayer(shape, mask, LayerAll)
}

// QueryOverlapLayer returns all bodies and tiles matching mask and layerMask that overlap shape in deterministic order.
// A zero layerMask defaults to LayerAll; bodies match when their effective Layer shares at least one bit with layerMask.
func (w *World) QueryOverlapLayer(shape Shape, mask QueryMask, layerMask LayerMask) []OverlapHit {
	if shape.Empty() {
		return nil
	}
	if mask == 0 {
		mask = QueryAll
	}
	layerMask = effectiveLayer(layerMask)
	var hits []OverlapHit

	if mask&QueryBodies != 0 {
		for _, b := range w.Bodies {
			if b == nil || b.Shape.Empty() || (b.layer()&layerMask) == 0 {
				continue
			}
			if c := TestShapes(shape, b.Shape); c.Hit {
				hits = append(hits, OverlapHit{Body: b, Contact: c})
			}
		}
	}

	return appendTileOverlaps(hits, w.Tiles, shape, mask)
}

// appendTileOverlaps appends to hits the tiles of grid matching mask that overlap shape, in row-major order.
func appendTileOverlaps(hits []OverlapHit, grid TileGrid, shape Shape, mask QueryMask) []OverlapHit {
	if grid == nil || mask&(QueryTiles|QueryOneWay) == 0 {
		return hits
	}
	tileW, tileH := grid.TileSize()
	cols, rows := grid.GridSize()
	if !(tileW > 0 && tileH > 0 && cols > 0 && rows > 0) {
		return hits
	}
	bounds := shape.Bounds()
	tw, th := float64(tileW), float64(tileH)
	ox, oy := tileGridOrigin(grid)
	ox32, oy32 := float32(ox), float32(oy)
	minCol := max(0, int(math.Floor((float64(bounds.X)-ox)/tw)))
	maxCol := min(cols-1, int(math.Ceil((float64(bounds.X)-ox+float64(bounds.W))/tw))-1)
	minRow := max(0, int(math.Floor((float64(bounds.Y)-oy)/th)))
	maxRow := min(rows-1, int(math.Ceil((float64(bounds.Y)-oy+float64(bounds.H))/th))-1)

	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			flags := grid.TileFlagsAt(col, row)
			if !tileMatchesOverlap(flags, mask) {
				continue
			}
			tileBox := Box{
				X: ox32 + float32(float32(col)*tileW),
				Y: oy32 + float32(float32(row)*tileH),
				W: tileW,
				H: tileH,
			}
			if c := TestShapes(shape, BoxShape(tileBox)); c.Hit {
				hits = append(hits, OverlapHit{
					Tile:      true,
					Col:       col,
					Row:       row,
					TileFlags: flags,
					Contact:   c,
				})
			}
		}
	}
	return hits
}

// QueryPoint returns all bodies in World.Bodies whose shape contains (x, y), in World.Bodies order.
func (w *World) QueryPoint(x, y float32) []*Body {
	return w.QueryPointLayer(x, y, LayerAll)
}

// QueryPointLayer returns all bodies in World.Bodies matching layerMask whose shape contains (x, y), in World.Bodies order.
func (w *World) QueryPointLayer(x, y float32, layerMask LayerMask) []*Body {
	layerMask = effectiveLayer(layerMask)
	var hits []*Body
	for _, b := range w.Bodies {
		if b != nil && (b.layer()&layerMask) != 0 && b.Shape.Contains(x, y) {
			hits = append(hits, b)
		}
	}
	return hits
}

// Raycast casts a segment from from to to against targets selected by mask and returns every hit
// sorted by ascending distance T in [0, 1]. Equal distances keep bodies (in World.Bodies order)
// before tiles (in traversal order).
func (w *World) Raycast(from, to core.Point, mask QueryMask) []RaycastHit {
	return w.RaycastLayer(from, to, mask, LayerAll)
}

// RaycastLayer casts a segment from from to to against targets selected by mask and layerMask
// and returns every hit sorted by ascending distance T in [0, 1].
func (w *World) RaycastLayer(from, to core.Point, mask QueryMask, layerMask LayerMask) []RaycastHit {
	if mask == 0 {
		mask = QueryAll
	}
	layerMask = effectiveLayer(layerMask)
	fx, fy := float64(from.X), float64(from.Y)
	dx, dy := float64(to.X)-fx, float64(to.Y)-fy

	var hits []RaycastHit
	if mask&QueryBodies != 0 {
		for _, b := range w.Bodies {
			if b == nil || b.Shape.Empty() || (b.layer()&layerMask) == 0 {
				continue
			}
			if h, ok := raycastShape(fx, fy, dx, dy, b.Shape); ok {
				h.Body = b
				hits = append(hits, h)
			}
		}
	}

	if w.Tiles != nil && mask&(QueryTiles|QueryOneWay) != 0 {
		tileHits := raycastTiles(fx, fy, dx, dy, w.Tiles, mask, 1.0, false)
		hits = append(hits, tileHits...)
	}

	slices.SortStableFunc(hits, func(a, b RaycastHit) int {
		return cmp.Compare(a.T, b.T)
	})
	return hits
}

// RaycastFirst returns the closest hit along the segment from from to to matching mask.
func (w *World) RaycastFirst(from, to core.Point, mask QueryMask) (RaycastHit, bool) {
	return w.RaycastFirstLayer(from, to, mask, LayerAll)
}

// RaycastFirstLayer returns the closest hit along the segment from from to to matching mask and layerMask.
func (w *World) RaycastFirstLayer(from, to core.Point, mask QueryMask, layerMask LayerMask) (RaycastHit, bool) {
	if mask == 0 {
		mask = QueryAll
	}
	layerMask = effectiveLayer(layerMask)
	fx, fy := float64(from.X), float64(from.Y)
	dx, dy := float64(to.X)-fx, float64(to.Y)-fy

	var best RaycastHit
	hasBest := false
	maxT := 1.0

	if mask&QueryBodies != 0 {
		for _, b := range w.Bodies {
			if b == nil || b.Shape.Empty() || (b.layer()&layerMask) == 0 {
				continue
			}
			if h, ok := raycastShape(fx, fy, dx, dy, b.Shape); ok {
				if !hasBest || h.T < best.T {
					h.Body = b
					best = h
					hasBest = true
					maxT = float64(h.T)
				}
			}
		}
	}

	if w.Tiles != nil && mask&(QueryTiles|QueryOneWay) != 0 {
		tileHits := raycastTiles(fx, fy, dx, dy, w.Tiles, mask, maxT, true)
		if len(tileHits) > 0 {
			h := tileHits[0]
			if !hasBest || h.T < best.T {
				best = h
				hasBest = true
			}
		}
	}

	return best, hasBest
}

func tileMatchesOverlap(flags TileFlags, mask QueryMask) bool {
	return (flags&TileSolid != 0 && mask&QueryTiles != 0) ||
		(flags&TileOneWay != 0 && mask&QueryOneWay != 0)
}

func tileMatchesRay(flags TileFlags, mask QueryMask, ny float32) bool {
	if flags&TileSolid != 0 && mask&QueryTiles != 0 {
		return true
	}
	if flags&TileOneWay != 0 && mask&QueryOneWay != 0 && ny < 0 {
		return true
	}
	return false
}

func raycastShape(fx, fy, dx, dy float64, s Shape) (RaycastHit, bool) {
	switch s.Kind {
	case ShapeBox:
		return raycastBox(fx, fy, dx, dy, s.Box)
	case ShapeCircle:
		return raycastCircle(fx, fy, dx, dy, s.Circle)
	case ShapeChain:
		return raycastChain(fx, fy, dx, dy, s.Chain)
	default:
		h, ok := shapeHull(s)
		if !ok {
			return RaycastHit{}, false
		}
		return raycastHull(fx, fy, dx, dy, &h)
	}
}

func raycastBox(fx, fy, dx, dy float64, b Box) (RaycastHit, bool) {
	if b.Empty() {
		return RaycastHit{}, false
	}
	minX, maxX := float64(b.X), float64(b.X)+float64(b.W)
	minY, maxY := float64(b.Y), float64(b.Y)+float64(b.H)

	if dx == 0 && dy == 0 {
		if fx >= minX && fx <= maxX && fy >= minY && fy <= maxY {
			return RaycastHit{
				T:     0,
				Point: core.Point{X: core.Points(fx), Y: core.Points(fy)},
			}, true
		}
		return RaycastHit{}, false
	}

	tNear := math.Inf(-1)
	tFar := math.Inf(1)
	var nx, ny float32

	if dx == 0 {
		if fx < minX || fx > maxX {
			return RaycastHit{}, false
		}
	} else {
		inv := 1.0 / dx
		t1 := (minX - fx) * inv
		t2 := (maxX - fx) * inv
		sign := float32(-1)
		if t1 > t2 {
			t1, t2 = t2, t1
			sign = 1
		}
		if t1 > tNear {
			tNear = t1
			nx, ny = sign, 0
		}
		if t2 < tFar {
			tFar = t2
		}
	}

	if dy == 0 {
		if fy < minY || fy > maxY {
			return RaycastHit{}, false
		}
	} else {
		inv := 1.0 / dy
		t1 := (minY - fy) * inv
		t2 := (maxY - fy) * inv
		sign := float32(-1)
		if t1 > t2 {
			t1, t2 = t2, t1
			sign = 1
		}
		if t1 > tNear {
			tNear = t1
			nx, ny = 0, sign
		}
		if t2 < tFar {
			tFar = t2
		}
	}

	if tNear > tFar || tFar < 0 || tNear > 1 {
		return RaycastHit{}, false
	}
	if tNear < 0 {
		// Ray started on an exit edge moving outward: zero interior intersection.
		if tFar == 0 {
			return RaycastHit{}, false
		}
		return RaycastHit{
			T:     0,
			Point: core.Point{X: core.Points(fx), Y: core.Points(fy)},
		}, true
	}
	return RaycastHit{
		T:       float32(tNear),
		Point:   core.Point{X: core.Points(fx + float64(dx*tNear)), Y: core.Points(fy + float64(dy*tNear))},
		NormalX: nx,
		NormalY: ny,
	}, true
}

func raycastCircle(fx, fy, dx, dy float64, c Circle) (RaycastHit, bool) {
	if c.Empty() {
		return RaycastHit{}, false
	}
	cx, cy, r := float64(c.X), float64(c.Y), float64(c.R)
	ox, oy := fx-cx, fy-cy
	rSq := float64(r * r)
	distSq := float64(ox*ox) + float64(oy*oy)

	if distSq < rSq {
		return RaycastHit{
			T:     0,
			Point: core.Point{X: core.Points(fx), Y: core.Points(fy)},
		}, true
	}
	if distSq == rSq {
		if (dx == 0 && dy == 0) || float64(ox*dx)+float64(oy*dy) < 0 {
			return RaycastHit{
				T:       0,
				Point:   core.Point{X: core.Points(fx), Y: core.Points(fy)},
				NormalX: float32(ox / r),
				NormalY: float32(oy / r),
			}, true
		}
		return RaycastHit{}, false
	}

	a := float64(dx*dx) + float64(dy*dy)
	if a == 0 {
		return RaycastHit{}, false
	}
	b := float64(ox*dx) + float64(oy*dy)
	if b >= 0 {
		return RaycastHit{}, false
	}
	cVal := distSq - rSq
	disc := float64(b*b) - float64(a*cVal)
	if disc < 0 {
		return RaycastHit{}, false
	}
	t := (-b - math.Sqrt(disc)) / a
	if t < 0 || t > 1 {
		return RaycastHit{}, false
	}
	px := fx + float64(dx*t)
	py := fy + float64(dy*t)
	nx, ny := px-cx, py-cy
	if l := detmath.Hypot(nx, ny); l > 0 {
		nx /= l
		ny /= l
	}
	return RaycastHit{
		T:       float32(t),
		Point:   core.Point{X: core.Points(px), Y: core.Points(py)},
		NormalX: float32(nx),
		NormalY: float32(ny),
	}, true
}

// raycastTiles traverses grid with 2D DDA along [0, maxT] and returns all matching tile hits (or only the first when stopAtFirst is true).
func raycastTiles(fx, fy, dx, dy float64, grid TileGrid, mask QueryMask, maxT float64, stopAtFirst bool) []RaycastHit {
	tileW, tileH := grid.TileSize()
	cols, rows := grid.GridSize()
	if tileW <= 0 || tileH <= 0 || cols <= 0 || rows <= 0 {
		return nil
	}
	tw, th := float64(tileW), float64(tileH)
	ox, oy := tileGridOrigin(grid)
	fx -= ox
	fy -= oy
	gridW, gridH := float64(cols)*tw, float64(rows)*th

	// Clip the segment [0, maxT] to the grid bounds [0, gridW] x [0, gridH].
	tStart := 0.0
	tEnd := maxT
	var entryNX, entryNY float32

	if dx == 0 {
		if fx < 0 || fx > gridW {
			return nil
		}
	} else {
		inv := 1.0 / dx
		t1 := (0 - fx) * inv
		t2 := (float64(gridW) - fx) * inv
		sign := float32(-1)
		if t1 > t2 {
			t1, t2 = t2, t1
			sign = 1
		}
		if t1 > tStart {
			tStart = t1
			entryNX, entryNY = sign, 0
		}
		if t2 < tEnd {
			tEnd = t2
		}
	}

	if dy == 0 {
		if fy < 0 || fy > gridH {
			return nil
		}
	} else {
		inv := 1.0 / dy
		t1 := (0 - fy) * inv
		t2 := (float64(gridH) - fy) * inv
		sign := float32(-1)
		if t1 > t2 {
			t1, t2 = t2, t1
			sign = 1
		}
		if t1 > tStart {
			tStart = t1
			entryNX, entryNY = 0, sign
		}
		if t2 < tEnd {
			tEnd = t2
		}
	}

	if tStart > tEnd || tEnd < 0 || tStart > maxT {
		return nil
	}

	startX := min(max(fx+float64(dx*tStart), 0), gridW)
	startY := min(max(fy+float64(dy*tStart), 0), gridH)
	if tStart > 0 {
		if entryNX < 0 {
			startX = 0
		} else if entryNX > 0 {
			startX = gridW
		}
		if entryNY < 0 {
			startY = 0
		} else if entryNY > 0 {
			startY = gridH
		}
	}

	// Zero-length ray inside the grid.
	if dx == 0 && dy == 0 {
		col := int(math.Floor(startX / tw))
		row := int(math.Floor(startY / th))
		if col == cols && startX == gridW {
			col = cols - 1
		}
		if row == rows && startY == gridH {
			row = rows - 1
		}
		if col < 0 || col >= cols || row < 0 || row >= rows {
			return nil
		}
		flags := grid.TileFlagsAt(col, row)
		if flags&TileSolid != 0 && mask&QueryTiles != 0 {
			return []RaycastHit{{
				T:         0,
				Point:     core.Point{X: core.Points(ox + startX), Y: core.Points(oy + startY)},
				Tile:      true,
				Col:       col,
				Row:       row,
				TileFlags: flags,
			}}
		}
		return nil
	}

	// Determine the first cell entered at tStart.
	col := int(math.Floor(startX / tw))
	if dx < 0 && float64(col)*tw == startX {
		col--
	}
	if col < 0 || col >= cols {
		return nil
	}
	row := int(math.Floor(startY / th))
	if dy < 0 && float64(row)*th == startY {
		row--
	}
	if row < 0 || row >= rows {
		return nil
	}

	// Determine the normal on the first cell if the ray starts on its entry boundary.
	nx, ny := entryNX, entryNY
	if tStart == 0 {
		if dx > 0 && float64(col)*tw == startX {
			nx, ny = -1, 0
		} else if dx < 0 && float64(col+1)*tw == startX {
			nx, ny = 1, 0
		}
		if dy > 0 && float64(row)*th == startY {
			nx, ny = 0, -1
		} else if dy < 0 && float64(row+1)*th == startY {
			nx, ny = 0, 1
		}
	}

	var hits []RaycastHit
	if flags := grid.TileFlagsAt(col, row); tileMatchesRay(flags, mask, ny) {
		hits = append(hits, RaycastHit{
			T:         float32(tStart),
			Point:     core.Point{X: core.Points(ox + startX), Y: core.Points(oy + startY)},
			NormalX:   nx,
			NormalY:   ny,
			Tile:      true,
			Col:       col,
			Row:       row,
			TileFlags: flags,
		})
		if stopAtFirst {
			return hits
		}
	}

	var stepX, stepY int
	var invDX, invDY float64
	tMaxX := math.Inf(1)
	tMaxY := math.Inf(1)

	if dx > 0 {
		stepX = 1
		invDX = 1.0 / dx
		tMaxX = (float64(float64(col+1)*tw) - fx) * invDX
	} else if dx < 0 {
		stepX = -1
		invDX = 1.0 / dx
		tMaxX = (float64(float64(col)*tw) - fx) * invDX
	}

	if dy > 0 {
		stepY = 1
		invDY = 1.0 / dy
		tMaxY = (float64(float64(row+1)*th) - fy) * invDY
	} else if dy < 0 {
		stepY = -1
		invDY = 1.0 / dy
		tMaxY = (float64(float64(row)*th) - fy) * invDY
	}

	tLimit := min(maxT, tEnd)
	for {
		var tHit float64
		var hnx, hny float32
		if tMaxX <= tMaxY {
			if tMaxX > tLimit {
				break
			}
			tHit = tMaxX
			col += stepX
			if col < 0 || col >= cols {
				break
			}
			if stepX > 0 {
				hnx = -1
				tMaxX = (float64(float64(col+1)*tw) - fx) * invDX
			} else {
				hnx = 1
				tMaxX = (float64(float64(col)*tw) - fx) * invDX
			}
		} else {
			if tMaxY > tLimit {
				break
			}
			tHit = tMaxY
			row += stepY
			if row < 0 || row >= rows {
				break
			}
			if stepY > 0 {
				hny = -1
				tMaxY = (float64(float64(row+1)*th) - fy) * invDY
			} else {
				hny = 1
				tMaxY = (float64(float64(row)*th) - fy) * invDY
			}
		}

		if flags := grid.TileFlagsAt(col, row); tileMatchesRay(flags, mask, hny) {
			hits = append(hits, RaycastHit{
				T:         float32(tHit),
				Point:     core.Point{X: core.Points(ox + fx + float64(dx*tHit)), Y: core.Points(oy + fy + float64(dy*tHit))},
				NormalX:   hnx,
				NormalY:   hny,
				Tile:      true,
				Col:       col,
				Row:       row,
				TileFlags: flags,
			})
			if stopAtFirst {
				return hits
			}
		}
	}

	return hits
}
