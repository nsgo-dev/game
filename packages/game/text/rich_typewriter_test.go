package text_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/text"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func makeSolidRGBA(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestParseRichMarkup(t *testing.T) {
	t.Run("nested colors styles scale effects icons pause and escapes", func(t *testing.T) {
		src := `[[ok] [color=#ff0000][b]HI[/b][/color] [color=gold][scale=1.5][i]A[/i][/scale][/color][icon=coin w=16 h=16][pause=0.25][wave amp=3 freq=4 speed=6]W[/wave][shake amp=2 speed=30 seed=7]S[/shake]`
		rt := text.ParseRich(src)

		if got, want := rt.PlainText(), "[ok] HI AWS"; got != want {
			t.Fatalf("PlainText() = %q, want %q", got, want)
		}

		// Verify token sequence:
		// 0..4: "[ok] " (plain)
		// 5..6: "HI" (red, bold)
		// 7:    " " (plain)
		// 8:    "A" (gold, scale 1.5, italic)
		// 9:    icon coin 16x16
		// 10:   pause 0.25
		// 11:   "W" (wave amp=3 freq=4 speed=6)
		// 12:   "S" (shake amp=2 speed=30 seed=7)
		if len(rt.Tokens) != 13 {
			t.Fatalf("len(Tokens) = %d, want 13: %+v", len(rt.Tokens), rt.Tokens)
		}
		if rt.Tokens[0].Rune != '[' || rt.Tokens[0].Style.Color != 0 || rt.Tokens[0].Style.Bold {
			t.Fatalf("token 0 = %+v, want plain '['", rt.Tokens[0])
		}
		if rt.Tokens[5].Rune != 'H' || rt.Tokens[5].Style.Color != core.RGB(0xff0000) || !rt.Tokens[5].Style.Bold {
			t.Fatalf("token 5 = %+v, want red bold 'H'", rt.Tokens[5])
		}
		if rt.Tokens[8].Rune != 'A' || rt.Tokens[8].Style.Color != core.RGB(0xffd700) || rt.Tokens[8].Style.Scale != 1.5 || !rt.Tokens[8].Style.Italic {
			t.Fatalf("token 8 = %+v, want gold italic scale=1.5 'A'", rt.Tokens[8])
		}
		iconTok := rt.Tokens[9]
		if iconTok.Kind != text.TokenIcon || iconTok.IconName != "coin" || iconTok.IconWidth != 16 || iconTok.IconHeight != 16 {
			t.Fatalf("token 9 = %+v, want coin 16x16 icon", iconTok)
		}
		pauseTok := rt.Tokens[10]
		if pauseTok.Kind != text.TokenPause || pauseTok.PauseSeconds != 0.25 {
			t.Fatalf("token 10 = %+v, want pause 0.25", pauseTok)
		}
		waveTok := rt.Tokens[11]
		if waveTok.Rune != 'W' || waveTok.Style.Effect.Kind != text.EffectWave ||
			waveTok.Style.Effect.Amplitude != 3 || waveTok.Style.Effect.Frequency != 4 || waveTok.Style.Effect.Speed != 6 {
			t.Fatalf("token 11 = %+v, want wave effect", waveTok)
		}
		shakeTok := rt.Tokens[12]
		if shakeTok.Rune != 'S' || shakeTok.Style.Effect.Kind != text.EffectShake ||
			shakeTok.Style.Effect.Amplitude != 2 || shakeTok.Style.Effect.Speed != 30 || shakeTok.Style.Effect.Seed != 7 {
			t.Fatalf("token 12 = %+v, want shake effect", shakeTok)
		}

		runs := rt.Runs()
		if len(runs) != 8 {
			t.Fatalf("len(Runs) = %d, want 8: %+v", len(runs), runs)
		}
		if runs[0].Text != "[ok] " || runs[1].Text != "HI" || runs[4].Kind != text.TokenIcon || runs[5].Kind != text.TokenPause {
			t.Fatalf("unexpected runs: %+v", runs)
		}
	})

	t.Run("color formats and nested scale/size", func(t *testing.T) {
		rt := text.ParseRich(`[color=#11223380]a[/color][color=#0f0]b[/color][size=2]c[scale=0.5]d[/scale]e[/size]`)
		if rt.PlainText() != "abcde" {
			t.Fatalf("PlainText = %q, want abcde", rt.PlainText())
		}
		if rt.Tokens[0].Style.Color != core.Color(0x11223380) {
			t.Fatalf("rgba color = %#x, want 0x11223380", uint32(rt.Tokens[0].Style.Color))
		}
		if rt.Tokens[1].Style.Color != core.RGB(0x00ff00) {
			t.Fatalf("short hex color = %#x, want 0x00ff00ff", uint32(rt.Tokens[1].Style.Color))
		}
		if rt.Tokens[2].Style.Scale != 2 || rt.Tokens[3].Style.Scale != 1 || rt.Tokens[4].Style.Scale != 2 {
			t.Fatalf("nested scales = (%v, %v, %v), want (2, 1, 2)",
				rt.Tokens[2].Style.Scale, rt.Tokens[3].Style.Scale, rt.Tokens[4].Style.Scale)
		}
	})
}

func TestParseRichFallbackOnInvalidAndUnclosedMarkup(t *testing.T) {
	plainCases := []struct {
		name string
		src  string
	}{
		{"unclosed bracket", "Hello [color=red"},
		{"unclosed color tag", "[color=red]Hello world"},
		{"unclosed bold tag", "[b]Hello"},
		{"unclosed wave tag", "[wave amp=3]wavy"},
		{"mismatched nesting", "[color=red][b]Hello[/color]"},
		{"mismatched close tag", "[b]Hello[/i]"},
		{"stray close tag", "[/color]Hello[/b]"},
		{"unknown tag", "[unknown]Hello[/unknown]"},
		{"invalid color", "[color=notacolor]Hello[/color]"},
		{"zero scale", "[scale=0]Hello[/scale]"},
		{"negative scale", "[scale=-1.5]Hello[/scale]"},
		{"non-numeric scale", "[scale=big]Hello[/scale]"},
		{"empty icon name", "[icon=]Hello"},
		{"bad icon size", "[icon=coin w=-5]Hello"},
		{"bad wave attr", "[wave amp=nope]Hello[/wave]"},
		{"bad shake attr", "[shake speed=NaN]Hello[/shake]"},
		{"negative pause", "[pause=-0.5]Hello"},
		{"empty brackets", "[] [/] Hello"},
		{"newline inside tag", "[color=red\n]Hello[/color]"},
	}

	for _, tc := range plainCases {
		t.Run(tc.name, func(t *testing.T) {
			rt := text.ParseRich(tc.src)
			if got := rt.PlainText(); got != tc.src {
				t.Fatalf("PlainText(%q) = %q, want original plain text", tc.src, got)
			}
			for i, tok := range rt.Tokens {
				if tok.Kind != text.TokenGlyph {
					t.Fatalf("token %d kind = %v, want TokenGlyph", i, tok.Kind)
				}
				if tok.Style.Color != 0 || tok.Style.Bold || tok.Style.Italic || tok.Style.Scale != 1 || tok.Style.Effect.Kind != text.EffectNone {
					t.Fatalf("token %d has non-plain style: %+v", i, tok.Style)
				}
			}
		})
	}

	t.Run("valid span survives beside unclosed tag", func(t *testing.T) {
		rt := text.ParseRich("[color=red]OK[/color] [b]unclosed")
		if got := rt.PlainText(); got != "OK [b]unclosed" {
			t.Fatalf("PlainText = %q, want %q", got, "OK [b]unclosed")
		}
		if rt.Tokens[0].Style.Color != core.RGB(0xff0000) || rt.Tokens[1].Style.Color != core.RGB(0xff0000) {
			t.Fatalf("valid red span lost color: %+v", rt.Tokens[:2])
		}
		for _, tok := range rt.Tokens[2:] {
			if tok.Style.Color != 0 || tok.Style.Bold {
				t.Fatalf("trailing unclosed text should be plain, got %+v", tok)
			}
		}
	})

	t.Run("deep nesting does not panic", func(t *testing.T) {
		src := strings.Repeat("[b]", 200) + "X" + strings.Repeat("[/b]", 200)
		rt := text.ParseRich(src)
		if !strings.Contains(rt.PlainText(), "X") {
			t.Fatalf("expected 'X' in parsed text, got %q", rt.PlainText())
		}
	})
}

func TestLayoutRichWrappingAlignmentAndInlineIcons(t *testing.T) {
	f, err := text.LoadBMFontImage(pixelImg, pixelText)
	if err != nil {
		t.Fatal(err)
	}

	coinImg := core.ImageSource{URI: "asset:icons.png"}
	coinAtlas := &sprite.Atlas{
		Image: coinImg,
		Frames: map[string]sprite.Frame{
			"coin": {Name: "coin", X: 0, Y: 0, W: 10, H: 10, Width: 10, Height: 10},
			"tall": {Name: "tall", X: 16, Y: 0, W: 12, H: 18, Width: 12, Height: 18},
		},
	}
	resolver := text.AtlasIconResolver(coinAtlas)

	t.Run("inline icons wrap like glyphs", func(t *testing.T) {
		// "AB" (20) + " " (5) + [icon=coin w=16 h=16] (16) = 41 > MaxWidth 35 =>
		// Line 0: "AB" (20)
		// Line 1: [icon=coin] (16) + " " (5) + "Hi" (15) = 36 > 35 =>
		// Line 1: [icon=coin] (16)
		// Line 2: "Hi" (15)
		rt := text.ParseRich("AB [icon=coin w=16 h=16] Hi")
		layout := text.LayoutRich(f, rt, text.RichOptions{
			Options: text.Options{MaxWidth: 35, LineSpacing: 2},
			Icons:   resolver,
		})
		if len(layout.Lines) != 3 {
			t.Fatalf("len(Lines) = %d, want 3: %+v", len(layout.Lines), layout.Lines)
		}
		if layout.Lines[0].Width != 20 || layout.Lines[1].Width != 16 || layout.Lines[2].Width != 15 {
			t.Fatalf("line widths = (%v, %v, %v), want (20, 16, 15)",
				layout.Lines[0].Width, layout.Lines[1].Width, layout.Lines[2].Width)
		}
		// Consecutive icons without spaces break item-by-item when exceeding MaxWidth.
		rtIcons := text.ParseRich("[icon=coin][icon=coin][icon=coin]")
		iconLayout := text.LayoutRich(f, rtIcons, text.RichOptions{
			Options: text.Options{MaxWidth: 25}, // 2 coins = 20pt fit, 3rd wraps
			Icons:   resolver,
		})
		if len(iconLayout.Lines) != 2 || len(iconLayout.Placements) != 3 {
			t.Fatalf("icon wrap lines=%d placements=%d, want 2 lines, 3 placements", len(iconLayout.Lines), len(iconLayout.Placements))
		}
		if iconLayout.Placements[0].Line != 0 || iconLayout.Placements[1].Line != 0 || iconLayout.Placements[2].Line != 1 {
			t.Fatalf("icon wrap placement lines = (%d, %d, %d), want (0, 0, 1)",
				iconLayout.Placements[0].Line, iconLayout.Placements[1].Line, iconLayout.Placements[2].Line)
		}
	})

	t.Run("baseline alignment for normal icons, tall icons, baseline offset, and mixed scale", func(t *testing.T) {
		// Normal 10x10 icon alongside 'A' (LineHeight=16, Base=12, A yoffset=2 h=10 -> bottom=12).
		rt := text.ParseRich("A[icon=coin]")
		layout := text.LayoutRich(f, rt, text.RichOptions{Icons: resolver})
		if len(layout.Placements) != 2 {
			t.Fatalf("placements = %d, want 2", len(layout.Placements))
		}
		a, icon := layout.Placements[0], layout.Placements[1]
		if a.BaselineY != 12 || icon.BaselineY != 12 {
			t.Fatalf("baselines = (%v, %v), want 12", a.BaselineY, icon.BaselineY)
		}
		if a.Y+a.H != 12 || icon.Y+icon.H != 12 {
			t.Fatalf("bottoms = (A:%v, icon:%v), want both on baseline 12", a.Y+a.H, icon.Y+icon.H)
		}

		// Tall 12x18 icon expands line ascent to 18; both 'A' and icon share baseline 18!
		rtTall := text.ParseRich("A[icon=tall]")
		tallLayout := text.LayoutRich(f, rtTall, text.RichOptions{Icons: resolver})
		aT, tallIcon := tallLayout.Placements[0], tallLayout.Placements[1]
		if aT.BaselineY != 18 || tallIcon.BaselineY != 18 {
			t.Fatalf("tall baselines = (%v, %v), want 18", aT.BaselineY, tallIcon.BaselineY)
		}
		if aT.Y+aT.H != 18 || tallIcon.Y+tallIcon.H != 18 {
			t.Fatalf("tall bottoms = (A:%v, tall:%v), want both on baseline 18", aT.Y+aT.H, tallIcon.Y+tallIcon.H)
		}

		// BaselineOffset shifts icon down relative to baseline.
		offsetResolver := func(name string) (text.Icon, bool) {
			ic, ok := resolver(name)
			ic.BaselineOffset = 2
			return ic, ok
		}
		offLayout := text.LayoutRich(f, rt, text.RichOptions{Icons: offsetResolver})
		if gotBottom := offLayout.Placements[1].Y + offLayout.Placements[1].H; gotBottom != 14 {
			t.Fatalf("offset icon bottom = %v, want 14 (baseline 12 + offset 2)", gotBottom)
		}

		// Mixed scale: [scale=2]A[/scale]B -> both sit on baseline 24.
		rtScale := text.ParseRich("[scale=2]A[/scale]B")
		scLayout := text.LayoutRich(f, rtScale, text.RichOptions{})
		if len(scLayout.Placements) != 2 {
			t.Fatalf("scaled placements = %d, want 2", len(scLayout.Placements))
		}
		bigA, smallB := scLayout.Placements[0], scLayout.Placements[1]
		if bigA.BaselineY != 24 || smallB.BaselineY != 24 {
			t.Fatalf("scaled baselines = (%v, %v), want 24", bigA.BaselineY, smallB.BaselineY)
		}
		if bigA.Y+bigA.H != 24 || smallB.Y+smallB.H != 24 {
			t.Fatalf("scaled bottoms = (%v, %v), want both 24", bigA.Y+bigA.H, smallB.Y+smallB.H)
		}
	})

	t.Run("alignment box honors icon widths", func(t *testing.T) {
		// "A[icon=coin]" width = 10 + 10 = 20 inside MaxWidth = 100.
		rt := text.ParseRich("A[icon=coin]")
		for _, tc := range []struct {
			align     text.Align
			wantLineX float32
		}{
			{text.AlignLeft, 0},
			{text.AlignCenter, 40},
			{text.AlignRight, 80},
		} {
			layout := text.LayoutRich(f, rt, text.RichOptions{
				Options: text.Options{MaxWidth: 100, Align: tc.align},
				Icons:   resolver,
			})
			if layout.Lines[0].X != tc.wantLineX {
				t.Errorf("align %v: Line.X = %v, want %v", tc.align, layout.Lines[0].X, tc.wantLineX)
			}
			// Coin icon starts after 'A' (advance 10).
			if layout.Placements[1].X != tc.wantLineX+10 {
				t.Errorf("align %v: icon.X = %v, want %v", tc.align, layout.Placements[1].X, tc.wantLineX+10)
			}
		}
	})
}

func TestDrawRichColoredSpansIconsBatchAndStyles(t *testing.T) {
	whitePixels := makeSolidRGBA(128, 64, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	whitePNG := encodePNG(t, whitePixels)

	fontPixels, err := text.LoadBMFontImage(core.ImageSource{Pixels: whitePixels}, pixelText)
	if err != nil {
		t.Fatal(err)
	}
	fontPNG, err := text.LoadBMFontImage(core.ImageSource{Data: whitePNG}, pixelText)
	if err != nil {
		t.Fatal(err)
	}

	coinImg := core.ImageSource{Pixels: makeSolidRGBA(16, 16, color.RGBA{R: 255, G: 215, B: 0, A: 255})}
	coinAtlas := &sprite.Atlas{
		Image: coinImg,
		Frames: map[string]sprite.Frame{
			"coin": {Name: "coin", X: 0, Y: 0, W: 10, H: 10, Width: 10, Height: 10},
		},
	}

	// Verify colored spans genuinely tint pixels (both Pixels and PNG Data sources)
	// and inline icons draw baseline-aligned.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "pixels", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				rt := text.ParseRich("[color=#ff0000]A[/color][color=#00ff0080]B[/color][icon=coin]")
				text.DrawRich(d, fontPixels, rt, core.Point{X: 10, Y: 20}, text.RichOptions{
					Icons: text.AtlasIconResolver(coinAtlas),
				})
			}},
			core.Canvas{TestID: "png", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				rt := text.ParseRich("[color=#0000ff]A[/color]")
				text.DrawRich(d, fontPNG, rt, core.Point{X: 0, Y: 0}, text.RichOptions{})
			}},
			core.Canvas{TestID: "styles", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				rt := text.ParseRich("[b]A[/b][i]B[/i]")
				text.DrawRich(d, fontPixels, rt, core.Point{X: 10, Y: 20}, text.RichOptions{})
			}},
		}}
	})

	pixelOps := app.Find("pixels").Ops
	if len(pixelOps) != 3 {
		t.Fatalf("pixels ops = %d, want 3 (A, B, coin): %+v", len(pixelOps), pixelOps)
	}
	// Op 0: red 'A'
	if pixelOps[0].Image == nil || pixelOps[0].Image.Pixels == nil {
		t.Fatalf("op 0 missing tinted Pixels: %+v", pixelOps[0])
	}
	gotRed := pixelOps[0].Image.Pixels.RGBAAt(0, 0)
	if gotRed != (color.RGBA{R: 255, G: 0, B: 0, A: 255}) {
		t.Fatalf("red glyph pixel = %+v, want {255 0 0 255}", gotRed)
	}
	// Op 1: 50% green 'B' (premultiplied: 0, 128, 0, 128)
	if pixelOps[1].Image == nil || pixelOps[1].Image.Pixels == nil {
		t.Fatalf("op 1 missing tinted Pixels: %+v", pixelOps[1])
	}
	gotGreen := pixelOps[1].Image.Pixels.RGBAAt(0, 0)
	if gotGreen.R != 0 || gotGreen.B != 0 || absDiff(gotGreen.G, 128) > 1 || absDiff(gotGreen.A, 128) > 1 {
		t.Fatalf("semi-transparent green glyph pixel = %+v, want ~(0, 128, 0, 128)", gotGreen)
	}
	// Op 2: coin icon at x=10+20=30, y=20+2=22, w=10, h=10 (bottom=32 == baseline 20+12)
	coinOp := pixelOps[2]
	if coinOp.X != 30 || coinOp.Y != 22 || coinOp.W != 10 || coinOp.H != 10 {
		t.Fatalf("coin op rect = (%v,%v,%v,%v), want (30,22,10,10)", coinOp.X, coinOp.Y, coinOp.W, coinOp.H)
	}

	// PNG Data source tinting:
	pngOps := app.Find("png").Ops
	if len(pngOps) != 1 || pngOps[0].Image == nil || pngOps[0].Image.Pixels == nil {
		t.Fatalf("png ops = %+v, want 1 tinted op", pngOps)
	}
	if gotBlue := pngOps[0].Image.Pixels.RGBAAt(0, 0); gotBlue != (color.RGBA{R: 0, G: 0, B: 255, A: 255}) {
		t.Fatalf("blue PNG glyph pixel = %+v, want {0 0 255 255}", gotBlue)
	}

	// Faux-bold (2 draws for A) + Faux-italic (Save + Transform + Draw + Restore for B):
	styleOps := app.Find("styles").Ops
	drawCount := len(gametest.ImageDraws(styleOps))
	hasTransform := false
	for _, op := range styleOps {
		if op.Kind == core.DrawTransform {
			hasTransform = true
		}
	}
	if drawCount != 3 {
		t.Fatalf("faux bold+italic image draws = %d, want 3 (2 for bold A, 1 for italic B)", drawCount)
	}
	if !hasTransform {
		t.Fatalf("faux italic did not emit DrawTransform shear op")
	}
}

func TestAppendRichToBatchSharedAtlas(t *testing.T) {
	f, err := text.LoadBMFontImage(pixelImg, pixelText)
	if err != nil {
		t.Fatal(err)
	}
	// Add an inline icon frame directly onto the font's own atlas page 0.
	sharedAtlas := f.Pages[0]
	sharedAtlas.Frames["star"] = sprite.Frame{Name: "star", X: 96, Y: 0, W: 10, H: 10, Width: 10, Height: 10}

	rt := text.ParseRich("A[icon=star]B")
	opts := text.RichOptions{Icons: text.AtlasIconResolver(sharedAtlas)}
	layout := text.LayoutRich(f, rt, opts)

	batch := &sprite.Batch{Atlas: sharedAtlas}
	n := text.AppendRichToBatch(batch, layout, core.Point{X: 10, Y: 20}, opts)
	if n != 3 || len(batch.Sprites) != 3 {
		t.Fatalf("AppendRichToBatch appended %d (len=%d), want 3", n, len(batch.Sprites))
	}

	// Drawing the batch produces identical DrawOps to DrawRich.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "batched", Style: core.Style{Width: core.Pt(100), Height: core.Pt(50)}, Draw: func(d *core.Drawing, _ core.Size) {
				batch.Draw(d)
			}},
			core.Canvas{TestID: "direct", Style: core.Style{Width: core.Pt(100), Height: core.Pt(50)}, Draw: func(d *core.Drawing, _ core.Size) {
				text.DrawRich(d, f, rt, core.Point{X: 10, Y: 20}, opts)
			}},
		}}
	})
	sameOps(t, app.Find("batched").Ops, app.Find("direct").Ops)
}

func TestDrawRichPlatformSpansAndIcons(t *testing.T) {
	baseFont := core.Font{Family: "sans-serif", Size: 10}
	coinAtlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:coin.png"},
		Frames: map[string]sprite.Frame{
			"coin": {Name: "coin", X: 0, Y: 0, W: 10, H: 10, Width: 10, Height: 10},
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "plat", Style: core.Style{Width: core.Pt(300), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.SetFillColor(core.RGB(0xffffff))
				rt := text.ParseRich("[color=red][b]CRIT[/b][/color] [color=gold][i]100[/i][/color][icon=coin] plain")
				text.DrawRich(d, nil, rt, core.Point{X: 10, Y: 20}, text.RichOptions{
					Icons: text.AtlasIconResolver(coinAtlas),
					Platform: &text.PlatformStyle{
						Font:     baseFont,
						Baseline: core.BaselineTop,
					},
				})
			}},
		}}
	})

	ops := app.Find("plat").Ops
	var sawRedBold, sawGoldItalic, sawCoin, sawPlain bool
	var curColor core.Color
	var colorStack []core.Color
	for _, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			colorStack = append(colorStack, curColor)
		case core.DrawRestore:
			if len(colorStack) > 0 {
				curColor = colorStack[len(colorStack)-1]
				colorStack = colorStack[:len(colorStack)-1]
			}
		case core.DrawFillColor:
			curColor = op.Color
		case core.DrawFillText:
			switch op.Text {
			case "CRIT":
				if curColor == core.RGB(0xff0000) && op.Font.Weight == core.FontWeightBold {
					sawRedBold = true
				}
			case "100":
				if curColor == core.RGB(0xffd700) && op.Font.Italic {
					sawGoldItalic = true
				}
			case "plain":
				// Must have restored back to the canvas's white fill color!
				if curColor == core.RGB(0xffffff) && !op.Font.Italic && op.Font.Weight != core.FontWeightBold {
					sawPlain = true
				}
			}
		case core.DrawImage:
			if op.Image != nil && op.Image.URI == "asset:coin.png" && op.W == 10 && op.H == 10 {
				sawCoin = true
			}
		}
	}
	if !sawRedBold || !sawGoldItalic || !sawCoin || !sawPlain {
		t.Fatalf("platform rich draw missing expected ops: redBold=%v goldItalic=%v coin=%v plain=%v\nops: %+v",
			sawRedBold, sawGoldItalic, sawCoin, sawPlain, ops)
	}
}

func TestTypewriterProgressionPauseSkipReset(t *testing.T) {
	t.Run("fixed step 50fps and inline icons", func(t *testing.T) {
		tw := text.NewTypewriterString("A[icon=coin]B", 10)
		if tw.TotalCount() != 3 || tw.VisibleCount() != 0 || tw.Done() || tw.Progress() != 0 {
			t.Fatalf("initial state = (vis=%d, tot=%d, done=%v, prog=%v)",
				tw.VisibleCount(), tw.TotalCount(), tw.Done(), tw.Progress())
		}

		type ev struct {
			idx int
			r   rune
		}
		var reveals []ev
		completeCount := 0
		tw.OnReveal = func(idx int, r rune) { reveals = append(reveals, ev{idx, r}) }
		tw.OnComplete = func() { completeCount++ }

		// 50fps: 5 steps of 0.02s == 0.1s == 1 unit at 10 cps.
		for i := 0; i < 4; i++ {
			tw.Update(0.02)
		}
		if tw.VisibleCount() != 0 {
			t.Fatalf("after 0.08s visible = %d, want 0", tw.VisibleCount())
		}
		tw.Update(0.02) // 0.10s -> 'A'
		if tw.VisibleCount() != 1 || tw.Done() {
			t.Fatalf("after 0.10s visible=%d done=%v, want 1 false", tw.VisibleCount(), tw.Done())
		}

		for i := 0; i < 5; i++ {
			tw.Update(0.02) // 0.20s -> [icon=coin]
		}
		if tw.VisibleCount() != 2 || tw.Done() {
			t.Fatalf("after 0.20s visible=%d done=%v, want 2 false", tw.VisibleCount(), tw.Done())
		}

		for i := 0; i < 5; i++ {
			tw.Update(0.02) // 0.30s -> 'B' + complete
		}
		if tw.VisibleCount() != 3 || !tw.Done() || tw.Progress() != 1 || completeCount != 1 {
			t.Fatalf("after 0.30s vis=%d done=%v prog=%v completes=%d",
				tw.VisibleCount(), tw.Done(), tw.Progress(), completeCount)
		}
		wantReveals := []ev{{0, 'A'}, {1, 0}, {2, 'B'}}
		if !reflect.DeepEqual(reveals, wantReveals) {
			t.Fatalf("reveals = %+v, want %+v", reveals, wantReveals)
		}

		// Subsequent Updates advance Elapsed for wave/shake animations but never re-fire OnComplete.
		prevElapsed := tw.Elapsed()
		tw.Update(0.5)
		tw.Skip()
		if completeCount != 1 {
			t.Fatalf("OnComplete fired %d times, want strictly 1", completeCount)
		}
		if tw.Elapsed() <= prevElapsed {
			t.Fatalf("Elapsed did not advance after completion: %v <= %v", tw.Elapsed(), prevElapsed)
		}
	})

	t.Run("pause tag and punctuation pause", func(t *testing.T) {
		// Stepwise 0.1s vs single 0.5s Update must agree identically.
		stepTW := text.NewTypewriterString("Hi[pause=0.2]!", 10)
		for i := 0; i < 4; i++ {
			stepTW.Update(0.1)
		}
		if stepTW.VisibleCount() != 2 || stepTW.Done() {
			t.Fatalf("at 0.4s (during pause) vis=%d done=%v, want 2 false", stepTW.VisibleCount(), stepTW.Done())
		}
		stepTW.Update(0.1)
		if stepTW.VisibleCount() != 3 || !stepTW.Done() {
			t.Fatalf("at 0.5s vis=%d done=%v, want 3 true", stepTW.VisibleCount(), stepTW.Done())
		}

		bulkTW := text.NewTypewriterString("Hi[pause=0.2]!", 10)
		bulkTW.Update(0.5)
		if bulkTW.VisibleCount() != 3 || !bulkTW.Done() {
			t.Fatalf("bulk 0.5s vis=%d done=%v, want 3 true", bulkTW.VisibleCount(), bulkTW.Done())
		}

		// PunctuationPause: '.' has multiplier 4 => 3 extra steps (0.3s) + 0.1s normal = 0.4s before 'B'.
		pTW := text.NewTypewriterString("A.B", 10)
		pTW.PunctuationPause = text.DefaultPunctuationPause()
		pTW.Update(0.2) // reveals 'A' (0.1) and '.' (0.2)
		if pTW.VisibleCount() != 2 {
			t.Fatalf("after 0.2s vis=%d, want 2", pTW.VisibleCount())
		}
		pTW.Update(0.3) // still waiting on punctuation pause (needs 0.4s after '.')
		if pTW.VisibleCount() != 2 {
			t.Fatalf("during punctuation pause vis=%d, want 2", pTW.VisibleCount())
		}
		pTW.Update(0.1) // total 0.4s after '.' -> reveals 'B'
		if pTW.VisibleCount() != 3 || !pTW.Done() {
			t.Fatalf("after punctuation pause vis=%d done=%v, want 3 true", pTW.VisibleCount(), pTW.Done())
		}
	})

	t.Run("Skip and Reset", func(t *testing.T) {
		tw := text.NewTypewriterString("Hello", 10)
		completes := 0
		tw.OnComplete = func() { completes++ }

		tw.Skip()
		if tw.VisibleCount() != 5 || !tw.Done() || completes != 1 {
			t.Fatalf("after Skip: vis=%d done=%v completes=%d", tw.VisibleCount(), tw.Done(), completes)
		}
		tw.Skip()
		tw.Update(1.0)
		if completes != 1 {
			t.Fatalf("after second Skip + Update: completes=%d, want 1", completes)
		}

		tw.Reset(text.ParseRich("XY"))
		if tw.VisibleCount() != 0 || tw.TotalCount() != 2 || tw.Done() || tw.Elapsed() != 0 {
			t.Fatalf("after Reset: vis=%d tot=%d done=%v elapsed=%v",
				tw.VisibleCount(), tw.TotalCount(), tw.Done(), tw.Elapsed())
		}
		tw.Update(0.2)
		if !tw.Done() || completes != 2 {
			t.Fatalf("after Reset completion: done=%v completes=%d, want true 2", tw.Done(), completes)
		}
	})

	t.Run("empty text completes once", func(t *testing.T) {
		tw := text.NewTypewriter(text.ParseRich(""), 10)
		if tw.TotalCount() != 0 || !tw.Done() || tw.Progress() != 1 {
			t.Fatalf("empty typewriter state = (tot=%d done=%v prog=%v)", tw.TotalCount(), tw.Done(), tw.Progress())
		}
		completes := 0
		tw.OnComplete = func() { completes++ }
		tw.Update(0.1)
		tw.Skip()
		tw.Update(0.1)
		if completes != 1 {
			t.Fatalf("empty typewriter OnComplete fired %d times, want 1", completes)
		}
	})
}

func TestEvalGlyphTransformDeterminismAndEffects(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		dx, dy, col := text.EvalGlyphTransform(text.GlyphEffect{Kind: text.EffectNone}, 3, 1.5)
		if dx != 0 || dy != 0 || col != nil {
			t.Fatalf("EffectNone = (%v, %v, %v), want (0, 0, nil)", dx, dy, col)
		}
	})

	t.Run("wave deterministic and bounded", func(t *testing.T) {
		eff := text.GlyphEffect{Kind: text.EffectWave, Seed: 42, Amplitude: 4, Frequency: 1.5, Speed: 5}
		dx1, dy1, col1 := text.EvalGlyphTransform(eff, 2, 0.75)
		dx2, dy2, col2 := text.EvalGlyphTransform(eff, 2, 0.75)
		if dx1 != 0 || dx2 != 0 || dy1 != dy2 || col1 != nil || col2 != nil {
			t.Fatalf("wave not deterministic: (%v,%v) vs (%v,%v)", dx1, dy1, dx2, dy2)
		}
		if math.Abs(float64(dy1)) > 4.0001 {
			t.Fatalf("wave dy %v exceeds amplitude 4", dy1)
		}
		_, dyOther, _ := text.EvalGlyphTransform(eff, 3, 0.75)
		if dyOther == dy1 {
			t.Fatalf("adjacent glyph indices produced identical wave dy %v", dy1)
		}
	})

	t.Run("shake deterministic across repeated runs and varies across seeds", func(t *testing.T) {
		effA := text.GlyphEffect{Kind: text.EffectShake, Seed: 12345, Amplitude: 2.5, Speed: 30}
		effB := text.GlyphEffect{Kind: text.EffectShake, Seed: 99999, Amplitude: 2.5, Speed: 30}

		type pt struct{ dx, dy float32 }
		sample := func(eff text.GlyphEffect) []pt {
			out := make([]pt, 0, 20)
			for g := 0; g < 5; g++ {
				for s := 0; s < 4; s++ {
					dx, dy, col := text.EvalGlyphTransform(eff, g, float32(s)*0.1)
					if col != nil {
						t.Fatalf("shake returned unexpected colorOverride %v", col)
					}
					if math.Abs(float64(dx)) > 2.5001 || math.Abs(float64(dy)) > 2.5001 {
						t.Fatalf("shake (%v, %v) exceeds amplitude 2.5", dx, dy)
					}
					out = append(out, pt{dx, dy})
				}
			}
			return out
		}

		run1 := sample(effA)
		run2 := sample(effA)
		if !reflect.DeepEqual(run1, run2) {
			t.Fatalf("shake not deterministic across runs:\nrun1=%+v\nrun2=%+v", run1, run2)
		}
		otherSeed := sample(effB)
		if reflect.DeepEqual(run1, otherSeed) {
			t.Fatalf("different seeds produced identical shake samples")
		}
	})

	t.Run("custom effect", func(t *testing.T) {
		gold := core.RGB(0xffd700)
		eff := text.GlyphEffect{
			Kind: text.EffectCustom,
			Custom: func(e text.GlyphEffect, idx int, elapsed float32) (float32, float32, *core.Color) {
				return float32(idx) * 2, elapsed * 3, &gold
			},
		}
		dx, dy, col := text.EvalGlyphTransform(eff, 4, 2.0)
		if dx != 8 || dy != 6 || col == nil || *col != gold {
			t.Fatalf("EffectCustom = (%v, %v, %v), want (8, 6, gold)", dx, dy, col)
		}
	})
}

func TestTypewriterDrawPartialRevealAndPerGlyphEffects(t *testing.T) {
	whitePixels := makeSolidRGBA(128, 64, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	f, err := text.LoadBMFontImage(core.ImageSource{Pixels: whitePixels}, pixelText)
	if err != nil {
		t.Fatal(err)
	}

	// Verify partial reveal keeps wrapped coordinates rock-steady and applies wave/custom offsets.
	twWrap := text.NewTypewriterString("AB Hi", 10)
	twWrap.Update(0.4) // reveals 'A'(0), 'B'(1), ' '(2), 'H'(3) — 'i'(4) stays hidden!

	gold := core.RGB(0xffd700)
	customRT := text.RichText{
		Tokens: []text.RichToken{
			{
				Kind: text.TokenGlyph,
				Rune: 'A',
				Style: text.RichStyle{
					Scale: 1,
					Effect: text.GlyphEffect{
						Kind: text.EffectCustom,
						Custom: func(_ text.GlyphEffect, idx int, elapsed float32) (float32, float32, *core.Color) {
							return 5, -3, &gold
						},
					},
				},
			},
		},
	}
	twEffect := text.NewTypewriter(customRT, 10)
	twEffect.Update(0.1)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "partial", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				twWrap.Draw(d, f, core.Point{X: 10, Y: 20}, text.RichOptions{
					Options: text.Options{MaxWidth: 39, LineSpacing: 4},
				})
			}},
			core.Canvas{TestID: "full", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				text.DrawRich(d, f, twWrap.RichText(), core.Point{X: 10, Y: 20}, text.RichOptions{
					Options: text.Options{MaxWidth: 39, LineSpacing: 4},
				})
			}},
			core.Canvas{TestID: "effect", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				twEffect.Draw(d, f, core.Point{X: 10, Y: 20}, text.RichOptions{})
			}},
		}}
	})

	partialOps := app.Find("partial").Ops
	fullOps := app.Find("full").Ops
	// Full "AB Hi" draws 4 visible glyphs (A, B, H, i); partial draws strictly 3 (A, B, H) at exact same positions!
	partialIms, fullIms := gametest.ImageDraws(partialOps), gametest.ImageDraws(fullOps)
	if len(partialIms) != 3 || len(fullIms) != 4 {
		t.Fatalf("image draws partial=%d full=%d, want 3 and 4", len(partialIms), len(fullIms))
	}
	if !reflect.DeepEqual(partialIms, fullIms[:3]) {
		t.Fatalf("partial glyphs %+v, want the full text's first three %+v", partialIms, fullIms[:3])
	}

	// Custom effect shifts 'A' (normally at 10+1=11, 20+2=22) by (+5, -3) -> (16, 19) and tints it gold!
	effOps := app.Find("effect").Ops
	if len(effOps) != 1 {
		t.Fatalf("effect ops = %d, want 1", len(effOps))
	}
	if effOps[0].X != 16 || effOps[0].Y != 19 {
		t.Fatalf("effect glyph pos = (%v, %v), want (16, 19)", effOps[0].X, effOps[0].Y)
	}
	if effOps[0].Image == nil || effOps[0].Image.Pixels == nil {
		t.Fatalf("effect glyph missing tinted pixels")
	}
	if gotGold := effOps[0].Image.Pixels.RGBAAt(0, 0); gotGold != (color.RGBA{R: 255, G: 215, B: 0, A: 255}) {
		t.Fatalf("effect glyph pixel = %+v, want gold {255 215 0 255}", gotGold)
	}
}

func TestUnicodeAlternateFontsAndPlatformTypewriter(t *testing.T) {
	t.Run("unicode markup and typewriter counts runes", func(t *testing.T) {
		tw := text.NewTypewriterString("[color=gold]勇者[/color]!", 10)
		if tw.TotalCount() != 3 || tw.RichText().PlainText() != "勇者!" {
			t.Fatalf("unicode typewriter tot=%d plain=%q, want 3 %q", tw.TotalCount(), tw.RichText().PlainText(), "勇者!")
		}
		var got []rune
		tw.OnReveal = func(_ int, r rune) { got = append(got, r) }
		tw.Update(0.3)
		if !reflect.DeepEqual(got, []rune{'勇', '者', '!'}) || !tw.Done() {
			t.Fatalf("unicode reveals = %q done=%v", string(got), tw.Done())
		}
	})

	t.Run("alternate bold and italic fonts", func(t *testing.T) {
		baseImg := core.ImageSource{URI: "asset:base.png"}
		boldImg := core.ImageSource{URI: "asset:bold.png"}
		italicImg := core.ImageSource{URI: "asset:italic.png"}
		baseFont, err := text.LoadBMFontImage(baseImg, pixelText)
		if err != nil {
			t.Fatal(err)
		}
		boldFont, err := text.LoadBMFontImage(boldImg, pixelText)
		if err != nil {
			t.Fatal(err)
		}
		italicFont, err := text.LoadBMFontImage(italicImg, pixelText)
		if err != nil {
			t.Fatal(err)
		}

		rt := text.ParseRich("A[b]B[/b][i]C[/i]")
		opts := text.RichOptions{BoldFont: boldFont, ItalicFont: italicFont}
		if sz := text.MeasureRich(baseFont, rt, opts); sz != (core.Size{Width: 30, Height: 16}) {
			t.Fatalf("MeasureRich = %+v, want {30 16}", sz)
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return core.Canvas{TestID: "alt", Style: core.Style{Width: core.Pt(100), Height: core.Pt(50)}, Draw: func(d *core.Drawing, _ core.Size) {
				text.DrawRichLayout(d, text.LayoutRich(baseFont, rt, opts), core.Point{}, opts)
			}}
		})
		ops := app.Find("alt").Ops
		if len(ops) != 3 {
			t.Fatalf("alt font ops = %d, want 3 (no faux-bold/italic extra ops)", len(ops))
		}
		if ops[0].Image.URI != "asset:base.png" || ops[1].Image.URI != "asset:bold.png" || ops[2].Image.URI != "asset:italic.png" {
			t.Fatalf("alt font URIs = (%q, %q, %q)", ops[0].Image.URI, ops[1].Image.URI, ops[2].Image.URI)
		}
	})

	t.Run("platform typewriter partial reveal with wave effect", func(t *testing.T) {
		tw := text.NewTypewriterString("[wave amp=4 freq=1 speed=2]ABC[/wave]", 10)
		tw.Update(0.2) // reveals 'A' (0) and 'B' (1); 'C' (2) stays hidden; elapsed = 0.2.

		style := text.PlatformStyle{
			Font:     core.Font{Family: "sans-serif", Size: 10},
			Color:    core.RGB(0xffffff),
			Baseline: core.BaselineTop,
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return core.Canvas{TestID: "ptw", Style: core.Style{Width: core.Pt(100), Height: core.Pt(50)}, Draw: func(d *core.Drawing, _ core.Size) {
				tw.DrawPlatform(d, core.Point{X: 10, Y: 20}, style, text.RichOptions{})
			}}
		})

		var textOps []core.DrawOp
		for _, op := range app.Find("ptw").Ops {
			if op.Kind == core.DrawFillText {
				textOps = append(textOps, op)
			}
		}
		if len(textOps) != 2 || textOps[0].Text != "A" || textOps[1].Text != "B" {
			t.Fatalf("platform typewriter text ops = %+v, want A and B only", textOps)
		}
		eff := text.GlyphEffect{Kind: text.EffectWave, Amplitude: 4, Frequency: 1, Speed: 2}
		_, dy0, _ := text.EvalGlyphTransform(eff, 0, tw.Elapsed())
		_, dy1, _ := text.EvalGlyphTransform(eff, 1, tw.Elapsed())
		if math.Abs(float64((textOps[1].Y-textOps[0].Y)-(dy1-dy0))) > 1e-4 {
			t.Fatalf("wave delta Y = %v, want %v", textOps[1].Y-textOps[0].Y, dy1-dy0)
		}
	})
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
