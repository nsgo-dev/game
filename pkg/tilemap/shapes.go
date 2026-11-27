package tilemap

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// EllipseShapeSegments is the number of straight edges LoadTiled uses to approximate an ellipse a tileset draws on a tile as its collision shape.
const EllipseShapeSegments = 16

// TileShape is one collision shape a tileset gives a tile, such as the rectangles, polygons and ellipses Tiled's collision editor stores on a tile.
//
// Points are in the tile's own coordinates, with (0, 0) at the top-left of the tileset's tile and (TileWidth, TileHeight) at its bottom-right, already rotated by the object's rotation. A closed shape (every Shape except ObjectPolyline) is the polygon through its points, with an edge from the last point back to the first; an ellipse is approximated by EllipseShapeSegments points on its outline, and a rectangle has its four corners.
type TileShape struct {
	// Shape is the kind of object the shape came from: ObjectRect, ObjectEllipse, ObjectPolygon or ObjectPolyline.
	Shape ObjectShape
	// Name and Type are the object's name and type (or class) in the collision editor.
	Name string
	Type string
	// Points are the shape's vertices in the tile's coordinates.
	Points []core.Point
	// Properties are the object's custom properties.
	Properties Properties
}

// Closed reports whether s is a closed polygon rather than an open polyline.
func (s TileShape) Closed() bool { return s.Shape != ObjectPolyline }

// Flipped returns s mirrored and rotated as Tiled's flip bits in flags place a tile of tileWidth by tileHeight on a map, the same transform DrawLayer applies to the tile's image.
//
// FlipDiagonal swaps the axes first, then FlipHorizontal mirrors left to right and FlipVertical top to bottom, so the eight combinations give every 90 degree rotation of a tile with and without a mirror. Flipped returns s unchanged when flags holds no flip bit, and a copy with new Points otherwise.
func (s TileShape) Flipped(flags uint32, tileWidth, tileHeight float32) TileShape {
	flags &= FlipMask
	if flags == 0 || len(s.Points) == 0 {
		return s
	}
	a, b, c, d, e, f := flipMatrix(flags, 0, 0, tileWidth, tileHeight)
	pts := make([]core.Point, len(s.Points))
	for i, p := range s.Points {
		x, y := float32(p.X), float32(p.Y)
		pts[i] = core.Point{X: core.Points(a*x + c*y + e), Y: core.Points(b*x + d*y + f)}
	}
	s.Points = pts
	return s
}

// TileCollisionGroup returns the collision ObjectLayer authored on gid's tile in its tileset (flip bits ignored), and whether the tile defines one.
func (m *Tilemap) TileCollisionGroup(gid uint32) (ObjectLayer, bool) {
	ts, localID := m.TilesetForGID(gid)
	if ts == nil || len(ts.TileCollisions) == 0 {
		return ObjectLayer{}, false
	}
	g, ok := ts.TileCollisions[localID]
	if !ok || g == nil {
		return ObjectLayer{}, false
	}
	return *g, true
}

// TileShapes returns the collision shapes of gid's tile, flipped and rotated by gid's flip bits in the coordinates of its tileset's tile, or nil when the tile has none.
//
// An unflipped gid returns the tileset's own slice, which the caller must not modify.
func (m *Tilemap) TileShapes(gid uint32) []TileShape {
	ts, localID := m.TilesetForGID(gid)
	if ts == nil || len(ts.Shapes) == 0 {
		return nil
	}
	shapes := ts.Shapes[localID]
	flags := gid & FlipMask
	if len(shapes) == 0 || flags == 0 {
		return shapes
	}
	tw, th := m.tileBox(ts, localID)
	out := make([]TileShape, len(shapes))
	for i, s := range shapes {
		out[i] = s.Flipped(flags, tw, th)
	}
	return out
}

// TileBox returns the size of gid's tile in its tileset, which is the box its collision shapes are measured in, falling back to the map's tile size when the tileset gives none or gid has no tileset.
func (m *Tilemap) TileBox(gid uint32) (width, height float32) {
	ts, localID := m.TilesetForGID(gid)
	return m.tileBox(ts, localID)
}

func (m *Tilemap) tileBox(ts *Tileset, localID uint32) (float32, float32) {
	tw, th := m.TileWidth, m.TileHeight
	if ts != nil {
		if ti, ok := ts.TileImages[localID]; ok {
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
			if w > 0 {
				tw = w
			}
			if h > 0 {
				th = h
			}
			return tw, th
		}
		if ts.TileWidth > 0 {
			tw = ts.TileWidth
		}
		if ts.TileHeight > 0 {
			th = ts.TileHeight
		}
	}
	return tw, th
}

// tileShapesFromObjects converts already-parsed Objects of a tile's collision object group into TileShapes without re-parsing properties or polygon points.
func tileShapesFromObjects(objects []Object) []TileShape {
	var shapes []TileShape
	for _, o := range objects {
		if o.Shape == ObjectPoint || o.Shape == ObjectTile || o.Shape == ObjectText || o.GID != 0 {
			continue
		}
		s := TileShape{Name: o.Name, Type: o.Type, Properties: o.Properties}
		switch o.Shape {
		case ObjectEllipse:
			if o.Width <= 0 || o.Height <= 0 {
				continue
			}
			s.Shape = ObjectEllipse
			rx, ry := float64(o.Width)/2, float64(o.Height)/2
			s.Points = make([]core.Point, EllipseShapeSegments)
			for k := range s.Points {
				a := 2 * math.Pi * float64(k) / EllipseShapeSegments
				s.Points[k] = core.Point{X: core.Points(rx + rx*math.Cos(a)), Y: core.Points(ry + ry*math.Sin(a))}
			}
		case ObjectPolygon:
			if len(o.Points) < 3 {
				continue
			}
			s.Shape = ObjectPolygon
			if o.X == 0 && o.Y == 0 && o.Rotation == 0 {
				s.Points = o.Points
				shapes = append(shapes, s)
				continue
			}
			s.Points = append([]core.Point(nil), o.Points...)
		case ObjectPolyline:
			if len(o.Points) < 2 {
				continue
			}
			s.Shape = ObjectPolyline
			if o.X == 0 && o.Y == 0 && o.Rotation == 0 {
				s.Points = o.Points
				shapes = append(shapes, s)
				continue
			}
			s.Points = append([]core.Point(nil), o.Points...)
		default:
			if o.Width <= 0 || o.Height <= 0 {
				continue
			}
			s.Shape = ObjectRect
			s.Points = []core.Point{
				{X: 0, Y: 0},
				{X: core.Points(o.Width), Y: 0},
				{X: core.Points(o.Width), Y: core.Points(o.Height)},
				{X: 0, Y: core.Points(o.Height)},
			}
		}
		placeObjectPoints(s.Points, float64(o.X), float64(o.Y), float64(o.Rotation))
		shapes = append(shapes, s)
	}
	return shapes
}

// tiledTileShapes converts the objects of a tile's collision object group into TileShapes. Points and tile objects are skipped, as are rectangles and ellipses with no area and polygons or polylines with too few points to make an edge. The fields its errors name are relative to the tile.
func tiledTileShapes(objects []tiledObject) ([]TileShape, error) {
	var shapes []TileShape
	for i, o := range objects {
		if o.Point || o.GID != 0 {
			continue
		}
		props, err := parseProperties(o.Properties)
		if err != nil {
			return nil, nest(err, idxPath("objectgroup.objects", i, "properties"))
		}
		typ := o.Type
		if typ == "" {
			typ = o.Class
		}
		ow, oh, orot := float32Val(o.Width), float32Val(o.Height), float32Val(o.Rotation)
		s := TileShape{Name: o.Name, Type: typ, Properties: props}
		switch {
		case o.Ellipse:
			if ow <= 0 || oh <= 0 {
				continue
			}
			s.Shape = ObjectEllipse
			rx, ry := float64(ow)/2, float64(oh)/2
			s.Points = make([]core.Point, EllipseShapeSegments)
			for k := range s.Points {
				a := 2 * math.Pi * float64(k) / EllipseShapeSegments
				s.Points[k] = core.Point{X: core.Points(rx + rx*math.Cos(a)), Y: core.Points(ry + ry*math.Sin(a))}
			}
		case len(o.Polygon) > 0:
			if len(o.Polygon) < 3 {
				continue
			}
			s.Shape = ObjectPolygon
			s.Points = convertPoints(o.Polygon)
		case len(o.Polyline) > 0:
			if len(o.Polyline) < 2 {
				continue
			}
			s.Shape = ObjectPolyline
			s.Points = convertPoints(o.Polyline)
		default:
			if ow <= 0 || oh <= 0 {
				continue
			}
			s.Shape = ObjectRect
			s.Points = []core.Point{
				{X: 0, Y: 0},
				{X: core.Points(ow), Y: 0},
				{X: core.Points(ow), Y: core.Points(oh)},
				{X: 0, Y: core.Points(oh)},
			}
		}
		placeObjectPoints(s.Points, float64(o.X), float64(o.Y), float64(orot))
		shapes = append(shapes, s)
	}
	return shapes, nil
}

// placeObjectPoints rotates pts, given relative to an object's origin, by rotation degrees clockwise about that origin and moves them to (x, y), as Tiled places an object.
func placeObjectPoints(pts []core.Point, x, y, rotation float64) {
	sin, cos := 0.0, 1.0
	if r := math.Mod(rotation, 360); r != 0 {
		sin, cos = math.Sincos(r * math.Pi / 180)
	}
	for i, p := range pts {
		px, py := float64(p.X), float64(p.Y)
		pts[i] = core.Point{X: core.Points(x + px*cos - py*sin), Y: core.Points(y + px*sin + py*cos)}
	}
}

// Flags returns the TileFlags set on global tile ID tileID with SetFlags or from its tileset's properties, ignoring flip bits and any per-cell override.
func (m *Tilemap) Flags(tileID uint32) TileFlags {
	id := tileID & TileIDMask
	if id == 0 || m.tileFlags == nil {
		return 0
	}
	return m.tileFlags[id]
}

// CellFlags returns the per-cell override SetCellFlags set at (col, row) and whether one is set.
func (m *Tilemap) CellFlags(col, row int) (TileFlags, bool) {
	if m.cellFlags == nil {
		return 0, false
	}
	f, ok := m.cellFlags[cellKey{col, row}]
	return f, ok
}
