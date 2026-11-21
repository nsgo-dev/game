package tilemap

// Bitmask constants for 4-bit cardinal and 8-bit corner-gated autotiling.
const (
	MaskNorth     uint8 = 1 << 0
	MaskEast      uint8 = 1 << 1
	MaskSouth     uint8 = 1 << 2
	MaskWest      uint8 = 1 << 3
	MaskNorthEast uint8 = 1 << 4
	MaskSouthEast uint8 = 1 << 5
	MaskSouthWest uint8 = 1 << 6
	MaskNorthWest uint8 = 1 << 7
)

// AutotileMode chooses between 4-bit cardinal (16-tile) and 8-bit corner-gated (47-tile) autotiling.
type AutotileMode int

const (
	// Autotile4 uses the 4 cardinal neighbors (N=1, E=2, S=4, W=8; 16 combinations).
	Autotile4 AutotileMode = iota
	// Autotile8 uses the 4 cardinal neighbors plus 4 corner-gated diagonal neighbors (47 combinations).
	Autotile8
)

// AutotileSet maps autotile neighbor bitmasks to tile GIDs, falling back to Default when a mask has no entry in Tiles.
type AutotileSet struct {
	Mode    AutotileMode
	Tiles   map[uint8]uint32
	Default uint32
}

// Tile returns the GID mapped to mask, or s.Default when mask is not present in s.Tiles.
func (s AutotileSet) Tile(mask uint8) uint32 {
	if s.Tiles != nil {
		if gid, ok := s.Tiles[mask]; ok {
			return gid
		}
	}
	return s.Default
}

// ComputeMask4 computes the 4-bit cardinal neighbor mask (N=1, E=2, S=4, W=8) at (col, row).
func ComputeMask4(match func(col, row int) bool, col, row int) uint8 {
	if match == nil {
		return 0
	}
	var mask uint8
	if match(col, row-1) {
		mask |= MaskNorth
	}
	if match(col+1, row) {
		mask |= MaskEast
	}
	if match(col, row+1) {
		mask |= MaskSouth
	}
	if match(col-1, row) {
		mask |= MaskWest
	}
	return mask
}

// ComputeMask8 computes the 8-bit corner-gated Wang/blob neighbor mask (47-tile blob) at (col, row).
//
// Diagonal bits (NE, SE, SW, NW) are only set when both adjacent cardinal neighbors also match.
func ComputeMask8(match func(col, row int) bool, col, row int) uint8 {
	if match == nil {
		return 0
	}
	n := match(col, row-1)
	e := match(col+1, row)
	s := match(col, row+1)
	w := match(col-1, row)

	var mask uint8
	if n {
		mask |= MaskNorth
	}
	if e {
		mask |= MaskEast
	}
	if s {
		mask |= MaskSouth
	}
	if w {
		mask |= MaskWest
	}
	if n && e && match(col+1, row-1) {
		mask |= MaskNorthEast
	}
	if s && e && match(col+1, row+1) {
		mask |= MaskSouthEast
	}
	if s && w && match(col-1, row+1) {
		mask |= MaskSouthWest
	}
	if n && w && match(col-1, row-1) {
		mask |= MaskNorthWest
	}
	return mask
}

func (s AutotileSet) computeMask(match func(col, row int) bool, col, row int) uint8 {
	if s.Mode == Autotile8 {
		return ComputeMask8(match, col, row)
	}
	return ComputeMask4(match, col, row)
}

// ApplyAutotile updates every terrain cell in layerIndex using set's bitmask-to-GID mapping.
//
// terrainID reports whether a raw tile GID belongs to the terrain (nil treats any non-zero GID as terrain).
func (m *Tilemap) ApplyAutotile(layerIndex int, set AutotileSet, terrainID func(gid uint32) bool) {
	if layerIndex < 0 || layerIndex >= len(m.Layers) {
		return
	}
	l := &m.Layers[layerIndex]
	lw, lh := m.layerSize(l)
	if lw <= 0 || lh <= 0 || len(l.Data) == 0 {
		return
	}
	if terrainID == nil {
		terrainID = func(gid uint32) bool { return gid != 0 }
	}

	// Snapshot terrain membership so writing tiles does not disturb neighbor evaluation.
	member := make([]bool, lw*lh)
	for r := range lh {
		rowStart := r * lw
		for c := range lw {
			idx := rowStart + c
			if idx < len(l.Data) && terrainID(l.Data[idx]) {
				member[idx] = true
			}
		}
	}
	match := func(c, r int) bool {
		if c < 0 || r < 0 || c >= lw || r >= lh {
			return false
		}
		return member[r*lw+c]
	}

	for r := range lh {
		rowStart := r * lw
		for c := range lw {
			if !member[rowStart+c] {
				continue
			}
			mask := set.computeMask(match, c, r)
			m.SetTile(layerIndex, c, r, set.Tile(mask))
		}
	}
}

// SetAutotile places (placed=true) or removes (placed=false) a terrain tile at (col, row) in layerIndex and updates that cell and all 8 surrounding terrain neighbors cleanly.
//
// terrainID reports whether a raw tile GID belongs to the terrain (nil treats any non-zero GID as terrain).
func (m *Tilemap) SetAutotile(layerIndex, col, row int, placed bool, set AutotileSet, terrainID func(gid uint32) bool) {
	if layerIndex < 0 || layerIndex >= len(m.Layers) {
		return
	}
	l := &m.Layers[layerIndex]
	lw, lh := m.layerSize(l)
	if col < 0 || row < 0 || col >= lw || row >= lh {
		return
	}
	if terrainID == nil {
		terrainID = func(gid uint32) bool { return gid != 0 }
	}

	// Snapshot the 5x5 neighborhood around (col, row) so the target and its 8 neighbors all evaluate against consistent post-edit membership.
	var grid [5][5]bool
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			nc, nr := col+dx, row+dy
			if dx == 0 && dy == 0 {
				grid[dy+2][dx+2] = placed
				continue
			}
			if nc >= 0 && nr >= 0 && nc < lw && nr < lh {
				grid[dy+2][dx+2] = terrainID(m.TileAt(layerIndex, nc, nr))
			}
		}
	}

	match := func(c, r int) bool {
		dx, dy := c-col, r-row
		if dx >= -2 && dx <= 2 && dy >= -2 && dy <= 2 {
			return grid[dy+2][dx+2]
		}
		if c < 0 || r < 0 || c >= lw || r >= lh {
			return false
		}
		return terrainID(m.TileAt(layerIndex, c, r))
	}

	if !placed {
		m.SetTile(layerIndex, col, row, 0)
	} else {
		mask := set.computeMask(match, col, row)
		m.SetTile(layerIndex, col, row, set.Tile(mask))
	}

	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nc, nr := col+dx, row+dy
			if nc < 0 || nr < 0 || nc >= lw || nr >= lh {
				continue
			}
			if !grid[dy+2][dx+2] {
				continue
			}
			mask := set.computeMask(match, nc, nr)
			m.SetTile(layerIndex, nc, nr, set.Tile(mask))
		}
	}
}
