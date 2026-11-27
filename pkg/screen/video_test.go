package screen_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// fullScreenRequest is one full-screen request a Video sent to its window.
type fullScreenRequest struct {
	On      bool
	Display string
}

// recordFullScreen records the full-screen requests sent until the test ends.
func recordFullScreen(t *testing.T) *[]fullScreenRequest {
	t.Helper()
	var got []fullScreenRequest
	t.Cleanup(screen.SetFullScreenHook(func(_ *core.Scope, on bool, displayID string) {
		got = append(got, fullScreenRequest{On: on, Display: displayID})
	}))
	return &got
}

var twoDisplays = core.Environment{Displays: []core.DisplayInfo{
	{ID: "main", Main: true, Frame: core.Bounds{Size: core.Size{Width: 1920, Height: 1080}}},
	{ID: "side", Frame: core.Bounds{Origin: core.Point{X: 1920}, Size: core.Size{Width: 2560, Height: 1440}}},
}}

// startVideo runs v and l in an app's root view with two displays.
func startVideo(t *testing.T, v *screen.Video, l *game.Loop) *nsgotest.App {
	t.Helper()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		if l != nil {
			l.Run(cx)
		}
		v.Run(cx)
		return core.View{}
	})
	app.SetEnvironment(twoDisplays)
	return app
}

func apply(app *nsgotest.App, v *screen.Video, s save.DisplaySettings) {
	app.Post(func() { _ = v.Apply(s) })
	app.Settle()
}

func TestVideoMovesTheWindowWhenFullScreenOrDisplayChanges(t *testing.T) {
	got := recordFullScreen(t)
	v := &screen.Video{}
	app := startVideo(t, v, nil)
	s := save.DefaultSettings().Display

	apply(app, v, s)
	if len(*got) != 0 {
		t.Fatalf("windowed settings on a windowed window sent %v, want nothing", *got)
	}
	s.Fullscreen = true
	s.DisplayID = "side"
	apply(app, v, s)
	s.DisplayID = "main"
	apply(app, v, s)
	apply(app, v, s) // unchanged: no request
	s.Fullscreen = false
	apply(app, v, s)
	want := []fullScreenRequest{{true, "side"}, {true, "main"}, {false, ""}}
	if !slices.Equal(*got, want) {
		t.Fatalf("requests %v, want %v", *got, want)
	}
}

func TestVideoLeavesAFullScreenWindowOnFirstApply(t *testing.T) {
	got := recordFullScreen(t)
	v := &screen.Video{}
	app := startVideo(t, v, nil)
	env := twoDisplays
	env.FullScreen = true
	app.SetEnvironment(env)
	apply(app, v, save.DefaultSettings().Display)
	if want := []fullScreenRequest{{false, ""}}; !slices.Equal(*got, want) {
		t.Fatalf("requests %v, want %v", *got, want)
	}
}

func TestVideoKeepsAFullScreenWindowOnFirstApply(t *testing.T) {
	got := recordFullScreen(t)
	v := &screen.Video{}
	app := startVideo(t, v, nil)
	env := twoDisplays
	env.FullScreen = true
	app.SetEnvironment(env)
	s := save.DefaultSettings().Display
	s.Fullscreen, s.DisplayIndex = true, 5 // no display to move to
	apply(app, v, s)
	if len(*got) != 0 {
		t.Fatalf("full-screen settings on a full-screen window sent %v, want nothing", *got)
	}
}

func TestVideoReappliesTheWindowWhenRunAfterStop(t *testing.T) {
	got := recordFullScreen(t)
	v := &screen.Video{}
	var root *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		root = cx
		v.Run(cx)
		return core.View{}
	}, nsgotest.Environment(twoDisplays))
	app.Settle()
	s := save.DefaultSettings().Display
	s.Fullscreen = true
	apply(app, v, s)
	if want := []fullScreenRequest{{true, "main"}}; !slices.Equal(*got, want) {
		t.Fatalf("requests %v, want %v", *got, want)
	}
	// The player leaves full screen with the system's own controls while the video is stopped, so the next Run restores the settings.
	app.SetEnvironment(twoDisplays)
	app.Post(func() { v.Run(root) })
	app.Settle()
	if want := []fullScreenRequest{{true, "main"}, {true, "main"}}; !slices.Equal(*got, want) {
		t.Fatalf("requests after Run again %v, want %v", *got, want)
	}
}

func TestVideoSettingsAreRestoredOnTheNextLaunch(t *testing.T) {
	dir := t.TempDir()
	settings := save.DefaultSettings()
	settings.Display.Fullscreen = true
	settings.Display.DisplayID = "side"
	settings.Display.FrameCap = 45
	if err := save.NewFileSettingsStore(dir).Save(settings); err != nil {
		t.Fatal(err)
	}

	got := recordFullScreen(t)
	l := &game.Loop{}
	v := &screen.Video{Loop: l}
	var loadErr error
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		v.Run(cx)
		_, loadErr = save.NewFileSettingsStore(dir).LoadAndApply(save.SettingsTargets{ApplyDisplay: v.Apply})
		return core.View{}
	}, nsgotest.Environment(twoDisplays))
	app.Settle()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if want := []fullScreenRequest{{true, "side"}}; !slices.Equal(*got, want) {
		t.Fatalf("requests at launch %v, want %v", *got, want)
	}
	if l.DrawRate != 45 {
		t.Fatalf("DrawRate %d at launch, want 45", l.DrawRate)
	}
}

func TestVideoSettingsAppliedBeforeRunTakeEffectWhenItStarts(t *testing.T) {
	got := recordFullScreen(t)
	l := &game.Loop{}
	v := &screen.Video{Loop: l}
	s := save.DefaultSettings().Display
	s.Fullscreen, s.DisplayIndex, s.FrameCap = true, 1, 20
	if err := v.Apply(s); err != nil {
		t.Fatal(err)
	}
	if l.DrawRate != 20 {
		t.Fatalf("DrawRate %d, want 20", l.DrawRate)
	}
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v.Run(cx)
		return core.View{}
	}, nsgotest.Environment(twoDisplays))
	if want := []fullScreenRequest{{true, "side"}}; !slices.Equal(*got, want) {
		t.Fatalf("requests %v, want %v", *got, want)
	}
}

func TestDisplayIDPrefersAConnectedIDThenTheIndex(t *testing.T) {
	displays := twoDisplays.Displays
	for _, tc := range []struct {
		id    string
		index int
		want  string
	}{
		{"side", 0, "side"},
		{"gone", 1, "side"},
		{"", 0, "main"},
		{"gone", 5, ""},
		{"", -1, ""},
	} {
		s := save.DisplaySettings{DisplayID: tc.id, DisplayIndex: tc.index}
		if got := screen.DisplayID(s, displays); got != tc.want {
			t.Errorf("DisplayID(%q, %d) = %q, want %q", tc.id, tc.index, got, tc.want)
		}
	}
	if got := screen.DisplayID(save.DisplaySettings{DisplayID: "side"}, nil); got != "" {
		t.Errorf("DisplayID without displays = %q, want \"\"", got)
	}
}

// drawsPerSecond counts l's draws over a second of display frames.
func drawsPerSecond(app *nsgotest.App, draws *int) int {
	before := *draws
	app.Advance(time.Second)
	return *draws - before
}

func TestFrameCapChangesTheDrawingRate(t *testing.T) {
	draws := 0
	l := &game.Loop{Draw: func(float64) { draws++ }}
	v := &screen.Video{Loop: l}
	app := startVideo(t, v, l)
	s := save.DefaultSettings().Display

	// nsgotest runs a display frame every 16.667 ms, 60 a second.
	for _, tc := range []struct{ cap, want int }{{0, 60}, {30, 30}, {20, 20}, {120, 60}, {0, 60}} {
		s.FrameCap = tc.cap
		apply(app, v, s)
		drawsPerSecond(app, &draws) // let the cap's pace settle
		if got := drawsPerSecond(app, &draws); got != tc.want {
			t.Fatalf("cap %d: %d draws a second, want %d", tc.cap, got, tc.want)
		}
	}
}

func TestAutoDropLowersTheCapWhileConstrainedAndRestoresIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  func(*core.Environment)
	}{
		{"low power", func(e *core.Environment) { e.LowPowerMode = core.LowPowerOn }},
		{"serious thermal", func(e *core.Environment) { e.ThermalState = core.ThermalSerious }},
		{"critical thermal", func(e *core.Environment) { e.ThermalState = core.ThermalCritical }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draws := 0
			l := &game.Loop{Draw: func(float64) { draws++ }}
			v := &screen.Video{Loop: l}
			app := startVideo(t, v, l)
			apply(app, v, save.DefaultSettings().Display)
			if got := drawsPerSecond(app, &draws); got != 60 {
				t.Fatalf("%d draws a second before, want 60", got)
			}

			env := twoDisplays
			tc.env(&env)
			app.SetEnvironment(env)
			if !v.Dropped() || v.FrameCap() != screen.DefaultLowPowerCap || l.DrawRate != screen.DefaultLowPowerCap {
				t.Fatalf("constrained: Dropped %v, FrameCap %d, DrawRate %d", v.Dropped(), v.FrameCap(), l.DrawRate)
			}
			drawsPerSecond(app, &draws)
			if got := drawsPerSecond(app, &draws); got != 30 {
				t.Fatalf("%d draws a second while constrained, want 30", got)
			}

			app.SetEnvironment(twoDisplays)
			if v.Dropped() || l.DrawRate != 0 {
				t.Fatalf("after: Dropped %v, DrawRate %d", v.Dropped(), l.DrawRate)
			}
			drawsPerSecond(app, &draws)
			if got := drawsPerSecond(app, &draws); got != 60 {
				t.Fatalf("%d draws a second after, want 60", got)
			}
		})
	}
}

func TestAutoDropKeepsALowerCapAndHonorsTheSetting(t *testing.T) {
	l := &game.Loop{}
	v := &screen.Video{Loop: l, LowPowerCap: 24}
	app := startVideo(t, v, l)
	s := save.DefaultSettings().Display
	env := twoDisplays
	env.LowPowerMode = core.LowPowerOn
	env.ThermalState = core.ThermalFair
	app.SetEnvironment(env)

	for _, tc := range []struct {
		cap  int
		auto bool
		want int
	}{
		{0, true, 24},
		{60, true, 24},
		{15, true, 15},
		{60, false, 60},
		{0, false, 0},
	} {
		s.FrameCap, s.AutoDropLowPower = tc.cap, tc.auto
		apply(app, v, s)
		if l.DrawRate != tc.want || v.FrameCap() != tc.want {
			t.Errorf("cap %d auto %v: DrawRate %d, FrameCap %d, want %d", tc.cap, tc.auto, l.DrawRate, v.FrameCap(), tc.want)
		}
	}
}

func TestConstrained(t *testing.T) {
	for _, lp := range []core.LowPowerMode{core.LowPowerUnknown, core.LowPowerOff, core.LowPowerOn} {
		for _, th := range []core.ThermalState{core.ThermalUnknown, core.ThermalNominal, core.ThermalFair, core.ThermalSerious, core.ThermalCritical} {
			want := lp == core.LowPowerOn || th == core.ThermalSerious || th == core.ThermalCritical
			if got := screen.Constrained(lp, th); got != want {
				t.Errorf("Constrained(%v, %v) = %v, want %v", lp, th, got, want)
			}
		}
	}
}

func TestStopReleasesTheDrop(t *testing.T) {
	l := &game.Loop{}
	v := &screen.Video{Loop: l}
	var stop func()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stop = v.Run(cx)
		return core.View{}
	}, nsgotest.Environment(core.Environment{LowPowerMode: core.LowPowerOn}))
	apply(app, v, save.DefaultSettings().Display)
	if l.DrawRate != screen.DefaultLowPowerCap {
		t.Fatalf("DrawRate %d, want %d", l.DrawRate, screen.DefaultLowPowerCap)
	}
	app.Post(stop)
	app.Settle()
	if v.Dropped() || l.DrawRate != 0 {
		t.Fatalf("after stop: Dropped %v, DrawRate %d", v.Dropped(), l.DrawRate)
	}
}

func TestDisposingTheOwnerReleasesTheDropAndTheWindow(t *testing.T) {
	got := recordFullScreen(t)
	l := &game.Loop{}
	v := &screen.Video{Loop: l}
	var root *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		root = cx
		return core.View{}
	}, nsgotest.Environment(core.Environment{LowPowerMode: core.LowPowerOn}))
	// Run outside a computation, so the child scope is the owner that disposal releases.
	var owner *core.Scope
	app.Post(func() {
		owner = root.Child()
		v.Run(owner)
	})
	app.Settle()
	apply(app, v, save.DefaultSettings().Display)
	if l.DrawRate != screen.DefaultLowPowerCap {
		t.Fatalf("DrawRate %d, want %d", l.DrawRate, screen.DefaultLowPowerCap)
	}
	app.Post(owner.Dispose)
	app.Settle()
	if v.Dropped() || l.DrawRate != 0 {
		t.Fatalf("after dispose: Dropped %v, DrawRate %d", v.Dropped(), l.DrawRate)
	}
	s := save.DefaultSettings().Display
	s.Fullscreen = true
	apply(app, v, s)
	if len(*got) != 0 {
		t.Fatalf("Apply after dispose sent %v, want nothing", *got)
	}
}

func TestDroppedUpdatesTheComputationsReadingIt(t *testing.T) {
	v := &screen.Video{}
	var seen []bool
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v.Run(cx)
		cx.Effect(func() { seen = append(seen, v.Dropped()) })
		return core.View{}
	}, nsgotest.Environment(twoDisplays))
	apply(app, v, save.DefaultSettings().Display)
	env := twoDisplays
	env.ThermalState = core.ThermalCritical
	app.SetEnvironment(env)
	app.SetEnvironment(twoDisplays)
	if len(seen) == 0 || seen[len(seen)-1] {
		t.Fatalf("Dropped seen %v, want it to end false", seen)
	}
	if !slices.Contains(seen, true) {
		t.Fatalf("Dropped seen %v, want true while constrained", seen)
	}
}

func ExampleVideo() {
	l := &game.Loop{}
	v := &screen.Video{Loop: l}
	s := save.DefaultSettings().Display
	s.FrameCap = 30
	_ = v.Apply(s)
	fmt.Println(l.DrawRate, v.FrameCap())
	// Output: 30 30
}

func TestVideoDefersSavedFullScreenOnWebUntilFirstPress(t *testing.T) {
	webEnv := core.Environment{
		OS:       core.OSWeb,
		Idiom:    core.IdiomDesktop,
		Displays: twoDisplays.Displays,
	}

	t.Run("Attach sends on first press", func(t *testing.T) {
		got := recordFullScreen(t)
		v := &screen.Video{}
		saved := save.DefaultSettings().Display
		saved.Fullscreen = true
		saved.DisplayID = "main"

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			v.Run(cx)
			_ = v.Apply(saved)
			view := core.View{
				TestID: "game",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
			}
			v.Attach(&view)
			return view
		}, nsgotest.Environment(webEnv))

		if len(*got) != 0 {
			t.Fatalf("saved fullscreen on web requested at start: %v, want nothing", *got)
		}
		if !v.PendingFullScreen() {
			t.Fatal("PendingFullScreen() = false at start on web, want true")
		}

		app.Press("id:game")
		if want := []fullScreenRequest{{true, "main"}}; !slices.Equal(*got, want) {
			t.Fatalf("requests after first press = %v, want %v", *got, want)
		}
		if v.PendingFullScreen() {
			t.Fatal("PendingFullScreen() = true after first press, want false")
		}

		// A second press does not send another request.
		app.Press("id:game")
		if len(*got) != 1 {
			t.Fatalf("requests after second press = %v, want 1", *got)
		}
	})

	t.Run("ApplyPending in press handler sends on first press", func(t *testing.T) {
		got := recordFullScreen(t)
		v := &screen.Video{}
		saved := save.DefaultSettings().Display
		saved.Fullscreen = true
		// Apply before Run to verify startup deferral in that order too.
		_ = v.Apply(saved)

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			v.Run(cx)
			return core.View{
				TestID:  "play",
				Style:   core.Style{Width: core.Pt(120), Height: core.Pt(40)},
				OnPress: func(core.PressEvent) { v.ApplyPending() },
			}
		}, nsgotest.Environment(webEnv))

		if len(*got) != 0 {
			t.Fatalf("requests at start = %v, want nothing", *got)
		}
		if !v.PendingFullScreen() {
			t.Fatal("PendingFullScreen() = false, want true")
		}

		app.Press("id:play")
		if want := []fullScreenRequest{{true, "main"}}; !slices.Equal(*got, want) {
			t.Fatalf("requests after press = %v, want %v", *got, want)
		}
	})

	t.Run("Attach sends on first key press", func(t *testing.T) {
		got := recordFullScreen(t)
		v := &screen.Video{}
		saved := save.DefaultSettings().Display
		saved.Fullscreen = true

		var ref core.Ref
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			v.Run(cx)
			_ = v.Apply(saved)
			view := core.View{
				TestID:    "game",
				Ref:       &ref,
				Focusable: func() bool { return true },
				Style:     core.Style{Width: core.Pt(200), Height: core.Pt(100)},
			}
			v.Attach(&view)
			return view
		}, nsgotest.Environment(webEnv))
		app.Post(ref.Focus)
		app.Settle()

		if len(*got) != 0 {
			t.Fatalf("requests at start = %v, want nothing", *got)
		}
		app.Key("a")
		if want := []fullScreenRequest{{true, "main"}}; !slices.Equal(*got, want) {
			t.Fatalf("requests after key press = %v, want %v", *got, want)
		}
	})
}

func TestVideoWebWindowedApplyClearsPendingFullScreen(t *testing.T) {
	got := recordFullScreen(t)
	v := &screen.Video{}
	webEnv := core.Environment{OS: core.OSWeb, Idiom: core.IdiomDesktop, Displays: twoDisplays.Displays}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v.Run(cx)
		s := save.DefaultSettings().Display
		s.Fullscreen = true
		_ = v.Apply(s)
		view := core.View{
			TestID: "game",
			Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
		}
		v.Attach(&view)
		return view
	}, nsgotest.Environment(webEnv))

	if !v.PendingFullScreen() {
		t.Fatal("expected PendingFullScreen() == true at start")
	}
	apply(app, v, save.DefaultSettings().Display) // Fullscreen: false
	if v.PendingFullScreen() {
		t.Fatal("expected windowed Apply to clear PendingFullScreen")
	}
	app.Press("id:game")
	if len(*got) != 0 {
		t.Fatalf("expected no fullscreen request after clearing pending, got %v", *got)
	}
}

func TestVideoTracksExternalFullScreenChangesAndCallsOnWindowChanged(t *testing.T) {
	got := recordFullScreen(t)
	var changes []save.DisplaySettings
	v := &screen.Video{
		OnWindowChanged: func(d save.DisplaySettings) {
			changes = append(changes, d)
		},
	}
	webEnv := core.Environment{
		OS:       core.OSWeb,
		Idiom:    core.IdiomDesktop,
		Displays: twoDisplays.Displays,
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v.Run(cx)
		return core.View{}
	}, nsgotest.Environment(webEnv))

	// Apply windowed first, then enter full screen via a normal Apply.
	s := save.DefaultSettings().Display
	apply(app, v, s)
	s.Fullscreen = true
	apply(app, v, s)
	if want := []fullScreenRequest{{true, "main"}}; !slices.Equal(*got, want) {
		t.Fatalf("requests = %v, want %v", *got, want)
	}

	// Browser confirms entering full screen: our own request, so OnWindowChanged does not fire.
	fullEnv := webEnv
	fullEnv.FullScreen = true
	app.SetEnvironment(fullEnv)
	if len(changes) != 0 {
		t.Fatalf("Video's own fullscreen entry fired OnWindowChanged: %+v", changes)
	}

	// Player leaves full screen outside the game (Escape / browser chrome).
	app.SetEnvironment(webEnv)
	if v.Settings().Fullscreen {
		t.Fatal("Video.Settings().Fullscreen = true after external exit, want false")
	}
	if len(changes) != 1 || changes[0].Fullscreen {
		t.Fatalf("OnWindowChanged = %+v, want single change with Fullscreen=false", changes)
	}

	// Re-applying Fullscreen: true requests full screen again in one shot (no stale lastFull).
	apply(app, v, s)
	if want := []fullScreenRequest{{true, "main"}, {true, "main"}}; !slices.Equal(*got, want) {
		t.Fatalf("requests after re-enter = %v, want %v", *got, want)
	}

	// Browser confirms entry again; then external entry from windowed updates settings to true.
	app.SetEnvironment(fullEnv)
	s.Fullscreen = false
	apply(app, v, s)
	app.SetEnvironment(webEnv)
	if len(changes) != 1 {
		t.Fatalf("Video's own fullscreen exit fired OnWindowChanged: %+v", changes)
	}
	app.SetEnvironment(fullEnv)
	if !v.Settings().Fullscreen {
		t.Fatal("Video.Settings().Fullscreen = false after external entry, want true")
	}
	if len(changes) != 2 || !changes[1].Fullscreen {
		t.Fatalf("OnWindowChanged = %+v, want second change with Fullscreen=true", changes)
	}
}

func TestVideoWebMenuToggleEntersFullScreenInOnePress(t *testing.T) {
	got := recordFullScreen(t)
	webEnv := core.Environment{
		OS:       core.OSWeb,
		Idiom:    core.IdiomDesktop,
		Displays: twoDisplays.Displays,
	}

	var (
		v    *screen.Video
		menu *gameui.SettingsMenu
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		store := save.NewStorageSettingsStore(cx.Storage(), "web-fs-test")
		saved := save.DefaultSettings()
		saved.Display.Fullscreen = true
		if err := store.Save(saved); err != nil {
			t.Fatal(err)
		}

		v = &screen.Video{
			OnWindowChanged: func(d save.DisplaySettings) {
				if menu == nil {
					return
				}
				cur := menu.Settings()
				cur.Display = d
				menu.SetSettings(cur)
			},
		}
		v.Run(cx)
		if _, err := store.LoadAndApply(save.SettingsTargets{ApplyDisplay: v.Apply}); err != nil {
			t.Fatal(err)
		}

		menu = gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
			Store:     store,
			Targets:   save.SettingsTargets{ApplyDisplay: v.Apply},
			Env:       cx.Env(),
			AutoApply: true,
		})
		menu.SetTab(gameui.SettingsTabVideo)
		menu.Layout(core.Bounds{Size: core.Size{Width: 960, Height: 540}})

		return core.View{
			TestID: "menu-host",
			Style:  core.Style{Width: core.Pt(960), Height: core.Pt(540)},
			OnPressDown: func(ev core.PressEvent) {
				menu.OnPointerDown(ev.Position)
			},
			OnPressUp: func(ev core.PressEvent) {
				menu.OnPointerUp(ev.Position)
			},
		}
	}, nsgotest.Size(960, 540), nsgotest.Environment(webEnv))

	// At start on the web, nothing was requested and the menu reports Windowed.
	if len(*got) != 0 {
		t.Fatalf("requests at start = %v, want nothing", *got)
	}
	if menu.Settings().Display.Fullscreen {
		t.Fatal("menu reported Fullscreen=true on windowed web page")
	}

	// Pressing the Window Mode toggle once enters full screen immediately.
	winBtn := menu.TabControls(gameui.SettingsTabVideo)[0]
	b := winBtn.Bounds()
	center := core.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
	app.PressAt("id:menu-host", center)
	if want := []fullScreenRequest{{true, "main"}}; !slices.Equal(*got, want) {
		t.Fatalf("requests after one menu toggle = %v, want %v", *got, want)
	}
	if !menu.Settings().Display.Fullscreen {
		t.Fatal("menu Fullscreen = false after toggle, want true")
	}

	// Browser enters full screen, then player exits via Escape.
	fullEnv := webEnv
	fullEnv.FullScreen = true
	app.SetEnvironment(fullEnv)
	app.SetEnvironment(webEnv)
	if v.Settings().Fullscreen {
		t.Fatal("Video.Settings().Fullscreen = true after Escape, want false")
	}
	if menu.Settings().Display.Fullscreen {
		t.Fatal("menu Fullscreen = true after Escape, want false")
	}

	// Pressing Window Mode once enters full screen again.
	app.PressAt("id:menu-host", center)
	if want := []fullScreenRequest{{true, "main"}, {true, "main"}}; !slices.Equal(*got, want) {
		t.Fatalf("requests after second menu toggle = %v, want %v", *got, want)
	}
}

func TestCanFullScreen(t *testing.T) {
	if !screen.CanFullScreen(nil) {
		t.Fatal("CanFullScreen(nil) = false, want true")
	}
	cases := []struct {
		name string
		env  core.Environment
		want bool
	}{
		{"unset OS", core.Environment{}, true},
		{"macos", core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}, true},
		{"windows", core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop}, true},
		{"linux", core.Environment{OS: core.OSLinux, Idiom: core.IdiomDesktop}, true},
		{"web desktop", core.Environment{OS: core.OSWeb, Idiom: core.IdiomDesktop}, true},
		{"web tablet", core.Environment{OS: core.OSWeb, Idiom: core.IdiomTablet}, true},
		{"web phone", core.Environment{OS: core.OSWeb, Idiom: core.IdiomPhone}, false},
		{"ios phone", core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone}, false},
		{"android phone", core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			nsgotest.Start(t, func(cx *core.Scope) core.Node {
				got = screen.CanFullScreen(cx.Env())
				return core.View{}
			}, nsgotest.Environment(tc.env))
			if got != tc.want {
				t.Fatalf("CanFullScreen(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func frameRequests(app *nsgotest.App) []core.WindowFrame {
	var out []core.WindowFrame
	for _, r := range app.Window {
		if r.Action == nsgotest.WindowSetFrame {
			out = append(out, r.Frame)
		}
	}
	return out
}

func TestSupportsWindowResolution(t *testing.T) {
	if screen.SupportsWindowResolution(nil) {
		t.Fatal("SupportsWindowResolution(nil) = true, want false")
	}
	cases := []struct {
		name string
		env  core.Environment
		want bool
	}{
		{"macos desktop", core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}, true},
		{"windows desktop", core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop}, true},
		{"linux desktop", core.Environment{OS: core.OSLinux, Idiom: core.IdiomDesktop}, true},
		{"macos omitted idiom", core.Environment{OS: core.OSMacOS}, true},
		{"ios phone", core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone}, false},
		{"ios tablet", core.Environment{OS: core.OSIOS, Idiom: core.IdiomTablet}, false},
		{"android phone", core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}, false},
		{"web desktop", core.Environment{OS: core.OSWeb, Idiom: core.IdiomDesktop}, false},
		{"simulated macos on web", core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Simulated: true}, false},
		{"headless desktop idiom", core.Environment{Idiom: core.IdiomDesktop}, true},
		{"headless mac idiom", core.Environment{Idiom: core.IdiomMac}, true},
		{"headless phone idiom", core.Environment{Idiom: core.IdiomPhone}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			nsgotest.Start(t, func(cx *core.Scope) core.Node {
				got = screen.SupportsWindowResolution(cx.Env())
				return core.View{}
			}, nsgotest.Environment(tc.env))
			if got != tc.want {
				t.Fatalf("SupportsWindowResolution(%+v) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestVideoAppliesWindowedResolutionCenteredOnCurrentDisplay(t *testing.T) {
	desktopEnv := core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		DisplayID: "side",
		Displays: []core.DisplayInfo{
			{ID: "main", Main: true, Frame: core.Bounds{Size: core.Size{Width: 1920, Height: 1080}}},
			{
				ID:           "side",
				Frame:        core.Bounds{Origin: core.Point{X: 1920}, Size: core.Size{Width: 2560, Height: 1440}},
				VisibleFrame: core.Bounds{Origin: core.Point{X: 1920, Y: 25}, Size: core.Size{Width: 2560, Height: 1415}},
			},
		},
	}

	v := &screen.Video{NativeWindowSize: core.Size{Width: 960, Height: 540}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v.Run(cx)
		return core.View{}
	}, nsgotest.Size(960, 540), nsgotest.Environment(desktopEnv))

	// Applying the starting size (960x540) sends nothing.
	app.Post(func() { v.ApplyResolution(960, 540) })
	app.Settle()
	if got := frameRequests(app); len(got) != 0 {
		t.Fatalf("initial matching resolution sent %+v, want nothing", got)
	}

	// Picking 1280x720 centers on the current display's VisibleFrame.
	app.Post(func() { v.ApplyResolution(1280, 720) })
	app.Settle()
	// Re-applying the same resolution is deduplicated.
	app.Post(func() { v.ApplyResolution(1280, 720) })
	app.Settle()
	// Picking Native (0, 0) restores NativeWindowSize (960x540).
	app.Post(func() { v.ApplyResolution(0, 0) })
	app.Settle()
	if v.Resolution() != (core.Size{}) {
		t.Fatalf("Resolution() after native = %+v, want zero", v.Resolution())
	}

	want := []core.WindowFrame{
		{X: 2560, Y: 373, Move: true, Width: 1280, Height: 720},
		{X: 2720, Y: 463, Move: true, Width: 960, Height: 540},
	}
	if got := frameRequests(app); !slices.Equal(got, want) {
		t.Fatalf("window frame requests = %+v, want %+v", got, want)
	}
}

func TestVideoNativeRestoresInitialWindowSizeAndClampsOversizedOrigin(t *testing.T) {
	desktopEnv := core.Environment{
		OS:    core.OSWindows,
		Idiom: core.IdiomDesktop,
		Displays: []core.DisplayInfo{
			{ID: "main", Main: true, Frame: core.Bounds{Origin: core.Point{X: 10, Y: 20}, Size: core.Size{Width: 1440, Height: 900}}},
		},
	}

	v := &screen.Video{} // NativeWindowSize omitted -> captures initial 960x540 on Run.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v.Run(cx)
		return core.View{}
	}, nsgotest.Size(960, 540), nsgotest.Environment(desktopEnv))

	// 1920x1080 exceeds 1440x900: origin clamps to display origin (10, 20) instead of going negative.
	app.Post(func() { v.ApplyResolution(1920, 1080) })
	app.Settle()
	app.Post(func() { v.ApplyResolution(0, 0) })
	app.Settle()

	want := []core.WindowFrame{
		{X: 10, Y: 20, Move: true, Width: 1920, Height: 1080},
		{X: 250, Y: 200, Move: true, Width: 960, Height: 540},
	}
	if got := frameRequests(app); !slices.Equal(got, want) {
		t.Fatalf("window frame requests = %+v, want %+v", got, want)
	}
}

func TestVideoDefersResolutionWhileFullscreenAndAppliesWhenWindowed(t *testing.T) {
	gotFull := recordFullScreen(t)
	desktopEnv := core.Environment{
		OS:    core.OSLinux,
		Idiom: core.IdiomDesktop,
		Displays: []core.DisplayInfo{
			{ID: "main", Main: true, Frame: core.Bounds{Size: core.Size{Width: 1920, Height: 1080}}},
		},
	}
	v := &screen.Video{NativeWindowSize: core.Size{Width: 960, Height: 540}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v.Run(cx)
		return core.View{}
	}, nsgotest.Size(960, 540), nsgotest.Environment(desktopEnv))

	s := save.DefaultSettings().Display
	s.Fullscreen = true
	app.Post(func() {
		_ = v.Apply(s)
		v.ApplyResolution(1280, 720)
	})
	app.Settle()
	if got := frameRequests(app); len(got) != 0 {
		t.Fatalf("fullscreen ApplyResolution sent %+v, want nothing", got)
	}

	// Returning to windowed applies the remembered 1280x720 resolution.
	s.Fullscreen = false
	apply(app, v, s)
	wantFull := []fullScreenRequest{{true, "main"}, {false, ""}}
	if !slices.Equal(*gotFull, wantFull) {
		t.Fatalf("fullscreen requests = %+v, want %+v", *gotFull, wantFull)
	}
	wantFrames := []core.WindowFrame{
		{X: 320, Y: 180, Move: true, Width: 1280, Height: 720},
	}
	if got := frameRequests(app); !slices.Equal(got, wantFrames) {
		t.Fatalf("window frame requests after leaving fullscreen = %+v, want %+v", got, wantFrames)
	}
}

func TestVideoIgnoresWindowResolutionOnMobileAndWeb(t *testing.T) {
	envs := []struct {
		name string
		env  core.Environment
	}{
		{"ios", core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone}},
		{"android", core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}},
		{"web", core.Environment{OS: core.OSWeb, Idiom: core.IdiomDesktop}},
		{"simulated_macos", core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Simulated: true}},
	}
	for _, tc := range envs {
		t.Run(tc.name, func(t *testing.T) {
			v := &screen.Video{NativeWindowSize: core.Size{Width: 960, Height: 540}}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				v.Run(cx)
				return core.View{}
			}, nsgotest.Size(960, 540), nsgotest.Environment(tc.env))
			app.Post(func() { v.ApplyResolution(1280, 720) })
			app.Settle()
			if got := frameRequests(app); len(got) != 0 {
				t.Fatalf("%s sent window frames %+v, want nothing", tc.name, got)
			}
		})
	}
}

func TestVideoAppliesResolutionSetBeforeRun(t *testing.T) {
	v := &screen.Video{NativeWindowSize: core.Size{Width: 960, Height: 540}}
	v.ApplyResolution(1280, 720)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v.Run(cx)
		return core.View{}
	}, nsgotest.Size(960, 540), nsgotest.Environment(core.Environment{
		OS:    core.OSMacOS,
		Idiom: core.IdiomDesktop,
	}))
	want := []core.WindowFrame{{Width: 1280, Height: 720}}
	if got := frameRequests(app); !slices.Equal(got, want) {
		t.Fatalf("frames before Run = %+v, want %+v", got, want)
	}
}
