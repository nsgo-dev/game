package gen

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/importsettings"
)

// The font's glyphs are drawn on a 5 by 8 grid: rows 0 to 6 hold capitals and digits, row 6 is the baseline's row, and row 7 holds descenders. Each glyph sits in a cell of the page with a pixel of space to its right and below, so glyphs never bleed into each other.
const (
	glyphW     = 5
	glyphH     = 8
	fontCellW  = glyphW + 1
	fontCellH  = glyphH + 1
	fontCols   = 16
	fontFirst  = ' '
	fontLast   = '~'
	fontBase   = 7
	fontLine   = 10
	fontSpaceW = 3
)

// glyphs are the font's characters, printable ASCII, drawn for this pack: each row of '#' and '.' is one row of pixels, top to bottom.
var glyphs = map[rune]string{
	' ':  "..... ..... ..... ..... ..... ..... .....",
	'!':  "..#.. ..#.. ..#.. ..#.. ..#.. ..... ..#..",
	'"':  ".#.#. .#.#. ..... ..... ..... ..... .....",
	'#':  ".#.#. .#.#. ##### .#.#. ##### .#.#. .#.#.",
	'$':  "..#.. .#### #.#.. .###. ..#.# ####. ..#..",
	'%':  "##..# ##.#. ...#. ..#.. .#... .#.## #..##",
	'&':  ".##.. #..#. #.#.. .#... #.#.# #..#. .##.#",
	'\'': "..#.. ..#.. ..... ..... ..... ..... .....",
	'(':  "...#. ..#.. .#... .#... .#... ..#.. ...#.",
	')':  ".#... ..#.. ...#. ...#. ...#. ..#.. .#...",
	'*':  "..... ..#.. #.#.# .###. #.#.# ..#.. .....",
	'+':  "..... ..#.. ..#.. ##### ..#.. ..#.. .....",
	',':  "..... ..... ..... ..... ..... ..#.. ..#.. .#...",
	'-':  "..... ..... ..... ##### ..... ..... .....",
	'.':  "..... ..... ..... ..... ..... ..... ..#..",
	'/':  "....# ...#. ...#. ..#.. .#... .#... #....",
	'0':  ".###. #...# #..## #.#.# ##..# #...# .###.",
	'1':  "..#.. .##.. ..#.. ..#.. ..#.. ..#.. .###.",
	'2':  ".###. #...# ....# ...#. ..#.. .#... #####",
	'3':  "####. ....# ....# .###. ....# ....# ####.",
	'4':  "...#. ..##. .#.#. #..#. ##### ...#. ...#.",
	'5':  "##### #.... ####. ....# ....# #...# .###.",
	'6':  "..##. .#... #.... ####. #...# #...# .###.",
	'7':  "##### ....# ...#. ..#.. .#... .#... .#...",
	'8':  ".###. #...# #...# .###. #...# #...# .###.",
	'9':  ".###. #...# #...# .#### ....# ...#. .##..",
	':':  "..... ..#.. ..... ..... ..... ..#.. .....",
	';':  "..... ..#.. ..... ..... ..... ..#.. ..#.. .#...",
	'<':  "...#. ..#.. .#... #.... .#... ..#.. ...#.",
	'=':  "..... ..... ##### ..... ##### ..... .....",
	'>':  ".#... ..#.. ...#. ....# ...#. ..#.. .#...",
	'?':  ".###. #...# ....# ...#. ..#.. ..... ..#..",
	'@':  ".###. #...# #.### #.#.# #.### #.... .###.",
	'A':  ".###. #...# #...# ##### #...# #...# #...#",
	'B':  "####. #...# #...# ####. #...# #...# ####.",
	'C':  ".###. #...# #.... #.... #.... #...# .###.",
	'D':  "####. #...# #...# #...# #...# #...# ####.",
	'E':  "##### #.... #.... ####. #.... #.... #####",
	'F':  "##### #.... #.... ####. #.... #.... #....",
	'G':  ".###. #...# #.... #.### #...# #...# .####",
	'H':  "#...# #...# #...# ##### #...# #...# #...#",
	'I':  ".###. ..#.. ..#.. ..#.. ..#.. ..#.. .###.",
	'J':  "..### ...#. ...#. ...#. ...#. #..#. .##..",
	'K':  "#...# #..#. #.#.. ##... #.#.. #..#. #...#",
	'L':  "#.... #.... #.... #.... #.... #.... #####",
	'M':  "#...# ##.## #.#.# #.#.# #...# #...# #...#",
	'N':  "#...# ##..# #.#.# #..## #...# #...# #...#",
	'O':  ".###. #...# #...# #...# #...# #...# .###.",
	'P':  "####. #...# #...# ####. #.... #.... #....",
	'Q':  ".###. #...# #...# #...# #.#.# #..#. .##.#",
	'R':  "####. #...# #...# ####. #.#.. #..#. #...#",
	'S':  ".#### #.... #.... .###. ....# ....# ####.",
	'T':  "##### ..#.. ..#.. ..#.. ..#.. ..#.. ..#..",
	'U':  "#...# #...# #...# #...# #...# #...# .###.",
	'V':  "#...# #...# #...# #...# #...# .#.#. ..#..",
	'W':  "#...# #...# #...# #.#.# #.#.# ##.## #...#",
	'X':  "#...# #...# .#.#. ..#.. .#.#. #...# #...#",
	'Y':  "#...# #...# .#.#. ..#.. ..#.. ..#.. ..#..",
	'Z':  "##### ....# ...#. ..#.. .#... #.... #####",
	'[':  ".###. .#... .#... .#... .#... .#... .###.",
	'\\': "#.... .#... .#... ..#.. ...#. ...#. ....#",
	']':  ".###. ...#. ...#. ...#. ...#. ...#. .###.",
	'^':  "..#.. .#.#. #...# ..... ..... ..... .....",
	'_':  "..... ..... ..... ..... ..... ..... ..... #####",
	'`':  ".#... ..#.. ..... ..... ..... ..... .....",
	'a':  "..... ..... .###. ....# .#### #...# .####",
	'b':  "#.... #.... ####. #...# #...# #...# ####.",
	'c':  "..... ..... .###. #.... #.... #...# .###.",
	'd':  "....# ....# .#### #...# #...# #...# .####",
	'e':  "..... ..... .###. #...# ##### #.... .###.",
	'f':  "..##. .#... ###.. .#... .#... .#... .#...",
	'g':  "..... ..... .#### #...# #...# .#### ....# .###.",
	'h':  "#.... #.... ####. #...# #...# #...# #...#",
	'i':  "..#.. ..... .##.. ..#.. ..#.. ..#.. .###.",
	'j':  "...#. ..... ..##. ...#. ...#. ...#. #..#. .##..",
	'k':  "#.... #.... #..#. #.#.. ##... #.#.. #..#.",
	'l':  ".##.. ..#.. ..#.. ..#.. ..#.. ..#.. .###.",
	'm':  "..... ..... ##.#. #.#.# #.#.# #.#.# #.#.#",
	'n':  "..... ..... ####. #...# #...# #...# #...#",
	'o':  "..... ..... .###. #...# #...# #...# .###.",
	'p':  "..... ..... ####. #...# #...# ####. #.... #....",
	'q':  "..... ..... .#### #...# #...# .#### ....# ....#",
	'r':  "..... ..... #.##. ##..# #.... #.... #....",
	's':  "..... ..... .###. #.... .###. ....# ####.",
	't':  ".#... .#... ###.. .#... .#... .#..# ..##.",
	'u':  "..... ..... #...# #...# #...# #..## .##.#",
	'v':  "..... ..... #...# #...# #...# .#.#. ..#..",
	'w':  "..... ..... #...# #...# #.#.# #.#.# .#.#.",
	'x':  "..... ..... #...# .#.#. ..#.. .#.#. #...#",
	'y':  "..... ..... #...# #...# #...# .#### ....# .###.",
	'z':  "..... ..... ##### ...#. ..#.. .#... #####",
	'{':  "...#. ..#.. ..#.. .#... ..#.. ..#.. ...#.",
	'|':  "..#.. ..#.. ..#.. ..#.. ..#.. ..#.. ..#..",
	'}':  ".#... ..#.. ..#.. ...#. ..#.. ..#.. .#...",
	'~':  "..... ..... .#... #.#.# ...#. ..... .....",
}

// glyphRows parses a glyph's rows, checking their size.
func glyphRows(r rune) ([]string, error) {
	src, ok := glyphs[r]
	if !ok {
		return nil, fmt.Errorf("no glyph for %q", r)
	}
	rows := strings.Fields(src)
	if len(rows) < glyphH-1 || len(rows) > glyphH {
		return nil, fmt.Errorf("glyph %q has %d rows, want %d or %d", r, len(rows), glyphH-1, glyphH)
	}
	for _, row := range rows {
		if len(row) != glyphW || strings.Trim(row, ".#") != "" {
			return nil, fmt.Errorf("glyph %q has row %q, want %d of '.' and '#'", r, row, glyphW)
		}
	}
	return rows, nil
}

// addFont draws the font's page and writes it with its AngelCode BMFont text descriptor and the page's sidecar. Each character's rectangle is trimmed to its inked columns, so the font is proportional: a character advances by its width plus one pixel.
func addFont(files map[string][]byte) error {
	count := int(fontLast - fontFirst + 1)
	rowsN := (count + fontCols - 1) / fontCols
	c := newCanvas(fontCols*fontCellW, rowsN*fontCellH)
	var fnt strings.Builder
	fmt.Fprintf(&fnt, "info face=\"Placeholder Pixel\" size=%d bold=0 italic=0 charset=\"\" unicode=1 stretchH=100 smooth=0 aa=1 padding=0,0,0,0 spacing=1,1 outline=0\n", glyphH)
	fmt.Fprintf(&fnt, "common lineHeight=%d base=%d scaleW=%d scaleH=%d pages=1 packed=0 alphaChnl=0 redChnl=4 greenChnl=4 blueChnl=4\n", fontLine, fontBase, c.img.Rect.Dx(), c.img.Rect.Dy())
	fnt.WriteString("page id=0 file=\"pixel.png\"\n")
	fmt.Fprintf(&fnt, "chars count=%d\n", count)
	for r := fontFirst; r <= fontLast; r++ {
		rows, err := glyphRows(r)
		if err != nil {
			return err
		}
		i := int(r - fontFirst)
		cx, cy := i%fontCols*fontCellW, i/fontCols*fontCellH
		minCol, maxCol := glyphW, -1
		for y, row := range rows {
			for x, ch := range row {
				if ch != '#' {
					continue
				}
				c.set(cx+x, cy+y, white)
				minCol, maxCol = min(minCol, x), max(maxCol, x)
			}
		}
		x, w := cx+minCol, maxCol-minCol+1
		if maxCol < 0 {
			x, w = cx, fontSpaceW
		}
		fmt.Fprintf(&fnt, "char id=%d x=%d y=%d width=%d height=%d xoffset=0 yoffset=0 xadvance=%d page=0 chnl=15\n", r, x, cy, w, glyphH, w+1)
	}
	img, err := encodePNG(c.img)
	if err != nil {
		return err
	}
	files["fonts/pixel.png"] = img
	files["fonts/pixel.fnt"] = []byte(fnt.String())
	return addSidecar(files, "fonts/pixel.png", &importsettings.File{Image: &importsettings.Image{Filter: "nearest"}})
}
