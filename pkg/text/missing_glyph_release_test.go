//go:build release

package text_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/text"
)

func TestMissingGlyphReleaseBuildIsNoOp(t *testing.T) {
	called := false
	text.SetMissingGlyphLogger(func(string, rune) {
		called = true
	})
	text.SetLogMissingGlyphs(true)
	if text.LogMissingGlyphsEnabled() {
		t.Fatal("LogMissingGlyphsEnabled() = true in release build, want false")
	}
	f := &text.Font{
		Face:       "HUD",
		Size:       16,
		LineHeight: 16,
		Glyphs:     map[rune]text.Glyph{'A': {ID: 'A', XAdvance: 8}},
	}
	f.Measure("A龙", text.Options{})
	f.Layout("A龙", 0, 0, text.Options{})
	if called {
		t.Fatal("missing glyph logger was called in release build")
	}
}
