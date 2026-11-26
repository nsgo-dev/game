package editor

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec-derived alpha tints and layout metrics from docs/design/editor/Spec.dc.html.
// Base color tokens (specBg, specPanel, specRaised, specLine, specLineRaised,
// specTextStrong, specText, specTextSecondary, specTextMuted, specAccent,
// specAdded, specChanged, specProblem, specProblemEdge, specWarning,
// specWarningFill, specWarningEdge, specFocusRingStyle, specRowFocusRingStyle)
// are defined in profiler_ui.go.
var (
	specProblemFill = core.RGBA(0xFF6B6B, 0.12)
	specHoverFill   = core.RGBA(0xFFFFFF, 0.06)
	specActiveFill  = core.RGBA(0xFFFFFF, 0.10)
	specPrimaryHov  = core.RGB(0xD9DDE5) // Spec text token used for subtle primary hover.
)

const (
	specSpace4  core.Points = 4
	specSpace8  core.Points = 8
	specSpace12 core.Points = 12
	specSpace16 core.Points = 16
	specSpace24 core.Points = 24

	specRadiusControl core.Points = 6
	specRadiusButton  core.Points = 8
	specRadiusCard    core.Points = 12

	specRowHeight    core.Points = 28
	specButtonHeight core.Points = 30
	specHeaderHeight core.Points = 44
	specIconSize     core.Points = 15
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
func specTooltipNode(tip string) core.Node {
	if tip == "" {
		return nil
	}
	return core.View{
		Style: core.Style{
			Background:   specRaised,
			BorderWidth:  1,
			BorderColor:  specLineRaised,
			CornerRadius: specRadiusControl,
			Padding:      core.Symmetric(specSpace4, specSpace8),
		},
		Children: []core.Node{
			core.Text{
				Content: tip,
				Style: core.Style{
					FontSize: 11.5,
					Color:    specText,
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

// specPanelContainer renders a bottom panel root region using only Spec tokens.
func specPanelContainer(testID, ariaLabel string, height core.Points, children ...core.Node) core.Node {
	return core.View{
		TestID: testID,
		Role:   core.RoleRegion,
		Label:  ariaLabel,
		Style: core.Style{
			Height:      core.Pt(height),
			FlexShrink:  0,
			Background:  specPanel,
			Borders:     core.Edges{Top: 1},
			BorderColor: specLine,
			Overflow:    core.Hidden,
		},
		Children: children,
	}
}

// specPanelHeader renders a 44 px Spec panel header bar with 8/16 spacing and a bottom line divider.
func specPanelHeader(children ...core.Node) core.Node {
	return core.View{
		Style: core.Style{
			MinHeight:   core.Pt(specHeaderHeight),
			FlexShrink:  0,
			Direction:   core.Row,
			Wrap:        core.Wrap,
			AlignItems:  core.AlignCenter,
			Padding:     core.Symmetric(specSpace8, specSpace16),
			Gap:         specSpace8,
			Background:  specPanel,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: specLine,
		},
		Children: children,
	}
}

// specPanelTitle renders a Spec panel title (Inter 14 / 600, #FFFFFF) with a leading Lucide icon.
func specPanelTitle(testID string, icon func() *core.SVG, title string, titleFunc func() string) core.Node {
	txt := core.Text{
		TestID:      testID,
		Content:     title,
		ContentFunc: titleFunc,
		Style: core.Style{
			FontSize:   14,
			FontWeight: core.FontWeightSemibold,
			Color:      specTextStrong,
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
			specIcon(icon, specIconSize, specTextSecondary),
			txt,
		},
	}
}

// specSectionLabel renders a Spec section header label (11 / 600 / +6% tracking, #8B92A0).
func specSectionLabel(text string) core.Node {
	return core.Text{
		Content: text,
		Style: core.Style{
			FontSize:      11,
			FontWeight:    core.FontWeightSemibold,
			LetterSpacing: 0.66,
			Color:         specTextMuted,
			FlexShrink:    0,
		},
	}
}

// specPanelButton renders a real base.Button on Spec tokens, heights (28/30), radius (8),
// contrast-verified text/fill pairs, focus ring, aria-label, and optional tooltip.
func specPanelButton(b specPanelButtonSpec) core.Node {
	bg, hoverBg, activeBg, fg, border := specButtonColors(b.Variant)
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

	focusRing := specFocusRingStyle
	if b.InsetFocus {
		focusRing = specRowFocusRingStyle
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
		Tooltip:   specTooltipNode(b.Tooltip),
		Disabled:  b.Disabled,
		Busy:      b.Busy,
		Style:     baseStyle,
		OnPress:   onPress,
		Children:  children,
	}
}

func specButtonColors(v specButtonVariant) (bg, hover, active, fg, border core.Color) {
	switch v {
	case specBtnPrimary:
		// Spec contrast table: #101216 on #FFFFFF (18.75:1 AAA).
		return specTextStrong, specPrimaryHov, specTextSecondary, specBg, specTextStrong
	case specBtnAccent:
		// Spec contrast table: #101216 on #3B7BFF (4.88:1 AA). Never #FFFFFF on #3B7BFF.
		return specAccent, specAccent, specAccent, specBg, specAccent
	case specBtnSecondary:
		return specLineRaised, specHoverFill.Over(specLineRaised), specActiveFill.Over(specLineRaised), specText, specLineRaised
	case specBtnGhost:
		return core.RGBA(0x171A21, 0), specRaised, specLineRaised, specTextSecondary, core.RGBA(0x171A21, 0)
	case specBtnDanger:
		return specProblemFill, specHoverFill.Over(specProblemFill), specActiveFill.Over(specProblemFill), specProblem, specProblemEdge
	default: // specBtnQuiet
		return specRaised, specLineRaised, specActiveFill.Over(specLineRaised), specText, specLineRaised
	}
}

// specPanelToggle renders a real base.Toggle button styled strictly with Spec tokens.
func specPanelToggle(b specPanelToggleSpec) core.Node {
	focusRing := specFocusRingStyle
	if b.InsetFocus {
		focusRing = specRowFocusRingStyle
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
				col := specTextSecondary
				if b.On != nil && b.On() {
					col = specTextStrong
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
				col := specTextSecondary
				weight := core.FontWeightMedium
				if b.On != nil && b.On() {
					col = specTextStrong
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
		Tooltip:  specTooltipNode(b.Tooltip),
		Pressed:  b.On,
		Disabled: b.Disabled,
		OnChange: b.OnChange,
		StyleFunc: func() core.Style {
			bg := specRaised
			border := specLineRaised
			if b.On != nil && b.On() {
				bg = specAccentFill
				border = specAccent
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
				Hover:          &core.Style{Background: specLineRaised},
				Disabled:       &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault},
				FocusVisible:   focusRing,
			}
		},
		Children: children,
	}
}

// specPanelToggleGroup wraps toggles in a horizontal roving-focus base.ToggleGroup
// so arrow keys move between choices and Tab reaches the group cleanly.
func specPanelToggleGroup(label string, children ...core.Node) core.Node {
	return base.ToggleGroup{
		Label: label,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          specSpace4,
			Padding:      core.All(specSpace4),
			CornerRadius: specRadiusButton,
			Background:   specBg,
			BorderWidth:  1,
			BorderColor:  specLineRaised,
			FlexShrink:   0,
		},
		Children: children,
	}
}

// specPanelRowButton renders a real base.Button for clickable list, search, reference, and problem rows.
func specPanelRowButton(b specPanelRowButtonSpec) core.Node {
	baseStyle := core.Style{
		Direction:    core.Row,
		AlignItems:   core.AlignCenter,
		MinHeight:    core.Pt(specRowHeight),
		Width:        core.Pct(100),
		Gap:          specSpace8,
		Padding:      core.Symmetric(specSpace4, specSpace12),
		CornerRadius: specRadiusControl,
		Background:   specRaised,
		BorderWidth:  1,
		BorderColor:  specLine,
		Cursor:       core.CursorPointer,
		Hover:        &core.Style{Background: specLineRaised, BorderColor: specLineRaised},
		Active:       &core.Style{Background: specLineRaised},
		Disabled:     &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault},
		FocusVisible: specRowFocusRingStyle,
	}.Merge(b.Style)

	var onPress func(core.PressEvent)
	if b.OnPress != nil {
		onPress = func(core.PressEvent) { b.OnPress() }
	}
	return base.Button{
		TestID:    b.TestID,
		Label:     b.Label,
		LabelFunc: b.LabelFunc,
		Tooltip:   specTooltipNode(b.Tooltip),
		Disabled:  b.Disabled,
		Selected:  b.Selected,
		Style:     baseStyle,
		StyleFunc: b.StyleFunc,
		OnPress:   onPress,
		Children:  b.Children,
	}
}

// specInputStyle returns the Spec control style for text and numeric inputs (28 px height, 6 px radius).
func specInputStyle() core.Style {
	return core.Style{
		Height:           core.Pt(specRowHeight),
		CornerRadius:     specRadiusControl,
		FontSize:         12.5,
		Color:            specText,
		PlaceholderColor: specTextMuted,
		Background:       specRaised,
		BorderWidth:      1,
		BorderColor:      specLineRaised,
		CaretColor:       specAccent,
		Focus:            &core.Style{BorderColor: specAccent},
		FocusVisible:     specRowFocusRingStyle,
	}
}

// specNumberInputStyle returns the Spec control style for ui.NumberInput filling its container width
// (overriding ui.NumberInput's default 160 pt width so steppers stay inside the wrapper).
func specNumberInputStyle() core.Style {
	s := specInputStyle()
	s.Width = core.Pct(100)
	return s
}

// specSelectStyle returns the Spec control style for ui.Select controls filling their container width.
func specSelectStyle() core.Style {
	return core.Style{
		Width:        core.Pct(100),
		Height:       core.Pt(specRowHeight),
		CornerRadius: specRadiusControl,
		FontSize:     12.5,
		Color:        specText,
		Background:   specRaised,
		BorderWidth:  1,
		BorderColor:  specLineRaised,
		FocusVisible: specRowFocusRingStyle,
	}
}

// specStatusBanner renders a Spec problem (#FF6B6B + TriangleAlert) or warning (#F2C14E on #2A2414 + #4A3F1F) banner.
func specStatusBanner(testID string, isWarning bool, msg string, msgFunc func() string, action core.Node) core.Node {
	bg := specProblemFill
	border := specProblemEdge
	iconCol := specProblem
	if isWarning {
		bg = specWarningFill
		border = specWarningEdge
		iconCol = specWarning
	}
	children := []core.Node{
		specIcon(lucide.TriangleAlert, specIconSize, iconCol),
		core.Text{
			Content:     msg,
			ContentFunc: msgFunc,
			Style: core.Style{
				FontSize:   12.5,
				Color:      specText, // Spec contrast table: 11.33:1 AAA on warning fill, high contrast on problem tint.
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
			Background:   bg,
			BorderWidth:  1,
			BorderColor:  border,
			FlexShrink:   0,
		},
		Children: children,
	}
}

// specEmptyState renders a Spec empty state card with a leading Lucide icon, title, optional detail, and optional action.
func specEmptyState(testID string, icon func() *core.SVG, iconColor core.Color, title, detail string, action core.Node) core.Node {
	textChildren := []core.Node{
		core.Text{
			Content: title,
			Style: core.Style{
				FontSize:   12.5,
				FontWeight: core.FontWeightMedium,
				Color:      specText,
			},
		},
	}
	if detail != "" {
		textChildren = append(textChildren, core.Text{
			Content: detail,
			Style: core.Style{
				FontSize: 11.5,
				Color:    specTextMuted,
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
			Background:   specRaised,
			BorderWidth:  1,
			BorderColor:  specLine,
		},
		Children: children,
	}
}

// specLoadingState renders a Spec loading state card with a spinner and status label.
func specLoadingState(testID, label string) core.Node {
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
			Background:   specRaised,
			BorderWidth:  1,
			BorderColor:  specLine,
		},
		Children: []core.Node{
			ui.Spinner{
				Size:  specIconSize,
				Label: label,
				Tint:  func() core.Color { return specAccent },
			},
			core.Text{
				Content: label,
				Style: core.Style{
					FontSize: 12.5,
					Color:    specTextSecondary,
				},
			},
		},
	}
}

// specSeverityPill renders a small semantic badge (Problem or Warning) with icon + label.
func specSeverityPill(testID string, isWarning bool, text string) core.Node {
	bg := specProblemFill
	border := specProblemEdge
	fg := specProblem
	if isWarning {
		bg = specWarningFill
		border = specWarningEdge
		fg = specWarning
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
			Background:   bg,
			BorderWidth:  1,
			BorderColor:  border,
			FlexShrink:   0,
		},
		Children: []core.Node{
			specIcon(lucide.TriangleAlert, 12, fg),
			core.Text{
				Content: text,
				Style: core.Style{
					FontSize:   11,
					FontWeight: core.FontWeightSemibold,
					Color:      fg,
				},
			},
		},
	}
}
