// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/shatter-wipe.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the impact flash, cracks racing out from the impact, white flash before the burst, and glass splinters, glints and cold light of the burst from Sprite Fusion's open-source ice shatter (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package transitions

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// ShatterWipe is a glass-shatter screen transition: a dark glass pane drops and bounces, cracks race out from an impact flash, then it bursts into tumbling glass shards that fall away. It is registered as "transitions/shatter-wipe".
var ShatterWipe = &pixelfx.Effect{
	Meta: screenMeta(
		"Shatter wipe",
		"A dark glass pane drops over the screen and bounces; cracks race out from an impact with a flash, then it bursts into tumbling glass shards with lit edges that fall away.",
		[]string{"shatter", "glass", "break", "shards", "wipe"},
		2.8,
		pixelfx.Option{Name: "palette", Default: pixelfx.Palette("#0c1430", "#162c5c", "#1f4f94", "#2f7fcc", "#58b4ec", "#9bdcff", "#d8f4ff", "#ffffff")},
		pixelfx.Option{Name: "seed", Default: pixelfx.Num(2)},
	),
	New: newShatterWipe,
}

func init() { pixelfx.Register("transitions/shatter-wipe", ShatterWipe) }

type shatterBounce struct {
	t0, v, T float64
}

var (
	shatterD       = 2.8
	shatterLand    = 0.55
	shatterGP      = (2 * float64(H)) / (shatterLand * shatterLand)
	shatterRest    = 0.3
	shatterCrack   = 1.32
	shatterBreak   = 1.62
	shatterWave    = 520.0
	shatterLX      = -0.6
	shatterLY      = -0.8
	shatterL3      = [3]float64{-0.42, -0.56, 0.71}
	shatterBounces = func() [3]shatterBounce {
		var out [3]shatterBounce
		t0 := shatterLand
		v := shatterGP * shatterLand * shatterRest
		for k := range 3 {
			T := (2 * v) / shatterGP
			out[k] = shatterBounce{t0: t0, v: v, T: T}
			t0 += T
			v *= shatterRest
		}
		return out
	}()
)

func shatterPaneY(t float64) float64 {
	if t < shatterLand {
		return -H + 0.5*shatterGP*t*t
	}
	for _, b := range shatterBounces {
		if t < b.t0+b.T {
			dt := t - b.t0
			return -(b.v*dt - 0.5*shatterGP*dt*dt)
		}
	}
	return 0
}

type shatterCrackPt struct {
	x, y int
	t0   float64
}

type shatterCell struct {
	pts, lp [][2]float64
	cx, cy  float64
	d, r    float64
	tb      float64
	broke   bool
	gone    bool
	vx, vy  float64
	a, va   float64
	phi     float64
	vphi    float64
	psi     float64
	z, vz   float64
	g       float64
	edgeOn  bool
}

type shatterPattern struct {
	Ix, Iy float64
	cracks []shatterCrackPt
	cells  []shatterCell
}

func buildShatterPattern(R *pixelfx.Rng) shatterPattern {
	Ix := W * R.Range(0.4, 0.6)
	Iy := H * R.Range(0.36, 0.54)
	const nr = 13
	rings := [9]float64{0, 9, 21, 37, 58, 86, 122, 168, 232}
	var ang, speed [nr]float64
	for i := range nr {
		ang[i] = ((float64(i) + R.Range(-0.3, 0.3)) / nr) * tau
	}
	for i := range nr {
		speed[i] = R.Range(800, 1300)
	}
	var V [nr][9][2]float64
	for i := range nr {
		a := ang[i]
		for j, r := range rings {
			if j == 0 {
				V[i][j] = [2]float64{Ix, Iy}
				continue
			}
			rr := r * R.Range(0.84, 1.16)
			aa := a + R.Range(-0.07, 0.07)
			V[i][j] = [2]float64{Ix + math.Cos(aa)*rr, Iy + math.Sin(aa)*rr}
		}
	}
	jag := func(A, Bp [2]float64) [][2]float64 {
		dx, dy := Bp[0]-A[0], Bp[1]-A[1]
		L := pixelfx.Hypot(dx, dy)
		if L == 0 {
			L = 1
		}
		n := 1
		if L > 18 {
			n = 2
		}
		out := make([][2]float64, 0, n+2)
		out = append(out, A)
		for k := 1; k <= n; k++ {
			f := float64(k)/float64(n+1) + R.Range(-0.08, 0.08)
			off := R.Range(-1, 1) * math.Min(3, L*0.07)
			out = append(out, [2]float64{A[0] + dx*f - (dy/L)*off, A[1] + dy*f + (dx/L)*off})
		}
		out = append(out, Bp)
		return out
	}
	var rad [nr][8][][2]float64
	for i := range nr {
		for j := range 8 {
			rad[i][j] = jag(V[i][j], V[i][j+1])
		}
	}
	var ring [nr][9][][2]float64
	for i := range nr {
		for j := 1; j < 9; j++ {
			ring[i][j] = jag(V[i][j], V[(i+1)%nr][j])
		}
	}
	var keep [nr][9]bool
	for i := range nr {
		for j := range 9 {
			keep[i][j] = j <= 2 || j == 8 || R.Chance(0.78-0.07*float64(j))
		}
	}
	var cracks []shatterCrackPt
	reach := func(i int, x, y float64) float64 {
		return shatterCrack + pixelfx.Hypot(x-Ix, y-Iy)/speed[i]
	}
	trace := func(pts [][2]float64, when func(x, y float64) float64) {
		for k := 1; k < len(pts); k++ {
			ax, ay := pts[k-1][0], pts[k-1][1]
			bx, by := pts[k][0], pts[k][1]
			n := int(math.Ceil(pixelfx.Hypot(bx-ax, by-ay) * 2))
			for s := range n {
				x := ax + ((bx-ax)*float64(s))/float64(n)
				y := ay + ((by-ay)*float64(s))/float64(n)
				if x >= 0 && y >= 0 && x < W && y < H {
					cracks = append(cracks, shatterCrackPt{x: int(math.Floor(x)), y: int(math.Floor(y)), t0: when(x, y)})
				}
			}
		}
	}
	for i := range nr {
		for j := range 8 {
			trace(rad[i][j], func(x, y float64) float64 { return reach(i, x, y) })
		}
	}
	for i := range nr {
		for j := 1; j < 8; j++ {
			if !keep[i][j] {
				continue
			}
			i2 := (i + 1) % nr
			e := ring[i][j]
			start := math.Max(reach(i, V[i][j][0], V[i][j][1]), reach(i2, V[i2][j][0], V[i2][j][1])) + R.Range(0.02, 0.07)
			ax, ay := e[0][0], e[0][1]
			trace(e, func(x, y float64) float64 { return start + pixelfx.Hypot(x-ax, y-ay)/420 })
		}
	}
	slices.SortStableFunc(cracks, func(a, b shatterCrackPt) int { return cmp.Compare(a.t0, b.t0) })

	var cells []shatterCell
	for i := range nr {
		for j := 0; j < 8; {
			jb := j
			for !keep[i][jb+1] {
				jb++
			}
			i2 := (i + 1) % nr
			side := func(r int) [][2]float64 {
				var out [][2]float64
				for k := j; k <= jb; k++ {
					e := rad[r][k]
					if k > j {
						out = append(out, e[1:]...)
					} else {
						out = append(out, e...)
					}
				}
				return out
			}
			s1 := side(i)
			s2 := side(i2)
			slices.Reverse(s2)
			pts := make([][2]float64, 0, len(s1)+len(ring[i][jb+1])+len(s2))
			pts = append(pts, s1...)
			pts = append(pts, ring[i][jb+1][1:]...)
			pts = append(pts, s2[1:]...)
			if j != 0 {
				rj := slices.Clone(ring[i][j])
				slices.Reverse(rj)
				if len(rj) > 2 {
					pts = append(pts, rj[1:len(rj)-1]...)
				}
			} else if len(pts) > 0 {
				pts = pts[:len(pts)-1]
			}
			x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for _, q := range pts {
				if q[0] < x0 {
					x0 = q[0]
				}
				if q[0] > x1 {
					x1 = q[0]
				}
				if q[1] < y0 {
					y0 = q[1]
				}
				if q[1] > y1 {
					y1 = q[1]
				}
			}
			if !(x1 < 0 || y1 < 0 || x0 >= W || y0 >= H) {
				cx := (pixelfx.ClampTo(x0, 0, W) + pixelfx.ClampTo(x1, 0, W)) / 2
				cy := (pixelfx.ClampTo(y0, 0, H) + pixelfx.ClampTo(y1, 0, H)) / 2
				d := pixelfx.Hypot(cx-Ix, cy-Iy)
				lp := make([][2]float64, len(pts))
				for k, q := range pts {
					lp[k] = [2]float64{q[0] - cx, q[1] - cy}
				}
				cells = append(cells, shatterCell{
					pts: pts,
					lp:  lp,
					cx:  cx,
					cy:  cy,
					d:   d,
					r:   pixelfx.Hypot(x1-x0, y1-y0) / 2,
					tb:  shatterBreak + d/shatterWave + R.Range(0, 0.04),
				})
			}
			j = jb + 1
		}
	}
	return shatterPattern{Ix: Ix, Iy: Iy, cracks: cracks, cells: cells}
}

func newShatterWipe(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	ink := o.Color("color")
	R := pixelfx.NewRng(o.Seed())
	pat := buildShatterPattern(R)
	Ix, Iy := pat.Ix, pat.Iy
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[7], C[6], C[5], C[4]}, Haze: C[5]})
	bits := pixelfx.NewBits(nil)
	lights := &pixelfx.Lights{}
	t := 0.0
	bounce := -1
	struck, burst := false, false
	end := shatterD - 0.05
	shardCols := []uint32{C[7], C[6], C[5]}

	splinters := func(x, y float64, n int, vx, vy, spread, sp0, sp1 float64) {
		for range n {
			a := math.Atan2(vy, vx) + R.Range(-spread, spread)
			sp := R.Range(sp0, sp1)
			life := math.Min(R.Range(0.6, 1.2), end-t)
			if life > 0.2 {
				ang := R.Range(0, tau)
				va := R.Range(-20, 20)
				sz := R.Range(1.5, 3.5)
				col := pixelfx.Pick(R, shardCols)
				bits.Add(pixelfx.Bit{
					Kind: pixelfx.Shard,
					X:    x,
					Y:    y,
					VX:   math.Cos(a) * sp,
					VY:   math.Sin(a) * sp,
					G:    0.8,
					Drag: 0.6,
					Life: life,
					A:    ang,
					VA:   va,
					S:    sz,
					C:    col,
				})
			}
		}
	}
	addGlint := func(x, y, s float64) {
		vx := R.Range(-8, 8)
		vy := R.Range(-10, 4)
		life := math.Min(R.Range(0.3, 0.6), end-t)
		col := C[6]
		if R.Chance(0.5) {
			col = pixelfx.White
		}
		ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, s, 0, 0)
	}
	var fall []*shatterCell

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for bounce+1 < len(shatterBounces) && t >= shatterBounces[bounce+1].t0 {
				bounce++
				k := shatterBounces[bounce].v / shatterBounces[0].v
				if k < 0.05 {
					continue
				}
				for i := range 5 {
					lights.Add(W*(float64(i)+0.5)/5, H-2, 70, C[5], 0.5*k, 0.35)
				}
				nSplint := int(pixelfx.Round(26 * k))
				for range nSplint {
					sx := R.Range(4, W-4)
					svx := R.Range(-0.4, 0.4)
					splinters(sx, H-1, 1, svx, -1, 0.5, 120*k, 330*k)
				}
				nGlint := int(pixelfx.Round(9 * k))
				for range nGlint {
					gx := R.Range(8, W-8)
					gy := H - R.Range(2, 10)
					addGlint(gx, gy, R.Range(1, 2.4))
				}
			}
			if !struck && t >= shatterCrack {
				struck = true
				lights.Add(Ix, Iy, 90, C[5], 0.8, 0.35)
				lights.Add(Ix, Iy, 34, pixelfx.White, 0.9, 0.1)
				splinters(Ix, Iy, 16, 0, -1, pi, 60, 240)
				for range 5 {
					vx := R.Range(-40, 40)
					vy := R.Range(-40, 30)
					life := R.Range(0.35, 0.6)
					sz := R.Range(2, 3.5)
					ps.Spawn(pixelfx.Vapour, Ix, Iy, vx, vy, life, C[6], sz, 0, 0)
				}
			}
			if t > shatterCrack && t < shatterBreak && R.Chance(0.12) {
				c := pat.cracks[int(R.Int(0, float64(len(pat.cracks)-1)))]
				if c.t0 < t {
					addGlint(float64(c.x), float64(c.y), R.Range(0.6, 1.6))
				}
			}
			if !burst && t >= shatterBreak {
				burst = true
				lights.Add(Ix, Iy, 170, C[4], 0.55, 0.5)
				lights.Add(Ix, Iy, 60, pixelfx.White, 0.8, 0.14)
				splinters(Ix, Iy, 46, 0, -1, pi, 90, 340)
				for range 14 {
					a := R.Range(0, tau)
					sp := R.Range(30, 110)
					life := R.Range(0.45, 0.8)
					col := C[5]
					if R.Chance(0.6) {
						col = C[6]
					}
					sz := R.Range(3, 6)
					ps.Spawn(pixelfx.Vapour, Ix+math.Cos(a)*4, Iy+math.Sin(a)*4, math.Cos(a)*sp, math.Sin(a)*sp, life, col, sz, 0, 0)
				}
			}
			for ci := range pat.cells {
				s := &pat.cells[ci]
				if !s.broke && t >= s.tb {
					s.broke = true
					dx, dy := s.cx-Ix, s.cy-Iy
					d := pixelfx.Hypot(dx, dy)
					if d == 0 {
						d = 1
					}
					sp := R.Range(40, 130) * (1.15 - math.Min(1, s.d/260))
					s.vx = (dx/d)*sp + R.Range(-15, 15)
					s.vy = (dy/d)*sp*0.6 - R.Range(20, 90)
					s.a = 0
					s.va = R.Range(-2.5, 2.5) * (1 + 20/(s.r+10))
					s.phi = 0
					s.vphi = R.Sign() * R.Range(3, 9)
					s.psi = R.Range(0, tau)
					s.z = 0
					s.vz = R.Range(0.25, 0.7)
					s.g = R.Range(0.85, 1.1)
					s.edgeOn = false
					cnt := 2
					if s.r > 24 {
						cnt = 4
					}
					splinters(s.cx, s.cy, cnt, s.vx, s.vy, 1.2, 60, 220)
					if R.Chance(0.15) {
						gx := s.cx + R.Range(-4, 4)
						gy := s.cy + R.Range(-4, 4)
						addGlint(gx, gy, R.Range(1, 2.4))
					}
				}
				if !s.broke || s.gone {
					continue
				}
				s.vy += 950 * s.g * dt
				s.vx -= s.vx * 0.3 * dt
				s.cx += s.vx * dt
				s.cy += s.vy * dt
				s.a += s.va * dt
				s.phi += s.vphi * dt
				s.z += s.vz * dt
				on := math.Abs(math.Cos(s.phi)) < 0.2
				if on && !s.edgeOn && t < end-0.3 {
					addGlint(s.cx, s.cy, R.Range(1.4, 2.6))
				}
				s.edgeOn = on
				if s.cy-s.r/(1+s.z) > H+2 {
					s.gone = true
				}
			}
			ps.Update(dt)
			bits.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p0 *pixelfx.Painter, _ float64) {
			p := clipTo(p0)
			if t < shatterBreak {
				yb := shatterPaneY(t) + H
				r1 := min(H, int(math.Floor(yb)))
				if r1 > 0 {
					rect(p0, 0, 0, W, float64(r1), ink)
					shine := 1 - pixelfx.Smooth(0.98, 1.22, t)
					y0 := yb - H
					if shine > 0 {
						for _, band := range [3][3]float64{{0, 16, 0.05}, {22, 3, 0.1}, {30, 1, 0.12}} {
							off, bw, al := band[0], band[1], band[2]
							for y := range r1 {
								xc := W*0.18 + off + (H-(float64(y)-y0))*0.7
								xStart := max(0, int(math.Floor(xc-bw/2)))
								xEnd := min(W, int(math.Ceil(xc+bw/2)))
								for x := xStart; x < xEnd; x++ {
									p0.Blend(float64(x)+0.5, float64(y)+0.5, C[6], al*shine)
								}
							}
						}
						st := 2
						if shine > 0.66 {
							st = 0
						} else if shine > 0.33 {
							st = 1
						}
						bevels := [3]struct {
							dy int
							c  uint32
						}{
							{1, C[6-st*2]},
							{2, C[4-st]},
							{3, C[max(1, 2-st)]},
						}
						for _, bv := range bevels {
							if r1-bv.dy >= 0 && r1-bv.dy < H {
								rect(p0, 0, float64(r1-bv.dy), W, 1, bv.c)
							}
						}
						gx := W*pixelfx.Clamp(t/(shatterLand+0.25))*1.2 - 20
						for x := range W {
							xf := float64(x)
							k := math.Max(0, 1-math.Abs(xf-gx)/28)
							if k > 0 && r1-1 < H {
								ec := C[6]
								if k > 0.5 {
									ec = pixelfx.White
								}
								p0.Px(xf, float64(r1-1), ec)
								p0.Light(xf, float64(r1-1), C[6], 0.5*k)
							}
							for d := 0; d < 14 && r1+d < H; d++ {
								dm := 1 - float64(d)/14
								p0.Light(xf, float64(r1+d), C[5], 0.3*shine*dm*dm*(0.6+0.8*k))
							}
						}
						if t < shatterLand+0.25 && gx > 0 && gx < W && r1-1 < H {
							pixelfx.Sparkle(p, gx, float64(r1-1), 6, C[6], C[4], 0.5)
						}
					}
				}
				if t >= shatterCrack {
					for _, c := range pat.cracks {
						if c.t0 > t {
							break
						}
						age := t - c.t0
						if c.x+1 < W && c.y+1 < H {
							p0.Px(float64(c.x+1), float64(c.y+1), C[1])
						}
						col := C[5]
						if age < 0.04 {
							col = pixelfx.White
						} else if age < 0.18 {
							col = C[6]
						}
						p0.Px(float64(c.x), float64(c.y), col)
						if age < 0.3 {
							p0.Light(float64(c.x), float64(c.y), C[5], 0.55*(1-age/0.3))
						}
					}
					if u := (t - shatterCrack) / 0.22; u < 1 {
						pixelfx.Flare(p, Ix, Iy, 30*math.Pow(1-u, 1.5), 1-u, C)
						p.Glow(Ix, Iy, 50, pixelfx.White, 0.7*(1-u))
					}
					if w := (t - (shatterBreak - 0.07)) / 0.07; w > 0 {
						for _, c := range pat.cracks {
							p0.Px(float64(c.x), float64(c.y), pixelfx.White)
							p0.Light(float64(c.x), float64(c.y), C[6], 0.4*w)
						}
					}
				}
			} else {
				fall = fall[:0]
				for ci := range pat.cells {
					s := &pat.cells[ci]
					if s.broke && !s.gone {
						fall = append(fall, s)
					}
				}
				slices.SortStableFunc(fall, func(a, b *shatterCell) int { return cmp.Compare(b.z, a.z) })
				for ci := range pat.cells {
					s := &pat.cells[ci]
					if !s.broke {
						drawShatterShard(p, p0, s, C, ink, true)
					}
				}
				for _, s := range fall {
					drawShatterShard(p, p0, s, C, ink, false)
				}
			}
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}

func drawShatterShard(p, p0 *pixelfx.Painter, s *shatterCell, C []uint32, ink uint32, still bool) {
	var pts [][2]float64
	face := ink
	sliver := false
	gl := 0.0
	if still {
		pts = s.pts
	} else {
		cp, sp := math.Cos(s.phi), math.Sin(s.phi)
		ci, si := math.Cos(s.psi), math.Sin(s.psi)
		ca, sa := math.Cos(s.a), math.Sin(s.a)
		k := 1 / (1 + s.z)
		pts = make([][2]float64, len(s.lp))
		for i, q := range s.lp {
			u := q[0]*ci + q[1]*si
			v := (-q[0]*si + q[1]*ci) * cp
			qx, qy := u*ci-v*si, u*si+v*ci
			pts[i] = [2]float64{s.cx + (qx*ca-qy*sa)*k, s.cy + (qx*sa+qy*ca)*k}
		}
		nx0, ny0, nz := si*sp, -ci*sp, cp
		nx, ny := nx0*ca-ny0*sa, nx0*sa+ny0*ca
		d := math.Abs(nx*shatterL3[0] + ny*shatterL3[1] + nz*shatterL3[2])
		gl = math.Pow(d, 6)
		gm := pixelfx.Clamp(1 - math.Abs(cp)/0.3)
		graze := gm * gm
		face = pixelfx.Mix(pixelfx.Mix(ink, C[2], 0.12+0.4*gl), C[3], 0.55*graze)
		sliver = math.Abs(cp) < 0.2
		if math.Abs(cp)*s.r < 1.5 {
			face = C[5]
		}
	}
	fillConvex(p0, pts, face, 1)
	a2 := 0.0
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		a2 += pts[j][0]*pts[i][1] - pts[i][0]*pts[j][1]
	}
	sg := -1.0
	if a2 > 0 {
		sg = 1
	}
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		ax, ay := pts[j][0], pts[j][1]
		bx, by := pts[i][0], pts[i][1]
		dx, dy := bx-ax, by-ay
		L := pixelfx.Hypot(dx, dy)
		if L < 0.5 {
			continue
		}
		mx, my := (dy/L)*sg, (-dx/L)*sg
		el := mx*shatterLX + my*shatterLY
		if still {
			col := C[4]
			if el > 0 {
				col = C[5]
			}
			p.Line(ax, ay, bx, by, col, 1)
			continue
		}
		if el > 0.2 || sliver {
			p.Line(ax-mx, ay-my, bx-mx, by-my, C[4], 1)
			col := C[6]
			if gl > 0.5 || sliver {
				col = pixelfx.White
			}
			p.Line(ax, ay, bx, by, col, 1)
			p.Glow((ax+bx)/2, (ay+by)/2, math.Min(12, 4+L*0.3), C[5], 0.12+0.3*gl)
		} else {
			col := C[2]
			if el > -0.3 {
				col = C[4]
			}
			p.Line(ax, ay, bx, by, col, 1)
		}
	}
}
