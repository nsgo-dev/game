package save

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// settingsKey is the dedicated key/filename stem used to persist player settings
	// outside the save-slot namespace.
	settingsKey = "settings"

	minCaptionScale = 0.5
	maxCaptionScale = 3.0
	minGameSpeed    = 0.25
	maxGameSpeed    = 2.0
)

// AudioSettings holds player audio preferences stored independently of save slots.
type AudioSettings struct {
	// Master is the overall volume in [0, 1] (1.0 by default).
	Master float64 `json:"master"`
	// Groups maps audio group names (such as "music", "sfx", "ui") to volumes in [0, 1].
	Groups map[string]float64 `json:"groups,omitempty"`
	// Muted silences all audio without discarding the stored Master level.
	Muted bool `json:"muted"`
}

// UnmarshalJSON decodes AudioSettings while preserving defaults for omitted fields
// and replacing Groups cleanly when "groups" is explicitly present in JSON.
func (a *AudioSettings) UnmarshalJSON(data []byte) error {
	type rawAudio struct {
		Master *float64        `json:"master"`
		Groups json.RawMessage `json:"groups"`
		Muted  *bool           `json:"muted"`
	}
	var raw rawAudio
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Master != nil {
		a.Master = *raw.Master
	}
	if raw.Muted != nil {
		a.Muted = *raw.Muted
	}
	if len(raw.Groups) > 0 {
		trimmed := bytes.TrimSpace(raw.Groups)
		if bytes.Equal(trimmed, []byte("null")) {
			a.Groups = nil
		} else {
			var g map[string]float64
			if err := json.Unmarshal(trimmed, &g); err != nil {
				return err
			}
			a.Groups = g
		}
	}
	return nil
}

// DisplaySettings holds player display and presentation preferences.
type DisplaySettings struct {
	// Fullscreen puts the window in full screen on the chosen display, as the platform provides it (on macOS, native full screen in its own Space; on the web, the browser's full screen, granted only during a user gesture); false keeps it windowed.
	Fullscreen bool `json:"fullscreen"`
	// DisplayID is the ID, from core.Env.Displays, of the display full screen uses. It names a display the same way across launches and display changes, so a UI that offers displays sets it; when it is empty or the display is not connected, DisplayIndex chooses instead.
	DisplayID string `json:"display_id,omitempty"`
	// DisplayIndex is the position in core.Env.Displays of the display full screen uses when DisplayID is empty or not connected; out of range keeps the window's current display.
	DisplayIndex int    `json:"display_index"`
	ScaleMode    string `json:"scale_mode,omitempty"`
	IntegerScale bool   `json:"integer_scale"`
	// FrameCap is the most frames drawn a second, uncapped when 0.
	FrameCap int `json:"frame_cap"`
	// VSync requests vertical sync. NSGo v0.3.4 does not yet expose swap-interval control, so screen.Video does not apply this field and gameui.SettingsMenu hides the toggle by default.
	VSync bool `json:"vsync"`
	// AutoDropLowPower lowers the frame cap while the system is in low-power mode or under serious thermal pressure, and restores it once the condition clears.
	AutoDropLowPower bool `json:"auto_drop_low_power"`
	// Quality is the player's quality setting as quality.NormalizeSetting writes it: "auto" (the default) lets quality.Selector choose from the device and its frame times, and "low", "medium" or "high" holds that preset.
	Quality string `json:"quality,omitempty"`
}

// AccessibilitySettings holds player accessibility preferences.
type AccessibilitySettings struct {
	Captions bool `json:"captions"`
	// CaptionScale is the subtitle/caption scale factor in [0.5, 3.0] (1.0 by default).
	CaptionScale   float64 `json:"caption_scale"`
	ColorBlindMode string  `json:"color_blind_mode,omitempty"`
	ReduceMotion   bool    `json:"reduce_motion"`
	HighContrast   bool    `json:"high_contrast"`
	// ScreenShake is the camera shake intensity in [0, 1] (1.0 by default, 0 disables shake).
	ScreenShake float64 `json:"screen_shake"`
	// GameSpeed is the gameplay speed multiplier in [0.25, 2.0] (1.0 by default).
	GameSpeed float64 `json:"game_speed"`
}

// Settings holds typed, versioned player preferences kept completely separate from save slots.
type Settings struct {
	Audio         AudioSettings              `json:"audio"`
	Bindings      map[string][]string        `json:"bindings,omitempty"`
	Display       DisplaySettings            `json:"display"`
	Locale        string                     `json:"locale,omitempty"`
	Accessibility AccessibilitySettings      `json:"accessibility"`
	Custom        map[string]json.RawMessage `json:"custom,omitempty"`
}

// DefaultSettings returns a Settings populated with sane defaults.
func DefaultSettings() Settings {
	return Settings{
		Audio: AudioSettings{
			Master: 1.0,
			Groups: map[string]float64{
				"music": 1.0,
				"sfx":   1.0,
				"ui":    1.0,
			},
			Muted: false,
		},
		Bindings: map[string][]string{},
		Display: DisplaySettings{
			Fullscreen:       false,
			DisplayIndex:     0,
			ScaleMode:        "fit",
			IntegerScale:     false,
			FrameCap:         0,
			VSync:            true,
			AutoDropLowPower: true,
			Quality:          qualityAuto,
		},
		Locale: "",
		Accessibility: AccessibilitySettings{
			Captions:       false,
			CaptionScale:   1.0,
			ColorBlindMode: "none",
			ReduceMotion:   false,
			HighContrast:   false,
			ScreenShake:    1.0,
			GameSpeed:      1.0,
		},
	}
}

// Validate clamps out-of-range numeric fields and normalizes empty/whitespace strings in place.
func (s *Settings) Validate() {
	s.Audio.Master = clampUnit(s.Audio.Master, 1.0)
	for group, vol := range s.Audio.Groups {
		s.Audio.Groups[group] = clampUnit(vol, 1.0)
	}

	if s.Display.DisplayIndex < 0 {
		s.Display.DisplayIndex = 0
	}
	s.Display.DisplayID = strings.TrimSpace(s.Display.DisplayID)
	if s.Display.FrameCap < 0 {
		s.Display.FrameCap = 0
	}
	s.Display.ScaleMode = strings.TrimSpace(s.Display.ScaleMode)
	if s.Display.ScaleMode == "" {
		s.Display.ScaleMode = "fit"
	}
	s.Display.Quality = normalizeQuality(s.Display.Quality)

	s.Locale = strings.TrimSpace(s.Locale)

	s.Accessibility.CaptionScale = clampPositiveRange(s.Accessibility.CaptionScale, minCaptionScale, maxCaptionScale, 1.0)
	s.Accessibility.ColorBlindMode = strings.TrimSpace(s.Accessibility.ColorBlindMode)
	if s.Accessibility.ColorBlindMode == "" {
		s.Accessibility.ColorBlindMode = "none"
	}
	s.Accessibility.ScreenShake = clampUnit(s.Accessibility.ScreenShake, 1.0)
	s.Accessibility.GameSpeed = clampPositiveRange(s.Accessibility.GameSpeed, minGameSpeed, maxGameSpeed, 1.0)
}

// qualityAuto is quality.SettingAuto. save is stable and quality is experimental, so save keeps its own copy of the setting's names rather than importing quality; quality's tests check that the two agree.
const qualityAuto = "auto"

// normalizeQuality returns the canonical form of a quality setting as quality.NormalizeSetting does, ignoring case and surrounding space, and "auto" for anything it does not name.
func normalizeQuality(q string) string {
	switch t := strings.ToLower(strings.TrimSpace(q)); t {
	case "low", "medium", "high":
		return t
	default:
		return qualityAuto
	}
}

func clampUnit(v, defaultVal float64) float64 {
	if math.IsNaN(v) {
		return defaultVal
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampPositiveRange(v, minVal, maxVal, defaultVal float64) float64 {
	if math.IsNaN(v) || v == 0 {
		return defaultVal
	}
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

func cloneSettings(s Settings) Settings {
	out := s
	if s.Audio.Groups != nil {
		out.Audio.Groups = maps.Clone(s.Audio.Groups)
	}
	if s.Bindings != nil {
		out.Bindings = make(map[string][]string, len(s.Bindings))
		for action, inputs := range s.Bindings {
			out.Bindings[action] = slices.Clone(inputs)
		}
	}
	if s.Custom != nil {
		out.Custom = make(map[string]json.RawMessage, len(s.Custom))
		for k, v := range s.Custom {
			out.Custom[k] = slices.Clone(v)
		}
	}
	return out
}

// SettingsSchema describes the version and step-by-step migrations for a SettingsStore.
type SettingsSchema struct {
	// Version is the current settings schema version written by Save and expected by Load; 1 when 0 or negative.
	Version int
	// Migrations maps a version v to the Migration that upgrades settings JSON data from v to v+1.
	Migrations map[int]Migration
}

type settingsConfig struct {
	version    int
	migrations map[int]Migration
}

// SettingsOption configures a SettingsStore or DefaultSettingsStore.
type SettingsOption func(*settingsConfig)

// WithSettingsVersion sets the settings store's current schema version. Versions below 1 are treated as 1.
func WithSettingsVersion(v int) SettingsOption {
	return func(c *settingsConfig) {
		c.version = v
	}
}

// WithSettingsMigration registers fn to migrate settings JSON data from version from to from+1.
func WithSettingsMigration(from int, fn Migration) SettingsOption {
	return func(c *settingsConfig) {
		if c.migrations == nil {
			c.migrations = make(map[int]Migration)
		}
		c.migrations[from] = fn
	}
}

// WithSettingsMigrations registers each migration in m, keyed by its starting version (from to from+1).
func WithSettingsMigrations(m map[int]Migration) SettingsOption {
	return func(c *settingsConfig) {
		if len(m) == 0 {
			return
		}
		if c.migrations == nil {
			c.migrations = make(map[int]Migration, len(m))
		}
		for from, fn := range m {
			c.migrations[from] = fn
		}
	}
}

// WithSettingsSchema applies schema's Version and Migrations to a settings store.
func WithSettingsSchema(schema SettingsSchema) SettingsOption {
	return func(c *settingsConfig) {
		if schema.Version > 0 {
			c.version = schema.Version
		}
		if len(schema.Migrations) > 0 {
			if c.migrations == nil {
				c.migrations = make(map[int]Migration, len(schema.Migrations))
			}
			for from, fn := range schema.Migrations {
				c.migrations[from] = fn
			}
		}
	}
}

// settingsBackend isolates settings persistence from save-slot keys while reusing the same underlying storage or directory.
type settingsBackend interface {
	read() ([]byte, error)
	readBackup() ([]byte, error)
	write(data []byte) error
	delete() error
	exists() bool
}

func adaptSettingsBackend(b Backend) settingsBackend {
	if b == nil {
		return nil
	}
	// Settings are not save slots: see through wrappers so they stay under the wrapped backend's own settings key.
	for {
		w, ok := b.(interface{ Unwrap() Backend })
		if !ok {
			break
		}
		inner := w.Unwrap()
		if inner == nil {
			break
		}
		b = inner
	}
	if sb, ok := b.(*storageBackend); ok {
		return &storageSettingsBackend{store: sb.store, prefix: sb.prefix}
	}
	return &slotSettingsBackend{backend: b}
}

type storageSettingsBackend struct {
	store  core.Storage
	prefix string
}

func (b *storageSettingsBackend) primaryKey() string {
	if b.prefix == "" {
		return settingsKey
	}
	return b.prefix + "." + settingsKey
}

func (b *storageSettingsBackend) backupKey() string {
	return b.primaryKey() + ".bak"
}

func (b *storageSettingsBackend) copyName(_ string, backup bool) string {
	if backup {
		return b.backupKey()
	}
	return b.primaryKey()
}

func (b *storageSettingsBackend) read() ([]byte, error) {
	v, ok := b.store.GetItem(b.primaryKey())
	if !ok {
		return nil, ErrNotFound
	}
	return []byte(v), nil
}

func (b *storageSettingsBackend) readBackup() ([]byte, error) {
	v, ok := b.store.GetItem(b.backupKey())
	if !ok {
		return nil, ErrNotFound
	}
	return []byte(v), nil
}

func (b *storageSettingsBackend) write(data []byte) error {
	primary := b.primaryKey()
	backup := b.backupKey()
	if prev, ok := b.store.GetItem(primary); ok && isValidEnvelope([]byte(prev)) {
		if err := b.store.SetItem(backup, prev); err != nil {
			return fmt.Errorf("save: write backup for %q: %w", settingsKey, err)
		}
	}
	if err := b.store.SetItem(primary, string(data)); err != nil {
		return fmt.Errorf("save: write %q: %w", settingsKey, err)
	}
	return nil
}

func (b *storageSettingsBackend) delete() error {
	b.store.RemoveItem(b.primaryKey())
	b.store.RemoveItem(b.backupKey())
	return nil
}

func (b *storageSettingsBackend) exists() bool {
	if _, ok := b.store.GetItem(b.primaryKey()); ok {
		return true
	}
	_, ok := b.store.GetItem(b.backupKey())
	return ok
}

type slotSettingsBackend struct {
	backend Backend
}

func (b *slotSettingsBackend) read() ([]byte, error)       { return b.backend.Read(settingsKey) }
func (b *slotSettingsBackend) readBackup() ([]byte, error) { return b.backend.ReadBackup(settingsKey) }
func (b *slotSettingsBackend) write(data []byte) error     { return b.backend.Write(settingsKey, data) }
func (b *slotSettingsBackend) delete() error               { return b.backend.Delete(settingsKey) }
func (b *slotSettingsBackend) exists() bool                { return b.backend.Exists(settingsKey) }

func (b *slotSettingsBackend) copyName(_ string, backup bool) string {
	return copyName(b.backend, settingsKey, backup)
}

// SettingsStore persists a versioned settings value of type T in a dedicated settings file or storage key,
// completely separate from save slots.
type SettingsStore[T any] struct {
	// Version is the current settings schema version written by Save and expected by Load; 1 when 0 or negative.
	Version int
	// Migrations maps a version v to the Migration that upgrades settings JSON data from v to v+1.
	Migrations map[int]Migration

	backend  settingsBackend
	defaults func() T
}

// NewSettingsStore returns a SettingsStore for T backed by backend.
//
// defaults supplies the baseline value returned when no settings exist yet, used as the base when unmarshaling
// partial JSON, and written on Reset. When defaults is nil, DefaultSettings() is used if T is Settings,
// otherwise the zero value of T.
func NewSettingsStore[T any](backend Backend, defaults func() T, opts ...SettingsOption) *SettingsStore[T] {
	cfg := settingsConfig{version: 1}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &SettingsStore[T]{
		Version:    cfg.version,
		Migrations: cfg.migrations,
		backend:    adaptSettingsBackend(backend),
		defaults:   defaults,
	}
}

// RegisterMigration registers fn to upgrade settings JSON data from version from to from+1.
func (s *SettingsStore[T]) RegisterMigration(from int, fn Migration) {
	if s.Migrations == nil {
		s.Migrations = make(map[int]Migration)
	}
	s.Migrations[from] = fn
}

func (s *SettingsStore[T]) version() int {
	if s.Version <= 0 {
		return 1
	}
	return s.Version
}

func (s *SettingsStore[T]) defaultValue() T {
	if s.defaults != nil {
		return s.defaults()
	}
	var zero T
	if _, ok := any(zero).(Settings); ok {
		return any(DefaultSettings()).(T)
	}
	return zero
}

func validateValue[T any](v *T) {
	if val, ok := any(v).(interface{ Validate() }); ok {
		val.Validate()
	}
}

func cloneForSave[T any](v T) T {
	if st, ok := any(v).(Settings); ok {
		return any(cloneSettings(st)).(T)
	}
	return v
}

// Load reads settings, migrating older versions up to s.Version one step at a time and validating the result.
//
// If no settings have been saved yet, Load returns the store's default value and a nil error.
// If the primary copy is missing, unreadable or corrupted, Load recovers from the backup copy.
// Settings from a newer schema version return ErrFutureVersion without falling back to an older backup.
func (s *SettingsStore[T]) Load() (T, error) {
	var zero T
	if s.backend == nil {
		return zero, errors.New("save: settings store has no backend")
	}
	primary, primaryErr := s.backend.read()
	if primaryErr == nil {
		out := s.defaultValue()
		err := decodeEnvelope(copyName(s.backend, settingsKey, false), primary, s.version(), s.Migrations, &out, nil)
		if err == nil {
			validateValue(&out)
			return out, nil
		}
		if errors.Is(err, ErrFutureVersion) || errors.Is(err, ErrMissingMigration) {
			return zero, err
		}
		if backup, bakErr := s.backend.readBackup(); bakErr == nil {
			rec := s.defaultValue()
			recErr := decodeEnvelope(copyName(s.backend, settingsKey, true), backup, s.version(), s.Migrations, &rec, nil)
			if recErr == nil {
				validateValue(&rec)
				return rec, nil
			}
			if errors.Is(recErr, ErrFutureVersion) || errors.Is(recErr, ErrMissingMigration) {
				return zero, recErr
			}
		}
		return zero, err
	}

	backup, bakErr := s.backend.readBackup()
	if bakErr != nil {
		if errors.Is(primaryErr, ErrNotFound) && errors.Is(bakErr, ErrNotFound) {
			out := s.defaultValue()
			validateValue(&out)
			return out, nil
		}
		return zero, fmt.Errorf("save: load %q: %w", settingsKey, primaryErr)
	}
	rec := s.defaultValue()
	if err := decodeEnvelope(copyName(s.backend, settingsKey, true), backup, s.version(), s.Migrations, &rec, nil); err != nil {
		return zero, err
	}
	validateValue(&rec)
	return rec, nil
}

// Save validates value (if T implements Validate()), encodes it into a versioned JSON envelope,
// and writes it atomically while keeping the previous valid copy as a backup.
func (s *SettingsStore[T]) Save(value T) error {
	if s.backend == nil {
		return errors.New("save: settings store has no backend")
	}
	v := cloneForSave(value)
	validateValue(&v)
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("save: marshal %q: %w", settingsKey, err)
	}
	payload, err := json.Marshal(envelope{
		Version: s.version(),
		Data:    json.RawMessage(raw),
	})
	if err != nil {
		return fmt.Errorf("save: encode %q: %w", settingsKey, err)
	}
	return s.backend.write(payload)
}

// Reset restores and saves the store's default settings.
func (s *SettingsStore[T]) Reset() error {
	return s.Save(s.defaultValue())
}

// Delete removes the stored settings and its backup copy. Deleting absent settings returns nil.
func (s *SettingsStore[T]) Delete() error {
	if s.backend == nil {
		return errors.New("save: settings store has no backend")
	}
	return s.backend.delete()
}

// Exists reports whether a primary or backup settings payload exists.
func (s *SettingsStore[T]) Exists() bool {
	if s.backend == nil {
		return false
	}
	return s.backend.exists()
}

// DefaultSettingsStore is a convenience SettingsStore for the standard Settings struct with startup Apply support.
type DefaultSettingsStore struct {
	*SettingsStore[Settings]
}

// NewDefaultSettingsStore returns a DefaultSettingsStore backed by backend.
func NewDefaultSettingsStore(backend Backend, opts ...SettingsOption) *DefaultSettingsStore {
	return &DefaultSettingsStore{
		SettingsStore: NewSettingsStore(backend, DefaultSettings, opts...),
	}
}

// NewFileSettingsStore returns a DefaultSettingsStore backed by settings.json (and settings.json.bak) inside dir.
func NewFileSettingsStore(dir string, opts ...SettingsOption) *DefaultSettingsStore {
	return NewDefaultSettingsStore(FileBackend(dir), opts...)
}

// NewStorageSettingsStore returns a DefaultSettingsStore backed by store under <prefix>.settings (outside the slot namespace).
func NewStorageSettingsStore(store core.Storage, prefix string, opts ...SettingsOption) *DefaultSettingsStore {
	return NewDefaultSettingsStore(StorageBackend(store, prefix), opts...)
}

// LoadAndApply loads settings (returning DefaultSettings when absent) and applies them to targets.
// If Load fails, targets are not called. If Apply fails, the loaded settings are still returned alongside the error.
func (s *DefaultSettingsStore) LoadAndApply(targets SettingsTargets) (Settings, error) {
	loaded, err := s.Load()
	if err != nil {
		return Settings{}, err
	}
	if err := Apply(loaded, targets); err != nil {
		return loaded, err
	}
	return loaded, nil
}

// SettingsTargets wires subsystems to receive loaded player settings at startup or when settings change.
// Every field is optional; nil targets are skipped.
type SettingsTargets struct {
	// Sound receives master and group volumes from Settings.Audio.
	// When Settings.Audio.Muted is true, the mixer's master volume is set to 0.
	Sound *sound.Mixer

	// SetMasterVolume receives the effective master volume (0 when Muted is true) in [0, 1].
	SetMasterVolume func(volume float64)

	// SetGroupVolume receives each audio group name and volume from Settings.Audio.Groups in sorted key order.
	SetGroupVolume func(group string, volume float64)

	// ApplyAudio receives the validated AudioSettings.
	ApplyAudio func(AudioSettings) error

	// ApplyBindings receives a cloned map of action bindings.
	ApplyBindings func(map[string][]string) error

	// ApplyDisplay receives the validated DisplaySettings.
	ApplyDisplay func(DisplaySettings) error

	// ApplyLocale receives the validated Locale string.
	ApplyLocale func(locale string) error

	// ApplyAccessibility receives the validated AccessibilitySettings.
	ApplyAccessibility func(AccessibilitySettings) error
}

// Apply validates a copy of s and dispatches its sections to every non-nil target in targets.
// All non-nil targets are invoked even if an earlier target returns an error; errors are combined with errors.Join.
func Apply(s Settings, targets SettingsTargets) error {
	s = cloneSettings(s)
	s.Validate()

	effectiveMaster := s.Audio.Master
	if s.Audio.Muted {
		effectiveMaster = 0
	}

	groupNames := make([]string, 0, len(s.Audio.Groups))
	for name := range s.Audio.Groups {
		groupNames = append(groupNames, name)
	}
	slices.Sort(groupNames)

	var errs []error

	if targets.Sound != nil {
		if err := targets.Sound.SetMaster(float32(effectiveMaster)); err != nil {
			errs = append(errs, fmt.Errorf("save: apply master volume: %w", err))
		}
		for _, name := range groupNames {
			if err := targets.Sound.SetVolume(name, float32(s.Audio.Groups[name])); err != nil {
				errs = append(errs, fmt.Errorf("save: apply audio group %q: %w", name, err))
			}
		}
	}
	if targets.SetMasterVolume != nil {
		targets.SetMasterVolume(effectiveMaster)
	}
	if targets.SetGroupVolume != nil {
		for _, name := range groupNames {
			targets.SetGroupVolume(name, s.Audio.Groups[name])
		}
	}
	if targets.ApplyAudio != nil {
		if err := targets.ApplyAudio(s.Audio); err != nil {
			errs = append(errs, fmt.Errorf("save: apply audio: %w", err))
		}
	}
	if targets.ApplyBindings != nil {
		if err := targets.ApplyBindings(s.Bindings); err != nil {
			errs = append(errs, fmt.Errorf("save: apply bindings: %w", err))
		}
	}
	if targets.ApplyDisplay != nil {
		if err := targets.ApplyDisplay(s.Display); err != nil {
			errs = append(errs, fmt.Errorf("save: apply display: %w", err))
		}
	}
	if targets.ApplyLocale != nil {
		if err := targets.ApplyLocale(s.Locale); err != nil {
			errs = append(errs, fmt.Errorf("save: apply locale: %w", err))
		}
	}
	if targets.ApplyAccessibility != nil {
		if err := targets.ApplyAccessibility(s.Accessibility); err != nil {
			errs = append(errs, fmt.Errorf("save: apply accessibility: %w", err))
		}
	}

	return errors.Join(errs...)
}
