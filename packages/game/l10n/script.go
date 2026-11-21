package l10n

import (
	"sync"
	"unicode"
	"unicode/utf8"
)

// Script identifies a Unicode writing script for font selection and text
// segmentation.
type Script uint8

const (
	ScriptCommon Script = iota
	ScriptLatin
	ScriptCJK
	ScriptJapanese
	ScriptKorean
	ScriptArabic
	ScriptHebrew
	ScriptCyrillic
	ScriptGreek
	ScriptThai
	ScriptDevanagari
)

// String returns the human-readable name of s.
func (s Script) String() string {
	switch s {
	case ScriptCommon:
		return "Common"
	case ScriptLatin:
		return "Latin"
	case ScriptCJK:
		return "CJK"
	case ScriptJapanese:
		return "Japanese"
	case ScriptKorean:
		return "Korean"
	case ScriptArabic:
		return "Arabic"
	case ScriptHebrew:
		return "Hebrew"
	case ScriptCyrillic:
		return "Cyrillic"
	case ScriptGreek:
		return "Greek"
	case ScriptThai:
		return "Thai"
	case ScriptDevanagari:
		return "Devanagari"
	default:
		return "Common"
	}
}

// ScriptRun is a contiguous slice of text sharing a resolved Script.
// Start and End are byte offsets into the original string.
type ScriptRun struct {
	Text   string
	Script Script
	Start  int
	End    int
}

// RuneScript classifies a single Unicode rune into its Script.
// Spaces, digits, punctuation, and symbols return ScriptCommon.
func RuneScript(r rune) Script {
	switch {
	case unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
		return ScriptJapanese
	case unicode.Is(unicode.Hangul, r):
		return ScriptKorean
	case unicode.Is(unicode.Han, r):
		return ScriptCJK
	case unicode.Is(unicode.Arabic, r):
		return ScriptArabic
	case unicode.Is(unicode.Hebrew, r):
		return ScriptHebrew
	case unicode.Is(unicode.Cyrillic, r):
		return ScriptCyrillic
	case unicode.Is(unicode.Greek, r):
		return ScriptGreek
	case unicode.Is(unicode.Thai, r):
		return ScriptThai
	case unicode.Is(unicode.Devanagari, r):
		return ScriptDevanagari
	case unicode.Is(unicode.Latin, r):
		return ScriptLatin
	default:
		return ScriptCommon
	}
}

// DetectScript returns the dominant non-Common Script present in s, or
// ScriptCommon if s contains only neutral characters (digits, spaces,
// punctuation) or is empty.
//
// When Japanese Kana (Hiragana/Katakana) appears alongside CJK ideographs
// (Kanji), ScriptJapanese takes precedence; similarly ScriptKorean takes
// precedence when Hangul appears alongside Hanja.
func DetectScript(s string) Script {
	var counts [ScriptDevanagari + 1]int
	var firstOrder []Script
	var seen [ScriptDevanagari + 1]bool

	for _, r := range s {
		sc := RuneScript(r)
		if sc == ScriptCommon {
			continue
		}
		counts[sc]++
		if !seen[sc] {
			seen[sc] = true
			firstOrder = append(firstOrder, sc)
		}
	}

	if len(firstOrder) == 0 {
		return ScriptCommon
	}

	// Japanese text mixes Kana with Kanji (Han); fold Han counts into Japanese
	// when Kana is present and Hangul is absent.
	if counts[ScriptJapanese] > 0 && counts[ScriptKorean] == 0 {
		counts[ScriptJapanese] += counts[ScriptCJK]
		counts[ScriptCJK] = 0
	} else if counts[ScriptKorean] > 0 && counts[ScriptJapanese] == 0 {
		counts[ScriptKorean] += counts[ScriptCJK]
		counts[ScriptCJK] = 0
	}

	best := ScriptCommon
	bestCount := 0
	for _, sc := range firstOrder {
		if counts[sc] > bestCount {
			bestCount = counts[sc]
			best = sc
		}
	}
	return best
}

// SegmentByScript splits mixed-script text (such as "Score: 100 得分") into
// contiguous ScriptRuns.
//
// Neutral ScriptCommon characters (spaces, digits, punctuation) attach to the
// preceding strong script, or to the first strong script when they appear at
// the start of the string. A string containing only ScriptCommon characters
// returns a single ScriptCommon run; an empty string returns nil.
func SegmentByScript(s string) []ScriptRun {
	if s == "" {
		return nil
	}

	var runs []ScriptRun
	curScript := ScriptCommon
	runStart := 0

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		sc := RuneScript(r)
		if sc != ScriptCommon {
			if curScript == ScriptCommon {
				// Leading neutral characters attach to the first strong script.
				curScript = sc
			} else if sc != curScript {
				runs = append(runs, ScriptRun{
					Text:   s[runStart:i],
					Script: curScript,
					Start:  runStart,
					End:    i,
				})
				runStart = i
				curScript = sc
			}
		}
		i += size
	}

	runs = append(runs, ScriptRun{
		Text:   s[runStart:],
		Script: curScript,
		Start:  runStart,
		End:    len(s),
	})
	return runs
}

// LocaleScript returns the primary writing Script associated with a BCP-47
// locale tag (respecting explicit script subtags such as "sr-Latn" or
// "zh-Hans" before falling back to the primary language subtag).
func LocaleScript(locale string) Script {
	if sub := scriptSubtag(locale); sub != "" {
		switch sub {
		case "Latn":
			return ScriptLatin
		case "Hans", "Hant", "Hani":
			return ScriptCJK
		case "Jpan", "Hira", "Kana":
			return ScriptJapanese
		case "Kore", "Hang":
			return ScriptKorean
		case "Arab":
			return ScriptArabic
		case "Hebr":
			return ScriptHebrew
		case "Cyrl":
			return ScriptCyrillic
		case "Grek":
			return ScriptGreek
		case "Thai":
			return ScriptThai
		case "Deva":
			return ScriptDevanagari
		}
	}

	switch primaryLanguage(locale) {
	case "zh", "yue", "wuu":
		return ScriptCJK
	case "ja":
		return ScriptJapanese
	case "ko":
		return ScriptKorean
	case "ar", "fa", "ur", "ps", "sd", "ug", "ckb", "prs", "ks", "pnb":
		return ScriptArabic
	case "he", "iw", "yi", "ji":
		return ScriptHebrew
	case "ru", "uk", "be", "bg", "sr", "mk", "kk", "ky", "mn", "tg":
		return ScriptCyrillic
	case "el":
		return ScriptGreek
	case "th":
		return ScriptThai
	case "hi", "mr", "ne", "sa":
		return ScriptDevanagari
	case "":
		return ScriptCommon
	default:
		return ScriptLatin
	}
}

func localeMatchesScript(locale string, script Script) bool {
	ls := LocaleScript(locale)
	if ls == script {
		return true
	}
	// Han ideographs (ScriptCJK) are used by Japanese (Kanji) and Korean (Hanja).
	if script == ScriptCJK && (ls == ScriptJapanese || ls == ScriptKorean) {
		return true
	}
	return false
}

// FontRun pairs a segmented text run with its resolved Script and font.
type FontRun[F any] struct {
	Text   string
	Script Script
	Font   F
	Start  int
	End    int
}

// FontSet maps Unicode scripts and BCP-47 locales to fonts of any type F, so
// the same selector works with platform fonts (core.Font) as well as bitmap or
// atlas fonts (such as *text.Font).
type FontSet[F any] struct {
	mu          sync.RWMutex
	defaultFont F
	scripts     map[Script]F
	locales     map[string]F
}

// NewFontSet creates a FontSet with defaultFont as its fallback font.
func NewFontSet[F any](defaultFont F) *FontSet[F] {
	return &FontSet[F]{
		defaultFont: defaultFont,
		scripts:     make(map[Script]F),
		locales:     make(map[string]F),
	}
}

// Default returns the fallback font configured on fs.
func (fs *FontSet[F]) Default() F {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.defaultFont
}

// SetScript registers font for a Unicode script (for example ScriptCJK or
// ScriptArabic).
func (fs *FontSet[F]) SetScript(script Script, font F) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.scripts[script] = font
}

// SetLocale registers font for a BCP-47 locale tag (for example "zh-Hans" vs
// "zh-Hant" vs "ja" to pick region-specific Han glyph forms).
func (fs *FontSet[F]) SetLocale(locale string, font F) {
	loc := NormalizeLocale(locale)
	if loc == "" {
		return
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.locales[loc] = font
}

// ForScript returns the font registered for script, or the default font if none
// is set.
func (fs *FontSet[F]) ForScript(script Script) F {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if f, ok := fs.scripts[script]; ok {
		return f
	}
	if script == ScriptJapanese {
		if f, ok := fs.scripts[ScriptCJK]; ok {
			return f
		}
	}
	return fs.defaultFont
}

// ForLocale selects the best font for locale by walking its BCP-47 fallback
// chain in registered locale fonts, then falling back to the font registered
// for LocaleScript(locale), and finally the default font.
func (fs *FontSet[F]) ForLocale(locale string) F {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.forLocaleLocked(locale)
}

// ForText selects the best font for rendering text in locale.
//
// If text's dominant script matches locale's script, locale-specific overrides
// (such as "zh-Hant" vs "zh-Hans") take precedence over generic script fonts.
// Otherwise the font registered for text's detected script is selected.
func (fs *FontSet[F]) ForText(locale, text string) F {
	sc := DetectScript(text)
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.forScriptLocked(locale, sc)
}

// RunsForText segments text by script and attaches the resolved font for each
// run, enabling mixed-script rendering (for example Latin + CJK or Arabic).
func (fs *FontSet[F]) RunsForText(locale, text string) []FontRun[F] {
	segRuns := SegmentByScript(text)
	if len(segRuns) == 0 {
		return nil
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := make([]FontRun[F], len(segRuns))
	for i, r := range segRuns {
		out[i] = FontRun[F]{
			Text:   r.Text,
			Script: r.Script,
			Font:   fs.forScriptLocked(locale, r.Script),
			Start:  r.Start,
			End:    r.End,
		}
	}
	return out
}

func (fs *FontSet[F]) forLocaleLocked(locale string) F {
	if f, ok := fs.lookupLocaleLocked(locale); ok {
		return f
	}
	sc := LocaleScript(locale)
	if f, ok := fs.scripts[sc]; ok {
		return f
	}
	if sc == ScriptJapanese {
		if f, ok := fs.scripts[ScriptCJK]; ok {
			return f
		}
	}
	return fs.defaultFont
}

func (fs *FontSet[F]) forScriptLocked(locale string, script Script) F {
	if script == ScriptCommon {
		return fs.forLocaleLocked(locale)
	}
	if localeMatchesScript(locale, script) {
		if f, ok := fs.lookupLocaleLocked(locale); ok {
			return f
		}
	}
	if f, ok := fs.scripts[script]; ok {
		return f
	}
	if script == ScriptJapanese {
		if f, ok := fs.scripts[ScriptCJK]; ok {
			return f
		}
	}
	return fs.defaultFont
}

func (fs *FontSet[F]) lookupLocaleLocked(locale string) (F, bool) {
	for _, cand := range localeFallbackChain(locale, "") {
		if f, ok := fs.locales[cand]; ok {
			return f, true
		}
	}
	var zero F
	return zero, false
}
