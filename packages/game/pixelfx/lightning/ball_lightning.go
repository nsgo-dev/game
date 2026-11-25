// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/ball-lightning.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The layered ball, its tapering wake and the zap to the floor with stars where it lands follow Sprite Fusion's open-source energy orb and bolt (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// BallLightning is a crackling ball of plasma on a wandering loop, spitting arcs off its skin and shedding fading motes, that zaps the floor when it dips low. It is registered as "lightning/ball-lightning".
var BallLightning = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ball lightning",
		Desc:     "A crackling ball of plasma drifts on a wandering loop, spitting arcs off its skin and leaving a wake of fading motes, and zaps the floor when it dips low.",
		Tags:     []string{"lightning", "electric", "plasma", "orb", "wisp", "ambient", "loop"},
		Size:     [2]int{136, 104},
		Origin:   [2]int{68, 92},
		Duration: 4,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options:  append(voltOptions(), pixelfx.Option{Name: "seed", Default: pixelfx.Num(9)}),
	},
	New: newBallLightning,
}

func init() { pixelfx.Register("lightning/ball-lightning", BallLightning) }

const (
	blFPS = 30
	blN   = 120
	blP   = 4
	blZap = 62
)

var blZapK = [4]float64{1, 0.6, 0.9, 0.4}

// blAt returns where the ball is at t: a closed, wandering path that dips toward the floor once a loop.
func blAt(t float64) (float64, float64) {
	a := (t / blP) * tau
	return 38*math.Sin(a) + 9*math.Sin(2*a+1), -42 + 13*math.Sin(2*a+0.4) + 6*math.Sin(3*a+0.5)
}

// b2f is JavaScript's conversion of a bool to a number.
func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func newBallLightning(o *pixelfx.Options) pixelfx.Instance {
	C, S, seed := pixelfx.Fit(o.Palette(), 8), o.Ramp("sparks"), o.Seed()
	zx, _ := blAt(float64(blZap) / blFPS)
	gx := zx + pixelfx.RngOf(seed, 3).Range(-10, 10)
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		f := frameOf(t, blFPS, blN)
		r := pixelfx.RngOf(seed, 1, float64(f))
		x, y := blAt(t)
		// light under the ball on the floor, stronger as it comes low
		p.Glow(x, -2, 30, C[3], 0.12+0.3*pixelfx.Clamp((y+60)/40))
		// the wake: where it has been, shrinking and cooling, and motes shed along it, drifting and dying
		for i := 14; i >= 1; i-- {
			wx, wy := blAt(t - float64(i)*0.034)
			u := float64(i) / 14
			rr := 3*(1-u) + 0.3
			if u > 0.6 && (i+f)%2 != 0 {
				continue
			}
			c0, c1 := C[2], C[4]
			if u < 0.35 {
				c0, c1 = C[3], C[6]
			} else if u < 0.7 {
				c1 = C[5]
			}
			p.Circle(wx, wy, rr+0.8, c0, 1)
			p.Circle(wx, wy, rr, c1, 1)
			p.Glow(wx, wy, 7*(1-u)+3, C[4], 0.25*(1-u))
		}
		pixelfx.Cycle(t, blP, 40, seed+5, func(g *pixelfx.Rng) float64 { return g.Range(0.5, 1) }, func(age, life float64, q *pixelfx.Rng, _ int) {
			born := t - age
			bx, by := blAt(born)
			k := 1 - age/life
			a, sp := q.Range(0, tau), q.Range(4, 14)
			mx := bx + math.Cos(a)*sp*age + q.Range(-3, 3)
			my := by + math.Sin(a)*sp*age + 6*age + q.Range(-3, 3)
			if k > 0.25 || f%2 != 0 {
				c := C[3]
				if k > 0.6 {
					c = C[6]
				} else if k > 0.3 {
					c = C[4]
				}
				p.Px(mx, my, c)
			}
			p.Glow(mx, my, 3, C[4], 0.3*k)
		})
		// the ball: a churning skin with hot spots rolling round it, a white heart, corona bristling off it
		R0 := 5.2 + 0.6*math.Sin(t*9) + 0.4*r.Range(-1, 1)
		p.Circle(x, y, R0+1.3, C[2], 1)
		p.Circle(x, y, R0, C[4], 1)
		p.Circle(x-0.4, y-0.4, R0*0.72, C[5], 1)
		for i := range 3 {
			fi := float64(i)
			a, d := t*(5+fi*2.3)+fi*2.1, R0*0.6
			c := pixelfx.White
			if i != 0 {
				c = C[6]
			}
			p.Circle(x+math.Cos(a)*d, y+math.Sin(a)*d*0.8, 1.2, c, 1)
		}
		p.Circle(x-0.6, y-0.6, R0*0.32, pixelfx.White, 1)
		for i := range 8 {
			if r.Next() < 0.35 {
				continue
			}
			a := (float64(i)/8)*tau + t*2 + r.Range(-0.2, 0.2)
			l := r.Range(1.5, 4)
			c := C[4]
			if i%2 != 0 {
				c = C[5]
			}
			p.Line(x+math.Cos(a)*(R0+1), y+math.Sin(a)*(R0+1), x+math.Cos(a)*(R0+1+l), y+math.Sin(a)*(R0+1+l), c, 1)
		}
		p.Glow(x, y, 30, C[4], 0.3)
		p.Glow(x, y, 9, C[6], 0.1)
		// arcs spat off its skin, re-struck every frame (pixelfx draws a number on every test of the loop)
		for i := 0; float64(i) < 2+b2f(r.Next() < 0.4); i++ {
			a, l := r.Range(0, tau), r.Range(5, 11)
			x0, y0 := x+math.Cos(a)*(R0+1), y+math.Sin(a)*(R0+1)
			b := a + r.Range(-0.7, 0.7)
			crackle(p, zigzag(r, x0, y0, x0+math.Cos(b)*l, y0+math.Sin(b)*l, 0.4, 0.3), r.Range(0.4, 0.7), C, 0.25, 1)
		}
		// once a loop, low over the floor, it zaps down: a bolt, the floor lit, sparks kicked up
		if za := f - blZap; za >= 0 && za < 4 {
			k := blZapK[za]
			stroke(p, zigzag(pixelfx.RngOf(seed, 4, float64(f)), x, y+R0, gx, 0, 0.28, 0.4), k, C, 0.7, strokeOpts{hero: za == 0})
			p.Circle(gx, -1, 2.5*k+1, C[5], 1)
			p.Px(gx, -1, pixelfx.White)
			p.Glow(gx, -4, 34, C[4], 0.6*k)
		}
		sa := pixelfx.Wrap(t-float64(blZap)/blFPS, blP)
		for j := range 14 {
			q := pixelfx.RngOf(seed, 7, float64(j))
			life := q.Range(0.25, 0.55)
			if sa >= life {
				continue
			}
			a, sp := -pi/2+q.Range(-1.3, 1.3), q.Range(50, 170)
			sx, sy, vx, vy := flight(gx, -1, math.Cos(a)*sp, math.Sin(a)*sp, sa, 0)
			spark(p, sx, sy, vx, vy, 1-sa/life, S)
		}
	}}
}
