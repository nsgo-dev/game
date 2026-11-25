package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// TilePalette is what the tile palette shows for the active layer: the tiles of the tileset its inline tilemap names, in tile ID order, or, when there are none to pick, why.
type TilePalette struct {
	// Layer is the active layer's name.
	Layer string
	// Tileset is the atlas a single grid names, or the YAML tileset file a layered grid names; empty when the layer has no inline tilemap.
	Tileset string
	// TileWidth and TileHeight are the tilemap's tile size in points, which the palette shows each tile at.
	TileWidth, TileHeight int
	// TileLayers are the names of a layered grid's tile layers, in order, and TileLayer the one Paint, Erase, Fill and Terrain change (Editor.ActiveTileLayer, or the first); both are empty for a single grid.
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
	if tm.IsLayered() {
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
		p.Tiles = append(p.Tiles, PaletteTile{ID: id, Number: id, Image: atlas.Image, Frame: f})
	}
	slices.SortFunc(p.Tiles, func(a, b PaletteTile) int { return a.ID - b.ID })
	if len(p.Tiles) == 0 {
		p.Message = fmt.Sprintf("Tileset %q has no tile frames (frames named 1, 2, ...)", tm.Tileset)
	}
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
	}
	for id := range min(doc.Count(), maxPaletteTiles) {
		if f, ok := tilemap.TilesetFrame(doc, atlas, id); ok {
			p.Tiles = append(p.Tiles, PaletteTile{ID: id + 1, Number: id, Image: img, Frame: f})
		}
	}
	if len(p.Tiles) == 0 {
		p.Message = fmt.Sprintf("Tileset %q has no tiles", tm.TilesetFile)
	}
	return p
}

// isTileTool reports whether t paints, erases, fills, picks tiles or paints terrain.
func isTileTool(t Tool) bool {
	return t == ToolPaintTile || t == ToolEraseTile || t == ToolFillTile || t == ToolPickTile || t == ToolTerrain
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
		a.TileWidth == b.TileWidth && a.TileHeight == b.TileHeight &&
		slices.EqualFunc(a.Tiles, b.Tiles, func(x, y PaletteTile) bool {
			return x.ID == y.ID && x.Frame == y.Frame && sameImage(x.Image, y.Image)
		})
}

// sameImage reports whether a and b are the same image source: the same URI, version, pixels or canvas, and the same encoded bytes by identity rather than content.
func sameImage(a, b core.ImageSource) bool {
	if a.URI != b.URI || a.Version != b.Version || a.Pixels != b.Pixels || a.Offscreen != b.Offscreen || a.Size != b.Size || len(a.Data) != len(b.Data) {
		return false
	}
	return len(a.Data) == 0 || &a.Data[0] == &b.Data[0]
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

// TilePaletteView renders the tile palette panel, shown right of the viewport while a tile tool (Paint, Erase, Fill or Pick) is active in 2D: the active layer's tileset (Editor.TilePalette) as a grid of its tiles in ID order at the tilemap's tile size, scaled down only when a tile is wider than the panel. Pressing a tile, or moving between tiles with the arrow keys, makes it the tile Paint and Fill use (Editor.SetActiveTileID); as in base.RadioGroup, every arrow key steps through the tiles in ID order rather than by rows. The selected tile is ringed. A layer without tiles to pick shows why instead of an empty grid.
func TilePaletteView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	shown := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.ViewMode == ViewMode2D && isTileTool(ed.Tool)
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
	// The grid is built again only when the active layer's tiles change, not on every edit.
	palette := core.NewMemoFunc(cx, func() TilePalette {
		_ = rev.Get()
		return ed.TilePalette()
	}, samePalette)
	active := core.NewMemo(cx, func() int {
		_ = rev.Get()
		return ed.ActiveTileID
	})

	grid := core.Dynamic(func(cx *core.Scope) core.Node {
		p := palette.Get()
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
								ed.SelectObject("")
								ed.SetActiveLayer(p.Layer)
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
						Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text(), FlexGrow: 1},
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
			core.Dynamic(func(cx *core.Scope) core.Node {
				return tileLayerControls(ed, rev, palette.Get())
			}),
			ui.ScrollView{
				Style:    core.Style{FlexGrow: 1},
				Children: []core.Node{grid},
			},
		},
	}
}

// tileLayerControls returns the controls a layered grid's palette shows above its tiles: a toggle per tile layer that picks the one tile tools change, the flips Paint and Fill place tiles with, the terrain selector for ToolTerrain, and an Edit Tileset button. A single grid has none, and gets an empty view.
func tileLayerControls(ed *Editor, rev *core.Signal[uint64], p TilePalette) core.Node {
	if len(p.TileLayers) == 0 {
		return core.View{}
	}
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
	flips := []struct {
		text, label string
		bit         uint32
	}{
		{"H", ed.T("tilePalette.flipHoriz"), tilemap.FlipHorizontal},
		{"V", ed.T("tilePalette.flipVert"), tilemap.FlipVertical},
		{"D", ed.T("tilePalette.flipDiag"), tilemap.FlipDiagonal},
	}
	flipButtons := make([]core.Node, 0, len(flips))
	for _, f := range flips {
		flipButtons = append(flipButtons, ui.ToggleButton{
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
	children := []core.Node{
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4}, Children: layers},
		core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
			Children: append(flipButtons, ui.Button{
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
			}),
		},
	}
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
			Style:   core.Style{FontSize: 11},
		})
	}
	return core.View{
		TestID:   "tile-layer-controls",
		Style:    core.Style{Gap: 6},
		Children: children,
	}
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
