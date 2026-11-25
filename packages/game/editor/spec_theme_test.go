package editor_test

import (
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
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

	// In 3D mode the 2D overlay bar is hidden, so toast drops from Bottom: 60 to Bottom: 16 (+44pt lower).
	app.Post(func() {
		ed.SetViewMode(editor.ViewMode3D)
	})
	app.Settle()
	toast3DRect := app.Find("id:editor-toast").Bounds()
	if toast3DRect.Origin.Y <= toastRect.Origin.Y+40 {
		t.Fatalf("3D mode toast Y = %v, want > %v (44pt lower than 2D edit mode)", toast3DRect.Origin.Y, toastRect.Origin.Y+40)
	}

	// Switching back to 2D restores the 60pt clearance.
	app.Post(func() {
		ed.SetViewMode(editor.ViewMode2D)
	})
	app.Settle()
	toastBackRect := app.Find("id:editor-toast").Bounds()
	if math.Abs(float64(toastBackRect.Origin.Y-toastRect.Origin.Y)) > 1 {
		t.Fatalf("restored 2D mode toast Y = %v, want %v", toastBackRect.Origin.Y, toastRect.Origin.Y)
	}
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
