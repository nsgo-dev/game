package editor

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec toast motion tokens (docs/design/editor/Toasts.dc.html):
// 180 ms in, fading and rising 8 px; 120 ms out. Reduced motion skips both.
var (
	toastEnterMotion = core.MotionSpec{
		Timing: core.KeyframeTiming{
			Duration: 180 * time.Millisecond,
			Easing:   core.BezierEasing(core.EaseOut),
		},
		Tracks: []core.MotionTrack{
			{Channel: core.MotionOpacity, Stops: core.Stops(0, 1)},
			{Channel: core.MotionTranslateY, Stops: core.Stops(8, 0)},
		},
	}
	toastExitMotion = core.MotionSpec{
		Timing: core.KeyframeTiming{
			Duration: 120 * time.Millisecond,
			Easing:   core.BezierEasing(core.EaseIn),
		},
		Tracks: []core.MotionTrack{
			{Channel: core.MotionOpacity, Stops: core.Stops(1, 0)},
		},
	}
)

// OverlayLegendOpen reports whether the compact viewport overlay legend is visible (true by default in edit mode).
func (e *Editor) OverlayLegendOpen() bool {
	return e != nil && !e.overlayLegendHidden
}

// SetOverlayLegendOpen shows or hides the compact viewport overlay legend in the viewport top-left corner.
func (e *Editor) SetOverlayLegendOpen(open bool) {
	if e == nil || e.overlayLegendHidden == !open {
		return
	}
	e.overlayLegendHidden = !open
	e.notify()
}

// ToastView renders the editor's active toast at the bottom center of the viewport (8 px above
// the viewport bar in edit mode, never over the scene tree, inspector, or agent panel) while
// the viewport is on screen, using exclusively SpecPalette tokens (docs/design/editor/Toasts.dc.html).
func ToastView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return toastHostView(cx, ed, rev, true)
}

// WindowToastView renders the editor's active toast at the bottom center of the window, 24 px up,
// whenever no viewport is on screen (Start screen, Settings, Export, and other full-window screens),
// using exclusively SpecPalette tokens (docs/design/editor/Toasts.dc.html).
func WindowToastView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return toastHostView(cx, ed, rev, false)
}

// toastEscapeKeyView binds Escape to dismiss the focused toast and restore prior keyboard focus
// strictly while focus is inside RegionToast and a toast is active.
func toastEscapeKeyView(ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			if ed == nil || !ed.HasActiveToast() || ed.focus.ctrl == nil || ed.focus.ctrl.toastFocused == nil {
				return false
			}
			return ed.focus.ctrl.toastFocused.Get()
		},
		Then: func(cx *core.Scope) core.Node {
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
				if ed.shortcutCaptureActive {
					return
				}
				ed.dismissFocusedToast()
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

func toastHostView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], inViewport bool) core.Node {
	activeInst := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		if ed == nil || ed.viewportVisibleOnScreen() != inViewport {
			return 0
		}
		return ed.activeToastInstanceID()
	})

	bottomOffset := core.NewMemo(cx, func() core.Points {
		_ = rev.Get()
		if !inViewport {
			// Spec (Toasts.dc.html): with no viewport on screen (Start, Settings), bottom center of window, 24 px up.
			return 24
		}
		if ed != nil && !ed.Playing() {
			// Spec (Toasts.dc.html): 8 px above the bottom-center viewport bar (Bottom 12–16, height ~36–40).
			return 60
		}
		return 16
	})

	return core.View{
		TestID:        toastHostTestID(inViewport),
		Role:          core.RoleStatus,
		LiveRegion:    core.LiveRegionPolite,
		PointerEvents: core.PointerEventsBoxNone,
		StyleFunc: func() core.Style {
			return core.Style{
				Position:       core.Absolute,
				Left:           core.Pt(12),
				Right:          core.Pt(12),
				Bottom:         core.Pt(bottomOffset.Get()),
				Direction:      core.Row,
				JustifyContent: core.JustifyCenter,
			}
		},
		Children: []core.Node{
			core.PresenceOf[uint64]{
				Key:        activeInst.Get,
				Wait:       true,
				MaxLeaving: 1,
				Content: func(cx *core.Scope, instID uint64) core.Node {
					if instID == 0 {
						return nil
					}
					return toastAnimatedBranch(cx, ed, rev, instID, inViewport)
				},
			},
		},
	}
}

func toastHostTestID(inViewport bool) string {
	if inViewport {
		return "editor-toast-host"
	}
	return "editor-window-toast-host"
}

func toastAnimatedBranch(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], instID uint64, inViewport bool) core.Node {
	var motion *core.Motion
	if core.Untracked(cx, cx.Env().ReduceMotion) {
		motion = core.NewMotion(cx, nil)
	} else {
		motion = core.NewMotion(cx, &toastEnterMotion)
	}
	cx.OnExit(func(done func()) {
		// Leave immediately under ReduceMotion, or when transferring between viewport and
		// window host (for example opening Settings while a toast is showing) so two hosts
		// never display duplicate toast nodes at once.
		if core.Untracked(cx, cx.Env().ReduceMotion) || (ed != nil && ed.viewportVisibleOnScreen() != inViewport) {
			done()
			return
		}
		motion.Play(toastExitMotion, func(bool) { done() })
	})

	// Snapshot the last known toast state for this instance so exit animations keep
	// displaying the departing toast's content smoothly even after e.toasts advances.
	var lastSnap Toast
	var lastWaiting int
	if ed != nil && ed.activeToastInstanceID() == instID {
		if t, ok := ed.ActiveToast(); ok {
			lastSnap = t
			lastWaiting = ed.QueuedToastCount()
		}
	}

	readToast := func() (Toast, int) {
		_ = rev.Get()
		if ed != nil && ed.activeToastInstanceID() == instID {
			if t, ok := ed.ActiveToast(); ok {
				lastSnap = t
				lastWaiting = ed.QueuedToastCount()
			}
		}
		return lastSnap, lastWaiting
	}

	type toastStructKey struct {
		dark           bool
		kind           ToastKind
		actionLabel    string
		actionShortcut string
		canDismiss     bool
	}
	structKey := core.NewMemo(cx, func() toastStructKey {
		t, _ := readToast()
		return toastStructKey{
			dark:           cx.Dark(),
			kind:           t.Kind,
			actionLabel:    t.ActionLabel,
			actionShortcut: t.ActionShortcut,
			canDismiss:     t.canDismiss(),
		}
	})

	actionRef := &core.Ref{}
	dismissRef := &core.Ref{}
	var toastRootRef *core.Ref
	if ed != nil && ed.focus.ctrl != nil {
		toastRootRef = ed.focus.ctrl.rootRef(RegionToast)
		ed.focus.ctrl.registerCandidate(cx, RegionToast, actionRef, func() bool {
			cur, ok := ed.ActiveToast()
			return ok && cur.ActionLabel != ""
		})
		ed.focus.ctrl.registerCandidate(cx, RegionToast, dismissRef, func() bool {
			cur, ok := ed.ActiveToast()
			return ok && cur.canDismiss()
		})
	}

	toastRegion := core.View{
		Ref:        toastRootRef,
		TestID:     "editor-region-toast",
		Role:       core.RoleRegion,
		Label:      ed.T("region.toast"),
		LiveRegion: core.LiveRegionPolite,
		Focusable: func() bool {
			return ed != nil && ed.focus.ctrl != nil && ed.focus.ctrl.fallbackRegion.Get() == RegionToast
		},
		OnFocusIn: func() {
			if ed != nil && ed.focus.ctrl != nil {
				ed.focus.ctrl.onRegionFocusIn(RegionToast)
			}
		},
		OnFocusOut: func() {
			if ed != nil && ed.focus.ctrl != nil {
				ed.focus.ctrl.onRegionFocusOut(RegionToast)
			}
		},
		StyleFunc: func() core.Style {
			base := core.Style{
				CornerRadius: 12,
				MaxWidth:     core.Pct(100),
				FlexShrink:   1,
				MinWidth:     core.Pt(0),
			}
			if ed == nil || ed.focus.ctrl == nil {
				return base
			}
			return ed.focus.ctrl.regionOutlineStyle(RegionToast, base)
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				sk := structKey.Get()
				p := SpecPaletteFor(sk.dark)
				return specLiveToastCard(p, sk.kind, sk.actionLabel, sk.actionShortcut, sk.canDismiss, readToast, ed, actionRef, dismissRef)
			}),
		},
	}

	return core.View{
		TestID:        "editor-toast-motion",
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			MaxWidth:   core.Pct(100),
			FlexShrink: 1,
			MinWidth:   core.Pt(0),
			Animated:   motion.Style(),
		},
		Children: []core.Node{toastRegion},
	}
}

func toastProgressPercentText(t Toast) string {
	if t.Kind != ToastProgress || strings.Contains(t.Message, "%") {
		return ""
	}
	pct := int(math.Round(float64(clampToastProgress(t.Progress) * 100)))
	return fmt.Sprintf("%d%%", pct)
}

func specLiveToastCard(
	p SpecPalette,
	kind ToastKind,
	actionLabel, actionShortcut string,
	canDismiss bool,
	readToast func() (Toast, int),
	ed *Editor,
	actionRef, dismissRef *core.Ref,
) core.Node {
	bg, border, actionBg, actionBorder, shadowAlpha := specToastSurfaceTokens(p, kind)

	badgeBg := p.LineRaised
	badgeFg := p.TextMuted
	if kind == ToastWarning {
		badgeBg = p.Warning.Edge
		badgeFg = p.TextPrimary
	}

	children := []core.Node{
		specToastIcon(p, kind),
		core.Text{
			TestID: "editor-toast-message",
			ContentFunc: func() string {
				t, _ := readToast()
				return t.Message
			},
			MaxLines: 1,
			Style: core.Style{
				FontSize:   12.5,
				FontWeight: 400,
				Color:      p.TextSecondary,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
				Margin:     core.Edges{Right: 6},
			},
		},
	}

	if kind == ToastProgress {
		children = append(children, core.Show{
			When: func() bool {
				t, _ := readToast()
				return toastProgressPercentText(t) != ""
			},
			Then: func(cx *core.Scope) core.Node {
				return core.Text{
					TestID: "editor-toast-progress-pct",
					ContentFunc: func() string {
						t, _ := readToast()
						return toastProgressPercentText(t)
					},
					MaxLines: 1,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						FontWeight: 400,
						Color:      p.TextMuted,
						FlexShrink: 0,
						Margin:     core.Edges{Right: 4},
					},
				}
			},
		})
	}

	children = append(children, core.Show{
		When: func() bool {
			_, w := readToast()
			return w > 0
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "editor-toast-waiting",
				Style: core.Style{
					Height:         core.Pt(20),
					FlexShrink:     0,
					Padding:        core.Symmetric(0, 6),
					CornerRadius:   6,
					Background:     badgeBg,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.Text{
						ContentFunc: func() string {
							_, w := readToast()
							return fmt.Sprintf("+%d", w)
						},
						MaxLines: 1,
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11,
							FontWeight: 500,
							Color:      badgeFg,
						},
					},
				},
			}
		},
	})

	if actionLabel != "" {
		children = append(children, specToastActionButton(p, actionLabel, actionShortcut, actionBg, actionBorder, actionRef, func() {
			t, _ := readToast()
			if t.OnAction != nil {
				t.OnAction()
			}
		}))
	}

	if canDismiss {
		children = append(children, specToastDismissButton(p, dismissRef, func() {
			if ed == nil {
				return
			}
			wasFocused := ed.focus.ctrl != nil && ed.focus.ctrl.toastFocused != nil && ed.focus.ctrl.toastFocused.GetUntracked()
			ed.DismissToast()
			if wasFocused && !ed.HasActiveToast() && ed.focus.ctrl != nil {
				ed.focus.ctrl.restoreFocusFromToast()
			}
		}))
	}

	if kind == ToastProgress {
		children = append(children, core.View{
			TestID:        "editor-toast-progress-track",
			PointerEvents: core.PointerEventsNone,
			Style: core.Style{
				Position:     core.Absolute,
				Left:         core.Pt(12),
				Right:        core.Pt(12),
				Bottom:       core.Pt(0),
				Height:       core.Pt(2),
				CornerRadius: 1,
				Background:   p.LineRaised,
				Overflow:     core.Hidden,
			},
			Children: []core.Node{
				core.View{
					TestID: "editor-toast-progress-bar",
					StyleFunc: func() core.Style {
						t, _ := readToast()
						frac := clampToastProgress(t.Progress)
						return core.Style{
							Width:        core.Pct(frac * 100),
							Height:       core.Pt(2),
							CornerRadius: 1,
							Background:   p.TextMuted,
						}
					},
				},
			},
		})
	}

	return core.View{
		TestID: "editor-toast",
		OnMouseEnter: func(core.MouseEvent) {
			if ed != nil {
				ed.setToastHovered(true)
			}
		},
		OnMouseLeave: func(core.MouseEvent) {
			if ed != nil {
				ed.setToastHovered(false)
			}
		},
		OnTouch: func(ev core.TouchEvent) {
			if ed == nil {
				return
			}
			switch ev.Phase {
			case core.TouchStart:
				ed.setToastTouched(true)
			case core.TouchEnd, core.TouchCancel:
				ed.setToastTouched(false)
			}
		},
		Style: core.Style{
			Position:     core.Relative,
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          8,
			MaxWidth:     core.Pct(100),
			Height:       core.Pt(40),
			Padding:      core.Edges{Left: 12, Right: 6},
			CornerRadius: 12,
			Background:   bg,
			BorderWidth:  1,
			BorderColor:  border,
			Overflow:     core.Hidden,
			Shadows: []core.BoxShadow{
				ui.Shadow(0, 10, 28, 0, ui.Black(shadowAlpha)),
			},
		},
		Children: children,
	}
}

func specToastSurfaceTokens(p SpecPalette, kind ToastKind) (bg, border, actionBg, actionBorder core.Color, shadowAlpha float32) {
	bg = p.Raised
	border = p.LineRaised
	actionBg = p.LineRaised
	actionBorder = core.Transparent
	if kind == ToastWarning {
		bg = p.Warning.Fill
		border = p.Warning.Edge
		actionBg = p.Warning.Fill
		actionBorder = p.Warning.Edge
	}
	shadowAlpha = 0.42
	if !p.Dark {
		shadowAlpha = 0.14
	}
	return bg, border, actionBg, actionBorder, shadowAlpha
}

func specToastActionButton(p SpecPalette, label, shortcut string, bg, border core.Color, ref *core.Ref, onPress func()) core.Node {
	actionChildren := []core.Node{
		core.Text{
			Content:  label,
			MaxLines: 1,
			Style: core.Style{
				FontSize:   12.5,
				FontWeight: 500,
				Color:      p.TextPrimary,
			},
		},
	}
	if shortcut != "" {
		actionChildren = append(actionChildren, core.Text{
			TestID:   "editor-toast-action-shortcut",
			Content:  shortcut,
			MaxLines: 1,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11,
				FontWeight: 400,
				Color:      p.TextFaint,
			},
		})
	}
	actionStyle := core.Style{
		Direction:      core.Row,
		AlignItems:     core.AlignCenter,
		FlexShrink:     0,
		Gap:            6,
		Height:         core.Pt(28),
		Padding:        core.Symmetric(0, 10),
		CornerRadius:   8,
		Background:     bg,
		FocusVisible:   p.FocusRing(2),
		JustifyContent: core.JustifyCenter,
	}
	if border != core.Transparent {
		actionStyle.BorderWidth = 1
		actionStyle.BorderColor = border
	}
	return core.View{
		Ref:       ref,
		TestID:    "editor-toast-action",
		Role:      core.RoleButton,
		Label:     label,
		Focusable: func() bool { return true },
		Style:     actionStyle,
		OnPress: func(core.PressEvent) {
			if onPress != nil {
				onPress()
			}
		},
		Children: actionChildren,
	}
}

func specToastDismissButton(p SpecPalette, ref *core.Ref, onDismiss func()) core.Node {
	return core.View{
		Ref:       ref,
		TestID:    "editor-toast-dismiss",
		Role:      core.RoleButton,
		Label:     "Dismiss",
		Focusable: func() bool { return true },
		Style: core.Style{
			Width:          core.Pt(28),
			Height:         core.Pt(28),
			FlexShrink:     0,
			CornerRadius:   6,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			FocusVisible:   p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			if onDismiss != nil {
				onDismiss()
			}
		},
		Children: []core.Node{
			core.Canvas{
				Style: core.Style{Width: core.Pt(14), Height: core.Pt(14)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.SetStrokeColor(p.TextFaint).SetLineWidth(1.6).SetLineCap(core.CapRound)
					d.BeginPath().MoveTo(3.5, 3.5).LineTo(10.5, 10.5).MoveTo(10.5, 3.5).LineTo(3.5, 10.5).Stroke()
				},
			},
		},
	}
}

// SpecToastCard renders a single Spec toast pill (40 pt tall, 12 pt radius, single line, semantic icon,
// optional waiting count badge, optional action button with shortcut, optional dismiss button) using
// exclusively tokens from p.
func SpecToastCard(p SpecPalette, t Toast, waiting int, onDismiss func()) core.Node {
	return specToastCardWithRefs(p, t, waiting, onDismiss, nil, nil)
}

func specToastCardWithRefs(p SpecPalette, t Toast, waiting int, onDismiss func(), actionRef, dismissRef *core.Ref) core.Node {
	bg, border, actionBg, actionBorder, shadowAlpha := specToastSurfaceTokens(p, t.Kind)

	children := []core.Node{
		specToastIcon(p, t.Kind),
		core.Text{
			TestID:   "editor-toast-message",
			Content:  t.Message,
			MaxLines: 1,
			Style: core.Style{
				FontSize:   12.5,
				FontWeight: 400,
				Color:      p.TextSecondary,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
				Margin:     core.Edges{Right: 6},
			},
		},
	}

	if pct := toastProgressPercentText(t); pct != "" {
		children = append(children, core.Text{
			TestID:   "editor-toast-progress-pct",
			Content:  pct,
			MaxLines: 1,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				FontWeight: 400,
				Color:      p.TextMuted,
				FlexShrink: 0,
				Margin:     core.Edges{Right: 4},
			},
		})
	}

	if waiting > 0 {
		badgeBg := p.LineRaised
		if t.Kind == ToastWarning {
			badgeBg = p.Warning.Edge
		}
		badgeFg := p.TextMuted
		if t.Kind == ToastWarning {
			badgeFg = p.TextPrimary
		}
		children = append(children, core.View{
			TestID: "editor-toast-waiting",
			Style: core.Style{
				Height:         core.Pt(20),
				FlexShrink:     0,
				Padding:        core.Symmetric(0, 6),
				CornerRadius:   6,
				Background:     badgeBg,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content:  fmt.Sprintf("+%d", waiting),
					MaxLines: 1,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11,
						FontWeight: 500,
						Color:      badgeFg,
					},
				},
			},
		})
	}

	if t.ActionLabel != "" {
		children = append(children, specToastActionButton(p, t.ActionLabel, t.ActionShortcut, actionBg, actionBorder, actionRef, t.OnAction))
	}

	if t.canDismiss() {
		children = append(children, specToastDismissButton(p, dismissRef, onDismiss))
	}

	if t.Kind == ToastProgress {
		frac := clampToastProgress(t.Progress)
		children = append(children, core.View{
			TestID:        "editor-toast-progress-track",
			PointerEvents: core.PointerEventsNone,
			Style: core.Style{
				Position:     core.Absolute,
				Left:         core.Pt(12),
				Right:        core.Pt(12),
				Bottom:       core.Pt(0),
				Height:       core.Pt(2),
				CornerRadius: 1,
				Background:   p.LineRaised,
				Overflow:     core.Hidden,
			},
			Children: []core.Node{
				core.View{
					TestID: "editor-toast-progress-bar",
					Style: core.Style{
						Width:        core.Pct(frac * 100),
						Height:       core.Pt(2),
						CornerRadius: 1,
						Background:   p.TextMuted,
					},
				},
			},
		})
	}

	return core.View{
		TestID: "editor-toast",
		Style: core.Style{
			Position:     core.Relative,
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          8,
			MaxWidth:     core.Pct(100),
			Height:       core.Pt(40),
			Padding:      core.Edges{Left: 12, Right: 6},
			CornerRadius: 12,
			Background:   bg,
			BorderWidth:  1,
			BorderColor:  border,
			Overflow:     core.Hidden,
			Shadows: []core.BoxShadow{
				ui.Shadow(0, 10, 28, 0, ui.Black(shadowAlpha)),
			},
		},
		Children: children,
	}
}

func specToastIcon(p SpecPalette, kind ToastKind) core.Node {
	return core.Canvas{
		TestID: "editor-toast-icon-" + string(kind),
		Style:  core.Style{Width: core.Pt(16), Height: core.Pt(16), FlexShrink: 0},
		Draw: func(d *core.Drawing, _ core.Size) {
			d.SetLineWidth(1.6).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
			switch kind {
			case ToastSuccess:
				d.SetStrokeColor(p.Added.Fg)
				d.BeginPath().Ellipse(8, 8, 6.2, 6.2).Stroke()
				d.BeginPath().MoveTo(5.2, 8.3).LineTo(7.1, 10.2).LineTo(11.1, 6.1).Stroke()
			case ToastWarning:
				d.SetStrokeColor(p.Warning.Fg)
				d.BeginPath().MoveTo(8, 2.2).LineTo(14.2, 13.4).LineTo(1.8, 13.4).ClosePath().Stroke()
				d.BeginPath().MoveTo(8, 6.4).LineTo(8, 9.6).Stroke()
				d.SetFillColor(p.Warning.Fg).BeginPath().Ellipse(8, 11.4, 0.85, 0.85).Fill()
			case ToastError:
				d.SetStrokeColor(p.Removed.Fg)
				d.BeginPath().Ellipse(8, 8, 6.2, 6.2).Stroke()
				d.BeginPath().MoveTo(8, 4.8).LineTo(8, 8.8).Stroke()
				d.SetFillColor(p.Removed.Fg).BeginPath().Ellipse(8, 11.2, 0.85, 0.85).Fill()
			case ToastProgress:
				d.SetStrokeColor(p.LineRaised)
				d.BeginPath().Ellipse(8, 8, 6.2, 6.2).Stroke()
				d.SetStrokeColor(p.TextMuted)
				d.BeginPath().Arc(8, 8, 6.2, -float32(math.Pi/2), 0, false).Stroke()
			default: // ToastInfo
				d.SetStrokeColor(p.TextMuted)
				d.BeginPath().Ellipse(8, 8, 6.2, 6.2).Stroke()
				d.BeginPath().MoveTo(8, 7.2).LineTo(8, 11.2).Stroke()
				d.SetFillColor(p.TextMuted).BeginPath().Ellipse(8, 5.1, 0.85, 0.85).Fill()
			}
		},
	}
}

// viewportOverlayLegendView renders the Spec overlay legend in the top-left corner of every
// edit-mode viewport (both 2D and 3D) as six accessible role="switch" toggles — one per kind
// (Selection, Agent working, Added, Changed, Removed, Problem) with its exact Spec line style
// and second cue. Turning a row off hides that kind and saves the choice per person in EditorSettings.
func viewportOverlayLegendView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type legendStateKey struct {
		visible  bool
		dark     bool
		viewMode ViewMode
		lang     string
	}
	keyMemo := core.NewMemo(cx, func() legendStateKey {
		_ = rev.Get()
		if ed == nil || ed.Playing() || ed.HistoryPreviewActive() || !ed.OverlayLegendOpen() {
			return legendStateKey{}
		}
		if ed.ViewMode == ViewMode2D && ed.SketchActive() && !ed.sketchBlockedByScreen() {
			return legendStateKey{}
		}
		return legendStateKey{
			visible:  true,
			dark:     ed.SpecPalette(cx).Dark,
			viewMode: ed.ViewMode,
			lang:     ed.ResolvedLanguage(),
		}
	})

	return core.Show{
		When: func() bool {
			return keyMemo.Get().visible
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				k := keyMemo.Get()
				if !k.visible {
					return core.View{}
				}
				p := SpecPaletteFor(k.dark)
				leftPt := core.Pt(10)
				topPt := core.Pt(8)
				if k.viewMode == ViewMode3D {
					// Sit as a compact 1×6 strip at y ∈ [120, 148] beneath view3d-bar + view3d-help
					// and the top-right orientation widget, clear of top-view ortho pick targets (y≈110),
					// upper-left 3D plugin tool drags (y=100), and middle-left orbit/pan drags (y=170).
					leftPt = core.Pt(16)
					topPt = core.Pt(120)
				}

				legendLabel := func(kind OverlayKind) string {
					switch kind {
					case OverlayKindSelection:
						return ed.T("overlays.legend.selection")
					case OverlayKindAgent:
						return ed.T("overlays.legend.agent")
					case OverlayKindAdded:
						return ed.T("overlays.legend.added")
					case OverlayKindChanged:
						return ed.T("overlays.legend.changed")
					case OverlayKindRemoved:
						return ed.T("overlays.legend.removed")
					case OverlayKindProblem:
						return ed.T("overlays.legend.problem")
					default:
						return string(kind)
					}
				}

				legendRole := func(kind OverlayKind) SpecRole {
					switch kind {
					case OverlayKindSelection:
						return p.Accent
					case OverlayKindAgent:
						return p.Agent
					case OverlayKindAdded:
						return p.Added
					case OverlayKindChanged:
						return p.Changed
					case OverlayKindRemoved:
						return p.Removed
					case OverlayKindProblem:
						return p.Problem()
					default:
						return p.Accent
					}
				}

				legendRow := func(kind OverlayKind) core.Node {
					kindCopy := kind
					role := legendRole(kindCopy)
					label := legendLabel(kindCopy)

					swatch := core.Canvas{
						Style: core.Style{Width: core.Pt(18), Height: core.Pt(12), FlexShrink: 0},
						Draw: func(d *core.Drawing, _ core.Size) {
							_ = rev.Get()
							swatchAlpha := float32(1)
							if !ed.OverlayKindVisible(kindCopy) {
								swatchAlpha = 0.32
							}
							d.Save()
							edge := ui.Alpha(role.Edge, swatchAlpha)
							fg := ui.Alpha(role.Fg, swatchAlpha)
							switch kindCopy {
							case OverlayKindSelection:
								// 1.5 px solid accent line + two corner handle squares.
								d.SetStrokeColor(edge).SetLineWidth(1.5)
								d.BeginPath().MoveTo(3, 6).LineTo(15, 6).Stroke()
								hf := ui.Alpha(specHandleFill(p), swatchAlpha)
								for _, hx := range [2]float32{1, 13} {
									d.SetFillColor(hf).BeginPath().RoundRect(hx, 4, 4, 4, 1).Fill()
									d.SetStrokeColor(edge).SetLineWidth(1.1).BeginPath().RoundRect(hx, 4, 4, 4, 1).Stroke()
								}
							case OverlayKindAgent:
								// 1.5 px solid agent violet line + 4-point agent sparkle glyph.
								d.SetStrokeColor(edge).SetLineWidth(1.5)
								d.BeginPath().MoveTo(1, 6).LineTo(9.5, 6).Stroke()
								drawAgentSparkleGlyph(d, 14, 6, 3.6, fg)
							case OverlayKindAdded:
								// 1.5 px dashed added green line + "+" cue.
								d.SetStrokeColor(edge).SetLineWidth(1.5).SetLineDash(3, 2)
								d.BeginPath().MoveTo(1, 6).LineTo(10, 6).Stroke()
								d.SetLineDash()
								d.SetStrokeColor(fg).SetLineWidth(1.4).SetLineCap(core.CapRound)
								d.BeginPath().MoveTo(12, 6).LineTo(17, 6).MoveTo(14.5, 3.5).LineTo(14.5, 8.5).Stroke()
							case OverlayKindChanged:
								// 1.5 px dashed changed amber line + "→" cue.
								d.SetStrokeColor(edge).SetLineWidth(1.5).SetLineDash(3, 2)
								d.BeginPath().MoveTo(1, 6).LineTo(9.5, 6).Stroke()
								d.SetLineDash()
								d.SetStrokeColor(fg).SetLineWidth(1.4).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
								d.BeginPath().MoveTo(11.5, 6).LineTo(17, 6).MoveTo(15, 4).LineTo(17, 6).LineTo(15, 8).Stroke()
							case OverlayKindRemoved:
								// 1.5 px solid removed red frame around a 25% grey ghost + strikethrough cue.
								d.SetFillColor(ui.Alpha(p.TextFaint, 0.28*swatchAlpha)).BeginPath().RoundRect(2, 2.5, 8, 7, 1.5).Fill()
								d.SetStrokeColor(edge).SetLineWidth(1.5).BeginPath().RoundRect(1, 1.5, 10, 9, 2).Stroke()
								d.SetStrokeColor(fg).SetLineWidth(1.3).SetLineCap(core.CapRound)
								d.BeginPath().MoveTo(12.5, 6).LineTo(17.5, 6).Stroke()
							case OverlayKindProblem:
								// 2 px solid problem red line + filled alert triangle badge.
								d.SetStrokeColor(edge).SetLineWidth(2)
								d.BeginPath().MoveTo(1, 6).LineTo(7, 6).Stroke()
								d.SetFillColor(fg).BeginPath().RoundRect(8.5, 1, 9.5, 9.5, 4.75).Fill()
								onFill := ui.Alpha(role.OnFill, swatchAlpha)
								d.SetStrokeColor(onFill).SetLineWidth(1.1).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
								d.BeginPath().MoveTo(13.25, 2.8).LineTo(16, 8.2).LineTo(10.5, 8.2).ClosePath().Stroke()
								d.BeginPath().MoveTo(13.25, 4.8).LineTo(13.25, 6.4).Stroke()
							}
							d.Restore()
						},
					}

					return core.View{
						TestID:    "legend-" + string(kindCopy),
						Role:      core.RoleSwitch,
						Label:     label,
						Checked:   func() bool { _ = rev.Get(); return ed.OverlayKindVisible(kindCopy) },
						Focusable: func() bool { return true },
						OnPress: func(core.PressEvent) {
							ed.report(ed.ToggleOverlayKind(kindCopy))
						},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							chipBg := ui.Alpha(role.Fill, 0.7)
							chipBorder := ui.Alpha(role.Edge, 0.45)
							if !ed.OverlayKindVisible(kindCopy) {
								chipBg = core.Transparent
								chipBorder = core.Transparent
							}
							return core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          3,
								Height:       core.Pt(20),
								Padding:      core.Symmetric(0, 4),
								CornerRadius: 5,
								Background:   chipBg,
								BorderWidth:  1,
								BorderColor:  chipBorder,
								FlexShrink:   0,
								FocusVisible: p.FocusRing(2),
							}
						},
						Children: []core.Node{
							swatch,
							core.Text{
								Content:  label,
								MaxLines: 1,
								StyleFunc: func() core.Style {
									_ = rev.Get()
									textColor := p.TextSecondary
									if !ed.OverlayKindVisible(kindCopy) {
										textColor = p.TextFaint
									}
									return core.Style{
										FontSize:   10,
										FontWeight: 500,
										Color:      textColor,
									}
								},
							},
						},
					}
				}

				children := make([]core.Node, 0, 6)
				for _, kind := range OverlayLegendKinds() {
					children = append(children, legendRow(kind))
				}

				return core.View{
					TestID: "viewport-overlay-legend",
					Role:   core.RoleGroup,
					Label:  ed.T("overlays.legendTitle"),
					Style: core.Style{
						Position:     core.Absolute,
						Left:         leftPt,
						Top:          topPt,
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          2,
						Padding:      core.Symmetric(3, 4),
						CornerRadius: 8,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: children,
				}
			})
		},
	}
}

// SpecOverlayLegendView renders the complete Spec.dc.html Overlay Legend board
// (Selection, Agent working, Added, Changed, Removed, Problem, and Focused change in Review)
// on the active theme's viewport surround, drawing every card through DrawObjectOverlay using
// exclusively SpecPalette tokens so Dark and Light appearances can be inspected side by side.
func SpecOverlayLegendView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = rev.Get()
		p := ed.SpecPalette(cx)
		modeLabel := "Light theme"
		if p.Dark {
			modeLabel = "Dark theme"
		}

		drawVPBackdrop := func(d *core.Drawing, sz core.Size) {
			w, h := float32(sz.Width), float32(sz.Height)
			d.SetFillColor(p.Viewport).FillRect(0, 0, w, h)
			d.SetFillColor(p.ViewportDot)
			for y := float32(8); y < h; y += 16 {
				for x := float32(8); x < w; x += 16 {
					d.FillRect(x, y, 1.2, 1.2)
				}
			}
			floorY := h - 34
			d.SetFillColor(p.Inset).FillRect(0, floorY, w, 34)
			d.SetFillColor(p.LineRaised).FillRect(0, floorY, w, 3)
		}

		card := func(testID, title, desc string, drawPreview func(d *core.Drawing, sz core.Size)) core.Node {
			return core.View{
				TestID: testID,
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Gap:       8,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Position:     core.Relative,
							Height:       core.Pt(164),
							CornerRadius: 12,
							Background:   p.Viewport,
							BorderWidth:  1,
							BorderColor:  p.Line,
							Overflow:     core.Hidden,
						},
						Children: []core.Node{
							core.Canvas{
								Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
								Draw: func(d *core.Drawing, sz core.Size) {
									drawVPBackdrop(d, sz)
									drawPreview(d, sz)
								},
							},
						},
					},
					core.Text{
						Content: title,
						Style: core.Style{
							FontSize:   14,
							FontWeight: 600,
							Color:      p.TextPrimary,
						},
					},
					core.Text{
						Content: desc,
						Style: core.Style{
							FontSize: 12.5,
							Color:    p.TextMuted,
						},
					},
				},
			}
		}

		selectionCard := card(
			"spec-legend-selection",
			"Selection",
			"accent · 1.5 px solid · four handles and a name tag.",
			func(d *core.Drawing, sz core.Size) {
				cx := float32(sz.Width) / 2
				fy := float32(sz.Height) - 34
				bounds := core.Bounds{Origin: core.Point{X: core.Points(cx - 12), Y: core.Points(fy - 34)}, Size: core.Size{Width: 24, Height: 34}}
				d.SetFillColor(p.Changed.Fg).BeginPath().RoundRect(cx-12, fy-34, 24, 34, 4).Fill()
				DrawObjectOverlay(d, p, ObjectOverlay{Kind: OverlayKindSelection, Label: "Knight", Bounds: bounds, CanvasSize: sz})
			},
		)

		agentCard := card(
			"spec-legend-agent",
			"Agent working",
			"agent · 1.5 px solid · pulsing violet · agent pill.",
			func(d *core.Drawing, sz core.Size) {
				cx := float32(sz.Width) / 2
				fy := float32(sz.Height) - 34
				bounds := core.Bounds{Origin: core.Point{X: core.Points(cx - 15), Y: core.Points(fy - 22)}, Size: core.Size{Width: 30, Height: 22}}
				d.SetFillColor(ui.Alpha(p.Agent.Fg, 0.28)).BeginPath().RoundRect(cx-15, fy-22, 30, 22, 8).Fill()
				DrawObjectOverlay(d, p, ObjectOverlay{Kind: OverlayKindAgent, Label: "placing slime_02", Bounds: bounds, CanvasSize: sz})
			},
		)

		addedCard := card(
			"spec-legend-added",
			"Added",
			"added · 1.5 px dashed · “+ name” pill.",
			func(d *core.Drawing, sz core.Size) {
				cx := float32(sz.Width) / 2
				fy := float32(sz.Height) - 34
				bounds := core.Bounds{Origin: core.Point{X: core.Points(cx - 15), Y: core.Points(fy - 22)}, Size: core.Size{Width: 30, Height: 22}}
				d.SetFillColor(ui.Alpha(p.Added.Fg, 0.28)).BeginPath().RoundRect(cx-15, fy-22, 30, 22, 8).Fill()
				DrawObjectOverlay(d, p, ObjectOverlay{Kind: OverlayKindAdded, Label: "slime_03", Bounds: bounds, CanvasSize: sz})
			},
		)

		changedCard := card(
			"spec-legend-changed",
			"Changed",
			"changed · 1.5 px dashed · field old → new pill.",
			func(d *core.Drawing, sz core.Size) {
				cx := float32(sz.Width) / 2
				fy := float32(sz.Height) - 34
				bounds := core.Bounds{Origin: core.Point{X: core.Points(cx - 12), Y: core.Points(fy - 34)}, Size: core.Size{Width: 24, Height: 34}}
				d.SetFillColor(p.Changed.Fg).BeginPath().RoundRect(cx-12, fy-34, 24, 34, 4).Fill()
				DrawObjectOverlay(d, p, ObjectOverlay{Kind: OverlayKindChanged, Label: "z 10 → 12", Bounds: bounds, CanvasSize: sz})
			},
		)

		removedCard := card(
			"spec-legend-removed",
			"Removed",
			"removed · 1.5 px solid around 25% grey ghost · struck-through name.",
			func(d *core.Drawing, sz core.Size) {
				cx := float32(sz.Width) / 2
				fy := float32(sz.Height) - 34
				bounds := core.Bounds{Origin: core.Point{X: core.Points(cx - 10), Y: core.Points(fy - 24)}, Size: core.Size{Width: 20, Height: 20}}
				DrawObjectOverlay(d, p, ObjectOverlay{Kind: OverlayKindRemoved, Label: "coin_04", Bounds: bounds, CanvasSize: sz})
			},
		)

		problemCard := card(
			"spec-legend-problem",
			"Problem",
			"problem · 2 px solid on live object · filled alert pill.",
			func(d *core.Drawing, sz core.Size) {
				cx := float32(sz.Width) / 2
				fy := float32(sz.Height) - 34
				bounds := core.Bounds{Origin: core.Point{X: core.Points(cx - 14), Y: core.Points(fy - 38)}, Size: core.Size{Width: 28, Height: 38}}
				d.SetFillColor(p.TextMuted).BeginPath().RoundRect(cx-14, fy-38, 28, 38, 4).Fill()
				DrawObjectOverlay(d, p, ObjectOverlay{Kind: OverlayKindProblem, Label: "door: lever_02 missing", Bounds: bounds, CanvasSize: sz})
			},
		)

		focusedCard := card(
			"spec-legend-focused",
			"Focused change in Review",
			"own color & dash · 2.5 px · outlined pill · never accent.",
			func(d *core.Drawing, sz core.Size) {
				cx := float32(sz.Width) / 2
				fy := float32(sz.Height) - 34
				leftBounds := core.Bounds{Origin: core.Point{X: core.Points(cx - 56), Y: core.Points(fy - 22)}, Size: core.Size{Width: 30, Height: 22}}
				d.SetFillColor(ui.Alpha(p.Added.Fg, 0.28)).BeginPath().RoundRect(cx-56, fy-22, 30, 22, 8).Fill()
				DrawObjectOverlay(d, p, ObjectOverlay{Kind: OverlayKindAdded, Bounds: leftBounds, CanvasSize: sz})

				rightBounds := core.Bounds{Origin: core.Point{X: core.Points(cx + 12), Y: core.Points(fy - 22)}, Size: core.Size{Width: 30, Height: 22}}
				d.SetFillColor(ui.Alpha(p.Added.Fg, 0.45)).BeginPath().RoundRect(cx+12, fy-22, 30, 22, 8).Fill()
				DrawObjectOverlay(d, p, ObjectOverlay{Kind: OverlayKindFocusedChange, ChangeKind: OverlayKindAdded, Label: "slime_02", Bounds: rightBounds, CanvasSize: sz})
			},
		)

		pairedCard := card(
			"spec-legend-paired",
			"Selection + Agent",
			"selection keeps handles · agent outline sits 3 px further out.",
			func(d *core.Drawing, sz core.Size) {
				cx := float32(sz.Width) / 2
				fy := float32(sz.Height) - 34
				bounds := core.Bounds{Origin: core.Point{X: core.Points(cx - 12), Y: core.Points(fy - 34)}, Size: core.Size{Width: 24, Height: 34}}
				d.SetFillColor(p.Changed.Fg).BeginPath().RoundRect(cx-12, fy-34, 24, 34, 4).Fill()
				DrawObjectOverlay(d, p, ObjectOverlay{Kind: OverlayKindSelection, Label: "Knight", Bounds: bounds, CanvasSize: sz})
				DrawObjectOverlay(d, p, ObjectOverlay{Kind: OverlayKindAgent, Label: "tuning jump", Bounds: bounds, CanvasSize: sz, PairedWithSelection: true, PillStackOffset: 24})
			},
		)

		return core.View{
			TestID: "spec-overlay-legend-board",
			Style: core.Style{
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Padding:    core.All(24),
				Gap:        20,
				Background: p.Bg,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{Gap: 4},
							Children: []core.Node{
								core.Text{
									Content: "Overlay legend",
									Style: core.Style{
										FontSize:   20,
										FontWeight: 600,
										Color:      p.TextPrimary,
									},
								},
								core.Text{
									Content: "One meaning per color and line. Every overlay also has a second cue: handles, a label pill, a ghost or a pulse.",
									Style: core.Style{
										FontSize: 12.5,
										Color:    p.TextFaint,
									},
								},
							},
						},
						core.View{
							Style: core.Style{
								Height:         core.Pt(26),
								Padding:        core.Symmetric(0, 10),
								CornerRadius:   13,
								Background:     p.Raised,
								BorderWidth:    1,
								BorderColor:    p.LineRaised,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							},
							Children: []core.Node{
								core.Text{
									Content: modeLabel,
									Style: core.Style{
										FontSize:   11.5,
										FontWeight: 600,
										Color:      p.TextSecondary,
									},
								},
							},
						},
					},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
						Gap:       16,
					},
					Children: []core.Node{selectionCard, agentCard, addedCard, changedCard},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
						Gap:       16,
					},
					Children: []core.Node{removedCard, problemCard, focusedCard, pairedCard},
				},
			},
		}
	})
}
