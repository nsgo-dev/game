// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/vine-whip.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the smear that trails the lash (white at its leading edge, cooling down the ramp as it thins), the glint and crack burst at the tip, from Sprite Fusion's open-source slash (https://www.spritefusion.com/pixel-art-vfx/slash), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// VineWhip is a thorny vine that bursts from the floor, rears back coiled, lashes out right with a whip crack that sprays sap and tears off leaves, then slides back into the ground. It is registered as "nature/vine-whip".
var VineWhip = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Vine whip",
		Desc:     "A thorny vine bursts from the floor, rears back coiled, then lashes out right in a whip crack that sprays sap and tears off leaves, before sliding back into the ground.",
		Tags:     []string{"nature", "vine", "plant", "whip", "melee", "directional", "one-shot"},
		Size:     [2]int{216, 128},
		Origin:   [2]int{60, 114},
		Duration: vineWhipDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0c1a10", "#173a1a", "#24582a", "#357a34", "#52a03e", "#86c84e", "#c4ec7a", "#f4ffd0")},
			{Name: "thorn", Default: pixelfx.Palette("#2a1410", "#5e2a1a", "#a04c2e", "#e89a6a")},
			{Name: "soil", Default: pixelfx.Palette("#120c0c", "#251917", "#3b2a24", "#553d31", "#715440", "#8f6e52", "#b8966e", "#e6d0a8")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newVineWhip,
}

func init() { pixelfx.Register("nature/vine-whip", VineWhip) }

const (
	vineWhipDuration = 1.4
	vineWhipN        = 24
)

var (
	vineWhipLS    = 3.3
	vineWhipLash  = 0.36
	vineWhipLag   = 0.0052
	vineWhipTauS  = 0.055
	vineWhipOmega = tau / 0.21
	vineWhipCrack = vineWhipLash + vineWhipLag*vineWhipN + (pi-math.Atan(vineWhipOmega*vineWhipTauS))/vineWhipOmega
)

// vineSpring is a damped spring's step: 0 before u = 0, swings past 1, settles.
func vineSpring(u float64) float64 {
	if u <= 0 {
		return 0
	}
	return 1 - math.Exp(-u/vineWhipTauS)*(math.Cos(vineWhipOmega*u)+math.Sin(vineWhipOmega*u)/(vineWhipOmega*vineWhipTauS))
}

// vinePose returns the vine's points at time t.
func vinePose(t float64) [vineWhipN + 1][2]float64 {
	grow := pixelfx.Ease.OutCubic(pixelfx.Clamp(t/0.14)) * (1 - pixelfx.Ease.InCubic(pixelfx.Clamp((t-1.0)/0.3)))
	rear := pixelfx.Smooth(0.1, vineWhipLash, t)
	relax := pixelfx.Smooth(0.82, 1.0, t)
	var pts [vineWhipN + 1][2]float64
	pts[0] = [2]float64{0, (1 - grow) * (vineWhipN*vineWhipLS + 6)}
	for i := 0; i < vineWhipN; i++ {
		s := float64(i) / vineWhipN
		stand := -pi/2 + 0.25*math.Sin(s*3+t*7)*s
		back := -pi/2 - 0.3 - 1.65*math.Pow(s, 1.3)
		out := -1.0 + 0.9*math.Pow(s, 0.6)
		a := pixelfx.Lerp(stand, back, rear)
		a = pixelfx.Lerp(a, out, vineSpring(t-vineWhipLash-vineWhipLag*float64(i)))
		a = pixelfx.Lerp(a, stand, relax)
		x, y := pts[i][0], pts[i][1]
		pts[i+1] = [2]float64{x + math.Cos(a)*vineWhipLS, y + math.Sin(a)*vineWhipLS}
	}
	return pts
}

// drawVineHaze draws a translucent polygon: the soft body of the smear under its streaks.
func drawVineHaze(p *pixelfx.Painter, pts [4][2]float64, c uint32, a float64) {
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, pt := range pts {
		x0 = math.Min(x0, pt[0])
		x1 = math.Max(x1, pt[0])
		y0 = math.Min(y0, pt[1])
		y1 = math.Max(y1, pt[1])
	}
	for y := math.Floor(y0); y <= math.Ceil(y1); y++ {
		for x := math.Floor(x0); x <= math.Ceil(x1); x++ {
			inside := false
			for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
				xi, yi := pts[i][0], pts[i][1]
				xj, yj := pts[j][0], pts[j][1]
				if (yi > y+0.5) != (yj > y+0.5) && x+0.5 < ((xj-xi)*(y+0.5-yi))/(yj-yi)+xi {
					inside = !inside
				}
			}
			if inside {
				p.Blend(x, y, c, a)
			}
		}
	}
}

func vineWidth(s float64) float64 { return 1.2 + 4.8*math.Pow(1-s, 0.7) }

// drawVine draws the vine itself: a thick stroke lit on its upper side with a bright edge, dark beneath, bark notches.
func drawVine(p *pixelfx.Painter, pts *[vineWhipN + 1][2]float64, C []uint32) {
	for i := 0; i < vineWhipN; i++ {
		ax, ay := pts[i][0], pts[i][1]
		bx, by := pts[i+1][0], pts[i+1][1]
		dx, dy := bx-ax, by-ay
		L := pixelfx.Hypot(dx, dy)
		if L == 0 {
			L = 1
		}
		nx, ny := -dy/L, dx/L
		w0 := vineWidth(float64(i)/vineWhipN) / 2
		w1 := vineWidth(float64(i+1)/vineWhipN) / 2
		up := -1.0
		if nx*-0.6+ny*-0.8 > 0 {
			up = 1
		}
		X0, X1 := math.Floor(math.Min(ax, bx)-4), math.Ceil(math.Max(ax, bx)+4)
		Y0, Y1 := math.Floor(math.Min(ay, by)-4), math.Ceil(math.Max(ay, by)+4)
		for y := Y0; y <= Y1; y++ {
			for x := X0; x <= X1; x++ {
				if y > 0 {
					continue
				}
				px, py := x+0.5-ax, y+0.5-ay
				u := (px*dx + py*dy) / (L * L)
				if u < -0.15 || u > 1.15 {
					continue
				}
				d := px*nx + py*ny
				w := pixelfx.Lerp(w0, w1, pixelfx.Clamp(u)) + 0.35
				if math.Abs(d) > w {
					continue
				}
				side := pixelfx.Sign(d) * up
				q := math.Abs(d) / w
				arc := (float64(i) + u) * vineWhipLS
				var c uint32
				switch {
				case q > 0.8:
					if side > 0 {
						c = C[5]
					} else {
						c = C[1]
					}
				case side > 0:
					if q > 0.35 {
						c = C[4]
					} else {
						c = C[3]
					}
				default:
					if q > 0.4 {
						c = C[2]
					} else {
						c = C[3]
					}
				}
				if w > 1.6 && math.Abs(math.Mod(arc+d*0.8, 5)-2.5) < 0.45 && q < 0.8 {
					c = C[2]
				}
				p.Px(x, y, c)
			}
		}
	}
}

// drawVineThorns draws thorns along the vine, alternating sides, hooked back toward the root, plus leaves on short stalks.
func drawVineThorns(p *pixelfx.Painter, pts *[vineWhipN + 1][2]float64, T, C []uint32, t float64) {
	for i := 2; i < vineWhipN-1; i += 2 {
		ax, ay := pts[i][0], pts[i][1]
		bx, by := pts[i+1][0], pts[i+1][1]
		dx, dy := bx-ax, by-ay
		L := pixelfx.Hypot(dx, dy)
		if L == 0 {
			L = 1
		}
		side := -1.0
		if (i/2)%2 != 0 {
			side = 1
		}
		nx, ny := (-dy/L)*side, (dx/L)*side
		w := vineWidth(float64(i)/vineWhipN) / 2
		length := 1.8 + 2.4*(1-float64(i)/vineWhipN)
		tx0, ty0 := dx/L, dy/L
		x0, y0 := ax+nx*(w-0.5), ay+ny*(w-0.5)
		tx, ty := x0+nx*length-tx0*length*0.7, y0+ny*length-ty0*length*0.7
		if y0 > 0 {
			continue
		}
		// a hooked thorn: a broad foot on the stem, a sharp lit point raking back toward the root
		p.Tri(x0-tx0*1.6, y0-ty0*1.6, x0+tx0*1.4, y0+ty0*1.4, tx, ty, T[1], 1)
		p.Line(x0+tx0*0.8, y0+ty0*0.8, tx, ty, T[2], 1)
		p.Px(tx, ty, T[3])
	}
	for _, k := range [3]int{7, 13, 18} {
		ax, ay := pts[k][0], pts[k][1]
		bx, by := pts[k+1][0], pts[k+1][1]
		a := math.Atan2(by-ay, bx-ax)
		side := -1.0
		if k%2 != 0 {
			side = 1
		}
		if ay > -2 {
			continue
		}
		la := a + side*1.1 + math.Sin(t*9+float64(k))*0.2
		lx, ly := ax+math.Cos(la)*4, ay+math.Sin(la)*4
		drawLeaf(p, lx, ly, 7, 2.2, la, 0.3+math.Sin(t*5+float64(k))*0.4, [4]uint32{C[1], C[3], C[5], C[6]}, 1)
	}
}

type vineSpot struct {
	x, t0, w float64
}

func newVineWhip(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	T := pixelfx.Fit(o.Ramp("thorn"), 4)
	S := pixelfx.Fit(o.Ramp("soil"), 8)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[7], C[6], C[5], C[4]}})
	ds := newDrops(floor)
	lights := &pixelfx.Lights{}
	var chips []barkChip
	var torn []bloomFlying
	var spots []vineSpot
	t := 0.0
	burst, cracked, sunk := false, false, false
	dirt := func(n int, sp float64) {
		for range n {
			a := -pi/2 + R.Range(-1.2, 1.2)
			v := R.Range(60, 200) * sp
			cx := R.Range(-3, 3)
			ang := R.Range(0, tau)
			va := R.Range(-15, 15)
			sz := R.Range(1.4, 2.6)
			pts := stoneShape(R, 0)
			life := math.Min(R.Range(0.6, 0.9), vineWhipDuration-0.05-t)
			chips = append(chips, barkChip{x: cx, y: -1, vx: math.Cos(a) * v, vy: math.Sin(a) * v, a: ang, va: va, s: sz, pts: pts, life: life})
		}
		for range n * 3 {
			a := -pi/2 + R.Range(-1, 1)
			v := R.Range(50, 200) * sp
			px := R.Range(-3, 3)
			life := math.Min(R.Range(0.5, 0.9), vineWhipDuration-0.05-t)
			col := pixelfx.Pick(R, []uint32{S[5], S[4], S[3]})
			sz := 1.0
			if R.Chance(0.3) {
				sz = 2
			}
			ps.Spawn(pixelfx.Grit, px, -1, math.Cos(a)*v, math.Sin(a)*v, life, col, sz, 0, 0)
		}
		for range n {
			px := R.Range(-4, 4)
			py := -R.Range(0, 3)
			vx := R.Sign() * R.Range(15, 60)
			vy := -R.Range(4, 20)
			life := math.Min(R.Range(0.4, 0.8), vineWhipDuration-0.05-t)
			sz := R.Range(2, 4)
			ps.Spawn(pixelfx.Dust, px, py, vx, vy, life, S[5], sz, 0, 0)
		}
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !burst {
				burst = true
				dirt(6, 1)
			}
			if !cracked && t >= vineWhipCrack {
				cracked = true
				a, b := vinePose(t), vinePose(t-0.01)
				x, y := a[vineWhipN][0], a[vineWhipN][1]
				vx, vy := (x-b[vineWhipN][0])/0.01, (y-b[vineWhipN][1])/0.01
				sp := pixelfx.Hypot(vx, vy)
				if sp == 0 {
					sp = 1
				}
				lights.Add(x, y, 22, pixelfx.White, 0.5, 0.08)
				lights.Add(x, y, 50, C[5], 0.35, 0.4)
				for range 18 {
					ang := math.Atan2(vy, vx) + R.Range(-0.7, 0.7)
					v := R.Range(80, 260)
					dvx := math.Cos(ang) * v
					dvy := math.Sin(ang)*v - R.Range(0, 60)
					sz := 1.0
					if R.Chance(0.3) {
						sz = 2
					}
					ds.add(x, y, dvx, dvy, sz, 0)
				}
				for range 4 {
					lvx := (vx/sp)*R.Range(40, 120) + R.Range(-30, 30)
					lvy := R.Range(-90, -20)
					la := R.Range(0, tau)
					lva := R.Range(-12, 12)
					lfl := R.Range(0, tau)
					lvf := R.Range(8, 16)
					life := R.Range(0.65, 0.8)
					torn = append(torn, bloomFlying{x: x, y: y, vx: lvx, vy: lvy, a: la, va: lva, fl: lfl, vf: lvf, life: life, max: life})
				}
				for range 8 {
					px := x + R.Range(-6, 6)
					py := y + R.Range(-6, 6)
					svx := R.Range(-20, 20)
					svy := R.Range(-20, 20)
					life := R.Range(0.15, 0.35)
					sz := R.Range(0.6, 1.6)
					ps.Spawn(pixelfx.StarKind, px, py, svx, svy, life, C[7], sz, 0, 0)
				}
			}
			if !sunk && t >= 1.08 {
				sunk = true
				dirt(3, 0.5)
			}
			ds.update(dt, func(d drop, _ float64) {
				spots = append(spots, vineSpot{x: d.x, t0: t, w: d.s*0.7 + R.Range(0.2, 1)})
			})
			for i := len(chips) - 1; i >= 0; i-- {
				c := &chips[i]
				c.life -= dt
				if c.life <= 0 {
					chips = append(chips[:i], chips[i+1:]...)
					continue
				}
				c.vy += 950 * dt
				c.x += c.vx * dt
				c.y += c.vy * dt
				c.a += c.va * dt
				if c.y > 0 {
					c.y = 0
					c.vy *= -0.3
					c.vx *= 0.6
				}
			}
			for i := len(torn) - 1; i >= 0; i-- {
				l := &torn[i]
				l.life -= dt
				if l.life <= 0 {
					torn = append(torn[:i], torn[i+1:]...)
					continue
				}
				l.vy += 160 * dt
				l.vx -= l.vx * 2.5 * dt
				l.vy -= l.vy * 2 * dt
				l.x += l.vx*dt + math.Sin(l.fl)*20*dt
				l.y += l.vy * dt
				l.a += l.va * dt
				l.fl += l.vf * dt
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// a hole torn in the floor where it comes up, sap spots where it sprays
			k := 1 - pixelfx.Smooth(1.15, 1.35, t)
			for x := -5.0; x <= 5; x++ {
				col := S[1]
				if math.Abs(x) < 3 {
					col = S[0]
				}
				p.Dot(x, 0, col, k*(1-math.Abs(x)/7))
			}
			for _, s := range spots {
				a := (1 - pixelfx.Smooth(0.25, 0.6, t-s.t0)) * (1 - pixelfx.Smooth(1.2, 1.35, t))
				col := C[4]
				if t-s.t0 < 0.1 {
					col = C[6]
				}
				for x := math.Floor(s.x - s.w); x <= math.Ceil(s.x+s.w); x++ {
					p.Dot(x, 0, col, a)
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			pts := vinePose(t)
			// the smear: a fan of streaks, each the path one point of the outer vine just swept
			if t > vineWhipLash+0.04 && t < vineWhipCrack+0.1 {
				age := pixelfx.Clamp((t - vineWhipCrack + 0.02) / 0.12)
				var hist [12][vineWhipN + 1][2]float64
				for k := 0; k < 12; k++ {
					hist[k] = vinePose(t - float64(k)*0.0045)
				}
				for k := 0; k < 6; k++ {
					col := C[5]
					if k < 2 {
						col = C[6]
					}
					drawVineHaze(p, [4][2]float64{hist[k][vineWhipN], hist[k+1][vineWhipN], hist[k+1][vineWhipN-12], hist[k][vineWhipN-12]}, col, 0.4*(1-float64(k)/6)*(1-age))
				}
				for i := vineWhipN - 12; i <= vineWhipN; i += 2 {
					q := float64(vineWhipN-i) / 12
					var col uint32
					switch {
					case q < 0.1:
						col = pixelfx.White
					case q < 0.3:
						col = C[7]
					case q < 0.55:
						col = C[6]
					case q < 0.8:
						col = C[5]
					default:
						col = C[4]
					}
					length := int(pixelfx.Round(11 * (1 - q*0.6)))
					for k := 0; k < length; k++ {
						c := col
						if k < 2 && q < 0.1 {
							c = pixelfx.White
						}
						fade := 1.0
						if float64(k) >= float64(length)*0.5 {
							fade = 2 * (1 - float64(k)/float64(length))
						}
						p.Line(hist[k][i][0], hist[k][i][1], hist[k+1][i][0], hist[k+1][i][1], c, (1-age)*fade)
					}
				}
				p.GlowLine(hist[0][vineWhipN][0], hist[0][vineWhipN][1], hist[6][vineWhipN][0], hist[6][vineWhipN][1], 14, C[5], 0.35*(1-age))
			}
			drawVine(p, &pts, C)
			drawVineThorns(p, &pts, T, C, t)
			lk := pixelfx.Smooth(vineWhipLash, vineWhipCrack, t) * (1 - pixelfx.Smooth(vineWhipCrack, vineWhipCrack+0.35, t))
			if lk > 0 {
				for i := 6; i <= vineWhipN; i += 6 {
					p.Glow(pts[i][0], pts[i][1], 12, C[5], 0.18*lk)
				}
			}
			// the crack: a white burst of air lines and a glint at the tip
			e := t - vineWhipCrack
			if e >= 0 && e < 0.12 {
				x, y := pts[vineWhipN][0], pts[vineWhipN][1]
				k := 1 - e/0.12
				col := C[6]
				if k > 0.5 {
					col = pixelfx.White
				}
				for j := 0; j < 7; j++ {
					a := (float64(j)/7)*tau + 0.3
					r0 := 4 + 10*(1-k)
					r1 := r0 + 5*k
					p.Line(x+math.Cos(a)*r0, y+math.Sin(a)*r0, x+math.Cos(a)*r1, y+math.Sin(a)*r1, col, 1)
				}
				p.Ring(x, y, 3+8*(1-k), 1, C[7], k)
				pixelfx.Sparkle(p, x, y, 7*k, C[7], C[5], 0.4)
			}
			for _, c := range chips {
				drawRock(p, c.x, c.y, c.s, c.a, c.pts, S, math.Min(1, c.life/0.25), false)
			}
			for _, l := range torn {
				drawLeaf(p, l.x, l.y, 6, 2, l.a, l.fl, [4]uint32{C[1], C[3], C[5], C[6]}, math.Min(1, l.life/0.3))
			}
			ds.draw(p, C)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
