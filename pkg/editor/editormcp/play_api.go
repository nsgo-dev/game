package editormcp

import "fmt"

const (
	ToolPlayStart  = "play_start"
	ToolPlayStop   = "play_stop"
	ToolPlayStep   = "play_step"
	ToolPlayInput  = "play_input"
	ToolPlayState  = "play_state"
	ToolPlayFrame  = "play_frame"
	ToolOutputRead = "output_read"
)

// PlayStartArgs controls an editor Play-in-editor session.
type PlayStartArgs struct {
	Paused     bool   `json:"paused"`
	Seed       *int64 `json:"seed,omitempty"`
	ReplayFile string `json:"replay_file,omitempty"`
}
type PlayStepArgs struct {
	Count int `json:"count"`
}
type PlayInputArgs struct {
	Action, Event string
	Value         float64 `json:"value,omitempty"`
	X             float64 `json:"x,omitempty"`
	Y             float64 `json:"y,omitempty"`
}
type PlayState struct {
	Playing, Paused bool
	Step            int
	Scene           string
	Entities        []PlayEntity
}
type PlayEntity struct {
	ID, Name string
	X, Y, Z  float64
	Tags     []string
}
type OutputEntry struct {
	Cursor               uint64
	Level, Message, File string
	Line                 int
}

func ValidatePlayStep(a PlayStepArgs) error {
	if a.Count < 1 || a.Count > 3600 {
		return fmt.Errorf("editormcp: play_step count must be between 1 and 3600")
	}
	return nil
}
func ValidatePlayInput(a PlayInputArgs) error {
	if a.Action == "" {
		return fmt.Errorf("editormcp: play_input action is required")
	}
	switch a.Event {
	case "press", "release", "tap", "axis", "pointer":
		return nil
	default:
		return fmt.Errorf("editormcp: unknown play_input event %q", a.Event)
	}
}
