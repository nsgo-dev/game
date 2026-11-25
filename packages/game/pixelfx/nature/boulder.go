// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/boulder.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the impact (rocks thrown up that tumble and bounce and kick up dust where they land, gravel, dust rolling out along the floor and climbing, a ring on the floor, a white flash) from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Boulder is a spinning boulder that arcs right, smashes into the floor in a white flash and bursts into tumbling chunks, gravel and rolling dust. It is registered as "nature/boulder".
var Boulder = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Boulder",
		Desc:     "A spinning boulder arcs right shedding dust and pebbles, smashes into the floor in a white flash and bursts into tumbling chunks, gravel and rolling dust.",
		Tags:     []string{"nature", "earth", "rock", "projectile", "impact", "directional", "one-shot"},
		Size:     [2]int{264, 120},
		Origin:   [2]int{40, 104},
		Duration: boulderDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#100e12", "#1e1a22", "#302a36", "#463e4c", "#5e5464", "#7c7080", "#a0949e", "#cec4c8")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "size", Default: pixelfx.Num(11)},
			{Name: "seed", Default: pixelfx.Num(11)},
		},
	},
	New: newBoulder,
}

func init() { pixelfx.Register("nature/boulder", Boulder) }

const boulderDuration = 2.3

const (
	boulderX0 = 6.0
	boulderY0 = -26.0
	boulderVX = 220.0
	boulderVY = -170.0
	boulderG  = 860.0
)

// boulderSurface draws the boulder's own surface, turning with it: a few shallow pits, a mossy rim along one side.
func boulderSurface(p *pixelfx.Painter, x, y, r, a float64, S []uint32, M [3]uint32) {
	c, s := math.Cos(a), math.Sin(a)
	at := func(u, v float64) (float64, float64) {
		return x + (u*c-v*s)*r, y + (u*s+v*c)*r
	}
	for _, pt := range [2][2]float64{{0.42, 0.12}, {-0.3, 0.38}} {
		px, py := at(pt[0], pt[1])
		p.Px(px, py, S[3])
	}
	for k := 0; k < 9; k++ {
		g := 3.6 + (float64(k)/8)*1.3
		rr := 0.92
		if k%3 == 1 {
			rr = 0.78
		}
		px, py := at(math.Cos(g)*rr, math.Sin(g)*rr)
		mi := 1
		if k%3 == 1 {
			mi = 0
		} else if py < y {
			mi = 2
		}
		p.Px(px, py, M[mi])
	}
}

func newBoulder(o *pixelfx.Options) pixelfx.Instance {
	S := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	RAD := o.Num("size")
	DUST := pixelfx.Mix(S[6], pixelfx.Hex("#9a8f86"), 0.4)
	DUST2 := pixelfx.Mix(S[5], pixelfx.Hex("#7a7068"), 0.35)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: o.Ramp("sparks")})
	MOSSC := [3]uint32{pixelfx.Hex("#1f3a1e"), pixelfx.Hex("#3f7a2c"), pixelfx.Hex("#7cb83a")}
	bits := pixelfx.NewBits(floor)
	lights := &pixelfx.Lights{}
	shape := stoneShape(R, 8)
	tHit := (-boulderVY + math.Sqrt(boulderVY*boulderVY-2*boulderG*(boulderY0+RAD*0.8))) / boulderG
	xHit := boulderX0 + boulderVX*tHit
	t := 0.0
	hit, thrown := false, false
	at := func(tt float64) (float64, float64) {
		return boulderX0 + boulderVX*tt, boulderY0 + boulderVY*tt + 0.5*boulderG*tt*tt
	}
	rock := func(b pixelfx.Bit) {
		b.Data = stoneShape(R, 0)
		bits.Add(b)
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			f := math.Min(1, dt*120)
			if !thrown {
				thrown = true
				for range 6 {
					x := boulderX0 - R.Range(0, 6)
					y := boulderY0 + R.Range(-4, 4)
					vx := -R.Range(20, 70)
					vy := R.Range(-20, 20)
					life := R.Range(0.4, 0.7)
					sz := R.Range(2.5, 4.5)
					ps.Spawn(pixelfx.Dust, x, y, vx, vy, life, DUST, sz, 0, 0)
				}
				lights.Add(boulderX0, boulderY0, 30, pixelfx.White, 0.3, 0.1)
			}
			if t < tHit {
				// it sheds dust and the odd pebble as it flies
				x, y := at(t)
				if R.Chance(0.5 * f) {
					px := x - R.Range(2, 6)
					py := y + R.Range(-4, 4)
					vx := -R.Range(10, 40)
					vy := R.Range(-10, 10)
					life := R.Range(0.3, 0.6)
					sz := R.Range(2, 3.5)
					ps.Spawn(pixelfx.Dust, px, py, vx, vy, life, DUST, sz, 0, 0)
				}
				if R.Chance(0.12 * f) {
					px := x + R.Range(-4, 4)
					py := y + R.Range(-2, 4)
					vy := R.Range(-40, 20)
					life := R.Range(0.6, 0.9)
					c := pixelfx.Pick(R, []uint32{S[5], S[4]})
					sz := 1.0
					if R.Chance(0.3) {
						sz = 2
					}
					ps.Spawn(pixelfx.Grit, px, py, boulderVX*0.3, vy, life, c, sz, 0, 0)
				}
			}
			if !hit && t >= tHit {
				hit = true
				x, y := xHit, -RAD*0.6
				lights.Add(x, y, 36, pixelfx.White, 0.75, 0.1)
				lights.Add(x, y, 60, pixelfx.Hex("#ffb060"), 0.45, 0.5)
				// big chunks of the boulder, thrown up and on, spinning
				for range 9 {
					a := -pi/2 + R.Range(-1.2, 1.1)
					sp := R.Range(100, 240)
					bx := x + R.Range(-5, 5)
					by := y + R.Range(-5, 5)
					life := R.Range(1.3, 1.7)
					ang := R.Range(0, tau)
					va := R.Range(-14, 14)
					s := R.Range(2.8, 5.2)
					rock(pixelfx.Bit{Kind: pixelfx.Chunk, X: bx, Y: by, VX: math.Cos(a)*sp + 30, VY: math.Sin(a) * sp, G: 1, Drag: 0.7, Bounce: 0.35, Life: life, A: ang, VA: va, S: s})
				}
				for range 14 {
					a := -pi/2 + R.Range(-1.4, 1.4)
					sp := R.Range(120, 280)
					bx := x + R.Range(-4, 4)
					life := R.Range(1.1, 1.5)
					ang := R.Range(0, tau)
					va := R.Range(-18, 18)
					s := R.Range(1.4, 2.6)
					rock(pixelfx.Bit{Kind: pixelfx.Chunk, X: bx, Y: y, VX: math.Cos(a)*sp + 25, VY: math.Sin(a) * sp, G: 1, Drag: 0.8, Bounce: 0.3, Life: life, A: ang, VA: va, S: s})
				}
				for range 60 {
					u := R.Range(-1, 1)
					a := -pi/2 + u*1.1
					sp := R.Range(80, 300) * (1 - 0.3*math.Abs(u))
					life := R.Range(1, 1.8)
					c := pixelfx.Pick(R, []uint32{S[6], S[5], S[4], S[4], S[3]})
					sz := 1.0
					if R.Chance(0.4) {
						sz = 2
					}
					ps.Spawn(pixelfx.Grit, x+u*6, -1, math.Cos(a)*sp+30, math.Sin(a)*sp, life, c, sz, 0, 0)
				}
				for range 12 {
					a := -pi/2 + R.Range(-1.3, 1.3)
					sp := R.Range(150, 380)
					ps.Spawn(pixelfx.Spark, x, y, math.Cos(a)*sp+40, math.Sin(a)*sp, R.Range(0.15, 0.4), pixelfx.White, 1, 0, 0)
				}
				// dust rolling out both ways along the floor, and a cloud climbing off the hit
				for range 14 {
					sd := R.Sign()
					px := x + sd*R.Range(2, 10)
					py := -R.Range(1, 5)
					vx := sd*R.Range(40, 140) + 10
					vy := -R.Range(4, 20)
					life := R.Range(1.1, 1.8)
					c := DUST2
					if R.Chance(0.5) {
						c = DUST
					}
					sz := R.Range(4.5, 8)
					delay := R.Range(0, 0.05)
					ps.Spawn(pixelfx.Smoke, px, py, vx, vy, life, c, sz, delay, pixelfx.Big)
				}
				for range 6 {
					px := x + R.Range(-6, 6)
					py := -R.Range(4, 14)
					vx := R.Range(-15, 25)
					vy := -R.Range(30, 80)
					life := R.Range(1.2, 1.8)
					sz := R.Range(6, 9)
					delay := R.Range(0, 0.1)
					ps.Spawn(pixelfx.Smoke, px, py, vx, vy, life, DUST, sz, delay, pixelfx.Big)
				}
				for range 3 {
					px := x + R.Range(-8, 8)
					py := y + R.Range(-10, 4)
					life := R.Range(0.2, 0.4)
					sz := R.Range(1, 2)
					ps.Spawn(pixelfx.StarKind, px, py, 0, -4, life, S[7], sz, 0, 0)
				}
			}
			bits.Update(dt)
			for i := range bits.List {
				b := &bits.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					for range 2 {
						px := b.X + R.Range(-1.5, 1.5)
						vx := R.Sign() * R.Range(8, 35)
						vy := -R.Range(2, 12)
						life := math.Min(R.Range(0.3, 0.6), boulderDuration-0.05-t)
						sz := R.Range(1.3, 2.6)
						ps.Spawn(pixelfx.Dust, px, -0.5, vx, vy, life, DUST2, sz, 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			e := t - tHit
			// the scar it leaves: a dark dent with cracks running from it, fading at the end
			if e > 0 {
				fade := 1 - pixelfx.Smooth(boulderDuration-0.6, boulderDuration-0.15, t)
				w := 10 * math.Min(1, e/0.05)
				for x := math.Floor(-w); x <= w; x++ {
					d := math.Abs(x) / w
					col := S[1]
					if d < 0.6 {
						col = S[0]
					}
					p.Dot(xHit+x, 0, col, fade)
					if d < 0.5 {
						p.Dot(xHit+x, 1, S[1], fade*0.8)
					}
				}
				for _, sd := range [2]float64{-1, 1} {
					for k := 0; k < 4; k++ {
						y := 0.0
						if k&1 != 0 {
							y = 1
						}
						p.Dot(xHit+sd*(w+2+float64(k)*3), y, S[1], fade*(1-float64(k)/4))
					}
				}
				k := e / 0.45
				if k < 1 {
					pixelfx.Band(p, xHit, 0.5, 46*pixelfx.Ease.OutCubic(k), 0.18, math.Max(1, 3*(1-k)), 1.15-k, []uint32{S[4], S[6], pixelfx.Hex("#ffd27a"), pixelfx.White}, func(g float64) bool { return math.Sin(g) < 0 }, false)
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < tHit {
				x, y := at(t)
				sp := pixelfx.Hypot(boulderVX, boulderVY+boulderG*t)
				ux, uy := boulderVX/sp, (boulderVY+boulderG*t)/sp
				// speed lines trailing it
				for i := 0; i < 4; i++ {
					off := (float64(i) - 1.5) * RAD * 0.55
					nx, ny := -uy*off, ux*off
					L := 10.0 + float64(i%2)*6
					col := S[6]
					if i%2 != 0 {
						col = S[5]
					}
					p.Line(x-ux*(RAD+2)+nx, y-uy*(RAD+2)+ny, x-ux*(RAD+2+L)+nx, y-uy*(RAD+2+L)+ny, col, 0.8)
				}
				drawRock(p, x, y, RAD, t*13, shape, S, 1, false)
				boulderSurface(p, x, y, RAD*0.8, t*13, S, MOSSC)
				p.Glow(x, y, RAD*2, S[6], 0.12)
			}
			e := t - tHit
			if e >= 0 && e < 0.07 {
				// the hero frame: the boulder flashes white as it breaks, lines of force burst out
				drawRock(p, xHit, -RAD*0.8, RAD, tHit*13, shape, S, 1, true)
				pixelfx.Sparkle(p, xHit, -RAD*0.6, 12*(1-e/0.07), pixelfx.White, pixelfx.Hex("#ffd27a"), 0.6)
			}
			if e >= 0 && e < 0.14 {
				k := e / 0.14
				col := S[6]
				if k < 0.5 {
					col = pixelfx.White
				}
				for i := 0; i < 9; i++ {
					a := -pi + (float64(i)/8)*pi
					r0 := RAD + 4 + 16*k
					r1 := r0 + 8*(1-k)
					p.Line(xHit+math.Cos(a)*r0, -RAD*0.6+math.Sin(a)*r0, xHit+math.Cos(a)*r1, -RAD*0.6+math.Sin(a)*r1, col, 1)
				}
			}
			if e > 0 {
				k := e / 0.45
				if k < 1 {
					pixelfx.Band(p, xHit, 0.5, 46*pixelfx.Ease.OutCubic(k), 0.18, math.Max(1, 3*(1-k)), 1.15-k, []uint32{S[4], S[6], pixelfx.Hex("#ffd27a"), pixelfx.White}, func(g float64) bool { return math.Sin(g) >= 0 }, false)
				}
			}
			for i := range bits.List {
				b := &bits.List[i]
				drawRock(p, b.X, b.Y, b.S, b.A, b.Data.([][2]float64), S, math.Min(1, b.Life/0.4), false)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
