package tilemap

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/sprite"
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

// Tileset maps global tile IDs starting at FirstGID to rectangles of a grid image or frames of a sprite.Atlas.
type Tileset struct {
	Name       string
	FirstGID   uint32
	Image      core.ImageSource
	Atlas      *sprite.Atlas
	Frames     []sprite.Frame
	TileWidth  float32
	TileHeight float32
	Columns    int
	TileCount  int
	Spacing    float32
	Margin     float32
	Properties Properties
	// TileProperties holds custom properties keyed by 0-based local tile ID.
	TileProperties map[uint32]Properties
	// Animations holds tile animations keyed by 0-based local tile ID.
	Animations map[uint32]*TileAnimation
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

func (ts *Tileset) tileSource(localID uint32) (img core.ImageSource, sx, sy, sw, sh, ox, oy, tw, th float32, ok bool) {
	if ts.Animations != nil {
		if anim, found := ts.Animations[localID]; found && anim != nil && len(anim.Frames) > 0 {
			localID = anim.CurrentTileID()
		}
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

// TileLayer is one row-major grid of tile GIDs drawn in map order.
//
// Its zero value is hidden and transparent; NewTileLayer returns a visible layer at full opacity.
type TileLayer struct {
	Name       string
	Width      int
	Height     int
	Data       []uint32
	OffsetX    float32
	OffsetY    float32
	Opacity    float32
	Visible    bool
	Properties Properties
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
)

// Object is one object from a Tiled objectgroup layer.
type Object struct {
	ID         int
	Name       string
	Type       string
	Shape      ObjectShape
	X, Y       float32
	Width      float32
	Height     float32
	Rotation   float32
	GID        uint32
	Visible    bool
	Points     []core.Point
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
	Opacity    float32
	Visible    bool
	Objects    []Object
	Properties Properties
}

type cellKey struct {
	col, row int
}

// Tilemap is an orthogonal grid of tiles, tilesets, object layers, custom properties and per-tile collision flags.
type Tilemap struct {
	Width      int
	Height     int
	TileWidth  float32
	TileHeight float32
	Layers     []TileLayer
	Tilesets   []*Tileset
	Objects    []ObjectLayer
	Properties Properties

	tileFlags map[uint32]TileFlags
	cellFlags map[cellKey]TileFlags
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

// WorldBounds returns the world rectangle covering the whole map, from (0, 0) to (Width*TileWidth, Height*TileHeight).
func (m *Tilemap) WorldBounds() core.Bounds {
	return core.Bounds{
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

// ObjectLayer returns the first object layer with the given name, or nil if none matches.
func (m *Tilemap) ObjectLayer(name string) *ObjectLayer {
	for i := range m.Objects {
		if m.Objects[i].Name == name {
			return &m.Objects[i]
		}
	}
	return nil
}

// TileAt returns the raw GID (including flip flags) at (col, row) in layer index, or 0 if out of bounds.
func (m *Tilemap) TileAt(layer, col, row int) uint32 {
	if layer < 0 || layer >= len(m.Layers) {
		return 0
	}
	l := &m.Layers[layer]
	lw, lh := m.layerSize(l)
	if col < 0 || row < 0 || col >= lw || row >= lh {
		return 0
	}
	idx := row*lw + col
	if idx >= len(l.Data) {
		return 0
	}
	return l.Data[idx]
}

// SetTile writes a raw GID at (col, row) in layer index; out-of-bounds calls are ignored.
func (m *Tilemap) SetTile(layer, col, row int, gid uint32) {
	if layer < 0 || layer >= len(m.Layers) {
		return
	}
	l := &m.Layers[layer]
	lw, lh := m.layerSize(l)
	if col < 0 || row < 0 || col >= lw || row >= lh {
		return
	}
	idx := row*lw + col
	if idx >= len(l.Data) {
		return
	}
	l.Data[idx] = gid
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

// Draw draws every visible tile layer into d in order, culling to cam's visible world region when cam is non-nil.
//
// Draw draws in world coordinates: call cam.Apply(d) before Draw (and any world-space sprites) to position the view.
func (m *Tilemap) Draw(d *core.Drawing, cam *Camera) {
	for i := range m.Layers {
		m.DrawLayer(d, &m.Layers[i], cam)
	}
}

// DrawLayer draws one tile layer into d, visiting only the tile columns and rows overlapping cam.VisibleBounds() when cam is non-nil.
func (m *Tilemap) DrawLayer(d *core.Drawing, layer *TileLayer, cam *Camera) {
	if layer == nil || !layer.Visible || layer.Opacity <= 0 {
		return
	}
	lw, lh := m.layerSize(layer)
	if lw <= 0 || lh <= 0 || m.TileWidth <= 0 || m.TileHeight <= 0 || len(layer.Data) == 0 {
		return
	}

	minCol, maxCol := 0, lw-1
	minRow, maxRow := 0, lh-1
	if cam != nil {
		vb := cam.VisibleBounds()
		if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
			return
		}
		extraW, extraH := float64(0), float64(0)
		for _, ts := range m.Tilesets {
			if ts == nil {
				continue
			}
			if ew := float64(ts.TileWidth - m.TileWidth); ew > extraW {
				extraW = ew
			}
			if eh := float64(ts.TileHeight - m.TileHeight); eh > extraH {
				extraH = eh
			}
		}
		tw, th := float64(m.TileWidth), float64(m.TileHeight)
		minX := float64(vb.Origin.X) - float64(layer.OffsetX)
		maxX := minX + float64(vb.Size.Width)
		minY := float64(vb.Origin.Y) - float64(layer.OffsetY)
		maxY := minY + float64(vb.Size.Height)

		minCol = max(0, int(math.Floor((minX-extraW)/tw)))
		maxCol = min(lw-1, int(math.Ceil(maxX/tw))-1)
		minRow = max(0, int(math.Floor(minY/th)))
		maxRow = min(lh-1, int(math.Ceil((maxY+extraH)/th))-1)
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
			dstX := layer.OffsetX + float32(col)*m.TileWidth
			dstY := layer.OffsetY + float32(row+1)*m.TileHeight - th
			flags := gid & FlipMask
			if flags == 0 {
				d.DrawImageFrom(img, sx, sy, sw, sh, dstX+ox, dstY+oy, sw, sh)
			} else {
				a, b, c, dd, e, f := flipMatrix(flags, dstX, dstY, tw, th)
				d.Save().Transform(a, b, c, dd, e, f).DrawImageFrom(img, sx, sy, sw, sh, ox, oy, sw, sh).Restore()
			}
		}
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
