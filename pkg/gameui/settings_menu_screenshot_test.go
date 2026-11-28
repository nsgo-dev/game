package gameui_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/input"
	_ "github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestSettingsMenuControlsConflictAndPlayStationMacOSScreenshots(t *testing.T) {
	var menu *gameui.SettingsMenu
	var redraw *core.Signal[int]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		redraw = core.NewSignal(cx, 0)
		menu = gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
			Scope:         cx,
			DeviceTracker: input.NewDeviceTracker(cx, input.DeviceGamepadPlayStation),
			Actions: []gameui.SettingsAction{
				{Name: "left", Label: "Move Left", Defaults: []string{"key:a", "gamepad:dpad_left"}},
				{Name: "jump", Label: "Jump", Defaults: []string{"key:space", "gamepad:south"}},
				{Name: "pause", Label: "Pause", Defaults: []string{"key:escape", "gamepad:start"}},
			},
		})
		menu.SetTab(gameui.SettingsTabControls)
		return core.Canvas{
			TestID: "settings-controls-screenshot",
			Style:  core.Style{Width: core.Pt(960), Height: core.Pt(540)},
			Draw: func(d *core.Drawing, size core.Size) {
				redraw.Get()
				menu.Draw(d, core.Bounds{Size: size})
			},
		}
	}, nsgotest.Size(960, 540))

	menu.BeginCapture("jump")
	menu.CaptureBinding("key:a")
	redraw.Set(1)
	app.Advance(time.Second / 60)
	app.Settle()
	app.Screenshot("settings-controls-conflict-playstation")
}
