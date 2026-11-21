package gameui_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestViewportScalingLetterboxAndInput(t *testing.T) {
	// 1. Verify math across all ScaleModes.
	// Virtual 320x180 (16:9) inside 800x600 (4:3):
	// Fit/Letterbox: scale = 800/320 = 2.5, rect = (0, 75, 800, 450), top & bottom bars of height 75.
	vpFit := gameui.NewViewport(320, 180, gameui.ScaleLetterbox).WithWindowSize(800, 600)
	sx, sy := vpFit.ScaleXY()
	assertNear(t, "fit.sx", sx, 2.5)
	assertNear(t, "fit.sy", sy, 2.5)
	assertBounds(t, "fit.Rect", vpFit.Rect(), 0, 75, 800, 450)
	assertBounds(t, "fit.VisibleBounds", vpFit.VisibleBounds(), 0, 75, 800, 450)
	bars := vpFit.LetterboxBars()
	if len(bars) != 2 {
		t.Fatalf("expected 2 letterbox bars (top+bottom), got %d: %+v", len(bars), bars)
	}
	assertBounds(t, "fit.topBar", bars[0], 0, 0, 800, 75)
	assertBounds(t, "fit.bottomBar", bars[1], 0, 525, 800, 75)

	// Pillarbox: 320x240 (4:3) inside 1280x600 -> scale = 2.5, rect = (240, 0, 800, 600), left & right bars of width 240.
	vpPillar := gameui.NewViewport(320, 240, gameui.ScaleFit).WithWindowSize(1280, 600)
	assertBounds(t, "pillar.Rect", vpPillar.Rect(), 240, 0, 800, 600)
	pBars := vpPillar.LetterboxBars()
	if len(pBars) != 2 {
		t.Fatalf("expected 2 pillarbox bars (left+right), got %d: %+v", len(pBars), pBars)
	}
	assertBounds(t, "pillar.leftBar", pBars[0], 0, 0, 240, 600)
	assertBounds(t, "pillar.rightBar", pBars[1], 1040, 0, 240, 600)
	if _, ok := vpPillar.WindowToVirtual(core.Point{X: 100, Y: 300}); ok {
		t.Fatal("left pillarbox click should return ok=false")
	}
	if _, ok := vpPillar.WindowToVirtual(core.Point{X: 1200, Y: 300}); ok {
		t.Fatal("right pillarbox click should return ok=false")
	}

	// Fill: 320x180 inside 800x600 -> scale = 600/180 = 10/3, rect extends horizontally past window.
	vpFill := gameui.NewViewport(320, 180, gameui.ScaleFill).WithWindowSize(800, 600)
	fillS := vpFill.Scale()
	assertNear(t, "fill.Scale", fillS, float32(600)/180)
	assertBounds(t, "fill.VisibleBounds", vpFill.VisibleBounds(), 0, 0, 800, 600)
	if len(vpFill.LetterboxBars()) != 0 {
		t.Fatalf("ScaleFill should have 0 letterbox bars, got %d", len(vpFill.LetterboxBars()))
	}

	// Stretch: 320x180 inside 800x600 -> sx=2.5, sy=10/3, rect = (0, 0, 800, 600).
	vpStretch := gameui.NewViewport(320, 180, gameui.ScaleStretch).WithWindowSize(800, 600)
	stX, stY := vpStretch.ScaleXY()
	assertNear(t, "stretch.sx", stX, 2.5)
	assertNear(t, "stretch.sy", stY, float32(600)/180)
	assertBounds(t, "stretch.Rect", vpStretch.Rect(), 0, 0, 800, 600)

	// IntegerScale: 320x180 inside 800x600 -> floor(2.5) = 2x -> 640x360 centered at (80, 120) with 4 bars!
	vpInt := gameui.NewViewport(320, 180, gameui.IntegerScale).WithWindowSize(800, 600)
	assertNear(t, "int.Scale", vpInt.Scale(), 2)
	assertBounds(t, "int.Rect", vpInt.Rect(), 80, 120, 640, 360)
	intBars := vpInt.LetterboxBars()
	if len(intBars) != 4 {
		t.Fatalf("IntegerScale centered should have 4 bars (top, bottom, left, right), got %d", len(intBars))
	}
	// Verify round-trip VirtualToWindow <-> WindowToVirtual.
	vOrig := core.Point{X: 123.5, Y: 77.25}
	wMapped := vpInt.VirtualToWindow(vOrig)
	vBack, ok := vpInt.WindowToVirtual(wMapped)
	if !ok {
		t.Fatal("round-trip inside integer viewport should return ok=true")
	}
	assertNear(t, "roundtrip.X", float32(vBack.X), float32(vOrig.X))
	assertNear(t, "roundtrip.Y", float32(vBack.Y), float32(vOrig.Y))

	// 2. End-to-end scaled Canvas draw + pointer/touch press hitting exact scaled widgets & ignoring letterbox bars.
	var playClicks int
	var selectedItem string
	playBtn := &gameui.Button{Text: "Play", Height: 40, OnClick: func() { playClicks++ }}
	slider := &gameui.Slider{Min: 0, Max: 100, Value: 0, Height: 30}
	list := &gameui.List{
		Items:      []string{"Stage 1", "Stage 2"},
		ItemHeight: 24,
		Height:     60,
		OnSelect:   func(_ int, item string) { selectedItem = item },
	}

	vp := gameui.Viewport{
		VirtualWidth:   320,
		VirtualHeight:  180,
		ScaleMode:      gameui.ScaleLetterbox,
		LetterboxColor: core.RGB(0x050607),
	}
	root := gameui.NewRoot(&gameui.Panel{
		Padding: core.All(10),
		Child: &gameui.VBox{
			Spacing:  8,
			Children: []gameui.Widget{playBtn, slider, list},
		},
	})
	root.Viewport = &vp

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "scaled-canvas",
			Style:  core.Style{Width: core.Pt(800), Height: core.Pt(600)},
			OnPress: func(ev core.PressEvent) {
				root.OnPress(ev)
			},
			Draw: func(d *core.Drawing, size core.Size) {
				root.Draw(d, core.Bounds{Size: size})
			},
		}
	})

	// Verify DrawOps include letterbox bar fills, clip, translate(0,75), scale(2.5,2.5), and balanced Save/Restore.
	ops := app.Find("scaled-canvas").Ops
	var foundBarColor, foundTranslate, foundScale, foundClip bool
	saves, restores := 0, 0
	for _, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			saves++
		case core.DrawRestore:
			restores++
		case core.DrawFillColor:
			if op.Color == vp.LetterboxColor {
				foundBarColor = true
			}
		case core.DrawClip:
			foundClip = true
		case core.DrawTranslate:
			if math.Abs(float64(op.X)) < 1e-3 && math.Abs(float64(op.Y-75)) < 1e-3 {
				foundTranslate = true
			}
		case core.DrawScale:
			if math.Abs(float64(op.X-2.5)) < 1e-3 && math.Abs(float64(op.Y-2.5)) < 1e-3 {
				foundScale = true
			}
		}
	}
	if !foundBarColor || !foundClip || !foundTranslate || !foundScale {
		t.Fatalf("missing viewport draw ops: barColor=%v clip=%v translate=%v scale=%v", foundBarColor, foundClip, foundTranslate, foundScale)
	}
	if saves == 0 || saves != restores {
		t.Fatalf("unbalanced Save/Restore: saves=%d restores=%d", saves, restores)
	}

	// Clicking in top and bottom letterbox bars must be ignored.
	app.PressAt("scaled-canvas", core.Point{X: 400, Y: 20})
	app.PressAt("scaled-canvas", core.Point{X: 400, Y: 580})
	if playClicks != 0 || selectedItem != "" {
		t.Fatalf("letterbox clicks should be ignored, got playClicks=%d selected=%q", playClicks, selectedItem)
	}
	if root.HitTest(core.Point{X: 400, Y: 10}) != nil {
		t.Fatal("HitTest inside letterbox bar should return nil")
	}

	// Clicking at the exact scaled window position of Play button activates it.
	btnWinPt := vp.VirtualToWindow(centerOf(playBtn.Bounds()))
	app.PressAt("scaled-canvas", btnWinPt)
	if playClicks != 1 {
		t.Fatalf("expected scaled PressAt on Play button to click once, got %d", playClicks)
	}

	// Touch/pointer tap on Stage 2 of the List selects Stage 2.
	item2WinPt := vp.VirtualToWindow(centerOf(list.ItemBounds(1)))
	app.PressAt("scaled-canvas", item2WinPt)
	if selectedItem != "Stage 2" || list.Selected != 1 {
		t.Fatalf("expected scaled PressAt on Stage 2, got selected=%d (%q)", list.Selected, selectedItem)
	}

	// Dragging scaled Slider via physical window coordinates updates value accurately.
	tr := slider.TrackBounds()
	midY := tr.Origin.Y + tr.Size.Height/2
	winStart := vp.VirtualToWindow(core.Point{X: tr.Origin.X + tr.Size.Width*0.25, Y: midY})
	winEnd := vp.VirtualToWindow(core.Point{X: tr.Origin.X + tr.Size.Width*0.75, Y: midY})
	if !root.OnPointerDown(winStart) {
		t.Fatal("scaled OnPointerDown on slider should be consumed")
	}
	assertNear(t, "scaled slider@25%", slider.Value, 25)
	// Drag to 75% even while Y drifts into the top letterbox bar!
	if !root.OnPointerMove(core.Point{X: winEnd.X, Y: 10}) {
		t.Fatal("active slider drag should continue smoothly across letterbox")
	}
	assertNear(t, "scaled slider@75%", slider.Value, 75)
	root.OnPointerUp(winEnd)
	assertNear(t, "scaled slider after up", slider.Value, 75)
}

func TestNineSliceFrameAndBitmapTextDrawer(t *testing.T) {
	img := core.Src("assets/ui_frame.png")
	nsNormal := &gameui.NineSlice{
		Image:  img,
		Src:    core.Bounds{Size: core.Size{Width: 48, Height: 48}},
		Left:   12,
		Top:    12,
		Right:  12,
		Bottom: 12,
	}
	nsHover := &gameui.NineSlice{
		Image:  img,
		Src:    core.Bounds{Origin: core.Point{X: 48}, Size: core.Size{Width: 48, Height: 48}},
		Left:   12,
		Top:    12,
		Right:  12,
		Bottom: 12,
	}
	nsPressed := &gameui.NineSlice{
		Image:  img,
		Src:    core.Bounds{Origin: core.Point{X: 96}, Size: core.Size{Width: 48, Height: 48}},
		Left:   12,
		Top:    12,
		Right:  12,
		Bottom: 12,
	}
	nsDisabled := &gameui.NineSlice{
		Image:  img,
		Src:    core.Bounds{Origin: core.Point{X: 144}, Size: core.Size{Width: 48, Height: 48}},
		Left:   12,
		Top:    12,
		Right:  12,
		Bottom: 12,
	}

	var bitmapTexts []gameui.TextDrawParams
	theme := gameui.DefaultTheme()
	theme.ButtonFrame = gameui.NineSliceStateFrame(nsNormal, nsHover, nsPressed, nsDisabled)
	theme.TextMeasurer = func(text string, _ core.Font) (float32, float32) {
		return float32(len(text)) * 8, 12
	}
	theme.TextDrawer = func(d *core.Drawing, p gameui.TextDrawParams) {
		bitmapTexts = append(bitmapTexts, p)
		// Deliberately mutate state to confirm gameui isolates TextDrawer inside Save/Restore.
		d.SetAlpha(0.25).SetFillColor(p.Color).FillRect(p.X, p.Y, float32(len(p.Text))*8, 12)
	}

	btn := &gameui.Button{Text: "START", Height: 40}
	lbl := &gameui.Label{Text: "TITLE", Height: 20}
	panel := &gameui.Panel{
		Padding:   core.All(8),
		NineSlice: nsNormal,
		Child: &gameui.VBox{
			Spacing:  6,
			Children: []gameui.Widget{lbl, btn},
		},
	}
	root := &gameui.Root{Theme: &theme, Child: panel}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "skinned",
			Style:  core.Style{Width: core.Pt(240), Height: core.Pt(120)},
			Draw: func(d *core.Drawing, size core.Size) {
				root.Draw(d, core.Bounds{Size: size})
			},
		}
	})

	ops := app.Find("skinned").Ops
	drawImageCount := 0
	fillTextCount := 0
	for _, op := range ops {
		switch op.Kind {
		case core.DrawImage:
			drawImageCount++
		case core.DrawFillText:
			fillTextCount++
		}
	}
	// Panel (9 slices) + Button (9 slices) = 18 DrawImage ops, and 0 default FillText ops because TextDrawer handled all text!
	if drawImageCount != 18 {
		t.Fatalf("expected 18 nine-slice DrawImage ops (9 panel + 9 button), got %d", drawImageCount)
	}
	if fillTextCount != 0 {
		t.Fatalf("expected 0 default FillText ops when Theme.TextDrawer is set, got %d", fillTextCount)
	}
	if len(bitmapTexts) != 2 || bitmapTexts[0].Text != "TITLE" || bitmapTexts[1].Text != "START" {
		t.Fatalf("unexpected bitmap text calls: %+v", bitmapTexts)
	}

	// Verify NineSlice corner clamping when destination is smaller than Left+Right / Top+Bottom.
	tinyApp := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "tiny-ns",
			Style:  core.Style{Width: core.Pt(20), Height: core.Pt(20)},
			Draw: func(d *core.Drawing, size core.Size) {
				nsNormal.Draw(d, core.Bounds{Size: core.Size{Width: 16, Height: 10}})
			},
		}
	})
	tinyOps := tinyApp.Find("tiny-ns").Ops
	tinyImages := 0
	for _, op := range tinyOps {
		if op.Kind == core.DrawImage {
			tinyImages++
			if op.W <= 0 || op.H <= 0 {
				t.Fatalf("clamped nine-slice emitted non-positive patch size: %+v", op)
			}
		}
	}
	// Middle row & middle col collapse to 0 width/height, leaving 4 corner patches.
	if tinyImages != 4 {
		t.Fatalf("expected 4 corner DrawImage ops when dst < margins, got %d", tinyImages)
	}
}

// canonicalOpsDigest returns a deterministic SHA-256 hex digest of a recorded DrawOp stream.
func canonicalOpsDigest(ops []core.DrawOp) string {
	var b strings.Builder
	for _, op := range ops {
		fmt.Fprintf(&b, "%d|%.3f,%.3f,%.3f,%.3f,%.3f,%.3f|%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f|%v|%08x|%d|%d|%q|%.2f|%v|%d|%d\n",
			op.Kind,
			op.X, op.Y, op.X1, op.Y1, op.X2, op.Y2,
			op.W, op.H, op.R, op.A, op.A0, op.A1, op.RY,
			op.CCW,
			uint32(op.Color),
			op.Cap, op.Join,
			op.Text,
			op.Font.Size,
			op.EvenOdd,
			op.TextAlign, op.TextBaseline,
		)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// rasterizeOps deterministically paints recorded Fill/Stroke rectangle/roundrect/arc ops into an RGBA image
// so tests can verify both DrawOp determinism and rendered pixel colors across all widget states in headless runs.
func rasterizeOps(w, h int, ops []core.DrawOp) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	type paintState struct {
		fill   core.Color
		stroke core.Color
		lineW  float32
	}
	st := paintState{lineW: 1}
	var stack []paintState
	type rectShape struct {
		x, y, w, h float32
		isArc      bool
		cx, cy, r  float32
	}
	var curPath []rectShape

	toRGBA := func(c core.Color) color.RGBA {
		u := uint32(c)
		return color.RGBA{R: uint8(u >> 24), G: uint8(u >> 16), B: uint8(u >> 8), A: uint8(u)}
	}
	fillPixel := func(x, y int, c color.RGBA) {
		if x >= 0 && x < w && y >= 0 && y < h && c.A > 0 {
			img.SetRGBA(x, y, c)
		}
	}
	paintFill := func(s rectShape, col color.RGBA) {
		if s.isArc {
			x0 := int(math.Floor(float64(s.cx - s.r)))
			y0 := int(math.Floor(float64(s.cy - s.r)))
			x1 := int(math.Ceil(float64(s.cx + s.r)))
			y1 := int(math.Ceil(float64(s.cy + s.r)))
			rr := s.r * s.r
			for py := y0; py <= y1; py++ {
				for px := x0; px <= x1; px++ {
					dx := float32(px) + 0.5 - s.cx
					dy := float32(py) + 0.5 - s.cy
					if dx*dx+dy*dy <= rr {
						fillPixel(px, py, col)
					}
				}
			}
			return
		}
		x0 := int(math.Floor(float64(s.x)))
		y0 := int(math.Floor(float64(s.y)))
		x1 := int(math.Ceil(float64(s.x + s.w)))
		y1 := int(math.Ceil(float64(s.y + s.h)))
		for py := y0; py < y1; py++ {
			for px := x0; px < x1; px++ {
				fillPixel(px, py, col)
			}
		}
	}
	paintStroke := func(s rectShape, col color.RGBA, lw float32) {
		band := max(1, int(math.Ceil(float64(lw))))
		if s.isArc {
			x0 := int(math.Floor(float64(s.cx - s.r - lw)))
			y0 := int(math.Floor(float64(s.cy - s.r - lw)))
			x1 := int(math.Ceil(float64(s.cx + s.r + lw)))
			y1 := int(math.Ceil(float64(s.cy + s.r + lw)))
			for py := y0; py <= y1; py++ {
				for px := x0; px <= x1; px++ {
					dist := float32(math.Hypot(float64(float32(px)+0.5-s.cx), float64(float32(py)+0.5-s.cy)))
					if math.Abs(float64(dist-s.r)) <= float64(lw)*0.75 {
						fillPixel(px, py, col)
					}
				}
			}
			return
		}
		x0 := int(math.Floor(float64(s.x)))
		y0 := int(math.Floor(float64(s.y)))
		x1 := int(math.Ceil(float64(s.x + s.w)))
		y1 := int(math.Ceil(float64(s.y + s.h)))
		for py := y0; py < y1; py++ {
			for px := x0; px < x1; px++ {
				if px < x0+band || px >= x1-band || py < y0+band || py >= y1-band {
					fillPixel(px, py, col)
				}
			}
		}
	}

	for _, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, st)
		case core.DrawRestore:
			if len(stack) > 0 {
				st = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case core.DrawFillColor:
			st.fill = op.Color
		case core.DrawStrokeColor:
			st.stroke = op.Color
		case core.DrawLineWidth:
			st.lineW = op.W
		case core.DrawBeginPath:
			curPath = curPath[:0]
		case core.DrawRect, core.DrawRoundRect:
			curPath = append(curPath, rectShape{x: op.X, y: op.Y, w: op.W, h: op.H})
		case core.DrawArc:
			curPath = append(curPath, rectShape{isArc: true, cx: op.X, cy: op.Y, r: op.R})
		case core.DrawFill:
			col := toRGBA(st.fill)
			for _, s := range curPath {
				paintFill(s, col)
			}
		case core.DrawStroke:
			col := toRGBA(st.stroke)
			for _, s := range curPath {
				paintStroke(s, col, st.lineW)
			}
		}
	}
	return img
}

func pixelDigest(img *image.RGBA) string {
	sum := sha256.Sum256(img.Pix)
	return hex.EncodeToString(sum[:])
}

func TestWidgetStatesGoldenDeterministicCanvas(t *testing.T) {
	theme := gameui.DefaultTheme()
	const canvasW, canvasH = 220, 120

	type stateSpec struct {
		name  string
		setup func(root *gameui.Root, target gameui.Widget)
	}
	states := []stateSpec{
		{
			name:  "Normal",
			setup: func(*gameui.Root, gameui.Widget) {},
		},
		{
			name: "Hover",
			setup: func(root *gameui.Root, target gameui.Widget) {
				root.OnPointerMove(centerOf(target.Bounds()))
			},
		},
		{
			name: "Pressed",
			setup: func(root *gameui.Root, target gameui.Widget) {
				root.OnPointerDown(centerOf(target.Bounds()))
			},
		},
		{
			name: "Focused",
			setup: func(root *gameui.Root, target gameui.Widget) {
				root.SetFocus(target)
			},
		},
		{
			name: "Disabled",
			setup: func(root *gameui.Root, target gameui.Widget) {
				switch w := target.(type) {
				case *gameui.Button:
					w.Disabled = true
				case *gameui.Toggle:
					w.Disabled = true
				case *gameui.Slider:
					w.Disabled = true
				case *gameui.List:
					w.Disabled = true
				}
				root.Layout(core.Bounds{Size: core.Size{Width: canvasW, Height: canvasH}})
			},
		},
	}

	renderOnce := func(t *testing.T, build func() gameui.Widget, st stateSpec) (ops []core.DrawOp, opHash string, pixHash string) {
		t.Helper()
		w := build()
		root := &gameui.Root{
			Theme: &theme,
			Child: &gameui.Panel{
				Padding: core.All(10),
				Child:   w,
			},
		}
		root.Layout(core.Bounds{Size: core.Size{Width: canvasW, Height: canvasH}})
		st.setup(root, w)

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return core.Canvas{
				TestID: "golden",
				Style:  core.Style{Width: core.Pt(canvasW), Height: core.Pt(canvasH)},
				Draw: func(d *core.Drawing, size core.Size) {
					root.Draw(d, core.Bounds{Size: size})
				},
			}
		})
		ops = app.Find("golden").Ops
		opHash = canonicalOpsDigest(ops)
		pixHash = pixelDigest(rasterizeOps(canvasW, canvasH, ops))
		return ops, opHash, pixHash
	}

	interactiveWidgets := []struct {
		name  string
		build func() gameui.Widget
	}{
		{
			name: "Button",
			build: func() gameui.Widget {
				return &gameui.Button{Text: "Confirm", Height: 40}
			},
		},
		{
			name: "ToggleCheckbox",
			build: func() gameui.Widget {
				return &gameui.Toggle{Label: "VSync", Checked: true, Style: gameui.ToggleCheckbox, Height: 36}
			},
		},
		{
			name: "ToggleSwitch",
			build: func() gameui.Widget {
				return &gameui.Toggle{Label: "Audio", Checked: true, Style: gameui.ToggleSwitch, Height: 36}
			},
		},
		{
			name: "Slider",
			build: func() gameui.Widget {
				return &gameui.Slider{Label: "Vol", Min: 0, Max: 100, Value: 60, Height: 36}
			},
		},
		{
			name: "List",
			build: func() gameui.Widget {
				return &gameui.List{Items: []string{"Alpha", "Beta", "Gamma"}, Selected: 1, ItemHeight: 24, Height: 90}
			},
		},
	}

	hasStroke := func(ops []core.DrawOp, col core.Color) bool {
		for _, op := range ops {
			if op.Kind == core.DrawStrokeColor && op.Color == col {
				return true
			}
		}
		return false
	}
	hasFill := func(ops []core.DrawOp, col core.Color) bool {
		for _, op := range ops {
			if op.Kind == core.DrawFillColor && op.Color == col {
				return true
			}
		}
		return false
	}

	for _, spec := range interactiveWidgets {
		t.Run(spec.name, func(t *testing.T) {
			seenOpHashes := make(map[string]string)
			seenPixHashes := make(map[string]string)

			for _, st := range states {
				ops1, opHash1, pixHash1 := renderOnce(t, spec.build, st)
				_, opHash2, pixHash2 := renderOnce(t, spec.build, st)

				if len(ops1) == 0 {
					t.Fatalf("%s/%s emitted zero DrawOps", spec.name, st.name)
				}
				if opHash1 != opHash2 || pixHash1 != pixHash2 {
					t.Fatalf("%s/%s render is non-deterministic: opHash %s!=%s pixHash %s!=%s",
						spec.name, st.name, opHash1, opHash2, pixHash1, pixHash2)
				}
				if prev, dup := seenOpHashes[opHash1]; dup {
					t.Errorf("%s state %s produced identical DrawOps to %s", spec.name, st.name, prev)
				}
				seenOpHashes[opHash1] = st.name
				if prev, dup := seenPixHashes[pixHash1]; dup {
					t.Errorf("%s state %s produced identical rasterized pixels to %s", spec.name, st.name, prev)
				}
				seenPixHashes[pixHash1] = st.name

				// Verify key state-specific visual cues.
				switch st.name {
				case "Focused":
					if !hasStroke(ops1, theme.FocusedBorder) {
						t.Errorf("%s/Focused missing focus border color %#x", spec.name, theme.FocusedBorder)
					}
				case "Disabled":
					if !hasFill(ops1, theme.Disabled) {
						t.Errorf("%s/Disabled missing Disabled fill color %#x", spec.name, theme.Disabled)
					}
					if hasStroke(ops1, theme.FocusedBorder) {
						t.Errorf("%s/Disabled should not draw focus ring", spec.name)
					}
				case "Hover":
					if !hasFill(ops1, theme.Hover) && !hasFill(ops1, theme.AccentHover) {
						t.Errorf("%s/Hover missing Hover/AccentHover fill color", spec.name)
					}
				case "Pressed":
					if !hasFill(ops1, theme.Active) && !hasFill(ops1, theme.AccentActive) {
						t.Errorf("%s/Pressed missing Active/AccentActive fill color", spec.name)
					}
				}
			}
		})
	}

	// Also verify deterministic rendering for Panel container itself.
	t.Run("Panel", func(t *testing.T) {
		buildPanel := func() gameui.Widget {
			return &gameui.Panel{
				Padding: core.All(8),
				Child:   &gameui.Label{Text: "HUD", Height: 24},
			}
		}
		ops1, opHash1, pixHash1 := renderOnce(t, buildPanel, states[0])
		_, opHash2, pixHash2 := renderOnce(t, buildPanel, states[0])
		if opHash1 != opHash2 || pixHash1 != pixHash2 {
			t.Fatalf("Panel render is non-deterministic")
		}
		if !hasFill(ops1, theme.PanelBackground) || !hasStroke(ops1, theme.PanelBorder) {
			t.Fatalf("Panel missing PanelBackground/PanelBorder ops")
		}
	})
}

func TestSpatialFocusNavigation2DGridActionsAndFocusRing(t *testing.T) {
	var confirmed []string
	var backCount int
	var focusTransitions int

	b00 := &gameui.Button{Text: "B00", Flex: 1, OnClick: func() { confirmed = append(confirmed, "B00") }}
	b01 := &gameui.Button{Text: "B01", Flex: 1, OnClick: func() { confirmed = append(confirmed, "B01") }}
	b02 := &gameui.Button{Text: "B02", Flex: 1, OnClick: func() { confirmed = append(confirmed, "B02") }}

	tog10 := &gameui.Toggle{Label: "T10", Flex: 1}
	dis11 := &gameui.Button{Text: "Locked", Flex: 1, Disabled: true}
	sld12 := &gameui.Slider{Min: 0, Max: 100, Value: 50, Step: 10, Flex: 1}

	b20 := &gameui.Button{Text: "B20", Flex: 1, OnClick: func() { confirmed = append(confirmed, "B20") }}
	lst21 := &gameui.List{Items: []string{"Easy", "Hard"}, Selected: 0, Flex: 1}
	b22 := &gameui.Button{Text: "B22", Flex: 1, OnClick: func() { confirmed = append(confirmed, "B22") }}

	grid := &gameui.Panel{
		Padding: core.All(10),
		Child: &gameui.VBox{
			Spacing: 10,
			Children: []gameui.Widget{
				&gameui.HBox{Height: 40, Spacing: 10, Children: []gameui.Widget{b00, b01, b02}},
				&gameui.HBox{Height: 40, Spacing: 10, Children: []gameui.Widget{tog10, dis11, sld12}},
				&gameui.HBox{Height: 64, Spacing: 10, Children: []gameui.Widget{b20, lst21, b22}},
			},
		},
	}

	root := gameui.NewRoot(grid)
	root.Layout(core.Bounds{Size: core.Size{Width: 420, Height: 200}})

	nav := gameui.NewSpatialNavigator(root)
	nav.OnConfirm = func(w gameui.Widget) {
		if w == tog10 {
			confirmed = append(confirmed, "T10")
		}
	}
	nav.OnBack = func() { backCount++ }
	nav.OnFocusChange = func(_, _ gameui.Widget) { focusTransitions++ }

	// Simulated keyboard action source compatible with input.Actions.JustPressed.
	kbPressed := map[string]bool{}
	kbMap := map[gameui.NavAction]string{
		gameui.NavUp:      "ArrowUp",
		gameui.NavDown:    "ArrowDown",
		gameui.NavLeft:    "ArrowLeft",
		gameui.NavRight:   "ArrowRight",
		gameui.NavConfirm: "Enter",
		gameui.NavBack:    "Escape",
	}
	stepKB := func(key string) bool {
		clear(kbPressed)
		kbPressed[key] = true
		return nav.StepActions(func(a gameui.NavAction) bool { return kbPressed[kbMap[a]] })
	}

	// Simulated gamepad action source.
	padPressed := map[string]bool{}
	padMap := map[gameui.NavAction]string{
		gameui.NavUp:      "DPadUp",
		gameui.NavDown:    "DPadDown",
		gameui.NavLeft:    "DPadLeft",
		gameui.NavRight:   "DPadRight",
		gameui.NavConfirm: "ButtonA",
		gameui.NavBack:    "ButtonB",
	}
	stepPad := func(btn string) bool {
		clear(padPressed)
		padPressed[btn] = true
		return nav.StepActions(func(a gameui.NavAction) bool { return padPressed[padMap[a]] })
	}

	// Start at B00 via keyboard Down (initial focus).
	if !stepKB("ArrowDown") || nav.Focused() != b00 {
		t.Fatalf("expected initial focus on B00, got %v", nav.Focused())
	}
	// Move right across row 0: B00 -> B01 -> B02, then wrap to B00.
	if !stepKB("ArrowRight") || nav.Focused() != b01 {
		t.Fatalf("expected B01, got %v", nav.Focused())
	}
	if !stepPad("DPadRight") || nav.Focused() != b02 {
		t.Fatalf("expected B02, got %v", nav.Focused())
	}
	if !stepPad("DPadRight") || nav.Focused() != b00 {
		t.Fatalf("expected wrap right to B00, got %v", nav.Focused())
	}

	// Disable wrap-around and confirm left from B00 does not wrap.
	nav.WrapAround = false
	if stepKB("ArrowLeft") || nav.Focused() != b00 {
		t.Fatalf("with WrapAround=false, left from B00 should stay on B00")
	}
	nav.WrapAround = true

	// Move down from B00 -> Toggle(1,0), then right across disabled Locked(1,1) -> Slider(1,2)!
	if !stepKB("ArrowDown") || nav.Focused() != tog10 {
		t.Fatalf("expected tog10, got %v", nav.Focused())
	}
	if !stepPad("ButtonA") || !tog10.Checked {
		t.Fatalf("expected gamepad ButtonA to toggle tog10, checked=%v", tog10.Checked)
	}
	if !stepPad("DPadRight") || nav.Focused() != sld12 {
		t.Fatalf("expected right from tog10 to skip disabled dis11 and land on sld12, got %v", nav.Focused())
	}

	// Focused Slider steps with NavRight/NavLeft when SliderSteps=true.
	if !stepKB("ArrowRight") {
		t.Fatal("NavRight on slider should step value")
	}
	assertNear(t, "slider after NavRight", sld12.Value, 60)
	if !stepPad("DPadLeft") {
		t.Fatal("DPadLeft on slider should step value")
	}
	assertNear(t, "slider after DPadLeft", sld12.Value, 50)

	// Move down from Slider(1,2) -> B22, left -> List(2,1), left -> B20, confirm via Enter, back via Escape/ButtonB.
	if !stepKB("ArrowDown") || nav.Focused() != b22 {
		t.Fatalf("expected down from sld12 to focus b22, got %v", nav.Focused())
	}
	if !stepPad("DPadLeft") || nav.Focused() != lst21 {
		t.Fatalf("expected left from b22 to focus lst21, got %v", nav.Focused())
	}
	if !stepKB("ArrowLeft") || nav.Focused() != b20 {
		t.Fatalf("expected left from lst21 to focus b20, got %v", nav.Focused())
	}
	if !stepKB("Enter") {
		t.Fatal("expected Enter confirm on b20")
	}
	if !stepKB("Escape") || !stepPad("ButtonB") || backCount != 2 {
		t.Fatalf("expected keyboard Escape and gamepad ButtonB to trigger OnBack twice, got %d", backCount)
	}

	// Explicit overrides take precedence over spatial lookup (both widget Nav field and navigator.SetOverrides).
	// Set widget-level override: from b20 going Up jumps directly to b02 (instead of tog10).
	b20.Nav.Up = b02
	if !nav.Move(gameui.NavUp) || nav.Focused() != b02 {
		t.Fatalf("expected explicit widget Nav.Up override from b20 -> b02, got %v", nav.Focused())
	}
	// Set navigator-level override: from b02 going Down -> dis11 (disabled) chained to b20!
	nav.SetOverrides(b02, gameui.NavOverrides{Down: dis11})
	nav.SetOverrides(dis11, gameui.NavOverrides{Down: b20})
	if !nav.Move(gameui.NavDown) || nav.Focused() != b20 {
		t.Fatalf("expected override chain b02 -> disabled(dis11) -> b20, got %v", nav.Focused())
	}
	if focusTransitions < 10 {
		t.Fatalf("expected OnFocusChange to fire on transitions, got %d", focusTransitions)
	}
	if len(confirmed) < 2 || confirmed[0] != "T10" || confirmed[1] != "B20" {
		t.Fatalf("unexpected confirmed actions: %v", confirmed)
	}

	// Verify customizable FocusRingStyle draws ring ops with custom Color, Width, Outset, and Dash.
	ringColor := core.RGB(0x00ffaa)
	ringStyle := &gameui.FocusRingStyle{
		Color:  ringColor,
		Width:  3.5,
		Outset: 4,
		Dash:   []float32{6, 3},
	}
	root.FocusRing = ringStyle
	nav.FocusRing = ringStyle

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "focus-ring-canvas",
			Style:  core.Style{Width: core.Pt(420), Height: core.Pt(200)},
			Draw: func(d *core.Drawing, size core.Size) {
				root.Draw(d, core.Bounds{Size: size})
			},
		}
	})
	ops := app.Find("focus-ring-canvas").Ops
	var foundRingColor, foundRingWidth, foundDash bool
	for _, op := range ops {
		if op.Kind == core.DrawStrokeColor && op.Color == ringColor {
			foundRingColor = true
		}
		if op.Kind == core.DrawLineWidth && math.Abs(float64(op.W-3.5)) < 1e-3 {
			foundRingWidth = true
		}
		if op.Kind == core.DrawLineDash && op.Dash != nil && len(*op.Dash) == 2 {
			foundDash = true
		}
	}
	if !foundRingColor || !foundRingWidth || !foundDash {
		t.Fatalf("custom focus ring ops missing: color=%v width=%v dash=%v", foundRingColor, foundRingWidth, foundDash)
	}
}

func TestWorldAnchoredNativeViews(t *testing.T) {
	var overlay *gameui.AnchorOverlay
	var heroAnchor *gameui.WorldAnchor
	var bubbleClicks int
	var canvasClicks int
	var typedChat string

	const winW, winH = 800, 600
	const virtW, virtH = 320, 180

	// 320x180 letterboxed inside 800x600 -> scale 2.5, viewport rect (0, 75, 800, 450).
	vp := gameui.NewViewport(virtW, virtH, gameui.ScaleLetterbox).WithWindowSize(winW, winH)
	initialProj := gameui.NewCameraViewportProjection(0, 0, 1.0, virtW, virtH, vp)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		overlay = gameui.NewAnchorOverlay(cx)
		overlay.TestID = "anchor-overlay"

		heroAnchor = &gameui.WorldAnchor{
			ID:      "hero",
			WorldX:  0,
			WorldY:  0,
			OffsetX: 0,
			OffsetY: -12,
			AnchorX: 0.5,
			AnchorY: 1.0,
			Visible: true,
			TestID:  "hero-anchor",
			View: func(cx *core.Scope) core.View {
				return core.View{
					TestID: "hero-tag",
					Style:  core.Style{Width: core.Pt(80), Height: core.Pt(20)},
					Children: []core.Node{
						core.Text{TestID: "hero-text", Content: "Knight"},
					},
				}
			},
		}
		npcAnchor := &gameui.WorldAnchor{
			ID:      "npc",
			WorldX:  40,
			WorldY:  20,
			Visible: true,
			TestID:  "npc-anchor",
			View: func(cx *core.Scope) core.View {
				return core.View{
					TestID:  "npc-bubble",
					Style:   core.Style{Width: core.Pt(90), Height: core.Pt(28)},
					OnPress: func(core.PressEvent) { bubbleClicks++ },
					Children: []core.Node{
						core.Text{Content: "Talk"},
					},
				}
			},
		}
		chatAnchor := &gameui.WorldAnchor{
			ID:      "chat",
			WorldX:  -60,
			WorldY:  10,
			Visible: true,
			TestID:  "chat-anchor",
			View: func(cx *core.Scope) core.View {
				return core.View{
					Style: core.Style{Width: core.Pt(100), Height: core.Pt(28)},
					Children: []core.Node{
						core.Input{
							TestID:      "chat-input",
							Placeholder: "Say...",
							OnChange:    func(s string) { typedChat = s },
						},
					},
				}
			},
		}

		overlay.Add(heroAnchor)
		overlay.Add(npcAnchor)
		overlay.Add(chatAnchor)
		overlay.Sync(initialProj)

		return core.View{
			Style: core.Style{Width: core.Pt(winW), Height: core.Pt(winH)},
			Children: []core.Node{
				core.Canvas{
					TestID:  "world-canvas",
					Style:   core.Style{Width: core.Pt(winW), Height: core.Pt(winH)},
					OnPress: func(core.PressEvent) { canvasClicks++ },
					Draw:    func(*core.Drawing, core.Size) {},
				},
				overlay.View(cx),
			},
		}
	})

	// Resize the headless test surface to match the game window dimensions (800x600).
	app.Resize(winW, winH)

	// 1. Verify container is non-blocking (PointerEventsNone resolved to BoxNone because interactive children opt back in)
	// and anchored wrapper re-enables interaction (PointerEventsOn sent as PointerEventsAuto).
	ovView := app.Find("anchor-overlay")
	if ovView.Props.PointerEvents != core.PointerEventsNone && ovView.Props.PointerEvents != core.PointerEventsBoxNone {
		t.Fatalf("overlay container PointerEvents = %v, want PointerEventsNone or PointerEventsBoxNone", ovView.Props.PointerEvents)
	}
	heroWrap := app.Find("hero-anchor")
	if heroWrap.Props.PointerEvents != core.PointerEventsAuto && heroWrap.Props.PointerEvents != core.PointerEventsOn {
		t.Fatalf("anchored wrapper PointerEvents = %v, want PointerEventsAuto/PointerEventsOn", heroWrap.Props.PointerEvents)
	}
	// Verify AnchorX=0.5, AnchorY=1.0 on the 80x20 hero-tag resolves to (-40, -20) transform translation.
	assertNear(t, "hero.TranslateX", float32(heroWrap.Style.Transform.TranslateX), -40)
	assertNear(t, "hero.TranslateY", float32(heroWrap.Style.Transform.TranslateY), -20)

	// Verify canvas underneath receives presses and anchored native views (bubble + input) are interactive.
	app.PressAt("world-canvas", core.Point{X: 10, Y: 10})
	if canvasClicks != 1 {
		t.Fatalf("expected click to reach canvas underneath non-blocking overlay, got %d", canvasClicks)
	}
	app.Press("npc-bubble")
	if bubbleClicks != 1 {
		t.Fatalf("expected anchored native view press to work, got %d", bubbleClicks)
	}
	app.Type("chat-input", "GG!")
	if typedChat != "GG!" {
		t.Fatalf("expected anchored native Input to receive text, got %q", typedChat)
	}

	// 2. Verify 0px (exact same-frame) alignment across moving sprite, camera pan, zoom (0.5x, 2.0x), and letterboxing!
	type frameCase struct {
		name         string
		worldX       float32
		worldY       float32
		camX         float32
		camY         float32
		zoom         float32
		viewport     gameui.Viewport
		expectScreen bool
	}
	cases := []frameCase{
		{
			name:         "origin zoom 1x letterbox",
			worldX:       0,
			worldY:       0,
			camX:         0,
			camY:         0,
			zoom:         1.0,
			viewport:     vp,
			expectScreen: true,
		},
		{
			name:         "moving sprite + camera pan zoom 2.0x letterbox",
			worldX:       45,
			worldY:       -20,
			camX:         15,
			camY:         -5,
			zoom:         2.0,
			viewport:     vp,
			expectScreen: true,
		},
		{
			name:         "moving sprite + camera pan zoom 0.5x letterbox",
			worldX:       -100,
			worldY:       56,
			camX:         -20,
			camY:         8,
			zoom:         0.5,
			viewport:     vp,
			expectScreen: true,
		},
		{
			name:         "pillarbox viewport 1280x600 zoom 1.5x",
			worldX:       20,
			worldY:       -10,
			camX:         5,
			camY:         0,
			zoom:         1.5,
			viewport:     gameui.NewViewport(320, 240, gameui.ScaleFit).WithWindowSize(1280, 600),
			expectScreen: true,
		},
		{
			name:         "moved off-screen horizontally",
			worldX:       500,
			worldY:       0,
			camX:         0,
			camY:         0,
			zoom:         1.0,
			viewport:     vp,
			expectScreen: false,
		},
		{
			name:         "moved into top letterbox bar (off-screen vertically)",
			worldX:       0,
			worldY:       -100, // virtY = -10 < 0 -> projects into top letterbox bar!
			camX:         0,
			camY:         0,
			zoom:         1.0,
			viewport:     vp,
			expectScreen: false,
		},
		{
			name:         "returns back on-screen",
			worldX:       12,
			worldY:       8,
			camX:         0,
			camY:         0,
			zoom:         2.0,
			viewport:     vp,
			expectScreen: true,
		},
	}

	for _, tc := range cases {
		app.Resize(core.Points(tc.viewport.WindowWidth), core.Points(tc.viewport.WindowHeight))
		proj := gameui.NewCameraViewportProjection(tc.camX, tc.camY, tc.zoom, tc.viewport.VirtualWidth, tc.viewport.VirtualHeight, tc.viewport)
		app.Post(func() {
			heroAnchor.WorldX = tc.worldX
			heroAnchor.WorldY = tc.worldY
			overlay.Sync(proj)
		})
		app.Settle()

		if !tc.expectScreen {
			if app.Exists("hero-anchor") || app.Exists("hero-tag") {
				t.Fatalf("%s: off-screen anchor should be hidden immediately", tc.name)
			}
			continue
		}

		if !app.Exists("hero-anchor") {
			t.Fatalf("%s: expected on-screen anchor to be rendered", tc.name)
		}
		wantX, wantY := proj.WorldToScreen(tc.worldX, tc.worldY)
		wantX += heroAnchor.OffsetX
		wantY += heroAnchor.OffsetY

		gotBounds := app.Find("hero-anchor").Bounds()
		// NSGo snaps layout frames to whole points (math.Round), so check 0px error against the snapped coordinate
		// while verifying exact sub-pixel precision on overlay.ScreenPosition.
		wantLayoutX := float32(math.Round(float64(wantX)))
		wantLayoutY := float32(math.Round(float64(wantY)))
		if math.Abs(float64(float32(gotBounds.Origin.X)-wantLayoutX)) > 1e-3 ||
			math.Abs(float64(float32(gotBounds.Origin.Y)-wantLayoutY)) > 1e-3 {
			t.Fatalf("%s: anchored view origin = (%v, %v), want exact (%v, %v) (raw %v, %v)",
				tc.name, gotBounds.Origin.X, gotBounds.Origin.Y, wantLayoutX, wantLayoutY, wantX, wantY)
		}
		gotScreen, vis := overlay.ScreenPosition("hero")
		if !vis {
			t.Fatalf("%s: ScreenPosition reported visible=false", tc.name)
		}
		assertNear(t, tc.name+".ScreenPosition.X", float32(gotScreen.X), wantX)
		assertNear(t, tc.name+".ScreenPosition.Y", float32(gotScreen.Y), wantY)
	}

	// 3. Explicit Visible=false and Remove hide/unmount the anchor cleanly.
	app.Post(func() {
		overlay.SetVisible("hero", false)
		overlay.Sync(initialProj)
	})
	app.Settle()
	if app.Exists("hero-anchor") {
		t.Fatal("SetVisible(false) should hide anchor")
	}
	app.Post(func() {
		overlay.Remove("npc")
	})
	app.Settle()
	if app.Exists("npc-bubble") {
		t.Fatal("Remove should unmount anchor")
	}
}
