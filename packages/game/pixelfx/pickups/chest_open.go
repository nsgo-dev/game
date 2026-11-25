// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/chest-open.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the collection pop (white squash, a ring snapping out, star sparks) and the glints from Sprite Fusion's open-source pickup (https://www.spritefusion.com/pixel-art-vfx/pickup), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// ChestOpen is a banded chest that rattles with gold light leaking from its seam, kicks its lid open, fans rays up, and launches coins and gems that arc out, bounce, glint and get collected. It is registered as "pickups/chest-open".
var ChestOpen = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Chest open",
		Desc:     "A banded chest rattles with gold light leaking from its seam, the lid kicks open, rays fan up, and coins and gems arc out, bounce, glint and get collected.",
		Tags:     []string{"pickup", "chest", "treasure", "loot", "reward", "gold", "one-shot"},
		Size:     [2]int{176, 112},
		Origin:   [2]int{88, 96},
		Duration: 4.6,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1e1206", "#4a2c08", "#86520c", "#c48412", "#f0b424", "#ffe066", "#fff6c4", "#ffffff")},
			{Name: "wood", Default: pixelfx.Palette("#110a10", "#24131a", "#3a1f1f", "#552d24", "#73402c", "#925737", "#b07246")},
			{Name: "coins", Default: pixelfx.Num(12)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newChestOpen,
}

func init() { pixelfx.Register("pickups/chest-open", ChestOpen) }

const (
	chestTOpen    = 0.8
	chestTCollect = 2.55
	chestTPoof    = 3.3
	chestHW       = 12
	chestFH       = 9
	chestCS       = 0.94
	chestSN       = 0.34
	chestLA       = 6.0
	chestLB       = 5.5
)

var (
	chestHB      = float64(chestFH) / chestCS
	chestRattles = [4][2]float64{{0.1, 0.4}, {0.32, 0.62}, {0.5, 0.8}, {0.64, 1}}
	chestStraps  = [2]int{-9, 7}
	chestIron    = pixelfx.Ramp("#0e0d16", "#1d1c28", "#2f2e3e", "#474658", "#6a6a7e", "#9898ac")
	chestDustCol = pixelfx.Hex("#5e5068")
	chestGems    = [][]uint32{
		pixelfx.Ramp("#2a0612", "#6e0f2a", "#b81c3c", "#f0405a", "#ff8a96", "#ffd2d6"),
		pixelfx.Ramp("#06200f", "#0f4a26", "#1a8040", "#34c060", "#8af0a0", "#d8ffe0"),
		pixelfx.Ramp("#0a1030", "#142a70", "#2050c0", "#3a8cf0", "#8ccaff", "#dcf0ff"),
		pixelfx.Ramp("#1a0a2a", "#3e1466", "#6a28a8", "#a050e0", "#d09cff", "#f0e0ff"),
	}
)

type chestLidRow struct {
	row         int
	al, dep     float64
	out         bool
	lit         float64
	crest, seam bool
}

func chestLidRows(th float64) []*chestLidRow {
	best := make(map[int]*chestLidRow)
	ct, st := math.Cos(th), math.Sin(th)
	for i := 0; i <= 260; i++ {
		al := (float64(i) / 260) * pi
		dz, dy := chestLA+chestLA*math.Cos(al), chestLB*math.Sin(al)
		z, y := -chestLA+dz*ct-dy*st, chestHB+dz*st+dy*ct
		if y < chestHB-0.05 {
			continue
		}
		nz, ny := math.Cos(al)/chestLA, math.Sin(al)/chestLB
		nl := pixelfx.Hypot(nz, ny)
		nz /= nl
		ny /= nl
		Nz, Ny := nz*ct-ny*st, nz*st+ny*ct
		v, dep := y*chestCS+(chestLA-z)*chestSN, z*chestCS+y*chestSN
		row := int(math.Floor(-v))
		cur, ok := best[row]
		if !ok || dep > cur.dep {
			best[row] = &chestLidRow{
				row: row,
				al:  al,
				dep: dep,
				out: Nz*chestCS+Ny*chestSN > 0,
				lit: Ny*0.95 + Nz*0.3 - 0.2,
			}
		}
	}
	rows := make([]*chestLidRow, 0, len(best))
	for _, r := range best {
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(a, b *chestLidRow) int { return cmp.Compare(a.row, b.row) })
	var hi *chestLidRow
	for _, r := range rows {
		if r.out && (hi == nil || r.lit > hi.lit) {
			hi = r
		}
	}
	if hi != nil && hi.lit > 0.5 {
		hi.crest = true
	}
	for _, s := range [2]float64{pi / 3, (2 * pi) / 3} {
		var k *chestLidRow
		for _, r := range rows {
			if k == nil || math.Abs(r.al-s) < math.Abs(k.al-s) {
				k = r
			}
		}
		if k != nil && math.Abs(k.al-s) < 0.4 {
			k.seam = true
		}
	}
	return rows
}

func chestTone(R []uint32, l float64, steps [5]int) uint32 {
	switch {
	case l > 0.6:
		return R[steps[0]]
	case l > 0.25:
		return R[steps[1]]
	case l > -0.1:
		return R[steps[2]]
	case l > -0.45:
		return R[steps[3]]
	default:
		return R[steps[4]]
	}
}

func paintChestHoard(L *layer, C, W []uint32, g float64) {
	heat := func(k float64) uint32 { return C[int(pixelfx.ClampTo(pixelfx.Round(k+g*2.2), 0, 7))] }
	for y := -13; y <= -10; y++ {
		for x := -chestHW; x < chestHW; x++ {
			h := pixelfx.Rand(float64(x), float64(y), 7)
			k := 3.0
			if h < 0.18 {
				k = 5
			} else if h < 0.55 {
				k = 4
			}
			if y == -13 && math.Abs(float64(x)+0.5) > 9 {
				L.set(float64(x), float64(y), W[2])
			} else {
				off := 0.0
				if y == -13 {
					off = 1
				}
				L.set(float64(x), float64(y), heat(k-off))
			}
		}
	}
	for x := -9; x < 9; x++ {
		top := -13 - int(pixelfx.Round(2.2*math.Pow(math.Cos(((float64(x)+0.5)/10)*(pi/2)), 1.4)))
		for y := top; y < -13; y++ {
			var k float64
			if y == top {
				k = 5
			} else {
				k = 4
				if pixelfx.Rand(float64(x), float64(y), 3) < 0.3 {
					k = 5
				}
			}
			L.set(float64(x), float64(y), heat(k))
		}
	}
}

func paintChestLid(L *layer, C, W []uint32, th, g float64) {
	rows := chestLidRows(th)
	heat := func(k float64) uint32 { return C[int(pixelfx.ClampTo(pixelfx.Round(k+g*2.2), 0, 7))] }
	top := 0
	if len(rows) > 0 {
		top = rows[0].row
	}
	for _, r := range rows {
		u := r.al
		for x := -chestHW; x < chestHW; x++ {
			strap := x == chestStraps[0] || x == chestStraps[0]+1 || x == chestStraps[1] || x == chestStraps[1]+1
			left := x == chestStraps[0] || x == chestStraps[1]
			side := (float64(x+chestHW) + 0.5) / float64(2*chestHW)
			corner := x == -chestHW || x == chestHW-1
			var c uint32
			if r.out {
				if r.row == top && corner && th < 0.6 {
					continue
				}
				topOff := 0.0
				if r.row == top {
					topOff = 0.45
				}
				l := r.lit + 0.2 - side*0.35 - topOff
				switch {
				case u < 0.3 || (u < 0.55 && x >= -2 && x <= 1):
					add := 0.1
					if x >= -2 && x <= 1 {
						add = 0.15
					}
					c = chestTone(C, l+add, [5]int{6, 5, 5, 4, 3})
				case corner:
					c = pick(x < 0, chestIron[4], chestIron[2])
				case strap:
					c = chestTone(chestIron, l+pick(left, 0.3, -0.3), [5]int{5, 4, 3, 2, 1})
				case r.seam:
					c = W[0]
				default:
					if r.crest && side < 0.62 {
						c = W[6]
					} else {
						c = chestTone(W, l, [5]int{5, 4, 3, 2, 1})
					}
					if pixelfx.Rand(float64(x>>1), pixelfx.Round(u*6), 11) < 0.2 {
						c = W[max(0, slices.Index(W, c)-1)]
					}
				}
			} else {
				k := g * math.Pow(u/pi, 1.3)
				switch {
				case u < 0.3:
					c = heat(1)
				case r.seam:
					c = W[0]
				case k > 0.6:
					c = pixelfx.Mix(W[4], C[5], 0.3)
				case k > 0.35:
					c = W[3]
				case k > 0.15:
					c = W[2]
				default:
					c = W[1]
				}
				if corner {
					c = W[0]
				}
			}
			L.set(float64(x), float64(r.row), c)
		}
	}
}

func paintChestFront(L *layer, C, W []uint32, g float64) {
	for y := -chestFH; y <= -1; y++ {
		for x := -chestHW; x < chestHW; x++ {
			side := (float64(x+chestHW) + 0.5) / float64(2*chestHW)
			var c uint32
			switch y {
			case -chestFH:
				if side < 0.3 {
					c = C[6]
				} else if side < 0.7 {
					c = C[5]
				} else {
					c = C[4]
				}
			case -1:
				c = W[1]
			case -5:
				c = W[0]
			default:
				top := y == -8 || y == -4
				bot := y == -6 || y == -2
				switch {
				case top:
					c = pick(side < 0.45, W[4], W[3])
				case bot:
					c = W[2]
				default:
					c = W[3]
				}
				if !top && pixelfx.Rand(float64(x>>1), float64(y), 5) < 0.2 {
					c = pick(bot, W[1], W[2])
				}
			}
			if x == -chestHW {
				c = pick(y == -chestFH, C[5], chestIron[4])
			}
			if x == chestHW-1 {
				c = pick(y == -chestFH, C[3], chestIron[2])
			}
			for _, s := range chestStraps {
				if x == s && y > -chestFH {
					c = pick(y == -7 || y == -3, chestIron[5], chestIron[4])
				}
				if x == s+1 && y > -chestFH {
					c = chestIron[2]
				}
			}
			L.set(float64(x), float64(y), c)
		}
	}
	for y := -chestFH; y <= -4; y++ {
		for x := -2; x <= 1; x++ {
			var c uint32
			switch {
			case x == 1 || y == -4:
				c = C[3]
			case x == -2 && y == -chestFH:
				c = C[7]
			default:
				c = C[5]
			}
			L.set(float64(x), float64(y), c)
		}
	}
	var kh uint32
	switch {
	case g < 0.15:
		kh = W[0]
	case g < 0.4:
		kh = C[4]
	case g < 0.7:
		kh = C[6]
	default:
		kh = pixelfx.White
	}
	L.set(-1, -7, kh)
	L.set(0, -7, kh)
	L.set(-1, -6, kh)
	L.set(0, -6, pixelfx.Scale(kh, 0.85))
}

func paintChest(L *layer, C, W []uint32, th, g float64) {
	if th > 1.2 {
		paintChestLid(L, C, W, th, g)
		paintChestHoard(L, C, W, g)
	} else {
		paintChestHoard(L, C, W, g)
		paintChestLid(L, C, W, th, g)
	}
	paintChestFront(L, C, W, g)
}

type chestRay struct{ a, len, w, d, f, ph float64 }

type chestLoot struct {
	gem                         bool
	G                           []uint32
	x, y, vx, vy, t0            float64
	up, rest                    bool
	r, ax, spin, vs, settle, ph float64
	pop                         float64
	gone                        bool
}

type chestRing struct{ x, y, t0 float64 }

type chestBit struct {
	x, y float64
	c    uint32
	td   float64
	mote bool
	done bool
}

func newChestOpen(o *pixelfx.Options) pixelfx.Instance {
	C, W := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("wood"), 7)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[7], C[6], C[5], C[4]}, Haze: C[4]})
	lights := &pixelfx.Lights{}
	dust := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[5], C[5], C[4], C[3]}})
	L, IL := newLayer(40, 44), newLayer(16, 16)

	rays := make([]chestRay, 9)
	for i := range rays {
		u := (float64(i)/8)*2 - 1
		a := u*1.05 + R.Range(-0.06, 0.06)
		ln := R.Range(46, 64) * (1.2 - 0.4*math.Abs(u))
		w := R.Range(0.035, 0.08)
		d := R.Range(0, 0.06)
		f := R.Range(5, 10)
		ph := R.Range(0, tau)
		rays[i] = chestRay{a: a, len: ln, w: w, d: d, f: f, ph: ph}
	}

	n := o.Num("coins")
	var loot []*chestLoot
	for i := 0; float64(i) < n+4; i++ {
		gem := float64(i) >= n
		s := -1.0
		if i%2 != 0 {
			s = 1
		}
		a := s * R.Range(0.1, 0.5)
		sp := R.Range(190, 270)
		var G []uint32
		if gem {
			G = chestGems[int(float64(i)-n)%len(chestGems)]
		}
		x := R.Range(-7, 7)
		t0 := chestTOpen + 0.03 + R.Range(0, 0.2)
		r := 3.0
		if gem {
			r = 2.6
		}
		ax := R.Range(-0.6, 0.6)
		spin := R.Range(0, tau)
		vs := R.Sign() * R.Range(14, 24)
		ph := R.Next()
		loot = append(loot, &chestLoot{
			gem: gem, G: G, x: x, y: -12, vx: math.Sin(a) * sp, vy: -math.Cos(a) * sp,
			t0: t0, r: r, ax: ax, spin: spin, vs: vs, ph: ph,
		})
	}
	sorted := slices.Clone(loot)
	slices.SortStableFunc(sorted, func(a, b *chestLoot) int {
		return cmp.Compare(math.Abs(b.vx), math.Abs(a.vx))
	})
	for k, it := range sorted {
		it.pop = chestTCollect + float64(k)*0.04 + R.Range(0, 0.02)
	}

	var rings []chestRing
	var bits []chestBit
	t, opened, poofed := 0.0, false, false

	shake := func() (dx, hop, lid float64) {
		for _, rt := range chestRattles {
			t0, s := rt[0], rt[1]
			u := t - t0
			if u < 0 || u > 0.18 {
				continue
			}
			e := 1 - u/0.18
			dx += math.Sin(u*90) * 1.6 * s * e
			if u < 0.06 && s > 0.5 {
				hop = 1
			}
			lid = math.Max(lid, math.Max(0, math.Sin(u*38))*0.13*s*e)
		}
		if t >= chestTOpen && t < chestTOpen+0.08 {
			hop = 1
		}
		strain := pixelfx.Smooth(chestTOpen-0.14, chestTOpen, t)
		if t < chestTOpen {
			lid = math.Max(lid, 0.07*strain)
			if strain > 0.2 {
				dx += pick(int(math.Floor(t*60))%2 != 0, 1.0, -1.0) * 0.6
			}
		}
		return pixelfx.Round(dx), hop, lid
	}

	lidAngle := func(lid float64) float64 {
		if t < chestTOpen {
			return lid
		}
		u, w, z := t-chestTOpen, 30.0, 0.55
		wd := w * math.Sqrt(1-z*z)
		return 1.98 * (1 - math.Exp(-z*w*u)*(math.Cos(wd*u)+((z*w)/wd)*math.Sin(wd*u)))
	}

	inner := func() float64 {
		if t < chestTOpen {
			k := 0.1 + 0.45*pixelfx.Smooth(0, chestTOpen, t) + 0.5*pixelfx.Smooth(chestTOpen-0.14, chestTOpen, t)
			for _, rt := range chestRattles {
				t0, s := rt[0], rt[1]
				if t > t0 {
					k += 0.45 * s * math.Exp(-(t-t0)*9)
				}
			}
			return k
		}
		return (0.62+0.3*math.Exp(-(t-chestTOpen)*5))*(1-pixelfx.Smooth(chestTCollect-0.3, chestTPoof, t)) + 0.05*math.Sin(t*9)
	}

	rayK := func(r chestRay) float64 {
		u := t - chestTOpen - r.d
		if u < 0 {
			return 0
		}
		return pixelfx.Ease.OutCubic(pixelfx.Clamp(u/0.1)) * (1 - pixelfx.Smooth(1.0, 2.1, u)) * (0.72 + 0.28*math.Sin(t*r.f+r.ph))
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			g := inner()
			for _, rt := range chestRattles {
				t0, s := rt[0], rt[1]
				if s > 0.5 && t-dt < t0+0.05 && t >= t0+0.05 {
					for i := range 4 {
						sd := pick(i%2 != 0, 1.0, -1.0)
						ps.Spawn(pixelfx.Dust, sd*R.Range(9, 13), -1, sd*R.Range(20, 50), -R.Range(2, 8), R.Range(0.3, 0.5), chestDustCol, R.Range(1.2, 2.2)*s, 0, 0)
					}
				}
			}
			if t < chestTOpen && R.Chance(g*0.08) {
				x := R.Range(-11, 11)
				vy := -R.Range(8, 30)
				life := R.Range(0.3, 0.6)
				col := pick(R.Chance(0.5), C[6], C[5])
				size := R.Range(0.4, 1.2)
				ps.Spawn(pixelfx.StarKind, x, -10, x*1.5, vy, life, col, size, 0, 0)
			}
			if !opened && t >= chestTOpen {
				opened = true
				lights.Add(0, -18, 64, C[5], 0.45, 0.7)
				lights.Add(0, -16, 22, pixelfx.White, 0.35, 0.1)
				for range 7 {
					a, sp := -pi/2+R.Range(-1.1, 1.1), R.Range(120, 260)
					ps.Spawn(pixelfx.Spark, R.Range(-8, 8), -13, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.12, 0.26), pixelfx.White, 1, 0, 0)
				}
				for range 8 {
					s := R.Sign()
					ps.Spawn(pixelfx.Dust, s*R.Range(10, 14), -1, s*R.Range(30, 80), -R.Range(2, 10), R.Range(0.4, 0.7), chestDustCol, R.Range(1.6, 3), 0, 0)
				}
			}
			if t > chestTOpen && t < chestTCollect {
				if R.Chance(0.22) {
					ps.Spawn(pixelfx.Ember, R.Range(-9, 9), -13, R.Range(-12, 12), -R.Range(25, 60), R.Range(0.8, 1.5), C[5], R.Range(0.8, 1.6), 0, 0)
				}
				if R.Chance(0.12) {
					a, d := R.Range(-1, 1), R.Range(10, 40)
					vy := -R.Range(4, 12)
					life := R.Range(0.3, 0.7)
					col := pick(R.Chance(0.5), pixelfx.White, C[6])
					size := R.Range(0.5, 2)
					ps.Spawn(pixelfx.StarKind, math.Sin(a)*d, -12-math.Cos(a)*d, 0, vy, life, col, size, 0, 0)
				}
			}
			for _, it := range loot {
				if it.gone || t < it.t0 {
					continue
				}
				it.up = true
				if t >= it.pop+0.13 {
					it.gone = true
					y := it.y - 6
					if it.gem {
						rings = append(rings, chestRing{x: it.x, y: y, t0: t})
					}
					lcol := C[5]
					if it.gem {
						lcol = it.G[4]
					}
					lights.Add(it.x, y, 18, lcol, 0.65, 0.25)
					for i := range 4 {
						a, sp := R.Range(0, tau), R.Range(25, 55)
						col := pixelfx.White
						if i != 0 {
							if it.gem {
								col = it.G[5]
							} else {
								col = C[6]
							}
						}
						ps.Spawn(pixelfx.StarKind, it.x, y, math.Cos(a)*sp-it.x*0.25, math.Sin(a)*sp-20, R.Range(0.3, 0.55), col, R.Range(0.6, 1.5), 0, 0)
					}
					continue
				}
				if it.rest {
					it.settle += dt
					it.vx -= it.vx * 9 * dt
					it.x = pixelfx.ClampTo(it.x+it.vx*dt, -74, 74)
					continue
				}
				it.vy += 950 * dt
				it.x += it.vx * dt
				if math.Abs(it.x) > 74 {
					it.x = pixelfx.Sign(it.x) * 74
					it.vx *= -0.3
				}
				it.y += it.vy * dt
				it.spin += it.vs * dt
				bottom := it.y + it.r
				if bottom >= -0.5 && it.vy > 0 {
					it.y = -0.5 - it.r
					if it.vy > 110 {
						ps.Spawn(pixelfx.Dust, it.x, -1, R.Range(-20, 20), -R.Range(4, 12), R.Range(0.25, 0.45), chestDustCol, R.Range(1, 1.8), 0, 0)
						if it.vy > 220 {
							for range 2 {
								ps.Spawn(pixelfx.Spark, it.x, -1, R.Range(-90, 90), -R.Range(60, 140), R.Range(0.1, 0.22), pixelfx.White, 1, 0, 0)
							}
							lights.Add(it.x, -2, 9, C[5], 0.5, 0.12)
						}
						it.vy = -it.vy * 0.42
						it.vx *= 0.5
						it.vs *= 0.55
					} else {
						it.rest = true
						it.vy = 0
					}
				}
			}
			for i := len(rings) - 1; i >= 0; i-- {
				if t-rings[i].t0 > 0.3 {
					rings = slices.Delete(rings, i, i+1)
				}
			}
			if !poofed && t >= chestTPoof {
				poofed = true
				lights.Add(0, -12, 40, C[5], 0.5, 0.6)
				L.begin(-20, -32)
				paintChest(L, C, W, lidAngle(0), 0.3)
				L.outline(W[0])
				f := L.f
				top := 0
				for i, c := range f.Color {
					if c != 0 {
						top = min(top, i/f.W+f.OY)
					}
				}
				for i, c := range f.Color {
					if c == 0 {
						continue
					}
					x, y := float64(i%f.W+f.OX), float64(i/f.W+f.OY)
					// Upstream compares Uint32Array element c against signed int32 W[0] without >>> 0, so c !== W[0] is always true.
					bits = append(bits, chestBit{
						x: x, y: y, c: c,
						td:   chestTPoof + ((y-float64(top))/float64(-top))*0.32 + pixelfx.Rand(x, y, 21)*0.05,
						mote: pixelfx.Rand(x, y, 23) < 0.14,
					})
				}
			}
			for i := range bits {
				b := &bits[i]
				if !b.done && t >= b.td {
					b.done = true
					if b.mote {
						dust.Spawn(pixelfx.Ember, b.x+0.5, b.y+0.5, R.Range(-28, 28), -R.Range(14, 50), R.Range(0.4, 0.8), C[5], R.Range(0.5, 1.1), 0, 0)
					}
				}
			}
			ps.Update(dt)
			dust.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			if t < chestTPoof+0.2 {
				blobShadow(p, 0, 15, 1-pixelfx.Smooth(chestTPoof, chestTPoof+0.2, t), 0)
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			g := inner()
			shDx, shHop, shLid := shake()
			th := lidAngle(shLid)
			open := 0.0
			if t >= chestTOpen {
				open = 1
			}
			pool(p, 0, 0, 30+10*g, 4, C[4], (0.1+0.18*open)*g)
			p.Glow(0, -16, 24+8*g, C[4], (0.1+0.2*open)*g)
			if t < chestTPoof {
				L.begin(-20, -32)
				paintChest(L, C, W, th, math.Min(1.2, g))
				L.outline(W[0])
				L.draw(p, shDx, -shHop, 1)
			} else {
				for _, b := range bits {
					if !b.done {
						col := b.c
						if b.td-t < 0.04 {
							// Upstream compares Uint32Array element b.c against signed int32 W[0] without >>> 0, so b.c === W[0] is always false.
							col = C[6]
						}
						p.Px(b.x, b.y, col)
					}
				}
			}
			if t < chestTOpen {
				p.GlowLine(-11, -9.5-shHop, 11, -9.5-shHop, 5, C[5], 0.22*g)
				if g > 0.35 && shLid < 0.015 {
					p.LineFn(-10+shDx, -10-shHop, 9+shDx, -10-shHop, func(u float64) (uint32, float64) {
						return pick(math.Abs(u-0.5) < 0.2, pixelfx.White, C[6]), (g - 0.35) * 2.2 * (1 - math.Abs(u-0.5)*0.9)
					})
				}
				p.Glow(shDx, -7-shHop, 6, C[6], 0.35*g)
				if shLid > 0.015 {
					for i := range 8 {
						x := -10.5 + float64(i)*3 + shDx + (pixelfx.Rand(float64(i), 3)-0.5)*1.5
						a := -pi/2 + (x/11)*1.25
						l := (7 + pixelfx.Rand(float64(i), math.Floor(t*30), 1)*12) * math.Min(1, shLid/0.05)
						y0 := -10 - shHop
						p.LineFn(x, y0, x+math.Cos(a)*l, y0+math.Sin(a)*l, func(u float64) (uint32, float64) {
							col := C[5]
							if u < 0.25 {
								col = pixelfx.White
							} else if u < 0.6 {
								col = C[6]
							}
							return col, 1.15 - u
						})
						p.GlowLine(x, y0, x+math.Cos(a)*l*0.7, y0+math.Sin(a)*l*0.7, 3, C[5], 0.25)
					}
				}
			}
			for _, r := range rays {
				k := rayK(r)
				if k <= 0.01 {
					continue
				}
				ox, oy := math.Sin(r.a)*7, -12.0
				dx, dy := math.Sin(r.a), -math.Cos(r.a)
				ln := r.len * (0.88 + 0.12*math.Sin(t*r.f*0.7+r.ph))
				tw := math.Tan(r.w)
				x1, y1 := ox+dx*ln, oy+dy*ln
				p.Shade(math.Min(ox, x1)-8, math.Min(oy, y1)-8, math.Max(ox, x1)+8, math.Max(oy, y1)+8, func(x, y float64, out *pixelfx.Out) uint32 {
					rx, ry := x-ox, y-oy
					d := rx*dx + ry*dy
					if d < 0 || d > ln {
						return 0
					}
					q, hw := math.Abs(rx*dy-ry*dx), 0.6+d*tw
					var f float64
					if q < hw {
						f = 1 - 0.35*(q/hw)
					} else {
						f = 0.65 * (1 - (q-hw)/1.6)
					}
					if f <= 0 {
						return 0
					}
					out.L = pick(q < hw*0.45, C[6], C[5])
					out.K = k * 0.55 * f * math.Pow(1-d/ln, 1.4) * pixelfx.Smooth(5, 14, d)
					return 0
				})
			}
			if t >= chestTOpen && t < chestTOpen+0.2 {
				a := (t - chestTOpen) / 0.2
				pixelfx.Flare(p, 0, -14, 22*math.Pow(1-a, 1.4), 1-a, C)
			}
			for _, it := range loot {
				if !it.up || it.gone {
					continue
				}
				pu := t - it.pop
				lift := 0.0
				if pu > 0 {
					lift = 6 * pixelfx.Ease.OutCubic(pixelfx.Clamp(pu/0.1))
				}
				wh := pu > 0.095
				y, ax, spin := it.y-lift, it.ax, it.spin
				flat := false
				if it.rest && !it.gem {
					w := math.Exp(-it.settle*8) * math.Abs(math.Cos(it.settle*22))
					ax = 0
					spin = math.Acos(pixelfx.ClampTo(0.5+0.5*w, 0, 1))
					flat = w < 0.12
					if flat {
						y = -1.5 - lift
					} else {
						y = -it.r*math.Cos(spin) - lift
					}
				}
				if it.y > -10 {
					blobShadow(p, it.x, 3, 0.7*(1-pixelfx.Clamp(-it.y/30)), 0)
				}
				if it.gem {
					ang := it.spin
					if it.rest {
						ang = 0.4 * pixelfx.Sign(it.vs)
					}
					gemlet(IL, p, it.x, y, it.r, ang, it.G, wh, 1)
				} else {
					cx, cspin := it.x, spin
					if flat {
						cx = pixelfx.Round(it.x) + 0.5
						cspin = math.Acos(0.5)
					}
					coin(IL, p, cx, y, it.r, ax, cspin, C, wh, 1, !flat)
				}
				gcol := C[5]
				if it.gem {
					gcol = it.G[4]
				}
				p.Glow(it.x, y, 9, gcol, pick(it.rest, 0.26, 0.18))
				p.Glow(it.x, y, 3, pixelfx.White, 0.12)
				if it.rest && t < it.pop {
					u := math.Mod(t*0.9+it.ph, 1)
					if u < 0.16 {
						glintCol := C[6]
						if it.gem {
							glintCol = it.G[5]
						}
						glint(p, it.x-1, y-1.5, 2.6*math.Sin((u/0.16)*pi), glintCol, 0.5)
					}
				}
			}
			for _, r := range rings {
				k := (t - r.t0) / 0.3
				e := pixelfx.Ease.OutCubic(k)
				p.Ring(r.x, r.y, 2+5*e, 1, pick(k < 0.3, pixelfx.White, C[6]), 1.15-k)
				p.Glow(r.x, r.y, 12, C[5], 0.45*(1-k))
				if k < 0.5 {
					glint(p, r.x, r.y, 3.4*(1-k/0.5), C[6], 0.55)
				}
			}
			ps.Draw(p, pixelfx.LayerFront)
			dust.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
