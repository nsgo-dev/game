package editormcp

import (
	"fmt"
	"path"
	"strings"
)

// Names reserved for the live Play tools (#1743). Server does not register them yet; the argument and result types below are the wire contract their handlers will use.
const (
	// ToolPlayStart starts a Play-in-editor session (PlayStartArgs).
	ToolPlayStart = "play_start"
	// ToolPlayStop stops the Play session and restores the pre-play scene snapshot.
	ToolPlayStop = "play_stop"
	// ToolPlayStep advances a paused Play session by a bounded number of fixed steps (PlayStepArgs).
	ToolPlayStep = "play_step"
	// ToolPlayInput injects one input action event into the Play session (PlayInputArgs).
	ToolPlayInput = "play_input"
	// ToolPlayState reads the Play session's loop status and entities (PlayState).
	ToolPlayState = "play_state"
	// ToolPlayFrame captures a PNG of the Play viewport.
	ToolPlayFrame = "play_frame"
	// ToolOutputRead reads Output panel entries after a cursor (OutputEntry).
	ToolOutputRead = "output_read"
)

// Play input event kinds accepted by PlayInputArgs.Event.
const (
	PlayInputPress   = "press"
	PlayInputRelease = "release"
	PlayInputTap     = "tap"
	PlayInputAxis    = "axis"
	PlayInputPointer = "pointer"
)

// PlayStartArgs are the arguments of play_start.
type PlayStartArgs struct {
	// Paused starts the session paused so play_step drives it frame by frame.
	Paused bool `json:"paused"`
	// Seed fixes the session's random seed when set.
	Seed *int64 `json:"seed,omitempty"`
	// ReplayFile is an optional slash-separated project-relative .replay.yaml path to load.
	ReplayFile string `json:"replay_file,omitempty"`
}

// PlayStepArgs are the arguments of play_step.
type PlayStepArgs struct {
	// Count is the number of fixed steps to advance, from 1 to 3600.
	Count int `json:"count"`
}

// PlayInputArgs are the arguments of play_input.
type PlayInputArgs struct {
	// Action is the input action name from the project's input bindings.
	Action string `json:"action"`
	// Event is one of PlayInputPress, PlayInputRelease, PlayInputTap, PlayInputAxis or PlayInputPointer.
	Event string `json:"event"`
	// Value is the axis value for PlayInputAxis, from -1 to 1.
	Value float64 `json:"value,omitempty"`
	// X and Y are the pointer position for PlayInputPointer.
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
}

// PlayState is the result of play_state.
type PlayState struct {
	Playing  bool         `json:"playing"`
	Paused   bool         `json:"paused"`
	Step     int          `json:"step"`
	Scene    string       `json:"scene"`
	Entities []PlayEntity `json:"entities"`
}

// PlayEntity is one live entity in a PlayState.
type PlayEntity struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	X    float64  `json:"x"`
	Y    float64  `json:"y"`
	Z    float64  `json:"z"`
	Tags []string `json:"tags,omitempty"`
}

// OutputEntry is one Output panel entry returned by output_read; Cursor orders entries so the next read can resume after it.
type OutputEntry struct {
	Cursor  uint64 `json:"cursor"`
	Level   string `json:"level"`
	Message string `json:"message"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
}

// ValidatePlayStep reports whether a play_step count is within 1..3600.
func ValidatePlayStep(a PlayStepArgs) error {
	if a.Count < 1 || a.Count > maxPlayReplaySteps {
		return fmt.Errorf("editormcp: play_step count must be between 1 and %d, got %d", maxPlayReplaySteps, a.Count)
	}
	return nil
}

// ValidatePlayInput reports whether play_input names an action and a known event kind, with an axis value within -1..1.
func ValidatePlayInput(a PlayInputArgs) error {
	if a.Action == "" {
		return fmt.Errorf("editormcp: play_input action is required")
	}
	switch a.Event {
	case PlayInputPress, PlayInputRelease, PlayInputTap, PlayInputPointer:
		return nil
	case PlayInputAxis:
		if a.Value < -1 || a.Value > 1 {
			return fmt.Errorf("editormcp: play_input axis value must be between -1 and 1, got %v", a.Value)
		}
		return nil
	default:
		return fmt.Errorf("editormcp: unknown play_input event %q", a.Event)
	}
}

// ValidatePlayStart checks replay_file containment and mutually valid start options.
func ValidatePlayStart(a PlayStartArgs) error {
	p := strings.TrimSpace(a.ReplayFile)
	if p == "" {
		return nil
	}
	clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if clean != p || clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return fmt.Errorf("editormcp: play_start replay_file must be a project-relative path")
	}
	if !strings.HasSuffix(clean, ".replay.yaml") {
		return fmt.Errorf("editormcp: play_start replay_file must end in .replay.yaml")
	}
	return nil
}
