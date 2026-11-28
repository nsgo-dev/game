package a11y_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/a11y"
)

func TestCaptionEmitterDebouncesOnTheTrackClockAndKeepsDirection(t *testing.T) {
	track := &a11y.CaptionTrack{MaxVisible: 4}
	e := a11y.NewCaptionEmitter(track)
	if e.Emit("Shot", a11y.CaptionSFX, -1, time.Second) == 0 {
		t.Fatal("first caption was suppressed")
	}
	track.Update(100 * time.Millisecond)
	if e.Emit("Shot", a11y.CaptionSFX, 1, time.Second) != 0 {
		t.Fatal("caption repeated inside MinInterval")
	}
	if e.Emit("Door", a11y.CaptionSFX, 0.5, time.Second) == 0 {
		t.Fatal("a different label was suppressed")
	}
	track.Update(200 * time.Millisecond)
	if e.Emit("Shot", a11y.CaptionSFX, 1, time.Second) == 0 {
		t.Fatal("caption suppressed after MinInterval")
	}
	if e.Emit("  ", a11y.CaptionSFX, 0, time.Second) != 0 {
		t.Fatal("blank label shown")
	}
	active := track.Active()
	if len(active) != 3 || active[0].Direction != -1 || active[1].Text != "Door" || active[2].Direction != 1 {
		t.Fatalf("captions = %+v", active)
	}

	e.MinInterval = 0
	if e.Emit("Shot", a11y.CaptionSFX, 0, time.Second) == 0 {
		t.Fatal("MinInterval 0 still suppressed a repeat")
	}
	var nilEmitter *a11y.CaptionEmitter
	if nilEmitter.Emit("Shot", a11y.CaptionSFX, 0, 0) != 0 {
		t.Fatal("nil emitter showed a caption")
	}
}
