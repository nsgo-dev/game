package sound_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var (
	playsAudio = core.Environment{Audio: true}
	background = core.Environment{Audio: true, Lifecycle: core.LifecycleBackground}
)

const (
	clickURI = "asset:sounds/click.wav"
	trackA   = "asset:music/a.m4a"
	trackB   = "asset:music/b.m4a"
)

// mixed is an app with a mixer, a click in "effects" limited to 4 at once and music in "music".
type mixed struct {
	app   *nsgotest.App
	mixer *sound.Mixer
	click *sound.Effect
	music *sound.Music
}

func startMixed(t *testing.T, opts ...nsgotest.Option) *mixed {
	t.Helper()
	m := &mixed{}
	opts = append([]nsgotest.Option{nsgotest.Environment(playsAudio), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{clickURI: 100 * time.Millisecond, trackA: time.Minute, trackB: time.Minute}
	}}, opts...)
	m.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		m.mixer = sound.NewMixer(cx, "sound")
		audio.LoadSound(cx, audio.Source{URI: clickURI}, func(s *audio.Sound, err error) {
			if err != nil {
				t.Errorf("load the click: %v", err)
				return
			}
			m.click = m.mixer.Effect(s, "effects", 4)
		})
		m.music = m.mixer.Music("music")
		return core.View{}
	}, opts...)
	if m.click == nil {
		t.Fatal("the click did not load")
	}
	return m
}

// plays are the click's playbacks so far.
func (m *mixed) plays() []audio.Voice {
	m.app.Settle()
	return m.app.Sounds[0].Plays
}

// player is the platform's player of the music's i-th track.
func (m *mixed) player(t *testing.T, i int) *nsgotest.AudioPlayer {
	t.Helper()
	m.app.Settle()
	if len(m.app.Players) <= i {
		t.Fatalf("%d players, want at least %d", len(m.app.Players), i+1)
	}
	return m.app.Players[i]
}

// playbacks are the click's playbacks so far, oldest first.
func (m *mixed) playbacks() []*nsgotest.AudioPlayback {
	m.app.Settle()
	return m.app.Sounds[0].Playbacks
}

// expectPlayback fails unless p plays on at volume once the app settles.
func (m *mixed) expectPlayback(t *testing.T, p *nsgotest.AudioPlayback, volume float32) {
	t.Helper()
	m.app.Settle()
	if p.Ended || !near(p.Volume, volume) {
		t.Fatalf("playback: ended %v, volume %v; want playing at %v", p.Ended, p.Volume, volume)
	}
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func expectPlayer(t *testing.T, p *nsgotest.AudioPlayer, playing bool, volume float32) {
	t.Helper()
	if p.Playing != playing || p.Closed || (playing && !near(p.Volume, volume)) {
		t.Fatalf("%s: playing %v, closed %v, volume %v; want playing %v at %v", p.Source.URI, p.Playing, p.Closed, p.Volume, playing, volume)
	}
}

func TestVolumesDefaultToFull(t *testing.T) {
	m := startMixed(t)
	if m.mixer.Master() != 1 || m.mixer.Volume("effects") != 1 {
		t.Fatalf("master %v, effects %v, want 1 and 1", m.mixer.Master(), m.mixer.Volume("effects"))
	}
	m.click.Play(audio.Voice{})
	if plays := m.plays(); len(plays) != 1 || plays[0].Volume != 1 {
		t.Fatalf("plays %+v, want one at 1", plays)
	}
}

func TestDefaultVolumesApplyUntilThePlayerSetsOne(t *testing.T) {
	m := startMixed(t)
	m.mixer.SetDefaultMaster(0.5)
	m.mixer.SetDefaultVolume("effects", 0.4)
	m.mixer.SetDefaultVolume("music", 2)
	if !near(m.mixer.Master(), 0.5) || !near(m.mixer.Volume("effects"), 0.4) || m.mixer.Volume("music") != 1 || m.mixer.Volume("ui") != 1 {
		t.Fatalf("master %v, effects %v, music %v, ui %v; want 0.5, 0.4, 1 and 1", m.mixer.Master(), m.mixer.Volume("effects"), m.mixer.Volume("music"), m.mixer.Volume("ui"))
	}
	m.click.Play(audio.Voice{})
	if plays := m.plays(); len(plays) != 1 || !near(plays[0].Volume, 0.2) {
		t.Fatalf("plays %+v, want one at 0.2", plays)
	}

	// A volume the player keeps wins over the default, and the default stays out of the storage.
	m.mixer.SetVolume("effects", 1)
	if m.mixer.Volume("effects") != 1 {
		t.Fatalf("effects %v after SetVolume(1), want 1", m.mixer.Volume("effects"))
	}
	if _, ok := m.app.Storage().GetItem("sound.master"); ok {
		t.Fatal("SetDefaultMaster kept the master volume in the storage")
	}
}

func TestGroupAndMasterVolumesApplyToNewAndPlayingSounds(t *testing.T) {
	m := startMixed(t)
	m.music.Play(audio.Source{URI: trackA}, 0)
	expectPlayer(t, m.player(t, 0), true, 1)
	m.app.Advance(time.Second)

	m.mixer.SetMaster(0.8)
	m.mixer.SetVolume("effects", 0.5)
	if !m.click.Play(audio.Voice{Volume: 0.5, Rate: 1.5}) {
		t.Fatal("the click did not play")
	}
	plays := m.plays()
	if len(plays) != 1 || !near(plays[0].Volume, 0.2) || plays[0].Rate != 1.5 {
		t.Fatalf("plays %+v, want one at 0.2 at 1.5 times", plays)
	}
	// The music plays on at the master volume, and its group's as it changes.
	expectPlayer(t, m.player(t, 0), true, 0.8)
	m.mixer.SetVolume("music", 0.5)
	expectPlayer(t, m.player(t, 0), true, 0.4)
	// Other groups leave it be.
	m.mixer.SetVolume("ui", 0.1)
	expectPlayer(t, m.player(t, 0), true, 0.4)

	// The click playing follows its group's volume and the master volume, as its own voice's volume times theirs.
	click := m.playbacks()[0]
	m.expectPlayback(t, click, 0.2)
	m.mixer.SetVolume("effects", 1)
	m.expectPlayback(t, m.playbacks()[0], 0.4)
	m.mixer.SetMaster(0.4)
	m.expectPlayback(t, click, 0.2)
	m.mixer.SetVolume("ui", 0.1)
	m.expectPlayback(t, click, 0.2)
	m.mixer.SetMaster(0.8)
	m.mixer.SetVolume("effects", 0.5)

	// A group at 0 silences its sounds playing, without stopping them, and plays no more of them.
	m.click.Play(audio.Voice{})
	m.mixer.SetVolume("effects", 0)
	pbs := m.playbacks()
	m.expectPlayback(t, pbs[0], 0)
	m.expectPlayback(t, pbs[1], 0)
	if stops := m.app.Sounds[0].Stops; stops != 0 {
		t.Fatalf("%d stops, want 0", stops)
	}
	if m.click.Play(audio.Voice{}) || len(m.plays()) != 2 {
		t.Fatalf("a click played at 0: %+v", m.plays())
	}
	// They are heard again as the group comes back.
	m.mixer.SetVolume("effects", 0.5)
	m.expectPlayback(t, pbs[0], 0.2)
	m.expectPlayback(t, pbs[1], 0.4)

	// A master volume of 0 pauses the music, and one above plays it on.
	m.mixer.SetMaster(0)
	expectPlayer(t, m.player(t, 0), false, 0)
	m.mixer.SetMaster(1)
	expectPlayer(t, m.player(t, 0), true, 0.5)
	if m.app.Players[0].Position == 0 {
		t.Fatal("the music started over")
	}
}

func TestVolumesClampToZeroToOne(t *testing.T) {
	m := startMixed(t)
	m.mixer.SetMaster(2)
	m.mixer.SetVolume("effects", -1)
	if m.mixer.Master() != 1 || m.mixer.Volume("effects") != 0 {
		t.Fatalf("master %v, effects %v, want 1 and 0", m.mixer.Master(), m.mixer.Volume("effects"))
	}
}

func TestASoundLimitedToFourStopsItsOldestToPlayAFifth(t *testing.T) {
	m := startMixed(t)
	for i := range 6 {
		if !m.click.Play(audio.Voice{Volume: float32(i+1) / 10}) {
			t.Fatalf("click %d did not play", i)
		}
	}
	pbs := m.playbacks()
	if len(pbs) != 6 || len(m.plays()) != 6 {
		t.Fatalf("%d playbacks, want 6", len(pbs))
	}
	// The two oldest stopped to make room, and the newest four play.
	for i, p := range pbs {
		if p.Ended != (i < 2) {
			t.Fatalf("playback %d: ended %v, want %v", i, p.Ended, i < 2)
		}
	}
	if stops := m.app.Sounds[0].Stops; stops != 0 {
		t.Fatalf("%d stops of every playback, want 0", stops)
	}

	// Playbacks that end on their own make room without stopping another, even one older than they are: at half the speed the first of these lasts 200 ms, the other three 100 ms.
	m.app.Advance(150 * time.Millisecond)
	m.click.Play(audio.Voice{Rate: 0.5})
	for range 3 {
		m.click.Play(audio.Voice{})
	}
	m.app.Advance(120 * time.Millisecond)
	pbs = m.playbacks()
	if len(pbs) != 10 || pbs[6].Ended || !pbs[7].Ended || !pbs[8].Ended || !pbs[9].Ended {
		t.Fatalf("%d playbacks; want 10 with only the slow one of the last four playing", len(pbs))
	}
	for range 3 {
		m.click.Play(audio.Voice{})
	}
	pbs = m.playbacks()
	live := []*nsgotest.AudioPlayback{pbs[6], pbs[10], pbs[11], pbs[12]}
	for i, p := range live {
		if p.Ended {
			t.Fatalf("playback %d of four within the limit ended", i)
		}
	}

	// The playbacks left still follow the volumes.
	m.mixer.SetVolume("effects", 0.5)
	for _, p := range live {
		m.expectPlayback(t, p, 0.5)
	}

	// A limit of 0 is the platform's alone.
	m.click.Limit = 0
	for range 10 {
		if !m.click.Play(audio.Voice{}) {
			t.Fatal("an unlimited click did not play")
		}
	}
	n := 0
	for _, p := range m.playbacks() {
		if !p.Ended {
			n++
		}
	}
	if n != 14 {
		t.Fatalf("%d playbacks play, want 14", n)
	}
}

func TestALoweredLimitStopsAsManyOfTheOldestAsItTakes(t *testing.T) {
	m := startMixed(t)
	for range 4 {
		m.click.Play(audio.Voice{})
	}
	m.click.Limit = 2
	if !m.click.Play(audio.Voice{}) {
		t.Fatal("the click did not play")
	}
	pbs := m.playbacks()
	if len(pbs) != 5 {
		t.Fatalf("%d playbacks, want 5", len(pbs))
	}
	for i, p := range pbs {
		if p.Ended != (i < 3) {
			t.Fatalf("playback %d: ended %v, want %v", i, p.Ended, i < 3)
		}
	}
}

func TestAMutedVoicePlaysNothing(t *testing.T) {
	m := startMixed(t)
	if m.click.Play(audio.Voice{Muted: true}) || len(m.plays()) != 0 {
		t.Fatalf("a muted click played: %+v", m.plays())
	}
}

func TestVolumesPersistAcrossARestart(t *testing.T) {
	m := startMixed(t)
	m.mixer.SetMaster(0.5)
	m.mixer.SetVolume("effects", 0.25)
	m.mixer.SetVolume("music", 0)
	kept := map[string]string{}
	st := m.app.Storage()
	for _, k := range st.Keys() {
		kept[k], _ = st.GetItem(k)
	}

	again := startMixed(t, nsgotest.Storage(kept))
	if again.mixer.Master() != 0.5 || again.mixer.Volume("effects") != 0.25 || again.mixer.Volume("music") != 0 || again.mixer.Volume("ui") != 1 {
		t.Fatalf("after a restart: master %v, effects %v, music %v, ui %v", again.mixer.Master(), again.mixer.Volume("effects"), again.mixer.Volume("music"), again.mixer.Volume("ui"))
	}
	again.click.Play(audio.Voice{})
	if plays := again.plays(); len(plays) != 1 || !near(plays[0].Volume, 0.125) {
		t.Fatalf("plays %+v, want one at 0.125", plays)
	}
	// Music in a group at 0 waits to be heard.
	again.music.Play(audio.Source{URI: trackA}, 0)
	again.app.Settle()
	if len(again.app.Players) != 0 {
		t.Fatal("music in a silent group opened")
	}
	again.mixer.SetVolume("music", 1)
	expectPlayer(t, again.player(t, 0), true, 0.5)
}

func TestTheBackgroundMutesAndTheForegroundRestores(t *testing.T) {
	m := startMixed(t)
	m.mixer.SetVolume("music", 0.5)
	m.music.Play(audio.Source{URI: trackA}, 0)
	m.click.Play(audio.Voice{Volume: 0.5, Loop: true})
	expectPlayer(t, m.player(t, 0), true, 0.5)
	m.expectPlayback(t, m.playbacks()[0], 0.5)

	m.app.SetEnvironment(background)
	m.app.Settle()
	if stops := m.app.Sounds[0].Stops; stops != 0 {
		t.Fatalf("%d stops, want 0", stops)
	}
	m.expectPlayback(t, m.playbacks()[0], 0)
	expectPlayer(t, m.player(t, 0), false, 0)
	if m.click.Play(audio.Voice{}) {
		t.Fatal("a click played in the background")
	}
	at := m.app.Players[0].Position
	m.app.Advance(time.Second)
	if m.app.Players[0].Position != at {
		t.Fatal("the music played on in the background")
	}

	m.app.SetEnvironment(playsAudio)
	expectPlayer(t, m.player(t, 0), true, 0.5)
	m.expectPlayback(t, m.playbacks()[0], 0.5)
	if !m.click.Play(audio.Voice{}) {
		t.Fatal("no click played back in the foreground")
	}
	// A window that shows without the focus plays on.
	m.app.SetEnvironment(core.Environment{Audio: true, Lifecycle: core.LifecycleInactive})
	expectPlayer(t, m.player(t, 0), true, 0.5)
}

func TestSuspendSilencesUntilEveryHoldResumes(t *testing.T) {
	m := startMixed(t)
	m.mixer.SetVolume("music", 0.5)
	m.music.Play(audio.Source{URI: trackA}, 0)
	m.click.Play(audio.Voice{})
	expectPlayer(t, m.player(t, 0), true, 0.5)

	first := m.mixer.Suspend()
	second := m.mixer.Suspend()
	m.app.Settle()
	if !m.mixer.Suspended() {
		t.Fatal("the mixer is not suspended")
	}
	if stops := m.app.Sounds[0].Stops; stops != 0 {
		t.Fatalf("%d stops, want 0", stops)
	}
	expectPlayer(t, m.player(t, 0), false, 0)
	if m.click.Play(audio.Voice{}) {
		t.Fatal("a click played while suspended")
	}
	at := m.app.Players[0].Position
	m.app.Advance(time.Second)
	if m.app.Players[0].Position != at {
		t.Fatal("the music played on while suspended")
	}

	// Resuming one hold twice does not release the other.
	first()
	first()
	expectPlayer(t, m.player(t, 0), false, 0)
	if !m.mixer.Suspended() {
		t.Fatal("the mixer resumed with a hold left")
	}
	second()
	expectPlayer(t, m.player(t, 0), true, 0.5)
	if m.mixer.Suspended() || !m.click.Play(audio.Voice{}) {
		t.Fatal("no click played after the mixer resumed")
	}

	// Music started while suspended waits for the resume.
	resume := m.mixer.Suspend()
	m.music.Play(audio.Source{URI: trackB}, 0)
	m.app.Settle()
	if ps := m.app.Players; len(ps) > 1 && ps[1].Playing {
		t.Fatal("music started while suspended plays")
	}
	resume()
	expectPlayer(t, m.player(t, 1), true, 0.5)
}

func TestACrossfadeEndsWithOnlyTheNewTrackAtFullGroupVolume(t *testing.T) {
	m := startMixed(t)
	m.mixer.SetVolume("music", 0.5)
	m.music.Loop = true
	m.music.Play(audio.Source{URI: trackA}, 0)
	expectPlayer(t, m.player(t, 0), true, 0.5)
	if !m.app.Players[0].Loop {
		t.Fatal("the track does not loop")
	}

	m.music.Play(audio.Source{URI: trackB}, time.Second)
	// The new track starts at once, at silence.
	b := m.player(t, 1)
	if !b.Playing || b.Volume != 0 || slices.Contains(b.Commands, "pause") {
		t.Fatalf("the new track starts playing %v at %v, commands %v; want playing at 0", b.Playing, b.Volume, b.Commands)
	}
	expectPlayer(t, m.player(t, 0), true, 0.5)
	m.app.Advance(500 * time.Millisecond)
	a := m.player(t, 0)
	if !a.Playing || !b.Playing || math.Abs(float64(a.Volume+b.Volume-0.5)) > 0.02 || a.Volume < 0.2 || a.Volume > 0.3 {
		t.Fatalf("halfway: a at %v playing %v, b at %v playing %v; want both near 0.25", a.Volume, a.Playing, b.Volume, b.Playing)
	}

	m.app.Advance(600 * time.Millisecond)
	if !a.Closed {
		t.Fatal("the old track was not closed")
	}
	if !b.Playing || b.Volume != 0.5 || !b.Loop {
		t.Fatalf("the new track: playing %v at %v, loop %v; want playing at 0.5, looping", b.Playing, b.Volume, b.Loop)
	}

	// A crossfade from one still fading in fades it out from where it got to.
	m.music.Play(audio.Source{URI: trackA}, time.Second)
	m.app.Advance(500 * time.Millisecond)
	m.music.Play(audio.Source{URI: trackB}, time.Second)
	m.app.Advance(1100 * time.Millisecond)
	for i, p := range m.app.Players[:3] {
		if !p.Closed {
			t.Fatalf("player %d is not closed", i)
		}
	}
	expectPlayer(t, m.player(t, 3), true, 0.5)

	m.music.Stop(0)
	if p := m.player(t, 3); !p.Closed {
		t.Fatal("Stop left the music open")
	}
}
