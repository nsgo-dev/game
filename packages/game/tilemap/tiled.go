package tilemap

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

// TilesetLoader resolves external tileset files and tileset images referenced by a Tiled map.
//
// Either field may be nil: LoadJSON is only needed when the map references an external .tsj tileset, and LoadImage defaults to core.ImageSource{URI: imagePath}.
type TilesetLoader struct {
	// LoadJSON reads the raw JSON of an external tileset file given its source path in the map.
	LoadJSON func(source string) ([]byte, error)
	// LoadImage returns the ImageSource for a tileset's image path (resolved relative to the external tileset's directory when loaded from a .tsj).
	LoadImage func(imagePath string) (core.ImageSource, error)
}

// LoadTiled parses a Tiled JSON map (.tmj) into a Tilemap, loading any external .tsj tilesets and resolving tileset and image-layer images through resolve.
//
// Orthogonal, isometric, staggered, and hexagonal maps, infinite chunked maps, image layers, nested group layers, embedded and external tilesets, GID flip flags, object layers, custom properties and tile animations are supported.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it. An error in an external tileset names that tileset's source; for one in the map itself, a caller that read data from a file names it with loaderr.WithFile.
func LoadTiled(data []byte, resolve TilesetLoader) (*Tilemap, error) {
	m, err := loadTiled(data, resolve)
	return m, loaderr.Locate(err, "", data, fixTiled)
}

// fixTiled is the fix for a Tiled map error that has no more specific one.
const fixTiled = "correct the map in Tiled and export it again as JSON (.tmj)"

func loadTiled(data []byte, resolve TilesetLoader) (*Tilemap, error) {
	var raw tiledMap
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	orientation, err := parseTiledOrientation(raw.Orientation)
	if err != nil {
		return nil, &loaderr.Error{Field: "orientation", Problem: err.Error(), Expected: `"orthogonal", "isometric", "staggered" or "hexagonal"`, Fix: "set the map's orientation to one of those in Tiled's map properties", Err: err}
	}
	staggerAxis, err := parseTiledStaggerAxis(raw.StaggerAxis)
	if err != nil {
		return nil, &loaderr.Error{Field: "staggeraxis", Problem: err.Error(), Expected: `"x" or "y"`, Fix: "set the map's stagger axis to x or y in Tiled", Err: err}
	}
	staggerIndex, err := parseTiledStaggerIndex(raw.StaggerIndex)
	if err != nil {
		return nil, &loaderr.Error{Field: "staggerindex", Problem: err.Error(), Expected: `"odd" or "even"`, Fix: "set the map's stagger index to odd or even in Tiled", Err: err}
	}
	if raw.Width < 0 || raw.Height < 0 {
		field := "width"
		if raw.Width >= 0 {
			field = "height"
		}
		return nil, loaderr.Errorf(field, "give the map a width and height of 0 or more tiles", "map size %dx%d must be non-negative", raw.Width, raw.Height)
	}
	if raw.Width > 0 && raw.Height > 0 && raw.Width > math.MaxInt/raw.Height {
		return nil, loaderr.Errorf("width", "make the map smaller", "map size %dx%d overflows", raw.Width, raw.Height)
	}
	if raw.TileWidth <= 0 || raw.TileHeight <= 0 {
		field := "tilewidth"
		if raw.TileWidth > 0 {
			field = "tileheight"
		}
		return nil, loaderr.Errorf(field, "give the map a tile size above 0 in Tiled's map properties", "tile size %gx%g must be positive", raw.TileWidth, raw.TileHeight)
	}

	props, err := parseProperties(raw.Properties)
	if err != nil {
		return nil, nest(err, "properties")
	}

	m := &Tilemap{
		Width:         raw.Width,
		Height:        raw.Height,
		TileWidth:     raw.TileWidth,
		TileHeight:    raw.TileHeight,
		Orientation:   orientation,
		StaggerAxis:   staggerAxis,
		StaggerIndex:  staggerIndex,
		HexSideLength: raw.HexSideLength,
		Infinite:      raw.Infinite,
		Properties:    props,
	}

	for i, ref := range raw.Tilesets {
		if ref.FirstGID == 0 {
			return nil, loaderr.Errorf(loaderr.Path("tilesets", i, "firstgid"), "export the map again from Tiled; a tileset's firstgid is 1 or more", "tileset %d has firstgid 0", i)
		}
		var tsRaw tiledTileset
		if ref.Source != "" {
			if resolve.LoadJSON == nil {
				return nil, loaderr.Errorf(loaderr.Path("tilesets", i, "source"), "set TilesetLoader.LoadJSON, or embed the tileset in the map in Tiled", "external tileset %q requires TilesetLoader.LoadJSON", ref.Source)
			}
			ext, err := resolve.LoadJSON(ref.Source)
			if err != nil {
				return nil, loaderr.Errorf(loaderr.Path("tilesets", i, "source"), "check that the tileset file exists next to the map at this path", "load tileset %q: %w", ref.Source, err)
			}
			if err := json.Unmarshal(ext, &tsRaw); err != nil {
				return nil, loaderr.Locate(err, ref.Source, ext, "export the tileset again from Tiled as JSON (.tsj)")
			}
			ts, err := buildTileset(m, ref.FirstGID, tsRaw, path.Dir(ref.Source), resolve)
			if err != nil {
				// The error is about the tileset's own file.
				return nil, loaderr.Locate(err, ref.Source, ext, "correct the tileset in Tiled and export it again as JSON (.tsj)")
			}
			m.Tilesets = append(m.Tilesets, ts)
			continue
		}
		tsRaw = ref.tiledTileset

		ts, err := buildTileset(m, ref.FirstGID, tsRaw, "", resolve)
		if err != nil {
			return nil, nest(err, loaderr.Path("tilesets", i))
		}
		m.Tilesets = append(m.Tilesets, ts)
	}
	slices.SortFunc(m.Tilesets, func(a, b *Tileset) int {
		return cmp.Compare(a.FirstGID, b.FirstGID)
	})

	if err := loadTiledLayers(m, raw.Layers, 0, 0, 1, 1, 1, true, resolve); err != nil {
		return nil, nest(err, "layers")
	}
	return m, nil
}

func parseTiledOrientation(s string) (Orientation, error) {
	switch s {
	case "", "orthogonal":
		return OrientationOrthogonal, nil
	case "isometric":
		return OrientationIsometric, nil
	case "staggered":
		return OrientationStaggered, nil
	case "hexagonal":
		return OrientationHexagonal, nil
	default:
		return 0, fmt.Errorf("tilemap: tiled: orientation %q is not supported", s)
	}
}

func parseTiledStaggerAxis(s string) (StaggerAxis, error) {
	switch s {
	case "", "y":
		return StaggerY, nil
	case "x":
		return StaggerX, nil
	default:
		return 0, fmt.Errorf("tilemap: tiled: staggeraxis %q is not supported", s)
	}
}

func parseTiledStaggerIndex(s string) (StaggerIndex, error) {
	switch s {
	case "", "odd":
		return StaggerOdd, nil
	case "even":
		return StaggerEven, nil
	default:
		return 0, fmt.Errorf("tilemap: tiled: staggerindex %q is not supported", s)
	}
}

// buildTileset builds the tileset raw. The fields its errors name are relative to the tileset.
func buildTileset(m *Tilemap, firstGID uint32, raw tiledTileset, baseDir string, resolve TilesetLoader) (*Tileset, error) {
	tw, th := raw.TileWidth, raw.TileHeight
	if tw <= 0 {
		tw = m.TileWidth
	}
	if th <= 0 {
		th = m.TileHeight
	}
	cols := raw.Columns
	if cols <= 0 && raw.ImageWidth > 0 && tw > 0 {
		avail := raw.ImageWidth - 2*raw.Margin + raw.Spacing
		step := tw + raw.Spacing
		if avail > 0 && step > 0 {
			cols = int(avail / step)
		}
	}

	var img core.ImageSource
	if raw.Image != "" {
		imgPath := raw.Image
		if baseDir != "" && baseDir != "." && !path.IsAbs(imgPath) && !strings.Contains(imgPath, ":") {
			imgPath = path.Join(baseDir, imgPath)
		}
		if resolve.LoadImage != nil {
			var err error
			img, err = resolve.LoadImage(imgPath)
			if err != nil {
				return nil, loaderr.Errorf("image", "check that the tileset image exists at this path, relative to the tileset", "load image %q: %w", imgPath, err)
			}
		} else {
			img = core.ImageSource{URI: imgPath}
		}
	}

	props, err := parseProperties(raw.Properties)
	if err != nil {
		return nil, nest(err, "properties")
	}

	ts := &Tileset{
		Name:       raw.Name,
		FirstGID:   firstGID,
		Image:      img,
		TileWidth:  tw,
		TileHeight: th,
		Columns:    cols,
		TileCount:  raw.TileCount,
		Spacing:    raw.Spacing,
		Margin:     raw.Margin,
		Properties: props,
	}

	for ti, tile := range raw.Tiles {
		if len(tile.Properties) > 0 {
			tp, err := parseProperties(tile.Properties)
			if err != nil {
				return nil, nest(err, loaderr.Path("tiles", ti, "properties"))
			}
			if ts.TileProperties == nil {
				ts.TileProperties = make(map[uint32]Properties)
			}
			ts.TileProperties[tile.ID] = tp
			if flags := flagsFromProperties(tp); flags != 0 {
				m.SetFlags(firstGID+tile.ID, flags)
			}
		}
		if len(tile.Animation) > 0 {
			frames := make([]TileAnimFrame, len(tile.Animation))
			for j, f := range tile.Animation {
				if f.Duration < 0 || int64(f.Duration) > math.MaxInt64/int64(time.Millisecond) {
					return nil, loaderr.Errorf(loaderr.Path("tiles", ti, "animation", j, "duration"), "give the animation frame a duration of 0 or more milliseconds in Tiled", "tile %d animation frame %d has invalid duration %d", tile.ID, j, f.Duration)
				}
				frames[j] = TileAnimFrame{
					TileID:   f.TileID,
					Duration: time.Duration(f.Duration) * time.Millisecond,
				}
			}
			if ts.Animations == nil {
				ts.Animations = make(map[uint32]*TileAnimation)
			}
			ts.Animations[tile.ID] = &TileAnimation{Frames: frames}
		}
	}
	return ts, nil
}

func flagsFromProperties(p Properties) TileFlags {
	var flags TileFlags
	if solid, ok := p.Bool("solid"); ok && solid {
		flags |= TileSolid
	}
	for _, key := range []string{"oneWay", "one-way", "oneway", "one_way"} {
		if ow, ok := p.Bool(key); ok && ow {
			flags |= TileOneWay
			break
		}
	}
	if custom, ok := p.Int("flags"); ok && custom > 0 {
		flags |= TileFlags(custom)
	}
	return flags
}

// loadTiledLayers adds layers to m. The fields its errors name are relative to the array of layers, as in "[2].data".
func loadTiledLayers(m *Tilemap, layers []tiledLayer, parentOX, parentOY, parentPX, parentPY, parentOpacity float32, parentVisible bool, resolve TilesetLoader) error {
	for li, raw := range layers {
		visible := parentVisible
		if raw.Visible != nil {
			visible = visible && *raw.Visible
		}
		opacity := parentOpacity
		if raw.Opacity != nil {
			opacity *= *raw.Opacity
		}
		ox := parentOX + raw.OffsetX
		oy := parentOY + raw.OffsetY
		px := parentPX
		if raw.ParallaxX != nil {
			px *= *raw.ParallaxX
		}
		py := parentPY
		if raw.ParallaxY != nil {
			py *= *raw.ParallaxY
		}

		props, err := parseProperties(raw.Properties)
		if err != nil {
			return nest(err, loaderr.Path(li, "properties"))
		}

		const fixEncoding = `in Tiled's map properties, set "Tile Layer Format" to CSV, and export again`
		switch raw.Type {
		case "tilelayer":
			if raw.Encoding != "" {
				return loaderr.Errorf(loaderr.Path(li, "encoding"), fixEncoding, "layer %q uses encoding %q; export uncompressed JSON tile data", raw.Name, raw.Encoding)
			}
			if raw.Compression != "" {
				return loaderr.Errorf(loaderr.Path(li, "compression"), fixEncoding, "layer %q uses compression %q; export uncompressed JSON tile data", raw.Name, raw.Compression)
			}
			if len(raw.Chunks) > 0 {
				layer, err := buildChunkedTileLayer(m, raw, li, ox, oy, px, py, opacity, visible, props)
				if err != nil {
					return err
				}
				m.addTileLayer(layer)
				if m.Infinite {
					if layer.Width > m.Width {
						m.Width = layer.Width
					}
					if layer.Height > m.Height {
						m.Height = layer.Height
					}
				}
				continue
			}

			lw, lh := raw.Width, raw.Height
			if lw <= 0 {
				lw = m.Width
			}
			if lh <= 0 {
				lh = m.Height
			}
			if lw > 0 && lh > 0 && lw > math.MaxInt/lh {
				return loaderr.Errorf(loaderr.Path(li, "width"), "make the layer smaller", "layer %q size %dx%d overflows", raw.Name, lw, lh)
			}
			var data []uint32
			if len(raw.Data) > 0 {
				if err := json.Unmarshal(raw.Data, &data); err != nil {
					return loaderr.Errorf(loaderr.Path(li, "data"), "export the map again from Tiled; data is an array of tile GIDs", "layer %q data: %w", raw.Name, err)
				}
				if len(data) != lw*lh {
					return &loaderr.Error{Field: loaderr.Path(li, "data"), Problem: fmt.Sprintf("layer %q has %d tiles", raw.Name, len(data)), Expected: fmt.Sprintf("%d (%dx%d)", lw*lh, lw, lh), Fix: "export the map again from Tiled so data has one GID per tile of the layer"}
				}
			}
			m.addTileLayer(TileLayer{
				Name:       raw.Name,
				Width:      lw,
				Height:     lh,
				StartX:     raw.StartX,
				StartY:     raw.StartY,
				Data:       data,
				OffsetX:    ox,
				OffsetY:    oy,
				ParallaxX:  px,
				ParallaxY:  py,
				Opacity:    opacity,
				Visible:    visible,
				Properties: props,
			})

		case "imagelayer":
			var img core.ImageSource
			if raw.Image != "" {
				if resolve.LoadImage != nil {
					img, err = resolve.LoadImage(raw.Image)
					if err != nil {
						return loaderr.Errorf(loaderr.Path(li, "image"), "check that the layer's image exists at this path", "image layer %q load image %q: %w", raw.Name, raw.Image, err)
					}
				} else {
					img = core.ImageSource{URI: raw.Image}
				}
			}
			m.addImageLayer(ImageLayer{
				Name:       raw.Name,
				Image:      img,
				Width:      raw.ImageWidth,
				Height:     raw.ImageHeight,
				OffsetX:    ox,
				OffsetY:    oy,
				ParallaxX:  px,
				ParallaxY:  py,
				RepeatX:    raw.RepeatX,
				RepeatY:    raw.RepeatY,
				Opacity:    opacity,
				Visible:    visible,
				Properties: props,
			})

		case "objectgroup":
			objs := make([]Object, len(raw.Objects))
			for i, ro := range raw.Objects {
				op, err := parseProperties(ro.Properties)
				if err != nil {
					return nest(err, loaderr.Path(li, "objects", i, "properties"))
				}
				objVisible := true
				if ro.Visible != nil {
					objVisible = *ro.Visible
				}
				typ := ro.Type
				if typ == "" {
					typ = ro.Class
				}
				obj := Object{
					ID:         ro.ID,
					Name:       ro.Name,
					Type:       typ,
					X:          ro.X,
					Y:          ro.Y,
					Width:      ro.Width,
					Height:     ro.Height,
					Rotation:   ro.Rotation,
					GID:        ro.GID,
					Visible:    objVisible,
					Properties: op,
				}
				switch {
				case ro.GID != 0:
					obj.Shape = ObjectTile
				case ro.Point:
					obj.Shape = ObjectPoint
				case ro.Ellipse:
					obj.Shape = ObjectEllipse
				case len(ro.Polygon) > 0:
					obj.Shape = ObjectPolygon
					obj.Points = convertPoints(ro.Polygon)
				case len(ro.Polyline) > 0:
					obj.Shape = ObjectPolyline
					obj.Points = convertPoints(ro.Polyline)
				default:
					obj.Shape = ObjectRect
				}
				objs[i] = obj
			}
			m.Objects = append(m.Objects, ObjectLayer{
				Name:       raw.Name,
				OffsetX:    ox,
				OffsetY:    oy,
				Opacity:    opacity,
				Visible:    visible,
				Objects:    objs,
				Properties: props,
			})

		case "group":
			if err := loadTiledLayers(m, raw.Layers, ox, oy, px, py, opacity, visible, resolve); err != nil {
				return nest(err, loaderr.Path(li, "layers"))
			}
		}
	}
	return nil
}

// buildChunkedTileLayer builds the infinite map's tile layer raw, layer li of its array of layers, which the fields its errors name are relative to.
func buildChunkedTileLayer(m *Tilemap, raw tiledLayer, li int, ox, oy, px, py, opacity float32, visible bool, props Properties) (TileLayer, error) {
	type parsedChunk struct {
		x, y, w, h int
		data       []uint32
	}
	chunks := make([]parsedChunk, len(raw.Chunks))
	var minX, minY, maxX, maxY int
	for i, rc := range raw.Chunks {
		if rc.Width <= 0 || rc.Height <= 0 {
			return TileLayer{}, loaderr.Errorf(loaderr.Path(li, "chunks", i), "export the map again from Tiled; a chunk's width and height are above 0", "layer %q chunk %d has non-positive size %dx%d", raw.Name, i, rc.Width, rc.Height)
		}
		var cdata []uint32
		if err := json.Unmarshal(rc.Data, &cdata); err != nil {
			return TileLayer{}, loaderr.Errorf(loaderr.Path(li, "chunks", i, "data"), "export the map again from Tiled; data is an array of tile GIDs", "layer %q chunk %d data: %w", raw.Name, i, err)
		}
		if len(cdata) != rc.Width*rc.Height {
			return TileLayer{}, &loaderr.Error{Field: loaderr.Path(li, "chunks", i, "data"), Problem: fmt.Sprintf("layer %q chunk %d has %d tiles", raw.Name, i, len(cdata)), Expected: fmt.Sprintf("%d (%dx%d)", rc.Width*rc.Height, rc.Width, rc.Height), Fix: "export the map again from Tiled so each chunk's data has one GID per tile"}
		}
		chunks[i] = parsedChunk{x: rc.X, y: rc.Y, w: rc.Width, h: rc.Height, data: cdata}
		if i == 0 {
			minX, minY = rc.X, rc.Y
			maxX, maxY = rc.X+rc.Width, rc.Y+rc.Height
		} else {
			if rc.X < minX {
				minX = rc.X
			}
			if rc.Y < minY {
				minY = rc.Y
			}
			if rc.X+rc.Width > maxX {
				maxX = rc.X + rc.Width
			}
			if rc.Y+rc.Height > maxY {
				maxY = rc.Y + rc.Height
			}
		}
	}

	// Preserve stagger parity when normalizing staggered/hex infinite chunks into a 0-based grid.
	if m.Orientation == OrientationStaggered || m.Orientation == OrientationHexagonal {
		if m.StaggerAxis == StaggerX && (minX&1) != 0 {
			minX--
		} else if m.StaggerAxis == StaggerY && (minY&1) != 0 {
			minY--
		}
	}

	lw := maxX - minX
	lh := maxY - minY
	data := make([]uint32, lw*lh)
	for _, ch := range chunks {
		for r := range ch.h {
			dstRow := (ch.y - minY + r) * lw
			srcRow := r * ch.w
			copy(data[dstRow+(ch.x-minX):dstRow+(ch.x-minX)+ch.w], ch.data[srcRow:srcRow+ch.w])
		}
	}

	tx, ty := m.tileOrigin(minX, minY)
	return TileLayer{
		Name:       raw.Name,
		Width:      lw,
		Height:     lh,
		StartX:     minX,
		StartY:     minY,
		Data:       data,
		OffsetX:    ox + tx,
		OffsetY:    oy + ty,
		ParallaxX:  px,
		ParallaxY:  py,
		Opacity:    opacity,
		Visible:    visible,
		Properties: props,
	}, nil
}

func convertPoints(pts []tiledPoint) []core.Point {
	out := make([]core.Point, len(pts))
	for i, pt := range pts {
		out[i] = core.Point{X: core.Points(pt.X), Y: core.Points(pt.Y)}
	}
	return out
}

// parseProperties reads Tiled custom properties. The fields its errors name are relative to the array of properties, as in "[1].value".
func parseProperties(raw []tiledProperty) (Properties, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	props := make(Properties, len(raw))
	for pi, rp := range raw {
		bad := func(err error) error {
			return loaderr.Errorf(loaderr.Path(pi, "value"), fmt.Sprintf("give the property a value of its type, %s, in Tiled", typeName(rp.Type)), "property %q (%s): %w", rp.Name, typeName(rp.Type), err)
		}
		typ := rp.Type
		if typ == "" {
			typ = "string"
		}
		var val any
		switch typ {
		case "string", "color", "file":
			var s string
			if err := json.Unmarshal(rp.Value, &s); err != nil {
				return nil, bad(err)
			}
			val = s
		case "int", "object":
			var n int
			if err := json.Unmarshal(rp.Value, &n); err != nil {
				var f float64
				if err2 := json.Unmarshal(rp.Value, &f); err2 != nil {
					return nil, bad(err)
				}
				n = int(f)
			}
			val = n
		case "float":
			var f float64
			if err := json.Unmarshal(rp.Value, &f); err != nil {
				return nil, bad(err)
			}
			val = f
		case "bool":
			var b bool
			if err := json.Unmarshal(rp.Value, &b); err != nil {
				return nil, bad(err)
			}
			val = b
		default:
			if err := json.Unmarshal(rp.Value, &val); err != nil {
				return nil, bad(err)
			}
		}
		props[rp.Name] = Property{Name: rp.Name, Type: typ, Value: val}
	}
	return props, nil
}

// nest returns err with the field path base put in front of the field it names, when err is an *loaderr.Error, so a helper can name fields relative to the value it was handed and the full path is built only for an error.
func nest(err error, base string) error {
	e, ok := err.(*loaderr.Error)
	if !ok || e == nil {
		return err
	}
	c := *e
	switch {
	case c.Field == "":
		c.Field = base
	case c.Field[0] == '[':
		c.Field = base + c.Field
	default:
		c.Field = base + "." + c.Field
	}
	return &c
}

// typeName is a Tiled property type, "string" when the export leaves it out.
func typeName(typ string) string {
	if typ == "" {
		return "string"
	}
	return typ
}

type tiledMap struct {
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	TileWidth     float32           `json:"tilewidth"`
	TileHeight    float32           `json:"tileheight"`
	Orientation   string            `json:"orientation"`
	StaggerAxis   string            `json:"staggeraxis"`
	StaggerIndex  string            `json:"staggerindex"`
	HexSideLength float32           `json:"hexsidelength"`
	Infinite      bool              `json:"infinite"`
	Layers        []tiledLayer      `json:"layers"`
	Tilesets      []tiledTilesetRef `json:"tilesets"`
	Properties    []tiledProperty   `json:"properties"`
}

type tiledTilesetRef struct {
	FirstGID uint32 `json:"firstgid"`
	Source   string `json:"source"`
	tiledTileset
}

type tiledTileset struct {
	Name       string          `json:"name"`
	Image      string          `json:"image"`
	ImageWidth float32         `json:"imagewidth"`
	TileWidth  float32         `json:"tilewidth"`
	TileHeight float32         `json:"tileheight"`
	Columns    int             `json:"columns"`
	TileCount  int             `json:"tilecount"`
	Spacing    float32         `json:"spacing"`
	Margin     float32         `json:"margin"`
	Properties []tiledProperty `json:"properties"`
	Tiles      []tiledTile     `json:"tiles"`
}

type tiledTile struct {
	ID         uint32               `json:"id"`
	Type       string               `json:"type"`
	Class      string               `json:"class"`
	Properties []tiledProperty      `json:"properties"`
	Animation  []tiledAnimFrameJSON `json:"animation"`
}

type tiledAnimFrameJSON struct {
	TileID   uint32 `json:"tileid"`
	Duration int    `json:"duration"`
}

type tiledChunk struct {
	X      int             `json:"x"`
	Y      int             `json:"y"`
	Width  int             `json:"width"`
	Height int             `json:"height"`
	Data   json.RawMessage `json:"data"`
}

type tiledLayer struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Width       int             `json:"width"`
	Height      int             `json:"height"`
	StartX      int             `json:"startx"`
	StartY      int             `json:"starty"`
	OffsetX     float32         `json:"offsetx"`
	OffsetY     float32         `json:"offsety"`
	ParallaxX   *float32        `json:"parallaxx"`
	ParallaxY   *float32        `json:"parallaxy"`
	Opacity     *float32        `json:"opacity"`
	Visible     *bool           `json:"visible"`
	Encoding    string          `json:"encoding"`
	Compression string          `json:"compression"`
	Data        json.RawMessage `json:"data"`
	Chunks      []tiledChunk    `json:"chunks"`
	Image       string          `json:"image"`
	ImageWidth  float32         `json:"imagewidth"`
	ImageHeight float32         `json:"imageheight"`
	RepeatX     bool            `json:"repeatx"`
	RepeatY     bool            `json:"repeaty"`
	Objects     []tiledObject   `json:"objects"`
	Layers      []tiledLayer    `json:"layers"`
	Properties  []tiledProperty `json:"properties"`
}

type tiledObject struct {
	ID         int             `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Class      string          `json:"class"`
	X          float32         `json:"x"`
	Y          float32         `json:"y"`
	Width      float32         `json:"width"`
	Height     float32         `json:"height"`
	Rotation   float32         `json:"rotation"`
	GID        uint32          `json:"gid"`
	Visible    *bool           `json:"visible"`
	Point      bool            `json:"point"`
	Ellipse    bool            `json:"ellipse"`
	Polygon    []tiledPoint    `json:"polygon"`
	Polyline   []tiledPoint    `json:"polyline"`
	Properties []tiledProperty `json:"properties"`
}

type tiledPoint struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

type tiledProperty struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}
