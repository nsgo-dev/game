// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/crystal-shard.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the crystal shading (a lit ridge, bright faces turned to the light, a dark rim) and the orbiting motes from Sprite Fusion's open-source ice crystals and pickup (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// CrystalShard is a broken amber shard hovering and swaying with glowing cracks, a flaring break and orbiting splinters. It is registered as "pickups/crystal-shard".
var CrystalShard = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Crystal shard",
		Desc:     "A broken amber shard hovers and sways: chipped uneven faces, its top sheared off, light pulsing up through glowing cracks and flaring out of the break; splinters orbit it.",
		Tags:     []string{"pickup", "crystal", "shard", "relic", "broken", "loop"},
		Size:     [2]int{64, 76},
		Origin:   [2]int{32, 68},
		Duration: crystalShardDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0a04", "#3e1606", "#702a08", "#a8480c", "#e07818", "#ffa838", "#ffd890", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: newCrystalShard,
}

func init() { pixelfx.Register("pickups/crystal-shard", CrystalShard) }

const (
	crystalShardDuration = 3.2
	crystalHalf          = 6.0
	crystalTilt          = 0.18
	crystalLean          = 0.2
	crystalHover         = 27.0
	crystalRX            = 15.0
	crystalRY            = 3.8
)

func crystalRoll(pt [3]float64) [3]float64 {
	x, y, z := pt[0], pt[1], pt[2]
	return [3]float64{x*math.Cos(crystalLean) - y*math.Sin(crystalLean), x*math.Sin(crystalLean) + y*math.Cos(crystalLean), z}
}

func crystalUnit(x, y, z float64) [3]float64 {
	l := pixelfx.Hypot3(x, y, z)
	return [3]float64{x / l, y / l, z / l}
}

var crystalSolid = func() gemSolid {
	var planes []gemPlane
	deg := pi / 180
	jit := [6]float64{0, 8, -6, 4, -9, 5}
	rr := [6]float64{1, 0.86, 1.08, 0.94, 0.82, 1.04}
	low := [6]float64{6.5, 5.5, 7, 6, 5, 6.8}
	for k := range 6 {
		a := (float64(k)*60 + jit[k]) * deg
		r := 4.8 * rr[k]
		ct, cb := math.Atan2(13, r), math.Atan2(low[k], r)
		planes = append(planes,
			gemPlane{n: [3]float64{math.Cos(a), 0, math.Sin(a)}, d: r, kind: 6},
			gemPlane{n: [3]float64{math.Sin(ct) * math.Cos(a), math.Cos(ct), math.Sin(ct) * math.Sin(a)}, d: math.Sin(ct)*r + math.Cos(ct)*crystalHalf, kind: 7},
			gemPlane{n: [3]float64{math.Sin(cb) * math.Cos(a), -math.Cos(cb), math.Sin(cb) * math.Sin(a)}, d: math.Sin(cb)*r + math.Cos(cb)*crystalHalf, kind: 8},
		)
	}
	shear := crystalUnit(0.7, 0.62, 0.34)
	planes = append(planes, gemPlane{n: shear, d: shear[1]*7.2 + shear[0]*-0.6, kind: 9})
	for _, chip := range [3]struct {
		n [3]float64
		d float64
	}{
		{crystalUnit(-0.7, -0.25, 0.65), 4.0},
		{crystalUnit(0.75, 0.15, 0.6), 3.9},
		{crystalUnit(-0.55, 0.5, 0.3), 6.8},
	} {
		planes = append(planes, gemPlane{n: chip.n, d: chip.d, kind: 10})
	}
	for i := range planes {
		planes[i].n = crystalRoll(planes[i].n)
	}
	return gemSolid{planes: planes, r: 8, top: crystalHalf + 13, bottom: crystalHalf + 8}
}()

var crystalCracks = [][][3]float64{
	{{0.5, -9, 0.3}, {-0.8, -5, 0.5}, {0.6, -1, 0.2}, {-0.4, 3, 0.6}, {1.4, 6.5, 0.4}, {2.2, 8.5, 0.6}},
	{{0.6, -1, 0.2}, {2.4, 1.5, 1}, {3.4, 4.2, 1.2}},
	{{-0.4, 3, 0.6}, {-2, 6.5, 0.4}, {-2.6, 10.5, 0.2}, {-2.4, 14.5, 0.1}},
	{{-0.8, -5, 0.5}, {-2.8, -3, 0.8}},
}

func crystalPose(ph float64) (y, spin float64) {
	return -crystalHover - 1.8*math.Sin(ph*tau), 0.3 + 0.62*math.Sin(ph*tau)
}

func crystalClimb(ph float64) float64 {
	return math.Mod(ph*2, 1)*1.7 - 0.3
}

func crystalProject(pt [3]float64, spin, cy float64) (float64, float64) {
	r := crystalRoll(pt)
	x, y, z := r[0], r[1], r[2]
	xr, zr := x*math.Cos(spin)-z*math.Sin(spin), x*math.Sin(spin)+z*math.Cos(spin)
	return xr, cy - (y*math.Cos(crystalTilt) - zr*math.Sin(crystalTilt))
}

func drawCrystalCracks(L *layer, p *pixelfx.Painter, C []uint32, spin, cy, ph float64) {
	s0, rim := crystalClimb(ph), C[0]
	for _, crack := range crystalCracks {
		for i := 1; i < len(crack); i++ {
			x0, y0 := crystalProject(crack[i-1], spin, cy)
			x1, y1 := crystalProject(crack[i], spin, cy)
			n := int(math.Ceil(pixelfx.Hypot(x1-x0, y1-y0) * 2))
			for j := 0; j <= n; j++ {
				u := float64(j) / float64(n)
				x, y := x0+(x1-x0)*u, y0+(y1-y0)*u
				h := crack[i-1][1] + (crack[i][1]-crack[i-1][1])*u
				c := L.get(x, y)
				if c == 0 || c == rim {
					continue
				}
				d := ((h+9)/22 - s0) / 0.14
				b := 0.3 + math.Exp(-(d * d))
				col := C[5]
				if b > 0.9 {
					col = pixelfx.White
				} else if b > 0.55 {
					col = C[6]
				}
				p.Px(x, y, col)
				if j%4 == 0 && b > 0.5 {
					p.Glow(x+0.5, y+0.5, 2+3*b, C[5], 0.16*b)
				}
			}
		}
	}
}

func drawCrystalSplinter(SL *layer, p *pixelfx.Painter, C []uint32, x, y, ln, a float64, far bool) {
	ca, sa, X, Y := math.Cos(a), math.Sin(a), pixelfx.Round(x), pixelfx.Round(y)
	SL.begin(X-7, Y-7)
	SL.fill(X-7, Y-7, X+6, Y+6, func(px, py float64) uint32 {
		dx, dy := px-X, py-Y
		u, v := dx*ca+dy*sa, -dx*sa+dy*ca
		denom := 1.1
		if u > 0 {
			denom = 1.7
		}
		if math.Abs(u)/ln+math.Abs(v)/denom > 1 {
			return 0
		}
		vSign := 1.0
		if v < 0 {
			vSign = -1
		}
		lit := -(ca*0.6 + sa*0.8) * vSign
		if far {
			return pick(v < 0, C[4], C[3])
		}
		if math.Abs(v) < 0.5 && u < 0 {
			return pixelfx.White
		}
		return pick((v < 0) == (lit > 0), C[6], C[4])
	})
	SL.outline(pick(far, C[1], C[0])).draw(p, 0, 0, 1)
}

func drawCrystalSplinters(SL *layer, p *pixelfx.Painter, C []uint32, ph, cy float64, back bool) {
	for i := range 3 {
		fi := float64(i)
		a := ph*tau + (fi*tau)/3
		s := math.Sin(a)
		if (s < 0) != back {
			continue
		}
		x, y, near := math.Cos(a)*crystalRX, cy+1+s*crystalRY-math.Cos(a)*1.5, (s+1)/2
		drawCrystalSplinter(SL, p, C, x, y, 3+1.6*near, a*2+fi*1.7, back)
		p.Glow(x, y, 6+3*near, C[5], pick(back, 0.18, 0.32)*(0.6+0.4*near))
		if !back && s > 0.92 {
			glint(p, x-1, y-1, (2.5*(s-0.92))/0.08, pixelfx.White, 0.4)
		}
	}
}

func newCrystalShard(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	D := float64(crystalShardDuration)
	L, SL := newLayer(32, 52), newLayer(16, 16)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			y, _ := crystalPose(ph)
			blobShadow(p, 0, 6, 0.75, 0)
			pool(p, 0, 0.5, 20, 3.5, C[4], 0.3)
			p.GlowRing(0, y, 7, 20, C[4], 0.22)
			drawCrystalSplinters(SL, p, C, ph, y, true)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			y, spin := crystalPose(ph)
			s0 := crystalClimb(ph)
			drawGem(L, p, crystalSolid, 0, y, spin, C, drawGemOpts{tilt: crystalTilt, a: 1, edges: true, gain: 0.6})
			drawCrystalCracks(L, p, C, spin, y, ph)
			ins := (s0 - 0.45) / 0.45
			inside := math.Exp(-(ins * ins))
			p.Glow(0, y, 10, C[5], 0.2*inside)
			drawCrystalSplinters(SL, p, C, ph, y, false)
			fx, fy := crystalProject([3]float64{1.8, 8.4, 0.9}, spin, y)
			out := pixelfx.Clamp(1 - math.Abs(s0-1.05)/0.18)
			if out > 0 {
				glint(p, fx, fy, 3.5*out, pixelfx.White, 0.55*out)
				p.Glow(fx, fy, 12, C[5], 0.4*out)
			}
			P2 := D / 2
			burst := ((1.05 + 0.3) / 1.7) * P2
			for i := range 7 {
				fi := float64(i)
				born := burst + pixelfx.Rand(fi, seed, 1)*0.12
				at := pixelfx.Wrap(t-born, P2)
				life := 0.6 + pixelfx.Rand(fi, seed, 2)*0.45
				if at > life {
					continue
				}
				k := 1 - at/life
				x := fx + (pixelfx.Rand(fi, seed, 3)-0.5)*4 + math.Sin(at*5+fi)*2*(at/life)
				yy := fy - at*(14+pixelfx.Rand(fi, seed, 4)*14)
				col := C[5]
				if k > 0.6 {
					col = pixelfx.White
				} else if k > 0.3 {
					col = C[6]
				}
				p.Px(x, yy, col)
				p.Glow(x+0.5, yy+0.5, 3, C[5], 0.35*k)
			}
		},
	}
}
