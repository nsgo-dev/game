package editor

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// InputMapTab selects which page of the Input Map panel is visible.
type InputMapTab string

const (
	// InputMapTabActions shows actions, bindings, key capture and the region canvas.
	InputMapTabActions InputMapTab = "actions"
	// InputMapTabAxes shows action axes, motion axes and gamepad stick axes.
	InputMapTabAxes InputMapTab = "axes"
)

type regionDragMode uint8

const (
	regionDragNone regionDragMode = iota
	regionDragMove
	regionDragResize
	regionDragCreate
)

// inputMapState holds the Input Map page's transient UI state, key capture mode and region canvas drag state.
type inputMapState struct {
	tab             InputMapTab
	selectedAction  string
	selectedBinding int
	regionDrawKind  string

	capturingKey  bool
	captureAction string
	captureKind   string

	dragMode    regionDragMode
	dragAction  string
	dragBinding int
	dragStartX  float32
	dragStartY  float32
	dragOrig    project.Rect
	dragPreview project.Rect
	dragChanged bool
}

func (e *Editor) resetInputMapTransient() {
	if e == nil {
		return
	}
	e.inputMap.capturingKey = false
	e.inputMap.captureAction = ""
	e.inputMap.captureKind = ""
	e.inputMap.dragMode = regionDragNone
	e.inputMap.dragAction = ""
	e.inputMap.dragBinding = -1
	e.inputMap.dragChanged = false
}

// InputMapOpen reports whether the Settings view is open on the Input map page.
func (e *Editor) InputMapOpen() bool {
	return e != nil && e.settingsOpen && e.settingsPage == SettingsPageInput
}

// SetInputMapOpen opens the Settings view on the Input map page, or closes Settings when open is false and the Input map page is active. Closing cancels any active key capture or region drag.
func (e *Editor) SetInputMapOpen(open bool) {
	if e == nil {
		return
	}
	if open {
		e.OpenSettingsPage(SettingsPageInput)
		return
	}
	if e.InputMapOpen() {
		e.CloseSettings()
		return
	}
	e.resetInputMapTransient()
}

// InputMapTab returns the active tab of the Input Map panel, defaulting to InputMapTabActions.
func (e *Editor) InputMapTab() InputMapTab {
	if e == nil || e.inputMap.tab == "" {
		return InputMapTabActions
	}
	return e.inputMap.tab
}

// SetInputMapTab switches the active tab of the Input Map panel.
func (e *Editor) SetInputMapTab(tab InputMapTab) {
	if e == nil {
		return
	}
	if tab != InputMapTabActions && tab != InputMapTabAxes {
		tab = InputMapTabActions
	}
	if e.InputMapTab() == tab {
		return
	}
	e.inputMap.tab = tab
	if tab != InputMapTabActions {
		e.resetInputMapTransient()
	}
	e.notify()
}

// InputMapRegionDrawKind returns the binding kind created when dragging a new region on empty space ("touchRegion" or "mouseRegion").
func (e *Editor) InputMapRegionDrawKind() string {
	if e == nil || e.inputMap.regionDrawKind == "" {
		return project.KindTouchRegion
	}
	return e.inputMap.regionDrawKind
}

// SetInputMapRegionDrawKind sets the binding kind ("touchRegion" or "mouseRegion") created when drawing a new region on the canvas.
func (e *Editor) SetInputMapRegionDrawKind(kind string) {
	if e == nil {
		return
	}
	if kind != project.KindTouchRegion && kind != project.KindMouseRegion {
		kind = project.KindTouchRegion
	}
	if e.InputMapRegionDrawKind() == kind {
		return
	}
	e.inputMap.regionDrawKind = kind
	e.notify()
}

// InputMapDisplaySize returns the virtual display size the Input Map's region canvas maps onto: Project.Display when set, else Project.Window, else 640x360.
func (e *Editor) InputMapDisplaySize() (width, height float32) {
	if e != nil && e.Project != nil {
		if e.Project.Display != nil && e.Project.Display.Width > 0 && e.Project.Display.Height > 0 {
			return e.Project.Display.Width, e.Project.Display.Height
		}
		if e.Project.Window.Width > 0 && e.Project.Window.Height > 0 {
			return e.Project.Window.Width, e.Project.Window.Height
		}
	}
	return 640, 360
}

// BuildInputActions builds a fresh input.Actions populated from the open project's input map via project.File.BindInput.
func (e *Editor) BuildInputActions() *input.Actions {
	a := input.NewActions()
	if e != nil && e.Project != nil {
		e.Project.BindInput(a)
	}
	return a
}

// InputActions returns a deep copy of the open project's declared input actions in order.
func (e *Editor) InputActions() []project.Action {
	if e == nil || e.Project == nil || e.Project.Input == nil || len(e.Project.Input.Actions) == 0 {
		return nil
	}
	return e.Project.Clone().Input.Actions
}

// InputAxes returns a copy of the open project's declared action axes in order.
func (e *Editor) InputAxes() []project.Axis {
	if e == nil || e.Project == nil || e.Project.Input == nil || len(e.Project.Input.Axes) == 0 {
		return nil
	}
	return slices.Clone(e.Project.Input.Axes)
}

// InputMotionAxes returns a copy of the open project's declared motion axes in order.
func (e *Editor) InputMotionAxes() []project.MotionAxis {
	if e == nil || e.Project == nil || e.Project.Input == nil || len(e.Project.Input.MotionAxes) == 0 {
		return nil
	}
	return slices.Clone(e.Project.Input.MotionAxes)
}

// InputGamepadAxes returns a copy of the open project's declared gamepad stick axes in order.
func (e *Editor) InputGamepadAxes() []project.GamepadAxis {
	if e == nil || e.Project == nil || e.Project.Input == nil || len(e.Project.Input.GamepadAxes) == 0 {
		return nil
	}
	return slices.Clone(e.Project.Input.GamepadAxes)
}

// SelectedInputAction returns the selected action name in the Input Map panel, falling back to the first declared action when unset or missing.
func (e *Editor) SelectedInputAction() string {
	if e == nil || e.Project == nil || e.Project.Input == nil || len(e.Project.Input.Actions) == 0 {
		return ""
	}
	if e.inputMap.selectedAction != "" {
		if e.findInputActionIndex(e.inputMap.selectedAction) >= 0 {
			return e.inputMap.selectedAction
		}
	}
	return e.Project.Input.Actions[0].Name
}

// SelectInputAction selects action in the Input Map panel when it exists.
func (e *Editor) SelectInputAction(name string) bool {
	if e == nil || e.findInputActionIndex(name) < 0 {
		return false
	}
	if e.inputMap.selectedAction == name {
		return true
	}
	e.inputMap.selectedAction = name
	e.inputMap.selectedBinding = 0
	if e.inputMap.capturingKey && e.inputMap.captureAction != name {
		e.inputMap.capturingKey = false
	}
	e.notify()
	return true
}

// SelectedInputBinding returns the selected binding index within the selected action, or -1 when the selected action has no bindings.
func (e *Editor) SelectedInputBinding() int {
	if e == nil {
		return -1
	}
	idx := e.findInputActionIndex(e.SelectedInputAction())
	if idx < 0 {
		return -1
	}
	n := len(e.Project.Input.Actions[idx].Bindings)
	if n == 0 {
		return -1
	}
	if e.inputMap.selectedBinding < 0 || e.inputMap.selectedBinding >= n {
		return 0
	}
	return e.inputMap.selectedBinding
}

// SelectInputBinding selects binding index within the currently selected action.
func (e *Editor) SelectInputBinding(index int) bool {
	if e == nil {
		return false
	}
	idx := e.findInputActionIndex(e.SelectedInputAction())
	if idx < 0 || index < 0 || index >= len(e.Project.Input.Actions[idx].Bindings) {
		return false
	}
	if e.inputMap.selectedBinding == index {
		return true
	}
	e.inputMap.selectedBinding = index
	e.notify()
	return true
}

// AddInputAction appends a new named action to game.yaml's input.actions through EditProject and selects it.
func (e *Editor) AddInputAction(name string) error {
	if err := e.EditProject("Add Input Action "+name, func(f *project.File) {
		if f.Input == nil {
			f.Input = &project.Input{}
		}
		f.Input.Actions = append(f.Input.Actions, project.Action{Name: name})
	}); err != nil {
		return err
	}
	e.inputMap.selectedAction = name
	e.inputMap.selectedBinding = 0
	e.notify()
	return nil
}

// RenameInputAction renames an existing action and updates every axis in input.axes whose negative or positive action references oldName, all in one undoable EditProject step.
func (e *Editor) RenameInputAction(oldName, newName string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findInputActionIndex(oldName)
	if idx < 0 {
		return fmt.Errorf("editor: input action %q not found", oldName)
	}
	if oldName == newName {
		return nil
	}
	if err := e.EditProject(fmt.Sprintf("Rename Input Action %s to %s", oldName, newName), func(f *project.File) {
		f.Input.Actions[idx].Name = newName
		for i := range f.Input.Axes {
			if f.Input.Axes[i].Negative == oldName {
				f.Input.Axes[i].Negative = newName
			}
			if f.Input.Axes[i].Positive == oldName {
				f.Input.Axes[i].Positive = newName
			}
		}
	}); err != nil {
		return err
	}
	if e.inputMap.selectedAction == oldName {
		e.inputMap.selectedAction = newName
	}
	if e.inputMap.captureAction == oldName {
		e.inputMap.captureAction = newName
	}
	e.notify()
	return nil
}

// DeleteInputAction removes an action from game.yaml's input.actions through EditProject. If an axis still reads the action, project.Validate refuses the edit and nothing changes.
func (e *Editor) DeleteInputAction(name string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findInputActionIndex(name)
	if idx < 0 {
		return fmt.Errorf("editor: input action %q not found", name)
	}
	if err := e.EditProject("Delete Input Action "+name, func(f *project.File) {
		f.Input.Actions = slices.Delete(f.Input.Actions, idx, idx+1)
		pruneEmptyInput(f)
	}); err != nil {
		return err
	}
	if e.inputMap.selectedAction == name {
		e.inputMap.selectedAction = ""
		e.inputMap.selectedBinding = 0
	}
	if e.inputMap.captureAction == name {
		e.resetInputMapTransient()
	}
	e.notify()
	return nil
}

// DefaultInputBinding returns a valid default project.Binding of kind sized sensibly for the project's virtual display.
func (e *Editor) DefaultInputBinding(kind string) (project.Binding, error) {
	dw, dh := e.InputMapDisplaySize()
	rw := max(float32(16), float32(math.Round(float64(dw)/4)))
	rh := max(float32(16), float32(math.Round(float64(dh)/4)))
	rx := float32(math.Round(float64(dw-rw) / 2))
	ry := float32(math.Round(float64(dh-rh) / 2))
	switch kind {
	case project.KindKey:
		return project.Binding{Kind: project.KindKey, Key: "space"}, nil
	case project.KindKeyCode:
		return project.Binding{Kind: project.KindKeyCode, Code: "Space"}, nil
	case project.KindGamepadButton:
		return project.Binding{Kind: project.KindGamepadButton, Button: "south"}, nil
	case project.KindMouseButton:
		return project.Binding{Kind: project.KindMouseButton, Button: "left"}, nil
	case project.KindTouchRegion:
		return project.Binding{Kind: project.KindTouchRegion, Region: &project.Rect{X: rx, Y: ry, Width: rw, Height: rh}}, nil
	case project.KindMouseRegion:
		return project.Binding{Kind: project.KindMouseRegion, Button: "left", Region: &project.Rect{X: rx, Y: ry, Width: rw, Height: rh}}, nil
	default:
		return project.Binding{}, fmt.Errorf("editor: unknown binding kind %q", kind)
	}
}

// AddInputBinding appends binding b to action through EditProject and returns its index.
func (e *Editor) AddInputBinding(action string, b project.Binding) (int, error) {
	if e == nil {
		return -1, errors.New("editor: nil editor")
	}
	if e.playing {
		return -1, ErrPlaying
	}
	if e.Project == nil {
		return -1, ErrNoProject
	}
	idx := e.findInputActionIndex(action)
	if idx < 0 {
		return -1, fmt.Errorf("editor: input action %q not found", action)
	}
	newIdx := -1
	if err := e.EditProject(fmt.Sprintf("Add %s Binding to %s", b.Kind, action), func(f *project.File) {
		nb := cloneBinding(b)
		f.Input.Actions[idx].Bindings = append(f.Input.Actions[idx].Bindings, nb)
		newIdx = len(f.Input.Actions[idx].Bindings) - 1
	}); err != nil {
		return -1, err
	}
	e.inputMap.selectedAction = action
	e.inputMap.selectedBinding = newIdx
	e.notify()
	return newIdx, nil
}

// SetInputBinding replaces binding index on action through EditProject.
func (e *Editor) SetInputBinding(action string, index int, b project.Binding) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findInputActionIndex(action)
	if idx < 0 {
		return fmt.Errorf("editor: input action %q not found", action)
	}
	if index < 0 || index >= len(e.Project.Input.Actions[idx].Bindings) {
		return fmt.Errorf("editor: input binding index %d out of range", index)
	}
	return e.EditProject(fmt.Sprintf("Edit Binding %d on %s", index, action), func(f *project.File) {
		f.Input.Actions[idx].Bindings[index] = cloneBinding(b)
	})
}

// SetInputRegion updates the rectangle of a touchRegion or mouseRegion binding through EditProject.
func (e *Editor) SetInputRegion(action string, index int, r project.Rect) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findInputActionIndex(action)
	if idx < 0 {
		return fmt.Errorf("editor: input action %q not found", action)
	}
	if index < 0 || index >= len(e.Project.Input.Actions[idx].Bindings) {
		return fmt.Errorf("editor: input binding index %d out of range", index)
	}
	cur := e.Project.Input.Actions[idx].Bindings[index]
	if cur.Kind != project.KindTouchRegion && cur.Kind != project.KindMouseRegion {
		return fmt.Errorf("editor: binding %d on %q has kind %q, which has no region", index, action, cur.Kind)
	}
	nb := cloneBinding(cur)
	nr := r
	nb.Region = &nr
	return e.EditProject(fmt.Sprintf("Update Region on %s", action), func(f *project.File) {
		f.Input.Actions[idx].Bindings[index] = nb
	})
}

// RemoveInputBinding removes binding index from action through EditProject.
func (e *Editor) RemoveInputBinding(action string, index int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findInputActionIndex(action)
	if idx < 0 {
		return fmt.Errorf("editor: input action %q not found", action)
	}
	if index < 0 || index >= len(e.Project.Input.Actions[idx].Bindings) {
		return fmt.Errorf("editor: input binding index %d out of range", index)
	}
	if err := e.EditProject(fmt.Sprintf("Remove Binding %d from %s", index, action), func(f *project.File) {
		f.Input.Actions[idx].Bindings = slices.Delete(f.Input.Actions[idx].Bindings, index, index+1)
	}); err != nil {
		return err
	}
	if e.inputMap.selectedAction == action && e.inputMap.selectedBinding >= len(e.Project.Input.Actions[idx].Bindings) {
		e.inputMap.selectedBinding = max(0, len(e.Project.Input.Actions[idx].Bindings)-1)
	}
	e.notify()
	return nil
}

// CapturingKey reports whether the Input Map panel is waiting for a key press to bind.
func (e *Editor) CapturingKey() bool {
	return e != nil && e.inputMap.capturingKey
}

// CaptureKeyAction returns the action name waiting for a captured key, or empty when not capturing.
func (e *Editor) CaptureKeyAction() string {
	if e == nil || !e.inputMap.capturingKey {
		return ""
	}
	return e.inputMap.captureAction
}

// CaptureKeyKind returns the binding kind being captured ("key" or "keyCode"), or empty when not capturing.
func (e *Editor) CaptureKeyKind() string {
	if e == nil || !e.inputMap.capturingKey {
		return ""
	}
	return e.inputMap.captureKind
}

// BeginKeyCapture enters "press a key" capture mode for action. kind must be project.KindKey (or empty, which defaults to KindKey) or project.KindKeyCode.
func (e *Editor) BeginKeyCapture(action, kind string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if action == "" {
		action = e.SelectedInputAction()
	}
	if e.findInputActionIndex(action) < 0 {
		return fmt.Errorf("editor: input action %q not found", action)
	}
	if kind == "" {
		kind = project.KindKey
	}
	if kind != project.KindKey && kind != project.KindKeyCode {
		return fmt.Errorf("editor: key capture kind %q must be %s or %s", kind, project.KindKey, project.KindKeyCode)
	}
	e.inputMap.selectedAction = action
	e.inputMap.capturingKey = true
	e.inputMap.captureAction = action
	e.inputMap.captureKind = kind
	e.Status = fmt.Sprintf("Press a key to bind to %q...", action)
	e.notify()
	return nil
}

// CancelKeyCapture exits "press a key" capture mode without modifying the project.
func (e *Editor) CancelKeyCapture() {
	if e == nil || !e.inputMap.capturingKey {
		return
	}
	e.inputMap.capturingKey = false
	e.inputMap.captureAction = ""
	e.inputMap.captureKind = ""
	e.Status = "Key capture cancelled"
	e.notify()
}

// CaptureKeyEvent handles a hardware key press while key capture is active, adding the captured key or keyCode binding to the target action through EditProject and ending capture mode.
func (e *Editor) CaptureKeyEvent(ev core.KeyEvent) (bool, error) {
	if e == nil || !e.inputMap.capturingKey {
		return false, nil
	}
	if ev.Repeat {
		return true, nil
	}
	action := e.inputMap.captureAction
	kind := e.inputMap.captureKind
	var b project.Binding
	switch kind {
	case project.KindKey:
		k := strings.ToLower(strings.TrimSpace(ev.Key))
		if k == "" {
			return false, nil
		}
		b = project.Binding{Kind: project.KindKey, Key: k}
	case project.KindKeyCode:
		code := strings.TrimSpace(ev.Code)
		if code == "" {
			return false, nil
		}
		b = project.Binding{Kind: project.KindKeyCode, Code: code}
	default:
		e.CancelKeyCapture()
		return false, nil
	}
	e.inputMap.capturingKey = false
	e.inputMap.captureAction = ""
	e.inputMap.captureKind = ""
	if _, err := e.AddInputBinding(action, b); err != nil {
		e.notify()
		return true, err
	}
	if b.Kind == project.KindKey {
		e.Status = fmt.Sprintf("Bound key %q to %q", b.Key, action)
	} else {
		e.Status = fmt.Sprintf("Bound keyCode %q to %q", b.Code, action)
	}
	e.notify()
	return true, nil
}

// AddInputAxis appends an action axis to game.yaml's input.axes through EditProject.
func (e *Editor) AddInputAxis(ax project.Axis) error {
	return e.EditProject("Add Input Axis "+ax.Name, func(f *project.File) {
		if f.Input == nil {
			f.Input = &project.Input{}
		}
		f.Input.Axes = append(f.Input.Axes, ax)
	})
}

// SetInputAxis replaces input.axes[index] through EditProject.
func (e *Editor) SetInputAxis(index int, ax project.Axis) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Input == nil || index < 0 || index >= len(e.Project.Input.Axes) {
		return fmt.Errorf("editor: input axis index %d out of range", index)
	}
	return e.EditProject("Edit Input Axis "+ax.Name, func(f *project.File) {
		f.Input.Axes[index] = ax
	})
}

// DeleteInputAxis removes input.axes[index] through EditProject.
func (e *Editor) DeleteInputAxis(index int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Input == nil || index < 0 || index >= len(e.Project.Input.Axes) {
		return fmt.Errorf("editor: input axis index %d out of range", index)
	}
	name := e.Project.Input.Axes[index].Name
	return e.EditProject("Delete Input Axis "+name, func(f *project.File) {
		f.Input.Axes = slices.Delete(f.Input.Axes, index, index+1)
		pruneEmptyInput(f)
	})
}

// AddInputMotionAxis appends a motion axis to game.yaml's input.motionAxes through EditProject.
func (e *Editor) AddInputMotionAxis(m project.MotionAxis) error {
	return e.EditProject("Add Motion Axis "+m.Name, func(f *project.File) {
		if f.Input == nil {
			f.Input = &project.Input{}
		}
		f.Input.MotionAxes = append(f.Input.MotionAxes, m)
	})
}

// SetInputMotionAxis replaces input.motionAxes[index] through EditProject.
func (e *Editor) SetInputMotionAxis(index int, m project.MotionAxis) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Input == nil || index < 0 || index >= len(e.Project.Input.MotionAxes) {
		return fmt.Errorf("editor: motion axis index %d out of range", index)
	}
	return e.EditProject("Edit Motion Axis "+m.Name, func(f *project.File) {
		f.Input.MotionAxes[index] = m
	})
}

// DeleteInputMotionAxis removes input.motionAxes[index] through EditProject.
func (e *Editor) DeleteInputMotionAxis(index int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Input == nil || index < 0 || index >= len(e.Project.Input.MotionAxes) {
		return fmt.Errorf("editor: motion axis index %d out of range", index)
	}
	name := e.Project.Input.MotionAxes[index].Name
	return e.EditProject("Delete Motion Axis "+name, func(f *project.File) {
		f.Input.MotionAxes = slices.Delete(f.Input.MotionAxes, index, index+1)
		pruneEmptyInput(f)
	})
}

// AddInputGamepadAxis appends a gamepad stick axis to game.yaml's input.gamepadAxes through EditProject.
func (e *Editor) AddInputGamepadAxis(g project.GamepadAxis) error {
	return e.EditProject("Add Gamepad Axis "+g.Name, func(f *project.File) {
		if f.Input == nil {
			f.Input = &project.Input{}
		}
		f.Input.GamepadAxes = append(f.Input.GamepadAxes, g)
	})
}

// SetInputGamepadAxis replaces input.gamepadAxes[index] through EditProject.
func (e *Editor) SetInputGamepadAxis(index int, g project.GamepadAxis) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Input == nil || index < 0 || index >= len(e.Project.Input.GamepadAxes) {
		return fmt.Errorf("editor: gamepad axis index %d out of range", index)
	}
	return e.EditProject("Edit Gamepad Axis "+g.Name, func(f *project.File) {
		f.Input.GamepadAxes[index] = g
	})
}

// DeleteInputGamepadAxis removes input.gamepadAxes[index] through EditProject.
func (e *Editor) DeleteInputGamepadAxis(index int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Input == nil || index < 0 || index >= len(e.Project.Input.GamepadAxes) {
		return fmt.Errorf("editor: gamepad axis index %d out of range", index)
	}
	name := e.Project.Input.GamepadAxes[index].Name
	return e.EditProject("Delete Gamepad Axis "+name, func(f *project.File) {
		f.Input.GamepadAxes = slices.Delete(f.Input.GamepadAxes, index, index+1)
		pruneEmptyInput(f)
	})
}

// BeginRegionDrag begins dragging, resizing, or drawing a touch/mouse region at virtual display coordinates (x, y).
func (e *Editor) BeginRegionDrag(x, y float32) {
	if e == nil || e.playing || e.Project == nil {
		return
	}
	dw, dh := e.InputMapDisplaySize()
	handleTol := max(float32(6), min(dw, dh)*0.06)

	// 1. Check the selected action's selected region binding first for resize handle or move.
	selAct := e.SelectedInputAction()
	selBind := e.SelectedInputBinding()
	if ai := e.findInputActionIndex(selAct); ai >= 0 && selBind >= 0 && selBind < len(e.Project.Input.Actions[ai].Bindings) {
		b := e.Project.Input.Actions[ai].Bindings[selBind]
		if b.Region != nil {
			r := *b.Region
			if nearCorner(x, y, r.X+r.Width, r.Y+r.Height, handleTol) {
				e.inputMap.dragMode = regionDragResize
				e.inputMap.dragAction = selAct
				e.inputMap.dragBinding = selBind
				e.inputMap.dragStartX = x
				e.inputMap.dragStartY = y
				e.inputMap.dragOrig = r
				e.inputMap.dragPreview = r
				e.inputMap.dragChanged = false
				e.notify()
				return
			}
			if pointInRect(x, y, r) {
				e.inputMap.dragMode = regionDragMove
				e.inputMap.dragAction = selAct
				e.inputMap.dragBinding = selBind
				e.inputMap.dragStartX = x
				e.inputMap.dragStartY = y
				e.inputMap.dragOrig = r
				e.inputMap.dragPreview = r
				e.inputMap.dragChanged = false
				e.notify()
				return
			}
		}
	}

	// 2. Hit-test any other region binding across all actions (topmost / last drawn first).
	if e.Project.Input != nil {
		for ai := len(e.Project.Input.Actions) - 1; ai >= 0; ai-- {
			act := e.Project.Input.Actions[ai]
			for bi := len(act.Bindings) - 1; bi >= 0; bi-- {
				b := act.Bindings[bi]
				if b.Region == nil {
					continue
				}
				r := *b.Region
				if nearCorner(x, y, r.X+r.Width, r.Y+r.Height, handleTol) {
					e.inputMap.selectedAction = act.Name
					e.inputMap.selectedBinding = bi
					e.inputMap.dragMode = regionDragResize
					e.inputMap.dragAction = act.Name
					e.inputMap.dragBinding = bi
					e.inputMap.dragStartX = x
					e.inputMap.dragStartY = y
					e.inputMap.dragOrig = r
					e.inputMap.dragPreview = r
					e.inputMap.dragChanged = false
					e.notify()
					return
				}
				if pointInRect(x, y, r) {
					e.inputMap.selectedAction = act.Name
					e.inputMap.selectedBinding = bi
					e.inputMap.dragMode = regionDragMove
					e.inputMap.dragAction = act.Name
					e.inputMap.dragBinding = bi
					e.inputMap.dragStartX = x
					e.inputMap.dragStartY = y
					e.inputMap.dragOrig = r
					e.inputMap.dragPreview = r
					e.inputMap.dragChanged = false
					e.notify()
					return
				}
			}
		}
	}

	// 3. Drag on empty space draws a new region rectangle for the selected action.
	if selAct == "" {
		return
	}
	e.inputMap.dragMode = regionDragCreate
	e.inputMap.dragAction = selAct
	e.inputMap.dragBinding = -1
	e.inputMap.dragStartX = x
	e.inputMap.dragStartY = y
	e.inputMap.dragOrig = project.Rect{X: roundDisplay(x), Y: roundDisplay(y), Width: 1, Height: 1}
	e.inputMap.dragPreview = e.inputMap.dragOrig
	e.inputMap.dragChanged = false
	e.notify()
}

// UpdateRegionDrag updates the live preview rectangle during a region drag in virtual display coordinates (x, y).
func (e *Editor) UpdateRegionDrag(x, y float32) {
	if e == nil || e.inputMap.dragMode == regionDragNone {
		return
	}
	dx := x - e.inputMap.dragStartX
	dy := y - e.inputMap.dragStartY
	switch e.inputMap.dragMode {
	case regionDragMove:
		e.inputMap.dragPreview = project.Rect{
			X:      roundDisplay(e.inputMap.dragOrig.X + dx),
			Y:      roundDisplay(e.inputMap.dragOrig.Y + dy),
			Width:  e.inputMap.dragOrig.Width,
			Height: e.inputMap.dragOrig.Height,
		}
		e.inputMap.dragChanged = e.inputMap.dragPreview != e.inputMap.dragOrig
	case regionDragResize:
		w := max(float32(1), roundDisplay(e.inputMap.dragOrig.Width+dx))
		h := max(float32(1), roundDisplay(e.inputMap.dragOrig.Height+dy))
		e.inputMap.dragPreview = project.Rect{
			X:      e.inputMap.dragOrig.X,
			Y:      e.inputMap.dragOrig.Y,
			Width:  w,
			Height: h,
		}
		e.inputMap.dragChanged = e.inputMap.dragPreview != e.inputMap.dragOrig
	case regionDragCreate:
		x0, y0 := e.inputMap.dragStartX, e.inputMap.dragStartY
		minX, maxX := min(x0, x), max(x0, x)
		minY, maxY := min(y0, y), max(y0, y)
		w := roundDisplay(maxX - minX)
		h := roundDisplay(maxY - minY)
		if w >= 2 && h >= 2 {
			e.inputMap.dragPreview = project.Rect{
				X:      roundDisplay(minX),
				Y:      roundDisplay(minY),
				Width:  w,
				Height: h,
			}
			e.inputMap.dragChanged = true
		}
	}
	e.notify()
}

// EndRegionDrag commits the dragged, resized, or newly drawn region as a single undoable EditProject step.
func (e *Editor) EndRegionDrag() error {
	if e == nil || e.inputMap.dragMode == regionDragNone {
		return nil
	}
	mode := e.inputMap.dragMode
	action := e.inputMap.dragAction
	bindIdx := e.inputMap.dragBinding
	preview := e.inputMap.dragPreview
	changed := e.inputMap.dragChanged
	e.inputMap.dragMode = regionDragNone
	e.inputMap.dragAction = ""
	e.inputMap.dragBinding = -1
	e.inputMap.dragChanged = false

	if !changed {
		e.notify()
		return nil
	}
	switch mode {
	case regionDragMove, regionDragResize:
		return e.SetInputRegion(action, bindIdx, preview)
	case regionDragCreate:
		r := preview
		b := project.Binding{Kind: e.InputMapRegionDrawKind(), Region: &r}
		if b.Kind == project.KindMouseRegion {
			b.Button = "left"
		}
		_, err := e.AddInputBinding(action, b)
		return err
	}
	e.notify()
	return nil
}

// DrawInputRegionCanvas draws the virtual display frame and all touch/mouse input regions scaled to size.
func (e *Editor) DrawInputRegionCanvas(d *core.Drawing, size core.Size) {
	if e == nil || d == nil || size.Width <= 0 || size.Height <= 0 {
		return
	}
	cw := float32(size.Width)
	ch := float32(size.Height)
	dw, dh := e.InputMapDisplaySize()
	sx := cw / dw
	sy := ch / dh

	// Background and display bounds border.
	d.SetFillColor(core.RGB(0x0F172A)).FillRect(0, 0, cw, ch)
	d.SetStrokeColor(core.RGBA(0x94A3B8, 0.25)).SetLineWidth(1)
	d.BeginPath()
	d.MoveTo(cw/2, 0).LineTo(cw/2, ch)
	d.MoveTo(0, ch/2).LineTo(cw, ch/2)
	d.Stroke()

	selAct := e.SelectedInputAction()
	selBind := e.SelectedInputBinding()

	if e.Project != nil && e.Project.Input != nil {
		for _, act := range e.Project.Input.Actions {
			for bi, b := range act.Bindings {
				if b.Region == nil {
					continue
				}
				r := *b.Region
				if e.inputMap.dragMode != regionDragNone && e.inputMap.dragMode != regionDragCreate &&
					e.inputMap.dragAction == act.Name && e.inputMap.dragBinding == bi {
					r = e.inputMap.dragPreview
				}
				selected := act.Name == selAct && bi == selBind
				drawInputRegionBox(d, r, sx, sy, b.Kind, selected)
			}
		}
	}

	if e.inputMap.dragMode == regionDragCreate && e.inputMap.dragChanged {
		drawInputRegionBox(d, e.inputMap.dragPreview, sx, sy, e.InputMapRegionDrawKind(), true)
	}

	d.SetStrokeColor(core.RGBA(0xCBD5E1, 0.6)).SetLineWidth(1).StrokeRect(0.5, 0.5, cw-1, ch-1)
}

func drawInputRegionBox(d *core.Drawing, r project.Rect, sx, sy float32, kind string, selected bool) {
	rx := r.X * sx
	ry := r.Y * sy
	rw := r.Width * sx
	rh := r.Height * sy
	baseHex := uint32(0x38BDF8) // touchRegion: sky blue
	if kind == project.KindMouseRegion {
		baseHex = 0xF59E0B // mouseRegion: amber
	}
	fillAlpha := float32(0.18)
	strokeWidth := float32(1)
	if selected {
		fillAlpha = 0.34
		strokeWidth = 2
	}
	d.SetFillColor(core.RGBA(baseHex, fillAlpha)).FillRect(rx, ry, rw, rh)
	d.SetStrokeColor(core.RGBA(baseHex, 0.95)).SetLineWidth(strokeWidth).StrokeRect(rx, ry, rw, rh)
	if selected {
		const hs float32 = 6
		d.SetFillColor(core.RGB(baseHex)).FillRect(rx+rw-hs, ry+rh-hs, hs, hs)
	}
}

func regionCanvasSize(dw, dh, maxW, maxH float32) (float32, float32) {
	if dw <= 0 || dh <= 0 {
		return maxW, maxH
	}
	scale := min(maxW/dw, maxH/dh)
	cw := max(float32(48), float32(math.Round(float64(dw*scale))))
	ch := max(float32(48), float32(math.Round(float64(dh*scale))))
	return cw, ch
}

func (e *Editor) findInputActionIndex(name string) int {
	if e == nil || e.Project == nil || e.Project.Input == nil || name == "" {
		return -1
	}
	for i, a := range e.Project.Input.Actions {
		if a.Name == name {
			return i
		}
	}
	return -1
}

func (e *Editor) nextDefaultActionName() string {
	for i := 1; ; i++ {
		name := fmt.Sprintf("action_%d", i)
		if e.findInputActionIndex(name) < 0 {
			return name
		}
	}
}

func (e *Editor) nextDefaultAxisName(prefix string, used []string) string {
	for i := 1; ; i++ {
		name := fmt.Sprintf("%s_%d", prefix, i)
		if !slices.Contains(used, name) {
			return name
		}
	}
}

func pruneEmptyInput(f *project.File) {
	if f.Input != nil &&
		len(f.Input.Actions) == 0 &&
		len(f.Input.Axes) == 0 &&
		len(f.Input.MotionAxes) == 0 &&
		len(f.Input.GamepadAxes) == 0 {
		f.Input = nil
	}
}

func cloneBinding(b project.Binding) project.Binding {
	out := b
	if b.Region != nil {
		r := *b.Region
		out.Region = &r
	}
	return out
}

func pointInRect(x, y float32, r project.Rect) bool {
	return x >= r.X && x <= r.X+r.Width && y >= r.Y && y <= r.Y+r.Height
}

func nearCorner(x, y, cx, cy, tol float32) bool {
	return math.Abs(float64(x-cx)) <= float64(tol) && math.Abs(float64(y-cy)) <= float64(tol)
}

func roundDisplay(v float32) float32 {
	return float32(math.Round(float64(v)))
}

func bindingKindLabel(kind string) string {
	switch kind {
	case project.KindKey:
		return "Key"
	case project.KindKeyCode:
		return "Key Code"
	case project.KindGamepadButton:
		return "Gamepad Button"
	case project.KindMouseButton:
		return "Mouse Button"
	case project.KindTouchRegion:
		return "Touch Region"
	case project.KindMouseRegion:
		return "Mouse Region"
	default:
		return kind
	}
}

// InputMapPanelView renders the Input Map panel below the viewport while a project is open and InputMapOpen reports true. It edits game.yaml's input section (actions, key/keyCode/gamepad/mouse/touch bindings, regions over display size, axes, motion axes and gamepad stick axes) through EditProject.
func InputMapPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.Project != nil && ed.InputMapOpen()
		},
		Then: func(cx *core.Scope) core.Node {
			return inputMapPanel(cx, ed, rev)
		},
	}
}

func formatInputActionTriggers(act project.Action) string {
	if len(act.Bindings) == 0 {
		return "No bindings"
	}
	var keys, pads, touches []string
	for _, b := range act.Bindings {
		switch b.Kind {
		case project.KindKey:
			if b.Key != "" {
				keys = append(keys, b.Key)
			}
		case project.KindKeyCode:
			if b.Code != "" {
				keys = append(keys, b.Code)
			}
		case project.KindGamepadButton:
			if b.Button != "" {
				pads = append(pads, b.Button)
			}
		case project.KindMouseButton:
			if b.Button != "" {
				keys = append(keys, "mouse "+b.Button)
			}
		case project.KindTouchRegion:
			touches = append(touches, "touch region")
		case project.KindMouseRegion:
			touches = append(touches, "mouse region")
		}
	}
	var groups []string
	if len(keys) > 0 {
		groups = append(groups, strings.Join(keys, " "))
	}
	if len(pads) > 0 {
		groups = append(groups, strings.Join(pads, " "))
	}
	if len(touches) > 0 {
		groups = append(groups, strings.Join(touches, " "))
	}
	if len(groups) == 0 {
		return fmt.Sprintf("%d binding(s)", len(act.Bindings))
	}
	return strings.Join(groups, " · ")
}

func inputMapPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	newActionName := core.NewSignal(cx, "")
	addBindingKind := core.NewSignal(cx, project.KindKey)
	renameText := core.NewSignal(cx, ed.SelectedInputAction())

	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.Project == nil
	}

	selectedAction := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.SelectedInputAction()
	})
	cx.Effect(func() {
		renameText.Set(selectedAction.Get())
	})

	actionListKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Project == nil || ed.Project.Input == nil {
			return ""
		}
		var b strings.Builder
		for i, a := range ed.Project.Input.Actions {
			fmt.Fprintf(&b, "%d:%s:%d:%s|", i, a.Name, len(a.Bindings), formatInputActionTriggers(a))
		}
		return b.String()
	})

	bindingListKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		name := ed.SelectedInputAction()
		idx := ed.findInputActionIndex(name)
		if idx < 0 {
			return ""
		}
		var b strings.Builder
		b.WriteString(name)
		b.WriteByte('|')
		for i, bind := range ed.Project.Input.Actions[idx].Bindings {
			fmt.Fprintf(&b, "%d:%s|", i, bind.Kind)
		}
		return b.String()
	})

	header := core.View{
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Gap:         8,
			Padding:     core.Edges{Bottom: 12},
			Borders:     core.Edges{Bottom: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			ui.ToggleButton{
				TestID: "input-tab-actions",
				Text:   "Actions",
				On: func() bool {
					_ = rev.Get()
					return ed.InputMapTab() == InputMapTabActions
				},
				OnChange: func(on bool) {
					if on {
						ed.SetInputMapTab(InputMapTabActions)
					}
				},
			},
			ui.ToggleButton{
				TestID: "input-tab-axes",
				Text:   "Axes",
				On: func() bool {
					_ = rev.Get()
					return ed.InputMapTab() == InputMapTabAxes
				},
				OnChange: func(on bool) {
					if on {
						ed.SetInputMapTab(InputMapTabAxes)
					}
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			settingsSpecButton("input-map-close", "Close", "Close Input map", false, nil, func() {
				ed.SetInputMapOpen(false)
			}),
		},
	}

	// Overview summary rows matching Settings.dc.html (action, Keyboard · gamepad · touch, trigger value button, policy).
	actionSummary := core.Dynamic(func(cx *core.Scope) core.Node {
		if actionListKey.Get() == "" {
			return nil
		}
		actions := ed.Project.Input.Actions
		rows := make([]core.Node, 0, len(actions))
		for i, act := range actions {
			actName := act.Name
			idx := i
			summary := formatInputActionTriggers(act)
			policy := policyAgentMayChange
			if strings.EqualFold(actName, "pause") {
				policy = policyYouOnly
			}
			rows = append(rows, settingsRow(
				fmt.Sprintf("settings-input-row-%d", idx),
				actName,
				"Keyboard · gamepad · touch",
				ui.Button{
					TestID:  fmt.Sprintf("settings-input-row-%d-edit", idx),
					Text:    summary,
					Label:   "Edit triggers for " + actName,
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color: func() core.Color {
						if selectedAction.Get() == actName {
							return specSettingsActiveNav
						}
						return specRaised
					},
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      specText,
					},
					Style: core.Style{
						Height:       core.Pt(28),
						CornerRadius: 8,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
						FocusVisible: specFocusRingStyle,
					},
					OnPress: func() {
						ed.SetInputMapTab(InputMapTabActions)
						ed.SelectInputAction(actName)
					},
				},
				policy,
				nil,
			))
		}
		return core.View{
			TestID: "settings-input-summary",
			Style: core.Style{
				MaxHeight: core.Pt(184),
				Overflow:  core.Hidden,
			},
			Children: []core.Node{
				ui.ScrollView{
					Style:    core.Style{FlexGrow: 1},
					Children: rows,
				},
			},
		}
	})

	return core.View{
		TestID: "input-map-panel",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Gap:       12,
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			header,
			actionSummary,
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.InputMapTab() == InputMapTabActions
				},
				Then: func(cx *core.Scope) core.Node {
					return inputActionsTab(cx, ed, rev, newActionName, addBindingKind, renameText, selectedAction, actionListKey, bindingListKey, disabled)
				},
				Else: func(cx *core.Scope) core.Node {
					return inputAxesTab(cx, ed, rev, disabled)
				},
			},
		},
	}
}

func inputActionsTab(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	newActionName *core.Signal[string],
	addBindingKind *core.Signal[string],
	renameText *core.Signal[string],
	selectedAction *core.Memo[string],
	actionListKey *core.Memo[string],
	bindingListKey *core.Memo[string],
	disabled func() bool,
) core.Node {
	label := func(text string) core.Node {
		return core.Text{Content: text, Style: core.Style{FontSize: 11, FontWeight: 600, LetterSpacing: 0.66, Color: specTextMuted}}
	}

	kindOpts := make([]ui.Option[string], len(project.BindingKinds))
	for i, k := range project.BindingKinds {
		kindOpts[i] = ui.Option[string]{Value: k, Label: bindingKindLabel(k)}
	}

	actionsColumn := core.View{
		TestID: "input-actions-column",
		Style: core.Style{
			Width:       core.Pt(244),
			FlexShrink:  0,
			Gap:         8,
			Padding:     core.Edges{Right: 12},
			Borders:     core.Edges{Right: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0)},
						Children: []core.Node{
							ui.Input{
								TestID:      "input-new-action-name",
								Label:       "New Action Name",
								Placeholder: "Action name",
								Disabled:    disabled,
								Value:       newActionName.Get,
								OnChange:    newActionName.Set,
								Style:       core.Style{Width: core.Pct(100), MinWidth: core.Pt(0)},
							},
						},
					},
					settingsSpecButton("input-add-action", "Add", "Add Action", false, disabled, func() {
						name := strings.TrimSpace(newActionName.Get())
						if name == "" {
							name = ed.nextDefaultActionName()
						}
						if err := ed.AddInputAction(name); err != nil {
							ed.report(err)
							return
						}
						newActionName.Set("")
					}),
				},
			},
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1},
				ContentStyle: core.Style{Gap: 4},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						if actionListKey.Get() == "" {
							return core.Text{
								TestID:  "input-empty-actions",
								Content: "No actions yet. Add an action to bind keys, gamepad or regions.",
								Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
							}
						}
						actions := ed.Project.Input.Actions
						rows := make([]core.Node, 0, len(actions))
						for i, act := range actions {
							actName := act.Name
							bindCount := len(act.Bindings)
							idx := i
							rows = append(rows, core.View{
								TestID: fmt.Sprintf("input-action-%d", idx),
								StyleFunc: func() core.Style {
									bg := specPanel
									border := specLine
									if selectedAction.Get() == actName {
										bg = specRaised
										border = specSettingsAccentEdge
									}
									return core.Style{
										Direction:    core.Row,
										AlignItems:   core.AlignCenter,
										Gap:          4,
										Padding:      core.Symmetric(4, 8),
										CornerRadius: 8,
										Background:   bg,
										BorderWidth:  1,
										BorderColor:  border,
									}
								},
								Children: []core.Node{
									ui.Button{
										TestID:  fmt.Sprintf("input-action-%d-select", idx),
										Text:    fmt.Sprintf("%s (%d)", actName, bindCount),
										Label:   "Select Action " + actName,
										Variant: ui.ButtonGhost,
										Size:    ui.SizeSmall,
										Flex:    1,
										Color: func() core.Color {
											if selectedAction.Get() == actName {
												return specSettingsActiveNav
											}
											return specPanel
										},
										TextStyle: core.Style{
											FontSize:   12,
											FontWeight: 600,
											Color:      specTextStrong,
											FlexGrow:   1,
											TextAlign:  core.TextAlignLeft,
										},
										Style: core.Style{
											Height:       core.Pt(28),
											CornerRadius: 6,
											FocusVisible: specFocusRingStyle,
										},
										OnPress: func() {
											ed.SelectInputAction(actName)
										},
									},
									settingsSpecButton(fmt.Sprintf("input-action-%d-delete", idx), "×", "Delete Action "+actName, false, disabled, func() {
										ed.report(ed.DeleteInputAction(actName))
									}),
								},
							})
						}
						return core.View{Style: core.Style{Gap: 4}, Children: rows}
					}),
				},
			},
		},
	}

	bindingsColumn := core.View{
		TestID: "input-bindings-column",
		Style: core.Style{
			FlexGrow:    1,
			FlexBasis:   core.Pt(0),
			Gap:         8,
			Padding:     core.Symmetric(0, 12),
			Borders:     core.Edges{Right: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			core.Show{
				When: func() bool { return selectedAction.Get() != "" },
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{FlexGrow: 1, Gap: 8},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Direction:  core.Row,
									Wrap:       core.Wrap,
									AlignItems: core.AlignCenter,
									Gap:        8,
								},
								Children: []core.Node{
									label("ACTION"),
									core.View{
										Style: core.Style{Width: core.Pt(120)},
										Children: []core.Node{
											ui.Input{
												TestID:   "input-action-rename-input",
												Label:    "Rename Action",
												Disabled: disabled,
												Value:    renameText.Get,
												OnChange: renameText.Set,
											},
										},
									},
									settingsSpecButton("input-action-rename", "Rename", "Rename selected action", false, func() bool {
										if disabled() {
											return true
										}
										cur := selectedAction.Get()
										next := strings.TrimSpace(renameText.Get())
										return cur == "" || next == "" || next == cur
									}, func() {
										ed.report(ed.RenameInputAction(selectedAction.Get(), strings.TrimSpace(renameText.Get())))
									}),
									settingsSpecButton("input-capture-key", "Press a key", "Capture key binding", true, disabled, func() {
										ed.report(ed.BeginKeyCapture(selectedAction.Get(), project.KindKey))
									}),
									settingsSpecButton("input-capture-keycode", "Press code", "Capture physical keyCode binding", false, disabled, func() {
										ed.report(ed.BeginKeyCapture(selectedAction.Get(), project.KindKeyCode))
									}),
									core.View{
										Style: core.Style{Width: core.Pt(148), FlexShrink: 0},
										Children: []core.Node{
											ui.Select[string]{
												TestID:   "input-add-binding-kind",
												Label:    "Binding Kind",
												Disabled: disabled,
												Options:  kindOpts,
												Value:    addBindingKind.Get,
												OnChange: addBindingKind.Set,
												Style:    core.Style{Width: core.Pt(148)},
											},
										},
									},
									settingsSpecButton("input-add-binding", "Add Binding", "Add input binding", false, disabled, func() {
										b, err := ed.DefaultInputBinding(addBindingKind.Get())
										if err != nil {
											ed.report(err)
											return
										}
										_, err = ed.AddInputBinding(selectedAction.Get(), b)
										ed.report(err)
									}),
								},
							},
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return ed.CapturingKey()
								},
								Then: func(cx *core.Scope) core.Node {
									return keyCaptureBanner(cx, ed, rev)
								},
							},
							ui.ScrollView{
								Style:        core.Style{FlexGrow: 1},
								ContentStyle: core.Style{Gap: 4},
								Children: []core.Node{
									core.Dynamic(func(cx *core.Scope) core.Node {
										if bindingListKey.Get() == "" {
											return nil
										}
										actName := selectedAction.Get()
										ai := ed.findInputActionIndex(actName)
										if ai < 0 {
											return nil
										}
										binds := ed.Project.Input.Actions[ai].Bindings
										if len(binds) == 0 {
											return core.Text{
												TestID:  "input-empty-bindings",
												Content: "No bindings yet. Press \"Press a key\" or add a binding.",
												Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
											}
										}
										rows := make([]core.Node, 0, len(binds))
										for bi, bind := range binds {
											rows = append(rows, inputBindingRow(cx, ed, rev, actName, bi, bind.Kind, disabled))
										}
										return core.View{Style: core.Style{Gap: 4}, Children: rows}
									}),
								},
							},
						},
					}
				},
				Else: func(cx *core.Scope) core.Node {
					return core.Text{
						Content: "Select or add an action to edit its bindings.",
						Style:   core.Style{FontSize: 12, Color: specTextMuted},
					}
				},
			},
		},
	}

	regionColumn := inputRegionCanvasColumn(cx, ed, rev, disabled)

	return core.View{
		Style: core.Style{
			Direction: core.Row,
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Overflow:  core.Hidden,
		},
		Children: []core.Node{actionsColumn, bindingsColumn, regionColumn},
	}
}

func keyCaptureBanner(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	captureRef := &core.Ref{}

	// Record keys already held when capture begins so a key held across clicking the button is not captured immediately.
	initialKeys := core.Untracked(cx, func() []string { return core.KeysDown(cx) })
	initialCodes := core.Untracked(cx, func() []string { return core.KeyCodesDown(cx) })

	cx.Effect(func() {
		captureRef.Focus()
	})

	// Fallback window-level watcher: catches keys even if focus left the capture box (or in tests calling app.KeyDown directly).
	cx.Effect(func() {
		keys := core.KeysDown(cx)
		codes := core.KeyCodesDown(cx)
		cx.Untrack(func() {
			if !ed.CapturingKey() {
				return
			}
			switch ed.CaptureKeyKind() {
			case project.KindKey:
				for _, k := range keys {
					if !slices.Contains(initialKeys, k) {
						_, err := ed.CaptureKeyEvent(core.KeyEvent{Key: k})
						ed.report(err)
						return
					}
				}
			case project.KindKeyCode:
				for _, c := range codes {
					if !slices.Contains(initialCodes, c) {
						_, err := ed.CaptureKeyEvent(core.KeyEvent{Code: c})
						ed.report(err)
						return
					}
				}
			}
		})
	})

	return core.View{
		TestID:    "input-key-capture-target",
		Label:     "Key Capture Target",
		Ref:       captureRef,
		Focusable: func() bool { return true },
		OnKeyDown: func(ev core.KeyEvent) bool {
			captured, err := ed.CaptureKeyEvent(ev)
			ed.report(err)
			return captured
		},
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          8,
			Padding:      core.Symmetric(8, 12),
			CornerRadius: 8,
			Background:   specSettingsAccentTint,
			BorderWidth:  1,
			BorderColor:  specAccent,
			FocusVisible: specFocusRingStyle,
		},
		Children: []core.Node{
			core.Text{
				TestID: "input-key-capture-prompt",
				ContentFunc: func() string {
					_ = rev.Get()
					return fmt.Sprintf("Press a key to bind %s to %q...", ed.CaptureKeyKind(), ed.CaptureKeyAction())
				},
				Style: core.Style{FontSize: 12, FontWeight: 600, Color: specTextStrong, FlexGrow: 1},
			},
			settingsSpecButton("input-key-capture-cancel", "Cancel", "Cancel key capture", false, nil, func() {
				ed.CancelKeyCapture()
			}),
		},
	}
}

func inputBindingRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], actName string, bi int, kind string, disabled func() bool) core.Node {
	readBinding := func() (project.Binding, bool) {
		ai := ed.findInputActionIndex(actName)
		if ai < 0 || bi < 0 || bi >= len(ed.Project.Input.Actions[ai].Bindings) {
			return project.Binding{}, false
		}
		return ed.Project.Input.Actions[ai].Bindings[bi], true
	}

	var editorControls []core.Node
	switch kind {
	case project.KindKey:
		draft := core.NewSignal(cx, func() string {
			b, _ := readBinding()
			return b.Key
		}())
		cx.Effect(func() {
			_ = rev.Get()
			if b, ok := readBinding(); ok {
				draft.Set(b.Key)
			}
		})
		commit := func() {
			next := strings.ToLower(strings.TrimSpace(draft.Get()))
			if b, ok := readBinding(); ok && next != "" && next != b.Key {
				b.Key = next
				ed.report(ed.SetInputBinding(actName, bi, b))
			}
		}
		editorControls = append(editorControls, core.View{
			Style: core.Style{Width: core.Pt(110)},
			Children: []core.Node{
				ui.Input{
					TestID:   fmt.Sprintf("input-binding-%d-key", bi),
					Label:    fmt.Sprintf("Binding %d Key", bi),
					Disabled: disabled,
					Value:    draft.Get,
					OnChange: draft.Set,
					OnSubmit: func(string) { commit() },
					OnBlur:   commit,
				},
			},
		})
	case project.KindKeyCode:
		draft := core.NewSignal(cx, func() string {
			b, _ := readBinding()
			return b.Code
		}())
		cx.Effect(func() {
			_ = rev.Get()
			if b, ok := readBinding(); ok {
				draft.Set(b.Code)
			}
		})
		commit := func() {
			next := strings.TrimSpace(draft.Get())
			if b, ok := readBinding(); ok && next != "" && next != b.Code {
				b.Code = next
				ed.report(ed.SetInputBinding(actName, bi, b))
			}
		}
		editorControls = append(editorControls, core.View{
			Style: core.Style{Width: core.Pt(120)},
			Children: []core.Node{
				ui.Input{
					TestID:   fmt.Sprintf("input-binding-%d-code", bi),
					Label:    fmt.Sprintf("Binding %d KeyCode", bi),
					Disabled: disabled,
					Value:    draft.Get,
					OnChange: draft.Set,
					OnSubmit: func(string) { commit() },
					OnBlur:   commit,
				},
			},
		})
	case project.KindGamepadButton:
		opts := make([]ui.Option[string], len(project.GamepadButtons))
		for i, btn := range project.GamepadButtons {
			opts[i] = ui.Option[string]{Value: btn, Label: btn}
		}
		editorControls = append(editorControls, core.View{
			Style: core.Style{Width: core.Pt(140)},
			Children: []core.Node{
				ui.Select[string]{
					TestID:   fmt.Sprintf("input-binding-%d-gamepad-button", bi),
					Label:    fmt.Sprintf("Binding %d Gamepad Button", bi),
					Disabled: disabled,
					Options:  opts,
					Value: func() string {
						_ = rev.Get()
						b, _ := readBinding()
						return b.Button
					},
					OnChange: func(v string) {
						if b, ok := readBinding(); ok && v != b.Button {
							b.Button = v
							ed.report(ed.SetInputBinding(actName, bi, b))
						}
					},
				},
			},
		})
	case project.KindMouseButton:
		editorControls = append(editorControls, mouseButtonSelect(ed, rev, actName, bi, readBinding, disabled))
	case project.KindMouseRegion:
		editorControls = append(editorControls, mouseButtonSelect(ed, rev, actName, bi, readBinding, disabled))
		editorControls = append(editorControls, regionFields(ed, rev, actName, bi, readBinding, disabled)...)
	case project.KindTouchRegion:
		editorControls = append(editorControls, regionFields(ed, rev, actName, bi, readBinding, disabled)...)
	}

	rowChildren := make([]core.Node, 0, len(editorControls)+3)
	rowChildren = append(rowChildren,
		ui.Button{
			TestID:  fmt.Sprintf("input-binding-%d-select", bi),
			Text:    bindingKindLabel(kind),
			Label:   fmt.Sprintf("Select Binding %d", bi),
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Color: func() core.Color {
				_ = rev.Get()
				if ed.SelectedInputBinding() == bi {
					return specSettingsActiveNav
				}
				return specPanel
			},
			TextStyle: core.Style{
				FontSize:   11.5,
				FontWeight: 600,
				Color:      specTextSecondary,
				FlexGrow:   1,
				TextAlign:  core.TextAlignLeft,
			},
			Style: core.Style{
				Width:        core.Pt(112),
				Height:       core.Pt(28),
				CornerRadius: 6,
				FocusVisible: specFocusRingStyle,
			},
			OnPress: func() {
				ed.SelectInputBinding(bi)
			},
		},
	)
	rowChildren = append(rowChildren, editorControls...)
	rowChildren = append(rowChildren,
		core.View{Style: core.Style{FlexGrow: 1}},
		settingsSpecButton(fmt.Sprintf("input-binding-%d-remove", bi), "Remove", fmt.Sprintf("Remove Binding %d", bi), false, disabled, func() {
			ed.report(ed.RemoveInputBinding(actName, bi))
		}),
	)

	return core.View{
		TestID: fmt.Sprintf("input-binding-%d", bi),
		StyleFunc: func() core.Style {
			_ = rev.Get()
			bg := specPanel
			border := specLine
			if ed.SelectedInputBinding() == bi {
				bg = specRaised
				border = specSettingsAccentEdge
			}
			return core.Style{
				Direction:    core.Row,
				Wrap:         core.Wrap,
				AlignItems:   core.AlignCenter,
				Gap:          8,
				Padding:      core.Symmetric(4, 8),
				CornerRadius: 8,
				Background:   bg,
				BorderWidth:  1,
				BorderColor:  border,
			}
		},
		Children: rowChildren,
	}
}

func mouseButtonSelect(ed *Editor, rev *core.Signal[uint64], actName string, bi int, readBinding func() (project.Binding, bool), disabled func() bool) core.Node {
	opts := make([]ui.Option[string], len(project.MouseButtons))
	for i, btn := range project.MouseButtons {
		opts[i] = ui.Option[string]{Value: btn, Label: btn}
	}
	return core.View{
		Style: core.Style{Width: core.Pt(96)},
		Children: []core.Node{
			ui.Select[string]{
				TestID:   fmt.Sprintf("input-binding-%d-mouse-button", bi),
				Label:    fmt.Sprintf("Binding %d Mouse Button", bi),
				Disabled: disabled,
				Options:  opts,
				Value: func() string {
					_ = rev.Get()
					b, _ := readBinding()
					return b.Button
				},
				OnChange: func(v string) {
					if b, ok := readBinding(); ok && v != b.Button {
						b.Button = v
						ed.report(ed.SetInputBinding(actName, bi, b))
					}
				},
			},
		},
	}
}

func regionFields(ed *Editor, rev *core.Signal[uint64], actName string, bi int, readBinding func() (project.Binding, bool), disabled func() bool) []core.Node {
	numField := func(suffix, label string, minVal float64, get func(project.Rect) float32, set func(*project.Rect, float32)) core.Node {
		return core.View{
			Style: core.Style{Width: core.Pt(56)},
			Children: []core.Node{
				ui.NumberInput{
					TestID:   fmt.Sprintf("input-binding-%d-%s", bi, suffix),
					Label:    fmt.Sprintf("Binding %d %s", bi, label),
					Min:      minVal,
					Max:      100000,
					Step:     1,
					Disabled: disabled,
					Value: func() float64 {
						_ = rev.Get()
						b, ok := readBinding()
						if !ok || b.Region == nil {
							return 0
						}
						return float64(get(*b.Region))
					},
					OnChange: func(v float64) {
						b, ok := readBinding()
						if !ok || b.Region == nil || !isFinite64(v) {
							return
						}
						r := *b.Region
						set(&r, float32(v))
						if r != *b.Region {
							ed.report(ed.SetInputRegion(actName, bi, r))
						}
					},
				},
			},
		}
	}
	return []core.Node{
		numField("x", "X", -100000, func(r project.Rect) float32 { return r.X }, func(r *project.Rect, v float32) { r.X = v }),
		numField("y", "Y", -100000, func(r project.Rect) float32 { return r.Y }, func(r *project.Rect, v float32) { r.Y = v }),
		numField("w", "Width", 1, func(r project.Rect) float32 { return r.Width }, func(r *project.Rect, v float32) { r.Width = max(float32(1), v) }),
		numField("h", "Height", 1, func(r project.Rect) float32 { return r.Height }, func(r *project.Rect, v float32) { r.Height = max(float32(1), v) }),
	}
}

func inputRegionCanvasColumn(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	const maxCanvasW float32 = 240
	const maxCanvasH float32 = 156

	var pressPos core.Point
	var hasPress bool
	var panActive bool
	var mouseDragging bool

	toDisplay := func(px, py float32) (float32, float32) {
		dw, dh := ed.InputMapDisplaySize()
		cw, ch := regionCanvasSize(dw, dh, maxCanvasW, maxCanvasH)
		return px * dw / cw, py * dh / ch
	}

	return core.View{
		TestID: "input-region-column",
		Style: core.Style{
			Width:   core.Pt(264),
			Gap:     8,
			Padding: core.Edges{Left: 12},
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.Text{
						TestID: "input-region-size",
						ContentFunc: func() string {
							_ = rev.Get()
							dw, dh := ed.InputMapDisplaySize()
							return fmt.Sprintf("Regions (%g×%g)", dw, dh)
						},
						Style: core.Style{FontSize: 11, FontWeight: 600, LetterSpacing: 0.66, Color: specTextMuted, FlexGrow: 1},
					},
					ui.ToggleButton{
						TestID:   "input-region-draw-mouse",
						Text:     "Mouse",
						Label:    "Draw mouse regions instead of touch regions",
						Disabled: disabled,
						On: func() bool {
							_ = rev.Get()
							return ed.InputMapRegionDrawKind() == project.KindMouseRegion
						},
						OnChange: func(on bool) {
							if on {
								ed.SetInputMapRegionDrawKind(project.KindMouseRegion)
							} else {
								ed.SetInputMapRegionDrawKind(project.KindTouchRegion)
							}
						},
					},
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				dw, dh := ed.InputMapDisplaySize()
				cw, ch := regionCanvasSize(dw, dh, maxCanvasW, maxCanvasH)
				return core.View{
					TestID: "input-region-canvas",
					Style: core.Style{
						Width:        core.Pt(core.Points(cw)),
						Height:       core.Pt(core.Points(ch)),
						CornerRadius: 8,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
						Background:   specPanel,
					},
					OnTouch: func(ev core.TouchEvent) {
						if disabled() {
							return
						}
						switch ev.Phase {
						case core.TouchStart:
							for _, t := range ev.Touches {
								if t.Changed {
									pressPos = t.Position
									hasPress = true
									return
								}
							}
						case core.TouchEnd, core.TouchCancel:
							hasPress = false
						}
					},
					OnPan: func(ev core.PanEvent) {
						if disabled() {
							return
						}
						switch ev.Phase {
						case core.GestureBegan:
							if hasPress {
								dx, dy := toDisplay(float32(pressPos.X), float32(pressPos.Y))
								ed.BeginRegionDrag(dx, dy)
								panActive = true
							}
						case core.GestureChanged:
							if panActive {
								dx, dy := toDisplay(
									float32(pressPos.X)+float32(ev.Translation.X),
									float32(pressPos.Y)+float32(ev.Translation.Y),
								)
								ed.UpdateRegionDrag(dx, dy)
							}
						case core.GestureEnded, core.GestureCancelled:
							if panActive {
								ed.report(ed.EndRegionDrag())
								panActive = false
							}
						}
					},
					OnPressDown: func(ev core.PressEvent) {
						if disabled() || ev.Keyboard || panActive {
							return
						}
						dx, dy := toDisplay(float32(ev.Position.X), float32(ev.Position.Y))
						ed.BeginRegionDrag(dx, dy)
						mouseDragging = true
					},
					OnMouseMove: func(ev core.MouseEvent) {
						if !mouseDragging || panActive {
							return
						}
						dx, dy := toDisplay(float32(ev.Position.X), float32(ev.Position.Y))
						ed.UpdateRegionDrag(dx, dy)
					},
					OnPressUp: func(ev core.PressEvent) {
						if !mouseDragging || panActive {
							return
						}
						mouseDragging = false
						ed.report(ed.EndRegionDrag())
					},
					Children: []core.Node{
						core.Canvas{
							TestID: "input-region-canvas-draw",
							Label:  "Input Region Canvas",
							Style: core.Style{
								Width:  core.Pt(core.Points(cw)),
								Height: core.Pt(core.Points(ch)),
							},
							Draw: func(d *core.Drawing, size core.Size) {
								_ = rev.Get()
								ed.DrawInputRegionCanvas(d, size)
							},
						},
					},
				}
			}),
			core.Text{
				Content: "Drag a region to move, its corner to resize, or empty space to draw.",
				Style:   core.Style{FontSize: 11, Color: specTextMuted},
			},
		},
	}
}

func inputAxesTab(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	newAxisName := core.NewSignal(cx, "")
	newAxisNeg := core.NewSignal(cx, "")
	newAxisPos := core.NewSignal(cx, "")

	newMotionName := core.NewSignal(cx, "")
	newMotionAxis := core.NewSignal(cx, project.MotionAxes[0])

	newGamepadName := core.NewSignal(cx, "")
	newGamepadAxis := core.NewSignal(cx, project.GamepadAxes[0])

	actionOptions := func() []ui.Option[string] {
		_ = rev.Get()
		opts := []ui.Option[string]{{Value: "", Label: "(none)"}}
		if ed.Project != nil && ed.Project.Input != nil {
			for _, a := range ed.Project.Input.Actions {
				opts = append(opts, ui.Option[string]{Value: a.Name, Label: a.Name})
			}
		}
		return opts
	}

	motionOpts := make([]ui.Option[string], len(project.MotionAxes))
	for i, a := range project.MotionAxes {
		motionOpts[i] = ui.Option[string]{Value: a, Label: a}
	}

	gamepadAxisOpts := make([]ui.Option[string], len(project.GamepadAxes))
	for i, a := range project.GamepadAxes {
		gamepadAxisOpts[i] = ui.Option[string]{Value: a, Label: a}
	}

	axesKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Project == nil || ed.Project.Input == nil {
			return ""
		}
		var b strings.Builder
		for i, ax := range ed.Project.Input.Axes {
			fmt.Fprintf(&b, "%d:%s|", i, ax.Name)
		}
		return b.String()
	})
	motionKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Project == nil || ed.Project.Input == nil {
			return ""
		}
		var b strings.Builder
		for i, m := range ed.Project.Input.MotionAxes {
			fmt.Fprintf(&b, "%d:%s|", i, m.Name)
		}
		return b.String()
	})
	gamepadAxesKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Project == nil || ed.Project.Input == nil {
			return ""
		}
		var b strings.Builder
		for i, g := range ed.Project.Input.GamepadAxes {
			fmt.Fprintf(&b, "%d:%s|", i, g.Name)
		}
		return b.String()
	})

	sectionHeader := func(title string) core.Node {
		return core.Text{Content: title, Style: core.Style{FontSize: 11, FontWeight: 600, LetterSpacing: 0.66, Color: specTextMuted}}
	}

	actionAxesCol := core.View{
		TestID: "input-axes-column",
		Style: core.Style{
			FlexGrow:    1,
			FlexBasis:   core.Pt(0),
			Gap:         8,
			Padding:     core.Edges{Right: 12},
			Borders:     core.Edges{Right: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			sectionHeader("ACTION AXES"),
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					core.View{
						Style: core.Style{Width: core.Pt(88)},
						Children: []core.Node{
							ui.Input{
								TestID:      "input-new-axis-name",
								Label:       "New Axis Name",
								Placeholder: "Axis",
								Disabled:    disabled,
								Value:       newAxisName.Get,
								OnChange:    newAxisName.Set,
							},
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1},
						Children: []core.Node{
							ui.Select[string]{
								TestID:      "input-new-axis-neg",
								Label:       "New Axis Negative Action",
								Placeholder: "Neg",
								Disabled:    disabled,
								OptionsFunc: actionOptions,
								Value:       newAxisNeg.Get,
								OnChange:    newAxisNeg.Set,
							},
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1},
						Children: []core.Node{
							ui.Select[string]{
								TestID:      "input-new-axis-pos",
								Label:       "New Axis Positive Action",
								Placeholder: "Pos",
								Disabled:    disabled,
								OptionsFunc: actionOptions,
								Value:       newAxisPos.Get,
								OnChange:    newAxisPos.Set,
							},
						},
					},
					settingsSpecButton("input-add-axis", "Add", "Add Axis", false, disabled, func() {
						name := strings.TrimSpace(newAxisName.Get())
						if name == "" {
							used := make([]string, 0)
							for _, ax := range ed.InputAxes() {
								used = append(used, ax.Name)
							}
							name = ed.nextDefaultAxisName("axis", used)
						}
						neg, pos := newAxisNeg.Get(), newAxisPos.Get()
						if neg == "" && pos == "" {
							if acts := ed.InputActions(); len(acts) > 0 {
								pos = acts[0].Name
							}
						}
						if err := ed.AddInputAxis(project.Axis{Name: name, Negative: neg, Positive: pos}); err != nil {
							ed.report(err)
							return
						}
						newAxisName.Set("")
					}),
				},
			},
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1},
				ContentStyle: core.Style{Gap: 4},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						if axesKey.Get() == "" {
							return core.Text{
								TestID:  "input-empty-axes",
								Content: "No action axes.",
								Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
							}
						}
						axes := ed.Project.Input.Axes
						rows := make([]core.Node, 0, len(axes))
						for i, ax := range axes {
							idx := i
							axName := ax.Name
							rows = append(rows, core.View{
								TestID: fmt.Sprintf("input-axis-%d", idx),
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          4,
									Padding:      core.Symmetric(4, 8),
									CornerRadius: 8,
									Background:   specPanel,
									BorderWidth:  1,
									BorderColor:  specLine,
								},
								Children: []core.Node{
									core.Text{
										TestID:  fmt.Sprintf("input-axis-%d-name", idx),
										Content: axName,
										Style:   core.Style{FontSize: 12, FontWeight: 600, Color: specTextStrong, Width: core.Pt(70)},
									},
									core.View{
										Style: core.Style{FlexGrow: 1},
										Children: []core.Node{
											ui.Select[string]{
												TestID:      fmt.Sprintf("input-axis-%d-neg", idx),
												Label:       fmt.Sprintf("Axis %d Negative", idx),
												Disabled:    disabled,
												OptionsFunc: actionOptions,
												Value: func() string {
													_ = rev.Get()
													if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.Axes) {
														return ""
													}
													return ed.Project.Input.Axes[idx].Negative
												},
												OnChange: func(v string) {
													if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.Axes) {
														return
													}
													next := ed.Project.Input.Axes[idx]
													next.Negative = v
													ed.report(ed.SetInputAxis(idx, next))
												},
											},
										},
									},
									core.View{
										Style: core.Style{FlexGrow: 1},
										Children: []core.Node{
											ui.Select[string]{
												TestID:      fmt.Sprintf("input-axis-%d-pos", idx),
												Label:       fmt.Sprintf("Axis %d Positive", idx),
												Disabled:    disabled,
												OptionsFunc: actionOptions,
												Value: func() string {
													_ = rev.Get()
													if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.Axes) {
														return ""
													}
													return ed.Project.Input.Axes[idx].Positive
												},
												OnChange: func(v string) {
													if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.Axes) {
														return
													}
													next := ed.Project.Input.Axes[idx]
													next.Positive = v
													ed.report(ed.SetInputAxis(idx, next))
												},
											},
										},
									},
									settingsSpecButton(fmt.Sprintf("input-axis-%d-delete", idx), "×", "Delete Axis "+axName, false, disabled, func() {
										ed.report(ed.DeleteInputAxis(idx))
									}),
								},
							})
						}
						return core.View{Style: core.Style{Gap: 4}, Children: rows}
					}),
				},
			},
		},
	}

	motionAxesCol := core.View{
		TestID: "input-motion-column",
		Style: core.Style{
			FlexGrow:    1,
			FlexBasis:   core.Pt(0),
			Gap:         8,
			Padding:     core.Symmetric(0, 12),
			Borders:     core.Edges{Right: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			sectionHeader("MOTION AXES"),
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1},
						Children: []core.Node{
							ui.Input{
								TestID:      "input-new-motion-name",
								Label:       "New Motion Axis Name",
								Placeholder: "Motion",
								Disabled:    disabled,
								Value:       newMotionName.Get,
								OnChange:    newMotionName.Set,
							},
						},
					},
					core.View{
						Style: core.Style{Width: core.Pt(92)},
						Children: []core.Node{
							ui.Select[string]{
								TestID:   "input-new-motion-axis",
								Label:    "New Motion Sensor Axis",
								Disabled: disabled,
								Options:  motionOpts,
								Value:    newMotionAxis.Get,
								OnChange: newMotionAxis.Set,
							},
						},
					},
					settingsSpecButton("input-add-motion", "Add", "Add Motion Axis", false, disabled, func() {
						name := strings.TrimSpace(newMotionName.Get())
						if name == "" {
							used := make([]string, 0)
							for _, m := range ed.InputMotionAxes() {
								used = append(used, m.Name)
							}
							name = ed.nextDefaultAxisName("motion", used)
						}
						if err := ed.AddInputMotionAxis(project.MotionAxis{Name: name, Axis: newMotionAxis.Get()}); err != nil {
							ed.report(err)
							return
						}
						newMotionName.Set("")
					}),
				},
			},
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1},
				ContentStyle: core.Style{Gap: 4},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						if motionKey.Get() == "" {
							return core.Text{
								TestID:  "input-empty-motion",
								Content: "No motion axes.",
								Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
							}
						}
						list := ed.Project.Input.MotionAxes
						rows := make([]core.Node, 0, len(list))
						for i, m := range list {
							idx := i
							mName := m.Name
							rows = append(rows, core.View{
								TestID: fmt.Sprintf("input-motion-%d", idx),
								Style: core.Style{
									Gap:          4,
									Padding:      core.Symmetric(8, 8),
									CornerRadius: 8,
									Background:   specPanel,
									BorderWidth:  1,
									BorderColor:  specLine,
								},
								Children: []core.Node{
									ui.HStack{
										Gap: 4,
										Children: []core.Node{
											core.Text{
												Content: mName,
												Style:   core.Style{FontSize: 12, FontWeight: 600, Color: specTextStrong, FlexGrow: 1},
											},
											core.View{
												Style: core.Style{Width: core.Pt(86)},
												Children: []core.Node{
													ui.Select[string]{
														TestID:   fmt.Sprintf("input-motion-%d-axis", idx),
														Label:    fmt.Sprintf("Motion %d Axis", idx),
														Disabled: disabled,
														Options:  motionOpts,
														Value: func() string {
															_ = rev.Get()
															if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.MotionAxes) {
																return ""
															}
															return ed.Project.Input.MotionAxes[idx].Axis
														},
														OnChange: func(v string) {
															if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.MotionAxes) {
																return
															}
															next := ed.Project.Input.MotionAxes[idx]
															next.Axis = v
															ed.report(ed.SetInputMotionAxis(idx, next))
														},
													},
												},
											},
											ui.ToggleButton{
												TestID:   fmt.Sprintf("input-motion-%d-invert", idx),
												Text:     "Inv",
												Label:    fmt.Sprintf("Invert Motion %d", idx),
												Disabled: disabled,
												On: func() bool {
													_ = rev.Get()
													if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.MotionAxes) {
														return false
													}
													return ed.Project.Input.MotionAxes[idx].Invert
												},
												OnChange: func(on bool) {
													if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.MotionAxes) {
														return
													}
													next := ed.Project.Input.MotionAxes[idx]
													next.Invert = on
													ed.report(ed.SetInputMotionAxis(idx, next))
												},
											},
											settingsSpecButton(fmt.Sprintf("input-motion-%d-delete", idx), "×", "Delete Motion Axis "+mName, false, disabled, func() {
												ed.report(ed.DeleteInputMotionAxis(idx))
											}),
										},
									},
									ui.HStack{
										Gap: 4,
										Children: []core.Node{
											motionNumberInput(ed, rev, idx, "dz", "DeadZone", 0, 0.99, 0.05, disabled,
												func(m project.MotionAxis) float64 { return m.DeadZone },
												func(m *project.MotionAxis, v float64) { m.DeadZone = clampFiniteRange(v, 0, 0.99) }),
											motionNumberInput(ed, rev, idx, "sens", "Sensitivity", 0, 100, 0.1, disabled,
												func(m project.MotionAxis) float64 { return m.Sensitivity },
												func(m *project.MotionAxis, v float64) { m.Sensitivity = max(0, v) }),
											motionNumberInput(ed, rev, idx, "smooth", "Smoothing", 0, 0.99, 0.05, disabled,
												func(m project.MotionAxis) float64 { return m.Smoothing },
												func(m *project.MotionAxis, v float64) { m.Smoothing = clampFiniteRange(v, 0, 0.99) }),
										},
									},
								},
							})
						}
						return core.View{Style: core.Style{Gap: 4}, Children: rows}
					}),
				},
			},
		},
	}

	gamepadAxesCol := core.View{
		TestID: "input-gamepad-axes-column",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Gap:       8,
			Padding:   core.Edges{Left: 12},
		},
		Children: []core.Node{
			sectionHeader("GAMEPAD STICKS"),
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1},
						Children: []core.Node{
							ui.Input{
								TestID:      "input-new-gamepad-axis-name",
								Label:       "New Gamepad Axis Name",
								Placeholder: "Stick",
								Disabled:    disabled,
								Value:       newGamepadName.Get,
								OnChange:    newGamepadName.Set,
							},
						},
					},
					core.View{
						Style: core.Style{Width: core.Pt(92)},
						Children: []core.Node{
							ui.Select[string]{
								TestID:   "input-new-gamepad-axis",
								Label:    "New Gamepad Stick Axis",
								Disabled: disabled,
								Options:  gamepadAxisOpts,
								Value:    newGamepadAxis.Get,
								OnChange: newGamepadAxis.Set,
							},
						},
					},
					settingsSpecButton("input-add-gamepad-axis", "Add", "Add Gamepad Axis", false, disabled, func() {
						name := strings.TrimSpace(newGamepadName.Get())
						if name == "" {
							used := make([]string, 0)
							for _, g := range ed.InputGamepadAxes() {
								used = append(used, g.Name)
							}
							name = ed.nextDefaultAxisName("stick", used)
						}
						if err := ed.AddInputGamepadAxis(project.GamepadAxis{Name: name, Axis: newGamepadAxis.Get()}); err != nil {
							ed.report(err)
							return
						}
						newGamepadName.Set("")
					}),
				},
			},
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1},
				ContentStyle: core.Style{Gap: 4},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						if gamepadAxesKey.Get() == "" {
							return core.Text{
								TestID:  "input-empty-gamepad-axes",
								Content: "No gamepad stick axes.",
								Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
							}
						}
						list := ed.Project.Input.GamepadAxes
						rows := make([]core.Node, 0, len(list))
						for i, g := range list {
							idx := i
							gName := g.Name
							rows = append(rows, core.View{
								TestID: fmt.Sprintf("input-gamepad-axis-%d", idx),
								Style: core.Style{
									Gap:          4,
									Padding:      core.Symmetric(8, 8),
									CornerRadius: 8,
									Background:   specPanel,
									BorderWidth:  1,
									BorderColor:  specLine,
								},
								Children: []core.Node{
									ui.HStack{
										Gap: 4,
										Children: []core.Node{
											core.Text{
												Content: gName,
												Style:   core.Style{FontSize: 12, FontWeight: 600, Color: specTextStrong, FlexGrow: 1},
											},
											core.View{
												Style: core.Style{Width: core.Pt(86)},
												Children: []core.Node{
													ui.Select[string]{
														TestID:   fmt.Sprintf("input-gamepad-axis-%d-axis", idx),
														Label:    fmt.Sprintf("Gamepad Axis %d Stick", idx),
														Disabled: disabled,
														Options:  gamepadAxisOpts,
														Value: func() string {
															_ = rev.Get()
															if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.GamepadAxes) {
																return ""
															}
															return ed.Project.Input.GamepadAxes[idx].Axis
														},
														OnChange: func(v string) {
															if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.GamepadAxes) {
																return
															}
															next := ed.Project.Input.GamepadAxes[idx]
															next.Axis = v
															ed.report(ed.SetInputGamepadAxis(idx, next))
														},
													},
												},
											},
											ui.ToggleButton{
												TestID:   fmt.Sprintf("input-gamepad-axis-%d-axial", idx),
												Text:     "Axial",
												Label:    fmt.Sprintf("Axial Gamepad Axis %d", idx),
												Disabled: disabled,
												On: func() bool {
													_ = rev.Get()
													if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.GamepadAxes) {
														return false
													}
													return ed.Project.Input.GamepadAxes[idx].Axial
												},
												OnChange: func(on bool) {
													if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.GamepadAxes) {
														return
													}
													next := ed.Project.Input.GamepadAxes[idx]
													next.Axial = on
													ed.report(ed.SetInputGamepadAxis(idx, next))
												},
											},
											ui.ToggleButton{
												TestID:   fmt.Sprintf("input-gamepad-axis-%d-invert", idx),
												Text:     "Inv",
												Label:    fmt.Sprintf("Invert Gamepad Axis %d", idx),
												Disabled: disabled,
												On: func() bool {
													_ = rev.Get()
													if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.GamepadAxes) {
														return false
													}
													return ed.Project.Input.GamepadAxes[idx].Invert
												},
												OnChange: func(on bool) {
													if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.GamepadAxes) {
														return
													}
													next := ed.Project.Input.GamepadAxes[idx]
													next.Invert = on
													ed.report(ed.SetInputGamepadAxis(idx, next))
												},
											},
											settingsSpecButton(fmt.Sprintf("input-gamepad-axis-%d-delete", idx), "×", "Delete Gamepad Axis "+gName, false, disabled, func() {
												ed.report(ed.DeleteInputGamepadAxis(idx))
											}),
										},
									},
									ui.HStack{
										Gap: 4,
										Children: []core.Node{
											gamepadAxisNumberInput(ed, rev, idx, "dz", "DeadZone", -1, 0.99, 0.05, disabled,
												func(g project.GamepadAxis) float64 { return g.DeadZone },
												func(g *project.GamepadAxis, v float64) { g.DeadZone = clampFiniteRange(v, -1, 0.99) }),
											gamepadAxisNumberInput(ed, rev, idx, "sens", "Sensitivity", 0, 100, 0.1, disabled,
												func(g project.GamepadAxis) float64 { return g.Sensitivity },
												func(g *project.GamepadAxis, v float64) { g.Sensitivity = max(0, v) }),
										},
									},
								},
							})
						}
						return core.View{Style: core.Style{Gap: 4}, Children: rows}
					}),
				},
			},
		},
	}

	return core.View{
		Style: core.Style{
			Direction: core.Row,
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Overflow:  core.Hidden,
		},
		Children: []core.Node{actionAxesCol, motionAxesCol, gamepadAxesCol},
	}
}

func motionNumberInput(
	ed *Editor,
	rev *core.Signal[uint64],
	idx int,
	suffix, label string,
	minVal, maxVal, step float64,
	disabled func() bool,
	get func(project.MotionAxis) float64,
	set func(*project.MotionAxis, float64),
) core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1},
		Children: []core.Node{
			ui.NumberInput{
				TestID:   fmt.Sprintf("input-motion-%d-%s", idx, suffix),
				Label:    fmt.Sprintf("Motion %d %s", idx, label),
				Min:      minVal,
				Max:      maxVal,
				Step:     step,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.MotionAxes) {
						return 0
					}
					return get(ed.Project.Input.MotionAxes[idx])
				},
				OnChange: func(v float64) {
					if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.MotionAxes) || !isFinite64(v) {
						return
					}
					next := ed.Project.Input.MotionAxes[idx]
					set(&next, v)
					if next != ed.Project.Input.MotionAxes[idx] {
						ed.report(ed.SetInputMotionAxis(idx, next))
					}
				},
			},
		},
	}
}

func gamepadAxisNumberInput(
	ed *Editor,
	rev *core.Signal[uint64],
	idx int,
	suffix, label string,
	minVal, maxVal, step float64,
	disabled func() bool,
	get func(project.GamepadAxis) float64,
	set func(*project.GamepadAxis, float64),
) core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1},
		Children: []core.Node{
			ui.NumberInput{
				TestID:   fmt.Sprintf("input-gamepad-axis-%d-%s", idx, suffix),
				Label:    fmt.Sprintf("Gamepad Axis %d %s", idx, label),
				Min:      minVal,
				Max:      maxVal,
				Step:     step,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.GamepadAxes) {
						return 0
					}
					return get(ed.Project.Input.GamepadAxes[idx])
				},
				OnChange: func(v float64) {
					if ed.Project == nil || ed.Project.Input == nil || idx >= len(ed.Project.Input.GamepadAxes) || !isFinite64(v) {
						return
					}
					next := ed.Project.Input.GamepadAxes[idx]
					set(&next, v)
					if next != ed.Project.Input.GamepadAxes[idx] {
						ed.report(ed.SetInputGamepadAxis(idx, next))
					}
				},
			},
		},
	}
}
