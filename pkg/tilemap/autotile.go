package tilemap

import "fmt"

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

// ComputeMask computes the neighbor bitmask at (col, row) using s.Mode (ComputeMask4 for Autotile4, ComputeMask8 for Autotile8).
func (s AutotileSet) ComputeMask(match func(col, row int) bool, col, row int) uint8 {
	if s.Mode == Autotile8 {
		return ComputeMask8(match, col, row)
	}
	return ComputeMask4(match, col, row)
}

func (s AutotileSet) computeMask(match func(col, row int) bool, col, row int) uint8 {
	return s.ComputeMask(match, col, row)
}

// ValidTerrainMask reports whether mask is a valid neighbor bitmask for mode:
// for Autotile4 only the four cardinal bits (N, E, S, W) may be set;
// for Autotile8 each diagonal corner bit may only be set when both adjacent cardinal bits are also set.
func ValidTerrainMask(mode AutotileMode, mask uint8) bool {
	if mode == Autotile4 {
		return mask&^(MaskNorth|MaskEast|MaskSouth|MaskWest) == 0
	}
	for _, c := range []struct{ corner, edges uint8 }{
		{MaskNorthEast, MaskNorth | MaskEast},
		{MaskSouthEast, MaskSouth | MaskEast},
		{MaskSouthWest, MaskSouth | MaskWest},
		{MaskNorthWest, MaskNorth | MaskWest},
	} {
		if mask&c.corner != 0 && mask&c.edges != c.edges {
			return false
		}
	}
	return true
}

// ToggleTerrainMask toggles bit in mask while keeping the result valid for mode.
// In Autotile4 diagonal bits are ignored and stripped.
// In Autotile8 enabling a corner bit also enables its two adjacent cardinal edges,
// and disabling an edge bit clears any corner bits that depend on it.
func ToggleTerrainMask(mode AutotileMode, mask, bit uint8) uint8 {
	const cardinal = MaskNorth | MaskEast | MaskSouth | MaskWest
	if mode == Autotile4 {
		mask &= cardinal
		if bit&cardinal == 0 {
			return mask
		}
		return mask ^ (bit & cardinal)
	}
	if bit == 0 {
		return mask
	}
	if mask&bit != 0 {
		mask &^= bit
		// Clear any corner bit whose adjacent edges are no longer both set.
		for _, c := range []struct{ corner, edges uint8 }{
			{MaskNorthEast, MaskNorth | MaskEast},
			{MaskSouthEast, MaskSouth | MaskEast},
			{MaskSouthWest, MaskSouth | MaskWest},
			{MaskNorthWest, MaskNorth | MaskWest},
		} {
			if mask&c.corner != 0 && mask&c.edges != c.edges {
				mask &^= c.corner
			}
		}
		return mask
	}
	mask |= bit
	if edges := cornerEdgeMask(bit); edges != 0 {
		mask |= edges
	}
	return mask
}

// terrainNeighborOrder is the canonical order in which neighbor directions are written to YAML.
var terrainNeighborOrder = []struct {
	bit  uint8
	name string
}{
	{MaskNorth, "north"},
	{MaskEast, "east"},
	{MaskSouth, "south"},
	{MaskWest, "west"},
	{MaskNorthEast, "northeast"},
	{MaskSouthEast, "southeast"},
	{MaskSouthWest, "southwest"},
	{MaskNorthWest, "northwest"},
}

// TerrainMaskNeighbors returns the canonical list of neighbor direction names for mask,
// or nil when mask is 0.
func TerrainMaskNeighbors(mask uint8) []string {
	if mask == 0 {
		return nil
	}
	var out []string
	for _, entry := range terrainNeighborOrder {
		if mask&entry.bit != 0 {
			out = append(out, entry.name)
		}
	}
	return out
}

// TerrainNeighborsMask converts a list of neighbor direction names to a bitmask,
// returning an error on unknown or duplicate directions.
func TerrainNeighborsMask(neighbors []string) (uint8, error) {
	var mask uint8
	for _, n := range neighbors {
		bit, ok := terrainNeighborBit(n)
		if !ok {
			return 0, fmt.Errorf("tilemap: unknown terrain neighbor %q", n)
		}
		if mask&bit != 0 {
			return 0, fmt.Errorf("tilemap: duplicate terrain neighbor %q", n)
		}
		mask |= bit
	}
	return mask, nil
}

// ApplyAutotile updates every terrain cell in layerIndex using set's bitmask-to-GID mapping.
//
// terrainID reports whether a raw tile GID belongs to the terrain (nil treats any non-zero GID as terrain).
func (m *Tilemap) ApplyAutotile(layerIndex int, set AutotileSet, terrainID func(gid uint32) bool) {
	if layerIndex < 0 || layerIndex >= len(m.Layers) {
		return
	}
	l := &m.Layers[layerIndex]
	if terrainID == nil {
		terrainID = func(gid uint32) bool { return gid != 0 }
	}
	if l.IsChunked() {
		if l.chunks == nil || len(l.chunks.chunks) == 0 {
			return
		}
		member := make(map[cellKey]bool)
		var cells []cellKey
		for _, coord := range l.ChunkCoords() {
			ch := l.Chunk(coord)
			if ch == nil || ch.nonEmpty == 0 {
				continue
			}
			baseCol := coord.X * ch.Size
			baseRow := coord.Y * ch.Size
			for lr := range ch.Size {
				rowOff := lr * ch.Size
				for lc := range ch.Size {
					gid := ch.Tiles[rowOff+lc]
					if terrainID(gid) {
						k := cellKey{col: baseCol + lc, row: baseRow + lr}
						member[k] = true
						cells = append(cells, k)
					}
				}
			}
		}
		match := func(c, r int) bool {
			return member[cellKey{col: c, row: r}]
		}
		for _, k := range cells {
			mask := set.computeMask(match, k.col, k.row)
			m.SetTile(layerIndex, k.col, k.row, set.Tile(mask))
		}
		return
	}
	lw, lh := m.layerSize(l)
	if lw <= 0 || lh <= 0 || len(l.Data) == 0 {
		return
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
	chunked := l.IsChunked()
	lw, lh := m.layerSize(l)
	if !chunked && (col < 0 || row < 0 || col >= lw || row >= lh) {
		return
	}
	if terrainID == nil {
		terrainID = func(gid uint32) bool { return gid != 0 }
	}
	inBounds := func(c, r int) bool {
		return chunked || (c >= 0 && r >= 0 && c < lw && r < lh)
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
			if inBounds(nc, nr) {
				grid[dy+2][dx+2] = terrainID(m.TileAt(layerIndex, nc, nr))
			}
		}
	}

	match := func(c, r int) bool {
		dx, dy := c-col, r-row
		if dx >= -2 && dx <= 2 && dy >= -2 && dy <= 2 {
			return grid[dy+2][dx+2]
		}
		if !inBounds(c, r) {
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
			if !inBounds(nc, nr) {
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
