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

// OverlayLegendOpen reports whether the compact viewport overlay legend is visible.
func (e *Editor) OverlayLegendOpen() bool {
	return e != nil && e.overlayLegendOpen
}

// SetOverlayLegendOpen shows or hides the compact viewport overlay legend in the viewport top-left corner.
func (e *Editor) SetOverlayLegendOpen(open bool) {
	if e == nil || e.overlayLegendOpen == open {
		return
	}
	e.overlayLegendOpen = open
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

// viewportOverlayLegendView renders the compact interactive Spec overlay legend in the top-left of the
// viewport when Editor.OverlayLegendOpen is true or live agent overlays are active (matching Spec.dc.html's
// line-style legend). Each row toggles its overlay kind on or off and persists the choice per user in
// EditorSettings.HiddenOverlayKinds.
func viewportOverlayLegendView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.OverlayLegendOpen() || ed.HasAgentLiveOverlays()
		},
		Then: func(cx *core.Scope) core.Node {
			p := ed.SpecPalette(cx)
			row := func(testID string, kind OverlayLegendKind, labelKey string, col core.Color, dashed bool) core.Node {
				k := kind
				isOn := func() bool {
					_ = rev.Get()
					return ed.OverlayLegendKindEnabled(k)
				}
				return core.View{
					TestID:    testID,
					Role:      core.RoleSwitch,
					LabelFunc: func() string { _ = rev.Get(); return ed.T(labelKey) },
					Checked:   isOn,
					Focusable: func() bool { return true },
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          8,
						Height:       core.Pt(28),
						Padding:      core.Symmetric(0, 6),
						CornerRadius: 6,
						FocusVisible: p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.ToggleOverlayLegendKind(k))
					},
					Children: []core.Node{
						core.Canvas{
							Style: core.Style{Width: core.Pt(18), Height: core.Pt(10), FlexShrink: 0},
							Draw: func(d *core.Drawing, _ core.Size) {
								c := col
								if !ed.OverlayLegendKindEnabled(k) {
									c = ui.Alpha(p.TextFaint, 0.45)
								}
								d.SetStrokeColor(c).SetLineWidth(1.8)
								if dashed {
									d.SetLineDash(4, 2.5)
								}
								d.BeginPath().MoveTo(1, 5).LineTo(17, 5).Stroke()
							},
						},
						core.Text{
							ContentFunc: func() string {
								_ = rev.Get()
								return ed.T(labelKey)
							},
							Style: core.Style{
								FontSize:   11.5,
								FontWeight: 500,
								Color:      p.TextSecondary,
							},
							StyleFunc: func() core.Style {
								if !isOn() {
									return core.Style{
										Color:          p.TextFaint,
										TextDecoration: core.TextDecorationLineThrough,
									}
								}
								return core.Style{Color: p.TextSecondary}
							},
						},
					},
				}
			}
			return core.View{
				TestID: "viewport-overlay-legend",
				Style: core.Style{
					Position:     core.Absolute,
					Left:         core.Pt(12),
					Top:          core.Pt(12),
					Padding:      core.Symmetric(8, 10),
					Gap:          0,
					CornerRadius: 10,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: []core.Node{
					core.Text{
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.T("agent.legendTitle")
						},
						Style: core.Style{
							FontSize:      10.5,
							FontWeight:    600,
							LetterSpacing: 0.6,
							Color:         p.TextFaint,
							Padding:       core.Edges{Left: 6, Right: 6, Bottom: 4},
						},
					},
					row("legend-selection", OverlayLegendSelection, "agent.legendSelection", p.Accent.Edge, false),
					row("legend-agent", OverlayLegendAgent, "agent.legendAgent", p.Agent.Edge, false),
					row("legend-added", OverlayLegendAdded, "agent.legendAdded", p.Added.Edge, true),
					row("legend-changed", OverlayLegendChanged, "agent.legendChanged", p.Changed.Edge, true),
					row("legend-removed", OverlayLegendRemoved, "agent.legendRemoved", p.Removed.Edge, false),
					row("legend-problem", OverlayLegendProblem, "agent.legendProblem", p.Problem().Edge, false),
				},
			}
		},
	}
}

// SpecOverlayLegendView renders the complete 6-card Spec.dc.html Overlay Legend board
// (Selection, Agent working, Added, Changed, Removed, Problem) on the active theme's viewport surround,
// using exclusively SpecPalette tokens so every outline (>= 3:1) and pill label (>= 4.5:1) can be
// inspected side by side in Dark and Light appearances.
func SpecOverlayLegendView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = rev.Get()
		p := ed.SpecPalette(cx)
		modeLabel := "Light theme"
		if p.Dark {
			modeLabel = "Dark theme"
		}

		card := func(testID, title, desc string, preview core.Node) core.Node {
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
						Children: []core.Node{preview},
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

		drawVPBackdrop := func(d *core.Drawing, sz core.Size) {
			w, h := float32(sz.Width), float32(sz.Height)
			d.SetFillColor(p.Viewport).FillRect(0, 0, w, h)
			d.SetFillColor(p.ViewportDot)
			for y := float32(8); y < h; y += 16 {
				for x := float32(8); x < w; x += 16 {
					d.FillRect(x, y, 1.2, 1.2)
				}
			}
			// Floor strip using Spec Inset / LineRaised tokens.
			floorY := h - 34
			d.SetFillColor(p.Inset).FillRect(0, floorY, w, 34)
			d.SetFillColor(p.LineRaised).FillRect(0, floorY, w, 3)
		}

		pillNode := func(testID, label string, fg, bg, border core.Color, strikePrefix string) core.Node {
			kids := []core.Node{}
			if strikePrefix != "" {
				kids = append(kids, core.Text{
					Content: strikePrefix,
					Style: core.Style{
						FontFamily:     core.FontMonospace,
						FontSize:       11,
						FontWeight:     500,
						Color:          fg,
						TextDecoration: core.TextDecorationLineThrough,
					},
				})
			}
			kids = append(kids, core.Text{
				Content: label,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11,
					FontWeight: 600,
					Color:      fg,
				},
			})
			st := core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				Gap:            6,
				Height:         core.Pt(22),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   11,
				Background:     bg,
				JustifyContent: core.JustifyCenter,
			}
			if border != core.Transparent {
				st.BorderWidth = 1
				st.BorderColor = border
			}
			return core.View{TestID: testID, Style: st, Children: kids}
		}

		selectionCard := card(
			"spec-legend-selection",
			"Selection",
			"accent · 1.5 px solid · four handles and a name tag.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(p.Changed.Fg).BeginPath().RoundRect(cx-12, fy-34, 24, 34, 4).Fill()
							d.SetStrokeColor(p.Accent.Edge).SetLineWidth(1.5).BeginPath().RoundRect(cx-18, fy-40, 36, 46, 6).Stroke()
							for _, pt := range [4][2]float32{{cx - 18, fy - 40}, {cx + 18, fy - 40}, {cx - 18, fy + 6}, {cx + 18, fy + 6}} {
								d.SetFillColor(p.Raised).FillRect(pt[0]-4, pt[1]-4, 8, 8)
								d.SetStrokeColor(p.Accent.Edge).SetLineWidth(1.5).StrokeRect(pt[0]-4, pt[1]-4, 8, 8)
							}
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(56),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.View{
								TestID: "spec-legend-selection-tag",
								Style: core.Style{
									Height:         core.Pt(20),
									Padding:        core.Symmetric(0, 8),
									CornerRadius:   5,
									Background:     p.Accent.Fg,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
								},
								Children: []core.Node{
									core.Text{
										Content: "Knight",
										Style: core.Style{
											FontSize:   11,
											FontWeight: 600,
											Color:      p.Accent.OnFill,
										},
									},
								},
							},
						},
					},
				},
			},
		)

		agentCard := card(
			"spec-legend-agent",
			"Agent working",
			"agent · 1.5 px solid · pulsing violet · agent pill.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(ui.Alpha(p.Agent.Fg, 0.28)).BeginPath().RoundRect(cx-15, fy-22, 30, 22, 8).Fill()
							d.SetStrokeColor(p.Agent.Edge).SetLineWidth(1.5).BeginPath().RoundRect(cx-19, fy-26, 38, 30, 10).Stroke()
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(68),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.View{
								TestID: "spec-legend-agent-pill",
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          6,
									Height:       core.Pt(22),
									Padding:      core.Symmetric(0, 8),
									CornerRadius: 11,
									Background:   p.PillBg,
									BorderWidth:  1,
									BorderColor:  p.Agent.Edge,
								},
								Children: []core.Node{
									core.Canvas{
										Style: core.Style{Width: core.Pt(10), Height: core.Pt(10)},
										Draw: func(d *core.Drawing, _ core.Size) {
											d.SetFillColor(p.Agent.Fg).BeginPath().MoveTo(5, 0.5).LineTo(6.6, 3.4).LineTo(9.5, 5).LineTo(6.6, 6.6).LineTo(5, 9.5).LineTo(3.4, 6.6).LineTo(0.5, 5).LineTo(3.4, 3.4).ClosePath().Fill()
										},
									},
									core.Text{
										Content: "placing slime_02",
										Style: core.Style{
											FontSize:   11,
											FontWeight: 500,
											Color:      p.TextSecondary,
										},
									},
								},
							},
						},
					},
				},
			},
		)

		addedCard := card(
			"spec-legend-added",
			"Added",
			"added · 1.5 px dashed · “+ name” pill.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(ui.Alpha(p.Added.Fg, 0.28)).BeginPath().RoundRect(cx-15, fy-22, 30, 22, 8).Fill()
							d.SetStrokeColor(p.Added.Edge).SetLineWidth(1.5).SetLineDash(5, 3).BeginPath().RoundRect(cx-19, fy-26, 38, 30, 10).Stroke()
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(68),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							pillNode("spec-legend-added-pill", "+ slime_03", p.Added.Fg, p.Added.Fill, p.Added.Edge, ""),
						},
					},
				},
			},
		)

		changedCard := card(
			"spec-legend-changed",
			"Changed",
			"changed · 1.5 px dashed · field old → new pill.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(p.Changed.Fg).BeginPath().RoundRect(cx-12, fy-34, 24, 34, 4).Fill()
							d.SetStrokeColor(p.Changed.Edge).SetLineWidth(1.5).SetLineDash(5, 3).BeginPath().RoundRect(cx-17, fy-39, 34, 44, 6).Stroke()
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(56),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							pillNode("spec-legend-changed-pill", "z 10 → 12", p.Changed.Fg, p.Changed.Fill, p.Changed.Edge, ""),
						},
					},
				},
			},
		)

		removedCard := card(
			"spec-legend-removed",
			"Removed",
			"removed · 1.5 px solid around ghost · struck-through name.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(ui.Alpha(p.TextFaint, 0.30)).BeginPath().Ellipse(cx, fy-16, 8, 8).Fill()
							d.SetStrokeColor(p.Removed.Edge).SetLineWidth(1.5).BeginPath().RoundRect(cx-13, fy-29, 26, 26, 6).Stroke()
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(64),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							pillNode("spec-legend-removed-pill", "removed", p.Removed.Fg, p.Removed.Fill, p.Removed.Edge, "coin_04"),
						},
					},
				},
			},
		)

		problemCard := card(
			"spec-legend-problem",
			"Problem",
			"problem · 2 px solid on live object · filled pill.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(p.TextMuted).BeginPath().RoundRect(cx-14, fy-38, 28, 38, 4).Fill()
							d.SetStrokeColor(p.Problem().Edge).SetLineWidth(2).BeginPath().RoundRect(cx-18, fy-42, 36, 46, 6).Stroke()
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(52),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							pillNode("spec-legend-problem-pill", "door: lever_02 missing", p.Problem().OnFill, p.Problem().Fg, core.Transparent, ""),
						},
					},
				},
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
					Children: []core.Node{selectionCard, agentCard, addedCard},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
						Gap:       16,
					},
					Children: []core.Node{changedCard, removedCard, problemCard},
				},
			},
		}
	})
}
