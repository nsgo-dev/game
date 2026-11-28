package gameui

import (
	"encoding/json"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// FirstLaunchSettingsKey is the settings custom-field used by FirstLaunchSettings
// to record that the first-launch accessibility prompt has been completed.
const FirstLaunchSettingsKey = "first_launch_accessibility_complete"

// FirstLaunchSettingsText contains the copy shown before a game plays its first sound.
// Applications can localize the prompt without replacing its layout.
type FirstLaunchSettingsText struct {
	Title         string
	Body          string
	Captions      string
	TextSize      string
	ReducedMotion string
	MoreOptions   string
	Continue      string
}

// DefaultFirstLaunchSettingsText returns the English first-launch copy.
func DefaultFirstLaunchSettingsText() FirstLaunchSettingsText {
	return FirstLaunchSettingsText{
		Title:         "Accessibility",
		Body:          "Choose accessibility options before the game starts.",
		Captions:      "Captions are available in Settings.",
		TextSize:      "Text size can be changed in Settings.",
		ReducedMotion: "Reduced motion can be changed in Settings.",
		MoreOptions:   "More options",
		Continue:      "Continue",
	}
}

func resolveFirstLaunchText(t FirstLaunchSettingsText) FirstLaunchSettingsText {
	d := DefaultFirstLaunchSettingsText()
	if t.Title == "" {
		t.Title = d.Title
	}
	if t.Body == "" {
		t.Body = d.Body
	}
	if t.Captions == "" {
		t.Captions = d.Captions
	}
	if t.TextSize == "" {
		t.TextSize = d.TextSize
	}
	if t.ReducedMotion == "" {
		t.ReducedMotion = d.ReducedMotion
	}
	if t.MoreOptions == "" {
		t.MoreOptions = d.MoreOptions
	}
	if t.Continue == "" {
		t.Continue = d.Continue
	}
	return t
}

// FirstLaunchSettingsConfig configures the first-launch accessibility prompt.
type FirstLaunchSettingsConfig struct {
	// Store persists the completion marker and the initial accessibility values.
	Store *save.DefaultSettingsStore
	// Initial supplies settings when Store is nil or has no saved value.
	Initial *save.Settings
	// Text localizes the prompt. Empty fields use DefaultFirstLaunchSettingsText.
	Text FirstLaunchSettingsText
	// OnMoreOptions opens the full SettingsMenu's Accessibility tab.
	OnMoreOptions func()
	// OnComplete runs after the marker and settings have been saved.
	OnComplete func(save.Settings) error
}

// FirstLaunchSettings is a small, reusable first-launch gate for templates.
// It enables captions by default, preserves a player's existing text and motion
// preferences, and disappears permanently after Continue succeeds.
type FirstLaunchSettings struct {
	store      *save.DefaultSettingsStore
	settings   save.Settings
	text       FirstLaunchSettingsText
	onMore     func()
	onComplete func(save.Settings) error
	completed  bool
	lastErr    error
}

// NewFirstLaunchSettings creates a prompt from persisted settings or defaults.
func NewFirstLaunchSettings(cfg FirstLaunchSettingsConfig) *FirstLaunchSettings {
	f := &FirstLaunchSettings{
		store:      cfg.Store,
		text:       resolveFirstLaunchText(cfg.Text),
		onMore:     cfg.OnMoreOptions,
		onComplete: cfg.OnComplete,
		settings:   save.DefaultSettings(),
	}
	if cfg.Initial != nil {
		f.settings = *cfg.Initial
	}
	if cfg.Store != nil {
		if loaded, err := cfg.Store.Load(); err == nil {
			f.settings = loaded
		} else if cfg.Store.Exists() {
			f.lastErr = err
		}
	}
	f.settings.Validate()
	f.completed = FirstLaunchComplete(f.settings)
	return f
}

// Required reports whether the prompt should block the first game screen.
func (f *FirstLaunchSettings) Required() bool { return f != nil && !f.completed }

// Settings returns a validated copy of the settings currently held by the prompt.
func (f *FirstLaunchSettings) Settings() save.Settings {
	if f == nil {
		return save.DefaultSettings()
	}
	return f.settings
}

// Err returns the last load or save error.
func (f *FirstLaunchSettings) Err() error {
	if f == nil {
		return nil
	}
	return f.lastErr
}

// Complete enables captions, persists the completion marker, and closes the prompt.
func (f *FirstLaunchSettings) Complete() error {
	if f == nil || f.completed {
		return nil
	}
	settings := f.settings
	settings.Accessibility.Captions = true
	SetFirstLaunchComplete(&settings, true)
	settings.Validate()
	if f.store != nil {
		if err := f.store.Save(settings); err != nil {
			f.lastErr = err
			return err
		}
	}
	if f.onComplete != nil {
		if err := f.onComplete(settings); err != nil {
			f.lastErr = err
			return err
		}
	}
	f.settings = settings
	f.completed = true
	f.lastErr = nil
	return nil
}

// View renders the prompt as a native NSGo overlay. Call it before the first
// sound is played and omit it once Required returns false.
func (f *FirstLaunchSettings) View(_ *core.Scope) core.Node {
	if f == nil || !f.Required() {
		return nil
	}
	return core.View{
		TestID: "first-launch-accessibility",
		Style: core.Style{
			Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0),
			Background: core.RGBA(0x0f172a, 0.98), Padding: core.All(24), Gap: 14,
			JustifyContent: core.JustifyCenter, AlignItems: core.AlignCenter,
		},
		Children: []core.Node{
			core.Text{TestID: "first-launch-title", Content: f.text.Title, Style: core.Style{Color: core.RGB(0xffffff), FontSize: 26, FontWeight: core.FontWeightBold}},
			core.Text{Content: f.text.Body, Style: core.Style{Color: core.RGB(0xe2e8f0), FontSize: 16}, MaxLines: 3},
			core.Text{Content: "• " + f.text.Captions + "\n• " + f.text.TextSize + "\n• " + f.text.ReducedMotion, Style: core.Style{Color: core.RGB(0xcbd5e1), FontSize: 14}, MaxLines: 4},
			base.Button{TestID: "first-launch-more-options", Label: f.text.MoreOptions, OnPress: func(core.PressEvent) {
				if f.onMore != nil {
					f.onMore()
				}
			}, Style: core.Style{Padding: core.Symmetric(10, 16), BorderWidth: 1, BorderColor: core.RGB(0x94a3b8), CornerRadius: 8}, Children: []core.Node{core.Text{Content: f.text.MoreOptions, Style: core.Style{Color: core.RGB(0xffffff), FontSize: 14}}}},
			base.Button{TestID: "first-launch-continue", Label: f.text.Continue, OnPress: func(core.PressEvent) { _ = f.Complete() }, Style: core.Style{Padding: core.Symmetric(12, 20), Background: core.RGB(0x2563eb), CornerRadius: 8}, Children: []core.Node{core.Text{Content: f.text.Continue, Style: core.Style{Color: core.RGB(0xffffff), FontSize: 16, FontWeight: core.FontWeightBold}}}},
		},
	}
}

// FirstLaunchComplete reports whether a settings value contains the completion marker.
func FirstLaunchComplete(settings save.Settings) bool {
	if settings.Custom == nil {
		return false
	}
	var complete bool
	if err := json.Unmarshal(settings.Custom[FirstLaunchSettingsKey], &complete); err != nil {
		return false
	}
	return complete
}

// SetFirstLaunchComplete updates the completion marker while preserving other custom settings.
func SetFirstLaunchComplete(settings *save.Settings, complete bool) {
	if settings == nil {
		return
	}
	if settings.Custom == nil {
		settings.Custom = make(map[string]json.RawMessage)
	}
	encoded, err := json.Marshal(complete)
	if err != nil {
		return
	}
	settings.Custom[FirstLaunchSettingsKey] = encoded
}
