// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/flame-pillar.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame-puff, smoke, spark, debris and floor-ring recipe from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), and the column's levels, pulses and strands from its holy light (https://www.spritefusion.com/pixel-art-vfx/holy-light), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// FlamePillar is the floor cracking and glowing, then a column of fire erupting white-hot and roaring, spewing embers and shedding flame, then collapsing into rolling smoke. It is registered as "fire/flame-pillar".
var FlamePillar = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Flame pillar",
		Desc:     "The floor cracks and glows, a column of fire erupts white-hot and roars, spewing embers and shedding flame, then collapses into rolling smoke.",
		Tags:     []string{"fire", "pillar", "eruption", "spell", "ground", "one-shot"},
		Size:     [2]int{184, 256},
		Origin:   [2]int{92, 232},
		Duration: pillarDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "stone", Default: pixelfx.Color("#4a3f69")},
			{Name: "height", Default: pixelfx.Num(140)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newFlamePillar,
}

func init() { pixelfx.Register("fire/flame-pillar", FlamePillar) }

const pillarDuration = 5

// When the column erupts, how long it takes to rise, when it starts to fall and when it is gone; variables so sums with them round as pixelfx's do.
var pillarErupt, pillarRise, pillarFall, pillarGone = 0.55, 0.12, 1.95, 2.45

// pillarCrack is a crack racing out across the floor: its points, when it starts and how long it takes to grow, and whether it is a main crack (1) or a branch (0).
type pillarCrack struct {
	pts         [][2]float64
	t0, grow, w float64
}

// pillarStrand is a strand of hotter flame running up the column: its offset across it, its half-width and how fast it sways.
type pillarStrand struct{ o, w, f float64 }

func newFlamePillar(o *pixelfx.Options) pixelfx.Instance {
	F := pixelfx.Fit(o.Palette(), 9)
	H, HMAX, st, R := hotFirst(F), o.Num("height"), stoneOf(o.Color("stone")), pixelfx.NewRng(o.Seed())
	D, ERUPT, RISE, FALL, GONE := float64(pillarDuration), pillarErupt, pillarRise, pillarFall, pillarGone
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{H[1], H[2], H[4], H[5]}})
	bits, lights := pixelfx.NewBits(floor), &pixelfx.Lights{}
	GLOW, S := pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.25), o.Seed()*13
	// the cracks: jagged lines racing out from the middle across the floor (squashed for perspective), with branches
	var cracks []pillarCrack
	crack := func(x, y, a, ln, t0, w float64) [][2]float64 {
		pts := [][2]float64{{x, y}}
		for d := 0.0; d < ln; d += 2 {
			a += R.Range(-0.55, 0.55)
			x += math.Cos(a) * 2
			y += math.Sin(a) * 0.76
			pts = append(pts, [2]float64{x, y})
		}
		cracks = append(cracks, pillarCrack{pts: pts, t0: t0, grow: R.Range(0.22, 0.36), w: w})
		return pts
	}
	for i := range 9 {
		a := (float64(i)/9)*tau + R.Range(-0.25, 0.25)
		pts := crack(R.Range(-1, 1), R.Range(-0.3, 0.3), a, R.Range(20, 40), R.Range(0, 0.16), 1)
		j := 3 + int(R.Int(0, float64(len(pts)-6)))
		if len(pts) > 8 {
			crack(pts[j][0], pts[j][1], a+R.Sign()*R.Range(0.5, 1), R.Range(6, 14), 0.18+R.Range(0, 0.15), 0)
		}
	}
	// strands of hotter flame running up the column
	var strands [7]pillarStrand
	for i := range strands {
		fi := float64(i)
		strands[i] = pillarStrand{o: (pixelfx.Rand(fi, S, 1) - 0.5) * 1.4, w: 0.05 + pixelfx.Rand(fi, S, 2)*0.06, f: 1 + pixelfx.Rand(fi, S, 3)*2}
	}
	t, erupted, fell := 0.0, false, false

	height := func(tt float64) float64 {
		switch {
		case tt < ERUPT:
			return 0
		case tt < ERUPT+RISE:
			return HMAX * pixelfx.Ease.OutCubic((tt-ERUPT)/RISE)
		case tt < FALL:
			return HMAX * (1 + 0.05*pixelfx.Noise(tt*3, S))
		}
		return HMAX * (1 - pixelfx.Ease.InQuad(pixelfx.Clamp((tt-FALL)/(GONE-FALL))))
	}
	width := func(tt float64) float64 {
		switch {
		case tt < ERUPT:
			return 0
		case tt < FALL:
			return 12 * (1 + 0.45*math.Exp(-(tt-ERUPT)*7)) * (0.94 + 0.06*pixelfx.Noise(tt*9, S+1))
		}
		return 12*(1-pixelfx.Smooth(FALL, GONE, tt)) + 1.5*(1-pixelfx.Smooth(GONE-0.1, GONE, tt))
	}
	heat := func(tt float64) float64 {
		return pixelfx.Smooth(0, ERUPT, tt)*0.75 + 0.25*pixelfx.Smooth(ERUPT-0.05, ERUPT+0.1, tt) - pixelfx.Smooth(GONE-0.3, D-0.6, tt)*1.05
	}
	lvlC := [8]uint32{H[0], H[1], H[2], H[3], H[4], H[5], H[6], H[7]}

	column := func(p *pixelfx.Painter) {
		h, w0 := height(t), width(t)
		if h < 1 || w0 < 0.3 {
			return
		}
		// a crown of tongues where the column breaks into the air, drawn first so the column flows up into them
		if t < FALL+0.25 {
			k := pixelfx.Clamp(w0 / 12)
			NZ := func(rate, seed, off float64) float64 { return pixelfx.Noise(t*rate+off, seed) }
			for i := range 5 {
				n, fi := (S+float64(i))*17, float64(i)
				dx := (fi - 2) * w0 * 0.4
				share, wk := 0.9, 1.0
				switch {
				case i == 2:
					share = 1.2
				case i%4 == 0:
					share, wk = 0.6, 0.7
				}
				th := (12 + 12*(0.5+0.5*pixelfx.Noise(t*3.3+fi, n))) * k * share
				flameTongue(p, dx+pixelfx.Noise(t*2, n+2)*1.5, -h+12, th, 3.4*k*wk, -dx*0.3+pixelfx.Noise(t*2.2, n+1)*3, NZ, n, H, layers, 0.5, 1.1)
			}
		}
		for y := 0.0; y >= -h; y-- {
			v := -y / h
			cx := pixelfx.Noise(-y*0.04+t*1.6, S+2) * 1.6 * v
			// licks bulge out of the sides and run up them; a wider splash at the foot; the top thins into tongues
			foot, top := 1+0.7*math.Pow(math.Max(0, 1- -y/12), 2), 1-pixelfx.Smooth(0.82, 1, v)*0.7
			wl := w0 * foot * top * (1 + 0.65*math.Pow(math.Max(0, pixelfx.Noise(y*0.2+t*8, S+3)), 1.5))
			wr := w0 * foot * top * (1 + 0.65*math.Pow(math.Max(0, pixelfx.Noise(y*0.2+t*8, S+4)), 1.5))
			pulse := 0
			if math.Mod(-y*0.032+t*3.6, 1) < 0.09 {
				pulse = 1
			}
			for x := math.Floor(cx - wl*1.15); x <= math.Ceil(cx+wr*1.15); x++ {
				dx := x + 0.5 - cx
				side := wr
				if dx < 0 {
					side = wl
				}
				q := math.Abs(dx) / side
				if q > 1.15 {
					continue
				}
				lvl := 7
				switch {
				case q < 0.12:
					lvl = 0
				case q < 0.26:
					lvl = 1
				case q < 0.42:
					lvl = 2
				case q < 0.56:
					lvl = 3
				case q < 0.7:
					lvl = 4
				case q < 0.84:
					lvl = 5
				case q < 0.96:
					lvl = 6
				}
				for _, s := range strands {
					if math.Abs(dx/w0-s.o-0.06*math.Sin(t*s.f*3+s.o*9)) < s.w {
						lvl = max(0, lvl-1)
					}
				}
				lvl = max(0, lvl-pulse)
				if lvl == 7 && pixelfx.Rand(x, y, math.Floor(t*20)) < 0.5 {
					continue
				}
				p.Px(x, y, lvlC[lvl])
			}
			if math.Mod(y, 12) == 0 {
				p.Glow(cx, y, w0*2.4+8, GLOW, 0.28*pixelfx.Clamp(w0/12))
			}
		}
		p.Glow(0, -h*0.45, 92, GLOW, 0.3*pixelfx.Clamp(w0/12))
	}

	cracksDraw := func(p *pixelfx.Painter) {
		k := heat(t)
		if k <= -0.05 {
			return
		}
		for ci := range cracks {
			c := &cracks[ci]
			nf := float64(len(c.pts))
			n := int(math.Floor(nf * pixelfx.Clamp((t-c.t0)/c.grow)))
			for i := 1; i < n; i++ {
				u := float64(i) / nf
				hot := k * (1 - 0.45*u)
				x0, y0, x1, y1 := c.pts[i-1][0], c.pts[i-1][1], c.pts[i][0], c.pts[i][1]
				if hot < 0 {
					// cooled: soot that fades away
					if a := 1 + hot*3; a > 0 {
						p.Line(x0, y0, x1, y1, soot, a)
					}
					continue
				}
				col := H[7]
				switch {
				case hot > 0.85:
					col = H[1]
				case hot > 0.65:
					col = H[2]
				case hot > 0.45:
					col = H[4]
				case hot > 0.25:
					col = H[5]
				case hot > 0.1:
					col = H[6]
				}
				// the crack: a hot seam with a darker glowing lip, wider near the middle
				if c.w != 0 && float64(i) < nf*0.5 && hot > 0.2 {
					lip := H[6]
					if hot > 0.6 {
						lip = H[5]
					}
					p.Line(x0, y0+1, x1, y1+1, lip, 1)
					p.Line(x0, y0-1, x1, y1-1, H[7], 1)
				}
				p.Line(x0, y0, x1, y1, col, 1)
				if i%2 == 0 {
					p.Glow(x1, y1, 5+7*hot, GLOW, 0.3*hot)
				}
			}
		}
		// the floor glows from beneath while it's hot
		if k > 0 {
			floorLight(p, 0, 0.5, 44, 8, GLOW, 0.35*k)
		}
	}
	bandRamp := []uint32{st.body, st.light, H[4], H[1]}
	gritCols := []uint32{st.light, st.body, st.body, st.shade}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the charge: the crust in the middle glows, little flames lick out of it, heat leaks up the cracks
			if t < ERUPT && R.Chance(0.6*dt*60*pixelfx.Smooth(0.15, ERUPT, t)) {
				ps.Spawn(pixelfx.Fire, R.Range(-5, 5), -1, R.Range(-6, 6), -R.Range(20, 45), R.Range(0.15, 0.3), 0, R.Range(1, 2.2), 0, 0)
			}
			if t < ERUPT && R.Chance(0.5*dt*60*pixelfx.Smooth(0, ERUPT, t)) {
				c := pixelfx.Pick(R, cracks)
				i := int(R.Int(0, math.Floor(float64(len(c.pts))*pixelfx.Clamp((t-c.t0)/c.grow))-1))
				if i >= 0 {
					x, y := c.pts[i][0], c.pts[i][1]
					if R.Chance(0.5) {
						ps.Spawn(pixelfx.Fire, x, y-1, R.Range(-5, 5), -R.Range(15, 35), R.Range(0.15, 0.3), 0, R.Range(0.8, 1.6), 0, 0)
					} else {
						ps.Spawn(pixelfx.Ember, x, y-1, R.Range(-15, 15), -R.Range(30, 70), R.Range(0.5, 1), H[3], 1, 0, 0)
					}
				}
			}
			if !erupted && t >= ERUPT {
				erupted = true
				lights.Add(0, -40, 90, warm, 0.7, 0.6)
				lights.Add(0, -8, 44, pixelfx.White, 0.7, 0.1)
				for range 70 {
					a, sp := -pi/2+R.Range(-1.1, 1.1), R.Range(100, 260)
					ps.Spawn(pixelfx.Spark, R.Range(-4, 4), -2, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.25, 0.7), pixelfx.White, 1, 0, 0)
				}
				for range 18 {
					bits.Add(pixelfx.Bit{Kind: pixelfx.Chunk, X: R.Range(-6, 6), Y: -2, VX: R.Range(-110, 110), VY: -R.Range(150, 330), G: 1, Drag: 0.4, Bounce: 0.3, Life: R.Range(1.6, 2.4), A: R.Range(0, tau), VA: R.Range(-16, 16), S: R.Int(1, 3)})
				}
				for range 60 {
					a, sp := -pi/2+R.Range(-0.9, 0.9), R.Range(80, 260)
					size := 1.0
					x, life, c := R.Range(-5, 5), R.Range(1, 1.8), pixelfx.Pick(R, gritCols)
					if R.Chance(0.4) {
						size = 2
					}
					ps.Spawn(pixelfx.Grit, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, c, size, 0, 0)
				}
				for range 14 {
					s := R.Sign()
					ps.Spawn(pixelfx.Smoke, s*R.Range(2, 10), -R.Range(1, 4), s*R.Range(40, 110), -R.Range(2, 10), R.Range(1, 1.8), st.dust, R.Range(4, 7), R.Range(0, 0.06), pixelfx.Big)
				}
			}
			// the roar: flame shed off the sides, a billowing cap, embers spewed high
			h, w := height(t), width(t)
			if t > ERUPT && t < GONE && h > 4 {
				if R.Chance(0.9 * dt * 60) {
					s := R.Sign()
					y := -R.Range(4, h*0.9)
					x, vx, vy, life, size := s*w*R.Range(0.7, 1.1), s*R.Range(15, 40), -R.Range(30, 70), R.Range(0.25, 0.5), R.Range(1.6, 3.2)
					var flag pixelfx.Flag
					if R.Chance(0.35) {
						flag = pixelfx.Smokes
					}
					ps.Spawn(pixelfx.Fire, x, y, vx, vy, life, 0, size, 0, flag)
				}
				if R.Chance(0.7 * dt * 60) {
					x, y, vx, vy, life, size := R.Range(-w, w), -h+R.Range(0, 6), R.Range(-20, 20), -R.Range(30, 80), R.Range(0.35, 0.7), R.Range(3, 5.5)
					var flag pixelfx.Flag
					if R.Chance(0.6) {
						flag = pixelfx.Smokes
						if t < FALL-0.3 {
							flag |= pixelfx.Big
						}
					}
					ps.Spawn(pixelfx.Fire, x, y, vx, vy, life, 0, size, 0, flag)
				}
				if t < FALL && R.Chance(0.5*dt*60) {
					x, vx, vy, life := R.Range(-w, w)*0.6, R.Range(-60, 60), -R.Range(50, 110), R.Range(0.7, 1.3)
					size := 1.0
					if R.Chance(0.2) {
						size = 2
					}
					ps.Spawn(pixelfx.Ember, x, -h+4, vx, vy, life, H[3], size, 0, 0)
				}
			}
			// the collapse: the falling top breaks into puffs that roll off as smoke
			if t >= FALL && !fell {
				fell = true
			}
			if fell && t < GONE-0.1 && R.Chance(1.6*dt*60) {
				ps.Spawn(pixelfx.Fire, R.Range(-w-2, w+2), -h-R.Range(0, 8), R.Range(-30, 30), -R.Range(10, 50), R.Range(0.4, 0.75), 0, R.Range(3.5, 6.5), 0, pixelfx.Smokes)
			}
			bits.Update(dt)
			for i := range bits.List {
				b := &bits.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					for range 3 {
						ps.Spawn(pixelfx.Dust, b.X+R.Range(-1.5, 1.5), -0.5, R.Sign()*R.Range(8, 35), -R.Range(2, 18), R.Range(0.3, 0.6), st.dust, R.Range(1.3, 2.6), 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			cracksDraw(p)
			simDraw(p, ps, H, pixelfx.LayerBack)
			if k := (t - ERUPT) / 0.45; k > 0 && k < 1 {
				pixelfx.Band(p, 0, 0.5, 84*pixelfx.Ease.OutCubic(k), 0.18, math.Max(1, 4*(1-k)), 1.15-k, bandRamp, farHalf, false)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			column(p)
			for i := range bits.List {
				rock(p, &bits.List[i], st)
			}
			simDraw(p, ps, H, pixelfx.LayerFront)
			if k := (t - ERUPT) / 0.45; k > 0 && k < 1 {
				pixelfx.Band(p, 0, 0.5, 84*pixelfx.Ease.OutCubic(k), 0.18, math.Max(1, 4*(1-k)), 1.15-k, bandRamp, nearHalf, k < 0.15)
			}
			// the eruption's white flash at the foot
			if f := (t - ERUPT) / 0.08; f > 0 && f < 1 {
				p.Ellipse(0, -2, 16*(1-f*0.3), 6, H[2], 1)
				c := H[1]
				if f < 0.5 {
					c = pixelfx.White
				}
				p.Ellipse(0, -3, 11*(1-f*0.3), 4, c, 1)
			}
			lights.Draw(p)
		},
	}
}
