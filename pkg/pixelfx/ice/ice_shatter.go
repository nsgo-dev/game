// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/ice-shatter.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The crack-and-burst recipe (cracks walking out from the point of impact, a white frame, the ice flung apart as shards with ice dust and glints) is adapted from Sprite Fusion's open-source ice (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package ice

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// IceShatter is a block of ice taking a hit from the left: it jolts, cracks race over its faces, it flashes white and bursts into chunks that tumble, bounce and skid off in a roll of ice dust. It is registered as "ice/ice-shatter".
var IceShatter = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ice shatter",
		Desc:     "A block of ice takes a hit from the left: it jolts, cracks race over its faces, it flashes white and bursts into chunks that tumble, bounce and skid off in a roll of ice dust.",
		Tags:     []string{"ice", "shatter", "break", "destroy", "impact", "directional", "one-shot"},
		Size:     [2]int{224, 112},
		Origin:   [2]int{68, 90},
		Duration: shatterDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(icePalette...)},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newIceShatter,
}

func init() { pixelfx.Register("ice/ice-shatter", IceShatter) }

var (
	shatterDuration                                        = 2.8
	shatterHit, shatterCrack2, shatterShiver, shatterFlash = 0.26, 0.4, 0.54, 0.62
	shatterBurst                                           = 0.68
	// the block, seen from the front and a little above: a front face, a top, a side in shadow
	shatterFront = [][2]float64{{-16, 0}, {-16, -28}, {12, -28}, {12, 0}}
	shatterTop   = [][2]float64{{-16, -28}, {-8, -34}, {20, -34}, {12, -28}}
	shatterSide  = [][2]float64{{12, 0}, {12, -28}, {20, -34}, {20, -6}}
	shatterSil   = [][2]float64{{-16, 0}, {-16, -28}, {-8, -34}, {20, -34}, {20, -6}, {12, 0}}
	shatterRim   = [][2]float64{{-17, 1}, {-17, -28.5}, {-8.3, -35.2}, {21, -35.2}, {21, -5.6}, {12.5, 1}}
	shatterAt    = [2]float64{-16, -15}
	// a fracture plane inside, lighter
	shatterPlane = [][2]float64{{-16, -21}, {-5, -28}, {3, -13}, {-16, -9}}
	// the far corner in shadow
	shatterShade = [][2]float64{{5, -28}, {12, -28}, {12, -10}}
)

// shatterScuff is frost a sliding chunk scuffed onto the floor.
type shatterScuff struct{ x, t float64 }

func newIceShatter(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed())
	ps, bits, lights := newSim(R, C)
	W := pixelfx.White
	var scuffs []*shatterScuff
	splinter := func(x, y, vx float64, n int) {
		for range n {
			bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: x + R.Range(-2, 2), Y: y - 1, VX: vx*0.6 + R.Range(-35, 45), VY: -R.Range(30, 100), G: 0.8, Drag: 0.6, Bounce: 0.35, Life: R.Range(0.4, 0.8), A: R.Range(0, tau), VA: R.Range(-18, 18), S: R.Range(1.2, 2.4), C: C[int(R.Int(4, 7))]})
		}
	}
	pn := &panes{
		ground: floor0,
		onLand: func(q *pane, x, y, v float64) {
			// a chunk hitting the floor: a puff of ice dust, a few splinters, sometimes a glint
			if v < 100 {
				return
			}
			ps.Spawn(pixelfx.Vapour, x, y-1, q.vx*0.3+R.Range(-15, 15), -R.Range(2, 10), R.Range(0.4, 0.7), C[6], R.Range(1.6, 2.8), 0, 0)
			splinter(x, y, q.vx, 2)
			if R.Chance(0.3) {
				ps.Spawn(pixelfx.StarKind, x, y-3, 0, -5, R.Range(0.2, 0.4), W, R.Range(0.8, 1.6), 0, 0)
			}
		},
		onSkid: func(_ *pane, x, _ float64) {
			// sliding chunks scuff frost onto the floor
			if R.Chance(0.5) {
				scuffs = append(scuffs, &shatterScuff{x: pixelfx.Round(x + R.Range(-1, 1))})
			}
		},
		onCrumble: func(q *pane) {
			splinter(q.x, 0, q.vx, 3)
			ps.Spawn(pixelfx.Vapour, q.x, -1, R.Range(-10, 14), -R.Range(2, 6), R.Range(0.4, 0.7), C[6], R.Range(1.2, 2), 0, 0)
		},
	}

	// the ice's insides: veins of frost and trapped bubbles in the front face
	var veins [][2]float64
	var bubbles [][3]float64
	for range 3 {
		x, y, a := R.Range(-12, 8), R.Range(-24, -6), R.Range(0, tau)
		for range 9 {
			a += R.Range(-0.6, 0.6)
			x += math.Cos(a) * 1.5
			y += math.Sin(a) * 1.2
			if x < -14 || x > 10 || y < -26 || y > -2 {
				break
			}
			veins = append(veins, [2]float64{math.Floor(x), math.Floor(y)})
		}
	}
	for range 5 {
		bx := R.Int(-12, 8)
		by := R.Int(-23, -4)
		bubbles = append(bubbles, [3]float64{bx, by, pick(R.Chance(0.4), 1.0, 0)})
	}
	inside := func(x, y float64) bool { return inPoly(shatterSil, x, y) }
	cracks := fractures(R, inside, shatterAt[0]+1, shatterAt[1], 4, fractureOpts{len: 8, a0: pixelfx.Val(-0.9), spread: pixelfx.Val(1.9)})
	cracks2 := fractures(R, inside, shatterAt[0]+1, shatterAt[1], 6, fractureOpts{len: 18, a0: pixelfx.Val(-1.3), spread: pixelfx.Val(2.7), fork: pixelfx.Val(0.16)})

	t, struck, burst, wave2, white := 0.0, false, false, false, false
	jolt := func() float64 {
		switch {
		case t >= shatterHit && t < shatterHit+0.05:
			return 2
		case t >= shatterHit+0.05 && t < shatterHit+0.12:
			return 1
		case t >= shatterShiver && t < shatterFlash:
			return pick(int(math.Floor(t*40))%2 != 0, 1.0, 0)
		}
		return 0
	}

	// the block: shaded faces, frost veins and bubbles inside, lit edges, a dark rim
	drawBlock := func(p *pixelfx.Painter) {
		if t >= shatterBurst {
			return
		}
		jx, wh, sweep := jolt(), t >= shatterFlash, -40+70*pixelfx.Seg(t, 0.04, 0.3)
		sh := func(pts [][2]float64) [][2]float64 {
			out := make([][2]float64, len(pts))
			for i, q := range pts {
				out[i] = [2]float64{q[0] + jx, q[1]}
			}
			return out
		}
		// rim
		p.Poly(sh(shatterRim), C[0], 1)
		if wh {
			p.Poly(sh(shatterSil), W, 1)
			return
		}
		// the front: lit at the top, deep at the foot, the far edges showing through, a fracture plane catching light, reflections streaked across the upper left
		p.PolyFn(sh(shatterFront), func(x, y float64) (uint32, float64) {
			lx := x - jx
			d := lx + y*0.8
			switch {
			case math.Abs(d-sweep) < 1.3 && y < -6:
				return W, 1
			case y < -9 && (math.Abs(d+21) < 1.1 || math.Abs(d+16.5) < 0.6):
				return C[6], 1
			case ((math.Abs(lx+7.5) < 0.5 && y < -6) || (math.Abs(y+5.5) < 0.5 && lx > -8)) && pixelfx.ToInt32(math.Floor(lx)+math.Floor(y))%2 == 0:
				return C[5], 1
			case inPoly(shatterPlane, lx, y):
				return pick(y < -25.5, C[6], C[5]), 1
			case inPoly(shatterShade, lx, y):
				return C[3], 1
			case y < -25.5:
				return C[5], 1
			case y < -8:
				return C[4], 1
			case y < -2.5:
				return C[3], 1
			}
			return C[2], 1
		})
		p.PolyFn(sh(shatterTop), func(x, y float64) (uint32, float64) {
			switch {
			case y > -29.5:
				return C[7], 1
			case y < -32.5:
				return C[5], 1
			case pixelfx.Rand(math.Floor(x), math.Floor(y), 3) < 0.08:
				return C[7], 1
			}
			return C[6], 1
		})
		p.PolyFn(sh(shatterSide), func(x, y float64) (uint32, float64) {
			lx := x - jx
			switch {
			case lx < 13.5:
				return C[3], 1
			case math.Abs(lx-16.5) < 0.6 && y < -8:
				return C[3], 1
			case y > (lx-12)*-0.75-3:
				return C[1], 1
			}
			return C[2], 1
		})
		for _, v := range veins {
			p.Px(v[0]+jx, v[1], C[5])
		}
		for _, b := range bubbles {
			x, y := b[0], b[1]
			p.Px(x+jx, y, C[6])
			if b[2] != 0 {
				p.Px(x+jx+1, y, C[5])
				p.Px(x+jx, y+1, C[5])
			}
		}
		E := func(ax, ay, bx, by float64, c uint32) { p.Line(ax+jx, ay, bx+jx, by, c, 1) }
		E(-16, -28, 12, -28, W)
		E(-16, -28, -8, -34, C[6])
		E(-8, -34, 20, -34, C[5])
		E(12, -28, 20, -34, C[7])
		E(-16, -1, -16, -28, C[5])
		E(12, -1, 12, -28, C[6])
		E(20, -6, 20, -34, C[3])
		E(13, -1, 20, -6, C[1])
		if t >= shatterHit {
			drawFractures(p, cracks, (t-shatterHit)*170, C, crackOpts{ox: jx})
		}
		if t >= shatterCrack2 {
			drawFractures(p, cracks2, (t-shatterCrack2)*200, C, crackOpts{ox: jx})
		}
	}

	// the floor: a frost patch where the block stands, frost scuffed along by sliding chunks
	drawFloor := func(p *pixelfx.Painter) {
		if out := 1 - pixelfx.Smooth(shatterBurst+0.6, shatterBurst+1.5, t); out > 0 {
			for x := -22.0; x <= 26; x++ {
				d, h := math.Abs(x-2)/24, pixelfx.Rand(x, 41)
				a := out * pixelfx.Clamp((1.1-d)/0.25)
				if a <= 0 {
					continue
				}
				p.Dot(x, 0, pick(h < 0.35, W, C[6]), a)
				if h < 0.4 && (x < -16 || x > 20) {
					p.Dot(x, -1, C[5], a)
				}
				for j := 1.0; j <= 1+pixelfx.Round(pixelfx.Rand(x, 7)*3*(1-d)); j++ {
					p.Dot(x, j, pick(j < 2, C[5], C[4]), a*(1-j/5))
				}
			}
		}
		for _, s := range scuffs {
			if a := 1 - s.t/0.9; a > 0 {
				p.Dot(s.x, 0, pick(s.t < 0.15, W, C[6]), a)
				p.Dot(s.x, 1, C[4], a*0.6)
			}
		}
	}

	explode := func() {
		// chunks of each face, cut at random, flung away from the hit, mostly right and up
		var chop func(pts [][2]float64, maxArea float64, depth int) [][][2]float64
		chop = func(pts [][2]float64, maxArea float64, depth int) [][][2]float64 {
			c := centroid(pts)
			if c.area < maxArea || depth > 4 {
				return [][][2]float64{pts}
			}
			return splitAll(splitPoly(pts, c.x+R.Range(-1, 1), c.y+R.Range(-1, 1), R.Range(0, pi)), func(q [][2]float64) [][][2]float64 { return chop(q, maxArea, depth+1) })
		}
		faces := []struct {
			pts           [][2]float64
			tone, maxArea float64
		}{{shatterFront, 0.6, 46}, {shatterTop, 1, 40}, {shatterSide, 0.3, 46}}
		for _, face := range faces {
			for _, part := range chop(face.pts, face.maxArea, 0) {
				m := centroid(part)
				dx, dy := m.x-shatterAt[0], m.y-shatterAt[1]
				d := pixelfx.Hypot(dx, dy)
				if d == 0 {
					d = 1
				}
				sp := R.Range(50, 150) * (0.7 + 0.3*(1-d/40))
				pts := make([][2]float64, len(part))
				for i, q := range part {
					pts[i] = [2]float64{q[0] - m.x, q[1] - m.y}
				}
				np := pane{pts: pts, x: m.x, y: m.y}
				np.vx = (dx/d)*sp*0.9 + R.Range(40, 110)
				np.vy = (dy/d)*sp*0.4 - R.Range(60, 200)
				np.va = R.Range(-10, 10)
				np.f = R.Range(-0.9, 0.9)
				np.vf = R.Range(-7, 7)
				np.tone, np.lit, np.g, np.drag, np.bounce, np.solid = pixelfx.Val(face.tone), face.tone, 0.9, 0.5, 0.38, true
				np.brittle = 3
				np.crumble = pixelfx.Val(R.Range(0.25, 0.6))
				np.life = R.Range(1.5, 1.9)
				pn.add(np)
			}
		}
		for range 46 {
			x, y := R.Range(-15, 19), -R.Range(1, 33)
			dx, dy := x-shatterAt[0], y-shatterAt[1]
			d := pixelfx.Hypot(dx, dy)
			if d == 0 {
				d = 1
			}
			sp := R.Range(60, 200)
			if !inside(x, y) {
				continue
			}
			kind := pick(R.Chance(0.15), pixelfx.Chunk, pixelfx.Shard)
			bits.Add(pixelfx.Bit{Kind: kind, X: x, Y: y, VX: (dx/d)*sp*0.8 + R.Range(20, 90), VY: (dy/d)*sp*0.4 - R.Range(60, 240), G: 0.85, Drag: 0.6, Bounce: 0.32, Life: R.Range(0.8, 1.4), A: R.Range(0, tau), VA: R.Range(-22, 22), S: R.Range(1.2, 3.4), C: C[int(R.Int(4, 7))], Ramp: []uint32{C[1], C[3], C[4], C[6], C[7]}})
		}
		// ice dust rolls out along the floor, vapour hangs where it stood, a flurry, glints, a flash of light
		for range 9 {
			ps.Spawn(pixelfx.Vapour, R.Range(-6, 18), -R.Range(1, 6), R.Range(10, 130), -R.Range(2, 12), R.Range(0.6, 1.1), C[6], R.Range(2.2, 3.8), 0, 0)
		}
		for range 5 {
			ps.Spawn(pixelfx.Dust, R.Range(-6, 16), -R.Range(1, 4), R.Range(30, 110), -R.Range(2, 8), R.Range(0.5, 0.9), C[5], R.Range(1.6, 2.8), 0, 0)
		}
		for range 4 {
			ps.Spawn(pixelfx.Vapour, R.Range(-8, 18), -R.Range(8, 28), R.Range(-10, 50), -R.Range(8, 24), R.Range(0.6, 1), C[6], R.Range(2.6, 4), 0, 0)
		}
		for range 16 {
			a, sp := R.Range(-pi*0.9, -0.1), R.Range(40, 140)
			ps.Spawn(pixelfx.Flake, R.Range(-8, 16), -R.Range(6, 28), math.Cos(a)*sp+20, math.Sin(a)*sp, R.Range(1, 1.6), pick(R.Chance(0.6), W, C[6]), R.Range(0.5, 1.6), 0, 0)
		}
		for range 7 {
			ps.Spawn(pixelfx.StarKind, R.Range(-14, 40), -R.Range(4, 40), R.Range(-8, 16), -R.Range(4, 16), R.Range(0.4, 0.8), pick(R.Chance(0.5), W, C[6]), R.Range(1, 2.4), 0, 0)
		}
		lights.Add(2, -17, 64, W, 0.85, 0.14)
		lights.Add(16, -18, 86, C[4], 0.5, 0.55)
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the hit: a flash on the left face, chips and sparks thrown back, the block jolts
			if !struck && t >= shatterHit {
				struck = true
				lights.Add(shatterAt[0], shatterAt[1], 24, W, 0.6, 0.08)
				lights.Add(shatterAt[0]+6, shatterAt[1], 40, C[5], 0.4, 0.3)
				for range 12 {
					a, sp := pi+R.Range(-0.9, 0.9), R.Range(50, 120)
					ps.Spawn(pixelfx.Spark, shatterAt[0]-1, shatterAt[1], math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(0.15, 0.4), W, 1, 0, 0)
				}
				for range 6 {
					a, sp := pi+R.Range(-1, 0.8), R.Range(40, 100)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: shatterAt[0] - 1, Y: shatterAt[1] + R.Range(-2, 2), VX: math.Cos(a) * sp, VY: math.Sin(a)*sp - 60, G: 0.85, Drag: 0.6, Bounce: 0.3, Life: R.Range(0.5, 0.9), A: R.Range(0, tau), VA: R.Range(-20, 20), S: R.Range(1.2, 2.4), C: C[int(R.Int(5, 7))]})
				}
			}
			if !wave2 && t >= shatterCrack2 {
				wave2 = true
				lights.Add(0, -16, 36, C[6], 0.35, 0.15)
				for range 3 {
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: R.Range(-14, 18), Y: -R.Range(4, 30), VX: R.Range(-30, 30), VY: -R.Range(10, 50), G: 0.85, Drag: 0.6, Bounce: 0.3, Life: R.Range(0.4, 0.7), A: R.Range(0, tau), VA: R.Range(-20, 20), S: R.Range(1, 2), C: C[int(R.Int(5, 7))]})
				}
			}
			if !white && t >= shatterFlash {
				white = true
				lights.Add(2, -17, 44, W, 0.7, 0.08)
			}
			if !burst && t >= shatterBurst {
				burst = true
				explode()
			}
			// glints wink on the settling debris
			if burst && t < shatterBurst+1 && R.Chance(0.08) && len(bits.List) > 0 {
				b := pixelfx.Pick(R, bits.List)
				ps.Spawn(pixelfx.StarKind, b.X, b.Y-1, 0, -3, R.Range(0.2, 0.4), pick(R.Chance(0.5), W, C[6]), R.Range(0.6, 1.5), 0, 0)
			}
			for i := len(scuffs) - 1; i >= 0; i-- {
				if scuffs[i].t += dt; scuffs[i].t > 0.9 {
					scuffs = append(scuffs[:i], scuffs[i+1:]...)
				}
			}
			ps.Update(dt)
			bits.Update(dt)
			pn.update(dt)
			lights.Update(dt)
			endBy(t, shatterDuration, ps, bits, pn)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			drawFloor(p)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			drawBlock(p)
			// the hit: a star and a short flare on the left face
			if h := t - shatterHit; h >= 0 && h < 0.2 {
				pixelfx.Sparkle(p, shatterAt[0]-1, shatterAt[1], 9*pixelfx.Ease.OutBack(math.Min(1, h/0.04))*(1-h/0.2), C[6], C[4], 0.7)
				if h < 0.08 {
					pixelfx.Flare(p, shatterAt[0]-1, shatterAt[1], 14*(1-h/0.08), 1-h/0.08, C)
				}
			}
			// cold light in the ice, brighter as it cracks; a pool on the floor
			k, crk := 1-pixelfx.Smooth(shatterBurst, shatterBurst+0.4, t), pixelfx.Smooth(shatterHit, shatterFlash, t)
			if k > 0 {
				p.Glow(2, -17, 34, C[4], (0.16+0.22*crk)*k)
				p.Glow(2, -2, 30, C[4], 0.14*k)
			}
			if t >= shatterFlash && t < shatterBurst+0.12 {
				f := pixelfx.Clamp((t - shatterFlash) / 0.18)
				pixelfx.Flare(p, 2, -17, 26*(1-f), 1-f, C)
			}
			pn.draw(p, C)
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
