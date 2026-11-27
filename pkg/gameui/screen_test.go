package gameui_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestScreenParseValidationAndRoundTrip(t *testing.T) {
	for _, name := range []string{"hud.screen.yaml", "pause.screen.yaml"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		doc, err := gameui.ParseScreen(data)
		if err != nil {
			t.Fatalf("%s: ParseScreen: %v", name, err)
		}
		encoded, err := gameui.EncodeScreen(doc)
		if err != nil {
			t.Fatalf("%s: EncodeScreen: %v", name, err)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatalf("%s: round-trip mismatch:\ngot:\n%s\nwant:\n%s", name, encoded, data)
		}
	}

	invalid := []struct {
		name string
		yaml string
		want string
	}{
		{"empty", "", "empty"},
		{"missing version", "designWidth: 640\ndesignHeight: 360\n", "version"},
		{"zero version", "version: 0\ndesignWidth: 640\ndesignHeight: 360\n", "version"},
		{"future version", "version: 2\ndesignWidth: 640\ndesignHeight: 360\n", "version"},
		{"unknown field", "version: 1\ndesignWidth: 640\ndesignHeight: 360\nextra: 1\n", "extra"},
		{"non-positive designWidth", "version: 1\ndesignWidth: 0\ndesignHeight: 360\n", "designWidth"},
		{"bad scaleMode", "version: 1\ndesignWidth: 640\ndesignHeight: 360\nscaleMode: zoom\n", "scaleMode"},
		{"unknown node type", "version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: a\n    type: slider\n", "unknown type"},
		{"unknown anchor", "version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: a\n    type: label\n    anchor: middle\n", "unknown anchor"},
		{"duplicate id", "version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: dup\n    type: label\n  - id: dup\n    type: button\n", "duplicate node id"},
		{"duplicate focusOrder", "version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: a\n    type: button\n    focusOrder: 1\n  - id: b\n    type: button\n    focusOrder: 1\n", "repeats focusOrder"},
		{"invalid deviceClass", "version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: a\n    type: container\n    deviceClasses: [watch]\n", "unknown deviceClass"},
		{"invalid color", "version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: a\n    type: color_rect\n    background: notacolor\n", "invalid background"},
		{"children on leaf", "version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: a\n    type: label\n    children:\n      - id: b\n        type: label\n", "cannot have children"},
		{"yaml anchor", "version: 1\ndesignWidth: 640\ndesignHeight: 360\nname: &n hud\n", "YAML anchors"},
		{"trailing doc", "version: 1\ndesignWidth: 640\ndesignHeight: 360\n---\nversion: 1\n", "trailing YAML document"},
	}

	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gameui.ParseScreen([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("ParseScreen accepted invalid YAML:\n%s", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
			if _, ok := loaderr.As(err); !ok {
				t.Fatalf("error %T is not *loaderr.Error: %v", err, err)
			}
		})
	}
}

// Acceptance: A HUD file with a top-left status box and a bottom-right touch button builds
// and places them in the safe area on a notched phone size and on desktop in a test.
func TestScreenBuildHUDNotchedPhoneAndDesktop(t *testing.T) {
	hudYAML := `version: 1
name: hud
designWidth: 640
designHeight: 360
nodes:
  - id: status
    type: vbox
    anchor: top_left
    offset:
      x: 12
      "y": 12
    width: 52
    height: 38
    spacing: 4
    children:
      - id: hearts
        type: sprite_row
        width: 52
        height: 14
        spriteCount: 3
      - id: coins
        type: label
        width: 52
        height: 16
        textKey: hud.coins
  - id: jump
    type: touch_button
    anchor: bottom_right
    offset:
      x: 24
      "y": 28
    width: 52
    height: 52
    touchHitSize: 64
    textKey: hud.jump
  - id: mobile_stick
    type: touch_stick
    anchor: bottom_left
    offset:
      x: 24
      "y": 20
    width: 72
    height: 72
    touchHitSize: 96
    deviceClasses:
      - phone
      - tablet
`
	doc, err := gameui.ParseScreen([]byte(hudYAML))
	if err != nil {
		t.Fatalf("ParseScreen: %v", err)
	}

	cat := l10n.NewCatalog("en")
	cat.Set("en", "hud.coins", "x 12")
	cat.Set("en", "hud.jump", "Jump")

	// 1. Notched phone: 780x360 with left/right 44 pt notch insets and 20 pt bottom home-indicator inset.
	phone := gameui.ReferenceDevices()[0]
	if phone.Class != gameui.DevicePhone {
		t.Fatalf("expected phone first in ReferenceDevices, got %+v", phone)
	}
	jumpClicks := 0
	phoneScreen, err := gameui.BuildScreen(doc, gameui.ScreenBuildOptions{
		SafeArea:       phone.SafeArea,
		DeviceClass:    phone.Class,
		ViewportWidth:  phone.Width,
		ViewportHeight: phone.Height,
		Catalog:        cat,
		Locale:         "en",
		OnClick:        map[string]func(){"jump": func() { jumpClicks++ }},
	})
	if err != nil {
		t.Fatalf("BuildScreen phone: %v", err)
	}
	phoneScreen.Layout(core.Bounds{Size: core.Size{Width: core.Points(phone.Width), Height: core.Points(phone.Height)}})

	statusB := phoneScreen.Widget("status").Bounds()
	// Safe left (44) + offset.x (12) = 56; Safe top (0) + offset.y (12) = 12.
	if statusB.Origin.X != 56 || statusB.Origin.Y != 12 {
		t.Fatalf("phone status origin = (%v, %v), want (56, 12)", statusB.Origin.X, statusB.Origin.Y)
	}
	// Verify status stays strictly inside safe area [44, 736] x [0, 340].
	if float32(statusB.Origin.X) < float32(phone.SafeArea.Left) ||
		float32(statusB.Origin.X+statusB.Size.Width) > phone.Width-float32(phone.SafeArea.Right) {
		t.Fatalf("phone status %+v violates horizontal safe area", statusB)
	}

	jumpB := phoneScreen.Widget("jump").Bounds()
	// Right edge 780 - safeRight(44) - offset.x(24) - width(52) = 660.
	// Bottom edge 360 - safeBottom(20) - offset.y(28) - height(52) = 260.
	if jumpB.Origin.X != 660 || jumpB.Origin.Y != 260 || jumpB.Size.Width != 52 || jumpB.Size.Height != 52 {
		t.Fatalf("phone jump bounds = %+v, want origin (660,260) size (52,52)", jumpB)
	}
	if float32(jumpB.Origin.X+jumpB.Size.Width) > phone.Width-float32(phone.SafeArea.Right) ||
		float32(jumpB.Origin.Y+jumpB.Size.Height) > phone.Height-float32(phone.SafeArea.Bottom) {
		t.Fatalf("phone jump %+v violates safe area", jumpB)
	}

	// Expanded touch hit size (64 pt diameter around center (686, 286), radius 32):
	// visual edge is at x=660 (26 pt from center), so tapping x=656 (30 pt from center) still hits!
	if !phoneScreen.Root.OnPress(core.PressEvent{Position: core.Point{X: 656, Y: 286}}) {
		t.Fatal("expected tap inside 64 pt expanded touchHitSize to hit jump button")
	}
	if jumpClicks != 1 {
		t.Fatalf("jumpClicks = %d, want 1", jumpClicks)
	}

	// Conditional mobile_stick is visible on phone.
	stickB := phoneScreen.Widget("mobile_stick").Bounds()
	if stickB.Size.Width != 72 || stickB.Origin.X != 68 { // 44 + 24 = 68
		t.Fatalf("phone mobile_stick bounds = %+v, want x=68 w=72", stickB)
	}

	// 2. Desktop: 640x360 with zero safe-area insets.
	desktop := gameui.ReferenceDevices()[2]
	if desktop.Class != gameui.DeviceDesktop {
		t.Fatalf("expected desktop third in ReferenceDevices, got %+v", desktop)
	}
	desktopScreen, err := gameui.BuildScreen(doc, gameui.ScreenBuildOptions{
		SafeArea:       desktop.SafeArea,
		DeviceClass:    desktop.Class,
		ViewportWidth:  desktop.Width,
		ViewportHeight: desktop.Height,
		Catalog:        cat,
		Locale:         "en",
	})
	if err != nil {
		t.Fatalf("BuildScreen desktop: %v", err)
	}
	desktopScreen.Layout(core.Bounds{Size: core.Size{Width: core.Points(desktop.Width), Height: core.Points(desktop.Height)}})

	dStatus := desktopScreen.Widget("status").Bounds()
	if dStatus.Origin.X != 12 || dStatus.Origin.Y != 12 {
		t.Fatalf("desktop status origin = (%v, %v), want (12, 12)", dStatus.Origin.X, dStatus.Origin.Y)
	}
	dJump := desktopScreen.Widget("jump").Bounds()
	// 640 - 24 - 52 = 564; 360 - 28 - 52 = 280.
	if dJump.Origin.X != 564 || dJump.Origin.Y != 280 {
		t.Fatalf("desktop jump origin = (%v, %v), want (564, 280)", dJump.Origin.X, dJump.Origin.Y)
	}
	// mobile_stick is restricted to [phone, tablet], so hidden on desktop.
	dStick := desktopScreen.Widget("mobile_stick").Bounds()
	if dStick.Size.Width != 0 || dStick.Size.Height != 0 {
		t.Fatalf("expected mobile_stick hidden on desktop, got bounds %+v", dStick)
	}
}

func TestScreenBuildFocusOrderAndLocaleSwitch(t *testing.T) {
	// Deliberately declare buttons in YAML out of focus order (3, 1, 2) to prove declared focusOrder controls navigation.
	src := `version: 1
name: menu
designWidth: 640
designHeight: 360
nodes:
  - id: third
    type: button
    anchor: center
    offset:
      "y": 40
    width: 120
    height: 26
    textKey: menu.third
    focusOrder: 3
  - id: first
    type: button
    anchor: center
    offset:
      "y": -40
    sizeMode: fit_text
    height: 26
    padding:
      left: 8
      right: 8
    textKey: menu.first
    focusOrder: 1
  - id: second
    type: button
    anchor: center
    width: 120
    height: 26
    textKey: menu.second
    focusOrder: 2
`
	doc, err := gameui.ParseScreen([]byte(src))
	if err != nil {
		t.Fatal(err)
	}

	cat := l10n.NewCatalog("en")
	cat.Set("en", "menu.first", "Play")
	cat.Set("en", "menu.second", "Options")
	cat.Set("en", "menu.third", "Quit")
	cat.Set("zh", "menu.first", "从上一个检查点重新开始")
	cat.Set("zh", "menu.second", "设置")
	cat.Set("zh", "menu.third", "退出")

	built, err := gameui.BuildScreen(doc, gameui.ScreenBuildOptions{
		Catalog: cat,
		Locale:  "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	built.Layout(core.Bounds{Size: core.Size{Width: 640, Height: 360}})

	first := built.Widget("first")
	second := built.Widget("second")
	third := built.Widget("third")

	// Tab / FocusNext traverses 1 -> 2 -> 3 -> 1.
	built.Root.FocusNext()
	if built.Root.Focused() != first {
		t.Fatalf("1st FocusNext focused %v, want first", built.Root.Focused())
	}
	built.Root.FocusNext()
	if built.Root.Focused() != second {
		t.Fatalf("2nd FocusNext focused %v, want second", built.Root.Focused())
	}
	built.Root.FocusNext()
	if built.Root.Focused() != third {
		t.Fatalf("3rd FocusNext focused %v, want third", built.Root.Focused())
	}
	built.Root.FocusNext()
	if built.Root.Focused() != first {
		t.Fatalf("4th FocusNext wrap focused %v, want first", built.Root.Focused())
	}

	// SpatialNavigator NavDown / NavUp follows declared focusOrder too.
	built.Navigator.Move(gameui.NavDown)
	if built.Navigator.Focused() != second {
		t.Fatalf("NavDown focused %v, want second", built.Navigator.Focused())
	}
	built.Navigator.Move(gameui.NavUp)
	if built.Navigator.Focused() != first {
		t.Fatalf("NavUp focused %v, want first", built.Navigator.Focused())
	}

	// Switching to zh expands the fit_text button horizontally.
	enWidth := first.Bounds().Size.Width
	built.ApplyCatalog(cat, "zh")
	built.Layout(core.Bounds{Size: core.Size{Width: 640, Height: 360}})
	zhWidth := first.Bounds().Size.Width
	if zhWidth <= enWidth {
		t.Fatalf("expected fit_text width in zh (%v) > en (%v)", zhWidth, enWidth)
	}
}

func TestCheckScreenRules(t *testing.T) {
	src := `version: 1
name: hud
designWidth: 640
designHeight: 360
nodes:
  - id: floor_label
    type: label
    anchor: top_center
    offset:
      "y": 12
    sizeMode: fit_text
    height: 18
    textKey: hud.floor
    textColor: '#8a7d62'
    background: '#1c2230'
  - id: hint
    type: label
    anchor: bottom_center
    offset:
      "y": 70
    width: 160
    height: 22
    padding:
      left: 6
      right: 6
    textKey: hud.hint
    textColor: '#ffffff'
    background: rgba(16,18,22,0.78)
  - id: missing_lbl
    type: label
    anchor: top_left
    sizeMode: fit_text
    height: 18
    textKey: hud.missing
  - id: attack
    type: touch_button
    anchor: bottom_right
    offset:
      x: 92
      "y": 20
    width: 36
    height: 36
    touchHitSize: 36
    deviceClasses:
      - phone
      - tablet
`
	doc, err := gameui.ParseScreen([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	issues := gameui.CheckScreen(doc, gameui.ScreenCheckOptions{
		Locales: map[string]map[string]string{
			"en": {
				"hud.floor":   "Floor 1",
				"hud.hint":    "Jump at levers to pull",
				"hud.missing": "Only English",
			},
			"zh": {
				"hud.floor": "第一层 · 地窖",
				"hud.hint":  "靠近拉杆时按下跳跃键即可拉动",
			},
		},
	})

	got := make(map[gameui.ScreenIssueCode]gameui.ScreenIssue)
	for _, iss := range issues {
		got[iss.Code] = iss
	}

	if iss, ok := got[gameui.IssueTouchAreaTooSmall]; !ok || iss.NodeID != "attack" {
		t.Fatalf("expected touch area issue for attack, got %+v", issues)
	}
	if iss, ok := got[gameui.IssueTextOverflow]; !ok || iss.NodeID != "hint" || iss.Locale != "zh" {
		t.Fatalf("expected zh overflow issue for hint, got %+v", issues)
	}
	if iss, ok := got[gameui.IssueMissingTextKey]; !ok || iss.NodeID != "missing_lbl" || iss.Locale != "zh" {
		t.Fatalf("expected missing textKey issue for missing_lbl in zh, got %+v", issues)
	}
	if iss, ok := got[gameui.IssueLowContrast]; !ok || iss.NodeID != "floor_label" {
		t.Fatalf("expected low contrast issue for floor_label, got %+v", issues)
	}
}

func TestScreenRenderInNSGoTestCanvas(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "hud.screen.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := gameui.ParseScreen(data)
	if err != nil {
		t.Fatal(err)
	}
	cat := l10n.NewCatalog("en")
	cat.Set("en", "hud.coins", "x 12")
	cat.Set("en", "hud.floor.1", "Floor 1")
	cat.Set("en", "hud.pause", "II")
	cat.Set("en", "hud.hint.lever", "Jump at levers")
	cat.Set("en", "hud.jump", "Jump")
	cat.Set("en", "hud.attack", "Hit")

	phone := gameui.ReferenceDevices()[0]
	built, err := gameui.BuildScreen(doc, gameui.ScreenBuildOptions{
		SafeArea:       phone.SafeArea,
		DeviceClass:    phone.Class,
		ViewportWidth:  phone.Width,
		ViewportHeight: phone.Height,
		Catalog:        cat,
		Locale:         "en",
	})
	if err != nil {
		t.Fatal(err)
	}

	drawn := 0
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "hud-canvas",
			Style:  core.Style{Width: core.Pt(core.Points(phone.Width)), Height: core.Pt(core.Points(phone.Height))},
			Draw: func(d *core.Drawing, size core.Size) {
				built.Draw(d, core.Bounds{Size: size})
				drawn++
			},
		}
	})
	app.Settle()

	if drawn == 0 {
		t.Fatal("expected Canvas.Draw to render BuiltScreen")
	}
	if b := built.Widget("status").Bounds(); b.Origin.X != 56 || b.Origin.Y != 12 {
		t.Fatalf("rendered status bounds = %+v, want origin (56, 12)", b)
	}
	if b := built.Widget("jump").Bounds(); b.Origin.X != 660 || b.Origin.Y != 260 {
		t.Fatalf("rendered jump bounds = %+v, want origin (660, 260)", b)
	}
}

func TestScreenNestedDeviceClassesValidationAndSetDeviceClass(t *testing.T) {
	// 1. Disjoint parent/child deviceClasses are refused by ParseScreen, ValidateScreen, and BuildScreen.
	disjointCases := []struct {
		name     string
		yaml     string
		wantPath string
	}{
		{
			name: "direct disjoint child",
			yaml: `version: 1
designWidth: 640
designHeight: 360
nodes:
  - id: phone_box
    type: vbox
    deviceClasses: [phone]
    children:
      - id: tablet_lbl
        type: label
        deviceClasses: [tablet]
`,
			wantPath: "nodes[0].children[0].deviceClasses",
		},
		{
			name: "three-level disjoint grandchild",
			yaml: `version: 1
designWidth: 640
designHeight: 360
nodes:
  - id: mobile
    type: container
    deviceClasses: [phone, tablet]
    children:
      - id: wide_touch
        type: vbox
        deviceClasses: [tablet, desktop]
        children:
          - id: phone_only
            type: label
            deviceClasses: [phone]
`,
			wantPath: "nodes[0].children[0].children[0].deviceClasses",
		},
	}

	for _, tc := range disjointCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gameui.ParseScreen([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("ParseScreen accepted disjoint deviceClasses:\n%s", tc.yaml)
			}
			le, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("error %T is not *loaderr.Error: %v", err, err)
			}
			if le.Field != tc.wantPath {
				t.Errorf("error Field = %q, want %q", le.Field, tc.wantPath)
			}
			if le.Line <= 0 {
				t.Errorf("expected positive Line in %+v", le)
			}
			if !strings.Contains(le.Problem, "do not overlap parent deviceClasses") {
				t.Errorf("error Problem = %q, want overlap message", le.Problem)
			}
			if le.Fix == "" {
				t.Errorf("expected non-empty Fix hint in %+v", le)
			}
		})
	}

	// ValidateScreen and BuildScreen also reject a programmatically constructed disjoint ScreenDoc.
	disjointDoc := &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []gameui.ScreenNodeDoc{
			{
				ID:            "phone_box",
				Type:          gameui.NodeVBox,
				DeviceClasses: []gameui.DeviceClass{gameui.DevicePhone},
				Children: []gameui.ScreenNodeDoc{
					{
						ID:            "tablet_lbl",
						Type:          gameui.NodeLabel,
						DeviceClasses: []gameui.DeviceClass{gameui.DeviceTablet},
					},
				},
			},
		},
	}
	if err := gameui.ValidateScreen(disjointDoc); err == nil {
		t.Fatal("ValidateScreen accepted disjoint nested deviceClasses")
	}
	if _, err := gameui.BuildScreen(disjointDoc, gameui.ScreenBuildOptions{DeviceClass: gameui.DevicePhone}); err == nil {
		t.Fatal("BuildScreen accepted disjoint nested deviceClasses")
	}

	// CheckScreen must treat a hand-constructed disjoint touch node as visible on no device
	// (no spurious IssueTouchAreaTooSmall on phones/tablets).
	disjointTouchDoc := &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []gameui.ScreenNodeDoc{
			{
				ID:            "desktop_group",
				Type:          gameui.NodeContainer,
				DeviceClasses: []gameui.DeviceClass{gameui.DeviceDesktop},
				Children: []gameui.ScreenNodeDoc{
					{
						ID:            "tiny_phone_btn",
						Type:          gameui.NodeTouchButton,
						Width:         20,
						Height:        20,
						TouchHitSize:  20,
						DeviceClasses: []gameui.DeviceClass{gameui.DevicePhone},
					},
				},
			},
		},
	}
	for _, iss := range gameui.CheckScreen(disjointTouchDoc, gameui.ScreenCheckOptions{}) {
		if iss.Code == gameui.IssueTouchAreaTooSmall && iss.NodeID == "tiny_phone_btn" {
			t.Fatalf("CheckScreen reported touch area issue on unreachable disjoint node: %+v", iss)
		}
	}

	// 2. Overlapping nested deviceClasses narrow visibility cleanly on every device class and under SetDeviceClass.
	overlapYAML := `version: 1
name: overlap
designWidth: 640
designHeight: 360
nodes:
  - id: mobile
    type: container
    deviceClasses: [phone, tablet]
    children:
      - id: tablet_only_box
        type: vbox
        anchor: top_left
        width: 120
        height: 40
        deviceClasses: [tablet, desktop]
        children:
          - id: inherited_lbl
            type: label
            width: 100
            height: 18
            textKey: hud.tablet_only
`
	doc, err := gameui.ParseScreen([]byte(overlapYAML))
	if err != nil {
		t.Fatalf("ParseScreen overlapping: %v", err)
	}
	bounds := core.Bounds{Size: core.Size{Width: 640, Height: 360}}

	checkVisible := func(t *testing.T, built *gameui.BuiltScreen, dc gameui.DeviceClass) {
		t.Helper()
		built.Layout(bounds)
		wantMobile := dc == "" || dc == gameui.DevicePhone || dc == gameui.DeviceTablet
		wantTabletOnly := dc == "" || dc == gameui.DeviceTablet

		if got := built.Widget("mobile").Bounds().Size.Width > 0; got != wantMobile {
			t.Errorf("dc=%q: mobile visible=%v, want %v", dc, got, wantMobile)
		}
		if got := built.Widget("tablet_only_box").Bounds().Size.Width > 0; got != wantTabletOnly {
			t.Errorf("dc=%q: tablet_only_box visible=%v, want %v", dc, got, wantTabletOnly)
		}
		if got := built.Widget("inherited_lbl").Bounds().Size.Width > 0; got != wantTabletOnly {
			t.Errorf("dc=%q: inherited_lbl visible=%v, want %v", dc, got, wantTabletOnly)
		}
	}

	allClasses := []gameui.DeviceClass{
		gameui.DevicePhone,
		gameui.DeviceTablet,
		gameui.DeviceDesktop,
		gameui.DeviceUltrawide,
		"",
	}
	for _, dc := range allClasses {
		built, err := gameui.BuildScreen(doc, gameui.ScreenBuildOptions{DeviceClass: dc})
		if err != nil {
			t.Fatalf("BuildScreen(%q): %v", dc, err)
		}
		checkVisible(t, built, dc)
	}

	// Dynamically switching device class after build updates every nested node consistently.
	built, err := gameui.BuildScreen(doc, gameui.ScreenBuildOptions{DeviceClass: gameui.DevicePhone})
	if err != nil {
		t.Fatalf("BuildScreen: %v", err)
	}
	for _, dc := range []gameui.DeviceClass{
		gameui.DeviceTablet,
		gameui.DeviceDesktop,
		gameui.DevicePhone,
		gameui.DeviceUltrawide,
		"",
		gameui.DeviceTablet,
	} {
		built.SetDeviceClass(dc)
		checkVisible(t, built, dc)
	}
}

func TestScreenNodeTypeFieldValidationMatrix(t *testing.T) {
	allTypes := []gameui.ScreenNodeType{
		gameui.NodeVBox,
		gameui.NodeHBox,
		gameui.NodeContainer,
		gameui.NodeLabel,
		gameui.NodeButton,
		gameui.NodeSpriteRow,
		gameui.NodeColorRect,
		gameui.NodeTouchStick,
		gameui.NodeTouchButton,
		gameui.NodeImageProgress,
	}

	type fieldCase struct {
		field   string
		snippet string
		allowed map[gameui.ScreenNodeType]bool
	}

	fields := []fieldCase{
		{
			field:   "background",
			snippet: "    background: '#112233'\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeVBox:        true,
				gameui.NodeHBox:        true,
				gameui.NodeContainer:   true,
				gameui.NodeLabel:       true,
				gameui.NodeButton:      true,
				gameui.NodeSpriteRow:   true,
				gameui.NodeColorRect:   true,
				gameui.NodeTouchStick:  true,
				gameui.NodeTouchButton: true,
			},
		},
		{
			field:   "sizeMode",
			snippet: "    sizeMode: fit_text\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeLabel:  true,
				gameui.NodeButton: true,
			},
		},
		{
			field:   "padding",
			snippet: "    padding:\n      left: 6\n      right: 6\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeVBox:   true,
				gameui.NodeHBox:   true,
				gameui.NodeLabel:  true,
				gameui.NodeButton: true,
			},
		},
		{
			field:   "spacing",
			snippet: "    spacing: 4\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeVBox:      true,
				gameui.NodeHBox:      true,
				gameui.NodeSpriteRow: true,
			},
		},
		{
			field:   "textKey",
			snippet: "    textKey: ui.ok\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeLabel:       true,
				gameui.NodeButton:      true,
				gameui.NodeTouchButton: true,
			},
		},
		{
			field:   "fontSize",
			snippet: "    fontSize: 14\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeLabel:       true,
				gameui.NodeButton:      true,
				gameui.NodeTouchButton: true,
			},
		},
		{
			field:   "textColor",
			snippet: "    textColor: '#ffffff'\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeLabel:       true,
				gameui.NodeButton:      true,
				gameui.NodeTouchButton: true,
				gameui.NodeTouchStick:  true,
			},
		},
		{
			field:   "cornerRadius",
			snippet: "    cornerRadius: 6\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeVBox:      true,
				gameui.NodeHBox:      true,
				gameui.NodeContainer: true,
				gameui.NodeLabel:     true,
				gameui.NodeButton:    true,
				gameui.NodeColorRect: true,
			},
		},
		{
			field:   "focusOrder",
			snippet: "    focusOrder: 1\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeButton:      true,
				gameui.NodeTouchButton: true,
			},
		},
		{
			field:   "touchHitSize",
			snippet: "    touchHitSize: 48\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeButton:      true,
				gameui.NodeTouchStick:  true,
				gameui.NodeTouchButton: true,
			},
		},
		{
			field:   "sprite",
			snippet: "    sprite: art/ui/heart.png\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeSpriteRow: true,
			},
		},
		{
			field:   "spriteFrame",
			snippet: "    sprite: art/ui/atlas.json\n    spriteFrame: heart\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeSpriteRow: true,
			},
		},
		{
			field:   "spriteCount",
			snippet: "    spriteCount: 3\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeSpriteRow: true,
			},
		},
		{
			field:   "under",
			snippet: "    under: art/ui/bar_under.png\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeImageProgress: true,
			},
		},
		{
			field:   "progress",
			snippet: "    progress: art/ui/bar_fill.png\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeImageProgress: true,
			},
		},
		{
			field:   "over",
			snippet: "    over: art/ui/bar_over.png\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeImageProgress: true,
			},
		},
		{
			field:   "fill",
			snippet: "    fill: clockwise\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeImageProgress: true,
			},
		},
		{
			field:   "startAngle",
			snippet: "    startAngle: -90\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeImageProgress: true,
			},
		},
		{
			field:   "span",
			snippet: "    span: 180\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeImageProgress: true,
			},
		},
		{
			field:   "min",
			snippet: "    min: 10\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeImageProgress: true,
			},
		},
		{
			field:   "max",
			snippet: "    max: 100\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeImageProgress: true,
			},
		},
		{
			field:   "value",
			snippet: "    value: 40\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeImageProgress: true,
			},
		},
		{
			field:   "children",
			snippet: "    children:\n      - id: child\n        type: color_rect\n",
			allowed: map[gameui.ScreenNodeType]bool{
				gameui.NodeVBox:      true,
				gameui.NodeHBox:      true,
				gameui.NodeContainer: true,
			},
		},
	}

	for _, nt := range allTypes {
		for _, fc := range fields {
			t.Run(string(nt)+"/"+fc.field, func(t *testing.T) {
				src := "version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: n\n    type: " + string(nt) + "\n" + fc.snippet
				doc, err := gameui.ParseScreen([]byte(src))
				if fc.allowed[nt] {
					if err != nil {
						t.Fatalf("ParseScreen rejected allowed field %s on %s: %v", fc.field, nt, err)
					}
					encoded, err := gameui.EncodeScreen(doc)
					if err != nil {
						t.Fatalf("EncodeScreen failed for %s/%s: %v", nt, fc.field, err)
					}
					if _, err := gameui.ParseScreen(encoded); err != nil {
						t.Fatalf("ParseScreen round-trip failed for %s/%s: %v", nt, fc.field, err)
					}
					return
				}

				if err == nil {
					t.Fatalf("ParseScreen accepted unsupported field %s on %s", fc.field, nt)
				}
				le, ok := loaderr.As(err)
				if !ok {
					t.Fatalf("error %T is not *loaderr.Error: %v", err, err)
				}
				wantField := fc.field
				if fc.field == "spriteFrame" {
					// Snippet also sets sprite before spriteFrame, so sprite is caught first.
					wantField = "sprite"
				}
				if le.Field != "nodes[0]."+wantField {
					t.Errorf("Field = %q, want %q (err=%v)", le.Field, "nodes[0]."+wantField, err)
				}
				if le.Line <= 0 {
					t.Errorf("expected positive Line for %s/%s: %+v", nt, fc.field, le)
				}
				if le.Fix == "" {
					t.Errorf("expected non-empty Fix hint for %s/%s", nt, fc.field)
				}
			})
		}
	}

	// Also verify spriteFrame alone (without sprite) is refused on non-sprite_row nodes as unsupported.
	_, err := gameui.ParseScreen([]byte("version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: n\n    type: label\n    spriteFrame: heart\n"))
	if err == nil || !strings.Contains(err.Error(), "does not support spriteFrame") {
		t.Fatalf("expected unsupported spriteFrame error on label, got %v", err)
	}
}

func TestScreenAllAllowedStylingFieldsAppliedOnBuild(t *testing.T) {
	// Build a screen exercising every allowed styling/behavioral field on each of the 9 node types
	// and assert BuildScreen applies each field to the resulting widget.
	src := `version: 1
name: all_types
designWidth: 640
designHeight: 360
nodes:
  - id: v
    type: vbox
    anchor: top_left
    width: 140
    height: 90
    padding:
      top: 4
      right: 6
      bottom: 8
      left: 10
    spacing: 5
    background: '#112233'
    cornerRadius: 6
    children:
      - id: h
        type: hbox
        width: 120
        height: 30
        padding:
          top: 2
          right: 3
          bottom: 4
          left: 5
        spacing: 7
        background: '#223344'
        cornerRadius: 4
        children:
          - id: sr
            type: sprite_row
            width: 60
            height: 16
            spacing: 3
            sprite: art/ui/heart.png
            spriteFrame: full
            spriteCount: 4
            background: '#ff3344'
      - id: lbl
        type: label
        sizeMode: fit_text
        height: 20
        padding:
          left: 4
          right: 6
        textKey: ui.lbl
        fontSize: 15
        textColor: '#eeeeee'
        background: '#334455'
        cornerRadius: 3
  - id: cont
    type: container
    anchor: top_right
    width: 160
    height: 120
    background: '#101010'
    cornerRadius: 9
    children:
      - id: rect
        type: color_rect
        anchor: top_left
        width: 40
        height: 20
        background: '#445566'
        cornerRadius: 5
      - id: btn
        type: button
        anchor: bottom_left
        sizeMode: fit_text
        height: 28
        padding:
          top: 2
          right: 8
          bottom: 2
          left: 8
        textKey: ui.btn
        focusOrder: 1
        touchHitSize: 48
        fontSize: 13
        textColor: '#ffffff'
        background: '#243044'
        cornerRadius: 7
  - id: stick
    type: touch_stick
    anchor: bottom_left
    width: 72
    height: 72
    touchHitSize: 96
    textColor: '#aabbcc'
    background: '#112244'
  - id: tbtn
    type: touch_button
    anchor: bottom_right
    width: 52
    height: 52
    textKey: ui.jump
    focusOrder: 2
    touchHitSize: 64
    fontSize: 11
    textColor: '#ddeeff'
    background: '#334466'
  - id: hp_bar
    type: image_progress
    anchor: top_center
    width: 180
    height: 20
    under: art/ui/bar_under.png
    progress: art/ui/bar_fill.png
    over: art/ui/bar_over.png
    fill: clockwise
    startAngle: -90
    span: 270
    min: 5
    max: 50
    value: 25
`
	doc, err := gameui.ParseScreen([]byte(src))
	if err != nil {
		t.Fatalf("ParseScreen: %v", err)
	}
	built, err := gameui.BuildScreen(doc, gameui.ScreenBuildOptions{
		Text: func(key string) string { return "T:" + key },
		Sprites: func(path, frame string) (core.ImageSource, core.Bounds, bool) {
			return core.Src("asset:" + path), core.Bounds{Size: core.Size{Width: 32, Height: 32}}, true
		},
	})
	if err != nil {
		t.Fatalf("BuildScreen: %v", err)
	}
	built.Layout(core.Bounds{Size: core.Size{Width: 640, Height: 360}})

	vbox := built.Widget("v").(*gameui.VBox)
	if vbox.Spacing != 5 || vbox.Padding != (core.Edges{Top: 4, Right: 6, Bottom: 8, Left: 10}) || vbox.DrawBackground == nil {
		t.Errorf("vbox fields not applied: spacing=%v padding=%+v hasBg=%v", vbox.Spacing, vbox.Padding, vbox.DrawBackground != nil)
	}

	hbox := built.Widget("h").(*gameui.HBox)
	if hbox.Spacing != 7 || hbox.Padding != (core.Edges{Top: 2, Right: 3, Bottom: 4, Left: 5}) || hbox.DrawBackground == nil {
		t.Errorf("hbox fields not applied: spacing=%v padding=%+v hasBg=%v", hbox.Spacing, hbox.Padding, hbox.DrawBackground != nil)
	}

	sr := built.Widget("sr").(*gameui.SpriteRow)
	if sr.Spacing != 3 || sr.SpritePath != "art/ui/heart.png" || sr.SpriteFrame != "full" || sr.Count != 4 || sr.Color == 0 {
		t.Errorf("sprite_row fields not applied: %+v", sr)
	}

	lbl := built.Widget("lbl").(*gameui.Label)
	if lbl.Text != "T:ui.lbl" || lbl.Font.Size != 15 || lbl.Color == 0 || lbl.Background == 0 || lbl.CornerRadius != 3 || lbl.Padding.Left != 4 || lbl.Padding.Right != 6 {
		t.Errorf("label fields not applied: %+v", lbl)
	}

	cont := built.Widget("cont").(*gameui.ScreenCanvas)
	if cont.Background == 0 || cont.CornerRadius != 9 {
		t.Errorf("container fields not applied: bg=%v cr=%v", cont.Background, cont.CornerRadius)
	}

	rect := built.Widget("rect").(*gameui.ColorRect)
	if rect.Color == 0 || rect.CornerRadius != 5 {
		t.Errorf("color_rect fields not applied: color=%v cr=%v", rect.Color, rect.CornerRadius)
	}

	btn := built.Widget("btn").(*gameui.Button)
	if btn.Text != "T:ui.btn" || btn.Font.Size != 13 || btn.TextColor == 0 || btn.Background == 0 || btn.CornerRadius != 7 || btn.Padding.Left != 8 || btn.FocusOrder != 1 || btn.TouchHitSize != 48 {
		t.Errorf("button fields not applied: %+v", btn)
	}

	stick := built.Widget("stick").(*gameui.TouchStick)
	if stick.HitSize != 96 || stick.BaseColor == 0 || stick.KnobColor == 0 {
		t.Errorf("touch_stick fields not applied: %+v", stick)
	}

	tbtn := built.Widget("tbtn").(*gameui.TouchButton)
	if tbtn.Text != "T:ui.jump" || tbtn.Font.Size != 11 || tbtn.TextColor == 0 || tbtn.Background == 0 || tbtn.FocusOrder != 2 || tbtn.HitSize != 64 {
		t.Errorf("touch_button fields not applied: %+v", tbtn)
	}

	hpBar := built.Widget("hp_bar").(*gameui.ImageProgress)
	if hpBar.Under == nil || hpBar.Progress == nil || hpBar.Over == nil || hpBar.Fill != gameui.FillClockwise || hpBar.StartAngle != -90 || hpBar.Span != 270 || hpBar.Min != 5 || hpBar.Max != 50 || hpBar.Value != 25 {
		t.Errorf("image_progress fields not applied: %+v", hpBar)
	}

	if len(built.FocusOrder) != 2 || built.FocusOrder[0] != btn || built.FocusOrder[1] != tbtn {
		t.Errorf("FocusOrder = %+v, want [btn, tbtn]", built.FocusOrder)
	}
}
