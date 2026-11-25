// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/scanner.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package scifi

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	scanStiff = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.Crouch.Is(-1), pixelfx.FX.Is(-7), pixelfx.FY.Is(-14), pixelfx.BX.Is(8), pixelfx.BY.Is(-14), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	scanOK    = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.FX.Is(-9), pixelfx.FY.Is(-37), pixelfx.BX.Is(8), pixelfx.BY.Is(-15), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
)

// Scanner snaps brackets onto the hero, sweeps a scan line head to toe leaving a cooling outline, and counts a readout up to OK before letting go. It is registered as "scifi/scanner".
var Scanner = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Scanner",
		Desc:     "Brackets snap onto the hero, a scan line sweeps head to toe leaving a cooling outline, and a readout counts up to OK before it all lets go.",
		Tags:     []string{"scifi", "scan", "target", "hud", "tech", "status", "ui", "loop"},
		Size:     [2]int{112, 78},
		Origin:   [2]int{42, 58},
		Duration: scanDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#03140a", "#06301a", "#0c5a2c", "#16893e", "#2cc256", "#7cf08c", "#d0ffd4", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(22)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: pixelfx.Pose{}},
			{T: 0.3, Pose: scanStiff, How: "snap"},
			{T: 1.75, Pose: scanStiff},
			{T: 1.85, Pose: scanOK, How: "snap"},
			{T: 2.5, Pose: scanOK},
			{T: 2.8, Pose: pixelfx.Pose{}},
			{T: 3.2, Pose: pixelfx.Pose{}},
		},
	},
	New: newScanner,
}

func init() { pixelfx.Register("scifi/scanner", Scanner) }

const scanDuration = 3.2

var (
	scanLock = 0.3
	scan0    = 0.42
	scan1    = 1.6
	scanDone = 1.75
	scanLet  = 2.5
)

type scanBody struct {
	edge [][2]float64
	rows map[float64][2]float64
}

func buildScanBody(A *pixelfx.Actor) scanBody {
	m := A.Mask
	on := func(x, y int) bool { return x >= 0 && y >= 0 && x < m.W && y < m.H && m.Data[y*m.W+x] != 0 }
	var edge [][2]float64
	rows := map[float64][2]float64{}
	for y := -1; y <= m.H; y++ {
		for x := -1; x <= m.W; x++ {
			if on(x, y) {
				ry := A.Y + float64(y)
				rx := A.X + float64(x)
				if r, ok := rows[ry]; ok {
					rows[ry] = [2]float64{math.Min(r[0], rx), math.Max(r[1], rx)}
				} else {
					rows[ry] = [2]float64{rx, rx}
				}
			} else if on(x-1, y) || on(x+1, y) || on(x, y-1) || on(x, y+1) {
				edge = append(edge, [2]float64{A.X + float64(x), A.Y + float64(y)})
			}
		}
	}
	return scanBody{edge: edge, rows: rows}
}

func scannerY(t float64, A *pixelfx.Actor) float64 {
	return pixelfx.Lerp(A.Y-3, 1, pixelfx.Ease.InOutSine(pixelfx.Clamp((t-scan0)/(scan1-scan0))))
}

func scannerT(y float64, A *pixelfx.Actor) float64 {
	return scan0 + ((scan1-scan0)*math.Acos(1-2*pixelfx.Clamp((y+0.5-(A.Y-3))/(4-A.Y))))/pi
}

// scanBracket draws an L-shaped bracket: two arms from a corner, a hot pixel at the corner.
func scanBracket(p *pixelfx.Painter, x, y, sx, sy, L float64, c, hot uint32, a float64) {
	p.Line(x, y, x+sx*L, y, c, a)
	p.Line(x, y, x, y+sy*L, c, a)
	p.Line(x+sx, y+sy, x+sx*3, y+sy, c, a*0.6)
	p.Dot(x, y, hot, a)
}

func newScanner(o *pixelfx.Options) pixelfx.Instance {
	C, A, seed := pixelfx.Fit(o.Palette(), 8), o.Actor, o.Seed()
	b := buildScanBody(A)
	g := newGhosts(scanDuration, ghostOpts{
		ground: zeroGround,
		hasBox: true,
		box:    [4]float64{-40, -58, 68, 9},
	})
	// data bits flicked off the body's edges as the line crosses them
	for y := A.Y; y < 0; y += 3 {
		r, ok := b.rows[y]
		if !ok {
			continue
		}
		for _, pair := range [2][2]float64{{r[0] - 1, -1}, {r[1] + 1, 1}} {
			x, s := pair[0], pair[1]
			if pixelfx.Rand(seed, y, s) < 0.6 {
				c := C[5]
				if pixelfx.Rand(y, s, 6) < 0.4 {
					c = pixelfx.White
				}
				g.add(scannerT(y, A), pixelfx.Mote, x, y, s*(30+50*pixelfx.Rand(y, s, 3)), -10+20*pixelfx.Rand(y, s, 4), 0.2+0.15*pixelfx.Rand(y, s, 5), c, 0.6, 0)
			}
		}
	}

	return &pixelfx.Funcs{
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			// the box the brackets lock to, and how far out they are: closing in with a snap, letting go at the end
			bx0, bx1, by0, by1 := A.X-4, A.X+float64(A.W)+3, A.Y-4, 3.0
			lock := pixelfx.OutBack(pixelfx.Clamp(t/scanLock), 2.2)
			out := pixelfx.Ease.InCubic(pixelfx.Clamp((t - scanLet) / 0.45))
			spread := (1-lock)*14 + out*12
			gone := t > scanLet+0.45 || (t > scanLet+0.25 && int(math.Floor((t-scanLet)*20))%2 == 1)
			vis := 1.0
			if gone {
				vis = 0
			}
			fl := (t > scanLock && t < scanLock+0.07) || (t > scanDone && t < scanDone+0.05) || (t > scanDone+0.12 && t < scanDone+0.17)
			bc := C[5]
			if fl {
				bc = pixelfx.White
			}
			L := 7.0
			if vis > 0 {
				for _, cr := range [4][4]float64{
					{bx0 - spread, by0 - spread, 1, 1},
					{bx1 + spread, by0 - spread, -1, 1},
					{bx0 - spread, by1 + spread, 1, -1},
					{bx1 + spread, by1 + spread, -1, -1},
				} {
					scanBracket(p, cr[0], cr[1], cr[2], cr[3], L, bc, pixelfx.White, vis)
					p.Glow(cr[0], cr[1], 6, C[4], 0.35*vis)
				}
			}
			// on lock: a box flashes out from the brackets
			lk := (t - scanLock) / 0.3
			if lk >= 0 && lk < 1 {
				e := 3 + 10*pixelfx.Ease.OutCubic(lk)
				c := C[3]
				switch {
				case lk < 0.25:
					c = pixelfx.White
				case lk < 0.5:
					c = C[6]
				case lk < 0.75:
					c = C[5]
				}
				p.Line(bx0-e, by0-e, bx1+e, by0-e, c, 1)
				p.Line(bx0-e, by1+e, bx1+e, by1+e, c, 1)
				p.Line(bx0-e, by0-e, bx0-e, by1+e, c, 1)
				p.Line(bx1+e, by0-e, bx1+e, by1+e, c, 1)
			}
			// the outline, lit row by row as the line passes and cooling behind it
			pulse := 0.0
			if sd := since(t, scanDone, scanDuration); sd < 0.3 {
				pulse = 1 - sd/0.3
			}
			offY := pixelfx.Lerp(A.Y-2, 2, pixelfx.Clamp((t-scanLet)/0.4))
			if t > scan0 {
				for _, pt := range b.edge {
					x, y := pt[0], pt[1]
					a := t - scannerT(y, A)
					if a < 0 || (t > scanLet && y < offY) {
						continue
					}
					c := C[4]
					switch {
					case pulse > 0.4 || a < 0.07:
						c = pixelfx.White
					case a < 0.25:
						c = C[6]
					case a < 0.6:
						c = C[5]
					case t > scanLet-0.3:
						c = C[3]
					}
					p.Px(x, y, c)
				}
			}
			// the scan line and the slice of light just above it
			if t > scan0 && t < scan1 {
				y := math.Floor(scannerY(t, A))
				for k := 1.0; k <= 5; k++ {
					if r, ok := b.rows[y-k]; ok {
						my := int(y - k - A.Y)
						c := C[5]
						if k < 2 {
							c = C[6]
						}
						for x := r[0]; x <= r[1]; x++ {
							mx := int(x - A.X)
							if A.Mask.Data[my*A.W+mx] != 0 {
								p.Blend(x+0.5, y-k+0.5, c, 0.55-k*0.09)
							}
						}
					}
				}
				r, hasR := b.rows[y]
				for x := bx0 - 2; x <= bx1+2; x++ {
					c := C[6]
					if hasR && x >= r[0] && x <= r[1] {
						c = pixelfx.White
					}
					p.Px(x, y, c)
					p.Blend(x+0.5, y-0.5, C[5], 0.35)
				}
				p.GlowLine(bx0, y, bx1, y, 6, C[5], 0.45)
				if hasR {
					for _, x := range [2]float64{r[0] - 0.5, r[1] + 1.5} {
						pixelfx.Sparkle(p, x, y+0.5, 2, C[6], C[5], 0.3)
					}
				}
			}
			// the readout: a leader from the top-right bracket to a panel, a count, a bar filling, then OK
			lead := pixelfx.Clamp((t - scanLock) / 0.1)
			if vis > 0 && lead > 0 {
				px, py := bx1+spread+6, by0-spread+2
				k := pixelfx.Clamp((t - scan0) / (scan1 - scan0))
				ok := t > scanDone
				p.Line(bx1+spread, by0-spread, pixelfx.Lerp(bx1+spread, px-1, lead), pixelfx.Lerp(by0-spread, py+4, lead), C[3], 1)
				if lead < 1 {
					g.at(t).Draw(p, pixelfx.LayerBoth)
					return
				}
				p.Line(px-1, py+4, px+1, py+4, C[3], 1)
				del := pixelfx.Clamp((t - scanLet) / 0.25)
				full := "OK"
				if !ok {
					full = fmt.Sprintf("%d%%", int(math.Floor(k*100)))
				}
				nChars := int(math.Ceil(float64(len(full)) * (1 - del)))
				if nChars < 0 {
					nChars = 0
				} else if nChars > len(full) {
					nChars = len(full)
				}
				label := full[:nChars]
				blinkOk := ok && int(math.Floor((t-scanDone)*8))%2 == 0 && t < scanDone+0.5
				if label != "" {
					tc := C[6]
					if blinkOk {
						tc = pixelfx.White
					}
					p.Text(label, px+2, py, tc, 1, pixelfx.TextOpts{Font: "mini", Scale: 2, Shadow: C[1]})
				}
				// the bar: ten cells filling as it goes, emptying from the right as it lets go
				for i := range 10 {
					if float64(i) >= 10*(1-del) {
						break
					}
					on := float64(i+1)/10 <= k+1e-6 || ok
					c := C[2]
					if on {
						if ok {
							c = C[6]
						} else {
							c = C[5]
						}
					}
					p.Rect(px+2+float64(i)*3, py+13, 2, 3, c, 1)
				}
				p.Glow(px+16, py+6, 18, C[4], 0.2*vis)
			}
			g.at(t).Draw(p, pixelfx.LayerBoth)
		},
	}
}
