package gameui_test

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/postfx"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func makePad(id int, pressed []gamepad.Button, lx, ly float32) gamepad.Pad {
	p := gamepad.Pad{ID: id}
	for _, b := range pressed {
		p.Buttons[b] = 1
	}
	p.Axes[gamepad.AxisLeftX] = lx
	p.Axes[gamepad.AxisLeftY] = ly
	return p
}

func pressGamepadButton(m *gameui.SettingsMenu, btn gamepad.Button) {
	m.StepGamepad([]gamepad.Pad{makePad(1, []gamepad.Button{btn}, 0, 0)})
	m.StepGamepad([]gamepad.Pad{makePad(1, nil, 0, 0)})
}

func pushLeftStick(m *gameui.SettingsMenu, lx, ly float32) {
	m.StepGamepad([]gamepad.Pad{makePad(1, nil, lx, ly)})
	m.StepGamepad([]gamepad.Pad{makePad(1, nil, 0, 0)})
}

func TestSettingsMenuLayoutNoOverlapAllTabsAndDraw(t *testing.T) {
	menu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{})
	bounds := core.Bounds{Size: core.Size{Width: 960, Height: 540}}

	tabs := []gameui.SettingsTab{
		gameui.SettingsTabAudio,
		gameui.SettingsTabVideo,
		gameui.SettingsTabControls,
		gameui.SettingsTabLanguage,
		gameui.SettingsTabAccessibility,
	}

	for _, tab := range tabs {
		menu.SetTab(tab)
		menu.Layout(bounds)

		// Verify tab buttons, active controls, and footer buttons all have positive bounds and do not overlap.
		var items []gameui.Widget
		for _, tb := range tabs {
			items = append(items, menu.TabButton(tb))
		}
		items = append(items, menu.TabControls(tab)...)
		aBtn, sBtn, rBtn, bBtn := menu.FooterButtons()
		items = append(items, aBtn, sBtn, rBtn, bBtn)

		for i, w := range items {
			b := w.Bounds()
			if b.Size.Width <= 0 || b.Size.Height <= 0 {
				t.Fatalf("tab %s widget %d has non-positive bounds: %+v", tab, i, b)
			}
			if !boundsInside(b, bounds) {
				t.Fatalf("tab %s widget %d bounds %+v lie outside menu bounds %+v", tab, i, b, bounds)
			}
			for j := i + 1; j < len(items); j++ {
				if boundsOverlap(b, items[j].Bounds()) {
					t.Fatalf("tab %s widgets %d (%+v) and %d (%+v) overlap", tab, i, b, j, items[j].Bounds())
				}
			}
		}

		// Draw inside a real canvas and verify draw ops are recorded cleanly.
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return core.Canvas{
				TestID: "settings-canvas",
				Style:  core.Style{Width: core.Pt(960), Height: core.Pt(540)},
				Draw: func(d *core.Drawing, sz core.Size) {
					menu.Draw(d, core.Bounds{Size: sz})
				},
			}
		}, nsgotest.Size(960, 540))
		ops := app.Find("id:settings-canvas").Ops
		if len(ops) == 0 {
			t.Fatalf("tab %s produced zero draw ops", tab)
		}
	}
}

func TestSettingsMenuGamepadAloneChangesAndSavesEverySetting(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileSettingsStore(dir)

	var appliedDisplay save.DisplaySettings
	var appliedLocale string
	var appliedA11y save.AccessibilitySettings
	var appliedBindings map[string][]string
	var appliedResolution gameui.SettingsResolution
	var saveCount, backCount int

	var mixer *sound.Mixer
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mixer = sound.NewMixer(cx, "test-settings-mixer")
		return core.View{}
	}, nsgotest.Environment(core.Environment{Audio: true}))

	menu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
		Store: store,
		Targets: save.SettingsTargets{
			Sound: mixer,
			ApplyDisplay: func(d save.DisplaySettings) error {
				appliedDisplay = d
				return nil
			},
			ApplyLocale: func(l string) error {
				appliedLocale = l
				return nil
			},
			ApplyAccessibility: func(a save.AccessibilitySettings) error {
				appliedA11y = a
				return nil
			},
			ApplyBindings: func(b map[string][]string) error {
				appliedBindings = b
				return nil
			},
		},
		OnResolutionChange: func(res gameui.SettingsResolution) {
			appliedResolution = res
		},
		OnSave: func(save.Settings) error {
			saveCount++
			return nil
		},
		OnBack: func() {
			backCount++
		},
	})
	menu.Layout(core.Bounds{Size: core.Size{Width: 960, Height: 540}})

	// --- 1. AUDIO TAB (default tab) ---
	// Controls: [0] Mute, [1] Master, [2] Music, [3] SFX, [4] UI, [5] Mono Audio, [6] Balance
	// Initial focus is Mute toggle. Toggle Mute on -> off (confirm twice) -> leave unmuted so mixer gets master volume.
	pressGamepadButton(menu, gamepad.ButtonSouth) // Mute = true
	if !menu.Settings().Audio.Muted {
		t.Fatal("expected Audio.Muted = true after South on Mute toggle")
	}
	pressGamepadButton(menu, gamepad.ButtonDPadLeft) // Mute = false via Left on toggle
	if menu.Settings().Audio.Muted {
		t.Fatal("expected Audio.Muted = false after Left on Mute toggle")
	}

	// Move down to Master slider and step down 4 times (1.0 -> 0.80)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	for range 4 {
		pressGamepadButton(menu, gamepad.ButtonDPadLeft)
	}
	// Move down to Music slider and step down 6 times (1.0 -> 0.70)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	for range 6 {
		pressGamepadButton(menu, gamepad.ButtonDPadLeft)
	}
	// Move down to SFX slider and step down 8 times (1.0 -> 0.60)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	for range 8 {
		pressGamepadButton(menu, gamepad.ButtonDPadLeft)
	}
	// Move down to UI slider and step down 10 times (1.0 -> 0.50)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	for range 10 {
		pressGamepadButton(menu, gamepad.ButtonDPadLeft)
	}
	// Move down to Mono Audio button and step right once (System Default -> On)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	// Move down to Balance slider and step right 3 times (0 -> +0.30)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	for range 3 {
		pressGamepadButton(menu, gamepad.ButtonDPadRight)
	}

	// --- 2. VIDEO TAB (switch with RB) ---
	// Default controls (VSync hidden by default): [0] WindowMode, [1] Resolution, [2] ScaleMode, [3] IntegerScale, [4] Quality, [5] FrameCap
	pressGamepadButton(menu, gamepad.ButtonRB)
	if menu.Tab() != gameui.SettingsTabVideo {
		t.Fatalf("tab after RB = %v, want video", menu.Tab())
	}
	if got := len(menu.TabControls(gameui.SettingsTabVideo)); got != 6 {
		t.Fatalf("default Video tab control count = %d, want 6 (VSync hidden by default)", got)
	}
	// WindowMode -> Fullscreen (via Right)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	// Resolution -> 960x540 -> 1280x720 (via Right twice)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	// ScaleMode -> fill (via Right)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	// IntegerScale -> true (via South)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonSouth)
	// Quality -> low -> medium (via Right twice)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	// FrameCap -> 30 -> 60 (via Right twice; VSync is absent by default)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)

	// --- 3. CONTROLS TAB (switch with RB) ---
	// Default actions: [0] left, [1] right, [2] jump, [3] confirm, [4] pause, [5] ResetControls
	pressGamepadButton(menu, gamepad.ButtonRB)
	if menu.Tab() != gameui.SettingsTabControls {
		t.Fatalf("tab after RB = %v, want controls", menu.Tab())
	}
	// Navigate down twice to "jump" using left stick!
	pushLeftStick(menu, 0, 1)
	pushLeftStick(menu, 0, 1)
	// Press South to enter rebinding capture for "jump"
	pressGamepadButton(menu, gamepad.ButtonSouth)
	if got := menu.CapturingAction(); got != "jump" {
		t.Fatalf("CapturingAction() = %q, want %q", got, "jump")
	}
	// Press West (X) on gamepad to bind jump -> gamepad:west (keeping key:space!)
	pressGamepadButton(menu, gamepad.ButtonWest)
	if menu.CapturingAction() != "" {
		t.Fatalf("expected capture to finish, still capturing %q", menu.CapturingAction())
	}

	// --- 4. LANGUAGE TAB (switch with RB) ---
	// Default locales: [0] "", [1] "en", [2] "zh-Hans", [3] "ja", [4] "es"
	pressGamepadButton(menu, gamepad.ButtonRB)
	if menu.Tab() != gameui.SettingsTabLanguage {
		t.Fatalf("tab after RB = %v, want language", menu.Tab())
	}
	// Down twice to "zh-Hans" and confirm
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonSouth)

	// --- 5. ACCESSIBILITY TAB (switch with RB) ---
	// Controls: [0] Captions, [1] SFXCaptions, [2] TextScale, [3] HighContrast, [4] ReduceMotion, [5] ScreenShake, [6] ReduceFlashes, [7] ColorBlind, [8] ColorBlindSeverity, [9] GameSpeed, [10] Invincibility, [11] AutoAim, [12] HoldToToggle
	pressGamepadButton(menu, gamepad.ButtonRB)
	if menu.Tab() != gameui.SettingsTabAccessibility {
		t.Fatalf("tab after RB = %v, want accessibility", menu.Tab())
	}
	// Captions -> true
	pressGamepadButton(menu, gamepad.ButtonSouth)
	// SFXCaptions -> true
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonSouth)
	// TextScale -> 1.5 (two steps up from 1.0 by 0.25)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	// HighContrast -> true
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonSouth)
	// ReduceMotion -> true
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonSouth)
	// ScreenShake -> 0.5 (5 steps left from 1.0 by 0.1)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	for range 5 {
		pressGamepadButton(menu, gamepad.ButtonDPadLeft)
	}
	// ReduceFlashes -> true
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonSouth)
	// ColorBlind -> protanopia -> deuteranopia (Right twice)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	// ColorBlindSeverity -> 0.8 (Left twice from 1.0 by 0.1)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadLeft)
	pressGamepadButton(menu, gamepad.ButtonDPadLeft)
	// GameSpeed -> 0.75 (Left once from 1.0 by 0.25)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadLeft)
	// Invincibility -> true
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonSouth)
	// AutoAim -> true
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonSouth)
	// HoldToToggle -> true
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonSouth)

	// --- SAVE VIA FOOTER USING GAMEPAD ALONE ---
	// From last control (HoldToToggle), Down moves to footer Apply, Right moves to Save, South saves!
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadRight)
	pressGamepadButton(menu, gamepad.ButtonSouth)

	if menu.Err() != nil {
		t.Fatalf("Save error: %v", menu.Err())
	}
	if saveCount != 1 {
		t.Fatalf("saveCount = %d, want 1", saveCount)
	}
	if menu.Dirty() {
		t.Fatal("expected Dirty() == false after Save")
	}

	// Verify persisted settings reload identically from save.DefaultSettingsStore!
	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if math.Abs(reloaded.Audio.Master-0.80) > 1e-4 {
		t.Fatalf("Audio.Master = %v, want 0.80", reloaded.Audio.Master)
	}
	if math.Abs(reloaded.Audio.Groups["music"]-0.70) > 1e-4 {
		t.Fatalf("Audio.Groups[music] = %v, want 0.70", reloaded.Audio.Groups["music"])
	}
	if math.Abs(reloaded.Audio.Groups["sfx"]-0.60) > 1e-4 {
		t.Fatalf("Audio.Groups[sfx] = %v, want 0.60", reloaded.Audio.Groups["sfx"])
	}
	if math.Abs(reloaded.Audio.Groups["ui"]-0.50) > 1e-4 {
		t.Fatalf("Audio.Groups[ui] = %v, want 0.50", reloaded.Audio.Groups["ui"])
	}
	if reloaded.Audio.Mono == nil || !*reloaded.Audio.Mono {
		t.Fatalf("Audio.Mono = %v, want true", reloaded.Audio.Mono)
	}
	if math.Abs(reloaded.Audio.Balance-0.30) > 1e-4 {
		t.Fatalf("Audio.Balance = %v, want 0.30", reloaded.Audio.Balance)
	}
	if !reloaded.Display.Fullscreen || gameui.SettingsResolutionFrom(reloaded) != "1280x720" ||
		reloaded.Display.ScaleMode != "fill" || !reloaded.Display.IntegerScale ||
		reloaded.Display.Quality != "medium" || !reloaded.Display.VSync || reloaded.Display.FrameCap != 60 {
		t.Fatalf("reloaded Display = %+v, resolution = %q", reloaded.Display, gameui.SettingsResolutionFrom(reloaded))
	}
	if appliedResolution.ID != "1280x720" {
		t.Fatalf("OnResolutionChange = %+v, want 1280x720", appliedResolution)
	}
	if !slices.Equal(reloaded.Bindings["jump"], []string{"key:space", "gamepad:west"}) {
		t.Fatalf("Bindings[jump] = %v, want [key:space gamepad:west]", reloaded.Bindings["jump"])
	}
	if reloaded.Locale != "zh-Hans" {
		t.Fatalf("Locale = %q, want zh-Hans", reloaded.Locale)
	}
	if !reloaded.Accessibility.Captions ||
		!reloaded.Accessibility.SFXCaptions ||
		math.Abs(reloaded.Accessibility.CaptionScale-1.5) > 1e-4 ||
		!reloaded.Accessibility.HighContrast ||
		!reloaded.Accessibility.ReduceMotion ||
		math.Abs(reloaded.Accessibility.ScreenShake-0.5) > 1e-4 ||
		!reloaded.Accessibility.ReduceFlashes ||
		reloaded.Accessibility.ColorBlindMode != "deuteranopia" ||
		math.Abs(reloaded.Accessibility.GameSpeed-0.75) > 1e-4 ||
		!reloaded.Accessibility.Invincibility ||
		!reloaded.Accessibility.AutoAim ||
		!reloaded.Accessibility.HoldToToggle {
		t.Fatalf("reloaded Accessibility = %+v", reloaded.Accessibility)
	}

	// Verify Targets were applied too!
	if math.Abs(float64(mixer.Master())-0.80) > 1e-3 || math.Abs(float64(mixer.Volume("music"))-0.70) > 1e-3 {
		t.Fatalf("mixer master=%v music=%v, want 0.80, 0.70", mixer.Master(), mixer.Volume("music"))
	}
	if !mixer.Mono() || math.Abs(float64(mixer.Balance())-0.30) > 1e-3 {
		t.Fatalf("mixer mono=%v balance=%v, want true, 0.30", mixer.Mono(), mixer.Balance())
	}
	if appliedDisplay.Quality != "medium" || appliedLocale != "zh-Hans" || !appliedA11y.HighContrast {
		t.Fatalf("targets not applied: display=%+v locale=%q a11y=%+v", appliedDisplay, appliedLocale, appliedA11y)
	}
	if !slices.Equal(appliedBindings["jump"], []string{"key:space", "gamepad:west"}) {
		t.Fatalf("appliedBindings[jump] = %v", appliedBindings["jump"])
	}

	// Gamepad B (East) triggers Back.
	pressGamepadButton(menu, gamepad.ButtonEast)
	if backCount != 1 {
		t.Fatalf("backCount = %d, want 1", backCount)
	}
}

func TestSettingsMenuMouseTouchAndKeyboardInteraction(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileSettingsStore(dir)
	menu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
		Store: store,
	})
	bounds := core.Bounds{Size: core.Size{Width: 960, Height: 540}}
	menu.Layout(bounds)

	// Mouse click Video tab button.
	vidTab := centerOf(menu.TabButton(gameui.SettingsTabVideo).Bounds())
	menu.OnPointerDown(vidTab)
	menu.OnPointerUp(vidTab)
	if menu.Tab() != gameui.SettingsTabVideo {
		t.Fatalf("tab after mouse click = %v, want video", menu.Tab())
	}

	// Touch tap Window Mode button to toggle Fullscreen.
	winBtn := menu.TabControls(gameui.SettingsTabVideo)[0]
	pt := centerOf(winBtn.Bounds())
	menu.OnTouch(core.TouchEvent{
		Phase:   core.TouchStart,
		Touches: []core.Touch{{ID: 7, Position: pt, Changed: true}},
	})
	menu.OnTouch(core.TouchEvent{
		Phase:   core.TouchEnd,
		Touches: []core.Touch{{ID: 7, Position: pt, Changed: true}},
	})
	if !menu.Settings().Display.Fullscreen {
		t.Fatal("expected Fullscreen = true after touch tap")
	}

	// Touch drag on Audio Master slider (switch to Audio tab via touch first).
	audioTab := centerOf(menu.TabButton(gameui.SettingsTabAudio).Bounds())
	menu.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 8, Position: audioTab, Changed: true}}})
	menu.OnTouch(core.TouchEvent{Phase: core.TouchEnd, Touches: []core.Touch{{ID: 8, Position: audioTab, Changed: true}}})
	if menu.Tab() != gameui.SettingsTabAudio {
		t.Fatalf("tab after touch tap = %v, want audio", menu.Tab())
	}
	masterSlider := menu.TabControls(gameui.SettingsTabAudio)[1].(*gameui.Slider)
	tr := masterSlider.TrackBounds()
	midPt := core.Point{X: tr.Origin.X + tr.Size.Width/2, Y: tr.Origin.Y + tr.Size.Height/2}
	menu.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 9, Position: tr.Origin, Changed: true}}})
	menu.OnTouch(core.TouchEvent{Phase: core.TouchMove, Touches: []core.Touch{{ID: 9, Position: midPt, Changed: true}}})
	menu.OnTouch(core.TouchEvent{Phase: core.TouchEnd, Touches: []core.Touch{{ID: 9, Position: midPt, Changed: true}}})
	if math.Abs(menu.Settings().Audio.Master-0.5) > 0.05 {
		t.Fatalf("Audio.Master after touch drag = %v, want ~0.5", menu.Settings().Audio.Master)
	}

	// Keyboard rebinding capture + Escape cancel + ResetControls.
	menu.SetTab(gameui.SettingsTabControls)
	menu.Layout(bounds)
	// First control is "left"
	menu.OnKeyDown(core.KeyEvent{Key: "enter"})
	if menu.CapturingAction() != "left" {
		t.Fatalf("CapturingAction = %q, want left", menu.CapturingAction())
	}
	// Escape cancels capture without closing menu or changing bindings.
	menu.OnKeyDown(core.KeyEvent{Key: "escape"})
	if menu.CapturingAction() != "" {
		t.Fatalf("expected Escape to cancel capture")
	}
	// Re-enter capture and bind keyboard key "j".
	menu.OnKeyDown(core.KeyEvent{Key: "enter"})
	menu.OnKeyDown(core.KeyEvent{Key: "j"})
	if !slices.Equal(menu.Settings().Bindings["left"], []string{"key:j", "gamepad:dpad_left"}) {
		t.Fatalf("Bindings[left] = %v, want [key:j gamepad:dpad_left]", menu.Settings().Bindings["left"])
	}

	// ResetControls restores defaults.
	menu.ResetControls()
	if !slices.Equal(menu.Settings().Bindings["left"], []string{"key:a", "gamepad:dpad_left"}) {
		t.Fatalf("Bindings[left] after ResetControls = %v, want [key:a gamepad:dpad_left]", menu.Settings().Bindings["left"])
	}

	// Mouse click Save footer button and verify persisted.
	_, saveBtn, resetBtn, _ := menu.FooterButtons()
	savePt := centerOf(saveBtn.Bounds())
	menu.OnPointerDown(savePt)
	menu.OnPointerUp(savePt)
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if !loaded.Display.Fullscreen {
		t.Fatal("expected Fullscreen persisted via Save button click")
	}

	// Mouse click Reset footer button restores defaults.
	resetPt := centerOf(resetBtn.Bounds())
	menu.OnPointerDown(resetPt)
	menu.OnPointerUp(resetPt)
	if menu.Settings().Display.Fullscreen {
		t.Fatal("expected Reset to restore Fullscreen = false")
	}
}

func TestBindingTokenRoundTripAndApplyToActions(t *testing.T) {
	cases := []struct {
		in    input.Input
		token string
	}{
		{input.Key("space"), "key:space"},
		{input.KeyCode("KeyW"), "keycode:KeyW"},
		{input.GamepadButton(gamepad.ButtonNorth), "gamepad:north"},
		{input.MouseButton(core.MouseButtonRight), "mouse:right"},
	}
	for _, tc := range cases {
		gotTok := gameui.FormatBindingToken(tc.in)
		if gotTok != tc.token {
			t.Fatalf("FormatBindingToken(%+v) = %q, want %q", tc.in, gotTok, tc.token)
		}
		parsed, ok := gameui.ParseBindingToken(gotTok)
		if !ok || parsed != tc.in {
			t.Fatalf("ParseBindingToken(%q) = (%+v, %v), want (%+v, true)", gotTok, parsed, ok, tc.in)
		}
	}

	// ApplyBindingsToActions preserves touch/mouse regions when requested.
	actions := input.NewActions()
	region := input.TouchRegion(core.Bounds{Size: core.Size{Width: 50, Height: 50}})
	actions.Bind("jump", region, input.Key("space"), input.GamepadButton(gamepad.ButtonSouth))
	gameui.ApplyBindingsToActions(actions, map[string][]string{
		"jump": {"key:k", "gamepad:west"},
	}, true)
	got := actions.Bindings("jump")
	want := []input.Input{region, input.Key("k"), input.GamepadButton(gamepad.ButtonWest)}
	if !slices.Equal(got, want) {
		t.Fatalf("Bindings(jump) = %+v, want %+v", got, want)
	}
}

func TestSettingsMenuHidesWindowModeWhenPlatformCannotFullScreen(t *testing.T) {
	cases := []struct {
		name           string
		env            core.Environment
		wantWindowMode bool
		wantCount      int
	}{
		{"web phone", core.Environment{OS: core.OSWeb, Idiom: core.IdiomPhone}, false, 4},
		{"ios phone", core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone}, false, 4},
		{"android phone", core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}, false, 4},
		{"web desktop", core.Environment{OS: core.OSWeb, Idiom: core.IdiomDesktop}, true, 5},
		{"macos desktop", core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}, true, 6},
	}

	bounds := core.Bounds{Size: core.Size{Width: 960, Height: 540}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var menu *gameui.SettingsMenu
			nsgotest.Start(t, func(cx *core.Scope) core.Node {
				menu = gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
					Env: cx.Env(),
				})
				return core.View{}
			}, nsgotest.Environment(tc.env))

			menu.SetTab(gameui.SettingsTabVideo)
			menu.Layout(bounds)

			controls := menu.TabControls(gameui.SettingsTabVideo)
			hasWindowMode := false
			for _, w := range controls {
				if btn, ok := w.(*gameui.Button); ok && strings.HasPrefix(btn.Text, "Window Mode:") {
					hasWindowMode = true
				}
			}
			if hasWindowMode != tc.wantWindowMode {
				t.Fatalf("%s: hasWindowMode = %v, want %v (controls=%d)", tc.name, hasWindowMode, tc.wantWindowMode, len(controls))
			}

			if len(controls) != tc.wantCount {
				t.Fatalf("%s: Video tab control count = %d, want %d", tc.name, len(controls), tc.wantCount)
			}

			// Verify layout bounds remain non-overlapping and inside bounds.
			for i, w := range controls {
				b := w.Bounds()
				if b.Size.Width <= 0 || b.Size.Height <= 0 || !boundsInside(b, bounds) {
					t.Fatalf("%s: control %d invalid bounds %+v", tc.name, i, b)
				}
				for j := i + 1; j < len(controls); j++ {
					if boundsOverlap(b, controls[j].Bounds()) {
						t.Fatalf("%s: controls %d and %d overlap", tc.name, i, j)
					}
				}
			}
		})
	}
}

func TestSettingsMenuWebSyncsWindowModeWithEnvFullScreen(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileSettingsStore(dir)
	saved := save.DefaultSettings()
	saved.Display.Fullscreen = true
	if err := store.Save(saved); err != nil {
		t.Fatal(err)
	}

	var applied []bool
	var menu *gameui.SettingsMenu
	webEnv := core.Environment{OS: core.OSWeb, Idiom: core.IdiomDesktop, FullScreen: false}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		menu = gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
			Store:     store,
			Env:       cx.Env(),
			AutoApply: true,
			Targets: save.SettingsTargets{
				ApplyDisplay: func(d save.DisplaySettings) error {
					applied = append(applied, d.Fullscreen)
					return nil
				},
			},
		})
		return core.View{}
	}, nsgotest.Environment(webEnv))

	menu.SetTab(gameui.SettingsTabVideo)
	menu.Layout(core.Bounds{Size: core.Size{Width: 960, Height: 540}})

	// Saved Fullscreen=true must not lie while the web page is windowed.
	winBtn := menu.TabControls(gameui.SettingsTabVideo)[0].(*gameui.Button)
	if !strings.Contains(winBtn.Text, "Windowed") {
		t.Fatalf("Window Mode text = %q, want Windowed", winBtn.Text)
	}

	// One click enters full screen.
	pt := centerOf(winBtn.Bounds())
	menu.OnPointerDown(pt)
	menu.OnPointerUp(pt)
	if !slices.Equal(applied, []bool{true}) {
		t.Fatalf("applied fullscreen = %v, want [true]", applied)
	}

	// Browser enters full screen, then leaves via Escape.
	fullEnv := webEnv
	fullEnv.FullScreen = true
	app.SetEnvironment(fullEnv)
	_ = menu.Settings()
	app.SetEnvironment(webEnv)
	if menu.Settings().Display.Fullscreen {
		t.Fatal("expected Display.Fullscreen == false after browser left full screen")
	}
	if !strings.Contains(winBtn.Text, "Windowed") {
		t.Fatalf("Window Mode text after Escape = %q, want Windowed", winBtn.Text)
	}
}

func TestSettingsMenuVSyncHiddenByDefaultAndOptIn(t *testing.T) {
	// Default config omits the VSync toggle.
	defMenu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{})
	defControls := defMenu.TabControls(gameui.SettingsTabVideo)
	if len(defControls) != 6 {
		t.Fatalf("default Video controls = %d, want 6", len(defControls))
	}
	for _, w := range defControls {
		if tog, ok := w.(*gameui.Toggle); ok && tog.Label == gameui.DefaultSettingsText().VSync {
			t.Fatalf("unexpected VSync toggle in default Video tab")
		}
	}

	// Opt-in ShowVSync shows the toggle and updates Display.VSync.
	optMenu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{ShowVSync: true})
	optMenu.SetTab(gameui.SettingsTabVideo)
	optMenu.Layout(core.Bounds{Size: core.Size{Width: 960, Height: 540}})
	optControls := optMenu.TabControls(gameui.SettingsTabVideo)
	if len(optControls) != 7 {
		t.Fatalf("ShowVSync Video controls = %d, want 7", len(optControls))
	}
	vsyncTog, ok := optControls[5].(*gameui.Toggle)
	if !ok || vsyncTog.Label != gameui.DefaultSettingsText().VSync || !vsyncTog.Checked {
		t.Fatalf("control[5] = %T (%+v), want checked VSync toggle", optControls[5], optControls[5])
	}
	optMenu.Navigator().SetFocus(vsyncTog)
	optMenu.HandleAction(gameui.NavConfirm)
	if optMenu.Settings().Display.VSync {
		t.Fatal("expected ShowVSync toggle to flip Display.VSync to false")
	}
}

func TestSettingsMenuHidesResolutionOnPhonesAndWeb(t *testing.T) {
	cases := []struct {
		name           string
		env            core.Environment
		hideResolution bool
		wantCount      int
	}{
		{"macos_desktop", core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}, false, 6},
		{"windows_desktop", core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop}, false, 6},
		{"linux_desktop", core.Environment{OS: core.OSLinux, Idiom: core.IdiomDesktop}, false, 6},
		{"ios_phone", core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone}, false, 4},
		{"android_phone", core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}, false, 4},
		{"web", core.Environment{OS: core.OSWeb, Idiom: core.IdiomDesktop}, false, 5},
		{"simulated_macos", core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Simulated: true}, false, 5},
		{"explicit_hide_on_desktop", core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}, true, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var menu *gameui.SettingsMenu
			nsgotest.Start(t, func(cx *core.Scope) core.Node {
				menu = gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
					Env:            cx.Env(),
					HideResolution: tc.hideResolution,
				})
				return core.View{}
			}, nsgotest.Environment(tc.env))

			menu.SetTab(gameui.SettingsTabVideo)
			menu.Layout(core.Bounds{Size: core.Size{Width: 960, Height: 540}})
			controls := menu.TabControls(gameui.SettingsTabVideo)
			if len(controls) != tc.wantCount {
				t.Fatalf("Video controls = %d, want %d", len(controls), tc.wantCount)
			}
			hasRes := false
			for _, w := range controls {
				if btn, ok := w.(*gameui.Button); ok && len(btn.Text) >= len("Resolution") && btn.Text[:len("Resolution")] == "Resolution" {
					hasRes = true
				}
			}
			if wantRes := tc.wantCount == 6; hasRes != wantRes {
				t.Fatalf("hasResolution = %v, want %v", hasRes, wantRes)
			}
		})
	}
}

func TestSettingsMenuAppliesResolutionToVideo(t *testing.T) {
	v := &screen.Video{NativeWindowSize: core.Size{Width: 960, Height: 540}}
	var menu *gameui.SettingsMenu
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v.Run(cx)
		menu = gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
			Env:     cx.Env(),
			Video:   v,
			Targets: save.SettingsTargets{ApplyDisplay: v.Apply},
		})
		return core.View{}
	}, nsgotest.Size(960, 540), nsgotest.Environment(core.Environment{
		OS:    core.OSMacOS,
		Idiom: core.IdiomDesktop,
		Displays: []core.DisplayInfo{
			{ID: "main", Main: true, Frame: core.Bounds{Size: core.Size{Width: 1920, Height: 1080}}},
		},
	}))

	menu.SetTab(gameui.SettingsTabVideo)
	menu.Layout(core.Bounds{Size: core.Size{Width: 960, Height: 540}})

	// Move from WindowMode [0] to Resolution [1] (starts on "native"), step twice -> 960x540 -> 1280x720, then Apply.
	app.Post(func() {
		menu.HandleAction(gameui.NavDown)
		menu.HandleAction(gameui.NavRight)
		menu.HandleAction(gameui.NavRight)
		_ = menu.Apply()
	})
	app.Settle()

	// Step twice more -> 1920x1080 -> native, then Apply.
	app.Post(func() {
		menu.HandleAction(gameui.NavRight)
		menu.HandleAction(gameui.NavRight)
		_ = menu.Apply()
	})
	app.Settle()

	var frames []core.WindowFrame
	for _, r := range app.Window {
		if r.Action == nsgotest.WindowSetFrame {
			frames = append(frames, r.Frame)
		}
	}
	want := []core.WindowFrame{
		{X: 320, Y: 180, Move: true, Width: 1280, Height: 720},
		{X: 480, Y: 270, Move: true, Width: 960, Height: 540},
	}
	if !slices.Equal(frames, want) {
		t.Fatalf("WindowSetFrame requests = %+v, want %+v", frames, want)
	}
}

func TestParseSettingsResolution(t *testing.T) {
	cases := []struct {
		id     string
		w, h   int
		wantOK bool
	}{
		{"", 0, 0, true},
		{"native", 0, 0, true},
		{" Native ", 0, 0, true},
		{"1280x720", 1280, 720, true},
		{"1920×1080", 1920, 1080, true},
		{" 960 x 540 ", 960, 540, true},
		{"0x720", 0, 0, false},
		{"-1280x720", 0, 0, false},
		{"custom", 0, 0, false},
	}
	for _, tc := range cases {
		w, h, ok := gameui.ParseSettingsResolution(tc.id)
		if ok != tc.wantOK || w != tc.w || h != tc.h {
			t.Errorf("ParseSettingsResolution(%q) = (%d, %d, %v), want (%d, %d, %v)", tc.id, w, h, ok, tc.w, tc.h, tc.wantOK)
		}
	}
}

func TestSettingsMenuSaveUpdatesAssistCaptionsViewAndFlash(t *testing.T) {
	run := func(t *testing.T, makeStore func(cx *core.Scope) *save.DefaultSettingsStore) {
		var store *save.DefaultSettingsStore
		var assist *a11y.Assist
		var track *a11y.CaptionTrack
		var flash *postfx.Flash
		var menu *gameui.SettingsMenu

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			store = makeStore(cx)
			assist = a11y.NewAssistFromSettings(cx, store)
			track = a11y.NewCaptionTrack(cx)
			flash = postfx.NewFlash(cx, postfx.FlashConfig{
				Color:        core.RGB(0xffffff),
				ReduceMotion: assist.ReduceMotion,
			})
			menu = gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
				Store: store,
				Env:   cx.Env(),
			})
			return a11y.CaptionsView(cx, track, a11y.CaptionStyle{
				FontSize: 16,
				Assist:   assist,
			})
		}, nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

		// Initially captions are off and flashes run normally.
		app.Post(func() {
			track.ShowDialogue("Guide", "Watch the bridge!", 5*time.Second)
			track.ShowSFX("Thunder", 5*time.Second)
		})
		app.Settle()

		if app.Exists("text:Guide: Watch the bridge!") || app.Exists("text:Thunder") {
			t.Fatal("captions rendered before player enabled them in SettingsMenu")
		}
		var triggeredBefore bool
		app.Post(func() {
			triggeredBefore = flash.Trigger(0.8, 200*time.Millisecond)
			flash.Stop()
		})
		app.Settle()
		if !triggeredBefore {
			t.Fatal("flash did not trigger while Reduced Motion was off")
		}

		// Player toggles Subtitles, SFX Captions, Text Scale (1.5x), Reduced Motion, Reduce Flashes,
		// Invincibility, Auto-Aim, Hold to Toggle in SettingsMenu and clicks Save.
		app.Post(func() {
			menu.SetTab(gameui.SettingsTabAccessibility)
			menu.Layout(core.Bounds{Size: core.Size{Width: 960, Height: 540}})
			// [0] Captions = true
			menu.HandleAction(gameui.NavConfirm)
			// [1] SFX Captions = true
			menu.HandleAction(gameui.NavDown)
			menu.HandleAction(gameui.NavConfirm)
			// [2] Text Scale = 1.5 (two steps up from 1.0)
			menu.HandleAction(gameui.NavDown)
			menu.HandleAction(gameui.NavRight)
			menu.HandleAction(gameui.NavRight)
			// [3] High Contrast = skip
			menu.HandleAction(gameui.NavDown)
			// [4] Reduced Motion = true
			menu.HandleAction(gameui.NavDown)
			menu.HandleAction(gameui.NavConfirm)
			// [5] Screen Shake = skip
			menu.HandleAction(gameui.NavDown)
			// [6] Reduce Flashes = true
			menu.HandleAction(gameui.NavDown)
			menu.HandleAction(gameui.NavConfirm)
			// [7] Color-Blind = protanopia
			menu.HandleAction(gameui.NavDown)
			menu.HandleAction(gameui.NavRight)
			// [8] Color-Blind Strength = 0.8 (two steps left from 1.0)
			menu.HandleAction(gameui.NavDown)
			menu.HandleAction(gameui.NavLeft)
			menu.HandleAction(gameui.NavLeft)
			// [9] Game Speed = skip
			menu.HandleAction(gameui.NavDown)
			// [10] Invincibility = true
			menu.HandleAction(gameui.NavDown)
			menu.HandleAction(gameui.NavConfirm)
			// [11] Auto-Aim = true
			menu.HandleAction(gameui.NavDown)
			menu.HandleAction(gameui.NavConfirm)
			// [12] Hold to Toggle = true
			menu.HandleAction(gameui.NavDown)
			menu.HandleAction(gameui.NavConfirm)

			if err := menu.Save(); err != nil {
				t.Errorf("menu.Save: %v", err)
			}
		})
		app.Settle()

		// Assist observes every saved change immediately.
		if !assist.CaptionsEnabled() || !assist.SFXCaptionsEnabled() ||
			assist.TextScale() != 1.5 || !assist.ReduceMotion() ||
			!assist.ReduceFlashes() || assist.ColorVision() != a11y.VisionProtanopia ||
			math.Abs(assist.ColorBlindSeverity()-0.8) > 1e-4 ||
			!assist.Invincibility() || !assist.AutoAim() || !assist.HoldToToggle() {
			t.Fatalf("Assist did not observe SettingsMenu.Save: %+v severity=%v", assist.Snapshot(), assist.ColorBlindSeverity())
		}
		if _, ok := assist.ColorVisionFilter(); !ok {
			t.Fatal("expected Assist.ColorVisionFilter to be active after saving protanopia")
		}

		// CaptionsView reactively renders both dialogue (at 1.5x = 24pt) and SFX captions.
		dlgText := app.Find("text:Guide: Watch the bridge!")
		if dlgText.TextStyle.Font.Size != 24 {
			t.Fatalf("scaled caption Font.Size = %v, want 24", dlgText.TextStyle.Font.Size)
		}
		app.Find("text:[Thunder]")

		// postfx.Flash respects Assist.ReduceMotion and refuses to trigger.
		var triggeredAfter bool
		app.Post(func() {
			triggeredAfter = flash.Trigger(0.8, 200*time.Millisecond)
		})
		app.Settle()
		if triggeredAfter {
			t.Fatal("postfx.Flash triggered despite Reduced Motion saved in SettingsMenu")
		}
	}

	t.Run("StorageBackend", func(t *testing.T) {
		run(t, func(cx *core.Scope) *save.DefaultSettingsStore {
			return save.NewStorageSettingsStore(cx.Storage(), "mygame")
		})
	})

	t.Run("FileBackend", func(t *testing.T) {
		dir := t.TempDir()
		run(t, func(*core.Scope) *save.DefaultSettingsStore {
			return save.NewFileSettingsStore(dir)
		})
	})
}

func TestSettingsMenuAccessibilityFollowsPlatformAndOverrides(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileSettingsStore(dir)

	var menu *gameui.SettingsMenu
	var assist *a11y.Assist
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		assist = a11y.NewAssistFromSettings(cx, store)
		menu = gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
			Store: store,
			Env:   cx.Env(),
		})
		return core.View{}
	}, nsgotest.Environment(core.Environment{
		OS:               core.OSMacOS,
		Idiom:            core.IdiomDesktop,
		ReduceMotion:     true,
		IncreaseContrast: true,
		FontScale:        1.5,
	}))

	menu.SetTab(gameui.SettingsTabAccessibility)
	menu.Layout(core.Bounds{Size: core.Size{Width: 960, Height: 540}})
	controls := menu.TabControls(gameui.SettingsTabAccessibility)

	// Platform defaults are reflected in the Accessibility controls without locking overrides.
	textScale := controls[2].(*gameui.Slider)
	highContrast := controls[3].(*gameui.Toggle)
	reduceMotion := controls[4].(*gameui.Toggle)
	if math.Abs(float64(textScale.Value)-1.5) > 1e-4 || !highContrast.Checked || !reduceMotion.Checked {
		t.Fatalf("controls did not reflect platform env: scale=%v hc=%v rm=%v", textScale.Value, highContrast.Checked, reduceMotion.Checked)
	}

	// Saving an unrelated Audio change preserves follow-platform behavior (no *Set flags).
	app.Post(func() {
		menu.SetTab(gameui.SettingsTabAudio)
		menu.HandleAction(gameui.NavConfirm) // toggle mute
		if err := menu.Save(); err != nil {
			t.Errorf("Save audio: %v", err)
		}
	})
	app.Settle()

	saved, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Accessibility.ReduceMotionSet || saved.Accessibility.HighContrastSet || saved.Accessibility.CaptionScaleSet {
		t.Fatalf("unrelated save locked platform overrides: %+v", saved.Accessibility)
	}
	if !assist.ReduceMotion() || !assist.HighContrast() || assist.TextScale() != 1.5 {
		t.Fatalf("Assist lost platform fallback after unrelated save: %+v", assist.Snapshot())
	}

	// Explicitly toggling Reduced Motion off overrides the OS setting.
	app.Post(func() {
		menu.SetTab(gameui.SettingsTabAccessibility)
		menu.Navigator().SetFocus(reduceMotion)
		menu.HandleAction(gameui.NavConfirm)
		if err := menu.Save(); err != nil {
			t.Errorf("Save override: %v", err)
		}
	})
	app.Settle()
	if assist.ReduceMotion() {
		t.Fatal("explicit Reduced Motion off in SettingsMenu did not override OS ReduceMotion=true")
	}

	// Reset restores platform fallback.
	app.Post(func() {
		menu.Reset()
		if err := menu.Save(); err != nil {
			t.Errorf("Save reset: %v", err)
		}
	})
	app.Settle()
	if !assist.ReduceMotion() {
		t.Fatal("Reset did not restore OS ReduceMotion=true fallback")
	}
}

func TestSettingsMenuAudioMonoAndBalanceControls(t *testing.T) {
	var mixer *sound.Mixer
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mixer = sound.NewMixer(cx, "test-audio-tab")
		return core.View{}
	}, nsgotest.Environment(core.Environment{Audio: true}))

	menu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
		Targets:   save.SettingsTargets{Sound: mixer},
		AutoApply: true,
	})
	menu.Layout(core.Bounds{Size: core.Size{Width: 960, Height: 540}})

	controls := menu.TabControls(gameui.SettingsTabAudio)
	if len(controls) != 7 {
		t.Fatalf("Audio tab controls = %d, want 7", len(controls))
	}
	monoBtn, ok := controls[5].(*gameui.Button)
	if !ok {
		t.Fatalf("control[5] = %T, want *gameui.Button", controls[5])
	}
	balSlider, ok := controls[6].(*gameui.Slider)
	if !ok {
		t.Fatalf("control[6] = %T, want *gameui.Slider", controls[6])
	}
	if monoBtn.Text != "Mono Audio: < System Default >" {
		t.Fatalf("initial Mono Audio text = %q", monoBtn.Text)
	}
	if balSlider.Label != "Balance: Center" {
		t.Fatalf("initial Balance label = %q", balSlider.Label)
	}

	// Cycle Mono Audio: System Default -> On -> Off -> System Default.
	menu.Navigator().SetFocus(monoBtn)
	menu.HandleAction(gameui.NavRight)
	if s := menu.Settings().Audio.Mono; s == nil || !*s || monoBtn.Text != "Mono Audio: < On >" || !mixer.Mono() {
		t.Fatalf("after 1st step: mono=%v text=%q mixer=%v", s, monoBtn.Text, mixer.Mono())
	}
	menu.HandleAction(gameui.NavRight)
	if s := menu.Settings().Audio.Mono; s == nil || *s || monoBtn.Text != "Mono Audio: < Off >" || mixer.Mono() {
		t.Fatalf("after 2nd step: mono=%v text=%q mixer=%v", s, monoBtn.Text, mixer.Mono())
	}
	menu.HandleAction(gameui.NavRight)
	if s := menu.Settings().Audio.Mono; s != nil || monoBtn.Text != "Mono Audio: < System Default >" || mixer.Mono() {
		t.Fatalf("after 3rd step: mono=%v text=%q mixer=%v", s, monoBtn.Text, mixer.Mono())
	}

	// Step Balance left and right and verify label formatting and live mixer balance.
	menu.Navigator().SetFocus(balSlider)
	menu.HandleAction(gameui.NavLeft)
	menu.HandleAction(gameui.NavLeft)
	if math.Abs(menu.Settings().Audio.Balance-(-0.2)) > 1e-4 || balSlider.Label != "Balance: L 20%" || math.Abs(float64(mixer.Balance())-(-0.2)) > 1e-3 {
		t.Fatalf("after 2 left steps: bal=%v label=%q mixer=%v", menu.Settings().Audio.Balance, balSlider.Label, mixer.Balance())
	}
	for range 5 {
		menu.HandleAction(gameui.NavRight)
	}
	if math.Abs(menu.Settings().Audio.Balance-0.3) > 1e-4 || balSlider.Label != "Balance: R 30%" || math.Abs(float64(mixer.Balance())-0.3) > 1e-3 {
		t.Fatalf("after 5 right steps: bal=%v label=%q mixer=%v", menu.Settings().Audio.Balance, balSlider.Label, mixer.Balance())
	}

	// Reset restores System Default mono and centered balance.
	menu.Reset()
	if menu.Settings().Audio.Mono != nil || menu.Settings().Audio.Balance != 0 || mixer.Mono() || mixer.Balance() != 0 {
		t.Fatalf("after Reset: mono=%v bal=%v mixer=(%v, %v)", menu.Settings().Audio.Mono, menu.Settings().Audio.Balance, mixer.Mono(), mixer.Balance())
	}
}

func TestSettingsMenuControlsTabLookAndCameraRows(t *testing.T) {
	store := save.NewFileSettingsStore(t.TempDir())
	var appliedControls save.ControlSettings
	var appliedCamera save.CameraSettings
	menu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
		Store:              store,
		ShowCameraSettings: true,
		Actions: []gameui.SettingsAction{
			{Name: "jump", Label: "Jump", Defaults: []string{"key:space"}},
		},
		Targets: save.SettingsTargets{
			ApplyControls: func(c save.ControlSettings) error { appliedControls = c; return nil },
			ApplyCamera:   func(c save.CameraSettings) error { appliedCamera = c; return nil },
		},
	})
	bounds := core.Bounds{Size: core.Size{Width: 960, Height: 540}}
	menu.SetTab(gameui.SettingsTabControls)
	menu.Layout(bounds)

	// 1 action + MinPress + AcceptDelay + 9 look and vibration rows + 4 camera rows + ResetControls.
	controls := menu.TabControls(gameui.SettingsTabControls)
	if len(controls) != 17 {
		t.Fatalf("Controls tab control count = %d, want 17", len(controls))
	}
	for i, w := range controls {
		b := w.Bounds()
		if b.Size.Width <= 0 || b.Size.Height <= 0 || !boundsInside(b, bounds) {
			t.Fatalf("control %d out of bounds: %+v", i, b)
		}
		for j := i + 1; j < len(controls); j++ {
			if boundsOverlap(b, controls[j].Bounds()) {
				t.Fatalf("controls %d and %d overlap: %+v vs %+v", i, j, b, controls[j].Bounds())
			}
		}
	}

	stick, invertY, deadZone, vibration, fov, motionBlur := controls[4], controls[8], controls[9], controls[11], controls[12], controls[15]
	menu.Navigator().SetFocus(stick) // Stick Sensitivity: 1 + 4 * 0.05
	for range 4 {
		pressGamepadButton(menu, gamepad.ButtonDPadRight)
	}
	menu.Navigator().SetFocus(invertY)
	pressGamepadButton(menu, gamepad.ButtonSouth)
	menu.Navigator().SetFocus(deadZone) // Stick Dead Zone: 0.15 down to 0, which turns it off
	for range 5 {
		pressGamepadButton(menu, gamepad.ButtonDPadLeft)
	}
	menu.Navigator().SetFocus(vibration) // Vibration: 1 down to 0
	for range 25 {
		pressGamepadButton(menu, gamepad.ButtonDPadLeft)
	}
	menu.Navigator().SetFocus(fov) // Field of View: 90 + 10 degrees
	for range 10 {
		pressGamepadButton(menu, gamepad.ButtonDPadRight)
	}
	menu.Navigator().SetFocus(motionBlur)
	pressGamepadButton(menu, gamepad.ButtonSouth)

	wantControls := save.DefaultSettings().Controls
	wantControls.StickSensitivity, wantControls.InvertY, wantControls.StickDeadZone, wantControls.Vibration = 1.2, true, 0, 0
	wantCamera := save.DefaultSettings().Camera
	wantCamera.FieldOfView, wantCamera.MotionBlur = 100, true
	if got := menu.Settings(); got.Controls != wantControls || got.Camera != wantCamera {
		t.Fatalf("menu settings = %+v / %+v, want %+v / %+v", got.Controls, got.Camera, wantControls, wantCamera)
	}
	if err := menu.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if appliedControls != wantControls || appliedCamera != wantCamera {
		t.Fatalf("applied = %+v / %+v, want %+v / %+v", appliedControls, appliedCamera, wantControls, wantCamera)
	}
	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if reloaded.Controls != wantControls || reloaded.Camera != wantCamera {
		t.Fatalf("reloaded = %+v / %+v, want %+v / %+v", reloaded.Controls, reloaded.Camera, wantControls, wantCamera)
	}

	menu.ResetControls()
	if got, def := menu.Settings(), save.DefaultSettings(); got.Controls != def.Controls || got.Camera != def.Camera {
		t.Fatalf("after ResetControls = %+v / %+v, want defaults", got.Controls, got.Camera)
	}
}

func TestSettingsMenuControlsTabMotorAssists(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileSettingsStore(dir)

	var appliedAssists save.InputAssistSettings
	menu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
		Store: store,
		Actions: []gameui.SettingsAction{
			{Name: "left", Label: "Move Left", Defaults: []string{"key:a"}},
			{Name: "right", Label: "Move Right", Defaults: []string{"key:d"}},
			{Name: "sprint", Label: "Sprint", Defaults: []string{"key:shift"}, AllowToggle: true},
			{Name: "struggle", Label: "Struggle", Defaults: []string{"key:e"}, AllowToggle: true, AllowMash: true},
			{Name: "pause", Label: "Pause", Defaults: []string{"key:escape"}},
		},
		Targets: save.SettingsTargets{
			ApplyInputAssists: func(ia save.InputAssistSettings) error {
				appliedAssists = ia
				return nil
			},
		},
	})
	bounds := core.Bounds{Size: core.Size{Width: 960, Height: 540}}
	menu.SetTab(gameui.SettingsTabControls)
	menu.Layout(bounds)

	// Controls layout: 5 action buttons + 2 toggle switches (sprint, struggle) + 1 mash switch (struggle) + MinPress + AcceptDelay + 9 look and vibration rows + ResetControls = 20 controls; the camera rows are hidden without ShowCameraSettings.
	controls := menu.TabControls(gameui.SettingsTabControls)
	if len(controls) != 20 {
		t.Fatalf("Controls tab control count = %d, want 20", len(controls))
	}
	// Verify zero overlap and full containment in 960x540 viewport.
	for i, w := range controls {
		b := w.Bounds()
		if b.Size.Width <= 0 || b.Size.Height <= 0 || !boundsInside(b, bounds) {
			t.Fatalf("control %d out of bounds: %+v", i, b)
		}
		for j := i + 1; j < len(controls); j++ {
			if boundsOverlap(b, controls[j].Bounds()) {
				t.Fatalf("controls %d and %d overlap: %+v vs %+v", i, j, b, controls[j].Bounds())
			}
		}
	}

	// Toggle Sprint Hold-to-Toggle ([5]), Struggle Hold-to-Mash ([7]), set MinPress=150ms ([8]), AcceptDelay=500ms ([9]).
	menu.Navigator().SetFocus(controls[5]) // Sprint: Hold to Toggle
	pressGamepadButton(menu, gamepad.ButtonSouth)
	menu.Navigator().SetFocus(controls[7]) // Struggle: Hold to Mash
	pressGamepadButton(menu, gamepad.ButtonSouth)
	menu.Navigator().SetFocus(controls[8]) // Min Press Time: 3 steps * 50ms = 150ms
	for range 3 {
		pressGamepadButton(menu, gamepad.ButtonDPadRight)
	}
	menu.Navigator().SetFocus(controls[9]) // Accept Delay: 10 steps * 50ms = 500ms
	for range 10 {
		pressGamepadButton(menu, gamepad.ButtonDPadRight)
	}

	if err := menu.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if !slices.Equal(reloaded.InputAssists.Toggle, []string{"sprint"}) {
		t.Fatalf("reloaded Toggle = %v, want [sprint]", reloaded.InputAssists.Toggle)
	}
	if !slices.Equal(reloaded.InputAssists.MashToHold, []string{"struggle"}) {
		t.Fatalf("reloaded MashToHold = %v, want [struggle]", reloaded.InputAssists.MashToHold)
	}
	if reloaded.InputAssists.MinPressMs != 150 || reloaded.InputAssists.AcceptDelayMs != 500 {
		t.Fatalf("reloaded timings = (%d, %d), want (150, 500)", reloaded.InputAssists.MinPressMs, reloaded.InputAssists.AcceptDelayMs)
	}
	if !slices.Equal(appliedAssists.Toggle, []string{"sprint"}) || appliedAssists.AcceptDelayMs != 500 {
		t.Fatalf("ApplyInputAssists = %+v", appliedAssists)
	}

	// ApplyInputAssistsToActions transfers settings + HoldToToggle onto live Actions.
	actions := input.NewActions()
	actions.SetActionAssist("sprint", input.ActionAssist{Toggle: true})
	actions.SetActionAssist("struggle", input.ActionAssist{Toggle: true, Mash: true})
	actions.SetActionAssist("pause", input.ActionAssist{})
	reloaded.Accessibility.HoldToToggle = true
	gameui.ApplyInputAssistsToActions(actions, reloaded)
	got := actions.Assists()
	if !slices.Equal(got.Toggle, []string{"sprint", "struggle"}) || !slices.Equal(got.MashToHold, []string{"struggle"}) ||
		got.MinPress != 150*time.Millisecond || got.AcceptDelay != 500*time.Millisecond {
		t.Fatalf("ApplyInputAssistsToActions = %+v", got)
	}

	// ResetControls clears per-action and timing motor assists back to defaults.
	menu.ResetControls()
	if !menu.Settings().InputAssists.IsZero() {
		t.Fatalf("ResetControls did not clear InputAssists: %+v", menu.Settings().InputAssists)
	}
}
