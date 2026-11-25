package tilemap

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// Orientation selects how tiles are arranged and projected into world coordinates.
type Orientation int

const (
	// OrientationOrthogonal lays tiles out on an axis-aligned rectangular grid.
	OrientationOrthogonal Orientation = iota
	// OrientationIsometric lays tiles out on a diamond isometric grid.
	OrientationIsometric
	// OrientationStaggered lays isometric diamonds out with alternating rows or columns shifted by half a tile.
	OrientationStaggered
	// OrientationHexagonal lays hexagons out with alternating rows (pointy-top) or columns (flat-top) shifted by half a tile.
	OrientationHexagonal
)

// StaggerAxis selects which axis is staggered for staggered isometric and hexagonal maps.
type StaggerAxis int

const (
	// StaggerY staggers alternating rows horizontally (pointy-top hexagons).
	StaggerY StaggerAxis = iota
	// StaggerX staggers alternating columns vertically (flat-top hexagons).
	StaggerX
)

// StaggerIndex selects whether odd or even rows/columns are shifted in staggered and hexagonal maps.
type StaggerIndex int

const (
	// StaggerOdd shifts odd-indexed rows or columns (Tiled default).
	StaggerOdd StaggerIndex = iota
	// StaggerEven shifts even-indexed rows or columns.
	StaggerEven
)

func isStaggered(index int, staggerIndex StaggerIndex) bool {
	odd := (index & 1) != 0
	if staggerIndex == StaggerEven {
		return !odd
	}
	return odd
}

func (m *Tilemap) hexSide() float64 {
	if m.HexSideLength > 0 {
		return float64(m.HexSideLength)
	}
	if m.StaggerAxis == StaggerX {
		return float64(m.TileWidth) / 2
	}
	return float64(m.TileHeight) / 2
}

// TileOrigin returns the top-left corner of tile (col, row)'s bounding box in world coordinates.
func (m *Tilemap) TileOrigin(col, row int) core.Point {
	ox, oy := m.tileOrigin(col, row)
	return core.Point{
		X: m.Origin.X + core.Points(ox),
		Y: m.Origin.Y + core.Points(oy),
	}
}

func (m *Tilemap) tileOrigin(col, row int) (float32, float32) {
	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	switch m.Orientation {
	case OrientationIsometric:
		x := float64(col-row) * (tw / 2)
		y := float64(col+row) * (th / 2)
		return float32(x), float32(y)

	case OrientationStaggered:
		if m.StaggerAxis == StaggerX {
			x := float64(col) * (tw / 2)
			y := float64(row) * th
			if isStaggered(col, m.StaggerIndex) {
				y += th / 2
			}
			return float32(x), float32(y)
		}
		x := float64(col) * tw
		if isStaggered(row, m.StaggerIndex) {
			x += tw / 2
		}
		y := float64(row) * (th / 2)
		return float32(x), float32(y)

	case OrientationHexagonal:
		s := m.hexSide()
		if m.StaggerAxis == StaggerX {
			colW := (tw + s) / 2
			x := float64(col) * colW
			y := float64(row) * th
			if isStaggered(col, m.StaggerIndex) {
				y += th / 2
			}
			return float32(x), float32(y)
		}
		rowH := (th + s) / 2
		x := float64(col) * tw
		if isStaggered(row, m.StaggerIndex) {
			x += tw / 2
		}
		y := float64(row) * rowH
		return float32(x), float32(y)

	default:
		return float32(float64(col) * tw), float32(float64(row) * th)
	}
}

// TileToWorld returns the world-space center of tile (col, row) for the map's Orientation and Origin.
//
// WorldToTile(TileToWorld(col, row)) round-trips back to (col, row) for orthogonal, isometric, staggered, and hexagonal maps.
func (m *Tilemap) TileToWorld(col, row int) core.Point {
	ox, oy := m.tileOrigin(col, row)
	return core.Point{
		X: m.Origin.X + core.Points(ox+m.TileWidth/2),
		Y: m.Origin.Y + core.Points(oy+m.TileHeight/2),
	}
}

// WorldToTile converts a world-space point into the (col, row) tile coordinates containing it for the map's Orientation and Origin.
func (m *Tilemap) WorldToTile(p core.Point) (col, row int) {
	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	if tw <= 0 || th <= 0 {
		return 0, 0
	}
	px, py := float64(p.X-m.Origin.X), float64(p.Y-m.Origin.Y)

	switch m.Orientation {
	case OrientationIsometric:
		hw, hh := tw/2, th/2
		u := (px - hw) / hw
		v := py / hh
		return int(math.Floor((u + v) / 2)), int(math.Floor((v - u) / 2))

	case OrientationStaggered:
		hw, hh := tw/2, th/2
		if m.StaggerAxis == StaggerX {
			c := int(math.Floor(px / hw))
			bestCol, bestRow := 0, 0
			bestDist := math.Inf(1)
			for _, cc := range [2]int{c, c - 1} {
				off := 0.0
				if isStaggered(cc, m.StaggerIndex) {
					off = hh
				}
				rr := int(math.Floor((py - off) / th))
				cx := float64(cc)*hw + hw
				cy := float64(rr)*th + off + hh
				dist := math.Abs(px-cx)/hw + math.Abs(py-cy)/hh
				if dist < bestDist {
					bestDist = dist
					bestCol, bestRow = cc, rr
				}
			}
			return bestCol, bestRow
		}
		r := int(math.Floor(py / hh))
		bestCol, bestRow := 0, 0
		bestDist := math.Inf(1)
		for _, rr := range [2]int{r, r - 1} {
			off := 0.0
			if isStaggered(rr, m.StaggerIndex) {
				off = hw
			}
			cc := int(math.Floor((px - off) / tw))
			cx := float64(cc)*tw + off + hw
			cy := float64(rr)*hh + hh
			dist := math.Abs(px-cx)/hw + math.Abs(py-cy)/hh
			if dist < bestDist {
				bestDist = dist
				bestCol, bestRow = cc, rr
			}
		}
		return bestCol, bestRow

	case OrientationHexagonal:
		s := m.hexSide()
		if m.StaggerAxis == StaggerX {
			colW := (tw + s) / 2
			if colW <= 0 {
				return 0, 0
			}
			x0 := tw / 2
			y0 := th / 2
			if m.StaggerIndex == StaggerEven {
				y0 = th
			}
			qf := (px - x0) / colW
			rf := (py-y0)/th - 0.5*qf
			q, r := cubeRound(qf, rf)
			return m.axialToOffset(q, r)
		}
		rowH := (th + s) / 2
		if rowH <= 0 {
			return 0, 0
		}
		x0 := tw / 2
		if m.StaggerIndex == StaggerEven {
			x0 = tw
		}
		y0 := th / 2
		rf := (py - y0) / rowH
		qf := (px-x0)/tw - 0.5*rf
		q, r := cubeRound(qf, rf)
		return m.axialToOffset(q, r)

	default:
		return int(math.Floor(px / tw)), int(math.Floor(py / th))
	}
}

func cubeRound(qf, rf float64) (int, int) {
	sf := -qf - rf
	q := math.Round(qf)
	r := math.Round(rf)
	s := math.Round(sf)
	qDiff := math.Abs(q - qf)
	rDiff := math.Abs(r - rf)
	sDiff := math.Abs(s - sf)
	if qDiff > rDiff && qDiff > sDiff {
		q = -r - s
	} else if rDiff > sDiff {
		r = -q - s
	}
	return int(q), int(r)
}

func (m *Tilemap) offsetToAxial(col, row int) (q, r int) {
	if m.StaggerAxis == StaggerX {
		if m.StaggerIndex == StaggerEven {
			return col, row - (col+(col&1))/2
		}
		return col, row - (col-(col&1))/2
	}
	if m.StaggerIndex == StaggerEven {
		return col - (row+(row&1))/2, row
	}
	return col - (row-(row&1))/2, row
}

func (m *Tilemap) axialToOffset(q, r int) (col, row int) {
	if m.StaggerAxis == StaggerX {
		if m.StaggerIndex == StaggerEven {
			return q, r + (q+(q&1))/2
		}
		return q, r + (q-(q&1))/2
	}
	if m.StaggerIndex == StaggerEven {
		return q + (r+(r&1))/2, r
	}
	return q + (r-(r&1))/2, r
}

var hexAxialDirections = [6][2]int{
	{1, 0},
	{1, -1},
	{0, -1},
	{-1, 0},
	{-1, 1},
	{0, 1},
}

// HexNeighbors returns the 6 hexagonal neighbors of offset cell (col, row) for the map's StaggerAxis and StaggerIndex.
func (m *Tilemap) HexNeighbors(col, row int) [6][2]int {
	q, r := m.offsetToAxial(col, row)
	var out [6][2]int
	for i, dir := range hexAxialDirections {
		nc, nr := m.axialToOffset(q+dir[0], r+dir[1])
		out[i] = [2]int{nc, nr}
	}
	return out
}

// HexDistance returns the grid step distance between hexagonal cells (c1, r1) and (c2, r2).
func (m *Tilemap) HexDistance(c1, r1, c2, r2 int) int {
	q1, r1a := m.offsetToAxial(c1, r1)
	q2, r2a := m.offsetToAxial(c2, r2)
	dq := q1 - q2
	dr := r1a - r2a
	ds := -dq - dr
	if dq < 0 {
		dq = -dq
	}
	if dr < 0 {
		dr = -dr
	}
	if ds < 0 {
		ds = -ds
	}
	return (dq + dr + ds) / 2
}

func (m *Tilemap) projectedWorldBounds(w, h int) core.Bounds {
	if w <= 0 || h <= 0 || m.TileWidth <= 0 || m.TileHeight <= 0 {
		return core.Bounds{}
	}
	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	switch m.Orientation {
	case OrientationIsometric:
		minX := -float64(h-1) * (tw / 2)
		maxX := float64(w-1)*(tw/2) + tw
		minY := 0.0
		maxY := float64(w+h-2)*(th/2) + th
		return core.Bounds{
			Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
			Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
		}

	case OrientationStaggered, OrientationHexagonal:
		var stepX, stepY float64
		if m.Orientation == OrientationStaggered {
			if m.StaggerAxis == StaggerX {
				stepX, stepY = tw/2, th
			} else {
				stepX, stepY = tw, th/2
			}
		} else {
			s := m.hexSide()
			if m.StaggerAxis == StaggerX {
				stepX, stepY = (tw+s)/2, th
			} else {
				stepX, stepY = tw, (th+s)/2
			}
		}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		if m.StaggerAxis == StaggerX {
			minX = 0
			maxX = float64(w-1)*stepX + tw
			for c := 0; c < min(w, 2); c++ {
				off := 0.0
				if isStaggered(c, m.StaggerIndex) {
					off = th / 2
				}
				minY = min(minY, off)
				maxY = max(maxY, float64(h-1)*stepY+off+th)
			}
		} else {
			minY = 0
			maxY = float64(h-1)*stepY + th
			for r := 0; r < min(h, 2); r++ {
				off := 0.0
				if isStaggered(r, m.StaggerIndex) {
					off = tw / 2
				}
				minX = min(minX, off)
				maxX = max(maxX, float64(w-1)*stepX+off+tw)
			}
		}
		return core.Bounds{
			Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
			Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
		}

	default:
		return core.Bounds{
			Size: core.Size{
				Width:  core.Points(float32(w) * m.TileWidth),
				Height: core.Points(float32(h) * m.TileHeight),
			},
		}
	}
}

func (m *Tilemap) projectedTileRangeBounds(minCol, minRow, maxCol, maxRow int) core.Bounds {
	if minCol > maxCol || minRow > maxRow || m.TileWidth <= 0 || m.TileHeight <= 0 {
		return core.Bounds{}
	}
	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	consider := func(c, r int) {
		ox, oy := m.tileOrigin(c, r)
		minX = min(minX, float64(ox))
		minY = min(minY, float64(oy))
		maxX = max(maxX, float64(ox)+tw)
		maxY = max(maxY, float64(oy)+th)
	}
	// Check the four extreme corners plus adjacent parity cells for staggered/hex projections.
	cols := [4]int{minCol, min(minCol+1, maxCol), max(maxCol-1, minCol), maxCol}
	rows := [4]int{minRow, min(minRow+1, maxRow), max(maxRow-1, minRow), maxRow}
	for _, r := range rows {
		for _, c := range cols {
			consider(c, r)
		}
	}
	return core.Bounds{
		Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
		Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
	}
}

func (m *Tilemap) drawProjectedLayer(d *core.Drawing, layer *TileLayer, cam *Camera, offX, offY float32) {
	chunked := layer.IsChunked()
	var boundMinCol, boundMinRow, boundMaxCol, boundMaxRow int
	var lw int
	if chunked {
		var ok bool
		boundMinCol, boundMinRow, boundMaxCol, boundMaxRow, ok = layer.ChunkTileBounds()
		if !ok {
			return
		}
	} else {
		var lh int
		lw, lh = m.layerSize(layer)
		if lw <= 0 || lh <= 0 || len(layer.Data) == 0 {
			return
		}
		boundMinCol, boundMinRow = 0, 0
		boundMaxCol, boundMaxRow = lw-1, lh-1
	}
	extraL, extraR, extraT, extraB := m.maxTilePadding()

	hasCam := cam != nil
	var vbMinX, vbMinY, vbMaxX, vbMaxY float64
	if hasCam {
		vb := cam.VisibleBounds()
		if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
			return
		}
		vbMinX = float64(vb.Origin.X)
		vbMinY = float64(vb.Origin.Y)
		vbMaxX = vbMinX + float64(vb.Size.Width)
		vbMaxY = vbMinY + float64(vb.Size.Height)
	}

	savedLayer := false
	drawCell := func(col, row int) {
		var gid uint32
		if chunked {
			gid = layer.tileAtChunked(col, row)
		} else {
			idx := row*lw + col
			if idx < 0 || idx >= len(layer.Data) {
				return
			}
			gid = layer.Data[idx]
		}
		if gid == 0 {
			return
		}
		ts, localID := m.TilesetForGID(gid)
		if ts == nil {
			return
		}
		img, sx, sy, sw, sh, ox, oy, tw, th, ok := ts.tileSource(localID)
		if !ok {
			return
		}
		tx, ty := m.tileOrigin(col, row)
		dstX := offX + tx + ts.TileOffsetX
		dstY := offY + ty + m.TileHeight - th + ts.TileOffsetY
		if hasCam {
			rx0 := float64(dstX + ox)
			ry0 := float64(dstY + oy)
			if rx0+float64(sw) <= vbMinX || rx0 >= vbMaxX || ry0+float64(sh) <= vbMinY || ry0 >= vbMaxY {
				return
			}
		}
		if layer.Opacity < 1 && !savedLayer {
			d.Save().SetAlpha(layer.Opacity)
			savedLayer = true
		}
		drawPlacedTile(d, img, layer.TintColor, sx, sy, sw, sh, ox, oy, dstX, dstY, tw, th, gid&FlipMask)
	}

	tw, th := float64(m.TileWidth), float64(m.TileHeight)

	switch m.Orientation {
	case OrientationIsometric:
		minCol, maxCol := boundMinCol, boundMaxCol
		minRow, maxRow := boundMinRow, boundMaxRow
		dMin, dMax := float64(minCol-maxRow), float64(maxCol-minRow)
		sMin, sMax := float64(minCol+minRow), float64(maxCol+maxRow)
		if hasCam {
			lx0 := vbMinX - float64(offX)
			lx1 := vbMaxX - float64(offX)
			ly0 := vbMinY - float64(offY)
			ly1 := vbMaxY - float64(offY)
			dMin = (lx0 - tw - extraR) / (tw / 2)
			dMax = (lx1 + extraL) / (tw / 2)
			sMin = (ly0 - th - extraB) / (th / 2)
			sMax = (ly1 + extraT) / (th / 2)
			minCol = max(boundMinCol, int(math.Floor((sMin+dMin)/2)))
			maxCol = min(boundMaxCol, int(math.Ceil((sMax+dMax)/2)))
			minRow = max(boundMinRow, int(math.Floor((sMin-dMax)/2)))
			maxRow = min(boundMaxRow, int(math.Ceil((sMax-dMin)/2)))
		}
		if minCol <= maxCol && minRow <= maxRow {
			startS := max(minCol+minRow, int(math.Floor(sMin)))
			endS := min(maxCol+maxRow, int(math.Ceil(sMax)))
			for s := startS; s <= endS; s++ {
				c0 := max(minCol, s-maxRow)
				c1 := min(maxCol, s-minRow)
				if hasCam {
					c0 = max(c0, int(math.Floor((float64(s)+dMin)/2)))
					c1 = min(c1, int(math.Ceil((float64(s)+dMax)/2)))
				}
				for col := c0; col <= c1; col++ {
					row := s - col
					drawCell(col, row)
				}
			}
		}

	case OrientationStaggered, OrientationHexagonal:
		var stepX, stepY float64
		if m.Orientation == OrientationStaggered {
			if m.StaggerAxis == StaggerX {
				stepX, stepY = tw/2, th
			} else {
				stepX, stepY = tw, th/2
			}
		} else {
			s := m.hexSide()
			if m.StaggerAxis == StaggerX {
				stepX, stepY = (tw+s)/2, th
			} else {
				stepX, stepY = tw, (th+s)/2
			}
		}
		if stepX <= 0 || stepY <= 0 {
			return
		}

		minCol, maxCol := boundMinCol, boundMaxCol
		minRow, maxRow := boundMinRow, boundMaxRow
		if hasCam {
			lx0 := vbMinX - float64(offX)
			lx1 := vbMaxX - float64(offX)
			ly0 := vbMinY - float64(offY)
			ly1 := vbMaxY - float64(offY)
			if m.StaggerAxis == StaggerX {
				minCol = max(boundMinCol, int(math.Floor((lx0-tw-extraR)/stepX)))
				maxCol = min(boundMaxCol, int(math.Ceil((lx1+extraL)/stepX)))
				minRow = max(boundMinRow, int(math.Floor((ly0-th*1.5-extraB)/stepY)))
				maxRow = min(boundMaxRow, int(math.Ceil((ly1+extraT)/stepY)))
			} else {
				minCol = max(boundMinCol, int(math.Floor((lx0-tw*1.5-extraR)/stepX)))
				maxCol = min(boundMaxCol, int(math.Ceil((lx1+extraL)/stepX)))
				minRow = max(boundMinRow, int(math.Floor((ly0-th-extraB)/stepY)))
				maxRow = min(boundMaxRow, int(math.Ceil((ly1+extraT)/stepY)))
			}
		}
		if minCol <= maxCol && minRow <= maxRow {
			if m.StaggerAxis == StaggerX {
				for row := minRow; row <= maxRow; row++ {
					// Draw non-staggered (higher Y) columns before staggered (lower Y) columns for strict painter's order.
					for col := minCol; col <= maxCol; col++ {
						if !isStaggered(col, m.StaggerIndex) {
							drawCell(col, row)
						}
					}
					for col := minCol; col <= maxCol; col++ {
						if isStaggered(col, m.StaggerIndex) {
							drawCell(col, row)
						}
					}
				}
			} else {
				for row := minRow; row <= maxRow; row++ {
					for col := minCol; col <= maxCol; col++ {
						drawCell(col, row)
					}
				}
			}
		}
	}

	if savedLayer {
		d.Restore()
	}
}
