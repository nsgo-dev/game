package sound

import (
	"math"
	"reflect"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// DefaultMaxVoices is the default global concurrent voice budget on a Mixer,
	// kept below the platform's 32-voice hardware limit so brief steal fade-outs
	// never cause the backend to hard-drop high-priority sounds.
	DefaultMaxVoices = 30

	// platformMaxVoices is NSGo's cross-platform concurrent sound voice cap.
	platformMaxVoices = 32
)

// ConcurrencyResolve chooses how a ConcurrencyGroup resolves a new sound when its Max active voice limit is reached.
type ConcurrencyResolve uint8

const (
	// ResolveRefuse prevents the new sound from playing when the concurrency limit is reached.
	ResolveRefuse ConcurrencyResolve = iota
	// ResolveOldest stops the oldest active voice in the concurrency scope to make room for the new sound.
	ResolveOldest
	// ResolveFarthest stops the farthest active voice when the new sound is at least as close, and refuses the new sound otherwise.
	ResolveFarthest
	// ResolveQuietest stops the quietest active voice when the new sound is at least as loud, and refuses the new sound otherwise.
	ResolveQuietest
	// ResolveLowestPriority stops the lowest-priority active voice (ties broken by oldest) when the new sound's priority is at least as high, and refuses the new sound otherwise.
	ResolveLowestPriority
)

// String returns the canonical YAML name of r.
func (r ConcurrencyResolve) String() string {
	switch r {
	case ResolveOldest:
		return "oldest"
	case ResolveFarthest:
		return "farthest"
	case ResolveQuietest:
		return "quietest"
	case ResolveLowestPriority:
		return "lowestPriority"
	default:
		return "refuse"
	}
}

// ConcurrencyScope chooses whether a ConcurrencyGroup's voice limit, retrigger time, and ducking apply across the whole group or independently per owner.
type ConcurrencyScope uint8

const (
	// ScopeGlobal shares the concurrency limit, retrigger cooldown, and older-voice ducking across every sound in the group.
	ScopeGlobal ConcurrencyScope = iota
	// ScopeOwner scopes the concurrency limit, retrigger cooldown, and older-voice ducking independently per PlayOptions.Owner.
	ScopeOwner
)

// String returns the canonical YAML name of s.
func (s ConcurrencyScope) String() string {
	switch s {
	case ScopeOwner:
		return "owner"
	default:
		return "global"
	}
}

// DuckEnvelope configures how much and how quickly a ducking reduction engages and releases.
type DuckEnvelope struct {
	// Amount is the volume reduction fraction in [0, 1]: 0 disables ducking (1.0x gain),
	// 0.4 lowers volume by 40% (0.6x gain), and 1 lowers volume to silence (0x gain).
	Amount float32
	// Attack is how quickly the ducking reduction ramps in; 0 or negative applies immediately.
	Attack time.Duration
	// Release is how quickly volume recovers after the ducking sound finishes; 0 or negative restores immediately.
	Release time.Duration
}

// ConcurrencyGroup defines shared voice limiting, resolution, retrigger cooldown, steal fade-out,
// and older-voice ducking across all Effects and Cues assigned to a group name.
type ConcurrencyGroup struct {
	// Max is the maximum number of simultaneous active voices in this group (or per owner when Scope is ScopeOwner).
	// Values <= 0 impose no group voice cap (global voice budget, retrigger, and ducking still apply).
	Max int
	// Resolve chooses which voice to drop (or whether to refuse the new sound) when Max is reached.
	// Defaults to ResolveRefuse.
	Resolve ConcurrencyResolve
	// Scope chooses whether Max, Retrigger, and Duck apply globally across the group or independently per owner.
	// Defaults to ScopeGlobal.
	Scope ConcurrencyScope
	// Retrigger is the minimum duration that must elapse between accepted voice starts in this group (or owner).
	// Calls arriving sooner are refused without stopping existing voices.
	Retrigger time.Duration
	// StealFade is the fade-out duration applied to voices stolen by this group's resolution rule before stopping them.
	// 0 or negative stops stolen voices immediately.
	StealFade time.Duration
	// Duck lowers older active voices in the same concurrency scope while newer voices play,
	// ramping in over Duck.Attack and recovering over Duck.Release as newer voices finish.
	Duck DuckEnvelope
}

// BusDuckRule lowers Bus's volume while any voice or music plays on By.
type BusDuckRule struct {
	// Bus is the mixer bus (group) whose volume is lowered.
	Bus string
	// By is the triggering mixer bus (or concurrency group name) whose active sounds trigger ducking.
	By string
	// Amount is the volume reduction fraction in [0, 1]: 0 disables ducking, 0.4 lowers Bus by 40% (0.6x gain), and 1 silences Bus.
	Amount float32
	// Attack is how quickly Bus fades down when By starts playing; 0 or negative applies immediately.
	Attack time.Duration
	// Release is how quickly Bus recovers after all sounds on By finish; 0 or negative restores immediately.
	Release time.Duration
}

// PlayOptions configures per-playback concurrency and priority settings for Effect.PlayWith,
// Effect.PlayAtWith, Effect.PlayAt3DWith, Effect.Play3DWith, and their Cue counterparts.
type PlayOptions struct {
	// Concurrency overrides Effect.Concurrency for this playback when non-empty.
	Concurrency string
	// Owner identifies the sound's emitter (for example an entity ID, string, or pointer) when the
	// concurrency group uses ScopeOwner. Any comparable value may be used; nil or uncomparable values
	// fall back to the shared owner bucket.
	Owner any
	// Priority ranks this playback during ResolveLowestPriority and global voice-budget stealing (higher wins).
	// When HasPriority is true or Priority is non-zero, it overrides Effect.Priority.
	Priority int
	// HasPriority forces Priority (including 0) to override Effect.Priority.
	HasPriority bool
}

// MixerDebugSnapshot summarizes active voice usage, concurrency groups, and bus ducking on a Mixer
// for debug overlays and MCP inspection.
type MixerDebugSnapshot struct {
	// ActiveVoices is the total number of live non-fading effect voices currently playing across the mixer.
	ActiveVoices int `json:"active_voices"`
	// FadingVoices is the number of stolen voices currently fading out before stopping.
	FadingVoices int `json:"fading_voices,omitempty"`
	// MaxVoices is the mixer's global voice budget.
	MaxVoices int `json:"max_voices"`
	// Groups lists concurrency group telemetry sorted by group name.
	Groups []ConcurrencyGroupDebug `json:"groups,omitempty"`
	// Buses lists active bus voice counts and current ducking gain multipliers sorted by bus name.
	Buses []BusDebug `json:"buses,omitempty"`
}

// ConcurrencyGroupDebug reports live voice counts, configuration, and counters for one concurrency group.
type ConcurrencyGroupDebug struct {
	Name         string `json:"name"`
	Active       int    `json:"active"`
	Max          int    `json:"max"`
	Resolve      string `json:"resolve"`
	Scope        string `json:"scope"`
	ActiveOwners int    `json:"active_owners,omitempty"`
	Refused      uint64 `json:"refused,omitempty"`
	Stolen       uint64 `json:"stolen,omitempty"`
	Retriggered  uint64 `json:"retriggered,omitempty"`
}

// BusDebug reports active voice count and current ducking gain multiplier for one mixer bus.
type BusDebug struct {
	Name         string  `json:"name"`
	ActiveVoices int     `json:"active_voices"`
	DuckGain     float32 `json:"duck_gain"`
}

type concurrencyState struct {
	cfg         ConcurrencyGroup
	lastTrigger map[any]time.Time
	refused     uint64
	stolen      uint64
	retriggered uint64
}

type busDuckState struct {
	rule     BusDuckRule
	level    float32
	target   float32
	stopAnim func()
}

type fadingPlayback struct {
	effect     *Effect
	playback   *audio.Playback
	volume     float32
	pan        float32
	spatial    *Playback3D
	duckLevel  float32
	stealLevel float32
	stopAnim   func()
	stopWatch  func()
}

func (f *fadingPlayback) effectiveVolume(groupGain float32) float32 {
	v := f.volume * groupGain * f.duckLevel * f.stealLevel
	if f.spatial != nil {
		v *= f.spatial.spatial.Gain
	}
	return v
}

func (f *fadingPlayback) basePan() float32 {
	if f.spatial != nil {
		return f.spatial.spatial.Pan
	}
	return f.pan
}

func (f *fadingPlayback) cleanup() {
	if f.stopAnim != nil {
		f.stopAnim()
		f.stopAnim = nil
	}
	if f.stopWatch != nil {
		f.stopWatch()
		f.stopWatch = nil
	}
}

func sanitizeDuckAmount(a float32) float32 {
	if a != a || a <= 0 {
		return 0
	}
	if a > 1 {
		return 1
	}
	return a
}

func sanitizeDuration(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

func sanitizeConcurrencyGroup(cfg ConcurrencyGroup) ConcurrencyGroup {
	if cfg.Max < 0 {
		cfg.Max = 0
	}
	cfg.Retrigger = sanitizeDuration(cfg.Retrigger)
	cfg.StealFade = sanitizeDuration(cfg.StealFade)
	cfg.Duck.Amount = sanitizeDuckAmount(cfg.Duck.Amount)
	cfg.Duck.Attack = sanitizeDuration(cfg.Duck.Attack)
	cfg.Duck.Release = sanitizeDuration(cfg.Duck.Release)
	return cfg
}

func sanitizeBusDuckRule(r BusDuckRule) (BusDuckRule, bool) {
	if r.Bus == "" || r.By == "" || r.Bus == r.By {
		return BusDuckRule{}, false
	}
	r.Amount = sanitizeDuckAmount(r.Amount)
	r.Attack = sanitizeDuration(r.Attack)
	r.Release = sanitizeDuration(r.Release)
	if r.Amount <= 0 {
		return BusDuckRule{}, false
	}
	return r, true
}

func normalizeOwner(owner any) any {
	if owner == nil {
		return nil
	}
	switch owner.(type) {
	case string, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64, bool:
		return owner
	}
	t := reflect.TypeOf(owner)
	if t != nil && t.Comparable() {
		return owner
	}
	return nil
}

// SetMaxVoices sets the mixer's global concurrent voice budget across all effects and concurrency groups.
// Values <= 0 reset the budget to DefaultMaxVoices (30). When the budget is reached, higher- or
// equal-priority sounds steal the lowest-priority (oldest on ties) active voice, while strictly
// lower-priority sounds are refused.
func (m *Mixer) SetMaxVoices(maxVoices int) {
	if m == nil {
		return
	}
	if maxVoices <= 0 {
		m.maxVoices = DefaultMaxVoices
		return
	}
	m.maxVoices = maxVoices
}

// MaxVoices returns the mixer's global concurrent voice budget (DefaultMaxVoices until set).
func (m *Mixer) MaxVoices() int {
	if m == nil || m.maxVoices <= 0 {
		return DefaultMaxVoices
	}
	return m.maxVoices
}

// SetConcurrencyGroup registers or updates a named shared ConcurrencyGroup on m.
// Empty names are ignored.
func (m *Mixer) SetConcurrencyGroup(name string, cfg ConcurrencyGroup) {
	if m == nil || name == "" {
		return
	}
	cfg = sanitizeConcurrencyGroup(cfg)
	if m.concurrency == nil {
		m.concurrency = map[string]*concurrencyState{}
	}
	if st, ok := m.concurrency[name]; ok {
		st.cfg = cfg
	} else {
		m.concurrency[name] = &concurrencyState{cfg: cfg}
	}
	m.ensureWatchers()
	m.syncGroupDucking()
	m.apply()
}

// ConcurrencyGroup returns the named ConcurrencyGroup configuration and whether it is registered on m.
func (m *Mixer) ConcurrencyGroup(name string) (ConcurrencyGroup, bool) {
	if m == nil || name == "" || m.concurrency == nil {
		return ConcurrencyGroup{}, false
	}
	st, ok := m.concurrency[name]
	if !ok {
		return ConcurrencyGroup{}, false
	}
	return st.cfg, true
}

// RemoveConcurrencyGroup unregisters the named concurrency group from m.
// Active voices previously in the group recover from any group ducking immediately.
func (m *Mixer) RemoveConcurrencyGroup(name string) {
	if m == nil || name == "" || m.concurrency == nil {
		return
	}
	if _, ok := m.concurrency[name]; !ok {
		return
	}
	delete(m.concurrency, name)
	m.syncGroupDucking()
	m.apply()
}

// SetBusDucking replaces m's bus ducking rules. Rules with an empty Bus or By, Bus == By,
// or Amount <= 0 are ignored. Existing bus duck animations are reset and recomputed immediately
// against currently playing sounds and music.
func (m *Mixer) SetBusDucking(rules ...BusDuckRule) {
	if m == nil {
		return
	}
	for _, st := range m.busDucks {
		if st.stopAnim != nil {
			st.stopAnim()
			st.stopAnim = nil
		}
	}
	m.busDucks = m.busDucks[:0]
	for _, raw := range rules {
		r, ok := sanitizeBusDuckRule(raw)
		if !ok {
			continue
		}
		m.busDucks = append(m.busDucks, &busDuckState{
			rule:   r,
			level:  1,
			target: 1,
		})
	}
	m.ensureWatchers()
	m.syncBusDucking()
	m.apply()
}

// AddBusDucking appends or replaces the bus ducking rule matching rule.Bus and rule.By.
// Rules with an empty Bus or By, Bus == By, or Amount <= 0 are ignored.
func (m *Mixer) AddBusDucking(rule BusDuckRule) {
	if m == nil {
		return
	}
	r, ok := sanitizeBusDuckRule(rule)
	if !ok {
		return
	}
	for _, st := range m.busDucks {
		if st.rule.Bus == r.Bus && st.rule.By == r.By {
			st.rule = r
			m.ensureWatchers()
			m.syncBusDucking()
			m.apply()
			return
		}
	}
	m.busDucks = append(m.busDucks, &busDuckState{
		rule:   r,
		level:  1,
		target: 1,
	})
	m.ensureWatchers()
	m.syncBusDucking()
	m.apply()
}

// BusDucking returns a copy of m's configured bus ducking rules.
func (m *Mixer) BusDucking() []BusDuckRule {
	if m == nil || len(m.busDucks) == 0 {
		return nil
	}
	out := make([]BusDuckRule, len(m.busDucks))
	for i, st := range m.busDucks {
		out[i] = st.rule
	}
	return out
}

// DebugSnapshot returns a deterministic snapshot of active voices, global voice budget,
// per-concurrency-group counts and counters, and per-bus voice counts and ducking gains.
func (m *Mixer) DebugSnapshot() MixerDebugSnapshot {
	if m == nil {
		return MixerDebugSnapshot{MaxVoices: DefaultMaxVoices}
	}
	m.pruneAll()
	snap := MixerDebugSnapshot{
		MaxVoices:    m.MaxVoices(),
		FadingVoices: len(m.fading),
	}

	groupCounts := make(map[string]int, len(m.concurrency))
	groupOwners := make(map[string]map[any]struct{}, len(m.concurrency))
	busCounts := map[string]int{}
	busNames := map[string]struct{}{}

	for s := range m.sounds {
		n := len(s.live)
		if n == 0 {
			continue
		}
		snap.ActiveVoices += n
		busCounts[s.Group] += n
		busNames[s.Group] = struct{}{}
		for _, p := range s.live {
			if p.concurrency != "" {
				groupCounts[p.concurrency]++
				if st, ok := m.concurrency[p.concurrency]; ok && st.cfg.Scope == ScopeOwner {
					if groupOwners[p.concurrency] == nil {
						groupOwners[p.concurrency] = map[any]struct{}{}
					}
					groupOwners[p.concurrency][p.owner] = struct{}{}
				}
			}
		}
	}
	for _, st := range m.busDucks {
		busNames[st.rule.Bus] = struct{}{}
		busNames[st.rule.By] = struct{}{}
	}
	for _, mu := range m.music {
		if mu.activeForDucking() {
			busNames[mu.Group] = struct{}{}
		}
	}

	if len(m.concurrency) > 0 || len(groupCounts) > 0 {
		names := make([]string, 0, len(m.concurrency)+len(groupCounts))
		seen := make(map[string]struct{}, len(m.concurrency)+len(groupCounts))
		for name := range m.concurrency {
			seen[name] = struct{}{}
			names = append(names, name)
		}
		for name := range groupCounts {
			if _, ok := seen[name]; !ok {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		snap.Groups = make([]ConcurrencyGroupDebug, 0, len(names))
		for _, name := range names {
			entry := ConcurrencyGroupDebug{
				Name:   name,
				Active: groupCounts[name],
			}
			if st, ok := m.concurrency[name]; ok {
				entry.Max = st.cfg.Max
				entry.Resolve = st.cfg.Resolve.String()
				entry.Scope = st.cfg.Scope.String()
				entry.Refused = st.refused
				entry.Stolen = st.stolen
				entry.Retriggered = st.retriggered
				if st.cfg.Scope == ScopeOwner {
					entry.ActiveOwners = len(groupOwners[name])
				}
			} else {
				entry.Resolve = ResolveRefuse.String()
				entry.Scope = ScopeGlobal.String()
			}
			snap.Groups = append(snap.Groups, entry)
		}
	}

	if len(busNames) > 0 {
		names := make([]string, 0, len(busNames))
		for name := range busNames {
			names = append(names, name)
		}
		slices.Sort(names)
		snap.Buses = make([]BusDebug, 0, len(names))
		for _, name := range names {
			snap.Buses = append(snap.Buses, BusDebug{
				Name:         name,
				ActiveVoices: busCounts[name],
				DuckGain:     m.busDuckGain(name),
			})
		}
	}

	return snap
}

func (m *Mixer) now() time.Time {
	if m != nil && m.cx != nil {
		return m.cx.Now()
	}
	return time.Now()
}

func (m *Mixer) busDuckGain(bus string) float32 {
	if m == nil || len(m.busDucks) == 0 {
		return 1
	}
	g := float32(1)
	for _, st := range m.busDucks {
		if st.rule.Bus == bus {
			g *= st.level
		}
	}
	return g
}

// voiceRef identifies a live active playback and its owning Effect.
type voiceRef struct {
	effect *Effect
	pb     *playback
}

// candidateMetrics captures the incoming voice's comparison attributes for concurrency resolution.
type candidateMetrics struct {
	volume   float32 // effective audible volume (baseVol * spatialGain * groupGain)
	distance float32 // distance from listener (0 for non-positional)
	priority int
}

func (v voiceRef) audibleVolume(m *Mixer) float32 {
	g := m.gain(v.effect.Group)
	vol := v.pb.volume * g
	if v.pb.spatial != nil {
		vol *= v.pb.spatial.spatial.Gain
	}
	return vol
}

func (v voiceRef) distance() float32 {
	if v.pb.spatial != nil {
		return v.pb.spatial.spatial.Distance
	}
	return v.pb.distance
}

// selectGroupVictims evaluates cfg against the active voices in the same concurrency scope.
// It returns whether the incoming sound is admitted and which active voices must be stolen to make room.
func (m *Mixer) selectGroupVictims(cfg ConcurrencyGroup, active []voiceRef, cand candidateMetrics) (admit bool, victims []voiceRef) {
	if cfg.Max <= 0 || len(active) < cfg.Max {
		return true, nil
	}
	need := len(active) - cfg.Max + 1
	if cfg.Resolve == ResolveRefuse {
		return false, nil
	}

	pool := make([]voiceRef, len(active))
	copy(pool, active)

	switch cfg.Resolve {
	case ResolveOldest:
		slices.SortFunc(pool, func(a, b voiceRef) int {
			return cmpUint64(a.pb.seq, b.pb.seq)
		})
		return true, pool[:need]

	case ResolveFarthest:
		// Sort worst-first: farthest distance first, ties by lower priority, quieter volume, oldest seq.
		slices.SortFunc(pool, func(a, b voiceRef) int {
			da, db := a.distance(), b.distance()
			if da != db {
				if da > db {
					return -1
				}
				return 1
			}
			if a.pb.priority != b.pb.priority {
				if a.pb.priority < b.pb.priority {
					return -1
				}
				return 1
			}
			va, vb := a.audibleVolume(m), b.audibleVolume(m)
			if va != vb {
				if va < vb {
					return -1
				}
				return 1
			}
			return cmpUint64(a.pb.seq, b.pb.seq)
		})
		// Incoming sound must be at least as close as the worst victim it would displace.
		// When need > 1 (Max was lowered), compare against the closest of the displaced victims (pool[need-1]).
		if cand.distance > pool[need-1].distance() {
			return false, nil
		}
		return true, pool[:need]

	case ResolveQuietest:
		// Sort worst-first: quietest audible volume first, ties by lower priority, farthest distance, oldest seq.
		slices.SortFunc(pool, func(a, b voiceRef) int {
			va, vb := a.audibleVolume(m), b.audibleVolume(m)
			if va != vb {
				if va < vb {
					return -1
				}
				return 1
			}
			if a.pb.priority != b.pb.priority {
				if a.pb.priority < b.pb.priority {
					return -1
				}
				return 1
			}
			da, db := a.distance(), b.distance()
			if da != db {
				if da > db {
					return -1
				}
				return 1
			}
			return cmpUint64(a.pb.seq, b.pb.seq)
		})
		if cand.volume < pool[need-1].audibleVolume(m) {
			return false, nil
		}
		return true, pool[:need]

	case ResolveLowestPriority:
		// Sort worst-first: lowest priority first, ties by oldest seq.
		slices.SortFunc(pool, func(a, b voiceRef) int {
			if a.pb.priority != b.pb.priority {
				if a.pb.priority < b.pb.priority {
					return -1
				}
				return 1
			}
			return cmpUint64(a.pb.seq, b.pb.seq)
		})
		if cand.priority < pool[need-1].pb.priority {
			return false, nil
		}
		return true, pool[:need]

	default:
		return false, nil
	}
}

// selectGlobalVictims checks the mixer's global voice budget across all remaining active voices.
func (m *Mixer) selectGlobalVictims(remaining []voiceRef, candPriority int) (admit bool, victims []voiceRef) {
	budget := m.MaxVoices()
	if len(remaining) < budget {
		return true, nil
	}
	need := len(remaining) - budget + 1
	pool := make([]voiceRef, len(remaining))
	copy(pool, remaining)
	slices.SortFunc(pool, func(a, b voiceRef) int {
		if a.pb.priority != b.pb.priority {
			if a.pb.priority < b.pb.priority {
				return -1
			}
			return 1
		}
		return cmpUint64(a.pb.seq, b.pb.seq)
	})
	if candPriority < pool[need-1].pb.priority {
		return false, nil
	}
	return true, pool[:need]
}

func cmpUint64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// collectGroupActive returns all live non-fading voices matching groupName (and owner when ScopeOwner).
func (m *Mixer) collectGroupActive(groupName string, scope ConcurrencyScope, owner any) []voiceRef {
	var out []voiceRef
	for s := range m.sounds {
		for _, p := range s.live {
			if p.concurrency != groupName {
				continue
			}
			if scope == ScopeOwner && p.owner != owner {
				continue
			}
			out = append(out, voiceRef{effect: s, pb: p})
		}
	}
	return out
}

// collectAllActiveExcluding returns all live non-fading voices across m not in excluded.
func (m *Mixer) collectAllActiveExcluding(excluded map[*playback]struct{}) []voiceRef {
	var out []voiceRef
	for s := range m.sounds {
		for _, p := range s.live {
			if _, skip := excluded[p]; skip {
				continue
			}
			out = append(out, voiceRef{effect: s, pb: p})
		}
	}
	return out
}

func (m *Mixer) totalLiveHardwareVoices() int {
	n := len(m.fading)
	for s := range m.sounds {
		n += len(s.live)
	}
	return n
}

func (m *Mixer) freeHardwareHeadroom(wantFree int) {
	for len(m.fading) > 0 && m.totalLiveHardwareVoices()+wantFree > platformMaxVoices {
		f := m.fading[0]
		m.fading = slices.Delete(m.fading, 0, 1)
		f.cleanup()
		f.playback.Stop()
	}
}

// stealVoice removes v from its Effect's live slice and either stops it immediately or fades it out over fade.
func (m *Mixer) stealVoice(v voiceRef, fade time.Duration) {
	s := v.effect
	pb := v.pb
	idx := slices.Index(s.live, pb)
	if idx < 0 {
		return
	}
	s.live = slices.Delete(s.live, idx, idx+1)
	if len(s.live) == 0 {
		delete(m.sounds, s)
	}
	if pb.stopDuck != nil {
		pb.stopDuck()
		pb.stopDuck = nil
	}
	if fade <= 0 || ended(m.cx, pb.playback) {
		if pb.stopWatch != nil {
			pb.stopWatch()
			pb.stopWatch = nil
		}
		pb.playback.Stop()
		return
	}

	// Move to fading slice; cancel ended watcher used for active ducking and attach fade animation.
	if pb.stopWatch != nil {
		pb.stopWatch()
		pb.stopWatch = nil
	}
	f := &fadingPlayback{
		effect:     s,
		playback:   pb.playback,
		volume:     pb.volume,
		pan:        pb.pan,
		spatial:    pb.spatial,
		duckLevel:  pb.duckLevel,
		stealLevel: 1,
	}
	if pb.spatial != nil {
		pb.spatial.fading = f
		pb.spatial.entry = nil
	}
	m.fading = append(m.fading, f)
	m.cx.Run(func() {
		f.stopAnim = m.cx.Animate(fade, core.Linear, func(progress float64) {
			f.stealLevel = float32(max(0, 1-progress))
			if progress >= 1 || ended(m.cx, f.playback) {
				f.stopAnim = nil
				f.cleanup()
				f.playback.Stop()
				if fi := slices.Index(m.fading, f); fi >= 0 {
					m.fading = slices.Delete(m.fading, fi, fi+1)
				}
				return
			}
			g := m.gain(f.effect.Group)
			f.playback.SetVolume(f.effectiveVolume(g))
		})
	})
}

// pruneAll prunes ended playbacks across all tracked effects and fading voices.
func (m *Mixer) pruneAll() {
	if m == nil {
		return
	}
	for s := range m.sounds {
		if s.prune() == 0 {
			delete(m.sounds, s)
		}
	}
	n := 0
	for _, f := range m.fading {
		if ended(m.cx, f.playback) {
			f.cleanup()
			continue
		}
		m.fading[n] = f
		n++
	}
	clear(m.fading[n:])
	m.fading = m.fading[:n]
}

// needsWatcher reports whether a playback in bus/concurrencyGroup needs an Ended() watcher
// to drive automatic ducking release when the voice finishes naturally.
func (m *Mixer) needsWatcher(bus, concurrencyGroup string) bool {
	if m == nil {
		return false
	}
	if concurrencyGroup != "" {
		if st, ok := m.concurrency[concurrencyGroup]; ok && st.cfg.Duck.Amount > 0 {
			return true
		}
	}
	for _, bd := range m.busDucks {
		if bd.rule.By == bus || (concurrencyGroup != "" && bd.rule.By == concurrencyGroup) {
			return true
		}
	}
	return false
}

func (m *Mixer) attachWatcher(pb *playback) {
	if m == nil || pb == nil || pb.stopWatch != nil || pb.playback == nil {
		return
	}
	stopped := false
	var stop func()
	m.cx.Run(func() {
		stop = m.cx.Effect(func() {
			if pb.playback.Ended() {
				if !stopped {
					stopped = true
					if stop != nil {
						stop()
					}
					pb.stopWatch = nil
					m.onPlaybackEnded()
				}
			}
		})
	})
	if stopped {
		if stop != nil {
			stop()
		}
		return
	}
	pb.stopWatch = func() {
		if stopped {
			return
		}
		stopped = true
		if stop != nil {
			stop()
		}
	}
}

func (m *Mixer) ensureWatchers() {
	if m == nil {
		return
	}
	for s := range m.sounds {
		for _, pb := range s.live {
			if pb.stopWatch == nil && m.needsWatcher(s.Group, pb.concurrency) {
				m.attachWatcher(pb)
			}
		}
	}
}

func (m *Mixer) onPlaybackEnded() {
	if m == nil {
		return
	}
	m.pruneAll()
	m.syncGroupDucking()
	m.syncBusDucking()
}

type duckScopeKey struct {
	group string
	owner any
}

// syncGroupDucking recomputes per-voice duck targets for every concurrency group and drives smooth attack/release ramps.
func (m *Mixer) syncGroupDucking() {
	if m == nil {
		return
	}
	buckets := map[duckScopeKey][]voiceRef{}
	for s := range m.sounds {
		for _, pb := range s.live {
			if pb.concurrency == "" {
				m.setVoiceDuckTarget(s, pb, 1, 0, 0)
				continue
			}
			st, ok := m.concurrency[pb.concurrency]
			if !ok || st.cfg.Duck.Amount <= 0 {
				m.setVoiceDuckTarget(s, pb, 1, 0, 0)
				continue
			}
			var ownerKey any
			if st.cfg.Scope == ScopeOwner {
				ownerKey = pb.owner
			}
			k := duckScopeKey{group: pb.concurrency, owner: ownerKey}
			buckets[k] = append(buckets[k], voiceRef{effect: s, pb: pb})
		}
	}
	for k, list := range buckets {
		st := m.concurrency[k.group]
		// Sort oldest to newest by seq so index i has (len(list)-1-i) newer voices.
		slices.SortFunc(list, func(a, b voiceRef) int {
			return cmpUint64(a.pb.seq, b.pb.seq)
		})
		factor := float64(1 - st.cfg.Duck.Amount)
		n := len(list)
		for i, v := range list {
			newer := n - 1 - i
			target := float32(1)
			if newer > 0 {
				target = float32(math.Pow(factor, float64(newer)))
			}
			m.setVoiceDuckTarget(v.effect, v.pb, target, st.cfg.Duck.Attack, st.cfg.Duck.Release)
		}
	}
}

func (m *Mixer) setVoiceDuckTarget(s *Effect, pb *playback, target float32, attack, release time.Duration) {
	if pb == nil {
		return
	}
	if target == pb.duckTarget {
		return
	}
	pb.duckTarget = target
	if pb.stopDuck != nil {
		pb.stopDuck()
		pb.stopDuck = nil
	}
	from := pb.duckLevel
	if from == target {
		return
	}
	var dur time.Duration
	if target < from {
		dur = attack
	} else {
		dur = release
	}
	if dur <= 0 {
		pb.duckLevel = target
		if !ended(m.cx, pb.playback) {
			pb.playback.SetVolume(pb.effectiveVolume(m.gain(s.Group)))
		}
		return
	}
	m.cx.Run(func() {
		pb.stopDuck = m.cx.Animate(dur, core.Linear, func(progress float64) {
			pb.duckLevel = from + (target-from)*float32(progress)
			if progress >= 1 {
				pb.duckLevel = target
				pb.stopDuck = nil
			}
			if ended(m.cx, pb.playback) {
				if pb.stopDuck != nil {
					pb.stopDuck()
					pb.stopDuck = nil
				}
				return
			}
			pb.playback.SetVolume(pb.effectiveVolume(m.gain(s.Group)))
		})
	})
}

func (m *Mixer) isDuckSourceActive(by string) bool {
	for s := range m.sounds {
		for _, pb := range s.live {
			if ended(m.cx, pb.playback) {
				continue
			}
			if s.Group == by || pb.concurrency == by {
				return true
			}
		}
	}
	for _, mu := range m.music {
		if mu.Group == by && mu.activeForDucking() {
			return true
		}
	}
	return false
}

// syncBusDucking updates every configured BusDuckRule envelope based on whether its By bus/group currently has active sounds.
func (m *Mixer) syncBusDucking() {
	if m == nil || len(m.busDucks) == 0 || m.syncingDuck {
		return
	}
	m.syncingDuck = true
	defer func() { m.syncingDuck = false }()

	var changedBuses []string
	for _, st := range m.busDucks {
		target := float32(1)
		if m.isDuckSourceActive(st.rule.By) {
			target = 1 - st.rule.Amount
		}
		if target == st.target {
			continue
		}
		st.target = target
		if st.stopAnim != nil {
			st.stopAnim()
			st.stopAnim = nil
		}
		from := st.level
		if from == target {
			continue
		}
		var dur time.Duration
		if target < from {
			dur = st.rule.Attack
		} else {
			dur = st.rule.Release
		}
		if dur <= 0 {
			st.level = target
			if !slices.Contains(changedBuses, st.rule.Bus) {
				changedBuses = append(changedBuses, st.rule.Bus)
			}
			continue
		}
		stRef := st
		m.cx.Run(func() {
			stRef.stopAnim = m.cx.Animate(dur, core.Linear, func(progress float64) {
				stRef.level = from + (target-from)*float32(progress)
				if progress >= 1 {
					stRef.level = target
					stRef.stopAnim = nil
				}
				m.applyBus(stRef.rule.Bus)
			})
		})
	}
	for _, bus := range changedBuses {
		m.applyBus(bus)
	}
}

func (m *Mixer) applyBus(bus string) {
	if m == nil {
		return
	}
	g := m.gain(bus)
	for s := range m.sounds {
		if s.Group != bus {
			continue
		}
		if s.prune() == 0 {
			delete(m.sounds, s)
			continue
		}
		for _, p := range s.live {
			p.playback.SetVolume(p.effectiveVolume(g))
		}
	}
	for _, f := range m.fading {
		if f.effect.Group == bus && !ended(m.cx, f.playback) {
			f.playback.SetVolume(f.effectiveVolume(g))
		}
	}
	for _, mu := range m.music {
		if mu.Group == bus {
			mu.apply()
		}
	}
}
