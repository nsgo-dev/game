package scene_test

import (
	"errors"
	"image"
	"image/color"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// overlayMeshWGSL transforms 3D mesh vertices through render3d.CameraWGSL and
// writes solid RGBA color from vertex attributes (u, v, normal.z, 1).
const overlayMeshWGSL = render3d.CameraWGSL + `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) pos: vec4<f32>,
	@location(0) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.pos = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.color = vec4<f32>(in.uv.x, in.uv.y, in.normal.z, 1.0);
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return in.color;
}
`

var overlay3DClearColor = color.RGBA{R: 16, G: 24, B: 40, A: 255}

func overlayQuad3D(center math3d.Vec3, half float32) render3d.Mesh {
	n := math3d.Vec3{Z: 1}
	uv := math3d.Vec2{X: 1, Y: 0} // (u=1, v=0, nz=1) => magenta (255, 0, 255, 255)
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: center.X - half, Y: center.Y - half, Z: center.Z},
			{X: center.X + half, Y: center.Y - half, Z: center.Z},
			{X: center.X + half, Y: center.Y + half, Z: center.Z},
			{X: center.X - half, Y: center.Y + half, Z: center.Z},
		},
		Normals: []math3d.Vec3{n, n, n, n},
		UVs:     []math3d.Vec2{uv, uv, uv, uv},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

func solidRGBAImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func solidSpriteAtlas(name string, w, h int, c color.RGBA) *sprite.Atlas {
	return &sprite.Atlas{
		Image: core.ImageSource{Pixels: solidRGBAImage(w, h, c)},
		Frames: map[string]sprite.Frame{
			name: {Name: name, X: 0, Y: 0, W: float32(w), H: float32(h), Width: float32(w), Height: float32(h)},
		},
	}
}

// compositeCanvasOpsOver3D software-composites the 2D core.DrawOp stream recorded
// by an overlay core.Canvas onto a copy of the 3D GPUReadPixels snapshot using
// Porter-Duff SourceOver alpha blending, verifying end-to-end 2D-over-3D pixels.
func compositeCanvasOpsOver3D(base *image.RGBA, ops []core.DrawOp) *image.RGBA {
	out := image.NewRGBA(base.Bounds())
	copy(out.Pix, base.Pix)

	type state struct {
		tx, ty float32
		sx, sy float32
		alpha  float32
		fill   core.Color
		clip   image.Rectangle
	}
	cur := state{sx: 1, sy: 1, alpha: 1, fill: core.RGB(0x000000), clip: out.Bounds()}
	var stack []state
	var pathRect [4]float32
	hasPathRect := false

	blendOver := func(px, py int, sr, sg, sb, sa uint8) {
		if !image.Pt(px, py).In(cur.clip) || sa == 0 {
			return
		}
		dst := out.RGBAAt(px, py)
		a := float64(sa) / 255.0
		inv := 1.0 - a
		out.SetRGBA(px, py, color.RGBA{
			R: uint8(math.Round(float64(sr)*a + float64(dst.R)*inv)),
			G: uint8(math.Round(float64(sg)*a + float64(dst.G)*inv)),
			B: uint8(math.Round(float64(sb)*a + float64(dst.B)*inv)),
			A: uint8(math.Round(float64(sa) + float64(dst.A)*inv)),
		})
	}

	fillTransformedRect := func(x, y, w, h float32, c color.RGBA, extraAlpha float32) {
		x0 := int(math.Floor(float64(cur.tx + x*cur.sx)))
		y0 := int(math.Floor(float64(cur.ty + y*cur.sy)))
		x1 := int(math.Ceil(float64(cur.tx + (x+w)*cur.sx)))
		y1 := int(math.Ceil(float64(cur.ty + (y+h)*cur.sy)))
		a := uint8(math.Round(float64(c.A) * float64(cur.alpha) * float64(extraAlpha)))
		for py := y0; py < y1; py++ {
			for px := x0; px < x1; px++ {
				blendOver(px, py, c.R, c.G, c.B, a)
			}
		}
	}

	drawTransformedPixels := func(src *image.RGBA, sx0, sy0, sw, sh, dx, dy, dw, dh, extraAlpha float32) {
		if src == nil || dw <= 0 || dh <= 0 {
			return
		}
		if sw <= 0 || sh <= 0 {
			sw = float32(src.Bounds().Dx())
			sh = float32(src.Bounds().Dy())
		}
		x0 := int(math.Floor(float64(cur.tx + dx*cur.sx)))
		y0 := int(math.Floor(float64(cur.ty + dy*cur.sy)))
		x1 := int(math.Ceil(float64(cur.tx + (dx+dw)*cur.sx)))
		y1 := int(math.Ceil(float64(cur.ty + (dy+dh)*cur.sy)))
		for py := y0; py < y1; py++ {
			v := (float32(py) + 0.5 - cur.ty) / cur.sy
			srcY := int(sy0 + (v-dy)/dh*sh)
			if srcY < 0 || srcY >= src.Bounds().Dy() {
				continue
			}
			for px := x0; px < x1; px++ {
				u := (float32(px) + 0.5 - cur.tx) / cur.sx
				srcX := int(sx0 + (u-dx)/dw*sw)
				if srcX < 0 || srcX >= src.Bounds().Dx() {
					continue
				}
				c := src.RGBAAt(srcX, srcY)
				a := uint8(math.Round(float64(c.A) * float64(cur.alpha) * float64(extraAlpha)))
				blendOver(px, py, c.R, c.G, c.B, a)
			}
		}
	}

	unpackColor := func(c core.Color) color.RGBA {
		return color.RGBA{
			R: uint8((c >> 24) & 0xff),
			G: uint8((c >> 16) & 0xff),
			B: uint8((c >> 8) & 0xff),
			A: uint8(c & 0xff),
		}
	}

	for _, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, cur)
		case core.DrawRestore:
			if len(stack) > 0 {
				cur = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case core.DrawTranslate:
			cur.tx += op.X * cur.sx
			cur.ty += op.Y * cur.sy
		case core.DrawScale:
			cur.sx *= op.X
			cur.sy *= op.Y
		case core.DrawAlpha:
			cur.alpha = op.A
		case core.DrawFillColor:
			cur.fill = op.Color
		case core.DrawBeginPath:
			hasPathRect = false
		case core.DrawRect:
			pathRect = [4]float32{op.X, op.Y, op.W, op.H}
			hasPathRect = true
		case core.DrawClip:
			if hasPathRect {
				r := image.Rect(
					int(math.Floor(float64(cur.tx+pathRect[0]*cur.sx))),
					int(math.Floor(float64(cur.ty+pathRect[1]*cur.sy))),
					int(math.Ceil(float64(cur.tx+(pathRect[0]+pathRect[2])*cur.sx))),
					int(math.Ceil(float64(cur.ty+(pathRect[1]+pathRect[3])*cur.sy))),
				)
				cur.clip = cur.clip.Intersect(r)
			}
		case core.DrawFill:
			if hasPathRect {
				fillTransformedRect(pathRect[0], pathRect[1], pathRect[2], pathRect[3], unpackColor(cur.fill), 1)
			}
		case core.DrawImage:
			if op.Image != nil && op.Image.Pixels != nil {
				drawTransformedPixels(op.Image.Pixels, op.X1, op.Y1, op.X2, op.Y2, op.X, op.Y, op.W, op.H, 1)
			}
		case core.DrawAtlas:
			if op.Image != nil && op.Image.Pixels != nil && op.Atlas != nil {
				for _, sp := range *op.Atlas {
					w := float32(sp.Rect.Size.Width) * sp.Transform.SCos
					h := float32(sp.Rect.Size.Height) * sp.Transform.SCos
					drawTransformedPixels(op.Image.Pixels,
						float32(sp.Rect.Origin.X), float32(sp.Rect.Origin.Y), float32(sp.Rect.Size.Width), float32(sp.Rect.Size.Height),
						sp.Transform.TX, sp.Transform.TY, w, h, sp.Alpha)
				}
			}
		}
	}
	return out
}

func wantOverlayPixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA) {
	t.Helper()
	got := img.RGBAAt(x, y)
	near := func(a, b uint8) bool { return max(a, b)-min(a, b) <= 2 }
	if !near(got.R, want.R) || !near(got.G, want.G) || !near(got.B, want.B) || !near(got.A, want.A) {
		t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want)
	}
}

func new3DQuadViewProps(t *testing.T, cx *core.Scope, size int, cam *scene3d.Camera, mesh render3d.Mesh, extra gpu.ViewProps) gpu.ViewProps {
	t.Helper()
	var (
		gpuMesh *render3d.GPUMesh
		camBuf  *render3d.CameraBuffer
		pipe    *gpu.Pipeline
	)
	props := extra
	if props.TestID == "" {
		props.TestID = "viewport"
	}
	if props.Style.Width == (core.Length{}) && props.Style.Height == (core.Length{}) {
		props.Style = core.Style{Width: core.Pt(core.Points(size)), Height: core.Pt(core.Points(size))}
	}
	props.DepthFormat = gpu.FormatDepth24Plus
	props.OnReady = func(dev *gpu.Device) {
		var err error
		gpuMesh, err = render3d.UploadMesh(cx, dev, mesh, render3d.MeshUpload{Label: "quad3d"})
		if err != nil {
			t.Fatalf("UploadMesh: %v", err)
		}
		camBuf, err = render3d.NewCameraBuffer(cx, dev)
		if err != nil {
			t.Fatalf("NewCameraBuffer: %v", err)
		}
		mod, err := gpu.CompileWGSL(overlayMeshWGSL)
		if err != nil {
			t.Fatalf("CompileWGSL: %v", err)
		}
		pipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
			Label:         "overlay3d-mesh",
			Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
			Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
			Layouts:       []*gpu.BindGroupLayout{render3d.CameraGroup: camBuf.Layout()},
			VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
			Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
			DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
			Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
		})
		if err != nil {
			t.Fatalf("CreatePipeline: %v", err)
		}
	}
	props.OnFrame = func(frame *gpu.Frame) {
		camBuf.WriteCamera(cam)
		pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
		pass.ClearColor(core.RGB(0x101828))
		pass.ClearDepth(1)
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		gpuMesh.Draw(pass)
		pass.End()
		frame.Submit()
	}
	return props
}

func TestOverlay3DRenders2DSpritesAndScreenLayersOver3DMesh(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			const size = 64
			cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(60), 1, 0.1, 20)
			mesh := overlayQuad3D(math3d.Vec3{X: 0, Y: 0, Z: 0}, 1.0)

			// 2D pixel atlases for distinct sprite batches and screen layers.
			redAtlas := solidSpriteAtlas("red", 12, 12, color.RGBA{R: 240, G: 20, B: 20, A: 255})
			blueAtlas := solidSpriteAtlas("blue", 12, 12, color.RGBA{R: 20, G: 80, B: 250, A: 255})
			yellowAtlas := solidSpriteAtlas("yellow", 16, 16, color.RGBA{R: 255, G: 255, B: 0, A: 255})

			// Scene has a SpaceWorld layer (must be skipped) and two SpaceScreen layers out of order.
			sc := scene.NewScene(&tilemap.Camera{X: 0, Y: 0, Zoom: 1, Width: size, Height: size})
			worldSkipped := sc.AddLayer(scene.NewLayer("world-2d", scene.SpaceWorld, 1))
			worldSkipped.AddDrawable(scene.NewDrawable(func(d *core.Drawing) {
				d.SetFillColor(core.RGB(0x00ff00))
				d.FillRect(0, 0, size, size)
			}))

			topScreen := sc.AddLayer(scene.NewLayer("top-screen", scene.SpaceScreen, 20))
			topScreen.AddDrawable(scene.NewDrawable(func(d *core.Drawing) {
				d.SetFillColor(core.RGB(0x00dc50))
				d.FillRect(6, 6, 8, 8) // Overlaps lower red sprite at (6..14, 6..14)
			}))

			lowScreen := sc.AddLayer(scene.NewLayer("low-screen", scene.SpaceScreen, 5))
			redSprite := sprite.NewSprite(redAtlas.Frames["red"])
			redSprite.X, redSprite.Y = 4, 4 // Covers (4..16, 4..16) over 3D clear area
			lowScreen.AddBatch(&sprite.Batch{Atlas: redAtlas, Sprites: []*sprite.Sprite{&redSprite}})

			// Additional overlay layer ordered between low-screen (5) and top-screen (20).
			midLayer := scene.NewLayer("mid-overlay", scene.SpaceScreen, 10)
			blueSprite := sprite.NewSprite(blueAtlas.Frames["blue"])
			blueSprite.X, blueSprite.Y = 48, 4 // Top-right corner
			midLayer.AddBatch(&sprite.Batch{Atlas: blueAtlas, Sprites: []*sprite.Sprite{&blueSprite}})

			// Standalone semi-transparent sprite batch with two sprites (exercises DrawAtlas over 3D).
			yellowSprite := sprite.NewSprite(yellowAtlas.Frames["yellow"])
			yellowSprite.X, yellowSprite.Y = 24, 24 // Covers (24..40, 24..40) on top of 3D magenta quad
			yellowSprite.Alpha = 0.5
			yellowCorner := sprite.NewSprite(yellowAtlas.Frames["yellow"])
			yellowCorner.X, yellowCorner.Y = 44, 44 // Bottom-right over 3D clear area
			yellowCorner.Alpha = 0.5
			centerBatch := &sprite.Batch{Atlas: yellowAtlas, Sprites: []*sprite.Sprite{&yellowSprite, &yellowCorner}}

			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, prof.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				ov := scene.NewOverlay3D(cx)
				ov.TestID = "overlay-2d"
				ov.Scene = sc
				ov.AddLayer(midLayer)
				ov.AddBatch(centerBatch)

				props := new3DQuadViewProps(t, cx, size, cam, mesh, gpu.ViewProps{
					Children: []core.Node{
						core.Text{TestID: "hud-text", Content: "HP 100"},
					},
				})
				return ov.View(cx, props)
			}, opts...)

			app.Advance(16 * time.Millisecond)

			gpuView := app.Find("id:viewport")
			overlayCanvas := app.Find("id:overlay-2d")
			hudText := app.Find("id:hud-text")

			if gpuView.Kind != nsgotest.KindGPU {
				t.Fatalf("viewport Kind = %v, want KindGPU", gpuView.Kind)
			}
			if overlayCanvas.Kind != nsgotest.KindCanvas {
				t.Fatalf("overlay Kind = %v, want KindCanvas", overlayCanvas.Kind)
			}
			if overlayCanvas.Parent != gpuView {
				t.Fatal("overlay canvas must be mounted directly inside gpu.View")
			}
			if len(gpuView.Children) != 2 || gpuView.Children[0] != overlayCanvas || gpuView.Children[1] != hudText {
				t.Fatalf("gpu.View child order mismatch: got %d children, want [overlayCanvas, hudText]", len(gpuView.Children))
			}
			if overlayCanvas.Frame != (core.Bounds{Size: core.Size{Width: size, Height: size}}) {
				t.Fatalf("overlay frame = %+v, want full %dx%d view bounds", overlayCanvas.Frame, size, size)
			}
			if overlayCanvas.Style.Background != 0 {
				t.Fatalf("overlay canvas background = %#x, want 0 (transparent)", overlayCanvas.Style.Background)
			}
			if overlayCanvas.Props.PointerEvents != core.PointerEventsNone {
				t.Fatalf("overlay PointerEvents = %v, want PointerEventsNone", overlayCanvas.Props.PointerEvents)
			}
			if len(overlayCanvas.Ops) == 0 {
				t.Fatal("overlay canvas recorded zero draw ops")
			}

			// 1. Verify underlying 3D GPU pixels directly.
			gpuImg, err := app.GPUReadPixels("id:viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			wantOverlayPixel(t, gpuImg, 2, 2, overlay3DClearColor)
			wantOverlayPixel(t, gpuImg, 32, 32, color.RGBA{R: 255, G: 0, B: 255, A: 255})

			// 2. Composite 2D overlay canvas ops over the 3D surface and verify exact pixels.
			composited := compositeCanvasOpsOver3D(gpuImg, overlayCanvas.Ops)

			// Transparent region (2, 2) preserves 3D background clear color (SpaceWorld was skipped).
			wantOverlayPixel(t, composited, 2, 2, overlay3DClearColor)
			// Uncovered part of the 3D mesh (20, 20) preserves 3D magenta quad untouched.
			wantOverlayPixel(t, composited, 20, 20, color.RGBA{R: 255, G: 0, B: 255, A: 255})
			// Low screen layer (Order 5) red sprite outside top-screen rect: opaque red.
			wantOverlayPixel(t, composited, 5, 5, color.RGBA{R: 240, G: 20, B: 20, A: 255})
			// Overlap of low-screen (Order 5) and top-screen (Order 20): green wins over red.
			wantOverlayPixel(t, composited, 10, 10, color.RGBA{R: 0, G: 220, B: 80, A: 255})
			// Mid overlay layer (Order 10) blue sprite at top-right: opaque blue.
			wantOverlayPixel(t, composited, 52, 8, color.RGBA{R: 20, G: 80, B: 250, A: 255})
			// Semi-transparent yellow (255,255,0 @ 0.5) over 3D magenta (255,0,255):
			// R = 255*0.5 + 255*0.5 = 255, G = 255*0.5 + 0 = 128, B = 0 + 255*0.5 = 128.
			wantOverlayPixel(t, composited, 32, 32, color.RGBA{R: 255, G: 128, B: 128, A: 255})
			// Second DrawAtlas sprite (yellow @ 0.5) over 3D clear (16, 24, 40):
			// R = 136, G = 140, B = 20.
			wantOverlayPixel(t, composited, 50, 50, color.RGBA{R: 136, G: 140, B: 20, A: 255})
		})
	}
}

func TestOverlay3DLayerVisibilityAndHiddenToggle(t *testing.T) {
	sc := scene.NewScene(&tilemap.Camera{X: 100, Y: 50, Zoom: 2, Width: 100, Height: 100})
	world := sc.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
	world.AddDrawable(scene.NewDrawable(func(d *core.Drawing) {
		d.FillRect(1, 1, 10, 10)
	}))
	hudA := sc.AddLayer(scene.NewLayer("hud-a", scene.SpaceScreen, 1))
	hudA.AddDrawable(scene.NewDrawable(func(d *core.Drawing) {
		d.SetFillColor(core.RGB(0x112233))
		d.FillRect(2, 2, 10, 10)
	}))
	hudB := sc.AddLayer(scene.NewLayer("hud-b", scene.SpaceScreen, 2))
	hudB.AddDrawable(scene.NewDrawable(func(d *core.Drawing) {
		d.SetFillColor(core.RGB(0x445566))
		d.FillRect(20, 20, 10, 10)
	}))

	// Verify Scene.DrawScreenLayers skips SpaceWorld and keeps Scene.Layers untouched.
	var ov *scene.Overlay3D
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ov = scene.NewOverlay3D(cx)
		ov.TestID = "ov"
		ov.Scene = sc
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "screen-only", Style: core.Style{Width: core.Pt(64), Height: core.Pt(64)}, Draw: func(d *core.Drawing, _ core.Size) {
				sc.DrawScreenLayers(d)
			}},
			core.Canvas{TestID: "expected-screen", Style: core.Style{Width: core.Pt(64), Height: core.Pt(64)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.Save()
				d.SetFillColor(core.RGB(0x112233))
				d.FillRect(2, 2, 10, 10)
				d.Restore()
				d.Save()
				d.SetFillColor(core.RGB(0x445566))
				d.FillRect(20, 20, 10, 10)
				d.Restore()
			}},
			core.View{Style: core.Style{Width: core.Pt(64), Height: core.Pt(64)}, Children: []core.Node{
				ov.Canvas(cx),
			}},
		}}
	})

	sameOps(t, app.Find("screen-only").Ops, app.Find("expected-screen").Ops)
	sameOps(t, app.Find("ov").Ops, app.Find("expected-screen").Ops)

	// Toggle hudA hidden and invalidate: only hudB draws.
	app.Post(func() {
		hudA.Visible = false
		ov.Invalidate()
	})
	app.Settle()

	gotOps := app.Find("ov").Ops
	var fillColors []core.Color
	for _, op := range gotOps {
		if op.Kind == core.DrawFillColor {
			fillColors = append(fillColors, op.Color)
		}
	}
	if !reflect.DeepEqual(fillColors, []core.Color{core.RGB(0x445566)}) {
		t.Fatalf("after hiding hudA, fillColors = %v, want [0x445566]", fillColors)
	}

	// Hide entire Overlay3D: canvas clears to empty ops.
	app.Post(func() {
		ov.SetHidden(true)
	})
	app.Settle()
	if got := len(app.Find("ov").Ops); got != 0 {
		t.Fatalf("hidden Overlay3D recorded %d ops, want 0", got)
	}

	// Unhide Overlay3D: hudB returns.
	app.Post(func() {
		ov.SetHidden(false)
	})
	app.Settle()
	if got := len(app.Find("ov").Ops); got == 0 {
		t.Fatal("unhidden Overlay3D recorded 0 ops")
	}
}

func TestOverlay3DViewportScalingAndTransparentBars(t *testing.T) {
	// 160x120 (4:3) window with 160x90 (16:9) ModeFit virtual viewport => 15pt top/bottom bars.
	const winW, winH = 160, 120
	vp := &screen.Viewport{Width: 160, Height: 90, Mode: screen.ModeFit}

	var ov *scene.Overlay3D
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ov = scene.NewOverlay3D(cx)
		ov.TestID = "vp-ov"
		ov.Viewport = vp
		ov.OnDraw = func(d *core.Drawing, size core.Size) {
			if size != (core.Size{Width: 160, Height: 90}) {
				t.Errorf("OnDraw virtual size = %+v, want 160x90", size)
			}
			d.SetFillColor(core.RGB(0xff8800))
			d.FillRect(10, 10, 20, 10)
		}
		return core.View{Style: core.Style{Width: core.Pt(winW), Height: core.Pt(winH)}, Children: []core.Node{
			ov.Canvas(cx),
		}}
	}, nsgotest.Size(winW, winH))

	// With zero BarColor, overlay treats bars as transparent so no opaque black bar FillRects are recorded.
	ops := app.Find("vp-ov").Ops
	var fillCount int
	var hasTranslate15 bool
	for _, op := range ops {
		if op.Kind == core.DrawFill {
			fillCount++
		}
		if op.Kind == core.DrawTranslate && math.Abs(float64(op.Y-15)) < 1e-3 {
			hasTranslate15 = true
		}
	}
	if fillCount != 1 {
		t.Fatalf("transparent-bar viewport recorded %d fills, want 1 (only OnDraw rect, no opaque letterbox bars)", fillCount)
	}
	if vp.BarColor != 0 {
		t.Fatalf("Overlay3D.Draw mutated caller Viewport.BarColor to %#x", vp.BarColor)
	}
	if !hasTranslate15 {
		t.Fatalf("expected viewport translate Y=15 in ops: %+v", ops)
	}

	// Explicitly setting BarColor draws the 2 letterbox bars plus the OnDraw rect (3 fills total).
	app.Post(func() {
		vp.BarColor = core.RGB(0x000000)
		ov.Invalidate()
	})
	app.Settle()

	fillCount = 0
	for _, op := range app.Find("vp-ov").Ops {
		if op.Kind == core.DrawFill {
			fillCount++
		}
	}
	if fillCount != 3 {
		t.Fatalf("explicit BarColor viewport recorded %d fills, want 3 (2 bars + 1 rect)", fillCount)
	}

	// Verify Overlay3D.Project converts 3D world points into virtual coordinates.
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 5}, math3d.DegToRad(60), float32(winW)/float32(winH), 0.1, 50)
	win := core.Size{Width: winW, Height: winH}

	// Center of 3D view (0, 0, 0) maps to window center (80, 60) => virtual center (80, 45).
	gx, gy, vis := ov.Project(cam, math3d.Vec3{X: 0, Y: 0, Z: 0}, win)
	if !vis || math.Abs(float64(gx-80)) > 1e-2 || math.Abs(float64(gy-45)) > 1e-2 {
		t.Fatalf("Project(origin) = (%v, %v, %v), want (80, 45, true)", gx, gy, vis)
	}

	// Point high enough to land in the top letterbox margin (view Y < 15) is clipped => visible == false.
	// At Z=0, distance=5, halfH = 5*tan(30 deg) ≈ 2.88675; Y=2.6 projects to ndcY ≈ 0.9 => viewY ≈ 6 < 15.
	if _, _, vis := ov.Project(cam, math3d.Vec3{X: 0, Y: 2.6, Z: 0}, win); vis {
		t.Fatal("Project into top letterbox bar reported visible == true, want false")
	}
}

func TestOverlay3DProjectToOverlayAndMarkerTracking(t *testing.T) {
	const size = 64
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(60), 1, 0.1, 20)
	target := math3d.Vec3{X: 0.6, Y: 0.3, Z: 0}
	mesh := overlayQuad3D(target, 0.35)

	px, py, visible := scene.ProjectToOverlay(cam, target, size, size)
	if !visible {
		t.Fatalf("ProjectToOverlay(%v) = (%v, %v, false), want visible", target, px, py)
	}

	// Place a 6x6 cyan marker sprite centered at the projected 2D overlay position.
	markerAtlas := solidSpriteAtlas("marker", 6, 6, color.RGBA{R: 0, G: 255, B: 255, A: 255})
	marker := sprite.NewSprite(markerAtlas.Frames["marker"])
	marker.OriginX, marker.OriginY = 3, 3
	marker.X, marker.Y = px, py
	markerBatch := &sprite.Batch{Atlas: markerAtlas, Sprites: []*sprite.Sprite{&marker}}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ov := scene.NewOverlay3D(cx)
		ov.TestID = "marker-ov"
		ov.AddBatch(markerBatch)
		return ov.View(cx, new3DQuadViewProps(t, cx, size, cam, mesh, gpu.ViewProps{}))
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)

	// Verify the 3D GPU pixel at (px, py) IS the 3D magenta quad!
	gpuImg, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	wantOverlayPixel(t, gpuImg, int(math.Round(float64(px))), int(math.Round(float64(py))), color.RGBA{R: 255, G: 0, B: 255, A: 255})

	// And compositing the 2D marker sprite places cyan directly over (px, py)!
	composited := compositeCanvasOpsOver3D(gpuImg, app.Find("id:marker-ov").Ops)
	wantOverlayPixel(t, composited, int(math.Round(float64(px))), int(math.Round(float64(py))), color.RGBA{R: 0, G: 255, B: 255, A: 255})

	// Verify standalone gpu.NewSoftwareRenderer matches the exact same projection!
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer camBuf.Close()
	gm, err := render3d.UploadMesh(nil, dev, mesh, render3d.MeshUpload{Label: "sw"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	defer gm.Close()
	mod, err := gpu.CompileWGSL(overlayMeshWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	pipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:         "sw-pipe",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{render3d.CameraGroup: camBuf.Layout()},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
		Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	defer pipe.Close()
	camBuf.WriteCamera(cam)
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x101828)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreStore, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	gm.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	wantOverlayPixel(t, sr.Image(), int(math.Round(float64(px))), int(math.Round(float64(py))), color.RGBA{R: 255, G: 0, B: 255, A: 255})

	// Edge cases: orthographic camera, behind camera, outside frustum, nil/invalid inputs.
	ortho := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 2, 0.1, 20)
	ox, oy, ovis := scene.ProjectToOverlay(ortho, math3d.Vec3{X: 2, Y: 1, Z: 0}, 200, 100)
	// OrthoHeight=4, Aspect=2 => halfW=4, halfH=2 => (2, 1) -> ndc=(0.5, 0.5) -> (150, 25).
	if !ovis || math.Abs(float64(ox-150)) > 1e-2 || math.Abs(float64(oy-25)) > 1e-2 {
		t.Fatalf("ortho ProjectToOverlay = (%v, %v, %v), want (150, 25, true)", ox, oy, ovis)
	}
	if _, _, vis := scene.ProjectToOverlay(cam, math3d.Vec3{Z: 10}, size, size); vis {
		t.Fatal("point behind camera reported visible == true")
	}
	offX, _, offVis := scene.ProjectToOverlay(cam, math3d.Vec3{X: 50, Y: 0, Z: 0}, size, size)
	if offVis || offX <= size {
		t.Fatalf("off-screen right point = (%v, vis=%v), want x > %d and vis=false", offX, offVis, size)
	}
	if _, _, vis := scene.ProjectToOverlay(nil, target, size, size); vis {
		t.Fatal("nil camera reported visible == true")
	}
	if _, _, vis := scene.ProjectToOverlay(cam, target, 0, size); vis {
		t.Fatal("zero width reported visible == true")
	}
	if _, _, vis := scene.ProjectToOverlay(cam, math3d.Vec3{X: float32(math.NaN())}, size, size); vis {
		t.Fatal("NaN worldPos reported visible == true")
	}
}

func TestOverlay3DPointerRoutingAndHitTesting(t *testing.T) {
	const size = 100

	// 1. Default non-interactive Overlay3D lets presses pass through directly to gpu.View.OnPress.
	var gpuPressCount int
	var gpuPressPos core.Point
	appPassThrough := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ov := scene.NewOverlay3D(cx)
		ov.TestID = "passthrough-ov"
		ov.OnDraw = func(d *core.Drawing, _ core.Size) {
			d.FillRect(0, 0, 20, 20)
		}
		return ov.View(cx, gpu.ViewProps{
			TestID: "viewport",
			Style:  core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			OnPress: func(ev core.PressEvent) {
				gpuPressCount++
				gpuPressPos = ev.Position
			},
		})
	}, nsgotest.Size(size, size))

	if got := appPassThrough.Find("id:passthrough-ov").Props.PointerEvents; got != core.PointerEventsNone {
		t.Fatalf("passthrough PointerEvents = %v, want PointerEventsNone", got)
	}
	appPassThrough.PressAt("id:viewport", core.Point{X: 10, Y: 10})
	if gpuPressCount != 1 || gpuPressPos != (core.Point{X: 10, Y: 10}) {
		t.Fatalf("gpu.View OnPress = count %d pos %+v, want count 1 pos (10, 10)", gpuPressCount, gpuPressPos)
	}

	// 2. Interactive Overlay3D receives presses and dispatches to screen layers (with Viewport scaling) & standalone batches.
	sc := scene.NewScene(nil)
	var updateLog []string
	var pressedDrawable, pressedSprite, touchedDrawable bool

	layer := sc.AddLayer(scene.NewLayer("buttons", scene.SpaceScreen, 10))
	btn := scene.NewDrawable(func(d *core.Drawing) {
		d.FillRect(10, 10, 30, 20)
	})
	btn.Hit = scene.RectHit(core.Bounds{Origin: core.Point{X: 10, Y: 10}, Size: core.Size{Width: 30, Height: 20}})
	btn.OnPress = func(hit scene.HitResult, _ core.PressEvent) {
		pressedDrawable = true
		if hit.Layer != layer || hit.ViewPoint != (core.Point{X: 40, Y: 30}) {
			t.Errorf("drawable OnPress hit = %+v", hit)
		}
	}
	btn.OnTouch = func(_ scene.HitResult, _ core.TouchEvent) {
		touchedDrawable = true
	}
	btn.Update = func(dt time.Duration) {
		updateLog = append(updateLog, "btn:"+dt.String())
	}
	layer.AddDrawable(btn)

	badgeAtlas := solidSpriteAtlas("badge", 10, 10, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	badge := sprite.NewSprite(badgeAtlas.Frames["badge"])
	badge.X, badge.Y = 20, 15 // Overlaps btn in virtual space (20..30, 15..25)
	sc.SetSpriteHit(&badge, scene.HitOptions{
		OnPress: func(hit scene.HitResult, _ core.PressEvent) {
			pressedSprite = true
			if hit.Sprite != &badge || hit.Space != scene.SpaceScreen {
				t.Errorf("standalone batch sprite hit = %+v", hit)
			}
		},
	})
	topBatch := &sprite.Batch{Atlas: badgeAtlas, Sprites: []*sprite.Sprite{&badge}}

	// Virtual 50x50 scaled 2x into 100x100 window.
	vp := &screen.Viewport{Width: 50, Height: 50, Mode: screen.ModeFit}
	var interactiveOv *scene.Overlay3D
	var gpuBlockedPresses int
	appInteractive := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		interactiveOv = scene.NewOverlay3D(cx)
		interactiveOv.TestID = "interactive-ov"
		interactiveOv.Scene = sc
		interactiveOv.AddBatch(topBatch)
		interactiveOv.Viewport = vp
		interactiveOv.Interactive = true
		return interactiveOv.View(cx, gpu.ViewProps{
			TestID: "viewport",
			Style:  core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			OnPress: func(core.PressEvent) {
				gpuBlockedPresses++
			},
		})
	}, nsgotest.Size(size, size))

	if got := appInteractive.Find("id:interactive-ov").Props.PointerEvents; got != core.PointerEventsAuto {
		t.Fatalf("interactive PointerEvents = %v, want PointerEventsAuto", got)
	}

	// View (40, 30) => virtual (20, 15) hits BOTH topBatch badge (topmost) and btn (underneath).
	win := core.Size{Width: size, Height: size}
	allHits := interactiveOv.HitTestAll(win, core.Point{X: 40, Y: 30})
	if len(allHits) != 2 || allHits[0].Sprite != &badge || allHits[1].Drawable != btn {
		t.Fatalf("HitTestAll = %+v, want [badge, btn]", allHits)
	}

	// Pressing at (40, 30) hits topmost standalone batch sprite.
	appInteractive.PressAt("id:viewport", core.Point{X: 40, Y: 30})
	if !pressedSprite || pressedDrawable || gpuBlockedPresses != 0 {
		t.Fatalf("press at overlap: sprite=%v drawable=%v gpu=%d, want true, false, 0", pressedSprite, pressedDrawable, gpuBlockedPresses)
	}

	// Pressing at (24, 24) => virtual (12, 12) hits btn only.
	btn.OnPress = func(hit scene.HitResult, _ core.PressEvent) {
		pressedDrawable = true
		if hit.Point != (core.Point{X: 12, Y: 12}) {
			t.Errorf("virtual hit point = %+v, want (12, 12)", hit.Point)
		}
	}
	appInteractive.PressAt("id:viewport", core.Point{X: 24, Y: 24})
	if !pressedDrawable {
		t.Fatal("expected press at (24, 24) to dispatch to btn drawable")
	}

	// DispatchTouch hits btn via virtual coordinates too.
	if !interactiveOv.DispatchTouch(win, core.TouchEvent{
		Phase: core.TouchStart,
		Touches: []core.Touch{{
			ID:       1,
			Position: core.Point{X: 24, Y: 24},
			Changed:  true,
		}},
	}) || !touchedDrawable {
		t.Fatal("expected DispatchTouch to invoke btn.OnTouch")
	}

	// Overlay3D.Update updates custom drawables in layer order.
	interactiveOv.Update(16 * time.Millisecond)
	if !reflect.DeepEqual(updateLog, []string{"btn:16ms"}) {
		t.Fatalf("Update log = %v, want [btn:16ms]", updateLog)
	}
}

func TestOverlay3DNoGPUFallback(t *testing.T) {
	var gotErr gpu.Error
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ov := scene.NewOverlay3D(cx)
		ov.TestID = "fallback-ov"
		ov.OnDraw = func(d *core.Drawing, _ core.Size) {
			d.FillRect(1, 2, 10, 10)
		}
		return ov.View(cx, gpu.ViewProps{
			TestID:   "viewport",
			Style:    core.Style{Width: core.Pt(80), Height: core.Pt(60)},
			Children: []core.Node{core.Text{Content: "Fallback HUD"}},
			OnError: func(err gpu.Error) {
				gotErr = err
			},
		})
	}, nsgotest.Size(80, 60), nsgotest.NoGPU())

	if !errors.Is(gotErr, gpu.ErrUnsupported) {
		t.Fatalf("NoGPU OnError = %v, want gpu.ErrUnsupported", gotErr)
	}
	app.Find("text:Fallback HUD")
	if len(app.Find("id:fallback-ov").Ops) == 0 {
		t.Fatal("2D overlay canvas did not draw under NoGPU fallback")
	}
}

func TestOverlay3DStructLiteralCustomOnPressAndNilSafety(t *testing.T) {
	// Struct-literal Overlay3D (without NewOverlay3D) binds scope on mount and supports custom OnPress.
	var customPresses int
	var colorVal = core.RGB(0x112233)
	ov := &scene.Overlay3D{
		TestID: "literal-ov",
		OnDraw: func(d *core.Drawing, _ core.Size) {
			d.SetFillColor(colorVal)
			d.FillRect(0, 0, 10, 10)
		},
		OnPress: func(core.PressEvent) {
			customPresses++
		},
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return ov.View(cx, gpu.ViewProps{
			TestID: "viewport",
			Style:  core.Style{Width: core.Pt(40), Height: core.Pt(40)},
		})
	}, nsgotest.Size(40, 40))

	if got := app.Find("id:literal-ov").Props.PointerEvents; got != core.PointerEventsAuto {
		t.Fatalf("OnPress Overlay3D PointerEvents = %v, want PointerEventsAuto", got)
	}
	app.PressAt("id:viewport", core.Point{X: 5, Y: 5})
	if customPresses != 1 {
		t.Fatalf("custom OnPress count = %d, want 1", customPresses)
	}

	app.Post(func() {
		colorVal = core.RGB(0x445566)
		ov.Invalidate()
	})
	app.Settle()

	var sawUpdated bool
	for _, op := range app.Find("id:literal-ov").Ops {
		if op.Kind == core.DrawFillColor && op.Color == core.RGB(0x445566) {
			sawUpdated = true
		}
	}
	if !sawUpdated {
		t.Fatal("struct-literal Overlay3D.Invalidate did not trigger redraw")
	}

	// Nil receiver safety across all methods.
	var nilOv *scene.Overlay3D
	var nilScene *scene.Scene
	nilScene.DrawScreenLayers(nil)
	if nilOv.AddLayer(scene.NewLayer("x", scene.SpaceScreen, 0)) != nil {
		t.Fatal("nil Overlay3D.AddLayer returned non-nil")
	}
	nilOv.AddBatch(nil)
	nilOv.Invalidate()
	nilOv.SetHidden(true)
	nilOv.Update(16 * time.Millisecond)
	nilOv.Draw(nil, core.Size{Width: 10, Height: 10})
	if _, _, ok := nilOv.Project(nil, math3d.Vec3{}, core.Size{Width: 10, Height: 10}); ok {
		t.Fatal("nil Overlay3D.Project returned visible == true")
	}
	if _, ok := nilOv.HitTest(core.Size{Width: 10, Height: 10}, core.Point{}); ok {
		t.Fatal("nil Overlay3D.HitTest returned ok == true")
	}
	if len(nilOv.HitTestAll(core.Size{Width: 10, Height: 10}, core.Point{})) != 0 {
		t.Fatal("nil Overlay3D.HitTestAll returned non-empty")
	}
	if nilOv.DispatchPress(core.Size{Width: 10, Height: 10}, core.PressEvent{}) {
		t.Fatal("nil Overlay3D.DispatchPress returned true")
	}
	if nilOv.DispatchTouch(core.Size{Width: 10, Height: 10}, core.TouchEvent{}) {
		t.Fatal("nil Overlay3D.DispatchTouch returned true")
	}
}
