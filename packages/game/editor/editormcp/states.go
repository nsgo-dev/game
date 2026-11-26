package editormcp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/editor"
)

// The names of the sprite state machine tools added to the editor's MCP server.
const (
	// ToolStatesWrite validates and writes a *.states.yaml file in the open project and opens it in the editor.
	ToolStatesWrite = "states_write"
	// ToolStatesTest plays scripted input against a *.states.yaml file and reports the state trace and any stuck states.
	ToolStatesTest = "states_test"
)

const statesWriteSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["file", "yaml"],
  "properties": {
    "file": {"type": "string", "description": "Slash-separated path inside the project root ending in .states.yaml, such as anim/knight.states.yaml"},
    "yaml": {"type": "string", "description": "YAML source of the *.states.yaml document; validated strictly and written in canonical form"},
    "target": {"type": "string", "description": "Optional scene object ID to preview the state machine on"},
    "open": {"type": "boolean", "description": "Open the written state machine in the editor graph (default true)"}
  }
}`

const statesTestSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "file": {"type": "string", "description": "Slash-separated path of a *.states.yaml file from the project root; empty tests the state machine open in the editor"},
    "durationSeconds": {"type": "number", "minimum": 0.1, "maximum": 60, "description": "Total simulated duration in seconds (default 8)"},
    "rate": {"type": "integer", "minimum": 1, "maximum": 240, "description": "Fixed simulation step rate in Hz (default 60)"},
    "script": {
      "type": "array",
      "description": "Optional scripted input steps; omitted runs the default 8 s locomotion, jump, attack and hurt script",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "atMs": {"type": "integer", "minimum": 0},
          "durationMs": {"type": "integer", "minimum": 0},
          "moveX": {"type": "number"},
          "jump": {"type": "boolean"},
          "attack": {"type": "boolean"},
          "hurt": {"type": "boolean"},
          "floats": {"type": "object", "additionalProperties": {"type": "number"}},
          "bools": {"type": "object", "additionalProperties": {"type": "boolean"}},
          "triggers": {"type": "array", "items": {"type": "string"}}
        }
      }
    }
  }
}`

// StatesWriteResult is what states_write reports.
type StatesWriteResult struct {
	File        string `json:"file"`
	Entry       string `json:"entry"`
	States      int    `json:"states"`
	Transitions int    `json:"transitions"`
	Opened      bool   `json:"opened"`
}

// StatesTestResult is what states_test reports.
type StatesTestResult struct {
	File            string                     `json:"file,omitempty"`
	Summary         string                     `json:"summary"`
	DurationSeconds float64                    `json:"durationSeconds"`
	Steps           int                        `json:"steps"`
	Trace           []string                   `json:"trace"`
	Transitions     []animclip.TransitionEvent `json:"transitions,omitempty"`
	VisitedStates   []string                   `json:"visitedStates"`
	UnvisitedStates []string                   `json:"unvisitedStates,omitempty"`
	StuckStates     []string                   `json:"stuckStates,omitempty"`
}

type statesWriteArgs struct {
	File   string `json:"file"`
	YAML   string `json:"yaml"`
	Target string `json:"target"`
	Open   *bool  `json:"open"`
}

type statesScriptStepArgs struct {
	AtMs       int                `json:"atMs"`
	DurationMs int                `json:"durationMs"`
	MoveX      float64            `json:"moveX"`
	Jump       bool               `json:"jump"`
	Attack     bool               `json:"attack"`
	Hurt       bool               `json:"hurt"`
	Floats     map[string]float64 `json:"floats"`
	Bools      map[string]bool    `json:"bools"`
	Triggers   []string           `json:"triggers"`
}

type statesTestArgs struct {
	File            string                 `json:"file"`
	DurationSeconds float64                `json:"durationSeconds"`
	Rate            int                    `json:"rate"`
	Script          []statesScriptStepArgs `json:"script"`
}

func statesWrite(ctx context.Context, c *Call) (*Result, error) {
	var args statesWriteArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("states_write: %w", err)
	}
	clean, err := cleanProjectPath(args.File)
	if err != nil {
		return nil, fmt.Errorf("states_write: %w", err)
	}
	lower := strings.ToLower(clean)
	if !strings.HasSuffix(lower, ".states.yaml") && !strings.HasSuffix(lower, ".states.yml") {
		return nil, fmt.Errorf("states_write: %s: file name must end with .states.yaml", clean)
	}
	if strings.TrimSpace(args.YAML) == "" {
		return nil, errors.New("states_write: yaml must not be empty")
	}
	doc, err := animclip.ParseStateMachine([]byte(args.YAML))
	if err != nil {
		return nil, fmt.Errorf("states_write: %s: %w", clean, err)
	}
	canonical, err := animclip.EncodeStateMachine(doc)
	if err != nil {
		return nil, fmt.Errorf("states_write: %s: %w", clean, err)
	}

	openInEditor := true
	if args.Open != nil {
		openInEditor = *args.Open
	}

	var res StatesWriteResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.Playing() {
			return fmt.Errorf("states_write: %w", editor.ErrPlaying)
		}
		if holdErr := sketchHoldEditError(ed, clean); holdErr != nil {
			return holdErr
		}
		root := projectRoot(ed)
		if root == "" {
			return errors.New("states_write: no project is open in the editor; open one with go tool game editor <project-dir>")
		}
		target := args.Target
		if openInEditor {
			if target == "" {
				target = ed.StatesTarget()
			}
			if target != "" {
				if obj, _, _ := ed.FindObject(target); obj == nil {
					return fmt.Errorf("states_write: object %q not found", target)
				}
			}
		}
		abs, err := writeProjectFileAtomic(root, clean, canonical)
		if err != nil {
			return fmt.Errorf("states_write: %w", err)
		}
		if openInEditor {
			if err := ed.OpenStatesFile(abs, target); err != nil {
				return fmt.Errorf("states_write: open %s: %w", clean, err)
			}
		} else {
			ed.CheckProblems()
		}
		res = StatesWriteResult{
			File:        clean,
			Entry:       doc.Entry,
			States:      len(doc.States),
			Transitions: len(doc.Transitions),
			Opened:      openInEditor,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	text := fmt.Sprintf("Wrote %s (%d states, %d transitions, entry %q).", res.File, res.States, res.Transitions, res.Entry)
	return &Result{Text: text, Structured: res}, nil
}

// writeProjectFileAtomic writes data atomically to rel inside root, refusing any symlink that escapes root or any non-regular existing destination.
func writeProjectFileAtomic(root, rel string, data []byte) (string, error) {
	realRoot := root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		realRoot = r
	}
	abs := filepath.Join(realRoot, filepath.FromSlash(rel))

	// Walk up from abs to the deepest existing ancestor and verify symlinks cannot escape realRoot.
	probe := abs
	for {
		_, err := os.Lstat(probe)
		if err == nil {
			realProbe, err := filepath.EvalSymlinks(probe)
			if err != nil {
				return "", fmt.Errorf("%s: %w", rel, err)
			}
			if !pathWithinRoot(realRoot, realProbe) {
				return "", fmt.Errorf("%s: path links to %s, outside the project", rel, realProbe)
			}
			if probe == abs {
				st, err := os.Stat(realProbe)
				if err != nil {
					return "", fmt.Errorf("%s: %w", rel, err)
				}
				if !st.Mode().IsRegular() {
					return "", fmt.Errorf("%s: destination is not a regular file", rel)
				}
				abs = realProbe
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%s: %w", rel, err)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}

	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	if !pathWithinRoot(realRoot, realDir) {
		return "", fmt.Errorf("%s: directory links to %s, outside the project", rel, realDir)
	}
	finalPath := filepath.Join(realDir, filepath.Base(abs))

	tmp, err := os.CreateTemp(realDir, ".states-*.tmp")
	if err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	cleanup = false
	return finalPath, nil
}

func pathWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func statesTest(ctx context.Context, c *Call) (*Result, error) {
	var args statesTestArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("states_test: %w", err)
	}
	if math.IsNaN(args.DurationSeconds) || math.IsInf(args.DurationSeconds, 0) ||
		args.DurationSeconds < 0 || (args.DurationSeconds > 0 && args.DurationSeconds < 0.1) || args.DurationSeconds > 60 {
		return nil, fmt.Errorf("states_test: durationSeconds %v must be in [0.1, 60]", args.DurationSeconds)
	}
	if args.Rate < 0 || args.Rate > 240 {
		return nil, fmt.Errorf("states_test: rate %d must be in [1, 240]", args.Rate)
	}
	for i, s := range args.Script {
		if s.AtMs < 0 {
			return nil, fmt.Errorf("states_test: script[%d].atMs %d must be >= 0", i, s.AtMs)
		}
		if s.DurationMs < 0 {
			return nil, fmt.Errorf("states_test: script[%d].durationMs %d must be >= 0", i, s.DurationMs)
		}
		if math.IsNaN(s.MoveX) || math.IsInf(s.MoveX, 0) {
			return nil, fmt.Errorf("states_test: script[%d].moveX must be finite", i)
		}
		for k, v := range s.Floats {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("states_test: script[%d].floats[%q] must be finite", i, k)
			}
		}
	}

	var (
		doc   *animclip.StateMachineDoc
		clips animclip.ClipResolver
		file  string
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		clips = ed.StatesClipResolver()
		if args.File == "" {
			doc = ed.States()
			if doc == nil {
				return errors.New("states_test: no sprite state machine is open in the editor; pass file or open a *.states.yaml file first")
			}
			file = ed.StatesPath()
			if root := projectRoot(ed); root != "" && file != "" {
				if rel, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rel, "..") {
					file = filepath.ToSlash(rel)
				}
			}
			return nil
		}
		root := projectRoot(ed)
		if root == "" {
			return fmt.Errorf("states_test: %s: no project is open in the editor", args.File)
		}
		clean, err := cleanProjectPath(args.File)
		if err != nil {
			return fmt.Errorf("states_test: %w", err)
		}
		data, err := readProjectFile(root, clean)
		if err != nil {
			return fmt.Errorf("states_test: %w", err)
		}
		parsed, err := animclip.ParseStateMachine(data)
		if err != nil {
			return fmt.Errorf("states_test: %s: %w", clean, err)
		}
		doc = parsed
		file = clean
		if clips == nil {
			clips = animclip.FSClips{FS: os.DirFS(root)}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	opts := animclip.SimulationOptions{
		Rate: args.Rate,
	}
	if args.DurationSeconds > 0 {
		opts.Duration = time.Duration(args.DurationSeconds * float64(time.Second))
	}
	if len(args.Script) > 0 {
		opts.Script = make([]animclip.ScriptStep, len(args.Script))
		for i, s := range args.Script {
			opts.Script[i] = animclip.ScriptStep{
				At:       time.Duration(s.AtMs) * time.Millisecond,
				Duration: time.Duration(s.DurationMs) * time.Millisecond,
				MoveX:    s.MoveX,
				Jump:     s.Jump,
				Attack:   s.Attack,
				Hurt:     s.Hurt,
				Floats:   s.Floats,
				Bools:    s.Bools,
				Triggers: s.Triggers,
			}
		}
	}

	report, err := animclip.SimulateStateMachine(doc, clips, nil, nil, opts)
	if err != nil {
		return nil, fmt.Errorf("states_test: %w", err)
	}
	summary := report.Summary()
	res := StatesTestResult{
		File:            file,
		Summary:         summary,
		DurationSeconds: report.Duration.Seconds(),
		Steps:           report.Steps,
		Trace:           report.Trace,
		Transitions:     report.Transitions,
		VisitedStates:   report.VisitedStates,
		UnvisitedStates: report.UnvisitedStates,
		StuckStates:     report.StuckStates,
	}
	text := summary + "\nTrace: " + strings.Join(report.Trace, " -> ") + "\n"
	return &Result{Text: text, Structured: res}, nil
}
