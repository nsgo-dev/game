package render3d_test

import (
	"encoding/binary"
	"image"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	refWidth  = 128
	refHeight = 96
)

// refShaderWGSL shades unlit vertex colors by how squarely a face looks at the viewer, using the camera block's eye point, so perspective and orthographic cameras give visibly different shading as well as different shapes.
const refShaderWGSL = render3d.CameraWGSL + `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) color: vec4<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.world = in.pos;
	out.normal = in.normal;
	out.color = in.color;
	return out;
}

@vertex
fn vs_split(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.proj * (camera.view * vec4<f32>(in.pos, 1.0));
	out.world = in.pos;
	out.normal = in.normal;
	out.color = in.color;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let v = normalize(camera.eye.xyz - in.world * camera.eye.w);
	let k = 0.45 + 0.55 * max(dot(normalize(in.normal), v), 0.0);
	return vec4<f32>(in.color.rgb * k, 1.0);
}
`

// Reference scene: two identical unit cubes, red near the camera and green far from it, a blue one to the side, all on a gray ground square.
var (
	refRed    = refCube{center: math3d.Vec3{X: -1.5, Y: 0.5, Z: 1.5}, color: [3]float32{0.95, 0.2, 0.15}}
	refGreen  = refCube{center: math3d.Vec3{X: 1.5, Y: 0.5, Z: -2.5}, color: [3]float32{0.2, 0.9, 0.25}}
	refBlue   = refCube{center: math3d.Vec3{X: 2.6, Y: 0.5, Z: 1.2}, color: [3]float32{0.2, 0.35, 0.95}}
	refGround = [3]float32{0.55, 0.55, 0.55}
	refClear  = core.RGB(0x10141c)
)

type refCube struct {
	center math3d.Vec3
	color  [3]float32
}

func appendVertex(dst []float32, p, n math3d.Vec3, c [3]float32) []float32 {
	return append(dst, p.X, p.Y, p.Z, n.X, n.Y, n.Z, c[0], c[1], c[2], 1)
}

// appendQuad appends two triangles for the quad a, b, c, d sharing normal n.
func appendQuad(dst []float32, a, b, c, d, n math3d.Vec3, col [3]float32) []float32 {
	for _, p := range [...]math3d.Vec3{a, b, c, a, c, d} {
		dst = appendVertex(dst, p, n, col)
	}
	return dst
}

func appendCube(dst []float32, cube refCube) []float32 {
	type face struct{ n, u, v math3d.Vec3 }
	faces := [...]face{
		{math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{X: -1}, math3d.Vec3{Z: 1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{Y: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}},
		{math3d.Vec3{Y: -1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: 1}},
		{math3d.Vec3{Z: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{Z: -1}, math3d.Vec3{X: -1}, math3d.Vec3{Y: 1}},
	}
	for _, f := range faces {
		c := cube.center.Add(f.n.Scale(0.5))
		u, v := f.u.Scale(0.5), f.v.Scale(0.5)
		dst = appendQuad(dst,
			c.Sub(u).Sub(v), c.Add(u).Sub(v), c.Add(u).Add(v), c.Sub(u).Add(v),
			f.n, cube.color)
	}
	return dst
}

func refSceneVertices() []float32 {
	var v []float32
	up := math3d.Vec3Up()
	v = appendQuad(v,
		math3d.Vec3{X: -5, Z: 5}, math3d.Vec3{X: 5, Z: 5}, math3d.Vec3{X: 5, Z: -5}, math3d.Vec3{X: -5, Z: -5},
		up, refGround)
	for _, c := range [...]refCube{refRed, refGreen, refBlue} {
		v = appendCube(v, c)
	}
	return v
}

func packFloat32s(fs []float32) []byte {
	out := make([]byte, 4*len(fs))
	for i, f := range fs {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(f))
	}
	return out
}

// refRenderer draws the reference scene headless through NSGo's software GPU device with a render3d.CameraBuffer bound at render3d.CameraGroup.
type refRenderer struct {
	sr       *gpu.SoftwareRenderer
	dev      *gpu.Device
	cam      *render3d.CameraBuffer
	pipeline *gpu.Pipeline
	split    *gpu.Pipeline
	vertices *gpu.Buffer
	count    uint32
}

func newRefRenderer(t *testing.T) *refRenderer {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(refWidth, refHeight, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	mod, err := gpu.CompileWGSL(refShaderWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	verts := refSceneVertices()
	vb, err := dev.CreateBuffer(nil, gpu.BufferDesc{Label: "scene", Usage: gpu.UsageVertex, Data: packFloat32s(verts)})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	layouts := make([]*gpu.BindGroupLayout, render3d.CameraGroup+1)
	layouts[render3d.CameraGroup] = camBuf.Layout()
	desc := func(vertexEntry string) gpu.PipelineDesc {
		return gpu.PipelineDesc{
			Label:    vertexEntry,
			Vertex:   gpu.ShaderStage{Module: mod, EntryPoint: vertexEntry},
			Fragment: gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
			Layouts:  layouts,
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
				Format:       gpu.FormatDepth24Plus,
				DepthWrite:   true,
				DepthCompare: gpu.CompareLess,
			},
			Targets: []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
		}
	}
	pipe, err := dev.CreatePipeline(nil, desc("vs_main"))
	if err != nil {
		t.Fatalf("CreatePipeline vs_main: %v", err)
	}
	split, err := dev.CreatePipeline(nil, desc("vs_split"))
	if err != nil {
		t.Fatalf("CreatePipeline vs_split: %v", err)
	}
	return &refRenderer{sr: sr, dev: dev, cam: camBuf, pipeline: pipe, split: split, vertices: vb, count: uint32(len(verts) / 10)}
}

// render draws one frame seen through cam with pipe and returns it.
func (r *refRenderer) render(t *testing.T, cam *scene3d.Camera, pipe *gpu.Pipeline) *image.RGBA {
	t.Helper()
	r.cam.WriteCamera(cam)
	enc := r.dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "camera-reference",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: refClear}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, r.cam.BindGroup())
	pass.SetVertexBuffer(0, r.vertices, 0)
	pass.Draw(r.count, 0)
	pass.End()
	r.dev.Submit(enc.Finish())
	if err := r.sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return r.sr.Image()
}

// renderOnLoop orbits cam around the scene on a game.Loop: Update steps an OrbitController by the fixed dt and Draw renders the camera interpolated between the last two updates. It returns the last frame drawn and the camera it was drawn with.
func renderOnLoop(t *testing.T, r *refRenderer, cam *scene3d.Camera) (*image.RGBA, scene3d.Camera) {
	t.Helper()
	cam.SetViewport(refWidth, refHeight)
	orbit := scene3d.NewOrbitController(math3d.Vec3{Y: 0.5, Z: -0.5}, 9)
	orbit.SetAngles(math3d.DegToRad(10), math3d.DegToRad(28))
	orbit.Snap()
	orbit.Apply(cam)
	prev := *cam

	var (
		frame *image.RGBA
		drawn scene3d.Camera
		draws int
	)
	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			prev = *cam
			orbit.Orbit(math3d.DegToRad(30)*float32(dt.Seconds()), 0)
			orbit.Update(cam, dt)
		},
		Draw: func(alpha float64) {
			drawn = prev.Lerp(cam, float32(alpha))
			frame = r.render(t, &drawn, r.pipeline)
			draws++
		},
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loop.Run(cx)
		return core.View{}
	})
	app.Advance(500 * time.Millisecond)
	if draws == 0 || frame == nil {
		t.Fatal("game.Loop drew no frame")
	}
	if drawn.Pos.ApproxEqual(cam.Pos, 1e-6) && drawn.Pos.ApproxEqual(prev.Pos, 1e-6) {
		t.Fatal("camera did not move on the loop")
	}
	return frame, drawn
}

// assertReference compares got with testdata/golden/<name>.png, allowing a few edge pixels to differ by rounding between CPU architectures (fused multiply-add on arm64), and writes got and a diff image on mismatch. UPDATE_GOLDEN=1 rewrites the reference.
func assertReference(t *testing.T, name string, got *image.RGBA) {
	t.Helper()
	assertReferenceAt(t, gametest.GoldenPath("", name), got)
}

// assertReferenceAt compares got with the reference PNG at path, as assertReference does for the golden directory.
func assertReferenceAt(t *testing.T, path string, got *image.RGBA) {
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
		_ = gametest.WritePNG(dir+"/"+name+".got.png", got)
		_ = gametest.WritePNG(dir+"/"+name+".diff.png", diff)
		t.Fatalf("%s: %d pixels differ from the reference (limit %d); got and diff written to %s", name, far, limit, dir)
	}
}

// dominant classifies a pixel as the scene's red, green or blue cube by its strongest channel, or 0 for the gray ground and the background.
func dominant(c [3]uint8) byte {
	r, g, b := int(c[0]), int(c[1]), int(c[2])
	switch {
	case r > g+40 && r > b+40:
		return 'r'
	case g > r+40 && g > b+40:
		return 'g'
	case b > r+40 && b > g+40:
		return 'b'
	}
	return 0
}

func pixel(img *image.RGBA, x, y int) [3]uint8 {
	c := img.RGBAAt(x, y)
	return [3]uint8{c.R, c.G, c.B}
}

func coverage(img *image.RGBA) map[byte]int {
	n := map[byte]int{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			n[dominant(pixel(img, x, y))]++
		}
	}
	return n
}

// assertProjectedColors checks the rendered frame against the CPU camera: each cube's top-face center, projected with Camera.WorldToScreen, lands on that cube's color.
func assertProjectedColors(t *testing.T, img *image.RGBA, cam *scene3d.Camera) {
	t.Helper()
	for _, tc := range [...]struct {
		cube refCube
		want byte
	}{{refRed, 'r'}, {refGreen, 'g'}, {refBlue, 'b'}} {
		top := tc.cube.center.Add(math3d.Vec3{Y: 0.5})
		s, _, visible := cam.WorldToScreen(top, refWidth, refHeight)
		if !visible {
			t.Fatalf("top of %c cube projects outside the view: %v", tc.want, s)
		}
		x, y := int(s.X), int(s.Y)
		if got := dominant(pixel(img, x, y)); got != tc.want {
			t.Errorf("pixel (%d, %d) under the %c cube's top = %v, classified %q", x, y, tc.want, pixel(img, x, y), got)
		}
	}
	if c := pixel(img, 1, 1); c != [3]uint8{0x10, 0x14, 0x1c} {
		t.Errorf("top-left pixel = %v, want the clear color", c)
	}
}

func TestPerspectiveCameraMatchesReference(t *testing.T) {
	r := newRefRenderer(t)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(50), 1, 0.1, 50)
	img, drawn := renderOnLoop(t, r, cam)
	assertReference(t, "camera_perspective", img)
	assertProjectedColors(t, img, &drawn)

	// Perspective foreshortening: the far green cube covers clearly fewer pixels than the identical near red one.
	n := coverage(img)
	if n['r'] == 0 || float64(n['g']) > 0.75*float64(n['r']) {
		t.Errorf("perspective coverage red=%d green=%d, want green well under red", n['r'], n['g'])
	}

	// The split pipeline computes proj * (view * p) from the block's separate matrices; it must draw what view_proj draws.
	split := r.render(t, &drawn, r.split)
	whole := r.render(t, &drawn, r.pipeline)
	if _, mismatched := gametest.DiffImage(split, whole); mismatched > refWidth*refHeight/200 {
		t.Errorf("proj*view and view_proj frames differ in %d pixels", mismatched)
	}
}

func TestOrthographicCameraMatchesReference(t *testing.T) {
	r := newRefRenderer(t)
	cam := scene3d.NewOrthographicCamera(math3d.Vec3Zero(), 7, 1, 0.1, 50)
	img, drawn := renderOnLoop(t, r, cam)
	assertReference(t, "camera_orthographic", img)
	assertProjectedColors(t, img, &drawn)

	// No foreshortening: identical cubes cover the same area wherever they stand.
	n := coverage(img)
	if n['r'] == 0 || math.Abs(float64(n['g']-n['r'])) > 0.06*float64(n['r']) {
		t.Errorf("orthographic coverage red=%d green=%d, want equal", n['r'], n['g'])
	}
}

func TestCameraUniformEncodeLayout(t *testing.T) {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 1, Y: 2, Z: 3}, math3d.DegToRad(60), 1.5, 0.1, 100)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	u := render3d.NewCameraUniform(cam)
	if !u.ViewProj.ApproxEqual(cam.ViewProj(), 1e-6) {
		t.Fatalf("ViewProj = %v, want %v", u.ViewProj, cam.ViewProj())
	}
	if u.Eye != (math3d.Vec4{X: 1, Y: 2, Z: 3, W: 1}) {
		t.Fatalf("perspective Eye = %v, want (1, 2, 3, 1)", u.Eye)
	}
	var buf [render3d.CameraUniformSize + 8]byte
	out := u.Encode(buf[:])
	if len(out) != render3d.CameraUniformSize || render3d.CameraUniformSize != 208 {
		t.Fatalf("encoded %d bytes, CameraUniformSize %d, want 208", len(out), render3d.CameraUniformSize)
	}
	f := func(i int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(out[4*i:])) }
	for i := range 16 {
		if f(i) != u.View[i] || f(16+i) != u.Proj[i] || f(32+i) != u.ViewProj[i] {
			t.Fatalf("matrix float %d out of place", i)
		}
	}
	if f(48) != 1 || f(49) != 2 || f(50) != 3 || f(51) != 1 {
		t.Fatalf("eye = (%v, %v, %v, %v), want (1, 2, 3, 1)", f(48), f(49), f(50), f(51))
	}
	if allocs := testing.AllocsPerRun(100, func() { u.Encode(buf[:]) }); allocs != 0 {
		t.Fatalf("Encode allocated %v times", allocs)
	}

	// A slice with room in its capacity but too short a length still panics: Encode never writes past len(dst).
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Encode(len 0, cap 208) did not panic")
			}
		}()
		u.Encode(make([]byte, 0, render3d.CameraUniformSize))
	}()

	ortho := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 100)
	if e := render3d.NewCameraUniform(ortho).Eye; !e.ApproxEqual(math3d.Vec4{Z: 1}, 1e-6) {
		t.Fatalf("orthographic Eye = %v, want (0, 0, 1, 0)", e)
	}
}

func TestCameraBufferLifecycle(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	b, err := render3d.NewCameraBuffer(nil, sr.Device())
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	if b.Buffer().Size() != render3d.CameraUniformSize || b.Buffer().Usage()&gpu.UsageUniform == 0 {
		t.Fatalf("buffer size %d usage %v", b.Buffer().Size(), b.Buffer().Usage())
	}
	if !b.Layout().Valid() || !b.BindGroup().Valid() {
		t.Fatal("layout or bind group not valid after creation")
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if b.Buffer().Valid() || b.Layout().Valid() || b.BindGroup().Valid() {
		t.Fatal("resources still valid after Close")
	}
}

func BenchmarkCameraUniformEncode(b *testing.B) {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 5}, math3d.DegToRad(60), 1.5, 0.1, 100)
	var buf [render3d.CameraUniformSize]byte
	b.ReportAllocs()
	for b.Loop() {
		u := render3d.NewCameraUniform(cam)
		u.Encode(buf[:])
	}
}
