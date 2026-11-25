// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/blunt-hit.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the clods and the dust they kick up from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// BluntHit is a heavy, soft thud: a white-hot lumpy blob cools to cream in a warm pool of light, inked wedges jab out and snap back, a squashed ring of dust puffs swells and drifts, clods drop. It is registered as "impacts/blunt-hit".
var BluntHit = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Blunt hit",
		Desc:     "A heavy, soft thud: a white-hot lumpy blob cools to cream in a warm pool of light, inked wedges jab out and snap back, a squashed ring of dust puffs swells and drifts, clods drop.",
		Tags:     []string{"impact", "hit", "blunt", "punch", "club", "thump", "dust", "comic", "one-shot"},
		Size:     [2]int{128, 88},
		Origin:   [2]int{64, 72},
		Duration: 0.9,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1d1620", "#4a3a3c", "#7d6a66", "#b09c90", "#dccab4", "#f6ead2", "#ffffff")},
			{Name: "warm", Default: pixelfx.Color("#ffc98a")},
			{Name: "dust", Default: pixelfx.Color("#b7a8a6")},
			{Name: "clod", Default: pixelfx.Color("#6e5a5a")},
			{Name: "height", Default: pixelfx.Num(22)},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newBluntHit,
}

func init() { pixelfx.Register("impacts/blunt-hit", BluntHit) }

// bluntLump is a lumpy outline: the radius at angle a of a blob of radius r.
func bluntLump(r, a, ph float64) float64 {
	return r * (1 + 0.15*math.Sin(5*a+ph) + 0.08*math.Sin(9*a+ph*2.3))
}

// bluntWedge is an inked wedge that jabs out at its own length and pace, then snaps back.
type bluntWedge struct{ a, len, w, r0, t0, out, back float64 }

// bluntPuff is a soft puff of dust.
type bluntPuff struct{ x, y, vx, vy, r, life, max, t0, lift float64 }

func newBluntHit(o *pixelfx.Options) pixelfx.Instance {
	C, R, X, Y := pixelfx.Fit(o.Palette(), 7), pixelfx.NewRng(o.Seed()), 0.0, -o.Num("height")
	PH := R.Range(0, pixelfx.Tau)
	floor := func(float64) float64 { return 0 }
	WARM, DUST, st := o.Color("warm"), o.Color("dust"), stoneOf(o.Color("clod"))
	lights, bits := &pixelfx.Lights{}, pixelfx.NewBits(floor)
	// a warm pool of light, a white-hot heart, and a glow on the floor below
	lights.Add(X, Y, 48, WARM, 0.6, 0.4)
	lights.Add(X, Y, 16, pixelfx.White, 0.6, 0.07)
	lights.Add(X, -2, 36, WARM, 0.25, 0.4)
	// inked wedges: each jabs out at its own length and pace, then snaps back
	wedges := make([]bluntWedge, 9)
	for i := range wedges {
		var w bluntWedge
		w.a = (float64(i)/9)*pixelfx.Tau + R.Range(-0.2, 0.2)
		w.len = R.Range(6, 15)
		w.w = R.Range(3.2, 5)
		w.r0 = R.Range(13, 16)
		w.t0 = R.Range(0.015, 0.04)
		w.out = R.Range(0.04, 0.06)
		w.back = R.Range(0.09, 0.14)
		wedges[i] = w
	}
	// the squash ring: soft puffs flung out from the blob's rim, wider than tall, each its own size, pace and life
	puffs := make([]bluntPuff, 0, 35)
	for i := range 30 {
		a := (float64(i)/30)*pixelfx.Tau + R.Range(-0.12, 0.12)
		side := math.Abs(math.Cos(a))
		v := R.Range(40, 140) * (0.6 + 0.5*side)
		var q bluntPuff
		q.x, q.y = X+math.Cos(a)*9, Y+math.Sin(a)*6
		q.vx, q.vy = math.Cos(a)*v, math.Sin(a)*v*0.5
		q.r = R.Range(2.4, 6.2) * (0.75 + 0.35*side)
		q.life = R.Range(0.35, 0.85)
		q.t0 = R.Range(0.03, 0.1)
		q.lift = R.Range(4, 16)
		puffs = append(puffs, q)
	}
	// and a few that billow up off the top and hang longer
	for range 5 {
		var q bluntPuff
		q.x = X + R.Range(-6, 6)
		q.y = Y - R.Range(3, 6)
		q.vx = R.Range(-30, 30)
		q.vy = -R.Range(20, 45)
		q.r = R.Range(3.5, 5.5)
		q.life = R.Range(0.6, 0.85)
		q.t0 = R.Range(0.05, 0.1)
		q.lift = R.Range(14, 24)
		puffs = append(puffs, q)
	}
	for i := range puffs {
		puffs[i].max = puffs[i].life
	}
	// heavier clods knocked loose: they arc out, drop to the floor, bounce once and settle
	for range 5 {
		s := R.Sign()
		a := -pi/2 + s*R.Range(0.5, 1.3)
		v := R.Range(70, 140)
		var b pixelfx.Bit
		b.Kind = pixelfx.Chunk
		b.X = X + s*R.Range(4, 8)
		b.Y = Y + R.Range(-3, 3)
		b.VX, b.VY = math.Cos(a)*v, math.Sin(a)*v
		b.G, b.Drag, b.Bounce = 1, 0.6, 0.28
		b.Life = R.Range(0.65, 0.85)
		b.A = R.Range(0, pixelfx.Tau)
		b.VA = s * R.Range(8, 16)
		b.S = 1
		if R.Chance(0.4) {
			b.S = 2
		}
		b.Pts = rockShape(R, 6)
		bits.Add(b)
	}
	var kicks []bluntPuff

	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := range puffs {
				q := &puffs[i]
				if t < q.t0 {
					continue
				}
				q.life -= dt
				q.vx -= q.vx * 4.5 * dt
				// they slow, and lift a little as they thin
				q.vy -= q.vy*4.5*dt + q.lift*dt
				q.x += q.vx * dt
				q.y += q.vy * dt
			}
			for i := range bits.List {
				bits.List[i].PVY = bits.List[i].VY
			}
			bits.Update(dt)
			// a clod hitting the floor kicks a little dust
			for i := range bits.List {
				b := &bits.List[i]
				if b.PVY > 60 && b.VY <= 0 {
					for range 2 {
						vx := R.Sign() * R.Range(8, 22)
						vy := -R.Range(3, 10)
						kicks = append(kicks, bluntPuff{x: b.X, y: -1, vx: vx, vy: vy, r: R.Range(1.2, 2), life: 0.4, max: 0.4})
					}
					b.PVY = 0
				}
			}
			for i := range kicks {
				q := &kicks[i]
				q.life -= dt
				q.vx -= q.vx * 4 * dt
				q.vy -= q.vy * 4 * dt
				q.x += q.vx * dt
				q.y += q.vy * dt
			}
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// dust: each puff swells and thins, warm-lit at first, its top catching the light
			puff := func(q *bluntPuff) {
				k := q.life / q.max
				if t < q.t0 || k <= 0 {
					return
				}
				r := q.r * (0.7 + (1-k)*0.85)
				a := math.Pow(k, 1.25) * math.Min(1, (t-q.t0)/0.03)
				c := pixelfx.Mix(DUST, WARM, pixelfx.Clamp(1-(t-0.03)/0.25)*0.55)
				p.DiscFn(q.x, q.y, r, func(d, _ float64) (uint32, float64) { return c, 0.58 * a * (1 - d*d*0.75) })
				lit := pixelfx.Mix(c, pixelfx.White, 0.55)
				p.DiscFn(q.x-r*0.22, q.y-r*0.32, r*0.58, func(d, _ float64) (uint32, float64) { return lit, 0.48 * a * (1 - d*d) })
			}
			for i := range puffs {
				puff(&puffs[i])
			}
			for i := range kicks {
				puff(&kicks[i])
			}
			for i := range bits.List {
				rock(p, &bits.List[i], st)
			}
			// the blob: white-hot and lumpy for a frame, then cream and a little bigger, then gone into the dust
			if t < 0.058 {
				f, R0, sq := 1.0, 13.5, 1.25
				if t < 0.025 {
					f, R0, sq = 0, 11.5, 1.1
				}
				p.Shade(X-R0*1.4, Y-R0*1.4, X+R0*1.4, Y+R0*1.4, func(x, y float64, _ *pixelfx.Out) uint32 {
					dx, dy := x-X, (y-Y)*sq
					d, ro := pixelfx.Hypot(dx, dy), bluntLump(R0, math.Atan2(dy, dx), PH+f*0.6)
					if d > ro {
						return 0
					}
					q := d / ro
					if f == 0 {
						switch {
						case q < 0.55:
							return pixelfx.White
						case q < 0.86:
							return C[5]
						}
						return C[4]
					}
					switch {
					case q < 0.3:
						return pixelfx.White
					case q < 0.72:
						return C[5]
					case q < 0.9:
						return C[4]
					}
					return C[3]
				})
			}
			// inked wedges jab out from the blob and snap back
			for _, w := range wedges {
				u := t - w.t0
				if u < 0 || u > w.out+w.back {
					continue
				}
				s := 1 - pixelfx.Ease.InQuad((u-w.out)/w.back)
				if u < w.out {
					s = pixelfx.Ease.OutCubic(u / w.out)
				}
				L := w.len * s
				if L < 1.5 {
					continue
				}
				ca, sa, r0, hw := math.Cos(w.a), math.Sin(w.a), w.r0+2*s, (w.w*(0.6+0.4*s))/2
				tipX, tipY := X+ca*r0, Y+sa*r0
				endX, endY := X+ca*(r0+L), Y+sa*(r0+L)
				nX, nY := -sa*hw, ca*hw
				wedge := func(g float64) [][2]float64 {
					return [][2]float64{
						{tipX - ca*g, tipY - sa*g},
						{endX + nX*(1+g*0.45) + ca*g, endY + nY*(1+g*0.45) + sa*g},
						{endX - nX*(1+g*0.45) + ca*g, endY - nY*(1+g*0.45) + sa*g},
					}
				}
				p.Poly(wedge(1), C[1], 1)
				c := C[5]
				if u < w.out+0.02 {
					c = pixelfx.White
				}
				p.Poly(wedge(0), c, 1)
			}
			if pop := math.Exp(-t / 0.06); pop > 0.02 {
				p.Glow(X, Y, 28, WARM, 0.5*pop)
			}
			lights.Draw(p)
		},
	}
}
