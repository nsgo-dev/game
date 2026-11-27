package tilemap_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func points(xy ...float32) []core.Point {
	out := make([]core.Point, 0, len(xy)/2)
	for i := 0; i+1 < len(xy); i += 2 {
		out = append(out, core.Point{X: core.Points(xy[i]), Y: core.Points(xy[i+1])})
	}
	return out
}

func TestObjectPhysicsShape(t *testing.T) {
	cases := []struct {
		name   string
		obj    tilemap.Object
		ok     bool
		kind   physics.ShapeKind
		bounds physics.Box
	}{
		{"rect", tilemap.Object{Shape: tilemap.ObjectRect, X: 10, Y: 20, Width: 30, Height: 8}, true, physics.ShapeBox, physics.Box{X: 10, Y: 20, W: 30, H: 8}},
		{"rotated rect", tilemap.Object{Shape: tilemap.ObjectRect, X: 10, Y: 20, Width: 30, Height: 8, Rotation: 90}, true, physics.ShapePolygon, physics.Box{X: 2, Y: 20, W: 8, H: 30}},
		{"empty rect", tilemap.Object{Shape: tilemap.ObjectRect, X: 10, Y: 20}, false, 0, physics.Box{}},
		{"circle", tilemap.Object{Shape: tilemap.ObjectEllipse, X: 10, Y: 20, Width: 12, Height: 12}, true, physics.ShapeCircle, physics.Box{X: 10, Y: 20, W: 12, H: 12}},
		{"ellipse", tilemap.Object{Shape: tilemap.ObjectEllipse, X: 0, Y: 0, Width: 40, Height: 20}, true, physics.ShapePolygon, physics.Box{X: 0, Y: 0, W: 40, H: 20}},
		{"convex polygon", tilemap.Object{Shape: tilemap.ObjectPolygon, X: 100, Y: 50, Points: points(0, 0, 40, 30, 0, 30)}, true, physics.ShapePolygon, physics.Box{X: 100, Y: 50, W: 40, H: 30}},
		{"concave polygon", tilemap.Object{Shape: tilemap.ObjectPolygon, X: 0, Y: 0, Points: points(0, 0, 20, 10, 40, 0, 40, 30, 0, 30)}, true, physics.ShapeChain, physics.Box{X: 0, Y: 0, W: 40, H: 30}},
		{"polygon with nine vertices", tilemap.Object{Shape: tilemap.ObjectPolygon, Points: ninegon()}, true, physics.ShapeChain, physics.Box{X: -9.848078, Y: -9.396926, W: 19.696156, H: 19.396927}},
		{"pentagram", tilemap.Object{Shape: tilemap.ObjectPolygon, Points: pentagram()}, true, physics.ShapeChain, physics.Box{X: -9.510565, Y: -8.090170, W: 19.021130, H: 18.090170}},
		{"collinear polygon", tilemap.Object{Shape: tilemap.ObjectPolygon, Points: points(0, 0, 10, 0, 20, 0)}, false, 0, physics.Box{}},
		{"polyline pair", tilemap.Object{Shape: tilemap.ObjectPolyline, X: 5, Y: 5, Points: points(0, 0, 10, 5)}, true, physics.ShapeSegment, physics.Box{X: 5, Y: 5, W: 10, H: 5}},
		{"polyline", tilemap.Object{Shape: tilemap.ObjectPolyline, X: 0, Y: 100, Points: points(0, 0, 50, -20, 100, 0)}, true, physics.ShapeChain, physics.Box{X: 0, Y: 80, W: 100, H: 20}},
		{"polyline repeated point", tilemap.Object{Shape: tilemap.ObjectPolyline, Points: points(3, 3, 3, 3)}, false, 0, physics.Box{}},
		{"point", tilemap.Object{Shape: tilemap.ObjectPoint, X: 1, Y: 1}, false, 0, physics.Box{}},
		{"tile", tilemap.Object{Shape: tilemap.ObjectTile, GID: 3, Width: 16, Height: 16}, false, 0, physics.Box{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ok := tc.obj.PhysicsShape()
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (%+v)", ok, tc.ok, s)
			}
			if !ok {
				return
			}
			if s.Kind != tc.kind {
				t.Fatalf("kind = %v, want %v", s.Kind, tc.kind)
			}
			b := s.Bounds()
			if math.Abs(float64(b.X-tc.bounds.X)) > 1e-3 || math.Abs(float64(b.Y-tc.bounds.Y)) > 1e-3 || math.Abs(float64(b.W-tc.bounds.W)) > 1e-3 || math.Abs(float64(b.H-tc.bounds.H)) > 1e-3 {
				t.Fatalf("bounds = %+v, want %+v", b, tc.bounds)
			}
		})
	}
}

// pentagram returns the self-intersecting star {5/2}, which turns the same way at every vertex but twice around in total.
func pentagram() []core.Point {
	out := make([]core.Point, 5)
	for i := range out {
		a := 2*math.Pi*float64(2*i)/5 + math.Pi/2
		out[i] = core.Point{X: core.Points(10 * math.Cos(a)), Y: core.Points(10 * math.Sin(a))}
	}
	return out
}

func ninegon() []core.Point {
	out := make([]core.Point, 9)
	for i := range out {
		a := 2*math.Pi*float64(i)/9 + math.Pi/2
		out[i] = core.Point{X: core.Points(10 * math.Cos(a)), Y: core.Points(10 * math.Sin(a))}
	}
	return out
}

func TestObjectLayerPhysicsBodies(t *testing.T) {
	layer := tilemap.ObjectLayer{
		OffsetX: 5, OffsetY: -5,
		Objects: []tilemap.Object{
			{ID: 7, Type: "ground", Shape: tilemap.ObjectPolyline, X: 0, Y: 100, Points: points(0, 0, 100, 0, 200, 0)},
			{ID: 8, Type: "spawn", Shape: tilemap.ObjectPoint, X: 50, Y: 50},
			{ID: 9, Type: "rock", Shape: tilemap.ObjectRect, X: 10, Y: 10, Width: 4, Height: 4, Visible: false},
		},
	}
	bodies := layer.PhysicsBodies()
	if len(bodies) != 2 {
		t.Fatalf("got %d bodies, want 2 (the point has no shape; hidden objects count)", len(bodies))
	}
	ground, rock := bodies[0], bodies[1]
	if ground.ID != 7 || ground.Tag != "ground" || ground.Shape.Kind != physics.ShapeChain || ground.UserData.(tilemap.Object).ID != 7 {
		t.Fatalf("ground body = %+v", ground)
	}
	if b := ground.Shape.Bounds(); b.X != 5 || b.Y != 95 {
		t.Fatalf("ground not offset by the layer: %+v", b)
	}
	if rock.Shape.Box != (physics.Box{X: 15, Y: 5, W: 4, H: 4}) {
		t.Fatalf("rock = %+v", rock.Shape.Box)
	}
	// The original layer is untouched.
	if layer.Objects[0].X != 0 || layer.Objects[0].Points[1].X != 100 {
		t.Fatal("PhysicsBodies changed the layer's objects")
	}
	// A missing layer, as Tilemap.ObjectLayer returns it, has no bodies instead of panicking.
	var missing *tilemap.ObjectLayer
	if got := missing.PhysicsBodies(); got != nil {
		t.Fatalf("nil layer bodies = %v", got)
	}
}

func TestTilemapSatisfiesPhysicsTileGridDirectly(t *testing.T) {
	if uint32(tilemap.TileSolid) != uint32(physics.TileSolid) || uint32(tilemap.TileOneWay) != uint32(physics.TileOneWay) {
		t.Fatalf("flag bits diverged: solid %d vs %d, oneWay %d vs %d",
			tilemap.TileSolid, physics.TileSolid, tilemap.TileOneWay, physics.TileOneWay)
	}
	if got := (tilemap.TileSolid | tilemap.TileFlags(1<<5)).PhysicsFlags(); got != (physics.TileSolid | physics.TileFlags(1<<5)) {
		t.Fatalf("PhysicsFlags = %v, want Solid|(1<<5)", got)
	}

	// Nil receiver returns zero values safely.
	var nilMap *tilemap.Tilemap
	if w, h := nilMap.TileSize(); w != 0 || h != 0 {
		t.Fatalf("nil TileSize = %v, %v", w, h)
	}
	if c, r := nilMap.GridSize(); c != 0 || r != 0 {
		t.Fatalf("nil GridSize = %v, %v", c, r)
	}
	if x, y := nilMap.GridOrigin(); x != 0 || y != 0 {
		t.Fatalf("nil GridOrigin = %v, %v", x, y)
	}
	if f := nilMap.TileFlagsAt(0, 0); f != 0 {
		t.Fatalf("nil TileFlagsAt = %v", f)
	}

	// Loaded Tiled .tmj works directly with physics.MoveAndSlide and physics.World.
	raw, err := os.ReadFile(filepath.Join("testdata", "sample.tmj"))
	if err != nil {
		t.Fatal(err)
	}
	tm, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{
		LoadTileset: func(source string) ([]byte, error) {
			return os.ReadFile(filepath.Join("testdata", filepath.FromSlash(source)))
		},
	})
	if err != nil {
		t.Fatalf("LoadTiled(sample.tmj): %v", err)
	}

	var grid physics.TileGrid = tm
	var origin physics.TileGridOrigin = tm
	if w, h := grid.TileSize(); w != 16 || h != 16 {
		t.Fatalf("TileSize = %v, %v", w, h)
	}
	if c, r := grid.GridSize(); c != 4 || r != 3 {
		t.Fatalf("GridSize = %v, %v", c, r)
	}
	if x, y := origin.GridOrigin(); x != 0 || y != 0 {
		t.Fatalf("GridOrigin = %v, %v", x, y)
	}
	if got := grid.TileFlagsAt(0, 0); got != physics.TileSolid {
		t.Fatalf("TileFlagsAt(0,0) = %v, want TileSolid", got)
	}
	if got := grid.TileFlagsAt(0, 2); got != physics.TileOneWay {
		t.Fatalf("TileFlagsAt(0,2) = %v, want TileOneWay", got)
	}
	if got := grid.TileFlagsAt(3, 2); got != physics.TileFlags(4) {
		t.Fatalf("TileFlagsAt(3,2) = %v, want custom flag 4", got)
	}

	// Drop a body onto row 0 (solid tiles at y=0..16) from above (y=-20) with physics.MoveAndSlide(body, tm, dt).
	body := physics.NewBoxBody(2, -20, 12, 12)
	body.VY = 240
	res := physics.MoveAndSlide(body, tm, 200*time.Millisecond)
	if !res.Grounded || body.Bounds().Bottom() != 0 {
		t.Fatalf("MoveAndSlide(body, tm, dt) = grounded:%v bottom:%v, want grounded:true bottom:0", res.Grounded, body.Bounds().Bottom())
	}

	// Works directly with physics.World queries too.
	w := &physics.World{Tiles: tm}
	if hit, ok := w.RaycastFirst(core.Point{X: 8, Y: -10}, core.Point{X: 8, Y: 10}, physics.QueryTiles); !ok || hit.Point.Y != 0 {
		t.Fatalf("World.RaycastFirst on *Tilemap = (%+v, %v), want y=0", hit, ok)
	}
}
