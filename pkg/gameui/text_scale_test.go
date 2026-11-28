package gameui

import (
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

// fontSizeMeasurer reports text as one point wide per font point, so a measured width equals the font size the widget resolved.
func fontSizeMeasurer(_ string, font core.Font) (float32, float32) {
	return float32(font.Size), float32(font.Size)
}

func TestClampTextScale(t *testing.T) {
	for _, tc := range []struct{ in, want float32 }{
		{0, 1}, {-3, 1}, {0.5, 1}, {1, 1}, {1.5, 1.5}, {2, 2}, {3, 2},
		{float32(math.NaN()), 1}, {float32(math.Inf(1)), 2},
	} {
		if got := clampTextScale(tc.in); got != tc.want {
			t.Errorf("clampTextScale(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRootTextScaleScalesDefaultAndExplicitFontsOnce(t *testing.T) {
	theme := DefaultTheme()
	theme.Font.Size = 10
	theme.TextMeasurer = fontSizeMeasurer
	plain := &Label{Text: "default"}
	explicit := &Label{Text: "explicit", Font: core.Font{Size: 16}}
	r := NewRoot(&VBox{Children: []Widget{plain, explicit}})
	r.Theme = &theme

	if got := r.SetTextScale(3); got != 2 || r.TextScale != 2 {
		t.Fatalf("SetTextScale(3) = %v, root = %v; want 2", got, r.TextScale)
	}
	r.Layout(core.Bounds{Size: core.Size{Width: 400, Height: 400}})
	if got := plain.Bounds().Size.Height; got != 20 {
		t.Errorf("default-font label height = %v, want 20 (10 pt theme font at 200%%)", got)
	}
	if got := explicit.Bounds().Size.Height; got != 32 {
		t.Errorf("explicit-font label height = %v, want 32 (16 pt font at 200%%)", got)
	}
}

func TestThemeTextScaleAppliesWhenRootLeavesItUnset(t *testing.T) {
	theme := DefaultTheme()
	theme.Font.Size = 10
	theme.TextScale = 1.5
	theme.TextMeasurer = fontSizeMeasurer
	l := &Label{Text: "theme"}
	r := NewRoot(&VBox{Children: []Widget{l}})
	r.Theme = &theme
	r.Layout(core.Bounds{Size: core.Size{Width: 400, Height: 400}})
	if got := l.Bounds().Size.Height; got != 15 {
		t.Fatalf("label height = %v, want 15 (10 pt at the theme's 150%%)", got)
	}

	r.TextScale = 5 // set directly, bypassing SetTextScale
	r.Layout(core.Bounds{Size: core.Size{Width: 400, Height: 400}})
	if got := l.Bounds().Size.Height; got != 20 {
		t.Fatalf("label height = %v, want 20 (Root.TextScale clamped to 200%%)", got)
	}
}

func TestCheckScreenTextScaleFindsOverflowOnlyAtLargerScale(t *testing.T) {
	// "ABCDEFGH" at fontSize 14 measures 56 pt, 84 pt at 150% and 112 pt at 200%, against a 100 pt box.
	src := `version: 1
name: hud
designWidth: 640
designHeight: 360
nodes:
  - id: title
    type: label
    anchor: top_left
    width: 100
    height: 40
    fontSize: 14
    textKey: hud.title
    textColor: '#ffffff'
    background: '#000000'
`
	doc, err := ParseScreen([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	overflows := func(scale float32) []ScreenIssue {
		var out []ScreenIssue
		for _, iss := range CheckScreen(doc, ScreenCheckOptions{
			TextScale: scale,
			Locales:   map[string]map[string]string{"en": {"hud.title": "ABCDEFGH"}},
		}) {
			if iss.Code == IssueTextOverflow {
				out = append(out, iss)
			}
		}
		return out
	}

	for _, scale := range []float32{0, 1, 1.5} {
		if got := overflows(scale); len(got) != 0 {
			t.Fatalf("TextScale %v: unexpected overflow %+v", scale, got)
		}
	}
	got := overflows(2)
	if len(got) != 1 || got[0].NodeID != "title" {
		t.Fatalf("TextScale 2: want one overflow for title, got %+v", got)
	}
	if !strings.Contains(got[0].Message, "at text scale 200%") || !strings.Contains(got[0].Message, "needs 112 pt") {
		t.Fatalf("TextScale 2: message %q does not name the scale and scaled width", got[0].Message)
	}
	if got := overflows(9); len(got) != 1 || !strings.Contains(got[0].Message, "at text scale 200%") {
		t.Fatalf("TextScale 9: want the scale clamped to 200%%, got %+v", got)
	}
}
