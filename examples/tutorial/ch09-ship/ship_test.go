package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/game/examples/tutorial/ch09-ship/internal/icon"
	"github.com/nsgo-dev/nsgo/core"
)

func TestIconIsUpToDate(t *testing.T) {
	data, err := os.ReadFile("assets/Icon.png")
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("assets/Icon.png: %v", err)
	}
	if b := got.Bounds(); b.Dx() != 1024 || b.Dy() != 1024 {
		t.Fatalf("icon is %dx%d, want 1024x1024", b.Dx(), b.Dy())
	}
	want := icon.Render(1024)
	for y := range 1024 {
		for x := range 1024 {
			if !sameColor(got, want, x, y) {
				t.Fatalf("icon differs from icon.Render at %d,%d; run go generate", x, y)
			}
		}
	}
}

func sameColor(a, b image.Image, x, y int) bool {
	ar, ag, ab, aa := a.At(x, y).RGBA()
	br, bg, bb, ba := b.At(x, y).RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func TestLosingTheFocusPausesTheGame(t *testing.T) {
	for _, lc := range []core.Lifecycle{core.LifecycleInactive, core.LifecycleBackground} {
		t.Run(lc.String(), func(t *testing.T) {
			app, _ := startPlaying(t)
			app.SetEnvironment(core.Environment{Lifecycle: lc})
			app.Advance(step)
			app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
			app.Advance(step)
			app.Find("paused")
			app.Press("role:button Resume")
			app.Find("hud")
		})
	}
}

func TestBackgroundLeavesMenusAlone(t *testing.T) {
	app, _ := start(t, 1)
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.Advance(step)
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	app.Advance(step)
	app.Find("title")
}

// bot plays w as a player would: it heads for the lowest star, and steps aside from a rock about to land in the basket.
func bot(w *world) int {
	target := w.basketLane
	lowest := float32(-1e9)
	for _, it := range w.items {
		if it.kind == kindStar && it.y > lowest {
			lowest, target = it.y, it.lane
		}
	}
	for _, it := range w.items {
		if it.kind == kindRock && it.lane == target && it.y > basketY-3*itemSize && it.y > lowest {
			if target > 0 {
				target--
			} else {
				target++
			}
		}
	}
	switch {
	case target < w.basketLane:
		return -1
	case target > w.basketLane:
		return 1
	}
	return 0
}

// TestAWholeGame plays whole games of the finished rules with a simple bot, as a check before a release that the game can be played, gets harder, and ends.
func TestAWholeGame(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3} {
		w := newWorld(seed)
		steps := 0
		for !w.over && steps < 60*60*20 {
			w.step(step, bot(w))
			steps++
		}
		played := time.Duration(steps) * step
		t.Logf("seed %d: %d stars in %v", seed, w.score, played)
		if !w.over {
			t.Fatalf("seed %d: a game lasted over 20 minutes", seed)
		}
		if w.score < 20 {
			t.Fatalf("seed %d: the bot caught only %d stars", seed, w.score)
		}
	}
}
