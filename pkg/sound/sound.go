package sound

import (
	"errors"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

// Mixer sets how loud a game's sounds and music play: a master volume and a volume for each group the app names, such as "effects", "music" and "ui", each from 0 to 1 and 1 until set. It keeps them in the app's storage (core.Storage), so they hold across launches, pauses gameplay groups with SetPaused while letting pause-exempt groups (SetPauseExempt) keep playing, and plays nothing while the app is in the background or suspended.
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
	// paused says gameplay audio is paused (as under a pause menu); groups not in pauseExempt are silenced and paused.
	paused bool
	// pauseExempt are the groups that keep playing while paused (such as "ui"); backgrounding and Suspend still silence them.
	pauseExempt map[string]bool
	// suspends counts the Suspend calls not resumed yet; nothing plays while it is above 0.
	suspends int
	// sounds are the effects whose playbacks may still play; music what Music made.
	sounds map[*Effect]struct{}
	music  []*Music
	// effects are the bus DSP chains set per group.
	effects map[string]EffectChain
	// dryLogged are the groups whose chain was already reported skipped for a source it cannot process; SetGroupEffects resets a group's entry.
	dryLogged map[string]bool
	// defaults are the volumes, by storage key, that SetDefaultMaster and SetDefaultVolume give keys the storage keeps none for.
	defaults map[string]float32
	// listener3D is the active 3D listener used by Effect.Play3D and Cue.Play3D; SetListener3D refreshes live Playback3D volumes.
	listener3D Listener3D
	// mono holds an explicit SetMono override when monoSet is true; otherwise systemMono applies.
	mono       bool
	monoSet    bool
	systemMono bool
	// balance offsets stereo voice panning in [-1, 1] when mono is off.
	balance float32
	// maxVoices is the global concurrent voice budget; <= 0 uses DefaultMaxVoices.
	maxVoices int
	// nextSeq assigns strictly increasing sequence numbers to started playbacks for deterministic oldest tie-breaking.
	nextSeq uint64
	// concurrency holds registered shared concurrency groups keyed by name.
	concurrency map[string]*concurrencyState
	// busDucks holds configured bus ducking rules and their live envelope states.
	busDucks []*busDuckState
	// fading holds stolen playbacks fading out over StealFade before stopping.
	fading []*fadingPlayback
	// syncingDuck guards syncBusDucking against re-entrant cycles.
	syncingDuck bool
}

// NewMixer returns a mixer that keeps its volumes in cx's storage under keys starting with key, such as "sound", and lives as long as cx.
//
// The mixer mutes everything while cx's window is in the background (core.Env.Lifecycle), that is hidden, minimized or covered on a desktop and the app in the background on a phone: it silences the effects playing, pauses the music and, as the window shows again, restores the effects still playing and plays the music on from there.
//
// Until SetMono is called, the mixer follows the platform's mono audio setting once NSGo exposes it on core.Env (nsgo-dev/nsgo#2677; false on NSGo v0.3.4).
func NewMixer(cx *core.Scope, key string) *Mixer {
	m := &Mixer{cx: cx, store: cx.Storage(), key: key, sounds: map[*Effect]struct{}{}}
	cx.Effect(func() {
		env := cx.Env()
		m.background = env.Lifecycle() == core.LifecycleBackground
		m.systemMono = envMonoAudio(env)
		m.apply()
	})
	return m
}

// envMonoAudio reads the platform's mono audio switch from env once NSGo exposes
// Env.MonoAudio (nsgo-dev/nsgo#2677); on NSGo v0.3.4 it returns false.
func envMonoAudio(env *core.Env) bool {
	if env == nil {
		return false
	}
	if m, ok := any(env).(interface{ MonoAudio() bool }); ok {
		return m.MonoAudio()
	}
	return false
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

// SetPaused pauses or resumes gameplay audio on the mixer, as when a pause menu opens or closes. While paused, every group not marked exempt with SetPauseExempt is silenced: effects playing are silenced through their audio.Playback without stopping, new Effect.Play calls play nothing, and music pauses at its current position. Groups marked exempt (such as "ui") keep playing while paused, though they are still silenced in the background or while suspended. When resumed, effects still playing return to their volume and music plays on from where it paused, unless the app is in the background or the mixer is suspended.
//
// Pair with game.Loop.OnPause (loop.OnPause = mixer.SetPaused) so gameplay audio pauses and resumes with the loop while backgrounding pauses and resumes everything in sync through core.Env.Lifecycle.
func (m *Mixer) SetPaused(paused bool) {
	if m.paused == paused {
		return
	}
	m.paused = paused
	m.apply()
}

// Paused reports whether gameplay audio is paused by SetPaused.
func (m *Mixer) Paused() bool { return m.paused }

// SetPauseExempt sets whether group keeps playing while the mixer is paused with SetPaused, such as a "ui" group for pause-menu sounds. Exempt groups are still silenced while the app is in the background or while the mixer is suspended (Suspend). Changing a group's exemption while paused applies at once to effects and music in that group.
func (m *Mixer) SetPauseExempt(group string, exempt bool) {
	if m.pauseExempt[group] == exempt {
		return
	}
	if exempt {
		if m.pauseExempt == nil {
			m.pauseExempt = map[string]bool{}
		}
		m.pauseExempt[group] = true
	} else {
		delete(m.pauseExempt, group)
	}
	if m.paused {
		m.apply()
	}
}

// PauseExempt reports whether group keeps playing while the mixer is paused with SetPaused.
func (m *Mixer) PauseExempt(group string) bool { return m.pauseExempt[group] }

// Suspend silences the mixer until resume is called, as the background does: it silences the sounds playing (including pause-exempt groups), plays no new ones and pauses the music, which plays on from where it paused once every Suspend has been resumed. A game suspends its sound while something else has the speakers, such as a cutscene (cutscene.Player does). Calling resume again does nothing.
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

// SetMono explicitly enables or disables mono voice panning on the mixer, overriding the platform's mono audio switch.
//
// While mono is active, every voice the mixer starts (via Effect.Play, Effect.PlayAt, Effect.PlayAt3D, Effect.Play3D, and Cue variants) or updates plays centered at pan 0 while keeping distance, rear, and cone attenuation unchanged, so distance cues remain and listeners still report the true direction for directional captions.
//
// Stereo audio files (such as stereo music or ambiences) keep their channels until NSGo supports mixing the output down to mono (nsgo-dev/nsgo#2676). Call FollowSystemMono to clear the override and follow the platform setting again.
func (m *Mixer) SetMono(mono bool) {
	if m == nil {
		return
	}
	if m.monoSet && m.mono == mono {
		return
	}
	m.mono = mono
	m.monoSet = true
	m.apply()
}

// Mono reports whether mono voice panning is currently active on the mixer, either from SetMono or from the platform's mono audio switch (nsgo-dev/nsgo#2677).
func (m *Mixer) Mono() bool {
	if m == nil {
		return false
	}
	if m.monoSet {
		return m.mono
	}
	return m.systemMono
}

// FollowSystemMono clears any explicit SetMono override so the mixer follows the platform's mono audio setting (nsgo-dev/nsgo#2677; false on NSGo v0.3.4).
func (m *Mixer) FollowSystemMono() {
	if m == nil || !m.monoSet {
		return
	}
	m.mono = false
	m.monoSet = false
	m.apply()
}

// SetBalance sets the stereo left/right balance offset in [-1, 1] (-1 full left, 0 centered, +1 full right). Values outside [-1, 1] clamp, and NaN resets to 0.
//
// When mono is off, every voice the mixer starts or updates adds balance to its pan and clamps to [-1, 1]. While mono is on, every voice plays at pan 0 while the configured balance is kept for when mono is turned off.
func (m *Mixer) SetBalance(balance float32) {
	if m == nil {
		return
	}
	b := clampPan(balance)
	if m.balance == b {
		return
	}
	m.balance = b
	m.apply()
}

// Balance returns the mixer's stereo left/right balance offset in [-1, 1], 0 until set.
func (m *Mixer) Balance() float32 {
	if m == nil {
		return 0
	}
	return m.balance
}

func clampPan(v float32) float32 {
	if v != v {
		return 0
	}
	p := min(max(v, -1), 1)
	if p == 0 {
		return 0
	}
	return p
}

// effectivePan resolves a voice's requested stereo pan against the mixer's mono and balance settings.
func (m *Mixer) effectivePan(pan float32) float32 {
	if m.Mono() {
		return 0
	}
	return clampPan(clampPan(pan) + m.balance)
}

// panSetter matches NSGo's audio.Playback.SetPan once available (nsgo-dev/nsgo#2676; absent on NSGo v0.3.4).
type panSetter interface {
	SetPan(float32)
}

func setPlaybackPan(p *audio.Playback, pan float32) {
	if ps, ok := any(p).(panSetter); ok {
		ps.SetPan(pan)
	}
}

// Unblock retries any music tracks whose playback was refused with audio.NotAllowed, such as tracks started before the page's first user gesture on the web. Blocked tracks keep their group volume, crossfade level, loop settings and any armed intro handoff, starting from position 0 once audible; tracks in a silent, paused, suspended or backgrounded mixer clear their blocked state and start as soon as the group becomes audible. Calling Unblock when no track is blocked does nothing.
//
// Call Unblock from a press, touch or key handler, or assign the mixer to input.Actions.Mixer so the first press, touch or key press retries automatically until NSGo reports audio unlock directly (nsgo-dev/nsgo#2675).
func (m *Mixer) Unblock() {
	if m == nil {
		return
	}
	for _, mu := range m.music {
		if mu.unblock() {
			mu.syncBlocked()
			mu.apply()
		}
	}
}

// baseGain is group's un-ducked gain: its volume times the master volume, 0 in the background, while suspended, or while paused unless group is exempt from game pause.
func (m *Mixer) baseGain(group string) float32 {
	if m.background || m.suspends > 0 {
		return 0
	}
	if m.paused && !m.pauseExempt[group] {
		return 0
	}
	return core.Untracked(m.cx, func() float32 { return m.Master() * m.Volume(group) })
}

// gain is what group plays at: its base gain times any active bus ducking multiplier.
func (m *Mixer) gain(group string) float32 {
	g := m.baseGain(group)
	if g <= 0 {
		return 0
	}
	return g * m.busDuckGain(group)
}

// apply sets the effects and the music playing to the volumes and updates pan where supported.
func (m *Mixer) apply() {
	for s := range m.sounds {
		if s.prune() == 0 {
			delete(m.sounds, s)
			continue
		}
		g := m.gain(s.Group)
		for _, p := range s.live {
			p.playback.SetVolume(p.effectiveVolume(g))
			setPlaybackPan(p.playback, m.effectivePan(p.basePan()))
		}
	}
	n := 0
	for _, f := range m.fading {
		if ended(m.cx, f.playback) {
			f.cleanup()
			continue
		}
		g := m.gain(f.effect.Group)
		f.playback.SetVolume(f.effectiveVolume(g))
		setPlaybackPan(f.playback, m.effectivePan(f.basePan()))
		m.fading[n] = f
		n++
	}
	clear(m.fading[n:])
	m.fading = m.fading[:n]
	for _, mu := range m.music {
		mu.apply()
	}
	m.syncBusDucking()
}

// Effect is a short sound played through a mixer, in a group, optionally belonging to a shared concurrency group, with a priority and per-sound voice limit.
type Effect struct {
	// Group is the group (bus) whose volume the sound plays at.
	Group string
	// Limit is the most playbacks of the sound at once, so a burst of it does not take every voice; 0 for no per-effect limit (still subject to any concurrency group and the mixer's global voice budget).
	Limit int
	// Concurrency is the optional name of a shared ConcurrencyGroup registered on the mixer.
	Concurrency string
	// Priority ranks this sound during ResolveLowestPriority and global voice-budget stealing; higher wins.
	Priority int
	m        *Mixer
	sound    *audio.Sound
	// live are the active non-fading playbacks that have not ended, oldest first.
	live []*playback
	// caption and captionLabel are the optional callback SetCaption installs for positional plays.
	caption      func(label string, pan float32)
	captionLabel string
}

// SetCaption installs callback to run with label and the listener pan each time PlayAt, PlayAt3D, or their With and Cue forms start a playback of the effect, so an a11y.CaptionEmitter can show a caption in the direction the player hears without the sound package depending on a11y. A nil callback removes it. Plain Play has no position and does not call it.
func (s *Effect) SetCaption(label string, callback func(label string, pan float32)) {
	if s == nil {
		return
	}
	s.captionLabel, s.caption = label, callback
}

func (s *Effect) emitCaption(pan float32) {
	if s.caption != nil {
		s.caption(s.captionLabel, pan)
	}
}

// playback is a playback of an effect and its voice's own volume, base pan, concurrency metadata, and duck envelope.
// When spatial is non-nil, the playback is tracked in 3D and spatial.spatial provides its live Gain, Distance, and base Pan.
type playback struct {
	playback    *audio.Playback
	volume      float32
	pan         float32
	distance    float32
	spatial     *Playback3D
	seq         uint64
	concurrency string
	owner       any
	priority    int
	duckLevel   float32
	duckTarget  float32
	stopDuck    func()
	stopWatch   func()
}

func (p *playback) effectiveVolume(groupGain float32) float32 {
	v := p.volume * groupGain * p.duckLevel
	if p.spatial != nil {
		v *= p.spatial.spatial.Gain
	}
	return v
}

func (p *playback) basePan() float32 {
	if p.spatial != nil {
		return p.spatial.spatial.Pan
	}
	return p.pan
}

func (p *playback) cleanup() {
	if p.stopDuck != nil {
		p.stopDuck()
		p.stopDuck = nil
	}
	if p.stopWatch != nil {
		p.stopWatch()
		p.stopWatch = nil
	}
}

// Effect returns s played through the mixer in group, at most limit at once (0 for no limit of its own). Close s as the app is done with it, as without the mixer.
//
// The group's bus effect chain (SetGroupEffects) does not apply here: s is already decoded, so it plays dry. Load a source through LoadEffect to play it with the chain applied.
func (m *Mixer) Effect(s *audio.Sound, group string, limit int) *Effect {
	return &Effect{Group: group, Limit: limit, m: m, sound: s}
}

// LoadEffect loads src as an effect played through the mixer in group, at most limit at once, as Effect does for a sound already loaded, applying group's bus effect chain (SetGroupEffects) first: a source holding in-memory 16-bit PCM WAV data is processed through the chain before loading, and any other source loads unchanged, with one log line per group saying its effects are skipped for it. done receives the effect or why it did not load, as audio.LoadSound's callback does.
func (m *Mixer) LoadEffect(src audio.Source, group string, limit int, done func(*Effect, error)) {
	audio.LoadSound(m.cx, m.process(group, src), func(s *audio.Sound, err error) {
		if err != nil {
			done(nil, err)
			return
		}
		done(m.Effect(s, group, limit), nil)
	})
}

// Play starts a playback of the sound at v's volume times its group's and the master volume, which follows those volumes as they change while it plays. Its pan is centered to 0 when the mixer is in mono mode (SetMono) and otherwise offset by the mixer's balance (SetBalance) and clamped to [-1, 1]. It reports whether it asked for one: false while that volume is 0, as for a Muted voice, while the app is in the background, suspended, or the effect's group is paused, or when refused by a concurrency group, retrigger cooldown, or global voice budget, and true otherwise, even where the backend's playback (audio.Playback) ends at once because it could not start. With Limit playbacks still playing it stops the oldest to make room, so the newest are heard; a playback that cannot start leaves them playing. A backend that cannot address playbacks plays it as it started, outside the limit and the volume changes.
func (s *Effect) Play(v audio.Voice) bool {
	return s.PlayWith(v, PlayOptions{})
}

// PlayWith behaves like Play while applying per-call concurrency, owner, and priority overrides from opts.
func (s *Effect) PlayWith(v audio.Voice, opts PlayOptions) bool {
	if s == nil || s.m == nil || s.sound == nil {
		return false
	}
	_, ok := s.playInternal(v, 0, nil, opts)
	return ok
}

// playInternal runs the full concurrency, retrigger, per-effect Limit, global voice budget, ducking, and playback pipeline.
// When spatial3D is non-nil, spatial3D.spatial supplies distance, gain, and base pan; otherwise v.Volume and v.Pan are used directly with dist.
func (s *Effect) playInternal(v audio.Voice, dist float32, spatial3D *Playback3D, opts PlayOptions) (*playback, bool) {
	baseVol := voiceVolume(v.Volume)
	if v.Muted {
		baseVol = 0
	}
	spatialGain := float32(1)
	basePan := clampPan(v.Pan)
	if spatial3D != nil {
		spatialGain = spatial3D.spatial.Gain
		basePan = spatial3D.spatial.Pan
		dist = spatial3D.spatial.Distance
	}
	if dist != dist || dist < 0 {
		dist = 0
	}
	baseBusGain := s.m.baseGain(s.Group)
	if !(baseBusGain*baseVol*spatialGain > 0) {
		return nil, false
	}

	m := s.m
	m.pruneAll()

	groupName := opts.Concurrency
	if groupName == "" {
		groupName = s.Concurrency
	}
	priority := s.Priority
	if opts.HasPriority || opts.Priority != 0 {
		priority = opts.Priority
	}
	ownerKey := normalizeOwner(opts.Owner)

	var (
		cState       *concurrencyState
		hasGroup     bool
		groupVictims []voiceRef
		stealFade    time.Duration
		now          time.Time
	)
	if groupName != "" && m.concurrency != nil {
		cState, hasGroup = m.concurrency[groupName]
	}

	// 1. Retrigger cooldown check (before stealing any voices).
	if hasGroup && cState.cfg.Retrigger > 0 {
		now = m.now()
		scopeOwner := any(nil)
		if cState.cfg.Scope == ScopeOwner {
			scopeOwner = ownerKey
		}
		if last, ok := cState.lastTrigger[scopeOwner]; ok && now.Sub(last) < cState.cfg.Retrigger {
			cState.retriggered++
			cState.refused++
			return nil, false
		}
	}

	audibleVol := baseBusGain * m.busDuckGain(s.Group) * baseVol * spatialGain
	cand := candidateMetrics{
		volume:   audibleVol,
		distance: dist,
		priority: priority,
	}

	// 2. Concurrency group cap & resolution rule check.
	if hasGroup && cState.cfg.Max > 0 {
		active := m.collectGroupActive(groupName, cState.cfg.Scope, ownerKey)
		admit, victims := m.selectGroupVictims(cState.cfg, active, cand)
		if !admit {
			cState.refused++
			return nil, false
		}
		groupVictims = victims
		stealFade = cState.cfg.StealFade
	}

	excluded := make(map[*playback]struct{}, len(groupVictims)+2)
	for _, gv := range groupVictims {
		excluded[gv.pb] = struct{}{}
	}

	// 3. Per-effect Limit check (excluding voices already scheduled to be stolen by the concurrency group).
	var effectVictims []voiceRef
	if s.Limit > 0 {
		var rem []*playback
		for _, p := range s.live {
			if _, skip := excluded[p]; !skip {
				rem = append(rem, p)
			}
		}
		if len(rem) >= s.Limit {
			need := len(rem) - s.Limit + 1
			for _, p := range rem[:need] {
				effectVictims = append(effectVictims, voiceRef{effect: s, pb: p})
				excluded[p] = struct{}{}
			}
		}
	}

	// 4. Global voice budget check (priority stealing across all groups).
	remainingGlobal := m.collectAllActiveExcluding(excluded)
	admitGlobal, globalVictims := m.selectGlobalVictims(remainingGlobal, priority)
	if !admitGlobal {
		if hasGroup {
			cState.refused++
		}
		return nil, false
	}

	// Ensure hardware slot headroom below platformMaxVoices (32) before starting backend playback.
	m.freeHardwareHeadroom(1)
	if m.totalLiveHardwareVoices() >= platformMaxVoices {
		// Hard-stop planned immediate victims first if we are right at the 32-voice platform ceiling.
		for _, ev := range effectVictims {
			m.stealVoice(ev, 0)
		}
		effectVictims = nil
		for _, gv := range globalVictims {
			m.stealVoice(gv, 0)
		}
		globalVictims = nil
		if m.totalLiveHardwareVoices() >= platformMaxVoices && len(groupVictims) > 0 {
			for _, gv := range groupVictims {
				m.stealVoice(gv, 0)
				if hasGroup {
					cState.stolen++
				}
			}
			groupVictims = nil
		}
	}

	v.Volume = audibleVol
	v.Pan = m.effectivePan(basePan)
	p := s.sound.Play(v)
	if spatial3D != nil {
		spatial3D.playback = p
	}
	if ended(m.cx, p) {
		// The playback never started, so it takes no place and the ones already playing keep it.
		return nil, true
	}

	// Playback started: record retrigger timestamp and apply planned steals.
	if hasGroup && cState.cfg.Retrigger > 0 {
		if now.IsZero() {
			now = m.now()
		}
		if cState.lastTrigger == nil {
			cState.lastTrigger = map[any]time.Time{}
		}
		scopeOwner := any(nil)
		if cState.cfg.Scope == ScopeOwner {
			scopeOwner = ownerKey
			// Drop owners whose cooldown has expired so a group played by many short-lived emitters does not keep one entry per emitter forever.
			for k, t := range cState.lastTrigger {
				if now.Sub(t) >= cState.cfg.Retrigger {
					delete(cState.lastTrigger, k)
				}
			}
		}
		cState.lastTrigger[scopeOwner] = now
	}

	for _, gv := range groupVictims {
		m.stealVoice(gv, stealFade)
		if hasGroup {
			cState.stolen++
		}
	}
	for _, ev := range effectVictims {
		m.stealVoice(ev, 0)
	}
	for _, gv := range globalVictims {
		m.stealVoice(gv, 0)
	}

	m.nextSeq++
	entry := &playback{
		playback:    p,
		volume:      baseVol,
		pan:         basePan,
		distance:    dist,
		spatial:     spatial3D,
		seq:         m.nextSeq,
		concurrency: groupName,
		owner:       ownerKey,
		priority:    priority,
		duckLevel:   1,
		duckTarget:  1,
	}
	if spatial3D != nil {
		spatial3D.entry = entry
	}
	s.live = append(s.live, entry)
	m.sounds[s] = struct{}{}

	if m.needsWatcher(s.Group, groupName) {
		m.attachWatcher(entry)
	}
	m.syncGroupDucking()
	m.syncBusDucking()
	// Re-apply effective volume in case bus ducking snapped immediately on start.
	if finalVol := entry.effectiveVolume(m.gain(s.Group)); finalVol != audibleVol {
		p.SetVolume(finalVol)
	}
	return entry, true
}

// Stop stops every playback of the sound, and of every other sound of the same source, as audio.Sound.Stop does.
func (s *Effect) Stop() {
	if s == nil || s.m == nil {
		return
	}
	s.sound.Stop()
	for _, p := range s.live {
		p.cleanup()
	}
	clear(s.live)
	s.live = s.live[:0]
	delete(s.m.sounds, s)
	n := 0
	for _, f := range s.m.fading {
		if f.effect == s {
			f.cleanup()
			continue
		}
		s.m.fading[n] = f
		n++
	}
	clear(s.m.fading[n:])
	s.m.fading = s.m.fading[:n]
	s.m.syncGroupDucking()
	s.m.syncBusDucking()
}

// prune drops the playbacks that ended from live and returns how many still play.
func (s *Effect) prune() int {
	n := 0
	for _, p := range s.live {
		if ended(s.m.cx, p.playback) {
			p.cleanup()
			continue
		}
		s.live[n] = p
		n++
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
	// blocked mirrors whether current is currently blocked by audio.NotAllowed for reactive views calling Blocked.
	blocked *core.Signal[bool]
}

// track is a player of music and how loud it plays within its group.
type track struct {
	p *audio.Player
	// level is from 0 to 1, and from is where a fade out started.
	level, from float32
	// playing says the track was asked to play and not to pause since; done that it ended or failed permanently, so it plays no more; blocked that playback was refused with audio.NotAllowed and waits for Mixer.Unblock.
	playing, done, blocked bool
	// next is the armed loop track handed off to when an intro track finishes, nil for a single track; preloaded says Preload was called on this track.
	next      *track
	preloaded bool
	// stopHandoff cancels a scheduled pre-end handoff timer for a two-file intro, nil when none is armed.
	stopHandoff func() bool
	// prev is a two-file intro finishing its remaining samples after an early handoff into this track, closed when its OnEnd runs or when this track stops or fades out.
	prev *track
}

// Music returns music played through the mixer in group, which plays nothing until Play.
func (m *Mixer) Music(group string) *Music {
	mu := &Music{Group: group, m: m, blocked: core.NewSignal(m.cx, false)}
	m.music = append(m.music, mu)
	return mu
}

// Blocked reports whether the current track is waiting to retry after the platform refused playback with audio.NotAllowed (such as browser autoplay before the page's first user gesture). Read in a computation, it subscribes it to changes, so a game can show a "tap for sound" hint until Mixer.Unblock retries the track or the track is stopped or replaced.
func (mu *Music) Blocked() bool {
	if mu == nil || mu.blocked == nil {
		return false
	}
	return mu.blocked.Get()
}

func (mu *Music) syncBlocked() {
	if mu == nil || mu.blocked == nil {
		return
	}
	blocked := mu.current != nil && mu.current.blocked
	if mu.blocked.GetUntracked() != blocked {
		mu.blocked.Set(blocked)
	}
}

func (mu *Music) unblock() bool {
	any := false
	clearBlocked := func(t *track) {
		for ; t != nil; t = t.prev {
			if t.blocked {
				t.blocked = false
				any = true
			}
			if t.next != nil && t.next.blocked {
				t.next.blocked = false
				any = true
			}
		}
	}
	clearBlocked(mu.current)
	for _, o := range mu.out {
		clearBlocked(o)
	}
	return any
}

func (mu *Music) handleError(t *track, err error, isIntro bool) {
	if errors.Is(err, audio.NotAllowed) {
		t.playing = false
		t.blocked = true
		t.cancelHandoff()
		mu.syncBlocked()
		mu.m.syncBusDucking()
		return
	}
	t.playing, t.blocked, t.done = false, false, true
	if isIntro {
		t.cancelNext()
		mu.finishPrev(t)
	}
	mu.syncBlocked()
	mu.m.syncBusDucking()
}

func (mu *Music) activeForDucking() bool {
	if mu == nil {
		return false
	}
	if mu.current != nil && mu.current.playing && !mu.current.done && !mu.current.blocked {
		return true
	}
	for _, o := range mu.out {
		if o.playing && !o.done && !o.blocked {
			return true
		}
	}
	return false
}

// Play plays src, fading it in over fade as the tracks playing fade out, by the display's frames (core.Scope.Animate), and closing them as they reach silence; it switches at once when fade is 0. A track that fails with audio.NotAllowed stays current and blocked until Mixer.Unblock retries it; any other failure, such as a track the platform cannot decode, ends the track and plays nothing.
//
// When the music's group has a bus effect chain (SetGroupEffects), a src holding in-memory 16-bit PCM WAV data is processed through the chain before playing, as ProcessSource does; any other source, such as an MP3 or AAC file or a URI, plays unchanged, with one log line per group saying its effects are skipped for it.
func (mu *Music) Play(src audio.Source, fade time.Duration) {
	src = mu.m.process(mu.Group, src)
	mu.playSingle(src, mu.Loop, 0, fade)
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
		if mu.current.prev != nil {
			mu.out = append(mu.out, mu.current.prev)
			mu.current.prev = nil
		}
		mu.out = append(mu.out, mu.current)
	}
	mu.current = t
	mu.syncBlocked()
	for _, o := range mu.out {
		o.from = o.level
	}
	step := func(progress float64) {
		if mu.current != nil {
			mu.current.level = float32(progress)
			if mu.current.prev != nil {
				mu.current.prev.level = mu.current.level
			}
		}
		for _, o := range mu.out {
			o.level = o.from * float32(1-progress)
		}
		if progress >= 1 {
			for _, o := range mu.out {
				o.cancelNext()
				if o.prev != nil {
					o.prev.p.Close()
					o.prev = nil
				}
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
	g := mu.m.baseGain(mu.Group)
	duck := mu.m.busDuckGain(mu.Group)
	if mu.current != nil {
		resumed := mu.current.apply(g, duck)
		if resumed && mu.current.next != nil {
			mu.scheduleHandoff(mu.current, mu.current.p.Position(), mu.current.p.Duration())
		}
	}
	for _, o := range mu.out {
		o.apply(g, duck)
	}
	mu.m.syncBusDucking()
}

// apply plays the track at gain times its level times duck, muted at a level or duck of 0, such as a track a crossfade starts or a fully ducked bus, as an audio.Player takes a volume of 0 for 1; it pauses the track while base gain is 0, so music in a group that cannot be heard waits for it to be.
func (t *track) apply(gain, duck float32) (started bool) {
	if t.prev != nil {
		t.prev.apply(gain, duck)
	}
	if t.done {
		return false
	}
	if gain <= 0 {
		if t.playing {
			t.p.Pause()
			t.playing = false
			t.cancelHandoff()
		}
		return false
	}
	v := gain * t.level * duck
	t.p.SetMuted(v <= 0)
	if v > 0 {
		t.p.SetVolume(v)
	}
	if t.blocked {
		return false
	}
	if !t.playing {
		t.p.Play(nil)
		t.playing = true
		started = true
	}
	if t.next != nil && !t.next.preloaded {
		t.next.p.Preload()
		t.next.preloaded = true
	}
	return started
}
