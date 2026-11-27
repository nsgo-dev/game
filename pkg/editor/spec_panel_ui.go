package editor

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	specSpace4  = SpecSpace4
	specSpace8  = SpecSpace8
	specSpace12 = SpecSpace12
	specSpace16 = SpecSpace16
	specSpace24 = SpecSpace24

	specRadiusControl = SpecRadiusControl
	specRadiusButton  = SpecRadiusButton
	specRadiusCard    = SpecRadiusCard

	specRowHeight    = SpecRowHeight
	specButtonHeight = SpecButtonHeight
	specHeaderHeight = SpecHeaderHeight
	specIconSize     = SpecIconSize
)

type specButtonVariant uint8

const (
	specBtnQuiet specButtonVariant = iota
	specBtnPrimary
	specBtnAccent
	specBtnSecondary
	specBtnGhost
	specBtnDanger
)

type specPanelButtonSpec struct {
	TestID     string
	Text       string
	TextFunc   func() string
	Icon       func() *core.SVG
	IconFunc   func() *core.SVG
	Label      string
	LabelFunc  func() string
	Tooltip    string
	Variant    specButtonVariant
	Compact    bool
	InsetFocus bool
	Disabled   func() bool
	Busy       func() bool
	OnPress    func()
	Style      core.Style
}

type specPanelToggleSpec struct {
	TestID     string
	Text       string
	Icon       func() *core.SVG
	Label      string
	Tooltip    string
	On         func() bool
	Disabled   func() bool
	InsetFocus bool
	OnChange   func(bool)
}

type specPanelRowButtonSpec struct {
	TestID    string
	Label     string
	LabelFunc func() string
	Tooltip   string
	Disabled  func() bool
	Selected  func() bool
	Style     core.Style
	StyleFunc func() core.Style
	OnPress   func()
	Children  []core.Node
}

func specHoverOverlay(p SpecPalette) core.Color {
	return p.HoverOverlay
}

func specActiveOverlay(p SpecPalette) core.Color {
	return p.ActiveOverlay
}

func specPrimaryHover(p SpecPalette) core.Color {
	return p.PrimaryHover
}

// specShortcutTip formats a Spec tooltip string ("Name · shortcut") when shortcut is non-empty.
func specShortcutTip(name, shortcut string) string {
	if name == "" {
		return shortcut
	}
	if shortcut == "" {
		return name
	}
	return name + " · " + shortcut
}

// specTooltipNode builds a Spec-styled tooltip bubble for base.Button / base.Toggle Tooltip props.
func specTooltipNode(p SpecPalette, tip string) core.Node {
	if tip == "" {
		return nil
	}
	return core.View{
		Style: core.Style{
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			CornerRadius: specRadiusControl,
			Padding:      core.Symmetric(specSpace4, specSpace8),
		},
		Children: []core.Node{
			core.Text{
				Content: tip,
				Style: core.Style{
					FontSize: 11.5,
					Color:    p.TextSecondary,
				},
			},
		},
	}
}

// specIcon renders a decorative Lucide stroke icon at the Spec's 15 px size (decorative / aria-hidden by default in core.Icon).
func specIcon(svg func() *core.SVG, size core.Points, col core.Color) core.Node {
	if size == 0 {
		size = specIconSize
	}
	return core.Icon{
		SVG: svg(),
		Style: core.Style{
			Width:      core.Pt(size),
			Height:     core.Pt(size),
			Color:      col,
			FlexShrink: 0,
		},
	}
}

// specDynamicIcon renders a reactive decorative Lucide stroke icon.
// Callers should read a memoized signal inside svgFunc so unrelated rev ticks do not remount the icon.
func specDynamicIcon(svgFunc func() *core.SVG, size core.Points, colFunc func() core.Color) core.Node {
	if size == 0 {
		size = specIconSize
	}
	return core.Dynamic(func(*core.Scope) core.Node {
		svg := svgFunc()
		if svg == nil {
			return nil
		}
		return core.Icon{
			SVG: svg,
			StyleFunc: func() core.Style {
				return core.Style{
					Width:      core.Pt(size),
					Height:     core.Pt(size),
					Color:      colFunc(),
					FlexShrink: 0,
				}
			},
		}
	})
}

// specPanelContainer renders a bottom panel root region using SpecPalette tokens (Dark & Light).
func specPanelContainer(cx *core.Scope, p SpecPalette, testID, ariaLabel string, height core.Points, children ...core.Node) core.Node {
	style := dockedStyle(cx, core.Style{
		Height:      core.Pt(height),
		FlexShrink:  0,
		Background:  p.Panel,
		Borders:     core.Edges{Top: 1},
		BorderColor: p.Line,
		Overflow:    core.Hidden,
	})
	style.Borders = core.Edges{Top: 1}
	style.BorderColor = p.Line
	return core.View{
		TestID:   testID,
		Role:     core.RoleRegion,
		Label:    ariaLabel,
		Style:    style,
		Children: children,
	}
}

// specPanelHeader renders a 44 px Spec panel header bar with 8/16 spacing and a bottom line divider.
func specPanelHeader(p SpecPalette, children ...core.Node) core.Node {
	return core.View{
		Style: core.Style{
			MinHeight:   core.Pt(specHeaderHeight),
			FlexShrink:  0,
			Direction:   core.Row,
			Wrap:        core.Wrap,
			AlignItems:  core.AlignCenter,
			Padding:     core.Symmetric(specSpace8, specSpace16),
			Gap:         specSpace8,
			Background:  p.Panel,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.Line,
		},
		Children: children,
	}
}

// specPanelTitle renders a Spec panel title (Inter 14 / 600) with a leading Lucide icon.
func specPanelTitle(p SpecPalette, testID string, icon func() *core.SVG, title string, titleFunc func() string) core.Node {
	txt := core.Text{
		TestID:      testID,
		Content:     title,
		ContentFunc: titleFunc,
		Style: core.Style{
			FontSize:   14,
			FontWeight: core.FontWeightSemibold,
			Color:      p.TextPrimary,
		},
	}
	if icon == nil {
		return txt
	}
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        specSpace8,
			FlexShrink: 0,
		},
		Children: []core.Node{
			specIcon(icon, specIconSize, p.TextMuted),
			txt,
		},
	}
}

// specSectionLabel renders a Spec section header label (11 / 600 / +6% tracking).
func specSectionLabel(p SpecPalette, text string) core.Node {
	return core.Text{
		Content: text,
		Style: core.Style{
			FontSize:      11,
			FontWeight:    core.FontWeightSemibold,
			LetterSpacing: 0.66,
			Color:         p.TextFaint,
			FlexShrink:    0,
		},
	}
}

// specPanelButton renders a real base.Button on SpecPalette tokens, heights (28/30), radius (8),
// contrast-verified text/fill pairs, focus ring, aria-label, and optional tooltip.
func specPanelButton(p SpecPalette, b specPanelButtonSpec) core.Node {
	bg, hoverBg, activeBg, fg, border := specButtonColors(p, b.Variant)
	weight := core.FontWeightMedium
	if b.Variant == specBtnPrimary || b.Variant == specBtnAccent {
		weight = core.FontWeightSemibold
	}

	height := specButtonHeight
	if b.Compact {
		height = specRowHeight
	}
	hasText := b.Text != "" || b.TextFunc != nil
	pad := core.Symmetric(0, specSpace12)
	var width core.Length
	if !hasText {
		pad = core.All(0)
		width = core.Pt(height)
	}

	focusRing := p.FocusRing(2)
	if b.InsetFocus {
		focusRing = p.FocusRing(-2)
	}

	label := b.Label
	if label == "" && b.LabelFunc == nil {
		label = b.Text
	}
	labelFunc := b.LabelFunc
	if label == "" && labelFunc == nil && b.TextFunc != nil {
		labelFunc = b.TextFunc
	}

	var children []core.Node
	if b.Busy != nil {
		children = append(children, core.Show{
			When: b.Busy,
			Then: func(*core.Scope) core.Node {
				return ui.Spinner{
					Size: 13,
					Tint: func() core.Color { return fg },
				}
			},
			Else: func(*core.Scope) core.Node {
				if b.IconFunc != nil {
					return specDynamicIcon(b.IconFunc, specIconSize, func() core.Color { return fg })
				}
				if b.Icon != nil {
					return specIcon(b.Icon, specIconSize, fg)
				}
				return nil
			},
		})
	} else if b.IconFunc != nil {
		children = append(children, specDynamicIcon(b.IconFunc, specIconSize, func() core.Color { return fg }))
	} else if b.Icon != nil {
		children = append(children, specIcon(b.Icon, specIconSize, fg))
	}

	if hasText {
		children = append(children, core.Text{
			Content:     b.Text,
			ContentFunc: b.TextFunc,
			MaxLines:    1,
			Style: core.Style{
				FontSize:   12.5,
				FontWeight: weight,
				Color:      fg,
			},
		})
	}

	baseStyle := core.Style{
		Height:         core.Pt(height),
		MinHeight:      core.Pt(specRowHeight),
		MinWidth:       core.Pt(specRowHeight),
		Width:          width,
		FlexShrink:     0,
		Direction:      core.Row,
		AlignItems:     core.AlignCenter,
		JustifyContent: core.JustifyCenter,
		Gap:            specSpace8,
		Padding:        pad,
		CornerRadius:   specRadiusButton,
		Background:     bg,
		BorderWidth:    1,
		BorderColor:    border,
		Cursor:         core.CursorPointer,
		Hover:          &core.Style{Background: hoverBg},
		Active:         &core.Style{Background: activeBg},
		Disabled:       &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault},
		FocusVisible:   focusRing,
	}.Merge(b.Style)

	var onPress func(core.PressEvent)
	if b.OnPress != nil {
		onPress = func(core.PressEvent) {
			if b.Busy != nil && b.Busy() {
				return
			}
			b.OnPress()
		}
	}

	return base.Button{
		TestID:    b.TestID,
		Label:     label,
		LabelFunc: labelFunc,
		Tooltip:   specTooltipNode(p, b.Tooltip),
		Disabled:  b.Disabled,
		Busy:      b.Busy,
		Style:     baseStyle,
		OnPress:   onPress,
		Children:  children,
	}
}

func specButtonColors(p SpecPalette, v specButtonVariant) (bg, hover, active, fg, border core.Color) {
	hov := specHoverOverlay(p)
	act := specActiveOverlay(p)
	transparent := core.Color(0)
	switch v {
	case specBtnPrimary:
		// Spec contrast table: 18.75:1 AAA in Dark (#101216 on #FFFFFF) and Light (#FFFFFF on #101216).
		return p.TextPrimary, specPrimaryHover(p), p.TextSecondary, p.OnTextPrimary, p.TextPrimary
	case specBtnAccent:
		// Spec contrast table: OnFill ink on Accent fill (and hover/pressed) meets WCAG AA in both Dark and Light.
		return p.Accent.Fg, p.AccentHover, p.AccentPressed, p.Accent.OnFill, p.Accent.Fg
	case specBtnSecondary:
		return p.LineRaised, hov.Over(p.LineRaised), act.Over(p.LineRaised), p.TextSecondary, p.LineRaised
	case specBtnGhost:
		return transparent, p.Raised, p.LineRaised, p.TextMuted, transparent
	case specBtnDanger:
		prob := p.Problem()
		return prob.Fill, hov.Over(prob.Fill), act.Over(prob.Fill), prob.Fg, prob.Edge
	default: // specBtnQuiet
		return p.Raised, p.LineRaised, act.Over(p.LineRaised), p.TextSecondary, p.LineRaised
	}
}

// specPanelToggle renders a real base.Toggle button styled strictly with SpecPalette tokens.
func specPanelToggle(p SpecPalette, b specPanelToggleSpec) core.Node {
	focusRing := p.FocusRing(2)
	if b.InsetFocus {
		focusRing = p.FocusRing(-2)
	}
	label := b.Label
	if label == "" {
		label = b.Text
	}

	var children []core.Node
	if b.Icon != nil {
		children = append(children, core.Icon{
			SVG: b.Icon(),
			StyleFunc: func() core.Style {
				col := p.TextMuted
				if b.On != nil && b.On() {
					col = p.TextPrimary
				}
				return core.Style{
					Width:      core.Pt(specIconSize),
					Height:     core.Pt(specIconSize),
					Color:      col,
					FlexShrink: 0,
				}
			},
		})
	}
	if b.Text != "" {
		children = append(children, core.Text{
			Content:  b.Text,
			MaxLines: 1,
			StyleFunc: func() core.Style {
				col := p.TextMuted
				weight := core.FontWeightMedium
				if b.On != nil && b.On() {
					col = p.TextPrimary
					weight = core.FontWeightSemibold
				}
				return core.Style{
					FontSize:   12.5,
					FontWeight: weight,
					Color:      col,
				}
			},
		})
	}

	return base.Toggle{
		TestID:   b.TestID,
		Label:    label,
		Tooltip:  specTooltipNode(p, b.Tooltip),
		Pressed:  b.On,
		Disabled: b.Disabled,
		OnChange: b.OnChange,
		StyleFunc: func() core.Style {
			bg := p.Raised
			border := p.LineRaised
			if b.On != nil && b.On() {
				bg = p.Accent.Fill
				border = p.Accent.Edge
			}
			return core.Style{
				Height:         core.Pt(specRowHeight),
				MinHeight:      core.Pt(specRowHeight),
				MinWidth:       core.Pt(specRowHeight),
				FlexShrink:     0,
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Gap:            specSpace8,
				Padding:        core.Symmetric(0, specSpace12),
				CornerRadius:   specRadiusButton,
				Background:     bg,
				BorderWidth:    1,
				BorderColor:    border,
				Cursor:         core.CursorPointer,
				Hover:          &core.Style{Background: p.LineRaised},
				Disabled:       &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault},
				FocusVisible:   focusRing,
			}
		},
		Children: children,
	}
}

// specPanelToggleGroup wraps toggles in a horizontal roving-focus base.ToggleGroup
// so arrow keys move between choices and Tab reaches the group cleanly.
func specPanelToggleGroup(p SpecPalette, label string, children ...core.Node) core.Node {
	return base.ToggleGroup{
		Label: label,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          specSpace4,
			Padding:      core.All(specSpace4),
			CornerRadius: specRadiusButton,
			Background:   p.Bg,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			FlexShrink:   0,
		},
		Children: children,
	}
}

// specPanelRowButton renders a real base.Button for clickable list, search, reference, and problem rows.
func specPanelRowButton(p SpecPalette, b specPanelRowButtonSpec) core.Node {
	baseStyle := core.Style{
		Direction:    core.Row,
		AlignItems:   core.AlignCenter,
		MinHeight:    core.Pt(specRowHeight),
		Width:        core.Pct(100),
		Gap:          specSpace8,
		Padding:      core.Symmetric(specSpace4, specSpace12),
		CornerRadius: specRadiusControl,
		Background:   p.Raised,
		BorderWidth:  1,
		BorderColor:  p.Line,
		Cursor:       core.CursorPointer,
		Hover:        &core.Style{Background: p.LineRaised, BorderColor: p.LineRaised},
		Active:       &core.Style{Background: p.LineRaised},
		Disabled:     &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault},
		FocusVisible: p.FocusRing(-2),
	}.Merge(b.Style)

	var onPress func(core.PressEvent)
	if b.OnPress != nil {
		onPress = func(core.PressEvent) { b.OnPress() }
	}
	return base.Button{
		TestID:    b.TestID,
		Label:     b.Label,
		LabelFunc: b.LabelFunc,
		Tooltip:   specTooltipNode(p, b.Tooltip),
		Disabled:  b.Disabled,
		Selected:  b.Selected,
		Style:     baseStyle,
		StyleFunc: b.StyleFunc,
		OnPress:   onPress,
		Children:  b.Children,
	}
}

// specInputStyle returns the Spec control style for text and numeric inputs (28 px height, 6 px radius).
func specInputStyle(p SpecPalette) core.Style {
	return core.Style{
		Height:           core.Pt(specRowHeight),
		CornerRadius:     specRadiusControl,
		FontSize:         12.5,
		Color:            p.TextSecondary,
		PlaceholderColor: p.TextFaint,
		Background:       p.Raised,
		BorderWidth:      1,
		BorderColor:      p.LineRaised,
		CaretColor:       p.Accent.Edge,
		Focus:            &core.Style{BorderColor: p.Accent.Edge},
		FocusVisible:     p.FocusRing(-2),
	}
}

// specNumberInputStyle returns the Spec control style for ui.NumberInput filling its container width
// (overriding ui.NumberInput's default 160 pt width so steppers stay inside the wrapper).
func specNumberInputStyle(p SpecPalette) core.Style {
	s := specInputStyle(p)
	s.Width = core.Pct(100)
	return s
}

// specSelectStyle returns the Spec control style for ui.Select controls filling their container width.
func specSelectStyle(p SpecPalette) core.Style {
	return core.Style{
		Width:        core.Pct(100),
		Height:       core.Pt(specRowHeight),
		CornerRadius: specRadiusControl,
		FontSize:     12.5,
		Color:        p.TextSecondary,
		Background:   p.Raised,
		BorderWidth:  1,
		BorderColor:  p.LineRaised,
		FocusVisible: p.FocusRing(-2),
	}
}

// specStatusBanner renders a Spec problem or warning banner with TriangleAlert icon.
func specStatusBanner(p SpecPalette, testID string, isWarning bool, msg string, msgFunc func() string, action core.Node) core.Node {
	role := p.Problem()
	if isWarning {
		role = p.Warning
	}
	children := []core.Node{
		specIcon(lucide.TriangleAlert, specIconSize, role.Fg),
		core.Text{
			Content:     msg,
			ContentFunc: msgFunc,
			Style: core.Style{
				FontSize:   12.5,
				Color:      p.TextSecondary,
				FlexGrow:   1,
				FlexShrink: 1,
			},
		},
	}
	if action != nil {
		children = append(children, action)
	}
	return core.View{
		TestID: testID,
		Role:   core.RoleAlert,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			MinHeight:    core.Pt(specRowHeight),
			Gap:          specSpace8,
			Padding:      core.Symmetric(specSpace8, specSpace12),
			CornerRadius: specRadiusButton,
			Background:   role.Fill,
			BorderWidth:  1,
			BorderColor:  role.Edge,
			FlexShrink:   0,
		},
		Children: children,
	}
}

// specEmptyState renders a Spec empty state card with a leading Lucide icon, title, optional detail, and optional action.
func specEmptyState(p SpecPalette, testID string, icon func() *core.SVG, iconColor core.Color, title, detail string, action core.Node) core.Node {
	textChildren := []core.Node{
		core.Text{
			Content: title,
			Style: core.Style{
				FontSize:   12.5,
				FontWeight: core.FontWeightMedium,
				Color:      p.TextSecondary,
			},
		},
	}
	if detail != "" {
		textChildren = append(textChildren, core.Text{
			Content: detail,
			Style: core.Style{
				FontSize: 11.5,
				Color:    p.TextFaint,
			},
		})
	}
	children := []core.Node{
		specIcon(icon, specIconSize, iconColor),
		core.View{
			Style: core.Style{
				FlexGrow:   1,
				FlexShrink: 1,
				Gap:        specSpace4,
			},
			Children: textChildren,
		},
	}
	if action != nil {
		children = append(children, action)
	}
	return core.View{
		TestID: testID,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          specSpace12,
			Padding:      core.Symmetric(specSpace12, specSpace16),
			CornerRadius: specRadiusCard,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.Line,
		},
		Children: children,
	}
}

// specLoadingState renders a Spec loading state card with a spinner and status label.
func specLoadingState(p SpecPalette, testID, label string) core.Node {
	return core.View{
		TestID: testID,
		Role:   core.RoleStatus,
		Label:  label,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          specSpace12,
			Padding:      core.Symmetric(specSpace12, specSpace16),
			CornerRadius: specRadiusCard,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.Line,
		},
		Children: []core.Node{
			ui.Spinner{
				Size:  specIconSize,
				Label: label,
				Tint:  func() core.Color { return p.Accent.Fg },
			},
			core.Text{
				Content: label,
				Style: core.Style{
					FontSize: 12.5,
					Color:    p.TextMuted,
				},
			},
		},
	}
}

// specSeverityPill renders a small semantic badge (Problem or Warning) with icon + label.
func specSeverityPill(p SpecPalette, testID string, isWarning bool, text string) core.Node {
	role := p.Problem()
	if isWarning {
		role = p.Warning
	}
	return core.View{
		TestID: testID,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          specSpace4,
			Height:       core.Pt(20),
			Padding:      core.Symmetric(0, specSpace8),
			CornerRadius: specRadiusControl,
			Background:   role.Fill,
			BorderWidth:  1,
			BorderColor:  role.Edge,
			FlexShrink:   0,
		},
		Children: []core.Node{
			specIcon(lucide.TriangleAlert, 12, role.Fg),
			core.Text{
				Content: text,
				Style: core.Style{
					FontSize:   11,
					FontWeight: core.FontWeightSemibold,
					Color:      role.Fg,
				},
			},
		},
	}
}
