// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/teleporter-pad.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source heal (https://www.spritefusion.com/pixel-art-vfx/heal) for the rising band and the column of light it rides in, MIT License, Copyright (c) 2026 Hugo Duprez.

package scifi

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// TeleporterPad is a steel pad glowing on the floor, its rings rippling outward, with a soft column of light rising off it, rings of light lifting away and sparkles drifting up through them. It is registered as "scifi/teleporter-pad".
var TeleporterPad = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Teleporter pad",
		Desc:     "A steel pad glows on the floor, its rings rippling outward; a soft column of light rises off it and rings of light lift away, sparkles drifting up through them.",
		Tags:     []string{"scifi", "teleporter", "pad", "portal", "beam", "spawn", "ground", "loop"},
		Size:     [2]int{96, 104},
		Origin:   [2]int{48, 92},
		Duration: tpDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#120a26", "#26145a", "#3c2296", "#5a3ad2", "#7f66f0", "#b4a2ff", "#e6deff", "#ffffff")},
			{Name: "metal", Default: pixelfx.Palette("#0e0d14", "#1e1c27", "#312e3e", "#4b4759", "#6f6a82", "#9d98b0")},
			{Name: "seed", Default: pixelfx.Num(26)},
		},
	},
	New: newTeleporterPad,
}

func init() { pixelfx.Register("scifi/teleporter-pad", TeleporterPad) }

const (
	tpDuration = 2.4
	tpRX       = 31.0
	tpRY       = 7.0
	tpPY       = -3.0
	tpIn       = 0.78
	tpHigh     = -84.0
)

// tpPad draws the pad: a steel rim with a lit far edge, a recessed disc of light whose rings ripple outward, a hot middle.
func tpPad(p *pixelfx.Painter, t float64, C, H []uint32) {
	ph := t / tpDuration
	p.Shade(-tpRX-1, tpPY-tpRY-1, tpRX+1, 2, func(x, y float64, out *pixelfx.Out) uint32 {
		u, v := x/tpRX, (y-tpPY)/tpRY
		d := math.Sqrt(u*u + v*v)
		// the drum's side, below the top face
		if d > 1 {
			vy := (y - 3 - tpPY) / tpRY
			if math.Abs(x) <= tpRX && y > tpPY && y <= 1 && math.Sqrt(u*u+vy*vy) <= 1 {
				switch {
				case y == 1 || math.Abs(x) > tpRX-1:
					out.C = H[0]
				case x < -tpRX*0.4:
					out.C = H[3]
				case x > tpRX*0.4:
					out.C = H[1]
				default:
					out.C = H[2]
				}
			}
			return 0
		}
		if d > 0.97 {
			out.C = H[0]
			return 0
		}
		if d > tpIn {
			// the rim: lit along its far edge, shadowed on the near one, a seam round it
			switch {
			case math.Abs(d-(tpIn+0.1)) < 0.03:
				out.C = H[1]
			case d < tpIn+0.03:
				out.C = C[3]
			case v < -0.3:
				out.C = H[5]
			case v < 0.2:
				out.C = H[4]
			default:
				out.C = H[3]
			}
			return 0
		}
		// the light well: rings rolling outward, brighter toward the middle
		r := d / tpIn
		ring := pixelfx.Wrap(r*3-ph*3, 1) < 0.16
		heat := math.Pow(1-r, 1.5)
		switch {
		case r > 0.94:
			out.C = C[3]
		case ring:
			if heat > 0.3 {
				out.C = pixelfx.White
			} else {
				out.C = C[6]
			}
		case heat > 0.55:
			out.C = C[6]
		case heat > 0.25:
			out.C = C[5]
		case r > 0.8:
			out.C = C[3]
		default:
			out.C = C[4]
		}
		return 0
	})
	// emitters round the rim, chasing
	for i := range 10 {
		a := (float64(i)/10)*tau + 0.15
		x, y := math.Cos(a)*tpRX*0.885, tpPY+math.Sin(a)*tpRY*0.885
		k := 0.5 + 0.5*math.Cos(tau*(2*ph-float64(i)/10))
		c := C[2]
		if k > 0.7 {
			c = pixelfx.White
		} else if k > 0.4 {
			c = C[5]
		}
		p.Px(x, y, c)
		if k > 0.5 {
			p.Glow(x, y, 4, C[4], 0.35*k)
		}
	}
}

// tpLift draws rings of light lifting off the pad: crisp lines stepping down the ramp as they climb, a glint running round each; its far half or its near half.
func tpLift(p *pixelfx.Painter, t float64, C []uint32, near bool) {
	for i := range 4 {
		k := since(t, (float64(i)/4)*tpDuration, tpDuration) / 1.25
		if k >= 1 {
			continue
		}
		y := pixelfx.Lerp(tpPY, tpHigh+12, math.Pow(k, 0.85))
		rx := tpRX * tpIn * (1 + 0.08*k)
		ry := rx * (tpRY / tpRX)
		c := C[3]
		switch {
		case k < 0.12:
			c = pixelfx.White
		case k < 0.4:
			c = C[6]
		case k < 0.65:
			c = C[5]
		case k < 0.85:
			c = C[4]
		}
		glint := math.Mod(float64(i)*0.37+k*1.5, 1) * tau
		n := int(math.Ceil(rx * 7))
		for j := range n {
			g := (float64(j) / float64(n)) * tau
			sg := math.Sin(g)
			if (sg >= 0) != near {
				continue
			}
			x, yy := math.Cos(g)*rx, y+sg*ry
			hot := math.Abs(math.Mod(g-glint+pi*3, tau)-pi) < 0.25
			if k > 0.85 && j&1 != 0 {
				continue
			}
			col := C[3]
			if hot && k < 0.8 {
				col = pixelfx.White
			} else if near {
				col = c
			} else if k < 0.4 {
				col = C[5]
			}
			p.Px(x, yy, col)
			if k < 0.3 {
				c2 := C[3]
				if near {
					c2 = C[5]
				}
				p.Px(x, yy+1, c2)
			}
		}
		if near {
			p.Glow(0, y+ry*0.5, rx+8, C[4], 0.22*(1-k))
		}
	}
}

func newTeleporterPad(o *pixelfx.Options) pixelfx.Instance {
	C, H, seed := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("metal"), 6), o.Seed()
	rn := pixelfx.NewRng(seed)
	g := newGhosts(tpDuration, ghostOpts{
		hasBox: true,
		box:    [4]float64{-46, -90, 46, 10},
	})
	// motes and sparkles drifting up through the column
	for i := range 40 {
		a := rn.Range(0, tau)
		r := math.Sqrt(rn.Next()) * tpRX * tpIn * 0.9
		star := i%3 == 0
		born := rn.Range(0, tpDuration)
		kind := pixelfx.Mote
		if star {
			kind = pixelfx.StarKind
		}
		vx, vy := rn.Range(-4, 4), -rn.Range(30, 75)
		life := rn.Range(0.7, 1.3)
		col := pixelfx.Pick(rn, []uint32{pixelfx.White, C[6], C[5]})
		var size float64
		if star {
			size = rn.Range(1.8, 3.4)
		} else {
			size = rn.Range(0.6, 1.2)
		}
		g.add(born, kind, math.Cos(a)*r, tpPY+math.Sin(a)*r*(tpRY/tpRX), vx, vy, life, col, size, 0)
	}

	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			tpPad(p, t, C, H)
			// the column: a soft tube of light standing on the well, fading as it rises, light drawn up it
			W := tpRX * tpIn
			for y := tpHigh; y < tpPY; y++ {
				v := (y - tpPY) / (tpHigh - tpPY)
				fade := math.Pow(1-v, 1.6)
				for x := math.Floor(-W); x <= math.Ceil(W); x++ {
					q := math.Abs(x+0.5) / W
					if q > 1 {
						continue
					}
					c := C[5]
					if q > 0.9 {
						c = C[6]
					}
					p.Blend(x+0.5, y+0.5, c, fade*(0.07+0.26*math.Pow(q, 4)+0.12*math.Pow(1-v, 4)))
				}
			}
			pixelfx.Cycle(t, tpDuration, 30, seed+3, func(r *pixelfx.Rng) float64 { return r.Range(0.6, 1.1) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				k := age / life
				x := r.Range(-0.9, 0.9) * W
				y := pixelfx.Lerp(tpPY-2, tpHigh+10, k)
				L := 3 + 7*k
				a := (1 - k) * 1.2
				p.LineFn(x, y, x, y+L, func(u float64) (uint32, float64) {
					if u < 0.3 {
						return C[6], a
					}
					return C[4], a
				})
			})
			tpLift(p, t, C, false)
			for y := tpPY - 6; y > tpHigh+20; y -= 12 {
				p.Glow(0, y, W+8, C[4], 0.16*(1-(y-tpPY)/(tpHigh-tpPY)))
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / tpDuration
			tpLift(p, t, C, true)
			g.at(t).Draw(p, pixelfx.LayerBoth)
			// the light: a pool on the floor, a hot heart to the well, pulsing
			k := 0.85 + 0.15*math.Sin(tau*2*ph)
			for _, x := range [3]float64{-tpRX * 0.6, 0, tpRX * 0.6} {
				p.Glow(x, tpPY, 20, C[4], 0.25*k)
			}
			p.Glow(0, tpPY, 10, pixelfx.White, 0.3*k)
		},
	}
}
