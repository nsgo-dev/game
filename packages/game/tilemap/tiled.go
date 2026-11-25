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
	"sync"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

// TilesetLoader resolves external tileset files, object template files, and images referenced by a Tiled map.
//
// Any callback may be nil: LoadTileset (or LoadJSON as a fallback) is only needed when the map or a template references an external .tsx or .tsj tileset, LoadTemplate is only needed when an object references an external .tx or .tj template, and LoadImage defaults to core.ImageSource{URI: imagePath}.
type TilesetLoader struct {
	// LoadTileset reads the raw bytes of an external tileset file (.tsx or .tsj) given its source path in the map (or resolved relative to a template's directory).
	// When nil, LoadTiled falls back to LoadJSON.
	LoadTileset func(source string) ([]byte, error)
	// LoadJSON reads the raw bytes of an external tileset file (.tsj or .tsx) given its source path in the map.
	// Kept for compatibility with existing callers; used when LoadTileset is nil.
	LoadJSON func(source string) ([]byte, error)
	// LoadTemplate reads the raw bytes of an external object template file (.tx or .tj) given its source path in the map.
	LoadTemplate func(source string) ([]byte, error)
	// LoadImage returns the ImageSource for a tileset, tile, or image-layer image path (resolved relative to the external tileset's or template's directory when loaded from one).
	LoadImage func(imagePath string) (core.ImageSource, error)
}

func (l TilesetLoader) loadTilesetFn() func(string) ([]byte, error) {
	if l.LoadTileset != nil {
		return l.LoadTileset
	}
	return l.LoadJSON
}

// LoadTiled parses a Tiled map — either JSON (.tmj) or XML (.tmx) — into a Tilemap, loading any external .tsx or .tsj tilesets and .tx or .tj object templates and resolving tileset, collection-tile, and image-layer images through resolve.
//
// Orthogonal, isometric, staggered, and hexagonal maps, infinite chunked maps, CSV, XML, and base64 tile data (uncompressed, zlib, and gzip), image layers, nested group layers, layer tint colors, embedded and external grid and image-collection tilesets (with tileoffset, objectalignment, and cross-format TMX/TSJ and TMJ/TSX references), GID flip flags, object layers (including tile objects, text objects, and .tx/.tj templates), custom properties, tile animations and the collision shapes Tiled's collision editor draws on tiles (into Tileset.Shapes) are supported. zstd compression is refused with an actionable fix hint.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it. An error in an external tileset or template names that file's source; for one in the map itself, a caller that read data from a file names it with loaderr.WithFile.
var tiledMapPool = sync.Pool{
	New: func() any { return new(tiledMap) },
}

func getTiledMap() *tiledMap {
	return tiledMapPool.Get().(*tiledMap)
}

func putTiledMap(m *tiledMap) {
	layers := m.Layers
	tilesets := m.Tilesets
	props := m.Properties
	clear(layers)
	clear(tilesets)
	clear(props)
	*m = tiledMap{}
	if cap(layers) <= 64 {
		m.Layers = layers[:0]
	}
	if cap(tilesets) <= 32 {
		m.Tilesets = tilesets[:0]
	}
	if cap(props) <= 64 {
		m.Properties = props[:0]
	}
	tiledMapPool.Put(m)
}

func LoadTiled(data []byte, resolve TilesetLoader) (*Tilemap, error) {
	if isXML(data) {
		raw, err := parseTMX(data)
		if err != nil {
			return nil, loaderr.LocateJSON(err, "", nil, fixTiledXML)
		}
		m, err := buildTiledMap(raw, resolve)
		return m, loaderr.LocateJSON(err, "", nil, fixTiledXML)
	}
	raw := getTiledMap()
	defer putTiledMap(raw)
	if err := json.Unmarshal(data, raw); err != nil {
		return nil, loaderr.LocateJSON(err, "", data, fixTiled)
	}
	m, err := buildTiledMap(*raw, resolve)
	return m, loaderr.LocateJSON(err, "", data, fixTiled)
}

// fixTiled is the fix for a Tiled JSON map error that has no more specific one.
const (
	fixTiled        = "correct the map in Tiled and export it again as JSON (.tmj)"
	fixTilesetJSON  = "correct the tileset in Tiled and export it again as JSON (.tsj)"
	fixTemplateJSON = "correct the template in Tiled and export it again as JSON (.tj)"
)

type sourceTilesetEntry struct {
	source string
	ts     *Tileset
}

type loadedTemplate struct {
	source       string
	obj          tiledObject
	props        Properties
	ts           *Tileset
	tmplFirstGID uint32
}

type tiledLoadCtx struct {
	m         *Tilemap
	resolve   TilesetLoader
	budget    int64
	rawTS     []tiledTilesetRef
	sourceTS  []sourceTilesetEntry
	templates []loadedTemplate
}

func (c *tiledLoadCtx) findSourceTileset(cleanSource string) *Tileset {
	for i := range c.sourceTS {
		if c.sourceTS[i].source == cleanSource {
			return c.sourceTS[i].ts
		}
	}
	for i := range c.rawTS {
		if c.rawTS[i].Source != "" && path.Clean(c.rawTS[i].Source) == cleanSource {
			if ts, _ := c.m.TilesetForGID(c.rawTS[i].FirstGID); ts != nil && ts.FirstGID == c.rawTS[i].FirstGID {
				return ts
			}
		}
	}
	return nil
}

func joinTiledPath(baseDir, rel string) string {
	if baseDir != "" && baseDir != "." && !path.IsAbs(rel) && !strings.Contains(rel, ":") {
		return path.Join(baseDir, rel)
	}
	return rel
}

func resolveTiledImage(rawPath, baseDir string, resolve TilesetLoader, line, col int, field, prefix, fix string) (core.ImageSource, error) {
	imgPath := joinTiledPath(baseDir, rawPath)
	if resolve.LoadImage == nil {
		return core.ImageSource{URI: imgPath}, nil
	}
	img, err := resolve.LoadImage(imgPath)
	if err != nil {
		return core.ImageSource{}, &loaderr.Error{
			Line: line, Column: col, Field: field,
			Problem: prefix + "load image " + strconv.Quote(imgPath) + ": " + err.Error(),
			Fix:     fix, Err: err,
		}
	}
	return img, nil
}

func loadExternalTileset(m *Tilemap, firstGID uint32, source string, line, col int, field string, resolve TilesetLoader) (*Tileset, error) {
	loadExternal := resolve.loadTilesetFn()
	if loadExternal == nil {
		return nil, &loaderr.Error{
			Line: line, Column: col, Field: field,
			Problem: "external tileset " + strconv.Quote(source) + " requires TilesetLoader.LoadTileset (or LoadJSON)",
			Fix:     "set TilesetLoader.LoadTileset (or LoadJSON), or embed the tileset in the map in Tiled",
		}
	}
	ext, err := loadExternal(source)
	if err != nil {
		return nil, &loaderr.Error{
			Line: line, Column: col, Field: field,
			Problem: "load tileset " + strconv.Quote(source) + ": " + err.Error(),
			Fix:     "check that the tileset file exists next to the map at this path",
			Err:     err,
		}
	}
	var tsRaw tiledTileset
	var extLocData []byte
	extFix := fixTilesetJSON
	if isXML(ext) {
		extFix = fixTilesetXML
		tsRaw, err = parseTSX(ext)
		if err != nil {
			return nil, loaderr.LocateJSON(err, source, nil, extFix)
		}
	} else {
		extLocData = ext
		if err := json.Unmarshal(ext, &tsRaw); err != nil {
			return nil, loaderr.LocateJSON(err, source, ext, "export the tileset again from Tiled as JSON (.tsj)")
		}
	}
	ts, err := buildTileset(m, firstGID, tsRaw, path.Dir(source), resolve)
	if err != nil {
		return nil, loaderr.LocateJSON(err, source, extLocData, extFix)
	}
	return ts, nil
}

func (c *tiledLoadCtx) loadTemplate(source string, line, col int, field string) (loadedTemplate, error) {
	for i := range c.templates {
		if c.templates[i].source == source {
			return c.templates[i], nil
		}
	}
	if c.resolve.LoadTemplate == nil {
		return loadedTemplate{}, &loaderr.Error{
			Line: line, Column: col, Field: field,
			Problem: "object template " + strconv.Quote(source) + " requires TilesetLoader.LoadTemplate",
			Fix:     "set TilesetLoader.LoadTemplate, or detach the object from its template in Tiled",
		}
	}
	rawBytes, err := c.resolve.LoadTemplate(source)
	if err != nil {
		return loadedTemplate{}, &loaderr.Error{
			Line: line, Column: col, Field: field,
			Problem: "load template " + strconv.Quote(source) + ": " + err.Error(),
			Fix:     "check that the template file exists at this path",
			Err:     err,
		}
	}

	var tmpl tiledTemplate
	var locData []byte
	fix := fixTemplateJSON
	if isXML(rawBytes) {
		fix = fixTemplateXML
		tmpl, err = parseTX(rawBytes)
		if err != nil {
			return loadedTemplate{}, loaderr.LocateJSON(err, source, nil, fix)
		}
	} else {
		locData = rawBytes
		if err := json.Unmarshal(rawBytes, &tmpl); err != nil {
			return loadedTemplate{}, loaderr.LocateJSON(err, source, rawBytes, fix)
		}
	}
	if tmpl.Object == nil {
		return loadedTemplate{}, loaderr.LocateJSON(&loaderr.Error{
			Field: "object", Problem: "template has no object definition", Fix: fix,
		}, source, locData, fix)
	}

	lt := loadedTemplate{source: source, obj: *tmpl.Object}
	if len(tmpl.Object.Properties) > 0 {
		props, err := parseProperties(tmpl.Object.Properties)
		if err != nil {
			return loadedTemplate{}, loaderr.LocateJSON(nest(err, "object.properties"), source, locData, fix)
		}
		lt.props = props
	}
	if tmpl.Object.Text != nil {
		if _, err := parseTiledText(tmpl.Object.Text); err != nil {
			return loadedTemplate{}, loaderr.LocateJSON(nest(err, "object.text"), source, locData, fix)
		}
	}
	if tmpl.Tileset != nil {
		ref := tmpl.Tileset
		fg := ref.FirstGID
		if fg == 0 {
			fg = 1
		}
		lt.tmplFirstGID = fg
		tmplDir := path.Dir(source)
		if ref.Source != "" {
			resolvedSrc := path.Clean(joinTiledPath(tmplDir, ref.Source))
			if existing := c.findSourceTileset(resolvedSrc); existing != nil {
				lt.ts = existing
			} else {
				nextGID, ok := c.m.nextFirstGID()
				if !ok {
					return loadedTemplate{}, loaderr.LocateJSON(tiledErr(ref.line, ref.col, "tileset", "template tileset firstgid overflows tile ID range", "", "reduce tileset tile counts or firstgid values in Tiled", nil), source, locData, fix)
				}
				ts, err := loadExternalTileset(c.m, nextGID, resolvedSrc, ref.line, ref.col, "tileset.source", c.resolve)
				if err != nil {
					if le, ok := err.(*loaderr.Error); ok && le.File == "" {
						return loadedTemplate{}, loaderr.LocateJSON(err, source, locData, fix)
					}
					return loadedTemplate{}, err
				}
				c.m.Tilesets = append(c.m.Tilesets, ts)
				c.sourceTS = append(c.sourceTS, sourceTilesetEntry{source: resolvedSrc, ts: ts})
				lt.ts = ts
			}
		} else {
			nextGID, ok := c.m.nextFirstGID()
			if !ok {
				return loadedTemplate{}, loaderr.LocateJSON(tiledErr(ref.line, ref.col, "tileset", "template tileset firstgid overflows tile ID range", "", "reduce tileset tile counts or firstgid values in Tiled", nil), source, locData, fix)
			}
			ts, err := buildTileset(c.m, nextGID, ref.tiledTileset, tmplDir, c.resolve)
			if err != nil {
				return loadedTemplate{}, loaderr.LocateJSON(nest(err, "tileset"), source, locData, fix)
			}
			c.m.Tilesets = append(c.m.Tilesets, ts)
			lt.ts = ts
		}
	}

	c.templates = append(c.templates, lt)
	return lt, nil
}

//go:noinline
func idxPath(prefix string, i int, suffix string) string {
	s := prefix + "[" + strconv.Itoa(i) + "]"
	if suffix != "" {
		s += "." + suffix
	}
	return s
}

//go:noinline
func idxPath2(p1 string, i1 int, p2 string, i2 int, suffix string) string {
	return idxPath(idxPath(p1, i1, p2), i2, suffix)
}

//go:noinline
func tiledErr(line, col int, field, problem, expected, fix string, err error) error {
	return &loaderr.Error{Line: line, Column: col, Field: field, Problem: problem, Expected: expected, Fix: fix, Err: err}
}

func buildTiledMap(raw tiledMap, resolve TilesetLoader) (*Tilemap, error) {
	orientation, err := parseTiledOrientation(raw.Orientation)
	if err != nil {
		return nil, tiledErr(raw.line, raw.col, "orientation", err.Error(), `"orthogonal", "isometric", "staggered" or "hexagonal"`, "set the map's orientation to one of those in Tiled's map properties", err)
	}
	staggerAxis, err := parseTiledStaggerAxis(raw.StaggerAxis)
	if err != nil {
		return nil, tiledErr(raw.line, raw.col, "staggeraxis", err.Error(), `"x" or "y"`, "set the map's stagger axis to x or y in Tiled", err)
	}
	staggerIndex, err := parseTiledStaggerIndex(raw.StaggerIndex)
	if err != nil {
		return nil, tiledErr(raw.line, raw.col, "staggerindex", err.Error(), `"odd" or "even"`, "set the map's stagger index to odd or even in Tiled", err)
	}
	if raw.Width < 0 || raw.Height < 0 {
		field := "width"
		if raw.Width >= 0 {
			field = "height"
		}
		return nil, tiledErr(raw.line, raw.col, field, fmt.Sprintf("map size %dx%d must be non-negative", raw.Width, raw.Height), "", "give the map a width and height of 0 or more tiles", nil)
	}
	if raw.Width > 0 && raw.Height > 0 && raw.Width > math.MaxInt/raw.Height {
		return nil, tiledErr(raw.line, raw.col, "width", fmt.Sprintf("map size %dx%d overflows", raw.Width, raw.Height), "", "make the map smaller", nil)
	}
	if raw.TileWidth <= 0 || raw.TileHeight <= 0 {
		field := "tilewidth"
		if raw.TileWidth > 0 {
			field = "tileheight"
		}
		return nil, tiledErr(raw.line, raw.col, field, fmt.Sprintf("tile size %gx%g must be positive", raw.TileWidth, raw.TileHeight), "", "give the map a tile size above 0 in Tiled's map properties", nil)
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

	ctx := tiledLoadCtx{
		m:       m,
		resolve: resolve,
		budget:  maxMapDecompressedBytes,
		rawTS:   raw.Tilesets,
	}

	for i, ref := range raw.Tilesets {
		if ref.FirstGID == 0 {
			return nil, tiledErr(ref.line, ref.col, idxPath("tilesets", i, "firstgid"), "tileset "+strconv.Itoa(i)+" has firstgid 0", "", "save or export the map again from Tiled; a tileset's firstgid is 1 or more", nil)
		}
		if ref.Source != "" {
			ts, err := loadExternalTileset(m, ref.FirstGID, ref.Source, ref.line, ref.col, idxPath("tilesets", i, "source"), resolve)
			if err != nil {
				return nil, err
			}
			m.Tilesets = append(m.Tilesets, ts)
			continue
		}

		ts, err := buildTileset(m, ref.FirstGID, ref.tiledTileset, "", resolve)
		if err != nil {
			return nil, nest(err, idxPath("tilesets", i, ""))
		}
		m.Tilesets = append(m.Tilesets, ts)
	}
	slices.SortFunc(m.Tilesets, func(a, b *Tileset) int {
		return cmp.Compare(a.FirstGID, b.FirstGID)
	})

	if err := loadTiledLayers(&ctx, raw.Layers, 0, 0, 1, 1, 1, 0, false, true); err != nil {
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

func parseObjectAlignment(s string) (ObjectAlignment, bool) {
	switch s {
	case "", "unspecified":
		return ObjectAlignmentUnspecified, true
	case "topleft":
		return ObjectAlignmentTopLeft, true
	case "top":
		return ObjectAlignmentTop, true
	case "topright":
		return ObjectAlignmentTopRight, true
	case "left":
		return ObjectAlignmentLeft, true
	case "center":
		return ObjectAlignmentCenter, true
	case "right":
		return ObjectAlignmentRight, true
	case "bottomleft":
		return ObjectAlignmentBottomLeft, true
	case "bottom":
		return ObjectAlignmentBottom, true
	case "bottomright":
		return ObjectAlignmentBottomRight, true
	default:
		return 0, false
	}
}

// buildTileset builds the tileset raw. The fields its errors name are relative to the tileset.
func buildTileset(m *Tilemap, firstGID uint32, raw tiledTileset, baseDir string, resolve TilesetLoader) (*Tileset, error) {
	align, ok := parseObjectAlignment(raw.ObjectAlignment)
	if !ok {
		return nil, &loaderr.Error{
			Line:     int(raw.line),
			Column:   int(raw.col),
			Field:    "objectalignment",
			Problem:  fmt.Sprintf("unknown objectalignment %q", raw.ObjectAlignment),
			Expected: `"unspecified", "topleft", "top", "topright", "left", "center", "right", "bottomleft", "bottom" or "bottomright"`,
			Fix:      "set the tileset's Object Alignment in Tiled to one of the supported values",
		}
	}
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
		imgLine, imgCol := int(raw.imageLine), int(raw.imageCol)
		if imgLine == 0 {
			imgLine, imgCol = int(raw.line), int(raw.col)
		}
		var err error
		img, err = resolveTiledImage(raw.Image, baseDir, resolve, imgLine, imgCol, "image", "", "check that the tileset image exists at this path, relative to the tileset")
		if err != nil {
			return nil, err
		}
	}

	props, err := parseProperties(raw.Properties)
	if err != nil {
		return nil, nest(err, "properties")
	}

	var offX, offY float32
	if raw.TileOffset != nil {
		offX, offY = raw.TileOffset.X, raw.TileOffset.Y
	}
	ts := &Tileset{
		Name:            raw.Name,
		FirstGID:        firstGID,
		Image:           img,
		TileWidth:       tw,
		TileHeight:      th,
		TileOffsetX:     offX,
		TileOffsetY:     offY,
		ObjectAlignment: align,
		Columns:         cols,
		TileCount:       raw.TileCount,
		Spacing:         raw.Spacing,
		Margin:          raw.Margin,
		Properties:      props,
	}

	for ti, tile := range raw.Tiles {
		if tile.Probability != nil {
			p := *tile.Probability
			if p < 0 || math.IsNaN(p) || math.IsInf(p, 0) {
				return nil, tiledErr(0, 0, idxPath("tiles", ti, "probability"), "invalid tile probability", "", "give the tile a probability of 0 or more in Tiled", nil)
			}
			if ts.TileProbabilities == nil {
				ts.TileProbabilities = make(map[uint32]float64)
			}
			ts.TileProbabilities[tile.ID] = p
		}
		if tile.Image != "" {
			imgLine, imgCol := int(tile.imageLine), int(tile.imageCol)
			if imgLine == 0 {
				imgLine, imgCol = int(tile.line), int(tile.col)
			}
			timg, err := resolveTiledImage(tile.Image, baseDir, resolve, imgLine, imgCol, idxPath("tiles", ti, "image"), "", "check that the tile image exists at this path, relative to the tileset")
			if err != nil {
				return nil, err
			}
			w, h := tile.Width, tile.Height
			if w <= 0 {
				w = tile.ImageWidth
			}
			if h <= 0 {
				h = tile.ImageHeight
			}
			if ts.TileImages == nil {
				ts.TileImages = make(map[uint32]TileImage)
			}
			ts.TileImages[tile.ID] = TileImage{
				Image:  timg,
				X:      tile.X,
				Y:      tile.Y,
				Width:  w,
				Height: h,
			}
		}
		if len(tile.Properties) > 0 {
			tp, err := parseProperties(tile.Properties)
			if err != nil {
				return nil, nest(err, idxPath("tiles", ti, "properties"))
			}
			if ts.TileProperties == nil {
				ts.TileProperties = make(map[uint32]Properties)
			}
			ts.TileProperties[tile.ID] = tp
			if flags := flagsFromProperties(tp); flags != 0 {
				if sum := uint64(firstGID) + uint64(tile.ID); sum <= uint64(TileIDMask) {
					m.SetFlags(uint32(sum), flags)
				}
			}
		}
		if len(tile.Animation) > 0 {
			frames := make([]TileAnimFrame, len(tile.Animation))
			for j, f := range tile.Animation {
				if f.Duration < 0 || int64(f.Duration) > math.MaxInt64/int64(time.Millisecond) {
					return nil, tiledErr(int(f.line), int(f.col), idxPath2("tiles", ti, "animation", j, "duration"), "invalid animation frame duration", "", "give the animation frame a duration of 0 or more milliseconds in Tiled", nil)
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
		if tile.ObjectGroup != nil {
			group, err := parseTileCollisionGroup(tile.ObjectGroup)
			if err != nil {
				return nil, nest(err, idxPath("tiles", ti, "objectgroup"))
			}
			if len(group.Objects) > 0 || group.Name != "" || len(group.Properties) > 0 {
				if ts.TileCollisions == nil {
					ts.TileCollisions = make(map[uint32]*ObjectLayer)
				}
				g := group
				ts.TileCollisions[tile.ID] = &g
			}
			if len(group.Objects) > 0 {
				if shapes := tileShapesFromObjects(group.Objects); len(shapes) > 0 {
					if ts.Shapes == nil {
						ts.Shapes = make(map[uint32][]TileShape)
					}
					ts.Shapes[tile.ID] = shapes
				}
			}
		}
	}
	if len(raw.WangSets) > 0 {
		wsets, err := parseTiledWangSets(raw.WangSets, firstGID, ts.TileProbabilities)
		if err != nil {
			return nil, err
		}
		ts.WangSets = wsets
	}
	return ts, nil
}

// parseTileCollisionGroup converts a tileset tile's Tiled objectgroup into an ObjectLayer,
// preserving object IDs, names, types, transforms, shapes, visibility, and custom properties.
func parseTileCollisionGroup(raw *tiledLayer) (ObjectLayer, error) {
	props, err := parseProperties(raw.Properties)
	if err != nil {
		return ObjectLayer{}, nest(err, "properties")
	}
	opacity := float32(1)
	if raw.Opacity != nil {
		opacity = *raw.Opacity
	}
	visible := true
	if raw.Visible != nil {
		visible = *raw.Visible
	}
	px := float32(1)
	if raw.ParallaxX != nil {
		px = *raw.ParallaxX
	}
	py := float32(1)
	if raw.ParallaxY != nil {
		py = *raw.ParallaxY
	}
	var tint core.Color
	tinted := false
	if raw.TintColor != "" {
		c, ok := parseTiledColor(raw.TintColor)
		if !ok {
			line, col := raw.pos()
			return ObjectLayer{}, tiledErr(line, col, "tintcolor", "invalid tintcolor "+strconv.Quote(raw.TintColor), `"#RRGGBB" or "#AARRGGBB"`, "set a valid hex tintcolor on the tile collision group in Tiled", nil)
		}
		tint = c
		tinted = true
	}
	var objs []Object
	if len(raw.Objects) > 0 {
		objs = make([]Object, len(raw.Objects))
		for i, ro := range raw.Objects {
			op, err := parseProperties(ro.Properties)
			if err != nil {
				return ObjectLayer{}, nest(err, idxPath("objects", i, "properties"))
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
				Width:      float32Val(ro.Width),
				Height:     float32Val(ro.Height),
				Rotation:   float32Val(ro.Rotation),
				GID:        ro.GID,
				Visible:    objVisible,
				Properties: op,
			}
			switch {
			case ro.GID != 0:
				obj.Shape = ObjectTile
			case ro.Text != nil:
				txt, err := parseTiledText(ro.Text)
				if err != nil {
					return ObjectLayer{}, nest(err, idxPath("objects", i, "text"))
				}
				obj.Shape = ObjectText
				obj.Text = txt
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
	}
	return ObjectLayer{
		Name:       raw.Name,
		OffsetX:    raw.OffsetX,
		OffsetY:    raw.OffsetY,
		ParallaxX:  px,
		ParallaxY:  py,
		Opacity:    opacity,
		TintColor:  tint,
		Tinted:     tinted,
		Visible:    visible,
		Objects:    objs,
		Properties: props,
	}, nil
}

func parseTiledWangSets(raw []tiledWangSet, firstGID uint32, tileProbs map[uint32]float64) ([]WangSet, error) {
	out := make([]WangSet, len(raw))
	for wi, rws := range raw {
		var typ WangSetType
		switch rws.Type {
		case "corner":
			typ = WangCorner
		case "edge":
			typ = WangEdge
		case "mixed":
			typ = WangMixed
		default:
			return nil, tiledErr(0, 0, idxPath("wangsets", wi, "type"), "unsupported wang set type "+rws.Type, `"corner", "edge" or "mixed"`, "set the Wang set type to corner, edge or mixed in Tiled", nil)
		}
		if len(rws.Colors) > 255 {
			return nil, tiledErr(0, 0, idxPath("wangsets", wi, "colors"), "wang set exceeds 255 colors", "", "keep a Wang set to at most 255 colors in Tiled", nil)
		}
		wsProps, err := parseProperties(rws.Properties)
		if err != nil {
			return nil, nest(err, idxPath("wangsets", wi, "properties"))
		}
		wsTile := -1
		if rws.Tile != nil {
			wsTile = *rws.Tile
		}
		colors := make([]WangColor, len(rws.Colors))
		for ci, rc := range rws.Colors {
			var col core.Color
			if rc.Color != "" {
				parsed, ok := parseTiledColor(rc.Color)
				if !ok {
					return nil, tiledErr(0, 0, idxPath2("wangsets", wi, "colors", ci, "color"), "invalid wang color "+rc.Color, "#RRGGBB or #AARRGGBB", "set a valid hex color on the terrain in Tiled", nil)
				}
				col = parsed
			}
			prob := 1.0
			zeroProb := false
			if rc.Probability != nil {
				p := *rc.Probability
				if p < 0 || math.IsNaN(p) || math.IsInf(p, 0) {
					return nil, tiledErr(0, 0, idxPath2("wangsets", wi, "colors", ci, "probability"), "invalid wang color probability", "", "give the terrain a probability of 0 or more in Tiled", nil)
				}
				prob = p
				zeroProb = p == 0
			}
			cProps, err := parseProperties(rc.Properties)
			if err != nil {
				return nil, nest(err, idxPath2("wangsets", wi, "colors", ci, "properties"))
			}
			cTile := -1
			if rc.Tile != nil {
				cTile = *rc.Tile
			}
			colors[ci] = WangColor{
				Name:            rc.Name,
				Color:           col,
				Tile:            cTile,
				Probability:     prob,
				Class:           rc.Class,
				Properties:      cProps,
				zeroProbability: zeroProb,
			}
		}

		wtiles := make([]WangTile, len(rws.WangTiles))
		for wti, rwt := range rws.WangTiles {
			if len(rwt.WangID) != 8 {
				return nil, tiledErr(0, 0, idxPath2("wangsets", wi, "wangtiles", wti, "wangid"), "wangid must have 8 entries", "8 entries [Top, TopRight, Right, BottomRight, Bottom, BottomLeft, Left, TopLeft]", "export the tileset again from Tiled so each wangtile has an 8-element wangid", nil)
			}
			var wid WangID
			for si, v := range rwt.WangID {
				if v < 0 || v > 255 {
					return nil, tiledErr(0, 0, idxPath2("wangsets", wi, "wangtiles", wti, "wangid"), "wangid index out of range 0..255", "", "use terrain color indices between 0 and 255 in Tiled", nil)
				}
				if len(colors) > 0 && v > len(colors) {
					return nil, tiledErr(0, 0, idxPath2("wangsets", wi, "wangtiles", wti, "wangid"), "wangid index exceeds color count", "", "keep every wangid index within the Wang set's colors list in Tiled", nil)
				}
				wid[si] = uint8(v)
			}
			prob := 1.0
			zeroProb := false
			if p, ok := tileProbs[rwt.TileID]; ok {
				prob = p
				zeroProb = p == 0
			}
			wtiles[wti] = WangTile{
				TileID:          rwt.TileID,
				WangID:          wid,
				Probability:     prob,
				zeroProbability: zeroProb,
			}
		}

		out[wi] = WangSet{
			Name:       rws.Name,
			Type:       typ,
			Tile:       wsTile,
			Class:      rws.Class,
			FirstGID:   firstGID,
			Colors:     colors,
			WangTiles:  wtiles,
			Properties: wsProps,
		}
	}
	return out, nil
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

func float32Val(p *float32) float32 {
	if p == nil {
		return 0
	}
	return *p
}

func boolVal(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func parseTiledText(rt *tiledText) (*TextObject, error) {
	if rt == nil {
		return nil, nil
	}
	col := core.RGB(0x000000)
	if rt.Color != "" {
		c, ok := parseTiledColor(rt.Color)
		if !ok {
			return nil, tiledErr(rt.line, rt.col, "color", fmt.Sprintf("invalid text color %q", rt.Color), `"#RRGGBB" or "#AARRGGBB"`, "set a valid hex color on the text object in Tiled", nil)
		}
		col = c
	}
	halign := rt.HAlign
	if halign == "" {
		halign = "left"
	}
	switch halign {
	case "left", "center", "right", "justify":
	default:
		return nil, tiledErr(rt.line, rt.col, "halign", fmt.Sprintf("invalid halign %q", halign), `"left", "center", "right" or "justify"`, "set a valid horizontal alignment on the text object in Tiled", nil)
	}
	valign := rt.VAlign
	if valign == "" {
		valign = "top"
	}
	switch valign {
	case "top", "center", "bottom":
	default:
		return nil, tiledErr(rt.line, rt.col, "valign", fmt.Sprintf("invalid valign %q", valign), `"top", "center" or "bottom"`, "set a valid vertical alignment on the text object in Tiled", nil)
	}
	font := rt.FontFamily
	if font == "" {
		font = "sans-serif"
	}
	px := rt.PixelSize
	if px <= 0 {
		px = 16
	}
	kerning := true
	if rt.Kerning != nil {
		kerning = *rt.Kerning
	}
	return &TextObject{
		Text:       strVal(rt.Text),
		FontFamily: font,
		PixelSize:  px,
		Wrap:       boolVal(rt.Wrap),
		Color:      col,
		Bold:       boolVal(rt.Bold),
		Italic:     boolVal(rt.Italic),
		Underline:  boolVal(rt.Underline),
		Strikeout:  boolVal(rt.Strikeout),
		Kerning:    kerning,
		HAlign:     halign,
		VAlign:     valign,
	}, nil
}

func mergeTiledText(base, over *tiledText) *tiledText {
	if over == nil {
		return base
	}
	if base == nil {
		return over
	}
	m := *base
	if over.Text != nil {
		m.Text = over.Text
	}
	if over.FontFamily != "" {
		m.FontFamily = over.FontFamily
	}
	if over.PixelSize > 0 {
		m.PixelSize = over.PixelSize
	}
	if over.Wrap != nil {
		m.Wrap = over.Wrap
	}
	if over.Color != "" {
		m.Color = over.Color
	}
	if over.Bold != nil {
		m.Bold = over.Bold
	}
	if over.Italic != nil {
		m.Italic = over.Italic
	}
	if over.Underline != nil {
		m.Underline = over.Underline
	}
	if over.Strikeout != nil {
		m.Strikeout = over.Strikeout
	}
	if over.Kerning != nil {
		m.Kerning = over.Kerning
	}
	if over.HAlign != "" {
		m.HAlign = over.HAlign
	}
	if over.VAlign != "" {
		m.VAlign = over.VAlign
	}
	m.line, m.col = over.line, over.col
	return &m
}

// loadTiledLayers adds layers to ctx.m. The fields its errors name are relative to the array of layers, as in "[2].data".
func loadTiledLayers(ctx *tiledLoadCtx, layers []tiledLayer, parentOX, parentOY, parentPX, parentPY, parentOpacity float32, parentTint core.Color, parentTinted bool, parentVisible bool) error {
	m := ctx.m
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
		tint, tinted := parentTint, parentTinted
		if raw.TintColor != "" {
			c, ok := parseTiledColor(raw.TintColor)
			if !ok {
				ll, lc := raw.pos()
				return tiledErr(ll, lc, idxPath("", li, "tintcolor"), fmt.Sprintf("layer %q has invalid tintcolor %q", raw.Name, raw.TintColor), `"#RRGGBB" or "#AARRGGBB"`, "set a valid hex tint color on the layer in Tiled", nil)
			}
			tint, tinted = multiplyTint(tint, tinted, c, true)
		}

		props, err := parseProperties(raw.Properties)
		if err != nil {
			return nest(err, idxPath("", li, "properties"))
		}

		switch raw.Type {
		case "tilelayer":
			if err := validateLayerEncoding(raw, li); err != nil {
				return err
			}
			if m.Infinite || len(raw.Chunks) > 0 {
				layer, err := buildChunkedTileLayer(raw, li, ox, oy, px, py, opacity, tint, tinted, visible, props, &ctx.budget)
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
				field := idxPath("", li, "width")
				if lw >= 0 {
					field = idxPath("", li, "height")
				}
				ll, lc := raw.pos()
				return tiledErr(ll, lc, field, fmt.Sprintf("layer %q size %dx%d must be non-negative", raw.Name, lw, lh), "", "give the layer a width and height of 0 or more tiles", nil)
			}
			if lw == 0 {
				lw = m.Width
			}
			if lh == 0 {
				lh = m.Height
			}
			if lw > 0 && lh > 0 && lw > math.MaxInt/lh {
				ll, lc := raw.pos()
				return tiledErr(ll, lc, idxPath("", li, "width"), fmt.Sprintf("layer %q size %dx%d overflows", raw.Name, lw, lh), "", "make the layer smaller", nil)
			}

			data, err := decodeFiniteLayerData(raw, li, lw, lh, &ctx.budget)
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
				TintColor:  tint,
				Tinted:     tinted,
				Visible:    visible,
				YSort:      ySortFromProperties(props),
				Properties: props,
			})

		case "imagelayer":
			var img core.ImageSource
			if raw.Image != "" {
				var imgLine, imgCol int
				if raw.xml != nil {
					imgLine, imgCol = raw.xml.imageLine, raw.xml.imageCol
					if imgLine == 0 {
						imgLine, imgCol = raw.xml.line, raw.xml.col
					}
				}
				img, err = resolveTiledImage(raw.Image, "", ctx.resolve, imgLine, imgCol, idxPath("", li, "image"), "image layer "+raw.Name+" ", "check that the layer's image exists at this path")
				if err != nil {
					return err
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
				TintColor:  tint,
				Tinted:     tinted,
				Visible:    visible,
				Properties: props,
			})

		case "objectgroup":
			objs := make([]Object, len(raw.Objects))
			for i, ro := range raw.Objects {
				op, err := parseProperties(ro.Properties)
				if err != nil {
					return nest(err, idxPath2("", li, "objects", i, "properties"))
				}
				var tmpl loadedTemplate
				hasTmpl := ro.Template != ""
				if hasTmpl {
					tmpl, err = ctx.loadTemplate(ro.Template, int(ro.line), int(ro.col), idxPath2("", li, "objects", i, "template"))
					if err != nil {
						return err
					}
					if len(tmpl.props) > 0 {
						merged := make(Properties, len(tmpl.props)+len(op))
						for k, v := range tmpl.props {
							merged[k] = v
						}
						for k, v := range op {
							merged[k] = v
						}
						op = merged
					}
				}

				objVisible := true
				if hasTmpl && tmpl.obj.Visible != nil {
					objVisible = *tmpl.obj.Visible
				}
				if ro.Visible != nil {
					objVisible = *ro.Visible
				}

				name := ro.Name
				if name == "" && hasTmpl {
					name = tmpl.obj.Name
				}
				typ := ro.Type
				if typ == "" {
					typ = ro.Class
				}
				if typ == "" && hasTmpl {
					typ = tmpl.obj.Type
					if typ == "" {
						typ = tmpl.obj.Class
					}
				}
				var w, h, rot float32
				if hasTmpl {
					w = float32Val(tmpl.obj.Width)
					h = float32Val(tmpl.obj.Height)
					rot = float32Val(tmpl.obj.Rotation)
				}
				if ro.Width != nil {
					w = *ro.Width
				}
				if ro.Height != nil {
					h = *ro.Height
				}
				if ro.Rotation != nil {
					rot = *ro.Rotation
				}
				gid := ro.GID
				if gid == 0 && hasTmpl && tmpl.obj.GID != 0 {
					gid = tmpl.obj.GID
					if tmpl.ts != nil {
						flags := gid & FlipMask
						rawID := gid & TileIDMask
						if rawID >= tmpl.tmplFirstGID {
							remapped := uint64(tmpl.ts.FirstGID) + uint64(rawID-tmpl.tmplFirstGID)
							if remapped == 0 || remapped > uint64(TileIDMask) {
								return tiledErr(int(ro.line), int(ro.col), idxPath2("", li, "objects", i, "gid"), "remapped template gid overflows tile ID range", "", "reduce tileset tile counts or firstgid values in Tiled", nil)
							}
							gid = flags | uint32(remapped)
						}
					}
				}

				var rawText *tiledText
				if hasTmpl {
					rawText = mergeTiledText(tmpl.obj.Text, ro.Text)
				} else {
					rawText = ro.Text
				}

				obj := Object{
					ID:         ro.ID,
					Name:       name,
					Type:       typ,
					X:          ro.X,
					Y:          ro.Y,
					Width:      w,
					Height:     h,
					Rotation:   rot,
					GID:        gid,
					Visible:    objVisible,
					Properties: op,
				}
				switch {
				case gid != 0:
					obj.Shape = ObjectTile
				case rawText != nil:
					txt, err := parseTiledText(rawText)
					if err != nil {
						return nest(err, idxPath2("", li, "objects", i, "text"))
					}
					obj.Shape = ObjectText
					obj.Text = txt
				case ro.Point || (hasTmpl && tmpl.obj.Point):
					obj.Shape = ObjectPoint
				case ro.Ellipse || (hasTmpl && tmpl.obj.Ellipse):
					obj.Shape = ObjectEllipse
				case len(ro.Polygon) > 0:
					obj.Shape = ObjectPolygon
					obj.Points = convertPoints(ro.Polygon)
				case len(ro.Polyline) > 0:
					obj.Shape = ObjectPolyline
					obj.Points = convertPoints(ro.Polyline)
				case hasTmpl && len(tmpl.obj.Polygon) > 0:
					obj.Shape = ObjectPolygon
					obj.Points = convertPoints(tmpl.obj.Polygon)
				case hasTmpl && len(tmpl.obj.Polyline) > 0:
					obj.Shape = ObjectPolyline
					obj.Points = convertPoints(tmpl.obj.Polyline)
				default:
					obj.Shape = ObjectRect
				}
				objs[i] = obj
			}
			m.addObjectLayer(ObjectLayer{
				Name:       raw.Name,
				OffsetX:    ox,
				OffsetY:    oy,
				ParallaxX:  px,
				ParallaxY:  py,
				Opacity:    opacity,
				TintColor:  tint,
				Tinted:     tinted,
				Visible:    visible,
				Objects:    objs,
				Properties: props,
			})

		case "group":
			if err := loadTiledLayers(ctx, raw.Layers, ox, oy, px, py, opacity, tint, tinted, visible); err != nil {
				return nest(err, idxPath("", li, "layers"))
			}
		}
	}
	return nil
}

func decodeFiniteLayerData(raw tiledLayer, li, lw, lh int, budget *int64) ([]uint32, error) {
	var isXML, hasXMLData bool
	var xmlText string
	var xmlTiles []uint32
	var dataLine, dataCol int
	if raw.xml != nil {
		isXML = true
		hasXMLData = raw.xml.hasData
		xmlText = raw.xml.dataText
		xmlTiles = raw.xml.tiles
		dataLine, dataCol = raw.xml.dataLine, raw.xml.dataCol
	}
	return decodeTiledTileData(
		raw.Encoding, raw.Compression, isXML, hasXMLData,
		xmlText, xmlTiles, raw.Data,
		lw, lh, budget,
		idxPath("", li, "data"),
		"layer "+raw.Name,
		dataLine, dataCol, false,
	)
}

func decodeChunkTileData(raw tiledLayer, rc tiledChunk, li, ci int, budget *int64) ([]uint32, error) {
	label := "layer " + raw.Name + " chunk " + strconv.Itoa(ci)
	var isXML bool
	var xmlText string
	var xmlTiles []uint32
	var line, col int
	if rc.xml != nil {
		isXML = true
		xmlText = rc.xml.dataText
		xmlTiles = rc.xml.tiles
		line, col = rc.xml.line, rc.xml.col
	}
	if rc.Width > math.MaxInt/rc.Height {
		return nil, tiledErr(line, col, idxPath2("", li, "chunks", ci, ""), fmt.Sprintf("%s size %dx%d overflows", label, rc.Width, rc.Height), "", "make the chunk smaller in Tiled", nil)
	}
	return decodeTiledTileData(
		raw.Encoding, raw.Compression, isXML, true,
		xmlText, xmlTiles, rc.Data,
		rc.Width, rc.Height, budget,
		idxPath2("", li, "chunks", ci, "data"),
		label, line, col, true,
	)
}

func decodeTiledTileData(encoding, compression string, isXML, hasXMLData bool, xmlText string, xmlTiles []uint32, rawJSON json.RawMessage, w, h int, budget *int64, field, label string, line, col int, isChunk bool) ([]uint32, error) {
	expected := w * h
	var data []uint32
	var err error
	if isXML {
		if !hasXMLData {
			return nil, nil
		}
		switch encoding {
		case "base64":
			return decodeBase64TileData(xmlText, compression, expected, budget, field, line, col, label)
		case "csv":
			data, err = decodeCSVTileData(xmlText, field, line, col, label)
			if err != nil {
				return nil, err
			}
		case "", "xml":
			data = xmlTiles
		}
	} else {
		if !isChunk && len(rawJSON) == 0 {
			return nil, nil
		}
		if encoding == "base64" {
			var b64 string
			if err = json.Unmarshal(rawJSON, &b64); err != nil {
				return nil, loaderr.Errorf(field, "export the map again from Tiled; base64 tile data must be a JSON string", "%s data: %w", label, err)
			}
			return decodeBase64TileData(b64, compression, expected, budget, field, 0, 0, label)
		}
		trimmed := bytes.TrimSpace(rawJSON)
		if len(trimmed) > 0 && trimmed[0] == '"' && encoding == "csv" {
			var csvStr string
			if err = json.Unmarshal(rawJSON, &csvStr); err != nil {
				return nil, loaderr.Errorf(field, "export the map again from Tiled; data is an array of tile GIDs", "%s data: %w", label, err)
			}
			data, err = decodeCSVTileData(csvStr, field, 0, 0, label)
			if err != nil {
				return nil, err
			}
		} else if err = json.Unmarshal(rawJSON, &data); err != nil {
			return nil, loaderr.Errorf(field, "export the map again from Tiled; data is an array of tile GIDs", "%s data: %w", label, err)
		}
		line, col = 0, 0
	}
	if len(data) != expected {
		fix := "export the map again from Tiled so data has one GID per tile of the layer"
		if isChunk {
			fix = "export the map again from Tiled so each chunk's data has one GID per tile"
		}
		return nil, tiledErr(line, col, field, fmt.Sprintf("%s has %d tiles", label, len(data)), fmt.Sprintf("%d (%dx%d)", expected, w, h), fix, nil)
	}
	return data, nil
}

// buildChunkedTileLayer loads an infinite Tiled map's tile layer raw directly into a sparse chunked TileLayer,
// preserving true negative and positive chunk coordinates without flattening into a dense array.
func buildChunkedTileLayer(raw tiledLayer, li int, ox, oy, px, py, opacity float32, tint core.Color, tinted bool, visible bool, props Properties, budget *int64) (TileLayer, error) {
	type parsedChunk struct {
		x, y, w, h int
		data       []uint32
	}
	chunks := make([]parsedChunk, len(raw.Chunks))
	var minX, minY, maxX, maxY int64
	uniformChunkSize := 0
	uniform := len(raw.Chunks) > 0

	for i, rc := range raw.Chunks {
		if rc.Width <= 0 || rc.Height <= 0 {
			var line, col int
			if rc.xml != nil {
				line, col = rc.xml.line, rc.xml.col
			}
			return TileLayer{}, tiledErr(line, col, idxPath2("", li, "chunks", i, ""), fmt.Sprintf("layer %q chunk %d has non-positive size %dx%d", raw.Name, i, rc.Width, rc.Height), "", "save or export the map again from Tiled; a chunk's width and height are above 0", nil)
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
	layer.StartX = raw.StartX
	layer.StartY = raw.StartY
	if len(chunks) > 0 {
		if minX >= math.MinInt32 && maxX <= math.MaxInt32 && maxX-minX <= math.MaxInt32 {
			layer.Width = int(maxX - minX)
			layer.StartX = int(minX)
		}
		if minY >= math.MinInt32 && maxY <= math.MaxInt32 && maxY-minY <= math.MaxInt32 {
			layer.Height = int(maxY - minY)
			layer.StartY = int(minY)
		}
	}
	layer.OffsetX = ox
	layer.OffsetY = oy
	layer.ParallaxX = px
	layer.ParallaxY = py
	layer.Opacity = opacity
	layer.TintColor = tint
	layer.Tinted = tinted
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
			tn := typeName(rp.Type)
			var line, col int
			if rp.xml != nil {
				line, col = rp.xml.line, rp.xml.col
			}
			return tiledErr(line, col, idxPath("", pi, "value"), "property "+rp.Name+" ("+tn+"): "+err.Error(), "", "give the property a value of its type, "+tn+", in Tiled", err)
		}
		typ := rp.Type
		if typ == "" {
			typ = "string"
		}
		var val any
		if rp.xml != nil {
			xmlValue := rp.xml.value
			switch typ {
			case "string", "color", "file":
				val = xmlValue
			case "int", "object":
				n, err := strconv.Atoi(strings.TrimSpace(xmlValue))
				if err != nil {
					return nil, bad(err)
				}
				val = n
			case "float":
				f, err := strconv.ParseFloat(strings.TrimSpace(xmlValue), 64)
				if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
					if err == nil {
						err = fmt.Errorf("non-finite float %q", xmlValue)
					}
					return nil, bad(err)
				}
				val = f
			case "bool":
				switch strings.TrimSpace(xmlValue) {
				case "true":
					val = true
				case "false":
					val = false
				default:
					return nil, bad(fmt.Errorf("invalid bool %q", xmlValue))
				}
			default:
				val = xmlValue
			}
			props[rp.Name] = Property{Name: rp.Name, Type: typ, Value: val}
			continue
		}
		switch typ {
		case "string", "color", "file":
			if s, ok := fastJSONString(rp.Value); ok {
				val = s
			} else {
				var s string
				if err := json.Unmarshal(rp.Value, &s); err != nil {
					return nil, bad(err)
				}
				val = s
			}
		case "int", "object":
			if n, ok := fastJSONInt(rp.Value); ok {
				val = n
			} else {
				var n int
				if err := json.Unmarshal(rp.Value, &n); err != nil {
					var f float64
					if err2 := json.Unmarshal(rp.Value, &f); err2 != nil {
						return nil, bad(err)
					}
					n = int(f)
				}
				val = n
			}
		case "float":
			if f, ok := fastJSONFloat(rp.Value); ok {
				val = f
			} else {
				var f float64
				if err := json.Unmarshal(rp.Value, &f); err != nil {
					return nil, bad(err)
				}
				val = f
			}
		case "bool":
			trimmed := bytes.TrimSpace(rp.Value)
			switch {
			case len(trimmed) == 4 && string(trimmed) == "true":
				val = true
			case len(trimmed) == 5 && string(trimmed) == "false":
				val = false
			default:
				var b bool
				if err := json.Unmarshal(rp.Value, &b); err != nil {
					return nil, bad(err)
				}
				val = b
			}
		default:
			if err := json.Unmarshal(rp.Value, &val); err != nil {
				return nil, bad(err)
			}
		}
		props[rp.Name] = Property{Name: rp.Name, Type: typ, Value: val}
	}
	return props, nil
}

// nest returns err with the field path base put in front of the field it names, when err is an *loaderr.Error belonging to the current file, so a helper can name fields relative to the value it was handed and errors from external tilesets or templates keep their own file-relative field paths.
func nest(err error, base string) error {
	e, ok := err.(*loaderr.Error)
	if !ok || e == nil || e.File != "" {
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

// fastJSONString decodes a plain unescaped ASCII JSON string without invoking json.Unmarshal.
func fastJSONString(raw json.RawMessage) (string, bool) {
	b := bytes.TrimSpace(raw)
	if len(b) < 2 || b[0] != '"' || b[len(b)-1] != '"' {
		return "", false
	}
	inner := b[1 : len(b)-1]
	for _, c := range inner {
		if c < 0x20 || c >= 0x7f || c == '"' || c == '\\' {
			return "", false
		}
	}
	return string(inner), true
}

// fastJSONInt decodes a plain JSON integer without invoking json.Unmarshal.
func fastJSONInt(raw json.RawMessage) (int, bool) {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return 0, false
	}
	start := 0
	if b[0] == '-' {
		if len(b) == 1 {
			return 0, false
		}
		start = 1
	}
	if len(b)-start > 1 && b[start] == '0' {
		return 0, false
	}
	for _, c := range b[start:] {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(string(b))
	if err != nil {
		return 0, false
	}
	return n, true
}

// fastJSONFloat decodes a plain JSON number without invoking json.Unmarshal.
func fastJSONFloat(raw json.RawMessage) (float64, bool) {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return 0, false
	}
	c0 := b[0]
	if c0 != '-' && (c0 < '0' || c0 > '9') {
		return 0, false
	}
	for _, c := range b {
		if (c < '0' || c > '9') && c != '-' && c != '+' && c != '.' && c != 'e' && c != 'E' {
			return 0, false
		}
	}
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
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
	Name            string          `json:"name"`
	Image           string          `json:"image"`
	ImageWidth      float32         `json:"imagewidth"`
	TileWidth       float32         `json:"tilewidth"`
	TileHeight      float32         `json:"tileheight"`
	TileOffset      *tiledPoint     `json:"tileoffset"`
	ObjectAlignment string          `json:"objectalignment"`
	Columns         int             `json:"columns"`
	TileCount       int             `json:"tilecount"`
	Spacing         float32         `json:"spacing"`
	Margin          float32         `json:"margin"`
	Properties      []tiledProperty `json:"properties"`
	Tiles           []tiledTile     `json:"tiles"`
	WangSets        []tiledWangSet  `json:"wangsets"`
	line            int32
	col             int32
	imageLine       int32
	imageCol        int32
}

type tiledTile struct {
	Type        string               `json:"type"`
	Class       string               `json:"class"`
	Image       string               `json:"image"`
	Probability *float64             `json:"probability"`
	ObjectGroup *tiledLayer          `json:"objectgroup"`
	Properties  []tiledProperty      `json:"properties"`
	Animation   []tiledAnimFrameJSON `json:"animation"`
	ID          uint32               `json:"id"`
	ImageWidth  float32              `json:"imagewidth"`
	ImageHeight float32              `json:"imageheight"`
	X           float32              `json:"x"`
	Y           float32              `json:"y"`
	Width       float32              `json:"width"`
	Height      float32              `json:"height"`
	line        int32
	col         int32
	imageLine   int32
	imageCol    int32
}

type tiledWangSet struct {
	Name       string           `json:"name"`
	Type       string           `json:"type"`
	Class      string           `json:"class"`
	Tile       *int             `json:"tile"`
	Colors     []tiledWangColor `json:"colors"`
	WangTiles  []tiledWangTile  `json:"wangtiles"`
	Properties []tiledProperty  `json:"properties"`
}

type tiledWangColor struct {
	Name        string          `json:"name"`
	Color       string          `json:"color"`
	Tile        *int            `json:"tile"`
	Probability *float64        `json:"probability"`
	Class       string          `json:"class"`
	Properties  []tiledProperty `json:"properties"`
}

type tiledWangTile struct {
	TileID uint32 `json:"tileid"`
	WangID []int  `json:"wangid"`
}

type tiledTemplate struct {
	Type    string           `json:"type"`
	Tileset *tiledTilesetRef `json:"tileset"`
	Object  *tiledObject     `json:"object"`
}

type tiledText struct {
	Text       *string `json:"text"`
	FontFamily string  `json:"fontfamily"`
	PixelSize  int     `json:"pixelsize"`
	Wrap       *bool   `json:"wrap"`
	Color      string  `json:"color"`
	Bold       *bool   `json:"bold"`
	Italic     *bool   `json:"italic"`
	Underline  *bool   `json:"underline"`
	Strikeout  *bool   `json:"strikeout"`
	Kerning    *bool   `json:"kerning"`
	HAlign     string  `json:"halign"`
	VAlign     string  `json:"valign"`
	line       int
	col        int
}
type tiledAnimFrameJSON struct {
	Duration int    `json:"duration"`
	TileID   uint32 `json:"tileid"`
	line     int32
	col      int32
}

type xmlChunkMeta struct {
	dataText string
	tiles    []uint32
	line     int
	col      int
}

type tiledChunk struct {
	Data   json.RawMessage `json:"data"`
	xml    *xmlChunkMeta
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type xmlLayerMeta struct {
	dataText  string
	tiles     []uint32
	line      int
	col       int
	dataLine  int
	dataCol   int
	imageLine int
	imageCol  int
	hasData   bool
}

type tiledLayer struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	TintColor   string          `json:"tintcolor"`
	Encoding    string          `json:"encoding"`
	Compression string          `json:"compression"`
	Image       string          `json:"image"`
	Data        json.RawMessage `json:"data"`
	Chunks      []tiledChunk    `json:"chunks"`
	Objects     []tiledObject   `json:"objects"`
	Layers      []tiledLayer    `json:"layers"`
	Properties  []tiledProperty `json:"properties"`
	ParallaxX   *float32        `json:"parallaxx"`
	ParallaxY   *float32        `json:"parallaxy"`
	Opacity     *float32        `json:"opacity"`
	Visible     *bool           `json:"visible"`
	xml         *xmlLayerMeta
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	StartX      int     `json:"startx"`
	StartY      int     `json:"starty"`
	OffsetX     float32 `json:"offsetx"`
	OffsetY     float32 `json:"offsety"`
	ImageWidth  float32 `json:"imagewidth"`
	ImageHeight float32 `json:"imageheight"`
	RepeatX     bool    `json:"repeatx"`
	RepeatY     bool    `json:"repeaty"`
}

func (l *tiledLayer) pos() (int, int) {
	if l.xml == nil {
		return 0, 0
	}
	return l.xml.line, l.xml.col
}

type tiledObject struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Class      string          `json:"class"`
	Template   string          `json:"template"`
	Polygon    []tiledPoint    `json:"polygon"`
	Polyline   []tiledPoint    `json:"polyline"`
	Properties []tiledProperty `json:"properties"`
	Width      *float32        `json:"width"`
	Height     *float32        `json:"height"`
	Rotation   *float32        `json:"rotation"`
	Visible    *bool           `json:"visible"`
	Text       *tiledText      `json:"text"`
	ID         int             `json:"id"`
	X          float32         `json:"x"`
	Y          float32         `json:"y"`
	GID        uint32          `json:"gid"`
	line       int32
	col        int32
	Point      bool `json:"point"`
	Ellipse    bool `json:"ellipse"`
}

type tiledPoint struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

type xmlPropMeta struct {
	value string
	line  int
	col   int
}

type tiledProperty struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
	xml   *xmlPropMeta
}
