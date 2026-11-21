package gameui

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

func rectPath(x, y, w, h, r float32) *core.Path {
	p := new(core.Path)
	if w <= 0 || h <= 0 {
		return p
	}
	if r > 0 {
		maxR := min(w, h) / 2
		if r > maxR {
			r = maxR
		}
		return p.RoundRect(x, y, w, h, r)
	}
	return p.Rect(x, y, w, h)
}

func resolveRadius(override, base float32) float32 {
	if override < 0 {
		return 0
	}
	if override > 0 {
		return override
	}
	return max(0, base)
}

func drawCustomBackground(d *core.Drawing, fn func(*core.Drawing, core.Bounds), b core.Bounds) {
	d.Save()
	fn(d, b)
	d.Restore()
}

func drawFocusRing(d *core.Drawing, b core.Bounds, radius float32, theme *Theme) {
	if theme.FocusedBorder == 0 || theme.FocusedBorder == core.Transparent || theme.FocusWidth <= 0 {
		return
	}
	x, y := float32(b.Origin.X), float32(b.Origin.Y)
	w, h := float32(b.Size.Width), float32(b.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	d.SetStrokeColor(theme.FocusedBorder).
		SetLineWidth(theme.FocusWidth).
		StrokePath(rectPath(x, y, w, h, radius))
}

func (p *Panel) draw(d *core.Drawing, theme *Theme, _ WidgetState) {
	b := p.bounds
	w, h := float32(b.Size.Width), float32(b.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	if p.DrawBackground != nil {
		drawCustomBackground(d, p.DrawBackground, b)
		return
	}
	x, y := float32(b.Origin.X), float32(b.Origin.Y)
	radius := resolveRadius(p.CornerRadius, theme.CornerRadius)
	shape := rectPath(x, y, w, h, radius)

	bg := p.Background
	if bg == 0 {
		bg = theme.PanelBackground
	}
	if bg != 0 && bg != core.Transparent {
		d.SetFillColor(bg).FillPath(shape, core.NonZero)
	}

	border := p.Border
	if border == 0 {
		border = theme.PanelBorder
	}
	if border != 0 && border != core.Transparent && theme.BorderWidth > 0 {
		d.SetStrokeColor(border).SetLineWidth(theme.BorderWidth).StrokePath(shape)
	}
}

func (b *VBox) draw(d *core.Drawing, _ *Theme, _ WidgetState) {
	if b.DrawBackground != nil && b.bounds.Size.Width > 0 && b.bounds.Size.Height > 0 {
		drawCustomBackground(d, b.DrawBackground, b.bounds)
	}
}

func (b *HBox) draw(d *core.Drawing, _ *Theme, _ WidgetState) {
	if b.DrawBackground != nil && b.bounds.Size.Width > 0 && b.bounds.Size.Height > 0 {
		drawCustomBackground(d, b.DrawBackground, b.bounds)
	}
}

func (l *Label) draw(d *core.Drawing, theme *Theme, _ WidgetState) {
	b := l.bounds
	w, h := float32(b.Size.Width), float32(b.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	if l.DrawBackground != nil {
		drawCustomBackground(d, l.DrawBackground, b)
	}
	if l.Text == "" {
		return
	}
	x, y := float32(b.Origin.X), float32(b.Origin.Y)
	font := resolveFont(l.Font, theme.Font)
	col := l.Color
	if col == 0 {
		col = theme.Text
	}
	align := l.TextAlign
	if align == 0 {
		align = core.TextAlignStart
	}
	tx := x
	switch align {
	case core.TextAlignCenter:
		tx = x + w/2
	case core.TextAlignEnd, core.TextAlignRight:
		tx = x + w
	}
	d.SetFillColor(col).
		SetTextAlign(align).
		SetTextBaseline(core.BaselineMiddle).
		FillText(l.Text, tx, y+h/2, font)
}

func (b *Button) draw(d *core.Drawing, theme *Theme, state WidgetState) {
	rect := b.bounds
	w, h := float32(rect.Size.Width), float32(rect.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	x, y := float32(rect.Origin.X), float32(rect.Origin.Y)
	radius := resolveRadius(0, theme.CornerRadius)

	fill := theme.Background
	border := theme.Border
	textColor := theme.Text
	switch {
	case state.Disabled:
		fill = theme.Disabled
		textColor = theme.DisabledText
	case state.Pressed:
		fill = theme.Active
		border = theme.Accent
	case state.Hovered:
		fill = theme.Hover
	}

	if b.DrawBackground != nil {
		drawCustomBackground(d, b.DrawBackground, rect)
	} else {
		shape := rectPath(x, y, w, h, radius)
		d.SetFillColor(fill).FillPath(shape, core.NonZero)
		if border != 0 && border != core.Transparent && theme.BorderWidth > 0 {
			d.SetStrokeColor(border).SetLineWidth(theme.BorderWidth).StrokePath(shape)
		}
	}

	if state.Focused && !state.Disabled {
		drawFocusRing(d, rect, radius, theme)
	}

	if b.Text != "" {
		font := resolveFont(b.Font, theme.Font)
		d.SetFillColor(textColor).
			SetTextAlign(core.TextAlignCenter).
			SetTextBaseline(core.BaselineMiddle).
			FillText(b.Text, x+w/2, y+h/2, font)
	}
}

func (t *Toggle) draw(d *core.Drawing, theme *Theme, state WidgetState) {
	rect := t.bounds
	w, h := float32(rect.Size.Width), float32(rect.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	x, y := float32(rect.Origin.X), float32(rect.Origin.Y)
	radius := resolveRadius(0, theme.CornerRadius)

	if t.DrawBackground != nil {
		drawCustomBackground(d, t.DrawBackground, rect)
	} else if !state.Disabled {
		if state.Pressed {
			d.SetFillColor(theme.Active).FillPath(rectPath(x, y, w, h, radius), core.NonZero)
		} else if state.Hovered {
			d.SetFillColor(theme.Hover).FillPath(rectPath(x, y, w, h, radius), core.NonZero)
		}
	}

	if state.Focused && !state.Disabled {
		drawFocusRing(d, rect, radius, theme)
	}

	font := resolveFont(t.Font, theme.Font)
	pad := min(max(theme.Padding*0.5, 4), w*0.25)
	boxSize := min(max(float32(font.Size)*1.15, 16), max(h-4, 8))
	cy := y + h/2

	accent := theme.Accent
	textColor := theme.Text
	trackColor := theme.Track
	if state.Disabled {
		accent = theme.Disabled
		textColor = theme.DisabledText
		trackColor = theme.Disabled
	} else if state.Pressed {
		accent = theme.AccentActive
	} else if state.Hovered {
		accent = theme.AccentHover
	}

	var indicatorRight float32
	if t.Style == ToggleSwitch {
		tw := min(boxSize*1.8, max(w-2*pad, boxSize))
		th := boxSize
		tx := x + pad
		ty := cy - th/2
		pill := rectPath(tx, ty, tw, th, th/2)
		fill := trackColor
		if t.Checked {
			fill = accent
		}
		d.SetFillColor(fill).FillPath(pill, core.NonZero)
		if theme.BorderWidth > 0 {
			d.SetStrokeColor(theme.Border).SetLineWidth(theme.BorderWidth).StrokePath(pill)
		}
		knobR := max(th/2-2.5, 2)
		knobX := tx + th/2
		if t.Checked {
			knobX = tx + tw - th/2
		}
		knob := new(core.Path).Arc(knobX, cy, knobR, 0, 2*math.Pi, false)
		d.SetFillColor(textColor).FillPath(knob, core.NonZero)
		indicatorRight = tx + tw
	} else {
		bx := x + pad
		by := cy - boxSize/2
		boxR := min(radius*0.6, boxSize/4)
		box := rectPath(bx, by, boxSize, boxSize, boxR)
		fill := trackColor
		border := theme.Border
		if t.Checked {
			fill = accent
			if !state.Disabled {
				border = accent
			}
		}
		d.SetFillColor(fill).FillPath(box, core.NonZero)
		if theme.BorderWidth > 0 {
			d.SetStrokeColor(border).SetLineWidth(theme.BorderWidth).StrokePath(box)
		}
		if t.Checked {
			check := new(core.Path).
				MoveTo(bx+boxSize*0.22, by+boxSize*0.52).
				LineTo(bx+boxSize*0.44, by+boxSize*0.74).
				LineTo(bx+boxSize*0.80, by+boxSize*0.28)
			d.SetStrokeColor(textColor).
				SetLineWidth(max(2, boxSize*0.12)).
				SetLineCap(core.CapRound).
				SetLineJoin(core.JoinRound).
				StrokePath(check)
		}
		indicatorRight = bx + boxSize
	}

	if t.Label != "" {
		gap := max(theme.Padding*0.75, 8)
		d.SetFillColor(textColor).
			SetTextAlign(core.TextAlignStart).
			SetTextBaseline(core.BaselineMiddle).
			FillText(t.Label, indicatorRight+gap, cy, font)
	}
}

func (s *Slider) draw(d *core.Drawing, theme *Theme, state WidgetState) {
	rect := s.bounds
	w, h := float32(rect.Size.Width), float32(rect.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	x, y := float32(rect.Origin.X), float32(rect.Origin.Y)
	radius := resolveRadius(0, theme.CornerRadius)

	if s.DrawBackground != nil {
		drawCustomBackground(d, s.DrawBackground, rect)
	} else if !state.Disabled {
		if state.Pressed {
			d.SetFillColor(theme.Active).FillPath(rectPath(x, y, w, h, radius), core.NonZero)
		} else if state.Hovered {
			d.SetFillColor(theme.Hover).FillPath(rectPath(x, y, w, h, radius), core.NonZero)
		}
	}

	if state.Focused && !state.Disabled {
		drawFocusRing(d, rect, radius, theme)
	}

	textColor := theme.Text
	trackColor := theme.Track
	fillColor := theme.Accent
	if state.Disabled {
		textColor = theme.DisabledText
		trackColor = theme.Disabled
		fillColor = theme.DisabledText
	} else if state.Pressed {
		fillColor = theme.AccentActive
	} else if state.Hovered {
		fillColor = theme.AccentHover
	}

	cy := y + h/2
	if s.Label != "" {
		font := resolveFont(s.Font, theme.Font)
		d.SetFillColor(textColor).
			SetTextAlign(core.TextAlignStart).
			SetTextBaseline(core.BaselineMiddle).
			FillText(s.Label, x+4, cy, font)
	}

	tr := s.trackRect
	if tr.Size.Width <= 0 {
		return
	}
	tx := float32(tr.Origin.X)
	tw := float32(tr.Size.Width)
	barH := clampf(h*0.28, 4, 10)
	barY := cy - barH/2
	barR := barH / 2

	d.SetFillColor(trackColor).FillPath(rectPath(tx, barY, tw, barH, barR), core.NonZero)

	frac := s.Fraction()
	fillW := tw * frac
	if fillW > 0 {
		d.SetFillColor(fillColor).FillPath(rectPath(tx, barY, fillW, barH, min(barR, fillW/2)), core.NonZero)
	}

	knobR := clampf(h*0.36, 6, 12)
	knobX := tx + fillW
	knob := new(core.Path).Arc(knobX, cy, knobR, 0, 2*math.Pi, false)
	d.SetFillColor(textColor).FillPath(knob, core.NonZero)

	knobBorder := theme.Border
	if state.Focused && !state.Disabled {
		knobBorder = theme.FocusedBorder
	}
	if knobBorder != 0 && knobBorder != core.Transparent && theme.BorderWidth > 0 {
		d.SetStrokeColor(knobBorder).SetLineWidth(theme.BorderWidth).StrokePath(knob)
	}
}

func (p *ProgressBar) draw(d *core.Drawing, theme *Theme, _ WidgetState) {
	rect := p.bounds
	w, h := float32(rect.Size.Width), float32(rect.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	x, y := float32(rect.Origin.X), float32(rect.Origin.Y)
	radius := min(resolveRadius(0, theme.CornerRadius), h/2)

	if p.DrawBackground != nil {
		drawCustomBackground(d, p.DrawBackground, rect)
	} else {
		track := p.TrackColor
		if track == 0 {
			track = theme.Track
		}
		shape := rectPath(x, y, w, h, radius)
		d.SetFillColor(track).FillPath(shape, core.NonZero)
		if theme.Border != 0 && theme.Border != core.Transparent && theme.BorderWidth > 0 {
			d.SetStrokeColor(theme.Border).SetLineWidth(theme.BorderWidth).StrokePath(shape)
		}
	}

	frac := p.Fraction()
	fillW := w * frac
	if fillW > 0 {
		fill := p.FillColor
		if fill == 0 {
			fill = theme.Accent
		}
		d.SetFillColor(fill).FillPath(rectPath(x, y, fillW, h, min(radius, fillW/2)), core.NonZero)
	}

	if p.Label != "" {
		font := resolveFont(p.Font, theme.Font)
		d.SetFillColor(theme.Text).
			SetTextAlign(core.TextAlignCenter).
			SetTextBaseline(core.BaselineMiddle).
			FillText(p.Label, x+w/2, y+h/2, font)
	}
}

func (s *Spacer) draw(_ *core.Drawing, _ *Theme, _ WidgetState) {}
