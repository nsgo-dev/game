// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/holy-pillar.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package holy

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// HolyPillar is light breaking through from above: a sun opens at the top and a fan of shafts converges on the mark, pulsing, while a sigil burns on the floor; then the fan folds shut. It is registered as "holy/holy-pillar".
var HolyPillar = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Holy pillar",
		Desc:     "Light breaks through from above: a sun opens at the top and a fan of shafts converges on the mark, pulsing, while a sigil burns on the floor; then the fan folds shut.",
		Tags:     []string{"holy", "pillar", "light", "rays", "aoe", "sigil", "ground", "one-shot"},
		Size:     [2]int{192, 232},
		Origin:   [2]int{96, 204},
		Duration: 3.5,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(Gold...)},
			{Name: "shafts", Default: pixelfx.Num(7)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newHolyPillar,
}

func init() { pixelfx.Register("holy/holy-pillar", HolyPillar) }

// pillarW is the width of the rows the fan is gathered in, centred on the mark.
const pillarW = 192

var (
	pillarTop, pillarPY, pillarSY, pillarRX = -204.0, -1.0, 0.24, 26.0
	pillarSun                               = pillarTop + 38
	// the sun opens, the fan unfolds and pours down, it holds and pulses, folds shut, the light lifts away
	pillarOpen, pillarHit, pillarFold, pillarShut, pillarLift = 0.3, 0.66, 2.05, 2.45, 2.75
)

// pillarShaft is one shaft of the fan: its spread angle th, how fast it widens, how bright it is (b), its pulse offset and how far out in the fan it is (0 the middle, 1 the edge).
type pillarShaft struct{ th, spread, b, ph, far float64 }

type pillarMote struct{ x, y, v, amp, f, ph float64 }

func newHolyPillar(o *pixelfx.Options) pixelfx.Instance {
	C, R, W := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), pixelfx.White
	N := max(3, int(pixelfx.ToInt32(o.Num("shafts"))))
	mid := float64(N-1) / 2
	TOP, PY, SUN, SY, RX := pillarTop, pillarPY, pillarSun, pillarSY, pillarRX
	OPEN, HIT, FOLD, SHUT, LIFT := pillarOpen, pillarHit, pillarFold, pillarShut, pillarLift
	WING := []uint32{C[2], C[3], C[4], C[5], C[6], W}
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return 0 }, Sparks: []uint32{W, C[6], C[5], C[3]}})
	lights, feathers := &pixelfx.Lights{}, newFeathers(R, 0)
	// the shafts: spread angle, how fast each widens, how bright, its pulse offset
	shafts := make([]pillarShaft, N)
	for i := range shafts {
		off := float64(i) - mid
		far := math.Abs(off) / mid
		th := off*(0.4/mid) + R.Range(-0.02, 0.02)
		spread := R.Range(0.05, 0.085) * (1 - far*0.25)
		b := (1 - far*0.35) * R.Range(0.8, 1)
		shafts[i] = pillarShaft{th: th, spread: spread, b: b, ph: R.Range(0, 1), far: far}
	}
	motes := make([]pillarMote, 72)
	for i := range motes {
		x, y, v, amp, f, ph := R.Range(-80, 80), R.Range(0, 1), R.Range(7, 18), R.Range(0.5, 2.5), R.Range(0.6, 1.5), R.Range(0, tau)
		motes[i] = pillarMote{x, y, v, amp, f, ph}
	}
	for i := range 6 {
		x, y, vx, vy, a := R.Range(-46, 46), R.Range(-150, -100), R.Range(-8, 8), R.Range(0, 8), R.Range(-0.4, 0.4)
		fl, fw, life, sink := R.Range(7, 9), R.Range(1.9, 2.3), R.Range(1.4, 1.7), R.Range(26, 36)
		delay := HIT + 0.1 + float64(i)*0.16 + R.Range(0, 0.08)
		feathers.add(looseFeather{x: x, y: y, vx: vx, vy: vy, a: a, len: fl, wid: fw, life: life, sink: sink, back: i%3 == 0, delay: delay})
	}
	DMAX := PY - TOP
	var row [pillarW]float32
	var core [pillarW]uint8
	t, hit, shut, glinted := 0.0, false, false, false

	// each shaft's angle now: unfolding from the middle with an overshoot, folding back shut at the end
	angle := func(s *pillarShaft, tt float64) float64 {
		st := s.far * 0.1
		open := pixelfx.OutBack(pixelfx.Seg(tt, OPEN+st, OPEN+st+0.42), 1.5)
		fold := pixelfx.Ease.InOutCubic(pixelfx.Seg(tt, FOLD-s.far*0.1, SHUT))
		return s.th * open * (1 - fold)
	}
	// how far down each shaft the light reaches (it pours from the sky), and where it lifts away at the end
	frontD := func(s *pillarShaft, tt float64) float64 {
		return DMAX * (1 - pixelfx.Ease.InQuad(pixelfx.Seg(tt, OPEN+s.far*0.06, HIT+s.far*0.06)))
	}
	liftD := func(tt float64) float64 { return DMAX * pixelfx.Ease.InQuad(pixelfx.Seg(tt, SHUT+0.05, LIFT)) }
	fade := func(tt float64) float64 {
		return pixelfx.Smooth(OPEN, OPEN+0.1, tt) * (1 - pixelfx.Smooth(LIFT-0.05, LIFT+0.05, tt))
	}
	width := func(s *pillarShaft, d, tt float64) float64 {
		return (1.4 + d*s.spread) * pixelfx.Lerp(1, 0.25, pixelfx.Smooth(SHUT, LIFT, tt))
	}
	// brightness along a shaft: a pulse of light runs down each one, one after another
	pulse := func(s *pillarShaft, d, tt float64) float64 {
		if tt < HIT || tt > SHUT {
			return 1
		}
		D := DMAX * (1 - pixelfx.Fract((tt-HIT)/0.6+s.ph))
		e := (d - D) / 12
		return 1 + 0.9*math.Exp(-(e*e))
	}
	// light of all the shafts at a point (for motes)
	lightAt := func(x, y float64) float64 {
		dy := PY - y
		if dy <= 0 {
			return 0
		}
		I := 0.0
		for i := range shafts {
			s := &shafts[i]
			a := angle(s, t)
			d, xc := dy/math.Cos(a), math.Tan(a)*dy
			hw := width(s, d, t) / 2 / math.Cos(a)
			if d < liftD(t) || d < frontD(s, t) {
				continue
			}
			if v := (x - xc) / hw; math.Abs(v) < 1 {
				I += (1 - v*v) * s.b
			}
		}
		return I * fade(t)
	}

	fan := func(p *pixelfx.Painter) {
		k := fade(t)
		if k <= 0 {
			return
		}
		lift := liftD(t)
		for y := math.Ceil(TOP); y < PY; y++ {
			dy := PY - (y + 0.5)
			row = [pillarW]float32{}
			core = [pillarW]uint8{}
			any := false
			for si := range shafts {
				s := &shafts[si]
				a := angle(s, t)
				ca := math.Cos(a)
				d := dy / ca
				if d < lift || d < frontD(s, t)-6 {
					continue
				}
				xc, hw := math.Tan(a)*dy, width(s, d, t)/2/ca
				soft := pixelfx.Smooth(frontD(s, t)-6, frontD(s, t)+4, d) * pixelfx.Smooth(lift, lift+6, d)
				I0 := s.b * pulse(s, d, t) * soft * k
				for x := math.Floor(xc - hw); x <= math.Ceil(xc+hw); x++ {
					v, i := (x+0.5-xc)/hw, int(x)+pillarW/2
					if i < 0 || i >= pillarW || math.Abs(v) >= 1 {
						continue
					}
					// row is a Float32Array in pixelfx: each sum is rounded to float32 as it is stored
					row[i] = float32(float64(row[i]) + I0*math.Pow(1-v*v, 1.3))
					any = true
				}
				// core is a Uint8Array in pixelfx: each value is truncated to a byte as it is stored
				if ci := int(pixelfx.Round(xc-0.5)) + pillarW/2; ci >= 0 && ci < pillarW {
					core[ci] = uint8(pixelfx.ToInt32(math.Max(float64(core[ci]), math.Min(255, I0*200))))
				}
			}
			if !any {
				continue
			}
			for i := range pillarW {
				I := float64(row[i])
				if I < 0.02 {
					continue
				}
				x := float64(i - pillarW/2)
				p.Blend(x, y, C[6], math.Min(0.6, I*0.38))
				p.Light(x, y, C[4], math.Min(0.9, I*0.32))
				// a fine bright line runs down the middle of each shaft
				if core[i] != 0 {
					c := C[6]
					if core[i] > 150 {
						c = W
					}
					p.Dot(x, y, c, math.Min(1, float64(core[i])/180))
				}
			}
		}
		// where the shafts converge, the light gathers to a point
		if t > HIT-0.05 {
			p.Glow(0, PY-3, 20, C[5], 0.5*k)
		}
	}

	// the sun: a halo that opens at the top like an iris, rays ticking round it, then closes with a glint
	sun := func(p *pixelfx.Painter) {
		r := 15 * pixelfx.OutBack(pixelfx.Seg(t, 0.02, 0.45), 1.8) * (1 - pixelfx.InBack(pixelfx.Seg(t, SHUT, LIFT+0.1), 1.5))
		if r > 0.5 {
			spin := t * 0.6
			p.Glow(0, SUN, math.Min(32, r*3), C[4], 0.5)
			p.Glow(0, SUN, r*1.4, W, 0.35)
			p.Circle(0, SUN, r*0.45, W, 1)
			oval(p, 0, SUN, r*0.62, r*0.62, C[6], 1, nil)
			ovalFn(p, 0, SUN, r, r, func(g float64) (uint32, float64) {
				if math.Sin(g*6+spin*4) > 0.5 {
					return W, 1
				}
				return C[5], 1
			}, nil)
			for j := range 16 {
				g := spin + (float64(j)*tau)/16
				l := 6.0
				if j%2 != 0 {
					l = 3
				}
				p.LineFn(math.Cos(g)*(r+2), SUN+math.Sin(g)*(r+2), math.Cos(g)*(r+2+l), SUN+math.Sin(g)*(r+2+l), func(u float64) (uint32, float64) {
					if u < 0.4 {
						return C[6], 1.1 - u
					}
					return C[4], 1.1 - u
				})
			}
		}
		if t > LIFT && t < LIFT+0.3 {
			pixelfx.Sparkle(p, 0, SUN, 10*(1-(t-LIFT)/0.3), W, C[5], 0.6)
		}
	}

	// the sigil burning on the floor: two rings and a turning band of marks; white-hot, then gold, then cooling
	sigil := func(p *pixelfx.Painter, back bool) {
		if t < HIT {
			return
		}
		u, heat, out := pixelfx.Seg(t, HIT, HIT+0.2), 1-pixelfx.Smooth(SHUT, SHUT+0.8, t), 1-pixelfx.Smooth(SHUT+0.4, SHUT+1, t)
		if out <= 0 {
			return
		}
		keep := halfOf(back)
		// it burns in round from the front, both ways
		burnt := func(g float64) bool { return math.Abs(wrapPi(g-pi/2)) <= u*pi+0.01 }
		col := func(g float64, hot bool) uint32 {
			if !burnt(g) {
				return 0
			}
			flick := pixelfx.Rand(pixelfx.Round(g*40), math.Floor(t*20)) < 0.18
			switch {
			case t < HIT+0.08 || (flick && heat > 0.4):
				return W
			case heat > 0.6 && hot:
				return C[6]
			case heat > 0.6:
				return C[5]
			case heat > 0.3:
				return C[4]
			case heat > 0.1:
				return C[3]
			}
			return C[2]
		}
		keepBurnt := func(g float64) bool { return keep(g) && burnt(g) }
		ovalFn(p, 0, 0, RX, RX*SY, func(g float64) (uint32, float64) { return col(g, true), out }, keepBurnt)
		ovalFn(p, 0, 0, RX*0.72, RX*0.72*SY, func(g float64) (uint32, float64) { return col(g, false), out }, keepBurnt)
		// marks between the rings, turning slowly
		for j := range 12 {
			g := t*0.35 + (float64(j)*tau)/12
			if !keep(g) || !burnt(g) {
				continue
			}
			c, r0 := col(g, j%2 == 0), RX*0.78
			r1 := RX * 0.88
			if j%3 == 0 {
				r1 = RX * 0.95
			}
			if c != 0 {
				p.Line(math.Cos(g)*r0, math.Sin(g)*r0*SY, math.Cos(g)*r1, math.Sin(g)*r1*SY, c, out)
			}
		}
		if !back {
			for i := -2; i <= 2; i++ {
				p.Glow(float64(i)*RX*0.45, -2, 16, C[4], (0.34-math.Abs(float64(i))*0.05)*out*(0.5+0.5*heat))
			}
			p.Glow(0, -1, 10, W, 0.3*heat*out)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// light gathers to the sun as it opens
			if t < OPEN+0.1 && R.Chance(0.5) {
				a, D := R.Range(0, tau), R.Range(22, 34)
				x, y := math.Cos(a)*D, SUN+math.Sin(a)*D
				T := R.Range(0.18, 0.28)
				v := ((D - 10) * 3) / (math.Exp(3*T) - 1)
				ps.Spawn(pixelfx.Streak, x, y, (-x/D)*v, ((SUN-y)/D)*v, T, chance(R, 0.5, C[6], C[5]), 1, 0, 0)
			}
			if !hit && t >= HIT {
				hit = true
				lights.Add(0, -30, 64, C[5], 0.85, 0.5)
				lights.Add(0, -6, 30, W, 0.8, 0.12)
				for range 26 {
					a, sp := -pi/2+R.Range(-1.2, 1.2), R.Range(70, 190)
					x, life := R.Range(-3, 3), R.Range(0.25, 0.6)
					ps.Spawn(pixelfx.Spark, x, -2, math.Cos(a)*sp, math.Sin(a)*sp, life, W, 1, 0, 0)
				}
			}
			// while it holds: embers off the burning sigil, a few feathers drifting down through the shafts
			if t > HIT && t < SHUT+0.1 && R.Chance(0.35) {
				g := R.Range(0, tau)
				r := RX * 0.72
				if R.Chance(0.5) {
					r = RX
				}
				var flag pixelfx.Flag
				if math.Sin(g) < 0 {
					flag = pixelfx.Back
				}
				vx, vy, life := R.Range(-6, 6), -R.Range(12, 30), R.Range(0.5, 0.8)
				ps.Spawn(pixelfx.Ember, math.Cos(g)*r, math.Sin(g)*r*SY-1, vx, vy, life, C[5], 1, 0, flag)
			}
			if t > HIT && t < FOLD && R.Chance(0.05) {
				x, y, vy, life := R.Range(-1, 1)*40, -R.Range(20, 160), R.Range(2, 8), R.Range(0.3, 0.6)
				col, size := chance(R, 0.5, W, C[6]), R.Range(0.8, 1.8)
				ps.Spawn(pixelfx.StarKind, x, y, 0, vy, life, col, size, 0, 0)
			}
			// the fan snaps shut on the mark: a flash where the shafts become one
			if !shut && t >= SHUT {
				shut = true
				lights.Add(0, -60, 70, C[5], 0.7, 0.35)
				for range 12 {
					x, y, vx, vy, life := R.Range(-4, 4), -R.Range(10, 170), R.Range(-25, 25), R.Range(-10, 10), R.Range(0.4, 0.8)
					col, size := chance(R, 0.5, W, C[6]), R.Range(0.8, 2)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, 0, 0)
				}
			}
			if !glinted && t >= LIFT {
				glinted = true
				lights.Add(0, SUN, 40, W, 0.6, 0.2)
			}
			ps.Update(dt)
			feathers.update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			fan(p)
			sigil(p, true)
			feathers.draw(p, WING, pixelfx.LayerBack)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			sigil(p, false)
			// dust motes drifting down, lit only where a shaft catches them
			for _, m := range motes {
				y := TOP + math.Mod(math.Mod(m.y*DMAX+t*m.v, DMAX)+DMAX, DMAX)
				x := m.x + math.Sin(t*m.f+m.ph)*m.amp
				I := lightAt(x, y)
				if I < 0.2 {
					continue
				}
				c := C[6]
				if I > 0.7 {
					c = W
				}
				p.Dot(x, y, c, math.Min(1, I*1.3))
				p.Light(x, y, C[5], 0.4*math.Min(1, I))
			}
			sun(p)
			if t > HIT-0.02 && t < HIT+0.25 {
				pixelfx.Sparkle(p, 0, PY-2, 12*(1-(t-HIT)/0.25), W, C[5], 0.7)
			}
			if t > SHUT && t < SHUT+0.2 {
				pixelfx.Sparkle(p, 0, PY-2, 8*(1-(t-SHUT)/0.2), W, C[5], 0.5)
			}
			feathers.draw(p, WING, pixelfx.LayerFront)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
