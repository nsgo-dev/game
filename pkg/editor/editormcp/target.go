package editormcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor"
)

// objectTarget is the objects an edit tool writes to, resolved once before the tool takes its holds, so the hold and the write name the same objects whatever the person selects in between.
type objectTarget struct {
	ids []string
	// pinned reports that the open agent turn's pinned selection supplied ids, rather than the call's own id or ids.
	pinned bool
}

// resolveObjectTarget resolves an edit tool's target: the call's explicit IDs, else the selection pinned by the open agent turn the call belongs to. It never reads the live selection, which the person may change while the agent works. single refuses a pin of several objects for a tool that edits one. Nothing is held or changed when it fails.
func (c *Call) resolveObjectTarget(ctx context.Context, explicit []string, single bool) (objectTarget, error) {
	if len(explicit) > 0 {
		return objectTarget{ids: explicit}, nil
	}
	arg, noun := "id", "object"
	if !single {
		arg, noun = "ids", "objects"
	}
	var out objectTarget
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		file := sceneName(ed, projectRoot(ed))
		pin, ok := ed.OpenAgentTurnPin(c.taskID)
		if !ok {
			return &EditError{File: file, Problem: "no " + arg + " was given and no agent turn is open; the selection is context, never a write target", Fix: "pass " + arg + "; selection_get shows the selected " + noun}
		}
		if len(pin.SelectionIDs) == 0 {
			return &EditError{File: file, Problem: "no " + arg + " was given and the request of turn " + pin.TurnID + " pinned no selection", Fix: "pass " + arg + "; agent_turn_get shows what the request is about"}
		}
		if single && len(pin.SelectionIDs) > 1 {
			return &EditError{File: file, Problem: fmt.Sprintf("no id was given and the request of turn %s is about %d objects (%s)", pin.TurnID, len(pin.SelectionIDs), strings.Join(pin.SelectionIDs, ", ")), Fix: "pass id naming the one to edit"}
		}
		if !pin.SceneOpen {
			return &EditError{File: file, Problem: "the objects the request was about are in " + pin.ScenePath + ", which is no longer the open scene", Fix: "open " + pin.ScenePath + " again, or read the open scene with scene_read and pass " + arg}
		}
		out = objectTarget{ids: pin.SelectionIDs, pinned: true}
		return out.missing(ed, file)
	})
	if err != nil {
		return objectTarget{}, err
	}
	return out, nil
}

// missing returns the error for a pinned target whose object is no longer in the open scene, or nil. An explicit ID that names no object keeps the tool's own unknown-object error.
func (t objectTarget) missing(ed *editor.Editor, file string) error {
	if !t.pinned {
		return nil
	}
	for _, id := range t.ids {
		if obj, _, _ := ed.FindObject(id); obj != nil {
			continue
		}
		if obj, _ := ed.FindResolvedObject(id); obj != nil {
			continue
		}
		return &EditError{File: file, Object: id, Problem: "the object the request was about was removed or renamed", Fix: "read the scene again with scene_read and pass id"}
	}
	return nil
}

// holdIDs returns the IDs to hold for this target, with extra appended (such as a new parent).
func (t objectTarget) holdIDs(extra ...string) []string {
	out := append([]string(nil), t.ids...)
	for _, id := range extra {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

// nonEmpty returns id as a one-element target list, or nil when the call gave no id.
func nonEmpty(id string) []string {
	if id == "" {
		return nil
	}
	return []string{id}
}
