package input_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestActionsMixerUnblocksOnFirstWebGesture(t *testing.T) {
	const bgmURI = "asset:music/title.m4a"

	cases := []struct {
		name    string
		trigger func(app *nsgotest.App)
	}{
		{
			name:    "press",
			trigger: func(app *nsgotest.App) { app.PressDown("id:stage") },
		},
		{
			name: "touch",
			trigger: func(app *nsgotest.App) {
				f := app.Touch("id:stage", core.Point{X: 20, Y: 20})
				f.Lift()
			},
		},
		{
			name:    "key",
			trigger: func(app *nsgotest.App) { app.Key("space") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var actions *input.Actions
			var mixer *sound.Mixer
			var music *sound.Music

			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				mixer = sound.NewMixer(cx, "sound")
				_ = mixer.SetVolume("music", 0.6)
				music = mixer.Music("music")
				music.Loop = true

				actions = input.NewActions()
				actions.Mixer = mixer
				actions.WatchKeys(cx)

				music.Play(audio.Source{URI: bgmURI}, 0)

				stage := core.View{
					TestID: "stage",
					Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
				}
				actions.Attach(&stage)
				return stage
			},
				nsgotest.Environment(core.Environment{OS: core.OSWeb, Audio: true}),
				func(a *nsgotest.App) {
					a.AudioDurations = map[string]time.Duration{bgmURI: time.Minute}
					a.RefusePlayback = true
				},
			)

			if len(app.Players) != 1 || app.Players[0].Playing || !music.Blocked() {
				t.Fatalf("initial state: players=%d playing=%v blocked=%v; want 1, false, true", len(app.Players), len(app.Players) > 0 && app.Players[0].Playing, music.Blocked())
			}

			app.RefusePlayback = false
			tc.trigger(app)
			app.Settle()

			if music.Blocked() {
				t.Fatal("music remained blocked after first gesture")
			}
			p := app.Players[0]
			if !p.Playing || p.Closed || !p.Loop || p.Volume < 0.59 || p.Volume > 0.61 {
				t.Fatalf("after %s: playing=%v closed=%v loop=%v vol=%v; want playing looping at 0.6", tc.name, p.Playing, p.Closed, p.Loop, p.Volume)
			}
		})
	}
}
