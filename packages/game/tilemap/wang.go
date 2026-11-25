package tilemap

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// TerrainRand supplies deterministic uniform [0, 1) floats for weighted tie-breaking
// in SetTerrain and SetTerrainRect. *random.Stream satisfies TerrainRand.
type TerrainRand interface {
	Float64() float64
}

// WangSetType selects which tile edges and corners a Tiled WangSet matches.
type WangSetType uint8

const (
	// WangCorner matches the four tile corners (TopRight, BottomRight, BottomLeft, TopLeft).
	WangCorner WangSetType = iota
	// WangEdge matches the four tile edges (Top, Right, Bottom, Left).
	WangEdge
	// WangMixed matches all four tile edges and all four tile corners.
	WangMixed
)

// String returns the Tiled JSON name ("corner", "edge", or "mixed") for t.
func (t WangSetType) String() string {
	switch t {
	case WangCorner:
		return "corner"
	case WangEdge:
		return "edge"
	case WangMixed:
		return "mixed"
	default:
		return "unknown"
	}
}

// Clockwise indices into WangID, matching Tiled's wangid JSON array order.
const (
	WangTop         = 0
	WangTopRight    = 1
	WangRight       = 2
	WangBottomRight = 3
	WangBottom      = 4
	WangBottomLeft  = 5
	WangLeft        = 6
	WangTopLeft     = 7
)

// WangID holds the 8 terrain color indices around a tile in Tiled's clockwise order:
// [Top, TopRight, Right, BottomRight, Bottom, BottomLeft, Left, TopLeft].
//
// Terrain indices are 1-based (1..len(WangSet.Colors)); 0 means unset or empty.
type WangID [8]uint8

// WangColor describes one terrain color in a Tiled WangSet.
// Its 1-based terrain index in WangID values is its slice index + 1.
type WangColor struct {
	Name        string
	Color       core.Color
	Tile        int // 0-based local representative tile ID, or -1 if unset
	Probability float64
	Class       string
	Properties  Properties

	zeroProbability bool
}

func sanitizeWangWeight(p float64, explicitZero bool) float64 {
	if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 {
		return 0
	}
	if p > 0 {
		return p
	}
	if explicitZero {
		return 0
	}
	return 1
}

func (c WangColor) weight() float64 {
	return sanitizeWangWeight(c.Probability, c.zeroProbability)
}

// WangTile maps a 0-based local tile ID in its Tileset to its 8-slot WangID and selection probability.
type WangTile struct {
	TileID      uint32
	WangID      WangID
	Probability float64

	zeroProbability bool
}

func (t WangTile) weight() float64 {
	return sanitizeWangWeight(t.Probability, t.zeroProbability)
}

// WangSet is a Tiled Wang set (terrain set) defining corner, edge, or mixed multi-terrain autotiling rules.
type WangSet struct {
	Name       string
	Type       WangSetType
	Tile       int // 0-based local representative tile ID, or -1 if unset
	Class      string
	FirstGID   uint32 // FirstGID of the owning Tileset (defaults to 1 when 0)
	Colors     []WangColor
	WangTiles  []WangTile
	Properties Properties

	index *wangIndex
}

// ColorIndex returns the 1-based terrain index of the first color named name in ws.Colors.
func (ws *WangSet) ColorIndex(name string) (uint8, bool) {
	if ws == nil {
		return 0, false
	}
	for i := range ws.Colors {
		if ws.Colors[i].Name == name {
			return uint8(i + 1), true
		}
	}
	return 0, false
}

// WangIDForTile returns the WangID assigned to the 0-based local tile ID in ws, if any.
func (ws *WangSet) WangIDForTile(localTileID uint32) (WangID, bool) {
	if ws == nil || len(ws.WangTiles) == 0 {
		return WangID{}, false
	}
	idx := ws.ensureIndex()
	wid, ok := idx.tileToWang[localTileID]
	return wid, ok
}

// WangSet returns the first WangSet named name across all tilesets of m, or nil if none matches.
func (m *Tilemap) WangSet(name string) *WangSet {
	if m == nil {
		return nil
	}
	for _, ts := range m.Tilesets {
		if ws := ts.WangSet(name); ws != nil {
			return ws
		}
	}
	return nil
}

// wangDirs maps each clockwise WangID index 0..7 to its grid neighbor offset (dx, dy).
// In Tiled, diamond isometric grids rotate the square grid 45° clockwise, so the grid-space
// neighbor offsets for all 8 edges and corners are identical for orthogonal and isometric maps.
var wangDirs = [8][2]int{
	{0, -1},  // 0: Top (iso: Top-Right edge)
	{1, -1},  // 1: TopRight (iso: Right corner)
	{1, 0},   // 2: Right (iso: Bottom-Right edge)
	{1, 1},   // 3: BottomRight (iso: Bottom corner)
	{0, 1},   // 4: Bottom (iso: Bottom-Left edge)
	{-1, 1},  // 5: BottomLeft (iso: Left corner)
	{-1, 0},  // 6: Left (iso: Top-Left edge)
	{-1, -1}, // 7: TopLeft (iso: Top corner)
}

// wangWaveDirs visits cardinal neighbors before diagonal neighbors so diagonal cells in a BFS ring
// observe both adjacent cardinal cells already settled.
var wangWaveDirs8 = [8][2]int{
	{0, -1},
	{1, 0},
	{0, 1},
	{-1, 0},
	{1, -1},
	{1, 1},
	{-1, 1},
	{-1, -1},
}

var wangWaveDirs4 = [4][2]int{
	{0, -1},
	{1, 0},
	{0, 1},
	{-1, 0},
}

type wangIndex struct {
	typ          WangSetType
	tileCount    int
	colorCount   int
	activeSlots  [8]int
	activeCount  int
	tileToWang   map[uint32]WangID
	sortedTiles  []int
	dist         []int16
	stride       int
	maxColor     uint8
	hasZeroColor bool
}

func (ws *WangSet) ensureIndex() *wangIndex {
	if ws.index != nil &&
		ws.index.typ == ws.Type &&
		ws.index.tileCount == len(ws.WangTiles) &&
		ws.index.colorCount == len(ws.Colors) {
		return ws.index
	}

	idx := &wangIndex{
		typ:         ws.Type,
		tileCount:   len(ws.WangTiles),
		colorCount:  len(ws.Colors),
		tileToWang:  make(map[uint32]WangID, len(ws.WangTiles)),
		sortedTiles: make([]int, len(ws.WangTiles)),
	}
	switch ws.Type {
	case WangCorner:
		idx.activeSlots = [8]int{WangTopRight, WangBottomRight, WangBottomLeft, WangTopLeft}
		idx.activeCount = 4
	case WangEdge:
		idx.activeSlots = [8]int{WangTop, WangRight, WangBottom, WangLeft}
		idx.activeCount = 4
	default:
		idx.activeSlots = [8]int{0, 1, 2, 3, 4, 5, 6, 7}
		idx.activeCount = 8
	}

	maxC := uint8(min(len(ws.Colors), 255))
	for i, wt := range ws.WangTiles {
		idx.sortedTiles[i] = i
		if _, exists := idx.tileToWang[wt.TileID]; !exists {
			idx.tileToWang[wt.TileID] = wt.WangID
		}
		for s := range idx.activeCount {
			c := wt.WangID[idx.activeSlots[s]]
			if c == 0 {
				idx.hasZeroColor = true
			} else if c > maxC {
				maxC = c
			}
		}
	}
	slices.SortFunc(idx.sortedTiles, func(a, b int) int {
		if c := cmp.Compare(ws.WangTiles[a].TileID, ws.WangTiles[b].TileID); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})

	idx.maxColor = maxC
	stride := int(maxC) + 1
	idx.stride = stride
	dist := make([]int16, stride*stride)
	for i := range dist {
		dist[i] = -1
	}
	for c := range stride {
		dist[c*stride+c] = 0
	}
	for _, wt := range ws.WangTiles {
		for a := range idx.activeCount {
			ca := int(wt.WangID[idx.activeSlots[a]])
			for b := a + 1; b < idx.activeCount; b++ {
				cb := int(wt.WangID[idx.activeSlots[b]])
				if ca != cb {
					dist[ca*stride+cb] = 1
					dist[cb*stride+ca] = 1
				}
			}
		}
	}
	for k := range stride {
		kRow := k * stride
		for i := range stride {
			dik := dist[i*stride+k]
			if dik < 0 {
				continue
			}
			iRow := i * stride
			for j := range stride {
				dkj := dist[kRow+j]
				if dkj < 0 {
					continue
				}
				nd := dik + dkj
				if cur := dist[iRow+j]; cur < 0 || nd < cur {
					dist[iRow+j] = nd
				}
			}
		}
	}
	idx.dist = dist

	ws.index = idx
	return idx
}

func (idx *wangIndex) colorDistance(a, b uint8) int {
	if a == b {
		return 0
	}
	if int(a) >= idx.stride || int(b) >= idx.stride {
		return -1
	}
	return int(idx.dist[int(a)*idx.stride+int(b)])
}

func (idx *wangIndex) allActiveZero(wid WangID) bool {
	for s := range idx.activeCount {
		if wid[idx.activeSlots[s]] != 0 {
			return false
		}
	}
	return true
}

// transformWangID transforms a tile's WangID under Tiled's GID flip flags
// (diagonal transpose first, then horizontal mirror, then vertical mirror).
func transformWangID(wid WangID, flags uint32) WangID {
	if flags == 0 {
		return wid
	}
	if flags&FlipDiagonal != 0 {
		wid[WangTop], wid[WangLeft] = wid[WangLeft], wid[WangTop]
		wid[WangTopRight], wid[WangBottomLeft] = wid[WangBottomLeft], wid[WangTopRight]
		wid[WangRight], wid[WangBottom] = wid[WangBottom], wid[WangRight]
	}
	if flags&FlipHorizontal != 0 {
		wid[WangTopRight], wid[WangTopLeft] = wid[WangTopLeft], wid[WangTopRight]
		wid[WangRight], wid[WangLeft] = wid[WangLeft], wid[WangRight]
		wid[WangBottomRight], wid[WangBottomLeft] = wid[WangBottomLeft], wid[WangBottomRight]
	}
	if flags&FlipVertical != 0 {
		wid[WangTop], wid[WangBottom] = wid[WangBottom], wid[WangTop]
		wid[WangTopRight], wid[WangBottomRight] = wid[WangBottomRight], wid[WangTopRight]
		wid[WangTopLeft], wid[WangBottomLeft] = wid[WangBottomLeft], wid[WangTopLeft]
	}
	return wid
}

func (m *Tilemap) resolveWangFirstGID(ws *WangSet) uint32 {
	if ws.FirstGID != 0 {
		return ws.FirstGID
	}
	for _, ts := range m.Tilesets {
		if ts == nil {
			continue
		}
		for i := range ts.WangSets {
			if &ts.WangSets[i] == ws {
				if ts.FirstGID != 0 {
					return ts.FirstGID
				}
				return 1
			}
		}
	}
	if len(m.Tilesets) > 0 && m.Tilesets[0] != nil && m.Tilesets[0].FirstGID != 0 {
		return m.Tilesets[0].FirstGID
	}
	return 1
}

const wangUnreachablePenalty = 10000

// findBestMatch selects the tile in ws minimizing (hardPenalty, softPenalty) against desired.
// Tied best matches are chosen by probability using rng (or deterministically by lowest TileID when rng == nil).
func (ws *WangSet) findBestMatch(idx *wangIndex, desired WangID, hardMask [8]bool, rng TerrainRand) (tileID uint32, wid WangID, exact bool, ok bool) {
	bestHard := int(^uint(0) >> 1)
	bestSoft := int(^uint(0) >> 1)
	var bestBuf [16]int
	best := bestBuf[:0]

	for _, ti := range idx.sortedTiles {
		wt := &ws.WangTiles[ti]
		hardPen := 0
		softPen := 0
		for s := range idx.activeCount {
			slot := idx.activeSlots[s]
			want := desired[slot]
			got := wt.WangID[slot]
			if got == want {
				continue
			}
			d := idx.colorDistance(want, got)
			if d < 0 {
				d = wangUnreachablePenalty
			}
			if hardMask[slot] {
				hardPen += d
			} else {
				softPen += d
			}
		}
		if hardPen < bestHard || (hardPen == bestHard && softPen < bestSoft) {
			bestHard = hardPen
			bestSoft = softPen
			best = best[:0]
			best = append(best, ti)
		} else if hardPen == bestHard && softPen == bestSoft {
			best = append(best, ti)
		}
	}
	if len(best) == 0 {
		return 0, WangID{}, false, false
	}
	exact = bestHard == 0 && bestSoft == 0
	if len(best) == 1 {
		wt := &ws.WangTiles[best[0]]
		return wt.TileID, wt.WangID, exact, true
	}

	candWeight := func(ti int) float64 {
		wt := &ws.WangTiles[ti]
		w := wt.weight()
		if w <= 0 {
			return 0
		}
		for s := range idx.activeCount {
			slot := idx.activeSlots[s]
			if !hardMask[slot] {
				c := wt.WangID[slot]
				if c != desired[slot] && c > 0 && int(c) <= len(ws.Colors) {
					w *= ws.Colors[c-1].weight()
				}
			}
		}
		if math.IsNaN(w) || math.IsInf(w, 0) || w <= 0 {
			return 0
		}
		return w
	}

	if rng == nil {
		for _, ti := range best {
			if candWeight(ti) > 0 {
				wt := &ws.WangTiles[ti]
				return wt.TileID, wt.WangID, exact, true
			}
		}
		wt := &ws.WangTiles[best[0]]
		return wt.TileID, wt.WangID, exact, true
	}

	var total float64
	for _, ti := range best {
		total += candWeight(ti)
	}
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		wt := &ws.WangTiles[best[0]]
		return wt.TileID, wt.WangID, exact, true
	}
	target := rng.Float64() * total
	var acc float64
	lastPositive := best[0]
	for _, ti := range best {
		w := candWeight(ti)
		if w <= 0 {
			continue
		}
		lastPositive = ti
		acc += w
		if target < acc {
			wt := &ws.WangTiles[ti]
			return wt.TileID, wt.WangID, exact, true
		}
	}
	wt := &ws.WangTiles[lastPositive]
	return wt.TileID, wt.WangID, exact, true
}

// SetTerrain paints terrain (1-based index into set.Colors, or 0 to erase) at (col, row) in layerIndex,
// choosing tiles matching every neighbor's shared edges and corners for corner, edge, and mixed Wang sets.
//
// Works on both orthogonal and isometric maps as well as dense and sparse chunked layers.
// When multiple tiles tie for best match, SetTerrain picks deterministically by tile probability using rng
// (or the lowest local TileID when rng is nil).
func (m *Tilemap) SetTerrain(layerIndex, col, row int, set *WangSet, terrain uint8, rng TerrainRand) {
	m.SetTerrainRect(layerIndex, col, row, 1, 1, set, terrain, rng)
}

// SetTerrainRect paints terrain (1-based index into set.Colors, or 0 to erase) across [col, col+w) x [row, row+h)
// in layerIndex, updating painted cells and all affected neighbors so multi-terrain transitions remain seamless.
//
// On dense layers, out-of-bounds portions are clipped; on sparse chunked layers, arbitrary negative and positive
// rectangles are supported. Non-positive sizes are ignored.
func (m *Tilemap) SetTerrainRect(layerIndex, col, row, w, h int, set *WangSet, terrain uint8, rng TerrainRand) {
	if m == nil || set == nil || len(set.WangTiles) == 0 || w <= 0 || h <= 0 {
		return
	}
	if layerIndex < 0 || layerIndex >= len(m.Layers) {
		return
	}
	if terrain > 0 && len(set.Colors) > 0 && int(terrain) > len(set.Colors) {
		return
	}

	l := &m.Layers[layerIndex]
	chunked := l.IsChunked()
	lw, lh := m.layerSize(l)
	x0, y0, x1, y1 := col, row, col+w, row+h
	if !chunked {
		if lw <= 0 || lh <= 0 {
			return
		}
		x0 = max(0, x0)
		y0 = max(0, y0)
		x1 = min(lw, x1)
		y1 = min(lh, y1)
		if x0 >= x1 || y0 >= y1 {
			return
		}
	}
	inBounds := func(c, r int) bool {
		return chunked || (c >= 0 && r >= 0 && c < lw && r < lh)
	}

	idx := set.ensureIndex()
	firstGID := m.resolveWangFirstGID(set)

	readCell := func(c, r int) (gid uint32, wid WangID, inSet bool) {
		if !inBounds(c, r) {
			return 0, WangID{}, false
		}
		gid = m.TileAt(layerIndex, c, r)
		rawID := gid & TileIDMask
		if rawID == 0 || rawID < firstGID {
			return gid, WangID{}, false
		}
		localID := rawID - firstGID
		w0, ok := idx.tileToWang[localID]
		if !ok {
			return gid, WangID{}, false
		}
		if flags := gid & FlipMask; flags != 0 {
			w0 = transformWangID(w0, flags)
		}
		return gid, w0, true
	}

	// settled maps cells decided during this brush stroke to their resulting WangID.
	settled := make(map[cellKey]WangID, (x1-x0)*(y1-y0)+16)
	var allTerrain WangID
	var allHard [8]bool
	for s := range idx.activeCount {
		slot := idx.activeSlots[s]
		allTerrain[slot] = terrain
		allHard[slot] = true
	}

	// Phase 1: paint the target rectangle in deterministic row-major order.
	painted := make([]cellKey, 0, (x1-x0)*(y1-y0))
	for r := y0; r < y1; r++ {
		for c := x0; c < x1; c++ {
			k := cellKey{col: c, row: r}
			if terrain == 0 {
				var placed WangID
				if idx.hasZeroColor {
					if tileID, wid, exact, ok := set.findBestMatch(idx, allTerrain, allHard, rng); ok && exact {
						m.SetTile(layerIndex, c, r, firstGID+tileID)
						placed = wid
					} else {
						m.SetTile(layerIndex, c, r, 0)
					}
				} else {
					m.SetTile(layerIndex, c, r, 0)
				}
				settled[k] = placed
				painted = append(painted, k)
				continue
			}
			tileID, wid, _, ok := set.findBestMatch(idx, allTerrain, allHard, rng)
			if !ok {
				continue
			}
			m.SetTile(layerIndex, c, r, firstGID+tileID)
			settled[k] = wid
			painted = append(painted, k)
		}
	}

	// If erasing on a tileset with no 0/empty transitions, neighbors cannot transition to 0.
	if terrain == 0 && !idx.hasZeroColor {
		return
	}

	// Phase 2: BFS wave propagation outward from painted cells so shared edges/corners
	// match every neighbor (including multi-hop transitions like Water -> Dirt -> Grass).
	waveDirs := wangWaveDirs8[:]
	if set.Type == WangEdge {
		waveDirs = wangWaveDirs4[:]
	}

	queued := make(map[cellKey]bool, len(painted)*4)
	for _, k := range painted {
		queued[k] = true
	}

	var curWave, nextWave []cellKey
	enqueueNeighbors := func(src []cellKey, dst []cellKey) []cellKey {
		// Enqueue cardinal neighbors before diagonal neighbors across the wave for symmetric settlement.
		limit := len(waveDirs)
		if limit == 8 {
			for _, k := range src {
				for _, d := range waveDirs[:4] {
					nk := cellKey{col: k.col + d[0], row: k.row + d[1]}
					if !queued[nk] && inBounds(nk.col, nk.row) {
						queued[nk] = true
						dst = append(dst, nk)
					}
				}
			}
			for _, k := range src {
				for _, d := range waveDirs[4:] {
					nk := cellKey{col: k.col + d[0], row: k.row + d[1]}
					if !queued[nk] && inBounds(nk.col, nk.row) {
						queued[nk] = true
						dst = append(dst, nk)
					}
				}
			}
			return dst
		}
		for _, k := range src {
			for _, d := range waveDirs {
				nk := cellKey{col: k.col + d[0], row: k.row + d[1]}
				if !queued[nk] && inBounds(nk.col, nk.row) {
					queued[nk] = true
					dst = append(dst, nk)
				}
			}
		}
		return dst
	}

	curWave = enqueueNeighbors(painted, curWave)
	maxWaves := max(int(idx.maxColor)+1, 4)

	for wave := 0; wave < maxWaves && len(curWave) > 0; wave++ {
		nextWave = nextWave[:0]
		var changed []cellKey

		for _, pos := range curWave {
			gid, oldWID, inSet := readCell(pos.col, pos.row)
			// Never overwrite non-empty tiles outside this WangSet during neighbor correction.
			if gid != 0 && !inSet {
				continue
			}
			// Never materialize tiles in empty cells unless the WangSet explicitly has 0/empty transitions.
			if gid == 0 && !idx.hasZeroColor {
				continue
			}

			desired := oldWID
			var hardMask [8]bool
			anyHard := false

			for s := range idx.activeCount {
				slot := idx.activeSlots[s]
				if slot&1 == 0 {
					// Edge slot: shares edge with 1 cardinal neighbor.
					d := wangDirs[slot]
					if sw, ok := settled[cellKey{col: pos.col + d[0], row: pos.row + d[1]}]; ok {
						desired[slot] = sw[(slot+4)&7]
						hardMask[slot] = true
						anyHard = true
					}
				} else {
					// Corner slot: shared by up to 3 neighbors (2 cardinal + 1 diagonal).
					d1 := wangDirs[(slot+7)&7]
					if sw, ok := settled[cellKey{col: pos.col + d1[0], row: pos.row + d1[1]}]; ok {
						desired[slot] = sw[(slot+2)&7]
						hardMask[slot] = true
						anyHard = true
					}
					d2 := wangDirs[slot]
					if sw, ok := settled[cellKey{col: pos.col + d2[0], row: pos.row + d2[1]}]; ok {
						desired[slot] = sw[(slot+4)&7]
						hardMask[slot] = true
						anyHard = true
					}
					d3 := wangDirs[(slot+1)&7]
					if sw, ok := settled[cellKey{col: pos.col + d3[0], row: pos.row + d3[1]}]; ok {
						desired[slot] = sw[(slot+6)&7]
						hardMask[slot] = true
						anyHard = true
					}
				}
			}

			if !anyHard {
				continue
			}

			// If an existing WangSet tile already satisfies every hard constraint, keep it intact.
			if inSet {
				alreadyMatches := true
				for s := range idx.activeCount {
					slot := idx.activeSlots[s]
					if hardMask[slot] && oldWID[slot] != desired[slot] {
						alreadyMatches = false
						break
					}
				}
				if alreadyMatches {
					settled[pos] = oldWID
					continue
				}
			}

			// If all active slots became 0 (empty), clear unless an explicit all-0 tile exists.
			if idx.allActiveZero(desired) {
				if tileID, wid, exact, ok := set.findBestMatch(idx, desired, hardMask, rng); ok && exact {
					m.SetTile(layerIndex, pos.col, pos.row, firstGID+tileID)
					settled[pos] = wid
					if !inSet || wid != oldWID {
						changed = append(changed, pos)
					}
				} else if gid != 0 {
					m.SetTile(layerIndex, pos.col, pos.row, 0)
					settled[pos] = WangID{}
					changed = append(changed, pos)
				}
				continue
			}

			tileID, wid, exact, ok := set.findBestMatch(idx, desired, hardMask, rng)
			if !ok {
				continue
			}
			// For previously empty cells, only place a tile if it matches the empty-transition constraints.
			if gid == 0 && !exact {
				continue
			}
			m.SetTile(layerIndex, pos.col, pos.row, firstGID+tileID)
			settled[pos] = wid
			if !inSet || wid != oldWID {
				changed = append(changed, pos)
			}
		}

		if len(changed) == 0 {
			break
		}
		curWave = enqueueNeighbors(changed, nextWave)
	}
}
