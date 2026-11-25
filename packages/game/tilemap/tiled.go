package tilemap

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

// TilesetLoader resolves external tileset files and tileset images referenced by a Tiled map.
//
// Either tileset callback may be nil: LoadTileset (or LoadJSON as a fallback) is only needed when the map references an external .tsx or .tsj tileset, and LoadImage defaults to core.ImageSource{URI: imagePath}.
type TilesetLoader struct {
	// LoadTileset reads the raw bytes of an external tileset file (.tsx or .tsj) given its source path in the map.
	// When nil, LoadTiled falls back to LoadJSON.
	LoadTileset func(source string) ([]byte, error)
	// LoadJSON reads the raw bytes of an external tileset file (.tsj or .tsx) given its source path in the map.
	// Kept for compatibility with existing callers; used when LoadTileset is nil.
	LoadJSON func(source string) ([]byte, error)
	// LoadImage returns the ImageSource for a tileset's image path (resolved relative to the external tileset's directory when loaded from a .tsx or .tsj).
	LoadImage func(imagePath string) (core.ImageSource, error)
}

func (l TilesetLoader) loadTilesetFn() func(string) ([]byte, error) {
	if l.LoadTileset != nil {
		return l.LoadTileset
	}
	return l.LoadJSON
}

// LoadTiled parses a Tiled map — either JSON (.tmj) or XML (.tmx) — into a Tilemap, loading any external .tsx or .tsj tilesets and resolving tileset and image-layer images through resolve.
//
// Orthogonal, isometric, staggered, and hexagonal maps, infinite chunked maps, CSV, XML, and base64 tile data (uncompressed, zlib, and gzip), image layers, nested group layers, embedded and external tilesets (including cross-format TMX/TSJ and TMJ/TSX references), GID flip flags, object layers, custom properties, tile animations and the collision shapes Tiled's collision editor draws on tiles (into Tileset.Shapes) are supported. zstd compression is refused with an actionable fix hint.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it. An error in an external tileset names that tileset's source; for one in the map itself, a caller that read data from a file names it with loaderr.WithFile.
func LoadTiled(data []byte, resolve TilesetLoader) (*Tilemap, error) {
	if isXML(data) {
		raw, err := parseTMX(data)
		if err != nil {
			return nil, loaderr.Locate(err, "", nil, fixTiledXML)
		}
		m, err := buildTiledMap(raw, resolve)
		return m, loaderr.Locate(err, "", nil, fixTiledXML)
	}
	var raw tiledMap
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, loaderr.Locate(err, "", data, fixTiled)
	}
	m, err := buildTiledMap(raw, resolve)
	return m, loaderr.Locate(err, "", data, fixTiled)
}

// fixTiled is the fix for a Tiled JSON map error that has no more specific one.
const fixTiled = "correct the map in Tiled and export it again as JSON (.tmj)"

func buildTiledMap(raw tiledMap, resolve TilesetLoader) (*Tilemap, error) {
	orientation, err := parseTiledOrientation(raw.Orientation)
	if err != nil {
		return nil, &loaderr.Error{Line: raw.line, Column: raw.col, Field: "orientation", Problem: err.Error(), Expected: `"orthogonal", "isometric", "staggered" or "hexagonal"`, Fix: "set the map's orientation to one of those in Tiled's map properties", Err: err}
	}
	staggerAxis, err := parseTiledStaggerAxis(raw.StaggerAxis)
	if err != nil {
		return nil, &loaderr.Error{Line: raw.line, Column: raw.col, Field: "staggeraxis", Problem: err.Error(), Expected: `"x" or "y"`, Fix: "set the map's stagger axis to x or y in Tiled", Err: err}
	}
	staggerIndex, err := parseTiledStaggerIndex(raw.StaggerIndex)
	if err != nil {
		return nil, &loaderr.Error{Line: raw.line, Column: raw.col, Field: "staggerindex", Problem: err.Error(), Expected: `"odd" or "even"`, Fix: "set the map's stagger index to odd or even in Tiled", Err: err}
	}
	if raw.Width < 0 || raw.Height < 0 {
		field := "width"
		if raw.Width >= 0 {
			field = "height"
		}
		return nil, &loaderr.Error{Line: raw.line, Column: raw.col, Field: field, Problem: fmt.Sprintf("map size %dx%d must be non-negative", raw.Width, raw.Height), Fix: "give the map a width and height of 0 or more tiles"}
	}
	if raw.Width > 0 && raw.Height > 0 && raw.Width > math.MaxInt/raw.Height {
		return nil, &loaderr.Error{Line: raw.line, Column: raw.col, Field: "width", Problem: fmt.Sprintf("map size %dx%d overflows", raw.Width, raw.Height), Fix: "make the map smaller"}
	}
	if raw.TileWidth <= 0 || raw.TileHeight <= 0 {
		field := "tilewidth"
		if raw.TileWidth > 0 {
			field = "tileheight"
		}
		return nil, &loaderr.Error{Line: raw.line, Column: raw.col, Field: field, Problem: fmt.Sprintf("tile size %gx%g must be positive", raw.TileWidth, raw.TileHeight), Fix: "give the map a tile size above 0 in Tiled's map properties"}
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

	loadExternal := resolve.loadTilesetFn()
	for i, ref := range raw.Tilesets {
		if ref.FirstGID == 0 {
			return nil, &loaderr.Error{Line: ref.line, Column: ref.col, Field: loaderr.Path("tilesets", i, "firstgid"), Problem: fmt.Sprintf("tileset %d has firstgid 0", i), Fix: "save or export the map again from Tiled; a tileset's firstgid is 1 or more"}
		}
		var tsRaw tiledTileset
		if ref.Source != "" {
			if loadExternal == nil {
				return nil, &loaderr.Error{Line: ref.line, Column: ref.col, Field: loaderr.Path("tilesets", i, "source"), Problem: fmt.Sprintf("external tileset %q requires TilesetLoader.LoadTileset (or LoadJSON)", ref.Source), Fix: "set TilesetLoader.LoadTileset (or LoadJSON), or embed the tileset in the map in Tiled"}
			}
			ext, err := loadExternal(ref.Source)
			if err != nil {
				return nil, &loaderr.Error{Line: ref.line, Column: ref.col, Field: loaderr.Path("tilesets", i, "source"), Problem: fmt.Sprintf("load tileset %q: %v", ref.Source, err), Fix: "check that the tileset file exists next to the map at this path", Err: err}
			}
			var extLocData []byte
			extFix := "correct the tileset in Tiled and export it again as JSON (.tsj)"
			if isXML(ext) {
				extFix = fixTilesetXML
				tsRaw, err = parseTSX(ext)
				if err != nil {
					return nil, loaderr.Locate(err, ref.Source, nil, extFix)
				}
			} else {
				extLocData = ext
				if err := json.Unmarshal(ext, &tsRaw); err != nil {
					return nil, loaderr.Locate(err, ref.Source, ext, "export the tileset again from Tiled as JSON (.tsj)")
				}
			}
			ts, err := buildTileset(m, ref.FirstGID, tsRaw, path.Dir(ref.Source), resolve)
			if err != nil {
				// The error is about the tileset's own file.
				return nil, loaderr.Locate(err, ref.Source, extLocData, extFix)
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

	budget := maxMapDecompressedBytes
	if err := loadTiledLayers(m, raw.Layers, 0, 0, 1, 1, 1, true, resolve, &budget); err != nil {
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
				imgLine, imgCol := raw.imageLine, raw.imageCol
				if imgLine == 0 {
					imgLine, imgCol = raw.line, raw.col
				}
				return nil, &loaderr.Error{Line: imgLine, Column: imgCol, Field: "image", Problem: fmt.Sprintf("load image %q: %v", imgPath, err), Fix: "check that the tileset image exists at this path, relative to the tileset", Err: err}
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
					return nil, &loaderr.Error{
						Line:    f.line,
						Column:  f.col,
						Field:   loaderr.Path("tiles", ti, "animation", j, "duration"),
						Problem: fmt.Sprintf("tile %d animation frame %d has invalid duration %d", tile.ID, j, f.Duration),
						Fix:     "give the animation frame a duration of 0 or more milliseconds in Tiled",
					}
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
		if tile.ObjectGroup != nil && len(tile.ObjectGroup.Objects) > 0 {
			shapes, err := tiledTileShapes(tile.ObjectGroup.Objects)
			if err != nil {
				return nil, nest(err, loaderr.Path("tiles", ti))
			}
			if len(shapes) > 0 {
				if ts.Shapes == nil {
					ts.Shapes = make(map[uint32][]TileShape)
				}
				ts.Shapes[tile.ID] = shapes
			}
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

func ySortFromProperties(p Properties) bool {
	if len(p) == 0 {
		return false
	}
	for _, key := range []string{"y_sort", "ysort", "ySort", "y-sort"} {
		if v, ok := p.Bool(key); ok {
			return v
		}
	}
	return false
}

// loadTiledLayers adds layers to m. The fields its errors name are relative to the array of layers, as in "[2].data".
func loadTiledLayers(m *Tilemap, layers []tiledLayer, parentOX, parentOY, parentPX, parentPY, parentOpacity float32, parentVisible bool, resolve TilesetLoader, budget *int64) error {
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

		switch raw.Type {
		case "tilelayer":
			if err := validateLayerEncoding(raw, li); err != nil {
				return err
			}
			if m.Infinite || len(raw.Chunks) > 0 {
				layer, err := buildChunkedTileLayer(raw, li, ox, oy, px, py, opacity, visible, props, budget)
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
			if lw < 0 || lh < 0 {
				field := loaderr.Path(li, "width")
				if lw >= 0 {
					field = loaderr.Path(li, "height")
				}
				return &loaderr.Error{Line: raw.line, Column: raw.col, Field: field, Problem: fmt.Sprintf("layer %q size %dx%d must be non-negative", raw.Name, lw, lh), Fix: "give the layer a width and height of 0 or more tiles"}
			}
			if lw == 0 {
				lw = m.Width
			}
			if lh == 0 {
				lh = m.Height
			}
			if lw > 0 && lh > 0 && lw > math.MaxInt/lh {
				return &loaderr.Error{Line: raw.line, Column: raw.col, Field: loaderr.Path(li, "width"), Problem: fmt.Sprintf("layer %q size %dx%d overflows", raw.Name, lw, lh), Fix: "make the layer smaller"}
			}

			data, err := decodeFiniteLayerData(raw, li, lw, lh, budget)
			if err != nil {
				return err
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
				YSort:      ySortFromProperties(props),
				Properties: props,
			})

		case "imagelayer":
			var img core.ImageSource
			if raw.Image != "" {
				if resolve.LoadImage != nil {
					img, err = resolve.LoadImage(raw.Image)
					if err != nil {
						imgLine, imgCol := raw.imageLine, raw.imageCol
						if imgLine == 0 {
							imgLine, imgCol = raw.line, raw.col
						}
						return &loaderr.Error{Line: imgLine, Column: imgCol, Field: loaderr.Path(li, "image"), Problem: fmt.Sprintf("image layer %q load image %q: %v", raw.Name, raw.Image, err), Fix: "check that the layer's image exists at this path", Err: err}
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
			if err := loadTiledLayers(m, raw.Layers, ox, oy, px, py, opacity, visible, resolve, budget); err != nil {
				return nest(err, loaderr.Path(li, "layers"))
			}
		}
	}
	return nil
}

func decodeFiniteLayerData(raw tiledLayer, li, lw, lh int, budget *int64) ([]uint32, error) {
	field := loaderr.Path(li, "data")
	label := fmt.Sprintf("layer %q", raw.Name)
	expected := lw * lh

	if raw.isXML {
		if !raw.hasXMLData {
			return nil, nil
		}
		var data []uint32
		var err error
		switch raw.Encoding {
		case "base64":
			data, err = decodeBase64TileData(raw.xmlDataText, raw.Compression, expected, budget, field, raw.dataLine, raw.dataCol, label)
			if err != nil {
				return nil, err
			}
			return data, nil
		case "csv":
			data, err = decodeCSVTileData(raw.xmlDataText, field, raw.dataLine, raw.dataCol, label)
			if err != nil {
				return nil, err
			}
		case "", "xml":
			data = raw.xmlTiles
		}
		if len(data) != expected {
			return nil, &loaderr.Error{Line: raw.dataLine, Column: raw.dataCol, Field: field, Problem: fmt.Sprintf("layer %q has %d tiles", raw.Name, len(data)), Expected: fmt.Sprintf("%d (%dx%d)", expected, lw, lh), Fix: "save the map again from Tiled so data has one GID per tile of the layer"}
		}
		return data, nil
	}

	if len(raw.Data) == 0 {
		return nil, nil
	}
	if raw.Encoding == "base64" {
		var b64 string
		if err := json.Unmarshal(raw.Data, &b64); err != nil {
			return nil, loaderr.Errorf(field, "export the map again from Tiled; base64 tile data must be a JSON string", "layer %q data: %w", raw.Name, err)
		}
		return decodeBase64TileData(b64, raw.Compression, expected, budget, field, 0, 0, label)
	}
	trimmed := bytes.TrimSpace(raw.Data)
	var data []uint32
	if len(trimmed) > 0 && trimmed[0] == '"' && raw.Encoding == "csv" {
		var csvStr string
		if err := json.Unmarshal(raw.Data, &csvStr); err != nil {
			return nil, loaderr.Errorf(field, "export the map again from Tiled; data is an array of tile GIDs", "layer %q data: %w", raw.Name, err)
		}
		var err error
		data, err = decodeCSVTileData(csvStr, field, 0, 0, label)
		if err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(raw.Data, &data); err != nil {
			return nil, loaderr.Errorf(field, "export the map again from Tiled; data is an array of tile GIDs", "layer %q data: %w", raw.Name, err)
		}
	}
	if len(data) != expected {
		return nil, &loaderr.Error{Field: field, Problem: fmt.Sprintf("layer %q has %d tiles", raw.Name, len(data)), Expected: fmt.Sprintf("%d (%dx%d)", expected, lw, lh), Fix: "export the map again from Tiled so data has one GID per tile of the layer"}
	}
	return data, nil
}

func decodeChunkTileData(raw tiledLayer, rc tiledChunk, li, ci int, budget *int64) ([]uint32, error) {
	field := loaderr.Path(li, "chunks", ci, "data")
	label := fmt.Sprintf("layer %q chunk %d", raw.Name, ci)
	if rc.Width > math.MaxInt/rc.Height {
		return nil, &loaderr.Error{Line: rc.line, Column: rc.col, Field: loaderr.Path(li, "chunks", ci), Problem: fmt.Sprintf("%s size %dx%d overflows", label, rc.Width, rc.Height), Fix: "make the chunk smaller in Tiled"}
	}
	expected := rc.Width * rc.Height

	if rc.isXML {
		var cdata []uint32
		var err error
		switch raw.Encoding {
		case "base64":
			return decodeBase64TileData(rc.xmlDataText, raw.Compression, expected, budget, field, rc.line, rc.col, label)
		case "csv":
			cdata, err = decodeCSVTileData(rc.xmlDataText, field, rc.line, rc.col, label)
			if err != nil {
				return nil, err
			}
		case "", "xml":
			cdata = rc.xmlTiles
		}
		if len(cdata) != expected {
			return nil, &loaderr.Error{Line: rc.line, Column: rc.col, Field: field, Problem: fmt.Sprintf("%s has %d tiles", label, len(cdata)), Expected: fmt.Sprintf("%d (%dx%d)", expected, rc.Width, rc.Height), Fix: "save the map again from Tiled so each chunk's data has one GID per tile"}
		}
		return cdata, nil
	}

	if raw.Encoding == "base64" {
		var b64 string
		if err := json.Unmarshal(rc.Data, &b64); err != nil {
			return nil, loaderr.Errorf(field, "export the map again from Tiled; base64 chunk data must be a JSON string", "%s data: %w", label, err)
		}
		return decodeBase64TileData(b64, raw.Compression, expected, budget, field, 0, 0, label)
	}
	trimmed := bytes.TrimSpace(rc.Data)
	var cdata []uint32
	if len(trimmed) > 0 && trimmed[0] == '"' && raw.Encoding == "csv" {
		var csvStr string
		if err := json.Unmarshal(rc.Data, &csvStr); err != nil {
			return nil, loaderr.Errorf(field, "export the map again from Tiled; data is an array of tile GIDs", "%s data: %w", label, err)
		}
		var err error
		cdata, err = decodeCSVTileData(csvStr, field, 0, 0, label)
		if err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(rc.Data, &cdata); err != nil {
			return nil, loaderr.Errorf(field, "export the map again from Tiled; data is an array of tile GIDs", "%s data: %w", label, err)
		}
	}
	if len(cdata) != expected {
		return nil, &loaderr.Error{Field: field, Problem: fmt.Sprintf("%s has %d tiles", label, len(cdata)), Expected: fmt.Sprintf("%d (%dx%d)", expected, rc.Width, rc.Height), Fix: "export the map again from Tiled so each chunk's data has one GID per tile"}
	}
	return cdata, nil
}

// buildChunkedTileLayer loads an infinite Tiled map's tile layer raw directly into a sparse chunked TileLayer,
// preserving true negative and positive chunk coordinates without flattening into a dense array.
func buildChunkedTileLayer(raw tiledLayer, li int, ox, oy, px, py, opacity float32, visible bool, props Properties, budget *int64) (TileLayer, error) {
	if len(raw.Chunks) == 0 {
		layer := NewChunkedTileLayer(raw.Name, DefaultChunkSize)
		layer.StartX = raw.StartX
		layer.StartY = raw.StartY
		layer.OffsetX = ox
		layer.OffsetY = oy
		layer.ParallaxX = px
		layer.ParallaxY = py
		layer.Opacity = opacity
		layer.Visible = visible
		layer.YSort = ySortFromProperties(props)
		layer.Properties = props
		return layer, nil
	}

	type parsedChunk struct {
		x, y, w, h int
		data       []uint32
	}
	chunks := make([]parsedChunk, len(raw.Chunks))
	var minX, minY, maxX, maxY int64
	uniformChunkSize := 0
	uniform := true

	for i, rc := range raw.Chunks {
		if rc.Width <= 0 || rc.Height <= 0 {
			return TileLayer{}, &loaderr.Error{Line: rc.line, Column: rc.col, Field: loaderr.Path(li, "chunks", i), Problem: fmt.Sprintf("layer %q chunk %d has non-positive size %dx%d", raw.Name, i, rc.Width, rc.Height), Fix: "save or export the map again from Tiled; a chunk's width and height are above 0"}
		}
		cdata, err := decodeChunkTileData(raw, rc, li, i, budget)
		if err != nil {
			return TileLayer{}, err
		}
		chunks[i] = parsedChunk{x: rc.X, y: rc.Y, w: rc.Width, h: rc.Height, data: cdata}
		cx0 := int64(rc.X)
		cy0 := int64(rc.Y)
		cx1 := cx0 + int64(rc.Width)
		cy1 := cy0 + int64(rc.Height)
		if i == 0 {
			minX, minY = cx0, cy0
			maxX, maxY = cx1, cy1
			if rc.Width == rc.Height && ValidChunkSize(rc.Width) && rc.X%rc.Width == 0 && rc.Y%rc.Height == 0 {
				uniformChunkSize = rc.Width
			} else {
				uniform = false
			}
		} else {
			if cx0 < minX {
				minX = cx0
			}
			if cy0 < minY {
				minY = cy0
			}
			if cx1 > maxX {
				maxX = cx1
			}
			if cy1 > maxY {
				maxY = cy1
			}
			if uniform && (rc.Width != uniformChunkSize || rc.Height != uniformChunkSize || rc.X%uniformChunkSize != 0 || rc.Y%uniformChunkSize != 0) {
				uniform = false
			}
		}
	}

	chunkSize := DefaultChunkSize
	if uniform && uniformChunkSize > 0 {
		chunkSize = uniformChunkSize
	}

	layer := NewChunkedTileLayer(raw.Name, chunkSize)
	if minX >= math.MinInt32 && maxX <= math.MaxInt32 && maxX-minX <= math.MaxInt32 {
		layer.Width = int(maxX - minX)
		layer.StartX = int(minX)
	}
	if minY >= math.MinInt32 && maxY <= math.MaxInt32 && maxY-minY <= math.MaxInt32 {
		layer.Height = int(maxY - minY)
		layer.StartY = int(minY)
	}
	layer.OffsetX = ox
	layer.OffsetY = oy
	layer.ParallaxX = px
	layer.ParallaxY = py
	layer.Opacity = opacity
	layer.Visible = visible
	layer.YSort = ySortFromProperties(props)
	layer.Properties = props

	for _, ch := range chunks {
		for r := range ch.h {
			rowOff := r * ch.w
			for c := range ch.w {
				gid := ch.data[rowOff+c]
				if gid == 0 {
					continue
				}
				layer.setTileChunked(ch.x+c, ch.y+r, gid, false)
			}
		}
	}

	return layer, nil
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
			e := loaderr.Errorf(loaderr.Path(pi, "value"), fmt.Sprintf("give the property a value of its type, %s, in Tiled", typeName(rp.Type)), "property %q (%s): %w", rp.Name, typeName(rp.Type), err)
			e.Line, e.Column = rp.line, rp.col
			return e
		}
		typ := rp.Type
		if typ == "" {
			typ = "string"
		}
		var val any
		if rp.isXML {
			switch typ {
			case "string", "color", "file":
				val = rp.xmlValue
			case "int", "object":
				n, err := strconv.Atoi(strings.TrimSpace(rp.xmlValue))
				if err != nil {
					return nil, bad(err)
				}
				val = n
			case "float":
				f, err := strconv.ParseFloat(strings.TrimSpace(rp.xmlValue), 64)
				if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
					if err == nil {
						err = fmt.Errorf("non-finite float %q", rp.xmlValue)
					}
					return nil, bad(err)
				}
				val = f
			case "bool":
				switch strings.TrimSpace(rp.xmlValue) {
				case "true":
					val = true
				case "false":
					val = false
				default:
					return nil, bad(fmt.Errorf("invalid bool %q", rp.xmlValue))
				}
			default:
				val = rp.xmlValue
			}
			props[rp.Name] = Property{Name: rp.Name, Type: typ, Value: val}
			continue
		}
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
	line          int
	col           int
}

type tiledTilesetRef struct {
	FirstGID uint32 `json:"firstgid"`
	Source   string `json:"source"`
	tiledTileset
	line int
	col  int
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
	line       int
	col        int
	imageLine  int
	imageCol   int
}

type tiledTile struct {
	ID         uint32               `json:"id"`
	Type       string               `json:"type"`
	Class      string               `json:"class"`
	Properties []tiledProperty      `json:"properties"`
	Animation  []tiledAnimFrameJSON `json:"animation"`
	line       int
	col        int
	// ObjectGroup is the tile's collision shapes from Tiled's collision editor.
	ObjectGroup *tiledLayer `json:"objectgroup"`
}

type tiledAnimFrameJSON struct {
	TileID   uint32 `json:"tileid"`
	Duration int    `json:"duration"`
	line     int
	col      int
}

type tiledChunk struct {
	X           int             `json:"x"`
	Y           int             `json:"y"`
	Width       int             `json:"width"`
	Height      int             `json:"height"`
	Data        json.RawMessage `json:"data"`
	isXML       bool
	xmlDataText string
	xmlTiles    []uint32
	line        int
	col         int
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
	isXML       bool
	hasXMLData  bool
	xmlDataText string
	xmlTiles    []uint32
	line        int
	col         int
	dataLine    int
	dataCol     int
	imageLine   int
	imageCol    int
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
	line       int
	col        int
}

type tiledPoint struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

type tiledProperty struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Value    json.RawMessage `json:"value"`
	isXML    bool
	xmlValue string
	line     int
	col      int
}
