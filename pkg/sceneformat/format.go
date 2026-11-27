package sceneformat

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the newest scene format schema version, the one new documents are written in. Version 2 adds scene instancing (ObjectDoc.Scene and ObjectDoc.Overrides), inheritance (SceneDoc.Inherits, SceneDoc.Overrides and SceneDoc.LayerOverrides) and glTF models (ObjectDoc.Model, ObjectDoc.ModelNode and ObjectDoc.ModelMesh). Version 3 adds parallax and repeating layers (LayerDoc.ParallaxX, LayerDoc.ParallaxY, LayerDoc.RepeatX, LayerDoc.RepeatY, LayerDoc.RepeatWidth and LayerDoc.RepeatHeight, and the matching fields on LayerOverride) and tilemap offsets (TilemapDoc.OffsetX and TilemapDoc.OffsetY). Version 4 adds 3D lights (ObjectDoc.Light), 3D cameras (ObjectDoc.Camera), 3D reflection probes (ObjectDoc.ReflectionProbe), 3D light probe grids (ObjectDoc.LightProbeGrid) and scene environment settings (SceneDoc.Environment).
const CurrentVersion = 4

// MinVersion is the oldest scene format schema version this package still reads. A version 1, 2 or 3 document is read and written unchanged, and may not use any field added by a newer version.
const MinVersion = 1

// SchemaURL is where the JSON Schema of version 1 scene documents is published. Encode names it in a yaml-language-server comment on the first line of a version 1 document, so editors that run the YAML language server (such as VS Code's YAML extension) and AI tools validate and complete a scene file as it is written.
const SchemaURL = "https://game.nsgo.dev/schemas/scene.v1.json"

// SchemaComment is the first line Encode writes for a version 1 document: a YAML comment that points a YAML language server at SchemaURL. Parse ignores it like any other comment.
const SchemaComment = "# yaml-language-server: $schema=" + SchemaURL

// SchemaURLV2 is where the JSON Schema of version 2 scene documents is published, named by SchemaCommentV2 as SchemaURL is by SchemaComment.
const SchemaURLV2 = "https://game.nsgo.dev/schemas/scene.v2.json"

// SchemaCommentV2 is the first line Encode writes for a version 2 document, pointing a YAML language server at SchemaURLV2.
const SchemaCommentV2 = "# yaml-language-server: $schema=" + SchemaURLV2

// SchemaURLV3 is where the JSON Schema of version 3 scene documents is published, named by SchemaCommentV3 as SchemaURL is by SchemaComment.
const SchemaURLV3 = "https://game.nsgo.dev/schemas/scene.v3.json"

// SchemaCommentV3 is the first line Encode writes for a version 3 document, pointing a YAML language server at SchemaURLV3.
const SchemaCommentV3 = "# yaml-language-server: $schema=" + SchemaURLV3

// SchemaURLV4 is where the JSON Schema of version 4 scene documents is published, named by SchemaCommentV4 as SchemaURL is by SchemaComment.
const SchemaURLV4 = "https://game.nsgo.dev/schemas/scene.v4.json"

// SchemaCommentV4 is the first line Encode writes for a version 4 document, pointing a YAML language server at SchemaURLV4.
const SchemaCommentV4 = "# yaml-language-server: $schema=" + SchemaURLV4

// SchemaCommentFor returns the first line Encode writes for a document of the given version: SchemaComment for version 1, SchemaCommentV2 for version 2, SchemaCommentV3 for version 3, and SchemaCommentV4 for version 4.
func SchemaCommentFor(version int) string {
	switch {
	case version >= 4:
		return SchemaCommentV4
	case version == 3:
		return SchemaCommentV3
	case version == 2:
		return SchemaCommentV2
	default:
		return SchemaComment
	}
}

// Space is the coordinate space a layer draws in.
type Space string

const (
	// SpaceWorld draws the layer under the scene's camera transform.
	SpaceWorld Space = "world"
	// SpaceScreen draws the layer directly in canvas coordinates, ignoring the camera.
	SpaceScreen Space = "screen"
)

// SceneDoc is the root of a versioned YAML scene document.
type SceneDoc struct {
	// Version is the schema version, from MinVersion to CurrentVersion. Encode writes it back as it is.
	Version int `yaml:"version" schema:"required"`
	// UID is the scene file's stable ID, which stays the same when the file is renamed or moved so the tools can find it again (see package refs); empty until a tool such as game index gives it one. When set it must be a UID as resource.NewUID returns it. Resolve keeps the UID of the scene it resolves, not its base's. Both versions accept it.
	UID string `yaml:"uid,omitempty"`
	// Inherits optionally names a base scene file by its slash-separated path from the project root, such as "scenes/enemy.scene.yaml". The scene then starts as a copy of its base and changes only what it declares: its own scalar fields, camera and tilemaps replace the base's when set, its Overrides and LayerOverrides change inherited objects and layers, and its Layers add objects to the base's layers of the same name or add new layers after them. Resolve applies it. Version 2.
	Inherits string `yaml:"inherits,omitempty"`
	// Name is the scene's human-readable name.
	Name string `yaml:"name,omitempty"`
	// NavMesh optionally names the scene's baked navigation mesh file by its slash-separated path from the project root, such as "levels/level1.navmesh.yaml": a navbake3d file with one mesh per agent type, which the game navmesh command writes from the scene's geometry. In a scene that inherits another, a NavMesh set replaces the base's. Both versions accept it.
	NavMesh string `yaml:"navMesh,omitempty"`
	// VirtualWidth and VirtualHeight are the scene's virtual viewport size in points. Both must be 0 or both positive.
	VirtualWidth  float32 `yaml:"virtualWidth,omitempty" schema:"minimum=0"`
	VirtualHeight float32 `yaml:"virtualHeight,omitempty" schema:"minimum=0"`
	// BackgroundColor is an optional CSS color string parsed with core.ParseColor.
	BackgroundColor string `yaml:"backgroundColor,omitempty"`
	// AmbientColor is an optional CSS color string parsed with core.ParseColor (for example "rgba(12, 16, 30, 0.85)" or "#0c101ed8") that sets the 2D lighting ambient darkness tint (lights2d.Scene.Ambient). Both versions accept it.
	AmbientColor string `yaml:"ambientColor,omitempty"`
	// Camera holds the default camera transform for world-space layers. Nil leaves world space untransformed.
	Camera *CameraDoc `yaml:"camera,omitempty"`
	// Environment holds 3D sky, ambient/IBL lighting, fog, tone mapping and exposure settings. Version 4.
	Environment *EnvironmentDoc `yaml:"environment,omitempty"`
	// Tilemaps defines reusable named tilemaps referenced by layers via TilemapDoc.Ref.
	Tilemaps map[string]*TilemapDoc `yaml:"tilemaps,omitempty"`
	// LayerOverrides changes the inherited layers named by its keys; it requires Inherits. Version 2.
	LayerOverrides map[string]LayerOverride `yaml:"layerOverrides,omitempty"`
	// Overrides changes the inherited objects whose IDs are its keys, nested children and the objects of instanced scenes (such as "enemy_1/body") included; it requires Inherits. Version 2.
	Overrides map[string]ObjectOverride `yaml:"overrides,omitempty"`
	// Layers are the scene's layers, drawn lowest Order first and in slice order for equal Order. In a scene that inherits another, a layer named like one of the base's adds its objects to that layer and may set nothing else (LayerOverrides changes the layer itself), and any other layer is added after the base's layers.
	Layers []LayerDoc `yaml:"layers,omitempty"`
	// Connections connect events of the scene's objects to handlers of its objects, as Godot's connections in a .tscn file do, so a button can open a door with no Go code that wires the two. Connect binds them to the handlers a ComponentRegistry declares, and Resolve carries the connections of instanced and inherited scenes into the scene that uses them.
	Connections []ConnectionDoc `yaml:"connections,omitempty"`
}

// ConnectionDoc connects an event that one object emits to a handler of another object, or of the same one (SceneDoc.Connections). Signals.Emit calls the handler each time the object emits the event.
type ConnectionDoc struct {
	// From is the ID of the object that emits the event. In a scene that instances or inherits other scene files it may name their objects, such as "door_1/button".
	From string `yaml:"from" schema:"required,minLength=1"`
	// Event names an event a registered component type of From's object declares (ComponentType.Events), such as "pressed", or "button.pressed" to say which component when several of the object's components declare it.
	Event string `yaml:"event" schema:"required,minLength=1"`
	// To is the ID of the object whose handler runs.
	To string `yaml:"to" schema:"required,minLength=1"`
	// Handler names a handler a registered component type of To's object declares (ComponentType.Handlers), such as "open", or "door.open" to say which component.
	Handler string `yaml:"handler" schema:"required,minLength=1"`
	// Args are values for the handler's params by name; a param left out takes its default. Each is a bool, a number or a string.
	Args map[string]any `yaml:"args,omitempty"`
	// Once disconnects the connection after the first time the event fires, as Godot's one-shot connections do.
	Once bool `yaml:"once,omitempty"`
}

// LayerOverride changes an inherited layer in a scene that inherits another (SceneDoc.LayerOverrides). A field left unset keeps the base layer's value.
type LayerOverride struct {
	// Space replaces the layer's space when set.
	Space Space `yaml:"space,omitempty" schema:"enum=world|screen"`
	// Order replaces the layer's draw order when set.
	Order *int `yaml:"order,omitempty"`
	// Visible replaces the layer's visibility when set.
	Visible *bool `yaml:"visible,omitempty"`
	// ParallaxX replaces the layer's horizontal parallax scroll factor when set. Version 3.
	ParallaxX *float32 `yaml:"parallaxX,omitempty"`
	// ParallaxY replaces the layer's vertical parallax scroll factor when set. Version 3.
	ParallaxY *float32 `yaml:"parallaxY,omitempty"`
	// RepeatX replaces whether a world-space layer tiles horizontally across the camera's visible region when set. Version 3.
	RepeatX *bool `yaml:"repeatX,omitempty"`
	// RepeatY replaces whether a world-space layer tiles vertically across the camera's visible region when set. Version 3.
	RepeatY *bool `yaml:"repeatY,omitempty"`
	// RepeatWidth replaces the horizontal repeat period in layer points when set. Version 3.
	RepeatWidth *float32 `yaml:"repeatWidth,omitempty" schema:"minimum=0"`
	// RepeatHeight replaces the vertical repeat period in layer points when set. Version 3.
	RepeatHeight *float32 `yaml:"repeatHeight,omitempty" schema:"minimum=0"`
	// Tilemap replaces the layer's tilemap when set.
	Tilemap *TilemapDoc `yaml:"tilemap,omitempty"`
}

// CameraDoc describes a 2D camera transform for world-space layers.
type CameraDoc struct {
	// X and Y are the world coordinates centered in the virtual viewport (or mapped to the origin when virtual dimensions are 0).
	X float32 `yaml:"x,omitempty"`
	Y float32 `yaml:"y,omitempty"`
	// Zoom is the camera zoom factor; nil defaults to 1. Must be positive when set.
	Zoom *float32 `yaml:"zoom,omitempty" schema:"exclusiveMinimum=0"`
	// Rotation turns the camera clockwise in radians; world geometry rotates by -Rotation around (X, Y).
	Rotation float32 `yaml:"rotation,omitempty"`
	// Bounds optionally clamps the camera view inside a world rectangle.
	Bounds *BoundsDoc `yaml:"bounds,omitempty"`
}

// EffectiveZoom returns the camera's zoom factor, defaulting to 1 when Zoom is nil.
func (c *CameraDoc) EffectiveZoom() float32 {
	if c == nil || c.Zoom == nil {
		return 1
	}
	return *c.Zoom
}

// BoundsDoc is an axis-aligned rectangle in world coordinates.
type BoundsDoc struct {
	// X and Y are the rectangle's top-left corner.
	X float32 `yaml:"x,omitempty"`
	Y float32 `yaml:"y,omitempty"`
	// Width and Height are the rectangle's size; both must be positive.
	Width  float32 `yaml:"width" schema:"required,exclusiveMinimum=0"`
	Height float32 `yaml:"height" schema:"required,exclusiveMinimum=0"`
}

// LayerDoc is one ordered layer in a scene.
type LayerDoc struct {
	// Name uniquely identifies the layer within the scene.
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Space is SpaceWorld or SpaceScreen; empty defaults to SpaceWorld.
	Space Space `yaml:"space,omitempty" schema:"enum=world|screen"`
	// Order sets draw order across layers: lower values draw first, ties keep document order.
	Order int `yaml:"order,omitempty"`
	// Visible controls whether the layer draws; nil defaults to true.
	Visible *bool `yaml:"visible,omitempty"`
	// ParallaxX scales how fast a world-space layer scrolls horizontally relative to the camera; nil defaults to 1. Zero pins the layer horizontally against camera translation, 0.5 scrolls at half speed, and values above 1 scroll faster for foreground layers. Version 3.
	ParallaxX *float32 `yaml:"parallaxX,omitempty"`
	// ParallaxY scales how fast a world-space layer scrolls vertically relative to the camera; nil defaults to 1. Zero pins the layer vertically against camera translation, 0.5 scrolls at half speed, and values above 1 scroll faster for foreground layers. Version 3.
	ParallaxY *float32 `yaml:"parallaxY,omitempty"`
	// RepeatX tiles a world-space layer horizontally across the camera's visible region using RepeatWidth (or, when RepeatWidth is 0, the layer's tilemap pixel width or the scene's virtualWidth). Version 3.
	RepeatX bool `yaml:"repeatX,omitempty"`
	// RepeatY tiles a world-space layer vertically across the camera's visible region using RepeatHeight (or, when RepeatHeight is 0, the layer's tilemap pixel height or the scene's virtualHeight). Version 3.
	RepeatY bool `yaml:"repeatY,omitempty"`
	// RepeatWidth is the horizontal repeat period in layer points when RepeatX is true; 0 uses the layer's tilemap pixel width or the scene's virtualWidth. Must be non-negative. Version 3.
	RepeatWidth float32 `yaml:"repeatWidth,omitempty" schema:"minimum=0"`
	// RepeatHeight is the vertical repeat period in layer points when RepeatY is true; 0 uses the layer's tilemap pixel height or the scene's virtualHeight. Must be non-negative. Version 3.
	RepeatHeight float32 `yaml:"repeatHeight,omitempty" schema:"minimum=0"`
	// Tilemap optionally attaches a tilemap (by Ref, external Source, or inline grid) drawn beneath Objects.
	Tilemap *TilemapDoc `yaml:"tilemap,omitempty"`
	// Objects are the placed objects in this layer.
	Objects []ObjectDoc `yaml:"objects,omitempty"`
}

// IsVisible reports whether the layer is visible, defaulting to true when Visible is nil.
func (l *LayerDoc) IsVisible() bool {
	return l == nil || l.Visible == nil || *l.Visible
}

// EffectiveSpace returns SpaceWorld when Space is empty, and l.Space otherwise.
func (l *LayerDoc) EffectiveSpace() Space {
	if l == nil || l.Space == "" {
		return SpaceWorld
	}
	return l.Space
}

// EffectiveParallaxX returns ParallaxX, defaulting to 1 when nil.
func (l *LayerDoc) EffectiveParallaxX() float32 {
	if l == nil || l.ParallaxX == nil {
		return 1
	}
	return *l.ParallaxX
}

// EffectiveParallaxY returns ParallaxY, defaulting to 1 when nil.
func (l *LayerDoc) EffectiveParallaxY() float32 {
	if l == nil || l.ParallaxY == nil {
		return 1
	}
	return *l.ParallaxY
}

// TilemapDoc describes a tilemap in one of three modes: a reference to a named top-level tilemap (Ref), an external Tiled or LDtk map (Source), or an inline grid. An inline grid is either a single grid of atlas frame numbers (Tileset and Data) or, in version 2, a layered grid that places the tiles of a YAML tileset (TilesetFile and TileLayers).
type TilemapDoc struct {
	// Ref names an entry in SceneDoc.Tilemaps. Only valid on a layer's Tilemap.
	Ref string `yaml:"ref,omitempty"`
	// Source is the slash-separated path from the project root of an external map: a Tiled map (.tmj or .tmx) or an LDtk project (.ldtk). Build loads it itself when its resolver implements FileResolver, as FSAssets does, or through a TilemapResolver.
	Source string `yaml:"source,omitempty"`
	// Level names the level of an LDtk Source to build; empty builds the project's first level. It requires a .ldtk Source. Build reads a source with a Level through a FileResolver even when the resolver implements TilemapResolver. Version 2.
	Level string `yaml:"level,omitempty"`
	// OffsetX and OffsetY shift the tilemap's top-left corner (cell 0, 0) in its layer's coordinate space in points. Version 3.
	OffsetX float32 `yaml:"offsetX,omitempty"`
	OffsetY float32 `yaml:"offsetY,omitempty"`
	// TileWidth and TileHeight are each tile's size in points for an inline grid.
	TileWidth  int `yaml:"tileWidth,omitempty" schema:"minimum=0"`
	TileHeight int `yaml:"tileHeight,omitempty" schema:"minimum=0"`
	// Width and Height are the inline grid dimensions in tiles.
	Width  int `yaml:"width,omitempty" schema:"minimum=0"`
	Height int `yaml:"height,omitempty" schema:"minimum=0"`
	// Tileset is, for a single grid, the atlas name providing frames keyed by 1-based tile ID ("1", "2", ...).
	Tileset string `yaml:"tileset,omitempty"`
	// TilesetFile is, for a layered grid, the YAML tileset (*.tileset.yaml, tilemap.TilesetDoc) whose tiles the cells place, by its slash-separated path from the project root. Version 2.
	TilesetFile string `yaml:"tilesetFile,omitempty"`
	// Data holds a single grid's Width*Height row-major tile IDs; 0 is an empty cell.
	Data []int `yaml:"data,omitempty,flow"`
	// TileLayers are a layered grid's tile layers, drawn in order, each Width by Height cells, at most MaxTileLayers of them. Version 2.
	TileLayers []TileLayerDoc `yaml:"tileLayers,omitempty"`
}

// MaxTileLayers is the most tile layers one layered grid may hold (TilemapDoc.TileLayers).
const MaxTileLayers = 64

// TileLayerDoc is one named layer of tiles of a layered grid (TilemapDoc.TileLayers), the text counterpart of one of Godot's TileMapLayer nodes.
type TileLayerDoc struct {
	// Name identifies the layer within its tilemap; tilemap.Tilemap.Layer finds it by this name once built.
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Visible controls whether the layer draws; nil defaults to true.
	Visible *bool `yaml:"visible,omitempty"`
	// Opacity is the layer's opacity in [0, 1]; nil defaults to 1.
	Opacity *float32 `yaml:"opacity,omitempty" schema:"minimum=0,maximum=1"`
	// Rows are the layer's Height rows from the top, each Width cells separated by spaces. A cell is "." when empty, or the local ID of a tile of the tileset (0, 1, ...) followed by any of h, v and d to flip it horizontally, vertically or across its diagonal, as Tiled's flip bits do: "3", "3h" and "3hvd". Encode writes the cells separated by single spaces with the letters in the order h, v, d.
	Rows []string `yaml:"rows,omitempty"`
}

// IsVisible reports whether the tile layer is visible, defaulting to true when Visible is nil.
func (l *TileLayerDoc) IsVisible() bool { return l == nil || l.Visible == nil || *l.Visible }

// EffectiveOpacity returns Opacity, defaulting to 1 when nil.
func (l *TileLayerDoc) EffectiveOpacity() float32 {
	if l == nil || l.Opacity == nil {
		return 1
	}
	return *l.Opacity
}

// Cell returns the cell at (col, row) of the layer as a tile value: 0 for an empty cell, or the tile's local ID plus 1 with tilemap's flip bits (tilemap.FlipHorizontal, FlipVertical and FlipDiagonal) set, which is the GID the cell has in the tilemap.Tilemap Build makes, whose tileset starts at GID 1.
func (l *TileLayerDoc) Cell(col, row int) (uint32, error) {
	if row < 0 || row >= len(l.Rows) {
		return 0, fmt.Errorf("sceneformat: tile layer %q has no row %d", l.Name, row)
	}
	cells, err := ParseTileRow(l.Rows[row])
	if err != nil {
		return 0, fmt.Errorf("sceneformat: tile layer %q row %d: %w", l.Name, row, err)
	}
	if col < 0 || col >= len(cells) {
		return 0, fmt.Errorf("sceneformat: tile layer %q row %d has no column %d", l.Name, row, col)
	}
	return cells[col], nil
}

// SetCell sets the cell at (col, row) of the layer to the tile value cell, as Cell returns it, and writes that row in the form Encode does.
func (l *TileLayerDoc) SetCell(col, row int, cell uint32) error {
	if row < 0 || row >= len(l.Rows) {
		return fmt.Errorf("sceneformat: tile layer %q has no row %d", l.Name, row)
	}
	cells, err := ParseTileRow(l.Rows[row])
	if err != nil {
		return fmt.Errorf("sceneformat: tile layer %q row %d: %w", l.Name, row, err)
	}
	if col < 0 || col >= len(cells) {
		return fmt.Errorf("sceneformat: tile layer %q row %d has no column %d", l.Name, row, col)
	}
	if cell&tilemap.TileIDMask == 0 {
		cell = 0
	}
	cells[col] = cell & (tilemap.TileIDMask | tilemap.FlipMask)
	l.Rows[row] = FormatTileRow(cells)
	return nil
}

// Cells returns the layer's cells row by row from the top, as Cell returns each.
func (l *TileLayerDoc) Cells() ([]uint32, error) {
	var out []uint32
	for r, row := range l.Rows {
		cells, err := ParseTileRow(row)
		if err != nil {
			return nil, fmt.Errorf("sceneformat: tile layer %q row %d: %w", l.Name, r, err)
		}
		out = append(out, cells...)
	}
	return out, nil
}

// ParseTileRow parses one row of a tile layer (TileLayerDoc.Rows) into its cells, as TileLayerDoc.Cell returns each. Cells are separated by spaces or tabs, and a cell's flip letters may come in any order, each at most once.
func ParseTileRow(row string) ([]uint32, error) {
	fields := strings.Fields(row)
	cells := make([]uint32, len(fields))
	for i, f := range fields {
		if f == "." {
			continue
		}
		digits := strings.TrimRight(f, "hvd")
		if digits == "" || strings.TrimLeft(digits, "0123456789") != "" {
			return nil, fmt.Errorf("cell %d %q is not \".\" or a tile ID followed by h, v or d", i, f)
		}
		id, err := strconv.ParseUint(digits, 10, 32)
		if err != nil || id >= uint64(tilemap.TileIDMask) {
			return nil, fmt.Errorf("cell %d %q has a tile ID above %d", i, f, tilemap.TileIDMask-1)
		}
		cell := uint32(id) + 1
		for _, c := range f[len(digits):] {
			bit := tilemap.FlipHorizontal
			switch c {
			case 'v':
				bit = tilemap.FlipVertical
			case 'd':
				bit = tilemap.FlipDiagonal
			}
			if cell&bit != 0 {
				return nil, fmt.Errorf("cell %d %q flips %c twice", i, f, c)
			}
			cell |= bit
		}
		cells[i] = cell
	}
	return cells, nil
}

// FormatTileRow writes cells, tile values as TileLayerDoc.Cell returns them, as one row of a tile layer in the form Encode writes: single spaces between cells, "." for an empty one and flip letters in the order h, v, d.
func FormatTileRow(cells []uint32) string {
	var b strings.Builder
	for i, cell := range cells {
		if i > 0 {
			b.WriteByte(' ')
		}
		id := cell & tilemap.TileIDMask
		if id == 0 {
			b.WriteByte('.')
			continue
		}
		b.WriteString(strconv.FormatUint(uint64(id-1), 10))
		if cell&tilemap.FlipHorizontal != 0 {
			b.WriteByte('h')
		}
		if cell&tilemap.FlipVertical != 0 {
			b.WriteByte('v')
		}
		if cell&tilemap.FlipDiagonal != 0 {
			b.WriteByte('d')
		}
	}
	return b.String()
}

// IsRef reports whether t references a top-level tilemap by name.
func (t *TilemapDoc) IsRef() bool { return t != nil && t.Ref != "" }

// IsExternal reports whether t references an external tilemap source file.
func (t *TilemapDoc) IsExternal() bool { return t != nil && t.Source != "" }

// IsInline reports whether t carries an inline tile grid, single or layered.
func (t *TilemapDoc) IsInline() bool {
	return t != nil && (t.TileWidth != 0 || t.TileHeight != 0 || t.Width != 0 || t.Height != 0 || t.Tileset != "" || len(t.Data) > 0 || t.HasTilesetFile() || t.IsLayered())
}

// HasTilesetFile reports whether t names a YAML tileset file (TilesetFile), either in a layered grid (TileLayers) or in place of a sprite atlas on a single inline grid (Data).
func (t *TilemapDoc) HasTilesetFile() bool {
	return t != nil && t.TilesetFile != ""
}

// IsLayered reports whether t is a layered grid, one that holds tile layers (TileLayers) or names a YAML tileset (TilesetFile) without flat Data.
func (t *TilemapDoc) IsLayered() bool {
	return t != nil && (len(t.TileLayers) > 0 || (t.TilesetFile != "" && len(t.Data) == 0))
}

// TileLayer returns the tile layer of a layered grid named name, or nil.
func (t *TilemapDoc) TileLayer(name string) *TileLayerDoc {
	if t == nil {
		return nil
	}
	for i := range t.TileLayers {
		if t.TileLayers[i].Name == name {
			return &t.TileLayers[i]
		}
	}
	return nil
}

// ObjectDoc is one entity or visual object placed on a layer.
type ObjectDoc struct {
	// ID uniquely identifies the object across the entire scene.
	ID string `yaml:"id" schema:"required,minLength=1"`
	// Name is an optional human-readable display name.
	Name string `yaml:"name,omitempty"`
	// Prefab optionally names a prefab or archetype template.
	Prefab string `yaml:"prefab,omitempty"`
	// X and Y are where the object's origin sits in its parent's space, which for a top-level object is its layer's coordinate space.
	X float32 `yaml:"x,omitempty"`
	Y float32 `yaml:"y,omitempty"`
	// PosZ is the optional 3D Z coordinate (depth in 3D scenes) in its parent's space, distinct from 2D draw order Z.
	PosZ float32 `yaml:"posZ,omitempty"`
	// OriginX and OriginY are the pivot point inside the sprite's untrimmed frame.
	OriginX float32 `yaml:"originX,omitempty"`
	OriginY float32 `yaml:"originY,omitempty"`
	// Rotation turns the object clockwise in radians about its origin in 2D space.
	Rotation float32 `yaml:"rotation,omitempty"`
	// RotX, RotY, and RotZ are optional 3D Euler rotations in radians about the X, Y, and Z axes, applied in that order about fixed axes (the rotation Rz * Ry * Rx).
	RotX float32 `yaml:"rotX,omitempty"`
	RotY float32 `yaml:"rotY,omitempty"`
	RotZ float32 `yaml:"rotZ,omitempty"`
	// ScaleX, ScaleY, and ScaleZ scale the object about its origin; nil defaults to 1. Negative mirrors.
	ScaleX *float32 `yaml:"scaleX,omitempty"`
	ScaleY *float32 `yaml:"scaleY,omitempty"`
	ScaleZ *float32 `yaml:"scaleZ,omitempty"`
	// Depth is the optional 3D extent along the Z axis in world units; 0 uses a default extent.
	Depth float32 `yaml:"depth,omitempty" schema:"minimum=0"`
	// Mesh optionally names the built-in primitive the object draws in 3D: "box", "sphere", "plane", "capsule" or "cylinder" (MeshPrimitives lists them, and package scenemodel builds them). It cannot be combined with Model; scale the object to size the primitive.
	Mesh string `yaml:"mesh,omitempty" schema:"enum=box|sphere|plane|capsule|cylinder"`
	// Model optionally names the glTF 2.0 model the object draws in 3D, a .gltf or .glb file named by its slash-separated path from the project root, such as "models/crate.glb". Without ModelNode or ModelMesh the object draws every node of the model's default scene; package scenemodel loads it through a ModelResolver. Version 2.
	Model string `yaml:"model,omitempty"`
	// ModelNode optionally names one node of Model, the first with that name in the file, which the object draws with its descendants in place of the whole model; the object's own transform replaces the node's. It requires Model and cannot be combined with ModelMesh. Version 2.
	ModelNode string `yaml:"modelNode,omitempty"`
	// ModelMesh optionally names one mesh of Model, the first with that name in the file, which the object draws at its origin in place of the whole model. It requires Model and cannot be combined with ModelNode. Version 2.
	ModelMesh string `yaml:"modelMesh,omitempty"`
	// GridMap optionally names a 3D grid map file (*.gridmap.yaml or *.gridmap.yml) by its slash-separated path from the project root, such as "levels/dungeon.gridmap.yaml". Package scenemodel loads it and instances its tiles with world3d.GridMap.BuildInstances. It cannot be combined with Mesh or Model. Version 2.
	GridMap string `yaml:"gridMap,omitempty"`
	// Occluder optionally marks this 3D object as an occluder for render3d.OcclusionBuffer, using either its own 3D geometry (mode "model") or an explicit local-space box (mode "box"). Version 2.
	Occluder *OccluderDoc `yaml:"occluder,omitempty"`
	// FlipX and FlipY mirror the sprite inside its untrimmed frame box.
	FlipX bool `yaml:"flipX,omitempty"`
	FlipY bool `yaml:"flipY,omitempty"`
	// Alpha is the object's opacity in [0, 1]; nil defaults to 1.
	Alpha *float32 `yaml:"alpha,omitempty" schema:"minimum=0,maximum=1"`
	// Z orders objects within the layer, children included; lower Z draws first, and ties keep document order with each parent before its children.
	Z int `yaml:"z,omitempty"`
	// Sprite optionally binds the object to a frame in a sprite.Atlas.
	Sprite *SpriteRef `yaml:"sprite,omitempty"`
	// Material optionally names a shared material resource file by its slash-separated path from the project root, such as "materials/stone.material.yaml". Build resolves it through a MaterialResolver; the sprite draws with the material's opacity and blend mode, so every scene that uses the file changes when it is edited. In 3D it replaces every material of the object's Model, or gives its Mesh primitive its surface (package scenemodel).
	Material string `yaml:"material,omitempty"`
	// Particles optionally binds the object to a 2D particle emitter YAML file (particles.Config) and optional sprite atlas, so Build constructs a live particles.Emitter that BuiltScene.Update steps and BuiltScene.Draw renders at the object's world transform. Version 2.
	Particles *ParticlesRef `yaml:"particles,omitempty"`
	// Light optionally attaches a directional, point or spot 3D light to this object, placed and aimed by its 3D transform (local -Z). Version 4.
	Light *LightDoc `yaml:"light,omitempty"`
	// Camera optionally attaches a perspective or orthographic 3D camera to this object, placed and oriented by its 3D transform (looking down local -Z). Version 4.
	Camera *Camera3DDoc `yaml:"camera,omitempty"`
	// ReflectionProbe optionally attaches a local 3D reflection probe volume to this object, capturing reflections from its world position over its influence box. Version 4.
	ReflectionProbe *ReflectionProbeDoc `yaml:"reflectionProbe,omitempty"`
	// LightProbeGrid optionally attaches a regular 3D lattice of diffuse L2 spherical harmonic irradiance probes to this object. Version 4.
	LightProbeGrid *LightProbeGridDoc `yaml:"lightProbeGrid,omitempty"`
	// Components holds structured component data keyed by component type name.
	Components map[string]any `yaml:"components,omitempty"`
	// Properties holds arbitrary custom metadata for gameplay scripts or editors. Encode writes a whole-number float with a decimal point (2.0), so that every value reads back with the kind it was written with.
	Properties map[string]any `yaml:"properties,omitempty"`
	// Scene optionally instances another scene file, named by its slash-separated path from the project root such as "scenes/enemy.scene.yaml". Resolve makes the top-level objects of every layer of that scene, in document order, this object's first children, ahead of the ones in Children, with each instanced object's ID prefixed by this object's ID and a slash ("enemy_1/body"). The instanced scene's layer settings, camera, background and virtual size do not carry over, and a scene whose layers hold a tilemap cannot be instanced. Version 2.
	Scene string `yaml:"scene,omitempty"`
	// Overrides changes objects of the instanced Scene, keyed by their IDs inside that scene ("body", or "gun/barrel" for an object of a scene it instances in turn); it requires Scene. Version 2.
	Overrides map[string]ObjectOverride `yaml:"overrides,omitempty"`
	// Children are objects attached to this one, in document order. A child's transform (X, Y, Rotation, ScaleX, ScaleY, and in 3D PosZ, RotX, RotY, RotZ, ScaleZ) is local to this object, and its alpha multiplies this object's. Children belong to their parent's layer; their IDs share the scene-wide namespace, and nesting is limited to MaxObjectDepth levels.
	Children []ObjectDoc `yaml:"children,omitempty"`
}

// ObjectOverride changes one object of an instanced or inherited scene (ObjectDoc.Overrides, SceneDoc.Overrides). Each field set replaces the ObjectDoc field of the same name; a field left unset keeps it. Components and Properties replace the object's entries key by key and keep its other keys. An override cannot change an object's ID or children, or what it instances.
type ObjectOverride struct {
	// Name replaces the object's name.
	Name *string `yaml:"name,omitempty"`
	// Prefab replaces the prefab the object names.
	Prefab *string `yaml:"prefab,omitempty"`
	// X replaces where the object's origin sits along x in its parent's space.
	X *float32 `yaml:"x,omitempty"`
	// Y replaces where the object's origin sits along y in its parent's space.
	Y *float32 `yaml:"y,omitempty"`
	// PosZ replaces the object's 3D Z coordinate in its parent's space.
	PosZ *float32 `yaml:"posZ,omitempty"`
	// OriginX replaces the x of the object's pivot point.
	OriginX *float32 `yaml:"originX,omitempty"`
	// OriginY replaces the y of the object's pivot point.
	OriginY *float32 `yaml:"originY,omitempty"`
	// Rotation replaces the object's clockwise 2D rotation in radians.
	Rotation *float32 `yaml:"rotation,omitempty"`
	// RotX replaces the object's rotation about the x axis in 3D.
	RotX *float32 `yaml:"rotX,omitempty"`
	// RotY replaces the object's rotation about the y axis in 3D.
	RotY *float32 `yaml:"rotY,omitempty"`
	// RotZ replaces the object's rotation about the z axis in 3D.
	RotZ *float32 `yaml:"rotZ,omitempty"`
	// ScaleX replaces the object's x scale.
	ScaleX *float32 `yaml:"scaleX,omitempty"`
	// ScaleY replaces the object's y scale.
	ScaleY *float32 `yaml:"scaleY,omitempty"`
	// ScaleZ replaces the object's z scale in 3D.
	ScaleZ *float32 `yaml:"scaleZ,omitempty"`
	// Depth replaces the object's 3D extent along the Z axis.
	Depth *float32 `yaml:"depth,omitempty" schema:"minimum=0"`
	// Mesh replaces the built-in 3D primitive the object draws; "" removes it, as an override that gives the object a Model in place of a primitive must.
	Mesh *string `yaml:"mesh,omitempty" schema:"enum=|box|sphere|plane|capsule|cylinder"`
	// Model replaces the glTF model the object draws, a slash-separated path from the project root.
	Model *string `yaml:"model,omitempty"`
	// ModelNode replaces the node of the model the object draws.
	ModelNode *string `yaml:"modelNode,omitempty"`
	// ModelMesh replaces the mesh of the model the object draws.
	ModelMesh *string `yaml:"modelMesh,omitempty"`
	// GridMap replaces the 3D grid map file the object draws, a slash-separated path from the project root; "" removes it.
	GridMap *string `yaml:"gridMap,omitempty"`
	// Occluder replaces the object's 3D occluder definition.
	Occluder *OccluderDoc `yaml:"occluder,omitempty"`
	// FlipX replaces whether the sprite is mirrored horizontally.
	FlipX *bool `yaml:"flipX,omitempty"`
	// FlipY replaces whether the sprite is mirrored vertically.
	FlipY *bool `yaml:"flipY,omitempty"`
	// Alpha replaces the object's opacity, in [0, 1].
	Alpha *float32 `yaml:"alpha,omitempty" schema:"minimum=0,maximum=1"`
	// Z replaces the object's draw order within its layer.
	Z *int `yaml:"z,omitempty"`
	// Sprite replaces the atlas frame the object draws.
	Sprite *SpriteRef `yaml:"sprite,omitempty"`
	// Material replaces the material resource file the object names, a slash-separated path from the project root.
	Material *string `yaml:"material,omitempty"`
	// Particles replaces the particle emitter file and optional atlas the object runs.
	Particles *ParticlesRef `yaml:"particles,omitempty"`
	// Light replaces the 3D light attached to the object. Version 4.
	Light *LightDoc `yaml:"light,omitempty"`
	// Camera replaces the 3D camera attached to the object. Version 4.
	Camera *Camera3DDoc `yaml:"camera,omitempty"`
	// ReflectionProbe replaces the 3D reflection probe attached to the object. Version 4.
	ReflectionProbe *ReflectionProbeDoc `yaml:"reflectionProbe,omitempty"`
	// LightProbeGrid replaces the 3D light probe grid attached to the object. Version 4.
	LightProbeGrid *LightProbeGridDoc `yaml:"lightProbeGrid,omitempty"`
	// Components replaces the object's components key by key, keeping the others.
	Components map[string]any `yaml:"components,omitempty"`
	// Properties replaces the object's custom properties key by key, keeping the others.
	Properties map[string]any `yaml:"properties,omitempty"`
}

// EffectiveScaleX returns ScaleX, defaulting to 1 when nil.
func (o *ObjectDoc) EffectiveScaleX() float32 {
	if o == nil || o.ScaleX == nil {
		return 1
	}
	return *o.ScaleX
}

// EffectiveScaleY returns ScaleY, defaulting to 1 when nil.
func (o *ObjectDoc) EffectiveScaleY() float32 {
	if o == nil || o.ScaleY == nil {
		return 1
	}
	return *o.ScaleY
}

// EffectiveScaleZ returns ScaleZ, defaulting to 1 when nil.
func (o *ObjectDoc) EffectiveScaleZ() float32 {
	if o == nil || o.ScaleZ == nil {
		return 1
	}
	return *o.ScaleZ
}

// LocalTransform returns the object's 2D transform relative to its parent (or to its layer for a top-level object): translate by (X, Y), rotate by Rotation, then scale by (ScaleX, ScaleY). OriginX, OriginY and the flips place a sprite inside this space and are not part of it.
func (o *ObjectDoc) LocalTransform() math2d.Affine {
	if o == nil {
		return math2d.Identity()
	}
	return math2d.TRS(math2d.Vec2{X: o.X, Y: o.Y}, o.Rotation, math2d.Vec2{X: o.EffectiveScaleX(), Y: o.EffectiveScaleY()})
}

// EffectiveAlpha returns Alpha, defaulting to 1 when nil.
func (o *ObjectDoc) EffectiveAlpha() float32 {
	if o == nil || o.Alpha == nil {
		return 1
	}
	return *o.Alpha
}

// SpriteRef identifies one frame inside a named sprite.Atlas.
type SpriteRef struct {
	// Atlas names the sprite.Atlas an AssetResolver looks up.
	Atlas string `yaml:"atlas" schema:"required,minLength=1"`
	// Frame names the frame inside the atlas.
	Frame string `yaml:"frame" schema:"required,minLength=1"`
}

// OccluderMode selects the geometry a 3D object contributes to a render3d.OcclusionBuffer (OccluderDoc.Mode).
type OccluderMode string

const (
	// OccluderModel uses the object's own 3D geometry (Mesh primitive or glTF Model) as an occluder.
	OccluderModel OccluderMode = "model"
	// OccluderBox uses an explicit local-space solid box (Center, Size) transformed by the object's world matrix as an occluder.
	OccluderBox OccluderMode = "box"
)

// OccluderDoc marks a 3D object as an occluder that hides objects behind it in render3d.OcclusionBuffer (ObjectDoc.Occluder). Version 2.
type OccluderDoc struct {
	// Mode selects the occluder geometry: "model" (the object's own Mesh or Model) or "box" (an explicit local-space solid box).
	Mode OccluderMode `yaml:"mode" schema:"required,enum=model|box"`
	// Center is the optional local-space center [x, y, z] of a "box" occluder; nil centers it at the object's origin. Only valid when Mode is "box".
	Center *[3]float32 `yaml:"center,omitempty,flow"`
	// Size is the optional local-space dimensions [width, height, depth] of a "box" occluder; nil defaults to [1, 1, 1], scaled by the object's transform. When set, every axis must be positive. Only valid when Mode is "box".
	Size *[3]float32 `yaml:"size,omitempty,flow"`
	// DoubleSided rasterizes both front and back faces of a "model" occluder when true.
	DoubleSided bool `yaml:"doubleSided,omitempty"`
}

// EffectiveCenter returns Center, defaulting to [0, 0, 0] when nil.
func (o *OccluderDoc) EffectiveCenter() [3]float32 {
	if o == nil || o.Center == nil {
		return [3]float32{}
	}
	return *o.Center
}

// EffectiveSize returns Size, defaulting to [1, 1, 1] when nil or all zero.
func (o *OccluderDoc) EffectiveSize() [3]float32 {
	if o == nil || o.Size == nil || *o.Size == ([3]float32{}) {
		return [3]float32{1, 1, 1}
	}
	return *o.Size
}

// LocalBounds returns the local-space min and max corners of a "box" occluder.
func (o *OccluderDoc) LocalBounds() (lo, hi [3]float32) {
	if o == nil {
		return lo, hi
	}
	sz := o.EffectiveSize()
	hx, hy, hz := sz[0]*0.5, sz[1]*0.5, sz[2]*0.5
	c := o.EffectiveCenter()
	return [3]float32{c[0] - hx, c[1] - hy, c[2] - hz}, [3]float32{c[0] + hx, c[1] + hy, c[2] + hz}
}

// ParticlesRef binds an object to a versioned 2D particle emitter YAML file (particles.Config) and an optional sprite atlas for its frameNames.
type ParticlesRef struct {
	// File is the slash-separated path from the project root to a particle emitter YAML file, such as "fx/fire.particles.yaml".
	File string `yaml:"file" schema:"required,minLength=1"`
	// Atlas optionally names the sprite.Atlas an AssetResolver looks up to resolve the emitter's frameNames.
	Atlas string `yaml:"atlas,omitempty"`
}

// MaxObjectDepth is how many levels of objects a layer may nest: a top-level object is at depth 1, its children at depth 2, and so on. Validate refuses a deeper tree.
const MaxObjectDepth = 64

// WalkObjects calls fn for every object in objs and its descendants, depth first with each parent before its children and siblings in document order. parent is nil for an object in objs. The walk stops when fn returns false, and WalkObjects reports whether it visited every object.
func WalkObjects(objs []ObjectDoc, fn func(o, parent *ObjectDoc) bool) bool {
	return walkObjects(objs, nil, fn)
}

func walkObjects(objs []ObjectDoc, parent *ObjectDoc, fn func(o, parent *ObjectDoc) bool) bool {
	for i := range objs {
		o := &objs[i]
		if !fn(o, parent) {
			return false
		}
		if !walkObjects(o.Children, o, fn) {
			return false
		}
	}
	return true
}

// errShearedSprite explains why a sprite whose composed world transform is sheared is refused.
var errShearedSprite = errors.New("its world transform is sheared (a rotated object under a non-uniformly scaled parent), which a sprite cannot draw")

// CheckObjectTree checks the shape of the tree rooted at o, taking o as a top-level object: nesting no deeper than MaxObjectDepth levels, and no sprite whose composed world transform is sheared. Validate makes the same checks for every layer of a scene; CheckObjectTree lets a caller such as an editor refuse a tree or a transform change before it reaches a document. It does not check IDs or field values.
func CheckObjectTree(o *ObjectDoc) error {
	return checkObjectTree(o, nil, 1)
}

func checkObjectTree(o *ObjectDoc, parentWorld *math2d.Affine, depth int) error {
	if depth > MaxObjectDepth {
		return fmt.Errorf("sceneformat: object %q nests deeper than %d levels", o.ID, MaxObjectDepth)
	}
	world, ok := composeObjectWorld(parentWorld, o)
	if !ok {
		return fmt.Errorf("sceneformat: object %q: %w", o.ID, errShearedSprite)
	}
	for i := range o.Children {
		if err := checkObjectTree(&o.Children[i], &world, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// composeObjectWorld returns o's world transform under a parent whose world transform is parentWorld (nil for a top-level object), composed the way Build composes BuiltObject.World. It reports false when o has a sprite that this world transform shears, which only a child can be.
func composeObjectWorld(parentWorld *math2d.Affine, o *ObjectDoc) (math2d.Affine, bool) {
	world := o.LocalTransform()
	if parentWorld == nil {
		return world, true
	}
	world = parentWorld.Mul(world)
	if o.Sprite != nil {
		if _, _, _, _, _, ok := decomposeTRS(world); !ok {
			return world, false
		}
	}
	return world, true
}

// Bool returns a pointer to v for optional YAML boolean fields such as LayerDoc.Visible.
func Bool(v bool) *bool { return &v }

// Int returns a pointer to v for optional YAML integer fields such as ObjectOverride.Z and LayerOverride.Order.
func Int(v int) *int { return &v }

// String returns a pointer to v for optional YAML string fields such as ObjectOverride.Name.
func String(v string) *string { return &v }

// Float32 returns a pointer to v for optional YAML float fields such as ScaleX, ScaleY, Alpha and Zoom.
func Float32(v float32) *float32 { return &v }

// Parse decodes a YAML scene document and validates it. Unknown YAML fields and trailing YAML documents are rejected. Component payloads are not checked against any registry; use ParseWith to check them.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it, and wrap the *FieldError Validate returned for a document that decodes but breaks the schema; a caller that read data from a file names it with loaderr.WithFile, or with ParseOptions.File.
func Parse(data []byte) (*SceneDoc, error) {
	return ParseWith(data, ParseOptions{})
}

// ParseOptions configures ParseWith.
type ParseOptions struct {
	// File names the source in errors, such as "scenes/level1.scene.yaml": the File of a *ComponentError or of a *loaderr.Error. Empty leaves it unnamed.
	File string
	// Components, when non-nil, checks every object's components against the registered types: an unregistered type, a misspelled or unknown field, a value of the wrong kind or out of range fails the load with a *ComponentError carrying File, Line and Column. It also checks the scene's connections, unless the scene instances or inherits other scene files whose objects they may name: an event or handler no component of its object declares, and an argument the handler has no param for or of the wrong kind or out of range, fail the load with a *ConnectionError carrying File, Line and Column.
	Components *ComponentRegistry
	// PhysicsLayers, when non-nil, validates every named collision layer and mask in built-in physics components (collider2d, trigger2d, collider3d, trigger3d) against the table. An unknown or misspelled layer name fails the load with a *loaderr.Error carrying File and 1-based Line and Column.
	PhysicsLayers *PhysicsLayerTable
}

// ParseWith is Parse with a source name for errors and an optional component registry. Defaults of unset component fields are not written into the document, so saving it again changes nothing; read them with ComponentRegistry.Resolve.
func ParseWith(data []byte, opts ParseOptions) (*SceneDoc, error) {
	doc, err := parse(data, opts.Components, opts.PhysicsLayers)
	if err != nil {
		var ce *ComponentError
		if errors.As(err, &ce) {
			ce.File = opts.File
			return nil, ce
		}
		var cne *ConnectionError
		if errors.As(err, &cne) {
			cne.File = opts.File
			return nil, cne
		}
		return nil, loaderr.Locate(fieldError(err, data), opts.File, data, fixScene)
	}
	return doc, nil
}

// fixScene is the fix for a scene document error that has no more specific one.
const fixScene = "correct the scene document to match its version's schema in docs/en/scene.md"

// fieldError returns err as a *loaderr.Error about the field a *FieldError in its chain names, which wraps err, or err itself when it holds no FieldError.
func fieldError(err error, data []byte) error {
	var fe *FieldError
	if !errors.As(err, &fe) {
		return err
	}
	return &loaderr.Error{Field: fieldPath(data, fe.Path), Problem: fe.Error(), Fix: fixScene, Err: err}
}

// fieldPath writes steps, a FieldError's path, as loaderr.Path writes it: a step into a sequence of data is an index, any other a key. A step past what data holds is an index when it is a decimal number.
func fieldPath(data []byte, steps []string) string {
	var root yaml.Node
	n := &root
	if yaml.Unmarshal(data, &root) != nil {
		n = nil
	} else if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	parts := make([]any, 0, len(steps))
	for _, step := range steps {
		for n != nil && n.Kind == yaml.AliasNode {
			n = n.Alias
		}
		var next *yaml.Node
		switch {
		case n != nil && n.Kind == yaml.MappingNode:
			parts = append(parts, step)
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == step {
					next = n.Content[i+1]
					break
				}
			}
		default:
			i, err := strconv.Atoi(step)
			if err != nil || i < 0 || (n != nil && n.Kind != yaml.SequenceNode) {
				parts = append(parts, step)
				break
			}
			parts = append(parts, i)
			if n != nil && i < len(n.Content) {
				next = n.Content[i]
			}
		}
		n = next
	}
	return loaderr.Path(parts...)
}

func parse(data []byte, reg *ComponentRegistry, layers *PhysicsLayerTable) (*SceneDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("sceneformat: empty scene document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc SceneDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("sceneformat: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{Line: trailingDocumentLine(data), Problem: "sceneformat: unexpected trailing YAML document", Fix: "keep one scene per file; remove the --- and the document after it"}
		}
		return nil, fmt.Errorf("sceneformat: decode: %w", err)
	}
	if err := Validate(&doc); err != nil {
		return nil, err
	}
	var root yaml.Node
	haveRoot := false
	loadRoot := func() error {
		if haveRoot {
			return nil
		}
		if err := yaml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("sceneformat: decode: %w", err)
		}
		haveRoot = true
		return nil
	}
	if reg != nil {
		if err := loadRoot(); err != nil {
			return nil, err
		}
		if err := checkComponentNodes(&root, &doc, reg); err != nil {
			return nil, err
		}
		// The node walk above locates errors in plain mappings; this pass also covers values it could not see key by key, such as merge keys.
		if err := ValidateComponents(&doc, reg); err != nil {
			return nil, err
		}
		// The objects of instanced and inherited scenes are not in this file, so their connections are checked once Resolve has merged them (Connect, ValidateConnections).
		if !doc.RefersToScenes() {
			if _, err := bindConnections(&doc, reg); err != nil {
				locateConnectionError(&root, err)
				return nil, err
			}
		}
	}
	if layers != nil {
		if err := loadRoot(); err != nil {
			return nil, err
		}
		if err := checkPhysicsLayerNodes(&root, &doc, layers); err != nil {
			return nil, err
		}
		if err := ValidatePhysicsLayers(&doc, layers); err != nil {
			return nil, err
		}
	}
	yamlzero.Normalize(&doc)
	return &doc, nil
}

// trailingDocumentLine returns the 1-based line of the first "---" document marker in data that follows content, which starts a second document, or 0 when there is none.
func trailingDocumentLine(data []byte) int {
	sawContent := false
	for i, line := range bytes.Split(data, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("---")); ok && (len(rest) == 0 || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r') {
			if sawContent {
				return i + 1
			}
			continue
		}
		if t := bytes.TrimSpace(line); len(t) > 0 && t[0] != '#' {
			sawContent = true
		}
	}
	return 0
}

// Encode validates doc and serializes it as deterministic 2-space YAML, starting with the line SchemaCommentFor returns for its version. A whole-number float in a custom property (ObjectDoc.Properties) is written with a decimal point, such as 2.0, so that it reads back as a float and not an int.
func Encode(doc *SceneDoc) ([]byte, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(SchemaCommentFor(doc.Version) + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(canonicalComponents(canonicalTilemaps(floatKinds(yamlzero.Clean(doc))))); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("sceneformat: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("sceneformat: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// canonicalTilemaps returns doc, which validates, for encoding with every row of its layered grids written as FormatTileRow writes it, so that a row a person aligned with extra spaces or wrote with its flip letters in another order is saved in one form. doc is not changed: the copy shares everything but the tilemaps and layers it rewrites.
func canonicalTilemaps(doc *SceneDoc) *SceneDoc {
	out := *doc
	if doc.Tilemaps != nil {
		out.Tilemaps = make(map[string]*TilemapDoc, len(doc.Tilemaps))
		for name, tm := range doc.Tilemaps {
			out.Tilemaps[name] = canonicalTilemap(tm)
		}
	}
	if doc.LayerOverrides != nil {
		out.LayerOverrides = make(map[string]LayerOverride, len(doc.LayerOverrides))
		for name, lo := range doc.LayerOverrides {
			lo.Tilemap = canonicalTilemap(lo.Tilemap)
			out.LayerOverrides[name] = lo
		}
	}
	if doc.Layers != nil {
		out.Layers = make([]LayerDoc, len(doc.Layers))
		for i, l := range doc.Layers {
			l.Tilemap = canonicalTilemap(l.Tilemap)
			out.Layers[i] = l
		}
	}
	return &out
}

// canonicalTilemap returns tm with its tile layers' rows in canonical form, or tm itself when it has no tile layers.
func canonicalTilemap(tm *TilemapDoc) *TilemapDoc {
	if tm == nil || len(tm.TileLayers) == 0 {
		return tm
	}
	out := *tm
	out.TileLayers = make([]TileLayerDoc, len(tm.TileLayers))
	for i, l := range tm.TileLayers {
		rows := make([]string, len(l.Rows))
		for r, row := range l.Rows {
			// Validate has parsed every row, so this cannot fail.
			cells, _ := ParseTileRow(row)
			rows[r] = FormatTileRow(cells)
		}
		l.Rows = rows
		out.TileLayers[i] = l
	}
	return &out
}

// FieldError is the error Validate returns: what is wrong with a scene document and which field it is about, so a tool such as `game check` can point at the line that holds it.
type FieldError struct {
	// Path leads from the document's root to the field, one mapping key or decimal sequence index per step, such as ["layers", "2", "objects", "0", "alpha"]. It is empty when the error is about the whole document. A step may name a field the document leaves out, such as a required tileWidth, in which case the nearest written field is the one to point at.
	Path []string
	// Err is the error itself; FieldError's message is Err's.
	Err error
}

// Error returns Err's message.
func (e *FieldError) Error() string { return e.Err.Error() }

// Unwrap returns Err.
func (e *FieldError) Unwrap() error { return e.Err }

// fieldErr returns a FieldError at path with a formatted message.
func fieldErr(path []string, format string, args ...any) error {
	return &FieldError{Path: path, Err: fmt.Errorf(format, args...)}
}

// at returns base followed by steps, in a slice of its own so that paths built from one base never share storage.
func at(base []string, steps ...string) []string {
	p := make([]string, 0, len(base)+len(steps))
	p = append(p, base...)
	return append(p, steps...)
}

// Validate checks that doc conforms to the schema rules of its version. It checks a scene's references to other scene files only for their form; Resolve loads them and validates the result. Every error it returns is a *FieldError naming the field at fault. When several fields are wrong it reports the first in document order, taking the entries of Tilemaps in key order.
func Validate(doc *SceneDoc) error {
	if doc == nil {
		return fieldErr(nil, "sceneformat: nil scene document")
	}
	if doc.Version < MinVersion || doc.Version > CurrentVersion {
		return fieldErr([]string{"version"}, "sceneformat: unsupported version %d (want %d to %d)", doc.Version, MinVersion, CurrentVersion)
	}
	if doc.UID != "" && !resource.ValidUID(doc.UID) {
		return fieldErr([]string{"uid"}, "sceneformat: uid %q must be %d characters of 0-9 and a-z without i, l, o and u", doc.UID, resource.UIDLength)
	}
	if err := validateSceneRefs(doc); err != nil {
		return err
	}
	if doc.NavMesh != "" && !validFilePath(doc.NavMesh) {
		return fieldErr([]string{"navMesh"}, "sceneformat: navMesh %q must be a slash-separated path relative to the project root", doc.NavMesh)
	}
	if !isFinite(doc.VirtualWidth) || !isFinite(doc.VirtualHeight) || doc.VirtualWidth < 0 || doc.VirtualHeight < 0 {
		field := "virtualHeight"
		if !isFinite(doc.VirtualWidth) || doc.VirtualWidth < 0 {
			field = "virtualWidth"
		}
		return fieldErr([]string{field}, "sceneformat: virtual size (%v, %v) must be finite and non-negative", doc.VirtualWidth, doc.VirtualHeight)
	}
	if (doc.VirtualWidth == 0) != (doc.VirtualHeight == 0) {
		// Point at the size that is written: the one left out has no line.
		field := "virtualWidth"
		if doc.VirtualWidth == 0 {
			field = "virtualHeight"
		}
		return fieldErr([]string{field}, "sceneformat: virtualWidth (%v) and virtualHeight (%v) must both be positive or both be 0", doc.VirtualWidth, doc.VirtualHeight)
	}
	if doc.BackgroundColor != "" {
		if _, err := core.ParseColor(doc.BackgroundColor); err != nil {
			return fieldErr([]string{"backgroundColor"}, "sceneformat: backgroundColor %q: %w", doc.BackgroundColor, err)
		}
	}
	if doc.AmbientColor != "" {
		if _, err := core.ParseColor(doc.AmbientColor); err != nil {
			return fieldErr([]string{"ambientColor"}, "sceneformat: ambientColor %q: %w", doc.AmbientColor, err)
		}
	}
	if doc.Camera != nil {
		if err := validateCamera(doc.Camera); err != nil {
			return err
		}
	}
	if err := validateEnvironment(doc.Version, doc.Environment); err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(doc.Tilemaps)) {
		if name == "" {
			return fieldErr([]string{"tilemaps"}, "sceneformat: tilemaps contains an empty key")
		}
		if err := validateTilemap(doc.Version, []string{"tilemaps", name}, fmt.Sprintf("tilemap %q", name), doc.Tilemaps[name], false, nil, false); err != nil {
			return err
		}
	}

	layerNames := make(map[string]struct{}, len(doc.Layers))
	objectIDs := make(map[string]struct{})
	currentCameraID := ""
	for i := range doc.Layers {
		l := &doc.Layers[i]
		lp := []string{"layers", strconv.Itoa(i)}
		if l.Name == "" {
			return fieldErr(at(lp, "name"), "sceneformat: layer %d has an empty name", i)
		}
		if _, dup := layerNames[l.Name]; dup {
			return fieldErr(at(lp, "name"), "sceneformat: duplicate layer name %q", l.Name)
		}
		layerNames[l.Name] = struct{}{}

		switch l.Space {
		case "", SpaceWorld, SpaceScreen:
		default:
			return fieldErr(at(lp, "space"), "sceneformat: layer %q has invalid space %q (want %q or %q)", l.Name, l.Space, SpaceWorld, SpaceScreen)
		}
		if err := validateLayerParallax(doc.Version, lp, fmt.Sprintf("layer %q", l.Name), l.ParallaxX, l.ParallaxY, l.RepeatX, l.RepeatY, l.RepeatWidth, l.RepeatHeight); err != nil {
			return err
		}
		if l.Tilemap != nil {
			// A scene that inherits another may name the base's tilemaps, which Resolve checks once they are merged.
			if err := validateTilemap(doc.Version, at(lp, "tilemap"), fmt.Sprintf("layer %q tilemap", l.Name), l.Tilemap, true, doc.Tilemaps, doc.Inherits == ""); err != nil {
				return err
			}
		}
		if err := validateObjects(doc.Version, at(lp, "objects"), l.Name, "", nil, l.Objects, 1, objectIDs, &currentCameraID); err != nil {
			return err
		}
	}
	return validateConnections(doc, objectIDs)
}

// validateObjects checks objs, found at path in the document (the top-level objects of layer when parent is empty, or the children of the object parent, whose world transform is parentWorld), at nesting depth and their descendants, recording every ID in ids and tracking any camera marked current.
func validateObjects(version int, path []string, layer, parent string, parentWorld *math2d.Affine, objs []ObjectDoc, depth int, ids map[string]struct{}, currentCameraID *string) error {
	if len(objs) > 0 && depth > MaxObjectDepth {
		return fieldErr(path, "sceneformat: layer %q object %q nests children deeper than %d levels", layer, parent, MaxObjectDepth)
	}
	for j := range objs {
		o := &objs[j]
		op := at(path, strconv.Itoa(j))
		if o.ID == "" {
			if parent == "" {
				return fieldErr(at(op, "id"), "sceneformat: layer %q object %d has an empty id", layer, j)
			}
			return fieldErr(at(op, "id"), "sceneformat: layer %q object %q child %d has an empty id", layer, parent, j)
		}
		if _, dup := ids[o.ID]; dup {
			return fieldErr(at(op, "id"), "sceneformat: duplicate object id %q", o.ID)
		}
		ids[o.ID] = struct{}{}
		where := fmt.Sprintf("layer %q object %q", layer, o.ID)
		if version < 2 {
			if o.Particles != nil {
				return fieldErr(at(op, "particles"), "sceneformat: %s: particles needs version 2, not %d", where, version)
			}
			if o.Model != "" || o.ModelNode != "" || o.ModelMesh != "" {
				field := "model"
				if o.Model == "" {
					field = "modelNode"
					if o.ModelNode == "" {
						field = "modelMesh"
					}
				}
				return fieldErr(at(op, field), "sceneformat: %s: model, modelNode and modelMesh need version 2, not %d", where, version)
			}
			if o.GridMap != "" {
				return fieldErr(at(op, "gridMap"), "sceneformat: %s: gridMap needs version 2, not %d", where, version)
			}
			if o.Mesh != "" && !slices.Contains(meshPrimitives, o.Mesh) {
				// validateModel's hint names model, which version 1 refuses.
				return fieldErr(at(op, "mesh"), "sceneformat: %s mesh %q is not a built-in primitive (want one of %s); to draw a glTF file, move the scene to version 2 and name the file in model", where, o.Mesh, strings.Join(meshPrimitives, ", "))
			}
			if o.Occluder != nil {
				return fieldErr(at(op, "occluder"), "sceneformat: %s: occluder needs version 2, not %d", where, version)
			}
		}
		if err := validateObject(version, op, where, o); err != nil {
			return err
		}
		if o.Camera != nil && o.Camera.Current {
			if *currentCameraID != "" {
				return fieldErr(at(op, "camera", "current"), "sceneformat: %s marks a second current 3D camera (%q is already current)", where, *currentCameraID)
			}
			*currentCameraID = o.ID
		}
		if err := checkModelRefs(op, where, o); err != nil {
			return err
		}
		if err := validateInstance(version, op, layer, o); err != nil {
			return err
		}
		world, ok := composeObjectWorld(parentWorld, o)
		if !ok {
			return fieldErr(op, "sceneformat: layer %q object %q: %w", layer, o.ID, errShearedSprite)
		}
		if err := validateObjects(version, at(op, "children"), layer, o.ID, &world, o.Children, depth+1, ids, currentCameraID); err != nil {
			return err
		}
	}
	return nil
}

func validateCamera(c *CameraDoc) error {
	cp := []string{"camera"}
	for _, f := range []struct {
		field string
		v     float32
	}{{"x", c.X}, {"y", c.Y}, {"rotation", c.Rotation}} {
		if !isFinite(f.v) {
			return fieldErr(at(cp, f.field), "sceneformat: camera x, y and rotation must be finite")
		}
	}
	if c.Zoom != nil {
		if !isFinite(*c.Zoom) || *c.Zoom <= 0 {
			return fieldErr(at(cp, "zoom"), "sceneformat: camera zoom %v must be finite and positive", *c.Zoom)
		}
	}
	if c.Bounds != nil {
		b := c.Bounds
		for _, f := range []struct {
			field string
			v     float32
		}{{"x", b.X}, {"y", b.Y}, {"width", b.Width}, {"height", b.Height}} {
			if !isFinite(f.v) {
				return fieldErr(at(cp, "bounds", f.field), "sceneformat: camera bounds must be finite")
			}
		}
		if b.Width <= 0 {
			return fieldErr(at(cp, "bounds", "width"), "sceneformat: camera bounds size (%v, %v) must be positive", b.Width, b.Height)
		}
		if b.Height <= 0 {
			return fieldErr(at(cp, "bounds", "height"), "sceneformat: camera bounds size (%v, %v) must be positive", b.Width, b.Height)
		}
	}
	return nil
}

// validateTilemap checks tm, found at path in a document of the given version and described in messages as where; checkRef reports whether a ref must name an entry of table.
func validateTilemap(version int, path []string, where string, tm *TilemapDoc, allowRef bool, table map[string]*TilemapDoc, checkRef bool) error {
	if tm == nil {
		return fieldErr(path, "sceneformat: %s is nil", where)
	}
	if version < 2 {
		field := ""
		switch {
		case tm.Level != "":
			field = "level"
		case tm.TilesetFile != "":
			field = "tilesetFile"
		case len(tm.TileLayers) > 0:
			field = "tileLayers"
		}
		if field != "" {
			return fieldErr(at(path, field), "sceneformat: %s: level, tilesetFile and tileLayers need version 2, not %d", where, version)
		}
	}
	if version < 3 && (tm.OffsetX != 0 || tm.OffsetY != 0) {
		field := "offsetY"
		if tm.OffsetX != 0 {
			field = "offsetX"
		}
		return fieldErr(at(path, field), "sceneformat: %s: offsetX and offsetY need version 3, not %d", where, version)
	}
	if !isFinite(tm.OffsetX) {
		return fieldErr(at(path, "offsetX"), "sceneformat: %s has non-finite offsetX", where)
	}
	if !isFinite(tm.OffsetY) {
		return fieldErr(at(path, "offsetY"), "sceneformat: %s has non-finite offsetY", where)
	}
	isRef := tm.IsRef()
	isExt := tm.IsExternal()
	isInline := tm.IsInline()

	modes := 0
	if isRef {
		modes++
	}
	if isExt {
		modes++
	}
	if isInline {
		modes++
	}
	if modes == 0 {
		if tm.Level != "" {
			return fieldErr(at(path, "level"), "sceneformat: %s level %q requires an LDtk source", where, tm.Level)
		}
		return fieldErr(path, "sceneformat: %s must specify ref, source or inline tile grid", where)
	}
	if modes > 1 {
		return fieldErr(path, "sceneformat: %s cannot combine ref, source and inline tile grid", where)
	}
	if tm.Level != "" && (!isExt || !strings.EqualFold(pathExt(tm.Source), ".ldtk")) {
		return fieldErr(at(path, "level"), "sceneformat: %s level %q requires an LDtk (.ldtk) source", where, tm.Level)
	}
	if isRef {
		if tm.OffsetX != 0 || tm.OffsetY != 0 {
			field := "offsetY"
			if tm.OffsetX != 0 {
				field = "offsetX"
			}
			return fieldErr(at(path, field), "sceneformat: %s cannot set offsetX or offsetY on a ref tilemap; set them on the top-level tilemap instead", where)
		}
		if !allowRef {
			return fieldErr(at(path, "ref"), "sceneformat: %s cannot use ref in top-level tilemaps", where)
		}
		if _, ok := table[tm.Ref]; checkRef && !ok {
			return fieldErr(at(path, "ref"), "sceneformat: %s references unknown tilemap %q", where, tm.Ref)
		}
		return nil
	}
	if isExt {
		return nil
	}
	return validateInlineTilemap(path, where, tm)
}

// pathExt returns the extension of the slash-separated path p, as path.Ext does.
func pathExt(p string) string {
	for i := len(p) - 1; i >= 0 && p[i] != '/'; i-- {
		if p[i] == '.' {
			return p[i:]
		}
	}
	return ""
}

// maxTilesPerTilemap caps an inline grid's cells, over all its tile layers. Multiplying as int64 against it first stops a crafted width and height from overflowing to a small product, passing the length check and panicking the runtime builder on its tile index.
const maxTilesPerTilemap = 1 << 24

// validateInlineTilemap checks the inline grid tm, found at path in the document (nil when it was loaded from a source file) and described in messages as where.
func validateInlineTilemap(path []string, where string, tm *TilemapDoc) error {
	if tm.TileWidth <= 0 || tm.TileHeight <= 0 {
		field := "tileHeight"
		if tm.TileWidth <= 0 {
			field = "tileWidth"
		}
		return fieldErr(at(path, field), "sceneformat: %s tileWidth (%d) and tileHeight (%d) must be positive", where, tm.TileWidth, tm.TileHeight)
	}
	if tm.Width <= 0 || tm.Height <= 0 {
		field := "height"
		if tm.Width <= 0 {
			field = "width"
		}
		return fieldErr(at(path, field), "sceneformat: %s width (%d) and height (%d) must be positive", where, tm.Width, tm.Height)
	}
	if tm.IsLayered() {
		return validateLayeredTilemap(path, where, tm)
	}
	if tm.TilesetFile != "" {
		if tm.Tileset != "" {
			return fieldErr(at(path, "tileset"), "sceneformat: %s cannot combine tileset and tilesetFile", where)
		}
		if !validFilePath(tm.TilesetFile) {
			return fieldErr(at(path, "tilesetFile"), "sceneformat: %s tilesetFile %q must be a slash-separated path relative to the project root", where, tm.TilesetFile)
		}
	} else if tm.Tileset == "" {
		return fieldErr(at(path, "tileset"), "sceneformat: %s requires a tileset name", where)
	}
	if tm.Width > maxTilesPerTilemap || tm.Height > maxTilesPerTilemap || int64(tm.Width)*int64(tm.Height) > maxTilesPerTilemap {
		return fieldErr(at(path, "width"), "sceneformat: %s is %dx%d, more than %d tiles", where, tm.Width, tm.Height, maxTilesPerTilemap)
	}
	want := tm.Width * tm.Height
	if len(tm.Data) != want {
		return fieldErr(at(path, "data"), "sceneformat: %s has %d tile entries, want %d (%dx%d)", where, len(tm.Data), want, tm.Width, tm.Height)
	}
	for i, gid := range tm.Data {
		if gid < 0 {
			return fieldErr(at(path, "data", strconv.Itoa(i)), "sceneformat: %s tile at index %d has negative id %d", where, i, gid)
		}
	}
	return nil
}

// validateLayeredTilemap checks the layered grid tm, whose tile and grid sizes are already checked, as validateInlineTilemap does.
func validateLayeredTilemap(path []string, where string, tm *TilemapDoc) error {
	switch {
	case tm.Tileset != "":
		return fieldErr(at(path, "tileset"), "sceneformat: %s cannot combine tileset and data with tilesetFile and tileLayers", where)
	case len(tm.Data) > 0:
		return fieldErr(at(path, "data"), "sceneformat: %s cannot combine tileset and data with tilesetFile and tileLayers", where)
	case tm.TilesetFile == "":
		return fieldErr(at(path, "tilesetFile"), "sceneformat: %s requires a tilesetFile", where)
	case !validFilePath(tm.TilesetFile):
		return fieldErr(at(path, "tilesetFile"), "sceneformat: %s tilesetFile %q must be a slash-separated path relative to the project root", where, tm.TilesetFile)
	case len(tm.TileLayers) == 0:
		return fieldErr(at(path, "tileLayers"), "sceneformat: %s requires at least one tile layer", where)
	case len(tm.TileLayers) > MaxTileLayers:
		return fieldErr(at(path, "tileLayers"), "sceneformat: %s has %d tile layers, more than %d", where, len(tm.TileLayers), MaxTileLayers)
	}
	if tm.Width > maxTilesPerTilemap || tm.Height > maxTilesPerTilemap || int64(tm.Width)*int64(tm.Height)*int64(len(tm.TileLayers)) > maxTilesPerTilemap {
		return fieldErr(at(path, "width"), "sceneformat: %s is %d layers of %dx%d, more than %d tiles", where, len(tm.TileLayers), tm.Width, tm.Height, maxTilesPerTilemap)
	}
	names := make(map[string]struct{}, len(tm.TileLayers))
	for i := range tm.TileLayers {
		l := &tm.TileLayers[i]
		lp := at(path, "tileLayers", strconv.Itoa(i))
		if l.Name == "" {
			return fieldErr(at(lp, "name"), "sceneformat: %s tile layer %d has an empty name", where, i)
		}
		if _, dup := names[l.Name]; dup {
			return fieldErr(at(lp, "name"), "sceneformat: %s has two tile layers named %q", where, l.Name)
		}
		names[l.Name] = struct{}{}
		if l.Opacity != nil && (!isFinite(*l.Opacity) || *l.Opacity < 0 || *l.Opacity > 1) {
			return fieldErr(at(lp, "opacity"), "sceneformat: %s tile layer %q opacity %v must be in [0, 1]", where, l.Name, *l.Opacity)
		}
		if len(l.Rows) != tm.Height {
			return fieldErr(at(lp, "rows"), "sceneformat: %s tile layer %q has %d rows, want %d", where, l.Name, len(l.Rows), tm.Height)
		}
		for r, row := range l.Rows {
			cells, err := ParseTileRow(row)
			if err != nil {
				return fieldErr(at(lp, "rows", strconv.Itoa(r)), "sceneformat: %s tile layer %q row %d: %w", where, l.Name, r, err)
			}
			if len(cells) != tm.Width {
				return fieldErr(at(lp, "rows", strconv.Itoa(r)), "sceneformat: %s tile layer %q row %d has %d cells, want %d", where, l.Name, r, len(cells), tm.Width)
			}
		}
	}
	return nil
}

// validateObject checks o's own field values, found at path in the document; where names it in errors, such as `layer "a" object "b"`.
func validateObject(version int, path []string, where string, o *ObjectDoc) error {
	transform := []struct {
		field string
		v     float32
	}{
		{"x", o.X}, {"y", o.Y}, {"posZ", o.PosZ}, {"originX", o.OriginX}, {"originY", o.OriginY},
		{"rotation", o.Rotation}, {"rotX", o.RotX}, {"rotY", o.RotY}, {"rotZ", o.RotZ},
	}
	for _, f := range transform {
		if !isFinite(f.v) {
			return fieldErr(at(path, f.field), "sceneformat: %s has non-finite transform", where)
		}
	}
	if !isFinite(o.Depth) || o.Depth < 0 {
		return fieldErr(at(path, "depth"), "sceneformat: %s depth %v must be finite and non-negative", where, o.Depth)
	}
	if o.ScaleX != nil && !isFinite(*o.ScaleX) {
		return fieldErr(at(path, "scaleX"), "sceneformat: %s has non-finite scaleX", where)
	}
	if o.ScaleY != nil && !isFinite(*o.ScaleY) {
		return fieldErr(at(path, "scaleY"), "sceneformat: %s has non-finite scaleY", where)
	}
	if o.ScaleZ != nil && !isFinite(*o.ScaleZ) {
		return fieldErr(at(path, "scaleZ"), "sceneformat: %s has non-finite scaleZ", where)
	}
	if o.Alpha != nil {
		if !isFinite(*o.Alpha) || *o.Alpha < 0 || *o.Alpha > 1 {
			return fieldErr(at(path, "alpha"), "sceneformat: %s alpha %v must be in [0, 1]", where, *o.Alpha)
		}
	}
	if o.Sprite != nil {
		if o.Sprite.Atlas == "" || o.Sprite.Frame == "" {
			return fieldErr(at(path, "sprite"), "sceneformat: %s sprite requires both atlas and frame", where)
		}
	}
	if o.Material != "" && !validFilePath(o.Material) {
		return fieldErr(at(path, "material"), "sceneformat: %s material %q must be a slash-separated path relative to the project root", where, o.Material)
	}
	if o.Particles != nil {
		if o.Particles.File == "" {
			return fieldErr(at(path, "particles", "file"), "sceneformat: %s particles requires a file", where)
		}
		if !validFilePath(o.Particles.File) {
			return fieldErr(at(path, "particles", "file"), "sceneformat: %s particles file %q must be a slash-separated path relative to the project root", where, o.Particles.File)
		}
	}
	if err := validateLight(version, path, where, o.Light); err != nil {
		return err
	}
	if err := validateCamera3D(version, path, where, o.Camera); err != nil {
		return err
	}
	if err := validateReflectionProbe(version, path, where, o.ReflectionProbe); err != nil {
		return err
	}
	if err := validateLightProbeGrid(version, path, where, o.LightProbeGrid); err != nil {
		return err
	}
	return validateModel(path, where, o)
}

// The built-in primitives ObjectDoc.Mesh names.
const (
	// MeshBox is a 1 by 1 by 1 cube centered on the object's origin.
	MeshBox = "box"
	// MeshSphere is a sphere 1 across centered on the object's origin.
	MeshSphere = "sphere"
	// MeshPlane is a 1 by 1 square in the object's XZ plane, centered on its origin and facing +Y.
	MeshPlane = "plane"
	// MeshCapsule is a capsule 1 across and 2 tall along the object's Y axis, centered on its origin.
	MeshCapsule = "capsule"
	// MeshCylinder is a cylinder 1 across and 1 tall along the object's Y axis, centered on its origin.
	MeshCylinder = "cylinder"
)

// meshPrimitives are the values ObjectDoc.Mesh accepts, sorted.
var meshPrimitives = []string{MeshBox, MeshCapsule, MeshCylinder, MeshPlane, MeshSphere}

// MeshPrimitives returns the built-in primitive names ObjectDoc.Mesh accepts, sorted.
func MeshPrimitives() []string { return slices.Clone(meshPrimitives) }

// modelExtensions are the file extensions ObjectDoc.Model accepts, compared without case.
var modelExtensions = []string{".glb", ".gltf"}

// IsModelPath reports whether p names a glTF 2.0 file by its extension, .gltf or .glb in any case, as ObjectDoc.Model requires.
func IsModelPath(p string) bool {
	return slices.Contains(modelExtensions, strings.ToLower(path.Ext(p)))
}

// IsGridMapPath reports whether p names a 3D grid map file by its extension, .gridmap.yaml or .gridmap.yml in any case, as ObjectDoc.GridMap requires.
func IsGridMapPath(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, ".gridmap.yaml") || strings.HasSuffix(lower, ".gridmap.yml")
}

// validateModel checks the 3D look fields of o, found at path in the document; where names it in errors.
func validateModel(p []string, where string, o *ObjectDoc) error {
	if o.Mesh != "" && !slices.Contains(meshPrimitives, o.Mesh) {
		return fieldErr(at(p, "mesh"), "sceneformat: %s mesh %q is not a built-in primitive (want one of %s); name a glTF file in model instead", where, o.Mesh, strings.Join(meshPrimitives, ", "))
	}
	if o.Model != "" {
		if !validFilePath(o.Model) {
			return fieldErr(at(p, "model"), "sceneformat: %s model %q must be a slash-separated path relative to the project root", where, o.Model)
		}
		if !IsModelPath(o.Model) {
			return fieldErr(at(p, "model"), "sceneformat: %s model %q must be a glTF file ending in .gltf or .glb", where, o.Model)
		}
		if o.Mesh != "" {
			return fieldErr(at(p, "mesh"), "sceneformat: %s names both mesh %q and model %q; keep one", where, o.Mesh, o.Model)
		}
	}
	if o.GridMap != "" {
		if !validFilePath(o.GridMap) {
			return fieldErr(at(p, "gridMap"), "sceneformat: %s gridMap %q must be a slash-separated path relative to the project root", where, o.GridMap)
		}
		if !IsGridMapPath(o.GridMap) {
			return fieldErr(at(p, "gridMap"), "sceneformat: %s gridMap %q must be a grid map file ending in .gridmap.yaml or .gridmap.yml", where, o.GridMap)
		}
		if o.Mesh != "" {
			return fieldErr(at(p, "mesh"), "sceneformat: %s names both mesh %q and gridMap %q; keep one", where, o.Mesh, o.GridMap)
		}
		if o.Model != "" {
			return fieldErr(at(p, "model"), "sceneformat: %s names both model %q and gridMap %q; keep one", where, o.Model, o.GridMap)
		}
	}
	if o.ModelNode != "" && o.ModelMesh != "" {
		return fieldErr(at(p, "modelMesh"), "sceneformat: %s names both modelNode %q and modelMesh %q; keep one", where, o.ModelNode, o.ModelMesh)
	}
	if o.Occluder != nil {
		if err := validateOccluder(at(p, "occluder"), where, o.Occluder); err != nil {
			return err
		}
	}
	return nil
}

// validateOccluder checks the fields of occ at path p.
func validateOccluder(p []string, where string, occ *OccluderDoc) error {
	switch occ.Mode {
	case OccluderModel:
		if occ.Center != nil {
			return fieldErr(at(p, "center"), "sceneformat: %s occluder center requires mode %q", where, OccluderBox)
		}
		if occ.Size != nil {
			return fieldErr(at(p, "size"), "sceneformat: %s occluder size requires mode %q", where, OccluderBox)
		}
	case OccluderBox:
		if occ.Center != nil {
			for _, v := range *occ.Center {
				if !isFinite(v) {
					return fieldErr(at(p, "center"), "sceneformat: %s occluder center must be finite", where)
				}
			}
		}
		if occ.Size != nil {
			for _, v := range *occ.Size {
				if !isFinite(v) {
					return fieldErr(at(p, "size"), "sceneformat: %s occluder size must be finite", where)
				}
			}
			if *occ.Size != ([3]float32{}) && (occ.Size[0] <= 0 || occ.Size[1] <= 0 || occ.Size[2] <= 0) {
				return fieldErr(at(p, "size"), "sceneformat: %s occluder size %v must have all positive axes or all zero", where, *occ.Size)
			}
		}
	default:
		return fieldErr(at(p, "mode"), "sceneformat: %s occluder mode %q must be %q or %q", where, occ.Mode, OccluderModel, OccluderBox)
	}
	return nil
}

// CheckObjectModel checks the fields of o that say what it draws in 3D and how it occludes, as Validate checks them in a version 2 scene: Mesh is "" or one of MeshPrimitives, Model is "" or a slash-separated path to a .gltf or .glb file, Mesh and Model are not both set, nor ModelNode and ModelMesh, ModelNode and ModelMesh require Model, and Occluder (when set) is valid with mode "model" requiring Mesh or Model. It does not check o's descendants, and lets a caller such as an editor refuse an object before it reaches a document.
func CheckObjectModel(o *ObjectDoc) error {
	where := fmt.Sprintf("object %q", o.ID)
	if err := validateModel(nil, where, o); err != nil {
		return err
	}
	return checkModelRefs(nil, where, o)
}

// checkModelRefs checks that o's ModelNode, ModelMesh and model-mode Occluder refer to geometry o has, which an override may set alone on an object of another file and so is only checked on a whole object.
func checkModelRefs(p []string, where string, o *ObjectDoc) error {
	if o.Model == "" {
		switch {
		case o.ModelNode != "":
			return fieldErr(at(p, "modelNode"), "sceneformat: %s modelNode %q requires model", where, o.ModelNode)
		case o.ModelMesh != "":
			return fieldErr(at(p, "modelMesh"), "sceneformat: %s modelMesh %q requires model", where, o.ModelMesh)
		}
	}
	if o.Occluder != nil && o.Occluder.Mode == OccluderModel && o.Mesh == "" && o.Model == "" {
		return fieldErr(at(p, "occluder", "mode"), "sceneformat: %s occluder mode %q requires mesh or model", where, OccluderModel)
	}
	return nil
}

func isFinite(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// validateLayerParallax checks the parallax and repeat fields version 3 added on a layer or layerOverride at path.
func validateLayerParallax(version int, path []string, where string, px, py *float32, repeatX, repeatY bool, repeatW, repeatH float32) error {
	if version < 3 {
		field := ""
		switch {
		case px != nil:
			field = "parallaxX"
		case py != nil:
			field = "parallaxY"
		case repeatX:
			field = "repeatX"
		case repeatY:
			field = "repeatY"
		case repeatW != 0:
			field = "repeatWidth"
		case repeatH != 0:
			field = "repeatHeight"
		}
		if field != "" {
			return fieldErr(at(path, field), "sceneformat: %s: parallaxX, parallaxY, repeatX, repeatY, repeatWidth and repeatHeight need version 3, not %d", where, version)
		}
		return nil
	}
	if px != nil && !isFinite(*px) {
		return fieldErr(at(path, "parallaxX"), "sceneformat: %s has non-finite parallaxX", where)
	}
	if py != nil && !isFinite(*py) {
		return fieldErr(at(path, "parallaxY"), "sceneformat: %s has non-finite parallaxY", where)
	}
	if !isFinite(repeatW) || repeatW < 0 {
		return fieldErr(at(path, "repeatWidth"), "sceneformat: %s repeatWidth %v must be finite and non-negative", where, repeatW)
	}
	if !isFinite(repeatH) || repeatH < 0 {
		return fieldErr(at(path, "repeatHeight"), "sceneformat: %s repeatHeight %v must be finite and non-negative", where, repeatH)
	}
	return nil
}

// validFilePath reports whether p is a slash-separated file path relative to the project root, the form material and scene references take.
func validFilePath(p string) bool {
	return fs.ValidPath(p) && p != "."
}
