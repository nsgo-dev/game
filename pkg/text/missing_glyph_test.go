//go:build !release

package text_test

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/text"
	"github.com/nsgo-dev/nsgo/core"
)

func makeSimpleFont(face string, runes string) *text.Font {
	f := &text.Font{
		Face:       face,
		Size:       16,
		LineHeight: 16,
		Base:       12,
		Pages:      map[int]*sprite.Atlas{0: {Frames: map[string]sprite.Frame{}}},
		Glyphs:     make(map[rune]text.Glyph),
	}
	for _, r := range runes {
		fr := sprite.Frame{Name: string(r), W: 8, H: 12, Width: 8, Height: 12}
		f.Pages[0].Frames[fr.Name] = fr
		f.Glyphs[r] = text.Glyph{ID: r, Frame: fr, XAdvance: 8, Page: 0}
	}
	return f
}

func TestMissingGlyphDebugLoggingOncePerRuneAndFont(t *testing.T) {
	prevEnabled := text.LogMissingGlyphsEnabled()
	t.Cleanup(func() {
		text.SetLogMissingGlyphs(prevEnabled)
		text.SetMissingGlyphLogger(nil)
	})

	type entry struct {
		face string
		r    rune
	}
	var logged []entry
	text.SetMissingGlyphLogger(func(face string, r rune) {
		logged = append(logged, entry{face: face, r: r})
	})

	f1 := makeSimpleFont("HUD", "AB ")
	f2 := makeSimpleFont("Dialogue", "AB ")

	// Disabled by default: nothing logged.
	text.SetLogMissingGlyphs(false)
	if text.LogMissingGlyphsEnabled() {
		t.Fatal("LogMissingGlyphsEnabled() = true when disabled")
	}
	f1.Measure("A龙B龙", text.Options{})
	f1.Layout("A龙B龙", 0, 0, text.Options{})
	if len(logged) != 0 {
		t.Fatalf("logged %v while disabled, want none", logged)
	}

	// Enabled: each missing rune logged once per font across Draw, Layout, Measure, and Rich layout.
	text.SetLogMissingGlyphs(true)
	if !text.LogMissingGlyphsEnabled() {
		t.Fatal("LogMissingGlyphsEnabled() = false when enabled")
	}
	d := &core.Drawing{}
	f1.Draw(d, "A龙B龙", 0, 0, text.Options{})
	f1.Measure("龙凤龙", text.Options{})
	f1.Layout("凤龙", 0, 0, text.Options{})
	text.LayoutRich(f1, text.ParseRich("[b]龙凤虎[/b]"), text.RichOptions{})
	// Second font logs its own missing runes independently.
	f2.Draw(d, "A龙龙", 0, 0, text.Options{})

	want := []entry{
		{"HUD", '龙'},
		{"HUD", '凤'},
		{"HUD", '虎'},
		{"Dialogue", '龙'},
	}
	if !slices.Equal(logged, want) {
		t.Fatalf("logged = %v, want %v", logged, want)
	}
}

func TestSDFFontMissingGlyphLoggingOncePerRuneAndFont(t *testing.T) {
	prevEnabled := text.LogMissingGlyphsEnabled()
	t.Cleanup(func() {
		text.SetLogMissingGlyphs(prevEnabled)
		text.SetMissingGlyphLogger(nil)
	})

	var logged []rune
	text.SetMissingGlyphLogger(func(_ string, r rune) {
		logged = append(logged, r)
	})
	text.SetLogMissingGlyphs(true)

	sdf := &text.SDFFont{
		Face:       "Title",
		Size:       16,
		LineHeight: 20,
		Base:       16,
		Glyphs: map[rune]text.Glyph{
			'A': {ID: 'A', XAdvance: 10},
		},
	}
	sdf.Measure("A龙龙", text.SDFOptions{})
	sdf.Layout("A龙凤", 0, 0, text.SDFOptions{})
	sdf.Measure("凤龙", text.SDFOptions{})

	want := []rune{'龙', '凤'}
	if !slices.Equal(logged, want) {
		t.Fatalf("SDF logged = %q, want %q", logged, want)
	}
}

func TestMissingRunes(t *testing.T) {
	f := makeSimpleFont("HUD", "AB ")
	got := f.MissingRunes("A龙\nB\t凤龙")
	want := []rune{'凤', '龙'}
	if !slices.Equal(got, want) {
		t.Fatalf("MissingRunes = %q, want %q", got, want)
	}
	if got := f.MissingRunes("A B\n"); len(got) != 0 {
		t.Fatalf("MissingRunes on covered string = %q, want empty", got)
	}
	sdf := &text.SDFFont{Glyphs: f.Glyphs}
	if got := sdf.MissingRunes("A龙"); !slices.Equal(got, []rune{'龙'}) {
		t.Fatalf("SDFFont.MissingRunes = %q, want ['龙']", got)
	}
}
