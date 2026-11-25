package tilemap_test

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// shapesMap is a Tiled map whose tileset draws every kind of collision object on its tiles.
const shapesMap = `{
	"orientation": "orthogonal", "width": 2, "height": 1, "tilewidth": 16, "tileheight": 16,
	"layers": [{"type": "tilelayer", "name": "ground", "width": 2, "height": 1, "data": [1, 2]}],
	"tilesets": [{
		"firstgid": 1, "name": "terrain", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 2,
		"tiles": [
			{"id": 0, "objectgroup": {"type": "objectgroup", "objects": [
				{"id": 1, "name": "floor", "type": "ground", "x": 0, "y": 8, "width": 16, "height": 8,
				 "properties": [{"name": "friction", "type": "float", "value": 0.5}]},
				{"id": 2, "x": 4, "y": 4, "point": true},
				{"id": 3, "x": 0, "y": 0, "width": 0, "height": 0},
				{"id": 4, "class": "ramp", "x": 0, "y": 16, "polygon": [{"x": 0, "y": 0}, {"x": 16, "y": -16}, {"x": 16, "y": 0}]},
				{"id": 5, "x": 0, "y": 0, "polyline": [{"x": 0, "y": 2}, {"x": 16, "y": 2}]},
				{"id": 6, "x": 8, "y": 0, "width": 8, "height": 4, "rotation": 90}
			]}},
			{"id": 1, "objectgroup": {"type": "objectgroup", "objects": [
				{"id": 1, "x": 0, "y": 0, "width": 16, "height": 8, "ellipse": true}
			]}}
		]
	}]
}`

func TestLoadTiledTileShapes(t *testing.T) {
	m, err := tilemap.LoadTiled([]byte(shapesMap), tilemap.TilesetLoader{})
	if err != nil {
		t.Fatal(err)
	}
	shapes := m.Tilesets[0].Shapes[0]
	if len(shapes) != 4 {
		t.Fatalf("tile 0 has %d shapes, want 4 (the point and the empty rectangle are skipped): %+v", len(shapes), shapes)
	}
	rect := shapes[0]
	if rect.Shape != tilemap.ObjectRect || rect.Name != "floor" || rect.Type != "ground" || !rect.Closed() {
		t.Fatalf("rectangle = %+v", rect)
	}
	if f, _ := rect.Properties.Float("friction"); f != 0.5 {
		t.Fatalf("rectangle properties = %v", rect.Properties)
	}
	if want := []core.Point{pt(0, 8), pt(16, 8), pt(16, 16), pt(0, 16)}; !slices.Equal(rect.Points, want) {
		t.Fatalf("rectangle points = %v, want %v", rect.Points, want)
	}
	if poly := shapes[1]; poly.Shape != tilemap.ObjectPolygon || poly.Type != "ramp" || !slices.Equal(poly.Points, []core.Point{pt(0, 16), pt(16, 0), pt(16, 16)}) {
		t.Fatalf("polygon = %+v", poly)
	}
	if line := shapes[2]; line.Shape != tilemap.ObjectPolyline || line.Closed() || !slices.Equal(line.Points, []core.Point{pt(0, 2), pt(16, 2)}) {
		t.Fatalf("polyline = %+v", line)
	}
	// Rotated 90 degrees clockwise about its top-left corner at (8, 0), the 8x4 rectangle covers x 4..8, y 0..8.
	rot := shapes[3]
	want := []core.Point{pt(8, 0), pt(8, 8), pt(4, 8), pt(4, 0)}
	for i, p := range rot.Points {
		if math.Abs(float64(p.X-want[i].X)) > 1e-5 || math.Abs(float64(p.Y-want[i].Y)) > 1e-5 {
			t.Fatalf("rotated rectangle = %v, want %v", rot.Points, want)
		}
	}

	ellipse := m.Tilesets[0].Shapes[1]
	if len(ellipse) != 1 || ellipse[0].Shape != tilemap.ObjectEllipse || len(ellipse[0].Points) != tilemap.EllipseShapeSegments {
		t.Fatalf("ellipse = %+v", ellipse)
	}
	for _, p := range ellipse[0].Points {
		dx, dy := float64(p.X-8)/8, float64(p.Y-4)/4
		if math.Abs(dx*dx+dy*dy-1) > 1e-5 {
			t.Fatalf("ellipse point %v is off the 16x8 ellipse", p)
		}
	}

	if got := m.TileShapes(1); len(got) != 4 || &got[0] != &shapes[0] {
		t.Fatal("TileShapes of an unflipped tile is not the tileset's slice")
	}
	if got := m.TileShapes(0); got != nil {
		t.Fatalf("TileShapes(0) = %v", got)
	}
	flipped := m.TileShapes(1 | tilemap.FlipVertical)
	if want := []core.Point{pt(0, 8), pt(16, 8), pt(16, 0), pt(0, 0)}; !slices.Equal(flipped[0].Points, want) {
		t.Fatalf("vertically flipped rectangle = %v, want %v", flipped[0].Points, want)
	}
	if !slices.Equal(shapes[0].Points, []core.Point{pt(0, 8), pt(16, 8), pt(16, 16), pt(0, 16)}) {
		t.Fatal("flipping changed the tileset's shape")
	}
	if w, h := m.TileBox(1); w != 16 || h != 16 {
		t.Fatalf("TileBox = %v, %v", w, h)
	}
}

// shapesTMX is shapesMap written as Tiled XML.
const shapesTMX = `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" width="2" height="1" tilewidth="16" tileheight="16">
 <tileset firstgid="1" name="terrain" tilewidth="16" tileheight="16" tilecount="2" columns="2">
  <tile id="0">
   <objectgroup draworder="index">
    <object id="1" name="floor" type="ground" x="0" y="8" width="16" height="8">
     <properties><property name="friction" type="float" value="0.5"/></properties>
    </object>
    <object id="2" x="4" y="4"><point/></object>
    <object id="3" x="0" y="0"/>
    <object id="4" class="ramp" x="0" y="16"><polygon points="0,0 16,-16 16,0"/></object>
    <object id="5" x="0" y="0"><polyline points="0,2 16,2"/></object>
    <object id="6" x="8" y="0" width="8" height="4" rotation="90"/>
   </objectgroup>
  </tile>
  <tile id="1">
   <objectgroup draworder="index">
    <object id="1" x="0" y="0" width="16" height="8"><ellipse/></object>
   </objectgroup>
  </tile>
 </tileset>
 <layer id="1" name="ground" width="2" height="1"><data encoding="csv">1,2</data></layer>
</map>`

func TestLoadTiledTMXTileShapesMatchJSON(t *testing.T) {
	want, err := tilemap.LoadTiled([]byte(shapesMap), tilemap.TilesetLoader{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := tilemap.LoadTiled([]byte(shapesTMX), tilemap.TilesetLoader{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Tilesets[0].Shapes, want.Tilesets[0].Shapes) {
		t.Fatalf("TMX shapes = %+v, want the TMJ map's %+v", got.Tilesets[0].Shapes, want.Tilesets[0].Shapes)
	}
}

func TestTileShapeFlippedMatchesDrawing(t *testing.T) {
	s := tilemap.TileShape{Shape: tilemap.ObjectPolygon, Points: []core.Point{pt(0, 0), pt(16, 0), pt(0, 8)}}
	if got := s.Flipped(0, 16, 16); &got.Points[0] != &s.Points[0] {
		t.Fatal("Flipped without flip bits copied the points")
	}
	for _, tc := range []struct {
		flags uint32
		want  []core.Point
	}{
		{tilemap.FlipHorizontal, []core.Point{pt(16, 0), pt(0, 0), pt(16, 8)}},
		{tilemap.FlipVertical, []core.Point{pt(0, 16), pt(16, 16), pt(0, 8)}},
		{tilemap.FlipDiagonal, []core.Point{pt(0, 0), pt(0, 16), pt(8, 0)}},
		{tilemap.FlipDiagonal | tilemap.FlipHorizontal, []core.Point{pt(16, 0), pt(16, 16), pt(8, 0)}},
		{tilemap.FlipHorizontal | 7, []core.Point{pt(16, 0), pt(0, 0), pt(16, 8)}},
	} {
		if got := s.Flipped(tc.flags, 16, 16).Points; !slices.Equal(got, tc.want) {
			t.Errorf("Flipped(%#x) = %v, want %v", tc.flags, got, tc.want)
		}
	}
}

func TestFlagEditsAreTileChanges(t *testing.T) {
	m := tilemap.New(4, 3, 8, 8)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("a", 4, 3, nil))
	type cell struct{ col, row int }
	changes := func(rev uint64) ([]cell, uint64, bool) {
		var got []cell
		cur, ok := m.TileChangesSince(rev, func(col, row int) { got = append(got, cell{col, row}) })
		return got, cur, ok
	}

	rev := m.TileRevision()
	m.SetCellFlags(3, 0, tilemap.TileSolid)
	m.SetCellFlags(3, 0, tilemap.TileSolid) // unchanged
	m.ClearCellFlags(3, 0)
	m.ClearCellFlags(3, 0) // nothing to clear
	got, cur, ok := changes(rev)
	if want := []cell{{3, 0}, {3, 0}}; !ok || !slices.Equal(got, want) {
		t.Fatalf("cell flag changes = %v (ok %v), want %v", got, ok, want)
	}

	m.SetFlags(6, tilemap.TileSolid)
	if _, _, ok := changes(cur); ok {
		t.Fatal("SetFlags did not ask for a full rebuild")
	}
	rev = m.TileRevision()
	m.SetFlags(6, tilemap.TileSolid) // unchanged
	m.SetFlags(0, tilemap.TileSolid) // no tile
	if m.TileRevision() != rev {
		t.Fatal("a SetFlags call that changes nothing moved the revision")
	}
	if m.Flags(6|tilemap.FlipHorizontal) != tilemap.TileSolid || m.Flags(0) != 0 || m.Flags(7) != 0 {
		t.Fatal("Flags")
	}
	m.SetCellFlags(1, 1, 0)
	if f, ok := m.CellFlags(1, 1); !ok || f != 0 {
		t.Fatal("CellFlags did not report an empty override")
	}
	if _, ok := m.CellFlags(0, 0); ok {
		t.Fatal("CellFlags reported an override that is not set")
	}
}

func pt(x, y float32) core.Point { return core.Point{X: core.Points(x), Y: core.Points(y)} }
