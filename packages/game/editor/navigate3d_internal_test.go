package editor

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
)

func TestViewStateFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*viewState]{
		Decode: parseViewState,
		Encode: encodeViewState,
		Files:  []string{"testdata/viewstate/editor.yaml"},
		Doc:    "editor",
	})
}

// The view state refuses what it could not restore: a scene listed twice, a path outside the project and a camera value that is not finite.
func TestViewStateRefusesWhatItCannotRestore(t *testing.T) {
	scene := func(path, extra string) string {
		return "  - path: " + path + "\n    camera3D:\n      target: [0, 0, 0]\n      yaw: 0\n      pitch: 0\n      distance: 240\n      fov: 1\n      near: 0.5\n      far: 100\n" + extra
	}
	for name, doc := range map[string]string{
		"twice":        "version: 1\nscenes:\n" + scene("a.yaml", "") + scene("a.yaml", ""),
		"outside":      "version: 1\nscenes:\n" + scene("../a.yaml", ""),
		"absolute":     "version: 1\nscenes:\n" + scene("/a.yaml", ""),
		"not finite":   "version: 1\nscenes:\n" + strings.Replace(scene("a.yaml", ""), "yaw: 0", "yaw: .nan", 1),
		"camera field": "version: 1\nscenes:\n" + scene("a.yaml", "      roll: 1\n"),
	} {
		if _, err := parseViewState([]byte(doc)); err == nil {
			t.Errorf("%s: parseViewState accepted\n%s", name, doc)
		}
	}
}

func FuzzParseViewState(f *testing.F) {
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\nscenes:\n  - path: a.yaml\n    camera3D:\n      target: [1, 2, 3]\n      yaw: 0.5\n      pitch: 0.25\n      distance: 9\n      fov: 1\n      near: 0.5\n      far: 100\n      orthographic: true\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := parseViewState(data)
		if err != nil {
			return
		}
		out, err := encodeViewState(v)
		if err != nil {
			t.Fatalf("encode of a parsed state: %v", err)
		}
		again, err := parseViewState(out)
		if err != nil {
			t.Fatalf("the encoded state does not parse: %v\n%s", err, out)
		}
		out2, err := encodeViewState(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("a second round trip changed the state:\n%s\n%s", out, out2)
		}
	})
}

// In orthographic every pixel's ray runs along the view, from the eye's plane, and Project and ScreenRay invert each other; the GPU view's camera puts every point where Project does.
func TestOrthographicProjectAndScreenRay(t *testing.T) {
	const w, h float32 = 640, 360
	cam := DefaultOrbitCamera3D()
	cam.Target = [3]float32{10, -5, 8}
	cam.Yaw, cam.Pitch, cam.Distance = 0.72, 0.38, 180
	cam.Orthographic = true
	_, _, forward := cam.Basis()
	sc := cam.SceneCamera(w, h)
	for _, p := range [][3]float32{{10, -5, 8}, {0, 0, 0}, {35, 22, -14}, {-28, 12, 41}, {5, -20, -30}} {
		sx, sy, depth, vis := cam.Project(p, w, h)
		if !vis {
			t.Fatalf("%v is not visible in orthographic", p)
		}
		orig, dir := cam.ScreenRay(sx, sy, w, h)
		for i := range 3 {
			if math.Abs(float64(dir[i]-forward[i])) > 1e-5 {
				t.Fatalf("the ray through %v runs along %v, want the view's forward %v", p, dir, forward)
			}
		}
		for i := range 3 {
			if got := orig[i] + dir[i]*depth; math.Abs(float64(got-p[i])) > 1e-2 {
				t.Fatalf("the ray through %v reaches %v at depth %v", p, [3]float32{orig[0] + dir[0]*depth, orig[1] + dir[1]*depth, orig[2] + dir[2]*depth}, depth)
			}
		}
		s, _, _ := sc.WorldToScreen(math3d.Vec3{X: p[0], Y: p[1], Z: p[2]}, w, h)
		if math.Abs(float64(s.X-sx)) > 0.05 || math.Abs(float64(s.Y-sy)) > 0.05 {
			t.Fatalf("the GPU view's camera puts %v at %v, Project at (%v, %v)", p, s, sx, sy)
		}
	}
	// What lies at Target keeps its size on screen when the projection switches.
	persp := cam
	persp.Orthographic = false
	_, up, _ := cam.Basis()
	edge := [3]float32{cam.Target[0] + up[0]*20, cam.Target[1] + up[1]*20, cam.Target[2] + up[2]*20}
	_, oy, _, _ := cam.Project(edge, w, h)
	_, py, _, _ := persp.Project(edge, w, h)
	if math.Abs(float64(oy-py)) > 0.01 {
		t.Fatalf("a point 20 up the view from Target is at y %v in orthographic and %v in perspective", oy, py)
	}
}

// Each axis view looks along its axis, straight down and up included, and AxisView names it; the basis stays orthonormal.
func TestAxisViews(t *testing.T) {
	want := map[View3D][3]float32{
		View3DFront:  {0, 0, -1},
		View3DBack:   {0, 0, 1},
		View3DRight:  {-1, 0, 0},
		View3DLeft:   {1, 0, 0},
		View3DTop:    {0, -1, 0},
		View3DBottom: {0, 1, 0},
	}
	for v, fwd := range want {
		cam := DefaultOrbitCamera3D()
		cam.Yaw = 3
		cam.SetView(v)
		right, up, forward := cam.Basis()
		for i := range 3 {
			if math.Abs(float64(forward[i]-fwd[i])) > 1e-5 {
				t.Fatalf("%v looks along %v, want %v", v, forward, fwd)
			}
		}
		if d := dot64(vec64(right), vec64(up)); math.Abs(d) > 1e-6 || math.Abs(len64(vec64(up))-1) > 1e-6 {
			t.Fatalf("%v: right %v and up %v are not orthonormal", v, right, up)
		}
		if got, ok := cam.AxisView(); !ok || got != v {
			t.Fatalf("AxisView after SetView(%v) = %v, %v", v, got, ok)
		}
		cam.Orbit(0.2, 0)
		if got, ok := cam.AxisView(); ok {
			t.Fatalf("AxisView after an orbit from %v = %v, want none", v, got)
		}
	}
	// The top view has -Z at the top of the screen.
	cam := DefaultOrbitCamera3D()
	cam.SetView(View3DTop)
	if _, up, _ := cam.Basis(); math.Abs(float64(up[2]+1)) > 1e-5 {
		t.Fatalf("the top view's up is %v, want -Z", up)
	}
}

// The axis widget draws its ends from the camera's basis and a press on one picks its view, the one nearer the viewer where two overlap.
func TestAxisWidgetPicksTheNearerEnd(t *testing.T) {
	cam := DefaultOrbitCamera3D()
	cam.SetView(View3DFront)
	// Looking along -Z, +Z points at the viewer and -Z away from it, both at the center.
	c := axisWidgetSize3D / 2
	if v, ok := pickAxisMark3D(cam, c, c); !ok || v != View3DFront {
		t.Fatalf("a press at the center of the front view picks %v, %v, want Front", v, ok)
	}
	if v, ok := pickAxisMark3D(cam, c+axisWidgetReach3D, c); !ok || v != View3DRight {
		t.Fatalf("a press at the +X end picks %v, %v, want Right", v, ok)
	}
	if v, ok := pickAxisMark3D(cam, c, c-axisWidgetReach3D); !ok || v != View3DTop {
		t.Fatalf("a press at the +Y end picks %v, %v, want Top", v, ok)
	}
	if _, ok := pickAxisMark3D(cam, 2, 2); ok {
		t.Fatal("a press in the widget's corner picked a view")
	}
	var d core.Drawing
	drawAxisWidget3D(&d, cam, core.RGBA(0xFFFFFF, 1))
	texts := 0
	for _, op := range d.Ops {
		if op.Kind == core.DrawFillText {
			texts++
		}
	}
	if texts != 3 {
		t.Fatalf("the widget draws %d letters, want X, Y and Z", texts)
	}
}

// A touchpad's scroll pans the 3D view, a mouse wheel or a scroll with Control or Command zooms it, and while flying the wheel changes the fly speed.
func TestViewport3DWheelPansOrZooms(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   core.WheelEvent
		pan  bool
	}{
		{"touchpad scroll pans", core.WheelEvent{Touchpad: true, Delta: core.Point{X: 30, Y: 20}}, true},
		{"touchpad scroll with Control zooms", core.WheelEvent{Touchpad: true, Delta: core.Point{Y: 40}, Modifiers: core.Modifiers{Control: true}}, false},
		{"touchpad scroll with Command zooms", core.WheelEvent{Touchpad: true, Delta: core.Point{Y: 40}, Modifiers: core.Modifiers{Platform: true}}, false},
		{"mouse wheel zooms", core.WheelEvent{Delta: core.Point{Y: 40}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed, err := New(nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			ed.SetViewMode(ViewMode3D)
			ed.SetCanvasSize(640, 360)
			before := ed.Camera3D
			var g viewport3DGestures
			g.wheel(ed, tc.ev)
			after := ed.Camera3D
			if tc.pan {
				if after.Distance != before.Distance || after.Target == before.Target {
					t.Fatalf("camera %+v after %+v, want a pan at the same distance", after, before)
				}
				// The scene follows the fingers: what was at Target moves by the scroll.
				sx, sy, _, _ := after.Project(before.Target, 640, 360)
				if math.Abs(float64(sx-320-30)) > 0.05 || math.Abs(float64(sy-180-20)) > 0.05 {
					t.Fatalf("the old Target is at (%v, %v) after a scroll of (30, 20), want (350, 200)", sx, sy)
				}
				return
			}
			if after.Target != before.Target || after.Distance == before.Distance {
				t.Fatalf("camera %+v after %+v, want a zoom", after, before)
			}
		})
	}
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.SetViewMode(ViewMode3D)
	if !ed.BeginFly3D() {
		t.Fatal("BeginFly3D refused")
	}
	speed, cam := ed.FlySpeed3D, ed.Camera3D
	var g viewport3DGestures
	g.wheel(ed, core.WheelEvent{Delta: core.Point{Y: -200}})
	if ed.FlySpeed3D != speed*2 || ed.Camera3D != cam {
		t.Fatalf("a wheel turn while flying left the speed %v (from %v) and the camera %+v", ed.FlySpeed3D, speed, ed.Camera3D)
	}
}

// Two fingers on a touch screen pan the 3D view by the movement of their center and put back a gizmo drag the first finger began; the pan the first finger started then neither drags nor orbits.
func TestViewport3DTwoFingerTouchPans(t *testing.T) {
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.SetViewMode(ViewMode3D)
	ed.SetCanvasSize(640, 360)
	var g viewport3DGestures
	noShift := func() bool { return false }
	one := func(phase core.TouchPhase, p core.Point) core.TouchEvent {
		return core.TouchEvent{Phase: phase, Touches: []core.Touch{{ID: 1, Position: p, Changed: true}}}
	}
	two := func(phase core.TouchPhase, a, b core.Point) core.TouchEvent {
		return core.TouchEvent{Phase: phase, Touches: []core.Touch{{ID: 1, Position: a}, {ID: 2, Position: b, Changed: true}}}
	}
	g.touch(ed, one(core.TouchStart, core.Point{X: 100, Y: 100}))
	g.pan(ed, core.PanEvent{Phase: core.GestureBegan, Translation: core.Point{X: 10}}, noShift)
	if ed.drag3D.kind != drag3DOrbit {
		t.Fatalf("a one-finger drag began %v, want an orbit", ed.drag3D.kind)
	}
	g.touch(ed, two(core.TouchStart, core.Point{X: 100, Y: 100}, core.Point{X: 200, Y: 100}))
	if ed.drag3D.kind != drag3DNone {
		t.Fatalf("a second finger left the %v drag on", ed.drag3D.kind)
	}
	cam := ed.Camera3D
	g.pan(ed, core.PanEvent{Phase: core.GestureChanged, Translation: core.Point{X: 40, Y: 30}}, noShift)
	if ed.Camera3D != cam {
		t.Fatal("the first finger's pan moved the camera while two fingers were down")
	}
	g.touch(ed, two(core.TouchMove, core.Point{X: 100, Y: 100}, core.Point{X: 200, Y: 100}))
	target := ed.Camera3D.Target
	g.touch(ed, two(core.TouchMove, core.Point{X: 120, Y: 90}, core.Point{X: 220, Y: 90}))
	sx, sy, _, _ := ed.Camera3D.Project(target, 640, 360)
	if math.Abs(float64(sx-320-20)) > 0.05 || math.Abs(float64(sy-180+10)) > 0.05 {
		t.Fatalf("the old target is at (%v, %v) after the fingers moved (20, -10), want (340, 170)", sx, sy)
	}
}

// Zoomed out past the far plane, an orthographic view still shows Target and what lies around it: Distance only sets how much the view shows, never how far back the eye sits.
func TestOrthographicZoomedOutPastFarStillShowsTarget(t *testing.T) {
	const w, h float32 = 640, 360
	cam := DefaultOrbitCamera3D()
	cam.Target = [3]float32{40, 0, -25}
	cam.SetView(View3DTop)
	cam.Orthographic = true
	cam.Distance = maxOrbitDistance
	if cam.Distance <= cam.Far {
		t.Fatalf("distance %v does not reach past far %v", cam.Distance, cam.Far)
	}
	for _, p := range [][3]float32{cam.Target, {cam.Target[0], 500, cam.Target[2]}, {cam.Target[0] + 1000, -500, cam.Target[2]}} {
		_, _, depth, vis := cam.Project(p, w, h)
		if !vis || depth <= cam.Near || depth >= cam.Far {
			t.Fatalf("%v is at depth %v visible %v with near %v and far %v", p, depth, vis, cam.Near, cam.Far)
		}
	}
	s, _, ok := cam.SceneCamera(w, h).WorldToScreen(math3d.Vec3{X: cam.Target[0], Y: cam.Target[1], Z: cam.Target[2]}, w, h)
	if !ok || math.Abs(float64(s.X-w/2)) > 0.05 || math.Abs(float64(s.Y-h/2)) > 0.05 {
		t.Fatalf("the GPU view's camera puts Target at %v, ok %v", s, ok)
	}
}

// The camera's navigation refuses a move or turn that is not finite, so a NaN never reaches the camera or the view state.
func TestCameraNavigationRefusesNonFiniteInput(t *testing.T) {
	nan, inf := float32(math.NaN()), float32(math.Inf(1))
	cam := DefaultOrbitCamera3D()
	want := cam
	cam.Look(nan, 0)
	cam.Look(0, inf)
	cam.Fly(nan, 0, 0)
	cam.Fly(0, inf, 0)
	cam.Fly(0, 0, nan)
	if cam != want {
		t.Fatalf("non-finite Look and Fly changed the camera to %+v", cam)
	}
}
