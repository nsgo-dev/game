package sound_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
)

// camera hears effects from (100, 50), at full volume within 10 and silent from 110, panning fully 100 to a side.
var camera = sound.Listener2D{Position: math2d.V2(100, 50), Near: 10, Far: 110, PanWidth: 100}

func TestEffectLeftOfTheCameraPlaysPannedLeftAndQuieterWithDistance(t *testing.T) {
	m := startMixed(t)
	for _, x := range []float32{80, 40, 0} {
		if !m.click.PlayAt(camera, math2d.V2(x, 50), audio.Voice{}) {
			t.Fatalf("the click at x %v did not play", x)
		}
	}
	plays := m.plays()
	if len(plays) != 3 {
		t.Fatalf("plays %+v, want 3", plays)
	}
	for i, p := range plays {
		if !(p.Pan < 0) {
			t.Errorf("play %d pans %v, want left of center", i, p.Pan)
		}
		if i > 0 && !(p.Volume < plays[i-1].Volume && p.Pan <= plays[i-1].Pan) {
			t.Errorf("play %d at volume %v pan %v, want quieter and no further right than %v and %v", i, p.Volume, p.Pan, plays[i-1].Volume, plays[i-1].Pan)
		}
	}
	// 20 left: 10 of the 100 from Near to Far, and a fifth of PanWidth.
	if !near(plays[0].Volume, 0.9) || !near(plays[0].Pan, -0.2) {
		t.Errorf("the nearest plays at %v pan %v, want 0.9 and -0.2", plays[0].Volume, plays[0].Pan)
	}
	if !near(plays[2].Volume, 0.1) || plays[2].Pan != -1 {
		t.Errorf("the farthest plays at %v pan %v, want 0.1 and -1", plays[2].Volume, plays[2].Pan)
	}
}

func TestPlayAtAppliesTheVoiceAndTheMixer(t *testing.T) {
	m := startMixed(t)
	m.mixer.SetMaster(0.5)
	if !m.click.PlayAt(camera, math2d.V2(160, 50), audio.Voice{Volume: 0.5, Rate: 2, Pan: -1}) {
		t.Fatal("the click did not play")
	}
	plays := m.plays()
	// 60 right: 0.5 from distance, times the voice's 0.5 and the master's 0.5.
	if len(plays) != 1 || !near(plays[0].Volume, 0.125) || !near(plays[0].Pan, 0.6) || plays[0].Rate != 2 {
		t.Fatalf("plays %+v, want one at 0.125 panned 0.6 at 2 times", plays)
	}
}

func TestPlayAtPlaysNothingBeyondFar(t *testing.T) {
	m := startMixed(t)
	for _, pos := range []math2d.Vec2{math2d.V2(210, 50), math2d.V2(100, -500), math2d.V2(float32(math.NaN()), 0)} {
		if m.click.PlayAt(camera, pos, audio.Voice{}) {
			t.Errorf("the click at %v played", pos)
		}
	}
	if plays := m.plays(); len(plays) != 0 {
		t.Fatalf("plays %+v, want none", plays)
	}
	// Silent ones take none of the effect's limit of 4.
	for i := range 4 {
		if !m.click.PlayAt(camera, math2d.V2(100, 50), audio.Voice{}) {
			t.Fatalf("play %d did not play", i)
		}
	}
}

func TestListenerPan(t *testing.T) {
	for _, c := range []struct {
		name string
		l    sound.Listener2D
		pos  math2d.Vec2
		want float32
	}{
		{"centered on the listener", camera, math2d.V2(100, 50), 0},
		{"straight below", camera, math2d.V2(100, 90), 0},
		{"right", camera, math2d.V2(150, 0), 0.5},
		{"beyond PanWidth", camera, math2d.V2(-500, 50), -1},
		{"own PanWidth", sound.Listener2D{Far: 110, PanWidth: 20}, math2d.V2(-10, 0), -0.5},
		{"PanWidth without attenuation", sound.Listener2D{PanWidth: 20}, math2d.V2(30, 0), 1},
		{"no PanWidth nor Far", sound.Listener2D{}, math2d.V2(-30, 0), 0},
		// Turned a quarter clockwise, the view's right is the world's down.
		{"rotated", sound.Listener2D{Far: 100, Rotation: math.Pi / 2}, math2d.V2(0, -50), -0.5},
		{"NaN Rotation", sound.Listener2D{Far: 100, Rotation: float32(math.NaN())}, math2d.V2(-50, 0), -0.5},
		{"infinite Rotation", sound.Listener2D{Far: 100, Rotation: float32(math.Inf(1))}, math2d.V2(50, 0), 0.5},
		{"NaN", camera, math2d.V2(float32(math.NaN()), 0), 0},
		{"infinite", camera, math2d.V2(float32(math.Inf(1)), 0), 1},
		{"infinite PanWidth", sound.Listener2D{PanWidth: float32(math.Inf(1))}, math2d.V2(float32(math.Inf(1)), 0), 0},
	} {
		if got := c.l.Pan(c.pos); !near(got, c.want) {
			t.Errorf("%s: Pan(%v) = %v, want %v", c.name, c.pos, got, c.want)
		}
	}
}

func TestListenerGain(t *testing.T) {
	inverse := sound.Listener2D{Near: 10, Far: 110, Rolloff: sound.RolloffInverse}
	for _, c := range []struct {
		name string
		l    sound.Listener2D
		pos  math2d.Vec2
		want float32
	}{
		{"within Near", camera, math2d.V2(105, 55), 1},
		{"halfway", camera, math2d.V2(100, 110), 0.5},
		{"at Far", camera, math2d.V2(210, 50), 0},
		{"beyond Far", camera, math2d.V2(1000, 50), 0},
		// Near over the distance, 10/20, less Near over Far, 1/11, over 1 less 1/11: 0.45.
		{"inverse", inverse, math2d.V2(20, 0), 0.45},
		{"inverse at Near", inverse, math2d.V2(0, 10), 1},
		{"inverse at Far", inverse, math2d.V2(0, 110), 0},
		{"inverse without Near falls linearly", sound.Listener2D{Far: 100, Rolloff: sound.RolloffInverse}, math2d.V2(25, 0), 0.75},
		{"no Far", sound.Listener2D{Near: 10}, math2d.V2(1e6, 0), 1},
		{"Far within Near", sound.Listener2D{Near: 10, Far: 5}, math2d.V2(1e6, 0), 1},
		{"infinite Far", sound.Listener2D{Far: float32(math.Inf(1))}, math2d.V2(1e6, 0), 1},
		{"negative Near", sound.Listener2D{Near: -10, Far: 100}, math2d.V2(50, 0), 0.5},
		{"rotation keeps distance", sound.Listener2D{Far: 100, Rotation: 1}, math2d.V2(30, 40), 0.5},
		{"NaN Rotation", sound.Listener2D{Far: 100, Rotation: float32(math.NaN())}, math2d.V2(30, 40), 0.5},
		{"infinite Rotation", sound.Listener2D{Far: 100, Rotation: float32(math.Inf(-1))}, math2d.V2(30, 40), 0.5},
		{"NaN", camera, math2d.V2(0, float32(math.NaN())), 0},
		{"infinite", camera, math2d.V2(0, float32(math.Inf(-1))), 0},
	} {
		if got := c.l.Gain(c.pos); !near(got, c.want) {
			t.Errorf("%s: Gain(%v) = %v, want %v", c.name, c.pos, got, c.want)
		}
	}
}

func TestListenerVoice(t *testing.T) {
	v := camera.Voice(math2d.V2(40, 50), audio.Voice{Rate: 1.5, Loop: true, Pan: 1})
	if !near(v.Volume, 0.5) || !near(v.Pan, -0.6) || v.Rate != 1.5 || !v.Loop {
		t.Fatalf("Voice = %+v, want volume 0.5, pan -0.6, rate 1.5 and looping", v)
	}
}

func TestListenerVoiceIsMutedWhereSilent(t *testing.T) {
	for _, c := range []struct {
		name string
		pos  math2d.Vec2
		v    audio.Voice
	}{
		{"beyond Far", math2d.V2(1000, 50), audio.Voice{}},
		{"NaN", math2d.V2(float32(math.NaN()), 50), audio.Voice{}},
		{"negative volume", math2d.V2(100, 50), audio.Voice{Volume: -1}},
	} {
		if v := camera.Voice(c.pos, c.v); v.Volume != 0 || !v.Muted {
			t.Errorf("%s: Voice = %+v, want volume 0 and muted", c.name, v)
		}
	}
	if v := camera.Voice(math2d.V2(100, 50), audio.Voice{}); v.Muted {
		t.Errorf("Voice = %+v at the listener, want not muted", v)
	}
}

func TestCuePlayAt(t *testing.T) {
	m := startMixed(t)
	cue := sound.NewCue(7, sound.CueOptions{Clips: []sound.Clip{sound.EffectClip(m.click)}, PitchMin: 1.5, PitchMax: 1.5, VolumeMin: 0.5, VolumeMax: 0.5})
	if pick, ok := cue.PlayAt(camera, math2d.V2(40, 50)); !ok || pick.Index != 0 {
		t.Fatalf("PlayAt = %+v, %v, want the click played", pick, ok)
	}
	if _, ok := cue.PlayAt(camera, math2d.V2(1000, 50)); ok {
		t.Error("the cue beyond Far played")
	}
	plays := m.plays()
	// 60 left: 0.5 from distance times the cue's 0.5, panned 0.6 left.
	if len(plays) != 1 || !near(plays[0].Volume, 0.25) || !near(plays[0].Pan, -0.6) || plays[0].Rate != 1.5 {
		t.Fatalf("plays %+v, want one at 0.25 panned -0.6 at 1.5 times", plays)
	}
	var none *sound.Cue
	if pick, ok := none.PlayAt(camera, math2d.V2(100, 50)); ok || pick.Index != -1 {
		t.Errorf("a nil cue's PlayAt = %+v, %v, want index -1 and not played", pick, ok)
	}
}

func FuzzListener2D(f *testing.F) {
	f.Add(float32(100), float32(50), float32(0), float32(10), float32(110), float32(0), uint8(0), float32(0), float32(0))
	f.Add(float32(0), float32(0), float32(1), float32(0), float32(1), float32(-3), uint8(1), float32(-0.5), float32(1e30))
	f.Fuzz(func(t *testing.T, lx, ly, rot, nearD, farD, width float32, rolloff uint8, px, py float32) {
		l := sound.Listener2D{Position: math2d.V2(lx, ly), Rotation: rot, Near: nearD, Far: farD, Rolloff: sound.Rolloff(rolloff % 2), PanWidth: width}
		pos := math2d.V2(px, py)
		if p := l.Pan(pos); !(p >= -1 && p <= 1) {
			t.Fatalf("%+v: Pan(%v) = %v, want within -1 and 1", l, pos, p)
		}
		g := l.Gain(pos)
		if !(g >= 0 && g <= 1) {
			t.Fatalf("%+v: Gain(%v) = %v, want within 0 and 1", l, pos, g)
		}
		if v := l.Voice(pos, audio.Voice{}); v.Volume != g || v.Muted != (g == 0) {
			t.Fatalf("%+v: Voice(%v) volume %v muted %v, want Gain %v and muted only when silent", l, pos, v.Volume, v.Muted, g)
		}
	})
}

func BenchmarkListener2DVoice(b *testing.B) {
	l := sound.Listener2D{Position: math2d.V2(100, 50), Rotation: 0.3, Near: 10, Far: 500, Rolloff: sound.RolloffInverse}
	pos := math2d.V2(-120, 300)
	b.ReportAllocs()
	var v audio.Voice
	for b.Loop() {
		v = l.Voice(pos, audio.Voice{})
	}
	if !(v.Volume > 0) {
		b.Fatalf("Voice = %+v, want audible", v)
	}
}
