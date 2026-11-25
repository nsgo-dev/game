package procgen

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/random"
)

// ErrNoLayout is returned when room placement exhausts its attempts without finding a non-overlapping, connected layout that satisfies the graph and templates.
var ErrNoLayout = errors.New("procgen: room placement found no valid layout")

// PlacementMode selects how rooms are arranged in 2D space.
type PlacementMode string

const (
	// PlacementStitch places variable-sized room templates by aligning matching door sockets without overlap.
	PlacementStitch PlacementMode = "stitch"
	// PlacementGrid places uniform-sized room templates on a Spelunky-style coarse grid of room slots.
	PlacementGrid PlacementMode = "grid"
)

// PlacedRoom is one room template instantiated in a RoomLevel.
type PlacedRoom struct {
	// NodeID is the room's node ID in RoomLevel.Graph.
	NodeID int
	// TemplateID is the ID of the RoomTemplate chosen for this room.
	TemplateID string
	// Source is the RoomTemplate.Source path (.tmj or scene) when the template references an external file.
	Source string
	// Bounds is the room's cell rectangle inside RoomLevel.Grid.
	Bounds Rect
	// GridSlot is the room's coarse (col, row) slot in PlacementGrid mode; zero in PlacementStitch mode.
	GridSlot Point
	// Rotation is the clockwise rotation applied to the template in degrees (0, 90, 180 or 270).
	Rotation int
	// Tags is a copy of the chosen template's tags.
	Tags []string
	// Role is the graph node's role (start, goal, critical, branch or key).
	Role RoomRole
	// KeyID is non-empty when this room holds the key for the matching locked door.
	KeyID string
}

// PlacedDoor is one stitched connection between two PlacedRooms.
type PlacedDoor struct {
	// FromRoom and ToRoom are NodeIDs of the connected rooms.
	FromRoom int
	ToRoom   int
	// FromSide is the side of FromRoom where the doorway opens; ToSide is its opposite on ToRoom.
	FromSide Side
	ToSide   Side
	// Cells lists the doorway's Grid cells across both rooms' shared boundary.
	Cells []Point
	// LockID is non-empty when this doorway is locked.
	LockID string
	// Channel is the matched DoorSocket.Channel.
	Channel string
}

// RoomLevel is the result of GenerateRooms or PlaceRooms.
type RoomLevel struct {
	// Grid holds the stamped level tiles; Connected(passable) always holds.
	Grid *Grid
	// Rooms lists each placed room, indexed by NodeID.
	Rooms []PlacedRoom
	// Doors lists every stitched doorway corresponding to Graph.Edges.
	Doors []PlacedDoor
	// Graph is the dungeon layout graph realized by Rooms and Doors.
	Graph *LayoutGraph
}

// PlaceConfig configures PlaceRooms. Zero fields take the defaults given on each field.
type PlaceConfig struct {
	// Mode selects PlacementStitch (default) or PlacementGrid.
	Mode PlacementMode
	// GridCols and GridRows set the coarse grid size in PlacementGrid mode.
	// When both are 0, a square grid large enough for the graph is chosen automatically.
	GridCols int
	GridRows int
	// Attempts is the deterministic retry budget before returning ErrNoLayout. The default is 50.
	Attempts int
	// Padding adds this many Wall cells around the stitched bounding box in Grid. The default is 0.
	Padding int
	// ResolveGrid optionally loads the tile Grid for a RoomTemplate with Source set.
	// When nil, Source-only templates stamp a default walled floor footprint of Width by Height.
	ResolveGrid func(t *RoomTemplate) (*Grid, error)
	// Passable defines which cell values count as walkable for connectivity verification.
	// When nil, IsFloor is used (or non-Wall when a template uses custom tile letters 'a'..'z').
	Passable func(v uint32) bool
}

// RoomsConfig configures GenerateRooms. Zero fields take the defaults given on each field.
type RoomsConfig struct {
	// Templates is the room template set to draw from. It must be valid.
	Templates *RoomTemplates
	// Graph configures the layout graph (critical path, branches, loops, locks).
	Graph GraphConfig
	// Mode selects PlacementStitch (default) or PlacementGrid.
	Mode PlacementMode
	// GridCols and GridRows set the coarse grid size in PlacementGrid mode.
	// When both are 0, a grid large enough for the graph is chosen automatically.
	GridCols int
	GridRows int
	// Attempts is the deterministic retry budget before returning ErrNoLayout. The default is 50.
	Attempts int
	// Padding adds this many Wall cells around the stitched bounding box in Grid. The default is 0.
	Padding int
	// ResolveGrid optionally loads the tile Grid for a RoomTemplate with Source set.
	ResolveGrid func(t *RoomTemplate) (*Grid, error)
	// Passable defines which cell values count as walkable for connectivity verification.
	Passable func(v uint32) bool
}

func (c PlaceConfig) withDefaults() PlaceConfig {
	if c.Mode == "" {
		c.Mode = PlacementStitch
	}
	if c.Attempts == 0 {
		c.Attempts = 50
	}
	return c
}

func (c PlaceConfig) validate() error {
	switch {
	case c.Mode != PlacementStitch && c.Mode != PlacementGrid:
		return fmt.Errorf("%w: unknown placement mode %q", ErrInvalidConfig, c.Mode)
	case c.Attempts < 0:
		return fmt.Errorf("%w: Attempts %d is negative", ErrInvalidConfig, c.Attempts)
	case c.Padding < 0:
		return fmt.Errorf("%w: Padding %d is negative", ErrInvalidConfig, c.Padding)
	case (c.GridCols < 0 || c.GridRows < 0):
		return fmt.Errorf("%w: GridCols/GridRows (%d,%d) cannot be negative", ErrInvalidConfig, c.GridCols, c.GridRows)
	case (c.GridCols > 0) != (c.GridRows > 0):
		return fmt.Errorf("%w: GridCols and GridRows must both be set or both be 0", ErrInvalidConfig)
	}
	return nil
}

// roomVariant is one oriented (possibly rotated) instance of a RoomTemplate with precomputed grid and door sockets.
type roomVariant struct {
	tmpl     *RoomTemplate
	rotation int
	w, h     int
	doors    []DoorSocket
	grid     *Grid
	weight   float64
	isStart  bool
	isGoal   bool
	isKey    bool
}

func compileVariants(rt *RoomTemplates, resolve func(*RoomTemplate) (*Grid, error)) ([]roomVariant, map[string]bool, error) {
	if rt == nil {
		return nil, nil, fmt.Errorf("%w: Templates is nil", ErrInvalidConfig)
	}
	if err := rt.Validate(); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	allTags := map[string]bool{}
	var variants []roomVariant
	for i := range rt.Rooms {
		t := &rt.Rooms[i]
		var baseGrid *Grid
		var err error
		if t.Source != "" && resolve != nil {
			baseGrid, err = resolve(t)
			if err != nil {
				return nil, nil, fmt.Errorf("procgen: resolve room %q (%s): %w", t.ID, t.Source, err)
			}
			w, h := t.Size()
			if baseGrid == nil || baseGrid.Width != w || baseGrid.Height != h {
				return nil, nil, fmt.Errorf("%w: resolved grid for room %q has wrong size (want %dx%d)", ErrInvalidConfig, t.ID, w, h)
			}
		} else {
			baseGrid, err = t.ToGrid()
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
			}
		}
		for _, tag := range t.Tags {
			allTags[tag] = true
		}
		rots := []int{0}
		if t.AllowRotate {
			rots = []int{0, 90, 180, 270}
		}
		curGrid := baseGrid
		curDoors := slices.Clone(t.Doors)
		for _, rot := range rots {
			if rot > 0 {
				curGrid, curDoors = rotateRoomCW(curGrid, curDoors)
			}
			variants = append(variants, roomVariant{
				tmpl:     t,
				rotation: rot,
				w:        curGrid.Width,
				h:        curGrid.Height,
				doors:    slices.Clone(curDoors),
				grid:     curGrid.Clone(),
				weight:   t.EffectiveWeight() / float64(len(rots)),
				isStart:  t.HasTag("start"),
				isGoal:   t.HasTag("goal"),
				isKey:    t.HasTag("key"),
			})
		}
	}
	return variants, allTags, nil
}

// rotateRoomCW rotates a room grid and its door sockets 90 degrees clockwise.
func rotateRoomCW(g *Grid, doors []DoorSocket) (*Grid, []DoorSocket) {
	w, h := g.Width, g.Height
	out := NewGrid(h, w)
	for y := range h {
		for x := range w {
			out.Set(h-1-y, x, g.At(x, y))
		}
	}
	rd := make([]DoorSocket, len(doors))
	for i, d := range doors {
		sp := d.Span()
		off := d.Offset
		switch d.Side {
		case SideNorth, SideSouth:
			// North -> East (x'=h-1, y'=x), South -> West (x'=0, y'=x): offset unchanged.
		case SideEast, SideWest:
			// East -> South (x'=h-1-y, y'=w-1), West -> North (x'=h-1-y, y'=0): offset flips within h.
			off = h - d.Offset - sp
		}
		rd[i] = DoorSocket{
			Side:    d.Side.RotateCW(),
			Offset:  off,
			Width:   d.Width,
			Channel: d.Channel,
		}
	}
	return out, rd
}

// uniformSlotSize verifies every variant shares the same width and height (required for PlacementGrid).
func uniformSlotSize(variants []roomVariant) (int, int, error) {
	w, h := variants[0].w, variants[0].h
	for _, v := range variants[1:] {
		if v.w != w || v.h != h {
			return 0, 0, fmt.Errorf("%w: PlacementGrid requires uniform room dimensions (got %dx%d and %dx%d)", ErrInvalidConfig, w, h, v.w, v.h)
		}
	}
	return w, h, nil
}

func autoGridSize(nodeCount int) (int, int) {
	// Give comfortable room so paths, branches and key rooms have free neighbors.
	area := max(nodeCount*2, 9)
	side := int(math.Ceil(math.Sqrt(float64(area))))
	if side < 3 {
		side = 3
	}
	return side, side
}

// GenerateRooms generates a dungeon layout graph and places hand-made room templates onto a Grid, retrying deterministically from s up to cfg.Attempts times.
//
// In PlacementStitch mode, rooms of arbitrary sizes are stitched by matching door sockets without overlap and same-tier loops are closed where aligned sockets meet. In PlacementGrid mode (Spelunky-style), uniform-sized rooms are laid out on a coarse grid of room slots so critical path, branches, lock/key tiers and loops align naturally.
func GenerateRooms(s *random.Stream, cfg RoomsConfig) (*RoomLevel, error) {
	graphCfg := cfg.Graph.withDefaults()
	if err := graphCfg.validate(); err != nil {
		return nil, err
	}
	placeCfg := PlaceConfig{
		Mode:        cfg.Mode,
		GridCols:    cfg.GridCols,
		GridRows:    cfg.GridRows,
		Attempts:    cfg.Attempts,
		Padding:     cfg.Padding,
		ResolveGrid: cfg.ResolveGrid,
		Passable:    cfg.Passable,
	}.withDefaults()
	if err := placeCfg.validate(); err != nil {
		return nil, err
	}
	variants, allTags, err := compileVariants(cfg.Templates, placeCfg.ResolveGrid)
	if err != nil {
		return nil, err
	}
	passable := effectivePassable(placeCfg.Passable, variants)

	if placeCfg.Mode == PlacementGrid {
		slotW, slotH, err := uniformSlotSize(variants)
		if err != nil {
			return nil, err
		}
		cols, rows := placeCfg.GridCols, placeCfg.GridRows
		if cols == 0 && rows == 0 {
			cols, rows = autoGridSize(graphCfg.CriticalPath + graphCfg.Branches + graphCfg.Locks)
		}
		if cols*rows < graphCfg.CriticalPath {
			return nil, fmt.Errorf("%w: grid %dx%d (%d slots) is smaller than CriticalPath %d", ErrInvalidConfig, cols, rows, cols*rows, graphCfg.CriticalPath)
		}
		gridW := cols*slotW + 2*placeCfg.Padding
		gridH := rows*slotH + 2*placeCfg.Padding
		if gridW > MaxSide || gridH > MaxSide || tooManyCells(gridW, gridH) {
			return nil, fmt.Errorf("%w: resulting grid %dx%d exceeds limits", ErrInvalidConfig, gridW, gridH)
		}
		var best *RoomLevel
		bestLoops := -1
		for range placeCfg.Attempts {
			lvl := attemptGenerateGrid(s, graphCfg, variants, allTags, cols, rows, slotW, slotH, placeCfg.Padding, passable)
			if lvl == nil {
				continue
			}
			gotLoops := countLoops(lvl.Graph)
			if gotLoops >= graphCfg.Loops {
				return lvl, nil
			}
			if gotLoops > bestLoops {
				best = lvl
				bestLoops = gotLoops
			}
		}
		if best != nil {
			return best, nil
		}
		return nil, fmt.Errorf("%w after %d attempts", ErrNoLayout, placeCfg.Attempts)
	}

	// PlacementStitch: build tree backbone, stitch with loop-aware placement, close aligned same-tier loops.
	var best *RoomLevel
	bestLoops := -1
	for range placeCfg.Attempts {
		g, _ := buildGraphTree(s, graphCfg)
		lvl := attemptStitchTreeAndLoops(s, g, graphCfg.Loops, graphCfg.MaxDegree, variants, allTags, placeCfg.Padding, passable)
		if lvl == nil {
			continue
		}
		gotLoops := countLoops(lvl.Graph)
		if gotLoops >= graphCfg.Loops {
			return lvl, nil
		}
		if gotLoops > bestLoops {
			best = lvl
			bestLoops = gotLoops
		}
	}
	if best != nil {
		return best, nil
	}
	return nil, fmt.Errorf("%w after %d attempts", ErrNoLayout, placeCfg.Attempts)
}

// PlaceRooms places an existing LayoutGraph using templates according to cfg, retrying deterministically from s up to cfg.Attempts times.
func PlaceRooms(s *random.Stream, graph *LayoutGraph, templates *RoomTemplates, cfg PlaceConfig) (*RoomLevel, error) {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil, fmt.Errorf("%w: LayoutGraph is empty", ErrInvalidConfig)
	}
	if err := graph.VerifyLocks(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	variants, allTags, err := compileVariants(templates, cfg.ResolveGrid)
	if err != nil {
		return nil, err
	}
	passable := effectivePassable(cfg.Passable, variants)

	if cfg.Mode == PlacementGrid {
		slotW, slotH, err := uniformSlotSize(variants)
		if err != nil {
			return nil, err
		}
		cols, rows := cfg.GridCols, cfg.GridRows
		if cols == 0 && rows == 0 {
			cols, rows = autoGridSize(len(graph.Nodes))
		}
		if cols*rows < len(graph.Nodes) {
			return nil, fmt.Errorf("%w: grid %dx%d has fewer slots than %d graph nodes", ErrInvalidConfig, cols, rows, len(graph.Nodes))
		}
		gridW := cols*slotW + 2*cfg.Padding
		gridH := rows*slotH + 2*cfg.Padding
		if gridW > MaxSide || gridH > MaxSide || tooManyCells(gridW, gridH) {
			return nil, fmt.Errorf("%w: resulting grid %dx%d exceeds limits", ErrInvalidConfig, gridW, gridH)
		}
		for range cfg.Attempts {
			if lvl := attemptPlaceGraphOnGrid(s, cloneGraph(graph), variants, allTags, cols, rows, slotW, slotH, cfg.Padding, passable); lvl != nil {
				return lvl, nil
			}
		}
		return nil, fmt.Errorf("%w after %d attempts", ErrNoLayout, cfg.Attempts)
	}

	for range cfg.Attempts {
		if lvl := attemptPlaceGraphStitch(s, cloneGraph(graph), variants, allTags, cfg.Padding, passable); lvl != nil {
			return lvl, nil
		}
	}
	return nil, fmt.Errorf("%w after %d attempts", ErrNoLayout, cfg.Attempts)
}

func effectivePassable(user func(uint32) bool, variants []roomVariant) func(uint32) bool {
	if user != nil {
		return user
	}
	for _, v := range variants {
		for _, cell := range v.grid.Cells {
			if cell > Floor {
				return isNonWall
			}
		}
	}
	return IsFloor
}

func countLoops(g *LayoutGraph) int {
	n := 0
	for _, e := range g.Edges {
		if e.Loop {
			n++
		}
	}
	return n
}

func cloneGraph(g *LayoutGraph) *LayoutGraph {
	return &LayoutGraph{
		Start:        g.Start,
		Goal:         g.Goal,
		CriticalPath: slices.Clone(g.CriticalPath),
		Nodes:        slices.Clone(g.Nodes),
		Edges:        slices.Clone(g.Edges),
		Locks:        slices.Clone(g.Locks),
	}
}

// eligibleVariants returns the indices into variants allowed for node, prioritizing exact tag matches and avoiding start/goal-only templates for ordinary rooms.
func eligibleVariants(variants []roomVariant, allTags map[string]bool, node GraphNode, minDoors int) []int {
	// If the template set defines templates with node.Tag, require that tag.
	requireTag := node.Tag != "" && allTags[node.Tag]
	var preferred, fallback []int
	for i := range variants {
		v := &variants[i]
		if len(v.doors) < minDoors {
			continue
		}
		if requireTag {
			if v.tmpl.HasTag(node.Tag) {
				preferred = append(preferred, i)
			}
			continue
		}
		// For untagged/generic nodes, avoid reserved start/goal/key templates if generic ones exist.
		if (v.isStart && node.Role != RoleStart) || (v.isGoal && node.Role != RoleGoal) || (v.isKey && node.Role != RoleKey) {
			fallback = append(fallback, i)
			continue
		}
		preferred = append(preferred, i)
	}
	if len(preferred) > 0 {
		return preferred
	}
	return fallback
}

// weightedOrder returns a deterministic weighted permutation of indices using s.
func weightedOrder(s *random.Stream, variants []roomVariant, indices []int) []int {
	if len(indices) <= 1 {
		return slices.Clone(indices)
	}
	rem := slices.Clone(indices)
	weights := make([]float64, len(rem))
	out := make([]int, 0, len(rem))
	for len(rem) > 0 {
		weights = weights[:len(rem)]
		for i, idx := range rem {
			weights[i] = variants[idx].weight
		}
		pick := s.WeightedIndex(weights)
		if pick < 0 {
			pick = s.Intn(len(rem))
		}
		out = append(out, rem[pick])
		rem = slices.Delete(rem, pick, pick+1)
	}
	return out
}

func rectsOverlap(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// alignedPosition computes the top-left (x, y) of a child room of size (cw, ch) whose socket sockChild attaches to parent's socket sockParent on parent's bounds.
func alignedPosition(parent Rect, sockParent DoorSocket, cw, ch int, sockChild DoorSocket) Point {
	switch sockParent.Side {
	case SideEast:
		return Point{X: parent.X + parent.W, Y: parent.Y + sockParent.Offset - sockChild.Offset}
	case SideWest:
		return Point{X: parent.X - cw, Y: parent.Y + sockParent.Offset - sockChild.Offset}
	case SideSouth:
		return Point{X: parent.X + sockParent.Offset - sockChild.Offset, Y: parent.Y + parent.H}
	case SideNorth:
		return Point{X: parent.X + sockParent.Offset - sockChild.Offset, Y: parent.Y - ch}
	}
	return Point{}
}

// socketsAligned reports whether sockA on boundsA and sockB on boundsB meet flush along a shared boundary with identical span and channel.
func socketsAligned(boundsA Rect, sockA DoorSocket, boundsB Rect, sockB DoorSocket) bool {
	if !sockA.Matches(sockB) {
		return false
	}
	want := alignedPosition(boundsA, sockA, boundsB.W, boundsB.H, sockB)
	return want.X == boundsB.X && want.Y == boundsB.Y
}

// stitchPlacement holds the mutable state while placing a graph in PlacementStitch mode.
type stitchPlacement struct {
	s          *random.Stream
	g          *LayoutGraph
	variants   []roomVariant
	allTags    map[string]bool
	adj        [][]int // edge indices per node
	order      []int   // BFS placement order
	parentNode []int   // spanning parent (-1 for Start)
	parentEdge []int   // edge index to parent (-1 for Start)
	placed     []bool
	variantIdx []int
	bounds     []Rect
	usedDoor   [][]bool
	edgeDoors  [][2]int // for each edge: [doorIdxOnFrom, doorIdxOnTo]
	wantLoops  bool
	steps      int
	maxSteps   int
}

func newStitchPlacement(s *random.Stream, g *LayoutGraph, variants []roomVariant, allTags map[string]bool, wantLoops bool) *stitchPlacement {
	n := len(g.Nodes)
	adj := make([][]int, n)
	for ei, e := range g.Edges {
		adj[e.From] = append(adj[e.From], ei)
		adj[e.To] = append(adj[e.To], ei)
	}
	order := make([]int, 0, n)
	parentNode := make([]int, n)
	parentEdge := make([]int, n)
	seen := make([]bool, n)
	for i := range n {
		parentNode[i] = -1
		parentEdge[i] = -1
	}
	seen[g.Start] = true
	order = append(order, g.Start)
	for head := 0; head < len(order); head++ {
		u := order[head]
		for _, ei := range adj[u] {
			e := g.Edges[ei]
			v := e.To
			if v == u {
				v = e.From
			}
			if !seen[v] {
				seen[v] = true
				parentNode[v] = u
				parentEdge[v] = ei
				order = append(order, v)
			}
		}
	}
	edgeDoors := make([][2]int, len(g.Edges))
	for i := range edgeDoors {
		edgeDoors[i] = [2]int{-1, -1}
	}
	return &stitchPlacement{
		s:          s,
		g:          g,
		variants:   variants,
		allTags:    allTags,
		adj:        adj,
		order:      order,
		parentNode: parentNode,
		parentEdge: parentEdge,
		placed:     make([]bool, n),
		variantIdx: make([]int, n),
		bounds:     make([]Rect, n),
		usedDoor:   make([][]bool, n),
		edgeDoors:  edgeDoors,
		wantLoops:  wantLoops,
		maxSteps:   max(n*96, 512),
	}
}

func (sp *stitchPlacement) solve(idx int) bool {
	if idx == len(sp.order) {
		return true
	}
	if sp.steps >= sp.maxSteps {
		return false
	}
	sp.steps++

	nodeID := sp.order[idx]
	node := sp.g.Nodes[nodeID]
	deg := len(sp.adj[nodeID])
	elig := eligibleVariants(sp.variants, sp.allTags, node, deg)
	if len(elig) == 0 {
		return false
	}

	if idx == 0 {
		for _, vi := range weightedOrder(sp.s, sp.variants, elig) {
			v := &sp.variants[vi]
			sp.placed[nodeID] = true
			sp.variantIdx[nodeID] = vi
			sp.bounds[nodeID] = Rect{X: 0, Y: 0, W: v.w, H: v.h}
			sp.usedDoor[nodeID] = make([]bool, len(v.doors))
			if sp.solve(1) {
				return true
			}
			sp.placed[nodeID] = false
		}
		return false
	}

	pID := sp.parentNode[nodeID]
	pEdge := sp.parentEdge[nodeID]
	pVar := &sp.variants[sp.variantIdx[pID]]
	pBounds := sp.bounds[pID]

	// Collect any already-placed loop neighbors that must ALSO align with nodeID.
	var extraEdges []int
	for _, ei := range sp.adj[nodeID] {
		if ei == pEdge {
			continue
		}
		e := sp.g.Edges[ei]
		other := e.To
		if other == nodeID {
			other = e.From
		}
		if sp.placed[other] {
			extraEdges = append(extraEdges, ei)
		}
	}

	var cands []stitchCand
	for _, vi := range elig {
		cv := &sp.variants[vi]
		for pdIdx, pSock := range pVar.doors {
			if sp.usedDoor[pID][pdIdx] {
				continue
			}
			for cdIdx, cSock := range cv.doors {
				if !pSock.Matches(cSock) {
					continue
				}
				pos := alignedPosition(pBounds, pSock, cv.w, cv.h, cSock)
				rb := Rect{X: pos.X, Y: pos.Y, W: cv.w, H: cv.h}
				overlap := false
				for other := range sp.g.Nodes {
					if sp.placed[other] && rectsOverlap(rb, sp.bounds[other]) {
						overlap = true
						break
					}
				}
				if overlap {
					continue
				}
				adjBonus := 0
				if sp.wantLoops && nodeID != sp.g.Goal {
					for other := range sp.g.Nodes {
						if !sp.placed[other] || other == pID || other == sp.g.Goal {
							continue
						}
						if sp.g.Nodes[other].Tier != node.Tier {
							continue
						}
						ov := &sp.variants[sp.variantIdx[other]]
						for odIdx, oSock := range ov.doors {
							if sp.usedDoor[other][odIdx] {
								continue
							}
							for cd2, cSock2 := range cv.doors {
								if cd2 == cdIdx {
									continue
								}
								if socketsAligned(sp.bounds[other], oSock, rb, cSock2) {
									adjBonus++
								}
							}
						}
					}
				}
				cands = append(cands, stitchCand{
					vi:       vi,
					pDoor:    pdIdx,
					cDoor:    cdIdx,
					bounds:   rb,
					weight:   cv.weight,
					adjacent: adjBonus,
				})
			}
		}
	}
	if len(cands) == 0 {
		return false
	}

	// Order candidates deterministically by weighted draw, giving loop-touching placements priority when wantLoops is set.
	ordered := orderStitchCandidates(sp.s, cands, sp.wantLoops)
	// Cap branching factor per node to keep backtracking fast and bounded.
	const maxBranch = 16
	if len(ordered) > maxBranch {
		ordered = ordered[:maxBranch]
	}

	for _, c := range ordered {
		cv := &sp.variants[c.vi]
		usedChild := make([]bool, len(cv.doors))
		usedChild[c.cDoor] = true

		// Verify all already-placed loop edges align with unused doors on both rooms.
		type extraMatch struct {
			edgeIdx   int
			otherID   int
			otherDoor int
			childDoor int
		}
		var matchedExtras []extraMatch
		okExtras := true
		for _, ei := range extraEdges {
			e := sp.g.Edges[ei]
			other := e.To
			if other == nodeID {
				other = e.From
			}
			ov := &sp.variants[sp.variantIdx[other]]
			found := false
			for odIdx, oSock := range ov.doors {
				if sp.usedDoor[other][odIdx] {
					continue
				}
				for cdIdx, cSock := range cv.doors {
					if usedChild[cdIdx] {
						continue
					}
					if socketsAligned(sp.bounds[other], oSock, c.bounds, cSock) {
						matchedExtras = append(matchedExtras, extraMatch{
							edgeIdx:   ei,
							otherID:   other,
							otherDoor: odIdx,
							childDoor: cdIdx,
						})
						usedChild[cdIdx] = true
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				okExtras = false
				break
			}
		}
		if !okExtras {
			continue
		}

		// Commit placement.
		sp.placed[nodeID] = true
		sp.variantIdx[nodeID] = c.vi
		sp.bounds[nodeID] = c.bounds
		sp.usedDoor[nodeID] = usedChild
		sp.usedDoor[pID][c.pDoor] = true
		setEdgeDoors(sp.g.Edges[pEdge], &sp.edgeDoors[pEdge], pID, c.pDoor, nodeID, c.cDoor)
		for _, em := range matchedExtras {
			sp.usedDoor[em.otherID][em.otherDoor] = true
			setEdgeDoors(sp.g.Edges[em.edgeIdx], &sp.edgeDoors[em.edgeIdx], em.otherID, em.otherDoor, nodeID, em.childDoor)
		}

		if sp.solve(idx + 1) {
			return true
		}

		// Rollback.
		sp.placed[nodeID] = false
		sp.usedDoor[pID][c.pDoor] = false
		sp.edgeDoors[pEdge] = [2]int{-1, -1}
		for _, em := range matchedExtras {
			sp.usedDoor[em.otherID][em.otherDoor] = false
			sp.edgeDoors[em.edgeIdx] = [2]int{-1, -1}
		}
	}
	return false
}

func setEdgeDoors(e GraphEdge, slot *[2]int, aID, aDoor, bID, bDoor int) {
	if e.From == aID && e.To == bID {
		*slot = [2]int{aDoor, bDoor}
	} else {
		*slot = [2]int{bDoor, aDoor}
	}
}

type stitchCand struct {
	vi       int
	pDoor    int
	cDoor    int
	bounds   Rect
	weight   float64
	adjacent int // bonus count of aligned same-tier placed rooms (helps form loops in GenerateRooms)
}

func orderStitchCandidates(s *random.Stream, cands []stitchCand, preferAdjacent bool) []stitchCand {
	if len(cands) <= 1 {
		return slices.Clone(cands)
	}
	if !preferAdjacent {
		rem := slices.Clone(cands)
		weights := make([]float64, len(rem))
		out := make([]stitchCand, 0, len(rem))
		for len(rem) > 0 {
			weights = weights[:len(rem)]
			for i := range rem {
				weights[i] = rem[i].weight
			}
			pick := s.WeightedIndex(weights)
			if pick < 0 {
				pick = s.Intn(len(rem))
			}
			out = append(out, rem[pick])
			rem = slices.Delete(rem, pick, pick+1)
		}
		return out
	}
	// Partition into adjacent (>0) and non-adjacent (0), weighting each group independently.
	var withAdj, withoutAdj []stitchCand
	for _, c := range cands {
		if c.adjacent > 0 {
			withAdj = append(withAdj, c)
		} else {
			withoutAdj = append(withoutAdj, c)
		}
	}
	out := orderStitchCandidates(s, withAdj, false)
	return append(out, orderStitchCandidates(s, withoutAdj, false)...)
}

func attemptStitchTreeAndLoops(s *random.Stream, g *LayoutGraph, wantLoops, maxDegree int, variants []roomVariant, allTags map[string]bool, padding int, passable func(uint32) bool) *RoomLevel {
	sp := newStitchPlacement(s, g, variants, allTags, wantLoops > 0)
	if !sp.solve(0) {
		return nil
	}
	// Discover aligned same-tier pairs to form loops.
	if wantLoops > 0 {
		n := len(g.Nodes)
		deg := make([]int, n)
		connected := make([]bool, n*n)
		for _, e := range g.Edges {
			deg[e.From]++
			deg[e.To]++
			connected[e.From*n+e.To] = true
			connected[e.To*n+e.From] = true
		}
		type loopCand struct {
			a, b  int
			aDoor int
			bDoor int
		}
		var lc []loopCand
		for a := range n {
			if a == g.Goal {
				continue
			}
			va := &variants[sp.variantIdx[a]]
			for b := a + 1; b < n; b++ {
				if b == g.Goal || connected[a*n+b] || g.Nodes[a].Tier != g.Nodes[b].Tier {
					continue
				}
				vb := &variants[sp.variantIdx[b]]
				for ad, sa := range va.doors {
					if sp.usedDoor[a][ad] {
						continue
					}
					for bd, sb := range vb.doors {
						if sp.usedDoor[b][bd] {
							continue
						}
						if socketsAligned(sp.bounds[a], sa, sp.bounds[b], sb) {
							lc = append(lc, loopCand{a: a, b: b, aDoor: ad, bDoor: bd})
						}
					}
				}
			}
		}
		random.Shuffle(s, lc)
		added := 0
		for _, c := range lc {
			if added >= wantLoops {
				break
			}
			if connected[c.a*n+c.b] || deg[c.a] >= maxDegree || deg[c.b] >= maxDegree {
				continue
			}
			if sp.usedDoor[c.a][c.aDoor] || sp.usedDoor[c.b][c.bDoor] {
				continue
			}
			g.Edges = append(g.Edges, GraphEdge{From: c.a, To: c.b, Loop: true})
			sp.edgeDoors = append(sp.edgeDoors, [2]int{c.aDoor, c.bDoor})
			sp.usedDoor[c.a][c.aDoor] = true
			sp.usedDoor[c.b][c.bDoor] = true
			connected[c.a*n+c.b] = true
			connected[c.b*n+c.a] = true
			deg[c.a]++
			deg[c.b]++
			added++
		}
	}
	computeBFSDepths(g)
	return finalizeLevel(g, variants, sp.variantIdx, sp.bounds, nil, sp.edgeDoors, padding, passable)
}

func attemptPlaceGraphStitch(s *random.Stream, g *LayoutGraph, variants []roomVariant, allTags map[string]bool, padding int, passable func(uint32) bool) *RoomLevel {
	sp := newStitchPlacement(s, g, variants, allTags, false)
	if !sp.solve(0) {
		return nil
	}
	return finalizeLevel(g, variants, sp.variantIdx, sp.bounds, nil, sp.edgeDoors, padding, passable)
}

// --- Spelunky-style PlacementGrid mode ---

func attemptGenerateGrid(s *random.Stream, cfg GraphConfig, variants []roomVariant, allTags map[string]bool, cols, rows, slotW, slotH, padding int, passable func(uint32) bool) *RoomLevel {
	// 1. Build a self-avoiding critical path of length cfg.CriticalPath on the cols x rows grid.
	// Start on row 0 when rows > 1 for a Spelunky top-to-bottom progression feel.
	startSlot := Point{X: s.Intn(cols), Y: 0}
	path := buildGridPath(s, cols, rows, startSlot, cfg.CriticalPath)
	if len(path) < cfg.CriticalPath {
		return nil
	}

	p := cfg.CriticalPath
	g := &LayoutGraph{
		Start:        0,
		Goal:         p - 1,
		CriticalPath: make([]int, p),
		Nodes:        make([]GraphNode, p),
		Edges:        make([]GraphEdge, 0, p-1+cfg.Branches+cfg.Locks+cfg.Loops),
	}
	slots := make([]Point, p)
	occ := make([]int, cols*rows)
	for i := range occ {
		occ[i] = -1
	}
	degree := make([]int, p)
	branchDepth := make([]int, p)

	for i := range p {
		role := RoleCritical
		tag := ""
		switch i {
		case 0:
			role = RoleStart
			tag = "start"
		case p - 1:
			role = RoleGoal
			tag = "goal"
		}
		g.CriticalPath[i] = i
		g.Nodes[i] = GraphNode{ID: i, Role: role, Tag: tag}
		slots[i] = path[i]
		occ[path[i].Y*cols+path[i].X] = i
		if i > 0 {
			g.Edges = append(g.Edges, GraphEdge{From: i - 1, To: i})
			degree[i-1]++
			degree[i]++
		}
	}

	// 2. Pick cfg.Locks locked edges on the critical path and assign tiers.
	lockEdges := make([]int, 0, cfg.Locks)
	if cfg.Locks > 0 {
		pool := make([]int, p-1)
		for i := range p - 1 {
			pool[i] = i
		}
		random.Shuffle(s, pool)
		lockEdges = append(lockEdges, pool[:cfg.Locks]...)
		slices.Sort(lockEdges)
		for k, edgeIdx := range lockEdges {
			g.Edges[edgeIdx].LockID = fmt.Sprintf("key-%d", k+1)
		}
	}
	tier := 0
	for i := range p {
		g.Nodes[i].Tier = tier
		if i < p-1 && g.Edges[i].LockID != "" {
			tier++
		}
	}

	// Helper to grow one room adjacent to a valid parent slot.
	growIntoSlot := func(filter func(nodeID int) bool, role RoomRole, tag string) int {
		type cand struct {
			parent int
			slot   Point
		}
		var cands []cand
		for i := range g.Nodes {
			if i == g.Goal || degree[i] >= cfg.MaxDegree || !filter(i) {
				continue
			}
			for _, side := range allSides {
				d := side.Delta()
				np := Point{X: slots[i].X + d.X, Y: slots[i].Y + d.Y}
				if np.X < 0 || np.Y < 0 || np.X >= cols || np.Y >= rows {
					continue
				}
				if occ[np.Y*cols+np.X] >= 0 {
					continue
				}
				cands = append(cands, cand{parent: i, slot: np})
			}
		}
		if len(cands) == 0 {
			return -1
		}
		pick := random.Pick(s, cands)
		id := len(g.Nodes)
		g.Nodes = append(g.Nodes, GraphNode{
			ID:   id,
			Role: role,
			Tier: g.Nodes[pick.parent].Tier,
			Tag:  tag,
		})
		g.Edges = append(g.Edges, GraphEdge{From: pick.parent, To: id})
		slots = append(slots, pick.slot)
		occ[pick.slot.Y*cols+pick.slot.X] = id
		degree = append(degree, 1)
		degree[pick.parent]++
		branchDepth = append(branchDepth, branchDepth[pick.parent]+1)
		return id
	}

	// 3. Grow cfg.Branches side rooms into free adjacent grid slots.
	for range cfg.Branches {
		id := growIntoSlot(func(i int) bool { return branchDepth[i] < cfg.BranchMaxDepth }, RoleBranch, "")
		if id < 0 {
			break
		}
	}

	// 4. Place each lock's key in tier k (preferring a branch leaf, otherwise growing a key room in tier k).
	for k, edgeIdx := range lockEdges {
		lockID := g.Edges[edgeIdx].LockID
		keyNode := -1
		var leaves, branches []int
		for i := range g.Nodes {
			n := &g.Nodes[i]
			if n.Tier != k || n.Role != RoleBranch || n.KeyID != "" {
				continue
			}
			if degree[i] == 1 {
				leaves = append(leaves, i)
			} else {
				branches = append(branches, i)
			}
		}
		switch {
		case len(leaves) > 0:
			keyNode = random.Pick(s, leaves)
		case len(branches) > 0:
			keyNode = random.Pick(s, branches)
		default:
			keyNode = growIntoSlot(func(i int) bool { return g.Nodes[i].Tier == k }, RoleKey, "key")
			if keyNode < 0 {
				// Try earlier tiers if tier k has no free adjacent slot.
				for tt := k - 1; tt >= 0 && keyNode < 0; tt-- {
					keyNode = growIntoSlot(func(i int) bool { return g.Nodes[i].Tier == tt }, RoleKey, "key")
				}
			}
		}
		if keyNode < 0 {
			return nil
		}
		g.Nodes[keyNode].Role = RoleKey
		g.Nodes[keyNode].Tag = "key"
		g.Nodes[keyNode].KeyID = lockID
		g.Locks = append(g.Locks, LockKey{
			ID:        lockID,
			KeyNode:   keyNode,
			EdgeIndex: edgeIdx,
		})
	}

	// 5. Add up to cfg.Loops loop edges between 4-adjacent same-tier slots.
	if cfg.Loops > 0 {
		n := len(g.Nodes)
		connected := make([]bool, n*n)
		for _, e := range g.Edges {
			connected[e.From*n+e.To] = true
			connected[e.To*n+e.From] = true
		}
		type pair struct{ a, b int }
		var cands []pair
		for a := range n {
			if a == g.Goal {
				continue
			}
			for _, side := range [2]Side{SideEast, SideSouth} {
				d := side.Delta()
				np := Point{X: slots[a].X + d.X, Y: slots[a].Y + d.Y}
				if np.X >= cols || np.Y >= rows {
					continue
				}
				b := occ[np.Y*cols+np.X]
				if b < 0 || b == g.Goal || connected[a*n+b] {
					continue
				}
				if g.Nodes[a].Tier != g.Nodes[b].Tier {
					continue
				}
				cands = append(cands, pair{a: a, b: b})
			}
		}
		random.Shuffle(s, cands)
		added := 0
		for _, c := range cands {
			if added >= cfg.Loops {
				break
			}
			if degree[c.a] >= cfg.MaxDegree || degree[c.b] >= cfg.MaxDegree {
				continue
			}
			g.Edges = append(g.Edges, GraphEdge{From: c.a, To: c.b, Loop: true})
			degree[c.a]++
			degree[c.b]++
			added++
		}
	}

	computeBFSDepths(g)
	return assignGridTemplates(s, g, slots, variants, allTags, slotW, slotH, padding, passable)
}

// buildGridPath constructs a self-avoiding path of length target on a cols x rows grid starting at start, biasing toward horizontal and downward steps (Spelunky style) while allowing upward turns when needed.
func buildGridPath(s *random.Stream, cols, rows int, start Point, target int) []Point {
	visited := make([]bool, cols*rows)
	path := make([]Point, 0, target)
	visited[start.Y*cols+start.X] = true
	path = append(path, start)
	steps := 0
	maxSteps := max(target*64, 256)

	var dfs func(cur Point) bool
	dfs = func(cur Point) bool {
		if len(path) == target {
			return true
		}
		if steps >= maxSteps {
			return false
		}
		steps++

		// Prefer East, West, South before North for Spelunky-like downward flow.
		dirs := [4]Side{SideEast, SideWest, SideSouth, SideNorth}
		// Shuffle first 3 (E, W, S), then with 25% probability mix North in early.
		random.Shuffle(s, dirs[:3])
		if s.Intn(4) == 0 {
			random.Shuffle(s, dirs[:])
		}
		for _, side := range dirs {
			d := side.Delta()
			np := Point{X: cur.X + d.X, Y: cur.Y + d.Y}
			if np.X < 0 || np.Y < 0 || np.X >= cols || np.Y >= rows {
				continue
			}
			if visited[np.Y*cols+np.X] {
				continue
			}
			visited[np.Y*cols+np.X] = true
			path = append(path, np)
			if dfs(np) {
				return true
			}
			path = path[:len(path)-1]
			visited[np.Y*cols+np.X] = false
		}
		return false
	}
	if !dfs(start) {
		return nil
	}
	return path
}

// attemptPlaceGraphOnGrid embeds a pre-built LayoutGraph onto a cols x rows grid and assigns compatible templates.
func attemptPlaceGraphOnGrid(s *random.Stream, g *LayoutGraph, variants []roomVariant, allTags map[string]bool, cols, rows, slotW, slotH, padding int, passable func(uint32) bool) *RoomLevel {
	n := len(g.Nodes)
	adj := make([][]int, n)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}
	for i := range n {
		if len(adj[i]) > 4 {
			return nil
		}
	}
	order := make([]int, 0, n)
	parent := make([]int, n)
	seen := make([]bool, n)
	for i := range n {
		parent[i] = -1
	}
	seen[g.Start] = true
	order = append(order, g.Start)
	for head := 0; head < len(order); head++ {
		u := order[head]
		for _, v := range adj[u] {
			if !seen[v] {
				seen[v] = true
				parent[v] = u
				order = append(order, v)
			}
		}
	}
	if len(order) != n {
		return nil
	}

	slots := make([]Point, n)
	placed := make([]bool, n)
	occ := make([]int, cols*rows)
	for i := range occ {
		occ[i] = -1
	}
	startSlot := Point{X: s.Intn(cols), Y: s.Intn(rows)}
	slots[g.Start] = startSlot
	placed[g.Start] = true
	occ[startSlot.Y*cols+startSlot.X] = g.Start

	steps := 0
	maxSteps := max(n*96, 512)
	var dfs func(idx int) bool
	dfs = func(idx int) bool {
		if idx == n {
			return true
		}
		if steps >= maxSteps {
			return false
		}
		steps++
		u := order[idx]
		p := parent[u]
		dirs := allSides
		random.Shuffle(s, dirs[:])
		for _, side := range dirs {
			d := side.Delta()
			np := Point{X: slots[p].X + d.X, Y: slots[p].Y + d.Y}
			if np.X < 0 || np.Y < 0 || np.X >= cols || np.Y >= rows {
				continue
			}
			if occ[np.Y*cols+np.X] >= 0 {
				continue
			}
			// Check every already-placed neighbor is 4-adjacent to np.
			ok := true
			for _, nb := range adj[u] {
				if !placed[nb] {
					continue
				}
				if abs(slots[nb].X-np.X)+abs(slots[nb].Y-np.Y) != 1 {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			slots[u] = np
			placed[u] = true
			occ[np.Y*cols+np.X] = u
			if dfs(idx + 1) {
				return true
			}
			placed[u] = false
			occ[np.Y*cols+np.X] = -1
		}
		return false
	}
	if !dfs(1) {
		return nil
	}
	return assignGridTemplates(s, g, slots, variants, allTags, slotW, slotH, padding, passable)
}

func sideBetweenSlots(from, to Point) Side {
	switch {
	case to.X == from.X && to.Y == from.Y-1:
		return SideNorth
	case to.X == from.X && to.Y == from.Y+1:
		return SideSouth
	case to.X == from.X+1 && to.Y == from.Y:
		return SideEast
	case to.X == from.X-1 && to.Y == from.Y:
		return SideWest
	}
	return ""
}

// assignGridTemplates chooses compatible room templates and matching door sockets for every node placed at slots.
func assignGridTemplates(s *random.Stream, g *LayoutGraph, slots []Point, variants []roomVariant, allTags map[string]bool, slotW, slotH, padding int, passable func(uint32) bool) *RoomLevel {
	n := len(g.Nodes)
	type incident struct {
		edgeIdx int
		other   int
		side    Side
	}
	inc := make([][]incident, n)
	for ei, e := range g.Edges {
		sideF := sideBetweenSlots(slots[e.From], slots[e.To])
		if !sideF.Valid() {
			return nil
		}
		inc[e.From] = append(inc[e.From], incident{edgeIdx: ei, other: e.To, side: sideF})
		inc[e.To] = append(inc[e.To], incident{edgeIdx: ei, other: e.From, side: sideF.Opposite()})
	}

	variantIdx := make([]int, n)
	assigned := make([]bool, n)
	edgeDoors := make([][2]int, len(g.Edges))
	for i := range edgeDoors {
		edgeDoors[i] = [2]int{-1, -1}
	}

	// Assign templates in BFS order from Start so already-assigned neighbors constrain door offsets/channels.
	order := make([]int, 0, n)
	seen := make([]bool, n)
	seen[g.Start] = true
	order = append(order, g.Start)
	for head := 0; head < len(order); head++ {
		u := order[head]
		for _, in := range inc[u] {
			if !seen[in.other] {
				seen[in.other] = true
				order = append(order, in.other)
			}
		}
	}
	if len(order) != n {
		return nil
	}

	steps := 0
	maxSteps := max(n*64, 256)
	var assign func(idx int) bool
	assign = func(idx int) bool {
		if idx == n {
			return true
		}
		if steps >= maxSteps {
			return false
		}
		steps++

		u := order[idx]
		reqSides := inc[u]
		elig := eligibleVariants(variants, allTags, g.Nodes[u], len(reqSides))
		for _, vi := range weightedOrder(s, variants, elig) {
			v := &variants[vi]
			// Check v has a matching door for every incident edge (and matches already-assigned neighbors' chosen socket).
			chosen := make([]int, len(reqSides))
			used := make([]bool, len(v.doors))
			ok := true
			for k, in := range reqSides {
				var needSock *DoorSocket
				if assigned[in.other] {
					ov := &variants[variantIdx[in.other]]
					e := g.Edges[in.edgeIdx]
					otherDoorIdx := edgeDoors[in.edgeIdx][0]
					if e.To == in.other {
						otherDoorIdx = edgeDoors[in.edgeIdx][1]
					}
					if otherDoorIdx < 0 {
						ok = false
						break
					}
					sock := ov.doors[otherDoorIdx]
					needSock = &sock
				}
				found := -1
				for di, d := range v.doors {
					if used[di] || d.Side != in.side {
						continue
					}
					if needSock != nil {
						if !needSock.Matches(d) || needSock.Offset != d.Offset {
							continue
						}
					}
					found = di
					break
				}
				if found < 0 {
					ok = false
					break
				}
				used[found] = true
				chosen[k] = found
			}
			if !ok {
				continue
			}

			variantIdx[u] = vi
			assigned[u] = true
			prevDoors := make([][2]int, len(reqSides))
			for k, in := range reqSides {
				prevDoors[k] = edgeDoors[in.edgeIdx]
				e := g.Edges[in.edgeIdx]
				if e.From == u {
					edgeDoors[in.edgeIdx][0] = chosen[k]
				} else {
					edgeDoors[in.edgeIdx][1] = chosen[k]
				}
			}

			if assign(idx + 1) {
				return true
			}

			assigned[u] = false
			for k, in := range reqSides {
				edgeDoors[in.edgeIdx] = prevDoors[k]
			}
		}
		return false
	}
	if !assign(0) {
		return nil
	}

	bounds := make([]Rect, n)
	for i := range n {
		bounds[i] = Rect{
			X: slots[i].X * slotW,
			Y: slots[i].Y * slotH,
			W: slotW,
			H: slotH,
		}
	}
	return finalizeLevel(g, variants, variantIdx, bounds, slots, edgeDoors, padding, passable)
}

// finalizeLevel normalizes room bounds onto a Grid, stamps room tiles, opens connected doors, seals unused door sockets and verifies connectivity.
func finalizeLevel(g *LayoutGraph, variants []roomVariant, variantIdx []int, rawBounds []Rect, slots []Point, edgeDoors [][2]int, padding int, passable func(uint32) bool) *RoomLevel {
	n := len(g.Nodes)
	minX, minY := rawBounds[0].X, rawBounds[0].Y
	maxX, maxY := rawBounds[0].X+rawBounds[0].W, rawBounds[0].Y+rawBounds[0].H
	for i := 1; i < n; i++ {
		b := rawBounds[i]
		minX = min(minX, b.X)
		minY = min(minY, b.Y)
		maxX = max(maxX, b.X+b.W)
		maxY = max(maxY, b.Y+b.H)
	}
	shiftX := -minX + padding
	shiftY := -minY + padding
	gridW := (maxX - minX) + 2*padding
	gridH := (maxY - minY) + 2*padding
	if gridW <= 0 || gridH <= 0 || gridW > MaxSide || gridH > MaxSide || tooManyCells(gridW, gridH) {
		return nil
	}

	grid := NewGrid(gridW, gridH)
	rooms := make([]PlacedRoom, n)
	usedDoors := make([][]bool, n)
	for i := range n {
		v := &variants[variantIdx[i]]
		usedDoors[i] = make([]bool, len(v.doors))
		b := Rect{
			X: rawBounds[i].X + shiftX,
			Y: rawBounds[i].Y + shiftY,
			W: rawBounds[i].W,
			H: rawBounds[i].H,
		}
		// Stamp room tiles onto grid.
		for y := range v.h {
			for x := range v.w {
				grid.Set(b.X+x, b.Y+y, v.grid.At(x, y))
			}
		}
		var slot Point
		if slots != nil {
			slot = slots[i]
		}
		rooms[i] = PlacedRoom{
			NodeID:     i,
			TemplateID: v.tmpl.ID,
			Source:     v.tmpl.Source,
			Bounds:     b,
			GridSlot:   slot,
			Rotation:   v.rotation,
			Tags:       slices.Clone(v.tmpl.Tags),
			Role:       g.Nodes[i].Role,
			KeyID:      g.Nodes[i].KeyID,
		}
	}

	// Mark connected doors and collect their Grid cells.
	openCells := make(map[Point]bool)
	doors := make([]PlacedDoor, len(g.Edges))
	for ei, e := range g.Edges {
		fDoorIdx, tDoorIdx := edgeDoors[ei][0], edgeDoors[ei][1]
		if fDoorIdx < 0 || tDoorIdx < 0 {
			return nil
		}
		fv := &variants[variantIdx[e.From]]
		tv := &variants[variantIdx[e.To]]
		usedDoors[e.From][fDoorIdx] = true
		usedDoors[e.To][tDoorIdx] = true

		fSock := fv.doors[fDoorIdx]
		tSock := tv.doors[tDoorIdx]
		fb := rooms[e.From].Bounds
		tb := rooms[e.To].Bounds

		fCells := fSock.Cells(fb.W, fb.H)
		tCells := tSock.Cells(tb.W, tb.H)
		cells := make([]Point, 0, len(fCells)+len(tCells))
		for _, p := range fCells {
			gp := Point{X: fb.X + p.X, Y: fb.Y + p.Y}
			cells = append(cells, gp)
			openCells[gp] = true
		}
		for _, p := range tCells {
			gp := Point{X: tb.X + p.X, Y: tb.Y + p.Y}
			cells = append(cells, gp)
			openCells[gp] = true
		}
		doors[ei] = PlacedDoor{
			FromRoom: e.From,
			ToRoom:   e.To,
			FromSide: fSock.Side,
			ToSide:   tSock.Side,
			Cells:    cells,
			LockID:   e.LockID,
			Channel:  fSock.Channel,
		}
	}

	// Seal unused door sockets with Wall (unless that exact cell is also used by an open door, e.g. in 1-cell-thin templates).
	for i := range n {
		v := &variants[variantIdx[i]]
		b := rooms[i].Bounds
		for di, sock := range v.doors {
			if usedDoors[i][di] {
				continue
			}
			for _, p := range sock.Cells(b.W, b.H) {
				gp := Point{X: b.X + p.X, Y: b.Y + p.Y}
				if !openCells[gp] {
					grid.Set(gp.X, gp.Y, Wall)
				}
			}
		}
	}

	// Open connected door cells so rooms join across their shared boundary.
	for _, d := range doors {
		for _, gp := range d.Cells {
			if !passable(grid.At(gp.X, gp.Y)) {
				grid.Set(gp.X, gp.Y, Floor)
			}
		}
	}

	if grid.Count(passable) == 0 || !grid.Connected(passable) {
		return nil
	}
	return &RoomLevel{
		Grid:  grid,
		Rooms: rooms,
		Doors: doors,
		Graph: g,
	}
}
