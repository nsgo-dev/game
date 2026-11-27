// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/quest-complete.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package ui

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// QuestComplete unrolls a stitched ribbon from the middle, types QUEST COMPLETE on letter by letter, stamps down a gold seal that ticks, pops stars, then rolls away. It is registered as "ui/quest-complete".
var QuestComplete = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Quest complete",
		Desc:     "A stitched ribbon unrolls from the middle, QUEST COMPLETE types on letter by letter, a gold seal stamps down and ticks, stars pop; then it all rolls away.",
		Tags:     []string{"ui", "quest", "banner", "reward", "text", "one-shot"},
		Size:     [2]int{204, 92},
		Origin:   [2]int{102, 46},
		Duration: qcDuration,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1405", "#5c3008", "#9a5a0e", "#d48e18", "#f5b92c", "#ffde5e", "#fff4bc", "#ffffff")},
			{Name: "ribbon", Default: pixelfx.Palette("#1e0608", "#45101a", "#7a1a26", "#ad2632", "#d8433f", "#f27a5e", "#ffb79a")},
			{Name: "text", Default: pixelfx.Text("QUEST COMPLETE")},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: newQuestComplete,
}

func init() { pixelfx.Register("ui/quest-complete", QuestComplete) }

var (
	qcDuration = 2.6
	qcL        = 66.0
	qcBH       = 8.0
	qcOpen     = 0.32
	qcType     = 0.2
	qcStep     = 0.036
	qcStamp    = 0.95
	qcSX       = 64.0
	qcSY       = 4.0
	qcPopSeal  = 2.0
	qcRoll     = 2.08
)

func drawQcRibbon(p *pixelfx.Painter, Rb, G []uint32, l, tails float64) {
	if l < 1 {
		return
	}
	if tails > 0.02 {
		for _, s := range [2]float64{-1, 1} {
			drop := 6 * tails
			x0, x1 := s*(l-8), s*(l+16*tails)
			p.Shade(math.Min(x0, x1)-1, -qcBH+drop-1, math.Max(x0, x1)+1, qcBH+drop+1, func(x, y float64, _ *pixelfx.Out) uint32 {
				u, v := (x-x0)/(x1-x0), y-drop
				if u < 0 || u > 1 || math.Abs(v) > qcBH {
					return 0
				}
				if u > 1-(0.3*(qcBH-math.Abs(v)))/qcBH {
					return 0
				}
				e := math.Min(qcBH-math.Abs(v), (1-u)*(x1-x0)*s)
				if e < 1 || u < 0.02 {
					return Rb[0]
				}
				if v < -qcBH+2 {
					return Rb[3]
				}
				if v < 0 {
					return Rb[2]
				}
				return Rb[1]
			})
			p.Tri(s*(l-8), qcBH, s*(l-1), qcBH, s*(l-8), qcBH+drop, Rb[0], 1)
		}
	}
	p.Shade(-l-1, -qcBH-1, l+1, qcBH+1, func(x, y float64, _ *pixelfx.Out) uint32 {
		ax, v := math.Abs(x), y
		if ax > l || math.Abs(v) > qcBH {
			return 0
		}
		e := math.Min(l-ax, qcBH-math.Abs(v))
		if e < 1 {
			return Rb[0]
		}
		if v < -qcBH+2 {
			return Rb[5]
		}
		if v > qcBH-2 {
			return Rb[1]
		}
		if (math.Abs(v+qcBH-2.5) < 0.5 || math.Abs(v-qcBH+3.5) < 0.5) && int(math.Floor(x+0.5))%3 != 0 && ax < l-3 {
			return G[3]
		}
		if v < -1 {
			return Rb[4]
		}
		return Rb[3]
	})
	if tails < 0.02 {
		for _, s := range [2]float64{-1, 1} {
			rx := pixelfx.Round(s * l)
			p.Rect(rx-2, -qcBH-2, 4, qcBH*2+4, Rb[0], 1)
			p.Rect(rx-1, -qcBH-1, 1, qcBH*2+2, Rb[4], 1)
			p.Rect(rx, -qcBH-1, 1, qcBH*2+2, Rb[2], 1)
			p.Px(rx-1, -qcBH-1, Rb[6])
		}
	}
}

func drawQcSeal(p *pixelfx.Painter, G []uint32, x, y, r, sx, sy float64, white bool, k float64) {
	M := r*1.25 + 2
	p.Shade(x-M*sx, y-M*sy, x+M*sx, y+M*sy, func(px, py float64, _ *pixelfx.Out) uint32 {
		dx, dy := (px-x)/sx, (py-y)/sy
		d := pixelfx.Hypot(dx, dy)
		an := math.Atan2(dy, dx)
		edge := r * (1 + 0.09*math.Cos(an*14))
		if d > edge {
			return 0
		}
		if white {
			if edge-d < 1 {
				return G[5]
			}
			return pixelfx.White
		}
		if edge-d < 1 {
			return G[0]
		}
		l := -(dx*0.6 + dy*0.8) / r
		if edge-d < 3 {
			if l > 0.3 {
				return G[6]
			}
			if l > -0.3 {
				return G[5]
			}
			return G[3]
		}
		if math.Abs(d-r*0.66) < 0.5 {
			if l > 0 {
				return G[3]
			}
			return G[2]
		}
		if l > 0.45 {
			return G[7]
		}
		if l > 0.1 {
			return G[6]
		}
		if l > -0.35 {
			return G[5]
		}
		return G[4]
	})
	if white || k <= 0 {
		return
	}
	pts := [3][2]float64{
		{x + (-5)*sx, y + 0*sy},
		{x + (-1.5)*sx, y + 3.5*sy},
		{x + 5.5*sx, y + (-4.5)*sy},
	}
	len1 := pixelfx.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])
	len2 := pixelfx.Hypot(pts[2][0]-pts[1][0], pts[2][1]-pts[1][1])
	done := k * (len1 + len2)
	segs := [2]struct {
		a, b [2]float64
		u    float64
	}{
		{pts[0], pts[1], math.Min(1, done/len1)},
		{pts[1], pts[2], pixelfx.Clamp((done - len1) / len2)},
	}
	for _, st := range [2]struct {
		w float64
		c uint32
	}{{4.2, G[0]}, {2.4, pixelfx.White}} {
		for _, sg := range segs {
			if sg.u > 0 {
				p.Beam(sg.a[0], sg.a[1], sg.a[0]+(sg.b[0]-sg.a[0])*sg.u, sg.a[1]+(sg.b[1]-sg.a[1])*sg.u, st.w, st.c, 1)
			}
		}
	}
}

func newQuestComplete(o *pixelfx.Options) pixelfx.Instance {
	G, Rb := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("ribbon"), 7)
	R := pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{G[7], G[6], G[5], G[3]}})
	lights := &pixelfx.Lights{}
	word := spell(o.Text("text"), "mini")
	const TS = 2.0
	TW := float64(word.w) * TS
	TX, TY := -TW/2-4, -5.0
	var glyphs []wordLetter
	for _, Lt := range word.letters {
		if Lt.g != nil {
			glyphs = append(glyphs, Lt)
		}
	}
	sats := make([]luSat, 7)
	for i := range sats {
		s := -1.0
		if i%2 != 0 {
			s = 1
		}
		x := (float64(i)/6-0.5)*140 + R.Range(-6, 6)
		y := s * R.Range(15, 22)
		t0 := qcStamp + 0.12 + float64(i)*0.12 + R.Range(0, 0.04)
		dur := R.Range(0.3, 0.45)
		L := R.Range(3, 6)
		sats[i] = luSat{x: x, y: y, t0: t0, dur: dur, L: L}
	}
	t, stamped, popped := 0.0, false, false
	end := qcDuration - 0.05

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !stamped && t >= qcStamp {
				stamped = true
				lights.Add(qcSX, qcSY, 40, G[5], 0.7, 0.35)
				lights.Add(qcSX, qcSY, 18, pixelfx.White, 0.7, 0.1)
				for i := range 12 {
					an := (float64(i)/12)*tau + R.Range(-0.2, 0.2)
					mult := 1.0
					if math.Cos(an) > 0 {
						mult = 0.6
					}
					sp := R.Range(40, 80) * mult
					life := R.Range(0.25, 0.45)
					col := pixelfx.Pick(R, []uint32{G[7], G[6], G[5]})
					size := 0.6
					if i%3 == 0 {
						size = R.Range(1.2, 1.8)
					}
					ps.Spawn(pixelfx.StarKind, qcSX+math.Cos(an)*11, qcSY+math.Sin(an)*11, math.Cos(an)*sp, math.Sin(an)*sp*0.7, life, col, size, 0, 0)
				}
			}
			if t > qcStamp && t < qcPopSeal && R.Chance(0.08) {
				px := R.Range(-qcL, qcL)
				py := R.Range(-qcBH, qcBH)
				vx := R.Range(-5, 5)
				vy := -R.Range(8, 20)
				life := math.Min(R.Range(0.4, 0.8), end-t)
				col := pixelfx.Pick(R, []uint32{G[6], G[5]})
				ps.Spawn(pixelfx.StarKind, px, py, vx, vy, life, col, 0.6, 0, 0)
			}
			if !popped && t >= qcPopSeal+0.05 {
				popped = true
				lights.Add(qcSX, qcSY, 30, G[6], 0.6, 0.2)
				for i := range 8 {
					an := R.Range(0, tau)
					sp := R.Range(40, 90)
					life := math.Min(R.Range(0.25, 0.4), end-t)
					col := pixelfx.Pick(R, []uint32{G[7], G[6]})
					size := 0.6
					if i%2 == 0 {
						size = 1.5
					}
					ps.Spawn(pixelfx.StarKind, qcSX+math.Cos(an)*6, qcSY+math.Sin(an)*6, math.Cos(an)*sp, math.Sin(an)*sp, life, col, size, 0, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			jx, jy := shake(t, 2, qcStamp, 0.18, o.Seed(), 32)
			var l float64
			if t < qcRoll {
				l = qcL * pixelfx.Ease.OutCubic(pixelfx.Seg(t, 0, qcOpen))
			} else {
				l = qcL * (1 - pixelfx.Ease.InCubic(pixelfx.Seg(t, qcRoll, qcRoll+0.26)))
			}
			tails := 0.0
			if t < qcRoll {
				tails = pixelfx.Seg(t, qcOpen-0.02, qcOpen+0.12)
			}
			sh := pixelfx.Seg(t, 1.2, 1.5)
			hasShine := sh > 0 && sh < 1
			shine := -qcL - 10 + (2*qcL+20)*sh
			drawQcRibbon(p, Rb, G, l, pixelfx.OutBack(tails, 2))
			if hasShine {
				for y := -qcBH + 1; y < qcBH; y++ {
					x := shine + (y+qcBH)*0.6
					if math.Abs(x) < l-2 {
						p.Px(x, y, Rb[6])
						p.Px(x+1, y, Rb[5])
					}
				}
			}
			var items []wordItem
			for i, Lt := range glyphs {
				u := t - (qcType + float64(i)*qcStep)
				x := TX + float64(Lt.x)*TS + jx
				if u < 0 || math.Abs(x+float64(Lt.w)) > l-3 {
					continue
				}
				s := TS
				if u < 0.035 {
					s = 2.6
				}
				fl := 0.0
				if u < 0.07 {
					fl = 1
				}
				items = append(items, wordItem{
					g: Lt.g, x: x - ((s-TS)*float64(Lt.w))/2, y: TY + jy - ((s-TS)*5)/2,
					sx: s, sy: s, a: 1, flash: fl,
				})
			}
			drawWord(p, items, wordStyle{
				fill: []uint32{G[4], G[5], G[6], G[7]}, edge: pixelfx.White, under: G[3], outline: Rb[0], shadow: Rb[1],
			})
			dk := pixelfx.Seg(t, qcStamp-0.12, qcStamp)
			pk := t - qcPopSeal
			if dk > 0 && pk < 0.08 {
				fall := 1 - pixelfx.Ease.InQuad(dk)
				sq := 0.0
				if t >= qcStamp {
					sq = math.Exp(-(t - qcStamp) * 30)
				}
				s := 1 + 0.8*fall
				sx, sy := s*(1+0.2*sq), s*(1-0.2*sq)
				if pk > 0 {
					sx = 1 + 0.3*(pk/0.08)
					sy = sx
				}
				white := (t >= qcStamp && t < qcStamp+0.05) || pk > 0
				if dk < 1 {
					p.Ellipse(qcSX, qcSY+12, 10*(1-0.3*fall), 2, Rb[0], 0.5*dk)
				}
				drawQcSeal(p, G, qcSX+jx, qcSY-14*fall+jy, 11, sx, sy, white, pixelfx.Seg(t, qcStamp+0.08, qcStamp+0.24))
				extra := 0.0
				if white {
					extra = 0.4
				}
				p.Glow(qcSX, qcSY, 22, G[5], 0.3+extra)
			}
			rk := (t - qcStamp) / 0.3
			rr := 12 + 13*pixelfx.Ease.OutCubic(rk)
			if rk > 0 && rk < 1 {
				col := G[5]
				if rk < 0.3 {
					col = pixelfx.White
				}
				p.EllipseFn(qcSX, qcSY, rr*1.15, rr, func(d, _ float64) (uint32, float64) {
					if d > 1-1.4/rr {
						return col, 1.2 - rk
					}
					return col, 0
				})
			}
			for _, s := range sats {
				u := (t - s.t0) / s.dur
				if u > 0 && u < 1 {
					pixelfx.Sparkle(p, s.x, s.y, s.L*math.Pow(math.Sin(u*pi), 0.7), G[6], G[5], 0.5)
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			p.Glow(0, 0, 76, G[4], 0.2*pixelfx.Seg(t, 0.2, 0.5)*(1-pixelfx.Seg(t, qcRoll, qcRoll+0.2)))
			lights.Draw(p)
		},
	}
}
