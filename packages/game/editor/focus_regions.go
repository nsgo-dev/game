package editor

import (
	"slices"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec focus ring styles matching docs/design/editor/Spec.dc.html ("Focus ring").
//
//   - Controls (buttons, icon buttons, switches): 2 px accent ring, +2 px offset, :focus-visible only.
//   - Tree rows, list rows, fields, and regions: 2 px accent ring, -2 px inset offset so scrolling parents never clip them.
//   - Composer and cards: 2 px accent ring, +2 px offset, on :focus-within.
var (
	specFocusRingStyle = &core.Style{
		OutlineWidth:  2,
		OutlineColor:  specAccent,
		OutlineOffset: 2,
	}
	specRowFocusRingStyle = &core.Style{
		OutlineWidth:  2,
		OutlineColor:  specAccent,
		OutlineOffset: -2,
	}
	specCardFocusWithinStyle = &core.Style{
		OutlineWidth:  2,
		OutlineColor:  specAccent,
		OutlineOffset: 2,
	}
)

// FocusTheme returns the editor's NSGo theme extending core.DefaultTheme so every
// ui component calling ui.FocusRing draws the Spec's 2 px accent ring (#3B7BFF) on
// :focus-visible while preserving light and dark appearance surfaces.
func FocusTheme() *core.Theme {
	return editorFocusTheme()
}

// editorFocusTheme extends NSGo's DefaultTheme so every ui component that calls
// ui.FocusRing(cx, offset) — which reads ColorsOf(cx).Info() in NSGo v0.3.0 —
// renders the Spec's 2 px accent focus ring (#3B7BFF) on :focus-visible instead
// of NSGo's cyan info role (#64D2FF / #0891B2). Light and dark palette surfaces
// and appearance switching remain intact.
//
// NSGo gap (v0.3.0): ui.FocusRing reads Palette.Colors["info"] rather than a
// dedicated focus-ring token, ui.Switch carries no FocusVisible style, ui.IconButton
// hardcodes offset -2 on ghost buttons (and omits FocusVisible on circular variants),
// ui.Input uses only a 1 px border color change without an inset outline, and
// core.Modal / ui.Dialog do not restore focus to the opener on close. Editor-level
// helpers in this file bridge each gap.
func editorFocusTheme() *core.Theme {
	t := core.DefaultTheme()
	t.Light.Focus = specAccent
	t.Dark.Focus = specAccent
	t.Light.SetColor("info", specAccent)
	t.Dark.SetColor("info", specAccent)
	t.Light.SetColor("ring", specAccent)
	t.Dark.SetColor("ring", specAccent)
	return t
}

// EditorRegion identifies a fixed-order keyboard landmark region in the editor,
// cycled with F6 (next) and Shift-F6 (previous) per docs/design/editor/Spec.dc.html.
type EditorRegion string

const (
	// RegionNone indicates no region currently holds focus or an F6 region ring.
	RegionNone EditorRegion = ""
	// RegionHeader is the top toolbar / header region.
	RegionHeader EditorRegion = "header"
	// RegionSceneTree is the left rail region (scene tree, layers, asset and file browsers, or Assets rail).
	RegionSceneTree EditorRegion = "scene-tree"
	// RegionViewport is the central 2D/3D viewport region where focus is the selection.
	RegionViewport EditorRegion = "viewport"
	// RegionInspector is the right rail region (Agent and Inspector).
	RegionInspector EditorRegion = "inspector"
	// RegionStatusBar is the bottom status bar region.
	RegionStatusBar EditorRegion = "status-bar"
	// RegionToast is the floating toast region, participating in F6 order only while a toast is shown.
	RegionToast EditorRegion = "toast"
)

// EditorRegionOrder returns the fixed Spec order for F6 / Shift-F6 region cycling.
func EditorRegionOrder() []EditorRegion {
	return []EditorRegion{
		RegionHeader,
		RegionSceneTree,
		RegionViewport,
		RegionInspector,
		RegionStatusBar,
		RegionToast,
	}
}

type regionCandidate struct {
	ref     *core.Ref
	enabled func() bool
}

type focusRegionController struct {
	cx               *core.Scope
	roots            map[EditorRegion]*core.Ref
	candidates       map[EditorRegion][]regionCandidate
	lastFocused      map[EditorRegion]*core.Ref
	activeRegionRing *core.Signal[EditorRegion]
	focusedRegion    *core.Signal[EditorRegion]
	fallbackRegion   *core.Signal[EditorRegion]
	agentPanelRef    *core.Ref

	// Dialog focus restoration stack (top = opener for innermost open dialog) and
	// active open-dialog counter driving the modal Tab/Shift-Tab focus trap.
	dialogOpeners   []*core.Ref
	pendingOpener   *core.Ref
	openDialogCount *core.Signal[int]

	// Text input focus counter for focus-stability checks (IsTyping).
	textFocusDepth int
}

type focusRegionState struct {
	ctrl *focusRegionController
}

func newFocusRegionController(cx *core.Scope) *focusRegionController {
	return &focusRegionController{
		cx:               cx,
		roots:            make(map[EditorRegion]*core.Ref, 6),
		candidates:       make(map[EditorRegion][]regionCandidate, 6),
		lastFocused:      make(map[EditorRegion]*core.Ref, 6),
		activeRegionRing: core.NewSignal(cx, RegionNone),
		focusedRegion:    core.NewSignal(cx, RegionNone),
		fallbackRegion:   core.NewSignal(cx, RegionNone),
		openDialogCount:  core.NewSignal(cx, 0),
	}
}

func (c *focusRegionController) rootRef(r EditorRegion) *core.Ref {
	if c == nil {
		return &core.Ref{}
	}
	if ref := c.roots[r]; ref != nil {
		return ref
	}
	ref := &core.Ref{}
	c.roots[r] = ref
	return ref
}

func (c *focusRegionController) registerCandidate(r EditorRegion, ref *core.Ref, enabled func() bool) *core.Ref {
	if ref == nil {
		ref = &core.Ref{}
	}
	if c == nil {
		return ref
	}
	// Do not prune unmounted candidates here: sibling controls created during the same
	// render pass have not committed yet. Stale unmounted entries are pruned after commit
	// inside pruneCandidates.
	for i := range c.candidates[r] {
		if c.candidates[r][i].ref == ref {
			c.candidates[r][i].enabled = enabled
			return ref
		}
	}
	c.candidates[r] = append(c.candidates[r], regionCandidate{ref: ref, enabled: enabled})
	return ref
}

func (c *focusRegionController) pruneCandidates(r EditorRegion) {
	if c == nil {
		return
	}
	list := c.candidates[r][:0]
	for _, cand := range c.candidates[r] {
		if refMounted(cand.ref) {
			list = append(list, cand)
		}
	}
	c.candidates[r] = list
}

func refMounted(ref *core.Ref) bool {
	if ref == nil {
		return false
	}
	_, ok := ref.Bounds()
	return ok
}

func (c *focusRegionController) hasEnabledCandidate(r EditorRegion) bool {
	if c == nil {
		return false
	}
	for _, cand := range c.candidates[r] {
		if !refMounted(cand.ref) {
			continue
		}
		if cand.enabled != nil && !cand.enabled() {
			continue
		}
		return true
	}
	return false
}

func (c *focusRegionController) onRegionFocusIn(r EditorRegion) {
	if c == nil {
		return
	}
	c.focusedRegion.Set(r)
	if ref := core.FocusedRef(c.cx); ref != nil {
		c.lastFocused[r] = ref
	}
	// If keyboard focus entered a different region without F6 (for example via Tab),
	// clear any stale F6 region ring on the previous region.
	if ring := c.activeRegionRing.GetUntracked(); ring != RegionNone && ring != r {
		c.activeRegionRing.Set(RegionNone)
	}
}

func (c *focusRegionController) onRegionFocusOut(r EditorRegion) {
	if c == nil {
		return
	}
	if c.focusedRegion.GetUntracked() == r {
		// Check whether focus actually moved outside r.
		if root := c.roots[r]; root == nil || !root.HoldsFocus() {
			c.focusedRegion.Set(RegionNone)
		}
	}
	if c.activeRegionRing.GetUntracked() == r {
		if root := c.roots[r]; root == nil || !root.HoldsFocus() {
			c.activeRegionRing.Set(RegionNone)
		}
	}
	if c.fallbackRegion.GetUntracked() == r {
		if root := c.roots[r]; root == nil || !root.HoldsFocus() {
			c.fallbackRegion.Set(RegionNone)
		}
	}
}

func (c *focusRegionController) clearRegionRingOnPointer() {
	if c == nil {
		return
	}
	if c.activeRegionRing.GetUntracked() != RegionNone {
		c.activeRegionRing.Set(RegionNone)
	}
}

func (c *focusRegionController) currentFocusedRegion() EditorRegion {
	if c == nil {
		return RegionNone
	}
	// If an F6 region transition was just requested and the native host has not yet
	// delivered EventFocus for it, prefer the pending F6 target over the stale native
	// HoldsFocus so rapid back-to-back F6 presses always advance step by step.
	if ring := c.activeRegionRing.GetUntracked(); ring != RegionNone {
		return ring
	}
	for _, r := range EditorRegionOrder() {
		if root := c.roots[r]; root != nil && root.HoldsFocus() {
			return r
		}
	}
	return c.focusedRegion.GetUntracked()
}

func (c *focusRegionController) visibleRegions(ed *Editor) []EditorRegion {
	if c == nil {
		return nil
	}
	order := EditorRegionOrder()
	out := make([]EditorRegion, 0, len(order))
	for _, r := range order {
		if r == RegionToast && !ed.HasActiveToast() {
			continue
		}
		if refMounted(c.roots[r]) {
			out = append(out, r)
		}
	}
	return out
}

func (c *focusRegionController) regionOutlineStyle(r EditorRegion, baseStyle core.Style) core.Style {
	if c != nil && c.activeRegionRing.Get() == r {
		return baseStyle.Merge(*specRowFocusRingStyle)
	}
	return baseStyle
}

func (c *focusRegionController) snapshotCurrentFocus() EditorRegion {
	if c == nil {
		return RegionNone
	}
	cur := c.currentFocusedRegion()
	if cur != RegionNone {
		// Only overwrite lastFocused[cur] when native focus is genuinely inside cur,
		// so rapid back-to-back F6 presses do not store the previous region's ref under cur.
		if root := c.roots[cur]; root != nil && root.HoldsFocus() {
			if ref := core.FocusedRef(c.cx); ref != nil {
				c.lastFocused[cur] = ref
			}
		}
	}
	return cur
}

func (c *focusRegionController) focusRegion(ed *Editor, target EditorRegion) bool {
	if c == nil || target == RegionNone {
		return false
	}
	root := c.roots[target]
	if !refMounted(root) {
		return false
	}
	c.activeRegionRing.Set(target)
	c.focusedRegion.Set(target)

	// 1. Restore the last focused control inside this region when still mounted.
	if saved := c.lastFocused[target]; refMounted(saved) && saved != root {
		// Verify saved is not an unmounted or disabled known candidate.
		disabled := false
		for _, cand := range c.candidates[target] {
			if cand.ref == saved && cand.enabled != nil && !cand.enabled() {
				disabled = true
				break
			}
		}
		if !disabled {
			c.fallbackRegion.Set(RegionNone)
			saved.Focus()
			return true
		}
	}

	// 2. Otherwise focus the first mounted, enabled registered control in the region.
	c.pruneCandidates(target)
	for _, cand := range c.candidates[target] {
		if !refMounted(cand.ref) {
			continue
		}
		if cand.enabled != nil && !cand.enabled() {
			continue
		}
		c.fallbackRegion.Set(RegionNone)
		cand.ref.Focus()
		c.lastFocused[target] = cand.ref
		return true
	}

	// 3. Fallback: focus the region container itself (e.g. Status bar with no buttons,
	// or empty Inspector) so F6 always lands cleanly in the region and draws its inset ring.
	c.fallbackRegion.Set(target)
	root.Focus()
	c.lastFocused[target] = root
	return true
}

func (c *focusRegionController) cycleRegion(ed *Editor, delta int) EditorRegion {
	if c == nil || delta == 0 {
		return RegionNone
	}
	// Never move focus during an active pointer drag.
	if ed.IsDragging() {
		return c.currentFocusedRegion()
	}
	visible := c.visibleRegions(ed)
	if len(visible) == 0 {
		return RegionNone
	}
	cur := c.snapshotCurrentFocus()
	idx := slices.Index(visible, cur)
	var nextIdx int
	if idx < 0 {
		if delta > 0 {
			nextIdx = 0
		} else {
			nextIdx = len(visible) - 1
		}
	} else {
		nextIdx = (idx + delta%len(visible) + len(visible)) % len(visible)
	}
	target := visible[nextIdx]
	c.focusRegion(ed, target)
	return target
}

func (c *focusRegionController) recordDialogOpener(ref *core.Ref) {
	if c == nil {
		return
	}
	c.pendingOpener = ref
}

func (c *focusRegionController) pushDialogOpener() {
	if c == nil {
		return
	}
	opener := core.FocusedRef(c.cx)
	if opener == nil && refMounted(c.pendingOpener) {
		opener = c.pendingOpener
	}
	c.pendingOpener = nil
	c.dialogOpeners = append(c.dialogOpeners, opener)
	// Hide F6 region highlight while a modal dialog traps focus.
	c.activeRegionRing.Set(RegionNone)
}

func (c *focusRegionController) restoreDialogOpener() {
	if c == nil || len(c.dialogOpeners) == 0 {
		return
	}
	opener := c.dialogOpeners[len(c.dialogOpeners)-1]
	c.dialogOpeners = c.dialogOpeners[:len(c.dialogOpeners)-1]
	if refMounted(opener) {
		opener.Focus()
	}
}

// bindDialogFocusRestore hooks a modal dialog's open/close lifecycle so that:
//   - while open, Tab / Shift-Tab stay strictly trapped inside the topmost modal via
//     core.MoveFocus (bridging NSGo v0.3.0's nsgotest.App.moveFocus which does not
//     scope Tab traversal to KindModal), and
//   - when closed (Esc, Cancel, or confirmation), keyboard focus returns to the exact
//     control that opened the dialog once the modal has dismissed.
//
// NSGo gap (v0.3.0): core.Modal and ui.Dialog dismiss on Esc, but do not save or
// restore the pre-modal opener focus on dismissal, and nsgotest's Tab traversal does
// not filter out views behind a modal.
func bindDialogFocusRestore(cx *core.Scope, ed *Editor, isOpen func() bool) (onClose func()) {
	var wasOpen bool
	var needRestore bool
	cx.Effect(func() {
		open := isOpen()
		cx.Untrack(func() {
			if open && !wasOpen {
				wasOpen = true
				needRestore = true
				if ed.focus.ctrl != nil {
					ed.focus.ctrl.pushDialogOpener()
					ed.focus.ctrl.openDialogCount.Set(ed.focus.ctrl.openDialogCount.GetUntracked() + 1)
				}
			} else if !open && wasOpen {
				wasOpen = false
				if ed.focus.ctrl != nil {
					if n := ed.focus.ctrl.openDialogCount.GetUntracked(); n > 0 {
						ed.focus.ctrl.openDialogCount.Set(n - 1)
					}
				}
			}
		})
	})
	cx.Cleanup(func() {
		if wasOpen && ed.focus.ctrl != nil {
			wasOpen = false
			if n := ed.focus.ctrl.openDialogCount.GetUntracked(); n > 0 {
				ed.focus.ctrl.openDialogCount.Set(n - 1)
			}
		}
	})
	return func() {
		if !needRestore {
			return
		}
		needRestore = false
		if ed.focus.ctrl != nil {
			ed.focus.ctrl.restoreDialogOpener()
		}
	}
}

// dialogFocusTrapView binds Tab and Shift-Tab to core.MoveFocus (which strictly
// scopes stops to the topmost core.Modal and wraps inside it) whenever at least one
// editor dialog is open.
func dialogFocusTrapView(ed *Editor) core.Node {
	return core.Show{
		When: func() bool {
			return ed.focus.ctrl != nil && ed.focus.ctrl.openDialogCount.Get() > 0
		},
		Then: func(cx *core.Scope) core.Node {
			opts := core.KeyOptions{When: core.KeyAlways, OverModals: true}
			cx.OnKeyWith("tab", opts, func() {
				core.MoveFocus(cx, core.FocusDirNext, true)
			})
			cx.OnKeyWith("shift+tab", opts, func() {
				core.MoveFocus(cx, core.FocusDirPrev, true)
			})
			return core.View{
				PointerEvents: core.PointerEventsNone,
				Style: core.Style{
					Position: core.Absolute,
					Width:    core.Pt(0),
					Height:   core.Pt(0),
				},
			}
		},
	}
}

// --- Public Editor focus / region / stability API ---

// ActiveRegion returns the keyboard region that currently holds focus, or RegionNone.
func (e *Editor) ActiveRegion() EditorRegion {
	if e == nil || e.focus.ctrl == nil {
		return RegionNone
	}
	return e.focus.ctrl.currentFocusedRegion()
}

// ActiveRegionRing returns the keyboard region currently displaying its F6 inset ring,
// or RegionNone when no F6 region ring is shown.
func (e *Editor) ActiveRegionRing() EditorRegion {
	if e == nil || e.focus.ctrl == nil {
		return RegionNone
	}
	return e.focus.ctrl.activeRegionRing.GetUntracked()
}

// CycleRegion moves keyboard focus to the next (+1) or previous (-1) visible editor
// region in Spec order (Header -> Scene tree -> Viewport -> Inspector -> Status bar -> Toast),
// drawing the region's inset ring and restoring its last focused control.
func (e *Editor) CycleRegion(delta int) EditorRegion {
	if e == nil || e.focus.ctrl == nil {
		return RegionNone
	}
	return e.focus.ctrl.cycleRegion(e, delta)
}

// SetAgentPanelRef registers the agent panel container ref so focus-stability rules
// can check whether keyboard focus is already inside the agent panel before focusing
// a newly arrived permission card or question.
func (e *Editor) SetAgentPanelRef(ref *core.Ref) {
	if e == nil || e.focus.ctrl == nil {
		return
	}
	e.focus.ctrl.agentPanelRef = ref
}

// IsDragging reports whether any interactive drag, marquee, gizmo, camera drag, or
// paint stroke is currently active in the editor. Nothing may move focus while true.
func (e *Editor) IsDragging() bool {
	if e == nil {
		return false
	}
	_, gmActive := e.GridMapStrokeActive()
	_, strokeActive := e.TileStrokeActive()
	return e.Dragging2D() != Handle2DNone ||
		e.MarqueeActive() ||
		e.AssetDragActive() ||
		e.DraggingGizmo3D() ||
		e.DraggingHandle3D() ||
		e.DraggingComponentGizmo3D() ||
		e.PluginDragging3D() ||
		e.DraggingShape2D() ||
		e.PluginDragging2D() ||
		e.cameraDrag.active ||
		e.cameraBoundsDrag.active ||
		e.roomDrag.active ||
		gmActive ||
		(e.terrain != nil && e.terrain.stroke != nil) ||
		strokeActive
}

// IsTyping reports whether keyboard focus is currently inside a text field.
// Nothing may steal focus while true.
func (e *Editor) IsTyping() bool {
	if e == nil || e.focus.ctrl == nil {
		return false
	}
	return e.focus.ctrl.textFocusDepth > 0
}

// noteTextInputFocus updates the text-editing focus counter for focus-stability guards.
func (e *Editor) noteTextInputFocus(focused bool) {
	if e == nil || e.focus.ctrl == nil {
		return
	}
	if focused {
		e.focus.ctrl.textFocusDepth++
	} else if e.focus.ctrl.textFocusDepth > 0 {
		e.focus.ctrl.textFocusDepth--
	}
}

// FocusInAgentPanel reports whether keyboard focus is currently inside the agent panel.
func (e *Editor) FocusInAgentPanel() bool {
	if e == nil || e.focus.ctrl == nil {
		return false
	}
	return e.focus.ctrl.agentPanelRef != nil && e.focus.ctrl.agentPanelRef.HoldsFocus()
}

// CanAutoMoveFocus reports whether background UI activity is allowed to move focus
// (false during any drag or while the person is typing in a text field).
func (e *Editor) CanAutoMoveFocus() bool {
	return !e.IsDragging() && !e.IsTyping()
}

// ShouldFocusAgentPrompt reports whether a newly arrived agent permission card or
// question may take keyboard focus: true only when focus is already in the agent
// panel and no drag or text typing is in progress (Spec rule 05).
func (e *Editor) ShouldFocusAgentPrompt() bool {
	return e.CanAutoMoveFocus() && e.FocusInAgentPanel()
}

// --- Viewport Tab / Shift-Tab Object Selection ---

// SelectNextObject selects the next visible, unlocked object in scene order (wrapping
// around to the first), returning the newly selected ID or "" when the scene has none.
// Used by Tab in the viewport region.
func (e *Editor) SelectNextObject() string {
	return e.stepViewportSelection(+1)
}

// SelectPreviousObject selects the previous visible, unlocked object in scene order
// (wrapping around to the last), returning the newly selected ID or "" when the scene
// has none. Used by Shift-Tab in the viewport region.
func (e *Editor) SelectPreviousObject() string {
	return e.stepViewportSelection(-1)
}

func (e *Editor) stepViewportSelection(delta int) string {
	if e == nil || e.Playing() || delta == 0 {
		return ""
	}
	ids := e.viewportSelectableObjectIDs()
	if len(ids) == 0 {
		return ""
	}
	idx := slices.Index(ids, e.SelectedID)
	var next int
	if idx < 0 {
		if delta > 0 {
			next = 0
		} else {
			next = len(ids) - 1
		}
	} else {
		next = (idx + delta%len(ids) + len(ids)) % len(ids)
	}
	id := ids[next]
	e.SelectObject(id)
	return id
}

func (e *Editor) viewportSelectableObjectIDs() []string {
	if e == nil || e.Doc == nil {
		return nil
	}
	doc := e.Doc
	if e.Built != nil && e.Built.Doc != nil {
		doc = e.Built.Doc
	}
	var ids []string
	for i := range doc.Layers {
		layer := &doc.Layers[i]
		if !layer.IsVisible() || e.LayerLocked(layer.Name) {
			continue
		}
		e.collectSelectableObjects(layer.Objects, &ids)
	}
	return ids
}

func (e *Editor) collectSelectableObjects(objs []sceneformat.ObjectDoc, out *[]string) {
	for i := range objs {
		obj := &objs[i]
		if e.ObjectHidden(obj.ID) {
			continue
		}
		if !e.ObjectLocked(obj.ID) {
			*out = append(*out, obj.ID)
		}
		if len(obj.Children) > 0 {
			e.collectSelectableObjects(obj.Children, out)
		}
	}
}

// --- Spec Focus-Ring Control Primitives ---

// SpecSwitch is a Spec-compliant on/off switch (RoleSwitch, aria-checked) with a
// 2 px accent focus ring at +2 px offset on :focus-visible only. Mouse clicks toggle
// without drawing the ring.
//
// NSGo gap (v0.3.0): ui.Switch does not set FocusVisible on its pressable view or track.
type SpecSwitch struct {
	Ref      *core.Ref
	Checked  func() bool
	Disabled func() bool
	Invalid  func() bool
	OnChange func(bool)
	Label    string
	Size     ui.Size
	TestID   string
}

// Render implements core.Node.
func (s SpecSwitch) Render(cx *core.Scope) core.Node {
	checked := func() bool { return s.Checked != nil && s.Checked() }
	return base.Switch{
		Ref:      s.Ref,
		Checked:  checked,
		Disabled: s.Disabled,
		Invalid:  s.Invalid,
		OnChange: s.OnChange,
		Label:    s.Label,
		TestID:   s.TestID,
		// HitSlop extends the press target past 28×28 without enlarging the 32×18 capsule
		// box that carries the +2 px offset focus ring.
		HitSlop: core.All(8),
		Style: core.Style{
			Width:        core.Pt(32),
			Height:       core.Pt(18),
			CornerRadius: 9,
			FocusVisible: specFocusRingStyle,
			Disabled:     &core.Style{Opacity: core.Alpha(0.4)},
		},
		TrackStyleFunc: func() core.Style {
			bg := specLineRaised
			if checked() {
				bg = specAccent
			}
			return core.Style{
				Width:        core.Pt(32),
				Height:       core.Pt(18),
				CornerRadius: 9,
				Padding:      core.All(2),
				Background:   bg,
			}
		},
		ThumbStyleFunc: func() core.Style {
			tx := core.Points(0)
			if checked() {
				tx = 14
			}
			return core.Style{
				Width:        core.Pt(14),
				Height:       core.Pt(14),
				CornerRadius: 7,
				Background:   specTextStrong,
				Transform:    core.Transform{TranslateX: tx},
			}
		},
	}
}

// SpecIconButton is a Spec-compliant icon-only button with a 2 px accent focus ring
// at +2 px offset on :focus-visible only and a tooltip that opens on hover and focus.
//
// NSGo gap (v0.3.0): ui.IconButton hardcodes FocusRing(cx, -2) on ghost buttons and
// omits FocusVisible on circular variants.
type SpecIconButton struct {
	Ref      *core.Ref
	Icon     func() *core.SVG
	Label    string
	Tooltip  string
	Size     core.Points
	IconSize core.Points
	Tint     func() core.Color
	Selected func() bool
	Disabled func() bool
	OnPress  func()
	TestID   string
}

// Render implements core.Node.
func (b SpecIconButton) Render(cx *core.Scope) core.Node {
	size := b.Size
	if size <= 0 {
		size = 30
	}
	iconSize := b.IconSize
	if iconSize <= 0 {
		iconSize = 14
	}
	tint := b.Tint
	if tint == nil {
		tint = func() core.Color { return specText }
	}
	tip := b.Tooltip
	if tip == "" {
		tip = b.Label
	}
	btn := base.Button{
		Ref:      b.Ref,
		TestID:   b.TestID,
		Label:    b.Label,
		Selected: b.Selected,
		Disabled: b.Disabled,
		OnPress: func(core.PressEvent) {
			if b.OnPress != nil {
				b.OnPress()
			}
		},
		Style: core.Style{
			Width:          core.Pt(size),
			Height:         core.Pt(size),
			CornerRadius:   8,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     specRaised,
			BorderWidth:    1,
			BorderColor:    specLineRaised,
			Hover:          &core.Style{Background: specLineRaised},
			FocusVisible:   specFocusRingStyle,
			Disabled:       &core.Style{Opacity: core.Alpha(0.4)},
		},
		Children: []core.Node{
			ui.IconOf(b.Icon, iconSize, tint),
		},
	}
	if tip == "" {
		return btn
	}
	return ui.Tooltip{
		Text:    tip,
		Trigger: btn,
	}
}

// SpecFocusWithinCard wraps a composer or card container so keyboard focus on any
// control inside it draws the 2 px accent ring (+2 px offset) on the card itself
// via :focus-within, matching docs/design/editor/Spec.dc.html.
type SpecFocusWithinCard struct {
	Ref      *core.Ref
	TestID   string
	Style    core.Style
	Children []core.Node
}

// Render implements core.Node.
func (c SpecFocusWithinCard) Render(*core.Scope) core.Node {
	baseStyle := core.Style{
		CornerRadius: 12,
		Background:   specRaised,
		BorderWidth:  1,
		BorderColor:  specLineRaised,
		FocusWithin:  specCardFocusWithinStyle,
	}
	return core.View{
		Ref:      c.Ref,
		TestID:   c.TestID,
		Style:    baseStyle.Merge(c.Style),
		Children: c.Children,
	}
}

// specInputField wraps a ui.Input with the Spec inset -2 px focus ring and text-focus
// tracking for focus-stability guards (Editor.IsTyping).
func specInputField(ed *Editor, in ui.Input) ui.Input {
	if in.Style.FocusVisible == nil {
		in.Style.FocusVisible = specRowFocusRingStyle
	}
	origFocus, origBlur := in.OnFocus, in.OnBlur
	in.OnFocus = func() {
		ed.noteTextInputFocus(true)
		if origFocus != nil {
			origFocus()
		}
	}
	in.OnBlur = func() {
		ed.noteTextInputFocus(false)
		if origBlur != nil {
			origBlur()
		}
	}
	return in
}
