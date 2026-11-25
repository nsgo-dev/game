package main

import (
	"image"
	"image/color"
	"math"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/vn"
	"github.com/nsgo-dev/nsgo/core"
)

// The stage's art is drawn in code, so the template needs no image files: backgrounds are vector drawings and portraits are painted into one atlas. Replace a background's function or the atlas with sprite.LoadAtlas when you add art.

// backgroundFunc draws a background over a stage of w by h points, t seconds into the story, which moves its water, fog and lights.
type backgroundFunc func(d *core.Drawing, w, h, t float32)

// backgrounds are the backgrounds <<bg name>> picks, by name.
var backgrounds = map[string]backgroundFunc{
	"dock":   drawDock,
	"river":  drawRiver,
	"fork":   drawFork,
	"fog":    drawFog,
	"harbor": drawHarbor,
}

// drawBackground draws the background name, or a plain night sky for none.
func drawBackground(d *core.Drawing, name string, w, h, t float32) {
	if f, ok := backgrounds[name]; ok {
		f(d, w, h, t)
		return
	}
	sky(d, w, h, h, 0x0b1026, 0x1f2a4d)
}

func drawDock(d *core.Drawing, w, h, t float32) {
	horizon := h * 0.6
	sky(d, w, h, horizon, 0x0b1026, 0x24305a)
	stars(d, w, horizon*0.85, 46)
	// The far shore and the river.
	d.SetFillColor(core.RGB(0x141b33)).BeginPath().MoveTo(0, horizon)
	for i := 0; i <= 8; i++ {
		x := w * float32(i) / 8
		d.LineTo(x, horizon-h*0.03-h*0.025*float32(math.Sin(float64(i)*1.7)))
	}
	d.LineTo(w, horizon).ClosePath().Fill()
	water(d, w, h, horizon, 0x111b36, 0x2c3d6b, t)
	// The dock: planks running out from the left, on posts.
	deck := h * 0.74
	d.SetFillColor(core.RGB(0x3b2a20)).FillRect(0, deck, w*0.58, h*0.05)
	d.SetStrokeColor(core.RGBA(0x1c130e, 0.8)).SetLineWidth(1).BeginPath()
	for x := float32(18); x < w*0.58; x += 22 {
		d.MoveTo(x, deck).LineTo(x, deck+h*0.05)
	}
	d.Stroke()
	d.SetFillColor(core.RGB(0x2a1d16))
	for _, x := range []float32{0.06, 0.24, 0.42, 0.56} {
		d.FillRect(w*x, deck+h*0.05, 8, h*0.12)
	}
	// A lantern on a post at the dock's end, its glow flickering.
	post := w * 0.55
	d.SetFillColor(core.RGB(0x2a1d16)).FillRect(post, deck-h*0.2, 6, h*0.2)
	lantern(d, post+3, deck-h*0.21, h*0.16, 0.85+0.15*float32(math.Sin(float64(t)*7.3)))
}

func drawRiver(d *core.Drawing, w, h, t float32) {
	horizon := h * 0.5
	sky(d, w, h, horizon, 0x0d1428, 0x2a3556)
	stars(d, w, horizon*0.7, 18)
	water(d, w, h, horizon, 0x0f1a33, 0x2f416f, t)
	fog(d, w, h, horizon-h*0.08, 5, 0.12, t)
	// The bow of the ferry and the lantern Mira holds.
	d.SetFillColor(core.RGB(0x2a1d16)).BeginPath().
		MoveTo(w*0.22, h).LineTo(w*0.5, h*0.64).LineTo(w*0.78, h).ClosePath().Fill()
	d.SetStrokeColor(core.RGB(0x4a3426)).SetLineWidth(3).BeginPath().
		MoveTo(w*0.27, h).LineTo(w*0.5, h*0.68).LineTo(w*0.73, h).Stroke()
	lantern(d, w*0.5, h*0.6, h*0.22, 0.9+0.1*float32(math.Sin(float64(t)*5.1)))
}

func drawFork(d *core.Drawing, w, h, t float32) {
	horizon := h * 0.52
	sky(d, w, h, horizon, 0x101a30, 0x2e3a5e)
	stars(d, w, horizon*0.8, 24)
	water(d, w, h, horizon, 0x0f1a33, 0x2f416f, t)
	// The island that splits the river, the left bank and the marsh on the right.
	d.SetFillColor(core.RGB(0x16203a)).BeginPath().
		MoveTo(w*0.42, h).LineTo(w*0.5, horizon+h*0.04).LineTo(w*0.58, h).ClosePath().Fill()
	d.SetFillColor(core.RGB(0x131a30)).BeginPath().
		MoveTo(0, horizon-h*0.02).LineTo(w*0.3, horizon).LineTo(w*0.12, h*0.75).LineTo(0, h*0.8).ClosePath().Fill()
	d.SetStrokeColor(core.RGB(0x3c5a4a)).SetLineWidth(2).BeginPath()
	for i := 0; i < 26; i++ {
		x := w*0.7 + w*0.3*float32(i)/26
		top := horizon - h*0.02 - h*0.05*float32(math.Abs(math.Sin(float64(i)*2.3)))
		sway := 3 * float32(math.Sin(float64(t)*1.3+float64(i)))
		d.MoveTo(x, horizon+h*0.06).LineTo(x+sway, top)
	}
	d.Stroke()
	// The light on the left bank, blinking.
	blink := 0.5 + 0.5*float32(math.Sin(float64(t)*2.4))
	d.SetFillColor(core.RGB(0x1d2236)).FillRect(w*0.16, horizon-h*0.16, 10, h*0.15)
	glow(d, w*0.16+5, horizon-h*0.17, h*0.12, 0xf6e7a8, 0.25+0.6*blink)
	fog(d, w, h, horizon-h*0.05, 3, 0.08, t)
}

func drawFog(d *core.Drawing, w, h, t float32) {
	sky(d, w, h, h, 0x363f54, 0x5b667c)
	water(d, w, h, h*0.62, 0x3c475e, 0x667189, t)
	fog(d, w, h, h*0.1, 9, 0.18, t)
	// A last glimmer of the false light, far off and fading.
	glow(d, w*0.78, h*0.42, h*0.06, 0xf6e7a8, 0.25+0.1*float32(math.Sin(float64(t)*1.1)))
}

func drawHarbor(d *core.Drawing, w, h, t float32) {
	horizon := h * 0.58
	d.SetFillGradient(core.CanvasLinear(0, 0, 0, horizon,
		core.GradientStop{Offset: 0, Color: core.RGB(0x3d4a7a)},
		core.GradientStop{Offset: 0.6, Color: core.RGB(0xe98a6b)},
		core.GradientStop{Offset: 1, Color: core.RGB(0xf6c177)},
	)).FillRect(0, 0, w, horizon)
	// The sun rising behind the harbor.
	d.SetFillColor(core.RGB(0xfde7b0)).BeginPath().Arc(w*0.62, horizon, h*0.09, math.Pi, 2*math.Pi, false).Fill()
	// The harbor's roofs and its bell tower.
	d.SetFillColor(core.RGB(0x2b2440)).BeginPath().MoveTo(0, horizon)
	x := float32(0)
	for i := 0; x < w; i++ {
		top := horizon - h*(0.05+0.04*float32(i%3))
		d.LineTo(x, top).LineTo(x+w*0.06, top)
		x += w * 0.06
	}
	d.LineTo(w, horizon).ClosePath().Fill()
	d.FillRect(w*0.3, horizon-h*0.22, w*0.035, h*0.22)
	d.BeginPath().MoveTo(w*0.295, horizon-h*0.22).LineTo(w*0.3175, horizon-h*0.28).LineTo(w*0.34, horizon-h*0.22).ClosePath().Fill()
	water(d, w, h, horizon, 0x4b5a86, 0xf1b98a, t)
	// The sun's path on the water.
	d.SetFillColor(core.RGBA(0xfde7b0, 0.35))
	for i := 0; i < 6; i++ {
		y := horizon + h*0.05*float32(i+1)
		half := w * (0.02 + 0.012*float32(i))
		d.FillRect(w*0.62-half, y, half*2, 2)
	}
}

// sky fills the stage above horizon with a gradient from top to bottom, and below it with bottom.
func sky(d *core.Drawing, w, h, horizon float32, top, bottom uint32) {
	d.SetFillColor(core.RGB(bottom)).FillRect(0, 0, w, h)
	d.SetFillGradient(core.CanvasLinear(0, 0, 0, horizon,
		core.GradientStop{Offset: 0, Color: core.RGB(top)},
		core.GradientStop{Offset: 1, Color: core.RGB(bottom)},
	)).FillRect(0, 0, w, horizon)
}

// stars scatters n stars above the line y = below, at the same places every time.
func stars(d *core.Drawing, w, below float32, n int) {
	seed := uint32(0x9e3779b9)
	next := func() float32 {
		seed = seed*1664525 + 1013904223
		return float32(seed>>8) / float32(1<<24)
	}
	d.SetFillColor(core.RGBA(0xf8fafc, 0.8))
	for range n {
		x, y, r := next()*w, next()*below, 0.6+next()*1.2
		d.BeginPath().Ellipse(x, y, r, r).Fill()
	}
}

// water fills the stage below top with a river whose wave lines drift with t.
func water(d *core.Drawing, w, h, top float32, deep, shine uint32, t float32) {
	d.SetFillGradient(core.CanvasLinear(0, top, 0, h,
		core.GradientStop{Offset: 0, Color: core.RGB(shine)},
		core.GradientStop{Offset: 0.25, Color: core.RGB(deep)},
		core.GradientStop{Offset: 1, Color: core.RGB(deep)},
	)).FillRect(0, top, w, h-top)
	d.SetStrokeColor(core.RGBA(shine, 0.35)).SetLineWidth(1.5).BeginPath()
	for row := 0; row < 7; row++ {
		y := top + (h-top)*(0.08+0.13*float32(row))
		offset := float32(math.Mod(float64(t*(12+float32(row)*4)), 90))
		for x := -90 + offset; x < w; x += 90 {
			d.MoveTo(x, y).LineTo(x+30+float32(row)*4, y)
		}
	}
	d.Stroke()
}

// fog draws n drifting bands of fog from top down, each alpha opaque.
func fog(d *core.Drawing, w, h, top float32, n int, alpha, t float32) {
	for i := range n {
		y := top + (h-top)*float32(i)/float32(n)
		width := w * (0.5 + 0.1*float32(i%3))
		x := float32(math.Mod(float64(t*(8+float32(i)*3)+float32(i)*w*0.37), float64(w+width))) - width
		d.SetFillColor(core.RGBA(0xdfe6f0, alpha)).BeginPath().RoundRect(x, y, width, h*0.07, h*0.035).Fill()
	}
}

// lantern draws a lantern hanging at (x, y) with a warm glow of radius r, as bright as strength.
func lantern(d *core.Drawing, x, y, r, strength float32) {
	glow(d, x, y, r, 0xffc56b, 0.55*strength)
	d.SetFillColor(core.RGB(0x3a2a1c)).FillRect(x-7, y-12, 14, 3)
	d.SetFillColor(core.RGBA(0xffd27a, strength)).BeginPath().RoundRect(x-6, y-9, 12, 16, 3).Fill()
	d.SetFillColor(core.RGB(0x3a2a1c)).FillRect(x-7, y+7, 14, 3)
}

// glow draws a soft round light of color at (x, y) fading out at radius r.
func glow(d *core.Drawing, x, y, r float32, c uint32, alpha float32) {
	d.SetFillGradient(core.CanvasRadial(x, y, 0, x, y, r,
		core.GradientStop{Offset: 0, Color: core.RGBA(c, alpha)},
		core.GradientStop{Offset: 1, Color: core.RGBA(c, 0)},
	)).BeginPath().Ellipse(x, y, r, r).Fill()
}

// Portrait frames are portraitW by portraitH pixels.
const (
	portraitW = 140
	portraitH = 240
)

// look is how a character is painted: the colors of their coat, scarf or collar, and hair, and whether they wear a ferryman's cap and beard.
type look struct {
	coat, trim, hair color.RGBA
	ferryman         bool
}

// looks are the cast's looks by character id. A cast member without one is painted in grey.
var looks = map[string]look{
	"mira":  {coat: rgb(0x34405f), trim: rgb(0xe8a25c), hair: rgb(0x6b3324)},
	"tobin": {coat: rgb(0x2f5d55), trim: rgb(0xc9d3c4), hair: rgb(0x3a3430), ferryman: true},
}

// portraitAtlas paints every expression of every cast member as a frame of one atlas, named as cast.yaml names it.
func portraitAtlas(cast *vn.Cast) *sprite.Atlas {
	n := 0
	for _, ch := range cast.Characters {
		n += len(ch.Expressions)
	}
	img := image.NewRGBA(image.Rect(0, 0, portraitW*n, portraitH))
	atlas := &sprite.Atlas{Frames: map[string]sprite.Frame{}}
	i := 0
	for _, ch := range cast.Characters {
		lk, ok := looks[ch.ID]
		if !ok {
			lk = look{coat: rgb(0x555b66), trim: rgb(0x9aa1ad), hair: rgb(0x2b2b2b)}
		}
		for _, e := range ch.Expressions {
			x0 := i * portraitW
			paintPortrait(img, x0, lk, e.Name)
			atlas.Frames[e.Frame] = sprite.Frame{Name: e.Frame, X: float32(x0), W: portraitW, H: portraitH, Width: portraitW, Height: portraitH}
			i++
		}
	}
	atlas.Image = core.ImageSource{Pixels: img, Version: 1}
	return atlas
}

// paintPortrait paints one portrait at x0 of img: shoulders in the coat, a head with hair, and brows and a mouth that show expr.
func paintPortrait(img *image.RGBA, x0 int, lk look, expr string) {
	skin := rgb(0xf0cfb2)
	ink := rgb(0x2a1d1a)
	cx := x0 + portraitW/2
	// Shoulders and coat.
	for y := 150; y < portraitH; y++ {
		half := min(34+(y-150)*2/3, portraitW/2-1)
		for x := cx - half; x < cx+half; x++ {
			img.SetRGBA(x, y, lk.coat)
		}
	}
	// A scarf or collar.
	for y := 142; y < 162; y++ {
		for x := cx - 30; x < cx+30; x++ {
			img.SetRGBA(x, y, lk.trim)
		}
	}
	// Neck, head and hair.
	fillRect(img, cx-10, 124, 20, 22, skin)
	if !lk.ferryman {
		// Long hair falling behind the shoulders.
		fillEllipse(img, cx, 100, 50, 62, lk.hair)
	}
	fillEllipse(img, cx, 92, 40, 46, skin)
	if lk.ferryman {
		// A short beard and a flat cap.
		fillEllipse(img, cx, 120, 30, 16, lk.hair)
		fillEllipse(img, cx, 52, 46, 16, lk.coat)
		fillRect(img, cx-44, 54, 88, 8, lk.coat)
	} else {
		fillEllipse(img, cx, 58, 42, 20, lk.hair)
	}
	// Eyes.
	fillEllipse(img, cx-15, 92, 4, 5, ink)
	fillEllipse(img, cx+15, 92, 4, 5, ink)
	// Brows by expression.
	for dx := -9; dx <= 9; dx++ {
		left, right := 80, 80
		switch expr {
		case "worry": // Inner ends raised.
			left, right = 80-dx/3, 80+dx/3
		case "stern": // Inner ends lowered.
			left, right = 80+dx/3, 80-dx/3
		case "smile":
			left, right = 79, 79
		}
		img.SetRGBA(cx-15+dx, left, ink)
		img.SetRGBA(cx-15+dx, left+1, ink)
		img.SetRGBA(cx+15+dx, right, ink)
		img.SetRGBA(cx+15+dx, right+1, ink)
	}
	// Mouth by expression.
	for dx := -11; dx <= 11; dx++ {
		y := 112
		switch expr {
		case "smile":
			y += 4 - dx*dx/30
		case "worry":
			y += dx * dx / 30
		case "stern":
			if dx < -7 || dx > 7 {
				continue
			}
		}
		img.SetRGBA(cx+dx, y, ink)
		img.SetRGBA(cx+dx, y+1, ink)
	}
}

func fillRect(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	for py := y; py < y+h; py++ {
		for px := x; px < x+w; px++ {
			img.SetRGBA(px, py, c)
		}
	}
}

func fillEllipse(img *image.RGBA, cx, cy, rx, ry int, c color.RGBA) {
	for y := cy - ry; y <= cy+ry; y++ {
		for x := cx - rx; x <= cx+rx; x++ {
			dx, dy := float64(x-cx)/float64(rx), float64(y-cy)/float64(ry)
			if dx*dx+dy*dy <= 1 {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func rgb(v uint32) color.RGBA {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}
