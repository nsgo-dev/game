package tilemap

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// TileFlags is a bitmask of gameplay and collision properties on a tile.
type TileFlags uint32

const (
	// TileSolid marks a tile that blocks movement from all sides.
	TileSolid TileFlags = 1 << 0
	// TileOneWay marks a tile that blocks movement only from one side (such as a platform).
	TileOneWay TileFlags = 1 << 1
)

// TileHit is one tile cell returned by Tilemap.TilesInRect.
type TileHit struct {
	Col, Row int
	TileID   uint32
	Flags    TileFlags
	Bounds   core.Bounds
}

// RaycastHit is the first tile cell met by Tilemap.Raycast.
type RaycastHit struct {
	Col, Row int
	TileID   uint32
	Flags    TileFlags
	Point    core.Point
	Normal   core.Point
	// T is the hit's fraction along the segment from -> to, in [0, 1].
	T float32
}

// SetFlags sets the TileFlags associated with global tile ID tileID (flip bits are ignored).
func (m *Tilemap) SetFlags(tileID uint32, flags TileFlags) {
	id := tileID & TileIDMask
	if id == 0 {
		return
	}
	if flags == 0 {
		delete(m.tileFlags, id)
		return
	}
	if m.tileFlags == nil {
		m.tileFlags = make(map[uint32]TileFlags)
	}
	m.tileFlags[id] = flags
}

// SetCellFlags overrides the flags at a single map cell (col, row), taking precedence over per-tile flags.
// On maps with sparse chunked layers (or Infinite=true), negative and arbitrary positive coordinates are supported.
func (m *Tilemap) SetCellFlags(col, row int, flags TileFlags) {
	if !m.hasChunkedLayer() && (col < 0 || row < 0 || col >= m.Width || row >= m.Height) {
		return
	}
	if m.cellFlags == nil {
		m.cellFlags = make(map[cellKey]TileFlags)
	}
	m.cellFlags[cellKey{col, row}] = flags
}

// ClearCellFlags removes any per-cell flag override at (col, row), restoring its tile-ID flags.
func (m *Tilemap) ClearCellFlags(col, row int) {
	delete(m.cellFlags, cellKey{col, row})
}

// FlagsAt returns the combined TileFlags at (col, row) across all tile layers, or 0 if out of bounds.
func (m *Tilemap) FlagsAt(col, row int) TileFlags {
	_, flags, _ := m.cellInfo(col, row)
	return flags
}

func (m *Tilemap) cellInfo(col, row int) (tileID uint32, flags TileFlags, nonEmpty bool) {
	unbounded := m.hasChunkedLayer()
	if !unbounded && (col < 0 || row < 0 || col >= m.Width || row >= m.Height) {
		return 0, 0, false
	}
	for i := range m.Layers {
		l := &m.Layers[i]
		var rawGID uint32
		if l.IsChunked() {
			rawGID = l.tileAtChunked(col, row)
		} else {
			if col < 0 || row < 0 {
				continue
			}
			lw, lh := m.layerSize(l)
			if col >= lw || row >= lh {
				continue
			}
			idx := row*lw + col
			if idx >= len(l.Data) {
				continue
			}
			rawGID = l.Data[idx]
		}
		id := rawGID & TileIDMask
		if id == 0 {
			continue
		}
		nonEmpty = true
		tileID = id
		if m.tileFlags != nil {
			flags |= m.tileFlags[id]
		}
	}
	if m.cellFlags != nil {
		if cf, ok := m.cellFlags[cellKey{col, row}]; ok {
			flags = cf
			if cf != 0 {
				nonEmpty = true
			}
		}
	}
	return tileID, flags, nonEmpty
}

func (m *Tilemap) cellMatch(col, row int, mask TileFlags, unbounded bool) (tileID uint32, flags TileFlags, ok bool) {
	if !unbounded {
		return m.cellMatchDense(col, row, mask)
	}
	return m.cellMatchUnbounded(col, row, mask)
}

func (m *Tilemap) cellMatchDense(col, row int, mask TileFlags) (tileID uint32, flags TileFlags, ok bool) {
	if col < 0 || row < 0 || col >= m.Width || row >= m.Height {
		return 0, 0, false
	}
	var matchingID, anyID uint32
	nonEmpty := false
	for i := range m.Layers {
		l := &m.Layers[i]
		lw := l.Width
		if lw == 0 {
			lw = m.Width
		}
		lh := l.Height
		if lh == 0 {
			lh = m.Height
		}
		if col >= lw || row >= lh {
			continue
		}
		idx := row*lw + col
		if idx >= len(l.Data) {
			continue
		}
		id := l.Data[idx] & TileIDMask
		if id == 0 {
			continue
		}
		nonEmpty = true
		anyID = id
		if m.tileFlags != nil {
			f := m.tileFlags[id]
			flags |= f
			if mask != 0 && f&mask != 0 {
				matchingID = id
			}
		}
	}
	if m.cellFlags != nil {
		if cf, hasOverride := m.cellFlags[cellKey{col, row}]; hasOverride {
			flags = cf
			if cf != 0 {
				nonEmpty = true
			}
		}
	}
	if mask == 0 {
		if !nonEmpty {
			return 0, 0, false
		}
		return anyID, flags, true
	}
	if flags&mask == 0 {
		return 0, 0, false
	}
	if matchingID != 0 {
		return matchingID, flags, true
	}
	return anyID, flags, true
}

func (m *Tilemap) cellMatchUnbounded(col, row int, mask TileFlags) (tileID uint32, flags TileFlags, ok bool) {
	var matchingID, anyID uint32
	nonEmpty := false
	for i := range m.Layers {
		l := &m.Layers[i]
		var rawGID uint32
		if l.IsChunked() {
			rawGID = l.tileAtChunked(col, row)
		} else {
			if col < 0 || row < 0 {
				continue
			}
			lw, lh := m.layerSize(l)
			if col >= lw || row >= lh {
				continue
			}
			idx := row*lw + col
			if idx >= len(l.Data) {
				continue
			}
			rawGID = l.Data[idx]
		}
		id := rawGID & TileIDMask
		if id == 0 {
			continue
		}
		nonEmpty = true
		anyID = id
		if m.tileFlags != nil {
			f := m.tileFlags[id]
			flags |= f
			if mask != 0 && f&mask != 0 {
				matchingID = id
			}
		}
	}
	if m.cellFlags != nil {
		if cf, hasOverride := m.cellFlags[cellKey{col, row}]; hasOverride {
			flags = cf
			if cf != 0 {
				nonEmpty = true
			}
		}
	}
	if mask == 0 {
		if !nonEmpty {
			return 0, 0, false
		}
		return anyID, flags, true
	}
	if flags&mask == 0 {
		return 0, 0, false
	}
	if matchingID != 0 {
		return matchingID, flags, true
	}
	return anyID, flags, true
}

// TilesInRect returns every map cell overlapping r whose flags match mask (or every non-empty cell when mask == 0), in row-major order.
//
// Cells are half-open [col*TileWidth, (col+1)*TileWidth) x [row*TileHeight, (row+1)*TileHeight), so a rectangle touching an adjacent tile's edge or corner does not include it.
func (m *Tilemap) TilesInRect(r core.Bounds, mask TileFlags) []TileHit {
	if m.TileWidth <= 0 || m.TileHeight <= 0 || r.Size.Width <= 0 || r.Size.Height <= 0 {
		return nil
	}
	unbounded := m.hasChunkedLayer()
	var boundMinCol, boundMinRow, boundMaxCol, boundMaxRow int
	if unbounded {
		var ok bool
		boundMinCol, boundMinRow, boundMaxCol, boundMaxRow, ok = m.activeTileBounds()
		if !ok {
			return nil
		}
	} else {
		if m.Width <= 0 || m.Height <= 0 {
			return nil
		}
		boundMinCol, boundMinRow = 0, 0
		boundMaxCol, boundMaxRow = m.Width-1, m.Height-1
	}

	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	ox, oy := float64(m.Origin.X), float64(m.Origin.Y)
	minX := float64(r.Origin.X) - ox
	maxX := minX + float64(r.Size.Width)
	minY := float64(r.Origin.Y) - oy
	maxY := minY + float64(r.Size.Height)

	minCol := max(boundMinCol, int(math.Floor(minX/tw)))
	maxCol := min(boundMaxCol, int(math.Ceil(maxX/tw))-1)
	minRow := max(boundMinRow, int(math.Floor(minY/th)))
	maxRow := min(boundMaxRow, int(math.Ceil(maxY/th))-1)
	if minCol > maxCol || minRow > maxRow {
		return nil
	}

	var hits []TileHit
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			id, flags, ok := m.cellMatch(col, row, mask, unbounded)
			if !ok {
				continue
			}
			hits = append(hits, TileHit{
				Col:    col,
				Row:    row,
				TileID: id,
				Flags:  flags,
				Bounds: core.Bounds{
					Origin: core.Point{
						X: m.Origin.X + core.Points(float32(col)*m.TileWidth),
						Y: m.Origin.Y + core.Points(float32(row)*m.TileHeight),
					},
					Size: core.Size{
						Width:  core.Points(m.TileWidth),
						Height: core.Points(m.TileHeight),
					},
				},
			})
		}
	}
	return hits
}

// Raycast traces the line segment from -> to and returns the first tile matching mask (or any non-empty tile when mask == 0).
func (m *Tilemap) Raycast(from, to core.Point, mask TileFlags) (RaycastHit, bool) {
	if m.TileWidth <= 0 || m.TileHeight <= 0 {
		return RaycastHit{}, false
	}
	unbounded := m.hasChunkedLayer()
	var boundMinCol, boundMinRow, boundMaxCol, boundMaxRow int
	if unbounded {
		var ok bool
		boundMinCol, boundMinRow, boundMaxCol, boundMaxRow, ok = m.activeTileBounds()
		if !ok {
			return RaycastHit{}, false
		}
	} else {
		if m.Width <= 0 || m.Height <= 0 {
			return RaycastHit{}, false
		}
		boundMinCol, boundMinRow = 0, 0
		boundMaxCol, boundMaxRow = m.Width-1, m.Height-1
	}

	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	minX := float64(boundMinCol) * tw
	maxX := float64(boundMaxCol+1) * tw
	minY := float64(boundMinRow) * th
	maxY := float64(boundMaxRow+1) * th

	ox, oy := float64(m.Origin.X), float64(m.Origin.Y)
	fromX, fromY := float64(from.X)-ox, float64(from.Y)-oy
	dx := float64(to.X) - float64(from.X)
	dy := float64(to.Y) - float64(from.Y)

	if dx == 0 && dy == 0 {
		if fromX < minX || fromX >= maxX || fromY < minY || fromY >= maxY {
			return RaycastHit{}, false
		}
		col := int(math.Floor(fromX / tw))
		row := int(math.Floor(fromY / th))
		if id, flags, ok := m.cellMatch(col, row, mask, unbounded); ok {
			return RaycastHit{
				Col: col, Row: row, TileID: id, Flags: flags,
				Point: from, T: 0,
			}, true
		}
		return RaycastHit{}, false
	}

	// Slab intersection against active world bounds [minX, maxX] x [minY, maxY] for t in [0, 1].
	t0x, t1x := math.Inf(-1), math.Inf(1)
	if dx == 0 {
		if fromX < minX || fromX >= maxX {
			return RaycastHit{}, false
		}
	} else {
		a, b := (minX-fromX)/dx, (maxX-fromX)/dx
		if a > b {
			a, b = b, a
		}
		t0x, t1x = a, b
	}

	t0y, t1y := math.Inf(-1), math.Inf(1)
	if dy == 0 {
		if fromY < minY || fromY >= maxY {
			return RaycastHit{}, false
		}
	} else {
		a, b := (minY-fromY)/dy, (maxY-fromY)/dy
		if a > b {
			a, b = b, a
		}
		t0y, t1y = a, b
	}

	t0 := max(t0x, t0y)
	t1 := min(t1x, t1y)
	if t0 >= t1 {
		return RaycastHit{}, false
	}
	tEnter := max(0.0, t0)
	tExit := min(1.0, t1)
	if tEnter > tExit || tExit <= 0 {
		return RaycastHit{}, false
	}

	const eps = 1e-12
	px := fromX + tEnter*dx
	py := fromY + tEnter*dy

	// Determine the starting cell (col, row) at tEnter.
	var col, row int
	if tEnter > 0 && t0x >= t0y-eps {
		if dx > 0 {
			col = boundMinCol
		} else {
			col = boundMaxCol
		}
	} else {
		col = int(math.Floor(px / tw))
		if dx < 0 && float64(col)*tw == px {
			col--
		}
		col = min(max(col, boundMinCol), boundMaxCol)
	}

	if tEnter > 0 && t0y >= t0x-eps {
		if dy > 0 {
			row = boundMinRow
		} else {
			row = boundMaxRow
		}
	} else {
		row = int(math.Floor(py / th))
		if dy < 0 && float64(row)*th == py {
			row--
		}
		row = min(max(row, boundMinRow), boundMaxRow)
	}

	if id, flags, ok := m.cellMatch(col, row, mask, unbounded); ok {
		var nx, ny float32
		if tEnter > 0 {
			switch {
			case math.Abs(t0x-t0y) <= eps:
				inv := float32(1 / math.Sqrt2)
				nx, ny = -sign32(dx)*inv, -sign32(dy)*inv
			case t0x > t0y:
				nx = -sign32(dx)
			default:
				ny = -sign32(dy)
			}
		} else {
			onX := (dx > 0 && float64(col)*tw == px) || (dx < 0 && float64(col+1)*tw == px)
			onY := (dy > 0 && float64(row)*th == py) || (dy < 0 && float64(row+1)*th == py)
			switch {
			case onX && onY:
				inv := float32(1 / math.Sqrt2)
				nx, ny = -sign32(dx)*inv, -sign32(dy)*inv
			case onX:
				nx = -sign32(dx)
			case onY:
				ny = -sign32(dy)
			}
		}
		return RaycastHit{
			Col: col, Row: row, TileID: id, Flags: flags,
			Point:  core.Point{X: core.Points(ox + px), Y: core.Points(oy + py)},
			Normal: core.Point{X: core.Points(nx), Y: core.Points(ny)},
			T:      float32(tEnter),
		}, true
	}

	// Amanatides & Woo 2D grid traversal.
	var stepX, stepY int
	tDeltaX, tMaxX := math.Inf(1), math.Inf(1)
	if dx > 0 {
		stepX = 1
		tDeltaX = tw / dx
		tMaxX = (float64(col+1)*tw - fromX) / dx
	} else if dx < 0 {
		stepX = -1
		tDeltaX = -tw / dx
		tMaxX = (float64(col)*tw - fromX) / dx
	}

	tDeltaY, tMaxY := math.Inf(1), math.Inf(1)
	if dy > 0 {
		stepY = 1
		tDeltaY = th / dy
		tMaxY = (float64(row+1)*th - fromY) / dy
	} else if dy < 0 {
		stepY = -1
		tDeltaY = -th / dy
		tMaxY = (float64(row)*th - fromY) / dy
	}

	for {
		t := min(tMaxX, tMaxY)
		if t > tExit+eps {
			return RaycastHit{}, false
		}
		tc := min(t, 1.0)
		hitPt := core.Point{
			X: core.Points(ox + fromX + tc*dx),
			Y: core.Points(oy + fromY + tc*dy),
		}

		if math.Abs(tMaxX-tMaxY) <= eps {
			// Exact corner crossing: block if both orthogonal neighbors form a diagonal wall.
			idX, flagsX, hitX := m.cellMatch(col+stepX, row, mask, unbounded)
			_, _, hitY := m.cellMatch(col, row+stepY, mask, unbounded)
			if hitX && hitY {
				return RaycastHit{
					Col: col + stepX, Row: row, TileID: idX, Flags: flagsX,
					Point:  hitPt,
					Normal: core.Point{X: core.Points(-stepX), Y: 0},
					T:      float32(tc),
				}, true
			}
			col += stepX
			row += stepY
			tMaxX += tDeltaX
			tMaxY += tDeltaY
			if col < boundMinCol || col > boundMaxCol || row < boundMinRow || row > boundMaxRow {
				return RaycastHit{}, false
			}
			if id, flags, ok := m.cellMatch(col, row, mask, unbounded); ok {
				inv := float32(1 / math.Sqrt2)
				return RaycastHit{
					Col: col, Row: row, TileID: id, Flags: flags,
					Point:  hitPt,
					Normal: core.Point{X: core.Points(-float32(stepX) * inv), Y: core.Points(-float32(stepY) * inv)},
					T:      float32(tc),
				}, true
			}
		} else if tMaxX < tMaxY {
			col += stepX
			tMaxX += tDeltaX
			if col < boundMinCol || col > boundMaxCol {
				return RaycastHit{}, false
			}
			if id, flags, ok := m.cellMatch(col, row, mask, unbounded); ok {
				return RaycastHit{
					Col: col, Row: row, TileID: id, Flags: flags,
					Point:  hitPt,
					Normal: core.Point{X: core.Points(-stepX), Y: 0},
					T:      float32(tc),
				}, true
			}
		} else {
			row += stepY
			tMaxY += tDeltaY
			if row < boundMinRow || row > boundMaxRow {
				return RaycastHit{}, false
			}
			if id, flags, ok := m.cellMatch(col, row, mask, unbounded); ok {
				return RaycastHit{
					Col: col, Row: row, TileID: id, Flags: flags,
					Point:  hitPt,
					Normal: core.Point{X: 0, Y: core.Points(-stepY)},
					T:      float32(tc),
				}, true
			}
		}
	}
}

func sign32(v float64) float32 {
	if v > 0 {
		return 1
	}
	if v < 0 {
		return -1
	}
	return 0
}
