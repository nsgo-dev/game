package text_test

import (
	_ "embed"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/text"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var (
	//go:embed testdata/pixel.fnt
	pixelText []byte
	//go:embed testdata/pixel-xml.fnt
	pixelXML []byte
	//go:embed testdata/multipage.fnt
	multiPageText []byte
	//go:embed testdata/multipage-xml.fnt
	multiPageXML []byte
)

var (
	pixelImg = core.ImageSource{URI: "asset:pixel retro 0.png"}
	multi0   = core.ImageSource{URI: "asset:multi_0.png"}
	multi1   = core.ImageSource{URI: "asset:multi_1.png"}
)

func TestBMFontTextAndXMLParseIdentically(t *testing.T) {
	t.Run("single page", func(t *testing.T) {
		var textCalls, xmlCalls []string
		resolver := func(calls *[]string) func(int, string) core.ImageSource {
			return func(id int, file string) core.ImageSource {
				*calls = append(*calls, file)
				return core.ImageSource{URI: "asset:" + file}
			}
		}
		ft, err := text.LoadBMFont(pixelText, resolver(&textCalls))
		if err != nil {
			t.Fatalf("LoadBMFont(text): %v", err)
		}
		fx, err := text.LoadBMFont(pixelXML, resolver(&xmlCalls))
		if err != nil {
			t.Fatalf("LoadBMFont(xml): %v", err)
		}
		if !reflect.DeepEqual(ft, fx) {
			t.Fatalf("text and XML fonts differ:\ntext: %+v\nxml:  %+v", ft, fx)
		}
		if want := []string{"pixel retro 0.png"}; !reflect.DeepEqual(textCalls, want) || !reflect.DeepEqual(xmlCalls, want) {
			t.Fatalf("page callbacks text=%v xml=%v, want %v", textCalls, xmlCalls, want)
		}
		if ft.Face != "Pixel Retro Bold" || ft.Size != 16 || ft.LineHeight != 16 || ft.Base != 12 {
			t.Fatalf("header = (%q, %v, %v, %v), want (Pixel Retro Bold, 16, 16, 12)", ft.Face, ft.Size, ft.LineHeight, ft.Base)
		}
		if len(ft.Pages) != 1 || !reflect.DeepEqual(ft.Pages[0].Image, pixelImg) {
			t.Fatalf("pages = %+v", ft.Pages)
		}
		wantA := sprite.Frame{Name: "A", X: 0, Y: 0, W: 8, H: 10, Width: 8, Height: 10}
		if got := ft.Pages[0].Frames["A"]; got != wantA {
			t.Fatalf("atlas frame A = %+v, want %+v", got, wantA)
		}
		wantGlyphA := text.Glyph{ID: 'A', Frame: wantA, XOffset: 1, YOffset: 2, XAdvance: 10, Page: 0}
		if got := ft.Glyphs['A']; got != wantGlyphA {
			t.Fatalf("glyph A = %+v, want %+v", got, wantGlyphA)
		}
		if got := ft.Kerning('A', 'V'); got != -2 {
			t.Fatalf("kerning(A, V) = %v, want -2", got)
		}
		if got := ft.Kerning('T', 'o'); got != -1 {
			t.Fatalf("kerning(T, o) = %v, want -1", got)
		}
		if got := ft.Kerning('A', 'B'); got != 0 {
			t.Fatalf("kerning(A, B) = %v, want 0", got)
		}

		// UTF-8 BOM prefix parses identically.
		bomText := append([]byte{0xEF, 0xBB, 0xBF}, pixelText...)
		fb, err := text.LoadBMFontImage(pixelImg, bomText)
		if err != nil {
			t.Fatalf("LoadBMFontImage(BOM): %v", err)
		}
		if !reflect.DeepEqual(fb, ft) {
			t.Fatalf("BOM font differs from non-BOM font")
		}
	})

	t.Run("multi page", func(t *testing.T) {
		var textOrder, xmlOrder []int
		resolve := func(order *[]int) func(int, string) core.ImageSource {
			return func(id int, file string) core.ImageSource {
				*order = append(*order, id)
				return core.ImageSource{URI: "asset:" + file}
			}
		}
		ft, err := text.LoadBMFont(multiPageText, resolve(&textOrder))
		if err != nil {
			t.Fatalf("LoadBMFont(text): %v", err)
		}
		fx, err := text.LoadBMFont(multiPageXML, resolve(&xmlOrder))
		if err != nil {
			t.Fatalf("LoadBMFont(xml): %v", err)
		}
		if !reflect.DeepEqual(ft, fx) {
			t.Fatalf("multi-page text and XML fonts differ:\ntext: %+v\nxml:  %+v", ft, fx)
		}
		if want := []int{0, 1}; !reflect.DeepEqual(textOrder, want) || !reflect.DeepEqual(xmlOrder, want) {
			t.Fatalf("page order text=%v xml=%v, want %v", textOrder, xmlOrder, want)
		}
		if !reflect.DeepEqual(ft.Pages[0].Image, multi0) || !reflect.DeepEqual(ft.Pages[1].Image, multi1) {
			t.Fatalf("page images = (%+v, %+v), want (%+v, %+v)", ft.Pages[0].Image, ft.Pages[1].Image, multi0, multi1)
		}
	})
}

func TestLoadBMFontImageSingleAndMultiPage(t *testing.T) {
	f1, err := text.LoadBMFontImage(pixelImg, pixelText)
	if err != nil {
		t.Fatalf("LoadBMFontImage(text): %v", err)
	}
	f2, err := text.LoadBMFontImage(pixelImg, pixelXML)
	if err != nil {
		t.Fatalf("LoadBMFontImage(xml): %v", err)
	}
	if !reflect.DeepEqual(f1, f2) {
		t.Fatalf("LoadBMFontImage text and XML differ")
	}
	if _, err := text.LoadBMFontImage(multi0, multiPageText); err == nil || !strings.Contains(err.Error(), "2 pages") {
		t.Fatalf("multi-page LoadBMFontImage err = %v, want error mentioning 2 pages", err)
	}
}

func TestLoadBMFontRejectsInvalidInputs(t *testing.T) {
	validTextCommon := "common lineHeight=16 base=12 pages=1\n"
	validTextPage := "page id=0 file=\"a.png\"\n"
	validTextChar := "char id=65 x=0 y=0 width=8 height=8 xoffset=0 yoffset=0 xadvance=8 page=0\n"

	for _, tc := range []struct {
		name string
		data string
		want string
	}{
		{"empty", "", "empty"},
		{"whitespace", "   \n\t ", "empty"},
		{"unknown record", "bogus foo=1\n", "unknown record"},
		{"unclosed quote", validTextCommon + `page id=0 file="unclosed.png` + "\n" + validTextChar, "unterminated"},
		{"missing equals", validTextCommon + "page id=0 broken\n" + validTextChar, "key=value"},
		{"missing common text", validTextPage + validTextChar, "missing common"},
		{"zero lineHeight text", "common lineHeight=0 base=0 pages=1\n" + validTextPage + validTextChar, "lineHeight"},
		{"no pages text", validTextCommon + validTextChar, "no pages"},
		{"page count mismatch text", "common lineHeight=16 base=12 pages=2\n" + validTextPage + validTextChar, "declares 2 pages"},
		{"duplicate page text", "common lineHeight=16 base=12\n" + validTextPage + validTextPage + validTextChar, "duplicate page"},
		{"no chars text", validTextCommon + validTextPage, "no glyphs"},
		{"duplicate char text", validTextCommon + validTextPage + validTextChar + validTextChar, "duplicate char"},
		{"unknown page on char text", validTextCommon + validTextPage + "char id=65 x=0 y=0 width=8 height=8 xoffset=0 yoffset=0 xadvance=8 page=3\n", "unknown page 3"},
		{"negative bounds text", validTextCommon + validTextPage + "char id=65 x=0 y=0 width=-1 height=8 xoffset=0 yoffset=0 xadvance=8 page=0\n", "negative bounds"},
		{"bad number text", "common lineHeight=abc base=12 pages=1\n" + validTextPage + validTextChar, "lineHeight"},
		{"missing char id text", validTextCommon + validTextPage + "char x=0 y=0 width=8 height=8 page=0\n", "char missing id"},
		{"missing kerning second text", validTextCommon + validTextPage + validTextChar + "kerning first=65 amount=-1\n", "kerning missing second"},
		{"malformed xml", "<font><common", "XML"},
		{"wrong xml root", "<notfont/>", "expected element type <font>"},
		{"missing common xml", `<font><pages><page id="0" file="a.png"/></pages><chars><char id="65" x="0" y="0" width="8" height="8" xadvance="8" page="0"/></chars></font>`, "missing common"},
		{"zero lineHeight xml", `<font><common lineHeight="0" pages="1"/><pages><page id="0" file="a.png"/></pages><chars><char id="65" x="0" y="0" width="8" height="8" xadvance="8" page="0"/></chars></font>`, "lineHeight"},
		{"missing page id xml", `<font><common lineHeight="16" pages="1"/><pages><page file="a.png"/></pages><chars><char id="65" x="0" y="0" width="8" height="8" xadvance="8" page="0"/></chars></font>`, "missing id"},
		{"missing char id xml", `<font><common lineHeight="16" pages="1"/><pages><page id="0" file="a.png"/></pages><chars><char x="0" y="0" width="8" height="8" xadvance="8" page="0"/></chars></font>`, "missing id"},
		{"unknown page xml", `<font><common lineHeight="16" pages="1"/><pages><page id="0" file="a.png"/></pages><chars><char id="65" x="0" y="0" width="8" height="8" xadvance="8" page="2"/></chars></font>`, "unknown page 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := text.LoadBMFontImage(pixelImg, []byte(tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestDrawWithKerningMatchesHandPlacedOps(t *testing.T) {
	f, err := text.LoadBMFontImage(pixelImg, pixelText)
	if err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "bmfont", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				f.Draw(d, "AVA To", 10, 20, text.Options{})
				f.Draw(d, "AV", 5, 10, text.Options{Scale: 2, Alpha: 0.5})
				// Empty and negative alpha/scale draw nothing.
				f.Draw(d, "", 0, 0, text.Options{})
				f.Draw(d, "A", 0, 0, text.Options{Alpha: -1})
				f.Draw(d, "A", 0, 0, text.Options{Scale: -1})
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				// "AVA To" at (10, 20), scale 1:
				// A: pen=0, xoff=1, yoff=2 -> (11, 22), pen=10
				d.DrawImageFrom(pixelImg, 0, 0, 8, 10, 11, 22, 8, 10)
				// V: kern(A,V)=-2 -> pen=8, xoff=-1, yoff=2 -> (17, 22), pen=18
				d.DrawImageFrom(pixelImg, 56, 0, 8, 10, 17, 22, 8, 10)
				// A: kern(V,A)=-2 -> pen=16, xoff=1, yoff=2 -> (27, 22), pen=26
				d.DrawImageFrom(pixelImg, 0, 0, 8, 10, 27, 22, 8, 10)
				// ' ': w=0, h=0, xadv=5 -> pen=31 (no op)
				// T: pen=31, xoff=0, yoff=2 -> (41, 22), pen=41
				d.DrawImageFrom(pixelImg, 48, 0, 8, 10, 41, 22, 8, 10)
				// o: kern(T,o)=-1 -> pen=40, xoff=1, yoff=5 -> (51, 25), pen=48
				d.DrawImageFrom(pixelImg, 77, 0, 7, 7, 51, 25, 7, 7)

				// "AV" at (5, 10), scale 2, alpha 0.5:
				// A: pen=0, xoff=2, yoff=4 -> (7, 14), size (16, 20), pen=20
				d.Save().SetAlpha(0.5).DrawImageFrom(pixelImg, 0, 0, 8, 10, 7, 14, 16, 20).Restore()
				// V: kern(A,V)*2=-4 -> pen=16, xoff=-2, yoff=4 -> (19, 14), size (16, 20)
				d.Save().SetAlpha(0.5).DrawImageFrom(pixelImg, 56, 0, 8, 10, 19, 14, 16, 20).Restore()
			}},
		}}
	})
	got, want := app.Find("bmfont").Ops, app.Find("hand").Ops
	if len(got) == 0 {
		t.Fatal("bmfont drew nothing")
	}
	sameOps(t, got, want)
}

func TestWrappingAlignmentLayoutAndMeasure(t *testing.T) {
	f, err := text.LoadBMFontImage(pixelImg, pixelText)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("word wrap with spaces and kerning", func(t *testing.T) {
		// "AB" = 20, " " = 5, "Hi" = 10+5 = 15 => "AB Hi" = 40.
		if sz := f.Measure("AB Hi", text.Options{MaxWidth: 40}); sz != (core.Size{Width: 40, Height: 16}) {
			t.Fatalf("Measure fit = %+v, want {40 16}", sz)
		}
		// MaxWidth 39 forces wrap to ["AB", "Hi"].
		opts := text.Options{MaxWidth: 39, LineSpacing: 4}
		if sz := f.Measure("AB Hi", opts); sz != (core.Size{Width: 20, Height: 36}) {
			t.Fatalf("Measure wrapped = %+v, want {20 36}", sz)
		}
		got := f.Layout("AB Hi", 10, 20, opts)
		type pos struct {
			r    rune
			x, y float32
			line int
		}
		var gotPos []pos
		for _, p := range got {
			gotPos = append(gotPos, pos{p.Rune, p.X, p.Y, p.Line})
		}
		wantPos := []pos{
			{'A', 11, 22, 0}, // x=10 + pen=0 + xoff=1, y=20 + yoff=2
			{'B', 20, 22, 0}, // x=10 + pen=10 + xoff=0
			{'H', 10, 42, 1}, // y=20 + (16+4) + yoff=2
			{'i', 21, 42, 1}, // x=10 + pen=10 + xoff=1
		}
		if !reflect.DeepEqual(gotPos, wantPos) {
			t.Fatalf("wrapped layout:\ngot:  %+v\nwant: %+v", gotPos, wantPos)
		}

		// Kerning makes "AV" 18 points wide (10 - 2 + 10), so MaxWidth 18 fits on 1 line.
		if sz := f.Measure("AV", text.Options{MaxWidth: 18}); sz != (core.Size{Width: 18, Height: 16}) {
			t.Fatalf("Measure kerned fit = %+v, want {18 16}", sz)
		}
	})

	t.Run("explicit newlines and blank lines", func(t *testing.T) {
		opts := text.Options{LineSpacing: 2}
		// "A\r\n\r\nB" => 3 lines: height = 3*16 + 2*2 = 52.
		if sz := f.Measure("A\r\n\r\nB", opts); sz != (core.Size{Width: 10, Height: 52}) {
			t.Fatalf("Measure blank line = %+v, want {10 52}", sz)
		}
		got := f.Layout("A\r\n\r\nB", 0, 10, opts)
		if len(got) != 2 || got[0].Line != 0 || got[0].Y != 12 || got[1].Line != 2 || got[1].Y != 48 {
			t.Fatalf("blank line layout = %+v", got)
		}
	})

	t.Run("long word break fallback", func(t *testing.T) {
		// "ABCDE" (50pt) wrapped to MaxWidth 25 => ["AB", "CD", "E"].
		got := f.Layout("ABCDE", 0, 0, text.Options{MaxWidth: 25})
		wantLines := []int{0, 0, 1, 1, 2}
		if len(got) != len(wantLines) {
			t.Fatalf("len = %d, want %d", len(got), len(wantLines))
		}
		for i, wantLine := range wantLines {
			if got[i].Line != wantLine {
				t.Fatalf("glyph %d (%c) line = %d, want %d", i, got[i].Rune, got[i].Line, wantLine)
			}
		}
		// MaxWidth smaller than one glyph must still progress one rune per line.
		tiny := f.Layout("AB", 0, 0, text.Options{MaxWidth: 3})
		if len(tiny) != 2 || tiny[0].Line != 0 || tiny[1].Line != 1 {
			t.Fatalf("tiny wrap = %+v, want 2 lines", tiny)
		}
	})

	t.Run("alignment box and anchor modes", func(t *testing.T) {
		// "AB\nHi": line 0 width=20, line 1 width=15.
		// Box [10, 110] (x=10, MaxWidth=100):
		//   AlignLeft:   line 0 start=10,   line 1 start=10
		//   AlignCenter: line 0 start=50,   line 1 start=52.5
		//   AlignRight:  line 0 start=90,   line 1 start=95
		for _, tc := range []struct {
			name   string
			x      float32
			opts   text.Options
			wantAX float32 // 'A' has xoffset=1
			wantHX float32 // 'H' has xoffset=0
		}{
			{"box left", 10, text.Options{MaxWidth: 100, Align: text.AlignLeft}, 11, 10},
			{"box center", 10, text.Options{MaxWidth: 100, Align: text.AlignCenter}, 51, 52.5},
			{"box right", 10, text.Options{MaxWidth: 100, Align: text.AlignRight}, 91, 95},
			{"anchor left", 100, text.Options{Align: text.AlignLeft}, 101, 100},
			{"anchor center", 100, text.Options{Align: text.AlignCenter}, 91, 92.5},
			{"anchor right", 100, text.Options{Align: text.AlignRight}, 81, 85},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got := f.Layout("AB\nHi", tc.x, 0, tc.opts)
				if len(got) != 4 {
					t.Fatalf("len = %d, want 4", len(got))
				}
				if got[0].X != tc.wantAX {
					t.Errorf("A.X = %v, want %v", got[0].X, tc.wantAX)
				}
				if got[2].X != tc.wantHX {
					t.Errorf("H.X = %v, want %v", got[2].X, tc.wantHX)
				}
			})
		}
	})

	t.Run("measure empty and scaled", func(t *testing.T) {
		if sz := f.Measure("", text.Options{}); sz != (core.Size{}) {
			t.Fatalf("empty measure = %+v, want zero", sz)
		}
		// "AV" at scale 2: width = 18*2 = 36, height = 16*2 = 32.
		if sz := f.Measure("AV", text.Options{Scale: 2}); sz != (core.Size{Width: 36, Height: 32}) {
			t.Fatalf("scaled measure = %+v, want {36 32}", sz)
		}
	})
}

func TestMultiPageBMFontDraw(t *testing.T) {
	f, err := text.LoadBMFont(multiPageXML, func(id int, file string) core.ImageSource {
		return core.ImageSource{URI: "asset:" + file}
	})
	if err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "multi", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				// "AB C": A(page 0), B(page 1, kern(A,B)=-1), ' ', C(page 0).
				f.Draw(d, "AB C", 10, 20, text.Options{})
				// "BA": page 1 first, then page 0 (deterministic first-seen page order).
				f.Draw(d, "BA", 10, 50, text.Options{})
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				// "AB C" one batch per page in first-seen order (page 0 then page 1):
				// A: pen=0, xoff=0, yoff=2 -> (10, 22), pen=10
				// B: kern=-1 -> pen=9, xoff=1, yoff=2 -> (20, 22), pen=19
				// ' ': pen=24
				// C: pen=24, xoff=0, yoff=2 -> (34, 22)
				// Page 0 batch (A, C):
				d.DrawImageFrom(multi0, 0, 0, 8, 10, 10, 22, 8, 10)
				d.DrawImageFrom(multi0, 8, 0, 8, 10, 34, 22, 8, 10)
				// Page 1 batch (B):
				d.DrawImageFrom(multi1, 4, 2, 8, 10, 20, 22, 8, 10)

				// "BA" at (10, 50): page 1 batch (B), then page 0 batch (A):
				// B: pen=0, xoff=1, yoff=2 -> (11, 52), pen=10
				// A: pen=10, xoff=0, yoff=2 -> (20, 52)
				d.DrawImageFrom(multi1, 4, 2, 8, 10, 11, 52, 8, 10)
				d.DrawImageFrom(multi0, 0, 0, 8, 10, 20, 52, 8, 10)
			}},
		}}
	})
	sameOps(t, app.Find("multi").Ops, app.Find("hand").Ops)
}

func TestDrawPlatformAndVirtualResolutionTransform(t *testing.T) {
	titleFont := core.Font{Family: "sans-serif", Size: 10}
	white := core.RGB(0xffffff)
	gold := core.RGB(0xffcc00)

	var measured core.Size
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "platform", Style: core.Style{Width: core.Pt(960), Height: core.Pt(540)}, Draw: func(d *core.Drawing, _ core.Size) {
				// Simulate a game view applying a 3x virtual-resolution transform (320x180 -> 960x540).
				d.Save()
				d.Translate(40, 20).Scale(3, 3)

				// Single-line centered HUD text at virtual (160, 12).
				text.DrawPlatform(d, "SCORE 100", 160, 12, text.PlatformStyle{
					Font:     titleFont,
					Color:    white,
					Align:    text.AlignCenter,
					Baseline: core.BaselineTop,
				})

				// Wrapped right-aligned multiline text inside box [10, 60] (MaxWidth=50).
				// In headless ApproxTextEngine with size=10: each char is 6pt, lineHeight is 12pt.
				// "hello world": "hello" (30pt), "world" (30pt), "hello world" (66pt > 50pt) => 2 lines.
				wrapStyle := text.PlatformStyle{
					Font:        titleFont,
					Color:       gold,
					Alpha:       0.75,
					Align:       text.AlignRight,
					Baseline:    core.BaselineTop,
					MaxWidth:    50,
					LineSpacing: 2,
				}
				measured = text.MeasurePlatform(d, "hello world", wrapStyle)
				text.DrawPlatform(d, "hello world", 10, 40, wrapStyle)

				// Empty and negative-alpha calls emit no ops.
				text.DrawPlatform(d, "", 0, 0, wrapStyle)
				text.DrawPlatform(d, "hidden", 0, 0, text.PlatformStyle{Alpha: -1})

				d.Restore()
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(960), Height: core.Pt(540)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.Save()
				d.Translate(40, 20).Scale(3, 3)

				d.Save().
					SetFillColor(white).
					SetTextAlign(core.TextAlignCenter).
					SetTextBaseline(core.BaselineTop).
					FillText("SCORE 100", 160, 12, titleFont).
					Restore()

				d.Save().
					SetFillColor(gold).
					SetAlpha(0.75).
					SetTextAlign(core.TextAlignRight).
					SetTextBaseline(core.BaselineTop).
					FillText("hello", 60, 40, titleFont).
					FillText("world", 60, 54, titleFont).
					Restore()

				d.Restore()
			}},
		}}
	})
	got, want := app.Find("platform").Ops, app.Find("hand").Ops
	if len(got) == 0 {
		t.Fatal("DrawPlatform emitted no ops")
	}
	sameOps(t, got, want)

	// MeasurePlatform: max line width = 5*6 = 30, 2 lines of 12 + 2 spacing = 26.
	wantSize := core.Size{Width: 30, Height: 26}
	if math.Abs(float64(measured.Width-wantSize.Width)) > 1e-4 || math.Abs(float64(measured.Height-wantSize.Height)) > 1e-4 {
		t.Fatalf("MeasurePlatform = %+v, want %+v", measured, wantSize)
	}
}

// sameOps fails t unless got and want are the same operations, their numbers within float rounding.
func sameOps(t *testing.T, got, want []core.DrawOp) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d ops, want %d:\n%+v\nwant:\n%+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }
		gf := []float32{g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY}
		wf := []float32{w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY}
		for j := range gf {
			if !near(gf[j], wf[j]) {
				t.Fatalf("op %d is %+v, want %+v", i, g, w)
			}
		}
		if g.Kind != w.Kind || !reflect.DeepEqual(g.Image, w.Image) {
			t.Fatalf("op %d is %+v, want %+v", i, g, w)
		}
		g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY = w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY
		g.Image = w.Image
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("op %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}
