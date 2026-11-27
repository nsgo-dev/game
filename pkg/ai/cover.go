package ai

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/tilemap"
)

// CoverHeight classifies how much vertical protection a CoverPoint provides.
type CoverHeight uint8

const (
	// CoverHalf is crouch- or chest-high cover (agents crouch to hide and can fire or peek over it).
	CoverHalf CoverHeight = iota + 1
	// CoverFull is full-height cover (blocks line of sight while standing; agents peek around edges).
	CoverFull
)

// String returns "half" for CoverHalf, "full" for CoverFull, or "none" for an unset value.
func (h CoverHeight) String() string {
	switch h {
	case CoverHalf:
		return "half"
	case CoverFull:
		return "full"
	default:
		return "none"
	}
}

// ParseCoverHeight parses "half" or "full" (case-insensitive, trimmed) into a CoverHeight.
func ParseCoverHeight(s string) (CoverHeight, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "half":
		return CoverHalf, true
	case "full":
		return CoverFull, true
	default:
		return 0, false
	}
}

func defaultCoverHeight(h CoverHeight) CoverHeight {
	if h == CoverHalf || h == CoverFull {
		return h
	}
	return CoverFull
}

// CoverPoint is a tactical cover position beside an obstacle.
//
// Facing is a unit vector pointing from Pos into the cover obstacle — the incoming
// threat direction that this cover blocks (the obstacle's outward surface normal is
// Facing.Neg()). Elevation stores the optional 3D floor height (0 in 2D worlds).
type CoverPoint struct {
	// ID uniquely identifies this cover point for reservations and deterministic tie-breaking.
	// Generators assign sequential non-zero IDs starting from StartID (default 1).
	ID uint64
	// Pos is the world-space standing position where an agent takes cover (offset into open space).
	Pos math2d.Vec2
	// Facing is the unit vector pointing from Pos toward the obstacle that blocks incoming fire.
	Facing math2d.Vec2
	// Height indicates whether this cover is CoverHalf or CoverFull.
	Height CoverHeight
	// Elevation is the optional 3D vertical floor coordinate (0 for 2D cover points).
	Elevation float32
	// Tags holds optional user-defined labels such as "window", "flank", or "corner".
	Tags []string
}

// HasTag reports whether c carries tag.
func (c CoverPoint) HasTag(tag string) bool {
	return slices.Contains(c.Tags, tag)
}

// CoverSegment describes one straight obstacle edge from which cover points can be sampled.
type CoverSegment struct {
	// A and B are the endpoints of the cover-providing obstacle edge in world coordinates.
	A, B math2d.Vec2
	// Facing is the unit vector pointing from the open side into the obstacle.
	Facing math2d.Vec2
	// Height is CoverHalf or CoverFull (defaults to ColliderCoverOptions.DefaultHeight when unset).
	Height CoverHeight
	// Elevation is the optional 3D floor coordinate preserved on generated cover points.
	Elevation float32
	// Tags are optional tags copied onto cover points generated from this segment.
	Tags []string
}

// TileCoverOptions configures cover-point generation from a tilemap.Tilemap.
type TileCoverOptions struct {
	// Origin shifts generated world coordinates by Origin (defaults to (0, 0)).
	Origin math2d.Vec2
	// Spacing is the distance between cover points along a contiguous solid wall run in world units.
	// When <= 0, one cover point is placed at the midpoint of each exposed tile edge.
	Spacing float32
	// Standoff offsets each cover point from the solid tile edge into the adjacent open tile.
	// When <= 0, defaults to half of min(TileWidth, TileHeight) so points land inside the open tile.
	Standoff float32
	// EdgeInset is the margin from each end of a contiguous wall run when Spacing > 0.
	// When <= 0, defaults to Spacing * 0.5.
	EdgeInset float32
	// SolidMask selects which tile flags count as solid cover walls (defaults to tilemap.TileSolid when 0).
	SolidMask tilemap.TileFlags
	// HalfCoverMask optionally marks solid tiles whose flags intersect HalfCoverMask as CoverHalf.
	HalfCoverMask tilemap.TileFlags
	// DefaultHeight sets the height for solid tiles not classified as half cover (defaults to CoverFull).
	DefaultHeight CoverHeight
	// HeightFunc optionally overrides cover-height classification for solid cell (col, row).
	// Returning 0 falls back to HalfCoverMask, the tile's "cover" property ("half"/"full"), or DefaultHeight.
	HeightFunc func(col, row int) CoverHeight
	// StartID is the first CoverPoint.ID assigned (defaults to 1 when 0).
	StartID uint64
}

// GridCoverOptions configures cover-point generation from any pathfind.Grid (blocked cells are solid walls).
type GridCoverOptions struct {
	// Spacing is the distance between cover points along a contiguous wall run in world units.
	// When <= 0, one cover point is placed at the midpoint of each exposed cell edge.
	Spacing float32
	// Standoff offsets each cover point from the blocked cell edge into the adjacent walkable cell.
	// When <= 0, defaults to half of min(cellWidth, cellHeight).
	Standoff float32
	// EdgeInset is the margin from each end of a contiguous wall run when Spacing > 0.
	// When <= 0, defaults to Spacing * 0.5.
	EdgeInset float32
	// DefaultHeight sets the height for generated cover points (defaults to CoverFull).
	DefaultHeight CoverHeight
	// HeightFunc optionally classifies cover height for blocked cell (col, row).
	HeightFunc func(col, row int) CoverHeight
	// StartID is the first CoverPoint.ID assigned (defaults to 1 when 0).
	StartID uint64
}

// ColliderCoverOptions configures cover-point generation from 2D physics boxes, segments, or a physics.World.
type ColliderCoverOptions struct {
	// Spacing is the distance between cover points along an obstacle edge (defaults to 32 when <= 0).
	Spacing float32
	// Standoff offsets each cover point away from the obstacle edge into open space (defaults to 8 when <= 0).
	Standoff float32
	// EdgeInset is the margin from each edge corner (defaults to Spacing * 0.5 when <= 0).
	EdgeInset float32
	// MinEdgeLength skips obstacle edges shorter than this length (values <= 0 allow any positive edge).
	MinEdgeLength float32
	// DefaultHeight sets the cover height when not overridden (defaults to CoverFull).
	DefaultHeight CoverHeight
	// HeightForBody optionally classifies cover height for each body in GenerateCoverFromWorld.
	HeightForBody func(*physics.Body) CoverHeight
	// LayerMask filters bodies inspected by GenerateCoverFromWorld (0 defaults to physics.LayerAll).
	LayerMask physics.LayerMask
	// SkipTiles omits World.Tiles when generating from a physics.World.
	SkipTiles bool
	// StartID is the first CoverPoint.ID assigned (defaults to 1 when 0).
	StartID uint64
}

// GenerateCoverFromTilemap generates deterministic cover points along exposed solid edges of m.
//
// Contiguous solid edges sharing the same CoverHeight are merged before sampling, and generated
// positions are verified to lie in an open (non-solid) in-bounds cell.
func GenerateCoverFromTilemap(m *tilemap.Tilemap, opts TileCoverOptions) []CoverPoint {
	if m == nil || m.Width <= 0 || m.Height <= 0 || m.TileWidth <= 0 || m.TileHeight <= 0 {
		return nil
	}
	solidMask := opts.SolidMask
	if solidMask == 0 {
		solidMask = tilemap.TileSolid
	}
	defHeight := defaultCoverHeight(opts.DefaultHeight)

	isSolid := func(col, row int) bool {
		if col < 0 || row < 0 || col >= m.Width || row >= m.Height {
			return false
		}
		return m.FlagsAt(col, row)&solidMask != 0
	}

	classify := func(col, row int) CoverHeight {
		if opts.HeightFunc != nil {
			if h := opts.HeightFunc(col, row); h == CoverHalf || h == CoverFull {
				return h
			}
		}
		if opts.HalfCoverMask != 0 && m.FlagsAt(col, row)&opts.HalfCoverMask != 0 {
			return CoverHalf
		}
		// Check top-most non-empty tile's "cover" property if defined.
		for i := len(m.Layers) - 1; i >= 0; i-- {
			l := &m.Layers[i]
			lw, lh := l.Width, l.Height
			if lw <= 0 {
				lw = m.Width
			}
			if lh <= 0 {
				lh = m.Height
			}
			if col >= lw || row >= lh {
				continue
			}
			idx := row*lw + col
			if idx >= len(l.Data) {
				continue
			}
			gid := l.Data[idx] & tilemap.TileIDMask
			if gid == 0 {
				continue
			}
			if props := m.TileProperties(gid); props != nil {
				if s, ok := props.String("cover"); ok {
					if h, valid := ParseCoverHeight(s); valid {
						return h
					}
				}
			}
			break
		}
		return defHeight
	}

	return generateCoverFromSolidGrid(
		m.Width, m.Height,
		opts.Origin, m.TileWidth, m.TileHeight,
		isSolid, classify,
		opts.Spacing, opts.Standoff, opts.EdgeInset, opts.StartID,
	)
}

// GenerateCoverFromGrid generates deterministic cover points along exposed edges of blocked cells
// (!g.Walkable) on g, offset into adjacent walkable cells.
func GenerateCoverFromGrid(g pathfind.Grid, origin math2d.Vec2, cellWidth, cellHeight float32, opts GridCoverOptions) []CoverPoint {
	if g == nil || cellWidth <= 0 || cellHeight <= 0 {
		return nil
	}
	cols, rows := g.GridSize()
	if cols <= 0 || rows <= 0 {
		return nil
	}
	defHeight := defaultCoverHeight(opts.DefaultHeight)
	isSolid := func(col, row int) bool {
		if col < 0 || row < 0 || col >= cols || row >= rows {
			return false
		}
		return !g.Walkable(col, row)
	}
	classify := func(col, row int) CoverHeight {
		if opts.HeightFunc != nil {
			if h := opts.HeightFunc(col, row); h == CoverHalf || h == CoverFull {
				return h
			}
		}
		return defHeight
	}
	return generateCoverFromSolidGrid(
		cols, rows,
		origin, cellWidth, cellHeight,
		isSolid, classify,
		opts.Spacing, opts.Standoff, opts.EdgeInset, opts.StartID,
	)
}

func generateCoverFromSolidGrid(
	cols, rows int,
	origin math2d.Vec2,
	tw, th float32,
	isSolid func(col, row int) bool,
	classify func(col, row int) CoverHeight,
	spacing, standoff, edgeInset float32,
	startID uint64,
) []CoverPoint {
	if cols <= 0 || rows <= 0 || tw <= 0 || th <= 0 {
		return nil
	}
	if !(standoff > 0) || math.IsInf(float64(standoff), 0) {
		standoff = 0.5 * min(tw, th)
	}
	nextID := startID
	if nextID == 0 {
		nextID = 1
	}

	isOpenPoint := func(p math2d.Vec2) bool {
		fx := math.Floor((float64(p.X) - float64(origin.X)) / float64(tw))
		fy := math.Floor((float64(p.Y) - float64(origin.Y)) / float64(th))
		if math.IsNaN(fx) || math.IsNaN(fy) {
			return false
		}
		c, r := int(fx), int(fy)
		if c < 0 || c >= cols || r < 0 || r >= rows {
			return false
		}
		return !isSolid(c, r)
	}

	var out []CoverPoint

	emitRun := func(a, b, facing math2d.Vec2, height CoverHeight, defaultSpacing float32) {
		sp := spacing
		if !(sp > 0) || math.IsInf(float64(sp), 0) {
			sp = defaultSpacing
		}
		ins := edgeInset
		if !(ins > 0) || math.IsInf(float64(ins), 0) {
			ins = sp * 0.5
		}
		out = appendSegmentCoverPoints(out, a, b, facing, height, 0, nil, sp, standoff, ins, 0, &nextID, isOpenPoint)
	}

	// 1. Horizontal edges (top and bottom of each solid row), in ascending row then column order.
	for row := range rows {
		// Top edges: solid at (col, row), open at (col, row-1); cover faces +Y (into solid).
		if row > 0 {
			for col := 0; col < cols; {
				if !isSolid(col, row) || isSolid(col, row-1) {
					col++
					continue
				}
				h := classify(col, row)
				startCol := col
				col++
				for col < cols && isSolid(col, row) && !isSolid(col, row-1) && classify(col, row) == h {
					col++
				}
				y := origin.Y + float32(row)*th
				a := math2d.Vec2{X: origin.X + float32(startCol)*tw, Y: y}
				b := math2d.Vec2{X: origin.X + float32(col)*tw, Y: y}
				emitRun(a, b, math2d.Vec2{X: 0, Y: 1}, h, tw)
			}
		}
		// Bottom edges: solid at (col, row), open at (col, row+1); cover faces -Y (into solid).
		if row+1 < rows {
			for col := 0; col < cols; {
				if !isSolid(col, row) || isSolid(col, row+1) {
					col++
					continue
				}
				h := classify(col, row)
				startCol := col
				col++
				for col < cols && isSolid(col, row) && !isSolid(col, row+1) && classify(col, row) == h {
					col++
				}
				y := origin.Y + float32(row+1)*th
				a := math2d.Vec2{X: origin.X + float32(startCol)*tw, Y: y}
				b := math2d.Vec2{X: origin.X + float32(col)*tw, Y: y}
				emitRun(a, b, math2d.Vec2{X: 0, Y: -1}, h, tw)
			}
		}
	}

	// 2. Vertical edges (left and right of each solid column), in ascending column then row order.
	for col := range cols {
		// Left edges: solid at (col, row), open at (col-1, row); cover faces +X (into solid).
		if col > 0 {
			for row := 0; row < rows; {
				if !isSolid(col, row) || isSolid(col-1, row) {
					row++
					continue
				}
				h := classify(col, row)
				startRow := row
				row++
				for row < rows && isSolid(col, row) && !isSolid(col-1, row) && classify(col, row) == h {
					row++
				}
				x := origin.X + float32(col)*tw
				a := math2d.Vec2{X: x, Y: origin.Y + float32(startRow)*th}
				b := math2d.Vec2{X: x, Y: origin.Y + float32(row)*th}
				emitRun(a, b, math2d.Vec2{X: 1, Y: 0}, h, th)
			}
		}
		// Right edges: solid at (col, row), open at (col+1, row); cover faces -X (into solid).
		if col+1 < cols {
			for row := 0; row < rows; {
				if !isSolid(col, row) || isSolid(col+1, row) {
					row++
					continue
				}
				h := classify(col, row)
				startRow := row
				row++
				for row < rows && isSolid(col, row) && !isSolid(col+1, row) && classify(col, row) == h {
					row++
				}
				x := origin.X + float32(col+1)*tw
				a := math2d.Vec2{X: x, Y: origin.Y + float32(startRow)*th}
				b := math2d.Vec2{X: x, Y: origin.Y + float32(row)*th}
				emitRun(a, b, math2d.Vec2{X: -1, Y: 0}, h, th)
			}
		}
	}

	return out
}

// GenerateCoverFromSegments samples deterministic cover points along straight obstacle segments.
func GenerateCoverFromSegments(segments []CoverSegment, opts ColliderCoverOptions) []CoverPoint {
	if len(segments) == 0 {
		return nil
	}
	spacing, standoff, inset := normalizeColliderCoverOpts(opts)
	defHeight := defaultCoverHeight(opts.DefaultHeight)
	nextID := opts.StartID
	if nextID == 0 {
		nextID = 1
	}
	var out []CoverPoint
	for _, seg := range segments {
		facing := seg.Facing.Normalize()
		if facing == (math2d.Vec2{}) {
			continue
		}
		h := seg.Height
		if h != CoverHalf && h != CoverFull {
			h = defHeight
		}
		out = appendSegmentCoverPoints(out, seg.A, seg.B, facing, h, seg.Elevation, seg.Tags, spacing, standoff, inset, opts.MinEdgeLength, &nextID, nil)
	}
	return out
}

// GenerateCoverFromBoxes generates deterministic cover points along the four exterior edges of
// each non-empty physics.Box, discarding any point that lands inside another box.
func GenerateCoverFromBoxes(boxes []physics.Box, opts ColliderCoverOptions) []CoverPoint {
	if len(boxes) == 0 {
		return nil
	}
	spacing, standoff, inset := normalizeColliderCoverOpts(opts)
	defHeight := defaultCoverHeight(opts.DefaultHeight)
	nextID := opts.StartID
	if nextID == 0 {
		nextID = 1
	}

	isOpen := func(p math2d.Vec2) bool {
		for _, b := range boxes {
			if !b.Empty() && b.Contains(p.X, p.Y) {
				return false
			}
		}
		return true
	}

	var out []CoverPoint
	for _, b := range boxes {
		if b.Empty() {
			continue
		}
		x0, y0, x1, y1 := b.Left(), b.Top(), b.Right(), b.Bottom()
		// Top, Right, Bottom, Left in fixed order.
		out = appendSegmentCoverPoints(out, math2d.V2(x0, y0), math2d.V2(x1, y0), math2d.V2(0, 1), defHeight, 0, nil, spacing, standoff, inset, opts.MinEdgeLength, &nextID, isOpen)
		out = appendSegmentCoverPoints(out, math2d.V2(x1, y0), math2d.V2(x1, y1), math2d.V2(-1, 0), defHeight, 0, nil, spacing, standoff, inset, opts.MinEdgeLength, &nextID, isOpen)
		out = appendSegmentCoverPoints(out, math2d.V2(x1, y1), math2d.V2(x0, y1), math2d.V2(0, -1), defHeight, 0, nil, spacing, standoff, inset, opts.MinEdgeLength, &nextID, isOpen)
		out = appendSegmentCoverPoints(out, math2d.V2(x0, y1), math2d.V2(x0, y0), math2d.V2(1, 0), defHeight, 0, nil, spacing, standoff, inset, opts.MinEdgeLength, &nextID, isOpen)
	}
	return out
}

// GenerateCoverFromWorld generates deterministic cover points from all matching 2D physics bodies
// (boxes and circular pillars) and, unless opts.SkipTiles is true, solid tiles in w.Tiles.
// Candidate positions embedded inside any matching collider or solid tile are excluded.
func GenerateCoverFromWorld(w *physics.World, opts ColliderCoverOptions) []CoverPoint {
	if w == nil {
		return nil
	}
	spacing, standoff, inset := normalizeColliderCoverOpts(opts)
	defHeight := defaultCoverHeight(opts.DefaultHeight)
	layerMask := opts.LayerMask
	if layerMask == 0 {
		layerMask = physics.LayerAll
	}
	nextID := opts.StartID
	if nextID == 0 {
		nextID = 1
	}

	bodyMatches := func(b *physics.Body) bool {
		if b == nil || b.Shape.Empty() {
			return false
		}
		l := b.Layer
		if l == 0 {
			l = physics.LayerAll
		}
		return l&layerMask != 0
	}

	isOpen := func(p math2d.Vec2) bool {
		for _, b := range w.Bodies {
			if bodyMatches(b) && b.Shape.Contains(p.X, p.Y) {
				return false
			}
		}
		if w.Tiles != nil && !opts.SkipTiles {
			tw, th := w.Tiles.TileSize()
			cols, rows := w.Tiles.GridSize()
			if tw > 0 && th > 0 && cols > 0 && rows > 0 {
				c := int(math.Floor(float64(p.X) / float64(tw)))
				r := int(math.Floor(float64(p.Y) / float64(th)))
				if c >= 0 && c < cols && r >= 0 && r < rows {
					if w.Tiles.TileFlagsAt(c, r)&physics.TileSolid != 0 {
						return false
					}
				}
			}
		}
		return true
	}

	var out []CoverPoint

	for _, b := range w.Bodies {
		if !bodyMatches(b) {
			continue
		}
		h := defHeight
		if opts.HeightForBody != nil {
			if ch := opts.HeightForBody(b); ch == CoverHalf || ch == CoverFull {
				h = ch
			}
		}
		var tags []string
		if b.Tag != "" {
			tags = []string{b.Tag}
		}
		switch b.Shape.Kind {
		case physics.ShapeBox:
			box := b.Shape.Box
			x0, y0, x1, y1 := box.Left(), box.Top(), box.Right(), box.Bottom()
			out = appendSegmentCoverPoints(out, math2d.V2(x0, y0), math2d.V2(x1, y0), math2d.V2(0, 1), h, 0, tags, spacing, standoff, inset, opts.MinEdgeLength, &nextID, isOpen)
			out = appendSegmentCoverPoints(out, math2d.V2(x1, y0), math2d.V2(x1, y1), math2d.V2(-1, 0), h, 0, tags, spacing, standoff, inset, opts.MinEdgeLength, &nextID, isOpen)
			out = appendSegmentCoverPoints(out, math2d.V2(x1, y1), math2d.V2(x0, y1), math2d.V2(0, -1), h, 0, tags, spacing, standoff, inset, opts.MinEdgeLength, &nextID, isOpen)
			out = appendSegmentCoverPoints(out, math2d.V2(x0, y1), math2d.V2(x0, y0), math2d.V2(1, 0), h, 0, tags, spacing, standoff, inset, opts.MinEdgeLength, &nextID, isOpen)
		case physics.ShapeCircle:
			c := b.Shape.Circle
			perim := 2 * math.Pi * float64(c.R)
			if opts.MinEdgeLength > 0 && perim < float64(opts.MinEdgeLength) {
				continue
			}
			count := max(4, int(math.Round(perim/float64(spacing))))
			rad := float64(c.R) + float64(standoff)
			for i := range count {
				ang := 2 * math.Pi * float64(i) / float64(count)
				sn, cs := math.Sincos(ang)
				pos := math2d.Vec2{
					X: float32(float64(c.X) + cs*rad),
					Y: float32(float64(c.Y) + sn*rad),
				}
				if !isOpen(pos) {
					continue
				}
				out = append(out, CoverPoint{
					ID:     nextID,
					Pos:    pos,
					Facing: math2d.Vec2{X: float32(-cs), Y: float32(-sn)},
					Height: h,
					Tags:   slices.Clone(tags),
				})
				nextID++
			}
		}
	}

	if w.Tiles != nil && !opts.SkipTiles {
		tw, th := w.Tiles.TileSize()
		cols, rows := w.Tiles.GridSize()
		if tw > 0 && th > 0 && cols > 0 && rows > 0 {
			tilePts := generateCoverFromSolidGrid(
				cols, rows,
				math2d.Zero(), tw, th,
				func(col, row int) bool { return w.Tiles.TileFlagsAt(col, row)&physics.TileSolid != 0 },
				func(int, int) CoverHeight { return defHeight },
				opts.Spacing, standoff, opts.EdgeInset, nextID,
			)
			for _, pt := range tilePts {
				if !isOpen(pt.Pos) {
					continue
				}
				pt.ID = nextID
				nextID++
				out = append(out, pt)
			}
		}
	}

	return out
}

func normalizeColliderCoverOpts(opts ColliderCoverOptions) (spacing, standoff, inset float32) {
	spacing = opts.Spacing
	if !(spacing > 0) || math.IsInf(float64(spacing), 0) {
		spacing = 32
	}
	standoff = opts.Standoff
	if !(standoff > 0) || math.IsInf(float64(standoff), 0) {
		standoff = 8
	}
	inset = opts.EdgeInset
	if !(inset > 0) || math.IsInf(float64(inset), 0) {
		inset = spacing * 0.5
	}
	return spacing, standoff, inset
}

func appendSegmentCoverPoints(
	dst []CoverPoint,
	a, b, facing math2d.Vec2,
	height CoverHeight,
	elevation float32,
	tags []string,
	spacing, standoff, inset, minEdgeLen float32,
	nextID *uint64,
	isOpen func(math2d.Vec2) bool,
) []CoverPoint {
	dx := float64(b.X) - float64(a.X)
	dy := float64(b.Y) - float64(a.Y)
	edgeLen := math.Hypot(dx, dy)
	if edgeLen <= 1e-6 || math.IsNaN(edgeLen) || math.IsInf(edgeLen, 0) {
		return dst
	}
	if minEdgeLen > 0 && edgeLen < float64(minEdgeLen) {
		return dst
	}
	ux, uy := dx/edgeLen, dy/edgeLen
	// Outward normal points opposite to Facing (into open walkable space).
	ox, oy := -float64(facing.X), -float64(facing.Y)

	ins := float64(inset)
	sp := float64(spacing)
	usable := edgeLen - 2*ins

	emitAt := func(distAlong float64) {
		pos := math2d.Vec2{
			X: float32(float64(a.X) + ux*distAlong + ox*float64(standoff)),
			Y: float32(float64(a.Y) + uy*distAlong + oy*float64(standoff)),
		}
		if isOpen != nil && !isOpen(pos) {
			return
		}
		cp := CoverPoint{
			ID:        *nextID,
			Pos:       pos,
			Facing:    facing,
			Height:    height,
			Elevation: elevation,
		}
		if len(tags) > 0 {
			cp.Tags = slices.Clone(tags)
		}
		*nextID++
		dst = append(dst, cp)
	}

	if usable <= 1e-5 {
		emitAt(edgeLen * 0.5)
		return dst
	}

	count := int(math.Floor(usable/sp+1e-6)) + 1
	if count <= 1 {
		emitAt(edgeLen * 0.5)
		return dst
	}
	// Center samples symmetrically along the edge.
	span := float64(count-1) * sp
	start := (edgeLen - span) * 0.5
	for i := range count {
		emitAt(start + float64(i)*sp)
	}
	return dst
}

// ReservationConfig configures a PointReservations tracker.
type ReservationConfig struct {
	// MinSeparation is the minimum allowed distance between two reserved positions that do not
	// share a non-zero CoverID. Values <= 0 enforce exclusivity only on matching CoverIDs.
	MinSeparation float32
	// DefaultTimeout is the automatic expiration duration used when Reserve is called with
	// timeout <= 0. Values <= 0 keep reservations active until explicitly released.
	DefaultTimeout time.Duration
}

// Reservation describes a single agent's active tactical position or cover reservation.
type Reservation struct {
	// AgentID is the reserving agent's unique identifier.
	AgentID uint64
	// CoverID is the reserved CoverPoint.ID, or 0 for a non-cover spatial position.
	CoverID uint64
	// Pos is the reserved world position.
	Pos math2d.Vec2
	// Remaining is the time left before automatic expiry when HasTimeout is true.
	Remaining time.Duration
	// HasTimeout reports whether this reservation expires automatically on Step.
	HasTimeout bool
}

// PointReservations tracks exclusive reservations of cover points and tactical positions across agents.
//
// Each agent holds at most one active reservation at a time: reserving a new point automatically
// releases that agent's prior reservation, while two different agents can never hold the same
// CoverID or positions closer than MinSeparation. Step(dt) deterministically expires timed
// reservations with zero heap allocations.
//
// PointReservations is not safe for concurrent use; drive it on game.Loop's fixed steps.
type PointReservations struct {
	cfg     ReservationConfig
	entries []Reservation
}

// NewPointReservations returns a PointReservations tracker with cfg.
func NewPointReservations(cfg ReservationConfig) *PointReservations {
	if cfg.MinSeparation < 0 || math.IsNaN(float64(cfg.MinSeparation)) || math.IsInf(float64(cfg.MinSeparation), 0) {
		cfg.MinSeparation = 0
	}
	if cfg.DefaultTimeout < 0 {
		cfg.DefaultTimeout = 0
	}
	return &PointReservations{cfg: cfg}
}

// Config returns the tracker's effective ReservationConfig.
func (r *PointReservations) Config() ReservationConfig {
	if r == nil {
		return ReservationConfig{}
	}
	return r.cfg
}

// Count returns the number of currently active reservations.
func (r *PointReservations) Count() int {
	if r == nil {
		return 0
	}
	return len(r.entries)
}

// Clear removes all active reservations while retaining slice capacity.
func (r *PointReservations) Clear() {
	if r == nil {
		return
	}
	r.entries = r.entries[:0]
}

// Get returns agentID's active reservation, if any.
func (r *PointReservations) Get(agentID uint64) (Reservation, bool) {
	if r == nil || agentID == 0 {
		return Reservation{}, false
	}
	if idx := r.indexOfAgent(agentID); idx >= 0 {
		return r.entries[idx], true
	}
	return Reservation{}, false
}

// HolderOfCover returns the AgentID currently holding coverID, if any.
func (r *PointReservations) HolderOfCover(coverID uint64) (uint64, bool) {
	if r == nil || coverID == 0 {
		return 0, false
	}
	for i := range r.entries {
		if r.entries[i].CoverID == coverID {
			return r.entries[i].AgentID, true
		}
	}
	return 0, false
}

// IsReservedByOther reports whether coverID (when non-zero) or pos (within MinSeparation)
// is currently reserved by an agent other than agentID.
func (r *PointReservations) IsReservedByOther(agentID, coverID uint64, pos math2d.Vec2) bool {
	if r == nil {
		return false
	}
	minSepSq := float64(r.cfg.MinSeparation) * float64(r.cfg.MinSeparation)
	for i := range r.entries {
		e := &r.entries[i]
		if e.AgentID == agentID && agentID != 0 {
			continue
		}
		if coverID != 0 && e.CoverID == coverID {
			return true
		}
		if minSepSq > 0 {
			dx := float64(e.Pos.X) - float64(pos.X)
			dy := float64(e.Pos.Y) - float64(pos.Y)
			if dx*dx+dy*dy < minSepSq {
				return true
			}
		}
	}
	return false
}

// Reserve reserves candidate c for agentID for timeout (or Config().DefaultTimeout when timeout <= 0).
//
// It returns false without altering agentID's existing reservation if agentID is 0 or if the
// candidate conflicts with another agent's reservation. On success, any prior reservation held by
// agentID is replaced atomically.
func (r *PointReservations) Reserve(agentID uint64, c TacticalCandidate, timeout time.Duration) bool {
	return r.reserve(agentID, c.CoverID, c.Pos, timeout)
}

// ReserveCover reserves CoverPoint cp for agentID for timeout (or Config().DefaultTimeout when timeout <= 0).
func (r *PointReservations) ReserveCover(agentID uint64, cp CoverPoint, timeout time.Duration) bool {
	return r.reserve(agentID, cp.ID, cp.Pos, timeout)
}

// ReservePos reserves an arbitrary world position pos for agentID for timeout (or Config().DefaultTimeout when timeout <= 0).
func (r *PointReservations) ReservePos(agentID uint64, pos math2d.Vec2, timeout time.Duration) bool {
	return r.reserve(agentID, 0, pos, timeout)
}

func (r *PointReservations) reserve(agentID, coverID uint64, pos math2d.Vec2, timeout time.Duration) bool {
	if r == nil || agentID == 0 {
		return false
	}
	if r.IsReservedByOther(agentID, coverID, pos) {
		return false
	}
	dur := timeout
	if dur <= 0 {
		dur = r.cfg.DefaultTimeout
	}
	entry := Reservation{
		AgentID:    agentID,
		CoverID:    coverID,
		Pos:        pos,
		Remaining:  dur,
		HasTimeout: dur > 0,
	}
	if idx := r.indexOfAgent(agentID); idx >= 0 {
		r.entries[idx] = entry
		return true
	}
	r.entries = append(r.entries, entry)
	return true
}

// Refresh resets the timeout of agentID's active reservation to timeout (or DefaultTimeout when <= 0).
func (r *PointReservations) Refresh(agentID uint64, timeout time.Duration) bool {
	if r == nil || agentID == 0 {
		return false
	}
	idx := r.indexOfAgent(agentID)
	if idx < 0 {
		return false
	}
	dur := timeout
	if dur <= 0 {
		dur = r.cfg.DefaultTimeout
	}
	r.entries[idx].Remaining = dur
	r.entries[idx].HasTimeout = dur > 0
	return true
}

// Release frees agentID's active reservation (for example on death or disengage).
// It returns true if agentID held a reservation.
func (r *PointReservations) Release(agentID uint64) bool {
	if r == nil || agentID == 0 {
		return false
	}
	idx := r.indexOfAgent(agentID)
	if idx < 0 {
		return false
	}
	r.removeAt(idx)
	return true
}

// ReleaseCover frees whichever agent currently holds coverID (for example when destructible cover breaks).
func (r *PointReservations) ReleaseCover(coverID uint64) bool {
	if r == nil || coverID == 0 {
		return false
	}
	for i := range r.entries {
		if r.entries[i].CoverID == coverID {
			r.removeAt(i)
			return true
		}
	}
	return false
}

// Step advances all timed reservations by dt and removes any whose remaining time reaches <= 0.
// Preserves deterministic order and performs zero heap allocations.
func (r *PointReservations) Step(dt time.Duration) {
	if r == nil || dt <= 0 || len(r.entries) == 0 {
		return
	}
	dst := 0
	for i := range r.entries {
		e := r.entries[i]
		if e.HasTimeout {
			e.Remaining -= dt
			if e.Remaining <= 0 {
				continue
			}
		}
		r.entries[dst] = e
		dst++
	}
	r.entries = r.entries[:dst]
}

// Entries appends all active reservations to dst[:0] in deterministic ascending AgentID order.
func (r *PointReservations) Entries(dst []Reservation) []Reservation {
	if r == nil {
		return dst[:0]
	}
	dst = append(dst[:0], r.entries...)
	for i := 1; i < len(dst); i++ {
		v := dst[i]
		j := i - 1
		for j >= 0 && dst[j].AgentID > v.AgentID {
			dst[j+1] = dst[j]
			j--
		}
		dst[j+1] = v
	}
	return dst
}

func (r *PointReservations) indexOfAgent(agentID uint64) int {
	for i := range r.entries {
		if r.entries[i].AgentID == agentID {
			return i
		}
	}
	return -1
}

func (r *PointReservations) removeAt(idx int) {
	copy(r.entries[idx:], r.entries[idx+1:])
	r.entries = r.entries[:len(r.entries)-1]
}
