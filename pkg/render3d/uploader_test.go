package render3d_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// texturedMeshWGSL samples a 2D texture at @group(0) @binding(0)/(1) using vertex UVs so rendered output verifies both vertex/index buffers and texture pixels.
const texturedMeshWGSL = `
@group(0) @binding(0) var tex: texture_2d<f32>;
@group(0) @binding(1) var samp: sampler;

struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) pos: vec4<f32>,
	@location(0) uv: vec2<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.pos = vec4<f32>(in.pos, 1.0);
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return textureSample(tex, samp, in.uv);
}
`

func makeGridMesh(cols, rows int) render3d.Mesh {
	vertsX := cols + 1
	vertsY := rows + 1
	numVerts := vertsX * vertsY
	positions := make([]math3d.Vec3, numVerts)
	normals := make([]math3d.Vec3, numVerts)
	uvs := make([]math3d.Vec2, numVerts)
	for y := 0; y < vertsY; y++ {
		v := float32(y) / float32(rows)
		py := -0.75 + 1.5*v
		for x := 0; x < vertsX; x++ {
			u := float32(x) / float32(cols)
			px := -0.75 + 1.5*u
			i := y*vertsX + x
			positions[i] = math3d.Vec3{X: px, Y: py, Z: 0.5}
			normals[i] = math3d.Vec3{Z: 1}
			uvs[i] = math3d.Vec2{X: u, Y: v}
		}
	}
	indices := make([]uint32, 0, cols*rows*6)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			i0 := uint32(y*vertsX + x)
			i1 := i0 + 1
			i2 := i0 + uint32(vertsX)
			i3 := i2 + 1
			indices = append(indices, i0, i1, i3, i0, i3, i2)
		}
	}
	return render3d.Mesh{
		Positions: positions,
		Normals:   normals,
		UVs:       uvs,
		Indices:   indices,
	}
}

func makePatternRGBA(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			checker := ((x / max(w/8, 1)) + (y / max(h/8, 1))) % 2
			r := uint8((x * 255) / max(w-1, 1))
			g := uint8((y * 255) / max(h-1, 1))
			b := uint8(60)
			if checker == 1 {
				b = 220
			}
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

func renderTexturedMesh(t *testing.T, dev *gpu.Device, sr *gpu.SoftwareRenderer, mesh *render3d.GPUMesh, tex *gpu.Texture) *image.RGBA {
	t.Helper()
	mod, err := gpu.CompileWGSL(texturedMeshWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	bgl, err := dev.CreateBindGroupLayoutDesc(nil, gpu.BindGroupLayoutDesc{
		Label: "tex",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		t.Fatalf("CreateBindGroupLayoutDesc: %v", err)
	}
	defer func() { _ = bgl.Close() }()

	samp, err := dev.CreateSampler(nil, gpu.SamplerDesc{
		Label:     "nearest",
		MinFilter: gpu.FilterNearest,
		MagFilter: gpu.FilterNearest,
		AddressU:  gpu.AddressClampToEdge,
		AddressV:  gpu.AddressClampToEdge,
	})
	if err != nil {
		t.Fatalf("CreateSampler: %v", err)
	}
	defer func() { _ = samp.Close() }()

	bg, err := dev.CreateBindGroupDesc(nil, gpu.BindGroupDesc{
		Label:  "tex",
		Layout: bgl,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Texture: tex},
			{Binding: 1, Sampler: samp},
		},
	})
	if err != nil {
		t.Fatalf("CreateBindGroupDesc: %v", err)
	}
	defer func() { _ = bg.Close() }()

	pipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:         "textured-mesh",
		Layouts:       []*gpu.BindGroupLayout{bgl},
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	defer func() { _ = pipe.Close() }()

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{
		LoadOp:     gpu.LoadClear,
		StoreOp:    gpu.StoreStore,
		ClearColor: core.RGB(0x10141c),
	}}})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(0, bg)
	mesh.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	out := image.NewRGBA(sr.Image().Bounds())
	copy(out.Pix, sr.Image().Pix)
	return out
}

func stepUntilDone(t *testing.T, u *render3d.Uploader, maxFrames int, check func(frame int, stats render3d.UploadFrameStats)) int {
	t.Helper()
	for frame := 0; frame < maxFrames; frame++ {
		stats := u.Step()
		if check != nil {
			check(frame, stats)
		}
		if stats.Pending == 0 {
			return frame + 1
		}
		// Give background workers a scheduling slice if waiting on decode.
		if stats.BytesUploaded == 0 && stats.Completed == 0 && stats.Failed == 0 && stats.Canceled == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatalf("uploader did not finish within %d frames (pending=%d, inFlight=%d)", maxFrames, u.Pending(), u.InFlightDecodes())
	return maxFrames
}

// waitFor polls cond until it holds, failing the test after a few seconds.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(100 * time.Microsecond)
	}
}

func readBufferSync(t *testing.T, dev *gpu.Device, sr *gpu.SoftwareRenderer, buf *gpu.Buffer) []byte {
	t.Helper()
	var got []byte
	var readErr error
	dev.ReadBuffer(buf, 0, buf.Size(), func(data []byte, err error) {
		readErr = err
		got = append([]byte(nil), data...)
	})
	sr.WaitIdle()
	if readErr != nil {
		t.Fatalf("ReadBuffer: %v", readErr)
	}
	return got
}

func readTextureSync(t *testing.T, dev *gpu.Device, sr *gpu.SoftwareRenderer, tex *gpu.Texture, mip uint32) []byte {
	t.Helper()
	d := tex.Desc()
	mw, mh := gpu.MipSize(d.Width, d.Height, mip)
	var got []byte
	var readErr error
	dev.ReadTexture(tex, gpu.TextureRegion{
		MipLevel: mip,
		Width:    mw,
		Height:   mh,
	}, func(rgba []byte, err error) {
		readErr = err
		got = append([]byte(nil), rgba...)
	})
	sr.WaitIdle()
	if readErr != nil {
		t.Fatalf("ReadTexture mip %d: %v", mip, readErr)
	}
	return got
}

func TestUploaderBudgetedMeshAndTexturePixelIdentical(t *testing.T) {
	// Build a multi-megabyte mesh (256x256 grid = 66,049 vertices > 2.1 MiB vertex buffer + 393,216 uint32 indices = 1.57 MiB index buffer -> ~3.69 MiB total) and a 512x512 RGBA8 texture (1 MiB) -> ~4.69 MiB total.
	mesh := makeGridMesh(256, 256)
	texImg := makePatternRGBA(512, 512)

	const budget = 64 * 1024 // 64 KiB per frame

	srStream := gpu.NewSoftwareRenderer(64, 64, gpu.FormatUndefined)
	t.Cleanup(srStream.Close)
	devStream := srStream.Device()

	u := render3d.NewUploader(nil, devStream, render3d.UploaderOptions{
		BytesPerFrame: budget,
		ChunkBytes:    32 * 1024,
		Workers:       2,
	})
	t.Cleanup(func() { _ = u.Close() })

	var (
		streamMesh *render3d.GPUMesh
		streamTex  *render3d.GPUTexture
		meshErr    error
		texErr     error
	)

	hMesh := u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		return mesh, nil
	}, func(g *render3d.GPUMesh, err error) {
		streamMesh = g
		meshErr = err
	}, render3d.MeshUpload{Label: "grid"})

	hTex := u.QueueTexture(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (image.Image, error) {
		return texImg, nil
	}, func(tex *render3d.GPUTexture, err error) {
		streamTex = tex
		texErr = err
	}, render3d.TextureUpload{Label: "pattern"})

	uploadFrames := 0
	totalUploaded := 0
	stepUntilDone(t, u, 500, func(frame int, stats render3d.UploadFrameStats) {
		if stats.BytesUploaded > budget {
			t.Fatalf("frame %d uploaded %d bytes, exceeds budget %d", frame, stats.BytesUploaded, budget)
		}
		if stats.BytesUploaded > 0 {
			uploadFrames++
			totalUploaded += stats.BytesUploaded
		}
	})

	if meshErr != nil || streamMesh == nil {
		t.Fatalf("streamed mesh failed: mesh=%v err=%v", streamMesh, meshErr)
	}
	if texErr != nil || streamTex == nil {
		t.Fatalf("streamed texture failed: tex=%v err=%v", streamTex, texErr)
	}
	if hMesh.State() != render3d.UploadReady || hTex.State() != render3d.UploadReady {
		t.Fatalf("states = (%v, %v), want Ready", hMesh.State(), hTex.State())
	}
	wantBytes := mesh.VertexDataSize() + mesh.IndexDataSize() + 512*512*4
	if totalUploaded != wantBytes {
		t.Fatalf("totalUploaded = %d, want %d", totalUploaded, wantBytes)
	}
	minFrames := wantBytes / budget
	if uploadFrames < minFrames {
		t.Fatalf("uploadFrames = %d, want >= %d", uploadFrames, minFrames)
	}
	if up, tot := hMesh.Progress(); up != tot || tot != int64(mesh.VertexDataSize()+mesh.IndexDataSize()) {
		t.Fatalf("mesh progress = %d/%d, want %d", up, tot, mesh.VertexDataSize()+mesh.IndexDataSize())
	}
	if up, tot := hTex.Progress(); up != tot || tot != 512*512*4 {
		t.Fatalf("tex progress = %d/%d, want %d", up, tot, 512*512*4)
	}

	// Render streamed assets and compare pixel-for-pixel with synchronous upload.
	streamRender := renderTexturedMesh(t, devStream, srStream, streamMesh, streamTex.Texture())

	srRef := gpu.NewSoftwareRenderer(64, 64, gpu.FormatUndefined)
	t.Cleanup(srRef.Close)
	devRef := srRef.Device()
	refMesh, err := render3d.UploadMesh(nil, devRef, mesh, render3d.MeshUpload{Label: "ref"})
	if err != nil {
		t.Fatalf("UploadMesh ref: %v", err)
	}
	refTex, err := render3d.UploadTexture(nil, devRef, texImg, render3d.TextureUpload{Label: "pattern"})
	if err != nil {
		t.Fatalf("UploadTexture ref: %v", err)
	}
	refRender := renderTexturedMesh(t, devRef, srRef, refMesh, refTex.Texture())

	if !bytes.Equal(streamRender.Pix, refRender.Pix) {
		t.Fatalf("streamed render differs from synchronous reference render")
	}
}

func TestUploaderStreamsInGPUView(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			const (
				viewSize = 64
				budget   = 1024
			)
			texImg := image.NewRGBA(image.Rect(0, 0, 32, 32))
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					texImg.SetRGBA(x, y, color.RGBA{R: 40, G: 210, B: 90, A: 255})
				}
			}

			var (
				uploader  *render3d.Uploader
				mesh      *render3d.GPUMesh
				tex       *render3d.GPUTexture
				pipe      *gpu.Pipeline
				bgl       *gpu.BindGroupLayout
				samp      *gpu.Sampler
				bg        *gpu.BindGroup
				maxUpload int
				steps     int
			)
			opts := append([]nsgotest.Option{nsgotest.Size(viewSize, viewSize), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "viewport",
					Style:       core.Style{Width: core.Pt(viewSize), Height: core.Pt(viewSize)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						mod, err := gpu.CompileWGSL(texturedMeshWGSL)
						if err != nil {
							t.Fatalf("CompileWGSL: %v", err)
						}
						bgl, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
							Entries: []gpu.BindGroupLayoutEntry{
								{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
								{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
							},
						})
						if err != nil {
							t.Fatalf("CreateBindGroupLayoutDesc: %v", err)
						}
						samp, err = dev.CreateSampler(cx, gpu.SamplerDesc{
							MinFilter: gpu.FilterNearest,
							MagFilter: gpu.FilterNearest,
						})
						if err != nil {
							t.Fatalf("CreateSampler: %v", err)
						}
						pipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
							Layouts:       []*gpu.BindGroupLayout{bgl},
							Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
							Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
							VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
							Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
							DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
							Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
						})
						if err != nil {
							t.Fatalf("CreatePipeline: %v", err)
						}

						uploader = render3d.NewUploader(cx, dev, render3d.UploaderOptions{
							BytesPerFrame: budget,
							ChunkBytes:    512,
							Workers:       2,
						})
						uploader.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
							return quadMesh(), nil
						}, func(g *render3d.GPUMesh, err error) {
							if err != nil {
								t.Errorf("QueueMesh: %v", err)
								return
							}
							mesh = g
						}, render3d.MeshUpload{Label: "quad", Retained: true})

						uploader.QueueTexture(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (image.Image, error) {
							return texImg, nil
						}, func(tx *render3d.GPUTexture, err error) {
							if err != nil {
								t.Errorf("QueueTexture: %v", err)
								return
							}
							tex = tx
							bg, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
								Layout: bgl,
								Entries: []gpu.BindGroupEntry{
									{Binding: 0, Texture: tex.Texture()},
									{Binding: 1, Sampler: samp},
								},
							})
							if err != nil {
								t.Errorf("CreateBindGroupDesc: %v", err)
							}
						}, render3d.TextureUpload{Label: "green", Retained: true})
					},
					OnFrame: func(frame *gpu.Frame) {
						stats := uploader.Step()
						steps++
						if stats.BytesUploaded > maxUpload {
							maxUpload = stats.BytesUploaded
						}
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x10141c))
						pass.ClearDepth(1)
						if mesh != nil && bg != nil {
							pass.SetPipeline(pipe)
							pass.SetBindGroup(0, bg)
							mesh.Draw(pass)
						}
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			for i := 0; i < 60 && (mesh == nil || bg == nil); i++ {
				app.Advance(16 * time.Millisecond)
				if mesh == nil || bg == nil {
					time.Sleep(time.Millisecond)
				}
			}
			if mesh == nil || bg == nil {
				t.Fatalf("assets did not finish streaming after %d steps", steps)
			}
			// Draw one frame after both resources are ready.
			app.Advance(16 * time.Millisecond)

			if maxUpload > budget {
				t.Fatalf("maxUpload = %d, exceeds budget %d", maxUpload, budget)
			}
			img, err := app.GPUReadPixels("id:viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			wantPixel(t, img, 2, 2, meshClear)
			wantPixel(t, img, 32, 32, color.RGBA{R: 40, G: 210, B: 90, A: 255})
		})
	}
}

func TestUploaderLargeModelBytePacing(t *testing.T) {
	// A 316x316 grid has 100,489 vertices: a 3.07 MiB vertex buffer and a 2.29 MiB uint32 index buffer, plus 1.33 MiB for a 512x512 texture with its mip chain, about 6.7 MiB streamed at 128 KiB per frame.
	mesh := makeGridMesh(316, 316)
	texImg := makePatternRGBA(512, 512)
	const budget = 128 * 1024

	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{
		BytesPerFrame: budget,
		ChunkBytes:    64 * 1024,
		Workers:       2,
	})
	t.Cleanup(func() { _ = u.Close() })

	var (
		gotMesh *render3d.GPUMesh
		gotTex  *render3d.GPUTexture
	)
	u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		return mesh, nil
	}, func(g *render3d.GPUMesh, err error) {
		if err != nil {
			t.Errorf("mesh error: %v", err)
		}
		gotMesh = g
	}, render3d.MeshUpload{Label: "large-model"})

	u.QueueTexture(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (image.Image, error) {
		return texImg, nil
	}, func(tx *render3d.GPUTexture, err error) {
		if err != nil {
			t.Errorf("tex error: %v", err)
		}
		gotTex = tx
	}, render3d.TextureUpload{Label: "large-tex", Mipmaps: true})

	var (
		activeFrames int
		totalBytes   int
	)
	stepUntilDone(t, u, 500, func(frame int, stats render3d.UploadFrameStats) {
		if stats.BytesUploaded > budget {
			t.Fatalf("frame %d uploaded %d bytes > budget %d", frame, stats.BytesUploaded, budget)
		}
		if stats.BytesUploaded > 0 {
			activeFrames++
			totalBytes += stats.BytesUploaded
		}
	})

	if gotMesh == nil || !gotMesh.Valid() {
		t.Fatalf("gotMesh invalid: %v", gotMesh)
	}
	if gotTex == nil || !gotTex.Valid() {
		t.Fatalf("gotTex invalid: %v", gotTex)
	}
	// The per-frame byte cap checked above is what bounds each frame's upload work; wall-clock time is not asserted, as it depends on the machine.
	wantTotal := mesh.VertexDataSize() + mesh.IndexDataSize() + int(gotTex.ByteSize())
	if totalBytes != wantTotal {
		t.Fatalf("uploaded %d bytes, want %d", totalBytes, wantTotal)
	}
	if minFrames := (wantTotal + budget - 1) / budget; activeFrames < minFrames {
		t.Fatalf("uploaded in %d frames, want at least %d for %d bytes at %d bytes/frame", activeFrames, minFrames, wantTotal, budget)
	}

	// Verify mesh metadata, uint32 index selection (>65,535 vertices), and GPU rendering.
	if gotMesh.VertexCount() != uint32(len(mesh.Positions)) {
		t.Fatalf("VertexCount = %d, want %d", gotMesh.VertexCount(), len(mesh.Positions))
	}
	if gotMesh.IndexCount() != uint32(len(mesh.Indices)) {
		t.Fatalf("IndexCount = %d, want %d", gotMesh.IndexCount(), len(mesh.Indices))
	}
	if gotMesh.IndexFormat() != gpu.IndexUint32 {
		t.Fatalf("IndexFormat = %v, want IndexUint32", gotMesh.IndexFormat())
	}
	wantMeshBytes := uint64(mesh.VertexDataSize() + mesh.IndexDataSize())
	if gotMesh.ByteSize() != wantMeshBytes {
		t.Fatalf("ByteSize = %d, want %d", gotMesh.ByteSize(), wantMeshBytes)
	}
	if gotMesh.Bounds() != mesh.Bounds() {
		t.Fatalf("Bounds = %+v, want %+v", gotMesh.Bounds(), mesh.Bounds())
	}
	rendered := renderTexturedMesh(t, dev, sr, gotMesh, gotTex.Texture())
	center := rendered.RGBAAt(16, 16)
	if center == meshClear || center.A != 255 {
		t.Fatalf("expected large streamed mesh to draw at center, got %+v", center)
	}
	// Every mip level matches a synchronous upload of the same image.
	want, err := render3d.UploadTexture(nil, dev, texImg, render3d.TextureUpload{Mipmaps: true})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	assertSameTexture(t, dev, sr, gotTex, want)
}

// assertSameTexture checks that got has the format, size and mip count of want and the same texels in every mip level.
func assertSameTexture(t *testing.T, dev *gpu.Device, sr *gpu.SoftwareRenderer, got, want *render3d.GPUTexture) {
	t.Helper()
	if got.Format() != want.Format() || got.Width() != want.Width() || got.Height() != want.Height() || got.MipLevels() != want.MipLevels() || got.ByteSize() != want.ByteSize() {
		t.Fatalf("texture = %s %dx%d with %d levels (%d bytes), want %s %dx%d with %d levels (%d bytes)", got.Format(), got.Width(), got.Height(), got.MipLevels(), got.ByteSize(), want.Format(), want.Width(), want.Height(), want.MipLevels(), want.ByteSize())
	}
	if got.Texture().Desc().Label != want.Texture().Desc().Label {
		t.Fatalf("label = %q, want %q", got.Texture().Desc().Label, want.Texture().Desc().Label)
	}
	for mip := range want.MipLevels() {
		if g, w := readTextureSync(t, dev, sr, got.Texture(), mip), readTextureSync(t, dev, sr, want.Texture(), mip); !bytes.Equal(g, w) {
			t.Fatalf("mip %d differs from the synchronous upload", mip)
		}
	}
}

// TestUploaderTextureMatchesUploadTexture streams the images whose conversion used to differ from UploadTexture (a translucent *image.RGBA, a 16-bit image, a paletted image with transparency, an sRGB mip chain) and compares every mip level with UploadTexture's byte for byte.
func TestUploaderTextureMatchesUploadTexture(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{BytesPerFrame: 1024, ChunkBytes: 512, Workers: 2})
	t.Cleanup(func() { _ = u.Close() })

	translucent := image.NewRGBA(image.Rect(0, 0, 16, 16))
	wide := image.NewRGBA64(image.Rect(0, 0, 16, 16))
	pal := image.NewPaletted(image.Rect(0, 0, 16, 16), color.Palette{color.RGBA{}, color.RGBA{R: 60, G: 30, B: 10, A: 128}, color.RGBA{R: 200, G: 100, B: 50, A: 255}})
	for y := range 16 {
		for x := range 16 {
			a := uint8(x * 16)
			// Premultiplied: each color channel is at most alpha.
			translucent.SetRGBA(x, y, color.RGBA{R: a / 2, G: a / 3, B: uint8(int(a) * y / 16), A: a})
			wide.SetRGBA64(x, y, color.RGBA64{R: uint16(a) << 7, G: uint16(y) << 10, B: 0x1234, A: uint16(a)<<8 | 0xff})
			pal.SetColorIndex(x, y, uint8((x+y)%3))
		}
	}
	srgbPattern := makePatternRGBA(64, 32)
	cases := []struct {
		name string
		img  image.Image
		opts render3d.TextureUpload
	}{
		{"translucent rgba", translucent, render3d.TextureUpload{}},
		{"rgba64", wide, render3d.TextureUpload{Label: "wide"}},
		{"paletted", pal, render3d.TextureUpload{Mipmaps: true}},
		{"srgb mipmapped", srgbPattern, render3d.TextureUpload{Label: "albedo", SRGB: true, Mipmaps: true}},
		{"srgb capped mips", translucent, render3d.TextureUpload{SRGB: true, Mipmaps: true, MaxMipLevels: 2}},
	}
	got := make([]*render3d.GPUTexture, len(cases))
	for i, tc := range cases {
		u.QueueTexture(context.Background(), render3d.PriorityNormal, func(context.Context) (image.Image, error) {
			return tc.img, nil
		}, func(tex *render3d.GPUTexture, err error) {
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
			got[i] = tex
		}, tc.opts)
	}
	stepUntilDone(t, u, 500, nil)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, err := render3d.UploadTexture(nil, dev, tc.img, tc.opts)
			if err != nil {
				t.Fatalf("UploadTexture: %v", err)
			}
			if got[i] == nil {
				t.Fatal("no streamed texture")
			}
			assertSameTexture(t, dev, sr, got[i], want)
		})
	}
}

func TestUploaderSubRowTextureBudget(t *testing.T) {
	// 256x4 RGBA8 = 1024 bytes per row, 4096 bytes total. Budget is 256 bytes/frame (a quarter of ONE row!), exercising horizontal sub-row splitting.
	const budget = 256
	img := makePatternRGBA(256, 4)

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{
		BytesPerFrame: budget,
		ChunkBytes:    budget,
		Workers:       1,
	})
	t.Cleanup(func() { _ = u.Close() })

	var gotTex *render3d.GPUTexture
	u.QueueTexture(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (image.Image, error) {
		return img, nil
	}, func(tx *render3d.GPUTexture, err error) {
		if err != nil {
			t.Errorf("QueueTexture: %v", err)
		}
		gotTex = tx
	}, render3d.TextureUpload{})

	uploadFrames := 0
	stepUntilDone(t, u, 100, func(frame int, stats render3d.UploadFrameStats) {
		if stats.BytesUploaded > budget {
			t.Fatalf("frame %d uploaded %d > budget %d", frame, stats.BytesUploaded, budget)
		}
		if stats.BytesUploaded > 0 {
			uploadFrames++
		}
	})

	if uploadFrames != 16 {
		t.Fatalf("uploadFrames = %d, want 16 (4096 / 256)", uploadFrames)
	}
	gotPixels := readTextureSync(t, dev, sr, gotTex.Texture(), 0)
	if !bytes.Equal(gotPixels, img.Pix) {
		t.Fatalf("sub-row streamed texture pixels do not match source")
	}
}

func TestUploaderTextureFormatsAndRawUploads(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{
		BytesPerFrame: 512,
		ChunkBytes:    256,
		Workers:       2,
	})
	t.Cleanup(func() { _ = u.Close() })

	// 1. NRGBA with a non-zero origin uploads as linear RGBA8.
	nrgba := image.NewNRGBA(image.Rect(10, 20, 26, 36))
	for y := 20; y < 36; y++ {
		for x := 10; x < 26; x++ {
			nrgba.SetNRGBA(x, y, color.NRGBA{R: 120, G: 60, B: 200, A: 180})
		}
	}
	var gotDefault *render3d.GPUTexture
	u.QueueTexture(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (image.Image, error) {
		return nrgba, nil
	}, func(tx *render3d.GPUTexture, err error) {
		if err != nil {
			t.Errorf("NRGBA default format: %v", err)
		}
		gotDefault = tx
	}, render3d.TextureUpload{})

	// 2. R8 and padded RG8 pixel buffers keep their formats.
	r8 := render3d.PixelBuffer{Width: 8, Height: 8, Format: gpu.FormatR8Unorm, Pixels: bytes.Repeat([]byte{173}, 64)}
	rg8 := render3d.PixelBuffer{Width: 4, Height: 4, Stride: 12, Format: gpu.FormatRG8Unorm, Pixels: make([]byte, 12*4)}
	for y := range 4 {
		for x := range 4 {
			rg8.Pixels[y*12+x*2] = byte(10 + x)
			rg8.Pixels[y*12+x*2+1] = byte(20 + y)
		}
	}
	var gotR8, gotRG8 *render3d.GPUTexture
	u.QueueTexturePixels(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.PixelBuffer, error) {
		return r8, nil
	}, func(tx *render3d.GPUTexture, err error) {
		if err != nil {
			t.Errorf("R8: %v", err)
		}
		gotR8 = tx
	}, render3d.TextureUpload{})
	u.QueueTexturePixels(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.PixelBuffer, error) {
		return rg8, nil
	}, func(tx *render3d.GPUTexture, err error) {
		if err != nil {
			t.Errorf("RG8: %v", err)
		}
		gotRG8 = tx
	}, render3d.TextureUpload{Mipmaps: true})

	// 3. QueueRawBuffer.
	rawPayload := make([]byte, 1500)
	for i := range rawPayload {
		rawPayload[i] = byte((i * 31) & 0xff)
	}
	var gotBuf *gpu.Buffer
	u.QueueRawBuffer(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.RawBufferUpload, error) {
		return render3d.RawBufferUpload{
			Label: "raw-storage",
			Usage: gpu.UsageVertex | gpu.UsageCopySrc,
			Data:  rawPayload,
		}, nil
	}, func(b *gpu.Buffer, err error) {
		if err != nil {
			t.Errorf("QueueRawBuffer: %v", err)
		}
		gotBuf = b
	})

	// 4. QueueRawTexture with padded BytesPerRow repacking.
	const rw, rh = 8, 8
	const paddedStride = 48 // 32 tight + 16 padding bytes per row
	padded := make([]byte, paddedStride*rh)
	wantRawTex := make([]byte, rw*rh*4)
	for y := 0; y < rh; y++ {
		for x := 0; x < rw; x++ {
			px := [4]byte{byte(x * 30), byte(y * 30), 99, 255}
			copy(padded[y*paddedStride+x*4:], px[:])
			copy(wantRawTex[(y*rw+x)*4:], px[:])
		}
	}
	var gotRawTex *gpu.Texture
	u.QueueRawTexture(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.RawTextureUpload, error) {
		return render3d.RawTextureUpload{
			Desc: gpu.TextureDesc{
				Label:     "raw-tex",
				Dimension: gpu.Dim2D,
				Width:     rw,
				Height:    rh,
				Format:    gpu.FormatRGBA8Unorm,
			},
			Mips: []render3d.TextureMipData{{
				MipLevel:    0,
				ArrayLayer:  0,
				Data:        padded,
				BytesPerRow: paddedStride,
			}},
		}, nil
	}, func(tx *gpu.Texture, err error) {
		if err != nil {
			t.Errorf("QueueRawTexture: %v", err)
		}
		gotRawTex = tx
	})

	stepUntilDone(t, u, 200, func(frame int, stats render3d.UploadFrameStats) {
		if stats.BytesUploaded > 512 {
			t.Fatalf("frame %d uploaded %d > 512", frame, stats.BytesUploaded)
		}
	})

	if gotDefault == nil || gotDefault.Format() != gpu.FormatRGBA8Unorm {
		t.Fatalf("default texture format = %v, want RGBA8Unorm", gotDefault)
	}
	pixDefault := readTextureSync(t, dev, sr, gotDefault.Texture(), 0)
	if pixDefault[0] != 120 || pixDefault[1] != 60 || pixDefault[2] != 200 || pixDefault[3] != 180 {
		t.Fatalf("NRGBA pixel[0] = %v, want [120 60 200 180]", pixDefault[:4])
	}
	if gotR8 == nil || gotR8.Format() != gpu.FormatR8Unorm {
		t.Fatalf("R8 texture invalid")
	}
	pixR8 := readTextureSync(t, dev, sr, gotR8.Texture(), 0)
	if pixR8[0] != 173 {
		t.Fatalf("R8 pixel[0].R = %d, want 173", pixR8[0])
	}
	if gotRG8 == nil || gotRG8.Format() != gpu.FormatRG8Unorm {
		t.Fatalf("RG8 texture invalid")
	}
	wantRG8, err := render3d.UploadTexturePixels(nil, dev, rg8, render3d.TextureUpload{Mipmaps: true})
	if err != nil {
		t.Fatalf("UploadTexturePixels: %v", err)
	}
	assertSameTexture(t, dev, sr, gotRG8, wantRG8)
	gotBufData := readBufferSync(t, dev, sr, gotBuf)
	if !bytes.Equal(gotBufData, rawPayload) {
		t.Fatalf("raw buffer bytes mismatch")
	}
	gotRawPix := readTextureSync(t, dev, sr, gotRawTex, 0)
	if !bytes.Equal(gotRawPix, wantRawTex) {
		t.Fatalf("raw padded texture readback mismatch")
	}
}

// readTextureLayerSync reads one mip level of one array layer or cube face of tex.
func readTextureLayerSync(t *testing.T, dev *gpu.Device, sr *gpu.SoftwareRenderer, tex *gpu.Texture, mip, layer uint32) []byte {
	t.Helper()
	d := tex.Desc()
	mw, mh := gpu.MipSize(d.Width, d.Height, mip)
	var got []byte
	var readErr error
	dev.ReadTexture(tex, gpu.TextureRegion{MipLevel: mip, ArrayLayer: layer, Width: mw, Height: mh}, func(rgba []byte, err error) {
		readErr = err
		got = append([]byte(nil), rgba...)
	})
	sr.WaitIdle()
	if readErr != nil {
		t.Fatalf("ReadTexture mip %d layer %d: %v", mip, layer, readErr)
	}
	return got
}

// TestUploaderRawCompressedAndCubeTextures streams a BC1 mip chain under a budget smaller than one mip level, so it is written in block-row bands, and a six-face cube map, and reads both back against textures written in one go.
func TestUploaderRawCompressedAndCubeTextures(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	if !dev.SupportsCompressedFormat(gpu.FormatBC1RGBAUnorm) {
		t.Fatal("software renderer should support BC1")
	}
	const budget = 48 // one 32-byte block row of the 16x16 base level per frame
	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{BytesPerFrame: budget, ChunkBytes: budget, Workers: 1})
	t.Cleanup(func() { _ = u.Close() })

	// BC1: 8 bytes per 4x4 block; any bytes are a valid block. Levels 16x16, 8x8, 4x4, 2x2 and 1x1.
	bcDesc := gpu.TextureDesc{Label: "bc1", Dimension: gpu.Dim2D, Width: 16, Height: 16, MipLevels: 5, Format: gpu.FormatBC1RGBAUnorm}
	var bcMips []render3d.TextureMipData
	seed := uint32(7)
	for lvl := range uint32(5) {
		mw, mh := gpu.MipSize(16, 16, lvl)
		blocks := int((mw+3)/4) * int((mh+3)/4)
		data := make([]byte, blocks*8)
		for i := range data {
			seed = seed*1664525 + 1013904223
			data[i] = byte(seed >> 24)
		}
		bcMips = append(bcMips, render3d.TextureMipData{MipLevel: lvl, Data: data})
	}

	// Cube: six 8x8 RGBA8 faces, each a different color; Layers 0 defaults to 6 for DimCube.
	cubeDesc := gpu.TextureDesc{Label: "cube", Dimension: gpu.DimCube, Width: 8, Height: 8, Format: gpu.FormatRGBA8Unorm}
	var cubeFaces []render3d.TextureMipData
	for face := range uint32(6) {
		data := make([]byte, 8*8*4)
		for i := 0; i < len(data); i += 4 {
			copy(data[i:], []byte{byte(40 * face), byte(255 - 40*face), byte(i / 4), 255})
		}
		cubeFaces = append(cubeFaces, render3d.TextureMipData{ArrayLayer: face, Data: data})
	}

	var gotBC, gotCube *gpu.Texture
	u.QueueRawTexture(context.Background(), render3d.PriorityNormal, func(context.Context) (render3d.RawTextureUpload, error) {
		return render3d.RawTextureUpload{Desc: bcDesc, Mips: bcMips}, nil
	}, func(tex *gpu.Texture, err error) {
		if err != nil {
			t.Errorf("BC1: %v", err)
		}
		gotBC = tex
	})
	u.QueueRawTexture(context.Background(), render3d.PriorityNormal, func(context.Context) (render3d.RawTextureUpload, error) {
		return render3d.RawTextureUpload{Desc: cubeDesc, Mips: cubeFaces}, nil
	}, func(tex *gpu.Texture, err error) {
		if err != nil {
			t.Errorf("cube: %v", err)
		}
		gotCube = tex
	})
	stepUntilDone(t, u, 500, func(frame int, stats render3d.UploadFrameStats) {
		if stats.BytesUploaded > budget {
			t.Fatalf("frame %d uploaded %d bytes, budget %d", frame, stats.BytesUploaded, budget)
		}
	})
	if gotBC == nil || gotCube == nil {
		t.Fatalf("textures not delivered: bc1=%v cube=%v", gotBC, gotCube)
	}
	if d := gotCube.Desc(); d.Layers != 6 {
		t.Fatalf("cube layers = %d, want 6", d.Layers)
	}

	bcDesc.Usage = gpu.TextureSampled | gpu.TextureCopyDst
	wantBC, err := dev.CreateTexture(nil, bcDesc)
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	for _, m := range bcMips {
		mw, _ := gpu.MipSize(16, 16, m.MipLevel)
		dev.WriteTextureMip(wantBC, m.MipLevel, 0, m.Data, (mw+3)/4*8)
	}
	for lvl := range uint32(5) {
		if g, w := readTextureLayerSync(t, dev, sr, gotBC, lvl, 0), readTextureLayerSync(t, dev, sr, wantBC, lvl, 0); !bytes.Equal(g, w) {
			t.Fatalf("BC1 mip %d differs from a single write", lvl)
		}
	}
	for face, m := range cubeFaces {
		if got := readTextureLayerSync(t, dev, sr, gotCube, 0, uint32(face)); !bytes.Equal(got, m.Data) {
			t.Fatalf("cube face %d differs from its source", face)
		}
	}
}

func TestUploaderPriorityOrderingAndPreemption(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	// Single worker + small budget so decode order and upload preemption are deterministic.
	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{
		BytesPerFrame: 1024,
		ChunkBytes:    1024,
		Workers:       1,
	})
	t.Cleanup(func() { _ = u.Close() })

	// Occupy the single worker with a blocker job while we enqueue Low, Normal, High.
	blockerStarted := make(chan struct{})
	releaseBlocker := make(chan struct{})
	u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		close(blockerStarted)
		<-releaseBlocker
		return quadMesh(), nil
	}, nil, render3d.MeshUpload{})
	<-blockerStarted

	var (
		mu          sync.Mutex
		decodeOrder []string
		readyOrder  []string
	)
	recordDecode := func(name string) func(context.Context) (render3d.Mesh, error) {
		return func(ctx context.Context) (render3d.Mesh, error) {
			mu.Lock()
			decodeOrder = append(decodeOrder, name)
			mu.Unlock()
			return makeGridMesh(8, 8), nil
		}
	}
	recordReady := func(name string) func(*render3d.GPUMesh, error) {
		return func(g *render3d.GPUMesh, err error) {
			if err != nil {
				t.Errorf("%s failed: %v", name, err)
			}
			mu.Lock()
			readyOrder = append(readyOrder, name)
			mu.Unlock()
		}
	}

	u.QueueMesh(context.Background(), render3d.PriorityLow, recordDecode("low"), recordReady("low"), render3d.MeshUpload{})
	u.QueueMesh(context.Background(), render3d.PriorityNormal, recordDecode("normal-1"), recordReady("normal-1"), render3d.MeshUpload{})
	u.QueueMesh(context.Background(), render3d.PriorityHigh, recordDecode("high"), recordReady("high"), render3d.MeshUpload{})
	u.QueueMesh(context.Background(), render3d.PriorityNormal, recordDecode("normal-2"), recordReady("normal-2"), render3d.MeshUpload{})

	close(releaseBlocker)
	stepUntilDone(t, u, 200, nil)

	want := []string{"high", "normal-1", "normal-2", "low"}
	mu.Lock()
	defer mu.Unlock()
	if len(decodeOrder) != len(want) || len(readyOrder) != len(want) {
		t.Fatalf("decodeOrder=%v readyOrder=%v, want %v", decodeOrder, readyOrder, want)
	}
	for i := range want {
		if decodeOrder[i] != want[i] {
			t.Fatalf("decodeOrder = %v, want %v", decodeOrder, want)
		}
		if readyOrder[i] != want[i] {
			t.Fatalf("readyOrder = %v, want %v", readyOrder, want)
		}
	}
}

func TestUploaderUploadPhasePreemption(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{
		BytesPerFrame: 2048,
		ChunkBytes:    2048,
		Workers:       2,
	})
	t.Cleanup(func() { _ = u.Close() })

	// Queue a large PriorityLow mesh (~30 KiB) and upload 1 chunk so it's mid-upload.
	largeLow := makeGridMesh(30, 30)
	var readyOrder []string
	hLow := u.QueueMesh(context.Background(), render3d.PriorityLow, func(ctx context.Context) (render3d.Mesh, error) {
		return largeLow, nil
	}, func(g *render3d.GPUMesh, err error) {
		if err != nil {
			t.Errorf("low failed: %v", err)
		}
		readyOrder = append(readyOrder, "low")
	}, render3d.MeshUpload{})

	// Step until low has started uploading (1 frame of bytes written) but not finished.
	for i := 0; i < 100; i++ {
		stats := u.Step()
		if stats.BytesUploaded > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if hLow.State() != render3d.UploadUploading {
		t.Fatalf("expected low to be UploadUploading, got %v", hLow.State())
	}

	// Now queue a small PriorityHigh mesh; wait briefly for its decode to finish, then Step.
	highDecoded := make(chan struct{})
	u.QueueMesh(context.Background(), render3d.PriorityHigh, func(ctx context.Context) (render3d.Mesh, error) {
		defer close(highDecoded)
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		if err != nil {
			t.Errorf("high failed: %v", err)
		}
		readyOrder = append(readyOrder, "high")
	}, render3d.MeshUpload{})
	<-highDecoded
	// The worker pushes high onto the upload queue under the same lock that drops InFlightDecodes back to zero, so once it is zero high is ready to preempt low.
	waitFor(t, func() bool { return u.InFlightDecodes() == 0 })

	stepUntilDone(t, u, 200, nil)
	if len(readyOrder) != 2 || readyOrder[0] != "high" || readyOrder[1] != "low" {
		t.Fatalf("readyOrder = %v, want [high low]", readyOrder)
	}
}

func TestUploaderCancellation(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{
		BytesPerFrame: 1024,
		ChunkBytes:    1024,
		Workers:       1,
	})
	t.Cleanup(func() { _ = u.Close() })

	// 1. Cancel while decoding (decode waits on ctx.Done) AND cancel while still queued (decode never runs).
	decodeStarted := make(chan struct{})
	var queuedRan atomic.Bool
	var decodingErr, queuedErr error

	hDecoding := u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		close(decodeStarted)
		<-ctx.Done()
		return render3d.Mesh{}, ctx.Err()
	}, func(g *render3d.GPUMesh, err error) {
		if g != nil {
			t.Errorf("canceled decoding mesh should be nil")
		}
		decodingErr = err
	}, render3d.MeshUpload{})
	<-decodeStarted

	hQueued := u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		queuedRan.Store(true)
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		if g != nil {
			t.Errorf("canceled queued mesh should be nil")
		}
		queuedErr = err
	}, render3d.MeshUpload{})

	hQueued.Cancel()
	hDecoding.Cancel()
	// Idempotent second cancel.
	hDecoding.Cancel()

	stepUntilDone(t, u, 100, nil)
	if queuedRan.Load() {
		t.Fatalf("queued decode callback ran after Cancel")
	}
	if !errors.Is(queuedErr, render3d.ErrUploadCanceled) || hQueued.State() != render3d.UploadCanceled {
		t.Fatalf("queued cancel: state=%v err=%v", hQueued.State(), queuedErr)
	}
	if !errors.Is(decodingErr, render3d.ErrUploadCanceled) || hDecoding.State() != render3d.UploadCanceled {
		t.Fatalf("decoding cancel: state=%v err=%v", hDecoding.State(), decodingErr)
	}

	// 2. Cancel mid-upload (after at least 1 chunk uploaded to GPU).
	var midErr error
	hMid := u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		return makeGridMesh(20, 20), nil
	}, func(g *render3d.GPUMesh, err error) {
		if g != nil {
			t.Errorf("mid-upload canceled mesh should be nil")
		}
		midErr = err
	}, render3d.MeshUpload{})
	for i := 0; i < 100; i++ {
		st := u.Step()
		if st.BytesUploaded > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if hMid.State() != render3d.UploadUploading {
		t.Fatalf("expected UploadUploading before mid-upload cancel, got %v", hMid.State())
	}
	hMid.Cancel()
	stats := u.Step()
	if stats.Canceled != 1 || stats.Pending != 0 {
		t.Fatalf("mid-upload cancel step stats = %+v", stats)
	}
	if !errors.Is(midErr, render3d.ErrUploadCanceled) || hMid.State() != render3d.UploadCanceled {
		t.Fatalf("mid-upload cancel: state=%v err=%v", hMid.State(), midErr)
	}

	// 3. Caller context cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var ctxErr error
	hCtx := u.QueueMesh(ctx, render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		t.Errorf("pre-canceled context decode should not run")
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		ctxErr = err
	}, render3d.MeshUpload{})
	stepUntilDone(t, u, 20, nil)
	if !errors.Is(ctxErr, render3d.ErrUploadCanceled) || !errors.Is(ctxErr, context.Canceled) {
		t.Fatalf("pre-canceled ctx err = %v, want ErrUploadCanceled + context.Canceled", ctxErr)
	}
	if hCtx.State() != render3d.UploadCanceled {
		t.Fatalf("pre-canceled ctx state = %v, want Canceled", hCtx.State())
	}
}

func TestUploaderDecodeErrorsValidationAndMaxPending(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{
		BytesPerFrame: 4096,
		Workers:       1,
		MaxPending:    2,
	})
	t.Cleanup(func() { _ = u.Close() })

	sentinel := errors.New("disk read failed")
	var gotSentinel, gotInvalidMesh, gotInvalidTex, gotNilDecode error

	h1 := u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		return render3d.Mesh{}, sentinel
	}, func(g *render3d.GPUMesh, err error) {
		gotSentinel = err
	}, render3d.MeshUpload{})

	h2 := u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		return render3d.Mesh{Positions: []math3d.Vec3{{X: float32(math.NaN())}}}, nil
	}, func(g *render3d.GPUMesh, err error) {
		gotInvalidMesh = err
	}, render3d.MeshUpload{})

	// MaxPending is 2: a third simultaneous upload may fail immediately if neither finished yet. Use a dedicated blocked uploader to test MaxPending deterministically.
	stepUntilDone(t, u, 100, nil)
	if !errors.Is(gotSentinel, sentinel) || !errors.Is(h1.Err(), sentinel) || h1.State() != render3d.UploadFailed {
		t.Fatalf("sentinel error: callback=%v handle=%v state=%v", gotSentinel, h1.Err(), h1.State())
	}
	if !errors.Is(gotInvalidMesh, render3d.ErrInvalidMesh) || h2.State() != render3d.UploadFailed {
		t.Fatalf("invalid mesh: err=%v state=%v", gotInvalidMesh, h2.State())
	}

	h3 := u.QueueTexture(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (image.Image, error) {
		return nil, nil
	}, func(tx *render3d.GPUTexture, err error) {
		gotInvalidTex = err
	}, render3d.TextureUpload{})
	stepUntilDone(t, u, 100, nil)
	if !errors.Is(gotInvalidTex, render3d.ErrInvalidTexture) || h3.State() != render3d.UploadFailed {
		t.Fatalf("invalid tex: err=%v state=%v", gotInvalidTex, h3.State())
	}

	var gotEmptyMips error
	hEmptyMips := u.QueueRawTexture(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.RawTextureUpload, error) {
		return render3d.RawTextureUpload{
			Desc: gpu.TextureDesc{
				Dimension: gpu.Dim2D,
				Width:     8,
				Height:    8,
				Format:    gpu.FormatRGBA8Unorm,
			},
		}, nil
	}, func(tx *gpu.Texture, err error) {
		gotEmptyMips = err
	})
	stepUntilDone(t, u, 100, nil)
	if !errors.Is(gotEmptyMips, render3d.ErrInvalidTexture) || hEmptyMips.State() != render3d.UploadFailed {
		t.Fatalf("empty raw texture mips: err=%v state=%v", gotEmptyMips, hEmptyMips.State())
	}

	hNil := u.QueueMesh(context.Background(), render3d.PriorityNormal, nil, func(g *render3d.GPUMesh, err error) {
		gotNilDecode = err
	}, render3d.MeshUpload{})
	if !errors.Is(gotNilDecode, render3d.ErrInvalidMesh) || hNil.State() != render3d.UploadFailed {
		t.Fatalf("nil decode: err=%v state=%v", gotNilDecode, hNil.State())
	}

	// Deterministic MaxPending test.
	uCap := render3d.NewUploader(nil, dev, render3d.UploaderOptions{
		Workers:    1,
		MaxPending: 1,
	})
	t.Cleanup(func() { _ = uCap.Close() })
	release := make(chan struct{})
	started := make(chan struct{})
	uCap.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		close(started)
		<-release
		return quadMesh(), nil
	}, nil, render3d.MeshUpload{})
	<-started

	var fullErr error
	hFull := uCap.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		fullErr = err
	}, render3d.MeshUpload{})
	if !errors.Is(fullErr, render3d.ErrUploadQueueFull) || !errors.Is(hFull.Err(), render3d.ErrUploadQueueFull) {
		t.Fatalf("MaxPending err: callback=%v handle=%v, want ErrUploadQueueFull", fullErr, hFull.Err())
	}
	close(release)
	stepUntilDone(t, uCap, 100, nil)
}

func TestUploaderRuntimeBudgetAdjustment(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{
		BytesPerFrame: 8 * 1024,
		ChunkBytes:    8 * 1024,
		Workers:       1,
	})
	t.Cleanup(func() { _ = u.Close() })

	if u.BytesPerFrame() != 8*1024 {
		t.Fatalf("BytesPerFrame = %d, want 8192", u.BytesPerFrame())
	}

	mesh := makeGridMesh(64, 64) // > 130 KiB vertices + indices
	decoded := make(chan struct{})
	u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		defer close(decoded)
		return mesh, nil
	}, nil, render3d.MeshUpload{})
	<-decoded

	// First 3 active frames at 8 KiB/frame.
	smallFrames := 0
	for i := 0; i < 50 && smallFrames < 3; i++ {
		st := u.Step()
		if st.BytesUploaded > 8*1024 {
			t.Fatalf("small phase uploaded %d > 8192", st.BytesUploaded)
		}
		if st.BytesUploaded > 0 {
			smallFrames++
		} else {
			time.Sleep(time.Millisecond)
		}
	}
	if smallFrames != 3 {
		t.Fatalf("expected 3 small frames, got %d", smallFrames)
	}

	// Raise budget to 64 KiB/frame mid-stream.
	u.SetBytesPerFrame(64 * 1024)
	u.SetChunkBytes(64 * 1024)
	if u.BytesPerFrame() != 64*1024 || u.ChunkBytes() != 64*1024 {
		t.Fatalf("updated budget/chunk = (%d, %d)", u.BytesPerFrame(), u.ChunkBytes())
	}

	sawRaised := false
	stepUntilDone(t, u, 100, func(frame int, stats render3d.UploadFrameStats) {
		if stats.BytesUploaded > 64*1024 {
			t.Fatalf("raised phase uploaded %d > 65536", stats.BytesUploaded)
		}
		if stats.BytesUploaded > 8*1024 {
			sawRaised = true
		}
	})
	if !sawRaised {
		t.Fatalf("expected frames after SetBytesPerFrame to upload > 8 KiB")
	}

	// Non-positive values reset to defaults.
	u.SetBytesPerFrame(0)
	u.SetChunkBytes(-1)
	if u.BytesPerFrame() != render3d.DefaultUploadBytesPerFrame || u.ChunkBytes() != render3d.DefaultUploadChunkBytes {
		t.Fatalf("defaults not restored: (%d, %d)", u.BytesPerFrame(), u.ChunkBytes())
	}
}

func TestUploaderNilDeviceAndDeviceLoss(t *testing.T) {
	// 1. Nil device fails cleanly without panicking.
	uNil := render3d.NewUploader(nil, nil, render3d.UploaderOptions{Workers: 1})
	t.Cleanup(func() { _ = uNil.Close() })
	var nilDevErr error
	uNil.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		nilDevErr = err
	}, render3d.MeshUpload{})
	stepUntilDone(t, uNil, 100, nil)
	var gpuErr gpu.Error
	if !errors.As(nilDevErr, &gpuErr) || gpuErr.Code != gpu.ErrDeviceLost {
		t.Fatalf("nil device err = %v, want gpu.ErrDeviceLost", nilDevErr)
	}

	// 2. Device loss mid-upload fails the in-progress upload cleanly while already-completed retained meshes survive restore.
	var (
		uploader     *render3d.Uploader
		retainedMesh *render3d.GPUMesh
		lostMeshErr  error
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(32), Height: core.Pt(32)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				if uploader == nil {
					uploader = render3d.NewUploader(cx, dev, render3d.UploaderOptions{
						BytesPerFrame: 2048,
						ChunkBytes:    2048,
						Workers:       1,
					})
					uploader.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
						return quadMesh(), nil
					}, func(g *render3d.GPUMesh, err error) {
						if err != nil {
							t.Errorf("retained mesh: %v", err)
						}
						retainedMesh = g
					}, render3d.MeshUpload{Label: "retained", Retained: true})
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				if uploader != nil {
					uploader.Step()
				}
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(32, 32), nsgotest.ManualFrames())

	// Wait for retainedMesh to complete.
	for i := 0; i < 40 && retainedMesh == nil; i++ {
		app.Advance(16 * time.Millisecond)
		if retainedMesh == nil {
			time.Sleep(time.Millisecond)
		}
	}
	if retainedMesh == nil || !retainedMesh.Valid() {
		t.Fatalf("retained mesh not ready before loss")
	}

	// Queue a large mesh and let it start uploading (only 1 frame), then lose the device.
	hBig := uploader.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		return makeGridMesh(30, 30), nil
	}, func(g *render3d.GPUMesh, err error) {
		if g != nil {
			t.Errorf("expected nil mesh after device loss")
		}
		lostMeshErr = err
	}, render3d.MeshUpload{})
	for i := 0; i < 40 && hBig.State() != render3d.UploadUploading; i++ {
		app.Advance(16 * time.Millisecond)
		if hBig.State() != render3d.UploadUploading {
			time.Sleep(time.Millisecond)
		}
	}
	if hBig.State() != render3d.UploadUploading {
		t.Fatalf("expected UploadUploading before loss, got %v", hBig.State())
	}

	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "simulated loss")
	// Step directly on the uploader while device is lost; it must fail cleanly with ErrDeviceLost.
	uploader.Step()
	if !errors.As(lostMeshErr, &gpuErr) || gpuErr.Code != gpu.ErrDeviceLost {
		t.Fatalf("mid-upload device loss err = %v, want gpu.ErrDeviceLost", lostMeshErr)
	}
	if hBig.State() != render3d.UploadFailed {
		t.Fatalf("state after device loss = %v, want Failed", hBig.State())
	}

	app.RestoreGPUDevice("id:viewport")
	if !retainedMesh.Valid() {
		t.Fatalf("already-completed retained mesh should be valid after device restore")
	}
}

func TestUploaderScopeDisposalAndZeroGoroutineLeaks(t *testing.T) {
	// Settle any transient runtime goroutines before measuring baseline.
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	baseGoroutines := runtime.NumGoroutine()

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	root := core.NewRoot(core.RootOptions{})
	u := render3d.NewUploader(root, dev, render3d.UploaderOptions{
		BytesPerFrame: 1024,
		Workers:       4,
	})

	// Queue blocked decodes to prove scope disposal cancels them and joins all workers.
	started := make(chan struct{}, 4)
	handles := make([]*render3d.UploadHandle, 6)
	var closedCalls atomic.Int32
	for i := range handles {
		handles[i] = u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return render3d.Mesh{}, ctx.Err()
		}, func(g *render3d.GPUMesh, err error) {
			if g != nil || !errors.Is(err, render3d.ErrUploaderClosed) {
				t.Errorf("onReady after close = (%v, %v), want (nil, ErrUploaderClosed)", g, err)
			}
			closedCalls.Add(1)
		}, render3d.MeshUpload{})
	}
	// Wait for at least one worker to enter decode.
	<-started

	// Disposing the scope must invoke u.Close(), cancel all decodes, and join all 4 workers.
	root.Dispose()

	// Every handle must be finalized and closed, and every onReady called once Dispose returns.
	if n := closedCalls.Load(); n != int32(len(handles)) {
		t.Fatalf("onReady called %d times after scope disposal, want %d", n, len(handles))
	}
	for i, h := range handles {
		select {
		case <-h.Done():
		case <-time.After(time.Second):
			t.Fatalf("handle %d Done() not closed after scope disposal", i)
		}
		if !errors.Is(h.Err(), render3d.ErrUploaderClosed) {
			t.Fatalf("handle %d err = %v, want ErrUploaderClosed", i, h.Err())
		}
	}

	// Queueing after close fails immediately with ErrUploaderClosed.
	var afterErr error
	hAfter := u.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		afterErr = err
	}, render3d.MeshUpload{})
	if !errors.Is(afterErr, render3d.ErrUploaderClosed) || !errors.Is(hAfter.Err(), render3d.ErrUploaderClosed) {
		t.Fatalf("after-close err = (%v, %v), want ErrUploaderClosed", afterErr, hAfter.Err())
	}

	// Close is idempotent.
	if err := u.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Creating an uploader on an already-disposed scope spawns zero workers.
	uDisposed := render3d.NewUploader(root, dev, render3d.UploaderOptions{Workers: 4})
	if h := uDisposed.QueueMesh(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.Mesh, error) {
		return quadMesh(), nil
	}, nil, render3d.MeshUpload{}); !errors.Is(h.Err(), render3d.ErrUploaderClosed) {
		t.Fatalf("disposed-scope uploader err = %v, want ErrUploaderClosed", h.Err())
	}

	// Verify zero leaked goroutines.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= baseGoroutines+1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: baseline=%d after=%d", baseGoroutines, runtime.NumGoroutine())
}

func TestUploaderConcurrentRace(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	u := render3d.NewUploader(nil, dev, render3d.UploaderOptions{
		BytesPerFrame: 8 * 1024,
		ChunkBytes:    2 * 1024,
		Workers:       4,
	})
	t.Cleanup(func() { _ = u.Close() })

	const producers = 4
	const perProducer = 12
	var (
		wg         sync.WaitGroup
		completed  atomic.Int32
		canceled   atomic.Int32
		smallMesh  = makeGridMesh(8, 8)
		smallImage = makePatternRGBA(16, 16)
	)

	wg.Add(producers)
	for p := 0; p < producers; p++ {
		go func(pid int) {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				if i%3 == 0 {
					u.SetBytesPerFrame((4 + (pid+i)%8) * 1024)
				}
				var h *render3d.UploadHandle
				switch i % 3 {
				case 0:
					h = u.QueueMesh(context.Background(), render3d.UploadPriority((i%3)-1), func(ctx context.Context) (render3d.Mesh, error) {
						return smallMesh, nil
					}, func(g *render3d.GPUMesh, err error) {
						if err == nil {
							completed.Add(1)
							_ = g.Close()
						} else if errors.Is(err, render3d.ErrUploadCanceled) {
							canceled.Add(1)
						}
					}, render3d.MeshUpload{})
				case 1:
					h = u.QueueTexture(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (image.Image, error) {
						return smallImage, nil
					}, func(tx *render3d.GPUTexture, err error) {
						if err == nil {
							completed.Add(1)
							_ = tx.Close()
						} else if errors.Is(err, render3d.ErrUploadCanceled) {
							canceled.Add(1)
						}
					}, render3d.TextureUpload{})
				case 2:
					h = u.QueueRawBuffer(context.Background(), render3d.PriorityNormal, func(ctx context.Context) (render3d.RawBufferUpload, error) {
						return render3d.RawBufferUpload{
							Usage: gpu.UsageVertex,
							Data:  make([]byte, 512),
						}, nil
					}, func(b *gpu.Buffer, err error) {
						if err == nil {
							completed.Add(1)
							_ = b.Close()
						} else if errors.Is(err, render3d.ErrUploadCanceled) {
							canceled.Add(1)
						}
					})
				}
				// Cancel a fraction of handles concurrently while also inspecting state/progress.
				_ = h.State()
				_, _ = h.Progress()
				_ = h.Err()
				if i%4 == 3 {
					h.Cancel()
				}
			}
		}(p)
	}

	doneProducing := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneProducing)
	}()

	// Drive Step() on the engine goroutine until all producers finish and pending == 0.
	for frames := 0; frames < 2000; frames++ {
		stats := u.Step()
		select {
		case <-doneProducing:
			if stats.Pending == 0 {
				goto finished
			}
		default:
		}
		if stats.BytesUploaded == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatalf("concurrent race test did not drain (pending=%d)", u.Pending())

finished:
	total := int(completed.Load() + canceled.Load())
	if total != producers*perProducer {
		t.Fatalf("completed(%d) + canceled(%d) = %d, want %d", completed.Load(), canceled.Load(), total, producers*perProducer)
	}
}
