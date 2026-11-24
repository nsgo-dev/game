package input_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const frameStep = 16667 * time.Microsecond

type stepState struct {
	pressed      bool
	justPressed  bool
	justReleased bool
}

type harness struct {
	app     *nsgotest.App
	actions *input.Actions
	steps   int
	history map[string][]stepState
}

func rect(x, y, w, h core.Points) core.Bounds {
	return core.Bounds{
		Origin: core.Point{X: x, Y: y},
		Size:   core.Size{Width: w, Height: h},
	}
}

func pt(x, y core.Points) core.Point {
	return core.Point{X: x, Y: y}
}

func startHarness(t *testing.T, watch []string, setup func(cx *core.Scope, a *input.Actions, v *core.View), opts ...nsgotest.Option) *harness {
	t.Helper()
	h := &harness{
		actions: input.NewActions(),
		history: make(map[string][]stepState, len(watch)),
	}
	opts = append([]nsgotest.Option{nsgotest.Size(400, 300)}, opts...)
	h.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v := core.View{
			TestID: "game",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
		}
		if setup != nil {
			setup(cx, h.actions, &v)
		}
		h.actions.Attach(&v)
		loop := &game.Loop{
			Rate: 60,
			Update: func(time.Duration) {
				h.actions.Step()
				h.steps++
				for _, name := range watch {
					h.history[name] = append(h.history[name], stepState{
						pressed:      h.actions.Pressed(name),
						justPressed:  h.actions.JustPressed(name),
						justReleased: h.actions.JustReleased(name),
					})
				}
			},
		}
		loop.Run(cx)
		return v
	}, opts...)
	return h
}

func (h *harness) step() {
	before := h.steps
	for h.steps == before {
		h.app.Advance(frameStep)
	}
}

func (h *harness) last(name string) stepState {
	s := h.history[name]
	return s[len(s)-1]
}

func expectState(t *testing.T, label string, got, want stepState) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %+v, want %+v", label, got, want)
	}
}

func TestTouchInsideBoundRegionHoldsPressedAndJustPressedForFirstStepOnly(t *testing.T) {
	jumpRect := rect(200, 100, 100, 100)
	h := startHarness(t, []string{"jump"}, func(_ *core.Scope, a *input.Actions, _ *core.View) {
		a.Bind("jump", input.TouchRegion(jumpRect))
	})

	// Before any touch.
	h.step()
	expectState(t, "idle", h.last("jump"), stepState{})

	// Touch outside the region does not trigger.
	outside := h.app.Touch("game", pt(50, 50))
	h.step()
	expectState(t, "outside", h.last("jump"), stepState{})
	outside.Lift()
	h.step()

	// Touch inside the region holds Pressed for all its steps and JustPressed for the first only.
	beforeTouch := len(h.history["jump"])
	finger := h.app.Touch("game", pt(250, 150))
	h.step()
	h.step()
	h.step()

	held := h.history["jump"][beforeTouch:]
	if len(held) < 3 {
		t.Fatalf("got %d steps while held, want at least 3", len(held))
	}
	expectState(t, "first held step", held[0], stepState{pressed: true, justPressed: true})
	for i := 1; i < len(held); i++ {
		expectState(t, "later held step", held[i], stepState{pressed: true})
	}

	// Lifting ends the press: JustReleased for one step, then idle.
	beforeLift := len(h.history["jump"])
	finger.Lift()
	h.step()
	h.step()

	afterLift := h.history["jump"][beforeLift:]
	// Finger.Lift advances one frame while still held, then lifts; find the release step.
	released := -1
	for i, s := range afterLift {
		if s.justReleased {
			if released >= 0 {
				t.Fatalf("JustReleased fired more than once: %+v", afterLift)
			}
			released = i
		}
	}
	if released < 0 {
		t.Fatalf("JustReleased never fired after Lift: %+v", afterLift)
	}
	expectState(t, "after release", afterLift[len(afterLift)-1], stepState{})
}

func TestTouchRegionBoundaryEdges(t *testing.T) {
	leftRect := rect(0, 0, 100, 100)
	rightRect := rect(100, 0, 100, 100)
	a := input.NewActions()
	a.Bind("left", input.TouchRegion(leftRect))
	a.Bind("right", input.TouchRegion(rightRect))

	// Origin (0, 0) is inside left.
	a.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 1, Position: pt(0, 0), Changed: true}}})
	a.Step()
	if !a.Pressed("left") || a.Pressed("right") {
		t.Fatalf("at (0,0): left=%v right=%v, want true false", a.Pressed("left"), a.Pressed("right"))
	}

	// Far edge x=100 is excluded from left and included in adjacent right.
	a.OnTouch(core.TouchEvent{Phase: core.TouchMove, Touches: []core.Touch{{ID: 1, Position: pt(100, 0), Changed: true}}})
	a.Step()
	if a.Pressed("left") || !a.Pressed("right") {
		t.Fatalf("at (100,0): left=%v right=%v, want false true", a.Pressed("left"), a.Pressed("right"))
	}

	// Far bottom edge y=100 is excluded from both.
	a.OnTouch(core.TouchEvent{Phase: core.TouchMove, Touches: []core.Touch{{ID: 1, Position: pt(50, 100), Changed: true}}})
	a.Step()
	if a.Pressed("left") || a.Pressed("right") {
		t.Fatalf("at (50,100): left=%v right=%v, want false false", a.Pressed("left"), a.Pressed("right"))
	}
}

func TestTouchSlideIntoOutOfRegionAndCancel(t *testing.T) {
	zone := rect(100, 100, 100, 100)
	h := startHarness(t, []string{"fire"}, func(_ *core.Scope, a *input.Actions, _ *core.View) {
		a.Bind("fire", input.TouchRegion(zone))
	})

	// Start outside.
	f := h.app.Touch("game", pt(20, 20))
	h.step()
	expectState(t, "outside", h.last("fire"), stepState{})

	// Slide inside.
	f.Move(pt(150, 150))
	h.step()
	expectState(t, "slid in", h.last("fire"), stepState{pressed: true, justPressed: true})
	h.step()
	expectState(t, "held inside", h.last("fire"), stepState{pressed: true})

	// Slide out.
	f.Move(pt(250, 150))
	h.step()
	expectState(t, "slid out", h.last("fire"), stepState{justReleased: true})
	h.step()
	expectState(t, "outside again", h.last("fire"), stepState{})

	// Slide back in, then cancel.
	f.Move(pt(120, 120))
	h.step()
	expectState(t, "slid back in", h.last("fire"), stepState{pressed: true, justPressed: true})

	f.Cancel()
	h.step()
	expectState(t, "cancelled", h.last("fire"), stepState{justReleased: true})
	h.step()
	expectState(t, "after cancel", h.last("fire"), stepState{})
}

func TestMultiTouchIndependentAndSharedRegions(t *testing.T) {
	a := input.NewActions()
	a.Bind("left", input.TouchRegion(rect(0, 0, 100, 100)))
	a.Bind("jump", input.TouchRegion(rect(200, 0, 100, 100)))

	// Two fingers pressing two different regions simultaneously.
	a.OnTouch(core.TouchEvent{
		Phase: core.TouchStart,
		Touches: []core.Touch{
			{ID: 1, Position: pt(50, 50), Changed: true},
		},
	})
	a.OnTouch(core.TouchEvent{
		Phase: core.TouchStart,
		Touches: []core.Touch{
			{ID: 1, Position: pt(50, 50), Changed: false},
			{ID: 2, Position: pt(250, 50), Changed: true},
		},
	})
	a.Step()
	if !a.Pressed("left") || !a.JustPressed("left") || !a.Pressed("jump") || !a.JustPressed("jump") {
		t.Fatalf("both fingers down: left=(%v,%v) jump=(%v,%v)", a.Pressed("left"), a.JustPressed("left"), a.Pressed("jump"), a.JustPressed("jump"))
	}

	// Second finger moves into left's region too; lifting finger 1 keeps left pressed.
	a.OnTouch(core.TouchEvent{
		Phase: core.TouchMove,
		Touches: []core.Touch{
			{ID: 1, Position: pt(50, 50), Changed: false},
			{ID: 2, Position: pt(60, 60), Changed: true},
		},
	})
	a.Step()
	if !a.Pressed("left") || a.JustPressed("left") || a.Pressed("jump") || !a.JustReleased("jump") {
		t.Fatalf("finger 2 moved to left: left=(%v,%v) jump=(%v,%v)", a.Pressed("left"), a.JustPressed("left"), a.Pressed("jump"), a.JustReleased("jump"))
	}

	// Lift finger 1 while finger 2 remains inside left's region.
	a.OnTouch(core.TouchEvent{
		Phase: core.TouchEnd,
		Touches: []core.Touch{
			{ID: 1, Position: pt(50, 50), Changed: true},
			{ID: 2, Position: pt(60, 60), Changed: false},
		},
	})
	a.Step()
	if !a.Pressed("left") || a.JustReleased("left") {
		t.Fatalf("finger 1 lifted while finger 2 held: left pressed=%v justReleased=%v", a.Pressed("left"), a.JustReleased("left"))
	}

	// Lift finger 2 (without Changed set, testing fallback).
	a.OnTouch(core.TouchEvent{
		Phase:   core.TouchEnd,
		Touches: []core.Touch{{ID: 2, Position: pt(60, 60)}},
	})
	a.Step()
	if a.Pressed("left") || !a.JustReleased("left") {
		t.Fatalf("finger 2 lifted: left pressed=%v justReleased=%v", a.Pressed("left"), a.JustReleased("left"))
	}

	// TouchCancel with empty Touches clears all active fingers.
	a.OnTouch(core.TouchEvent{
		Phase:   core.TouchStart,
		Touches: []core.Touch{{ID: 3, Position: pt(25, 25), Changed: true}},
	})
	a.Step()
	if !a.Pressed("left") {
		t.Fatal("expected left pressed")
	}
	a.OnTouch(core.TouchEvent{Phase: core.TouchCancel})
	a.Step()
	if a.Pressed("left") || !a.JustReleased("left") {
		t.Fatalf("after empty cancel: pressed=%v justReleased=%v", a.Pressed("left"), a.JustReleased("left"))
	}
}

func TestQuickTapBetweenStepsRegistersForOneStep(t *testing.T) {
	h := startHarness(t, []string{"tap"}, func(_ *core.Scope, a *input.Actions, _ *core.View) {
		a.Bind("tap", input.TouchRegion(rect(0, 0, 400, 300)))
	})

	h.step()
	expectState(t, "before tap", h.last("tap"), stepState{})

	// PressAt sends TouchStart and TouchEnd before any Loop step runs.
	h.app.PressAt("game", pt(100, 100))
	h.step()
	expectState(t, "tap step", h.last("tap"), stepState{pressed: true, justPressed: true})
	h.step()
	expectState(t, "step after tap", h.last("tap"), stepState{justReleased: true})
	h.step()
	expectState(t, "settled", h.last("tap"), stepState{})

	// A held touch that moves inside the region and lifts before the next step does not delay release.
	a := input.NewActions()
	a.Bind("hold", input.TouchRegion(rect(0, 0, 100, 100)))
	a.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 1, Position: pt(10, 10), Changed: true}}})
	a.Step()
	if !a.Pressed("hold") || !a.JustPressed("hold") {
		t.Fatal("expected hold pressed on step 1")
	}
	a.OnTouch(core.TouchEvent{Phase: core.TouchMove, Touches: []core.Touch{{ID: 1, Position: pt(20, 20), Changed: true}}})
	a.OnTouch(core.TouchEvent{Phase: core.TouchEnd, Touches: []core.Touch{{ID: 1, Position: pt(20, 20), Changed: true}}})
	a.Step()
	if a.Pressed("hold") || !a.JustReleased("hold") {
		t.Fatalf("after move+end while held: pressed=%v justReleased=%v, want false true", a.Pressed("hold"), a.JustReleased("hold"))
	}
}

func TestMouseButtonsAndRegions(t *testing.T) {
	h := startHarness(t, []string{"primary", "secondary", "middle", "region", "touch"}, func(_ *core.Scope, a *input.Actions, _ *core.View) {
		a.Bind("primary", input.MouseButton(core.MouseButtonLeft))
		a.Bind("secondary", input.MouseButton(core.MouseButtonRight))
		a.Bind("middle", input.MouseButton(core.MouseButtonMiddle))
		a.Bind("region", input.MouseRegion(rect(0, 0, 100, 100), core.MouseButtonLeft))
		a.Bind("touch", input.TouchRegion(rect(0, 0, 400, 300)))
	})

	// Touching the view triggers TouchRegion, NOT MouseButtonLeft.
	f := h.app.Touch("game", pt(50, 50))
	h.step()
	expectState(t, "touch only", h.last("touch"), stepState{pressed: true, justPressed: true})
	expectState(t, "primary unaffected by touch", h.last("primary"), stepState{})
	f.Lift()
	h.step()
	h.step()

	// PressDown holds MouseButtonLeft across steps; HoverAt inside region activates MouseRegion without dropping PressDown.
	h.app.PressDown("game")
	h.step()
	expectState(t, "pressDown step 1", h.last("primary"), stepState{pressed: true, justPressed: true})
	expectState(t, "region outside center", h.last("region"), stepState{})

	h.app.HoverAt("game", pt(50, 50))
	h.step()
	expectState(t, "pressDown held after hover", h.last("primary"), stepState{pressed: true})
	expectState(t, "region active after hover", h.last("region"), stepState{pressed: true, justPressed: true})

	h.app.PressUp("game")
	h.step()
	expectState(t, "pressUp primary", h.last("primary"), stepState{justReleased: true})
	expectState(t, "pressUp region", h.last("region"), stepState{justReleased: true})

	// HoverOut (OnMouseLeave) releases held mouse buttons.
	h.app.PressDown("game")
	h.step()
	expectState(t, "pressDown before hoverOut", h.last("primary"), stepState{pressed: true, justPressed: true})
	h.app.HoverOut()
	h.step()
	expectState(t, "hoverOut releases", h.last("primary"), stepState{justReleased: true})

	// RightClick (OnContextMenu) pulses MouseButtonRight for one step.
	h.app.RightClick("game")
	h.step()
	expectState(t, "rightClick step 1", h.last("secondary"), stepState{pressed: true, justPressed: true})
	h.step()
	expectState(t, "rightClick step 2", h.last("secondary"), stepState{justReleased: true})

	// OnMouseMove with PressedButton tracks middle/right drag and releases on MouseButtonNone.
	h.app.Post(func() {
		h.actions.OnMouseMove(core.MouseEvent{Position: pt(20, 20), PressedButton: core.MouseButtonMiddle})
	})
	h.app.Settle()
	h.step()
	expectState(t, "middle drag step 1", h.last("middle"), stepState{pressed: true, justPressed: true})
	h.step()
	expectState(t, "middle drag step 2", h.last("middle"), stepState{pressed: true})
	h.app.Post(func() {
		h.actions.OnMouseMove(core.MouseEvent{Position: pt(30, 30), PressedButton: core.MouseButtonNone})
	})
	h.app.Settle()
	h.step()
	expectState(t, "middle released", h.last("middle"), stepState{justReleased: true})

	// Keyboard PressEvent and MouseButtonNone never activate mouse bindings.
	a := input.NewActions()
	a.Bind("none", input.MouseButton(core.MouseButtonNone))
	a.Bind("left", input.MouseButton(core.MouseButtonLeft))
	a.OnPressDown(core.PressEvent{Position: pt(10, 10), Keyboard: true})
	a.Step()
	if a.Pressed("none") || a.Pressed("left") {
		t.Fatalf("keyboard press or MouseButtonNone activated: none=%v left=%v", a.Pressed("none"), a.Pressed("left"))
	}
}

func TestAxisFromOppositeActionsAndSingleAction(t *testing.T) {
	a := input.NewActions()
	a.Bind("left", input.TouchRegion(rect(0, 0, 100, 100)))
	a.Bind("right", input.TouchRegion(rect(100, 0, 100, 100)))
	a.Bind("jump", input.MouseButton(core.MouseButtonLeft))
	a.BindAxis("move", "left", "right")

	neg, pos, ok := a.AxisBinding("move")
	if !ok || neg != "left" || pos != "right" {
		t.Fatalf("AxisBinding(move) = %q, %q, %v; want left, right, true", neg, pos, ok)
	}

	// Idle.
	a.Step()
	if v := a.Axis("move"); v != 0 {
		t.Fatalf("idle axis = %v, want 0", v)
	}
	if v := a.Axis("jump"); v != 0 {
		t.Fatalf("idle single action axis = %v, want 0", v)
	}

	// Negative only -> -1.
	a.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 1, Position: pt(50, 50), Changed: true}}})
	a.Step()
	if v := a.Axis("move"); v != -1 {
		t.Fatalf("left axis = %v, want -1", v)
	}

	// Both held -> 0.
	a.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{
		{ID: 1, Position: pt(50, 50), Changed: false},
		{ID: 2, Position: pt(150, 50), Changed: true},
	}})
	a.Step()
	if v := a.Axis("move"); v != 0 {
		t.Fatalf("both held axis = %v, want 0", v)
	}

	// Positive only -> +1.
	a.OnTouch(core.TouchEvent{Phase: core.TouchEnd, Touches: []core.Touch{
		{ID: 1, Position: pt(50, 50), Changed: true},
		{ID: 2, Position: pt(150, 50), Changed: false},
	}})
	a.Step()
	if v := a.Axis("move"); v != 1 {
		t.Fatalf("right axis = %v, want 1", v)
	}

	// Lift remaining finger before testing mouse press as a single-action axis.
	a.OnTouch(core.TouchEvent{Phase: core.TouchEnd, Touches: []core.Touch{
		{ID: 2, Position: pt(150, 50), Changed: true},
	}})
	// Regular action as axis -> 1 when pressed.
	a.OnPressDown(core.PressEvent{Position: pt(10, 10)})
	a.Step()
	if v := a.Axis("jump"); v != 1 {
		t.Fatalf("pressed single action axis = %v, want 1", v)
	}

	// Unbinding axis stops reporting opposite actions.
	a.UnbindAxis("move")
	if _, _, ok := a.AxisBinding("move"); ok {
		t.Fatal("move axis still bound after UnbindAxis")
	}
	if v := a.Axis("move"); v != 0 {
		t.Fatalf("unbound axis = %v, want 0", v)
	}
}

func TestMapPointTransformsCoordinatesAndIgnoresBars(t *testing.T) {
	// View is 400x300; virtual canvas is 100x100 scaled 2x centered at (100, 50)..(300, 250).
	mapToVirtual := func(p core.Point) (core.Point, bool) {
		if p.X < 100 || p.X >= 300 || p.Y < 50 || p.Y >= 250 {
			return core.Point{}, false
		}
		return core.Point{X: (p.X - 100) / 2, Y: (p.Y - 50) / 2}, true
	}

	h := startHarness(t, []string{"btn", "mouse_btn", "any_click"}, func(_ *core.Scope, a *input.Actions, _ *core.View) {
		a.MapPoint = mapToVirtual
		a.Bind("btn", input.TouchRegion(rect(0, 0, 50, 50)))
		a.Bind("mouse_btn", input.MouseRegion(rect(50, 50, 50, 50), core.MouseButtonLeft))
		a.Bind("any_click", input.MouseButton(core.MouseButtonLeft))
	})

	// Touch in the left letterbox bar (raw x=20, y=20 is inside [0,50) unmapped, but MapPoint returns false).
	barFinger := h.app.Touch("game", pt(20, 20))
	h.step()
	expectState(t, "letterbox bar ignored", h.last("btn"), stepState{})
	barFinger.Lift()
	h.step()

	// Touch at view (120, 70) -> virtual (10, 10), inside [0, 50).
	canvasFinger := h.app.Touch("game", pt(120, 70))
	h.step()
	expectState(t, "mapped touch inside", h.last("btn"), stepState{pressed: true, justPressed: true})
	canvasFinger.Lift()
	h.step()
	h.step()

	// MouseRegion also uses MapPoint: view (250, 200) -> virtual (75, 75), inside [50, 100).
	h.app.PressDown("game")
	h.app.HoverAt("game", pt(250, 200))
	h.step()
	expectState(t, "mapped mouse region", h.last("mouse_btn"), stepState{pressed: true, justPressed: true})
	expectState(t, "non-region mouse button", h.last("any_click"), stepState{pressed: true, justPressed: true})

	// Move mouse into letterbox bar while holding button: MouseRegion deactivates, MouseButton stays active.
	h.app.HoverAt("game", pt(10, 10))
	h.step()
	expectState(t, "mouse in bar deactivates region", h.last("mouse_btn"), stepState{justReleased: true})
	expectState(t, "non-region mouse stays pressed", h.last("any_click"), stepState{pressed: true})
	h.app.PressUp("game")
}

func TestRuntimeRebindingResetAndAttachChaining(t *testing.T) {
	regionA := rect(0, 0, 100, 100)
	regionB := rect(200, 0, 100, 100)

	var chainedTouches, chainedPressDowns int
	h := startHarness(t, []string{"jump"}, func(_ *core.Scope, a *input.Actions, v *core.View) {
		v.OnTouch = func(core.TouchEvent) { chainedTouches++ }
		v.OnPressDown = func(core.PressEvent) { chainedPressDowns++ }
		a.Bind("jump", input.TouchRegion(regionA))
	})

	// Bindings returns a defensive copy.
	got := h.actions.Bindings("jump")
	if !slices.Equal(got, []input.Input{input.TouchRegion(regionA)}) {
		t.Fatalf("Bindings(jump) = %+v, want regionA", got)
	}
	got[0] = input.TouchRegion(regionB)
	if !slices.Equal(h.actions.Bindings("jump"), []input.Input{input.TouchRegion(regionA)}) {
		t.Fatal("mutating returned Bindings slice modified Actions")
	}

	// Touch region A works, and existing OnTouch callback still runs.
	f := h.app.Touch("game", pt(50, 50))
	h.step()
	expectState(t, "region A pressed", h.last("jump"), stepState{pressed: true, justPressed: true})
	if chainedTouches == 0 || chainedPressDowns == 0 {
		t.Fatalf("chained callbacks did not run: touches=%d pressDowns=%d", chainedTouches, chainedPressDowns)
	}

	// Rebind at runtime while finger is still in region A -> releases on next step.
	h.app.Post(func() { h.actions.Bind("jump", input.TouchRegion(regionB)) })
	h.app.Settle()
	h.step()
	expectState(t, "rebound away from held region", h.last("jump"), stepState{justReleased: true})
	f.Lift()
	h.step()

	// Old region A no longer triggers; new region B triggers.
	fA := h.app.Touch("game", pt(50, 50))
	h.step()
	expectState(t, "old region A ignored", h.last("jump"), stepState{})
	fA.Lift()
	h.step()

	fB := h.app.Touch("game", pt(250, 50))
	h.step()
	expectState(t, "new region B triggers", h.last("jump"), stepState{pressed: true, justPressed: true})

	// Reset clears active touches and step state without removing bindings.
	h.app.Post(func() { h.actions.Reset() })
	h.app.Settle()
	if h.actions.Pressed("jump") || h.actions.JustPressed("jump") || h.actions.JustReleased("jump") {
		t.Fatal("Reset left active step state")
	}
	if !slices.Equal(h.actions.Bindings("jump"), []input.Input{input.TouchRegion(regionB)}) {
		t.Fatal("Reset cleared bindings")
	}
	fB.Lift()

	// Unbind removes the action.
	h.app.Post(func() { h.actions.Unbind("jump") })
	h.app.Settle()
	if h.actions.Bindings("jump") != nil {
		t.Fatalf("after Unbind, Bindings = %+v, want nil", h.actions.Bindings("jump"))
	}
}

func TestBindingsPersistAcrossRestart(t *testing.T) {
	defaultJump := input.TouchRegion(rect(0, 0, 100, 100))
	defaultLeft := input.TouchRegion(rect(0, 200, 100, 100))
	defaultRight := input.TouchRegion(rect(100, 200, 100, 100))
	defaultDrop := input.MouseButton(core.MouseButtonRight)

	reboundJump := input.TouchRegion(rect(250, 150, 120, 120))
	reboundJumpMouse := input.MouseRegion(rect(250, 150, 120, 120), core.MouseButtonLeft)
	reboundAlt := input.MouseButton(core.MouseButtonMiddle)

	const storageKey = "controls"

	bindDefaults := func(a *input.Actions) {
		a.Bind("jump", defaultJump)
		a.Bind("left", defaultLeft)
		a.Bind("right", defaultRight)
		a.Bind("drop", defaultDrop)
		a.BindAxis("move", "left", "right")
	}

	// First run: no saved bindings yet, defaults stay in place; player rebinds in settings and saves.
	var firstLoaded bool
	var firstLoadErr error
	first := startHarness(t, []string{"jump", "alt"}, func(cx *core.Scope, a *input.Actions, _ *core.View) {
		bindDefaults(a)
		firstLoaded, firstLoadErr = a.Load(cx.Storage(), storageKey)
	})
	if firstLoadErr != nil || firstLoaded {
		t.Fatalf("first Load = (%v, %v), want (false, nil)", firstLoaded, firstLoadErr)
	}
	if !slices.Equal(first.actions.Bindings("jump"), []input.Input{defaultJump}) {
		t.Fatalf("defaults not kept on absent key: %+v", first.actions.Bindings("jump"))
	}

	// Settings screen changes bindings and saves to core.Storage.
	first.app.Post(func() {
		first.actions.Bind("jump", reboundJump, reboundJumpMouse)
		first.actions.Bind("alt", reboundAlt)
		first.actions.Unbind("drop")
		first.actions.BindAxis("move", "right", "left") // inverted axis
		if err := first.actions.Save(first.app.Storage(), storageKey); err != nil {
			t.Errorf("Save failed: %v", err)
		}
	})
	first.app.Settle()

	// Snapshot storage as it survives across launches.
	kept := map[string]string{}
	st := first.app.Storage()
	for _, k := range st.Keys() {
		kept[k], _ = st.GetItem(k)
	}
	if _, ok := kept[storageKey]; !ok {
		t.Fatalf("storage missing key %q; keys=%v", storageKey, st.Keys())
	}

	// Second run (restart): sets defaults first, then loads saved bindings from storage.
	var secondLoaded bool
	var secondLoadErr error
	second := startHarness(t, []string{"jump", "alt"}, func(cx *core.Scope, a *input.Actions, _ *core.View) {
		bindDefaults(a)
		secondLoaded, secondLoadErr = a.Load(cx.Storage(), storageKey)
	}, nsgotest.Storage(kept))
	if secondLoadErr != nil || !secondLoaded {
		t.Fatalf("second Load = (%v, %v), want (true, nil)", secondLoaded, secondLoadErr)
	}

	if got := second.actions.Bindings("jump"); !slices.Equal(got, []input.Input{reboundJump, reboundJumpMouse}) {
		t.Fatalf("after restart, jump = %+v, want %+v", got, []input.Input{reboundJump, reboundJumpMouse})
	}
	if got := second.actions.Bindings("alt"); !slices.Equal(got, []input.Input{reboundAlt}) {
		t.Fatalf("after restart, alt = %+v, want %+v", got, []input.Input{reboundAlt})
	}
	if got := second.actions.Bindings("drop"); got != nil {
		t.Fatalf("after restart, unbound drop = %+v, want nil", got)
	}
	if neg, pos, ok := second.actions.AxisBinding("move"); !ok || neg != "right" || pos != "left" {
		t.Fatalf("after restart, move axis = (%q, %q, %v), want (right, left, true)", neg, pos, ok)
	}

	// Verify live input after restart: old default region does nothing, new rebound region triggers jump.
	oldFinger := second.app.Touch("game", pt(50, 50))
	second.step()
	expectState(t, "old default region after restart", second.last("jump"), stepState{})
	oldFinger.Lift()
	second.step()

	newFinger := second.app.Touch("game", pt(300, 200))
	second.step()
	expectState(t, "rebound region after restart", second.last("jump"), stepState{pressed: true, justPressed: true})
	newFinger.Lift()
}

func TestLoadInvalidStorageLeavesBindingsUntouched(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"bad json", "{not json"},
		{"unsupported version", `{"version":99}`},
		{"unknown kind", `{"version":1,"actions":{"jump":[{"kind":"gamepad"}]}}`},
		{"missing bounds", `{"version":1,"actions":{"jump":[{"kind":"touch_region"}]}}`},
		{"unknown button", `{"version":1,"actions":{"jump":[{"kind":"mouse_button","button":"side9"}]}}`},
		{"mouse region missing bounds", `{"version":1,"actions":{"jump":[{"kind":"mouse_region","button":"left"}]}}`},
		{"unknown field", `{"version":1,"controls":{}}`},
		{"misspelled pointer axis field", `{"version":1,"pointer_axes":{"yaw":{"axis":"pointer_x","sensitivty":0.002}}}`},
		{"data after the bindings", `{"version":1} {}`},
	}

	defaultJump := input.TouchRegion(rect(10, 20, 30, 40))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} },
				nsgotest.Storage(map[string]string{"controls": tc.raw}))
			a := input.NewActions()
			a.Bind("jump", defaultJump)
			loaded, err := a.Load(app.Storage(), "controls")
			if err == nil || loaded {
				t.Fatalf("Load(%s) = (%v, %v), want (false, error)", tc.name, loaded, err)
			}
			if !slices.Equal(a.Bindings("jump"), []input.Input{defaultJump}) {
				t.Fatalf("Load(%s) mutated existing bindings: %+v", tc.name, a.Bindings("jump"))
			}
		})
	}
}

func TestInjectedPressReleaseAxisAndNames(t *testing.T) {
	a := input.NewActions()
	a.Bind("jump", input.Key("space"))
	a.Bind("left", input.Key("a"))
	a.Bind("right", input.Key("d"))
	a.BindAxis("move_x", "left", "right")

	// ActionNames and AxisNames return sorted configured names.
	if got := a.ActionNames(); !slices.Equal(got, []string{"jump", "left", "right"}) {
		t.Fatalf("ActionNames = %v, want [jump left right]", got)
	}
	if got := a.AxisNames(); !slices.Equal(got, []string{"move_x"}) {
		t.Fatalf("AxisNames = %v, want [move_x]", got)
	}

	// Press holds across steps until Release.
	a.Press("right")
	a.Step()
	if !a.Pressed("right") || !a.JustPressed("right") || a.JustReleased("right") || a.Axis("move_x") != 1 {
		t.Fatalf("step 1 right: pressed=%v jp=%v jr=%v axis=%v", a.Pressed("right"), a.JustPressed("right"), a.JustReleased("right"), a.Axis("move_x"))
	}
	a.Step()
	if !a.Pressed("right") || a.JustPressed("right") || a.JustReleased("right") || a.Axis("move_x") != 1 {
		t.Fatalf("step 2 right: pressed=%v jp=%v jr=%v axis=%v", a.Pressed("right"), a.JustPressed("right"), a.JustReleased("right"), a.Axis("move_x"))
	}
	a.Release("right")
	a.Step()
	if a.Pressed("right") || a.JustPressed("right") || !a.JustReleased("right") || a.Axis("move_x") != 0 {
		t.Fatalf("step 3 right: pressed=%v jp=%v jr=%v axis=%v", a.Pressed("right"), a.JustPressed("right"), a.JustReleased("right"), a.Axis("move_x"))
	}

	// Quick Press+Release before Step still latches for one step (even for an unbound action).
	a.Press("dash")
	a.Release("dash")
	a.Step()
	if !a.Pressed("dash") || !a.JustPressed("dash") {
		t.Fatalf("latched unbound dash: pressed=%v jp=%v", a.Pressed("dash"), a.JustPressed("dash"))
	}
	a.Step()
	if a.Pressed("dash") || !a.JustReleased("dash") {
		t.Fatalf("released unbound dash: pressed=%v jr=%v", a.Pressed("dash"), a.JustReleased("dash"))
	}

	// SetAxis clamps to [-1, 1], ignores NaN, snapshots on Step, and clears via ClearAxis or Reset.
	a.SetAxis("move_x", 0.5)
	a.SetAxis("aim_y", -2.5)
	a.SetAxis("aim_y", math.NaN()) // ignored
	if got := a.AxisNames(); !slices.Equal(got, []string{"aim_y", "move_x"}) {
		t.Fatalf("AxisNames with injected = %v, want [aim_y move_x]", got)
	}
	a.Step()
	if a.Axis("move_x") != 0.5 || a.Axis("aim_y") != -1 {
		t.Fatalf("injected axes = (%v, %v), want (0.5, -1)", a.Axis("move_x"), a.Axis("aim_y"))
	}
	a.ClearAxis("move_x")
	a.Step()
	if a.Axis("move_x") != 0 || a.Axis("aim_y") != -1 {
		t.Fatalf("after ClearAxis = (%v, %v), want (0, -1)", a.Axis("move_x"), a.Axis("aim_y"))
	}

	// Reset clears injected holds and injected axes.
	a.Press("jump")
	a.Reset()
	a.Step()
	if a.Pressed("jump") || a.Axis("aim_y") != 0 {
		t.Fatalf("after Reset: jump=%v aim_y=%v, want false/0", a.Pressed("jump"), a.Axis("aim_y"))
	}
}
