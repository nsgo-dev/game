package a11y

import (
	"strings"
	"time"
)

// DefaultCaptionMinInterval is how long NewCaptionEmitter suppresses a repeat of the same label.
const DefaultCaptionMinInterval = 250 * time.Millisecond

// CaptionEmitter turns gameplay audio events, such as the plays sound.Effect.SetCaption reports, into CaptionTrack entries. It suppresses a label repeated within MinInterval so automatic sound hooks do not flood the caption area. Time is the track's own clock, which CaptionTrack.Update advances on the fixed loop step, so the debounce is deterministic and pauses with the track.
type CaptionEmitter struct {
	// Track receives the captions; Emit does nothing while it is nil.
	Track *CaptionTrack
	// MinInterval is the shortest time between two captions with the same label; 0 or less shows every one.
	MinInterval time.Duration
	// last is the track clock at which each label was last shown.
	last map[string]time.Duration
}

// NewCaptionEmitter returns an emitter for track with MinInterval DefaultCaptionMinInterval.
func NewCaptionEmitter(track *CaptionTrack) *CaptionEmitter {
	return &CaptionEmitter{Track: track, MinInterval: DefaultCaptionMinInterval}
}

// Emit shows a caption of kind for label in direction, the same [-1, 1] pan audio uses, lasting duration (0 for DefaultCaptionDuration), and returns its ID. It returns 0, showing nothing, for a blank label or a label shown less than MinInterval ago on the track's clock.
func (e *CaptionEmitter) Emit(label string, kind CaptionKind, direction float32, duration time.Duration) uint64 {
	if e == nil || e.Track == nil || strings.TrimSpace(label) == "" {
		return 0
	}
	now := e.Track.clock
	if last, ok := e.last[label]; ok && e.MinInterval > 0 && now-last < e.MinInterval {
		return 0
	}
	if e.last == nil {
		e.last = make(map[string]time.Duration)
	}
	e.last[label] = now
	return e.Track.Show(Caption{Text: label, Kind: kind, Direction: direction, Duration: duration})
}
