package rhythm_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/game/packages/game/rhythm"
	"github.com/nsgo-dev/nsgo/core"
)

var screenBounds = core.Bounds{Size: core.Size{Width: 400, Height: 600}}

// buttons returns the screen's Retry and Save buttons.
func buttons(t *testing.T, s *rhythm.CalibrationScreen) (retry, save *gameui.Button) {
	t.Helper()
	box := s.Root().Child.(*gameui.VBox)
	row := box.Children[len(box.Children)-1].(*gameui.HBox)
	return row.Children[0].(*gameui.Button), row.Children[1].(*gameui.Button)
}

func center(b core.Bounds) core.Point {
	return core.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
}

func hasFill(ops []core.DrawOp, c core.Color) bool {
	for _, op := range ops {
		if op.Kind == core.DrawFillColor && op.Color == c {
			return true
		}
	}
	return false
}

func TestCalibrationScreenMeasuresAndSavesTheVideoOffset(t *testing.T) {
	app, cal, cx := startCalibrationIn(t, rhythm.CalibrateVideo, core.Environment{Audio: true, OS: core.OSAndroid, DeviceModel: "Google Pixel 8"})
	var saved []rhythm.Offsets
	cal.Taps, cal.Warmup = 4, 1
	if key := rhythm.DeviceKey(cx); key != "android/Google Pixel 8" {
		t.Fatalf("device key %q", key)
	}
	store := rhythm.NewOffsetStore(cx, "rhythm.calibration")
	if err := store.Set("android/Google Pixel 8", rhythm.Offsets{Audio: 70 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	screen := rhythm.NewCalibrationScreen(cal, store, "android/Google Pixel 8")
	screen.FlashColor = core.RGB(0xff00aa)
	screen.OnSave = func(o rhythm.Offsets) { saved = append(saved, o) }
	if screen.Calibration() != cal || !cal.Started() {
		t.Fatal("the screen did not start its calibration")
	}
	retry, save := buttons(t, screen)
	frame := func() []core.DrawOp {
		d := &core.Drawing{}
		screen.Draw(d, screenBounds)
		return d.Ops
	}
	frame()
	if retry.Text != "Retry" || !save.Disabled {
		t.Fatalf("retry %q, save disabled %v", retry.Text, save.Disabled)
	}
	for k := range 5 {
		// The frame that first shows each cue comes 8ms after it, and the player taps 40ms after that frame.
		app.FastForward(cal.Cue(k).Add(8 * time.Millisecond).Sub(app.Clock.Now()))
		if ops := frame(); !hasFill(ops, screen.FlashColor) {
			t.Fatalf("cue %d did not flash", k)
		}
		app.FastForward(40 * time.Millisecond)
		frame()
		screen.OnPress(core.PressEvent{Keyboard: true})
	}
	frame()
	if r, done := cal.Result(); !done || r.Offset != 40*time.Millisecond {
		t.Fatalf("result %+v %v", r, done)
	}
	if save.Disabled {
		t.Fatal("save is disabled with a steady result")
	}
	if !screen.OnPress(core.PressEvent{Position: center(save.Bounds())}) {
		t.Fatal("the save button did not take the press")
	}
	want := rhythm.Offsets{Audio: 70 * time.Millisecond, Video: 40 * time.Millisecond}
	if o, ok := store.Get("android/Google Pixel 8"); !ok || o != want || len(saved) != 1 || saved[0] != want || screen.Err() != nil {
		t.Fatalf("kept %+v, saved %+v, err %v", o, saved, screen.Err())
	}
	if !screen.OnPress(core.PressEvent{Position: center(retry.Bounds())}) || cal.Count() != 0 {
		t.Fatal("retry did not start over")
	}
	frame()
	if !save.Disabled {
		t.Fatal("save is enabled after a retry")
	}
	if err := screen.Save(); err != nil || len(saved) != 1 {
		t.Fatalf("saved without a result: %v", err)
	}
}

func TestCalibrationScreenShowsAnUnsteadyResult(t *testing.T) {
	app, cal, cx := startCalibrationIn(t, rhythm.CalibrateAudio, core.Environment{Audio: true, AudioSchedule: true})
	cal.Taps, cal.Warmup = 3, -1
	store := rhythm.NewOffsetStore(cx, "cal")
	screen := rhythm.NewCalibrationScreen(cal, store, "dev")
	text := rhythm.DefaultCalibrationText()
	text.Unsteady = "spread %d"
	screen.Text = text
	for k, off := range []time.Duration{0, 200, -100} {
		app.FastForward(cal.Cue(k).Add(25*time.Millisecond + off*time.Millisecond).Sub(app.Clock.Now()))
		screen.Draw(&core.Drawing{}, screenBounds)
		screen.OnPress(core.PressEvent{Position: core.Point{X: 200, Y: 200}})
	}
	d := &core.Drawing{}
	screen.Draw(d, screenBounds)
	found := false
	for _, op := range d.Ops {
		if op.Kind == core.DrawFillText && op.Text == "spread 100" {
			found = true
		}
	}
	if !found {
		t.Fatal("the unsteady result is not shown")
	}
	_, save := buttons(t, screen)
	if !save.Disabled {
		t.Fatal("save is enabled with an unsteady result")
	}
	if err := screen.Save(); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("dev"); ok {
		t.Fatal("an unsteady result was kept")
	}
}
