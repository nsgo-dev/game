package sound_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
)

type captionCall struct {
	label string
	pan   float32
}

func TestSetCaptionReportsThePanOfPositionalPlays(t *testing.T) {
	m := startMixed(t)
	var calls []captionCall
	m.click.SetCaption("Click", func(label string, pan float32) {
		calls = append(calls, captionCall{label, pan})
	})

	if !m.click.PlayAt(camera, math2d.V2(80, 50), audio.Voice{}) {
		t.Fatal("the click left of the camera did not play")
	}
	if !m.click.PlayAt3D(listener3D, sound.V3(2, 0, -2), audio.Voice{}) {
		t.Fatal("the click right of the 3D listener did not play")
	}
	// Beyond Far nothing plays, so no caption either.
	if m.click.PlayAt(camera, math2d.V2(1000, 50), audio.Voice{}) {
		t.Fatal("the click beyond Far played")
	}
	// Play has no position and does not caption.
	m.click.Play(audio.Voice{})

	plays := m.plays()
	if len(calls) != 2 || len(plays) != 3 {
		t.Fatalf("captions %+v and plays %+v, want 2 captions for 3 plays", calls, plays)
	}
	if calls[0].label != "Click" || !near(calls[0].pan, plays[0].Pan) || !(calls[0].pan < 0) {
		t.Errorf("2D caption %+v, want Click panned left as the play's %v", calls[0], plays[0].Pan)
	}
	if calls[1].label != "Click" || !near(calls[1].pan, plays[1].Pan) || !(calls[1].pan > 0) {
		t.Errorf("3D caption %+v, want Click panned right as the play's %v", calls[1], plays[1].Pan)
	}

	m.click.SetCaption("", nil)
	m.click.PlayAt(camera, math2d.V2(80, 50), audio.Voice{})
	if len(calls) != 2 {
		t.Fatalf("captions %+v after removing the callback, want still 2", calls)
	}
}
