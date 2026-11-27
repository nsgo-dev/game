package tilemap

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// FlipHorizontal mirrors a tile left to right (Tiled GID bit 31).
	FlipHorizontal uint32 = 0x80000000
	// FlipVertical mirrors a tile top to bottom (Tiled GID bit 30).
	FlipVertical uint32 = 0x40000000
	// FlipDiagonal swaps a tile's X and Y axes before horizontal and vertical flips (Tiled GID bit 29).
	FlipDiagonal uint32 = 0x20000000
	// FlipMask masks the three orthogonal Tiled flip bits from a raw GID.
	FlipMask uint32 = FlipHorizontal | FlipVertical | FlipDiagonal
	// TileIDMask masks the global tile ID from a raw GID, stripping flip and rotation bits.
	TileIDMask uint32 = 0x0FFFFFFF
)

// Property is one typed custom property on a map, layer, tileset, tile or object.
type Property struct {
	Name  string
	Type  string
	Value any
}

// Properties is a set of named custom properties with typed getters.
type Properties map[string]Property

// Has reports whether a property named name exists.
func (p Properties) Has(name string) bool {
	_, ok := p[name]
	return ok
}

// String returns the string value of name (matching string, color and file properties).
func (p Properties) String(name string) (string, bool) {
	prop, ok := p[name]
	if !ok {
		return "", false
	}
	s, ok := prop.Value.(string)
	return s, ok
}

// File returns the file path value of a file property (or string property).
func (p Properties) File(name string) (string, bool) {
	return p.String(name)
}

// Int returns the integer value of name (matching int and object properties).
func (p Properties) Int(name string) (int, bool) {
	prop, ok := p[name]
	if !ok {
		return 0, false
	}
	switch v := prop.Value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v == math.Trunc(v) {
			return int(v), true
		}
	}
	return 0, false
}

// ObjectID returns the referenced object ID of an object property.
func (p Properties) ObjectID(name string) (int, bool) {
	return p.Int(name)
}

// Float returns the floating-point value of name (accepting float or int properties).
func (p Properties) Float(name string) (float64, bool) {
	prop, ok := p[name]
	if !ok {
		return 0, false
	}
	switch v := prop.Value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

// Bool returns the boolean value of name.
func (p Properties) Bool(name string) (bool, bool) {
	prop, ok := p[name]
	if !ok {
		return false, false
	}
	b, ok := prop.Value.(bool)
	return b, ok
}

// Color parses a Tiled color property (#RRGGBB or #AARRGGBB) into a core.Color.
func (p Properties) Color(name string) (core.Color, bool) {
	prop, ok := p[name]
	if !ok {
		return 0, false
	}
	if c, ok := prop.Value.(core.Color); ok {
		return c, true
	}
	s, ok := prop.Value.(string)
	if !ok {
		return 0, false
	}
	return parseTiledColor(s)
}

func parseTiledColor(s string) (core.Color, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	switch len(s) {
	case 6:
		v, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return 0, false
		}
		return core.RGB(uint32(v)), true
	case 8:
		v, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return 0, false
		}
		// Tiled encodes 8-digit hex colors as #AARRGGBB; core.Color packs 0xRRGGBBAA.
		a := uint32(v >> 24)
		rgb := uint32(v & 0x00ffffff)
		if a == 0 && rgb == 0 {
			return core.Transparent, true
		}
		return core.Color(rgb<<8 | a), true
	default:
		return 0, false
	}
}

// TileAnimFrame is one frame of an animated tile: its local tile ID in the tileset and how long it shows.
type TileAnimFrame struct {
	TileID   uint32
	Duration time.Duration
}

// TileAnimation advances one animated tile through its frames in a loop on Tilemap.Update.
type TileAnimation struct {
	Frames []TileAnimFrame

	index   int
	elapsed time.Duration
}

// Update moves the animation forward by dt, wrapping cleanly across full cycles.
func (a *TileAnimation) Update(dt time.Duration) {
	if dt <= 0 || len(a.Frames) == 0 {
		return
	}
	var round time.Duration
	for _, f := range a.Frames {
		if f.Duration > 0 {
			round += f.Duration
		}
	}
	if round <= 0 {
		return
	}
	a.elapsed += dt % round
	for {
		dur := max(0, a.Frames[a.index].Duration)
		if a.elapsed < dur {
			break
		}
		a.elapsed -= dur
		a.index = (a.index + 1) % len(a.Frames)
	}
}

// CurrentTileID returns the local tile ID showing now, or 0 for an empty animation.
func (a *TileAnimation) CurrentTileID() uint32 {
	if len(a.Frames) == 0 {
		return 0
	}
	return a.Frames[a.index].TileID
}

// Index returns the index in Frames of the frame showing now.
func (a *TileAnimation) Index() int { return a.index }

// Reset restarts the animation from its first frame.
func (a *TileAnimation) Reset() {
	a.index, a.elapsed = 0, 0
}

// ObjectAlignment controls which point of a tile object sits at its (X, Y) position and acts as its rotation pivot.
type ObjectAlignment int

const (
	// ObjectAlignmentUnspecified uses bottom-left in orthogonal/staggered/hexagonal maps and bottom in isometric maps, matching Tiled.
	ObjectAlignmentUnspecified ObjectAlignment = iota
	ObjectAlignmentTopLeft
	ObjectAlignmentTop
	ObjectAlignmentTopRight
	ObjectAlignmentLeft
	ObjectAlignmentCenter
	ObjectAlignmentRight
	ObjectAlignmentBottomLeft
	ObjectAlignmentBottom
	ObjectAlignmentBottomRight
)

// TileImage is one tile's image and source rectangle in an image-collection tileset.
type TileImage struct {
	Image  core.ImageSource
	X      float32
	Y      float32
	Width  float32
	Height float32
}

// Tileset maps global tile IDs starting at FirstGID to rectangles of a grid image, frames of a sprite.Atlas, or per-tile images of an image-collection tileset.
type Tileset struct {
	Name     string
	FirstGID uint32
	Image    core.ImageSource
	Atlas    *sprite.Atlas
	Frames   []sprite.Frame
	// TileImages holds per-tile images for image-collection tilesets, keyed by 0-based local tile ID.
	TileImages      map[uint32]TileImage
	TileWidth       float32
	TileHeight      float32
	TileOffsetX     float32
	TileOffsetY     float32
	ObjectAlignment ObjectAlignment
	Columns         int
	TileCount       int
	Spacing         float32
	Margin          float32
	Properties      Properties
	// TileProperties holds custom properties keyed by 0-based local tile ID.
	TileProperties map[uint32]Properties
	// TileProbabilities holds per-tile random selection weights keyed by 0-based local tile ID (unset defaults to 1).
	TileProbabilities map[uint32]float64
	// Animations holds tile animations keyed by 0-based local tile ID.
	Animations map[uint32]*TileAnimation
	// TileCollisions holds each tile's collision object group as authored in Tiled's collision editor (.tmj/.tsj/.tmx/.tsx), keyed by 0-based local tile ID.
	TileCollisions map[uint32]*ObjectLayer
	// Shapes holds the collision shapes of tiles keyed by 0-based local tile ID, such as the ones Tiled's collision editor draws; an animated tile keeps the shapes of its own ID whichever frame shows.
	Shapes map[uint32][]TileShape
	// WangSets holds Tiled Wang sets (terrain sets) defined on this tileset.
	WangSets []WangSet
	// Terrains are the tileset's terrain sets, such as the ones a YAML tileset defines (TilesetDoc.Terrains), in their order there; Tilemap.Terrain finds one by name.
	Terrains []*Terrain
}

// TileProbability returns the selection probability for localID, defaulting to 1 when unset.
func (ts *Tileset) TileProbability(localID uint32) float64 {
	if ts != nil && ts.TileProbabilities != nil {
		if p, ok := ts.TileProbabilities[localID]; ok {
			return p
		}
	}
	return 1
}

// WangSet returns the first WangSet on ts with the given name, or nil if none matches.
func (ts *Tileset) WangSet(name string) *WangSet {
	if ts == nil {
		return nil
	}
	for i := range ts.WangSets {
		if ts.WangSets[i].Name == name {
			return &ts.WangSets[i]
		}
	}
	return nil
}

// WangSetAt returns the WangSet at index in ts.WangSets, or nil if index is out of range.
func (ts *Tileset) WangSetAt(index int) *WangSet {
	if ts == nil || index < 0 || index >= len(ts.WangSets) {
		return nil
	}
	return &ts.WangSets[index]
}

// NewAtlasTileset builds a Tileset from atlas whose local tile IDs (0, 1, ...) correspond to frameNames in order.
//
// FirstGID defaults to 1 when 0.
func NewAtlasTileset(firstGID uint32, atlas *sprite.Atlas, frameNames ...string) *Tileset {
	if firstGID == 0 {
		firstGID = 1
	}
	ts := &Tileset{
		FirstGID:  firstGID,
		Atlas:     atlas,
		Frames:    make([]sprite.Frame, len(frameNames)),
		TileCount: len(frameNames),
	}
	if atlas != nil {
		ts.Image = atlas.Image
		for i, name := range frameNames {
			f := atlas.Frames[name]
			ts.Frames[i] = f
			if ts.TileWidth == 0 && f.Width > 0 {
				ts.TileWidth = f.Width
			} else if ts.TileWidth == 0 && f.W > 0 {
				ts.TileWidth = f.W
			}
			if ts.TileHeight == 0 && f.Height > 0 {
				ts.TileHeight = f.Height
			} else if ts.TileHeight == 0 && f.H > 0 {
				ts.TileHeight = f.H
			}
		}
	}
	return ts
}

// NewGridTileset builds a uniform grid Tileset over img with columns tiles per row.
//
// FirstGID defaults to 1 when 0.
func NewGridTileset(firstGID uint32, img core.ImageSource, tileWidth, tileHeight float32, columns, tileCount int) *Tileset {
	if firstGID == 0 {
		firstGID = 1
	}
	return &Tileset{
		FirstGID:   firstGID,
		Image:      img,
		TileWidth:  tileWidth,
		TileHeight: tileHeight,
		Columns:    columns,
		TileCount:  tileCount,
	}
}

func imageSourceSize(src core.ImageSource) (w, h float32) {
	w, h = float32(src.Size.Width), float32(src.Size.Height)
	if (w <= 0 || h <= 0) && src.Pixels != nil {
		b := src.Pixels.Bounds()
		if w <= 0 {
			w = float32(b.Dx())
		}
		if h <= 0 {
			h = float32(b.Dy())
		}
	}
	return w, h
}

func (ts *Tileset) tileSource(localID uint32) (img core.ImageSource, sx, sy, sw, sh, ox, oy, tw, th float32, ok bool) {
	if ts.Animations != nil {
		if anim, found := ts.Animations[localID]; found && anim != nil && len(anim.Frames) > 0 {
			localID = anim.CurrentTileID()
		}
	}
	if len(ts.TileImages) > 0 {
		ti, found := ts.TileImages[localID]
		if !found {
			return core.ImageSource{}, 0, 0, 0, 0, 0, 0, 0, 0, false
		}
		w, h := ti.Width, ti.Height
		if w <= 0 || h <= 0 {
			iw, ih := imageSourceSize(ti.Image)
			if w <= 0 {
				w = iw
			}
			if h <= 0 {
				h = ih
			}
		}
		if w <= 0 || h <= 0 {
			return core.ImageSource{}, 0, 0, 0, 0, 0, 0, 0, 0, false
		}
		return ti.Image, ti.X, ti.Y, w, h, 0, 0, w, h, true
	}
	if len(ts.Frames) > 0 {
		if int(localID) >= len(ts.Frames) {
			return core.ImageSource{}, 0, 0, 0, 0, 0, 0, 0, 0, false
		}
		f := ts.Frames[localID]
		if f.W <= 0 || f.H <= 0 {
			return core.ImageSource{}, 0, 0, 0, 0, 0, 0, 0, 0, false
		}
		img = ts.Image
		if ts.Atlas != nil && img.URI == "" && len(img.Data) == 0 && img.Pixels == nil {
			img = ts.Atlas.Image
		}
		tw, th = f.Width, f.Height
		if tw <= 0 {
			tw = f.W
		}
		if th <= 0 {
			th = f.H
		}
		return img, f.X, f.Y, f.W, f.H, f.OffsetX, f.OffsetY, tw, th, true
	}
	if ts.Columns <= 0 || ts.TileWidth <= 0 || ts.TileHeight <= 0 {
		return core.ImageSource{}, 0, 0, 0, 0, 0, 0, 0, 0, false
	}
	if ts.TileCount > 0 && int(localID) >= ts.TileCount {
		return core.ImageSource{}, 0, 0, 0, 0, 0, 0, 0, 0, false
	}
	col := int(localID) % ts.Columns
	row := int(localID) / ts.Columns
	sx = ts.Margin + float32(col)*(ts.TileWidth+ts.Spacing)
	sy = ts.Margin + float32(row)*(ts.TileHeight+ts.Spacing)
	return ts.Image, sx, sy, ts.TileWidth, ts.TileHeight, 0, 0, ts.TileWidth, ts.TileHeight, true
}

// TileLayer is one row-major grid of tile GIDs drawn in map order, or a sparse chunked layer when ChunkSize > 0.
//
// Its zero value is hidden and transparent; NewTileLayer and NewChunkedTileLayer return visible layers at full opacity.
type TileLayer struct {
	Name   string
	Width  int
	Height int
	StartX int
	StartY int
	Data   []uint32
	// ChunkSize is the power-of-two edge length (in tiles) for sparse chunked layers; 0 selects a dense Data layer.
	ChunkSize int
	OffsetX   float32
	OffsetY   float32
	ParallaxX float32
	ParallaxY float32
	Opacity   float32
	TintColor core.Color
	// Tinted distinguishes an explicit TintColor (including transparent black #00000000) from an untinted layer.
	// Setting TintColor to a non-zero color also tints the layer when Tinted is false.
	Tinted  bool
	Visible bool
	// YSort enables per-tile Y-sorting for this layer. When true, visible tiles sort by
	// their bottom edge (plus any per-tile PropSortOriginY property offset) and join a
	// Y-sorted scene.Layer's draw list alongside sprites and drawables.
	YSort      bool
	Properties Properties

	chunks *chunkGrid
}

// ImageLayer is an image layer (such as a sky, parallax backdrop, or foreground overlay) drawn in map layer order.
type ImageLayer struct {
	Name       string
	Image      core.ImageSource
	Width      float32
	Height     float32
	OffsetX    float32
	OffsetY    float32
	ParallaxX  float32
	ParallaxY  float32
	RepeatX    bool
	RepeatY    bool
	Opacity    float32
	TintColor  core.Color
	Tinted     bool
	Visible    bool
	Properties Properties
}

type layerKind uint8

const (
	layerKindTile layerKind = iota
	layerKindImage
	layerKindObject
)

type layerRef struct {
	kind  layerKind
	index int
}

// NewTileLayer returns a visible TileLayer at full opacity. When data is nil it allocates a width*height slice.
func NewTileLayer(name string, width, height int, data []uint32) TileLayer {
	if data == nil && width > 0 && height > 0 {
		data = make([]uint32, width*height)
	}
	return TileLayer{
		Name:    name,
		Width:   width,
		Height:  height,
		Data:    data,
		Opacity: 1,
		Visible: true,
	}
}

// ObjectShape identifies the geometric shape of a Tiled object.
type ObjectShape int

const (
	ObjectRect ObjectShape = iota
	ObjectPoint
	ObjectEllipse
	ObjectPolygon
	ObjectPolyline
	ObjectTile
	ObjectText
)

// TextObject holds the text content and styling of a Tiled text object.
type TextObject struct {
	Text       string
	FontFamily string
	PixelSize  int
	Wrap       bool
	Color      core.Color
	Bold       bool
	Italic     bool
	Underline  bool
	Strikeout  bool
	Kerning    bool
	HAlign     string
	VAlign     string
}

// Object is one object from a Tiled objectgroup layer.
type Object struct {
	ID       int
	Name     string
	Type     string
	Shape    ObjectShape
	X, Y     float32
	Width    float32
	Height   float32
	Rotation float32
	GID      uint32
	Visible  bool
	Points   []core.Point
	// Text holds the text content and styling when Shape is ObjectText, or nil otherwise.
	Text       *TextObject
	Properties Properties
}

// TileID returns the global tile ID of a tile object with flip bits stripped, or 0 for non-tile objects.
func (o Object) TileID() uint32 { return o.GID & TileIDMask }

// FlipFlags returns the Tiled flip bits set on a tile object's GID.
func (o Object) FlipFlags() uint32 { return o.GID & FlipMask }

// WorldPoints returns the vertices of a polygon or polyline object translated by (X, Y).
func (o Object) WorldPoints() []core.Point {
	if len(o.Points) == 0 {
		return nil
	}
	out := make([]core.Point, len(o.Points))
	for i, pt := range o.Points {
		out[i] = core.Point{
			X: core.Points(o.X) + pt.X,
			Y: core.Points(o.Y) + pt.Y,
		}
	}
	return out
}

// ObjectLayer is a Tiled objectgroup layer.
type ObjectLayer struct {
	Name       string
	OffsetX    float32
	OffsetY    float32
	ParallaxX  float32
	ParallaxY  float32
	Opacity    float32
	TintColor  core.Color
	Tinted     bool
	Visible    bool
	Objects    []Object
	Properties Properties
}

type cellKey struct {
	col, row int
}

// Tilemap is a tiled world of tile layers, image layers, tilesets, object layers, custom properties and per-tile collision flags.
type Tilemap struct {
	Width      int
	Height     int
	TileWidth  float32
	TileHeight float32
	// Origin is the world-space position of the map's local (0, 0) point (typically the top-left corner of cell (0, 0)).
	// Drawing, WorldBounds, TileOrigin, TileToWorld, WorldToTile, TilesInRect, Raycast, and AppendYSortTiles all honor Origin.
	Origin        core.Point
	Orientation   Orientation
	StaggerAxis   StaggerAxis
	StaggerIndex  StaggerIndex
	HexSideLength float32
	Infinite      bool
	Layers        []TileLayer
	ImageLayers   []ImageLayer
	Tilesets      []*Tileset
	Objects       []ObjectLayer
	Properties    Properties
	// Neighbours lists adjacent or overlapping levels in a multi-level world (populated by LDtk loaders).
	Neighbours []LevelNeighbour

	tileFlags   map[uint32]TileFlags
	cellFlags   map[cellKey]TileFlags
	layerOrder  []layerRef
	caches      map[int]*LayerCache
	edits       *tileEditLog
	ysortBuf    []YSortTile
	ysortDrawer YSortDrawer
	drawnTile   []bool
	drawnImage  []bool
	drawnObject []bool
}

// New constructs an empty Tilemap of width by height tiles sized tileWidth by tileHeight points.
func New(width, height int, tileWidth, tileHeight float32, tilesets ...*Tileset) *Tilemap {
	return &Tilemap{
		Width:      width,
		Height:     height,
		TileWidth:  tileWidth,
		TileHeight: tileHeight,
		Tilesets:   tilesets,
	}
}

// WorldBounds returns the world rectangle covering the whole map for its Orientation and Origin.
// For unbounded chunked maps with non-positive Width/Height, it returns the bounding box of currently loaded chunks.
func (m *Tilemap) WorldBounds() core.Bounds {
	if (m.Width <= 0 || m.Height <= 0) && m.hasChunkedLayer() {
		minCol, minRow, maxCol, maxRow, ok := m.activeTileBounds()
		if !ok || m.TileWidth <= 0 || m.TileHeight <= 0 {
			return core.Bounds{}
		}
		if m.Orientation == OrientationOrthogonal {
			return core.Bounds{
				Origin: core.Point{
					X: m.Origin.X + core.Points(float32(minCol)*m.TileWidth),
					Y: m.Origin.Y + core.Points(float32(minRow)*m.TileHeight),
				},
				Size: core.Size{
					Width:  core.Points(float32(maxCol-minCol+1) * m.TileWidth),
					Height: core.Points(float32(maxRow-minRow+1) * m.TileHeight),
				},
			}
		}
		b := m.projectedTileRangeBounds(minCol, minRow, maxCol, maxRow)
		b.Origin.X += m.Origin.X
		b.Origin.Y += m.Origin.Y
		return b
	}
	if m.Orientation != OrientationOrthogonal {
		b := m.projectedWorldBounds(m.Width, m.Height)
		if b.Size.Width > 0 || b.Size.Height > 0 {
			b.Origin.X += m.Origin.X
			b.Origin.Y += m.Origin.Y
		}
		return b
	}
	return core.Bounds{
		Origin: m.Origin,
		Size: core.Size{
			Width:  core.Points(float32(m.Width) * m.TileWidth),
			Height: core.Points(float32(m.Height) * m.TileHeight),
		},
	}
}

// Layer returns the first tile layer with the given name, or nil if none matches.
func (m *Tilemap) Layer(name string) *TileLayer {
	for i := range m.Layers {
		if m.Layers[i].Name == name {
			return &m.Layers[i]
		}
	}
	return nil
}

// ImageLayer returns the first image layer with the given name, or nil if none matches.
func (m *Tilemap) ImageLayer(name string) *ImageLayer {
	for i := range m.ImageLayers {
		if m.ImageLayers[i].Name == name {
			return &m.ImageLayers[i]
		}
	}
	return nil
}

// ObjectLayer returns the first object layer with the given name, or nil if none matches.
func (m *Tilemap) ObjectLayer(name string) *ObjectLayer {
	for i := range m.Objects {
		if m.Objects[i].Name == name {
			return &m.Objects[i]
		}
	}
	return nil
}

func (m *Tilemap) addTileLayer(l TileLayer) {
	idx := len(m.Layers)
	m.Layers = append(m.Layers, l)
	m.layerOrder = append(m.layerOrder, layerRef{kind: layerKindTile, index: idx})
}

func (m *Tilemap) addImageLayer(l ImageLayer) {
	idx := len(m.ImageLayers)
	m.ImageLayers = append(m.ImageLayers, l)
	m.layerOrder = append(m.layerOrder, layerRef{kind: layerKindImage, index: idx})
}

func (m *Tilemap) addObjectLayer(l ObjectLayer) {
	idx := len(m.Objects)
	m.Objects = append(m.Objects, l)
	m.layerOrder = append(m.layerOrder, layerRef{kind: layerKindObject, index: idx})
}

func (m *Tilemap) nextFirstGID() (uint32, bool) {
	var next uint64 = 1
	for _, ts := range m.Tilesets {
		if ts == nil {
			continue
		}
		fg := uint64(ts.FirstGID)
		if fg == 0 {
			fg = 1
		}
		var span uint64 = 1
		if ts.TileCount > 0 && uint64(ts.TileCount) > span {
			span = uint64(ts.TileCount)
		}
		if uint64(len(ts.Frames)) > span {
			span = uint64(len(ts.Frames))
		}
		for id := range ts.TileImages {
			if uint64(id)+1 > span {
				span = uint64(id) + 1
			}
		}
		for id := range ts.TileProperties {
			if uint64(id)+1 > span {
				span = uint64(id) + 1
			}
		}
		for id := range ts.TileProbabilities {
			if uint64(id)+1 > span {
				span = uint64(id) + 1
			}
		}
		for id := range ts.Animations {
			if uint64(id)+1 > span {
				span = uint64(id) + 1
			}
		}
		for id := range ts.Shapes {
			if uint64(id)+1 > span {
				span = uint64(id) + 1
			}
		}
		if fg > uint64(TileIDMask) || span > uint64(TileIDMask) || fg+span > uint64(TileIDMask) {
			return 0, false
		}
		if end := fg + span; end > next {
			next = end
		}
	}
	if next == 0 || next > uint64(TileIDMask) {
		return 0, false
	}
	return uint32(next), true
}

// TilesetForGID finds the tileset owning gid and returns it along with the 0-based local tile ID.
func (m *Tilemap) TilesetForGID(gid uint32) (*Tileset, uint32) {
	id := gid & TileIDMask
	if id == 0 {
		return nil, 0
	}
	var best *Tileset
	var bestFirst uint32
	for _, ts := range m.Tilesets {
		if ts == nil {
			continue
		}
		fg := ts.FirstGID
		if fg == 0 {
			fg = 1
		}
		if fg <= id && fg >= bestFirst {
			best = ts
			bestFirst = fg
		}
	}
	if best == nil {
		return nil, 0
	}
	return best, id - bestFirst
}

// TileProperties returns the custom properties attached to gid in its tileset, or nil if none exist.
func (m *Tilemap) TileProperties(gid uint32) Properties {
	ts, localID := m.TilesetForGID(gid)
	if ts == nil || ts.TileProperties == nil {
		return nil
	}
	return ts.TileProperties[localID]
}

// Update advances all animated tiles in every tileset by dt.
func (m *Tilemap) Update(dt time.Duration) {
	if dt <= 0 {
		return
	}
	for _, ts := range m.Tilesets {
		if ts == nil {
			continue
		}
		for _, anim := range ts.Animations {
			if anim != nil {
				anim.Update(dt)
			}
		}
	}
}

var untinted = core.RGB(0xffffff)

func isTinted(c core.Color, tinted bool) bool { return (tinted || c != 0) && c != untinted }

func isInvisibleTint(c core.Color, tinted bool) bool { return isTinted(c, tinted) && c&0xff == 0 }

func multiplyTint(a core.Color, aTinted bool, b core.Color, bTinted bool) (core.Color, bool) {
	hasA := aTinted || a != 0
	hasB := bTinted || b != 0
	if !hasA {
		return b, hasB
	}
	if !hasB {
		return a, hasA
	}
	if a == untinted {
		return b, true
	}
	if b == untinted {
		return a, true
	}
	ar, ag, ab, aa := uint32(a>>24&0xff), uint32(a>>16&0xff), uint32(a>>8&0xff), uint32(a&0xff)
	br, bg, bb, ba := uint32(b>>24&0xff), uint32(b>>16&0xff), uint32(b>>8&0xff), uint32(b&0xff)
	return core.Color(((ar*br+127)/255)<<24 | ((ag*bg+127)/255)<<16 | ((ab*bb+127)/255)<<8 | (aa*ba+127)/255), true
}

func drawTileImage(d *core.Drawing, img core.ImageSource, tint core.Color, tinted bool, sx, sy, sw, sh, dx, dy, dw, dh float32) {
	if isTinted(tint, tinted) {
		d.DrawTintedImageFrom(img, tint, sx, sy, sw, sh, dx, dy, dw, dh)
		return
	}
	d.DrawImageFrom(img, sx, sy, sw, sh, dx, dy, dw, dh)
}

func drawPlacedTile(d *core.Drawing, img core.ImageSource, tint core.Color, tinted bool, sx, sy, sw, sh, ox, oy, dstX, dstY, tw, th float32, flags uint32) {
	if flags == 0 {
		drawTileImage(d, img, tint, tinted, sx, sy, sw, sh, dstX+ox, dstY+oy, sw, sh)
		return
	}
	a, b, c, dd, e, f := flipMatrix(flags, dstX, dstY, tw, th)
	d.Save().Transform(a, b, c, dd, e, f)
	drawTileImage(d, img, tint, tinted, sx, sy, sw, sh, ox, oy, sw, sh)
	d.Restore()
}

// Draw draws every visible tile layer, image layer, and object layer (tile objects) into d in map layer order, culling to cam's visible world region when cam is non-nil.
//
// Draw draws in world coordinates: call cam.Apply(d) before Draw (and any world-space sprites) to position the view.
func (m *Tilemap) Draw(d *core.Drawing, cam *Camera) {
	m.visitAllLayers(func(tl *TileLayer, il *ImageLayer, ol *ObjectLayer) {
		if tl != nil {
			m.DrawLayer(d, tl, cam)
		} else if il != nil {
			m.DrawImageLayer(d, il, cam)
		} else if ol != nil {
			m.DrawObjectLayer(d, ol, cam)
		}
	})
}

func (m *Tilemap) objectAlignmentOffset(align ObjectAlignment, w, h float32) (float32, float32) {
	if align == ObjectAlignmentUnspecified {
		if m.Orientation == OrientationIsometric {
			align = ObjectAlignmentBottom
		} else {
			align = ObjectAlignmentBottomLeft
		}
	}
	switch align {
	case ObjectAlignmentTopLeft:
		return 0, 0
	case ObjectAlignmentTop:
		return -w / 2, 0
	case ObjectAlignmentTopRight:
		return -w, 0
	case ObjectAlignmentLeft:
		return 0, -h / 2
	case ObjectAlignmentCenter:
		return -w / 2, -h / 2
	case ObjectAlignmentRight:
		return -w, -h / 2
	case ObjectAlignmentBottom:
		return -w / 2, -h
	case ObjectAlignmentBottomRight:
		return -w, -h
	default: // ObjectAlignmentBottomLeft
		return 0, -h
	}
}

// DrawObjectLayer draws every visible tile object in layer into d in object order, applying layer visibility, opacity, tint, offset, parallax, object size, rotation, flip flags, tileset tileoffset, and tileset objectalignment, culling to cam.VisibleBounds() when cam is non-nil.
func (m *Tilemap) DrawObjectLayer(d *core.Drawing, layer *ObjectLayer, cam *Camera) {
	if layer == nil || !layer.Visible || layer.Opacity <= 0 || isInvisibleTint(layer.TintColor, layer.Tinted) || len(layer.Objects) == 0 {
		return
	}
	offX, offY := effectiveLayerOffset(float32(m.Origin.X)+layer.OffsetX, float32(m.Origin.Y)+layer.OffsetY, layer.ParallaxX, layer.ParallaxY, cam)

	hasCam := cam != nil
	var vbMinX, vbMinY, vbMaxX, vbMaxY float64
	if hasCam {
		vb := cam.VisibleBounds()
		if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
			return
		}
		vbMinX = float64(vb.Origin.X)
		vbMinY = float64(vb.Origin.Y)
		vbMaxX = vbMinX + float64(vb.Size.Width)
		vbMaxY = vbMinY + float64(vb.Size.Height)
	}

	savedLayer := false
	for i := range layer.Objects {
		o := &layer.Objects[i]
		if o.Shape != ObjectTile || !o.Visible || o.GID == 0 {
			continue
		}
		if math.IsNaN(float64(o.X)) || math.IsInf(float64(o.X), 0) || math.IsNaN(float64(o.Y)) || math.IsInf(float64(o.Y), 0) {
			continue
		}
		ts, localID := m.TilesetForGID(o.GID)
		if ts == nil {
			continue
		}
		img, sx, sy, sw, sh, ox, oy, tw, th, ok := ts.tileSource(localID)
		if !ok {
			continue
		}
		dw, dh := o.Width, o.Height
		if math.IsNaN(float64(dw)) || math.IsInf(float64(dw), 0) || math.IsNaN(float64(dh)) || math.IsInf(float64(dh), 0) {
			continue
		}
		if dw <= 0 {
			dw = tw
		}
		if dh <= 0 {
			dh = th
		}
		if dw <= 0 || dh <= 0 {
			continue
		}

		ax, ay := m.objectAlignmentOffset(ts.ObjectAlignment, dw, dh)
		ax += ts.TileOffsetX
		ay += ts.TileOffsetY
		px, py := offX+o.X, offY+o.Y
		flags := o.GID & FlipMask
		var rotDeg float64
		if r64 := float64(o.Rotation); !math.IsNaN(r64) && !math.IsInf(r64, 0) {
			rotDeg = math.Mod(r64, 360)
		}

		var fa, fb, fc, fd, fe, ff float32
		if flags&FlipDiagonal == 0 {
			fa, fe = dw/tw, ax
			if flags&FlipHorizontal != 0 {
				fa, fe = -fa, ax+dw
			}
			fd, ff = dh/th, ay
			if flags&FlipVertical != 0 {
				fd, ff = -fd, ay+dh
			}
		} else {
			fc, fe = dw/th, ax
			if flags&FlipHorizontal != 0 {
				fc, fe = -fc, ax+dw
			}
			fb, ff = dh/tw, ay
			if flags&FlipVertical != 0 {
				fb, ff = -fb, ay+dh
			}
		}

		var cos, sin float32 = 1, 0
		if rotDeg != 0 {
			s64, c64 := math.Sincos(float64(rotDeg*math.Pi) / 180)
			sin, cos = float32(s64), float32(c64)
		}
		// Explicit float32(...) conversions prevent arm64 FMA contraction so transforms are bit-identical across architectures.
		ma := float32(cos*fa) - float32(sin*fb)
		mb := float32(sin*fa) + float32(cos*fb)
		mc := float32(cos*fc) - float32(sin*fd)
		md := float32(sin*fc) + float32(cos*fd)
		me := float32(float32(cos*fe)-float32(sin*ff)) + px
		mf := float32(float32(sin*fe)+float32(cos*ff)) + py

		if hasCam {
			ox1, oy1 := ox+sw, oy+sh
			x0 := float64(float32(float32(ma*ox)+float32(mc*oy)) + me)
			y0 := float64(float32(float32(mb*ox)+float32(md*oy)) + mf)
			x1 := float64(float32(float32(ma*ox1)+float32(mc*oy)) + me)
			y1 := float64(float32(float32(mb*ox1)+float32(md*oy)) + mf)
			x2 := float64(float32(float32(ma*ox)+float32(mc*oy1)) + me)
			y2 := float64(float32(float32(mb*ox)+float32(md*oy1)) + mf)
			x3 := float64(float32(float32(ma*ox1)+float32(mc*oy1)) + me)
			y3 := float64(float32(float32(mb*ox1)+float32(md*oy1)) + mf)
			minX := min(min(x0, x1), min(x2, x3))
			maxX := max(max(x0, x1), max(x2, x3))
			minY := min(min(y0, y1), min(y2, y3))
			maxY := max(max(y0, y1), max(y2, y3))
			if maxX <= vbMinX || minX >= vbMaxX || maxY <= vbMinY || minY >= vbMaxY {
				continue
			}
		}

		if layer.Opacity < 1 && !savedLayer {
			d.Save().SetAlpha(layer.Opacity)
			savedLayer = true
		}
		if flags == 0 && rotDeg == 0 {
			scaleX, scaleY := dw/tw, dh/th
			drawTileImage(d, img, layer.TintColor, layer.Tinted, sx, sy, sw, sh, float32(px+ax)+float32(ox*scaleX), float32(py+ay)+float32(oy*scaleY), float32(sw*scaleX), float32(sh*scaleY))
		} else {
			d.Save().Transform(ma, mb, mc, md, me, mf)
			drawTileImage(d, img, layer.TintColor, layer.Tinted, sx, sy, sw, sh, ox, oy, sw, sh)
			d.Restore()
		}
	}
	if savedLayer {
		d.Restore()
	}
}

func effectiveLayerOffset(ox, oy, px, py float32, cam *Camera) (float32, float32) {
	if cam == nil {
		return ox, oy
	}
	cx := cam.X + cam.shakeX
	cy := cam.Y + cam.shakeY
	if px != 0 && px != 1 {
		ox += float32(cx * (1 - px))
	}
	if py != 0 && py != 1 {
		oy += float32(cy * (1 - py))
	}
	return ox, oy
}

// DrawImageLayer draws one image layer into d, applying its visibility, opacity, tint, offset, parallax factor, and repeat flags.
func (m *Tilemap) DrawImageLayer(d *core.Drawing, layer *ImageLayer, cam *Camera) {
	if layer == nil || !layer.Visible || layer.Opacity <= 0 || isInvisibleTint(layer.TintColor, layer.Tinted) {
		return
	}
	if layer.Image.URI == "" && len(layer.Image.Data) == 0 && layer.Image.Pixels == nil {
		return
	}
	ox, oy := effectiveLayerOffset(float32(m.Origin.X)+layer.OffsetX, float32(m.Origin.Y)+layer.OffsetY, layer.ParallaxX, layer.ParallaxY, cam)

	w, h := layer.Width, layer.Height
	if w <= 0 || h <= 0 {
		iw, ih := imageSourceSize(layer.Image)
		if w <= 0 {
			w = iw
		}
		if h <= 0 {
			h = ih
		}
	}

	tinted := isTinted(layer.TintColor, layer.Tinted)
	drawOne := func(x, y float32) {
		if w > 0 && h > 0 {
			if tinted {
				d.DrawTintedImageIn(layer.Image, layer.TintColor, x, y, w, h)
			} else {
				d.DrawImageIn(layer.Image, x, y, w, h)
			}
		} else if tinted {
			d.DrawTintedImage(layer.Image, layer.TintColor, x, y)
		} else {
			d.DrawImage(layer.Image, x, y)
		}
	}

	saved := false
	if layer.Opacity < 1 {
		d.Save().SetAlpha(layer.Opacity)
		saved = true
	}

	if (layer.RepeatX && w > 0) || (layer.RepeatY && h > 0) {
		var bounds core.Bounds
		if cam != nil {
			bounds = cam.VisibleBounds()
		} else {
			bounds = m.WorldBounds()
		}
		minCol, maxCol := 0, 0
		if layer.RepeatX && w > 0 && bounds.Size.Width > 0 {
			minX := float64(bounds.Origin.X)
			maxX := minX + float64(bounds.Size.Width)
			minCol = int(math.Floor((minX - float64(ox)) / float64(w)))
			maxCol = int(math.Ceil((maxX-float64(ox))/float64(w))) - 1
		}
		minRow, maxRow := 0, 0
		if layer.RepeatY && h > 0 && bounds.Size.Height > 0 {
			minY := float64(bounds.Origin.Y)
			maxY := minY + float64(bounds.Size.Height)
			minRow = int(math.Floor((minY - float64(oy)) / float64(h)))
			maxRow = int(math.Ceil((maxY-float64(oy))/float64(h))) - 1
		}
		for row := minRow; row <= maxRow; row++ {
			for col := minCol; col <= maxCol; col++ {
				drawOne(ox+float32(col)*w, oy+float32(row)*h)
			}
		}
	} else {
		drawOne(ox, oy)
	}

	if saved {
		d.Restore()
	}
}

func (m *Tilemap) maxTilePadding() (extraLeft, extraRight, extraTop, extraBottom float64) {
	consider := func(tw, th, ox, oy float32) {
		if el := float64(-ox); el > extraLeft {
			extraLeft = el
		}
		if er := float64(tw + ox - m.TileWidth); er > extraRight {
			extraRight = er
		}
		if et := float64(th - m.TileHeight - oy); et > extraTop {
			extraTop = et
		}
		if eb := float64(oy); eb > extraBottom {
			extraBottom = eb
		}
	}
	for _, ts := range m.Tilesets {
		if ts == nil {
			continue
		}
		consider(ts.TileWidth, ts.TileHeight, ts.TileOffsetX, ts.TileOffsetY)
		for i := range ts.Frames {
			f := ts.Frames[i]
			fw, fh := f.Width, f.Height
			if fw <= 0 {
				fw = f.W
			}
			if fh <= 0 {
				fh = f.H
			}
			consider(fw, fh, ts.TileOffsetX, ts.TileOffsetY)
		}
		for _, ti := range ts.TileImages {
			fw, fh := ti.Width, ti.Height
			if fw <= 0 || fh <= 0 {
				iw, ih := imageSourceSize(ti.Image)
				if fw <= 0 {
					fw = iw
				}
				if fh <= 0 {
					fh = ih
				}
			}
			consider(fw, fh, ts.TileOffsetX, ts.TileOffsetY)
		}
	}
	return extraLeft, extraRight, extraTop, extraBottom
}

// DrawLayer draws one tile layer into d, visiting only the visible tiles overlapping cam.VisibleBounds() when cam is non-nil.
//
// When layer.YSort is true, visible tiles are sorted in stable ascending SortY order (respecting per-tile PropSortOriginY offsets) and drawn individually without using any static layer cache.
// Otherwise, when the layer has a clean static cache created via CacheStaticLayer, DrawLayer draws it in a single DrawImageFrom operation.
func (m *Tilemap) DrawLayer(d *core.Drawing, layer *TileLayer, cam *Camera) {
	if layer == nil || !layer.Visible || layer.Opacity <= 0 || isInvisibleTint(layer.TintColor, layer.Tinted) {
		return
	}
	if layer.YSort {
		m.ysortBuf = m.AppendYSortTiles(m.ysortBuf[:0], layer, cam)
		SortYSortTiles(m.ysortBuf)
		for i := range m.ysortBuf {
			m.ysortDrawer.Push(d, &m.ysortBuf[i])
		}
		m.ysortDrawer.Flush(d)
		clear(m.ysortBuf)
		return
	}
	if m.TileWidth <= 0 || m.TileHeight <= 0 {
		return
	}
	if layer.IsChunked() {
		m.drawChunkedLayer(d, layer, cam)
		return
	}
	lw, lh := m.layerSize(layer)
	if lw <= 0 || lh <= 0 || len(layer.Data) == 0 {
		return
	}

	offX, offY := effectiveLayerOffset(float32(m.Origin.X)+layer.OffsetX, float32(m.Origin.Y)+layer.OffsetY, layer.ParallaxX, layer.ParallaxY, cam)

	if cache := m.layerCacheFor(layer); cache != nil && !cache.Dirty && cache.Pixels != nil {
		m.drawCachedLayer(d, layer, cache, cam, offX, offY)
		return
	}

	if m.Orientation != OrientationOrthogonal {
		m.drawProjectedLayer(d, layer, cam, offX, offY)
		return
	}

	minCol, maxCol := 0, lw-1
	minRow, maxRow := 0, lh-1
	if cam != nil {
		vb := cam.VisibleBounds()
		if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
			return
		}
		extraL, extraR, extraT, extraB := m.maxTilePadding()
		tw, th := float64(m.TileWidth), float64(m.TileHeight)
		minX := float64(vb.Origin.X) - float64(offX)
		maxX := minX + float64(vb.Size.Width)
		minY := float64(vb.Origin.Y) - float64(offY)
		maxY := minY + float64(vb.Size.Height)

		minCol = max(0, int(math.Floor((minX-extraR)/tw)))
		maxCol = min(lw-1, int(math.Ceil((maxX+extraL)/tw))-1)
		minRow = max(0, int(math.Floor((minY-extraB)/th)))
		maxRow = min(lh-1, int(math.Ceil((maxY+extraT)/th))-1)
		if minCol > maxCol || minRow > maxRow {
			return
		}
	}

	savedLayer := false
	for row := minRow; row <= maxRow; row++ {
		rowStart := row * lw
		for col := minCol; col <= maxCol; col++ {
			idx := rowStart + col
			if idx >= len(layer.Data) {
				continue
			}
			gid := layer.Data[idx]
			if gid == 0 {
				continue
			}
			ts, localID := m.TilesetForGID(gid)
			if ts == nil {
				continue
			}
			img, sx, sy, sw, sh, ox, oy, tw, th, ok := ts.tileSource(localID)
			if !ok {
				continue
			}
			if layer.Opacity < 1 && !savedLayer {
				d.Save().SetAlpha(layer.Opacity)
				savedLayer = true
			}
			dstX := offX + float32(col)*m.TileWidth + ts.TileOffsetX
			dstY := offY + float32(row+1)*m.TileHeight - th + ts.TileOffsetY
			drawPlacedTile(d, img, layer.TintColor, layer.Tinted, sx, sy, sw, sh, ox, oy, dstX, dstY, tw, th, gid&FlipMask)
		}
	}
	if savedLayer {
		d.Restore()
	}
}

func (m *Tilemap) drawChunkedLayer(d *core.Drawing, layer *TileLayer, cam *Camera) {
	if layer.chunks == nil || len(layer.chunks.chunks) == 0 {
		return
	}
	offX, offY := effectiveLayerOffset(float32(m.Origin.X)+layer.OffsetX, float32(m.Origin.Y)+layer.OffsetY, layer.ParallaxX, layer.ParallaxY, cam)
	if m.Orientation != OrientationOrthogonal {
		m.drawProjectedLayer(d, layer, cam, offX, offY)
		return
	}

	g := layer.chunks
	size := g.size
	shift := g.shift
	savedLayer := false

	drawChunkLive := func(ch *Chunk, minCol, maxCol, minRow, maxRow int) {
		if ch == nil || ch.nonEmpty == 0 {
			return
		}
		baseCol := ch.Coord.X << shift
		baseRow := ch.Coord.Y << shift
		c0 := max(0, minCol-baseCol)
		c1 := min(size-1, maxCol-baseCol)
		r0 := max(0, minRow-baseRow)
		r1 := min(size-1, maxRow-baseRow)
		if c0 > c1 || r0 > r1 {
			return
		}
		for lr := r0; lr <= r1; lr++ {
			rowOff := lr * size
			row := baseRow + lr
			for lc := c0; lc <= c1; lc++ {
				idx := rowOff + lc
				if idx >= len(ch.Tiles) {
					continue
				}
				gid := ch.Tiles[idx]
				if gid == 0 {
					continue
				}
				ts, localID := m.TilesetForGID(gid)
				if ts == nil {
					continue
				}
				img, sx, sy, sw, sh, ox, oy, tw, th, ok := ts.tileSource(localID)
				if !ok {
					continue
				}
				if layer.Opacity < 1 && !savedLayer {
					d.Save().SetAlpha(layer.Opacity)
					savedLayer = true
				}
				col := baseCol + lc
				dstX := offX + float32(col)*m.TileWidth + ts.TileOffsetX
				dstY := offY + float32(row+1)*m.TileHeight - th + ts.TileOffsetY
				drawPlacedTile(d, img, layer.TintColor, layer.Tinted, sx, sy, sw, sh, ox, oy, dstX, dstY, tw, th, gid&FlipMask)
			}
		}
	}

	if cam != nil {
		vb := cam.VisibleBounds()
		if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
			return
		}
		extraL, extraR, extraT, extraB := m.maxTilePadding()
		tw, th := float64(m.TileWidth), float64(m.TileHeight)
		minX := float64(vb.Origin.X) - float64(offX)
		maxX := minX + float64(vb.Size.Width)
		minY := float64(vb.Origin.Y) - float64(offY)
		maxY := minY + float64(vb.Size.Height)

		minCol := int(math.Floor((minX - extraR) / tw))
		maxCol := int(math.Ceil((maxX+extraL)/tw)) - 1
		minRow := int(math.Floor((minY - extraB) / th))
		maxRow := int(math.Ceil((maxY+extraT)/th)) - 1
		if minCol > maxCol || minRow > maxRow {
			return
		}

		minCX := minCol >> shift
		maxCX := maxCol >> shift
		minCY := minRow >> shift
		maxCY := maxRow >> shift
		for cy := minCY; cy <= maxCY; cy++ {
			for cx := minCX; cx <= maxCX; cx++ {
				ch := g.chunks[ChunkCoord{X: cx, Y: cy}]
				if ch == nil || ch.nonEmpty == 0 {
					continue
				}
				if ch.cache != nil && !ch.Dirty && !ch.cache.Dirty && ch.cache.Pixels != nil {
					if savedLayer {
						d.Restore()
						savedLayer = false
					}
					m.drawCachedLayer(d, layer, ch.cache, cam, offX, offY)
					continue
				}
				drawChunkLive(ch, minCol, maxCol, minRow, maxRow)
			}
		}
		if savedLayer {
			d.Restore()
		}
		return
	}

	coords := layer.ChunkCoords()
	for _, coord := range coords {
		ch := g.chunks[coord]
		if ch == nil || ch.nonEmpty == 0 {
			continue
		}
		if ch.cache != nil && !ch.Dirty && !ch.cache.Dirty && ch.cache.Pixels != nil {
			if savedLayer {
				d.Restore()
				savedLayer = false
			}
			m.drawCachedLayer(d, layer, ch.cache, nil, offX, offY)
			continue
		}
		baseCol := coord.X << shift
		baseRow := coord.Y << shift
		drawChunkLive(ch, baseCol, baseCol+size-1, baseRow, baseRow+size-1)
	}
	if savedLayer {
		d.Restore()
	}
}

func (m *Tilemap) layerSize(l *TileLayer) (int, int) {
	w, h := l.Width, l.Height
	if w <= 0 {
		w = m.Width
	}
	if h <= 0 {
		h = m.Height
	}
	return w, h
}

// flipMatrix returns the 2D affine matrix (a, b, c, d, e, f) mapping [0, tw] x [0, th] into the tile's box at (dx, dy) under Tiled's orthogonal flip flags.
func flipMatrix(flags uint32, dx, dy, tw, th float32) (a, b, c, dd, e, f float32) {
	a, b, c, dd = 1, 0, 0, 1
	if flags&FlipDiagonal != 0 {
		a, b, c, dd = 0, 1, 1, 0
	}
	if flags&FlipHorizontal != 0 {
		a, c = -a, -c
	}
	if flags&FlipVertical != 0 {
		b, dd = -b, -dd
	}
	cx, cy := dx+tw/2, dy+th/2
	e = cx - a*tw/2 - c*th/2
	f = cy - b*tw/2 - dd*th/2
	return
}
