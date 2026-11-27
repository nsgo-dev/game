package render3d_test

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// meshWGSL passes clip-space positions through and colors each fragment from its attributes, so a pixel shows which UV and normal reached it: red = u, green = v, blue = normal.z.
const meshWGSL = `
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
	out.pos = vec4<f32>(in.pos, 1.0);
	out.color = vec4<f32>(in.uv.x, in.uv.y, in.normal.z, 1.0);
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return in.color;
}
`

var meshClear = color.RGBA{R: 16, G: 20, B: 28, A: 255}

// quadMesh is an indexed quad covering the middle half of clip space whose every vertex has UV (1, 0) and normal +Z, so it draws magenta.
func quadMesh() render3d.Mesh {
	n := math3d.Vec3{Z: 1}
	uv := math3d.Vec2{X: 1}
	return render3d.Mesh{
		Positions: []math3d.Vec3{{X: -0.5, Y: -0.5, Z: 0.5}, {X: 0.5, Y: -0.5, Z: 0.5}, {X: 0.5, Y: 0.5, Z: 0.5}, {X: -0.5, Y: 0.5, Z: 0.5}},
		Normals:   []math3d.Vec3{n, n, n, n},
		UVs:       []math3d.Vec2{uv, uv, uv, uv},
		Indices:   []uint32{0, 1, 2, 0, 2, 3},
	}
}

func meshPipeline(t *testing.T, cx *core.Scope, dev *gpu.Device, depth gpu.TextureFormat) *gpu.Pipeline {
	t.Helper()
	mod, err := gpu.CompileWGSL(meshWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	desc := gpu.PipelineDesc{
		Label:         "mesh",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
	}
	if depth != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{Format: depth, DepthWrite: true, DepthCompare: gpu.CompareLess}
	}
	pipe, err := dev.CreatePipeline(cx, desc)
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	return pipe
}

// drawSoftware draws meshes with one pipeline into a software GPU device and returns the surface.
func drawSoftware(t *testing.T, size int, meshes ...render3d.Mesh) (*image.RGBA, []*render3d.GPUMesh) {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	pipe := meshPipeline(t, nil, dev, gpu.FormatUndefined)
	gms := make([]*render3d.GPUMesh, len(meshes))
	for i, m := range meshes {
		gm, err := render3d.UploadMesh(nil, dev, m, render3d.MeshUpload{Label: "test"})
		if err != nil {
			t.Fatalf("UploadMesh %d: %v", i, err)
		}
		gms[i] = gm
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{
		LoadOp:     gpu.LoadClear,
		StoreOp:    gpu.StoreStore,
		ClearColor: core.RGB(0x10141c),
	}}})
	pass.SetPipeline(pipe)
	for _, gm := range gms {
		gm.Draw(pass)
	}
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software device: %v", err)
	}
	return sr.Image(), gms
}

func wantPixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA) {
	t.Helper()
	got := img.RGBAAt(x, y)
	near := func(a, b uint8) bool { return max(a, b)-min(a, b) <= 2 }
	if !near(got.R, want.R) || !near(got.G, want.G) || !near(got.B, want.B) || !near(got.A, want.A) {
		t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want)
	}
}

// meshViewApp mounts a 64x64 gpu.View that uploads quadMesh (retained when retained is set) in OnReady and draws it every frame.
func meshViewApp(t *testing.T, retained bool, opts ...nsgotest.Option) (*nsgotest.App, **render3d.GPUMesh, *int) {
	t.Helper()
	const size = 64
	var (
		mesh   *render3d.GPUMesh
		pipe   *gpu.Pipeline
		frames int
	)
	opts = append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				// A retained mesh keeps its buffers across a device loss, so only the first OnReady uploads it.
				if mesh == nil || !retained {
					var err error
					mesh, err = render3d.UploadMesh(cx, dev, quadMesh(), render3d.MeshUpload{Label: "quad", Retained: retained})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				}
				pipe = meshPipeline(t, cx, dev, gpu.FormatDepth24Plus)
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				mesh.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, opts...)
	return app, &mesh, &frames
}

// wantQuadDrawn advances one frame and checks that it recorded a draw and that the quad covers the view's centre but not its corner.
func wantQuadDrawn(t *testing.T, app *nsgotest.App, frames *int) {
	t.Helper()
	before, drawsBefore := *frames, gpuDraws(app)
	app.Advance(16 * time.Millisecond)
	if *frames == before {
		t.Fatal("OnFrame did not run")
	}
	if gpuDraws(app) == drawsBefore {
		t.Fatal("no draw call was recorded")
	}
	img, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	wantPixel(t, img, 2, 2, meshClear)
	wantPixel(t, img, 32, 32, color.RGBA{R: 255, B: 255, A: 255})
}

func gpuDraws(app *nsgotest.App) int {
	n := 0
	for _, fr := range app.GPUFrames() {
		n += fr.DrawsCount
	}
	return n
}

func TestMeshDrawsInGPUView(t *testing.T) {
	// NoGPUCompute gives the device the capabilities of the WebGL 2 and OpenGL ES 3.0 backends (web fallback and Android), the most limited targets the GPU view supports.
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			app, meshp, frames := meshViewApp(t, false, p.opts...)
			wantQuadDrawn(t, app, frames)
			checkQuadMesh(t, *meshp)
		})
	}
}

func TestRetainedMeshDrawsAfterDeviceLoss(t *testing.T) {
	app, meshp, frames := meshViewApp(t, true)
	wantQuadDrawn(t, app, frames)
	first := *meshp
	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if first.Valid() {
		t.Fatal("mesh still valid while the device is lost")
	}
	app.RestoreGPUDevice("id:viewport")
	if *meshp != first || !first.Valid() {
		t.Fatalf("retained mesh not restored: same=%v valid=%v", *meshp == first, first.Valid())
	}
	wantQuadDrawn(t, app, frames)
}

func checkQuadMesh(t *testing.T, mesh *render3d.GPUMesh) {
	t.Helper()

	if !mesh.Valid() || !mesh.Indexed() || mesh.IndexFormat() != gpu.IndexUint16 {
		t.Errorf("mesh valid=%v indexed=%v format=%v, want an indexed uint16 mesh", mesh.Valid(), mesh.Indexed(), mesh.IndexFormat())
	}
	if mesh.VertexCount() != 4 || mesh.IndexCount() != 6 || mesh.Triangles() != 2 {
		t.Errorf("counts = %d vertices, %d indices, %d triangles, want 4, 6, 2", mesh.VertexCount(), mesh.IndexCount(), mesh.Triangles())
	}
	// 4 vertices * 32 bytes + 6 uint16 indices (12 bytes, already a multiple of four).
	if got := mesh.ByteSize(); got != 4*render3d.MeshVertexStride+12 {
		t.Errorf("ByteSize = %d, want %d", got, 4*render3d.MeshVertexStride+12)
	}
	wantBounds := math3d.AABB{Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: 0.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}
	if mesh.Bounds() != wantBounds {
		t.Errorf("Bounds = %+v, want %+v", mesh.Bounds(), wantBounds)
	}
}

func TestMeshDrawsNonIndexedWithMissingAttributes(t *testing.T) {
	// A triangle without normals or UVs draws black: both are written as zeros.
	tri := render3d.Mesh{Positions: []math3d.Vec3{{X: -0.8, Y: -0.8, Z: 0.5}, {X: 0.8, Y: -0.8, Z: 0.5}, {X: 0, Y: 0.8, Z: 0.5}}}
	img, gms := drawSoftware(t, 32, tri)
	wantPixel(t, img, 1, 1, meshClear)
	wantPixel(t, img, 16, 18, color.RGBA{A: 255})
	if gms[0].Indexed() || gms[0].IndexBuffer() != nil || gms[0].Triangles() != 1 {
		t.Errorf("non-indexed mesh: indexed=%v triangles=%d", gms[0].Indexed(), gms[0].Triangles())
	}
}

func TestMeshDrawsWithUint32Indices(t *testing.T) {
	// 65536 vertices is the first count whose highest index would be the WebGL 2 restart value 0xFFFF in uint16.
	m := quadMesh()
	const n = 65536
	pos := make([]math3d.Vec3, n)
	nrm := make([]math3d.Vec3, n)
	uvs := make([]math3d.Vec2, n)
	base := n - 4
	copy(pos[base:], m.Positions)
	copy(nrm[base:], m.Normals)
	copy(uvs[base:], m.UVs)
	idx := make([]uint32, len(m.Indices))
	for i, v := range m.Indices {
		idx[i] = v + uint32(base)
	}
	big := render3d.Mesh{Positions: pos, Normals: nrm, UVs: uvs, Indices: idx}
	if big.IndexFormat() != gpu.IndexUint32 {
		t.Fatalf("IndexFormat = %v, want uint32", big.IndexFormat())
	}
	img, gms := drawSoftware(t, 32, big)
	wantPixel(t, img, 1, 1, meshClear)
	wantPixel(t, img, 16, 16, color.RGBA{R: 255, B: 255, A: 255})
	if gms[0].IndexFormat() != gpu.IndexUint32 || gms[0].IndexBuffer().Size() != 6*4 {
		t.Errorf("index buffer format=%v size=%d, want uint32 and 24 bytes", gms[0].IndexFormat(), gms[0].IndexBuffer().Size())
	}
}

func TestMeshBackFacesCulled(t *testing.T) {
	// Reversing the winding turns the quad away from the camera, so the back-face-culling pipeline leaves only the clear color.
	m := quadMesh()
	m.Indices = []uint32{0, 2, 1, 0, 3, 2}
	img, _ := drawSoftware(t, 32, m)
	wantPixel(t, img, 16, 16, meshClear)
}

func TestMeshClose(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	gm, err := render3d.UploadMesh(nil, sr.Device(), quadMesh(), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	if !gm.Valid() {
		t.Fatal("fresh mesh is not valid")
	}
	if err := gm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if gm.Valid() || gm.VertexBuffer().Valid() || gm.IndexBuffer().Valid() {
		t.Error("closed mesh or its buffers still valid")
	}
	if err := gm.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	var nilMesh *render3d.GPUMesh
	if nilMesh.Valid() || nilMesh.Close() != nil {
		t.Error("nil mesh must be invalid and close cleanly")
	}
}

func TestInvalidMeshDrawsNothing(t *testing.T) {
	// A closed mesh drawn after a valid one must not reuse the quad's still-bound buffers with its own counts, and a nil mesh (a failed upload) must not panic.
	const size = 64
	var (
		quad, gone *render3d.GPUMesh
		pipe       *gpu.Pipeline
		frames     int
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				var err error
				if quad, err = render3d.UploadMesh(cx, dev, quadMesh(), render3d.MeshUpload{Label: "quad"}); err != nil {
					t.Fatalf("UploadMesh quad: %v", err)
				}
				// A triangle over the whole view with more indices than the quad, white where it would draw.
				cover := render3d.Mesh{
					Positions: []math3d.Vec3{{X: -3, Y: -1, Z: 0.1}, {X: 3, Y: -1, Z: 0.1}, {X: 0, Y: 3, Z: 0.1}},
					Normals:   []math3d.Vec3{{Z: 1}, {Z: 1}, {Z: 1}},
					UVs:       []math3d.Vec2{{X: 1, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 1}},
					Indices:   []uint32{0, 1, 2, 0, 1, 2, 0, 1, 2},
				}
				if gone, err = render3d.UploadMesh(cx, dev, cover, render3d.MeshUpload{Label: "gone"}); err != nil {
					t.Fatalf("UploadMesh cover: %v", err)
				}
				if err := gone.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}
				pipe = meshPipeline(t, cx, dev, gpu.FormatDepth24Plus)
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				quad.Draw(pass)
				gone.Draw(pass)
				gone.Bind(pass)
				var failed *render3d.GPUMesh
				failed.Bind(pass)
				failed.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	before := gpuDraws(app)
	app.Advance(16 * time.Millisecond)
	if frames == 0 {
		t.Fatal("OnFrame did not run")
	}
	if got := gpuDraws(app) - before; got != 1 {
		t.Fatalf("frame recorded %d draws, want only the valid quad's 1", got)
	}
	img, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	wantPixel(t, img, 2, 2, meshClear)
	wantPixel(t, img, 32, 32, color.RGBA{R: 255, B: 255, A: 255})
	var failed *render3d.GPUMesh
	if failed.ByteSize() != 0 {
		t.Error("nil mesh ByteSize must be zero")
	}
}

func TestMeshScopeDisposalReleasesBuffers(t *testing.T) {
	var (
		mesh *render3d.GPUMesh
		show *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					var err error
					mesh, err = render3d.UploadMesh(cx, dev, quadMesh(), render3d.MeshUpload{})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())
	if !mesh.Valid() {
		t.Fatal("mesh not valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if mesh.Valid() {
		t.Error("mesh still valid after its scope was disposed")
	}
}

func TestUploadMeshRejectsInvalidMesh(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	_, err := render3d.UploadMesh(nil, sr.Device(), render3d.Mesh{}, render3d.MeshUpload{})
	if !errors.Is(err, render3d.ErrInvalidMesh) {
		t.Fatalf("UploadMesh(empty) = %v, want ErrInvalidMesh", err)
	}
	if _, err := render3d.UploadMesh(nil, nil, quadMesh(), render3d.MeshUpload{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("UploadMesh(nil device) = %v, want ErrDeviceLost", err)
	}
}

func TestMeshValidate(t *testing.T) {
	p3 := []math3d.Vec3{{}, {X: 1}, {Y: 1}}
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	tests := []struct {
		name string
		mesh render3d.Mesh
		ok   bool
	}{
		{"indexed quad", quadMesh(), true},
		{"non-indexed triangle", render3d.Mesh{Positions: p3}, true},
		{"positions only indexed", render3d.Mesh{Positions: p3, Indices: []uint32{0, 1, 2, 2, 1, 0}}, true},
		{"empty", render3d.Mesh{}, false},
		{"indices without positions", render3d.Mesh{Indices: []uint32{0, 0, 0}}, false},
		{"partial triangle", render3d.Mesh{Positions: p3[:2]}, false},
		{"index count", render3d.Mesh{Positions: p3, Indices: []uint32{0, 1}}, false},
		{"index range", render3d.Mesh{Positions: p3, Indices: []uint32{0, 1, 3}}, false},
		{"normal count", render3d.Mesh{Positions: p3, Normals: p3[:1]}, false},
		{"uv count", render3d.Mesh{Positions: p3, UVs: make([]math3d.Vec2, 4)}, false},
		{"nan position", render3d.Mesh{Positions: []math3d.Vec3{{X: nan}, {}, {}}}, false},
		{"inf normal", render3d.Mesh{Positions: p3, Normals: []math3d.Vec3{{}, {Z: inf}, {}}}, false},
		{"nan uv", render3d.Mesh{Positions: p3, UVs: []math3d.Vec2{{}, {}, {Y: nan}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mesh.Validate()
			if tt.ok && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if !tt.ok && !errors.Is(err, render3d.ErrInvalidMesh) {
				t.Fatalf("Validate = %v, want ErrInvalidMesh", err)
			}
		})
	}
}

func TestMeshVertexData(t *testing.T) {
	m := render3d.Mesh{
		Positions: []math3d.Vec3{{X: 1, Y: 2, Z: 3}, {X: -1, Y: -2, Z: -3}},
		Normals:   []math3d.Vec3{{Z: 1}, {X: -1}},
		UVs:       []math3d.Vec2{{X: 0.25, Y: 0.75}, {X: 1, Y: 0}},
	}
	prefix := []byte{0xAA}
	data := m.AppendVertexData(prefix)
	if len(data) != 1+m.VertexDataSize() || m.VertexDataSize() != 2*render3d.MeshVertexStride {
		t.Fatalf("len = %d, VertexDataSize = %d", len(data), m.VertexDataSize())
	}
	if data[0] != 0xAA {
		t.Fatal("prefix overwritten")
	}
	got := decodeVertices(data[1:])
	want := [][8]float32{{1, 2, 3, 0, 0, 1, 0.25, 0.75}, {-1, -2, -3, -1, 0, 0, 1, 0}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("vertex %d = %v, want %v", i, got[i], want[i])
		}
	}

	// Stale bytes in a reused buffer are overwritten with zeros for missing attributes.
	bare := render3d.Mesh{Positions: m.Positions}
	reuse := make([]byte, 0, 64)
	reuse = append(reuse, make([]byte, 64)...)
	for i := range reuse {
		reuse[i] = 0xFF
	}
	out := bare.AppendVertexData(reuse[:0])
	for i, v := range decodeVertices(out) {
		if v != ([8]float32{m.Positions[i].X, m.Positions[i].Y, m.Positions[i].Z}) {
			t.Errorf("bare vertex %d = %v, want zero normal and UV", i, v)
		}
	}
	if &out[0] != &reuse[0] {
		t.Error("AppendVertexData reallocated despite enough capacity")
	}
}

func TestMeshIndexData(t *testing.T) {
	tests := []struct {
		name     string
		vertices int
		indices  []uint32
		format   gpu.IndexFormat
		size     int
	}{
		{"none", 3, nil, gpu.IndexUint16, 0},
		{"uint16 padded", 3, []uint32{0, 1, 2}, gpu.IndexUint16, 8},
		{"uint16 even", 3, []uint32{0, 1, 2, 2, 1, 0}, gpu.IndexUint16, 12},
		{"uint16 largest", 65535, []uint32{65534, 0, 1}, gpu.IndexUint16, 8},
		{"uint32 first", 65536, []uint32{65535, 0, 1}, gpu.IndexUint32, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := render3d.Mesh{Positions: make([]math3d.Vec3, tt.vertices), Indices: tt.indices}
			if m.IndexFormat() != tt.format || m.IndexDataSize() != tt.size {
				t.Fatalf("format=%v size=%d, want %v %d", m.IndexFormat(), m.IndexDataSize(), tt.format, tt.size)
			}
			dirty := []byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}
			data := m.AppendIndexData(dirty[:1])
			if len(data) != 1+tt.size || data[0] != 9 {
				t.Fatalf("data = %v", data)
			}
			got := decodeIndices(data[1:], tt.format, len(tt.indices))
			for i := range tt.indices {
				if got[i] != tt.indices[i] {
					t.Errorf("index %d = %d, want %d", i, got[i], tt.indices[i])
				}
			}
			width := 2
			if tt.format == gpu.IndexUint32 {
				width = 4
			}
			for i, b := range data[1+len(tt.indices)*width:] {
				if b != 0 {
					t.Errorf("padding byte %d = %d, want 0", i, b)
				}
			}
		})
	}
}

func TestMeshVertexLayout(t *testing.T) {
	l := render3d.MeshVertexLayout()
	if l.Stride != render3d.MeshVertexStride || l.StepMode != gpu.StepVertex || len(l.Attributes) != 3 {
		t.Fatalf("layout = %+v", l)
	}
	want := []gpu.VertexAttribute{
		{Format: gpu.VertexFloat32x3, Offset: 0, Location: 0},
		{Format: gpu.VertexFloat32x3, Offset: 12, Location: 1},
		{Format: gpu.VertexFloat32x2, Offset: 24, Location: 2},
	}
	for i := range want {
		if l.Attributes[i] != want[i] {
			t.Errorf("attribute %d = %+v, want %+v", i, l.Attributes[i], want[i])
		}
	}
	l.Attributes[0].Location = 7
	if render3d.MeshVertexLayout().Attributes[0].Location != 0 {
		t.Error("MeshVertexLayout shares its Attributes slice between calls")
	}
}

func TestMeshBounds(t *testing.T) {
	if !(render3d.Mesh{}).Bounds().Empty() {
		t.Error("empty mesh bounds not empty")
	}
	m := render3d.Mesh{Positions: []math3d.Vec3{{X: 1, Y: -2, Z: 3}, {X: -4, Y: 5, Z: -6}}}
	want := math3d.AABB{Min: math3d.Vec3{X: -4, Y: -2, Z: -6}, Max: math3d.Vec3{X: 1, Y: 5, Z: 3}}
	if m.Bounds() != want {
		t.Errorf("Bounds = %+v, want %+v", m.Bounds(), want)
	}
}

func TestMeshAppendDoesNotAllocate(t *testing.T) {
	m := gridMesh(32)
	vbuf := make([]byte, 0, m.VertexDataSize())
	ibuf := make([]byte, 0, m.IndexDataSize())
	allocs := testing.AllocsPerRun(10, func() {
		vbuf = m.AppendVertexData(vbuf[:0])
		ibuf = m.AppendIndexData(ibuf[:0])
	})
	if allocs != 0 {
		t.Errorf("allocs = %v, want 0", allocs)
	}
}

// FuzzMeshData checks that any mesh Validate accepts encodes to exactly the advertised sizes and decodes back to its attributes and indices.
func FuzzMeshData(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, 2}, uint8(3), uint8(3))
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9}, uint8(0), uint8(6))
	f.Fuzz(func(t *testing.T, raw []byte, flags uint8, nIdx uint8) {
		n := len(raw)
		if n == 0 || n > 512 {
			return
		}
		m := render3d.Mesh{Positions: make([]math3d.Vec3, n)}
		for i, b := range raw {
			m.Positions[i] = math3d.Vec3{X: float32(b), Y: -float32(b), Z: float32(i)}
		}
		if flags&1 != 0 {
			m.Normals = make([]math3d.Vec3, n)
			for i := range m.Normals {
				m.Normals[i] = math3d.Vec3{Z: float32(raw[i]) / 255}
			}
		}
		if flags&2 != 0 {
			m.UVs = make([]math3d.Vec2, n)
			for i := range m.UVs {
				m.UVs[i] = math3d.Vec2{X: float32(raw[i]) / 255, Y: 1}
			}
		}
		for i := range int(nIdx) {
			m.Indices = append(m.Indices, uint32(raw[i%n]))
		}
		if err := m.Validate(); err != nil {
			if !errors.Is(err, render3d.ErrInvalidMesh) {
				t.Fatalf("Validate error does not wrap ErrInvalidMesh: %v", err)
			}
			return
		}
		vd := m.AppendVertexData(nil)
		id := m.AppendIndexData(nil)
		if len(vd) != m.VertexDataSize() || len(id) != m.IndexDataSize() || len(id)%4 != 0 {
			t.Fatalf("sizes vertex=%d/%d index=%d/%d", len(vd), m.VertexDataSize(), len(id), m.IndexDataSize())
		}
		for i, v := range decodeVertices(vd) {
			p := m.Positions[i]
			if v[0] != p.X || v[1] != p.Y || v[2] != p.Z {
				t.Fatalf("vertex %d position = %v, want %v", i, v[:3], p)
			}
			if len(m.Normals) != 0 && v[5] != m.Normals[i].Z {
				t.Fatalf("vertex %d normal.z = %v", i, v[5])
			}
			if len(m.UVs) != 0 && (v[6] != m.UVs[i].X || v[7] != 1) {
				t.Fatalf("vertex %d uv = %v", i, v[6:])
			}
		}
		for i, idx := range decodeIndices(id, m.IndexFormat(), len(m.Indices)) {
			if idx != m.Indices[i] {
				t.Fatalf("index %d = %d, want %d", i, idx, m.Indices[i])
			}
		}
	})
}

func BenchmarkMeshAppendVertexData(b *testing.B) {
	m := gridMesh(128)
	buf := make([]byte, 0, m.VertexDataSize())
	b.SetBytes(int64(m.VertexDataSize()))
	b.ReportAllocs()
	for b.Loop() {
		buf = m.AppendVertexData(buf[:0])
	}
}

func BenchmarkMeshAppendIndexData(b *testing.B) {
	m := gridMesh(128)
	buf := make([]byte, 0, m.IndexDataSize())
	b.SetBytes(int64(m.IndexDataSize()))
	b.ReportAllocs()
	for b.Loop() {
		buf = m.AppendIndexData(buf[:0])
	}
}

func BenchmarkUploadMesh(b *testing.B) {
	m := gridMesh(128)
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()
	b.ReportAllocs()
	for b.Loop() {
		gm, err := render3d.UploadMesh(nil, dev, m, render3d.MeshUpload{})
		if err != nil {
			b.Fatal(err)
		}
		_ = gm.Close()
	}
}

// gridMesh returns an n by n grid of quads on the XY plane with normals, UVs and indices.
func gridMesh(n int) render3d.Mesh {
	var m render3d.Mesh
	for y := 0; y <= n; y++ {
		for x := 0; x <= n; x++ {
			u, v := float32(x)/float32(n), float32(y)/float32(n)
			m.Positions = append(m.Positions, math3d.Vec3{X: u*2 - 1, Y: v*2 - 1})
			m.Normals = append(m.Normals, math3d.Vec3{Z: 1})
			m.UVs = append(m.UVs, math3d.Vec2{X: u, Y: v})
		}
	}
	row := uint32(n + 1)
	for y := uint32(0); y < uint32(n); y++ {
		for x := uint32(0); x < uint32(n); x++ {
			i := y*row + x
			m.Indices = append(m.Indices, i, i+1, i+row+1, i, i+row+1, i+row)
		}
	}
	return m
}

func decodeVertices(b []byte) [][8]float32 {
	out := make([][8]float32, len(b)/render3d.MeshVertexStride)
	for i := range out {
		for j := range 8 {
			out[i][j] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*render3d.MeshVertexStride+j*4:]))
		}
	}
	return out
}

func decodeIndices(b []byte, format gpu.IndexFormat, n int) []uint32 {
	out := make([]uint32, n)
	for i := range out {
		if format == gpu.IndexUint32 {
			out[i] = binary.LittleEndian.Uint32(b[i*4:])
		} else {
			out[i] = uint32(binary.LittleEndian.Uint16(b[i*2:]))
		}
	}
	return out
}
