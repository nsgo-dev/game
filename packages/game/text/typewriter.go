package text

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// EffectKind identifies a per-glyph animation effect.
type EffectKind uint8

const (
	// EffectNone disables per-glyph motion and color overrides.
	EffectNone EffectKind = iota
	// EffectWave displaces glyphs vertically along a smooth sine wave.
	EffectWave
	// EffectShake jitters glyphs deterministically in X and Y.
	EffectShake
	// EffectCustom invokes GlyphEffect.Custom to compute offsets and color overrides.
	EffectCustom
)

// GlyphEffectFunc computes a deterministic per-glyph translation (dx, dy) and
// optional color override for EffectCustom.
type GlyphEffectFunc func(effect GlyphEffect, glyphIndex int, elapsed float32) (dx, dy float32, colorOverride *core.Color)

// GlyphEffect configures a deterministic per-glyph animation effect.
type GlyphEffect struct {
	Kind      EffectKind
	Seed      uint64
	Amplitude float32
	Frequency float32
	Speed     float32
	// Custom is invoked when Kind == EffectCustom.
	Custom GlyphEffectFunc
}

// EvalGlyphTransform evaluates effect for glyphIndex at time elapsed (in seconds).
//
// The result is completely deterministic for any (effect.Seed, glyphIndex, elapsed)
// tuple and uses pure integer/float math without global RNG state.
func EvalGlyphTransform(effect GlyphEffect, glyphIndex int, elapsed float32) (dx, dy float32, colorOverride *core.Color) {
	switch effect.Kind {
	case EffectWave:
		if effect.Amplitude == 0 {
			return 0, 0, nil
		}
		var seedPhase float64
		if effect.Seed != 0 {
			h := splitmix64(effect.Seed)
			seedPhase = (float64(h>>11) / float64(uint64(1)<<53)) * (2 * math.Pi)
		}
		phase := float64(elapsed)*float64(effect.Speed) + float64(glyphIndex)*float64(effect.Frequency) + seedPhase
		return 0, float32(math.Sin(phase)) * effect.Amplitude, nil

	case EffectShake:
		if effect.Amplitude == 0 {
			return 0, 0, nil
		}
		var step int64
		if effect.Speed != 0 {
			step = int64(math.Floor(float64(elapsed) * float64(effect.Speed)))
		}
		h0 := splitmix64(effect.Seed ^ (uint64(glyphIndex) * 0x9e3779b97f4a7c15) ^ (uint64(step) * 0xbf58476d1ce4e5b9))
		hx := splitmix64(h0 ^ 0x517cc1b727220a95)
		hy := splitmix64(h0 ^ 0x2545f4914f6cdd1d)
		nx := (float64(hx>>11)/float64(uint64(1)<<53))*2.0 - 1.0
		ny := (float64(hy>>11)/float64(uint64(1)<<53))*2.0 - 1.0
		return float32(nx) * effect.Amplitude, float32(ny) * effect.Amplitude, nil

	case EffectCustom:
		if effect.Custom != nil {
			return effect.Custom(effect, glyphIndex, elapsed)
		}
		return 0, 0, nil

	default:
		return 0, 0, nil
	}
}

func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

type typewriterUnit struct {
	runeVal     rune
	isIcon      bool
	pauseBefore float32
}

// Typewriter reveals a RichText progressively at a deterministic rate while
// tracking elapsed animation time for per-glyph effects.
type Typewriter struct {
	// CharsPerSecond is the reveal rate in units (glyphs or inline icons) per second.
	CharsPerSecond float32
	// PunctuationPause optionally defines step multipliers after punctuation runes
	// (e.g., '.' -> 4.0 waits 3 extra character steps after revealing '.').
	// Use DefaultPunctuationPause() for common dialogue pacing, or leave nil for
	// uniform character timing.
	PunctuationPause map[rune]float32
	// OnReveal is called once as each unit becomes visible. For inline icons, r is 0.
	OnReveal func(glyphIndex int, r rune)
	// OnComplete is called strictly once when all units have been revealed.
	OnComplete func()

	rt        RichText
	units     []typewriterUnit
	visible   int
	elapsed   float64
	budget    float64
	waitTimer float64
	completed bool
	gen       uint64
}

// DefaultPunctuationPause returns a standard dialogue punctuation pause table
// (4x step after sentence terminators, 2x step after clause separators).
func DefaultPunctuationPause() map[rune]float32 {
	return map[rune]float32{
		'.': 4,
		'!': 4,
		'?': 4,
		',': 2,
		';': 2,
		':': 2,
	}
}

// NewTypewriter creates a Typewriter revealing rt at charsPerSecond units per second.
func NewTypewriter(rt RichText, charsPerSecond float32) *Typewriter {
	tw := &Typewriter{
		CharsPerSecond: charsPerSecond,
	}
	tw.Reset(rt)
	return tw
}

// NewTypewriterString parses markup via ParseRich and creates a Typewriter
// revealing it at charsPerSecond units per second.
func NewTypewriterString(markup string, charsPerSecond float32) *Typewriter {
	return NewTypewriter(ParseRich(markup), charsPerSecond)
}

// Reset replaces the typewriter's RichText and resets reveal progress, timers,
// elapsed time, and completion state while preserving speed and callbacks.
func (tw *Typewriter) Reset(rt RichText) {
	if tw == nil {
		return
	}
	tw.rt = rt
	tw.units = buildTypewriterUnits(rt)
	tw.visible = 0
	tw.elapsed = 0
	tw.budget = 0
	tw.waitTimer = 0
	tw.completed = false
	tw.gen++
	if len(tw.units) > 0 && tw.units[0].pauseBefore > 0 {
		tw.waitTimer = float64(tw.units[0].pauseBefore)
	}
}

func buildTypewriterUnits(rt RichText) []typewriterUnit {
	if len(rt.Tokens) == 0 {
		return nil
	}
	units := make([]typewriterUnit, 0, len(rt.Tokens))
	var pendingPause float32
	for i := 0; i < len(rt.Tokens); i++ {
		tok := rt.Tokens[i]
		switch tok.Kind {
		case TokenPause:
			if tok.PauseSeconds > 0 {
				pendingPause += tok.PauseSeconds
			}
		case TokenGlyph:
			if tok.Rune == '\r' || tok.Rune == '\n' {
				continue
			}
			units = append(units, typewriterUnit{
				runeVal:     tok.Rune,
				pauseBefore: pendingPause,
			})
			pendingPause = 0
		case TokenIcon:
			units = append(units, typewriterUnit{
				isIcon:      true,
				pauseBefore: pendingPause,
			})
			pendingPause = 0
		}
	}
	return units
}

// Update advances the typewriter deterministically by dt seconds.
//
// Elapsed time continues advancing after the text is fully revealed so active
// per-glyph effects ([wave], [shake]) keep animating. OnReveal fires once per
// newly revealed unit and OnComplete fires strictly once when fully revealed.
func (tw *Typewriter) Update(dt float32) {
	if tw == nil || dt <= 0 || math.IsNaN(float64(dt)) || math.IsInf(float64(dt), 0) {
		return
	}
	tw.elapsed += float64(dt)

	if len(tw.units) == 0 {
		tw.fireCompleteOnce()
		return
	}
	if tw.visible >= len(tw.units) {
		tw.fireCompleteOnce()
		return
	}
	if tw.CharsPerSecond <= 0 {
		return
	}

	step := 1.0 / float64(tw.CharsPerSecond)
	eps := 1e-6 * step
	remaining := float64(dt)
	startGen := tw.gen

	for tw.visible < len(tw.units) {
		if tw.waitTimer > 0 {
			if remaining+1e-6*tw.waitTimer < tw.waitTimer {
				tw.waitTimer -= remaining
				remaining = 0
				break
			}
			remaining -= tw.waitTimer
			if remaining < 0 {
				remaining = 0
			}
			tw.waitTimer = 0
		}

		tw.budget += remaining
		remaining = 0

		if tw.budget+eps < step {
			break
		}

		tw.budget -= step
		if tw.budget < 0 {
			tw.budget = 0
		}

		idx := tw.visible
		u := tw.units[idx]
		tw.visible++

		if tw.OnReveal != nil {
			tw.OnReveal(idx, u.runeVal)
			if tw.gen != startGen || tw.completed {
				return
			}
		}

		if tw.visible >= len(tw.units) {
			tw.budget = 0
			tw.waitTimer = 0
			tw.fireCompleteOnce()
			return
		}

		var nextWait float64
		if !u.isIcon && len(tw.PunctuationPause) > 0 {
			if mult, ok := tw.PunctuationPause[u.runeVal]; ok && mult > 1 {
				nextWait += float64(mult-1) * step
			}
		}
		if tw.units[tw.visible].pauseBefore > 0 {
			nextWait += float64(tw.units[tw.visible].pauseBefore)
		}
		if nextWait > 0 {
			tw.waitTimer += nextWait
			if tw.budget > 0 {
				remaining = tw.budget
				tw.budget = 0
			}
		}
	}
}

// Skip immediately reveals all remaining units and invokes OnComplete once if
// it has not already fired.
func (tw *Typewriter) Skip() {
	if tw == nil {
		return
	}
	tw.visible = len(tw.units)
	tw.budget = 0
	tw.waitTimer = 0
	tw.fireCompleteOnce()
}

func (tw *Typewriter) fireCompleteOnce() {
	if tw.completed {
		return
	}
	tw.completed = true
	if tw.OnComplete != nil {
		tw.OnComplete()
	}
}

// RichText returns the Typewriter's parsed RichText.
func (tw *Typewriter) RichText() RichText {
	if tw == nil {
		return RichText{}
	}
	return tw.rt
}

// VisibleCount returns the number of currently revealed units (glyphs and icons).
func (tw *Typewriter) VisibleCount() int {
	if tw == nil {
		return 0
	}
	return tw.visible
}

// TotalCount returns the total number of revealable units (non-newline glyphs and icons).
func (tw *Typewriter) TotalCount() int {
	if tw == nil {
		return 0
	}
	return len(tw.units)
}

// Done reports whether all units have been revealed.
func (tw *Typewriter) Done() bool {
	if tw == nil {
		return true
	}
	return tw.visible >= len(tw.units)
}

// Progress returns the reveal fraction in [0, 1]. For empty text it returns 1.
func (tw *Typewriter) Progress() float32 {
	if tw == nil || len(tw.units) == 0 {
		return 1
	}
	return float32(tw.visible) / float32(len(tw.units))
}

// Elapsed returns the total elapsed time in seconds accumulated via Update.
func (tw *Typewriter) Elapsed() float32 {
	if tw == nil {
		return 0
	}
	return float32(tw.elapsed)
}

// Draw draws only the currently revealed glyphs and inline icons at pos into d,
// keeping the full wrapped layout stable while evaluating per-glyph effects at
// the typewriter's Elapsed time.
//
// When font is nil and opts.Platform is set, platform-text rendering is used.
func (tw *Typewriter) Draw(d *core.Drawing, font *Font, pos core.Point, opts RichOptions) {
	if tw == nil || d == nil || tw.visible <= 0 || len(tw.rt.Tokens) == 0 || opts.Scale < 0 || opts.Alpha < 0 {
		return
	}
	if opts.Elapsed == 0 {
		opts.Elapsed = tw.Elapsed()
	}
	if font == nil {
		if opts.Platform != nil {
			tw.DrawPlatform(d, pos, *opts.Platform, opts)
		}
		return
	}
	layout := LayoutRich(font, tw.rt, opts)
	drawRichLayoutLimit(d, layout, pos, opts, tw.visible)
}

// DrawPlatform draws only the currently revealed glyphs and inline icons at pos
// into d using platform text rendering, keeping the full wrapped layout stable.
func (tw *Typewriter) DrawPlatform(d *core.Drawing, pos core.Point, style PlatformStyle, opts RichOptions) {
	if tw == nil || d == nil || tw.visible <= 0 || len(tw.rt.Tokens) == 0 || style.Alpha < 0 || opts.Alpha < 0 || opts.Scale < 0 {
		return
	}
	if opts.Elapsed == 0 {
		opts.Elapsed = tw.Elapsed()
	}
	layout := LayoutRichPlatform(d, tw.rt, style, opts)
	drawRichLayoutLimit(d, layout, pos, opts, tw.visible)
}
