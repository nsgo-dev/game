package editor

import (
	_ "embed"
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

//go:embed spec_theme.yaml
var specThemeYAML []byte

// Typography tokens from design/editor/Spec.dc.html ("Type, space, shape").
const (
	SpecFontFamilySans = "Inter"
	SpecFontFamilyMono = "JetBrains Mono"

	SpecFontTitleSize     core.Points     = 14
	SpecFontTitleWeight   core.FontWeight = core.FontWeightSemibold // 600
	SpecFontBodySize      core.Points     = 12.5
	SpecFontBodyWeight    core.FontWeight = core.FontWeightNormal // 400
	SpecFontSectionSize   core.Points     = 11
	SpecFontSectionWeight core.FontWeight = core.FontWeightSemibold // 600
	SpecFontSectionTrack  core.Points     = 0.66                    // +6% tracking on 11 px
	SpecFontCodeSize      core.Points     = 11.5
	SpecFontCodeWeight    core.FontWeight = core.FontWeightNormal // 400
)

// Spacing scale tokens from design/editor/Spec.dc.html: 4, 8, 12, 16, 24 px only.
const (
	SpecSpace4  core.Points = 4
	SpecSpace8  core.Points = 8
	SpecSpace12 core.Points = 12
	SpecSpace16 core.Points = 16
	SpecSpace24 core.Points = 24
)

// Corner radius tokens from design/editor/Spec.dc.html.
const (
	SpecRadiusControl core.Points = 6  // Controls, inputs, tree rows, pills
	SpecRadiusButton  core.Points = 8  // Buttons
	SpecRadiusCard    core.Points = 12 // Cards, popovers, toasts
	SpecRadiusDialog  core.Points = 18 // Dialogs
)

// Height, border, and icon geometry tokens from design/editor/Spec.dc.html.
const (
	SpecRowHeight        core.Points = 28 // Rows and tool buttons
	SpecMinHitSize       core.Points = 28 // Minimum interactive hit area (28 x 28)
	SpecButtonHeight     core.Points = 30 // Standard button height (30-32 px)
	SpecButtonTallHeight core.Points = 32 // Tall button height (30-32 px)
	SpecHeaderHeight     core.Points = 44 // Header / toolbar height
	SpecBorderWidth      core.Points = 1  // 1 px inner borders in Line or LineRaised
	SpecIconSize         core.Points = 15 // Inline stroke icon size
	SpecIconStrokeWidth  float32     = 1.8
)

// Motion tokens from design/editor/Spec.dc.html ("Motion").
// All durations drop to 0 under Env.ReduceMotion (see SpecMotionDuration).
const (
	SpecMotionHover      = 120 * time.Millisecond  // Hover and press transitions
	SpecMotionPanel      = 180 * time.Millisecond  // Panels, popovers, and toasts
	SpecMotionOverlay    = 240 * time.Millisecond  // Agent change overlays fading in
	SpecMotionAgentPulse = 1600 * time.Millisecond // Agent outline pulse (100% <-> 45% opacity)

	SpecAgentPulseMinOpacity float32 = 0.45
	SpecAgentPulseMaxOpacity float32 = 1.0
)

// SpecMotionDuration returns d when motion is enabled, or 0 when cx is non-nil and
// cx.Env().ReduceMotion() is true.
func SpecMotionDuration(cx *core.Scope, d time.Duration) time.Duration {
	if cx != nil && cx.Env().ReduceMotion() {
		return 0
	}
	return d
}

// SpecAgentPulseOpacity returns the Spec agent-working outline opacity in [0.45, 1.0]
// for elapsed time since the pulse began. Under Env.ReduceMotion it stays solid at 1.0.
func SpecAgentPulseOpacity(cx *core.Scope, elapsed time.Duration) float32 {
	if cx != nil && cx.Env().ReduceMotion() {
		return SpecAgentPulseMaxOpacity
	}
	if SpecMotionAgentPulse <= 0 {
		return SpecAgentPulseMaxOpacity
	}
	phase := float64(elapsed%SpecMotionAgentPulse) / float64(SpecMotionAgentPulse)
	t := 0.5 * (1 + math.Cos(2*math.Pi*phase))
	return SpecAgentPulseMinOpacity + float32(t)*(SpecAgentPulseMaxOpacity-SpecAgentPulseMinOpacity)
}

var (
	parsedSpecTheme, darkSpecPalette, lightSpecPalette = loadSpecTheme()
)

func loadSpecTheme() (*core.Theme, SpecPalette, SpecPalette) {
	th, err := core.ParseTheme(specThemeYAML)
	if err != nil {
		panic(fmt.Sprintf("editor: parse embedded spec_theme.yaml: %v", err))
	}
	dark := extractSpecPalette(&th.Dark, true)
	light := extractSpecPalette(&th.Light, false)
	return th, dark, light
}

// SpecRole holds the coordinated colors for a semantic Spec token role
// (Accent, Agent, Added, Changed, Removed/Problem, or Warning).
type SpecRole struct {
	// Fg is the foreground text, icon, glyph, or solid badge fill color.
	Fg core.Color
	// Fill is the tinted surface or pill fill color behind Fg or body text.
	Fill core.Color
	// Edge is the border, focus ring, or viewport overlay outline stroke color.
	Edge core.Color
	// OnFill is the high-contrast ink drawn directly on a solid Fg fill (>= 4.5:1 AA).
	// It is never white on a fill that fails WCAG AA (for example dark Accent #3B7BFF or dark Agent #9B7BFF).
	OnFill core.Color
}

// SpecPalette holds every color token defined by design/editor/Spec.dc.html
// in either the Dark or Light appearance.
type SpecPalette struct {
	// Dark reports whether this palette is the dark appearance.
	Dark bool

	// Surfaces.
	Bg          core.Color // Window and outer background (`bg`).
	Panel       core.Color // Side panels, inspector, agent panel (`panel`).
	Raised      core.Color // Cards, composer, toasts, popovers (`raised`).
	Inset       core.Color // Recessed wells, tracks, sidebar rails (`inset`).
	Viewport    core.Color // Viewport canvas surround (`.vp`).
	ViewportDot core.Color // Viewport dot grid mark.
	PillBg      core.Color // Floating overlay pill background.

	// Borders.
	Line       core.Color // Dividers between regions (`line`).
	LineRaised core.Color // Edges of raised surfaces and quiet button fills (`line raised`).

	// Four text levels, all meeting WCAG 2.2 AA (>= 4.5:1) on Bg, Panel, and Raised.
	TextPrimary   core.Color // Spec `text strong`: titles, selected rows, primary button fill.
	TextSecondary core.Color // Spec `text`: body, messages, values.
	TextMuted     core.Color // Spec `text secondary`: labels, tree rows, file names.
	TextFaint     core.Color // Spec `text muted`: hints, placeholders, meta, section labels.
	OnTextPrimary core.Color // Ink drawn on a solid TextPrimary primary button fill (Play, Send, Create).

	// Semantic roles, each carrying Fg, Fill, Edge, and OnFill.
	Accent  SpecRole // Selection, focus ring, Keep; solid.
	Agent   SpecRole // Agent working; solid violet, never blue.
	Added   SpecRole // New objects and lines; dashed.
	Changed SpecRole // Edited objects and values; dashed.
	Removed SpecRole // Deleted objects and problems/errors; solid.
	Warning SpecRole // Permission asks, waiting, warning toasts (with dedicated Fill and Edge).

	// Surface interaction overlays and 3D viewport / axis tokens (design/editor/Viewport3D.dc.html).
	HoverOverlay     core.Color // Subtle hover wash over surfaces (`surfaceHover`).
	ActiveOverlay    core.Color // Subtle pressed wash over surfaces (`surfacePressed`).
	PrimaryHover     core.Color // Hover fill for solid TextPrimary buttons (`specPrimaryHover`).
	AccentHover      core.Color // Hover fill for solid Accent buttons (`accentHover`).
	AccentPressed    core.Color // Pressed fill for solid Accent buttons (`accentPressed`).
	Viewport3DGround core.Color // 3D viewport ground plane (`specViewport3DGround`).
	Viewport3DGrid   core.Color // 3D viewport grid line (`specViewport3DGrid`).
	AxisX            core.Color // 3D X axis token (`specAxisX`, #FF6B6B dark / #C92A2A light).
	AxisY            core.Color // 3D Y axis token (`specAxisY`, #4FD38A dark / #0D7A43 light).
	AxisZ            core.Color // 3D Z axis token (`specAxisZ`, #3B7BFF dark / #1D5CE6 light).
}

// Problem returns the semantic role shared by removed objects and problems/errors (Spec `removed · problem`).
func (p SpecPalette) Problem() SpecRole { return p.Removed }

// FocusRing returns the Spec keyboard focus ring style (2 px Accent.Edge outline with the given offset).
func (p SpecPalette) FocusRing(offset core.Points) *core.Style {
	return &core.Style{
		OutlineWidth:  2,
		OutlineColor:  p.Accent.Edge,
		OutlineOffset: offset,
	}
}

// SpecThemeYAML returns a copy of the embedded YAML theme file parsed by core.ParseTheme.
func SpecThemeYAML() []byte {
	return append([]byte(nil), specThemeYAML...)
}

// SpecTheme returns a clone of the editor's Spec *core.Theme containing both dark and light palettes,
// mapped onto every NSGo core.Palette and ui.Colors role as well as all Spec-specific tokens.
func SpecTheme() *core.Theme {
	return parsedSpecTheme.Clone()
}

// DarkSpecPalette returns the dark Spec token palette from design/editor/Spec.dc.html.
func DarkSpecPalette() SpecPalette {
	return darkSpecPalette
}

// LightSpecPalette returns the light Spec token palette measured against the same WCAG 2.2 contrast table.
func LightSpecPalette() SpecPalette {
	return lightSpecPalette
}

// SpecPaletteFor returns DarkSpecPalette when dark is true and LightSpecPalette otherwise.
func SpecPaletteFor(dark bool) SpecPalette {
	if dark {
		return darkSpecPalette
	}
	return lightSpecPalette
}

// SpecPalette returns the active SpecPalette for cx when non-nil (following cx.Dark(), which tracks
// Scope.SetAppearance, Scope.SetSubtreeAppearance, and live system appearance changes), or e.Dark() otherwise.
func (e *Editor) SpecPalette(cx *core.Scope) SpecPalette {
	if cx != nil {
		return SpecPaletteFor(cx.Dark())
	}
	return e.activeSpecPalette()
}

// activeSpecPalette returns the active SpecPalette for e, defaulting to dark when e is nil.
func (e *Editor) activeSpecPalette() SpecPalette {
	if e == nil {
		return darkSpecPalette
	}
	return SpecPaletteFor(e.Dark())
}

// specAlpha returns c with opacity a clamped to [0, 1].
func specAlpha(c core.Color, a float32) core.Color {
	return ui.Alpha(c, a)
}

// colorFromRGBA8 packs dynamic raster pixel or computed data channels into a core.Color.
func colorFromRGBA8(r, g, b, a uint8) core.Color {
	return core.Color(uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | uint32(a))
}

func mustPaletteColor(p *core.Palette, name string) core.Color {
	c, ok := p.Color(name)
	if !ok || c == 0 {
		panic(fmt.Sprintf("editor: spec_theme.yaml missing color token %q", name))
	}
	return c
}

func extractSpecPalette(p *core.Palette, dark bool) SpecPalette {
	role := func(prefix string) SpecRole {
		return SpecRole{
			Fg:     mustPaletteColor(p, prefix+"Fg"),
			Fill:   mustPaletteColor(p, prefix+"Fill"),
			Edge:   mustPaletteColor(p, prefix+"Edge"),
			OnFill: mustPaletteColor(p, prefix+"OnFill"),
		}
	}
	// Ensure ring focus token is declared directly in spec_theme.yaml.
	_ = mustPaletteColor(p, "ring")
	return SpecPalette{
		Dark:             dark,
		Bg:               mustPaletteColor(p, "specBg"),
		Panel:            mustPaletteColor(p, "specPanel"),
		Raised:           mustPaletteColor(p, "specRaised"),
		Inset:            mustPaletteColor(p, "specInset"),
		Viewport:         mustPaletteColor(p, "specViewport"),
		ViewportDot:      mustPaletteColor(p, "specViewportDot"),
		PillBg:           mustPaletteColor(p, "specPillBg"),
		Line:             mustPaletteColor(p, "specLine"),
		LineRaised:       mustPaletteColor(p, "specLineRaised"),
		TextPrimary:      mustPaletteColor(p, "specTextPrimary"),
		TextSecondary:    mustPaletteColor(p, "specTextSecondary"),
		TextMuted:        mustPaletteColor(p, "specTextMuted"),
		TextFaint:        mustPaletteColor(p, "specTextFaint"),
		OnTextPrimary:    mustPaletteColor(p, "specOnTextPrimary"),
		Accent:           role("specAccent"),
		Agent:            role("specAgent"),
		Added:            role("specAdded"),
		Changed:          role("specChanged"),
		Removed:          role("specRemoved"),
		Warning:          role("specWarning"),
		HoverOverlay:     p.SurfaceHover,
		ActiveOverlay:    p.SurfacePressed,
		PrimaryHover:     mustPaletteColor(p, "specPrimaryHover"),
		AccentHover:      p.AccentHover,
		AccentPressed:    p.AccentPressed,
		Viewport3DGround: mustPaletteColor(p, "specViewport3DGround"),
		Viewport3DGrid:   mustPaletteColor(p, "specViewport3DGrid"),
		AxisX:            mustPaletteColor(p, "specAxisX"),
		AxisY:            mustPaletteColor(p, "specAxisY"),
		AxisZ:            mustPaletteColor(p, "specAxisZ"),
	}
}

// RelativeLuminance returns the WCAG 2.1 / 2.2 relative luminance of c in [0, 1].
func RelativeLuminance(c core.Color) float64 {
	r, g, b, _ := c.Components()
	linearize := func(v float64) float64 {
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*linearize(r) + 0.7152*linearize(g) + 0.0722*linearize(b)
}

// CompositeOver alpha-blends fg over an opaque background bg in sRGB space.
func CompositeOver(fg, bg core.Color) core.Color {
	fr, fgC, fb, fa := fg.Components()
	if fa >= 1 {
		return fg
	}
	br, bgC, bb, _ := bg.Components()
	r := fr*fa + br*(1-fa)
	g := fgC*fa + bgC*(1-fa)
	b := fb*fa + bb*(1-fa)
	u := func(v float64) uint32 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint32(math.Round(v * 255))
	}
	return core.RGB(u(r)<<16 | u(g)<<8 | u(b))
}

// ContrastRatio returns the WCAG 2.1 / 2.2 contrast ratio between foreground a and background b,
// in [1, 21]. If a is translucent (alpha < 1), it is composited over b first.
func ContrastRatio(a, b core.Color) float64 {
	if _, _, _, alpha := a.Components(); alpha < 1 {
		a = CompositeOver(a, b)
	}
	la := RelativeLuminance(a)
	lb := RelativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// SpecContrastPair is one foreground/background measurement row in the Spec's WCAG 2.2 contrast table.
type SpecContrastPair struct {
	Name       string
	Foreground core.Color
	Background core.Color
	MinRatio   float64
	Usage      string
}

// Ratio returns the measured WCAG 2.2 contrast ratio for this pair.
func (p SpecContrastPair) Ratio() float64 {
	return ContrastRatio(p.Foreground, p.Background)
}

// SpecContrastTable returns every foreground/background and overlay contrast pair required by
// design/editor/Spec.dc.html for palette p (Dark or Light), with its WCAG 2.2 minimum threshold.
func SpecContrastTable(p SpecPalette) []SpecContrastPair {
	return []SpecContrastPair{
		// Core text levels on surfaces (Spec Contrast table).
		{Name: "text strong on panel", Foreground: p.TextPrimary, Background: p.Panel, MinRatio: 4.5, Usage: "Titles, selected tree rows"},
		{Name: "text strong on bg", Foreground: p.TextPrimary, Background: p.Bg, MinRatio: 4.5, Usage: "Window titles, status headings"},
		{Name: "text strong on raised", Foreground: p.TextPrimary, Background: p.Raised, MinRatio: 4.5, Usage: "Card and toast titles"},
		{Name: "text on panel", Foreground: p.TextSecondary, Background: p.Panel, MinRatio: 4.5, Usage: "Body, agent messages"},
		{Name: "text on raised", Foreground: p.TextSecondary, Background: p.Raised, MinRatio: 4.5, Usage: "Cards, composer, toasts"},
		{Name: "text on bg", Foreground: p.TextSecondary, Background: p.Bg, MinRatio: 4.5, Usage: "Window body copy"},
		{Name: "text on inset", Foreground: p.TextSecondary, Background: p.Inset, MinRatio: 4.5, Usage: "Inset fields and code blocks"},
		{Name: "text on line raised", Foreground: p.TextSecondary, Background: p.LineRaised, MinRatio: 4.5, Usage: "Quiet secondary buttons"},
		{Name: "secondary on panel", Foreground: p.TextMuted, Background: p.Panel, MinRatio: 4.5, Usage: "Labels, tree rows, file names"},
		{Name: "secondary on raised", Foreground: p.TextMuted, Background: p.Raised, MinRatio: 4.5, Usage: "Secondary text in cards"},
		{Name: "secondary on bg", Foreground: p.TextMuted, Background: p.Bg, MinRatio: 4.5, Usage: "Viewport labels and legends"},
		{Name: "muted on bg", Foreground: p.TextFaint, Background: p.Bg, MinRatio: 4.5, Usage: "Status bar, viewport captions"},
		{Name: "muted on panel", Foreground: p.TextFaint, Background: p.Panel, MinRatio: 4.5, Usage: "Hints, meta, section labels, diff line numbers"},
		{Name: "muted on raised", Foreground: p.TextFaint, Background: p.Raised, MinRatio: 4.5, Usage: "Placeholders, queued agent steps"},
		{Name: "muted on inset", Foreground: p.TextFaint, Background: p.Inset, MinRatio: 4.5, Usage: "Recessed hints and captions"},

		// Filled primary and accent buttons & tags.
		{Name: "onAccent on accent", Foreground: p.Accent.OnFill, Background: p.Accent.Fg, MinRatio: 4.5, Usage: "Keep button, Allow once, selection name tag; weight 600"},
		{Name: "onAccent on accentHover", Foreground: p.Accent.OnFill, Background: p.AccentHover, MinRatio: 4.5, Usage: "Hovered accent button fill"},
		{Name: "onAccent on accentPressed", Foreground: p.Accent.OnFill, Background: p.AccentPressed, MinRatio: 4.5, Usage: "Pressed accent button fill"},
		{Name: "onTextStrong on text strong", Foreground: p.OnTextPrimary, Background: p.TextPrimary, MinRatio: 4.5, Usage: "Primary buttons: Play, Send, Create"},
		{Name: "onTextStrong on primaryHover", Foreground: p.OnTextPrimary, Background: p.PrimaryHover, MinRatio: 4.5, Usage: "Hovered primary button fill"},
		{Name: "onAgent on agent", Foreground: p.Agent.OnFill, Background: p.Agent.Fg, MinRatio: 4.5, Usage: "Text on solid agent fill"},
		{Name: "onAdded on added", Foreground: p.Added.OnFill, Background: p.Added.Fg, MinRatio: 4.5, Usage: "Text on solid added fill"},
		{Name: "onChanged on changed", Foreground: p.Changed.OnFill, Background: p.Changed.Fg, MinRatio: 4.5, Usage: "Text on solid changed fill"},
		{Name: "onWarning on warning", Foreground: p.Warning.OnFill, Background: p.Warning.Fg, MinRatio: 4.5, Usage: "Text on solid warning fill"},
		{Name: "onProblem on problem", Foreground: p.Removed.OnFill, Background: p.Removed.Fg, MinRatio: 4.5, Usage: "Problem filled pill on live object"},

		// Warning card & toast surface.
		{Name: "warning on warning fill", Foreground: p.Warning.Fg, Background: p.Warning.Fill, MinRatio: 4.5, Usage: "Permission title, warning icon"},
		{Name: "text on warning fill", Foreground: p.TextSecondary, Background: p.Warning.Fill, MinRatio: 4.5, Usage: "Permission body and warning toast text"},

		// Semantic text & glyphs on surfaces and pill fills.
		{Name: "agent on panel", Foreground: p.Agent.Fg, Background: p.Panel, MinRatio: 4.5, Usage: "Agent glyph, step spinner, agent labels"},
		{Name: "agent on raised", Foreground: p.Agent.Fg, Background: p.Raised, MinRatio: 4.5, Usage: "Agent badges on cards"},
		{Name: "agent on agent fill", Foreground: p.Agent.Fg, Background: p.Agent.Fill, MinRatio: 4.5, Usage: "Agent banner and pill text"},
		{Name: "agent on pill bg", Foreground: p.Agent.Fg, Background: p.PillBg, MinRatio: 4.5, Usage: "Agent working overlay pill glyph"},
		{Name: "text on pill bg", Foreground: p.TextSecondary, Background: p.PillBg, MinRatio: 4.5, Usage: "Agent working overlay pill label"},
		{Name: "added on panel", Foreground: p.Added.Fg, Background: p.Panel, MinRatio: 4.5, Usage: "Added diff signs and status badges"},
		{Name: "added on added fill", Foreground: p.Added.Fg, Background: p.Added.Fill, MinRatio: 4.5, Usage: "Added pill text on added fill"},
		{Name: "added on pill bg", Foreground: p.Added.Fg, Background: p.PillBg, MinRatio: 4.5, Usage: "Added viewport overlay pill text"},
		{Name: "changed on panel", Foreground: p.Changed.Fg, Background: p.Panel, MinRatio: 4.5, Usage: "Changed diff signs and status badges"},
		{Name: "changed on changed fill", Foreground: p.Changed.Fg, Background: p.Changed.Fill, MinRatio: 4.5, Usage: "Changed pill text on changed fill"},
		{Name: "changed on pill bg", Foreground: p.Changed.Fg, Background: p.PillBg, MinRatio: 4.5, Usage: "Changed viewport overlay pill text"},
		{Name: "removed on panel", Foreground: p.Removed.Fg, Background: p.Panel, MinRatio: 4.5, Usage: "Removed/problem diff signs and errors"},
		{Name: "removed on removed fill", Foreground: p.Removed.Fg, Background: p.Removed.Fill, MinRatio: 4.5, Usage: "Removed pill text on removed fill"},
		{Name: "removed on pill bg", Foreground: p.Removed.Fg, Background: p.PillBg, MinRatio: 4.5, Usage: "Removed viewport overlay pill text"},
		{Name: "accent on accent fill", Foreground: p.Accent.Fg, Background: p.Accent.Fill, MinRatio: 4.5, Usage: "Accent chip text on accent tint"},

		// Non-text UI controls, focus rings, and viewport overlays (WCAG 2.2 3:1).
		{Name: "accent edge on panel", Foreground: p.Accent.Edge, Background: p.Panel, MinRatio: 3.0, Usage: "Focus ring and selection border on panel"},
		{Name: "accent edge on raised", Foreground: p.Accent.Edge, Background: p.Raised, MinRatio: 3.0, Usage: "Focus ring on raised cards and toasts"},
		{Name: "accent edge on bg", Foreground: p.Accent.Edge, Background: p.Bg, MinRatio: 3.0, Usage: "Selection outline on window surround"},
		{Name: "accent edge on viewport", Foreground: p.Accent.Edge, Background: p.Viewport, MinRatio: 3.0, Usage: "Selection outline on viewport surround"},
		{Name: "agent edge on viewport", Foreground: p.Agent.Edge, Background: p.Viewport, MinRatio: 3.0, Usage: "Agent working outline on viewport surround"},
		{Name: "added edge on viewport", Foreground: p.Added.Edge, Background: p.Viewport, MinRatio: 3.0, Usage: "Added dashed outline on viewport surround"},
		{Name: "changed edge on viewport", Foreground: p.Changed.Edge, Background: p.Viewport, MinRatio: 3.0, Usage: "Changed dashed outline on viewport surround"},
		{Name: "removed edge on viewport", Foreground: p.Removed.Edge, Background: p.Viewport, MinRatio: 3.0, Usage: "Removed/problem outline on viewport surround"},

		// 3D viewport axis tokens from design/editor/Viewport3D.dc.html (WCAG 2.2 3:1).
		{Name: "3D axis X on viewport", Foreground: p.AxisX, Background: p.Viewport, MinRatio: 3.0, Usage: "3D X gizmo, axis widget, and transform X label"},
		{Name: "3D axis Y on viewport", Foreground: p.AxisY, Background: p.Viewport, MinRatio: 3.0, Usage: "3D Y gizmo, axis widget, and transform Y label"},
		{Name: "3D axis Z on viewport", Foreground: p.AxisZ, Background: p.Viewport, MinRatio: 3.0, Usage: "3D Z gizmo, axis widget, and transform Z label"},
	}
}
