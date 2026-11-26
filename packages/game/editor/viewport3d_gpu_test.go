package editor_test

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// rayHitsSphere reports whether the ray from orig along the unit dir passes within r of c.
func rayHitsSphere(orig, dir, c [3]float32, r float64) bool {
	var oc [3]float64
	for i := range 3 {
		oc[i] = float64(c[i] - orig[i])
	}
	t := oc[0]*float64(dir[0]) + oc[1]*float64(dir[1]) + oc[2]*float64(dir[2])
	d2 := oc[0]*oc[0] + oc[1]*oc[1] + oc[2]*oc[2] - t*t
	return t > 0 && d2 <= r*r
}

// rayHitsBox reports whether the ray from orig along dir passes through the axis-aligned box [lo, hi].
func rayHitsBox(orig, dir, lo, hi [3]float32) bool {
	tMin, tMax := 0.0, math.Inf(1)
	for i := range 3 {
		if dir[i] == 0 {
			if orig[i] < lo[i] || orig[i] > hi[i] {
				return false
			}
			continue
		}
		t0 := float64(lo[i]-orig[i]) / float64(dir[i])
		t1 := float64(hi[i]-orig[i]) / float64(dir[i])
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		tMin, tMax = max(tMin, t0), min(tMax, t1)
		if tMax < tMin {
			return false
		}
	}
	return true
}

// TestPick3DHitsAMeshByItsGeometry presses a sphere 40 across: at its center it is selected, and just inside a corner of its bounding box, where its old sprite-sized box and its bounds are hit but the sphere is not, the press passes through it and clears the selection.
func TestPick3DHitsAMeshByItsGeometry(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "ball", Mesh: "sphere", ScaleX: sceneformat.Float32(40), ScaleY: sceneformat.Float32(40), ScaleZ: sceneformat.Float32(40)})
	const w, h = 640, 360
	if _, err := ed.Scene3D(); err != nil {
		t.Fatalf("Scene3D: %v", err)
	}
	obj, _, _ := ed.FindObject("ball")
	lo, hi := ed.ObjectAABB3D(obj)
	if hi[0]-lo[0] < 39 || hi[0]-lo[0] > 41 {
		t.Fatalf("the sphere's box is %v..%v, want its geometry's 40 across", lo, hi)
	}
	cx, cy, _, _ := ed.Camera3D.Project([3]float32{}, w, h)
	if id, ok := ed.Pick3DObject(cx, cy, w, h); !ok || id != "ball" {
		t.Fatalf("a press at the sphere's center picked %q, %v", id, ok)
	}

	// The box corner whose projection lies farthest from the sphere's center is on the edge of the box's silhouette; a few pixels in from it the box is hit and the sphere is not.
	var px, py float32
	far := -1.0
	for _, c := range ed.ObjectCorners3D(obj) {
		x, y, _, _ := ed.Camera3D.Project(c, w, h)
		if d := math.Hypot(float64(x-cx), float64(y-cy)); d > far {
			far, px, py = d, x, y
		}
	}
	px += (cx - px) * 4 / float32(far)
	py += (cy - py) * 4 / float32(far)
	orig, dir := ed.Camera3D.ScreenRay(px, py, w, h)
	if !rayHitsBox(orig, dir, lo, hi) || rayHitsSphere(orig, dir, [3]float32{}, 20) {
		t.Fatalf("the press at (%v, %v) should hit the box and miss the sphere", px, py)
	}
	ed.SelectObject("ball")
	if ed.Select3DAt(px, py, w, h) || ed.SelectedID != "" {
		t.Fatalf("a press beside the sphere, inside its bounds, selected %q", ed.SelectedID)
	}

	// A locked sphere is passed over even at its center.
	ed.SetObjectLocked("ball", true)
	if id, ok := ed.Pick3DObject(cx, cy, w, h); ok {
		t.Fatalf("a press on a locked sphere picked %q", id)
	}

	// Frame Selection frames the sphere's geometry, closer than the default camera.
	ed.SelectObject("ball")
	if !ed.FrameSelection() {
		t.Fatal("FrameSelection framed nothing")
	}
	if c := ed.Camera3D; math.Abs(float64(c.Target[0]))+math.Abs(float64(c.Target[1]))+math.Abs(float64(c.Target[2])) > 0.01 || c.Distance >= editor.DefaultOrbitCamera3D().Distance {
		t.Fatalf("framed the sphere with target %v at distance %v", c.Target, c.Distance)
	}
}

// TestPick3DSelectsAnInstanceByItsGeometry picks a mesh that an instanced scene adds: the press selects the instanced child directly so it can be edited through overrides.
func TestPick3DSelectsAnInstanceByItsGeometry(t *testing.T) {
	doc := &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion, Layers: []sceneformat.LayerDoc{{Name: "world", Objects: []sceneformat.ObjectDoc{{ID: "rock", Scene: "scenes/rock.scene.yaml"}}}}}
	ed, err := editor.New(doc, sceneFiles{AtlasMap: testResolver(), files: map[string]string{
		"scenes/rock.scene.yaml": "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: body\n        posZ: 30\n        scaleX: 20\n        scaleY: 20\n        scaleZ: 20\n        mesh: box\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	const w, h = 640, 360
	x, y, _, _ := ed.Camera3D.Project([3]float32{0, 0, 30}, w, h)
	if id, ok := ed.Pick3DObject(x, y, w, h); !ok || id != "rock/body" {
		t.Fatalf("a press on the instanced box picked %q, %v, want \"rock/body\"", id, ok)
	}
}

// TestViewport3DDrawsTheSceneOnTheGPUView mounts the editor on a target with a GPU: the 3D viewport's GPU view draws the courtyard's geometry, teal sphere included, and the canvas over it draws only the overlays, not the scene's background; each shading mode changes the picture or the overlay.
func TestViewport3DDrawsTheSceneOnTheGPUView(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	// A small window keeps the software GPU's frames quick.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(820, 560))
	app.Advance(20 * time.Millisecond)
	if !ed.Viewport3DOnGPU() {
		t.Fatalf("the GPU view does not draw the scene (error %v)", ed.Viewport3DError())
	}
	lit, err := app.GPUReadPixels("id:editor-viewport-3d")
	if err != nil {
		t.Fatal(err)
	}
	if n := tealPixels(lit); n < 50 {
		t.Fatalf("the GPU view shows %d teal pixels, want the sphere", n)
	}
	cw, ch := ed.CanvasSize()
	overlay := &core.Drawing{}
	ed.DrawViewport(overlay, core.Size{Width: core.Points(cw), Height: core.Points(ch)})
	bg, _ := core.ParseColor("#141824")
	if canvasBackgroundFilled(overlay, cw, ch, bg) || canvasBackgroundFilled(overlay, cw, ch, core.RGBA(0x141824, 1)) {
		t.Fatal("the canvas over the GPU view paints a background over the scene")
	}
	if got := fmt.Sprint(app.Find("id:editor-viewport").Ops); got != fmt.Sprint(overlay.Ops) {
		t.Fatal("the mounted canvas does not draw the overlay DrawViewport draws")
	}

	app.Post(func() { ed.SetShading3D(editor.Shading3DUnlit) })
	app.Settle()
	app.Advance(20 * time.Millisecond)
	if ed.Shading3D != editor.Shading3DUnlit {
		t.Fatalf("shading %v after pressing Unlit", ed.Shading3D)
	}
	unlit, _ := app.GPUReadPixels("id:editor-viewport-3d")
	if string(unlit.Pix) == string(lit.Pix) {
		t.Fatal("the unlit view looks the same as the lit one")
	}

	if err := ed.ExecuteCommand(editor.CmdShadingWire); err != nil {
		t.Fatalf("ExecuteCommand(%s): %v", editor.CmdShadingWire, err)
	}
	app.Settle()
	wire := app.Find("id:editor-viewport").Ops
	if len(wire) <= len(overlay.Ops) {
		t.Fatalf("the wireframe overlay draws %d operations, no more than the lit overlay's %d", len(wire), len(overlay.Ops))
	}
	if err := ed.ExecuteCommand(editor.CmdShadingLit); err != nil || ed.Shading3D != editor.Shading3DLit {
		t.Fatalf("ExecuteCommand(%s) = %v, shading %v", editor.CmdShadingLit, err, ed.Shading3D)
	}

	// Switching to 2D unmounts the GPU view, and the canvas draws the 2D scene.
	app.Post(func() { ed.SetViewMode(editor.ViewMode2D) })
	app.Settle()
	if ed.Viewport3DOnGPU() || app.Exists("id:editor-viewport-3d") {
		t.Fatal("the GPU view outlived the 3D view mode")
	}
}

func tealPixels(img *image.RGBA) int {
	n := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			c := img.RGBAAt(x, y)
			if int(c.G) > int(c.R)+40 && int(c.B) > int(c.R)+40 && c.G > 80 {
				n++
			}
		}
	}
	return n
}

// TestViewport3DWithoutAGPUDrawsTheWireframeView mounts the editor where the target has no GPU: the 3D viewport stays the wireframe view, drawing the scene's background and a box around each object's geometry.
func TestViewport3DWithoutAGPUDrawsTheWireframeView(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1200, 720), nsgotest.NoGPU())
	app.Advance(50 * time.Millisecond)
	if ed.Viewport3DOnGPU() {
		t.Fatal("the viewport reports a GPU on a target without one")
	}
	if v := app.Find("id:editor-viewport-3d"); v == nil || v.Kind == nsgotest.KindGPU {
		t.Fatalf("without a GPU the 3D viewport is %v, want a plain view", v)
	}
	cw, ch := ed.CanvasSize()
	d := &core.Drawing{}
	ed.DrawViewport(d, core.Size{Width: core.Points(cw), Height: core.Points(ch)})
	if !canvasBackgroundFilled(d, cw, ch, core.RGBA(0x141824, 1)) {
		bg, _ := core.ParseColor("#141824")
		if !canvasBackgroundFilled(d, cw, ch, bg) {
			t.Fatal("the wireframe view does not fill the scene's background")
		}
	}
	if got := fmt.Sprint(app.Find("id:editor-viewport").Ops); got != fmt.Sprint(d.Ops) {
		t.Fatal("the mounted canvas does not draw the wireframe view")
	}
	// The crate's box is its model's bounds, a 1 m crate with its lid, not a sprite-sized marker.
	crate, _, _ := ed.FindObject("crate")
	lo, hi := ed.ObjectAABB3D(crate)
	if size := hi[1] - lo[1]; size < 1 || size > 2 {
		t.Fatalf("the crate's box is %v tall, want its model's height", size)
	}
	// Picking works by geometry without a GPU too.
	built, err := ed.Scene3D()
	if err != nil {
		t.Fatal(err)
	}
	ball := built.Objects["ball"].WorldTransform().Pos
	x, y, _, _ := ed.Camera3D.Project([3]float32{ball.X, ball.Y, ball.Z}, cw, ch)
	app.Post(func() { ed.HandleCanvasPress(x, y) })
	app.Settle()
	if ed.SelectedID != "ball" {
		t.Fatalf("a press on the sphere selected %q", ed.SelectedID)
	}
}

// TestViewport3DReportsASceneThatDoesNotBuild names a model with no project open to load it from: the viewport says why in its corner and still draws every object's box.
func TestViewport3DReportsASceneThatDoesNotBuild(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "statue", Model: "models/statue.glb"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1200, 720))
	app.Advance(50 * time.Millisecond)
	if _, err := ed.Scene3D(); err == nil {
		t.Fatal("a model with nowhere to load it from built")
	}
	label := app.Find("id:editor-viewport-3d-error")
	if label == nil || !strings.Contains(label.Text, "statue") {
		t.Fatalf("the viewport's error is %v, want one naming the object", label)
	}
	obj, _, _ := ed.FindObject("statue")
	lo, hi := ed.ObjectAABB3D(obj)
	if hi[0]-lo[0] <= 0 {
		t.Fatal("the object that does not build has no box")
	}
}

// TestScene3DShowsTheClipPoseAndThePlayingGame poses the courtyard's sphere: an open clip moves its built node to the keyed x at the playhead without changing the scene, and while the game plays the node, and the box the viewport draws around it, follow what the game does to the object.
func TestScene3DShowsTheClipPoseAndThePlayingGame(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	built, err := ed.Scene3D()
	if err != nil {
		t.Fatal(err)
	}
	ball := built.Objects["ball"]
	if err := ed.NewClip("ball", "Roll", time.Second); err != nil {
		t.Fatalf("NewClip: %v", err)
	}
	if _, _, err := ed.KeyProperty("", "x", 0); err != nil {
		t.Fatalf("KeyProperty x @0: %v", err)
	}
	tr, k, err := ed.KeyProperty("", "x", time.Second)
	if err != nil {
		t.Fatalf("KeyProperty x @1s: %v", err)
	}
	if err := ed.SetKeyValue(tr, k, 2.5); err != nil {
		t.Fatalf("SetKeyValue: %v", err)
	}
	ed.SeekClip(time.Second)
	if again, _ := ed.Scene3D(); again != built || ball.WorldTransform().Pos.X != 2.5 {
		t.Fatalf("at the clip's end the sphere's node is at x %v (same build %v), want 2.5", ball.WorldTransform().Pos.X, again == built)
	}
	if obj, _, _ := ed.FindObject("ball"); obj.X != 0.9 {
		t.Fatalf("the edited scene took the clip's x %v", obj.X)
	}
	if err := ed.CloseClip(); err != nil {
		t.Fatal(err)
	}
	if ed.Scene3D(); ball.WorldTransform().Pos.X != 0.9 {
		t.Fatalf("with the clip closed the sphere's node is at x %v, want 0.9", ball.WorldTransform().Pos.X)
	}

	ed.OnPlayUpdate = func(scene *sceneformat.BuiltScene, dt time.Duration) {
		if o := scene.Object("ball"); o != nil {
			o.Doc.X += float32(dt.Seconds())
		}
	}
	// The software GPU would draw every frame of the game at the window's size, so the playing game is watched through the wireframe view, which draws the same posed scene.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1200, 720), nsgotest.NoGPU())
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Advance(200 * time.Millisecond)
	playing, err := ed.Scene3D()
	if err != nil {
		t.Fatal(err)
	}
	obj, _, _ := ed.FindObject("ball")
	if x := playing.Objects["ball"].WorldTransform().Pos.X; obj.X <= 0.9 || x != obj.X {
		t.Fatalf("while playing the sphere's node is at x %v and the object at %v, want both moved past 0.9", x, obj.X)
	}
	if lo, hi := ed.ObjectAABB3D(obj); (lo[0]+hi[0])/2 < obj.X-0.01 || (lo[0]+hi[0])/2 > obj.X+0.01 {
		t.Fatalf("while playing the sphere's box is centered at x %v, want %v", (lo[0]+hi[0])/2, obj.X)
	}
	app.Post(func() {
		if err := ed.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	app.Settle()
	stopped, _ := ed.Scene3D()
	if x := stopped.Objects["ball"].WorldTransform().Pos.X; x != 0.9 {
		t.Fatalf("after Stop the sphere's node is at x %v, want 0.9", x)
	}
}
