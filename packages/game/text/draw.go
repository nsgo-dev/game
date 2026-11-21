package text

import (
	"strings"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// Align controls horizontal alignment of laid-out text lines.
type Align uint8

const (
	// AlignLeft aligns lines to the left edge (x). This is the zero value.
	AlignLeft Align = iota
	// AlignCenter centers each line horizontally.
	AlignCenter
	// AlignRight aligns lines to the right edge.
	AlignRight
)

// Options configures how a Font measures, lays out, and draws text.
//
// Its zero value draws left-aligned at scale 1 and full opacity with no wrapping.
type Options struct {
	// MaxWidth wraps lines to fit within MaxWidth points when > 0, and defines
	// the alignment box [x, x+MaxWidth] for AlignCenter and AlignRight.
	// When MaxWidth <= 0, lines do not wrap and AlignCenter/AlignRight align
	// relative to x directly (centered at x, or right edge at x).
	MaxWidth float32
	// Align is the horizontal alignment of each line.
	Align Align
	// Scale multiplies glyph positions and sizes; 0 means 1.
	// A negative scale measures and draws nothing.
	Scale float32
	// Alpha is the text opacity from 0 to 1; 0 means 1 (fully opaque).
	// A negative alpha draws nothing.
	Alpha float32
	// LineSpacing is extra vertical space added between lines, in points.
	LineSpacing float32
	// Z is passed to each sprite in the underlying sprite.Batch.
	Z int
}

// GlyphPlacement is one visible glyph positioned by Font.Layout.
type GlyphPlacement struct {
	// Rune is the character this placement came from.
	Rune rune
	// Glyph is the font's glyph metrics and atlas frame.
	Glyph Glyph
	// X and Y are the top-left position of the drawn glyph rectangle, in points.
	X, Y float32
	// W and H are the drawn width and height of the glyph rectangle, in points.
	W, H float32
	// Line is the zero-based line index containing this glyph.
	Line int
}

// Measure returns the bounding width and height of s laid out with opts.
func (f *Font) Measure(s string, opts Options) core.Size {
	if f == nil || s == "" || opts.Scale < 0 {
		return core.Size{}
	}
	scale := resolveScale(opts.Scale)
	lines := wrapLines(s, opts.MaxWidth, func(line string) float32 {
		return f.measureLine(line, scale)
	})
	if len(lines) == 0 {
		return core.Size{}
	}
	var maxW float32
	for _, line := range lines {
		if w := f.measureLine(line, scale); w > maxW {
			maxW = w
		}
	}
	height := float32(len(lines))*f.LineHeight*scale + float32(len(lines)-1)*opts.LineSpacing
	return core.Size{Width: core.Points(maxW), Height: core.Points(height)}
}

// Layout computes the screen placements of all visible glyphs in s starting at (x, y).
func (f *Font) Layout(s string, x, y float32, opts Options) []GlyphPlacement {
	if f == nil || s == "" || opts.Scale < 0 {
		return nil
	}
	scale := resolveScale(opts.Scale)
	lines := wrapLines(s, opts.MaxWidth, func(line string) float32 {
		return f.measureLine(line, scale)
	})
	if len(lines) == 0 {
		return nil
	}
	lineStep := f.LineHeight*scale + opts.LineSpacing
	var placements []GlyphPlacement
	for lineIdx, line := range lines {
		lineW := f.measureLine(line, scale)
		startX := alignedX(x, lineW, opts.MaxWidth, opts.Align)
		lineY := y + float32(lineIdx)*lineStep

		var penX float32
		var prev rune
		hasPrev := false
		for _, r := range line {
			g, ok := f.Glyphs[r]
			if !ok {
				hasPrev = false
				continue
			}
			if hasPrev {
				penX += f.Kerning(prev, r) * scale
			}
			if g.Frame.W > 0 && g.Frame.H > 0 {
				placements = append(placements, GlyphPlacement{
					Rune:  r,
					Glyph: g,
					X:     startX + penX + g.XOffset*scale,
					Y:     lineY + g.YOffset*scale,
					W:     g.Frame.W * scale,
					H:     g.Frame.H * scale,
					Line:  lineIdx,
				})
			}
			penX += g.XAdvance * scale
			prev = r
			hasPrev = true
		}
	}
	return placements
}

// Draw draws s at (x, y) into d through sprite.Batch (one batch per texture
// page used, preserving order; single-page fonts use one sprite.Batch).
func (f *Font) Draw(d *core.Drawing, s string, x, y float32, opts Options) {
	if f == nil || d == nil || s == "" || opts.Scale < 0 || opts.Alpha < 0 {
		return
	}
	scale := resolveScale(opts.Scale)
	alpha := resolveAlpha(opts.Alpha)
	placements := f.Layout(s, x, y, opts)
	if len(placements) == 0 {
		return
	}

	sprites := make([]sprite.Sprite, len(placements))
	firstPage := placements[0].Glyph.Page
	singlePage := true
	for i, p := range placements {
		sp := sprite.NewSprite(p.Glyph.Frame)
		sp.X = p.X
		sp.Y = p.Y
		sp.ScaleX = scale
		sp.ScaleY = scale
		sp.Alpha = alpha
		sp.Z = opts.Z
		sprites[i] = sp
		if p.Glyph.Page != firstPage {
			singlePage = false
		}
	}

	if singlePage {
		atlas := f.Pages[firstPage]
		if atlas == nil {
			return
		}
		ptrs := make([]*sprite.Sprite, len(sprites))
		for i := range sprites {
			ptrs[i] = &sprites[i]
		}
		batch := sprite.Batch{Atlas: atlas, Sprites: ptrs}
		batch.Draw(d)
		return
	}

	var pageOrder []int
	batches := make(map[int]*sprite.Batch, len(f.Pages))
	for i, p := range placements {
		page := p.Glyph.Page
		b, ok := batches[page]
		if !ok {
			atlas := f.Pages[page]
			if atlas == nil {
				continue
			}
			b = &sprite.Batch{Atlas: atlas}
			batches[page] = b
			pageOrder = append(pageOrder, page)
		}
		b.Sprites = append(b.Sprites, &sprites[i])
	}
	for _, page := range pageOrder {
		batches[page].Draw(d)
	}
}

func (f *Font) measureLine(line string, scale float32) float32 {
	var penX float32
	var prev rune
	hasPrev := false
	for _, r := range line {
		g, ok := f.Glyphs[r]
		if !ok {
			hasPrev = false
			continue
		}
		if hasPrev {
			penX += f.Kerning(prev, r) * scale
		}
		penX += g.XAdvance * scale
		prev = r
		hasPrev = true
	}
	return penX
}

func resolveScale(scale float32) float32 {
	if scale == 0 {
		return 1
	}
	return scale
}

func resolveAlpha(alpha float32) float32 {
	if alpha == 0 {
		return 1
	}
	if alpha > 1 {
		return 1
	}
	return alpha
}

func alignedX(x, lineWidth, maxWidth float32, align Align) float32 {
	switch align {
	case AlignCenter:
		if maxWidth > 0 {
			return x + (maxWidth-lineWidth)/2
		}
		return x - lineWidth/2
	case AlignRight:
		if maxWidth > 0 {
			return x + maxWidth - lineWidth
		}
		return x - lineWidth
	default:
		return x
	}
}

// wrapLines splits s on explicit newlines and, when maxWidth > 0, word-wraps
// each paragraph to fit within maxWidth, breaking long words by rune if needed.
func wrapLines(s string, maxWidth float32, measure func(string) float32) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	paragraphs := strings.Split(s, "\n")
	if maxWidth <= 0 {
		return paragraphs
	}

	var lines []string
	for _, para := range paragraphs {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		paraStart := len(lines)
		tokens := tokenizeParagraph(para)
		var cur, pendingSpace string
		for _, tok := range tokens {
			if tok.space {
				if cur != "" || len(lines) == paraStart {
					pendingSpace += tok.text
				}
				continue
			}
			if cur != "" {
				candidate := cur + pendingSpace + tok.text
				if measure(candidate) <= maxWidth {
					cur = candidate
					pendingSpace = ""
					continue
				}
				lines = append(lines, cur)
				cur = ""
				pendingSpace = ""
				cur = placeWord(&lines, tok.text, maxWidth, measure)
			} else {
				if pendingSpace != "" && measure(pendingSpace+tok.text) <= maxWidth {
					cur = pendingSpace + tok.text
					pendingSpace = ""
				} else {
					pendingSpace = ""
					cur = placeWord(&lines, tok.text, maxWidth, measure)
				}
			}
		}
		if cur != "" || len(lines) == paraStart {
			lines = append(lines, cur)
		}
	}
	return lines
}

type wrapToken struct {
	text  string
	space bool
}

func tokenizeParagraph(para string) []wrapToken {
	var tokens []wrapToken
	i := 0
	for i < len(para) {
		isSpace := para[i] == ' ' || para[i] == '\t'
		start := i
		for i < len(para) && ((para[i] == ' ' || para[i] == '\t') == isSpace) {
			i++
		}
		tokens = append(tokens, wrapToken{text: para[start:i], space: isSpace})
	}
	return tokens
}

// placeWord fits a single non-whitespace word onto lines, breaking by rune if
// the word itself exceeds maxWidth. It returns the trailing partial line.
func placeWord(lines *[]string, word string, maxWidth float32, measure func(string) float32) string {
	if measure(word) <= maxWidth {
		return word
	}
	var chunk []rune
	for _, r := range word {
		if len(chunk) == 0 {
			chunk = append(chunk, r)
			continue
		}
		next := append(chunk, r)
		if measure(string(next)) <= maxWidth {
			chunk = next
		} else {
			*lines = append(*lines, string(chunk))
			chunk = []rune{r}
		}
	}
	return string(chunk)
}
