//go:build !release

package gamedebug_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestOverlayDisplaysSoundVoicesAndConcurrencyGroups(t *testing.T) {
	var mixer *sound.Mixer
	var hit *sound.Effect
	var ov *gamedebug.Overlay

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mixer = sound.NewMixer(cx, "sound")
		mixer.SetMaxVoices(8)
		mixer.SetConcurrencyGroup("impacts", sound.ConcurrencyGroup{
			Max:     2,
			Resolve: sound.ResolveOldest,
		})
		audio.LoadSound(cx, audio.Source{URI: "asset:hit.wav"}, func(s *audio.Sound, err error) {
			if err != nil {
				t.Errorf("load hit: %v", err)
				return
			}
			hit = mixer.Effect(s, "sfx", 0)
			hit.Concurrency = "impacts"
		})
		ov = gamedebug.New(gamedebug.Options{
			Visible: true,
			Sound:   mixer,
		})
		return ov.View(cx)
	}, nsgotest.Environment(core.Environment{Audio: true}), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{"asset:hit.wav": time.Second}
	})

	if ov.Sound() != mixer {
		t.Fatal("Overlay.Sound() did not return configured mixer")
	}
	if !hit.Play(audio.Voice{}) {
		t.Fatal("hit did not play")
	}
	ov.RecordFrame(16*time.Millisecond, 2*time.Millisecond, 2*time.Millisecond)
	app.Settle()

	if !app.Exists("debug-sound") {
		t.Fatal("debug-sound section missing from visible overlay")
	}
	snap := mixer.DebugSnapshot()
	if snap.ActiveVoices != 1 || snap.MaxVoices != 8 || len(snap.Groups) != 1 || snap.Groups[0].Name != "impacts" || snap.Groups[0].Active != 1 {
		t.Fatalf("unexpected sound debug snapshot: %+v", snap)
	}
}
