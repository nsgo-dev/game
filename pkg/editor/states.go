package editor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/animclip"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrNoStates is returned by state machine edits when no *.states.yaml document is open.
var ErrNoStates = errors.New("editor: no sprite state machine is open")

type statesMeta struct {
	path   string
	target string
}

// StatesClipMeta summarizes a state's animation clip for the graph node card and thumbnail.
type StatesClipMeta struct {
	Frames     int
	Loop       bool
	Summary    string
	FirstFrame string
}

// StatesOutgoing describes one row in the LEAVING <STATE> list.
type StatesOutgoing struct {
	Index   int
	To      string
	Detail  string
	FromAny bool
}

const maxStatesTrace = 512

type statesEditorState struct {
	doc      *animclip.StateMachineDoc
	meta     statesMeta
	saved    []byte
	dirty    bool
	selected string

	playing   bool
	stop      func()
	scene     *sceneformat.BuiltScene
	machine   *animclip.StateMachine
	err       error
	clipMeta  map[string]StatesClipMeta
	driveKeys bool
	actions   *input.Actions
	driver    *animclip.InputDriver
	heldKeys  map[string]bool
	trace     []string
}

// States returns a copy of the sprite state machine open in the editor, or nil.
func (e *Editor) States() *animclip.StateMachineDoc {
	if e == nil || e.states.doc == nil {
		return nil
	}
	return e.states.doc.Clone()
}

// StatesPath returns the file path the open state machine was loaded from or saved to, or "".
func (e *Editor) StatesPath() string { return e.states.meta.path }

// StatesTarget returns the object ID the open state machine previews on.
func (e *Editor) StatesTarget() string { return e.states.meta.target }

// StatesError returns why the open state machine cannot be previewed on the scene, or nil.
func (e *Editor) StatesError() error { return e.states.err }

// StatesDirty reports whether the open state machine differs from its last opened or saved YAML bytes.
func (e *Editor) StatesDirty() bool {
	return e != nil && e.states.doc != nil && e.states.dirty
}

func (e *Editor) updateStatesDirty() {
	if e == nil || e.states.doc == nil {
		if e != nil {
			e.states.dirty = false
		}
		return
	}
	data, err := animclip.EncodeStateMachine(e.states.doc)
	e.states.dirty = err != nil || !bytes.Equal(data, e.states.saved)
}

// SelectedStatesNode returns the currently selected (or active preview) state name in the graph.
func (e *Editor) SelectedStatesNode() string {
	if e == nil || e.states.doc == nil {
		return ""
	}
	if e.states.selected != "" && e.states.doc.State(e.states.selected) != nil {
		return e.states.selected
	}
	if e.states.machine != nil && e.states.machine.State() != "" {
		return e.states.machine.State()
	}
	return e.states.doc.Entry
}

// SelectStatesNode selects state name in the graph and switches the live preview to it.
func (e *Editor) SelectStatesNode(name string) {
	if e == nil || e.states.doc == nil || e.states.doc.State(name) == nil {
		return
	}
	e.states.selected = name
	if e.states.machine != nil {
		e.states.machine.SetState(name)
	}
	e.notify()
}

// StatesCurrentState returns the state currently active in the runtime preview, or SelectedStatesNode when no preview player is active.
func (e *Editor) StatesCurrentState() string {
	if e == nil || e.states.doc == nil {
		return ""
	}
	if e.states.machine != nil && e.states.machine.State() != "" {
		return e.states.machine.State()
	}
	return e.SelectedStatesNode()
}

// StatesPreviousState returns the state active before the most recent preview transition, or "".
func (e *Editor) StatesPreviousState() string {
	if e == nil || e.states.machine == nil {
		return ""
	}
	return e.states.machine.PreviousState()
}

// StatesActiveFrame returns the current sprite frame name shown on the preview target object, or "".
func (e *Editor) StatesActiveFrame() string {
	if e == nil || e.states.scene == nil || e.states.meta.target == "" {
		return ""
	}
	bo := e.states.scene.Object(e.states.meta.target)
	if bo == nil || bo.Sprite == nil {
		return ""
	}
	return bo.Sprite.Frame.Name
}

// StatesDriverPosY returns the simulated vertical jump offset of the Drive with keys preview (<= 0).
func (e *Editor) StatesDriverPosY() float64 {
	if e == nil || e.states.driver == nil {
		return 0
	}
	return e.states.driver.PosY
}

// StatesClipMetaFor returns cached clip metadata for state name.
func (e *Editor) StatesClipMetaFor(name string) (StatesClipMeta, bool) {
	if e == nil || e.states.clipMeta == nil {
		return StatesClipMeta{}, false
	}
	m, ok := e.states.clipMeta[name]
	return m, ok
}

// StatesTransitionTrace returns a copy of the state sequence observed since the preview started or reset.
func (e *Editor) StatesTransitionTrace() []string {
	if e == nil {
		return nil
	}
	return slices.Clone(e.states.trace)
}

// StatesOutgoingFor returns the transitions leaving state name (including Any-state transitions to other states), formatted for the LEAVING <STATE> panel.
func (e *Editor) StatesOutgoingFor(state string) []StatesOutgoing {
	if e == nil || e.states.doc == nil || state == "" {
		return nil
	}
	var direct, anyList []StatesOutgoing
	for i, tr := range e.states.doc.Transitions {
		if tr.From == state {
			direct = append(direct, StatesOutgoing{
				Index:  i,
				To:     tr.To,
				Detail: formatTransitionDetail(tr, false),
			})
		} else if tr.From == animclip.AnyState && tr.To != state {
			anyList = append(anyList, StatesOutgoing{
				Index:   i,
				To:      tr.To,
				Detail:  formatTransitionDetail(tr, true),
				FromAny: true,
			})
		}
	}
	return append(direct, anyList...)
}

func formatTransitionDetail(tr animclip.TransitionDoc, fromAny bool) string {
	var parts []string
	switch {
	case tr.Label != "" && tr.When != "":
		parts = append(parts, tr.Label+": "+tr.When)
	case tr.Label != "":
		parts = append(parts, tr.Label)
	case tr.When != "":
		parts = append(parts, tr.When)
	}
	if tr.OnEnd {
		if len(parts) == 0 {
			parts = append(parts, "when the clip ends")
		} else if !strings.Contains(strings.ToLower(parts[0]), "end") {
			parts = append(parts, "when the clip ends")
		}
	}
	if fromAny {
		parts = append(parts, "from any state")
	}
	if len(parts) == 0 {
		return "always"
	}
	return strings.Join(parts, ", ")
}

// StatesPlaying reports whether the state machine preview is running on game.Loop.
func (e *Editor) StatesPlaying() bool { return e != nil && e.states.playing }

// StatesDriveWithKeys reports whether Drive with keys is enabled in the state machine preview.
func (e *Editor) StatesDriveWithKeys() bool { return e != nil && e.states.driveKeys }

// SetStatesDriveWithKeys enables or disables Drive with keys in the state machine preview.
func (e *Editor) SetStatesDriveWithKeys(on bool) {
	if e == nil || e.states.driveKeys == on {
		return
	}
	if on && e.playing {
		return
	}
	e.states.driveKeys = on
	if on {
		e.ensureStatesInput()
		if e.states.doc != nil && !e.states.playing {
			_ = e.PlayStates()
		}
	} else {
		e.ReleaseAllStatesActions()
	}
	e.notify()
}

// OpenStatesYAML parses data as a *.states.yaml document and opens it to preview on target (selected object, or an object with an animator component, when empty) as an undoable command.
func (e *Editor) OpenStatesYAML(data []byte, target string) error {
	return e.openStates(data, "", target)
}

// OpenStatesFile reads a *.states.yaml file from disk and opens it in the state machine editor.
func (e *Editor) OpenStatesFile(path, target string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return e.openStates(data, path, target)
}

// OpenStatesProjectFile opens a project-relative *.states.yaml file in the state machine editor.
func (e *Editor) OpenStatesProjectFile(relPath string) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	abs := filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(relPath))
	target := e.animatorTargetForStates(relPath)
	return e.OpenStatesFile(abs, target)
}

func (e *Editor) animatorTargetForStates(relPath string) string {
	if e.Doc != nil {
		var matched string
		for li := range e.Doc.Layers {
			sceneformat.WalkObjects(e.Doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
				cfg, ok, err := sceneformat.ResolveAnimatorComponent(o)
				if err == nil && ok && cfg.States == relPath {
					matched = o.ID
					return false
				}
				return true
			})
			if matched != "" {
				return matched
			}
		}
	}
	return e.SelectedID
}

func (e *Editor) openStates(data []byte, path, target string) error {
	if e.playing {
		return ErrPlaying
	}
	doc, err := animclip.ParseStateMachine(data)
	if err != nil {
		if path != "" {
			return fmt.Errorf("%s: %w", path, err)
		}
		return err
	}
	if target == "" {
		target = e.SelectedID
	}
	if target == "" && e.Doc != nil {
		// Fall back to first object with an animator component or first sprite object.
		for li := range e.Doc.Layers {
			sceneformat.WalkObjects(e.Doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
				if _, ok, _ := sceneformat.ResolveAnimatorComponent(o); ok {
					target = o.ID
					return false
				}
				if target == "" && o.Sprite != nil {
					target = o.ID
				}
				return true
			})
			if target != "" {
				break
			}
		}
	}
	if target != "" {
		if obj, _, _ := e.FindObject(target); obj == nil {
			return fmt.Errorf("editor: object %q not found", target)
		}
	}
	canonical, err := animclip.EncodeStateMachine(doc)
	if err != nil {
		return err
	}
	label := "Open state machine"
	if path != "" {
		label += " " + path
	}
	before := e.states.meta
	e.History.Execute(&statesCmd{
		ed:          e,
		label:       label,
		before:      e.states.doc.Clone(),
		after:       doc,
		beforeMeta:  &before,
		afterMeta:   &statesMeta{path: path, target: target},
		beforeSaved: slices.Clone(e.states.saved),
		afterSaved:  canonical,
	})
	return nil
}

// CloseStates closes the open state machine as an undoable command.
func (e *Editor) CloseStates() error {
	if e.playing {
		return ErrPlaying
	}
	if e.states.doc == nil {
		return nil
	}
	before := e.states.meta
	e.History.Execute(&statesCmd{
		ed:          e,
		label:       "Close state machine",
		before:      e.states.doc.Clone(),
		after:       nil,
		beforeMeta:  &before,
		afterMeta:   &statesMeta{},
		beforeSaved: slices.Clone(e.states.saved),
		afterSaved:  nil,
	})
	return nil
}

// SaveStatesYAML encodes the open state machine as canonical deterministic YAML.
func (e *Editor) SaveStatesYAML() ([]byte, error) {
	if e == nil || e.states.doc == nil {
		return nil, ErrNoStates
	}
	return animclip.EncodeStateMachine(e.states.doc)
}

// SaveStatesFile writes the open state machine atomically to path and updates StatesPath.
func (e *Editor) SaveStatesFile(path string) error {
	if e.playing {
		return ErrPlaying
	}
	data, err := e.SaveStatesYAML()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	e.states.meta.path = path
	e.states.saved = data
	e.states.dirty = false
	e.filesRev++
	e.invalidateProjectIndex()
	e.Status = "Saved state machine " + path
	e.notify()
	e.CheckProblems()
	return nil
}

func (e *Editor) saveStatesFromPanel() error {
	if e.playing {
		return ErrPlaying
	}
	if e.OnSaveStates != nil {
		data, err := e.SaveStatesYAML()
		if err != nil {
			return err
		}
		if err := e.OnSaveStates(data); err != nil {
			return err
		}
		e.states.saved = data
		e.states.dirty = false
		e.Status = "Saved state machine"
		e.notify()
		return nil
	}
	if e.states.meta.path == "" {
		return errors.New("editor: no state machine path; open a *.states.yaml file or set OnSaveStates")
	}
	return e.SaveStatesFile(e.states.meta.path)
}

// SetStatesDoc replaces the open state machine's document with doc as one undoable command.
func (e *Editor) SetStatesDoc(label string, doc *animclip.StateMachineDoc) error {
	if e.playing {
		return ErrPlaying
	}
	if doc == nil {
		return errors.New("editor: nil state machine document")
	}
	if err := animclip.ValidateStateMachine(doc); err != nil {
		return err
	}
	if e.states.doc != nil && reflect.DeepEqual(doc, e.states.doc) {
		return nil
	}
	if label == "" {
		label = "Edit state machine"
	}
	e.History.Execute(&statesCmd{
		ed:     e,
		label:  label,
		before: e.states.doc.Clone(),
		after:  doc.Clone(),
	})
	return nil
}

// AddStatesState appends a state to the open state machine as an undoable command.
func (e *Editor) AddStatesState(st animclip.StateDoc) error {
	return e.editStates("Add state "+st.Name, func(d *animclip.StateMachineDoc) error {
		d.States = append(d.States, st)
		return nil
	})
}

// RemoveStatesState removes a state (and transitions touching it) from the open state machine as an undoable command.
func (e *Editor) RemoveStatesState(name string) error {
	return e.editStates("Remove state "+name, func(d *animclip.StateMachineDoc) error {
		idx := slices.IndexFunc(d.States, func(st animclip.StateDoc) bool { return st.Name == name })
		if idx < 0 {
			return fmt.Errorf("editor: state %q not found", name)
		}
		if len(d.States) == 1 {
			return errors.New("editor: cannot remove the last state")
		}
		d.States = slices.Delete(d.States, idx, idx+1)
		if d.Entry == name {
			d.Entry = d.States[0].Name
		}
		d.Transitions = slices.DeleteFunc(d.Transitions, func(tr animclip.TransitionDoc) bool {
			return tr.From == name || tr.To == name
		})
		return nil
	})
}

// SetStatesEntry sets the entry state of the open state machine as an undoable command.
func (e *Editor) SetStatesEntry(name string) error {
	return e.editStates("Set entry state "+name, func(d *animclip.StateMachineDoc) error {
		d.Entry = name
		return nil
	})
}

// SetStatesNodePos updates the graph layout coordinates of state name as an undoable command.
func (e *Editor) SetStatesNodePos(name string, x, y float32) error {
	return e.editStates("Move state "+name, func(d *animclip.StateMachineDoc) error {
		st := d.State(name)
		if st == nil {
			return fmt.Errorf("editor: state %q not found", name)
		}
		nx, ny := x, y
		st.X, st.Y = &nx, &ny
		return nil
	})
}

// AddStatesTransition appends a transition to the open state machine as an undoable command.
func (e *Editor) AddStatesTransition(tr animclip.TransitionDoc) error {
	return e.editStates("Add transition "+tr.From+" -> "+tr.To, func(d *animclip.StateMachineDoc) error {
		d.Transitions = append(d.Transitions, tr)
		return nil
	})
}

// RemoveStatesTransition deletes transition index from the open state machine as an undoable command.
func (e *Editor) RemoveStatesTransition(index int) error {
	return e.editStates("Remove transition", func(d *animclip.StateMachineDoc) error {
		if index < 0 || index >= len(d.Transitions) {
			return fmt.Errorf("editor: transition index %d out of range", index)
		}
		d.Transitions = slices.Delete(d.Transitions, index, index+1)
		return nil
	})
}

// AddStatesParam adds a parameter to the open state machine as an undoable command.
func (e *Editor) AddStatesParam(p animclip.ParamDoc) error {
	return e.editStates("Add parameter "+p.Name, func(d *animclip.StateMachineDoc) error {
		d.Parameters = append(d.Parameters, p)
		return nil
	})
}

// RemoveStatesParam removes parameter name from the open state machine as an undoable command.
func (e *Editor) RemoveStatesParam(name string) error {
	return e.editStates("Remove parameter "+name, func(d *animclip.StateMachineDoc) error {
		idx := slices.IndexFunc(d.Parameters, func(p animclip.ParamDoc) bool { return p.Name == name })
		if idx < 0 {
			return fmt.Errorf("editor: parameter %q not found", name)
		}
		d.Parameters = slices.Delete(d.Parameters, idx, idx+1)
		return nil
	})
}

func (e *Editor) editStates(label string, fn func(d *animclip.StateMachineDoc) error) error {
	if e.playing {
		return ErrPlaying
	}
	if e.states.doc == nil {
		return ErrNoStates
	}
	next := e.states.doc.Clone()
	if err := fn(next); err != nil {
		return err
	}
	if err := animclip.ValidateStateMachine(next); err != nil {
		return err
	}
	if reflect.DeepEqual(next, e.states.doc) {
		return nil
	}
	e.History.Execute(&statesCmd{
		ed:     e,
		label:  label,
		before: e.states.doc.Clone(),
		after:  next,
	})
	return nil
}

// StatesClipResolver returns the ClipResolver used to load clips for the open state machine.
func (e *Editor) StatesClipResolver() animclip.ClipResolver {
	if e == nil {
		return nil
	}
	if e.Clips != nil {
		return e.Clips
	}
	if e.ProjectPath != "" {
		return animclip.FSClips{FS: os.DirFS(filepath.Dir(e.ProjectPath))}
	}
	if e.states.meta.path != "" {
		return animclip.FSClips{FS: os.DirFS(filepath.Dir(e.states.meta.path))}
	}
	return nil
}

// PlayStates starts fixed-step preview playback of the open state machine on game.Loop.
func (e *Editor) PlayStates() error {
	if e.playing {
		return ErrPlaying
	}
	if e.states.doc == nil {
		return ErrNoStates
	}
	if e.states.machine == nil {
		if e.states.err != nil {
			return e.states.err
		}
		return errors.New("editor: state machine cannot be previewed on this scene")
	}
	if e.states.playing {
		return nil
	}
	e.ensureStatesInput()
	e.states.playing = true
	e.states.machine.Play()
	if e.scope != nil {
		loop := &game.Loop{
			Rate:   e.PlayRate,
			Update: e.StepStates,
		}
		e.scope.Run(func() {
			e.states.stop = loop.Run(e.scope)
		})
	}
	e.Status = "Previewing state machine"
	e.notify()
	return nil
}

// PauseStates pauses fixed-step preview playback of the open state machine.
func (e *Editor) PauseStates() {
	if !e.states.playing {
		return
	}
	e.stopStatesLoop()
	e.Status = "Paused state machine"
	e.notify()
}

func (e *Editor) stopStatesLoop() {
	e.states.playing = false
	if e.states.stop != nil {
		e.states.stop()
		e.states.stop = nil
	}
	if e.states.machine != nil {
		e.states.machine.Pause()
	}
}

// StepStates advances the state machine preview by one fixed step dt, feeding project input actions through animclip.InputDriver when Drive with keys is active.
func (e *Editor) StepStates(dt time.Duration) {
	if e == nil || e.states.machine == nil || dt <= 0 {
		return
	}
	if e.states.driveKeys {
		e.ensureStatesInput()
		e.states.actions.Step()
		e.states.driver.StepActions(e.states.machine, e.states.actions, dt)
	}
	if !e.states.machine.Playing() {
		e.states.machine.Play()
	}
	e.states.machine.Update(dt)
	e.states.selected = e.states.machine.State()
	e.notify()
}

// PressStatesAction holds a named input action (such as "move_left", "move_right", "jump", "attack", "hurt") while Drive with keys is enabled.
func (e *Editor) PressStatesAction(action string) {
	if e == nil || e.playing || e.states.doc == nil {
		return
	}
	e.ensureStatesInput()
	e.states.actions.Press(action)
	if e.states.heldKeys == nil {
		e.states.heldKeys = make(map[string]bool)
	}
	e.states.heldKeys[action] = true
	e.notify()
}

// ReleaseStatesAction releases a held input action in Drive with keys mode.
func (e *Editor) ReleaseStatesAction(action string) {
	if e == nil || e.states.actions == nil {
		return
	}
	e.states.actions.Release(action)
	delete(e.states.heldKeys, action)
	e.notify()
}

// TriggerStatesAction pulses a one-shot action (such as "jump", "attack", "hurt") for the next fixed step.
func (e *Editor) TriggerStatesAction(action string) {
	if e == nil || e.playing || e.states.doc == nil {
		return
	}
	e.ensureStatesInput()
	e.states.actions.Trigger(action)
	e.notify()
}

// ReleaseAllStatesActions releases every held movement/action input so the character returns to idle.
func (e *Editor) ReleaseAllStatesActions() {
	if e == nil {
		return
	}
	if e.states.actions != nil {
		for act := range e.states.heldKeys {
			e.states.actions.Release(act)
		}
		e.states.actions.Release("move_left")
		e.states.actions.Release("move_right")
		e.states.actions.Release("left")
		e.states.actions.Release("right")
		e.states.actions.Release("jump")
		e.states.actions.Release("attack")
		e.states.actions.Release("hurt")
	}
	clear(e.states.heldKeys)
	e.notify()
}

// StatesActionHeld reports whether action is currently held in Drive with keys mode.
func (e *Editor) StatesActionHeld(action string) bool {
	return e != nil && e.states.heldKeys[action]
}

// StatesHandleKeyEvent forwards a hardware key event to the Drive with keys input actions when active.
// It ignores events during Play mode, never swallows modifier shortcuts (Cmd/Ctrl/Alt) or unbound keys, and treats Escape as releasing keys and exiting Drive with keys.
func (e *Editor) StatesHandleKeyEvent(ev core.KeyEvent, down bool) bool {
	if e == nil || e.playing || e.states.doc == nil || !e.states.driveKeys {
		return false
	}
	if ev.Modifiers.Platform || ev.Modifiers.Control || ev.Modifiers.Alt {
		return false
	}
	e.ensureStatesInput()
	key := strings.ToLower(strings.TrimSpace(ev.Key))
	if key == "" && strings.TrimSpace(ev.Code) == "" {
		return false
	}
	if key == "escape" || key == "esc" {
		if down {
			e.SetStatesDriveWithKeys(false)
		}
		return true
	}
	norm := ev
	norm.Key = key
	if down {
		if !e.states.actions.OnKeyDown(norm) {
			return false
		}
		if action, ok := defaultDriveKeyAction(key); ok {
			e.states.heldKeys[action] = true
			e.notify()
		}
		return true
	}
	e.states.actions.OnKeyUp(norm)
	if action, ok := defaultDriveKeyAction(key); ok {
		delete(e.states.heldKeys, action)
		e.notify()
	}
	return false
}

func defaultDriveKeyAction(key string) (string, bool) {
	switch key {
	case "arrowleft", "left", "a":
		return "move_left", true
	case "arrowright", "right", "d":
		return "move_right", true
	case " ", "space":
		return "jump", true
	case "j":
		return "attack", true
	case "h":
		return "hurt", true
	default:
		return "", false
	}
}

func (e *Editor) ensureStatesInput() {
	if e.states.driver == nil {
		e.states.driver = animclip.NewInputDriver()
	}
	if e.states.actions == nil {
		a := e.BuildInputActions()
		// Ensure standard Drive with keys bindings exist when the project has not declared them.
		hasAny := func(name string) bool {
			return len(a.Bindings(name)) > 0
		}
		if !hasAny("move_left") && !hasAny("left") {
			a.Bind("move_left", input.Key("left"), input.Key("arrowleft"), input.Key("a"), input.KeyCode("ArrowLeft"))
		}
		if !hasAny("move_right") && !hasAny("right") {
			a.Bind("move_right", input.Key("right"), input.Key("arrowright"), input.Key("d"), input.KeyCode("ArrowRight"))
		}
		if !hasAny("jump") {
			a.Bind("jump", input.Key("space"), input.Key(" "), input.KeyCode("Space"))
		}
		if !hasAny("attack") {
			a.Bind("attack", input.Key("j"))
		}
		if !hasAny("hurt") {
			a.Bind("hurt", input.Key("h"))
		}
		e.states.actions = a
	}
	if e.states.heldKeys == nil {
		e.states.heldKeys = make(map[string]bool)
	}
}

func (e *Editor) refreshStatesPreview() {
	e.states.scene, e.states.machine, e.states.err = nil, nil, nil
	e.states.clipMeta = nil
	if e.states.doc == nil || e.playing || e.Doc == nil {
		e.stopStatesLoop()
		return
	}
	if e.BuildErr != nil {
		e.states.err = e.BuildErr
		e.stopStatesLoop()
		return
	}
	clips := e.StatesClipResolver()
	if clips == nil {
		e.states.err = errors.New("editor: no clip resolver configured for state machine preview")
		e.stopStatesLoop()
		return
	}

	// Build clip metadata for every state node card.
	meta := make(map[string]StatesClipMeta, len(e.states.doc.States))
	for _, st := range e.states.doc.States {
		clip, err := clips.Animation(st.Clip)
		if err != nil {
			continue
		}
		loop := clip.Loop
		if st.Loop != nil {
			loop = *st.Loop
		}
		frames := 0
		firstFrame := ""
		for _, tr := range clip.Tracks {
			if tr.IsFrameTrack() {
				frames = len(tr.Frames)
				if frames > 0 {
					firstFrame = tr.Frames[0].Frame
				}
				break
			}
		}
		mode := "once"
		if loop {
			mode = "loop"
		}
		summary := fmt.Sprintf("%.0f ms · %s", animclip.KeyValue(clip.Length)*1000, mode)
		if frames > 0 {
			summary = fmt.Sprintf("%d frames · %s", frames, mode)
		}
		meta[st.Name] = StatesClipMeta{
			Frames:     frames,
			Loop:       loop,
			Summary:    summary,
			FirstFrame: firstFrame,
		}
	}
	e.states.clipMeta = meta

	data, err := sceneformat.Encode(e.Doc)
	if err != nil {
		e.states.err = err
		e.stopStatesLoop()
		return
	}
	doc, err := sceneformat.Parse(data)
	if err != nil {
		e.states.err = err
		e.stopStatesLoop()
		return
	}
	built, err := sceneformat.Build(doc, e.Resolver)
	if err != nil {
		e.states.err = err
		e.stopStatesLoop()
		return
	}
	var bind animclip.Binder = animclip.NoopBinder
	if e.states.meta.target != "" {
		bind = animclip.BindScene(built, e.states.meta.target)
	}
	sm, err := animclip.NewStateMachine(e.states.doc, clips, e.components, bind)
	if err != nil {
		e.states.err = err
		e.stopStatesLoop()
		return
	}
	e.states.trace = []string{sm.State()}
	sm.OnTransition = func(_, to string) {
		if len(e.states.trace) >= maxStatesTrace {
			copy(e.states.trace, e.states.trace[1:])
			e.states.trace[len(e.states.trace)-1] = to
		} else {
			e.states.trace = append(e.states.trace, to)
		}
		e.states.selected = to
	}
	sm.OnEvent = func(st, ev string) {
		e.Status = fmt.Sprintf("State %s event: %s", st, ev)
	}
	if e.states.selected != "" && e.states.doc.State(e.states.selected) != nil && e.states.selected != sm.State() {
		sm.SetState(e.states.selected)
		e.states.trace = []string{sm.State()}
	} else {
		e.states.selected = sm.State()
	}
	if !e.states.playing {
		sm.Pause()
	}
	e.states.scene = built
	e.states.machine = sm
}

// isStatesFile reports whether relPath is a sprite state machine file (*.states.yaml or *.states.yml).
func isStatesFile(relPath string) bool {
	lower := strings.ToLower(relPath)
	return strings.HasSuffix(lower, ".states.yaml") || strings.HasSuffix(lower, ".states.yml")
}

// statesCmd applies and undoes state machine edits.
type statesCmd struct {
	ed                    *Editor
	label                 string
	before, after         *animclip.StateMachineDoc
	beforeMeta, afterMeta *statesMeta
	beforeSaved           []byte
	afterSaved            []byte
}

func (c *statesCmd) Label() string { return c.label }

func (c *statesCmd) Do() { c.apply(c.after, c.afterMeta, c.afterSaved) }

func (c *statesCmd) Undo() { c.apply(c.before, c.beforeMeta, c.beforeSaved) }

func (c *statesCmd) apply(doc *animclip.StateMachineDoc, meta *statesMeta, saved []byte) {
	e := c.ed
	e.states.doc = doc.Clone()
	if meta != nil {
		e.stopStatesLoop()
		e.states.meta = *meta
		e.states.saved = slices.Clone(saved)
		e.states.actions = nil
		if e.states.driver != nil {
			e.states.driver.Reset()
		}
		clear(e.states.heldKeys)
		if doc != nil {
			e.states.selected = doc.Entry
		} else {
			e.states.selected = ""
		}
	}
	e.updateStatesDirty()
	e.Status = c.label
}
