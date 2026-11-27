// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/jellyfish.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the bubbles from Sprite Fusion's open-source bubbles (https://www.spritefusion.com/pixel-art-vfx/bubbles), MIT License, Copyright (c) 2026 Hugo Duprez.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Jellyfish is a see-through jellyfish pulsing: its bell squeezes to shove it up and relaxes as it sinks, frilled arms and long tentacles trailing, a glow inside, bubbles drifting by. It is registered as "critters/jellyfish".
var Jellyfish = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Jellyfish",
		Desc:     "A see-through jellyfish pulses: its bell squeezes to shove it up and relaxes as it sinks, frilled arms and long tentacles trailing, a glow inside, bubbles drifting by.",
		Tags:     []string{"critter", "jellyfish", "sea", "underwater", "glow", "swim", "loop"},
		Size:     [2]int{96, 120},
		Origin:   [2]int{48, 38},
		Duration: 3.2,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#140b2b", "#2a1650", "#4b2386", "#7a3cb8", "#b066e0", "#e09cf5", "#ffd6ff", "#ffffff")},
			{Name: "glow", Default: pixelfx.Palette("#0b3550", "#1f7fa8", "#4fd2e8", "#b8fbff")},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newJellyfish,
}

func init() { pixelfx.Register("critters/jellyfish", Jellyfish) }

var (
	jellyD      = 3.2
	jellyBeat   = jellyD / 2
	jellyW0     = 32.0
	jellyH0     = 18.0
	jellyArmQ   = [4]float64{-0.32, -0.1, 0.12, 0.3}
	jellyLampsQ = [3]float64{0.14, 0.42, 0.78}
)

// jellySqueeze is the bell's squeeze through one beat: 1 squeezed, below 0 flared out past rest.
func jellySqueeze(u float64) float64 {
	switch {
	case u < 0.16:
		return pixelfx.Ease.OutCubic(u / 0.16)
	case u < 0.22:
		return 1
	case u < 0.72:
		return pixelfx.Lerp(1, -0.14, pixelfx.Ease.InOutSine((u-0.22)/0.5))
	}
	return pixelfx.Lerp(-0.14, 0, pixelfx.Ease.InOutSine((u-0.72)/0.28))
}

// jellyLift is the bell's height through one beat.
func jellyLift(u float64) float64 {
	if u < 0.42 {
		return -6 * pixelfx.Ease.OutCubic(pixelfx.Seg(u, 0.04, 0.42))
	}
	return -6 + 6*pixelfx.Ease.InOutSine((u-0.42)/0.58)
}

// jellyBell is the bell at a moment: rim center (x, y), width, height, squeeze.
type jellyBell struct{ x, y, c, W, H float64 }

func jellyBellAt(t float64) jellyBell {
	t = pixelfx.Wrap(t, jellyD)
	u := math.Mod(t, jellyBeat) / jellyBeat
	c := jellySqueeze(u)
	return jellyBell{x: 2 * math.Sin((t/jellyD)*tau), y: jellyLift(u), c: c, W: jellyW0 * (1 - 0.3*c), H: jellyH0 * (1 + 0.24*c)}
}

// jellyHalfW is the bell's half-width h px above its rim: a dome whose margin pulls in as it squeezes; -1 above it.
func jellyHalfW(h float64, B *jellyBell) float64 {
	if h > B.H {
		return -1
	}
	hw := (B.W / 2) * math.Sqrt(math.Max(0, 1-math.Pow(math.Max(0, h)/B.H, 2)))
	if h < B.H*0.3 {
		hw -= B.c * 2.4 * (1 - h/(B.H*0.3))
	}
	return hw
}

// jellyScallop is how far the margin hangs in little scallops.
func jellyScallop(dx float64, B *jellyBell) float64 {
	return 1.1 * math.Max(0, math.Sin((dx/(B.W/2))*pi*4+pi/2))
}

type jelly struct {
	C, G          []uint32
	seed          float64
	bell, arms    *grid
	tents, frills [][][2]float64
}

func (j *jelly) drawBell(p *pixelfx.Painter, B *jellyBell, t float64) {
	C, G := j.C, j.G
	g, bx, by := j.bell.clear(), pixelfx.Round(B.x), pixelfx.Round(B.y)
	inside := func(x, y float64) bool {
		dx, h := x-B.x, B.y-y
		if h < -jellyScallop(dx, B) {
			return false
		}
		hw := jellyHalfW(math.Max(0, h), B)
		return hw >= 0 && math.Abs(dx) <= hw
	}
	g.shade(-20, -26, 20, 3, func(x, y float64, o *gridOut) uint32 {
		if !inside(x+bx, y+by) {
			return 0
		}
		o.t = 1
		return C[3]
	})
	g.depths(4)
	// the subumbrella: the hollow under the bell, seen through it as a fainter inner dome
	iw, ih, lx, ly := B.W*0.36, B.H*0.66, B.x-B.W*0.22, B.y-B.H*0.78
	g.recolor(func(x, y float64, c uint32, _ float64, tag uint8) uint32 {
		if tag != 1 {
			return c
		}
		X, Y, d := x+bx+0.5, y+by+0.5, g.depth(x, y)
		dx, h := X-B.x, B.y-Y
		v := h / B.H
		i := g.idx(x, y)
		col, al := C[3], 0.26+0.2*pixelfx.Clamp(v)
		ie := math.Pow(dx/iw, 2) + math.Pow(math.Max(0, h)/ih, 2)
		if ie > 0.8 && ie < 1.12 && h > 0.5 {
			col, al = C[4], 0.55
		}
		if h < 1 {
			col, al = C[5], 0.85
		}
		if d == 2 {
			col, al = C[4], 0.75
			if v > 0.5 && dx < 0 {
				col = C[5]
			}
		}
		if d == 1 {
			col, al = C[5], 0.92
			if dx < 0 && v > 0.25 {
				col = C[6]
			}
		}
		// the gloss on the top-left of the dome
		if gx, gy := X-lx, Y-ly; d == 2 && dx < -1 && v > 0.45 && gx*gx+gy*gy < 26 {
			col, al = C[7], 1
		}
		g.a[i] = float32(al)
		return col
	})
	// radial canals running down from the crown
	for _, s := range [2]float64{-0.62, 0.62} {
		for h := 2.0; h < B.H-2; h++ {
			hw := jellyHalfW(h, B)
			x, y := B.x+s*hw*0.9, B.y-h
			if g.depth(x-bx, y-by) >= 3 {
				g.set(x-bx, y-by, C[4], 0.5, 0)
			}
		}
	}
	// the stomach and gonads: a soft glowing mass hung in the bell, brighter as it squeezes
	pulse, cy, rx, ry := 0.5+0.5*B.c, B.y-B.H*0.42, B.W*0.2, B.H*0.2
	for y := math.Floor(cy - ry); y <= cy+ry; y++ {
		for x := math.Floor(B.x - rx); x <= B.x+rx; x++ {
			d := math.Pow((x+0.5-B.x)/rx, 2) + math.Pow((y+0.5-cy)/ry, 2)
			if d > 1 || g.depth(x-bx, y-by) < 2 {
				continue
			}
			c, a := C[4], 0.6
			if d < 0.3 {
				c, a = C[6], 0.9
			} else if d < 0.65 {
				c = C[5]
			}
			g.set(x-bx, y-by, c, a, 0)
		}
	}
	g.outlineFn(func(_, _, _, ny float64) uint32 {
		if ny > 0 {
			return 0
		}
		return C[1]
	}, 0.9)
	g.draw(p, 1, bx, by)
	// the light inside, and the little lamps round the margin
	p.Glow(B.x, B.y-B.H*0.45, 30+6*B.c, C[4], 0.22+0.12*pulse)
	p.Glow(B.x, B.y-B.H*0.45, 11, C[6], 0.14+0.16*pulse)
	for _, s := range [2]float64{-1, 1} {
		for _, q := range jellyLampsQ {
			x, y, tw := B.x+s*jellyHalfW(0.5, B)*q, B.y+1, 0.6+0.4*math.Sin((t/jellyD)*tau*3+q*9+s)
			c := G[2]
			if tw > 0.85 {
				c = G[3]
			}
			p.Px(x, y, c)
			p.Glow(x, y, 4, G[2], 0.4*tw)
		}
	}
}

// jellyStrand is a strand hanging from the bell: each point follows where its root was a moment ago, so the strand trails behind every thrust and billows as the bell sinks; a slow wave runs down it.
func jellyStrand(pts [][2]float64, t float64, off func(b *jellyBell) [2]float64, length, step, lag, sway, ph, pull float64) [][2]float64 {
	pts = pts[:0]
	B1 := jellyBellAt(t)
	o1 := off(&B1)
	for s := 0.0; s <= length; s += step {
		B := jellyBellAt(t - s*lag)
		relax, ox := 1-pixelfx.Clamp(B.c), pixelfx.Lerp(o1[0], off(&B)[0], 0.4)
		w := (0.3 + s*sway) * (0.55 + 0.65*relax) * math.Sin((t/jellyD)*tau*2-s*0.11+ph)
		pts = append(pts, [2]float64{B.x + ox + w + ox*s*(0.009*relax-0.005*pull*B.c), B.y + o1[1] + s*(0.88+0.05*B.c)})
	}
	return pts
}

func (j *jelly) drawTentacle(p *pixelfx.Painter, pts [][2]float64, back bool, t float64, i int) {
	C, G := j.C, j.G
	n := len(pts)
	for k := 1; k < n; k++ {
		u := float64(k) / float64(n-1)
		var c uint32
		if back {
			switch {
			case u < 0.3:
				c = C[3]
			case u < 0.7:
				c = C[2]
			default:
				c = C[1]
			}
		} else {
			switch {
			case u < 0.2:
				c = C[5]
			case u < 0.45:
				c = C[4]
			case u < 0.78:
				c = C[3]
			default:
				c = C[2]
			}
		}
		p.Line(pts[k-1][0], pts[k-1][1], pts[k][0], pts[k][1], c, 1)
	}
	// stinging cells glinting along it
	if back {
		return
	}
	for k := 3; k < n; k += 4 {
		tw := math.Sin((t/jellyD)*tau*2 - float64(k)*0.5 + float64(i)*1.7)
		if tw < 0.3 {
			continue
		}
		x, y := pts[k][0], pts[k][1]
		c := G[2]
		if tw > 0.8 {
			c = G[3]
		}
		p.Px(x, y, c)
		p.Glow(x, y, 3.5, G[2], 0.35*tw)
	}
}

// drawArms draws the frilled oral arms: ribbons lit down their left edge, darker on their right, ruffled along both sides.
func (j *jelly) drawArms(p *pixelfx.Painter, back bool) {
	C := j.C
	g, a := j.arms.clear(), 0.88
	left, right, mid := C[6], C[4], C[5]
	if back {
		a, left, right, mid = 0.6, C[4], C[2], C[3]
	}
	for ai, pts := range j.frills {
		if (ai == 1 || ai == 2) != back {
			continue
		}
		n := len(pts)
		lastRow := -1e9
		for k := 1; k < n; k++ {
			x0, y0, x1, y1 := pts[k-1][0], pts[k-1][1], pts[k][0], pts[k][1]
			m := math.Max(1, math.Ceil(math.Abs(y1-y0)*2))
			for jj := 0.0; jj < m; jj++ {
				q := jj / m
				x, y := pixelfx.Lerp(x0, x1, q), pixelfx.Lerp(y0, y1, q)
				u := (float64(k-1) + q) / float64(n-1)
				wd := pixelfx.Lerp(2.2, 0.6, u)
				for i := -math.Ceil(wd); i <= math.Ceil(wd); i++ {
					if math.Abs(i) > wd+0.2 {
						continue
					}
					c := mid
					if i < 0 {
						c = left
					} else if i > 0 {
						c = right
					}
					g.set(x+i, y, c, a, 0)
				}
				row := math.Floor(y)
				if row != lastRow && math.Mod(row, 3) == 0 && u < 0.85 {
					dx := wd + 1.2
					if math.Mod(row/3, 2) != 0 {
						dx = -wd - 1.2
					}
					g.set(x+dx, y, left, a, 0)
				}
				lastRow = row
			}
		}
	}
	g.draw(p, 1, 0, 0)
}

func newJellyfish(o *pixelfx.Options) pixelfx.Instance {
	j := &jelly{C: pixelfx.Fit(o.Palette(), 8), G: pixelfx.Fit(o.Ramp("glow"), 4), seed: o.Seed(), bell: newGrid(-22, -28, 44, 36), arms: newGrid(-40, -40, 80, 100), tents: make([][][2]float64, 9), frills: make([][][2]float64, 4)}
	return &pixelfx.Funcs{DrawFunc: j.draw}
}

func (j *jelly) draw(p *pixelfx.Painter, time float64) {
	C, G, seed := j.C, j.G, j.seed
	t := shift(time, jellyD, seed)
	B := jellyBellAt(t)
	// specks drifting in the water
	pixelfx.Cycle(t, jellyD, 16, seed*5+1, pixelfx.Fixed(jellyD), func(age, L float64, r *pixelfx.Rng, _ int) {
		x := r.Range(-42, 42) + math.Sin(age*0.8+r.Next()*6)*3
		y := r.Range(-34, 66) + age*r.Range(1, 4)
		tw := math.Sin(age*2.2 + r.Next()*6)
		a := math.Min(math.Min(1, age*2), (L-age)*2) * (0.35 + 0.35*tw)
		c := C[3]
		if tw > 0.8 {
			c = G[2]
		}
		p.Dot(x, y, c, a)
	})
	for i := range j.tents {
		r := pixelfx.RngOf(seed, float64(i), 4)
		q := -0.88 + (1.76*float64(i))/8 + r.Range(-0.05, 0.05)
		length := r.Range(36, 50)
		j.tents[i] = jellyStrand(j.tents[i], t, func(b *jellyBell) [2]float64 { return [2]float64{q * jellyHalfW(0.5, b), 0.5} }, length, 1.5, 0.022, 0.065, r.Range(0, tau), 1)
	}
	for i := range j.frills {
		r := pixelfx.RngOf(seed, float64(i), 9)
		q := jellyArmQ[i]
		length := r.Range(20, 27)
		j.frills[i] = jellyStrand(j.frills[i], t, func(b *jellyBell) [2]float64 { return [2]float64{q * b.W * 0.5, -1} }, length, 1, 0.03, 0.06, r.Range(0, tau)+float64(i), 0.4)
	}
	// each squeeze shoves a ring of water out under the bell: a soft swirl of light that swells and thins
	for b := 0.0; b < 2; b++ {
		age := pixelfx.Wrap(t-b*jellyBeat-0.05, jellyD)
		if age > 0.9 {
			continue
		}
		B0 := jellyBellAt(b*jellyBeat + 0.05)
		e, k := pixelfx.Ease.OutCubic(age/0.9), 1-age/0.9
		cx, cy, rx, ry := B0.x, B0.y+3+14*e, B0.W*0.32+9*e, 1.5+2.5*e
		for jj := 0.0; jj < 8; jj++ {
			a := (jj/8)*tau + e*0.8
			p.Glow(cx+math.Cos(a)*rx, cy+math.Sin(a)*ry, 5+3*e, C[5], 0.14*k)
		}
		// plankton caught in the swirl, glinting as it spins
		for jj := 0; jj < 7; jj++ {
			a := (float64(jj)/7)*tau + e*2.4 + b
			x, y := cx+math.Cos(a)*rx*1.05, cy+math.Sin(a)*ry*1.4-math.Cos(a*2)*0.6
			if k < 0.15 && jj%2 != 0 {
				continue
			}
			c := G[2]
			if math.Sin(a) > 0 {
				c = G[3]
			}
			p.Dot(x, y, c, math.Min(1, k*2.5))
			p.Glow(x, y, 3, G[2], 0.3*k)
		}
		// and a few bubbles squeezed out of the margin
		for i := 0.0; i < 4; i++ {
			r := pixelfx.RngOf(seed, b, i, 13)
			L, ag := r.Range(1.4, 2.2), pixelfx.Wrap(t-b*jellyBeat-0.08-i*0.04, jellyD)
			if ag > L {
				continue
			}
			rad, sx := r.Range(0.6, 1.5), r.Sign()
			x := B0.x + sx*(B0.W*0.4+r.Range(0, 4)+ag*r.Range(4, 10))
			y := B0.y + 2 - (14+10*rad)*ag + 6*math.Exp(-ag*4)
			if y < -33 {
				continue
			}
			bubble(p, x+math.Sin(ag*5+i)*0.8, y, rad, [4]uint32{G[3], G[3], G[2], G[1]}, math.Min(math.Min(math.Min(1, ag*6), (L-ag)*3), (y+33)/6), 0)
		}
	}
	for i, pts := range j.tents {
		if i%2 == 1 {
			j.drawTentacle(p, pts, true, t, i)
		}
	}
	j.drawArms(p, true)
	j.drawBell(p, &B, t)
	j.drawArms(p, false)
	for i, pts := range j.tents {
		if i%2 != 1 {
			j.drawTentacle(p, pts, false, t, i)
		}
	}
	// bubbles wobbling up past it
	pixelfx.Cycle(t, jellyD, 7, seed*5+2, func(r *pixelfx.Rng) float64 { return r.Range(1.8, 3) }, func(age, L float64, r *pixelfx.Rng, _ int) {
		var rad float64
		if r.Chance(0.6) {
			rad = r.Range(0.6, 1.4)
		} else {
			rad = r.Range(1.6, 2.6)
		}
		x0, y0 := r.Range(-30, 30), r.Range(10, 70)
		v := 20 + 13*rad
		end := math.Min(L, (y0+30)/v)
		if age > end {
			return
		}
		ph := r.Next() * tau
		x, y := x0+math.Sin(age*(4+2/rad)+ph)*(0.6+rad*0.8), y0-v*age
		bubble(p, x, y, rad, [4]uint32{G[3], G[3], G[2], G[1]}, math.Min(math.Min(1, age*3), (end-age)*3), 0.1*math.Sin(age*9+ph))
	})
	// a soft cool glow all round
	p.Glow(B.x, B.y+6, 58, C[3], 0.14)
}
