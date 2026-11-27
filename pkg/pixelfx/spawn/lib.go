// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Shared by the spawn and death effects: the actor's own body as a list of pixels to break up, burn, freeze or copy, and the cells just outside it for outlines.

// pi and tau are float64 variables so expressions combining them with other constants round step by step as JavaScript's do, instead of being folded at compile time.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

// bodyCell is one filled pixel of an actor's body, in local space with the feet at (0, 0).
type bodyCell struct {
	x, y   float64
	mx, my int
	c      uint32
	edge   bool
	key    float64
	gone   bool
}

// actorBody is an actor's body: its filled pixels, mask bounds, origin, and cell lookup.
type actorBody struct {
	list  []*bodyCell
	cells []*bodyCell
	m     *pixelfx.Mask
	w, h  int
	x, y  float64
}

func (b *actorBody) on(mx, my int) bool {
	return mx >= 0 && my >= 0 && mx < b.w && my < b.h && b.m.Data[my*b.w+mx] != 0
}

func (b *actorBody) cell(mx, my int) *bodyCell {
	if b.on(mx, my) {
		return b.cells[my*b.w+mx]
	}
	return nil
}

// bodyOf builds the actor's body in local space with the feet at (0, 0). c is the character's own color (o.Actor.Pixels); an actor without pixels is shaded from the ramp C (lit at the top, darker toward the feet). edge marks pixels on the silhouette's rim.
func bodyOf(actor *pixelfx.Actor, C []uint32) *actorBody {
	m, px := actor.Mask, actor.Pixels
	b := &actorBody{
		cells: make([]*bodyCell, m.W*m.H),
		m:     m,
		w:     m.W,
		h:     m.H,
		x:     actor.X,
		y:     actor.Y,
	}
	for my := 0; my < m.H; my++ {
		for mx := 0; mx < m.W; mx++ {
			if !b.on(mx, my) {
				continue
			}
			var own uint32
			if len(px) > my*m.W+mx {
				own = px[my*m.W+mx]
			}
			c := own
			if c == 0 {
				idx := int(math.Max(1, math.Min(float64(len(C)-2), pixelfx.Round(float64(len(C)-2)*(0.85-(float64(my)/float64(m.H))*0.5)))))
				c = C[idx]
			}
			cell := &bodyCell{
				x:    actor.X + float64(mx),
				y:    actor.Y + float64(my),
				mx:   mx,
				my:   my,
				c:    c,
				edge: !b.on(mx-1, my) || !b.on(mx+1, my) || !b.on(mx, my-1) || !b.on(mx, my+1),
			}
			b.list = append(b.list, cell)
			b.cells[my*m.W+mx] = cell
		}
	}
	return b
}

// rimCell is one cell just outside the actor's body, in local space.
type rimCell struct {
	x, y   float64
	mx, my int
}

// rimOf returns cells just outside the body (4-neighbours of its rim), in local space.
func rimOf(body *actorBody) []rimCell {
	var out []rimCell
	for my := -1; my <= body.h; my++ {
		for mx := -1; mx <= body.w; mx++ {
			if !body.on(mx, my) && (body.on(mx-1, my) || body.on(mx+1, my) || body.on(mx, my-1) || body.on(mx, my+1)) {
				out = append(out, rimCell{x: body.x + float64(mx), y: body.y + float64(my), mx: mx, my: my})
			}
		}
	}
	return out
}
