package vn

import (
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/text"
	"github.com/nsgo-dev/nsgo/core"
)

// Style sets how Draw and DrawBacklog draw a presenter. Its zero value draws portraits at their frames' size and a translucent dark text box with platform text; a zero field uses the default its comment gives.
type Style struct {
	// Atlas holds the portraits' frames, which Cast.CheckFrames checks; nil draws no portraits.
	Atlas *sprite.Atlas
	// PortraitHeight scales each portrait to this height in points, keeping its aspect; 0 draws frames at their own size.
	PortraitHeight float32
	// PortraitBottom is how far above the stage's bottom edge the portraits stand.
	PortraitBottom float32
	// InactiveAlpha is the opacity of portraits whose character does not speak the line shown, while one on the stage does; 0 draws them opaque.
	InactiveAlpha float32

	// Box draws the text box; nil fills a rounded rectangle in BoxColor.
	Box *sprite.NineSlice
	// BoxColor fills the text box when Box is nil; 0 uses a translucent dark blue.
	BoxColor core.Color
	// BoxHeight is the text box's height; 0 uses 28% of the stage's height.
	BoxHeight float32
	// Margin is the space between the text box and the stage's edges; 0 uses 16.
	Margin float32
	// Padding is the space between the text box's edges and its text; 0 uses 16.
	Padding float32

	// Font draws the line's text as a bitmap font; nil draws platform text in TextFont.
	Font *text.Font
	// TextFont is the line's platform font; a zero size uses 20.
	TextFont core.Font
	// TextColor is the line's color; 0 uses white.
	TextColor core.Color
	// NameFont is the name plate's platform font; a zero size uses 18, bold.
	NameFont core.Font
	// NameColor is the name plate's text color; 0 uses white.
	NameColor core.Color
	// NamePlateColor fills the name plate of a speaker without a cast color; 0 uses a slate blue.
	NamePlateColor core.Color

	// ChoiceWidth is each choice button's width; 0 uses 60% of the stage's width.
	ChoiceWidth float32
	// ChoiceHeight is each choice button's height; 0 uses 44.
	ChoiceHeight float32
	// ChoiceGap is the space between choice buttons; 0 uses 12.
	ChoiceGap float32
	// ChoiceColor fills the choice buttons; 0 uses the box color.
	ChoiceColor core.Color

	// BacklogColor fills the backlog behind its entries; 0 uses a near-opaque dark blue.
	BacklogColor core.Color
}

// Layout is where Draw puts a presenter's text box and choices on a stage, for a game to hit test taps against.
type Layout struct {
	// Box is the text box.
	Box core.Bounds
	// Text is the area of the box the line's text wraps in.
	Text core.Bounds
	// Choices are the choice buttons, one for each option in the order Choices returns them.
	Choices []core.Bounds
}

func (st Style) margin() float32  { return orDefault(st.Margin, 16) }
func (st Style) padding() float32 { return orDefault(st.Padding, 16) }

func (st Style) boxColor() core.Color {
	if st.BoxColor != 0 {
		return st.BoxColor
	}
	return core.RGBA(0x101828, 0.86)
}

func (st Style) textFont() core.Font {
	f := st.TextFont
	if f.Size <= 0 {
		f.Size = 20
	}
	return f
}

func (st Style) nameFont() core.Font {
	f := st.NameFont
	if f.Size <= 0 {
		f.Size = 18
		f.Weight = core.FontWeightBold
	}
	return f
}

func orColor(c, def core.Color) core.Color {
	if c != 0 {
		return c
	}
	return def
}

func orDefault(v, def float32) float32 {
	if v > 0 {
		return v
	}
	return def
}

// Layout returns where Draw puts the text box and n choice buttons on a stage of size.
func (st Style) Layout(size core.Size, n int) Layout {
	w, h := float32(size.Width), float32(size.Height)
	m, pad := st.margin(), st.padding()
	boxH := orDefault(st.BoxHeight, h*0.28)
	box := bounds(m, h-m-boxH, max(w-2*m, 0), boxH)
	l := Layout{
		Box:  box,
		Text: bounds(float32(box.Origin.X)+pad, float32(box.Origin.Y)+pad, max(float32(box.Size.Width)-2*pad, 0), max(boxH-2*pad, 0)),
	}
	if n > 0 {
		cw := orDefault(st.ChoiceWidth, w*0.6)
		ch := orDefault(st.ChoiceHeight, 44)
		gap := orDefault(st.ChoiceGap, 12)
		total := float32(n)*ch + float32(n-1)*gap
		top := (float32(box.Origin.Y) - total) / 2
		for i := range n {
			l.Choices = append(l.Choices, bounds((w-cw)/2, top+float32(i)*(ch+gap), cw, ch))
		}
	}
	return l
}

// ChoiceAt returns the Index of the choice button at pt on a stage of size, for Choose, and whether pt is on an available one.
func (p *Presenter) ChoiceAt(size core.Size, st Style, pt core.Point) (int, bool) {
	choices := p.Choices()
	for i, b := range st.Layout(size, len(choices)).Choices {
		if contains(b, pt) && choices[i].Available {
			return choices[i].Index, true
		}
	}
	return 0, false
}

// Draw draws the presenter on a stage of size: the portraits, dimming those not speaking; the text box with the line's name plate and the text the typewriter has revealed; and the choice buttons while choices wait.
func (p *Presenter) Draw(d *core.Drawing, size core.Size, st Style) {
	if d == nil {
		return
	}
	p.drawPortraits(d, size, st)
	choices := p.Choices()
	l := st.Layout(size, len(choices))
	// A node can open with options, so the choice buttons are drawn with or without a line shown.
	if line, ok := p.Line(); ok {
		p.drawLine(d, st, l, line)
	}
	for i, c := range choices {
		b := l.Choices[i]
		d.Save()
		if !c.Available {
			d.SetAlpha(0.45)
		}
		d.SetFillColor(orColor(st.ChoiceColor, st.boxColor())).BeginPath().
			RoundRect(float32(b.Origin.X), float32(b.Origin.Y), float32(b.Size.Width), float32(b.Size.Height), 10).Fill()
		d.Restore()
		cs := text.PlatformStyle{Font: st.textFont(), Color: orColor(st.TextColor, core.RGB(0xffffff)), Align: text.AlignCenter, Baseline: core.BaselineMiddle, MaxWidth: float32(b.Size.Width)}
		if !c.Available {
			cs.Alpha = 0.45
		}
		text.DrawPlatform(d, text.ParseRich(c.Text).PlainText(), float32(b.Origin.X), float32(b.Origin.Y)+float32(b.Size.Height)/2, cs)
	}
}

// drawLine draws the text box with line's name plate and the text the typewriter has revealed.
func (p *Presenter) drawLine(d *core.Drawing, st Style, l Layout, line Line) {
	box := l.Box
	if st.Box != nil {
		st.Box.Draw(d, float32(box.Origin.X), float32(box.Origin.Y), float32(box.Size.Width), float32(box.Size.Height))
	} else {
		d.Save().SetFillColor(st.boxColor()).BeginPath().
			RoundRect(float32(box.Origin.X), float32(box.Origin.Y), float32(box.Size.Width), float32(box.Size.Height), 12).
			Fill().Restore()
	}

	if line.Name != "" {
		pad := st.padding()
		nf := st.nameFont()
		ns := text.PlatformStyle{Font: nf, Color: orColor(st.NameColor, core.RGB(0xffffff)), Baseline: core.BaselineMiddle}
		nameW := float32(text.MeasurePlatform(d, line.Name, ns).Width)
		plateW, plateH := nameW+pad*1.5, float32(nf.Size)+pad
		x, y := float32(box.Origin.X)+pad, float32(box.Origin.Y)-plateH*0.6
		plate := orColor(st.NamePlateColor, core.RGB(0x3b4b6b))
		if line.Color != "" {
			if c, err := core.ParseColor(line.Color); err == nil {
				plate = c
			}
		}
		d.Save().SetFillColor(plate).BeginPath().RoundRect(x, y, plateW, plateH, 8).Fill().Restore()
		text.DrawPlatform(d, line.Name, x+pad*0.75, y+plateH/2, ns)
	}

	if tw := p.Typewriter(); tw != nil {
		color := orColor(st.TextColor, core.RGB(0xffffff))
		pos := l.Text.Origin
		if line.Name != "" {
			pos.Y += core.Points(st.padding() * 0.5)
		}
		opts := text.RichOptions{Options: text.Options{MaxWidth: float32(l.Text.Size.Width)}, Color: color}
		if st.Font != nil {
			tw.Draw(d, st.Font, pos, opts)
		} else {
			tw.DrawPlatform(d, pos, text.PlatformStyle{Font: st.textFont(), Color: color, MaxWidth: float32(l.Text.Size.Width)}, opts)
		}
	}
}

func (p *Presenter) drawPortraits(d *core.Drawing, size core.Size, st Style) {
	if st.Atlas == nil {
		return
	}
	speaking := false
	for _, pt := range p.portraits {
		speaking = speaking || pt.Speaking
	}
	w, h := float32(size.Width), float32(size.Height)
	for _, pt := range p.portraits {
		f, ok := st.Atlas.Frames[pt.Frame]
		if !ok || f.Width <= 0 || f.Height <= 0 {
			continue
		}
		scale := float32(1)
		if st.PortraitHeight > 0 {
			scale = st.PortraitHeight / f.Height
		}
		left := pt.X*w - f.Width*scale/2
		top := h - st.PortraitBottom - f.Height*scale
		d.Save()
		if speaking && !pt.Speaking && st.InactiveAlpha > 0 {
			d.SetAlpha(st.InactiveAlpha)
		}
		d.DrawImageFrom(st.Atlas.Image, f.X, f.Y, f.W, f.H, left+f.OffsetX*scale, top+f.OffsetY*scale, f.W*scale, f.H*scale)
		d.Restore()
	}
}

// DrawBacklog draws the backlog over a stage of size: each entry's name in its plate color above its text, the newest at the bottom, skipping the newest scroll entries so a game can scroll back through it. An entry taller than the stage is drawn alone from the top, cut off at the bottom, so every entry can be read. It returns how many entries it drew.
func (p *Presenter) DrawBacklog(d *core.Drawing, size core.Size, st Style, scroll int) int {
	if d == nil {
		return 0
	}
	w, h := float32(size.Width), float32(size.Height)
	d.Save().SetFillColor(orColor(st.BacklogColor, core.RGBA(0x0b1020, 0.94))).FillRect(0, 0, w, h).Restore()
	m, pad := st.margin(), st.padding()
	textW := max(w-2*m-2*pad, 0)
	ts := text.PlatformStyle{Font: st.textFont(), Color: orColor(st.TextColor, core.RGB(0xffffff)), MaxWidth: textW, Baseline: core.BaselineTop}
	nf := st.nameFont()
	y := h - m
	drawn := 0
	for i := len(p.backlog) - 1 - max(scroll, 0); i >= 0; i-- {
		e := p.backlog[i]
		body := text.ParseRich(e.Text).PlainText()
		es := ts
		if e.Choice {
			es.Alpha = 0.7
			body = "> " + body
		}
		bodyH := float32(text.MeasurePlatform(d, body, es).Height)
		entryH := bodyH
		if e.Name != "" {
			entryH += float32(nf.Size) + pad*0.5
		}
		top := y - entryH
		if top < m {
			if drawn > 0 {
				break
			}
			top = m
		}
		x := m + pad
		ty := top
		if e.Name != "" {
			color := orColor(st.NamePlateColor, core.RGB(0x8fa3c8))
			if c, err := core.ParseColor(e.Color); e.Color != "" && err == nil {
				color = c
			}
			text.DrawPlatform(d, e.Name, x, ty, text.PlatformStyle{Font: nf, Color: color, Baseline: core.BaselineTop})
			ty += float32(nf.Size) + pad*0.5
		}
		text.DrawPlatform(d, body, x, ty, es)
		y = top - pad
		drawn++
	}
	return drawn
}

func bounds(x, y, w, h float32) core.Bounds {
	return core.Bounds{Origin: core.Point{X: core.Points(x), Y: core.Points(y)}, Size: core.Size{Width: core.Points(w), Height: core.Points(h)}}
}

func contains(b core.Bounds, pt core.Point) bool {
	return pt.X >= b.Origin.X && pt.X < b.Origin.X+b.Size.Width && pt.Y >= b.Origin.Y && pt.Y < b.Origin.Y+b.Size.Height
}
