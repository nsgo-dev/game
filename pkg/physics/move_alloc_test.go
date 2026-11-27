package physics_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
	"github.com/nsgo-dev/nsgo/core"
)

func makeSlideAllocGrid() *physics.Grid {
	grid := physics.NewGrid(20, 12, 16, 16)
	grid.FillRect(0, 10, 20, 2, physics.TileSolid) // floor top at Y=160
	grid.FillRect(0, 0, 1, 12, physics.TileSolid)  // left wall X=0..16
	grid.FillRect(19, 0, 1, 12, physics.TileSolid) // right wall X=304..320
	return grid
}

func TestMoveAndSlideIntoSemanticsAndBufferReuse(t *testing.T) {
	const dt = time.Second / 60
	grid := makeSlideAllocGrid()

	// Compare MoveAndSlide and MoveAndSlideInto on identical bodies hitting floor + right wall.
	b1 := physics.NewBoxBody(292, 144, 12, 16)
	b1.VX = 80
	b1.VY = 200
	want := physics.MoveAndSlide(b1, grid, dt)

	b2 := physics.NewBoxBody(292, 144, 12, 16)
	b2.VX = 80
	b2.VY = 200
	var got physics.MoveResult
	got.Grounded = false
	got.OnCeiling = true // stale flag must be cleared
	got.Collisions = []physics.SlideCollision{{Tile: true}}
	physics.MoveAndSlideInto(b2, grid, dt, &got)

	if b1.Shape != b2.Shape || b1.VX != b2.VX || b1.VY != b2.VY ||
		b1.Grounded != b2.Grounded || b1.OnRightWall != b2.OnRightWall ||
		b1.OnLeftWall != b2.OnLeftWall || b1.OnCeiling != b2.OnCeiling {
		t.Fatalf("body mismatch: got %+v, want %+v", b2, b1)
	}
	if got.Grounded != want.Grounded || got.OnWall != want.OnWall ||
		got.OnCeiling != want.OnCeiling || got.OnLeftWall != want.OnLeftWall ||
		got.OnRightWall != want.OnRightWall {
		t.Fatalf("flags mismatch: got %+v, want %+v", got, want)
	}
	if len(got.Contacts) != len(want.Contacts) || len(got.Contacts) < 2 {
		t.Fatalf("contacts count = %d, want %d (>=2)", len(got.Contacts), len(want.Contacts))
	}
	for i := range want.Contacts {
		if got.Contacts[i] != want.Contacts[i] {
			t.Fatalf("contact[%d] = %+v, want %+v", i, got.Contacts[i], want.Contacts[i])
		}
	}
	if len(got.Collisions) != 0 {
		t.Fatalf("expected stale Collisions to be cleared, got %d", len(got.Collisions))
	}

	// Reusing got on an airborne step clears flags and contacts while preserving backing capacity.
	prevCap := cap(got.Contacts)
	air := physics.NewBoxBody(80, 40, 12, 16)
	air.VX = 30
	air.VY = 10
	physics.MoveAndSlideInto(air, grid, dt, &got)
	if got.Grounded || got.OnWall || got.OnCeiling || got.OnLeftWall || got.OnRightWall || len(got.Contacts) != 0 {
		t.Fatalf("airborne reuse left stale state: %+v", got)
	}
	if cap(got.Contacts) != prevCap {
		t.Fatalf("backing capacity changed from %d to %d", prevCap, cap(got.Contacts))
	}

	// Nil result still moves the body and updates flags without panicking.
	bNil := physics.NewBoxBody(292, 144, 12, 16)
	bNil.VX = 80
	bNil.VY = 200
	physics.MoveAndSlideInto(bNil, grid, dt, nil)
	if bNil.Shape != b1.Shape || !bNil.Grounded || !bNil.OnRightWall {
		t.Fatalf("nil res mismatch: body=%+v", bNil)
	}
}

func TestMoveAndSlideIntoZeroAllocs(t *testing.T) {
	const dt = time.Second / 60
	grid := makeSlideAllocGrid()

	t.Run("grounded", func(t *testing.T) {
		body := physics.NewBoxBody(64, 144, 12, 16)
		var res physics.MoveResult
		step := func() {
			body.Shape.Box.X = 64
			body.Shape.Box.Y = 144
			body.VX = 50
			body.VY = 200
			physics.MoveAndSlideInto(body, grid, dt, &res)
		}
		step()
		if !res.Grounded || len(res.Contacts) == 0 {
			t.Fatalf("expected grounded contact, got %+v", res)
		}
		gamedebug.AssertZeroAllocs(t, 100, step)
	})

	t.Run("wall-sliding", func(t *testing.T) {
		body := physics.NewBoxBody(292, 48, 12, 16)
		var res physics.MoveResult
		step := func() {
			body.Shape.Box.X = 292
			body.Shape.Box.Y = 48
			body.VX = 120
			body.VY = 80
			physics.MoveAndSlideInto(body, grid, dt, &res)
		}
		step()
		if !res.OnRightWall || !res.OnWall || len(res.Contacts) == 0 {
			t.Fatalf("expected right-wall contact, got %+v", res)
		}
		gamedebug.AssertZeroAllocs(t, 100, step)
	})

	t.Run("airborne", func(t *testing.T) {
		body := physics.NewBoxBody(96, 48, 12, 16)
		var res physics.MoveResult
		step := func() {
			body.Shape.Box.X = 96
			body.Shape.Box.Y = 48
			body.VX = 50
			body.VY = 100
			physics.MoveAndSlideInto(body, grid, dt, &res)
		}
		step()
		if res.Grounded || res.OnWall || len(res.Contacts) != 0 {
			t.Fatalf("expected airborne step with no contacts, got %+v", res)
		}
		gamedebug.AssertZeroAllocs(t, 100, step)
	})

	t.Run("nil-result", func(t *testing.T) {
		body := physics.NewBoxBody(292, 144, 12, 16)
		step := func() {
			body.Shape.Box.X = 292
			body.Shape.Box.Y = 144
			body.VX = 80
			body.VY = 200
			physics.MoveAndSlideInto(body, grid, dt, nil)
		}
		step()
		if !body.Grounded || !body.OnRightWall {
			t.Fatalf("expected grounded and wall flags, got grounded=%v wall=%v", body.Grounded, body.OnRightWall)
		}
		gamedebug.AssertZeroAllocs(t, 100, step)
	})
}

func TestWorldMoveAndSlideZeroAllocs(t *testing.T) {
	const dt = time.Second / 60
	grid := makeSlideAllocGrid()

	w := &physics.World{Tiles: grid}
	wall := physics.NewBoxBody(160, 80, 16, 80)
	player := physics.NewBoxBody(148, 144, 12, 16)
	w.Add(wall, player)

	t.Run("World.MoveAndSlide", func(t *testing.T) {
		step := func() {
			player.Shape.Box.X = 148
			player.Shape.Box.Y = 144
			player.VX = 80
			player.VY = 200
			_ = w.MoveAndSlide(player, dt)
		}
		step()
		if !player.Grounded || !player.OnRightWall {
			t.Fatalf("expected grounded and right wall, got grounded=%v wall=%v", player.Grounded, player.OnRightWall)
		}
		gamedebug.AssertZeroAllocs(t, 100, step)
	})

	t.Run("World.MoveAndSlideInto", func(t *testing.T) {
		var res physics.SlideResult
		step := func() {
			player.Shape.Box.X = 148
			player.Shape.Box.Y = 144
			player.VX = 80
			player.VY = 200
			w.MoveAndSlideInto(player, dt, &res)
		}
		step()
		if !res.OnFloor || !res.OnRightWall || len(res.Collisions) < 2 {
			t.Fatalf("expected floor and wall collisions, got %+v", res)
		}
		gamedebug.AssertZeroAllocs(t, 100, step)

		// Verify caller-owned SlideResult is not overwritten by a subsequent World.MoveAndSlide call.
		saved := append([]physics.SlideCollision(nil), res.Collisions...)
		other := physics.NewBoxBody(40, 40, 12, 16)
		other.VX = 20
		_ = w.MoveAndSlide(other, dt)
		if len(res.Collisions) != len(saved) {
			t.Fatalf("caller SlideResult was mutated by World.MoveAndSlide")
		}
	})
}

func TestCharacterBodyZeroAllocs(t *testing.T) {
	const dt = time.Second / 60
	grid := makeSlideAllocGrid()

	t.Run("tiles-grounded", func(t *testing.T) {
		ch := physics.NewCharacterBody(physics.NewBoxBody(64, 144, 12, 16))
		var last physics.MoveResult
		step := func() {
			ch.Body.Shape.Box.X = 64
			ch.Body.Shape.Box.Y = 144
			ch.Body.VX = 50
			ch.Body.VY = 200
			last = ch.Move(grid, nil, nil, dt)
		}
		step()
		if !last.Grounded || len(last.Contacts) == 0 {
			t.Fatalf("expected grounded contact, got %+v", last)
		}
		gamedebug.AssertZeroAllocs(t, 100, step)
	})

	t.Run("tiles-wall-slide-into", func(t *testing.T) {
		ch := physics.NewCharacterBody(physics.NewBoxBody(292, 48, 12, 16))
		var res physics.MoveResult
		step := func() {
			ch.Body.Shape.Box.X = 292
			ch.Body.Shape.Box.Y = 48
			ch.Body.VX = 120
			ch.Body.VY = 80
			ch.MoveInto(grid, nil, nil, dt, &res)
		}
		step()
		if !res.OnRightWall || len(res.Contacts) == 0 {
			t.Fatalf("expected wall contact, got %+v", res)
		}
		gamedebug.AssertZeroAllocs(t, 100, step)
	})

	t.Run("world-bodies", func(t *testing.T) {
		w := &physics.World{Tiles: grid}
		door := physics.NewBoxBody(160, 80, 16, 80)
		ch := physics.NewCharacterBody(physics.NewBoxBody(148, 144, 12, 16))
		w.Add(door, ch.Body)
		var last physics.MoveResult
		step := func() {
			ch.Body.Shape.Box.X = 148
			ch.Body.Shape.Box.Y = 144
			ch.Body.VX = 80
			ch.Body.VY = 200
			last = ch.MoveWorld(w, nil, nil, dt)
		}
		step()
		if !last.Grounded || !last.OnRightWall || len(last.Collisions) == 0 {
			t.Fatalf("expected world floor + door collisions, got %+v", last)
		}
		gamedebug.AssertZeroAllocs(t, 100, step)
	})

	t.Run("world-with-slopes-and-platforms", func(t *testing.T) {
		w := &physics.World{Tiles: grid}
		door := physics.NewBoxBody(240, 80, 16, 80)
		plat := &physics.MovingPlatform{
			Body:   physics.NewBoxBody(40, 112, 64, 12),
			OneWay: true,
		}
		slopes := []physics.Slope{{A: core.Point{X: 120, Y: 160}, B: core.Point{X: 180, Y: 130}}}
		platforms := []*physics.MovingPlatform{plat}
		ch := physics.NewCharacterBody(physics.NewBoxBody(60, 96, 12, 16))
		w.Add(door, plat.Body, ch.Body)

		var last physics.MoveResult
		step := func() {
			ch.Body.Shape.Box.X = 60
			ch.Body.Shape.Box.Y = 96
			ch.Body.VX = 40
			ch.Body.VY = 120
			plat.DeltaX, plat.DeltaY = 0, 0
			last = ch.MoveWorld(w, slopes, platforms, dt)
		}
		step()
		if !last.Grounded {
			t.Fatalf("expected grounded on platform, got %+v", last)
		}
		gamedebug.AssertZeroAllocs(t, 100, step)
	})
}

func BenchmarkMoveAndSlide(b *testing.B) {
	const dt = time.Second / 60
	grid := makeSlideAllocGrid()
	body := physics.NewBoxBody(64, 144, 12, 16)
	var res physics.MoveResult
	// Warm up one grounded step so res.Contacts has backing capacity.
	body.VX = 50
	body.VY = 200
	physics.MoveAndSlideInto(body, grid, dt, &res)

	b.ReportAllocs()
	for b.Loop() {
		body.Shape.Box.X = 64
		body.Shape.Box.Y = 144
		body.VX = 50
		body.VY = 200
		physics.MoveAndSlideInto(body, grid, dt, &res)
	}
}
