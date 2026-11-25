package physics

import "math"

// DefaultTileFriction is the friction RigidWorld gives its Tiles when TileFriction is zero, the same as NewRigidBox gives a body.
const DefaultTileFriction = 0.4

const (
	// oneWayTileMargin is how far below the top of a one-way tile a body's lowest point may be, beyond the distance its velocity carries it down in a step, for the tile to catch it. A body deeper than that came from below or from the side and passes through.
	oneWayTileMargin = speculativeDistance
	// tileNormalTolerance is how far a contact normal may lean toward a tile face that a neighbouring solid tile covers and still count as leaving through the faces that are open.
	tileNormalTolerance = 1e-3
)

// Faces of a tile, numbered as the edges of boxHull.
const (
	tileFaceTop = iota
	tileFaceRight
	tileFaceBottom
	tileFaceLeft
)

// rigidTileGeom is the placement of a TileGrid in world space, or the zero value when there is no usable grid.
type rigidTileGeom struct {
	valid      bool
	cols, rows int
	tw, th     float64
	ox, oy     float64
}

// tileGeomOf returns the geometry of grid: valid when it has cells of positive finite size at a finite origin.
func tileGeomOf(grid TileGrid) rigidTileGeom {
	if grid == nil {
		return rigidTileGeom{}
	}
	tw, th := grid.TileSize()
	cols, rows := grid.GridSize()
	ox, oy := tileGridOrigin(grid)
	if !(tw > 0) || !(th > 0) || !isFinite32(tw) || !isFinite32(th) || cols <= 0 || rows <= 0 || math.IsNaN(ox) || math.IsInf(ox, 0) || math.IsNaN(oy) || math.IsInf(oy, 0) {
		return rigidTileGeom{}
	}
	return rigidTileGeom{valid: true, cols: cols, rows: rows, tw: float64(tw), th: float64(th), ox: ox, oy: oy}
}

// cell returns the sub that names tile (col, row) in contact manifolds and the contact cache.
func (g *rigidTileGeom) cell(col, row int) int { return row*g.cols + col }

// cellAt returns the column and row of the tile sub names.
func (g *rigidTileGeom) cellAt(sub int) (col, row int) { return sub % g.cols, sub / g.cols }

// cellBounds returns the world-space extents of tile (col, row). Neighbouring tiles share their edges exactly.
func (g *rigidTileGeom) cellBounds(col, row int) (minX, minY, maxX, maxY float64) {
	minX = g.ox + float64(float64(col)*g.tw)
	minY = g.oy + float64(float64(row)*g.th)
	maxX = g.ox + float64(float64(col+1)*g.tw)
	maxY = g.oy + float64(float64(row+1)*g.th)
	return minX, minY, maxX, maxY
}

// cellHull returns the hull of tile (col, row), wound as boxHull so its faces are numbered tileFaceTop to tileFaceLeft.
func (g *rigidTileGeom) cellHull(col, row int) hull {
	minX, minY, maxX, maxY := g.cellBounds(col, row)
	h := hull{count: 4}
	h.v[0], h.v[1], h.v[2], h.v[3] = vec2{minX, minY}, vec2{maxX, minY}, vec2{maxX, maxY}, vec2{minX, maxY}
	h.finish()
	return h
}

// cellRange returns the columns and rows of the tiles the box (minX, minY)-(maxX, maxY) meets, or false when it misses the grid.
func (g *rigidTileGeom) cellRange(minX, minY, maxX, maxY float64) (minCol, minRow, maxCol, maxRow int, ok bool) {
	clamp := func(v float64, n int) (int, bool) {
		if math.IsNaN(v) {
			return 0, false
		}
		return int(min(max(v, -1), float64(n))), true
	}
	c0, ok0 := clamp(math.Floor((minX-g.ox)/g.tw), g.cols)
	c1, ok1 := clamp(math.Floor((maxX-g.ox)/g.tw), g.cols)
	r0, ok2 := clamp(math.Floor((minY-g.oy)/g.th), g.rows)
	r1, ok3 := clamp(math.Floor((maxY-g.oy)/g.th), g.rows)
	if !ok0 || !ok1 || !ok2 || !ok3 {
		return 0, 0, 0, 0, false
	}
	minCol, maxCol = max(c0, 0), min(c1, g.cols-1)
	minRow, maxRow = max(r0, 0), min(r1, g.rows-1)
	return minCol, minRow, maxCol, maxRow, minCol <= maxCol && minRow <= maxRow
}

// tilesActive reports whether the current Step collides bodies with Tiles.
func (w *RigidWorld) tilesActive() bool { return w.tileGeom.valid && w.Tiles != nil }

// tileFlags returns the flags of tile (col, row), or 0 outside the grid.
func (w *RigidWorld) tileFlags(col, row int) TileFlags {
	g := &w.tileGeom
	if col < 0 || row < 0 || col >= g.cols || row >= g.rows {
		return 0
	}
	return w.Tiles.TileFlagsAt(col, row)
}

// solidTile reports whether tile (col, row) is TileSolid, so it covers the face of each neighbour it shares.
func (w *RigidWorld) solidTile(col, row int) bool {
	return w.tileFlags(col, row)&TileSolid != 0
}

// prepareTiles readies the body that stands for Tiles for this step. When the grid's geometry changed since the contact cache was built, the cache's tiles no longer name the same cells: their entries are dropped and the sleeping bodies that rested on them wake.
func (w *RigidWorld) prepareTiles() {
	if w.tileBody == nil {
		w.tileBody = &RigidBody{Static: true}
	}
	t := w.tileBody
	t.Static = true
	t.Friction = w.TileFriction
	if t.Friction == 0 {
		t.Friction = DefaultTileFriction
	}
	t.Restitution = w.TileRestitution
	t.Layer, t.Mask = w.TileLayer, w.TileMask

	geom := tileGeomOf(w.Tiles)
	if geom == w.tileGeom {
		return
	}
	w.tileGeom = geom
	kept := w.contacts[:0]
	for _, c := range w.contacts {
		if c.key.b == t {
			c.key.a.wakeIfSleeping()
			continue
		}
		kept = append(kept, c)
	}
	clear(w.contacts[len(kept):])
	w.contacts = kept
}

// tileContactChanged reports whether the tile that sub names stopped being one a body can rest on, which wakes a body sleeping on it.
func (w *RigidWorld) tileContactChanged(sub int) bool {
	if !w.tilesActive() {
		return true
	}
	col, row := w.tileGeom.cellAt(sub)
	return w.tileFlags(col, row)&(TileSolid|TileOneWay) == 0
}

// tileHullAt returns the hull of the tile sub names.
func (w *RigidWorld) tileHullAt(sub int) (hull, bool) {
	if !w.tilesActive() || sub < 0 {
		return hull{}, false
	}
	col, row := w.tileGeom.cellAt(sub)
	if row >= w.tileGeom.rows {
		return hull{}, false
	}
	h := w.tileGeom.cellHull(col, row)
	return h, h.finite()
}

// collidesWithTiles reports whether b is a body Tiles push this step: awake, dynamic, not a sensor, and in layers that interact with TileLayer and TileMask.
func (w *RigidWorld) collidesWithTiles(b *RigidBody) bool {
	return b.awakeDynamic() && !b.Sensor && !b.Shape.Empty() && CanCollideRigid(b, w.tileBody)
}

// detectTileContacts appends to manifolds the contacts of every awake dynamic body with Tiles, prepared for solving, in body order and, for each body, in row-major tile order.
func (w *RigidWorld) detectTileContacts(manifolds []rigidManifold, restitutionThresh, h float64) []rigidManifold {
	if !w.tilesActive() {
		return manifolds
	}
	// A one-way tile keeps holding a body it held on the last step, however deep the body has sunk.
	indexed := false
	held := func(key rigidPairKey) bool {
		if !indexed {
			w.indexContacts()
			indexed = true
		}
		_, ok := w.scratch.contactIndex[key]
		return ok
	}
	for _, b := range w.Bodies {
		if !w.collidesWithTiles(b) {
			continue
		}
		fall := max(0, float64(float64(b.VY)*h))
		first := len(manifolds)
		manifolds = w.collideTiles(b, manifolds, func(sub int, depth float64) bool {
			return held(rigidPairKey{a: b, b: w.tileBody, sub: sub}) || depth <= fall+oneWayTileMargin
		})
		for k := first; k < len(manifolds); k++ {
			prepareManifold(&manifolds[k], restitutionThresh, h)
		}
	}
	return manifolds
}

// correctTilePositions pushes every awake dynamic body out of the tiles it sinks into by more than slop, by percent of the depth, as the positional pass does for pairs of bodies, once per surface direction. A one-way tile pushes only a body it holds this step. buf is scratch, returned for reuse.
func (w *RigidWorld) correctTilePositions(buf []rigidManifold, slop, percent float64) []rigidManifold {
	if !w.tilesActive() {
		return buf
	}
	indexed := false
	for _, b := range w.Bodies {
		if !w.collidesWithTiles(b) {
			continue
		}
		buf = w.collideTiles(b, buf[:0], func(sub int, _ float64) bool {
			if !indexed {
				w.indexContacts()
				indexed = true
			}
			_, ok := w.scratch.contactIndex[rigidPairKey{a: b, b: w.tileBody, sub: sub}]
			return ok
		})
		// The tiles of one flat surface each report the body's full depth, so manifolds sharing a normal push once, by the deepest of them, as one surface would; the first of them in row-major order gives the direction.
		for mi := range buf {
			m := &buf[mi]
			if m.count == 0 {
				continue
			}
			depth := 0.0
			for mj := mi; mj < len(buf); mj++ {
				o := &buf[mj]
				if o.count == 0 || math.Abs(o.nx-m.nx) > tileNormalTolerance || math.Abs(o.ny-m.ny) > tileNormalTolerance {
					continue
				}
				for k := range o.count {
					depth = max(depth, o.points[k].depth)
				}
				if mj != mi {
					o.count = 0
				}
			}
			if depth <= slop {
				continue
			}
			corr := (depth - slop) * percent
			b.X = float32(float64(b.X) - float64(m.nx*corr))
			b.Y = float32(float64(b.Y) - float64(m.ny*corr))
		}
	}
	return buf
}

// indexContacts fills the scratch index of the contact cache by key. The index is only looked up, never iterated.
func (w *RigidWorld) indexContacts() {
	index := w.scratch.contactIndex
	if index == nil {
		index = make(map[rigidPairKey]int, len(w.contacts))
		w.scratch.contactIndex = index
	}
	clear(index)
	for i := range w.contacts {
		index[w.contacts[i].key] = i
	}
}

// collideTiles appends to out the contact manifolds of body b with the tiles it overlaps, in row-major order, with normals pointing from b toward the tile and each tile's cell as sub. Each manifold leaves the tile through a face no solid neighbour covers (tileFace), and a point a manifold earlier in the row-major order already reports with the same normal, such as the corner two floor tiles share, is dropped. A one-way tile only pushes up, and only when oneWay accepts its cell and the depth of its deepest point.
func (w *RigidWorld) collideTiles(b *RigidBody, out []rigidManifold, oneWay func(sub int, depth float64) bool) []rigidManifold {
	bh, ok := b.rigidHull()
	if !ok || !bh.finite() || !bh.hasArea() {
		return out
	}
	g := &w.tileGeom
	minX, minY, maxX, maxY := bh.bounds()
	minCol, minRow, maxCol, maxRow, ok := g.cellRange(minX, minY, maxX, maxY)
	if !ok {
		return out
	}
	kept := w.scratch.tileManifolds[:0]
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			flags := w.Tiles.TileFlagsAt(col, row)
			solid := flags&TileSolid != 0
			if !solid && flags&TileOneWay == 0 {
				continue
			}
			th := g.cellHull(col, row)
			m := collideHullsWithin(&th, &bh, speculativeDistance)
			if m.count == 0 {
				continue
			}
			if m, ok = w.tileFace(m, col, row, !solid, &th, &bh); !ok {
				continue
			}
			sub := g.cell(col, row)
			if !solid && !oneWay(sub, m.maxDepth()) {
				continue
			}
			if m = withoutReportedPoints(m, kept); m.count == 0 {
				continue
			}
			kept = append(kept, m)
			m.nx, m.ny = -m.nx, -m.ny
			m.sub = sub
			out = append(out, rigidManifoldFrom(m, b, w.tileBody))
		}
	}
	w.scratch.tileManifolds = kept
	return out
}

// tileFace returns manifold m of tile (col, row), with hull th, against body hull bh, normal from the tile toward the body, so that its normal leaves the tile through a face that is open: not covered by a solid neighbour, and for a one-way tile only its top. A manifold whose normal leans into a covered face is clipped again against the open face nearest its normal, as a chain does at a flat vertex, so a body sliding over a seam is pushed out of the floor's surface instead of back along it; with no open face facing the body the tile has no contact, and the neighbour that owns the surface reports it.
func (w *RigidWorld) tileFace(m hullManifold, col, row int, oneWay bool, th, bh *hull) (hullManifold, bool) {
	var open [4]bool
	open[tileFaceTop] = !w.solidTile(col, row-1)
	if !oneWay {
		open[tileFaceRight] = !w.solidTile(col+1, row)
		open[tileFaceBottom] = !w.solidTile(col, row+1)
		open[tileFaceLeft] = !w.solidTile(col-1, row)
	}
	nx, ny := m.nx, m.ny
	allowed := (ny >= -tileNormalTolerance || open[tileFaceTop]) &&
		(nx <= tileNormalTolerance || open[tileFaceRight]) &&
		(ny <= tileNormalTolerance || open[tileFaceBottom]) &&
		(nx >= -tileNormalTolerance || open[tileFaceLeft])
	if oneWay {
		allowed = open[tileFaceTop] && ny < 0 && math.Abs(nx) <= tileNormalTolerance
	}
	if allowed {
		return m, true
	}
	best, bestDot := -1, 0.0
	for f, isOpen := range open {
		if !isOpen {
			continue
		}
		if d := float64(th.n[f].x*nx) + float64(th.n[f].y*ny); d > bestDot {
			best, bestDot = f, d
		}
	}
	if best < 0 {
		return hullManifold{}, false
	}
	forced := clipHulls(th, best, bh, false, speculativeDistance)
	return forced, forced.count > 0
}

// appendTilePieces appends to dst the tiles a sweep of b inside the box (minX, minY)-(maxX, maxY) may hit, as pieces of the body that stands for Tiles: every solid tile with an open face, and every one-way tile whose top b starts above, moving down by dy. start is b's hull at the start of the step.
func (w *RigidWorld) appendTilePieces(b *RigidBody, start *hull, dy, minX, minY, maxX, maxY float64, dst []ccdCandidate) []ccdCandidate {
	if !w.tilesActive() || !CanCollideRigid(b, w.tileBody) {
		return dst
	}
	g := &w.tileGeom
	minCol, minRow, maxCol, maxRow, ok := g.cellRange(minX, minY, maxX, maxY)
	if !ok {
		return dst
	}
	_, _, _, startBottom := start.bounds()
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			flags := w.Tiles.TileFlagsAt(col, row)
			switch {
			case flags&TileSolid != 0:
				// A tile whose every face a neighbour covers can only be reached through a neighbour.
				if w.solidTile(col, row-1) && w.solidTile(col+1, row) && w.solidTile(col, row+1) && w.solidTile(col-1, row) {
					continue
				}
			case flags&TileOneWay != 0:
				_, top, _, _ := g.cellBounds(col, row)
				if !(dy > 0) || startBottom > top+oneWayTileMargin {
					continue
				}
			default:
				continue
			}
			if h := g.cellHull(col, row); h.finite() {
				dst = append(dst, ccdCandidate{st: w.tileBody, h: h})
			}
		}
	}
	return dst
}
