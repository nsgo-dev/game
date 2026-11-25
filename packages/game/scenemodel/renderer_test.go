package scenemodel_test

import (
	"image"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

const (
	renderWidth  = 192
	renderHeight = 144
	clearHex     = 0x10141c
)

// levelLights is a dim blue ambient fill and a warm key light from the upper right.
var levelLights = render3d.LightRig{
	Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.25, Y: 0.28, Z: 0.35}, Intensity: 1},
	Directional: []render3d.DirectionalLight{{
		Direction: math3d.Vec3{X: -0.45, Y: -0.8, Z: -0.4},
		Color:     math3d.Vec3{X: 1, Y: 0.95, Z: 0.85},
		Intensity: 3,
	}},
}

// levelCamera looks down at the middle of the level from the front right.
func levelCamera() *scene3d.Camera {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 2.6, Y: 3.1, Z: 4.6}, math3d.DegToRad(45), float32(renderWidth)/renderHeight, 0.1, 40)
	cam.LookAt(math3d.Vec3{Y: 0.4}, math3d.Vec3Up())
	return cam
}

// renderLevel draws built with a Renderer on a software GPU and returns the image.
func renderLevel(t *testing.T, built *scenemodel.Built) (*image.RGBA, render3d.RenderStats) {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(renderWidth, renderHeight, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	r, err := scenemodel.NewRenderer(nil, dev, built, scenemodel.RendererOptions{Label: "level"})
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	stats, err := r.Prepare(levelCamera(), levelLights)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "level",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(clearHex)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	r.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	src := sr.Image()
	img := image.NewRGBA(src.Bounds())
	copy(img.Pix, src.Pix)
	return img, stats
}

// projectPoint returns the pixel the world point p lands on.
func projectPoint(cam *scene3d.Camera, p math3d.Vec3) (int, int) {
	clip := cam.ViewProj().MulVec4(p.Vec4(1))
	x := (clip.X/clip.W + 1) / 2 * renderWidth
	y := (1 - clip.Y/clip.W) / 2 * renderHeight
	return int(x), int(y)
}

// TestLevelRendersLitAndTextured draws the test project's level, a textured GLB crate with its brass lid, a second lid from the same model, a stone floor, a teal sphere and a translucent glass capsule, and compares it with testdata/golden/level.png. UPDATE_GOLDEN=1 rewrites the reference.
func TestLevelRendersLitAndTextured(t *testing.T) {
	_, built := buildLevel(t)
	img, stats := renderLevel(t, built)
	assertReference(t, gametest.GoldenPath("", "level"), img)

	if stats.TotalItems != 6 || stats.VisibleItems != 6 || stats.TransparentDraws != 1 {
		t.Errorf("stats total %d visible %d transparent %d, want all 6 parts visible and the glass blended", stats.TotalItems, stats.VisibleItems, stats.TransparentDraws)
	}
	cam := levelCamera()
	clear := core.RGB(clearHex)
	cr, cg, cb, _ := clear.Components()
	isClear := func(x, y int) bool {
		c := img.RGBAAt(x, y)
		return c.R == uint8(cr*255+0.5) && c.G == uint8(cg*255+0.5) && c.B == uint8(cb*255+0.5)
	}
	if !isClear(0, 0) || !isClear(renderWidth-1, 0) {
		t.Error("the top corners should show the clear color above the floor")
	}

	// The crate's front face shows both colors of its checkerboard texture.
	crate := built.Objects["crate"].Children()[0]
	world := crate.WorldMatrix()
	light, dark := 0, 0
	for _, u := range []float32{-0.35, -0.15, 0.05, 0.25} {
		for _, v := range []float32{-0.35, -0.15, 0.05, 0.25} {
			x, y := projectPoint(cam, world.TransformPoint(math3d.Vec3{X: u, Y: v, Z: 0.5}))
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

	// The sphere is lit: its side toward the key light is brighter than the side away from it.
	ball := built.Objects["ball"].WorldTransform().Pos
	lx, ly := projectPoint(cam, ball.Add(math3d.Vec3{X: 0.3, Y: 0.3, Z: 0.15}))
	sx, sy := projectPoint(cam, ball.Add(math3d.Vec3{X: -0.3, Y: -0.3, Z: 0.15}))
	lit, shade := img.RGBAAt(lx, ly), img.RGBAAt(sx, sy)
	if int(lit.G)+int(lit.B) <= int(shade.G)+int(shade.B)+30 {
		t.Errorf("ball lit side %v is not brighter than its shaded side %v", lit, shade)
	}
	if lit.G <= lit.R {
		t.Errorf("ball %v should be teal", lit)
	}

	// Hiding the crate removes it from the render.
	built.Objects["crate"].SetVisible(false)
	hidden, _ := renderLevel(t, built)
	x, y := projectPoint(cam, world.TransformPoint(math3d.Vec3{Z: 0.5}))
	if hidden.RGBAAt(x, y) == img.RGBAAt(x, y) {
		t.Error("hiding the crate's object left the crate drawn")
	}
}

func TestRendererRejectsBadInput(t *testing.T) {
	if _, err := scenemodel.NewRenderer(nil, nil, &scenemodel.Built{}, scenemodel.RendererOptions{}); err == nil {
		t.Error("NewRenderer accepted a nil device")
	}
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	if _, err := scenemodel.NewRenderer(nil, sr.Device(), nil, scenemodel.RendererOptions{}); err == nil {
		t.Error("NewRenderer accepted a nil Built")
	}
	_, built := buildLevel(t)
	r, err := scenemodel.NewRenderer(nil, sr.Device(), built, scenemodel.RendererOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	bad := levelLights
	bad.Ambient.Intensity = -1
	if _, err := r.Prepare(levelCamera(), bad); err == nil || !strings.Contains(err.Error(), "lights") {
		t.Errorf("Prepare with invalid lights = %v", err)
	}
	if _, err := r.Prepare(nil, levelLights); err == nil {
		t.Error("Prepare accepted a nil camera")
	}
}

// assertReference compares got with the reference PNG at path, allowing a few edge pixels to differ by rounding between CPU architectures (fused multiply-add on arm64), and writes got and a diff image on mismatch. UPDATE_GOLDEN=1 rewrites the reference.
func assertReference(t *testing.T, path string, got *image.RGBA) {
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
