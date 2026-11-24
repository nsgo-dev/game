package render3d_test

import (
	"encoding/binary"
	"errors"
	"image"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	post3dWidth  = 128
	post3dHeight = 96
)

// post3dSceneWGSL renders unlit/HDR vertex-colored 3D geometry (rgb may exceed 1.0 for HDR emissives).
const post3dSceneWGSL = render3d.CameraWGSL + `
struct Post3DVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) color: vec4<f32>,
}

struct Post3DVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) normal: vec3<f32>,
	@location(1) color: vec4<f32>,
}

@vertex
fn vs_main(in: Post3DVertexIn) -> Post3DVertexOut {
	var out: Post3DVertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.normal = in.normal;
	out.color = in.color;
	return out;
}

@fragment
fn fs_main(in: Post3DVertexOut) -> @location(0) vec4<f32> {
	// color.a encodes shading mode: 1.0 = unlit/emissive (preserves HDR > 1.0), 0.5 = subtle directional shade.
	if (in.color.a > 0.75) {
		return vec4<f32>(in.color.rgb, 1.0);
	}
	let l = normalize(vec3<f32>(0.35, 0.85, 0.4));
	let k = 0.65 + 0.35 * max(dot(normalize(in.normal), l), 0.0);
	return vec4<f32>(in.color.rgb * k, 1.0);
}
`

func post3dAppendVert(dst []float32, p, n math3d.Vec3, col [3]float32, mode float32) []float32 {
	return append(dst, p.X, p.Y, p.Z, n.X, n.Y, n.Z, col[0], col[1], col[2], mode)
}

func post3dAppendQuad(dst []float32, a, b, c, d, n math3d.Vec3, col [3]float32, mode float32) []float32 {
	for _, p := range [...]math3d.Vec3{a, b, c, a, c, d} {
		dst = post3dAppendVert(dst, p, n, col, mode)
	}
	return dst
}

func post3dAppendBox(dst []float32, center, half math3d.Vec3, col [3]float32, mode float32) []float32 {
	type face struct {
		n, u, v    math3d.Vec3
		hn, hu, hv float32
	}
	faces := [...]face{
		{math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3{Y: 1}, half.X, half.Z, half.Y},
		{math3d.Vec3{X: -1}, math3d.Vec3{Z: 1}, math3d.Vec3{Y: 1}, half.X, half.Z, half.Y},
		{math3d.Vec3{Y: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, half.Y, half.X, half.Z},
		{math3d.Vec3{Y: -1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: 1}, half.Y, half.X, half.Z},
		{math3d.Vec3{Z: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Y: 1}, half.Z, half.X, half.Y},
		{math3d.Vec3{Z: -1}, math3d.Vec3{X: -1}, math3d.Vec3{Y: 1}, half.Z, half.X, half.Y},
	}
	for _, f := range faces {
		c := center.Add(f.n.Scale(f.hn))
		u, v := f.u.Scale(f.hu), f.v.Scale(f.hv)
		dst = post3dAppendQuad(dst,
			c.Sub(u).Sub(v), c.Add(u).Sub(v), c.Add(u).Add(v), c.Sub(u).Add(v),
			f.n, col, mode)
	}
	return dst
}

// post3dAppendCheckerPanel appends a front-facing (+Z) grid of alternating high-contrast tiles,
// ideal for measuring spatial sharpness vs Depth of Field blur.
func post3dAppendCheckerPanel(dst []float32, center math3d.Vec3, width, height float32, cols, rows int, colA, colB [3]float32) []float32 {
	n := math3d.Vec3{Z: 1}
	dx := width / float32(cols)
	dy := height / float32(rows)
	x0 := center.X - width*0.5
	y0 := center.Y - height*0.5
	for r := range rows {
		for c := range cols {
			col := colA
			if (r+c)%2 != 0 {
				col = colB
			}
			xa := x0 + float32(c)*dx
			xb := xa + dx
			ya := y0 + float32(r)*dy
			yb := ya + dy
			dst = post3dAppendQuad(dst,
				math3d.Vec3{X: xa, Y: ya, Z: center.Z},
				math3d.Vec3{X: xb, Y: ya, Z: center.Z},
				math3d.Vec3{X: xb, Y: yb, Z: center.Z},
				math3d.Vec3{X: xa, Y: yb, Z: center.Z},
				n, col, 1.0)
		}
	}
	return dst
}

func post3dPackFloats(fs []float32) []byte {
	out := make([]byte, len(fs)*4)
	for i, f := range fs {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(f))
	}
	return out
}

type post3dScenePipeline struct {
	cam  *render3d.CameraBuffer
	pipe *gpu.Pipeline
}

func newPost3dScenePipeline(t *testing.T, cx *core.Scope, dev *gpu.Device, colorFmt, depthFmt gpu.TextureFormat) *post3dScenePipeline {
	t.Helper()
	cam, err := render3d.NewCameraBuffer(cx, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	mod, err := gpu.CompileWGSL(post3dSceneWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	pipe, err := dev.CreatePipeline(cx, gpu.PipelineDesc{
		Label:    "post3d.test.scene",
		Vertex:   gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment: gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:  []*gpu.BindGroupLayout{render3d.CameraGroup: cam.Layout()},
		VertexBuffers: []gpu.VertexBufferLayout{{
			Stride: 10 * 4,
			Attributes: []gpu.VertexAttribute{
				{Location: 0, Offset: 0, Format: gpu.VertexFloat32x3},
				{Location: 1, Offset: 3 * 4, Format: gpu.VertexFloat32x3},
				{Location: 2, Offset: 6 * 4, Format: gpu.VertexFloat32x4},
			},
		}},
		Primitive: gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
		DepthStencil: &gpu.DepthStencilState{
			Format:       depthFmt,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		},
		Targets: []gpu.ColorTargetState{{Format: colorFmt, WriteMask: gpu.ColorWriteAll}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline scene: %v", err)
	}
	return &post3dScenePipeline{cam: cam, pipe: pipe}
}

func (sp *post3dScenePipeline) draw(pass *gpu.RenderPass, vb *gpu.Buffer, vertCount uint32) {
	pass.SetPipeline(sp.pipe)
	pass.SetBindGroup(render3d.CameraGroup, sp.cam.BindGroup())
	pass.SetVertexBuffer(0, vb, 0)
	pass.Draw(vertCount, 0)
}

func post3dAssertGolden(t *testing.T, name string, got *image.RGBA) {
	t.Helper()
	path := gametest.GoldenPath("", name)
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
		_ = gametest.WritePNG(dir+"/"+name+".got.png", got)
		_ = gametest.WritePNG(dir+"/"+name+".diff.png", diff)
		t.Fatalf("%s: %d pixels differ from reference (limit %d); got and diff written to %s", name, far, limit, dir)
	}
}

func post3dLuma(img *image.RGBA, x, y int) float64 {
	c := img.RGBAAt(x, y)
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255.0
}

func post3dRegionLumaMean(img *image.RGBA, x0, y0, x1, y1 int) float64 {
	var sum float64
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			sum += post3dLuma(img, x, y)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// post3dRegionContrast measures mean horizontal + vertical adjacent-pixel luminance gradient
// inside [x0, x1) x [y0, y1), which drops cleanly when a checkerboard blurs.
func post3dRegionContrast(img *image.RGBA, x0, y0, x1, y1 int) float64 {
	var sum float64
	n := 0
	for y := y0; y < y1-1; y++ {
		for x := x0; x < x1-1; x++ {
			c := post3dLuma(img, x, y)
			r := post3dLuma(img, x+1, y)
			d := post3dLuma(img, x, y+1)
			sum += math.Abs(c-r) + math.Abs(c-d)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func post3dMaxChannelDiff(a, b *image.RGBA) int {
	maxD := 0
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ca := a.RGBAAt(x, y)
			cb := b.RGBAAt(x, y)
			for _, d := range [4]int{
				int(ca.R) - int(cb.R),
				int(ca.G) - int(cb.G),
				int(ca.B) - int(cb.B),
				int(ca.A) - int(cb.A),
			} {
				if d < 0 {
					d = -d
				}
				if d > maxD {
					maxD = d
				}
			}
		}
	}
	return maxD
}

func TestPost3DBloomMatchesReference(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(post3dWidth, post3dHeight, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()

	sceneRT, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
		Label:       "bloom.scene",
		Width:       post3dWidth,
		Height:      post3dHeight,
		ColorFormat: gpu.FormatRGBA16Float,
		DepthFormat: gpu.FormatDepth32Float,
	})
	if err != nil {
		t.Fatalf("CreateRenderTarget: %v", err)
	}
	defer func() { _ = sceneRT.Close() }()

	sp := newPost3dScenePipeline(t, nil, dev, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)
	defer func() { _ = sp.cam.Close() }()

	// Scene: one bright HDR emissive box on the left, one muted box on the right, against a dark background.
	var verts []float32
	verts = post3dAppendBox(verts, math3d.Vec3{X: -1.1, Y: 0.0, Z: 0.0}, math3d.Vec3{X: 0.45, Y: 0.45, Z: 0.45}, [3]float32{2.4, 1.7, 0.5}, 1.0)
	verts = post3dAppendBox(verts, math3d.Vec3{X: 1.2, Y: 0.0, Z: 0.0}, math3d.Vec3{X: 0.45, Y: 0.45, Z: 0.45}, [3]float32{0.2, 0.3, 0.45}, 1.0)
	vb, err := dev.CreateBuffer(nil, gpu.BufferDesc{Label: "bloom.verts", Usage: gpu.UsageVertex, Data: post3dPackFloats(verts)})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	defer func() { _ = vb.Close() }()

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 5.0}, math3d.DegToRad(45), float32(post3dWidth)/float32(post3dHeight), 0.1, 50)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

	bloom, err := render3d.NewBloomPass(nil, dev, render3d.BloomPassOptions{
		Width:        post3dWidth,
		Height:       post3dHeight,
		ColorFormat:  gpu.FormatRGBA16Float,
		OutputFormat: gpu.FormatRGBA8Unorm,
		Config: render3d.BloomConfig{
			Threshold:  0.85,
			SoftKnee:   0.35,
			Intensity:  0.9,
			Radius:     1.6,
			BlurPasses: 2,
			HalfRes:    true,
		},
	})
	if err != nil {
		t.Fatalf("NewBloomPass: %v", err)
	}
	defer func() { _ = bloom.Close() }()

	renderFrame := func(cfg render3d.BloomConfig) *image.RGBA {
		if err := bloom.SetConfig(cfg); err != nil {
			t.Fatalf("SetConfig: %v", err)
		}
		sp.cam.WriteCamera(cam)
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				Texture:    sceneRT.Color,
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x06080d),
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				Texture:      sceneRT.Depth,
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreStore,
				ClearDepth:   1.0,
			},
		})
		sp.draw(pass, vb, uint32(len(verts)/10))
		pass.End()
		if err := bloom.Apply(dev, enc, sceneRT.Color, nil); err != nil {
			t.Fatalf("BloomPass.Apply: %v", err)
		}
		dev.Submit(enc.Finish())
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return sr.Image()
	}

	bloomImg := renderFrame(bloom.Config())
	post3dAssertGolden(t, "post3d_bloom", bloomImg)

	noIntensity := bloom.Config()
	noIntensity.Intensity = 0
	rawImg := renderFrame(noIntensity)

	highThresh := bloom.Config()
	highThresh.Threshold = 5.0
	highThresh.SoftKnee = 0
	highThreshImg := renderFrame(highThresh)

	// High threshold above HDR brightness leaves scene identical to Intensity=0.
	if diff := post3dMaxChannelDiff(rawImg, highThreshImg); diff > 1 {
		t.Errorf("high threshold diff vs zero intensity = %d, want <= 1", diff)
	}

	// Count halo pixels around the bright left box that were dark in rawImg and brightened in bloomImg.
	haloPixels := 0
	mutedHaloPixels := 0
	for y := 0; y < post3dHeight; y++ {
		for x := 0; x < post3dWidth; x++ {
			rL := post3dLuma(rawImg, x, y)
			bL := post3dLuma(bloomImg, x, y)
			if rL < 0.08 && bL-rL > 0.05 {
				if x < post3dWidth/2 {
					haloPixels++
				} else {
					mutedHaloPixels++
				}
			}
		}
	}
	if haloPixels < 150 {
		t.Errorf("bright HDR box produced only %d halo pixels, want >= 150", haloPixels)
	}
	if mutedHaloPixels > 5 {
		t.Errorf("muted non-blooming box produced %d halo pixels, want <= 5", mutedHaloPixels)
	}
}

func TestPost3DSSAOMatchesReference(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(post3dWidth, post3dHeight, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()

	sceneRT, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
		Label:       "ssao.scene",
		Width:       post3dWidth,
		Height:      post3dHeight,
		ColorFormat: gpu.FormatRGBA16Float,
		DepthFormat: gpu.FormatDepth32Float,
	})
	if err != nil {
		t.Fatalf("CreateRenderTarget: %v", err)
	}
	defer func() { _ = sceneRT.Close() }()

	sp := newPost3dScenePipeline(t, nil, dev, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)
	defer func() { _ = sp.cam.Close() }()

	// Uniform unlit gray floor + back wall + cube resting directly on the floor so all darkening comes from SSAO.
	gray := [3]float32{0.82, 0.82, 0.82}
	var verts []float32
	// Floor at Y = 0
	verts = post3dAppendQuad(verts,
		math3d.Vec3{X: -6, Y: 0, Z: 6},
		math3d.Vec3{X: 6, Y: 0, Z: 6},
		math3d.Vec3{X: 6, Y: 0, Z: -3},
		math3d.Vec3{X: -6, Y: 0, Z: -3},
		math3d.Vec3Up(), gray, 1.0)
	// Back wall at Z = -2.2
	verts = post3dAppendQuad(verts,
		math3d.Vec3{X: -6, Y: 0, Z: -2.2},
		math3d.Vec3{X: 6, Y: 0, Z: -2.2},
		math3d.Vec3{X: 6, Y: 4, Z: -2.2},
		math3d.Vec3{X: -6, Y: 4, Z: -2.2},
		math3d.Vec3{Z: 1}, gray, 1.0)
	// Cube resting on the floor (Y in [0, 1.2])
	verts = post3dAppendBox(verts, math3d.Vec3{X: 0, Y: 0.6, Z: 0.0}, math3d.Vec3{X: 0.65, Y: 0.6, Z: 0.65}, gray, 1.0)

	vb, err := dev.CreateBuffer(nil, gpu.BufferDesc{Label: "ssao.verts", Usage: gpu.UsageVertex, Data: post3dPackFloats(verts)})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	defer func() { _ = vb.Close() }()

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 2.2, Y: 2.4, Z: 3.6}, math3d.DegToRad(48), float32(post3dWidth)/float32(post3dHeight), 0.2, 30)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0.4, Z: -0.3}, math3d.Vec3Up())

	cfg := render3d.SSAOConfigFromCamera(cam)
	cfg.Radius = 0.85
	cfg.Bias = 0.03
	cfg.Intensity = 1.8
	cfg.Power = 1.6
	cfg.SampleCount = 12
	cfg.BlurRadius = 2
	cfg.DepthThreshold = 0.25

	ssao, err := render3d.NewSSAOPass(nil, dev, render3d.SSAOPassOptions{
		Width:        post3dWidth,
		Height:       post3dHeight,
		ColorFormat:  gpu.FormatRGBA16Float,
		OutputFormat: gpu.FormatRGBA8Unorm,
		Config:       cfg,
	})
	if err != nil {
		t.Fatalf("NewSSAOPass: %v", err)
	}
	defer func() { _ = ssao.Close() }()

	renderFrame := func(c render3d.SSAOConfig) *image.RGBA {
		if err := ssao.SetConfig(c); err != nil {
			t.Fatalf("SetConfig: %v", err)
		}
		sp.cam.WriteCamera(cam)
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				Texture:    sceneRT.Color,
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x284060),
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				Texture:      sceneRT.Depth,
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreStore,
				ClearDepth:   1.0,
			},
		})
		sp.draw(pass, vb, uint32(len(verts)/10))
		pass.End()
		if err := ssao.Apply(dev, enc, sceneRT.Color, sceneRT.Depth, nil); err != nil {
			t.Fatalf("SSAOPass.Apply: %v", err)
		}
		dev.Submit(enc.Finish())
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return sr.Image()
	}

	ssaoImg := renderFrame(cfg)
	post3dAssertGolden(t, "post3d_ssao", ssaoImg)

	if ssao.AOTexture() == nil || !ssao.AOTexture().Valid() {
		t.Error("AOTexture() invalid after Apply")
	}

	// Project a point right in the concave cube-to-floor crease and a point on open flat floor.
	creasePt, _, ok1 := cam.WorldToScreen(math3d.Vec3{X: 0.72, Y: 0.02, Z: 0.0}, post3dWidth, post3dHeight)
	openPt, _, ok2 := cam.WorldToScreen(math3d.Vec3{X: -1.6, Y: 0.02, Z: 0.8}, post3dWidth, post3dHeight)
	if !ok1 || !ok2 {
		t.Fatalf("projected test points out of view: crease=%v(%v) open=%v(%v)", creasePt, ok1, openPt, ok2)
	}
	cx, cy := int(creasePt.X), int(creasePt.Y)
	ox, oy := int(openPt.X), int(openPt.Y)
	creaseLuma := post3dRegionLumaMean(ssaoImg, max(0, cx-2), max(0, cy-2), min(post3dWidth, cx+3), min(post3dHeight, cy+3))
	openLuma := post3dRegionLumaMean(ssaoImg, max(0, ox-3), max(0, oy-3), min(post3dWidth, ox+4), min(post3dHeight, oy+4))

	if creaseLuma >= openLuma-0.08 {
		t.Errorf("SSAO crease luma %.3f not meaningfully darker than open floor luma %.3f", creaseLuma, openLuma)
	}

	// Intensity = 0 disables ambient occlusion darkening.
	zeroCfg := cfg
	zeroCfg.Intensity = 0
	noAOImg := renderFrame(zeroCfg)
	noAOCrease := post3dRegionLumaMean(noAOImg, max(0, cx-2), max(0, cy-2), min(post3dWidth, cx+3), min(post3dHeight, cy+3))
	if noAOCrease <= creaseLuma+0.08 {
		t.Errorf("Intensity=0 crease luma %.3f should be brighter than SSAO crease %.3f", noAOCrease, creaseLuma)
	}
}

func TestPost3DDepthOfFieldMatchesReference(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(post3dWidth, post3dHeight, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()

	sceneRT, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
		Label:       "dof.scene",
		Width:       post3dWidth,
		Height:      post3dHeight,
		ColorFormat: gpu.FormatRGBA16Float,
		DepthFormat: gpu.FormatDepth32Float,
	})
	if err != nil {
		t.Fatalf("CreateRenderTarget: %v", err)
	}
	defer func() { _ = sceneRT.Close() }()

	sp := newPost3dScenePipeline(t, nil, dev, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)
	defer func() { _ = sp.cam.Close() }()

	// Three high-contrast checkerboard panels placed at near (dist=2.2), focus (dist=5.0), and far (dist=10.5),
	// scaled proportionally to their distance so all three occupy similar screen regions side by side.
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 0}, math3d.DegToRad(50), float32(post3dWidth)/float32(post3dHeight), 0.5, 25.0)
	cam.LookAt(math3d.Vec3{Z: -5}, math3d.Vec3Up())

	var verts []float32
	// Left: near panel at z = -2.2 (red/white checker)
	verts = post3dAppendCheckerPanel(verts, math3d.Vec3{X: -0.82, Y: 0, Z: -2.2}, 0.65, 0.95, 6, 8, [3]float32{0.95, 0.2, 0.2}, [3]float32{0.95, 0.95, 0.95})
	// Center: in-focus panel at z = -5.0 (green/black checker)
	verts = post3dAppendCheckerPanel(verts, math3d.Vec3{X: 0.0, Y: 0, Z: -5.0}, 1.45, 2.15, 6, 8, [3]float32{0.15, 0.95, 0.25}, [3]float32{0.05, 0.08, 0.05})
	// Right: far panel at z = -10.5 (blue/white checker)
	verts = post3dAppendCheckerPanel(verts, math3d.Vec3{X: 3.9, Y: 0, Z: -10.5}, 3.1, 4.5, 6, 8, [3]float32{0.2, 0.4, 0.95}, [3]float32{0.95, 0.95, 0.95})

	vb, err := dev.CreateBuffer(nil, gpu.BufferDesc{Label: "dof.verts", Usage: gpu.UsageVertex, Data: post3dPackFloats(verts)})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	defer func() { _ = vb.Close() }()

	dofCfg := render3d.DepthOfFieldConfigFromCamera(cam, 5.0, 1.8, 5.0)
	dof, err := render3d.NewDepthOfFieldPass(nil, dev, render3d.DepthOfFieldPassOptions{
		Width:        post3dWidth,
		Height:       post3dHeight,
		ColorFormat:  gpu.FormatRGBA16Float,
		OutputFormat: gpu.FormatRGBA8Unorm,
		Config:       dofCfg,
	})
	if err != nil {
		t.Fatalf("NewDepthOfFieldPass: %v", err)
	}
	defer func() { _ = dof.Close() }()

	renderFrame := func(c render3d.DepthOfFieldConfig) *image.RGBA {
		if err := dof.SetConfig(c); err != nil {
			t.Fatalf("SetConfig: %v", err)
		}
		sp.cam.WriteCamera(cam)
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				Texture:    sceneRT.Color,
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x141820),
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				Texture:      sceneRT.Depth,
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreStore,
				ClearDepth:   1.0,
			},
		})
		sp.draw(pass, vb, uint32(len(verts)/10))
		pass.End()
		if err := dof.Apply(dev, enc, sceneRT.Color, sceneRT.Depth, nil); err != nil {
			t.Fatalf("DepthOfFieldPass.Apply: %v", err)
		}
		dev.Submit(enc.Finish())
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return sr.Image()
	}

	dofImg := renderFrame(dofCfg)
	post3dAssertGolden(t, "post3d_dof", dofImg)

	sharpCfg := dofCfg
	sharpCfg.MaxBlurRadius = 0
	sharpImg := renderFrame(sharpCfg)

	// Measure local contrast inside each panel's screen region.
	nearRaw := post3dRegionContrast(sharpImg, 12, 26, 36, 70)
	nearDoF := post3dRegionContrast(dofImg, 12, 26, 36, 70)
	focusRaw := post3dRegionContrast(sharpImg, 52, 26, 76, 70)
	focusDoF := post3dRegionContrast(dofImg, 52, 26, 76, 70)
	farRaw := post3dRegionContrast(sharpImg, 92, 26, 116, 70)
	farDoF := post3dRegionContrast(dofImg, 92, 26, 116, 70)

	// In-focus panel preserves high contrast (> 90% of unblurred contrast).
	if focusRaw < 0.05 || focusDoF < focusRaw*0.90 {
		t.Errorf("in-focus panel blurred unexpectedly: focusDoF=%.4f vs focusRaw=%.4f", focusDoF, focusRaw)
	}
	// Near and far out-of-focus panels lose substantial high-frequency contrast (< 60% of unblurred contrast).
	if nearRaw < 0.05 || nearDoF > nearRaw*0.60 {
		t.Errorf("near out-of-focus panel did not blur enough: nearDoF=%.4f vs nearRaw=%.4f", nearDoF, nearRaw)
	}
	if farRaw < 0.05 || farDoF > farRaw*0.60 {
		t.Errorf("far out-of-focus panel did not blur enough: farDoF=%.4f vs farRaw=%.4f", farDoF, farRaw)
	}
}

func TestPost3DStackCombinedResizeAndProfiles(t *testing.T) {
	// 1. Combined SSAO + DoF + Bloom golden render on headless software device.
	sr := gpu.NewSoftwareRenderer(post3dWidth, post3dHeight, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 1.8, Y: 2.0, Z: 4.5}, math3d.DegToRad(48), float32(post3dWidth)/float32(post3dHeight), 0.2, 40)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0.4, Z: -0.5}, math3d.Vec3Up())

	ssaoCfg := render3d.SSAOConfigFromCamera(cam)
	ssaoCfg.Radius = 0.8
	ssaoCfg.Intensity = 1.6
	ssaoCfg.BlurRadius = 1

	dofCfg := render3d.DepthOfFieldConfigFromCamera(cam, 5.2, 2.5, 3.5)
	bloomCfg := render3d.BloomConfig{
		Threshold:  0.9,
		SoftKnee:   0.4,
		Intensity:  0.8,
		Radius:     1.5,
		BlurPasses: 2,
		HalfRes:    true,
	}

	stack, err := render3d.NewPostStack3D(nil, dev, render3d.PostStack3DOptions{
		Width:        post3dWidth,
		Height:       post3dHeight,
		ColorFormat:  gpu.FormatRGBA16Float,
		DepthFormat:  gpu.FormatDepth32Float,
		OutputFormat: gpu.FormatRGBA8Unorm,
		EnableSSAO:   true,
		SSAO:         ssaoCfg,
		EnableDoF:    true,
		DoF:          dofCfg,
		EnableBloom:  true,
		Bloom:        bloomCfg,
	})
	if err != nil {
		t.Fatalf("NewPostStack3D: %v", err)
	}
	defer func() { _ = stack.Close() }()

	if stack.Width() != post3dWidth || stack.Height() != post3dHeight ||
		stack.ColorFormat() != gpu.FormatRGBA16Float || stack.DepthFormat() != gpu.FormatDepth32Float ||
		stack.OutputFormat() != gpu.FormatRGBA8Unorm || !stack.Valid() {
		t.Fatalf("unexpected stack metadata: %dx%d color=%s depth=%s out=%s valid=%v",
			stack.Width(), stack.Height(), stack.ColorFormat(), stack.DepthFormat(), stack.OutputFormat(), stack.Valid())
	}

	sp := newPost3dScenePipeline(t, nil, dev, stack.ColorFormat(), stack.DepthFormat())
	defer func() { _ = sp.cam.Close() }()

	var verts []float32
	// Ground floor
	verts = post3dAppendQuad(verts,
		math3d.Vec3{X: -6, Y: 0, Z: 6}, math3d.Vec3{X: 6, Y: 0, Z: 6},
		math3d.Vec3{X: 6, Y: 0, Z: -8}, math3d.Vec3{X: -6, Y: 0, Z: -8},
		math3d.Vec3Up(), [3]float32{0.55, 0.57, 0.6}, 0.5)
	// In-focus matte block resting on ground (shows SSAO contact crease)
	verts = post3dAppendBox(verts, math3d.Vec3{X: -0.2, Y: 0.5, Z: -0.2}, math3d.Vec3{X: 0.55, Y: 0.5, Z: 0.55}, [3]float32{0.75, 0.72, 0.68}, 0.5)
	// HDR emissive beacon box (produces Bloom halo)
	verts = post3dAppendBox(verts, math3d.Vec3{X: 1.35, Y: 0.45, Z: -0.4}, math3d.Vec3{X: 0.35, Y: 0.45, Z: 0.35}, [3]float32{2.5, 1.4, 0.4}, 1.0)
	// Far background checkered pillar (blurred by DoF)
	verts = post3dAppendCheckerPanel(verts, math3d.Vec3{X: -1.8, Y: 1.2, Z: -5.5}, 2.2, 2.2, 4, 4, [3]float32{0.2, 0.5, 0.85}, [3]float32{0.85, 0.85, 0.85})

	vb, err := dev.CreateBuffer(nil, gpu.BufferDesc{Label: "combined.verts", Usage: gpu.UsageVertex, Data: post3dPackFloats(verts)})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	defer func() { _ = vb.Close() }()

	renderCombined := func() *image.RGBA {
		sp.cam.WriteCamera(cam)
		enc := dev.BeginEncoder()
		pass := stack.BeginScenePass(enc, core.RGB(0x0c1018))
		sp.draw(pass, vb, uint32(len(verts)/10))
		pass.End()
		if err := stack.ApplyScene(dev, enc, nil); err != nil {
			t.Fatalf("ApplyScene: %v", err)
		}
		dev.Submit(enc.Finish())
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return sr.Image()
	}

	combinedImg := renderCombined()
	post3dAssertGolden(t, "post3d_combined", combinedImg)

	// Passthrough blit when all passes are disabled must differ cleanly from combined post-processed output.
	if err := stack.SetSSAO(false, ssaoCfg); err != nil {
		t.Fatalf("SetSSAO(false): %v", err)
	}
	if err := stack.SetDepthOfField(false, dofCfg); err != nil {
		t.Fatalf("SetDepthOfField(false): %v", err)
	}
	if err := stack.SetBloom(false, bloomCfg); err != nil {
		t.Fatalf("SetBloom(false): %v", err)
	}
	blitImg := renderCombined()
	if diff := post3dMaxChannelDiff(combinedImg, blitImg); diff < 25 {
		t.Errorf("combined post-processing max diff vs passthrough blit = %d, want >= 25", diff)
	}

	// Re-enable all passes and resize the stack.
	if err := stack.SetSSAO(true, ssaoCfg); err != nil {
		t.Fatalf("SetSSAO(true): %v", err)
	}
	if err := stack.SetDepthOfField(true, dofCfg); err != nil {
		t.Fatalf("SetDepthOfField(true): %v", err)
	}
	if err := stack.SetBloom(true, bloomCfg); err != nil {
		t.Fatalf("SetBloom(true): %v", err)
	}
	if err := stack.Resize(dev, 96, 72); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if stack.Width() != 96 || stack.Height() != 72 || stack.SceneTarget().Width != 96 || stack.SceneTarget().Height != 72 {
		t.Fatalf("Resize did not update dimensions: %dx%d", stack.Width(), stack.Height())
	}
	// Resize back and render again.
	if err := stack.Resize(dev, post3dWidth, post3dHeight); err != nil {
		t.Fatalf("Resize back: %v", err)
	}
	afterResize := renderCombined()
	if diff := post3dMaxChannelDiff(combinedImg, afterResize); diff > 1 {
		t.Errorf("render after resize round-trip differed by %d", diff)
	}

	// 2. Run inside mounted headless nsgotest gpu.View on both full and NoGPUCompute (WebGL 2 / GLES 3.0) profiles,
	// including Retained device-loss recovery.
	for _, tc := range []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", []nsgotest.Option{nsgotest.Size(64, 48)}},
		{"no_gpu_compute", []nsgotest.Option{nsgotest.Size(64, 48), nsgotest.NoGPUCompute()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				viewStack *render3d.PostStack3D
				viewPipe  *post3dScenePipeline
				viewVB    *gpu.Buffer
				frames    int
				applyErr  error
			)
			viewCam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 4}, math3d.DegToRad(50), 64.0/48.0, 0.2, 30)
			viewCam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
			boxVerts := post3dAppendBox(nil, math3d.Vec3Zero(), math3d.Vec3{X: 0.6, Y: 0.6, Z: 0.6}, [3]float32{2.0, 1.2, 0.4}, 1.0)

			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "post3d-view",
					Style:       core.Style{Width: core.Pt(64), Height: core.Pt(48)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(d *gpu.Device) {
						if viewStack == nil {
							var err error
							viewStack, err = render3d.NewPostStack3D(cx, d, render3d.PostStack3DOptions{
								Width:        64,
								Height:       48,
								ColorFormat:  gpu.FormatRGBA16Float,
								DepthFormat:  gpu.FormatDepth32Float,
								OutputFormat: d.SurfaceFormat(),
								Retained:     true,
								EnableSSAO:   true,
								SSAO:         render3d.SSAOConfigFromCamera(viewCam),
								EnableDoF:    true,
								DoF:          render3d.DepthOfFieldConfigFromCamera(viewCam, 4.0, 2.0, 3.0),
								EnableBloom:  true,
								Bloom:        render3d.DefaultBloomConfig(),
							})
							if err != nil {
								applyErr = err
								return
							}
							viewVB, err = d.CreateBuffer(cx, gpu.BufferDesc{
								Usage:    gpu.UsageVertex,
								Data:     post3dPackFloats(boxVerts),
								Retained: true,
							})
							if err != nil {
								applyErr = err
								return
							}
						}
						viewPipe = newPost3dScenePipeline(t, cx, d, viewStack.ColorFormat(), viewStack.DepthFormat())
					},
					OnFrame: func(frame *gpu.Frame) {
						if viewStack == nil || viewPipe == nil || viewVB == nil {
							return
						}
						viewPipe.cam.WriteCamera(viewCam)
						pass := viewStack.BeginScenePass(frame, core.RGB(0x080c14))
						viewPipe.draw(pass, viewVB, uint32(len(boxVerts)/10))
						pass.End()
						if err := viewStack.ApplyScene(frame.Device(), frame, nil); err != nil {
							applyErr = err
							return
						}
						frame.Submit()
						frames++
					},
				})
			}, tc.opts...)

			app.Advance(32 * time.Millisecond)
			if applyErr != nil {
				t.Fatalf("gpu.View post-processing error: %v", applyErr)
			}
			if frames == 0 {
				t.Fatal("gpu.View rendered 0 frames")
			}
			img, err := app.GPUReadPixels("id:post3d-view")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			// Center pixel should be bright (emissive box rendered through PostStack3D).
			if l := post3dLuma(img, 32, 24); l < 0.5 {
				t.Errorf("gpu.View center pixel luma = %.3f, want >= 0.5", l)
			}

			// Verify Retained device-loss restoration continues rendering cleanly.
			app.TriggerGPUDeviceLost("id:post3d-view", nsgotest.GPULostContextLost, "context lost")
			app.RestoreGPUDevice("id:post3d-view")
			prevFrames := frames
			app.Advance(32 * time.Millisecond)
			if applyErr != nil {
				t.Fatalf("after device restore: %v", applyErr)
			}
			if frames <= prevFrames {
				t.Fatalf("no frames drawn after device restore (before=%d after=%d)", prevFrames, frames)
			}
			restoredImg, err := app.GPUReadPixels("id:post3d-view")
			if err != nil {
				t.Fatalf("GPUReadPixels after restore: %v", err)
			}
			if l := post3dLuma(restoredImg, 32, 24); l < 0.5 {
				t.Errorf("gpu.View center pixel luma after restore = %.3f, want >= 0.5", l)
			}
		})
	}
}

func TestPost3DValidationAndLifecycle(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	// BloomConfig validation
	for _, tc := range []struct {
		name string
		cfg  render3d.BloomConfig
	}{
		{"neg threshold", render3d.BloomConfig{Threshold: -0.1}},
		{"nan threshold", render3d.BloomConfig{Threshold: nan}},
		{"knee > 1", render3d.BloomConfig{SoftKnee: 1.5}},
		{"neg knee", render3d.BloomConfig{SoftKnee: -0.1}},
		{"neg intensity", render3d.BloomConfig{Intensity: -1}},
		{"inf radius", render3d.BloomConfig{Radius: inf}},
		{"too many blur passes", render3d.BloomConfig{BlurPasses: 9}},
		{"neg blur passes", render3d.BloomConfig{BlurPasses: -1}},
	} {
		if err := tc.cfg.Validate(); !errors.Is(err, render3d.ErrInvalidPostProcess) {
			t.Errorf("BloomConfig(%s).Validate = %v, want ErrInvalidPostProcess", tc.name, err)
		}
	}

	// SSAOConfig validation
	validSSAO := render3d.DefaultSSAOConfig()
	for _, tc := range []struct {
		name   string
		mutate func(*render3d.SSAOConfig)
	}{
		{"zero radius", func(c *render3d.SSAOConfig) { c.Radius = 0 }},
		{"neg bias", func(c *render3d.SSAOConfig) { c.Bias = -0.01 }},
		{"intensity > 4", func(c *render3d.SSAOConfig) { c.Intensity = 5 }},
		{"zero power", func(c *render3d.SSAOConfig) { c.Power = 0 }},
		{"too few samples", func(c *render3d.SSAOConfig) { c.SampleCount = 2 }},
		{"too many samples", func(c *render3d.SSAOConfig) { c.SampleCount = 24 }},
		{"blur radius > 4", func(c *render3d.SSAOConfig) { c.BlurRadius = 5 }},
		{"neg depth thresh", func(c *render3d.SSAOConfig) { c.DepthThreshold = -1 }},
		{"bad near/far", func(c *render3d.SSAOConfig) { c.Near = 10; c.Far = 5 }},
		{"singular proj", func(c *render3d.SSAOConfig) { c.ProjMatrix = math3d.Mat4{0: 1} }},
		{"nan proj", func(c *render3d.SSAOConfig) { c.ProjMatrix = math3d.Mat4{0: nan} }},
	} {
		cfg := validSSAO
		tc.mutate(&cfg)
		if err := cfg.Validate(); !errors.Is(err, render3d.ErrInvalidPostProcess) {
			t.Errorf("SSAOConfig(%s).Validate = %v, want ErrInvalidPostProcess", tc.name, err)
		}
	}

	// DepthOfFieldConfig validation
	validDoF := render3d.DefaultDepthOfFieldConfig()
	for _, tc := range []struct {
		name   string
		mutate func(*render3d.DepthOfFieldConfig)
	}{
		{"zero focus dist", func(c *render3d.DepthOfFieldConfig) { c.FocusDistance = 0 }},
		{"zero focus range", func(c *render3d.DepthOfFieldConfig) { c.FocusRange = 0 }},
		{"neg max blur", func(c *render3d.DepthOfFieldConfig) { c.MaxBlurRadius = -1 }},
		{"far <= near", func(c *render3d.DepthOfFieldConfig) { c.Near = 5; c.Far = 5 }},
		{"nan near", func(c *render3d.DepthOfFieldConfig) { c.Near = nan }},
	} {
		cfg := validDoF
		tc.mutate(&cfg)
		if err := cfg.Validate(); !errors.Is(err, render3d.ErrInvalidPostProcess) {
			t.Errorf("DepthOfFieldConfig(%s).Validate = %v, want ErrInvalidPostProcess", tc.name, err)
		}
	}

	// Nil receiver safety
	var (
		nilBloom *render3d.BloomPass
		nilSSAO  *render3d.SSAOPass
		nilDoF   *render3d.DepthOfFieldPass
		nilStack *render3d.PostStack3D
	)
	if nilBloom.Valid() || nilBloom.Width() != 0 || nilBloom.Height() != 0 || nilBloom.Close() != nil {
		t.Error("nil BloomPass methods returned unexpected values")
	}
	if nilSSAO.Valid() || nilSSAO.Width() != 0 || nilSSAO.Height() != 0 || nilSSAO.AOTexture() != nil || nilSSAO.Close() != nil {
		t.Error("nil SSAOPass methods returned unexpected values")
	}
	if nilDoF.Valid() || nilDoF.Width() != 0 || nilDoF.Height() != 0 || nilDoF.Close() != nil {
		t.Error("nil DepthOfFieldPass methods returned unexpected values")
	}
	if nilStack.Valid() || nilStack.Width() != 0 || nilStack.Height() != 0 || nilStack.SceneTarget() != nil ||
		nilStack.SSAO() != nil || nilStack.DepthOfField() != nil || nilStack.Bloom() != nil ||
		nilStack.BeginScenePass(nil, core.RGB(0)) != nil || nilStack.Close() != nil {
		t.Error("nil PostStack3D methods returned unexpected values")
	}

	// Nil device / invalid dimensions / invalid formats
	if _, err := render3d.NewBloomPass(nil, nil, render3d.BloomPassOptions{Width: 16, Height: 16}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewBloomPass(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewSSAOPass(nil, nil, render3d.SSAOPassOptions{Width: 16, Height: 16, Config: validSSAO}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewSSAOPass(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewDepthOfFieldPass(nil, nil, render3d.DepthOfFieldPassOptions{Width: 16, Height: 16, Config: validDoF}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewDepthOfFieldPass(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewPostStack3D(nil, nil, render3d.PostStack3DOptions{Width: 16, Height: 16}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewPostStack3D(nil dev) = %v, want ErrDeviceLost", err)
	}

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()

	if _, err := render3d.NewPostStack3D(nil, dev, render3d.PostStack3DOptions{Width: 0, Height: 16}); !errors.Is(err, render3d.ErrInvalidPostProcess) {
		t.Errorf("zero width = %v, want ErrInvalidPostProcess", err)
	}
	if _, err := render3d.NewPostStack3D(nil, dev, render3d.PostStack3DOptions{Width: 16, Height: 16, ColorFormat: gpu.FormatDepth32Float}); !errors.Is(err, render3d.ErrInvalidPostProcess) {
		t.Errorf("depth as ColorFormat = %v, want ErrInvalidPostProcess", err)
	}
	if _, err := render3d.NewPostStack3D(nil, dev, render3d.PostStack3DOptions{Width: 16, Height: 16, DepthFormat: gpu.FormatRGBA8Unorm}); !errors.Is(err, render3d.ErrInvalidPostProcess) {
		t.Errorf("color as DepthFormat = %v, want ErrInvalidPostProcess", err)
	}

	// Close idempotency & post-close rejection
	stack, err := render3d.NewPostStack3D(nil, dev, render3d.PostStack3DOptions{Width: 16, Height: 16, EnableBloom: true, Bloom: render3d.DefaultBloomConfig()})
	if err != nil {
		t.Fatalf("NewPostStack3D: %v", err)
	}
	if err := stack.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := stack.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if stack.Valid() {
		t.Error("closed PostStack3D still reports Valid()")
	}
	if err := stack.Resize(dev, 32, 32); !errors.Is(err, render3d.ErrInvalidPostProcess) {
		t.Errorf("Resize after Close = %v, want ErrInvalidPostProcess", err)
	}
	enc := dev.BeginEncoder()
	if err := stack.ApplyScene(dev, enc, nil); !errors.Is(err, render3d.ErrInvalidPostProcess) {
		t.Errorf("ApplyScene after Close = %v, want ErrInvalidPostProcess", err)
	}
}
