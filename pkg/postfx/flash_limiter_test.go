package postfx_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/postfx"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestTheFlashLimiterKeepsARollingSecond(t *testing.T) {
	var l postfx.FlashLimiter
	start := time.Unix(100, 0)
	var allowed []time.Duration
	// Eight hits a second for two seconds.
	for i := range 16 {
		at := time.Duration(i) * 125 * time.Millisecond
		if l.Allow(start.Add(at)) {
			allowed = append(allowed, at)
		}
	}
	want := []time.Duration{0, 125 * time.Millisecond, 250 * time.Millisecond, 1125 * time.Millisecond, 1250 * time.Millisecond, 1375 * time.Millisecond}
	if len(allowed) != len(want) {
		t.Fatalf("allowed flashes at %v, want %v", allowed, want)
	}
	for i := range want {
		if allowed[i] != want[i] {
			t.Fatalf("allowed flashes at %v, want %v", allowed, want)
		}
	}
	// No closed second holds more than the limit.
	for i := range allowed {
		n := 0
		for _, at := range allowed[i:] {
			if at-allowed[i] <= time.Second {
				n++
			}
		}
		if n > postfx.MaxFlashesPerSecond {
			t.Fatalf("%d flashes in the second from %v", n, allowed[i])
		}
	}
}

func TestTheFlashLimiterCapsItsLimitAtTheGuideline(t *testing.T) {
	for _, tc := range []struct{ max, want int }{{0, 3}, {-1, 3}, {1, 1}, {2, 2}, {3, 3}, {10, 3}} {
		l := postfx.FlashLimiter{MaxPerSecond: tc.max}
		now := time.Unix(0, 0)
		got := 0
		for i := range 10 {
			if l.Allow(now.Add(time.Duration(i) * 10 * time.Millisecond)) {
				got++
			}
		}
		if got != tc.want {
			t.Errorf("MaxPerSecond %d allowed %d flashes in a second, want %d", tc.max, got, tc.want)
		}
	}
	var none *postfx.FlashLimiter
	if !none.Allow(time.Unix(0, 0)) {
		t.Fatal("a nil limiter refused a flash")
	}
}

// hits starts an app with a flash per config, a "hit" button that triggers every flash, and returns the app, the flashes and the results of each press.
func hits(t *testing.T, cfgs ...postfx.FlashConfig) (*nsgotest.App, []*postfx.Flash, *[][]bool) {
	t.Helper()
	flashes := make([]*postfx.Flash, len(cfgs))
	var ran [][]bool
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		for i, cfg := range cfgs {
			flashes[i] = postfx.NewFlash(cx, cfg)
		}
		return core.View{TestID: "hit", Style: core.Style{Width: core.Pt(40), Height: core.Pt(40)}, OnPress: func(core.PressEvent) {
			var r []bool
			for _, f := range flashes {
				r = append(r, f.Trigger(1, 50*time.Millisecond))
			}
			ran = append(ran, r)
		}}
	}, nsgotest.ManualFrames())
	return app, flashes, &ran
}

func TestAFlashRunsAtMostThreeTimesASecond(t *testing.T) {
	app, _, ran := hits(t, postfx.FlashConfig{Color: core.RGB(0xffffff)})
	for range 8 {
		app.Press("id:hit")
		app.Advance(125 * time.Millisecond)
	}
	n := 0
	for _, r := range *ran {
		if r[0] {
			n++
		}
	}
	if len(*ran) != 8 || n != 3 {
		t.Fatalf("8 hits in a second ran %d flashes of %d presses, want 3", n, len(*ran))
	}
	// After the second passes, the flash runs again.
	app.Advance(time.Second)
	app.Press("id:hit")
	if !(*ran)[8][0] {
		t.Fatal("the flash did not run once its second passed")
	}
}

func TestASharedLimiterBudgetsEveryFlash(t *testing.T) {
	shared := &postfx.FlashLimiter{}
	app, _, ran := hits(t, postfx.FlashConfig{Limiter: shared}, postfx.FlashConfig{Limiter: shared})
	app.Press("id:hit")
	app.Advance(100 * time.Millisecond)
	app.Press("id:hit")
	want := [][]bool{{true, true}, {true, false}}
	for i := range want {
		for j := range want[i] {
			if (*ran)[i][j] != want[i][j] {
				t.Fatalf("two flashes on one limiter ran %v, want %v", *ran, want)
			}
		}
	}
}

func TestReduceFlashesTurnsAFlashIntoATint(t *testing.T) {
	reduce := true
	app, flashes, ran := hits(t, postfx.FlashConfig{Color: core.RGB(0xffffff), ReduceFlashes: func() bool { return reduce }})
	app.Press("id:hit")
	if !(*ran)[0][0] {
		t.Fatal("a reduced flash did not run")
	}
	if got := value(app, flashes[0].Amount()); got != postfx.ReducedFlashPeak {
		t.Fatalf("a reduced flash peaks at %g, want %g", got, postfx.ReducedFlashPeak)
	}
	// The fade is at least ReducedFlashFade, though the trigger asked for 50 ms.
	app.Advance(60 * time.Millisecond)
	if got := value(app, flashes[0].Amount()); !(got > 0) {
		t.Fatalf("a reduced flash faded out after 60 ms, at %g", got)
	}
	app.Advance(time.Second)
	reduce = false
	app.Press("id:hit")
	if got := value(app, flashes[0].Amount()); got != 1 {
		t.Fatalf("with Reduce Flashes off the flash peaks at %g, want 1", got)
	}
}

func TestASaturatedRedFlashIsDesaturated(t *testing.T) {
	var red, orange, white, black *postfx.Flash
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		red = postfx.NewFlash(cx, postfx.FlashConfig{Color: core.RGBA(0xff0000, 0.5)})
		orange = postfx.NewFlash(cx, postfx.FlashConfig{Color: core.RGB(0xff8000)})
		white = postfx.NewFlash(cx, postfx.FlashConfig{Color: core.RGB(0xffffff)})
		black = postfx.NewFlash(cx, postfx.FlashConfig{})
		return core.View{}
	})
	r, g, b, a := red.Uniforms()["Color"].(core.Color).Components()
	if r != g || g != b {
		t.Fatalf("a red flash stays %v %v %v, want gray", r, g, b)
	}
	// Pure red's relative luminance is 0.2126, sRGB 0.5.
	if math.Abs(r-0.5) > 0.01 || math.Abs(a-0.5) > 0.01 {
		t.Fatalf("a red flash became gray %g at alpha %g, want 0.5 at 0.5", r, a)
	}
	for name, tc := range map[string]struct {
		f    *postfx.Flash
		want core.Color
	}{"orange": {orange, core.RGB(0xff8000)}, "white": {white, core.RGB(0xffffff)}, "black": {black, core.Color(0)}} {
		if got := tc.f.Uniforms()["Color"]; got != tc.want {
			t.Errorf("a %s flash became %v, want %v", name, got, tc.want)
		}
	}
}
