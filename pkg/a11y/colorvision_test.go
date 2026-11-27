package a11y_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/effects"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestColorVisionFilterFromSettingsAndAssist(t *testing.T) {
	// Default / none returns no filter.
	if _, ok := a11y.ColorVisionFilter(save.DefaultSettings().Accessibility); ok {
		t.Fatal("expected ColorVisionFilter(default) to return false")
	}
	if _, ok := a11y.ColorVisionFilter(save.AccessibilitySettings{ColorBlindMode: "normal"}); ok {
		t.Fatal("expected ColorVisionFilter(normal) to return false")
	}
	if _, ok := a11y.ColorVisionFilter(save.AccessibilitySettings{
		ColorBlindMode:        "protanopia",
		ColorBlindSeverity:    0,
		ColorBlindSeveritySet: true,
	}); ok {
		t.Fatal("expected ColorVisionFilter with explicit 0 severity to return false")
	}

	// Unset severity defaults to 1.0 when a deficiency mode is chosen.
	eff, ok := a11y.ColorVisionFilter(save.AccessibilitySettings{ColorBlindMode: "deuteranopia"})
	if !ok {
		t.Fatal("expected ColorVisionFilter(deuteranopia) to return true")
	}
	chain := effects.MustCompile(eff)
	frame := image.NewRGBA(image.Rect(0, 0, 2, 1))
	frame.SetRGBA(0, 0, color.RGBA{0xCC, 0x22, 0x22, 255})
	frame.SetRGBA(1, 0, color.RGBA{0x22, 0xAA, 0x22, 255})
	got, err := chain.Reference(frame, 2, 1, nil)
	if err != nil {
		t.Fatalf("Reference: %v", err)
	}
	if got.RGBAAt(0, 0) == frame.RGBAAt(0, 0) {
		t.Fatal("expected deuteranopia correction filter to alter red swatch")
	}

	// Assist exposes ColorBlindSeverity and ColorVisionFilter backed by shared settings store.
	var assist *a11y.Assist
	var store *save.DefaultSettingsStore
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		store = save.NewStorageSettingsStore(cx.Storage(), "cvd_test")
		assist = a11y.NewAssistFromSettings(cx, store)
		return core.View{}
	})
	app.Settle()

	if assist.ColorBlindSeverity() != 1.0 {
		t.Fatalf("default ColorBlindSeverity = %v, want 1.0", assist.ColorBlindSeverity())
	}
	if _, ok := assist.ColorVisionFilter(); ok {
		t.Fatal("expected no filter when ColorVision is normal")
	}

	app.Post(func() {
		if err := assist.SetColorVision(a11y.VisionProtanopia); err != nil {
			t.Errorf("SetColorVision: %v", err)
		}
		if err := assist.SetColorBlindSeverity(0.75); err != nil {
			t.Errorf("SetColorBlindSeverity: %v", err)
		}
	})
	app.Settle()

	if math.Abs(assist.ColorBlindSeverity()-0.75) > 1e-6 {
		t.Fatalf("ColorBlindSeverity = %v, want 0.75", assist.ColorBlindSeverity())
	}
	if _, ok := assist.ColorVisionFilter(); !ok {
		t.Fatal("expected active ColorVisionFilter for protanopia at 0.75")
	}

	// Setting explicit severity 0 disables the filter without clearing the mode.
	app.Post(func() {
		if err := assist.SetColorBlindSeverity(0); err != nil {
			t.Errorf("SetColorBlindSeverity(0): %v", err)
		}
	})
	app.Settle()
	if assist.ColorBlindSeverity() != 0 {
		t.Fatalf("ColorBlindSeverity = %v, want 0", assist.ColorBlindSeverity())
	}
	if _, ok := assist.ColorVisionFilter(); ok {
		t.Fatal("expected ColorVisionFilter to be disabled at severity 0")
	}
}

func TestSimulateAtDaltonizeAndColorDistance(t *testing.T) {
	red := core.RGB(0xCC2222)
	green := core.RGB(0x22AA22)

	for _, mode := range []a11y.ColorVision{a11y.VisionProtanopia, a11y.VisionDeuteranopia, a11y.VisionTritanopia} {
		// SimulateAt at 1.0 matches Simulate exactly.
		if got, want := a11y.SimulateAt(red, mode, 1.0), a11y.Simulate(red, mode); got != want {
			t.Errorf("%s: SimulateAt(1.0) = %#08x, want %#08x", mode, uint32(got), uint32(want))
		}
		// SimulateAt at 0 returns original.
		if got := a11y.SimulateAt(red, mode, 0); got != red {
			t.Errorf("%s: SimulateAt(0) = %#08x, want %#08x", mode, uint32(got), uint32(red))
		}
		// ColorVisionSimulation builds a valid effect.
		if _, ok := a11y.ColorVisionSimulation(mode, 1.0); !ok {
			t.Errorf("%s: expected ColorVisionSimulation to return true", mode)
		}
	}
	if _, ok := a11y.ColorVisionSimulation(a11y.VisionNormal, 1.0); ok {
		t.Error("expected ColorVisionSimulation(VisionNormal) to return false")
	}

	// Daltonize raises simulated distance between red and green for red-green deficiencies.
	for _, mode := range []a11y.ColorVision{a11y.VisionProtanopia, a11y.VisionDeuteranopia} {
		before := a11y.ColorDistance(a11y.Simulate(red, mode), a11y.Simulate(green, mode))
		after := a11y.ColorDistance(
			a11y.Simulate(a11y.Daltonize(red, mode, 1.0), mode),
			a11y.Simulate(a11y.Daltonize(green, mode, 1.0), mode),
		)
		if after <= before+5.0 {
			t.Errorf("%s: Daltonize did not increase simulated distance: before=%.1f after=%.1f", mode, before, after)
		}
	}
}
