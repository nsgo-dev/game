// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the tongue, lick, puff, smoke, ember and spark recipes from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Shared by the fire effects: flame tongues, licks, flame puffs that cool into smoke and embers, with their motion in closed form so looping effects can draw them from t alone and stay seamless.

// pi and tau are variables so expressions combining them with constants round step by step as JavaScript's do, rather than folding exactly at compile time.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

// pnoise is smooth value noise in [-1, 1] that repeats every period cells (a whole number).
func pnoise(x, period, seed float64) float64 {
	i := math.Floor(x)
	f := x - i
	u := f * f * (3 - 2*f)
	a, b := pixelfx.Rand(pixelfx.Wrap(i, period), seed), pixelfx.Rand(pixelfx.Wrap(i+1, period), seed)
	return (a+(b-a)*u)*2 - 1
}

// noiseLoop is noise for a loop: N(tt, rate, seed, off) wobbles about rate times a second.
type noiseLoop func(tt, rate, seed, off float64) float64

// noiseFor returns noise for a loop D seconds long, the same at tt and tt + D; D = 0 gives plain noise, for one-shots.
func noiseFor(D float64) noiseLoop {
	if D == 0 {
		return func(tt, rate, seed, off float64) float64 { return pixelfx.Noise(tt*rate+off, seed) }
	}
	return func(tt, rate, seed, off float64) float64 {
		P := math.Max(1, pixelfx.Round(rate*D))
		return pnoise((tt/D)*P+off, P, seed)
	}
}

// looped runs seamless particles for a loop D seconds long: count particles, each born at its own phase and reborn every D. make(r, i) builds particle i from its own generator (the same every frame) and returns it with its life (which may be longer than D); draw(q, age, life, i) is called for every live copy.
func looped[Q any](t, D float64, count int, seed float64, make func(r *pixelfx.Rng, i int) (Q, float64), draw func(q *Q, age, life float64, i int)) {
	for i := range count {
		r := pixelfx.RngOf(seed, float64(i))
		born := r.Next() * D
		q, life := make(r, i)
		for age := pixelfx.Wrap(t-born, D); age < life; age += D {
			draw(&q, age, life, i)
		}
	}
}

// hotFirst returns a fire ramp hot-first (H[0] white .. H[8] smouldering) from a palette dark to bright, any length.
func hotFirst(F []uint32) []uint32 {
	H := slices.Clone(F)
	slices.Reverse(H)
	return H
}

// layer is one layer of a flame tongue: its share of the width and the height, and its step of the hot-first ramp.
type layer struct {
	w, h float64
	ri   int
}

var (
	// layers are the tongue's layers: red rim, orange, yellow, white heart.
	layers = []layer{{1, 1, 6}, {0.76, 0.86, 4}, {0.54, 0.68, 2}, {0.3, 0.42, 0}}
	// bigLayers are for big tongues: more orange and a smaller white heart, so the light doesn't wash them out.
	bigLayers = []layer{{1, 1, 6}, {0.8, 0.9, 5}, {0.62, 0.78, 4}, {0.44, 0.6, 3}, {0.26, 0.4, 1}, {0.12, 0.22, 0}}
	// backLayers are the same layers a step cooler, for flames further back.
	backLayers = []layer{{1, 1, 7}, {0.76, 0.86, 5}, {0.54, 0.68, 3}, {0.3, 0.42, 2}}
)

// flameTongue draws a flame tongue standing at (x, y), h tall, w its half-width at the base, its tip swept sx px sideways: a red rim round orange round yellow round a white heart, every row's width and centre jittered by noise so the edges crackle. N(rate, seed, off) is the effect's noise at this frame; n seeds this tongue. jit is how far the edges crackle (px), wob how far the tip wanders (px).
func flameTongue(p *pixelfx.Painter, x, y, h, w, sx float64, N func(rate, seed, off float64) float64, n float64, H []uint32, ls []layer, jit, wob float64) {
	if h < 1 {
		return
	}
	for _, l := range ls {
		L, c := h*l.h, H[l.ri]
		for r := 0.0; r <= L; r++ {
			u := r / math.Max(1, L)
			half := w*l.w*math.Pow(1-u, 0.85)*math.Min(1, (r+1.5)/3.5) + N(9, n, r*0.45)*jit
			if half < 0.3 {
				continue
			}
			cx, Y := x+sx*u*u*l.h+N(4, n+7, r*0.2)*u*wob, pixelfx.Round(y-r)
			for px, x1 := pixelfx.Round(cx-half), pixelfx.Round(cx+half); px <= x1; px++ {
				p.Px(px, Y, c)
			}
		}
	}
}

// flameBlade draws the same tongue as flameTongue lying along any direction (ux, uy) from its root (x, y): for jets, breath and comet tails. ln is its length, w its half-width at the root, bend how far (px) its tip is swept sideways, to the left of the direction.
func flameBlade(p *pixelfx.Painter, x, y, ux, uy, ln, w, bend float64, N func(rate, seed, off float64) float64, n float64, H []uint32, ls []layer, jit, wob float64) {
	if ln < 1 {
		return
	}
	nx, ny := -uy, ux
	for _, l := range ls {
		L, c := ln*l.h, H[l.ri]
		for r := 0.0; r <= L; r += 0.5 {
			u := r / math.Max(1, L)
			half := w*l.w*math.Pow(1-u, 0.85)*math.Min(1, (r+1.5)/3.5) + N(9, n, r*0.45)*jit
			if half < 0.3 {
				continue
			}
			off := bend*u*u*l.h + N(4, n+7, r*0.2)*u*wob
			cx, cy := x+ux*r+nx*off, y+uy*r+ny*off
			for q := -half; q <= half; q += 0.5 {
				p.Px(cx+nx*q, cy+ny*q, c)
			}
		}
	}
}

// solid draws a solid disc straight in screen space (fast; a disc looks the same mirrored or turned).
func solid(p *pixelfx.Painter, x, y, r float64, c uint32) {
	if r < 0.6 {
		p.Px(x, y, c)
		return
	}
	cx, cy, r2 := p.ToX(x, y), p.ToY(x, y), r*r
	for sy := math.Floor(cy - r); sy <= math.Ceil(cy+r); sy++ {
		dy := sy + 0.5 - cy
		s2 := r2 - dy*dy
		if s2 < 0 {
			continue
		}
		s := math.Sqrt(s2)
		for sx := math.Ceil(cx - s - 0.5); sx <= math.Floor(cx+s-0.5); sx++ {
			p.Put(int(sx), int(sy), c, 1)
		}
	}
}

// soft draws a translucent disc in screen space: smoke and haze.
func soft(p *pixelfx.Painter, x, y, r float64, c uint32, a float64) {
	if a <= 0.004 {
		return
	}
	if r < 0.5 {
		p.Blend(x, y, c, a)
		return
	}
	cx, cy, r2 := p.ToX(x, y), p.ToY(x, y), r*r
	for sy := math.Floor(cy - r); sy <= math.Ceil(cy+r); sy++ {
		dy := sy + 0.5 - cy
		s2 := r2 - dy*dy
		if s2 < 0 {
			continue
		}
		s := math.Sqrt(s2)
		for sx := math.Ceil(cx - s - 0.5); sx <= math.Floor(cx+s-0.5); sx++ {
			p.BlendPx(int(sx), int(sy), c, a)
		}
	}
}

// firePuff draws a ball of flame with life left k (1 new, 0 gone) cooling down the hot-first ramp H as it ages: a darker rim, a lit core up and to the left, a little tongue on top, a hot heart while it's young, and its light. frac (0..1) nudges how hot it starts; (tx, ty) is which way its tongue points (pixelfx's defaults are glowK 0.18 and (0, -1)).
func firePuff(p *pixelfx.Painter, x, y, k, size, frac float64, H []uint32, glowK, tx, ty float64) {
	age, n := 1-k, len(H)
	heat := math.Min(0.999, math.Pow(age, 0.62)+frac*0.1)
	idx := int(math.Min(float64(n-2), math.Floor(heat*float64(n-1))))
	r := size * (0.55 + 0.45*math.Min(1, age*7)) * (1 - age*0.55)
	c := H[idx]
	if idx >= n-2 {
		soft(p, x, y, r, c, math.Min(1, k*3)*0.85)
		return
	}
	solid(p, x, y, r, H[min(n-1, idx+2)])
	if r > 1.6 && ty > -0.99 {
		solid(p, x+tx*r*1.5, y+ty*r*1.5, r*0.3, H[min(n-1, idx+1)])
	}
	solid(p, x-r*0.16, y-r*0.22, r*0.78, c)
	if r > 1.6 {
		solid(p, x+tx*r*0.9, y+ty*r*0.9, r*0.42, c)
	}
	if idx <= 3 && r > 1.8 {
		solid(p, x-r*0.28, y-r*0.34, r*0.36, H[max(0, idx-1)])
	}
	if idx < 6 {
		p.Glow(x, y, r*2.4+2, c, glowK*k)
	}
}

// lick draws a blob of flame torn off a tongue's tip, a red rim round a yellow heart, shrinking as it rises.
func lick(p *pixelfx.Painter, x, y, k, s float64, H []uint32) {
	r := s * (0.4 + 0.6*k)
	rim := H[6]
	if k > 0.5 {
		rim = H[5]
	}
	solid(p, x, y, r+0.6, rim)
	if k > 0.35 {
		heart := H[4]
		if k > 0.7 {
			heart = H[2]
		}
		solid(p, x, y-0.3, r*0.6, heart)
	}
}

func rampAt(r []uint32, t float64) uint32 {
	return r[int(math.Min(float64(len(r)-1), math.Max(0, math.Floor(t*float64(len(r))))))]
}

// smokePuff draws a puff of smoke with life left k: three soft discs (a body, a denser core sitting low, a lit cap while it's young), swelling and thinning. col tints it (0 for plain smoke); big makes slower, fuller smoke; aMul scales its alpha.
func smokePuff(p *pixelfx.Painter, x, y, k, size float64, col uint32, big bool, aMul float64) {
	age := 1 - k
	grow, alpha := 0.4, 0.58
	if big {
		grow, alpha = 0.5, 0.7
	}
	r := size * (0.65 + math.Min(1, age*2.5)*0.45 + age*grow)
	a := math.Min(1, age*12) * math.Min(1, k*2.4) * alpha * aMul
	var c uint32
	if col != 0 {
		c = pixelfx.Mix(col, rampAt(pixelfx.SmokeRamp, age), age)
	} else {
		c = rampAt(pixelfx.SmokeRamp, age*1.05)
	}
	soft(p, x, y, r, c, a*0.45)
	soft(p, x+r*0.05, y+r*0.06, r*0.74, c, a*0.62)
	if age < 0.5 {
		soft(p, x-r*0.22, y-r*0.3, r*0.5, pixelfx.Mix(c, pixelfx.White, 0.12), a*0.4*(1-age*2))
	}
}

// ember draws an ember with life left k: a hot pixel cooling through cols [hot, warm, cool, rim], flickering out near the end (ph: its flicker phase), with its own light. Big ones (size 2) are a little cross with a darker rim.
func ember(p *pixelfx.Painter, x, y, k, size float64, cols [4]uint32, ph float64) {
	if k < 0.25 && math.Sin(ph) < 0 {
		return
	}
	c := cols[2]
	if k > 0.6 {
		c = cols[0]
	} else if k > 0.3 {
		c = cols[1]
	}
	if size >= 2 {
		rim := cols[3]
		if k > 0.45 {
			rim = cols[2]
		}
		p.Px(x-1, y, rim)
		p.Px(x+1, y, rim)
		p.Px(x, y-1, rim)
		p.Px(x, y+1, rim)
		p.Px(x, y, c)
		p.Glow(x, y, 8, c, 0.4*k)
		return
	}
	p.Px(x, y, c)
	if size > 1.4 {
		p.Px(x, y+1, cols[3])
	}
	p.Glow(x, y, 3+size, c, 0.35*k)
}

// spark draws a spark: a streak stretched along its velocity, a hot head and a cooler tail (cols: hot, warm, cool, tail), and a point of light.
func spark(p *pixelfx.Painter, x, y, vx, vy, k float64, cols [4]uint32) {
	sp := pixelfx.Hypot(vx, vy)
	if sp == 0 {
		sp = 1
	}
	ux, uy, ln := vx/sp, vy/sp, math.Min(9, 1+sp*0.022)
	head := cols[2]
	if k > 0.6 {
		head = cols[0]
	} else if k > 0.3 {
		head = cols[1]
	}
	tail := cols[3]
	if k > 0.5 {
		tail = head
	}
	p.Line(x-ux*ln, y-uy*ln, x-ux*ln*0.4, y-uy*ln*0.4, tail, 1)
	p.Line(x-ux*ln*0.45, y-uy*ln*0.45, x, y, head, 1)
	p.Glow(x, y, 3, head, 0.45*k)
}

func along(x, v, a, k, t float64) (float64, float64) {
	if k <= 0 {
		return x + v*t + 0.5*a*t*t, v + a*t
	}
	e, vt := math.Exp(-k*t), a/k
	return x + vt*t + ((v-vt)*(1-e))/k, vt + (v-vt)*e
}

// drift returns where a body is t seconds after launch from (x, y) at (vx, vy), under acceleration (ax, ay) px/s² and linear drag kx, ky (1/s): x, y, vx, vy. The particle kinds' own numbers: Fire (0, -95, 5, 3.2), Smoke (0, -16, 1.6, 1.2), Ember (0, -30, 1.6, 1.6), Spark (0, 427, 2.2, 2.2).
func drift(x, y, vx, vy, t, ax, ay, kx, ky float64) (float64, float64, float64, float64) {
	X, VX := along(x, vx, ax, kx, t)
	Y, VY := along(y, vy, ay, ky, t)
	return X, Y, VX, VY
}

// sparkAt returns where a spark is t seconds after launch from (x, y) at (vx, vy): it falls under g px/s², drags by k (1/s) and bounces up to three times off the floor at y = floor, losing speed each time (pixelfx's defaults are floor 0, g 427 and k 2.2). It returns x, y, vx, vy.
func sparkAt(x, y, vx, vy, t, floor, g, k float64) (float64, float64, float64, float64) {
	s, left := [4]float64{x, y, vx, vy}, t
	at := func(tt float64) [4]float64 {
		X, Y, VX, VY := drift(s[0], s[1], s[2], s[3], tt, 0, g, k, k)
		return [4]float64{X, Y, VX, VY}
	}
	for range 3 {
		e := at(left)
		if e[1] < floor || s[1] >= floor {
			return e[0], e[1], e[2], e[3]
		}
		lo, hi := 0.0, left
		for range 16 {
			m := (lo + hi) / 2
			if at(m)[1] < floor {
				lo = m
			} else {
				hi = m
			}
		}
		c := at(hi)
		s = [4]float64{c[0], floor - 0.25, c[2] * 0.7, -math.Abs(c[3]) * 0.45}
		left -= hi
	}
	e := at(left)
	if e[1] < floor {
		return e[0], e[1], e[2], e[3]
	}
	return e[0], floor - 0.25, e[2], 0
}

// floorLight draws a pool of light on the floor: an ellipse of glow of color c, k at its middle falling to 0 at its rim.
func floorLight(p *pixelfx.Painter, x, y, rx, ry float64, c uint32, k float64) {
	if k <= 0.004 || rx < 1 || ry < 0.5 {
		return
	}
	p.Shade(x-rx, y-ry, x+rx, y+ry, func(px, py float64, out *pixelfx.Out) uint32 {
		dx, dy := (px-x)/rx, (py-y)/ry
		d := dx*dx + dy*dy
		if d >= 1 {
			return 0
		}
		f := 1 - math.Sqrt(d)
		out.L = c
		out.K = f * f * k
		return 0
	})
}

// simDraw draws a core particle system with its fire recolored down the hot-first ramp H as flame puffs (the core draws fire in its own colors): for layer LayerBack its soft and back particles and then the puffs flagged Back, for LayerFront the other puffs and then the rest on top, for LayerBoth both.
func simDraw(p *pixelfx.Painter, ps *pixelfx.Particles, H []uint32, layer pixelfx.Layer) {
	all := ps.List
	rest, fire := make([]pixelfx.Particle, 0, len(all)), make([]pixelfx.Particle, 0, len(all))
	for _, q := range all {
		if q.K == pixelfx.Fire {
			fire = append(fire, q)
		} else {
			rest = append(rest, q)
		}
	}
	puffs := func(back bool) {
		for i := range fire {
			q := &fire[i]
			if q.Delay <= 0 && (q.Flag&pixelfx.Back != 0) == back {
				firePuff(p, q.X, q.Y, q.Life/q.Max, q.Size, math.Mod(q.Seed, 1), H, 0.18, 0, -1)
			}
		}
	}
	ps.List = rest
	if layer != pixelfx.LayerFront {
		ps.Draw(p, pixelfx.LayerBack)
		puffs(true)
	}
	if layer != pixelfx.LayerBack {
		puffs(false)
		ps.Draw(p, pixelfx.LayerFront)
	}
	ps.List = all
}

// stoneCols are the shades of a stone color: its lit side, body, shaded side and dust.
type stoneCols struct{ light, body, shade, dust uint32 }

func stoneOf(c uint32) stoneCols {
	return stoneCols{
		light: pixelfx.Mix(pixelfx.Scale(c, 1.35), pixelfx.White, 0.08),
		body:  c,
		shade: pixelfx.Scale(c, 0.64),
		dust:  pixelfx.Mix(pixelfx.Scale(c, 1.2), pixelfx.RGB(150, 140, 150), 0.3),
	}
}

// rock draws a thrown rock: a lit side, a shaded side and an outline, spinning, fading out over its last half second.
func rock(p *pixelfx.Painter, b *pixelfx.Bit, st stoneCols) {
	r, fade := 0.6+b.S*0.75, math.Min(1, b.Life/0.5)
	lx, ly := math.Cos(b.A)*r*0.4, math.Sin(b.A)*r*0.4
	p.Shade(b.X-r-1, b.Y-r-1, b.X+r+1, b.Y+r+1, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x-b.X, y-b.Y
		d := pixelfx.Hypot(dx*1.15, dy)
		if d > r+0.9 {
			return 0
		}
		lit := dx*lx + dy*ly
		switch {
		case d > r:
			out.C = outline
		case lit < -0.4:
			out.C = st.light
		case lit > 0.6:
			out.C = st.shade
		default:
			out.C = st.body
		}
		out.A = fade
		return 0
	})
}

var (
	soot, soot2, outline = pixelfx.Hex("#0d0a12"), pixelfx.Hex("#17121d"), pixelfx.Hex("#1a1422")
	warm                 = pixelfx.RGB(255, 176, 96)
)

var smoke1 = pixelfx.Hex("#5b5566")

// tongue is one flame tongue of a burn: where it stands from the base, its share of the fire's height, its half-width, and whether it is in the back row.
type tongue struct {
	dx, dy, h, w float64
	back         bool
}

// burnOpts are a burn's settings, every one given (pixelfx's defaults are in brackets): H the hot-first ramp, D the loop's length, NL its noise, S the seed, X and Y the base [0, 0], Height, Lean [0.1], Spread how wide embers and smoke leave [6], rates per second of Puffs [8] and Licks [4] per tongue and of Smoke [12] and Embers [8], SmokeSize [3], SmokeLife [1.6, 2.6], Wind [3], Glow the light's strength [1] and GlowR its reach [46], and Mid, drawn after the tongues and before what flies off them: what the fire burns out of.
type burnOpts struct {
	H                           []uint32
	D                           float64
	NL                          noiseLoop
	S                           float64
	X, Y, Height, Lean, Spread  float64
	Puffs, Licks, Smoke, Embers float64
	SmokeSize                   float64
	SmokeLife                   [2]float64
	Wind, Glow, GlowR           float64
	Mid                         func(fl float64)
}

type burnTongue struct {
	dx, y, h, w, env, n float64
	back                bool
}

// burn draws a fire for loops from t alone: tongues T from the base, back row first then outer to inner; licks torn off their tips, flame puffs that burn out into smoke, a smoke plume curling off with the draught, embers, and the fire's light. Wide tongues get bigLayers. It returns this frame's flicker.
func burn(p *pixelfx.Painter, t float64, T []tongue, o burnOpts) float64 {
	H, D, NL, S := o.H, o.D, o.NL, o.S
	HC, X, Y, W, G := o.Height, o.X, o.Y, o.Spread, o.Glow
	N := func(rate, seed, off float64) float64 { return NL(t, rate, seed, off) }
	fla := func(tt float64) float64 { return 0.74 + 0.16*NL(tt, 7.3, S+1, 0) + 0.1*NL(tt, 19, S+2, 0) }
	fl := fla(t)
	SMOKE := pixelfx.Mix(smoke1, H[6], 0.25)
	EMB := [4]uint32{H[1], H[2], H[4], H[6]}
	GLOW := pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.25)
	R := pixelfx.RngOf(S, 77)
	count := func(rate float64) int { return int(pixelfx.Round(rate * D)) }
	TT := make([]burnTongue, len(T))
	for i, tg := range T {
		dx := tg.dx + R.Range(-0.3, 0.3)
		h := 2 + HC*tg.h*R.Range(0.92, 1.08)
		w := tg.w * R.Range(0.92, 1.08)
		TT[i] = burnTongue{dx: dx, y: tg.dy, h: h, w: w, back: tg.back, env: tg.h, n: (S*7 + float64(i)) * 13}
	}
	lean := func(tt float64) float64 { return NL(tt, 0.67, S+90, 0) * o.Lean }
	height := func(i int, tt float64) float64 {
		h, n := TT[i].h, TT[i].n
		leap := math.Max(0, NL(tt, 2.6, n+70, 0)-0.55) * 1.4
		return (h + 0.25*h*NL(tt, 3.3, n, 0) + h*leap) * (0.9 + 0.1*fla(tt))
	}
	sweep := func(i int, tt, h float64) float64 {
		return NL(tt, 2.2, TT[i].n+40, 0)*(1+h*0.08) + lean(tt)*h*0.9 - pixelfx.ClampTo(TT[i].dx*0.3, -2.5, 2.5)
	}
	tip := func(i int, tt float64) (float64, float64) {
		h := height(i, tt)
		return X + TT[i].dx + sweep(i, tt, h)*0.85, Y + TT[i].y - h*0.88 + 3
	}
	tongueAt := func(i int) {
		tg := TT[i]
		h := height(i, t)
		ls := layers
		switch {
		case tg.back:
			ls = backLayers
		case tg.w > 2.2:
			ls = bigLayers
		}
		flameTongue(p, X+tg.dx, Y+tg.y, h, tg.w, sweep(i, t, h), N, tg.n, H, ls, 0.5, 1.1+h*0.04)
	}

	// the plume: each puff keeps the sway the plume had when it left, so the wiggle climbs it
	type plume struct{ x, y, vx, vy, s float64 }
	looped(t, D, count(o.Smoke), S+300, func(r *pixelfx.Rng, _ int) (plume, float64) {
		var q plume
		q.x = r.Range(-W, W) * 0.7
		q.y = -HC*0.8 - r.Range(0, 6)
		q.vx = r.Range(-2, 2)
		q.vy = -r.Range(10, 17)
		life := r.Range(o.SmokeLife[0], o.SmokeLife[1])
		q.s = r.Range(0.7, 1.1) * o.SmokeSize
		return q, life
	}, func(q *plume, age, life float64, _ int) {
		x, y, _, _ := drift(q.x, q.y, q.vx, q.vy, age, o.Wind, -20, 1.6, 1)
		smokePuff(p, X+x+NL(t-age, 0.8, S+60, 0)*(1.5+2.5*age), Y+y, 1-age/life, q.s, SMOKE, false, 1)
	})
	// flame puffs shed from the tips
	type shed struct {
		i                              int
		fire, sl, ox, oy, vx, vy, s, f float64
		smokes                         bool
	}
	type puff struct{ x, y, k, s, f float64 }
	var puffs []puff
	looped(t, D, len(TT)*count(o.Puffs), S+200, func(r *pixelfx.Rng, j int) (shed, float64) {
		q := shed{i: j % len(TT)}
		q.fire = r.Range(0.14, 0.3)
		q.smokes = r.Chance(0.45)
		q.sl = r.Range(0.8, 1.4)
		q.ox = r.Range(-1, 1)
		q.oy = r.Range(0, 4)
		q.vx = r.Range(-4, 4)
		q.vy = -r.Range(15, 30)
		q.s = r.Range(0.8, 1.6) * (0.8 + 0.6*TT[q.i].env)
		q.f = r.Next()
		life := q.fire
		if q.smokes {
			life = q.fire + 1.4
		}
		return q, life
	}, func(q *shed, age, _ float64, _ int) {
		tx, ty := tip(q.i, t-age)
		x, y, vx, vy := drift(tx+q.ox, ty+q.oy, q.vx, q.vy, math.Min(age, q.fire), 0, -95, 5, 3.2)
		if age < q.fire {
			puffs = append(puffs, puff{x, y, 1 - age/q.fire, q.s, q.f})
			return
		}
		if age-q.fire > q.sl {
			return
		}
		sx, sy, _, _ := drift(x, y, vx*0.6, vy*0.6-6, age-q.fire, o.Wind, -16, 1.6, 1.2)
		smokePuff(p, sx, sy, 1-(age-q.fire)/q.sl, q.s*0.7, 0, false, 1)
	})
	for i := range TT {
		if TT[i].back {
			tongueAt(i)
		}
	}
	for i := range TT {
		if !TT[i].back {
			tongueAt(i)
		}
	}
	if o.Mid != nil {
		o.Mid(fl)
	}
	// licks torn off the tips, rising and shrinking
	type lk struct {
		i         int
		vx, vy, s float64
	}
	looped(t, D, len(TT)*count(o.Licks), S+220, func(r *pixelfx.Rng, j int) (lk, float64) {
		q := lk{i: j % len(TT)}
		q.vx = r.Range(-3, 3)
		q.vy = -r.Range(12, 22)
		q.s = r.Range(0.9, 1.6) * (0.8 + 0.5*TT[j%len(TT)].env)
		return q, 0.26
	}, func(q *lk, age, life float64, _ int) {
		tx, ty := tip(q.i, t-age)
		lick(p, tx+q.vx*age, ty-2+q.vy*age, 1-age/life, q.s, H)
	})
	for _, q := range puffs {
		firePuff(p, q.x, q.y, q.k, q.s, q.f, H, 0.18, 0, -1)
	}
	// embers carried up by the heat, wandering and cooling
	type em struct{ x, y, vx, vy, s, a, b float64 }
	looped(t, D, count(o.Embers), S+500, func(r *pixelfx.Rng, _ int) (em, float64) {
		var q em
		q.x = r.Range(-W, W)
		q.y = -r.Range(HC*0.3, HC*0.8)
		q.vx = r.Range(-15, 15)
		q.vy = -r.Range(25, 60)
		life := r.Range(1.1, 2.4)
		if r.Chance(0.15) {
			q.s = 2
		} else {
			q.s = r.Range(1, 1.6)
		}
		q.a = r.Range(0, 6.3)
		q.b = r.Range(0, 6.3)
		return q, life
	}, func(q *em, age, life float64, _ int) {
		x, y, _, _ := drift(q.x, q.y, q.vx, q.vy, age, 0, -30, 1.6, 1.6)
		ember(p, X+x+7*(math.Sin(age*2.3+q.a)-math.Sin(q.a)), Y+y+2*(math.Sin(age*3+q.b)-math.Sin(q.b)), 1-age/life, q.s, EMB, age*40+q.a)
	})
	// light: a broad warm wash, lights on the flames, a hot heart at the base
	if G > 0 {
		R1, lx := o.GlowR, X+lean(t)*12
		p.Glow(lx, Y-HC*0.35, R1*1.5, GLOW, 0.22*fl*G)
		p.Glow(lx, Y-HC*0.75, R1, GLOW, 0.34*fl*G)
		p.Glow(X, Y-HC*0.2, R1*0.32, H[2], 0.2*fl*G)
		p.Glow(X, Y-2, 8, H[0], 0.14*fl*G)
	}
	return fl
}

// cmpDesc orders by a number, largest first, as JavaScript's sort((a, b) => b - a) does.
func cmpDesc(a, b float64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	}
	return 0
}

// cmpAsc orders by a number, smallest first, as JavaScript's sort((a, b) => a - b) does.
func cmpAsc(a, b float64) int { return cmpDesc(b, a) }

// farHalf and nearHalf pick the far and near halves of a pixelfx.Band, so the far half can go behind a character and the near half in front.
func farHalf(g float64) bool  { return math.Sin(g) < 0 }
func nearHalf(g float64) bool { return math.Sin(g) >= 0 }
