// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the smoke puff, spark streak, flash lobe and casing recipes from Sprite Fusion's open-source muzzle-flash and explosion (https://www.spritefusion.com/pixel-art-vfx/muzzle-flash), MIT License, Copyright (c) 2026 Hugo Duprez.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Shared by the projectile effects: a steady emitter for loops, motion in a projectile's own frame, merged smoke clouds, speed-stretched sparks, muzzle-flash lobes, brass casings that bounce, and outlined pixel shapes.

// pi, tau and f1 are float64 variables so expressions combining them with other constants round step by step as JavaScript's do, instead of being folded exactly at compile time.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
	f1  = 1.0 / 30
)

func pick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

// beat is like pixelfx.Cycle, but particle i is born at i * period / n: an emitter with a steady beat, so a trail has no clumps or gaps. It loops seamlessly over period; fn(age, life, r, i) receives the same rng every period.
func beat(t, period float64, n int, seed float64, life func(r *pixelfx.Rng) float64, fn func(age, life float64, r *pixelfx.Rng, i int)) {
	for i := range n {
		L := life(pixelfx.RngOf(seed, float64(i), 911))
		for age := pixelfx.Wrap(t-(float64(i)*period)/float64(n)+1e-7, period); age < L; age += period {
			fn(age, L, pixelfx.RngOf(seed, float64(i)), i)
		}
	}
}

// wake returns where something is after age seconds in the frame of a projectile that flies right at V px/s: launched from (x, y) at (vx, vy) relative to the still air, slowed toward the air by drag k (1/s), pulled by g. Returns (x, y, vx, vy) in screen motion.
func wake(x, y, vx, vy, age, V, k, g float64) (float64, float64, float64, float64) {
	e := math.Exp(-k * age)
	f, vt := (1-e)/k, g/k
	return x + vx*f - V*age, y + vt*age + (vy-vt)*f, vx*e - V, vt + (vy-vt)*e
}

// cloudPuff is one puff fed into cloud: center (x, y), radius r, opacity a, brightness b (0..1) and fire light w (0..1).
type cloudPuff struct {
	x, y, r, a, b, w float64
}

type cloudOpts struct {
	dens, max, lit float64
}

// cloud merges puffs into one translucent volume: each puff is lit on top and shaded underneath, densities add up, and tone and density come out in clean steps. R is the smoke ramp (dark to bright), WR the same smoke lit by fire.
func cloud(p *pixelfx.Painter, list []cloudPuff, R, WR []uint32, o cloudOpts) {
	if len(list) == 0 {
		return
	}
	dens, maxA, lit := o.dens, o.max, o.lit
	if dens == 0 {
		dens = 1.5
	}
	if maxA == 0 {
		maxA = 0.86
	}
	if lit == 0 {
		lit = 1
	}
	X0, Y0, X1, Y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, q := range list {
		X0 = math.Min(X0, math.Floor(q.x-q.r))
		X1 = math.Max(X1, math.Ceil(q.x+q.r))
		Y0 = math.Min(Y0, math.Floor(q.y-q.r))
		Y1 = math.Max(Y1, math.Ceil(q.y+q.r))
	}
	W, H := int(X1-X0+3), int(Y1-Y0+3)
	D, L, F := make([]float32, W*H), make([]float32, W*H), make([]float32, W*H)
	X0--
	Y0--
	x0i, y0i := int(X0), int(Y0)
	for _, q := range list {
		r := q.r
		r2 := r * r
		for y := math.Floor(q.y - r); y <= math.Ceil(q.y+r); y++ {
			dy := y + 0.5 - q.y
			for x := math.Floor(q.x - r); x <= math.Ceil(q.x+r); x++ {
				dx := x + 0.5 - q.x
				d2 := (dx*dx + dy*dy) / r2
				if d2 >= 1 {
					continue
				}
				// a ball's thickness: flat in the middle, steep at the rim, so every lump keeps its own crown
				k := math.Sqrt(1-d2) * q.a
				i := (int(y)-y0i)*W + (int(x) - x0i)
				D[i] = float32(float64(D[i]) + k)
				L[i] = float32(float64(L[i]) + k*q.b)
				F[i] = float32(float64(F[i]) + k*q.w*(0.5+0.5*pixelfx.Clamp(0.5+dx/r)))
			}
		}
	}
	// light the merged surface: where density climbs downward the cloud faces up and catches the light. The gradient comes off a blurred copy, so only lump-sized features catch it, never the crossing rims
	B := blur(blur(D, W, H, 1, 2), W, H, W, 2)
	n := len(R)
	p.Each(X0+1, Y0+1, X1+1, Y1+1, func(lx, ly float64, sx, sy int) {
		cx, cy := int(math.Floor(lx))-x0i, int(math.Floor(ly))-y0i
		if cx < 1 || cy < 1 || cx >= W-1 || cy >= H-1 {
			return
		}
		i := cy*W + cx
		d := float64(D[i])
		if d < 0.05 {
			return
		}
		a := math.Min(maxA, math.Ceil((1-math.Exp(-d*dens))*6)/6)
		if a < 0.15 {
			return
		}
		gy, gx := float64(B[i+W])-float64(B[i-W]), float64(B[i+1])-float64(B[i-1])
		up := pixelfx.Clamp(0.5 + math.Atan((gy*0.9+gx*0.35)/(float64(B[i])*0.16+0.03))*0.42*lit)
		w := float64(F[i]) / d
		j := int(pixelfx.ClampTo(pixelfx.Round((float64(L[i])/d)*(0.3+0.7*up)*float64(n-1)+0.25+w*1.5), 0, float64(n-1)))
		c := R[j]
		if w > 0.03 {
			c = pixelfx.Mix(R[j], WR[j], math.Min(0.9, math.Ceil(w*10)/10))
		}
		p.BlendPx(sx, sy, c, a)
	})
}

// blur is a box blur of radius r along one axis of a w x h field (step 1 for rows, w for columns).
func blur(src []float32, w, h, step, r int) []float32 {
	out := make([]float32, len(src))
	n, lines := h, w
	if step == 1 {
		n, lines = w, h
	}
	k := 1 / float64(2*r+1)
	for l := range lines {
		base := l
		if step == 1 {
			base = l * w
		}
		acc := 0.0
		for j := -r; j <= r; j++ {
			if j >= 0 && j < n {
				acc += float64(src[base+j*step])
			}
		}
		for j := range n {
			out[base+j*step] = float32(acc * k)
			a, b := j-r, j+r+1
			if a >= 0 {
				acc -= float64(src[base+a*step])
			}
			if b < n {
				acc += float64(src[base+b*step])
			}
		}
	}
	return out
}

// smokeItem is one simulated puff of a smokeSys.
type smokeItem struct {
	x, y, vx, vy     float64
	r0, r1, life     float64
	drag, rise, grow float64
	curl, a, b, warm float64
	delay, age, seed float64
}

// smokeSys is simulated smoke for one-shots, drawn with cloud().
type smokeSys struct {
	r    *pixelfx.Rng
	list []smokeItem
	t    float64
}

func newSmoke(r *pixelfx.Rng) *smokeSys { return &smokeSys{r: r} }

func (s *smokeSys) add(q smokeItem) {
	if q.drag == 0 {
		q.drag = 2.5
	}
	if q.rise == 0 {
		q.rise = 12
	}
	if q.grow == 0 {
		q.grow = 2
	}
	if q.a == 0 {
		q.a = 0.6
	}
	if q.b == 0 {
		q.b = 0.85
	}
	q.age = 0
	q.seed = s.r.Next() * 100
	s.list = append(s.list, q)
}

func (s *smokeSys) update(dt float64) {
	s.t += dt
	for i := len(s.list) - 1; i >= 0; i-- {
		q := &s.list[i]
		if q.delay > 0 {
			q.delay -= dt
			continue
		}
		q.age += dt
		if q.age >= q.life {
			s.list = append(s.list[:i], s.list[i+1:]...)
			continue
		}
		if q.curl != 0 {
			c, sn, vx := math.Cos(q.curl*dt), math.Sin(q.curl*dt), q.vx
			q.vx = vx*c - q.vy*sn
			q.vy = vx*sn + q.vy*c
		}
		q.vy -= q.rise * dt
		q.vx += math.Sin(s.t*1.3+q.seed) * 8 * dt
		q.vx -= q.vx * q.drag * dt
		q.vy -= q.vy * q.drag * dt
		q.x += q.vx * dt
		q.y += q.vy * dt
	}
}

func (s *smokeSys) puffs() []cloudPuff {
	out := make([]cloudPuff, 0, len(s.list))
	for i := range s.list {
		q := &s.list[i]
		if q.delay > 0 {
			continue
		}
		k := q.age / q.life
		out = append(out, cloudPuff{
			x: q.x,
			y: q.y,
			r: q.r0 + (q.r1-q.r0)*(1-math.Exp(-q.grow*q.age)),
			a: q.a * math.Min(1, q.age*30) * math.Pow(1-k, 1.3),
			b: q.b * (1 - k*0.5),
			w: q.warm * math.Exp(-q.age*5),
		})
	}
	return out
}

// spark draws a two-tone streak stretched along its screen velocity, its head cooling down S (hot, warm, cool, tail) as k (life left, 1..0) falls, with a point of light.
func spark(p *pixelfx.Painter, x, y, vx, vy, k float64, S [4]uint32, glow, stretch, maxLen float64) {
	sp := pixelfx.Hypot(vx, vy)
	if sp == 0 {
		sp = 1
	}
	ux, uy, ln := vx/sp, vy/sp, math.Min(maxLen, 1+sp*stretch)
	head := S[2]
	if k > 0.6 {
		head = S[0]
	} else if k > 0.3 {
		head = S[1]
	}
	tail := S[3]
	if k > 0.5 {
		tail = head
	}
	p.Line(x-ux*ln, y-uy*ln, x-ux*ln*0.4, y-uy*ln*0.4, tail, 1)
	p.Line(x-ux*ln*0.45, y-uy*ln*0.45, x, y, head, 1)
	p.Glow(x, y, 3, head, glow*k)
}

type flashLobe struct {
	off, a, len, wid, core float64
}

type flashShapeOpts struct {
	size, core               float64
	spike, diag, cross, back [2]float64
}

// flashShape builds the lobes of one muzzle flash from r: a fat fireball at the muzzle, a spike forward, and a burst of rays round it.
func flashShape(r *pixelfx.Rng, f int, o flashShapeOpts) []flashLobe {
	S, core := o.size, o.core
	if S == 0 {
		S = 1
	}
	if core == 0 {
		core = 0.7
	}
	spike, diag, cross, back := o.spike, o.diag, o.cross, o.back
	if spike == [2]float64{} {
		spike = [2]float64{12, 20}
	}
	if diag == [2]float64{} {
		diag = [2]float64{5, 10}
	}
	if cross == [2]float64{} {
		cross = [2]float64{3, 6}
	}
	if back == [2]float64{} {
		back = [2]float64{2, 4.5}
	}
	k, w := 0.6, 0.72
	if f == 1 {
		k, w = 1, 1
	}
	L := make([]flashLobe, 0, 8)
	L = append(L,
		flashLobe{off: 0.5, a: r.Range(-0.08, 0.08), len: r.Range(5.5, 8) * S * k, wid: r.Range(2.1, 2.6) * S * w, core: 0.5 * core},
		flashLobe{off: 2, a: r.Range(-0.07, 0.07), len: r.Range(spike[0], spike[1]) * S * k, wid: r.Range(1, 1.3) * S * w, core: 0.35 * core},
	)
	ray := func(a, ln, wid float64, on bool) {
		if on {
			L = append(L, flashLobe{off: r.Range(0.5, 2), a: a, len: ln * S * k, wid: wid * S * w, core: 0.25 * core})
		}
	}
	for _, s := range [2]float64{-1, 1} {
		ray(s*r.Range(0.3, 0.8), r.Range(diag[0], diag[1]), r.Range(1.1, 1.5), r.Chance(0.85))
		ray(s*r.Range(1.15, 1.7), r.Range(cross[0], cross[1]), r.Range(1, 1.3), r.Chance(0.6))
		ray(s*r.Range(2, 2.7), r.Range(back[0], back[1]), 1, r.Chance(0.7) && f == 1)
	}
	return L
}

func lobeProfile(l flashLobe, u float64) float64 {
	k := u / l.len
	return l.wid * math.Pow(math.Max(0, 1-k), 0.8) * math.Min(1, 0.45+k*3)
}

// flash draws flash lobes L at (x, y) pointing along angle a: core, inner, body, rim in C; seed varies the ragged edge.
func flash(p *pixelfx.Painter, x, y, a float64, L []flashLobe, C [4]uint32, seed float64) float64 {
	c, s := math.Cos(a), math.Sin(a)
	ext := 2.0
	for _, l := range L {
		ext = math.Max(ext, l.off+l.len+2)
	}
	p.Shade(x-ext, y-ext, x+ext, y+ext, func(px, py float64, _ *pixelfx.Out) uint32 {
		dx, dy := px-x, py-y
		along, across := dx*c+dy*s, -dx*s+dy*c
		m, hot, warm := -9.0, false, false
		for _, l := range L {
			ca, sa := math.Cos(l.a), math.Sin(l.a)
			lu, lv := (along-l.off)*ca+across*sa, -(along-l.off)*sa+across*ca
			if lu < -1.5 || lu > l.len+1 {
				continue
			}
			r := lobeProfile(l, math.Max(0, lu))
			mm := r - math.Abs(lv)
			if mm > m {
				m = mm
			}
			if lu < l.len*l.core && math.Abs(lv) <= r*0.5 {
				hot = true
			}
			if mm > r*0.4 {
				warm = true
			}
		}
		dm := 1.6 - pixelfx.Hypot(along-0.5, across)
		if dm > m {
			m = dm
		}
		if dm > 0.6 {
			hot = true
		}
		m += (pixelfx.Rand(math.Floor(px)*73+math.Floor(py)*151, seed) - 0.5) * 0.5 * math.Min(1, ext/14)
		if m < -0.85 {
			return 0
		}
		switch {
		case m < 0:
			return C[3]
		case hot:
			return C[0]
		case warm:
			return C[1]
		}
		return C[2]
	})
	return ext
}

type casingState struct {
	x, y, a float64
	rest    bool
	hit     float64
}

// casing computes a brass casing thrown from (x, y) at (vx, vy) with spin va, falling under g onto y = floor, bouncing in closed form.
func casing(x, y, vx, vy, va, age, g, floor, bounce, a0 float64) casingState {
	t0, a, hit := 0.0, a0, -1.0
	for range 6 {
		d := floor - y
		T := (-vy + math.Sqrt(math.Max(0, vy*vy+2*g*d))) / g
		if age-t0 < T {
			u := age - t0
			return casingState{x: x + vx*u, y: y + vy*u + 0.5*g*u*u, a: a + va*u, rest: false, hit: hit}
		}
		x += vx * T
		a += va * T
		t0 += T
		hit = age - t0
		vin := vy + g*T
		y = floor
		if vin*bounce < 45 {
			u, k := age-t0, 7.0
			s := (1 - math.Exp(-k*u)) / k
			return casingState{x: x + vx*0.6*s, y: floor, a: pixelfx.Round(a/pi) * pi, rest: true, hit: hit}
		}
		vy = -vin * bounce
		vx *= 0.55
		va *= 0.6
	}
	return casingState{x: x, y: floor, a: pixelfx.Round(a/pi) * pi, rest: true, hit: hit}
}

// drawCasing draws a 3 px brass slug turned by its angle, lit on one end, with a glint when it has just hit.
func drawCasing(p *pixelfx.Painter, k casingState, B []uint32, fade float64) {
	ca, sa, x, y := math.Cos(k.a), math.Sin(k.a), k.x, k.y-0.5
	p.Dot(x-ca, y-sa, B[1], fade)
	p.Dot(x, y, B[2], fade)
	p.Dot(x+ca, y+sa, B[3], fade)
	if k.hit >= 0 && k.hit < 0.07 {
		p.Px(x+ca*0.5, y-1, pixelfx.White)
		p.Glow(x, y, 5, B[3], 0.55*(1-k.hit/0.07))
	}
}

// turnSprite draws a small sprite turned by ang about its pivot (px, py in sprite pixels) placed at (x, y), sampled nearest-pixel.
func turnSprite(p *pixelfx.Painter, rows []string, pal map[rune]uint32, x, y, ang, px, py, a float64) {
	H, W := len(rows), len(rows[0])
	cs, sn := math.Cos(ang), math.Sin(ang)
	R := pixelfx.Hypot(math.Max(px, float64(W)-px), math.Max(py, float64(H)-py)) + 1
	p.Shade(x-R, y-R, x+R, y+R, func(lx, ly float64, out *pixelfx.Out) uint32 {
		dx, dy := lx-x, ly-y
		i, j := int(math.Floor(dx*cs+dy*sn+px)), int(math.Floor(-dx*sn+dy*cs+py))
		if i < 0 || j < 0 || i >= W || j >= H {
			return 0
		}
		c := pal[rune(rows[j][i])]
		if c == 0 {
			return 0
		}
		out.C = c
		out.A = a
		return 0
	})
}

type shapeSprite struct {
	rows   []string
	x0, y0 float64
}

// pixelShape builds a sprite with an automatic 'o' outline around interior pixels written by build.
func pixelShape(x0, y0, w, h int, build func(set func(px, py int, ch byte))) shapeSprite {
	g := make([][]byte, h)
	for j := range h {
		row := make([]byte, w)
		for i := range w {
			row[i] = '.'
		}
		g[j] = row
	}
	set := func(px, py int, ch byte) {
		i, j := px-x0, py-y0
		if i >= 0 && j >= 0 && i < w && j < h {
			g[j][i] = ch
		}
	}
	build(set)
	solid := func(i, j int) bool {
		return i >= 0 && j >= 0 && i < w && j < h && g[j][i] != '.' && g[j][i] != 'o'
	}
	for j := range h {
		for i := range w {
			if g[j][i] == '.' && (solid(i-1, j) || solid(i+1, j) || solid(i, j-1) || solid(i, j+1)) {
				g[j][i] = 'o'
			}
		}
	}
	rows := make([]string, h)
	for j := range h {
		rows[j] = string(g[j])
	}
	return shapeSprite{rows: rows, x0: float64(x0), y0: float64(y0)}
}
