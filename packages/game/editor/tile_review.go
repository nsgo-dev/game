package editor

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrNoTileReview is returned by tile review actions when no tile edit review is open.
var ErrNoTileReview = errors.New("editor: no tile edit review is open")

// TileCoord is a 0-based (Col, Row) cell coordinate on an inline tilemap.
type TileCoord struct {
	Col int `json:"col"`
	Row int `json:"row"`
}

// TileQueryOptions configures Editor.QueryTiles.
type TileQueryOptions struct {
	// Layer names the scene layer to query; empty uses ActiveLayer or the first layer with an inline tilemap.
	Layer string
	// TileLayer names the tile layer of a layered tilemap; empty uses ActiveTileLayer or its first tile layer.
	TileLayer string
	// TileIDs filters cells matching any of these tile IDs (0 = empty, >0 = tile ID as PaintTile takes it).
	TileIDs []int
	// TileNames filters cells matching any of these named tiles in the layer's YAML tileset.
	TileNames []string
	// EmptyOnly matches only empty cells (tile ID 0) when true.
	EmptyOnly bool
	// Rect, when non-nil, restricts the query to an inclusive sub-rectangle clamped to the map bounds.
	Rect *TileRect
	// MinCells filters out connected regions smaller than MinCells.
	MinCells int
	// Diagonal uses 8-connected neighbors instead of the default 4-connected grid neighbors.
	Diagonal bool
	// IncludeCells controls whether each region includes its explicit Cells list.
	// When nil, cells are included automatically when total matched cells <= 512.
	IncludeCells *bool
}

// TileQueryRegion is one connected region of matching cells found by Editor.QueryTiles.
type TileQueryRegion struct {
	Bounds      TileRect    `json:"bounds"`
	Width       int         `json:"width"`
	Height      int         `json:"height"`
	CellCount   int         `json:"cellCount"`
	Rectangular bool        `json:"rectangular"`
	TileIDs     []int       `json:"tileIds,omitempty"`
	TileNames   []string    `json:"tileNames,omitempty"`
	Summary     string      `json:"summary"`
	Cells       []TileCoord `json:"cells,omitempty"`
}

// TileQueryResult is what Editor.QueryTiles returns.
type TileQueryResult struct {
	Layer        string            `json:"layer"`
	TileLayer    string            `json:"tileLayer,omitempty"`
	Tileset      string            `json:"tileset,omitempty"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	TileWidth    int               `json:"tileWidth"`
	TileHeight   int               `json:"tileHeight"`
	MatchedCells int               `json:"matchedCells"`
	Regions      []TileQueryRegion `json:"regions"`
}

// AgentTileCell specifies one cell to paint in AgentTilePaintRequest.
type AgentTileCell struct {
	Col      int
	Row      int
	TileID   int
	TileName string
	Flip     uint32
}

// AgentTileRect specifies an inclusive rectangle to paint in AgentTilePaintRequest.
type AgentTileRect struct {
	Col0     int
	Row0     int
	Col1     int
	Row1     int
	TileID   int
	TileName string
	Flip     uint32
}

// AgentTileWeighted specifies one weighted tile entry for deterministic random picks in AgentTilePaintRequest.
type AgentTileWeighted struct {
	TileID   int
	TileName string
	Weight   float64
}

// AgentTilePaintRequest paints explicit cells, rectangles, or weighted random picks on a tile layer.
type AgentTilePaintRequest struct {
	Layer                string
	TileLayer            string
	TileID               int
	TileName             string
	Flip                 uint32
	Weighted             []AgentTileWeighted
	Seed                 uint64
	NoAdjacentDuplicates bool
	Cells                []AgentTileCell
	Rects                []AgentTileRect
	Label                string
	TrackReview          bool
}

// AgentTileAutotileRequest re-edges or places a terrain set on a tile layer using YAML tileset autotile rules.
type AgentTileAutotileRequest struct {
	Layer       string
	TileLayer   string
	Terrain     string
	Cells       []TileCoord
	Rects       []TileRect
	Place       *bool
	Label       string
	TrackReview bool
}

// AgentTileEraseRequest clears explicit cells or rectangles on a tile layer.
type AgentTileEraseRequest struct {
	Layer       string
	TileLayer   string
	Cells       []TileCoord
	Rects       []TileRect
	Label       string
	TrackReview bool
}

// AgentTileEditResult reports the outcome of an agent tile edit or review commit.
type AgentTileEditResult struct {
	Layer        string `json:"layer"`
	TileLayer    string `json:"tileLayer,omitempty"`
	ChangedCells int    `json:"changedCells"`
	UndoLabel    string `json:"undo,omitempty"`
	ReviewOpen   bool   `json:"reviewOpen"`
	RegionCount  int    `json:"regionCount,omitempty"`
	KeptRegions  int    `json:"keptRegions,omitempty"`
	ReviewCells  int    `json:"reviewCells,omitempty"`
}

// TileReviewKind classifies how a tile review region differs from the pre-review tilemap.
type TileReviewKind string

const (
	// TileReviewPainted marks cells that were empty before and now hold a tile.
	TileReviewPainted TileReviewKind = "painted"
	// TileReviewReplaced marks cells that held a tile before and now hold a different tile or flip.
	TileReviewReplaced TileReviewKind = "replaced"
	// TileReviewErased marks cells that held a tile before and are now empty.
	TileReviewErased TileReviewKind = "erased"
)

// TileReviewView selects whether the viewport shows the Before or After state during a tile review.
type TileReviewView int

const (
	TileReviewAfter TileReviewView = iota
	TileReviewBefore
)

// TileReviewCell is one changed cell belonging to a TileReviewRegion.
type TileReviewCell struct {
	Col    int   `json:"col"`
	Row    int   `json:"row"`
	OldGID int64 `json:"oldTile"`
	NewGID int64 `json:"newTile"`
}

// TileReviewRegion is one connected region of pending agent tile edits that can be reverted or restored independently.
type TileReviewRegion struct {
	ID       string           `json:"id"`
	Kind     TileReviewKind   `json:"kind"`
	Sign     string           `json:"sign"`
	Title    string           `json:"title"`
	Detail   string           `json:"detail"`
	Bounds   TileRect         `json:"bounds"`
	Cells    []TileReviewCell `json:"cells,omitempty"`
	Reverted bool             `json:"reverted"`
}

// TileReviewStep records one agent tile tool step inside the active tile review.
type TileReviewStep struct {
	Tool         string `json:"tool"`
	Summary      string `json:"summary"`
	ChangedCells int    `json:"changedCells"`
}

const (
	// maxAgentTileSpecs caps the total number of rects + cells entries accepted by a single agent tile tool call.
	maxAgentTileSpecs = 65536
	// maxAgentTileOps caps the total cell iterations/writes processed by a single agent tile tool call.
	maxAgentTileOps = 1 << 20
	// maxQueryIncludeCells caps explicit includeCells: true results in QueryTiles so MCP responses stay bounded.
	maxQueryIncludeCells = 16384
)

type tileReviewCellMeta struct {
	label       string
	terrainName string
	tool        string
}

type tileReviewEdge struct {
	c0, r0, c1, r1 int
}

type tileReviewState struct {
	layerName         string
	tileLayer         string
	width             int
	height            int
	base              []int64
	proposed          []int64
	cellMeta          map[int]tileReviewCellMeta
	steps             []TileReviewStep
	regions           []TileReviewRegion
	perimeters        [][]tileReviewEdge
	revertedByID      map[string]bool
	focusedID         string
	view              TileReviewView
	peekBefore        bool
	historyStart      int
	lastHistoryCursor int
	maxReviewCursor   int
	historyOwned      bool
	inHistoryStep     bool
	applying          bool
	uiRev             uint64
}

func (e *Editor) nextTileReviewUIRev() uint64 {
	if e == nil {
		return 0
	}
	e.tileReviewSeq++
	return e.tileReviewSeq
}

func (e *Editor) tileReviewUIRev() uint64 {
	if !e.TileReviewOpen() {
		return 0
	}
	return e.tileReview.uiRev
}

func (e *Editor) checkTileEditAllowed() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.StartScreenOpen() {
		return errors.New("editor: close the Start Screen before editing tiles")
	}
	if e.MergeConflictOpen() {
		return errors.New("editor: finish or abort the scene merge conflict before editing tiles")
	}
	if e.Doc == nil {
		return errors.New("editor: no scene document is open")
	}
	return nil
}

func validateTileGridDimensions(w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("editor: invalid tilemap dimensions %dx%d", w, h)
	}
	if int64(w) > maxTilemapCells || int64(h) > maxTilemapCells || int64(w)*int64(h) > maxTilemapCells {
		return fmt.Errorf("editor: tilemap size %dx%d exceeds %d cells", w, h, maxTilemapCells)
	}
	return nil
}

func (e *Editor) resolveTileGridWithLayer(layer, tileLayer string) (string, string, tileGrid, *tilemap.TilesetDoc, error) {
	layerName, tm, err := e.resolveInlineTilemap(layer)
	if err != nil {
		return "", "", tileGrid{}, nil, err
	}
	resolvedTileLayer := ""
	if tm.IsLayered() {
		if tileLayer != "" {
			if tm.TileLayer(tileLayer) == nil {
				return "", "", tileGrid{}, nil, fmt.Errorf("editor: tilemap has no tile layer %q", tileLayer)
			}
			resolvedTileLayer = tileLayer
		} else {
			resolvedTileLayer = e.tileLayerName(tm)
		}
	} else if tileLayer != "" {
		return "", "", tileGrid{}, nil, fmt.Errorf("editor: layer %q has a single-grid tilemap and no tile layer %q", layerName, tileLayer)
	}
	g, err := gridOf(tm, resolvedTileLayer)
	if err != nil {
		return "", "", tileGrid{}, nil, err
	}
	var tsDoc *tilemap.TilesetDoc
	if tm.HasTilesetFile() {
		tsDoc, _ = e.loadTilesetDoc(tm.TilesetFile)
	}
	return layerName, resolvedTileLayer, g, tsDoc, nil
}

func tileIDFromName(doc *tilemap.TilesetDoc, name string) (int, bool) {
	if doc == nil || name == "" {
		return 0, false
	}
	for _, t := range doc.Tiles {
		if t.Name == name {
			return t.ID + 1, true
		}
	}
	for i, frame := range doc.Frames {
		if frame == name {
			return i + 1, true
		}
	}
	return 0, false
}

func tileMetaForGID(doc *tilemap.TilesetDoc, layered bool, raw int64) (id int, name string, terrain string) {
	if raw <= 0 {
		return 0, "", ""
	}
	if layered {
		id = int(uint32(raw) & tilemap.TileIDMask)
		if doc != nil && id > 0 {
			localID := id - 1
			for _, t := range doc.Tiles {
				if t.ID == localID {
					return id, t.Name, t.Terrain
				}
			}
			if localID >= 0 && localID < len(doc.Frames) {
				return id, doc.Frames[localID], ""
			}
		}
		return id, "", ""
	}
	return int(raw), "", ""
}

func resolveTileSpec(doc *tilemap.TilesetDoc, g tileGrid, tileID int, tileName string) (int, error) {
	if tileName != "" {
		if doc == nil {
			return 0, fmt.Errorf("editor: tileName %q requires a YAML tileset", tileName)
		}
		id, ok := tileIDFromName(doc, tileName)
		if !ok {
			return 0, fmt.Errorf("editor: tileset has no tile named %q", tileName)
		}
		if tileID > 0 && tileID != id {
			return 0, fmt.Errorf("editor: tile %d conflicts with tileName %q (tile %d)", tileID, tileName, id)
		}
		return id, nil
	}
	if tileID < 0 {
		return 0, fmt.Errorf("editor: tileID %d must be non-negative", tileID)
	}
	if g.layer != nil && tileID > int(tilemap.TileIDMask) {
		return 0, fmt.Errorf("editor: tileID %d is above %d", tileID, tilemap.TileIDMask)
	}
	return tileID, nil
}

func tileValueForGrid(g tileGrid, tileID int, flip uint32) (int64, error) {
	if tileID < 0 {
		return 0, fmt.Errorf("editor: tileID %d must be non-negative", tileID)
	}
	if g.layer == nil || tileID == 0 {
		return int64(tileID), nil
	}
	if tileID > int(tilemap.TileIDMask) {
		return 0, fmt.Errorf("editor: tileID %d is above %d", tileID, tilemap.TileIDMask)
	}
	return int64(uint32(tileID) | (flip & tilemap.FlipMask)), nil
}

// ActiveTileLayerCaption returns the active layer's display name and tile grid dimensions
// (for example "World", "24 × 20 cells · 16 px") when the active layer has an inline tilemap.
func (e *Editor) ActiveTileLayerCaption() (layerName, dims string, ok bool) {
	if e == nil || e.Doc == nil {
		return "", "", false
	}
	li := e.layerIndex(e.ActiveLayer)
	if li < 0 {
		return "", "", false
	}
	tm := e.inlineTilemapForLayer(&e.Doc.Layers[li])
	if tm == nil || tm.Width <= 0 || tm.Height <= 0 {
		return "", "", false
	}
	layerName = e.Doc.Layers[li].Name
	if tm.TileWidth == tm.TileHeight {
		dims = fmt.Sprintf("%d × %d cells · %d px", tm.Width, tm.Height, tm.TileWidth)
	} else {
		dims = fmt.Sprintf("%d × %d cells · %d × %d px", tm.Width, tm.Height, tm.TileWidth, tm.TileHeight)
	}
	return layerName, dims, true
}

// QueryTiles finds matching cells and groups them into connected regions on an inline tilemap layer.
func (e *Editor) QueryTiles(opts TileQueryOptions) (TileQueryResult, error) {
	if e == nil {
		return TileQueryResult{}, errors.New("editor: nil editor")
	}
	layerName, tileLayer, g, tsDoc, err := e.resolveTileGridWithLayer(opts.Layer, opts.TileLayer)
	if err != nil {
		return TileQueryResult{}, err
	}
	w, h := g.tm.Width, g.tm.Height
	if err := validateTileGridDimensions(w, h); err != nil {
		return TileQueryResult{}, err
	}
	bounds := TileRect{Col0: 0, Row0: 0, Col1: w - 1, Row1: h - 1}
	if opts.Rect != nil {
		clamped, ok := clampTileRect(normalizeTileRect(opts.Rect.Col0, opts.Rect.Row0, opts.Rect.Col1, opts.Rect.Row1), w, h)
		if !ok {
			return TileQueryResult{}, fmt.Errorf("editor: query rect (%d,%d)-(%d,%d) is out of bounds (%dx%d)",
				opts.Rect.Col0, opts.Rect.Row0, opts.Rect.Col1, opts.Rect.Row1, w, h)
		}
		bounds = clamped
	}

	filterSet := make(map[int]bool)
	hasFilter := false
	if opts.EmptyOnly {
		filterSet[0] = true
		hasFilter = true
	}
	for _, id := range opts.TileIDs {
		if id < 0 {
			return TileQueryResult{}, fmt.Errorf("editor: query tile ID %d must be non-negative", id)
		}
		filterSet[id] = true
		hasFilter = true
	}
	for _, name := range opts.TileNames {
		if tsDoc == nil {
			return TileQueryResult{}, fmt.Errorf("editor: tileName %q requires a YAML tileset", name)
		}
		id, ok := tileIDFromName(tsDoc, name)
		if !ok {
			return TileQueryResult{}, fmt.Errorf("editor: tileset has no tile named %q", name)
		}
		filterSet[id] = true
		hasFilter = true
	}
	emptyQuery := hasFilter && len(filterSet) == 1 && filterSet[0]

	values := g.values()
	layered := g.layer != nil
	tileIDAt := func(idx int) int {
		v := values[idx]
		if v <= 0 {
			return 0
		}
		if layered {
			return int(uint32(v) & tilemap.TileIDMask)
		}
		return int(v)
	}
	matches := func(col, row int) bool {
		if col < bounds.Col0 || col > bounds.Col1 || row < bounds.Row0 || row > bounds.Row1 {
			return false
		}
		tid := tileIDAt(row*w + col)
		if !hasFilter {
			return tid > 0
		}
		return filterSet[tid]
	}

	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	if opts.Diagonal {
		dirs = append(dirs, [2]int{1, 1}, [2]int{1, -1}, [2]int{-1, 1}, [2]int{-1, -1})
	}

	visited := make([]bool, w*h)
	var regions []TileQueryRegion
	totalMatched := 0

	for row := bounds.Row0; row <= bounds.Row1; row++ {
		for col := bounds.Col0; col <= bounds.Col1; col++ {
			idx := row*w + col
			if visited[idx] || !matches(col, row) {
				continue
			}
			visited[idx] = true
			queue := []TileCoord{{Col: col, Row: row}}
			var cells []TileCoord
			rBounds := TileRect{Col0: col, Row0: row, Col1: col, Row1: row}
			idSet := make(map[int]bool)
			nameSet := make(map[string]bool)

			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				cells = append(cells, cur)
				if cur.Col < rBounds.Col0 {
					rBounds.Col0 = cur.Col
				}
				if cur.Col > rBounds.Col1 {
					rBounds.Col1 = cur.Col
				}
				if cur.Row < rBounds.Row0 {
					rBounds.Row0 = cur.Row
				}
				if cur.Row > rBounds.Row1 {
					rBounds.Row1 = cur.Row
				}
				raw := values[cur.Row*w+cur.Col]
				tid, tname, _ := tileMetaForGID(tsDoc, layered, raw)
				idSet[tid] = true
				if tname != "" {
					nameSet[tname] = true
				}
				for _, d := range dirs {
					nc, nr := cur.Col+d[0], cur.Row+d[1]
					if nc < bounds.Col0 || nc > bounds.Col1 || nr < bounds.Row0 || nr > bounds.Row1 {
						continue
					}
					nidx := nr*w + nc
					if visited[nidx] || !matches(nc, nr) {
						continue
					}
					visited[nidx] = true
					queue = append(queue, TileCoord{Col: nc, Row: nr})
				}
			}

			if opts.MinCells > 0 && len(cells) < opts.MinCells {
				continue
			}
			slices.SortFunc(cells, func(a, b TileCoord) int {
				if a.Row != b.Row {
					return a.Row - b.Row
				}
				return a.Col - b.Col
			})
			ids := make([]int, 0, len(idSet))
			for id := range idSet {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			names := make([]string, 0, len(nameSet))
			for name := range nameSet {
				names = append(names, name)
			}
			slices.Sort(names)

			rw, rh := rBounds.Width(), rBounds.Height()
			reg := TileQueryRegion{
				Bounds:      rBounds,
				Width:       rw,
				Height:      rh,
				CellCount:   len(cells),
				Rectangular: len(cells) == rw*rh,
				TileIDs:     ids,
				TileNames:   names,
				Summary:     formatQueryRegionSummary(rBounds, len(cells), emptyQuery),
				Cells:       cells,
			}
			regions = append(regions, reg)
			totalMatched += len(cells)
		}
	}

	slices.SortFunc(regions, func(a, b TileQueryRegion) int {
		if a.CellCount != b.CellCount {
			return b.CellCount - a.CellCount
		}
		if a.Bounds.Row0 != b.Bounds.Row0 {
			return a.Bounds.Row0 - b.Bounds.Row0
		}
		return a.Bounds.Col0 - b.Bounds.Col0
	})

	includeCells := totalMatched <= 512
	if opts.IncludeCells != nil {
		if *opts.IncludeCells && totalMatched > maxQueryIncludeCells {
			return TileQueryResult{}, fmt.Errorf("editor: query matched %d cells, exceeding includeCells limit (%d); omit includeCells or narrow rect", totalMatched, maxQueryIncludeCells)
		}
		includeCells = *opts.IncludeCells
	}
	if !includeCells {
		for i := range regions {
			regions[i].Cells = nil
		}
	}

	tileset := g.tm.Tileset
	if g.tm.HasTilesetFile() {
		tileset = g.tm.TilesetFile
	}
	return TileQueryResult{
		Layer:        layerName,
		TileLayer:    tileLayer,
		Tileset:      tileset,
		Width:        w,
		Height:       h,
		TileWidth:    g.tm.TileWidth,
		TileHeight:   g.tm.TileHeight,
		MatchedCells: totalMatched,
		Regions:      regions,
	}, nil
}

func formatQueryRegionSummary(r TileRect, count int, empty bool) string {
	w, h := r.Width(), r.Height()
	noun := "region"
	if empty {
		noun = "pocket"
	}
	article := "a"
	if startsWithVowelDigit(w) {
		article = "an"
	}
	colRange := fmt.Sprintf("column %d", r.Col0)
	if r.Col0 != r.Col1 {
		colRange = fmt.Sprintf("columns %d–%d", r.Col0, r.Col1)
	}
	rowRange := fmt.Sprintf("row %d", r.Row0)
	if r.Row0 != r.Row1 {
		rowRange = fmt.Sprintf("rows %d–%d", r.Row0, r.Row1)
	}
	if count == w*h {
		return fmt.Sprintf("%s %d × %d %s at %s, %s", article, w, h, noun, colRange, rowRange)
	}
	return fmt.Sprintf("%s %d × %d %s (%d cells) at %s, %s", article, w, h, noun, count, colRange, rowRange)
}

func startsWithVowelDigit(n int) bool {
	s := strconv.Itoa(n)
	if s == "11" || s == "18" || strings.HasPrefix(s, "8") {
		return true
	}
	return false
}

// AgentPaintTiles paints explicit cells, rectangles, or weighted random tile picks in one undoable step.
// When req.TrackReview is true, the changes are also tracked in the active region-based tile review.
func (e *Editor) AgentPaintTiles(req AgentTilePaintRequest) (AgentTileEditResult, error) {
	if err := e.checkTileEditAllowed(); err != nil {
		return AgentTileEditResult{}, err
	}
	layerName, tileLayer, g, tsDoc, err := e.resolveTileGridWithLayer(req.Layer, req.TileLayer)
	if err != nil {
		return AgentTileEditResult{}, err
	}
	w, h := g.tm.Width, g.tm.Height
	if err := validateTileGridDimensions(w, h); err != nil {
		return AgentTileEditResult{}, err
	}
	if err := e.checkLayerEditableForTiles(layerName); err != nil {
		return AgentTileEditResult{}, err
	}
	if len(req.Cells) == 0 && len(req.Rects) == 0 {
		return AgentTileEditResult{}, errors.New("editor: tile_paint needs at least one cell or rect")
	}
	if len(req.Cells)+len(req.Rects) > maxAgentTileSpecs {
		return AgentTileEditResult{}, fmt.Errorf("editor: tile_paint received %d rect/cell entries, exceeding limit (%d)", len(req.Cells)+len(req.Rects), maxAgentTileSpecs)
	}

	// Resolve default tile and optional weighted pool (explicitly rejecting negative TileIDs).
	if req.TileID < 0 {
		return AgentTileEditResult{}, fmt.Errorf("editor: tileID %d must be non-negative", req.TileID)
	}
	defaultTileID := 0
	if req.TileID > 0 || req.TileName != "" {
		defaultTileID, err = resolveTileSpec(tsDoc, g, req.TileID, req.TileName)
		if err != nil {
			return AgentTileEditResult{}, err
		}
	}
	var pool []WeightedTile
	if len(req.Weighted) > 0 {
		pool = make([]WeightedTile, 0, len(req.Weighted))
		totalWeight := 0.0
		for i, wt := range req.Weighted {
			if wt.TileID < 0 {
				return AgentTileEditResult{}, fmt.Errorf("weighted[%d]: editor: tileID %d must be non-negative", i, wt.TileID)
			}
			tid, err := resolveTileSpec(tsDoc, g, wt.TileID, wt.TileName)
			if err != nil {
				return AgentTileEditResult{}, fmt.Errorf("weighted[%d]: %w", i, err)
			}
			if tid <= 0 {
				return AgentTileEditResult{}, fmt.Errorf("editor: weighted[%d] tile ID must be positive", i)
			}
			if _, err := tileValueForGrid(g, tid, req.Flip); err != nil {
				return AgentTileEditResult{}, fmt.Errorf("weighted[%d]: %w", i, err)
			}
			weight := wt.Weight
			if weight == 0 {
				weight = 1
			}
			if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
				return AgentTileEditResult{}, fmt.Errorf("editor: weighted[%d] weight %v must be positive and finite", i, weight)
			}
			totalWeight += weight
			if math.IsNaN(totalWeight) || math.IsInf(totalWeight, 0) {
				return AgentTileEditResult{}, errors.New("editor: weighted tile total weight overflows float64")
			}
			pool = append(pool, WeightedTile{TileID: tid, Weight: weight})
		}
	}
	seed := req.Seed
	if seed == 0 {
		seed = 1
	}

	// Validate all rects, cells, tile specs, and total cell-op caps BEFORE mutating any open review state.
	type validatedRect struct {
		nr     TileRect
		tileID int
		flip   uint32
	}
	type validatedCell struct {
		col    int
		row    int
		tileID int
		flip   uint32
	}
	var totalOps int64
	vRects := make([]validatedRect, 0, len(req.Rects))
	for ri, r := range req.Rects {
		if r.TileID < 0 {
			return AgentTileEditResult{}, fmt.Errorf("rects[%d]: editor: tileID %d must be non-negative", ri, r.TileID)
		}
		nr := normalizeTileRect(r.Col0, r.Row0, r.Col1, r.Row1)
		if nr.Col0 < 0 || nr.Row0 < 0 || nr.Col1 >= w || nr.Row1 >= h {
			return AgentTileEditResult{}, fmt.Errorf("editor: rects[%d] (%d,%d)-(%d,%d) out of bounds (%dx%d)",
				ri, r.Col0, r.Row0, r.Col1, r.Row1, w, h)
		}
		totalOps += int64(nr.Width()) * int64(nr.Height())
		if totalOps > maxAgentTileOps {
			return AgentTileEditResult{}, fmt.Errorf("editor: tile_paint exceeds %d cell operations limit", maxAgentTileOps)
		}
		rectTile := defaultTileID
		if r.TileID > 0 || r.TileName != "" {
			rectTile, err = resolveTileSpec(tsDoc, g, r.TileID, r.TileName)
			if err != nil {
				return AgentTileEditResult{}, fmt.Errorf("rects[%d]: %w", ri, err)
			}
		}
		if rectTile <= 0 && len(pool) == 0 {
			return AgentTileEditResult{}, fmt.Errorf("editor: rects[%d] has no tile, tileName, or weighted pool", ri)
		}
		flip := req.Flip
		if r.Flip != 0 {
			flip = r.Flip
		}
		if rectTile > 0 {
			if _, err := tileValueForGrid(g, rectTile, flip); err != nil {
				return AgentTileEditResult{}, fmt.Errorf("rects[%d]: %w", ri, err)
			}
		}
		vRects = append(vRects, validatedRect{nr: nr, tileID: rectTile, flip: flip})
	}
	vCells := make([]validatedCell, 0, len(req.Cells))
	for ci, cell := range req.Cells {
		if cell.TileID < 0 {
			return AgentTileEditResult{}, fmt.Errorf("cells[%d]: editor: tileID %d must be non-negative", ci, cell.TileID)
		}
		if cell.Col < 0 || cell.Col >= w || cell.Row < 0 || cell.Row >= h {
			return AgentTileEditResult{}, fmt.Errorf("editor: cells[%d] (%d,%d) out of bounds (%dx%d)",
				ci, cell.Col, cell.Row, w, h)
		}
		totalOps++
		if totalOps > maxAgentTileOps {
			return AgentTileEditResult{}, fmt.Errorf("editor: tile_paint exceeds %d cell operations limit", maxAgentTileOps)
		}
		tid := defaultTileID
		if cell.TileID > 0 || cell.TileName != "" {
			tid, err = resolveTileSpec(tsDoc, g, cell.TileID, cell.TileName)
			if err != nil {
				return AgentTileEditResult{}, fmt.Errorf("cells[%d]: %w", ci, err)
			}
		}
		if tid <= 0 && len(pool) == 0 {
			return AgentTileEditResult{}, fmt.Errorf("editor: cells[%d] has no tile, tileName, or weighted pool", ci)
		}
		flip := req.Flip
		if cell.Flip != 0 {
			flip = cell.Flip
		}
		if tid > 0 {
			if _, err := tileValueForGrid(g, tid, flip); err != nil {
				return AgentTileEditResult{}, fmt.Errorf("cells[%d]: %w", ci, err)
			}
		}
		vCells = append(vCells, validatedCell{col: cell.Col, row: cell.Row, tileID: tid, flip: flip})
	}

	// Only now commit any active stroke and prepare the active review (restoring After or committing another layer's review).
	e.endTileStrokeForChange()
	if req.TrackReview {
		if err := e.prepareTileReviewForEdit(layerName, tileLayer); err != nil {
			return AgentTileEditResult{}, err
		}
	} else {
		e.exitTileReviewBeforePreview()
	}

	values := g.values()
	// planned tracks cell index -> desired value so later specs override earlier ones deterministically.
	planned := make(map[int]int64)
	var order []int
	recordCell := func(col, row int, val int64) {
		idx := row*w + col
		if _, ok := planned[idx]; !ok {
			order = append(order, idx)
		}
		planned[idx] = val
	}

	pickWeighted := func(col, row int) int {
		if !req.NoAdjacentDuplicates || len(pool) <= 1 {
			return randomPickTile(seed, col, row, pool)
		}
		// Avoid matching left or top neighbor (checking already-planned cells first, then existing map tiles).
		avoid := map[int]bool{}
		for _, off := range [2][2]int{{-1, 0}, {0, -1}} {
			nc, nr := col+off[0], row+off[1]
			if nc >= 0 && nc < w && nr >= 0 && nr < h {
				nidx := nr*w + nc
				neighborVal := values[nidx]
				if prev, ok := planned[nidx]; ok {
					neighborVal = prev
				}
				if neighborVal > 0 {
					tid, _, _ := tileMetaForGID(tsDoc, g.layer != nil, neighborVal)
					if tid > 0 {
						avoid[tid] = true
					}
				}
			}
		}
		filtered := make([]WeightedTile, 0, len(pool))
		for _, entry := range pool {
			if !avoid[entry.TileID] {
				filtered = append(filtered, entry)
			}
		}
		if len(filtered) == 0 {
			filtered = pool
		}
		return randomPickTile(seed, col, row, filtered)
	}

	for _, vr := range vRects {
		for row := vr.nr.Row0; row <= vr.nr.Row1; row++ {
			for col := vr.nr.Col0; col <= vr.nr.Col1; col++ {
				tid := vr.tileID
				if tid <= 0 {
					tid = pickWeighted(col, row)
				}
				val, err := tileValueForGrid(g, tid, vr.flip)
				if err != nil {
					return AgentTileEditResult{}, err
				}
				recordCell(col, row, val)
			}
		}
	}

	for _, vc := range vCells {
		tid := vc.tileID
		if tid <= 0 {
			tid = pickWeighted(vc.col, vc.row)
		}
		val, err := tileValueForGrid(g, tid, vc.flip)
		if err != nil {
			return AgentTileEditResult{}, err
		}
		recordCell(vc.col, vc.row, val)
	}

	var changes []tileChange
	for _, idx := range order {
		newVal := planned[idx]
		if values[idx] != newVal {
			changes = append(changes, tileChange{index: idx, oldGID: values[idx], newGID: newVal})
		}
	}
	if len(changes) == 0 {
		return e.agentTileResult(layerName, tileLayer, 0, ""), nil
	}

	label := strings.TrimSpace(req.Label)
	undoLabel := label
	if undoLabel == "" {
		undoLabel = fmt.Sprintf("Agent: paint %d tiles on %s", len(changes), layerName)
	}
	stepSummary := label
	if stepSummary == "" {
		stepSummary = fmt.Sprintf("Painted %d tiles", len(changes))
	}
	return e.commitAgentTileChanges(layerName, tileLayer, g, changes, undoLabel, TileReviewStep{
		Tool:         "tile_paint",
		Summary:      stepSummary,
		ChangedCells: len(changes),
	}, tileReviewCellMeta{
		label: label,
		tool:  "tile_paint",
	}, req.TrackReview)
}

// AgentAutotile re-edges a region (or places terrain and re-edges) using a terrain set from the layer's YAML tileset.
func (e *Editor) AgentAutotile(req AgentTileAutotileRequest) (AgentTileEditResult, error) {
	if err := e.checkTileEditAllowed(); err != nil {
		return AgentTileEditResult{}, err
	}
	layerName, tm, err := e.resolveInlineTilemap(req.Layer)
	if err != nil {
		return AgentTileEditResult{}, err
	}
	if !tm.IsLayered() {
		return AgentTileEditResult{}, fmt.Errorf("editor: layer %q has no layered tilemap with a YAML tileset", layerName)
	}
	tileLayer := req.TileLayer
	if tileLayer == "" {
		tileLayer = e.tileLayerName(tm)
	}
	g, err := gridOf(tm, tileLayer)
	if err != nil {
		return AgentTileEditResult{}, err
	}
	if err := e.checkLayerEditableForTiles(layerName); err != nil {
		return AgentTileEditResult{}, err
	}
	w, h := g.tm.Width, g.tm.Height
	if err := validateTileGridDimensions(w, h); err != nil {
		return AgentTileEditResult{}, err
	}
	if len(req.Rects)+len(req.Cells) > maxAgentTileSpecs {
		return AgentTileEditResult{}, fmt.Errorf("editor: tile_autotile exceeds %d target specs limit", maxAgentTileSpecs)
	}
	doc, err := e.loadTilesetDoc(tm.TilesetFile)
	if err != nil {
		return AgentTileEditResult{}, fmt.Errorf("editor: tileset %q: %w", tm.TilesetFile, err)
	}
	terrainName := req.Terrain
	if terrainName == "" {
		terrainName = e.ActiveTerrain
	}
	if terrainName == "" && len(doc.Terrains) > 0 {
		terrainName = doc.Terrains[0].Name
	}
	if terrainName == "" {
		return AgentTileEditResult{}, fmt.Errorf("editor: tileset %q defines no terrain sets", tm.TilesetFile)
	}
	terrain := doc.Terrain(terrainName, 1)
	if terrain == nil {
		return AgentTileEditResult{}, fmt.Errorf("editor: tileset %q has no terrain %q", tm.TilesetFile, terrainName)
	}
	if terrain.Autotile.Default == 0 && len(terrain.Autotile.Tiles) == 0 {
		return AgentTileEditResult{}, fmt.Errorf("editor: terrain %q has no tiles mapped", terrainName)
	}

	// Validate all rects, cells, and total op limits BEFORE mutating any active review state.
	var totalOps int64
	normRects := make([]TileRect, 0, len(req.Rects))
	for ri, r := range req.Rects {
		nr := normalizeTileRect(r.Col0, r.Row0, r.Col1, r.Row1)
		if nr.Col0 < 0 || nr.Row0 < 0 || nr.Col1 >= w || nr.Row1 >= h {
			return AgentTileEditResult{}, fmt.Errorf("editor: rects[%d] (%d,%d)-(%d,%d) out of bounds (%dx%d)",
				ri, r.Col0, r.Row0, r.Col1, r.Row1, w, h)
		}
		totalOps += int64(nr.Width()) * int64(nr.Height())
		if totalOps > maxAgentTileOps {
			return AgentTileEditResult{}, fmt.Errorf("editor: tile_autotile exceeds %d cell operations limit", maxAgentTileOps)
		}
		normRects = append(normRects, nr)
	}
	for ci, cell := range req.Cells {
		if cell.Col < 0 || cell.Col >= w || cell.Row < 0 || cell.Row >= h {
			return AgentTileEditResult{}, fmt.Errorf("editor: cells[%d] (%d,%d) out of bounds (%dx%d)",
				ci, cell.Col, cell.Row, w, h)
		}
		totalOps++
		if totalOps > maxAgentTileOps {
			return AgentTileEditResult{}, fmt.Errorf("editor: tile_autotile exceeds %d cell operations limit", maxAgentTileOps)
		}
	}

	e.endTileStrokeForChange()
	if req.TrackReview {
		if err := e.prepareTileReviewForEdit(layerName, tileLayer); err != nil {
			return AgentTileEditResult{}, err
		}
	} else {
		e.exitTileReviewBeforePreview()
	}

	values := g.values()
	targetSet := make(map[int]bool)
	hasExplicitTargets := len(req.Cells) > 0 || len(req.Rects) > 0
	for _, nr := range normRects {
		for row := nr.Row0; row <= nr.Row1; row++ {
			for col := nr.Col0; col <= nr.Col1; col++ {
				targetSet[row*w+col] = true
			}
		}
	}
	for _, cell := range req.Cells {
		targetSet[cell.Row*w+cell.Col] = true
	}

	placeMode := false
	if req.Place != nil {
		placeMode = *req.Place
	} else if hasExplicitTargets {
		// Default heuristic: if no target cell currently belongs to this terrain, placing terrain into targetSet
		// converts those cells to the terrain and computes edge masks; if any already belongs to terrain,
		// re-edge the existing terrain cells inside targetSet.
		hasTerrainInTarget := false
		for idx := range targetSet {
			if terrain.Contains(cellGID(values[idx])) {
				hasTerrainInTarget = true
				break
			}
		}
		placeMode = !hasTerrainInTarget
	}

	// Build membership grid after placement.
	member := make([]bool, w*h)
	for i, v := range values {
		member[i] = terrain.Contains(cellGID(v))
	}
	evalSet := make(map[int]bool)
	if !hasExplicitTargets {
		for i, m := range member {
			if m {
				evalSet[i] = true
			}
		}
	} else if placeMode {
		for idx := range targetSet {
			member[idx] = true
			evalSet[idx] = true
			col, row := idx%w, idx/w
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nc, nr := col+dx, row+dy
					if nc >= 0 && nc < w && nr >= 0 && nr < h && member[nr*w+nc] {
						evalSet[nr*w+nc] = true
					}
				}
			}
		}
	} else {
		for idx := range targetSet {
			if member[idx] {
				evalSet[idx] = true
			}
		}
	}

	match := func(c, r int) bool {
		if c < 0 || r < 0 || c >= w || r >= h {
			return false
		}
		return member[r*w+c]
	}

	evalIndices := make([]int, 0, len(evalSet))
	for idx := range evalSet {
		evalIndices = append(evalIndices, idx)
	}
	slices.Sort(evalIndices)

	var changes []tileChange
	for _, idx := range evalIndices {
		col, row := idx%w, idx/w
		mask := terrain.Autotile.ComputeMask(match, col, row)
		newGID := int64(terrain.Autotile.Tile(mask))
		if values[idx] != newGID {
			changes = append(changes, tileChange{index: idx, oldGID: values[idx], newGID: newGID})
		}
	}
	if len(changes) == 0 {
		return e.agentTileResult(layerName, tileLayer, 0, ""), nil
	}

	label := strings.TrimSpace(req.Label)
	undoLabel := label
	if undoLabel == "" {
		undoLabel = fmt.Sprintf("Agent: autotile %q (%d tiles) on %s", terrainName, len(changes), layerName)
	}
	stepSummary := label
	if stepSummary == "" {
		stepSummary = fmt.Sprintf("Re-edged %d tiles with %s autotile", len(changes), terrainName)
	}
	return e.commitAgentTileChanges(layerName, tileLayer, g, changes, undoLabel, TileReviewStep{
		Tool:         "tile_autotile",
		Summary:      stepSummary,
		ChangedCells: len(changes),
	}, tileReviewCellMeta{
		label:       label,
		terrainName: terrainName,
		tool:        "tile_autotile",
	}, req.TrackReview)
}

// AgentEraseTiles clears explicit cells or rectangles on a tile layer in one undoable step.
func (e *Editor) AgentEraseTiles(req AgentTileEraseRequest) (AgentTileEditResult, error) {
	if err := e.checkTileEditAllowed(); err != nil {
		return AgentTileEditResult{}, err
	}
	layerName, tileLayer, g, _, err := e.resolveTileGridWithLayer(req.Layer, req.TileLayer)
	if err != nil {
		return AgentTileEditResult{}, err
	}
	if err := e.checkLayerEditableForTiles(layerName); err != nil {
		return AgentTileEditResult{}, err
	}
	if len(req.Cells) == 0 && len(req.Rects) == 0 {
		return AgentTileEditResult{}, errors.New("editor: tile_erase needs at least one cell or rect")
	}
	w, h := g.tm.Width, g.tm.Height
	if err := validateTileGridDimensions(w, h); err != nil {
		return AgentTileEditResult{}, err
	}
	if len(req.Rects)+len(req.Cells) > maxAgentTileSpecs {
		return AgentTileEditResult{}, fmt.Errorf("editor: tile_erase exceeds %d target specs limit", maxAgentTileSpecs)
	}

	// Validate all rects, cells, and total op limits BEFORE mutating any active review state.
	var totalOps int64
	normRects := make([]TileRect, 0, len(req.Rects))
	for ri, r := range req.Rects {
		nr := normalizeTileRect(r.Col0, r.Row0, r.Col1, r.Row1)
		if nr.Col0 < 0 || nr.Row0 < 0 || nr.Col1 >= w || nr.Row1 >= h {
			return AgentTileEditResult{}, fmt.Errorf("editor: rects[%d] (%d,%d)-(%d,%d) out of bounds (%dx%d)",
				ri, r.Col0, r.Row0, r.Col1, r.Row1, w, h)
		}
		totalOps += int64(nr.Width()) * int64(nr.Height())
		if totalOps > maxAgentTileOps {
			return AgentTileEditResult{}, fmt.Errorf("editor: tile_erase exceeds %d cell operations limit", maxAgentTileOps)
		}
		normRects = append(normRects, nr)
	}
	for ci, cell := range req.Cells {
		if cell.Col < 0 || cell.Col >= w || cell.Row < 0 || cell.Row >= h {
			return AgentTileEditResult{}, fmt.Errorf("editor: cells[%d] (%d,%d) out of bounds (%dx%d)",
				ci, cell.Col, cell.Row, w, h)
		}
		totalOps++
		if totalOps > maxAgentTileOps {
			return AgentTileEditResult{}, fmt.Errorf("editor: tile_erase exceeds %d cell operations limit", maxAgentTileOps)
		}
	}

	e.endTileStrokeForChange()
	if req.TrackReview {
		if err := e.prepareTileReviewForEdit(layerName, tileLayer); err != nil {
			return AgentTileEditResult{}, err
		}
	} else {
		e.exitTileReviewBeforePreview()
	}

	values := g.values()
	seen := make(map[int]bool)
	var order []int
	addTarget := func(col, row int) {
		idx := row*w + col
		if !seen[idx] {
			seen[idx] = true
			order = append(order, idx)
		}
	}
	for _, nr := range normRects {
		for row := nr.Row0; row <= nr.Row1; row++ {
			for col := nr.Col0; col <= nr.Col1; col++ {
				addTarget(col, row)
			}
		}
	}
	for _, cell := range req.Cells {
		addTarget(cell.Col, cell.Row)
	}

	var changes []tileChange
	for _, idx := range order {
		if values[idx] != 0 {
			changes = append(changes, tileChange{index: idx, oldGID: values[idx], newGID: 0})
		}
	}
	if len(changes) == 0 {
		return e.agentTileResult(layerName, tileLayer, 0, ""), nil
	}

	label := strings.TrimSpace(req.Label)
	undoLabel := label
	if undoLabel == "" {
		undoLabel = fmt.Sprintf("Agent: erase %d tiles on %s", len(changes), layerName)
	}
	stepSummary := label
	if stepSummary == "" {
		stepSummary = fmt.Sprintf("Erased %d tiles", len(changes))
	}
	return e.commitAgentTileChanges(layerName, tileLayer, g, changes, undoLabel, TileReviewStep{
		Tool:         "tile_erase",
		Summary:      stepSummary,
		ChangedCells: len(changes),
	}, tileReviewCellMeta{
		label: label,
		tool:  "tile_erase",
	}, req.TrackReview)
}

func (e *Editor) prepareTileReviewForEdit(layerName, tileLayer string) error {
	if e.tileReview == nil {
		return nil
	}
	// If an active review targets another layer/sub-layer, commit it cleanly first.
	if e.tileReview.layerName != layerName || e.tileReview.tileLayer != tileLayer {
		if _, err := e.KeepTileReview(); err != nil {
			return err
		}
		return nil
	}
	// If the user was peeking Before, restore After display before applying new agent edits.
	e.exitTileReviewBeforePreview()
	return nil
}

func (e *Editor) commitAgentTileChanges(
	layerName, tileLayer string,
	g tileGrid,
	changes []tileChange,
	undoLabel string,
	step TileReviewStep,
	meta tileReviewCellMeta,
	trackReview bool,
) (AgentTileEditResult, error) {
	if e.History == nil {
		e.History = NewHistory()
	}
	if !trackReview {
		e.History.Execute(&tileEditCmd{
			ed:        e,
			layerName: layerName,
			tileLayer: tileLayer,
			label:     undoLabel,
			changes:   changes,
		})
		return e.agentTileResult(layerName, tileLayer, len(changes), undoLabel), nil
	}

	rev := e.tileReview
	if rev == nil {
		base := g.values()
		rev = &tileReviewState{
			layerName:         layerName,
			tileLayer:         tileLayer,
			width:             g.tm.Width,
			height:            g.tm.Height,
			base:              base,
			proposed:          slices.Clone(base),
			cellMeta:          make(map[int]tileReviewCellMeta),
			revertedByID:      make(map[string]bool),
			view:              TileReviewAfter,
			historyStart:      e.History.Index(),
			lastHistoryCursor: e.History.Index(),
			maxReviewCursor:   e.History.Index(),
			historyOwned:      true,
			uiRev:             e.nextTileReviewUIRev(),
		}
		e.tileReview = rev
		e.cancelAutoSaveTimer()
	}

	rev.applying = true
	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		tileLayer: tileLayer,
		label:     undoLabel,
		changes:   changes,
	})
	rev.lastHistoryCursor = e.History.Index()
	if rev.historyOwned {
		rev.maxReviewCursor = rev.lastHistoryCursor
	}

	for _, ch := range changes {
		if ch.index >= 0 && ch.index < len(rev.proposed) {
			rev.proposed[ch.index] = ch.newGID
			rev.cellMeta[ch.index] = meta
		}
	}
	rev.steps = append(rev.steps, step)
	e.recomputeTileReviewRegions()
	rev.applying = false

	if len(rev.regions) == 0 {
		e.tileReview = nil
	} else {
		// Ensure live grid reflects composed state (preserving any previously reverted regions).
		e.applyTileReviewDisplay()
		e.cancelAutoSaveTimer()
	}
	e.notify()
	return e.agentTileResult(layerName, tileLayer, len(changes), undoLabel), nil
}

func (e *Editor) agentTileResult(layerName, tileLayer string, changed int, undoLabel string) AgentTileEditResult {
	res := AgentTileEditResult{
		Layer:        layerName,
		TileLayer:    tileLayer,
		ChangedCells: changed,
		UndoLabel:    undoLabel,
		ReviewOpen:   e.TileReviewOpen(),
	}
	if e.tileReview != nil {
		kept, total := e.TileReviewKeptCount()
		res.RegionCount = total
		res.KeptRegions = kept
		res.ReviewCells = e.TileReviewTotalCells()
	}
	return res
}

func (e *Editor) recomputeTileReviewRegions() {
	rev := e.tileReview
	if rev == nil {
		return
	}
	_, _, g, tsDoc, err := e.resolveTileGridWithLayer(rev.layerName, rev.tileLayer)
	if err != nil {
		return
	}
	w, h := rev.width, rev.height
	if err := validateTileGridDimensions(w, h); err != nil || len(rev.base) < w*h || len(rev.proposed) < w*h {
		rev.regions = nil
		rev.perimeters = nil
		rev.uiRev = e.nextTileReviewUIRev()
		return
	}
	layered := g.layer != nil
	kindAt := make([]TileReviewKind, w*h)
	for i := range w * h {
		oldV, newV := rev.base[i], rev.proposed[i]
		if oldV == newV {
			continue
		}
		switch {
		case oldV == 0 && newV != 0:
			kindAt[i] = TileReviewPainted
		case oldV != 0 && newV == 0:
			kindAt[i] = TileReviewErased
		default:
			kindAt[i] = TileReviewReplaced
		}
	}

	// 8-connected components grouped by TileReviewKind so diagonal autotile corners stay in one region,
	// while disjoint edits or different edit kinds remain distinct regions.
	dirs := [8][2]int{
		{1, 0}, {-1, 0}, {0, 1}, {0, -1},
		{1, 1}, {1, -1}, {-1, 1}, {-1, -1},
	}
	visited := make([]bool, w*h)
	type regionWithPerim struct {
		region TileReviewRegion
		perim  []tileReviewEdge
	}
	var items []regionWithPerim

	for row := range h {
		for col := range w {
			idx := row*w + col
			kind := kindAt[idx]
			if kind == "" || visited[idx] {
				continue
			}
			visited[idx] = true
			queue := []int{idx}
			var cells []TileReviewCell
			cellSet := make(map[int]bool)
			bounds := TileRect{Col0: col, Row0: row, Col1: col, Row1: row}
			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				cc, cr := cur%w, cur/w
				cellSet[cur] = true
				cells = append(cells, TileReviewCell{
					Col:    cc,
					Row:    cr,
					OldGID: rev.base[cur],
					NewGID: rev.proposed[cur],
				})
				if cc < bounds.Col0 {
					bounds.Col0 = cc
				}
				if cc > bounds.Col1 {
					bounds.Col1 = cc
				}
				if cr < bounds.Row0 {
					bounds.Row0 = cr
				}
				if cr > bounds.Row1 {
					bounds.Row1 = cr
				}
				for _, d := range dirs {
					nc, nr := cc+d[0], cr+d[1]
					if nc < 0 || nc >= w || nr < 0 || nr >= h {
						continue
					}
					nidx := nr*w + nc
					if visited[nidx] || kindAt[nidx] != kind {
						continue
					}
					visited[nidx] = true
					queue = append(queue, nidx)
				}
			}
			slices.SortFunc(cells, func(a, b TileReviewCell) int {
				if a.Row != b.Row {
					return a.Row - b.Row
				}
				return a.Col - b.Col
			})
			hasCell := func(cc, cr int) bool {
				if cc < 0 || cc >= w || cr < 0 || cr >= h {
					return false
				}
				return cellSet[cr*w+cc]
			}
			perim := make([]tileReviewEdge, 0, len(cells)*2)
			for _, c := range cells {
				if !hasCell(c.Col, c.Row-1) {
					perim = append(perim, tileReviewEdge{c0: c.Col, r0: c.Row, c1: c.Col + 1, r1: c.Row})
				}
				if !hasCell(c.Col, c.Row+1) {
					perim = append(perim, tileReviewEdge{c0: c.Col, r0: c.Row + 1, c1: c.Col + 1, r1: c.Row + 1})
				}
				if !hasCell(c.Col-1, c.Row) {
					perim = append(perim, tileReviewEdge{c0: c.Col, r0: c.Row, c1: c.Col, r1: c.Row + 1})
				}
				if !hasCell(c.Col+1, c.Row) {
					perim = append(perim, tileReviewEdge{c0: c.Col + 1, r0: c.Row, c1: c.Col + 1, r1: c.Row + 1})
				}
			}
			id := fmt.Sprintf("%s_%d_%d", kind, cells[0].Col, cells[0].Row)
			title, detail := describeTileReviewRegion(kind, bounds, cells, rev.cellMeta, w, tsDoc, layered)
			sign := "+"
			switch kind {
			case TileReviewReplaced:
				sign = "~"
			case TileReviewErased:
				sign = "-"
			}
			items = append(items, regionWithPerim{
				region: TileReviewRegion{
					ID:       id,
					Kind:     kind,
					Sign:     sign,
					Title:    title,
					Detail:   detail,
					Bounds:   bounds,
					Cells:    cells,
					Reverted: rev.revertedByID[id],
				},
				perim: perim,
			})
		}
	}

	kindOrder := func(k TileReviewKind) int {
		switch k {
		case TileReviewPainted:
			return 0
		case TileReviewReplaced:
			return 1
		case TileReviewErased:
			return 2
		default:
			return 3
		}
	}
	slices.SortFunc(items, func(a, b regionWithPerim) int {
		if ka, kb := kindOrder(a.region.Kind), kindOrder(b.region.Kind); ka != kb {
			return ka - kb
		}
		if a.region.Bounds.Row0 != b.region.Bounds.Row0 {
			return a.region.Bounds.Row0 - b.region.Bounds.Row0
		}
		if a.region.Bounds.Col0 != b.region.Bounds.Col0 {
			return a.region.Bounds.Col0 - b.region.Bounds.Col0
		}
		return strings.Compare(a.region.ID, b.region.ID)
	})

	regions := make([]TileReviewRegion, len(items))
	perimeters := make([][]tileReviewEdge, len(items))
	for i, it := range items {
		regions[i] = it.region
		perimeters[i] = it.perim
	}

	rev.regions = regions
	rev.perimeters = perimeters
	// Prune stale reverted flags and keep focusedID valid.
	validIDs := make(map[string]bool, len(regions))
	for _, r := range regions {
		validIDs[r.ID] = true
	}
	for id := range rev.revertedByID {
		if !validIDs[id] {
			delete(rev.revertedByID, id)
		}
	}
	if !validIDs[rev.focusedID] {
		if len(regions) > 0 {
			rev.focusedID = regions[0].ID
		} else {
			rev.focusedID = ""
		}
	}
	rev.uiRev = e.nextTileReviewUIRev()
}

func describeTileReviewRegion(
	kind TileReviewKind,
	bounds TileRect,
	cells []TileReviewCell,
	meta map[int]tileReviewCellMeta,
	w int,
	tsDoc *tilemap.TilesetDoc,
	layered bool,
) (title, detail string) {
	labels := make(map[string]bool)
	stepTerrains := make(map[string]bool)
	newIDs := make(map[int]bool)
	newNames := make(map[string]bool)
	newTerrains := make(map[string]bool)
	oldNames := make(map[string]bool)
	oldTerrains := make(map[string]bool)

	for _, c := range cells {
		idx := c.Row*w + c.Col
		if m, ok := meta[idx]; ok {
			if m.label != "" {
				labels[m.label] = true
			}
			if m.terrainName != "" {
				stepTerrains[m.terrainName] = true
			}
		}
		if nid, nname, nterr := tileMetaForGID(tsDoc, layered, c.NewGID); nid > 0 {
			newIDs[nid] = true
			if nname != "" {
				newNames[nname] = true
			}
			if nterr != "" {
				newTerrains[nterr] = true
			}
		}
		if oid, oname, oterr := tileMetaForGID(tsDoc, layered, c.OldGID); oid > 0 {
			if oname != "" {
				oldNames[oname] = true
			}
			if oterr != "" {
				oldTerrains[oterr] = true
			}
		}
	}

	singleOf := func(m map[string]bool) (string, bool) {
		if len(m) != 1 {
			return "", false
		}
		for k := range m {
			return k, true
		}
		return "", false
	}

	coordSpan := formatBoundsBrief(bounds)

	// Title
	if lbl, ok := singleOf(labels); ok {
		title = lbl
	} else {
		switch kind {
		case TileReviewPainted:
			if terr, ok := singleOf(newTerrains); ok {
				title = "Painted " + terr
			} else if name, ok := singleOf(newNames); ok {
				title = "Painted " + name
			} else {
				title = "Painted tiles"
			}
		case TileReviewReplaced:
			if terr, ok := singleOf(stepTerrains); ok {
				title = "Autotiled " + terr
			} else if terr, ok := singleOf(newTerrains); ok {
				title = "Replaced with " + terr
			} else {
				title = "Replaced tiles"
			}
		case TileReviewErased:
			if name, ok := singleOf(oldNames); ok {
				title = "Erased " + name
			} else {
				title = "Erased tiles"
			}
		}
	}

	// Detail
	switch kind {
	case TileReviewPainted:
		if len(newIDs) > 1 {
			if terr, ok := singleOf(newTerrains); ok {
				detail = fmt.Sprintf("from %d %s tiles", len(newIDs), terr)
			} else {
				detail = fmt.Sprintf("from %d tiles · %s", len(newIDs), coordSpan)
			}
		} else if name, ok := singleOf(newNames); ok {
			detail = fmt.Sprintf("%s · %s", name, coordSpan)
		} else {
			detail = coordSpan
		}
	case TileReviewReplaced:
		if terr, ok := singleOf(stepTerrains); ok {
			if oldName, ok := singleOf(oldNames); ok {
				detail = fmt.Sprintf("%s → %s autotile", oldName, terr)
			} else if oldTerr, ok := singleOf(oldTerrains); ok && oldTerr != terr {
				detail = fmt.Sprintf("%s → %s autotile", oldTerr, terr)
			} else {
				detail = fmt.Sprintf("re-edged with %s autotile", terr)
			}
		} else if len(newIDs) > 1 {
			if terr, ok := singleOf(newTerrains); ok {
				detail = fmt.Sprintf("from %d %s tiles", len(newIDs), terr)
			} else {
				detail = fmt.Sprintf("replaced with %d tiles · %s", len(newIDs), coordSpan)
			}
		} else if name, ok := singleOf(newNames); ok {
			detail = fmt.Sprintf("→ %s · %s", name, coordSpan)
		} else {
			detail = coordSpan
		}
	case TileReviewErased:
		if name, ok := singleOf(oldNames); ok {
			detail = fmt.Sprintf("cleared %s · %s", name, coordSpan)
		} else {
			detail = fmt.Sprintf("cleared · %s", coordSpan)
		}
	}
	return title, detail
}

func formatBoundsBrief(r TileRect) string {
	cols := fmt.Sprintf("col %d", r.Col0)
	if r.Col0 != r.Col1 {
		cols = fmt.Sprintf("cols %d–%d", r.Col0, r.Col1)
	}
	rows := fmt.Sprintf("row %d", r.Row0)
	if r.Row0 != r.Row1 {
		rows = fmt.Sprintf("rows %d–%d", r.Row0, r.Row1)
	}
	return cols + ", " + rows
}

func composeTileReviewValues(rev *tileReviewState) []int64 {
	if rev == nil {
		return nil
	}
	out := slices.Clone(rev.base)
	for _, r := range rev.regions {
		if r.Reverted {
			continue
		}
		for _, c := range r.Cells {
			idx := c.Row*rev.width + c.Col
			if idx >= 0 && idx < len(out) {
				out[idx] = c.NewGID
			}
		}
	}
	return out
}

func (e *Editor) applyTileReviewDisplay() {
	rev := e.tileReview
	if e == nil || rev == nil {
		return
	}
	_, _, g, _, err := e.resolveTileGridWithLayer(rev.layerName, rev.tileLayer)
	if err != nil {
		return
	}
	showingBefore := e.TileReviewShowingBefore()
	cur := g.values()
	var changes []tileChange
	for _, r := range rev.regions {
		wantNew := !showingBefore && !r.Reverted
		for _, c := range r.Cells {
			idx := c.Row*rev.width + c.Col
			if idx < 0 || idx >= len(cur) {
				continue
			}
			target := c.OldGID
			if wantNew {
				target = c.NewGID
			}
			// When non-review commands were interleaved, skip cells the user manually overwrote
			// to a value outside the review's OldGID/NewGID pair.
			if !rev.historyOwned && cur[idx] != c.OldGID && cur[idx] != c.NewGID {
				continue
			}
			if cur[idx] != target {
				changes = append(changes, tileChange{index: idx, oldGID: cur[idx], newGID: target})
			}
		}
	}
	if len(changes) > 0 {
		g.apply(changes, false)
		e.Rebuild()
	}
}

// exitTileReviewBeforePreview restores the live tilemap to After view (keeping any per-region reverts)
// whenever Before/peek preview is active, so Save, Play, tab switch, or manual edits never observe or
// persist the temporary Before display.
func (e *Editor) exitTileReviewBeforePreview() {
	if e == nil || e.tileReview == nil {
		return
	}
	rev := e.tileReview
	if rev.view != TileReviewBefore && !rev.peekBefore {
		return
	}
	rev.view = TileReviewAfter
	rev.peekBefore = false
	rev.uiRev = e.nextTileReviewUIRev()
	e.applyTileReviewDisplay()
}

// prepareTileReviewForHistoryStep synchronizes the live tilemap with History's expected state right
// before History.undo()/redo() executes so Before view or visually reverted regions never corrupt
// History commands or rev.proposed.
func (e *Editor) prepareTileReviewForHistoryStep() {
	if e == nil || e.tileReview == nil {
		return
	}
	rev := e.tileReview
	rev.inHistoryStep = true
	if !rev.historyOwned {
		e.exitTileReviewBeforePreview()
		return
	}
	rev.view = TileReviewAfter
	rev.peekBefore = false
	_, _, g, _, err := e.resolveTileGridWithLayer(rev.layerName, rev.tileLayer)
	if err != nil {
		return
	}
	cur := g.values()
	var changes []tileChange
	for i := range min(len(cur), len(rev.proposed)) {
		if cur[i] != rev.proposed[i] {
			changes = append(changes, tileChange{index: i, oldGID: cur[i], newGID: rev.proposed[i]})
		}
	}
	if len(changes) > 0 {
		g.apply(changes, false)
	}
}

// finishTileReviewHistoryStep clears the in-progress history step flag and restores the composed
// display if the undo/redo step was a no-op or failed.
func (e *Editor) finishTileReviewHistoryStep(ok bool) {
	if e == nil || e.tileReview == nil {
		return
	}
	rev := e.tileReview
	rev.inHistoryStep = false
	if !ok && rev.historyOwned {
		e.applyTileReviewDisplay()
	}
}

// TileReviewOpen reports whether a region-based tile edit review is currently open.
func (e *Editor) TileReviewOpen() bool {
	return e != nil && e.tileReview != nil && len(e.tileReview.regions) > 0
}

// TileReviewLayer returns the scene layer and layered tile sub-layer currently under review.
func (e *Editor) TileReviewLayer() (layer, tileLayer string) {
	if !e.TileReviewOpen() {
		return "", ""
	}
	return e.tileReview.layerName, e.tileReview.tileLayer
}

// TileReviewRegions returns a copy of the pending tile review regions in deterministic order.
func (e *Editor) TileReviewRegions() []TileReviewRegion {
	if !e.TileReviewOpen() {
		return nil
	}
	out := make([]TileReviewRegion, len(e.tileReview.regions))
	for i, r := range e.tileReview.regions {
		r.Cells = slices.Clone(r.Cells)
		out[i] = r
	}
	return out
}

// TileReviewSteps returns a copy of the agent steps recorded in the active tile review.
func (e *Editor) TileReviewSteps() []TileReviewStep {
	if !e.TileReviewOpen() {
		return nil
	}
	return slices.Clone(e.tileReview.steps)
}

// TileReviewKeptCount returns how many regions are currently marked kept and the total number of regions.
func (e *Editor) TileReviewKeptCount() (kept, total int) {
	if !e.TileReviewOpen() {
		return 0, 0
	}
	total = len(e.tileReview.regions)
	for _, r := range e.tileReview.regions {
		if !r.Reverted {
			kept++
		}
	}
	return kept, total
}

// TileReviewTotalCells returns the total number of changed cells across all pending review regions.
func (e *Editor) TileReviewTotalCells() int {
	if !e.TileReviewOpen() {
		return 0
	}
	n := 0
	for _, r := range e.tileReview.regions {
		n += len(r.Cells)
	}
	return n
}

// TileReviewFocusedRegion returns the ID of the currently focused tile review region, or "".
func (e *Editor) TileReviewFocusedRegion() string {
	if !e.TileReviewOpen() {
		return ""
	}
	return e.tileReview.focusedID
}

// FocusTileReviewRegion focuses regionID, switches the view to After, and highlights it in the viewport.
func (e *Editor) FocusTileReviewRegion(regionID string) bool {
	if e == nil || e.playing || !e.TileReviewOpen() {
		return false
	}
	for _, r := range e.tileReview.regions {
		if r.ID == regionID {
			e.tileReview.focusedID = regionID
			if e.tileReview.view != TileReviewAfter || e.tileReview.peekBefore {
				e.tileReview.view = TileReviewAfter
				e.tileReview.peekBefore = false
				e.applyTileReviewDisplay()
			}
			e.tileReview.uiRev = e.nextTileReviewUIRev()
			e.notify()
			return true
		}
	}
	return false
}

// TileReviewShowingBefore reports whether the viewport is currently showing the pre-review Before state
// (either because Before is selected or because B is held to peek).
func (e *Editor) TileReviewShowingBefore() bool {
	return e.TileReviewOpen() && (e.tileReview.view == TileReviewBefore || e.tileReview.peekBefore)
}

// TileReviewPeekingBefore reports whether hold-B Before peek is active.
func (e *Editor) TileReviewPeekingBefore() bool {
	return e.TileReviewOpen() && e.tileReview.peekBefore
}

// SetTileReviewViewMode switches between Before and After views during an open tile review.
func (e *Editor) SetTileReviewViewMode(view TileReviewView) {
	if !e.TileReviewOpen() || e.playing {
		return
	}
	if e.tileReview.view == view && !e.tileReview.peekBefore {
		return
	}
	e.tileReview.view = view
	e.tileReview.peekBefore = false
	e.tileReview.uiRev = e.nextTileReviewUIRev()
	e.applyTileReviewDisplay()
	e.notify()
}

// SetTileReviewPeekBefore sets temporary hold-B Before peek mode during an open tile review.
func (e *Editor) SetTileReviewPeekBefore(peeking bool) {
	if e == nil || e.tileReview == nil {
		return
	}
	if e.playing && peeking {
		return
	}
	if e.tileReview.peekBefore == peeking {
		return
	}
	e.tileReview.peekBefore = peeking
	e.tileReview.uiRev = e.nextTileReviewUIRev()
	e.applyTileReviewDisplay()
	e.notify()
}

// TileReviewHandleKeyEvent handles keyboard shortcuts for an open tile review:
// hold B to peek Before (ignoring modifier chords and Play mode).
func (e *Editor) TileReviewHandleKeyEvent(ev core.KeyEvent, down bool) bool {
	if e == nil || e.playing || !e.TileReviewOpen() || e.ViewMode != ViewMode2D {
		return false
	}
	if ev.Modifiers.Platform || ev.Modifiers.Control || ev.Modifiers.Alt {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(ev.Key))
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(ev.Code))
	}
	if key == "b" || key == "keyb" {
		e.SetTileReviewPeekBefore(down)
		return true
	}
	return false
}

// SetTileReviewRegionReverted marks regionID reverted (true) or kept/restored (false) and updates the live viewport.
func (e *Editor) SetTileReviewRegionReverted(regionID string, reverted bool) error {
	if err := e.checkTileEditAllowed(); err != nil {
		return err
	}
	if !e.TileReviewOpen() {
		return ErrNoTileReview
	}
	idx := -1
	for i := range e.tileReview.regions {
		if e.tileReview.regions[i].ID == regionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("editor: tile review has no region %q", regionID)
	}
	if e.tileReview.regions[idx].Reverted == reverted {
		return nil
	}
	e.tileReview.regions[idx].Reverted = reverted
	if reverted {
		e.tileReview.revertedByID[regionID] = true
	} else {
		delete(e.tileReview.revertedByID, regionID)
	}
	e.tileReview.uiRev = e.nextTileReviewUIRev()
	e.applyTileReviewDisplay()
	e.notify()
	return nil
}

// ToggleTileReviewRegion flips whether regionID is reverted or restored in the active tile review.
func (e *Editor) ToggleTileReviewRegion(regionID string) error {
	if !e.TileReviewOpen() {
		return ErrNoTileReview
	}
	for _, r := range e.tileReview.regions {
		if r.ID == regionID {
			return e.SetTileReviewRegionReverted(regionID, !r.Reverted)
		}
	}
	return fmt.Errorf("editor: tile review has no region %q", regionID)
}

// RevertTileReviewRegionByIndex marks the 0-based region index reverted.
func (e *Editor) RevertTileReviewRegionByIndex(index int) error {
	if !e.TileReviewOpen() {
		return ErrNoTileReview
	}
	if index < 0 || index >= len(e.tileReview.regions) {
		return fmt.Errorf("editor: tile review region index %d out of range (0..%d)", index, len(e.tileReview.regions)-1)
	}
	return e.SetTileReviewRegionReverted(e.tileReview.regions[index].ID, true)
}

// RestoreTileReviewRegionByIndex restores the 0-based region index.
func (e *Editor) RestoreTileReviewRegionByIndex(index int) error {
	if !e.TileReviewOpen() {
		return ErrNoTileReview
	}
	if index < 0 || index >= len(e.tileReview.regions) {
		return fmt.Errorf("editor: tile review region index %d out of range (0..%d)", index, len(e.tileReview.regions)-1)
	}
	return e.SetTileReviewRegionReverted(e.tileReview.regions[index].ID, false)
}

// KeepTileReview commits all non-reverted regions of the active tile review as ONE undoable step on History
// and closes the review. Undoing that single step restores the exact pre-review tilemap cleanly.
func (e *Editor) KeepTileReview() (AgentTileEditResult, error) {
	if err := e.checkTileEditAllowed(); err != nil {
		return AgentTileEditResult{}, err
	}
	if !e.TileReviewOpen() {
		return AgentTileEditResult{}, ErrNoTileReview
	}
	e.exitTileReviewBeforePreview()
	rev := e.tileReview
	kept, total := e.TileReviewKeptCount()
	finalValues := composeTileReviewValues(rev)

	_, _, g, _, err := e.resolveTileGridWithLayer(rev.layerName, rev.tileLayer)
	if err != nil {
		return AgentTileEditResult{}, err
	}
	cur := g.values()

	// Build net changes from pre-review base -> final kept values.
	var netChanges []tileChange
	for i := range min(len(rev.base), len(finalValues)) {
		if rev.base[i] != finalValues[i] {
			netChanges = append(netChanges, tileChange{index: i, oldGID: rev.base[i], newGID: finalValues[i]})
		}
	}

	layerName, tileLayer := rev.layerName, rev.tileLayer
	historyOwned := rev.historyOwned
	historyStart := rev.historyStart
	e.tileReview = nil

	if historyOwned && e.History != nil && historyStart >= 0 && historyStart <= e.History.cursor {
		// Collapse all intermediate agent review steps back to historyStart so Keep records exactly ONE undo step.
		clear(e.History.commands[historyStart:])
		e.History.commands = e.History.commands[:historyStart]
		e.History.cursor = historyStart
		e.History.truncateRecordsAfterCommand(historyStart)

		// Restore grid to base first so Do/Undo transitions cleanly from base <-> final.
		var toBase []tileChange
		for i := range min(len(cur), len(rev.base)) {
			if cur[i] != rev.base[i] {
				toBase = append(toBase, tileChange{index: i, oldGID: cur[i], newGID: rev.base[i]})
			}
		}
		if len(toBase) > 0 {
			g.apply(toBase, false)
		}

		if len(netChanges) == 0 {
			e.Status = fmt.Sprintf("Reverted all tile changes on %s", layerName)
			e.afterMutate()
			return AgentTileEditResult{
				Layer:        layerName,
				TileLayer:    tileLayer,
				ChangedCells: 0,
				ReviewOpen:   false,
				RegionCount:  total,
				KeptRegions:  kept,
			}, nil
		}

		label := fmt.Sprintf("Keep %d of %d tile regions (%d cells) on %s", kept, total, len(netChanges), layerName)
		summary := "tile edits on " + layerName
		if len(rev.steps) > 0 && rev.steps[len(rev.steps)-1].Summary != "" {
			summary = rev.steps[len(rev.steps)-1].Summary
		}
		prevMeta := e.historyPanel.pendingMeta
		e.historyPanel.pendingMeta = &HistoryMeta{
			Author:      HistoryAuthorAgent,
			Summary:     summary,
			Detail:      formatAgentTurnDetail("", total, kept),
			ChangeCount: total,
			KeptCount:   kept,
		}
		e.History.Execute(&tileEditCmd{
			ed:        e,
			layerName: layerName,
			tileLayer: tileLayer,
			label:     label,
			changes:   netChanges,
		})
		e.historyPanel.pendingMeta = prevMeta
		return AgentTileEditResult{
			Layer:        layerName,
			TileLayer:    tileLayer,
			ChangedCells: len(netChanges),
			UndoLabel:    label,
			ReviewOpen:   false,
			RegionCount:  total,
			KeptRegions:  kept,
		}, nil
	}

	// Fallback when non-review commands were interleaved: only commit reverts for regions explicitly
	// marked reverted whose cells still match the review's values, preserving all unrelated user edits.
	var delta []tileChange
	for _, r := range rev.regions {
		if !r.Reverted {
			continue
		}
		for _, c := range r.Cells {
			idx := c.Row*rev.width + c.Col
			if idx < 0 || idx >= len(cur) {
				continue
			}
			if cur[idx] == c.NewGID || cur[idx] == c.OldGID {
				delta = append(delta, tileChange{index: idx, oldGID: c.NewGID, newGID: c.OldGID})
			}
		}
	}
	label := fmt.Sprintf("Keep %d of %d tile regions on %s", kept, total, layerName)
	if len(delta) > 0 {
		if e.History == nil {
			e.History = NewHistory()
		}
		e.History.Execute(&tileEditCmd{
			ed:        e,
			layerName: layerName,
			tileLayer: tileLayer,
			label:     label,
			changes:   delta,
		})
	} else {
		e.Status = label
		e.afterMutate()
	}
	if total > 0 {
		if kept > 0 {
			e.RecordAgentReviewOutcome(true, kept)
		}
		if reverted := total - kept; reverted > 0 {
			e.RecordAgentReviewOutcome(false, reverted)
		}
	}
	return AgentTileEditResult{
		Layer:        layerName,
		TileLayer:    tileLayer,
		ChangedCells: len(netChanges),
		UndoLabel:    label,
		ReviewOpen:   false,
		RegionCount:  total,
		KeptRegions:  kept,
	}, nil
}

// RevertAllTileReview discards every pending region of the active tile review, restoring the exact pre-review tilemap.
func (e *Editor) RevertAllTileReview() error {
	if err := e.checkTileEditAllowed(); err != nil {
		return err
	}
	if !e.TileReviewOpen() {
		return ErrNoTileReview
	}
	e.exitTileReviewBeforePreview()
	rev := e.tileReview
	_, _, g, _, err := e.resolveTileGridWithLayer(rev.layerName, rev.tileLayer)
	if err != nil {
		return err
	}
	cur := g.values()

	layerName, tileLayer := rev.layerName, rev.tileLayer
	historyOwned := rev.historyOwned
	historyStart := rev.historyStart
	e.tileReview = nil

	if historyOwned && e.History != nil && historyStart >= 0 && historyStart <= e.History.cursor {
		var toBase []tileChange
		for i := range min(len(cur), len(rev.base)) {
			if cur[i] != rev.base[i] {
				toBase = append(toBase, tileChange{index: i, oldGID: cur[i], newGID: rev.base[i]})
			}
		}
		clear(e.History.commands[historyStart:])
		e.History.commands = e.History.commands[:historyStart]
		e.History.cursor = historyStart
		if len(toBase) > 0 {
			g.apply(toBase, false)
		}
		e.Status = fmt.Sprintf("Reverted all tile changes on %s", layerName)
		e.afterMutate()
		return nil
	}

	// Fallback when non-review commands were interleaved: revert only review cells that still match
	// the review's values so unrelated manual edits made while the review was open are preserved.
	var toBase []tileChange
	for _, r := range rev.regions {
		for _, c := range r.Cells {
			idx := c.Row*rev.width + c.Col
			if idx < 0 || idx >= len(cur) {
				continue
			}
			if cur[idx] == c.NewGID || (r.Reverted && cur[idx] == c.OldGID) {
				toBase = append(toBase, tileChange{index: idx, oldGID: c.NewGID, newGID: c.OldGID})
			}
		}
	}
	if len(toBase) > 0 {
		if e.History == nil {
			e.History = NewHistory()
		}
		e.History.Execute(&tileEditCmd{
			ed:        e,
			layerName: layerName,
			tileLayer: tileLayer,
			label:     fmt.Sprintf("Revert all tile review changes on %s", layerName),
			changes:   toBase,
		})
	} else {
		e.Status = fmt.Sprintf("Reverted all tile changes on %s", layerName)
		e.afterMutate()
	}
	return nil
}

// syncTileReviewAfterMutate keeps an open tile review consistent when History changes outside commitAgentTileChanges.
func (e *Editor) syncTileReviewAfterMutate() {
	rev := e.tileReview
	if rev == nil || rev.applying {
		return
	}
	if e.History != nil && e.History.cursor <= rev.historyStart {
		// All steps back to before the review started were undone.
		e.tileReview = nil
		return
	}
	_, _, g, _, err := e.resolveTileGridWithLayer(rev.layerName, rev.tileLayer)
	if err != nil || g.tm.Width != rev.width || g.tm.Height != rev.height {
		e.tileReview = nil
		return
	}
	curCursor := 0
	if e.History != nil {
		curCursor = e.History.cursor
	}
	if rev.historyOwned {
		isUndo := curCursor < rev.lastHistoryCursor
		isReviewRedo := curCursor > rev.lastHistoryCursor && rev.inHistoryStep && curCursor <= rev.maxReviewCursor
		if isUndo || isReviewRedo {
			rev.lastHistoryCursor = curCursor
			rev.proposed = g.values()
			rev.view = TileReviewAfter
			rev.peekBefore = false
			e.recomputeTileReviewRegions()
			if len(rev.regions) == 0 {
				e.tileReview = nil
			} else {
				e.applyTileReviewDisplay()
			}
			return
		}
		if curCursor != rev.lastHistoryCursor {
			// Another non-review command executed while the review was open.
			rev.historyOwned = false
			rev.lastHistoryCursor = curCursor
			rev.uiRev = e.nextTileReviewUIRev()
		}
		return
	}
	if curCursor != rev.lastHistoryCursor {
		rev.lastHistoryCursor = curCursor
		rev.uiRev = e.nextTileReviewUIRev()
	}
}

// drawTileReviewOverlay draws region tints, perimeter outlines (Spec added/changed/removed line styles),
// and focus halos over the 2D viewport while a tile review is open and showing After.
func (e *Editor) drawTileReviewOverlay(d *core.Drawing) {
	if e == nil || d == nil || e.playing || e.ViewMode != ViewMode2D || !e.TileReviewOpen() || e.TileReviewShowingBefore() {
		return
	}
	rev := e.tileReview
	layerName, _, g, _, err := e.resolveTileGridWithLayer(rev.layerName, rev.tileLayer)
	if err != nil || !e.layerDrawn(layerName) {
		return
	}
	tm := g.tm
	if tm == nil || tm.TileWidth <= 0 || tm.TileHeight <= 0 {
		return
	}
	space, px, py := e.layerSpaceAndParallax(layerName)
	m := e.layerAffineFor(space, px, py)
	scale := strokeScale(m)
	if scale <= 0 {
		scale = 1
	}
	tw, th := float32(tm.TileWidth), float32(tm.TileHeight)
	ox, oy := tm.OffsetX, tm.OffsetY
	lw := 1.5 / scale
	dashOn, dashOff := 4/scale, 3/scale

	d.Save()
	applyAffine(d, m)
	for i, r := range rev.regions {
		if r.Reverted || len(r.Cells) == 0 {
			continue
		}
		var lineColor, fillColor core.Color
		dashed := true
		switch r.Kind {
		case TileReviewPainted:
			lineColor = specAdded
			fillColor = core.RGBA(0x4FD38A, 0.16)
		case TileReviewReplaced:
			lineColor = specChanged
			fillColor = core.RGBA(0xE9A84A, 0.16)
		case TileReviewErased:
			lineColor = specProblem
			fillColor = core.RGBA(0xFF6B6B, 0.18)
			dashed = false
		default:
			lineColor = specAccent
			fillColor = core.RGBA(0x3B7BFF, 0.16)
		}

		for _, c := range r.Cells {
			x := ox + float32(c.Col)*tw
			y := oy + float32(c.Row)*th
			d.SetFillColor(fillColor).FillRect(x, y, tw, th)
		}

		var perimeter []tileReviewEdge
		if i < len(rev.perimeters) {
			perimeter = rev.perimeters[i]
		}

		if r.ID == rev.focusedID && len(perimeter) > 0 {
			d.SetLineDash()
			d.SetStrokeColor(core.RGBA(0x3B7BFF, 0.45)).SetLineWidth(4 / scale)
			d.BeginPath()
			for _, s := range perimeter {
				d.MoveTo(ox+float32(s.c0)*tw, oy+float32(s.r0)*th).
					LineTo(ox+float32(s.c1)*tw, oy+float32(s.r1)*th)
			}
			d.Stroke()
		}

		if dashed {
			d.SetLineDash(dashOn, dashOff)
		} else {
			d.SetLineDash()
		}
		d.SetStrokeColor(lineColor).SetLineWidth(lw)
		d.BeginPath()
		for _, s := range perimeter {
			d.MoveTo(ox+float32(s.c0)*tw, oy+float32(s.r0)*th).
				LineTo(ox+float32(s.c1)*tw, oy+float32(s.r1)*th)
		}
		d.Stroke()
	}
	d.Restore()
}
