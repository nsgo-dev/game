package cutscene_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/cutscene"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestCaptionBridgeShowsDialogueUntilTheClipEnds(t *testing.T) {
	c := loadFloor1Fixture(t)
	sc := floor1Scene(t)
	cat := floor1Catalog(t)

	var (
		player *cutscene.Player
		track  *a11y.CaptionTrack
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		player = cutscene.NewPlayer(cx, cutscene.Options{})
		track = a11y.NewCaptionTrack(cx)
		bridge := cutscene.NewCaptionBridge(player, track)
		loop := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				player.Step(dt)
				bridge.Update()
				track.Update(dt)
			},
		}
		loop.Run(cx)
		return core.View{Class: "flex-1", Children: []core.Node{player.View()}}
	}, nsgotest.Environment(foreground))

	if _, err := player.PlayTimeline(c, cutscene.TimelineTarget{Scene: sc, Catalog: cat, Locale: "en"}, nil); err != nil {
		t.Fatal(err)
	}
	app.Settle()

	// dl1 runs from 3.6s to 6s and dl2 from 8.2s to 10.4s; each caption must show once while its clip is active and go when it ends.
	app.Advance(4 * time.Second)
	app.Settle()
	active := track.Active()
	if len(active) != 1 || active[0].Kind != a11y.CaptionDialogue || active[0].Text != "Another floor. Another locked door." {
		t.Fatalf("captions at 4s = %+v, want the first line", active)
	}

	app.Advance(time.Second)
	app.Settle()
	if active := track.Active(); len(active) != 1 {
		t.Fatalf("captions at 5s = %+v, want the first line still shown once", active)
	}

	app.Advance(1500 * time.Millisecond)
	app.Settle()
	if active := track.Active(); len(active) != 0 {
		t.Fatalf("captions at 6.5s = %+v, want none after the clip ended", active)
	}

	app.Advance(2 * time.Second)
	app.Settle()
	active = track.Active()
	if len(active) != 1 || active[0].Text != "The lever has to be down here somewhere." {
		t.Fatalf("captions at 8.5s = %+v, want the second line", active)
	}
}
