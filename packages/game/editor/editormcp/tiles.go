package editormcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/tilemap"
)

// The names of the tile and tile-review tools added to the editor's MCP server.
const (
	// ToolTileQuery queries cells and connected regions/pockets on an inline tilemap layer.
	ToolTileQuery = "tile_query"
	// ToolTilePaint paints explicit cells, rectangles, or weighted random picks on a tile layer.
	ToolTilePaint = "tile_paint"
	// ToolTileAutotile re-edges (or places and re-edges) a region using a YAML tileset terrain set.
	ToolTileAutotile = "tile_autotile"
	// ToolTileErase clears explicit cells or rectangles on a tile layer.
	ToolTileErase = "tile_erase"
	// ToolTileReviewStatus inspects the active region-based tile edit review (and optionally toggles Before/After or focuses a region).
	ToolTileReviewStatus = "tile_review_status"
	// ToolTileReviewRevertRegion marks one pending tile review region reverted.
	ToolTileReviewRevertRegion = "tile_review_revert_region"
	// ToolTileReviewRestoreRegion restores a reverted tile review region.
	ToolTileReviewRestoreRegion = "tile_review_restore_region"
	// ToolTileReviewKeep commits all kept tile review regions cleanly as one undoable edit.
	ToolTileReviewKeep = "tile_review_keep"
	// ToolTileReviewRevertAll reverts all pending tile review changes and closes the review.
	ToolTileReviewRevertAll = "tile_review_revert_all"
)

func tileTools() []Tool {
	return []Tool{
		{
			Name:        ToolTileQuery,
			Title:       "Query tiles and connected regions",
			Description: "Query cells or connected regions/pockets on an inline tilemap layer by tile ID, tile name, or empty cells (e.g. finding an 11 × 5 empty pocket at columns 2–12), returning bounding rectangles, cell counts, and coordinates.",
			InputSchema: []byte(tileQuerySchema),
			Action:      ActionRead,
			Handler:     tileQuery,
		},
		{
			Name:        ToolTilePaint,
			Title:       "Paint tiles on a layer",
			Description: "Paint explicit cells, inclusive rectangles, or deterministic weighted random picks of tiles on an inline tilemap layer in one undoable edit, tracked in the active region-based tile edit review (default review: true).",
			InputSchema: []byte(tilePaintSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     tilePaint,
		},
		{
			Name:        ToolTileAutotile,
			Title:       "Autotile a region with a terrain set",
			Description: "Re-edge a region (or place terrain and re-edge it) on a layered tilemap using a YAML tileset terrain set's 4-bit cardinal or 8-bit blob autotile rules in one undoable edit, tracked in the active tile edit review (default review: true).",
			InputSchema: []byte(tileAutotileSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     tileAutotile,
		},
		{
			Name:        ToolTileErase,
			Title:       "Erase tiles on a layer",
			Description: "Erase explicit cells or inclusive rectangles on an inline tilemap layer in one undoable edit, tracked in the active region-based tile edit review (default review: true).",
			InputSchema: []byte(tileEraseSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     tileErase,
		},
		{
			Name:        ToolTileReviewStatus,
			Title:       "Inspect active tile edit review",
			Description: "Return the pending regions (painted, replaced, erased), cell counts, bounding rectangles, revert state, agent steps, and Before/After view mode of the active tile edit review; optionally switch view or focus a region.",
			InputSchema: []byte(tileReviewStatusSchema),
			Action:      ActionRead,
			Handler:     tileReviewStatus,
		},
		{
			Name:        ToolTileReviewRevertRegion,
			Title:       "Revert one tile review region",
			Description: "Mark one region in the active tile edit review as reverted (by region id or 0-based index) and update the live 2D viewport.",
			InputSchema: []byte(tileReviewRegionSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     tileReviewRevertRegion,
		},
		{
			Name:        ToolTileReviewRestoreRegion,
			Title:       "Restore one tile review region",
			Description: "Restore a previously reverted region in the active tile edit review (by region id or 0-based index) and update the live 2D viewport.",
			InputSchema: []byte(tileReviewRegionSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     tileReviewRestoreRegion,
		},
		{
			Name:        ToolTileReviewKeep,
			Title:       "Keep selected tile review regions",
			Description: "Commit all non-reverted regions of the active tile edit review cleanly as one undoable step on History and close the review.",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{}}`),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     tileReviewKeep,
		},
		{
			Name:        ToolTileReviewRevertAll,
			Title:       "Revert all tile review edits",
			Description: "Revert every pending change in the active tile edit review back to the pre-review tilemap and close the review.",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{}}`),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     tileReviewRevertAll,
		},
	}
}

const tileRectSchemaProps = `{"type":"object","additionalProperties":false,"required":["col0","row0","col1","row1"],"properties":{"col0":{"type":"integer","minimum":0},"row0":{"type":"integer","minimum":0},"col1":{"type":"integer","minimum":0},"row1":{"type":"integer","minimum":0}}}`

const tileQuerySchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "layer": {"type": "string", "description": "Scene layer name; empty uses the active layer or first inline tilemap layer"},
    "tileLayer": {"type": "string", "description": "Tile sub-layer name on a layered tilemap; empty uses the active or first tile layer"},
    "empty": {"type": "boolean", "description": "Match only empty cells (tile ID 0) when true"},
    "tile": {"type": "integer", "minimum": 0, "description": "Single tile ID to match (0 = empty, >0 = tile ID as PaintTile numbers tiles)"},
    "tiles": {"type": "array", "items": {"type": "integer", "minimum": 0}, "description": "List of tile IDs to match"},
    "tileName": {"type": "string", "description": "Named tile in the layer's YAML tileset to match"},
    "tileNames": {"type": "array", "items": {"type": "string"}, "description": "List of named tiles in the layer's YAML tileset to match"},
    "rect": ` + tileRectSchemaProps + `,
    "minCells": {"type": "integer", "minimum": 1, "description": "Minimum connected region cell count to include"},
    "diagonal": {"type": "boolean", "description": "Use 8-connected neighbors instead of 4-connected neighbors (default false)"},
    "includeCells": {"type": "boolean", "description": "Include explicit cell coordinates in each region (defaults to true when total matched cells <= 512)"}
  }
}`

const tilePaintSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "layer": {"type": "string", "description": "Scene layer name; empty uses the active layer or first inline tilemap layer"},
    "tileLayer": {"type": "string", "description": "Tile sub-layer name on a layered tilemap; empty uses the active or first tile layer"},
    "tile": {"type": "integer", "minimum": 1, "description": "Default tile ID to paint across rects and cells"},
    "tileName": {"type": "string", "description": "Default YAML tileset tile name to paint across rects and cells"},
    "flipH": {"type": "boolean"},
    "flipV": {"type": "boolean"},
    "flipD": {"type": "boolean"},
    "weighted": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "tile": {"type": "integer", "minimum": 1},
          "tileName": {"type": "string"},
          "weight": {"type": "number", "exclusiveMinimum": 0}
        }
      },
      "description": "Weighted random tile pool picked deterministically per cell using seed"
    },
    "seed": {"type": "integer", "minimum": 0, "description": "Deterministic random seed for weighted picks (defaults to 1)"},
    "noAdjacentDuplicates": {"type": "boolean", "description": "Avoid matching already-painted left or upper neighbors when picking from weighted"},
    "rect": {
      "type": "object",
      "additionalProperties": false,
      "required": ["col0", "row0", "col1", "row1"],
      "properties": {
        "col0": {"type": "integer", "minimum": 0},
        "row0": {"type": "integer", "minimum": 0},
        "col1": {"type": "integer", "minimum": 0},
        "row1": {"type": "integer", "minimum": 0},
        "tile": {"type": "integer", "minimum": 1},
        "tileName": {"type": "string"},
        "flipH": {"type": "boolean"},
        "flipV": {"type": "boolean"},
        "flipD": {"type": "boolean"}
      }
    },
    "rects": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["col0", "row0", "col1", "row1"],
        "properties": {
          "col0": {"type": "integer", "minimum": 0},
          "row0": {"type": "integer", "minimum": 0},
          "col1": {"type": "integer", "minimum": 0},
          "row1": {"type": "integer", "minimum": 0},
          "tile": {"type": "integer", "minimum": 1},
          "tileName": {"type": "string"},
          "flipH": {"type": "boolean"},
          "flipV": {"type": "boolean"},
          "flipD": {"type": "boolean"}
        }
      }
    },
    "cells": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["col", "row"],
        "properties": {
          "col": {"type": "integer", "minimum": 0},
          "row": {"type": "integer", "minimum": 0},
          "tile": {"type": "integer", "minimum": 1},
          "tileName": {"type": "string"},
          "flipH": {"type": "boolean"},
          "flipV": {"type": "boolean"},
          "flipD": {"type": "boolean"}
        }
      }
    },
    "label": {"type": "string", "description": "Short human-readable label for the review step and region"},
    "review": {"type": "boolean", "description": "Track these tile edits in the active region-based tile review (default true)"}
  }
}`

const tileAutotileSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "layer": {"type": "string", "description": "Scene layer name; empty uses the active layer or first inline tilemap layer"},
    "tileLayer": {"type": "string", "description": "Tile sub-layer name on a layered tilemap; empty uses the active or first tile layer"},
    "terrain": {"type": "string", "description": "Terrain set name from the layer's YAML tileset; defaults to ActiveTerrain or the first terrain"},
    "rect": ` + tileRectSchemaProps + `,
    "rects": {"type": "array", "items": ` + tileRectSchemaProps + `},
    "cells": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["col", "row"],
        "properties": {
          "col": {"type": "integer", "minimum": 0},
          "row": {"type": "integer", "minimum": 0}
        }
      }
    },
    "place": {"type": "boolean", "description": "True to place the terrain onto target cells and re-edge neighbors; false to only re-edge cells already belonging to the terrain; omitted auto-detects"},
    "label": {"type": "string", "description": "Short human-readable label for the review step and region"},
    "review": {"type": "boolean", "description": "Track these tile edits in the active region-based tile review (default true)"}
  }
}`

const tileEraseSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "layer": {"type": "string", "description": "Scene layer name; empty uses the active layer or first inline tilemap layer"},
    "tileLayer": {"type": "string", "description": "Tile sub-layer name on a layered tilemap; empty uses the active or first tile layer"},
    "rect": ` + tileRectSchemaProps + `,
    "rects": {"type": "array", "items": ` + tileRectSchemaProps + `},
    "cells": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["col", "row"],
        "properties": {
          "col": {"type": "integer", "minimum": 0},
          "row": {"type": "integer", "minimum": 0}
        }
      }
    },
    "label": {"type": "string", "description": "Short human-readable label for the review step and region"},
    "review": {"type": "boolean", "description": "Track these tile edits in the active region-based tile review (default true)"}
  }
}`

const tileReviewStatusSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "view": {"type": "string", "enum": ["before", "after"], "description": "Optional view mode to switch to while a tile review is open"},
    "focus": {"type": "string", "description": "Optional region ID to focus in the 2D viewport"}
  }
}`

const tileReviewRegionSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "id": {"type": "string", "description": "Region ID from tile_review_status"},
    "index": {"type": "integer", "minimum": 0, "description": "0-based region index from tile_review_status"}
  }
}`

type tileRectArg struct {
	Col0 int `json:"col0"`
	Row0 int `json:"row0"`
	Col1 int `json:"col1"`
	Row1 int `json:"row1"`
}

type tilePaintRectArg struct {
	Col0     int    `json:"col0"`
	Row0     int    `json:"row0"`
	Col1     int    `json:"col1"`
	Row1     int    `json:"row1"`
	Tile     int    `json:"tile"`
	TileName string `json:"tileName"`
	FlipH    bool   `json:"flipH"`
	FlipV    bool   `json:"flipV"`
	FlipD    bool   `json:"flipD"`
}

type tilePaintCellArg struct {
	Col      int    `json:"col"`
	Row      int    `json:"row"`
	Tile     int    `json:"tile"`
	TileName string `json:"tileName"`
	FlipH    bool   `json:"flipH"`
	FlipV    bool   `json:"flipV"`
	FlipD    bool   `json:"flipD"`
}

type tileWeightedArg struct {
	Tile     int     `json:"tile"`
	TileName string  `json:"tileName"`
	Weight   float64 `json:"weight"`
}

type tileCoordArg struct {
	Col int `json:"col"`
	Row int `json:"row"`
}

func packFlipBits(h, v, d bool) uint32 {
	var bits uint32
	if h {
		bits |= tilemap.FlipHorizontal
	}
	if v {
		bits |= tilemap.FlipVertical
	}
	if d {
		bits |= tilemap.FlipDiagonal
	}
	return bits
}

// TileReviewRegionView summarizes one region returned by tile_review_* MCP tools.
type TileReviewRegionView struct {
	Index     int             `json:"index"`
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Sign      string          `json:"sign"`
	Title     string          `json:"title"`
	Detail    string          `json:"detail"`
	Bounds    editor.TileRect `json:"bounds"`
	CellCount int             `json:"cellCount"`
	Reverted  bool            `json:"reverted"`
}

// TileReviewStatusResult is what tile_review_status, tile_review_revert_region, and tile_review_restore_region return.
type TileReviewStatusResult struct {
	Open          bool                    `json:"open"`
	Layer         string                  `json:"layer,omitempty"`
	TileLayer     string                  `json:"tileLayer,omitempty"`
	ShowingBefore bool                    `json:"showingBefore"`
	PeekingBefore bool                    `json:"peekingBefore"`
	FocusedRegion string                  `json:"focusedRegion,omitempty"`
	KeptRegions   int                     `json:"keptRegions"`
	TotalRegions  int                     `json:"totalRegions"`
	TotalCells    int                     `json:"totalCells"`
	Steps         []editor.TileReviewStep `json:"steps,omitempty"`
	Regions       []TileReviewRegionView  `json:"regions,omitempty"`
}

func snapshotTileReviewStatus(ed *editor.Editor) TileReviewStatusResult {
	if !ed.TileReviewOpen() {
		return TileReviewStatusResult{Open: false}
	}
	layer, tileLayer := ed.TileReviewLayer()
	kept, total := ed.TileReviewKeptCount()
	regions := ed.TileReviewRegions()
	views := make([]TileReviewRegionView, len(regions))
	for i, r := range regions {
		views[i] = TileReviewRegionView{
			Index:     i,
			ID:        r.ID,
			Kind:      string(r.Kind),
			Sign:      r.Sign,
			Title:     r.Title,
			Detail:    r.Detail,
			Bounds:    r.Bounds,
			CellCount: len(r.Cells),
			Reverted:  r.Reverted,
		}
	}
	return TileReviewStatusResult{
		Open:          true,
		Layer:         layer,
		TileLayer:     tileLayer,
		ShowingBefore: ed.TileReviewShowingBefore(),
		PeekingBefore: ed.TileReviewPeekingBefore(),
		FocusedRegion: ed.TileReviewFocusedRegion(),
		KeptRegions:   kept,
		TotalRegions:  total,
		TotalCells:    ed.TileReviewTotalCells(),
		Steps:         ed.TileReviewSteps(),
		Regions:       views,
	}
}

func formatTileReviewStatusText(st TileReviewStatusResult) string {
	if !st.Open {
		return "No tile edit review is open."
	}
	var b strings.Builder
	mode := "After"
	if st.ShowingBefore {
		mode = "Before"
	}
	fmt.Fprintf(&b, "Tile review on %s: %d of %d regions kept (%d cells total, viewing %s).\n",
		st.Layer, st.KeptRegions, st.TotalRegions, st.TotalCells, mode)
	for _, r := range st.Regions {
		state := "kept"
		if r.Reverted {
			state = "reverted"
		}
		fmt.Fprintf(&b, "  [%d] %s %s (%s) · %d cells (%s) — %s\n",
			r.Index, r.Sign, r.Title, r.ID, r.CellCount, state, r.Detail)
	}
	return b.String()
}

func tileQuery(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Layer        string       `json:"layer"`
		TileLayer    string       `json:"tileLayer"`
		Empty        bool         `json:"empty"`
		Tile         *int         `json:"tile"`
		Tiles        []int        `json:"tiles"`
		TileName     string       `json:"tileName"`
		TileNames    []string     `json:"tileNames"`
		Rect         *tileRectArg `json:"rect"`
		MinCells     int          `json:"minCells"`
		Diagonal     bool         `json:"diagonal"`
		IncludeCells *bool        `json:"includeCells"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tile_query: %w", err)
	}
	opts := editor.TileQueryOptions{
		Layer:        args.Layer,
		TileLayer:    args.TileLayer,
		EmptyOnly:    args.Empty,
		TileIDs:      append([]int(nil), args.Tiles...),
		TileNames:    append([]string(nil), args.TileNames...),
		MinCells:     args.MinCells,
		Diagonal:     args.Diagonal,
		IncludeCells: args.IncludeCells,
	}
	if args.Tile != nil {
		opts.TileIDs = append(opts.TileIDs, *args.Tile)
	}
	if args.TileName != "" {
		opts.TileNames = append(opts.TileNames, args.TileName)
	}
	if args.Rect != nil {
		r := editor.TileRect{Col0: args.Rect.Col0, Row0: args.Rect.Row0, Col1: args.Rect.Col1, Row1: args.Rect.Row1}
		opts.Rect = &r
	}
	var res editor.TileQueryResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		var err error
		res, err = ed.QueryTiles(opts)
		return err
	}); err != nil {
		return nil, fmt.Errorf("tile_query: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Layer %s (%d × %d cells · %d px): matched %d cells in %d region(s).",
		res.Layer, res.Width, res.Height, res.TileWidth, res.MatchedCells, len(res.Regions))
	for i, rg := range res.Regions {
		if i >= 10 {
			fmt.Fprintf(&b, "\n  ... and %d more regions.", len(res.Regions)-i)
			break
		}
		fmt.Fprintf(&b, "\n  - Found %s (%d cells).", rg.Summary, rg.CellCount)
	}
	return &Result{Text: b.String(), Structured: res}, nil
}

func tilePaint(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Layer                string             `json:"layer"`
		TileLayer            string             `json:"tileLayer"`
		Tile                 int                `json:"tile"`
		TileName             string             `json:"tileName"`
		FlipH                bool               `json:"flipH"`
		FlipV                bool               `json:"flipV"`
		FlipD                bool               `json:"flipD"`
		Weighted             []tileWeightedArg  `json:"weighted"`
		Seed                 uint64             `json:"seed"`
		NoAdjacentDuplicates bool               `json:"noAdjacentDuplicates"`
		Rect                 *tilePaintRectArg  `json:"rect"`
		Rects                []tilePaintRectArg `json:"rects"`
		Cells                []tilePaintCellArg `json:"cells"`
		Label                string             `json:"label"`
		Review               *bool              `json:"review"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tile_paint: %w", err)
	}
	trackReview := true
	if args.Review != nil {
		trackReview = *args.Review
	}
	req := editor.AgentTilePaintRequest{
		Layer:                args.Layer,
		TileLayer:            args.TileLayer,
		TileID:               args.Tile,
		TileName:             args.TileName,
		Flip:                 packFlipBits(args.FlipH, args.FlipV, args.FlipD),
		Seed:                 args.Seed,
		NoAdjacentDuplicates: args.NoAdjacentDuplicates,
		Label:                args.Label,
		TrackReview:          trackReview,
	}
	for _, w := range args.Weighted {
		req.Weighted = append(req.Weighted, editor.AgentTileWeighted{
			TileID:   w.Tile,
			TileName: w.TileName,
			Weight:   w.Weight,
		})
	}
	if args.Rect != nil {
		req.Rects = append(req.Rects, editor.AgentTileRect{
			Col0:     args.Rect.Col0,
			Row0:     args.Rect.Row0,
			Col1:     args.Rect.Col1,
			Row1:     args.Rect.Row1,
			TileID:   args.Rect.Tile,
			TileName: args.Rect.TileName,
			Flip:     packFlipBits(args.Rect.FlipH, args.Rect.FlipV, args.Rect.FlipD),
		})
	}
	for _, r := range args.Rects {
		req.Rects = append(req.Rects, editor.AgentTileRect{
			Col0:     r.Col0,
			Row0:     r.Row0,
			Col1:     r.Col1,
			Row1:     r.Row1,
			TileID:   r.Tile,
			TileName: r.TileName,
			Flip:     packFlipBits(r.FlipH, r.FlipV, r.FlipD),
		})
	}
	for _, cell := range args.Cells {
		req.Cells = append(req.Cells, editor.AgentTileCell{
			Col:      cell.Col,
			Row:      cell.Row,
			TileID:   cell.Tile,
			TileName: cell.TileName,
			Flip:     packFlipBits(cell.FlipH, cell.FlipV, cell.FlipD),
		})
	}
	var res editor.AgentTileEditResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		var err error
		res, err = ed.AgentPaintTiles(req)
		return err
	}); err != nil {
		return nil, fmt.Errorf("tile_paint: %w", err)
	}
	text := fmt.Sprintf("Painted %d cells on %s.", res.ChangedCells, res.Layer)
	if res.ReviewOpen {
		text += fmt.Sprintf(" Active tile review now has %d region(s) (%d cells).", res.RegionCount, res.ReviewCells)
	}
	return &Result{Text: text, Structured: res}, nil
}

func tileAutotile(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Layer     string         `json:"layer"`
		TileLayer string         `json:"tileLayer"`
		Terrain   string         `json:"terrain"`
		Rect      *tileRectArg   `json:"rect"`
		Rects     []tileRectArg  `json:"rects"`
		Cells     []tileCoordArg `json:"cells"`
		Place     *bool          `json:"place"`
		Label     string         `json:"label"`
		Review    *bool          `json:"review"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tile_autotile: %w", err)
	}
	trackReview := true
	if args.Review != nil {
		trackReview = *args.Review
	}
	req := editor.AgentTileAutotileRequest{
		Layer:       args.Layer,
		TileLayer:   args.TileLayer,
		Terrain:     args.Terrain,
		Place:       args.Place,
		Label:       args.Label,
		TrackReview: trackReview,
	}
	if args.Rect != nil {
		req.Rects = append(req.Rects, editor.TileRect{Col0: args.Rect.Col0, Row0: args.Rect.Row0, Col1: args.Rect.Col1, Row1: args.Rect.Row1})
	}
	for _, r := range args.Rects {
		req.Rects = append(req.Rects, editor.TileRect{Col0: r.Col0, Row0: r.Row0, Col1: r.Col1, Row1: r.Row1})
	}
	for _, cell := range args.Cells {
		req.Cells = append(req.Cells, editor.TileCoord{Col: cell.Col, Row: cell.Row})
	}
	var res editor.AgentTileEditResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		var err error
		res, err = ed.AgentAutotile(req)
		return err
	}); err != nil {
		return nil, fmt.Errorf("tile_autotile: %w", err)
	}
	text := fmt.Sprintf("Autotiled %d cells on %s.", res.ChangedCells, res.Layer)
	if res.ReviewOpen {
		text += fmt.Sprintf(" Active tile review now has %d region(s) (%d cells).", res.RegionCount, res.ReviewCells)
	}
	return &Result{Text: text, Structured: res}, nil
}

func tileErase(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Layer     string         `json:"layer"`
		TileLayer string         `json:"tileLayer"`
		Rect      *tileRectArg   `json:"rect"`
		Rects     []tileRectArg  `json:"rects"`
		Cells     []tileCoordArg `json:"cells"`
		Label     string         `json:"label"`
		Review    *bool          `json:"review"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tile_erase: %w", err)
	}
	trackReview := true
	if args.Review != nil {
		trackReview = *args.Review
	}
	req := editor.AgentTileEraseRequest{
		Layer:       args.Layer,
		TileLayer:   args.TileLayer,
		Label:       args.Label,
		TrackReview: trackReview,
	}
	if args.Rect != nil {
		req.Rects = append(req.Rects, editor.TileRect{Col0: args.Rect.Col0, Row0: args.Rect.Row0, Col1: args.Rect.Col1, Row1: args.Rect.Row1})
	}
	for _, r := range args.Rects {
		req.Rects = append(req.Rects, editor.TileRect{Col0: r.Col0, Row0: r.Row0, Col1: r.Col1, Row1: r.Row1})
	}
	for _, cell := range args.Cells {
		req.Cells = append(req.Cells, editor.TileCoord{Col: cell.Col, Row: cell.Row})
	}
	var res editor.AgentTileEditResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		var err error
		res, err = ed.AgentEraseTiles(req)
		return err
	}); err != nil {
		return nil, fmt.Errorf("tile_erase: %w", err)
	}
	text := fmt.Sprintf("Erased %d cells on %s.", res.ChangedCells, res.Layer)
	if res.ReviewOpen {
		text += fmt.Sprintf(" Active tile review now has %d region(s) (%d cells).", res.RegionCount, res.ReviewCells)
	}
	return &Result{Text: text, Structured: res}, nil
}

func tileReviewStatus(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		View  string `json:"view"`
		Focus string `json:"focus"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tile_review_status: %w", err)
	}
	if args.View != "" && args.View != "before" && args.View != "after" {
		return nil, fmt.Errorf("tile_review_status: view %q must be \"before\" or \"after\"", args.View)
	}
	var st TileReviewStatusResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if args.View != "" || args.Focus != "" {
			if ed.Playing() {
				return editor.ErrPlaying
			}
			if !ed.TileReviewOpen() {
				return editor.ErrNoTileReview
			}
			if args.Focus != "" {
				if !ed.FocusTileReviewRegion(args.Focus) {
					return fmt.Errorf("tile review has no region %q", args.Focus)
				}
			}
			if args.View == "before" {
				ed.SetTileReviewViewMode(editor.TileReviewBefore)
			} else if args.View == "after" {
				ed.SetTileReviewViewMode(editor.TileReviewAfter)
			}
		}
		st = snapshotTileReviewStatus(ed)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("tile_review_status: %w", err)
	}
	return &Result{Text: formatTileReviewStatusText(st), Structured: st}, nil
}

func tileReviewToggleRegion(ctx context.Context, c *Call, toolName string, revert bool) (*Result, error) {
	var args struct {
		ID    string `json:"id"`
		Index *int   `json:"index"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("%s: %w", toolName, err)
	}
	if args.ID == "" && args.Index == nil {
		return nil, errors.New(toolName + ": pass either id or index")
	}
	if args.ID != "" && args.Index != nil {
		return nil, errors.New(toolName + ": pass only one of id or index, not both")
	}
	var st TileReviewStatusResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		var err error
		if args.ID != "" {
			err = ed.SetTileReviewRegionReverted(args.ID, revert)
		} else if revert {
			err = ed.RevertTileReviewRegionByIndex(*args.Index)
		} else {
			err = ed.RestoreTileReviewRegionByIndex(*args.Index)
		}
		if err != nil {
			return err
		}
		st = snapshotTileReviewStatus(ed)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("%s: %w", toolName, err)
	}
	return &Result{Text: formatTileReviewStatusText(st), Structured: st}, nil
}

func tileReviewRevertRegion(ctx context.Context, c *Call) (*Result, error) {
	return tileReviewToggleRegion(ctx, c, ToolTileReviewRevertRegion, true)
}

func tileReviewRestoreRegion(ctx context.Context, c *Call) (*Result, error) {
	return tileReviewToggleRegion(ctx, c, ToolTileReviewRestoreRegion, false)
}

func tileReviewKeep(ctx context.Context, c *Call) (*Result, error) {
	var args struct{}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tile_review_keep: %w", err)
	}
	var res editor.AgentTileEditResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		var err error
		res, err = ed.KeepTileReview()
		return err
	}); err != nil {
		return nil, fmt.Errorf("tile_review_keep: %w", err)
	}
	text := fmt.Sprintf("Kept %d of %d tile regions (%d cells) on %s as one undo step.",
		res.KeptRegions, res.RegionCount, res.ChangedCells, res.Layer)
	return &Result{Text: text, Structured: res}, nil
}

func tileReviewRevertAll(ctx context.Context, c *Call) (*Result, error) {
	var args struct{}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tile_review_revert_all: %w", err)
	}
	var layer string
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		layer, _ = ed.TileReviewLayer()
		return ed.RevertAllTileReview()
	}); err != nil {
		return nil, fmt.Errorf("tile_review_revert_all: %w", err)
	}
	st := TileReviewStatusResult{Open: false, Layer: layer}
	return &Result{Text: fmt.Sprintf("Reverted all pending tile changes on %s.", layer), Structured: st}, nil
}
