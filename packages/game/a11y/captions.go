package a11y

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	// DefaultMaxVisibleCaptions is the default cap on simultaneously visible captions.
	DefaultMaxVisibleCaptions = 4
	// DefaultCaptionDuration is used when a Caption is shown without a positive Duration.
	DefaultCaptionDuration = 3 * time.Second
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

// Format renders c as human- and screen-reader-friendly text with speaker and
// directional stereo indicators ([<] for left, [>] for right).
func (c Caption) Format() string {
	var body string
	switch {
	case c.Speaker != "":
		body = c.Speaker + ": " + c.Text
	case c.Kind == CaptionSFX:
		if strings.HasPrefix(c.Text, "[") && strings.HasSuffix(c.Text, "]") {
			body = c.Text
		} else {
			body = "[" + c.Text + "]"
		}
	case c.Kind == CaptionMusic:
		body = "♪ " + c.Text
	case c.Kind == CaptionImportant:
		body = "! " + c.Text
	default:
		body = c.Text
	}

	switch {
	case c.Direction <= -directionalThreshold:
		return "[<] " + body
	case c.Direction >= directionalThreshold:
		return body + " [>]"
	default:
		return body
	}
}

// CaptionTrack manages timed captions stepped deterministically by Update(dt)
// inside game.Loop.Update, exposing a reactive signal revision for CaptionsView.
type CaptionTrack struct {
	// MaxVisible caps how many captions remain active at once; 4 when <= 0.
	MaxVisible int

	nextID   uint64
	captions []Caption
	version  uint64
	rev      *core.Signal[uint64]
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

// ShowDialogue appends a spoken dialogue caption attributed to speaker.
func (t *CaptionTrack) ShowDialogue(speaker, text string, duration time.Duration) uint64 {
	return t.Show(Caption{
		Speaker:  speaker,
		Text:     text,
		Kind:     CaptionDialogue,
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

// Update advances all active captions by dt and expires any whose Remaining
// time reaches zero. Intended to be called on each game.Loop.Update step.
func (t *CaptionTrack) Update(dt time.Duration) {
	if dt <= 0 || len(t.captions) == 0 {
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
			t.captions = append(t.captions[:i], t.captions[i+1:]...)
			t.bump()
			return true
		}
	}
	return false
}

// Clear removes all active captions.
func (t *CaptionTrack) Clear() {
	if len(t.captions) == 0 {
		return
	}
	t.captions = nil
	t.bump()
}

// CaptionStyle configures the appearance and optional Assist integration of CaptionsView.
type CaptionStyle struct {
	// Background is the caption pill backdrop color (default high-contrast near-black).
	Background core.Color
	// TextColor is the dialogue/music text color (default white).
	TextColor core.Color
	// SFXColor is the sound-effect caption text color (default soft cyan).
	SFXColor core.Color
	// ImportantColor is the text color for CaptionImportant cues (default high-contrast yellow).
	ImportantColor core.Color
	// FontSize is the base font size in points (default 16).
	FontSize core.Points
	// TextScale multiplies FontSize when > 0 (overridden by Assist.TextScale when Assist is set).
	TextScale float32
	// MaxWidth caps the caption overlay width in points (default 640).
	MaxWidth core.Points
	// Assist, when non-nil, automatically scales caption text, respects
	// HighContrast, and filters captions by CaptionsEnabled/SFXCaptionsEnabled.
	Assist *Assist
	// TestID overrides the root view's TestID (default "a11y-captions").
	TestID string
	// Style merges additional layout/positioning style onto the container.
	Style core.Style
}

// CaptionsView renders high-contrast live captions from track as native NSGo
// views with LiveRegionPolite on standard cues and LiveRegionAssertive on
// CaptionImportant cues.
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
		baseBg = core.RGBA(0x0B0D12, 0.90)
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

	visibleItems := func() []Caption {
		all := track.Active()
		if opts.Assist == nil || len(all) == 0 {
			return all
		}
		showMain := opts.Assist.CaptionsEnabled()
		showSFX := opts.Assist.SFXCaptionsEnabled()
		var filtered []Caption
		for _, c := range all {
			switch c.Kind {
			case CaptionImportant:
				if showMain || showSFX {
					filtered = append(filtered, c)
				}
			case CaptionSFX:
				if showSFX {
					filtered = append(filtered, c)
				}
			default:
				if showMain {
					filtered = append(filtered, c)
				}
			}
		}
		return filtered
	}

	containerStyle := core.Style{
		Direction:  core.Column,
		AlignItems: core.AlignCenter,
		Gap:        6,
		MaxWidth:   core.Pt(maxWidth),
		Padding:    core.Edges{Top: 8, Right: 12, Bottom: 8, Left: 12},
	}
	containerStyle = containerStyle.Merge(opts.Style)

	return core.View{
		TestID:        testID,
		Role:          core.RoleRegion,
		Label:         "Captions",
		LiveRegion:    core.LiveRegionPolite,
		PointerEvents: core.PointerEventsNone,
		Style:         containerStyle,
		Children: []core.Node{
			core.Each(visibleItems, func(c Caption) uint64 { return c.ID }, func(cx *core.Scope, item func() Caption, _ func() int) core.Node {
				c := item()
				live := core.LiveRegionPolite
				role := core.RoleStatus
				if c.Kind == CaptionImportant {
					live = core.LiveRegionAssertive
					role = core.RoleAlert
				}

				rowStyle := func() core.Style {
					bg := baseBg
					border := core.Transparent
					borderW := core.Points(0)
					if opts.Assist != nil && opts.Assist.HighContrast() {
						p := PaletteFor(VisionHighContrast)
						bg = p.Background
						border = p.Foreground
						borderW = 2
					}
					if c.Kind == CaptionImportant {
						border = EnsureContrast(baseImportant, bg, 4.5)
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
					bg := baseBg
					if opts.Assist != nil && opts.Assist.HighContrast() {
						bg = PaletteFor(VisionHighContrast).Background
					}
					fg := baseFg
					weight := core.FontWeightMedium
					switch c.Kind {
					case CaptionSFX:
						fg = baseSFX
					case CaptionImportant:
						fg = baseImportant
						weight = core.FontWeightBold
					}
					opaqueBg := bg.Over(core.RGB(0x000000))
					fg = EnsureContrast(fg, opaqueBg, 7.0)

					scale := opts.TextScale
					if scale <= 0 {
						scale = 1.0
					}
					if opts.Assist != nil {
						scale = opts.Assist.TextScale()
					}
					return core.Style{
						Color:      fg,
						FontSize:   baseFontSize * core.Points(scale),
						FontWeight: weight,
						TextAlign:  core.TextAlignCenter,
					}
				}

				return core.View{
					TestID:     fmt.Sprintf("%s-item-%d", testID, c.ID),
					Role:       role,
					LiveRegion: live,
					StyleFunc:  rowStyle,
					Children: []core.Node{
						core.Text{
							ContentFunc: func() string { return item().Format() },
							LiveRegion:  live,
							StyleFunc:   textStyle,
						},
					},
				}
			}),
		},
	}
}
