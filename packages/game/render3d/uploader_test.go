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

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// texturedMeshWGSL samples a 2D texture at @group(0) @binding(0)/(1) using
// vertex UVs so rendered output verifies both vertex/index buffers and texture pixels.
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
	// Build a multi-megabyte mesh (256x256 grid = 66,049 vertices > 2.1 MiB vertex buffer
	// + 393,216 uint32 indices = 1.57 MiB index buffer -> ~3.69 MiB total) and a 512x512
	// RGBA8 texture (1 MiB) -> ~4.69 MiB total.
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
		streamTex  *gpu.Texture
		meshErr    error
		texErr     error
	)

	hMesh := u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return mesh, nil
	}, func(g *render3d.GPUMesh, err error) {
		streamMesh = g
		meshErr = err
	}, render3d.MeshUploadOptions{Label: "grid"})

	hTex := u.QueueTexture(context.Background(), func(ctx context.Context) (image.Image, gpu.TextureFormat, error) {
		return texImg, gpu.FormatRGBA8Unorm, nil
	}, func(tex *gpu.Texture, err error) {
		streamTex = tex
		texErr = err
	}, render3d.TextureUploadOptions{Label: "pattern"})

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
	streamRender := renderTexturedMesh(t, devStream, srStream, streamMesh, streamTex)

	srRef := gpu.NewSoftwareRenderer(64, 64, gpu.FormatUndefined)
	t.Cleanup(srRef.Close)
	devRef := srRef.Device()
	refMesh, err := render3d.UploadMesh(nil, devRef, mesh, render3d.MeshUpload{Label: "ref"})
	if err != nil {
		t.Fatalf("UploadMesh ref: %v", err)
	}
	refTex, err := devRef.CreateTexture(nil, gpu.TextureDesc{
		Label:     "ref",
		Dimension: gpu.Dim2D,
		Width:     512,
		Height:    512,
		MipLevels: 1,
		Format:    gpu.FormatRGBA8Unorm,
		Usage:     gpu.TextureSampled | gpu.TextureCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateTexture ref: %v", err)
	}
	devRef.WriteTextureRGBA(refTex, 0, 0, texImg)
	refRender := renderTexturedMesh(t, devRef, srRef, refMesh, refTex)

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
				tex       *gpu.Texture
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
						uploader.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
							return quadMesh(), nil
						}, func(g *render3d.GPUMesh, err error) {
							if err != nil {
								t.Errorf("QueueMesh: %v", err)
								return
							}
							mesh = g
						}, render3d.MeshUploadOptions{Label: "quad", Retained: true})

						uploader.QueueTexture(context.Background(), func(ctx context.Context) (image.Image, gpu.TextureFormat, error) {
							return texImg, gpu.FormatRGBA8Unorm, nil
						}, func(tx *gpu.Texture, err error) {
							if err != nil {
								t.Errorf("QueueTexture: %v", err)
								return
							}
							tex = tx
							bg, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
								Layout: bgl,
								Entries: []gpu.BindGroupEntry{
									{Binding: 0, Texture: tex},
									{Binding: 1, Sampler: samp},
								},
							})
							if err != nil {
								t.Errorf("CreateBindGroupDesc: %v", err)
							}
						}, render3d.TextureUploadOptions{Label: "green", Retained: true})
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

func TestUploaderLargeModelFrameTimePacing(t *testing.T) {
	// 100,489 vertices (316x316 grid):
	// Vertex buffer: 100,489 * 32 = 3,215,648 bytes (~3.07 MiB)
	// Index buffer (uint32): 316*316*6*4 = 2,396,544 bytes (~2.29 MiB)
	// Texture (512x512 + full mip chain): ~1.33 MiB
	// Total: ~6.69 MiB streamed at 128 KiB/frame (~54 frames).
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
		gotTex  *gpu.Texture
	)
	u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return mesh, nil
	}, func(g *render3d.GPUMesh, err error) {
		if err != nil {
			t.Errorf("mesh error: %v", err)
		}
		gotMesh = g
	}, render3d.MeshUploadOptions{Label: "large-model"})

	u.QueueTexture(context.Background(), func(ctx context.Context) (image.Image, gpu.TextureFormat, error) {
		return texImg, gpu.FormatRGBA8Unorm, nil
	}, func(tx *gpu.Texture, err error) {
		if err != nil {
			t.Errorf("tex error: %v", err)
		}
		gotTex = tx
	}, render3d.TextureUploadOptions{Label: "large-tex", GenerateMipmaps: true})

	var (
		activeFrames int
		maxBytes     int
		totalBytes   int
		maxDuration  time.Duration
	)
	stepUntilDone(t, u, 500, func(frame int, stats render3d.UploadFrameStats) {
		if stats.BytesUploaded > budget {
			t.Fatalf("frame %d uploaded %d bytes > budget %d", frame, stats.BytesUploaded, budget)
		}
		if stats.BytesUploaded > maxBytes {
			maxBytes = stats.BytesUploaded
		}
		if stats.BytesUploaded > 0 {
			activeFrames++
			totalBytes += stats.BytesUploaded
		}
		if stats.FrameDuration > maxDuration {
			maxDuration = stats.FrameDuration
		}
	})

	if gotMesh == nil || !gotMesh.Valid() {
		t.Fatalf("gotMesh invalid: %v", gotMesh)
	}
	if gotTex == nil || !gotTex.Valid() {
		t.Fatalf("gotTex invalid: %v", gotTex)
	}
	if activeFrames < 40 {
		t.Fatalf("expected >= 40 paced frames for ~6.7 MiB at 128 KiB/frame, got %d", activeFrames)
	}
	// Each 128 KiB chunked Step must never stall for a full frame spike (generous CI ceiling: 200ms).
	if maxDuration > 200*time.Millisecond {
		t.Fatalf("max Step duration %v indicates a frame-time spike", maxDuration)
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
	rendered := renderTexturedMesh(t, dev, sr, gotMesh, gotTex)
	center := rendered.RGBAAt(16, 16)
	if center == meshClear || center.A != 255 {
		t.Fatalf("expected large streamed mesh to draw at center, got %+v", center)
	}
	// Verify mip 0 and mip 1 texture pixels.
	gotMip0 := readTextureSync(t, dev, sr, gotTex, 0)
	if !bytes.Equal(gotMip0, texImg.Pix) {
		t.Fatalf("mip 0 pixel mismatch")
	}
	wantMip1 := gpu.DownsampleRGBA2x(texImg)
	gotMip1 := readTextureSync(t, dev, sr, gotTex, 1)
	if !bytes.Equal(gotMip1, wantMip1.Pix) {
		t.Fatalf("mip 1 pixel mismatch")
	}
}

func TestUploaderSubRowTextureBudget(t *testing.T) {
	// 256x4 RGBA8 = 1024 bytes per row, 4096 bytes total.
	// Budget is 256 bytes/frame (a quarter of ONE row!), exercising horizontal sub-row splitting.
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

	var gotTex *gpu.Texture
	u.QueueTexture(context.Background(), func(ctx context.Context) (image.Image, gpu.TextureFormat, error) {
		return img, gpu.FormatRGBA8Unorm, nil
	}, func(tx *gpu.Texture, err error) {
		if err != nil {
			t.Errorf("QueueTexture: %v", err)
		}
		gotTex = tx
	})

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
	gotPixels := readTextureSync(t, dev, sr, gotTex, 0)
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

	// 1. NRGBA with non-zero origin and FormatUndefined (defaults to RGBA8Unorm).
	nrgba := image.NewNRGBA(image.Rect(10, 20, 26, 36))
	for y := 20; y < 36; y++ {
		for x := 10; x < 26; x++ {
			nrgba.SetNRGBA(x, y, color.NRGBA{R: 120, G: 60, B: 200, A: 180})
		}
	}
	var gotDefault *gpu.Texture
	u.QueueTexture(context.Background(), func(ctx context.Context) (image.Image, gpu.TextureFormat, error) {
		return nrgba, gpu.FormatUndefined, nil
	}, func(tx *gpu.Texture, err error) {
		if err != nil {
			t.Errorf("NRGBA default format: %v", err)
		}
		gotDefault = tx
	})

	// 2. BGRA8, RG8, R8 formats + generic image.Image (image.Gray).
	gray := image.NewGray(image.Rect(0, 0, 8, 8))
	for i := range gray.Pix {
		gray.Pix[i] = 173
	}
	var gotR8, gotBGRA, gotRG8 *gpu.Texture
	u.QueueTexture(context.Background(), func(ctx context.Context) (image.Image, gpu.TextureFormat, error) {
		return gray, gpu.FormatR8Unorm, nil
	}, func(tx *gpu.Texture, err error) {
		if err != nil {
			t.Errorf("R8: %v", err)
		}
		gotR8 = tx
	})
	rgbaSmall := makePatternRGBA(8, 8)
	u.QueueTexture(context.Background(), func(ctx context.Context) (image.Image, gpu.TextureFormat, error) {
		return rgbaSmall, gpu.FormatBGRA8Unorm, nil
	}, func(tx *gpu.Texture, err error) {
		if err != nil {
			t.Errorf("BGRA8: %v", err)
		}
		gotBGRA = tx
	})
	u.QueueTexture(context.Background(), func(ctx context.Context) (image.Image, gpu.TextureFormat, error) {
		return rgbaSmall, gpu.FormatRG8Unorm, nil
	}, func(tx *gpu.Texture, err error) {
		if err != nil {
			t.Errorf("RG8: %v", err)
		}
		gotRG8 = tx
	})

	// 3. QueueRawBuffer.
	rawPayload := make([]byte, 1500)
	for i := range rawPayload {
		rawPayload[i] = byte((i * 31) & 0xff)
	}
	var gotBuf *gpu.Buffer
	u.QueueRawBuffer(context.Background(), func(ctx context.Context) (render3d.RawBufferUpload, error) {
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
	u.QueueRawTexture(context.Background(), func(ctx context.Context) (render3d.RawTextureUpload, error) {
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

	if gotDefault == nil || gotDefault.Desc().Format != gpu.FormatRGBA8Unorm {
		t.Fatalf("default texture format = %v, want RGBA8Unorm", gotDefault)
	}
	pixDefault := readTextureSync(t, dev, sr, gotDefault, 0)
	if pixDefault[0] != 120 || pixDefault[1] != 60 || pixDefault[2] != 200 || pixDefault[3] != 180 {
		t.Fatalf("NRGBA pixel[0] = %v, want [120 60 200 180]", pixDefault[:4])
	}
	if gotR8 == nil || gotR8.Desc().Format != gpu.FormatR8Unorm {
		t.Fatalf("R8 texture invalid")
	}
	pixR8 := readTextureSync(t, dev, sr, gotR8, 0)
	if pixR8[0] != 173 {
		t.Fatalf("R8 pixel[0].R = %d, want 173", pixR8[0])
	}
	if gotBGRA == nil || gotRG8 == nil {
		t.Fatalf("BGRA8 or RG8 texture nil")
	}
	pixBGRA := readTextureSync(t, dev, sr, gotBGRA, 0)
	if !bytes.Equal(pixBGRA, rgbaSmall.Pix) {
		t.Fatalf("BGRA8 readback mismatch")
	}
	gotBufData := readBufferSync(t, dev, sr, gotBuf)
	if !bytes.Equal(gotBufData, rawPayload) {
		t.Fatalf("raw buffer bytes mismatch")
	}
	gotRawPix := readTextureSync(t, dev, sr, gotRawTex, 0)
	if !bytes.Equal(gotRawPix, wantRawTex) {
		t.Fatalf("raw padded texture readback mismatch")
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
	u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		close(blockerStarted)
		<-releaseBlocker
		return quadMesh(), nil
	}, nil)
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

	u.QueueMesh(context.Background(), recordDecode("low"), recordReady("low"), render3d.MeshUploadOptions{Priority: render3d.PriorityLow})
	u.QueueMesh(context.Background(), recordDecode("normal-1"), recordReady("normal-1"), render3d.MeshUploadOptions{Priority: render3d.PriorityNormal})
	u.QueueMesh(context.Background(), recordDecode("high"), recordReady("high"), render3d.MeshUploadOptions{Priority: render3d.PriorityHigh})
	u.QueueMesh(context.Background(), recordDecode("normal-2"), recordReady("normal-2"), render3d.MeshUploadOptions{Priority: render3d.PriorityNormal})

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
	hLow := u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return largeLow, nil
	}, func(g *render3d.GPUMesh, err error) {
		if err != nil {
			t.Errorf("low failed: %v", err)
		}
		readyOrder = append(readyOrder, "low")
	}, render3d.MeshUploadOptions{Priority: render3d.PriorityLow})

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
	u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		defer close(highDecoded)
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		if err != nil {
			t.Errorf("high failed: %v", err)
		}
		readyOrder = append(readyOrder, "high")
	}, render3d.MeshUploadOptions{Priority: render3d.PriorityHigh})
	<-highDecoded
	// Let worker push high into uploadHeap before stepping.
	time.Sleep(2 * time.Millisecond)

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

	hDecoding := u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		close(decodeStarted)
		<-ctx.Done()
		return render3d.Mesh{}, ctx.Err()
	}, func(g *render3d.GPUMesh, err error) {
		if g != nil {
			t.Errorf("canceled decoding mesh should be nil")
		}
		decodingErr = err
	})
	<-decodeStarted

	hQueued := u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		queuedRan.Store(true)
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		if g != nil {
			t.Errorf("canceled queued mesh should be nil")
		}
		queuedErr = err
	})

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
	hMid := u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return makeGridMesh(20, 20), nil
	}, func(g *render3d.GPUMesh, err error) {
		if g != nil {
			t.Errorf("mid-upload canceled mesh should be nil")
		}
		midErr = err
	})
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
	hCtx := u.QueueMesh(ctx, func(ctx context.Context) (render3d.Mesh, error) {
		t.Errorf("pre-canceled context decode should not run")
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		ctxErr = err
	})
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

	h1 := u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return render3d.Mesh{}, sentinel
	}, func(g *render3d.GPUMesh, err error) {
		gotSentinel = err
	})

	h2 := u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return render3d.Mesh{Positions: []math3d.Vec3{{X: float32(math.NaN())}}}, nil
	}, func(g *render3d.GPUMesh, err error) {
		gotInvalidMesh = err
	})

	// MaxPending is 2: a third simultaneous upload may fail immediately if neither finished yet.
	// Use a dedicated blocked uploader to test MaxPending deterministically.
	stepUntilDone(t, u, 100, nil)
	if !errors.Is(gotSentinel, sentinel) || !errors.Is(h1.Err(), sentinel) || h1.State() != render3d.UploadFailed {
		t.Fatalf("sentinel error: callback=%v handle=%v state=%v", gotSentinel, h1.Err(), h1.State())
	}
	if !errors.Is(gotInvalidMesh, render3d.ErrInvalidMesh) || h2.State() != render3d.UploadFailed {
		t.Fatalf("invalid mesh: err=%v state=%v", gotInvalidMesh, h2.State())
	}

	h3 := u.QueueTexture(context.Background(), func(ctx context.Context) (image.Image, gpu.TextureFormat, error) {
		return nil, gpu.FormatRGBA8Unorm, nil
	}, func(tx *gpu.Texture, err error) {
		gotInvalidTex = err
	})
	stepUntilDone(t, u, 100, nil)
	if !errors.Is(gotInvalidTex, render3d.ErrInvalidTexture) || h3.State() != render3d.UploadFailed {
		t.Fatalf("invalid tex: err=%v state=%v", gotInvalidTex, h3.State())
	}

	var gotEmptyMips error
	hEmptyMips := u.QueueRawTexture(context.Background(), func(ctx context.Context) (render3d.RawTextureUpload, error) {
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

	hNil := u.QueueMesh(context.Background(), nil, func(g *render3d.GPUMesh, err error) {
		gotNilDecode = err
	})
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
	uCap.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		close(started)
		<-release
		return quadMesh(), nil
	}, nil)
	<-started

	var fullErr error
	hFull := uCap.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		fullErr = err
	})
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
	u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		defer close(decoded)
		return mesh, nil
	}, nil)
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
	uNil.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		nilDevErr = err
	})
	stepUntilDone(t, uNil, 100, nil)
	var gpuErr gpu.Error
	if !errors.As(nilDevErr, &gpuErr) || gpuErr.Code != gpu.ErrDeviceLost {
		t.Fatalf("nil device err = %v, want gpu.ErrDeviceLost", nilDevErr)
	}

	// 2. Device loss mid-upload fails the in-progress upload cleanly while
	// already-completed retained meshes survive restore.
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
					uploader.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
						return quadMesh(), nil
					}, func(g *render3d.GPUMesh, err error) {
						if err != nil {
							t.Errorf("retained mesh: %v", err)
						}
						retainedMesh = g
					}, render3d.MeshUploadOptions{Label: "retained", Retained: true})
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
	hBig := uploader.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return makeGridMesh(30, 30), nil
	}, func(g *render3d.GPUMesh, err error) {
		if g != nil {
			t.Errorf("expected nil mesh after device loss")
		}
		lostMeshErr = err
	})
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
	for i := range handles {
		handles[i] = u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return render3d.Mesh{}, ctx.Err()
		}, nil)
	}
	// Wait for at least one worker to enter decode.
	<-started

	// Disposing the scope must invoke u.Close(), cancel all decodes, and join all 4 workers.
	root.Dispose()

	// Every handle must be finalized and closed.
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
	hAfter := u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return quadMesh(), nil
	}, func(g *render3d.GPUMesh, err error) {
		afterErr = err
	})
	if !errors.Is(afterErr, render3d.ErrUploaderClosed) || !errors.Is(hAfter.Err(), render3d.ErrUploaderClosed) {
		t.Fatalf("after-close err = (%v, %v), want ErrUploaderClosed", afterErr, hAfter.Err())
	}

	// Close is idempotent.
	if err := u.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Creating an uploader on an already-disposed scope spawns zero workers.
	uDisposed := render3d.NewUploader(root, dev, render3d.UploaderOptions{Workers: 4})
	if h := uDisposed.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
		return quadMesh(), nil
	}, nil); !errors.Is(h.Err(), render3d.ErrUploaderClosed) {
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
					h = u.QueueMesh(context.Background(), func(ctx context.Context) (render3d.Mesh, error) {
						return smallMesh, nil
					}, func(g *render3d.GPUMesh, err error) {
						if err == nil {
							completed.Add(1)
							_ = g.Close()
						} else if errors.Is(err, render3d.ErrUploadCanceled) {
							canceled.Add(1)
						}
					}, render3d.MeshUploadOptions{Priority: render3d.UploadPriority((i % 3) - 1)})
				case 1:
					h = u.QueueTexture(context.Background(), func(ctx context.Context) (image.Image, gpu.TextureFormat, error) {
						return smallImage, gpu.FormatRGBA8Unorm, nil
					}, func(tx *gpu.Texture, err error) {
						if err == nil {
							completed.Add(1)
							_ = tx.Close()
						} else if errors.Is(err, render3d.ErrUploadCanceled) {
							canceled.Add(1)
						}
					})
				case 2:
					h = u.QueueRawBuffer(context.Background(), func(ctx context.Context) (render3d.RawBufferUpload, error) {
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
