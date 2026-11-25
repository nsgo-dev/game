package tilemap

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// WorldLevel describes one level's world placement, metadata, and lazy loader inside a LevelWorld.
type WorldLevel struct {
	// ID is the unique level identifier (LDtk level identifier or Tiled map fileName).
	ID string
	// IID is the LDtk instance ID, when loaded from an LDtk project.
	IID string
	// UID is the LDtk numeric UID, when loaded from an LDtk project.
	UID int
	// FileName is the Tiled map file path, when loaded from a Tiled .world file.
	FileName string
	// Origin is the level's top-left world position in points.
	Origin core.Point
	// Bounds is the level's world-space bounding rectangle in points.
	Bounds core.Bounds
	// WorldDepth is the LDtk worldDepth layer (0 for Tiled worlds).
	WorldDepth int
	// Neighbours lists adjacent or overlapping levels (populated by LDtk world loaders).
	Neighbours []LevelNeighbour
	// Load builds this level's *Tilemap with Origin (and Neighbours) populated.
	Load func() (*Tilemap, error)
}

// LevelWorld holds the layout of a multi-level 2D world loaded from an LDtk project or a Tiled .world file.
type LevelWorld struct {
	// Name is the world identifier (for multi-world LDtk projects) or empty.
	Name string
	// Layout is the world layout ("Free", "GridVania", or "Tiled").
	Layout string
	// Levels lists every level in deterministic world order.
	Levels []WorldLevel
}

// Level returns the first WorldLevel with the given ID, or nil if none matches.
func (w *LevelWorld) Level(id string) *WorldLevel {
	if w == nil {
		return nil
	}
	for i := range w.Levels {
		if w.Levels[i].ID == id {
			return &w.Levels[i]
		}
	}
	return nil
}

// LevelAt returns the first WorldLevel whose Bounds contain p (using half-open [Origin, Origin+Size)), or nil if none matches.
func (w *LevelWorld) LevelAt(p core.Point) *WorldLevel {
	if w == nil {
		return nil
	}
	for i := range w.Levels {
		if boundsContainsPoint(w.Levels[i].Bounds, p) {
			return &w.Levels[i]
		}
	}
	return nil
}

// LevelsInRect returns every WorldLevel whose Bounds overlap r, in deterministic Levels order.
func (w *LevelWorld) LevelsInRect(r core.Bounds) []*WorldLevel {
	if w == nil {
		return nil
	}
	var out []*WorldLevel
	for i := range w.Levels {
		if boundsOverlap(r, w.Levels[i].Bounds) {
			out = append(out, &w.Levels[i])
		}
	}
	return out
}

// WorldBounds returns the bounding rectangle spanning every level in w, or a zero Bounds if w has no levels.
func (w *LevelWorld) WorldBounds() core.Bounds {
	if w == nil || len(w.Levels) == 0 {
		return core.Bounds{}
	}
	var minX, minY, maxX, maxY float64
	has := false
	for i := range w.Levels {
		b := w.Levels[i].Bounds
		if b.Size.Width <= 0 && b.Size.Height <= 0 {
			continue
		}
		x0 := float64(b.Origin.X)
		y0 := float64(b.Origin.Y)
		x1 := x0 + float64(b.Size.Width)
		y1 := y0 + float64(b.Size.Height)
		if !has {
			minX, minY, maxX, maxY = x0, y0, x1, y1
			has = true
		} else {
			minX = min(minX, x0)
			minY = min(minY, y0)
			maxX = max(maxX, x1)
			maxY = max(maxY, y1)
		}
	}
	if !has {
		return core.Bounds{}
	}
	return core.Bounds{
		Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
		Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
	}
}

// LoadLevel loads and returns the *Tilemap for the level with the given ID.
func (w *LevelWorld) LoadLevel(id string) (*Tilemap, error) {
	lvl := w.Level(id)
	if lvl == nil {
		return nil, fmt.Errorf("tilemap: world: level %q not found", id)
	}
	if lvl.Load == nil {
		return nil, fmt.Errorf("tilemap: world: level %q has no loader", id)
	}
	m, err := lvl.Load()
	if err != nil {
		return nil, err
	}
	if m != nil && m.Origin == (core.Point{}) && lvl.Origin != (core.Point{}) {
		m.Origin = lvl.Origin
	}
	return m, nil
}

// LoadAll eagerly loads every level in w and returns them keyed by WorldLevel.ID.
func (w *LevelWorld) LoadAll() (map[string]*Tilemap, error) {
	if w == nil {
		return nil, nil
	}
	out := make(map[string]*Tilemap, len(w.Levels))
	for i := range w.Levels {
		lvl := &w.Levels[i]
		if lvl.Load == nil {
			return nil, fmt.Errorf("tilemap: world: level %q has no loader", lvl.ID)
		}
		m, err := lvl.Load()
		if err != nil {
			return nil, err
		}
		if m != nil && m.Origin == (core.Point{}) && lvl.Origin != (core.Point{}) {
			m.Origin = lvl.Origin
		}
		out[lvl.ID] = m
	}
	return out, nil
}

// WorldStreamEventKind identifies whether a level entered or left a WorldStream's loaded set.
type WorldStreamEventKind uint8

const (
	// WorldLevelEntered reports that a level was loaded and added to the active set.
	WorldLevelEntered WorldStreamEventKind = iota
	// WorldLevelLeft reports that a level was unloaded and removed from the active set.
	WorldLevelLeft
)

// WorldStreamEvent describes one level load or unload transition produced by WorldStream.Update or Close.
type WorldStreamEvent struct {
	Kind  WorldStreamEventKind
	Level *WorldLevel
	Map   *Tilemap
}

// WorldStreamConfig configures a region-based WorldStream over a LevelWorld.
type WorldStreamConfig struct {
	// World is the multi-level world to stream.
	World *LevelWorld
	// LoadMargin expands the query/camera rectangle on all four sides (in world points)
	// when choosing which levels to load. Negative values are treated as 0.
	LoadMargin float32
	// UnloadMargin expands the query/camera rectangle on all four sides (in world points)
	// beyond which loaded levels are unloaded (hysteresis). Values smaller than LoadMargin
	// default to LoadMargin so a level cannot load and unload on the same update.
	UnloadMargin float32
	// OnEnter is called after a level is loaded and added to the active set.
	OnEnter func(level *WorldLevel, m *Tilemap)
	// OnLeave is called after a level is unloaded and removed from the active set.
	OnLeave func(level *WorldLevel, m *Tilemap)
	// OnError is called when loading a level fails.
	OnError func(level *WorldLevel, err error)
}

type streamedLevel struct {
	index int
	level *WorldLevel
	tm    *Tilemap
}

// WorldStream keeps the levels of a LevelWorld overlapping a moving camera or query region loaded
// at their world origins, unloads levels outside an optional hysteresis margin, reports enter/leave
// events, draws all loaded levels seamlessly, and supports cross-seam queries and uniform-grid physics adapters.
type WorldStream struct {
	cfg WorldStreamConfig

	loadedByIndex map[int]*Tilemap
	loaded        []streamedLevel

	// Cached uniform-grid summary across loaded levels for O(1) physics.TileGrid queries.
	gridValid   bool
	gridOriginX float32
	gridOriginY float32
	gridTileW   float32
	gridTileH   float32
	gridCols    int
	gridRows    int
}

// NewWorldStream constructs a WorldStream from cfg.
func NewWorldStream(cfg WorldStreamConfig) *WorldStream {
	if cfg.LoadMargin < 0 {
		cfg.LoadMargin = 0
	}
	if cfg.UnloadMargin < cfg.LoadMargin {
		cfg.UnloadMargin = cfg.LoadMargin
	}
	return &WorldStream{
		cfg:           cfg,
		loadedByIndex: make(map[int]*Tilemap),
	}
}

// Update unloads any loaded level whose Bounds no longer overlap region expanded by UnloadMargin,
// then loads any unloaded level whose Bounds overlap region expanded by LoadMargin.
//
// Transitions happen in deterministic World.Levels order (unloads first, then loads), firing OnLeave / OnEnter
// and returning the resulting events.
func (s *WorldStream) Update(region core.Bounds) []WorldStreamEvent {
	if s == nil || s.cfg.World == nil {
		return nil
	}
	w := s.cfg.World
	loadRect := expandBounds(region, s.cfg.LoadMargin)
	keepRect := expandBounds(region, s.cfg.UnloadMargin)

	var events []WorldStreamEvent
	changed := false

	// 1. Unload levels outside keepRect in deterministic world order.
	for i := range w.Levels {
		m, ok := s.loadedByIndex[i]
		if !ok {
			continue
		}
		lvl := &w.Levels[i]
		if boundsOverlap(keepRect, lvl.Bounds) {
			continue
		}
		delete(s.loadedByIndex, i)
		changed = true
		events = append(events, WorldStreamEvent{
			Kind:  WorldLevelLeft,
			Level: lvl,
			Map:   m,
		})
		if s.cfg.OnLeave != nil {
			s.cfg.OnLeave(lvl, m)
		}
	}

	// 2. Load levels overlapping loadRect in deterministic world order.
	for i := range w.Levels {
		if _, ok := s.loadedByIndex[i]; ok {
			continue
		}
		lvl := &w.Levels[i]
		if !boundsOverlap(loadRect, lvl.Bounds) {
			continue
		}
		if lvl.Load == nil {
			if s.cfg.OnError != nil {
				s.cfg.OnError(lvl, fmt.Errorf("tilemap: world stream: level %q has no loader", lvl.ID))
			}
			continue
		}
		m, err := lvl.Load()
		if err != nil {
			if s.cfg.OnError != nil {
				s.cfg.OnError(lvl, err)
			}
			continue
		}
		if m == nil {
			continue
		}
		if m.Origin == (core.Point{}) && lvl.Origin != (core.Point{}) {
			m.Origin = lvl.Origin
		}
		s.loadedByIndex[i] = m
		changed = true
		events = append(events, WorldStreamEvent{
			Kind:  WorldLevelEntered,
			Level: lvl,
			Map:   m,
		})
		if s.cfg.OnEnter != nil {
			s.cfg.OnEnter(lvl, m)
		}
	}

	if changed {
		s.rebuildLoadedCache()
	}
	return events
}

// UpdateCamera updates streaming for cam.VisibleBounds() (doing nothing when cam is nil).
func (s *WorldStream) UpdateCamera(cam *Camera) []WorldStreamEvent {
	if cam == nil {
		return nil
	}
	return s.Update(cam.VisibleBounds())
}

// Close unloads every currently loaded level in deterministic world order, firing OnLeave and returning the leave events.
func (s *WorldStream) Close() []WorldStreamEvent {
	if s == nil || s.cfg.World == nil || len(s.loaded) == 0 {
		return nil
	}
	events := make([]WorldStreamEvent, 0, len(s.loaded))
	for _, sl := range s.loaded {
		delete(s.loadedByIndex, sl.index)
		events = append(events, WorldStreamEvent{
			Kind:  WorldLevelLeft,
			Level: sl.level,
			Map:   sl.tm,
		})
		if s.cfg.OnLeave != nil {
			s.cfg.OnLeave(sl.level, sl.tm)
		}
	}
	s.loaded = s.loaded[:0]
	s.gridValid = false
	s.gridOriginX, s.gridOriginY = 0, 0
	s.gridTileW, s.gridTileH = 0, 0
	s.gridCols, s.gridRows = 0, 0
	return events
}

func (s *WorldStream) rebuildLoadedCache() {
	w := s.cfg.World
	s.loaded = s.loaded[:0]
	var minX, minY, maxX, maxY float64
	var tileW, tileH float32
	hasBounds := false
	for i := range w.Levels {
		m, ok := s.loadedByIndex[i]
		if !ok {
			continue
		}
		lvl := &w.Levels[i]
		s.loaded = append(s.loaded, streamedLevel{index: i, level: lvl, tm: m})
		if tileW <= 0 && m.TileWidth > 0 && m.TileHeight > 0 {
			tileW, tileH = m.TileWidth, m.TileHeight
		}
		wb := m.WorldBounds()
		if wb.Size.Width <= 0 && wb.Size.Height <= 0 {
			wb = lvl.Bounds
		}
		if wb.Size.Width > 0 && wb.Size.Height > 0 {
			x0 := float64(wb.Origin.X)
			y0 := float64(wb.Origin.Y)
			x1 := x0 + float64(wb.Size.Width)
			y1 := y0 + float64(wb.Size.Height)
			if !hasBounds {
				minX, minY, maxX, maxY = x0, y0, x1, y1
				hasBounds = true
			} else {
				minX = min(minX, x0)
				minY = min(minY, y0)
				maxX = max(maxX, x1)
				maxY = max(maxY, y1)
			}
		}
	}
	if hasBounds && tileW > 0 && tileH > 0 {
		s.gridValid = true
		s.gridOriginX = float32(minX)
		s.gridOriginY = float32(minY)
		s.gridTileW = tileW
		s.gridTileH = tileH
		s.gridCols = max(0, int(math.Ceil((maxX-minX)/float64(tileW))))
		s.gridRows = max(0, int(math.Ceil((maxY-minY)/float64(tileH))))
	} else {
		s.gridValid = false
		s.gridOriginX, s.gridOriginY = 0, 0
		s.gridTileW, s.gridTileH = 0, 0
		s.gridCols, s.gridRows = 0, 0
	}
}

// LoadedCount returns the number of currently loaded levels.
func (s *WorldStream) LoadedCount() int {
	if s == nil {
		return 0
	}
	return len(s.loaded)
}

// Loaded returns all currently loaded *Tilemap instances in deterministic world order.
func (s *WorldStream) Loaded() []*Tilemap {
	if s == nil || len(s.loaded) == 0 {
		return nil
	}
	out := make([]*Tilemap, len(s.loaded))
	for i, sl := range s.loaded {
		out[i] = sl.tm
	}
	return out
}

// LoadedLevels returns all currently loaded *WorldLevel descriptors in deterministic world order.
func (s *WorldStream) LoadedLevels() []*WorldLevel {
	if s == nil || len(s.loaded) == 0 {
		return nil
	}
	out := make([]*WorldLevel, len(s.loaded))
	for i, sl := range s.loaded {
		out[i] = sl.level
	}
	return out
}

// Map returns the currently loaded *Tilemap for level ID id, or nil if it is not loaded.
func (s *WorldStream) Map(id string) *Tilemap {
	if s == nil {
		return nil
	}
	for _, sl := range s.loaded {
		if sl.level.ID == id {
			return sl.tm
		}
	}
	return nil
}

// Draw draws every currently loaded level into d in deterministic world order, honoring each level's Origin and culling to cam.
func (s *WorldStream) Draw(d *core.Drawing, cam *Camera) {
	if s == nil {
		return
	}
	for _, sl := range s.loaded {
		sl.tm.Draw(d, cam)
	}
}

// UpdateAnimations advances animated tiles on every currently loaded level by dt.
func (s *WorldStream) UpdateAnimations(dt time.Duration) {
	if s == nil || dt <= 0 {
		return
	}
	for _, sl := range s.loaded {
		sl.tm.Update(dt)
	}
}

// LevelAt returns the first currently loaded level and its *Tilemap containing world point p, or (nil, nil) if none matches.
func (s *WorldStream) LevelAt(p core.Point) (*WorldLevel, *Tilemap) {
	if s == nil {
		return nil, nil
	}
	for _, sl := range s.loaded {
		if boundsContainsPoint(sl.tm.WorldBounds(), p) {
			return sl.level, sl.tm
		}
	}
	return nil, nil
}

// FlagsAtWorld returns the combined TileFlags at world point p across all currently loaded levels.
func (s *WorldStream) FlagsAtWorld(p core.Point) TileFlags {
	if s == nil {
		return 0
	}
	var flags TileFlags
	for _, sl := range s.loaded {
		m := sl.tm
		if !boundsContainsPoint(m.WorldBounds(), p) {
			continue
		}
		col, row := m.WorldToTile(p)
		flags |= m.FlagsAt(col, row)
	}
	return flags
}

// TilesInRect returns every matching tile across all currently loaded levels overlapping r, in deterministic level order.
func (s *WorldStream) TilesInRect(r core.Bounds, mask TileFlags) []TileHit {
	if s == nil || r.Size.Width <= 0 || r.Size.Height <= 0 {
		return nil
	}
	var hits []TileHit
	for _, sl := range s.loaded {
		m := sl.tm
		if !boundsOverlap(r, m.WorldBounds()) {
			continue
		}
		hits = append(hits, m.TilesInRect(r, mask)...)
	}
	return hits
}

// Raycast traces from -> to across all currently loaded levels and returns the closest hit matching mask.
func (s *WorldStream) Raycast(from, to core.Point, mask TileFlags) (RaycastHit, bool) {
	if s == nil {
		return RaycastHit{}, false
	}
	var best RaycastHit
	hasBest := false
	for _, sl := range s.loaded {
		hit, ok := sl.tm.Raycast(from, to, mask)
		if !ok {
			continue
		}
		if !hasBest || hit.T < best.T {
			best = hit
			hasBest = true
		}
	}
	return best, hasBest
}

// TileSize returns the tile dimensions of the loaded levels for uniform-grid physics adapters (0, 0 when no levels are loaded).
func (s *WorldStream) TileSize() (width, height float32) {
	if s == nil || !s.gridValid {
		return 0, 0
	}
	return s.gridTileW, s.gridTileH
}

// GridOrigin returns the world-space origin (min X, min Y) of the combined loaded levels, implementing physics.TileGridOrigin.
func (s *WorldStream) GridOrigin() (x, y float32) {
	if s == nil || !s.gridValid {
		return 0, 0
	}
	return s.gridOriginX, s.gridOriginY
}

// GridSize returns the combined column and row counts covering all currently loaded levels from GridOrigin.
func (s *WorldStream) GridSize() (cols, rows int) {
	if s == nil || !s.gridValid {
		return 0, 0
	}
	return s.gridCols, s.gridRows
}

// GridFlagsAt returns the combined TileFlags at uniform grid cell (col, row) relative to GridOrigin across all loaded levels.
//
// Together with TileSize, GridSize, and GridOrigin, this lets a thin physics.TileGrid + physics.TileGridOrigin adapter
// move a physics.Body or physics.CharacterBody seamlessly across seams between adjacent loaded levels.
func (s *WorldStream) GridFlagsAt(col, row int) TileFlags {
	if s == nil || !s.gridValid || col < 0 || row < 0 || col >= s.gridCols || row >= s.gridRows {
		return 0
	}
	p := core.Point{
		X: core.Points(s.gridOriginX + (float32(col)+0.5)*s.gridTileW),
		Y: core.Points(s.gridOriginY + (float32(row)+0.5)*s.gridTileH),
	}
	return s.FlagsAtWorld(p)
}

func expandBounds(r core.Bounds, margin float32) core.Bounds {
	if margin <= 0 {
		return r
	}
	m := core.Points(margin)
	return core.Bounds{
		Origin: core.Point{X: r.Origin.X - m, Y: r.Origin.Y - m},
		Size:   core.Size{Width: r.Size.Width + 2*m, Height: r.Size.Height + 2*m},
	}
}

func boundsContainsPoint(b core.Bounds, p core.Point) bool {
	if b.Size.Width <= 0 || b.Size.Height <= 0 {
		return false
	}
	return p.X >= b.Origin.X && p.X < b.Origin.X+b.Size.Width &&
		p.Y >= b.Origin.Y && p.Y < b.Origin.Y+b.Size.Height
}

func boundsOverlap(a, b core.Bounds) bool {
	if b.Size.Width <= 0 || b.Size.Height <= 0 {
		return false
	}
	if a.Size.Width < 0 || a.Size.Height < 0 {
		return false
	}
	if a.Size.Width == 0 && a.Size.Height == 0 {
		return boundsContainsPoint(b, a.Origin)
	}
	if a.Size.Width == 0 || a.Size.Height == 0 {
		return false
	}
	return a.Origin.X < b.Origin.X+b.Size.Width &&
		a.Origin.X+a.Size.Width > b.Origin.X &&
		a.Origin.Y < b.Origin.Y+b.Size.Height &&
		a.Origin.Y+a.Size.Height > b.Origin.Y
}
