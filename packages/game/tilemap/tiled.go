package tilemap

import (
	"cmp"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

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
func LoadTiled(data []byte, resolve TilesetLoader) (*Tilemap, error) {
	var raw tiledMap
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("tilemap: tiled: %w", err)
	}
	orientation, err := parseTiledOrientation(raw.Orientation)
	if err != nil {
		return nil, err
	}
	staggerAxis, err := parseTiledStaggerAxis(raw.StaggerAxis)
	if err != nil {
		return nil, err
	}
	staggerIndex, err := parseTiledStaggerIndex(raw.StaggerIndex)
	if err != nil {
		return nil, err
	}
	if raw.Width < 0 || raw.Height < 0 {
		return nil, fmt.Errorf("tilemap: tiled: map size %dx%d must be non-negative", raw.Width, raw.Height)
	}
	if raw.TileWidth <= 0 || raw.TileHeight <= 0 {
		return nil, fmt.Errorf("tilemap: tiled: tile size %gx%g must be positive", raw.TileWidth, raw.TileHeight)
	}

	props, err := parseProperties(raw.Properties)
	if err != nil {
		return nil, fmt.Errorf("tilemap: tiled: map properties: %w", err)
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
			return nil, fmt.Errorf("tilemap: tiled: tileset %d has firstgid 0", i)
		}
		var tsRaw tiledTileset
		baseDir := ""
		if ref.Source != "" {
			if resolve.LoadJSON == nil {
				return nil, fmt.Errorf("tilemap: tiled: external tileset %q requires TilesetLoader.LoadJSON", ref.Source)
			}
			ext, err := resolve.LoadJSON(ref.Source)
			if err != nil {
				return nil, fmt.Errorf("tilemap: tiled: load tileset %q: %w", ref.Source, err)
			}
			if err := json.Unmarshal(ext, &tsRaw); err != nil {
				return nil, fmt.Errorf("tilemap: tiled: parse tileset %q: %w", ref.Source, err)
			}
			baseDir = path.Dir(ref.Source)
		} else {
			tsRaw = ref.tiledTileset
		}

		ts, err := buildTileset(m, ref.FirstGID, tsRaw, baseDir, resolve)
		if err != nil {
			return nil, fmt.Errorf("tilemap: tiled: tileset %d (%q): %w", i, tsRaw.Name, err)
		}
		m.Tilesets = append(m.Tilesets, ts)
	}
	slices.SortFunc(m.Tilesets, func(a, b *Tileset) int {
		return cmp.Compare(a.FirstGID, b.FirstGID)
	})

	if err := loadTiledLayers(m, raw.Layers, 0, 0, 1, 1, 1, true, resolve); err != nil {
		return nil, err
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
				return nil, fmt.Errorf("load image %q: %w", imgPath, err)
			}
		} else {
			img = core.ImageSource{URI: imgPath}
		}
	}

	props, err := parseProperties(raw.Properties)
	if err != nil {
		return nil, fmt.Errorf("properties: %w", err)
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

	for _, tile := range raw.Tiles {
		if len(tile.Properties) > 0 {
			tp, err := parseProperties(tile.Properties)
			if err != nil {
				return nil, fmt.Errorf("tile %d properties: %w", tile.ID, err)
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
				if f.Duration < 0 {
					return nil, fmt.Errorf("tile %d animation frame %d has negative duration %d", tile.ID, j, f.Duration)
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

func loadTiledLayers(m *Tilemap, layers []tiledLayer, parentOX, parentOY, parentPX, parentPY, parentOpacity float32, parentVisible bool, resolve TilesetLoader) error {
	for _, raw := range layers {
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
			return fmt.Errorf("tilemap: tiled: layer %q properties: %w", raw.Name, err)
		}

		switch raw.Type {
		case "tilelayer":
			if raw.Encoding != "" {
				return fmt.Errorf("tilemap: tiled: layer %q uses encoding %q; export uncompressed JSON tile data", raw.Name, raw.Encoding)
			}
			if raw.Compression != "" {
				return fmt.Errorf("tilemap: tiled: layer %q uses compression %q; export uncompressed JSON tile data", raw.Name, raw.Compression)
			}
			if len(raw.Chunks) > 0 {
				layer, err := buildChunkedTileLayer(m, raw, ox, oy, px, py, opacity, visible, props)
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
			var data []uint32
			if len(raw.Data) > 0 {
				if err := json.Unmarshal(raw.Data, &data); err != nil {
					return fmt.Errorf("tilemap: tiled: layer %q data: %w", raw.Name, err)
				}
				if len(data) != lw*lh {
					return fmt.Errorf("tilemap: tiled: layer %q has %d tiles, want %d (%dx%d)", raw.Name, len(data), lw*lh, lw, lh)
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
						return fmt.Errorf("tilemap: tiled: image layer %q load image %q: %w", raw.Name, raw.Image, err)
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
					return fmt.Errorf("tilemap: tiled: layer %q object %d properties: %w", raw.Name, ro.ID, err)
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
				return err
			}
		}
	}
	return nil
}

func buildChunkedTileLayer(m *Tilemap, raw tiledLayer, ox, oy, px, py, opacity float32, visible bool, props Properties) (TileLayer, error) {
	type parsedChunk struct {
		x, y, w, h int
		data       []uint32
	}
	chunks := make([]parsedChunk, len(raw.Chunks))
	var minX, minY, maxX, maxY int
	for i, rc := range raw.Chunks {
		if rc.Width <= 0 || rc.Height <= 0 {
			return TileLayer{}, fmt.Errorf("tilemap: tiled: layer %q chunk %d has non-positive size %dx%d", raw.Name, i, rc.Width, rc.Height)
		}
		var cdata []uint32
		if err := json.Unmarshal(rc.Data, &cdata); err != nil {
			return TileLayer{}, fmt.Errorf("tilemap: tiled: layer %q chunk %d data: %w", raw.Name, i, err)
		}
		if len(cdata) != rc.Width*rc.Height {
			return TileLayer{}, fmt.Errorf("tilemap: tiled: layer %q chunk %d has %d tiles, want %d (%dx%d)", raw.Name, i, len(cdata), rc.Width*rc.Height, rc.Width, rc.Height)
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

func parseProperties(raw []tiledProperty) (Properties, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	props := make(Properties, len(raw))
	for _, rp := range raw {
		typ := rp.Type
		if typ == "" {
			typ = "string"
		}
		var val any
		switch typ {
		case "string", "color", "file":
			var s string
			if err := json.Unmarshal(rp.Value, &s); err != nil {
				return nil, fmt.Errorf("property %q (%s): %w", rp.Name, typ, err)
			}
			val = s
		case "int", "object":
			var n int
			if err := json.Unmarshal(rp.Value, &n); err != nil {
				var f float64
				if err2 := json.Unmarshal(rp.Value, &f); err2 != nil {
					return nil, fmt.Errorf("property %q (%s): %w", rp.Name, typ, err)
				}
				n = int(f)
			}
			val = n
		case "float":
			var f float64
			if err := json.Unmarshal(rp.Value, &f); err != nil {
				return nil, fmt.Errorf("property %q (%s): %w", rp.Name, typ, err)
			}
			val = f
		case "bool":
			var b bool
			if err := json.Unmarshal(rp.Value, &b); err != nil {
				return nil, fmt.Errorf("property %q (%s): %w", rp.Name, typ, err)
			}
			val = b
		default:
			if err := json.Unmarshal(rp.Value, &val); err != nil {
				return nil, fmt.Errorf("property %q (%s): %w", rp.Name, typ, err)
			}
		}
		props[rp.Name] = Property{Name: rp.Name, Type: typ, Value: val}
	}
	return props, nil
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
