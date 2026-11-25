package physics_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
)

func TestTileGridOriginMoveQueryRaycastAndDebugDraw(t *testing.T) {
	grid := physics.NewGrid(4, 3, 16, 16)
	grid.OriginX = -64
	grid.OriginY = 32
	// Solid floor on row 2 (world y in [64, 80)), solid right wall at col 3 (world x in [-16, 0)).
	grid.FillRect(0, 2, 4, 1, physics.TileSolid)
	grid.Set(3, 1, physics.TileSolid)

	// 1. MoveAndSlide lands on floor at y = 64 - 12 = 52 and hits right wall at x = -16 - 10 = -26.
	body := physics.NewBoxBody(-50, 40, 10, 12)
	body.VX = 200
	body.VY = 200
	res := physics.MoveAndSlide(body, grid, 300*time.Millisecond)
	if !res.Grounded || !res.OnRightWall {
		t.Fatalf("MoveAndSlide = %+v, want Grounded & OnRightWall", res)
	}
	if body.Bounds().X != -26 || body.Bounds().Y != 52 {
		t.Fatalf("body bounds = %+v, want X=-26 Y=52", body.Bounds())
	}

	// 2. CharacterBody.Move honors GridOrigin too.
	cb := physics.NewCharacterBody(physics.NewBoxBody(-50, 52, 10, 12))
	cb.Body.VX = 200
	cb.Body.VY = 100
	cres := cb.Move(grid, nil, nil, 300*time.Millisecond)
	if !cres.Grounded || !cres.OnRightWall || cb.Body.Bounds().X != -26 || cb.Body.Bounds().Y != 52 {
		t.Fatalf("CharacterBody.Move = (%+v, bounds=%+v)", cres, cb.Body.Bounds())
	}

	// 3. World.QueryOverlap and World.Raycast honor GridOrigin.
	w := &physics.World{Tiles: grid}
	overlaps := w.QueryOverlap(physics.BoxShape(physics.Box{X: -60, Y: 66, W: 8, H: 8}))
	if len(overlaps) != 1 || !overlaps[0].Tile || overlaps[0].Col != 0 || overlaps[0].Row != 2 {
		t.Fatalf("QueryOverlap = %+v, want tile (0,2)", overlaps)
	}
	rh, ok := w.RaycastFirst(core.Point{X: -56, Y: 56}, core.Point{X: 0, Y: 56}, physics.QueryTiles)
	if !ok || !rh.Tile || rh.Col != 3 || rh.Row != 1 || rh.Point != (core.Point{X: -16, Y: 56}) {
		t.Fatalf("RaycastFirst = (%+v, %v), want col=3 row=1 at (-16,56)", rh, ok)
	}

	// 4. DrawTileGrid and DrawTileContacts emit ops at world coordinates shifted by Origin.
	var d core.Drawing
	physics.DrawTileGrid(&d, grid, core.Bounds{
		Origin: core.Point{X: -64, Y: 32},
		Size:   core.Size{Width: 64, Height: 48},
	}, physics.DefaultDebugStyle())
	rectCount := 0
	for _, op := range d.Ops {
		if op.Kind == core.DrawRect {
			rectCount++
			if op.X < -64 || op.X >= 0 || op.Y < 32 || op.Y >= 80 {
				t.Fatalf("DrawTileGrid rect out of offset bounds: (%v,%v)", op.X, op.Y)
			}
		}
	}
	if rectCount != 5 {
		t.Fatalf("DrawTileGrid drew %d solid rects, want 5", rectCount)
	}
}
