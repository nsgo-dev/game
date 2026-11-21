package text

import (
	"bytes"
	"image"
	imagedraw "image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	maxRichTagDepth   = 64
	defaultWaveAmp    = float32(3)
	defaultWaveFreq   = float32(4)
	defaultWaveSpeed  = float32(6)
	defaultShakeAmp   = float32(2)
	defaultShakeSpeed = float32(30)
	fauxItalicShear   = float32(0.22)
	maxTintCacheSize  = 256
)

// RichStyle holds the active visual attributes of a parsed rich text token.
type RichStyle struct {
	// Color is the token's foreground color; 0 means inherit the default color.
	Color core.Color
	// Scale multiplies the base text scale; 0 means 1.
	Scale float32
	// Bold requests bold rendering (BoldFont/BoldItalicFont, faux-bold, or
	// core.FontWeightBold on platform text).
	Bold bool
	// Italic requests italic rendering (ItalicFont/BoldItalicFont, faux-italic
	// skew, or core.Font.Italic on platform text).
	Italic bool
	// Effect configures a deterministic per-glyph animation effect.
	Effect GlyphEffect
}

func (s RichStyle) effectiveScale() float32 {
	if s.Scale <= 0 {
		return 1
	}
	return s.Scale
}

// RichTokenKind distinguishes text glyphs, inline icons, and typewriter pauses.
type RichTokenKind uint8

const (
	// TokenGlyph is a single Unicode rune with an active RichStyle.
	TokenGlyph RichTokenKind = iota
	// TokenIcon is an inline icon ([icon=name ...]) with an active RichStyle.
	TokenIcon
	// TokenPause is a zero-width typewriter pause ([pause=seconds]).
	TokenPause
)

// RichToken is one parsed unit in a RichText document.
type RichToken struct {
	Kind RichTokenKind
	// Rune is set when Kind == TokenGlyph.
	Rune rune
	// IconName is set when Kind == TokenIcon.
	IconName string
	// IconWidth and IconHeight override the resolved icon's intrinsic dimensions when > 0.
	IconWidth, IconHeight float32
	// PauseSeconds is the pause duration in seconds when Kind == TokenPause.
	PauseSeconds float32
	// Style holds the color, scale, bold, italic, and effect active on this token.
	Style RichStyle
}

// RichRun groups contiguous glyphs sharing identical style into a string, or
// represents a single inline icon or pause token.
type RichRun struct {
	Kind RichTokenKind
	// Text holds the run's runes when Kind == TokenGlyph.
	Text string
	// IconName, IconWidth, IconHeight are set when Kind == TokenIcon.
	IconName              string
	IconWidth, IconHeight float32
	// PauseSeconds is set when Kind == TokenPause.
	PauseSeconds float32
	// Style is the shared RichStyle for this run.
	Style RichStyle
}

// RichText is a parsed rich text document consisting of styled tokens.
type RichText struct {
	Tokens []RichToken
}

// Empty reports whether rt has no tokens.
func (rt RichText) Empty() bool {
	return len(rt.Tokens) == 0
}

// PlainText returns all text runes in rt with markup tags, icons, and pauses removed.
func (rt RichText) PlainText() string {
	var b strings.Builder
	for _, tok := range rt.Tokens {
		if tok.Kind == TokenGlyph {
			b.WriteRune(tok.Rune)
		}
	}
	return b.String()
}

// Runs groups contiguous glyph tokens with matching style into string runs,
// preserving inline icons and pauses in order.
func (rt RichText) Runs() []RichRun {
	if len(rt.Tokens) == 0 {
		return nil
	}
	var runs []RichRun
	var textBuf strings.Builder
	var curStyle RichStyle
	inText := false

	flushText := func() {
		if !inText {
			return
		}
		runs = append(runs, RichRun{
			Kind:  TokenGlyph,
			Text:  textBuf.String(),
			Style: curStyle,
		})
		textBuf.Reset()
		inText = false
	}

	for _, tok := range rt.Tokens {
		if tok.Kind == TokenGlyph {
			if inText && richStylesEqual(curStyle, tok.Style) {
				textBuf.WriteRune(tok.Rune)
			} else {
				flushText()
				curStyle = tok.Style
				textBuf.WriteRune(tok.Rune)
				inText = true
			}
			continue
		}
		flushText()
		runs = append(runs, RichRun{
			Kind:         tok.Kind,
			IconName:     tok.IconName,
			IconWidth:    tok.IconWidth,
			IconHeight:   tok.IconHeight,
			PauseSeconds: tok.PauseSeconds,
			Style:        tok.Style,
		})
	}
	flushText()
	return runs
}

func richStylesEqual(a, b RichStyle) bool {
	return a.Color == b.Color &&
		a.Scale == b.Scale &&
		a.Bold == b.Bold &&
		a.Italic == b.Italic &&
		a.Effect.Kind == b.Effect.Kind &&
		a.Effect.Seed == b.Effect.Seed &&
		a.Effect.Amplitude == b.Effect.Amplitude &&
		a.Effect.Frequency == b.Effect.Frequency &&
		a.Effect.Speed == b.Effect.Speed &&
		a.Effect.Custom == nil && b.Effect.Custom == nil
}

// Icon describes an inline image or sprite atlas frame embedded inside rich text.
type Icon struct {
	// Image is the texture containing the icon. When empty and Atlas != nil,
	// Atlas.Image is used.
	Image core.ImageSource
	// Atlas optionally identifies the sprite.Atlas holding Frame for sprite.Batch grouping.
	Atlas *sprite.Atlas
	// Frame is the sub-rectangle in Image/Atlas. When Frame.W == 0 && Frame.H == 0,
	// the entire Image is drawn.
	Frame sprite.Frame
	// Width and Height are the icon's drawn size in points at scale 1.
	// When 0, they default to Frame.Width/Frame.Height (or Frame.W/Frame.H, or Image size).
	Width, Height float32
	// Advance is the horizontal cursor advance in points at scale 1.
	// When <= 0, it defaults to Width.
	Advance float32
	// BaselineOffset shifts the icon vertically relative to the text baseline
	// (positive moves down, negative moves up). With 0, the bottom of the icon
	// sits on the text baseline.
	BaselineOffset float32
}

// IconResolver resolves an inline icon name (from [icon=name]) to an Icon.
type IconResolver func(name string) (Icon, bool)

// AtlasIconResolver returns an IconResolver that looks up named frames in atlas.
func AtlasIconResolver(atlas *sprite.Atlas) IconResolver {
	return func(name string) (Icon, bool) {
		if atlas == nil {
			return Icon{}, false
		}
		frame, ok := atlas.Frames[name]
		if !ok {
			return Icon{}, false
		}
		w, h := frame.Width, frame.Height
		if w <= 0 {
			w = frame.W
		}
		if h <= 0 {
			h = frame.H
		}
		return Icon{
			Image:  atlas.Image,
			Atlas:  atlas,
			Frame:  frame,
			Width:  w,
			Height: h,
		}, true
	}
}

// RichOptions configures layout and rendering of RichText.
//
// Its zero value draws all tokens left-aligned at scale 1 and full opacity
// with no wrapping.
type RichOptions struct {
	// Options holds MaxWidth, Align, Scale, Alpha, LineSpacing, and Z.
	Options
	// Color is the default foreground color when a token has no [color=...] tag.
	// Zero means untinted for bitmap fonts, or the drawing's current fill color
	// for platform text.
	Color core.Color
	// Icons resolves [icon=name] tags. When nil or unresolved, icons are skipped.
	Icons IconResolver
	// BoldFont, ItalicFont, and BoldItalicFont are optional alternate bitmap fonts
	// used for [b], [i], and [b][i] spans. When nil, faux-bold or faux-italic
	// rendering is applied using the base font.
	BoldFont       *Font
	ItalicFont     *Font
	BoldItalicFont *Font
	// Elapsed is the animation time in seconds used when evaluating per-glyph
	// effects ([wave], [shake], or EffectCustom).
	Elapsed float32
	// Platform optionally supplies platform text rendering settings when drawing
	// via DrawRich with a nil bitmap Font.
	Platform *PlatformStyle
}

// RichPlacement is one laid-out visible glyph or inline icon in a RichLayout.
// Positions are relative to the layout's origin (0, 0).
type RichPlacement struct {
	// Index is the zero-based revealable unit index used by Typewriter and
	// EvalGlyphTransform.
	Index int
	// Kind is TokenGlyph or TokenIcon.
	Kind RichTokenKind
	// Rune is the character when Kind == TokenGlyph.
	Rune rune
	// Glyph is the bitmap glyph metrics when laid out with a bitmap Font.
	Glyph Glyph
	// Font is the resolved bitmap font for this glyph (base or Bold/Italic variant).
	Font *Font
	// Icon is the resolved inline icon when Kind == TokenIcon.
	Icon Icon
	// Style is the active RichStyle on this placement.
	Style RichStyle
	// Scale is the combined scale (baseScale * tokenScale).
	Scale float32
	// X and Y are the top-left position of the drawn glyph/icon rectangle, in points.
	X, Y float32
	// W and H are the drawn width and height of the glyph/icon rectangle, in points.
	W, H float32
	// BaselineY is the Y coordinate of this placement's line baseline, in points.
	BaselineY float32
	// Line is the zero-based line index containing this placement.
	Line int
	// FauxBold is true when Bold is active without a dedicated BoldFont/BoldItalicFont.
	FauxBold bool
	// FauxItalic is true when Italic is active without a dedicated ItalicFont/BoldItalicFont.
	FauxItalic bool
	// PlatformFont is set when laid out via LayoutRichPlatform.
	PlatformFont core.Font
	// isPlatform marks placements laid out for platform text rendering.
	isPlatform bool
}

// RichLine summarizes one wrapped line of a RichLayout.
type RichLine struct {
	// Index is the zero-based line index.
	Index int
	// X is the aligned starting X offset of the line relative to origin.
	X float32
	// Y is the top Y coordinate of the line relative to origin.
	Y float32
	// Width is the measured advance width of the line in points.
	Width float32
	// Height is the total line height in points.
	Height float32
	// Baseline is the distance from Y down to the line's baseline in points.
	Baseline float32
}

// RichLayout is the laid-out geometry of a RichText.
type RichLayout struct {
	// Size is the overall bounding width and height of the laid-out rich text.
	Size core.Size
	// Lines holds per-line metrics and alignment offsets.
	Lines []RichLine
	// Placements holds every visible glyph and inline icon in reading order.
	Placements []RichPlacement
	// TotalUnits is the total number of revealable units (non-newline glyphs + icons).
	TotalUnits int
	// isPlatform records whether this layout was produced by LayoutRichPlatform.
	isPlatform bool
	// platformBaselineYShift records the Y adjustment for platform text baseline mode.
	platformColor core.Color
	platformAlpha float32
}

// --- Markup Parser ---

type richTagKind uint8

const (
	richTagNone richTagKind = iota
	richTagColor
	richTagScale
	richTagSize
	richTagBold
	richTagItalic
	richTagWave
	richTagShake
	richTagIcon
	richTagPause
)

type richParseNodeKind uint8

const (
	parseNodeText richParseNodeKind = iota
	parseNodeIcon
	parseNodePause
	parseNodeTag
)

type richParseNode struct {
	kind     richParseNodeKind
	rawOpen  string
	text     string
	tagKind  richTagKind
	color    core.Color
	scale    float32
	effect   GlyphEffect
	iconName string
	iconW    float32
	iconH    float32
	pauseSec float32
	children []richParseNode
	closed   bool
}

// ParseRich parses forgiving inline rich text markup into a RichText.
//
// Supported tags:
//   - [color=#RRGGBB] / [color=#RRGGBBAA] / [color=gold] ... [/color]
//   - [scale=1.25] ... [/scale], [size=1.25] ... [/size]
//   - [b] ... [/b], [i] ... [/i]
//   - [icon=coin] or [icon=coin w=16 h=16]
//   - [wave amp=3 freq=4 speed=6] ... [/wave]
//   - [shake amp=2 speed=30] ... [/shake]
//   - [pause=0.2]
//   - [[ escapes a literal '['
//
// Any invalid, unknown, malformed, mismatched, or unclosed markup never errors
// or panics and falls back cleanly to plain text.
func ParseRich(src string) RichText {
	if src == "" {
		return RichText{}
	}

	root := &richParseNode{kind: parseNodeTag, closed: true}
	stack := []*richParseNode{root}

	appendText := func(s string) {
		if s == "" {
			return
		}
		top := stack[len(stack)-1]
		top.children = append(top.children, richParseNode{kind: parseNodeText, text: s})
	}

	i := 0
	for i < len(src) {
		if src[i] != '[' {
			next := strings.IndexByte(src[i:], '[')
			if next < 0 {
				appendText(src[i:])
				break
			}
			appendText(src[i : i+next])
			i += next
			continue
		}

		// Escaped literal bracket: "[[" -> "["
		if i+1 < len(src) && src[i+1] == '[' {
			appendText("[")
			i += 2
			continue
		}

		// Find matching ']' before any newline or nested '['.
		closeIdx := -1
		for j := i + 1; j < len(src); j++ {
			ch := src[j]
			if ch == ']' {
				closeIdx = j
				break
			}
			if ch == '[' || ch == '\n' || ch == '\r' {
				break
			}
		}
		if closeIdx < 0 {
			appendText("[")
			i++
			continue
		}

		rawTag := src[i : closeIdx+1]
		inner := strings.TrimSpace(src[i+1 : closeIdx])
		if inner == "" {
			appendText(rawTag)
			i = closeIdx + 1
			continue
		}

		if inner[0] == '/' {
			closeKind, ok := parseClosingTag(strings.TrimSpace(inner[1:]))
			if ok && len(stack) > 1 && stack[len(stack)-1].tagKind == closeKind {
				done := stack[len(stack)-1]
				done.closed = true
				stack = stack[:len(stack)-1]
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, *done)
				i = closeIdx + 1
				continue
			}
			appendText(rawTag)
			i = closeIdx + 1
			continue
		}

		node, selfClosing, ok := parseOpeningTag(inner, rawTag)
		if !ok {
			appendText(rawTag)
			i = closeIdx + 1
			continue
		}
		if selfClosing {
			top := stack[len(stack)-1]
			top.children = append(top.children, node)
			i = closeIdx + 1
			continue
		}
		if len(stack)-1 >= maxRichTagDepth {
			appendText(rawTag)
			i = closeIdx + 1
			continue
		}
		nodeCopy := node
		stack = append(stack, &nodeCopy)
		i = closeIdx + 1
	}

	// Unwind any unclosed tags; they remain closed=false and fall back to plain text.
	for len(stack) > 1 {
		unclosed := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		parent := stack[len(stack)-1]
		parent.children = append(parent.children, *unclosed)
	}

	var tokens []RichToken
	emitRichNodes(root.children, RichStyle{Scale: 1}, &tokens)
	return RichText{Tokens: tokens}
}

func emitRichNodes(nodes []richParseNode, style RichStyle, out *[]RichToken) {
	for _, n := range nodes {
		switch n.kind {
		case parseNodeText:
			for _, r := range n.text {
				*out = append(*out, RichToken{
					Kind:  TokenGlyph,
					Rune:  r,
					Style: style,
				})
			}
		case parseNodeIcon:
			*out = append(*out, RichToken{
				Kind:       TokenIcon,
				IconName:   n.iconName,
				IconWidth:  n.iconW,
				IconHeight: n.iconH,
				Style:      style,
			})
		case parseNodePause:
			*out = append(*out, RichToken{
				Kind:         TokenPause,
				PauseSeconds: n.pauseSec,
				Style:        style,
			})
		case parseNodeTag:
			if !n.closed {
				for _, r := range n.rawOpen {
					*out = append(*out, RichToken{
						Kind:  TokenGlyph,
						Rune:  r,
						Style: style,
					})
				}
				emitRichNodes(n.children, style, out)
				continue
			}
			childStyle := style
			switch n.tagKind {
			case richTagColor:
				childStyle.Color = n.color
			case richTagScale, richTagSize:
				childStyle.Scale = style.effectiveScale() * n.scale
			case richTagBold:
				childStyle.Bold = true
			case richTagItalic:
				childStyle.Italic = true
			case richTagWave, richTagShake:
				childStyle.Effect = n.effect
			}
			emitRichNodes(n.children, childStyle, out)
		}
	}
}

func parseClosingTag(name string) (richTagKind, bool) {
	switch strings.ToLower(name) {
	case "color":
		return richTagColor, true
	case "scale":
		return richTagScale, true
	case "size":
		return richTagSize, true
	case "b":
		return richTagBold, true
	case "i":
		return richTagItalic, true
	case "wave":
		return richTagWave, true
	case "shake":
		return richTagShake, true
	default:
		return richTagNone, false
	}
}

func parseOpeningTag(inner, rawTag string) (node richParseNode, selfClosing bool, ok bool) {
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return richParseNode{}, false, false
	}

	head := fields[0]
	rest := fields[1:]
	tagName, primaryVal, hasPrimary := strings.Cut(head, "=")
	tagName = strings.ToLower(tagName)

	switch tagName {
	case "b":
		if hasPrimary || len(rest) > 0 {
			return richParseNode{}, false, false
		}
		return richParseNode{kind: parseNodeTag, rawOpen: rawTag, tagKind: richTagBold}, false, true

	case "i":
		if hasPrimary || len(rest) > 0 {
			return richParseNode{}, false, false
		}
		return richParseNode{kind: parseNodeTag, rawOpen: rawTag, tagKind: richTagItalic}, false, true

	case "color":
		if !hasPrimary || primaryVal == "" || len(rest) > 0 {
			return richParseNode{}, false, false
		}
		col, valid := parseRichColor(primaryVal)
		if !valid {
			return richParseNode{}, false, false
		}
		return richParseNode{kind: parseNodeTag, rawOpen: rawTag, tagKind: richTagColor, color: col}, false, true

	case "scale", "size":
		if !hasPrimary || primaryVal == "" || len(rest) > 0 {
			return richParseNode{}, false, false
		}
		v, valid := parsePositiveFloat32(primaryVal)
		if !valid {
			return richParseNode{}, false, false
		}
		kind := richTagScale
		if tagName == "size" {
			kind = richTagSize
		}
		return richParseNode{kind: parseNodeTag, rawOpen: rawTag, tagKind: kind, scale: v}, false, true

	case "icon":
		if !hasPrimary || primaryVal == "" {
			return richParseNode{}, false, false
		}
		var w, h float32
		for _, attr := range rest {
			k, v, eq := strings.Cut(attr, "=")
			if !eq || v == "" {
				return richParseNode{}, false, false
			}
			switch strings.ToLower(k) {
			case "w", "width":
				fv, valid := parsePositiveFloat32(v)
				if !valid {
					return richParseNode{}, false, false
				}
				w = fv
			case "h", "height":
				fv, valid := parsePositiveFloat32(v)
				if !valid {
					return richParseNode{}, false, false
				}
				h = fv
			default:
				return richParseNode{}, false, false
			}
		}
		return richParseNode{
			kind:     parseNodeIcon,
			rawOpen:  rawTag,
			tagKind:  richTagIcon,
			iconName: primaryVal,
			iconW:    w,
			iconH:    h,
		}, true, true

	case "pause":
		if !hasPrimary || primaryVal == "" || len(rest) > 0 {
			return richParseNode{}, false, false
		}
		v, valid := parseNonNegativeFloat32(primaryVal)
		if !valid {
			return richParseNode{}, false, false
		}
		return richParseNode{
			kind:     parseNodePause,
			rawOpen:  rawTag,
			tagKind:  richTagPause,
			pauseSec: v,
		}, true, true

	case "wave":
		if hasPrimary {
			return richParseNode{}, false, false
		}
		eff := GlyphEffect{
			Kind:      EffectWave,
			Amplitude: defaultWaveAmp,
			Frequency: defaultWaveFreq,
			Speed:     defaultWaveSpeed,
		}
		for _, attr := range rest {
			k, v, eq := strings.Cut(attr, "=")
			if !eq || v == "" {
				return richParseNode{}, false, false
			}
			switch strings.ToLower(k) {
			case "amp", "amplitude":
				fv, valid := parseFiniteFloat32(v)
				if !valid {
					return richParseNode{}, false, false
				}
				eff.Amplitude = fv
			case "freq", "frequency":
				fv, valid := parseFiniteFloat32(v)
				if !valid {
					return richParseNode{}, false, false
				}
				eff.Frequency = fv
			case "speed":
				fv, valid := parseFiniteFloat32(v)
				if !valid {
					return richParseNode{}, false, false
				}
				eff.Speed = fv
			case "seed":
				sv, err := strconv.ParseUint(v, 10, 64)
				if err != nil {
					return richParseNode{}, false, false
				}
				eff.Seed = sv
			default:
				return richParseNode{}, false, false
			}
		}
		return richParseNode{kind: parseNodeTag, rawOpen: rawTag, tagKind: richTagWave, effect: eff}, false, true

	case "shake":
		if hasPrimary {
			return richParseNode{}, false, false
		}
		eff := GlyphEffect{
			Kind:      EffectShake,
			Amplitude: defaultShakeAmp,
			Speed:     defaultShakeSpeed,
		}
		for _, attr := range rest {
			k, v, eq := strings.Cut(attr, "=")
			if !eq || v == "" {
				return richParseNode{}, false, false
			}
			switch strings.ToLower(k) {
			case "amp", "amplitude":
				fv, valid := parseFiniteFloat32(v)
				if !valid {
					return richParseNode{}, false, false
				}
				eff.Amplitude = fv
			case "speed":
				fv, valid := parseFiniteFloat32(v)
				if !valid {
					return richParseNode{}, false, false
				}
				eff.Speed = fv
			case "freq", "frequency":
				fv, valid := parseFiniteFloat32(v)
				if !valid {
					return richParseNode{}, false, false
				}
				eff.Frequency = fv
			case "seed":
				sv, err := strconv.ParseUint(v, 10, 64)
				if err != nil {
					return richParseNode{}, false, false
				}
				eff.Seed = sv
			default:
				return richParseNode{}, false, false
			}
		}
		return richParseNode{kind: parseNodeTag, rawOpen: rawTag, tagKind: richTagShake, effect: eff}, false, true

	default:
		return richParseNode{}, false, false
	}
}

func parseFiniteFloat32(s string) (float32, bool) {
	v, err := strconv.ParseFloat(s, 32)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return float32(v), true
}

func parsePositiveFloat32(s string) (float32, bool) {
	v, ok := parseFiniteFloat32(s)
	if !ok || v <= 0 {
		return 0, false
	}
	return v, true
}

func parseNonNegativeFloat32(s string) (float32, bool) {
	v, ok := parseFiniteFloat32(s)
	if !ok || v < 0 {
		return 0, false
	}
	return v, true
}

var namedRichColors = map[string]core.Color{
	"black":   core.RGB(0x000000),
	"white":   core.RGB(0xffffff),
	"red":     core.RGB(0xff0000),
	"green":   core.RGB(0x008000),
	"lime":    core.RGB(0x00ff00),
	"blue":    core.RGB(0x0000ff),
	"yellow":  core.RGB(0xffff00),
	"gold":    core.RGB(0xffd700),
	"orange":  core.RGB(0xffa500),
	"cyan":    core.RGB(0x00ffff),
	"aqua":    core.RGB(0x00ffff),
	"magenta": core.RGB(0xff00ff),
	"purple":  core.RGB(0x800080),
	"pink":    core.RGB(0xffc0cb),
	"gray":    core.RGB(0x808080),
	"grey":    core.RGB(0x808080),
	"silver":  core.RGB(0xc0c0c0),
	"brown":   core.RGB(0xa52a2a),
}

func parseRichColor(s string) (core.Color, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if c, err := core.ParseColor(s); err == nil && c != 0 {
		return c, true
	}
	if c, ok := namedRichColors[strings.ToLower(s)]; ok {
		return c, true
	}
	if strings.HasPrefix(s, "#") {
		return parseHexColor(s[1:])
	}
	return 0, false
}

func parseHexColor(hex string) (core.Color, bool) {
	switch len(hex) {
	case 3:
		r, ok1 := parseHexNibble(hex[0])
		g, ok2 := parseHexNibble(hex[1])
		b, ok3 := parseHexNibble(hex[2])
		if !ok1 || !ok2 || !ok3 {
			return 0, false
		}
		return packColor(r*17, g*17, b*17, 255), true
	case 4:
		r, ok1 := parseHexNibble(hex[0])
		g, ok2 := parseHexNibble(hex[1])
		b, ok3 := parseHexNibble(hex[2])
		a, ok4 := parseHexNibble(hex[3])
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return 0, false
		}
		return packColor(r*17, g*17, b*17, a*17), true
	case 6:
		v, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return 0, false
		}
		return core.RGB(uint32(v)), true
	case 8:
		v, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return 0, false
		}
		c := core.Color(v)
		if c == 0 {
			return core.Transparent, true
		}
		return c, true
	default:
		return 0, false
	}
}

func parseHexNibble(ch byte) (uint32, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return uint32(ch - '0'), true
	case ch >= 'a' && ch <= 'f':
		return uint32(ch-'a') + 10, true
	case ch >= 'A' && ch <= 'F':
		return uint32(ch-'A') + 10, true
	default:
		return 0, false
	}
}

func packColor(r, g, b, a uint32) core.Color {
	c := core.Color((r << 24) | (g << 16) | (b << 8) | a)
	if c == 0 {
		return core.Transparent
	}
	return c
}

// --- Rich Layout & Wrapping ---

type indexedRichToken struct {
	token RichToken
	index int // revealable unit index (-1 for newlines)
}

func indexRichTokens(rt RichText) (items []indexedRichToken, totalUnits int) {
	items = make([]indexedRichToken, 0, len(rt.Tokens))
	for i := 0; i < len(rt.Tokens); i++ {
		tok := rt.Tokens[i]
		if tok.Kind == TokenPause {
			continue
		}
		if tok.Kind == TokenGlyph {
			if tok.Rune == '\r' {
				if i+1 < len(rt.Tokens) && rt.Tokens[i+1].Kind == TokenGlyph && rt.Tokens[i+1].Rune == '\n' {
					continue
				}
				tok.Rune = '\n'
			}
			if tok.Rune == '\n' {
				items = append(items, indexedRichToken{token: tok, index: -1})
				continue
			}
		}
		items = append(items, indexedRichToken{token: tok, index: totalUnits})
		totalUnits++
	}
	return items, totalUnits
}

type richWrapSegment struct {
	items []indexedRichToken
	space bool
}

func splitRichParagraphs(items []indexedRichToken) [][]indexedRichToken {
	if len(items) == 0 {
		return nil
	}
	var paras [][]indexedRichToken
	start := 0
	for i, it := range items {
		if it.token.Kind == TokenGlyph && it.token.Rune == '\n' {
			paras = append(paras, items[start:i])
			start = i + 1
		}
	}
	paras = append(paras, items[start:])
	return paras
}

func tokenizeRichParagraph(para []indexedRichToken) []richWrapSegment {
	var segs []richWrapSegment
	i := 0
	for i < len(para) {
		isSpace := isRichSpace(para[i].token)
		start := i
		for i < len(para) && isRichSpace(para[i].token) == isSpace {
			i++
		}
		segs = append(segs, richWrapSegment{items: para[start:i], space: isSpace})
	}
	return segs
}

func isRichSpace(tok RichToken) bool {
	return tok.Kind == TokenGlyph && (tok.Rune == ' ' || tok.Rune == '\t')
}

func wrapRichTokens(items []indexedRichToken, maxWidth float32, measure func([]indexedRichToken) float32) [][]indexedRichToken {
	if len(items) == 0 {
		return nil
	}
	paragraphs := splitRichParagraphs(items)
	if maxWidth <= 0 {
		return paragraphs
	}

	var lines [][]indexedRichToken
	for _, para := range paragraphs {
		if len(para) == 0 {
			lines = append(lines, nil)
			continue
		}
		paraStart := len(lines)
		segs := tokenizeRichParagraph(para)
		var cur, pendingSpace []indexedRichToken
		for _, seg := range segs {
			if seg.space {
				if len(cur) > 0 || len(lines) == paraStart {
					pendingSpace = append(pendingSpace, seg.items...)
				}
				continue
			}
			if len(cur) > 0 {
				candidate := make([]indexedRichToken, 0, len(cur)+len(pendingSpace)+len(seg.items))
				candidate = append(candidate, cur...)
				candidate = append(candidate, pendingSpace...)
				candidate = append(candidate, seg.items...)
				if measure(candidate) <= maxWidth {
					cur = candidate
					pendingSpace = nil
					continue
				}
				lines = append(lines, cur)
				cur = nil
				pendingSpace = nil
				cur = placeRichWord(&lines, seg.items, maxWidth, measure)
			} else {
				if len(pendingSpace) > 0 {
					candidate := make([]indexedRichToken, 0, len(pendingSpace)+len(seg.items))
					candidate = append(candidate, pendingSpace...)
					candidate = append(candidate, seg.items...)
					if measure(candidate) <= maxWidth {
						cur = candidate
						pendingSpace = nil
						continue
					}
				}
				pendingSpace = nil
				cur = placeRichWord(&lines, seg.items, maxWidth, measure)
			}
		}
		if len(cur) > 0 || len(lines) == paraStart {
			lines = append(lines, cur)
		}
	}
	return lines
}

func placeRichWord(lines *[][]indexedRichToken, word []indexedRichToken, maxWidth float32, measure func([]indexedRichToken) float32) []indexedRichToken {
	if measure(word) <= maxWidth {
		return append([]indexedRichToken(nil), word...)
	}
	var chunk []indexedRichToken
	for _, it := range word {
		if len(chunk) == 0 {
			chunk = append(chunk, it)
			continue
		}
		next := append(chunk, it)
		if measure(next) <= maxWidth {
			chunk = next
		} else {
			*lines = append(*lines, chunk)
			chunk = []indexedRichToken{it}
		}
	}
	return chunk
}

func resolveIconMetrics(icon Icon, tok RichToken, scale float32) (w, h, advance, baseOff float32) {
	iw, ih := icon.Width, icon.Height
	if iw <= 0 {
		if icon.Frame.Width > 0 {
			iw = icon.Frame.Width
		} else if icon.Frame.W > 0 {
			iw = icon.Frame.W
		} else if icon.Image.Size.Width > 0 {
			iw = float32(icon.Image.Size.Width)
		} else if icon.Image.Pixels != nil {
			iw = float32(icon.Image.Pixels.Bounds().Dx())
		}
	}
	if ih <= 0 {
		if icon.Frame.Height > 0 {
			ih = icon.Frame.Height
		} else if icon.Frame.H > 0 {
			ih = icon.Frame.H
		} else if icon.Image.Size.Height > 0 {
			ih = float32(icon.Image.Size.Height)
		} else if icon.Image.Pixels != nil {
			ih = float32(icon.Image.Pixels.Bounds().Dy())
		}
	}

	origW, origH := iw, ih
	if tok.IconWidth > 0 && tok.IconHeight > 0 {
		iw, ih = tok.IconWidth, tok.IconHeight
	} else if tok.IconWidth > 0 {
		if origW > 0 && origH > 0 {
			ih = origH * (tok.IconWidth / origW)
		} else {
			ih = tok.IconWidth
		}
		iw = tok.IconWidth
	} else if tok.IconHeight > 0 {
		if origW > 0 && origH > 0 {
			iw = origW * (tok.IconHeight / origH)
		} else {
			iw = tok.IconHeight
		}
		ih = tok.IconHeight
	}

	adv := iw
	if icon.Advance > 0 && tok.IconWidth == 0 {
		adv = icon.Advance
	}
	return iw * scale, ih * scale, adv * scale, icon.BaselineOffset * scale
}

func resolveBitmapStyleFont(base *Font, style RichStyle, opts RichOptions) (active *Font, fauxBold, fauxItalic bool) {
	if style.Bold && style.Italic {
		if opts.BoldItalicFont != nil {
			return opts.BoldItalicFont, false, false
		}
		if opts.BoldFont != nil {
			return opts.BoldFont, false, true
		}
		if opts.ItalicFont != nil {
			return opts.ItalicFont, true, false
		}
		return base, true, true
	}
	if style.Bold {
		if opts.BoldFont != nil {
			return opts.BoldFont, false, false
		}
		return base, true, false
	}
	if style.Italic {
		if opts.ItalicFont != nil {
			return opts.ItalicFont, false, false
		}
		return base, false, true
	}
	return base, false, false
}

func fontBase(f *Font) float32 {
	if f.Base > 0 {
		return f.Base
	}
	return f.LineHeight
}

// MeasureRich returns the bounding width and height of rt laid out with font and opts.
func MeasureRich(font *Font, rt RichText, opts RichOptions) core.Size {
	return LayoutRich(font, rt, opts).Size
}

// LayoutRich computes line wrapping, alignment, and per-glyph/per-icon placements
// for rt using bitmap font and opts.
//
// Inline icons are measured and wrapped identically to glyphs using their
// resolved advance width, and aligned to each line's baseline.
func LayoutRich(font *Font, rt RichText, opts RichOptions) RichLayout {
	if font == nil || len(rt.Tokens) == 0 || opts.Scale < 0 {
		return RichLayout{}
	}
	items, totalUnits := indexRichTokens(rt)
	if len(items) == 0 {
		return RichLayout{TotalUnits: totalUnits}
	}

	baseScale := resolveScale(opts.Scale)
	measureSeq := func(seq []indexedRichToken) float32 {
		var penX float32
		var prevRune rune
		var prevFont *Font
		var prevScale float32
		hasPrev := false
		for _, it := range seq {
			tok := it.token
			itemScale := baseScale * tok.Style.effectiveScale()
			switch tok.Kind {
			case TokenGlyph:
				af, _, _ := resolveBitmapStyleFont(font, tok.Style, opts)
				g, ok := af.Glyphs[tok.Rune]
				if !ok {
					hasPrev = false
					continue
				}
				if hasPrev && af == prevFont && itemScale == prevScale {
					penX += af.Kerning(prevRune, tok.Rune) * itemScale
				}
				penX += g.XAdvance * itemScale
				prevRune = tok.Rune
				prevFont = af
				prevScale = itemScale
				hasPrev = true
			case TokenIcon:
				hasPrev = false
				if opts.Icons == nil {
					continue
				}
				icon, ok := opts.Icons(tok.IconName)
				if !ok {
					continue
				}
				_, _, adv, _ := resolveIconMetrics(icon, tok, itemScale)
				penX += adv
			}
		}
		return penX
	}

	wrapped := wrapRichTokens(items, opts.MaxWidth, measureSeq)
	if len(wrapped) == 0 {
		return RichLayout{TotalUnits: totalUnits}
	}

	baseLineH := font.LineHeight * baseScale
	baseAscent := fontBase(font) * baseScale
	baseDescent := baseLineH - baseAscent
	if baseDescent < 0 {
		baseDescent = 0
	}

	layout := RichLayout{
		Lines:      make([]RichLine, 0, len(wrapped)),
		TotalUnits: totalUnits,
	}

	var curY float32
	var maxLineW float32

	for lineIdx, line := range wrapped {
		lineW := measureSeq(line)
		if lineW > maxLineW {
			maxLineW = lineW
		}
		startX := alignedX(0, lineW, opts.MaxWidth, opts.Align)

		// Compute line ascent and descent accommodating scaled glyphs and tall icons.
		ascent := baseAscent
		descent := baseDescent
		for _, it := range line {
			tok := it.token
			itemScale := baseScale * tok.Style.effectiveScale()
			switch tok.Kind {
			case TokenGlyph:
				af, _, _ := resolveBitmapStyleFont(font, tok.Style, opts)
				gAsc := fontBase(af) * itemScale
				gDesc := af.LineHeight*itemScale - gAsc
				if gDesc < 0 {
					gDesc = 0
				}
				if gAsc > ascent {
					ascent = gAsc
				}
				if gDesc > descent {
					descent = gDesc
				}
			case TokenIcon:
				if opts.Icons == nil {
					continue
				}
				icon, ok := opts.Icons(tok.IconName)
				if !ok {
					continue
				}
				_, ih, _, bOff := resolveIconMetrics(icon, tok, itemScale)
				iAsc := ih - bOff
				if iAsc > ascent {
					ascent = iAsc
				}
				if bOff > descent {
					descent = bOff
				}
			}
		}
		lineH := ascent + descent
		baselineY := curY + ascent

		layout.Lines = append(layout.Lines, RichLine{
			Index:    lineIdx,
			X:        startX,
			Y:        curY,
			Width:    lineW,
			Height:   lineH,
			Baseline: ascent,
		})

		var penX float32
		var prevRune rune
		var prevFont *Font
		var prevScale float32
		hasPrev := false

		for _, it := range line {
			tok := it.token
			itemScale := baseScale * tok.Style.effectiveScale()
			switch tok.Kind {
			case TokenGlyph:
				af, fauxB, fauxI := resolveBitmapStyleFont(font, tok.Style, opts)
				g, ok := af.Glyphs[tok.Rune]
				if !ok {
					hasPrev = false
					continue
				}
				if hasPrev && af == prevFont && itemScale == prevScale {
					penX += af.Kerning(prevRune, tok.Rune) * itemScale
				}
				if g.Frame.W > 0 && g.Frame.H > 0 {
					gTop := baselineY - fontBase(af)*itemScale + g.YOffset*itemScale
					layout.Placements = append(layout.Placements, RichPlacement{
						Index:      it.index,
						Kind:       TokenGlyph,
						Rune:       tok.Rune,
						Glyph:      g,
						Font:       af,
						Style:      tok.Style,
						Scale:      itemScale,
						X:          startX + penX + g.XOffset*itemScale,
						Y:          gTop,
						W:          g.Frame.W * itemScale,
						H:          g.Frame.H * itemScale,
						BaselineY:  baselineY,
						Line:       lineIdx,
						FauxBold:   fauxB,
						FauxItalic: fauxI,
					})
				}
				penX += g.XAdvance * itemScale
				prevRune = tok.Rune
				prevFont = af
				prevScale = itemScale
				hasPrev = true

			case TokenIcon:
				hasPrev = false
				if opts.Icons == nil {
					continue
				}
				icon, ok := opts.Icons(tok.IconName)
				if !ok {
					continue
				}
				iw, ih, adv, bOff := resolveIconMetrics(icon, tok, itemScale)
				if iw > 0 && ih > 0 {
					iconY := baselineY - ih + bOff
					layout.Placements = append(layout.Placements, RichPlacement{
						Index:     it.index,
						Kind:      TokenIcon,
						Icon:      icon,
						Style:     tok.Style,
						Scale:     itemScale,
						X:         startX + penX,
						Y:         iconY,
						W:         iw,
						H:         ih,
						BaselineY: baselineY,
						Line:      lineIdx,
					})
				}
				penX += adv
			}
		}

		curY += lineH
		if lineIdx+1 < len(wrapped) {
			curY += opts.LineSpacing
		}
	}

	layout.Size = core.Size{
		Width:  core.Points(maxLineW),
		Height: core.Points(curY),
	}
	return layout
}

// --- Platform Text Rich Layout & Drawing ---

func resolveRichPlatformFont(base core.Font, style RichStyle, extraScale float32) core.Font {
	f := resolvePlatformFont(base)
	scale := style.effectiveScale() * extraScale
	if scale != 1 {
		f.Size = core.Points(float32(f.Size) * scale)
	}
	if style.Bold {
		f.Weight = core.FontWeightBold
	}
	if style.Italic {
		f.Italic = true
	}
	return f
}

func platformFontMetrics(d *core.Drawing, f core.Font, lineHeightOverride float32) (lineH, ascent, descent float32) {
	m := d.TextMetrics("Mg", f)
	ascent = m.Ascent
	descent = m.Descent
	lineH = m.LineHeight
	if lineH <= 0 {
		if ascent+descent > 0 {
			lineH = ascent + descent
		} else {
			lineH = float32(f.Size) * 1.2
		}
	}
	if ascent <= 0 {
		ascent = lineH * 0.8
	}
	if descent <= 0 {
		descent = lineH - ascent
		if descent < 0 {
			descent = 0
		}
	}
	if lineHeightOverride > 0 {
		ratio := lineHeightOverride / lineH
		lineH = lineHeightOverride
		ascent *= ratio
		descent = lineH - ascent
		if descent < 0 {
			descent = 0
		}
	}
	return lineH, ascent, descent
}

// LayoutRichPlatform lays out rt using platform text measurements from d,
// treating inline icons as glyphs aligned to the text baseline.
func LayoutRichPlatform(d *core.Drawing, rt RichText, style PlatformStyle, opts RichOptions) RichLayout {
	if d == nil || len(rt.Tokens) == 0 || opts.Scale < 0 || style.Alpha < 0 || opts.Alpha < 0 {
		return RichLayout{}
	}
	items, totalUnits := indexRichTokens(rt)
	if len(items) == 0 {
		return RichLayout{TotalUnits: totalUnits, isPlatform: true}
	}

	baseScale := resolveScale(opts.Scale)
	maxWidth := style.MaxWidth
	if maxWidth <= 0 {
		maxWidth = opts.MaxWidth
	}
	align := style.Align
	if align == AlignLeft && opts.Align != AlignLeft {
		align = opts.Align
	}
	lineSpacing := style.LineSpacing
	if lineSpacing == 0 {
		lineSpacing = opts.LineSpacing
	}

	baseFont := resolvePlatformFont(style.Font)
	baseLineH, baseAsc, baseDesc := platformFontMetrics(d, resolveRichPlatformFont(baseFont, RichStyle{Scale: 1}, baseScale), style.LineHeight*baseScale)

	measureSeq := func(seq []indexedRichToken) float32 {
		var width float32
		var runBuf strings.Builder
		var runFont core.Font
		inRun := false
		flushRun := func() {
			if !inRun {
				return
			}
			width += d.MeasureText(runBuf.String(), runFont)
			runBuf.Reset()
			inRun = false
		}
		for _, it := range seq {
			tok := it.token
			switch tok.Kind {
			case TokenGlyph:
				pf := resolveRichPlatformFont(baseFont, tok.Style, baseScale)
				if inRun && pf == runFont {
					runBuf.WriteRune(tok.Rune)
				} else {
					flushRun()
					runFont = pf
					runBuf.WriteRune(tok.Rune)
					inRun = true
				}
			case TokenIcon:
				flushRun()
				if opts.Icons == nil {
					continue
				}
				icon, ok := opts.Icons(tok.IconName)
				if !ok {
					continue
				}
				_, _, adv, _ := resolveIconMetrics(icon, tok, baseScale*tok.Style.effectiveScale())
				width += adv
			}
		}
		flushRun()
		return width
	}

	wrapped := wrapRichTokens(items, maxWidth, measureSeq)
	if len(wrapped) == 0 {
		return RichLayout{TotalUnits: totalUnits, isPlatform: true}
	}

	// Compute vertical shift of line 0 relative to pos.Y based on style.Baseline.
	var firstLineOffsetY float32
	switch style.Baseline {
	case core.BaselineTop, core.BaselineHanging:
		firstLineOffsetY = 0
	case core.BaselineMiddle:
		firstLineOffsetY = -baseLineH / 2
	case core.BaselineBottom, core.BaselineIdeographic:
		firstLineOffsetY = -baseLineH
	default: // BaselineAlphabetic
		firstLineOffsetY = -baseAsc
	}

	defColor := style.Color
	if defColor == 0 {
		defColor = opts.Color
	}
	effAlpha := resolveAlpha(style.Alpha) * resolveAlpha(opts.Alpha)

	layout := RichLayout{
		Lines:         make([]RichLine, 0, len(wrapped)),
		TotalUnits:    totalUnits,
		isPlatform:    true,
		platformColor: defColor,
		platformAlpha: effAlpha,
	}

	curY := firstLineOffsetY
	var maxLineW float32
	var totalH float32

	for lineIdx, line := range wrapped {
		lineW := measureSeq(line)
		if lineW > maxLineW {
			maxLineW = lineW
		}
		startX := alignedX(0, lineW, maxWidth, align)

		ascent := baseAsc
		descent := baseDesc
		for _, it := range line {
			tok := it.token
			itemScale := baseScale * tok.Style.effectiveScale()
			switch tok.Kind {
			case TokenGlyph:
				pf := resolveRichPlatformFont(baseFont, tok.Style, baseScale)
				_, gAsc, gDesc := platformFontMetrics(d, pf, style.LineHeight*itemScale)
				if gAsc > ascent {
					ascent = gAsc
				}
				if gDesc > descent {
					descent = gDesc
				}
			case TokenIcon:
				if opts.Icons == nil {
					continue
				}
				icon, ok := opts.Icons(tok.IconName)
				if !ok {
					continue
				}
				_, ih, _, bOff := resolveIconMetrics(icon, tok, itemScale)
				if ih-bOff > ascent {
					ascent = ih - bOff
				}
				if bOff > descent {
					descent = bOff
				}
			}
		}
		lineH := ascent + descent
		baselineY := curY + ascent

		layout.Lines = append(layout.Lines, RichLine{
			Index:    lineIdx,
			X:        startX,
			Y:        curY,
			Width:    lineW,
			Height:   lineH,
			Baseline: ascent,
		})

		var penX float32
		for _, it := range line {
			tok := it.token
			itemScale := baseScale * tok.Style.effectiveScale()
			switch tok.Kind {
			case TokenGlyph:
				pf := resolveRichPlatformFont(baseFont, tok.Style, baseScale)
				glyphStr := string(tok.Rune)
				adv := d.MeasureText(glyphStr, pf)
				_, gAsc, gDesc := platformFontMetrics(d, pf, 0)
				if tok.Rune != ' ' && tok.Rune != '\t' {
					layout.Placements = append(layout.Placements, RichPlacement{
						Index:        it.index,
						Kind:         TokenGlyph,
						Rune:         tok.Rune,
						Style:        tok.Style,
						Scale:        itemScale,
						X:            startX + penX,
						Y:            baselineY - gAsc,
						W:            adv,
						H:            gAsc + gDesc,
						BaselineY:    baselineY,
						Line:         lineIdx,
						PlatformFont: pf,
						isPlatform:   true,
					})
				}
				penX += adv

			case TokenIcon:
				if opts.Icons == nil {
					continue
				}
				icon, ok := opts.Icons(tok.IconName)
				if !ok {
					continue
				}
				iw, ih, adv, bOff := resolveIconMetrics(icon, tok, itemScale)
				if iw > 0 && ih > 0 {
					layout.Placements = append(layout.Placements, RichPlacement{
						Index:      it.index,
						Kind:       TokenIcon,
						Icon:       icon,
						Style:      tok.Style,
						Scale:      itemScale,
						X:          startX + penX,
						Y:          baselineY - ih + bOff,
						W:          iw,
						H:          ih,
						BaselineY:  baselineY,
						Line:       lineIdx,
						isPlatform: true,
					})
				}
				penX += adv
			}
		}

		curY += lineH
		totalH += lineH
		if lineIdx+1 < len(wrapped) {
			curY += lineSpacing
			totalH += lineSpacing
		}
	}

	layout.Size = core.Size{
		Width:  core.Points(maxLineW),
		Height: core.Points(totalH),
	}
	return layout
}

// --- Drawing & Batching ---

// DrawRich lays out and draws rt at pos into d.
//
// When font is non-nil, bitmap glyph rendering is used (with cached pixel
// tinting for colored spans when the page image provides Pixels or Data, and
// sprite.Batch grouping for glyphs and inline icons). When font is nil and
// opts.Platform is set, platform-text rich span rendering is used.
func DrawRich(d *core.Drawing, font *Font, rt RichText, pos core.Point, opts RichOptions) {
	if d == nil || len(rt.Tokens) == 0 || opts.Scale < 0 || opts.Alpha < 0 {
		return
	}
	if font == nil {
		if opts.Platform != nil {
			DrawRichPlatform(d, rt, pos, *opts.Platform, opts)
		}
		return
	}
	layout := LayoutRich(font, rt, opts)
	DrawRichLayout(d, layout, pos, opts)
}

// DrawRichPlatform lays out and draws rt at pos into d using platform text
// (core.Drawing.FillText) for styled text spans and inline images/atlas frames
// for icons.
func DrawRichPlatform(d *core.Drawing, rt RichText, pos core.Point, style PlatformStyle, opts RichOptions) {
	if d == nil || len(rt.Tokens) == 0 || style.Alpha < 0 || opts.Alpha < 0 || opts.Scale < 0 {
		return
	}
	layout := LayoutRichPlatform(d, rt, style, opts)
	drawRichLayoutLimit(d, layout, pos, opts, -1)
}

// DrawRichLayout draws a previously computed RichLayout at pos into d,
// applying per-glyph effects evaluated at opts.Elapsed.
func DrawRichLayout(d *core.Drawing, layout RichLayout, pos core.Point, opts RichOptions) {
	drawRichLayoutLimit(d, layout, pos, opts, -1)
}

// AppendRichToBatch appends sprites from layout whose resolved atlas matches
// batch.Atlas (both bitmap glyphs and inline icons on that atlas) into batch,
// positioned at pos with per-glyph effects evaluated at opts.Elapsed.
//
// It returns the number of sprites appended to batch. Placements requiring a
// different atlas or faux-italic shear transform are skipped.
func AppendRichToBatch(batch *sprite.Batch, layout RichLayout, pos core.Point, opts RichOptions) int {
	if batch == nil || batch.Atlas == nil || len(layout.Placements) == 0 || opts.Alpha < 0 || opts.Scale < 0 {
		return 0
	}
	ox, oy := float32(pos.X), float32(pos.Y)
	alpha := resolveAlpha(opts.Alpha)
	appended := 0

	for _, p := range layout.Placements {
		if p.isPlatform || p.FauxItalic {
			continue
		}
		dx, dy, colOverride := evalPlacementEffect(p, opts.Elapsed)
		effColor := resolvePlacementColor(p, opts.Color, colOverride)

		atlas, sp, ok := buildPlacementSprite(p, ox+dx, oy+dy, alpha, effColor, opts.Z)
		if !ok || atlas == nil {
			continue
		}
		if atlas != batch.Atlas && !atlas.Image.Same(batch.Atlas.Image) {
			continue
		}
		spCopy := sp
		batch.Sprites = append(batch.Sprites, &spCopy)
		appended++

		if p.Kind == TokenGlyph && p.FauxBold {
			boldSp := sp
			boldSp.X += p.Scale
			batch.Sprites = append(batch.Sprites, &boldSp)
			appended++
		}
	}
	return appended
}

func drawRichLayoutLimit(d *core.Drawing, layout RichLayout, pos core.Point, opts RichOptions, visibleLimit int) {
	if d == nil || len(layout.Placements) == 0 || opts.Alpha < 0 || opts.Scale < 0 {
		return
	}
	if layout.isPlatform {
		drawPlatformRichLayout(d, layout, pos, opts, visibleLimit)
		return
	}
	drawBitmapRichLayout(d, layout, pos, opts, visibleLimit)
}

func drawBitmapRichLayout(d *core.Drawing, layout RichLayout, pos core.Point, opts RichOptions, visibleLimit int) {
	ox, oy := float32(pos.X), float32(pos.Y)
	baseAlpha := resolveAlpha(opts.Alpha)

	var curBatch *sprite.Batch
	flushBatch := func() {
		if curBatch != nil && len(curBatch.Sprites) > 0 {
			curBatch.Draw(d)
		}
		curBatch = nil
	}

	for _, p := range layout.Placements {
		if visibleLimit >= 0 && p.Index >= visibleLimit {
			continue
		}
		dx, dy, colOverride := evalPlacementEffect(p, opts.Elapsed)
		effColor := resolvePlacementColor(p, opts.Color, colOverride)
		px := ox + p.X + dx
		py := oy + p.Y + dy

		if p.Kind == TokenIcon && p.Icon.Frame.W <= 0 && p.Icon.Frame.H <= 0 {
			flushBatch()
			img := resolveIconImage(p.Icon)
			if img.Empty() || p.W <= 0 || p.H <= 0 {
				continue
			}
			if baseAlpha < 1 {
				d.Save().SetAlpha(baseAlpha).DrawImageIn(img, px, py, p.W, p.H).Restore()
			} else {
				d.DrawImageIn(img, px, py, p.W, p.H)
			}
			continue
		}

		atlas, sp, ok := buildPlacementSprite(p, ox+dx, oy+dy, baseAlpha, effColor, opts.Z)
		if !ok || atlas == nil {
			continue
		}

		if p.Kind == TokenGlyph && p.FauxItalic {
			flushBatch()
			drawShearedSprite(d, atlas.Image, sp, oy+p.BaselineY+dy)
			if p.FauxBold {
				boldSp := sp
				boldSp.X += p.Scale
				drawShearedSprite(d, atlas.Image, boldSp, oy+p.BaselineY+dy)
			}
			continue
		}

		if curBatch == nil || (curBatch.Atlas != atlas && !curBatch.Atlas.Image.Same(atlas.Image)) {
			flushBatch()
			curBatch = &sprite.Batch{Atlas: atlas}
		}
		spCopy := sp
		curBatch.Sprites = append(curBatch.Sprites, &spCopy)
		if p.Kind == TokenGlyph && p.FauxBold {
			boldSp := sp
			boldSp.X += p.Scale
			curBatch.Sprites = append(curBatch.Sprites, &boldSp)
		}
	}
	flushBatch()
}

func drawShearedSprite(d *core.Drawing, img core.ImageSource, sp sprite.Sprite, baselineY float32) {
	f := sp.Frame
	if f.W <= 0 || f.H <= 0 || sp.Alpha <= 0 || sp.ScaleX == 0 || sp.ScaleY == 0 {
		return
	}
	w := f.W * sp.ScaleX
	h := f.H * sp.ScaleY
	// Horizontal shear anchored vertically at baselineY:
	// x' = x - fauxItalicShear*(y - baselineY) = x - fauxItalicShear*y + fauxItalicShear*baselineY
	d.Save()
	if sp.Alpha < 1 {
		d.SetAlpha(sp.Alpha)
	}
	d.Transform(1, 0, -fauxItalicShear, 1, fauxItalicShear*baselineY, 0)
	d.DrawImageFrom(img, f.X, f.Y, f.W, f.H, sp.X, sp.Y, w, h)
	d.Restore()
}

func drawPlatformRichLayout(d *core.Drawing, layout RichLayout, pos core.Point, opts RichOptions, visibleLimit int) {
	ox, oy := float32(pos.X), float32(pos.Y)
	alpha := layout.platformAlpha
	if alpha <= 0 {
		alpha = resolveAlpha(opts.Alpha)
	}
	if alpha <= 0 {
		return
	}
	defColor := layout.platformColor
	if defColor == 0 {
		defColor = opts.Color
	}

	d.Save()
	if alpha < 1 {
		d.SetAlpha(alpha)
	}
	d.SetTextAlign(core.TextAlignLeft)
	d.SetTextBaseline(core.BaselineAlphabetic)

	var lastColor core.Color
	hasLastColor := false
	colorSaved := false

	applyColor := func(c core.Color) {
		if c == 0 {
			if colorSaved {
				d.Restore()
				colorSaved = false
				hasLastColor = false
				lastColor = 0
			}
			return
		}
		if defColor == 0 && !colorSaved {
			d.Save()
			colorSaved = true
		}
		if !hasLastColor || lastColor != c {
			d.SetFillColor(c)
			lastColor = c
			hasLastColor = true
		}
	}

	// Group contiguous unaffected glyphs on the same line/font/color into spans
	// for clean, minimal FillText ops, while drawing effected glyphs individually.
	i := 0
	for i < len(layout.Placements) {
		p := layout.Placements[i]
		if visibleLimit >= 0 && p.Index >= visibleLimit {
			i++
			continue
		}

		if p.Kind == TokenIcon {
			dx, dy, _ := evalPlacementEffect(p, opts.Elapsed)
			drawInlineIconDirect(d, p.Icon, ox+p.X+dx, oy+p.Y+dy, p.W, p.H)
			i++
			continue
		}

		if p.Style.Effect.Kind != EffectNone {
			dx, dy, colOverride := evalPlacementEffect(p, opts.Elapsed)
			effColor := resolvePlacementColor(p, defColor, colOverride)
			applyColor(effColor)
			d.FillText(string(p.Rune), ox+p.X+dx, oy+p.BaselineY+dy, p.PlatformFont)
			i++
			continue
		}

		// Coalesce contiguous unaffected glyphs with the same line, font, and color.
		spanColor := resolvePlacementColor(p, defColor, nil)
		var buf strings.Builder
		buf.WriteRune(p.Rune)
		startX := ox + p.X
		baseY := oy + p.BaselineY
		expectedX := p.X + p.W
		j := i + 1
		for j < len(layout.Placements) {
			next := layout.Placements[j]
			if visibleLimit >= 0 && next.Index >= visibleLimit {
				break
			}
			if next.Kind != TokenGlyph || next.Style.Effect.Kind != EffectNone {
				break
			}
			if next.Line != p.Line || next.PlatformFont != p.PlatformFont {
				break
			}
			if resolvePlacementColor(next, defColor, nil) != spanColor {
				break
			}
			// Only merge if directly adjacent (no intervening skipped space).
			if math.Abs(float64(next.X-expectedX)) > 1e-3 {
				break
			}
			buf.WriteRune(next.Rune)
			expectedX = next.X + next.W
			j++
		}
		applyColor(spanColor)
		d.FillText(buf.String(), startX, baseY, p.PlatformFont)
		i = j
	}

	if colorSaved {
		d.Restore()
	}
	d.Restore()
}

func drawInlineIconDirect(d *core.Drawing, icon Icon, x, y, w, h float32) {
	if w <= 0 || h <= 0 {
		return
	}
	img := resolveIconImage(icon)
	if img.Empty() {
		return
	}
	if icon.Frame.W > 0 && icon.Frame.H > 0 {
		d.DrawImageFrom(img, icon.Frame.X, icon.Frame.Y, icon.Frame.W, icon.Frame.H, x, y, w, h)
		return
	}
	d.DrawImageIn(img, x, y, w, h)
}

func resolveIconImage(icon Icon) core.ImageSource {
	if !icon.Image.Empty() {
		return icon.Image
	}
	if icon.Atlas != nil {
		return icon.Atlas.Image
	}
	return core.ImageSource{}
}

func evalPlacementEffect(p RichPlacement, elapsed float32) (dx, dy float32, colorOverride *core.Color) {
	if p.Style.Effect.Kind == EffectNone {
		return 0, 0, nil
	}
	return EvalGlyphTransform(p.Style.Effect, p.Index, elapsed)
}

func resolvePlacementColor(p RichPlacement, defaultColor core.Color, override *core.Color) core.Color {
	if override != nil && *override != 0 {
		return *override
	}
	if p.Style.Color != 0 {
		return p.Style.Color
	}
	return defaultColor
}

func buildPlacementSprite(p RichPlacement, ox, oy, baseAlpha float32, effColor core.Color, z int) (*sprite.Atlas, sprite.Sprite, bool) {
	switch p.Kind {
	case TokenGlyph:
		if p.Font == nil {
			return nil, sprite.Sprite{}, false
		}
		atlas := p.Font.Pages[p.Glyph.Page]
		if atlas == nil {
			return nil, sprite.Sprite{}, false
		}
		alpha := baseAlpha
		if effColor != 0 {
			tinted, applied := getTintedAtlas(atlas, effColor)
			if applied {
				atlas = tinted
			} else {
				// URI-only page image without in-memory pixels: honor the color's alpha channel.
				_, _, _, ca := effColor.Components()
				alpha *= float32(ca)
			}
		}
		sp := sprite.NewSprite(p.Glyph.Frame)
		sp.X = ox + p.X
		sp.Y = oy + p.Y
		sp.ScaleX = p.Scale
		sp.ScaleY = p.Scale
		sp.Alpha = alpha
		sp.Z = z
		return atlas, sp, true

	case TokenIcon:
		img := resolveIconImage(p.Icon)
		if img.Empty() || p.W <= 0 || p.H <= 0 || p.Icon.Frame.W <= 0 || p.Icon.Frame.H <= 0 {
			return nil, sprite.Sprite{}, false
		}
		atlas := p.Icon.Atlas
		if atlas == nil {
			atlas = &sprite.Atlas{Image: img}
		}
		frame := p.Icon.Frame
		// Strip trim offsets so icon draws precisely inside (p.X, p.Y, p.W, p.H).
		frame.OffsetX = 0
		frame.OffsetY = 0
		frame.Width = frame.W
		frame.Height = frame.H

		sp := sprite.NewSprite(frame)
		sp.X = ox + p.X
		sp.Y = oy + p.Y
		sp.ScaleX = p.W / frame.W
		sp.ScaleY = p.H / frame.H
		sp.Alpha = baseAlpha
		sp.Z = z
		return atlas, sp, true

	default:
		return nil, sprite.Sprite{}, false
	}
}

// --- Tinted Bitmap Atlas Cache ---

type tintCacheKey struct {
	pixels  *image.RGBA
	version uint64
	uri     string
	dataLen int
	dataPtr *byte
	color   core.Color
}

var (
	tintCacheMu sync.Mutex
	tintCache   = make(map[tintCacheKey]*sprite.Atlas)
)

// getTintedAtlas returns a cached pixel-tinted copy of atlas for color when
// atlas.Image has in-memory Pixels or decodable Data bytes.
func getTintedAtlas(atlas *sprite.Atlas, color core.Color) (*sprite.Atlas, bool) {
	if atlas == nil || color == 0 {
		return atlas, false
	}
	// Opaque white leaves pixels unchanged.
	if color == core.RGB(0xffffff) {
		return atlas, true
	}

	img := atlas.Image
	if img.Pixels == nil && len(img.Data) == 0 {
		return atlas, false
	}

	var dataPtr *byte
	if len(img.Data) > 0 {
		dataPtr = &img.Data[0]
	}
	key := tintCacheKey{
		pixels:  img.Pixels,
		version: img.Version,
		uri:     img.URI,
		dataLen: len(img.Data),
		dataPtr: dataPtr,
		color:   color,
	}

	tintCacheMu.Lock()
	if cached, ok := tintCache[key]; ok {
		tintCacheMu.Unlock()
		return cached, true
	}
	tintCacheMu.Unlock()

	srcRGBA := img.Pixels
	if srcRGBA == nil && len(img.Data) > 0 {
		decoded, _, err := image.Decode(bytes.NewReader(img.Data))
		if err != nil {
			return atlas, false
		}
		if rgba, ok := decoded.(*image.RGBA); ok {
			srcRGBA = rgba
		} else {
			b := decoded.Bounds()
			dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
			imagedraw.Draw(dst, dst.Bounds(), decoded, b.Min, imagedraw.Src)
			srcRGBA = dst
		}
	}
	if srcRGBA == nil {
		return atlas, false
	}

	tintedRGBA := tintRGBA(srcRGBA, color)
	tintedAtlas := &sprite.Atlas{
		Image: core.ImageSource{
			Pixels: tintedRGBA,
			Size:   img.Size,
		},
		Frames: atlas.Frames,
	}

	tintCacheMu.Lock()
	if len(tintCache) >= maxTintCacheSize {
		clear(tintCache)
	}
	tintCache[key] = tintedAtlas
	tintCacheMu.Unlock()

	return tintedAtlas, true
}

// tintRGBA multiplies a premultiplied *image.RGBA by color (0xRRGGBBAA).
func tintRGBA(src *image.RGBA, color core.Color) *image.RGBA {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))

	tr := uint32((color >> 24) & 0xff)
	tg := uint32((color >> 16) & 0xff)
	tb := uint32((color >> 8) & 0xff)
	ta := uint32(color & 0xff)
	if color == core.Transparent {
		ta = 0
	}

	// Premultiplied RGB multipliers: (tr * ta) / 255.
	mr := (tr*ta + 127) / 255
	mg := (tg*ta + 127) / 255
	mb := (tb*ta + 127) / 255

	for y := 0; y < h; y++ {
		sOff := y * src.Stride
		dOff := y * dst.Stride
		for x := 0; x < w; x++ {
			sr := uint32(src.Pix[sOff+0])
			sg := uint32(src.Pix[sOff+1])
			sb := uint32(src.Pix[sOff+2])
			sa := uint32(src.Pix[sOff+3])

			da := (sa*ta + 127) / 255
			dr := (sr*mr + 127) / 255
			dg := (sg*mg + 127) / 255
			db := (sb*mb + 127) / 255
			if dr > da {
				dr = da
			}
			if dg > da {
				dg = da
			}
			if db > da {
				db = da
			}

			dst.Pix[dOff+0] = uint8(dr)
			dst.Pix[dOff+1] = uint8(dg)
			dst.Pix[dOff+2] = uint8(db)
			dst.Pix[dOff+3] = uint8(da)
			sOff += 4
			dOff += 4
		}
	}
	return dst
}
