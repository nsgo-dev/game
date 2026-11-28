package gameui

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
)

func TestSettingsMenuTextScaleUsesFullAccessibilityRange(t *testing.T) {
	menu := NewSettingsMenu(SettingsMenuConfig{})
	bounds := core.Bounds{Size: core.Size{Width: 960, Height: 540}}
	settings := save.DefaultSettings()
	settings.Accessibility.SetCaptionScale(1.35)
	menu.SetSettings(settings)
	menu.Layout(bounds)
	baseFont := menu.Root().theme().Font.Size

	settings.Accessibility.SetCaptionScale(3)
	menu.SetSettings(settings)
	menu.Layout(bounds)
	largeFont := menu.Root().theme().Font.Size
	if largeFont <= baseFont {
		t.Fatalf("menu font at 300%% = %v, want larger than 135%% font %v", largeFont, baseFont)
	}
	if got := menu.Settings().Accessibility.EffectiveCaptionScale(nil); got != 3 {
		t.Fatalf("stored text scale = %v, want 3", got)
	}
}
