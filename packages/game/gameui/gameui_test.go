package gameui_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func assertNear(t *testing.T, label string, got, want float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > 1e-3 {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func assertBounds(t *testing.T, name string, got core.Bounds, x, y, w, h float32) {
	t.Helper()
	assertNear(t, name+".X", float32(got.Origin.X), x)
	assertNear(t, name+".Y", float32(got.Origin.Y), y)
	assertNear(t, name+".W", float32(got.Size.Width), w)
	assertNear(t, name+".H", float32(got.Size.Height), h)
}

func boundsOverlap(a, b core.Bounds) bool {
	ax0, ay0 := float32(a.Origin.X), float32(a.Origin.Y)
	ax1, ay1 := ax0+float32(a.Size.Width), ay0+float32(a.Size.Height)
	bx0, by0 := float32(b.Origin.X), float32(b.Origin.Y)
	bx1, by1 := bx0+float32(b.Size.Width), by0+float32(b.Size.Height)
	return ax0 < bx1-1e-3 && ax1 > bx0+1e-3 && ay0 < by1-1e-3 && ay1 > by0+1e-3
}

func boundsInside(child, parent core.Bounds) bool {
	cx0, cy0 := float32(child.Origin.X), float32(child.Origin.Y)
	cx1, cy1 := cx0+float32(child.Size.Width), cy0+float32(child.Size.Height)
	px0, py0 := float32(parent.Origin.X), float32(parent.Origin.Y)
	px1, py1 := px0+float32(parent.Size.Width), py0+float32(parent.Size.Height)
	return cx0 >= px0-1e-3 && cy0 >= py0-1e-3 && cx1 <= px1+1e-3 && cy1 <= py1+1e-3
}

func centerOf(b core.Bounds) core.Point {
	return core.Point{
		X: b.Origin.X + b.Size.Width/2,
		Y: b.Origin.Y + b.Size.Height/2,
	}
}

func TestLayoutNestedPanelVBoxHBox(t *testing.T) {
	title := &gameui.Label{Text: "Options", Height: 24}
	btnCancel := &gameui.Button{Text: "Cancel", Flex: 1}
	btnApply := &gameui.Button{Text: "Apply", Flex: 2}
	btnFixed := &gameui.Button{Text: "?", Width: 40}
	row := &gameui.HBox{
		Height:   40,
		Spacing:  10,
		Children: []gameui.Widget{btnCancel, btnApply, btnFixed},
	}
	spacer := &gameui.Spacer{} // defaults to Flex: 1
	toggle := &gameui.Toggle{Label: "Music", Height: 30}
	slider := &gameui.Slider{Min: 0, Max: 100, Value: 50, Height: 28}
	progress := &gameui.ProgressBar{Value: 0.6, Height: 16}
	hidden := &gameui.Button{Text: "Hidden", Height: 50, Hidden: true}

	vbox := &gameui.VBox{
		Padding:  core.Symmetric(8, 12),
		Spacing:  8,
		Children: []gameui.Widget{title, row, hidden, spacer, toggle, slider, progress},
	}
	panel := &gameui.Panel{
		Padding: core.All(16),
		Child:   vbox,
	}
	root := gameui.NewRoot(panel)

	canvasBounds := core.Bounds{
		Origin: core.Point{X: 20, Y: 10},
		Size:   core.Size{Width: 400, Height: 300},
	}
	root.Layout(canvasBounds)

	// Panel fills canvasBounds; VBox sits inset by 16 on each side.
	assertBounds(t, "panel", panel.Bounds(), 20, 10, 400, 300)
	assertBounds(t, "vbox", vbox.Bounds(), 36, 26, 368, 268)

	// VBox inner: x = 36+12 = 48, y = 26+8 = 34, w = 368-24 = 344, h = 268-16 = 252.
	// 6 visible children -> 5 gaps * 8 = 40 spacing.
	// Fixed heights: 24 + 40 + 30 + 28 + 16 = 138.
	// Spacer flex height: 252 - 40 - 138 = 74.
	assertBounds(t, "title", title.Bounds(), 48, 34, 344, 24)
	assertBounds(t, "row", row.Bounds(), 48, 66, 344, 40)
	assertBounds(t, "hidden", hidden.Bounds(), 0, 0, 0, 0)
	assertBounds(t, "spacer", spacer.Bounds(), 48, 114, 344, 74)
	assertBounds(t, "toggle", toggle.Bounds(), 48, 196, 344, 30)
	assertBounds(t, "slider", slider.Bounds(), 48, 234, 344, 28)
	assertBounds(t, "progress", progress.Bounds(), 48, 270, 344, 16)

	// HBox inner: x = 48, y = 66, w = 344, h = 40.
	// 3 children -> 2 gaps * 10 = 20 spacing; fixed width = 40; flex pool = 344 - 20 - 40 = 284.
	// Flex 1:2 -> 284/3 and 568/3.
	w1 := float32(284) / 3
	w2 := float32(568) / 3
	assertBounds(t, "btnCancel", btnCancel.Bounds(), 48, 66, w1, 40)
	assertBounds(t, "btnApply", btnApply.Bounds(), 48+w1+10, 66, w2, 40)
	assertBounds(t, "btnFixed", btnFixed.Bounds(), 48+w1+10+w2+10, 66, 40, 40)

	// Verify all leaf widgets are strictly inside panel and pairwise non-overlapping.
	leaves := []gameui.Widget{title, btnCancel, btnApply, btnFixed, spacer, toggle, slider, progress}
	for i, a := range leaves {
		if !boundsInside(a.Bounds(), panel.Bounds()) {
			t.Errorf("leaf %d bounds %+v escape panel %+v", i, a.Bounds(), panel.Bounds())
		}
		for j := i + 1; j < len(leaves); j++ {
			if boundsOverlap(a.Bounds(), leaves[j].Bounds()) {
				t.Errorf("leaf %d %+v overlaps leaf %d %+v", i, a.Bounds(), j, leaves[j].Bounds())
			}
		}
	}
}

func TestLayoutAlignmentAndJustify(t *testing.T) {
	a := &gameui.Button{Text: "A", Width: 80, Height: 30, Align: gameui.AlignStart}
	b := &gameui.Button{Text: "B", Width: 100, Height: 30, Align: gameui.AlignCenter}
	c := &gameui.Button{Text: "C", Width: 60, Height: 30, Align: gameui.AlignEnd}
	box := &gameui.VBox{
		Spacing:  10,
		Justify:  gameui.JustifyCenter,
		Children: []gameui.Widget{a, b, c},
	}
	root := gameui.NewRoot(box)
	root.Layout(core.Bounds{Size: core.Size{Width: 200, Height: 200}})

	// Total height = 3*30 + 2*10 = 110; leftover = 90 -> centered starts at y = 45.
	assertBounds(t, "a", a.Bounds(), 0, 45, 80, 30)
	assertBounds(t, "b", b.Bounds(), 50, 85, 100, 30)
	assertBounds(t, "c", c.Bounds(), 140, 125, 60, 30)
}

func TestPointerAndTouchInteraction(t *testing.T) {
	var clicks int
	var toggleStates []bool
	var sliderValues []float32
	var disabledClicks int

	btn := &gameui.Button{
		Text:    "Play",
		Height:  40,
		OnClick: func() { clicks++ },
	}
	disabledBtn := &gameui.Button{
		Text:     "Locked",
		Height:   40,
		Disabled: true,
		OnClick:  func() { disabledClicks++ },
	}
	toggle := &gameui.Toggle{
		Label:    "Fullscreen",
		Height:   36,
		Style:    gameui.ToggleSwitch,
		OnChange: func(v bool) { toggleStates = append(toggleStates, v) },
	}
	slider := &gameui.Slider{
		Min:      20,
		Max:      120,
		Value:    20,
		Height:   32,
		OnChange: func(v float32) { sliderValues = append(sliderValues, v) },
	}

	root := gameui.NewRoot(&gameui.Panel{
		Padding: core.All(10),
		Child: &gameui.VBox{
			Spacing:  8,
			Children: []gameui.Widget{btn, disabledBtn, toggle, slider},
		},
	})
	bounds := core.Bounds{Size: core.Size{Width: 300, Height: 240}}
	root.Layout(bounds)

	// Hover, press, and release inside Button activates OnClick.
	btnPt := centerOf(btn.Bounds())
	if !root.OnPointerMove(btnPt) || root.Hovered() != btn {
		t.Fatalf("expected hover over btn, got %v", root.Hovered())
	}
	if !root.OnPointerDown(btnPt) || root.Pressed() != btn || root.Focused() != btn {
		t.Fatalf("expected btn pressed and focused")
	}
	if clicks != 0 {
		t.Fatalf("OnClick fired before pointer up")
	}
	if !root.OnPointerUp(btnPt) || clicks != 1 {
		t.Fatalf("expected 1 click after pointer up, got %d", clicks)
	}

	// Pressing Button and releasing outside cancels activation.
	root.OnPointerDown(btnPt)
	root.OnPointerUp(core.Point{X: 299, Y: 239})
	if clicks != 1 {
		t.Fatalf("click should cancel when released outside button, got %d", clicks)
	}

	// Disabled button ignores hover, pointer down/up, and OnPress.
	disPt := centerOf(disabledBtn.Bounds())
	if root.OnPointerMove(disPt) || root.OnPointerDown(disPt) || root.OnPress(core.PressEvent{Position: disPt}) {
		t.Fatal("disabled button should not accept pointer events")
	}
	if disabledClicks != 0 {
		t.Fatalf("disabled button clicked %d times", disabledClicks)
	}

	// Clicking Toggle flips Checked and invokes OnChange.
	togPt := centerOf(toggle.Bounds())
	root.OnPointerDown(togPt)
	root.OnPointerUp(togPt)
	if !toggle.Checked || len(toggleStates) != 1 || !toggleStates[0] {
		t.Fatalf("expected toggle checked=true, got %v (%v)", toggle.Checked, toggleStates)
	}
	// OnPress also flips Toggle directly.
	if !root.OnPress(core.PressEvent{Position: togPt}) || toggle.Checked {
		t.Fatalf("expected OnPress to flip toggle back to false, got %v", toggle.Checked)
	}

	// Dragging Slider smoothly interpolates within [20, 120] and clamps at edges.
	tr := slider.TrackBounds()
	yMid := tr.Origin.Y + tr.Size.Height/2

	// Press at 25% of track -> value 45.
	p25 := core.Point{X: tr.Origin.X + tr.Size.Width*0.25, Y: yMid}
	if !root.OnPointerDown(p25) {
		t.Fatal("expected pointer down on slider to be consumed")
	}
	assertNear(t, "slider@25%", slider.Value, 45)

	// Drag smoothly to 75% even with Y moved outside slider bounds -> value 95.
	p75 := core.Point{X: tr.Origin.X + tr.Size.Width*0.75, Y: yMid + 50}
	if !root.OnPointerMove(p75) {
		t.Fatal("expected active slider drag to consume move")
	}
	assertNear(t, "slider@75%", slider.Value, 95)

	// Drag past max -> clamped to 120.
	root.OnPointerMove(core.Point{X: tr.Origin.X + tr.Size.Width + 100, Y: yMid})
	assertNear(t, "slider@max", slider.Value, 120)

	// Drag past min -> clamped to 20.
	root.OnPointerMove(core.Point{X: tr.Origin.X - 100, Y: yMid})
	assertNear(t, "slider@min", slider.Value, 20)

	// Release at midpoint -> value 70, and subsequent moves do not change value.
	p50 := core.Point{X: tr.Origin.X + tr.Size.Width*0.5, Y: yMid}
	root.OnPointerUp(p50)
	assertNear(t, "slider@50%", slider.Value, 70)

	root.OnPointerMove(core.Point{X: tr.Origin.X + tr.Size.Width, Y: yMid})
	assertNear(t, "slider after up", slider.Value, 70)
	if len(sliderValues) < 4 {
		t.Fatalf("expected OnChange callbacks during drag, got %v", sliderValues)
	}
}

func TestKeyboardAndDirectionalNavigation(t *testing.T) {
	var resumeClicks, cancelClicks, okClicks int
	var mutedChanges []bool
	var volChanges []float32

	header := &gameui.Label{Text: "Pause Menu", Height: 24}
	resumeBtn := &gameui.Button{Text: "Resume", Height: 36, OnClick: func() { resumeClicks++ }}
	lockedBtn := &gameui.Button{Text: "Cheats", Height: 36, Disabled: true}
	muteToggle := &gameui.Toggle{Label: "Mute", Height: 32, OnChange: func(v bool) { mutedChanges = append(mutedChanges, v) }}
	volSlider := &gameui.Slider{Label: "Volume", Min: 0, Max: 100, Value: 50, Step: 10, Height: 32, OnChange: func(v float32) { volChanges = append(volChanges, v) }}
	hpBar := &gameui.ProgressBar{Value: 0.75, Height: 16}
	cancelBtn := &gameui.Button{Text: "Cancel", Flex: 1}
	okBtn := &gameui.Button{Text: "OK", Flex: 1, OnClick: func() { okClicks++ }}
	cancelBtn.OnClick = func() { cancelClicks++ }

	root := gameui.NewRoot(&gameui.Panel{
		Padding: core.All(12),
		Child: &gameui.VBox{
			Spacing: 8,
			Children: []gameui.Widget{
				header,
				resumeBtn,
				lockedBtn,
				muteToggle,
				volSlider,
				hpBar,
				&gameui.Spacer{Height: 4},
				&gameui.HBox{Height: 36, Spacing: 8, Children: []gameui.Widget{cancelBtn, okBtn}},
			},
		},
	})
	bounds := core.Bounds{Size: core.Size{Width: 320, Height: 320}}
	root.Layout(bounds)

	// Focus order should skip Label, disabled Button, ProgressBar, and Spacer:
	// [0] resumeBtn, [1] muteToggle, [2] volSlider, [3] cancelBtn, [4] okBtn.
	if root.FocusIndex() != -1 {
		t.Fatalf("initial FocusIndex = %d, want -1", root.FocusIndex())
	}

	wantOrder := []gameui.Widget{resumeBtn, muteToggle, volSlider, cancelBtn, okBtn}
	for i, want := range wantOrder {
		if !root.FocusNext() {
			t.Fatalf("FocusNext %d returned false", i)
		}
		if root.FocusIndex() != i || root.Focused() != want {
			t.Fatalf("step %d: FocusIndex=%d Focused=%T, want %d %T", i, root.FocusIndex(), root.Focused(), i, want)
		}
	}
	// Wrap around forward.
	root.FocusNext()
	if root.FocusIndex() != 0 || root.Focused() != resumeBtn {
		t.Fatalf("expected wrap to 0, got %d", root.FocusIndex())
	}
	// Wrap around backward via Shift+Tab.
	if !root.OnKeyDown(core.KeyEvent{Key: "tab", Modifiers: core.Modifiers{Shift: true}}) {
		t.Fatal("Shift+Tab should be consumed")
	}
	if root.FocusIndex() != 4 || root.Focused() != okBtn {
		t.Fatalf("expected Shift+Tab wrap to okBtn (4), got %d", root.FocusIndex())
	}

	// Horizontal directional navigation inside HBox: okBtn -> left -> cancelBtn -> right -> okBtn.
	if !root.OnKeyDown(core.KeyEvent{Key: "left"}) || root.Focused() != cancelBtn {
		t.Fatalf("expected left from okBtn to focus cancelBtn, got %v", root.Focused())
	}
	if !root.OnKeyDown(core.KeyEvent{Key: "right"}) || root.Focused() != okBtn {
		t.Fatalf("expected right from cancelBtn to focus okBtn, got %v", root.Focused())
	}

	// Activate focused okBtn with Enter.
	if !root.OnKeyDown(core.KeyEvent{Key: "enter"}) || okClicks != 1 {
		t.Fatalf("expected Enter to click okBtn, okClicks=%d", okClicks)
	}

	// Move up from okBtn -> volSlider -> muteToggle -> resumeBtn (skipping lockedBtn and hpBar!).
	if !root.OnKeyDown(core.KeyEvent{Key: "up"}) || root.Focused() != volSlider {
		t.Fatalf("expected up to focus volSlider, got %v", root.Focused())
	}

	// Adjust volSlider via Left / Right keys.
	if !root.OnKeyDown(core.KeyEvent{Key: "right"}) {
		t.Fatal("right on slider should be consumed")
	}
	assertNear(t, "vol after right", volSlider.Value, 60)

	if !root.OnKeyDown(core.KeyEvent{Key: "left"}) {
		t.Fatal("left on slider should be consumed")
	}
	assertNear(t, "vol after left", volSlider.Value, 50)

	// Direct Adjust(+3) -> 80, and Home/End jump to Min/Max.
	if !root.Adjust(3) {
		t.Fatal("Adjust(3) on slider should succeed")
	}
	assertNear(t, "vol after Adjust(3)", volSlider.Value, 80)

	root.OnKeyDown(core.KeyEvent{Key: "end"})
	assertNear(t, "vol after End", volSlider.Value, 100)
	// Pressing right at Max stays on slider and clamps.
	if !root.OnKeyDown(core.KeyEvent{Key: "right"}) || root.Focused() != volSlider {
		t.Fatal("right at slider max should stay focused on slider")
	}
	assertNear(t, "vol clamped at max", volSlider.Value, 100)

	root.OnKeyDown(core.KeyEvent{Key: "home"})
	assertNear(t, "vol after Home", volSlider.Value, 0)
	if len(volChanges) == 0 {
		t.Fatal("expected slider OnChange to fire during keyboard adjustments")
	}

	// Move up to muteToggle and flip it with Space.
	if !root.OnKeyDown(core.KeyEvent{Key: "up"}) || root.Focused() != muteToggle {
		t.Fatalf("expected up to focus muteToggle, got %v", root.Focused())
	}
	if !root.OnKeyDown(core.KeyEvent{Key: "space"}) || !muteToggle.Checked {
		t.Fatalf("expected space to check muteToggle, checked=%v", muteToggle.Checked)
	}
	if len(mutedChanges) != 1 || !mutedChanges[0] {
		t.Fatalf("unexpected mutedChanges: %v", mutedChanges)
	}

	// Move up skips disabled lockedBtn and lands on resumeBtn.
	if !root.OnKeyDown(core.KeyEvent{Key: "up"}) || root.Focused() != resumeBtn {
		t.Fatalf("expected up to skip lockedBtn and focus resumeBtn, got %v", root.Focused())
	}
	if !root.Activate() || resumeClicks != 1 {
		t.Fatalf("expected Activate on resumeBtn, resumeClicks=%d", resumeClicks)
	}

	// Verify focused widget renders a focus border op in Theme.FocusedBorder color.
	theme := gameui.DefaultTheme()
	root.Theme = &theme
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "focused-ui",
			Style:  core.Style{Width: core.Pt(320), Height: core.Pt(320)},
			Draw: func(d *core.Drawing, size core.Size) {
				root.Draw(d, core.Bounds{Size: size})
			},
		}
	})
	ops := app.Find("focused-ui").Ops
	foundFocusBorder := false
	for _, op := range ops {
		if op.Kind == core.DrawStrokeColor && op.Color == theme.FocusedBorder {
			foundFocusBorder = true
			break
		}
	}
	if !foundFocusBorder {
		t.Fatalf("expected recorded DrawOps to contain focus ring stroke color %#x", theme.FocusedBorder)
	}
}

func TestDrawRecordsCleanOpsAndPreservesCallerState(t *testing.T) {
	var customBgBounds core.Bounds
	var customBgCalled int
	var canvasClicks int

	btn := &gameui.Button{
		Text:    "Start",
		Height:  40,
		OnClick: func() { canvasClicks++ },
		DrawBackground: func(d *core.Drawing, b core.Bounds) {
			customBgCalled++
			customBgBounds = b
			// Deliberately mutate state inside custom hook to confirm gameui isolates it.
			d.SetFillColor(core.RGB(0x112233)).SetAlpha(0.5).FillRect(
				float32(b.Origin.X), float32(b.Origin.Y), float32(b.Size.Width), float32(b.Size.Height),
			)
		},
	}
	disabledBtn := &gameui.Button{Text: "Disabled", Height: 36, Disabled: true}
	chk := &gameui.Toggle{Label: "VSync", Checked: true, Style: gameui.ToggleCheckbox, Height: 30}
	sw := &gameui.Toggle{Label: "SFX", Checked: true, Style: gameui.ToggleSwitch, Height: 30}
	sld := &gameui.Slider{Label: "Gain", Min: 0, Max: 1, Value: 0.4, Height: 30}
	bar := &gameui.ProgressBar{Value: 0.8, Label: "80%", Height: 22}

	root := gameui.NewRoot(&gameui.Panel{
		Padding: core.All(12),
		Child: &gameui.VBox{
			Spacing: 8,
			Children: []gameui.Widget{
				&gameui.Label{Text: "Settings", TextAlign: core.TextAlignCenter, Height: 24},
				btn,
				disabledBtn,
				chk,
				sw,
				sld,
				bar,
			},
		},
	})
	root.FocusNext() // Focus btn

	callerFill := core.RGB(0xee2233)
	callerStroke := core.RGB(0x22cc44)
	callerFont := core.Font{Size: 14}
	var uiOpsStart, uiOpsEnd int
	var afterInPath bool

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "game-canvas",
			Style:  core.Style{Width: core.Pt(360), Height: core.Pt(340)},
			OnPress: func(ev core.PressEvent) {
				root.OnPress(ev)
			},
			Draw: func(d *core.Drawing, size core.Size) {
				// Establish caller drawing state + an open path before drawing UI.
				d.Translate(10, 15).
					SetFillColor(callerFill).
					SetStrokeColor(callerStroke).
					SetLineWidth(4).
					SetAlpha(0.8).
					SetTextAlign(core.TextAlignEnd).
					SetTextBaseline(core.BaselineTop).
					BeginPath().
					Rect(0, 0, 50, 50)

				uiOpsStart = len(d.Ops)
				root.Draw(d, core.Bounds{Size: size})
				uiOpsEnd = len(d.Ops)

				// Caller's open path (0..50 shifted by translate 10,15 -> 10..60, 15..65) must remain intact.
				afterInPath = d.IsPointInPath(25, 30, core.NonZero)

				// Subsequent caller ops rely on restored state (FillText records current TextAlign/TextBaseline).
				d.Fill().FillText("after", 100, 100, callerFont)
			},
		}
	})

	ops := app.Find("game-canvas").Ops
	if uiOpsEnd <= uiOpsStart {
		t.Fatal("root.Draw recorded no DrawOps")
	}

	// Verify Save/Restore balance strictly inside root.Draw's emitted ops.
	saves, restores := 0, 0
	for _, op := range ops[uiOpsStart:uiOpsEnd] {
		switch op.Kind {
		case core.DrawSave:
			saves++
		case core.DrawRestore:
			restores++
		}
	}
	if saves == 0 || saves != restores {
		t.Fatalf("unbalanced Save/Restore in root.Draw: saves=%d restores=%d", saves, restores)
	}

	// Verify custom DrawBackground hook was called with the button's exact laid-out bounds.
	if customBgCalled == 0 {
		t.Fatal("expected DrawBackground hook to be invoked")
	}
	if customBgBounds != btn.Bounds() {
		t.Fatalf("DrawBackground bounds = %+v, want %+v", customBgBounds, btn.Bounds())
	}

	// Verify caller's open path and transform survived root.Draw intact.
	if !afterInPath {
		t.Fatal("caller's current path or transform was mutated by root.Draw")
	}

	// Verify caller's TextAlign and TextBaseline survived root.Draw intact.
	lastOp := ops[len(ops)-1]
	if lastOp.Kind != core.DrawFillText || lastOp.Text != "after" {
		t.Fatalf("expected final op to be caller's FillText, got %+v", lastOp)
	}
	if lastOp.TextAlign != core.TextAlignEnd || lastOp.TextBaseline != core.BaselineTop {
		t.Fatalf("caller text state mutated: align=%v baseline=%v, want End/Top", lastOp.TextAlign, lastOp.TextBaseline)
	}

	// Verify pressing on the real core.Canvas routes through OnPress to activate the Button.
	app.PressAt("game-canvas", centerOf(btn.Bounds()))
	if canvasClicks != 1 {
		t.Fatalf("expected Canvas PressAt on Start button to fire OnClick once, got %d", canvasClicks)
	}
}

func TestVisualStatesAndEdgeCases(t *testing.T) {
	// Zero-value Root must never panic on any operation.
	var empty gameui.Root
	empty.Layout(core.Bounds{Size: core.Size{Width: 100, Height: 100}})
	if empty.FocusIndex() != -1 || empty.Focused() != nil {
		t.Fatal("empty root should have no focus")
	}
	if empty.FocusNext() || empty.FocusPrev() || empty.MoveFocus(0, 1) || empty.Activate() || empty.Adjust(1) {
		t.Fatal("empty root operations should return false")
	}
	if empty.OnKeyDown(core.KeyEvent{Key: "down"}) || empty.OnPointerDown(core.Point{X: 10, Y: 10}) {
		t.Fatal("empty root input should return false")
	}

	// Verify distinct fill colors for Normal, Hover, Active (Pressed), and Disabled button states.
	theme := gameui.DefaultTheme()
	bNormal := &gameui.Button{Text: "Normal", Height: 32}
	bHover := &gameui.Button{Text: "Hover", Height: 32}
	bPressed := &gameui.Button{Text: "Pressed", Height: 32}
	bDisabled := &gameui.Button{Text: "Disabled", Height: 32, Disabled: true}

	root := &gameui.Root{
		Theme: &theme,
		Child: &gameui.VBox{
			Spacing:  6,
			Children: []gameui.Widget{bNormal, bHover, bPressed, bDisabled},
		},
	}
	bounds := core.Bounds{Size: core.Size{Width: 200, Height: 200}}
	root.Layout(bounds)

	// Press bPressed, then hover bHover (using separate roots or capturing ops for each state).
	root.OnPointerDown(centerOf(bPressed.Bounds()))
	appPressed := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "states",
			Style:  core.Style{Width: core.Pt(200), Height: core.Pt(200)},
			Draw: func(d *core.Drawing, size core.Size) {
				root.Draw(d, core.Bounds{Size: size})
			},
		}
	})
	hasFill := func(ops []core.DrawOp, c core.Color) bool {
		for _, op := range ops {
			if op.Kind == core.DrawFillColor && op.Color == c {
				return true
			}
		}
		return false
	}
	opsPressed := appPressed.Find("states").Ops
	for name, col := range map[string]core.Color{
		"Background": theme.Background,
		"Active":     theme.Active,
		"Disabled":   theme.Disabled,
	} {
		if !hasFill(opsPressed, col) {
			t.Errorf("expected fill color %s (%#x) in recorded ops", name, col)
		}
	}

	// Release and hover bHover to confirm Hover color appears.
	root.OnPointerUp(core.Point{X: 199, Y: 199})
	root.OnPointerMove(centerOf(bHover.Bounds()))
	appHover := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "hover",
			Style:  core.Style{Width: core.Pt(200), Height: core.Pt(200)},
			Draw: func(d *core.Drawing, size core.Size) {
				root.Draw(d, core.Bounds{Size: size})
			},
		}
	})
	if !hasFill(appHover.Find("hover").Ops, theme.Hover) {
		t.Errorf("expected Hover fill color (%#x) in recorded ops", theme.Hover)
	}

	// Dynamically disabling the currently focused widget clears focus cleanly and skips it.
	root.SetFocus(bHover)
	if root.Focused() != bHover {
		t.Fatal("expected bHover focused")
	}
	bHover.Disabled = true
	if root.Focused() != nil || root.FocusIndex() != -1 {
		t.Fatalf("expected focus cleared after disabling widget, got %v (%d)", root.Focused(), root.FocusIndex())
	}
	if !root.FocusNext() || root.Focused() != bNormal {
		t.Fatalf("expected FocusNext to focus bNormal, got %v", root.Focused())
	}
	// Pure vertical column: pressing left/right should not jump vertically; up wraps to bottom enabled widget.
	if root.MoveFocus(1, 0) || root.Focused() != bNormal {
		t.Fatal("right in single-column VBox should not change focus")
	}
	if !root.MoveFocus(0, -1) || root.Focused() != bPressed {
		t.Fatalf("up from top of VBox should wrap to bottom enabled widget (bPressed), got %v", root.Focused())
	}
}
