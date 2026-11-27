// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/lightning-strike.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The stepped leader, the stroke flicker, the cooling channel and the scorch follow Sprite Fusion's open-source lightning (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// LightningStrike is a targeted strike: a sigil charges on the floor, the scene darkens, a thick bolt slams into it, arcs race out along the cracks it splits, and the channel crumbles to a scorch. It is registered as "lightning/lightning-strike".
var LightningStrike = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Lightning strike",
		Desc:     "A targeted strike: a sigil charges on the floor, the scene darkens, a thick bolt slams into it, arcs race out along the cracks it splits, the channel crumbles to a scorch.",
		Tags:     []string{"lightning", "electric", "strike", "spell", "target", "impact", "ground", "one-shot"},
		Size:     [2]int{240, 264},
		Origin:   [2]int{120, 228},
		Duration: 3,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options:  append(voltOptions(), pixelfx.Option{Name: "seed", Default: pixelfx.Num(5)}),
	},
	New: newLightningStrike,
}

func init() { pixelfx.Register("lightning/lightning-strike", LightningStrike) }

// The frame's half width, the sky's top, the sigil's radius and squash, how far the cracks reach, the leader's steps and where it meets the streamer.
const (
	lsW     = 120
	lsTop   = -224
	lsRX    = 28
	lsReach = 52
	lsSteps = 9
	lsMeet  = -30
)

// The sigil's squash and the leader's start, variables so sums with them round as JavaScript's do.
var lsSY, lsLead0 = 0.22, 0.48

// lsStroke is a return stroke: when it strikes and how bright.
type lsStroke struct{ t, k float64 }

var (
	lsStrokes = [3]lsStroke{{0.76, 1}, {1.04, 0.8}, {1.26, 0.55}}
	lsS1      = lsStrokes[0].t
	lsFlick   = [...]float64{1, 0.72, 0.3, 0.95, 0.5, 0.8, 0.22}
	lsNight   = pixelfx.Hex("#04030a")
	lsDust    = pixelfx.Hex("#6e6382")
	lsAsh     = pixelfx.Hex("#5b5566")
	lsStone   = pixelfx.Ramp("#655b7a", "#4a4263", "#342d45")
)

// lsLevel returns a stroke's flicker a seconds after it strikes.
func lsLevel(a float64) float64 {
	if a < 0 {
		return 0
	}
	if i := int(math.Floor(a*30 + 1e-4)); i < len(lsFlick) {
		return lsFlick[i]
	}
	return 0
}

// lsCracks grows the cracks the strike splits in the floor: Lichtenberg branches lying on the floor's face.
func lsCracks(R *pixelfx.Rng) []branch {
	var out []branch
	var grow func(x, y, a, n, w, d float64, depth int)
	grow = func(x, y, a, n, w, d float64, depth int) {
		pts := []branchPoint{{x, y, d}}
		for s := 0.0; s < n; s += 1.5 {
			a += R.Range(-0.32, 0.32)
			x += math.Cos(a) * 1.5
			y = pixelfx.ClampTo(y+math.Sin(a)*0.45, 0, 5)
			d += 1.5
			pts = append(pts, branchPoint{x, y, d})
			if depth < 2 && s > 3 && R.Chance(0.09) {
				grow(x, y, a+R.Sign()*R.Range(0.5, 1.2), (n-s)*R.Range(0.3, 0.6), w*0.6, d, depth+1)
			}
		}
		out = append(out, branch{pts: pts, w: w})
	}
	for i := range 6 {
		x, y := R.Range(-1, 1), R.Range(0, 1)
		var a float64
		if i%2 != 0 {
			a = R.Range(-0.5, 0.7)
		} else {
			a = pi + R.Range(-0.7, 0.5)
		}
		grow(x, y, a, lsReach*R.Range(0.55, 1), 1, 0, 0)
	}
	return out
}

// lsSigil draws the sigil on the floor: a ring of dashes turning slowly, crosshair ticks, an inner ring and a heart; drawn in from the front, charging brighter; near picks the near part (in front) or the far one (behind).
func lsSigil(p *pixelfx.Painter, t float64, C []uint32, k float64, near bool) {
	RX, SY := float64(lsRX), lsSY
	reveal, spin, burnt := pixelfx.Clamp(t/0.22), t*0.8, -1.0
	if t > lsS1 {
		burnt = t - lsS1
	}
	if burnt > 0.45 {
		return
	}
	a0 := 1.0
	if burnt >= 0.07 {
		a0 = 1 - (burnt-0.07)/0.38
	}
	hot := burnt >= 0 && burnt < 0.07
	on := func(a float64) bool { return (math.Sin(a) >= 0) == near }
	from := func(a float64) float64 { return math.Abs(math.Mod(math.Mod(a-pi/2, tau)+tau+pi, tau)-pi) / pi }
	for i := range 22 {
		a := (float64(i)/22)*tau + spin
		if !on(a) || from(a) > reveal {
			continue
		}
		var c, c2 uint32
		switch {
		case hot:
			c, c2 = pixelfx.White, C[6]
		case near && k > 0.7:
			c = C[6]
		case near:
			c = C[5]
		case k > 0.7:
			c = C[4]
		default:
			c = C[3]
		}
		if !hot {
			c2 = C[2]
		}
		for _, da := range [3]float64{0, 0.06, 0.12} {
			x, y := math.Cos(a+da)*RX, math.Sin(a+da)*RX*SY
			p.Dot(x, y, c, a0)
			if near {
				p.Dot(x, y+1, c2, a0)
			}
		}
		if i%3 == 0 {
			p.Glow(math.Cos(a)*RX, math.Sin(a)*RX*SY, 5, C[4], 0.3*k*a0)
		}
	}
	for j := range 4 {
		a := spin*-0.5 + (float64(j)*tau)/4 + pi/4
		if !on(a) || reveal < 1 {
			continue
		}
		c := C[4]
		if hot {
			c = pixelfx.White
		} else if near {
			c = C[6]
		}
		p.Line(math.Cos(a)*(RX-5), math.Sin(a)*(RX-5)*SY, math.Cos(a)*(RX+5), math.Sin(a)*(RX+5)*SY, c, a0)
	}
	if t > 0.3 {
		c := C[2]
		if hot {
			c = pixelfx.White
		} else if near {
			c = C[4]
		}
		for a := 0.0; a < tau; a += 0.09 {
			if !on(a) {
				continue
			}
			p.Dot(math.Cos(a)*14, math.Sin(a)*14*SY, c, a0*pixelfx.Clamp((t-0.3)/0.12))
		}
	}
	if near && burnt < 0 {
		pulse := 0.6 + 0.4*math.Sin(t*(10+30*k))
		c := C[6]
		if k > 0.5 {
			c = pixelfx.White
		}
		p.Px(0, 0, c)
		for _, d := range [3][2]float64{{1, 0}, {-1, 0}, {0, -1}} {
			p.Px(d[0], d[1], C[5])
		}
		p.Glow(0, -1, 10+10*k, C[5], 0.3+0.4*k*pulse)
	}
}

// lsKeep keeps a bolt's forks on the canvas and above the floor.
func lsKeep(b bolt) bolt {
	var out bolt
	for _, q := range b {
		var pts [][2]float64
		for _, pt := range q.pts {
			if math.Abs(pt[0]) > lsW-5 || pt[1] > -3 || pt[1] < lsTop+6 {
				break
			}
			pts = append(pts, pt)
		}
		if q.w >= 1 {
			out = append(out, path{pts: q.pts, w: q.w})
		} else if len(pts) > 3 {
			out = append(out, path{pts: pts, w: q.w})
		}
	}
	return out
}

// lsNow is the stroke playing: which, its strike time and strength, how long since it struck, and how bright it is now.
type lsNow struct {
	i    int
	s    lsStroke
	a, k float64
}

func newLightningStrike(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	R := pixelfx.NewRng(seed)
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: o.Ramp("sparks")})
	hot := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor})
	lights := &pixelfx.Lights{}
	X0 := R.Range(8, 18)
	if !R.Chance(0.5) {
		X0 *= -1
	}
	main := lsKeep(zigzag(R, X0, lsTop, 0, -1, 0.17, 0.5))
	trunk := main[0].pts
	drift := func(dx float64) func(x, y float64) (float64, float64) {
		return func(x, y float64) (float64, float64) { return x + dx*math.Sin((pi*y)/lsTop), y }
	}
	bolts := [3]bolt{main, bend(bolt{{pts: trunk, w: 1}}, drift(1.5)), bend(bolt{{pts: trunk, w: 1}}, drift(3))}
	chanOf := func(i int) [][2]float64 { return bolts[i][0].pts }
	last := lsStrokes[len(lsStrokes)-1]
	end := chanOf(len(lsStrokes) - 1)
	tree := lsCracks(R)
	// the spent channel breaks up from the top down, each piece at its own moment
	gone := make([]float64, len(end))
	for i, pt := range end {
		gone[i] = last.t + 0.25 + 0.75*(0.55*pixelfx.Rand(seed, float64(i), 5)+0.45*(1-pt[1]/lsTop))
	}
	t, struck := 0.0, -1
	now := func() (lsNow, bool) {
		for i := len(lsStrokes) - 1; i >= 0; i-- {
			if s := lsStrokes[i]; t >= s.t {
				return lsNow{i: i, s: s, a: t - s.t, k: lsLevel(t-s.t) * s.k}, true
			}
		}
		return lsNow{}, false
	}
	charge := func() float64 { return math.Pow(pixelfx.Clamp(t/lsS1), 1.5) }
	heatNow := func() float64 {
		if struck < 0 {
			return 0
		}
		h := math.Inf(-1)
		for _, s := range lsStrokes[:struck+1] {
			h = math.Max(h, s.k*math.Pow(pixelfx.Clamp(1-(t-s.t)/1.6), 1.4))
		}
		return h
	}
	darkFrom, darkTo := lsS1-0.01, lsS1+0.02
	leadSpan, meetSpan := lsS1-lsLead0, lsS1-0.62
	smokeC := pixelfx.Mix(lsAsh, C[2], 0.3)

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i, s := range lsStrokes {
				if struck >= i || t < s.t {
					continue
				}
				struck = i
				k := s.k
				lights.Add(0, -50, 120, C[4], 0.55*k, 0.4)
				lights.Add(0, -3, 24, pixelfx.White, 0.5*k, 0.1)
				for j := 0; float64(j) < pixelfx.Round(44*k); j++ {
					a, sp := -pi/2+R.Range(-1.3, 1.3), R.Range(110, 320)*(0.65+0.35*k)
					ps.Spawn(pixelfx.Spark, R.Range(-1.5, 1.5), -2, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.2, 0.55), pixelfx.White, 1, 0, 0)
				}
				for j := 0; float64(j) < pixelfx.Round(12*k); j++ {
					a, sp := -pi/2+R.Range(-1.2, 1.2), R.Range(70, 220)
					hot.Spawn(pixelfx.Spark, R.Range(-2, 2), -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.5, 1), pixelfx.White, 1, 0, 0)
				}
				for j := 0; float64(j) < pixelfx.Round(5*k); j++ {
					hot.Spawn(pixelfx.Ember, R.Range(-4, 4), -2, R.Range(-25, 25), -R.Range(30, 80), R.Range(0.8, 1.5), pixelfx.White, 1, 0, 0)
				}
				for j := 0; float64(j) < pixelfx.Round(18*k); j++ {
					a, sp := -pi/2+R.Range(-1.1, 1.1), R.Range(50, 190)
					x, life, c := R.Range(-3, 3), R.Range(1, 1.5), pixelfx.Pick(R, lsStone)
					size := 1.0
					if R.Chance(0.35) {
						size = 2
					}
					ps.Spawn(pixelfx.Grit, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, c, size, 0, 0)
				}
				for range 8 {
					x := R.Range(-3, 3)
					vx := R.Sign() * R.Range(30, 110)
					ps.Spawn(pixelfx.Dust, x, -1, vx, -R.Range(4, 24), R.Range(0.4, 0.8), lsDust, R.Range(1.5, 3), 0, 0)
				}
				for j := 0; float64(j) < pixelfx.Round(4*k); j++ {
					ps.Spawn(pixelfx.Smoke, R.Range(-8, 8), -4, R.Range(-10, 10), -R.Range(10, 25), R.Range(1.1, 1.6), smokeC, R.Range(3, 6), R.Range(0.05, 0.25), 0)
				}
			}
			if struck >= 0 && t < 1.7 && R.Chance(6*dt) {
				ps.Spawn(pixelfx.Smoke, R.Range(-8, 8), -2, R.Range(-4, 4), -R.Range(9, 17), R.Range(1, 1.3), 0, R.Range(2, 3.5), 0, 0)
			}
			for i := 0; i < len(gone)-1; i++ {
				if gone[i] <= t && gone[i] > t-dt && pixelfx.Rand(seed, float64(i), 6) < 0.5 {
					x, y := end[i][0], end[i][1]
					vx, vy, life := R.Range(-8, 8), R.Range(6, 22), R.Range(0.4, 0.8)
					c := C[4]
					if R.Chance(0.5) {
						c = C[5]
					}
					ps.Spawn(pixelfx.Mote, x, y, vx, vy, life, c, R.Range(0.5, 0.9), 0, 0)
				}
			}
			ps.Update(dt)
			hot.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the scene darkens a beat before the strike: a soft veil, deepest round the bolt's path, gone at the flash
			dark := 0.5 * pixelfx.Smooth(0.46, 0.66, t) * (1 - pixelfx.Smooth(darkFrom, darkTo, t))
			if dark > 0.01 {
				for y := -228.0; y < 36; y++ {
					for x := -float64(lsW); x < lsW; x++ {
						d := pixelfx.Hypot(x/lsW, (y+96)/132)
						if a := dark * (1 - pixelfx.Smooth(0.55, 1, d)); a > 0.01 {
							p.Blend(x+0.5, y+0.5, lsNight, a)
						}
					}
				}
			}
			lsSigil(p, t, C, charge(), false)
			// the cracks: arcs race out along them white, they glow with heat, cool to soot and fade
			if struck >= 0 {
				s, _ := now()
				front := lsReach * pixelfx.Ease.OutCubic(pixelfx.Clamp((t-lsS1)/0.35))
				T0, fade := heatNow(), 1-pixelfx.Smooth(2.3, 2.95, t)
				elec := 0.0
				if s.a < 0.14 {
					elec = s.s.k * (1 - s.a/0.14)
				}
				for _, br := range tree {
					for _, pt := range br.pts {
						x, y, d := pt.x, pt.y, pt.d
						if d > front {
							break
						}
						q := d / lsReach
						n := pixelfx.Rand(pixelfx.Round(x*2), pixelfx.Round(y*2), 9)
						fresh := d > front-6 && front < lsReach
						w := 1.0
						if br.w < 1 {
							w = 0.8
						}
						T := T0*(1.15-q)*w - n*0.25
						var c uint32
						if fresh || (elec > 0.3 && n < 0.85) {
							c = C[6]
							if q < 0.5 {
								c = pixelfx.White
							}
						} else if c = heat(T); c == 0 {
							c = soot2
						}
						p.Dot(x, y+1, soot, fade)
						p.Dot(x, y, c, fade)
						if br.w >= 1 && d < 9 {
							p.Dot(x, y+2, soot, fade)
						}
						tick := int(pixelfx.Round(d))&3 == 0
						if (fresh || elec > 0.3) && tick {
							p.Glow(x, y, 7, C[5], 0.4)
						} else if T > 0.2 && tick {
							p.Glow(x, y, 5, c, 0.2*T)
						}
					}
				}
				scorch(p, 0, 0, 12, T0, fade)
				if T0 > 0 {
					p.Glow(0, -2, 34, heat(0.55), 0.3*T0)
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
			hot.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			s, playing := now()
			f := int(math.Floor(t*30 + 1e-4))
			k := charge()
			RX, SY := float64(lsRX), lsSY
			lsSigil(p, t, C, k, true)
			// static rising off the charging sigil, more and more of it
			if t > 0.15 && t < lsS1 {
				r := pixelfx.RngOf(seed, 61, float64(f))
				for i := 0; float64(i) < pixelfx.Round(1+4*k); i++ {
					a := r.Range(0, tau)
					x, y := math.Cos(a)*RX, math.Sin(a)*RX*SY
					crackle(p, zigzag(r, x, y, x+r.Range(-2.5, 2.5), y-r.Range(4, 9)*(0.6+k), 0.45, 0.3), 0.45+0.4*k, C, 0.3, 1)
				}
				p.Glow(0, -2, 36, C[4], 0.12+0.3*k)
			}
			// the stepped leader feels down from above; a streamer climbs out of the sigil to meet it
			if t >= lsLead0 && t < lsS1 {
				u := pixelfx.Clamp((t - lsLead0) / leadSpan)
				st := math.Min(lsSteps, math.Floor(u*lsSteps)+1)
				fresh := 1 - math.Mod(u*lsSteps, 1)
				reach := lsTop + (lsMeet-lsTop)*math.Pow(st/lsSteps, 1.2)
				prev := lsTop + (lsMeet-lsTop)*math.Pow((st-1)/lsSteps, 1.2)
				for _, q := range main {
					tk := q.w >= 1
					for i := 0; i < len(q.pts)-1; i++ {
						ax, ay, bx, by := q.pts[i][0], q.pts[i][1], q.pts[i+1][0], q.pts[i+1][1]
						if by > reach {
							if ay <= reach && i > 0 {
								p.Px(ax, ay, pixelfx.White)
								p.Glow(ax, ay, 7, C[5], 0.5*fresh+0.15)
							}
							break
						}
						young := by > prev
						var c uint32
						switch {
						case young && fresh > 0.4 && tk:
							c = C[6]
						case young && fresh > 0.4:
							c = C[5]
						case young:
							c = C[4]
						case tk:
							c = C[3]
						default:
							c = C[2]
						}
						p.Line(ax, ay, bx, by, c, 1)
						if i%4 == 0 && ay > lsTop+14 {
							if young {
								p.Glow(ax, ay, 8, C[4], 0.3*fresh+0.1)
							} else {
								p.Glow(ax, ay, 5, C[4], 0.12)
							}
						}
					}
				}
				if t > 0.62 {
					h := -lsMeet * pixelfx.Ease.OutQuad((t-0.62)/meetSpan)
					r := pixelfx.RngOf(seed, 77, float64(f))
					b := zigzag(r, 0, -1, r.Range(-2, 2), -math.Max(3, h), 0.3, 0.3)
					tip := b[0].pts[len(b[0].pts)-1]
					crackle(p, b, 0.85, C, 0.45, 1)
					p.Px(tip[0], tip[1], pixelfx.White)
					p.Glow(tip[0], tip[1], 9, C[5], 0.5)
				}
			}
			// the strokes, the dart that runs down the channel before each flicker, the channel cooling and crumbling
			next := -1
			for i, q := range lsStrokes {
				if q.t > t {
					next = i
					break
				}
			}
			switch {
			case next > 0 && t > lsStrokes[next].t-0.06:
				tr := chanOf(next)
				u := (t - (lsStrokes[next].t - 0.06)) / 0.06
				i1 := int(math.Floor(u * float64(len(tr))))
				traceAll(p, tr, C[3], 1)
				trace(p, tr, C[6], 1, i1-18, i1)
				for i := max(0, i1-18); i < min(len(tr), i1); i += 4 {
					if tr[i][1] > lsTop+14 {
						p.Glow(tr[i][0], tr[i][1], 9, C[4], 0.35)
					}
				}
			case playing && s.k > 0:
				hero := s.i == 0 && s.a < 1.0/30
				core := 0.0
				if s.k > 0.6 {
					core = 0.8
					if hero {
						core = 1.2
					}
				}
				sc := 1.2
				if s.i == 0 {
					sc = 1.5
				}
				stroke(p, bolts[s.i], s.k, C, sc, strokeOpts{hero: hero, core: core, minY: pixelfx.Val(lsTop + 14)})
				r := 4.5 * s.k
				p.Circle(0, -1, r+1.5, C[2], 1)
				p.Circle(0, -1, r+0.5, C[5], 1)
				p.Circle(0, -1, r*0.65, pixelfx.White, 1)
				if s.a < 0.07 {
					pixelfx.Sparkle(p, 0, -2, pixelfx.Round(12*s.k*(1-s.a/0.07)), C[6], C[4], 0.25)
				}
				p.Glow(0, -16, 96, C[3], 0.42*s.k)
				p.Glow(0, -3, 22, C[5], 0.3*s.k)
				if hero {
					p.Glow(X0*0.5, -110, 170, C[3], 0.3)
				}
			case playing:
				crumble := s.i == len(lsStrokes)-1
				cool := pixelfx.Clamp((s.a - 0.23) / 0.45)
				tr := chanOf(s.i)
				for i := 0; i < len(tr)-1; i++ {
					if crumble && t >= gone[i] {
						continue
					}
					h := 1 - cool
					if crumble {
						h = 1 - pixelfx.Clamp((t-last.t-0.23)/(gone[i]-last.t-0.23))
					}
					var c uint32
					switch {
					case h > 0.7:
						c = C[5]
					case h > 0.4:
						c = C[4]
					case h > 0.15:
						c = C[3]
					default:
						c = C[2]
					}
					p.Line(tr[i][0], tr[i][1], tr[i+1][0], tr[i+1][1], c, 1)
					if i%6 == 0 && h > 0.3 && tr[i][1] > lsTop+14 {
						p.Glow(tr[i][0], tr[i][1], 6, C[4], 0.15*h)
					}
				}
			}
			// arcs crawl out along the cracks as they split, and again with every flicker
			if playing && s.a < 0.4 {
				r := pixelfx.RngOf(seed, 91, float64(f))
				run := lsReach * pixelfx.Clamp(s.a/0.22)
				if s.i == 0 {
					run = lsReach * pixelfx.Ease.OutCubic(pixelfx.Clamp(s.a/0.35))
				}
				for _, br := range tree {
					if br.w < 1 || r.Next() < 0.3 {
						continue
					}
					j1 := slices.IndexFunc(br.pts, func(q branchPoint) bool { return q.d > run })
					if j1 < 0 {
						j1 = len(br.pts)
					}
					j0 := max(0, j1-int(r.Int(7, 13)))
					pts := make([][2]float64, 0, j1-j0)
					for _, q := range br.pts[j0:j1] {
						x := q.x + r.Range(-0.6, 0.6)
						pts = append(pts, [2]float64{x, q.y - 1.5 - r.Range(0, 2.5)})
					}
					if len(pts) > 1 {
						crackle(p, bolt{{pts: pts, w: 1}}, 0.9*(1-s.a/0.4)+0.1, C, 0.35, 1)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerFront)
			hot.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
