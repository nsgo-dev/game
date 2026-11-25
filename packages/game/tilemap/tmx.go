package tilemap

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

const (
	fixTiledXML   = "correct the map in Tiled and save it again as TMX (.tmx)"
	fixTilesetXML = "correct the tileset in Tiled and save it again as TSX (.tsx)"
	fixEncoding   = `in Tiled's map properties, set "Tile Layer Format" to CSV, Base64 (uncompressed), Base64 (zlib compressed), or Base64 (gzip compressed), and save again`
	fixZstd       = `in Tiled's map properties, change "Tile Layer Format" from zstd to Base64 (zlib compressed), Base64 (gzip compressed), Base64 (uncompressed), or CSV, and save again`

	// maxTiledLayerTiles caps a single tile layer or normalized chunk grid at 16,777,216 tiles (64 MiB of uint32 GIDs).
	maxTiledLayerTiles = 16 << 20
	// maxMapDecompressedBytes caps the total decompressed tile bytes across one LoadTiled call (64 MiB) to stop decompression bombs.
	maxMapDecompressedBytes int64 = 64 << 20
	// maxXMLDepth limits nested <group> elements so crafted XML cannot overflow the stack.
	maxXMLDepth = 64
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// isXML reports whether data begins with '<' after an optional UTF-8 BOM and ASCII whitespace.
func isXML(data []byte) bool {
	s := bytes.TrimPrefix(data, utf8BOM)
	for _, b := range s {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b == '<'
		}
	}
	return false
}

// xmlPos converts byte offsets in an XML document to 1-based line and column numbers in O(N) total time.
type xmlPos struct {
	data         []byte
	off          int
	line, column int
}

func newXMLPos(data []byte) xmlPos {
	return xmlPos{data: data, line: 1, column: 1}
}

func (p *xmlPos) at(off int) (int, int) {
	if off < p.off {
		p.off, p.line, p.column = 0, 1, 1
	}
	off = min(off, len(p.data))
	for p.off < off {
		if p.data[p.off] == '\n' {
			p.line++
			p.column = 1
			p.off++
			continue
		}
		_, size := utf8.DecodeRune(p.data[p.off:])
		p.off += size
		p.column++
	}
	return p.line, p.column
}

func xmlAttrErr(line, col int, field, expected, fix string, problem string, err error) error {
	return &loaderr.Error{
		Line:     line,
		Column:   col,
		Field:    field,
		Problem:  problem,
		Expected: expected,
		Fix:      fix,
		Err:      err,
	}
}

func xmlEOFErr(line, col int, field, tag string) error {
	return &loaderr.Error{
		Line:    line,
		Column:  col,
		Field:   field,
		Problem: "unexpected end of <" + tag + "> element",
		Fix:     loaderr.FixTruncated,
	}
}

func parseXMLInt(val string, line, col int, field, fix string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil {
		return 0, xmlAttrErr(line, col, field, "an integer", fix, "invalid integer "+strconv.Quote(val), err)
	}
	return n, nil
}

func parseXMLUint32(val string, line, col int, field, fix string) (uint32, error) {
	u, err := strconv.ParseUint(strings.TrimSpace(val), 10, 32)
	if err != nil {
		return 0, xmlAttrErr(line, col, field, "an unsigned 32-bit integer", fix, "invalid unsigned integer "+strconv.Quote(val), err)
	}
	return uint32(u), nil
}

func parseXMLFloat32(val string, line, col int, field, fix string) (float32, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(val), 32)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, xmlAttrErr(line, col, field, "a finite number", fix, "invalid number "+strconv.Quote(val), err)
	}
	return float32(f), nil
}

func parseXMLBoolAttr(val string, line, col int, field, fix string) (bool, error) {
	switch strings.TrimSpace(val) {
	case "1", "true":
		return true, nil
	case "0", "false":
		return false, nil
	default:
		return false, xmlAttrErr(line, col, field, `"0" or "1"`, fix, "invalid boolean "+strconv.Quote(val), nil)
	}
}

func parseXMLPropsChild(r *xmlReader, cl, cc int, dst *[]tiledProperty) error {
	p, err := parseXMLProperties(r, cl, cc)
	if err != nil {
		return nest(err, "properties")
	}
	*dst = append(*dst, p...)
	return nil
}

func parseXMLImageElem(r *xmlReader, t xmlStartElement, cl, cc int, fix string, img *string, w, h *float32, il, ic *int) error {
	*il, *ic = cl, cc
	for _, a := range t.Attr {
		var err error
		switch a.Name.Local {
		case "source":
			*img = a.Value
		case "width":
			if w != nil {
				*w, err = parseXMLFloat32(a.Value, cl, cc, "imagewidth", fix)
			}
		case "height":
			if h != nil {
				*h, err = parseXMLFloat32(a.Value, cl, cc, "imageheight", fix)
			}
		}
		if err != nil {
			return err
		}
	}
	return r.skip()
}

// parseXMLChildLayer handles layer/imagelayer/objectgroup/group/properties children shared by <map> and <group>.
func parseXMLChildLayer(r *xmlReader, t xmlStartElement, cl, cc, depth int, props *[]tiledProperty, layers *[]tiledLayer) (bool, error) {
	switch t.Name.Local {
	case "properties":
		return true, parseXMLPropsChild(r, cl, cc, props)
	case "layer", "imagelayer", "objectgroup", "group":
		idx := len(*layers)
		child, err := parseXMLLayer(r, t, cl, cc, depth)
		if err != nil {
			return true, nest(err, loaderr.Path("layers", idx))
		}
		*layers = append(*layers, child)
		return true, nil
	}
	return false, nil
}

func parseXMLTileElem(r *xmlReader, t xmlStartElement, cl, cc int) (uint32, error) {
	var gid uint32
	for _, a := range t.Attr {
		if a.Name.Local == "gid" {
			var err error
			gid, err = parseXMLUint32(a.Value, cl, cc, "data", fixTiledXML)
			if err != nil {
				return 0, err
			}
		}
	}
	return gid, r.skip()
}

func parseXMLPointsElem(r *xmlReader, t xmlStartElement, field string, cl, cc int) ([]tiledPoint, error) {
	var pts []tiledPoint
	for _, a := range t.Attr {
		if a.Name.Local == "points" {
			var err error
			pts, err = parseXMLPoints(a.Value, field, cl, cc)
			if err != nil {
				return nil, err
			}
		}
	}
	return pts, r.skip()
}

func parseXMLRoot(data []byte, want, emptyMsg, fix string) (*xmlReader, xmlStartElement, int, int, error) {
	r := newXMLReader(data, fix)
	for {
		tok, line, col, err := r.nextToken()
		if errors.Is(err, io.EOF) {
			return nil, xmlStartElement{}, 0, 0, &loaderr.Error{Line: 1, Column: 1, Problem: emptyMsg, Fix: fix}
		}
		if err != nil {
			return nil, xmlStartElement{}, 0, 0, err
		}
		se, ok := tok.(xmlStartElement)
		if !ok {
			continue
		}
		if se.Name.Local != want {
			return nil, xmlStartElement{}, 0, 0, &loaderr.Error{
				Line:     line,
				Column:   col,
				Problem:  "unexpected root element <" + se.Name.Local + ">",
				Expected: "<" + want + ">",
				Fix:      fix,
			}
		}
		return r, se, line, col, nil
	}
}

// parseTMX parses a Tiled XML map (.tmx) into the shared tiledMap representation with element line/column numbers.
func parseTMX(data []byte) (tiledMap, error) {
	r, se, line, col, err := parseXMLRoot(data, "map", "empty TMX document", fixTiledXML)
	if err != nil {
		return tiledMap{}, err
	}
	return parseXMLMap(r, se, line, col)
}

// parseTSX parses an external Tiled XML tileset (.tsx) into the shared tiledTileset representation with element line/column numbers.
func parseTSX(data []byte) (tiledTileset, error) {
	r, se, line, col, err := parseXMLRoot(data, "tileset", "empty TSX document", fixTilesetXML)
	if err != nil {
		return tiledTileset{}, err
	}
	ref, err := parseXMLTileset(r, se, line, col, false)
	if err != nil {
		return tiledTileset{}, err
	}
	return ref.tiledTileset, nil
}

func parseXMLMap(r *xmlReader, start xmlStartElement, line, col int) (tiledMap, error) {
	m := tiledMap{line: line, col: col}
	for _, a := range start.Attr {
		var err error
		switch a.Name.Local {
		case "width":
			m.Width, err = parseXMLInt(a.Value, line, col, "width", fixTiledXML)
		case "height":
			m.Height, err = parseXMLInt(a.Value, line, col, "height", fixTiledXML)
		case "tilewidth":
			m.TileWidth, err = parseXMLFloat32(a.Value, line, col, "tilewidth", fixTiledXML)
		case "tileheight":
			m.TileHeight, err = parseXMLFloat32(a.Value, line, col, "tileheight", fixTiledXML)
		case "orientation":
			m.Orientation = a.Value
		case "staggeraxis":
			m.StaggerAxis = a.Value
		case "staggerindex":
			m.StaggerIndex = a.Value
		case "hexsidelength":
			m.HexSideLength, err = parseXMLFloat32(a.Value, line, col, "hexsidelength", fixTiledXML)
		case "infinite":
			m.Infinite, err = parseXMLBoolAttr(a.Value, line, col, "infinite", fixTiledXML)
		}
		if err != nil {
			return tiledMap{}, err
		}
	}

	err := r.walkChildren(line, col, "", "map", nil, func(t xmlStartElement, cl, cc int) (bool, error) {
		if handled, err := parseXMLChildLayer(r, t, cl, cc, 1, &m.Properties, &m.Layers); handled || err != nil {
			return handled, err
		}
		if t.Name.Local == "tileset" {
			idx := len(m.Tilesets)
			ts, err := parseXMLTileset(r, t, cl, cc, true)
			if err != nil {
				return true, nest(err, loaderr.Path("tilesets", idx))
			}
			m.Tilesets = append(m.Tilesets, ts)
			return true, nil
		}
		return false, nil
	})
	return m, err
}

func parseXMLTileset(r *xmlReader, start xmlStartElement, line, col int, embedded bool) (tiledTilesetRef, error) {
	fix := fixTilesetXML
	if embedded {
		fix = fixTiledXML
	}
	ref := tiledTilesetRef{line: line, col: col}
	ref.tiledTileset.line = line
	ref.tiledTileset.col = col

	for _, a := range start.Attr {
		var err error
		switch a.Name.Local {
		case "firstgid":
			ref.FirstGID, err = parseXMLUint32(a.Value, line, col, "firstgid", fix)
		case "source":
			ref.Source = a.Value
		case "name":
			ref.Name = a.Value
		case "tilewidth":
			ref.TileWidth, err = parseXMLFloat32(a.Value, line, col, "tilewidth", fix)
		case "tileheight":
			ref.TileHeight, err = parseXMLFloat32(a.Value, line, col, "tileheight", fix)
		case "spacing":
			ref.Spacing, err = parseXMLFloat32(a.Value, line, col, "spacing", fix)
		case "margin":
			ref.Margin, err = parseXMLFloat32(a.Value, line, col, "margin", fix)
		case "tilecount":
			ref.TileCount, err = parseXMLInt(a.Value, line, col, "tilecount", fix)
		case "columns":
			ref.Columns, err = parseXMLInt(a.Value, line, col, "columns", fix)
		}
		if err != nil {
			return tiledTilesetRef{}, err
		}
	}

	err := r.walkChildren(line, col, "", "tileset", nil, func(t xmlStartElement, cl, cc int) (bool, error) {
		switch t.Name.Local {
		case "image":
			return true, parseXMLImageElem(r, t, cl, cc, fix, &ref.Image, &ref.ImageWidth, nil, &ref.imageLine, &ref.imageCol)
		case "properties":
			return true, parseXMLPropsChild(r, cl, cc, &ref.Properties)
		case "tile":
			ti := len(ref.Tiles)
			tile, err := parseXMLTile(r, t, cl, cc, fix)
			if err != nil {
				return true, nest(err, loaderr.Path("tiles", ti))
			}
			ref.Tiles = append(ref.Tiles, tile)
			return true, nil
		}
		return false, nil
	})
	return ref, err
}

func parseXMLTile(r *xmlReader, start xmlStartElement, line, col int, fix string) (tiledTile, error) {
	tile := tiledTile{line: line, col: col}
	hasID := false
	for _, a := range start.Attr {
		var err error
		switch a.Name.Local {
		case "id":
			hasID = true
			tile.ID, err = parseXMLUint32(a.Value, line, col, "id", fix)
		case "type":
			tile.Type = a.Value
		case "class":
			tile.Class = a.Value
		}
		if err != nil {
			return tiledTile{}, err
		}
	}
	if !hasID {
		return tiledTile{}, &loaderr.Error{Line: line, Column: col, Field: "id", Problem: "<tile> missing id attribute", Fix: fix}
	}

	err := r.walkChildren(line, col, "", "tile", nil, func(t xmlStartElement, cl, cc int) (bool, error) {
		switch t.Name.Local {
		case "properties":
			return true, parseXMLPropsChild(r, cl, cc, &tile.Properties)
		case "animation":
			frames, err := parseXMLAnimation(r, cl, cc, fix)
			if err != nil {
				return true, nest(err, "animation")
			}
			tile.Animation = append(tile.Animation, frames...)
			return true, nil
		case "objectgroup":
			group, err := parseXMLLayer(r, t, cl, cc, 1)
			if err != nil {
				return true, nest(err, "objectgroup")
			}
			tile.ObjectGroup = &group
			return true, nil
		}
		return false, nil
	})
	return tile, err
}

func parseXMLAnimation(r *xmlReader, line, col int, fix string) ([]tiledAnimFrameJSON, error) {
	var frames []tiledAnimFrameJSON
	err := r.walkChildren(line, col, "", "animation", nil, func(t xmlStartElement, cl, cc int) (bool, error) {
		if t.Name.Local != "frame" {
			return false, nil
		}
		fi := len(frames)
		f := tiledAnimFrameJSON{line: cl, col: cc}
		for _, a := range t.Attr {
			var err error
			switch a.Name.Local {
			case "tileid":
				f.TileID, err = parseXMLUint32(a.Value, cl, cc, loaderr.Path(fi, "tileid"), fix)
			case "duration":
				f.Duration, err = parseXMLInt(a.Value, cl, cc, loaderr.Path(fi, "duration"), fix)
			}
			if err != nil {
				return true, err
			}
		}
		if err := r.skip(); err != nil {
			return true, err
		}
		frames = append(frames, f)
		return true, nil
	})
	return frames, err
}

func parseXMLProperties(r *xmlReader, line, col int) ([]tiledProperty, error) {
	var props []tiledProperty
	err := r.walkChildren(line, col, "", "properties", nil, func(t xmlStartElement, cl, cc int) (bool, error) {
		if t.Name.Local != "property" {
			return false, nil
		}
		prop := tiledProperty{isXML: true, line: cl, col: cc}
		hasValueAttr := false
		for _, a := range t.Attr {
			switch a.Name.Local {
			case "name":
				prop.Name = a.Value
			case "type":
				prop.Type = a.Value
			case "value":
				prop.xmlValue = a.Value
				hasValueAttr = true
			}
		}
		var text strings.Builder
		var onText func(xmlCharData)
		if !hasValueAttr {
			onText = func(b xmlCharData) { text.Write(b) }
		}
		if err := r.walkChildren(cl, cc, "", "property", onText, nil); err != nil {
			return true, err
		}
		if !hasValueAttr {
			prop.xmlValue = text.String()
		}
		props = append(props, prop)
		return true, nil
	})
	return props, err
}

func parseXMLLayerCommon(attrs []xmlAttr, line, col int, l *tiledLayer) error {
	for _, a := range attrs {
		var err error
		switch a.Name.Local {
		case "name":
			l.Name = a.Value
		case "width":
			l.Width, err = parseXMLInt(a.Value, line, col, "width", fixTiledXML)
		case "height":
			l.Height, err = parseXMLInt(a.Value, line, col, "height", fixTiledXML)
		case "startx":
			l.StartX, err = parseXMLInt(a.Value, line, col, "startx", fixTiledXML)
		case "starty":
			l.StartY, err = parseXMLInt(a.Value, line, col, "starty", fixTiledXML)
		case "offsetx":
			l.OffsetX, err = parseXMLFloat32(a.Value, line, col, "offsetx", fixTiledXML)
		case "offsety":
			l.OffsetY, err = parseXMLFloat32(a.Value, line, col, "offsety", fixTiledXML)
		case "parallaxx":
			var v float32
			v, err = parseXMLFloat32(a.Value, line, col, "parallaxx", fixTiledXML)
			l.ParallaxX = &v
		case "parallaxy":
			var v float32
			v, err = parseXMLFloat32(a.Value, line, col, "parallaxy", fixTiledXML)
			l.ParallaxY = &v
		case "opacity":
			var v float32
			v, err = parseXMLFloat32(a.Value, line, col, "opacity", fixTiledXML)
			l.Opacity = &v
		case "visible":
			var b bool
			b, err = parseXMLBoolAttr(a.Value, line, col, "visible", fixTiledXML)
			l.Visible = &b
		case "repeatx":
			l.RepeatX, err = parseXMLBoolAttr(a.Value, line, col, "repeatx", fixTiledXML)
		case "repeaty":
			l.RepeatY, err = parseXMLBoolAttr(a.Value, line, col, "repeaty", fixTiledXML)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func parseXMLLayer(r *xmlReader, start xmlStartElement, line, col, depth int) (tiledLayer, error) {
	tag := start.Name.Local
	typ := tag
	if tag == "layer" {
		typ = "tilelayer"
	} else if tag == "group" && depth > maxXMLDepth {
		return tiledLayer{}, &loaderr.Error{
			Line:    line,
			Column:  col,
			Problem: fmt.Sprintf("group layers nested deeper than %d", maxXMLDepth),
			Fix:     "flatten deeply nested group layers in Tiled",
		}
	}
	l := tiledLayer{Type: typ, isXML: true, line: line, col: col}
	if err := parseXMLLayerCommon(start.Attr, line, col, &l); err != nil {
		return tiledLayer{}, err
	}
	err := r.walkChildren(line, col, "", tag, nil, func(t xmlStartElement, cl, cc int) (bool, error) {
		if tag == "group" {
			return parseXMLChildLayer(r, t, cl, cc, depth+1, &l.Properties, &l.Layers)
		}
		if t.Name.Local == "properties" {
			return true, parseXMLPropsChild(r, cl, cc, &l.Properties)
		}
		switch tag {
		case "layer":
			if t.Name.Local == "data" {
				return true, parseXMLData(r, t, cl, cc, &l)
			}
		case "imagelayer":
			if t.Name.Local == "image" {
				return true, parseXMLImageElem(r, t, cl, cc, fixTiledXML, &l.Image, &l.ImageWidth, &l.ImageHeight, &l.imageLine, &l.imageCol)
			}
		case "objectgroup":
			if t.Name.Local == "object" {
				oi := len(l.Objects)
				obj, err := parseXMLObject(r, t, cl, cc)
				if err != nil {
					return true, nest(err, loaderr.Path("objects", oi))
				}
				l.Objects = append(l.Objects, obj)
				return true, nil
			}
		}
		return false, nil
	})
	return l, err
}

func parseXMLData(r *xmlReader, start xmlStartElement, line, col int, l *tiledLayer) error {
	l.hasXMLData = true
	l.dataLine, l.dataCol = line, col
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "encoding":
			l.Encoding = a.Value
		case "compression":
			l.Compression = a.Value
		}
	}

	var text strings.Builder
	err := r.walkChildren(line, col, "data", "data", func(b xmlCharData) { text.Write(b) }, func(t xmlStartElement, cl, cc int) (bool, error) {
		switch t.Name.Local {
		case "chunk":
			ci := len(l.Chunks)
			ch, err := parseXMLChunk(r, t, cl, cc)
			if err != nil {
				return true, nest(err, loaderr.Path("chunks", ci))
			}
			l.Chunks = append(l.Chunks, ch)
			return true, nil
		case "tile":
			gid, err := parseXMLTileElem(r, t, cl, cc)
			if err != nil {
				return true, err
			}
			l.xmlTiles = append(l.xmlTiles, gid)
			return true, nil
		}
		return false, nil
	})
	l.xmlDataText = text.String()
	return err
}

func parseXMLChunk(r *xmlReader, start xmlStartElement, line, col int) (tiledChunk, error) {
	ch := tiledChunk{isXML: true, line: line, col: col}
	for _, a := range start.Attr {
		var err error
		switch a.Name.Local {
		case "x":
			ch.X, err = parseXMLInt(a.Value, line, col, "x", fixTiledXML)
		case "y":
			ch.Y, err = parseXMLInt(a.Value, line, col, "y", fixTiledXML)
		case "width":
			ch.Width, err = parseXMLInt(a.Value, line, col, "width", fixTiledXML)
		case "height":
			ch.Height, err = parseXMLInt(a.Value, line, col, "height", fixTiledXML)
		}
		if err != nil {
			return tiledChunk{}, err
		}
	}

	var text strings.Builder
	err := r.walkChildren(line, col, "", "chunk", func(b xmlCharData) { text.Write(b) }, func(t xmlStartElement, cl, cc int) (bool, error) {
		if t.Name.Local != "tile" {
			return false, nil
		}
		gid, err := parseXMLTileElem(r, t, cl, cc)
		if err != nil {
			return true, err
		}
		ch.xmlTiles = append(ch.xmlTiles, gid)
		return true, nil
	})
	ch.xmlDataText = text.String()
	return ch, err
}

func parseXMLObject(r *xmlReader, start xmlStartElement, line, col int) (tiledObject, error) {
	obj := tiledObject{line: line, col: col}
	for _, a := range start.Attr {
		var err error
		switch a.Name.Local {
		case "id":
			obj.ID, err = parseXMLInt(a.Value, line, col, "id", fixTiledXML)
		case "name":
			obj.Name = a.Value
		case "type":
			obj.Type = a.Value
		case "class":
			obj.Class = a.Value
		case "x":
			obj.X, err = parseXMLFloat32(a.Value, line, col, "x", fixTiledXML)
		case "y":
			obj.Y, err = parseXMLFloat32(a.Value, line, col, "y", fixTiledXML)
		case "width":
			obj.Width, err = parseXMLFloat32(a.Value, line, col, "width", fixTiledXML)
		case "height":
			obj.Height, err = parseXMLFloat32(a.Value, line, col, "height", fixTiledXML)
		case "rotation":
			obj.Rotation, err = parseXMLFloat32(a.Value, line, col, "rotation", fixTiledXML)
		case "gid":
			obj.GID, err = parseXMLUint32(a.Value, line, col, "gid", fixTiledXML)
		case "visible":
			var b bool
			b, err = parseXMLBoolAttr(a.Value, line, col, "visible", fixTiledXML)
			obj.Visible = &b
		}
		if err != nil {
			return tiledObject{}, err
		}
	}

	err := r.walkChildren(line, col, "", "object", nil, func(t xmlStartElement, cl, cc int) (bool, error) {
		switch t.Name.Local {
		case "properties":
			return true, parseXMLPropsChild(r, cl, cc, &obj.Properties)
		case "point":
			obj.Point = true
			return true, r.skip()
		case "ellipse":
			obj.Ellipse = true
			return true, r.skip()
		case "polygon":
			pts, err := parseXMLPointsElem(r, t, "polygon", cl, cc)
			obj.Polygon = pts
			return true, err
		case "polyline":
			pts, err := parseXMLPointsElem(r, t, "polyline", cl, cc)
			obj.Polyline = pts
			return true, err
		}
		return false, nil
	})
	return obj, err
}

func parseXMLPoints(raw, field string, line, col int) ([]tiledPoint, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil, nil
	}
	pts := make([]tiledPoint, 0, len(fields))
	for _, tok := range fields {
		xs, ys, ok := strings.Cut(tok, ",")
		if !ok {
			return nil, xmlAttrErr(line, col, field, `"x,y" coordinate pairs separated by spaces`, fixTiledXML, fmt.Sprintf("invalid point %q", tok), nil)
		}
		x, err := parseXMLFloat32(xs, line, col, field, fixTiledXML)
		if err != nil {
			return nil, err
		}
		y, err := parseXMLFloat32(ys, line, col, field, fixTiledXML)
		if err != nil {
			return nil, err
		}
		pts = append(pts, tiledPoint{X: x, Y: y})
	}
	return pts, nil
}

// validateLayerEncoding checks encoding and compression attributes for both TMJ and TMX tile layers.
func validateLayerEncoding(raw tiledLayer, li int) error {
	line, col := raw.dataLine, raw.dataCol
	if line == 0 {
		line, col = raw.line, raw.col
	}
	switch raw.Encoding {
	case "", "csv", "base64":
	case "xml":
		if !raw.isXML {
			return &loaderr.Error{
				Line:     line,
				Column:   col,
				Field:    loaderr.Path(li, "encoding"),
				Problem:  fmt.Sprintf("layer %q uses unsupported encoding %q", raw.Name, raw.Encoding),
				Expected: `"", "csv" or "base64"`,
				Fix:      fixEncoding,
			}
		}
	default:
		expected := `"", "csv" or "base64"`
		if raw.isXML {
			expected = `"", "xml", "csv" or "base64"`
		}
		return &loaderr.Error{
			Line:     line,
			Column:   col,
			Field:    loaderr.Path(li, "encoding"),
			Problem:  fmt.Sprintf("layer %q uses unsupported encoding %q", raw.Name, raw.Encoding),
			Expected: expected,
			Fix:      fixEncoding,
		}
	}

	if raw.Compression == "" {
		return nil
	}
	if raw.Encoding != "base64" {
		return &loaderr.Error{
			Line:     line,
			Column:   col,
			Field:    loaderr.Path(li, "compression"),
			Problem:  fmt.Sprintf("layer %q sets compression %q without base64 encoding", raw.Name, raw.Compression),
			Expected: `encoding="base64" when compression is set`,
			Fix:      fixEncoding,
		}
	}
	switch raw.Compression {
	case "zlib", "gzip":
		return nil
	case "zstd":
		return &loaderr.Error{
			Line:     line,
			Column:   col,
			Field:    loaderr.Path(li, "compression"),
			Problem:  fmt.Sprintf("layer %q uses zstd compression, which is not supported", raw.Name),
			Expected: `"zlib", "gzip" or uncompressed`,
			Fix:      fixZstd,
		}
	default:
		return &loaderr.Error{
			Line:     line,
			Column:   col,
			Field:    loaderr.Path(li, "compression"),
			Problem:  fmt.Sprintf("layer %q uses unsupported compression %q", raw.Name, raw.Compression),
			Expected: `"", "zlib" or "gzip"`,
			Fix:      fixEncoding,
		}
	}
}

// decodeCSVTileData parses comma-separated unsigned 32-bit tile GIDs from TMX (or string-encoded TMJ) data.
func decodeCSVTileData(s string, field string, line, col int, label string) ([]uint32, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	out := make([]uint32, 0, len(parts))
	for _, p := range parts {
		tok := strings.TrimSpace(p)
		if tok == "" {
			return nil, &loaderr.Error{
				Line:    line,
				Column:  col,
				Field:   field,
				Problem: fmt.Sprintf("%s has an empty CSV tile entry", label),
				Fix:     "save or export the map again from Tiled; CSV tile data must be comma-separated tile GIDs",
			}
		}
		gid, err := strconv.ParseUint(tok, 10, 32)
		if err != nil {
			return nil, &loaderr.Error{
				Line:     line,
				Column:   col,
				Field:    field,
				Problem:  fmt.Sprintf("%s has invalid CSV tile GID %q", label, tok),
				Expected: "an unsigned 32-bit integer",
				Fix:      "save or export the map again from Tiled; CSV tile data must be comma-separated tile GIDs",
				Err:      err,
			}
		}
		out = append(out, uint32(gid))
	}
	return out, nil
}

// decodeBase64TileData decodes base64 tile data (uncompressed, zlib, or gzip) into little-endian uint32 GIDs,
// enforcing both the expected tile count and a per-map decompression budget to defend against decompression bombs.
func decodeBase64TileData(encoded, compression string, expectedTiles int, budget *int64, field string, line, col int, label string) ([]uint32, error) {
	if expectedTiles < 0 || expectedTiles > maxTiledLayerTiles {
		return nil, &loaderr.Error{
			Line:    line,
			Column:  col,
			Field:   field,
			Problem: fmt.Sprintf("%s size (%d tiles) exceeds maximum allowed tile count (%d)", label, expectedTiles, maxTiledLayerTiles),
			Fix:     "reduce the layer or chunk dimensions in Tiled",
		}
	}
	expectedBytes := int64(expectedTiles) * 4
	if compression != "" && expectedBytes > *budget {
		return nil, &loaderr.Error{
			Line:    line,
			Column:  col,
			Field:   field,
			Problem: fmt.Sprintf("%s decompressed size (%d bytes) exceeds maximum allowed budget (%d bytes)", label, expectedBytes, maxMapDecompressedBytes),
			Fix:     "reduce the map dimensions or split the map into smaller files",
		}
	}

	clean := stripASCIIWhitespace(encoded)
	if expectedTiles == 0 && len(clean) == 0 {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return nil, &loaderr.Error{
			Line:    line,
			Column:  col,
			Field:   field,
			Problem: fmt.Sprintf("%s has invalid base64 tile data: %v", label, err),
			Fix:     "save or export the map again from Tiled; the base64 tile data is corrupted",
			Err:     err,
		}
	}

	if compression != "" {
		limit := min(expectedBytes, *budget)
		var decompressed []byte
		var oversized bool
		switch compression {
		case "zlib":
			decompressed, oversized, err = decompressZlib(raw, limit)
		case "gzip":
			decompressed, oversized, err = decompressGzip(raw, limit)
		default:
			return nil, &loaderr.Error{
				Line:    line,
				Column:  col,
				Field:   field,
				Problem: fmt.Sprintf("%s uses unsupported compression %q", label, compression),
				Fix:     fixEncoding,
			}
		}
		if oversized {
			return nil, &loaderr.Error{
				Line:     line,
				Column:   col,
				Field:    field,
				Problem:  fmt.Sprintf("%s %s tile data expands past %d bytes (oversized or corrupted stream)", label, compression, limit),
				Expected: fmt.Sprintf("%d bytes (%d tiles)", expectedBytes, expectedTiles),
				Fix:      "save or export the map again from Tiled so the compressed tile data matches the layer dimensions",
			}
		}
		if err != nil {
			return nil, &loaderr.Error{
				Line:    line,
				Column:  col,
				Field:   field,
				Problem: fmt.Sprintf("%s has corrupted %s tile data: %v", label, compression, err),
				Fix:     "save or export the map again from Tiled; the compressed tile data is corrupted",
				Err:     err,
			}
		}
		*budget -= int64(len(decompressed))
		raw = decompressed
	}

	if len(raw)%4 != 0 {
		return nil, &loaderr.Error{
			Line:     line,
			Column:   col,
			Field:    field,
			Problem:  fmt.Sprintf("%s decoded to %d bytes, which is not a multiple of 4", label, len(raw)),
			Expected: fmt.Sprintf("%d bytes (%d tiles)", expectedBytes, expectedTiles),
			Fix:      "save or export the map again from Tiled so tile data contains 4-byte little-endian GIDs",
		}
	}
	count := len(raw) / 4
	if count != expectedTiles {
		return nil, &loaderr.Error{
			Line:     line,
			Column:   col,
			Field:    field,
			Problem:  fmt.Sprintf("%s has %d tiles", label, count),
			Expected: fmt.Sprintf("%d tiles", expectedTiles),
			Fix:      "save or export the map again from Tiled so data has one GID per tile",
		}
	}
	if count == 0 {
		return nil, nil
	}
	gids := make([]uint32, count)
	for i := range count {
		gids[i] = binary.LittleEndian.Uint32(raw[i*4 : i*4+4])
	}
	return gids, nil
}

func stripASCIIWhitespace(s string) string {
	hasWS := false
	for i := range len(s) {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
			hasWS = true
		}
	}
	if !hasWS {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
