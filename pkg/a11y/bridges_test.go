package a11y

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestCaptionBridgeDirectionalDedupAndDialogueClear(t *testing.T) {
	var track *CaptionTrack
	nsgotest.Start(t, func(cx *core.Scope) core.Node { track = NewCaptionTrack(cx); return nil })
	b := NewCaptionBridge(track)
	if id := b.ShowSFXAt("shot", -1, time.Second, time.Second); id == 0 {
		t.Fatal("first sound was suppressed")
	}
	if id := b.ShowSFXAt("shot", -1, time.Second, time.Second); id != 0 {
		t.Fatalf("repeat id = %d", id)
	}
	b.Update(time.Second)
	if id := b.ShowSFXAt("shot", 1, time.Second, time.Second); id == 0 {
		t.Fatal("sound after interval was suppressed")
	}
	id := b.SyncDialogue(true, "Ada", "Move!", time.Second)
	if id == 0 || len(track.Active()) != 2 {
		t.Fatalf("dialogue id=%d active=%d", id, len(track.Active()))
	}
	if got := b.SyncDialogue(true, "Ada", "Move!", time.Second); got != id {
		t.Fatalf("same dialogue got %d, want %d", got, id)
	}
	b.SyncDialogue(false, "", "", 0)
	for _, c := range track.Active() {
		if c.ID == id {
			t.Fatal("dialogue remained active")
		}
	}
}
