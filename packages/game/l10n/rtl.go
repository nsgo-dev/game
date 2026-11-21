package l10n

import (
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/nsgo/core"
)

// Direction specifies horizontal text and layout reading direction.
type Direction uint8

const (
	// DirLTR is left-to-right direction (Latin, CJK, Cyrillic, etc.).
	DirLTR Direction = iota
	// DirRTL is right-to-left direction (Arabic, Hebrew, Persian, Urdu, etc.).
	DirRTL
)

// String returns "ltr" or "rtl".
func (d Direction) String() string {
	if d == DirRTL {
		return "rtl"
	}
	return "ltr"
}

// IsRTL reports whether d is DirRTL.
func (d Direction) IsRTL() bool {
	return d == DirRTL
}

// LocaleDirection returns the default writing direction for a BCP-47 locale
// tag. Explicit script subtags (such as "ar-Latn" vs "az-Arab") take precedence
// over the primary language subtag.
func LocaleDirection(locale string) Direction {
	if sub := scriptSubtag(locale); sub != "" {
		switch sub {
		case "Arab", "Hebr", "Syrc", "Thaa", "Nkoo", "Adlm", "Mand", "Samr", "Rohg", "Yezi", "Mend", "Chrs":
			return DirRTL
		default:
			return DirLTR
		}
	}

	switch primaryLanguage(locale) {
	case "ar", "he", "iw", "fa", "ur", "yi", "ji", "ps", "sd", "ug",
		"dv", "syr", "ckb", "ku", "arc", "nqo", "prs", "ks", "khw",
		"pnb", "glk", "mzn", "lrc", "bgn", "bal":
		return DirRTL
	default:
		return DirLTR
	}
}

// EnvDirection returns the active layout direction for cx, honoring
// cx.Env().RTL() and falling back to LocaleDirection(cx.Env().Locale()).
// Reading EnvDirection inside a reactive computation subscribes to environment
// RTL and locale changes.
func EnvDirection(cx *core.Scope) Direction {
	if cx == nil {
		return DirLTR
	}
	env := cx.Env()
	if env.RTL() {
		return DirRTL
	}
	return LocaleDirection(env.Locale())
}

// TextDirection determines the base direction of s from its first strong
// directional character (UAX #9 rules P2/P3). If s contains no strong
// directional characters, DirLTR is returned.
func TextDirection(s string) Direction {
	for _, r := range s {
		switch classifyBidi(r) {
		case bidiR, bidiAL:
			return DirRTL
		case bidiL:
			return DirLTR
		}
	}
	return DirLTR
}

// VisualOrder converts logical-order text s into visual left-to-right drawing
// order for baseDir, suitable for canvas/bitmap text renderers that draw glyphs
// left-to-right.
//
// Each line separated by '\n' is reordered independently. Embedded LTR words
// and numbers inside RTL lines remain left-to-right, combining marks stay
// attached to their base characters, and paired brackets ((), [], {}, <>, «»,
// ‹›) are mirrored inside RTL runs.
func VisualOrder(s string, baseDir Direction) string {
	if s == "" {
		return ""
	}
	if baseDir == DirLTR && isPureASCII(s) {
		return s
	}
	if !strings.ContainsRune(s, '\n') {
		return reorderLine(s, baseDir)
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = reorderLine(line, baseDir)
	}
	return strings.Join(lines, "\n")
}

// LayoutAlignment adapts a horizontal alignment string ("left", "right",
// "start", "end", "center", "justify") for dir.
//
// In DirLTR, "start" maps to "left" and "end" maps to "right".
// In DirRTL, "left" and "start" map to "right", while "right" and "end" map to
// "left". "center", "justify", and unrecognized values are preserved.
func LayoutAlignment(align string, dir Direction) string {
	norm := strings.ToLower(strings.TrimSpace(align))
	switch norm {
	case "left":
		if dir == DirRTL {
			return "right"
		}
		return "left"
	case "right":
		if dir == DirRTL {
			return "left"
		}
		return "right"
	case "start":
		if dir == DirRTL {
			return "right"
		}
		return "left"
	case "end":
		if dir == DirRTL {
			return "left"
		}
		return "right"
	case "center":
		return "center"
	case "justify":
		return "justify"
	default:
		return align
	}
}

// MirrorX mirrors a horizontal HUD or dialogue element position x (with element
// width inside a container of containerWidth) when dir is DirRTL.
// In DirLTR, x is returned unchanged.
func MirrorX(x, width, containerWidth float32, dir Direction) float32 {
	if dir == DirRTL {
		return containerWidth - x - width
	}
	return x
}

type bidiClass uint8

const (
	bidiL bidiClass = iota
	bidiR
	bidiAL
	bidiEN
	bidiAN
	bidiES
	bidiET
	bidiCS
	bidiNSM
	bidiWS
	bidiON
)

func isPureASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func classifyBidi(r rune) bidiClass {
	switch r {
	case '\u200E': // LRM
		return bidiL
	case '\u200F': // RLM
		return bidiR
	case '\u061C': // ALM
		return bidiAL
	case ' ', '\t':
		return bidiWS
	case '+', '-':
		return bidiES
	case '%', '$', '#', '°', '€', '£', '¥', '¢', '₪', '₩', '₹', '₽':
		return bidiET
	case ',', '.', ':', '/':
		return bidiCS
	}

	if r >= '0' && r <= '9' {
		return bidiEN
	}
	if (r >= 0x0660 && r <= 0x0669) || (r >= 0x06F0 && r <= 0x06F9) {
		return bidiAN
	}

	// Nonspacing and enclosing marks.
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return bidiNSM
	}

	// Strong Arabic letters (AL): Arabic, Syriac, Thaana, Arabic Presentation Forms.
	if unicode.Is(unicode.Arabic, r) ||
		unicode.Is(unicode.Syriac, r) ||
		unicode.Is(unicode.Thaana, r) ||
		(r >= 0xFB50 && r <= 0xFDFF) ||
		(r >= 0xFE70 && r <= 0xFEFF) {
		return bidiAL
	}

	// Strong RTL letters (R): Hebrew, N'Ko, Samaritan, Mandaic, Adlam, etc.
	if unicode.Is(unicode.Hebrew, r) ||
		unicode.Is(unicode.Nko, r) ||
		unicode.Is(unicode.Samaritan, r) ||
		unicode.Is(unicode.Mandaic, r) ||
		unicode.Is(unicode.Adlam, r) ||
		(r >= 0xFB1D && r <= 0xFB4F) {
		return bidiR
	}

	if unicode.Is(unicode.Sc, r) {
		return bidiET
	}
	if unicode.IsSpace(r) {
		return bidiWS
	}
	if unicode.IsLetter(r) {
		return bidiL
	}
	return bidiON
}

func isOpenBracket(r rune) (closing rune, ok bool) {
	switch r {
	case '(':
		return ')', true
	case '[':
		return ']', true
	case '{':
		return '}', true
	case '<':
		return '>', true
	case '«':
		return '»', true
	case '‹':
		return '›', true
	default:
		return 0, false
	}
}

func isCloseBracket(r rune) bool {
	switch r {
	case ')', ']', '}', '>', '»', '›':
		return true
	default:
		return false
	}
}

func mirrorBracket(r rune) rune {
	switch r {
	case '(':
		return ')'
	case ')':
		return '('
	case '[':
		return ']'
	case ']':
		return '['
	case '{':
		return '}'
	case '}':
		return '{'
	case '<':
		return '>'
	case '>':
		return '<'
	case '«':
		return '»'
	case '»':
		return '«'
	case '‹':
		return '›'
	case '›':
		return '‹'
	default:
		return r
	}
}

type bracketPair struct {
	open  int
	close int
}

type bidiCluster struct {
	runes []rune
	level uint8
}

func reorderLine(line string, baseDir Direction) string {
	if line == "" {
		return ""
	}
	if baseDir == DirLTR && isPureASCII(line) {
		return line
	}

	rs := []rune(line)
	n := len(rs)
	types := make([]bidiClass, n)
	origNSM := make([]bool, n)
	origWS := make([]bool, n)

	for i, r := range rs {
		c := classifyBidi(r)
		types[i] = c
		origNSM[i] = (c == bidiNSM)
		origWS[i] = (c == bidiWS)
	}

	embedClass := bidiL
	oppositeClass := bidiR
	var baseLevel uint8 = 0
	if baseDir == DirRTL {
		embedClass = bidiR
		oppositeClass = bidiL
		baseLevel = 1
	}

	// W1: Nonspacing marks inherit the type of the preceding character.
	for i := 0; i < n; i++ {
		if types[i] == bidiNSM {
			if i == 0 {
				types[i] = embedClass
			} else {
				types[i] = types[i-1]
			}
		}
	}

	// W2: European numbers preceded by a strong AL become AN.
	lastStrong := embedClass
	for i := 0; i < n; i++ {
		switch types[i] {
		case bidiL, bidiR, bidiAL:
			lastStrong = types[i]
		case bidiEN:
			if lastStrong == bidiAL {
				types[i] = bidiAN
			}
		}
	}

	// W3: AL becomes R.
	for i := 0; i < n; i++ {
		if types[i] == bidiAL {
			types[i] = bidiR
		}
	}

	// W4: Single ES between EN becomes EN; single CS between EN/EN or AN/AN matches.
	for i := 1; i < n-1; i++ {
		if types[i] == bidiES && types[i-1] == bidiEN && types[i+1] == bidiEN {
			types[i] = bidiEN
		} else if types[i] == bidiCS {
			if types[i-1] == bidiEN && types[i+1] == bidiEN {
				types[i] = bidiEN
			} else if types[i-1] == bidiAN && types[i+1] == bidiAN {
				types[i] = bidiAN
			}
		}
	}

	// W5: Sequences of ET adjacent to EN become EN.
	for i := 0; i < n; i++ {
		if types[i] != bidiET {
			continue
		}
		j := i
		for j < n && types[j] == bidiET {
			j++
		}
		adjEN := (i > 0 && types[i-1] == bidiEN) || (j < n && types[j] == bidiEN)
		if adjEN {
			for k := i; k < j; k++ {
				types[k] = bidiEN
			}
		}
		i = j - 1
	}

	// W6: Remaining separators and terminators become ON.
	for i := 0; i < n; i++ {
		if types[i] == bidiES || types[i] == bidiET || types[i] == bidiCS {
			types[i] = bidiON
		}
	}

	// W7: EN preceded by strong L (or sos=L) becomes L.
	lastStrong = embedClass
	for i := 0; i < n; i++ {
		switch types[i] {
		case bidiL, bidiR:
			lastStrong = types[i]
		case bidiEN:
			if lastStrong == bidiL {
				types[i] = bidiL
			}
		}
	}

	// N0: Paired bracket resolution.
	resolvePairedBrackets(rs, types, embedClass, oppositeClass)

	// N1 & N2: Resolve neutral runs (WS, ON).
	for i := 0; i < n; i++ {
		if types[i] != bidiWS && types[i] != bidiON {
			continue
		}
		j := i
		for j < n && (types[j] == bidiWS || types[j] == bidiON) {
			j++
		}
		prev := embedClass
		if i > 0 {
			prev = strongForNeutral(types[i-1], embedClass)
		}
		next := embedClass
		if j < n {
			next = strongForNeutral(types[j], embedClass)
		}
		resolved := embedClass
		if prev == next {
			resolved = prev
		}
		for k := i; k < j; k++ {
			types[k] = resolved
		}
		i = j - 1
	}

	// I1 & I2: Assign implicit embedding levels.
	levels := make([]uint8, n)
	for i := 0; i < n; i++ {
		if baseLevel == 0 {
			switch types[i] {
			case bidiR:
				levels[i] = 1
			case bidiEN, bidiAN:
				levels[i] = 2
			default:
				levels[i] = 0
			}
		} else {
			switch types[i] {
			case bidiL, bidiEN, bidiAN:
				levels[i] = 2
			default:
				levels[i] = 1
			}
		}
	}

	// L1: Reset trailing whitespace to the paragraph base level.
	for i := n - 1; i >= 0 && origWS[i]; i-- {
		levels[i] = baseLevel
	}

	// Group each base rune with any following nonspacing marks into a cluster
	// so reversing an RTL span keeps combining marks attached to their base.
	clusters := make([]bidiCluster, 0, n)
	for i := 0; i < n; {
		j := i + 1
		for j < n && origNSM[j] && levels[j] == levels[i] {
			j++
		}
		clusterRunes := make([]rune, j-i)
		copy(clusterRunes, rs[i:j])
		// L4: Mirror paired brackets at odd embedding levels.
		if levels[i]%2 == 1 && len(clusterRunes) == 1 {
			clusterRunes[0] = mirrorBracket(clusterRunes[0])
		}
		clusters = append(clusters, bidiCluster{
			runes: clusterRunes,
			level: levels[i],
		})
		i = j
	}

	// L2: Reverse contiguous sequences at each level from maxLevel down to minOdd.
	var maxLevel uint8
	var minOdd uint8 = 255
	for _, cl := range clusters {
		if cl.level > maxLevel {
			maxLevel = cl.level
		}
		if cl.level%2 == 1 && cl.level < minOdd {
			minOdd = cl.level
		}
	}
	if maxLevel > 0 && minOdd <= maxLevel {
		for lvl := maxLevel; lvl >= minOdd; lvl-- {
			i := 0
			for i < len(clusters) {
				if clusters[i].level < lvl {
					i++
					continue
				}
				j := i + 1
				for j < len(clusters) && clusters[j].level >= lvl {
					j++
				}
				slices.Reverse(clusters[i:j])
				i = j
			}
			if lvl == 0 {
				break
			}
		}
	}

	var b strings.Builder
	b.Grow(len(line))
	for _, cl := range clusters {
		for _, r := range cl.runes {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func strongForNeutral(c, fallback bidiClass) bidiClass {
	switch c {
	case bidiL:
		return bidiL
	case bidiR, bidiEN, bidiAN:
		return bidiR
	default:
		return fallback
	}
}

func resolvePairedBrackets(rs []rune, types []bidiClass, embedClass, oppositeClass bidiClass) {
	type stackEntry struct {
		index   int
		closing rune
	}
	const maxBracketDepth = 63
	var stack []stackEntry
	var pairs []bracketPair

	for i, r := range rs {
		if types[i] != bidiON {
			continue
		}
		if closing, ok := isOpenBracket(r); ok {
			if len(stack) >= maxBracketDepth {
				break
			}
			stack = append(stack, stackEntry{index: i, closing: closing})
		} else if isCloseBracket(r) {
			for s := len(stack) - 1; s >= 0; s-- {
				if stack[s].closing == r {
					pairs = append(pairs, bracketPair{open: stack[s].index, close: i})
					stack = stack[:s]
					break
				}
			}
		}
	}

	if len(pairs) == 0 {
		return
	}

	// Process pairs in order of opening bracket position (UAX #9 BD16).
	slices.SortFunc(pairs, func(a, b bracketPair) int {
		return a.open - b.open
	})

	for _, p := range pairs {
		hasEmbed := false
		hasOpposite := false
		for i := p.open + 1; i < p.close; i++ {
			st := strongForNeutral(types[i], bidiON)
			if st == embedClass {
				hasEmbed = true
				break
			}
			if st == oppositeClass {
				hasOpposite = true
			}
		}

		var resolved bidiClass
		if hasEmbed {
			resolved = embedClass
		} else if hasOpposite {
			ctx := embedClass
			for i := p.open - 1; i >= 0; i-- {
				st := strongForNeutral(types[i], bidiON)
				if st == bidiL || st == bidiR {
					ctx = st
					break
				}
			}
			if ctx == oppositeClass {
				resolved = oppositeClass
			} else {
				resolved = embedClass
			}
		} else {
			continue
		}

		types[p.open] = resolved
		types[p.close] = resolved
	}
}
