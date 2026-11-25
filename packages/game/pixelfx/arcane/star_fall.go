// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/star-fall.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The slanted streaks landing with a ring and a kick of bits follow Sprite Fusion's open-source rain (https://www.spritefusion.com/pixel-art-vfx/rain), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// how many stars, how wide they land, how high they fall from, how far they slant, how long their trails are; variables so expressions with them round step by step as JavaScript's do
var (
	sfN      = 11
	sfSpread = 46.0
	sfH      = 124.0
	sfSlant  = 0.38
	sfTrail  = 0.17
)

// StarFall streaks stars down on long trails across an area, each landing with a white flash, a ring across the floor and a kick of sparks. It is registered as "arcane/star-fall".
var StarFall = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Star fall",
		Desc:     "Stars streak down on long trails across an area, each landing with a white flash, a ring across the floor and a kick of sparks.",
		Tags:     []string{"arcane", "star", "meteor", "rain", "aoe", "magic", "ground", "one-shot"},
		Size:     [2]int{232, 172},
		Origin:   [2]int{124, 150},
		Duration: 2.5,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(13)},
		},
	},
	New: newStarFall,
}

func init() { pixelfx.Register("arcane/star-fall", StarFall) }

type sfStar struct {
	x, y, sx, sy, t0, T, ph float64
	landed, big             bool
}

// sfHit is where and when a star landed.
type sfHit struct {
	x, y, t float64
	big     bool
}

func newStarFall(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: zeroGround, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}
	// where they land: spread over the area in a shuffled order, a little in front of or behind the line
	xs := make([]float64, sfN)
	for i := range xs {
		xs[i] = -sfSpread + ((float64(i)+R.Range(0.2, 0.8))*2*sfSpread)/float64(sfN)
	}
	for i := sfN - 1; i > 0; i-- {
		j := int(R.Int(0, float64(i)))
		xs[i], xs[j] = xs[j], xs[i]
	}
	stars := make([]sfStar, sfN)
	for i, x := range xs {
		y, h := R.Range(-3, 2), sfH*R.Range(0.85, 1)
		s := sfStar{x: x, y: y, sx: x - h*sfSlant, sy: y - h, t0: 0.04 + float64(i)*0.105 + R.Range(0, 0.05)}
		s.T = R.Range(0.3, 0.4)
		s.big = R.Chance(0.35)
		s.ph = R.Range(0, tau)
		stars[i] = s
	}
	var hits []sfHit
	t := 0.0
	at := func(s *sfStar, tt float64) (float64, float64, float64) {
		q := pixelfx.Clamp((tt - s.t0) / s.T)
		e := q * (0.35 + 0.65*q)
		return s.sx + (s.x-s.sx)*e, s.sy + (s.y-s.sy)*e, q
	}

	star := func(p *pixelfx.Painter, s *sfStar) {
		x, y, _ := at(s, t)
		// the trail: a long tapered streak back up its path, white at the head, thinning into violet
		const n = 20
		for k := n; k >= 1; k-- {
			fk := float64(k)
			ax, ay, _ := at(s, t-(fk*sfTrail)/n)
			bx, by, _ := at(s, t-((fk-1)*sfTrail)/n)
			u := fk / n
			if t-(fk*sfTrail)/n < s.t0 {
				continue
			}
			c := C[4]
			switch {
			case u < 0.15:
				c = pixelfx.White
			case u < 0.4:
				c = C[6]
			case u < 0.7:
				c = C[5]
			}
			// a second line beside the first near the head gives it body; the tail thins to a dithered thread
			if u < pickF(s.big, 0.55, 0.35) {
				p.Line(ax+1, ay, bx+1, by, pick(u < 0.2, C[6], C[5]), 1.1-u*1.4)
			}
			p.Line(ax, ay, bx, by, c, 1.3-u)
			if k%3 == 0 {
				p.Glow(ax, ay, 7*(1-u)+2, C[5], 0.3*(1-u))
			}
		}
		// the head: a twinkling four-point star
		if s.big {
			pixelfx.Flare(p, x, y, 7+1.5*math.Sin(t*40+s.ph), 0.8, C)
		} else {
			pixelfx.Sparkle(p, x, y, 3.5+math.Sin(t*40+s.ph), C[6], C[5], 0.6)
		}
		p.Circle(x, y, pickF(s.big, 1.6, 1.1), pixelfx.White, 1)
		p.Glow(x, y, pickF(s.big, 18, 13), C[5], 0.6)
		p.Glow(x, y, 5, pixelfx.White, 0.35)
	}

	hit := func(p *pixelfx.Painter, h *sfHit, far bool) {
		k := (t - h.t) / 0.45
		if k < 0 || k >= 1 {
			return
		}
		pixelfx.Band(p, h.x, h.y, 3+pickF(h.big, 22, 16)*pixelfx.Ease.OutCubic(k), 0.26, math.Max(1, 2.5*(1-k)), 1.15-k, []uint32{C[3], C[5], C[6], pixelfx.White}, func(g float64) bool { return math.Sin(g) < 0 == far }, k < 0.2)
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := range stars {
				s := &stars[i]
				if t < s.t0 || s.landed {
					continue
				}
				x, y, q := at(s, t)
				if R.Chance(0.55) {
					px, py := x+R.Range(-1.5, 1.5), y+R.Range(-1.5, 1.5)
					vx, vy := R.Range(-12, 12), R.Range(-8, 4)
					life := R.Range(0.35, 0.7)
					c := pixelfx.Pick(R, []uint32{pixelfx.White, C[6], C[5]})
					size := 0.0
					if R.Chance(0.7) {
						size = R.Range(0.4, 0.9)
					} else {
						size = R.Range(1.2, 1.8)
					}
					ps.Spawn(pixelfx.StarKind, px, py, vx, vy, life, c, size, 0, 0)
				}
				if q >= 1 {
					s.landed = true
					hits = append(hits, sfHit{x: s.x, y: s.y, t: t, big: s.big})
					lights.Add(s.x, s.y-12, pickF(s.big, 46, 38), C[5], 0.9, 0.32)
					lights.Add(s.x, s.y-3, 16, pixelfx.White, 0.8, 0.08)
					n := 9
					if s.big {
						n = 14
					}
					for range n {
						a, sp := -pi/2+R.Range(-1.35, 1.35), R.Range(50, 150)
						ps.Spawn(pixelfx.Spark, s.x, math.Min(-1, s.y-1), math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.2, 0.5), pixelfx.White, 1, 0, 0)
					}
					for range 4 {
						px, py := s.x+R.Range(-4, 4), s.y-R.Range(2, 8)
						vx, vy := R.Range(-15, 15), -R.Range(10, 35)
						life := R.Range(0.4, 0.8)
						c := pixelfx.Pick(R, []uint32{pixelfx.White, C[6]})
						ps.Spawn(pixelfx.StarKind, px, py, vx, vy, life, c, R.Range(0.8, 2), 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the area lit beneath while they fall
			if k := pixelfx.Clamp(t/0.3) * (1 - pixelfx.Clamp((t-1.7)/0.7)); k > 0 {
				for _, x := range [3]float64{-36, 0, 36} {
					p.Glow(x, -1, 40, C[4], 0.14*k)
				}
			}
			for i := range hits {
				hit(p, &hits[i], true)
			}
			// a glowing mark where each landed, cooling away
			for _, h := range hits {
				k2 := 1 - (t-h.t)/0.7
				if k2 <= 0 {
					continue
				}
				p.Dot(h.x, h.y, pick(k2 > 0.6, pixelfx.White, C[6]), k2*1.5)
				for _, dx := range [2]float64{-1, 1} {
					p.Dot(h.x+dx, h.y, C[5], k2*1.2)
				}
				p.Glow(h.x, h.y-1, 10, C[5], 0.35*k2)
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			for i := range stars {
				if s := &stars[i]; t >= s.t0 && !s.landed {
					star(p, s)
				}
			}
			for i := range hits {
				h := &hits[i]
				hit(p, h, false)
				k := (t - h.t) / 0.22
				if k < 0 || k >= 1 {
					continue
				}
				if k < 0.35 {
					p.Circle(h.x, h.y-2, 5*(1-k/0.35)+1, pixelfx.White, 1)
				}
				pixelfx.Flare(p, h.x, h.y-2, pickF(h.big, 16, 12)*math.Pow(1-k, 1.5)+2, 1-k, C)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
