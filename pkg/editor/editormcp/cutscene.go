package editormcp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/cutscene"
	"github.com/nsgo-dev/game/pkg/editor"
)

// The names of the cutscene timeline tools added to the editor's MCP server.
const (
	// ToolCutsceneWrite validates a *.cutscene.yaml document and opens it as an interactive agent review on the cutscene timeline (or writes it directly when review is false).
	ToolCutsceneWrite = "cutscene_write"
	// ToolCutsceneScrub scrubs the cutscene timeline to a chosen timestamp and returns the posed camera, actors, active dialogue and review state.
	ToolCutsceneScrub = "cutscene_scrub"
)

const cutsceneWriteSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["file", "yaml"],
  "properties": {
    "file": {"type": "string", "description": "Slash-separated path inside the project root ending in .cutscene.yaml, such as cutscenes/floor1_intro.cutscene.yaml"},
    "yaml": {"type": "string", "description": "YAML source of the *.cutscene.yaml document; validated strictly against cutscene.Parse"},
    "prompt": {"type": "string", "description": "Short summary of the request that produced these cutscene changes, shown in the review header"},
    "review": {"type": "boolean", "description": "Open as an agent review on the timeline without overwriting disk until kept (default true when updating an existing cutscene or when prompt is set)"},
    "open": {"type": "boolean", "description": "Open the cutscene in the timeline editor when review is false (default true)"}
  }
}`

const cutsceneScrubSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "file": {"type": "string", "description": "Optional slash-separated *.cutscene.yaml path to open before scrubbing; empty uses the cutscene already open"},
    "timeSeconds": {"type": "number", "minimum": 0, "description": "Timestamp in seconds to scrub the playhead to"},
    "view": {"type": "string", "enum": ["before", "after"], "description": "Optional review view mode to switch to while a cutscene review is open"}
  }
}`

// CutsceneChangeSummary summarizes one change inside a pending cutscene review.
type CutsceneChangeSummary struct {
	ID       string `json:"id"`
	Sign     string `json:"sign"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Reverted bool   `json:"reverted,omitempty"`
}

// CutsceneWriteResult is what cutscene_write reports.
type CutsceneWriteResult struct {
	File    string                  `json:"file"`
	Status  string                  `json:"status"` // "pending_review" or "written"
	Kind    string                  `json:"kind"`
	Length  float64                 `json:"lengthSeconds,omitempty"`
	Tracks  int                     `json:"tracks"`
	Opened  bool                    `json:"opened"`
	Changes []CutsceneChangeSummary `json:"changes,omitempty"`
}

// CutsceneActorPose is one actor's posed coordinates and active state returned by cutscene_scrub.
type CutsceneActorPose struct {
	ID    string  `json:"id"`
	X     float32 `json:"x"`
	Y     float32 `json:"y"`
	State string  `json:"state,omitempty"`
}

// CutsceneCameraPose is the posed camera state returned by cutscene_scrub.
type CutsceneCameraPose struct {
	Label string  `json:"label"`
	X     float32 `json:"x"`
	Y     float32 `json:"y"`
	Zoom  float32 `json:"zoom"`
}

// CutsceneDialoguePose is the active dialogue subtitle returned by cutscene_scrub.
type CutsceneDialoguePose struct {
	Active  bool   `json:"active"`
	ID      string `json:"id,omitempty"`
	Speaker string `json:"speaker,omitempty"`
	Text    string `json:"text,omitempty"`
}

// CutsceneScrubResult is what cutscene_scrub reports.
type CutsceneScrubResult struct {
	File          string               `json:"file,omitempty"`
	TimeSeconds   float64              `json:"timeSeconds"`
	LengthSeconds float64              `json:"lengthSeconds"`
	Camera        CutsceneCameraPose   `json:"camera"`
	Actors        []CutsceneActorPose  `json:"actors,omitempty"`
	Dialogue      CutsceneDialoguePose `json:"dialogue"`
	ReviewOpen    bool                 `json:"reviewOpen,omitempty"`
	ShowingBefore bool                 `json:"showingBefore,omitempty"`
	KeptChanges   int                  `json:"keptChanges,omitempty"`
	TotalChanges  int                  `json:"totalChanges,omitempty"`
}

type cutsceneWriteArgs struct {
	File   string `json:"file"`
	YAML   string `json:"yaml"`
	Prompt string `json:"prompt"`
	Review *bool  `json:"review"`
	Open   *bool  `json:"open"`
}

type cutsceneScrubArgs struct {
	File        string   `json:"file"`
	TimeSeconds *float64 `json:"timeSeconds"`
	View        string   `json:"view"`
}

func cutsceneWrite(ctx context.Context, c *Call) (*Result, error) {
	var args cutsceneWriteArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("cutscene_write: %w", err)
	}
	clean, err := cleanProjectPath(args.File)
	if err != nil {
		return nil, fmt.Errorf("cutscene_write: %w", err)
	}
	lower := strings.ToLower(clean)
	if !strings.HasSuffix(lower, ".cutscene.yaml") && !strings.HasSuffix(lower, ".cutscene.yml") {
		return nil, fmt.Errorf("cutscene_write: %s: file name must end with .cutscene.yaml", clean)
	}
	if strings.TrimSpace(args.YAML) == "" {
		return nil, errors.New("cutscene_write: yaml must not be empty")
	}
	proposed, err := cutscene.Parse([]byte(args.YAML))
	if err != nil {
		return nil, fmt.Errorf("cutscene_write: %s: %w", clean, err)
	}
	canonical, err := cutscene.Encode(proposed)
	if err != nil {
		return nil, fmt.Errorf("cutscene_write: %s: %w", clean, err)
	}

	openInEditor := true
	if args.Open != nil {
		openInEditor = *args.Open
	}

	var res CutsceneWriteResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.Playing() {
			return fmt.Errorf("cutscene_write: %w", editor.ErrPlaying)
		}
		if holdErr := sketchHoldEditError(ed, clean); holdErr != nil {
			return holdErr
		}
		root := projectRoot(ed)
		if root == "" {
			return errors.New("cutscene_write: no project is open in the editor; open one with go tool game editor <project-dir>")
		}
		abs, exists, err := resolveProjectTargetPath(root, clean)
		if err != nil {
			return fmt.Errorf("cutscene_write: %w", err)
		}

		useReview := strings.TrimSpace(args.Prompt) != "" || exists
		if args.Review != nil {
			useReview = *args.Review
		}
		if proposed.EffectiveKind() != cutscene.KindTimeline {
			if args.Review != nil && *args.Review {
				return fmt.Errorf("cutscene_write: %s: review mode is only supported for timeline cutscenes, not %s", clean, proposed.EffectiveKind())
			}
			useReview = false
		}

		if useReview {
			var base *cutscene.Cutscene
			if exists {
				raw, err := os.ReadFile(abs)
				if err != nil {
					return fmt.Errorf("cutscene_write: %s: %w", clean, err)
				}
				parsedBase, err := cutscene.Parse(raw)
				if err != nil {
					return fmt.Errorf("cutscene_write: existing %s: %w", clean, err)
				}
				base = &parsedBase
			} else {
				base = &cutscene.Cutscene{
					Version:   proposed.Version,
					Kind:      proposed.Kind,
					Scene:     proposed.Scene,
					Length:    proposed.Length,
					Group:     proposed.Group,
					Skippable: proposed.Skippable,
					SkipAfter: proposed.SkipAfter,
				}
			}
			if err := ed.OpenCutsceneReview(editor.CutsceneReviewRequest{
				Prompt:   args.Prompt,
				Path:     abs,
				Base:     base,
				Proposed: &proposed,
			}); err != nil {
				return fmt.Errorf("cutscene_write: %s: %w", clean, err)
			}
			changes := ed.CutsceneReviewChanges()
			summaries := make([]CutsceneChangeSummary, len(changes))
			for i, ch := range changes {
				summaries[i] = CutsceneChangeSummary{
					ID:       ch.ID,
					Sign:     ch.Sign,
					Title:    ch.Title,
					Detail:   ch.Detail,
					Reverted: ch.Reverted,
				}
			}
			res = CutsceneWriteResult{
				File:    clean,
				Status:  "pending_review",
				Kind:    string(proposed.EffectiveKind()),
				Length:  time.Duration(proposed.Length).Seconds(),
				Tracks:  len(proposed.Tracks),
				Opened:  true,
				Changes: summaries,
			}
			return nil
		}

		writtenAbs, err := writeProjectFileAtomic(root, clean, canonical)
		if err != nil {
			return fmt.Errorf("cutscene_write: %w", err)
		}
		if openInEditor {
			if err := ed.OpenCutsceneFile(writtenAbs); err != nil {
				return fmt.Errorf("cutscene_write: open %s: %w", clean, err)
			}
		} else {
			ed.CheckProblems()
		}
		res = CutsceneWriteResult{
			File:   clean,
			Status: "written",
			Kind:   string(proposed.EffectiveKind()),
			Length: time.Duration(proposed.Length).Seconds(),
			Tracks: len(proposed.Tracks),
			Opened: openInEditor,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var text string
	if res.Status == "pending_review" {
		text = fmt.Sprintf("Opened cutscene review for %s (%d changes awaiting Keep or Revert; file on disk unchanged).", res.File, len(res.Changes))
	} else {
		text = fmt.Sprintf("Wrote %s (%s, %d tracks).", res.File, res.Kind, res.Tracks)
	}
	return &Result{Text: text, Structured: res}, nil
}

func cutsceneScrub(ctx context.Context, c *Call) (*Result, error) {
	var args cutsceneScrubArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("cutscene_scrub: %w", err)
	}
	if args.TimeSeconds != nil && (math.IsNaN(*args.TimeSeconds) || math.IsInf(*args.TimeSeconds, 0) || *args.TimeSeconds < 0) {
		return nil, fmt.Errorf("cutscene_scrub: timeSeconds %v must be finite and >= 0", *args.TimeSeconds)
	}
	if args.View != "" && args.View != "before" && args.View != "after" {
		return nil, fmt.Errorf("cutscene_scrub: view %q must be \"before\" or \"after\"", args.View)
	}

	var res CutsceneScrubResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		root := projectRoot(ed)
		if args.File != "" {
			if root == "" {
				return fmt.Errorf("cutscene_scrub: %s: no project is open in the editor", args.File)
			}
			clean, err := cleanProjectPath(args.File)
			if err != nil {
				return fmt.Errorf("cutscene_scrub: %w", err)
			}
			abs, exists, err := resolveProjectTargetPath(root, clean)
			if err != nil {
				return fmt.Errorf("cutscene_scrub: %w", err)
			}
			if !exists {
				return fmt.Errorf("cutscene_scrub: %s: no such file", clean)
			}
			if ed.CutscenePath() != abs {
				if err := ed.OpenCutsceneFile(abs); err != nil {
					return fmt.Errorf("cutscene_scrub: %s: %w", clean, err)
				}
			}
		}
		if !ed.CutsceneOpen() {
			return errors.New("cutscene_scrub: no cutscene is open in the editor; pass file or open a *.cutscene.yaml file first")
		}
		if args.View != "" && ed.CutsceneReviewOpen() {
			if args.View == "before" {
				ed.SetCutsceneReviewViewMode(editor.CutsceneReviewBefore)
			} else {
				ed.SetCutsceneReviewViewMode(editor.CutsceneReviewAfter)
			}
		}
		if args.TimeSeconds != nil {
			secs := *args.TimeSeconds
			const maxSecs = float64(math.MaxInt64/int64(time.Second)) - 1
			var target time.Duration
			if secs >= maxSecs {
				target = time.Duration(math.MaxInt64)
			} else {
				target = time.Duration(math.Round(secs * float64(time.Second)))
			}
			ed.SeekCutscene(target)
		}

		file := ed.CutscenePath()
		if root != "" && file != "" {
			if rel, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rel, "..") {
				file = filepath.ToSlash(rel)
			}
		}
		var cam CutsceneCameraPose
		cam.Label = ed.CutsceneCameraLabel()
		cam.Zoom = 1
		var actors []CutsceneActorPose
		if p := ed.CutscenePreviewPlayer(); p != nil {
			cx, cy, cz, _ := p.CameraState()
			cam.X, cam.Y, cam.Zoom = cx, cy, cz
			doc := ed.Cutscene()
			if doc != nil {
				for _, tr := range doc.Tracks {
					if tr.Kind == cutscene.TrackActor && tr.Target != "" {
						ax, ay, ok := p.ActorPosition(tr.Target)
						if ok {
							actors = append(actors, CutsceneActorPose{
								ID:    tr.Target,
								X:     ax,
								Y:     ay,
								State: p.ActorState(tr.Target),
							})
						}
					}
				}
			}
		}
		slices.SortFunc(actors, func(a, b CutsceneActorPose) int { return strings.Compare(a.ID, b.ID) })
		dl := ed.CutsceneActiveDialogue()
		kept, total := ed.CutsceneReviewKeptCount()
		res = CutsceneScrubResult{
			File:          file,
			TimeSeconds:   ed.CutsceneTime().Seconds(),
			LengthSeconds: ed.CutsceneLength().Seconds(),
			Camera:        cam,
			Actors:        actors,
			Dialogue: CutsceneDialoguePose{
				Active:  dl.Active,
				ID:      dl.ID,
				Speaker: dl.Speaker,
				Text:    dl.Text,
			},
			ReviewOpen:    ed.CutsceneReviewOpen(),
			ShowingBefore: ed.CutsceneReviewShowingBefore(),
			KeptChanges:   kept,
			TotalChanges:  total,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	text := fmt.Sprintf("Cutscene at %.1f s / %.1f s · %s", res.TimeSeconds, res.LengthSeconds, res.Camera.Label)
	if res.Dialogue.Active {
		text += fmt.Sprintf(" · subtitle %q (%s)", res.Dialogue.Text, res.Dialogue.ID)
	}
	return &Result{Text: text, Structured: res}, nil
}

// resolveProjectTargetPath resolves rel inside root while verifying no existing ancestor symlink escapes root.
func resolveProjectTargetPath(root, rel string) (abs string, exists bool, err error) {
	realRoot := filepath.Clean(root)
	if r, rerr := filepath.EvalSymlinks(realRoot); rerr == nil {
		realRoot = r
	}
	candidate := filepath.Join(realRoot, filepath.FromSlash(rel))
	probe := candidate
	var tail []string
	for {
		_, lerr := os.Lstat(probe)
		if lerr == nil {
			realProbe, serr := filepath.EvalSymlinks(probe)
			if serr != nil {
				return "", false, fmt.Errorf("%s: %w", rel, serr)
			}
			if !pathWithinRoot(realRoot, realProbe) {
				return "", false, fmt.Errorf("%s: path links to %s, outside the project", rel, realProbe)
			}
			if probe == candidate {
				st, serr := os.Stat(realProbe)
				if serr != nil {
					return "", false, fmt.Errorf("%s: %w", rel, serr)
				}
				if !st.Mode().IsRegular() {
					return "", false, fmt.Errorf("%s: destination is not a regular file", rel)
				}
				return realProbe, true, nil
			}
			slices.Reverse(tail)
			parts := append([]string{realProbe}, tail...)
			return filepath.Join(parts...), false, nil
		}
		if !errors.Is(lerr, os.ErrNotExist) {
			return "", false, fmt.Errorf("%s: %w", rel, lerr)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		tail = append(tail, filepath.Base(probe))
		probe = parent
	}
	return candidate, false, nil
}
