package sound

import (
	"slices"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

// Mixer sets how loud a game's sounds and music play: a master volume and a volume for each group the app names, such as "effects", "music" and "ui", each from 0 to 1 and 1 until set. It keeps them in the app's storage (core.Storage), so they hold across launches, and plays nothing while the app is in the background.
//
// An effect plays at its own voice's volume times its group's and the master volume, and music at its group's and the master volume; a volume change applies at once to the effects playing, through their audio.Playback, and to music as it plays. An effect whose group or master volume reaches 0 plays on in silence and is heard again as the volume comes back.
//
// The mixer's methods, its effects' and its music's are called on the engine's goroutine, as audio's are.
type Mixer struct {
	cx    *core.Scope
	store core.Storage
	key   string
	// background says the app is in the background, where nothing plays.
	background bool
	// suspends counts the Suspend calls not resumed yet; nothing plays while it is above 0.
	suspends int
	// sounds are the effects whose playbacks may still play; music what Music made.
	sounds map[*Effect]struct{}
	music  []*Music
	// effects are the bus DSP chains set per group.
	effects map[string]EffectChain
	// defaults are the volumes, by storage key, that SetDefaultMaster and SetDefaultVolume give keys the storage keeps none for.
	defaults map[string]float32
}

// NewMixer returns a mixer that keeps its volumes in cx's storage under keys starting with key, such as "sound", and lives as long as cx.
//
// The mixer mutes everything while cx's window is in the background (core.Env.Lifecycle), that is hidden, minimized or covered on a desktop and the app in the background on a phone: it silences the effects playing, pauses the music and, as the window shows again, restores the effects still playing and plays the music on from there.
func NewMixer(cx *core.Scope, key string) *Mixer {
	m := &Mixer{cx: cx, store: cx.Storage(), key: key, sounds: map[*Effect]struct{}{}}
	cx.Effect(func() {
		m.background = cx.Env().Lifecycle() == core.LifecycleBackground
		m.apply()
	})
	return m
}

// SetDefaultMaster sets the master volume the mixer has until SetMaster keeps one, clamped to 0 to 1, such as a project file's. It keeps nothing in the storage, so a volume the player set wins; set it before the game reads volumes, as it changes no subscribed computation.
func (m *Mixer) SetDefaultMaster(v float32) { m.setDefault(m.key+".master", v) }

// SetDefaultVolume sets the volume group has until SetVolume keeps one, clamped to 0 to 1, such as a project file's bus volume. It keeps nothing in the storage, so a volume the player set wins; set it before the game reads volumes, as it changes no subscribed computation.
func (m *Mixer) SetDefaultVolume(group string, v float32) { m.setDefault(m.groupKey(group), v) }

func (m *Mixer) setDefault(key string, v float32) {
	if v != v {
		v = 1
	}
	if m.defaults == nil {
		m.defaults = map[string]float32{}
	}
	m.defaults[key] = min(max(v, 0), 1)
	m.apply()
}

// Master returns the master volume, its default (SetDefaultMaster) or 1 until set. Read in a computation, it subscribes it to changes, as core.Storage does.
func (m *Mixer) Master() float32 { return m.load(m.key + ".master") }

// SetMaster sets the master volume, clamped to 0 to 1, and keeps it. It fails with a *core.StorageFullError when the storage is full, keeping the volume it had.
func (m *Mixer) SetMaster(v float32) error { return m.set(m.key+".master", v) }

// Volume returns group's volume: its default (SetDefaultVolume), or 1, for a group never set. Read in a computation, it subscribes it to changes, as core.Storage does.
func (m *Mixer) Volume(group string) float32 { return m.load(m.groupKey(group)) }

// SetVolume sets group's volume, clamped to 0 to 1, and keeps it. It fails with a *core.StorageFullError when the storage is full, keeping the volume it had.
func (m *Mixer) SetVolume(group string, v float32) error { return m.set(m.groupKey(group), v) }

func (m *Mixer) groupKey(group string) string { return m.key + ".group." + group }

// load reads the volume kept at key, key's default when none is or it is no number.
func (m *Mixer) load(key string) float32 {
	s, ok := m.store.GetItem(key)
	if !ok {
		return m.fallback(key)
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil || v != v {
		return m.fallback(key)
	}
	return min(max(float32(v), 0), 1)
}

// fallback is key's default volume, 1 when none is set.
func (m *Mixer) fallback(key string) float32 {
	if v, ok := m.defaults[key]; ok {
		return v
	}
	return 1
}

func (m *Mixer) set(key string, v float32) error {
	if v != v {
		v = 1
	}
	v = min(max(v, 0), 1)
	err := m.store.SetItem(key, strconv.FormatFloat(float64(v), 'g', -1, 32))
	m.apply()
	return err
}

// Suspend silences the mixer until resume is called, as the background does: it silences the sounds playing, plays no new ones and pauses the music, which plays on from where it paused once every Suspend has been resumed. A game suspends its sound while something else has the speakers, such as a cutscene (cutscene.Player does). Calling resume again does nothing.
func (m *Mixer) Suspend() (resume func()) {
	m.suspends++
	m.apply()
	resumed := false
	return func() {
		if resumed {
			return
		}
		resumed = true
		m.suspends--
		m.apply()
	}
}

// Suspended reports whether a Suspend has not been resumed yet.
func (m *Mixer) Suspended() bool { return m.suspends > 0 }

// gain is what group plays at: its volume times the master volume, 0 in the background or while suspended.
func (m *Mixer) gain(group string) float32 {
	if m.background || m.suspends > 0 {
		return 0
	}
	return core.Untracked(m.cx, func() float32 { return m.Master() * m.Volume(group) })
}

// apply sets the effects and the music playing to the volumes.
func (m *Mixer) apply() {
	for s := range m.sounds {
		if s.prune() == 0 {
			delete(m.sounds, s)
			continue
		}
		g := m.gain(s.Group)
		for _, p := range s.live {
			p.playback.SetVolume(p.volume * g)
		}
	}
	for _, mu := range m.music {
		mu.apply()
	}
}

// Effect is a short sound played through a mixer, in a group and with a limit of its own.
type Effect struct {
	// Group is the group whose volume the sound plays at.
	Group string
	// Limit is the most playbacks of the sound at once, so a burst of it does not take every voice; 0 for none but the platform's 32 for all sounds.
	Limit int
	m     *Mixer
	sound *audio.Sound
	// live are the playbacks that have not ended, oldest first.
	live []playback
}

// playback is a playback of an effect and its voice's own volume, which the mixer's volumes scale.
type playback struct {
	playback *audio.Playback
	volume   float32
}

// Effect returns s played through the mixer in group, at most limit at once (0 for no limit of its own). Close s as the app is done with it, as without the mixer.
func (m *Mixer) Effect(s *audio.Sound, group string, limit int) *Effect {
	return &Effect{Group: group, Limit: limit, m: m, sound: s}
}

// Play starts a playback of the sound at v's volume times its group's and the master volume, which follows those volumes as they change while it plays. It reports whether it asked for one: false while that volume is 0, as for a Muted voice, or while the app is in the background, and true otherwise, even where the backend's playback (audio.Playback) ends at once because it could not start. With Limit playbacks still playing it stops the oldest to make room, so the newest are heard; a playback that cannot start leaves them playing. A backend that cannot address playbacks plays it as it started, outside the limit and the volume changes.
func (s *Effect) Play(v audio.Voice) bool {
	volume := voiceVolume(v.Volume)
	if v.Muted {
		volume = 0
	}
	g := s.m.gain(s.Group) * volume
	if g <= 0 {
		return false
	}
	s.prune()
	v.Volume = g
	p := s.sound.Play(v)
	if ended(s.m.cx, p) {
		// The playback never started, so it takes no place and the ones already playing keep it.
		return true
	}
	s.live = append(s.live, playback{playback: p, volume: volume})
	// Only a playback that started makes room, so a backend that refuses one never costs an older sound.
	if s.Limit > 0 && len(s.live) > s.Limit {
		oldest := len(s.live) - s.Limit
		for _, q := range s.live[:oldest] {
			q.playback.Stop()
		}
		s.live = slices.Delete(s.live, 0, oldest)
	}
	s.m.sounds[s] = struct{}{}
	return true
}

// Stop stops every playback of the sound, and of every other sound of the same source, as audio.Sound.Stop does.
func (s *Effect) Stop() {
	s.sound.Stop()
	clear(s.live)
	s.live = s.live[:0]
	delete(s.m.sounds, s)
}

// prune drops the playbacks that ended from live and returns how many still play.
func (s *Effect) prune() int {
	n := 0
	for _, p := range s.live {
		if !ended(s.m.cx, p.playback) {
			s.live[n] = p
			n++
		}
	}
	clear(s.live[n:])
	s.live = s.live[:n]
	return n
}

// ended reports whether p ended, without subscribing the running computation to it.
func ended(cx *core.Scope, p *audio.Playback) bool {
	return core.Untracked(cx, p.Ended)
}

// voiceVolume is a voice's volume as audio plays it: from 0 to 1, 1 for 0.
func voiceVolume(v float32) float32 {
	if v == 0 || v != v {
		return 1
	}
	return min(max(v, 0), 1)
}

// Music plays long tracks through a mixer, one at a time, each through an audio.Player, crossfading from one to the next.
type Music struct {
	// Group is the group whose volume the music plays at.
	Group string
	// Loop has each track Play starts after it is set start over at its end.
	Loop bool
	m    *Mixer
	// current is the track playing or fading in, nil before Play and after Stop; out are the tracks fading out.
	current *track
	out     []*track
	// stopFade stops the fade running, nil when none does.
	stopFade func()
}

// track is a player of music and how loud it plays within its group.
type track struct {
	p *audio.Player
	// level is from 0 to 1, and from is where a fade out started.
	level, from float32
	// playing says the track was asked to play and not to pause since; done that it ended or failed, so it plays no more.
	playing, done bool
	// next is the armed loop track handed off to when an intro track ends, nil for a single track; preloaded says Preload was called on this track.
	next      *track
	preloaded bool
}

// Music returns music played through the mixer in group, which plays nothing until Play.
func (m *Mixer) Music(group string) *Music {
	mu := &Music{Group: group, m: m}
	m.music = append(m.music, mu)
	return mu
}

// Play plays src, fading it in over fade as the tracks playing fade out, by the display's frames (core.Scope.Animate), and closing them as they reach silence; it switches at once when fade is 0. A track that fails, such as one the platform cannot decode, plays nothing.
func (mu *Music) Play(src audio.Source, fade time.Duration) {
	var t *track
	mu.m.cx.Run(func() {
		t = &track{}
		end := func() { t.playing, t.done = false, true }
		t.p = audio.NewPlayer(mu.m.cx, src, audio.PlayerOptions{OnEnd: end, OnError: func(error) { end() }})
	})
	t.p.SetLoop(mu.Loop)
	mu.fadeTo(t, fade)
}

// Stop fades the music out over fade, closing its tracks as they reach silence; at once when fade is 0.
func (mu *Music) Stop(fade time.Duration) { mu.fadeTo(nil, fade) }

// fadeTo fades the current track and those fading out to silence over fade, and t, when not nil, in to full.
func (mu *Music) fadeTo(t *track, fade time.Duration) {
	if mu.stopFade != nil {
		mu.stopFade()
		mu.stopFade = nil
	}
	if mu.current != nil {
		mu.current.cancelNext()
		mu.out = append(mu.out, mu.current)
	}
	mu.current = t
	for _, o := range mu.out {
		o.from = o.level
	}
	step := func(progress float64) {
		if mu.current != nil {
			mu.current.level = float32(progress)
		}
		for _, o := range mu.out {
			o.level = o.from * float32(1-progress)
		}
		if progress >= 1 {
			for _, o := range mu.out {
				o.cancelNext()
				o.p.Close()
			}
			mu.out = nil
			mu.stopFade = nil
		}
		mu.apply()
	}
	if fade <= 0 {
		step(1)
		return
	}
	mu.m.cx.Run(func() { mu.stopFade = mu.m.cx.Animate(fade, core.Linear, step) })
	// The new track starts now, muted, rather than on the fade's first frame.
	mu.apply()
}

// apply sets each track's volume, pausing them all while the group cannot be heard and playing them otherwise.
func (mu *Music) apply() {
	g := mu.m.gain(mu.Group)
	if mu.current != nil {
		mu.current.apply(g)
	}
	for _, o := range mu.out {
		o.apply(g)
	}
}

// apply plays the track at gain times its level, muted at a level of 0, such as a track a crossfade starts, as an audio.Player takes a volume of 0 for 1; it pauses the track while gain is 0, so music in a group that cannot be heard waits for it to be.
func (t *track) apply(gain float32) {
	if t.done {
		return
	}
	if gain <= 0 {
		if t.playing {
			t.p.Pause()
			t.playing = false
		}
		return
	}
	v := gain * t.level
	t.p.SetMuted(v <= 0)
	if v > 0 {
		t.p.SetVolume(v)
	}
	if !t.playing {
		t.p.Play(nil)
		t.playing = true
	}
	if t.next != nil && !t.next.preloaded {
		t.next.p.Preload()
		t.next.preloaded = true
	}
}
