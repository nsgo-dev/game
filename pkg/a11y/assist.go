package a11y

import (
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// MinGameSpeed is the slowest allowed Assist.GameSpeed multiplier.
	MinGameSpeed = save.MinGameSpeed
	// MaxGameSpeed is the fastest allowed Assist.GameSpeed multiplier.
	MaxGameSpeed = save.MaxGameSpeed
	// MinTextScale is the minimum allowed Assist.TextScale multiplier.
	MinTextScale float32 = save.MinCaptionScale
	// MaxTextScale is the maximum allowed Assist.TextScale multiplier.
	MaxTextScale float32 = save.MaxCaptionScale

	defaultAssistKey         = "a11y.assist"
	defaultGameSpeed         = 1.0
	defaultTextScale float32 = 1.0
)

var legacyAssistSuffixes = []string{
	"gameSpeed",
	"reduceMotion",
	"invincibility",
	"autoAim",
	"holdToToggle",
	"highContrast",
	"captionsEnabled",
	"sfxCaptionsEnabled",
	"colorVision",
	"textScale",
}

// AssistOptions is a plain snapshot of every assist option managed by Assist.
type AssistOptions struct {
	GameSpeed          float64
	ReduceMotion       bool
	ReduceFlashes      bool
	Invincibility      bool
	AutoAim            bool
	HoldToToggle       bool
	HighContrast       bool
	CaptionsEnabled    bool
	SFXCaptionsEnabled bool
	ColorVision        ColorVision
	TextScale          float32
}

// Assist stores and persists a player's accessibility and gameplay-assist
// preferences in a save.DefaultSettingsStore (specifically its Accessibility
// section). Every getter subscribes reactive NSGo computations to store saves,
// live ApplySettings updates, and core.Env changes where appropriate.
type Assist struct {
	cx         *core.Scope
	store      *save.DefaultSettingsStore
	rev        *core.Signal[uint64]
	storage    core.Storage
	hasStorage bool
	storageKey string

	mu      sync.RWMutex
	version uint64
	loaded  bool
	cached  save.AccessibilitySettings
	lastRaw string
	hasLive bool
	live    save.AccessibilitySettings
}

// NewAssistFromSettings returns an Assist controller backed by store so
// CaptionsView, AccessibleMenu, effects, and gameui.SettingsMenu share a single
// set of persisted preferences. Any legacy "a11y.assist.*" keys present in
// cx.Storage() are migrated once into store and removed.
//
// When store is nil and cx is non-nil, a storage-backed DefaultSettingsStore
// under "a11y.assist.settings" is created automatically.
func NewAssistFromSettings(cx *core.Scope, store *save.DefaultSettingsStore) *Assist {
	var storage core.Storage
	var hasStorage bool
	var storageKey string
	if cx != nil {
		storage = cx.Storage()
		hasStorage = true
	}
	if store == nil && hasStorage {
		store = save.NewStorageSettingsStore(storage, defaultAssistKey)
		storageKey = defaultAssistKey + ".settings"
	}
	if hasStorage && store != nil {
		_, _ = MigrateLegacyAssist(storage, defaultAssistKey, store)
	}
	return newAssist(cx, store, storage, hasStorage, storageKey)
}

// NewAssist returns an Assist controller backed by a save.DefaultSettingsStore
// in cx's core.Storage under "<key>.settings" (defaulting key to "a11y.assist").
// Any legacy raw "<key>.*" storage keys written by earlier versions are migrated
// once into the settings store and removed.
//
// Deprecated: Use NewAssistFromSettings with the game's shared save.DefaultSettingsStore
// so SettingsMenu, CaptionsView, AccessibleMenu, and effects read the same settings.
func NewAssist(cx *core.Scope, key string) *Assist {
	if key == "" {
		key = defaultAssistKey
	}
	var storage core.Storage
	var hasStorage bool
	var store *save.DefaultSettingsStore
	if cx != nil {
		storage = cx.Storage()
		hasStorage = true
		store = save.NewStorageSettingsStore(storage, key)
		_, _ = MigrateLegacyAssist(storage, key, store)
	}
	return newAssist(cx, store, storage, hasStorage, key+".settings")
}

func newAssist(cx *core.Scope, store *save.DefaultSettingsStore, storage core.Storage, hasStorage bool, storageKey string) *Assist {
	a := &Assist{
		cx:         cx,
		store:      store,
		storage:    storage,
		hasStorage: hasStorage,
		storageKey: storageKey,
	}
	if cx != nil {
		a.rev = core.NewSignal(cx, uint64(0))
	}
	if store != nil {
		unsub := store.Subscribe(func() {
			a.mu.Lock()
			a.hasLive = false
			a.loaded = false
			a.lastRaw = ""
			a.mu.Unlock()
			a.bump()
		})
		if cx != nil {
			cx.Cleanup(unsub)
		}
	}
	return a
}

// MigrateLegacyAssist performs a one-time migration of legacy raw core.Storage
// keys ("<legacyKey>.gameSpeed", "<legacyKey>.reduceMotion", etc.) into store's
// AccessibilitySettings, removing the old keys after store.Save succeeds.
// legacyKey defaults to "a11y.assist" when empty. It reports whether any legacy
// keys were found and migrated.
func MigrateLegacyAssist(storage core.Storage, legacyKey string, store *save.DefaultSettingsStore) (bool, error) {
	if store == nil {
		return false, nil
	}
	if legacyKey == "" {
		legacyKey = defaultAssistKey
	}

	rawVals := make(map[string]string, len(legacyAssistSuffixes))
	for _, suffix := range legacyAssistSuffixes {
		if v, ok := storage.GetItem(legacyKey + "." + suffix); ok {
			rawVals[suffix] = v
		}
	}
	if len(rawVals) == 0 {
		return false, nil
	}

	s, err := store.Load()
	if err != nil {
		s = save.DefaultSettings()
	}

	if raw, ok := rawVals["gameSpeed"]; ok {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			s.Accessibility.GameSpeed = clampFloat64(v, MinGameSpeed, MaxGameSpeed)
		}
	}
	if raw, ok := rawVals["reduceMotion"]; ok {
		if v, err := strconv.ParseBool(raw); err == nil {
			s.Accessibility.SetReduceMotion(v)
		}
	}
	if raw, ok := rawVals["invincibility"]; ok {
		if v, err := strconv.ParseBool(raw); err == nil {
			s.Accessibility.Invincibility = v
		}
	}
	if raw, ok := rawVals["autoAim"]; ok {
		if v, err := strconv.ParseBool(raw); err == nil {
			s.Accessibility.AutoAim = v
		}
	}
	if raw, ok := rawVals["holdToToggle"]; ok {
		if v, err := strconv.ParseBool(raw); err == nil {
			s.Accessibility.HoldToToggle = v
		}
	}
	if raw, ok := rawVals["highContrast"]; ok {
		if v, err := strconv.ParseBool(raw); err == nil {
			s.Accessibility.SetHighContrast(v)
		}
	}
	if raw, ok := rawVals["captionsEnabled"]; ok {
		if v, err := strconv.ParseBool(raw); err == nil {
			s.Accessibility.Captions = v
		}
	}
	if raw, ok := rawVals["sfxCaptionsEnabled"]; ok {
		if v, err := strconv.ParseBool(raw); err == nil {
			s.Accessibility.SFXCaptions = v
		}
	}
	if raw, ok := rawVals["colorVision"]; ok {
		if mode, ok := ParseColorVision(raw); ok {
			s.Accessibility.ColorBlindMode = save.NormalizeColorBlindMode(mode.String())
		}
	}
	if raw, ok := rawVals["textScale"]; ok {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			s.Accessibility.SetCaptionScale(v)
		}
	}

	if err := store.Save(s); err != nil {
		return false, err
	}
	for suffix := range rawVals {
		storage.RemoveItem(legacyKey + "." + suffix)
	}
	return true, nil
}

func (a *Assist) env() *core.Env {
	if a.cx == nil {
		return nil
	}
	return a.cx.Env()
}

func (a *Assist) bump() {
	a.mu.Lock()
	a.version++
	v := a.version
	a.mu.Unlock()
	if a.rev != nil {
		a.rev.Set(v)
	}
}

func (a *Assist) load() save.AccessibilitySettings {
	if a.rev != nil {
		_ = a.rev.Get()
	}

	// Track direct core.Storage updates when storageKey is known (so multiple
	// Assist instances on the same storage stay reactively synchronized).
	var curRaw string
	watchStorage := a.hasStorage && a.storageKey != ""
	if watchStorage {
		curRaw, _ = a.storage.GetItem(a.storageKey)
	}

	a.mu.RLock()
	if a.hasLive {
		v := a.live
		a.mu.RUnlock()
		return v
	}
	if a.loaded && (!watchStorage || curRaw == a.lastRaw) {
		v := a.cached
		a.mu.RUnlock()
		return v
	}
	a.mu.RUnlock()

	acc := save.DefaultSettings().Accessibility
	if a.store != nil {
		if s, err := a.store.Load(); err == nil {
			acc = s.Accessibility
		}
	}

	a.mu.Lock()
	if !a.hasLive {
		a.cached = acc
		a.loaded = true
		if watchStorage {
			a.lastRaw = curRaw
		}
	}
	a.mu.Unlock()
	return acc
}

func (a *Assist) update(fn func(*save.AccessibilitySettings)) error {
	var s save.Settings
	if a.store != nil {
		loaded, err := a.store.Load()
		if err != nil {
			loaded = save.DefaultSettings()
		}
		s = loaded
	} else {
		s = save.DefaultSettings()
	}

	a.mu.RLock()
	if a.hasLive {
		s.Accessibility = a.live
	}
	a.mu.RUnlock()

	fn(&s.Accessibility)
	s.Validate()

	if a.store == nil {
		a.mu.Lock()
		a.live = s.Accessibility
		a.hasLive = true
		a.mu.Unlock()
		a.bump()
		return nil
	}

	a.mu.Lock()
	a.hasLive = false
	a.mu.Unlock()
	return a.store.Save(s)
}

// Settings returns the current AccessibilitySettings snapshot.
func (a *Assist) Settings() save.AccessibilitySettings {
	return a.load()
}

// ApplySettings applies a live AccessibilitySettings snapshot immediately without
// writing to disk, notifying reactive views such as CaptionsView and AccessibleMenu.
// It matches save.SettingsTargets.ApplyAccessibility so SettingsMenu.Apply previews
// changes before Save.
func (a *Assist) ApplySettings(s save.AccessibilitySettings) error {
	s.Validate()
	a.mu.Lock()
	a.live = s
	a.hasLive = true
	a.mu.Unlock()
	a.bump()
	return nil
}

// Reload discards any cached or previewed state and re-reads from the backing store,
// notifying reactive subscribers.
func (a *Assist) Reload() {
	a.mu.Lock()
	a.hasLive = false
	a.loaded = false
	a.lastRaw = ""
	a.mu.Unlock()
	a.bump()
}

// GameSpeed returns the gameplay speed multiplier in [MinGameSpeed, MaxGameSpeed],
// defaulting to 1.0.
func (a *Assist) GameSpeed() float64 {
	return clampFloat64(a.load().GameSpeed, MinGameSpeed, MaxGameSpeed)
}

// SetGameSpeed clamps v to [MinGameSpeed, MaxGameSpeed] and persists it.
func (a *Assist) SetGameSpeed(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		v = defaultGameSpeed
	}
	v = clampFloat64(v, MinGameSpeed, MaxGameSpeed)
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.GameSpeed = v
	})
}

// ScaleDT scales a simulation step dt by GameSpeed. Non-positive durations
// return 0, and a speed of 1.0 returns dt unchanged.
func (a *Assist) ScaleDT(dt time.Duration) time.Duration {
	if dt <= 0 {
		return 0
	}
	speed := a.GameSpeed()
	if speed == 1.0 {
		return dt
	}
	return time.Duration(math.Round(float64(dt) * speed))
}

// ReduceMotion reports whether screen shake, flashes, and non-essential motion
// should be disabled. When unset in settings, it follows cx.Env().ReduceMotion().
func (a *Assist) ReduceMotion() bool {
	return a.load().EffectiveReduceMotion(a.env())
}

// SetReduceMotion persists an explicit reduced-motion preference.
func (a *Assist) SetReduceMotion(v bool) error {
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.SetReduceMotion(v)
	})
}

// ReduceFlashes reports whether full-screen flash reduction is enabled.
func (a *Assist) ReduceFlashes() bool {
	return a.load().ReduceFlashes
}

// SetReduceFlashes persists the flash-reduction preference.
func (a *Assist) SetReduceFlashes(v bool) error {
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.ReduceFlashes = v
	})
}

// ScreenShake returns the screen shake intensity multiplier in [0, 1].
func (a *Assist) ScreenShake() float64 {
	return clampFloat64(a.load().ScreenShake, 0, 1)
}

// SetScreenShake clamps v to [0, 1] and persists it.
func (a *Assist) SetScreenShake(v float64) error {
	if math.IsNaN(v) {
		v = 1.0
	}
	v = clampFloat64(v, 0, 1)
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.ScreenShake = v
	})
}

// Invincibility reports whether player damage is disabled.
func (a *Assist) Invincibility() bool { return a.load().Invincibility }

// SetInvincibility persists the invincibility assist option.
func (a *Assist) SetInvincibility(v bool) error {
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.Invincibility = v
	})
}

// AutoAim reports whether aim assistance is enabled.
func (a *Assist) AutoAim() bool { return a.load().AutoAim }

// SetAutoAim persists the auto-aim assist option.
func (a *Assist) SetAutoAim(v bool) error {
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.AutoAim = v
	})
}

// HoldToToggle reports whether held inputs should act as toggles.
func (a *Assist) HoldToToggle() bool { return a.load().HoldToToggle }

// SetHoldToToggle persists the hold-to-toggle assist option.
func (a *Assist) SetHoldToToggle(v bool) error {
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.HoldToToggle = v
	})
}

// HighContrast reports whether high-contrast visuals are requested. When unset
// in settings, it follows cx.Env().IncreaseContrast().
func (a *Assist) HighContrast() bool {
	return a.load().EffectiveHighContrast(a.env())
}

// SetHighContrast persists an explicit high-contrast preference.
func (a *Assist) SetHighContrast(v bool) error {
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.SetHighContrast(v)
	})
}

// CaptionsEnabled reports whether dialogue and music captions are enabled.
func (a *Assist) CaptionsEnabled() bool { return a.load().Captions }

// SetCaptionsEnabled persists whether captions are enabled.
func (a *Assist) SetCaptionsEnabled(v bool) error {
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.Captions = v
	})
}

// SFXCaptionsEnabled reports whether sound-effect captions are enabled.
func (a *Assist) SFXCaptionsEnabled() bool { return a.load().SFXCaptions }

// SetSFXCaptionsEnabled persists whether sound-effect captions are enabled.
func (a *Assist) SetSFXCaptionsEnabled(v bool) error {
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.SFXCaptions = v
	})
}

// ColorVision returns the selected color-vision mode, defaulting to VisionNormal.
func (a *Assist) ColorVision() ColorVision {
	if mode, ok := ParseColorVision(a.load().ColorBlindMode); ok {
		return mode
	}
	return VisionNormal
}

// SetColorVision persists the selected color-vision mode.
func (a *Assist) SetColorVision(v ColorVision) error {
	mode := save.NormalizeColorBlindMode(v.String())
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.ColorBlindMode = mode
	})
}

// TextScale returns the UI/caption text scale multiplier in [MinTextScale, MaxTextScale].
// When unset in settings, it adopts cx.Env().FontScale() clamped to [MinTextScale, MaxTextScale].
func (a *Assist) TextScale() float32 {
	return float32(a.load().EffectiveCaptionScale(a.env()))
}

// SetTextScale clamps v to [MinTextScale, MaxTextScale] and persists it.
func (a *Assist) SetTextScale(v float32) error {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		v = defaultTextScale
	}
	v = clampFloat32(v, MinTextScale, MaxTextScale)
	return a.update(func(acc *save.AccessibilitySettings) {
		acc.SetCaptionScale(float64(v))
	})
}

// Palette returns the active Palette for the player's current settings,
// preferring VisionHighContrast when HighContrast is enabled.
func (a *Assist) Palette() Palette {
	if a.HighContrast() {
		return PaletteFor(VisionHighContrast)
	}
	return PaletteFor(a.ColorVision())
}

// Snapshot returns all current assist option values.
func (a *Assist) Snapshot() AssistOptions {
	acc := a.load()
	env := a.env()
	mode, ok := ParseColorVision(acc.ColorBlindMode)
	if !ok {
		mode = VisionNormal
	}
	return AssistOptions{
		GameSpeed:          clampFloat64(acc.GameSpeed, MinGameSpeed, MaxGameSpeed),
		ReduceMotion:       acc.EffectiveReduceMotion(env),
		ReduceFlashes:      acc.ReduceFlashes,
		Invincibility:      acc.Invincibility,
		AutoAim:            acc.AutoAim,
		HoldToToggle:       acc.HoldToToggle,
		HighContrast:       acc.EffectiveHighContrast(env),
		CaptionsEnabled:    acc.Captions,
		SFXCaptionsEnabled: acc.SFXCaptions,
		ColorVision:        mode,
		TextScale:          float32(acc.EffectiveCaptionScale(env)),
	}
}

// Apply persists every option in opts.
func (a *Assist) Apply(opts AssistOptions) error {
	return a.update(func(acc *save.AccessibilitySettings) {
		gs := opts.GameSpeed
		if math.IsNaN(gs) || math.IsInf(gs, 0) {
			gs = defaultGameSpeed
		}
		acc.GameSpeed = clampFloat64(gs, MinGameSpeed, MaxGameSpeed)
		acc.SetReduceMotion(opts.ReduceMotion)
		acc.ReduceFlashes = opts.ReduceFlashes
		acc.Invincibility = opts.Invincibility
		acc.AutoAim = opts.AutoAim
		acc.HoldToToggle = opts.HoldToToggle
		acc.SetHighContrast(opts.HighContrast)
		acc.Captions = opts.CaptionsEnabled
		acc.SFXCaptions = opts.SFXCaptionsEnabled
		acc.ColorBlindMode = save.NormalizeColorBlindMode(opts.ColorVision.String())
		ts := float64(opts.TextScale)
		if math.IsNaN(ts) || math.IsInf(ts, 0) {
			ts = float64(defaultTextScale)
		}
		acc.SetCaptionScale(clampFloat64(ts, float64(MinTextScale), float64(MaxTextScale)))
	})
}

// Reset restores all accessibility preferences to defaults so OS environment
// settings take effect again, leaving other sections of the backing settings
// store (Audio, Display, Bindings, Locale, Custom) intact.
func (a *Assist) Reset() {
	a.mu.Lock()
	a.hasLive = false
	a.loaded = false
	a.lastRaw = ""
	a.mu.Unlock()

	if a.store != nil && a.store.Exists() {
		s, err := a.store.Load()
		if err != nil {
			s = save.DefaultSettings()
		}
		s.Accessibility = save.DefaultSettings().Accessibility
		_ = a.store.Save(s)
		return
	}
	a.bump()
}

func clampFloat64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
