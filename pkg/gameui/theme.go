package gameui

import "github.com/nsgo-dev/nsgo/core"

// Theme holds the colors, spacing, corner radius, borders, and default font used to lay out and draw a game UI.
type Theme struct {
	// PanelBackground and PanelBorder style Panel containers.
	PanelBackground core.Color
	PanelBorder     core.Color

	// Background, Hover, Active, Disabled, Border, and FocusedBorder style interactive controls.
	Background    core.Color
	Hover         core.Color
	Active        core.Color
	FocusedBorder core.Color
	Disabled      core.Color
	Border        core.Color

	// Text, DisabledText, MutedText, Accent, AccentHover, AccentActive, and Track style labels, toggles, sliders, and progress bars.
	Text         core.Color
	DisabledText core.Color
	MutedText    core.Color
	Accent       core.Color
	AccentHover  core.Color
	AccentActive core.Color
	Track        core.Color

	// Padding is the default inner padding used for widget intrinsic sizing and spacing.
	Padding float32
	// CornerRadius is the default corner radius for rounded widget surfaces.
	CornerRadius float32
	// BorderWidth is the stroke width for widget borders.
	BorderWidth float32
	// FocusWidth is the stroke width for the focused outline.
	FocusWidth float32
	// Font is the default font used by text-bearing widgets.
	Font core.Font

	// PanelFrame optionally draws stylized or nine-slice frames for Panel widgets.
	PanelFrame FrameDrawer
	// ButtonFrame optionally draws stylized or nine-slice frames for Button widgets.
	ButtonFrame FrameDrawer
	// FrameDrawer optionally draws stylized or nine-slice frames when a widget has no specific frame drawer.
	FrameDrawer FrameDrawer
	// TextDrawer optionally replaces default Canvas FillText rendering (for example, with a bitmap font renderer).
	TextDrawer TextDrawer
	// TextMeasurer optionally provides text dimensions for layout when using a custom TextDrawer.
	TextMeasurer TextMeasurer
	// FocusRing optionally customizes the focus ring drawn around focused widgets.
	FocusRing *FocusRingStyle
}

// DefaultTheme returns a high-contrast dark theme suited for in-game overlays and menus.
func DefaultTheme() Theme {
	return Theme{
		PanelBackground: core.RGBA(0x161b26, 0.92),
		PanelBorder:     core.RGB(0x334155),
		Background:      core.RGB(0x243044),
		Hover:           core.RGB(0x334563),
		Active:          core.RGB(0x1d2738),
		FocusedBorder:   core.RGB(0xfbbf24),
		Disabled:        core.RGB(0x1c2331),
		Border:          core.RGB(0x475569),
		Text:            core.RGB(0xf8fafc),
		DisabledText:    core.RGB(0x64748b),
		MutedText:       core.RGB(0x94a3b8),
		Accent:          core.RGB(0x38bdf8),
		AccentHover:     core.RGB(0x7dd3fc),
		AccentActive:    core.RGB(0x0284c7),
		Track:           core.RGB(0x0f172a),
		Padding:         12,
		CornerRadius:    6,
		BorderWidth:     1.5,
		FocusWidth:      2.5,
		Font: core.Font{
			Size:   16,
			Weight: core.FontWeightMedium,
		},
	}
}

// resolveTheme fills zero fields in t from DefaultTheme so partial custom themes remain usable.
func resolveTheme(t *Theme) Theme {
	def := DefaultTheme()
	if t == nil {
		return def
	}
	out := *t
	if out.PanelBackground == 0 {
		out.PanelBackground = def.PanelBackground
	}
	if out.PanelBorder == 0 {
		out.PanelBorder = def.PanelBorder
	}
	if out.Background == 0 {
		out.Background = def.Background
	}
	if out.Hover == 0 {
		out.Hover = def.Hover
	}
	if out.Active == 0 {
		out.Active = def.Active
	}
	if out.FocusedBorder == 0 {
		out.FocusedBorder = def.FocusedBorder
	}
	if out.Disabled == 0 {
		out.Disabled = def.Disabled
	}
	if out.Border == 0 {
		out.Border = def.Border
	}
	if out.Text == 0 {
		out.Text = def.Text
	}
	if out.DisabledText == 0 {
		out.DisabledText = def.DisabledText
	}
	if out.MutedText == 0 {
		out.MutedText = def.MutedText
	}
	if out.Accent == 0 {
		out.Accent = def.Accent
	}
	if out.AccentHover == 0 {
		out.AccentHover = def.AccentHover
	}
	if out.AccentActive == 0 {
		out.AccentActive = def.AccentActive
	}
	if out.Track == 0 {
		out.Track = def.Track
	}
	if out.BorderWidth <= 0 {
		out.BorderWidth = def.BorderWidth
	}
	if out.FocusWidth <= 0 {
		out.FocusWidth = def.FocusWidth
	}
	if out.Font.Size <= 0 {
		out.Font.Size = def.Font.Size
	}
	if out.Font.Weight == 0 {
		out.Font.Weight = def.Font.Weight
	}
	return out
}

// resolveFont merges an optional per-widget Font override onto the theme's base font.
func resolveFont(override, base core.Font) core.Font {
	f := base
	if override.Family != "" {
		f.Family = override.Family
	}
	if override.Language != "" {
		f.Language = override.Language
	}
	if override.Size > 0 {
		f.Size = override.Size
	}
	if override.Weight != 0 {
		f.Weight = override.Weight
	}
	if override.Italic {
		f.Italic = true
	}
	if override.Variant != 0 {
		f.Variant = override.Variant
	}
	if f.Size <= 0 {
		f.Size = core.DefaultFontSize
	}
	return f
}
