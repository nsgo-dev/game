package mcp_test

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func makeTestSprite(name string, x, y, w, h float32) sprite.Sprite {
	f := sprite.Frame{
		Name:   name,
		W:      w,
		H:      h,
		Width:  w,
		Height: h,
		PivotX: w / 2,
		PivotY: h / 2,
	}
	sp := sprite.NewSprite(f)
	sp.X = x
	sp.Y = y
	return sp
}

// TestInputPointerHitsSpriteThroughLetterboxedViewport verifies the issue #463 acceptance criterion:
// a press at a sprite's game position (mapped through a letterboxed screen.Viewport, and with camera
// world coordinates via space="world") hits that exact sprite and invokes its OnPress callback.
func TestInputPointerHitsSpriteThroughLetterboxedViewport(t *testing.T) {
	// Virtual 320x180 canvas in an 800x600 window (ModeFit -> scale 2.5, rect 800x450 at y=75..525).
	vp := screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit}
	winSize := core.Size{Width: 800, Height: 600}

	// Camera centered at world (500, 300) with 2x zoom over the 320x180 game canvas.
	cam := &tilemap.Camera{X: 500, Y: 300, Zoom: 2, Width: 320, Height: 180}

	// World sprite at world (520, 310) -> game (160 + 20*2, 90 + 10*2) = (200, 110) -> view (500, 350).
	hero := makeTestSprite("hero", 520, 310, 16, 16)
	// Second world sprite at world (460, 290) -> game (160 - 40*2, 90 - 10*2) = (80, 70).
	slime := makeTestSprite("slime", 460, 290, 16, 16)
	worldBatch := &sprite.Batch{Sprites: []*sprite.Sprite{&hero, &slime}}
	worldLayer := scene.NewLayer("actors", scene.SpaceWorld, 10)
	worldLayer.AddBatch(worldBatch)

	// Screen-space HUD sprite at game (40, 20) -> view (100, 125).
	hudBtn := makeTestSprite("pause_btn", 40, 20, 24, 16)
	hudBatch := &sprite.Batch{Sprites: []*sprite.Sprite{&hudBtn}}
	hudDrawable := scene.NewDrawable(func(*core.Drawing) {})
	hudDrawable.Hit = scene.RectHit(core.Bounds{
		Origin: core.Point{X: 280, Y: 10},
		Size:   core.Size{Width: 30, Height: 20},
	})
	hudLayer := scene.NewLayer("hud", scene.SpaceScreen, 100)
	hudLayer.AddBatch(hudBatch)
	hudLayer.AddDrawable(hudDrawable)

	sc := scene.NewScene(cam)
	sc.AddLayer(worldLayer)
	sc.AddLayer(hudLayer)

	var pressLog []string
	sc.SetSpriteHit(&hero, scene.HitOptions{
		OnPress: func(h scene.HitResult, ev core.PressEvent) {
			pressLog = append(pressLog, "hero")
		},
	})
	sc.SetSpriteHit(&slime, scene.HitOptions{
		OnPress: func(h scene.HitResult, ev core.PressEvent) {
			pressLog = append(pressLog, "slime")
		},
	})
	sc.SetSpriteHit(&hudBtn, scene.HitOptions{
		OnPress: func(h scene.HitResult, ev core.PressEvent) {
			pressLog = append(pressLog, "pause_btn")
		},
	})
	hudDrawable.OnPress = func(h scene.HitResult, ev core.PressEvent) {
		pressLog = append(pressLog, "hud_drawable")
	}

	actions := input.NewActions()
	actions.MapPoint = func(p core.Point) (core.Point, bool) {
		return vp.ViewToGame(winSize, p)
	}
	// Bind a MouseRegion covering the HUD button in virtual game coordinates [28..52, 12..28).
	actions.Bind("hud_click", input.MouseRegion(core.Bounds{
		Origin: core.Point{X: 28, Y: 12},
		Size:   core.Size{Width: 24, Height: 16},
	}, core.MouseButtonLeft))

	var pointerEvents []mcp.PointerEvent
	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			Scene:        sc,
			Actions:      actions,
			Viewport:     &vp,
			ViewSizeFunc: func() core.Size { return winSize },
			OnPointer:    func(ev mcp.PointerEvent) { pointerEvents = append(pointerEvents, ev) },
		}); err != nil {
			t.Fatal(err)
		}
		return core.Canvas{Style: core.Style{Width: core.Pt(800), Height: core.Pt(600)}}
	})

	// 1. Click hero at its game coordinates (200, 110) through the letterboxed viewport.
	res := callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "click",
		"x":      200,
		"y":      110,
	})
	if !res.InViewport || !res.Hit || !res.Dispatched {
		t.Fatalf("hero game-space click = %+v, want in_viewport=true hit=true dispatched=true", res)
	}
	if res.GameX != 200 || res.GameY != 110 || res.ViewX != 500 || res.ViewY != 350 {
		t.Fatalf("hero mapped coords = game(%v,%v) view(%v,%v), want game(200,110) view(500,350)", res.GameX, res.GameY, res.ViewX, res.ViewY)
	}
	if res.WorldX == nil || res.WorldY == nil || *res.WorldX != 520 || *res.WorldY != 310 {
		t.Fatalf("hero world coords = (%v, %v), want (520, 310)", res.WorldX, res.WorldY)
	}
	if res.HitLayer != "actors" || res.HitSpace != "world" || res.HitKind != "sprite" || res.HitFrame != "hero" {
		t.Fatalf("hero hit metadata = %+v", res)
	}
	if res.HitBatchIndex == nil || *res.HitBatchIndex != 0 || res.HitSpriteIndex == nil || *res.HitSpriteIndex != 0 {
		t.Fatalf("hero indices = batch:%v sprite:%v, want 0/0", res.HitBatchIndex, res.HitSpriteIndex)
	}
	// Click emits PointerPress then PointerRelease to OnPointer.
	if len(pointerEvents) != 2 || pointerEvents[0].Action != mcp.PointerPress || !pointerEvents[0].Dispatched ||
		pointerEvents[1].Action != mcp.PointerRelease || pointerEvents[1].Dispatched {
		t.Fatalf("pointerEvents after click = %+v", pointerEvents)
	}

	// 2. Press slime using space="world" at its world position (460, 290).
	slimeRes := callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "press",
		"space":  "world",
		"x":      460,
		"y":      290,
	})
	// Expected game coords: (160 + (460-500)*2, 90 + (290-300)*2) = (80, 70) -> view (200, 75+175=250).
	if !slimeRes.InViewport || !slimeRes.Hit || !slimeRes.Dispatched {
		t.Fatalf("slime world-space press = %+v", slimeRes)
	}
	if slimeRes.GameX != 80 || slimeRes.GameY != 70 || slimeRes.ViewX != 200 || slimeRes.ViewY != 250 {
		t.Fatalf("slime coords = game(%v,%v) view(%v,%v), want game(80,70) view(200,250)", slimeRes.GameX, slimeRes.GameY, slimeRes.ViewX, slimeRes.ViewY)
	}
	if slimeRes.HitFrame != "slime" || slimeRes.HitSpriteIndex == nil || *slimeRes.HitSpriteIndex != 1 {
		t.Fatalf("slime hit = %+v, want frame=slime sprite_index=1", slimeRes)
	}

	// Release the press cleanly.
	_ = callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "release",
		"x":      80,
		"y":      70,
	})

	// 3. Click HUD sprite at game (40, 20): hits HUD sprite AND triggers input.Actions MouseRegion via MapPoint!
	hudRes := callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "click",
		"x":      40,
		"y":      20,
	})
	if !hudRes.Hit || !hudRes.Dispatched || hudRes.HitLayer != "hud" || hudRes.HitSpace != "screen" || hudRes.HitFrame != "pause_btn" {
		t.Fatalf("hud sprite click = %+v", hudRes)
	}
	if hudRes.ViewX != 100 || hudRes.ViewY != 125 {
		t.Fatalf("hud view coords = (%v,%v), want (100,125)", hudRes.ViewX, hudRes.ViewY)
	}
	app.Post(actions.Step)
	app.Settle()
	if !actions.Pressed("hud_click") || !actions.JustPressed("hud_click") {
		t.Fatalf("hud_click mouse region was not triggered through Viewport.ViewToGame: pressed=%v jp=%v", actions.Pressed("hud_click"), actions.JustPressed("hud_click"))
	}
	app.Post(actions.Step)
	app.Settle()
	if actions.Pressed("hud_click") || !actions.JustReleased("hud_click") {
		t.Fatalf("hud_click should release on following step: pressed=%v jr=%v", actions.Pressed("hud_click"), actions.JustReleased("hud_click"))
	}

	// 4. Click HUD drawable at game (295, 20).
	drawRes := callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "click",
		"x":      295,
		"y":      20,
	})
	if !drawRes.Hit || !drawRes.Dispatched || drawRes.HitKind != "drawable" || drawRes.HitDrawableIndex == nil || *drawRes.HitDrawableIndex != 0 {
		t.Fatalf("hud drawable click = %+v", drawRes)
	}

	// 5. Press empty space at game (10, 170): in_viewport=true, hit=false, dispatched=false.
	missRes := callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "click",
		"x":      10,
		"y":      170,
	})
	if !missRes.InViewport || missRes.Hit || missRes.Dispatched {
		t.Fatalf("empty space click = %+v, want in_viewport=true hit=false dispatched=false", missRes)
	}
	if missRes.ViewX != 25 || missRes.ViewY != 500 {
		t.Fatalf("empty space view coords = (%v,%v), want (25,500)", missRes.ViewX, missRes.ViewY)
	}

	// 6. Press outside the virtual canvas (into the top letterbox bar at game y=-10): in_viewport=false, hit=false.
	barRes := callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "click",
		"x":      160,
		"y":      -10,
	})
	if barRes.InViewport || barRes.Hit || barRes.Dispatched {
		t.Fatalf("letterbox click = %+v, want in_viewport=false hit=false dispatched=false", barRes)
	}
	if barRes.ViewX != 400 || barRes.ViewY != 50 {
		t.Fatalf("letterbox view coords = (%v,%v), want (400,50)", barRes.ViewX, barRes.ViewY)
	}

	wantLog := []string{"hero", "slime", "pause_btn", "hud_drawable"}
	if !slices.Equal(pressLog, wantLog) {
		t.Fatalf("pressLog = %v, want %v", pressLog, wantLog)
	}
}

func TestInputActionsAxesAndListAcrossSteps(t *testing.T) {
	actions := input.NewActions()
	actions.Bind("jump", input.Key("space"))
	actions.Bind("left", input.Key("a"))
	actions.Bind("right", input.Key("d"))
	actions.Bind("fire", input.MouseButton(core.MouseButtonRight))
	actions.BindAxis("move_x", "left", "right")

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			Actions: actions,
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// 1. List configured actions and axes before any input.
	list := callTool[mcp.InputActionsListResult](t, app, cx, mcp.ToolInputActionsList, nil)
	if !list.Configured || len(list.Actions) != 4 || len(list.Axes) != 1 {
		t.Fatalf("initial actions list = %+v", list)
	}
	if list.Axes[0].Name != "move_x" || list.Axes[0].Negative != "left" || list.Axes[0].Positive != "right" || list.Axes[0].Value != 0 {
		t.Fatalf("initial axis = %+v", list.Axes[0])
	}

	// 2. Tap "jump" (default mode) and press "right" (held across steps).
	tapRes := callTool[mcp.InputActionResult](t, app, cx, mcp.ToolInputAction, map[string]any{
		"name": "jump",
	})
	if tapRes.Mode != "tap" {
		t.Fatalf("tap result = %+v", tapRes)
	}
	pressRes := callTool[mcp.InputActionResult](t, app, cx, mcp.ToolInputAction, map[string]any{
		"name": "right",
		"mode": "press",
	})
	if pressRes.Mode != "press" {
		t.Fatalf("press result = %+v", pressRes)
	}

	// Step 1: both jump and right are JustPressed + Pressed; move_x == +1.
	app.Post(actions.Step)
	app.Settle()
	if !actions.Pressed("jump") || !actions.JustPressed("jump") {
		t.Fatalf("step 1 jump: pressed=%v jp=%v", actions.Pressed("jump"), actions.JustPressed("jump"))
	}
	if !actions.Pressed("right") || !actions.JustPressed("right") || actions.Axis("move_x") != 1 {
		t.Fatalf("step 1 right: pressed=%v jp=%v axis=%v", actions.Pressed("right"), actions.JustPressed("right"), actions.Axis("move_x"))
	}

	// List reflects current step state accurately.
	list1 := callTool[mcp.InputActionsListResult](t, app, cx, mcp.ToolInputActionsList, nil)
	var gotRight mcp.InputActionInfo
	for _, a := range list1.Actions {
		if a.Name == "right" {
			gotRight = a
		}
	}
	if !gotRight.Pressed || !gotRight.JustPressed || gotRight.BindingCount != 1 {
		t.Fatalf("step 1 list right = %+v", gotRight)
	}
	if list1.Axes[0].Value != 1 {
		t.Fatalf("step 1 list move_x = %+v", list1.Axes[0])
	}

	// Step 2: jump releases automatically (tap), right stays held.
	app.Post(actions.Step)
	app.Settle()
	if actions.Pressed("jump") || !actions.JustReleased("jump") {
		t.Fatalf("step 2 jump: pressed=%v jr=%v", actions.Pressed("jump"), actions.JustReleased("jump"))
	}
	if !actions.Pressed("right") || actions.JustPressed("right") || actions.Axis("move_x") != 1 {
		t.Fatalf("step 2 right: pressed=%v jp=%v axis=%v", actions.Pressed("right"), actions.JustPressed("right"), actions.Axis("move_x"))
	}

	// Release right.
	_ = callTool[mcp.InputActionResult](t, app, cx, mcp.ToolInputAction, map[string]any{
		"name": "right",
		"mode": "release",
	})
	app.Post(actions.Step)
	app.Settle()
	if actions.Pressed("right") || !actions.JustReleased("right") || actions.Axis("move_x") != 0 {
		t.Fatalf("step 3 right: pressed=%v jr=%v axis=%v", actions.Pressed("right"), actions.JustReleased("right"), actions.Axis("move_x"))
	}

	// 3. Inject 2D analog stick axes via game_input_axis and analog value via game_input_action.
	axRes := callTool[mcp.InputAxisResult](t, app, cx, mcp.ToolInputAxis, map[string]any{
		"axes": map[string]any{
			"move_x": -0.5,
			"move_y": 0.75,
		},
	})
	if axRes.Axes["move_x"] != -0.5 || axRes.Axes["move_y"] != 0.75 {
		t.Fatalf("axis inject result = %+v", axRes)
	}
	valRes := callTool[mcp.InputActionResult](t, app, cx, mcp.ToolInputAction, map[string]any{
		"name":  "throttle",
		"value": 0.25,
	})
	if valRes.Mode != "axis" || valRes.Value == nil || *valRes.Value != 0.25 {
		t.Fatalf("action value inject = %+v", valRes)
	}

	app.Post(actions.Step)
	app.Settle()
	if actions.Axis("move_x") != -0.5 || actions.Axis("move_y") != 0.75 || actions.Axis("throttle") != 0.25 {
		t.Fatalf("axes after step = (%v, %v, %v)", actions.Axis("move_x"), actions.Axis("move_y"), actions.Axis("throttle"))
	}

	// Clear axes cleanly.
	clrRes := callTool[mcp.InputAxisResult](t, app, cx, mcp.ToolInputAxis, map[string]any{
		"name":  "move_x",
		"axes":  map[string]any{"move_y": 0},
		"clear": true,
	})
	if !slices.Equal(clrRes.Cleared, []string{"move_x", "move_y"}) {
		t.Fatalf("cleared = %v, want [move_x move_y]", clrRes.Cleared)
	}
	_ = callTool[mcp.InputActionResult](t, app, cx, mcp.ToolInputAction, map[string]any{
		"name": "throttle",
		"mode": "release",
	})
	app.Post(actions.Step)
	app.Settle()
	if actions.Axis("move_x") != 0 || actions.Axis("move_y") != 0 || actions.Axis("throttle") != 0 {
		t.Fatalf("axes after clear = (%v, %v, %v), want all 0", actions.Axis("move_x"), actions.Axis("move_y"), actions.Axis("throttle"))
	}

	// 4. Right-click pointer press drives MouseButtonRight binding on Actions.
	_ = callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "click",
		"button": "right",
		"x":      10,
		"y":      20,
	})
	app.Post(actions.Step)
	app.Settle()
	if !actions.Pressed("fire") || !actions.JustPressed("fire") {
		t.Fatalf("right click did not trigger MouseButtonRight binding: pressed=%v jp=%v", actions.Pressed("fire"), actions.JustPressed("fire"))
	}
}

func TestInputPointerDragAndDynamicViewport(t *testing.T) {
	vp := screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFill}
	winSize := core.Size{Width: 800, Height: 600}
	var events []mcp.PointerEvent

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			ViewportFunc: func() screen.Viewport { return vp },
			ViewSizeFunc: func() core.Size { return winSize },
			OnPointer:    func(ev mcp.PointerEvent) { events = append(events, ev) },
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// In ModeFill (320x180 in 800x600): scale = 600/180 = 3.3333, ox = -133.3333.
	// Game (10, 90) maps to view x = -100 (clipped outside window -> in_viewport=false).
	clipped := callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "move",
		"x":      10,
		"y":      90,
	})
	if clipped.InViewport || math.Abs(float64(clipped.ViewX-(-100))) > 0.1 {
		t.Fatalf("ModeFill clipped point = %+v, want in_viewport=false view_x~-100", clipped)
	}
	if clipped.Button != "none" {
		t.Fatalf("hover move button = %q, want none", clipped.Button)
	}

	// Drag sequence: press -> move (preserves left button) -> release.
	_ = callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "press",
		"x":      160,
		"y":      90,
	})
	drag := callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "move",
		"x":      180,
		"y":      90,
	})
	if drag.Button != "left" {
		t.Fatalf("drag move button = %q, want left", drag.Button)
	}
	_ = callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{
		"action": "release",
		"x":      180,
		"y":      90,
	})

	if len(events) != 4 ||
		events[1].Action != mcp.PointerPress || events[1].Button != core.MouseButtonLeft ||
		events[2].Action != mcp.PointerMove || events[2].Button != core.MouseButtonLeft ||
		events[3].Action != mcp.PointerRelease || events[3].Button != core.MouseButtonLeft {
		t.Fatalf("drag events = %+v", events)
	}
}

func TestInputValidationAndNilSubsystems(t *testing.T) {
	var (
		cx     *core.Scope
		bridge *mcp.Bridge
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		var err error
		bridge, err = mcp.Attach(cx, mcp.Config{})
		if err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Nil Actions: actions_list succeeds gracefully with configured=false; action/axis return clean errors.
	list := callTool[mcp.InputActionsListResult](t, app, cx, mcp.ToolInputActionsList, nil)
	if list.Configured || len(list.Actions) != 0 || len(list.Axes) != 0 {
		t.Fatalf("nil actions list = %+v", list)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolInputAction, map[string]any{"name": "jump"}); err == nil || !strings.Contains(err.Error(), "no input actions") {
		t.Fatalf("action with nil Actions = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolInputAxis, map[string]any{"name": "move_x", "value": 0.5}); err == nil || !strings.Contains(err.Error(), "no input actions") {
		t.Fatalf("axis with nil Actions = %v", err)
	}

	// Nil Viewport & Scene: pointer maps 1:1 with in_viewport=true, hit=false, no panic.
	ptr := callTool[mcp.InputPointerResult](t, app, cx, mcp.ToolInputPointer, map[string]any{"x": 42, "y": 24})
	if !ptr.InViewport || ptr.Hit || ptr.GameX != 42 || ptr.GameY != 24 || ptr.ViewX != 42 || ptr.ViewY != 24 {
		t.Fatalf("nil viewport pointer = %+v", ptr)
	}

	// space="world" with no active scene returns clean error.
	if _, err := callToolRaw(t, app, cx, mcp.ToolInputPointer, map[string]any{"x": 10, "y": 20, "space": "world"}); err == nil || !strings.Contains(err.Error(), "requires an active scene") {
		t.Fatalf("world space without scene = %v", err)
	}

	// Attach Actions dynamically via SetActions and test strict argument validation.
	bridge.SetActions(input.NewActions())

	badCalls := []struct {
		name string
		tool string
		args any
		want string
	}{
		{"action unknown field", mcp.ToolInputAction, `{"name":"jump","bogus":1}`, "unknown field"},
		{"action missing name", mcp.ToolInputAction, `{}`, "name is required"},
		{"action bad mode", mcp.ToolInputAction, map[string]any{"name": "jump", "mode": "hold"}, "invalid mode"},
		{"action value out of range", mcp.ToolInputAction, map[string]any{"name": "move_x", "value": 1.5}, "in [-1, 1]"},
		{"axis unknown field", mcp.ToolInputAxis, `{"name":"move_x","value":0.5,"extra":true}`, "unknown field"},
		{"axis empty args", mcp.ToolInputAxis, `{}`, "name or axes is required"},
		{"axis missing value", mcp.ToolInputAxis, map[string]any{"name": "move_x"}, "value is required"},
		{"axis out of range", mcp.ToolInputAxis, map[string]any{"axes": map[string]any{"move_x": -1.1}}, "in [-1, 1]"},
		{"axis empty map key", mcp.ToolInputAxis, map[string]any{"axes": map[string]any{"": 0.5}}, "non-empty"},
		{"pointer unknown field", mcp.ToolInputPointer, `{"x":1,"y":2,"z":3}`, "unknown field"},
		{"pointer missing coords", mcp.ToolInputPointer, `{"x":1}`, "x and y are required"},
		{"pointer overflow float32", mcp.ToolInputPointer, `{"x":1e40,"y":0}`, "finite float32"},
		{"pointer bad action", mcp.ToolInputPointer, map[string]any{"x": 1, "y": 2, "action": "double"}, "invalid pointer action"},
		{"pointer bad space", mcp.ToolInputPointer, map[string]any{"x": 1, "y": 2, "space": "screen"}, "invalid space"},
		{"pointer bad button", mcp.ToolInputPointer, map[string]any{"x": 1, "y": 2, "button": "back"}, "invalid button"},
		{"actions_list unknown field", mcp.ToolInputActionsList, `{"all":true}`, "unknown field"},
	}

	for _, tc := range badCalls {
		if _, err := callToolRaw(t, app, cx, tc.tool, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %s(%v) = %v, want substring %q", tc.name, tc.tool, tc.args, err, tc.want)
		}
	}
}
