package physics_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func near(a, b, tol float32) bool {
	return math.Abs(float64(a-b)) <= float64(tol)
}

func expectContact(t *testing.T, got physics.Contact, wantNX, wantNY, wantDepth float32) {
	t.Helper()
	if !got.Hit {
		t.Fatalf("expected hit, got no hit")
	}
	if !near(got.NormalX, wantNX, 1e-4) || !near(got.NormalY, wantNY, 1e-4) {
		t.Fatalf("normal = (%v, %v), want (%v, %v)", got.NormalX, got.NormalY, wantNX, wantNY)
	}
	if !near(got.Depth, wantDepth, 1e-4) {
		t.Fatalf("depth = %v, want %v", got.Depth, wantDepth)
	}
	length := math.Hypot(float64(got.NormalX), float64(got.NormalY))
	if math.Abs(length-1) > 1e-4 {
		t.Fatalf("normal (%v, %v) is not unit length (len=%v)", got.NormalX, got.NormalY, length)
	}
}

func expectSeparated(t *testing.T, a, b physics.Shape, c physics.Contact) {
	t.Helper()
	moved := a.Translated(c.NormalX*c.Depth, c.NormalY*c.Depth)
	after := physics.TestShapes(moved, b)
	if after.Hit && after.Depth > 1e-4 {
		t.Fatalf("after separating by (%v, %v)*%v, shapes still overlap: %+v", c.NormalX, c.NormalY, c.Depth, after)
	}
}

func TestBoxBoxOverlapsAndSeparation(t *testing.T) {
	b := physics.Box{X: 10, Y: 10, W: 20, H: 20}

	// Non-overlapping and touching edges/corners do not hit.
	noHit := []struct {
		name string
		a    physics.Box
	}{
		{"far left", physics.Box{X: -20, Y: 10, W: 10, H: 10}},
		{"far right", physics.Box{X: 40, Y: 10, W: 10, H: 10}},
		{"far above", physics.Box{X: 10, Y: -20, W: 10, H: 10}},
		{"far below", physics.Box{X: 10, Y: 40, W: 10, H: 10}},
		{"touching left edge", physics.Box{X: 0, Y: 10, W: 10, H: 20}},
		{"touching right edge", physics.Box{X: 30, Y: 10, W: 10, H: 20}},
		{"touching top edge", physics.Box{X: 10, Y: 0, W: 20, H: 10}},
		{"touching bottom edge", physics.Box{X: 10, Y: 30, W: 20, H: 10}},
		{"touching corner", physics.Box{X: 30, Y: 30, W: 10, H: 10}},
		{"zero width", physics.Box{X: 15, Y: 15, W: 0, H: 10}},
		{"negative height", physics.Box{X: 15, Y: 15, W: 10, H: -5}},
	}
	for _, tc := range noHit {
		t.Run(tc.name, func(t *testing.T) {
			if c := physics.TestBoxBox(tc.a, b); c.Hit {
				t.Fatalf("expected no hit, got %+v", c)
			}
		})
	}

	// Overlapping from all 4 cardinal directions, nested, and concentric.
	hits := []struct {
		name           string
		a              physics.Box
		nx, ny, depth  float32
		exactSeparated bool
	}{
		{"from left", physics.Box{X: 4, Y: 12, W: 10, H: 16}, -1, 0, 4, true},
		{"from right", physics.Box{X: 27, Y: 12, W: 10, H: 16}, 1, 0, 3, true},
		{"from top", physics.Box{X: 12, Y: 5, W: 16, H: 10}, 0, -1, 5, true},
		{"from bottom", physics.Box{X: 12, Y: 24, W: 16, H: 10}, 0, 1, 6, true},
		{"nested near left", physics.Box{X: 12, Y: 18, W: 4, H: 4}, -1, 0, 6, true},
		{"concentric identical", b, 0, -1, 20, true},
	}
	for _, tc := range hits {
		t.Run(tc.name, func(t *testing.T) {
			c := physics.TestBoxBox(tc.a, b)
			expectContact(t, c, tc.nx, tc.ny, tc.depth)
			moved := tc.a.Translated(c.NormalX*c.Depth, c.NormalY*c.Depth)
			if after := physics.TestBoxBox(moved, b); after.Hit {
				t.Fatalf("still overlapping after separation: %+v", after)
			}
			// Reverse order negates normal for non-tie cases.
			if tc.name != "concentric identical" {
				rev := physics.TestBoxBox(b, tc.a)
				expectContact(t, rev, -tc.nx, -tc.ny, tc.depth)
			}
		})
	}
}

func TestCircleCircleOverlapsAndSeparation(t *testing.T) {
	b := physics.Circle{X: 50, Y: 50, R: 10}

	for _, tc := range []struct {
		name string
		a    physics.Circle
	}{
		{"separated", physics.Circle{X: 80, Y: 50, R: 10}},
		{"touching right", physics.Circle{X: 70, Y: 50, R: 10}},
		{"touching diagonal 3-4-5", physics.Circle{X: 62, Y: 66, R: 10}},
		{"zero radius", physics.Circle{X: 50, Y: 50, R: 0}},
		{"negative radius", physics.Circle{X: 50, Y: 50, R: -3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if c := physics.TestCircleCircle(tc.a, b); c.Hit {
				t.Fatalf("expected no hit, got %+v", c)
			}
		})
	}

	invSqrt2 := float32(1 / math.Sqrt2)
	diagDepth := float32(20 - math.Hypot(6, 6))

	// All 8 directions + concentric.
	for _, tc := range []struct {
		name          string
		a             physics.Circle
		nx, ny, depth float32
	}{
		{"left", physics.Circle{X: 35, Y: 50, R: 10}, -1, 0, 5},
		{"right", physics.Circle{X: 65, Y: 50, R: 10}, 1, 0, 5},
		{"top", physics.Circle{X: 50, Y: 35, R: 10}, 0, -1, 5},
		{"bottom", physics.Circle{X: 50, Y: 65, R: 10}, 0, 1, 5},
		{"top-left", physics.Circle{X: 44, Y: 44, R: 10}, -invSqrt2, -invSqrt2, diagDepth},
		{"top-right", physics.Circle{X: 56, Y: 44, R: 10}, invSqrt2, -invSqrt2, diagDepth},
		{"bottom-left", physics.Circle{X: 44, Y: 56, R: 10}, -invSqrt2, invSqrt2, diagDepth},
		{"bottom-right", physics.Circle{X: 56, Y: 56, R: 10}, invSqrt2, invSqrt2, diagDepth},
		{"3-4-5 diagonal", physics.Circle{X: 56, Y: 58, R: 10}, 0.6, 0.8, 10},
		{"concentric", physics.Circle{X: 50, Y: 50, R: 10}, 0, -1, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := physics.TestCircleCircle(tc.a, b)
			expectContact(t, c, tc.nx, tc.ny, tc.depth)
			expectSeparated(t, physics.CircleShape(tc.a), physics.CircleShape(b), c)
		})
	}
}

func TestCircleBoxAndBoxCircleOverlapsAndSeparation(t *testing.T) {
	box := physics.Box{X: 20, Y: 20, W: 20, H: 20}

	for _, tc := range []struct {
		name string
		c    physics.Circle
	}{
		{"far away", physics.Circle{X: 100, Y: 100, R: 5}},
		{"touching left", physics.Circle{X: 15, Y: 30, R: 5}},
		{"touching right", physics.Circle{X: 45, Y: 30, R: 5}},
		{"touching top", physics.Circle{X: 30, Y: 15, R: 5}},
		{"touching bottom", physics.Circle{X: 30, Y: 45, R: 5}},
		{"touching corner 3-4-5", physics.Circle{X: 17, Y: 16, R: 5}},
		{"zero radius", physics.Circle{X: 30, Y: 30, R: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if c := physics.TestCircleBox(tc.c, box); c.Hit {
				t.Fatalf("TestCircleBox: expected no hit, got %+v", c)
			}
			if c := physics.TestBoxCircle(box, tc.c); c.Hit {
				t.Fatalf("TestBoxCircle: expected no hit, got %+v", c)
			}
		})
	}

	invSqrt2 := float32(1 / math.Sqrt2)
	cornerDepth := float32(10 - math.Hypot(3, 3))

	// All 8 external directions + inside near each face + concentric.
	for _, tc := range []struct {
		name          string
		c             physics.Circle
		nx, ny, depth float32
	}{
		{"left", physics.Circle{X: 16, Y: 30, R: 10}, -1, 0, 6},
		{"right", physics.Circle{X: 44, Y: 30, R: 10}, 1, 0, 6},
		{"top", physics.Circle{X: 30, Y: 16, R: 10}, 0, -1, 6},
		{"bottom", physics.Circle{X: 30, Y: 44, R: 10}, 0, 1, 6},
		{"top-left corner", physics.Circle{X: 17, Y: 17, R: 10}, -invSqrt2, -invSqrt2, cornerDepth},
		{"top-right corner", physics.Circle{X: 43, Y: 17, R: 10}, invSqrt2, -invSqrt2, cornerDepth},
		{"bottom-left corner", physics.Circle{X: 17, Y: 43, R: 10}, -invSqrt2, invSqrt2, cornerDepth},
		{"bottom-right corner", physics.Circle{X: 43, Y: 43, R: 10}, invSqrt2, invSqrt2, cornerDepth},
		{"inside near top", physics.Circle{X: 30, Y: 23, R: 5}, 0, -1, 8},
		{"inside near bottom", physics.Circle{X: 30, Y: 37, R: 5}, 0, 1, 8},
		{"inside near left", physics.Circle{X: 23, Y: 30, R: 5}, -1, 0, 8},
		{"inside near right", physics.Circle{X: 37, Y: 30, R: 5}, 1, 0, 8},
		{"center on left edge", physics.Circle{X: 20, Y: 30, R: 5}, -1, 0, 5},
		{"concentric", physics.Circle{X: 30, Y: 30, R: 5}, 0, -1, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cb := physics.TestCircleBox(tc.c, box)
			expectContact(t, cb, tc.nx, tc.ny, tc.depth)
			expectSeparated(t, physics.CircleShape(tc.c), physics.BoxShape(box), cb)

			bc := physics.TestBoxCircle(box, tc.c)
			expectContact(t, bc, -tc.nx, -tc.ny, tc.depth)
			expectSeparated(t, physics.BoxShape(box), physics.CircleShape(tc.c), bc)
		})
	}
}

func TestShapesUniformDispatchAndHelpers(t *testing.T) {
	box := physics.BoxShape(physics.Box{X: 0, Y: 0, W: 10, H: 10})
	circle := physics.CircleShape(physics.Circle{X: 8, Y: 5, R: 4})
	none := physics.Shape{}

	if !none.Empty() || box.Empty() || circle.Empty() {
		t.Fatalf("unexpected Empty results")
	}
	if physics.TestShapes(none, box).Hit || physics.TestShapes(circle, none).Hit {
		t.Fatalf("ShapeNone should never hit")
	}
	if got := circle.Bounds(); got != (physics.Box{X: 4, Y: 1, W: 8, H: 8}) {
		t.Fatalf("circle bounds = %+v", got)
	}
	if !box.Contains(10, 10) || box.Contains(10.1, 5) {
		t.Fatalf("unexpected box.Contains")
	}
	if !circle.Contains(12, 5) || circle.Contains(12.1, 5) {
		t.Fatalf("unexpected circle.Contains")
	}
	c := physics.TestShapes(box, circle)
	// Circle center (8,5) is 2 units inside box's right edge (10), plus radius 4 => depth 6.
	expectContact(t, c, -1, 0, 6)
	expectSeparated(t, box, circle, c)
}

func TestLoopDrivenGravityLandsDeterministicallyOnSolidAndOneWay(t *testing.T) {
	const (
		tileSize = 16
		gravity  = 600 // px/s^2
	)
	makeLevel := func() *physics.Grid {
		// 10x10 grid: solid floor on row 8, one-way platform on row 5 (cols 2..6).
		g := physics.NewGrid(10, 10, tileSize, tileSize)
		g.FillRect(0, 8, 10, 1, physics.TileSolid)
		g.FillRect(2, 5, 5, 1, physics.TileOneWay)
		return g
	}

	simulate := func(t *testing.T, startX, startY float32, oneWayDrop bool) (finalY float32, grounded bool) {
		t.Helper()
		grid := makeLevel()
		body := physics.NewBoxBody(startX, startY, 12, 16)
		body.OneWayDrop = oneWayDrop

		loop := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				body.VY += gravity * float32(dt.Seconds())
				physics.MoveAndSlide(body, grid, dt)
			},
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop.Run(cx)
			return core.View{}
		})
		app.Advance(time.Second)
		return body.Shape.Box.Y, body.Grounded
	}

	// Falls onto solid floor (row 8 => y = 128; body height 16 => y = 112).
	y1, g1 := simulate(t, 16, 16, false)
	y2, g2 := simulate(t, 16, 16, false)
	if !g1 || !near(y1, 112, 1e-4) {
		t.Fatalf("solid floor landing: y=%v grounded=%v, want y=112 grounded=true", y1, g1)
	}
	if y1 != y2 || g1 != g2 {
		t.Fatalf("simulation was not deterministic: (%v,%v) vs (%v,%v)", y1, g1, y2, g2)
	}

	// Falls onto one-way platform (row 5 => y = 80; body height 16 => y = 64).
	py, pg := simulate(t, 48, 16, false)
	if !pg || !near(py, 64, 1e-4) {
		t.Fatalf("one-way landing: y=%v grounded=%v, want y=64 grounded=true", py, pg)
	}

	// With OneWayDrop set, drops straight through row 5 and lands on solid floor at row 8 (y = 112).
	dy, dg := simulate(t, 48, 16, true)
	if !dg || !near(dy, 112, 1e-4) {
		t.Fatalf("one-way drop: y=%v grounded=%v, want y=112 grounded=true", dy, dg)
	}
}

func TestJumpUpThroughOneWayPlatformAndLandOnTop(t *testing.T) {
	grid := physics.NewGrid(10, 10, 16, 16)
	grid.FillRect(0, 8, 10, 1, physics.TileSolid) // floor y = 128
	grid.FillRect(2, 5, 5, 1, physics.TileOneWay) // platform y = 80

	body := physics.NewBoxBody(48, 112, 12, 16) // standing on floor
	body.VY = -360                              // strong jump to clear y=80

	dt := time.Second / 60
	passedThrough := false
	for range 120 {
		body.VY += 600 * float32(dt.Seconds())
		res := physics.MoveAndSlide(body, grid, dt)
		if body.Shape.Box.Bottom() < 80 {
			passedThrough = true
		}
		if res.OnCeiling {
			t.Fatalf("one-way platform should never block upward jump")
		}
	}
	if !passedThrough {
		t.Fatalf("body never cleared the one-way platform")
	}
	if !body.Grounded || !near(body.Shape.Box.Y, 64, 1e-4) {
		t.Fatalf("after jumping through one-way platform: y=%v grounded=%v, want y=64 grounded=true", body.Shape.Box.Y, body.Grounded)
	}

	// Weak jump that peaks mid-platform should fall back to the floor without snagging.
	weak := physics.NewBoxBody(48, 112, 12, 16)
	weak.VY = -180
	for range 120 {
		weak.VY += 600 * float32(dt.Seconds())
		physics.MoveAndSlide(weak, grid, dt)
	}
	if !weak.Grounded || !near(weak.Shape.Box.Y, 112, 1e-4) {
		t.Fatalf("weak jump should return to solid floor y=112, got y=%v grounded=%v", weak.Shape.Box.Y, weak.Grounded)
	}
}

func TestMoveAndSlideWallsCeilingAndAntiTunneling(t *testing.T) {
	// Enclosed room: walls at col 0 and col 9, ceiling at row 0, floor at row 9.
	// Single-tile interior wall at col 5 (rows 1..8) to verify anti-tunneling.
	grid := physics.NewGrid(10, 10, 16, 16)
	grid.FillRect(0, 0, 10, 1, physics.TileSolid) // ceiling y=0..16
	grid.FillRect(0, 9, 10, 1, physics.TileSolid) // floor y=144..160
	grid.FillRect(0, 0, 1, 10, physics.TileSolid) // left wall x=0..16
	grid.FillRect(9, 0, 1, 10, physics.TileSolid) // right wall x=144..160
	grid.FillRect(5, 1, 1, 8, physics.TileSolid)  // 1-tile thin wall at x=80..96

	dt := time.Second / 60

	// High-speed shot to the right (5400 px/s => 90 px/step across a 16 px wall).
	fast := physics.NewBoxBody(24, 64, 12, 12)
	fast.VX = 5400
	res := physics.MoveAndSlide(fast, grid, dt)
	if !res.OnRightWall || !fast.OnRightWall {
		t.Fatalf("expected OnRightWall, got %+v", res)
	}
	if fast.VX != 0 {
		t.Fatalf("expected VX=0 after hitting wall, got %v", fast.VX)
	}
	if !near(fast.Shape.Box.X, 80-12, 1e-4) {
		t.Fatalf("body tunneled or misaligned: x=%v, want 68", fast.Shape.Box.X)
	}

	// Hit left wall while moving diagonally up-left.
	body := physics.NewBoxBody(24, 24, 12, 12)
	body.VX = -1200
	body.VY = -1200
	res = physics.MoveAndSlide(body, grid, dt)
	if !res.OnLeftWall || !res.OnCeiling {
		t.Fatalf("expected OnLeftWall and OnCeiling, got %+v", res)
	}
	if body.VX != 0 || body.VY != 0 {
		t.Fatalf("expected zeroed velocities, got (%v, %v)", body.VX, body.VY)
	}
	if !near(body.Shape.Box.X, 16, 1e-4) || !near(body.Shape.Box.Y, 16, 1e-4) {
		t.Fatalf("corner position = (%v, %v), want (16, 16)", body.Shape.Box.X, body.Shape.Box.Y)
	}

	// Fractional body size sliding smoothly along solid floor without snagging.
	slider := physics.NewBoxBody(20, 144-10.3, 10.3, 10.3)
	for range 10 {
		slider.VX = 120
		slider.VY = 100
		r := physics.MoveAndSlide(slider, grid, dt)
		if r.OnRightWall || r.OnLeftWall {
			t.Fatalf("fractional body snagged on floor seam at x=%v", slider.Shape.Box.X)
		}
		if !r.Grounded {
			t.Fatalf("expected slider to stay grounded")
		}
	}

	// Circle body landing on floor and hitting thin wall.
	ball := physics.NewCircleBody(40, 100, 6)
	ball.VX = 6000
	ball.VY = 6000
	res = physics.MoveAndSlide(ball, grid, dt)
	if !res.OnRightWall || !res.Grounded {
		t.Fatalf("expected circle body to hit right wall and floor, got %+v", res)
	}
	if !near(ball.Shape.Circle.X, 80-6, 1e-4) || !near(ball.Shape.Circle.Y, 144-6, 1e-4) {
		t.Fatalf("circle center = (%v, %v), want (74, 138)", ball.Shape.Circle.X, ball.Shape.Circle.Y)
	}
}

// customGrid verifies TileGrid can be implemented directly by any external type (such as tilemap.Tilemap).
type customGrid struct{}

func (customGrid) TileSize() (float32, float32) { return 16, 16 }
func (customGrid) GridSize() (int, int)         { return 4, 4 }
func (customGrid) TileFlagsAt(col, row int) physics.TileFlags {
	if row == 3 {
		return physics.TileSolid
	}
	return 0
}

func TestCustomTileGridInterface(t *testing.T) {
	b := physics.NewBoxBody(16, 16, 16, 16)
	b.VY = 600
	res := physics.MoveAndSlide(b, customGrid{}, 100*time.Millisecond)
	if !res.Grounded || !near(b.Shape.Box.Y, 32, 1e-4) {
		t.Fatalf("custom TileGrid failed: y=%v grounded=%v", b.Shape.Box.Y, res.Grounded)
	}
}

func TestWorldQueryPointAndOverlap(t *testing.T) {
	grid := physics.NewGrid(6, 6, 16, 16)
	grid.Set(1, 1, physics.TileSolid)
	grid.Set(2, 1, physics.TileOneWay)

	b1 := physics.NewBoxBody(10, 10, 20, 20)
	b1.ID = 1
	b1.Tag = "box"
	b2 := physics.NewCircleBody(25, 25, 10)
	b2.ID = 2
	b2.Tag = "circle"
	b3 := physics.NewBoxBody(100, 100, 10, 10)
	b3.ID = 3

	var w physics.World
	w.Tiles = grid
	w.Add(b1, b2, b3)

	// QueryPoint returns matching bodies in World.Bodies order.
	ptHits := w.QueryPoint(22, 22)
	if len(ptHits) != 2 || ptHits[0] != b1 || ptHits[1] != b2 {
		t.Fatalf("QueryPoint(22, 22) = %v, want [b1, b2]", ptHits)
	}
	if len(w.QueryPoint(0, 0)) != 0 {
		t.Fatalf("QueryPoint(0, 0) should be empty")
	}

	// QueryOverlap returns bodies in order followed by tiles in row-major order.
	probe := physics.BoxShape(physics.Box{X: 18, Y: 18, W: 20, H: 10})
	ov := w.QueryOverlap(probe)
	if len(ov) != 4 {
		t.Fatalf("QueryOverlap returned %d hits, want 4: %+v", len(ov), ov)
	}
	if ov[0].Body != b1 || ov[1].Body != b2 {
		t.Fatalf("expected bodies b1, b2 first, got %+v, %+v", ov[0], ov[1])
	}
	if !ov[2].Tile || ov[2].Col != 1 || ov[2].Row != 1 || ov[2].TileFlags != physics.TileSolid {
		t.Fatalf("expected solid tile (1,1) at index 2, got %+v", ov[2])
	}
	if !ov[3].Tile || ov[3].Col != 2 || ov[3].Row != 1 || ov[3].TileFlags != physics.TileOneWay {
		t.Fatalf("expected one-way tile (2,1) at index 3, got %+v", ov[3])
	}

	// Masking excludes unwanted layers.
	onlySolid := w.QueryOverlapMask(probe, physics.QueryTiles)
	if len(onlySolid) != 1 || !onlySolid[0].Tile || onlySolid[0].Col != 1 {
		t.Fatalf("QueryTiles mask returned %+v", onlySolid)
	}

	// Remove preserves order of remaining bodies.
	if !w.Remove(b1) || w.Remove(b1) {
		t.Fatalf("Remove returned unexpected result")
	}
	ptHits = w.QueryPoint(22, 22)
	if len(ptHits) != 1 || ptHits[0] != b2 {
		t.Fatalf("after Remove(b1), QueryPoint = %v, want [b2]", ptHits)
	}
}

func TestWorldRaycastBodiesAndTilesSortedByDistance(t *testing.T) {
	// Grid: 10x4 of 16x16 tiles (160x64 world).
	// Solid tiles at (2, 1) [x=32..48] and (6, 1) [x=96..112].
	grid := physics.NewGrid(10, 4, 16, 16)
	grid.Set(2, 1, physics.TileSolid)
	grid.Set(6, 1, physics.TileSolid)

	// Circle body centered at (20, 24) R=4 => left edge at x=16.
	cBody := physics.NewCircleBody(20, 24, 4)
	cBody.Tag = "circle"
	// Box body at x=64..80, y=16..32.
	bBody := physics.NewBoxBody(64, 16, 16, 16)
	bBody.Tag = "box"

	w := physics.World{
		Bodies: []*physics.Body{bBody, cBody}, // intentionally reversed vs ray distance
		Tiles:  grid,
	}

	// Ray from (0, 24) to (128, 24) along y=24 (length 128).
	// Expected hits in exact distance order:
	// 1. circle at x=16  => T = 16/128 = 0.125, normal (-1, 0)
	// 2. tile (2,1) x=32 => T = 32/128 = 0.25,  normal (-1, 0)
	// 3. box at x=64     => T = 64/128 = 0.5,   normal (-1, 0)
	// 4. tile (6,1) x=96 => T = 96/128 = 0.75,  normal (-1, 0)
	from := core.Point{X: 0, Y: 24}
	to := core.Point{X: 128, Y: 24}
	hits := w.Raycast(from, to, physics.QueryAll)
	if len(hits) != 4 {
		t.Fatalf("got %d hits, want 4: %+v", len(hits), hits)
	}

	wantT := []float32{0.125, 0.25, 0.5, 0.75}
	wantX := []float32{16, 32, 64, 96}
	for i, h := range hits {
		if !near(h.T, wantT[i], 1e-4) || !near(float32(h.Point.X), wantX[i], 1e-4) || !near(float32(h.Point.Y), 24, 1e-4) {
			t.Fatalf("hit %d: T=%v Point=(%v,%v), want T=%v Point=(%v,24)", i, h.T, h.Point.X, h.Point.Y, wantT[i], wantX[i])
		}
		if !near(h.NormalX, -1, 1e-4) || !near(h.NormalY, 0, 1e-4) {
			t.Fatalf("hit %d: normal=(%v,%v), want (-1, 0)", i, h.NormalX, h.NormalY)
		}
	}
	if hits[0].Body != cBody || !hits[1].Tile || hits[1].Col != 2 || hits[2].Body != bBody || !hits[3].Tile || hits[3].Col != 6 {
		t.Fatalf("unexpected hit targets: %+v", hits)
	}

	// RaycastFirst returns the closest hit.
	first, ok := w.RaycastFirst(from, to, physics.QueryAll)
	if !ok || first.Body != cBody || !near(first.T, 0.125, 1e-4) {
		t.Fatalf("RaycastFirst = %+v, %v", first, ok)
	}

	// Mask filtering: tiles only.
	tileFirst, ok := w.RaycastFirst(from, to, physics.QueryTiles)
	if !ok || !tileFirst.Tile || tileFirst.Col != 2 || !near(tileFirst.T, 0.25, 1e-4) {
		t.Fatalf("RaycastFirst(QueryTiles) = %+v, %v", tileFirst, ok)
	}

	// Reverse ray from outside the right edge of the grid (tests grid entry clamping).
	revFrom := core.Point{X: 200, Y: 24}
	revTo := core.Point{X: 100, Y: 24}
	revFirst, ok := w.RaycastFirst(revFrom, revTo, physics.QueryTiles)
	if !ok || !revFirst.Tile || revFirst.Col != 6 || !near(float32(revFirst.Point.X), 112, 1e-4) || !near(revFirst.NormalX, 1, 1e-4) {
		t.Fatalf("reverse RaycastFirst = %+v, %v", revFirst, ok)
	}
}

func TestWorldRaycastOneWayAndEdgeCases(t *testing.T) {
	grid := physics.NewGrid(4, 4, 16, 16)
	grid.Set(1, 1, physics.TileOneWay) // x=16..32, y=16..32
	grid.Set(1, 3, physics.TileSolid)  // x=16..32, y=48..64

	w := physics.World{Tiles: grid}

	// Downward ray hits one-way top face at y=16, then solid floor at y=48.
	downHits := w.Raycast(core.Point{X: 24, Y: 0}, core.Point{X: 24, Y: 64}, physics.QueryAll)
	if len(downHits) != 2 {
		t.Fatalf("downward ray hits = %+v, want 2", downHits)
	}
	if downHits[0].Row != 1 || downHits[0].TileFlags != physics.TileOneWay || downHits[0].NormalY != -1 || !near(float32(downHits[0].Point.Y), 16, 1e-4) {
		t.Fatalf("hit 0 = %+v", downHits[0])
	}
	if downHits[1].Row != 3 || downHits[1].TileFlags != physics.TileSolid || downHits[1].NormalY != -1 || !near(float32(downHits[1].Point.Y), 48, 1e-4) {
		t.Fatalf("hit 1 = %+v", downHits[1])
	}

	// Upward ray passes straight through the one-way tile without hitting it.
	upHits := w.Raycast(core.Point{X: 24, Y: 40}, core.Point{X: 24, Y: 0}, physics.QueryAll)
	if len(upHits) != 0 {
		t.Fatalf("upward ray should pass through one-way tile, got %+v", upHits)
	}

	// Horizontal ray through the middle of the one-way tile passes through.
	horizHits := w.Raycast(core.Point{X: 0, Y: 24}, core.Point{X: 64, Y: 24}, physics.QueryAll)
	if len(horizHits) != 0 {
		t.Fatalf("horizontal ray should pass through one-way tile, got %+v", horizHits)
	}

	// Ray entering grid from outside on the right with non-power-of-two denominator (tests float rounding).
	grid2 := physics.NewGrid(2, 1, 16, 16)
	grid2.Set(1, 0, physics.TileSolid)
	w2 := physics.World{Tiles: grid2}
	hit, ok := w2.RaycastFirst(core.Point{X: 100, Y: 8}, core.Point{X: 10, Y: 8}, physics.QueryTiles)
	if !ok || !hit.Tile || hit.Col != 1 || !near(float32(hit.Point.X), 32, 1e-4) || hit.NormalX != 1 {
		t.Fatalf("right-entry raycast missed or wrong: %+v, %v", hit, ok)
	}

	// Ray starting inside a box or circle hits at T=0 with zero normal; starting on exit edge pointing away misses.
	boxBody := physics.NewBoxBody(0, 0, 10, 10)
	circleBody := physics.NewCircleBody(30, 5, 5)
	w3 := physics.World{Bodies: []*physics.Body{boxBody, circleBody}}

	if h, ok := w3.RaycastFirst(core.Point{X: 5, Y: 5}, core.Point{X: 20, Y: 5}, physics.QueryBodies); !ok || h.Body != boxBody || h.T != 0 || h.NormalX != 0 || h.NormalY != 0 {
		t.Fatalf("inside-box raycast = %+v, %v", h, ok)
	}
	if _, ok := w3.RaycastFirst(core.Point{X: 10, Y: 5}, core.Point{X: 20, Y: 5}, physics.QueryBodies); ok {
		t.Fatalf("ray starting on exit edge moving away should not hit")
	}
	if h, ok := w3.RaycastFirst(core.Point{X: 30, Y: 5}, core.Point{X: 40, Y: 5}, physics.QueryBodies); !ok || h.Body != circleBody || h.T != 0 {
		t.Fatalf("inside-circle raycast = %+v, %v", h, ok)
	}

	// Zero mask defaults to QueryAll, and diagonal DDA visits tiles in strict distance order.
	diagGrid := physics.NewGrid(4, 4, 16, 16)
	diagGrid.Set(1, 1, physics.TileSolid)
	diagGrid.Set(2, 2, physics.TileSolid)
	w4 := physics.World{Tiles: diagGrid}
	diagHits := w4.Raycast(core.Point{X: 0, Y: 0}, core.Point{X: 64, Y: 64}, 0)
	if len(diagHits) != 2 {
		t.Fatalf("diagonal raycast hits = %+v, want 2", diagHits)
	}
	if diagHits[0].Col != 1 || diagHits[0].Row != 1 || !near(diagHits[0].T, 0.25, 1e-4) {
		t.Fatalf("diagonal hit 0 = %+v", diagHits[0])
	}
	if diagHits[1].Col != 2 || diagHits[1].Row != 2 || !near(diagHits[1].T, 0.5, 1e-4) {
		t.Fatalf("diagonal hit 1 = %+v", diagHits[1])
	}
}
