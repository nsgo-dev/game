package assetcheck_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
)

func TestCheckScreenReportsOverflowTouchMissingKeyAndContrast(t *testing.T) {
	hudScreen := `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/screen.v1.json
version: 1
name: hud
designWidth: 640
designHeight: 360
nodes:
  - id: floor_label
    type: label
    anchor: top_center
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
    textKey: hud.hint.lever
    textColor: '#ffffff'
    background: rgba(16,18,22,0.78)
  - id: only_en
    type: label
    anchor: top_left
    sizeMode: fit_text
    height: 16
    textKey: hud.only_en
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
  - id: hearts
    type: sprite_row
    anchor: top_left
    width: 52
    height: 14
    sprite: art/missing_heart.png
    spriteCount: 3
`
	enTable := `version: 1
locale: en
strings:
  hud.floor: Floor 1
  hud.hint.lever: Jump at levers to pull
  hud.only_en: English only
`
	zhTable := `version: 1
locale: zh
strings:
  hud.floor: 第一层 · 地窖
  hud.hint.lever: 靠近拉杆时按下跳跃键即可拉动
`
	fsys := fstest.MapFS{
		"game.yaml":               {Data: []byte("version: 1\nname: Test\nid: dev.nsgo.test\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n")},
		"screens/hud.screen.yaml": {Data: []byte(hudScreen)},
		"locales/en.yaml":         {Data: []byte(enTable)},
		"locales/zh.yaml":         {Data: []byte(zhTable)},
	}

	report, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.Screens != 1 {
		t.Fatalf("Screens = %d, want 1", report.Screens)
	}

	var lines []string
	for _, p := range report.Problems {
		lines = append(lines, p.String())
	}
	joined := strings.Join(lines, "\n")

	wants := []struct {
		desc string
		sub  string
	}{
		{"chinese overflow with file, line, node and locale", `screens/hud.screen.yaml:20: node "hint" (locale "zh"): text for key "hud.hint.lever" overflows 160 pt box`},
		{"touch area under 44 pt with file, line and node", `screens/hud.screen.yaml:42: node "attack": touch area is 36 pt (minimum 44 pt`},
		{"missing key in zh with file, line and node", `screens/hud.screen.yaml:33: node "only_en" (locale "zh"): textKey "hud.only_en" is missing from locale "zh"`},
		{"low contrast with file, line and node", `screens/hud.screen.yaml:13: node "floor_label": text contrast`},
		{"missing sprite file", `screens/hud.screen.yaml:51: sprite "art/missing_heart.png": not found`},
	}
	for _, w := range wants {
		if !strings.Contains(joined, w.sub) {
			t.Errorf("missing %s (%q) in problems:\n%s", w.desc, w.sub, joined)
		}
	}
}

func TestCheckScreenPassesWhenClean(t *testing.T) {
	hudScreen := `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/screen.v1.json
version: 1
name: hud
designWidth: 640
designHeight: 360
nodes:
  - id: hearts
    type: sprite_row
    anchor: top_left
    offset:
      x: 12
      "y": 12
    width: 52
    height: 14
    sprite: art/heart.png
    spriteCount: 3
  - id: hint
    type: label
    anchor: bottom_center
    offset:
      "y": 70
    sizeMode: fit_text
    height: 22
    padding:
      left: 6
      right: 6
    textKey: hud.hint.lever
    textColor: '#ffffff'
    background: '#101216'
  - id: jump
    type: touch_button
    anchor: bottom_right
    offset:
      x: 24
      "y": 28
    width: 52
    height: 52
    touchHitSize: 64
    deviceClasses:
      - phone
      - tablet
`
	fsys := fstest.MapFS{
		"screens/hud.screen.yaml": {Data: []byte(hudScreen)},
		"art/heart.png":           {Data: pngBytes(t, 16, 16)},
		"locales/en.yaml":         {Data: []byte("version: 1\nlocale: en\nstrings:\n  hud.hint.lever: Jump at levers to pull\n")},
		"locales/zh.yaml":         {Data: []byte("version: 1\nlocale: zh\nstrings:\n  hud.hint.lever: 靠近拉杆时按下跳跃键即可拉动\n")},
	}

	report, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.OK() {
		t.Fatalf("expected no problems, got:\n%v", report.Problems)
	}
	if !strings.Contains(report.Summary(), "1 screen") {
		t.Fatalf("Summary = %q, want 1 screen", report.Summary())
	}
}
