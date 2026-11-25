// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/laser-grid.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the beam's build (ramp across it, hot core, light along it) from Sprite Fusion's open-source energy beam (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package scifi

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// LaserGrid is security lasers between two steel posts: red beams slide and scissor across, dust and haze light up only where they cut through, and the floor sweepers scorch and spark. It is registered as "scifi/laser-grid".
var LaserGrid = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Laser grid",
		Desc:     "Security lasers between two steel posts: red beams slide and scissor across, dust and haze light up only where they cut through, and the floor sweepers scorch and spark.",
		Tags:     []string{"scifi", "laser", "security", "trap", "hazard", "beam", "ground", "loop"},
		Size:     [2]int{128, 84},
		Origin:   [2]int{64, 76},
		Duration: laserDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0408", "#4a0a12", "#8a1020", "#d01c2a", "#ff4a4a", "#ff9a90", "#ffe4e0", "#ffffff")},
			{Name: "metal", Default: pixelfx.Palette("#0e0d14", "#1e1c27", "#312e3e", "#4b4759", "#6f6a82", "#9d98b0")},
			{Name: "seed", Default: pixelfx.Num(31)},
		},
	},
	New: newLaserGrid,
}

func init() { pixelfx.Register("scifi/laser-grid", LaserGrid) }

const (
	laserDuration = 3.0
	laserPX       = 48.0
	laserTop      = -64.0
	laserIn       = laserPX - 4
)

var laserSmoke = pixelfx.Hex("#3a2a30")

// the sliding beams: [middle height, swing, sweeps a loop, phase]
var laserSlide = [3][4]float64{
	{-14, 8, 1, 0},
	{-31, 12, 2, 1.3},
	{-48, 9, 1, 3.6},
}

func laserSweepX(t, s float64) float64 {
	off := 1.9
	if s > 0 {
		off = 0
	}
	return s * (34 - 68*(0.5-0.5*math.Cos(tau*(t/laserDuration)+off)))
}

func laserSlideY(t float64, b [4]float64) float64 {
	return b[0] + b[1]*math.Sin(tau*b[2]*(t/laserDuration)+b[3])
}

func laserBeams(t float64) [5][4]float64 {
	var out [5][4]float64
	for i, b := range laserSlide {
		y := laserSlideY(t, b)
		out[i] = [4]float64{-laserIn, y, laserIn, y}
	}
	out[3] = [4]float64{-laserIn, laserTop + 6, laserSweepX(t, -1), 0}
	out[4] = [4]float64{laserIn, laserTop + 6, laserSweepX(t, 1), 0}
	return out
}

// laserDraw draws a laser: a white-hot core, a red body either side, a dithered fringe, haze lighting up along it.
func laserDraw(p *pixelfx.Painter, b [4]float64, C []uint32, t, seed float64) {
	x0, y0, x1, y1 := b[0], b[1], b[2], b[3]
	ox, oy := 0.0, 1.0
	if math.Abs(y1-y0) > math.Abs(x1-x0) {
		ox, oy = 1, 0
	}
	for _, s := range [2]float64{-1, 1} {
		c := C[3]
		if s < 0 {
			c = C[4]
		}
		p.Line(x0+s*ox, y0+s*oy, x1+s*ox, y1+s*oy, c, 1)
	}
	p.LineFn(x0, y0, x1, y1, func(u float64) (uint32, float64) {
		if pixelfx.Wrap(u*5-t*3, 1) < 0.12 {
			return C[6], 1
		}
		return pixelfx.White, 1
	})
	// the haze it passes through scatters its light, so its glow is uneven and drifts
	L := pixelfx.Hypot(x1-x0, y1-y0)
	for d := 0.0; d <= L; d += 5 {
		x, y := pixelfx.Lerp(x0, x1, d/L), pixelfx.Lerp(y0, y1, d/L)
		h := 0.5 + 0.5*pixelfx.TileFbm(x/14+(t/laserDuration)*4, y/10, 4, 0, seed, 3)
		p.Glow(x, y, 6+4*h, C[3], 0.14+0.3*h*h)
	}
}

func laserCross(a, b [4]float64) (float64, float64, bool) {
	x1, y1, x2, y2 := a[0], a[1], a[2], a[3]
	x3, y3, x4, y4 := b[0], b[1], b[2], b[3]
	d := (x1-x2)*(y3-y4) - (y1-y2)*(x3-x4)
	if math.Abs(d) < 1e-6 {
		return 0, 0, false
	}
	u := ((x1-x3)*(y3-y4) - (y1-y3)*(x3-x4)) / d
	v := -((x1-x2)*(y1-y3) - (y1-y2)*(x1-x3)) / d
	if u >= 0 && u <= 1 && v >= 0 && v <= 1 {
		return x1 + u*(x2-x1), y1 + u*(y2-y1), true
	}
	return 0, 0, false
}

func laserNear(b [4]float64, x, y float64) float64 {
	x0, y0, x1, y1 := b[0], b[1], b[2], b[3]
	dx, dy := x1-x0, y1-y0
	u := pixelfx.Clamp(((x-x0)*dx + (y-y0)*dy) / (dx*dx + dy*dy))
	ex, ey := x0+dx*u-x, y0+dy*u-y
	return math.Sqrt(ex*ex + ey*ey)
}

// laserPost draws a steel post: a lit edge, a shaded side, a groove the emitters ride in, red light where the beams meet it.
func laserPost(p *pixelfx.Painter, s float64, H, C []uint32, t float64) {
	x0 := s*laserPX - 4
	var B [3]float64
	for i, b := range laserSlide {
		B[i] = laserSlideY(t, b)
	}
	for y := laserTop + 4; y < 0; y++ {
		glint := 0.0
		for _, by := range B {
			z := (y - by) / 2.5
			glint = math.Max(glint, math.Exp(-(z * z)))
		}
		for i := range 8 {
			x := x0 + float64(i)
			inner := (s < 0 && i >= 6) || (s >= 0 && i <= 1)
			var c uint32
			switch i {
			case 0, 7:
				c = H[0]
			case 1:
				c = H[4]
			case 2:
				c = H[3]
			case 4:
				c = H[1]
			case 6:
				c = H[2]
			default:
				c = H[3]
			}
			if inner && i != 0 && i != 7 && glint > 0.35 {
				if glint > 0.75 {
					c = C[5]
				} else {
					c = C[3]
				}
			}
			p.Px(x, y, c)
		}
	}
	// bolts down the post, the head with its lens, the foot plate
	for y := laserTop + 12; y < -4; y += 9 {
		p.Px(x0+3, y, H[5])
	}
	for y := laserTop; y < laserTop+10; y++ {
		for i := -1; i < 9; i++ {
			var c uint32
			switch {
			case i == -1 || i == 8 || y == laserTop || y == laserTop+9:
				c = H[0]
			case y == laserTop+1:
				c = H[5]
			case i < 3:
				c = H[4]
			default:
				c = H[2]
			}
			p.Px(x0+float64(i), y, c)
		}
	}
	p.Px(s*laserIn, laserTop+6, pixelfx.White)
	p.Px(s*laserIn, laserTop+5, C[5])
	p.Px(s*laserIn, laserTop+7, C[4])
	for i := -3; i < 11; i++ {
		fi := float64(i)
		c2, c1 := H[4], H[2]
		if i == -3 || i == 10 {
			c2, c1 = H[0], H[0]
		}
		p.Px(x0+fi, -2, c2)
		p.Px(x0+fi, -1, c1)
		p.Px(x0+fi, 0, H[1])
	}
	// a carriage for each sliding beam, riding the groove
	for _, by := range B {
		y := pixelfx.Round(by)
		edgeI := 0
		baseX := x0 - 1
		if s < 0 {
			edgeI = 3
			baseX = x0 + 5
		}
		for j := -2; j <= 2; j++ {
			for i := range 4 {
				var c uint32
				switch {
				case j == -2 || j == 2 || i == edgeI:
					c = H[0]
				case j < 0:
					c = H[5]
				default:
					c = H[3]
				}
				p.Px(baseX+float64(i), y+float64(j), c)
			}
		}
		p.Px(s*laserIn, y, pixelfx.White)
		p.Glow(s*laserIn, y, 7, C[4], 0.55)
	}
	p.Glow(s*laserIn, laserTop+6, 8, C[4], 0.55)
}

func newLaserGrid(o *pixelfx.Options) pixelfx.Instance {
	C, H, S := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("metal"), 6), o.Seed()
	rn := pixelfx.NewRng(S)
	g := newGhosts(laserDuration, ghostOpts{
		ground: zeroGround,
		sparks: []uint32{pixelfx.White, C[6], C[4], C[3]},
		hasBox: true,
		box:    [4]float64{-62, -80, 62, 7},
	})
	// the sweepers kick up sparks and wisps of smoke where they burn the floor
	for i := range 36 {
		tb := (float64(i)/36)*laserDuration + rn.Range(0, 0.05)
		s := -1.0
		if i%2 != 0 {
			s = 1
		}
		x := laserSweepX(tb, s)
		dir := pixelfx.Sign(laserSweepX(tb+0.02, s) - x)
		if dir == 0 {
			dir = 1
		}
		a := -pi/2 - dir*rn.Range(0.2, 1.1)
		vx := math.Cos(a) * rn.Range(50, 140)
		vy := math.Sin(a) * rn.Range(50, 140)
		life := rn.Range(0.2, 0.45)
		g.add(tb, pixelfx.Spark, x, -1, vx, vy, life, pixelfx.White, 1, 0)
		if i%4 == 0 {
			svx, svy := rn.Range(-4, 4), -rn.Range(6, 14)
			slife, ssize := rn.Range(0.8, 1.2), rn.Range(1.6, 2.4)
			g.add(tb, pixelfx.Smoke, x, -2, svx, svy, slife, laserSmoke, ssize, 0)
		}
	}

	return &pixelfx.Funcs{
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			B := laserBeams(t)
			// the floor where the sweepers have just been: a scorch that cools from white to deep red
			for _, s := range [2]float64{-1, 1} {
				for k := range 14 {
					fk := float64(k)
					ago := fk * 0.025
					x := laserSweepX(t-ago, s)
					c := C[3]
					switch {
					case k < 2:
						c = pixelfx.White
					case k < 5:
						c = C[6]
					case k < 9:
						c = C[4]
					}
					p.Dot(x, 0, c, 1-fk/14)
					p.Dot(x, 1, C[3], 0.6-fk/24)
				}
			}
			// a low red pool on the floor under it all
			for x := -40.0; x <= 40; x += 20 {
				p.Glow(x, -2, 20, C[3], 0.12)
			}
			// haze hanging in the air, thickest low down, seen only where a beam passes through it and lights it red
			ph := t / laserDuration
			for y := -60.0; y < 0; y++ {
				for x := -laserIn + 1; x < laserIn; x++ {
					d := 99.0
					for _, b := range B {
						d = math.Min(d, laserNear(b, x+0.5, y+0.5))
					}
					if d > 11 {
						continue
					}
					low := 0.35 + 0.65*pixelfx.Clamp((y+60)/50)
					n := 0.5 + 0.5*pixelfx.TileFbm((x+0.5)/18+ph*3, (y+0.5)/7+0.4*math.Sin(tau*ph), 3, 0, S+7, 2)
					dens := pixelfx.Clamp((n-0.42)*2.2) * low
					if dens <= 0.02 {
						continue
					}
					lit := math.Exp(-d / 4.5)
					c := C[1]
					if lit > 0.5 {
						c = C[4]
					} else if lit > 0.15 {
						c = C[3]
					}
					p.Blend(x+0.5, y+0.5, c, dens*0.8*lit)
				}
			}
			for _, s := range [2]float64{-1, 1} {
				laserPost(p, s, H, C, t)
			}
			for _, b := range B {
				laserDraw(p, b, C, t, S)
			}
			// dust and haze motes, invisible until a beam catches them
			for i := range 90 {
				fi := float64(i)
				x := (pixelfx.Rand(S, fi)*2-1)*(laserIn-3) + 4*math.Sin(tau*(t/laserDuration)+fi)
				y := -2 - pixelfx.Rand(S, fi, 1)*62 + 3*math.Sin(tau*2*(t/laserDuration)+fi*1.7)
				d := 9.0
				for _, b := range B {
					d = math.Min(d, laserNear(b, x, y))
				}
				if d < 1.6 {
					c := C[6]
					if d < 0.9 {
						c = pixelfx.White
					}
					p.Px(x, y, c)
					p.Glow(x, y, 3, C[5], 0.45)
				}
			}
			// where beams cross, the haze between them flares
			for i := range len(B) {
				for j := i + 1; j < len(B); j++ {
					cx, cy, ok := laserCross(B[i], B[j])
					if !ok {
						continue
					}
					p.Glow(cx, cy, 9, C[5], 0.4)
					pixelfx.Sparkle(p, cx, cy, 2, C[6], C[5], 0.3)
				}
			}
			// the sweepers' hot spots on the floor
			for _, s := range [2]float64{-1, 1} {
				x := laserSweepX(t, s)
				p.Px(x, -1, pixelfx.White)
				p.Px(x-1, 0, C[6])
				p.Px(x+1, 0, C[6])
				p.Glow(x, -1, 10, C[4], 0.55)
				p.Glow(x, -1, 4, pixelfx.White, 0.35)
			}
			g.at(t).Draw(p, pixelfx.LayerBoth)
		},
	}
}
