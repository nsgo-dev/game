package input_test

import (
	"github.com/nsgo-dev/game/pkg/input"
	"testing"
)

func TestGlyphSetRoundTrip(t *testing.T) {
	g, err := input.ParseGlyphSet([]byte("version: 1\ndevices:\n  keyboard:\n    KeyK: {label: '[K]', glyph: K}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if g.Devices["keyboard"]["KeyK"].Glyph != "K" {
		t.Fatal(g)
	}
	if _, err := input.EncodeGlyphSet(g); err != nil {
		t.Fatal(err)
	}
}
