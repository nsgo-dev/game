// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/earth-spike.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the eruption (a spike punching up with overshoot and a white frame, a burst of debris, mist and glints, then breaking into tumbling pieces) from Sprite Fusion's open-source ice spikes, and the grit and rolling dust from his explosion (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// EarthSpike is a faceted stone spike that erupts from a cracking, glowing floor with two side shards, then splits and crumbles into rubble. It is registered as "nature/earth-spike".
var EarthSpike = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Earth spike",
		Desc:     "The floor cracks with light and heaves, a faceted stone spike rams up with two side shards in a burst of chips, grit and dust; rubble rains, then it splits and crumbles.",
		Tags:     []string{"nature", "earth", "stone", "spike", "ground", "attack", "one-shot"},
		Size:     [2]int{272, 150},
		Origin:   [2]int{136, 132},
		Duration: earthSpikeDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#120c0c", "#251917", "#3b2a24", "#553d31", "#715440", "#8f6e52", "#b8966e", "#e6d0a8")},
			{Name: "glow", Default: pixelfx.Palette("#4a1a0c", "#a8401a", "#ff8a2a", "#ffd27a", "#fff4d0")},
			{Name: "height", Default: pixelfx.Num(62)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newEarthSpike,
}

func init() { pixelfx.Register("nature/earth-spike", EarthSpike) }

const earthSpikeDuration = 3.4

var (
	earthUp    = 0.34
	earthRise  = 0.09
	earthCrack = 1.18
	earthSplit = 1.5
	earthPart  = 0.1
	earthFall  = 0.42
	earthSink  = 0.55
)

type earthShard struct {
	x, H, w, lean, at, split float64
	back, main               bool
	look                     spikeLookData
	cracks                   [][][2]float64
	up, split0               bool
	cut, top                 float64
}

type earthSlab struct {
	sd, x float64
	pts   [][2]float64
	s, a  float64
}

func newEarthSpike(o *pixelfx.Options) pixelfx.Instance {
	S := pixelfx.Fit(o.Palette(), 8)
	G := pixelfx.Fit(o.Ramp("glow"), 5)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	H := o.Num("height")
	DUST := pixelfx.Mix(S[6], pixelfx.Hex("#8c8494"), 0.45)
	DUST2 := pixelfx.Mix(S[5], pixelfx.Hex("#6c6474"), 0.4)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{G[4], G[3], G[2], G[1]}})
	bits := pixelfx.NewBits(floor)
	lights := &pixelfx.Lights{}

	// the spike and two side shards leaning away from it, each with its own chips, strata and cracks
	spikes := []earthShard{
		{x: -H * 0.17, H: H * 0.52, w: H * 0.22, lean: -H * R.Range(0.13, 0.18), at: earthUp + 0.045, split: earthSplit - 0.1, back: true},
		{x: H * 0.18, H: H * 0.4, w: H * 0.2, lean: H * R.Range(0.11, 0.15), at: earthUp + 0.085, split: earthSplit + 0.08, back: false},
		{x: 0, H: H, w: H * 0.36, lean: R.Range(-2, 2), at: earthUp, split: earthSplit, back: false, main: true},
	}
	for i := range spikes {
		s := &spikes[i]
		s.look = spikeLook(R)
		c0 := spikeCrack(R, R.Range(0.04, 0.16), R.Range(-0.6, -0.1), 1)
		c1 := spikeCrack(R, R.Range(0.22, 0.4), R.Range(0.1, 0.6), -1)
		c2 := spikeCrack(R, R.Range(0.42, 0.58), R.Range(-0.4, 0.4), R.Sign())
		s.cracks = [][][2]float64{c0, c1, c2}
		s.cut = R.Range(0.24, 0.32)
		s.top = 1
	}
	cracks := [][][2]float64{
		floorCrack(R, -1, R.Range(34, 44)),
		floorCrack(R, 1, R.Range(34, 44)),
		floorCrack(R, -1, R.Range(14, 20)),
		floorCrack(R, 1, R.Range(14, 20)),
	}
	slabs := make([]earthSlab, 2)
	for i, sd := range [2]float64{-1, 1} {
		sx := sd * R.Range(13, 16)
		ss := R.Range(5.5, 7)
		sa := sd * R.Range(0.45, 0.65)
		slabs[i] = earthSlab{sd: sd, x: sx, pts: [][2]float64{{-1.2, -0.32}, {1.1, -0.42}, {1.2, 0.36}, {-1.1, 0.4}}, s: ss, a: sa}
	}
	rock := func(b pixelfx.Bit) {
		b.Data = stoneShape(R, 0)
		bits.Add(b)
	}
	t := 0.0
	grown := func(s *earthShard) float64 {
		if t < s.at {
			return 0
		}
		return pixelfx.OutBack(pixelfx.Clamp((t-s.at)/earthRise), 2.2)
	}
	open := func() float64 { return pixelfx.Ease.OutCubic(pixelfx.Clamp((t - 0.02) / 0.24)) }
	heat := func() float64 {
		if t < earthUp {
			return pixelfx.Smooth(0, earthUp, t)
		}
		return 1 - pixelfx.Smooth(earthUp, earthUp+0.9, t)
	}
	// after the split the halves part, tip outwards and come down from the top as rubble
	gap := func(s *earthShard) float64 {
		m := 2.0
		if s.main {
			m = 3
		}
		return pixelfx.Smooth(s.split, s.split+earthPart, t) * m
	}
	tip := func(s *earthShard) float64 {
		return pixelfx.Smooth(s.split, s.split+earthPart+earthFall, t) * 0.12 * s.H
	}
	topAt := func(s *earthShard) float64 {
		return pixelfx.Lerp(1, s.cut, pixelfx.Ease.InQuad(pixelfx.Clamp((t-s.split-0.04)/earthFall)))
	}

	erupt := func(s *earthShard) {
		k := s.H / H
		lights.Add(s.x, -s.H*0.4, 30*k+14, pixelfx.White, 0.5*k+0.1, 0.1)
		lights.Add(s.x, -10, 60*k+20, G[2], 0.34*k, 0.5)
		for i := 0; i < int(pixelfx.Round(16*k))+3; i++ {
			a := -pi/2 + R.Range(-1.1, 1.1)
			sp := R.Range(140, 360) * (0.6 + 0.4*k)
			bx := s.x + R.Range(-s.w/3, s.w/3)
			by := -R.Range(1, 6)
			life := R.Range(1.4, 2.4)
			ang := R.Range(0, tau)
			va := R.Range(-14, 14)
			sz := R.Range(1.6, 3.8) * (0.6 + 0.4*k)
			rock(pixelfx.Bit{Kind: pixelfx.Chunk, X: bx, Y: by, VX: math.Cos(a) * sp, VY: math.Sin(a) * sp, G: 1, Drag: 0.35, Bounce: 0.32, Life: life, A: ang, VA: va, S: sz})
		}
		for i := 0; i < int(pixelfx.Round(55*k))+10; i++ {
			u := R.Range(-1, 1)
			a := -pi/2 + u*0.9
			sp := R.Range(90, 330) * (1 - 0.35*math.Abs(u))
			life := R.Range(1, 2)
			c := pixelfx.Pick(R, []uint32{S[6], S[5], S[4], S[4], S[3]})
			sz := 1.0
			if R.Chance(0.4) {
				sz = 2
			}
			ps.Spawn(pixelfx.Grit, s.x+u*s.w*0.4, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, c, sz, 0, 0)
		}
		for i := 0; i < int(pixelfx.Round(9*k))+2; i++ {
			a := -pi/2 + R.Range(-1.2, 1.2)
			sp := R.Range(160, 380)
			py := -R.Range(2, 10)
			life := R.Range(0.15, 0.4)
			ps.Spawn(pixelfx.Spark, s.x, py, math.Cos(a)*sp, math.Sin(a)*sp, life, pixelfx.White, 1, 0, 0)
		}
		// dust: a low skirt rolling out both ways, a few puffs hanging round the base
		for i := 0; i < int(pixelfx.Round(10*k))+3; i++ {
			sd := R.Sign()
			px := s.x + sd*R.Range(2, 10)
			py := -R.Range(1, 4)
			vx := sd * R.Range(50, 170) * (0.5 + 0.5*k)
			vy := -R.Range(3, 16)
			life := R.Range(0.9, 1.6)
			c := DUST2
			if R.Chance(0.5) {
				c = DUST
			}
			sz := R.Range(3, 5.5) * (0.6 + 0.4*k)
			delay := R.Range(0, 0.05)
			ps.Spawn(pixelfx.Smoke, px, py, vx, vy, life, c, sz, delay, pixelfx.Big)
		}
		for i := 0; i < int(pixelfx.Round(14*k))+4; i++ {
			sd := R.Sign()
			px := s.x + sd*R.Range(0, 8)
			py := -R.Range(0, 3)
			vx := sd * R.Range(40, 160)
			vy := -R.Range(2, 20)
			life := R.Range(0.5, 1)
			c := DUST2
			if R.Chance(0.5) {
				c = DUST
			}
			sz := R.Range(1.5, 3.2)
			ps.Spawn(pixelfx.Dust, px, py, vx, vy, life, c, sz, 0, 0)
		}
		for i := 0; i < int(pixelfx.Round(3*k))+1; i++ {
			px := s.x + R.Range(-6, 6)
			py := -R.Range(2, 12)
			vx := R.Range(-20, 20)
			vy := -R.Range(15, 45)
			life := R.Range(1, 1.6)
			sz := R.Range(4, 6)*k + 2
			delay := R.Range(0.02, 0.12)
			ps.Spawn(pixelfx.Smoke, px, py, vx, vy, life, DUST, sz, delay, pixelfx.Big)
		}
		if s.main {
			for range 3 {
				px := s.x + R.Range(-5, 5)
				py := -R.Range(10, s.H*0.9)
				life := R.Range(0.25, 0.5)
				sz := R.Range(1, 2.2)
				delay := R.Range(0.1, 0.4)
				ps.Spawn(pixelfx.StarKind, px, py, 0, -3, life, S[7], sz, delay, 0)
			}
			// rubble that went up high comes raining back down
			for range 14 {
				px := R.Range(-60, 60)
				py := -R.Range(110, 125)
				vx := R.Range(-15, 15)
				vy := R.Range(10, 60)
				life := R.Range(1.2, 1.8)
				c := pixelfx.Pick(R, []uint32{S[5], S[4], S[3]})
				sz := 1.0
				if R.Chance(0.35) {
					sz = 2
				}
				delay := R.Range(0.25, 0.9)
				ps.Spawn(pixelfx.Grit, px, py, vx, vy, life, c, sz, delay, 0)
			}
		}
	}

	// one band of a spike, u0..u1, breaks off both halves into rubble that drops and tumbles
	crumble := func(s *earthShard, u0, u1 float64) {
		g := gap(s)
		lean := s.lean
		tp := tip(s)
		for _, half := range [2]float64{-1, 1} {
			u := (u0 + u1) / 2
			hw := (s.w / 2) * math.Pow(1-u, 0.85)
			x := s.x + lean*u + half*(g/2+hw*0.45+tp*u)
			y := -u * s.H
			if hw < 0.8 && half > 0 {
				continue
			}
			vx := half*R.Range(8, 40) + lean*0.6
			vy := -R.Range(0, 50)
			life := math.Min(R.Range(1.1, 1.6), earthSpikeDuration-0.15-t)
			ang := R.Range(0, tau)
			va := R.Range(-8, 8)
			rock(pixelfx.Bit{Kind: pixelfx.Chunk, X: x, Y: y, VX: vx, VY: vy, G: 1, Drag: 0.3, Bounce: 0.3, Life: life, A: ang, VA: va, S: pixelfx.ClampTo(hw*0.62, 1.6, 4.8)})
			for range 3 {
				px := x + R.Range(-2, 2)
				py := y + R.Range(-2, 2)
				gvx := half * R.Range(5, 45)
				gvy := -R.Range(10, 70)
				glife := R.Range(0.9, 1.6)
				c := pixelfx.Pick(R, []uint32{S[6], S[5], S[4], S[3]})
				sz := 1.0
				if R.Chance(0.35) {
					sz = 2
				}
				ps.Spawn(pixelfx.Grit, px, py, gvx, gvy, glife, c, sz, 0, 0)
			}
		}
		if R.Chance(0.5) {
			px := s.x + lean*u1 + R.Range(-4, 4)
			vx := R.Sign() * R.Range(15, 45)
			vy := R.Range(20, 60)
			life := R.Range(0.35, 0.6)
			sz := R.Range(2, 3.5)
			ps.Spawn(pixelfx.Dust, px, -u1*s.H, vx, vy, life, DUST, sz, 0, 0)
		}
	}

	drawShard := func(p *pixelfx.Painter, s *earthShard) {
		g := grown(s)
		if g <= 0 {
			return
		}
		if t < s.split {
			white := t-s.at < 0.04 || t > s.split-0.045
			off := 0.05
			if s.main {
				off = 0
			}
			crack := pixelfx.Smooth(earthCrack+off, s.split-0.03, t)
			sw := (t-s.at)*0.9 - 0.15
			glint := -1.0
			if sw > 0 && math.Mod(sw, 1.2) < 0.55 {
				glint = math.Mod(sw, 1.2) / 0.55
			}
			drawSpike(p, s.x, 0, s.H*g, s.w, s.lean*math.Min(1, g), s.look, S, spikeOpts{
				a: 1, white: white, glint: glint, clip: 1, floorY: math.Inf(1),
				cracks: s.cracks, crack: crack, heat: pixelfx.Smooth(earthCrack, s.split, t), glowRamp: G,
			})
			return
		}
		// split: two halves part along the ridge and tip away, crumbling from the top; the stump sinks
		top, gp, tp := topAt(s), gap(s), tip(s)
		sink := pixelfx.Smooth(s.split+earthFall, s.split+earthFall+earthSink, t) * s.H * (s.cut + 0.06)
		if sink >= s.H*(s.cut+0.05) {
			return
		}
		for _, half := range [2]int{-1, 1} {
			drawSpike(p, s.x+(float64(half)*gp)/2, sink, s.H, s.w, s.lean+float64(half)*tp, s.look, S, spikeOpts{
				a: 1, glint: -1, clip: top, half: half, floorY: 0,
				cracks: s.cracks, crack: 1, heat: 1 - pixelfx.Smooth(s.split, s.split+0.45, t), glowRamp: G,
			})
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			f := math.Min(1, dt*120)
			// the floor splits open: grit hops out of the cracks as they run, dust puffs from them
			if t < earthUp {
				L := open()
				for _, c := range cracks {
					if R.Chance(0.35 * f) {
						idx := min(len(c)-1, int(math.Floor(R.Range(0, L)*float64(len(c)))))
						q := c[idx]
						vx := R.Range(-20, 20)
						vy := -R.Range(30, 110)
						life := R.Range(0.5, 1)
						col := pixelfx.Pick(R, []uint32{S[5], S[4]})
						ps.Spawn(pixelfx.Grit, q[0], -1, vx, vy, life, col, 1, 0, 0)
						if R.Chance(0.25) {
							ps.Spawn(pixelfx.Dust, q[0], -1, R.Range(-15, 15), -R.Range(4, 14), R.Range(0.3, 0.6), DUST2, R.Range(1.5, 3), 0, 0)
						}
					}
				}
				if t > 0.12 && R.Chance(0.2*f) {
					px := R.Sign() * R.Range(10, 16)
					py := -R.Range(1, 4)
					vx := R.Sign() * R.Range(10, 30)
					vy := -R.Range(4, 12)
					life := R.Range(0.4, 0.7)
					sz := R.Range(2, 3.5)
					ps.Spawn(pixelfx.Dust, px, py, vx, vy, life, DUST2, sz, 0, 0)
				}
			}
			for i := range spikes {
				s := &spikes[i]
				if !s.up && t >= s.at {
					s.up = true
					erupt(s)
				}
				if !s.split0 && t >= s.split {
					s.split0 = true
					lights.Add(s.x, -s.H*0.5, 30, pixelfx.White, 0.3, 0.08)
					ps.Spawn(pixelfx.StarKind, s.x+s.lean*0.5, -s.H*0.55, 0, -2, 0.3, S[7], 2.5, 0, 0)
				}
				// crumbling from the top: each band that goes turns to rubble
				if t >= s.split {
					top := topAt(s)
					for s.top-top > 0.045 || (top <= s.cut+1e-6 && s.top > s.cut+1e-6) {
						u1 := s.top
						u0 := math.Max(top, s.top-0.11)
						crumble(s, u0, u1)
						s.top = u0
						if u0 <= s.cut+1e-6 {
							break
						}
					}
				}
				// cracking: grit and dust dribble out of the cracks
				if t < s.split && t > earthCrack && R.Chance(0.12*f) {
					u := R.Range(0.1, 0.7)
					px := s.x + s.lean*u + R.Range(-2, 2)
					vx := R.Range(-15, 15)
					vy := -R.Range(0, 20)
					life := R.Range(0.6, 1.1)
					col := pixelfx.Pick(R, []uint32{S[5], S[4]})
					ps.Spawn(pixelfx.Grit, px, -u*s.H, vx, vy, life, col, 1, 0, 0)
					if R.Chance(0.3) {
						ps.Spawn(pixelfx.Dust, s.x+s.lean*u, -u*s.H, R.Range(-10, 10), -R.Range(2, 8), R.Range(0.4, 0.7), DUST, R.Range(1.5, 2.5), 0, 0)
					}
				}
				// while it stands, pebbles trickle off its flanks and light glints off its edges
				if s.up && t < s.split && t > s.at+0.2 && R.Chance(0.05*f) {
					u := R.Range(0.2, 0.8)
					sd := R.Sign()
					vx := sd * R.Range(10, 30)
					vy := -R.Range(0, 15)
					life := R.Range(0.7, 1.2)
					col := pixelfx.Pick(R, []uint32{S[6], S[5]})
					ps.Spawn(pixelfx.Grit, s.x+s.lean*u+sd*(s.w/2)*(1-u), -u*s.H, vx, vy, life, col, 1, 0, 0)
				}
				if s.main && s.up && t < s.split && t > s.at+0.25 && R.Chance(0.03*f) {
					px := s.x + R.Range(-4, 2)
					py := -R.Range(8, s.H*0.85)
					life := R.Range(0.25, 0.45)
					col := G[3]
					if R.Chance(0.5) {
						col = S[7]
					}
					sz := R.Range(0.8, 2)
					ps.Spawn(pixelfx.StarKind, px, py, 0, -2, life, col, sz, 0, 0)
				}
				// the stump sinking pushes up a last puff of dust
				if t > s.split+earthFall && t < s.split+earthFall+earthSink*0.8 && R.Chance(0.15*f) {
					px := s.x + R.Range(-s.w/2, s.w/2)
					py := -R.Range(0, 2)
					vx := R.Sign() * R.Range(20, 60)
					vy := -R.Range(0, 5)
					life := R.Range(0.4, 0.7)
					sz := R.Range(1.5, 3)
					ps.Spawn(pixelfx.Dust, px, py, vx, vy, life, DUST2, sz, 0, 0)
				}
			}
			bits.Update(dt)
			// stones that land hard kick up a little dust
			for i := range bits.List {
				b := &bits.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					for range 2 {
						px := b.X + R.Range(-1.5, 1.5)
						vx := R.Sign() * R.Range(8, 35)
						vy := -R.Range(2, 14)
						life := R.Range(0.3, 0.6)
						sz := R.Range(1.3, 2.6)
						ps.Spawn(pixelfx.Dust, px, -0.5, vx, vy, life, DUST2, sz, 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the cracks in the floor: a glowing seam that cools to a dark scar, then fades
			L, hk := open(), heat()
			fade := 1 - pixelfx.Smooth(earthSpikeDuration-1.1, earthSpikeDuration-0.3, t)
			for _, c := range cracks {
				m := max(2, int(math.Ceil(float64(len(c))*L)))
				for k := 1; k < m; k++ {
					x0, y0 := c[k-1][0], c[k-1][1]
					x1, y1 := c[k][0], c[k][1]
					p.Line(x0, y0+1, x1, y1+1, S[0], fade)
					col := S[1]
					switch {
					case hk > 0.65:
						col = G[4]
					case hk > 0.35:
						col = G[3]
					case hk > 0.12:
						col = G[2]
					case hk > 0.03:
						col = G[1]
					}
					p.Line(x0, y0, x1, y1, col, fade)
					if k%2 == 0 {
						p.Glow(x1, y1, 6, G[2], 0.3*hk)
					}
				}
			}
			if hk > 0 {
				p.Glow(0, -3, 26, G[2], 0.35*hk)
			}
			ps.Draw(p, pixelfx.LayerBack)
			for i := range spikes {
				if spikes[i].back {
					drawShard(p, &spikes[i])
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// before it breaks through, the floor heaves into a trembling mound
			if t < earthUp+0.02 {
				k := pixelfx.Smooth(0.06, earthUp, t)
				hm := 8 * k
				jx := math.Sin(t*95) * 0.8 * k
				if hm > 0.6 {
					p.Shade(-16, -hm-1, 16, 1, func(x, y float64, out *pixelfx.Out) uint32 {
						dx, dy := (x-jx)/15, y/hm
						d := dx*dx + dy*dy
						if y > 0.5 || d > 1.18 {
							return 0
						}
						switch {
						case d > 1:
							out.C = S[0]
						case dy < -0.72:
							out.C = S[6]
						case dx < -0.3:
							out.C = S[5]
						case dx > 0.35:
							out.C = S[3]
						default:
							out.C = S[4]
						}
						return 0
					})
				}
			}
			// slabs of floor shoved up around the base, sinking back as the spike crumbles
			if t >= earthUp {
				sink := pixelfx.Smooth(earthSplit+earthFall, earthSplit+earthFall+earthSink, t) * 7
				a := 1 - pixelfx.Smooth(earthSplit+earthFall, earthSplit+earthFall+earthSink, t)
				for _, sl := range slabs {
					g := pixelfx.OutBack(pixelfx.Clamp((t-earthUp)/0.1), 2)
					if a > 0 {
						drawRock(p, sl.x, -1.5+sink, sl.s*math.Min(1, g), sl.a*g, sl.pts, S, a, t-earthUp < 0.04)
					}
				}
			}
			for i := range spikes {
				if !spikes[i].back {
					drawShard(p, &spikes[i])
				}
			}
			m0 := &spikes[2]
			leak := pixelfx.Smooth(earthCrack, earthSplit, t) * (1 - pixelfx.Smooth(earthSplit, earthSplit+0.5, t))
			if leak > 0 {
				p.GlowLine(m0.x, -4, m0.x+m0.lean*0.6, -m0.H*0.6, 20, G[2], 0.22*leak)
			}
			for i := range bits.List {
				b := &bits.List[i]
				drawRock(p, b.X, b.Y, b.S, b.A, b.Data.([][2]float64), S, math.Min(1, b.Life/0.4), false)
			}
			ps.Draw(p, pixelfx.LayerFront)
			// the hero frame: a flare where the tip breaks through, light running up the spike
			m := &spikes[2]
			e := t - m.at
			if e > 0 && e < 0.2 {
				g := pixelfx.OutBack(pixelfx.Clamp(e/earthRise), 2.2)
				pixelfx.Sparkle(p, m.x+m.lean, -m.H*g, 10*(1-e/0.2), S[7], G[3], 0.5)
				p.GlowLine(m.x, 0, m.x+m.lean, -m.H*g, 10, G[3], 0.3*(1-e/0.2))
			}
			lights.Draw(p)
		},
	}
}
