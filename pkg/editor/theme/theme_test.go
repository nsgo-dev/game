package theme_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestThemeParsesAndMapsAllRoles(t *testing.T) {
	th, err := core.ParseTheme(theme.YAML())
	if err != nil {
		t.Fatalf("core.ParseTheme(theme.YAML): %v", err)
	}
	if th.Radius != 8 {
		t.Fatalf("theme.Core().Radius = %v, want 8", th.Radius)
	}

	dark := theme.Dark()
	light := theme.Light()
	if !dark.Dark {
		t.Fatal("theme.Dark().Dark = false, want true")
	}
	if light.Dark {
		t.Fatal("theme.Light().Dark = true, want false")
	}
	if dark.Bg == light.Bg || dark.Panel == light.Panel || dark.Raised == light.Raised {
		t.Fatalf("dark and light surfaces must differ: dark=%+v light=%+v", dark, light)
	}
	if theme.PaletteFor(true) != dark {
		t.Fatal("theme.PaletteFor(true) != theme.Dark()")
	}
	if theme.PaletteFor(false) != light {
		t.Fatal("theme.PaletteFor(false) != theme.Light()")
	}

	// Verify exact Spec.dc.html dark token values.
	wantDark := map[string]struct {
		got  core.Color
		want core.Color
	}{
		"bg":            {dark.Bg, core.RGB(0x101216)},
		"panel":         {dark.Panel, core.RGB(0x12141A)},
		"raised":        {dark.Raised, core.RGB(0x171A21)},
		"line":          {dark.Line, core.RGB(0x1F232A)},
		"lineRaised":    {dark.LineRaised, core.RGB(0x262B35)},
		"textPrimary":   {dark.TextPrimary, core.RGB(0xFFFFFF)},
		"textSecondary": {dark.TextSecondary, core.RGB(0xD9DDE5)},
		"textMuted":     {dark.TextMuted, core.RGB(0xB4BAC6)},
		"textFaint":     {dark.TextFaint, core.RGB(0x8B92A0)},
		"accent":        {dark.Accent.Fg, core.RGB(0x3B7BFF)},
		"accentOnFill":  {dark.Accent.OnFill, core.RGB(0x101216)},
		"agent":         {dark.Agent.Fg, core.RGB(0x9B7BFF)},
		"added":         {dark.Added.Fg, core.RGB(0x4FD38A)},
		"changed":       {dark.Changed.Fg, core.RGB(0xE9A84A)},
		"removed":       {dark.Removed.Fg, core.RGB(0xFF6B6B)},
		"warning":       {dark.Warning.Fg, core.RGB(0xF2C14E)},
		"warningFill":   {dark.Warning.Fill, core.RGB(0x2A2414)},
		"warningEdge":   {dark.Warning.Edge, core.RGB(0x4A3F1F)},
	}
	for name, tc := range wantDark {
		if tc.got != tc.want {
			t.Errorf("dark %s = #%08X, want #%08X", name, uint32(tc.got), uint32(tc.want))
		}
	}

	// Verify NSGo core.Palette role mappings on theme.Core() for both dark and light.
	for _, tc := range []struct {
		name string
		p    core.Palette
		spec theme.Palette
	}{
		{"dark", th.Dark, dark},
		{"light", th.Light, light},
	} {
		if tc.p.Background != tc.spec.Bg {
			t.Errorf("%s Palette.Background = #%08X, want #%08X", tc.name, uint32(tc.p.Background), uint32(tc.spec.Bg))
		}
		if tc.p.Surface != tc.spec.Panel {
			t.Errorf("%s Palette.Surface = #%08X, want #%08X", tc.name, uint32(tc.p.Surface), uint32(tc.spec.Panel))
		}
		if raised, ok := tc.p.Color("raised"); !ok || raised != tc.spec.Raised {
			t.Errorf("%s Palette.Color(raised) = #%08X (ok=%v), want #%08X", tc.name, uint32(raised), ok, uint32(tc.spec.Raised))
		}
		if line, ok := tc.p.Color("line"); !ok || line != tc.spec.Line {
			t.Errorf("%s Palette.Color(line) = #%08X (ok=%v), want #%08X", tc.name, uint32(line), ok, uint32(tc.spec.Line))
		}
		if tc.p.Border != tc.spec.LineRaised {
			t.Errorf("%s Palette.Border = #%08X, want #%08X", tc.name, uint32(tc.p.Border), uint32(tc.spec.LineRaised))
		}
		if tc.p.Text != tc.spec.TextSecondary {
			t.Errorf("%s Palette.Text = #%08X, want #%08X", tc.name, uint32(tc.p.Text), uint32(tc.spec.TextSecondary))
		}
		if tc.p.TextMuted != tc.spec.TextFaint {
			t.Errorf("%s Palette.TextMuted = #%08X, want #%08X", tc.name, uint32(tc.p.TextMuted), uint32(tc.spec.TextFaint))
		}
		if tc.p.Accent != tc.spec.Accent.Fg {
			t.Errorf("%s Palette.Accent = #%08X, want #%08X", tc.name, uint32(tc.p.Accent), uint32(tc.spec.Accent.Fg))
		}
		if tc.p.AccentText != tc.spec.Accent.OnFill {
			t.Errorf("%s Palette.AccentText = #%08X, want #%08X", tc.name, uint32(tc.p.AccentText), uint32(tc.spec.Accent.OnFill))
		}
		if tc.p.Success != tc.spec.Added.Fg {
			t.Errorf("%s Palette.Success = #%08X, want #%08X", tc.name, uint32(tc.p.Success), uint32(tc.spec.Added.Fg))
		}
		if tc.p.Warning != tc.spec.Warning.Fg {
			t.Errorf("%s Palette.Warning = #%08X, want #%08X", tc.name, uint32(tc.p.Warning), uint32(tc.spec.Warning.Fg))
		}
		if tc.p.Danger != tc.spec.Removed.Fg {
			t.Errorf("%s Palette.Danger = #%08X, want #%08X", tc.name, uint32(tc.p.Danger), uint32(tc.spec.Removed.Fg))
		}
		if tc.p.Focus != tc.spec.Accent.Edge {
			t.Errorf("%s Palette.Focus = #%08X, want #%08X", tc.name, uint32(tc.p.Focus), uint32(tc.spec.Accent.Edge))
		}
		if ring, ok := tc.p.Color("ring"); !ok || ring != tc.spec.Accent.Edge {
			t.Errorf("%s Palette.Color(ring) = #%08X (ok=%v), want #%08X (declared in theme.yaml)", tc.name, uint32(ring), ok, uint32(tc.spec.Accent.Edge))
		}
		if tc.spec.AccentHover == 0 || tc.spec.AccentHover != tc.p.AccentHover {
			t.Errorf("%s Palette.AccentHover = #%08X, want #%08X", tc.name, uint32(tc.spec.AccentHover), uint32(tc.p.AccentHover))
		}
		if tc.spec.AccentPressed == 0 || tc.spec.AccentPressed != tc.p.AccentPressed {
			t.Errorf("%s Palette.AccentPressed = #%08X, want #%08X", tc.name, uint32(tc.spec.AccentPressed), uint32(tc.p.AccentPressed))
		}
	}
}

func TestContrastTableDarkAndLight(t *testing.T) {
	// Verify every row of the Spec WCAG 2.2 contrast table passes for BOTH dark and light palettes.
	for _, tc := range []struct {
		name    string
		palette theme.Palette
	}{
		{"dark", theme.Dark()},
		{"light", theme.Light()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := theme.ContrastTable(tc.palette)
			if len(rows) == 0 {
				t.Fatal("theme.ContrastTable returned empty table")
			}
			for _, row := range rows {
				ratio := row.Ratio()
				if ratio < row.MinRatio {
					t.Errorf("%s: %s contrast = %.2f:1 (fg=#%06X on bg=#%06X), want >= %.1f:1 (%s)",
						tc.name, row.Name, ratio, uint32(row.Foreground)>>8, uint32(row.Background)>>8, row.MinRatio, row.Usage)
				}
			}
		})
	}

	// Verify exact WCAG 2.2 dark ratios published in design/editor/Spec.dc.html.
	dark := theme.Dark()
	wantPublished := []struct {
		name  string
		fg    core.Color
		bg    core.Color
		ratio float64
	}{
		{"text strong on panel", dark.TextPrimary, dark.Panel, 18.41},
		{"text on panel", dark.TextSecondary, dark.Panel, 13.52},
		{"text on raised", dark.TextSecondary, dark.Raised, 12.79},
		{"secondary on panel", dark.TextMuted, dark.Panel, 9.45},
		{"secondary on raised", dark.TextMuted, dark.Raised, 8.94},
		{"muted on bg", dark.TextFaint, dark.Bg, 6.00},
		{"muted on panel", dark.TextFaint, dark.Panel, 5.89},
		{"muted on raised", dark.TextFaint, dark.Raised, 5.57},
		{"#101216 on accent", dark.Accent.OnFill, dark.Accent.Fg, 4.88},
		{"#101216 on text strong", dark.OnTextPrimary, dark.TextPrimary, 18.75},
		{"warning on warning fill", dark.Warning.Fg, dark.Warning.Fill, 9.19},
		{"text on warning fill", dark.TextSecondary, dark.Warning.Fill, 11.33},
		{"agent on panel", dark.Agent.Fg, dark.Panel, 5.85},
		{"added on panel", dark.Added.Fg, dark.Panel, 9.66},
		{"changed on panel", dark.Changed.Fg, dark.Panel, 8.91},
		{"removed on panel", dark.Removed.Fg, dark.Panel, 6.63},
		{"accent edge on panel", dark.Accent.Edge, dark.Panel, 4.79},
		{"accent edge on raised", dark.Accent.Edge, dark.Raised, 4.53},
		{"accent edge on viewport", dark.Accent.Edge, dark.Viewport, 4.63},
	}
	for _, tc := range wantPublished {
		got := theme.ContrastRatio(tc.fg, tc.bg)
		if math.Abs(got-tc.ratio) > 0.02 {
			t.Errorf("Spec dark published ratio %q = %.2f, want %.2f", tc.name, got, tc.ratio)
		}
	}

	// Verify hover/pressed accent fills in theme.Core() also satisfy >= 4.5:1 with AccentText.
	th := theme.Core()
	for _, tc := range []struct {
		name string
		p    core.Palette
	}{
		{"dark", th.Dark},
		{"light", th.Light},
	} {
		if r := theme.ContrastRatio(tc.p.AccentText, tc.p.AccentHover); r < 4.5 {
			t.Errorf("%s AccentText on AccentHover = %.2f:1, want >= 4.5:1", tc.name, r)
		}
		if r := theme.ContrastRatio(tc.p.AccentText, tc.p.AccentPressed); r < 4.5 {
			t.Errorf("%s AccentText on AccentPressed = %.2f:1, want >= 4.5:1", tc.name, r)
		}
	}
}

func TestNoWhiteOnFailingAccentOrAgent(t *testing.T) {
	white := core.RGB(0xFFFFFF)

	// Confirm the Spec dark guardrail: white fails AA on dark accent (3.84:1) and dark agent (3.15:1), so theme.Dark must NEVER use white as OnFill on accent or agent.
	dark := theme.Dark()
	if r := theme.ContrastRatio(white, dark.Accent.Fg); r >= 4.5 {
		t.Fatalf("expected white on dark accent to fail 4.5:1, got %.2f", r)
	}
	if dark.Accent.OnFill == white {
		t.Fatal("dark Accent.OnFill must never be white")
	}
	if r := theme.ContrastRatio(dark.Accent.OnFill, dark.Accent.Fg); r < 4.5 {
		t.Fatalf("dark Accent.OnFill contrast = %.2f, want >= 4.5", r)
	}

	if r := theme.ContrastRatio(white, dark.Agent.Fg); r >= 4.5 {
		t.Fatalf("expected white on dark agent to fail 4.5:1, got %.2f", r)
	}
	if dark.Agent.OnFill == white {
		t.Fatal("dark Agent.OnFill must never be white")
	}
	if r := theme.ContrastRatio(dark.Agent.OnFill, dark.Agent.Fg); r < 4.5 {
		t.Fatalf("dark Agent.OnFill contrast = %.2f, want >= 4.5", r)
	}

	// In light mode, OnFill on Accent and Agent must pass >= 4.5:1 as well.
	light := theme.Light()
	if r := theme.ContrastRatio(light.Accent.OnFill, light.Accent.Fg); r < 4.5 {
		t.Fatalf("light Accent.OnFill contrast = %.2f, want >= 4.5", r)
	}
	if r := theme.ContrastRatio(light.Agent.OnFill, light.Agent.Fg); r < 4.5 {
		t.Fatalf("light Agent.OnFill contrast = %.2f, want >= 4.5", r)
	}
}

func TestDesignTokenConstantsAndReducedMotion(t *testing.T) {
	if theme.FontFamilySans != "Inter" || theme.FontFamilyMono != "JetBrains Mono" {
		t.Fatalf("font families = (%q, %q), want (Inter, JetBrains Mono)", theme.FontFamilySans, theme.FontFamilyMono)
	}
	if theme.FontTitleSize != 14 || theme.FontTitleWeight != core.FontWeightSemibold {
		t.Fatalf("title type = %v/%v, want 14/600", theme.FontTitleSize, theme.FontTitleWeight)
	}
	if theme.FontBodySize != 12.5 || theme.FontBodyWeight != core.FontWeightNormal {
		t.Fatalf("body type = %v/%v, want 12.5/400", theme.FontBodySize, theme.FontBodyWeight)
	}
	if theme.FontSectionSize != 11 || theme.FontSectionWeight != core.FontWeightSemibold || math.Abs(float64(theme.FontSectionTrack)-0.66) > 1e-6 {
		t.Fatalf("section type = %v/%v/+%v, want 11/600/+0.66 (+6%%)", theme.FontSectionSize, theme.FontSectionWeight, theme.FontSectionTrack)
	}
	if theme.FontCodeSize != 11.5 || theme.FontCodeWeight != core.FontWeightNormal {
		t.Fatalf("code type = %v/%v, want 11.5/400", theme.FontCodeSize, theme.FontCodeWeight)
	}
	if theme.Space4 != 4 || theme.Space8 != 8 || theme.Space12 != 12 || theme.Space16 != 16 || theme.Space24 != 24 {
		t.Fatalf("spacing scale = %v,%v,%v,%v,%v, want 4,8,12,16,24",
			theme.Space4, theme.Space8, theme.Space12, theme.Space16, theme.Space24)
	}
	if theme.RadiusControl != 6 || theme.RadiusButton != 8 || theme.RadiusCard != 12 || theme.RadiusDialog != 18 {
		t.Fatalf("radii = %v,%v,%v,%v, want 6,8,12,18",
			theme.RadiusControl, theme.RadiusButton, theme.RadiusCard, theme.RadiusDialog)
	}
	if theme.RowHeight != 28 || theme.MinHitSize != 28 || theme.ButtonHeight != 30 || theme.ButtonTallHeight != 32 || theme.HeaderHeight != 44 {
		t.Fatalf("heights = row:%v hit:%v btn:%v btnTall:%v header:%v",
			theme.RowHeight, theme.MinHitSize, theme.ButtonHeight, theme.ButtonTallHeight, theme.HeaderHeight)
	}
	if theme.BorderWidth != 1 || theme.IconSize != 15 || math.Abs(float64(theme.IconStrokeWidth)-1.8) > 1e-6 {
		t.Fatalf("border/icon = border:%v icon:%v stroke:%v", theme.BorderWidth, theme.IconSize, theme.IconStrokeWidth)
	}
	if theme.MotionHover != 120*time.Millisecond ||
		theme.MotionPanel != 180*time.Millisecond ||
		theme.MotionOverlay != 240*time.Millisecond ||
		theme.MotionAgentPulse != 1600*time.Millisecond {
		t.Fatalf("motion = hover:%v panel:%v overlay:%v pulse:%v",
			theme.MotionHover, theme.MotionPanel, theme.MotionOverlay, theme.MotionAgentPulse)
	}

	// Verify 3D axis tokens match design/editor/Viewport3D.dc.html.
	dark := theme.Dark()
	if dark.AxisX != core.RGB(0xFF6B6B) || dark.AxisY != core.RGB(0x4FD38A) || dark.AxisZ != core.RGB(0x3B7BFF) {
		t.Fatalf("dark 3D axes = (#%06X, #%06X, #%06X), want (#FF6B6B, #4FD38A, #3B7BFF)",
			uint32(dark.AxisX)>>8, uint32(dark.AxisY)>>8, uint32(dark.AxisZ)>>8)
	}

	// Verify motion disables cleanly under Env.ReduceMotion.
	var normalHover, reducedHover time.Duration
	var normalMidPulse, reducedMidPulse float32
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.Effect(func() {
			if cx.Env().ReduceMotion() {
				reducedHover = theme.MotionDuration(cx, theme.MotionHover)
				reducedMidPulse = theme.AgentPulseOpacity(cx, theme.MotionAgentPulse/2)
			} else {
				normalHover = theme.MotionDuration(cx, theme.MotionHover)
				normalMidPulse = theme.AgentPulseOpacity(cx, theme.MotionAgentPulse/2)
			}
		})
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, ReduceMotion: false}))

	if normalHover != 120*time.Millisecond {
		t.Fatalf("normal theme.MotionDuration(Hover) = %v, want 120ms", normalHover)
	}
	if math.Abs(float64(normalMidPulse-0.45)) > 0.01 {
		t.Fatalf("normal theme.AgentPulseOpacity(800ms) = %v, want 0.45", normalMidPulse)
	}

	app.SetEnvironment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, ReduceMotion: true})
	if reducedHover != 0 {
		t.Fatalf("ReduceMotion theme.MotionDuration(Hover) = %v, want 0", reducedHover)
	}
	if reducedMidPulse != 1.0 {
		t.Fatalf("ReduceMotion theme.AgentPulseOpacity(800ms) = %v, want 1.0", reducedMidPulse)
	}
}

// #1002 Acceptance 1: Setting dark text_muted back to the retired #6F7684 causes the theme-file WCAG 2.2 contrast table check to fail below 4.5:1.
func TestContrastTableRejectsRetiredMutedColor(t *testing.T) {
	raw := string(theme.YAML())
	mutated := strings.Replace(raw, `specTextFaint: "#8B92A0"`, `specTextFaint: "#6F7684"`, 1)
	if mutated == raw {
		t.Fatal("failed to locate dark specTextFaint in theme.yaml")
	}
	th, err := core.ParseTheme([]byte(mutated))
	if err != nil {
		t.Fatalf("core.ParseTheme(mutated): %v", err)
	}
	retired, ok := th.Dark.Color("specTextFaint")
	if !ok || retired != core.RGB(0x6F7684) {
		t.Fatalf("mutated specTextFaint = #%06X (ok=%v), want #6F7684", uint32(retired)>>8, ok)
	}

	pal := theme.Dark()
	pal.TextFaint = retired

	failed := 0
	for _, row := range theme.ContrastTable(pal) {
		if row.Ratio() < row.MinRatio {
			failed++
		}
	}
	if failed == 0 {
		t.Fatal("expected theme.ContrastTable to fail when dark text_muted is reverted to #6F7684")
	}
}
