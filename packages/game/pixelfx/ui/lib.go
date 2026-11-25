// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx's tintActor adapts the body flash of Sprite Fusion's open-source power-up (https://www.spritefusion.com/pixel-art-vfx/power-up), MIT License, Copyright (c) 2026 Hugo Duprez.

package ui

import (
	"math"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// pi and tau are float64 variables so expressions combining them with constants round step by step as JavaScript does.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

type wordLetter struct {
	ch      rune
	g       *pixelfx.Glyph
	x, w, h int
}

type wordLayout struct {
	letters []wordLetter
	w, h    int
}

// spell lays out the letters of str in a pixel font ("big" when font is empty) left to right in font pixels.
func spell(str, font string) wordLayout {
	if font == "" {
		font = "big"
	}
	F := pixelfx.Fonts[font]
	var letters []wordLetter
	x := 0
	for _, ch := range str {
		var gp *pixelfx.Glyph
		if g, ok := F.Glyphs[ch]; ok {
			gg := g
			gp = &gg
		} else if g, ok := F.Glyphs[unicode.ToUpper(ch)]; ok {
			gg := g
			gp = &gg
		}
		w := F.Space
		if gp != nil {
			w = gp.W
		}
		letters = append(letters, wordLetter{ch: ch, g: gp, x: x, w: w, h: F.H})
		x += w + F.Gap
	}
	return wordLayout{letters: letters, w: max(0, x-F.Gap), h: F.H}
}

type wordItem struct {
	g      *pixelfx.Glyph
	x, y   float64
	sx, sy float64
	a      float64
	flash  float64
	slant  float64
}

type wordStyle struct {
	fill      []uint32
	edge      uint32
	under     uint32
	outline   uint32
	ow        int
	shadow    uint32
	hasShine  bool
	shine     float64
	hasShineY bool
	shineY    float64
	glint     uint32
}

func openRow(r []bool, gx int) bool {
	if r == nil {
		return true
	}
	if r[gx] {
		return false
	}
	if gx > 0 && r[gx-1] {
		return false
	}
	if gx+1 < len(r) && r[gx+1] {
		return false
	}
	return true
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// drawWord draws letters as one word with a crafted 1 px finish: gradient, lit top edge, shaded underside, dark outline, drop shadow, optional diagonal shine and white flash.
func drawWord(p *pixelfx.Painter, items []wordItem, st wordStyle) {
	if len(items) == 0 {
		return
	}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, it := range items {
		gh := float64(len(it.g.Rows))
		h := gh * it.sy
		w := float64(it.g.W) * it.sx
		sl := math.Abs(it.slant) * gh
		x0 = math.Min(x0, it.x-sl)
		x1 = math.Max(x1, it.x+w+sl)
		y0 = math.Min(y0, it.y)
		y1 = math.Max(y1, it.y+h)
	}
	ow := st.ow
	if ow == 0 {
		ow = 1
	}
	ix0 := int(math.Floor(x0)) - 1 - ow
	iy0 := int(math.Floor(y0)) - 1 - ow
	ix1 := int(math.Ceil(x1)) + 2 + ow
	iy1 := int(math.Ceil(y1)) + 3 + ow
	W, H := ix1-ix0, iy1-iy0
	ids := make([]int16, W*H)
	edges := make([]uint8, W*H)
	for k, it := range items {
		g := it.g
		gh := len(g.Rows)
		h := float64(gh) * it.sy
		bot := it.y + h
		sl := it.slant
		for py := int(math.Floor(it.y)); py < int(math.Ceil(bot)); py++ {
			gy := int(math.Floor((float64(py) + 0.5 - it.y) / it.sy))
			if gy < 0 || gy >= gh {
				continue
			}
			shift := pixelfx.Round(((bot - float64(py) - 0.5) / it.sy) * sl)
			ry := float64(py) + 0.5 - it.y - float64(gy)*it.sy
			row := g.Rows[gy]
			for px := int(math.Floor(it.x+shift)) - 1; px <= int(math.Ceil(it.x+shift+float64(g.W)*it.sx)); px++ {
				fx := float64(px) + 0.5 - it.x - shift
				gx := int(math.Floor(fx / it.sx))
				if gx < 0 || gx >= g.W || !row[gx] {
					continue
				}
				i := (py-iy0)*W + (px - ix0)
				if i < 0 || i >= W*H {
					continue
				}
				ids[i] = int16(k + 1)
				rx := fx - float64(gx)*it.sx
				var up, dn []bool
				if gy > 0 {
					up = g.Rows[gy-1]
				}
				if gy < gh-1 {
					dn = g.Rows[gy+1]
				}
				var e uint8
				if ry < 1 && openRow(up, gx) {
					e |= 1
				}
				if ry >= it.sy-1 && openRow(dn, gx) {
					e |= 2
				}
				if rx < 1 && !(gx > 0 && row[gx-1]) {
					e |= 4
				}
				edges[i] = e
			}
		}
	}
	at := func(x, y int) int16 {
		if x < ix0 || y < iy0 || x >= ix1 || y >= iy1 {
			return 0
		}
		return ids[(y-iy0)*W+(x-ix0)]
	}
	F, nF := st.fill, len(st.fill)
	edgeCol := st.edge
	if edgeCol == 0 {
		edgeCol = F[nF-1]
	}
	underCol := st.under
	if underCol == 0 {
		underCol = F[0]
	}
	glint := st.glint
	if glint == 0 {
		glint = pixelfx.White
	}
	sy0 := float64(iy0)
	if st.hasShineY {
		sy0 = st.shineY
	}
	for y := iy0; y < iy1; y++ {
		for x := ix0; x < ix1; x++ {
			id := at(x, y)
			if id != 0 {
				it := items[id-1]
				v := (float64(y) + 0.5 - it.y) / (float64(len(it.g.Rows)) * it.sy)
				idx := int(math.Min(float64(nF-1), math.Max(0, math.Floor((1-v)*float64(nF)))))
				c := F[idx]
				e := edges[(y-iy0)*W+(x-ix0)]
				if e&1 != 0 {
					c = edgeCol
				} else if e&2 != 0 {
					c = underCol
				} else if e&4 != 0 && idx < nF-1 {
					c = F[idx+1]
				}
				if st.hasShine {
					d := math.Abs(float64(x) + 0.5 - (float64(y)+0.5-sy0)*0.6 - st.shine)
					if d < 1.1 {
						c = pixelfx.White
					} else if d < 2.4 {
						c = glint
					}
				}
				if it.flash > 0.6 || (it.flash > 0.25 && v < 0.5) {
					c = pixelfx.White
				}
				p.Dot(float64(x), float64(y), c, it.a)
				continue
			}
			var n int16
			for dy := -ow; dy <= ow && n == 0; dy++ {
				for dx := -ow; dx <= ow && n == 0; dx++ {
					if ow < 2 || absInt(dx)+absInt(dy) < 4 {
						n = at(x+dx, y+dy)
					}
				}
			}
			if n != 0 {
				p.Dot(float64(x), float64(y), st.outline, items[n-1].a)
				continue
			}
			s := at(x-ow, y-1-ow)
			if s == 0 {
				s = at(x+1-ow, y-1-ow)
			}
			if s == 0 {
				s = at(x-ow, y-ow)
			}
			if s != 0 && st.shadow != 0 {
				p.Dot(float64(x), float64(y), st.shadow, items[s-1].a*0.9)
			}
		}
	}
}

// tintActor flashes a character in light: its pixels recolored by brightness into ramp ([shadow, middle, light]).
func tintActor(p *pixelfx.Painter, A *pixelfx.Actor, k float64, ramp [3]uint32) {
	if A == nil || k <= 0.12 {
		return
	}
	m, pix := A.Mask, A.Pixels
	cut := 0.5
	if k > 0.6 {
		cut = 0
	} else if k > 0.3 {
		cut = 0.22
	}
	for j := 0; j < m.H; j++ {
		for i := 0; i < m.W; i++ {
			q := j*m.W + i
			if m.Data[q] == 0 {
				continue
			}
			l := 0.6
			if pix != nil {
				l = pixelfx.Luma(pix[q])
			}
			if l < cut {
				continue
			}
			c := ramp[0]
			if l > 0.5 {
				if k > 0.8 {
					c = pixelfx.White
				} else {
					c = ramp[2]
				}
			} else if l > 0.22 {
				c = ramp[1]
			}
			p.Px(A.X+float64(i), A.Y+float64(j), c)
		}
	}
}

// lightActor adds warm additive light over a character's body and rim lighting just inside its dark outline.
func lightActor(p *pixelfx.Painter, A *pixelfx.Actor, c uint32, k, rim, t float64) {
	if A == nil || (k <= 0.01 && rim <= 0.01) {
		return
	}
	m, pix := A.Mask, A.Pixels
	has := func(i, j int) bool {
		return i >= 0 && j >= 0 && i < m.W && j < m.H && m.Data[j*m.W+i] != 0
	}
	for j := 0; j < m.H; j++ {
		for i := 0; i < m.W; i++ {
			if m.Data[j*m.W+i] == 0 {
				continue
			}
			if k > 0.01 {
				p.Light(A.X+float64(i), A.Y+float64(j), c, k*(1.1-(0.5*float64(j))/float64(m.H)))
			}
			if rim <= 0.01 || !has(i-1, j) || !has(i+1, j) || !has(i, j-1) {
				continue
			}
			if has(i-2, j) && has(i+2, j) && has(i, j-2) {
				continue
			}
			f := rim * (0.75 + 0.35*pixelfx.Noise(float64(j)*0.6-t*8, float64(i)*3))
			if f > 0.25 {
				base := c
				if pix != nil && pix[j*m.W+i] != 0 {
					base = pix[j*m.W+i]
				}
				p.Px(A.X+float64(i), A.Y+float64(j), pixelfx.Mix(base, c, math.Min(0.85, f)))
			}
		}
	}
}

// haloActor draws a glowing 1 px flickering outline just outside a character.
func haloActor(p *pixelfx.Painter, A *pixelfx.Actor, c uint32, k, t float64) {
	if A == nil || k <= 0.02 {
		return
	}
	p.OutlineFn(A.Mask, A.X, A.Y, func(mx, my int) (uint32, float64) {
		return c, k * (0.75 + 0.45*pixelfx.Noise(float64(my)*0.45-t*7, float64(mx)*3))
	})
}

type panelCols struct {
	outline, rim, top, body, low uint32
}

type panelOpts struct {
	r        float64
	a        float64
	hasShine bool
	shine    float64
}

// panel draws a rounded UI plate with dark outline, lit top rim, shaded body and optional diagonal shine.
func panel(p *pixelfx.Painter, x, y, w, h float64, col panelCols, opts panelOpts) {
	if w < 2 || h < 2 {
		return
	}
	r := opts.r
	if r == 0 {
		r = 3
	}
	a := opts.a
	X1, Y1 := x+w, y+h
	p.Shade(x, y, X1, Y1, func(px, py float64, o *pixelfx.Out) uint32 {
		cx := math.Max(x+r-px, math.Max(0, px-(X1-r)))
		cy := math.Max(y+r-py, math.Max(0, py-(Y1-r)))
		var e float64
		if cx > 0 && cy > 0 {
			e = r - pixelfx.Hypot(cx, cy)
		} else {
			e = math.Min(math.Min(px-x, X1-px), math.Min(py-y, Y1-py))
		}
		if e < 0 {
			return 0
		}
		o.A = a
		if e < 1 {
			return col.outline
		}
		if py-y < 2 {
			return col.rim
		}
		if Y1-py < 2 {
			return col.low
		}
		if opts.hasShine {
			d := math.Abs(px - (py-y)*0.6 - opts.shine)
			if d < 1.3 {
				return pixelfx.White
			}
			if d < 3 {
				return col.rim
			}
		}
		if py-y < h*0.45 {
			return col.top
		}
		return col.body
	})
}

var heartRows = []string{
	"..kkk...kkk..",
	".khhok.khook.",
	"khwhookoooomk",
	"khwoooooooomk",
	"khoooooooommk",
	"kooooooooomdk",
	".kooooooomdk.",
	"..kooooomdk..",
	"...kooomdk...",
	"....komdk....",
	".....kdk.....",
	"......k......",
}

func heartPal(C []uint32) map[rune]uint32 {
	return map[rune]uint32{'k': C[0], 'w': pixelfx.White, 'h': C[5], 'o': C[4], 'm': C[3], 'd': C[2]}
}

// shake returns a whole-pixel shake offset that dies away over length seconds starting at t0.
func shake(t, amp, t0, length, seed, hz float64) (float64, float64) {
	if t < t0 || t > t0+length {
		return 0, 0
	}
	k := 1 - (t-t0)/length
	return pixelfx.Round(pixelfx.Noise(t*hz, seed) * amp * k * 1.7), pixelfx.Round(pixelfx.Noise(t*hz, seed+7) * amp * k * 1.3)
}
