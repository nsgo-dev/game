package tilemap

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// PropSortOriginY is the tile custom property name for an explicit vertical Y-sort origin offset
// (in points) added to a tile's default bottom-edge sort line.
//
// Negative values move the sort line upward (for example into a tall tree's trunk); positive
// values move it downward. Lookup also accepts "y_sort_origin" and "ysort_origin" as aliases.
const PropSortOriginY = "sort_origin_y"

// YSortTile is one visible tile collected from a TileLayer so it can be depth-sorted and
// interleaved with scene sprites and custom drawables in ascending SortY order.
type YSortTile struct {
	// SortY is the world-space Y coordinate used for painter's-order depth sorting:
	// the tile's bottom edge plus any per-tile PropSortOriginY offset.
	SortY float32
	// Col and Row are the tile's grid coordinates within its TileLayer.
	Col int
	Row int
	// GID is the raw global tile ID (including flip flags) stored in the layer.
	GID uint32
	// LayerIndex is the index of the source TileLayer in Tilemap.Layers, or -1 if not attached.
	LayerIndex int

	ts           *Tileset
	frameLocalID uint32
	sx, sy       float32
	sw, sh       float32
	ox, oy       float32
	dstX         float32
	dstY         float32
	tw, th       float32
	opacity      float32
	tint         core.Color
	tinted       bool
	flags        uint32
}

// Draw draws this single tile into d, applying its layer opacity, tint, and flip flags while leaving d's state unchanged.
func (t *YSortTile) Draw(d *core.Drawing) {
	if d == nil || t == nil || t.ts == nil || t.opacity <= 0 || isInvisibleTint(t.tint, t.tinted) {
		return
	}
	img := t.imageSource()
	if t.flags == 0 {
		if t.opacity < 1 {
			d.Save().SetAlpha(t.opacity)
			drawTileImage(d, img, t.tint, t.tinted, t.sx, t.sy, t.sw, t.sh, t.dstX+t.ox, t.dstY+t.oy, t.sw, t.sh)
			d.Restore()
			return
		}
		drawTileImage(d, img, t.tint, t.tinted, t.sx, t.sy, t.sw, t.sh, t.dstX+t.ox, t.dstY+t.oy, t.sw, t.sh)
		return
	}
	a, b, c, dd, e, f := flipMatrix(t.flags, t.dstX, t.dstY, t.tw, t.th)
	d.Save()
	if t.opacity < 1 {
		d.SetAlpha(t.opacity)
	}
	d.Transform(a, b, c, dd, e, f)
	drawTileImage(d, img, t.tint, t.tinted, t.sx, t.sy, t.sw, t.sh, t.ox, t.oy, t.sw, t.sh)
	d.Restore()
}

func (t *YSortTile) imageSource() core.ImageSource {
	if len(t.ts.TileImages) > 0 {
		if ti, ok := t.ts.TileImages[t.frameLocalID]; ok {
			return ti.Image
		}
	}
	img := t.ts.Image
	if t.ts.Atlas != nil && img.URI == "" && len(img.Data) == 0 && img.Pixels == nil {
		img = t.ts.Atlas.Image
	}
	return img
}

// SortYSortTiles sorts tiles in stable ascending SortY order without allocating.
func SortYSortTiles(tiles []YSortTile) {
	slices.SortStableFunc(tiles, func(a, b YSortTile) int {
		return cmp.Compare(a.SortY, b.SortY)
	})
}

// YSortDrawer batches consecutive unflipped YSortTile values sharing the same tileset and
// opacity into Drawing.DrawAtlas calls while reusing its internal scratch slices across frames.
type YSortDrawer struct {
	ts      *Tileset
	opacity float32
	lone    YSortTile
	rects   []core.Bounds
	xforms  []core.RSXform
}

// Push queues tile t into d, flushing any incompatible run first.
func (dr *YSortDrawer) Push(d *core.Drawing, t *YSortTile) {
	if d == nil || t == nil || t.ts == nil || t.opacity <= 0 || isInvisibleTint(t.tint, t.tinted) {
		return
	}
	if t.flags != 0 || isTinted(t.tint, t.tinted) {
		dr.Flush(d)
		t.Draw(d)
		return
	}
	if len(dr.rects) > 0 && (dr.ts != t.ts || dr.opacity != t.opacity || (len(t.ts.TileImages) > 0 && dr.lone.frameLocalID != t.frameLocalID)) {
		dr.Flush(d)
	}
	if len(dr.rects) == 0 {
		dr.ts = t.ts
		dr.opacity = t.opacity
		dr.lone = *t
	}
	dr.rects = append(dr.rects, core.Bounds{
		Origin: core.Point{X: core.Points(t.sx), Y: core.Points(t.sy)},
		Size:   core.Size{Width: core.Points(t.sw), Height: core.Points(t.sh)},
	})
	dr.xforms = append(dr.xforms, core.RSXform{
		SCos: 1,
		SSin: 0,
		TX:   t.dstX + t.ox,
		TY:   t.dstY + t.oy,
	})
}

// Flush emits any queued tiles into d and resets the run state while preserving slice capacity.
func (dr *YSortDrawer) Flush(d *core.Drawing) {
	switch {
	case len(dr.rects) == 1:
		dr.lone.Draw(d)
	case len(dr.rects) > 1:
		img := dr.lone.imageSource()
		if dr.opacity < 1 {
			d.Save().SetAlpha(dr.opacity).DrawAtlas(img, dr.rects, dr.xforms, nil).Restore()
		} else {
			d.DrawAtlas(img, dr.rects, dr.xforms, nil)
		}
	}
	dr.ts = nil
	dr.opacity = 0
	dr.lone = YSortTile{}
	dr.rects = dr.rects[:0]
	dr.xforms = dr.xforms[:0]
}

// SetLayerYSort enables or disables per-tile Y-sorting on m.Layers[layerIndex].
// Out-of-range layer indices are ignored.
func (m *Tilemap) SetLayerYSort(layerIndex int, enabled bool) {
	if m == nil || layerIndex < 0 || layerIndex >= len(m.Layers) {
		return
	}
	m.Layers[layerIndex].YSort = enabled
}

// HasYSortLayers reports whether m contains at least one visible tile layer with YSort enabled.
func (m *Tilemap) HasYSortLayers() bool {
	if m == nil {
		return false
	}
	for i := range m.Layers {
		l := &m.Layers[i]
		if l.YSort && l.Visible && l.Opacity > 0 {
			return true
		}
	}
	return false
}

// SetTileSortOriginY sets the per-tile vertical Y-sort origin offset (in points relative to the
// tile's default bottom edge) on gid's tileset under PropSortOriginY.
//
// Negative values shift the sort line upward (for example into a tall tree's trunk);
// positive values shift it downward.
func (m *Tilemap) SetTileSortOriginY(gid uint32, offsetY float32) {
	if m == nil {
		return
	}
	ts, localID := m.TilesetForGID(gid)
	if ts == nil {
		return
	}
	if ts.TileProperties == nil {
		ts.TileProperties = make(map[uint32]Properties)
	}
	props := ts.TileProperties[localID]
	if props == nil {
		props = make(Properties)
		ts.TileProperties[localID] = props
	}
	props[PropSortOriginY] = Property{
		Name:  PropSortOriginY,
		Type:  "float",
		Value: float64(offsetY),
	}
}

// TileSortOriginY returns the per-tile vertical Y-sort origin offset configured on gid
// (checking PropSortOriginY, "y_sort_origin", and "ysort_origin"), or 0 if unset.
func (m *Tilemap) TileSortOriginY(gid uint32) float32 {
	if m == nil {
		return 0
	}
	ts, localID := m.TilesetForGID(gid)
	off, _ := tileSortOriginOffset(ts, localID)
	return off
}

func tileSortOriginOffset(ts *Tileset, localID uint32) (float32, bool) {
	if ts == nil || ts.TileProperties == nil {
		return 0, false
	}
	props := ts.TileProperties[localID]
	if len(props) == 0 {
		return 0, false
	}
	if v, ok := props.Float(PropSortOriginY); ok {
		return float32(v), true
	}
	if v, ok := props.Float("y_sort_origin"); ok {
		return float32(v), true
	}
	if v, ok := props.Float("ysort_origin"); ok {
		return float32(v), true
	}
	return 0, false
}

// VisitLayers calls fn for every tile layer and image layer of m in map draw order.
// Exactly one of tile or img is non-nil on each call.
func (m *Tilemap) VisitLayers(fn func(tile *TileLayer, img *ImageLayer)) {
	if m == nil || fn == nil {
		return
	}
	m.visitAllLayers(func(tl *TileLayer, il *ImageLayer, _ *ObjectLayer) {
		if tl != nil || il != nil {
			fn(tl, il)
		}
	})
}

func (m *Tilemap) visitAllLayers(fn func(tile *TileLayer, img *ImageLayer, obj *ObjectLayer)) {
	if m == nil || fn == nil {
		return
	}
	if len(m.layerOrder) == 0 {
		for i := range m.ImageLayers {
			fn(nil, &m.ImageLayers[i], nil)
		}
		for i := range m.Layers {
			fn(&m.Layers[i], nil, nil)
		}
		for i := range m.Objects {
			fn(nil, nil, &m.Objects[i])
		}
		return
	}

	var stackTile [64]bool
	var stackImage [64]bool
	var stackObj [64]bool
	var drawnTile, drawnImage, drawnObj []bool

	prep := func(n int, stack []bool, heap *[]bool) []bool {
		if n == 0 {
			return nil
		}
		if n <= len(stack) {
			return stack[:n]
		}
		if cap(*heap) < n {
			*heap = make([]bool, n)
		} else {
			*heap = (*heap)[:n]
			clear(*heap)
		}
		return *heap
	}
	drawnTile = prep(len(m.Layers), stackTile[:], &m.drawnTile)
	drawnImage = prep(len(m.ImageLayers), stackImage[:], &m.drawnImage)
	drawnObj = prep(len(m.Objects), stackObj[:], &m.drawnObject)

	for _, ref := range m.layerOrder {
		switch ref.kind {
		case layerKindTile:
			if ref.index >= 0 && ref.index < len(m.Layers) && !drawnTile[ref.index] {
				drawnTile[ref.index] = true
				fn(&m.Layers[ref.index], nil, nil)
			}
		case layerKindImage:
			if ref.index >= 0 && ref.index < len(m.ImageLayers) && !drawnImage[ref.index] {
				drawnImage[ref.index] = true
				fn(nil, &m.ImageLayers[ref.index], nil)
			}
		case layerKindObject:
			if ref.index >= 0 && ref.index < len(m.Objects) && !drawnObj[ref.index] {
				drawnObj[ref.index] = true
				fn(nil, nil, &m.Objects[ref.index])
			}
		}
	}
	for i := range m.ImageLayers {
		if !drawnImage[i] {
			fn(nil, &m.ImageLayers[i], nil)
		}
	}
	for i := range m.Layers {
		if !drawnTile[i] {
			fn(&m.Layers[i], nil, nil)
		}
	}
	for i := range m.Objects {
		if !drawnObj[i] {
			fn(nil, nil, &m.Objects[i])
		}
	}
}

func (m *Tilemap) layerIndexFor(layer *TileLayer) int {
	for i := range m.Layers {
		if &m.Layers[i] == layer {
			return i
		}
	}
	return -1
}

// AppendYSortTiles appends every visible non-empty tile of layer overlapping cam.VisibleBounds()
// (or every non-empty tile when cam is nil) to dst in deterministic painter's order, returning the
// updated slice.
//
// Each tile's SortY is its bottom edge in world coordinates (offY + tileOriginY + TileHeight) plus
// any per-tile PropSortOriginY offset. Animated tiles resolve to their current frame at call time.
// Reusing dst across frames performs zero heap allocations once its capacity is warmed up.
func (m *Tilemap) AppendYSortTiles(dst []YSortTile, layer *TileLayer, cam *Camera) []YSortTile {
	if m == nil || layer == nil || !layer.Visible || layer.Opacity <= 0 || m.TileWidth <= 0 || m.TileHeight <= 0 {
		return dst
	}
	chunked := layer.IsChunked()
	var boundMinCol, boundMinRow, boundMaxCol, boundMaxRow int
	var lw int
	if chunked {
		var ok bool
		boundMinCol, boundMinRow, boundMaxCol, boundMaxRow, ok = layer.ChunkTileBounds()
		if !ok {
			return dst
		}
	} else {
		var lh int
		lw, lh = m.layerSize(layer)
		if lw <= 0 || lh <= 0 || len(layer.Data) == 0 {
			return dst
		}
		boundMinCol, boundMinRow = 0, 0
		boundMaxCol, boundMaxRow = lw-1, lh-1
	}

	offX, offY := effectiveLayerOffset(float32(m.Origin.X)+layer.OffsetX, float32(m.Origin.Y)+layer.OffsetY, layer.ParallaxX, layer.ParallaxY, cam)
	extraL, extraR, extraT, extraB := m.maxTilePadding()
	layerIdx := m.layerIndexFor(layer)

	hasCam := cam != nil
	var vbMinX, vbMinY, vbMaxX, vbMaxY float64
	if hasCam {
		vb := cam.VisibleBounds()
		if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
			return dst
		}
		vbMinX = float64(vb.Origin.X)
		vbMinY = float64(vb.Origin.Y)
		vbMaxX = vbMinX + float64(vb.Size.Width)
		vbMaxY = vbMinY + float64(vb.Size.Height)
	}

	appendCell := func(col, row int) {
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
		frameLocalID := localID
		if ts.Animations != nil {
			if anim, found := ts.Animations[localID]; found && anim != nil && len(anim.Frames) > 0 {
				frameLocalID = anim.CurrentTileID()
			}
		}
		_, sx, sy, sw, sh, ox, oy, tw, th, ok := ts.tileSource(localID)
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
		sortOff, found := tileSortOriginOffset(ts, localID)
		if !found && frameLocalID != localID {
			sortOff, _ = tileSortOriginOffset(ts, frameLocalID)
		}
		dst = append(dst, YSortTile{
			SortY:        offY + ty + m.TileHeight + sortOff,
			Col:          col,
			Row:          row,
			GID:          gid,
			LayerIndex:   layerIdx,
			ts:           ts,
			frameLocalID: frameLocalID,
			sx:           sx,
			sy:           sy,
			sw:           sw,
			sh:           sh,
			ox:           ox,
			oy:           oy,
			dstX:         dstX,
			dstY:         dstY,
			tw:           tw,
			th:           th,
			opacity:      layer.Opacity,
			tint:         layer.TintColor,
			tinted:       layer.Tinted,
			flags:        gid & FlipMask,
		})
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
					appendCell(col, s-col)
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
			return dst
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
					for col := minCol; col <= maxCol; col++ {
						if !isStaggered(col, m.StaggerIndex) {
							appendCell(col, row)
						}
					}
					for col := minCol; col <= maxCol; col++ {
						if isStaggered(col, m.StaggerIndex) {
							appendCell(col, row)
						}
					}
				}
			} else {
				for row := minRow; row <= maxRow; row++ {
					for col := minCol; col <= maxCol; col++ {
						appendCell(col, row)
					}
				}
			}
		}

	default: // OrientationOrthogonal
		minCol, maxCol := boundMinCol, boundMaxCol
		minRow, maxRow := boundMinRow, boundMaxRow
		if hasCam {
			minX := vbMinX - float64(offX)
			maxX := vbMaxX - float64(offX)
			minY := vbMinY - float64(offY)
			maxY := vbMaxY - float64(offY)
			minCol = max(boundMinCol, int(math.Floor((minX-extraR)/tw)))
			maxCol = min(boundMaxCol, int(math.Ceil((maxX+extraL)/tw))-1)
			minRow = max(boundMinRow, int(math.Floor((minY-extraB)/th)))
			maxRow = min(boundMaxRow, int(math.Ceil((maxY+extraT)/th))-1)
		}
		if minCol <= maxCol && minRow <= maxRow {
			for row := minRow; row <= maxRow; row++ {
				for col := minCol; col <= maxCol; col++ {
					appendCell(col, row)
				}
			}
		}
	}

	return dst
}
