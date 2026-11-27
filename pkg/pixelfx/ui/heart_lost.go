// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/heart-lost.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package ui

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// HeartLost shows a HUD heart taking a hit, flashing white, cracking down the middle and splitting into two tumbling halves over a fading empty socket. It is registered as "ui/heart-lost".
var HeartLost = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Heart lost",
		Desc:     "A HUD heart is hit: it flashes white and jolts, a crack runs down it, it splits into two halves that tumble away, leaving an empty socket that fades.",
		Tags:     []string{"ui", "health", "heart", "hud", "damage", "one-shot"},
		Size:     [2]int{96, 88},
		Origin:   [2]int{48, 34},
		Duration: hlDuration,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0508", "#4a0b12", "#8c1420", "#d42a2e", "#ff5a4a", "#ff9a86", "#ffd8cc", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newHeartLost,
}

func init() { pixelfx.Register("ui/heart-lost", HeartLost) }

var (
	hlDuration = 1.5
	hlHit      = 0.14
	hlCrack    = 0.24
	hlSplit    = 0.5
	hlMask     = [16]string{
		"....xxxx...xxxx....",
		"..xxxxxxx.xxxxxxx..",
		".xxxxxxxxxxxxxxxxx.",
		"xxxxxxxxxxxxxxxxxxx",
		"xxxxxxxxxxxxxxxxxxx",
		"xxxxxxxxxxxxxxxxxxx",
		"xxxxxxxxxxxxxxxxxxx",
		".xxxxxxxxxxxxxxxxx.",
		"..xxxxxxxxxxxxxxx..",
		"...xxxxxxxxxxxxx...",
		"....xxxxxxxxxxx....",
		".....xxxxxxxxx.....",
		"......xxxxxxx......",
		".......xxxxx.......",
		"........xxx........",
		".........x.........",
	}
	hlCrackX = [16]int{9, 9, 10, 9, 8, 8, 9, 10, 10, 9, 8, 9, 9, 10, 9, 9}
	hlSocket = [3]uint32{pixelfx.Hex("#0d0a14"), pixelfx.Hex("#2a2338"), pixelfx.Hex("#3c3350")}
)

const (
	hlMW = 19
	hlMH = 16
	hlOX = -9.5
	hlOY = -8.0
)

func hlInMask(i, j int) bool {
	return i >= 0 && j >= 0 && i < hlMW && j < hlMH && hlMask[j][i] == 'x'
}

func hlShadeAt(C []uint32, i, j int) uint32 {
	u, v := (float64(i)-9)/9.5, (float64(j)-7)/8
	if (i == 3 || i == 4) && (j == 2 || j == 3) && !(i == 4 && j == 3) {
		return pixelfx.White
	}
	if i >= 2 && i <= 5 && j >= 2 && j <= 5 && u+v < -0.75 {
		return C[5]
	}
	l := -(u*0.55 + v*0.65)
	if l > 0.25 {
		return C[4]
	}
	if l > -0.3 {
		return C[3]
	}
	if l > -0.6 {
		return C[2]
	}
	return C[1]
}

func drawHeartPart(p *pixelfx.Painter, C []uint32, side int, x, y, a float64, white bool, crackK, alpha float64) {
	ca, sa := math.Cos(a), math.Sin(a)
	px0 := 0.0
	if side < 0 {
		px0 = -4.5
	} else if side > 0 {
		px0 = 4.5
	}
	own := func(i, j int) bool {
		if !hlInMask(i, j) {
			return false
		}
		if side == 0 {
			return true
		}
		if side < 0 {
			return i < hlCrackX[j]
		}
		return i >= hlCrackX[j]
	}
	p.Shade(x-13, y-13, x+13, y+13, func(sx, sy float64, o *pixelfx.Out) uint32 {
		dx, dy := sx-x, sy-y
		lx, ly := dx*ca+dy*sa+px0, -dx*sa+dy*ca
		i, j := int(math.Floor(lx-hlOX)), int(math.Floor(ly-hlOY))
		if !own(i, j) {
			return 0
		}
		o.A = alpha
		edge := !own(i-1, j) || !own(i+1, j) || !own(i, j-1) || !own(i, j+1)
		if white {
			if edge {
				return C[6]
			}
			return pixelfx.White
		}
		if edge {
			return C[0]
		}
		if side == 0 && float64(j)/float64(hlMH) < crackK {
			if i == hlCrackX[j] {
				return C[0]
			}
			if i == hlCrackX[j]-1 {
				return C[6]
			}
		}
		return hlShadeAt(C, i, j)
	})
}

type heartHalf struct {
	s                   int
	x, y, vx, vy, a, va float64
}

func newHeartLost(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[7], C[6], C[5], C[3]}})
	bits := pixelfx.NewBits(nil)
	lights := &pixelfx.Lights{}
	var halves [2]heartHalf
	for idx, s := range [2]int{-1, 1} {
		fs := float64(s)
		vx := fs * R.Range(55, 70)
		vy := -R.Range(60, 80)
		va := fs * R.Range(2.2, 3.2)
		halves[idx] = heartHalf{s: s, x: fs * 4.5, y: 0, vx: vx, vy: vy, va: va}
	}
	t, hit, split, gone := 0.0, false, false, false
	end := hlDuration - 0.05

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !hit && t >= hlHit {
				hit = true
				lights.Add(0, 0, 40, C[5], 0.6, 0.3)
				for range 8 {
					an := R.Range(0, tau)
					sp := R.Range(80, 170)
					ps.Spawn(pixelfx.Spark, math.Cos(an)*9, math.Sin(an)*7, math.Cos(an)*sp, math.Sin(an)*sp-30, R.Range(0.1, 0.22), pixelfx.White, 1, 0, 0)
				}
			}
			if !split && t >= hlSplit {
				split = true
				lights.Add(0, 0, 44, pixelfx.White, 0.6, 0.12)
				lights.Add(0, 0, 50, C[5], 0.5, 0.4)
				for range 12 {
					j := int(R.Int(1, 14))
					an := -pi/2 + R.Range(-1.4, 1.4)
					sp := R.Range(40, 100)
					life := R.Range(0.35, 0.55)
					a := R.Range(0, tau)
					va := R.Range(-20, 20)
					s := R.Range(1.5, 3)
					c := pixelfx.Pick(R, []uint32{C[6], C[5], C[4], C[3]})
					bits.Add(pixelfx.Bit{
						Kind: pixelfx.Shard, X: float64(hlCrackX[j]) + hlOX, Y: float64(j) + hlOY,
						VX: math.Cos(an) * sp, VY: math.Sin(an) * sp,
						G: 0.5, Drag: 1, Life: life, A: a, VA: va, S: s, C: c,
					})
				}
				for range 5 {
					px := R.Range(-2, 2)
					py := R.Range(-6, 6)
					vx := R.Range(-25, 25)
					vy := R.Range(-20, 5)
					life := R.Range(0.35, 0.6)
					size := R.Range(2, 3.5)
					ps.Spawn(pixelfx.Dust, px, py, vx, vy, life, C[2], size, 0, 0)
				}
				for i := range 6 {
					an := R.Range(0, tau)
					sp := R.Range(30, 70)
					life := R.Range(0.25, 0.45)
					col := pixelfx.Pick(R, []uint32{C[7], C[6]})
					size := 0.6
					if i%2 == 0 {
						size = 1.4
					}
					ps.Spawn(pixelfx.StarKind, math.Cos(an)*4, math.Sin(an)*4, math.Cos(an)*sp, math.Sin(an)*sp, life, col, size, 0, 0)
				}
			}
			if split && !gone && t >= 0.95 {
				gone = true
				for hi := range halves {
					h := &halves[hi]
					for i := range 5 {
						an := R.Range(0, tau)
						sp := R.Range(20, 50)
						life := math.Min(R.Range(0.2, 0.35), end-t)
						col := pixelfx.Pick(R, []uint32{C[7], C[6], C[5]})
						size := 0.6
						if i == 0 {
							size = 1.3
						}
						ps.Spawn(pixelfx.StarKind, h.x+math.Cos(an)*4, h.y+math.Sin(an)*4, math.Cos(an)*sp, math.Sin(an)*sp-10, life, col, size, 0, 0)
					}
				}
			}
			if split {
				for hi := range halves {
					h := &halves[hi]
					h.vy += 520 * dt
					h.vx -= h.vx * 0.6 * dt
					h.x += h.vx * dt
					h.y += h.vy * dt
					h.a += h.va * dt
					if t < 0.92 && R.Chance(0.05) {
						vx := R.Range(-10, 10)
						vy := R.Range(-10, 5)
						life := math.Min(R.Range(0.3, 0.5), end-t)
						ps.Spawn(pixelfx.Ember, h.x, h.y, vx, vy, life, C[4], 1, 0, 0)
					}
				}
			}
			bits.Update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			jx, jy := shake(t, 3, hlHit, 0.25, o.Seed(), 32)
			tremble := 0.0
			if t > hlCrack && t < hlSplit {
				tremble = pixelfx.Round(math.Sin(t * 90))
			}
			sk := pixelfx.Seg(t, hlSplit-0.05, hlSplit+0.1) * (1 - pixelfx.Seg(t, 1.15, 1.4))
			if sk > 0 {
				p.Shade(-12, -10, 12, 10, func(sx, sy float64, out *pixelfx.Out) uint32 {
					i, j := int(math.Floor(sx-hlOX)), int(math.Floor(sy-hlOY))
					if !hlInMask(i, j) {
						return 0
					}
					out.A = sk
					if !hlInMask(i-1, j) || !hlInMask(i+1, j) || !hlInMask(i, j-1) || !hlInMask(i, j+1) {
						return hlSocket[2]
					}
					if j < 3 {
						return hlSocket[1]
					}
					return hlSocket[0]
				})
			}
			if t < hlSplit {
				beat := 1.0
				if t < hlHit {
					beat = 1 + 0.08*math.Max(0, math.Sin(t*22))
				}
				white := t >= hlHit && t < hlHit+0.07
				drawHeartPart(p, C, 0, jx+tremble, jy, 0, white, pixelfx.Seg(t, hlCrack, hlSplit-0.06), 1)
				extra := 0.0
				if white {
					extra = 0.5
				}
				p.Glow(0, 0, 18*beat, C[4], 0.3+extra)
				if t < hlHit {
					pixelfx.Sparkle(p, -6, -5, 2.5*math.Max(0, math.Sin(t*22)), C[7], C[6], 0.3)
				}
			} else {
				fade := 1 - pixelfx.Seg(t, 0.9, 1.0)
				white := t < hlSplit+0.05
				if fade > 0 {
					for hi := range halves {
						h := &halves[hi]
						drawHeartPart(p, C, h.s, h.x, h.y, h.a, white, 0, fade)
					}
				}
				rk := (t - hlSplit) / 0.3
				if rk < 1 {
					col := C[5]
					if rk < 0.3 {
						col = pixelfx.White
					}
					p.Ring(0, 0, 8+16*pixelfx.Ease.OutCubic(rk), math.Max(1, 2*(1-rk)), col, 1.2-rk)
				}
			}
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
