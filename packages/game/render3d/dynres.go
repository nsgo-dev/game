package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ErrInvalidDynamicResolution is wrapped by every error returned when
// validating [DynamicResolutionConfig] or configuring
// [DynamicResolutionScaler] and [DynamicResolutionTarget].
var ErrInvalidDynamicResolution = errors.New("render3d: invalid dynamic resolution config")

// Default values applied by [DefaultDynamicResolutionConfig] and when a zero
// field is supplied to [NewDynamicResolutionScaler].
const (
	// DefaultDynamicResolutionTargetFrameTime is the 60 FPS frame budget (~16.67ms).
	DefaultDynamicResolutionTargetFrameTime = time.Second / 60
	// DefaultDynamicResolutionMinScale is the default lower bound on linear scale (50% per axis, 25% pixels).
	DefaultDynamicResolutionMinScale float32 = 0.5
	// DefaultDynamicResolutionMaxScale is the default upper bound on linear scale (100% native).
	DefaultDynamicResolutionMaxScale float32 = 1.0
	// DefaultDynamicResolutionDeadband is the ±5% hysteresis deadband around TargetFrameTime.
	DefaultDynamicResolutionDeadband float32 = 0.05
	// DefaultDynamicResolutionSmoothingAlpha is the EMA smoothing factor for GPU frame times.
	DefaultDynamicResolutionSmoothingAlpha float32 = 0.25
	// DefaultDynamicResolutionStepDown is the maximum per-step scale drop under GPU overload.
	DefaultDynamicResolutionStepDown float32 = 0.12
	// DefaultDynamicResolutionStepUp is the maximum per-step scale recovery under GPU headroom.
	DefaultDynamicResolutionStepUp float32 = 0.04
	// DefaultDynamicResolutionCooldownDown is the minimum frame gap between consecutive scale-down steps.
	DefaultDynamicResolutionCooldownDown = 2
	// DefaultDynamicResolutionCooldownUp is the minimum consecutive headroom frames before stepping up.
	DefaultDynamicResolutionCooldownUp = 12
	// DefaultDynamicResolutionHitchLimit discards GPU frame times above 250ms as hitches.
	DefaultDynamicResolutionHitchLimit = 250 * time.Millisecond
	// DefaultDynamicResolutionAlignment rounds scaled dimensions to even pixel counts.
	DefaultDynamicResolutionAlignment = 2
	// DefaultDynamicResolutionSharpness is the default CAS sharpening strength for DynamicResolutionSharpen.
	DefaultDynamicResolutionSharpness float32 = 0.25
)

const (
	// dynResMinAdjustmentStep ensures mild sustained overload or headroom still makes measurable progress.
	dynResMinAdjustmentStep float32 = 0.01
	// dynResUniformSize is the 16-byte-aligned size of UpscaleParams in WGSL.
	dynResUniformSize = 48
)

// DynamicResolutionFilter selects how [DynamicResolutionTarget] resamples the
// scaled 3D render target when upscaling onto the native output surface.
type DynamicResolutionFilter uint8

const (
	// DynamicResolutionBilinear uses hardware bilinear filtering clamped to the
	// active scaled sub-viewport so out-of-viewport texels never bleed inward.
	DynamicResolutionBilinear DynamicResolutionFilter = iota
	// DynamicResolutionNearest uses nearest-neighbor point sampling for crisp
	// pixel-art or retro 3D presentation.
	DynamicResolutionNearest
	// DynamicResolutionSharpen applies bilinear upscaling followed by a
	// contrast-adaptive 5-tap sharpening pass clamped to the local
	// neighborhood min/max to restore detail without ringing halos.
	DynamicResolutionSharpen
)

// String returns the filter name: "bilinear", "nearest", or "sharpen".
func (f DynamicResolutionFilter) String() string {
	switch f {
	case DynamicResolutionBilinear:
		return "bilinear"
	case DynamicResolutionNearest:
		return "nearest"
	case DynamicResolutionSharpen:
		return "sharpen"
	default:
		return fmt.Sprintf("DynamicResolutionFilter(%d)", uint8(f))
	}
}

// Valid reports whether f is a recognized filter mode.
func (f DynamicResolutionFilter) Valid() bool {
	return f <= DynamicResolutionSharpen
}

// DynamicResolutionConfig configures [DynamicResolutionScaler] and
// [DynamicResolutionTarget]. Zero-valued numeric fields take defaults from
// [DefaultDynamicResolutionConfig].
type DynamicResolutionConfig struct {
	// TargetFrameTime is the GPU frame-time budget the scaler aims to hold.
	// Zero defaults to DefaultDynamicResolutionTargetFrameTime (~16.67ms, 60 FPS).
	TargetFrameTime time.Duration
	// MinScale is the lowest linear resolution scale in (0, 1].
	// Zero defaults to DefaultDynamicResolutionMinScale (0.5).
	MinScale float32
	// MaxScale is the highest linear resolution scale in [MinScale, 1].
	// Zero defaults to DefaultDynamicResolutionMaxScale (1.0).
	MaxScale float32
	// InitialScale optionally sets the starting scale in [MinScale, MaxScale].
	// Zero defaults to MaxScale.
	InitialScale float32
	// Deadband is the fractional hysteresis tolerance in [0, 0.5) around
	// TargetFrameTime inside which scale holds steady. Overload triggers above
	// TargetFrameTime*(1+Deadband); headroom triggers below TargetFrameTime*(1-Deadband).
	// Zero defaults to DefaultDynamicResolutionDeadband (0.05).
	Deadband float32
	// SmoothingAlpha is the exponential moving average weight in (0, 1] for
	// incoming GPU frame-time samples. Zero defaults to DefaultDynamicResolutionSmoothingAlpha (0.25).
	SmoothingAlpha float32
	// StepDown is the maximum linear scale reduction per adjustment step in (0, 1].
	// Zero defaults to DefaultDynamicResolutionStepDown (0.12).
	StepDown float32
	// StepUp is the maximum linear scale increase per adjustment step in (0, 1].
	// Zero defaults to DefaultDynamicResolutionStepUp (0.04).
	StepUp float32
	// CooldownDown is the minimum number of frames between consecutive
	// scale-down steps. Zero defaults to DefaultDynamicResolutionCooldownDown (2);
	// set 1 to permit stepping down on every consecutive overloaded frame.
	CooldownDown int
	// CooldownUp is the minimum number of consecutive headroom frames (and frames
	// since the last scale change) required before stepping up. Zero defaults to
	// DefaultDynamicResolutionCooldownUp (12); set 1 for immediate recovery.
	CooldownUp int
	// HitchLimit discards GPU frame-time outliers longer than HitchLimit as
	// loading or resume hitches. Zero defaults to DefaultDynamicResolutionHitchLimit (250ms);
	// a negative duration disables hitch filtering.
	HitchLimit time.Duration
	// Alignment rounds scaled widths and heights to a multiple of Alignment
	// (clamped to [1, native]) whenever scale < 1. Zero defaults to
	// DefaultDynamicResolutionAlignment (2); 1 disables alignment rounding.
	Alignment int
	// Filter selects the upscaling filter used by [DynamicResolutionTarget].
	Filter DynamicResolutionFilter
	// Sharpness controls contrast-adaptive sharpening strength in [0, 1] when
	// Filter is DynamicResolutionSharpen. Zero defaults to DefaultDynamicResolutionSharpness (0.25).
	Sharpness float32
}

// DefaultDynamicResolutionConfig returns the default 60 FPS dynamic resolution
// configuration (scale in [0.5, 1.0], ±5% deadband, fast scale-down, smooth
// scale-up, even pixel alignment, bilinear upscaling).
func DefaultDynamicResolutionConfig() DynamicResolutionConfig {
	return DynamicResolutionConfig{
		TargetFrameTime: DefaultDynamicResolutionTargetFrameTime,
		MinScale:        DefaultDynamicResolutionMinScale,
		MaxScale:        DefaultDynamicResolutionMaxScale,
		InitialScale:    DefaultDynamicResolutionMaxScale,
		Deadband:        DefaultDynamicResolutionDeadband,
		SmoothingAlpha:  DefaultDynamicResolutionSmoothingAlpha,
		StepDown:        DefaultDynamicResolutionStepDown,
		StepUp:          DefaultDynamicResolutionStepUp,
		CooldownDown:    DefaultDynamicResolutionCooldownDown,
		CooldownUp:      DefaultDynamicResolutionCooldownUp,
		HitchLimit:      DefaultDynamicResolutionHitchLimit,
		Alignment:       DefaultDynamicResolutionAlignment,
		Filter:          DynamicResolutionBilinear,
		Sharpness:       DefaultDynamicResolutionSharpness,
	}
}

func (c DynamicResolutionConfig) normalized() DynamicResolutionConfig {
	out := c
	if out.TargetFrameTime == 0 {
		out.TargetFrameTime = DefaultDynamicResolutionTargetFrameTime
	}
	if out.MinScale == 0 {
		out.MinScale = DefaultDynamicResolutionMinScale
	}
	if out.MaxScale == 0 {
		out.MaxScale = DefaultDynamicResolutionMaxScale
	}
	if out.InitialScale == 0 {
		out.InitialScale = out.MaxScale
	}
	if out.Deadband == 0 {
		out.Deadband = DefaultDynamicResolutionDeadband
	}
	if out.SmoothingAlpha == 0 {
		out.SmoothingAlpha = DefaultDynamicResolutionSmoothingAlpha
	}
	if out.StepDown == 0 {
		out.StepDown = DefaultDynamicResolutionStepDown
	}
	if out.StepUp == 0 {
		out.StepUp = DefaultDynamicResolutionStepUp
	}
	if out.CooldownDown == 0 {
		out.CooldownDown = DefaultDynamicResolutionCooldownDown
	}
	if out.CooldownUp == 0 {
		out.CooldownUp = DefaultDynamicResolutionCooldownUp
	}
	if out.HitchLimit == 0 {
		out.HitchLimit = DefaultDynamicResolutionHitchLimit
	}
	if out.Alignment == 0 {
		out.Alignment = DefaultDynamicResolutionAlignment
	}
	if out.Sharpness == 0 {
		out.Sharpness = DefaultDynamicResolutionSharpness
	}
	return out
}

// Validate checks c after applying zero-value defaults. Every error wraps
// [ErrInvalidDynamicResolution].
func (c DynamicResolutionConfig) Validate() error {
	if c.TargetFrameTime < 0 {
		return fmt.Errorf("%w: target frame time %v must be positive", ErrInvalidDynamicResolution, c.TargetFrameTime)
	}
	if c.MinScale < 0 || !dynResFinite32(c.MinScale) {
		return fmt.Errorf("%w: min scale %v outside (0, 1]", ErrInvalidDynamicResolution, c.MinScale)
	}
	if c.MaxScale < 0 || !dynResFinite32(c.MaxScale) {
		return fmt.Errorf("%w: max scale %v outside (0, 1]", ErrInvalidDynamicResolution, c.MaxScale)
	}
	if c.InitialScale < 0 || !dynResFinite32(c.InitialScale) {
		return fmt.Errorf("%w: initial scale %v outside [min, max]", ErrInvalidDynamicResolution, c.InitialScale)
	}
	if c.Deadband < 0 || !dynResFinite32(c.Deadband) {
		return fmt.Errorf("%w: deadband %v outside [0, 0.5)", ErrInvalidDynamicResolution, c.Deadband)
	}
	if c.SmoothingAlpha < 0 || !dynResFinite32(c.SmoothingAlpha) {
		return fmt.Errorf("%w: smoothing alpha %v outside (0, 1]", ErrInvalidDynamicResolution, c.SmoothingAlpha)
	}
	if c.StepDown < 0 || !dynResFinite32(c.StepDown) {
		return fmt.Errorf("%w: step down %v outside (0, 1]", ErrInvalidDynamicResolution, c.StepDown)
	}
	if c.StepUp < 0 || !dynResFinite32(c.StepUp) {
		return fmt.Errorf("%w: step up %v outside (0, 1]", ErrInvalidDynamicResolution, c.StepUp)
	}
	if c.CooldownDown < 0 {
		return fmt.Errorf("%w: cooldown down %d must be non-negative", ErrInvalidDynamicResolution, c.CooldownDown)
	}
	if c.CooldownUp < 0 {
		return fmt.Errorf("%w: cooldown up %d must be non-negative", ErrInvalidDynamicResolution, c.CooldownUp)
	}
	if c.Alignment < 0 {
		return fmt.Errorf("%w: alignment %d must be non-negative", ErrInvalidDynamicResolution, c.Alignment)
	}
	if c.Sharpness < 0 || !dynResFinite32(c.Sharpness) {
		return fmt.Errorf("%w: sharpness %v outside [0, 1]", ErrInvalidDynamicResolution, c.Sharpness)
	}
	if !c.Filter.Valid() {
		return fmt.Errorf("%w: unknown filter %v", ErrInvalidDynamicResolution, c.Filter)
	}

	n := c.normalized()
	if n.TargetFrameTime <= 0 {
		return fmt.Errorf("%w: target frame time %v must be positive", ErrInvalidDynamicResolution, n.TargetFrameTime)
	}
	if n.MinScale <= 0 || n.MinScale > 1 {
		return fmt.Errorf("%w: min scale %v outside (0, 1]", ErrInvalidDynamicResolution, n.MinScale)
	}
	if n.MaxScale <= 0 || n.MaxScale > 1 {
		return fmt.Errorf("%w: max scale %v outside (0, 1]", ErrInvalidDynamicResolution, n.MaxScale)
	}
	if n.MinScale > n.MaxScale {
		return fmt.Errorf("%w: min scale %v exceeds max scale %v", ErrInvalidDynamicResolution, n.MinScale, n.MaxScale)
	}
	if n.InitialScale < n.MinScale || n.InitialScale > n.MaxScale {
		return fmt.Errorf("%w: initial scale %v outside [%v, %v]", ErrInvalidDynamicResolution, n.InitialScale, n.MinScale, n.MaxScale)
	}
	if n.Deadband >= 0.5 {
		return fmt.Errorf("%w: deadband %v must be < 0.5", ErrInvalidDynamicResolution, n.Deadband)
	}
	if n.SmoothingAlpha <= 0 || n.SmoothingAlpha > 1 {
		return fmt.Errorf("%w: smoothing alpha %v outside (0, 1]", ErrInvalidDynamicResolution, n.SmoothingAlpha)
	}
	if n.StepDown <= 0 || n.StepDown > 1 {
		return fmt.Errorf("%w: step down %v outside (0, 1]", ErrInvalidDynamicResolution, n.StepDown)
	}
	if n.StepUp <= 0 || n.StepUp > 1 {
		return fmt.Errorf("%w: step up %v outside (0, 1]", ErrInvalidDynamicResolution, n.StepUp)
	}
	if n.Sharpness > 1 {
		return fmt.Errorf("%w: sharpness %v outside [0, 1]", ErrInvalidDynamicResolution, n.Sharpness)
	}
	if n.HitchLimit > 0 && n.HitchLimit <= n.TargetFrameTime {
		return fmt.Errorf("%w: hitch limit %v must exceed target frame time %v", ErrInvalidDynamicResolution, n.HitchLimit, n.TargetFrameTime)
	}
	return nil
}

// DynamicResolutionStats captures a snapshot of a [DynamicResolutionScaler]'s
// current scale, frame-time metrics, and adjustment counters.
type DynamicResolutionStats struct {
	Scale             float32
	MinScale          float32
	MaxScale          float32
	Enabled           bool
	Fixed             bool
	LastFrameTime     time.Duration
	SmoothedFrameTime time.Duration
	TargetFrameTime   time.Duration
	FramesRecorded    uint64
	HitchesIgnored    uint64
	ScaleDowns        uint64
	ScaleUps          uint64
}

// DynamicResolutionScaler adapts a linear 3D render resolution scale in
// [MinScale, MaxScale] to hold a GPU frame-time target (#383).
//
// Because fill-rate cost grows quadratically with linear scale (area = scale²),
// the controller scales proportionally toward scale * sqrt(target / smoothed)
// with a symmetric hysteresis deadband around TargetFrameTime, fast response on
// overload (CooldownDown, StepDown), and conservative recovery under sustained
// headroom (CooldownUp, StepUp).
//
// DynamicResolutionScaler is pure Go, deterministic, and independent of the GPU
// device so it can be driven from GPU pass timings, frame timers, or unit tests.
// It is not safe for concurrent use; call it from the render/loop goroutine.
type DynamicResolutionScaler struct {
	cfg               DynamicResolutionConfig
	enabled           bool
	fixedScale        float32 // 0 means automatic adaptation
	scale             float32
	smoothedNs        float64
	hasSmoothed       bool
	lastFrameTime     time.Duration
	framesSinceChange int
	headroomStreak    int
	framesRecorded    uint64
	hitchesIgnored    uint64
	scaleDowns        uint64
	scaleUps          uint64
}

// NewDynamicResolutionScaler creates a scaler from cfg (applying defaults for
// zero fields). It returns an error wrapping [ErrInvalidDynamicResolution] if
// cfg is invalid.
func NewDynamicResolutionScaler(cfg DynamicResolutionConfig) (*DynamicResolutionScaler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	norm := cfg.normalized()
	return &DynamicResolutionScaler{
		cfg:               norm,
		enabled:           true,
		scale:             norm.InitialScale,
		framesSinceChange: norm.CooldownDown,
	}, nil
}

// Config returns the normalized configuration in use by s.
func (s *DynamicResolutionScaler) Config() DynamicResolutionConfig {
	if s == nil {
		return DefaultDynamicResolutionConfig()
	}
	return s.cfg
}

// Enabled reports whether dynamic resolution scaling is currently active.
func (s *DynamicResolutionScaler) Enabled() bool {
	if s == nil {
		return false
	}
	return s.enabled
}

// SetEnabled enables or disables dynamic resolution scaling. When disabled,
// Scale returns 1.0 (native resolution) and RecordFrameTime makes no scale
// adjustments.
func (s *DynamicResolutionScaler) SetEnabled(enabled bool) {
	if s == nil {
		return
	}
	s.enabled = enabled
}

// SetFixedScale pins the reported scale to a fixed value while dynamic scaling
// is enabled, disabling automatic frame-time adjustments. Passing a value <= 0
// clears the override and resumes automatic adaptation. Positive values are
// clamped to [MinScale, MaxScale].
func (s *DynamicResolutionScaler) SetFixedScale(scale float32) {
	if s == nil {
		return
	}
	if !dynResFinite32(scale) || scale <= 0 {
		s.fixedScale = 0
		return
	}
	s.fixedScale = dynResClamp32(scale, s.cfg.MinScale, s.cfg.MaxScale)
}

// ClearFixedScale removes any pinned scale set by [SetFixedScale] and resumes
// automatic adaptation.
func (s *DynamicResolutionScaler) ClearFixedScale() {
	if s == nil {
		return
	}
	s.fixedScale = 0
}

// SetScaleBounds updates MinScale and MaxScale at runtime (for example, when a
// quality.Selector switches presets and caps MaxScale to Preset.ResolutionScale).
// Existing scale and fixedScale values are clamped into the new bounds.
func (s *DynamicResolutionScaler) SetScaleBounds(minScale, maxScale float32) error {
	if s == nil {
		return fmt.Errorf("%w: nil scaler", ErrInvalidDynamicResolution)
	}
	if !dynResFinite32(minScale) || minScale <= 0 || minScale > 1 {
		return fmt.Errorf("%w: min scale %v outside (0, 1]", ErrInvalidDynamicResolution, minScale)
	}
	if !dynResFinite32(maxScale) || maxScale <= 0 || maxScale > 1 {
		return fmt.Errorf("%w: max scale %v outside (0, 1]", ErrInvalidDynamicResolution, maxScale)
	}
	if minScale > maxScale {
		return fmt.Errorf("%w: min scale %v exceeds max scale %v", ErrInvalidDynamicResolution, minScale, maxScale)
	}
	s.cfg.MinScale = minScale
	s.cfg.MaxScale = maxScale
	s.cfg.InitialScale = dynResClamp32(s.cfg.InitialScale, minScale, maxScale)
	s.scale = dynResClamp32(s.scale, minScale, maxScale)
	if s.fixedScale > 0 {
		s.fixedScale = dynResClamp32(s.fixedScale, minScale, maxScale)
	}
	return nil
}

// Scale returns the active linear resolution scale in (0, 1].
// It returns 1.0 when s is nil or disabled, the pinned value when a fixed scale
// is set, or the adaptive scale otherwise.
func (s *DynamicResolutionScaler) Scale() float32 {
	if s == nil || !s.enabled {
		return 1.0
	}
	if s.fixedScale > 0 {
		return s.fixedScale
	}
	return s.scale
}

// SmoothedFrameTime returns the current exponential moving average of recorded
// GPU frame times, or 0 before any valid sample has been recorded.
func (s *DynamicResolutionScaler) SmoothedFrameTime() time.Duration {
	if s == nil || !s.hasSmoothed {
		return 0
	}
	return time.Duration(math.Round(s.smoothedNs))
}

// Stats returns a point-in-time snapshot of s's scale, timings, and counters.
func (s *DynamicResolutionScaler) Stats() DynamicResolutionStats {
	if s == nil {
		return DynamicResolutionStats{Scale: 1.0}
	}
	return DynamicResolutionStats{
		Scale:             s.Scale(),
		MinScale:          s.cfg.MinScale,
		MaxScale:          s.cfg.MaxScale,
		Enabled:           s.enabled,
		Fixed:             s.fixedScale > 0,
		LastFrameTime:     s.lastFrameTime,
		SmoothedFrameTime: s.SmoothedFrameTime(),
		TargetFrameTime:   s.cfg.TargetFrameTime,
		FramesRecorded:    s.framesRecorded,
		HitchesIgnored:    s.hitchesIgnored,
		ScaleDowns:        s.scaleDowns,
		ScaleUps:          s.scaleUps,
	}
}

// Reset restores the adaptive scale to InitialScale and clears smoothed timing,
// cooldowns, and counters. Enabled and fixed-scale overrides are preserved.
func (s *DynamicResolutionScaler) Reset() {
	if s == nil {
		return
	}
	s.scale = s.cfg.InitialScale
	s.smoothedNs = 0
	s.hasSmoothed = false
	s.lastFrameTime = 0
	s.framesSinceChange = s.cfg.CooldownDown
	s.headroomStreak = 0
	s.framesRecorded = 0
	s.hitchesIgnored = 0
	s.scaleDowns = 0
	s.scaleUps = 0
}

// ScaledSize returns the scaled 3D pixel dimensions for a native output target
// of size (nativeW, nativeH) using the current [Scale] and configured
// alignment. Returns (0, 0) when either native dimension is non-positive.
// When Scale() >= 1, (nativeW, nativeH) is returned unchanged.
func (s *DynamicResolutionScaler) ScaledSize(nativeW, nativeH int) (w, h int) {
	if nativeW <= 0 || nativeH <= 0 {
		return 0, 0
	}
	scale := s.Scale()
	if scale >= 1.0 {
		return nativeW, nativeH
	}
	align := DefaultDynamicResolutionAlignment
	if s != nil {
		align = s.cfg.Alignment
	}
	return dynResScaleDim(nativeW, scale, align), dynResScaleDim(nativeH, scale, align)
}

func dynResScaleDim(native int, scale float32, align int) int {
	scaled := int(math.Round(float64(native) * float64(scale)))
	if align > 1 {
		scaled = ((scaled + align/2) / align) * align
		if scaled < align && native >= align {
			scaled = align
		}
	}
	if scaled < 1 {
		scaled = 1
	}
	if scaled > native {
		scaled = native
	}
	return scaled
}

// RecordFrameTime updates the smoothed GPU frame time with gpuFrameTime and
// adapts Scale() to hold TargetFrameTime, returning the resulting Scale().
//
// Non-positive durations are ignored. Durations exceeding HitchLimit (when
// HitchLimit > 0) are counted as ignored hitches and do not disturb the EMA or
// resolution scale. When disabled or pinned via SetFixedScale, the current
// Scale() is returned without modifying adaptive scale.
func (s *DynamicResolutionScaler) RecordFrameTime(gpuFrameTime time.Duration) float32 {
	if s == nil {
		return 1.0
	}
	if gpuFrameTime <= 0 {
		return s.Scale()
	}
	if s.cfg.HitchLimit > 0 && gpuFrameTime > s.cfg.HitchLimit {
		s.hitchesIgnored++
		return s.Scale()
	}

	s.lastFrameTime = gpuFrameTime
	s.framesRecorded++

	sampleNs := float64(gpuFrameTime)
	if !s.hasSmoothed {
		s.smoothedNs = sampleNs
		s.hasSmoothed = true
	} else {
		alpha := float64(s.cfg.SmoothingAlpha)
		s.smoothedNs = alpha*sampleNs + (1.0-alpha)*s.smoothedNs
	}

	if !s.enabled || s.fixedScale > 0 {
		return s.Scale()
	}

	s.framesSinceChange++

	targetNs := float64(s.cfg.TargetFrameTime)
	deadband := float64(s.cfg.Deadband)
	highThreshold := targetNs * (1.0 + deadband)
	lowThreshold := targetNs * (1.0 - deadband)

	switch {
	case s.smoothedNs > highThreshold:
		s.headroomStreak = 0
		if s.scale <= s.cfg.MinScale || s.framesSinceChange < s.cfg.CooldownDown {
			return s.scale
		}
		// Fill cost scales with scale², so ideal scale ratio is sqrt(target / smoothed).
		ratio := math.Sqrt(targetNs / s.smoothedNs)
		desired := float32(float64(s.scale) * ratio)
		delta := s.scale - desired
		if delta < dynResMinAdjustmentStep {
			delta = dynResMinAdjustmentStep
		}
		if delta > s.cfg.StepDown {
			delta = s.cfg.StepDown
		}
		next := dynResClamp32(s.scale-delta, s.cfg.MinScale, s.cfg.MaxScale)
		if next < s.scale {
			s.scale = next
			s.scaleDowns++
			s.framesSinceChange = 0
		}

	case s.smoothedNs < lowThreshold:
		s.headroomStreak++
		if s.scale >= s.cfg.MaxScale {
			return s.scale
		}
		if s.headroomStreak < s.cfg.CooldownUp || s.framesSinceChange < s.cfg.CooldownUp {
			return s.scale
		}
		ratio := math.Sqrt(targetNs / s.smoothedNs)
		desired := float32(float64(s.scale) * ratio)
		delta := desired - s.scale
		if delta < dynResMinAdjustmentStep {
			delta = dynResMinAdjustmentStep
		}
		if delta > s.cfg.StepUp {
			delta = s.cfg.StepUp
		}
		next := dynResClamp32(s.scale+delta, s.cfg.MinScale, s.cfg.MaxScale)
		if next > s.scale {
			s.scale = next
			s.scaleUps++
			s.framesSinceChange = 0
			s.headroomStreak = 0
		}

	default:
		// Inside the hysteresis deadband: hold steady and reset recovery streak.
		s.headroomStreak = 0
	}

	return s.scale
}

// RecordPassTimings sums GPU pass durations from timings (for example from
// [gpu.Device.LastPassTimings] or [gpu.Frame.PassTimings]) and feeds the total
// into [RecordFrameTime]. When labels are provided, only passes whose Label
// matches one of labels are summed; when no labels are provided, all positive
// pass durations are summed.
func (s *DynamicResolutionScaler) RecordPassTimings(timings []gpu.PassTiming, labels ...string) float32 {
	if s == nil {
		return 1.0
	}
	var total time.Duration
	for _, pt := range timings {
		if pt.Duration <= 0 {
			continue
		}
		if len(labels) > 0 && !slices.Contains(labels, pt.Label) {
			continue
		}
		total += pt.Duration
	}
	if total <= 0 {
		return s.Scale()
	}
	return s.RecordFrameTime(total)
}

// RenderPassRecorder is satisfied by both [*gpu.Encoder] and [*gpu.Frame],
// allowing [DynamicResolutionTarget] to drive standalone software renderers as
// well as live [gpu.View] frame callbacks.
type RenderPassRecorder interface {
	BeginRenderPass(desc gpu.RenderPassDesc) *gpu.RenderPass
}

// DynamicResolutionTargetOptions configures [NewDynamicResolutionTarget].
type DynamicResolutionTargetOptions struct {
	// Label prefixes GPU resource labels for capture tools. Defaults to "render3d.dynres".
	Label string
	// ColorFormat is the offscreen 3D color attachment format.
	// Defaults to gpu.FormatRGBA8Unorm when FormatUndefined.
	ColorFormat gpu.TextureFormat
	// DepthFormat is the offscreen 3D depth attachment format.
	// Defaults to gpu.FormatDepth24Plus when FormatUndefined unless DisableDepth is true.
	DepthFormat gpu.TextureFormat
	// DisableDepth creates a color-only offscreen target with no depth attachment.
	DisableDepth bool
	// OutputFormat is the color format of the native target passed to Upscale.
	// Defaults to dev.SurfaceFormat() (or gpu.FormatRGBA8Unorm when undefined).
	OutputFormat gpu.TextureFormat
	// TimingLabel optionally sets RenderPassDesc.TimingLabel on Begin3D passes so
	// GPU pass timings can be matched in RecordPassTimings.
	TimingLabel string
	// Config configures the internal scaler when Scaler is nil.
	Config DynamicResolutionConfig
	// Scaler optionally supplies an external [DynamicResolutionScaler] to share
	// or control directly. When nil, a new scaler is constructed from Config.
	Scaler *DynamicResolutionScaler
}

// dynResBlitWGSL performs single-tap bilinear or nearest upscaling from the
// active sub-viewport [0..scaledW, 0..scaledH] of the offscreen color texture,
// clamping UVs to half-texel inset bounds so unused padding texels never bleed.
const dynResBlitWGSL = `
struct UpscaleParams {
	uv_scale: vec2<f32>,
	uv_min: vec2<f32>,
	uv_max: vec2<f32>,
	texel_size: vec2<f32>,
	sharpness: vec4<f32>,
}

@group(0) @binding(0) var<uniform> params: UpscaleParams;
@group(0) @binding(1) var src_tex: texture_2d<f32>;
@group(0) @binding(2) var src_samp: sampler;

struct VertexIn {
	@location(0) pos: vec2<f32>,
	@location(1) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) uv: vec2<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = vec4<f32>(in.pos, 0.0, 1.0);
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let uv = clamp(in.uv * params.uv_scale, params.uv_min, params.uv_max);
	return textureSample(src_tex, src_samp, uv);
}
`

// dynResSharpenWGSL performs bilinear upscaling followed by a contrast-adaptive
// 5-tap unsharp filter clamped to the local neighborhood min/max (CAS-style),
// avoiding ringing halos and using only algebraic ops supported by every backend
// including NSGo's software GPU VM.
const dynResSharpenWGSL = `
struct UpscaleParams {
	uv_scale: vec2<f32>,
	uv_min: vec2<f32>,
	uv_max: vec2<f32>,
	texel_size: vec2<f32>,
	sharpness: vec4<f32>,
}

@group(0) @binding(0) var<uniform> params: UpscaleParams;
@group(0) @binding(1) var src_tex: texture_2d<f32>;
@group(0) @binding(2) var src_samp: sampler;

struct VertexIn {
	@location(0) pos: vec2<f32>,
	@location(1) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) uv: vec2<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = vec4<f32>(in.pos, 0.0, 1.0);
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let uv = clamp(in.uv * params.uv_scale, params.uv_min, params.uv_max);
	let c = textureSample(src_tex, src_samp, uv);
	let n = textureSample(src_tex, src_samp, clamp(uv + vec2<f32>(0.0, -params.texel_size.y), params.uv_min, params.uv_max)).rgb;
	let s = textureSample(src_tex, src_samp, clamp(uv + vec2<f32>(0.0,  params.texel_size.y), params.uv_min, params.uv_max)).rgb;
	let e = textureSample(src_tex, src_samp, clamp(uv + vec2<f32>( params.texel_size.x, 0.0), params.uv_min, params.uv_max)).rgb;
	let w = textureSample(src_tex, src_samp, clamp(uv + vec2<f32>(-params.texel_size.x, 0.0), params.uv_min, params.uv_max)).rgb;
	let mn = min(c.rgb, min(min(n, s), min(e, w)));
	let mx = max(c.rgb, max(max(n, s), max(e, w)));
	let avg = (n + s + e + w) * 0.25;
	let sharp = clamp(c.rgb + (c.rgb - avg) * params.sharpness.x, mn, mx);
	return vec4<f32>(sharp, c.a);
}
`

// DynamicResolutionTarget manages an offscreen 3D render target and a
// full-screen upscale pass so the 3D scene renders at an adaptive resolution
// scale while the native swapchain and 2D HUD overlays (such as scene.Overlay3D
// or a follow-up native GPU HUD pass) stay at crisp 1:1 native resolution (#383).
//
// To avoid GPU texture reallocation churn as the resolution scale fluctuates
// frame to frame, the offscreen RenderTarget is allocated at native capacity
// and 3D rendering is restricted via SetViewport and SetScissor to the active
// scaled sub-rectangle [0, 0, scaledW, scaledH]. The upscale pass maps that
// sub-rectangle across the full native viewport with half-texel UV clamping so
// unused padding texels never bleed into the image.
type DynamicResolutionTarget struct {
	cx           *core.Scope
	dev          *gpu.Device
	scaler       *DynamicResolutionScaler
	label        string
	timingLabel  string
	colorFormat  gpu.TextureFormat
	depthFormat  gpu.TextureFormat
	outputFormat gpu.TextureFormat
	filter       DynamicResolutionFilter
	sharpness    float32

	quadVB      *gpu.Buffer
	paramBuf    *gpu.Buffer
	layout      *gpu.BindGroupLayout
	linearSamp  *gpu.Sampler
	nearestSamp *gpu.Sampler
	blitPipe    *gpu.Pipeline
	sharpenPipe *gpu.Pipeline

	rt        *gpu.RenderTarget
	linearBG  *gpu.BindGroup
	nearestBG *gpu.BindGroup
	allocW    int
	allocH    int

	lastNativeW int
	lastNativeH int
	lastScaledW int
	lastScaledH int
	closed      bool

	scratch [dynResUniformSize]byte
}

// NewDynamicResolutionTarget creates the upscale pipelines, static quad buffer,
// and samplers on dev owned by cx (nil for caller-closed resources). The
// offscreen render target itself is allocated lazily on the first [Begin3D] or
// [EnsureCapacity] call once native dimensions are known.
func NewDynamicResolutionTarget(cx *core.Scope, dev *gpu.Device, opts DynamicResolutionTargetOptions) (*DynamicResolutionTarget, error) {
	if dev == nil {
		return nil, fmt.Errorf("%w: nil gpu.Device", ErrInvalidDynamicResolution)
	}
	scaler := opts.Scaler
	if scaler == nil {
		var err error
		scaler, err = NewDynamicResolutionScaler(opts.Config)
		if err != nil {
			return nil, err
		}
	}

	label := opts.Label
	if label == "" {
		label = "render3d.dynres"
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = gpu.FormatRGBA8Unorm
	}
	depthFmt := opts.DepthFormat
	if opts.DisableDepth {
		depthFmt = gpu.FormatUndefined
	} else if depthFmt == gpu.FormatUndefined {
		depthFmt = gpu.FormatDepth24Plus
	}
	outFmt := opts.OutputFormat
	if outFmt == gpu.FormatUndefined {
		outFmt = dev.SurfaceFormat()
		if outFmt == gpu.FormatUndefined {
			outFmt = gpu.FormatRGBA8Unorm
		}
	}

	cfg := scaler.Config()
	filter := cfg.Filter
	if !filter.Valid() {
		return nil, fmt.Errorf("%w: unknown filter %v", ErrInvalidDynamicResolution, filter)
	}
	sharpness := cfg.Sharpness

	// Fullscreen quad: 6 vertices, pos(x,y) + uv(u,v).
	// Top-left (-1, +1) maps to UV (0, 0), matching top-left render viewport (0, 0).
	quadVerts := dynResPackFloat32s(
		-1, -1, 0, 1,
		1, -1, 1, 1,
		1, 1, 1, 0,
		-1, -1, 0, 1,
		1, 1, 1, 0,
		-1, 1, 0, 0,
	)

	var created []interface{ Close() error }
	cleanupOnErr := func() {
		for i := len(created) - 1; i >= 0; i-- {
			_ = created[i].Close()
		}
	}

	quadVB, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".quad",
		Usage:    gpu.UsageVertex,
		Data:     quadVerts,
		Retained: true,
	})
	if err != nil {
		return nil, err
	}
	created = append(created, quadVB)

	paramBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".params",
		Size:     dynResUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: true,
	})
	if err != nil {
		cleanupOnErr()
		return nil, err
	}
	created = append(created, paramBuf)

	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		cleanupOnErr()
		return nil, err
	}
	created = append(created, layout)

	linearSamp, err := dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + ".linear",
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterNearest,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     true,
	})
	if err != nil {
		cleanupOnErr()
		return nil, err
	}
	created = append(created, linearSamp)

	nearestSamp, err := dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + ".nearest",
		MinFilter:    gpu.FilterNearest,
		MagFilter:    gpu.FilterNearest,
		MipmapFilter: gpu.FilterNearest,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     true,
	})
	if err != nil {
		cleanupOnErr()
		return nil, err
	}
	created = append(created, nearestSamp)

	blitMod, err := gpu.CompileWGSL(dynResBlitWGSL)
	if err != nil {
		cleanupOnErr()
		return nil, fmt.Errorf("render3d: compile dynamic resolution blit shader: %w", err)
	}
	sharpenMod, err := gpu.CompileWGSL(dynResSharpenWGSL)
	if err != nil {
		cleanupOnErr()
		return nil, fmt.Errorf("render3d: compile dynamic resolution sharpen shader: %w", err)
	}

	makePipe := func(pipeLabel string, mod *gpu.ShaderModule) (*gpu.Pipeline, error) {
		return dev.CreatePipeline(cx, gpu.PipelineDesc{
			Label:    pipeLabel,
			Vertex:   gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
			Fragment: gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
			Layouts:  []*gpu.BindGroupLayout{layout},
			VertexBuffers: []gpu.VertexBufferLayout{{
				Stride: 4 * 4,
				Attributes: []gpu.VertexAttribute{
					{Location: 0, Offset: 0, Format: gpu.VertexFloat32x2},
					{Location: 1, Offset: 2 * 4, Format: gpu.VertexFloat32x2},
				},
			}},
			Primitive: gpu.PrimitiveState{
				Topology:  gpu.TopologyTriangleList,
				FrontFace: gpu.FrontCCW,
				CullMode:  gpu.CullNone,
			},
			Targets: []gpu.ColorTargetState{{
				Format:    outFmt,
				WriteMask: gpu.ColorWriteAll,
			}},
		})
	}

	blitPipe, err := makePipe(label+".blit", blitMod)
	if err != nil {
		cleanupOnErr()
		return nil, err
	}
	created = append(created, blitPipe)

	sharpenPipe, err := makePipe(label+".sharpen", sharpenMod)
	if err != nil {
		cleanupOnErr()
		return nil, err
	}
	created = append(created, sharpenPipe)

	return &DynamicResolutionTarget{
		cx:           cx,
		dev:          dev,
		scaler:       scaler,
		label:        label,
		timingLabel:  opts.TimingLabel,
		colorFormat:  colorFmt,
		depthFormat:  depthFmt,
		outputFormat: outFmt,
		filter:       filter,
		sharpness:    sharpness,
		quadVB:       quadVB,
		paramBuf:     paramBuf,
		layout:       layout,
		linearSamp:   linearSamp,
		nearestSamp:  nearestSamp,
		blitPipe:     blitPipe,
		sharpenPipe:  sharpenPipe,
	}, nil
}

// Scaler returns the [DynamicResolutionScaler] controlling t.
func (t *DynamicResolutionTarget) Scaler() *DynamicResolutionScaler {
	if t == nil {
		return nil
	}
	return t.scaler
}

// Scale returns the current linear resolution scale from the target's scaler.
func (t *DynamicResolutionTarget) Scale() float32 {
	if t == nil || t.scaler == nil {
		return 1.0
	}
	return t.scaler.Scale()
}

// RecordFrameTime forwards gpuFrameTime to the target's scaler and returns the
// updated linear resolution scale.
func (t *DynamicResolutionTarget) RecordFrameTime(gpuFrameTime time.Duration) float32 {
	if t == nil || t.scaler == nil {
		return 1.0
	}
	return t.scaler.RecordFrameTime(gpuFrameTime)
}

// RecordPassTimings forwards GPU pass timings to the target's scaler. When no
// labels are supplied and t was configured with a non-empty TimingLabel, that
// TimingLabel is matched automatically.
func (t *DynamicResolutionTarget) RecordPassTimings(timings []gpu.PassTiming, labels ...string) float32 {
	if t == nil || t.scaler == nil {
		return 1.0
	}
	if len(labels) == 0 && t.timingLabel != "" {
		return t.scaler.RecordPassTimings(timings, t.timingLabel)
	}
	return t.scaler.RecordPassTimings(timings, labels...)
}

// Filter returns the active upscale sampling mode.
func (t *DynamicResolutionTarget) Filter() DynamicResolutionFilter {
	if t == nil {
		return DynamicResolutionBilinear
	}
	return t.filter
}

// SetFilter changes the upscale sampling mode without reallocating GPU resources.
func (t *DynamicResolutionTarget) SetFilter(f DynamicResolutionFilter) {
	if t == nil || !f.Valid() {
		return
	}
	t.filter = f
}

// Sharpness returns the contrast-adaptive sharpening strength used when Filter
// is [DynamicResolutionSharpen].
func (t *DynamicResolutionTarget) Sharpness() float32 {
	if t == nil {
		return 0
	}
	return t.sharpness
}

// SetSharpness updates the sharpening strength in [0, 1] for
// [DynamicResolutionSharpen]. Non-finite inputs are ignored.
func (t *DynamicResolutionTarget) SetSharpness(s float32) {
	if t == nil || !dynResFinite32(s) {
		return
	}
	t.sharpness = dynResClamp32(s, 0, 1)
}

// ColorFormat returns the offscreen 3D color attachment format.
func (t *DynamicResolutionTarget) ColorFormat() gpu.TextureFormat {
	if t == nil {
		return gpu.FormatUndefined
	}
	return t.colorFormat
}

// DepthFormat returns the offscreen 3D depth attachment format, or
// [gpu.FormatUndefined] when depth is disabled.
func (t *DynamicResolutionTarget) DepthFormat() gpu.TextureFormat {
	if t == nil {
		return gpu.FormatUndefined
	}
	return t.depthFormat
}

// OutputFormat returns the native color format targeted by [Upscale].
func (t *DynamicResolutionTarget) OutputFormat() gpu.TextureFormat {
	if t == nil {
		return gpu.FormatUndefined
	}
	return t.outputFormat
}

// RenderTarget returns the underlying offscreen [*gpu.RenderTarget], or nil
// before the first [Begin3D] or [EnsureCapacity] call.
func (t *DynamicResolutionTarget) RenderTarget() *gpu.RenderTarget {
	if t == nil {
		return nil
	}
	return t.rt
}

// ColorTexture returns the offscreen 3D color attachment texture, or nil before
// allocation.
func (t *DynamicResolutionTarget) ColorTexture() *gpu.Texture {
	if t == nil || t.rt == nil {
		return nil
	}
	return t.rt.Color
}

// DepthTexture returns the offscreen 3D depth attachment texture, or nil when
// depth is disabled or before allocation.
func (t *DynamicResolutionTarget) DepthTexture() *gpu.Texture {
	if t == nil || t.rt == nil {
		return nil
	}
	return t.rt.Depth
}

// AllocatedSize returns the backing offscreen render target's pixel capacity.
func (t *DynamicResolutionTarget) AllocatedSize() (w, h int) {
	if t == nil {
		return 0, 0
	}
	return t.allocW, t.allocH
}

// ScaledSize returns the active scaled 3D viewport dimensions from the most
// recent [Begin3D] call.
func (t *DynamicResolutionTarget) ScaledSize() (w, h int) {
	if t == nil {
		return 0, 0
	}
	return t.lastScaledW, t.lastScaledH
}

// NativeSize returns the native output dimensions from the most recent
// [Begin3D] call.
func (t *DynamicResolutionTarget) NativeSize() (w, h int) {
	if t == nil {
		return 0, 0
	}
	return t.lastNativeW, t.lastNativeH
}

// ByteSize estimates the resident GPU memory (in bytes) of the offscreen color
// and depth attachments for [RenderStats.TextureBytes] and [Budget] checks.
func (t *DynamicResolutionTarget) ByteSize() uint64 {
	if t == nil || t.rt == nil || !t.rt.Valid() || t.allocW <= 0 || t.allocH <= 0 {
		return 0
	}
	pixels := uint64(t.allocW) * uint64(t.allocH)
	bpp := uint64(dynResFormatBytesPerPixel(t.colorFormat))
	if t.depthFormat != gpu.FormatUndefined {
		bpp += uint64(dynResFormatBytesPerPixel(t.depthFormat))
	}
	return pixels * bpp
}

// Valid reports whether t has not been closed and its core GPU pipelines and
// buffers are still live on its device.
func (t *DynamicResolutionTarget) Valid() bool {
	if t == nil || t.closed {
		return false
	}
	return t.quadVB != nil && t.quadVB.Valid() &&
		t.paramBuf != nil && t.paramBuf.Valid() &&
		t.layout != nil && t.layout.Valid() &&
		t.blitPipe != nil && t.blitPipe.Valid() &&
		t.sharpenPipe != nil && t.sharpenPipe.Valid()
}

// EnsureCapacity guarantees the backing offscreen render target can cover a
// native resolution of (nativeW, nativeH). The backing target grows on demand
// and shrinks only when native dimensions drop below half of the allocated
// capacity, avoiding texture reallocations when scale changes frame to frame.
func (t *DynamicResolutionTarget) EnsureCapacity(nativeW, nativeH int) error {
	if !t.Valid() {
		return fmt.Errorf("%w: dynamic resolution target is closed or invalid", ErrInvalidDynamicResolution)
	}
	if nativeW <= 0 || nativeH <= 0 {
		return fmt.Errorf("%w: native dimensions %dx%d must be positive", ErrInvalidDynamicResolution, nativeW, nativeH)
	}
	if uint64(nativeW) > math.MaxUint32 || uint64(nativeH) > math.MaxUint32 {
		return fmt.Errorf("%w: native dimensions %dx%d exceed uint32 range", ErrInvalidDynamicResolution, nativeW, nativeH)
	}

	needsAlloc := t.rt == nil || !t.rt.Valid() || t.linearBG == nil || !t.linearBG.Valid() || t.nearestBG == nil || !t.nearestBG.Valid()
	if !needsAlloc {
		grew := nativeW > t.allocW || nativeH > t.allocH
		shrunkSignificantly := nativeW*2 < t.allocW || nativeH*2 < t.allocH
		needsAlloc = grew || shrunkSignificantly
	}
	if !needsAlloc {
		return nil
	}

	return t.reallocateTarget(nativeW, nativeH)
}

func (t *DynamicResolutionTarget) reallocateTarget(w, h int) error {
	newRT, err := t.dev.CreateRenderTarget(t.cx, gpu.RenderTargetDesc{
		Label:       t.label + ".rt",
		Width:       uint32(w),
		Height:      uint32(h),
		ColorFormat: t.colorFormat,
		DepthFormat: t.depthFormat,
		Retained:    true,
	})
	if err != nil {
		return err
	}

	newLinearBG, err := t.dev.CreateBindGroupDesc(t.cx, gpu.BindGroupDesc{
		Label:  t.label + ".linear.bg",
		Layout: t.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: t.paramBuf, Size: dynResUniformSize},
			{Binding: 1, Texture: newRT.Color},
			{Binding: 2, Sampler: t.linearSamp},
		},
	})
	if err != nil {
		_ = newRT.Close()
		return err
	}

	newNearestBG, err := t.dev.CreateBindGroupDesc(t.cx, gpu.BindGroupDesc{
		Label:  t.label + ".nearest.bg",
		Layout: t.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: t.paramBuf, Size: dynResUniformSize},
			{Binding: 1, Texture: newRT.Color},
			{Binding: 2, Sampler: t.nearestSamp},
		},
	})
	if err != nil {
		_ = newLinearBG.Close()
		_ = newRT.Close()
		return err
	}

	t.closeOffscreen()
	t.rt = newRT
	t.linearBG = newLinearBG
	t.nearestBG = newNearestBG
	t.allocW = w
	t.allocH = h
	return nil
}

// Begin3D ensures offscreen capacity for (nativeW, nativeH), begins a render
// pass targeting the offscreen color (and optional depth) buffer cleared to
// clearColor, and configures SetViewport and SetScissor to the current scaled
// sub-rectangle [0, 0, scaledW, scaledH].
//
// Draw your 3D scene into the returned [*gpu.RenderPass] and call pass.End(),
// then call [Upscale] (or [UpscaleIntoPass]) to present the scaled 3D image at
// native resolution before drawing native-resolution HUD overlays.
func (t *DynamicResolutionTarget) Begin3D(rec RenderPassRecorder, nativeW, nativeH int, clearColor core.Color) (*gpu.RenderPass, error) {
	if rec == nil {
		return nil, fmt.Errorf("%w: nil render pass recorder", ErrInvalidDynamicResolution)
	}
	if err := t.EnsureCapacity(nativeW, nativeH); err != nil {
		return nil, err
	}
	scaledW, scaledH := t.scaler.ScaledSize(nativeW, nativeH)
	if scaledW <= 0 || scaledH <= 0 {
		return nil, fmt.Errorf("%w: invalid scaled dimensions %dx%d", ErrInvalidDynamicResolution, scaledW, scaledH)
	}

	desc := gpu.RenderPassDesc{
		Label:       t.label + ".3d",
		TimingLabel: t.timingLabel,
		ColorAttachments: []gpu.ColorAttachment{{
			Texture:    t.rt.Color,
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: clearColor,
		}},
	}
	if t.rt.Depth != nil {
		desc.DepthStencil = &gpu.DepthStencilAttachment{
			Texture:      t.rt.Depth,
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1.0,
		}
	}

	pass := rec.BeginRenderPass(desc)
	pass.SetViewport(0, 0, float32(scaledW), float32(scaledH), 0, 1)
	pass.SetScissor(0, 0, int32(scaledW), int32(scaledH))

	t.lastNativeW = nativeW
	t.lastNativeH = nativeH
	t.lastScaledW = scaledW
	t.lastScaledH = scaledH
	return pass, nil
}

// Upscale opens a native-resolution color render pass targeting dst (or the
// view's swapchain drawable when dst is nil), draws the full-screen upscale
// pass from the active scaled 3D sub-viewport to (nativeW, nativeH), and ends
// the pass.
//
// To composite a native-resolution HUD on top without losing pixel sharpness:
//   - Mount [github.com/nsgo-dev/game/packages/game/scene.Overlay3D] above the
//     [gpu.View], which draws 2D HUD layers in a native-resolution core.Canvas, OR
//   - Record a follow-up native GPU HUD render pass with gpu.LoadLoad on dst, OR
//   - Call [UpscaleIntoPass] inside a single native render pass and draw GPU HUD
//     elements directly afterward in the same pass.
func (t *DynamicResolutionTarget) Upscale(rec RenderPassRecorder, dst *gpu.Texture, nativeW, nativeH int) error {
	if rec == nil {
		return fmt.Errorf("%w: nil render pass recorder", ErrInvalidDynamicResolution)
	}
	if err := t.prepareUpscale(nativeW, nativeH); err != nil {
		return err
	}
	pass := rec.BeginRenderPass(gpu.RenderPassDesc{
		Label: t.label + ".upscale",
		ColorAttachments: []gpu.ColorAttachment{{
			Texture: dst,
			LoadOp:  gpu.LoadClear,
			StoreOp: gpu.StoreStore,
		}},
	})
	t.recordUpscaleDraws(pass, nativeW, nativeH)
	pass.End()
	return nil
}

// UpscaleIntoPass records the full-screen upscale draw directly into an active
// color-only [*gpu.RenderPass] targeting a native (nativeW, nativeH) surface,
// setting the viewport and scissor to the full native rectangle. This lets a
// caller draw native-resolution GPU HUD elements in the same render pass right
// after the upscaled 3D scene.
func (t *DynamicResolutionTarget) UpscaleIntoPass(pass *gpu.RenderPass, nativeW, nativeH int) error {
	if pass == nil {
		return fmt.Errorf("%w: nil render pass", ErrInvalidDynamicResolution)
	}
	if err := t.prepareUpscale(nativeW, nativeH); err != nil {
		return err
	}
	t.recordUpscaleDraws(pass, nativeW, nativeH)
	return nil
}

func (t *DynamicResolutionTarget) prepareUpscale(nativeW, nativeH int) error {
	if !t.Valid() {
		return fmt.Errorf("%w: dynamic resolution target is closed or invalid", ErrInvalidDynamicResolution)
	}
	if t.rt == nil || !t.rt.Valid() || t.allocW <= 0 || t.allocH <= 0 {
		return fmt.Errorf("%w: Begin3D must be called before Upscale", ErrInvalidDynamicResolution)
	}
	if nativeW <= 0 || nativeH <= 0 {
		return fmt.Errorf("%w: native dimensions %dx%d must be positive", ErrInvalidDynamicResolution, nativeW, nativeH)
	}

	scaledW, scaledH := t.lastScaledW, t.lastScaledH
	if scaledW <= 0 || scaledH <= 0 {
		scaledW, scaledH = t.scaler.ScaledSize(nativeW, nativeH)
	}
	if scaledW > t.allocW {
		scaledW = t.allocW
	}
	if scaledH > t.allocH {
		scaledH = t.allocH
	}

	invAllocW := 1.0 / float32(t.allocW)
	invAllocH := 1.0 / float32(t.allocH)

	uvScaleX := float32(scaledW) * invAllocW
	uvScaleY := float32(scaledH) * invAllocH
	uvMinX := 0.5 * invAllocW
	uvMinY := 0.5 * invAllocH
	uvMaxX := (float32(scaledW) - 0.5) * invAllocW
	uvMaxY := (float32(scaledH) - 0.5) * invAllocH
	if uvMaxX < uvMinX {
		uvMaxX = uvMinX
	}
	if uvMaxY < uvMinY {
		uvMaxY = uvMinY
	}

	sharp := float32(0)
	if t.filter == DynamicResolutionSharpen && (scaledW < nativeW || scaledH < nativeH) {
		sharp = t.sharpness
	}

	t.encodeParams(uvScaleX, uvScaleY, uvMinX, uvMinY, uvMaxX, uvMaxY, invAllocW, invAllocH, sharp)
	t.dev.WriteBuffer(t.paramBuf, 0, t.scratch[:])
	return nil
}

func (t *DynamicResolutionTarget) recordUpscaleDraws(pass *gpu.RenderPass, nativeW, nativeH int) {
	pass.SetViewport(0, 0, float32(nativeW), float32(nativeH), 0, 1)
	pass.SetScissor(0, 0, int32(nativeW), int32(nativeH))

	useSharpen := t.filter == DynamicResolutionSharpen && t.sharpness > 0 && (t.lastScaledW < nativeW || t.lastScaledH < nativeH)
	if useSharpen {
		pass.SetPipeline(t.sharpenPipe)
		pass.SetBindGroup(0, t.linearBG)
	} else {
		pass.SetPipeline(t.blitPipe)
		if t.filter == DynamicResolutionNearest {
			pass.SetBindGroup(0, t.nearestBG)
		} else {
			pass.SetBindGroup(0, t.linearBG)
		}
	}
	pass.SetVertexBuffer(0, t.quadVB, 0)
	pass.Draw(6, 0)
}

func (t *DynamicResolutionTarget) encodeParams(uvScaleX, uvScaleY, uvMinX, uvMinY, uvMaxX, uvMaxY, texelX, texelY, sharpness float32) {
	vals := [12]float32{
		uvScaleX, uvScaleY,
		uvMinX, uvMinY,
		uvMaxX, uvMaxY,
		texelX, texelY,
		sharpness, 0, 0, 0,
	}
	for i, v := range vals {
		binary.LittleEndian.PutUint32(t.scratch[i*4:], math.Float32bits(v))
	}
}

func (t *DynamicResolutionTarget) closeOffscreen() error {
	var first error
	for _, c := range []interface{ Close() error }{t.linearBG, t.nearestBG, t.rt} {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	t.linearBG = nil
	t.nearestBG = nil
	t.rt = nil
	t.allocW = 0
	t.allocH = 0
	return first
}

// Close releases all GPU resources owned by t now rather than waiting for its
// owning scope. It is safe to call more than once and returns the first error.
func (t *DynamicResolutionTarget) Close() error {
	if t == nil || t.closed {
		return nil
	}
	t.closed = true
	first := t.closeOffscreen()
	for _, c := range []interface{ Close() error }{
		t.sharpenPipe,
		t.blitPipe,
		t.nearestSamp,
		t.linearSamp,
		t.layout,
		t.paramBuf,
		t.quadVB,
	} {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func dynResFinite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

func dynResClamp32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func dynResPackFloat32s(fs ...float32) []byte {
	out := make([]byte, 4*len(fs))
	for i, f := range fs {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(f))
	}
	return out
}

func dynResFormatBytesPerPixel(f gpu.TextureFormat) int {
	switch f {
	case gpu.FormatR8Unorm, gpu.FormatStencil8:
		return 1
	case gpu.FormatRG8Unorm:
		return 2
	case gpu.FormatRGBA16Float:
		return 8
	case gpu.FormatRGBA32Float:
		return 16
	default:
		return 4
	}
}
