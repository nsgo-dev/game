package gameui

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/quality"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
)

const (
	// SettingsCustomResolution is the key inside save.Settings.Custom storing the selected resolution ID.
	SettingsCustomResolution = "resolution"
	// SettingsCustomReduceFlashes is the legacy key inside save.Settings.Custom that stored the flash-reduction toggle before save.AccessibilitySettings.ReduceFlashes existed.
	//
	// Deprecated: Use save.AccessibilitySettings.ReduceFlashes directly.
	SettingsCustomReduceFlashes = "reduce_flashes"
)

// SettingsTab identifies one of the five built-in sections of a SettingsMenu.
type SettingsTab uint8

const (
	// SettingsTabAudio edits mute, master volume, audio bus volumes, mono audio, and left/right balance.
	SettingsTabAudio SettingsTab = iota
	// SettingsTabVideo edits window mode, resolution (on desktop), scale mode, integer scale, quality preset, frame cap, and (when enabled via ShowVSync) VSync.
	SettingsTabVideo
	// SettingsTabControls edits keyboard and gamepad action bindings with capture and reset.
	SettingsTabControls
	// SettingsTabLanguage selects the active locale.
	SettingsTabLanguage
	// SettingsTabAccessibility edits captions, SFX captions, text scale, high contrast, reduced motion, screen shake, flash reduction, color-blind mode, game speed, invincibility, auto-aim, and hold-to-toggle.
	SettingsTabAccessibility
)

const settingsTabCount = int(SettingsTabAccessibility) + 1

// String returns the canonical lowercase name of tab.
func (t SettingsTab) String() string {
	switch t {
	case SettingsTabAudio:
		return "audio"
	case SettingsTabVideo:
		return "video"
	case SettingsTabControls:
		return "controls"
	case SettingsTabLanguage:
		return "language"
	case SettingsTabAccessibility:
		return "accessibility"
	default:
		return fmt.Sprintf("SettingsTab(%d)", uint8(t))
	}
}

// SettingsAudioBus describes an audio group slider shown in the Audio tab.
type SettingsAudioBus struct {
	// Name is the key inside save.AudioSettings.Groups (for example "music", "sfx", "ui").
	Name string
	// Label is shown next to the slider; defaults to Name when empty.
	Label string
}

// SettingsResolution describes a selectable resolution entry in the Video tab.
type SettingsResolution struct {
	// ID is persisted in save.Settings.Custom["resolution"] (for example "native" or "1280x720").
	ID string
	// Label is displayed in the Video tab; defaults to ID when empty.
	Label string
	// Width and Height optionally carry the target dimensions in points/pixels (0 for native/auto).
	Width  int
	Height int
}

// SettingsOptionItem pairs a persisted setting value with a user-visible display label.
type SettingsOptionItem struct {
	Value string
	Label string
}

// SettingsConflictPolicy controls how CaptureBinding resolves a binding already used by another action in the same group.
type SettingsConflictPolicy uint8

const (
	// ConflictSwap gives the other action the binding replaced on the capturing action. If there is no replaced binding, it clears the conflict.
	ConflictSwap SettingsConflictPolicy = iota
	// ConflictClear removes the binding from the other action.
	ConflictClear
	// ConflictRefuse leaves every binding unchanged and keeps capture active.
	ConflictRefuse
)

// SettingsAction describes a rebindable input action shown in the Controls tab.
type SettingsAction struct {
	// Name is the action key stored in save.Settings.Bindings (for example "jump", "left", "pause").
	Name string
	// Label is displayed in the Controls tab; defaults to Name when empty.
	Label string
	// Group identifies actions that may not share a binding. Actions with different groups may share one; the empty group is the default.
	Group string
	// Defaults lists default binding tokens (for example "key:space", "gamepad:south") restored on Reset.
	Defaults []string
	// AllowToggle reports whether the Controls tab offers a per-action Hold to Toggle assist switch for this action.
	AllowToggle bool
	// AllowMash reports whether the Controls tab offers a per-action Hold to Mash assist switch for this action.
	AllowMash bool
}

// SettingsLocale describes a selectable language entry in the Language tab.
type SettingsLocale struct {
	// Tag is the BCP-47 locale tag stored in save.Settings.Locale ("" selects the system default).
	Tag string
	// Label is displayed in the Language tab; defaults to Tag or SystemDefault.
	Label string
}

// SettingsText holds every user-visible string displayed by SettingsMenu, so a game can localize it.
// Partial structs passed via SettingsMenuConfig.Text inherit unset fields from DefaultSettingsText.
type SettingsText struct {
	Title string

	TabAudio         string
	TabVideo         string
	TabControls      string
	TabLanguage      string
	TabAccessibility string

	MuteAll       string
	MasterVolume  string
	MonoAudio     string
	MonoOn        string
	MonoOff       string
	Balance       string
	BalanceLeft   string
	BalanceCenter string
	BalanceRight  string

	WindowMode    string
	Windowed      string
	Fullscreen    string
	Resolution    string
	ScaleMode     string
	IntegerScale  string
	QualityPreset string
	VSync         string
	FrameCap      string
	Uncapped      string

	ControlsHint   string
	CapturePrompt  string
	ConflictStatus string
	Unbound        string
	ResetControls  string
	ActionToggle   string
	ActionMash     string
	MinPressTime   string
	AcceptDelay    string
	AssistOff      string

	LanguageHint  string
	SystemDefault string

	Subtitles     string
	SFXCaptions   string
	TextScale     string
	HighContrast  string
	ReduceMotion  string
	ScreenShake   string
	ReduceFlashes string
	ColorBlind    string
	GameSpeed     string
	Invincibility string
	AutoAim       string
	HoldToToggle  string

	Apply string
	Save  string
	Reset string
	Back  string

	SavedStatus   string
	AppliedStatus string
	ResetStatus   string
}

// DefaultSettingsText returns the English strings used by SettingsMenu.
func DefaultSettingsText() SettingsText {
	return SettingsText{
		Title: "Settings",

		TabAudio:         "Audio",
		TabVideo:         "Video",
		TabControls:      "Controls",
		TabLanguage:      "Language",
		TabAccessibility: "Accessibility",

		MuteAll:       "Mute All Audio",
		MasterVolume:  "Master",
		MonoAudio:     "Mono Audio",
		MonoOn:        "On",
		MonoOff:       "Off",
		Balance:       "Balance",
		BalanceLeft:   "L",
		BalanceCenter: "Center",
		BalanceRight:  "R",

		WindowMode:    "Window Mode",
		Windowed:      "Windowed",
		Fullscreen:    "Fullscreen",
		Resolution:    "Resolution",
		ScaleMode:     "Scale Mode",
		IntegerScale:  "Integer Scale",
		QualityPreset: "Quality",
		VSync:         "VSync",
		FrameCap:      "Frame Cap",
		Uncapped:      "Uncapped",

		ControlsHint:   "Select an action, then press a key or gamepad button to rebind.",
		CapturePrompt:  "Press key or button...",
		ConflictStatus: "Binding conflict with %s",
		Unbound:        "Unbound",
		ResetControls:  "Reset Controls to Defaults",
		ActionToggle:   "Hold to Toggle",
		ActionMash:     "Hold to Mash",
		MinPressTime:   "Min Press Time",
		AcceptDelay:    "Accept Delay",
		AssistOff:      "Off",

		LanguageHint:  "Choose display language",
		SystemDefault: "System Default",

		Subtitles:     "Subtitles / Captions",
		SFXCaptions:   "SFX Captions",
		TextScale:     "Text Scale",
		HighContrast:  "High Contrast",
		ReduceMotion:  "Reduced Motion",
		ScreenShake:   "Screen Shake",
		ReduceFlashes: "Reduce Flashes",
		ColorBlind:    "Color-Blind Mode",
		GameSpeed:     "Game Speed",
		Invincibility: "Invincibility",
		AutoAim:       "Auto-Aim",
		HoldToToggle:  "Hold to Toggle",

		Apply: "Apply",
		Save:  "Save",
		Reset: "Reset",
		Back:  "Back",

		SavedStatus:   "Settings saved",
		AppliedStatus: "Settings applied",
		ResetStatus:   "Defaults restored",
	}
}

func resolveSettingsText(in SettingsText) SettingsText {
	def := DefaultSettingsText()
	fill := func(dst *string, fallback string) {
		if *dst == "" {
			*dst = fallback
		}
	}
	fill(&in.Title, def.Title)
	fill(&in.TabAudio, def.TabAudio)
	fill(&in.TabVideo, def.TabVideo)
	fill(&in.TabControls, def.TabControls)
	fill(&in.TabLanguage, def.TabLanguage)
	fill(&in.TabAccessibility, def.TabAccessibility)
	fill(&in.MuteAll, def.MuteAll)
	fill(&in.MasterVolume, def.MasterVolume)
	fill(&in.MonoAudio, def.MonoAudio)
	fill(&in.MonoOn, def.MonoOn)
	fill(&in.MonoOff, def.MonoOff)
	fill(&in.Balance, def.Balance)
	fill(&in.BalanceLeft, def.BalanceLeft)
	fill(&in.BalanceCenter, def.BalanceCenter)
	fill(&in.BalanceRight, def.BalanceRight)
	fill(&in.WindowMode, def.WindowMode)
	fill(&in.Windowed, def.Windowed)
	fill(&in.Fullscreen, def.Fullscreen)
	fill(&in.Resolution, def.Resolution)
	fill(&in.ScaleMode, def.ScaleMode)
	fill(&in.IntegerScale, def.IntegerScale)
	fill(&in.QualityPreset, def.QualityPreset)
	fill(&in.VSync, def.VSync)
	fill(&in.FrameCap, def.FrameCap)
	fill(&in.Uncapped, def.Uncapped)
	fill(&in.ControlsHint, def.ControlsHint)
	fill(&in.CapturePrompt, def.CapturePrompt)
	fill(&in.ConflictStatus, def.ConflictStatus)
	fill(&in.Unbound, def.Unbound)
	fill(&in.ResetControls, def.ResetControls)
	fill(&in.ActionToggle, def.ActionToggle)
	fill(&in.ActionMash, def.ActionMash)
	fill(&in.MinPressTime, def.MinPressTime)
	fill(&in.AcceptDelay, def.AcceptDelay)
	fill(&in.AssistOff, def.AssistOff)
	fill(&in.LanguageHint, def.LanguageHint)
	fill(&in.SystemDefault, def.SystemDefault)
	fill(&in.Subtitles, def.Subtitles)
	fill(&in.SFXCaptions, def.SFXCaptions)
	fill(&in.TextScale, def.TextScale)
	fill(&in.HighContrast, def.HighContrast)
	fill(&in.ReduceMotion, def.ReduceMotion)
	fill(&in.ScreenShake, def.ScreenShake)
	fill(&in.ReduceFlashes, def.ReduceFlashes)
	fill(&in.ColorBlind, def.ColorBlind)
	fill(&in.GameSpeed, def.GameSpeed)
	fill(&in.Invincibility, def.Invincibility)
	fill(&in.AutoAim, def.AutoAim)
	fill(&in.HoldToToggle, def.HoldToToggle)
	fill(&in.Apply, def.Apply)
	fill(&in.Save, def.Save)
	fill(&in.Reset, def.Reset)
	fill(&in.Back, def.Back)
	fill(&in.SavedStatus, def.SavedStatus)
	fill(&in.AppliedStatus, def.AppliedStatus)
	fill(&in.ResetStatus, def.ResetStatus)
	return in
}

// HighContrastTheme returns a high-contrast dark gameui Theme suitable when Accessibility.HighContrast is enabled.
func HighContrastTheme() Theme {
	th := DefaultTheme()
	th.PanelBackground = core.RGBA(0x000000, 0.96)
	th.PanelBorder = core.RGB(0xffffff)
	th.Background = core.RGB(0x0a0a0a)
	th.Hover = core.RGB(0x1f2937)
	th.Active = core.RGB(0x111827)
	th.FocusedBorder = core.RGB(0xfde047)
	th.Disabled = core.RGB(0x18181b)
	th.Border = core.RGB(0xe5e7eb)
	th.Text = core.RGB(0xffffff)
	th.DisabledText = core.RGB(0x9ca3af)
	th.MutedText = core.RGB(0xe5e7eb)
	th.Accent = core.RGB(0x38bdf8)
	th.AccentHover = core.RGB(0x7dd3fc)
	th.AccentActive = core.RGB(0x0284c7)
	th.Track = core.RGB(0x1f2937)
	th.BorderWidth = 2.0
	th.FocusWidth = 3.5
	return th
}

// SettingsResolutionFrom reads the persisted resolution ID from s.Custom["resolution"], or "" if unset.
func SettingsResolutionFrom(s save.Settings) string {
	if len(s.Custom) == 0 {
		return ""
	}
	raw, ok := s.Custom[SettingsCustomResolution]
	if !ok || len(raw) == 0 {
		return ""
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return ""
	}
	return strings.TrimSpace(id)
}

// SetSettingsResolution writes id into s.Custom["resolution"].
func SetSettingsResolution(s *save.Settings, id string) {
	if s == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		delete(s.Custom, SettingsCustomResolution)
		return
	}
	if s.Custom == nil {
		s.Custom = make(map[string]json.RawMessage)
	}
	raw, _ := json.Marshal(id)
	s.Custom[SettingsCustomResolution] = raw
}

// ParseSettingsResolution parses a persisted resolution ID (such as "1280x720", "1280×720",
// "native", or "") into target dimensions in points. "native" and "" return (0, 0, true).
func ParseSettingsResolution(id string) (width, height int, ok bool) {
	s := strings.ToLower(strings.TrimSpace(id))
	if s == "" || s == "native" {
		return 0, 0, true
	}
	s = strings.ReplaceAll(s, "×", "x")
	parts := strings.Split(s, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// ApplySettingsResolution reads the persisted resolution ID from s.Custom["resolution"],
// resolves its dimensions against resolutions (falling back to ParseSettingsResolution),
// and applies it to v via v.ApplyResolution. A nil v is a no-op.
func ApplySettingsResolution(v *screen.Video, s save.Settings, resolutions ...SettingsResolution) {
	if v == nil {
		return
	}
	id := SettingsResolutionFrom(s)
	for _, r := range resolutions {
		if r.ID == id {
			v.ApplyResolution(core.Points(r.Width), core.Points(r.Height))
			return
		}
	}
	if w, h, ok := ParseSettingsResolution(id); ok {
		v.ApplyResolution(core.Points(w), core.Points(h))
	}
}

// SettingsReduceFlashesFrom reports whether flash reduction is enabled in s,
// reading s.Accessibility.ReduceFlashes and falling back to legacy s.Custom["reduce_flashes"].
//
// Deprecated: Read save.AccessibilitySettings.ReduceFlashes directly.
func SettingsReduceFlashesFrom(s save.Settings) bool {
	if s.Accessibility.ReduceFlashes {
		return true
	}
	if len(s.Custom) == 0 {
		return false
	}
	raw, ok := s.Custom[SettingsCustomReduceFlashes]
	if !ok || len(raw) == 0 {
		return false
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	return v
}

// SetSettingsReduceFlashes sets s.Accessibility.ReduceFlashes and clears any legacy
// s.Custom["reduce_flashes"] entry.
//
// Deprecated: Set save.AccessibilitySettings.ReduceFlashes directly.
func SetSettingsReduceFlashes(s *save.Settings, reduce bool) {
	if s == nil {
		return
	}
	s.Accessibility.ReduceFlashes = reduce
	delete(s.Custom, SettingsCustomReduceFlashes)
	if len(s.Custom) == 0 {
		s.Custom = nil
	}
}

// SettingsMenuConfig configures a ready-to-use SettingsMenu. Every field is optional.
type SettingsMenuConfig struct {
	// Store optionally loads initial player settings and persists changes when Save is called.
	Store *save.DefaultSettingsStore
	// Targets optionally receives validated settings via save.Apply when Apply or Save is called.
	Targets save.SettingsTargets
	// Video optionally receives the selected windowed resolution via ApplySettingsResolution whenever Apply or Save runs.
	Video *screen.Video
	// Env optionally supplies the platform environment so the Video tab hides controls that cannot apply on the current platform (hiding Window Mode where screen.CanFullScreen is false and hiding Resolution where screen.SupportsWindowResolution is false) and keeps Window Mode synchronized with Env.FullScreen. When nil, desktop capabilities are assumed unless HideResolution is set.
	Env *core.Env
	// Scope optionally resolves physical key codes to labels in the active keyboard layout. When nil, readable QWERTY fallback labels are used.
	Scope *core.Scope
	// Initial supplies starting settings when Store is nil (or if Store.Load fails).
	Initial *save.Settings
	// Defaults supplies baseline settings restored by Reset.
	Defaults *save.Settings

	// AudioBuses configures group volume sliders in the Audio tab (defaults to music, sfx, ui).
	AudioBuses []SettingsAudioBus
	// Resolutions configures selectable resolutions in the Video tab (defaults to Native, 960x540, 1280x720, 1920x1080).
	Resolutions []SettingsResolution
	// HideResolution omits the Resolution row from the Video tab even on desktop or when Env is nil.
	HideResolution bool
	// ShowVSync includes the VSync toggle in the Video tab. False by default because NSGo v0.3.4 does not yet expose swap-interval control; enable only when wiring save.DisplaySettings.VSync to a real backend control.
	ShowVSync bool
	// ScaleModes configures scale mode options in the Video tab (defaults to fit, fill, stretch, expand).
	ScaleModes []SettingsOptionItem
	// QualityPresets configures quality preset options in the Video tab (defaults to auto, low, medium, high).
	QualityPresets []SettingsOptionItem
	// FrameCaps configures selectable FPS caps in the Video tab; 0 means uncapped (defaults to 0, 30, 60, 120).
	FrameCaps []int
	// Actions configures rebindable input actions in the Controls tab.
	Actions []SettingsAction
	// OnConflict selects how CaptureBinding handles a binding used by another action in the same group. The zero value is ConflictSwap.
	OnConflict SettingsConflictPolicy
	// DeviceTracker optionally supplies the controller family used by binding labels and is updated by StepGamepad. When nil, generic gamepad labels are used.
	DeviceTracker *input.DeviceTracker
	// Locales configures selectable languages in the Language tab.
	Locales []SettingsLocale
	// ColorBlindModes configures color-blind palette modes in the Accessibility tab.
	ColorBlindModes []SettingsOptionItem

	// Text customizes displayed labels; unset fields inherit DefaultSettingsText.
	Text SettingsText
	// Theme optionally overrides the Theme used by the menu's Root.
	// When nil, DefaultTheme is used and switches automatically to HighContrastTheme while Accessibility.HighContrast is true.
	Theme *Theme
	// Viewport optionally attaches virtual-resolution scaling to the menu's Root.
	Viewport *Viewport

	// AutoApply dispatches Targets and OnApply immediately whenever any control changes.
	AutoApply bool
	// AutoSave persists changes to Store immediately whenever any control changes.
	AutoSave bool

	// OnChange runs whenever any setting changes in the working copy.
	OnChange func(save.Settings)
	// OnApply runs when Apply or Save applies settings.
	OnApply func(save.Settings) error
	// OnSave runs after Save succeeds.
	OnSave func(save.Settings) error
	// OnReset runs after Reset restores defaults.
	OnReset func(save.Settings)
	// OnBack runs when Back or NavBack is triggered outside capture mode.
	OnBack func()
	// OnResolutionChange runs when the selected resolution changes.
	OnResolutionChange func(SettingsResolution)
}

// SettingsMenu is a ready-to-use, gameui-based settings screen covering Audio, Video,
// Controls (rebinding), Language, and Accessibility, persisted via save.DefaultSettingsStore
// and operable with mouse, touch, keyboard, and gamepad alone.
type SettingsMenu struct {
	store     *save.DefaultSettingsStore
	targets   save.SettingsTargets
	video     *screen.Video
	defaults  save.Settings
	settings  save.Settings
	savedSnap save.Settings
	dirty     bool
	lastErr   error

	showResolution bool
	showVSync      bool

	audioBuses      []SettingsAudioBus
	resolutions     []SettingsResolution
	scaleModes      []SettingsOptionItem
	qualityPresets  []SettingsOptionItem
	frameCaps       []int
	actions         []SettingsAction
	onConflict      SettingsConflictPolicy
	deviceTracker   *input.DeviceTracker
	locales         []SettingsLocale
	colorBlindModes []SettingsOptionItem

	text      SettingsText
	userTheme *Theme
	autoApply bool
	autoSave  bool

	env                 *core.Env
	scope               *core.Scope
	canFullScreen       bool
	lastEnvFull         bool
	lastEnvReduceMotion bool
	lastEnvHighContrast bool
	lastEnvFontScale    float32

	onChange           func(save.Settings)
	onApply            func(save.Settings) error
	onSave             func(save.Settings) error
	onReset            func(save.Settings)
	onBack             func()
	onResolutionChange func(SettingsResolution)

	root      *Root
	nav       *SpatialNavigator
	activeTab SettingsTab

	titleLabel  *Label
	statusLabel *Label
	tabButtons  [settingsTabCount]*Button
	tabPanels   [settingsTabCount]*VBox
	tabControls [settingsTabCount][]Widget

	// Audio widgets
	muteToggle    *Toggle
	masterSlider  *Slider
	busSliders    []*Slider
	monoBtn       *Button
	balanceSlider *Slider

	// Video widgets
	windowModeBtn   *Button
	resolutionBtn   *Button
	scaleModeBtn    *Button
	integerScaleTog *Toggle
	qualityBtn      *Button
	vsyncToggle     *Toggle
	frameCapBtn     *Button

	// Controls widgets
	controlsHintLabel  *Label
	actionButtons      []*Button
	actionToggleChecks []actionAssistToggle
	actionMashChecks   []actionAssistToggle
	minPressSlider     *Slider
	acceptDelaySlider  *Slider
	resetControlsBtn   *Button
	capturingAction    string

	// Language widgets
	languageHintLabel *Label
	localeButtons     []*Button

	// Accessibility widgets
	captionsToggle      *Toggle
	sfxCaptionsToggle   *Toggle
	textScaleSlider     *Slider
	highContrastToggle  *Toggle
	reduceMotionToggle  *Toggle
	screenShakeSlider   *Slider
	reduceFlashesToggle *Toggle
	colorBlindBtn       *Button
	gameSpeedSlider     *Slider
	invincibilityToggle *Toggle
	autoAimToggle       *Toggle
	holdToToggleToggle  *Toggle

	// Footer widgets
	applyBtn *Button
	saveBtn  *Button
	resetBtn *Button
	backBtn  *Button

	// Cycle handlers keyed by focusable widget for NavLeft/NavRight stepping.
	cycleHandlers map[Widget]func(delta int)

	// Active touch tracking for OnTouch.
	hasActiveTouch bool
	activeTouchID  int

	// Gamepad edge-detection state for StepGamepad.
	prevPadButtons map[int]uint32
	prevStickX     int
	prevStickY     int

	// suppressCallbacks avoids feedback loops while syncing widget values from settings.
	suppressCallbacks bool
}

// NewSettingsMenu constructs a SettingsMenu wired to cfg and ready to draw and receive input.
func NewSettingsMenu(cfg SettingsMenuConfig) *SettingsMenu {
	showRes := !cfg.HideResolution && (cfg.Env == nil || screen.SupportsWindowResolution(cfg.Env))
	m := &SettingsMenu{
		store:              cfg.Store,
		targets:            cfg.Targets,
		video:              cfg.Video,
		showResolution:     showRes,
		showVSync:          cfg.ShowVSync,
		text:               resolveSettingsText(cfg.Text),
		userTheme:          cfg.Theme,
		autoApply:          cfg.AutoApply,
		autoSave:           cfg.AutoSave,
		env:                cfg.Env,
		scope:              cfg.Scope,
		onConflict:         cfg.OnConflict,
		deviceTracker:      cfg.DeviceTracker,
		canFullScreen:      screen.CanFullScreen(cfg.Env),
		onChange:           cfg.OnChange,
		onApply:            cfg.OnApply,
		onSave:             cfg.OnSave,
		onReset:            cfg.OnReset,
		onBack:             cfg.OnBack,
		onResolutionChange: cfg.OnResolutionChange,
		cycleHandlers:      make(map[Widget]func(delta int)),
		prevPadButtons:     make(map[int]uint32),
	}

	m.audioBuses = defaultAudioBuses(cfg.AudioBuses)
	m.resolutions = defaultResolutions(cfg.Resolutions)
	m.scaleModes = defaultScaleModes(cfg.ScaleModes)
	m.qualityPresets = defaultQualityPresets(cfg.QualityPresets)
	m.frameCaps = defaultFrameCaps(cfg.FrameCaps)
	m.locales = defaultLocales(cfg.Locales, m.text.SystemDefault)
	m.colorBlindModes = defaultColorBlindModes(cfg.ColorBlindModes)

	// Resolve baseline defaults.
	if cfg.Defaults != nil {
		m.defaults = cloneSaveSettings(*cfg.Defaults)
	} else {
		m.defaults = save.DefaultSettings()
	}
	m.defaults.Validate()
	ensureAudioGroups(&m.defaults, m.audioBuses)
	if SettingsResolutionFrom(m.defaults) == "" && len(m.resolutions) > 0 {
		SetSettingsResolution(&m.defaults, m.resolutions[0].ID)
	}

	// Load initial settings.
	var initial save.Settings
	switch {
	case cfg.Store != nil && cfg.Store.Exists():
		loaded, err := cfg.Store.Load()
		if err != nil {
			m.lastErr = err
			if cfg.Initial != nil {
				initial = cloneSaveSettings(*cfg.Initial)
			} else {
				initial = cloneSaveSettings(m.defaults)
			}
		} else {
			initial = loaded
		}
	case cfg.Initial != nil:
		initial = cloneSaveSettings(*cfg.Initial)
	default:
		initial = cloneSaveSettings(m.defaults)
	}
	initial.Validate()
	ensureAudioGroups(&initial, m.audioBuses)
	if SettingsResolutionFrom(initial) == "" && len(m.resolutions) > 0 {
		SetSettingsResolution(&initial, m.resolutions[0].ID)
	}

	m.actions = defaultSettingsActions(cfg.Actions, initial.Bindings, m.defaults.Bindings)
	ensureActionBindings(&m.defaults, m.actions)
	ensureActionBindings(&initial, m.actions)

	if m.env != nil {
		m.lastEnvFull = m.env.FullScreen()
		m.lastEnvReduceMotion = m.env.ReduceMotion()
		m.lastEnvHighContrast = m.env.IncreaseContrast()
		m.lastEnvFontScale = m.env.FontScale()
		if m.canFullScreen && m.env.OS() == core.OSWeb {
			// On the web, startup full-screen requests are deferred until a user gesture, so show the page's actual state rather than a stale saved true.
			initial.Display.Fullscreen = m.lastEnvFull
		}
	}

	m.settings = cloneSaveSettings(initial)
	m.savedSnap = cloneSaveSettings(initial)

	m.buildTree(cfg.Viewport)
	m.syncWidgets()
	if m.deviceTracker != nil {
		m.deviceTracker.OnChange(func(input.DeviceKind) {
			m.syncWidgets()
		})
	}
	m.applyTheme()
	m.SetTab(SettingsTabAudio)
	if len(m.tabControls[SettingsTabAudio]) > 0 {
		m.nav.SetFocus(m.tabControls[SettingsTabAudio][0])
	} else {
		m.nav.SetFocus(m.tabButtons[SettingsTabAudio])
	}
	return m
}

func defaultAudioBuses(in []SettingsAudioBus) []SettingsAudioBus {
	if len(in) == 0 {
		return []SettingsAudioBus{
			{Name: "music", Label: "Music"},
			{Name: "sfx", Label: "SFX"},
			{Name: "ui", Label: "UI"},
		}
	}
	out := make([]SettingsAudioBus, 0, len(in))
	for _, b := range in {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			continue
		}
		label := strings.TrimSpace(b.Label)
		if label == "" {
			label = strings.ToUpper(name[:1]) + name[1:]
		}
		out = append(out, SettingsAudioBus{Name: name, Label: label})
	}
	return out
}

func defaultResolutions(in []SettingsResolution) []SettingsResolution {
	if len(in) == 0 {
		return []SettingsResolution{
			{ID: "native", Label: "Native"},
			{ID: "960x540", Label: "960x540", Width: 960, Height: 540},
			{ID: "1280x720", Label: "1280x720", Width: 1280, Height: 720},
			{ID: "1920x1080", Label: "1920x1080", Width: 1920, Height: 1080},
		}
	}
	out := make([]SettingsResolution, 0, len(in))
	for _, r := range in {
		id := strings.TrimSpace(r.ID)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(r.Label)
		if label == "" {
			label = id
		}
		out = append(out, SettingsResolution{ID: id, Label: label, Width: r.Width, Height: r.Height})
	}
	if len(out) == 0 {
		return defaultResolutions(nil)
	}
	return out
}

func defaultScaleModes(in []SettingsOptionItem) []SettingsOptionItem {
	if len(in) == 0 {
		return []SettingsOptionItem{
			{Value: "fit", Label: "Fit"},
			{Value: "fill", Label: "Fill"},
			{Value: "stretch", Label: "Stretch"},
			{Value: "expand", Label: "Expand"},
		}
	}
	return cloneOptionItems(in)
}

func defaultQualityPresets(in []SettingsOptionItem) []SettingsOptionItem {
	if len(in) == 0 {
		out := make([]SettingsOptionItem, 0, 1+len(quality.Levels()))
		out = append(out, SettingsOptionItem{Value: quality.SettingAuto, Label: "Auto"})
		for _, lv := range quality.Levels() {
			name := lv.String()
			out = append(out, SettingsOptionItem{Value: name, Label: strings.ToUpper(name[:1]) + name[1:]})
		}
		return out
	}
	return cloneOptionItems(in)
}

func defaultFrameCaps(in []int) []int {
	if len(in) == 0 {
		return []int{0, 30, 60, 120}
	}
	out := make([]int, 0, len(in))
	for _, fps := range in {
		if fps < 0 {
			fps = 0
		}
		if !slices.Contains(out, fps) {
			out = append(out, fps)
		}
	}
	if len(out) == 0 {
		return []int{0, 30, 60, 120}
	}
	return out
}

func defaultSettingsActions(in []SettingsAction, initialBindings, defaultBindings map[string][]string) []SettingsAction {
	if len(in) > 0 {
		out := make([]SettingsAction, 0, len(in))
		for _, act := range in {
			name := strings.TrimSpace(act.Name)
			if name == "" {
				continue
			}
			label := strings.TrimSpace(act.Label)
			if label == "" {
				label = strings.ToUpper(name[:1]) + name[1:]
			}
			defs := slices.Clone(act.Defaults)
			if len(defs) == 0 && len(defaultBindings[name]) > 0 {
				defs = slices.Clone(defaultBindings[name])
			}
			out = append(out, SettingsAction{
				Name:        name,
				Label:       label,
				Group:       strings.TrimSpace(act.Group),
				Defaults:    defs,
				AllowToggle: act.AllowToggle,
				AllowMash:   act.AllowMash,
			})
		}
		if len(out) > 0 {
			return out
		}
	}
	if len(initialBindings) > 0 || len(defaultBindings) > 0 {
		names := make([]string, 0, len(initialBindings)+len(defaultBindings))
		for k := range defaultBindings {
			names = append(names, k)
		}
		for k := range initialBindings {
			if !slices.Contains(names, k) {
				names = append(names, k)
			}
		}
		slices.Sort(names)
		out := make([]SettingsAction, 0, len(names))
		for _, name := range names {
			defs := slices.Clone(defaultBindings[name])
			if len(defs) == 0 {
				defs = slices.Clone(initialBindings[name])
			}
			out = append(out, SettingsAction{
				Name:     name,
				Label:    strings.ToUpper(name[:1]) + name[1:],
				Defaults: defs,
			})
		}
		return out
	}
	return []SettingsAction{
		{Name: "left", Label: "Move Left", Defaults: []string{"key:a", "gamepad:dpad_left"}},
		{Name: "right", Label: "Move Right", Defaults: []string{"key:d", "gamepad:dpad_right"}},
		{Name: "jump", Label: "Jump", Defaults: []string{"key:space", "gamepad:south"}},
		{Name: "confirm", Label: "Confirm", Defaults: []string{"key:enter", "gamepad:south"}},
		{Name: "pause", Label: "Pause", Defaults: []string{"key:escape", "gamepad:start"}},
	}
}

func defaultLocales(in []SettingsLocale, sysLabel string) []SettingsLocale {
	if len(in) == 0 {
		return []SettingsLocale{
			{Tag: "", Label: sysLabel},
			{Tag: "en", Label: "English"},
			{Tag: "zh-Hans", Label: "简体中文"},
			{Tag: "ja", Label: "日本語"},
			{Tag: "es", Label: "Español"},
		}
	}
	out := make([]SettingsLocale, 0, len(in))
	for _, loc := range in {
		tag := strings.TrimSpace(loc.Tag)
		label := strings.TrimSpace(loc.Label)
		if label == "" {
			if tag == "" {
				label = sysLabel
			} else {
				label = tag
			}
		}
		out = append(out, SettingsLocale{Tag: tag, Label: label})
	}
	return out
}

func defaultColorBlindModes(in []SettingsOptionItem) []SettingsOptionItem {
	if len(in) == 0 {
		return []SettingsOptionItem{
			{Value: "none", Label: "None"},
			{Value: "protanopia", Label: "Protanopia"},
			{Value: "deuteranopia", Label: "Deuteranopia"},
			{Value: "tritanopia", Label: "Tritanopia"},
		}
	}
	return cloneOptionItems(in)
}

func cloneOptionItems(in []SettingsOptionItem) []SettingsOptionItem {
	out := make([]SettingsOptionItem, 0, len(in))
	for _, it := range in {
		val := strings.TrimSpace(it.Value)
		label := strings.TrimSpace(it.Label)
		if label == "" {
			label = val
		}
		out = append(out, SettingsOptionItem{Value: val, Label: label})
	}
	return out
}

func ensureAudioGroups(s *save.Settings, buses []SettingsAudioBus) {
	if s.Audio.Groups == nil {
		s.Audio.Groups = make(map[string]float64, len(buses))
	}
	for _, b := range buses {
		if _, ok := s.Audio.Groups[b.Name]; !ok {
			s.Audio.Groups[b.Name] = 1.0
		}
	}
}

func ensureActionBindings(s *save.Settings, actions []SettingsAction) {
	if s.Bindings == nil {
		s.Bindings = make(map[string][]string, len(actions))
	}
	for _, act := range actions {
		if _, ok := s.Bindings[act.Name]; !ok && len(act.Defaults) > 0 {
			s.Bindings[act.Name] = slices.Clone(act.Defaults)
		}
	}
}

func cloneSaveSettings(s save.Settings) save.Settings {
	out := s
	if s.Audio.Groups != nil {
		out.Audio.Groups = maps.Clone(s.Audio.Groups)
	}
	if s.Audio.Mono != nil {
		v := *s.Audio.Mono
		out.Audio.Mono = &v
	}
	if s.Bindings != nil {
		out.Bindings = make(map[string][]string, len(s.Bindings))
		for k, v := range s.Bindings {
			out.Bindings[k] = slices.Clone(v)
		}
	}
	if s.InputAssists.Toggle != nil {
		out.InputAssists.Toggle = slices.Clone(s.InputAssists.Toggle)
	}
	if s.InputAssists.MashToHold != nil {
		out.InputAssists.MashToHold = slices.Clone(s.InputAssists.MashToHold)
	}
	if s.Custom != nil {
		out.Custom = make(map[string]json.RawMessage, len(s.Custom))
		for k, v := range s.Custom {
			out.Custom[k] = slices.Clone(v)
		}
	}
	return out
}

func (m *SettingsMenu) buildTree(vp *Viewport) {
	m.titleLabel = &Label{
		Text:      m.text.Title,
		Font:      core.Font{Size: 18, Weight: core.FontWeightBold},
		TextAlign: core.TextAlignStart,
		Flex:      1,
	}
	m.statusLabel = &Label{
		Text:      "",
		Font:      core.Font{Size: 13},
		TextAlign: core.TextAlignEnd,
		Flex:      1,
	}
	headerRow := &HBox{
		Height:   24,
		Spacing:  8,
		Align:    AlignCenter,
		Children: []Widget{m.titleLabel, m.statusLabel},
	}

	// Tab bar
	tabWidgets := make([]Widget, settingsTabCount)
	for i := range settingsTabCount {
		tab := SettingsTab(i)
		btn := &Button{
			Flex:   1,
			Height: 32,
			OnClick: func() {
				m.SetTab(tab)
			},
		}
		btn.FrameDrawer = m.tabFrameDrawer(tab)
		m.tabButtons[i] = btn
		tabWidgets[i] = btn
	}
	tabBar := &HBox{
		Height:   32,
		Spacing:  6,
		Children: tabWidgets,
	}

	// Build each tab's panel
	m.buildAudioTab()
	m.buildVideoTab()
	m.buildControlsTab()
	m.buildLanguageTab()
	m.buildAccessibilityTab()

	contentChildren := make([]Widget, 0, settingsTabCount)
	for i := range settingsTabCount {
		contentChildren = append(contentChildren, m.tabPanels[i])
	}
	contentPanel := &Panel{
		Flex:    1,
		Padding: core.All(10),
		Child: &VBox{
			Spacing:  0,
			Align:    AlignStretch,
			Children: contentChildren,
		},
	}

	// Footer bar
	m.applyBtn = &Button{
		Text:    m.text.Apply,
		Flex:    1,
		Height:  34,
		OnClick: func() { _ = m.Apply() },
	}
	m.saveBtn = &Button{
		Text:    m.text.Save,
		Flex:    1,
		Height:  34,
		OnClick: func() { _ = m.Save() },
	}
	m.resetBtn = &Button{
		Text:    m.text.Reset,
		Flex:    1,
		Height:  34,
		OnClick: func() { m.Reset() },
	}
	m.backBtn = &Button{
		Text:    m.text.Back,
		Flex:    1,
		Height:  34,
		OnClick: func() { m.Back() },
	}
	footerBar := &HBox{
		Height:   34,
		Spacing:  8,
		Children: []Widget{m.applyBtn, m.saveBtn, m.resetBtn, m.backBtn},
	}

	rootPanel := &Panel{
		Padding: core.All(12),
		Child: &VBox{
			Spacing:  8,
			Align:    AlignStretch,
			Children: []Widget{headerRow, tabBar, contentPanel, footerBar},
		},
	}

	m.root = NewRoot(rootPanel)
	m.root.Viewport = vp
	m.nav = NewSpatialNavigator(m.root)
	m.nav.WrapAround = true
	m.nav.SliderSteps = true
	m.nav.OnBack = func() { m.Back() }
}

func (m *SettingsMenu) tabFrameDrawer(tab SettingsTab) FrameDrawer {
	return func(d *core.Drawing, b core.Bounds, state WidgetState) {
		th := m.root.theme()
		x, y := float32(b.Origin.X), float32(b.Origin.Y)
		w, h := float32(b.Size.Width), float32(b.Size.Height)
		if w <= 0 || h <= 0 {
			return
		}
		radius := resolveRadius(0, th.CornerRadius)
		shape := rectPath(x, y, w, h, radius)
		fill := th.Background
		border := th.Border
		if m.activeTab == tab {
			fill = th.Accent
			border = th.AccentHover
			if state.Pressed {
				fill = th.AccentActive
			} else if state.Hovered {
				fill = th.AccentHover
			}
		} else {
			if state.Pressed {
				fill = th.Active
				border = th.Accent
			} else if state.Hovered {
				fill = th.Hover
			}
		}
		d.SetFillColor(fill).FillPath(shape, core.NonZero)
		if border != 0 && border != core.Transparent && th.BorderWidth > 0 {
			d.SetStrokeColor(border).SetLineWidth(th.BorderWidth).StrokePath(shape)
		}
	}
}

func (m *SettingsMenu) buildAudioTab() {
	m.muteToggle = &Toggle{
		Label:  m.text.MuteAll,
		Style:  ToggleSwitch,
		Height: 32,
		OnChange: func(v bool) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Audio.Muted = v
			m.onSettingMutated()
		},
	}
	m.masterSlider = &Slider{
		Min:    0,
		Max:    1,
		Step:   0.05,
		Height: 32,
		OnChange: func(v float32) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Audio.Master = roundStep(float64(v), 0.01)
			m.onSettingMutated()
		},
	}
	children := []Widget{m.muteToggle, m.masterSlider}
	controls := []Widget{m.muteToggle, m.masterSlider}

	m.busSliders = make([]*Slider, len(m.audioBuses))
	for i, bus := range m.audioBuses {
		busName := bus.Name
		sld := &Slider{
			Min:    0,
			Max:    1,
			Step:   0.05,
			Height: 32,
			OnChange: func(v float32) {
				if m.suppressCallbacks {
					return
				}
				if m.settings.Audio.Groups == nil {
					m.settings.Audio.Groups = make(map[string]float64)
				}
				m.settings.Audio.Groups[busName] = roundStep(float64(v), 0.01)
				m.onSettingMutated()
			},
		}
		m.busSliders[i] = sld
		children = append(children, sld)
		controls = append(controls, sld)
	}

	stepMono := func(delta int) {
		idx := 0
		if m.settings.Audio.Mono != nil {
			if *m.settings.Audio.Mono {
				idx = 1
			} else {
				idx = 2
			}
		}
		next := (idx + delta + 3) % 3
		switch next {
		case 0:
			m.settings.Audio.Mono = nil
		case 1:
			v := true
			m.settings.Audio.Mono = &v
		case 2:
			v := false
			m.settings.Audio.Mono = &v
		}
		m.onSettingMutated()
	}
	m.monoBtn = &Button{
		Height:  32,
		OnClick: func() { stepMono(1) },
	}
	m.cycleHandlers[m.monoBtn] = stepMono
	children = append(children, m.monoBtn)
	controls = append(controls, m.monoBtn)

	m.balanceSlider = &Slider{
		Min:    -1,
		Max:    1,
		Step:   0.1,
		Height: 32,
		OnChange: func(v float32) {
			if m.suppressCallbacks {
				return
			}
			b := roundStep(float64(v), 0.01)
			if math.Abs(b) < 0.005 {
				b = 0
			}
			m.settings.Audio.Balance = b
			m.onSettingMutated()
		},
	}
	children = append(children, m.balanceSlider)
	controls = append(controls, m.balanceSlider)

	m.tabPanels[SettingsTabAudio] = &VBox{
		Spacing:  8,
		Align:    AlignStretch,
		Flex:     1,
		Children: children,
	}
	m.tabControls[SettingsTabAudio] = controls
}

func (m *SettingsMenu) buildVideoTab() {
	if m.canFullScreen {
		stepWindowMode := func(_ int) {
			m.settings.Display.Fullscreen = !m.settings.Display.Fullscreen
			m.onSettingMutated()
		}
		m.windowModeBtn = &Button{
			Height:  30,
			OnClick: func() { stepWindowMode(1) },
		}
		m.cycleHandlers[m.windowModeBtn] = stepWindowMode
	}

	controls := make([]Widget, 0, 7)
	if m.windowModeBtn != nil {
		controls = append(controls, m.windowModeBtn)
	}

	if m.showResolution {
		stepResolution := func(delta int) {
			if len(m.resolutions) == 0 {
				return
			}
			cur := SettingsResolutionFrom(m.settings)
			idx := 0
			for i, r := range m.resolutions {
				if r.ID == cur {
					idx = i
					break
				}
			}
			next := (idx + delta + len(m.resolutions)) % len(m.resolutions)
			chosen := m.resolutions[next]
			SetSettingsResolution(&m.settings, chosen.ID)
			m.onSettingMutated()
			if m.onResolutionChange != nil {
				m.onResolutionChange(chosen)
			}
		}
		m.resolutionBtn = &Button{
			Height:  30,
			OnClick: func() { stepResolution(1) },
		}
		m.cycleHandlers[m.resolutionBtn] = stepResolution
		controls = append(controls, m.resolutionBtn)
	}

	stepScaleMode := func(delta int) {
		if len(m.scaleModes) == 0 {
			return
		}
		idx := optionIndex(m.scaleModes, m.settings.Display.ScaleMode)
		next := (idx + delta + len(m.scaleModes)) % len(m.scaleModes)
		m.settings.Display.ScaleMode = m.scaleModes[next].Value
		m.onSettingMutated()
	}
	m.scaleModeBtn = &Button{
		Height:  30,
		OnClick: func() { stepScaleMode(1) },
	}
	m.cycleHandlers[m.scaleModeBtn] = stepScaleMode
	controls = append(controls, m.scaleModeBtn)

	m.integerScaleTog = &Toggle{
		Label:  m.text.IntegerScale,
		Style:  ToggleSwitch,
		Height: 30,
		OnChange: func(v bool) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Display.IntegerScale = v
			m.onSettingMutated()
		},
	}
	controls = append(controls, m.integerScaleTog)

	stepQuality := func(delta int) {
		if len(m.qualityPresets) == 0 {
			return
		}
		idx := optionIndex(m.qualityPresets, m.settings.Display.Quality)
		next := (idx + delta + len(m.qualityPresets)) % len(m.qualityPresets)
		m.settings.Display.Quality = m.qualityPresets[next].Value
		m.onSettingMutated()
	}
	m.qualityBtn = &Button{
		Height:  30,
		OnClick: func() { stepQuality(1) },
	}
	m.cycleHandlers[m.qualityBtn] = stepQuality
	controls = append(controls, m.qualityBtn)

	if m.showVSync {
		m.vsyncToggle = &Toggle{
			Label:  m.text.VSync,
			Style:  ToggleSwitch,
			Height: 30,
			OnChange: func(v bool) {
				if m.suppressCallbacks {
					return
				}
				m.settings.Display.VSync = v
				m.onSettingMutated()
			},
		}
		controls = append(controls, m.vsyncToggle)
	}

	stepFrameCap := func(delta int) {
		if len(m.frameCaps) == 0 {
			return
		}
		idx := slices.Index(m.frameCaps, m.settings.Display.FrameCap)
		if idx < 0 {
			idx = 0
		}
		next := (idx + delta + len(m.frameCaps)) % len(m.frameCaps)
		m.settings.Display.FrameCap = m.frameCaps[next]
		m.onSettingMutated()
	}
	m.frameCapBtn = &Button{
		Height:  30,
		OnClick: func() { stepFrameCap(1) },
	}
	m.cycleHandlers[m.frameCapBtn] = stepFrameCap
	controls = append(controls, m.frameCapBtn)

	m.tabPanels[SettingsTabVideo] = &VBox{
		Spacing:  6,
		Align:    AlignStretch,
		Flex:     1,
		Hidden:   true,
		Children: controls,
	}
	m.tabControls[SettingsTabVideo] = controls
}

type actionAssistToggle struct {
	action string
	label  string
	toggle *Toggle
}

func toggleStringInSlice(list []string, value string, include bool) []string {
	idx := slices.Index(list, value)
	if include {
		if idx >= 0 {
			return list
		}
		return append(list, value)
	}
	if idx < 0 {
		return list
	}
	out := slices.Delete(slices.Clone(list), idx, idx+1)
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *SettingsMenu) buildControlsTab() {
	assistRows := 0
	for _, act := range m.actions {
		if act.AllowToggle {
			assistRows++
		}
		if act.AllowMash {
			assistRows++
		}
	}
	totalRows := len(m.actions) + assistRows + 3 // minPressSlider + acceptDelaySlider + resetControlsBtn
	var rowH float32 = 26
	var spacing float32 = 4
	if totalRows > 12 {
		rowH = 20
		spacing = 2
	} else if totalRows > 10 {
		rowH = 24
		spacing = 3
	}

	m.controlsHintLabel = &Label{
		Text:      m.text.ControlsHint,
		Font:      core.Font{Size: 13},
		TextAlign: core.TextAlignStart,
		Height:    20,
	}
	children := make([]Widget, 0, totalRows+1)
	children = append(children, m.controlsHintLabel)
	controls := make([]Widget, 0, totalRows)
	m.actionButtons = make([]*Button, len(m.actions))

	for i, act := range m.actions {
		actionName := act.Name
		btn := &Button{
			Height: rowH,
			OnClick: func() {
				if m.capturingAction == actionName {
					m.CancelCapture()
					return
				}
				m.BeginCapture(actionName)
			},
		}
		m.actionButtons[i] = btn
		children = append(children, btn)
		controls = append(controls, btn)
	}

	m.actionToggleChecks = nil
	m.actionMashChecks = nil
	for _, act := range m.actions {
		actionName := act.Name
		actionLabel := act.Label
		if act.AllowToggle {
			tog := &Toggle{
				Style:  ToggleSwitch,
				Height: rowH,
				OnChange: func(v bool) {
					if m.suppressCallbacks {
						return
					}
					m.settings.InputAssists.Toggle = toggleStringInSlice(m.settings.InputAssists.Toggle, actionName, v)
					m.onSettingMutated()
				},
			}
			m.actionToggleChecks = append(m.actionToggleChecks, actionAssistToggle{
				action: actionName,
				label:  actionLabel,
				toggle: tog,
			})
			children = append(children, tog)
			controls = append(controls, tog)
		}
		if act.AllowMash {
			tog := &Toggle{
				Style:  ToggleSwitch,
				Height: rowH,
				OnChange: func(v bool) {
					if m.suppressCallbacks {
						return
					}
					m.settings.InputAssists.MashToHold = toggleStringInSlice(m.settings.InputAssists.MashToHold, actionName, v)
					m.onSettingMutated()
				},
			}
			m.actionMashChecks = append(m.actionMashChecks, actionAssistToggle{
				action: actionName,
				label:  actionLabel,
				toggle: tog,
			})
			children = append(children, tog)
			controls = append(controls, tog)
		}
	}

	m.minPressSlider = &Slider{
		Min:    0,
		Max:    float32(save.MaxMinPressMs),
		Step:   50,
		Height: rowH,
		OnChange: func(v float32) {
			if m.suppressCallbacks {
				return
			}
			m.settings.InputAssists.MinPressMs = int(math.Round(float64(v)))
			m.onSettingMutated()
		},
	}
	children = append(children, m.minPressSlider)
	controls = append(controls, m.minPressSlider)

	m.acceptDelaySlider = &Slider{
		Min:    0,
		Max:    float32(save.MaxAcceptDelayMs),
		Step:   50,
		Height: rowH,
		OnChange: func(v float32) {
			if m.suppressCallbacks {
				return
			}
			m.settings.InputAssists.AcceptDelayMs = int(math.Round(float64(v)))
			m.onSettingMutated()
		},
	}
	children = append(children, m.acceptDelaySlider)
	controls = append(controls, m.acceptDelaySlider)

	m.resetControlsBtn = &Button{
		Text:    m.text.ResetControls,
		Height:  rowH,
		OnClick: func() { m.ResetControls() },
	}
	children = append(children, m.resetControlsBtn)
	controls = append(controls, m.resetControlsBtn)

	m.tabPanels[SettingsTabControls] = &VBox{
		Spacing:  spacing,
		Align:    AlignStretch,
		Flex:     1,
		Hidden:   true,
		Children: children,
	}
	m.tabControls[SettingsTabControls] = controls
}

func (m *SettingsMenu) buildLanguageTab() {
	m.languageHintLabel = &Label{
		Text:      m.text.LanguageHint,
		Font:      core.Font{Size: 13},
		TextAlign: core.TextAlignStart,
		Height:    20,
	}
	children := []Widget{m.languageHintLabel}
	controls := make([]Widget, 0, len(m.locales))
	m.localeButtons = make([]*Button, len(m.locales))

	for i, loc := range m.locales {
		tag := loc.Tag
		btn := &Button{
			Height: 32,
			OnClick: func() {
				m.settings.Locale = tag
				m.onSettingMutated()
			},
		}
		m.localeButtons[i] = btn
		children = append(children, btn)
		controls = append(controls, btn)
	}

	m.tabPanels[SettingsTabLanguage] = &VBox{
		Spacing:  6,
		Align:    AlignStretch,
		Flex:     1,
		Hidden:   true,
		Children: children,
	}
	m.tabControls[SettingsTabLanguage] = controls
}

func (m *SettingsMenu) buildAccessibilityTab() {
	const rowH float32 = 26

	m.captionsToggle = &Toggle{
		Label:  m.text.Subtitles,
		Style:  ToggleSwitch,
		Height: rowH,
		OnChange: func(v bool) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.Captions = v
			m.onSettingMutated()
		},
	}
	m.sfxCaptionsToggle = &Toggle{
		Label:  m.text.SFXCaptions,
		Style:  ToggleSwitch,
		Height: rowH,
		OnChange: func(v bool) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.SFXCaptions = v
			m.onSettingMutated()
		},
	}
	m.textScaleSlider = &Slider{
		Min:    float32(save.MinCaptionScale),
		Max:    float32(save.MaxCaptionScale),
		Step:   0.25,
		Height: rowH,
		OnChange: func(v float32) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.SetCaptionScale(roundStep(float64(v), 0.05))
			m.onSettingMutated()
		},
	}
	m.highContrastToggle = &Toggle{
		Label:  m.text.HighContrast,
		Style:  ToggleSwitch,
		Height: rowH,
		OnChange: func(v bool) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.SetHighContrast(v)
			m.onSettingMutated()
		},
	}
	m.reduceMotionToggle = &Toggle{
		Label:  m.text.ReduceMotion,
		Style:  ToggleSwitch,
		Height: rowH,
		OnChange: func(v bool) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.SetReduceMotion(v)
			m.onSettingMutated()
		},
	}
	m.screenShakeSlider = &Slider{
		Min:    0,
		Max:    1,
		Step:   0.1,
		Height: rowH,
		OnChange: func(v float32) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.ScreenShake = roundStep(float64(v), 0.01)
			m.onSettingMutated()
		},
	}
	m.reduceFlashesToggle = &Toggle{
		Label:  m.text.ReduceFlashes,
		Style:  ToggleSwitch,
		Height: rowH,
		OnChange: func(v bool) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.ReduceFlashes = v
			m.onSettingMutated()
		},
	}
	stepColorBlind := func(delta int) {
		if len(m.colorBlindModes) == 0 {
			return
		}
		idx := colorBlindOptionIndex(m.colorBlindModes, m.settings.Accessibility.ColorBlindMode)
		next := (idx + delta + len(m.colorBlindModes)) % len(m.colorBlindModes)
		m.settings.Accessibility.ColorBlindMode = save.NormalizeColorBlindMode(m.colorBlindModes[next].Value)
		m.onSettingMutated()
	}
	m.colorBlindBtn = &Button{
		Height:  rowH,
		OnClick: func() { stepColorBlind(1) },
	}
	m.cycleHandlers[m.colorBlindBtn] = stepColorBlind

	m.gameSpeedSlider = &Slider{
		Min:    float32(save.MinGameSpeed),
		Max:    float32(save.MaxGameSpeed),
		Step:   0.25,
		Height: rowH,
		OnChange: func(v float32) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.GameSpeed = roundStep(float64(v), 0.05)
			m.onSettingMutated()
		},
	}
	m.invincibilityToggle = &Toggle{
		Label:  m.text.Invincibility,
		Style:  ToggleSwitch,
		Height: rowH,
		OnChange: func(v bool) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.Invincibility = v
			m.onSettingMutated()
		},
	}
	m.autoAimToggle = &Toggle{
		Label:  m.text.AutoAim,
		Style:  ToggleSwitch,
		Height: rowH,
		OnChange: func(v bool) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.AutoAim = v
			m.onSettingMutated()
		},
	}
	m.holdToToggleToggle = &Toggle{
		Label:  m.text.HoldToToggle,
		Style:  ToggleSwitch,
		Height: rowH,
		OnChange: func(v bool) {
			if m.suppressCallbacks {
				return
			}
			m.settings.Accessibility.HoldToToggle = v
			m.onSettingMutated()
		},
	}

	controls := []Widget{
		m.captionsToggle,
		m.sfxCaptionsToggle,
		m.textScaleSlider,
		m.highContrastToggle,
		m.reduceMotionToggle,
		m.screenShakeSlider,
		m.reduceFlashesToggle,
		m.colorBlindBtn,
		m.gameSpeedSlider,
		m.invincibilityToggle,
		m.autoAimToggle,
		m.holdToToggleToggle,
	}
	m.tabPanels[SettingsTabAccessibility] = &VBox{
		Spacing:  4,
		Align:    AlignStretch,
		Flex:     1,
		Hidden:   true,
		Children: controls,
	}
	m.tabControls[SettingsTabAccessibility] = controls
}

func (m *SettingsMenu) wireNav() {
	controls := m.tabControls[m.activeTab]
	footer := []*Button{m.applyBtn, m.saveBtn, m.resetBtn, m.backBtn}
	activeTabBtn := m.tabButtons[m.activeTab]

	var firstCtrl, lastCtrl Widget
	if len(controls) > 0 {
		firstCtrl = controls[0]
		lastCtrl = controls[len(controls)-1]
	}

	for i := range settingsTabCount {
		prev := m.tabButtons[(i-1+settingsTabCount)%settingsTabCount]
		next := m.tabButtons[(i+1)%settingsTabCount]
		down := Widget(footer[0])
		if firstCtrl != nil {
			down = firstCtrl
		}
		m.nav.SetOverrides(m.tabButtons[i], NavOverrides{
			Left:  prev,
			Right: next,
			Down:  down,
			Up:    footer[0],
		})
	}

	for i, w := range controls {
		up := Widget(activeTabBtn)
		if i > 0 {
			up = controls[i-1]
		}
		down := Widget(footer[0])
		if i+1 < len(controls) {
			down = controls[i+1]
		}
		m.nav.SetOverrides(w, NavOverrides{
			Up:   up,
			Down: down,
		})
	}

	for i, btn := range footer {
		prev := footer[(i-1+len(footer))%len(footer)]
		next := footer[(i+1)%len(footer)]
		up := Widget(activeTabBtn)
		if lastCtrl != nil {
			up = lastCtrl
		}
		m.nav.SetOverrides(btn, NavOverrides{
			Left:  prev,
			Right: next,
			Up:    up,
			Down:  activeTabBtn,
		})
	}
}

func (m *SettingsMenu) applyTheme() {
	if m.userTheme != nil {
		m.root.Theme = m.userTheme
		return
	}
	var th Theme
	if m.settings.Accessibility.EffectiveHighContrast(m.env) {
		th = HighContrastTheme()
	} else {
		th = DefaultTheme()
	}
	scale := clampf(float32(m.settings.Accessibility.EffectiveCaptionScale(m.env)), 0.85, 1.35)
	if scale <= 0 {
		scale = 1
	}
	th.Font.Size = core.Points(float32(th.Font.Size) * scale)
	m.root.Theme = &th
}

func (m *SettingsMenu) syncWidgets() {
	m.suppressCallbacks = true
	defer func() { m.suppressCallbacks = false }()

	m.titleLabel.Text = m.text.Title
	m.tabButtons[SettingsTabAudio].Text = m.text.TabAudio
	m.tabButtons[SettingsTabVideo].Text = m.text.TabVideo
	m.tabButtons[SettingsTabControls].Text = m.text.TabControls
	m.tabButtons[SettingsTabLanguage].Text = m.text.TabLanguage
	m.tabButtons[SettingsTabAccessibility].Text = m.text.TabAccessibility

	// Audio
	m.muteToggle.Label = m.text.MuteAll
	m.muteToggle.Checked = m.settings.Audio.Muted
	m.masterSlider.Label = fmt.Sprintf("%s: %d%%", m.text.MasterVolume, pctInt(m.settings.Audio.Master))
	m.masterSlider.Value = float32(m.settings.Audio.Master)
	for i, bus := range m.audioBuses {
		vol := 1.0
		if v, ok := m.settings.Audio.Groups[bus.Name]; ok {
			vol = v
		}
		m.busSliders[i].Label = fmt.Sprintf("%s: %d%%", bus.Label, pctInt(vol))
		m.busSliders[i].Value = float32(vol)
	}
	monoLabel := m.text.SystemDefault
	if m.settings.Audio.Mono != nil {
		if *m.settings.Audio.Mono {
			monoLabel = m.text.MonoOn
		} else {
			monoLabel = m.text.MonoOff
		}
	}
	m.monoBtn.Text = fmt.Sprintf("%s: < %s >", m.text.MonoAudio, monoLabel)
	balPct := pctInt(m.settings.Audio.Balance)
	switch {
	case balPct < 0:
		m.balanceSlider.Label = fmt.Sprintf("%s: %s %d%%", m.text.Balance, m.text.BalanceLeft, -balPct)
	case balPct > 0:
		m.balanceSlider.Label = fmt.Sprintf("%s: %s %d%%", m.text.Balance, m.text.BalanceRight, balPct)
	default:
		m.balanceSlider.Label = fmt.Sprintf("%s: %s", m.text.Balance, m.text.BalanceCenter)
	}
	m.balanceSlider.Value = float32(m.settings.Audio.Balance)

	// Video
	if m.windowModeBtn != nil {
		winLabel := m.text.Windowed
		if m.settings.Display.Fullscreen {
			winLabel = m.text.Fullscreen
		}
		m.windowModeBtn.Text = fmt.Sprintf("%s: < %s >", m.text.WindowMode, winLabel)
	}

	if m.resolutionBtn != nil {
		resID := SettingsResolutionFrom(m.settings)
		resLabel := resID
		for _, r := range m.resolutions {
			if r.ID == resID {
				resLabel = r.Label
				break
			}
		}
		if resLabel == "" && len(m.resolutions) > 0 {
			resLabel = m.resolutions[0].Label
		}
		m.resolutionBtn.Text = fmt.Sprintf("%s: < %s >", m.text.Resolution, resLabel)
	}

	m.scaleModeBtn.Text = fmt.Sprintf("%s: < %s >", m.text.ScaleMode, optionLabel(m.scaleModes, m.settings.Display.ScaleMode))
	m.integerScaleTog.Label = m.text.IntegerScale
	m.integerScaleTog.Checked = m.settings.Display.IntegerScale
	m.qualityBtn.Text = fmt.Sprintf("%s: < %s >", m.text.QualityPreset, optionLabel(m.qualityPresets, m.settings.Display.Quality))
	if m.vsyncToggle != nil {
		m.vsyncToggle.Label = m.text.VSync
		m.vsyncToggle.Checked = m.settings.Display.VSync
	}

	capLabel := m.text.Uncapped
	if m.settings.Display.FrameCap > 0 {
		capLabel = fmt.Sprintf("%d FPS", m.settings.Display.FrameCap)
	}
	m.frameCapBtn.Text = fmt.Sprintf("%s: < %s >", m.text.FrameCap, capLabel)

	// Controls
	m.controlsHintLabel.Text = m.text.ControlsHint
	device := input.DeviceUnknown
	if m.deviceTracker != nil {
		device = m.deviceTracker.LastDeviceUntracked()
	}
	for i, act := range m.actions {
		if m.capturingAction == act.Name {
			m.actionButtons[i].Text = fmt.Sprintf("%s: [%s]", act.Label, m.text.CapturePrompt)
			continue
		}
		tokens := m.settings.Bindings[act.Name]
		if len(tokens) == 0 {
			m.actionButtons[i].Text = fmt.Sprintf("%s: %s", act.Label, m.text.Unbound)
			continue
		}
		labels := make([]string, 0, len(tokens))
		for _, tok := range tokens {
			if lbl := FormatBindingLabelForDevice(m.scope, tok, device); lbl != "" {
				labels = append(labels, lbl)
			}
		}
		if len(labels) == 0 {
			m.actionButtons[i].Text = fmt.Sprintf("%s: %s", act.Label, m.text.Unbound)
		} else {
			m.actionButtons[i].Text = fmt.Sprintf("%s: %s", act.Label, strings.Join(labels, " / "))
		}
	}
	for _, item := range m.actionToggleChecks {
		item.toggle.Label = fmt.Sprintf("%s: %s", item.label, m.text.ActionToggle)
		item.toggle.Checked = slices.Contains(m.settings.InputAssists.Toggle, item.action)
	}
	for _, item := range m.actionMashChecks {
		item.toggle.Label = fmt.Sprintf("%s: %s", item.label, m.text.ActionMash)
		item.toggle.Checked = slices.Contains(m.settings.InputAssists.MashToHold, item.action)
	}
	if m.minPressSlider != nil {
		if m.settings.InputAssists.MinPressMs <= 0 {
			m.minPressSlider.Label = fmt.Sprintf("%s: %s", m.text.MinPressTime, m.text.AssistOff)
		} else {
			m.minPressSlider.Label = fmt.Sprintf("%s: %d ms", m.text.MinPressTime, m.settings.InputAssists.MinPressMs)
		}
		m.minPressSlider.Value = float32(m.settings.InputAssists.MinPressMs)
	}
	if m.acceptDelaySlider != nil {
		if m.settings.InputAssists.AcceptDelayMs <= 0 {
			m.acceptDelaySlider.Label = fmt.Sprintf("%s: %s", m.text.AcceptDelay, m.text.AssistOff)
		} else {
			m.acceptDelaySlider.Label = fmt.Sprintf("%s: %d ms", m.text.AcceptDelay, m.settings.InputAssists.AcceptDelayMs)
		}
		m.acceptDelaySlider.Value = float32(m.settings.InputAssists.AcceptDelayMs)
	}
	m.resetControlsBtn.Text = m.text.ResetControls

	// Language
	m.languageHintLabel.Text = m.text.LanguageHint
	for i, loc := range m.locales {
		marker := "[ ]"
		if strings.EqualFold(strings.TrimSpace(m.settings.Locale), strings.TrimSpace(loc.Tag)) {
			marker = "[x]"
		}
		lbl := loc.Label
		if loc.Tag == "" && m.text.SystemDefault != "" {
			lbl = m.text.SystemDefault
		}
		m.localeButtons[i].Text = fmt.Sprintf("%s %s", marker, lbl)
	}

	// Accessibility
	effScale := m.settings.Accessibility.EffectiveCaptionScale(m.env)
	m.captionsToggle.Label = m.text.Subtitles
	m.captionsToggle.Checked = m.settings.Accessibility.Captions
	m.sfxCaptionsToggle.Label = m.text.SFXCaptions
	m.sfxCaptionsToggle.Checked = m.settings.Accessibility.SFXCaptions
	m.textScaleSlider.Label = fmt.Sprintf("%s: %d%%", m.text.TextScale, pctInt(effScale))
	m.textScaleSlider.Value = float32(effScale)
	m.highContrastToggle.Label = m.text.HighContrast
	m.highContrastToggle.Checked = m.settings.Accessibility.EffectiveHighContrast(m.env)
	m.reduceMotionToggle.Label = m.text.ReduceMotion
	m.reduceMotionToggle.Checked = m.settings.Accessibility.EffectiveReduceMotion(m.env)
	m.screenShakeSlider.Label = fmt.Sprintf("%s: %d%%", m.text.ScreenShake, pctInt(m.settings.Accessibility.ScreenShake))
	m.screenShakeSlider.Value = float32(m.settings.Accessibility.ScreenShake)
	m.reduceFlashesToggle.Label = m.text.ReduceFlashes
	m.reduceFlashesToggle.Checked = m.settings.Accessibility.ReduceFlashes
	m.colorBlindBtn.Text = fmt.Sprintf("%s: < %s >", m.text.ColorBlind, colorBlindOptionLabel(m.colorBlindModes, m.settings.Accessibility.ColorBlindMode))
	m.gameSpeedSlider.Label = fmt.Sprintf("%s: %d%%", m.text.GameSpeed, pctInt(m.settings.Accessibility.GameSpeed))
	m.gameSpeedSlider.Value = float32(m.settings.Accessibility.GameSpeed)
	m.invincibilityToggle.Label = m.text.Invincibility
	m.invincibilityToggle.Checked = m.settings.Accessibility.Invincibility
	m.autoAimToggle.Label = m.text.AutoAim
	m.autoAimToggle.Checked = m.settings.Accessibility.AutoAim
	m.holdToToggleToggle.Label = m.text.HoldToToggle
	m.holdToToggleToggle.Checked = m.settings.Accessibility.HoldToToggle

	// Footer
	m.applyBtn.Text = m.text.Apply
	m.saveBtn.Text = m.text.Save
	m.resetBtn.Text = m.text.Reset
	m.backBtn.Text = m.text.Back
}

func (m *SettingsMenu) onSettingMutated() {
	m.settings.Validate()
	m.dirty = true
	m.statusLabel.Text = ""
	m.applyTheme()
	m.syncWidgets()
	if m.onChange != nil {
		m.onChange(m.Settings())
	}
	if m.autoApply {
		_ = m.Apply()
	}
	if m.autoSave {
		_ = m.Save()
	}
}

// Root returns the menu's underlying gameui Root.
func (m *SettingsMenu) Root() *Root { return m.root }

// Navigator returns the menu's SpatialNavigator.
func (m *SettingsMenu) Navigator() *SpatialNavigator { return m.nav }

// Tab returns the currently active SettingsTab.
func (m *SettingsMenu) Tab() SettingsTab { return m.activeTab }

// SetTab switches the active settings tab, cancelling any in-progress control capture
// and updating focus and directional navigation cleanly.
func (m *SettingsMenu) SetTab(tab SettingsTab) {
	if int(tab) >= settingsTabCount {
		tab = SettingsTabAudio
	}
	wasOnTabBar := false
	curFocus := m.root.Focused()
	for _, tb := range m.tabButtons {
		if curFocus == tb {
			wasOnTabBar = true
			break
		}
	}
	m.capturingAction = ""
	m.activeTab = tab
	for i := range settingsTabCount {
		m.tabPanels[i].Hidden = SettingsTab(i) != tab
	}
	m.syncEnvFullScreen()
	m.syncWidgets()
	m.wireNav()
	if b := m.root.Bounds(); b.Size.Width > 0 && b.Size.Height > 0 {
		m.root.Layout(b)
	}
	if wasOnTabBar {
		m.nav.SetFocus(m.tabButtons[tab])
	} else if len(m.tabControls[tab]) > 0 {
		m.nav.SetFocus(m.tabControls[tab][0])
	} else {
		m.nav.SetFocus(m.tabButtons[tab])
	}
}

// NextTab switches to the next settings tab, wrapping from Accessibility back to Audio.
func (m *SettingsMenu) NextTab() {
	m.SetTab(SettingsTab((int(m.activeTab) + 1) % settingsTabCount))
}

// PrevTab switches to the previous settings tab, wrapping from Audio to Accessibility.
func (m *SettingsMenu) PrevTab() {
	m.SetTab(SettingsTab((int(m.activeTab) - 1 + settingsTabCount) % settingsTabCount))
}

// TabButton returns the tab-bar Button for tab.
func (m *SettingsMenu) TabButton(tab SettingsTab) *Button {
	if int(tab) >= settingsTabCount {
		return nil
	}
	return m.tabButtons[tab]
}

// TabControls returns the ordered focusable widgets inside tab.
func (m *SettingsMenu) TabControls(tab SettingsTab) []Widget {
	if int(tab) >= settingsTabCount {
		return nil
	}
	return slices.Clone(m.tabControls[tab])
}

// FooterButtons returns the Apply, Save, Reset, and Back buttons in order.
func (m *SettingsMenu) FooterButtons() (apply, saveBtn, reset, back *Button) {
	return m.applyBtn, m.saveBtn, m.resetBtn, m.backBtn
}

// syncEnvFullScreen reflects transitions of Env.FullScreen into the working Display.Fullscreen
// without clobbering unapplied edits while Env.FullScreen has not changed, and refreshes
// platform-following Accessibility controls when OS accessibility settings change.
func (m *SettingsMenu) syncEnvFullScreen() {
	if m.env == nil {
		return
	}
	widgetsDirty := false
	themeDirty := false
	if m.canFullScreen {
		cur := m.env.FullScreen()
		if cur != m.lastEnvFull {
			m.lastEnvFull = cur
			if m.settings.Display.Fullscreen != cur {
				m.settings.Display.Fullscreen = cur
				widgetsDirty = true
			}
		}
	}
	if rm := m.env.ReduceMotion(); rm != m.lastEnvReduceMotion {
		m.lastEnvReduceMotion = rm
		widgetsDirty = true
	}
	if hc := m.env.IncreaseContrast(); hc != m.lastEnvHighContrast {
		m.lastEnvHighContrast = hc
		themeDirty = true
		widgetsDirty = true
	}
	if fs := m.env.FontScale(); fs != m.lastEnvFontScale {
		m.lastEnvFontScale = fs
		themeDirty = true
		widgetsDirty = true
	}
	if themeDirty {
		m.applyTheme()
	}
	if widgetsDirty {
		m.syncWidgets()
	}
}

// Settings returns a deep copy of the menu's current working player settings.
func (m *SettingsMenu) Settings() save.Settings {
	m.syncEnvFullScreen()
	return cloneSaveSettings(m.settings)
}

// SetSettings replaces the menu's working settings and refreshes all controls.
func (m *SettingsMenu) SetSettings(s save.Settings) {
	cp := cloneSaveSettings(s)
	cp.Validate()
	ensureAudioGroups(&cp, m.audioBuses)
	ensureActionBindings(&cp, m.actions)
	if SettingsResolutionFrom(cp) == "" && len(m.resolutions) > 0 {
		SetSettingsResolution(&cp, m.resolutions[0].ID)
	}
	m.settings = cp
	if m.env != nil {
		m.lastEnvFull = m.env.FullScreen()
		m.lastEnvReduceMotion = m.env.ReduceMotion()
		m.lastEnvHighContrast = m.env.IncreaseContrast()
		m.lastEnvFontScale = m.env.FontScale()
	}
	m.capturingAction = ""
	m.applyTheme()
	m.syncWidgets()
}

// SetText updates the menu's user-visible strings (for example after changing locale).
func (m *SettingsMenu) SetText(text SettingsText) {
	m.text = resolveSettingsText(text)
	m.syncWidgets()
}

// Dirty reports whether the working settings have unsaved changes.
func (m *SettingsMenu) Dirty() bool { return m.dirty }

// Err returns the last error produced by Load, Apply, or Save.
func (m *SettingsMenu) Err() error { return m.lastErr }

// CapturingAction returns the action name currently awaiting a key or gamepad button press, or "".
func (m *SettingsMenu) CapturingAction() string { return m.capturingAction }

// BeginCapture enters control-rebinding capture mode for actionName.
func (m *SettingsMenu) BeginCapture(actionName string) bool {
	found := false
	for _, act := range m.actions {
		if act.Name == actionName {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	m.capturingAction = actionName
	m.statusLabel.Text = m.text.CapturePrompt
	m.syncWidgets()
	return true
}

// CancelCapture exits control-rebinding capture mode without modifying bindings.
func (m *SettingsMenu) CancelCapture() {
	if m.capturingAction == "" {
		return
	}
	m.capturingAction = ""
	m.statusLabel.Text = ""
	m.syncWidgets()
}

// CaptureBinding assigns token to the currently capturing action, replacing the existing binding of the same input device class (keyboard vs gamepad vs mouse) while keeping other classes intact. Conflicts with actions in the same SettingsAction.Group are resolved with SettingsMenuConfig.OnConflict.
func (m *SettingsMenu) CaptureBinding(token string) bool {
	if m.capturingAction == "" {
		return false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if _, ok := ParseBindingToken(token); !ok {
		return false
	}
	action := m.capturingAction
	conflicts := m.conflictingActions(action, token)
	if len(conflicts) > 0 && m.onConflict == ConflictRefuse {
		m.statusLabel.Text = fmt.Sprintf(m.text.ConflictStatus, conflicts[0].Label)
		m.syncWidgets()
		return false
	}

	replaced := replacedBindingForClass(m.settings.Bindings[action], token)
	m.capturingAction = ""
	if m.settings.Bindings == nil {
		m.settings.Bindings = make(map[string][]string)
	}
	m.settings.Bindings[action] = replaceBindingForClass(m.settings.Bindings[action], token)
	for _, other := range conflicts {
		replacement := ""
		if m.onConflict != ConflictClear {
			replacement = replaced
		}
		m.settings.Bindings[other.Name] = replaceConflictingBinding(m.settings.Bindings[other.Name], token, replacement)
	}
	m.onSettingMutated()
	if len(conflicts) > 0 {
		m.statusLabel.Text = fmt.Sprintf(m.text.ConflictStatus, conflicts[0].Label)
	}
	return true
}

func (m *SettingsMenu) conflictingActions(action, token string) []SettingsAction {
	var group string
	for _, act := range m.actions {
		if act.Name == action {
			group = act.Group
			break
		}
	}
	var conflicts []SettingsAction
	for _, act := range m.actions {
		if act.Name == action || act.Group != group {
			continue
		}
		for _, existing := range m.settings.Bindings[act.Name] {
			if bindingTokensEqual(existing, token) {
				conflicts = append(conflicts, act)
				break
			}
		}
	}
	return conflicts
}

// ResetControls restores all configured actions' bindings and motor assists to their default values.
func (m *SettingsMenu) ResetControls() {
	m.capturingAction = ""
	if m.settings.Bindings == nil {
		m.settings.Bindings = make(map[string][]string, len(m.actions))
	}
	for _, act := range m.actions {
		if len(act.Defaults) > 0 {
			m.settings.Bindings[act.Name] = slices.Clone(act.Defaults)
		} else if len(m.defaults.Bindings[act.Name]) > 0 {
			m.settings.Bindings[act.Name] = slices.Clone(m.defaults.Bindings[act.Name])
		} else {
			delete(m.settings.Bindings, act.Name)
		}
	}
	m.settings.InputAssists = save.InputAssistSettings{
		Toggle:        slices.Clone(m.defaults.InputAssists.Toggle),
		MashToHold:    slices.Clone(m.defaults.InputAssists.MashToHold),
		MinPressMs:    m.defaults.InputAssists.MinPressMs,
		AcceptDelayMs: m.defaults.InputAssists.AcceptDelayMs,
	}
	m.onSettingMutated()
	m.statusLabel.Text = m.text.ResetStatus
}

// Apply validates the working settings and dispatches them to Targets, Video (for resolution), and OnApply.
func (m *SettingsMenu) Apply() error {
	m.settings.Validate()
	snap := m.Settings()
	var errs []error
	if err := save.Apply(snap, m.targets); err != nil {
		errs = append(errs, err)
	}
	if m.video != nil {
		ApplySettingsResolution(m.video, snap, m.resolutions...)
	}
	if m.onApply != nil {
		if err := m.onApply(snap); err != nil {
			errs = append(errs, err)
		}
	}
	m.lastErr = errors.Join(errs...)
	if m.lastErr == nil {
		m.statusLabel.Text = m.text.AppliedStatus
	} else {
		m.statusLabel.Text = m.lastErr.Error()
	}
	return m.lastErr
}

// Save validates the working settings, persists them through Store (when configured),
// applies them to Targets/Video/OnApply, and invokes OnSave.
func (m *SettingsMenu) Save() error {
	m.settings.Validate()
	snap := m.Settings()
	var errs []error
	if m.store != nil {
		if err := m.store.Save(snap); err != nil {
			errs = append(errs, err)
		}
	}
	if err := save.Apply(snap, m.targets); err != nil {
		errs = append(errs, err)
	}
	if m.video != nil {
		ApplySettingsResolution(m.video, snap, m.resolutions...)
	}
	if m.onApply != nil {
		if err := m.onApply(snap); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 && m.onSave != nil {
		if err := m.onSave(snap); err != nil {
			errs = append(errs, err)
		}
	}
	m.lastErr = errors.Join(errs...)
	if m.lastErr == nil {
		m.savedSnap = cloneSaveSettings(snap)
		m.dirty = false
		m.statusLabel.Text = m.text.SavedStatus
	} else {
		m.statusLabel.Text = m.lastErr.Error()
	}
	return m.lastErr
}

// Reset restores all settings to their configured defaults and refreshes the UI.
func (m *SettingsMenu) Reset() {
	m.capturingAction = ""
	m.settings = cloneSaveSettings(m.defaults)
	m.settings.Validate()
	ensureAudioGroups(&m.settings, m.audioBuses)
	ensureActionBindings(&m.settings, m.actions)
	m.dirty = true
	m.applyTheme()
	m.syncWidgets()
	m.statusLabel.Text = m.text.ResetStatus
	if m.onReset != nil {
		m.onReset(m.Settings())
	}
	if m.onChange != nil {
		m.onChange(m.Settings())
	}
	if m.autoApply {
		_ = m.Apply()
	}
	if m.autoSave {
		_ = m.Save()
	}
}

// Back cancels capture if active; otherwise invokes OnBack.
func (m *SettingsMenu) Back() {
	if m.capturingAction != "" {
		m.CancelCapture()
		return
	}
	if m.onBack != nil {
		m.onBack()
	}
}

// Layout lays out the menu inside bounds.
func (m *SettingsMenu) Layout(bounds core.Bounds) {
	m.syncEnvFullScreen()
	m.root.Layout(bounds)
}

// Draw lays out and renders the settings menu into d within bounds.
func (m *SettingsMenu) Draw(d *core.Drawing, bounds core.Bounds) {
	m.syncEnvFullScreen()
	m.root.Draw(d, bounds)
}

// OnPointerMove updates hover state and drags any active slider.
func (m *SettingsMenu) OnPointerMove(p core.Point) bool {
	return m.root.OnPointerMove(p)
}

// OnPointerDown focuses and presses the control under p.
func (m *SettingsMenu) OnPointerDown(p core.Point) bool {
	m.syncEnvFullScreen()
	return m.root.OnPointerDown(p)
}

// OnPointerUp finishes any press or slider drag, activating controls released inside their bounds.
func (m *SettingsMenu) OnPointerUp(p core.Point) bool {
	m.syncEnvFullScreen()
	return m.root.OnPointerUp(p)
}

// OnPress handles a completed core.PressEvent directly.
func (m *SettingsMenu) OnPress(ev core.PressEvent) bool {
	m.syncEnvFullScreen()
	return m.root.OnPress(ev)
}

// OnTouch routes touch start, move, end, and cancel events to sliders, toggles, and buttons.
func (m *SettingsMenu) OnTouch(ev core.TouchEvent) bool {
	m.syncEnvFullScreen()
	switch ev.Phase {
	case core.TouchStart:
		if m.hasActiveTouch || len(ev.Touches) == 0 {
			return false
		}
		picked := ev.Touches[0]
		for _, t := range ev.Touches {
			if t.Changed {
				picked = t
				break
			}
		}
		m.activeTouchID = picked.ID
		m.hasActiveTouch = true
		return m.OnPointerDown(picked.Position)

	case core.TouchMove:
		if !m.hasActiveTouch {
			return false
		}
		for _, t := range ev.Touches {
			if t.ID == m.activeTouchID {
				return m.OnPointerMove(t.Position)
			}
		}
		return false

	case core.TouchEnd:
		if !m.hasActiveTouch {
			return false
		}
		hasChanged := false
		for _, t := range ev.Touches {
			if t.Changed {
				hasChanged = true
				break
			}
		}
		for _, t := range ev.Touches {
			if t.ID == m.activeTouchID && (t.Changed || !hasChanged) {
				m.hasActiveTouch = false
				return m.OnPointerUp(t.Position)
			}
		}
		return false

	case core.TouchCancel:
		if !m.hasActiveTouch {
			return false
		}
		m.hasActiveTouch = false
		m.root.pressed = nil
		m.root.dragging = nil
		m.root.hovered = nil
		return true

	default:
		return false
	}
}

// OnKeyDown handles keyboard navigation, slider/option stepping, and control rebinding capture.
func (m *SettingsMenu) OnKeyDown(ev core.KeyEvent) bool {
	key := strings.ToLower(strings.TrimSpace(ev.Key))
	if m.capturingAction != "" {
		if ev.Repeat {
			return true
		}
		if key == "escape" || key == "esc" {
			m.CancelCapture()
			return true
		}
		if key != "" {
			if key == " " {
				key = "space"
			}
			return m.CaptureBinding("key:" + key)
		}
		if code := strings.TrimSpace(ev.Code); code != "" {
			return m.CaptureBinding("keycode:" + code)
		}
		return true
	}

	switch key {
	case "escape", "esc":
		return m.HandleAction(NavBack)
	case "up", "arrowup":
		return m.HandleAction(NavUp)
	case "down", "arrowdown":
		return m.HandleAction(NavDown)
	case "left", "arrowleft":
		return m.HandleAction(NavLeft)
	case "right", "arrowright":
		return m.HandleAction(NavRight)
	case "enter", "return", "space", " ":
		return m.HandleAction(NavConfirm)
	case "[", "pageup":
		m.PrevTab()
		return true
	case "]", "pagedown":
		m.NextTab()
		return true
	default:
		return m.root.OnKeyDown(ev)
	}
}

// HandleAction dispatches a logical NavAction through the menu, supporting slider adjustment,
// toggle switching, cycle-option stepping on NavLeft/NavRight, and capture cancellation on NavBack.
func (m *SettingsMenu) HandleAction(action NavAction) bool {
	m.syncEnvFullScreen()
	if m.capturingAction != "" {
		if action == NavBack {
			m.CancelCapture()
			return true
		}
		return false
	}
	switch action {
	case NavLeft:
		if focused := m.root.Focused(); focused != nil {
			if fn, ok := m.cycleHandlers[focused]; ok {
				fn(-1)
				return true
			}
			if _, ok := focused.(*Toggle); ok && m.nav.Overrides(focused).Left == nil {
				return m.root.Adjust(-1)
			}
		}
		return m.nav.HandleAction(NavLeft)
	case NavRight:
		if focused := m.root.Focused(); focused != nil {
			if fn, ok := m.cycleHandlers[focused]; ok {
				fn(1)
				return true
			}
			if _, ok := focused.(*Toggle); ok && m.nav.Overrides(focused).Right == nil {
				return m.root.Adjust(1)
			}
		}
		return m.nav.HandleAction(NavRight)
	case NavBack:
		m.Back()
		return true
	default:
		return m.nav.HandleAction(action)
	}
}

// StepActions polls logical UI navigation actions (such as from input.Actions.JustPressed)
// and dispatches them in deterministic order.
func (m *SettingsMenu) StepActions(pressed func(NavAction) bool) bool {
	if pressed == nil {
		return false
	}
	order := [6]NavAction{NavUp, NavDown, NavLeft, NavRight, NavConfirm, NavBack}
	handled := false
	for _, act := range order {
		if pressed(act) && m.HandleAction(act) {
			handled = true
		}
	}
	return handled
}

// OnGamepadButton processes a single gamepad button press (rising edge).
// While capturing a control rebind, any valid button is captured; otherwise D-pad,
// South (confirm), East (back), and LB/RB (previous/next tab) drive the menu.
func (m *SettingsMenu) OnGamepadButton(btn gamepad.Button) bool {
	if int(btn) >= gamepad.ButtonCount {
		return false
	}
	if m.capturingAction != "" {
		return m.CaptureBinding(GamepadButtonToken(btn))
	}
	switch btn {
	case gamepad.ButtonDPadUp:
		return m.HandleAction(NavUp)
	case gamepad.ButtonDPadDown:
		return m.HandleAction(NavDown)
	case gamepad.ButtonDPadLeft:
		return m.HandleAction(NavLeft)
	case gamepad.ButtonDPadRight:
		return m.HandleAction(NavRight)
	case gamepad.ButtonSouth:
		return m.HandleAction(NavConfirm)
	case gamepad.ButtonEast:
		return m.HandleAction(NavBack)
	case gamepad.ButtonLB:
		m.PrevTab()
		return true
	case gamepad.ButtonRB:
		m.NextTab()
		return true
	default:
		return false
	}
}

// SyncGamepad records the currently held buttons and left-stick position on pads as the
// baseline without triggering any actions. Call it when opening the menu so buttons held
// to open the screen do not immediately fire inside it.
func (m *SettingsMenu) SyncGamepad(pads []gamepad.Pad) {
	clear(m.prevPadButtons)
	var stickX, stickY float32
	for i := range pads {
		p := &pads[i]
		var mask uint32
		for b := range gamepad.ButtonCount {
			if p.Pressed(gamepad.Button(b)) {
				mask |= 1 << uint(b)
			}
		}
		m.prevPadButtons[p.ID] = mask
		if ax := p.Axes[gamepad.AxisLeftX]; absf(ax) > absf(stickX) {
			stickX = ax
		}
		if ay := p.Axes[gamepad.AxisLeftY]; absf(ay) > absf(stickY) {
			stickY = ay
		}
	}
	m.prevStickX = stickDir(stickX, 0)
	m.prevStickY = stickDir(stickY, 0)
}

// StepGamepad polls connected gamepads once per frame, driving navigation, tab switching,
// value adjustment, and control-rebinding capture on button and left-stick rising edges.
func (m *SettingsMenu) StepGamepad(pads []gamepad.Pad) bool {
	if len(pads) == 0 {
		clear(m.prevPadButtons)
		m.prevStickX = 0
		m.prevStickY = 0
		return false
	}

	sorted := slices.Clone(pads)
	slices.SortStableFunc(sorted, func(a, b gamepad.Pad) int { return cmp.Compare(a.ID, b.ID) })

	// Clean up disconnected pads.
	for id := range m.prevPadButtons {
		stillConnected := false
		for i := range sorted {
			if sorted[i].ID == id {
				stillConnected = true
				break
			}
		}
		if !stillConnected {
			delete(m.prevPadButtons, id)
		}
	}

	var risingMask uint32
	var stickX, stickY float32
	activeDevice := input.DeviceUnknown
	for i := range sorted {
		p := &sorted[i]
		var mask uint32
		for b := range gamepad.ButtonCount {
			if p.Pressed(gamepad.Button(b)) {
				mask |= 1 << uint(b)
			}
		}
		prev := m.prevPadButtons[p.ID]
		rising := mask &^ prev
		risingMask |= rising
		m.prevPadButtons[p.ID] = mask
		if activeDevice == input.DeviceUnknown && (rising != 0 || absf(p.Axes[gamepad.AxisLeftX]) >= 0.5 || absf(p.Axes[gamepad.AxisLeftY]) >= 0.5) {
			activeDevice = gamepadDeviceKind(*p)
		}

		if ax := p.Axes[gamepad.AxisLeftX]; absf(ax) > absf(stickX) {
			stickX = ax
		}
		if ay := p.Axes[gamepad.AxisLeftY]; absf(ay) > absf(stickY) {
			stickY = ay
		}
	}
	if m.deviceTracker != nil && activeDevice != input.DeviceUnknown {
		m.deviceTracker.OnGamepad(activeDevice)
	}

	// Capture mode consumes the first newly pressed button and ignores stick drift.
	if m.capturingAction != "" {
		m.prevStickX = stickDir(stickX, m.prevStickX)
		m.prevStickY = stickDir(stickY, m.prevStickY)
		if risingMask != 0 {
			for b := range gamepad.ButtonCount {
				if risingMask&(1<<uint(b)) != 0 {
					return m.CaptureBinding(GamepadButtonToken(gamepad.Button(b)))
				}
			}
		}
		return false
	}

	handled := false

	// Left-stick rising edges (deadzone 0.5, release hysteresis 0.35).
	nextSX := stickDir(stickX, m.prevStickX)
	nextSY := stickDir(stickY, m.prevStickY)
	if nextSY != m.prevStickY {
		if nextSY < 0 && m.HandleAction(NavUp) {
			handled = true
		} else if nextSY > 0 && m.HandleAction(NavDown) {
			handled = true
		}
	}
	if nextSX != m.prevStickX {
		if nextSX < 0 && m.HandleAction(NavLeft) {
			handled = true
		} else if nextSX > 0 && m.HandleAction(NavRight) {
			handled = true
		}
	}
	m.prevStickX = nextSX
	m.prevStickY = nextSY

	// Process button rising edges in deterministic priority order.
	btnOrder := []gamepad.Button{
		gamepad.ButtonLB,
		gamepad.ButtonRB,
		gamepad.ButtonDPadUp,
		gamepad.ButtonDPadDown,
		gamepad.ButtonDPadLeft,
		gamepad.ButtonDPadRight,
		gamepad.ButtonSouth,
		gamepad.ButtonEast,
	}
	for _, btn := range btnOrder {
		if risingMask&(1<<uint(btn)) != 0 {
			if m.OnGamepadButton(btn) {
				handled = true
				// If ButtonSouth just started rebind capture, stop processing further buttons this frame.
				if m.capturingAction != "" {
					break
				}
			}
		}
	}
	return handled
}

func gamepadDeviceKind(p gamepad.Pad) input.DeviceKind {
	switch p.Family {
	case gamepad.FamilyXbox:
		return input.DeviceGamepadXbox
	case gamepad.FamilyPlayStation:
		return input.DeviceGamepadPlayStation
	case gamepad.FamilyNintendo:
		return input.DeviceGamepadNintendo
	default:
		return input.DetectGamepadKind(p.Name)
	}
}

func stickDir(v float32, prev int) int {
	const activate = 0.5
	const release = 0.35
	if prev > 0 {
		if v >= release {
			return 1
		}
	} else if prev < 0 {
		if v <= -release {
			return -1
		}
	}
	if v >= activate {
		return 1
	}
	if v <= -activate {
		return -1
	}
	return 0
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func optionIndex(items []SettingsOptionItem, value string) int {
	for i, it := range items {
		if strings.EqualFold(strings.TrimSpace(it.Value), strings.TrimSpace(value)) {
			return i
		}
	}
	return 0
}

func optionLabel(items []SettingsOptionItem, value string) string {
	for _, it := range items {
		if strings.EqualFold(strings.TrimSpace(it.Value), strings.TrimSpace(value)) {
			return it.Label
		}
	}
	if len(items) > 0 {
		return items[0].Label
	}
	return value
}

func colorBlindOptionIndex(items []SettingsOptionItem, value string) int {
	norm := save.NormalizeColorBlindMode(value)
	for i, it := range items {
		if save.NormalizeColorBlindMode(it.Value) == norm {
			return i
		}
	}
	return 0
}

func colorBlindOptionLabel(items []SettingsOptionItem, value string) string {
	norm := save.NormalizeColorBlindMode(value)
	for _, it := range items {
		if save.NormalizeColorBlindMode(it.Value) == norm {
			return it.Label
		}
	}
	if len(items) > 0 {
		return items[0].Label
	}
	return value
}

func pctInt(v float64) int {
	return int(math.Round(v * 100))
}

func roundStep(v, step float64) float64 {
	if step <= 0 {
		return v
	}
	return math.Round(v/step) * step
}
