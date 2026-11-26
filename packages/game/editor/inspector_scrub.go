package editor

import (
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	inspectorDragTip   = "Drag to change · ↑↓ steps, ⇧ ×10 · ↵ commits, Esc restores"
	inspectorDragTip3D = "Drag a label to scrub · Shift for 0.01 steps · ↑↓ steps, ⇧ ×10 · ↵ commits, Esc restores"
)

// specInspectorTheme scopes Spec dark-surface tokens to the Inspector subtree so every standard
// ui.Button, ui.Input, ui.Select, ui.Checkbox, and plugin view inherits readable Spec dark colors
// regardless of the host OS's light/dark appearance.
var specInspectorTheme = newSpecInspectorTheme()

func newSpecInspectorTheme() *core.Theme {
	t := core.DefaultTheme().Clone()
	p := t.Dark
	p.Background = specBg
	p.Surface = specPanel
	p.SurfaceHover = specRaised
	p.SurfacePressed = specLineRaised
	p.Border = specLineRaised
	p.Text = specText
	p.TextMuted = specTextMuted
	p.Accent = specAccent
	p.AccentHover = specAccent
	p.AccentPressed = specAccent
	p.AccentText = specBg
	p.Danger = specProblem
	p.Warning = specChanged
	p.Success = specAdded
	p.Selection = core.RGBA(0x3B7BFF, 0.24)
	p.Focus = core.RGBA(0x3B7BFF, 0.85)
	p.Scrollbar = specTextMuted
	p.SetColor("inset", specRaised)
	p.SetColor("raised", specRaised)
	p.SetColor("field", specBg)
	p.SetColor("fieldBorder", specLineRaised)
	p.SetColor("line", specLine)
	p.SetColor("faint", specTextMuted)
	p.SetColor("accentFill", specAccent)
	p.SetColor("light", specTextStrong)
	p.SetColor("onLight", specBg)
	p.SetColor("track", specLineRaised)
	p.SetColor("knob", specTextStrong)
	p.SetColor("code", specText)
	p.SetColor("info", specAccent)
	p.SetColor("down", specProblem)
	t.Dark = p
	t.Light = p
	return t.Clone()
}

// inspectorThemeScope establishes a child scope with Spec dark appearance and palette for the
// Inspector subtree without affecting sibling editor panels.
type inspectorThemeScope struct {
	build func(*core.Scope) core.Node
}

func (s inspectorThemeScope) Render(cx *core.Scope) core.Node {
	cx.SetSubtreeAppearance(core.AppearanceDark)
	cx.SetTheme(specInspectorTheme)
	return s.build(cx)
}

// inspectorNumberFieldCfg configures a Spec-styled scrubbable numeric input in the inspector.
type inspectorNumberFieldCfg struct {
	TestID    string
	Label     string
	AxisLabel string     // optional inline scrub handle inside the box ("X", "Y", "Z")
	AxisColor core.Color // color for AxisLabel when set
	Unit      string     // optional trailing unit text (e.g. "°", "px/s")
	Step      float64
	Min, Max  *float64
	Integer   bool
	Scrub     InspectorScrubField
	Format    func(float64) string
	Disabled  func() bool
	Invalid   func() bool
	Value     func() float64
	OnChange  func(float64)
}

// inspectorScrubHandle wraps content in a horizontal drag-to-scrub view backed by Editor's
// BeginInspectorScrub / UpdateInspectorScrub / EndInspectorScrub / CancelInspectorScrub session
// so dragging records one undo step and Escape restores the original value.
func inspectorScrubHandle(
	ed *Editor,
	testID string,
	tip string,
	spec InspectorScrubField,
	disabled func() bool,
	style core.Style,
	content core.Node,
) core.Node {
	var shiftHeld bool
	tipText := tip
	if tipText == "" {
		if ed != nil && ed.ViewMode == ViewMode3D {
			tipText = inspectorDragTip3D
		} else {
			tipText = inspectorDragTip
		}
	}
	return withTooltip(core.View{
		TestID: testID,
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Cursor:     core.CursorColResize,
		}.Merge(style),
		PanActivation: core.PanActivation{ActiveOffsetX: 2, FailOffsetY: 12},
		OnMouseDown: func(ev core.MouseButtonEvent) {
			shiftHeld = ev.Modifiers.Shift
		},
		OnMouseMove: func(ev core.MouseEvent) {
			shiftHeld = ev.Modifiers.Shift
		},
		OnPan: func(ev core.PanEvent) {
			if ev.Phase == core.GestureCancelled {
				ed.CancelInspectorScrub()
				return
			}
			if disabled != nil && disabled() {
				if ed.InspectorScrubbing() {
					ed.CancelInspectorScrub()
				}
				return
			}
			switch ev.Phase {
			case core.GestureBegan:
				_ = ed.BeginInspectorScrub(spec)
			case core.GestureChanged:
				if ed.InspectorScrubbing() {
					_, _ = ed.UpdateInspectorScrub(float64(ev.Translation.X), shiftHeld)
				}
			case core.GestureEnded:
				if ed.InspectorScrubbing() {
					_, _ = ed.UpdateInspectorScrub(float64(ev.Translation.X), shiftHeld)
					ed.EndInspectorScrub()
				}
			}
		},
		Children: []core.Node{content},
	}, tipText)
}

// inspectorNumberBox renders a compact Spec-styled numeric field with optional inline axis scrub
// handle ("X", "Y", "Z"), keyboard ↑↓ / ⇧×10 stepping, Enter commit, and Escape restore.
func inspectorNumberBox(cx *core.Scope, ed *Editor, cfg inspectorNumberFieldCfg) core.Node {
	step := cfg.Step
	if !isFiniteFloat64(step) || step <= 0 {
		if cfg.Integer {
			step = 1
		} else {
			step = 0.1
		}
	}
	format := cfg.Format
	if format == nil {
		if cfg.Integer {
			format = func(v float64) string {
				if !isFiniteFloat64(v) {
					return "0"
				}
				v = clampInt32Float(math.Round(v))
				return strconv.Itoa(int(v))
			}
		} else {
			format = func(v float64) string {
				if !isFiniteFloat64(v) {
					return "0"
				}
				return strconv.FormatFloat(v, 'g', -1, 64)
			}
		}
	}
	parse := func(s string) (float64, bool) {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v, err == nil && isFiniteFloat64(v)
	}
	clamp := func(v float64) (float64, bool) {
		if !isFiniteFloat64(v) {
			return 0, false
		}
		if cfg.Min != nil && isFiniteFloat64(*cfg.Min) && v < *cfg.Min {
			v = *cfg.Min
		}
		if cfg.Max != nil && isFiniteFloat64(*cfg.Max) && v > *cfg.Max {
			v = *cfg.Max
		}
		if cfg.Integer {
			v = clampInt32Float(math.Round(v))
		}
		return v, isFiniteFloat64(v)
	}

	fieldRef := new(core.Ref)
	focused := core.NewSignal(cx, false)
	var focusStart float64
	var focusHistoryIdx int
	var cancelling bool

	stepBy := func(delta float64) {
		if cfg.Disabled != nil && cfg.Disabled() {
			return
		}
		if !isFiniteFloat64(delta) {
			return
		}
		cur := 0.0
		if cfg.Value != nil {
			cur = core.Untracked(cx, cfg.Value)
		}
		if !isFiniteFloat64(cur) {
			return
		}
		next, ok := clamp(roundToStepPrecision(cur+delta, math.Abs(delta)))
		if !ok {
			return
		}
		if cfg.OnChange != nil {
			cfg.OnChange(next)
		}
	}

	var prefix core.Node
	if cfg.AxisLabel != "" {
		axisColor := cfg.AxisColor
		if axisColor == 0 {
			axisColor = specTextMuted
		}
		prefix = inspectorScrubHandle(
			ed,
			cfg.TestID+"-scrub",
			"",
			cfg.Scrub,
			cfg.Disabled,
			core.Style{
				Height:         core.Pt(26),
				Padding:        core.Edges{Left: 7, Right: 3},
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			core.Text{
				Content:  cfg.AxisLabel,
				MaxLines: 1,
				Style: core.Style{
					FontSize:   10.5,
					FontWeight: core.FontWeightSemibold,
					Color:      axisColor,
				},
			},
		)
	}

	var suffix core.Node
	if cfg.Unit != "" {
		suffix = core.View{
			Style: core.Style{
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Padding:        core.Edges{Right: 7},
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.Text{
					Content:  cfg.Unit,
					MaxLines: 1,
					Style: core.Style{
						FontSize: 10.5,
						Color:    specTextMuted,
					},
				},
			},
		}
	}

	in := base.NumberInput{
		Ref:           fieldRef,
		TestID:        cfg.TestID,
		Label:         cfg.Label,
		Invalid:       cfg.Invalid,
		Disabled:      cfg.Disabled,
		ControlsRight: true,
		OnCommit:      true,
		Snap:          false,
		Format:        format,
		Parse:         parse,
		Step:          func() float64 { return step },
		Value: func() *float64 {
			v := 0.0
			if cfg.Value != nil {
				v = cfg.Value()
			}
			return &v
		},
		OnChange: func(v *float64) {
			if cancelling || v == nil || cfg.OnChange == nil {
				return
			}
			if clamped, ok := clamp(*v); ok {
				cfg.OnChange(clamped)
			}
		},
		OnFocus: func() {
			cancelling = false
			if cfg.Value != nil {
				focusStart = core.Untracked(cx, cfg.Value)
			}
			if ed != nil && ed.History != nil {
				focusHistoryIdx = ed.History.Index()
			}
			focused.Set(true)
		},
		OnBlur: func() {
			cancelling = false
			focused.Set(false)
		},
		Prefix: prefix,
		Suffix: suffix,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Height:       core.Pt(28),
			FlexGrow:     1,
			FlexShrink:   1,
			MinWidth:     core.Pt(0),
			CornerRadius: 6,
			BorderWidth:  1,
			Overflow:     core.Hidden,
		},
		StyleFunc: func() core.Style {
			border := specLineRaised
			width := core.Points(1)
			if cfg.Invalid != nil && cfg.Invalid() {
				border = specProblem
				width = 1.5
			} else if focused.Get() {
				border = specAccent
			}
			bg := specRaised
			op := core.Alpha(1)
			if cfg.Disabled != nil && cfg.Disabled() {
				op = core.Alpha(0.55)
			}
			return core.Style{
				Background:  bg,
				BorderColor: border,
				BorderWidth: width,
				Opacity:     op,
			}
		},
		InputStyle: core.Style{
			FlexGrow:   1,
			FlexShrink: 1,
			MinWidth:   core.Pt(24),
			Height:     core.Pt(26),
			FontFamily: core.FontMonospace,
			FontSize:   11.5,
			Color:      specTextStrong,
			Padding:    core.Symmetric(0, 6),
		},
		InputStyleFunc: func() core.Style {
			padLeft := core.Points(8)
			if cfg.AxisLabel != "" {
				padLeft = 2
			}
			return core.Style{
				Color:            specTextStrong,
				PlaceholderColor: specTextMuted,
				CaretColor:       specAccent,
				Padding:          core.Edges{Left: padLeft, Right: 6},
			}
		},
	}
	if cfg.Min != nil {
		lo := *cfg.Min
		in.Min = func() *float64 { return &lo }
	}
	if cfg.Max != nil {
		hi := *cfg.Max
		in.Max = func() *float64 { return &hi }
	}

	// While focused, bind Shift+Up/Down for ×10 steps and Escape to restore the pre-focus value.
	keyOverlay := core.Show{
		When: focused.Get,
		Then: func(cx *core.Scope) core.Node {
			cx.OnKeyWith("shift+up", core.KeyOptions{When: core.KeyAlways}, func() {
				stepBy(step * 10)
			})
			cx.OnKeyWith("shift+down", core.KeyOptions{When: core.KeyAlways}, func() {
				stepBy(-step * 10)
			})
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
				// Restore any keyboard-stepped edits cleanly via Undo (avoiding extra undo steps),
				// then suppress commit-on-blur so uncommitted typed text is discarded and replaced
				// by the formatted focusStart value.
				if ed != nil && ed.History != nil && !ed.Playing() {
					for ed.History.Index() > focusHistoryIdx {
						if !ed.Undo() {
							break
						}
					}
				}
				if cfg.OnChange != nil && cfg.Value != nil && isFiniteFloat64(focusStart) {
					if cur := core.Untracked(cx, cfg.Value); cur != focusStart {
						cfg.OnChange(focusStart)
					}
				}
				cancelling = true
				fieldRef.Blur()
			})
			return nil
		},
	}

	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			FlexGrow:   1,
			FlexShrink: 1,
			MinWidth:   core.Pt(0),
		},
		Children: []core.Node{in, keyOverlay},
	}
}

// inspectorTitledSection renders a Spec-styled collapsible titled section header (uppercase title,
// disclosure chevron, field count badge, optional problem pill or in-review count, optional
// trailing action such as component remove) above its collapsible body.
func inspectorTitledSection(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	sectionID string,
	titleTestID string,
	title string,
	titleTip string,
	fieldCount int,
	problemCount func() int,
	reviewCount func() int,
	trailing core.Node,
	forceExpanded bool,
	body []core.Node,
) core.Node {
	type sectionMeta struct {
		probs   int
		reviews int
	}
	openMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		if forceExpanded {
			return true
		}
		return !ed.InspectorSectionCollapsed(sectionID)
	})
	isOpen := openMemo.Get
	metaMemo := core.NewMemo(cx, func() sectionMeta {
		_ = rev.Get()
		probs := 0
		if problemCount != nil {
			probs = problemCount()
		}
		reviews := 0
		if reviewCount != nil {
			reviews = reviewCount()
		}
		return sectionMeta{probs: probs, reviews: reviews}
	})

	countText := func() string {
		if fieldCount == 1 {
			return ed.T("inspector.fieldCountOne", l10n.Param("count", 1))
		}
		return ed.T("inspector.fieldCountOther", l10n.Param("count", fieldCount))
	}

	var titleNode core.Node = core.Text{
		TestID:   titleTestID,
		Content:  title,
		MaxLines: 1,
		Style: core.Style{
			FontSize:      11,
			FontWeight:    core.FontWeightSemibold,
			LetterSpacing: 0.7,
			Color:         specTextMuted,
		},
	}
	if titleTip != "" {
		titleNode = withTooltip(titleNode, titleTip)
	}

	metaBadge := core.Dynamic(func(cx *core.Scope) core.Node {
		m := metaMemo.Get()
		probs, reviews := m.probs, m.reviews
		if probs > 0 {
			label := ed.T("inspector.problemCountOther", l10n.Param("count", probs))
			if probs == 1 {
				label = ed.T("inspector.problemCountOne", l10n.Param("count", 1))
			}
			return core.View{
				TestID: "inspector-section-" + sectionID + "-problem",
				Style: core.Style{
					Height:         core.Pt(18),
					Padding:        core.Symmetric(0, 6),
					CornerRadius:   9,
					Background:     specProblemTint,
					BorderWidth:    1,
					BorderColor:    specProblemEdge,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.Text{
						Content:  label,
						MaxLines: 1,
						Style: core.Style{
							FontSize:   10.5,
							FontWeight: core.FontWeightMedium,
							Color:      specProblem,
						},
					},
				},
			}
		}
		if reviews > 0 {
			return core.Text{
				TestID:   "inspector-section-" + sectionID + "-meta",
				Content:  ed.T("inspector.inReviewCount", l10n.Param("count", reviews)),
				MaxLines: 1,
				Style: core.Style{
					FontSize: 11,
					Color:    specChanged,
				},
			}
		}
		return core.Text{
			TestID:   "inspector-section-" + sectionID + "-meta",
			Content:  countText(),
			MaxLines: 1,
			Style: core.Style{
				FontSize: 11,
				Color:    specTextMuted,
			},
		}
	})

	headerBtn := core.View{
		TestID: "inspector-section-" + sectionID,
		Role:   core.RoleButton,
		Label:  title,
		Expanded: func() bool {
			return isOpen()
		},
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Height:       core.Pt(26),
			FlexGrow:     1,
			FlexShrink:   1,
			MinWidth:     core.Pt(0),
			Padding:      core.Symmetric(0, 4),
			CornerRadius: 6,
			Gap:          5,
			Hover:        &core.Style{Background: specRaised},
			FocusVisible: specRowFocusRingStyle,
		},
		OnPress: func(core.PressEvent) {
			if !forceExpanded {
				ed.ToggleInspectorSection(sectionID)
			}
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				icon := lucide.ChevronDown
				if !isOpen() {
					icon = lucide.ChevronRight
				}
				return ui.Icon(icon, 12, func() core.Color { return specTextMuted })
			}),
			core.View{
				Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{titleNode},
			},
			metaBadge,
		},
	}

	headerRow := core.Node(headerBtn)
	if trailing != nil {
		headerRow = core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        4,
			},
			Children: []core.Node{headerBtn, trailing},
		}
	}

	return core.View{
		Style: core.Style{
			Gap:         6,
			Padding:     core.Edges{Top: 6},
			Borders:     core.Edges{Top: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			headerRow,
			core.Show{
				When: isOpen,
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style:    core.Style{Gap: 6},
						Children: body,
					}
				},
			},
		},
	}
}

// inspectorFieldRow wraps one inspector row with Spec label styling, optional review amber bar
// ("was <old> · in review"), and optional problem pill beneath the control.
func inspectorFieldRow(
	ed *Editor,
	rev *core.Signal[uint64],
	objID, component, fieldKey, fieldTestID string,
	labelNode core.Node,
	control core.Node,
) core.Node {
	var keys []string
	if fieldKey != "" {
		keys = []string{fieldKey}
	}
	return inspectorFieldRowKeys(ed, rev, objID, component, keys, fieldTestID, labelNode, control)
}

// inspectorFieldRowKeys wraps an inspector row whose controls edit one or more field keys
// (such as compact X/Y/Z rows), surfacing review changes and field problems for every key.
func inspectorFieldRowKeys(
	ed *Editor,
	rev *core.Signal[uint64],
	objID, component string,
	fieldKeys []string,
	fieldTestID string,
	labelNode core.Node,
	control core.Node,
) core.Node {
	multi := len(fieldKeys) > 1
	reviewsText := func() (string, bool) {
		_ = rev.Get()
		var parts []string
		for _, fk := range fieldKeys {
			if fk == "" {
				continue
			}
			if rc, ok := ed.inspectorReviewChangeFor(objID, component, fk); ok {
				text := ed.T("inspector.wasInReview", l10n.Param("value", rc.OldValue))
				if multi {
					text = fk + ": " + text
				}
				parts = append(parts, text)
			}
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, " · "), true
	}
	problemsText := func() (string, bool) {
		_ = rev.Get()
		var parts []string
		for _, fk := range fieldKeys {
			if fk == "" {
				continue
			}
			if msg, ok := ed.inspectorFieldProblemFor(objID, component, fk); ok {
				if multi {
					msg = fk + ": " + msg
				}
				parts = append(parts, msg)
			}
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, " · "), true
	}

	mainRow := core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        8,
		},
		StyleFunc: func() core.Style {
			if _, ok := reviewsText(); ok {
				return core.Style{
					Borders:     core.Edges{Left: 2},
					BorderColor: specChanged,
					Padding:     core.Edges{Left: 6},
				}
			}
			return core.Style{}
		},
		Children: []core.Node{
			labelNode,
			core.View{
				Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{control},
			},
		},
	}

	reviewSub := core.Show{
		When: func() bool {
			_, ok := reviewsText()
			return ok
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: fieldTestID + "-review",
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Padding:    core.Edges{Left: 76},
					Gap:        6,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   specChanged,
						},
					},
					core.Text{
						ContentFunc: func() string {
							msg, _ := reviewsText()
							return msg
						},
						Style: core.Style{
							FontSize: 11,
							Color:    specTextMuted,
						},
					},
				},
			}
		},
	}

	problemSub := core.Show{
		When: func() bool {
			_, ok := problemsText()
			return ok
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Padding:    core.Edges{Left: 76},
				},
				Children: []core.Node{
					core.View{
						TestID: fieldTestID + "-problem",
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          5,
							Padding:      core.Symmetric(2, 8),
							CornerRadius: 9,
							Background:   specProblemTint,
							BorderWidth:  1,
							BorderColor:  specProblemEdge,
						},
						Children: []core.Node{
							ui.Icon(lucide.CircleAlert, 11, func() core.Color { return specProblem }),
							core.Text{
								ContentFunc: func() string {
									msg, _ := problemsText()
									return msg
								},
								Style: core.Style{
									FontSize: 11,
									Color:    specProblem,
								},
							},
						},
					},
				},
			}
		},
	}

	return core.View{
		Style: core.Style{Gap: 3},
		Children: []core.Node{
			mainRow,
			reviewSub,
			problemSub,
		},
	}
}
