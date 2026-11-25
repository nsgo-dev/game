package tilemap_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/tilemap"
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
