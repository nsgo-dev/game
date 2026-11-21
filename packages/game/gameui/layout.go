package gameui

import (
	"unicode/utf8"

	"github.com/nsgo-dev/nsgo/core"
)

func approxTextSize(text string, font core.Font) (w, h float32) {
	size := float32(font.Size)
	if size <= 0 {
		size = float32(core.DefaultFontSize)
	}
	w = 0.6 * size * float32(utf8.RuneCountInString(text))
	h = 1.25 * size
	return w, h
}

func applySizeConstraints(prefW, prefH float32, sp layoutSpec) core.Size {
	w, h := prefW, prefH
	if sp.Width > 0 {
		w = sp.Width
	}
	if sp.Height > 0 {
		h = sp.Height
	}
	if sp.MinWidth > 0 && w < sp.MinWidth {
		w = sp.MinWidth
	}
	if sp.MinHeight > 0 && h < sp.MinHeight {
		h = sp.MinHeight
	}
	return core.Size{Width: core.Points(max(0, w)), Height: core.Points(max(0, h))}
}

func effectiveAlign(childAlign, containerAlign Align) Align {
	if childAlign != AlignDefault {
		return childAlign
	}
	if containerAlign != AlignDefault {
		return containerAlign
	}
	return AlignStretch
}

func zeroTreeBounds(w Widget) {
	if w == nil {
		return
	}
	w.setBounds(core.Bounds{})
	for _, c := range w.childWidgets() {
		zeroTreeBounds(c)
	}
}

// --- Measure implementations ---

func (p *Panel) measure(theme *Theme) core.Size {
	padX := float32(p.Padding.Left + p.Padding.Right)
	padY := float32(p.Padding.Top + p.Padding.Bottom)
	var cw, ch float32
	if p.Child != nil && !p.Child.spec().Hidden {
		cs := p.Child.measure(theme)
		cw, ch = float32(cs.Width), float32(cs.Height)
	}
	return applySizeConstraints(cw+padX, ch+padY, p.spec())
}

func (b *VBox) measure(theme *Theme) core.Size {
	padX := float32(b.Padding.Left + b.Padding.Right)
	padY := float32(b.Padding.Top + b.Padding.Bottom)
	var maxW, sumH float32
	visible := 0
	for _, c := range b.Children {
		if c == nil || c.spec().Hidden {
			continue
		}
		cs := c.measure(theme)
		if float32(cs.Width) > maxW {
			maxW = float32(cs.Width)
		}
		sumH += float32(cs.Height)
		visible++
	}
	if visible > 1 && b.Spacing > 0 {
		sumH += b.Spacing * float32(visible-1)
	}
	return applySizeConstraints(maxW+padX, sumH+padY, b.spec())
}

func (b *HBox) measure(theme *Theme) core.Size {
	padX := float32(b.Padding.Left + b.Padding.Right)
	padY := float32(b.Padding.Top + b.Padding.Bottom)
	var sumW, maxH float32
	visible := 0
	for _, c := range b.Children {
		if c == nil || c.spec().Hidden {
			continue
		}
		cs := c.measure(theme)
		sumW += float32(cs.Width)
		if float32(cs.Height) > maxH {
			maxH = float32(cs.Height)
		}
		visible++
	}
	if visible > 1 && b.Spacing > 0 {
		sumW += b.Spacing * float32(visible-1)
	}
	return applySizeConstraints(sumW+padX, maxH+padY, b.spec())
}

func (l *Label) measure(theme *Theme) core.Size {
	font := resolveFont(l.Font, theme.Font)
	tw, th := approxTextSize(l.Text, font)
	return applySizeConstraints(tw, th, l.spec())
}

func (b *Button) measure(theme *Theme) core.Size {
	font := resolveFont(b.Font, theme.Font)
	tw, th := approxTextSize(b.Text, font)
	pad := max(theme.Padding, 4)
	prefW := tw + 2*pad
	prefH := max(th+pad, 32)
	return applySizeConstraints(prefW, prefH, b.spec())
}

func (t *Toggle) measure(theme *Theme) core.Size {
	font := resolveFont(t.Font, theme.Font)
	tw, th := approxTextSize(t.Label, font)
	boxSize := max(float32(font.Size)*1.15, 18)
	indicatorW := boxSize
	if t.Style == ToggleSwitch {
		indicatorW = boxSize * 1.8
	}
	pad := max(theme.Padding*0.5, 4)
	prefW := indicatorW + 2*pad
	if t.Label != "" {
		prefW += max(theme.Padding*0.75, 8) + tw
	}
	prefH := max(max(boxSize, th)+pad*2, 28)
	return applySizeConstraints(prefW, prefH, t.spec())
}

func (s *Slider) measure(theme *Theme) core.Size {
	font := resolveFont(s.Font, theme.Font)
	prefW := float32(160)
	if s.Label != "" {
		lw, _ := approxTextSize(s.Label, font)
		prefW += lw + max(theme.Padding*0.75, 8)
	}
	prefH := max(float32(font.Size)+theme.Padding, 28)
	return applySizeConstraints(prefW, prefH, s.spec())
}

func (p *ProgressBar) measure(theme *Theme) core.Size {
	font := resolveFont(p.Font, theme.Font)
	prefH := float32(16)
	if p.Label != "" {
		_, th := approxTextSize(p.Label, font)
		prefH = max(th+6, 22)
	}
	return applySizeConstraints(160, prefH, p.spec())
}

func (s *Spacer) measure(_ *Theme) core.Size {
	return applySizeConstraints(0, 0, s.spec())
}

// --- Layout implementations ---

func (p *Panel) layout(bounds core.Bounds, theme *Theme) {
	p.bounds = bounds
	if p.Child == nil {
		return
	}
	sp := p.Child.spec()
	if sp.Hidden {
		zeroTreeBounds(p.Child)
		return
	}
	innerX := float32(bounds.Origin.X) + float32(p.Padding.Left)
	innerY := float32(bounds.Origin.Y) + float32(p.Padding.Top)
	innerW := max(0, float32(bounds.Size.Width)-float32(p.Padding.Left+p.Padding.Right))
	innerH := max(0, float32(bounds.Size.Height)-float32(p.Padding.Top+p.Padding.Bottom))

	align := effectiveAlign(sp.Align, p.Align)
	pref := p.Child.measure(theme)

	var cw, ch, cx, cy float32
	if align == AlignStretch {
		if sp.Width > 0 {
			cw = min(sp.Width, innerW)
		} else {
			cw = innerW
		}
		if sp.Height > 0 {
			ch = min(sp.Height, innerH)
		} else {
			ch = innerH
		}
		cx, cy = innerX, innerY
	} else {
		cw = min(float32(pref.Width), innerW)
		ch = min(float32(pref.Height), innerH)
		if sp.Flex > 0 {
			cw, ch = innerW, innerH
		}
		switch align {
		case AlignCenter:
			cx = innerX + (innerW-cw)/2
			cy = innerY + (innerH-ch)/2
		case AlignEnd:
			cx = innerX + innerW - cw
			cy = innerY + innerH - ch
		default: // AlignStart
			cx = innerX
			cy = innerY
		}
	}

	p.Child.layout(core.Bounds{
		Origin: core.Point{X: core.Points(cx), Y: core.Points(cy)},
		Size:   core.Size{Width: core.Points(cw), Height: core.Points(ch)},
	}, theme)
}

func (b *VBox) layout(bounds core.Bounds, theme *Theme) {
	b.bounds = bounds
	layoutLinear(true, bounds, b.Padding, b.Spacing, b.Align, b.Justify, b.Children, theme)
}

func (b *HBox) layout(bounds core.Bounds, theme *Theme) {
	b.bounds = bounds
	layoutLinear(false, bounds, b.Padding, b.Spacing, b.Align, b.Justify, b.Children, theme)
}

func layoutLinear(vertical bool, bounds core.Bounds, padding core.Edges, spacing float32, align Align, justify Justify, children []Widget, theme *Theme) {
	innerX := float32(bounds.Origin.X) + float32(padding.Left)
	innerY := float32(bounds.Origin.Y) + float32(padding.Top)
	innerW := max(0, float32(bounds.Size.Width)-float32(padding.Left+padding.Right))
	innerH := max(0, float32(bounds.Size.Height)-float32(padding.Top+padding.Bottom))

	visible := make([]Widget, 0, len(children))
	for _, c := range children {
		if c == nil {
			continue
		}
		if c.spec().Hidden {
			zeroTreeBounds(c)
			continue
		}
		visible = append(visible, c)
	}
	if len(visible) == 0 {
		return
	}

	if spacing < 0 {
		spacing = 0
	}
	totalSpacing := spacing * float32(len(visible)-1)

	var innerMain, innerCross, mainStart, crossStart float32
	if vertical {
		innerMain, innerCross = innerH, innerW
		mainStart, crossStart = innerY, innerX
	} else {
		innerMain, innerCross = innerW, innerH
		mainStart, crossStart = innerX, innerY
	}

	specs := make([]layoutSpec, len(visible))
	prefs := make([]core.Size, len(visible))
	mainSizes := make([]float32, len(visible))
	flexes := make([]float32, len(visible))
	flexMins := make([]float32, len(visible))

	var fixedSum, totalFlex float32
	for i, c := range visible {
		sp := c.spec()
		pr := c.measure(theme)
		specs[i] = sp
		prefs[i] = pr
		if sp.Flex > 0 {
			flexes[i] = sp.Flex
			totalFlex += sp.Flex
			if vertical {
				flexMins[i] = sp.MinHeight
			} else {
				flexMins[i] = sp.MinWidth
			}
		} else {
			if vertical {
				mainSizes[i] = float32(pr.Height)
			} else {
				mainSizes[i] = float32(pr.Width)
			}
			fixedSum += mainSizes[i]
		}
	}

	avail := max(0, innerMain-totalSpacing)
	var leftover float32
	if fixedSum > avail && fixedSum > 0 {
		// Shrink fixed children proportionally so they never overlap or overflow container bounds.
		scale := avail / fixedSum
		for i := range visible {
			if flexes[i] <= 0 {
				mainSizes[i] *= scale
			}
		}
	} else {
		rem := avail - fixedSum
		if totalFlex > 0 {
			flexSizes := distributeFlex(rem, flexes, flexMins)
			for i := range visible {
				if flexes[i] > 0 {
					mainSizes[i] = flexSizes[i]
				}
			}
		} else {
			leftover = rem
		}
	}

	cursor := mainStart
	gap := spacing
	if leftover > 0 {
		switch justify {
		case JustifyCenter:
			cursor += leftover / 2
		case JustifyEnd:
			cursor += leftover
		case JustifySpaceBetween:
			if len(visible) > 1 {
				gap = spacing + leftover/float32(len(visible)-1)
			} else {
				cursor += leftover / 2
			}
		}
	}

	for i, c := range visible {
		sp := specs[i]
		pr := prefs[i]
		a := effectiveAlign(sp.Align, align)

		var fixedCross, prefCross float32
		if vertical {
			fixedCross, prefCross = sp.Width, float32(pr.Width)
		} else {
			fixedCross, prefCross = sp.Height, float32(pr.Height)
		}

		var crossSize, crossPos float32
		if a == AlignStretch {
			if fixedCross > 0 {
				crossSize = min(fixedCross, innerCross)
			} else {
				crossSize = innerCross
			}
			crossPos = crossStart
		} else {
			crossSize = min(prefCross, innerCross)
			switch a {
			case AlignCenter:
				crossPos = crossStart + (innerCross-crossSize)/2
			case AlignEnd:
				crossPos = crossStart + innerCross - crossSize
			default: // AlignStart
				crossPos = crossStart
			}
		}

		var cb core.Bounds
		if vertical {
			cb = core.Bounds{
				Origin: core.Point{X: core.Points(crossPos), Y: core.Points(cursor)},
				Size:   core.Size{Width: core.Points(crossSize), Height: core.Points(mainSizes[i])},
			}
		} else {
			cb = core.Bounds{
				Origin: core.Point{X: core.Points(cursor), Y: core.Points(crossPos)},
				Size:   core.Size{Width: core.Points(mainSizes[i]), Height: core.Points(crossSize)},
			}
		}
		c.layout(cb, theme)
		cursor += mainSizes[i] + gap
	}
}

func distributeFlex(avail float32, flexes, mins []float32) []float32 {
	sizes := make([]float32, len(flexes))
	if avail <= 0 {
		return sizes
	}
	locked := make([]bool, len(flexes))
	rem := avail
	for {
		var sumFlex float32
		for i, f := range flexes {
			if f > 0 && !locked[i] {
				sumFlex += f
			}
		}
		if sumFlex <= 0 {
			break
		}
		clampedAny := false
		for i, f := range flexes {
			if f <= 0 || locked[i] {
				continue
			}
			share := rem * (f / sumFlex)
			if mins[i] > 0 && share < mins[i] {
				sizes[i] = min(mins[i], rem)
				rem -= sizes[i]
				if rem < 0 {
					rem = 0
				}
				locked[i] = true
				clampedAny = true
			}
		}
		if !clampedAny {
			for i, f := range flexes {
				if f > 0 && !locked[i] {
					sizes[i] = rem * (f / sumFlex)
				}
			}
			break
		}
	}
	return sizes
}

func (l *Label) layout(bounds core.Bounds, _ *Theme)       { l.bounds = bounds }
func (b *Button) layout(bounds core.Bounds, _ *Theme)      { b.bounds = bounds }
func (t *Toggle) layout(bounds core.Bounds, _ *Theme)      { t.bounds = bounds }
func (p *ProgressBar) layout(bounds core.Bounds, _ *Theme) { p.bounds = bounds }
func (s *Spacer) layout(bounds core.Bounds, _ *Theme)      { s.bounds = bounds }

func (s *Slider) layout(bounds core.Bounds, theme *Theme) {
	s.bounds = bounds
	x := float32(bounds.Origin.X)
	y := float32(bounds.Origin.Y)
	w := float32(bounds.Size.Width)
	h := float32(bounds.Size.Height)

	knobR := clampf(h*0.36, 6, 12)
	pad := max(knobR, 6)
	tx := x + pad
	tw := max(0, w-2*pad)

	if s.Label != "" {
		font := resolveFont(s.Font, theme.Font)
		lw, _ := approxTextSize(s.Label, font)
		gap := max(theme.Padding*0.75, 8)
		labelReserve := min(lw+gap, w*0.45)
		tx = x + labelReserve + pad*0.5
		tw = max(0, (x+w-pad)-tx)
	}

	s.trackRect = core.Bounds{
		Origin: core.Point{X: core.Points(tx), Y: core.Points(y)},
		Size:   core.Size{Width: core.Points(tw), Height: core.Points(h)},
	}
}
