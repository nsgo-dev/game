package liveops

import (
	"encoding"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/nsgo-dev/game/packages/game"
)

// DefaultHistorySize is the number of recent steps a CrashReporter keeps when CrashOptions.HistorySize is 0 or negative.
const DefaultHistorySize = 120

// StepRecord describes one recorded step in a CrashReporter's fixed-size ring buffer.
type StepRecord struct {
	// Step is the 1-based monotonic step counter assigned when the step was recorded.
	Step uint64 `json:"step"`
	// DT is the fixed simulation step duration passed to RecordStep or Loop.Update.
	DT time.Duration `json:"dt"`
	// Summary is an optional short label describing what ran in the step.
	Summary string `json:"summary,omitempty"`
	// Data holds optional structured context attached to the step.
	Data any `json:"data,omitempty"`
}

// CrashReport is a JSON-marshalable snapshot built when a guarded callback panics.
type CrashReport struct {
	// Timestamp is when the panic was caught, from CrashOptions.Now or time.Now.
	Timestamp time.Time `json:"timestamp"`
	// Panic is the raw value returned by recover. When marshaled to JSON, an error that does not implement json.Marshaler or encoding.TextMarshaler, or any non-JSON-marshalable value, is encoded as its string message.
	Panic any `json:"panic,omitempty"`
	// Message is fmt.Sprint(Panic).
	Message string `json:"message"`
	// Stack is the goroutine stack trace captured at recovery via runtime/debug.Stack.
	Stack string `json:"stack"`
	// Step is the reporter's monotonic step counter at the time of the crash.
	Step uint64 `json:"step"`
	// LastSteps holds the buffered steps in chronological order, oldest to newest.
	LastSteps []StepRecord `json:"last_steps,omitempty"`
	// State is the optional snapshot returned by CrashOptions.CaptureState.
	State any `json:"state,omitempty"`
	// Metadata holds a copy of the reporter's metadata key-value pairs (such as app version, target OS or scene name).
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MarshalJSON encodes r as JSON, ensuring Panic values that are plain errors or non-JSON types serialize cleanly as strings rather than empty objects or marshal errors.
func (r CrashReport) MarshalJSON() ([]byte, error) {
	type alias CrashReport
	out := alias(r)
	out.Panic = jsonSafePanic(r.Panic)
	return json.Marshal(out)
}

func jsonSafePanic(v any) any {
	if v == nil {
		return nil
	}
	if err, ok := v.(error); ok {
		if _, jm := v.(json.Marshaler); !jm {
			if _, tm := v.(encoding.TextMarshaler); !tm {
				return err.Error()
			}
		}
	}
	if _, err := json.Marshal(v); err != nil {
		return fmt.Sprint(v)
	}
	return v
}

// CrashOptions configures a CrashReporter.
type CrashOptions struct {
	// HistorySize is how many recent steps the ring buffer keeps; DefaultHistorySize (120) when <= 0.
	HistorySize int
	// Reporter receives every CrashReport produced by a recovered panic.
	Reporter func(CrashReport)
	// CaptureState optionally returns a JSON-marshalable snapshot of the game's state when a panic is caught.
	CaptureState func() any
	// Metadata holds initial key-value pairs copied into every CrashReport (such as app version, target OS or scene name).
	Metadata map[string]string
	// Rethrow re-panics with the recovered value after Reporter returns.
	Rethrow bool
	// Now returns the clock time for CrashReport.Timestamp; time.Now when nil.
	Now func() time.Time
}

// CrashReporter records recent simulation steps in a fixed-size ring buffer and catches panics in guarded callbacks or game.Loop runs, building a CrashReport for the app's Reporter.
type CrashReporter struct {
	mu           sync.Mutex
	ring         []StepRecord
	head         int
	count        int
	step         uint64
	report       func(CrashReport)
	captureState func() any
	metadata     map[string]string
	rethrow      bool
	now          func() time.Time
}

// NewCrashReporter returns a CrashReporter configured by opts, pre-allocating its step ring buffer so steady-state RecordStep calls perform no heap allocations.
func NewCrashReporter(opts CrashOptions) *CrashReporter {
	size := opts.HistorySize
	if size <= 0 {
		size = DefaultHistorySize
	}
	return &CrashReporter{
		ring:         make([]StepRecord, size),
		report:       opts.Reporter,
		captureState: opts.CaptureState,
		metadata:     copyStringMap(opts.Metadata),
		rethrow:      opts.Rethrow,
		now:          opts.Now,
	}
}

// RecordStep appends a step to the ring buffer and returns its monotonic step number. It allocates nothing in steady state.
func (c *CrashReporter) RecordStep(dt time.Duration, summary string) uint64 {
	return c.RecordStepData(dt, summary, nil)
}

// RecordStepData appends a step with optional structured data to the ring buffer and returns its monotonic step number. It allocates nothing in steady state.
func (c *CrashReporter) RecordStepData(dt time.Duration, summary string, data any) uint64 {
	c.mu.Lock()
	c.step++
	s := c.step
	if len(c.ring) > 0 {
		c.ring[c.head] = StepRecord{
			Step:    s,
			DT:      dt,
			Summary: summary,
			Data:    data,
		}
		c.head++
		if c.head == len(c.ring) {
			c.head = 0
		}
		if c.count < len(c.ring) {
			c.count++
		}
	}
	c.mu.Unlock()
	return s
}

// Step returns the reporter's current monotonic step count.
func (c *CrashReporter) Step() uint64 {
	c.mu.Lock()
	s := c.step
	c.mu.Unlock()
	return s
}

// LastSteps returns a chronological copy (oldest to newest) of the buffered steps.
func (c *CrashReporter) LastSteps() []StepRecord {
	c.mu.Lock()
	steps := c.snapshotStepsLocked()
	c.mu.Unlock()
	return steps
}

// Reset clears the buffered steps without reallocating the ring buffer. The monotonic step counter continues from its current value.
func (c *CrashReporter) Reset() {
	c.mu.Lock()
	clear(c.ring)
	c.head = 0
	c.count = 0
	c.mu.Unlock()
}

// SetMetadata sets or updates a metadata key included in subsequent CrashReports.
func (c *CrashReporter) SetMetadata(key, value string) {
	c.mu.Lock()
	if c.metadata == nil {
		c.metadata = make(map[string]string)
	}
	c.metadata[key] = value
	c.mu.Unlock()
}

// DeleteMetadata removes a metadata key from subsequent CrashReports.
func (c *CrashReporter) DeleteMetadata(key string) {
	c.mu.Lock()
	delete(c.metadata, key)
	c.mu.Unlock()
}

// RunSafe runs fn and recovers any panic into a CrashReport passed to Reporter.
// It reports whether fn panicked. When CrashOptions.Rethrow is true, RunSafe re-panics with the original value after Reporter returns.
func (c *CrashReporter) RunSafe(fn func()) (panicked bool) {
	if fn == nil {
		return false
	}
	var rethrowVal any
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				rethrowVal = c.reportPanic(r)
			}
		}()
		fn()
	}()
	if panicked && c.rethrow {
		panic(rethrowVal)
	}
	return panicked
}

// WrapLoop returns a new *game.Loop that delegates to l.Update and l.Draw with panic recovery and step recording.
// If l.Update does not call RecordStep itself during a step, the wrapper records an "update" step automatically (including for a step that panics).
// Returns nil when l is nil.
func (c *CrashReporter) WrapLoop(l *game.Loop) *game.Loop {
	if l == nil {
		return nil
	}
	return &game.Loop{
		Rate:     l.Rate,
		MaxSteps: l.MaxSteps,
		Update: func(dt time.Duration) {
			if l.Update == nil {
				return
			}
			c.runUpdateStep(dt, l.Update)
		},
		Draw: func(alpha float64) {
			if l.Draw == nil {
				return
			}
			c.RunSafe(func() { l.Draw(alpha) })
		},
	}
}

// GuardLoop wraps l.Update and l.Draw in place with panic recovery and step recording.
// If l.Update does not call RecordStep itself during a step, the guard records an "update" step automatically (including for a step that panics).
func (c *CrashReporter) GuardLoop(l *game.Loop) {
	if l == nil {
		return
	}
	if orig := l.Update; orig != nil {
		l.Update = func(dt time.Duration) {
			c.runUpdateStep(dt, orig)
		}
	}
	if orig := l.Draw; orig != nil {
		l.Draw = func(alpha float64) {
			c.RunSafe(func() { orig(alpha) })
		}
	}
}

func (c *CrashReporter) runUpdateStep(dt time.Duration, fn func(time.Duration)) (panicked bool) {
	prev := c.Step()
	var rethrowVal any
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				if c.Step() == prev {
					c.RecordStep(dt, "update")
				}
				rethrowVal = c.reportPanic(r)
			}
		}()
		fn(dt)
	}()
	if panicked {
		if c.rethrow {
			panic(rethrowVal)
		}
		return true
	}
	if c.Step() == prev {
		c.RecordStep(dt, "update")
	}
	return false
}

func (c *CrashReporter) reportPanic(r any) any {
	stack := string(debug.Stack())
	c.mu.Lock()
	nowFn := c.now
	step := c.step
	steps := c.snapshotStepsLocked()
	meta := copyStringMap(c.metadata)
	capture := c.captureState
	reporter := c.report
	c.mu.Unlock()

	var ts time.Time
	if nowFn != nil {
		ts = nowFn()
	} else {
		ts = time.Now()
	}

	var state any
	if capture != nil {
		state = safeCapture(capture)
	}

	rep := CrashReport{
		Timestamp: ts,
		Panic:     r,
		Message:   fmt.Sprint(r),
		Stack:     stack,
		Step:      step,
		LastSteps: steps,
		State:     state,
		Metadata:  meta,
	}
	if reporter != nil {
		reporter(rep)
	}
	return r
}

func safeCapture(fn func() any) (state any) {
	defer func() {
		if r := recover(); r != nil {
			state = fmt.Sprintf("<capture state panic: %v>", r)
		}
	}()
	return fn()
}

func (c *CrashReporter) snapshotStepsLocked() []StepRecord {
	if c.count == 0 {
		return nil
	}
	out := make([]StepRecord, c.count)
	if c.count < len(c.ring) {
		copy(out, c.ring[:c.count])
		return out
	}
	n := copy(out, c.ring[c.head:])
	copy(out[n:], c.ring[:c.head])
	return out
}

func copyStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
