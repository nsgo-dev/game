package a11y

import "time"

// CaptionBridge connects producers that already know their caption text to a
// CaptionTrack. It keeps sound spam suppression and active-dialogue replacement
// in one deterministic place without coupling a11y to cutscene or VN types.
type CaptionBridge struct {
	Track      *CaptionTrack
	lastSound  map[string]time.Duration
	dialogueID uint64
	clock      time.Duration
}

// NewCaptionBridge returns a bridge writing to track. A nil track is allowed so
// callers can keep caption hooks optional.
func NewCaptionBridge(track *CaptionTrack) *CaptionBridge {
	return &CaptionBridge{Track: track, lastSound: make(map[string]time.Duration)}
}

// Update advances the bridge clock and the track clock.
func (b *CaptionBridge) Update(dt time.Duration) {
	if b == nil {
		return
	}
	if dt < 0 {
		dt = 0
	}
	b.clock += dt
	if b.Track != nil {
		b.Track.Update(dt)
	}
}

// ShowSFXAt adds a directional sound caption unless the same label was shown
// within minInterval. A non-positive interval disables suppression.
func (b *CaptionBridge) ShowSFXAt(label string, direction float32, minInterval, duration time.Duration) uint64 {
	if b == nil || b.Track == nil || label == "" {
		return 0
	}
	if minInterval > 0 {
		if at, ok := b.lastSound[label]; ok && b.clock-at < minInterval {
			return 0
		}
	}
	b.lastSound[label] = b.clock
	return b.Track.ShowSFXAt(label, direction, duration)
}

// SyncDialogue replaces the previous dialogue caption when the active line
// changes and dismisses it when active is false. It returns the current ID.
func (b *CaptionBridge) SyncDialogue(active bool, speaker, text string, duration time.Duration) uint64 {
	if b == nil || b.Track == nil {
		return 0
	}
	if !active || text == "" {
		if b.dialogueID != 0 {
			b.Track.Dismiss(b.dialogueID)
			b.dialogueID = 0
		}
		return 0
	}
	if b.dialogueID != 0 {
		for _, c := range b.Track.Active() {
			if c.ID == b.dialogueID && c.Speaker == speaker && c.Text == text {
				return b.dialogueID
			}
		}
		b.Track.Dismiss(b.dialogueID)
	}
	b.dialogueID = b.Track.ShowDialogue(speaker, text, duration)
	return b.dialogueID
}
