package a11y

import (
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// DefaultMaxVisibleCaptions is the default cap on simultaneously active captions in CaptionTrack.
	DefaultMaxVisibleCaptions = 4
	// DefaultCaptionDuration is used when a Caption is shown without a positive Duration.
	DefaultCaptionDuration = 3 * time.Second
	// DefaultCaptionMaxLines is the default maximum number of wrapped lines shown at once in CaptionsView (XAG 104).
	DefaultCaptionMaxLines = 2
	// DefaultCaptionMaxChars is the default maximum number of characters per wrapped line (XAG 104).
	DefaultCaptionMaxChars = 40
	// DefaultSpeakerRepeatGap is the default silence gap after which ShowDialogue shows a repeated speaker's name again.
	DefaultSpeakerRepeatGap = 5 * time.Second
	// directionalThreshold is the stereo pan magnitude at which [<] / [>] cues appear.
	directionalThreshold float32 = 0.25
)

// CaptionKind classifies a live caption entry for styling, filtering, and
// screen-reader politeness.
type CaptionKind int

const (
	// CaptionDialogue is spoken character dialogue.
	CaptionDialogue CaptionKind = iota
	// CaptionSFX is a non-speech sound effect cue (footsteps, explosion, door).
	CaptionSFX
	// CaptionMusic is a musical cue or track mood indicator.
	CaptionMusic
	// CaptionImportant is a critical gameplay or hazard audio cue announced assertively.
	CaptionImportant
	// CaptionChatter is ambient or background NPC speech shown separately from main dialogue.
	CaptionChatter
)

// String returns the canonical name of k.
func (k CaptionKind) String() string {
	switch k {
	case CaptionDialogue:
		return "dialogue"
	case CaptionSFX:
		return "sfx"
	case CaptionMusic:
		return "music"
	case CaptionImportant:
		return "important"
	case CaptionChatter:
		return "chatter"
	default:
		return "dialogue"
	}
}

// Caption represents one active timed caption on a CaptionTrack.
type Caption struct {
	// ID uniquely identifies the caption within its track.
	ID uint64
	// Speaker is the optional speaker name (e.g. "Ada").
	Speaker string
	// Qualifier is an optional delivery qualifier shown beside Speaker (e.g. "on radio" -> "Ada (on radio): ...").
	Qualifier string
	// OmitSpeaker suppresses the speaker prefix when formatting consecutive lines from the same speaker.
	OmitSpeaker bool
	// Text is the caption body.
	Text string
	// Kind classifies the cue for formatting, filtering, and LiveRegion priority.
	Kind CaptionKind
	// Direction is an optional stereo pan hint in [-1, +1] (-1 left, +1 right).
	Direction float32
	// Duration is the total lifetime of the caption.
	Duration time.Duration
	// Remaining is the time left before the caption expires.
	Remaining time.Duration
}

func formatQualifier(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	if (strings.HasPrefix(q, "(") && strings.HasSuffix(q, ")")) ||
		(strings.HasPrefix(q, "（") && strings.HasSuffix(q, "）")) {
		return q
	}
	return "(" + q + ")"
}

// FormatCaptionParts splits a formatted caption into its directional left prefix,
// speaker/qualifier prefix, body text, and directional right suffix so renderers
// can tint the speaker portion independently while preserving exact plain-text formatting.
func FormatCaptionParts(c Caption, importantPrefix, musicPrefix string) (prefix, speakerPart, body, suffix string) {
	if importantPrefix == "" {
		importantPrefix = defaultCaptionImportantPrefix
	}
	if musicPrefix == "" {
		musicPrefix = defaultCaptionMusicPrefix
	}

	if c.Direction <= -directionalThreshold {
		prefix = "[<] "
	} else if c.Direction >= directionalThreshold {
		suffix = " [>]"
	}

	qual := formatQualifier(c.Qualifier)
	switch {
	case c.Speaker != "":
		if !c.OmitSpeaker {
			if qual != "" {
				speakerPart = c.Speaker + " " + qual + ": "
			} else {
				speakerPart = c.Speaker + ": "
			}
		}
		body = c.Text
	case c.Kind == CaptionSFX:
		if strings.HasPrefix(c.Text, "[") && strings.HasSuffix(c.Text, "]") {
			body = c.Text
		} else {
			body = "[" + c.Text + "]"
		}
	case c.Kind == CaptionMusic:
		body = musicPrefix + c.Text
	case c.Kind == CaptionImportant:
		body = importantPrefix + c.Text
	default:
		if qual != "" && !c.OmitSpeaker {
			speakerPart = qual + " "
		}
		body = c.Text
	}
	return prefix, speakerPart, body, suffix
}

// Format renders c as human- and screen-reader-friendly text with speaker,
// qualifier, and directional stereo indicators ([<] for left, [>] for right).
func (c Caption) Format() string {
	return c.formatWith(defaultCaptionImportantPrefix, defaultCaptionMusicPrefix)
}

func (c Caption) formatWith(importantPrefix, musicPrefix string) string {
	prefix, speakerPart, body, suffix := FormatCaptionParts(c, importantPrefix, musicPrefix)
	return prefix + speakerPart + body + suffix
}

// WrapCaptionLines wraps text into lines of at most maxChars characters
// (defaulting to DefaultCaptionMaxChars = 40 when maxChars <= 0).
// Space-separated text wraps at word boundaries; CJK ideographs and Kana/Hangul
// wrap by character, and oversized words break at maxChars so no line exceeds the limit.
func WrapCaptionLines(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = DefaultCaptionMaxChars
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rawParas := strings.Split(text, "\n")
	var out []string
	for _, para := range rawParas {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		out = append(out, wrapCaptionParagraph(para, maxChars)...)
	}
	if len(out) == 0 && text != "" {
		return []string{text}
	}
	return out
}

type captionToken struct {
	text  string
	runes int
	space bool
	cjk   bool
}

func isCJKWrapRune(r rune) bool {
	if unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) {
		return true
	}
	// CJK symbols & punctuation (U+3000..U+303F) and Fullwidth forms (U+FF00..U+FFEF).
	return (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF)
}

func tokenizeCaptionParagraph(para string) []captionToken {
	var tokens []captionToken
	runes := []rune(para)
	for i := 0; i < len(runes); {
		r := runes[i]
		if unicode.IsSpace(r) {
			start := i
			for i < len(runes) && unicode.IsSpace(runes[i]) {
				i++
			}
			tokens = append(tokens, captionToken{text: " ", runes: 1, space: true})
			_ = start
			continue
		}
		if isCJKWrapRune(r) {
			tokens = append(tokens, captionToken{text: string(r), runes: 1, cjk: true})
			i++
			continue
		}
		start := i
		for i < len(runes) && !unicode.IsSpace(runes[i]) && !isCJKWrapRune(runes[i]) {
			i++
		}
		seg := string(runes[start:i])
		tokens = append(tokens, captionToken{text: seg, runes: i - start})
	}
	return tokens
}

func wrapCaptionParagraph(para string, maxChars int) []string {
	tokens := tokenizeCaptionParagraph(para)
	var lines []string
	var cur strings.Builder
	curLen := 0
	pendingSpace := false

	flush := func() {
		if curLen > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curLen = 0
		}
		pendingSpace = false
	}

	for _, tok := range tokens {
		if tok.space {
			if curLen > 0 {
				pendingSpace = true
			}
			continue
		}

		spaceCost := 0
		if pendingSpace && curLen > 0 {
			spaceCost = 1
		}

		if curLen+spaceCost+tok.runes <= maxChars {
			if spaceCost == 1 {
				cur.WriteByte(' ')
				curLen++
			}
			cur.WriteString(tok.text)
			curLen += tok.runes
			pendingSpace = false
			continue
		}

		// Does not fit on current line: flush current line first if non-empty.
		if curLen > 0 {
			flush()
		}

		// If the token itself fits on a fresh line, place it there.
		if tok.runes <= maxChars {
			cur.WriteString(tok.text)
			curLen = tok.runes
			pendingSpace = false
			continue
		}

		// Single word exceeds maxChars: split cleanly by rune count.
		wordRunes := []rune(tok.text)
		for len(wordRunes) > 0 {
			take := min(maxChars, len(wordRunes))
			if take < len(wordRunes) {
				lines = append(lines, string(wordRunes[:take]))
			} else {
				cur.WriteString(string(wordRunes[:take]))
				curLen = take
			}
			wordRunes = wordRunes[take:]
		}
		pendingSpace = false
	}
	flush()
	return lines
}

// ResolveCaptionBackground converts a canonical caption background preset name
// ("black", "gray", "blue", "white") or "#rrggbb" hex string plus opacity in [0, 1]
// into a core.Color.
func ResolveCaptionBackground(name string, opacity float64) core.Color {
	if math.IsNaN(opacity) {
		opacity = save.DefaultCaptionBackgroundOpacity
	}
	opacity = clamp01(opacity)
	norm := save.NormalizeCaptionBackground(name)
	rgb := uint32(0x0B0D12)
	switch norm {
	case save.CaptionBackgroundBlack:
		rgb = 0x0B0D12
	case save.CaptionBackgroundGray:
		rgb = 0x2B303A
	case save.CaptionBackgroundBlue:
		rgb = 0x10213D
	case save.CaptionBackgroundWhite:
		rgb = 0xF8FAFC
	default:
		if parsed, ok := parseHexRGB(norm); ok {
			rgb = parsed
		}
	}
	return core.RGBA(rgb, float32(opacity))
}

// ResolveCaptionTextColor converts a canonical caption text color preset name
// ("white", "yellow", "cyan", "green", "black") or "#rrggbb" hex string into a core.Color.
func ResolveCaptionTextColor(name string) core.Color {
	norm := save.NormalizeCaptionTextColor(name)
	switch norm {
	case save.CaptionTextYellow:
		return core.RGB(0xFFE55C)
	case save.CaptionTextCyan:
		return core.RGB(0x9BE7FF)
	case save.CaptionTextGreen:
		return core.RGB(0x98F5A6)
	case save.CaptionTextBlack:
		return core.RGB(0x0B0D12)
	case save.CaptionTextWhite:
		return core.RGB(0xFFFFFF)
	default:
		if parsed, ok := parseHexRGB(norm); ok {
			return core.RGB(parsed)
		}
		return core.RGB(0xFFFFFF)
	}
}

func parseHexRGB(s string) (uint32, bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

// SpeakerColor returns a deterministic, color-vision-aware highlight color for speaker.
// Explicit entries in overrides take precedence (checked exact match first, then case-insensitive).
func SpeakerColor(speaker string, mode ColorVision, overrides map[string]core.Color) core.Color {
	trimmed := strings.TrimSpace(speaker)
	if trimmed == "" {
		return PaletteFor(mode).Highlight
	}
	if len(overrides) > 0 {
		if c, ok := overrides[trimmed]; ok && c != 0 {
			return c
		}
		for k, c := range overrides {
			if c != 0 && strings.EqualFold(strings.TrimSpace(k), trimmed) {
				return c
			}
		}
	}
	p := PaletteFor(mode)
	choices := [...]core.Color{
		p.Player,
		p.Highlight,
		p.Ally,
		p.Pickup,
		p.Accent,
		p.Enemy,
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(trimmed)))
	return choices[int(h.Sum32())%len(choices)]
}

// CaptionTrack manages timed captions stepped deterministically by Update(dt)
// inside game.Loop.Update, exposing a reactive signal revision for CaptionsView.
type CaptionTrack struct {
	// MaxVisible caps how many captions remain active at once; 4 when <= 0.
	MaxVisible int
	// SpeakerRepeatGap controls how long a silence pause must last before
	// ShowDialogue repeats the same speaker's name. Zero defaults to
	// DefaultSpeakerRepeatGap (5s); negative values disable suppression so
	// speaker names are always shown.
	SpeakerRepeatGap time.Duration

	nextID         uint64
	captions       []Caption
	clock          time.Duration
	lastSpeaker    string
	lastQualifier  string
	lastSpeakerEnd time.Duration
	lastSpeakerID  uint64
	version        uint64
	rev            *core.Signal[uint64]
}

// NewCaptionTrack returns a CaptionTrack bound to cx's reactive graph with
// MaxVisible set to DefaultMaxVisibleCaptions (4).
func NewCaptionTrack(cx *core.Scope) *CaptionTrack {
	return &CaptionTrack{
		MaxVisible: DefaultMaxVisibleCaptions,
		rev:        core.NewSignal(cx, uint64(0)),
	}
}

func (t *CaptionTrack) maxVisible() int {
	if t.MaxVisible > 0 {
		return t.MaxVisible
	}
	return DefaultMaxVisibleCaptions
}

func (t *CaptionTrack) speakerRepeatGap() (time.Duration, bool) {
	if t.SpeakerRepeatGap < 0 {
		return 0, false
	}
	if t.SpeakerRepeatGap == 0 {
		return DefaultSpeakerRepeatGap, true
	}
	return t.SpeakerRepeatGap, true
}

func (t *CaptionTrack) shouldOmitSpeaker(speaker, qualifier string) bool {
	gap, enabled := t.speakerRepeatGap()
	if !enabled {
		return false
	}
	sp := strings.TrimSpace(speaker)
	if sp == "" || t.lastSpeaker == "" {
		return false
	}
	if !strings.EqualFold(sp, t.lastSpeaker) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(qualifier), t.lastQualifier) {
		return false
	}
	silence := t.clock - t.lastSpeakerEnd
	if silence < 0 {
		silence = 0
	}
	return silence <= gap
}

func (t *CaptionTrack) bump() {
	t.version++
	if t.rev != nil {
		t.rev.Set(t.version)
	}
}

// Show adds c to the track, assigning an ID and defaults if unset, enforcing
// MaxVisible, and notifying reactive subscribers. It returns the caption's ID.
func (t *CaptionTrack) Show(c Caption) uint64 {
	if c.ID == 0 {
		t.nextID++
		c.ID = t.nextID
	} else if c.ID > t.nextID {
		t.nextID = c.ID
	}
	if c.Duration <= 0 {
		c.Duration = DefaultCaptionDuration
	}
	if c.Remaining <= 0 {
		c.Remaining = c.Duration
	}
	if math.IsNaN(float64(c.Direction)) || math.IsInf(float64(c.Direction), 0) {
		c.Direction = 0
	} else {
		c.Direction = clampFloat32(c.Direction, -1, 1)
	}

	if c.Kind == CaptionDialogue && strings.TrimSpace(c.Speaker) != "" {
		t.lastSpeaker = strings.TrimSpace(c.Speaker)
		t.lastQualifier = strings.TrimSpace(c.Qualifier)
		t.lastSpeakerEnd = t.clock + c.Remaining
		t.lastSpeakerID = c.ID
	}

	t.captions = append(t.captions, c)
	limit := t.maxVisible()
	for len(t.captions) > limit {
		evict := 0
		// Prefer evicting the oldest non-important caption so critical cues stay visible.
		if t.captions[0].Kind == CaptionImportant {
			for i := 1; i < len(t.captions); i++ {
				if t.captions[i].Kind != CaptionImportant {
					evict = i
					break
				}
			}
		}
		t.captions = append(t.captions[:evict], t.captions[evict+1:]...)
	}
	t.bump()
	return c.ID
}

// ShowSFX appends a sound-effect caption for label lasting duration.
func (t *CaptionTrack) ShowSFX(label string, duration time.Duration) uint64 {
	return t.Show(Caption{
		Text:     label,
		Kind:     CaptionSFX,
		Duration: duration,
	})
}

// ShowSFXAt appends a directional sound-effect caption with stereo direction in [-1, +1].
func (t *CaptionTrack) ShowSFXAt(label string, direction float32, duration time.Duration) uint64 {
	return t.Show(Caption{
		Text:      label,
		Kind:      CaptionSFX,
		Direction: direction,
		Duration:  duration,
	})
}

// ShowDialogue appends a spoken dialogue caption attributed to speaker,
// suppressing the repeated speaker name if the same speaker spoke within
// SpeakerRepeatGap without an intervening speaker change.
func (t *CaptionTrack) ShowDialogue(speaker, text string, duration time.Duration) uint64 {
	return t.ShowDialogueWithQualifier(speaker, "", text, duration)
}

// ShowDialogueWithQualifier appends a spoken dialogue caption attributed to
// speaker with an optional delivery qualifier (such as "on radio"), suppressing
// the repeated speaker name only when both speaker and qualifier match within
// SpeakerRepeatGap.
func (t *CaptionTrack) ShowDialogueWithQualifier(speaker, qualifier, text string, duration time.Duration) uint64 {
	omit := t.shouldOmitSpeaker(speaker, qualifier)
	return t.Show(Caption{
		Speaker:     speaker,
		Qualifier:   qualifier,
		OmitSpeaker: omit,
		Text:        text,
		Kind:        CaptionDialogue,
		Duration:    duration,
	})
}

// ShowChatter appends an ambient or background character chatter caption.
func (t *CaptionTrack) ShowChatter(speaker, text string, duration time.Duration) uint64 {
	return t.Show(Caption{
		Speaker:  speaker,
		Text:     text,
		Kind:     CaptionChatter,
		Duration: duration,
	})
}

// ShowImportant appends a high-priority gameplay cue announced assertively.
func (t *CaptionTrack) ShowImportant(text string, duration time.Duration) uint64 {
	return t.Show(Caption{
		Text:     text,
		Kind:     CaptionImportant,
		Duration: duration,
	})
}

// Update advances the track's logical clock and all active captions by dt,
// expiring any whose Remaining time reaches zero. Intended to be called on each
// game.Loop.Update step.
func (t *CaptionTrack) Update(dt time.Duration) {
	if dt <= 0 {
		return
	}
	t.clock += dt
	if len(t.captions) == 0 {
		return
	}
	kept := t.captions[:0]
	expired := false
	for _, c := range t.captions {
		c.Remaining -= dt
		if c.Remaining > 0 {
			kept = append(kept, c)
		} else {
			expired = true
		}
	}
	for i := len(kept); i < len(t.captions); i++ {
		t.captions[i] = Caption{}
	}
	t.captions = kept
	if expired {
		t.bump()
	}
}

// Active returns a snapshot of the currently active captions in display order
// (oldest to newest). Calling Active inside a reactive computation subscribes
// it to caption additions, expirations, and clears.
func (t *CaptionTrack) Active() []Caption {
	if t.rev != nil {
		_ = t.rev.Get()
	}
	if len(t.captions) == 0 {
		return nil
	}
	out := make([]Caption, len(t.captions))
	copy(out, t.captions)
	return out
}

// Dismiss removes the caption with id if present and reports whether one was removed.
func (t *CaptionTrack) Dismiss(id uint64) bool {
	for i, c := range t.captions {
		if c.ID == id {
			if c.ID == t.lastSpeakerID && t.lastSpeakerEnd > t.clock {
				t.lastSpeakerEnd = t.clock
			}
			t.captions = append(t.captions[:i], t.captions[i+1:]...)
			t.bump()
			return true
		}
	}
	return false
}

// Clear removes all active captions and resets speaker-repeat tracking.
func (t *CaptionTrack) Clear() {
	t.lastSpeaker = ""
	t.lastQualifier = ""
	t.lastSpeakerEnd = 0
	t.lastSpeakerID = 0
	if len(t.captions) == 0 {
		return
	}
	t.captions = nil
	t.bump()
}

// CaptionStyle configures the appearance and optional Assist integration of CaptionsView.
type CaptionStyle struct {
	// Background is the caption pill backdrop color when Assist is nil (default high-contrast near-black at 90% opacity).
	Background core.Color
	// TextColor is the dialogue/music text color when Assist is nil (default white).
	TextColor core.Color
	// SFXColor is the sound-effect caption text color (default soft cyan).
	SFXColor core.Color
	// ImportantColor is the text color for CaptionImportant cues (default high-contrast yellow).
	ImportantColor core.Color
	// ChatterColor overrides the text color for CaptionChatter cues when non-zero.
	ChatterColor core.Color
	// SpeakerColors supplies optional per-speaker color overrides keyed by speaker name.
	SpeakerColors map[string]core.Color
	// EnableSpeakerColors enables per-speaker name coloring when Assist is nil.
	// When Assist is non-nil, Assist.CaptionSpeakerColors() takes precedence.
	EnableSpeakerColors bool
	// FontSize is the base font size in points (default 16).
	FontSize core.Points
	// TextScale multiplies FontSize when > 0 (overridden by Assist.TextScale when Assist is set).
	TextScale float32
	// MaxWidth caps the caption overlay width in points (default 640).
	MaxWidth core.Points
	// MaxLines caps the total number of wrapped caption lines shown simultaneously
	// across all visible captions (default DefaultCaptionMaxLines = 2; older lines leave first).
	MaxLines int
	// MaxChars caps the number of characters per wrapped line (default DefaultCaptionMaxChars = 40).
	MaxChars int
	// Assist, when non-nil, automatically applies player caption settings
	// (background color & opacity, text color, speaker colors, size preset, per-kind
	// toggles, HighContrast, ColorVision, and TextScale).
	Assist *Assist
	// RegionLabel overrides the accessible label of the captions live region
	// (default "Captions").
	RegionLabel string
	// ImportantPrefix overrides the prefix prepended to speakerless
	// CaptionImportant cues (default "! ").
	ImportantPrefix string
	// MusicPrefix overrides the prefix prepended to speakerless CaptionMusic
	// cues (default "♪ ").
	MusicPrefix string
	// TestID overrides the root view's TestID (default "a11y-captions").
	TestID string
	// Style merges additional layout/positioning style onto the container.
	Style core.Style
}

// FormatCaption renders c using s's ImportantPrefix and MusicPrefix, falling
// back to "! " and "♪ " when unset.
func (s CaptionStyle) FormatCaption(c Caption) string {
	imp := s.ImportantPrefix
	if imp == "" {
		imp = defaultCaptionImportantPrefix
	}
	mus := s.MusicPrefix
	if mus == "" {
		mus = defaultCaptionMusicPrefix
	}
	return c.formatWith(imp, mus)
}

// ColorForSpeaker returns the display color for speaker under s (and Assist.ColorVision when set).
func (s CaptionStyle) ColorForSpeaker(speaker string) core.Color {
	mode := VisionNormal
	if s.Assist != nil {
		if s.Assist.HighContrast() {
			mode = VisionHighContrast
		} else {
			mode = s.Assist.ColorVision()
		}
	}
	return SpeakerColor(speaker, mode, s.SpeakerColors)
}

type renderedCaption struct {
	caption     Caption
	text        string
	prefix      string
	speakerPart string
	rest        string
	hasSpeaker  bool
}

func layoutVisibleCaptions(captions []Caption, opts CaptionStyle) []renderedCaption {
	if len(captions) == 0 {
		return nil
	}
	maxLines := opts.MaxLines
	if maxLines <= 0 {
		maxLines = DefaultCaptionMaxLines
	}
	maxChars := opts.MaxChars
	if maxChars <= 0 {
		maxChars = DefaultCaptionMaxChars
	}

	type wrappedEntry struct {
		caption     Caption
		lines       []string
		prefix      string
		speakerPart string
		droppedTop  int
	}

	entries := make([]wrappedEntry, 0, len(captions))
	totalLines := 0
	for _, c := range captions {
		prefix, speakerPart, body, suffix := FormatCaptionParts(c, opts.ImportantPrefix, opts.MusicPrefix)
		full := prefix + speakerPart + body + suffix
		lines := WrapCaptionLines(full, maxChars)
		if len(lines) == 0 {
			continue
		}
		entries = append(entries, wrappedEntry{
			caption:     c,
			lines:       lines,
			prefix:      prefix,
			speakerPart: speakerPart,
		})
		totalLines += len(lines)
	}

	// Older lines leave first so total visible lines never exceed maxLines.
	excess := totalLines - maxLines
	for excess > 0 && len(entries) > 0 {
		if len(entries[0].lines) <= excess {
			excess -= len(entries[0].lines)
			entries = entries[1:]
		} else {
			entries[0].lines = entries[0].lines[excess:]
			entries[0].droppedTop += excess
			excess = 0
		}
	}

	out := make([]renderedCaption, 0, len(entries))
	for _, e := range entries {
		joined := strings.Join(e.lines, "\n")
		rc := renderedCaption{
			caption: e.caption,
			text:    joined,
		}
		if e.droppedTop == 0 && e.speakerPart != "" {
			if strings.HasPrefix(joined, e.prefix+e.speakerPart) {
				rc.prefix = e.prefix
				rc.speakerPart = e.speakerPart
				rc.rest = joined[len(e.prefix)+len(e.speakerPart):]
				rc.hasSpeaker = true
			}
		}
		out = append(out, rc)
	}
	return out
}

// CaptionsView renders high-contrast live captions from track as native NSGo
// views with LiveRegionPolite on standard cues and LiveRegionAssertive on
// CaptionImportant cues, applying player caption preferences, per-kind toggles,
// speaker coloring, and the two-line / 40-character wrapping limit.
func CaptionsView(cx *core.Scope, track *CaptionTrack, opts CaptionStyle) core.Node {
	if track == nil {
		return core.View{}
	}
	if track.rev == nil {
		track.rev = core.NewSignal(cx, track.version)
	}

	testID := opts.TestID
	if testID == "" {
		testID = "a11y-captions"
	}
	baseBg := opts.Background
	if baseBg == 0 {
		baseBg = core.RGBA(0x0B0D12, float32(save.DefaultCaptionBackgroundOpacity))
	}
	baseFg := opts.TextColor
	if baseFg == 0 {
		baseFg = core.RGB(0xFFFFFF)
	}
	baseSFX := opts.SFXColor
	if baseSFX == 0 {
		baseSFX = core.RGB(0x9BE7FF)
	}
	baseImportant := opts.ImportantColor
	if baseImportant == 0 {
		baseImportant = core.RGB(0xFFE55C)
	}
	baseFontSize := opts.FontSize
	if baseFontSize <= 0 {
		baseFontSize = 16
	}
	maxWidth := opts.MaxWidth
	if maxWidth <= 0 {
		maxWidth = 640
	}

	resolveBg := func() (core.Color, bool) {
		if opts.Assist != nil {
			if opts.Assist.HighContrast() {
				return PaletteFor(VisionHighContrast).Background, true
			}
			cs := opts.Assist.CaptionSettings()
			return ResolveCaptionBackground(cs.Background, cs.EffectiveBackgroundOpacity()), false
		}
		return baseBg, false
	}

	resolveColors := func(c Caption, bg core.Color, hc bool) (fg core.Color, spk core.Color, useSpeakerColor bool, weight core.FontWeight) {
		fg = baseFg
		if opts.Assist != nil {
			cs := opts.Assist.CaptionSettings()
			fg = ResolveCaptionTextColor(cs.TextColor)
			useSpeakerColor = cs.EffectiveSpeakerColors()
		} else {
			useSpeakerColor = opts.EnableSpeakerColors || len(opts.SpeakerColors) > 0
		}

		weight = core.FontWeightMedium
		switch c.Kind {
		case CaptionSFX:
			fg = baseSFX
		case CaptionImportant:
			fg = baseImportant
			weight = core.FontWeightBold
		case CaptionChatter:
			if opts.ChatterColor != 0 {
				fg = opts.ChatterColor
			}
		}

		bgAlpha := float64(bg&0xff) / 255.0
		if hc || bgAlpha >= 0.5 {
			opaqueBg := bg.Over(core.RGB(0x000000))
			fg = EnsureContrast(fg, opaqueBg, 7.0)
			if useSpeakerColor && c.Speaker != "" {
				spk = EnsureContrast(opts.ColorForSpeaker(c.Speaker), opaqueBg, 4.5)
			}
		} else if useSpeakerColor && c.Speaker != "" {
			spk = opts.ColorForSpeaker(c.Speaker)
		}
		return fg, spk, useSpeakerColor, weight
	}

	visibleItems := func() []renderedCaption {
		all := track.Active()
		if len(all) == 0 {
			return nil
		}
		filtered := all
		if opts.Assist != nil {
			showCaptions := opts.Assist.CaptionsEnabled()
			showDialogue := showCaptions && opts.Assist.DialogueCaptionsEnabled()
			showChatter := showCaptions && opts.Assist.ChatterCaptionsEnabled()
			showSounds := opts.Assist.SFXCaptionsEnabled()
			filtered = make([]Caption, 0, len(all))
			for _, c := range all {
				switch c.Kind {
				case CaptionImportant:
					if showDialogue || showChatter || showSounds {
						filtered = append(filtered, c)
					}
				case CaptionSFX, CaptionMusic:
					if showSounds {
						filtered = append(filtered, c)
					}
				case CaptionChatter:
					if showChatter {
						filtered = append(filtered, c)
					}
				default: // CaptionDialogue
					if showDialogue {
						filtered = append(filtered, c)
					}
				}
			}
		}
		return layoutVisibleCaptions(filtered, opts)
	}

	containerStyle := core.Style{
		Direction:  core.Column,
		AlignItems: core.AlignCenter,
		Gap:        6,
		MaxWidth:   core.Pt(maxWidth),
		Padding:    core.Edges{Top: 8, Right: 12, Bottom: 8, Left: 12},
	}
	containerStyle = containerStyle.Merge(opts.Style)

	regionLabel := opts.RegionLabel
	if regionLabel == "" {
		regionLabel = defaultCaptionRegionLabel
	}

	return core.View{
		TestID:        testID,
		Role:          core.RoleRegion,
		Label:         regionLabel,
		LiveRegion:    core.LiveRegionPolite,
		PointerEvents: core.PointerEventsNone,
		Style:         containerStyle,
		Children: []core.Node{
			core.Each(visibleItems, func(rc renderedCaption) uint64 { return rc.caption.ID }, func(cx *core.Scope, item func() renderedCaption, _ func() int) core.Node {
				c := item().caption
				live := core.LiveRegionPolite
				role := core.RoleStatus
				if c.Kind == CaptionImportant {
					live = core.LiveRegionAssertive
					role = core.RoleAlert
				}

				rowStyle := func() core.Style {
					bg, hc := resolveBg()
					border := core.Transparent
					borderW := core.Points(0)
					if hc {
						p := PaletteFor(VisionHighContrast)
						border = p.Foreground
						borderW = 2
					}
					if c.Kind == CaptionImportant {
						border = EnsureContrast(baseImportant, bg.Over(core.RGB(0x000000)), 4.5)
						borderW = 2
					}
					return core.Style{
						Background:   bg,
						CornerRadius: 6,
						BorderWidth:  borderW,
						BorderColor:  border,
						Padding:      core.Edges{Top: 6, Right: 12, Bottom: 6, Left: 12},
					}
				}

				textStyle := func() core.Style {
					bg, hc := resolveBg()
					fg, _, _, weight := resolveColors(c, bg, hc)

					scale := opts.TextScale
					if scale <= 0 {
						scale = 1.0
					}
					sizeScale := float32(1.0)
					if opts.Assist != nil {
						scale = opts.Assist.TextScale()
						sizeScale = float32(save.CaptionSizeScale(opts.Assist.CaptionSize()))
					}
					st := core.Style{
						Color:      fg,
						FontSize:   baseFontSize * core.Points(scale*sizeScale),
						FontWeight: weight,
						TextAlign:  core.TextAlignCenter,
					}
					if !hc && float64(bg&0xff)/255.0 < 0.5 {
						st.TextShadowColor = core.RGBA(0x000000, 0.85)
						st.TextShadowOffset = core.Point{X: 1, Y: 1}
						st.TextShadowRadius = 2
					}
					return st
				}

				spansFunc := func() []core.Span {
					rc := item()
					bg, hc := resolveBg()
					fg, spk, useSpeakerColor, _ := resolveColors(rc.caption, bg, hc)
					if !useSpeakerColor || !rc.hasSpeaker || spk == 0 {
						return []core.Span{{Content: rc.text}}
					}
					spans := make([]core.Span, 0, 3)
					if rc.prefix != "" {
						spans = append(spans, core.Span{Content: rc.prefix, Style: core.Style{Color: fg}})
					}
					spans = append(spans, core.Span{
						Content: rc.speakerPart,
						Style: core.Style{
							Color:      spk,
							FontWeight: core.FontWeightBold,
						},
					})
					if rc.rest != "" {
						spans = append(spans, core.Span{Content: rc.rest, Style: core.Style{Color: fg}})
					}
					return spans
				}

				return core.View{
					TestID:     fmt.Sprintf("%s-item-%d", testID, c.ID),
					Role:       role,
					LiveRegion: live,
					StyleFunc:  rowStyle,
					Children: []core.Node{
						core.Text{
							SpansFunc:  spansFunc,
							LiveRegion: live,
							StyleFunc:  textStyle,
						},
					},
				}
			}),
		},
	}
}
