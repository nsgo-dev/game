package main

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/quality"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestSettingsMenuGamepadAloneChangesSavesAndReloads(t *testing.T) {
	app, g := startCaveRun(t, nsgotest.Environment(core.Environment{
		OS:    core.OSMacOS,
		Idiom: core.IdiomDesktop,
		Displays: []core.DisplayInfo{
			{ID: "main", Main: true, Frame: core.Bounds{Size: core.Size{Width: 1920, Height: 1080}}},
		},
	}))

	// Connect a gamepad and open the Settings menu from the title screen using gamepad alone (ButtonWest / X).
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.Advance(testFrame)
	pressPad(app, gamepad.ButtonWest)

	if !g.settingsOpen.Get() || !app.Exists("id:settings-menu") {
		t.Fatal("expected ButtonWest on title screen to open settings-menu")
	}
	if len(app.Find("id:settings-canvas").Ops) == 0 {
		t.Fatal("expected settings-canvas to render draw operations")
	}

	// --- 1. Audio tab ---
	// Focus starts on Mute toggle -> skip mute (leave unmuted), step Master down 4x (1.0 -> 0.80),
	// Music down 6x (1.0 -> 0.70), SFX down 8x (1.0 -> 0.60), UI down 10x (1.0 -> 0.50).
	pressPad(app, gamepad.ButtonDPadDown) // Master
	for range 4 {
		pressPad(app, gamepad.ButtonDPadLeft)
	}
	pressPad(app, gamepad.ButtonDPadDown) // Music
	for range 6 {
		pressPad(app, gamepad.ButtonDPadLeft)
	}
	pressPad(app, gamepad.ButtonDPadDown) // SFX
	for range 8 {
		pressPad(app, gamepad.ButtonDPadLeft)
	}
	pressPad(app, gamepad.ButtonDPadDown) // UI
	for range 10 {
		pressPad(app, gamepad.ButtonDPadLeft)
	}

	// --- 2. Video tab (switch with RB) ---
	pressPad(app, gamepad.ButtonRB)
	if g.settingsMenu.Tab() != gameui.SettingsTabVideo {
		t.Fatalf("tab = %v, want video", g.settingsMenu.Tab())
	}
	if got := len(g.settingsMenu.TabControls(gameui.SettingsTabVideo)); got != 6 {
		t.Fatalf("Video tab controls = %d, want 6 (VSync absent by default)", got)
	}
	// WindowMode -> Fullscreen
	pressPad(app, gamepad.ButtonDPadRight)
	// Resolution -> 1280x720 (next from 960x540)
	pressPad(app, gamepad.ButtonDPadDown)
	pressPad(app, gamepad.ButtonDPadRight)
	// ScaleMode -> fill
	pressPad(app, gamepad.ButtonDPadDown)
	pressPad(app, gamepad.ButtonDPadRight)
	// IntegerScale -> false (default was true from game.yaml)
	pressPad(app, gamepad.ButtonDPadDown)
	pressPad(app, gamepad.ButtonSouth)
	// Quality -> low
	pressPad(app, gamepad.ButtonDPadDown)
	pressPad(app, gamepad.ButtonDPadRight)
	// FrameCap -> 30 (VSync is hidden by default)
	pressPad(app, gamepad.ButtonDPadDown)
	pressPad(app, gamepad.ButtonDPadRight)

	// --- 3. Controls tab (switch with RB) ---
	// Actions: [0] left, [1] right, [2] jump, [3] pause, [4] restart, [5] swarm
	pressPad(app, gamepad.ButtonRB)
	if g.settingsMenu.Tab() != gameui.SettingsTabControls {
		t.Fatalf("tab = %v, want controls", g.settingsMenu.Tab())
	}
	pressPad(app, gamepad.ButtonDPadDown) // right
	pressPad(app, gamepad.ButtonDPadDown) // jump
	pressPad(app, gamepad.ButtonSouth)    // begin capture for jump
	if got := g.settingsMenu.CapturingAction(); got != actionJump {
		t.Fatalf("CapturingAction = %q, want %q", got, actionJump)
	}
	pressPad(app, gamepad.ButtonRB) // rebind jump gamepad button to RB!
	if g.settingsMenu.CapturingAction() != "" {
		t.Fatalf("expected capture to finish, still capturing %q", g.settingsMenu.CapturingAction())
	}

	// --- 4. Language tab (switch with RB) ---
	// Locales: [0] "", [1] "en", [2] "zh-Hans"
	pressPad(app, gamepad.ButtonRB)
	if g.settingsMenu.Tab() != gameui.SettingsTabLanguage {
		t.Fatalf("tab = %v, want language", g.settingsMenu.Tab())
	}
	pressPad(app, gamepad.ButtonDPadDown) // en
	pressPad(app, gamepad.ButtonDPadDown) // zh-Hans
	pressPad(app, gamepad.ButtonSouth)    // select zh-Hans

	// --- 5. Accessibility tab (switch with RB) ---
	// Controls: [0] Captions, [1] SFXCaptions, [2] TextScale, [3] HighContrast, [4] ReduceMotion, [5] ScreenShake, [6] ReduceFlashes, [7] ColorBlind, [8] ColorBlindSeverity, [9] GameSpeed, [10] Invincibility, [11] AutoAim, [12] HoldToToggle
	pressPad(app, gamepad.ButtonRB)
	if g.settingsMenu.Tab() != gameui.SettingsTabAccessibility {
		t.Fatalf("tab = %v, want accessibility", g.settingsMenu.Tab())
	}
	pressPad(app, gamepad.ButtonSouth)    // Captions = true
	pressPad(app, gamepad.ButtonDPadDown) // SFXCaptions = true
	pressPad(app, gamepad.ButtonSouth)
	pressPad(app, gamepad.ButtonDPadDown) // TextScale
	pressPad(app, gamepad.ButtonDPadRight)
	pressPad(app, gamepad.ButtonDPadDown) // HighContrast = true
	pressPad(app, gamepad.ButtonSouth)
	pressPad(app, gamepad.ButtonDPadDown) // ReduceMotion = true
	pressPad(app, gamepad.ButtonSouth)
	pressPad(app, gamepad.ButtonDPadDown) // ScreenShake -> 0.5
	for range 5 {
		pressPad(app, gamepad.ButtonDPadLeft)
	}
	pressPad(app, gamepad.ButtonDPadDown) // ReduceFlashes = true
	pressPad(app, gamepad.ButtonSouth)
	pressPad(app, gamepad.ButtonDPadDown) // ColorBlind -> protanopia
	pressPad(app, gamepad.ButtonDPadRight)
	pressPad(app, gamepad.ButtonDPadDown) // ColorBlindSeverity -> 0.8
	pressPad(app, gamepad.ButtonDPadLeft)
	pressPad(app, gamepad.ButtonDPadLeft)
	pressPad(app, gamepad.ButtonDPadDown) // GameSpeed -> 0.75
	pressPad(app, gamepad.ButtonDPadLeft)
	pressPad(app, gamepad.ButtonDPadDown) // Invincibility = true
	pressPad(app, gamepad.ButtonSouth)
	pressPad(app, gamepad.ButtonDPadDown) // AutoAim = true
	pressPad(app, gamepad.ButtonSouth)
	pressPad(app, gamepad.ButtonDPadDown) // HoldToToggle = true
	pressPad(app, gamepad.ButtonSouth)

	// --- Save via footer bar using gamepad alone ---
	pressPad(app, gamepad.ButtonDPadDown)  // Apply
	pressPad(app, gamepad.ButtonDPadRight) // Save
	pressPad(app, gamepad.ButtonSouth)     // Confirm Save

	if g.settingsMenu.Err() != nil {
		t.Fatalf("settings save failed: %v", g.settingsMenu.Err())
	}

	// Close settings menu via gamepad East (B).
	pressPad(app, gamepad.ButtonEast)
	if g.settingsOpen.Get() || app.Exists("id:settings-menu") {
		t.Fatal("expected ButtonEast to close settings-menu")
	}

	// Verify live subsystems received every saved setting.
	if math.Abs(float64(g.mixer.Master())-0.80) > 1e-3 ||
		math.Abs(float64(g.mixer.Volume(soundGroupMusic))-0.70) > 1e-3 ||
		math.Abs(float64(g.mixer.Volume(soundGroupSFX))-0.60) > 1e-3 ||
		math.Abs(float64(g.mixer.Volume("ui"))-0.50) > 1e-3 {
		t.Fatalf("live mixer = master:%v music:%v sfx:%v ui:%v",
			g.mixer.Master(), g.mixer.Volume(soundGroupMusic), g.mixer.Volume(soundGroupSFX), g.mixer.Volume("ui"))
	}
	if g.resolution != "1280x720" || g.viewport.Mode != screen.ModeFill || g.viewport.IntegerScale {
		t.Fatalf("live display = res:%q mode:%v intScale:%v", g.resolution, g.viewport.Mode, g.viewport.IntegerScale)
	}
	if g.quality.Level() != quality.Low || g.quality.Auto() {
		t.Fatalf("live quality = level:%v auto:%v, want low/false", g.quality.Level(), g.quality.Auto())
	}
	if g.loop.DrawRate != 30 {
		t.Fatalf("live loop.DrawRate = %d, want 30", g.loop.DrawRate)
	}
	if g.locale.Get() != "zh-Hans" {
		t.Fatalf("live locale = %q, want zh-Hans", g.locale.Get())
	}
	if !g.a11y.Captions || !g.a11y.SFXCaptions || !g.a11y.HighContrast || !g.a11y.ReduceMotion || !g.reduceFlashes ||
		!g.a11y.ReduceFlashes || !g.a11y.Invincibility || !g.a11y.AutoAim || !g.a11y.HoldToToggle ||
		g.a11y.ColorBlindMode != "protanopia" || math.Abs(g.a11y.GameSpeed-0.75) > 1e-4 {
		t.Fatalf("live a11y = %+v, reduceFlashes=%v", g.a11y, g.reduceFlashes)
	}
	if g.level.shakeScale != 0 {
		t.Fatalf("level.shakeScale with ReduceMotion = %v, want 0", g.level.shakeScale)
	}

	// Verify saved settings persisted in save.DefaultSettingsStore!
	loaded, err := g.settingsStore.Load()
	if err != nil {
		t.Fatalf("settingsStore.Load: %v", err)
	}
	if !loaded.Display.Fullscreen || loaded.Display.Quality != "low" || loaded.Display.FrameCap != 30 ||
		gameui.SettingsResolutionFrom(loaded) != "1280x720" || loaded.Locale != "zh-Hans" ||
		!loaded.Accessibility.HighContrast || !loaded.Accessibility.ReduceFlashes ||
		!loaded.Accessibility.SFXCaptions || !loaded.Accessibility.Invincibility ||
		!loaded.Accessibility.AutoAim || !loaded.Accessibility.HoldToToggle {
		t.Fatalf("persisted settings mismatch: %+v", loaded)
	}
	if !slices.Contains(loaded.Bindings[actionJump], "gamepad:right_shoulder") {
		t.Fatalf("persisted jump bindings = %v, want gamepad:right_shoulder", loaded.Bindings[actionJump])
	}

	// Start the game with gamepad Start and verify the rebound RB button actually makes the explorer jump!
	pressPad(app, gamepad.ButtonStart)
	app.Advance(250 * time.Millisecond)
	if g.phase.Get() != phasePlaying {
		t.Fatalf("phase after Start = %v, want phasePlaying", g.phase.Get())
	}
	app.SetGamepadButton(1, gamepad.ButtonRB, 1)
	app.Advance(2 * testFrame)
	if g.level.playerBody.VY >= 0 {
		t.Fatalf("rebound jump button (RB) did not trigger jump: VY=%v", g.level.playerBody.VY)
	}
	app.SetGamepadButton(1, gamepad.ButtonRB, 0)
}

func TestSettingsMenuOpensFromTitleAndPauseButtons(t *testing.T) {
	var g *caveRun
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return rootWithHook(cx, func(c *caveRun) { g = c })
	}, nsgotest.Size(960, 540), nsgotest.ManualFrames(), caveRunTestMediaOptions(), nsgotest.Environment(core.Environment{
		OS:    core.OSMacOS,
		Audio: true,
	}))
	app.WaitUntil(2*time.Second, func() bool { return g.levelLoaded.Get() || g.levelErr.Get() != nil })
	if err := g.levelErr.Get(); err != nil {
		t.Fatalf("level group failed to load: %v", err)
	}
	app.Advance(3 * testFrame)

	// Open from title screen button.
	app.Press("id:settings-btn")
	app.Advance(testFrame)
	if !g.settingsOpen.Get() || !app.Exists("id:settings-menu") {
		t.Fatal("expected id:settings-btn on title screen to open settings-menu")
	}
	// Escape closes settings menu cleanly (settings-menu auto-focused on mount).
	app.Key("escape")
	app.Settle()
	if g.settingsOpen.Get() || app.Exists("id:settings-menu") {
		t.Fatal("expected Escape to close settings-menu")
	}

	// Open from pause menu button.
	startPlaying(t, app, g)
	app.Press("id:pause-btn")
	app.Settle()
	app.Press("id:pause-settings-btn")
	app.Settle()
	if !g.settingsOpen.Get() || !app.Exists("id:settings-menu") {
		t.Fatal("expected id:pause-settings-btn in pause menu to open settings-menu")
	}
}

func TestSavedSettingsRestoreOnStartup(t *testing.T) {
	var g *caveRun
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		saved := caveRunDefaultSettings()
		saved.Audio.Master = 0.65
		saved.Audio.Groups[soundGroupMusic] = 0.40
		saved.Display.Quality = "high"
		saved.Display.FrameCap = 60
		saved.Locale = "zh-Hans"
		saved.Accessibility.HighContrast = true
		saved.Accessibility.ReduceMotion = true
		gameui.SetSettingsResolution(&saved, "1920x1080")
		if err := save.NewStorageSettingsStore(cx.Storage(), "caverun").Save(saved); err != nil {
			t.Fatal(err)
		}
		return rootWithHook(cx, func(c *caveRun) { g = c })
	}, nsgotest.Size(960, 540), nsgotest.ManualFrames(), caveRunTestMediaOptions(), nsgotest.Environment(core.Environment{
		OS:    core.OSMacOS,
		Idiom: core.IdiomDesktop,
		Displays: []core.DisplayInfo{
			{ID: "main", Main: true, Frame: core.Bounds{Size: core.Size{Width: 2560, Height: 1440}}},
		},
	}))

	if g == nil {
		t.Fatal("caveRun did not initialize")
	}
	app.WaitUntil(2*time.Second, func() bool { return g.levelLoaded.Get() || g.levelErr.Get() != nil })
	if err := g.levelErr.Get(); err != nil {
		t.Fatalf("level group failed to load: %v", err)
	}
	if math.Abs(float64(g.mixer.Master())-0.65) > 1e-3 || math.Abs(float64(g.mixer.Volume(soundGroupMusic))-0.40) > 1e-3 {
		t.Fatalf("restored audio = master:%v music:%v", g.mixer.Master(), g.mixer.Volume(soundGroupMusic))
	}
	if g.quality.Level() != quality.High || g.loop.DrawRate != 60 || g.resolution != "1920x1080" {
		t.Fatalf("restored display = quality:%v drawRate:%d res:%q", g.quality.Level(), g.loop.DrawRate, g.resolution)
	}
	if g.locale.Get() != "zh-Hans" || !g.a11y.HighContrast || g.level.shakeScale != 0 {
		t.Fatalf("restored locale/a11y = locale:%q highContrast:%v shakeScale:%v", g.locale.Get(), g.a11y.HighContrast, g.level.shakeScale)
	}
	var frames []core.WindowFrame
	for _, r := range app.Window {
		if r.Action == nsgotest.WindowSetFrame {
			frames = append(frames, r.Frame)
		}
	}
	wantFrames := []core.WindowFrame{{X: 320, Y: 180, Move: true, Width: 1920, Height: 1080}}
	if !slices.Equal(frames, wantFrames) {
		t.Fatalf("startup WindowSetFrame requests = %+v, want %+v", frames, wantFrames)
	}
}

func TestSettingsMenuVideoResolutionWindowFrame(t *testing.T) {
	desktopEnv := core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		DisplayID: "main",
		Displays: []core.DisplayInfo{
			{
				ID:           "main",
				Main:         true,
				Frame:        core.Bounds{Size: core.Size{Width: 1920, Height: 1080}},
				VisibleFrame: core.Bounds{Origin: core.Point{Y: 25}, Size: core.Size{Width: 1920, Height: 1055}},
			},
		},
	}
	app, g := startCaveRun(t, nsgotest.Environment(desktopEnv))

	app.Press("id:settings-btn")
	app.Advance(testFrame)
	if !g.settingsOpen.Get() {
		t.Fatal("expected settings menu to open")
	}

	// Switch to Video tab (']' triggers NextTab from Audio -> Video).
	app.Key("]")
	app.Settle()
	if g.settingsMenu.Tab() != gameui.SettingsTabVideo {
		t.Fatalf("tab = %v, want video", g.settingsMenu.Tab())
	}

	// Verify VSync row is absent by default.
	for _, w := range g.settingsMenu.TabControls(gameui.SettingsTabVideo) {
		if tog, ok := w.(*gameui.Toggle); ok && tog.Label == gameui.DefaultSettingsText().VSync {
			t.Fatal("VSync row should be absent by default")
		}
	}

	// Move down from WindowMode [0] to Resolution [1] (starts at 960x540), pick 1280x720, and Apply.
	app.Key("down")
	app.Key("right")
	app.Post(func() { _ = g.settingsMenu.Apply() })
	app.Settle()

	// Step twice more (1920x1080 -> Native) and Apply: restores Cave Run's project window size (960x540).
	app.Key("right")
	app.Key("right")
	app.Post(func() { _ = g.settingsMenu.Apply() })
	app.Settle()

	var frames []core.WindowFrame
	for _, r := range app.Window {
		if r.Action == nsgotest.WindowSetFrame {
			frames = append(frames, r.Frame)
		}
	}
	wantW, wantH := gameProject.WindowSize()
	want := []core.WindowFrame{
		{X: 320, Y: 193, Move: true, Width: 1280, Height: 720},
		{X: 480, Y: 283, Move: true, Width: wantW, Height: wantH},
	}
	if !slices.Equal(frames, want) {
		t.Fatalf("WindowSetFrame requests = %+v, want %+v", frames, want)
	}
}

func TestSettingsMenuHidesResolutionOnPhoneAndWeb(t *testing.T) {
	cases := []struct {
		name      string
		env       core.Environment
		wantCount int
	}{
		{"ios_phone", core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone}, 4},
		{"android_phone", core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}, 4},
		{"web", core.Environment{OS: core.OSWeb, Idiom: core.IdiomDesktop}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, g := startCaveRun(t, nsgotest.Environment(tc.env))
			controls := g.settingsMenu.TabControls(gameui.SettingsTabVideo)
			if len(controls) != tc.wantCount {
				t.Fatalf("%s Video tab controls = %d, want %d", tc.name, len(controls), tc.wantCount)
			}
			for _, w := range controls {
				if btn, ok := w.(*gameui.Button); ok && len(btn.Text) >= len("Resolution") && btn.Text[:len("Resolution")] == "Resolution" {
					t.Fatalf("%s unexpectedly showed Resolution row: %q", tc.name, btn.Text)
				}
			}
			// Applying settings must not send any WindowSetFrame request on phones/web.
			app.Post(func() { _ = g.settingsMenu.Apply() })
			app.Settle()
			for _, r := range app.Window {
				if r.Action == nsgotest.WindowSetFrame {
					t.Fatalf("%s unexpectedly sent WindowSetFrame: %+v", tc.name, r.Frame)
				}
			}
		})
	}
}
