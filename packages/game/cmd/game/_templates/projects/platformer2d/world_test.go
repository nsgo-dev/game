package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/platformer"
	"github.com/nsgo-dev/game/packages/game/rooms"
)

const step = time.Second / 60

var (
	runRight = platformer.Input{Move: 1}
	standBy  = platformer.Input{}
)

// testWorld builds a run of a level made of tiles, in a single room, with the default controller.
func testWorld(t *testing.T, tiles string, lives int) *world {
	t.Helper()
	l := &Level{Version: levelVersion, Name: "Test", TileSize: 16, Lives: lives, Tiles: tiles}
	if err := l.validate(); err != nil {
		t.Fatal(err)
	}
	rows := l.rows()
	m := &rooms.Map{Version: 1, Rooms: []rooms.Room{{ID: "all", Width: float32(len(rows[0]) * 16), Height: float32(len(rows) * 16)}}}
	w, err := newWorld(gameData{level: l, rooms: m, player: platformer.DefaultConfig()}, 320, 180)
	if err != nil {
		t.Fatal(err)
	}
	// Let the player settle onto the ground.
	for range 5 {
		w.step(standBy, step)
	}
	return w
}

// stepUntil steps w with in until done reports true, failing after limit steps.
func stepUntil(t *testing.T, w *world, in platformer.Input, limit int, done func() bool) {
	t.Helper()
	for range limit {
		if done() {
			return
		}
		w.step(in, step)
	}
	if !done() {
		t.Fatalf("not done after %d steps: player at %+v, lives %d, state %v", limit, w.body.Bounds(), w.lives, w.state)
	}
}

func TestSpikesCostALifeAndRespawnAtTheStart(t *testing.T) {
	w := testWorld(t, "#........#\n#P.^...G.#\n##########\n", 3)
	start := w.body.Bounds()
	stepUntil(t, w, runRight, 120, func() bool { return w.lives < 3 })
	if w.lives != 2 || w.state != running {
		t.Fatalf("after the spikes: lives %d, state %v, want 2 and running", w.lives, w.state)
	}
	if got := w.body.Bounds(); got.X != start.X || got.Y != start.Y || w.body.VX != 0 {
		t.Fatalf("the player came back at %+v moving %v, want at rest at the start %+v", got, w.body.VX, start)
	}
}

func TestCheckpointMovesTheRespawn(t *testing.T) {
	w := testWorld(t, "#..........#\n#P..C..^.G.#\n############\n", 3)
	stepUntil(t, w, runRight, 120, func() bool { return w.lives < 3 })
	if !w.checkpoints[0].lit {
		t.Fatal("running past the lamp did not light it")
	}
	want := w.standingAt(4, 1)
	if got := w.body.Bounds(); float64(got.X) != float64(want.X) || float64(got.Y) != float64(want.Y) {
		t.Fatalf("the player came back at %+v, want the lamp at %+v", got, want)
	}
}

func TestFallingOutOfTheLevelCostsALife(t *testing.T) {
	w := testWorld(t, "#.......#\n#P.....G#\n###..####\n", 3)
	stepUntil(t, w, runRight, 240, func() bool { return w.lives < 3 })
	if w.lives != 2 {
		t.Fatalf("lives %d after falling into the pit, want 2", w.lives)
	}
}

func TestLosingTheLastLifeLosesTheRun(t *testing.T) {
	w := testWorld(t, "#........#\n#P.^...G.#\n##########\n", 2)
	stepUntil(t, w, runRight, 400, func() bool { return w.state != running })
	if w.state != lost || w.lives != 0 {
		t.Fatalf("state %v with %d lives, want lost with 0", w.state, w.lives)
	}
	at := w.body.Bounds()
	w.step(runRight, step)
	if w.body.Bounds() != at {
		t.Fatal("the player kept moving after the run was lost")
	}
}

func TestReachingTheGoalWinsAndSparksCountOnce(t *testing.T) {
	w := testWorld(t, "#.......#\n#P.o..G.#\n#########\n", 3)
	stepUntil(t, w, runRight, 240, func() bool { return w.state != running })
	if w.state != won || w.collected != 1 || w.sparksTotal() != 1 {
		t.Fatalf("state %v with %d of %d sparks, want won with 1 of 1", w.state, w.collected, w.sparksTotal())
	}
	w.collect(0)
	if w.collected != 1 {
		t.Fatalf("collecting a taken spark again counted it: %d", w.collected)
	}
}

// TestCoyoteTimeAllowsALateJump walks off a ledge and jumps a few steps later, still within the coyote time player.yaml sets, and then too late.
func TestCoyoteTimeAllowsALateJump(t *testing.T) {
	const tiles = "#..........#\n#..........#\n#P.........#\n###.......G#\n###.......##\n###.......##\n############\n"
	for _, tc := range []struct {
		late int
		want platformer.JumpKind
	}{{late: 2, want: platformer.JumpGround}, {late: 12, want: platformer.JumpNone}} {
		w := testWorld(t, tiles, 3)
		stepUntil(t, w, platformer.Input{Move: 0.5}, 120, func() bool { return !w.body.Grounded })
		for range tc.late - 1 {
			w.step(standBy, step)
		}
		w.step(platformer.Input{JumpPressed: true, JumpHeld: true}, step)
		if w.lastJump != tc.want {
			t.Errorf("a jump %d steps after leaving the ledge started %v, want %v", tc.late, w.lastJump, tc.want)
		}
	}
}

// TestJumpBufferJumpsOnLanding presses jump just before the player lands and checks that it jumps as soon as it does.
func TestJumpBufferJumpsOnLanding(t *testing.T) {
	const tiles = "#....#\n#.P..#\n#....#\n#....#\n#....#\n#...G#\n######\n"
	// Find the step the player lands on when it drops from the start, which is in the air.
	dry := testWorld(t, tiles, 3)
	landing := 0
	stepUntil(t, dry, standBy, 120, func() bool {
		landing++
		return dry.body.Grounded
	})
	landing--
	if landing < 5 {
		t.Fatalf("the player landed %d steps after the start, too soon to buffer a jump", landing)
	}

	w := testWorld(t, tiles, 3)
	for range landing - 4 {
		w.step(standBy, step)
	}
	w.step(platformer.Input{JumpPressed: true, JumpHeld: true}, step)
	jumped := false
	for range 6 {
		w.step(platformer.Input{JumpHeld: true}, step)
		jumped = jumped || w.lastJump == platformer.JumpGround
	}
	if !jumped {
		t.Fatal("a jump pressed 4 steps before landing did not jump on landing")
	}
}

// TestEnteringARoomSlidesTheCameraAndFreezesThePlayer runs from the first room of the real level into the second.
func TestEnteringARoomSlidesTheCameraAndFreezesThePlayer(t *testing.T) {
	w, err := newWorld(mustLoadData(), 320, 180)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.director.Room().ID; got != "meadow" {
		t.Fatalf("the run starts in %q, want meadow", got)
	}
	w.placePlayer(w.standingAt(23, 9))
	stepUntil(t, w, runRight, 120, func() bool { return w.director.Room().ID != "meadow" })
	if got := w.director.Room().ID; got != "ravine" || !w.director.Transitioning() {
		t.Fatalf("after crossing x 400: room %q, transitioning %v, want a slide into ravine", got, w.director.Transitioning())
	}
	at := w.body.Bounds()
	w.step(runRight, step)
	if w.body.Bounds() != at {
		t.Fatal("the player moved during the slide")
	}
	stepUntil(t, w, runRight, 60, func() bool { return !w.director.Transitioning() })
	if bounds := w.camera.Bounds; bounds == nil || bounds.Origin.X != 400 {
		t.Fatalf("camera bounds %+v after the slide, want the ravine's", bounds)
	}
}
