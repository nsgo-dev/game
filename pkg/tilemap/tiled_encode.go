package tilemap

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// EncodeTiled encodes m as a deterministic, indented Tiled JSON (.tmj) document.
//
// All tilesets (embedded inline), tile layers (dense or chunked), image layers, object layers,
// custom properties, tile animations, Wang sets, and per-tile collision object groups
// (from Tileset.TileCollisions, or synthesized from Tileset.Shapes when no TileCollisions entry exists)
// are written in canonical order so round-tripping through LoadTiled and EncodeTiled preserves
// per-tile collision data and produces byte-identical output.
func EncodeTiled(m *Tilemap) ([]byte, error) {
	if m == nil {
		return nil, errors.New("tilemap: EncodeTiled: nil tilemap")
	}
	if m.Width < 0 || m.Height < 0 {
		return nil, fmt.Errorf("tilemap: EncodeTiled: map size %dx%d must be non-negative", m.Width, m.Height)
	}
	if !(m.TileWidth > 0) || !(m.TileHeight > 0) || math.IsInf(float64(m.TileWidth), 0) || math.IsInf(float64(m.TileHeight), 0) {
		return nil, fmt.Errorf("tilemap: EncodeTiled: tile size %gx%g must be finite and positive", m.TileWidth, m.TileHeight)
	}

	layers, err := encodeTiledLayers(m)
	if err != nil {
		return nil, err
	}

	out := tmjMap{
		Width:         m.Width,
		Height:        m.Height,
		TileWidth:     m.TileWidth,
		TileHeight:    m.TileHeight,
		Orientation:   formatTiledOrientation(m.Orientation),
		Infinite:      m.Infinite,
		Properties:    encodeTiledProperties(m.Properties),
		Tilesets:      make([]tmjTileset, 0, len(m.Tilesets)),
		Layers:        layers,
		Type:          "map",
		RenderOrder:   "right-down",
		TiledVersion:  "1.10",
		FormatVersion: "1.10",
	}
	if m.Orientation == OrientationStaggered || m.Orientation == OrientationHexagonal {
		out.StaggerAxis = formatTiledStaggerAxis(m.StaggerAxis)
		out.StaggerIndex = formatTiledStaggerIndex(m.StaggerIndex)
	}
	if m.Orientation == OrientationHexagonal && m.HexSideLength != 0 {
		out.HexSideLength = m.HexSideLength
	}

	for _, ts := range m.Tilesets {
		if ts == nil {
			continue
		}
		out.Tilesets = append(out.Tilesets, encodeTiledTileset(m, ts))
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("tilemap: EncodeTiled: %w", err)
	}
	return buf.Bytes(), nil
}

func formatTiledOrientation(o Orientation) string {
	switch o {
	case OrientationIsometric:
		return "isometric"
	case OrientationStaggered:
		return "staggered"
	case OrientationHexagonal:
		return "hexagonal"
	default:
		return "orthogonal"
	}
}

func formatTiledStaggerAxis(a StaggerAxis) string {
	if a == StaggerX {
		return "x"
	}
	return "y"
}

func formatTiledStaggerIndex(i StaggerIndex) string {
	if i == StaggerEven {
		return "even"
	}
	return "odd"
}

func formatObjectAlignment(a ObjectAlignment) string {
	switch a {
	case ObjectAlignmentTopLeft:
		return "topleft"
	case ObjectAlignmentTop:
		return "top"
	case ObjectAlignmentTopRight:
		return "topright"
	case ObjectAlignmentLeft:
		return "left"
	case ObjectAlignmentCenter:
		return "center"
	case ObjectAlignmentRight:
		return "right"
	case ObjectAlignmentBottomLeft:
		return "bottomleft"
	case ObjectAlignmentBottom:
		return "bottom"
	case ObjectAlignmentBottomRight:
		return "bottomright"
	default:
		return ""
	}
}

func formatTiledColor(c core.Color) string {
	r := uint32(c>>24) & 0xff
	g := uint32(c>>16) & 0xff
	b := uint32(c>>8) & 0xff
	a := uint32(c) & 0xff
	if a == 0xff {
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", a, r, g, b)
}

func encodeTiledProperties(props Properties) []tmjProperty {
	if len(props) == 0 {
		return nil
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	slices.Sort(names)
	out := make([]tmjProperty, 0, len(names))
	for _, name := range names {
		p := props[name]
		typ := typeName(p.Type)
		val := p.Value
		if typ == "color" {
			if c, ok := val.(core.Color); ok {
				val = formatTiledColor(c)
			}
		}
		out = append(out, tmjProperty{
			Name:  name,
			Type:  typ,
			Value: val,
		})
	}
	return out
}

func encodeTiledTileset(m *Tilemap, ts *Tileset) tmjTileset {
	out := tmjTileset{
		FirstGID:        ts.FirstGID,
		Name:            ts.Name,
		Image:           ts.Image.URI,
		TileWidth:       ts.TileWidth,
		TileHeight:      ts.TileHeight,
		ObjectAlignment: formatObjectAlignment(ts.ObjectAlignment),
		Columns:         ts.Columns,
		TileCount:       ts.TileCount,
		Spacing:         ts.Spacing,
		Margin:          ts.Margin,
		Properties:      encodeTiledProperties(ts.Properties),
	}
	if ts.TileOffsetX != 0 || ts.TileOffsetY != 0 {
		out.TileOffset = &tiledPoint{X: ts.TileOffsetX, Y: ts.TileOffsetY}
	}

	// Collect all local tile IDs that carry metadata, images, probabilities, animations, collisions, shapes, or flags.
	idSet := make(map[uint32]struct{})
	for id := range ts.TileImages {
		idSet[id] = struct{}{}
	}
	for id := range ts.TileProperties {
		idSet[id] = struct{}{}
	}
	for id := range ts.TileProbabilities {
		idSet[id] = struct{}{}
	}
	for id := range ts.Animations {
		idSet[id] = struct{}{}
	}
	for id := range ts.TileCollisions {
		idSet[id] = struct{}{}
	}
	for id := range ts.Shapes {
		idSet[id] = struct{}{}
	}
	for gid := range m.tileFlags {
		owner, localID := m.TilesetForGID(gid)
		if owner == ts {
			idSet[localID] = struct{}{}
		}
	}
	if len(idSet) > 0 {
		ids := make([]uint32, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		tiles := make([]tmjTile, 0, len(ids))
		for _, id := range ids {
			t := tmjTile{ID: id}
			if ti, ok := ts.TileImages[id]; ok {
				t.Image = ti.Image.URI
				if ti.X != 0 || ti.Y != 0 {
					t.X = ti.X
					t.Y = ti.Y
					t.Width = ti.Width
					t.Height = ti.Height
				} else {
					t.ImageWidth = ti.Width
					t.ImageHeight = ti.Height
				}
			}
			if prob, ok := ts.TileProbabilities[id]; ok {
				p := prob
				t.Probability = &p
			}
			t.Properties = encodeTilePropertiesWithFlags(m, ts, id)
			if anim := ts.Animations[id]; anim != nil && len(anim.Frames) > 0 {
				frames := make([]tmjAnimFrame, len(anim.Frames))
				for i, f := range anim.Frames {
					frames[i] = tmjAnimFrame{
						TileID:   f.TileID,
						Duration: int(f.Duration / time.Millisecond),
					}
				}
				t.Animation = frames
			}
			if cg := ts.TileCollisions[id]; cg != nil {
				layer := encodeTiledObjectLayer(*cg)
				t.ObjectGroup = &layer
			} else if shapes := ts.Shapes[id]; len(shapes) > 0 {
				layer := encodeTileShapesAsObjectGroup(shapes)
				t.ObjectGroup = &layer
			}
			if t.Image != "" || t.Probability != nil || len(t.Properties) > 0 || len(t.Animation) > 0 || t.ObjectGroup != nil {
				tiles = append(tiles, t)
			}
		}
		if len(tiles) > 0 {
			out.Tiles = tiles
		}
	}

	if len(ts.WangSets) > 0 {
		out.WangSets = encodeTiledWangSets(ts.WangSets)
	}
	return out
}

func encodeTilePropertiesWithFlags(m *Tilemap, ts *Tileset, localID uint32) []tmjProperty {
	base := ts.TileProperties[localID]
	var combined Properties
	if len(base) > 0 {
		combined = make(Properties, len(base)+3)
		for k, v := range base {
			combined[k] = v
		}
	}
	if sum := uint64(ts.FirstGID) + uint64(localID); sum <= uint64(TileIDMask) {
		want := m.Flags(uint32(sum))
		existing := flagsFromProperties(base)
		if want != existing {
			if combined == nil {
				combined = make(Properties, 3)
			}
			if want&TileSolid != 0 {
				if existing&TileSolid == 0 {
					combined["solid"] = Property{Name: "solid", Type: "bool", Value: true}
				}
			} else {
				delete(combined, "solid")
			}
			if want&TileOneWay != 0 {
				if existing&TileOneWay == 0 {
					combined["one-way"] = Property{Name: "one-way", Type: "bool", Value: true}
				}
			} else {
				for _, key := range []string{"oneWay", "one-way", "oneway", "one_way"} {
					delete(combined, key)
				}
			}
			customWant := want &^ (TileSolid | TileOneWay)
			customExisting := existing &^ (TileSolid | TileOneWay)
			if customWant == 0 {
				delete(combined, "flags")
			} else if customWant != customExisting || (existing&^want) != 0 {
				combined["flags"] = Property{Name: "flags", Type: "int", Value: uint32(customWant)}
			}
		}
	}
	return encodeTiledProperties(combined)
}

func encodeTileShapesAsObjectGroup(shapes []TileShape) tmjLayer {
	vis := true
	op := float32(1)
	objs := make([]tmjObject, 0, len(shapes))
	for i, s := range shapes {
		o := tmjObject{
			ID:         i + 1,
			Name:       s.Name,
			Type:       s.Type,
			Visible:    &vis,
			Properties: encodeTiledProperties(s.Properties),
		}
		switch s.Shape {
		case ObjectRect:
			if len(s.Points) == 4 && isAxisAlignedRectPoints(s.Points) {
				minX, minY, maxX, maxY := pointBounds(s.Points)
				w, h := maxX-minX, maxY-minY
				o.X = minX
				o.Y = minY
				o.Width = &w
				o.Height = &h
			} else if len(s.Points) >= 3 {
				o.Polygon = encodePoints(s.Points)
			}
		case ObjectPolyline:
			o.Polyline = encodePoints(s.Points)
		default:
			// Polygons and approximated ellipses are encoded as exact polygons so round-tripping keeps identical vertices.
			o.Polygon = encodePoints(s.Points)
		}
		objs = append(objs, o)
	}
	return tmjLayer{
		Type:    "objectgroup",
		Opacity: &op,
		Visible: &vis,
		Objects: objs,
	}
}

func isAxisAlignedRectPoints(pts []core.Point) bool {
	if len(pts) != 4 {
		return false
	}
	for i, p := range pts {
		q := pts[(i+1)%4]
		if math.Abs(float64(p.X-q.X)) > 1e-5 && math.Abs(float64(p.Y-q.Y)) > 1e-5 {
			return false
		}
	}
	return true
}

func pointBounds(pts []core.Point) (minX, minY, maxX, maxY float32) {
	minX, minY = float32(pts[0].X), float32(pts[0].Y)
	maxX, maxY = minX, minY
	for _, p := range pts[1:] {
		x, y := float32(p.X), float32(p.Y)
		minX = min(minX, x)
		minY = min(minY, y)
		maxX = max(maxX, x)
		maxY = max(maxY, y)
	}
	return minX, minY, maxX, maxY
}

func encodePoints(pts []core.Point) []tiledPoint {
	if len(pts) == 0 {
		return nil
	}
	out := make([]tiledPoint, len(pts))
	for i, p := range pts {
		out[i] = tiledPoint{X: float32(p.X), Y: float32(p.Y)}
	}
	return out
}

func encodeTiledWangSets(sets []WangSet) []tmjWangSet {
	out := make([]tmjWangSet, len(sets))
	for i, ws := range sets {
		typ := "corner"
		switch ws.Type {
		case WangEdge:
			typ = "edge"
		case WangMixed:
			typ = "mixed"
		}
		tile := ws.Tile
		colors := make([]tmjWangColor, len(ws.Colors))
		for ci, c := range ws.Colors {
			ct := c.Tile
			prob := c.Probability
			wc := tmjWangColor{
				Name:        c.Name,
				Tile:        &ct,
				Probability: &prob,
				Properties:  encodeTiledProperties(c.Properties),
			}
			if c.Color != 0 {
				wc.Color = formatTiledColor(c.Color)
			}
			colors[ci] = wc
		}
		tiles := make([]tmjWangTile, len(ws.WangTiles))
		for ti, wt := range ws.WangTiles {
			wid := make([]int, 8)
			for k := range 8 {
				wid[k] = int(wt.WangID[k])
			}
			tiles[ti] = tmjWangTile{
				TileID: wt.TileID,
				WangID: wid,
			}
		}
		out[i] = tmjWangSet{
			Name:       ws.Name,
			Type:       typ,
			Tile:       &tile,
			Colors:     colors,
			WangTiles:  tiles,
			Properties: encodeTiledProperties(ws.Properties),
		}
	}
	return out
}

func encodeTiledLayers(m *Tilemap) ([]tmjLayer, error) {
	seenTile := make([]bool, len(m.Layers))
	seenImage := make([]bool, len(m.ImageLayers))
	seenObject := make([]bool, len(m.Objects))
	out := make([]tmjLayer, 0, len(m.Layers)+len(m.ImageLayers)+len(m.Objects))
	for _, ref := range m.layerOrder {
		switch ref.kind {
		case layerKindTile:
			if ref.index >= 0 && ref.index < len(m.Layers) && !seenTile[ref.index] {
				seenTile[ref.index] = true
				l, err := encodeTiledTileLayer(m, &m.Layers[ref.index])
				if err != nil {
					return nil, err
				}
				out = append(out, l)
			}
		case layerKindImage:
			if ref.index >= 0 && ref.index < len(m.ImageLayers) && !seenImage[ref.index] {
				seenImage[ref.index] = true
				out = append(out, encodeTiledImageLayer(&m.ImageLayers[ref.index]))
			}
		case layerKindObject:
			if ref.index >= 0 && ref.index < len(m.Objects) && !seenObject[ref.index] {
				seenObject[ref.index] = true
				out = append(out, encodeTiledObjectLayer(m.Objects[ref.index]))
			}
		}
	}
	for i := range m.Layers {
		if !seenTile[i] {
			l, err := encodeTiledTileLayer(m, &m.Layers[i])
			if err != nil {
				return nil, err
			}
			out = append(out, l)
		}
	}
	for i := range m.ImageLayers {
		if !seenImage[i] {
			out = append(out, encodeTiledImageLayer(&m.ImageLayers[i]))
		}
	}
	for i := range m.Objects {
		if !seenObject[i] {
			out = append(out, encodeTiledObjectLayer(m.Objects[i]))
		}
	}
	return out, nil
}

func encodeTiledParallax(out *tmjLayer, px, py float32, hasParallax bool) {
	px, py = effectiveParallax(px, py, hasParallax)
	if px != 1 {
		if px == 0 {
			px = 0
		}
		out.ParallaxX = &px
	}
	if py != 1 {
		if py == 0 {
			py = 0
		}
		out.ParallaxY = &py
	}
}

func encodeTiledTileLayer(m *Tilemap, l *TileLayer) (tmjLayer, error) {
	op := l.Opacity
	vis := l.Visible
	out := tmjLayer{
		Name:       l.Name,
		Type:       "tilelayer",
		Width:      l.Width,
		Height:     l.Height,
		StartX:     l.StartX,
		StartY:     l.StartY,
		OffsetX:    l.OffsetX,
		OffsetY:    l.OffsetY,
		Opacity:    &op,
		Visible:    &vis,
		Properties: encodeTiledProperties(l.Properties),
	}
	encodeTiledParallax(&out, l.ParallaxX, l.ParallaxY, l.HasParallax)
	if l.Tinted || l.TintColor != 0 {
		out.TintColor = formatTiledColor(l.TintColor)
	}
	if l.IsChunked() {
		coords := l.ChunkCoords()
		slices.SortFunc(coords, func(a, b ChunkCoord) int {
			return cmp.Or(cmp.Compare(a.Y, b.Y), cmp.Compare(a.X, b.X))
		})
		chunks := make([]tmjChunk, 0, len(coords))
		for _, coord := range coords {
			ch := l.Chunk(coord)
			if ch == nil {
				continue
			}
			data := make([]uint32, len(ch.Tiles))
			copy(data, ch.Tiles)
			chunks = append(chunks, tmjChunk{
				X:      coord.X * ch.Size,
				Y:      coord.Y * ch.Size,
				Width:  ch.Size,
				Height: ch.Size,
				Data:   data,
			})
		}
		out.Chunks = chunks
	} else {
		lw, lh := m.layerSize(l)
		out.Width = lw
		out.Height = lh
		if len(l.Data) > 0 {
			out.Data = slices.Clone(l.Data)
		} else if lw > 0 && lh > 0 {
			if lw > math.MaxInt/lh || lw*lh > maxTiledLayerTiles {
				return tmjLayer{}, fmt.Errorf("tilemap: EncodeTiled: layer %q size %dx%d exceeds limit", l.Name, lw, lh)
			}
			out.Data = make([]uint32, lw*lh)
		}
	}
	return out, nil
}

func encodeTiledImageLayer(l *ImageLayer) tmjLayer {
	op := l.Opacity
	vis := l.Visible
	out := tmjLayer{
		Name:        l.Name,
		Type:        "imagelayer",
		Image:       l.Image.URI,
		ImageWidth:  l.Width,
		ImageHeight: l.Height,
		OffsetX:     l.OffsetX,
		OffsetY:     l.OffsetY,
		RepeatX:     l.RepeatX,
		RepeatY:     l.RepeatY,
		Opacity:     &op,
		Visible:     &vis,
		Properties:  encodeTiledProperties(l.Properties),
	}
	encodeTiledParallax(&out, l.ParallaxX, l.ParallaxY, l.HasParallax)
	if l.Tinted || l.TintColor != 0 {
		out.TintColor = formatTiledColor(l.TintColor)
	}
	return out
}

func encodeTiledObjectLayer(l ObjectLayer) tmjLayer {
	op := l.Opacity
	vis := l.Visible
	objs := make([]tmjObject, len(l.Objects))
	for i, o := range l.Objects {
		objs[i] = encodeTiledObject(o)
	}
	out := tmjLayer{
		Name:       l.Name,
		Type:       "objectgroup",
		OffsetX:    l.OffsetX,
		OffsetY:    l.OffsetY,
		Opacity:    &op,
		Visible:    &vis,
		Objects:    objs,
		Properties: encodeTiledProperties(l.Properties),
	}
	encodeTiledParallax(&out, l.ParallaxX, l.ParallaxY, l.HasParallax)
	if l.Tinted || l.TintColor != 0 {
		out.TintColor = formatTiledColor(l.TintColor)
	}
	return out
}

func encodeTiledObject(o Object) tmjObject {
	vis := o.Visible
	out := tmjObject{
		ID:         o.ID,
		Name:       o.Name,
		Type:       o.Type,
		X:          o.X,
		Y:          o.Y,
		Visible:    &vis,
		Properties: encodeTiledProperties(o.Properties),
	}
	if o.Width != 0 {
		w := o.Width
		out.Width = &w
	}
	if o.Height != 0 {
		h := o.Height
		out.Height = &h
	}
	if o.Rotation != 0 {
		rot := o.Rotation
		out.Rotation = &rot
	}
	switch o.Shape {
	case ObjectPoint:
		out.Point = true
	case ObjectEllipse:
		out.Ellipse = true
	case ObjectPolygon:
		out.Polygon = encodePoints(o.Points)
	case ObjectPolyline:
		out.Polyline = encodePoints(o.Points)
	case ObjectTile:
		out.GID = o.GID
	case ObjectText:
		if o.Text != nil {
			txt := o.Text.Text
			wrap := o.Text.Wrap
			bold := o.Text.Bold
			italic := o.Text.Italic
			underline := o.Text.Underline
			strikeout := o.Text.Strikeout
			kerning := o.Text.Kerning
			tt := &tmjText{
				Text:       &txt,
				FontFamily: o.Text.FontFamily,
				PixelSize:  o.Text.PixelSize,
				Wrap:       &wrap,
				Bold:       &bold,
				Italic:     &italic,
				Underline:  &underline,
				Strikeout:  &strikeout,
				Kerning:    &kerning,
				HAlign:     o.Text.HAlign,
				VAlign:     o.Text.VAlign,
			}
			if o.Text.Color != 0 {
				tt.Color = formatTiledColor(o.Text.Color)
			}
			out.Text = tt
		}
	}
	return out
}

type tmjMap struct {
	Type          string        `json:"type"`
	FormatVersion string        `json:"version"`
	TiledVersion  string        `json:"tiledversion"`
	Orientation   string        `json:"orientation"`
	RenderOrder   string        `json:"renderorder"`
	Width         int           `json:"width"`
	Height        int           `json:"height"`
	TileWidth     float32       `json:"tilewidth"`
	TileHeight    float32       `json:"tileheight"`
	StaggerAxis   string        `json:"staggeraxis,omitempty"`
	StaggerIndex  string        `json:"staggerindex,omitempty"`
	HexSideLength float32       `json:"hexsidelength,omitempty"`
	Infinite      bool          `json:"infinite"`
	Properties    []tmjProperty `json:"properties,omitempty"`
	Tilesets      []tmjTileset  `json:"tilesets"`
	Layers        []tmjLayer    `json:"layers"`
}

type tmjTileset struct {
	FirstGID        uint32        `json:"firstgid"`
	Name            string        `json:"name,omitempty"`
	Image           string        `json:"image,omitempty"`
	TileWidth       float32       `json:"tilewidth,omitempty"`
	TileHeight      float32       `json:"tileheight,omitempty"`
	TileOffset      *tiledPoint   `json:"tileoffset,omitempty"`
	ObjectAlignment string        `json:"objectalignment,omitempty"`
	Columns         int           `json:"columns,omitempty"`
	TileCount       int           `json:"tilecount,omitempty"`
	Spacing         float32       `json:"spacing,omitempty"`
	Margin          float32       `json:"margin,omitempty"`
	Properties      []tmjProperty `json:"properties,omitempty"`
	Tiles           []tmjTile     `json:"tiles,omitempty"`
	WangSets        []tmjWangSet  `json:"wangsets,omitempty"`
}

type tmjTile struct {
	ID          uint32         `json:"id"`
	Image       string         `json:"image,omitempty"`
	ImageWidth  float32        `json:"imagewidth,omitempty"`
	ImageHeight float32        `json:"imageheight,omitempty"`
	X           float32        `json:"x,omitempty"`
	Y           float32        `json:"y,omitempty"`
	Width       float32        `json:"width,omitempty"`
	Height      float32        `json:"height,omitempty"`
	Probability *float64       `json:"probability,omitempty"`
	Properties  []tmjProperty  `json:"properties,omitempty"`
	Animation   []tmjAnimFrame `json:"animation,omitempty"`
	ObjectGroup *tmjLayer      `json:"objectgroup,omitempty"`
}

type tmjAnimFrame struct {
	TileID   uint32 `json:"tileid"`
	Duration int    `json:"duration"`
}

type tmjWangSet struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Tile       *int           `json:"tile,omitempty"`
	Colors     []tmjWangColor `json:"colors"`
	WangTiles  []tmjWangTile  `json:"wangtiles"`
	Properties []tmjProperty  `json:"properties,omitempty"`
}

type tmjWangColor struct {
	Name        string        `json:"name"`
	Color       string        `json:"color,omitempty"`
	Tile        *int          `json:"tile,omitempty"`
	Probability *float64      `json:"probability,omitempty"`
	Properties  []tmjProperty `json:"properties,omitempty"`
}

type tmjWangTile struct {
	TileID uint32 `json:"tileid"`
	WangID []int  `json:"wangid"`
}

type tmjLayer struct {
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Width       int           `json:"width,omitempty"`
	Height      int           `json:"height,omitempty"`
	StartX      int           `json:"startx,omitempty"`
	StartY      int           `json:"starty,omitempty"`
	OffsetX     float32       `json:"offsetx,omitempty"`
	OffsetY     float32       `json:"offsety,omitempty"`
	ParallaxX   *float32      `json:"parallaxx,omitempty"`
	ParallaxY   *float32      `json:"parallaxy,omitempty"`
	Opacity     *float32      `json:"opacity,omitempty"`
	TintColor   string        `json:"tintcolor,omitempty"`
	Visible     *bool         `json:"visible,omitempty"`
	Image       string        `json:"image,omitempty"`
	ImageWidth  float32       `json:"imagewidth,omitempty"`
	ImageHeight float32       `json:"imageheight,omitempty"`
	RepeatX     bool          `json:"repeatx,omitempty"`
	RepeatY     bool          `json:"repeaty,omitempty"`
	Data        []uint32      `json:"data,omitempty"`
	Chunks      []tmjChunk    `json:"chunks,omitempty"`
	Objects     []tmjObject   `json:"objects,omitempty"`
	Properties  []tmjProperty `json:"properties,omitempty"`
}

type tmjChunk struct {
	X      int      `json:"x"`
	Y      int      `json:"y"`
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Data   []uint32 `json:"data"`
}

type tmjObject struct {
	ID         int           `json:"id"`
	Name       string        `json:"name,omitempty"`
	Type       string        `json:"type,omitempty"`
	X          float32       `json:"x"`
	Y          float32       `json:"y"`
	Width      *float32      `json:"width,omitempty"`
	Height     *float32      `json:"height,omitempty"`
	Rotation   *float32      `json:"rotation,omitempty"`
	GID        uint32        `json:"gid,omitempty"`
	Visible    *bool         `json:"visible,omitempty"`
	Point      bool          `json:"point,omitempty"`
	Ellipse    bool          `json:"ellipse,omitempty"`
	Polygon    []tiledPoint  `json:"polygon,omitempty"`
	Polyline   []tiledPoint  `json:"polyline,omitempty"`
	Text       *tmjText      `json:"text,omitempty"`
	Properties []tmjProperty `json:"properties,omitempty"`
}

type tmjText struct {
	Text       *string `json:"text,omitempty"`
	FontFamily string  `json:"fontfamily,omitempty"`
	PixelSize  int     `json:"pixelsize,omitempty"`
	Wrap       *bool   `json:"wrap,omitempty"`
	Color      string  `json:"color,omitempty"`
	Bold       *bool   `json:"bold,omitempty"`
	Italic     *bool   `json:"italic,omitempty"`
	Underline  *bool   `json:"underline,omitempty"`
	Strikeout  *bool   `json:"strikeout,omitempty"`
	Kerning    *bool   `json:"kerning,omitempty"`
	HAlign     string  `json:"halign,omitempty"`
	VAlign     string  `json:"valign,omitempty"`
}

type tmjProperty struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}
