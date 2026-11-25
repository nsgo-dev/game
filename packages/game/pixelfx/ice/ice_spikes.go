// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/ice-spikes.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The eruption recipe (a frost line racing ahead, each spike punching up white with a spray of shards, a puff and a glint, the floor frosting behind) is adapted from Sprite Fusion's open-source ice (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package ice

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// IceSpikes is a frost line racing along the floor while clusters of jagged ice blades burst up behind it in a rising wave, glint, flash white together and shatter into toppling shards. It is registered as "ice/ice-spikes".
var IceSpikes = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ice spikes",
		Desc:     "A frost line races along the floor and clusters of jagged ice blades burst up behind it in a rising wave, glint, flash white together and shatter into toppling shards.",
		Tags:     []string{"ice", "spikes", "ground", "wave", "directional", "magic", "one-shot"},
		Size:     [2]int{264, 112},
		Origin:   [2]int{24, 96},
		Duration: spikesDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(icePalette...)},
			{Name: "reach", Default: pixelfx.Num(172)},
			{Name: "height", Default: pixelfx.Num(56)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newIceSpikes,
}

func init() { pixelfx.Register("ice/ice-spikes", IceSpikes) }

var (
	spikesDuration = 3.0
	// the frost line runs out
	spikesStart, spikesRun = 0.04, 0.44
	// a glint runs down the line
	spikesShimmer = 0.74
	// they shiver, flash white together and break
	spikesTremble, spikesFlash, spikesShatter = 1.2, 1.36, 1.42
	// the frost goes, oldest first
	spikesMelt = 1.75
	// spikesEase is the power the frost line slows by as it runs out.
	spikesEase = 1.7
)

// spikeBlade is one blade of a cluster: dx from the cluster's foot, its height, width and lean, whether it stands in the back row, how long after the cluster it bursts up, where its notch starts (below 0 none) and whether it is the cluster's main blade.
type spikeBlade struct {
	dx, h, w, lean float64
	back           bool
	delay, jag     float64
	main           bool
}

// spikeCluster is a cluster of blades along the line.
type spikeCluster struct {
	x, q, H, W float64
	blades     []*spikeBlade
	main       *spikeBlade
	t0         float64
	up         bool
}

// spikePose is a blade's base, tip and look at a moment.
type spikePose struct {
	g, x, tx, ty float64
	white        bool
}

func newIceSpikes(o *pixelfx.Options) pixelfx.Instance {
	C, R, REACH, height := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), o.Num("reach"), o.Num("height")
	ps, bits, lights := newSim(R, C)
	W := pixelfx.White
	splinter := func(x, y, vx float64, n int) {
		for range n {
			bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: x + R.Range(-2, 2), Y: y - 1, VX: vx*0.5 + R.Range(-45, 45), VY: -R.Range(30, 100), G: 0.8, Drag: 0.6, Bounce: 0.35, Life: R.Range(0.4, 0.75), A: R.Range(0, tau), VA: R.Range(-18, 18), S: R.Range(1.2, 2.4), C: C[int(R.Int(4, 7))]})
		}
	}
	pn := &panes{
		ground: floor0,
		onCrumble: func(q *pane) {
			splinter(q.x, 0, 0, 3)
			ps.Spawn(pixelfx.Vapour, q.x, -1, R.Range(-12, 12), -R.Range(2, 8), R.Range(0.4, 0.7), C[6], R.Range(1.4, 2.2), 0, 0)
		},
		onLand: func(q *pane, x, y, v float64) {
			if v < 110 {
				return
			}
			ps.Spawn(pixelfx.Vapour, x, y-1, R.Range(-20, 20), -R.Range(2, 8), R.Range(0.4, 0.7), C[6], R.Range(1.4, 2.4), 0, 0)
			splinter(x, y, q.vx, 3)
			if R.Chance(0.3) {
				ps.Spawn(pixelfx.StarKind, x, y-2, 0, -5, R.Range(0.2, 0.4), W, R.Range(0.8, 1.6), 0, 0)
			}
		},
	}
	front := func(t float64) float64 {
		return REACH * (1 - math.Pow(1-pixelfx.Seg(t, spikesStart, spikesStart+spikesRun), spikesEase))
	}
	reachedAt := func(x float64) float64 {
		return spikesStart + spikesRun*(1-math.Pow(1-pixelfx.Clamp(x/REACH), 1/spikesEase))
	}

	// clusters along the line: a main blade leaning into the wave and two to four splayed round it, heights swelling to a crest past the middle and falling off at the end
	env := func(q float64) float64 { return 0.28 + 0.72*math.Exp(-math.Pow((q-0.64)/0.3, 2)) }
	var clusters []*spikeCluster
	for x := 10.0; x < REACH-2; x += R.Range(20, 26) {
		q := x / REACH
		H := height * env(q) * R.Range(0.88, 1.06)
		Wd := pixelfx.ClampTo(H*0.36, 5, 14)
		lean := H * R.Range(0.2, 0.34)
		// a big blade leaning into the wave, a smaller one behind it leaning further, a stub in front of its foot
		backB := &spikeBlade{dx: Wd * R.Range(0.35, 0.6), h: H * R.Range(0.5, 0.7), w: Wd * 0.7, lean: H * R.Range(0.4, 0.6), back: true, delay: R.Range(0.02, 0.05), jag: -1}
		if R.Chance(0.5) {
			backB.jag = R.Range(0.35, 0.6)
		}
		mainB := &spikeBlade{dx: 0, h: H, w: Wd, lean: lean, back: false, delay: 0, jag: -1, main: true}
		if R.Chance(0.7) {
			mainB.jag = R.Range(0.32, 0.58)
		}
		list := []*spikeBlade{backB, mainB}
		if H > 14 {
			list = append(list, &spikeBlade{dx: -Wd * R.Range(0.35, 0.55), h: H * R.Range(0.24, 0.4), w: Wd * 0.55, lean: -H * R.Range(0, 0.12), back: false, delay: R.Range(0.03, 0.07), jag: -1})
		}
		clusters = append(clusters, &spikeCluster{x: x, q: q, H: H, W: Wd, blades: list, main: list[1], t0: reachedAt(x) + 0.012})
	}
	var crest *spikeCluster
	if len(clusters) > 0 {
		crest = clusters[0]
		for _, c := range clusters {
			if c.H > crest.H {
				crest = c
			}
		}
	}
	t, broke, trembled := 0.0, false, false

	// a blade's base, tip and look right now
	pose := func(c *spikeCluster, b *spikeBlade) spikePose {
		g, x := pixelfx.OutBack(pixelfx.Clamp((t-c.t0-b.delay)/0.09), 2.2), c.x+b.dx
		return spikePose{g: g, x: x, tx: x + b.lean*g, ty: -b.h * g, white: t-c.t0-b.delay < 0.03 || (t >= spikesFlash && t < spikesShatter)}
	}
	drawClusters := func(p *pixelfx.Painter) {
		if t >= spikesShatter {
			return
		}
		for pass := range 3 {
			for _, c := range clusters {
				if t < c.t0 {
					continue
				}
				jx := 0.0
				if t >= spikesTremble && t < spikesFlash && int(math.Floor(t*40)+pixelfx.Round(c.x))%2 != 0 {
					jx = 1
				}
				for _, b := range c.blades {
					if (pass == 0) != b.back || (pass == 1) != b.main {
						continue
					}
					s := pose(c, b)
					if s.g <= 0 {
						continue
					}
					gl := (t - spikesShimmer - c.q*0.34) / 0.2
					blade(p, s.x+jx, 0, s.tx+jx, s.ty, b.w, C, bladeOpts{white: s.white, jag: b.jag, dim: pick(b.back, 1.0, 0), glint: pick(gl >= 0 && gl <= 1, gl, -1)})
				}
				if pass == 2 {
					// light: a pop at the base as it bursts, a cold glow up its length while it stands
					pop, wh := pixelfx.Clamp(1-(t-c.t0)/0.15), t >= spikesFlash
					p.Glow(c.x+c.main.lean*0.4, -c.H*0.45, c.H*0.9+10, C[4], (0.16+0.42*pop+pick(wh, 0.4, 0))*(0.85+0.15*math.Sin(t*3+c.x)))
					if pop > 0 {
						p.Glow(c.x, -2, 14, W, 0.55*pop)
					}
				}
			}
		}
	}

	// the frosted floor: a crust with rime tufts, a bloom under the surface, a white-hot head while it runs
	drawFrost := func(p *pixelfx.Painter) {
		fr := front(t)
		for x := -3.0; x <= math.Min(fr, REACH+6); x++ {
			q := pixelfx.Clamp(x / REACH)
			age := t - reachedAt(math.Max(0, x))
			a := 1 - pixelfx.Smooth(spikesMelt+q*0.35, spikesMelt+q*0.35+0.6, t)
			if a <= 0 || age < 0 {
				continue
			}
			h, grow, head := pixelfx.Rand(x, 17), pixelfx.Smooth(0, 0.3, age), fr-x < 3 && fr < REACH
			p.Dot(x, 0, pick(head || age < 0.06 || h < 0.3, W, C[6]), a)
			L := 0.0
			if h < 0.45 {
				L = pixelfx.Round(grow * (1 + pixelfx.Rand(x, 5)*2.6))
			}
			for j := 1.0; j <= L; j++ {
				p.Dot(x, -j, pick(j == L, W, C[5]), a)
			}
			if L > 1 && pixelfx.Rand(x, 9) < 0.5 {
				p.Dot(x+1, -L+1, C[6], a)
			}
			D := grow * (2 + pixelfx.Rand(x, 11)*4) * (1 - q*0.3)
			for j := 1.0; j <= D; j++ {
				var c uint32
				switch {
				case pixelfx.Rand(x*31+j, 7) < 0.06:
					c = W
				case j < 2:
					c = C[5]
				case j < 4:
					c = C[4]
				default:
					c = C[3]
				}
				p.Dot(x, j, c, a*(1.1-j/(D+1)))
			}
			if head {
				p.Dot(x, -1, W, 1)
			}
		}
		if fr < REACH && t > spikesStart {
			p.Glow(fr, -2, 16, C[6], 0.6)
			p.Glow(fr, -1, 7, W, 0.6)
		}
	}

	breakBlade := func(c *spikeCluster, b *spikeBlade) {
		s := pose(c, b)
		ax, ay := s.tx-s.x, s.ty
		L := pixelfx.Hypot(ax, ay)
		if L == 0 {
			L = 1
		}
		nx, ny, hw := -ay/L, ax/L, b.w/2
		tri := [][2]float64{{s.x - nx*hw, -ny * hw}, {s.tx, s.ty}, {s.x + nx*hw, ny * hw}}
		ang := math.Atan2(ay, ax) + pi/2
		parts := [][][2]float64{tri}
		if b.h > 9 {
			parts = splitAll(parts, func(poly [][2]float64) [][][2]float64 { return splitPoly(poly, s.x+ax*0.4, ay*0.4, ang) })
		}
		if b.h > 22 {
			parts = splitAll(parts, func(poly [][2]float64) [][][2]float64 {
				if slices.ContainsFunc(poly, func(q [2]float64) bool { return q[1] < ay*0.5 }) {
					return splitPoly(poly, s.x+ax*0.72, ay*0.72, ang)
				}
				return [][][2]float64{poly}
			})
		}
		for _, part := range parts {
			mx, my := 0.0, 0.0
			for _, q := range part {
				mx += q[0]
				my += q[1]
			}
			mx /= float64(len(part))
			my /= float64(len(part))
			up := pixelfx.Clamp(-my / 40)
			pts := make([][2]float64, len(part))
			for i, q := range part {
				pts[i] = [2]float64{q[0] - mx, q[1] - my}
			}
			np := pane{pts: pts, x: mx, y: my}
			np.vx = R.Range(-25, 60)
			np.vy = -R.Range(40, 130) * (0.5 + up)
			np.va = R.Range(-8, 8)
			np.f = R.Range(-0.7, 0.7)
			np.vf = R.Range(-6, 6)
			np.lit, np.g, np.drag, np.bounce, np.solid = pick(b.back, 0.45, 0.75), 0.9, 0.6, 0.3, true
			np.ridge = pixelfx.Val(math.Atan2(ay, ax))
			np.brittle = pick(b.h > 30, 2, 1)
			np.crumble = pixelfx.Val(R.Range(0.12, 0.4))
			np.life = R.Range(1, 1.4)
			pn.add(np)
		}
		for i := 0.0; i < 2+b.h/7; i++ {
			u := R.Next()
			kind := pick(R.Chance(0.15), pixelfx.Chunk, pixelfx.Shard)
			bits.Add(pixelfx.Bit{Kind: kind, X: s.x + ax*u + R.Range(-1.5, 1.5), Y: ay * u, VX: R.Range(-60, 90), VY: -R.Range(60, 230), G: 0.9, Drag: 0.6, Bounce: 0.3, Life: R.Range(0.7, 1.3), A: R.Range(0, tau), VA: R.Range(-20, 20), S: R.Range(1.2, 3.2), C: C[int(R.Int(4, 7))], Ramp: []uint32{C[1], C[3], C[4], C[6], C[7]}})
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the cast: a puff and a spray where the line starts
			if t-dt <= spikesStart && t > spikesStart {
				for range 4 {
					ps.Spawn(pixelfx.Vapour, R.Range(0, 6), -R.Range(0, 3), R.Range(10, 60), -R.Range(2, 12), R.Range(0.4, 0.7), C[6], R.Range(2, 3.2), 0, 0)
				}
				lights.Add(4, -4, 26, C[5], 0.6, 0.25)
			}
			// the running front throws frost: streaks along the floor, motes, vapour
			fr := front(t)
			if t > spikesStart && fr < REACH-1 {
				if R.Chance(0.5) {
					ps.Spawn(pixelfx.Streak, fr-R.Range(4, 12), -R.Range(1, 3), R.Range(40, 80), -R.Range(0, 6), R.Range(0.08, 0.14), pick(R.Chance(0.5), C[6], W), 1, 0, 0)
				}
				if R.Chance(0.3) {
					ps.Spawn(pixelfx.Flake, fr, -1, R.Range(10, 50), -R.Range(20, 70), R.Range(0.5, 0.9), pick(R.Chance(0.5), W, C[6]), R.Range(0.5, 1.2), 0, 0)
				}
				if R.Chance(0.12) {
					ps.Spawn(pixelfx.Vapour, fr, -R.Range(0, 3), R.Range(20, 60), -R.Range(2, 10), R.Range(0.4, 0.7), C[6], R.Range(1.6, 2.8), 0, 0)
				}
			}
			for _, c := range clusters {
				if c.up || t < c.t0 {
					continue
				}
				c.up = true
				// each cluster bursts up: shards sprayed forward, a puff, motes, a glint at the tip once it stands
				big, n := c == crest, 3+pixelfx.Round(c.H/8)
				for i := 0.0; i < n; i++ {
					a := -pi/2 + R.Range(-1.1, 1.0)
					sp := R.Range(60, 190) * (0.45 + c.H/44)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: c.x + R.Range(-3, 3), Y: -1, VX: math.Cos(a)*sp + 25, VY: math.Sin(a) * sp, G: 0.8, Drag: 0.6, Bounce: 0.3, Life: R.Range(0.6, 1.1), A: R.Range(0, tau), VA: R.Range(-18, 18), S: R.Range(1.2, 3), C: C[int(R.Int(5, 7))]})
				}
				ps.Spawn(pixelfx.Vapour, c.x, -1, R.Range(-10, 40), -R.Range(2, 10), R.Range(0.5, 0.9), C[6], R.Range(2.2, 3.4)*(0.6+c.H/60), 0, 0)
				for range 2 {
					ps.Spawn(pixelfx.Flake, c.x+R.Range(-4, 4), -R.Range(2, c.H*0.5), R.Range(-20, 40), -R.Range(20, 60), R.Range(0.6, 1.1), pick(R.Chance(0.5), W, C[6]), R.Range(0.5, 1.3), 0, 0)
				}
				if c.H > 16 {
					ps.Spawn(pixelfx.StarKind, c.x+c.main.lean, -c.H*0.96, 0, -4, R.Range(0.3, 0.5), W, R.Range(1.4, 2.6), 0.08, 0)
				}
				lights.Add(c.x, -c.H*0.4, 16+c.H*0.6, C[5], 0.5, 0.18)
				if big {
					lights.Add(c.x, -c.H*0.5, 54, W, 0.5, 0.14)
					for range 4 {
						ps.Spawn(pixelfx.Vapour, c.x+R.Range(-6, 6), -R.Range(0, 4), R.Range(-40, 60), -R.Range(4, 16), R.Range(0.6, 1), C[6], R.Range(3, 4.5), 0, 0)
					}
				}
			}
			// while they stand: glints wink on the blades
			if t > spikesStart+spikesRun && t < spikesTremble && R.Chance(0.09) {
				c := pixelfx.Pick(R, clusters)
				b := pixelfx.Pick(R, c.blades)
				s, u := pose(c, b), R.Range(0.3, 0.95)
				ps.Spawn(pixelfx.StarKind, s.x+(s.tx-s.x)*u, s.ty*u, 0, -3, R.Range(0.25, 0.5), pick(R.Chance(0.6), W, C[6]), R.Range(0.6, 2), 0, 0)
			}
			// they shiver: chips fall
			if !trembled && t >= spikesTremble {
				trembled = true
				for _, c := range clusters {
					for range 2 {
						bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: c.x + R.Range(-4, 4), Y: -R.Range(4, c.H*0.8), VX: R.Range(-20, 20), VY: -R.Range(0, 30), G: 0.8, Drag: 0.5, Bounce: 0.3, Life: R.Range(0.3, 0.6), A: R.Range(0, tau), VA: R.Range(-18, 18), S: R.Range(1, 1.8), C: C[int(R.Int(5, 7))]})
					}
				}
			}
			if !broke && t >= spikesShatter {
				broke = true
				// all at once: every blade breaks into toppling pieces, splinters and ice dust, light along the line
				for _, c := range clusters {
					for _, b := range c.blades {
						breakBlade(c, b)
					}
					for range 2 {
						ps.Spawn(pixelfx.Vapour, c.x+R.Range(-6, 6), -R.Range(1, 8), R.Range(-30, 50), -R.Range(4, 18), R.Range(0.6, 1.1), C[6], R.Range(2.4, 4)*(0.7+c.H/60), 0, 0)
					}
					if R.Chance(0.7) {
						ps.Spawn(pixelfx.StarKind, c.x+R.Range(-6, 6), -R.Range(4, c.H), R.Range(-10, 10), -R.Range(6, 20), R.Range(0.4, 0.8), pick(R.Chance(0.5), W, C[6]), R.Range(1, 2.2), 0, 0)
					}
					for range 2 {
						ps.Spawn(pixelfx.Flake, c.x+R.Range(-6, 6), -R.Range(4, c.H), R.Range(-30, 60), -R.Range(30, 90), R.Range(0.9, 1.5), pick(R.Chance(0.6), W, C[6]), R.Range(0.5, 1.5), 0, 0)
					}
					lights.Add(c.x, -c.H*0.5, 22+c.H, W, 0.45, 0.12)
				}
				for i := range 5 {
					lights.Add((REACH*(float64(i)+0.5))/5, -18, 40, C[4], 0.4, 0.5)
				}
			}
			// the shatter's dust settles; glints on the debris
			if broke && t < spikesShatter+0.8 && R.Chance(0.1) && len(bits.List) > 0 {
				b := pixelfx.Pick(R, bits.List)
				ps.Spawn(pixelfx.StarKind, b.X, b.Y-1, 0, -3, R.Range(0.2, 0.4), pick(R.Chance(0.5), W, C[6]), R.Range(0.6, 1.5), 0, 0)
			}
			ps.Update(dt)
			bits.Update(dt)
			pn.update(dt)
			lights.Update(dt)
			endBy(t, spikesDuration, ps, bits, pn)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			drawFrost(p)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			drawClusters(p)
			// the cold light the frosted floor gives off
			k, fr := (1-pixelfx.Smooth(spikesMelt, spikesMelt+0.9, t))*pixelfx.Smooth(spikesStart, spikesStart+0.3, t), front(t)
			for i := range 6 {
				p.Glow((fr*(float64(i)+0.5))/6, -3, 26, C[4], 0.15*k)
			}
			if t >= spikesFlash && t < spikesShatter+0.1 {
				for _, c := range clusters {
					pixelfx.Sparkle(p, c.x+c.main.lean, -c.H, 6*(1-pixelfx.Clamp((t-spikesFlash)/0.16)), C[6], C[4], 0.4)
				}
			}
			pn.draw(p, C)
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
