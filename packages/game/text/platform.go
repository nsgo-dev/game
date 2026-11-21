package text

import "github.com/nsgo-dev/nsgo/core"

// PlatformStyle configures platform-rendered text drawn with DrawPlatform.
//
// Its zero value draws in core.DefaultFontSize at full opacity, left-aligned,
// on the alphabetic baseline, keeping the drawing's current fill color, with no
// wrapping.
type PlatformStyle struct {
	// Font selects the platform font family, size, weight, italic, and language.
	// When Font.Size <= 0, core.DefaultFontSize is used.
	Font core.Font
	// Color sets the text fill color when non-zero; when 0, the drawing's
	// current fill color is kept.
	Color core.Color
	// Alpha sets the text opacity from 0 to 1; 0 means 1 (keep full opacity).
	// A negative value draws nothing.
	Alpha float32
	// Align aligns each line horizontally (AlignLeft, AlignCenter, AlignRight).
	Align Align
	// Baseline sets which line of text sits at y (zero value is core.BaselineAlphabetic).
	Baseline core.TextBaseline
	// MaxWidth wraps lines to fit within MaxWidth points when > 0, and defines
	// the alignment box [x, x+MaxWidth] for AlignCenter and AlignRight.
	// When MaxWidth <= 0, lines do not wrap and AlignCenter/AlignRight align
	// relative to x directly.
	MaxWidth float32
	// LineHeight overrides the distance between consecutive lines in points when > 0.
	// When <= 0, it uses the font's measured line height from d.TextMetrics.
	LineHeight float32
	// LineSpacing adds extra vertical space between lines, in points.
	LineSpacing float32
}

// DrawPlatform draws text at (x, y) into d using core.Drawing.FillText.
//
// It draws inside the drawing's active transform stack so text scales
// automatically with a game view's virtual resolution transform, and wraps its
// style changes in Save/Restore so fill color, alpha, alignment, and baseline
// never leak to subsequent draw calls.
func DrawPlatform(d *core.Drawing, text string, x, y float32, style PlatformStyle) {
	if d == nil || text == "" || style.Alpha < 0 {
		return
	}
	font := resolvePlatformFont(style.Font)
	alpha := resolveAlpha(style.Alpha)
	lines := wrapLines(text, style.MaxWidth, func(line string) float32 {
		return d.MeasureText(line, font)
	})
	if len(lines) == 0 {
		return
	}

	var lineStep float32
	if len(lines) > 1 {
		lineStep = resolvePlatformLineHeight(d, font, style.LineHeight) + style.LineSpacing
	}

	var ta core.TextAlign
	drawX := x
	switch style.Align {
	case AlignCenter:
		ta = core.TextAlignCenter
		if style.MaxWidth > 0 {
			drawX = x + style.MaxWidth/2
		}
	case AlignRight:
		ta = core.TextAlignRight
		if style.MaxWidth > 0 {
			drawX = x + style.MaxWidth
		}
	default:
		ta = core.TextAlignLeft
	}

	d.Save()
	if style.Color != 0 {
		d.SetFillColor(style.Color)
	}
	if alpha < 1 {
		d.SetAlpha(alpha)
	}
	d.SetTextAlign(ta)
	d.SetTextBaseline(style.Baseline)
	for i, line := range lines {
		if line == "" {
			continue
		}
		d.FillText(line, drawX, y+float32(i)*lineStep, font)
	}
	d.Restore()
}

// MeasurePlatform returns the bounding width and height of text laid out with
// style on d.
func MeasurePlatform(d *core.Drawing, text string, style PlatformStyle) core.Size {
	if d == nil || text == "" {
		return core.Size{}
	}
	font := resolvePlatformFont(style.Font)
	lines := wrapLines(text, style.MaxWidth, func(line string) float32 {
		return d.MeasureText(line, font)
	})
	if len(lines) == 0 {
		return core.Size{}
	}
	var maxW float32
	for _, line := range lines {
		if w := d.MeasureText(line, font); w > maxW {
			maxW = w
		}
	}
	lineHeight := resolvePlatformLineHeight(d, font, style.LineHeight)
	height := float32(len(lines))*lineHeight + float32(len(lines)-1)*style.LineSpacing
	return core.Size{Width: core.Points(maxW), Height: core.Points(height)}
}

func resolvePlatformFont(font core.Font) core.Font {
	if font.Size <= 0 {
		font.Size = core.DefaultFontSize
	}
	return font
}

func resolvePlatformLineHeight(d *core.Drawing, font core.Font, override float32) float32 {
	if override > 0 {
		return override
	}
	m := d.TextMetrics("Mg", font)
	if m.LineHeight > 0 {
		return m.LineHeight
	}
	if sum := m.Ascent + m.Descent; sum > 0 {
		return sum
	}
	return float32(font.Size) * 1.2
}
