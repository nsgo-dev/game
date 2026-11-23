package text_test

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/text"
	"github.com/nsgo-dev/nsgo/core"
)

// The test view is 200x200 points through a 90° camera: at 10 world units an em of 2 world units is 20 points, 1.25 points an atlas pixel.
var testView = core.Bounds{Size: core.Size{Width: 200, Height: 200}}

func cameraAt(pos math3d.Vec3) *scene3d.Camera {
	cam := scene3d.NewPerspectiveCamera(pos, math.Pi/2, 1, 0.1, 1000)
	if pos != (math3d.Vec3{Z: pos.Z}) {
		cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
	}
	return cam
}

func near(a, b, eps float32) bool { return float32(math.Abs(float64(a-b))) <= eps }

func sameAffine(t *testing.T, what string, got, want math2d.Affine, eps float32) {
	t.Helper()
	if !got.ApproxEqual(want, eps) {
		t.Fatalf("%s transform = %+v, want %+v", what, got, want)
	}
}

func layoutWorld(t *testing.T, f *text.SDFFont, cam *scene3d.Camera, wt text.WorldText) text.WorldLayout {
	t.Helper()
	l, ok := f.LayoutWorld(cam, testView, wt)
	if !ok {
		t.Fatalf("%q at %+v is not visible", wt.Text, wt.Pos)
	}
	return l
}

func TestBillboardTextFacesTheCameraAndShrinksWithDistance(t *testing.T) {
	f := loadTestSDF(t)
	label := text.WorldText{Text: "I", Size: 2, AnchorX: 0.5, AnchorY: 0.5}

	l := layoutWorld(t, f, cameraAt(math3d.Vec3{Z: 10}), label)
	if len(l.Glyphs) != 1 || l.Glyphs[0].Glyph.ID != 'I' {
		t.Fatalf("glyphs = %+v, want I", l.Glyphs)
	}
	// "I" is 8 atlas pixels wide and 24 tall, centered on the screen's center; its rectangle starts 4 left of its pen.
	sameAffine(t, "at 10", l.Glyphs[0].Transform, math2d.Affine{A: 1.25, D: 1.25, Tx: 100 - 1.25*8, Ty: 100 - 1.25*12}, 1e-3)
	if !(l.Depth > 0 && l.Depth < 1) {
		t.Fatalf("depth = %v, want within (0, 1)", l.Depth)
	}

	far := layoutWorld(t, f, cameraAt(math3d.Vec3{Z: 20}), label)
	sameAffine(t, "at 20", far.Glyphs[0].Transform, math2d.Affine{A: 0.625, D: 0.625, Tx: 100 - 0.625*8, Ty: 100 - 0.625*12}, 1e-3)
	if far.Depth <= l.Depth {
		t.Fatalf("depth at 20 = %v, not past depth at 10 = %v", far.Depth, l.Depth)
	}

	// Seen from the side, it still faces the camera, unturned on the screen.
	side := layoutWorld(t, f, cameraAt(math3d.Vec3{X: 10}), label)
	sameAffine(t, "from the side", side.Glyphs[0].Transform, l.Glyphs[0].Transform, 1e-3)
	above := layoutWorld(t, f, cameraAt(math3d.Vec3{Y: 6, Z: 8}), label)
	sameAffine(t, "from above", above.Glyphs[0].Transform, l.Glyphs[0].Transform, 1e-3)
}

func TestScreenSizedBillboardsKeepTheirSize(t *testing.T) {
	f := loadTestSDF(t)
	label := text.WorldText{Text: "I", Size: 2, ScreenSize: 24}
	for _, z := range []float32{5, 10, 80} {
		l := layoutWorld(t, f, cameraAt(math3d.Vec3{Z: z}), label)
		// 24 points an em of 16 atlas pixels: 1.5 points an atlas pixel, the top left of the text at the screen's center.
		sameAffine(t, fmt.Sprint("at ", z), l.Glyphs[0].Transform, math2d.Affine{A: 1.5, D: 1.5, Tx: 100 - 1.5*4, Ty: 100}, 1e-3)
	}
}

func TestWorldTextAnchorsItsBox(t *testing.T) {
	f := loadTestSDF(t)
	cam := cameraAt(math3d.Vec3{Z: 10})
	for _, tc := range []struct {
		name   string
		wt     text.WorldText
		wantTx float32
	}{
		// A centered line is placed about x, so its box is too: anchoring its left edge puts the line's left at Pos.
		{"centered line, left anchor", text.WorldText{Options: text.SDFOptions{Align: text.AlignCenter}}, 100 - 1.25*4},
		// A wrapping box is [0, MaxWidth]: 3.2 world units are 25.6 atlas pixels, the line centered within them.
		{"wrapping box, right anchor", text.WorldText{AnchorX: 1, Options: text.SDFOptions{Align: text.AlignCenter, MaxWidth: 3.2}}, 100 + 1.25*(-25.6+(25.6-8)/2-4)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wt := tc.wt
			wt.Text, wt.Size = "I", 2
			l := layoutWorld(t, f, cam, wt)
			if got := l.Glyphs[0].Transform.Tx; !near(got, tc.wantTx, 1e-3) {
				t.Fatalf("glyph starts at x %v, want %v", got, tc.wantTx)
			}
		})
	}
}

func TestFixedTextLiesInItsPlane(t *testing.T) {
	f := loadTestSDF(t)
	cam := cameraAt(math3d.Vec3{Z: 10})
	label := text.WorldText{Text: "I", Size: 2, AnchorX: 0.5, AnchorY: 0.5, Facing: text.FacingFixed}

	// Facing the camera square on, fixed text is drawn as a billboard is.
	square := layoutWorld(t, f, cam, label)
	sameAffine(t, "square on", square.Glyphs[0].Transform, math2d.Affine{A: 1.25, D: 1.25, Tx: 100 - 1.25*8, Ty: 100 - 1.25*12}, 1e-3)

	// Turned 60° about Y, it is half as wide on the screen and as tall.
	label.Rot = math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(60))
	turned := layoutWorld(t, f, cam, label).Glyphs[0].Transform
	if !near(turned.B, 0, 1e-4) || !near(turned.C, 0, 1e-4) {
		t.Fatalf("turned about Y, the text shears: %+v", turned)
	}
	if !near(turned.D, 1.25, 0.05) || !near(turned.A/turned.D, 0.5, 0.03) {
		t.Fatalf("turned 60°: %+v, want width/height about 0.5 and height about 1.25", turned)
	}

	// Laid on the ground and seen from above at 45°, it is foreshortened along its height and stays upright.
	ground := label
	ground.Rot = math3d.QuatFromAxisAngle(math3d.Vec3Right(), -math.Pi/2)
	flat := layoutWorld(t, f, cameraAt(math3d.Vec3{Y: 10, Z: 10}), ground).Glyphs[0].Transform
	if !(flat.A > 0 && flat.D > 0) || !near(flat.D/flat.A, float32(math.Sqrt2/2), 0.05) {
		t.Fatalf("on the ground: %+v, want upright with height/width about 0.71", flat)
	}
}

func TestFixedTextSeenFromBehindIsMirroredOrCulled(t *testing.T) {
	f := loadTestSDF(t)
	cam := cameraAt(math3d.Vec3{Z: 10})
	back := text.WorldText{Text: "I", Size: 2, Facing: text.FacingFixed, Rot: math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi)}
	m := layoutWorld(t, f, cam, back).Glyphs[0].Transform
	if det := m.A*m.D - m.B*m.C; det >= 0 {
		t.Fatalf("seen from behind: %+v, want a mirrored transform", m)
	}
	back.CullBackFace = true
	if _, ok := f.LayoutWorld(cam, testView, back); ok {
		t.Fatal("culled text seen from behind is visible")
	}
	back.Rot = math3d.Quat{}
	if _, ok := f.LayoutWorld(cam, testView, back); !ok {
		t.Fatal("culled text seen from the front is hidden")
	}
	ortho := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 10}, 20, 1, 0.1, 100)
	if _, ok := f.LayoutWorld(ortho, testView, back); !ok {
		t.Fatal("culled text seen from the front by an orthographic camera is hidden")
	}
}

func TestWorldTextOutOfViewIsHidden(t *testing.T) {
	f := loadTestSDF(t)
	cam := cameraAt(math3d.Vec3{Z: 10})
	for _, tc := range []struct {
		name string
		wt   text.WorldText
	}{
		{"billboard behind the camera", text.WorldText{Pos: math3d.Vec3{Z: 20}}},
		{"billboard past the far plane", text.WorldText{Pos: math3d.Vec3{Z: -2000}}},
		{"billboard beside the view", text.WorldText{Pos: math3d.Vec3{X: 100}}},
		{"fixed behind the camera", text.WorldText{Pos: math3d.Vec3{Z: 10.5}, Facing: text.FacingFixed}},
		{"no size", text.WorldText{Size: -1}},
		{"no text", text.WorldText{Text: "-"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wt := tc.wt
			if wt.Text == "" {
				wt.Text = "IO"
			} else if wt.Text == "-" {
				wt.Text = ""
			}
			if wt.Size == 0 {
				wt.Size = 2
			}
			if l, ok := f.LayoutWorld(cam, testView, wt); ok || len(l.Glyphs) != 0 {
				t.Fatalf("visible with %d glyphs", len(l.Glyphs))
			}
			d := &core.Drawing{}
			f.DrawWorld(d, cam, testView, wt)
			if len(d.Ops) != 0 {
				t.Fatalf("drew %d ops", len(d.Ops))
			}
		})
	}
	// Of a sign the camera stands in front of, only the glyphs in view are kept.
	sign := text.WorldText{Text: "IIIIIIIIII", Size: 2, Pos: math3d.Vec3{X: 5}, Facing: text.FacingFixed}
	l := layoutWorld(t, f, cam, sign)
	if len(l.Glyphs) == 0 || len(l.Glyphs) == 10 {
		t.Fatalf("kept %d of 10 glyphs, want only those in view", len(l.Glyphs))
	}
}

func TestFixedTextAnchoredOutOfViewTakesItsGlyphsDepth(t *testing.T) {
	f := loadTestSDF(t)
	cam := cameraAt(math3d.Vec3{Z: 10})
	// A sign 36 world units long on the wall x = 2, starting 2 units behind the camera and running away from it: its anchor is behind the camera, most of its glyphs in view.
	sign := text.WorldText{
		Text: strings.Repeat("I", 36), Size: 2, Pos: math3d.Vec3{X: 2, Z: 12},
		Facing: text.FacingFixed, Rot: math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/2),
	}
	l := layoutWorld(t, f, cam, sign)
	if len(l.Glyphs) == 0 || len(l.Glyphs) == 36 {
		t.Fatalf("kept %d of 36 glyphs, want only those in front of the camera", len(l.Glyphs))
	}
	// Its visible glyphs run from 2.5 to 34 units in front of the camera, so their mean depth lies between labels at 3 and 35.
	nearest := layoutWorld(t, f, cam, text.WorldText{Text: "I", Size: 2, Pos: math3d.Vec3{Z: 7}})
	farthest := layoutWorld(t, f, cam, text.WorldText{Text: "I", Size: 2, Pos: math3d.Vec3{Z: -25}})
	if !(l.Depth > nearest.Depth && l.Depth < farthest.Depth) {
		t.Fatalf("depth = %v, want between %v and %v", l.Depth, nearest.Depth, farthest.Depth)
	}
}

func TestWorldTextStaysSharpAtEveryDistance(t *testing.T) {
	f := loadTestSDF(t)
	for _, facing := range []text.Facing{text.FacingBillboard, text.FacingFixed} {
		for _, z := range []float32{2.5, 5, 10, 20, 40, 80} {
			label := text.WorldText{Text: "I", Size: 2, Facing: facing, AnchorX: 0.5, AnchorY: 0.5, Options: text.SDFOptions{PixelScale: 2}}
			if facing == text.FacingFixed {
				label.Rot = math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(45))
			}
			cam := cameraAt(math3d.Vec3{Z: z})
			l := layoutWorld(t, f, cam, label)
			m := l.Glyphs[0].Transform
			d := &core.Drawing{}
			f.DrawLayout(d, l)
			ops := glyphImages(d)
			if len(ops) != 1 {
				t.Fatalf("%v at %v: %d images", facing, z, len(ops))
			}
			img := ops[0].Image.Pixels
			// The glyph is resolved at the device pixels its atlas pixels cover on average, so a pixel of the image is about a pixel of the screen.
			want := 2 * math.Sqrt(math.Abs(float64(m.A*m.D-m.B*m.C)))
			if got := float64(img.Rect.Dx()) / 12; math.Abs(got/want-1) > 0.03+1/(12*want) {
				t.Fatalf("%v at %v: resolved at %v device pixels an atlas pixel, want %v", facing, z, got, want)
			}
			if partial, _, _ := edgeProfile(img, img.Rect.Dy()/2); partial > 4 {
				t.Fatalf("%v at %v: %d soft pixels across the bar, want at most 4", facing, z, partial)
			}
		}
	}
}

func TestDrawWorldDrawsBillboardsPlainAndTurnedTextTransformed(t *testing.T) {
	f := loadTestSDF(t)
	cam := cameraAt(math3d.Vec3{Z: 10})
	d := &core.Drawing{}
	f.DrawWorld(d, cam, testView, text.WorldText{Text: "IO", Size: 2})
	if len(d.Ops) != 2 || d.Ops[0].Kind != core.DrawImage || d.Ops[1].Kind != core.DrawImage {
		t.Fatalf("billboard ops = %+v, want two plain images", d.Ops)
	}
	d = &core.Drawing{}
	f.DrawWorld(d, cam, testView, text.WorldText{Text: "I", Size: 2, Facing: text.FacingFixed, Rot: math3d.QuatFromAxisAngle(math3d.Vec3UnitZ(), 0.5), Options: text.SDFOptions{Alpha: 0.5}})
	kinds := make([]core.DrawKind, len(d.Ops))
	for i, op := range d.Ops {
		kinds[i] = op.Kind
	}
	want := []core.DrawKind{core.DrawSave, core.DrawAlpha, core.DrawSave, core.DrawTransform, core.DrawImage, core.DrawRestore, core.DrawRestore}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("turned ops = %v, want %v", kinds, want)
	}
}

func BenchmarkSDFResolveGlyph(b *testing.B) {
	f := loadTestSDF(b)
	d := &core.Drawing{}
	opts := text.SDFOptions{Size: 64, Outline: 0.08, OutlineColor: core.RGB(0x000000)}
	b.ReportAllocs()
	for b.Loop() {
		f.ClearCache()
		d.Ops = d.Ops[:0]
		f.Draw(d, "O", 0, 0, opts)
	}
}

func BenchmarkSDFDrawWorldCached(b *testing.B) {
	f := loadTestSDF(b)
	cam := cameraAt(math3d.Vec3{Y: 3, Z: 10})
	d := &core.Drawing{}
	labels := []text.WorldText{
		{Text: "IO IO", Size: 1, Pos: math3d.Vec3{X: -2}, AnchorX: 0.5, AnchorY: 1},
		{Text: "OI", Size: 1, Pos: math3d.Vec3{X: 2}, AnchorX: 0.5, AnchorY: 1},
		{Text: "IOI", Size: 1, Facing: text.FacingFixed, Rot: math3d.QuatFromAxisAngle(math3d.Vec3Up(), 0.5)},
	}
	b.ReportAllocs()
	for b.Loop() {
		d.Ops = d.Ops[:0]
		for _, l := range labels {
			f.DrawWorld(d, cam, testView, l)
		}
	}
}

func FuzzLoadSDFFont(f *testing.F) {
	for _, seed := range [][]byte{sdfAtlasJSON, sdfAtlasTopJSON, sdfBMFontText, sdfBMFontXML, sdfBMFontJSON} {
		f.Add(seed)
	}
	atlas := testAtlas(false)
	cam := cameraAt(math3d.Vec3{Y: 2, Z: 6})
	f.Fuzz(func(t *testing.T, data []byte) {
		font, err := text.LoadSDFFont(data, func(int, string) (image.Image, error) { return atlas, nil })
		if err != nil {
			return
		}
		if !(font.Size >= 1 && font.Size <= 256) {
			// Drawing is what the rest exercises; an em far from the atlas's pixels only resolves the same glyphs larger, up to 1024 pixels a side, which slows the fuzzer without reaching new code.
			return
		}
		font.CacheBytes = 1 << 20
		d := &core.Drawing{}
		font.Draw(d, "IO O\nI", 3, 4, text.SDFOptions{Size: 24, Outline: 0.1, OutlineColor: core.RGB(0), MaxWidth: 40})
		font.DrawWorld(d, cam, testView, text.WorldText{Text: "IO", Size: 1, Facing: text.FacingFixed, Rot: math3d.QuatFromAxisAngle(math3d.Vec3Up(), 0.7)})
		font.DrawWorld(d, cam, testView, text.WorldText{Text: "OI", Size: 1})
	})
}

func ExampleSDFFont_DrawWorld() {
	// Load an msdf-atlas-gen atlas (its JSON and its decoded PNG) once.
	font, err := text.LoadSDFFontImage(testAtlas(false), sdfAtlasJSON)
	if err != nil {
		panic(err)
	}
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Y: 2, Z: 8}, math3d.DegToRad(60), 16.0/9, 0.1, 100)
	cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
	viewport := core.Bounds{Size: core.Size{Width: 640, Height: 360}}

	// In a core.Canvas's Draw: a damage number over an enemy's head, and a sign on a wall.
	d := &core.Drawing{}
	font.DrawWorld(d, cam, viewport, text.WorldText{
		Text: "IO", Pos: math3d.Vec3{Y: 1.5}, Size: 0.5, AnchorX: 0.5, AnchorY: 1,
		Options: text.SDFOptions{Color: core.RGB(0xffd23f), Outline: 0.08, OutlineColor: core.RGB(0x000000), PixelScale: 2},
	})
	font.DrawWorld(d, cam, viewport, text.WorldText{
		Text: "OI", Pos: math3d.Vec3{X: 2, Y: 1}, Size: 0.4, Facing: text.FacingFixed,
		Rot: math3d.QuatFromAxisAngle(math3d.Vec3Up(), -math.Pi/4), CullBackFace: true,
	})
	fmt.Println(len(glyphImages(d)), "glyphs drawn")
	// Output: 4 glyphs drawn
}
