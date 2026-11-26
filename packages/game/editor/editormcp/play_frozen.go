package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

const (
	// ToolPlayFrozenContext returns the frozen Play frame image, prompt, rolling/saved replay YAML, live entities, marks, and panic report.
	ToolPlayFrozenContext = "play_frozen_context"
	// ToolPlayReplayStep steps the Play mode replay across frames A..B while keeping the game paused and updating the viewport agent pill and step log.
	ToolPlayReplayStep = "play_replay_step"

	maxPlayReplaySteps = 3600
)

const playFrozenContextSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "includeImage": {"type": "boolean", "description": "Attach the frozen viewport PNG image (default true)"}
  }
}`

const playReplayStepSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "from_frame": {"type": "integer", "minimum": 0, "description": "Starting frame number shown in the agent pill and step log (defaults to current Play frame)"},
    "to_frame": {"type": "integer", "minimum": 0, "description": "Target frame number to step to; mutually exclusive with steps"},
    "steps": {"type": "integer", "minimum": 1, "maximum": 3600, "description": "Number of frames to advance from from_frame; mutually exclusive with to_frame"},
    "replay_file": {"type": "string", "description": "Optional slash-separated project-relative .replay.yaml path to load before stepping"},
    "note": {"type": "string", "description": "Optional finding appended to the Agent tab step log (e.g. 'the knight stops at x 251 with velocity 0')"},
    "includeImage": {"type": "boolean", "description": "When true, also return a PNG of the resulting paused frame"}
  }
}`

func playFrozenTools() []Tool {
	return []Tool{
		{
			Name:        ToolPlayFrozenContext,
			Title:       "Inspect frozen Play frame",
			Description: "Read the frozen Play mode context: the captured frame PNG, the author's question, the saved/rolling input replay (version 2 YAML), live entity states, marks, and any panic report.",
			InputSchema: []byte(playFrozenContextSchema),
			Action:      ActionRead,
			Handler:     playFrozenContext,
		},
		{
			Name:        ToolPlayReplayStep,
			Title:       "Step Play mode replay",
			Description: "Step Play mode across a frame range (from_frame..to_frame or steps) using the saved/rolling replay while keeping the game paused, showing the viewport agent pill ('agent stepping frames A–B') and recording the step log.",
			InputSchema: []byte(playReplayStepSchema),
			Action:      ActionRun,
			Target: func(_ json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
				return editor.PermissionActionRun, editor.PermissionRunPlaytests
			},
			Handler: playReplayStep,
		},
	}
}

// PlayFrozenContextResult is the structured payload returned by play_frozen_context.
type PlayFrozenContextResult struct {
	Playing          bool                         `json:"playing"`
	Paused           bool                         `json:"paused"`
	Frame            int                          `json:"frame"`
	Seconds          float64                      `json:"seconds"`
	Scene            string                       `json:"scene,omitempty"`
	Prompt           string                       `json:"prompt,omitempty"`
	ReplayPath       string                       `json:"replayPath,omitempty"`
	ReplayFrames     int                          `json:"replayFrames"`
	WindowStartFrame int                          `json:"windowStartFrame,omitempty"`
	ReplayYAML       string                       `json:"replayYaml,omitempty"`
	SelectedID       string                       `json:"selectedId,omitempty"`
	LiveEntities     []editor.PlayLiveEntityState `json:"liveEntities,omitempty"`
	LiveChanges      []editor.RemoteLiveChange    `json:"liveChanges,omitempty"`
	Marks            []editor.PlayMark            `json:"marks,omitempty"`
	LastMarksThread  *editor.PlayMarksThread      `json:"lastMarksThread,omitempty"`
	Panic            *editor.PanicReport          `json:"panic,omitempty"`
	AgentPill        string                       `json:"agentPill,omitempty"`
	AgentSteps       []editor.PlayAgentStep       `json:"agentSteps,omitempty"`
}

// PlayReplayStepResult is the structured payload returned by play_replay_step.
type PlayReplayStepResult struct {
	FromFrame    int                          `json:"fromFrame"`
	ToFrame      int                          `json:"toFrame"`
	CurrentFrame int                          `json:"currentFrame"`
	Paused       bool                         `json:"paused"`
	Pill         string                       `json:"pill"`
	AgentSteps   []editor.PlayAgentStep       `json:"agentSteps,omitempty"`
	LiveEntities []editor.PlayLiveEntityState `json:"liveEntities,omitempty"`
}

type playFrozenContextArgs struct {
	IncludeImage *bool `json:"includeImage"`
}

func playFrozenContext(ctx context.Context, c *Call) (*Result, error) {
	var args playFrozenContextArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("play_frozen_context: %w", err)
	}
	includeImage := true
	if args.IncludeImage != nil {
		includeImage = *args.IncludeImage
	}

	var (
		out PlayFrozenContextResult
		png []byte
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		out.Playing = ed.Playing()
		out.Paused = ed.PlayPaused()
		out.Marks = ed.PlayMarks()
		out.LastMarksThread = ed.LastPlayMarksThread()
		out.Panic = ed.OutputPanic()
		out.AgentPill = ed.PlayAgentPill()
		out.AgentSteps = ed.PlayAgentSteps()

		if req := ed.ActiveFrozenFrameContext(); req != nil {
			out.Frame = req.Frame
			out.Seconds = req.Seconds
			out.Scene = req.Scene
			out.Prompt = req.Prompt
			out.ReplayPath = req.ReplayPath
			out.ReplayFrames = req.ReplayFrames
			out.SelectedID = req.SelectedID
			out.LiveEntities = req.LiveEntities
			out.LiveChanges = req.LiveChanges
			if includeImage && len(req.FramePNG) > 0 {
				png = req.FramePNG
			}
		} else {
			out.Frame = ed.PlaySteps()
			out.ReplayPath = ed.PlayLastReplayPath()
			out.ReplayFrames = ed.PlayRollingFrameCount()
		}

		out.WindowStartFrame = ed.PlayRollingWindowStart()
		if rec := ed.PlayRollingRecording(); rec != nil {
			if data, err := rec.Encode(); err == nil {
				out.ReplayYAML = string(data)
				if out.ReplayFrames == 0 {
					out.ReplayFrames = rec.TotalSteps
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Play mode (playing=%v, paused=%v) at frame %d", out.Playing, out.Paused, out.Frame)
	if out.Scene != "" {
		fmt.Fprintf(&b, " in %s", out.Scene)
	}
	b.WriteString(".")
	if out.Prompt != "" {
		fmt.Fprintf(&b, "\nQuestion: %s", out.Prompt)
	}
	if out.ReplayPath != "" {
		fmt.Fprintf(&b, "\nReplay: %s (%d frames)", out.ReplayPath, out.ReplayFrames)
	} else if out.ReplayFrames > 0 {
		fmt.Fprintf(&b, "\nRolling replay: %d frames", out.ReplayFrames)
	}
	if len(out.Marks) > 0 {
		fmt.Fprintf(&b, "\nMarks: %d", len(out.Marks))
	}
	if out.Panic != nil {
		fmt.Fprintf(&b, "\nPanic at frame %d: %s", out.Panic.Frame, out.Panic.Message)
	}
	return &Result{
		Text:       b.String(),
		Structured: out,
		PNG:        png,
	}, nil
}

type playReplayStepArgs struct {
	FromFrame    *int   `json:"from_frame"`
	ToFrame      *int   `json:"to_frame"`
	Steps        int    `json:"steps"`
	ReplayFile   string `json:"replay_file"`
	Note         string `json:"note"`
	IncludeImage bool   `json:"includeImage"`
}

func playReplayStep(ctx context.Context, c *Call) (*Result, error) {
	var args playReplayStepArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("play_replay_step: %w", err)
	}
	if args.ToFrame != nil && args.Steps > 0 {
		return nil, errors.New("play_replay_step: pass either to_frame or steps, not both")
	}
	if args.Steps < 0 || args.Steps > maxPlayReplaySteps {
		return nil, fmt.Errorf("play_replay_step: steps must be between 1 and %d", maxPlayReplaySteps)
	}

	var (
		out PlayReplayStepResult
		png []byte
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		var absReplay string
		if args.ReplayFile != "" {
			clean, err := cleanProjectPath(args.ReplayFile)
			if err != nil {
				return err
			}
			lower := strings.ToLower(clean)
			if !strings.HasSuffix(lower, ".replay.yaml") && !strings.HasSuffix(lower, ".replay.yml") {
				return fmt.Errorf("%s: replay_file must end in .replay.yaml or .replay.yml", clean)
			}
			root := projectRoot(ed)
			if root == "" {
				root = ed.ReplayDir
			}
			if root == "" {
				return errors.New("no project is open to resolve replay_file")
			}
			resolved, exists, err := resolveProjectTargetPath(root, clean)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%s: replay file does not exist", clean)
			}
			resolvedLower := strings.ToLower(resolved)
			if !strings.HasSuffix(resolvedLower, ".replay.yaml") && !strings.HasSuffix(resolvedLower, ".replay.yml") {
				return fmt.Errorf("%s: resolved replay_file must end in .replay.yaml or .replay.yml", clean)
			}
			absReplay = resolved
		}

		fromFrame := ed.PlaySteps()
		if args.FromFrame != nil {
			fromFrame = *args.FromFrame
		}
		var toFrame int
		switch {
		case args.ToFrame != nil:
			toFrame = *args.ToFrame
		case args.Steps > 0:
			toFrame = fromFrame + args.Steps
		default:
			toFrame = fromFrame + 1
		}
		if fromFrame < 0 || toFrame < 0 {
			return errors.New("from_frame and to_frame must be non-negative")
		}
		if toFrame < fromFrame {
			return fmt.Errorf("to_frame (%d) must be >= from_frame (%d)", toFrame, fromFrame)
		}
		if toFrame-fromFrame > maxPlayReplaySteps {
			return fmt.Errorf("frame span %d exceeds maximum %d steps", toFrame-fromFrame, maxPlayReplaySteps)
		}

		cur, err := ed.AgentStepPlayReplayFile(absReplay, fromFrame, toFrame, args.Note)
		if err != nil {
			return err
		}

		out = PlayReplayStepResult{
			FromFrame:    fromFrame,
			ToFrame:      toFrame,
			CurrentFrame: cur,
			Paused:       ed.PlayPaused(),
			Pill:         ed.PlayAgentPill(),
			AgentSteps:   ed.PlayAgentSteps(),
		}
		if ctxSnap := ed.ActiveFrozenFrameContext(); ctxSnap != nil {
			out.LiveEntities = ctxSnap.LiveEntities
			if args.IncludeImage && len(ctxSnap.FramePNG) > 0 {
				png = ctxSnap.FramePNG
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("play_replay_step: %w", err)
	}

	return &Result{
		Text:       fmt.Sprintf("%s (now paused at frame %d).", out.Pill, out.CurrentFrame),
		Structured: out,
		PNG:        png,
	}, nil
}
