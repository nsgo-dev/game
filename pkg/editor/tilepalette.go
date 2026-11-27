package editor

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

type paletteFileImageCache struct {
	valid     bool
	uri       string
	filesRev  uint64
	assetsRev uint64
	resolver  sceneformat.AssetResolver
	data      []byte
	diskReads uint64
}

func sameAssetResolver(a, b sceneformat.AssetResolver) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb || !ta.Comparable() {
		return false
	}
	return a == b
}

func (e *Editor) cachedPaletteFileImage(uri string, fr sceneformat.FileResolver) []byte {
	c := &e.paletteImgCache
	if c.valid && c.uri == uri && c.filesRev == e.filesRev && c.assetsRev == e.assetsRev && sameAssetResolver(c.resolver, e.Resolver) {
		return c.data
	}
	c.diskReads++
	data, err := fr.ReadFile(uri)
	if err != nil {
		data = nil
	}
	c.valid = true
	c.uri = uri
	c.filesRev = e.filesRev
	c.assetsRev = e.assetsRev
	c.resolver = e.Resolver
	c.data = data
	return data
}

// TilePalette is what the tile palette shows for the active layer: the tiles of the tileset its inline tilemap names, in tile ID order, or, when there are none to pick, why.
type TilePalette struct {
	// Layer is the active layer's name.
	Layer string
	// Tileset is the atlas a single grid names, or the YAML tileset file a layered grid names; empty when the layer has no inline tilemap.
	Tileset string
	// TileWidth and TileHeight are the tilemap's tile size in points, which the palette shows each tile at.
	TileWidth, TileHeight int
	// Columns is the number of tiles per row in the palette grid (the YAML tileset's Columns when set, otherwise the panel's wrapped row width).
	Columns int
	// TileLayers are the names of a layered grid's tile layers, in order, and TileLayer the one tile tools change (Editor.ActiveTileLayer, or the first); both are empty for a single grid.
	TileLayers []string
	TileLayer  string
	// Terrains are the terrain sets defined in a layered grid's YAML tileset, and ActiveTerrain the one ToolTerrain paints (Editor.ActiveTerrain, or the first).
	Terrains      []PaletteTerrain
	ActiveTerrain string
	// Tiles are the tileset's tiles in ascending ID order.
	Tiles []PaletteTile
	// Message says why Tiles is empty, such as a layer without an inline tilemap or a tileset that does not resolve; it is empty when Tiles is not.
	Message string
}

// PaletteTerrain describes one terrain set shown in the tile palette for a layered grid's YAML tileset.
type PaletteTerrain struct {
	Name string
	Mode string
}

// PaletteTile is one tile of a TilePalette: for a single grid, a frame of the tileset atlas whose name is its tile ID ("1", "2", ...), as sceneformat looks tiles up; for a layered grid, a tile of its YAML tileset.
type PaletteTile struct {
	// ID is the tile ID painting writes: the value in a single grid's data, or a layered grid's tile's local ID plus 1, as PaintTile takes it.
	ID int
	// Number is the tile's number as the scene file writes it, which the palette and the status bar show: ID for a single grid, and the local ID (ID less 1) for a layered grid.
	Number int
	// Name is the tile's human-readable name from the YAML tileset (or atlas frame name), or empty when unnamed.
	Name string
	// Col and Row are the tile's 0-based atlas/sheet cell coordinates.
	Col, Row int
	// Collision summarizes the tile's collision rule ("solid", "one-way top", "shapes", or "none") for YAML tilesets.
	Collision string
	// Terrain names the terrain set this tile belongs to in a YAML tileset, if any.
	Terrain string
	// Image is the image the tile is drawn from: the tileset atlas's image, or a YAML tileset's grid image.
	Image core.ImageSource
	// Frame is the tile's frame in Image.
	Frame sprite.Frame
}

// maxPaletteTiles is the most tiles the palette lists from a layered grid's tileset.
const maxPaletteTiles = 1 << 14

// Has reports whether p holds a tile with the given ID.
func (p TilePalette) Has(id int) bool {
	return slices.ContainsFunc(p.Tiles, func(t PaletteTile) bool { return t.ID == id })
}

// Tile returns the tile of p with the given ID, and whether p holds one.
func (p TilePalette) Tile(id int) (PaletteTile, bool) {
	i := slices.IndexFunc(p.Tiles, func(t PaletteTile) bool { return t.ID == id })
	if i < 0 {
		return PaletteTile{}, false
	}
	return p.Tiles[i], true
}

func paletteWrapColumns(tileWidth, count int) int {
	if count <= 0 {
		return 0
	}
	cellW := min(max(tileWidth, 1), paletteTileLimit)
	boxW := cellW + 2*(paletteBorder+paletteTilePadding)
	const gap = 2
	cols := (paletteInner + gap) / (boxW + gap)
	return max(cols, 1)
}

// TilePalette returns the palette of the active layer: the frames of its inline tilemap's tileset atlas whose names are tile IDs, or for a layered grid the tiles of its YAML tileset (up to the first 16384), in ID order, at the tilemap's tile size. A layer with no inline tilemap (none, or an external Tiled or LDtk map, which the editor does not paint), a tileset the Resolver cannot resolve and a tileset with no tile frames give a palette with no tiles and a Message saying so. A layered grid's tileset is read through the Resolver's sceneformat.FileResolver, and its image through its sceneformat.ImageResolver when it has one.
func (e *Editor) TilePalette() TilePalette {
	p := TilePalette{Layer: e.ActiveLayer}
	li := e.layerIndex(e.ActiveLayer)
	if e.Doc == nil || li < 0 {
		p.Message = "No layer is selected"
		return p
	}
	tm := e.inlineTilemapForLayer(&e.Doc.Layers[li])
	if tm == nil {
		p.Message = fmt.Sprintf("Layer %q has no inline tilemap to paint", e.ActiveLayer)
		return p
	}
	if tm.HasTilesetFile() || tm.IsLayered() {
		return e.layeredPalette(p, tm)
	}
	p.Tileset, p.TileWidth, p.TileHeight = tm.Tileset, tm.TileWidth, tm.TileHeight
	if e.Resolver == nil {
		p.Message = fmt.Sprintf("Tileset %q cannot be resolved: the editor has no asset resolver", tm.Tileset)
		return p
	}
	atlas, err := e.Resolver.Atlas(tm.Tileset)
	if err != nil || atlas == nil {
		if err == nil {
			err = fmt.Errorf("the resolver returned no atlas")
		}
		p.Message = fmt.Sprintf("Tileset %q cannot be resolved: %v", tm.Tileset, err)
		return p
	}
	for name, f := range atlas.Frames {
		// sceneformat looks a tile up by strconv.Itoa of its ID, so only canonical positive integers name tiles.
		id, err := strconv.Atoi(name)
		if err != nil || id <= 0 || strconv.Itoa(id) != name {
			continue
		}
		col, row := 0, 0
		if tm.TileWidth > 0 {
			col = int(f.X) / tm.TileWidth
		}
		if tm.TileHeight > 0 {
			row = int(f.Y) / tm.TileHeight
		}
		p.Tiles = append(p.Tiles, PaletteTile{ID: id, Number: id, Col: col, Row: row, Image: atlas.Image, Frame: f})
	}
	slices.SortFunc(p.Tiles, func(a, b PaletteTile) int { return a.ID - b.ID })
	if len(p.Tiles) == 0 {
		p.Message = fmt.Sprintf("Tileset %q has no tile frames (frames named 1, 2, ...)", tm.Tileset)
		return p
	}
	p.Columns = paletteWrapColumns(p.TileWidth, len(p.Tiles))
	return p
}

// layeredPalette fills p with the tiles of the YAML tileset the layered grid tm names.
func (e *Editor) layeredPalette(p TilePalette, tm *sceneformat.TilemapDoc) TilePalette {
	p.Tileset, p.TileWidth, p.TileHeight = tm.TilesetFile, tm.TileWidth, tm.TileHeight
	for _, l := range tm.TileLayers {
		p.TileLayers = append(p.TileLayers, l.Name)
	}
	p.TileLayer = e.tileLayerName(tm)
	doc, err := e.loadTilesetDoc(tm.TilesetFile)
	if err != nil {
		p.Message = fmt.Sprintf("Tileset %q cannot be read: %v", tm.TilesetFile, err)
		return p
	}
	for _, td := range doc.Terrains {
		p.Terrains = append(p.Terrains, PaletteTerrain{Name: td.Name, Mode: td.Mode})
	}
	if len(p.Terrains) > 0 {
		p.ActiveTerrain = p.Terrains[0].Name
		if e.ActiveTerrain != "" && slices.ContainsFunc(p.Terrains, func(t PaletteTerrain) bool { return t.Name == e.ActiveTerrain }) {
			p.ActiveTerrain = e.ActiveTerrain
		}
	}
	img := core.ImageSource{URI: doc.Image}
	var atlas *sprite.Atlas
	if doc.Atlas != "" {
		if e.Resolver == nil {
			p.Message = fmt.Sprintf("Tileset %q atlas %q cannot be resolved: the editor has no asset resolver", tm.TilesetFile, doc.Atlas)
			return p
		}
		if atlas, err = e.Resolver.Atlas(doc.Atlas); err == nil && atlas == nil {
			err = fmt.Errorf("the resolver returned no atlas")
		}
		if err != nil {
			p.Message = fmt.Sprintf("Tileset %q atlas %q cannot be resolved: %v", tm.TilesetFile, doc.Atlas, err)
			return p
		}
		img = atlas.Image
	} else if ir, ok := e.Resolver.(sceneformat.ImageResolver); ok {
		if img, err = ir.Image(doc.Image); err != nil {
			p.Message = fmt.Sprintf("Tileset %q image %q cannot be resolved: %v", tm.TilesetFile, doc.Image, err)
			return p
		}
	} else if fr, ok := e.Resolver.(sceneformat.FileResolver); ok {
		if data := e.cachedPaletteFileImage(doc.Image, fr); len(data) > 0 {
			img = core.ImageSource{URI: doc.Image, Data: data}
		}
	}
	metaByID := make(map[int]tilemap.TileDoc, len(doc.Tiles))
	for _, td := range doc.Tiles {
		metaByID[td.ID] = td
	}
	for id := range min(doc.Count(), maxPaletteTiles) {
		if f, ok := tilemap.TilesetFrame(doc, atlas, id); ok {
			col, row := 0, 0
			if doc.Columns > 0 {
				col, row = id%doc.Columns, id/doc.Columns
			} else {
				if doc.TileWidth > 0 {
					col = int(f.X) / doc.TileWidth
				}
				if doc.TileHeight > 0 {
					row = int(f.Y) / doc.TileHeight
				}
			}
			td := metaByID[id]
			name := td.Name
			if name == "" && doc.Atlas != "" && id < len(doc.Frames) {
				name = doc.Frames[id]
			}
			coll := "none"
			switch {
			case td.OneWay:
				coll = "one-way top"
			case td.Solid:
				coll = "solid"
			case len(td.Shapes) > 0:
				coll = "shapes"
			}
			p.Tiles = append(p.Tiles, PaletteTile{
				ID:        id + 1,
				Number:    id,
				Name:      name,
				Col:       col,
				Row:       row,
				Collision: coll,
				Terrain:   td.Terrain,
				Image:     img,
				Frame:     f,
			})
		}
	}
	if len(p.Tiles) == 0 {
		p.Message = fmt.Sprintf("Tileset %q has no tiles", tm.TilesetFile)
		return p
	}
	if doc.Columns > 0 {
		p.Columns = doc.Columns
	} else {
		p.Columns = paletteWrapColumns(p.TileWidth, len(p.Tiles))
	}
	return p
}

// isTileTool reports whether t edits or picks tiles or paints terrain on an inline tilemap.
func isTileTool(t Tool) bool {
	return t == ToolPaintTile || t == ToolEraseTile || t == ToolFillTile || t == ToolPickTile || t == ToolTerrain ||
		t == ToolRectTile || t == ToolLineTile || t == ToolStampTile || t == ToolRandomTile
}

// keepTileInPalette selects the first tile of the active layer's palette when the active tile ID is not one of its tiles, and selects the first terrain when ActiveTerrain is not one of its terrains, so the palette follows the active layer.
func (e *Editor) keepTileInPalette() {
	p := e.TilePalette()
	if len(p.Tiles) > 0 && !p.Has(e.ActiveTileID) {
		e.ActiveTileID = p.Tiles[0].ID
	}
	if len(p.Terrains) > 0 && !slices.ContainsFunc(p.Terrains, func(t PaletteTerrain) bool { return t.Name == e.ActiveTerrain }) {
		e.ActiveTerrain = p.Terrains[0].Name
	}
}

// samePalette reports whether a and b show the same tiles, so that edits that leave the active layer's tiles as they were keep the palette's grid mounted.
func samePalette(a, b TilePalette) bool {
	return a.Layer == b.Layer && a.Tileset == b.Tileset && a.Message == b.Message &&
		a.TileLayer == b.TileLayer && slices.Equal(a.TileLayers, b.TileLayers) &&
		a.ActiveTerrain == b.ActiveTerrain && slices.Equal(a.Terrains, b.Terrains) &&
		a.TileWidth == b.TileWidth && a.TileHeight == b.TileHeight && a.Columns == b.Columns &&
		slices.EqualFunc(a.Tiles, b.Tiles, func(x, y PaletteTile) bool {
			return x.ID == y.ID && x.Name == y.Name && x.Col == y.Col && x.Row == y.Row &&
				x.Collision == y.Collision && x.Terrain == y.Terrain &&
				x.Frame == y.Frame && sameImage(x.Image, y.Image)
		})
}

// sameImage reports whether a and b are the same image source: the same URI, version, pixels or canvas, and the same encoded bytes by identity rather than content.
func sameImage(a, b core.ImageSource) bool {
	if a.URI != b.URI || a.Version != b.Version || a.Pixels != b.Pixels || a.Offscreen != b.Offscreen || a.Size != b.Size || len(a.Data) != len(b.Data) {
		return false
	}
	return len(a.Data) == 0 || &a.Data[0] == &b.Data[0] || bytes.Equal(a.Data, b.Data)
}

// paletteWidth is the tile palette panel's width in points, and paletteInner the width its tiles wrap in: the panel less its padding on both sides and its left border, which NSGo lays out inside the panel's box.
const (
	paletteWidth   = 224
	palettePadding = 10
	paletteInner   = paletteWidth - 2*palettePadding - 1
	// paletteBorder is the width of the ring around each tile, which shows the selected one, and paletteTilePadding the gap between the ring and the tile's picture.
	paletteBorder      = 2
	paletteTilePadding = 2
	// paletteTileLimit is the widest a tile's picture shows at: a tile's ring and padding sit inside its box, so the box stays within paletteInner.
	paletteTileLimit = paletteInner - 2*(paletteBorder+paletteTilePadding)
)

// TilePaletteView renders the tile palette panel, shown right of the viewport while a tile tool is active in 2D: the active layer's tileset (Editor.TilePalette) as a grid of its tiles in ID order at the tilemap's tile size, scaled down only when a tile is wider than the panel. Pressing a tile, or moving between tiles with the arrow keys, makes it the tile Paint, Fill, Rect and Line use (Editor.SetActiveTileID); as in base.RadioGroup, every arrow key steps through the tiles in ID order rather than by rows. With Stamp active, pressing tiles selects a 1x1 or rectangular pattern (Shift or the Rect toggle extends from the anchor tile) alongside Copy/Cut/Paste controls; with Random active, pressing tiles toggles them in the weighted random pool alongside Seed and weight controls. A layer without tiles to pick shows why instead of an empty grid.
func TilePaletteView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	shown := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.ViewMode == ViewMode2D && (isTileTool(ed.Tool) || (!ed.Playing() && ed.TileReviewOpen()))
	})
	return core.Show{
		When: shown.Get,
		Then: func(cx *core.Scope) core.Node {
			return tilePalettePanel(cx, ed, rev)
		},
	}
}

func tilePalettePanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	// The grid is built again only when the active layer's tiles or brush interaction mode change, not on every edit.
	palette := core.NewMemoFunc(cx, func() TilePalette {
		_ = rev.Get()
		return ed.TilePalette()
	}, samePalette)
	active := core.NewMemo(cx, func() int {
		_ = rev.Get()
		return ed.ActiveTileID
	})
	brushMode := core.NewMemo(cx, func() Tool {
		_ = rev.Get()
		switch ed.Tool {
		case ToolStampTile, ToolRandomTile:
			return ed.Tool
		default:
			return ToolPaintTile
		}
	})
	rangeSelect := core.NewSignal(cx, false)
	shiftHeld := func() bool { return core.Untracked(cx, func() bool { return core.KeyHeld(cx, "shift") }) }

	grid := core.Dynamic(func(cx *core.Scope) core.Node {
		p := palette.Get()
		mode := brushMode.Get()
		if len(p.Tiles) == 0 {
			emptyText := core.Text{
				TestID:  "tile-palette-empty",
				Content: p.Message,
				Style:   core.Style{FontSize: 12, Color: colors.Muted()},
			}
			if li := ed.layerIndex(p.Layer); li >= 0 && ed.Doc.Layers[li].Tilemap == nil && !ed.inheritedLayers()[p.Layer] {
				return core.View{
					Style: core.Style{Gap: 8},
					Children: []core.Node{
						emptyText,
						ui.Button{
							TestID:  "tile-palette-setup-layer",
							Text:    ed.T("tilePalette.setupTilemap"),
							Size:    ui.SizeSmall,
							Variant: ui.ButtonSecondary,
							Disabled: func() bool {
								_ = rev.Get()
								return ed.Playing()
							},
							OnPress: func() {
								if ed.activeTab >= 0 && ed.activeTab < len(ed.tabs) {
									ed.tabs[ed.activeTab].placeholder = false
								}
								ed.SelectObject("")
								ed.SetActiveLayer(p.Layer)
								ed.SetRightRailTab(RightTabInspector)
								ed.notify()
							},
						},
					},
				}
			}
			return emptyText
		}
		// A tile wider than the panel is scaled down to fit it; others show at the tilemap's tile size.
		cellW, cellH := float32(max(p.TileWidth, 1)), float32(max(p.TileHeight, 1))
		if limit := float32(paletteTileLimit); cellW > limit {
			cellH *= limit / cellW
			cellW = limit
		}
		if mode == ToolStampTile || mode == ToolRandomTile {
			tiles := make([]core.Node, 0, len(p.Tiles))
			for idx, t := range p.Tiles {
				tile := t
				col, row := idx%max(p.Columns, 1), idx/max(p.Columns, 1)
				selected := func() bool {
					_ = rev.Get()
					if mode == ToolRandomTile {
						return slices.ContainsFunc(ed.randomTiles, func(w WeightedTile) bool { return w.TileID == tile.ID })
					}
					if ed.hasPaletteStampRect {
						r := ed.paletteStampRect
						return col >= r.Col0 && col <= r.Col1 && row >= r.Row0 && row <= r.Row1
					}
					return ed.paletteStampAnchor == tile.ID
				}
				tiles = append(tiles, base.Button{
					TestID: fmt.Sprintf("tile-palette-%d", tile.ID),
					Label:  fmt.Sprintf("Tile %d", tile.Number),
					OnPress: func(ev core.PressEvent) {
						if mode == ToolRandomTile {
							ed.ToggleRandomBrushTile(tile.ID)
							return
						}
						if (ev.Modifiers.Shift || shiftHeld() || rangeSelect.Get()) && ed.paletteStampAnchor > 0 {
							if _, err := ed.SelectPaletteStampRange(ed.paletteStampAnchor, tile.ID); err != nil {
								ed.report(err)
							}
							rangeSelect.Set(false)
							return
						}
						if _, err := ed.SelectPaletteStamp(col, row, col, row); err != nil {
							ed.report(err)
						}
					},
					StyleFunc: func() core.Style {
						ring := colors.Inset()
						if selected() {
							ring = colors.Accent()
						}
						return core.Style{
							Padding:      core.All(paletteTilePadding),
							BorderWidth:  paletteBorder,
							BorderColor:  ring,
							CornerRadius: 3,
							FocusVisible: ui.FocusRing(cx, 2),
						}
					},
					Children: []core.Node{core.Canvas{
						TestID: fmt.Sprintf("tile-palette-thumb-%d", tile.ID),
						Style: core.Style{
							Width:      core.Pt(core.Points(cellW)),
							Height:     core.Pt(core.Points(cellH)),
							Background: colors.Surface(),
						},
						Draw: func(d *core.Drawing, size core.Size) {
							drawPaletteTile(d, tile, float32(size.Width), float32(size.Height))
						},
					}},
				})
			}
			return core.View{
				TestID:   "tile-palette-grid",
				Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 2},
				Children: tiles,
			}
		}

		tiles := make([]core.Node, 0, len(p.Tiles))
		for _, t := range p.Tiles {
			tile := t
			tiles = append(tiles, base.Radio{
				TestID: fmt.Sprintf("tile-palette-%d", tile.ID),
				Label:  ed.T("tilePalette.tileLabel", l10n.Param("number", tile.Number)),
				Checked: func() bool {
					return active.Get() == tile.ID
				},
				OnChange: func(checked bool) {
					if checked {
						ed.SetActiveTileID(tile.ID)
					}
				},
				StyleFunc: func() core.Style {
					ring := colors.Inset()
					if active.Get() == tile.ID {
						ring = colors.Accent()
					}
					return core.Style{
						Padding:      core.All(paletteTilePadding),
						BorderWidth:  paletteBorder,
						BorderColor:  ring,
						CornerRadius: 3,
						FocusVisible: ui.FocusRing(cx, 2),
					}
				},
				Children: []core.Node{core.Canvas{
					TestID: fmt.Sprintf("tile-palette-thumb-%d", tile.ID),
					Style: core.Style{
						Width:      core.Pt(core.Points(cellW)),
						Height:     core.Pt(core.Points(cellH)),
						Background: colors.Surface(),
					},
					Draw: func(d *core.Drawing, size core.Size) {
						drawPaletteTile(d, tile, float32(size.Width), float32(size.Height))
					},
				}},
			})
		}
		return base.RadioGroup{
			TestID:   "tile-palette-grid",
			Label:    ed.T("tilePalette.gridLabel", l10n.Param("tileset", p.Tileset)),
			Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 2},
			Children: tiles,
		}
	})

	return core.View{
		TestID: "tile-palette",
		Style: core.Style{
			Width:       core.Pt(paletteWidth),
			Background:  colors.Inset(),
			Borders:     core.Edges{Left: 1},
			BorderColor: colors.Line(),
			Padding:     core.All(palettePadding),
			Gap:         8,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.Text{
						TestID:  "tile-palette-title",
						Content: ed.T("tilePalette.title"),
						Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Muted(), FlexGrow: 1},
					},
					core.Text{
						TestID: "tile-palette-tileset",
						ContentFunc: func() string {
							return palette.Get().Tileset
						},
						Style: core.Style{FontSize: 11, Color: colors.Muted()},
					},
				},
			},
			tileBrushBar(ed, rev),
			core.Dynamic(func(cx *core.Scope) core.Node {
				return tileLayerControls(cx, ed, rev, palette.Get())
			}),
			core.Dynamic(func(cx *core.Scope) core.Node {
				switch brushMode.Get() {
				case ToolStampTile:
					return tileStampControls(cx, ed, rev, rangeSelect)
				case ToolRandomTile:
					return tileRandomControls(cx, ed, rev)
				default:
					return core.View{}
				}
			}),
			ui.ScrollView{
				Style:    core.Style{FlexGrow: 1},
				Children: []core.Node{grid},
			},
			tilePaletteSelectedCard(cx, ed, palette, active),
		},
	}
}

func tileBrushBar(ed *Editor, rev *core.Signal[uint64]) core.Node {
	notPlaying := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	brushBtn := func(testID, text, label string, tool Tool) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     text,
			Label:    label,
			Disabled: notPlaying,
			On: func() bool {
				_ = rev.Get()
				return ed.Tool == tool
			},
			OnChange: func(on bool) {
				if on {
					ed.SetTool(tool)
				}
			},
		}
	}
	return core.View{
		TestID: "tile-brush-bar",
		Style:  core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
		Children: []core.Node{
			brushBtn("tool-rect", "Rect", "Rectangle tile brush (U)", ToolRectTile),
			brushBtn("tool-line", "Line", "Line tile brush (L)", ToolLineTile),
			brushBtn("tool-stamp", "Stamp", "Stamp tile brush (C)", ToolStampTile),
			brushBtn("tool-random", "Random", "Random tile brush (N)", ToolRandomTile),
		},
	}
}

func tileStampControls(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], rangeSelect *core.Signal[bool]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "tile-stamp-controls",
		Style:  core.Style{Gap: 6},
		Children: []core.Node{
			core.Text{
				TestID: "tile-stamp-info",
				ContentFunc: func() string {
					_ = rev.Get()
					if s, ok := ed.TileStamp(); ok {
						return fmt.Sprintf("Stamp: %dx%d", s.Width, s.Height)
					}
					return "Stamp: 1x1 (right-drag map to select)"
				},
				Style: core.Style{FontSize: 11, Color: colors.Muted()},
			},
			core.View{
				Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
				Children: []core.Node{
					ui.Button{
						TestID:  "tile-stamp-copy",
						Text:    "Copy",
						Label:   "Copy selected tile region",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || !ed.hasTileSel
						},
						OnPress: func() {
							if _, err := ed.CopySelectedTileRegion(); err != nil {
								ed.report(err)
							}
						},
					},
					ui.Button{
						TestID:  "tile-stamp-cut",
						Text:    "Cut",
						Label:   "Cut selected tile region",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || !ed.hasTileSel
						},
						OnPress: func() {
							if _, err := ed.CutSelectedTileRegion(); err != nil {
								ed.report(err)
							}
						},
					},
					ui.Button{
						TestID:  "tile-stamp-paste",
						Text:    "Paste",
						Label:   "Paste tile clipboard",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || ed.tileClipboard.Empty()
						},
						OnPress: func() {
							if err := ed.ExecuteCommand(CmdPasteTileRegion); err != nil {
								ed.report(err)
							}
						},
					},
					ui.ToggleButton{
						TestID: "tile-stamp-range",
						Text:   "Rect",
						Label:  "Extend palette stamp rectangle on next tile press",
						On:     rangeSelect.Get,
						OnChange: func(on bool) {
							rangeSelect.Set(on)
						},
					},
					ui.Button{
						TestID:  "tile-stamp-clear",
						Text:    "Clear",
						Label:   "Clear active stamp and selection",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.activeStamp.Empty() && !ed.hasTileSel
						},
						OnPress: func() {
							ed.ClearTileStamp()
							ed.ClearTileSelection()
							rangeSelect.Set(false)
						},
					},
				},
			},
		},
	}
}

func tileRandomControls(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	seedDraft := core.NewSignal(cx, strconv.FormatUint(ed.RandomBrushSeed(), 10))
	cx.Effect(func() {
		_ = rev.Get()
		seedDraft.Set(strconv.FormatUint(ed.RandomBrushSeed(), 10))
	})
	commitSeed := func(text string) {
		raw := strings.TrimSpace(text)
		if raw == "" {
			seedDraft.Set(strconv.FormatUint(ed.RandomBrushSeed(), 10))
			return
		}
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			ed.report(fmt.Errorf("editor: invalid random seed %q", raw))
			seedDraft.Set(strconv.FormatUint(ed.RandomBrushSeed(), 10))
			return
		}
		ed.SetRandomBrushSeed(v)
	}
	poolSig := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		for _, w := range ed.randomTiles {
			fmt.Fprintf(&b, "%d:%g;", w.TileID, w.Weight)
		}
		return b.String()
	})
	return core.View{
		TestID: "tile-random-controls",
		Style:  core.Style{Gap: 6},
		Children: []core.Node{
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.Text{
						Content: "Seed",
						Style:   core.Style{FontSize: 11, Color: colors.Muted()},
					},
					ui.Input{
						TestID:   "tile-random-seed",
						Label:    "Random brush seed",
						Value:    seedDraft.Get,
						OnChange: func(v string) { seedDraft.Set(v) },
						OnSubmit: commitSeed,
						OnBlur:   func() { commitSeed(seedDraft.Get()) },
						Style:    core.Style{FlexGrow: 1},
					},
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = poolSig.Get()
				pool := ed.RandomBrushTiles()
				if len(pool) == 0 {
					return core.Text{
						TestID:  "tile-random-empty",
						Content: "Click tiles below to build weighted pool",
						Style:   core.Style{FontSize: 11, Color: colors.Muted()},
					}
				}
				rows := make([]core.Node, 0, len(pool))
				for _, entry := range pool {
					tid := entry.TileID
					weight := entry.Weight
					rows = append(rows, ui.HStack{
						Gap: 4,
						Children: []core.Node{
							core.Text{
								TestID:  fmt.Sprintf("tile-random-entry-%d", tid),
								Content: fmt.Sprintf("#%d ×%.1f", tid, weight),
								Style:   core.Style{FontSize: 11, Color: colors.Text(), FlexGrow: 1},
							},
							ui.Button{
								TestID:  fmt.Sprintf("tile-random-dec-%d", tid),
								Text:    "-",
								Label:   fmt.Sprintf("Decrease weight for tile %d", tid),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								OnPress: func() {
									next := max(weight-0.5, 0.5)
									ed.report(ed.SetRandomBrushTileWeight(tid, next))
								},
							},
							ui.Button{
								TestID:  fmt.Sprintf("tile-random-inc-%d", tid),
								Text:    "+",
								Label:   fmt.Sprintf("Increase weight for tile %d", tid),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								OnPress: func() {
									ed.report(ed.SetRandomBrushTileWeight(tid, weight+0.5))
								},
							},
							ui.Button{
								TestID:  fmt.Sprintf("tile-random-remove-%d", tid),
								Text:    "×",
								Label:   fmt.Sprintf("Remove tile %d from random pool", tid),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								OnPress: func() {
									ed.ToggleRandomBrushTile(tid)
								},
							},
						},
					})
				}
				return core.View{
					TestID:   "tile-random-pool",
					Style:    core.Style{Gap: 2},
					Children: rows,
				}
			}),
		},
	}
}

// tileLayerControls returns the controls a YAML-tileset grid's palette shows above its tiles: a toggle per tile layer (for layered grids), the flips tile brushes place tiles with, the terrain selector for ToolTerrain, an Edit Tileset button, and the palette note from Tiles.dc.html. A legacy atlas single grid has none, and gets an empty view.
func tileLayerControls(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p TilePalette) core.Node {
	if len(p.TileLayers) == 0 && !isTilesetFile(p.Tileset) {
		return core.View{}
	}
	spec := ed.SpecPalette(cx)
	var children []core.Node
	if len(p.TileLayers) > 0 {
		layers := make([]core.Node, 0, len(p.TileLayers))
		for _, name := range p.TileLayers {
			layers = append(layers, ui.ToggleButton{
				TestID: "tile-layer-" + name,
				Text:   name,
				Label:  ed.T("tilePalette.paintLayer", l10n.Param("name", name)),
				On:     func() bool { return p.TileLayer == name },
				OnChange: func(on bool) {
					if on {
						ed.SetActiveTileLayer(name)
					}
				},
			})
		}
		children = append(children, core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4}, Children: layers})
	}
	var rowChildren []core.Node
	if len(p.TileLayers) > 0 {
		flips := []struct {
			text, label string
			bit         uint32
		}{
			{"H", ed.T("tilePalette.flipHoriz"), tilemap.FlipHorizontal},
			{"V", ed.T("tilePalette.flipVert"), tilemap.FlipVertical},
			{"D", ed.T("tilePalette.flipDiag"), tilemap.FlipDiagonal},
		}
		for _, f := range flips {
			rowChildren = append(rowChildren, ui.ToggleButton{
				TestID: "tile-flip-" + strings.ToLower(f.text),
				Text:   f.text,
				Label:  f.label,
				On: func() bool {
					_ = rev.Get()
					return ed.ActiveTileFlip&f.bit != 0
				},
				OnChange: func(on bool) {
					if on {
						ed.SetActiveTileFlip(ed.ActiveTileFlip | f.bit)
					} else {
						ed.SetActiveTileFlip(ed.ActiveTileFlip &^ f.bit)
					}
				},
			})
		}
	}
	rowChildren = append(rowChildren, ui.Button{
		TestID:  "tile-palette-edit-tileset",
		Text:    "Edit Tileset",
		Label:   "Open this layer's tileset in the Tileset Editor",
		Size:    ui.SizeSmall,
		Variant: ui.ButtonSecondary,
		Disabled: func() bool {
			_ = rev.Get()
			return ed.Playing()
		},
		OnPress: func() {
			ed.report(ed.OpenActiveLayerTileset())
		},
	})
	children = append(children, core.View{
		Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
		Children: rowChildren,
	})
	if len(p.Terrains) > 0 {
		tBtns := make([]core.Node, 0, len(p.Terrains))
		for _, t := range p.Terrains {
			terrain := t
			modeTag := "4-bit"
			if terrain.Mode == "blob" {
				modeTag = "8-bit"
			}
			tBtns = append(tBtns, ui.ToggleButton{
				TestID: "tile-terrain-" + terrain.Name,
				Text:   fmt.Sprintf("%s (%s)", terrain.Name, modeTag),
				Label:  "Paint terrain " + terrain.Name,
				On: func() bool {
					return p.ActiveTerrain == terrain.Name
				},
				OnChange: func(on bool) {
					if on {
						ed.SetActiveTerrain(terrain.Name)
					}
				},
			})
		}
		children = append(children, core.View{
			TestID:   "tile-terrain-controls",
			Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
			Children: tBtns,
		})
	} else if ed.Tool == ToolTerrain {
		children = append(children, core.Text{
			TestID:  "tile-terrain-empty",
			Content: "No terrain sets defined. Press Edit Tileset to add one.",
			Style:   core.Style{FontSize: 11, Color: spec.TextFaint},
		})
	}
	children = append(children, core.Text{
		TestID:  "tile-palette-rules-note",
		Content: "Rules for collision, terrain and variants live in the tileset file.",
		Style:   core.Style{FontSize: 11, Color: spec.TextFaint},
	})
	return core.View{
		TestID:   "tile-layer-controls",
		Style:    core.Style{Gap: 6},
		Children: children,
	}
}

// tilePaletteSelectedCard renders the picked tile's swatch, name, atlas cell coordinates, and rules at the bottom of the tile palette.
func tilePaletteSelectedCard(cx *core.Scope, ed *Editor, palette *core.Memo[TilePalette], active *core.Memo[int]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := palette.Get()
		tile, ok := p.Tile(active.Get())
		if !ok {
			return core.View{}
		}
		spec := ed.SpecPalette(cx)
		name := tile.Name
		if name == "" {
			name = fmt.Sprintf("Tile %d", tile.Number)
		}
		var parts []string
		parts = append(parts, fmt.Sprintf("cell %d, %d", tile.Col, tile.Row))
		if tile.Collision != "" {
			parts = append(parts, tile.Collision)
		}
		if tile.Terrain != "" {
			parts = append(parts, tile.Terrain)
		}
		meta := strings.Join(parts, " · ")

		return core.View{
			TestID: "tile-palette-selected",
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          10,
				Padding:      core.All(8),
				CornerRadius: 10,
				Background:   spec.Raised,
				BorderWidth:  1,
				BorderColor:  spec.LineRaised,
			},
			Children: []core.Node{
				core.Canvas{
					TestID: "tile-palette-selected-swatch",
					Style: core.Style{
						Width:        core.Pt(28),
						Height:       core.Pt(28),
						FlexShrink:   0,
						CornerRadius: 6,
						Background:   spec.Panel,
					},
					Draw: func(d *core.Drawing, size core.Size) {
						drawPaletteTile(d, tile, float32(size.Width), float32(size.Height))
					},
				},
				core.View{
					Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2},
					Children: []core.Node{
						core.Text{
							TestID:  "tile-palette-selected-name",
							Content: name,
							Style:   core.Style{FontSize: 11.5, FontWeight: 500, Color: spec.TextPrimary},
						},
						core.Text{
							TestID:  "tile-palette-selected-meta",
							Content: meta,
							Style:   core.Style{FontSize: 11, Color: spec.TextFaint},
						},
					},
				},
			},
		}
	})
}

// drawPaletteTile draws tile's untrimmed picture fitted to a w by h cell, its trimmed part at its offset inside it.
func drawPaletteTile(d *core.Drawing, tile PaletteTile, w, h float32) {
	f := tile.Frame
	fullW, fullH := f.Width, f.Height
	if fullW <= 0 || fullH <= 0 {
		fullW, fullH = f.W, f.H
	}
	if f.W <= 0 || f.H <= 0 || fullW <= 0 || fullH <= 0 || w <= 0 || h <= 0 {
		return
	}
	scale := min(w/fullW, h/fullH)
	ox, oy := (w-fullW*scale)/2, (h-fullH*scale)/2
	d.DrawImageFrom(tile.Image, f.X, f.Y, f.W, f.H, ox+f.OffsetX*scale, oy+f.OffsetY*scale, f.W*scale, f.H*scale)
}
