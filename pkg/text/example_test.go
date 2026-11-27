package text_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/text"
	"github.com/nsgo-dev/nsgo/core"
)

// font is a small BMFont in the text format that tools such as BMFont and Hiero export.
const font = `info face="Demo" size=16
common lineHeight=16 base=12 scaleW=64 scaleH=16 pages=1
page id=0 file="demo.png"
chars count=3
char id=32 x=0 y=0 width=0 height=0 xoffset=0 yoffset=0 xadvance=5 page=0 chnl=15
char id=65 x=0 y=0 width=8 height=10 xoffset=1 yoffset=2 xadvance=10 page=0 chnl=15
char id=66 x=8 y=0 width=8 height=10 xoffset=0 yoffset=2 xadvance=9 page=0 chnl=15
kernings count=1
kerning first=65 second=66 amount=-2
`

// LoadBMFontImage reads a bitmap font for one page image; Measure and Layout place its glyphs with their advances and kerning.
func ExampleLoadBMFontImage() {
	f, err := text.LoadBMFontImage(core.ImageSource{URI: "asset:demo.png"}, []byte(font))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(f.Face, f.LineHeight, f.Kerning('A', 'B'))
	fmt.Println(f.Measure("AB A", text.Options{}))
	for _, g := range f.Layout("AB", 0, 0, text.Options{}) {
		fmt.Println(string(g.Rune), g.X)
	}
	// Output:
	// Demo 16 -2
	// {32 16}
	// A 1
	// B 8
}

// ParseRich reads inline markup for colors, emphasis, icons and pauses; markup it cannot read stays as plain text instead of failing.
func ExampleParseRich() {
	rt := text.ParseRich("You found [color=gold]50 coins[/color] [icon=coin]!")
	fmt.Println(rt.PlainText())
	kinds := map[text.RichTokenKind]string{text.TokenGlyph: "glyph", text.TokenIcon: "icon", text.TokenPause: "pause"}
	for _, run := range rt.Runs() {
		fmt.Printf("%s %q %q\n", kinds[run.Kind], run.Text, run.IconName)
	}
	fmt.Println(text.ParseRich("[colr=red]typo").PlainText())
	// Output:
	// You found 50 coins !
	// glyph "You found " ""
	// glyph "50 coins" ""
	// glyph " " ""
	// icon "" "coin"
	// glyph "!" ""
	// [colr=red]typo
}

// A Typewriter reveals text a character at a time, pausing at punctuation and markup pauses, as dialogue boxes do; Skip shows the rest at once.
func ExampleTypewriter() {
	tw := text.NewTypewriterString("Hi.[pause=0.5] Bye", 10)
	for range 4 {
		tw.Update(0.25)
		fmt.Println(tw.VisibleCount(), "of", tw.TotalCount())
	}
	tw.Skip()
	fmt.Println(tw.Done())
	// Output:
	// 2 of 7
	// 3 of 7
	// 3 of 7
	// 5 of 7
	// true
}
