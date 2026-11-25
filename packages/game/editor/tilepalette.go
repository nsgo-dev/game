package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

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
	// TileLayers are the names of a layered grid's tile layers, in order, and TileLayer the one Paint, Erase and Fill change (Editor.ActiveTileLayer, or the first); both are empty for a single grid.
	TileLayers []string
	TileLayer  string
	// Tiles are the tileset's tiles in ascending ID order.
	Tiles []PaletteTile
	// Message says why Tiles is empty, such as a layer without an inline tilemap or a tileset that does not resolve; it is empty when Tiles is not.
	Message string
}

// PaletteTile is one tile of a TilePalette: for a single grid, a frame of the tileset atlas whose name is its tile ID ("1", "2", ...), as sceneformat looks tiles up; for a layered grid, a tile of its YAML tileset.
type PaletteTile struct {
	// ID is the tile ID painting writes: the value in a single grid's data, or a layered grid's tile's local ID plus 1, as PaintTile takes it.
	ID int
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
		p.Tiles = append(p.Tiles, PaletteTile{ID: id, Image: atlas.Image, Frame: f})
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
	fr, ok := e.Resolver.(sceneformat.FileResolver)
	if !ok {
		p.Message = fmt.Sprintf("Tileset %q cannot be read: the editor's resolver does not read project files (sceneformat.FileResolver)", tm.TilesetFile)
		return p
	}
	data, err := fr.ReadFile(tm.TilesetFile)
	var doc *tilemap.TilesetDoc
	if err == nil {
		doc, err = tilemap.ParseTileset(data)
	}
	if err != nil {
		p.Message = fmt.Sprintf("Tileset %q cannot be read: %v", tm.TilesetFile, err)
		return p
	}
	img := core.ImageSource{URI: doc.Image}
	var atlas *sprite.Atlas
	if doc.Atlas != "" {
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
			p.Tiles = append(p.Tiles, PaletteTile{ID: id + 1, Image: img, Frame: f})
		}
	}
	if len(p.Tiles) == 0 {
		p.Message = fmt.Sprintf("Tileset %q has no tiles", tm.TilesetFile)
	}
	return p
}

// isTileTool reports whether t paints, erases, fills or picks tiles.
func isTileTool(t Tool) bool {
	return t == ToolPaintTile || t == ToolEraseTile || t == ToolFillTile || t == ToolPickTile
}

// keepTileInPalette selects the first tile of the active layer's palette when the active tile ID is not one of its tiles, so the palette follows a layer whose tileset lacks the tile that was selected. It leaves the ID alone when the palette has no tiles.
func (e *Editor) keepTileInPalette() {
	p := e.TilePalette()
	if len(p.Tiles) == 0 || p.Has(e.ActiveTileID) {
		return
	}
	e.ActiveTileID = p.Tiles[0].ID
}

// samePalette reports whether a and b show the same tiles, so that edits that leave the active layer's tiles as they were keep the palette's grid mounted.
func samePalette(a, b TilePalette) bool {
	return a.Layer == b.Layer && a.Tileset == b.Tileset && a.Message == b.Message &&
		a.TileLayer == b.TileLayer && slices.Equal(a.TileLayers, b.TileLayers) &&
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
			return core.Text{
				TestID:  "tile-palette-empty",
				Content: p.Message,
				Style:   core.Style{FontSize: 12, Color: colors.Muted()},
			}
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
				Label:  fmt.Sprintf("Tile %d", tile.ID),
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
			Label:    fmt.Sprintf("Tiles of %s", p.Tileset),
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
						Content: "Tiles",
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
				return tileLayerControls(ed, palette.Get())
			}),
			ui.ScrollView{
				Style:    core.Style{FlexGrow: 1},
				Children: []core.Node{grid},
			},
		},
	}
}

// tileLayerControls returns the controls a layered grid's palette shows above its tiles: a toggle per tile layer that picks the one tile tools change, and the flips Paint and Fill place tiles with. A single grid has neither, and gets an empty view.
func tileLayerControls(ed *Editor, p TilePalette) core.Node {
	if len(p.TileLayers) == 0 {
		return core.View{}
	}
	layers := make([]core.Node, 0, len(p.TileLayers))
	for _, name := range p.TileLayers {
		layers = append(layers, ui.ToggleButton{
			TestID: "tile-layer-" + name,
			Text:   name,
			Label:  "Paint tile layer " + name,
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
		{"H", "Flip horizontally", tilemap.FlipHorizontal},
		{"V", "Flip vertically", tilemap.FlipVertical},
		{"D", "Flip diagonally", tilemap.FlipDiagonal},
	}
	flipButtons := make([]core.Node, 0, len(flips))
	for _, f := range flips {
		flipButtons = append(flipButtons, ui.ToggleButton{
			TestID: "tile-flip-" + strings.ToLower(f.text),
			Text:   f.text,
			Label:  f.label,
			On:     func() bool { return ed.ActiveTileFlip&f.bit != 0 },
			OnChange: func(on bool) {
				if on {
					ed.SetActiveTileFlip(ed.ActiveTileFlip | f.bit)
				} else {
					ed.SetActiveTileFlip(ed.ActiveTileFlip &^ f.bit)
				}
			},
		})
	}
	return core.View{
		TestID: "tile-layer-controls",
		Style:  core.Style{Gap: 6},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4}, Children: layers},
			core.View{Style: core.Style{Direction: core.Row, Gap: 4}, Children: flipButtons},
		},
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
