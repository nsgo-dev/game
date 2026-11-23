package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/quality"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// lowEndAndroid is a budget Android phone as NSGo reports it.
var lowEndAndroid = core.Environment{
	OS:          core.OSAndroid,
	Idiom:       core.IdiomPhone,
	DeviceModel: "Samsung SM-A045F",
	OSVersion:   core.OSVersion{Major: 33},
}

func startStarburst(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *GameController) {
	t.Helper()
	var gc *GameController
	opts = append([]nsgotest.Option{nsgotest.Size(960, 540)}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		gc, node = NewGameView(cx, 4242)
		return node
	}, opts...)
	return app, gc
}

func TestLowEndAndroidStartsOnTheLowPreset(t *testing.T) {
	app, gc := startStarburst(t, nsgotest.Environment(lowEndAndroid))
	low := quality.DefaultPreset(quality.Low)
	if gc.Quality.Level() != quality.Low || !gc.Quality.Auto() || gc.Arena.Quality != low {
		t.Fatalf("started at %v (auto %v) with preset %+v, want low", gc.Quality.Level(), gc.Quality.Auto(), gc.Arena.Quality)
	}
	if gc.Arena.explosions.MaxParticles != 128 || gc.Arena.sparks.MaxParticles != 64 || gc.Arena.thruster.MaxParticles != 40 {
		t.Fatalf("particle budgets %d/%d/%d, want 128/64/40", gc.Arena.explosions.MaxParticles, gc.Arena.sparks.MaxParticles, gc.Arena.thruster.MaxParticles)
	}
	if got := qualityButton(t, app); got != "AUTO: LOW" {
		t.Fatalf("quality button = %q, want AUTO: LOW", got)
	}
	// Two seconds at the target frame rate, as a low-end phone that holds 30 fps runs, keep the low preset.
	for range 60 {
		gc.Quality.Observe(time.Second / 30)
	}
	if gc.Quality.Level() != quality.Low {
		t.Fatalf("30 fps moved the level to %v", gc.Quality.Level())
	}
}

func TestRecentIPhoneStartsHighAndSlowFramesStepItDown(t *testing.T) {
	_, gc := startStarburst(t, nsgotest.Environment(core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone, DeviceModel: "iPhone15,2"}))
	if gc.Quality.Level() != quality.High || gc.Arena.Quality != quality.DefaultPreset(quality.High) {
		t.Fatalf("an iPhone 14 Pro started at %v", gc.Quality.Level())
	}
	// Frames at 20 fps, past warm-up and a window, step down a level and apply its preset to the arena.
	for range 30 + 90 {
		gc.Quality.Observe(time.Second / 20)
	}
	if gc.Quality.Level() != quality.Medium || gc.Arena.Quality != quality.DefaultPreset(quality.Medium) {
		t.Fatalf("after slow frames: level %v, arena preset %+v", gc.Quality.Level(), gc.Arena.Quality)
	}
}

func TestLoopFrameTimesReachTheSelector(t *testing.T) {
	app, gc := startStarburst(t, nsgotest.Environment(lowEndAndroid))
	// nsgotest runs 60 frames a second, well inside the 30 fps target, so auto steps the phone up once it has watched long enough.
	app.Advance(2 * time.Second)
	if gc.Quality.Level() != quality.Low {
		t.Fatalf("stepped up to %v within 2 s", gc.Quality.Level())
	}
	app.Advance(6 * time.Second)
	if gc.Quality.Level() != quality.Medium || gc.Arena.Quality != quality.DefaultPreset(quality.Medium) {
		t.Fatalf("after 8 s of 60 fps: level %v, preset %+v, want medium", gc.Quality.Level(), gc.Arena.Quality)
	}
	if got := qualityButton(t, app); got != "AUTO: MEDIUM" {
		t.Fatalf("quality button = %q, want AUTO: MEDIUM", got)
	}
}

func TestQualityButtonCyclesAndStoresTheSetting(t *testing.T) {
	var gc *GameController
	var store *save.DefaultSettingsStore
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		store = save.NewStorageSettingsStore(cx.Storage(), "starburst")
		var node core.Node
		gc, node = NewGameView(cx, 4242)
		return node
	}, nsgotest.Size(960, 540), nsgotest.Environment(lowEndAndroid))

	for _, want := range []struct{ setting, label string }{
		{"low", "LOW"}, {"medium", "MEDIUM"}, {"high", "HIGH"}, {"auto", "AUTO: LOW"}, {"low", "LOW"},
	} {
		app.Press("btn-quality")
		app.Settle()
		if got := qualityButton(t, app); got != want.label {
			t.Fatalf("button = %q, want %q", got, want.label)
		}
		stored, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if stored.Display.Quality != want.setting || gc.Quality.Setting() != want.setting {
			t.Fatalf("stored %q, selector %q, want %q", stored.Display.Quality, gc.Quality.Setting(), want.setting)
		}
	}
	app.Press("btn-quality")
	app.Settle()
	if gc.Quality.Level() != quality.Medium || gc.Arena.Quality != quality.DefaultPreset(quality.Medium) {
		t.Fatalf("medium setting: level %v, preset %+v", gc.Quality.Level(), gc.Arena.Quality)
	}
	// A fixed setting holds against slow frames.
	for range 1000 {
		gc.Quality.Observe(time.Second / 10)
	}
	if gc.Quality.Level() != quality.Medium {
		t.Fatalf("slow frames moved a fixed setting to %v", gc.Quality.Level())
	}
}

func TestStoredSettingOverridesTheDevice(t *testing.T) {
	var gc *GameController
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := save.DefaultSettings()
		s.Display.Quality = "high"
		if err := save.NewStorageSettingsStore(cx.Storage(), "starburst").Save(s); err != nil {
			t.Fatal(err)
		}
		var node core.Node
		gc, node = NewGameView(cx, 4242)
		return node
	}, nsgotest.Size(960, 540), nsgotest.Environment(lowEndAndroid))
	if gc.Quality.Level() != quality.High || gc.Quality.Auto() || gc.Arena.Quality != quality.DefaultPreset(quality.High) {
		t.Fatalf("stored high on a low-end phone: level %v, auto %v", gc.Quality.Level(), gc.Quality.Auto())
	}
}

// drawOps steps a stress-mode arena at preset p and returns the draw ops of its last frame and its live particles.
func drawOps(p quality.Preset) (ops, particles int) {
	a := NewArena(2026)
	a.ApplyQuality(p)
	a.SetStressMode(true)
	d := core.Drawing{}
	for range 120 {
		a.Step(FixedDT)
	}
	a.DrawWorld(&d)
	return len(d.Ops), a.ActiveParticles()
}

func TestLowPresetDrawsLess(t *testing.T) {
	highOps, highParticles := drawOps(quality.DefaultPreset(quality.High))
	lowOps, lowParticles := drawOps(quality.DefaultPreset(quality.Low))
	if lowOps >= highOps || lowParticles >= highParticles {
		t.Fatalf("low draws %d ops and %d particles, high %d and %d; want fewer at low", lowOps, lowParticles, highOps, highParticles)
	}
	if lowParticles > 128+64+40 {
		t.Fatalf("low runs %d particles, past its budgets", lowParticles)
	}
}

func BenchmarkArenaPresets(b *testing.B) {
	for _, level := range quality.Levels() {
		b.Run(level.String(), func(b *testing.B) {
			a := NewArena(2026)
			a.ApplyQuality(quality.DefaultPreset(level))
			a.SetStressMode(true)
			d := core.Drawing{Ops: make([]core.DrawOp, 0, 8192)}
			for range 20 {
				a.Step(FixedDT)
				d.Ops = d.Ops[:0]
				a.DrawWorld(&d)
			}
			b.ReportAllocs()
			for b.Loop() {
				a.Step(FixedDT)
				d.Ops = d.Ops[:0]
				a.DrawWorld(&d)
			}
			b.ReportMetric(float64(len(d.Ops)), "ops")
		})
	}
}

// qualityButton returns the quality button's label, the text of its one child.
func qualityButton(t *testing.T, app *nsgotest.App) string {
	t.Helper()
	btn := app.Find("btn-quality")
	if len(btn.Children) != 1 {
		t.Fatalf("quality button has %d children, want its label", len(btn.Children))
	}
	return btn.Children[0].Text
}
