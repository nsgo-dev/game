package mcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestSoundReadoutMatchesMixerAndOverlay(t *testing.T) {
	var (
		cx    *core.Scope
		mixer *sound.Mixer
		hit   *sound.Effect
		voice *sound.Effect
	)
	app := nsgotest.Start(t, func(s *core.Scope) core.Node {
		cx = s
		mixer = sound.NewMixer(s, "sound")
		mixer.SetMaxVoices(12)
		mixer.SetConcurrencyGroup("impacts", sound.ConcurrencyGroup{
			Max:     2,
			Resolve: sound.ResolveRefuse,
		})
		mixer.SetBusDucking(sound.BusDuckRule{
			Bus:    "music",
			By:     "dialogue",
			Amount: 0.4,
		})
		audio.LoadSound(s, audio.Source{URI: "asset:hit.wav"}, func(snd *audio.Sound, err error) {
			if err != nil {
				t.Errorf("load hit: %v", err)
				return
			}
			hit = mixer.Effect(snd, "sfx", 0)
			hit.Concurrency = "impacts"
		})
		audio.LoadSound(s, audio.Source{URI: "asset:voice.wav"}, func(snd *audio.Sound, err error) {
			if err != nil {
				t.Errorf("load voice: %v", err)
				return
			}
			voice = mixer.Effect(snd, "dialogue", 0)
		})
		ov := gamedebug.New(gamedebug.Options{Sound: mixer})
		if _, err := mcp.Attach(s, mcp.Config{Overlay: ov}); err != nil {
			t.Errorf("Attach: %v", err)
		}
		return core.View{}
	}, nsgotest.Environment(core.Environment{Audio: true}), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{
			"asset:hit.wav":   time.Second,
			"asset:voice.wav": time.Second,
		}
	})

	hit.Play(audio.Voice{})
	hit.Play(audio.Voice{})
	hit.Play(audio.Voice{}) // refused
	voice.Play(audio.Voice{})
	app.Settle()

	res := callTool[sound.MixerDebugSnapshot](t, app, cx, mcp.ToolSoundReadout, nil)
	if res.ActiveVoices != 3 || res.MaxVoices != 12 {
		t.Fatalf("readout voices = %d/%d, want 3/12", res.ActiveVoices, res.MaxVoices)
	}
	if len(res.Groups) != 1 || res.Groups[0].Name != "impacts" || res.Groups[0].Active != 2 || res.Groups[0].Max != 2 || res.Groups[0].Refused != 1 {
		t.Fatalf("readout groups = %+v", res.Groups)
	}
	var musicDuck float32
	for _, b := range res.Buses {
		if b.Name == "music" {
			musicDuck = b.DuckGain
		}
	}
	if musicDuck < 0.59 || musicDuck > 0.61 {
		t.Fatalf("music duck_gain = %v, want 0.6", musicDuck)
	}
}

func TestSoundReadoutWithoutMixerErrors(t *testing.T) {
	var cx *core.Scope
	app := nsgotest.Start(t, func(s *core.Scope) core.Node {
		cx = s
		if _, err := mcp.Attach(s, mcp.Config{}); err != nil {
			t.Errorf("Attach: %v", err)
		}
		return core.View{}
	})
	if _, err := callToolRaw(t, app, cx, mcp.ToolSoundReadout, nil); err == nil || !strings.Contains(err.Error(), "no sound mixer") {
		t.Fatalf("expected no sound mixer error, got %v", err)
	}
}
