// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/rocket.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the spark and fire-ball recipes from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Rocket is a steel rocket roaring along: a flickering jet with shock diamonds and a white-hot nozzle, a thick billowing smoke trail lit by the flame, sparks shed behind. It is registered as "projectiles/rocket".
var Rocket = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Rocket",
		Desc:     "A steel rocket roars along: a flickering jet with shock diamonds and a white-hot nozzle, a thick billowing smoke trail lit by the flame, sparks shed behind.",
		Tags:     []string{"projectile", "rocket", "missile", "smoke", "fire", "directional", "loop"},
		Size:     [2]int{288, 76},
		Origin:   [2]int{252, 42},
		Duration: rocketDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "metal", Default: pixelfx.Palette("#0f0d16", "#25243a", "#3e4058", "#61667f", "#8e97b0", "#c4cce0", "#f2f6ff")},
			{Name: "paint", Default: pixelfx.Palette("#3a0d14", "#7c1a22", "#bd2f2c", "#ec5a45", "#ff9b7e")},
			{Name: "smoke", Default: pixelfx.Palette("#26222e", "#3d3846", "#5a5463", "#7e7887", "#a7a1ad", "#d0cad2", "#f1ecee")},
			{Name: "speed", Default: pixelfx.Num(120)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newRocket,
}

func init() { pixelfx.Register("projectiles/rocket", Rocket) }

const (
	rocketDuration = 1.2
	rocketOriginX  = 252
	rocketNozzle   = -14.0 // the bell's exit: the jet starts here
	rocketJ0       = 22.0  // the jet's mean length
)

var (
	rocketOutline = pixelfx.Hex("#100d17")
	// rocketShip: a steel body lit from above with a specular line, a painted ogive nose and band, swept fins, a dark bell with a glowing lip
	rocketShip = pixelShape(-15, -9, 30, 19, func(set func(px, py int, ch byte)) {
		body := map[int]byte{-3: '4', -2: '6', -1: '5', 0: '4', 1: '3', 2: '2', 3: '1'}
		seam := map[int]byte{-3: '3', -2: '4', -1: '4', 0: '3', 1: '2', 2: '1', 3: '1'}
		paint := map[int]byte{-3: 'd', -2: 'e', -1: 'e', 0: 'd', 1: 'c', 2: 'b', 3: 'a'}
		for y := -3; y <= 3; y++ {
			for x := -11; x <= 5; x++ {
				set(x, y, body[y])
			}
		}
		for y := -3; y <= 3; y++ {
			set(-3, y, seam[y])
			set(-9, y, paint[y])
			set(-8, y, paint[y])
		}
		nose := [8][2]int{{6, 3}, {7, 3}, {8, 2}, {9, 2}, {10, 1}, {11, 1}, {12, 0}, {13, 0}}
		for _, nh := range nose {
			for y := -nh[1]; y <= nh[1]; y++ {
				set(nh[0], y, paint[y])
			}
		}
		set(7, -2, 'w')
		set(8, -2, 'w')
		set(9, -1, 'e')
		set(13, 0, 'e')
		// fins, the top one in the light, the bottom one in shadow; each leading edge catches a highlight
		fins := [4][2]int{{-13, -8}, {-13, -9}, {-13, -10}, {-13, -11}}
		for i, ab := range fins {
			for x := ab[0]; x <= ab[1]; x++ {
				topCh := byte('3')
				if x == ab[1] {
					topCh = '5'
				} else if i > 1 {
					topCh = '4'
				}
				botCh := byte('2')
				if x == ab[1] {
					botCh = '3'
				} else if i > 1 {
					botCh = '1'
				}
				set(x, -4-i, topCh)
				set(x, 4+i, botCh)
			}
		}
		// the bell: a throat and a flared exit, its lip glowing
		for y := -2; y <= 2; y++ {
			set(-12, y, pick(y < 0, byte('3'), byte('2')))
		}
		for y := -3; y <= 3; y++ {
			set(-13, y, pick(y < -1, byte('2'), byte('1')))
		}
		set(-14, -3, 'h')
		set(-14, 3, 'h')
		for y := -2; y <= 2; y++ {
			ay := y
			if ay < 0 {
				ay = -ay
			}
			set(-14, y, pick(ay < 2, byte('x'), byte('h')))
		}
	})
)

// rocketJet draws the cone of flame out of the bell in clean bands with shock diamonds down its middle.
func rocketJet(p *pixelfx.Painter, F []uint32, x0, y0, J, wob, fr float64) {
	var dia [4]float64
	for i := range 4 {
		dia[i] = 3.6 + float64(i)*4.3 + (pixelfx.Rand(fr, float64(i), 41)-0.5)*0.7
	}
	p.Shade(x0-J-2, y0-6, x0, y0+6, func(x, y float64, _ *pixelfx.Out) uint32 {
		u := x0 - x
		if u < 0 {
			return 0
		}
		q := u / J
		if q > 1.05 {
			return 0
		}
		ady, qq := math.Abs(y-y0), math.Min(q, 1)
		w := (2.6 + 2.4*math.Pow(qq, 0.7)) * (1 - qq*qq) * wob
		m := w - ady + (pixelfx.Rand(math.Floor(x)*7+math.Floor(y)*131, fr)-0.5)*0.9*math.Min(1, 0.25+q*2)
		if m < -0.5 {
			return 0
		}
		if m < 0.5 {
			return F[pick(q < 0.45, 3, 2)]
		}
		for i := range dia {
			if dia[i] > J*0.72 {
				break
			}
			fi := float64(i)
			d := math.Abs(u-dia[i])/(1.9-fi*0.15) + ady/(1.7-fi*0.25)
			if d < 1 {
				return pick(d < 0.55 && i < 3, F[8], F[7])
			}
		}
		if u < 1.6 && ady < 1.6 {
			return F[8]
		}
		lvl := (1 - ady/math.Max(0.6, w)) * (1 - math.Pow(qq, 1.5))
		switch {
		case lvl > 0.62:
			return pick(u < 3, F[7], F[5])
		case lvl > 0.38:
			return F[5]
		case lvl > 0.16:
			return F[4]
		}
		return F[3]
	})
}

func newRocket(o *pixelfx.Options) pixelfx.Instance {
	F, M, A, SM := pixelfx.Fit(o.Palette(), 9), pixelfx.Fit(o.Ramp("metal"), 7), pixelfx.Fit(o.Ramp("paint"), 5), pixelfx.Fit(o.Ramp("smoke"), 7)
	S, D, V := o.Seed(), float64(rocketDuration), o.Num("speed")
	left := float64(-rocketOriginX + 2)
	SP := [4]uint32{F[7], F[6], F[4], F[3]}
	warmRamp := []uint32{pixelfx.Mix(F[0], SM[0], 0.5), pixelfx.Mix(F[1], SM[1], 0.45), pixelfx.Mix(F[2], SM[2], 0.35), F[3], F[4], F[6], F[7]}
	shipPal := map[rune]uint32{
		'o': rocketOutline,
		'1': M[1], '2': M[2], '3': M[3], '4': M[4], '5': M[5], '6': M[6],
		'a': A[0], 'b': A[1], 'c': A[2], 'd': A[3], 'e': A[4],
		'w': pixelfx.White, 'h': F[5], 'x': F[8],
	}
	bob := func(tt float64) float64 { return pixelfx.Round(math.Sin((tt/D)*tau) * 0.62) }
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		fr, by := math.Floor(t*30+1e-6), bob(t)
		J := rocketJ0 * (0.84 + 0.32*pixelfx.Rand(fr, S, 1))
		wob := 0.92 + 0.16*pixelfx.Rand(fr, S, 2)
		fl := J / rocketJ0

		// smoke: puffs leave the tail of the flame, stream back, swell and rise, lit by the fire while young
		var list []cloudPuff
		beat(t, D, 66, S+1, func(r *pixelfx.Rng) float64 { return r.Range(1.1, 1.45) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			x, y, _, _ := wake(rocketNozzle-rocketJ0*r.Range(0.62, 0.8), bob(t-age)+r.Range(-1, 1), -r.Range(75, 105), r.Range(-20, 20), age, V, 3, 0)
			k := age / life
			rise := 0.5 * r.Range(6, 16) * age * age
			sway := math.Sin(age*2.3+r.Range(0, tau)) * age * 2
			r0 := r.Range(2.2, 3)
			var r1 float64
			if r.Chance(0.25) {
				r1 = r.Range(14, 19)
			} else {
				r1 = r.Range(8, 13)
			}
			rad := r0 + (r1-2.6)*(1-math.Exp(-2.2*age))
			if x-rad < left+1 {
				return
			}
			a := math.Min(1, age*16) * math.Pow(1-k, 1.25) * 0.68 * pixelfx.Smooth(left, left+40, x-rad)
			list = append(list, cloudPuff{
				x: x,
				y: y - rise + sway,
				r: rad,
				a: a,
				b: pixelfx.Clamp(0.95 - k*0.75 + r.Range(-0.08, 0.08)),
				w: math.Exp(-age * r.Range(3.5, 6)),
			})
		})
		cloud(p, list, SM, warmRamp, cloudOpts{dens: 1.3, lit: 1.5})
		// the smoke right behind the flame glows from inside
		p.Glow(rocketNozzle-rocketJ0-6, by, 26, F[4], 0.24*fl)

		// embers carried off in the smoke, cooling and winking out
		beat(t, D, 9, S+4, func(r *pixelfx.Rng) float64 { return r.Range(0.6, 1.1) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			k := 1 - age/life
			x, y, _, _ := wake(rocketNozzle-rocketJ0*0.6, bob(t-age), -r.Range(40, 120), r.Range(-40, 30), age, V, 2.5, -8)
			if x < left+2 || (k < 0.3 && pixelfx.Rand(fr, r.Int(0, 999)) < 0.5) {
				return
			}
			c := F[2]
			if k > 0.6 {
				c = F[6]
			} else if k > 0.3 {
				c = F[4]
			}
			p.Px(x, y, c)
			p.Glow(x, y, 3, c, 0.35*k)
		})

		// tongues of flame torn off the tip of the jet, cooling and shrinking as they fall behind
		beat(t, D, 30, S+3, func(r *pixelfx.Rng) float64 { return r.Range(0.08, 0.16) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			k := age / life
			x, y, _, _ := wake(rocketNozzle-rocketJ0*r.Range(0.6, 0.9), bob(t-age)+r.Range(-1.5, 1.5), -r.Range(10, 50), r.Range(-12, 12), age, V, 4, -30)
			idx := 7 - int(math.Floor(math.Pow(k, 0.7)*4))
			rad := r.Range(1.2, 2.2) * (1 - k*0.6)
			p.Circle(x, y, rad, F[idx-2], 1)
			p.Circle(x+rad*0.25, y-rad*0.1, rad*0.65, F[idx], 1)
			p.Glow(x, y, rad*2.4+2, F[idx], 0.2*(1-k))
		})

		// sparks shed behind, streaking back and dropping
		beat(t, D, 30, S+2, func(r *pixelfx.Rng) float64 { return r.Range(0.18, 0.45) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			x, y, vx, vy := wake(rocketNozzle-r.Range(2, rocketJ0*0.7), bob(t-age)+r.Range(-1.5, 1.5), -r.Range(20, 190), r.Range(-80, 50), age, V, 2.6, 160)
			if x > left+10 {
				spark(p, x, y, vx, vy, 1-age/life, SP, 0.45, 0.022, 9)
			}
		})
		beat(t, D, 8, S+6, func(r *pixelfx.Rng) float64 { return r.Range(0.5, 0.8) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			x, y, vx, vy := wake(rocketNozzle-r.Range(4, rocketJ0*0.8), bob(t-age)+r.Range(-1, 1), -r.Range(10, 80), r.Range(-60, 10), age, V, 1.8, 170)
			if x > left+10 {
				spark(p, x, y, vx, vy, 1-age/life, SP, 0.5, 0.022, 9)
			}
		})

		rocketJet(p, F, rocketNozzle, by+0.5, J, wob, fr+S*31)
		p.Sprite(rocketShip.rows, shipPal, rocketShip.x0, rocketShip.y0+by, 1, pixelfx.SpriteOpts{})

		// light: a warm pool round the tail that breathes with the jet, the flame's own glow, a white-hot nozzle
		p.Glow(rocketNozzle-10, by, 60, F[4], 0.28*fl)
		p.GlowLine(rocketNozzle-3, by+0.5, rocketNozzle-J*0.9, by+0.5, 11, F[4], 0.2)
		p.Glow(rocketNozzle-1, by+0.5, 8, F[7], 0.45)
		p.Glow(rocketNozzle+3, by+5, 12, F[4], 0.22*fl) // the flame lights the tail from below
	}}
}
