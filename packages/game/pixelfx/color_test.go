package pixelfx_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

func TestColorsMatchPixelfx(t *testing.T) {
	u := func(x int32) uint32 { return uint32(x) }
	for _, c := range []struct {
		name      string
		got, want uint32
	}{
		{"Hex", pixelfx.Hex("#f53cd1"), u(-3064587)},
		{"Mix", pixelfx.Mix(pixelfx.Hex("#102030"), pixelfx.Hex("#f0e0d0"), 0.37), u(-9738398)},
		{"Scale", pixelfx.Scale(pixelfx.Hex("#808080"), 1.7), u(-2500135)},
		{"RGB wraps and truncates", pixelfx.RGB(-1, 256.7, 12.9), u(-15990529)},
		{"short hex", pixelfx.Hex("#fa0"), pixelfx.Hex("#ffaa00")},
	} {
		if c.got != c.want {
			t.Errorf("%s = %#08x, want %#08x", c.name, c.got, c.want)
		}
	}
	got := pixelfx.Fit(pixelfx.Ramp("#000000", "#ffffff", "#ff0000"), 5)
	want := []string{"#000000", "#ffffff", "#ffffff", "#ff0000", "#ff0000"}
	for i := range want {
		if pixelfx.ToHex(got[i]) != want[i] {
			t.Errorf("Fit step %d is %s, want %s", i, pixelfx.ToHex(got[i]), want[i])
		}
	}
	for _, bad := range []string{"", "#12", "#zzzzzz", "red"} {
		if _, err := pixelfx.ParseHex(bad); err == nil {
			t.Errorf("ParseHex(%q) succeeded", bad)
		}
	}
}

func TestEveryPaletteParses(t *testing.T) {
	for name, hexes := range pixelfx.Pal {
		if len(pixelfx.Ramps[name]) != len(hexes) || len(hexes) < 6 {
			t.Errorf("palette %s has %d steps", name, len(hexes))
		}
	}
}

func TestFontWidths(t *testing.T) {
	if got := pixelfx.TextWidth("LEVEL UP!", "big"); got != 47 {
		t.Errorf("big LEVEL UP! is %d wide, want 47", got)
	}
	if got := pixelfx.TextWidth("x2 ?", "mini"); got != 14 {
		t.Errorf("mini x2 ? is %d wide, want 14", got)
	}
}
