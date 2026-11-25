package moves_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/moves"
)

// The parity references sample arrow shot at a quarter, a half and three quarters of its run, which miss the arrow's flight (0.56 to 0.8 s), and its mirrored flight leaves the 300-pixel sheet altogether. This test renders the flight on a canvas wide enough for both directions and checks that the arrow's head is where the chain's XFn puts it, plainly and mirrored.
func TestArrowShotFlight(t *testing.T) {
	const (
		fps    = 30
		w, h   = 640, 74
		ox, oy = 320, 66
		loose  = 0.56
		fly    = 0.24
		speed  = (180.0 - 30) / fly
	)
	// render returns, for each frame of the flight, the drawn pixels' extent in x from the origin.
	render := func(flip bool) map[int][2]int {
		p := pixelfx.NewPlayer(moves.ArrowShot, pixelfx.Options{X: ox, Y: oy, Flip: flip})
		f := pixelfx.NewFrame(w, h, 0, 0)
		span := make(map[int][2]int)
		for i := 0; float64(i)/fps < loose+fly; i++ {
			if i > 0 {
				p.Update(1.0 / fps)
			}
			if float64(i)/fps <= loose {
				continue
			}
			f.Clear()
			p.Draw(f, pixelfx.LayerBoth)
			lo, hi := w, -w
			for y := range h {
				for x := range w {
					if f.Color[y*w+x] != 0 {
						lo, hi = min(lo, x-ox), max(hi, x-ox)
					}
				}
			}
			if lo > hi {
				t.Fatalf("flip=%v: nothing drawn at frame %d (%.3f s), during the arrow's flight", flip, i, float64(i)/fps)
			}
			span[i] = [2]int{lo, hi}
		}
		return span
	}
	plain, mirrored := render(false), render(true)
	if len(plain) == 0 {
		t.Fatal("no frame falls inside the arrow's flight")
	}
	for i, s := range plain {
		want := 30 + (float64(i)/fps-loose)*speed
		if math.Abs(float64(s[1])-want) > 6 {
			t.Errorf("frame %d: the arrow is drawn over x=%d..%d from the origin, want its head about %.1f", i, s[0], s[1], want)
		}
		if m := mirrored[i]; abs(m[0]+s[1]) > 1 {
			t.Errorf("frame %d: mirrored, the arrow's head is drawn at x=%d from the origin, want about %d", i, m[0], -s[1])
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
