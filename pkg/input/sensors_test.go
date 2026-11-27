package input_test

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const testG = 9.80665

func approxEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestWatchMotionDrivesAxesWithDeadZoneCalibrationAndScopeDisposal(t *testing.T) {
	actions := input.NewActions()
	actions.BindMotionAxis("steer", input.TiltX, input.MotionAxisOptions{
		DeadZone: 0.1,
		MaxRange: 1.0,
	})
	actions.BindMotionAxis("pitch", input.TiltY, input.MotionAxisOptions{
		MaxRange: 0.5,
	})
	actions.BindMotionAxis("yaw", input.GyroZ, input.MotionAxisOptions{
		MaxRange: math.Pi,
		Invert:   true,
	})

	var (
		shown  *core.Signal[bool]
		motion *input.MotionInput
		watchE error
		steps  int
		steer  float64
		pitch  float64
		yaw    float64
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shown = core.NewSignal(cx, false)
		loop := &game.Loop{
			Rate: 60,
			Update: func(time.Duration) {
				actions.Step()
				steps++
				steer = actions.Axis("steer")
				pitch = actions.Axis("pitch")
				yaw = actions.Axis("yaw")
			},
		}
		loop.Run(cx)
		return core.Show{
			When: shown.Get,
			Then: func(child *core.Scope) core.Node {
				motion, watchE = actions.WatchMotion(child, input.MotionOptions{
					Interval: 16 * time.Millisecond,
				})
				return core.View{}
			},
		}
	})

	step := func() {
		t.Helper()
		before := steps
		for steps == before {
			app.Advance(frameStep)
		}
	}

	// Grant motion permission, then mount the child scope that starts WatchMotion.
	app.Permissions[core.PermissionMotion] = core.PermissionGranted
	app.Post(func() { shown.Set(true) })
	app.Settle()

	if watchE != nil {
		t.Fatalf("WatchMotion failed: %v", watchE)
	}
	if !motion.Active() {
		t.Fatal("expected MotionInput to be active")
	}
	if got := app.ActiveSensorStreams(); got != 1 {
		t.Fatalf("ActiveSensorStreams = %d, want 1", got)
	}

	// Reading inside dead zone (0.08g <= 0.1) snaps to exact 0.
	app.FeedMotion(core.MotionSample{
		Sensor: core.SensorAccelerometer,
		Vector: core.SensorVector{X: 0.08 * testG, Y: 0.5 * testG, Z: 0.866 * testG},
	})
	app.Settle()
	// Snapshot has not updated yet before Step().
	if actions.Axis("steer") != 0 || actions.Axis("pitch") != 0 {
		t.Fatalf("axes changed before Step: steer=%v pitch=%v", actions.Axis("steer"), actions.Axis("pitch"))
	}

	step()
	if steer != 0 {
		t.Fatalf("inside dead zone: steer = %v, want 0", steer)
	}
	if !approxEqual(pitch, 1.0, 1e-6) {
		t.Fatalf("0.5g with MaxRange=0.5: pitch = %v, want 1.0", pitch)
	}

	// Calibrate neutral hold orientation (phone tilted forward at X=0, Y=0.5g, Z=0.866g).
	app.FeedMotion(core.MotionSample{
		Sensor: core.SensorAccelerometer,
		Vector: core.SensorVector{X: 0, Y: 0.5 * testG, Z: 0.866 * testG},
	})
	app.Settle()
	app.Post(func() { actions.CalibrateMotion() })
	app.Settle()
	step()
	if !motion.Calibrated() {
		t.Fatal("expected MotionInput.Calibrated() == true")
	}
	if !approxEqual(pitch, 0, 1e-6) || steer != 0 {
		t.Fatalf("after calibration at neutral hold: steer=%v pitch=%v, want 0, 0", steer, pitch)
	}

	// Tilt relative to neutral hold: +0.25g on Y with MaxRange=0.5 -> +0.5; X=0.55g with DeadZone=0.1 -> (0.55-0.1)/0.9 = 0.5.
	app.FeedMotion(core.MotionSample{
		Sensor: core.SensorAccelerometer,
		Vector: core.SensorVector{X: 0.55 * testG, Y: 0.75 * testG, Z: 0.66 * testG},
	})
	// Feed gyro Z = +pi/2 with MaxRange=pi and Invert=true -> -0.5.
	app.FeedMotion(core.MotionSample{
		Sensor: core.SensorGyroscope,
		Vector: core.SensorVector{Z: math.Pi / 2},
	})
	app.Settle()
	step()

	if !approxEqual(steer, 0.5, 1e-6) {
		t.Fatalf("dead-zoned steer = %v, want 0.5", steer)
	}
	if !approxEqual(pitch, 0.5, 1e-6) {
		t.Fatalf("calibrated pitch = %v, want 0.5", pitch)
	}
	if !approxEqual(yaw, -0.5, 1e-6) {
		t.Fatalf("inverted gyro yaw = %v, want -0.5", yaw)
	}

	// Out-of-range spikes clamp strictly to [-1, +1].
	app.FeedMotion(core.MotionSample{
		Sensor: core.SensorAccelerometer,
		Vector: core.SensorVector{X: -2.5 * testG, Y: 0.0, Z: 0.866 * testG},
	})
	app.FeedMotion(core.MotionSample{
		Sensor: core.SensorGyroscope,
		Vector: core.SensorVector{Z: -4 * math.Pi},
	})
	app.Settle()
	step()

	if steer != -1 {
		t.Fatalf("clamped negative steer = %v, want -1", steer)
	}
	if pitch != -1 {
		t.Fatalf("clamped negative pitch = %v, want -1", pitch)
	}
	if yaw != 1 {
		t.Fatalf("clamped inverted gyro yaw = %v, want 1", yaw)
	}

	// Disposing the child scope automatically stops the sensor stream and releases the native provider.
	app.Post(func() { shown.Set(false) })
	app.Settle()

	if got := app.ActiveSensorStreams(); got != 0 {
		t.Fatalf("after scope disposal, ActiveSensorStreams = %d, want 0", got)
	}
	if motion.Active() {
		t.Fatal("expected MotionInput.Active() == false after scope disposal")
	}

	// Feeding samples after disposal does not change axis readings.
	app.FeedMotion(core.MotionSample{
		Sensor: core.SensorAccelerometer,
		Vector: core.SensorVector{X: 1.0 * testG},
	})
	app.Settle()
	step()
	if steer != -1 {
		t.Fatalf("disposed stream mutated steer: got %v, want -1", steer)
	}
}

func TestMotionDeviceMotionFusedSmoothingGapAndDeferredCalibrate(t *testing.T) {
	m := input.NewMotionInput(input.MotionOptions{
		Smoothing: 0.5,
	})

	// Calibrate before any tilt sample arrives: first sample becomes neutral reference.
	m.Calibrate()
	if m.Calibrated() {
		t.Fatal("should not report calibrated before first tilt sample arrives")
	}

	// First fused DeviceMotion sample: snaps directly (no startup lag) and completes deferred calibration.
	m.OnMotion(core.MotionSample{
		Sensor:       core.SensorDeviceMotion,
		Gravity:      core.SensorVector{X: 0.2 * testG, Y: 0.4 * testG, Z: 0.89 * testG},
		RotationRate: core.SensorVector{X: math.Pi / 2, Y: -math.Pi / 4, Z: math.Pi},
	})
	m.Step()

	if !m.Calibrated() {
		t.Fatal("expected deferred calibration to complete on first tilt sample")
	}
	if !approxEqual(m.Axis(input.TiltX), 0, 1e-6) || !approxEqual(m.Axis(input.TiltY), 0, 1e-6) {
		t.Fatalf("after deferred calibrate: tilt=(%v, %v), want (0, 0)", m.Axis(input.TiltX), m.Axis(input.TiltY))
	}
	if !approxEqual(m.Axis(input.GyroX), 0.5, 1e-6) || !approxEqual(m.Axis(input.GyroY), -0.25, 1e-6) || !approxEqual(m.Axis(input.GyroZ), 1.0, 1e-6) {
		t.Fatalf("gyro axes = (%v, %v, %v), want (0.5, -0.25, 1.0)", m.Axis(input.GyroX), m.Axis(input.GyroY), m.Axis(input.GyroZ))
	}

	// Second sample with Smoothing=0.5 blends halfway: TiltX moves from 0.2g toward 1.0g -> 0.6g (delta 0.4g from 0.2g neutral).
	m.OnMotion(core.MotionSample{
		Sensor:       core.SensorDeviceMotion,
		Gravity:      core.SensorVector{X: 1.0 * testG, Y: 0.4 * testG, Z: 0.89 * testG},
		RotationRate: core.SensorVector{Z: 0},
	})
	m.Step()
	if !approxEqual(m.Axis(input.TiltX), 0.4, 1e-6) {
		t.Fatalf("smoothed TiltX = %v, want 0.4", m.Axis(input.TiltX))
	}
	if !approxEqual(m.Axis(input.GyroZ), 0.5, 1e-6) {
		t.Fatalf("smoothed GyroZ = %v, want 0.5", m.Axis(input.GyroZ))
	}

	// A sample marked Gap=true (e.g. after returning from background) resets filter history and snaps immediately.
	m.OnMotion(core.MotionSample{
		Sensor:       core.SensorDeviceMotion,
		Gravity:      core.SensorVector{X: -0.3 * testG, Y: 0.4 * testG, Z: 0.89 * testG},
		RotationRate: core.SensorVector{Z: -math.Pi / 2},
		Gap:          true,
	})
	m.Step()
	if !approxEqual(m.Axis(input.TiltX), -0.5, 1e-6) {
		t.Fatalf("post-gap TiltX = %v, want -0.5", m.Axis(input.TiltX))
	}
	if !approxEqual(m.Axis(input.GyroZ), -0.5, 1e-6) {
		t.Fatalf("post-gap GyroZ = %v, want -0.5", m.Axis(input.GyroZ))
	}

	// NaN / Inf samples are ignored without corrupting state.
	m.OnMotion(core.MotionSample{
		Sensor: core.SensorAccelerometer,
		Vector: core.SensorVector{X: math.NaN(), Y: math.Inf(1), Z: 0},
	})
	m.Step()
	if math.IsNaN(m.Axis(input.TiltX)) || !approxEqual(m.Axis(input.TiltX), -0.5, 1e-6) {
		t.Fatalf("NaN sample corrupted TiltX: %v", m.Axis(input.TiltX))
	}

	// ResetCalibration restores absolute tilt relative to 0g.
	m.ResetCalibration()
	if m.Calibrated() {
		t.Fatal("expected Calibrated() == false after ResetCalibration")
	}
	if !approxEqual(m.Axis(input.TiltX), -0.3, 1e-6) {
		t.Fatalf("uncalibrated TiltX = %v, want -0.3", m.Axis(input.TiltX))
	}
}

func TestMotionCombinesWithOppositeActionAxesResetAndStorage(t *testing.T) {
	a := input.NewActions()
	a.Bind("left", input.TouchRegion(rect(0, 0, 100, 100)))
	a.Bind("right", input.TouchRegion(rect(100, 0, 100, 100)))
	a.BindAxis("steer", "left", "right")
	opts := input.MotionAxisOptions{
		DeadZone:    0.1,
		Sensitivity: 1.5,
		MaxRange:    1.0,
		Smoothing:   0.25,
		Invert:      true,
	}
	a.BindMotionAxis("steer", input.TiltX, input.MotionAxisOptions{MaxRange: 1.0})

	// Tilt left (-0.4) with no button pressed -> -0.4.
	a.OnMotion(core.MotionSample{
		Sensor: core.SensorAccelerometer,
		Vector: core.SensorVector{X: -0.4 * testG},
	})
	a.Step()
	if !approxEqual(a.Axis("steer"), -0.4, 1e-6) {
		t.Fatalf("tilt only steer = %v, want -0.4", a.Axis("steer"))
	}

	// Hold right (+1.0) while tilting left (-0.4) -> +0.6.
	a.OnTouch(core.TouchEvent{
		Phase:   core.TouchStart,
		Touches: []core.Touch{{ID: 1, Position: pt(150, 50), Changed: true}},
	})
	a.Step()
	if !approxEqual(a.Axis("steer"), 0.6, 1e-6) {
		t.Fatalf("combined steer = %v, want 0.6", a.Axis("steer"))
	}

	// Hold right (+1.0) while tilting right (+0.5) -> clamped to +1.0.
	a.OnMotion(core.MotionSample{
		Sensor: core.SensorAccelerometer,
		Vector: core.SensorVector{X: 0.5 * testG},
	})
	a.Step()
	if a.Axis("steer") != 1 {
		t.Fatalf("clamped combined steer = %v, want 1", a.Axis("steer"))
	}

	// Reset clears active touch and motion readings while preserving bindings.
	a.Reset()
	if a.Axis("steer") != 0 {
		t.Fatalf("after Reset, steer = %v, want 0", a.Axis("steer"))
	}
	if ax, _, ok := a.MotionAxisBinding("steer"); !ok || ax != input.TiltX {
		t.Fatalf("Reset removed motion binding: (%v, %v)", ax, ok)
	}

	// Persist motion axis bindings across Save/Load in core.Storage.
	a.BindMotionAxis("aim", input.GyroY, opts)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	app.Post(func() {
		if err := a.Save(app.Storage(), "controls"); err != nil {
			t.Errorf("Save failed: %v", err)
		}
	})
	app.Settle()

	b := input.NewActions()
	loaded, err := b.Load(app.Storage(), "controls")
	if err != nil || !loaded {
		t.Fatalf("Load = (%v, %v), want (true, nil)", loaded, err)
	}
	gotAxis, gotOpts, ok := b.MotionAxisBinding("aim")
	if !ok || gotAxis != input.GyroY || gotOpts != opts {
		t.Fatalf("loaded aim = (%v, %+v, %v), want (%v, %+v, true)", gotAxis, gotOpts, ok, input.GyroY, opts)
	}

	// Invalid motion axis in storage leaves bindings untouched.
	badApp := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} },
		nsgotest.Storage(map[string]string{"controls": `{"version":1,"motion_axes":{"aim":{"axis":"bogus"}}}`}))
	if ok, err := b.Load(badApp.Storage(), "controls"); err == nil || ok {
		t.Fatalf("Load invalid motion axis = (%v, %v), want (false, error)", ok, err)
	}
	if ax, _, ok := b.MotionAxisBinding("aim"); !ok || ax != input.GyroY {
		t.Fatalf("failed Load mutated motion binding: (%v, %v)", ax, ok)
	}
}

func TestWatchMotionPermissionDeniedReportsError(t *testing.T) {
	var (
		cx       *core.Scope
		gotError error
		motion   *input.MotionInput
	)
	app := nsgotest.Start(t, func(c *core.Scope) core.Node {
		cx = c
		return core.View{}
	})
	// PermissionMotion is not granted (defaults to PermissionNotAsked).
	app.Post(func() {
		var err error
		motion, err = input.WatchMotion(cx, input.MotionOptions{
			OnError: func(e error) { gotError = e },
		})
		if err != nil {
			t.Errorf("WatchMotion returned unexpected sync error: %v", err)
		}
	})
	app.Settle()

	if !errors.Is(gotError, core.SensorNotAllowed) {
		t.Fatalf("OnError = %v, want core.SensorNotAllowed", gotError)
	}
	if motion.Active() {
		t.Fatal("denied stream should not remain active")
	}
	if got := app.ActiveSensorStreams(); got != 0 {
		t.Fatalf("ActiveSensorStreams = %d, want 0", got)
	}
}

func TestHapticsPresetsDisabledToggleAndStorage(t *testing.T) {
	var (
		h      *input.Haptics
		played []core.Haptic
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		h = input.NewHaptics(cx)
		h.OnHaptic = func(k core.Haptic) { played = append(played, k) }
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSIOS}))

	app.Post(func() {
		h.Selection()
		h.Light()
		h.Medium()
		h.Heavy()
		h.Soft()
		h.Rigid()
		h.Success()
		h.Warning()
		h.Error()
		h.Play(core.HapticMedium)
		// Invalid values are ignored.
		h.Play(0)
		h.Play(core.HapticError + 1)
	})
	app.Settle()

	want := []core.Haptic{
		core.HapticSelection,
		core.HapticLight,
		core.HapticMedium,
		core.HapticHeavy,
		core.HapticSoft,
		core.HapticRigid,
		core.HapticSuccess,
		core.HapticWarning,
		core.HapticError,
		core.HapticMedium,
	}
	if !slices.Equal(played, want) {
		t.Fatalf("played = %v, want %v", played, want)
	}

	// Disabling haptics silences all presets and rumble.
	played = nil
	var rumbleCalls int
	h.GamepadRumble = func(float32, float32, time.Duration) { rumbleCalls++ }
	app.Post(func() {
		h.Enabled = false
		h.Selection()
		h.Light()
		h.Medium()
		h.Heavy()
		h.Success()
		h.Warning()
		h.Error()
		h.Rumble(1.0, 1.0, 100*time.Millisecond)
		h.Update(50 * time.Millisecond)
	})
	app.Settle()

	if len(played) != 0 || rumbleCalls != 0 || h.Rumbling() {
		t.Fatalf("disabled haptics emitted feedback: played=%v rumbleCalls=%d rumbling=%v", played, rumbleCalls, h.Rumbling())
	}

	// Save disabled setting and restore in a fresh Haptics instance.
	app.Post(func() {
		if err := h.Save(app.Storage(), "haptics.enabled"); err != nil {
			t.Errorf("Save failed: %v", err)
		}
	})
	app.Settle()

	kept := map[string]string{}
	for _, k := range app.Storage().Keys() {
		kept[k], _ = app.Storage().GetItem(k)
	}

	var restored *input.Haptics
	var loadOK bool
	var loadErr error
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		restored = input.NewHaptics(cx)
		loadOK, loadErr = restored.Load(cx.Storage(), "haptics.enabled")
		return core.View{}
	}, nsgotest.Storage(kept))
	if loadErr != nil || !loadOK || restored.Enabled {
		t.Fatalf("restored Load = (%v, %v), Enabled=%v; want (true, nil), false", loadOK, loadErr, restored.Enabled)
	}

	// Invalid stored value leaves Enabled untouched.
	badApp := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} },
		nsgotest.Storage(map[string]string{"haptics.enabled": "notabool"}))
	restored.Enabled = true
	if ok, err := restored.Load(badApp.Storage(), "haptics.enabled"); err == nil || ok || !restored.Enabled {
		t.Fatalf("invalid Load = (%v, %v), Enabled=%v; want (false, err), true", ok, err, restored.Enabled)
	}
}

func TestHapticsRumblePulseTrainPatternsAndGamepadSink(t *testing.T) {
	type rumbleEvent struct {
		low, high float32
		dur       time.Duration
	}

	var (
		h       *input.Haptics
		played  []core.Haptic
		rumbles []rumbleEvent
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		h = input.NewHaptics(cx)
		h.PulseInterval = 50 * time.Millisecond
		h.OnHaptic = func(k core.Haptic) { played = append(played, k) }
		h.GamepadRumble = func(low, high float32, dur time.Duration) {
			rumbles = append(rumbles, rumbleEvent{low: low, high: high, dur: dur})
		}
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSIOS}))

	// Heavy rumble for 120ms with 50ms pulse interval: immediate pulse at t=0, t=50ms, t=100ms, stops at t=125ms.
	app.Post(func() {
		h.Rumble(0.8, 0.4, 120*time.Millisecond)
		for range 5 {
			h.Update(25 * time.Millisecond)
		}
	})
	app.Settle()

	if h.Rumbling() {
		t.Fatal("rumble still active after duration elapsed")
	}
	wantHaptics := []core.Haptic{core.HapticHeavy, core.HapticHeavy, core.HapticHeavy}
	if !slices.Equal(played, wantHaptics) {
		t.Fatalf("rumble pulses = %v, want %v", played, wantHaptics)
	}
	wantRumbles := []rumbleEvent{
		{low: 0.8, high: 0.4, dur: 120 * time.Millisecond},
		{low: 0, high: 0, dur: 0},
	}
	if !slices.Equal(rumbles, wantRumbles) {
		t.Fatalf("gamepad rumble events = %+v, want %+v", rumbles, wantRumbles)
	}

	// Multi-step pattern with silent gap: heavy -> pause -> medium -> light.
	played = nil
	rumbles = nil
	app.Post(func() {
		h.PlayPattern(
			input.RumbleStep{LowFreq: 0.9, HighFreq: 0.2, Duration: 40 * time.Millisecond},
			input.RumbleStep{LowFreq: 0, HighFreq: 0, Duration: 40 * time.Millisecond},
			input.RumbleStep{LowFreq: 0.5, HighFreq: 0.3, Duration: 40 * time.Millisecond},
			input.RumbleStep{LowFreq: 0.2, HighFreq: 0.1, Duration: 40 * time.Millisecond},
		)
		for range 4 {
			h.Update(40 * time.Millisecond)
		}
	})
	app.Settle()

	if h.Rumbling() {
		t.Fatal("pattern still active after total duration")
	}
	wantPatternHaptics := []core.Haptic{core.HapticHeavy, core.HapticMedium, core.HapticLight}
	if !slices.Equal(played, wantPatternHaptics) {
		t.Fatalf("pattern haptics = %v, want %v", played, wantPatternHaptics)
	}
	wantPatternRumbles := []rumbleEvent{
		{low: 0.9, high: 0.2, dur: 40 * time.Millisecond},
		{low: 0, high: 0, dur: 0}, // silent pause zeroes motors
		{low: 0.5, high: 0.3, dur: 40 * time.Millisecond},
		{low: 0.2, high: 0.1, dur: 40 * time.Millisecond},
		{low: 0, high: 0, dur: 0}, // pattern complete zeroes motors
	}
	if !slices.Equal(rumbles, wantPatternRumbles) {
		t.Fatalf("pattern gamepad rumbles = %+v, want %+v", rumbles, wantPatternRumbles)
	}

	// Disabling mid-rumble via SetEnabled(false) stops immediately and zeroes motors.
	played = nil
	rumbles = nil
	app.Post(func() {
		h.Rumble(0.6, 0.6, 200*time.Millisecond)
		h.SetEnabled(false)
		h.Update(50 * time.Millisecond)
	})
	app.Settle()

	if h.Rumbling() {
		t.Fatal("rumble stayed active after SetEnabled(false)")
	}
	if len(rumbles) != 2 || rumbles[1] != (rumbleEvent{}) {
		t.Fatalf("expected motor stop on SetEnabled(false), got %+v", rumbles)
	}
	if len(played) != 1 || played[0] != core.HapticMedium {
		t.Fatalf("unexpected haptics after SetEnabled(false): %v", played)
	}
}

func TestHapticsSilencedInBackgroundAndScopeDisposal(t *testing.T) {
	type rumbleEvent struct {
		low, high float32
		dur       time.Duration
	}

	var (
		shown   *core.Signal[bool]
		h       *input.Haptics
		played  []core.Haptic
		rumbles []rumbleEvent
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shown = core.NewSignal(cx, true)
		return core.Show{
			When: shown.Get,
			Then: func(child *core.Scope) core.Node {
				h = input.NewHaptics(child)
				h.PulseInterval = 50 * time.Millisecond
				h.OnHaptic = func(k core.Haptic) { played = append(played, k) }
				h.GamepadRumble = func(low, high float32, dur time.Duration) {
					rumbles = append(rumbles, rumbleEvent{low: low, high: high, dur: dur})
				}
				return core.View{}
			},
		}
	}, nsgotest.Environment(core.Environment{OS: core.OSAndroid, Lifecycle: core.LifecycleActive}))

	// Start a rumble in foreground.
	app.Post(func() {
		h.Rumble(1.0, 0.5, 300*time.Millisecond)
	})
	app.Settle()
	if !h.Rumbling() || len(played) != 1 || len(rumbles) != 1 {
		t.Fatalf("foreground rumble did not start: rumbling=%v played=%v rumbles=%+v", h.Rumbling(), played, rumbles)
	}

	// Enter LifecycleBackground: active rumble stops immediately with (0, 0, 0).
	app.SetEnvironment(core.Environment{OS: core.OSAndroid, Lifecycle: core.LifecycleBackground})
	if h.Rumbling() {
		t.Fatal("rumble stayed active in LifecycleBackground")
	}
	if len(rumbles) != 2 || rumbles[1] != (rumbleEvent{}) {
		t.Fatalf("background did not zero gamepad motors: %+v", rumbles)
	}

	// All presets and rumble calls stay completely silent while in background.
	played = nil
	rumbles = nil
	app.Post(func() {
		h.Light()
		h.Medium()
		h.Heavy()
		h.Success()
		h.Warning()
		h.Error()
		h.Selection()
		h.Rumble(0.9, 0.9, 200*time.Millisecond)
		h.Update(50 * time.Millisecond)
	})
	app.Settle()

	if len(played) != 0 || len(rumbles) != 0 || h.Rumbling() {
		t.Fatalf("emitted feedback in background: played=%v rumbles=%+v rumbling=%v", played, rumbles, h.Rumbling())
	}

	// Returning to foreground re-enables haptics and rumble.
	app.SetEnvironment(core.Environment{OS: core.OSAndroid, Lifecycle: core.LifecycleActive})
	app.Post(func() {
		h.Success()
		h.Rumble(0.5, 0.5, 300*time.Millisecond)
	})
	app.Settle()
	if !slices.Equal(played, []core.Haptic{core.HapticSuccess, core.HapticMedium}) || !h.Rumbling() {
		t.Fatalf("foreground resume failed: played=%v rumbling=%v", played, h.Rumbling())
	}

	// Disposing the owning scope stops active rumble and silences subsequent calls.
	app.Post(func() { shown.Set(false) })
	app.Settle()
	if h.Rumbling() {
		t.Fatal("rumble stayed active after scope disposal")
	}
	if rumbles[len(rumbles)-1] != (rumbleEvent{}) {
		t.Fatalf("scope disposal did not zero gamepad motors: %+v", rumbles)
	}

	played = nil
	rumbles = nil
	app.Post(func() {
		h.Heavy()
		h.Rumble(1.0, 1.0, 100*time.Millisecond)
	})
	app.Settle()
	if len(played) != 0 || len(rumbles) != 0 {
		t.Fatalf("disposed Haptics emitted feedback: played=%v rumbles=%+v", played, rumbles)
	}
}
