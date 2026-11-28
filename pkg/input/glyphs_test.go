package input

import "testing"

func TestGlyphSetRoundTripAndStrictFields(t *testing.T) {
	raw := []byte("version: 1\ndevices:\n  keyboard:\n    Space:\n      label: '[Space]'\n      glyph: Space\n      icon: key_space\n")
	g, err := ParseGlyphSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := EncodeGlyphSet(g)
	if err != nil || len(out) == 0 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	if _, err := ParseGlyphSet([]byte("version: 1\ndevices:\n  keyboard:\n    Space:\n      typo: x\n")); err == nil {
		t.Fatal("unknown field accepted")
	}
}
