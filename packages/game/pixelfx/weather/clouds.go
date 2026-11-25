// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/clouds.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the banded volumes with a lit crest from Sprite Fusion's open-source fog (https://www.spritefusion.com/pixel-art-vfx/fog), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Clouds is layers of clouds drifting past a bright moon at night: silver-lit tops, dark bellies, edges that flare where they cross the moon, forming and thinning as they go. It is registered as "weather/clouds".
var Clouds = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Clouds",
		Desc:     "Layers of clouds drift past a bright moon at night: silver-lit tops, dark bellies, edges that flare where they cross the moon, forming and thinning as they go.",
		Tags:     []string{"weather", "clouds", "moon", "night", "sky", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 180},
		Duration: cloudsDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0c0c18", "#16172a", "#232540", "#363a5a", "#545a80", "#8088ae", "#bcc4e2", "#f2f4ff")},
			{Name: "seed", Default: pixelfx.Num(7)},
		},
	},
	New: newClouds,
}

func init() { pixelfx.Register("weather/clouds", Clouds) }

const (
	cloudsDuration = 12
	cloudsMX       = 92.0
	cloudsMY       = -128.0
	cloudsMR       = 12.0
)

type cloudLayer struct {
	n     int
	w, h  [2]float64
	y     [2]float64
	v     float64
	life  [2]float64
	tone  [4]int
	alpha float64
}

var cloudLayers = []cloudLayer{
	{n: 7, w: [2]float64{40, 70}, h: [2]float64{6, 11}, y: [2]float64{-150, -110}, v: 3, life: [2]float64{8, 11}, tone: [4]int{2, 3, 4, 5}, alpha: 0.7},
	{n: 6, w: [2]float64{60, 100}, h: [2]float64{9, 15}, y: [2]float64{-120, -70}, v: 6, life: [2]float64{8, 11}, tone: [4]int{1, 2, 4, 6}, alpha: 0.88},
	{n: 5, w: [2]float64{90, 140}, h: [2]float64{12, 20}, y: [2]float64{-80, -30}, v: 10, life: [2]float64{9, 12}, tone: [4]int{1, 2, 3, 6}, alpha: 1},
}

type cloudPuff struct {
	px, py, r float64
}

type cloudsInst struct {
	S     float64
	C     []uint32
	puffs []cloudPuff
}

func newClouds(o *pixelfx.Options) pixelfx.Instance {
	return &cloudsInst{S: o.Seed(), C: pixelfx.Fit(o.Palette(), 8)}
}

func (cl *cloudsInst) Update(float64)                     {}
func (cl *cloudsInst) DrawBack(*pixelfx.Painter, float64) {}

func (cl *cloudsInst) Draw(p *pixelfx.Painter, t float64) {
	q := clip(p, &Clouds.Meta)
	drawMoon(q, cl.C)
	for j, L := range cloudLayers {
		for i := 0; i < L.n; i++ {
			cl.puffs = drawCloud(q, t, cl.S, cl.C, L, j, i, cl.puffs[:0])
		}
	}
	q.glow(cloudsMX, cloudsMY, 110, cl.C[4], 0.14)
	q.glow(cloudsMX, cloudsMY, 40, cl.C[5], 0.25)
	q.glow(cloudsMX, cloudsMY, 18, cl.C[6], 0.3)
}

func drawMoon(q *clipper, C []uint32) {
	q.shade(cloudsMX-cloudsMR-1, cloudsMY-cloudsMR-1, cloudsMX+cloudsMR+1, cloudsMY+cloudsMR+1, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x-cloudsMX, y-cloudsMY
		d := pixelfx.Hypot(dx, dy) / cloudsMR
		if d > 1 {
			return 0
		}
		sea := (dx+4)*(dx+4)+(dy+2)*(dy+2) < 14 || (dx-3)*(dx-3)+(dy-4)*(dy-4) < 9 || (dx-5)*(dx-5)+(dy+5)*(dy+5) < 4
		switch {
		case d > 0.86:
			if dx+dy < 0 {
				out.C = C[6]
			} else {
				out.C = pixelfx.White
			}
		case sea:
			out.C = C[5]
		case d < 0.5 && dx+dy < 0:
			out.C = pixelfx.White
		default:
			out.C = C[7]
		}
		return 0
	})
}

func drawCloud(q *clipper, t, S float64, C []uint32, L cloudLayer, j, i int, puffs []cloudPuff) []cloudPuff {
	fj, fi := float64(j), float64(i)
	R := func(k float64) float64 { return pixelfx.Rand(S, fj, fi, k) }
	life := L.life[0] + R(1)*(L.life[1]-L.life[0])
	age := pixelfx.Wrap(t-R(2)*cloudsDuration, cloudsDuration)
	if age >= life {
		return puffs
	}
	u := age / life
	grow := math.Min(1, u*4) * math.Min(1, (1-u)*4)
	w := (L.w[0] + R(3)*(L.w[1]-L.w[0])) * (0.6 + 0.4*grow)
	x0 := -200 + R(4)*330 + L.v*age
	base := L.y[0] + R(5)*(L.y[1]-L.y[0])
	H := (L.h[0] + R(6)*(L.h[1]-L.h[0])) * grow
	if H < 1.5 {
		return puffs
	}
	n := 4 + int(math.Floor(R(7)*4))
	for k := 0; k < n; k++ {
		fk := float64(k)
		f := (fk + 0.5) / float64(n)
		r := H * (0.5 + 0.35*math.Sin(pi*f)) * (0.75 + 0.5*R(10+fk)) * (1 + 0.06*math.Sin((tau*t)/cloudsDuration+fk+fi))
		puffs = append(puffs, cloudPuff{
			px: x0 + w*(f-0.5) + (R(20+fk)-0.5)*6,
			py: base - r*0.3 - R(30+fk)*H*0.15,
			r:  r,
		})
	}
	n2 := 1 + int(math.Floor(R(8)*3))
	for k := 0; k < n2; k++ {
		fk := float64(k)
		f := 0.3 + R(40+fk)*0.4
		r := H * (0.75 + 0.35*R(50+fk)) * (1 + 0.05*math.Sin((tau*t)/cloudsDuration+fk*2+fi))
		puffs = append(puffs, cloudPuff{
			px: x0 + w*(f-0.5),
			py: base - H*0.55 - r*0.2,
			r:  r,
		})
	}
	x1, x2 := x0-w/2-H, x0+w/2+H
	B := box(&Clouds.Meta)
	ix0 := max(B.x0, int(math.Floor(x1)))
	ix1 := min(B.x1-1, int(math.Ceil(x2)))
	for x := ix0; x <= ix1; x++ {
		cx := float64(x) + 0.5
		top, top2 := math.Inf(1), math.Inf(1)
		for _, pf := range puffs {
			dx := cx - pf.px
			if dx*dx >= pf.r*pf.r {
				continue
			}
			y := pf.py - math.Sqrt(pf.r*pf.r-dx*dx)
			if y < top {
				top2 = top
				top = y
			} else if y < top2 {
				top2 = y
			}
		}
		if math.IsInf(top, 1) {
			continue
		}
		end := math.Min(cx-x1, x2-cx) / H
		bot := base + 1.5 + math.Sin(cx*0.21+fi)*0.8
		if end < 1 {
			bot -= (1 - end) * 4
		}
		if bot <= top {
			continue
		}
		h := bot - top
		inner := -999
		if top2-top > 2.5 && top2 < bot-2 {
			inner = int(math.Floor(top2))
		}
		for y := int(math.Floor(top)); float64(y) < bot; y++ {
			fy := float64(y)
			d := fy + 0.5 - top
			v := d / h
			th := bayer(float64(x), fy) * 0.12
			md := pixelfx.Hypot(cx-cloudsMX, fy-cloudsMY)
			var c int
			switch {
			case d < 1:
				c = L.tone[3]
			case v < 0.25+th:
				c = L.tone[2]
			case v < 0.62+th:
				c = L.tone[1]
			default:
				c = L.tone[0]
			}
			if y == inner {
				c = max(c, L.tone[2])
			}
			if bot-fy-0.5 < 1 && v > 0.5 {
				c = max(0, L.tone[0]-1)
			}
			if md < 46 && (d < 2 || y == inner || v < 0.3) {
				add := 1
				if md < 24 {
					add = 2
				}
				c = min(7, c+add)
			}
			a := L.alpha * math.Min(1, grow*1.5)
			if a >= 1 {
				q.px(float64(x), fy, C[c])
			} else {
				q.blend(float64(x), fy, C[c], a)
			}
			if d < 1 && md < 46 {
				q.light(float64(x), fy, C[6], 0.35*(1-md/46))
			}
		}
	}
	return puffs
}
