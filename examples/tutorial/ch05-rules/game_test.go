package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// start runs the game with seed in a 240 by 320 window, where a view point is a playfield point.
func start(t *testing.T, seed uint64) (*nsgotest.App, *starCatcher) {
	t.Helper()
	var s *starCatcher
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx, seed)
		return s.view()
	}, nsgotest.Size(240, 320))
	return app, s
}

func TestHUDCountsCatches(t *testing.T) {
	app, s := start(t, 1)
	app.Find("text:Score: 0")
	app.Find("text:Lives: 3")

	s.world.spawnIn = time.Hour
	s.world.drop(kindStar, s.world.basketLane, basketY-itemSize)
	app.Advance(time.Second / 2)
	app.Find("text:Score: 1")
}

func TestCanvasDrawsEveryItem(t *testing.T) {
	app, s := start(t, 1)
	s.world.spawnIn = time.Hour
	s.world.drop(kindStar, 0, 40)
	s.world.drop(kindRock, 4, 80)
	app.Advance(step)

	if images := len(gametest.ImageDraws(app.Find("game-canvas").Ops)); images != 3 {
		t.Fatalf("canvas drew %d sprites, want the basket, a star and a rock", images)
	}
}

func TestGameOverAndPlayAgain(t *testing.T) {
	app, s := start(t, 1)
	s.world.spawnIn = time.Hour
	s.world.lives = 1
	s.world.drop(kindStar, 0, groundY-itemSize)
	app.Advance(time.Second / 2)
	app.Find("text:Game over")
	app.Find("text:Lives: 0")

	app.Key("enter")
	app.Advance(step * 2)
	if app.Exists("game-over") {
		t.Fatal("game over panel still shows after Enter")
	}
	app.Find("text:Lives: 3")
	app.Find("text:Score: 0")
}

func TestTapAfterGameOverPlaysAgain(t *testing.T) {
	app, s := start(t, 1)
	s.world.spawnIn = time.Hour
	s.world.lives = 1
	s.world.drop(kindStar, 0, groundY-itemSize)
	app.Advance(time.Second / 2)
	app.Find("text:Game over")

	finger := app.Touch("game-view", core.Point{X: 40, Y: 160})
	app.Advance(step * 2)
	finger.Lift()
	app.Advance(step * 2)
	if app.Exists("game-over") {
		t.Fatal("game over panel still shows after a tap")
	}
	if s.world.basketLane != laneCount/2 {
		t.Fatalf("the tap that started the game also moved the basket to lane %d", s.world.basketLane)
	}
}
