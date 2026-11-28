package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestGameAdvancesAndDrawsSprite(t *testing.T) {
	app := nsgotest.Start(t, Root)
	app.Press("id:first-launch-continue")
	if app.Exists("id:first-launch-accessibility") {
		t.Fatal("the first-launch prompt is still shown after Continue")
	}
	app.Find("text:Score: 0")

	first, ok := spriteDrawOp(app.Find("game-canvas").Ops)
	if !ok {
		t.Fatal("expected initial canvas drawing to include a sprite DrawImageFrom op")
	}

	app.Advance(500 * time.Millisecond)

	later, ok := spriteDrawOp(app.Find("game-canvas").Ops)
	if !ok {
		t.Fatal("expected updated canvas drawing to include a sprite DrawImageFrom op")
	}
	if first == later {
		t.Fatalf("expected sprite draw op to change after advancing time, still %+v", later)
	}
}

func TestTapScoresAndResetClearsScore(t *testing.T) {
	app := nsgotest.Start(t, Root)
	app.Press("id:first-launch-continue")
	app.Find("text:Score: 0")

	app.Press("game-canvas")
	app.Find("text:Score: 1")

	app.Press("game-canvas")
	app.Find("text:Score: 2")

	app.Press("role:button Reset")
	app.Find("text:Score: 0")
}

func TestSpriteStaysInsideArenaAndReversesOnTap(t *testing.T) {
	g := newStarterGame(nil)
	g.resize(200, 160)

	vxBefore := g.vx
	g.tap()
	if g.vx != -vxBefore {
		t.Fatalf("vx after tap = %v, want %v", g.vx, -vxBefore)
	}

	half := float32(spriteSize) / 2
	step := time.Second / 60
	for range 600 {
		g.update(step)
		if g.x < half || g.x > g.width-half || g.y < half || g.y > g.height-half {
			t.Fatalf("sprite left arena: (%v, %v) outside [%v,%v]x[%v,%v]", g.x, g.y, half, g.width-half, half, g.height-half)
		}
	}
}

func spriteDrawOp(ops []core.DrawOp) (core.DrawOp, bool) {
	for _, op := range ops {
		if op.Image != nil {
			return op, true
		}
	}
	return core.DrawOp{}, false
}
