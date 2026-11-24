package particles_test

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const testCameraUniformSize = 3*64 + 16

type testCameraGPU struct {
	buf    *gpu.Buffer
	layout *gpu.BindGroupLayout
	group  *gpu.BindGroup
}

func newTestCameraGPU(t *testing.T, cx *core.Scope, dev *gpu.Device, view, proj math3d.Mat4, eye math3d.Vec4, retained bool) *testCameraGPU {
	t.Helper()
	vp := proj.Mul(view)
	data := make([]byte, testCameraUniformSize)
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(data[off:], math.Float32bits(f))
		off += 4
	}
	for _, m := range [...]*math3d.Mat4{&view, &proj, &vp} {
		for _, f := range m {
			put(f)
		}
	}
	put(eye.X)
	put(eye.Y)
	put(eye.Z)
	put(eye.W)

	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    "test camera",
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     data,
		Retained: retained,
	})
	if err != nil {
		t.Fatalf("CreateBuffer camera: %v", err)
	}
	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "test camera layout",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageVertex | gpu.StageFragment,
			Type:       gpu.BindingUniform,
		}},
	})
	if err != nil {
		t.Fatalf("CreateBindGroupLayoutDesc: %v", err)
	}
	group, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:   "test camera group",
		Layout:  layout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: buf, Size: testCameraUniformSize}},
	})
	if err != nil {
		t.Fatalf("CreateBindGroupDesc: %v", err)
	}
	return &testCameraGPU{buf: buf, layout: layout, group: group}
}

func (c *testCameraGPU) close() {
	_ = c.group.Close()
	_ = c.layout.Close()
	_ = c.buf.Close()
}

func orthoTestCamera() (view, proj math3d.Mat4, eye, camRight, camUp, camPos, camFwd math3d.Vec3) {
	// Camera at (0, 0, 10) looking along -Z with orthographic projection [-2, 2] x [-2, 2], z in [0.1, 50].
	camPos = math3d.Vec3{Z: 10}
	camFwd = math3d.Vec3Forward()
	camRight = math3d.Vec3Right()
	camUp = math3d.Vec3Up()
	view = math3d.LookAt(camPos, math3d.Vec3Zero(), camUp)
	proj = math3d.Ortho(-2, 2, -2, 2, 0.1, 50)
	return view, proj, camPos, camRight, camUp, camPos, camFwd
}

func wantRGBA(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, tol uint8) {
	t.Helper()
	got := img.RGBAAt(x, y)
	closeCh := func(a, b uint8) bool { return max(a, b)-min(a, b) <= tol }
	if !closeCh(got.R, want.R) || !closeCh(got.G, want.G) || !closeCh(got.B, want.B) || !closeCh(got.A, want.A) {
		t.Errorf("pixel (%d,%d) = %+v, want %+v (tol %d)", x, y, got, want, tol)
	}
}

func TestGPUParticles3DSoftwareBlendingAndSorting(t *testing.T) {
	view, proj, eyePos, camRight, camUp, camPos, camFwd := orthoTestCamera()
	eye := eyePos.Vec4(1)

	t.Run("alpha_sorted_back_to_front", func(t *testing.T) {
		sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatDepth24Plus)
		t.Cleanup(sr.Close)
		dev := sr.Device()

		cam := newTestCameraGPU(t, nil, dev, view, proj, eye, false)
		t.Cleanup(cam.close)

		pipe, err := particles.NewParticlePipeline(nil, dev, particles.ParticlePipelineOptions{
			Label:        "alpha",
			CameraLayout: cam.layout,
			DepthFormat:  gpu.FormatDepth24Plus,
			Blend:        particles.BlendAlpha,
		})
		if err != nil {
			t.Fatalf("NewParticlePipeline: %v", err)
		}
		t.Cleanup(func() { _ = pipe.Close() })

		gp, err := particles.NewGPUParticles3D(nil, dev, particles.GPUParticles3DOptions{Capacity: 1})
		if err != nil {
			t.Fatalf("NewGPUParticles3D: %v", err)
		}
		t.Cleanup(func() { _ = gp.Close() })

		// Emit near semi-transparent blue first, then far opaque red second so unsorted order would be wrong.
		e := &particles.Emitter3D{
			MaxParticles: 4,
			LifetimeMin:  time.Hour,
			SizeMin:      2,
			SizeMax:      2,
			Color:        core.RGBA(0x0000ff, 0.5),
			Position:     math3d.Vec3{Z: 2}, // nearer to camera at z=10
		}
		e.Burst(1)
		e.Color = core.RGBA(0xff0000, 1)
		e.Position = math3d.Vec3{Z: -2} // farther from camera
		e.Burst(1)

		e.SortByDepth(camPos, camFwd)
		// Initial capacity was 1; uploading 2 particles exercises dynamic GPU buffer growth.
		if err := gp.Upload(e, camRight, camUp); err != nil {
			t.Fatalf("Upload: %v", err)
		}
		if gp.Count() != 2 || gp.Capacity() < 2 {
			t.Fatalf("count=%d capacity=%d, want 2 and >=2", gp.Count(), gp.Capacity())
		}

		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x000000),
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreStore,
				ClearDepth:   1,
			},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(0, cam.group)
		gp.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}

		img := sr.Image()
		// Corner is black clear color; center is 50% blue over 100% red -> ~(128, 0, 128).
		wantRGBA(t, img, 2, 2, color.RGBA{R: 0, G: 0, B: 0, A: 255}, 2)
		wantRGBA(t, img, 32, 32, color.RGBA{R: 128, G: 0, B: 128, A: 255}, 4)
	})

	t.Run("additive_blending", func(t *testing.T) {
		sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatUndefined)
		t.Cleanup(sr.Close)
		dev := sr.Device()

		cam := newTestCameraGPU(t, nil, dev, view, proj, eye, false)
		t.Cleanup(cam.close)

		pipe, err := particles.NewParticlePipeline(nil, dev, particles.ParticlePipelineOptions{
			Label:        "additive",
			CameraLayout: cam.layout,
			Blend:        particles.BlendAdditive,
		})
		if err != nil {
			t.Fatalf("NewParticlePipeline: %v", err)
		}
		t.Cleanup(func() { _ = pipe.Close() })

		gp, err := particles.NewGPUParticles3D(nil, dev, particles.GPUParticles3DOptions{Capacity: 4})
		if err != nil {
			t.Fatalf("NewGPUParticles3D: %v", err)
		}
		t.Cleanup(func() { _ = gp.Close() })

		// Red + green overlapping additive particles -> yellow (255, 255, 0) without sorting!
		e := &particles.Emitter3D{
			MaxParticles: 4,
			LifetimeMin:  time.Hour,
			SizeMin:      2,
			SizeMax:      2,
			Blend:        particles.BlendAdditive,
			Color:        core.RGBA(0xff0000, 1),
		}
		e.Burst(1)
		e.Color = core.RGBA(0x00ff00, 1)
		e.Burst(1)

		if err := gp.Upload(e, camRight, camUp); err != nil {
			t.Fatalf("Upload: %v", err)
		}
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x000000),
			}},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(0, cam.group)
		gp.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}

		wantRGBA(t, sr.Image(), 32, 32, color.RGBA{R: 255, G: 255, B: 0, A: 255}, 3)
	})

	t.Run("soft_circle_shader", func(t *testing.T) {
		sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatUndefined)
		t.Cleanup(sr.Close)
		dev := sr.Device()

		cam := newTestCameraGPU(t, nil, dev, view, proj, eye, false)
		t.Cleanup(cam.close)

		pipe, err := particles.NewParticlePipeline(nil, dev, particles.ParticlePipelineOptions{
			Label:        "soft",
			CameraLayout: cam.layout,
			Blend:        particles.BlendAlpha,
			SoftCircle:   true,
		})
		if err != nil {
			t.Fatalf("NewParticlePipeline: %v", err)
		}
		t.Cleanup(func() { _ = pipe.Close() })

		gp, err := particles.NewGPUParticles3D(nil, dev, particles.GPUParticles3DOptions{Capacity: 2})
		if err != nil {
			t.Fatalf("NewGPUParticles3D: %v", err)
		}
		t.Cleanup(func() { _ = gp.Close() })

		e := &particles.Emitter3D{
			MaxParticles: 2,
			LifetimeMin:  time.Hour,
			SizeMin:      4,
			SizeMax:      4,
			Color:        core.RGBA(0xffffff, 1),
		}
		e.Burst(1)
		if err := gp.Upload(e, camRight, camUp); err != nil {
			t.Fatalf("Upload: %v", err)
		}
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x000000),
			}},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(0, cam.group)
		gp.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}

		img := sr.Image()
		// Center should be near white; corner of the full-viewport quad is outside the unit circle -> black!
		wantRGBA(t, img, 32, 32, color.RGBA{R: 255, G: 255, B: 255, A: 255}, 8)
		wantRGBA(t, img, 1, 1, color.RGBA{R: 0, G: 0, B: 0, A: 255}, 3)
	})

	t.Run("textured_pipeline", func(t *testing.T) {
		sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatUndefined)
		t.Cleanup(sr.Close)
		dev := sr.Device()

		cam := newTestCameraGPU(t, nil, dev, view, proj, eye, false)
		t.Cleanup(cam.close)

		tex, err := dev.CreateTexture(nil, gpu.TextureDesc{
			Label:  "particle tex",
			Width:  2,
			Height: 2,
			Format: gpu.FormatRGBA8Unorm,
			Usage:  gpu.TextureSampled | gpu.TextureCopyDst,
		})
		if err != nil {
			t.Fatalf("CreateTexture: %v", err)
		}
		t.Cleanup(func() { _ = tex.Close() })
		rgba := image.NewRGBA(image.Rect(0, 0, 2, 2))
		for y := range 2 {
			for x := range 2 {
				rgba.SetRGBA(x, y, color.RGBA{R: 0, G: 255, B: 0, A: 255})
			}
		}
		dev.WriteTextureRGBA(tex, 0, 0, rgba)

		samp, err := dev.CreateSampler(nil, gpu.SamplerDesc{Label: "particle sampler"})
		if err != nil {
			t.Fatalf("CreateSampler: %v", err)
		}
		t.Cleanup(func() { _ = samp.Close() })

		texLayout, err := dev.CreateBindGroupLayoutDesc(nil, gpu.BindGroupLayoutDesc{
			Label: "particle tex layout",
			Entries: []gpu.BindGroupLayoutEntry{
				{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
				{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			},
		})
		if err != nil {
			t.Fatalf("CreateBindGroupLayoutDesc tex: %v", err)
		}
		t.Cleanup(func() { _ = texLayout.Close() })

		texGroup, err := dev.CreateBindGroupDesc(nil, gpu.BindGroupDesc{
			Label:  "particle tex group",
			Layout: texLayout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Texture: tex},
				{Binding: 1, Sampler: samp},
			},
		})
		if err != nil {
			t.Fatalf("CreateBindGroupDesc tex: %v", err)
		}
		t.Cleanup(func() { _ = texGroup.Close() })

		pipe, err := particles.NewParticlePipeline(nil, dev, particles.ParticlePipelineOptions{
			Label:         "textured",
			CameraLayout:  cam.layout,
			TextureLayout: texLayout,
			Blend:         particles.BlendAlpha,
		})
		if err != nil {
			t.Fatalf("NewParticlePipeline textured: %v", err)
		}
		t.Cleanup(func() { _ = pipe.Close() })

		gp, err := particles.NewGPUParticles3D(nil, dev, particles.GPUParticles3DOptions{Capacity: 2})
		if err != nil {
			t.Fatalf("NewGPUParticles3D: %v", err)
		}
		t.Cleanup(func() { _ = gp.Close() })

		// White particle tinted by green texture -> green (0, 255, 0).
		e := &particles.Emitter3D{
			MaxParticles: 2,
			LifetimeMin:  time.Hour,
			SizeMin:      2,
			SizeMax:      2,
			Color:        core.RGBA(0xffffff, 1),
		}
		e.Burst(1)
		if err := gp.Upload(e, camRight, camUp); err != nil {
			t.Fatalf("Upload: %v", err)
		}

		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x000000),
			}},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(0, cam.group)
		pass.SetBindGroup(1, texGroup)
		gp.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}

		wantRGBA(t, sr.Image(), 32, 32, color.RGBA{R: 0, G: 255, B: 0, A: 255}, 3)
	})
}

func TestGPUParticles3DViewReadbackProfiles(t *testing.T) {
	// Test both full capabilities and the WebGL 2 / OpenGL ES 3.0 profile (NoGPUCompute).
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	view, proj, eyePos, camRight, camUp, _, _ := orthoTestCamera()
	eye := eyePos.Vec4(1)

	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			const size = 64
			var (
				cam  *testCameraGPU
				pipe *gpu.Pipeline
				gp   *particles.GPUParticles3D
			)
			emitter := &particles.Emitter3D{
				MaxParticles: 16,
				LifetimeMin:  time.Hour,
				SizeMin:      2,
				SizeMax:      2,
				Color:        core.RGBA(0x00ccff, 1),
			}
			emitter.Burst(1)

			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, prof.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "p3d",
					Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						cam = newTestCameraGPU(t, cx, dev, view, proj, eye, false)
						var err error
						pipe, err = particles.NewParticlePipeline(cx, dev, particles.ParticlePipelineOptions{
							Label:        "p3d-view",
							CameraLayout: cam.layout,
							DepthFormat:  gpu.FormatDepth24Plus,
							Blend:        particles.BlendAlpha,
						})
						if err != nil {
							t.Fatalf("NewParticlePipeline: %v", err)
						}
						gp, err = particles.NewGPUParticles3D(cx, dev, particles.GPUParticles3DOptions{
							Label:    "p3d-view",
							Capacity: 16,
						})
						if err != nil {
							t.Fatalf("NewGPUParticles3D: %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						if err := gp.Upload(emitter, camRight, camUp); err != nil {
							t.Fatalf("Upload: %v", err)
						}
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x10141c))
						pass.ClearDepth(1)
						pass.SetPipeline(pipe)
						pass.SetBindGroup(0, cam.group)
						gp.Draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)

			img, err := app.GPUReadPixels("id:p3d")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			wantRGBA(t, img, 2, 2, color.RGBA{R: 16, G: 20, B: 28, A: 255}, 2)
			wantRGBA(t, img, 32, 32, color.RGBA{R: 0, G: 204, B: 255, A: 255}, 3)
		})
	}
}

func TestGPUParticles3D10kSingleDrawCall(t *testing.T) {
	const count = 10000
	const size = 64
	view, proj, eyePos, camRight, camUp, _, _ := orthoTestCamera()
	eye := eyePos.Vec4(1)

	emitter := &particles.Emitter3D{
		Shape:        particles.Box(3.5, 3.5, 1),
		MaxParticles: count,
		LifetimeMin:  time.Hour,
		SizeMin:      0.2,
		SizeMax:      0.2,
		Color:        core.RGBA(0xff8800, 1),
		Seed:         10000,
	}
	emitter.Burst(count)

	var (
		cam  *testCameraGPU
		pipe *gpu.Pipeline
		gp   *particles.GPUParticles3D
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID: "p3d10k",
			Style:  core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			OnReady: func(dev *gpu.Device) {
				cam = newTestCameraGPU(t, cx, dev, view, proj, eye, false)
				var err error
				pipe, err = particles.NewParticlePipeline(cx, dev, particles.ParticlePipelineOptions{
					Label:        "p3d-10k",
					CameraLayout: cam.layout,
					Blend:        particles.BlendAdditive,
				})
				if err != nil {
					t.Fatalf("NewParticlePipeline: %v", err)
				}
				gp, err = particles.NewGPUParticles3D(cx, dev, particles.GPUParticles3DOptions{
					Label:    "p3d-10k",
					Capacity: count,
				})
				if err != nil {
					t.Fatalf("NewGPUParticles3D: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				if err := gp.Upload(emitter, camRight, camUp); err != nil {
					t.Fatalf("Upload: %v", err)
				}
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x000000))
				pass.SetPipeline(pipe)
				pass.SetBindGroup(0, cam.group)
				gp.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)

	var drawFrame *nsgotest.GPUFrameRecord
	for i := range app.GPUFrames() {
		fr := app.GPUFrames()[i]
		if fr.DrawsCount > 0 {
			drawFrame = &fr
			break
		}
	}
	if drawFrame == nil {
		t.Fatal("expected a submitted GPU draw frame")
	}
	if drawFrame.DrawsCount != 1 {
		t.Fatalf("DrawsCount = %d, want 1 single instanced draw call", drawFrame.DrawsCount)
	}
	if drawFrame.InstancesCount != count {
		t.Fatalf("InstancesCount = %d, want %d", drawFrame.InstancesCount, count)
	}
	if gp.ByteSize() != 64+12+uint64(count)*particles.ParticleInstanceStride {
		t.Fatalf("ByteSize = %d, want %d", gp.ByteSize(), 64+12+uint64(count)*particles.ParticleInstanceStride)
	}

	img, err := app.GPUReadPixels("id:p3d10k")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	// Center is saturated by overlapping additive orange particles -> bright yellow/white (R=255, G=255).
	got := img.RGBAAt(32, 32)
	if got.R < 250 || got.G < 250 {
		t.Fatalf("center pixel after 10k additive particles = %+v, expected saturated R,G", got)
	}
}

func TestGPUParticles3DRetainedAcrossDeviceLoss(t *testing.T) {
	const size = 64
	view, proj, eyePos, camRight, camUp, _, _ := orthoTestCamera()
	eye := eyePos.Vec4(1)

	emitter := &particles.Emitter3D{
		MaxParticles: 4,
		LifetimeMin:  time.Hour,
		SizeMin:      2,
		SizeMax:      2,
		Color:        core.RGBA(0xff2266, 1),
	}
	emitter.Burst(1)

	var (
		cam  *testCameraGPU
		pipe *gpu.Pipeline
		gp   *particles.GPUParticles3D
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID: "p3d-lost",
			Style:  core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			OnReady: func(dev *gpu.Device) {
				if cam == nil {
					cam = newTestCameraGPU(t, cx, dev, view, proj, eye, true)
				}
				if pipe == nil {
					var err error
					pipe, err = particles.NewParticlePipeline(cx, dev, particles.ParticlePipelineOptions{
						Label:        "p3d-retained",
						CameraLayout: cam.layout,
						Retained:     true,
					})
					if err != nil {
						t.Fatalf("NewParticlePipeline: %v", err)
					}
				}
				if gp == nil {
					var err error
					gp, err = particles.NewGPUParticles3D(cx, dev, particles.GPUParticles3DOptions{
						Label:    "p3d-retained",
						Capacity: 4,
						Retained: true,
					})
					if err != nil {
						t.Fatalf("NewGPUParticles3D: %v", err)
					}
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				if err := gp.Upload(emitter, camRight, camUp); err != nil {
					t.Fatalf("Upload: %v", err)
				}
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x000000))
				pass.SetPipeline(pipe)
				pass.SetBindGroup(0, cam.group)
				gp.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)
	first := gp
	app.TriggerGPUDeviceLost("id:p3d-lost", nsgotest.GPULostContextLost, "context lost")
	if first.Valid() {
		t.Fatal("GPUParticles3D still Valid while device is lost")
	}
	app.RestoreGPUDevice("id:p3d-lost")
	if gp != first || !first.Valid() {
		t.Fatalf("retained GPUParticles3D not restored: same=%v valid=%v", gp == first, first.Valid())
	}
	app.Advance(16 * time.Millisecond)
	img, err := app.GPUReadPixels("id:p3d-lost")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	wantRGBA(t, img, 32, 32, color.RGBA{R: 255, G: 34, B: 102, A: 255}, 3)
}

func TestGPUParticles3DValidationErrors(t *testing.T) {
	if _, err := particles.NewGPUParticles3D(nil, nil, particles.GPUParticles3DOptions{}); err == nil {
		t.Fatal("NewGPUParticles3D(nil dev) succeeded, want error")
	}
	if _, err := particles.NewParticlePipeline(nil, nil, particles.ParticlePipelineOptions{}); err == nil {
		t.Fatal("NewParticlePipeline(nil dev) succeeded, want error")
	}

	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	if _, err := particles.NewParticlePipeline(nil, dev, particles.ParticlePipelineOptions{}); err == nil {
		t.Fatal("NewParticlePipeline(nil CameraLayout) succeeded, want error")
	}

	gp, err := particles.NewGPUParticles3D(nil, dev, particles.GPUParticles3DOptions{Capacity: 4})
	if err != nil {
		t.Fatalf("NewGPUParticles3D: %v", err)
	}
	if err := gp.UploadBytes([]byte{1, 2, 3}); err == nil {
		t.Fatal("UploadBytes with misaligned slice succeeded, want error")
	}
	if err := gp.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if gp.Valid() {
		t.Fatal("Valid() = true after Close")
	}
	if err := gp.Upload(&particles.Emitter3D{}, math3d.Vec3Right(), math3d.Vec3Up()); err == nil {
		t.Fatal("Upload after Close succeeded, want error")
	}
}
