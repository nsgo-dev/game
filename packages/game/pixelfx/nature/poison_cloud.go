// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/poison-cloud.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// PoisonCloud is a miasma hugging the floor: ragged sheets of gas lit yellow-green from within, fading to violet, with curling tongues and twisting ribbon plumes. It is registered as "nature/poison-cloud".
var PoisonCloud = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Poison cloud",
		Desc:     "A miasma hugging the floor: ragged sheets of gas lit yellow-green from within, fading to violet, creep out in tongues that curl over at their tips; plumes twist up as ribbons.",
		Tags:     []string{"nature", "poison", "gas", "cloud", "miasma", "toxic", "area", "ground", "loop"},
		Size:     [2]int{256, 100},
		Origin:   [2]int{128, 86},
		Duration: poisonCloudDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0e0816", "#24123a", "#3e2056", "#5a3a62", "#6a6a3a", "#96aa30", "#cce040", "#f4ffa0")},
			{Name: "spread", Default: pixelfx.Num(1)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newPoisonCloud,
}

func init() { pixelfx.Register("nature/poison-cloud", PoisonCloud) }

const poisonCloudDuration = 4.0

type poisonSheet struct {
	h, e, a float64
	dim     int
	flow    float64
	n       int
	back    bool
}

// poisonSheets are sheets of gas, back to front: how tall, how wide, how thick, how much dimmer, how fast their edges flow.
var poisonSheets = []poisonSheet{
	{h: 36, e: 0.84, a: 0.46, dim: 2, flow: 4, n: 2, back: true},
	{h: 26, e: 0.96, a: 0.5, dim: 1, flow: 8, n: 2, back: true},
	{h: 16, e: 1.04, a: 0.56, dim: 0, flow: 8, n: 3, back: false},
}

type poisonTongue struct {
	s, q, x, h float64
}

// poisonTongues computes tongues creeping out along the floor from the middle: where each one's front is, how tall, how far along.
func poisonTongues(sh poisonSheet, si int, E, ph float64) []poisonTongue {
	out := make([]poisonTongue, 0, sh.n*2)
	for _, s := range [2]float64{-1, 1} {
		for i := 0; i < sh.n; i++ {
			off := 0.0
			if s > 0 {
				off = 0.5 / float64(sh.n)
			}
			q := math.Mod(ph+float64(i)/float64(sh.n)+float64(si)*0.17+off, 1)
			out = append(out, poisonTongue{s: s, q: q, x: s * E * (0.3 + 0.82*q), h: sh.h * (0.75 - 0.45*q)})
		}
	}
	return out
}

// poisonHeight computes a sheet's height above the floor at x.
func poisonHeight(sh poisonSheet, si int, E, x, ph float64, T []poisonTongue, seed float64) float64 {
	ax := math.Abs(x)
	env := 1 - pixelfx.Smooth(0.5, 1, ax/(E*sh.e))
	r := pixelfx.TileNoise(ax*0.07-ph*sh.flow, float64(si)*3.7, sh.flow, 0, seed+float64(si))
	h := sh.h * env * (0.55 + 0.45*r)
	for _, g := range T {
		behind := g.s * (g.x - x)
		if behind < -1.5 || behind > 30 {
			continue
		}
		h = math.Max(h, g.h*math.Sqrt(math.Max(0, 1-behind/30))*pixelfx.Smooth(-1.5, 1.5, behind)*(1-pixelfx.Smooth(0.75, 1, g.q)))
	}
	return h
}

// drawPoisonSheet draws one sheet of gas and its rolling leading-edge curls.
func drawPoisonSheet(p *pixelfx.Painter, C []uint32, sh poisonSheet, si int, E, t, seed float64) {
	ph := t / poisonCloudDuration
	T := poisonTongues(sh, si, E, ph)
	d := sh.dim
	X := int(math.Ceil(E * 1.25))
	for x := -X; x <= X; x++ {
		fx := float64(x)
		H := poisonHeight(sh, si, E, fx+0.5, ph, T, seed)
		if H < 0.6 {
			continue
		}
		top := -pixelfx.Round(H)
		sgn := 1.0
		if x != 0 {
			sgn = pixelfx.Sign(fx)
		}
		for y := top; y <= 1; y++ {
			v := pixelfx.Clamp(-y / H)
			streak := 0.7 + 0.3*pixelfx.TileNoise(fx*0.05-ph*sh.flow*sgn, y*0.55, sh.flow, 0, seed+9+float64(si))
			lv := v - (streak-0.7)*0.6
			var c uint32
			switch {
			case y == top:
				c = C[max(1, 4-d)]
			case lv < 0.16:
				c = C[6-d]
			case lv < 0.42:
				c = C[5-d]
			case lv < 0.68:
				c = C[4-d]
			default:
				c = C[max(1, 3-d)]
			}
			alpha := (0.6 + 0.4*(1-v)) * streak
			if y == top {
				alpha = 0.65
			}
			p.Blend(fx, y, c, sh.a*alpha)
		}
	}
	// the tongues' leading edges roll over in curls as they creep forward
	for _, g := range T {
		k := math.Sin(pi * g.q)
		if k < 0.15 || g.h < 4 {
			continue
		}
		r := 1.8 + g.h*0.22
		cx := g.x - g.s*r*0.6
		cy := -g.h * 0.72
		spin := -g.s * g.q * tau * 2.2
		// a roll of gas: a thick soft band winding in on itself, lighter along its outer edge
		for a := 0.0; a < 4.6; a += 0.1 {
			rr := r * (1 - a*0.12)
			px := cx + g.s*math.Cos(spin+a)*rr
			py := cy + math.Sin(spin+a)*rr
			w := 1.6 * (1 - a/6)
			for o := -w; o <= w; o += 0.8 {
				qx := px + g.s*math.Cos(spin+a)*o
				qy := py + math.Sin(spin+a)*o
				col := C[4-d]
				if o > w*0.4 {
					col = C[5-d]
				}
				p.Blend(qx, qy, col, sh.a*0.75*k*(1-a/5.5))
			}
		}
	}
}

// drawPoisonPlumes draws columns that lift off the sheets and twist into ribbons, thinning as they climb.
func drawPoisonPlumes(p *pixelfx.Painter, C []uint32, E, t, seed float64) {
	pixelfx.Cycle(t, poisonCloudDuration, 5, seed, func(g *pixelfx.Rng) float64 { return g.Range(2.4, 3.2) }, func(age, L float64, R *pixelfx.Rng, _ int) {
		u := age / L
		x0 := R.Range(-0.55, 0.55) * E
		rise := R.Range(13, 19)
		length := R.Range(34, 48)
		ph := R.Range(0, tau)
		amp := R.Range(3, 5)
		head := 14 + rise*age
		tail := math.Max(10, head-length)
		fade := pixelfx.Smooth(0, 0.15, u) * (1 - pixelfx.Smooth(0.5, 1, u))
		for h := tail; h <= head; h += 0.7 {
			s := (h - tail) / math.Max(1, head-tail)
			tw := math.Cos(h*0.22 + age*2.4 + ph)
			w := 0.6 + 2.2*math.Abs(tw)*(1-s*0.5)
			x := x0 + amp*math.Sin(h*0.12+age+ph)*(0.4+s)
			a := fade * math.Sin(pi*math.Min(1, s*1.2)) * 0.32
			for o := -w; o <= w; o += 1 {
				var col uint32
				if tw > 0 {
					if math.Abs(o) < w*0.5 {
						col = C[6]
					} else {
						col = C[5]
					}
				} else if math.Abs(o) < w*0.5 {
					col = C[3]
				} else {
					col = C[2]
				}
				p.Blend(x+o, -h, col, a)
			}
		}
	})
}

// drawPoisonSpecks draws specks drifting out through the gas with its flow, twinkling.
func drawPoisonSpecks(p *pixelfx.Painter, C []uint32, E, t, seed float64) {
	pixelfx.Cycle(t, poisonCloudDuration, 30, seed, func(g *pixelfx.Rng) float64 { return g.Range(1.5, 2.6) }, func(age, L float64, R *pixelfx.Rng, _ int) {
		u := age / L
		s := R.Sign()
		x := s * (R.Range(0.05, 0.7)*E + age*R.Range(6, 14))
		y := -R.Range(2, 20) - math.Sin(age*R.Range(1.5, 3)+R.Next()*tau)*2
		a := math.Min(1, u*5) * math.Min(1, (1-u)*3) * (0.6 + 0.4*math.Sin(age*R.Range(6, 10)+R.Next()*tau))
		if a < 0.1 {
			return
		}
		col := C[6]
		if a > 0.7 {
			col = C[7]
		}
		p.Dot(x, y, col, a*1.3)
		p.Glow(x, y, 4, C[6], 0.3*a)
	})
}

func newPoisonCloud(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	E := 96 * o.Num("spread")
	seed := o.Seed()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			pulse := 0.85 + 0.15*math.Sin((tau*t*2)/poisonCloudDuration)
			// lit from within, low down: a soft line of light along the floor under it all
			for x := -E; x <= E; x += 22 {
				p.Glow(x, -3, 30, C[5], 0.14*pulse*(1-math.Abs(x)/(E*1.3)))
			}
			// pockets of light wandering inside it, low down
			for i := 0; i < 6; i++ {
				R := pixelfx.RngOf(seed, float64(i), 7)
				x := math.Sin((tau*t)/poisonCloudDuration+R.Range(0, tau))*E*R.Range(0.2, 0.7) + R.Range(-0.3, 0.3)*E
				k := 0.6 + 0.4*math.Sin((tau*t*3)/poisonCloudDuration+float64(i))
				py := -R.Range(4, 10)
				r := R.Range(14, 22)
				p.Glow(x, py, r, C[6], 0.2*k)
			}
			for i, sh := range poisonSheets {
				if sh.back {
					drawPoisonSheet(p, C, sh, i, E, t, seed)
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			drawPoisonPlumes(p, C, E, t, seed*7+2)
			for i, sh := range poisonSheets {
				if !sh.back {
					drawPoisonSheet(p, C, sh, i, E, t, seed)
				}
			}
			drawPoisonSpecks(p, C, E, t, seed*11+5)
			p.Glow(0, -6, E*0.9, C[5], 0.1)
		},
	}
}
