package text_test

import (
	_ "embed"
	"image"
	"image/color"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/text"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	//go:embed testdata/sdf-atlas.json
	sdfAtlasJSON []byte
	// sdf-atlas-top.json is sdf-atlas.json as msdf-atlas-gen writes it with -yorigin top: atlasBounds measured down from the atlas's top, and planeBounds and the metrics negated.
	//go:embed testdata/sdf-atlas-top.json
	sdfAtlasTopJSON []byte
	//go:embed testdata/sdf-bmfont.fnt
	sdfBMFontText []byte
	//go:embed testdata/sdf-bmfont-xml.fnt
	sdfBMFontXML []byte
	//go:embed testdata/sdf-bmfont.json
	sdfBMFontJSON []byte
)

// The test atlas is 64x32 atlas pixels at 16 pixels an em with a distance range of 4: 'I' is a bar 4 pixels wide and 16 tall in the 12x24 rectangle at (0, 0), 'O' a disk of radius 8 in the 20x24 rectangle at (16, 0).
const (
	atlasRange   = 4
	barLeft      = 4
	barWidth     = 4
	barTop       = 4
	barHeight    = 16
	diskCenterX  = 26
	diskCenterY  = 12
	diskRadius   = 8
	testFontSize = 16
)

// signedDistance returns the distance from atlas point (x, y) to the test glyphs' outlines, positive inside, and whether the point lies in a glyph's rectangle.
func signedDistance(x, y float64) (float64, bool) {
	switch {
	case x < 12 && y < 24:
		// Distance to an axis-aligned box: inside, the nearest side; outside, the nearest point.
		cx, cy := float64(barLeft)+barWidth/2.0, float64(barTop)+barHeight/2.0
		dx := math.Abs(x-cx) - barWidth/2.0
		dy := math.Abs(y-cy) - barHeight/2.0
		outside := math.Hypot(math.Max(dx, 0), math.Max(dy, 0))
		inside := math.Min(math.Max(dx, dy), 0)
		return -(outside + inside), true
	case x >= 16 && x < 36 && y < 24:
		return diskRadius - math.Hypot(x-diskCenterX, y-diskCenterY), true
	}
	return 0, false
}

// testAtlas returns the test atlas as msdf-atlas-gen would write it, the same distance in red, green and blue (a multi-channel field whose channels agree is a true one); gray returns it as a one-channel image instead.
func testAtlas(gray bool) image.Image {
	return testAtlasRange(gray, -atlasRange/2.0)
}

// testAtlasRange returns the test atlas with its field mapped from lower atlas pixels (0) to lower+atlasRange (1), as msdfgen maps a range: -atlasRange/2 is the symmetric range, whose middle is the outline.
func testAtlasRange(gray bool, lower float64) image.Image {
	rect := image.Rect(0, 0, 64, 32)
	g := image.NewGray(rect)
	n := image.NewNRGBA(rect)
	for y := range 32 {
		for x := range 64 {
			var v uint8
			if sd, ok := signedDistance(float64(x)+0.5, float64(y)+0.5); ok {
				v = uint8(math.Round(math.Min(math.Max((sd-lower)/atlasRange, 0), 1) * 255))
			}
			g.SetGray(x, y, color.Gray{Y: v})
			n.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 0xff})
		}
	}
	if gray {
		return g
	}
	return n
}

func loadTestSDF(t testing.TB) *text.SDFFont {
	t.Helper()
	f, err := text.LoadSDFFontImage(testAtlas(false), sdfAtlasJSON)
	if err != nil {
		t.Fatalf("LoadSDFFontImage: %v", err)
	}
	return f
}

func TestSDFFontLoadsFromEveryGeneratorsFormatAlike(t *testing.T) {
	atlas := testAtlas(false)
	var files []string
	load := func(data []byte) *text.SDFFont {
		t.Helper()
		f, err := text.LoadSDFFont(data, func(id int, file string) (image.Image, error) {
			if id != 0 {
				t.Fatalf("page id %d, want 0", id)
			}
			files = append(files, file)
			return atlas, nil
		})
		if err != nil {
			t.Fatalf("LoadSDFFont: %v", err)
		}
		return f
	}
	want := load(sdfAtlasJSON)
	if want.Face != "Test SDF" || want.Size != 16 || want.LineHeight != 24 || want.Base != 20 || want.Field != text.FieldMSDF || want.DistanceRange != 4 {
		t.Fatalf("header = (%q, %v, %v, %v, %v, %v)", want.Face, want.Size, want.LineHeight, want.Base, want.Field, want.DistanceRange)
	}
	wantI := text.Glyph{ID: 'I', XOffset: -4, YOffset: 0, XAdvance: 8}
	wantI.Frame.Name, wantI.Frame.W, wantI.Frame.H, wantI.Frame.Width, wantI.Frame.Height = "I", 12, 24, 12, 24
	if got := want.Glyphs['I']; got != wantI {
		t.Fatalf("glyph I = %+v, want %+v", got, wantI)
	}
	if got := want.Glyphs[' ']; got.XAdvance != 4 || got.Frame.W != 0 {
		t.Fatalf("space = %+v, want an advance of 4 and no rectangle", got)
	}
	if got := want.Kerning('I', 'O'); got != -1 {
		t.Fatalf("kerning(I, O) = %v, want -1", got)
	}
	for name, data := range map[string][]byte{"bmfont text": sdfBMFontText, "bmfont xml": sdfBMFontXML, "bmfont json": sdfBMFontJSON} {
		got := load(data)
		if got.Face != want.Face || got.Size != want.Size || got.LineHeight != want.LineHeight || got.Base != want.Base ||
			got.Field != want.Field || got.DistanceRange != want.DistanceRange ||
			!reflect.DeepEqual(got.Glyphs, want.Glyphs) || !reflect.DeepEqual(got.Kernings, want.Kernings) {
			t.Fatalf("%s font differs from msdf-atlas-gen's:\n got %+v\nwant %+v", name, got, want)
		}
	}
	if wantFiles := []string{"", "sdf-atlas.png", "sdf-atlas.png", "sdf-atlas.png"}; !reflect.DeepEqual(files, wantFiles) {
		t.Fatalf("page files = %q, want %q", files, wantFiles)
	}
}

func TestBMFontAcceptsADistanceFieldRecord(t *testing.T) {
	f, err := text.LoadBMFontImage(core.ImageSource{URI: "asset:sdf-atlas.png"}, sdfBMFontText)
	if err != nil {
		t.Fatalf("LoadBMFontImage: %v", err)
	}
	if f.LineHeight != 24 || len(f.Glyphs) != 3 {
		t.Fatalf("font = %+v", f)
	}
}

func TestLoadSDFFontRefusesWhatItCannotDraw(t *testing.T) {
	atlas := testAtlas(false)
	replace := func(old, new string) string {
		if !strings.Contains(string(sdfAtlasJSON), old) {
			t.Fatalf("test atlas has no %q", old)
		}
		return strings.Replace(string(sdfAtlasJSON), old, new, 1)
	}
	for _, tc := range []struct {
		name, data, want string
	}{
		{"empty", " ", "empty"},
		{"bitmap bmfont", string(pixelText), "missing distanceField"},
		{"unknown field type", strings.Replace(string(sdfBMFontText), "fieldType=msdf", "fieldType=bitmap", 1), "unknown distance field type"},
		{"no distance range", strings.Replace(string(sdfBMFontText), "distanceRange=4", "distanceRange=0", 1), "distanceRange"},
		{"duplicate distance field", string(sdfBMFontText) + "distanceField fieldType=sdf distanceRange=2\n", "duplicate distanceField"},
		{"char outside page", strings.Replace(string(sdfBMFontText), "x=16 y=0 width=20", "x=56 y=0 width=20", 1), "outside page 0"},
		{"neither json", `{"glyphs": []}`, "neither"},
		{"hardmask", replace(`"type": "msdf"`, `"type": "hardmask"`), "not a distance field"},
		{"no range", replace(`"distanceRange": 4,`, ``), "distanceRange must be positive"},
		{"range middle past the range", replace(`"distanceRangeMiddle": 0`, `"distanceRangeMiddle": 2`), "distanceRangeMiddle 2 must lie within"},
		{"zero size", replace(`"size": 16`, `"size": 0`), "size 0 must be positive"},
		{"wrong image size", replace(`"width": 64`, `"width": 128`), "image is 64x32"},
		{"bad y origin", replace(`"yOrigin": "bottom"`, `"yOrigin": "middle"`), "yOrigin"},
		{"no line height", replace(`"lineHeight": 1.5`, `"lineHeight": 0`), "lineHeight"},
		{"glyph index", replace(`{"unicode": 32,`, `{"index": 3,`), "-charset"},
		{"duplicate glyph", replace(`{"unicode": 79,`, `{"unicode": 73,`), "duplicate glyph 73"},
		{"outside atlas", replace(`"right": 36, "top": 32`, `"right": 66, "top": 32`), "outside the 64x32 atlas"},
		{"half bounds", replace(`"advance": 1, "planeBounds": {"left": -0.25, "bottom": -0.25, "right": 1, "top": 1.25}, `, `"advance": 1, `), "only one of"},
		{"variants", replace(`"name": "Test SDF",`, `"name": "Test SDF", "variants": [],`), "variants"},
		{"kerning without pair", replace(`"unicode2": 79, `, ``), "kerning 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := text.LoadSDFFontImage(atlas, []byte(tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
	if _, err := text.LoadSDFFont(sdfAtlasJSON, nil); err == nil {
		t.Fatal("a nil page loader loaded")
	}
	if _, err := text.LoadSDFFontImage(nil, sdfAtlasJSON); err == nil || !strings.Contains(err.Error(), "no image") {
		t.Fatalf("nil image error = %v", err)
	}
}

// glyphImages returns the images d draws, in order.
func glyphImages(d *core.Drawing) []core.DrawOp {
	var ops []core.DrawOp
	for _, op := range d.Ops {
		if op.Kind == core.DrawImage {
			ops = append(ops, op)
		}
	}
	return ops
}

// coverage returns the alpha of img's pixel (x, y) from 0 to 1.
func coverage(img *image.RGBA, x, y int) float64 {
	return float64(img.RGBAAt(x, y).A) / 255
}

// edgeProfile measures img's row y: how many pixels are neither empty nor full, the summed coverage in pixels, and where the coverage starts (its first pixel's left edge plus the missing part of it).
func edgeProfile(img *image.RGBA, y int) (partial int, total, left float64) {
	left = -1
	for x := range img.Rect.Dx() {
		c := coverage(img, x, y)
		if c > 0.03 && c < 0.97 {
			partial++
		}
		if c > 0 && left < 0 {
			left = float64(x) + 1 - c
		}
		total += c
	}
	return partial, total, left
}

func TestSDFGlyphEdgesStaySharpAcrossScales(t *testing.T) {
	f := loadTestSDF(t)
	for _, size := range []float32{8, 16, 24, 32, 64, 128, 256, 512} {
		for _, pixelScale := range []float32{1, 2} {
			if size*pixelScale > 512 {
				// Past 1024 pixels a glyph's image stops growing (and is stretched), which this test is not about.
				continue
			}
			d := &core.Drawing{}
			f.Draw(d, "I", 0, 0, text.SDFOptions{Size: size, PixelScale: pixelScale})
			ops := glyphImages(d)
			if len(ops) != 1 {
				t.Fatalf("size %v: %d images drawn, want 1", size, len(ops))
			}
			img := ops[0].Image.Pixels
			// s is the device pixels an atlas pixel covers.
			s := float64(size/testFontSize) * float64(pixelScale)
			// The image is resolved at a scale within 2^(1/32) of s and spans the glyph's 12x24 rectangle in points.
			q := float64(img.Rect.Dx()) / 12
			if math.Abs(q/s-1) > 0.03+1/(12*s) {
				t.Fatalf("size %v scale %v: resolved %d pixels wide, want about %v", size, pixelScale, img.Rect.Dx(), 12*s)
			}
			// The image is whole pixels, so it may reach up to a pixel past the glyph's rectangle.
			em := float64(size) / testFontSize
			if w := float64(ops[0].W); w < 12*em-1e-3 || w > 12*em+1.05/float64(pixelScale) {
				t.Fatalf("size %v: drawn %v points wide, want %v", size, w, 12*em)
			}
			mid := img.Rect.Dy() / 2
			partial, total, left := edgeProfile(img, mid)
			// A sharp edge is at most a pixel or two wide, whatever the scale; a stretched bitmap's would be s pixels wide.
			if partial > 4 {
				t.Fatalf("size %v scale %v: %d soft pixels across the bar, want at most 4 (2 per edge)", size, pixelScale, partial)
			}
			// q is the scale the image was resolved at: its pixels over the points it is drawn across, in atlas pixels.
			q = float64(img.Rect.Dx()) * em / float64(ops[0].W)
			if math.Abs(total-barWidth*q) > 0.6 {
				t.Fatalf("size %v scale %v: bar covers %.2f pixels, want %.2f", size, pixelScale, total, barWidth*q)
			}
			if math.Abs(left-barLeft*q) > 0.6 {
				t.Fatalf("size %v scale %v: bar starts at %.2f, want %.2f", size, pixelScale, left, barLeft*q)
			}
		}
	}
}

func TestSDFDiskStaysRoundWhenEnlarged(t *testing.T) {
	f := loadTestSDF(t)
	d := &core.Drawing{}
	f.Draw(d, "O", 0, 0, text.SDFOptions{Size: 256})
	img := glyphImages(d)[0].Image.Pixels
	q := float64(img.Rect.Dx()) / 20
	// Every pixel's coverage is within a pixel's width of the true disk's: inside by more than a pixel full, outside by more than a pixel empty.
	for y := range img.Rect.Dy() {
		for x := range img.Rect.Dx() {
			ax, ay := (float64(x)+0.5)/q+16, (float64(y)+0.5)/q
			dist := (diskRadius - math.Hypot(ax-diskCenterX, ay-diskCenterY)) * q
			c := coverage(img, x, y)
			if dist > 1 && c < 0.99 || dist < -1 && c > 0.01 {
				t.Fatalf("pixel (%d, %d) %.2f pixels from the outline has coverage %.2f", x, y, dist, c)
			}
		}
	}
}

func TestSDFDrawColorsOutlinesAndFades(t *testing.T) {
	f := loadTestSDF(t)
	d := &core.Drawing{}
	f.Draw(d, "I", 10, 20, text.SDFOptions{Size: 32, Color: core.RGB(0x00ff00), Outline: 0.1, OutlineColor: core.RGB(0xff0000), Alpha: 0.5})
	if len(d.Ops) != 4 || d.Ops[0].Kind != core.DrawSave || d.Ops[1].Kind != core.DrawAlpha || d.Ops[1].A != 0.5 || d.Ops[3].Kind != core.DrawRestore {
		t.Fatalf("ops = %+v, want Save, SetAlpha(0.5), DrawImage, Restore", d.Ops)
	}
	op := d.Ops[2]
	// I's rectangle starts 4 atlas pixels (8 points at size 32) left of the pen.
	if op.X != 2 || op.Y != 20 || op.W != 24 || op.H != 48 {
		t.Fatalf("drawn at (%v, %v) %vx%v, want (2, 20) 24x48", op.X, op.Y, op.W, op.H)
	}
	img := op.Image.Pixels
	mid := img.Rect.Dy() / 2
	// The bar spans pixels 8 to 16 at scale 2; its outline, 0.1 em = 3.2 pixels, reaches to about 4.8.
	if c := img.RGBAAt(12, mid); c != (color.RGBA{G: 0xff, A: 0xff}) {
		t.Fatalf("bar center = %v, want opaque green", c)
	}
	if c := img.RGBAAt(6, mid); c != (color.RGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("outline = %v, want opaque red", c)
	}
	if c := img.RGBAAt(1, mid); c.A != 0 {
		t.Fatalf("outside the outline = %v, want transparent", c)
	}
}

func TestSDFFontCachesResolvedGlyphs(t *testing.T) {
	f := loadTestSDF(t)
	draw := func(size, alpha float32) *image.RGBA {
		d := &core.Drawing{}
		f.Draw(d, "I", 0, 0, text.SDFOptions{Size: size, Alpha: alpha})
		return glyphImages(d)[0].Image.Pixels
	}
	a := draw(32, 1)
	if b := draw(32.2, 0.3); b != a {
		t.Fatal("a size within the same step and another alpha resolved the glyph again")
	}
	if n, _ := f.CachedGlyphs(); n != 1 {
		t.Fatalf("cached %d glyphs, want 1", n)
	}
	if c := draw(48, 1); c == a {
		t.Fatal("a size 1.5 times larger reused the smaller glyph")
	}
	f.CacheBytes = 1
	draw(64, 1)
	if n, bytes := f.CachedGlyphs(); n != 1 || bytes == 0 {
		t.Fatalf("over its bound the cache keeps %d glyphs (%d bytes), want only the last", n, bytes)
	}
	f.ClearCache()
	if n, bytes := f.CachedGlyphs(); n != 0 || bytes != 0 {
		t.Fatalf("cleared cache keeps %d glyphs (%d bytes)", n, bytes)
	}
}

func TestSDFFontMeasuresAndLaysOutLikeABitmapFont(t *testing.T) {
	f := loadTestSDF(t)
	// "IO" at 32 points: I advances 16, kerning -2, O advances 32.
	if got := f.Measure("IO", text.SDFOptions{Size: 32}); got != (core.Size{Width: 46, Height: 48}) {
		t.Fatalf("Measure = %+v, want 46x48", got)
	}
	got := f.Layout("I O", 5, 7, text.SDFOptions{Size: 32, LineSpacing: 0.5})
	if len(got) != 2 || got[0].X != -3 || got[0].Y != 7 || got[1].X != 5+16+8-8 {
		t.Fatalf("Layout = %+v", got)
	}
	lines := f.Layout("I\nI", 0, 0, text.SDFOptions{Size: 32, LineSpacing: 0.5})
	if len(lines) != 2 || lines[1].Y != 48+16 {
		t.Fatalf("second line at %+v, want y 64 (a line of 48 and half an em of spacing)", lines)
	}
	if f.Measure("I", text.SDFOptions{Size: -1}) != (core.Size{}) || f.Layout("I", 0, 0, text.SDFOptions{Size: -1}) != nil {
		t.Fatal("a negative size measured or laid out something")
	}
	d := &core.Drawing{}
	f.Draw(d, "I", 0, 0, text.SDFOptions{Alpha: -1})
	f.Draw(d, "", 0, 0, text.SDFOptions{})
	var nilFont *text.SDFFont
	nilFont.Draw(d, "I", 0, 0, text.SDFOptions{})
	if len(d.Ops) != 0 {
		t.Fatalf("drew %d ops with nothing to draw", len(d.Ops))
	}
}

func TestSDFFontReadsOneChannelAtlases(t *testing.T) {
	data := strings.Replace(string(sdfAtlasJSON), `"type": "msdf"`, `"type": "sdf"`, 1)
	gray, err := text.LoadSDFFontImage(testAtlas(true), []byte(data))
	if err != nil {
		t.Fatalf("LoadSDFFontImage: %v", err)
	}
	msdf := loadTestSDF(t)
	draw := func(f *text.SDFFont) *image.RGBA {
		d := &core.Drawing{}
		f.Draw(d, "O", 0, 0, text.SDFOptions{Size: 40})
		return glyphImages(d)[0].Image.Pixels
	}
	if a, b := draw(gray), draw(msdf); !reflect.DeepEqual(a.Pix, b.Pix) {
		t.Fatal("a gray sdf atlas and an msdf atlas of equal channels resolve differently")
	}
}

func TestSDFMultiChannelFieldsTakeTheMedian(t *testing.T) {
	// field returns a 4x4 atlas of one texel value repeated.
	field := func(c color.NRGBA) image.Image {
		img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for y := range 4 {
			for x := range 4 {
				img.SetNRGBA(x, y, c)
			}
		}
		return img
	}
	// A top-origin atlas, as msdf-atlas-gen writes one: y measured down, so the ascender and the plane's top are negative.
	src := `{"atlas": {"type": "%s", "distanceRange": 2, "size": 4, "width": 4, "height": 4, "yOrigin": "top"},
		"metrics": {"lineHeight": 1, "ascender": -1},
		"glyphs": [{"unicode": 65, "advance": 1, "planeBounds": {"left": 0, "top": -1, "right": 1, "bottom": 0}, "atlasBounds": {"left": 0, "top": 0, "right": 4, "bottom": 4}}]}`
	white, red := color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, color.RGBA{R: 0xff, A: 0xff}
	for _, tc := range []struct {
		name  string
		field string
		texel color.NRGBA
		want  color.RGBA
	}{
		// Two of three channels inside: the median is inside, whatever alpha says.
		{"msdf inside", "msdf", color.NRGBA{R: 0xff, G: 0xff}, white},
		{"mtsdf inside", "mtsdf", color.NRGBA{R: 0xff, G: 0xff}, white},
		// Two of three channels outside: no fill; only a true field in alpha draws the outline there.
		{"msdf outside", "msdf", color.NRGBA{B: 0xff, A: 0xff}, color.RGBA{}},
		{"mtsdf outline from alpha", "mtsdf", color.NRGBA{B: 0xff, A: 0xff}, red},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := text.LoadSDFFontImage(field(tc.texel), []byte(strings.Replace(src, "%s", tc.field, 1)))
			if err != nil {
				t.Fatal(err)
			}
			d := &core.Drawing{}
			f.Draw(d, "A", 0, 0, text.SDFOptions{Outline: 0.5, OutlineColor: core.RGB(0xff0000)})
			if got := glyphImages(d)[0].Image.Pixels.RGBAAt(1, 1); got != tc.want {
				t.Fatalf("pixel = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSDFFontReadsTopOriginAtlases(t *testing.T) {
	bottom := loadTestSDF(t)
	top, err := text.LoadSDFFontImage(testAtlas(false), sdfAtlasTopJSON)
	if err != nil {
		t.Fatalf("LoadSDFFontImage: %v", err)
	}
	if top.LineHeight != bottom.LineHeight || top.Base != bottom.Base || !reflect.DeepEqual(top.Glyphs, bottom.Glyphs) || !reflect.DeepEqual(top.Kernings, bottom.Kernings) {
		t.Fatalf("top-origin font differs from the bottom-origin one:\n got %+v\nwant %+v", top, bottom)
	}
	for _, r := range []string{"I", "O"} {
		a, b := &core.Drawing{}, &core.Drawing{}
		top.Draw(a, r, 0, 0, text.SDFOptions{Size: 40})
		bottom.Draw(b, r, 0, 0, text.SDFOptions{Size: 40})
		if !reflect.DeepEqual(glyphImages(a)[0].Image.Pixels.Pix, glyphImages(b)[0].Image.Pixels.Pix) {
			t.Fatalf("%s resolves differently from a top-origin atlas", r)
		}
	}
}

// maxAlphaDiff returns the largest difference between the alphas of two images of one size.
func maxAlphaDiff(a, b *image.RGBA) int {
	var most int
	for i := 3; i < len(a.Pix); i += 4 {
		most = max(most, int(a.Pix[i])-int(b.Pix[i]), int(b.Pix[i])-int(a.Pix[i]))
	}
	return most
}

func TestSDFFontThresholdsAnAsymmetricRangeAtItsOutline(t *testing.T) {
	// msdf-atlas-gen -apxrange -1 3: the band reaches 1 atlas pixel outside the outline and 3 inside, its middle 1 pixel inside.
	asym := testAtlasRange(false, -1)
	withMiddle := strings.Replace(string(sdfAtlasJSON), `"distanceRangeMiddle": 0`, `"distanceRangeMiddle": 1`, 1)
	f, err := text.LoadSDFFontImage(asym, []byte(withMiddle))
	if err != nil {
		t.Fatalf("LoadSDFFontImage: %v", err)
	}
	if f.DistanceRangeMiddle != 1 {
		t.Fatalf("DistanceRangeMiddle = %v, want 1", f.DistanceRangeMiddle)
	}
	unaware, err := text.LoadSDFFontImage(asym, sdfAtlasJSON)
	if err != nil {
		t.Fatalf("LoadSDFFontImage: %v", err)
	}
	draw := func(f *text.SDFFont) *image.RGBA {
		d := &core.Drawing{}
		f.Draw(d, "O", 0, 0, text.SDFOptions{Size: 40})
		return glyphImages(d)[0].Image.Pixels
	}
	want := draw(loadTestSDF(t))
	// The fields differ only in how their values round to bytes, a few hundredths of a pixel.
	if diff := maxAlphaDiff(draw(f), want); diff > 16 {
		t.Fatalf("asymmetric range resolves up to %d/255 away from the symmetric one", diff)
	}
	if diff := maxAlphaDiff(draw(unaware), want); diff < 128 {
		t.Fatalf("ignoring the range's middle moves the outline only %d/255; the test does not tell them apart", diff)
	}
}

func TestSDFFontReadsItsFieldsOnEveryCall(t *testing.T) {
	f := loadTestSDF(t)
	two := f.Measure("I\nI", text.SDFOptions{})
	f.LineHeight *= 2
	if got := f.Measure("I\nI", text.SDFOptions{}); got.Height != 2*two.Height {
		t.Fatalf("after doubling LineHeight two lines measure %v, want %v", got.Height, 2*two.Height)
	}
	kerned := f.Measure("IO", text.SDFOptions{})
	f.Kernings = nil
	if got := f.Measure("IO", text.SDFOptions{}); got.Width != kerned.Width+1 {
		t.Fatalf("without kernings IO measures %v, want %v", got.Width, kerned.Width+1)
	}

	draw := func() *image.RGBA {
		d := &core.Drawing{}
		f.Draw(d, "I", 0, 0, text.SDFOptions{Size: 32})
		return glyphImages(d)[0].Image.Pixels
	}
	sharp := draw()
	f.DistanceRange *= 4
	if soft := draw(); soft == sharp || maxAlphaDiff(soft, sharp) == 0 {
		t.Fatal("a changed DistanceRange drew the glyph resolved before it")
	}
	f.DistanceRange /= 4
	if again := draw(); again != sharp {
		t.Fatal("the restored DistanceRange resolved the glyph again rather than reuse it")
	}
}

func TestSDFFontReadsEveryPage(t *testing.T) {
	// 'O' is on page 1, the test atlas; 'Q' has the same rectangle on page 0, an empty field.
	src := `info face="Two Pages" size=16 bold=0 italic=0 charset="" unicode=1 stretchH=100 smooth=1 aa=1 padding=2,2,2,2 spacing=0,0
common lineHeight=24 base=20 scaleW=64 scaleH=32 pages=2 packed=0 alphaChnl=0 redChnl=0 greenChnl=0 blueChnl=0
page id=0 file="sdf-0.png"
page id=1 file="sdf-1.png"
chars count=2
char id=79 x=16 y=0 width=20 height=24 xoffset=-4 yoffset=0 xadvance=16 page=1 chnl=15
char id=81 x=16 y=0 width=20 height=24 xoffset=-4 yoffset=0 xadvance=16 page=0 chnl=15
distanceField fieldType=msdf distanceRange=4
`
	pages := []image.Image{image.NewNRGBA(image.Rect(0, 0, 64, 32)), testAtlas(false)}
	type call struct {
		id   int
		file string
	}
	var calls []call
	f, err := text.LoadSDFFont([]byte(src), func(id int, file string) (image.Image, error) {
		calls = append(calls, call{id, file})
		return pages[id], nil
	})
	if err != nil {
		t.Fatalf("LoadSDFFont: %v", err)
	}
	if want := []call{{0, "sdf-0.png"}, {1, "sdf-1.png"}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("page calls = %+v, want %+v", calls, want)
	}
	draw := func(f *text.SDFFont, s string) *image.RGBA {
		d := &core.Drawing{}
		f.Draw(d, s, 0, 0, text.SDFOptions{Size: 40})
		return glyphImages(d)[0].Image.Pixels
	}
	if !reflect.DeepEqual(draw(f, "O").Pix, draw(loadTestSDF(t), "O").Pix) {
		t.Fatal("O on page 1 does not resolve from page 1's field")
	}
	q := draw(f, "Q")
	for i := 3; i < len(q.Pix); i += 4 {
		if q.Pix[i] != 0 {
			t.Fatal("Q on page 0 resolves from another page's field")
		}
	}
	if _, err := text.LoadSDFFontImage(testAtlas(false), []byte(src)); err == nil || !strings.Contains(err.Error(), "font has 2 pages; use LoadSDFFont") {
		t.Fatalf("LoadSDFFontImage of a two-page font: error = %v", err)
	}
}
