package tilemap

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// TilesetFormatVersion is the schema version of the YAML tileset format (*.tileset.yaml) that ParseTileset reads and EncodeTileset writes.
const TilesetFormatVersion = 1

// TilesetSchemaURL is where the JSON Schema of version 1 tileset documents is published. EncodeTileset names it in TilesetSchemaComment on the first line of every file.
const TilesetSchemaURL = "https://game.nsgo.dev/schemas/tileset.v1.json"

// TilesetSchemaComment is the first line EncodeTileset writes: a YAML comment that points a YAML language server at TilesetSchemaURL. ParseTileset ignores it like any other comment.
const TilesetSchemaComment = "# yaml-language-server: $schema=" + TilesetSchemaURL

// MaxTilesetTiles is the most tiles one YAML tileset may define, its TileCount or the length of its Frames.
const MaxTilesetTiles = 1 << 24

// fixTilesetDoc is the fix for a tileset document error that has no more specific one.
const fixTilesetDoc = "correct the tileset to match its version 1 format in docs/en/tilemap.md"

// TilesetDoc is a versioned YAML tileset (*.tileset.yaml), the text counterpart of Godot's TileSet resource: what each tile of one image or sprite atlas is, its custom properties, collision flags and shapes, animation and terrain, defined once so that maps only place tiles. Tilemap.AddTilesetDoc builds it into a Tileset.
//
// A tileset draws its tiles either from a grid image (Image, Columns and TileCount, with optional Spacing and Margin) or from frames of a sprite atlas (Atlas and Frames); exactly one of Image and Atlas is set. A tile's local ID is its index in the grid, row by row from the top left, or in Frames, starting at 0.
type TilesetDoc struct {
	// Version is the schema version; it must be 1.
	Version int `yaml:"version" schema:"required"`
	// Name is the tileset's human-readable name.
	Name string `yaml:"name,omitempty"`
	// TileWidth and TileHeight are each tile's size in points; both must be positive.
	TileWidth  int `yaml:"tileWidth" schema:"required,exclusiveMinimum=0"`
	TileHeight int `yaml:"tileHeight" schema:"required,exclusiveMinimum=0"`
	// Image is the grid image the tiles are cut from, a slash-separated path from the project root such as "tiles/cave.png". It requires Columns and TileCount.
	Image string `yaml:"image,omitempty"`
	// Columns is how many tiles one row of Image holds.
	Columns int `yaml:"columns,omitempty" schema:"minimum=0"`
	// TileCount is how many tiles Image holds, at most MaxTilesetTiles.
	TileCount int `yaml:"tileCount,omitempty" schema:"minimum=0"`
	// Spacing is the gap in points between neighbouring tiles of Image, and Margin the gap around its edge.
	Spacing int `yaml:"spacing,omitempty" schema:"minimum=0"`
	Margin  int `yaml:"margin,omitempty" schema:"minimum=0"`
	// Atlas names the sprite atlas the tiles are frames of, as a scene's sprites name atlases. It requires Frames.
	Atlas string `yaml:"atlas,omitempty"`
	// Frames are the names of Atlas's frames that are the tiles, in local ID order, at most MaxTilesetTiles and each named once.
	Frames []string `yaml:"frames,omitempty"`
	// Properties are custom properties of the whole tileset, each a bool, an integer, a number or a string. EncodeTileset writes a whole-number float with a decimal point (2.0), so that it reads back as a float.
	Properties map[string]any `yaml:"properties,omitempty"`
	// Terrains are the terrain sets the tiles' Terrain fields name, each with a unique name.
	Terrains []TerrainDoc `yaml:"terrains,omitempty"`
	// Tiles describe the tiles that carry anything beyond their picture, in ascending ID order with each ID at most once. A tile left out has no properties, flags, shapes, animation or terrain.
	Tiles []TileDoc `yaml:"tiles,omitempty"`
}

// TerrainDoc is one terrain set of a TilesetDoc: a family of tiles, such as the edges and corners of grass, from which autotiling picks a tile by which neighbouring cells hold the same terrain (Tilemap.ApplyAutotile).
type TerrainDoc struct {
	// Name identifies the terrain within the tileset; tiles name it in TileDoc.Terrain.
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Mode is "cardinal", which matches the four edge neighbours (Autotile4, 16 tiles), or "blob", which also matches a corner neighbour when both edges beside it match (Autotile8, 47 tiles).
	Mode string `yaml:"mode" schema:"required,enum=cardinal|blob"`
	// Default is the optional local tile ID used when a cell's neighbour mask has no tile mapped to it. When omitted, the tile of the terrain that lists no neighbours (mask 0) is the default.
	Default *int `yaml:"default,omitempty" schema:"minimum=0"`
}

// TileDoc describes one tile of a TilesetDoc.
type TileDoc struct {
	// ID is the tile's local ID, from 0 to the tileset's tile count less 1.
	ID int `yaml:"id" schema:"required,minimum=0"`
	// Name is an optional human-readable name for the tile, such as "stone" or "moss".
	Name string `yaml:"name,omitempty"`
	// Solid sets TileSolid on the tile, so collision queries and tileshape treat its cells as blocked.
	Solid bool `yaml:"solid,omitempty"`
	// OneWay sets TileOneWay on the tile, a platform that blocks only from above.
	OneWay bool `yaml:"oneWay,omitempty"`
	// Flags are further TileFlags bits the game defines, such as a water or ladder bit, set on the tile beside Solid and OneWay.
	Flags TileFlags `yaml:"flags,omitempty"`
	// Weight is the tile's optional relative selection weight for random and variant brushes (>= 0; omitted defaults to 1).
	Weight *float64 `yaml:"weight,omitempty" schema:"minimum=0"`
	// Properties are the tile's custom properties, each a bool, an integer, a number or a string, as Tilemap.TileProperties returns them.
	Properties map[string]any `yaml:"properties,omitempty"`
	// Shapes are the tile's collision shapes in its own coordinates, (0, 0) at its top left, as Tileset.Shapes holds them.
	Shapes []TileShapeDoc `yaml:"shapes,omitempty"`
	// Animation, when set, makes the tile cycle through these frames in a loop as Tilemap.Update advances time.
	Animation []TileFrameDoc `yaml:"animation,omitempty"`
	// Terrain names the entry of TilesetDoc.Terrains the tile belongs to.
	Terrain string `yaml:"terrain,omitempty"`
	// Neighbors lists the directions in which a cell of the same terrain lies next to a cell this tile fills: north, east, south and west, and for a blob terrain also northeast, southeast, southwest and northwest, each only with both edges beside it (northeast with north and east), since a corner matches only when they do. No two tiles of one terrain list the same set, and the tile that lists none is the terrain's default. It requires Terrain.
	Neighbors []string `yaml:"neighbors,omitempty,flow"`
}

// TileShapeDoc is one collision shape of a tile, in the tile's coordinates.
type TileShapeDoc struct {
	// Kind is "rect" or "ellipse", given by X, Y, Width and Height, or "polygon" or "polyline", given by Points. An ellipse becomes a polygon of EllipseShapeSegments edges.
	Kind string `yaml:"kind" schema:"required,enum=rect|ellipse|polygon|polyline"`
	// Name and Type label the shape for game code, as a Tiled object's name and type do.
	Name string `yaml:"name,omitempty"`
	Type string `yaml:"type,omitempty"`
	// X and Y are the top left of a rect or ellipse's box, and Width and Height its positive size.
	X      float32 `yaml:"x,omitempty"`
	Y      float32 `yaml:"y,omitempty"`
	Width  float32 `yaml:"width,omitempty" schema:"minimum=0"`
	Height float32 `yaml:"height,omitempty" schema:"minimum=0"`
	// Points are the [x, y] vertices of a polygon (at least 3) or polyline (at least 2).
	Points [][]float32 `yaml:"points,omitempty,flow"`
	// Properties are the shape's custom properties, each a bool, an integer, a number or a string.
	Properties map[string]any `yaml:"properties,omitempty"`
}

// TileFrameDoc is one frame of a tile's animation.
type TileFrameDoc struct {
	// Tile is the local ID of the tile the frame shows. It is required, so a frame never shows tile 0 by omission.
	Tile int `yaml:"tile" schema:"required,minimum=0"`
	// Duration is how long the frame shows, positive, written as a Go duration such as 150ms.
	Duration time.Duration `yaml:"duration" schema:"required"`
}

// TilesetAssets resolves the image or atlas a TilesetDoc draws from, for Tilemap.AddTilesetDoc.
type TilesetAssets struct {
	// Image returns the image at a tileset's Image path. When nil the image is core.ImageSource{URI: path}.
	Image func(path string) (core.ImageSource, error)
	// Atlas returns the atlas a tileset's Atlas names. It is required for an atlas tileset.
	Atlas func(name string) (*sprite.Atlas, error)
}

// Terrain is one terrain set of a tileset, from a TilesetDoc's Terrains: the autotile set that maps the neighbour mask of a cell to the terrain's tile, in global tile IDs, ready for Tilemap.ApplyAutotile and Tilemap.SetAutotile with Contains as their terrainID.
type Terrain struct {
	// Name is the terrain's name in its tileset.
	Name string
	// Autotile maps neighbour masks to the terrain's GIDs; its Default is the tile that lists no neighbours, or 0 when none does.
	Autotile AutotileSet
}

// Contains reports whether gid, flip bits ignored, is one of the terrain's tiles.
func (t *Terrain) Contains(gid uint32) bool {
	id := gid & TileIDMask
	if t == nil || id == 0 {
		return false
	}
	if t.Autotile.Default&TileIDMask == id {
		return true
	}
	for _, g := range t.Autotile.Tiles {
		if g&TileIDMask == id {
			return true
		}
	}
	return false
}

// Terrain returns the terrain set named name of the first of m's tilesets that defines one, or nil.
func (m *Tilemap) Terrain(name string) *Terrain {
	for _, ts := range m.Tilesets {
		if ts == nil {
			continue
		}
		for _, t := range ts.Terrains {
			if t != nil && t.Name == name {
				return t
			}
		}
	}
	return nil
}

// ApplyTerrain updates every cell belonging to the named terrain in layerIndex using its autotile rules, returning false when the terrain is not defined on any tileset of m or layerIndex is out of range.
func (m *Tilemap) ApplyTerrain(layerIndex int, terrainName string) bool {
	if m == nil || layerIndex < 0 || layerIndex >= len(m.Layers) {
		return false
	}
	t := m.Terrain(terrainName)
	if t == nil {
		return false
	}
	m.ApplyAutotile(layerIndex, t.Autotile, t.Contains)
	return true
}

// SetTerrainTile places (placed=true) or removes (placed=false) a tile of the named terrain at (col, row) in layerIndex and updates that cell and its neighbors using SetAutotile, returning false when the terrain is not defined on any tileset of m or layerIndex is out of range.
func (m *Tilemap) SetTerrainTile(layerIndex, col, row int, terrainName string, placed bool) bool {
	if m == nil || layerIndex < 0 || layerIndex >= len(m.Layers) {
		return false
	}
	t := m.Terrain(terrainName)
	if t == nil {
		return false
	}
	m.SetAutotile(layerIndex, col, row, placed, t.Autotile, t.Contains)
	return true
}

// Clone returns a deep copy of doc, or nil when doc is nil.
func (doc *TilesetDoc) Clone() *TilesetDoc {
	if doc == nil {
		return nil
	}
	cp := *doc
	if doc.Frames != nil {
		cp.Frames = slices.Clone(doc.Frames)
	}
	if doc.Properties != nil {
		cp.Properties = maps.Clone(doc.Properties)
	}
	if doc.Terrains != nil {
		cp.Terrains = make([]TerrainDoc, len(doc.Terrains))
		for i, td := range doc.Terrains {
			if td.Default != nil {
				v := *td.Default
				td.Default = &v
			}
			cp.Terrains[i] = td
		}
	}
	if doc.Tiles != nil {
		cp.Tiles = make([]TileDoc, len(doc.Tiles))
		for i, tile := range doc.Tiles {
			if tile.Weight != nil {
				w := *tile.Weight
				tile.Weight = &w
			}
			if tile.Properties != nil {
				tile.Properties = maps.Clone(tile.Properties)
			}
			if tile.Shapes != nil {
				shapes := make([]TileShapeDoc, len(tile.Shapes))
				for j, s := range tile.Shapes {
					if s.Points != nil {
						pts := make([][]float32, len(s.Points))
						for k, pt := range s.Points {
							pts[k] = slices.Clone(pt)
						}
						s.Points = pts
					}
					if s.Properties != nil {
						s.Properties = maps.Clone(s.Properties)
					}
					shapes[j] = s
				}
				tile.Shapes = shapes
			}
			if tile.Animation != nil {
				tile.Animation = slices.Clone(tile.Animation)
			}
			if tile.Neighbors != nil {
				tile.Neighbors = slices.Clone(tile.Neighbors)
			}
			cp.Tiles[i] = tile
		}
	}
	return &cp
}

// buildDocTerrains constructs runtime Terrain values from doc using firstGID (defaulting to 1 when 0).
func buildDocTerrains(doc *TilesetDoc, firstGID uint32) []*Terrain {
	if doc == nil || len(doc.Terrains) == 0 {
		return nil
	}
	if firstGID == 0 {
		firstGID = 1
	}
	terrains := make(map[string]*Terrain, len(doc.Terrains))
	hasExplicitDefault := make(map[string]bool, len(doc.Terrains))
	out := make([]*Terrain, 0, len(doc.Terrains))
	for _, td := range doc.Terrains {
		t := &Terrain{Name: td.Name, Autotile: AutotileSet{Mode: Autotile4, Tiles: make(map[uint8]uint32)}}
		if td.Mode == "blob" {
			t.Autotile.Mode = Autotile8
		}
		if td.Default != nil && *td.Default >= 0 && uint64(*td.Default) <= uint64(TileIDMask) {
			t.Autotile.Default = firstGID + uint32(*td.Default)
			hasExplicitDefault[td.Name] = true
		}
		terrains[td.Name] = t
		out = append(out, t)
	}
	for _, tile := range doc.Tiles {
		t := terrains[tile.Terrain]
		if t == nil || tile.ID < 0 || uint64(tile.ID) > uint64(TileIDMask) {
			continue
		}
		gid := firstGID + uint32(tile.ID)
		var mask uint8
		for _, n := range tile.Neighbors {
			bit, ok := terrainNeighborBit(n)
			if ok {
				mask |= bit
			}
		}
		t.Autotile.Tiles[mask] = gid
		if mask == 0 && !hasExplicitDefault[tile.Terrain] {
			t.Autotile.Default = gid
		}
	}
	return out
}

// Terrain builds and returns the named terrain set from doc with global tile IDs starting at firstGID (1 when 0), or nil if doc defines no terrain with that name.
func (doc *TilesetDoc) Terrain(name string, firstGID uint32) *Terrain {
	for _, t := range buildDocTerrains(doc, firstGID) {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// terrainNeighborBit maps a TileDoc neighbour name to its autotile mask bit.
func terrainNeighborBit(name string) (uint8, bool) {
	switch name {
	case "north":
		return MaskNorth, true
	case "east":
		return MaskEast, true
	case "south":
		return MaskSouth, true
	case "west":
		return MaskWest, true
	case "northeast":
		return MaskNorthEast, true
	case "southeast":
		return MaskSouthEast, true
	case "southwest":
		return MaskSouthWest, true
	case "northwest":
		return MaskNorthWest, true
	}
	return 0, false
}

// Count returns how many tiles doc defines: its TileCount for a grid image, or the length of its Frames for an atlas.
func (doc *TilesetDoc) Count() int {
	if doc == nil {
		return 0
	}
	if doc.Atlas != "" {
		return len(doc.Frames)
	}
	return doc.TileCount
}

// ParseTileset decodes and validates a YAML tileset document (ValidateTileset), refusing unknown fields, a key given twice and a trailing document.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile.
func ParseTileset(data []byte) (*TilesetDoc, error) {
	doc, err := parseTileset(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixTilesetDoc)
	}
	return doc, nil
}

// LoadTilesetFS reads the tileset file at path from fsys and parses it with ParseTileset, naming the file in its errors.
func LoadTilesetFS(fsys fs.FS, path string) (*TilesetDoc, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, err
	}
	doc, err := ParseTileset(data)
	return doc, loaderr.WithFile(err, path)
}

func parseTileset(data []byte) (*TilesetDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{Problem: "tilemap: tileset: empty document", Fix: fixTilesetDoc}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc TilesetDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("tilemap: tileset: decode yaml: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{Line: trailingYAMLDocumentLine(data), Problem: "tilemap: tileset: unexpected trailing YAML document", Fix: "keep one tileset per file; remove the --- and anything after it"}
		}
		return nil, fmt.Errorf("tilemap: tileset: decode yaml: %w", err)
	}
	if err := requireFrameTiles(data); err != nil {
		return nil, err
	}
	if err := ValidateTileset(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// requireFrameTiles refuses an animation frame of the tileset in data, which has already decoded, that leaves out its tile: decoding would make it tile 0 unnoticed, and saving would then add the field.
func requireFrameTiles(data []byte) error {
	var frames struct {
		Tiles []struct {
			ID        int              `yaml:"id"`
			Animation []map[string]any `yaml:"animation"`
		} `yaml:"tiles"`
	}
	if err := yaml.Unmarshal(data, &frames); err != nil {
		return fmt.Errorf("tilemap: tileset: decode yaml: %w", err)
	}
	for i, tile := range frames.Tiles {
		for j, f := range tile.Animation {
			if v, ok := f["tile"]; !ok || v == nil {
				return loaderr.Errorf(loaderr.Join(loaderr.Path("tiles", i), "animation", j, "tile"), "name the tile the frame shows, such as tile: 0", "tilemap: tileset: tile %d animation frame %d has no tile", tile.ID, j)
			}
		}
	}
	return nil
}

// EncodeTileset validates doc and writes it as deterministic 2-space YAML, starting with TilesetSchemaComment. Keys come in a fixed order and maps in sorted key order, so the same tileset always gives the same bytes, and a file in that form parses and encodes back unchanged.
func EncodeTileset(doc *TilesetDoc) ([]byte, error) {
	if err := ValidateTileset(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(TilesetSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(tilesetFloatKinds(doc)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("tilemap: tileset: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("tilemap: tileset: encode yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// ValidateTileset checks doc against the version 1 tileset schema: a positive tile size, exactly one of a grid image (with its columns and tile count) or an atlas (with its frames), tiles in ascending ID order within the tile count, scalar properties, well-formed shapes, animations of positive durations over existing tiles, and terrains whose tiles list valid, distinct neighbour sets. Its errors are *loaderr.Error values naming the field at fault.
func ValidateTileset(doc *TilesetDoc) error {
	if doc == nil {
		return &loaderr.Error{Problem: "tilemap: tileset: nil document", Fix: fixTilesetDoc}
	}
	if doc.Version != TilesetFormatVersion {
		return &loaderr.Error{Field: "version", Problem: fmt.Sprintf("tilemap: tileset: unsupported version %d", doc.Version), Expected: strconv.Itoa(TilesetFormatVersion), Fix: "set version: 1"}
	}
	if doc.TileWidth <= 0 {
		return loaderr.Errorf("tileWidth", "give the tiles a width above 0", "tilemap: tileset: tileWidth %d must be positive", doc.TileWidth)
	}
	if doc.TileHeight <= 0 {
		return loaderr.Errorf("tileHeight", "give the tiles a height above 0", "tilemap: tileset: tileHeight %d must be positive", doc.TileHeight)
	}
	switch {
	case doc.Image != "" && doc.Atlas != "":
		return loaderr.Errorf("atlas", "keep either image or atlas", "tilemap: tileset: image and atlas cannot both be set")
	case doc.Image == "" && doc.Atlas == "":
		return loaderr.Errorf("image", "set image (a grid image with columns and tileCount) or atlas (with frames)", "tilemap: tileset: needs an image or an atlas")
	case doc.Image != "":
		if !fs.ValidPath(doc.Image) || doc.Image == "." {
			return loaderr.Errorf("image", "write the image's path from the project root with slashes, such as tiles/cave.png", "tilemap: tileset: image %q must be a slash-separated path relative to the project root", doc.Image)
		}
		if len(doc.Frames) > 0 {
			return loaderr.Errorf("frames", "remove frames, which only an atlas tileset has", "tilemap: tileset: frames need atlas, not image")
		}
		if doc.Columns <= 0 {
			return loaderr.Errorf("columns", "set columns to how many tiles one row of the image holds", "tilemap: tileset: columns %d must be positive for an image tileset", doc.Columns)
		}
		if doc.TileCount <= 0 || doc.TileCount > MaxTilesetTiles {
			return loaderr.Errorf("tileCount", "set tileCount to how many tiles the image holds", "tilemap: tileset: tileCount %d must be from 1 to %d", doc.TileCount, MaxTilesetTiles)
		}
		if doc.Spacing < 0 {
			return loaderr.Errorf("spacing", "set spacing to 0 or more", "tilemap: tileset: spacing %d must not be negative", doc.Spacing)
		}
		if doc.Margin < 0 {
			return loaderr.Errorf("margin", "set margin to 0 or more", "tilemap: tileset: margin %d must not be negative", doc.Margin)
		}
	default:
		for _, f := range []struct {
			name string
			v    int
		}{{"columns", doc.Columns}, {"tileCount", doc.TileCount}, {"spacing", doc.Spacing}, {"margin", doc.Margin}} {
			if f.v != 0 {
				return loaderr.Errorf(f.name, "remove it: an atlas tileset's tiles are its frames", "tilemap: tileset: %s needs image, not atlas", f.name)
			}
		}
		if len(doc.Frames) == 0 || len(doc.Frames) > MaxTilesetTiles {
			return loaderr.Errorf("frames", "list the atlas frames that are the tiles, in tile ID order", "tilemap: tileset: an atlas tileset needs from 1 to %d frames, not %d", MaxTilesetTiles, len(doc.Frames))
		}
		seen := make(map[string]int, len(doc.Frames))
		for i, name := range doc.Frames {
			if name == "" {
				return loaderr.Errorf(loaderr.Path("frames", i), "name the atlas frame", "tilemap: tileset: frame %d has an empty name", i)
			}
			if j, dup := seen[name]; dup {
				return loaderr.Errorf(loaderr.Path("frames", i), "list each frame once", "tilemap: tileset: frame %q is tile %d and tile %d", name, j, i)
			}
			seen[name] = i
		}
	}
	if err := validateProps("properties", doc.Properties); err != nil {
		return err
	}
	count := doc.Count()
	terrains := make(map[string]string, len(doc.Terrains))
	for i, t := range doc.Terrains {
		if t.Name == "" {
			return loaderr.Errorf(loaderr.Path("terrains", i, "name"), "name the terrain", "tilemap: tileset: terrain %d has an empty name", i)
		}
		if _, dup := terrains[t.Name]; dup {
			return loaderr.Errorf(loaderr.Path("terrains", i, "name"), "give each terrain its own name", "tilemap: tileset: duplicate terrain %q", t.Name)
		}
		if t.Mode != "cardinal" && t.Mode != "blob" {
			return &loaderr.Error{Field: loaderr.Path("terrains", i, "mode"), Problem: fmt.Sprintf("tilemap: tileset: terrain %q has mode %q", t.Name, t.Mode), Expected: `"cardinal" or "blob"`, Fix: "set mode to cardinal (16 tiles) or blob (47 tiles)"}
		}
		if t.Default != nil && (*t.Default < 0 || *t.Default >= count) {
			return loaderr.Errorf(loaderr.Path("terrains", i, "default"), "use a tile ID from 0 to the tile count less 1", "tilemap: tileset: terrain %q default tile %d is outside the tileset's %d tiles", t.Name, *t.Default, count)
		}
		terrains[t.Name] = t.Mode
	}
	masks := make(map[string]map[uint8]int)
	prev := -1
	for i := range doc.Tiles {
		tile := &doc.Tiles[i]
		tp := loaderr.Path("tiles", i)
		if tile.ID < 0 || tile.ID >= count {
			return loaderr.Errorf(loaderr.Join(tp, "id"), "use a tile ID from 0 to the tile count less 1", "tilemap: tileset: tile id %d is outside the tileset's %d tiles", tile.ID, count)
		}
		if tile.ID <= prev {
			return loaderr.Errorf(loaderr.Join(tp, "id"), "list tiles in ascending ID order, each once", "tilemap: tileset: tile %d comes after tile %d", tile.ID, prev)
		}
		prev = tile.ID
		if tile.Weight != nil {
			if math.IsNaN(*tile.Weight) || math.IsInf(*tile.Weight, 0) || *tile.Weight < 0 {
				return loaderr.Errorf(loaderr.Join(tp, "weight"), "set weight to a finite number 0 or greater", "tilemap: tileset: tile %d weight %v must be finite and non-negative", tile.ID, *tile.Weight)
			}
		}
		if err := validateProps(loaderr.Join(tp, "properties"), tile.Properties); err != nil {
			return err
		}
		for j := range tile.Shapes {
			if err := validateShapeDoc(loaderr.Join(tp, "shapes", j), &tile.Shapes[j]); err != nil {
				return err
			}
		}
		for j, f := range tile.Animation {
			fp := loaderr.Join(tp, "animation", j)
			if f.Tile < 0 || f.Tile >= count {
				return loaderr.Errorf(loaderr.Join(fp, "tile"), "show a tile of this tileset", "tilemap: tileset: tile %d animation frame %d shows tile %d, outside the tileset's %d tiles", tile.ID, j, f.Tile, count)
			}
			if f.Duration <= 0 {
				return loaderr.Errorf(loaderr.Join(fp, "duration"), "give the frame a positive duration such as 150ms", "tilemap: tileset: tile %d animation frame %d has duration %v", tile.ID, j, f.Duration)
			}
		}
		if tile.Terrain == "" {
			if len(tile.Neighbors) > 0 {
				return loaderr.Errorf(loaderr.Join(tp, "neighbors"), "set terrain, or remove neighbors", "tilemap: tileset: tile %d lists neighbors without a terrain", tile.ID)
			}
			continue
		}
		mode, ok := terrains[tile.Terrain]
		if !ok {
			return loaderr.Errorf(loaderr.Join(tp, "terrain"), "name an entry of terrains", "tilemap: tileset: tile %d names unknown terrain %q", tile.ID, tile.Terrain)
		}
		var mask uint8
		for j, n := range tile.Neighbors {
			bit, ok := terrainNeighborBit(n)
			np := loaderr.Join(tp, "neighbors", j)
			if !ok {
				return &loaderr.Error{Field: np, Problem: fmt.Sprintf("tilemap: tileset: tile %d has unknown neighbor %q", tile.ID, n), Expected: "north, east, south, west, northeast, southeast, southwest or northwest", Fix: "name a direction"}
			}
			if mode == "cardinal" && bit > MaskWest {
				return loaderr.Errorf(np, "use a blob terrain to match corners, or remove the corner", "tilemap: tileset: tile %d lists corner %q in cardinal terrain %q", tile.ID, n, tile.Terrain)
			}
			if mask&bit != 0 {
				return loaderr.Errorf(np, "list each direction once", "tilemap: tileset: tile %d lists neighbor %q twice", tile.ID, n)
			}
			mask |= bit
		}
		// ComputeMask8 sets a corner bit only when both edges beside it match, so a blob tile listing a corner without them could never be chosen.
		for j, n := range tile.Neighbors {
			bit, _ := terrainNeighborBit(n)
			if edges := cornerEdgeMask(bit); edges != 0 && mask&edges != edges {
				return loaderr.Errorf(loaderr.Join(tp, "neighbors", j), "list both edges beside the corner as well, or remove the corner", "tilemap: tileset: tile %d lists corner %q without both edges beside it, so autotiling never chooses it", tile.ID, n)
			}
		}
		byMask := masks[tile.Terrain]
		if byMask == nil {
			byMask = make(map[uint8]int)
			masks[tile.Terrain] = byMask
		}
		if other, dup := byMask[mask]; dup {
			return loaderr.Errorf(loaderr.Join(tp, "neighbors"), "give each tile of a terrain its own set of neighbors", "tilemap: tileset: tiles %d and %d of terrain %q list the same neighbors", other, tile.ID, tile.Terrain)
		}
		byMask[mask] = tile.ID
	}
	return nil
}

// cornerEdgeMask maps each corner neighbour bit to the two edge bits that must match for ComputeMask8 to set it.
func cornerEdgeMask(bit uint8) uint8 {
	switch bit {
	case MaskNorthEast:
		return MaskNorth | MaskEast
	case MaskSouthEast:
		return MaskSouth | MaskEast
	case MaskSouthWest:
		return MaskSouth | MaskWest
	case MaskNorthWest:
		return MaskNorth | MaskWest
	}
	return 0
}

func validateShapeDoc(path string, s *TileShapeDoc) error {
	for _, f := range []struct {
		name string
		v    float32
	}{{"x", s.X}, {"y", s.Y}, {"width", s.Width}, {"height", s.Height}} {
		if !finite32(f.v) {
			return loaderr.Errorf(loaderr.Join(path, f.name), "use a finite number", "tilemap: tileset: shape %s is not finite", f.name)
		}
	}
	if err := validateProps(loaderr.Join(path, "properties"), s.Properties); err != nil {
		return err
	}
	switch s.Kind {
	case "rect", "ellipse":
		if len(s.Points) > 0 {
			return loaderr.Errorf(loaderr.Join(path, "points"), "give a rect or ellipse x, y, width and height, or make it a polygon", "tilemap: tileset: a %s shape has no points", s.Kind)
		}
		if s.Width <= 0 {
			return loaderr.Errorf(loaderr.Join(path, "width"), "give the shape a positive width", "tilemap: tileset: %s width %v must be positive", s.Kind, s.Width)
		}
		if s.Height <= 0 {
			return loaderr.Errorf(loaderr.Join(path, "height"), "give the shape a positive height", "tilemap: tileset: %s height %v must be positive", s.Kind, s.Height)
		}
	case "polygon", "polyline":
		for _, f := range []struct {
			name string
			v    float32
		}{{"x", s.X}, {"y", s.Y}, {"width", s.Width}, {"height", s.Height}} {
			if f.v != 0 {
				return loaderr.Errorf(loaderr.Join(path, f.name), "give a polygon or polyline its points only, in the tile's coordinates", "tilemap: tileset: a %s shape has no %s", s.Kind, f.name)
			}
		}
		least := 3
		if s.Kind == "polyline" {
			least = 2
		}
		if len(s.Points) < least {
			return loaderr.Errorf(loaderr.Join(path, "points"), fmt.Sprintf("give the %s at least %d points", s.Kind, least), "tilemap: tileset: %s has %d points", s.Kind, len(s.Points))
		}
		for i, p := range s.Points {
			if len(p) != 2 || !finite32(p[0]) || !finite32(p[1]) {
				return loaderr.Errorf(loaderr.Join(path, "points", i), "write each point as [x, y] with finite numbers", "tilemap: tileset: point %d is not a finite [x, y] pair", i)
			}
		}
	default:
		return &loaderr.Error{Field: loaderr.Join(path, "kind"), Problem: fmt.Sprintf("tilemap: tileset: unknown shape kind %q", s.Kind), Expected: "rect, ellipse, polygon or polyline", Fix: "set kind to one of those"}
	}
	return nil
}

// validateProps checks that every property in props, found at path, has a name and a bool, integer, finite number or string value.
func validateProps(path string, props map[string]any) error {
	for _, name := range slices.Sorted(maps.Keys(props)) {
		if name == "" {
			return loaderr.Errorf(path, "name every property", "tilemap: tileset: a property has an empty name")
		}
		switch v := props[name].(type) {
		case bool, int, string:
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return loaderr.Errorf(loaderr.Join(path, name), "use a finite number", "tilemap: tileset: property %q is not finite", name)
			}
		default:
			return &loaderr.Error{Field: loaderr.Join(path, name), Problem: fmt.Sprintf("tilemap: tileset: property %q has a %T value", name, v), Expected: "a bool, an integer, a number or a string", Fix: "give the property a single scalar value"}
		}
	}
	return nil
}

func finite32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// AddTilesetDoc builds doc, which must validate, into a Tileset whose global tile IDs start at firstGID (1 when 0), resolving its image or atlas through assets, appends it to m.Tilesets in FirstGID order and sets the flags of its tiles on m (SetFlags). The new tileset carries doc's properties, per-tile properties, collision shapes, animations and terrain sets.
//
// It refuses a tileset whose IDs would run past TileIDMask or overlap a tileset m already has, an atlas tileset without assets.Atlas, and atlas frames the atlas lacks.
func (m *Tilemap) AddTilesetDoc(doc *TilesetDoc, firstGID uint32, assets TilesetAssets) (*Tileset, error) {
	if err := ValidateTileset(doc); err != nil {
		return nil, err
	}
	if firstGID == 0 {
		firstGID = 1
	}
	count := uint32(doc.Count())
	if firstGID > TileIDMask || TileIDMask-firstGID < count-1 {
		return nil, fmt.Errorf("tilemap: tileset %q: %d tiles from first GID %d run past %d", doc.Name, count, firstGID, TileIDMask)
	}
	last := firstGID + count - 1
	for _, other := range m.Tilesets {
		if other == nil {
			continue
		}
		oc := uint32(max(other.TileCount, len(other.Frames), 1))
		if firstGID <= other.FirstGID+oc-1 && other.FirstGID <= last {
			return nil, fmt.Errorf("tilemap: tileset %q: GIDs %d to %d overlap tileset %q from GID %d", doc.Name, firstGID, last, other.Name, other.FirstGID)
		}
	}

	var ts *Tileset
	if doc.Atlas != "" {
		if assets.Atlas == nil {
			return nil, fmt.Errorf("tilemap: tileset %q: atlas %q needs TilesetAssets.Atlas", doc.Name, doc.Atlas)
		}
		atlas, err := assets.Atlas(doc.Atlas)
		if err != nil {
			return nil, fmt.Errorf("tilemap: tileset %q: atlas %q: %w", doc.Name, doc.Atlas, err)
		}
		if atlas == nil {
			return nil, fmt.Errorf("tilemap: tileset %q: atlas %q resolved to nil", doc.Name, doc.Atlas)
		}
		for _, name := range doc.Frames {
			if _, ok := atlas.Frames[name]; !ok {
				return nil, fmt.Errorf("tilemap: tileset %q: atlas %q has no frame %q", doc.Name, doc.Atlas, name)
			}
		}
		ts = NewAtlasTileset(firstGID, atlas, doc.Frames...)
	} else {
		img := core.ImageSource{URI: doc.Image}
		if assets.Image != nil {
			var err error
			if img, err = assets.Image(doc.Image); err != nil {
				return nil, fmt.Errorf("tilemap: tileset %q: image %q: %w", doc.Name, doc.Image, err)
			}
		}
		ts = NewGridTileset(firstGID, img, float32(doc.TileWidth), float32(doc.TileHeight), doc.Columns, doc.TileCount)
		ts.Spacing, ts.Margin = float32(doc.Spacing), float32(doc.Margin)
	}
	ts.Name = doc.Name
	ts.TileWidth, ts.TileHeight = float32(doc.TileWidth), float32(doc.TileHeight)
	ts.Properties = docProperties(doc.Properties)

	ts.Terrains = buildDocTerrains(doc, firstGID)
	for _, tile := range doc.Tiles {
		local := uint32(tile.ID)
		if tile.Weight != nil {
			if ts.TileProbabilities == nil {
				ts.TileProbabilities = make(map[uint32]float64)
			}
			ts.TileProbabilities[local] = *tile.Weight
		}
		if len(tile.Properties) > 0 {
			if ts.TileProperties == nil {
				ts.TileProperties = make(map[uint32]Properties)
			}
			ts.TileProperties[local] = docProperties(tile.Properties)
		}
		if len(tile.Shapes) > 0 {
			if ts.Shapes == nil {
				ts.Shapes = make(map[uint32][]TileShape)
			}
			shapes := make([]TileShape, len(tile.Shapes))
			for i := range tile.Shapes {
				shapes[i] = docShape(&tile.Shapes[i])
			}
			ts.Shapes[local] = shapes
		}
		if len(tile.Animation) > 0 {
			if ts.Animations == nil {
				ts.Animations = make(map[uint32]*TileAnimation)
			}
			frames := make([]TileAnimFrame, len(tile.Animation))
			for i, f := range tile.Animation {
				frames[i] = TileAnimFrame{TileID: uint32(f.Tile), Duration: f.Duration}
			}
			ts.Animations[local] = &TileAnimation{Frames: frames}
		}
	}

	m.Tilesets = append(m.Tilesets, ts)
	slices.SortStableFunc(m.Tilesets, func(a, b *Tileset) int {
		if a == nil || b == nil {
			return 0
		}
		return cmp.Compare(a.FirstGID, b.FirstGID)
	})
	for _, tile := range doc.Tiles {
		flags := tile.Flags
		if tile.Solid {
			flags |= TileSolid
		}
		if tile.OneWay {
			flags |= TileOneWay
		}
		if flags != 0 {
			m.SetFlags(firstGID+uint32(tile.ID), flags)
		}
	}
	return ts, nil
}

// docProperties converts validated YAML properties into Properties, typed as Tiled types them: "bool", "int", "float" or "string".
func docProperties(props map[string]any) Properties {
	if len(props) == 0 {
		return nil
	}
	out := make(Properties, len(props))
	for name, v := range props {
		typ := "string"
		switch v.(type) {
		case bool:
			typ = "bool"
		case int:
			typ = "int"
		case float64:
			typ = "float"
		}
		out[name] = Property{Name: name, Type: typ, Value: v}
	}
	return out
}

// docShape converts a validated shape into a TileShape in the tile's coordinates.
func docShape(s *TileShapeDoc) TileShape {
	out := TileShape{Name: s.Name, Type: s.Type, Properties: docProperties(s.Properties)}
	x, y, w, h := float64(s.X), float64(s.Y), float64(s.Width), float64(s.Height)
	switch s.Kind {
	case "rect":
		out.Shape = ObjectRect
		out.Points = []core.Point{
			{X: core.Points(x), Y: core.Points(y)},
			{X: core.Points(x + w), Y: core.Points(y)},
			{X: core.Points(x + w), Y: core.Points(y + h)},
			{X: core.Points(x), Y: core.Points(y + h)},
		}
	case "ellipse":
		out.Shape = ObjectEllipse
		rx, ry := w/2, h/2
		out.Points = make([]core.Point, EllipseShapeSegments)
		for k := range out.Points {
			a := 2 * math.Pi * float64(k) / EllipseShapeSegments
			out.Points[k] = core.Point{X: core.Points(x + rx + rx*math.Cos(a)), Y: core.Points(y + ry + ry*math.Sin(a))}
		}
	default:
		out.Shape = ObjectPolygon
		if s.Kind == "polyline" {
			out.Shape = ObjectPolyline
		}
		out.Points = make([]core.Point, len(s.Points))
		for i, p := range s.Points {
			out.Points[i] = core.Point{X: core.Points(p[0]), Y: core.Points(p[1])}
		}
	}
	return out
}

// tilesetFloatKinds returns doc for encoding with every whole-number float property marked to be written with a decimal point, so that it reads back as a float and not an int. doc is not changed.
func tilesetFloatKinds(doc *TilesetDoc) *TilesetDoc {
	out := *doc
	out.Properties = floatProps(doc.Properties)
	if doc.Tiles != nil {
		out.Tiles = make([]TileDoc, len(doc.Tiles))
		for i, t := range doc.Tiles {
			t.Properties = floatProps(t.Properties)
			if t.Shapes != nil {
				shapes := make([]TileShapeDoc, len(t.Shapes))
				for j, s := range t.Shapes {
					s.Properties = floatProps(s.Properties)
					shapes[j] = s
				}
				t.Shapes = shapes
			}
			out.Tiles[i] = t
		}
	}
	return &out
}

func floatProps(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if f, ok := v.(float64); ok {
			if s := strconv.FormatFloat(f, 'g', -1, 64); strings.TrimLeft(strings.TrimPrefix(s, "-"), "0123456789") == "" {
				v = wholeFloat(s)
			}
		}
		out[k] = v
	}
	return out
}

// wholeFloat is a whole-number float, as strconv formats it, that encodes with ".0" appended so that it reads back as a float.
type wholeFloat string

// MarshalYAML writes the number as a plain !!float scalar, such as 2.0.
func (f wholeFloat) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: string(f) + ".0"}, nil
}

// TilesetFrame returns where local tile id of doc lies in the image it is drawn from: a rectangle of the grid image for an image tileset, or the frame of atlas, the atlas doc names, for an atlas tileset. It reports false for an ID outside the tileset, an atlas tileset with a nil atlas and a frame the atlas lacks. An editor's tile palette uses it to show a tileset's tiles.
func TilesetFrame(doc *TilesetDoc, atlas *sprite.Atlas, id int) (sprite.Frame, bool) {
	if doc == nil || id < 0 || id >= doc.Count() {
		return sprite.Frame{}, false
	}
	if doc.Atlas != "" {
		if atlas == nil {
			return sprite.Frame{}, false
		}
		f, ok := atlas.Frames[doc.Frames[id]]
		return f, ok
	}
	if doc.Columns <= 0 {
		return sprite.Frame{}, false
	}
	col, row := id%doc.Columns, id/doc.Columns
	tw, th := float32(doc.TileWidth), float32(doc.TileHeight)
	return sprite.Frame{
		Name:   strconv.Itoa(id),
		X:      float32(doc.Margin) + float32(col)*(tw+float32(doc.Spacing)),
		Y:      float32(doc.Margin) + float32(row)*(th+float32(doc.Spacing)),
		W:      tw,
		H:      th,
		Width:  tw,
		Height: th,
	}, true
}
