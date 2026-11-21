package tilemap

// TileAt returns the raw GID (including flip flags) at (col, row) in layerIndex, or 0 if out of bounds.
func (m *Tilemap) TileAt(layerIndex, col, row int) uint32 {
	if layerIndex < 0 || layerIndex >= len(m.Layers) {
		return 0
	}
	l := &m.Layers[layerIndex]
	lw, lh := m.layerSize(l)
	if col < 0 || row < 0 || col >= lw || row >= lh {
		return 0
	}
	idx := row*lw + col
	if idx >= len(l.Data) {
		return 0
	}
	return l.Data[idx]
}

// SetTile writes a raw GID at (col, row) in layerIndex; out-of-bounds calls are ignored.
//
// Any cached static image for layerIndex is marked dirty so subsequent draws reflect the change immediately, and collision queries (FlagsAt, TilesInRect, Raycast) observe the updated tile at once.
func (m *Tilemap) SetTile(layerIndex, col, row int, gid uint32) {
	if layerIndex < 0 || layerIndex >= len(m.Layers) {
		return
	}
	l := &m.Layers[layerIndex]
	lw, lh := m.layerSize(l)
	if col < 0 || row < 0 || col >= lw || row >= lh {
		return
	}
	idx := row*lw + col
	if l.Data == nil && lw > 0 && lh > 0 {
		l.Data = make([]uint32, lw*lh)
	}
	if idx >= len(l.Data) {
		return
	}
	if l.Data[idx] == gid {
		return
	}
	l.Data[idx] = gid
	m.InvalidateLayerCache(layerIndex)
}

// FillRect writes gid across the clamped rectangle [col, col+w) x [row, row+h) in layerIndex.
//
// Out-of-bounds portions are clipped, non-positive sizes are ignored, and any static cache for layerIndex is invalidated when at least one cell changes.
func (m *Tilemap) FillRect(layerIndex, col, row, w, h int, gid uint32) {
	if layerIndex < 0 || layerIndex >= len(m.Layers) || w <= 0 || h <= 0 {
		return
	}
	l := &m.Layers[layerIndex]
	lw, lh := m.layerSize(l)
	if lw <= 0 || lh <= 0 {
		return
	}
	x0 := max(0, col)
	y0 := max(0, row)
	x1 := min(lw, col+w)
	y1 := min(lh, row+h)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	if l.Data == nil {
		l.Data = make([]uint32, lw*lh)
	}
	changed := false
	for r := y0; r < y1; r++ {
		rowStart := r * lw
		for c := x0; c < x1; c++ {
			idx := rowStart + c
			if idx >= len(l.Data) {
				continue
			}
			if l.Data[idx] != gid {
				l.Data[idx] = gid
				changed = true
			}
		}
	}
	if changed {
		m.InvalidateLayerCache(layerIndex)
	}
}
