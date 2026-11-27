// Package crashreport holds the JSON schema of a crash report, shared by the
// client-side liveops.CrashReporter and the server-side server.Panic report.
// It imports no NSGo or game package, so a headless dedicated server links it
// without pulling in the windowing layer.
package crashreport

import (
	"encoding"
	"encoding/json"
	"fmt"
	"time"
)

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
