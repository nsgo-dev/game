package gameui

import (
	"encoding/json"
	"testing"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestFirstLaunchSettingsPersistsCompletionAndCaptions(t *testing.T) {
	store := save.NewFileSettingsStore(t.TempDir())
	prompt := NewFirstLaunchSettings(FirstLaunchSettingsConfig{Store: store})
	if !prompt.Required() {
		t.Fatal("new settings should require the first-launch prompt")
	}
	if err := prompt.Complete(); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if prompt.Required() || !prompt.Settings().Accessibility.Captions {
		t.Fatalf("prompt state after complete = required %v, settings %+v", prompt.Required(), prompt.Settings())
	}

	reloaded := NewFirstLaunchSettings(FirstLaunchSettingsConfig{Store: store})
	if reloaded.Required() {
		t.Fatal("completion marker was not persisted")
	}
	if !reloaded.Settings().Accessibility.Captions {
		t.Fatal("captions were not enabled by the first-launch prompt")
	}
}

func TestFirstLaunchSettingsPreservesCustomValues(t *testing.T) {
	settings := save.DefaultSettings()
	settings.Custom = map[string]json.RawMessage{"resolution": json.RawMessage(`"1280x720"`)}
	SetFirstLaunchComplete(&settings, true)
	if !FirstLaunchComplete(settings) {
		t.Fatal("completion marker was not recognized")
	}
	if got := string(settings.Custom["resolution"]); got != `"1280x720"` {
		t.Fatalf("custom resolution changed to %s", got)
	}
}

func TestFirstLaunchSettingsViewHasSingleContinueAction(t *testing.T) {
	prompt := NewFirstLaunchSettings(FirstLaunchSettingsConfig{})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return prompt.View(cx) })
	app.Find("id:first-launch-accessibility")
	app.Find("id:first-launch-continue")
	if app.Exists("id:first-launch-more-options") {
		t.Fatal("More options is shown without OnMoreOptions")
	}
	app.Press("id:first-launch-continue")
	if prompt.Required() {
		t.Fatal("continue did not dismiss the prompt")
	}
	if app.Exists("id:first-launch-accessibility") {
		t.Fatal("the overlay is still mounted after Continue")
	}
}

func TestFirstLaunchSettingsMoreOptionsCallsCallback(t *testing.T) {
	opened := 0
	prompt := NewFirstLaunchSettings(FirstLaunchSettingsConfig{OnMoreOptions: func() { opened++ }})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return prompt.View(cx) })
	app.Press("id:first-launch-more-options")
	if opened != 1 {
		t.Fatalf("OnMoreOptions calls = %d, want 1", opened)
	}
	if !prompt.Required() {
		t.Fatal("More options completed the prompt")
	}
}

func TestFirstLaunchSettingsViewHiddenWhenAlreadyComplete(t *testing.T) {
	settings := save.DefaultSettings()
	SetFirstLaunchComplete(&settings, true)
	prompt := NewFirstLaunchSettings(FirstLaunchSettingsConfig{Initial: &settings})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{prompt.View(cx)}}
	})
	if app.Exists("id:first-launch-accessibility") {
		t.Fatal("a completed prompt rendered its overlay")
	}
}
