// Package theme holds the NSGo editor's design tokens from design/editor/Spec.dc.html: the embedded YAML theme, the dark and light palettes, type, spacing, shape and motion constants, and the WCAG 2.2 contrast table every palette is measured against.
package theme

import (
	_ "embed"
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

//go:embed theme.yaml
var themeYAML []byte

// Typography tokens from design/editor/Spec.dc.html ("Type, space, shape").
const (
	FontFamilySans = "Inter"
	FontFamilyMono = "JetBrains Mono"

	FontTitleSize     core.Points     = 14
	FontTitleWeight   core.FontWeight = core.FontWeightSemibold // 600
	FontBodySize      core.Points     = 12.5
	FontBodyWeight    core.FontWeight = core.FontWeightNormal // 400
	FontSectionSize   core.Points     = 11
	FontSectionWeight core.FontWeight = core.FontWeightSemibold // 600
	FontSectionTrack  core.Points     = 0.66                    // +6% tracking on 11 px
	FontCodeSize      core.Points     = 11.5
	FontCodeWeight    core.FontWeight = core.FontWeightNormal // 400
)

// Spacing scale tokens from design/editor/Spec.dc.html: 4, 8, 12, 16, 24 px only.
const (
	Space4  core.Points = 4
	Space8  core.Points = 8
	Space12 core.Points = 12
	Space16 core.Points = 16
	Space24 core.Points = 24
)

// Corner radius tokens from design/editor/Spec.dc.html.
const (
	RadiusControl core.Points = 6  // Controls, inputs, tree rows, pills
	RadiusButton  core.Points = 8  // Buttons
	RadiusCard    core.Points = 12 // Cards, popovers, toasts
	RadiusDialog  core.Points = 18 // Dialogs
)

// Height, border, and icon geometry tokens from design/editor/Spec.dc.html.
const (
	RowHeight        core.Points = 28 // Rows and tool buttons
	MinHitSize       core.Points = 28 // Minimum interactive hit area (28 x 28)
	ButtonHeight     core.Points = 30 // Standard button height (30-32 px)
	ButtonTallHeight core.Points = 32 // Tall button height (30-32 px)
	HeaderHeight     core.Points = 44 // Header / toolbar height
	BorderWidth      core.Points = 1  // 1 px inner borders in Line or LineRaised
	IconSize         core.Points = 15 // Inline stroke icon size
	IconStrokeWidth  float32     = 1.8
)

// Motion tokens from design/editor/Spec.dc.html ("Motion"). All durations drop to 0 under Env.ReduceMotion (see MotionDuration).
const (
	MotionHover      = 120 * time.Millisecond  // Hover and press transitions
	MotionPanel      = 180 * time.Millisecond  // Panels, popovers, and toasts
	MotionOverlay    = 240 * time.Millisecond  // Agent change overlays fading in
	MotionAgentPulse = 1600 * time.Millisecond // Agent outline pulse (100% <-> 45% opacity)

	AgentPulseMinOpacity float32 = 0.45
	AgentPulseMaxOpacity float32 = 1.0
)

// MotionDuration returns d when motion is enabled, or 0 when cx is non-nil and cx.Env().ReduceMotion() is true.
func MotionDuration(cx *core.Scope, d time.Duration) time.Duration {
	if cx != nil && cx.Env().ReduceMotion() {
		return 0
	}
	return d
}

// AgentPulseOpacity returns the Spec agent-working outline opacity in [0.45, 1.0] for elapsed time since the pulse began. Under Env.ReduceMotion it stays solid at 1.0.
func AgentPulseOpacity(cx *core.Scope, elapsed time.Duration) float32 {
	if cx != nil && cx.Env().ReduceMotion() {
		return AgentPulseMaxOpacity
	}
	if MotionAgentPulse <= 0 {
		return AgentPulseMaxOpacity
	}
	phase := float64(elapsed%MotionAgentPulse) / float64(MotionAgentPulse)
	t := 0.5 * (1 + math.Cos(2*math.Pi*phase))
	return AgentPulseMinOpacity + float32(t)*(AgentPulseMaxOpacity-AgentPulseMinOpacity)
}

var (
	parsed, darkPalette, lightPalette = load()
)

func load() (*core.Theme, Palette, Palette) {
	th, err := core.ParseTheme(themeYAML)
	if err != nil {
		panic(fmt.Sprintf("theme: parse embedded theme.yaml: %v", err))
	}
	dark := extractPalette(&th.Dark, true)
	light := extractPalette(&th.Light, false)
	return th, dark, light
}

// Role holds the coordinated colors for a semantic Spec token role (Accent, Agent, Added, Changed, Removed/Problem, or Warning).
type Role struct {
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

// Palette holds every color token defined by design/editor/Spec.dc.html in either the Dark or Light appearance.
type Palette struct {
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
	Accent  Role // Selection, focus ring, Keep; solid.
	Agent   Role // Agent working; solid violet, never blue.
	Added   Role // New objects and lines; dashed.
	Changed Role // Edited objects and values; dashed.
	Removed Role // Deleted objects and problems/errors; solid.
	Warning Role // Permission asks, waiting, warning toasts (with dedicated Fill and Edge).

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
func (p Palette) Problem() Role { return p.Removed }

// FocusRing returns the Spec keyboard focus ring style (2 px Accent.Edge outline with the given offset).
func (p Palette) FocusRing(offset core.Points) *core.Style {
	return &core.Style{
		OutlineWidth:  2,
		OutlineColor:  p.Accent.Edge,
		OutlineOffset: offset,
	}
}

// YAML returns a copy of the embedded YAML theme file parsed by core.ParseTheme.
func YAML() []byte {
	return append([]byte(nil), themeYAML...)
}

// Core returns a clone of the editor's Spec *core.Theme containing both dark and light palettes, mapped onto every NSGo core.Palette and ui.Colors role as well as all Spec-specific tokens.
func Core() *core.Theme {
	return parsed.Clone()
}

// Dark returns the dark Spec token palette from design/editor/Spec.dc.html.
func Dark() Palette {
	return darkPalette
}

// Light returns the light Spec token palette measured against the same WCAG 2.2 contrast table.
func Light() Palette {
	return lightPalette
}

// PaletteFor returns Dark when dark is true and Light otherwise.
func PaletteFor(dark bool) Palette {
	if dark {
		return darkPalette
	}
	return lightPalette
}

func mustPaletteColor(p *core.Palette, name string) core.Color {
	c, ok := p.Color(name)
	if !ok || c == 0 {
		panic(fmt.Sprintf("theme: theme.yaml missing color token %q", name))
	}
	return c
}

func extractPalette(p *core.Palette, dark bool) Palette {
	role := func(prefix string) Role {
		return Role{
			Fg:     mustPaletteColor(p, prefix+"Fg"),
			Fill:   mustPaletteColor(p, prefix+"Fill"),
			Edge:   mustPaletteColor(p, prefix+"Edge"),
			OnFill: mustPaletteColor(p, prefix+"OnFill"),
		}
	}
	// Ensure ring focus token is declared directly in theme.yaml.
	_ = mustPaletteColor(p, "ring")
	return Palette{
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

// ContrastRatio returns the WCAG 2.1 / 2.2 contrast ratio between foreground a and background b, in [1, 21]. If a is translucent (alpha < 1), it is composited over b first.
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

// ContrastPair is one foreground/background measurement row in the Spec's WCAG 2.2 contrast table.
type ContrastPair struct {
	Name       string
	Foreground core.Color
	Background core.Color
	MinRatio   float64
	Usage      string
}

// Ratio returns the measured WCAG 2.2 contrast ratio for this pair.
func (p ContrastPair) Ratio() float64 {
	return ContrastRatio(p.Foreground, p.Background)
}

// ContrastTable returns every foreground/background and overlay contrast pair required by design/editor/Spec.dc.html for palette p (Dark or Light), with its WCAG 2.2 minimum threshold.
func ContrastTable(p Palette) []ContrastPair {
	return []ContrastPair{
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
