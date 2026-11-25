// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/icicle-fall.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The break recipe (a white frame, the ice flung apart as shards with ice dust and glints) is adapted from Sprite Fusion's open-source ice (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package ice

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// IcicleFall is an icicle that drips and trembles, snaps off its frosted root, drops, stabs the floor and bursts into skidding shards, splinters and rolling frost. It is registered as "ice/icicle-fall".
var IcicleFall = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Icicle fall",
		Desc:     "An icicle drips and trembles, snaps off its frosted root, drops, stabs the floor and bursts into skidding shards, splinters and rolling frost.",
		Tags:     []string{"ice", "icicle", "fall", "trap", "shatter", "one-shot"},
		Size:     [2]int{152, 120},
		Origin:   [2]int{76, 100},
		Duration: icicleDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(icePalette...)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newIcicleFall,
}

func init() { pixelfx.Register("ice/icicle-fall", IcicleFall) }

var (
	icicleDuration = 2.6
	// where it hangs, how long, how wide at the root
	icicleRoot, icicleLen, icicleW = -80.0, 36.0, 12.0
	icicleDrip, icicleTremble      = 0.16, 0.3
	icicleSnap                     = 0.6
	icicleFree                     = icicleSnap + 0.04
	icicleFallT                    = math.Sqrt((2 * (-icicleRoot - icicleLen)) / pixelfx.Gravity)
	icicleImpact                   = icicleFree + icicleFallT
)

// icicleOpts are how icicleShape draws: white flashes it, glint (0..1) puts a highlight across it at that height (below 0 none), tilt leans it.
type icicleOpts struct {
	white       bool
	glint, tilt float64
}

// icicleShape draws the icicle: a rippled taper hanging from (x, top), lit down its left side with a bright core streak, rings of light at the ripples, deep on the right, a dark rim.
func icicleShape(p *pixelfx.Painter, C []uint32, x, top, length, w float64, o icicleOpts) {
	hw := func(u float64) float64 {
		return (w/2)*math.Pow(1-u, 0.75)*(1+0.14*math.Sin(u*13+0.5))*pick(u < 0.08, 1.15, 1) + 0.3
	}
	tilt, cut := o.tilt, 0.0
	p.Shade(x-w/2-4, top-1, x+w/2+4+math.Abs(tilt)*length, top+length+1, func(px, py float64, out *pixelfx.Out) uint32 {
		u := (py - top) / length
		cx := x + tilt*length*u + 0.7*math.Sin(u*6+1)
		s := px - cx
		if u < cut || u > 1 {
			return 0
		}
		h := hw(u)
		if math.Abs(s) > h+1 {
			return 0
		}
		if math.Abs(s) > h {
			out.C = C[1]
			return 0
		}
		if o.white {
			out.C = pixelfx.White
			return out.C
		}
		q, ring := s/h, math.Sin(u*13+0.5) > 0.7
		var c uint32
		switch {
		case u > 0.88:
			c = pixelfx.White
		case q < -0.72:
			c = C[6]
		case q < -0.2:
			c = pick(ring, C[7], C[5])
		case q < 0.18:
			c = pick(u < 0.72, C[7], C[6])
		case q > 0.8:
			c = C[2]
		case math.Abs(q-0.45) < 0.1 && u > 0.15 && u < 0.6:
			c = C[5]
		case q < 0.6:
			c = C[4]
		default:
			c = C[3]
		}
		if o.glint >= 0 && q < 0.25 && math.Abs(u-o.glint) < 0.06 {
			c = pixelfx.White
		}
		out.C = c
		return 0
	})
}

// icicleRime is a column of the rime the icicle hangs from: its height and the stub of an older icicle under it.
type icicleRime struct{ x, h, stub float64 }

func newIcicleFall(o *pixelfx.Options) pixelfx.Instance {
	C, R, seed := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), o.Seed()
	ps, bits, lights := newSim(R, C)
	W := pixelfx.White
	ROOT, LEN, IW, IMPACT := icicleRoot, icicleLen, icicleW, icicleImpact
	splinter := func(x, y, vx float64, n int) {
		for range n {
			bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: x + R.Range(-2, 2), Y: y - 1, VX: vx*0.5 + R.Range(-45, 45), VY: -R.Range(30, 100), G: 0.8, Drag: 0.6, Bounce: 0.35, Life: R.Range(0.4, 0.75), A: R.Range(0, tau), VA: R.Range(-18, 18), S: R.Range(1.2, 2.4), C: C[int(R.Int(4, 7))]})
		}
	}
	pn := &panes{
		ground: floor0,
		onLand: func(q *pane, x, y, v float64) {
			if v < 100 {
				return
			}
			ps.Spawn(pixelfx.Vapour, x, y-1, R.Range(-15, 15), -R.Range(2, 8), R.Range(0.4, 0.6), C[6], R.Range(1.4, 2.2), 0, 0)
			splinter(x, y, q.vx, 2)
		},
		onCrumble: func(q *pane) { splinter(q.x, 0, q.vx, 3) },
	}
	// the rime it hangs from: a frosted crust with stubs of older icicles
	var rime []icicleRime
	for x := -18.0; x <= 18; x++ {
		h, d := pixelfx.Rand(x, seed, 2), math.Abs(x)/18
		stub := 0.0
		if h < 0.3 && math.Abs(x) > 7 {
			stub = 2 + pixelfx.Round(pixelfx.Rand(x, 5)*5*(1-d*0.5))
		}
		rime = append(rime, icicleRime{x: x, h: 1 + pixelfx.Round((0.6+h*1.8)*(1-d*0.7)), stub: stub})
	}
	t, snapped, landed, dripped := 0.0, false, false, false
	top := func() float64 {
		if t < icicleFree {
			return ROOT
		}
		return ROOT + 0.5*pixelfx.Gravity*math.Pow(t-icicleFree, 2)
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// a drop of meltwater swells at the tip and falls
			if !dripped && t >= icicleDrip {
				dripped = true
				bits.Add(pixelfx.Bit{Kind: pixelfx.Drip, X: 0.5, Y: ROOT + LEN + 1, VY: 20, G: 0.9, Life: 1.2, C: C[6]})
			}
			// it trembles: frost sifts down from the root
			if t > icicleTremble && t < icicleSnap && R.Chance(0.25) {
				ps.Spawn(pixelfx.Flake, R.Range(-6, 6), ROOT+R.Range(0, 3), R.Range(-8, 8), R.Range(5, 20), R.Range(0.6, 1.1), pick(R.Chance(0.5), W, C[6]), R.Range(0.5, 1.1), 0, 0)
			}
			// it snaps: a crack flashes across the root, chips and a puff burst off
			if !snapped && t >= icicleSnap {
				snapped = true
				lights.Add(0, ROOT+4, 24, W, 0.6, 0.1)
				lights.Add(0, ROOT+4, 32, C[5], 0.4, 0.3)
				for range 7 {
					a, sp := R.Range(0, tau), R.Range(30, 110)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: R.Range(-4, 4), Y: ROOT + 2, VX: math.Cos(a) * sp, VY: math.Sin(a)*sp - 20, G: 0.8, Drag: 0.8, Bounce: 0.3, Life: R.Range(0.5, 0.9), A: R.Range(0, tau), VA: R.Range(-20, 20), S: R.Range(1, 2), C: C[int(R.Int(5, 7))]})
				}
				for range 3 {
					ps.Spawn(pixelfx.Vapour, R.Range(-5, 5), ROOT+2, R.Range(-25, 25), R.Range(-5, 5), R.Range(0.4, 0.7), C[6], R.Range(1.6, 2.6), 0, 0)
				}
				for range 5 {
					ps.Spawn(pixelfx.Flake, R.Range(-6, 6), ROOT+2, R.Range(-30, 30), R.Range(-20, 20), R.Range(0.6, 1.2), pick(R.Chance(0.5), W, C[6]), R.Range(0.5, 1.3), 0, 0)
				}
			}
			// falling: it sheds frost motes that hang in the air behind it
			if t > icicleFree && t < IMPACT && R.Chance(0.5) {
				tp := top()
				ps.Spawn(pixelfx.Mote, R.Range(-3, 3), tp+R.Range(0, LEN*0.6), R.Range(-10, 10), R.Range(-40, -10), R.Range(0.2, 0.35), pick(R.Chance(0.5), C[6], C[5]), 0.6, 0, 0)
			}
			// it stabs the floor and bursts
			if !landed && t >= IMPACT {
				landed = true
				tp := -LEN
				lights.Add(0, -16, 46, W, 0.85, 0.12)
				lights.Add(0, -16, 58, C[4], 0.5, 0.5)
				// the icicle in pieces: cut across its length, then down the middle
				parts := [][][2]float64{{{-IW / 2, tp}, {IW / 2, tp}, {0.5, 0}}}
				for _, v := range [3]float64{0.3, 0.58, 0.8} {
					parts = splitAll(parts, func(poly [][2]float64) [][][2]float64 {
						return splitPoly(poly, 0, tp+LEN*v+R.Range(-2, 2), R.Range(-0.4, 0.4))
					})
				}
				parts = splitAll(parts, func(poly [][2]float64) [][][2]float64 {
					if centroid(poly).area > 22 {
						return splitPoly(poly, centroid(poly).x, centroid(poly).y, pi/2+R.Range(-0.5, 0.5))
					}
					return [][][2]float64{poly}
				})
				for _, part := range parts {
					m := centroid(part)
					side, low := pick(m.x < 0.5, -1.0, 1), pixelfx.Clamp(-m.y/LEN)
					pts := make([][2]float64, len(part))
					for i, q := range part {
						pts[i] = [2]float64{q[0] - m.x, q[1] - m.y}
					}
					np := pane{pts: pts, x: m.x, y: m.y}
					np.vx = side*R.Range(30, 110) + R.Range(-15, 15)
					np.vy = -R.Range(70, 200) * (0.4 + low)
					np.va = R.Range(-12, 12)
					np.f = R.Range(-0.7, 0.7)
					np.vf = R.Range(-8, 8)
					np.lit, np.g, np.drag, np.bounce, np.solid = 0.75, 0.9, 0.6, 0.35, true
					np.ridge = pixelfx.Val(pi / 2)
					np.brittle = 2
					np.crumble = pixelfx.Val(R.Range(0.15, 0.45))
					np.life = R.Range(1, 1.4)
					pn.add(np)
				}
				for range 52 {
					a, sp := -pi/2+R.Range(-1.25, 1.25), R.Range(60, 230)
					kind := pick(R.Chance(0.15), pixelfx.Chunk, pixelfx.Shard)
					bits.Add(pixelfx.Bit{Kind: kind, X: R.Range(-3, 3), Y: -R.Range(1, 10), VX: math.Cos(a) * sp, VY: math.Sin(a) * sp, G: 0.85, Drag: 0.6, Bounce: 0.32, Life: R.Range(0.6, 1.2), A: R.Range(0, tau), VA: R.Range(-22, 22), S: R.Range(1.2, 3), C: C[int(R.Int(4, 7))], Ramp: []uint32{C[1], C[3], C[4], C[6], C[7]}})
				}
				for range 10 {
					s := R.Sign()
					ps.Spawn(pixelfx.Vapour, s*R.Range(0, 8), -R.Range(1, 6), s*R.Range(25, 90), -R.Range(2, 14), R.Range(0.6, 1.1), C[6], R.Range(2.2, 3.8), 0, 0)
				}
				for range 12 {
					ps.Spawn(pixelfx.Flake, R.Range(-6, 6), -R.Range(2, 16), R.Range(-70, 70), -R.Range(30, 110), R.Range(1, 1.5), pick(R.Chance(0.6), W, C[6]), R.Range(0.5, 1.5), 0, 0)
				}
				for range 6 {
					ps.Spawn(pixelfx.StarKind, R.Range(-24, 24), -R.Range(4, 34), R.Range(-10, 10), -R.Range(4, 14), R.Range(0.4, 0.8), pick(R.Chance(0.5), W, C[6]), R.Range(1, 2.2), 0, 0)
				}
			}
			if landed && t < IMPACT+0.9 && R.Chance(0.08) && len(bits.List) > 0 {
				b := pixelfx.Pick(R, bits.List)
				ps.Spawn(pixelfx.StarKind, b.X, b.Y-1, 0, -3, R.Range(0.2, 0.4), pick(R.Chance(0.5), W, C[6]), R.Range(0.6, 1.4), 0, 0)
			}
			ps.Update(dt)
			bits.Update(dt)
			pn.update(dt)
			lights.Update(dt)
			endBy(t, icicleDuration, ps, bits, pn)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// its shadow gathers on the floor as it comes down
			if t > icicleTremble && t < IMPACT+0.05 {
				k := 0.35 + 0.65*pixelfx.Seg(t, icicleFree, IMPACT)
				if t < icicleFree {
					k = pixelfx.Seg(t, icicleTremble, icicleFree) * 0.35
				}
				rx := 3 + 7*k
				for x := math.Floor(-rx); x <= rx; x++ {
					f := 1 - math.Pow(x/rx, 2)
					p.Blend(x+0.5, 0, C[0], 0.7*k*f)
					if math.Abs(x) < rx*0.7 {
						p.Blend(x+0.5, 1, C[0], 0.45*k*f)
					}
				}
			}
			// frost splashed across the floor where it struck, melting away
			if t >= IMPACT {
				g, out := pixelfx.Ease.OutCubic(pixelfx.Seg(t, IMPACT, IMPACT+0.25)), 1-pixelfx.Smooth(IMPACT+0.8, IMPACT+1.5, t)
				for x := -26.0; x <= 26; x++ {
					d, h := math.Abs(x+0.5)/26, pixelfx.Rand(x, 23)
					a := out * pixelfx.Clamp((g*1.15-d)/0.15)
					if a <= 0 {
						continue
					}
					p.Dot(x, 0, pick(h < 0.35 || d > g-0.12, W, C[6]), a)
					if h < 0.4 {
						for j := 1.0; j <= pixelfx.Round((1+pixelfx.Rand(x, 3)*2.5)*(1-d)); j++ {
							p.Dot(x+pixelfx.Sign(x)*pick(j > 1, 1.0, 0), -j, pick(j > 1, W, C[5]), a)
						}
					}
					for j := 1.0; j <= pixelfx.Round((2+pixelfx.Rand(x, 7)*3)*(1-d*0.6)); j++ {
						p.Dot(x, j, pick(j < 2, C[5], C[4]), a*(1.1-j/6))
					}
				}
				if k := pixelfx.Seg(t, IMPACT, IMPACT+0.35); k < 1 {
					pixelfx.Band(p, 0, 0.5, 6+34*pixelfx.Ease.OutCubic(k), 0.18, math.Max(1, 4*(1-k)), 1.1-k, []uint32{C[3], C[4], C[5], C[6]}, func(a float64) bool { return math.Sin(a) < 0 }, false)
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the rime it hangs from, fading at the end
			ra := 1 - pixelfx.Smooth(icicleDuration-0.7, icicleDuration-0.2, t)
			for _, r := range rime {
				// a lumpy crust: white on top, pale below, a deep underside; little icicles hang off it
				for j := 0.0; j < r.h+1; j++ {
					var c uint32
					switch j {
					case r.h:
						c = W
					case r.h - 1:
						c = C[6]
					case 0:
						c = C[3]
					default:
						c = C[5]
					}
					p.Dot(r.x, ROOT-1-j, c, ra)
				}
				for j := 0.0; j < r.stub; j++ {
					var c uint32
					switch j {
					case r.stub - 1:
						c = W
					case 0:
						c = C[5]
					default:
						c = C[4]
					}
					p.Dot(r.x, ROOT+j, c, ra)
				}
				if r.stub > 4 {
					p.Dot(r.x+1, ROOT, C[3], ra)
				}
			}
			// the snapped root: a jagged stub left behind
			if snapped {
				for x := -IW/2 + 1; x <= IW/2-1; x++ {
					L := 1 + pixelfx.Round(pixelfx.Rand(x, 77)*2.5)
					for j := 0.0; j < L; j++ {
						var c uint32
						switch {
						case j == L-1:
							c = pick(t < icicleSnap+0.15, W, C[6])
						case x < 0:
							c = C[5]
						default:
							c = C[3]
						}
						p.Dot(x, ROOT+j, c, ra)
					}
				}
			}
			p.Glow(0, ROOT, 18, C[4], 0.3*ra)
			if t >= IMPACT {
				if k := pixelfx.Seg(t, IMPACT, IMPACT+0.35); k < 1 {
					pixelfx.Band(p, 0, 0.5, 6+34*pixelfx.Ease.OutCubic(k), 0.18, math.Max(1, 4*(1-k)), 1.1-k, []uint32{C[3], C[4], C[5], C[6]}, func(a float64) bool { return math.Sin(a) >= 0 }, k < 0.15)
				}
				if t < IMPACT+0.22 {
					pixelfx.Sparkle(p, 0.5, -3, 10*pixelfx.Ease.OutBack(math.Min(1, (t-IMPACT)/0.05))*(1-(t-IMPACT)/0.22), C[6], C[4], 0.7)
				}
			}
			if t < IMPACT {
				// the icicle: trembling before it snaps, then falling with a streak of air behind it
				tp := top()
				jx := 0.0
				if t > icicleTremble && t < icicleSnap {
					jx = pick(int(math.Floor(t*36))%2 != 0, 1.0, 0) * pixelfx.Round(pixelfx.Seg(t, icicleTremble, icicleSnap)*1.4+0.4)
				}
				falling := t >= icicleFree
				sp := 0.0
				if falling {
					sp = pixelfx.Gravity * (t - icicleFree)
				}
				if falling {
					for _, s := range [3][2]float64{{-3, 0.7}, {0.5, 1}, {4, 0.6}} {
						dx, k := s[0], s[1]
						if L := math.Min(30, sp*0.07) * k; L > 2 {
							p.LineFn(dx, tp-L, dx, tp+3, func(u float64) (uint32, float64) {
								return pick(u > 0.75, C[6], pick(u > 0.4, C[4], C[3])), 1
							})
						}
					}
				}
				gl := (t - 0.04) / 0.3
				icicleShape(p, C, jx+0.5, tp, LEN, IW, icicleOpts{white: t >= IMPACT-0.025 || (t >= icicleSnap && t < icicleSnap+0.03), glint: pick(gl >= 0 && gl <= 1, gl, -1)})
				if snapped && t < icicleSnap+0.12 {
					p.Line(-IW/2-1, ROOT+1, IW/2+1, ROOT+1, W, 1)
				}
				p.Glow(0.5, tp+LEN*0.5, 20, C[4], 0.28)
				// the drop of meltwater swelling at the tip
				if t < icicleDrip {
					s := pixelfx.Seg(t, 0, icicleDrip)
					p.Dot(0.5, ROOT+LEN+1, C[6], s)
					if s > 0.6 {
						p.Px(0.5, ROOT+LEN+2, W)
					}
				}
			}
			pn.draw(p, C)
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
