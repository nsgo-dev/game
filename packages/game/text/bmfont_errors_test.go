package text_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/text"
)

func TestLoadBMFontErrorsNameFileLineAndFix(t *testing.T) {
	const head = "info face=\"Pixel\" size=16\ncommon lineHeight=16 base=12 pages=1\npage id=0 file=\"pixel.png\"\nchars count=2\n"
	cases := []struct {
		name, data string
		line       int
	}{
		{"bad char value", head + "char id=65 x=0 y=0 width=8 height=8 xadvance=8 page=0\nchar id=66 x=zero y=0 width=8 height=8 xadvance=8 page=0\n", 6},
		{"unknown page", head + "char id=65 x=0 y=0 width=8 height=8 xadvance=8 page=3\n", 5},
		{"duplicate char", head + "char id=65 x=0 y=0 width=8 height=8 xadvance=8 page=0\nchar id=65 x=8 y=0 width=8 height=8 xadvance=8 page=0\n", 6},
		{"unknown record", head + "glyph id=1\n", 5},
		{"leading blank lines", "\n\n" + head + "char id=65 x=-1 y=0 width=8 height=8 xadvance=8 page=0\n", 7},
		{"line height", "common lineHeight=0 base=12 pages=1\npage id=0 file=\"a.png\"\nchar id=65 x=0 y=0 width=8 height=8 xadvance=8 page=0\n", 1},
		{"xml char", "<font>\n  <common lineHeight=\"16\" pages=\"1\"/>\n  <pages><page id=\"0\" file=\"a.png\"/></pages>\n  <chars>\n    <char id=\"65\" x=\"0\" y=\"0\" width=\"8\" height=\"8\" xadvance=\"8\" page=\"0\"/>\n    <char id=\"66\" x=\"0\" y=\"0\" width=\"8\" height=\"8\" xadvance=\"8\" page=\"4\"/>\n  </chars>\n</font>", 6},
		{"xml syntax", "<font>\n  <common lineHeight=\"16\" pages=\"1\"/>\n  <pages><page id=\"0\" file=\"a.png\"></pages>\n</font>", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := text.LoadBMFont([]byte(tc.data), nil)
			loaderrtest.Expect(t, loaderr.WithFile(err, "pixel.fnt"), "pixel.fnt", tc.line)
		})
	}
}
