package input

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// standardGravity is standard gravity in m/s², matching NSGo's MotionSample convention
// where a face-up device at rest reports +9.80665 m/s² on Z.
const standardGravity = 9.80665

// defaultGyroRange is the default angular velocity in rad/s that maps to ±1.0 for Gyro axes.
const defaultGyroRange = math.Pi

// MotionAxis identifies one tilt (accelerometer/gravity) or gyroscope axis.
type MotionAxis uint8

const (
	// MotionAxisNone is the zero MotionAxis, which always reads 0.
	MotionAxisNone MotionAxis = iota
	// TiltX is device tilt along the screen X axis (right, in g units),
	// rotated from the natural device frame according to MotionOptions.Orientation.
	TiltX
	// TiltY is device tilt along the screen Y axis (up, in g units),
	// rotated from the natural device frame according to MotionOptions.Orientation.
	TiltY
	// TiltZ is device tilt along the screen Z axis (out of the screen, in g units).
	TiltZ
	// GyroX is angular velocity around the screen X axis (rad/s),
	// rotated from the natural device frame according to MotionOptions.Orientation.
	GyroX
	// GyroY is angular velocity around the screen Y axis (rad/s),
	// rotated from the natural device frame according to MotionOptions.Orientation.
	GyroY
	// GyroZ is angular velocity around the screen Z axis (rad/s).
	GyroZ
)

// MotionOrientation selects how device motion vectors are rotated from NSGo's
// right-handed natural device frame (+X right, +Y up, +Z out of the screen)
// into screen axes before smoothing and calibration.
type MotionOrientation uint8

const (
	// MotionOrientationPortrait uses the natural device frame (device top at screen top).
	// The zero value keeps portrait so callers that omit Orientation behave unchanged.
	MotionOrientationPortrait MotionOrientation = iota
	// MotionOrientationPortraitUpsideDown rotates 180° (device top at screen bottom):
	// screen (+X, +Y, +Z) = device (-X, -Y, +Z).
	MotionOrientationPortraitUpsideDown
	// MotionOrientationLandscapeLeft rotates 90° counter-clockwise (device top at screen left,
	// matching NSGo's core.OrientationLandscapeLeft on Android, HarmonyOS and Web):
	// screen (+X, +Y, +Z) = device (-Y, +X, +Z).
	MotionOrientationLandscapeLeft
	// MotionOrientationLandscapeRight rotates 90° clockwise (device top at screen right,
	// matching NSGo's core.OrientationLandscapeRight on Android, HarmonyOS and Web):
	// screen (+X, +Y, +Z) = device (+Y, -X, +Z).
	MotionOrientationLandscapeRight
	// MotionOrientationAuto infers portrait or landscape from the window's aspect ratio
	// (via cx.Env().Window() when started on a core.Scope, or SetWindowSize otherwise).
	// When the window is wider than tall, it uses MotionOptions.LandscapeSide
	// (defaulting to MotionOrientationLandscapeLeft); otherwise it uses MotionOrientationPortrait.
	MotionOrientationAuto
)

// String returns a stable name for o.
func (o MotionOrientation) String() string {
	switch o {
	case MotionOrientationPortrait:
		return "portrait"
	case MotionOrientationPortraitUpsideDown:
		return "portrait_upside_down"
	case MotionOrientationLandscapeLeft:
		return "landscape_left"
	case MotionOrientationLandscapeRight:
		return "landscape_right"
	case MotionOrientationAuto:
		return "auto"
	default:
		return fmt.Sprintf("MotionOrientation(%d)", o)
	}
}

// String returns a stable name for m.
func (m MotionAxis) String() string {
	switch m {
	case MotionAxisNone:
		return "none"
	case TiltX:
		return "tilt_x"
	case TiltY:
		return "tilt_y"
	case TiltZ:
		return "tilt_z"
	case GyroX:
		return "gyro_x"
	case GyroY:
		return "gyro_y"
	case GyroZ:
		return "gyro_z"
	default:
		return fmt.Sprintf("MotionAxis(%d)", m)
	}
}

func (m MotionAxis) isTilt() bool {
	return m == TiltX || m == TiltY || m == TiltZ
}

func (m MotionAxis) isGyro() bool {
	return m == GyroX || m == GyroY || m == GyroZ
}

func (m MotionAxis) valid() bool {
	return m >= TiltX && m <= GyroZ
}

func encodeMotionAxis(m MotionAxis) (string, error) {
	switch m {
	case MotionAxisNone:
		return "none", nil
	case TiltX, TiltY, TiltZ, GyroX, GyroY, GyroZ:
		return m.String(), nil
	default:
		return "", fmt.Errorf("unknown motion axis %d", m)
	}
}

func decodeMotionAxis(s string) (MotionAxis, error) {
	switch s {
	case "", "none":
		return MotionAxisNone, nil
	case "tilt_x":
		return TiltX, nil
	case "tilt_y":
		return TiltY, nil
	case "tilt_z":
		return TiltZ, nil
	case "gyro_x":
		return GyroX, nil
	case "gyro_y":
		return GyroY, nil
	case "gyro_z":
		return GyroZ, nil
	default:
		return MotionAxisNone, fmt.Errorf("unknown motion axis %q", s)
	}
}

// MotionOptions configures a MotionInput watching device sensors via core.Scope.WatchMotion.
type MotionOptions struct {
	// Sensors chooses which NSGo motion sensors to watch.
	// Zero requests core.SensorAccelerometer | core.SensorGyroscope.
	Sensors core.MotionSensors
	// Interval is the requested sample interval, clamped by NSGo to 10ms..1s (0 defaults to 20ms).
	Interval time.Duration
	// Orientation rotates tilt and gyro vectors from the natural device frame into screen axes
	// before smoothing and calibration. Zero defaults to MotionOrientationPortrait.
	Orientation MotionOrientation
	// LandscapeSide chooses which landscape side MotionOrientationAuto selects when the window is
	// wider than tall. Setting MotionOrientationLandscapeRight selects landscape right; any other
	// value (including zero) selects MotionOrientationLandscapeLeft.
	LandscapeSide MotionOrientation
	// DeadZone is the default magnitude in [0, 1) below which normalized axis readings snap to 0.
	DeadZone float64
	// Sensitivity multiplies calibrated axis values before clamping (1 when <= 0).
	Sensitivity float64
	// TiltRange is the tilt magnitude in g (9.80665 m/s²) that maps to 1.0 before Sensitivity (1.0 when <= 0).
	TiltRange float64
	// GyroRange is the rotation rate in rad/s that maps to 1.0 before Sensitivity (math.Pi when <= 0).
	GyroRange float64
	// Smoothing is the low-pass filter factor in [0, 1) applied to incoming samples.
	// 0 disables smoothing; higher values smooth more heavily (filtered = prev*Smoothing + raw*(1-Smoothing)).
	Smoothing float64
	// OnError is called on the engine goroutine if WatchMotion's provider reports an error
	// (such as core.SensorNotAllowed or core.SensorNotAvailable).
	OnError func(error)
}

// MotionAxisOptions configures how one bound motion axis is mapped into [-1, 1].
// Zero-valued DeadZone, Sensitivity and MaxRange inherit defaults from the attached MotionInput.
type MotionAxisOptions struct {
	// DeadZone is the magnitude in [0, 1) below which the axis reads 0.
	DeadZone float64
	// Sensitivity multiplies the calibrated reading before clamping.
	Sensitivity float64
	// MaxRange is the input magnitude that maps to 1.0 before Sensitivity:
	// in g for Tilt axes (default 1.0), and in rad/s for Gyro axes (default math.Pi).
	MaxRange float64
	// Smoothing is an optional per-axis low-pass factor in [0, 1) applied on each Step.
	Smoothing float64
	// Invert negates the resulting axis value.
	Invert bool
}

type motionAxisBinding struct {
	axis MotionAxis
	opts MotionAxisOptions
}

// MotionInput watches device accelerometer and gyroscope sensors via core.Scope.WatchMotion,
// calibrates tilt against a neutral hold angle, applies low-pass smoothing, dead zone and sensitivity,
// and exposes [-1, 1] axis values sampled once per fixed game.Loop step.
//
// MotionInput's methods are called on the engine's goroutine, as core.Scope.WatchMotion callbacks
// and game.Loop.Update are.
type MotionInput struct {
	opts       MotionOptions
	cx         *core.Scope
	stream     *core.SensorStream
	stopEnv    func()
	watching   bool
	watchGen   uint64
	paused     bool
	windowSize core.Size

	// Live smoothed sensor readings in screen axes (tilt in g, gyro in rad/s).
	tilt    core.SensorVector
	hasTilt bool
	gyro    core.SensorVector
	hasGyro bool

	// Neutral tilt reference captured by Calibrate (in screen axes, g units).
	zeroTilt      core.SensorVector
	calibrated    bool
	calibrateNext bool

	// Stepped snapshot updated only by Step().
	snapTilt    core.SensorVector
	hasSnapTilt bool
	snapGyro    core.SensorVector
	hasSnapGyro bool

	// Incremented whenever a provider gap, orientation change or Reset invalidates smoothing history.
	gapSeq uint64
}

// NewMotionInput returns a MotionInput configured with opts.
// Call Start(cx) to subscribe to live device sensors, or feed samples directly with OnMotion.
func NewMotionInput(opts MotionOptions) *MotionInput {
	return &MotionInput{opts: opts}
}

// WatchMotion creates a MotionInput with opts and immediately subscribes to device motion sensors
// on cx via cx.WatchMotion. The sensor stream stops automatically when cx is disposed (or when Stop
// is called), pauses while the app is in background, and can be paused with SetPaused (e.g. from game.Loop.OnPause).
func WatchMotion(cx *core.Scope, opts MotionOptions) (*MotionInput, error) {
	m := NewMotionInput(opts)
	if err := m.Start(cx); err != nil {
		return nil, err
	}
	return m, nil
}

// Start subscribes to live device motion sensors on cx using m's options.
// Any previously active stream on m is stopped first. If m is currently paused via SetPaused(true),
// Start records cx and waits to open the sensor stream until SetPaused(false) is called.
func (m *MotionInput) Start(cx *core.Scope) error {
	m.Stop()
	if cx == nil || cx.Disposed() {
		return core.SensorNotAvailable
	}
	m.watchGen++
	gen := m.watchGen
	m.cx = cx
	m.watching = true
	m.stopEnv = cx.Effect(func() {
		m.applyWindowSize(cx.Env().Window())
	})
	cx.Cleanup(func() {
		if m.watchGen != gen {
			return
		}
		m.stream = nil
		m.stopEnv = nil
		m.cx = nil
		m.watching = false
	})
	if m.paused {
		return nil
	}
	if err := m.startStream(); err != nil {
		m.Stop()
		return err
	}
	return nil
}

func (m *MotionInput) startStream() error {
	if m.cx == nil || m.cx.Disposed() {
		return core.SensorNotAvailable
	}
	sensors := m.opts.Sensors
	if sensors == 0 {
		sensors = core.SensorAccelerometer | core.SensorGyroscope
	}
	stream, err := m.cx.WatchMotion(
		core.MotionOptions{Sensors: sensors, Interval: m.opts.Interval},
		m.OnMotion,
		func(err error) {
			if m.opts.OnError != nil {
				m.opts.OnError(err)
			}
		},
	)
	if err != nil {
		return err
	}
	m.stream = stream
	return nil
}

// Stop cancels the active sensor stream, if any, and stops watching the scope.
func (m *MotionInput) Stop() {
	m.watchGen++
	if m.stream != nil {
		m.stream.Stop()
		m.stream = nil
	}
	if m.stopEnv != nil {
		m.stopEnv()
		m.stopEnv = nil
	}
	m.cx = nil
	m.watching = false
}

// SetPaused stops the active motion sensor stream while paused and restarts it on resume,
// preserving calibration and options. While paused, live and snapped readings are cleared and
// incoming samples are ignored so no stale tilt or gyro values persist across a pause menu.
//
// Pair with game.Loop.OnPause (loop.OnPause = motion.SetPaused) so sensors stop while the game
// is paused as well as when the app enters the background.
func (m *MotionInput) SetPaused(paused bool) {
	if m.paused == paused {
		return
	}
	m.paused = paused
	if paused {
		if m.stream != nil {
			m.stream.Stop()
			m.stream = nil
		}
		m.Reset()
		return
	}
	if m.watching && m.cx != nil && !m.cx.Disposed() {
		if err := m.startStream(); err != nil && m.opts.OnError != nil {
			m.opts.OnError(err)
		}
	}
}

// Paused reports whether m is currently paused via SetPaused.
func (m *MotionInput) Paused() bool {
	return m.paused
}

// Active reports whether m currently has a live, unstopped sensor stream.
func (m *MotionInput) Active() bool {
	return m.stream != nil && !m.stream.Stopped()
}

// SetOptions updates Orientation, LandscapeSide, DeadZone, Sensitivity, TiltRange, GyroRange,
// Smoothing and OnError at runtime. Changing Sensors or Interval takes effect on the next Start
// or resume from SetPaused(false).
func (m *MotionInput) SetOptions(opts MotionOptions) {
	prev := m.effectiveOrientation()
	m.opts = opts
	if m.effectiveOrientation() != prev {
		m.invalidateSmoothing()
	}
}

// Options returns m's current MotionOptions.
func (m *MotionInput) Options() MotionOptions {
	return m.opts
}

// SetOrientation sets m's orientation mode at runtime without replacing other MotionOptions.
func (m *MotionInput) SetOrientation(orientation MotionOrientation) {
	prev := m.effectiveOrientation()
	m.opts.Orientation = orientation
	if m.effectiveOrientation() != prev {
		m.invalidateSmoothing()
	}
}

// Orientation returns the resolved orientation currently applied to incoming motion vectors
// (one of MotionOrientationPortrait, MotionOrientationPortraitUpsideDown,
// MotionOrientationLandscapeLeft or MotionOrientationLandscapeRight).
func (m *MotionInput) Orientation() MotionOrientation {
	return m.effectiveOrientation()
}

// SetWindowSize supplies the active view or window size used to infer portrait vs. landscape
// when MotionOptions.Orientation is MotionOrientationAuto. When Start or WatchMotion is active,
// cx.Env().Window() updates this automatically.
func (m *MotionInput) SetWindowSize(size core.Size) {
	m.applyWindowSize(size)
}

func (m *MotionInput) applyWindowSize(size core.Size) {
	prev := m.effectiveOrientation()
	m.windowSize = size
	if m.effectiveOrientation() != prev {
		m.invalidateSmoothing()
	}
}

func (m *MotionInput) effectiveOrientation() MotionOrientation {
	switch m.opts.Orientation {
	case MotionOrientationPortraitUpsideDown, MotionOrientationLandscapeLeft, MotionOrientationLandscapeRight:
		return m.opts.Orientation
	case MotionOrientationAuto:
		if m.windowSize.Width > m.windowSize.Height {
			if m.opts.LandscapeSide == MotionOrientationLandscapeRight {
				return MotionOrientationLandscapeRight
			}
			return MotionOrientationLandscapeLeft
		}
		return MotionOrientationPortrait
	default:
		return MotionOrientationPortrait
	}
}

func (m *MotionInput) invalidateSmoothing() {
	m.hasTilt = false
	m.hasGyro = false
	m.gapSeq++
}

// OnMotion ingests one core.MotionSample (from WatchMotion, a unit test, or a replay).
// Samples are rotated from the natural device frame into screen axes according to Orientation
// before smoothing and calibration. Updated values are held until the next Step call so reads
// stay stable within a fixed game.Loop step. Calls while paused are ignored.
func (m *MotionInput) OnMotion(sample core.MotionSample) {
	if m.paused {
		return
	}
	if sample.Gap {
		m.invalidateSmoothing()
	}
	alpha := clampSmoothing(m.opts.Smoothing)
	orient := m.effectiveOrientation()

	switch sample.Sensor {
	case core.SensorAccelerometer:
		m.ingestTilt(rotateMotionVector(scaleVector(sample.Vector, 1/standardGravity), orient), alpha)
	case core.SensorGyroscope:
		m.ingestGyro(rotateMotionVector(sample.Vector, orient), alpha)
	case core.SensorDeviceMotion:
		m.ingestTilt(rotateMotionVector(scaleVector(sample.Gravity, 1/standardGravity), orient), alpha)
		m.ingestGyro(rotateMotionVector(sample.RotationRate, orient), alpha)
	}
}

func (m *MotionInput) ingestTilt(raw core.SensorVector, smoothing float64) {
	if !finiteVector(raw) {
		return
	}
	if !m.hasTilt || smoothing == 0 {
		m.tilt = raw
	} else {
		m.tilt = lerpVector(raw, m.tilt, smoothing)
	}
	m.hasTilt = true
	if m.calibrateNext {
		m.zeroTilt = m.tilt
		m.calibrated = true
		m.calibrateNext = false
	}
}

func (m *MotionInput) ingestGyro(raw core.SensorVector, smoothing float64) {
	if !finiteVector(raw) {
		return
	}
	if !m.hasGyro || smoothing == 0 {
		m.gyro = raw
	} else {
		m.gyro = lerpVector(raw, m.gyro, smoothing)
	}
	m.hasGyro = true
}

// Step snapshots the latest smoothed sensor readings for the current game.Loop step.
// Calling Step multiple times with no new samples in between is idempotent.
func (m *MotionInput) Step() {
	m.snapTilt = m.tilt
	m.hasSnapTilt = m.hasTilt
	m.snapGyro = m.gyro
	m.hasSnapGyro = m.hasGyro
}

// Reset clears live and snapped sensor readings without changing options, calibration or the active stream.
func (m *MotionInput) Reset() {
	m.tilt = core.SensorVector{}
	m.hasTilt = false
	m.gyro = core.SensorVector{}
	m.hasGyro = false
	m.snapTilt = core.SensorVector{}
	m.hasSnapTilt = false
	m.snapGyro = core.SensorVector{}
	m.hasSnapGyro = false
	m.gapSeq++
}

// Calibrate captures the current tilt reading as the neutral hold orientation so TiltX, TiltY and TiltZ
// read 0 at that hold angle. If no tilt sample has arrived yet, the next tilt sample received is captured
// as the neutral orientation.
func (m *MotionInput) Calibrate() {
	if m.hasTilt {
		m.zeroTilt = m.tilt
		m.calibrated = true
		m.calibrateNext = false
		return
	}
	m.calibrateNext = true
}

// ResetCalibration clears any neutral tilt offset captured by Calibrate.
func (m *MotionInput) ResetCalibration() {
	m.zeroTilt = core.SensorVector{}
	m.calibrated = false
	m.calibrateNext = false
}

// Calibrated reports whether a neutral tilt offset has been captured.
func (m *MotionInput) Calibrated() bool {
	return m.calibrated
}

// Calibration returns the captured neutral tilt vector in g units.
func (m *MotionInput) Calibration() core.SensorVector {
	return m.zeroTilt
}

// Axis returns the stepped value of axis in [-1, 1] using m's default options.
func (m *MotionInput) Axis(axis MotionAxis) float64 {
	return m.AxisWith(axis, MotionAxisOptions{})
}

// AxisWith returns the stepped value of axis in [-1, 1] using per-axis opts, with zero fields
// falling back to m's defaults.
func (m *MotionInput) AxisWith(axis MotionAxis, opts MotionAxisOptions) float64 {
	if !axis.valid() {
		return 0
	}
	raw, ok := m.rawSnapshot(axis)
	if !ok {
		return 0
	}

	deadZone := opts.DeadZone
	if deadZone <= 0 {
		deadZone = m.opts.DeadZone
	}
	sensitivity := opts.Sensitivity
	if sensitivity <= 0 {
		sensitivity = m.opts.Sensitivity
	}
	if sensitivity <= 0 {
		sensitivity = 1
	}
	maxRange := opts.MaxRange
	if maxRange <= 0 {
		if axis.isTilt() {
			maxRange = m.opts.TiltRange
			if maxRange <= 0 {
				maxRange = 1
			}
		} else {
			maxRange = m.opts.GyroRange
			if maxRange <= 0 {
				maxRange = defaultGyroRange
			}
		}
	}

	return mapMotionValue(raw, deadZone, sensitivity, maxRange, opts.Invert)
}

func (m *MotionInput) rawSnapshot(axis MotionAxis) (float64, bool) {
	switch axis {
	case TiltX:
		if !m.hasSnapTilt {
			return 0, false
		}
		return m.snapTilt.X - m.zeroTilt.X, true
	case TiltY:
		if !m.hasSnapTilt {
			return 0, false
		}
		return m.snapTilt.Y - m.zeroTilt.Y, true
	case TiltZ:
		if !m.hasSnapTilt {
			return 0, false
		}
		return m.snapTilt.Z - m.zeroTilt.Z, true
	case GyroX:
		if !m.hasSnapGyro {
			return 0, false
		}
		return m.snapGyro.X, true
	case GyroY:
		if !m.hasSnapGyro {
			return 0, false
		}
		return m.snapGyro.Y, true
	case GyroZ:
		if !m.hasSnapGyro {
			return 0, false
		}
		return m.snapGyro.Z, true
	default:
		return 0, false
	}
}

// BindMotionAxis binds axis name on a to a tilt or gyroscope axis with opts.
// Calling BindMotionAxis with MotionAxisNone unbinds the motion axis for name.
// A motion axis may share a name with an opposite-action axis bound via BindAxis;
// Actions.Axis sums both contributions and clamps the result to [-1, 1].
func (a *Actions) BindMotionAxis(name string, axis MotionAxis, opts MotionAxisOptions) {
	if !axis.valid() {
		a.UnbindMotionAxis(name)
		return
	}
	if a.motionAxes == nil {
		a.motionAxes = make(map[string]motionAxisBinding)
	}
	a.motionAxes[name] = motionAxisBinding{axis: axis, opts: opts}
	delete(a.unboundMotionAxes, name)
	delete(a.motionInit, name)
}

// UnbindMotionAxis removes the motion axis binding for name. Save records the unbinding so a later Load keeps name unbound rather than restoring a default.
func (a *Actions) UnbindMotionAxis(name string) {
	delete(a.motionAxes, name)
	delete(a.motionSnap, name)
	delete(a.motionInit, name)
	if name != "" {
		if a.unboundMotionAxes == nil {
			a.unboundMotionAxes = make(map[string]bool)
		}
		a.unboundMotionAxes[name] = true
	}
}

// MotionAxisBinding returns the motion axis and options bound to name.
func (a *Actions) MotionAxisBinding(name string) (MotionAxis, MotionAxisOptions, bool) {
	mb, ok := a.motionAxes[name]
	return mb.axis, mb.opts, ok
}

// AttachMotion connects m as the motion sample source for a's motion axes.
// Passing nil detaches any existing MotionInput without stopping it.
func (a *Actions) AttachMotion(m *MotionInput) {
	a.motion = m
	clear(a.motionSnap)
	clear(a.motionInit)
	if m != nil {
		a.motionGap = m.gapSeq
	} else {
		a.motionGap = 0
	}
}

// Motion returns the MotionInput currently attached to a, or nil if none is attached.
func (a *Actions) Motion() *MotionInput {
	return a.motion
}

// WatchMotion creates a MotionInput with opts, starts watching device motion sensors on cx,
// attaches it to a, and returns it. Any previously attached stream on a's internal MotionInput
// is replaced (not stopped if owned externally). The new stream stops automatically when cx is disposed.
func (a *Actions) WatchMotion(cx *core.Scope, opts MotionOptions) (*MotionInput, error) {
	m, err := WatchMotion(cx, opts)
	if err != nil {
		return nil, err
	}
	a.AttachMotion(m)
	return m, nil
}

// OnMotion feeds a core.MotionSample directly into a's attached MotionInput (creating one lazily
// if none is attached). Useful for deterministic unit tests and input replays.
func (a *Actions) OnMotion(sample core.MotionSample) {
	a.ensureMotion().OnMotion(sample)
}

// CalibrateMotion captures the current tilt reading as the neutral hold angle on a's MotionInput.
func (a *Actions) CalibrateMotion() {
	a.ensureMotion().Calibrate()
}

// ResetMotionCalibration clears any neutral tilt offset on a's MotionInput.
func (a *Actions) ResetMotionCalibration() {
	if a.motion != nil {
		a.motion.ResetCalibration()
	}
}

func (a *Actions) ensureMotion() *MotionInput {
	if a.motion == nil {
		a.motion = NewMotionInput(MotionOptions{})
	}
	return a.motion
}

func (a *Actions) stepMotion() {
	if a.motion != nil {
		a.motion.Step()
		if a.motion.gapSeq != a.motionGap {
			clear(a.motionInit)
			a.motionGap = a.motion.gapSeq
		}
	}
	if len(a.motionAxes) == 0 {
		clear(a.motionSnap)
		clear(a.motionInit)
		return
	}
	if a.motionSnap == nil {
		a.motionSnap = make(map[string]float64, len(a.motionAxes))
	}
	if a.motionInit == nil {
		a.motionInit = make(map[string]bool, len(a.motionAxes))
	}
	for name, mb := range a.motionAxes {
		var target float64
		if a.motion != nil {
			target = a.motion.AxisWith(mb.axis, mb.opts)
		}
		smoothing := clampSmoothing(mb.opts.Smoothing)
		if smoothing == 0 || !a.motionInit[name] {
			a.motionSnap[name] = target
		} else {
			a.motionSnap[name] = clampUnit(a.motionSnap[name]*smoothing + target*(1-smoothing))
		}
		a.motionInit[name] = true
	}
}

func (a *Actions) resetMotion() {
	if a.motion != nil {
		a.motion.Reset()
		a.motionGap = a.motion.gapSeq
	}
	clear(a.motionSnap)
	clear(a.motionInit)
}

func mapMotionValue(raw, deadZone, sensitivity, maxRange float64, invert bool) float64 {
	if math.IsNaN(raw) || math.IsInf(raw, 0) {
		return 0
	}
	if maxRange <= 0 || math.IsNaN(maxRange) || math.IsInf(maxRange, 0) {
		maxRange = 1
	}
	if sensitivity <= 0 || math.IsNaN(sensitivity) || math.IsInf(sensitivity, 0) {
		sensitivity = 1
	}
	if deadZone < 0 || math.IsNaN(deadZone) {
		deadZone = 0
	}
	if deadZone >= 1 {
		return 0
	}
	v := (raw * sensitivity) / maxRange
	if invert {
		v = -v
	}
	abs := math.Abs(v)
	if abs <= deadZone {
		return 0
	}
	scaled := (abs - deadZone) / (1 - deadZone)
	if scaled > 1 {
		scaled = 1
	}
	if v < 0 {
		return -scaled
	}
	return scaled
}

func clampUnit(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampSmoothing(s float64) float64 {
	if math.IsNaN(s) || s <= 0 {
		return 0
	}
	if s >= 1 {
		return 0.999
	}
	return s
}

func rotateMotionVector(v core.SensorVector, orientation MotionOrientation) core.SensorVector {
	switch orientation {
	case MotionOrientationPortraitUpsideDown:
		return core.SensorVector{X: -v.X, Y: -v.Y, Z: v.Z}
	case MotionOrientationLandscapeLeft:
		return core.SensorVector{X: -v.Y, Y: v.X, Z: v.Z}
	case MotionOrientationLandscapeRight:
		return core.SensorVector{X: v.Y, Y: -v.X, Z: v.Z}
	default:
		return v
	}
}

func scaleVector(v core.SensorVector, k float64) core.SensorVector {
	return core.SensorVector{X: v.X * k, Y: v.Y * k, Z: v.Z * k}
}

func lerpVector(raw, prev core.SensorVector, smoothing float64) core.SensorVector {
	keep := smoothing
	take := 1 - smoothing
	return core.SensorVector{
		X: prev.X*keep + raw.X*take,
		Y: prev.Y*keep + raw.Y*take,
		Z: prev.Z*keep + raw.Z*take,
	}
}

func finiteVector(v core.SensorVector) bool {
	return !math.IsNaN(v.X) && !math.IsInf(v.X, 0) &&
		!math.IsNaN(v.Y) && !math.IsInf(v.Y, 0) &&
		!math.IsNaN(v.Z) && !math.IsInf(v.Z, 0)
}
