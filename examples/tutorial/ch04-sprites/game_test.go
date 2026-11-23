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

const step = time.Second / 60

func TestStarFallsAtFallSpeed(t *testing.T) {
	w := newWorld()
	start := w.starY
	for range 60 {
		w.step(step, 0)
	}
	if got, want := w.starY-start, float32(fallSpeed); !near(got, want) {
		t.Fatalf("star fell %v points in a second, want %v", got, want)
	}
}

func TestStarRespawnsTwoLanesOver(t *testing.T) {
	w := newWorld()
	lanes := []int{w.starLane}
	for len(lanes) < 4 {
		w.step(step, 0)
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
	w.step(step, 0)
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

func TestBasketGlidesOneLaneAPress(t *testing.T) {
	w := newWorld()
	w.step(step, -1)
	if w.basketLane != laneCount/2-1 {
		t.Fatalf("basket heads for lane %d, want %d", w.basketLane, laneCount/2-1)
	}
	if w.basketX >= w.prevX {
		t.Fatalf("basket moved from %v to %v, want left", w.prevX, w.basketX)
	}
	for range 10 {
		w.step(step, 0)
	}
	if got, want := w.basketX, laneX(laneCount/2-1); got != want {
		t.Fatalf("basket at %v after gliding, want lane middle %v", got, want)
	}
}

func TestBasketStaysOnThePlayfield(t *testing.T) {
	w := newWorld()
	for range laneCount + 2 {
		w.step(step, 1)
	}
	if w.basketLane != laneCount-1 {
		t.Fatalf("basket lane = %d, want the last lane %d", w.basketLane, laneCount-1)
	}
	for range 2 * (laneCount + 2) {
		w.step(step, -1)
	}
	if w.basketLane != 0 {
		t.Fatalf("basket lane = %d, want 0", w.basketLane)
	}
}

func TestKeysMoveTheBasket(t *testing.T) {
	var s *starCatcher
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx)
		return s.view()
	}, nsgotest.Size(240, 320))

	app.Key("left")
	app.Advance(time.Second / 4)
	if s.world.basketLane != laneCount/2-1 {
		t.Fatalf("after the left key the basket heads for lane %d, want %d", s.world.basketLane, laneCount/2-1)
	}

	app.Key("d")
	app.Key("d")
	app.Advance(time.Second / 4)
	if s.world.basketLane != laneCount/2 {
		t.Fatalf("after d the basket heads for lane %d, want %d", s.world.basketLane, laneCount/2)
	}
}

func TestTouchingASideMovesTheBasket(t *testing.T) {
	var s *starCatcher
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx)
		return s.view()
	}, nsgotest.Size(480, 640))

	// The 480 by 640 window shows the playfield at twice its size, so x 400 is in its right half.
	finger := app.Touch("game-view", core.Point{X: 400, Y: 300})
	app.Advance(time.Second / 2)
	finger.Lift()
	app.Advance(time.Second / 4)
	if s.world.basketLane != laneCount/2+1 {
		t.Fatalf("after a held touch the basket heads for lane %d, want one lane right, %d", s.world.basketLane, laneCount/2+1)
	}
	if got, want := s.world.basketX, laneX(laneCount/2+1); got != want {
		t.Fatalf("basket at %v, want %v", got, want)
	}
}

func TestLoopMovesTheStarSprite(t *testing.T) {
	var s *starCatcher
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx)
		return s.view()
	}, nsgotest.Size(240, 320))
	before := gametest.ImageDraws(app.Find("game-canvas").Ops)
	if len(before) != 2 {
		t.Fatalf("canvas drew %d sprites, want the basket and the star", len(before))
	}
	starY := s.star.Y

	app.Advance(time.Second)

	after := gametest.ImageDraws(app.Find("game-canvas").Ops)
	if len(after) != 2 {
		t.Fatalf("canvas drew %d sprites after a second, want 2", len(after))
	}
	if after[0].Corners != before[0].Corners {
		t.Fatalf("basket moved from %v to %v without input", before[0].Corners[0], after[0].Corners[0])
	}
	if s.star.Y <= starY {
		t.Fatalf("star sprite at y %v after a second, want below %v", s.star.Y, starY)
	}
}

func TestStarTwinkles(t *testing.T) {
	var s *starCatcher
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx)
		return s.view()
	}, nsgotest.ManualFrames())

	first := s.star.Frame.Name
	s.update(twinkleFrame)
	if s.star.Frame.Name == first {
		t.Fatalf("star still shows %s after a twinkle frame", first)
	}
	s.update(twinkleFrame)
	if s.star.Frame.Name != first {
		t.Fatalf("star shows %s after two twinkle frames, want %s again", s.star.Frame.Name, first)
	}
}

func TestAtlasPaintsTheSprites(t *testing.T) {
	atlas := makeAtlas()
	sheet := atlas.Image.Pixels
	if got := sheet.Bounds().Size(); got.X != sheetWidth || got.Y != sheetHeight {
		t.Fatalf("sheet is %v, want %dx%d", got, sheetWidth, sheetHeight)
	}
	for _, name := range []string{"basket", "star_0", "star_1"} {
		f, ok := atlas.Frames[name]
		if !ok {
			t.Fatalf("atlas has no frame %q", name)
		}
		cx, cy := int(f.X+f.W/2), int(f.Y+f.H/2)
		if _, _, _, a := sheet.At(cx, cy).RGBA(); a == 0 {
			t.Fatalf("frame %q is transparent in its middle", name)
		}
		if _, _, _, a := sheet.At(int(f.X), int(f.Y+f.H-1)).RGBA(); a != 0 {
			t.Fatalf("frame %q is painted in its bottom-left corner", name)
		}
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
