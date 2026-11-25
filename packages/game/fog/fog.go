// Package fog tracks fog of war for strategy games: which cells of an orthogonal, isometric, staggered or hexagonal tile grid each team has explored and which it can see right now.
//
// A Fog holds a grid of opaque cells (walls, cliffs, forests) and a set of vision sources (units and buildings), each with a team, a cell and a radius in cells. Update recomputes every changed team's visible cells by symmetric shadowcasting from its sources, limited to each source's radius, and adds them to the cells the team has explored, which stay explored when the sources move away. A cell is therefore Unseen, Explored or Visible to each team.
//
// Draw paints the fog over a tilemap in world coordinates under a tilemap.Camera aligned to the map's orientation (rectangles for orthogonal grids, diamonds for isometric and staggered grids, hexagons for hexagonal grids), and a Fog marshals to and from versioned JSON text, so a save.Store slot keeps a team's explored map, the sources and the opaque cells through a save and load.
package fog

import (
	"fmt"
	"math"
	"math/bits"
	"slices"
	"sync/atomic"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// MaxCells is the largest number of cells (width times height) a Fog holds.
const MaxCells = 1 << 24

// Team identifies a side whose sources share vision and exploration.
type Team int

// State is what a team knows about one cell.
type State uint8

const (
	// Unseen marks a cell the team has never seen.
	Unseen State = iota
	// Explored marks a cell the team has seen before but cannot see now.
	Explored
	// Visible marks a cell one of the team's sources can see now.
	Visible
)

// String returns "unseen", "explored" or "visible".
func (s State) String() string {
	switch s {
	case Unseen:
		return "unseen"
	case Explored:
		return "explored"
	case Visible:
		return "visible"
	}
	return fmt.Sprintf("fog.State(%d)", uint8(s))
}

// SourceID identifies a vision source added with Fog.AddSource; IDs start at 1 and are never reused by one Fog.
type SourceID uint32

// Source is something that gives its team vision: a unit, a building or a ward.
type Source struct {
	// Team is the side the source reveals cells for.
	Team Team
	// Col and Row are the cell the source stands on; a source outside the grid sees nothing.
	Col, Row int
	// Radius is how far the source sees, in cells: a cell dx columns and dy rows away is in range when dx*dx+dy*dy <= Radius*Radius+Radius, which rounds the disc. 0 sees only the source's own cell, and a negative radius behaves as 0.
	Radius int
}

// Fog is the fog of war of one map for every team: opaque cells, vision sources, and each team's visible and explored cells.
//
// A Fog is not safe for concurrent use. Change sources and opaque cells during a fixed step, then call Update once before reading or drawing the fog.
type Fog struct {
	// TileWidth and TileHeight are a cell's size in world points, used by Draw and CellAt.
	TileWidth, TileHeight float32
	// Orientation selects how cells are arranged for visibility, CellAt and Draw.
	// The zero value (tilemap.OrientationOrthogonal) preserves orthogonal behavior.
	Orientation tilemap.Orientation
	// StaggerAxis selects which axis is staggered for staggered isometric and hexagonal grids.
	StaggerAxis tilemap.StaggerAxis
	// StaggerIndex selects whether odd or even rows or columns are shifted for staggered and hexagonal grids.
	StaggerIndex tilemap.StaggerIndex
	// HexSideLength is the length of a hex's flat side along its staggered axis; 0 defaults to half TileWidth (StaggerX) or half TileHeight (StaggerY), matching tilemap.Tilemap.
	HexSideLength float32

	width, height int
	opaque        []uint64
	opaqueCount   int
	sources       map[SourceID]Source
	nextID        SourceID
	teams         map[Team]*layer

	scratch []uint64
	stack   []scanRow
}

// layer is one team's knowledge of the grid.
type layer struct {
	visible  []uint64
	explored []uint64
	dirty    bool
	revision uint64
}

// New returns a Fog over a grid of width by height cells, each tileWidth by tileHeight world points, with no opaque cells, sources or teams.
//
// New panics if width or height is negative, exactly one of them is zero, or the grid has more than MaxCells cells.
func New(width, height int, tileWidth, tileHeight float32) *Fog {
	if !validSize(width, height) {
		panic(fmt.Sprintf("fog: invalid grid size %dx%d", width, height))
	}
	words := wordsFor(width * height)
	return &Fog{
		TileWidth:  tileWidth,
		TileHeight: tileHeight,
		width:      width,
		height:     height,
		opaque:     make([]uint64, words),
		sources:    make(map[SourceID]Source),
		teams:      make(map[Team]*layer),
	}
}

// NewFromTilemap returns a Fog matching m's grid size, tile size and projection (Orientation, StaggerAxis, StaggerIndex, HexSideLength) in which every cell whose tilemap flags include any bit of mask is opaque; a zero mask makes no cell opaque.
func NewFromTilemap(m *tilemap.Tilemap, mask tilemap.TileFlags) *Fog {
	f := New(m.Width, m.Height, m.TileWidth, m.TileHeight)
	f.Orientation = m.Orientation
	f.StaggerAxis = m.StaggerAxis
	f.StaggerIndex = m.StaggerIndex
	f.HexSideLength = m.HexSideLength
	if mask == 0 {
		return f
	}
	for row := range f.height {
		for col := range f.width {
			if m.FlagsAt(col, row)&mask != 0 {
				f.SetOpaque(col, row, true)
			}
		}
	}
	return f
}

func (f *Fog) projectionMap() tilemap.Tilemap {
	return tilemap.Tilemap{
		Width:         f.width,
		Height:        f.height,
		TileWidth:     f.TileWidth,
		TileHeight:    f.TileHeight,
		Orientation:   f.Orientation,
		StaggerAxis:   f.StaggerAxis,
		StaggerIndex:  f.StaggerIndex,
		HexSideLength: f.HexSideLength,
	}
}

func wordsFor(cells int) int { return (cells + 63) / 64 }

// validSize reports whether New accepts a grid of width by height cells. Each size is bounded on its own, not only their product, so a grid such as 0 by 10^11 cannot drive a row loop or a slice of rows that long.
func validSize(width, height int) bool {
	if width == 0 || height == 0 {
		return width == 0 && height == 0
	}
	return width > 0 && height > 0 && height <= MaxCells/width
}

// revisions hands out every layer revision, so no Fog in the program repeats a revision value, including a Fog loaded in place of another.
var revisions atomic.Uint64

func nextRevision() uint64 { return revisions.Add(1) }

// Width returns the grid's width in cells.
func (f *Fog) Width() int { return f.width }

// Height returns the grid's height in cells.
func (f *Fog) Height() int { return f.height }

// InBounds reports whether (col, row) is a cell of the grid.
func (f *Fog) InBounds(col, row int) bool {
	return col >= 0 && row >= 0 && col < f.width && row < f.height
}

func (f *Fog) index(col, row int) int { return row*f.width + col }

func bit(set []uint64, i int) bool { return set[i>>6]&(1<<(uint(i)&63)) != 0 }

func setBit(set []uint64, i int) { set[i>>6] |= 1 << (uint(i) & 63) }

func clearBit(set []uint64, i int) { set[i>>6] &^= 1 << (uint(i) & 63) }

// Opaque reports whether (col, row) blocks sight; cells outside the grid report true.
func (f *Fog) Opaque(col, row int) bool {
	if !f.InBounds(col, row) {
		return true
	}
	return bit(f.opaque, f.index(col, row))
}

// SetOpaque makes (col, row) block sight or not, such as when a door opens or a forest burns, and marks every team for the next Update. Cells outside the grid are ignored.
//
// An opaque cell is still seen itself when a source has sight of it, so walls show at the edge of vision.
func (f *Fog) SetOpaque(col, row int, opaque bool) {
	if !f.InBounds(col, row) || f.Opaque(col, row) == opaque {
		return
	}
	i := f.index(col, row)
	if opaque {
		setBit(f.opaque, i)
		f.opaqueCount++
	} else {
		clearBit(f.opaque, i)
		f.opaqueCount--
	}
	for _, l := range f.teams {
		l.dirty = true
	}
}

func (f *Fog) layer(team Team) *layer {
	l := f.teams[team]
	if l == nil {
		words := wordsFor(f.width * f.height)
		l = &layer{visible: make([]uint64, words), explored: make([]uint64, words)}
		f.teams[team] = l
	}
	return l
}

// AddSource adds a vision source and returns its ID; its team sees from it at the next Update.
func (f *Fog) AddSource(s Source) SourceID {
	f.nextID++
	id := f.nextID
	f.sources[id] = s
	f.layer(s.Team).dirty = true
	return id
}

// Source returns the source with id and whether it exists.
func (f *Fog) Source(id SourceID) (Source, bool) {
	s, ok := f.sources[id]
	return s, ok
}

// SetSource replaces the source with id, such as when a unit moves, changes sides or gains sight range, and reports whether it exists. A change marks the old and new teams for the next Update; setting the same values does nothing.
func (f *Fog) SetSource(id SourceID, s Source) bool {
	old, ok := f.sources[id]
	if !ok {
		return false
	}
	if old == s {
		return true
	}
	f.sources[id] = s
	f.layer(old.Team).dirty = true
	f.layer(s.Team).dirty = true
	return true
}

// Move moves the source with id to (col, row) and reports whether it exists.
func (f *Fog) Move(id SourceID, col, row int) bool {
	s, ok := f.sources[id]
	if !ok {
		return false
	}
	s.Col, s.Row = col, row
	return f.SetSource(id, s)
}

// MoveTo moves the source with id to the cell holding world point p (see CellAt) and reports whether it exists.
func (f *Fog) MoveTo(id SourceID, p core.Point) bool {
	col, row := f.cellAt(p)
	return f.Move(id, col, row)
}

// RemoveSource removes the source with id, such as when a unit dies, and reports whether it existed. Its team stops seeing from it at the next Update, but keeps what it explored.
func (f *Fog) RemoveSource(id SourceID) bool {
	s, ok := f.sources[id]
	if !ok {
		return false
	}
	delete(f.sources, id)
	f.layer(s.Team).dirty = true
	return true
}

// Sources returns the IDs of every source in increasing order.
func (f *Fog) Sources() []SourceID {
	ids := make([]SourceID, 0, len(f.sources))
	for id := range f.sources {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Teams returns every team that has had a source or explored cells, in increasing order.
func (f *Fog) Teams() []Team {
	teams := make([]Team, 0, len(f.teams))
	for t := range f.teams {
		teams = append(teams, t)
	}
	slices.Sort(teams)
	return teams
}

// Update recomputes the visible cells of every team whose sources or opaque cells changed since the last Update, adds them to the team's explored cells, and reports whether any team's visible or explored cells changed.
func (f *Fog) Update() bool {
	changed := false
	// Teams recompute independently, so map order cannot change the result.
	for team, l := range f.teams {
		if !l.dirty {
			continue
		}
		l.dirty = false
		if f.recompute(team, l) {
			l.revision = nextRevision()
			changed = true
		}
	}
	return changed
}

// recompute rebuilds l's visible cells from team's sources and reports whether its visible or explored cells changed.
func (f *Fog) recompute(team Team, l *layer) bool {
	if cap(f.scratch) < len(l.visible) {
		f.scratch = make([]uint64, len(l.visible))
	}
	next := f.scratch[:len(l.visible)]
	clear(next)
	for _, s := range f.sources {
		if s.Team == team {
			f.see(s, next)
		}
	}
	changed := false
	for i, w := range next {
		if w != l.visible[i] {
			changed = true
		}
		if e := l.explored[i] | w; e != l.explored[i] {
			l.explored[i] = e
			changed = true
		}
	}
	l.visible, f.scratch = next, l.visible
	return changed
}

// see sets in vis every cell s can see.
func (f *Fog) see(s Source, vis []uint64) {
	if !f.InBounds(s.Col, s.Row) {
		return
	}
	// A radius past the grid's diagonal sees no more cells, so clamping bounds the work for huge radii.
	r := min(max(s.Radius, 0), f.width+f.height)
	switch f.Orientation {
	case tilemap.OrientationHexagonal:
		if f.opaqueCount == 0 {
			f.hexDisc(s.Col, s.Row, r, vis)
			return
		}
		f.hexShadowcast(s.Col, s.Row, r, vis)
	case tilemap.OrientationStaggered:
		if f.opaqueCount == 0 {
			f.staggeredDisc(s.Col, s.Row, r, vis)
			return
		}
		f.staggeredShadowcast(s.Col, s.Row, r, vis)
	default:
		if f.opaqueCount == 0 {
			f.disc(s.Col, s.Row, r, vis)
			return
		}
		f.shadowcast(s.Col, s.Row, r, vis)
	}
}

// inRange reports whether an offset of (dx, dy) cells lies in a disc of radius r.
func inRange(dx, dy, r int) bool { return dx*dx+dy*dy <= r*r+r }

// disc sets every in-range grid cell around (ox, oy) in vis, for a grid with no opaque cells.
func (f *Fog) disc(ox, oy, r int, vis []uint64) {
	for dy := -r; dy <= r; dy++ {
		row := oy + dy
		if row < 0 || row >= f.height {
			continue
		}
		// The widest dx with dx*dx <= r*r+r-dy*dy.
		limit := r*r + r - dy*dy
		w := int(math.Sqrt(float64(limit)))
		for w*w > limit {
			w--
		}
		for (w+1)*(w+1) <= limit {
			w++
		}
		lo, hi := max(ox-w, 0), min(ox+w, f.width-1)
		for col := lo; col <= hi; col++ {
			setBit(vis, f.index(col, row))
		}
	}
}

// State returns what team knows about (col, row) as of the last Update; cells outside the grid and teams with nothing are Unseen.
func (f *Fog) State(team Team, col, row int) State {
	l := f.teams[team]
	if l == nil || !f.InBounds(col, row) {
		return Unseen
	}
	i := f.index(col, row)
	switch {
	case bit(l.visible, i):
		return Visible
	case bit(l.explored, i):
		return Explored
	}
	return Unseen
}

// Visible reports whether team can see (col, row) as of the last Update, such as to show or hide an enemy unit standing there.
func (f *Fog) Visible(team Team, col, row int) bool { return f.State(team, col, row) == Visible }

// Explored reports whether team has ever seen (col, row), including cells it sees now.
func (f *Fog) Explored(team Team, col, row int) bool { return f.State(team, col, row) != Unseen }

// CellAt returns the cell holding world point p, given TileWidth and TileHeight, and whether it lies in the grid.
func (f *Fog) CellAt(p core.Point) (col, row int, ok bool) {
	col, row = f.cellAt(p)
	return col, row, f.InBounds(col, row)
}

func (f *Fog) cellAt(p core.Point) (col, row int) {
	if f.TileWidth <= 0 || f.TileHeight <= 0 {
		return -1, -1
	}
	px, py := float64(p.X), float64(p.Y)
	if math.IsNaN(px) || math.IsNaN(py) || math.IsInf(px, 0) || math.IsInf(py, 0) {
		return -1, -1
	}
	if f.Orientation == tilemap.OrientationOrthogonal {
		x := math.Floor(px / float64(f.TileWidth))
		y := math.Floor(py / float64(f.TileHeight))
		if x < -1 || y < -1 || x > float64(f.width) || y > float64(f.height) {
			return -1, -1
		}
		return int(x), int(y)
	}
	maxCoord := float64(f.width+f.height+2) * float64(max(f.TileWidth, f.TileHeight)) * 2
	if math.Abs(px) > maxCoord || math.Abs(py) > maxCoord {
		return -1, -1
	}
	m := f.projectionMap()
	return m.WorldToTile(p)
}

// VisibleAt reports whether team can see the cell holding world point p as of the last Update.
func (f *Fog) VisibleAt(team Team, p core.Point) bool {
	col, row, ok := f.CellAt(p)
	return ok && f.Visible(team, col, row)
}

// Explore marks (col, row) explored for team without making it visible, such as for a scouting report or a revealed landmark. Cells outside the grid are ignored.
func (f *Fog) Explore(team Team, col, row int) {
	f.ExploreRect(team, col, row, 1, 1)
}

// ExploreRect marks the cells from (col, row) spanning w columns and h rows explored for team, clipped to the grid.
func (f *Fog) ExploreRect(team Team, col, row, w, h int) {
	// Clip the origin first and shrink the span with it; w > 0 and col < 0 cannot overflow w+col.
	if col < 0 {
		w, col = w+col, 0
	}
	if row < 0 {
		h, row = h+row, 0
	}
	if w <= 0 || h <= 0 || col >= f.width || row >= f.height {
		return
	}
	// 0 <= col < width here, so col plus at most width-col cannot overflow.
	c0, r0 := col, row
	c1, r1 := col+min(w, f.width-col), row+min(h, f.height-row)
	l := f.layer(team)
	changed := false
	for r := r0; r < r1; r++ {
		for c := c0; c < c1; c++ {
			i := f.index(c, r)
			if !bit(l.explored, i) {
				setBit(l.explored, i)
				changed = true
			}
		}
	}
	if changed {
		l.revision = nextRevision()
	}
}

// ExploreAll marks the whole grid explored for team, such as for a map-reveal cheat or a replay viewer.
func (f *Fog) ExploreAll(team Team) { f.ExploreRect(team, 0, 0, f.width, f.height) }

// Forget clears every cell team has explored except those it sees now, such as for a "re-fog" effect.
func (f *Fog) Forget(team Team) {
	l := f.teams[team]
	if l == nil {
		return
	}
	if !slices.Equal(l.explored, l.visible) {
		copy(l.explored, l.visible)
		l.revision = nextRevision()
	}
}

// ExploredCount returns how many cells team has explored, such as for an exploration percentage.
func (f *Fog) ExploredCount(team Team) int {
	l := f.teams[team]
	if l == nil {
		return 0
	}
	n := 0
	for _, w := range l.explored {
		n += bits.OnesCount64(w)
	}
	return n
}

// Revision returns a number that grows whenever team's visible or explored cells change, and that every team gets afresh when the fog is loaded, so a minimap or cached fog texture can redraw only when it differs. Revisions come from one counter shared by every Fog in the program, so a Fog loaded in place of another, even through save.Store.Load, never repeats a revision a cache has already seen. A team with nothing reports 0.
func (f *Fog) Revision(team Team) uint64 {
	if l := f.teams[team]; l != nil {
		return l.revision
	}
	return 0
}
