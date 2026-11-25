package physics_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/physics"
)

func nearF32(a, b, tol float32) bool {
	return float32(math.Abs(float64(a-b))) <= tol
}

func TestShapeCastBoxVsBox(t *testing.T) {
	w := &physics.World{}
	wall := physics.NewBoxBody(100, 0, 20, 40)
	wall.Tag = "wall"
	w.Add(wall)

	// Sweep a 10x10 box rightward from x=10 by dx=200: hits wall at x=100 when box.X=90 -> dx=80 -> frac=0.4.
	moving := physics.BoxShape(physics.Box{X: 10, Y: 10, W: 10, H: 10})
	hit, ok := w.ShapeCast(moving, 200, 0, physics.QueryBodies)
	if !ok {
		t.Fatal("expected hit against wall")
	}
	if hit.Body != wall {
		t.Fatalf("hit.Body = %v, want wall", hit.Body)
	}
	if !nearF32(hit.Fraction, 0.4, 1e-5) {
		t.Fatalf("Fraction = %v, want 0.4", hit.Fraction)
	}
	if hit.NormalX != -1 || hit.NormalY != 0 {
		t.Fatalf("Normal = (%v, %v), want (-1, 0)", hit.NormalX, hit.NormalY)
	}
	if !nearF32(float32(hit.Point.X), 100, 1e-4) || !nearF32(float32(hit.Point.Y), 15, 1e-4) {
		t.Fatalf("Point = %+v, want (100, 15)", hit.Point)
	}

	// Grazing flush along the top of the wall (y = -10, H = 10 -> bottom = 0 == wall.Y) must NOT snag.
	grazing := physics.BoxShape(physics.Box{X: 10, Y: -10, W: 10, H: 10})
	if h, ok := w.ShapeCast(grazing, 200, 0, physics.QueryBodies); ok {
		t.Fatalf("grazing box unexpectedly hit: %+v", h)
	}

	// Touching at t=0 moving inward reports Fraction == 0.
	touching := physics.BoxShape(physics.Box{X: 90, Y: 10, W: 10, H: 10})
	if h, ok := w.ShapeCast(touching, 20, 0, physics.QueryBodies); !ok || h.Fraction != 0 || h.NormalX != -1 {
		t.Fatalf("touching inward = (%+v, %v), want Fraction=0 NormalX=-1", h, ok)
	}
	// Touching at t=0 moving outward misses.
	if h, ok := w.ShapeCast(touching, -20, 0, physics.QueryBodies); ok {
		t.Fatalf("touching outward unexpectedly hit: %+v", h)
	}

	// Initial overlap reports Fraction == 0 even with zero displacement.
	overlapping := physics.BoxShape(physics.Box{X: 95, Y: 10, W: 10, H: 10})
	if h, ok := w.ShapeCast(overlapping, 0, 0, physics.QueryBodies); !ok || h.Fraction != 0 || h.NormalX != -1 {
		t.Fatalf("initial overlap = (%+v, %v), want Fraction=0 NormalX=-1", h, ok)
	}

	// Invalid / empty / NaN inputs return no hit.
	if _, ok := w.ShapeCast(physics.Shape{}, 10, 0, physics.QueryAll); ok {
		t.Fatal("empty shape should not hit")
	}
	if _, ok := w.ShapeCast(moving, float32(math.NaN()), 0, physics.QueryAll); ok {
		t.Fatal("NaN displacement should not hit")
	}
}

func TestShapeCastCircleAndBoxPairs(t *testing.T) {
	// Circle vs Circle: centers at (0, 0) r=5 and (30, 0) r=5, dx=50 -> hit at center x=20 -> frac=0.4.
	h, ok := physics.ShapeCastShape(
		physics.CircleShape(physics.Circle{X: 0, Y: 0, R: 5}),
		50, 0,
		physics.CircleShape(physics.Circle{X: 30, Y: 0, R: 5}),
	)
	if !ok || !nearF32(h.Fraction, 0.4, 1e-5) || !nearF32(h.NormalX, -1, 1e-5) || !nearF32(h.NormalY, 0, 1e-5) {
		t.Fatalf("circle-circle = (%+v, %v), want frac=0.4 normal=(-1,0)", h, ok)
	}
	if !nearF32(float32(h.Point.X), 25, 1e-4) || !nearF32(float32(h.Point.Y), 0, 1e-4) {
		t.Fatalf("circle-circle point = %+v, want (25, 0)", h.Point)
	}

	// Circle vs Box face: circle r=4 at (0, 20), box [20, 40]x[10, 30], dx=40 -> center stops at x=16 -> frac=0.4.
	box := physics.BoxShape(physics.Box{X: 20, Y: 10, W: 20, H: 20})
	h, ok = physics.ShapeCastShape(physics.CircleShape(physics.Circle{X: 0, Y: 20, R: 4}), 40, 0, box)
	if !ok || !nearF32(h.Fraction, 0.4, 1e-5) || h.NormalX != -1 || h.NormalY != 0 {
		t.Fatalf("circle-box face = (%+v, %v), want frac=0.4 normal=(-1,0)", h, ok)
	}
	if !nearF32(float32(h.Point.X), 20, 1e-4) || !nearF32(float32(h.Point.Y), 20, 1e-4) {
		t.Fatalf("circle-box face point = %+v, want (20, 20)", h.Point)
	}

	// Circle vs Box rounded corner: circle r=5 moving diagonally toward top-left corner (20, 10) from (0, -10).
	// Displacement (40, 40) passes directly through (20, 10). Distance from (0,-10) to (20,10) is 20*sqrt(2).
	// Circle center stops at distance 5 before corner: t = (20*sqrt(2) - 5) / (40*sqrt(2)) = 0.5 - 5/(40*sqrt(2)).
	wantT := float32(0.5 - 5.0/(40.0*math.Sqrt2))
	h, ok = physics.ShapeCastShape(physics.CircleShape(physics.Circle{X: 0, Y: -10, R: 5}), 40, 40, box)
	if !ok || !nearF32(h.Fraction, wantT, 1e-4) {
		t.Fatalf("circle-box corner = (%+v, %v), want frac=%v", h, ok, wantT)
	}
	invS2 := float32(-1 / math.Sqrt2)
	if !nearF32(h.NormalX, invS2, 1e-4) || !nearF32(h.NormalY, invS2, 1e-4) {
		t.Fatalf("circle-box corner normal = (%v, %v), want (%v, %v)", h.NormalX, h.NormalY, invS2, invS2)
	}
	if !nearF32(float32(h.Point.X), 20, 1e-4) || !nearF32(float32(h.Point.Y), 10, 1e-4) {
		t.Fatalf("circle-box corner point = %+v, want (20, 10)", h.Point)
	}

	// Circle grazing flush along top of box (y = 10 - 4 = 6, dy = 0) must NOT hit.
	if gh, ok := physics.ShapeCastShape(physics.CircleShape(physics.Circle{X: 0, Y: 6, R: 4}), 60, 0, box); ok {
		t.Fatalf("grazing circle unexpectedly hit box: %+v", gh)
	}

	// Box vs Circle (symmetric): box [0, 10]x[0, 10], dx=20, circle at (25, 5) r=5 -> hits at x=10+20*t=20 -> t=0.5.
	h, ok = physics.ShapeCastShape(
		physics.BoxShape(physics.Box{X: 0, Y: 0, W: 10, H: 10}),
		20, 0,
		physics.CircleShape(physics.Circle{X: 25, Y: 5, R: 5}),
	)
	if !ok || !nearF32(h.Fraction, 0.5, 1e-5) || !nearF32(h.NormalX, -1, 1e-5) || !nearF32(h.NormalY, 0, 1e-5) {
		t.Fatalf("box-circle = (%+v, %v), want frac=0.5 normal=(-1,0)", h, ok)
	}
	if !nearF32(float32(h.Point.X), 20, 1e-4) || !nearF32(float32(h.Point.Y), 5, 1e-4) {
		t.Fatalf("box-circle point = %+v, want (20, 5)", h.Point)
	}
}

func TestShapeCastCorridorTiles(t *testing.T) {
	// 12x3 corridor of 16x16 tiles:
	// Row 0: solid ceiling
	// Row 1: open corridor with solid walls at col 7 and col 10
	// Row 2: solid floor
	grid := physics.NewGrid(12, 3, 16, 16)
	grid.FillRect(0, 0, 12, 1, physics.TileSolid)
	grid.FillRect(0, 2, 12, 1, physics.TileSolid)
	grid.Set(7, 1, physics.TileSolid)
	grid.Set(10, 1, physics.TileSolid)

	w := &physics.World{Tiles: grid}

	// Box filling the exact corridor height (16 px) starts at col 1 (x=16, y=16), swept right by 160 px.
	// Must travel cleanly through the corridor without snagging ceiling/floor and hit tile (7, 1) at x=112.
	// Box right edge starts at 32 -> travels 80 px to x=112 -> Fraction = 80/160 = 0.5.
	box := physics.BoxShape(physics.Box{X: 16, Y: 16, W: 16, H: 16})
	hit, ok := w.ShapeCast(box, 160, 0, physics.QueryTiles)
	if !ok {
		t.Fatal("expected corridor box cast to hit first solid tile")
	}
	if !hit.Tile || hit.Col != 7 || hit.Row != 1 || hit.TileFlags != physics.TileSolid {
		t.Fatalf("hit = %+v, want tile (7, 1) solid", hit)
	}
	if !nearF32(hit.Fraction, 0.5, 1e-5) || hit.NormalX != -1 || hit.NormalY != 0 {
		t.Fatalf("hit frac/normal = (%v, %v, %v), want (0.5, -1, 0)", hit.Fraction, hit.NormalX, hit.NormalY)
	}
	if !nearF32(float32(hit.Point.X), 112, 1e-4) || !nearF32(float32(hit.Point.Y), 24, 1e-4) {
		t.Fatalf("hit point = %+v, want (112, 24)", hit.Point)
	}

	// ShapeCastAll reports both solid tiles in the corridor: (7, 1) then (10, 1).
	all := w.ShapeCastAll(box, 160, 0, physics.QueryTiles)
	if len(all) != 2 || all[0].Col != 7 || all[1].Col != 10 {
		t.Fatalf("ShapeCastAll = %+v, want cols [7, 10]", all)
	}
	if all[0] != hit {
		t.Fatalf("ShapeCast (%+v) != ShapeCastAll[0] (%+v)", hit, all[0])
	}

	// Circle of exact corridor radius (r=8 at y=24) also glides cleanly to tile (7, 1).
	circle := physics.CircleShape(physics.Circle{X: 24, Y: 24, R: 8})
	ch, ok := w.ShapeCast(circle, 160, 0, physics.QueryTiles)
	if !ok || !ch.Tile || ch.Col != 7 || ch.Row != 1 || !nearF32(ch.Fraction, 0.5, 1e-5) || ch.NormalX != -1 {
		t.Fatalf("corridor circle cast = (%+v, %v), want tile (7, 1) frac=0.5 normal=(-1,0)", ch, ok)
	}
}

func TestShapeCastTilesExactBoundaryFractionOne(t *testing.T) {
	// 8x8 grid of 16x16 tiles with solid walls around a central open area and a one-way platform.
	grid := physics.NewGrid(8, 8, 16, 16)
	grid.Set(5, 3, physics.TileSolid)  // +X target at x=[80, 96], y=[48, 64]
	grid.Set(1, 3, physics.TileSolid)  // -X target at x=[16, 32], y=[48, 64]
	grid.Set(3, 5, physics.TileSolid)  // +Y target at x=[48, 64], y=[80, 96]
	grid.Set(3, 1, physics.TileSolid)  // -Y target at x=[48, 64], y=[16, 32]
	grid.Set(6, 5, physics.TileOneWay) // +Y one-way at x=[96, 112], y=[80, 96]

	w := &physics.World{Tiles: grid}
	center := physics.BoxShape(physics.Box{X: 48, Y: 48, W: 16, H: 16})

	cases := []struct {
		name      string
		shape     physics.Shape
		dx, dy    float32
		mask      physics.QueryMask
		wantCol   int
		wantRow   int
		wantNX    float32
		wantNY    float32
		wantFlags physics.TileFlags
	}{
		{"+X solid", center, 16, 0, physics.QueryTiles, 5, 3, -1, 0, physics.TileSolid},
		{"-X solid", center, -16, 0, physics.QueryTiles, 1, 3, 1, 0, physics.TileSolid},
		{"+Y solid", center, 0, 16, physics.QueryTiles, 3, 5, 0, -1, physics.TileSolid},
		{"-Y solid", center, 0, -16, physics.QueryTiles, 3, 1, 0, 1, physics.TileSolid},
		{"+Y one-way", physics.BoxShape(physics.Box{X: 96, Y: 48, W: 16, H: 16}), 0, 16, physics.QueryOneWay, 6, 5, 0, -1, physics.TileOneWay},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, ok := w.ShapeCast(tc.shape, tc.dx, tc.dy, tc.mask)
			if !ok {
				t.Fatalf("expected hit at Fraction == 1.0, got miss")
			}
			if !h.Tile || h.Col != tc.wantCol || h.Row != tc.wantRow || h.TileFlags != tc.wantFlags {
				t.Fatalf("hit = %+v, want tile (%d,%d) flags=%v", h, tc.wantCol, tc.wantRow, tc.wantFlags)
			}
			if !nearF32(h.Fraction, 1.0, 1e-5) || h.NormalX != tc.wantNX || h.NormalY != tc.wantNY {
				t.Fatalf("frac/normal = (%v, %v, %v), want (1.0, %v, %v)", h.Fraction, h.NormalX, h.NormalY, tc.wantNX, tc.wantNY)
			}
		})
	}
}

func TestShapeCastRejectsNonFiniteShapesAndPoses(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	grid := physics.NewGrid(4, 4, 16, 16)
	grid.Set(2, 1, physics.TileSolid)
	w := &physics.World{Tiles: grid}
	w.Add(physics.NewBoxBody(32, 16, 16, 16))
	// Body with NaN dimensions must be skipped cleanly.
	w.Add(&physics.Body{Shape: physics.BoxShape(physics.Box{X: 0, Y: 0, W: nan, H: 10})})

	badShapes := []physics.Shape{
		physics.BoxShape(physics.Box{X: nan, Y: 0, W: 10, H: 10}),
		physics.BoxShape(physics.Box{X: 0, Y: 0, W: nan, H: 10}),
		physics.BoxShape(physics.Box{X: inf, Y: 0, W: 10, H: 10}),
		physics.CircleShape(physics.Circle{X: nan, Y: 0, R: 5}),
		physics.CircleShape(physics.Circle{X: 0, Y: 0, R: nan}),
		physics.CircleShape(physics.Circle{X: 0, Y: inf, R: 5}),
	}
	validBox := physics.BoxShape(physics.Box{X: 0, Y: 16, W: 10, H: 10})

	for i, s := range badShapes {
		if h, ok := physics.ShapeCastShape(s, 50, 0, validBox); ok {
			t.Fatalf("badShapes[%d] unexpectedly hit ShapeCastShape: %+v", i, h)
		}
		if h, ok := physics.ShapeCastShape(validBox, 50, 0, s); ok {
			t.Fatalf("badShapes[%d] target unexpectedly hit ShapeCastShape: %+v", i, h)
		}
		if h, ok := w.ShapeCast(s, 50, 0, physics.QueryAll); ok {
			t.Fatalf("badShapes[%d] unexpectedly hit World.ShapeCast: %+v", i, h)
		}
		if hits := w.ShapeCastAll(s, 50, 0, physics.QueryAll); len(hits) != 0 {
			t.Fatalf("badShapes[%d] unexpectedly hit World.ShapeCastAll: %+v", i, hits)
		}
	}

	// RigidWorld with rotated OBB and non-finite poses must not produce NaN hits.
	rw := &physics.RigidWorld{}
	diamond := physics.NewStaticRigidBox(50, 0, 20, 20)
	diamond.Angle = float32(math.Pi / 4)
	badPose := physics.NewStaticRigidBox(nan, 0, 20, 20)
	badPose.Angle = float32(math.Pi / 4)
	badAngle := physics.NewStaticRigidBox(20, 0, 20, 20)
	badAngle.Angle = nan
	rw.AddBody(badPose, badAngle, diamond)

	for i, s := range badShapes {
		if h, ok := rw.ShapeCast(s, 100, 0, physics.LayerAll); ok {
			t.Fatalf("badShapes[%d] unexpectedly hit RigidWorld.ShapeCast: %+v", i, h)
		}
	}
	// Valid cast skips badPose and badAngle and hits diamond cleanly.
	h, ok := rw.ShapeCast(physics.BoxShape(physics.Box{X: -5, Y: -5, W: 10, H: 10}), 100, 0, physics.LayerAll)
	if !ok || h.RigidBody != diamond {
		t.Fatalf("valid RigidWorld cast after bad bodies = (%+v, %v), want diamond", h, ok)
	}
}

func TestShapeCastTileGridOriginNegative(t *testing.T) {
	grid := physics.NewGrid(6, 4, 16, 16)
	grid.OriginX = -80
	grid.OriginY = -48
	// Place a solid tile at col 4, row 2 -> world [ -16, 0 ) x [ -16, 0 ).
	grid.Set(4, 2, physics.TileSolid)

	w := &physics.World{Tiles: grid}
	// Sweep a 10x10 box from (-66, -13) rightward by 100 -> right edge starts at -56, hits x=-16 after 40 px -> frac=0.4.
	hit, ok := w.ShapeCast(physics.BoxShape(physics.Box{X: -66, Y: -13, W: 10, H: 10}), 100, 0, physics.QueryTiles)
	if !ok || !hit.Tile || hit.Col != 4 || hit.Row != 2 {
		t.Fatalf("offset grid hit = (%+v, %v), want tile (4, 2)", hit, ok)
	}
	if !nearF32(hit.Fraction, 0.4, 1e-5) || !nearF32(float32(hit.Point.X), -16, 1e-4) || hit.NormalX != -1 {
		t.Fatalf("offset grid hit details = %+v", hit)
	}
}

func TestShapeCastOneWayTiles(t *testing.T) {
	grid := physics.NewGrid(4, 4, 16, 16)
	grid.FillRect(0, 2, 4, 1, physics.TileOneWay) // top edge at y=32
	w := &physics.World{Tiles: grid}

	// Falling from above (bottom at y=16 <= 32) hits the one-way tile at y=32 -> frac = 16/40 = 0.4.
	box := physics.BoxShape(physics.Box{X: 8, Y: 4, W: 12, H: 12})
	h, ok := w.ShapeCast(box, 0, 40, physics.QueryOneWay)
	if !ok || !h.Tile || h.Row != 2 || h.TileFlags != physics.TileOneWay || !nearF32(h.Fraction, 0.4, 1e-5) || h.NormalY != -1 {
		t.Fatalf("downward one-way = (%+v, %v), want row=2 frac=0.4 normal=(0,-1)", h, ok)
	}

	// QueryTiles alone ignores TileOneWay.
	if _, ok := w.ShapeCast(box, 0, 40, physics.QueryTiles); ok {
		t.Fatal("QueryTiles should not hit TileOneWay")
	}

	// Rising from below passes through TileOneWay.
	below := physics.BoxShape(physics.Box{X: 8, Y: 50, W: 12, H: 12})
	if h, ok := w.ShapeCast(below, 0, -40, physics.QueryOneWay); ok {
		t.Fatalf("upward cast should pass through one-way tile, got %+v", h)
	}
}

func TestShapeCastLayerAndIgnoreBody(t *testing.T) {
	const (
		layerPlayer = physics.LayerMask(1 << 0)
		layerEnemy  = physics.LayerMask(1 << 1)
		layerWall   = physics.LayerMask(1 << 2)
	)
	w := &physics.World{}
	player := physics.NewBoxBody(0, 0, 10, 10)
	player.Layer = layerPlayer
	enemy := physics.NewBoxBody(40, 0, 10, 10)
	enemy.Layer = layerEnemy
	wall := physics.NewBoxBody(80, 0, 10, 10)
	wall.Layer = layerWall
	w.Add(player, enemy, wall)

	// Without ignoring player, starting at player's box hits player at Fraction == 0.
	h, ok := w.ShapeCast(player.Shape, 100, 0, physics.QueryBodies)
	if !ok || h.Body != player || h.Fraction != 0 {
		t.Fatalf("unfiltered cast = (%+v, %v), want player at 0", h, ok)
	}

	// Ignoring player hits enemy at x=40 (box right=10 -> travel 30 / 100 = 0.3).
	h, ok = w.ShapeCastWith(player.Shape, 100, 0, physics.ShapeCastOptions{
		QueryMask:  physics.QueryBodies,
		IgnoreBody: player,
	})
	if !ok || h.Body != enemy || !nearF32(h.Fraction, 0.3, 1e-5) {
		t.Fatalf("ignore player = (%+v, %v), want enemy at 0.3", h, ok)
	}

	// Filtering to layerWall skips player and enemy, hitting wall at 0.7.
	h, ok = w.ShapeCastLayer(player.Shape, 100, 0, physics.QueryBodies, layerWall)
	if !ok || h.Body != wall || !nearF32(h.Fraction, 0.7, 1e-5) {
		t.Fatalf("layerWall = (%+v, %v), want wall at 0.7", h, ok)
	}
}

func TestShapeCastRigidWorld(t *testing.T) {
	rw := &physics.RigidWorld{}
	self := physics.NewRigidBox(0, 0, 10, 10, 1)
	self.Tag = "self"
	self.Layer = physics.LayerBit(0)

	// Rotated 45-degree static diamond centered at (50, 0), size 20x20 -> half-diagonal = 10*sqrt(2).
	diamond := physics.NewStaticRigidBox(50, 0, 20, 20)
	diamond.Tag = "diamond"
	diamond.Angle = float32(math.Pi / 4)
	diamond.Layer = physics.LayerBit(1)

	circle := physics.NewStaticRigidCircle(90, 0, 10)
	circle.Tag = "circle"
	circle.Layer = physics.LayerBit(2)

	rw.AddBody(self, diamond, circle)

	// Cast a 10x10 box from self rightward by 100, ignoring self.
	// Box right edge starts at 5, diamond leftmost vertex is at 50 - 10*sqrt(2).
	// Travel distance = (50 - 10*sqrt(2)) - 5 = 45 - 10*sqrt(2).
	wantFrac := float32((45.0 - 10.0*math.Sqrt2) / 100.0)
	h, ok := rw.ShapeCastWith(self.Shape, 100, 0, physics.ShapeCastOptions{IgnoreRigidBody: self})
	if !ok || h.RigidBody != diamond {
		t.Fatalf("rigid box vs OBB = (%+v, %v), want diamond", h, ok)
	}
	if !nearF32(h.Fraction, wantFrac, 1e-4) {
		t.Fatalf("rigid box vs OBB fraction = %v, want %v", h.Fraction, wantFrac)
	}

	// Cast a circle (r=5 at (0, 0)) rightward by 100 against diamond (ignoring self).
	// Circle hits diamond's leftmost vertex at x = 50 - 10*sqrt(2) - 5.
	wantCircleFrac := float32((45.0 - 10.0*math.Sqrt2) / 100.0)
	ch, ok := rw.ShapeCastWith(physics.CircleShape(physics.Circle{X: 0, Y: 0, R: 5}), 100, 0, physics.ShapeCastOptions{IgnoreRigidBody: self})
	if !ok || ch.RigidBody != diamond || !nearF32(ch.Fraction, wantCircleFrac, 1e-4) {
		t.Fatalf("rigid circle vs OBB = (%+v, %v), want diamond frac=%v", ch, ok, wantCircleFrac)
	}

	// Layer filter for circle only (skips self and diamond).
	lh, ok := rw.ShapeCast(self.Shape, 100, 0, physics.LayerBit(2))
	if !ok || lh.RigidBody != circle || !nearF32(lh.Fraction, 0.75, 1e-5) {
		t.Fatalf("rigid layer filter = (%+v, %v), want circle at 0.75", lh, ok)
	}

	// ShapeCastAll returns diamond then circle when ignoring self.
	all := rw.ShapeCastAllWith(self.Shape, 100, 0, physics.ShapeCastOptions{IgnoreRigidBody: self})
	if len(all) != 2 || all[0].RigidBody != diamond || all[1].RigidBody != circle {
		t.Fatalf("rigid ShapeCastAll = %+v, want [diamond, circle]", all)
	}
}

func TestShapeCastDeterminism(t *testing.T) {
	// Two stacked bodies and two stacked tiles all aligned at x=48 -> exact Fraction tie.
	grid := physics.NewGrid(6, 4, 16, 16)
	grid.Set(3, 1, physics.TileSolid) // x=48, y=16..32
	grid.Set(3, 2, physics.TileSolid) // x=48, y=32..48

	b1 := physics.NewBoxBody(48, 16, 16, 16)
	b1.Tag = "b1"
	b2 := physics.NewBoxBody(48, 32, 16, 16)
	b2.Tag = "b2"
	w := &physics.World{Tiles: grid}
	w.Add(b1, b2)

	castBox := physics.BoxShape(physics.Box{X: 0, Y: 20, W: 16, H: 24})
	baseline := w.ShapeCastAll(castBox, 100, 0, physics.QueryAll)
	if len(baseline) != 4 {
		t.Fatalf("expected 4 tied hits, got %d: %+v", len(baseline), baseline)
	}
	// Deterministic order: bodies in World.Bodies order (b1, b2), then tiles in row-major order ((3,1), (3,2)).
	if baseline[0].Body != b1 || baseline[1].Body != b2 ||
		!baseline[2].Tile || baseline[2].Col != 3 || baseline[2].Row != 1 ||
		!baseline[3].Tile || baseline[3].Col != 3 || baseline[3].Row != 2 {
		t.Fatalf("unexpected tie order: %+v", baseline)
	}

	first, ok := w.ShapeCast(castBox, 100, 0, physics.QueryAll)
	if !ok || first != baseline[0] {
		t.Fatalf("ShapeCast (%+v) != ShapeCastAll[0] (%+v)", first, baseline[0])
	}

	for range 20 {
		got := w.ShapeCastAll(castBox, 100, 0, physics.QueryAll)
		if len(got) != len(baseline) {
			t.Fatalf("non-deterministic length")
		}
		for i := range baseline {
			if got[i] != baseline[i] {
				t.Fatalf("run diverged at %d: %+v != %+v", i, got[i], baseline[i])
			}
		}
	}
}
