// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/necro-skulls.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package shadow

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// NecroSkulls orbits spectral skulls around a cold green flame on a tilted path, passing behind and before it, eyes lit, trailing curls of ghost smoke. It is registered as "shadow/necro-skulls".
var NecroSkulls = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Necro skulls",
		Desc:     "Spectral skulls circle a cold green flame on a tilted orbit, passing behind and before it, eyes lit, trailing curls of ghost smoke.",
		Tags:     []string{"shadow", "necromancy", "skull", "orbit", "ghost", "spectral", "magic", "loop"},
		Size:     [2]int{136, 116},
		Origin:   [2]int{68, 108},
		Duration: 3,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#040e10", "#0a2228", "#0f3d44", "#16615f", "#23907f", "#45c4a0", "#9af0c8", "#e8fff4")},
			{Name: "bone", Default: pixelfx.Palette("#0c1a1c", "#2c4a4a", "#5c847c", "#a6cdb8", "#e4fff0")},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newNecroSkulls,
}

func init() { pixelfx.Register("shadow/necro-skulls", NecroSkulls) }

// the flame's base and height; the orbit (centre, radii, tilt); five skulls, one turn a loop
const (
	necroCY   = -52.0
	necroFB   = -32.0
	necroFH   = 34.0
	necroRX   = 42.0
	necroRY   = 11.0
	necroTilt = -0.12
	necroD    = 3.0
	necroN    = 5
)

var necroSkullSprite = []string{
	"...kkkkk...",
	".kkhhhllkk.",
	"khhhhlllmmk",
	"khhllllllmk",
	"kleeeleeemk",
	"kleeeleeedk",
	"kmleelleedk",
	".kmllnllmk.",
	".kmmlnlmdk.",
	"..kmmmmmk..",
	"..ktgtgtk..",
	"...kkkkk...",
}

type necroPos struct {
	i          int
	x, y, z, a float64
}

// necroSkullAt returns a skull's place at t: round the tilted orbit, bobbing; z > 0 is in front of the flame.
func necroSkullAt(i int, t float64) necroPos {
	a := (tau*t)/necroD + (float64(i)*tau)/necroN
	ex, ey := math.Cos(a)*necroRX, math.Sin(a)*necroRY
	bob := math.Sin((tau*2*t)/necroD+float64(i)*1.7) * 2
	ct, st := math.Cos(necroTilt), math.Sin(necroTilt)
	return necroPos{i: i, x: ex*ct - ey*st, y: necroCY + ex*st + ey*ct + bob, z: math.Sin(a), a: a}
}

// necroTail draws ghost smoke curling off behind each skull along its orbit: puffs that swell, rise and thin with age.
func necroTail(p *pixelfx.Painter, C []uint32, i int, t float64) {
	for k := 8; k >= 1; k-- {
		tt := t - float64(k)*0.05
		s := necroSkullAt(i, tt)
		age := float64(k) / 8
		r := 2 + age*3.6
		a := (1 - age) * 0.5 * (0.6 + 0.4*(s.z+1)/2)
		c := C[3]
		if age < 0.4 {
			c = C[5]
		} else if age < 0.7 {
			c = C[4]
		}
		p.Disc(s.x, s.y-age*7+2, r, c, a)
	}
}

func necroDrawSkull(p *pixelfx.Painter, C, K []uint32, s necroPos, t float64, i int) {
	front := s.z >= 0
	depth := 0.55 + 0.45*(s.z+1)/2
	var pal map[rune]uint32
	if front {
		pal = map[rune]uint32{'k': K[0], 'd': K[1], 'm': K[2], 'l': K[3], 'h': K[4], 'e': C[0], 'n': C[0], 't': K[3], 'g': K[0]}
	} else {
		pal = map[rune]uint32{'k': K[0], 'd': K[0], 'm': K[1], 'l': K[2], 'h': K[3], 'e': C[0], 'n': C[0], 't': K[2], 'g': K[0]}
	}
	x0, y0 := pixelfx.Round(s.x)-5, pixelfx.Round(s.y)-6
	p.Sprite(necroSkullSprite, pal, x0, y0, 1, pixelfx.SpriteOpts{})
	// eyes: cold points of light in the dark, flaring as the skull swings toward us
	k := (0.5+0.5*math.Sin((tau*3*t)/necroD+float64(i)*2.1))*0.5 + 0.5
	for _, ex := range [2]float64{3, 7} {
		ec := C[6]
		if front {
			ec = pick(k > 0.75, pixelfx.White, C[7])
		}
		p.Px(x0+ex+0.5, y0+5.5, ec)
		if front {
			p.Px(x0+ex+0.5, y0+4.5, C[5])
		}
		p.Glow(x0+ex+1, y0+6, 6, C[5], 0.45*depth*k)
	}
	p.Glow(s.x, s.y, 11, C[4], 0.18*depth)
}

// necroFlame draws the cold flame: a body that swells off its base and tapers into licking tongues, white only low in its heart, cooling outward and upward through the ghost greens, scrolling up (the noise wraps once a loop).
func necroFlame(p *pixelfx.Painter, C []uint32, t float64) {
	const W = 10.0
	scroll := t / necroD
	p.Shade(-W-4, necroFB-necroFH-4, W+4, necroFB+2, func(x, y float64, out *pixelfx.Out) uint32 {
		u := (necroFB - y) / necroFH
		if u < -0.04 || u >= 1 {
			return 0
		}
		n1 := pixelfx.TileNoise(x*0.3, y*0.2+scroll*8, 0, 8, 3)
		n2 := pixelfx.TileNoise(x*0.6, y*0.45+scroll*16, 0, 16, 5)
		var prof float64
		if u < 0.22 {
			prof = 0.35 + 0.65*math.Sqrt(math.Max(0, u)/0.22)
		} else {
			prof = math.Pow(1-(u-0.22)/0.78, 0.85)
		}
		w := W*prof*(1+0.3*n1) + n2*2.2*u
		sway := n1 * 3 * u * u
		d := math.Abs(x-sway) / math.Max(0.6, w)
		if d > 1 {
			return 0
		}
		heat := math.Pow(1-d, 0.7)*math.Pow(math.Max(0, 1-u), 0.55) + n2*0.12
		if (pixelfx.ToInt32(math.Floor(x)+math.Floor(y)) & 1) == 0 {
			out.K = 0.16 * heat
		}
		switch {
		case heat > 0.82:
			return pixelfx.White
		case heat > 0.66:
			return C[7]
		case heat > 0.46:
			return C[6]
		case heat > 0.27:
			return C[5]
		case heat > 0.12:
			return C[4]
		}
		return C[3]
	})
	p.Glow(0, necroFB-necroFH*0.35, 40, C[5], 0.45)
	p.Glow(0, necroFB-5, 12, C[7], 0.25)
}

func necroOrder(t float64, front bool) []necroPos {
	order := make([]necroPos, 0, necroN)
	for i := range necroN {
		s := necroSkullAt(i, t)
		if (s.z >= 0) == front {
			order = append(order, s)
		}
	}
	slices.SortStableFunc(order, func(a, b necroPos) int { return cmp.Compare(a.z, b.z) })
	return order
}

func newNecroSkulls(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	K := pixelfx.Fit(o.Ramp("bone"), 5)
	S := o.Seed()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			p.Glow(0, -1, 40, C[4], 0.2)
			// the far half of the orbit: tails and skulls behind the flame, back to front
			order := necroOrder(t, false)
			for _, s := range order {
				necroTail(p, C, s.i, t)
			}
			for _, s := range order {
				necroDrawSkull(p, C, K, s, t, s.i)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			// motes and cold sparks rising off the flame
			pixelfx.Cycle(t, necroD, 18, S, func(r *pixelfx.Rng) float64 { return r.Range(0.8, 1.4) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				x0 := r.Range(-6, 6)
				u := age / L
				x := x0 + math.Sin(u*5+r.Range(0, 6))*3*u
				y := necroFB - 6 - u*r.Range(36, 56)
				al := 1.0
				if u >= 0.7 {
					al = (1 - u) * 3.3
				}
				p.Dot(x, y, pick(u < 0.5, C[7], C[6]), al)
				p.Glow(x, y, 3, C[6], 0.3*(1-u))
			})
			necroFlame(p, C, t)
			// the near half of the orbit, back to front
			order := necroOrder(t, true)
			for _, s := range order {
				necroTail(p, C, s.i, t)
			}
			for _, s := range order {
				necroDrawSkull(p, C, K, s, t, s.i)
			}
			halo(p, 0, necroCY, 0, 46, C[4], 0.12)
		},
	}
}
