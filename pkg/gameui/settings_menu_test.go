package gameui_test

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/save"
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
	// Controls: [0] Mute, [1] Master, [2] Music, [3] SFX, [4] UI
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

	// --- 2. VIDEO TAB (switch with RB) ---
	// Controls: [0] WindowMode, [1] Resolution, [2] ScaleMode, [3] IntegerScale, [4] Quality, [5] VSync, [6] FrameCap
	pressGamepadButton(menu, gamepad.ButtonRB)
	if menu.Tab() != gameui.SettingsTabVideo {
		t.Fatalf("tab after RB = %v, want video", menu.Tab())
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
	// VSync -> false (via South)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonSouth)
	// FrameCap -> 30 -> 60 (via Right twice)
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
	// Controls: [0] Captions, [1] TextScale, [2] HighContrast, [3] ReduceMotion, [4] ScreenShake, [5] ReduceFlashes, [6] ColorBlind, [7] GameSpeed
	pressGamepadButton(menu, gamepad.ButtonRB)
	if menu.Tab() != gameui.SettingsTabAccessibility {
		t.Fatalf("tab after RB = %v, want accessibility", menu.Tab())
	}
	// Captions -> true
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
	// GameSpeed -> 0.75 (Left once from 1.0 by 0.25)
	pressGamepadButton(menu, gamepad.ButtonDPadDown)
	pressGamepadButton(menu, gamepad.ButtonDPadLeft)

	// --- SAVE VIA FOOTER USING GAMEPAD ALONE ---
	// From last control (GameSpeed), Down moves to footer Apply, Right moves to Save, South saves!
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
	if !reloaded.Display.Fullscreen || gameui.SettingsResolutionFrom(reloaded) != "1280x720" ||
		reloaded.Display.ScaleMode != "fill" || !reloaded.Display.IntegerScale ||
		reloaded.Display.Quality != "medium" || reloaded.Display.VSync || reloaded.Display.FrameCap != 60 {
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
		math.Abs(reloaded.Accessibility.CaptionScale-1.5) > 1e-4 ||
		!reloaded.Accessibility.HighContrast ||
		!reloaded.Accessibility.ReduceMotion ||
		math.Abs(reloaded.Accessibility.ScreenShake-0.5) > 1e-4 ||
		!gameui.SettingsReduceFlashesFrom(reloaded) ||
		reloaded.Accessibility.ColorBlindMode != "deuteranopia" ||
		math.Abs(reloaded.Accessibility.GameSpeed-0.75) > 1e-4 {
		t.Fatalf("reloaded Accessibility = %+v, reduceFlashes=%v", reloaded.Accessibility, gameui.SettingsReduceFlashesFrom(reloaded))
	}

	// Verify Targets were applied too!
	if math.Abs(float64(mixer.Master())-0.80) > 1e-3 || math.Abs(float64(mixer.Volume("music"))-0.70) > 1e-3 {
		t.Fatalf("mixer master=%v music=%v, want 0.80, 0.70", mixer.Master(), mixer.Volume("music"))
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
	}{
		{"web phone", core.Environment{OS: core.OSWeb, Idiom: core.IdiomPhone}, false},
		{"ios phone", core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone}, false},
		{"android phone", core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}, false},
		{"web desktop", core.Environment{OS: core.OSWeb, Idiom: core.IdiomDesktop}, true},
		{"macos desktop", core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}, true},
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

			wantCount := 6
			if tc.wantWindowMode {
				wantCount = 7
			}
			if len(controls) != wantCount {
				t.Fatalf("%s: Video tab control count = %d, want %d", tc.name, len(controls), wantCount)
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
