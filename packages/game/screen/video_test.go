package screen_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/game/packages/game/screen"
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
