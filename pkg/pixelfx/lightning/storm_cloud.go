// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/storm-cloud.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The bolt's forked shape, its halo-body-core inking and its flicker follow Sprite Fusion's open-source lightning (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// StormCloud is a storm cloud boiling overhead, its billows lit from inside by flickering discharges, ragged scud under its flat dark base, slanting rain, and once a loop a bolt cracking to the floor. It is registered as "lightning/storm-cloud".
var StormCloud = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Storm cloud",
		Desc:     "A storm cloud boils overhead, billows lit from inside by flickering discharges, ragged scud under its flat dark base; rain slants down and once a loop a bolt cracks to the floor.",
		Tags:     []string{"lightning", "storm", "weather", "cloud", "rain", "ambient", "loop"},
		Size:     [2]int{168, 176},
		Origin:   [2]int{84, 164},
		Duration: 3,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: append(voltOptions(),
			pixelfx.Option{Name: "cloud", Default: pixelfx.Palette("#09070f", "#120f1e", "#1b182e", "#252640", "#323758", "#464d78", "#646b9c")},
			pixelfx.Option{Name: "rain", Default: pixelfx.Palette("#2b3250", "#46527a", "#6d7cab", "#a5b5e0")},
			pixelfx.Option{Name: "seed", Default: pixelfx.Num(8)},
		),
	},
	New: newStormCloud,
}

func init() { pixelfx.Register("lightning/storm-cloud", StormCloud) }

const (
	scFPS   = 30
	scN     = 90
	scP     = 3
	scBase  = -100
	scTop   = -156
	scBolt  = 52
	scBoltB = 12
	scW0    = -84
	scH0    = scTop - 6
	scGW    = 168
	scGH    = scBase + 8 - scH0
)

// scTH is the field's threshold, a variable so sums with it round as JavaScript's do; scVis is the share of a billow's radius that shows.
var (
	scTH  = 0.35
	scVis = math.Sqrt(1 - math.Sqrt(scTH))
)

// scBillows are the billows: x, y, visible radius and squash; a tower leaning into a flat anvil sheared off to the right, big billows of all sizes below it, smaller shoulders, a flat run along the base.
var scBillows = [...][4]float64{
	{-6, -132, 17, 1}, {18, -127, 14, 1}, {-28, -122, 12, 1}, {38, -118, 10, 1}, {0, -142, 12, 1},
	{14, -149, 19, 0.38}, {40, -148, 14, 0.34},
	{-48, -113, 10, 1}, {55, -110, 9, 1}, {-17, -115, 13, 1}, {22, -113, 12, 1},
	{-42, -104, 11, 1}, {-10, -104, 12, 1}, {20, -104, 11, 1}, {48, -103, 9, 1}, {-63, -104, 7, 1}, {66, -103, 6, 1},
}

// scFlashes are the discharges inside: the frame, the billow and the strength.
var scFlashes = [...]struct {
	f, b int
	k    float64
}{{7, 2, 0.8}, {8, 9, 0.55}, {29, 1, 0.75}, {31, 10, 1}, {33, 3, 0.5}, {70, 0, 0.8}, {71, 4, 0.55}, {72, 5, 0.35}}

// scBK is the bolt's brightness over the frames it lives.
var scBK = [...]float64{1, 0.7, 0.3, 0.95, 0.5, 0.75, 0.2}

type scBillow struct{ sq, x, y, r float64 }

// scFlash is a billow lit this frame and how strongly, in the order pixelfx's Map holds them.
type scFlash struct {
	b int
	k float64
}

// scBillowsAt returns where each billow is at phase ph: drifting, breathing and boiling up on its own clock, all seamless over the loop.
func scBillowsAt(B []scBillow, seed, ph float64) {
	for i, s := range scBillows {
		x, y, v, sq := s[0], s[1], s[2], s[3]
		fi := float64(i)
		top := y < -112 && sq == 1
		boil := 0.82 + 0.3*math.Pow(math.Sin(pi*math.Mod(ph+fi*0.37, 1)), 2)
		dx := 0.0
		if sq < 1 {
			dx = 4 * pixelfx.LoopNoise(ph, 0.5, seed+fi+90)
		}
		dy := 0.0
		if top {
			dy = 4 * (boil - 0.97)
		}
		g := boil
		if !top {
			g = 1 + 0.08*pixelfx.LoopNoise(ph, 0.6, seed+fi+60)
		}
		B[i] = scBillow{sq: sq, x: x + 3.5*pixelfx.LoopNoise(ph, 0.7, seed+fi) + dx, y: y + 2*pixelfx.LoopNoise(ph, 0.7, seed+fi+30) - dy, r: (v * g) / scVis}
	}
}

type scCloud struct {
	field, gx, gy, lit []float32
}

// draw draws the cloud as one field: billows summed so they fuse, clipped flat at the base, its edge ragged with drifting noise; shaded as one mass (a lit top, a cool middle, a dark belly), discharges lighting whole billows from inside. The field is float32 as pixelfx's typed arrays are.
func (cl *scCloud) draw(p *pixelfx.Painter, B []scBillow, ph, seed float64, ramp []uint32, n int, flashes []scFlash) {
	clear(cl.field)
	clear(cl.gx)
	clear(cl.gy)
	clear(cl.lit)
	for i, b := range B {
		k := 0.0
		for _, fl := range flashes {
			if fl.b == i {
				k = fl.k
			}
		}
		r2, ry := b.r*b.r, b.r*b.sq
		for y := max(0, int(math.Floor(b.y-ry-scH0))); y < min(scGH, int(math.Ceil(b.y+ry-scH0))); y++ {
			for x := max(0, int(math.Floor(b.x-b.r-scW0))); x < min(scGW, int(math.Ceil(b.x+b.r-scW0))); x++ {
				dx := float64(x+scW0) + 0.5 - b.x
				dy := (float64(y+scH0) + 0.5 - b.y) / b.sq
				d2 := (dx*dx + dy*dy) / r2
				if d2 >= 1 {
					continue
				}
				j := y*scGW + x
				u := 1 - d2
				c := u * u
				cl.field[j] = float32(float64(cl.field[j]) + c)
				cl.gx[j] = float32(float64(cl.gx[j]) + (-4*u*dx)/r2)
				cl.gy[j] = float32(float64(cl.gy[j]) + (-4*u*dy)/(r2*b.sq))
				if k != 0 {
					cl.lit[j] = float32(float64(cl.lit[j]) + k*(0.25+0.75*c))
				}
			}
		}
	}
	th0, th5, th8 := scTH-0.06, scTH+0.05, scTH+0.08
	for y := range scGH {
		cy := float64(y+scH0) + 0.5
		for x := range scGW {
			j := y*scGW + x
			fj := float64(cl.field[j])
			if fj < th0 {
				continue
			}
			cx := float64(x+scW0) + 0.5
			base := scBase + 1.5*pixelfx.TileNoise(cx*0.12+9*ph, 3, 9, 0, seed+5)
			F := fj*(1-pixelfx.Smooth(base-3, base, cy)) + 0.05*pixelfx.TileNoise(cx*0.21+18*ph, cy*0.21, 18, 0, seed)
			if F < scTH {
				continue
			}
			// the field's slope rounds each billow's top; height does the rest: lit above, cool in the middle, dark below
			gx, gy := float64(cl.gx[j]), float64(cl.gy[j])
			gl := pixelfx.Hypot(gx, gy)
			if gl == 0 || gl != gl {
				gl = 1
			}
			lam := -(gx*0.42 + gy*0.9) / gl
			edge := math.Min(1, gl*5) * 0.55
			if F < th8 {
				edge = math.Min(1, gl*5) * 1
			}
			h := 1 - 2*pixelfx.Clamp((cy-scTop)/(scBase-scTop))
			i := 2.7 + 2.2*h + 1.3*lam*edge + 0.4*pixelfx.TileNoise(cx*0.16+12*ph, cy*0.16, 12, 0, seed+9)
			if F < th5 && lam > 0.3 && h > -0.5 {
				i++
			}
			if cy > base-6 {
				i--
			}
			i = pixelfx.ClampTo(pixelfx.Round(i), 0, float64(n-1))
			// lit from inside: brightest deep in the billow, breaking up through the cloud's own lumps toward its edges
			L := float64(cl.lit[j]) * (0.7 + 0.6*pixelfx.TileNoise(cx*0.2-14*ph, cy*0.2, 14, 0, seed+3))
			if L > 0.12 {
				i = math.Min(float64(len(ramp)-1), i+pixelfx.Round(L*5.5))
			}
			p.Px(cx, cy, ramp[int(i)])
		}
	}
}

// scScud draws scud: ragged wisps that form under the base, drift off on the wind and fray away.
func scScud(p *pixelfx.Painter, ph, seed float64, K []uint32) {
	r := pixelfx.RngOf(seed, 13)
	for w := range 6 {
		fw := float64(w)
		x0, y0, rx, ry, off := r.Range(-60, 60), scBase+r.Range(3, 9), r.Range(10, 20), r.Range(1.3, 2.2), r.Next()
		u := math.Mod(ph+off, 1)
		a := math.Pow(math.Sin(pi*u), 0.8)
		xc := x0 + 16*(u-0.5)
		for y := math.Floor(y0 - ry - 1); y <= y0+ry+1; y++ {
			for x := math.Floor(xc - rx - 1); x <= xc+rx+1; x++ {
				dx, dy := (x+0.5-xc)/rx, (y+0.5-y0)/ry
				nz := pixelfx.TileNoise((x+0.5)*0.35+10*ph+fw*7, (y+0.5)*0.5, 10, 0, seed+20+fw)
				v := (1-dx*dx-dy*dy)*a + 0.6*nz - 0.25
				if v > 0 {
					c := K[3]
					if v > 0.4 {
						c = K[2]
					}
					p.Blend(x+0.5, y+0.5, c, math.Min(0.6, 0.2+v*0.7))
				}
			}
		}
	}
}

func newStormCloud(o *pixelfx.Options) pixelfx.Instance {
	C, K, RN, S, seed := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("cloud"), 7), pixelfx.Fit(o.Ramp("rain"), 4), o.Ramp("sparks"), o.Seed()
	ramp := append(append([]uint32{}, K...), C[3], C[4], C[5], C[6])
	cl := &scCloud{field: make([]float32, scGW*scGH), gx: make([]float32, scGW*scGH), gy: make([]float32, scGW*scGH), lit: make([]float32, scGW*scGH)}
	B := make([]scBillow, len(scBillows))
	br := pixelfx.RngOf(seed, 5, scBolt)
	bx0 := scBillows[scBoltB][0] + br.Range(-4, 4)
	bx1 := br.Range(-24, 24)
	var flashes []scFlash
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		f := frameOf(t, scFPS, scN)
		ph := float64(f) / scN
		scBillowsAt(B, seed, ph)
		ba := f - scBolt
		// what's lighting the cloud this frame: discharges inside billows, and the billow the bolt leaves from
		flashes = flashes[:0]
		set := func(b int, k float64) {
			for i := range flashes {
				if flashes[i].b == b {
					flashes[i].k = k
					return
				}
			}
			flashes = append(flashes, scFlash{b, k})
		}
		for _, fl := range scFlashes {
			if fl.f == f {
				set(fl.b, fl.k)
			}
		}
		if ba >= 0 && ba < len(scBK) {
			k := 0.0
			for _, fl := range flashes {
				if fl.b == scBoltB {
					k = fl.k
				}
			}
			set(scBoltB, math.Max(k, scBK[ba]))
		}
		flash := 0.0
		for _, fl := range flashes {
			flash = math.Max(flash, fl.k)
		}

		// rain: slanting streaks from the belly, splashing on the floor, lit when the sky flashes
		pixelfx.Cycle(t, scP, 150, seed+11, func(g *pixelfx.Rng) float64 { return g.Range(0.5, 0.62) }, func(age, _ float64, r *pixelfx.Rng, _ int) {
			x0, vy := r.Range(-62, 62), r.Range(190, 240)
			vx, tf := -vy*0.18, (0-(scBase+4))/vy
			if age < tf {
				x, y, l := x0+vx*age, scBase+4+vy*age, r.Range(4, 6.5)
				p.LineFn(x-vx*0.018*(l/4), y-l, x, y, func(u float64) (uint32, float64) {
					switch {
					case u > 0.7 && flash > 0.5:
						return C[5], 1
					case u > 0.7:
						return RN[3], 1
					case flash > 0.5:
						return C[3], 1
					case u > 0.35:
						return RN[2], 1
					}
					return RN[1], 1
				})
			} else if age < tf+0.12 {
				x, a := x0+vx*tf, (age-tf)/0.12
				c := RN[2]
				if a < 0.5 {
					c = RN[3]
				}
				for _, s := range [2]float64{-1, 1} {
					p.Px(x+s*(1+a*2.5), -1-math.Sin(a*pi)*2.5, c)
				}
				if a < 0.4 {
					p.Px(x, 0, RN[3])
				}
			}
		})
		// the scud hanging under the base, the cloud, then the light of whatever burns inside it
		scScud(p, ph, seed, K)
		cl.draw(p, B, ph, seed, ramp, len(K), flashes)
		for _, fl := range flashes {
			b := B[fl.b]
			r := 2.2
			if b.sq < 1 {
				r = 1.6
			}
			p.Glow(b.x, b.y, b.r*scVis*r, C[4], 0.4*fl.k)
		}
		p.Glow(0, -122, 80, C[2], 0.05+0.22*flash)
		// a discharge crawls along the belly now and then
		for _, fl := range scFlashes {
			if fl.f == f && fl.k > 0.6 {
				r, x := pixelfx.RngOf(seed, 7, float64(fl.f)), B[fl.b].x
				crackle(p, zigzag(r, x-r.Range(12, 22), scBase-r.Range(2, 6), x+r.Range(12, 22), scBase-r.Range(1, 5), 0.25, 0.5), 0.6, C, 0.4, 1)
			}
		}
		// the bolt: struck once a loop, forked, flickering, the floor lit and sparks thrown where it lands
		if ba >= 0 && ba < len(scBK) {
			k, s := scBK[ba], 1.0
			if ba == 0 {
				s = 1.15
			}
			stroke(p, zigzag(pixelfx.RngOf(seed, 9, scBolt), bx0, scBase-3, bx1, 0, 0.22, 0.7), k, C, s, strokeOpts{hero: ba == 0})
			p.Circle(bx1, -1, 3*k+1.5, C[2], 1)
			p.Circle(bx1, -1, 3*k+0.5, C[5], 1)
			p.Circle(bx1, -1, 2*k, pixelfx.White, 1)
			p.Glow(bx1, -22, 78, C[3], 0.5*k)
			p.Glow(bx1, -3, 18, C[5], 0.45*k)
			p.Glow(bx0, scBase, 84, C[4], 0.3*k)
		}
		sa := pixelfx.Wrap(t-float64(scBolt)/scFPS, scP)
		for j := range 16 {
			q := pixelfx.RngOf(seed, 31, float64(j))
			life := q.Range(0.25, 0.6)
			if sa >= life {
				continue
			}
			a, sp := -pi/2+q.Range(-1.3, 1.3), q.Range(60, 200)
			x, y, vx, vy := flight(bx1, -1, math.Cos(a)*sp, math.Sin(a)*sp, sa, 0)
			spark(p, x, y, vx, vy, 1-sa/life, S)
		}
	}}
}
