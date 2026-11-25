package editor

import (
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/gpu"
)

const (
	gpuViewWidth  = 192
	gpuViewHeight = 144
)

// courtyardResolver serves the courtyard's material files, as a game's editor resolves the materials its scenes name, so the 2D build, which Play and the clip preview pose, builds; it loads no models, which the 3D build loads from the project's files.
type courtyardResolver struct {
	sceneformat.AtlasMap
	materials *scenemodel.Assets
}

func (r courtyardResolver) Material(path string) (*resource.Material, error) {
	return r.materials.Material(path)
}

// openCourtyard opens a copy of scenemodel's test project, whose level has a textured GLB crate, a lid from the same model, a stone floor, a teal sphere and a glass capsule, in an editor whose Resolver serves the material files but not the models, so the 3D build loads the models from the project's files. The 3D camera frames the level from the front right, as scenemodel's own test draws it.
func openCourtyard(t *testing.T) *Editor {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "scenemodel", "testdata", "project", "models"))); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"scenes", "materials"} {
		if err := os.CopyFS(filepath.Join(dir, sub), os.DirFS(filepath.Join("..", "scenemodel", "testdata", "project", sub))); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "crate.glb"), filepath.Join(dir, "models", "crate.glb")); err != nil {
		t.Fatal(err)
	}
	game := "version: 1\nname: Courtyard\nid: dev.example.courtyard\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level\n  scenes:\n    - name: level\n      path: scenes/level.scene.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(game), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := New(nil, courtyardResolver{materials: scenemodel.NewAssets(os.DirFS(dir))})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SetViewMode(ViewMode3D)
	// The camera at (2.6, 3.1, 4.6), looking at (0, 0.4, 0).
	ed.Camera3D = OrbitCamera3D{
		Target:   [3]float32{0, 0.4, 0},
		Yaw:      float32(math.Atan2(2.6, 4.6)),
		Pitch:    float32(math.Asin(2.7 / math.Sqrt(2.6*2.6+2.7*2.7+4.6*4.6))),
		Distance: float32(math.Sqrt(2.6*2.6 + 2.7*2.7 + 4.6*4.6)),
		FOV:      float32(math.Pi / 4),
		Near:     0.1,
		Far:      40,
	}
	return ed
}

// OpenCourtyard opens the courtyard project for the tests outside the package (openCourtyard).
var OpenCourtyard = openCourtyard

// softwareViewport renders ed's 3D viewport on a software GPU of the viewport's size, holding the renderer across frames as the GPU view does.
type softwareViewport struct {
	t  *testing.T
	sr *gpu.SoftwareRenderer
	r  *viewport3DRenderer
}

func newSoftwareViewport(t *testing.T, ed *Editor) *softwareViewport {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(gpuViewWidth, gpuViewHeight, viewport3DDepthFormat)
	t.Cleanup(sr.Close)
	r, err := newViewport3DRenderer(ed, sr.Device())
	if err != nil {
		t.Fatalf("newViewport3DRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := r.close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return &softwareViewport{t: t, sr: sr, r: r}
}

// frame renders one frame and returns whether it recorded one, and the surface after it.
func (v *softwareViewport) frame() (bool, *image.RGBA) {
	v.t.Helper()
	dev := v.sr.Device()
	enc := dev.BeginEncoder()
	drew, err := v.r.render(enc, gpuViewWidth, gpuViewHeight)
	if err != nil {
		v.t.Fatalf("render: %v", err)
	}
	dev.Submit(enc.Finish())
	v.sr.WaitIdle()
	if err := v.sr.LastError(); err != nil {
		v.t.Fatalf("software renderer: %v", err)
	}
	src := v.sr.Image()
	img := image.NewRGBA(src.Bounds())
	copy(img.Pix, src.Pix)
	return drew, img
}

// projectPixel returns the surface pixel the world point p lands on through the editor's camera.
func projectPixel(ed *Editor, p math3d.Vec3) (int, int) {
	x, y, _, ok := ed.Camera3D.Project([3]float32{p.X, p.Y, p.Z}, gpuViewWidth, gpuViewHeight)
	if !ok {
		return -1, -1
	}
	return int(x), int(y)
}

// TestViewport3DRendersTheSceneWithRender3D draws the courtyard through the editor's 3D viewport renderer on gpu.NewSoftwareRenderer and compares it with testdata/golden/viewport3d.png: the crate shaded and textured, the floor, the sphere and the glass lit by the default rig under the gradient sky. UPDATE_GOLDEN=1 rewrites the reference.
func TestViewport3DRendersTheSceneWithRender3D(t *testing.T) {
	ed := openCourtyard(t)
	built, err := ed.Scene3D()
	if err != nil {
		t.Fatalf("Scene3D: %v", err)
	}
	v := newSoftwareViewport(t, ed)
	drew, img := v.frame()
	if !drew {
		t.Fatal("the first frame recorded nothing")
	}
	assertViewportReference(t, gametest.GoldenPath("", "viewport3d"), img)

	// The crate's front face shows both colors of its checkerboard texture.
	world := built.Objects["crate"].Children()[0].WorldMatrix()
	light, dark := 0, 0
	for _, u := range []float32{-0.35, -0.15, 0.05, 0.25} {
		for _, w := range []float32{-0.35, -0.15, 0.05, 0.25} {
			x, y := projectPixel(ed, world.TransformPoint(math3d.Vec3{X: u, Y: w, Z: 0.5}))
			c := img.RGBAAt(x, y)
			switch {
			case c.R > 120 && c.R > c.B+60:
				light++
			case c.R < 90 && c.R >= c.B:
				dark++
			}
		}
	}
	if light == 0 || dark == 0 {
		t.Errorf("crate face samples: %d light and %d dark planks, want both", light, dark)
	}
	// The sky shows above the floor: bluer at the top than at the horizon.
	if top := img.RGBAAt(gpuViewWidth/2, 0); top.B <= top.R {
		t.Errorf("the top of the view is %v, want the sky's blue", top)
	}

	// A frame with nothing changed records nothing.
	if drew, _ := v.frame(); drew {
		t.Error("a frame with nothing changed was recorded again")
	}

	// Unlit shading draws the sphere one flat color on its lit and shaded sides.
	ed.SetShading3D(Shading3DUnlit)
	_, unlit := v.frame()
	ball := built.Objects["ball"].WorldTransform().Pos
	lx, ly := projectPixel(ed, ball.Add(math3d.Vec3{X: 0.3, Y: 0.3, Z: 0.15}))
	sx, sy := projectPixel(ed, ball.Add(math3d.Vec3{X: -0.3, Y: -0.3, Z: 0.15}))
	if a, b := unlit.RGBAAt(lx, ly), unlit.RGBAAt(sx, sy); absDiff8(a.R, b.R) > 2 || absDiff8(a.G, b.G) > 2 || absDiff8(a.B, b.B) > 2 {
		t.Errorf("unlit ball sides %v and %v differ, want one flat color", a, b)
	}
	if a, b := img.RGBAAt(lx, ly), img.RGBAAt(sx, sy); a == b {
		t.Errorf("lit ball sides are both %v, want the light to shade them", a)
	}

	// With the editor's own lights the scene's background replaces the sky.
	ed.SetShading3D(Shading3DLit)
	lights := DefaultLights3D()
	ed.SetLights3D(&lights)
	_, own := v.frame()
	bg := ed.background3D()
	r, g, b, _ := bg.Components()
	if c := own.RGBAAt(gpuViewWidth/2, 0); absDiff8(c.R, uint8(r*255+0.5)) > 1 || absDiff8(c.G, uint8(g*255+0.5)) > 1 || absDiff8(c.B, uint8(b*255+0.5)) > 1 {
		t.Errorf("with Lights3D the top of the view is %v, want the scene's background %v", c, bg)
	}
}

// TestViewport3DFollowsAGizmoDragWithoutRebuilding drags the crate along X with the gizmo: every frame of the drag draws the crate where it is, and the scene is not built or uploaded again until the drag ends, which builds it once.
func TestViewport3DFollowsAGizmoDragWithoutRebuilding(t *testing.T) {
	ed := openCourtyard(t)
	ed.SelectObject("crate")
	v := newSoftwareViewport(t, ed)
	_, before := v.frame()
	gen, scene := ed.s3d.gen, v.r.scene
	built, _ := ed.Scene3D()
	crate := built.Objects["crate"]
	start := crate.WorldTransform().Pos

	// Press on the X arrow of the gizmo, two thirds of the way along it, and drag right.
	obj, _ := ed.SelectedObject()
	origin := ed.worldPosition3D(obj)
	l := ed.GizmoLength3D(origin)
	px, py, _, ok := ed.Camera3D.Project([3]float32{origin[0] + l*0.66, origin[1], origin[2]}, gpuViewWidth, gpuViewHeight)
	if !ok {
		t.Fatal("the gizmo's X arrow is off the view")
	}
	if axis := ed.BeginGizmoDrag3D(px, py, gpuViewWidth, gpuViewHeight); axis != Axis3DX {
		t.Fatalf("BeginGizmoDrag3D picked %v, want X", axis)
	}
	var last *image.RGBA
	for step := 1; step <= 3; step++ {
		ed.UpdateGizmoDrag3D(px+float32(step*8), py)
		drew, img := v.frame()
		if !drew {
			t.Fatalf("drag step %d recorded no frame", step)
		}
		if ed.s3d.gen != gen || v.r.scene != scene {
			t.Fatalf("drag step %d built or uploaded the scene again", step)
		}
		pos := crate.WorldTransform().Pos
		if obj.X <= start.X || pos.X != obj.X {
			t.Fatalf("drag step %d: crate node at x %v, object at x %v, want both right of %v", step, pos.X, obj.X, start.X)
		}
		last = img
	}
	if samePixels(before, last) {
		t.Fatal("the dragged crate draws where it was")
	}
	if !ed.EndGizmoDrag3D() {
		t.Fatal("the drag committed nothing")
	}
	if _, err := ed.Scene3D(); err != nil || ed.s3d.gen != gen {
		t.Fatalf("committing a move built the scene again (generation %d, was %d; %v), though its look did not change", ed.s3d.gen, gen, err)
	}
	ed.History.Undo()
	_, undone := v.frame()
	if !samePixels(before, undone) {
		t.Error("after Undo the viewport does not draw the crate where it was")
	}

	// Changing what an object draws builds the scene again.
	if err := ed.SetObjectMesh("ball", "box"); err != nil {
		t.Fatalf("SetObjectMesh: %v", err)
	}
	if v.frame(); ed.s3d.gen == gen || v.r.scene == scene {
		t.Error("turning the sphere into a box did not build and upload the scene again")
	}
}

func samePixels(a, b *image.RGBA) bool {
	return string(a.Pix) == string(b.Pix)
}

func absDiff8(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

// assertViewportReference compares got with the reference PNG at path, allowing a few edge pixels to differ by rounding between CPU architectures (fused multiply-add on arm64), and writes got and a diff image on mismatch. UPDATE_GOLDEN=1 rewrites the reference.
func assertViewportReference(t *testing.T, path string, got *image.RGBA) {
	t.Helper()
	name := strings.TrimSuffix(filepath.Base(path), ".png")
	if gametest.ShouldUpdateGolden() {
		if err := gametest.WritePNG(path, got); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := gametest.ReadPNG(path)
	if err != nil {
		t.Fatalf("read reference %s: %v (UPDATE_GOLDEN=1 writes it)", path, err)
	}
	if !want.Bounds().Eq(got.Bounds()) {
		t.Fatalf("%s: size %v, reference %v", name, got.Bounds(), want.Bounds())
	}
	const channelTolerance = 3
	far := 0
	b := got.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := got.RGBAAt(x, y)
			wr, wg, wb, wa := want.At(x, y).RGBA()
			w := [4]int{int(wr >> 8), int(wg >> 8), int(wb >> 8), int(wa >> 8)}
			for i, c := range [4]int{int(g.R), int(g.G), int(g.B), int(g.A)} {
				if d := c - w[i]; d > channelTolerance || d < -channelTolerance {
					far++
					break
				}
			}
		}
	}
	if limit := b.Dx() * b.Dy() / 200; far > limit {
		dir := t.TempDir()
		diff, _ := gametest.DiffImage(got, want)
		_ = gametest.WritePNG(filepath.Join(dir, name+".got.png"), got)
		_ = gametest.WritePNG(filepath.Join(dir, name+".diff.png"), diff)
		t.Fatalf("%s: %d pixels differ from the reference (limit %d); got and diff written to %s", name, far, limit, dir)
	}
}

// TestPick3DPrefersGeometryToAMarkerAroundTheCamera adds an empty object at the courtyard's origin, whose 16-unit marker box holds the camera and the whole level: a press on the sphere still selects the sphere, and a press on empty sky falls back to the marker.
func TestPick3DPrefersGeometryToAMarkerAroundTheCamera(t *testing.T) {
	ed := openCourtyard(t)
	ed.Doc.Layers[0].Objects = append(ed.Doc.Layers[0].Objects, sceneformat.ObjectDoc{ID: "spawn"})
	ed.Rebuild()
	if _, err := ed.Scene3D(); err != nil {
		t.Fatalf("Scene3D: %v", err)
	}
	spawn, _, _ := ed.FindObject("spawn")
	lo, hi := ed.ObjectAABB3D(spawn)
	eye := ed.Camera3D.Eye()
	for i := range 3 {
		if eye[i] < lo[i] || eye[i] > hi[i] {
			t.Fatalf("the camera at %v is outside the marker box %v..%v, so the test proves nothing", eye, lo, hi)
		}
	}
	x, y := projectPixel(ed, math3d.Vec3{X: 0.9, Y: 0.5, Z: 0.6})
	if id, ok := ed.Pick3DObject(float32(x), float32(y), gpuViewWidth, gpuViewHeight); !ok || id != "ball" {
		t.Fatalf("a press on the sphere picked %q, %v, want the sphere", id, ok)
	}
	if id, ok := ed.Pick3DObject(1, 1, gpuViewWidth, gpuViewHeight); !ok || id != "spawn" {
		t.Fatalf("a press on the sky picked %q, %v, want the marker the camera is in", id, ok)
	}
}

// TestScene3DGenerationOnlyGrows closes the scene between two builds: the second build's generation is still newer than the first's, so a renderer holding the first build's upload uploads the second.
func TestScene3DGenerationOnlyGrows(t *testing.T) {
	ed := openCourtyard(t)
	if _, err := ed.Scene3D(); err != nil {
		t.Fatalf("Scene3D: %v", err)
	}
	gen, doc := ed.s3d.gen, ed.Doc
	ed.Doc = nil
	if built, _ := ed.Scene3D(); built != nil {
		t.Fatal("without a scene Scene3D returned a build")
	}
	ed.Doc = doc
	if _, err := ed.Scene3D(); err != nil {
		t.Fatalf("Scene3D: %v", err)
	}
	if ed.s3d.gen <= gen {
		t.Fatalf("the build after closing the scene has generation %d, want more than %d", ed.s3d.gen, gen)
	}
}

// TestScene3DKeepsItsAssetsWhenTheSceneIsSaved saves the open scene, which writes no model, material file or image, and checks the 3D build is kept; rescanning the project from disk builds it again.
func TestScene3DKeepsItsAssetsWhenTheSceneIsSaved(t *testing.T) {
	ed := openCourtyard(t)
	if _, err := ed.Scene3D(); err != nil {
		t.Fatalf("Scene3D: %v", err)
	}
	gen := ed.s3d.gen
	if ed.FilePath == "" {
		t.Fatal("the courtyard opened without a scene file")
	}
	if err := ed.SaveFile(ed.FilePath); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if _, err := ed.Scene3D(); err != nil || ed.s3d.gen != gen {
		t.Fatalf("saving the scene built it again (generation %d, was %d; %v)", ed.s3d.gen, gen, err)
	}
	if err := ed.RefreshSearch(); err != nil {
		t.Fatalf("RefreshSearch: %v", err)
	}
	if _, err := ed.Scene3D(); err != nil || ed.s3d.gen == gen {
		t.Fatalf("rescanning the project did not build the scene again (generation %d; %v)", ed.s3d.gen, err)
	}
}

// TestViewport3DDoesNotRetryAFailedFrame sets lights the renderer refuses: the frame fails, the same frame is not tried again, and valid lights draw again.
func TestViewport3DDoesNotRetryAFailedFrame(t *testing.T) {
	ed := openCourtyard(t)
	v := newSoftwareViewport(t, ed)
	v.frame()
	bad := DefaultLights3D()
	bad.Directional = make([]render3d.DirectionalLight, render3d.MaxDirectionalLights+1)
	ed.SetLights3D(&bad)
	enc := v.sr.Device().BeginEncoder()
	drew, err := v.r.render(enc, gpuViewWidth, gpuViewHeight)
	if drew || err == nil {
		t.Fatalf("too many lights drew %v with error %v, want an error", drew, err)
	}
	drew, again := v.r.render(enc, gpuViewWidth, gpuViewHeight)
	if drew || again != err {
		t.Fatalf("the same failed frame was tried again: drew %v, error %v (was %v)", drew, again, err)
	}
	v.sr.Device().Submit(enc.Finish())
	v.sr.WaitIdle()
	ed.SetLights3D(nil)
	if drew, _ := v.frame(); !drew {
		t.Fatal("after valid lights the viewport drew nothing")
	}
}

// TestMeshEdges3DJoinsSplitVertices takes the box primitive, which splits each corner into one vertex per face: its wireframe has each of the 12 sides and 6 face diagonals once.
func TestMeshEdges3DJoinsSplitVertices(t *testing.T) {
	ed := openCourtyard(t)
	if err := ed.SetObjectMesh("ball", "box"); err != nil {
		t.Fatalf("SetObjectMesh: %v", err)
	}
	built, err := ed.Scene3D()
	if err != nil {
		t.Fatalf("Scene3D: %v", err)
	}
	for _, d := range built.Drawables {
		if d.Object != "ball" {
			continue
		}
		p := &d.Parts[0]
		if len(p.Mesh.Positions) <= 8 {
			t.Fatalf("the box has %d vertices, want its corners split per face", len(p.Mesh.Positions))
		}
		if edges := ed.s3d.meshEdges3D(p.MeshID, p.Mesh); len(edges) != 18 {
			t.Fatalf("the box has %d wireframe edges, want 18", len(edges))
		}
		return
	}
	t.Fatal("the box draws nothing")
}
