// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/burn-away.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez, for the layered flame tongues and the licks, embers and smoke shed off a burning edge.

package transitions

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// BurnAway is a fire screen transition: the screen burns in from its edges behind a ragged ember line of licking flames, embers and smoke, then smoulders and burns open from the middle. It is registered as "transitions/burn-away".
var BurnAway = &pixelfx.Effect{
	Meta: screenMeta(
		"Burn away",
		"The screen burns in from its edges behind a ragged ember line of licking flames, embers and smoke; then the black smoulders and burns open from the middle.",
		[]string{"burn", "fire", "embers", "smoke", "wipe"},
		3,
		pixelfx.Option{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
		pixelfx.Option{Name: "seed", Default: pixelfx.Num(3)},
	),
	New: newBurnAway,
}

func init() { pixelfx.Register("transitions/burn-away", BurnAway) }

var (
	burnD        = 3.0
	burnClose    = 1.18
	burnCover    = 1.35
	burnSmoulder = 1.5
	burnPop      = 1.8
	burnOpen     = 2.62
	burnSC       = 90.0
	burnHalo     = 24.0
	burnPool     = 48.0
	burnAsh      = pixelfx.Ramp("#a39cab", "#7b7484", "#5c5664", "#433e4b")
	burnLayers   = [4]struct {
		lw, lh float64
		ri     int
	}{
		{1, 1, 2},
		{0.76, 0.86, 4},
		{0.54, 0.68, 6},
		{0.3, 0.42, 8},
	}
	burnRateIn  = [6]float64{0.006, 0.0014, 0.0006, 0.0024, 0.0012, 0.0004}
	burnRateOut = [6]float64{0.006, 0.0005, 0.0004, 0.0012, 0.0004, 0.0004}
)

func burnInField(seed float64) *scalarField {
	return newField(func(x, y float64) float64 {
		ux := (x - W/2) / (W / 2)
		uy := (y - H/2) / (H / 2)
		ux2, uy2 := ux*ux, uy*uy
		m := math.Pow(ux2*ux2+uy2*uy2, 0.25)
		return (1-m)*burnSC + 14*pixelfx.Fbm(x/44, y/44, seed, 3) + 2.4*pixelfx.Fbm(x/7, y/7, seed+9, 2) + 7*(0.5-y/H)
	}, 0.45, 3)
}

func burnOutField(seed float64) *scalarField {
	R := pixelfx.NewRng(seed*31 + 5)
	ox := W/2 + R.Range(-26, 26)
	oy := H/2 + R.Range(-14, 6)
	type sat struct{ x, y, d float64 }
	var sats [2]sat
	for idx, s := range [2]float64{-1, 1} {
		sats[idx] = sat{x: ox + s*R.Range(70, 110), y: oy + R.Range(-40, 40), d: R.Range(30, 44)}
	}
	f := newField(func(x, y float64) float64 {
		v := pixelfx.Hypot((x-ox)/(W/2), (y-oy)/(H/2)) * burnSC
		for _, s := range sats {
			v = math.Min(v, s.d+pixelfx.Hypot((x-s.x)/(W/2), (y-s.y)/(H/2))*burnSC*1.15)
		}
		return v + 13*pixelfx.Fbm(x/40, y/40, seed+21, 3) + 2.4*pixelfx.Fbm(x/7, y/7, seed+33, 2)
	}, 0.45, 3)
	f.ox = ox
	f.oy = oy
	return f
}

func burnTongue(p *pixelfx.Painter, F []uint32, x, y, h, w, sway, t, n float64) {
	for _, l := range burnLayers {
		Ht, col := h*l.lh, F[l.ri]
		for r := 0.0; r <= Ht; r++ {
			u := r / math.Max(1, Ht)
			half := w*l.lw*math.Pow(1-u, 0.85)*math.Min(1, (r+1.5)/3.5) + pixelfx.Noise(t*9+r*0.45, n)*0.5
			if half < 0.3 {
				continue
			}
			cx := x + sway*u*u*l.lh + pixelfx.Noise(t*4+r*0.2, n+7)*u*1.1
			ry := pixelfx.Round(y - r)
			for px, px1 := pixelfx.Round(cx-half), pixelfx.Round(cx+half); px <= px1; px++ {
				p.Px(px, ry, col)
			}
		}
	}
}

func newBurnAway(o *pixelfx.Options) pixelfx.Instance {
	F := pixelfx.Fit(o.Palette(), 9)
	ink := o.Color("color")
	R := pixelfx.NewRng(o.Seed())
	seed := o.Seed() * 13
	CHAR := pixelfx.Mix(ink, F[0], 0.45)
	SCORCH := pixelfx.Mix(ink, pixelfx.Hex("#3a2216"), 0.6)
	WARM := pixelfx.Mix(F[4], F[5], 0.4)
	SMOKE := pixelfx.Mix(pixelfx.Hex("#8a7276"), F[2], 0.2)
	A, B := burnInField(seed), burnOutField(seed+1)

	const total = W * H
	heat := make([]float32, total)
	ph := make([]uint8, total)
	fleck := make([]float32, total)
	jit := make([]float32, total)
	vein := make([]float32, total)
	for i := range total {
		x, y := float64(i%W), float64(i/W)
		heat[i] = float32(0.5 + 0.5*pixelfx.Noise2(x/13, y/13, seed+41))
		ph[i] = uint8(math.Min(63, math.Floor((0.5+0.5*pixelfx.Noise2(x/23, y/23, seed+43))*64)))
		if pixelfx.Rand(x, y, seed+45) < 0.04 {
			fleck[i] = float32(9 + 22*pixelfx.Rand(x, y, seed+47))
		}
		jit[i] = float32((pixelfx.Rand(x, y, seed+49) - 0.5) * 1.5)
		rx, ry := x*0.8+y*0.6, y*0.8-x*0.6
		vein[i] = float32(math.Min(math.Abs(pixelfx.Noise2(rx/11, ry/9, seed+61)), math.Abs(pixelfx.Noise2(ry/5.5, rx/5, seed+63))*1.5))
	}
	var FL, HV [64]float32
	var TW [64]uint8

	kIn := func(t float64) float64 {
		return A.lo - 8 + (A.hi-A.lo+8)*math.Pow(math.Max(0, t)/burnClose, 1.25)
	}
	kOut := func(t float64) float64 {
		if t < burnPop {
			return B.lo - 24 + 24*pixelfx.Ease.InQuad(pixelfx.Clamp((t-burnSmoulder)/(burnPop-burnSmoulder)))
		}
		return B.lo + (B.hi-B.lo+16)*pixelfx.Ease.OutQuad(pixelfx.Clamp((t-burnPop)/(burnOpen-burnPop)))
	}

	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{F[7], F[6], F[4], F[2]}})
	ash := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: burnAsh})
	lights := &pixelfx.Lights{}
	var line []int
	t := 0.0
	pin, pout := 0, 0
	closed, popped := false, false

	shed := func(i int, inA bool) {
		rate := burnRateOut
		end := burnD - 0.05
		if inA {
			rate = burnRateIn
			end = burnPop + 0.1
		}
		fl, em, sm, sh, as, li := rate[0], rate[1], rate[2], rate[3], rate[4], rate[5]
		x, y := float64(i%W)+0.5, float64(i/W)+0.5
		r := R.Next()
		if r -= fl; r < 0 {
			life := lifeLeft(R.Range(0.18, 0.4), t, end)
			if life > 0.08 {
				vx := R.Range(-8, 8)
				vy := -R.Range(25, 50)
				ps.Spawn(pixelfx.Fire, x, y-3, vx, vy, life, 0, R.Range(0.8, 1.7), 0, 0)
			}
		} else if r -= em; r < 0 {
			var life float64
			if inA {
				life = lifeLeft(R.Range(0.6, 1.3), t, end)
			} else {
				life = lifeLeft(R.Range(0.5, 1), t, end)
			}
			if life > 0.2 {
				vx := R.Range(-30, 30)
				vy := -R.Range(40, 100)
				sz := 1.0
				if R.Chance(0.25) {
					sz = 2
				}
				ps.Spawn(pixelfx.Ember, x, y-2, vx, vy, life, 0, sz, 0, 0)
			}
		} else if hi := float64(heat[i]); func() bool { r -= sm + sh*hi*hi; return r < 0 }() {
			n := int(R.Int(4, 6))
			vx := R.Range(-6, 6)
			fd := B
			if inA {
				fd = A
			}
			up := fd.ny[i] < -0.25
			for j := range n {
				var base float64
				if up {
					base = R.Range(0.8, 1.3)
				} else {
					base = R.Range(0.35, 0.65)
				}
				life := lifeLeft(base, t+float64(j)*0.05, end)
				if life > 0.4 {
					px := x + R.Range(-3.5, 3.5)
					py := y - 4 - float64(j)*1.2
					pvx := vx + R.Range(-3, 3)
					pvy := -R.Range(18, 34)
					ps.Spawn(pixelfx.Smoke, px, py, pvx, pvy, life, SMOKE, R.Range(2.8, 4.8), float64(j)*0.05, 0)
				}
			}
		} else if r -= as; r < 0 {
			life := lifeLeft(R.Range(0.9, 1.6), t, end)
			if life > 0.3 {
				vx := R.Range(-18, 18)
				vy := -R.Range(25, 60)
				ash.Spawn(pixelfx.Ember, x, y-2, vx, vy, life, pixelfx.White, 1, 0, 0)
			}
		} else if r -= li; r < 0 {
			rad := R.Range(20, 32)
			lk := R.Range(0.14, 0.26)
			life := R.Range(0.18, 0.4)
			lights.Add(x, y-4, rad, WARM, lk, life)
		}
	}

	LIT := pixelfx.Mix(pixelfx.Hex("#8a7276"), F[3], 0.25)
	DIM := pixelfx.SmokeRamp[3]
	drawSmoke := func(p, p0 *pixelfx.Painter) {
		inPhase := t < burnCover
		open := t >= burnSmoulder && t < burnOpen+0.25
		k := kOut(t)
		f := B.f
		if inPhase {
			k = kIn(t)
			f = A.f
		}
		for qi := range ps.List {
			q := &ps.List[qi]
			if q.K != pixelfx.Smoke || q.Delay > 0 {
				continue
			}
			kk := q.Life / q.Max
			age := 1 - kk
			r := q.Size * (0.65 + math.Min(1, age*2.5)*0.45 + age*0.4)
			i := int(pixelfx.ClampTo(math.Floor(q.Y), 0, H-1))*W + int(pixelfx.ClampTo(math.Floor(q.X), 0, W-1))
			e := 99.0
			if inPhase || open {
				if inPhase {
					e = k - float64(f[i])
				} else {
					e = float64(f[i]) - k
				}
			}
			lit := pixelfx.Clamp(1 - math.Abs(e)/30)
			var vis float64
			switch {
			case e > 0:
				vis = 0.05 + 0.95*lit
			case inPhase:
				vis = 1
			default:
				vis = 0.2 + 0.8*lit
			}
			a := math.Min(1, age*12) * math.Min(1, kk*2.4) * 0.58 * vis
			if a < 0.04 {
				continue
			}
			c := pixelfx.Mix(pixelfx.Mix(DIM, LIT, lit), pixelfx.SmokeRamp[2], age*0.5)
			d := p
			if q.X-r > 0 && q.Y-r > 0 && q.X+r < W-1 && q.Y+r < H-1 {
				d = p0
			}
			d.Disc(q.X, q.Y, r, c, a*0.45)
			d.Disc(q.X+r*0.05, q.Y+r*0.06, r*0.74, c, a*0.62)
			if age < 0.5 {
				d.Disc(q.X-r*0.22, q.Y-r*0.3, r*0.5, pixelfx.Mix(c, pixelfx.White, 0.15), a*0.4*(1-age*2))
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < burnCover {
				k := kIn(t)
				for pin < len(A.order) && float64(A.f[A.order[pin]]) <= k {
					shed(int(A.order[pin]), true)
					pin++
				}
				if !closed && t >= burnClose {
					closed = true
					i := int(A.order[len(A.order)-1])
					x, y := float64(i%W), float64(i/W)
					lights.Add(x, y, 64, WARM, 0.5, 0.4)
					for range 12 {
						px := x + R.Range(-6, 6)
						py := y + R.Range(-3, 3)
						vx := R.Range(-50, 50)
						vy := -R.Range(40, 120)
						life := R.Range(0.5, 0.75)
						sz := 1.0
						if R.Chance(0.3) {
							sz = 2
						}
						ps.Spawn(pixelfx.Ember, px, py, vx, vy, life, 0, sz, 0, 0)
					}
					for range 5 {
						px := x + R.Range(-8, 8)
						py := y + R.Range(-4, 2)
						vx := R.Range(-10, 10)
						vy := -R.Range(12, 28)
						life := R.Range(1.1, 1.6)
						sz := R.Range(3.5, 5.5)
						dl := R.Range(0, 0.15)
						ps.Spawn(pixelfx.Smoke, px, py, vx, vy, life, SMOKE, sz, dl, 0)
					}
				}
			}
			if t >= burnSmoulder && t < burnPop && R.Chance(0.1) {
				i := int(B.order[int(R.Int(0, 400))])
				x, y := float64(i%W)+0.5, float64(i/W)+0.5
				if R.Chance(0.6) {
					vx := R.Range(-4, 4)
					vy := -R.Range(8, 16)
					life := lifeLeft(R.Range(0.9, 1.4), t, burnD-0.05)
					sz := R.Range(2, 3.5)
					ps.Spawn(pixelfx.Smoke, x, y, vx, vy, life, SMOKE, sz, 0, 0)
				} else {
					vx := R.Range(-12, 12)
					vy := -R.Range(15, 40)
					life := R.Range(0.4, 0.8)
					ps.Spawn(pixelfx.Ember, x, y, vx, vy, life, pixelfx.White, 1, 0, 0)
				}
			}
			if !popped && t >= burnPop {
				popped = true
				lights.Add(B.ox, B.oy, 90, WARM, 0.6, 0.45)
				lights.Add(B.ox, B.oy, 34, F[7], 0.7, 0.12)
				for range 12 {
					a := R.Range(0, tau)
					sp := R.Range(50, 150)
					life := R.Range(0.4, 0.9)
					sz := 1.0
					if R.Chance(0.3) {
						sz = 2
					}
					ps.Spawn(pixelfx.Ember, B.ox+math.Cos(a)*3, B.oy+math.Sin(a)*2, math.Cos(a)*sp, math.Sin(a)*sp*0.7-40, life, 0, sz, 0, 0)
				}
				for range 10 {
					a := R.Range(0, tau)
					sp := R.Range(120, 260)
					life := R.Range(0.15, 0.35)
					ps.Spawn(pixelfx.Spark, B.ox, B.oy, math.Cos(a)*sp, math.Sin(a)*sp-50, life, pixelfx.White, 1, 0, 0)
				}
			}
			if t >= burnPop && t < burnOpen+0.2 {
				k := kOut(t)
				for pout < len(B.order) && float64(B.f[B.order[pout]]) <= k {
					shed(int(B.order[pout]), false)
					pout++
				}
			}
			ps.Update(dt)
			ash.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p0 *pixelfx.Painter, _ float64) {
			p := clipTo(p0)
			line = line[:0]
			if t >= burnCover && t < burnSmoulder {
				rect(p0, 0, 0, W, H, ink)
			} else if t < burnOpen+0.25 {
				inPhase := t < burnCover
				k := kOut(t)
				Fd := B
				fade := 1.0
				if inPhase {
					k = kIn(t)
					Fd = A
					fade = 1 - pixelfx.Smooth(burnCover-0.15, burnCover, t)
				}
				f, g := Fd.f, Fd.g
				for j := range 64 {
					q := float64(j) / 63
					FL[j] = float32(0.75 + 0.25*math.Sin(t*7+q*9))
					HV[j] = float32(0.3 * math.Sin(t*5.5+q*11))
					if math.Sin(t*13+q*40) > -0.5 {
						TW[j] = 1
					} else {
						TW[j] = 0
					}
				}
				px := newPixelWriter(p0)
				ord := Fd.order
				N := len(ord)
				su := pixelfx.Clamp((t - burnSmoulder) / (burnPop - burnSmoulder))
				SR := 5 + 17*su
				sk := 0.0
				if !inPhase && t >= burnSmoulder {
					if t < burnPop {
						sk = 0.3 + 0.7*su
					} else {
						sk = 1 - (t-burnPop)/0.3
					}
				}
				shadeAt := func(i int) {
					var e float64
					if inPhase {
						e = k - float64(f[i])
					} else {
						e = float64(f[i]) - k
					}
					s := e * float64(g[i])
					px.at(i)
					if e < 0 {
						if inPhase && s > -2.5 {
							px.blend(SCORCH, 0.5*(1+s/2.5))
						}
						u := math.Max(0, 1+e/burnHalo)
						v := 1 + e/burnPool
						px.add(WARM, (0.3*u*u+0.12*v*v)*float64(FL[ph[i]]))
						return
					}
					hv := pixelfx.Clamp(float64(heat[i]) + float64(HV[ph[i]]))
					th := 0.6 + 0.6*hv
					u := s / th
					if u < 6.4 {
						if u < 0.9 {
							line = append(line, i)
						}
						var col uint32
						switch {
						case u < 0.9:
							if hv > 0.6 {
								col = F[8]
							} else {
								col = F[7]
							}
						case u < 1.7:
							col = F[6]
						case u < 2.4:
							col = F[5]
						case u < 3.1:
							col = F[4]
						case u < 3.8:
							col = F[3]
						case u < 4.6:
							col = F[2]
						case u < 5.5:
							col = F[1]
						default:
							col = F[0]
						}
						px.put(col, 1)
						if u < 4.6 {
							px.add(WARM, 0.45*(1-u/4.6)*(0.7+0.3*hv))
						}
						return
					}
					if sk > 0 && float64(f[i]) < B.lo+SR {
						hm := (1 - (float64(f[i])-B.lo)/SR) * sk * (0.7 + 0.6*hv)
						if float64(vein[i]) < 0.04+0.08*hm {
							var col uint32
							switch {
							case hm > 0.95:
								col = F[6]
							case hm > 0.75:
								col = F[5]
							case hm > 0.55:
								col = F[4]
							case hm > 0.38:
								col = F[3]
							case hm > 0.22:
								col = F[2]
							default:
								col = F[1]
							}
							px.put(col, 1)
							px.add(WARM, 0.4*hm)
							return
						}
						px.add(WARM, 0.2*hm*hm)
					}
					L := float64(fleck[i]) * (0.8 + 0.4*hv)
					if L > 0 && s < L {
						v := (s - 6.4*th) / math.Max(1, L-6.4*th)
						if v < 1 && TW[(int(ph[i])*5)&63] != 0 && !(fade < 1 && fade <= float64(jit[i])/1.5+0.5) {
							var col uint32
							switch {
							case v < 0.3:
								col = F[4]
							case v < 0.55:
								col = F[3]
							case v < 0.8:
								col = F[2]
							default:
								col = F[1]
							}
							px.put(col, 1)
							px.add(WARM, 0.3*(1-v))
							return
						}
					}
					if e < 22 {
						em := 1 - e/22
						px.add(WARM, 0.2*em*em)
					}
					col := ink
					if u-6.4 < 1.6*th+float64(jit[i]) {
						col = CHAR
					}
					px.put(col, 1)
				}
				loOff, hiOff := burnPool, 33.0
				if inPhase {
					loOff, hiOff = 33.0, burnPool
				}
				hiVal := k + hiOff
				if sk > 0 {
					hiVal = math.Max(hiVal, B.lo+SR)
				} else {
					hiVal = math.Max(hiVal, -1e9)
				}
				lo, hi := Fd.below(k-loOff), Fd.below(hiVal)
				c0, c1, a0, a1 := hi, N, lo, hi
				if inPhase {
					c0, c1, a0, a1 = 0, lo, lo, hi
				}
				for j := c0; j < c1; j++ {
					i := int(ord[j])
					if fleck[i] != 0 {
						shadeAt(i)
					} else {
						px.at(i)
						px.put(ink, 1)
					}
				}
				for j := a0; j < a1; j++ {
					shadeAt(int(ord[j]))
				}
				for _, i := range line {
					x, y := i%W, i/W
					dn := float64(Fd.ny[i])
					if inPhase {
						dn = -dn
					}
					h0 := pixelfx.Rand(float64(x), float64(y>>3), seed+51)
					if h0 > 0.12+0.6*math.Max(0, dn) {
						continue
					}
					n := float64((x*7 + (y>>4)*131) % 977)
					hv := float64(heat[i])
					leap := 1 + math.Max(0, pixelfx.Noise(t*2.6+n*3.1, n+70)-0.5)*1.8
					dnMul := 0.5
					if dn > 0 {
						dnMul = 0.65 + 0.5*dn
					}
					h := (5 + 7*pixelfx.Rand(float64(x), 1, seed+53)) * (0.6 + 0.4*pixelfx.Noise(t*3.3+n*1.7, n)) * (0.6 + 0.6*hv) * leap * fade * dnMul
					if h >= 1.5 {
						burnTongue(p, F, float64(x)+0.5, float64(y)+1, h, (1.1+0.7*hv)*(0.8+h/16), pixelfx.Noise(t*2.2+n, n+40)*1.5, t, n)
					}
				}
			}
			drawSmoke(p, p0)
			ash.Draw(p, pixelfx.LayerFront)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
