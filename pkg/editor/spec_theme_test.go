package editor_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/nsgo-dev/nsgo/ui"
)

func TestSpecThemeParsesAndMapsAllRoles(t *testing.T) {
	th, err := core.ParseTheme(editor.SpecThemeYAML())
	if err != nil {
		t.Fatalf("core.ParseTheme(SpecThemeYAML): %v", err)
	}
	if th.Radius != 8 {
		t.Fatalf("SpecTheme.Radius = %v, want 8", th.Radius)
	}

	dark := editor.DarkSpecPalette()
	light := editor.LightSpecPalette()
	if !dark.Dark {
		t.Fatal("DarkSpecPalette().Dark = false, want true")
	}
	if light.Dark {
		t.Fatal("LightSpecPalette().Dark = true, want false")
	}
	if dark.Bg == light.Bg || dark.Panel == light.Panel || dark.Raised == light.Raised {
		t.Fatalf("dark and light surfaces must differ: dark=%+v light=%+v", dark, light)
	}
	if editor.SpecPaletteFor(true) != dark {
		t.Fatal("SpecPaletteFor(true) != DarkSpecPalette()")
	}
	if editor.SpecPaletteFor(false) != light {
		t.Fatal("SpecPaletteFor(false) != LightSpecPalette()")
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

	// Verify NSGo core.Palette role mappings on SpecTheme for both dark and light.
	for _, tc := range []struct {
		name string
		p    core.Palette
		spec editor.SpecPalette
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
			t.Errorf("%s Palette.Color(ring) = #%08X (ok=%v), want #%08X (declared in spec_theme.yaml)", tc.name, uint32(ring), ok, uint32(tc.spec.Accent.Edge))
		}
		if tc.spec.AccentHover == 0 || tc.spec.AccentHover != tc.p.AccentHover {
			t.Errorf("%s SpecPalette.AccentHover = #%08X, want #%08X", tc.name, uint32(tc.spec.AccentHover), uint32(tc.p.AccentHover))
		}
		if tc.spec.AccentPressed == 0 || tc.spec.AccentPressed != tc.p.AccentPressed {
			t.Errorf("%s SpecPalette.AccentPressed = #%08X, want #%08X", tc.name, uint32(tc.spec.AccentPressed), uint32(tc.p.AccentPressed))
		}
	}
}

func TestSpecContrastTableDarkAndLight(t *testing.T) {
	// Verify every row of the Spec WCAG 2.2 contrast table passes for BOTH dark and light palettes.
	for _, theme := range []struct {
		name    string
		palette editor.SpecPalette
	}{
		{"dark", editor.DarkSpecPalette()},
		{"light", editor.LightSpecPalette()},
	} {
		t.Run(theme.name, func(t *testing.T) {
			rows := editor.SpecContrastTable(theme.palette)
			if len(rows) == 0 {
				t.Fatal("SpecContrastTable returned empty table")
			}
			for _, row := range rows {
				ratio := row.Ratio()
				if ratio < row.MinRatio {
					t.Errorf("%s: %s contrast = %.2f:1 (fg=#%06X on bg=#%06X), want >= %.1f:1 (%s)",
						theme.name, row.Name, ratio, uint32(row.Foreground)>>8, uint32(row.Background)>>8, row.MinRatio, row.Usage)
				}
			}
		})
	}

	// Verify exact WCAG 2.2 dark ratios published in docs/design/editor/Spec.dc.html.
	dark := editor.DarkSpecPalette()
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
		got := editor.ContrastRatio(tc.fg, tc.bg)
		if math.Abs(got-tc.ratio) > 0.02 {
			t.Errorf("Spec dark published ratio %q = %.2f, want %.2f", tc.name, got, tc.ratio)
		}
	}

	// Verify hover/pressed accent fills in SpecTheme also satisfy >= 4.5:1 with AccentText.
	th := editor.SpecTheme()
	for _, tc := range []struct {
		name string
		p    core.Palette
	}{
		{"dark", th.Dark},
		{"light", th.Light},
	} {
		if r := editor.ContrastRatio(tc.p.AccentText, tc.p.AccentHover); r < 4.5 {
			t.Errorf("%s AccentText on AccentHover = %.2f:1, want >= 4.5:1", tc.name, r)
		}
		if r := editor.ContrastRatio(tc.p.AccentText, tc.p.AccentPressed); r < 4.5 {
			t.Errorf("%s AccentText on AccentPressed = %.2f:1, want >= 4.5:1", tc.name, r)
		}
	}
}

func TestSpecNoWhiteOnFailingAccentOrAgent(t *testing.T) {
	white := core.RGB(0xFFFFFF)

	// Confirm the Spec dark guardrail: white fails AA on dark accent (3.84:1) and dark agent (3.15:1),
	// so DarkSpecPalette must NEVER use white as OnFill on accent or agent.
	dark := editor.DarkSpecPalette()
	if r := editor.ContrastRatio(white, dark.Accent.Fg); r >= 4.5 {
		t.Fatalf("expected white on dark accent to fail 4.5:1, got %.2f", r)
	}
	if dark.Accent.OnFill == white {
		t.Fatal("dark Accent.OnFill must never be white")
	}
	if r := editor.ContrastRatio(dark.Accent.OnFill, dark.Accent.Fg); r < 4.5 {
		t.Fatalf("dark Accent.OnFill contrast = %.2f, want >= 4.5", r)
	}

	if r := editor.ContrastRatio(white, dark.Agent.Fg); r >= 4.5 {
		t.Fatalf("expected white on dark agent to fail 4.5:1, got %.2f", r)
	}
	if dark.Agent.OnFill == white {
		t.Fatal("dark Agent.OnFill must never be white")
	}
	if r := editor.ContrastRatio(dark.Agent.OnFill, dark.Agent.Fg); r < 4.5 {
		t.Fatalf("dark Agent.OnFill contrast = %.2f, want >= 4.5", r)
	}

	// In light mode, OnFill on Accent and Agent must pass >= 4.5:1 as well.
	light := editor.LightSpecPalette()
	if r := editor.ContrastRatio(light.Accent.OnFill, light.Accent.Fg); r < 4.5 {
		t.Fatalf("light Accent.OnFill contrast = %.2f, want >= 4.5", r)
	}
	if r := editor.ContrastRatio(light.Agent.OnFill, light.Agent.Fg); r < 4.5 {
		t.Fatalf("light Agent.OnFill contrast = %.2f, want >= 4.5", r)
	}
}

// #1062 Acceptance 2: With Appearance set to system, changing the system appearance live
// repaints the editor between the Spec dark and light palettes without a restart.
func TestEditorSystemAppearanceLiveSwitchWithoutRestart(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetOverlayLegendOpen(true)
	ed.ShowToast(editor.Toast{
		ID:             "kept",
		Kind:           editor.ToastSuccess,
		Message:        "Kept 6 changes to level_01",
		ActionLabel:    "Undo",
		ActionShortcut: "⌘Z",
	})
	if ed.Appearance() != editor.AppearanceSystem {
		t.Fatalf("default Appearance = %q, want system", ed.Appearance())
	}

	var activeSpec editor.SpecPalette
	var uiBg, uiSurface, uiRaised, uiAccent, uiOnAccent core.Color

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		node := editor.EditorView(cx, ed)
		colors := ui.ColorsOf(cx)
		cx.Effect(func() {
			activeSpec = ed.SpecPalette(cx)
			uiBg = colors.Background()
			uiSurface = colors.Surface()
			uiRaised = colors.Raised()
			uiAccent = colors.Accent()
			uiOnAccent = colors.OnAccent()
		})
		return node
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	assertPalette := func(step string, want editor.SpecPalette) {
		t.Helper()
		if activeSpec != want {
			t.Fatalf("%s: activeSpec.Dark=%v Bg=#%06X, want Dark=%v Bg=#%06X",
				step, activeSpec.Dark, uint32(activeSpec.Bg)>>8, want.Dark, uint32(want.Bg)>>8)
		}
		if ed.Dark() != want.Dark {
			t.Fatalf("%s: ed.Dark() = %v, want %v", step, ed.Dark(), want.Dark)
		}
		if uiBg != want.Bg || uiSurface != want.Panel || uiRaised != want.Raised || uiAccent != want.Accent.Fg || uiOnAccent != want.Accent.OnFill {
			t.Fatalf("%s: ui.ColorsOf mismatch: bg=#%06X surface=#%06X raised=#%06X accent=#%06X onAccent=#%06X",
				step, uint32(uiBg)>>8, uint32(uiSurface)>>8, uint32(uiRaised)>>8, uint32(uiAccent)>>8, uint32(uiOnAccent)>>8)
		}
		if got := app.Find("id:scene-editor").Style.Background; got != want.Bg {
			t.Fatalf("%s: scene-editor background = #%06X, want #%06X", step, uint32(got)>>8, uint32(want.Bg)>>8)
		}
		if got := app.Find("id:editor-viewport-container").Style.Background; got != want.Viewport {
			t.Fatalf("%s: viewport background = #%06X, want #%06X", step, uint32(got)>>8, uint32(want.Viewport)>>8)
		}
		if got := app.Find("id:editor-toast").Style.Background; got != want.Raised {
			t.Fatalf("%s: toast background = #%06X, want #%06X", step, uint32(got)>>8, uint32(want.Raised)>>8)
		}
		if got := app.Find("id:viewport-overlay-legend").Style.Background; got != want.Raised {
			t.Fatalf("%s: overlay legend background = #%06X, want #%06X", step, uint32(got)>>8, uint32(want.Raised)>>8)
		}
	}

	// Starts in Spec Dark on a dark system.
	assertPalette("initial dark system", editor.DarkSpecPalette())

	// Switch the system appearance live to Light -> repaints immediately to Spec Light without restart.
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	assertPalette("live switch to light system", editor.LightSpecPalette())

	// Switch back to Dark live -> repaints immediately to Spec Dark without restart.
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	assertPalette("live switch back to dark system", editor.DarkSpecPalette())

	// Explicit AppearanceLight override stays on Spec Light even while system is Dark.
	app.Post(func() {
		if err := ed.SetAppearance(editor.AppearanceLight); err != nil {
			t.Errorf("SetAppearance(light): %v", err)
		}
	})
	app.Settle()
	assertPalette("explicit light on dark system", editor.LightSpecPalette())
}

func TestEditorToastQueueAndSpecStyling(t *testing.T) {
	ed := newTestEditor(t)
	actionFired := false
	ed.ShowToast(editor.Toast{
		ID:             "kept",
		Kind:           editor.ToastSuccess,
		Message:        "Kept 6 changes to level_01",
		ActionLabel:    "Undo",
		ActionShortcut: "⌘Z",
		OnAction:       func() { actionFired = true },
	})
	ed.ShowToast(editor.Toast{
		ID:          "disk",
		Kind:        editor.ToastWarning,
		Message:     "level_01.scene.yaml changed on disk and was reloaded",
		ActionLabel: "Show",
	})
	// Duplicate ID merges rather than adding a third entry.
	ed.ShowToast(editor.Toast{
		ID:          "disk",
		Kind:        editor.ToastWarning,
		Message:     "level_01.scene.yaml changed on disk and was reloaded (2)",
		ActionLabel: "Show",
	})
	if ed.QueuedToastCount() != 1 {
		t.Fatalf("QueuedToastCount = %d, want 1", ed.QueuedToastCount())
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	light := editor.LightSpecPalette()
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "Kept 6 changes") {
		t.Fatalf("active toast message = %q", got)
	}
	if got := allText(app.Find("id:editor-toast-waiting")); !strings.Contains(got, "+1") {
		t.Fatalf("waiting badge = %q, want +1", got)
	}
	if got := app.Find("id:editor-toast").Style.Background; got != light.Raised {
		t.Fatalf("light success toast bg = #%06X, want #%06X", uint32(got)>>8, uint32(light.Raised)>>8)
	}

	app.Press("id:editor-toast-action")
	if !actionFired {
		t.Fatal("toast action callback did not run")
	}

	// Dismiss advances to the warning toast, which uses Warning.Fill and Warning.Edge in Light mode.
	app.Press("id:editor-toast-dismiss")
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "changed on disk") {
		t.Fatalf("second toast message = %q", got)
	}
	toastStyle := app.Find("id:editor-toast").Style
	if toastStyle.Background != light.Warning.Fill || toastStyle.BorderColor != light.Warning.Edge {
		t.Fatalf("light warning toast bg=#%06X border=#%06X, want bg=#%06X border=#%06X",
			uint32(toastStyle.Background)>>8, uint32(toastStyle.BorderColor)>>8,
			uint32(light.Warning.Fill)>>8, uint32(light.Warning.Edge)>>8)
	}

	// Dismissing the last toast hides the toast host.
	app.Press("id:editor-toast-dismiss")
	if app.Exists("id:editor-toast") {
		t.Fatal("toast should be gone after dismissing last item")
	}
}

func TestToastViewAvoids2DOverlayBarAndMemoizesRevisions(t *testing.T) {
	ed := newTestEditor(t)
	ed.ShowToast(editor.Toast{
		ID:      "saved",
		Kind:    editor.ToastSuccess,
		Message: "Saved scene",
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	// In 2D edit mode, toast sits above the bottom-center viewport overlay bar without overlapping it.
	toast2D := app.Find("id:editor-toast")
	toastRect := toast2D.Bounds()
	barRect := app.Find("id:viewport-overlay-bar").Bounds()
	if toastRect.Origin.Y+toastRect.Size.Height > barRect.Origin.Y {
		t.Fatalf("toast rect %+v overlaps viewport overlay bar %+v", toastRect, barRect)
	}

	// Unrelated editor state changes bump rev without remounting the toast node.
	app.Post(func() {
		ed.SetSnapToGrid(!ed.SnapToGrid)
		ed.SetSnapToGrid(!ed.SnapToGrid)
	})
	app.Settle()
	toastAfter := app.Find("id:editor-toast")
	if toastAfter.ID != toast2D.ID {
		t.Fatalf("unrelated rev bump remounted toast node: id %v -> %v", toast2D.ID, toastAfter.ID)
	}
	if got := app.Find("id:editor-toast-message").Text; got != "Saved scene" {
		t.Fatalf("toast message after unrelated rev bumps = %q, want %q", got, "Saved scene")
	}

	// In 3D edit mode the bottom-center 3D navigation bar sits at Bottom: 16 inside the viewport container
	// (which sits above the 3D bottom MODELS strip), so the toast keeps 60pt bottom clearance above the bar and strip.
	vp2DRect := app.Find("id:editor-viewport-container").Bounds()
	bottomClearance2D := (vp2DRect.Origin.Y + vp2DRect.Size.Height) - (toastRect.Origin.Y + toastRect.Size.Height)
	app.Post(func() {
		ed.SetViewMode(editor.ViewMode3D)
	})
	app.Settle()
	toast3DRect := app.Find("id:editor-toast").Bounds()
	bar3DRect := app.Find("id:view3d-bottom-bar").Bounds()
	if toast3DRect.Origin.Y+toast3DRect.Size.Height > bar3DRect.Origin.Y {
		t.Fatalf("3D mode toast rect %+v overlaps 3D bottom bar %+v", toast3DRect, bar3DRect)
	}
	vp3DRect := app.Find("id:editor-viewport-container").Bounds()
	bottomClearance3D := (vp3DRect.Origin.Y + vp3DRect.Size.Height) - (toast3DRect.Origin.Y + toast3DRect.Size.Height)
	if math.Abs(float64(bottomClearance3D-bottomClearance2D)) > 1 {
		t.Fatalf("3D edit mode toast clearance = %v, want %v (same 60pt clearance as 2D)", bottomClearance3D, bottomClearance2D)
	}
	stripRect := app.Find("id:viewport3d-models-strip").Bounds()
	if toast3DRect.Origin.Y+toast3DRect.Size.Height > stripRect.Origin.Y {
		t.Fatalf("3D mode toast rect %+v overlaps bottom MODELS strip %+v", toast3DRect, stripRect)
	}

	// Switching back to 2D keeps the 60pt clearance.
	app.Post(func() {
		ed.SetViewMode(editor.ViewMode2D)
	})
	app.Settle()
	toastBackRect := app.Find("id:editor-toast").Bounds()
	if math.Abs(float64(toastBackRect.Origin.Y-toastRect.Origin.Y)) > 1 {
		t.Fatalf("restored 2D mode toast Y = %v, want %v", toastBackRect.Origin.Y, toastRect.Origin.Y)
	}

	// During Play mode the bottom bars are hidden/inactive, so toast drops from Bottom: 60 to Bottom: 16.
	bottomGap := func() core.Points {
		host := app.Find("id:editor-toast-host")
		hb := host.Bounds()
		pb := host.Parent.Bounds()
		return (pb.Origin.Y + pb.Size.Height) - (hb.Origin.Y + hb.Size.Height)
	}
	editGap := bottomGap()
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	playGap := bottomGap()
	if math.Abs(float64(editGap-60)) > 1 || math.Abs(float64(playGap-16)) > 1 {
		t.Fatalf("toast bottom clearance: edit=%v play=%v, want 60pt and 16pt", editGap, playGap)
	}
	app.Post(func() {
		_ = ed.Stop()
	})
	app.Settle()
}

func TestSpecToastCardClampsNonFiniteProgress(t *testing.T) {
	cases := []struct {
		name     string
		progress float32
	}{
		{"nan", float32(math.NaN())},
		{"posInf", float32(math.Inf(1))},
		{"negInf", float32(math.Inf(-1))},
		{"negative", -0.5},
		{"overOne", 1.5},
		{"half", 0.5},
	}
	pal := editor.LightSpecPalette()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return editor.SpecToastCard(pal, editor.Toast{
					ID:       "prog-" + tc.name,
					Kind:     editor.ToastProgress,
					Message:  "Building...",
					Progress: tc.progress,
				}, 0, nil)
			}, nsgotest.Size(480, 120))
			toast := app.Find("id:editor-toast")
			w := float64(toast.Bounds().Size.Width)
			if math.IsNaN(w) || math.IsInf(w, 0) || w <= 0 {
				t.Fatalf("toast bounds width invalid: %+v", toast.Bounds())
			}
		})
	}
}

func TestSpecLightChromeOverlaysAndMarqueeContrast(t *testing.T) {
	ed := newTestEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	light := editor.LightSpecPalette()
	dark := editor.DarkSpecPalette()

	assertChrome := func(label string, want editor.SpecPalette) {
		t.Helper()
		bar := app.Find("id:viewport-overlay-bar").Style
		wantBarBg := ui.Alpha(want.Raised, 0.96)
		if bar.Background != wantBarBg || bar.BorderColor != want.LineRaised {
			t.Fatalf("%s: viewport-overlay-bar bg=%#x border=%#x, want bg=%#x border=%#x",
				label, uint32(bar.Background), uint32(bar.BorderColor), uint32(wantBarBg), uint32(want.LineRaised))
		}
		rail := app.Find("id:left-rail-switch").Style
		if rail.Background != want.Bg || rail.BorderColors.Bottom != want.Line {
			t.Fatalf("%s: left-rail-switch bg=%#x bottomBorder=%#x, want bg=%#x bottomBorder=%#x",
				label, uint32(rail.Background), uint32(rail.BorderColors.Bottom), uint32(want.Bg), uint32(want.Line))
		}
		if got := app.Find("id:rail-mode-scene").Style.Background; got != want.LineRaised {
			t.Fatalf("%s: active rail-mode-scene bg=%#x, want %#x", label, uint32(got), uint32(want.LineRaised))
		}
	}

	assertChrome("light", light)

	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	assertChrome("dark", dark)

	// Built-in viewport overlays, collision stroke, and marquee edge must all meet WCAG 2.2
	// non-text 3:1 contrast against Viewport in both Light and Dark appearances.
	for _, tc := range []struct {
		name       string
		appearance editor.Appearance
		pal        editor.SpecPalette
	}{
		{"light", editor.AppearanceLight, light},
		{"dark", editor.AppearanceDark, dark},
	} {
		if err := ed.SetAppearance(tc.appearance); err != nil {
			t.Fatalf("SetAppearance(%s): %v", tc.name, err)
		}
		ovs := ed.ViewportOverlays()
		if len(ovs) == 0 {
			t.Fatalf("%s: expected built-in overlays", tc.name)
		}
		for _, ov := range ovs {
			ratio := editor.ContrastRatio(ov.Color, tc.pal.Viewport)
			if ratio < 3.0 {
				t.Errorf("%s: overlay %q color #%06X on Viewport #%06X has contrast %.2f:1, want >= 3.0:1",
					tc.name, ov.ID, uint32(ov.Color)>>8, uint32(tc.pal.Viewport)>>8, ratio)
			}
		}
		collStroke := editor.CompositeOver(ui.Alpha(tc.pal.TextMuted, 0.85), tc.pal.Viewport)
		if ratio := editor.ContrastRatio(collStroke, tc.pal.Viewport); ratio < 3.0 {
			t.Errorf("%s: collision stroke contrast %.2f:1, want >= 3.0:1", tc.name, ratio)
		}
		if ratio := editor.ContrastRatio(tc.pal.Accent.Edge, tc.pal.Viewport); ratio < 3.0 {
			t.Errorf("%s: marquee/selection Accent.Edge contrast %.2f:1, want >= 3.0:1", tc.name, ratio)
		}
	}
}

func TestSpecDesignTokenConstantsAndReducedMotion(t *testing.T) {
	if editor.SpecFontFamilySans != "Inter" || editor.SpecFontFamilyMono != "JetBrains Mono" {
		t.Fatalf("font families = (%q, %q), want (Inter, JetBrains Mono)", editor.SpecFontFamilySans, editor.SpecFontFamilyMono)
	}
	if editor.SpecFontTitleSize != 14 || editor.SpecFontTitleWeight != core.FontWeightSemibold {
		t.Fatalf("title type = %v/%v, want 14/600", editor.SpecFontTitleSize, editor.SpecFontTitleWeight)
	}
	if editor.SpecFontBodySize != 12.5 || editor.SpecFontBodyWeight != core.FontWeightNormal {
		t.Fatalf("body type = %v/%v, want 12.5/400", editor.SpecFontBodySize, editor.SpecFontBodyWeight)
	}
	if editor.SpecFontSectionSize != 11 || editor.SpecFontSectionWeight != core.FontWeightSemibold || math.Abs(float64(editor.SpecFontSectionTrack)-0.66) > 1e-6 {
		t.Fatalf("section type = %v/%v/+%v, want 11/600/+0.66 (+6%%)", editor.SpecFontSectionSize, editor.SpecFontSectionWeight, editor.SpecFontSectionTrack)
	}
	if editor.SpecFontCodeSize != 11.5 || editor.SpecFontCodeWeight != core.FontWeightNormal {
		t.Fatalf("code type = %v/%v, want 11.5/400", editor.SpecFontCodeSize, editor.SpecFontCodeWeight)
	}
	if editor.SpecSpace4 != 4 || editor.SpecSpace8 != 8 || editor.SpecSpace12 != 12 || editor.SpecSpace16 != 16 || editor.SpecSpace24 != 24 {
		t.Fatalf("spacing scale = %v,%v,%v,%v,%v, want 4,8,12,16,24",
			editor.SpecSpace4, editor.SpecSpace8, editor.SpecSpace12, editor.SpecSpace16, editor.SpecSpace24)
	}
	if editor.SpecRadiusControl != 6 || editor.SpecRadiusButton != 8 || editor.SpecRadiusCard != 12 || editor.SpecRadiusDialog != 18 {
		t.Fatalf("radii = %v,%v,%v,%v, want 6,8,12,18",
			editor.SpecRadiusControl, editor.SpecRadiusButton, editor.SpecRadiusCard, editor.SpecRadiusDialog)
	}
	if editor.SpecRowHeight != 28 || editor.SpecMinHitSize != 28 || editor.SpecButtonHeight != 30 || editor.SpecButtonTallHeight != 32 || editor.SpecHeaderHeight != 44 {
		t.Fatalf("heights = row:%v hit:%v btn:%v btnTall:%v header:%v",
			editor.SpecRowHeight, editor.SpecMinHitSize, editor.SpecButtonHeight, editor.SpecButtonTallHeight, editor.SpecHeaderHeight)
	}
	if editor.SpecBorderWidth != 1 || editor.SpecIconSize != 15 || math.Abs(float64(editor.SpecIconStrokeWidth)-1.8) > 1e-6 {
		t.Fatalf("border/icon = border:%v icon:%v stroke:%v", editor.SpecBorderWidth, editor.SpecIconSize, editor.SpecIconStrokeWidth)
	}
	if editor.SpecMotionHover != 120*time.Millisecond ||
		editor.SpecMotionPanel != 180*time.Millisecond ||
		editor.SpecMotionOverlay != 240*time.Millisecond ||
		editor.SpecMotionAgentPulse != 1600*time.Millisecond {
		t.Fatalf("motion = hover:%v panel:%v overlay:%v pulse:%v",
			editor.SpecMotionHover, editor.SpecMotionPanel, editor.SpecMotionOverlay, editor.SpecMotionAgentPulse)
	}

	// Verify 3D axis tokens match docs/design/editor/Viewport3D.dc.html.
	dark := editor.DarkSpecPalette()
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
				reducedHover = editor.SpecMotionDuration(cx, editor.SpecMotionHover)
				reducedMidPulse = editor.SpecAgentPulseOpacity(cx, editor.SpecMotionAgentPulse/2)
			} else {
				normalHover = editor.SpecMotionDuration(cx, editor.SpecMotionHover)
				normalMidPulse = editor.SpecAgentPulseOpacity(cx, editor.SpecMotionAgentPulse/2)
			}
		})
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, ReduceMotion: false}))

	if normalHover != 120*time.Millisecond {
		t.Fatalf("normal SpecMotionDuration(Hover) = %v, want 120ms", normalHover)
	}
	if math.Abs(float64(normalMidPulse-0.45)) > 0.01 {
		t.Fatalf("normal SpecAgentPulseOpacity(800ms) = %v, want 0.45", normalMidPulse)
	}

	app.SetEnvironment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, ReduceMotion: true})
	if reducedHover != 0 {
		t.Fatalf("ReduceMotion SpecMotionDuration(Hover) = %v, want 0", reducedHover)
	}
	if reducedMidPulse != 1.0 {
		t.Fatalf("ReduceMotion SpecAgentPulseOpacity(800ms) = %v, want 1.0", reducedMidPulse)
	}
}

// #1002 Acceptance 1: Setting dark text_muted back to the retired #6F7684 causes the
// theme-file WCAG 2.2 contrast table check to fail below 4.5:1.
func TestSpecContrastTableRejectsRetiredMutedColor(t *testing.T) {
	raw := string(editor.SpecThemeYAML())
	mutated := strings.Replace(raw, `specTextFaint: "#8B92A0"`, `specTextFaint: "#6F7684"`, 1)
	if mutated == raw {
		t.Fatal("failed to locate dark specTextFaint in spec_theme.yaml")
	}
	th, err := core.ParseTheme([]byte(mutated))
	if err != nil {
		t.Fatalf("core.ParseTheme(mutated): %v", err)
	}
	retired, ok := th.Dark.Color("specTextFaint")
	if !ok || retired != core.RGB(0x6F7684) {
		t.Fatalf("mutated specTextFaint = #%06X (ok=%v), want #6F7684", uint32(retired)>>8, ok)
	}

	pal := editor.DarkSpecPalette()
	pal.TextFaint = retired

	failed := 0
	for _, row := range editor.SpecContrastTable(pal) {
		if row.Ratio() < row.MinRatio {
			failed++
		}
	}
	if failed == 0 {
		t.Fatal("expected SpecContrastTable to fail when dark text_muted is reverted to #6F7684")
	}
}

var (
	specHexColorStringRe = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	specRGBColorStringRe = regexp.MustCompile(`(?i)^rgba?\s*\(\s*[0-9.+%\s,-]+\)$`)
)

func isSpecColorStringLiteral(s string) bool {
	return specHexColorStringRe.MatchString(s) || specRGBColorStringRe.MatchString(s)
}

func exprTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			return pkg.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.StarExpr:
		return exprTypeName(t.X)
	}
	return ""
}

// evalConstInt evaluates constant integer expressions (including hex/decimal literals,
// unary ops, integer casts like uint32(...), and constant arithmetic/bitwise ops).
func evalConstInt(expr ast.Expr) (uint64, bool) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return evalConstInt(e.X)
	case *ast.BasicLit:
		if e.Kind != token.INT {
			return 0, false
		}
		v, err := strconv.ParseUint(strings.ReplaceAll(e.Value, "_", ""), 0, 64)
		if err != nil {
			return 0, false
		}
		return v, true
	case *ast.UnaryExpr:
		v, ok := evalConstInt(e.X)
		if !ok {
			return 0, false
		}
		switch e.Op {
		case token.ADD:
			return v, true
		case token.SUB:
			return uint64(-int64(v)), true
		case token.XOR:
			return ^v, true
		}
		return 0, false
	case *ast.BinaryExpr:
		lv, lok := evalConstInt(e.X)
		rv, rok := evalConstInt(e.Y)
		if !lok || !rok {
			return 0, false
		}
		switch e.Op {
		case token.OR:
			return lv | rv, true
		case token.AND:
			return lv & rv, true
		case token.XOR:
			return lv ^ rv, true
		case token.SHL:
			return lv << rv, true
		case token.SHR:
			return lv >> rv, true
		case token.ADD:
			return lv + rv, true
		case token.SUB:
			return lv - rv, true
		case token.MUL:
			return lv * rv, true
		case token.QUO:
			if rv == 0 {
				return 0, false
			}
			return lv / rv, true
		}
		return 0, false
	case *ast.CallExpr:
		if len(e.Args) == 1 {
			if id, ok := e.Fun.(*ast.Ident); ok {
				switch id.Name {
				case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "int", "int8", "int16", "int32", "int64", "byte":
					return evalConstInt(e.Args[0])
				}
			}
		}
		return 0, false
	}
	return 0, false
}

// isConstNumericExpr reports whether expr is composed strictly of numeric literals,
// numeric casts, and constant arithmetic/bitwise operators (no runtime variables or calls).
func isConstNumericExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return isConstNumericExpr(e.X)
	case *ast.BasicLit:
		return e.Kind == token.INT || e.Kind == token.FLOAT
	case *ast.UnaryExpr:
		return isConstNumericExpr(e.X)
	case *ast.BinaryExpr:
		return isConstNumericExpr(e.X) && isConstNumericExpr(e.Y)
	case *ast.CallExpr:
		if len(e.Args) == 1 {
			if id, ok := e.Fun.(*ast.Ident); ok {
				switch id.Name {
				case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "int", "int8", "int16", "int32", "int64", "float32", "float64", "byte":
					return isConstNumericExpr(e.Args[0])
				}
			}
		}
		return false
	}
	return false
}

// allowedColorStringInContext returns true only for the explicitly narrowed scene-content
// data defaults and hex input placeholders (never UI chrome like core.Style).
func allowedColorStringInContext(relPath string, stack []ast.Node) bool {
	if len(stack) < 2 {
		return false
	}
	// Check immediate parent.
	parent := stack[len(stack)-2]
	if kv, ok := parent.(*ast.KeyValueExpr); ok {
		keyIdent, ok := kv.Key.(*ast.Ident)
		if !ok {
			return false
		}
		key := keyIdent.Name
		var structType string
		if len(stack) >= 3 {
			if cl, ok := stack[len(stack)-3].(*ast.CompositeLit); ok {
				structType = exprTypeName(cl.Type)
			}
		}
		switch relPath {
		case "demo.go", "first_run.go", "state.go":
			return key == "BackgroundColor" && structType == "sceneformat.SceneDoc"
		case "inspector_room.go", "inspector_scene.go":
			return key == "Placeholder" && structType == "ui.Input"
		case "environment_panel.go":
			if key == "Placeholder" && structType == "ui.Input" {
				return true
			}
			switch structType {
			case "sceneformat.SkyDoc":
				return key == "Color" || key == "Zenith" || key == "Horizon" || key == "Ground"
			case "sceneformat.FogDoc", "sceneformat.LightDoc":
				return key == "Color"
			}
		case "scene3d_edit.go":
			return key == "Color" && structType == "sceneformat.LightDoc"
		}
		return false
	}
	// materials_ui.go passes "#ffffff" / "#000000" as the placeholder argument to colorControl(...).
	if relPath == "materials_ui.go" {
		if call, ok := parent.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "colorControl" {
				return true
			}
		}
	}
	return false
}

// allowedCoreColorConstInContext returns true only for particle data default ramp/trail stops.
func allowedCoreColorConstInContext(relPath string, stack []ast.Node) bool {
	switch relPath {
	case "particles.go":
		// Allowed only inside particles.ColorCurve or particles.TrailConfig Color field.
		if len(stack) >= 3 {
			if kv, ok := stack[len(stack)-2].(*ast.KeyValueExpr); ok {
				if keyIdent, ok := kv.Key.(*ast.Ident); ok && keyIdent.Name == "Color" {
					if cl, ok := stack[len(stack)-3].(*ast.CompositeLit); ok {
						st := exprTypeName(cl.Type)
						if st == "particles.TrailConfig" {
							return true
						}
						if st == "" && len(stack) >= 4 {
							if outer, ok := stack[len(stack)-4].(*ast.CompositeLit); ok && exprTypeName(outer.Type) == "particles.ColorCurve" {
								return true
							}
						}
					}
				}
			}
		}
	case "particles_ui.go":
		for i := len(stack) - 1; i >= 0; i-- {
			if fn, ok := stack[i].(*ast.FuncDecl); ok {
				return fn.Name.Name == "particleCurveWidgetView"
			}
		}
	}
	return false
}

func checkSpecColorLiteralsInFile(fset *token.FileSet, relPath string, file *ast.File) []string {
	coreAliases := map[string]bool{}
	coreDotImported := false
	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil || importPath != "github.com/nsgo-dev/nsgo/core" {
			continue
		}
		if imp.Name == nil {
			coreAliases["core"] = true
		} else if imp.Name.Name == "." {
			coreDotImported = true
		} else if imp.Name.Name != "_" {
			coreAliases[imp.Name.Name] = true
		}
	}

	var violations []string
	var stack []ast.Node

	var walk func(ast.Node)
	walk = func(n ast.Node) {
		if n == nil {
			return
		}
		stack = append(stack, n)
		defer func() { stack = stack[:len(stack)-1] }()

		switch node := n.(type) {
		case *ast.CallExpr:
			var coreFunc string
			switch fn := node.Fun.(type) {
			case *ast.SelectorExpr:
				if ident, ok := fn.X.(*ast.Ident); ok && coreAliases[ident.Name] {
					coreFunc = fn.Sel.Name
				}
			case *ast.Ident:
				if coreDotImported {
					switch fn.Name {
					case "RGB", "RGBA", "ParseColor", "Color":
						coreFunc = fn.Name
					}
				}
				if fn.Name == "colorFromRGBA8" && len(node.Args) > 0 {
					allConst := true
					for _, arg := range node.Args {
						if !isConstNumericExpr(arg) {
							allConst = false
							break
						}
					}
					if allConst {
						pos := fset.Position(node.Pos())
						violations = append(violations, pos.String()+": forbidden colorFromRGBA8 call with all-constant arguments outside spec_theme.go")
					}
				}
			}

			switch coreFunc {
			case "RGB", "RGBA":
				pos := fset.Position(node.Pos())
				violations = append(violations, pos.String()+": forbidden core."+coreFunc+" call outside spec_theme.go")
			case "ParseColor":
				if len(node.Args) == 1 {
					if lit, ok := node.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, err := strconv.Unquote(lit.Value); err == nil && isSpecColorStringLiteral(s) {
							pos := fset.Position(node.Pos())
							violations = append(violations, pos.String()+": forbidden core.ParseColor("+lit.Value+") literal outside spec_theme.go")
						}
					}
				}
			case "Color":
				if len(node.Args) == 1 {
					if val, ok := evalConstInt(node.Args[0]); ok && val != 0 && !allowedCoreColorConstInContext(relPath, stack) {
						pos := fset.Position(node.Pos())
						violations = append(violations, pos.String()+": forbidden non-zero constant core.Color(...) literal outside spec_theme.go")
					}
				}
			}

		case *ast.BasicLit:
			if node.Kind == token.STRING {
				if s, err := strconv.Unquote(node.Value); err == nil && isSpecColorStringLiteral(s) {
					if !allowedColorStringInContext(relPath, stack) {
						pos := fset.Position(node.Pos())
						violations = append(violations, pos.String()+": forbidden color string literal "+node.Value+" outside spec_theme.go")
					}
				}
			}
		}

		ast.Inspect(n, func(child ast.Node) bool {
			if child == n || child == nil {
				return child == n
			}
			walk(child)
			return false
		})
	}
	walk(file)
	return violations
}

// #1002 Acceptance 2: Every non-test Go file in pkg/editor outside spec_theme.go
// must use Spec theme tokens instead of core.RGB, core.RGBA, constant core.Color/colorFromRGBA8,
// or hardcoded UI hex/rgb/rgba color literals. Scene content defaults (such as scene.BackgroundColor,
// sky/fog/light defaults, and hex input placeholders) stay scene data and are narrowed by exact
// struct field / call site context.
func TestSpecNoHardcodedColorLiteralsInEditor(t *testing.T) {
	fset := token.NewFileSet()
	var violations []string

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(filepath.Clean(path))
		if rel == "spec_theme.go" {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Errorf("parse %s: %v", path, parseErr)
			return nil
		}
		violations = append(violations, checkSpecColorLiteralsInFile(fset, rel, file)...)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("found %d hardcoded color literal(s) outside spec_theme.go:\n%s", len(violations), strings.Join(violations, "\n"))
	}
}

func TestSpecColorLiteralASTGuardrailCatchesPatterns(t *testing.T) {
	parseAndCheck := func(relPath, src string) []string {
		t.Helper()
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, relPath, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", relPath, err)
		}
		return checkSpecColorLiteralsInFile(fset, relPath, file)
	}

	mustReject := []struct {
		name    string
		relPath string
		src     string
	}{
		{
			name:    "3-digit hex string",
			relPath: "panel.go",
			src:     `package editor; var s = "#fff"`,
		},
		{
			name:    "4-digit hex string",
			relPath: "panel.go",
			src:     `package editor; var s = "#1234"`,
		},
		{
			name:    "6-digit hex in UI style inside allowlisted file",
			relPath: "environment_panel.go",
			src:     `package editor; import "github.com/nsgo-dev/nsgo/core"; var s = core.Style{Color: "#112233"}`,
		},
		{
			name:    "rgb string literal",
			relPath: "panel.go",
			src:     `package editor; var s = "rgb(255, 128, 0)"`,
		},
		{
			name:    "rgba string literal outside ui.Input Placeholder",
			relPath: "inspector_scene.go",
			src:     `package editor; var s = "rgba(12, 16, 30, 0.85)"`,
		},
		{
			name:    "decimal core.Color literal",
			relPath: "panel.go",
			src:     `package editor; import "github.com/nsgo-dev/nsgo/core"; var c = core.Color(123456)`,
		},
		{
			name:    "core.Color(uint32(hex)) literal",
			relPath: "panel.go",
			src:     `package editor; import "github.com/nsgo-dev/nsgo/core"; var c = core.Color(uint32(0x112233ff))`,
		},
		{
			name:    "core.Color(uint32(decimal)) literal",
			relPath: "panel.go",
			src:     `package editor; import "github.com/nsgo-dev/nsgo/core"; var c = core.Color(uint32(987654))`,
		},
		{
			name:    "aliased core import RGB",
			relPath: "panel.go",
			src:     `package editor; import c "github.com/nsgo-dev/nsgo/core"; var col = c.RGB(0x112233)`,
		},
		{
			name:    "aliased core import RGBA",
			relPath: "panel.go",
			src:     `package editor; import c "github.com/nsgo-dev/nsgo/core"; var col = c.RGBA(0x112233, 0.5)`,
		},
		{
			name:    "aliased core import ParseColor 3-digit hex",
			relPath: "panel.go",
			src:     `package editor; import c "github.com/nsgo-dev/nsgo/core"; var col, _ = c.ParseColor("#abc")`,
		},
		{
			name:    "aliased core import Color constant",
			relPath: "panel.go",
			src:     `package editor; import c "github.com/nsgo-dev/nsgo/core"; var col = c.Color(uint32(0xff0000ff))`,
		},
		{
			name:    "colorFromRGBA8 with all-constant arguments",
			relPath: "panel.go",
			src:     `package editor; var col = colorFromRGBA8(255, 255, 255, 255)`,
		},
		{
			name:    "colorFromRGBA8 with constant hex expressions",
			relPath: "panel.go",
			src:     `package editor; var col = colorFromRGBA8(0x10, 0x20, 0x30, uint8(0xff))`,
		},
	}

	for _, tc := range mustReject {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			if got := parseAndCheck(tc.relPath, tc.src); len(got) == 0 {
				t.Fatalf("expected AST guardrail to reject %q, but got 0 violations", tc.name)
			}
		})
	}

	mustAllow := []struct {
		name    string
		relPath string
		src     string
	}{
		{
			name:    "transparent core.Color(0)",
			relPath: "panel.go",
			src:     `package editor; import "github.com/nsgo-dev/nsgo/core"; var col = core.Color(0)`,
		},
		{
			name:    "dynamic colorFromRGBA8 raster pixel",
			relPath: "terrain.go",
			src:     `package editor; func f(r, g, b uint8) { _ = colorFromRGBA8(r, g, b, 255) }`,
		},
		{
			name:    "dynamic core.Color(uint32(v))",
			relPath: "particles.go",
			src:     `package editor; import "github.com/nsgo-dev/nsgo/core"; func f(v uint32) { _ = core.Color(uint32(v)) }`,
		},
		{
			name:    "allowlisted ui.Input Placeholder",
			relPath: "inspector_scene.go",
			src:     `package editor; import "github.com/nsgo-dev/nsgo/ui"; var _ = ui.Input{Placeholder: "rgba(12, 16, 30, 0.85)"}`,
		},
		{
			name:    "allowlisted sceneformat.LightDoc Color",
			relPath: "scene3d_edit.go",
			src:     `package editor; var _ = sceneformat.LightDoc{Color: "#ffffff"}`,
		},
	}

	for _, tc := range mustAllow {
		t.Run("allow/"+tc.name, func(t *testing.T) {
			if got := parseAndCheck(tc.relPath, tc.src); len(got) > 0 {
				t.Fatalf("expected AST guardrail to allow %q, got violations: %v", tc.name, got)
			}
		})
	}
}

// #1002 Acceptance 3: Header, left rail, panels, inspector, viewport surround, and status bar render in
// Spec bg, panel, viewport, and line tokens, section headings use Spec 11/600 typography, and the
// selection outline strokes in Spec Accent.Edge.
func TestSpecEditorChromeRegionsAndSelectionOutline(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("hero")
	ed.SetLayersPanelOpen(true)
	ed.SetAssetBrowserOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	verifyMode := func(label string, want editor.SpecPalette) {
		t.Helper()
		if got := app.Find("id:scene-editor").Style.Background; got != want.Bg {
			t.Errorf("%s: root bg = #%06X, want Bg #%06X", label, uint32(got)>>8, uint32(want.Bg)>>8)
		}
		tbView := app.Find("id:editor-toolbar")
		tb := tbView.Style
		if tb.Background != want.Bg || tb.BorderColors.Bottom != want.Line {
			t.Errorf("%s: header bg=#%06X bottomBorder=#%06X, want Bg=#%06X Line=#%06X",
				label, uint32(tb.Background)>>8, uint32(tb.BorderColors.Bottom)>>8, uint32(want.Bg)>>8, uint32(want.Line)>>8)
		}
		if h := tbView.Bounds().Size.Height; h != editor.SpecHeaderHeight {
			t.Errorf("%s: header height = %v, want SpecHeaderHeight %v", label, h, editor.SpecHeaderHeight)
		}
		if got := app.Find("id:header-brand-mark").Style.Background; got != want.Accent.Fg {
			t.Errorf("%s: header-brand-mark bg = #%06X, want Accent.Fg #%06X", label, uint32(got)>>8, uint32(want.Accent.Fg)>>8)
		}
		sectionTitle := app.Find("id:scene-rail-title").TextStyle
		if sectionTitle.Font.Size != editor.SpecFontSectionSize || sectionTitle.Font.Weight != editor.SpecFontSectionWeight || sectionTitle.Color != want.TextFaint {
			t.Errorf("%s: scene-rail-title = %v/%v/#%06X, want %v/%v/#%06X",
				label, sectionTitle.Font.Size, sectionTitle.Font.Weight, uint32(sectionTitle.Color)>>8,
				editor.SpecFontSectionSize, editor.SpecFontSectionWeight, uint32(want.TextFaint)>>8)
		}
		if got := app.Find("id:editor-viewport-container").Style.Background; got != want.Viewport {
			t.Errorf("%s: viewport surround = #%06X, want Viewport #%06X", label, uint32(got)>>8, uint32(want.Viewport)>>8)
		}
		dockArea := app.Find("id:editor-dock-area")
		if got := dockArea.Style.Background; got != want.Bg {
			t.Errorf("%s: editor-dock-area bg=#%06X, want Bg=#%06X",
				label, uint32(got)>>8, uint32(want.Bg)>>8)
		}
		var dockHandles int
		var walkDockHandles func(v *nsgotest.View)
		walkDockHandles = func(v *nsgotest.View) {
			if v == nil {
				return
			}
			if v.Props.Label == "Resize panel" {
				dockHandles++
				if len(v.Children) == 0 || v.Children[0].Style.Background != want.Line {
					var got core.Color
					if len(v.Children) > 0 {
						got = v.Children[0].Style.Background
					}
					t.Errorf("%s: dock split handle line bg=#%06X, want Line=#%06X",
						label, uint32(got)>>8, uint32(want.Line)>>8)
				}
			}
			for _, ch := range v.Children {
				walkDockHandles(ch)
			}
		}
		walkDockHandles(dockArea)
		if dockHandles == 0 {
			t.Errorf("%s: expected dock split handles inside editor-dock-area", label)
		}
		col := app.Find("id:editor-scene-column").Style
		if col.Background != want.Bg {
			t.Errorf("%s: editor-scene-column bg=#%06X, want Bg=#%06X",
				label, uint32(col.Background)>>8, uint32(want.Bg)>>8)
		}
		st := app.Find("id:scene-tree-panel").Style
		if st.Background != want.Bg {
			t.Errorf("%s: scene-tree-panel bg=#%06X, want Bg=#%06X",
				label, uint32(st.Background)>>8, uint32(want.Bg)>>8)
		}
		lp := app.Find("id:layers-panel").Style
		if lp.Background != want.Panel {
			t.Errorf("%s: layers-panel bg=#%06X, want Panel=#%06X",
				label, uint32(lp.Background)>>8, uint32(want.Panel)>>8)
		}
		ab := app.Find("id:asset-browser").Style
		if ab.Background != want.Panel {
			t.Errorf("%s: asset-browser bg=#%06X, want Panel=#%06X",
				label, uint32(ab.Background)>>8, uint32(want.Panel)>>8)
		}
		insp := app.Find("id:editor-inspector").Style
		if insp.Background != want.Panel {
			t.Errorf("%s: editor-inspector bg=#%06X, want Panel=#%06X",
				label, uint32(insp.Background)>>8, uint32(want.Panel)>>8)
		}
		sb := app.Find("id:editor-status").Style
		if sb.Background != want.Bg || sb.BorderColors.Top != want.Line {
			t.Errorf("%s: status bar bg=#%06X topBorder=#%06X, want Bg=#%06X Line=#%06X",
				label, uint32(sb.Background)>>8, uint32(sb.BorderColors.Top)>>8, uint32(want.Bg)>>8, uint32(want.Line)>>8)
		}

		// Verify the 2D selection outline strokes in Spec Accent.Edge.
		cw, ch := ed.CanvasSize()
		d := &core.Drawing{}
		ed.DrawViewport(d, core.Size{Width: core.Points(cw), Height: core.Points(ch)})
		foundAccentStroke := false
		for _, op := range d.Ops {
			if op.Kind == core.DrawStrokeColor && op.Color == want.Accent.Edge {
				foundAccentStroke = true
				break
			}
		}
		if !foundAccentStroke {
			t.Errorf("%s: 2D viewport selection did not stroke with Accent.Edge #%06X", label, uint32(want.Accent.Edge)>>8)
		}
	}

	verifyMode("dark", editor.DarkSpecPalette())

	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	verifyMode("light", editor.LightSpecPalette())
}

func TestSpecLightModePlayUIAndTilePaletteContrast(t *testing.T) {
	ed := newTestEditor(t)
	light := editor.LightSpecPalette()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	// 1. Enter Play mode in Light appearance and verify Play bar, Inspector|Agent tabs, and Agent panel use Spec Light tokens.
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()

	tabs := app.Find("id:play-inspector-tabs").Style
	if tabs.Background != light.Panel || tabs.BorderColor != light.Line {
		t.Fatalf("light play-inspector-tabs bg=#%06X border=#%06X, want Panel=#%06X Line=#%06X",
			uint32(tabs.Background)>>8, uint32(tabs.BorderColor)>>8, uint32(light.Panel)>>8, uint32(light.Line)>>8)
	}
	var findTextViews func(v *nsgotest.View, text string) []*nsgotest.View
	findTextViews = func(v *nsgotest.View, text string) []*nsgotest.View {
		if v == nil {
			return nil
		}
		var out []*nsgotest.View
		if v.Text == text {
			out = append(out, v)
		}
		for _, ch := range v.Children {
			out = append(out, findTextViews(ch, text)...)
		}
		return out
	}

	tabInsp := app.Find("id:play-tab-inspector")
	foundLightTabText := false
	for _, n := range findTextViews(tabInsp, "Inspector") {
		if n.TextStyle.Color != light.TextPrimary {
			t.Fatalf("light play-tab-inspector text = #%06X, want TextPrimary #%06X",
				uint32(n.TextStyle.Color)>>8, uint32(light.TextPrimary)>>8)
		}
		if r := editor.ContrastRatio(n.TextStyle.Color, light.Panel); r < 4.5 {
			t.Fatalf("light play-tab-inspector contrast = %.2f:1, want >= 4.5:1", r)
		}
		foundLightTabText = true
	}
	if !foundLightTabText {
		t.Fatal("could not find Inspector text node inside play-tab-inspector")
	}

	stopBtn := app.Find("id:play-stop-btn")
	if stopBtn.Style.Background != light.TextPrimary {
		t.Fatalf("light play-stop-btn bg = #%06X, want TextPrimary #%06X",
			uint32(stopBtn.Style.Background)>>8, uint32(light.TextPrimary)>>8)
	}
	foundStopText := false
	for _, n := range findTextViews(stopBtn, "Stop") {
		if n.TextStyle.Color != light.OnTextPrimary {
			t.Fatalf("light play-stop-btn text = #%06X, want OnTextPrimary #%06X",
				uint32(n.TextStyle.Color)>>8, uint32(light.OnTextPrimary)>>8)
		}
		foundStopText = true
	}
	if !foundStopText {
		t.Fatal("could not find Stop text node inside play-stop-btn")
	}

	app.Press("id:play-tab-agent")
	marksHdr := app.Find("id:play-marks-header")
	if marksHdr.TextStyle.Color != light.TextFaint {
		t.Fatalf("light play-marks-header color = #%06X, want TextFaint #%06X",
			uint32(marksHdr.TextStyle.Color)>>8, uint32(light.TextFaint)>>8)
	}

	app.Post(func() {
		_ = ed.Stop()
	})
	app.Settle()

	// 2. Open TilePaletteView in Light appearance and verify selected tile card uses Spec Light tokens.
	app.Post(func() {
		ed.SetActiveLayer("floor")
		ed.SetTool(editor.ToolPaintTile)
	})
	app.Settle()

	selCard := app.Find("id:tile-palette-selected").Style
	if selCard.Background != light.Raised || selCard.BorderColor != light.LineRaised {
		t.Fatalf("light tile-palette-selected bg=#%06X border=#%06X, want Raised=#%06X LineRaised=#%06X",
			uint32(selCard.Background)>>8, uint32(selCard.BorderColor)>>8, uint32(light.Raised)>>8, uint32(light.LineRaised)>>8)
	}
	selName := app.Find("id:tile-palette-selected-name")
	if selName.TextStyle.Color != light.TextPrimary {
		t.Fatalf("light tile-palette-selected-name color = #%06X, want TextPrimary #%06X",
			uint32(selName.TextStyle.Color)>>8, uint32(light.TextPrimary)>>8)
	}
	selMeta := app.Find("id:tile-palette-selected-meta")
	if selMeta.TextStyle.Color != light.TextFaint {
		t.Fatalf("light tile-palette-selected-meta color = #%06X, want TextFaint #%06X",
			uint32(selMeta.TextStyle.Color)>>8, uint32(light.TextFaint)>>8)
	}
}

func TestSpecViewport3DGridUsesActiveSpecPalette(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetViewMode(editor.ViewMode3D)

	for _, tc := range []struct {
		name       string
		appearance editor.Appearance
		want       editor.SpecPalette
	}{
		{"dark", editor.AppearanceDark, editor.DarkSpecPalette()},
		{"light", editor.AppearanceLight, editor.LightSpecPalette()},
	} {
		if err := ed.SetAppearance(tc.appearance); err != nil {
			t.Fatalf("SetAppearance(%s): %v", tc.name, err)
		}
		d := &core.Drawing{}
		ed.DrawViewport3D(d, core.Size{Width: 640, Height: 360})

		wantGround := ui.Alpha(tc.want.Viewport3DGround, 0.55)
		foundGrid, foundGround := false, false
		for _, op := range d.Ops {
			if op.Kind == core.DrawStrokeColor && op.Color == tc.want.Viewport3DGrid {
				foundGrid = true
			}
			if op.Kind == core.DrawFillColor && op.Color == wantGround {
				foundGround = true
			}
		}
		if !foundGrid {
			t.Errorf("%s: DrawViewport3D did not stroke grid with Viewport3DGrid #%06X", tc.name, uint32(tc.want.Viewport3DGrid)>>8)
		}
		if !foundGround {
			t.Errorf("%s: DrawViewport3D did not fill ground plane with Viewport3DGround #%06X", tc.name, uint32(tc.want.Viewport3DGround)>>8)
		}
	}
}
