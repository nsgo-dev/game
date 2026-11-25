package tileshape

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/lights2d"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// chunkSize is the width and height, in cells, of the blocks a View caches and rebuilds.
const chunkSize = 16

// Options choose which layers of a map a View reads and how it reports shaped cells.
type Options struct {
	// Layers are indices in Tilemap.Layers of the tile layers that collide. When Layers and LayerNames are both empty, every tile layer collides; a View notices a layer added to or removed from Tilemap.Layers after New on its next call and rebuilds every cell.
	Layers []int
	// LayerNames adds the tile layers with these names, looked up when New is called.
	LayerNames []string
	// ShapedSolid makes a cell whose tiles collide only through shapes that do not fill it report TileSolid (or TileOneWay, for a one-way tile) from TileFlagsAt, for code that knows only whole cells, such as physics.MoveAndSlide and physics.World's queries. By default such a cell reports neither, and collides through Slopes.
	ShapedSolid bool
}

// Segment is one straight edge of a View's outline, from A to B, with the solid geometry on its right as seen walking from A to B on screen (Y down), so its outward normal is (B.Y-A.Y, A.X-B.X) normalized.
type Segment struct {
	A, B core.Point
}

// Normal returns the unit normal of s pointing out of the solid geometry, or (0, 0) for a segment with no length.
func (s Segment) Normal() core.Point {
	dx, dy := float64(s.B.X-s.A.X), float64(s.B.Y-s.A.Y)
	l := math.Hypot(dx, dy)
	if l == 0 {
		return core.Point{}
	}
	return core.Point{X: core.Points(dy / l), Y: core.Points(-dx / l)}
}

// View is the collision and lighting geometry of a tilemap's chosen layers, in world coordinates that honour the map's Origin. It satisfies physics.TileGrid and physics.TileGridOrigin, so it is passed directly to physics.MoveAndSlide, physics.CharacterBody.Move, platformer.Controller.Step and physics.World.Tiles.
//
// A View is not safe for concurrent use, and the slices it returns are shared with its cache: they stay valid, and must not be modified, until the map changes.
type View struct {
	m           *tilemap.Tilemap
	all         bool
	layers      []int
	selected    map[int]bool
	shapedSolid bool

	// Cached grid, rebuilt from scratch when the map's size or layer count changes.
	cols, rows   int
	tw, th       float64
	ox, oy       float64 // the map's Origin, where cell (0, 0) starts
	nlayers      int
	ccols, crows int
	chunks       []chunk
	rev          uint64 // the Tilemap.TileRevision the chunks reflect

	// reachC and reachR are how many whole cells, across and down or up, the geometry of a tile placed in a cell can reach past that cell, such as a tile taller than the map's cells.
	reachC, reachR int

	shapes map[uint32]*tileGeom // the geometry of each gid's shapes, nil for none

	// Whole-map results, each rebuilt on use after any chunk changes.
	aggOK        aggregates
	outline      []Segment
	slopes       []physics.Slope
	boxes        []physics.Box
	staticShapes []physics.Shape
	occluders    []lights2d.Occluder

	// Scratch space reused by every chunk build.
	rawBuf  []edge
	keys    []keyedEdge
	order   []int32
	cutters [][2]vec
	keep    [][2]float64
	cuts    [][2]float64
}

// aggregates is a set of whole-map results that are up to date.
type aggregates uint8

const (
	aggOutline aggregates = 1 << iota
	aggSlopes
	aggBoxes
	aggStaticShapes
	aggOccluders
)

// New returns a View of m's layers chosen by opts. A View of a nil map, or of an unbounded map (Tilemap.Infinite set, or a sparse chunked layer), has no cells.
//
// A View follows the map through Tilemap.TileChangesSince: on its next call after SetTile, FillRect, SetCellFlags, ClearCellFlags or SetFlags it rebuilds only the chunks around the cells they changed, or every cell when the change log cannot say which (a SetFlags call, a large FillRect, more edits than the log holds).
func New(m *tilemap.Tilemap, opts Options) *View {
	v := &View{m: m, shapedSolid: opts.ShapedSolid}
	if m == nil {
		return v
	}
	if len(opts.Layers) == 0 && len(opts.LayerNames) == 0 {
		v.all = true
	} else {
		v.selected = make(map[int]bool)
		add := func(i int) {
			if i >= 0 && i < len(m.Layers) && !v.selected[i] {
				v.selected[i] = true
				v.layers = append(v.layers, i)
			}
		}
		for _, i := range opts.Layers {
			add(i)
		}
		for _, name := range opts.LayerNames {
			for i := range m.Layers {
				if m.Layers[i].Name == name {
					add(i)
				}
			}
		}
	}
	return v
}

// Map returns the tilemap v reads.
func (v *View) Map() *tilemap.Tilemap { return v.m }

// TileSize returns the map's tile width and height.
func (v *View) TileSize() (width, height float32) {
	if v.m == nil {
		return 0, 0
	}
	return v.m.TileWidth, v.m.TileHeight
}

// GridOrigin returns the map's Origin, the world position of cell (0, 0), implementing physics.TileGridOrigin so physics places v's cells where the map draws them. Every segment, slope and box v returns is in the same world coordinates.
func (v *View) GridOrigin() (x, y float32) {
	if v.m == nil {
		return 0, 0
	}
	return float32(v.m.Origin.X), float32(v.m.Origin.Y)
}

// GridSize returns the map's size in cells.
func (v *View) GridSize() (cols, rows int) {
	if v.m == nil {
		return 0, 0
	}
	return v.m.Width, v.m.Height
}

// TileFlagsAt returns the collision flags of the cell at (col, row) across v's layers, or 0 out of bounds. A cell that collides only through partial shapes reports neither TileSolid nor TileOneWay unless Options.ShapedSolid is set; every other flag bit of its tiles is kept.
func (v *View) TileFlagsAt(col, row int) physics.TileFlags {
	if !v.sync() || col < 0 || row < 0 || col >= v.cols || row >= v.rows {
		return 0
	}
	ch := v.cellChunk(col, row)
	return ch.flags[(row%chunkSize)*chunkSize+col%chunkSize]
}

// Slopes returns the segments physics.CharacterBody walks for every tile shape of the map: the upward-facing and vertical edges of solid shapes, the upward-facing edges of one-way shapes, and every polyline, with edges shared by neighbouring tiles removed and collinear edges merged within each 16 by 16 chunk.
func (v *View) Slopes() []physics.Slope {
	if v.aggregate(aggSlopes) {
		v.slopes = v.slopes[:0]
		v.eachChunk(func(ch *chunk) { v.slopes = append(v.slopes, ch.slopes...) })
		if len(v.slopes) == 0 {
			v.slopes = nil
		}
	}
	return v.slopes
}

// AppendSlopesIn appends to dst the slopes of the chunks that r, grown by one cell, overlaps, and returns the extended slice: a superset of the slopes in r that is cheaper to pass to physics.CharacterBody.Move on a large map than Slopes.
func (v *View) AppendSlopesIn(dst []physics.Slope, r core.Bounds) []physics.Slope {
	c0, r0, c1, r1, ok := v.chunkRange(r)
	if !ok {
		return dst
	}
	for cy := r0; cy <= r1; cy++ {
		for cx := c0; cx <= c1; cx++ {
			ch := v.geomChunk(cx, cy)
			dst = append(dst, ch.slopes...)
		}
	}
	return dst
}

// Outline returns the boundary of the solid geometry, whole cells and solid tile shapes together, as segments with the solid side on their right: edges shared by neighbouring solid cells or shapes are removed, and collinear edges are merged within each 16 by 16 chunk. One-way cells and shapes are not part of it.
func (v *View) Outline() []Segment {
	if v.aggregate(aggOutline) {
		v.outline = v.outline[:0]
		v.eachChunk(func(ch *chunk) { v.outline = append(v.outline, ch.outline...) })
		if len(v.outline) == 0 {
			v.outline = nil
		}
	}
	return v.outline
}

// Boxes returns the solid whole cells and axis-aligned rectangular solid tile shapes (such as half tiles), merged across neighbours into maximal rectangles, as static physics boxes; pass each to physics.NewStaticRigidBox for a physics.RigidWorld. For non-rectangular partial shapes such as slopes, rounded corners, and ellipses, use StaticShapes, Outline, or Slopes.
func (v *View) Boxes() []physics.Box {
	if v.aggregate(aggBoxes) {
		v.boxes = v.boxes[:0]
		v.eachChunk(func(ch *chunk) { v.boxes = append(v.boxes, ch.boxes...) })
		v.eachChunk(func(ch *chunk) { v.boxes = append(v.boxes, ch.shapeBoxes...) })
		if len(v.boxes) == 0 {
			v.boxes = nil
		} else {
			v.boxes = mergeRects(v.boxes, 1e-4*max(v.tw, v.th))
		}
	}
	return v.boxes
}

// StaticShapes returns static physics.Shape colliders for every solid whole cell and solid partial tile shape,
// merged across neighbours where possible: maximal physics.Box shapes for solid cells and axis-aligned rectangular
// partial tiles, merged convex physics.Polygon shapes for adjacent convex partial tiles (such as multi-tile slopes
// and bevels), and closed physics.Chain shapes for concave or high-vertex partial shapes (such as ellipses).
// Pass each shape to physics.NewStaticRigidBody or &physics.Body{Shape: s} to collide in RigidWorld or World.
func (v *View) StaticShapes() []physics.Shape {
	if v.aggregate(aggStaticShapes) {
		v.staticShapes = v.staticShapes[:0]
		boxes := v.Boxes()
		for _, b := range boxes {
			v.staticShapes = append(v.staticShapes, physics.BoxShape(b))
		}
		var polys []poly
		v.eachChunk(func(ch *chunk) {
			polys = append(polys, ch.shapePolys...)
		})
		eps := 1e-4 * max(v.tw, v.th)
		for _, p := range mergeConvexPolys(polys, eps) {
			if s, ok := polyToPhysicsShape(p, eps); ok {
				v.staticShapes = append(v.staticShapes, s)
			}
		}
		if len(v.staticShapes) == 0 {
			v.staticShapes = nil
		}
	}
	return v.staticShapes
}

// AppendStaticShapesIn appends to dst the static physics shapes of the chunks that r, grown by one cell and oversized-tile reach, overlaps, merged across those chunks.
func (v *View) AppendStaticShapesIn(dst []physics.Shape, r core.Bounds) []physics.Shape {
	c0, r0, c1, r1, ok := v.chunkRange(r)
	if !ok {
		return dst
	}
	var boxes []physics.Box
	var polys []poly
	for cy := r0; cy <= r1; cy++ {
		for cx := c0; cx <= c1; cx++ {
			ch := v.geomChunk(cx, cy)
			boxes = append(boxes, ch.boxes...)
			boxes = append(boxes, ch.shapeBoxes...)
			polys = append(polys, ch.shapePolys...)
		}
	}
	eps := 1e-4 * max(v.tw, v.th)
	for _, b := range mergeRects(boxes, eps) {
		dst = append(dst, physics.BoxShape(b))
	}
	for _, p := range mergeConvexPolys(polys, eps) {
		if s, ok := polyToPhysicsShape(p, eps); ok {
			dst = append(dst, s)
		}
	}
	return dst
}

// Occluders returns Outline as lights2d occluders, one per 16 by 16 chunk with solid geometry, which cast the same shadows as a rectangle occluder per solid cell and a polygon occluder per solid shape. Assign the result to lights2d.Scene.Occluders again after the map changes: a change rebuilds only the chunks around it, and the others' segments are reused.
func (v *View) Occluders() []lights2d.Occluder {
	if v.aggregate(aggOccluders) {
		v.occluders = v.occluders[:0]
		v.eachChunk(func(ch *chunk) {
			if len(ch.occ) > 0 {
				v.occluders = append(v.occluders, lights2d.Occluder{Segments: ch.occ})
			}
		})
		if len(v.occluders) == 0 {
			v.occluders = nil
		}
	}
	return v.occluders
}

// Invalidate rebuilds the cells of the rectangle [col, col+w) x [row, row+h), and the geometry around them, on next use. Call it after writing a layer's Data directly; the map's own setters need no call. After editing a tileset's Shapes call InvalidateAll instead, since Invalidate keeps the geometry v has derived from each tile's shapes.
func (v *View) Invalidate(col, row, w, h int) {
	if v.chunks == nil || w <= 0 || h <= 0 {
		return
	}
	v.markCells(col, row, col+w, row+h)
}

// InvalidateAll rebuilds every cell on next use.
//
// Call it after changing a tileset's Shapes, which every cell showing the tile may depend on.
func (v *View) InvalidateAll() {
	v.chunks = nil
	v.shapes = nil
	v.reachC, v.reachR = 0, 0
	v.aggOK = 0
}

// markCells marks the cells of [c0, c1) x [r0, r1) for rebuilding and the geometry of every chunk whose edges can meet them: within one cell, grown by twice the reach of oversized tiles, since an edge depends on its neighbours and a tile's geometry can reach past its cell.
func (v *View) markCells(c0, r0, c1, r1 int) {
	c0, r0 = max(c0, 0), max(r0, 0)
	c1, r1 = min(c1, v.cols), min(r1, v.rows)
	if c0 >= c1 || r0 >= r1 {
		return
	}
	for cy := r0 / chunkSize; cy <= (r1-1)/chunkSize; cy++ {
		for cx := c0 / chunkSize; cx <= (c1-1)/chunkSize; cx++ {
			v.chunks[cy*v.ccols+cx].cellsOK = false
		}
	}
	mc, mr := 1+2*v.reachC, 1+2*v.reachR
	gc0, gr0 := max(c0-mc, 0), max(r0-mr, 0)
	gc1, gr1 := min(c1+mc, v.cols), min(r1+mr, v.rows)
	for cy := gr0 / chunkSize; cy <= (gr1-1)/chunkSize; cy++ {
		for cx := gc0 / chunkSize; cx <= (gc1-1)/chunkSize; cx++ {
			v.chunks[cy*v.ccols+cx].geomOK = false
		}
	}
	v.aggOK = 0
}

// sync resets the cache when the map's size, tile size, Origin or layer count has changed, applies the map's tile changes since the last call, and reports whether the map has any cells.
func (v *View) sync() bool {
	m := v.m
	if m == nil || m.Width <= 0 || m.Height <= 0 || m.TileWidth <= 0 || m.TileHeight <= 0 || unbounded(m) {
		v.chunks = nil
		v.cols, v.rows = 0, 0
		return false
	}
	if v.chunks != nil && v.cols == m.Width && v.rows == m.Height && v.tw == float64(m.TileWidth) && v.th == float64(m.TileHeight) && v.ox == float64(m.Origin.X) && v.oy == float64(m.Origin.Y) && v.nlayers == len(m.Layers) {
		if m.TileRevision() == v.rev {
			return true
		}
		rev, ok := m.TileChangesSince(v.rev, func(col, row int) { v.markCells(col, row, col+1, row+1) })
		v.rev = rev
		if !ok {
			for i := range v.chunks {
				v.chunks[i].cellsOK = false
				v.chunks[i].geomOK = false
			}
			v.aggOK = 0
		}
		return true
	}
	v.cols, v.rows = m.Width, m.Height
	v.tw, v.th = float64(m.TileWidth), float64(m.TileHeight)
	v.ox, v.oy = float64(m.Origin.X), float64(m.Origin.Y)
	v.nlayers = len(m.Layers)
	v.rev = m.TileRevision()
	v.ccols = (v.cols + chunkSize - 1) / chunkSize
	v.crows = (v.rows + chunkSize - 1) / chunkSize
	v.chunks = make([]chunk, v.ccols*v.crows)
	v.shapes = nil
	v.reachC, v.reachR = v.reach()
	v.aggOK = 0
	return true
}

// reach returns how many whole cells, across and down or up, the shapes of any tile of the map's tilesets reach past the cell the tile is placed in, under every flip, as makeTileGeom places them.
func (v *View) reach() (cols, rows int) {
	cells := func(lo, hi, size float64) int {
		eps := 1e-4 * size
		return max(int(math.Ceil((-lo-eps)/size)), int(math.Ceil((hi-size-eps)/size)), 0)
	}
	for _, ts := range v.m.Tilesets {
		if ts == nil {
			continue
		}
		for id, shapes := range ts.Shapes {
			if len(shapes) == 0 {
				continue
			}
			gid := max(ts.FirstGID, 1) + id
			_, th := v.m.TileBox(gid)
			oy := v.th - float64(th)
			for flags := range uint32(8) {
				for _, s := range v.m.TileShapes(gid | flags<<29) { // the three flip bits, tilemap.FlipMask
					for _, p := range s.Points {
						x, y := float64(p.X), oy+float64(p.Y)
						if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
							continue
						}
						cols = max(cols, cells(x, x, v.tw))
						rows = max(rows, cells(y, y, v.th))
					}
				}
			}
		}
	}
	return cols, rows
}

// unbounded reports whether m has no fixed grid: Infinite is set or a tile layer is sparse and chunked, so its cells can lie at negative coordinates or past Width and Height.
func unbounded(m *tilemap.Tilemap) bool {
	if m.Infinite {
		return true
	}
	for i := range m.Layers {
		if m.Layers[i].IsChunked() {
			return true
		}
	}
	return false
}

// chunkRange returns the chunks r, grown by one cell and by the reach of oversized tiles, overlaps.
func (v *View) chunkRange(r core.Bounds) (c0, r0, c1, r1 int, ok bool) {
	if !v.sync() {
		return 0, 0, 0, 0, false
	}
	gc, gr := float64(1+v.reachC), float64(1+v.reachR)
	rx, ry := float64(r.Origin.X)-v.ox, float64(r.Origin.Y)-v.oy
	x0 := math.Floor(rx/v.tw) - gc
	y0 := math.Floor(ry/v.th) - gr
	x1 := math.Floor((rx+float64(r.Size.Width))/v.tw) + gc
	y1 := math.Floor((ry+float64(r.Size.Height))/v.th) + gr
	if math.IsNaN(x0+y0+x1+y1) || x1 < 0 || y1 < 0 || x0 >= float64(v.cols) || y0 >= float64(v.rows) {
		return 0, 0, 0, 0, false
	}
	clamp := func(f float64, n int) int { return int(min(max(f, 0), float64(n-1))) }
	return clamp(x0, v.cols) / chunkSize, clamp(y0, v.rows) / chunkSize, clamp(x1, v.cols) / chunkSize, clamp(y1, v.rows) / chunkSize, true
}

// aggregate reports whether the whole-map result kind must be rebuilt, and marks it up to date; it clears every result when the map has no cells.
func (v *View) aggregate(kind aggregates) bool {
	if !v.sync() {
		v.outline, v.slopes, v.boxes, v.staticShapes, v.occluders = nil, nil, nil, nil, nil
		return false
	}
	if v.aggOK&kind != 0 {
		return false
	}
	// Build every chunk first, since a build resets aggOK.
	for cy := range v.crows {
		for cx := range v.ccols {
			v.geomChunk(cx, cy)
		}
	}
	v.aggOK |= kind
	return true
}

// eachChunk calls fn with every chunk in row-major order; aggregate has built them.
func (v *View) eachChunk(fn func(*chunk)) {
	for i := range v.chunks {
		fn(&v.chunks[i])
	}
}
