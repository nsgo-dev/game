// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/waterfall.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the pool's caustic light (two warped sine nets, light where they cross zero) from Sprite Fusion's open-source bubbles (https://www.spritefusion.com/pixel-art-vfx/bubbles), MIT License, Copyright (c) 2026 Hugo Duprez.

package water

import (
	"math"
	"sync"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Waterfall pours a river from a notch in a rock outcrop: a white curl, a deep sheet that splits into strands, churning foam, spray, drifting mist and rings on a dark pool. It is registered as "water/waterfall".
var Waterfall = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Waterfall",
		Desc:     "A river pours from a notch in a rock outcrop: a white curl, a deep sheet that splits into strands, churning foam, spray, drifting mist and rings on a dark pool.",
		Tags:     []string{"water", "waterfall", "cascade", "foam", "mist", "ambient", "loop"},
		Size:     [2]int{144, 136},
		Origin:   [2]int{72, 118},
		Duration: waterfallDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "height", Default: pixelfx.Num(92)},
			{Name: "width", Default: pixelfx.Num(13)},
			{Name: "ledge", Default: pixelfx.Bool(true)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newWaterfall,
}

func init() { pixelfx.Register("water/waterfall", Waterfall) }

const (
	waterfallDuration = 2
	waterfallSY       = 0.2
	waterfallG        = 760
	waterfallPoolRX   = 56
	waterfallPoolRY   = 11
)

var cross4x2 = [4][2]float64{{2, 0}, {-2, 0}, {0, 2}, {0, -2}}

// waterfallHalfW returns half the sheet's width at depth d below the lip: it spreads a little as it falls.
func waterfallHalfW(W, d, H float64) float64 {
	return W + 3.2*math.Pow(pixelfx.Clamp(d/H), 1.6)
}

// waterfallSheet draws the falling sheet, in 1 px lanes.
func waterfallSheet(p *pixelfx.Painter, C []uint32, ph, H, W, S float64) {
	D := float64(waterfallDuration)
	for y := -H + 2; y <= -1; y++ {
		d := y + 0.5 + H
		v := d / H
		s := math.Sqrt(d + 3)
		hw0 := waterfallHalfW(W, d, H)
		split := pixelfx.Smooth(0.56, 0.95, v)
		glass := 1 - pixelfx.Smooth(4, 24, d)
		wl := pixelfx.TileNoise(2, s*1.4-ph*12, 0, 12, S+2) * (0.4 + 1.8*v)
		wr := pixelfx.TileNoise(11, s*1.4-ph*12, 0, 12, S+2) * (0.4 + 1.8*v)
		fan := 0.0
		if split > 0 {
			fan = pixelfx.TileNoise(3, s*0.8-ph*4, 0, 4, S+61) * 1.6
		}
		sd, sb := s*1.5-ph*20, s*0.45-ph*6
		for X := -W - 8; X <= W+8; X++ {
			x := X + 0.5
			hw := hw0 + wr
			if x < 0 {
				hw = hw0 + wl
			}
			ax := math.Abs(x)
			if ax > hw {
				continue
			}
			e := ax / hw
			edge := false
			if split > 0 {
				f := pixelfx.Wrap((x/(1+0.22*split)+fan+64)/5.4, 1)
				if f > 1-0.46*split {
					continue
				}
				edge = f < 0.2
			}
			dash := pixelfx.TileNoise(X*1.618+0.31, sd, 0, 20, S)
			if pixelfx.Rand(X, S+60) < 0.48 && e < 0.86 && !edge {
				c, a := C[2], 0.42+0.15*v
				if dash > 0.45 {
					c, a = C[4], 0.7
				} else if glass > 0.5 {
					c = C[3]
				}
				p.Blend(x, y+0.5, c, a)
				continue
			}
			broad := pixelfx.TileNoise(math.Floor((x+40)/3)*2.7, sb, 0, 6, S+1)
			leftW := 0.3
			if x < 0 {
				leftW = 1
			}
			rightShade := 0.0
			if x > 0 {
				rightShade = 0.12 * e
			}
			edgeBoost := 0.0
			if edge {
				edgeBoost = 0.3
			}
			b := -0.34 + 0.46*v + 0.3*broad - glass*0.5 + 0.35*pixelfx.Smooth(0.7, 1, e)*leftW - rightShade + edgeBoost
			if dash > 0.2-0.22*v {
				b += 0.36 + 0.7*(dash-0.2)
			}
			if b > -0.2 {
				var c uint32
				switch {
				case b > 0.62:
					c = pixelfx.White
				case b > 0.34:
					c = C[6]
				case b > 0.08:
					c = C[5]
				default:
					c = C[4]
				}
				p.Px(X, y, c)
			} else {
				p.Blend(x, y+0.5, C[3], 0.8)
			}
		}
	}
	// strands peel off the edges and fall free
	pixelfx.Cycle(ph*D, D, 16, S+30, pixelfx.Fixed(0.4), func(age, life float64, r *pixelfx.Rng, _ int) {
		side := r.Sign()
		d0 := r.Range(14, H*0.7)
		d := d0 + 50*age + 260*age*age
		k := age / life
		if d > H-6 {
			return
		}
		x := side * (waterfallHalfW(W, d, H) + 1.5 + age*r.Range(6, 16))
		y := -H + d
		ln := 2 + 6*k
		p.LineFn(x, y-ln, x, y, func(u float64) (uint32, float64) {
			if u > 0.7 {
				return C[6], 1 - k*0.5
			}
			return C[4], 1 - k*0.5
		})
	})
}

type rockMapKey struct {
	W, H, S float64
}

type rockMapData struct {
	id  []int8
	lit []float32
}

var (
	waterfallRocksMu sync.Mutex
	waterfallRocks   = map[rockMapKey]*rockMapData{}
)

func buildRockMap(lumps [6][4]float64, x0, y0 float64, w, hgt int, Lx, Ly, Lz, S float64) *rockMapData {
	id := make([]int8, w*hgt)
	for i := range id {
		id[i] = -1
	}
	lit := make([]float32, w*hgt)
	for j := 0; j < hgt; j++ {
		for i := 0; i < w; i++ {
			x, y := x0+float64(i)+0.5, y0+float64(j)+0.5
			best := int8(-1)
			z, l := -1e9, 0.0
			for k, lm := range lumps {
				cx, cy, rx, ry := lm[0], lm[1], lm[2], lm[3]
				nx, ny := (x-cx)/rx, (y-cy)/ry
				r2 := nx*nx + ny*ny
				if r2 >= 1 {
					continue
				}
				nz := math.Sqrt(1 - r2)
				bias := 0.0
				switch {
				case k == 5:
					bias = -6
				case k == 2:
					bias = -3
				case k > 2:
					bias = -1
				}
				depth := nz*ry + bias
				if depth > z {
					z, best, l = depth, int8(k), nx*Lx+ny*Ly+nz*Lz
				}
			}
			id[j*w+i] = best
			lit[j*w+i] = float32(l + 0.2*pixelfx.TileNoise(x*0.32, y*0.32, 0, 0, S+70))
		}
	}
	return &rockMapData{id: id, lit: lit}
}

// waterfallOutcrop draws the outcrop the river pours from.
func waterfallOutcrop(p *pixelfx.Painter, C []uint32, ph, H, W, S float64) {
	ST := rock()
	const Lx, Ly, Lz = -0.55, -0.72, 0.42
	lumps := [6][4]float64{
		{-W - 11, -H - 4, 13, 10},
		{W + 8, -H - 2, 9, 7.5},
		{0, -H + 3.5, W + 3, 4.2},
		{-W - 21, -H + 4, 7, 5.5},
		{W + 15, -H + 4.5, 5, 4},
		{-2, -H - 5, W + 7, 6.5},
	}
	x0, x1, y0, y1 := -W-29, W+21, -H-15, -H+10
	w, hgt := int(x1-x0+1), int(y1-y0+1)
	key := rockMapKey{W: W, H: H, S: S}
	waterfallRocksMu.Lock()
	rm := waterfallRocks[key]
	if rm == nil {
		rm = buildRockMap(lumps, x0, y0, w, hgt, Lx, Ly, Lz, S)
		waterfallRocks[key] = rm
	}
	waterfallRocksMu.Unlock()
	id, lit := rm.id, rm.lit
	// a soft shadow under it on whatever is behind
	p.Each(-W-26, -H, W+18, -H+20, func(x, y float64, sx, sy int) {
		k := 1 - pixelfx.Hypot((x+4)/(W+22), (y+H)/20)
		if k > 0 {
			p.BlendPx(sx, sy, ST[0], 0.4*math.Min(1, k*3))
		}
	})
	at := func(i, j int) int8 {
		if i < 0 || j < 0 || i >= w || j >= hgt {
			return -1
		}
		return id[j*w+i]
	}
	for j := 0; j < hgt; j++ {
		for i := 0; i < w; i++ {
			k := id[j*w+i]
			x, y := x0+float64(i), y0+float64(j)
			// the river runs over the sill: nothing of the sill shows through it
			if (k == 2 || k == 5 || k < 0) && y >= -H-5 && y < -H+1 && math.Abs(x+0.5) < W+1.5 {
				continue
			}
			if k < 0 {
				if at(i-1, j) >= 0 || at(i+1, j) >= 0 || at(i, j-1) >= 0 || at(i, j+1) >= 0 {
					p.Px(x, y, ST[0])
				}
				continue
			}
			// a crease where one boulder sits in front of another
			r, b := at(i+1, j), at(i, j+1)
			if (r >= 0 && r != k && r < k) || (b >= 0 && b != k) {
				p.Px(x, y, ST[1])
				continue
			}
			l := float64(lit[j*w+i])
			wet := math.Abs(x+0.5) < W+6 && y > -H-9
			var c uint32
			switch {
			case l > 0.9:
				c = ST[5]
			case l > 0.64:
				c = ST[4]
			case l > 0.3:
				c = ST[3]
			case l > -0.05:
				c = ST[2]
			default:
				c = ST[1]
			}
			if k == 5 {
				switch {
				case l > 0.7:
					c = ST[3]
				case l > 0.2:
					c = ST[2]
				default:
					c = ST[1]
				}
			}
			if at(i, j-1) < 0 && l > 0.25 {
				c = ST[5]
			}
			if wet {
				// wet stone: a step darker, a cool gloss running down it where the light catches
				switch {
				case l > 0.9:
					c = ST[4]
				case l > 0.64:
					c = ST[3]
				case l > 0.3:
					c = ST[2]
				default:
					c = ST[1]
				}
				if l > 0.3 && pixelfx.TileNoise(x*0.9, y*0.6-ph*8, 0, 8, S+71) > 0.42 {
					if l > 0.6 {
						c = C[6]
					} else {
						c = C[4]
					}
				}
			}
			p.Px(x, y, c)
		}
	}
	// cracks: dark lines with a lit lip above them
	for q := 0; q < 3; q++ {
		L := lumps[q%2]
		xf, yf := 0.15, -0.25
		if q == 2 {
			xf, yf = -0.5, 0.1
		}
		cx, cy := math.Floor(L[0]+xf*L[2]), math.Floor(L[1]+L[3]*yf)
		ln := 3 + int(math.Floor(pixelfx.Rand(float64(q), S+74)*4))
		dx := -1.0
		if pixelfx.Rand(float64(q), S+75) < 0.5 {
			dx = 1
		}
		for n := 0; n <= ln; n++ {
			x, y := cx+pixelfx.Round(float64(n)*0.6)*dx, cy+float64(n)
			i, j := int(x-x0), int(y-y0)
			if at(i, j) < 0 || at(i, j) > 1 || at(i, j+1) < 0 {
				break
			}
			c := ST[0]
			if n == ln {
				c = ST[1]
			}
			p.Px(x, y, c)
			if n == 0 {
				p.Px(x-dx, y-1, ST[4])
			}
		}
	}
	// trickles run down the wet faces either side of the notch
	for _, sx := range [2]float64{-W - 3, W + 3} {
		for y := -H - 2; y < -H+7; y++ {
			g := pixelfx.Wrap(y*0.35-ph*6, 1)
			i, j := int(math.Floor(sx)-x0), int(math.Floor(y)-y0)
			if at(i, j) >= 0 {
				c := C[3]
				if g < 0.3 {
					c = C[6]
				}
				p.Px(sx, y, c)
			}
		}
	}
}

// waterfallRiver draws the river sliding out of the notch between the boulders, seen from just above.
func waterfallRiver(p *pixelfx.Painter, C []uint32, ph, H, W, S float64) {
	top := -H - 6
	p.Shade(-W-3, top, W+3, -H, func(x, y float64, out *pixelfx.Out) uint32 {
		back := top + 1.5 + pixelfx.TileNoise(x*0.3, 4, 0, 0, S+62)*1.5
		v := (y - back) / (-H - back)
		hw := W + 2 - (1-v)*3
		if y < back || math.Abs(x) > hw {
			return 0
		}
		n := pixelfx.TileNoise(x*0.32, (y-top)*0.7-ph*8, 0, 8, S+3)
		switch {
		case n > 0.66:
			out.C = C[6]
		case n > 0.4:
			out.C = C[5]
		case v < 0.5:
			out.C = C[3]
		default:
			out.C = C[4]
		}
		return 0
	})
}

// waterfallCurl draws the curl over the edge: glassy blue as it rolls, a hard white line along the bend; its light on the stone.
func waterfallCurl(p *pixelfx.Painter, C []uint32, ph, H, W, S float64) {
	D := float64(waterfallDuration)
	for x := -W; x <= W; x++ {
		g := pixelfx.TileNoise(x*0.45, ph*4, 0, 4, S+4)
		p.Px(x, -H-1, C[4])
		p.Px(x, -H, pixelfx.White)
		c := C[5]
		if g > 0.2 {
			c = C[6]
		}
		p.Px(x, -H+1, c)
	}
	p.GlowLine(-W, -H, W, -H, 6, C[6], 0.18)
	p.Glow(0, -H, 34, C[5], 0.2)
	// glints run along it
	pixelfx.Cycle(ph*D, D, 4, S+5, pixelfx.Fixed(0.36), func(age, life float64, r *pixelfx.Rng, _ int) {
		k := math.Sin((pi * age) / life)
		x := r.Range(-W, W)
		if k > 0.5 {
			for _, d := range cross4 {
				p.Px(x+d[0], -H+d[1], C[6])
			}
		}
		p.Glow(x, -H, 6, C[5], 0.4*k)
	})
}

// waterfallPool draws the pool: dark see-through water, a lit far rim, caustic light swimming in it, the falls' reflection.
func waterfallPool(p *pixelfx.Painter, C []uint32, ph, W, S float64) {
	const RX, RY, cy = waterfallPoolRX, waterfallPoolRY, 2.0
	T := ph * tau
	var cols [RX * 2][2]float64
	for X := -RX; X < RX; X++ {
		x := float64(X) + 0.5
		cols[X+RX] = [2]float64{math.Sin(x*0.11 - T*2), math.Sin(T+x*0.05)*1.4 + x*0.13}
	}
	for Y := math.Floor(cy - RY); Y < cy+RY; Y++ {
		y := Y + 0.5
		dy := (y - cy) / RY
		sy := math.Sin(y*0.5 + T)
		for X := -RX; X < RX; X++ {
			x := float64(X) + 0.5
			dx := x / RX
			d := dx*dx + dy*dy
			if d > 1 {
				continue
			}
			c := C[1]
			if d < 0.12 {
				c = C[2]
			}
			p.Blend(x, y, c, 0.62*math.Min(1, (1-d)*4))
			// caustics: two warped sine nets, light where either crosses zero
			cx1, cx2 := cols[X+RX][0], cols[X+RX][1]
			u, w := x*0.2+1.8*sy, y*0.55+1.8*cx1
			f := (math.Max(0, 1-math.Abs(math.Sin(u)+math.Sin(w))*2.6) + 0.6*math.Max(0, 1-math.Abs(math.Sin(cx2+y*0.3))*3)) * (1 - d)
			if f > 0.5 {
				p.Light(x, y, C[5], 0.18*f)
			}
		}
	}
	// a few glints sit on the far shore
	for i := 0; i < 7; i++ {
		a := pi * (1.12 + 0.76*((float64(i)+0.5)/7))
		x, y := math.Cos(a)*RX*0.96, cy+math.Sin(a)*RY*0.96
		g := math.Sin(ph*tau*2 + float64(i)*2.4)
		if g > 0.2 {
			c := C[4]
			if g > 0.8 {
				c = C[6]
			}
			p.Px(x, y, c)
		}
	}
	// the falls mirrored in the water, broken by the chop
	p.Shade(-W-3, 3, W+3, 10, func(x, y float64, out *pixelfx.Out) uint32 {
		n := pixelfx.TileNoise(x*0.9, (y-3)*0.35+ph*6, 0, 6, S+6)
		k := 1 - (y-3)/7
		if math.Abs(x) > W+2-(y-3)*0.4 || n < 0.25+(1-k)*0.4 {
			return 0
		}
		if n > 0.6 && k > 0.6 {
			out.C = C[6]
		} else {
			out.C = C[5]
		}
		return 0
	})
}

// waterfallFoam draws the foot: a churning white band, billows rolling out of it, spray thrown clear landing with rings.
func waterfallFoam(p *pixelfx.Painter, C []uint32, t, ph, W, S float64) {
	D := float64(waterfallDuration)
	R := W + 10
	p.Shade(-R, -13, R, 3, func(x, y float64, out *pixelfx.Out) uint32 {
		ax := math.Abs(x)
		lump := pixelfx.TileNoise(x*0.22, ph*6, 0, 6, S+7)*3 + pixelfx.TileNoise(x*0.55+9, ph*12, 0, 12, S+8)*1.5
		top := -8 + lump + math.Pow(ax/R, 2)*7
		if ax > R-math.Max(0, -y-1)*0.6 || y < top {
			return 0
		}
		// the churn in lumps: each boil lit on top, shadowed under, darker where it meets the pool
		n := pixelfx.TileNoise(x*0.5, (y-top)*0.5-ph*10, 0, 10, S+9)
		boil := pixelfx.TileNoise(x*0.42+3, y*0.42-ph*8, 0, 8, S+12)
		u := y - top
		switch {
		case y > 1:
			if n > 0 {
				out.C = C[5]
			} else {
				out.C = C[4]
			}
		case u < 1.2:
			out.C = pixelfx.White
		case boil < -0.3:
			if u > 4 {
				out.C = C[4]
			} else {
				out.C = C[5]
			}
		case boil < -0.05:
			out.C = C[6]
		default:
			if n > -0.2 {
				out.C = pixelfx.White
			} else {
				out.C = C[6]
			}
		}
		return 0
	})
	pixelfx.Cycle(t, D, 30, S+10, pixelfx.Fixed(0.7), func(age, life float64, r *pixelfx.Rng, _ int) {
		k := age / life
		side := r.Sign()
		x0 := r.Range(-W, W) * 0.9
		sp := r.Range(2, 8)
		up := r.Range(2, 8)
		x := x0 + side*pixelfx.Ease.OutCubic(k)*sp
		y := -r.Range(2, 5) - pixelfx.Ease.OutCubic(k)*up
		rad := r.Range(1.4, 3) * math.Pow(math.Sin(pi*k), 0.7)
		if rad > 0.6 {
			billow(p, x, y, rad, C, 1)
		}
	})
	// spray: beads thrown clear of the churn, landing in the pool with a ring
	pixelfx.Cycle(t, D, 18, S+11, pixelfx.Fixed(1), func(age, _ float64, r *pixelfx.Rng, _ int) {
		side := r.Sign()
		x0 := side * r.Range(2, W+4)
		vx := side * r.Range(12, 60)
		vy := -r.Range(70, 150)
		z := r.Range(-3, 9)
		s := 1.0
		if r.Next() < 0.3 {
			s = 2
		}
		land := z * waterfallSY
		tl := (-vy + math.Sqrt(vy*vy+2*waterfallG*(land+5))) / waterfallG
		if age < tl {
			bead(p, x0+vx*age, -5+vy*age+0.5*waterfallG*age*age, vx, vy+waterfallG*age, s, C, 1)
		} else if age < tl+0.35 {
			ring(p, x0+vx*tl, land, 1+(age-tl)*12, waterfallSY, 1-(age-tl)/0.35, C, ringOpts{n: 1})
		}
	})
}

// waterfallMist draws soft puffs rolled out of the churn that drift sideways along the pool, swell and thin, with a cool light.
func waterfallMist(p *pixelfx.Painter, C []uint32, t, S float64, n int, seed float64) {
	D := float64(waterfallDuration)
	pixelfx.Cycle(t, D, n, S+seed, func(g *pixelfx.Rng) float64 { return g.Range(1.8, 2.8) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		k := age / life
		side := r.Sign()
		drift := pixelfx.Ease.OutCubic(k)
		x := side * (r.Range(3, 12) + drift*r.Range(24, 48))
		y := -r.Range(2, 7) - k*r.Range(3, 12)
		rad := r.Range(4, 6) + k*r.Range(5, 9)
		a := r.Range(0.2, 0.3) * math.Min(1, k/0.12) * math.Pow(1-k, 1.7)
		p.DiscFn(x, y, rad, func(d, _ float64) (uint32, float64) { return C[6], a * (1 - d*d) })
		if a > 0.05 {
			p.Glow(x, y, rad*1.2, C[4], 0.35*a)
		}
	})
}

func newWaterfall(o *pixelfx.Options) pixelfx.Instance {
	C, H, W, ledge, S := pixelfx.Fit(o.Palette(), 8), o.Num("height"), o.Num("width"), o.Bool("ledge"), o.Seed()
	D := float64(waterfallDuration)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			waterfallPool(p, C, ph, W, S)
			for i := 0; i < 3; i++ {
				f := math.Mod(ph+float64(i)/3, 1)
				ring(p, 0, 1, W+6+f*38, waterfallSY, 0.95*math.Pow(1-f, 1.5), C, ringOpts{n: 1, half: -1, seed: float64(i + 1)})
			}
			waterfallMist(p, C, t, S, 7, 20)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			T := ph * tau
			for i := 0; i < 3; i++ {
				f := math.Mod(ph+float64(i)/3, 1)
				glint := 0.0
				if f < 0.4 {
					glint = 1 - f*2.5
				}
				ring(p, 0, 1, W+6+f*38, waterfallSY, 0.95*math.Pow(1-f, 1.5), C, ringOpts{n: 1, half: 1, seed: float64(i + 1), glint: glint})
			}
			waterfallRiver(p, C, ph, H, W, S)
			if ledge {
				waterfallOutcrop(p, C, ph, H, W, S)
			}
			waterfallSheet(p, C, ph, H, W, S)
			waterfallCurl(p, C, ph, H, W, S)
			waterfallFoam(p, C, t, ph, W, S)
			waterfallMist(p, C, t, S, 5, 21)
			// glints wink on the falling water and in the spray
			pixelfx.Cycle(t, D, 12, S+50, func(g *pixelfx.Rng) float64 { return g.Range(0.2, 0.4) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				k := math.Sin((pi * age) / life)
				low := r.Next() < 0.45
				var d, x float64
				if low {
					d = H - r.Range(4, 16)
					x = r.Range(-W-10, W+10)
				} else {
					d = r.Range(12, H-16)
					x = r.Range(-0.8, 0.8) * waterfallHalfW(W, d, H)
				}
				dy := -10.0
				if low {
					dy = 6
				}
				y := -H + d - (age/life)*dy
				p.Px(x, y, pixelfx.White)
				if k > 0.55 {
					for _, q := range cross4 {
						p.Px(x+q[0], y+q[1], C[6])
					}
				}
				if k > 0.85 && r.Next() < 0.5 {
					for _, q := range cross4x2 {
						p.Px(x+q[0], y+q[1], C[5])
					}
				}
				p.Glow(x, y, 5, C[5], 0.35*k)
			})
			// fine spray hangs in the air off the foot, drifting up and out
			pixelfx.Cycle(t, D, 34, S+51, func(g *pixelfx.Rng) float64 { return g.Range(0.9, 1.6) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				k := age / life
				side := r.Sign()
				x := side*(r.Range(0, W+6)+k*r.Range(6, 22)) + math.Sin(age*5+r.Range(0, tau))*1.2
				y := -r.Range(3, 10) - k*r.Range(10, 34)
				if k > 0.7 && r.Next() < (k-0.7)*3.3 {
					return
				}
				c := C[6]
				if k < 0.4 && r.Next() < 0.5 {
					c = pixelfx.White
				}
				p.Px(x, y, c)
			})
			// light: down the sheet, a pool of it where the water lands, on the water around
			p.GlowLine(0, -H+6, 0, -8, 26, C[4], 0.12)
			p.Glow(0, -5, 40, C[5], 0.32+0.05*math.Sin(T*2))
			p.Glow(0, 4, 50, C[3], 0.14)
		},
	}
}
