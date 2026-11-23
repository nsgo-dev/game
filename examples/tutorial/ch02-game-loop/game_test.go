package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

const step = time.Second / 60

func TestStarFallsAtFallSpeed(t *testing.T) {
	w := newWorld()
	start := w.starY
	for range 60 {
		w.step(step)
	}
	if got, want := w.starY-start, float32(fallSpeed); !near(got, want) {
		t.Fatalf("star fell %v points in a second, want %v", got, want)
	}
}

func TestStarRespawnsTwoLanesOver(t *testing.T) {
	w := newWorld()
	lanes := []int{w.starLane}
	for len(lanes) < 4 {
		w.step(step)
		if w.starLane != lanes[len(lanes)-1] {
			if w.starY != -itemSize {
				t.Fatalf("star respawned at y %v, want %v", w.starY, -itemSize)
			}
			lanes = append(lanes, w.starLane)
		}
	}
	for i, want := range []int{0, 2, 4, 1} {
		if lanes[i] != want {
			t.Fatalf("lanes = %v, want 0 2 4 1", lanes)
		}
	}
}

func TestStarAtInterpolates(t *testing.T) {
	w := newWorld()
	w.step(step)
	if got := w.starAt(0); got != w.prevY {
		t.Fatalf("starAt(0) = %v, want the previous y %v", got, w.prevY)
	}
	if got := w.starAt(1); got != w.starY {
		t.Fatalf("starAt(1) = %v, want the current y %v", got, w.starY)
	}
	if got, want := w.starAt(0.5), (w.prevY+w.starY)/2; !near(got, want) {
		t.Fatalf("starAt(0.5) = %v, want %v", got, want)
	}
}

func TestLoopMovesTheStarOnScreen(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(240, 320))
	before := starEllipse(t, app.Find("game-canvas").Ops)

	app.Advance(time.Second)

	after := starEllipse(t, app.Find("game-canvas").Ops)
	if after.Y <= before.Y {
		t.Fatalf("star at y %v after a second, want below %v", after.Y, before.Y)
	}
	if after.X != before.X {
		t.Fatalf("star moved sideways from x %v to %v", before.X, after.X)
	}
}

func TestViewportLetterboxesWideWindows(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(960, 640))
	place := viewport.Layout(core.Size{Width: 960, Height: 640})
	if place.ScaleX != 2 || place.ScaleY != 2 {
		t.Fatalf("scale = %v by %v, want 2", place.ScaleX, place.ScaleY)
	}
	if len(place.Bars) != 2 {
		t.Fatalf("%d bars, want one on each side", len(place.Bars))
	}
	if !hasFillColor(app.Find("game-canvas").Ops, barColor) {
		t.Fatal("canvas drew no bars beside the playfield")
	}
}

func starEllipse(t *testing.T, ops []core.DrawOp) core.DrawOp {
	t.Helper()
	for _, op := range ops {
		if op.Kind == core.DrawEllipse {
			return op
		}
	}
	t.Fatal("canvas drew no star")
	return core.DrawOp{}
}

func hasFillColor(ops []core.DrawOp, c core.Color) bool {
	for _, op := range ops {
		if op.Kind == core.DrawFillColor && op.Color == c {
			return true
		}
	}
	return false
}

func near(a, b float32) bool {
	d := a - b
	return d < 0.01 && d > -0.01
}
