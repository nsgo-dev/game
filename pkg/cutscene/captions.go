package cutscene

import (
	"time"

	"github.com/nsgo-dev/game/pkg/a11y"
)

// captionHold is how long a bridged dialogue caption lasts on the track; the bridge dismisses it when the line ends, so it only has to outlast any dialogue clip.
const captionHold = time.Hour

// CaptionBridge mirrors a timeline player's active dialogue into a CaptionTrack, so cutscene lines get the caption settings (size, contrast, speaker colors, toggles) of CaptionsView. Call Update on the same fixed loop step as Player.Step.
type CaptionBridge struct {
	Player *Player
	Track  *a11y.CaptionTrack
	// lastID is the line ID shown last and captionID its caption on Track, 0 when none is shown.
	lastID    string
	captionID uint64
}

// NewCaptionBridge creates a bridge for player and track. Call Update after Player.Step on each fixed update.
func NewCaptionBridge(player *Player, track *a11y.CaptionTrack) *CaptionBridge {
	return &CaptionBridge{Player: player, Track: track}
}

// Update shows a newly active dialogue line once, with its speaker, and dismisses its caption when the line ends or another replaces it, so the caption lasts as long as the dialogue clip and a replayed line shows again.
func (b *CaptionBridge) Update() {
	if b == nil || b.Player == nil || b.Track == nil {
		return
	}
	line := b.Player.dialogue.GetUntracked()
	if line.Active && line.ID == b.lastID {
		return
	}
	if b.captionID != 0 {
		b.Track.Dismiss(b.captionID)
		b.captionID = 0
	}
	b.lastID = ""
	if !line.Active {
		return
	}
	b.lastID = line.ID
	b.captionID = b.Track.ShowDialogue(line.Speaker, line.Text, captionHold)
}
