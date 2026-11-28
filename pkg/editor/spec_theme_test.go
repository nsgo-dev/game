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

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/nsgo-dev/nsgo/ui"
)

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

	var activeSpec theme.Palette
	var uiBg, uiSurface, uiRaised, uiAccent, uiOnAccent core.Color

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		node := editor.EditorView(cx, ed)
		colors := ui.ColorsOf(cx)
		cx.Effect(func() {
			activeSpec = ed.Palette(cx)
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

	assertPalette := func(step string, want theme.Palette) {
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
	assertPalette("initial dark system", theme.Dark())

	// Switch the system appearance live to Light -> repaints immediately to Spec Light without restart.
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	assertPalette("live switch to light system", theme.Light())

	// Switch back to Dark live -> repaints immediately to Spec Dark without restart.
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	assertPalette("live switch back to dark system", theme.Dark())

	// Explicit AppearanceLight override stays on Spec Light even while system is Dark.
	app.Post(func() {
		if err := ed.SetAppearance(editor.AppearanceLight); err != nil {
			t.Errorf("SetAppearance(light): %v", err)
		}
	})
	app.Settle()
	assertPalette("explicit light on dark system", theme.Light())
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

	light := theme.Light()
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
	pal := theme.Light()
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

	light := theme.Light()
	dark := theme.Dark()

	assertChrome := func(label string, want theme.Palette) {
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
		pal        theme.Palette
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
			ratio := theme.ContrastRatio(ov.Color, tc.pal.Viewport)
			if ratio < 3.0 {
				t.Errorf("%s: overlay %q color #%06X on Viewport #%06X has contrast %.2f:1, want >= 3.0:1",
					tc.name, ov.ID, uint32(ov.Color)>>8, uint32(tc.pal.Viewport)>>8, ratio)
			}
		}
		collStroke := theme.CompositeOver(ui.Alpha(tc.pal.TextMuted, 0.85), tc.pal.Viewport)
		if ratio := theme.ContrastRatio(collStroke, tc.pal.Viewport); ratio < 3.0 {
			t.Errorf("%s: collision stroke contrast %.2f:1, want >= 3.0:1", tc.name, ratio)
		}
		if ratio := theme.ContrastRatio(tc.pal.Accent.Edge, tc.pal.Viewport); ratio < 3.0 {
			t.Errorf("%s: marquee/selection Accent.Edge contrast %.2f:1, want >= 3.0:1", tc.name, ratio)
		}
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
						violations = append(violations, pos.String()+": forbidden colorFromRGBA8 call with all-constant arguments outside theme/theme.go")
					}
				}
			}

			switch coreFunc {
			case "RGB", "RGBA":
				pos := fset.Position(node.Pos())
				violations = append(violations, pos.String()+": forbidden core."+coreFunc+" call outside theme/theme.go")
			case "ParseColor":
				if len(node.Args) == 1 {
					if lit, ok := node.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, err := strconv.Unquote(lit.Value); err == nil && isSpecColorStringLiteral(s) {
							pos := fset.Position(node.Pos())
							violations = append(violations, pos.String()+": forbidden core.ParseColor("+lit.Value+") literal outside theme/theme.go")
						}
					}
				}
			case "Color":
				if len(node.Args) == 1 {
					if val, ok := evalConstInt(node.Args[0]); ok && val != 0 && !allowedCoreColorConstInContext(relPath, stack) {
						pos := fset.Position(node.Pos())
						violations = append(violations, pos.String()+": forbidden non-zero constant core.Color(...) literal outside theme/theme.go")
					}
				}
			}

		case *ast.BasicLit:
			if node.Kind == token.STRING {
				if s, err := strconv.Unquote(node.Value); err == nil && isSpecColorStringLiteral(s) {
					if !allowedColorStringInContext(relPath, stack) {
						pos := fset.Position(node.Pos())
						violations = append(violations, pos.String()+": forbidden color string literal "+node.Value+" outside theme/theme.go")
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

// #1002 Acceptance 2: Every non-test Go file in pkg/editor outside theme/theme.go must use Spec theme tokens instead of core.RGB, core.RGBA, constant core.Color/colorFromRGBA8, or hardcoded UI hex/rgb/rgba color literals. Scene content defaults (such as scene.BackgroundColor, sky/fog/light defaults, and hex input placeholders) stay scene data and are narrowed by exact struct field / call site context.
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
		if rel == "theme/theme.go" {
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
		t.Fatalf("found %d hardcoded color literal(s) outside theme/theme.go:\n%s", len(violations), strings.Join(violations, "\n"))
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

	verifyMode := func(label string, want theme.Palette) {
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
		if h := tbView.Bounds().Size.Height; h != theme.HeaderHeight {
			t.Errorf("%s: header height = %v, want theme.HeaderHeight %v", label, h, theme.HeaderHeight)
		}
		if got := app.Find("id:header-brand-mark").Style.Background; got != want.Accent.Fg {
			t.Errorf("%s: header-brand-mark bg = #%06X, want Accent.Fg #%06X", label, uint32(got)>>8, uint32(want.Accent.Fg)>>8)
		}
		sectionTitle := app.Find("id:scene-rail-title").TextStyle
		if sectionTitle.Font.Size != theme.FontSectionSize || sectionTitle.Font.Weight != theme.FontSectionWeight || sectionTitle.Color != want.TextFaint {
			t.Errorf("%s: scene-rail-title = %v/%v/#%06X, want %v/%v/#%06X",
				label, sectionTitle.Font.Size, sectionTitle.Font.Weight, uint32(sectionTitle.Color)>>8,
				theme.FontSectionSize, theme.FontSectionWeight, uint32(want.TextFaint)>>8)
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

	verifyMode("dark", theme.Dark())

	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	verifyMode("light", theme.Light())
}

func TestSpecLightModePlayUIAndTilePaletteContrast(t *testing.T) {
	ed := newTestEditor(t)
	light := theme.Light()

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
		if r := theme.ContrastRatio(n.TextStyle.Color, light.Panel); r < 4.5 {
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
		want       theme.Palette
	}{
		{"dark", editor.AppearanceDark, theme.Dark()},
		{"light", editor.AppearanceLight, theme.Light()},
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

// themeImportNames returns the names file uses for the github.com/nsgo-dev/game/pkg/editor/theme package.
func themeImportNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil || importPath != "github.com/nsgo-dev/game/pkg/editor/theme" {
			continue
		}
		switch {
		case imp.Name == nil:
			names["theme"] = true
		case imp.Name.Name != "_" && imp.Name.Name != ".":
			names[imp.Name.Name] = true
		}
	}
	return names
}

// checkDarkOnlyPackageColorVarsInFile reports every package-level var in file whose value selects theme.Dark or theme.Light, which would freeze that color to one appearance.
func checkDarkOnlyPackageColorVarsInFile(fset *token.FileSet, file *ast.File) []string {
	themeNames := themeImportNames(file)
	if len(themeNames) == 0 {
		return nil
	}
	var violations []string
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, val := range vs.Values {
				ast.Inspect(val, func(n ast.Node) bool {
					sel, ok := n.(*ast.SelectorExpr)
					if !ok || (sel.Sel.Name != "Dark" && sel.Sel.Name != "Light") {
						return true
					}
					if x, ok := sel.X.(*ast.Ident); ok && themeNames[x.Name] {
						pos := fset.Position(sel.Pos())
						violations = append(violations, pos.String()+": forbidden package-level var referencing "+x.Name+"."+sel.Sel.Name+" (use ed.Palette(cx) instead)")
					}
					return true
				})
			}
		}
	}
	return violations
}

// TestSpecNoDarkOnlyPackageColorVarsInEditor enforces that no non-test file under pkg/editor declares package-level color variables initialized from theme.Dark or theme.Light, ensuring every editor panel and overlay reacts dynamically to Dark/Light theme changes via Editor.Palette(cx) or Editor.activePalette().
func TestSpecNoDarkOnlyPackageColorVarsInEditor(t *testing.T) {
	fset := token.NewFileSet()
	var violations []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// The walk root is ".", so a hidden-directory check must not match it, or the walk would skip every file.
			if d.Name() == "testdata" || (path != "." && strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		violations = append(violations, checkDarkOnlyPackageColorVarsInFile(fset, file)...)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("found %d dark/light-only package-level color var(s):\n%s", len(violations), strings.Join(violations, "\n"))
	}
}

// TestSpecDarkOnlyPackageColorVarGuardrailCatchesPatterns keeps TestSpecNoDarkOnlyPackageColorVarsInEditor from passing vacuously: it must reject package-level vars built from theme.Dark or theme.Light, under any import name, and accept the same calls inside functions.
func TestSpecDarkOnlyPackageColorVarGuardrailCatchesPatterns(t *testing.T) {
	check := func(src string) []string {
		t.Helper()
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "x.go", src, 0)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return checkDarkOnlyPackageColorVarsInFile(fset, file)
	}
	const imp = `import "github.com/nsgo-dev/game/pkg/editor/theme"; `
	for _, src := range []string{
		`package editor; ` + imp + `var bg = theme.Dark().Bg`,
		`package editor; ` + imp + `var line = theme.Light().Line`,
		`package editor; ` + imp + `var pal = theme.Dark`,
		`package editor; import tk "github.com/nsgo-dev/game/pkg/editor/theme"; var bg = tk.Dark().Bg`,
		`package editor; ` + imp + `var (a = 1; fg = theme.Light().TextPrimary)`,
	} {
		if got := check(src); len(got) != 1 {
			t.Errorf("guardrail reported %d violation(s), want 1, for %s: %v", len(got), src, got)
		}
	}
	for _, src := range []string{
		`package editor; ` + imp + `func f() { _ = theme.Dark().Bg }`,
		`package editor; type x struct{ Dark bool }; var v x; var d = v.Dark`,
	} {
		if got := check(src); len(got) != 0 {
			t.Errorf("guardrail reported %v, want none, for %s", got, src)
		}
	}
}
