package editor

import (
	"slices"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec focus ring fallback styles matching docs/design/editor/Spec.dc.html ("Focus ring").
// Prefer ed.SpecPalette(cx).FocusRing(offset) or SpecPaletteFor(cx.Dark()).FocusRing(offset)
// whenever a *core.Scope is in scope so Dark (#3B7BFF) and Light (#1D5CE6) both meet WCAG AA.
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

// FocusTheme returns the editor's unified Spec *core.Theme (SpecTheme), mapping
// focus, info, and ring roles to Spec accent (#3B7BFF in Dark, #1D5CE6 in Light)
// so every ui component calling ui.FocusRing draws the Spec 2 px focus ring on
// :focus-visible while preserving both Spec Dark and Spec Light surfaces.
//
// NSGo gap (v0.3.0): ui.FocusRing reads Palette.Colors["info"] rather than a
// dedicated focus-ring token, ui.Switch carries no FocusVisible style, ui.IconButton
// hardcodes offset -2 on ghost buttons (and omits FocusVisible on circular variants),
// ui.Input uses only a 1 px border color change without an inset outline, and
// core.Modal / ui.Dialog do not restore focus to the opener on close. Editor-level
// helpers in this file bridge each gap.
func FocusTheme() *core.Theme {
	return SpecTheme()
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

	// Pending opener recorded on pointer press before a modal/dialog or Settings view opens,
	// and active open-modal counter driving the modal Tab/Shift-Tab focus trap.
	pendingOpener   *core.Ref
	openDialogCount *core.Signal[int]

	// Text input focus tracking for focus-stability checks (IsTyping).
	// textFocusRef is verified mounted + HoldsFocus inside IsTyping so unmounting a focused
	// input without OnBlur can never leave IsTyping permanently stuck true.
	textFocusDepth int
	textFocusRef   *core.Ref

	// Toast region focus tracking: remembers where keyboard focus was before entering
	// RegionToast so Esc dismisses the focused toast and restores focus cleanly, and
	// drives reactive timer pausing + focused-toast Esc binding.
	toastFocused       *core.Signal[bool]
	lastNonToastFocus  *core.Ref
	lastNonToastRegion EditorRegion
	toastReturnFocus   *core.Ref
	toastReturnRegion  EditorRegion
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
		toastFocused:     core.NewSignal(cx, false),
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

func (c *focusRegionController) registerCandidate(cx *core.Scope, r EditorRegion, ref *core.Ref, enabled func() bool) *core.Ref {
	if ref == nil {
		ref = &core.Ref{}
	}
	if c == nil {
		return ref
	}
	updated := false
	for i := range c.candidates[r] {
		if c.candidates[r][i].ref == ref {
			c.candidates[r][i].enabled = enabled
			updated = true
			break
		}
	}
	if !updated {
		c.candidates[r] = append(c.candidates[r], regionCandidate{ref: ref, enabled: enabled})
	}
	if cx != nil {
		cx.Cleanup(func() {
			c.unregisterCandidate(r, ref)
		})
	}
	return ref
}

func (c *focusRegionController) unregisterCandidate(r EditorRegion, ref *core.Ref) {
	if c == nil || ref == nil {
		return
	}
	list := c.candidates[r]
	for i := range list {
		if list[i].ref == ref {
			c.candidates[r] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if c.lastFocused[r] == ref {
		delete(c.lastFocused, r)
	}
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
	// ToastView is mounted inside ViewportView; ignore bubbled focus-in on RegionViewport
	// while native focus is inside RegionToast.
	if r == RegionViewport {
		if toastRoot := c.roots[RegionToast]; toastRoot != nil && toastRoot.HoldsFocus() {
			return
		}
	}
	c.focusedRegion.Set(r)
	if ref := core.FocusedRef(c.cx); ref != nil {
		c.lastFocused[r] = ref
		if r != RegionToast {
			c.lastNonToastFocus = ref
			c.lastNonToastRegion = r
		}
	}
	if r == RegionToast {
		if c.toastReturnFocus == nil && refMounted(c.lastNonToastFocus) {
			c.toastReturnFocus = c.lastNonToastFocus
			c.toastReturnRegion = c.lastNonToastRegion
		}
		if c.toastFocused != nil {
			c.toastFocused.Set(true)
		}
	} else {
		c.toastReturnFocus = nil
		c.toastReturnRegion = RegionNone
		if c.toastFocused != nil {
			c.toastFocused.Set(false)
		}
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
	if r == RegionToast {
		if root := c.roots[RegionToast]; root == nil || !root.HoldsFocus() {
			if c.toastFocused != nil {
				c.toastFocused.Set(false)
			}
		}
	}
	if c.focusedRegion.GetUntracked() == r {
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

func (c *focusRegionController) restoreFocusFromToast() {
	if c == nil {
		return
	}
	target := c.toastReturnFocus
	targetRegion := c.toastReturnRegion
	c.toastReturnFocus = nil
	c.toastReturnRegion = RegionNone
	if c.toastFocused != nil {
		c.toastFocused.Set(false)
	}
	if c.activeRegionRing.GetUntracked() == RegionToast {
		c.activeRegionRing.Set(RegionNone)
	}
	if c.fallbackRegion.GetUntracked() == RegionToast {
		c.fallbackRegion.Set(RegionNone)
	}
	if !refMounted(target) {
		target = c.lastNonToastFocus
		targetRegion = c.lastNonToastRegion
	}
	if refMounted(target) {
		target.Focus()
		if targetRegion != RegionNone {
			c.focusedRegion.Set(targetRegion)
		}
		return
	}
	if targetRegion != RegionNone {
		if root := c.roots[targetRegion]; refMounted(root) {
			root.Focus()
			c.focusedRegion.Set(targetRegion)
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
	// Check RegionToast before RegionViewport because ToastView is mounted inside ViewportView.
	if toastRoot := c.roots[RegionToast]; toastRoot != nil && toastRoot.HoldsFocus() {
		return RegionToast
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
		dark := true
		if c.cx != nil {
			dark = c.cx.Dark()
		}
		return baseStyle.Merge(*SpecPaletteFor(dark).FocusRing(-2))
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
	if c.toastFocused != nil {
		c.toastFocused.Set(target == RegionToast)
	}

	// 1. Restore the last focused control inside this region when still mounted.
	if saved := c.lastFocused[target]; refMounted(saved) && saved != root {
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

func (e *Editor) regionCyclingBlocked() bool {
	if e == nil {
		return true
	}
	return e.shortcutCaptureActive ||
		e.AskInPlaceOpen() ||
		e.StartScreenOpen() ||
		e.MergeConflictOpen() ||
		e.PlaytestReportOpen() ||
		e.UsagesScreenOpen() ||
		e.ChangesOpen() ||
		e.HUDLayoutOpen() ||
		e.BuildReleaseScreenOpen() ||
		e.AssetGeneratorOpen() ||
		e.SettingsOpen() ||
		(e.CutsceneOpen() && !e.Playing())
}

func (c *focusRegionController) cycleRegion(ed *Editor, delta int) EditorRegion {
	if c == nil || delta == 0 {
		return RegionNone
	}
	// Never move focus during an active drag/scrub or while shortcut capture / full-window screens are active.
	if ed.IsDragging() || ed.regionCyclingBlocked() {
		return c.currentFocusedRegion()
	}
	if ed.ObjectContextMenuOpen() {
		ed.CloseObjectContextMenu()
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
	if target == RegionToast && cur != RegionToast {
		if prev := core.FocusedRef(c.cx); refMounted(prev) {
			c.toastReturnFocus = prev
			c.toastReturnRegion = cur
		} else if refMounted(c.lastNonToastFocus) {
			c.toastReturnFocus = c.lastNonToastFocus
			c.toastReturnRegion = c.lastNonToastRegion
		}
	}
	c.focusRegion(ed, target)
	return target
}

func (c *focusRegionController) recordDialogOpener(ref *core.Ref) {
	if c == nil {
		return
	}
	c.pendingOpener = ref
}

func (c *focusRegionController) captureDialogOpener() *core.Ref {
	if c == nil {
		return nil
	}
	opener := core.FocusedRef(c.cx)
	if opener == nil && refMounted(c.pendingOpener) {
		opener = c.pendingOpener
	}
	c.pendingOpener = nil
	// Hide F6 region highlight while a modal dialog or overlay screen traps focus.
	c.activeRegionRing.Set(RegionNone)
	return opener
}

// bindDialogFocusRestore hooks a modal dialog's open/close lifecycle so that:
//   - while open, Tab / Shift-Tab stay strictly trapped inside the topmost modal via
//     core.MoveFocus (bridging NSGo v0.3.0's nsgotest.App.moveFocus which does not
//     scope Tab traversal to KindModal), and
//   - when closed (Esc, Cancel, or confirmation), keyboard focus returns to the exact
//     control that opened the dialog once the modal has dismissed.
//
// Each binding holds its own opener ref locally rather than sharing a global LIFO slice,
// so out-of-order closes and scope cleanups (for example language/theme chrome remounts)
// can never desync or restore another dialog's opener.
func bindDialogFocusRestore(cx *core.Scope, ed *Editor, isOpen func() bool) (onClose func()) {
	var wasOpen bool
	var needRestore bool
	var opener *core.Ref
	cx.Effect(func() {
		open := isOpen()
		cx.Untrack(func() {
			if open && !wasOpen {
				wasOpen = true
				needRestore = true
				if ed.focus.ctrl != nil {
					opener = ed.focus.ctrl.captureDialogOpener()
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
		opener = nil
		needRestore = false
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
		target := opener
		opener = nil
		if refMounted(target) {
			target.Focus()
		}
	}
}

// bindViewFocusRestore tracks opener focus for a non-modal full-window view (such as SettingsView)
// that opens over the workspace without core.Modal, restoring focus to the opener after the view unmounts.
func bindViewFocusRestore(cx *core.Scope, ed *Editor, isOpen func() bool) {
	var wasOpen bool
	var opener *core.Ref
	cx.Effect(func() {
		open := isOpen()
		cx.Untrack(func() {
			if open && !wasOpen {
				wasOpen = true
				if ed.focus.ctrl != nil {
					opener = ed.focus.ctrl.captureDialogOpener()
				}
			} else if !open && wasOpen {
				wasOpen = false
				target := opener
				opener = nil
				if target != nil {
					cx.Post(func() {
						if refMounted(target) {
							target.Focus()
						}
					})
				}
			}
		})
	})
	cx.Cleanup(func() {
		wasOpen = false
		opener = nil
	})
}

// dialogFocusTrapView binds Tab and Shift-Tab to core.MoveFocus (which strictly
// scopes stops to the topmost core.Modal and wraps inside it) whenever at least one
// editor dialog is open and shortcut capture is inactive.
func dialogFocusTrapView(ed *Editor) core.Node {
	return core.Show{
		When: func() bool {
			return ed.focus.ctrl != nil && ed.focus.ctrl.openDialogCount.Get() > 0
		},
		Then: func(cx *core.Scope) core.Node {
			opts := core.KeyOptions{When: core.KeyAlways, OverModals: true}
			cx.OnKeyWith("tab", opts, func() {
				if ed.shortcutCaptureActive {
					return
				}
				core.MoveFocus(cx, core.FocusDirNext, true)
			})
			cx.OnKeyWith("shift+tab", opts, func() {
				if ed.shortcutCaptureActive {
					return
				}
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

// IsDragging reports whether any interactive drag, scrub, marquee, gizmo, camera drag/flight,
// or paint stroke is currently active in the editor. Nothing may move focus while true.
func (e *Editor) IsDragging() bool {
	if e == nil {
		return false
	}
	_, gmActive := e.GridMapStrokeActive()
	_, strokeActive := e.TileStrokeActive()
	rigDragging := e.RigOpen() && e.rig.drag.handle != RigHandleNone
	return e.Dragging2D() != Handle2DNone ||
		e.MarqueeActive() ||
		e.InspectorScrubbing() ||
		e.AssetDragActive() ||
		e.DraggingGizmo3D() ||
		e.DraggingHandle3D() ||
		e.DraggingComponentGizmo3D() ||
		e.PluginDragging3D() ||
		e.DraggingShape2D() ||
		e.PluginDragging2D() ||
		e.drag3D.kind != drag3DNone ||
		e.fly3D ||
		rigDragging ||
		e.HUDTouchDragActive() ||
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
	c := e.focus.ctrl
	if c.textFocusRef != nil {
		if !refMounted(c.textFocusRef) || !c.textFocusRef.HoldsFocus() {
			c.textFocusRef = nil
			c.textFocusDepth = 0
			return false
		}
		return true
	}
	return c.textFocusDepth > 0
}

// noteTextInputFocus updates the text-editing focus state for focus-stability guards.
func (e *Editor) noteTextInputFocus(focused bool) {
	if e == nil || e.focus.ctrl == nil {
		return
	}
	c := e.focus.ctrl
	if focused {
		c.textFocusDepth++
		if ref := core.FocusedRef(c.cx); ref != nil {
			c.textFocusRef = ref
		}
	} else {
		if c.textFocusDepth > 0 {
			c.textFocusDepth--
		}
		if c.textFocusDepth == 0 {
			c.textFocusRef = nil
		}
	}
}

// resetTextInputFocus clears stale text-input focus tracking across lifecycle transitions
// (tab switches, Play mode, OpenProject, OpenDemo).
func (e *Editor) resetTextInputFocus() {
	if e == nil || e.focus.ctrl == nil {
		return
	}
	e.focus.ctrl.textFocusDepth = 0
	e.focus.ctrl.textFocusRef = nil
}

// FocusInAgentPanel reports whether keyboard focus is currently inside the agent panel.
func (e *Editor) FocusInAgentPanel() bool {
	if e == nil || e.focus.ctrl == nil {
		return false
	}
	return e.focus.ctrl.agentPanelRef != nil && e.focus.ctrl.agentPanelRef.HoldsFocus()
}

// CanAutoMoveFocus reports whether background UI activity is allowed to move focus
// (false during any drag/scrub or while the person is typing in a text field).
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
// (wrapping around to the last), returning the newly selected ID or "" when the scene has none.
// Used by Shift-Tab in the viewport region.
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
// 2 px accent focus ring at +2 px offset on :focus-visible only in both Spec Dark
// and Spec Light appearances. Mouse clicks toggle without drawing the ring.
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
	p := SpecPaletteFor(cx.Dark())
	colors := ui.ColorsOf(cx)
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
			FocusVisible: p.FocusRing(2),
			Disabled:     &core.Style{Opacity: core.Alpha(0.4)},
		},
		TrackStyleFunc: func() core.Style {
			cur := SpecPaletteFor(cx.Dark())
			bg := colors.Track()
			if bg == 0 {
				bg = cur.LineRaised
			}
			if checked() {
				bg = cur.Accent.Fg
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
			knob := colors.Knob()
			if knob == 0 {
				knob = core.RGB(0xFFFFFF)
			}
			return core.Style{
				Width:        core.Pt(14),
				Height:       core.Pt(14),
				CornerRadius: 7,
				Background:   knob,
				Transform:    core.Transform{TranslateX: tx},
			}
		},
	}
}

// SpecIconButton is a Spec-compliant icon-only button with a 2 px accent focus ring
// at +2 px offset on :focus-visible only in both Spec Dark and Spec Light appearances,
// and a tooltip that opens on hover and focus.
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
	p := SpecPaletteFor(cx.Dark())
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
		tint = func() core.Color { return SpecPaletteFor(cx.Dark()).TextSecondary }
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
			Background:     p.Raised,
			BorderWidth:    1,
			BorderColor:    p.LineRaised,
			Hover:          &core.Style{Background: p.LineRaised},
			FocusVisible:   p.FocusRing(2),
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
// via :focus-within in both Spec Dark and Spec Light appearances.
type SpecFocusWithinCard struct {
	Ref      *core.Ref
	TestID   string
	Style    core.Style
	Children []core.Node
}

// Render implements core.Node.
func (c SpecFocusWithinCard) Render(cx *core.Scope) core.Node {
	p := SpecPaletteFor(cx.Dark())
	baseStyle := core.Style{
		CornerRadius: 12,
		Background:   p.Raised,
		BorderWidth:  1,
		BorderColor:  p.LineRaised,
		FocusWithin:  p.FocusRing(2),
	}
	return core.View{
		Ref:      c.Ref,
		TestID:   c.TestID,
		Style:    baseStyle.Merge(c.Style),
		Children: c.Children,
	}
}

// specInputField wraps a ui.Input with the Spec inset -2 px focus ring (matching Dark/Light
// appearance) and unmount-safe text-focus tracking for focus-stability guards (Editor.IsTyping).
func specInputField(cx *core.Scope, ed *Editor, in ui.Input) ui.Input {
	if in.Style.FocusVisible == nil {
		in.Style.FocusVisible = ed.SpecPalette(cx).FocusRing(-2)
	}
	var focused bool
	if cx != nil {
		cx.Cleanup(func() {
			if focused {
				focused = false
				ed.noteTextInputFocus(false)
			}
		})
	}
	origFocus, origBlur := in.OnFocus, in.OnBlur
	in.OnFocus = func() {
		if !focused {
			focused = true
			ed.noteTextInputFocus(true)
		}
		if origFocus != nil {
			origFocus()
		}
	}
	in.OnBlur = func() {
		if focused {
			focused = false
			ed.noteTextInputFocus(false)
		}
		if origBlur != nil {
			origBlur()
		}
	}
	return in
}
