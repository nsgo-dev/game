package editor_test

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	overlayBoardCols  = 4
	overlayBoardRows  = 2
	overlayCellW      = 196
	overlayCellH      = 148
	overlayBoardPad   = 12
	overlayBoardGap   = 10
	overlayBoardWidth = overlayBoardPad*2 + overlayBoardCols*overlayCellW + (overlayBoardCols-1)*overlayBoardGap
	overlayBoardH     = overlayBoardPad*2 + overlayBoardRows*overlayCellH + (overlayBoardRows-1)*overlayBoardGap
)

type overlayBoardSlot struct {
	name string
	x, y float32
	draw func(d *core.Drawing, p editor.SpecPalette, cell core.Bounds)
}

func specOverlayBoardSlots() []overlayBoardSlot {
	return []overlayBoardSlot{
		{
			name: "selection",
			draw: func(d *core.Drawing, p editor.SpecPalette, cell core.Bounds) {
				cx := float32(cell.Origin.X + cell.Size.Width/2)
				fy := float32(cell.Origin.Y+cell.Size.Height) - 24
				obj := core.Bounds{Origin: core.Point{X: core.Points(cx - 12), Y: core.Points(fy - 34)}, Size: core.Size{Width: 24, Height: 34}}
				d.SetFillColor(p.Changed.Fg).BeginPath().RoundRect(cx-12, fy-34, 24, 34, 4).Fill()
				editor.DrawObjectOverlay(d, p, editor.ObjectOverlay{
					Kind:       editor.OverlayKindSelection,
					Label:      "Knight",
					Bounds:     obj,
					CanvasSize: cell.Size,
				})
			},
		},
		{
			name: "agent",
			draw: func(d *core.Drawing, p editor.SpecPalette, cell core.Bounds) {
				cx := float32(cell.Origin.X + cell.Size.Width/2)
				fy := float32(cell.Origin.Y+cell.Size.Height) - 24
				obj := core.Bounds{Origin: core.Point{X: core.Points(cx - 15), Y: core.Points(fy - 22)}, Size: core.Size{Width: 30, Height: 22}}
				d.SetFillColor(ui.Alpha(p.Agent.Fg, 0.30)).BeginPath().RoundRect(cx-15, fy-22, 30, 22, 6).Fill()
				editor.DrawObjectOverlay(d, p, editor.ObjectOverlay{
					Kind:       editor.OverlayKindAgent,
					Label:      "placing slime_02",
					Bounds:     obj,
					CanvasSize: cell.Size,
				})
			},
		},
		{
			name: "added",
			draw: func(d *core.Drawing, p editor.SpecPalette, cell core.Bounds) {
				cx := float32(cell.Origin.X + cell.Size.Width/2)
				fy := float32(cell.Origin.Y+cell.Size.Height) - 24
				obj := core.Bounds{Origin: core.Point{X: core.Points(cx - 15), Y: core.Points(fy - 22)}, Size: core.Size{Width: 30, Height: 22}}
				d.SetFillColor(ui.Alpha(p.Added.Fg, 0.30)).BeginPath().RoundRect(cx-15, fy-22, 30, 22, 6).Fill()
				editor.DrawObjectOverlay(d, p, editor.ObjectOverlay{
					Kind:       editor.OverlayKindAdded,
					Label:      "slime_03",
					Bounds:     obj,
					CanvasSize: cell.Size,
				})
			},
		},
		{
			name: "changed",
			draw: func(d *core.Drawing, p editor.SpecPalette, cell core.Bounds) {
				cx := float32(cell.Origin.X + cell.Size.Width/2)
				fy := float32(cell.Origin.Y+cell.Size.Height) - 24
				obj := core.Bounds{Origin: core.Point{X: core.Points(cx - 12), Y: core.Points(fy - 34)}, Size: core.Size{Width: 24, Height: 34}}
				d.SetFillColor(p.Changed.Fg).BeginPath().RoundRect(cx-12, fy-34, 24, 34, 4).Fill()
				editor.DrawObjectOverlay(d, p, editor.ObjectOverlay{
					Kind:       editor.OverlayKindChanged,
					Label:      "z 10 → 12",
					Bounds:     obj,
					CanvasSize: cell.Size,
				})
			},
		},
		{
			name: "removed",
			draw: func(d *core.Drawing, p editor.SpecPalette, cell core.Bounds) {
				cx := float32(cell.Origin.X + cell.Size.Width/2)
				fy := float32(cell.Origin.Y+cell.Size.Height) - 24
				obj := core.Bounds{Origin: core.Point{X: core.Points(cx - 10), Y: core.Points(fy - 22)}, Size: core.Size{Width: 20, Height: 20}}
				editor.DrawObjectOverlay(d, p, editor.ObjectOverlay{
					Kind:       editor.OverlayKindRemoved,
					Label:      "coin_04",
					Bounds:     obj,
					CanvasSize: cell.Size,
				})
			},
		},
		{
			name: "problem",
			draw: func(d *core.Drawing, p editor.SpecPalette, cell core.Bounds) {
				cx := float32(cell.Origin.X + cell.Size.Width/2)
				fy := float32(cell.Origin.Y+cell.Size.Height) - 24
				obj := core.Bounds{Origin: core.Point{X: core.Points(cx - 14), Y: core.Points(fy - 38)}, Size: core.Size{Width: 28, Height: 38}}
				d.SetFillColor(p.TextMuted).BeginPath().RoundRect(cx-14, fy-38, 28, 38, 4).Fill()
				editor.DrawObjectOverlay(d, p, editor.ObjectOverlay{
					Kind:       editor.OverlayKindProblem,
					Label:      "door: lever_02 missing",
					Bounds:     obj,
					CanvasSize: cell.Size,
				})
			},
		},
		{
			name: "focused",
			draw: func(d *core.Drawing, p editor.SpecPalette, cell core.Bounds) {
				cx := float32(cell.Origin.X + cell.Size.Width/2)
				fy := float32(cell.Origin.Y+cell.Size.Height) - 24
				// Unfocused peer on left + focused 2.5 px change with outlined pill on right (never accent).
				left := core.Bounds{Origin: core.Point{X: core.Points(cx - 56), Y: core.Points(fy - 22)}, Size: core.Size{Width: 28, Height: 20}}
				d.SetFillColor(ui.Alpha(p.Added.Fg, 0.28)).BeginPath().RoundRect(cx-56, fy-22, 28, 20, 6).Fill()
				editor.DrawObjectOverlay(d, p, editor.ObjectOverlay{
					Kind:       editor.OverlayKindAdded,
					Bounds:     left,
					CanvasSize: cell.Size,
				})
				right := core.Bounds{Origin: core.Point{X: core.Points(cx + 12), Y: core.Points(fy - 22)}, Size: core.Size{Width: 28, Height: 20}}
				d.SetFillColor(ui.Alpha(p.Added.Fg, 0.48)).BeginPath().RoundRect(cx+12, fy-22, 28, 20, 6).Fill()
				editor.DrawObjectOverlay(d, p, editor.ObjectOverlay{
					Kind:       editor.OverlayKindFocusedChange,
					ChangeKind: editor.OverlayKindAdded,
					Label:      "slime_02",
					Bounds:     right,
					CanvasSize: cell.Size,
				})
			},
		},
		{
			name: "selection_and_agent",
			draw: func(d *core.Drawing, p editor.SpecPalette, cell core.Bounds) {
				cx := float32(cell.Origin.X + cell.Size.Width/2)
				fy := float32(cell.Origin.Y+cell.Size.Height) - 24
				obj := core.Bounds{Origin: core.Point{X: core.Points(cx - 12), Y: core.Points(fy - 34)}, Size: core.Size{Width: 24, Height: 34}}
				d.SetFillColor(p.Changed.Fg).BeginPath().RoundRect(cx-12, fy-34, 24, 34, 4).Fill()
				editor.DrawObjectOverlay(d, p, editor.ObjectOverlay{
					Kind:       editor.OverlayKindSelection,
					Label:      "Knight",
					Bounds:     obj,
					CanvasSize: cell.Size,
				})
				editor.DrawObjectOverlay(d, p, editor.ObjectOverlay{
					Kind:                editor.OverlayKindAgent,
					Label:               "tuning jump",
					Bounds:              obj,
					CanvasSize:          cell.Size,
					PairedWithSelection: true,
					PillStackOffset:     24,
				})
			},
		},
	}
}

func renderOverlayBoard(scale int) (*image.RGBA, []image.Rectangle, []string) {
	p := editor.DarkSpecPalette()
	slots := specOverlayBoardSlots()
	var d core.Drawing
	if scale > 1 {
		d.Scale(float32(scale), float32(scale))
	}
	d.SetFillColor(p.Bg).FillRect(0, 0, overlayBoardWidth, overlayBoardH)

	cellRects := make([]image.Rectangle, len(slots))
	names := make([]string, len(slots))
	for i, slot := range slots {
		col := i % overlayBoardCols
		row := i / overlayBoardCols
		x := float32(overlayBoardPad + col*(overlayCellW+overlayBoardGap))
		y := float32(overlayBoardPad + row*(overlayCellH+overlayBoardGap))
		cellRects[i] = image.Rect(int(x)*scale, int(y)*scale, int(x+overlayCellW)*scale, int(y+overlayCellH)*scale)
		names[i] = slot.name

		d.Save()
		d.Translate(x, y)
		// Viewport cell backdrop matching Spec.dc.html.
		d.SetFillColor(p.Viewport).BeginPath().RoundRect(0, 0, overlayCellW, overlayCellH, 10).Fill()
		d.SetStrokeColor(p.Line).SetLineWidth(1).BeginPath().RoundRect(0, 0, overlayCellW, overlayCellH, 10).Stroke()
		d.SetFillColor(p.ViewportDot)
		for dy := float32(10); dy < overlayCellH-24; dy += 16 {
			for dx := float32(10); dx < overlayCellW-6; dx += 16 {
				d.FillRect(dx, dy, 1.2, 1.2)
			}
		}
		d.SetFillColor(p.Inset).FillRect(1, overlayCellH-24, overlayCellW-2, 23)
		d.SetFillColor(p.LineRaised).FillRect(1, overlayCellH-24, overlayCellW-2, 2)

		slot.draw(&d, p, core.Bounds{Size: core.Size{Width: overlayCellW, Height: overlayCellH}})
		d.Restore()
	}

	img := gametest.RenderOps(overlayBoardWidth*scale, overlayBoardH*scale, d.Ops)
	return img, cellRects, names
}

// #1006 Acceptance 1: Golden-image test draws each overlay kind at 1x and 2x and matches the Spec's legend.
func TestViewportOverlayGolden1xAnd2x(t *testing.T) {
	img1x, _, _ := renderOverlayBoard(1)
	gametest.AssertGolden(t, "viewport_overlays_1x", img1x)

	img2x, _, _ := renderOverlayBoard(2)
	gametest.AssertGolden(t, "viewport_overlays_2x", img2x)
}

func toGreyscale(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := src.RGBAAt(x, y)
			// WCAG Rec. 709 luminance.
			lum := uint8(math.Round(0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)))
			out.SetRGBA(x, y, color.RGBA{R: lum, G: lum, B: lum, A: c.A})
		}
	}
	return out
}

// #1006 Acceptance 2: The same render in greyscale still tells every kind apart by line, handles, pill or ghost.
func TestViewportOverlayGreyscaleDistinguishable(t *testing.T) {
	p := editor.DarkSpecPalette()
	// Draw every kind onto identical 180x140 cells with the SAME neutral object block
	// (except Removed which draws its own 25% grey ghost) so hue CANNOT account for differences.
	const w, h = 180, 140
	obj := core.Bounds{
		Origin: core.Point{X: 76, Y: 72},
		Size:   core.Size{Width: 28, Height: 34},
	}

	kinds := []struct {
		name      string
		drawSolid bool
		ov        editor.ObjectOverlay
	}{
		{"selection", true, editor.ObjectOverlay{Kind: editor.OverlayKindSelection, Label: "Knight", Bounds: obj, CanvasSize: core.Size{Width: w, Height: h}}},
		{"agent", true, editor.ObjectOverlay{Kind: editor.OverlayKindAgent, Label: "placing slime_02", Bounds: obj, CanvasSize: core.Size{Width: w, Height: h}}},
		{"added", true, editor.ObjectOverlay{Kind: editor.OverlayKindAdded, Label: "slime_03", Bounds: obj, CanvasSize: core.Size{Width: w, Height: h}}},
		{"changed", true, editor.ObjectOverlay{Kind: editor.OverlayKindChanged, Label: "z 10 → 12", Bounds: obj, CanvasSize: core.Size{Width: w, Height: h}}},
		{"removed", false, editor.ObjectOverlay{Kind: editor.OverlayKindRemoved, Label: "coin_04", Bounds: obj, CanvasSize: core.Size{Width: w, Height: h}}},
		{"problem", true, editor.ObjectOverlay{Kind: editor.OverlayKindProblem, Label: "door: lever_02 missing", Bounds: obj, CanvasSize: core.Size{Width: w, Height: h}}},
		{"focused", true, editor.ObjectOverlay{Kind: editor.OverlayKindFocusedChange, ChangeKind: editor.OverlayKindAdded, Label: "slime_03", Bounds: obj, CanvasSize: core.Size{Width: w, Height: h}}},
	}

	renders := make([]*image.RGBA, len(kinds))
	for i, k := range kinds {
		var d core.Drawing
		d.SetFillColor(p.Viewport).FillRect(0, 0, w, h)
		if k.drawSolid {
			// Identical neutral live object body across every non-Removed kind.
			d.SetFillColor(p.TextMuted).FillRect(float32(obj.Origin.X), float32(obj.Origin.Y), float32(obj.Size.Width), float32(obj.Size.Height))
		}
		editor.DrawObjectOverlay(&d, p, k.ov)
		renders[i] = toGreyscale(gametest.RenderOps(w, h, d.Ops))
	}

	// Verify every pair of kinds has a substantial structural greyscale difference (not identical or hue-only).
	for i := 0; i < len(kinds); i++ {
		for j := i + 1; j < len(kinds); j++ {
			diffPixels := 0
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					a := renders[i].RGBAAt(x, y).R
					b := renders[j].RGBAAt(x, y).R
					if int(a)-int(b) > 12 || int(b)-int(a) > 12 {
						diffPixels++
					}
				}
			}
			if diffPixels < 120 {
				t.Errorf("greyscale renders of %q and %q differ by only %d pixels (want >= 120 structural cue pixels)", kinds[i].name, kinds[j].name, diffPixels)
			}
		}
	}

	// Verify Focused change NEVER uses Accent color.
	var focusedDraw core.Drawing
	editor.DrawObjectOverlay(&focusedDraw, p, editor.ObjectOverlay{
		Kind:       editor.OverlayKindFocusedChange,
		ChangeKind: editor.OverlayKindAdded,
		Label:      "slime_02",
		Bounds:     obj,
	})
	for _, op := range focusedDraw.Ops {
		if (op.Kind == core.DrawStrokeColor || op.Kind == core.DrawFillColor) && op.Color == p.Accent.Edge {
			t.Fatal("Focused change overlay used Accent color; Spec forbids Accent on Focused change")
		}
	}
	// Verify Focused change uses 2.5 px width.
	has25 := false
	for _, op := range focusedDraw.Ops {
		if op.Kind == core.DrawLineWidth && op.W == 2.5 {
			has25 = true
			break
		}
	}
	if !has25 {
		t.Fatal("Focused change overlay did not use 2.5 px stroke width")
	}
}

// #1006 Scope: Selection + Agent on one object keeps selection handles and places agent outline 3 px further out.
func TestViewportOverlaySelectionAndAgentPairedOffset(t *testing.T) {
	p := editor.DarkSpecPalette()
	obj := core.Bounds{Origin: core.Point{X: 100, Y: 100}, Size: core.Size{Width: 40, Height: 40}}

	var selDraw, agentAloneDraw, agentPairedDraw core.Drawing
	editor.DrawObjectOverlay(&selDraw, p, editor.ObjectOverlay{Kind: editor.OverlayKindSelection, Label: "Knight", Bounds: obj})
	editor.DrawObjectOverlay(&agentAloneDraw, p, editor.ObjectOverlay{Kind: editor.OverlayKindAgent, Label: "editing", Bounds: obj})
	editor.DrawObjectOverlay(&agentPairedDraw, p, editor.ObjectOverlay{Kind: editor.OverlayKindAgent, Label: "editing", Bounds: obj, PairedWithSelection: true})

	findOutline := func(ops []core.DrawOp) (core.DrawOp, bool) {
		for _, op := range ops {
			if op.Kind == core.DrawRoundRect && op.W >= 40 && op.H >= 40 {
				return op, true
			}
		}
		return core.DrawOp{}, false
	}

	selRR, ok1 := findOutline(selDraw.Ops)
	aloneRR, ok2 := findOutline(agentAloneDraw.Ops)
	pairedRR, ok3 := findOutline(agentPairedDraw.Ops)
	if !ok1 || !ok2 || !ok3 {
		t.Fatalf("missing outline RoundRect ops: sel=%v alone=%v paired=%v", ok1, ok2, ok3)
	}
	if selRR.X != 97 || selRR.W != 46 {
		t.Fatalf("selection outline = (x=%v, w=%v), want (97, 46)", selRR.X, selRR.W)
	}
	if aloneRR.X != 97 || aloneRR.W != 46 {
		t.Fatalf("unpaired agent outline = (x=%v, w=%v), want (97, 46)", aloneRR.X, aloneRR.W)
	}
	if pairedRR.X != selRR.X-3 || pairedRR.W != selRR.W+6 {
		t.Fatalf("paired agent outline = (x=%v, w=%v), want 3 px further out than selection (x=%v, w=%v)",
			pairedRR.X, pairedRR.W, selRR.X-3, selRR.W+6)
	}

	// Selection has 4 corner handles (8x8 rounded rects); Agent has 0 handles.
	countHandles := func(ops []core.DrawOp) int {
		n := 0
		for _, op := range ops {
			if op.Kind == core.DrawRoundRect && op.W == 8 && op.H == 8 {
				n++
			}
		}
		return n
	}
	if got := countHandles(selDraw.Ops); got != 8 { // 4 fill + 4 stroke RoundRects
		t.Fatalf("selection handle RoundRect count = %d, want 8 (4 filled + 4 stroked corner handles)", got)
	}
	if got := countHandles(agentPairedDraw.Ops); got != 0 {
		t.Fatalf("agent outline unexpectedly drew %d handle RoundRects, want 0", got)
	}
}

// #1006 Acceptance 4: Change overlays fade in over 240 ms and Agent pulses 1.6 s between 100% and 45%;
// with reduced motion on, nothing fades or pulses.
func TestViewportOverlayMotionAndReduceMotion(t *testing.T) {
	// 1. Change fade-in math (240 ms).
	if got := editor.OverlayFadeAlpha(0, false); got != 0 {
		t.Fatalf("OverlayFadeAlpha(0, false) = %v, want 0", got)
	}
	if got := editor.OverlayFadeAlpha(120*time.Millisecond, false); math.Abs(float64(got-0.5)) > 0.01 {
		t.Fatalf("OverlayFadeAlpha(120ms, false) = %v, want ~0.5", got)
	}
	if got := editor.OverlayFadeAlpha(240*time.Millisecond, false); got != 1 {
		t.Fatalf("OverlayFadeAlpha(240ms, false) = %v, want 1", got)
	}
	if got := editor.OverlayFadeAlpha(0, true); got != 1 {
		t.Fatalf("OverlayFadeAlpha(0, reduceMotion=true) = %v, want 1", got)
	}

	// 2. Agent pulse math (1.6 s between 100% and 45%).
	if got := editor.OverlayAgentPulseAlpha(0, false); math.Abs(float64(got-1.0)) > 0.01 {
		t.Fatalf("OverlayAgentPulseAlpha(0, false) = %v, want 1.0", got)
	}
	if got := editor.OverlayAgentPulseAlpha(800*time.Millisecond, false); math.Abs(float64(got-0.45)) > 0.01 {
		t.Fatalf("OverlayAgentPulseAlpha(800ms, false) = %v, want 0.45", got)
	}
	if got := editor.OverlayAgentPulseAlpha(1600*time.Millisecond, false); math.Abs(float64(got-1.0)) > 0.01 {
		t.Fatalf("OverlayAgentPulseAlpha(1600ms, false) = %v, want 1.0", got)
	}
	for _, dt := range []time.Duration{0, 400 * time.Millisecond, 800 * time.Millisecond, 1200 * time.Millisecond} {
		if got := editor.OverlayAgentPulseAlpha(dt, true); got != 1 {
			t.Fatalf("OverlayAgentPulseAlpha(%v, reduceMotion=true) = %v, want solid 1.0", dt, got)
		}
	}

	// 3. Live Editor DrawViewport with deterministic clock and ReduceMotion toggle.
	ed := newOverlaysTestEditor(t)
	_ = ed.SetAppearance(editor.AppearanceDark)
	// Turn off unrelated scene inspection overlays (such as Navigation, which uses Spec Added.Fg
	// for walkable navregion2d polygons) so stroke assertions measure only the Spec scene overlays.
	if err := ed.SetViewportOverlays(nil); err != nil {
		t.Fatalf("SetViewportOverlays(nil): %v", err)
	}
	baseTime := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	now := baseTime
	ed.SetOverlayClock(func() time.Time { return now })

	ed.SetSceneOverlays([]editor.SceneOverlayEntry{
		{ID: "a1", ObjectID: "slime_01", Kind: editor.OverlayKindAgent, Label: "placing slime_01", StartedAt: baseTime},
		{ID: "c1", ObjectID: "knight", Kind: editor.OverlayKindAdded, Label: "knight", StartedAt: baseTime},
	})

	spec := editor.DarkSpecPalette()
	findStrokeColor := func(ops []core.DrawOp, rgb core.Color) (uint8, bool) {
		wantRGB := uint32(rgb) >> 8
		for _, op := range ops {
			if op.Kind == core.DrawStrokeColor && (uint32(op.Color)>>8) == wantRGB {
				return uint8(op.Color & 0xFF), true
			}
		}
		return 0, false
	}

	// With ReduceMotion OFF at t=0: Added overlay starts at alpha 0 (no stroke emitted yet).
	ed.SetReduceMotion(false)
	var d0 core.Drawing
	ed.DrawViewport(&d0, core.Size{Width: 640, Height: 360})
	if _, ok := findStrokeColor(d0.Ops, spec.Added.Edge); ok {
		t.Fatal("expected Added overlay at t=0 without ReduceMotion to start at 0 alpha")
	}

	// At t=800ms without ReduceMotion: Agent outline pulses down to 45% (~115/255), Added is at 100%.
	now = baseTime.Add(800 * time.Millisecond)
	var d800 core.Drawing
	ed.DrawViewport(&d800, core.Size{Width: 640, Height: 360})
	agentA, okA := findStrokeColor(d800.Ops, spec.Agent.Edge)
	if !okA || agentA > 130 || agentA < 100 {
		t.Fatalf("Agent stroke alpha at t=800ms = %d (found=%v), want ~115 (45%%)", agentA, okA)
	}
	addedA, okAdd := findStrokeColor(d800.Ops, spec.Added.Edge)
	if !okAdd || addedA != 255 {
		t.Fatalf("Added stroke alpha at t=800ms = %d (found=%v), want 255", addedA, okAdd)
	}

	// With ReduceMotion ON via core.Environment: at t=0 Added appears immediately at 100%,
	// and at t=800ms Agent stays solid at 100%!
	ed.ClearReduceMotion()
	now = baseTime
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 720), nsgotest.Environment(core.Environment{
		OS:           core.OSMacOS,
		Idiom:        core.IdiomDesktop,
		Dark:         true,
		ReduceMotion: true,
	}))
	app.Settle()

	if !ed.ReduceMotion() {
		t.Fatal("ed.ReduceMotion() = false under Environment{ReduceMotion: true}, want true")
	}
	var rm0 core.Drawing
	ed.DrawViewport(&rm0, core.Size{Width: 640, Height: 360})
	if a, ok := findStrokeColor(rm0.Ops, spec.Added.Edge); !ok || a != 255 {
		t.Fatalf("with ReduceMotion at t=0, Added alpha = %d (found=%v), want instant 255", a, ok)
	}
	now = baseTime.Add(800 * time.Millisecond)
	var rm800 core.Drawing
	ed.DrawViewport(&rm800, core.Size{Width: 640, Height: 360})
	if a, ok := findStrokeColor(rm800.Ops, spec.Agent.Edge); !ok || a != 255 {
		t.Fatalf("with ReduceMotion at t=800ms, Agent alpha = %d (found=%v), want solid 255", a, ok)
	}
}

// #1006 Acceptance 3: Top-left legend renders six role="switch" toggles in both 2D and 3D viewports,
// turning a row off hides that kind, and the choice persists in EditorSettings YAML across restarts.
func TestViewportOverlayLegendTogglesAndRestartPersistence(t *testing.T) {
	ed, projDir := openTestProject(t)
	_ = ed.SetAppearance(editor.AppearanceDark)
	settingsPath := filepath.Join(t.TempDir(), "user-config", "editor.yaml")
	if err := ed.LoadSettings(settingsPath); err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}

	// Seed every overlay kind onto an existing object in the open scene (never mutating the scene file).
	var objID string
	if ed.Doc != nil {
		for _, layer := range ed.Doc.Layers {
			if len(layer.Objects) > 0 {
				objID = layer.Objects[0].ID
				break
			}
		}
	}
	if objID == "" {
		var err error
		objID, err = ed.PlaceObject(ed.ActiveLayer, sceneformat.ObjectDoc{ID: "knight", Name: "Knight", X: 160, Y: 180})
		if err != nil {
			t.Fatal(err)
		}
		if ed.ScenePath != "" {
			if err := ed.SaveFile(ed.ScenePath); err != nil {
				t.Fatal(err)
			}
		}
	}

	gamePath := filepath.Join(projDir, "game.yaml")
	scenePath := filepath.Join(projDir, "scenes", "room.scene.yaml")
	if ed.ScenePath != "" {
		if filepath.IsAbs(ed.ScenePath) {
			scenePath = ed.ScenePath
		} else {
			scenePath = filepath.Join(projDir, ed.ScenePath)
		}
	}
	gameBefore, err := os.ReadFile(gamePath)
	if err != nil {
		t.Fatalf("read game.yaml before: %v", err)
	}
	sceneBefore, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatalf("read %s before: %v", scenePath, err)
	}
	ed.SelectObject(objID)
	ed.SetReduceMotion(true)
	ed.SetSceneOverlays([]editor.SceneOverlayEntry{
		{ID: "ag", ObjectID: objID, Kind: editor.OverlayKindAgent, Label: "tuning knight"},
		{ID: "ad", ObjectID: objID, Kind: editor.OverlayKindAdded, Label: "new_slime"},
		{ID: "ch", ObjectID: objID, Kind: editor.OverlayKindChanged, Label: "z 10 → 12"},
		{ID: "rm", ObjectID: "gone_coin", Kind: editor.OverlayKindRemoved, Label: "coin_04", GhostBounds: &core.Bounds{Origin: core.Point{X: 260, Y: 180}, Size: core.Size{Width: 20, Height: 20}}},
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:           core.OSMacOS,
		Idiom:        core.IdiomDesktop,
		Dark:         true,
		ReduceMotion: true,
	}))
	app.Settle()

	// 1. Verify top-left legend is shown in 2D with all 6 role="switch" rows checked.
	if !app.Exists("id:viewport-overlay-legend") {
		t.Fatal("expected top-left viewport-overlay-legend in 2D viewport")
	}
	for _, k := range editor.OverlayLegendKinds() {
		row := app.Find("id:legend-" + string(k))
		if row.Props.Role != core.RoleSwitch {
			t.Errorf("legend-%s Role = %v, want RoleSwitch", k, row.Props.Role)
		}
		if !row.Props.Checked {
			t.Errorf("legend-%s Checked = false, want true by default", k)
		}
	}

	// Verify 2D viewport initially draws Selection, Agent, Added, Changed, and Removed labels.
	vp := app.Find("id:editor-viewport")
	for _, wantText := range []string{"tuning knight", "+ new_slime", "z 10 → 12", "coin_04"} {
		if !hasOverlayFillText(vp.Ops, wantText) {
			t.Fatalf("2D viewport missing overlay text %q before toggling legend off", wantText)
		}
	}

	// 2. Toggle Selection, Added, and Removed OFF via the top-left legend switches.
	app.Press("id:legend-selection")
	app.Settle()
	app.Press("id:legend-added")
	app.Settle()
	app.Press("id:legend-removed")
	app.Settle()

	if app.Find("id:legend-selection").Props.Checked {
		t.Fatal("legend-selection still checked after pressing switch")
	}
	if app.Find("id:legend-added").Props.Checked {
		t.Fatal("legend-added still checked after pressing switch")
	}
	if app.Find("id:legend-removed").Props.Checked {
		t.Fatal("legend-removed still checked after pressing switch")
	}

	vpAfter := app.Find("id:editor-viewport")
	if hasOverlayFillText(vpAfter.Ops, "+ new_slime") {
		t.Fatal("Added overlay still drew after turning legend-added off")
	}
	if hasOverlayFillText(vpAfter.Ops, "coin_04") {
		t.Fatal("Removed overlay still drew after turning legend-removed off")
	}
	if !hasOverlayFillText(vpAfter.Ops, "tuning knight") || !hasOverlayFillText(vpAfter.Ops, "z 10 → 12") {
		t.Fatal("turning off other legend kinds hid Agent or Changed overlays unexpectedly")
	}

	// 3. Switch to 3D viewport: the SAME legend is shown and honors the SAME toggle choices!
	app.Post(func() { ed.SetViewMode(editor.ViewMode3D) })
	app.Settle()
	if !app.Exists("id:viewport-overlay-legend") {
		t.Fatal("expected the same viewport-overlay-legend in 3D viewport")
	}
	if app.Find("id:legend-added").Props.Checked {
		t.Fatal("3D viewport legend-added did not reflect persisted off state")
	}
	vp3D := app.Find("id:editor-viewport")
	if hasOverlayFillText(vp3D.Ops, "+ new_slime") {
		t.Fatal("3D viewport drew Added overlay while legend-added was off")
	}
	if !hasOverlayFillText(vp3D.Ops, "tuning knight") || !hasOverlayFillText(vp3D.Ops, "z 10 → 12") {
		t.Fatal("3D viewport missing active Agent/Changed overlays through shared helper")
	}

	// 4. Verify project files were untouched and EditorSettings YAML persists choices across restart.
	gameAfter, err := os.ReadFile(gamePath)
	if err != nil {
		t.Fatalf("read game.yaml after: %v", err)
	}
	sceneAfter, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatalf("read %s after: %v", scenePath, err)
	}
	if !bytes.Equal(gameBefore, gameAfter) || !bytes.Equal(sceneBefore, sceneAfter) {
		t.Fatal("toggling overlay legend modified project or scene files")
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read persisted settings: %v", err)
	}
	if !strings.Contains(string(raw), "overlayLegend:") {
		t.Fatalf("persisted settings missing overlayLegend:\n%s", raw)
	}

	restarted, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New (restarted): %v", err)
	}
	if err := restarted.LoadSettings(settingsPath); err != nil {
		t.Fatalf("restarted.LoadSettings: %v", err)
	}
	wantActive := []editor.OverlayKind{
		editor.OverlayKindAgent,
		editor.OverlayKindChanged,
		editor.OverlayKindProblem,
	}
	if got := restarted.ActiveOverlayLegendKinds(); !slices.Equal(got, wantActive) {
		t.Fatalf("restarted ActiveOverlayLegendKinds = %v, want %v", got, wantActive)
	}

	// Turning all 6 off writes explicit overlayLegend: [] and survives restart without reverting to defaults.
	if err := restarted.SetOverlayLegendKinds(nil); err != nil {
		t.Fatalf("SetOverlayLegendKinds(nil): %v", err)
	}
	emptyRaw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(emptyRaw), "overlayLegend: []") {
		t.Fatalf("expected explicit overlayLegend: [] in settings YAML, got:\n%s", emptyRaw)
	}
	restartedEmpty, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restartedEmpty.LoadSettings(settingsPath); err != nil {
		t.Fatal(err)
	}
	if got := restartedEmpty.ActiveOverlayLegendKinds(); len(got) != 0 {
		t.Fatalf("restartedEmpty ActiveOverlayLegendKinds = %v, want empty", got)
	}

	// Invalid and duplicate overlayLegend values are refused by ParseEditorSettings.
	for name, bad := range map[string]string{
		"unknown":   "version: 1\nlanguage: system\nappearance: system\noverlayLegend:\n  - bogus\n",
		"duplicate": "version: 1\nlanguage: system\nappearance: system\noverlayLegend:\n  - selection\n  - selection\n",
	} {
		if _, err := editor.ParseEditorSettings([]byte(bad)); err == nil {
			t.Errorf("ParseEditorSettings(%s) succeeded, want error", name)
		}
	}
}

// #1006 Scope: Problems panel problems naming an object in the open scene draw the Problem overlay
// (2 px solid + filled alert pill) in both 2D and 3D viewports, and assetcheck.Problem.Object is populated.
func TestViewportOverlayProblemsPanelIntegration2DAnd3D(t *testing.T) {
	ed, dir := setupLiveProblemFixture(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	// Rename door_3 -> gate so boss_room's connection ("lever" pulled -> "door_3" open) breaks.
	if err := ed.OpenScene("scenes/doors.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectID("door_3", "gate"); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	probs := ed.Problems()
	if len(probs) != 1 {
		t.Fatalf("expected 1 live problem, got %+v", probs)
	}
	if probs[0].Object != "lever" {
		t.Fatalf("live problem Object = %q, want %q", probs[0].Object, "lever")
	}

	if err := ed.OpenScene("scenes/boss_room.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	ed.SetCanvasSize(640, 360)

	// 2D viewport draws the Problem overlay on live object "lever".
	var d2 core.Drawing
	ed.DrawViewport(&d2, core.Size{Width: 640, Height: 360})
	if !hasFillTextContaining(d2.Ops, `target "door_3" not found`) {
		t.Fatal("2D viewport missing Problem overlay pill on live object lever")
	}

	// 3D viewport also draws the Problem overlay on live object "lever".
	ed.SetViewMode(editor.ViewMode3D)
	ed.FrameSelection()
	var d3 core.Drawing
	ed.DrawViewport(&d3, core.Size{Width: 640, Height: 360})
	if !hasFillTextContaining(d3.Ops, `target "door_3" not found`) {
		t.Fatal("3D viewport missing Problem overlay pill on live object lever")
	}

	// Turning Problem off in the legend hides it in both 3D and 2D!
	if err := ed.SetOverlayKindVisible(editor.OverlayKindProblem, false); err != nil {
		t.Fatal(err)
	}
	var d3Off core.Drawing
	ed.DrawViewport(&d3Off, core.Size{Width: 640, Height: 360})
	if hasFillTextContaining(d3Off.Ops, `target "door_3" not found`) {
		t.Fatal("3D viewport still drew Problem overlay after turning Problem legend toggle off")
	}
	ed.SetViewMode(editor.ViewMode2D)
	var d2Off core.Drawing
	ed.DrawViewport(&d2Off, core.Size{Width: 640, Height: 360})
	if hasFillTextContaining(d2Off.Ops, `target "door_3" not found`) {
		t.Fatal("2D viewport still drew Problem overlay after turning Problem legend toggle off")
	}
}

// #1006 Scope: Camera frame, camera bounds, and tile grid use Spec neutral tokens in a style
// distinct from all semantic legend kinds (never Accent, Agent, Added, Changed, Removed, Problem).
func TestCameraFrameBoundsAndTileGridUseNeutralSpecTokens(t *testing.T) {
	ed := newOverlaysTestEditor(t)
	_ = ed.SetAppearance(editor.AppearanceDark)
	ed.ClearSelection() // ShowingSceneSettings() == true -> highlights camera frame & bounds
	ed.ShowGrid = true

	spec := editor.DarkSpecPalette()
	forbiddenLegendRGBs := map[uint32]string{
		uint32(spec.Accent.Edge) >> 8:  "accent",
		uint32(spec.Agent.Edge) >> 8:   "agent",
		uint32(spec.Added.Edge) >> 8:   "added",
		uint32(spec.Changed.Edge) >> 8: "changed",
		uint32(spec.Removed.Edge) >> 8: "removed",
		0x38BDF8:                       "legacy sky blue",
	}

	// Draw with only camera_bounds overlay enabled (no selection, no semantic overlays).
	var d core.Drawing
	if err := ed.DrawViewportWithOverlays(&d, core.Size{Width: 640, Height: 360}, []string{editor.OverlayCameraBounds}); err != nil {
		t.Fatal(err)
	}
	for _, op := range d.Ops {
		if op.Kind == core.DrawStrokeColor {
			rgb := uint32(op.Color) >> 8
			if name, bad := forbiddenLegendRGBs[rgb]; bad {
				t.Errorf("camera frame/bounds/snap grid used %s color #%06X; must use neutral Spec tokens distinct from legend kinds", name, rgb)
			}
		}
	}
}

// #1006 Acceptance 5: Real nsgotest screenshot on macOS showing live overlays and the top-left
// toggle legend in both Dark and Light Spec themes.
func TestViewportOverlaysSpecMacOSScreenshot(t *testing.T) {
	ed := newOverlaysTestEditor(t)
	ed.SelectObject("knight")
	ed.SetReduceMotion(true)
	ed.SetSceneOverlays([]editor.SceneOverlayEntry{
		{ID: "ag", ObjectID: "knight", Kind: editor.OverlayKindAgent, Label: "tuning jump"},
		{ID: "ad", ObjectID: "slime_02", Kind: editor.OverlayKindAdded, Label: "slime_02"},
		{ID: "ch", ObjectID: "slime_01", Kind: editor.OverlayKindChanged, Label: "z 10 → 12"},
		{ID: "rm", ObjectID: "coin_04", Kind: editor.OverlayKindRemoved, Label: "coin_04", GhostBounds: &core.Bounds{Origin: core.Point{X: 330, Y: 256}, Size: core.Size{Width: 16, Height: 16}}},
		{ID: "fc", ObjectID: "door_01", Kind: editor.OverlayKindFocusedChange, ChangeKind: editor.OverlayKindAdded, Label: "door_01"},
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:           core.OSMacOS,
		Idiom:        core.IdiomDesktop,
		Dark:         true,
		ReduceMotion: true,
	}))
	app.Settle()
	app.Screenshot("viewport-overlays-spec-dark")

	app.SetEnvironment(core.Environment{
		OS:           core.OSMacOS,
		Idiom:        core.IdiomDesktop,
		Dark:         false,
		ReduceMotion: true,
	})
	app.Settle()
	app.Screenshot("viewport-overlays-spec-light")
}

// Regression test: Play mode and History preview hide overlays and legend in both 2D and 3D and restore
// them cleanly; SceneOverlays are scoped per scene tab, deep-copied, and cleared on OpenDemo;
// toggling a legend switch does not remount or steal keyboard focus.
func TestViewportOverlayLifecyclePlayHistoryAndFocus(t *testing.T) {
	ed, _ := openTestProject(t)
	ed.SetReduceMotion(true)

	var objID string
	for _, layer := range ed.Doc.Layers {
		if len(layer.Objects) > 0 {
			objID = layer.Objects[0].ID
			break
		}
	}
	if objID == "" {
		var err error
		objID, err = ed.PlaceObject(ed.ActiveLayer, sceneformat.ObjectDoc{ID: "knight", Name: "Knight", X: 160, Y: 180})
		if err != nil {
			t.Fatal(err)
		}
	}
	ed.SelectObject(objID)

	ghost := core.Bounds{Origin: core.Point{X: 200, Y: 160}, Size: core.Size{Width: 20, Height: 20}}
	ghostPos3D := [3]float32{0, 0, 0}
	ghostSize3D := [3]float32{32, 48, 24}
	ed.SetSceneOverlays([]editor.SceneOverlayEntry{
		{ID: "ad", ObjectID: objID, Kind: editor.OverlayKindAdded, Label: "new_slime"},
		{ID: "rm", ObjectID: "gone", Kind: editor.OverlayKindRemoved, Label: "coin_04", GhostBounds: &ghost, GhostPos3D: &ghostPos3D, GhostSize3D: &ghostSize3D},
	})

	// 1. SceneOverlays() returns a deep copy that does not alias internal pointers.
	snap := ed.SceneOverlays()
	if len(snap) != 2 || snap[1].GhostBounds == nil || snap[1].GhostSize3D == nil {
		t.Fatalf("SceneOverlays() = %+v, want 2 entries with ghost pointers", snap)
	}
	snap[1].GhostBounds.Origin.X = 9999
	snap[1].GhostSize3D[0] = 9999
	snap2 := ed.SceneOverlays()
	if snap2[1].GhostBounds.Origin.X == 9999 || snap2[1].GhostSize3D[0] == 9999 {
		t.Fatal("mutating SceneOverlays() result mutated internal editor state")
	}

	// 2. Play mode hides overlays and legend in both 2D and 3D, and Stop() restores them.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 720), nsgotest.Environment(core.Environment{
		OS:           core.OSMacOS,
		Idiom:        core.IdiomDesktop,
		Dark:         true,
		ReduceMotion: true,
	}))
	app.Settle()

	if !app.Exists("id:viewport-overlay-legend") {
		t.Fatal("expected legend visible before Play")
	}
	// Focus legend-added and toggle it via Space: switch toggles cleanly and keeps keyboard focus (no remount).
	app.Focus("id:legend-added")
	app.Settle()
	if !app.Find("id:legend-added").Focused {
		t.Fatal("legend-added did not gain keyboard focus")
	}
	app.Key("space")
	app.Settle()
	if app.Find("id:legend-added").Props.Checked {
		t.Fatal("Space key did not toggle focused legend-added switch off")
	}
	if !app.Find("id:legend-added").Focused {
		t.Fatal("toggling legend-added via Space remounted the switch and lost keyboard focus")
	}
	// Toggle it back ON via Space so overlays are active for Play & History checks.
	app.Key("space")
	app.Settle()
	if !app.Find("id:legend-added").Props.Checked || !app.Find("id:legend-added").Focused {
		t.Fatal("second Space key press did not restore legend-added while keeping focus")
	}

	app.Post(func() { _ = ed.Play() })
	app.Settle()
	if app.Exists("id:viewport-overlay-legend") {
		t.Fatal("legend stayed visible during Play mode")
	}
	var play2D core.Drawing
	ed.DrawViewport(&play2D, core.Size{Width: 640, Height: 360})
	if hasOverlayFillText(play2D.Ops, "+ new_slime") {
		t.Fatal("2D overlays drew during Play mode")
	}
	ed.SetViewMode(editor.ViewMode3D)
	var play3D core.Drawing
	ed.DrawViewport(&play3D, core.Size{Width: 640, Height: 360})
	if hasOverlayFillText(play3D.Ops, "+ new_slime") || hasOverlayFillText(play3D.Ops, objID) {
		t.Fatal("3D overlays drew during Play mode")
	}

	app.Post(func() {
		_ = ed.Stop()
		ed.SetViewMode(editor.ViewMode2D)
	})
	app.Settle()
	if !app.Exists("id:viewport-overlay-legend") {
		t.Fatal("legend did not restore after Stop()")
	}

	// 3. History preview hides overlays & legend in both 2D and 3D and restores them on exit.
	if _, err := ed.PlaceObject(ed.ActiveLayer, sceneformat.ObjectDoc{ID: "hist_prop", Name: "HistProp", X: 80, Y: 80}); err != nil {
		t.Fatal(err)
	}
	ed.SetHistoryPanelOpen(true)
	entries := ed.HistoryEntries()
	if len(entries) == 0 {
		t.Fatal("expected at least one history entry")
	}
	app.Post(func() { _ = ed.PreviewHistoryEntry(entries[0].ID) })
	app.Settle()
	if !ed.HistoryPreviewActive() {
		t.Fatal("expected HistoryPreviewActive()")
	}
	if app.Exists("id:viewport-overlay-legend") {
		t.Fatal("legend stayed visible during History preview")
	}
	var hist2D core.Drawing
	ed.DrawViewport(&hist2D, core.Size{Width: 640, Height: 360})
	if hasOverlayFillText(hist2D.Ops, "+ new_slime") {
		t.Fatal("2D overlays drew during History preview")
	}
	ed.SetViewMode(editor.ViewMode3D)
	var hist3D core.Drawing
	ed.DrawViewport(&hist3D, core.Size{Width: 640, Height: 360})
	if hasOverlayFillText(hist3D.Ops, "+ new_slime") || hasOverlayFillText(hist3D.Ops, "coin_04") {
		t.Fatal("3D overlays drew during History preview")
	}
	app.Post(func() {
		ed.ExitHistoryPreview()
		ed.SetViewMode(editor.ViewMode2D)
	})
	app.Settle()
	if !app.Exists("id:viewport-overlay-legend") {
		t.Fatal("legend did not restore after ExitHistoryPreview()")
	}

	// 3b. Active 2D Sketch mode hides the top-left legend so it never collides with the floating Sketch toolbar,
	// and finishing Sketch mode restores the legend immediately.
	app.Post(func() { ed.SetSketchActive(true) })
	app.Settle()
	if !app.Exists("id:viewport-sketch-toolbar") {
		t.Fatal("expected viewport-sketch-toolbar while Sketch mode is active")
	}
	if app.Exists("id:viewport-overlay-legend") {
		t.Fatal("viewport-overlay-legend overlapped active viewport-sketch-toolbar")
	}
	app.Post(func() { ed.FinishSketch() })
	app.Settle()
	if !app.Exists("id:viewport-overlay-legend") {
		t.Fatal("viewport-overlay-legend did not restore after FinishSketch()")
	}

	// 4. Tab/scene lifecycle: new scene starts with clean SceneOverlays; switching back preserves tab 0;
	// OpenDemo clears all scene overlays.
	if _, err := ed.NewScene("scenes/second.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if got := ed.SceneOverlays(); len(got) != 0 {
		t.Fatalf("new scene tab inherited stale SceneOverlays: %+v", got)
	}
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	if got := ed.SceneOverlays(); len(got) != 2 {
		t.Fatalf("switching back to tab 0 lost SceneOverlays: got %d, want 2", len(got))
	}
	if err := ed.OpenDemo(); err != nil {
		t.Fatal(err)
	}
	if got := ed.SceneOverlays(); len(got) != 0 {
		t.Fatalf("OpenDemo() left stale SceneOverlays: %+v", got)
	}
}

// Regression test: adding a second change overlay after a first 240 ms fade completes re-arms the
// viewport fade animation timer so the new overlay smoothly animates to full opacity.
func TestViewportOverlaySequentialFadeRearmsTimer(t *testing.T) {
	ed := newOverlaysTestEditor(t)
	_ = ed.SetAppearance(editor.AppearanceDark)
	ed.ClearReduceMotion()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	ed.SetOverlayClock(func() time.Time { return now })

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 720), nsgotest.Environment(core.Environment{
		OS:           core.OSMacOS,
		Idiom:        core.IdiomDesktop,
		Dark:         true,
		ReduceMotion: false,
	}))
	app.Settle()

	// 1. First Added overlay starts fading and finishes after 240 ms.
	app.Post(func() {
		ed.AddSceneOverlay(editor.SceneOverlayEntry{ID: "first", ObjectID: "knight", Kind: editor.OverlayKindAdded, Label: "knight", StartedAt: now})
	})
	app.Settle()
	now = now.Add(300 * time.Millisecond)
	app.Advance(40 * time.Millisecond)
	app.Settle()

	// 2. Second Added overlay starts at t=300ms (initially alpha 0); timer must re-arm and redraw it at 100%.
	secondStart := now
	app.Post(func() {
		ed.AddSceneOverlay(editor.SceneOverlayEntry{ID: "second", ObjectID: "slime_01", Kind: editor.OverlayKindAdded, Label: "slime_01", StartedAt: secondStart})
	})
	app.Settle()
	if hasOverlayFillText(app.Find("id:editor-viewport").Ops, "+ slime_01") {
		t.Fatal("expected second Added overlay to start invisible at t=0")
	}

	now = secondStart.Add(300 * time.Millisecond)
	app.Advance(50 * time.Millisecond)
	app.Settle()
	if !hasOverlayFillText(app.Find("id:editor-viewport").Ops, "+ slime_01") {
		t.Fatal("sequential Added overlay stayed invisible: fade animation timer did not re-arm")
	}
}
