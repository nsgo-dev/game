package main

import (
	"image"
	"image/color"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// The game's pictures are pixel art written as text below and painted into one atlas when the game starts, so the template needs no image files. Each picture is 8 by 8 pixels, one letter per pixel from palette ('.' is transparent), drawn at artScale. Replace newAtlas with sprite.LoadAtlas when you add real art; the frame names stay the same.

// artScale is how many game points one pixel of the art covers, so a tile is 8*artScale points wide.
const artScale = 2

// portraitScale is how many points one pixel of a portrait covers.
const portraitScale = 6

// palette maps the letters of the art to colors.
var palette = map[byte]color.NRGBA{
	'g': rgb(0x4f9a4a), 'G': rgb(0x3d7f3b),
	'd': rgb(0xc9a46a), 'D': rgb(0xa88352),
	'w': rgb(0x3b7dd8), 'W': rgb(0x8fc1f2),
	't': rgb(0x6b4a2b), 'L': rgb(0x2f6b34), 'l': rgb(0x46a04b),
	's': rgb(0x9a9aa6), 'S': rgb(0x6e6e7a),
	'r': rgb(0xb5483c), 'R': rgb(0x8a3229),
	'o': rgb(0x7a5230), 'k': rgb(0x1b1b24), 'f': rgb(0xb08654),
	'p': rgb(0xe86fa0), 'Y': rgb(0xf2d14b), 'b': rgb(0x9c7346),
	'e': rgb(0xf1c79b), 'h': rgb(0x5b3a1f), 'H': rgb(0xe8e4f0),
	'u': rgb(0x3a62c9), 'v': rgb(0x7c4dbe), 'a': rgb(0x5a4630),
	'm': rgb(0x6fc35a), 'M': rgb(0x3f8a35), 'c': rgb(0xf28c28), 'q': rgb(0xefe6d2),
}

// tileArt are the map tiles, named as world.yaml's tiles name them.
var tileArt = []struct {
	name string
	rows [8]string
}{
	{"grass", [8]string{"gggggggg", "gggGgggg", "gggggggg", "gGgggggg", "gggggGgg", "gggggggg", "ggGggggg", "gggggggg"}},
	{"path", [8]string{"dddddddd", "ddDddddd", "dddddddd", "dddddDdd", "dddddddd", "dDdddddd", "ddddddDd", "dddddddd"}},
	{"flowers", [8]string{"gggggggg", "gpggggYg", "pYpggggg", "gpgggYgg", "ggggYpYg", "gYgggYgg", "YpYggggg", "gYgggggg"}},
	{"bridge", [8]string{"bbbbbbbb", "tttttttt", "bbbbbbbb", "bbbbbbbb", "tttttttt", "bbbbbbbb", "bbbbbbbb", "tttttttt"}},
	{"tree", [8]string{"ggLLLLgg", "gLlllLLg", "LlllllLL", "LllLllLL", "gLLLLLLg", "gggttggg", "gggttggg", "gggggggg"}},
	{"water", [8]string{"wwwwwwww", "wWWwwwww", "wwwwwwww", "wwwwwWWw", "wwwwwwww", "wWWwwwww", "wwwwwwww", "wwwwwwww"}},
	{"wall", [8]string{"ssssssss", "SSSSSSSS", "ssSsssSs", "ssSsssSs", "SSSSSSSS", "sssSsssS", "sssSsssS", "SSSSSSSS"}},
	{"roof", [8]string{"rrrrrrrr", "RRRRRRRR", "rrrrrrrr", "rRrrrRrr", "rrrrrrrr", "RRRRRRRR", "rrrrrrrr", "rrRrrrRr"}},
	{"door", [8]string{"ssssssss", "sSooooSs", "sooooooS", "soooooos", "sooooYos", "soooooos", "soooooos", "SSSSSSSS"}},
	{"fence", [8]string{"gggggggg", "gfggggfg", "ffffffff", "gfggggfg", "gfggggfg", "ffffffff", "gfggggfg", "gggggggg"}},
}

// figure is a character, foe or item drawn over the map; a person's figure also makes their portrait.
type figure struct {
	name string
	rows [8]string
	// smile, when set, is the row and its replacement that turn the figure's portrait into its smiling expression.
	smileRow int
	smile    string
}

// figureArt are the player, the people, the foes and the items on the maps.
var figureArt = []figure{
	{name: "hero", rows: [8]string{"..hhhh..", ".hhhhhh.", ".ekeeke.", "..eeee..", ".uuuuuu.", "euuuuuue", "..uuuu..", "..t..t.."}},
	{name: "mira", rows: [8]string{"..HHHH..", ".HHHHHH.", ".HkeekH.", "..eeee..", ".vvvvvv.", "evvvvvve", ".vvvvvv.", ".vv..vv."}, smileRow: 3, smile: "..ekke.."},
	{name: "bram", rows: [8]string{"..kkkk..", ".kkkkkk.", ".ekeeke.", ".hhhhhh.", ".aaaaaa.", "eaaaaaae", ".aaaaaa.", ".kk..kk."}, smileRow: 3, smile: ".hkkkkh."},
	{name: "pell", rows: [8]string{"..ssss..", ".ssssss.", ".ekeeke.", "..eeee..", ".rrrrrr.", "srrrrrrs", ".rrrrrr.", ".kk..kk."}, smileRow: 3, smile: "..ekke.."},
	{name: "slime", rows: [8]string{"........", "........", "...mm...", "..mmmm..", ".mkmmkm.", ".mmmmmm.", "MMMMMMMM", "........"}},
	{name: "boar", rows: [8]string{"..L..L..", ".LoLLoL.", "oooooooo", "okooooo.", "qoooooo.", "oooooooo", ".o.o.o.o", "........"}},
	{name: "glowcap", rows: [8]string{"........", "..cccc..", ".cYccYc.", "cccccccc", "...qq...", "...qq...", "..qqqq..", "........"}},
	{name: "potion", rows: [8]string{"...tt...", "...WW...", "..WWWW..", ".WrrrrW.", ".WrrrrW.", ".WrrrrW.", "..WWWW..", "........"}},
	{name: "bundle", rows: [8]string{"........", "...tt...", "..bbbb..", ".bbffbb.", ".bbbbbb.", ".bbbbbb.", "..bbbb..", "........"}},
}

// portraitBackground fills a portrait behind its figure.
var portraitBackground = rgb(0x24283b)

// newAtlas paints every picture into one sheet: a row of tiles, a row of figures and a row of portraits, each person's plain and smiling ("-smile") face. Tiles and figures are named as world.yaml names them; portraits are "portrait/<name>", as data/cast.yaml names them.
func newAtlas() *sprite.Atlas {
	const cell = 8 * artScale
	const face = 8 * portraitScale
	var people []figure
	for _, f := range figureArt {
		if f.smile != "" {
			people = append(people, f)
		}
	}
	width := max(len(tileArt), len(figureArt)) * cell
	width = max(width, 2*len(people)*face)
	sheet := image.NewRGBA(image.Rect(0, 0, width, 2*cell+face))
	frames := map[string]sprite.Frame{}
	add := func(name string, x, y, size int) {
		s := float32(size)
		frames[name] = sprite.Frame{Name: name, X: float32(x), Y: float32(y), W: s, H: s, Width: s, Height: s}
	}
	for i, t := range tileArt {
		paint(sheet, i*cell, 0, artScale, t.rows)
		add(t.name, i*cell, 0, cell)
	}
	for i, f := range figureArt {
		paint(sheet, i*cell, cell, artScale, f.rows)
		add(f.name, i*cell, cell, cell)
	}
	for i, f := range people {
		smiling := f.rows
		smiling[f.smileRow] = f.smile
		for j, rows := range [][8]string{f.rows, smiling} {
			x := (2*i + j) * face
			fill(sheet, image.Rect(x, 2*cell, x+face, 2*cell+face), portraitBackground)
			paint(sheet, x, 2*cell, portraitScale, rows)
			name := "portrait/" + f.name
			if j == 1 {
				name += "-smile"
			}
			add(name, x, 2*cell, face)
		}
	}
	return &sprite.Atlas{Image: core.ImageSource{Pixels: sheet}, Frames: frames}
}

// paint draws rows of art at (x, y), each pixel scale by scale.
func paint(dst *image.RGBA, x, y, scale int, rows [8]string) {
	for r, row := range rows {
		for c := range len(row) {
			col, ok := palette[row[c]]
			if !ok {
				continue
			}
			fill(dst, image.Rect(x+c*scale, y+r*scale, x+(c+1)*scale, y+(r+1)*scale), col)
		}
	}
}

// fill paints rect with col.
func fill(dst *image.RGBA, rect image.Rectangle, col color.NRGBA) {
	for py := rect.Min.Y; py < rect.Max.Y; py++ {
		for px := rect.Min.X; px < rect.Max.X; px++ {
			dst.Set(px, py, col)
		}
	}
}

// rgb returns the opaque color 0xRRGGBB.
func rgb(hex uint32) color.NRGBA {
	return color.NRGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: 0xff}
}
