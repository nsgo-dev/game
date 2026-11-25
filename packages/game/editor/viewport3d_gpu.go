package editor

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// viewport3DDepthFormat is the depth buffer of the 3D viewport's GPU view, which its passes draw the scene's geometry and the sky against.
const viewport3DDepthFormat = gpu.FormatDepth24Plus

// The gradient sky drawn behind the scene when the editor has no Lights3D, as display colors: a blue zenith, a pale horizon and a dark ground.
var (
	skyZenith3D  = [3]float32{0.24, 0.38, 0.58}
	skyHorizon3D = [3]float32{0.66, 0.72, 0.78}
	skyGround3D  = [3]float32{0.17, 0.17, 0.18}
)

// DefaultLights3D returns the lights the 3D viewport draws the scene's geometry with when the editor has no Lights3D: a cool ambient fill like the sky's and a warm key light from above, to the front and right of the default camera.
func DefaultLights3D() render3d.LightRig {
	return render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.32, Y: 0.36, Z: 0.42}, Intensity: 1},
		Directional: []render3d.DirectionalLight{{
			Direction: math3d.Vec3{X: -0.45, Y: -0.8, Z: -0.4},
			Color:     math3d.Vec3{X: 1, Y: 0.95, Z: 0.86},
			Intensity: 3,
		}},
	}
}

// SceneCamera returns c as a scene3d.Camera for a viewport w by h, with the same eye, view direction, projection (perspective, or orthographic with OrthoHeight) and clip planes that Project and ScreenRay use, which the 3D viewport draws the scene's geometry through.
func (c OrbitCamera3D) SceneCamera(w, h float32) *scene3d.Camera {
	_, fov, near, far := c.effectiveParams()
	aspect := float32(1)
	if w > 0 && h > 0 {
		aspect = w / h
	}
	eye := c.Eye()
	pos := math3d.Vec3{X: eye[0], Y: eye[1], Z: eye[2]}
	var cam *scene3d.Camera
	if c.Orthographic {
		cam = scene3d.NewOrthographicCamera(pos, c.OrthoHeight(), aspect, float32(near), float32(far))
	} else {
		cam = scene3d.NewPerspectiveCamera(pos, float32(fov), aspect, float32(near), float32(far))
	}
	// The camera's own up, not the world's, so a view straight down or up still has one.
	_, up, _ := c.Basis()
	cam.LookAt(math3d.Vec3{X: c.Target[0], Y: c.Target[1], Z: c.Target[2]}, math3d.Vec3{X: up[0], Y: up[1], Z: up[2]})
	return cam
}

// Viewport3DOnGPU reports whether the 3D viewport's GPU view drew the scene's geometry the last frame, so the canvas over it draws only the overlays (DrawViewport3D). It is false on a target without GPU support, before the GPU view's first frame, after a device loss and when it cannot draw, and the canvas then draws the whole wireframe view.
func (e *Editor) Viewport3DOnGPU() bool {
	return e != nil && e.gpu3D
}

// Viewport3DError returns why the 3D viewport cannot show the scene's geometry, or nil: the scene's 3D build failed (Scene3D), such as on a model that does not load, or the GPU view failed to draw it.
func (e *Editor) Viewport3DError() error {
	if e == nil {
		return nil
	}
	if e.s3d.err != nil {
		return e.s3d.err
	}
	return e.gpu3DErr
}

// setViewport3DGPU records whether the GPU view draws the scene and, when it does not, why, telling the listeners when that changes so the canvas switches between the overlay and the whole wireframe view.
func (e *Editor) setViewport3DGPU(on bool, err error) {
	if e.gpu3D == on && sameError(e.gpu3DErr, err) {
		return
	}
	e.gpu3D, e.gpu3DErr = on, err
	e.notify()
}

// sameError reports whether a and b are both nil or say the same thing, so an error repeated every frame tells the listeners once.
func sameError(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Error() == b.Error()
}

// renderPassEncoder is what the 3D viewport records its pass into: a gpu.Frame in the GPU view, a gpu.Encoder offscreen.
type renderPassEncoder interface {
	BeginRenderPass(desc gpu.RenderPassDesc) *gpu.RenderPass
}

// viewport3DRenderer draws the editor's 3D build (Scene3D) on a gpu.Device with scenemodel.Renderer: the sky, then the geometry. It uploads the geometry again only when the build changes, and records a frame only when the editor, the size or the build changed since the last one it recorded. Use it from the engine goroutine, like the device.
type viewport3DRenderer struct {
	ed  *Editor
	dev *gpu.Device

	sky      *render3d.SkyboxPipeline
	skyCube  *render3d.GPUCubemap
	skyGroup *gpu.BindGroup

	// scene draws the build of generation gen.
	scene *scenemodel.Renderer
	gen   uint64

	// drawn is what the last recorded frame showed.
	drawn    viewport3DFrame
	hasDrawn bool
	// failed is the last frame that could not be recorded and failErr why, nil when the last try did not fail. A frame that would fail the same way is not tried again, uploading the whole build on every display frame, until the editor, the size or the build changes.
	failed  viewport3DFrame
	failErr error
}

// viewport3DFrame is what decides a frame's pixels: the editor's revision, the surface's size and the build's generation.
type viewport3DFrame struct {
	rev  uint64
	w, h int
	gen  uint64
}

// newViewport3DRenderer creates the sky on dev; the scene's geometry uploads on the first render.
func newViewport3DRenderer(ed *Editor, dev *gpu.Device) (*viewport3DRenderer, error) {
	r := &viewport3DRenderer{ed: ed, dev: dev}
	var err error
	if r.sky, err = render3d.NewSkyboxPipeline(nil, dev, render3d.SkyboxPipelineDesc{Label: "editor.viewport3d.sky", DepthFormat: viewport3DDepthFormat}); err != nil {
		return nil, fmt.Errorf("editor: 3D viewport sky: %w", err)
	}
	cube, err := render3d.GradientSkyCubemap(skyZenith3D, skyHorizon3D, skyGround3D, 16)
	if err == nil {
		r.skyCube, err = render3d.UploadCubemap(nil, dev, cube, render3d.CubemapUpload{Label: "editor.viewport3d.sky"})
	}
	if err == nil {
		r.skyGroup, err = r.sky.BindCubemap(nil, r.skyCube)
	}
	if err != nil {
		_ = r.close()
		return nil, fmt.Errorf("editor: 3D viewport sky: %w", err)
	}
	return r, nil
}

// render records the 3D viewport, w by h pixels, into enc and reports whether it recorded anything: nothing when the frame would show what the last one did. On an error it records nothing, and it returns the same error without trying again until the editor's revision, the size or the build changes.
func (r *viewport3DRenderer) render(enc renderPassEncoder, w, h int) (bool, error) {
	ed := r.ed
	s := ed.scene3D()
	frame := viewport3DFrame{rev: ed.revision, w: w, h: h, gen: s.gen}
	if r.failErr != nil && frame == r.failed {
		return false, r.failErr
	}
	r.failErr = nil
	if r.hasDrawn && frame == r.drawn {
		return false, nil
	}
	fail := func(err error) (bool, error) {
		r.failed, r.failErr = frame, fmt.Errorf("editor: 3D viewport: %w", err)
		return false, r.failErr
	}
	if r.scene != nil && r.gen != s.gen {
		_ = r.scene.Close()
		r.scene = nil
	}
	if r.scene == nil && s.built != nil {
		scene, err := scenemodel.NewRenderer(nil, r.dev, s.built, scenemodel.RendererOptions{Label: "editor.viewport3d", DepthFormat: viewport3DDepthFormat})
		if err != nil {
			return fail(err)
		}
		r.scene, r.gen = scene, s.gen
	}
	cam := ed.Camera3D.SceneCamera(float32(w), float32(h))
	lights, sky := DefaultLights3D(), true
	if ed.Lights3D != nil {
		lights, sky = *ed.Lights3D, false
	}
	if r.scene != nil {
		shading := scenemodel.ShadingLit
		if ed.Shading3D == Shading3DUnlit {
			shading = scenemodel.ShadingUnlit
		}
		if err := r.scene.SetShading(shading); err != nil {
			return fail(err)
		}
		if _, err := r.scene.Prepare(cam, lights); err != nil {
			return fail(err)
		}
	}
	if sky {
		r.sky.WriteCamera(cam)
	}
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "editor.viewport3d",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: ed.background3D()}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	// The sky goes first: it writes no depth, so the geometry drawn after it, blended surfaces included, covers it.
	if sky {
		r.sky.Draw(pass, r.skyGroup)
	}
	r.scene.Draw(pass)
	pass.End()
	r.drawn, r.hasDrawn = frame, true
	return true, nil
}

// close releases r's GPU resources. It is safe to call more than once.
func (r *viewport3DRenderer) close() error {
	if r == nil {
		return nil
	}
	var err error
	if r.scene != nil {
		err = errors.Join(err, r.scene.Close())
		r.scene = nil
	}
	if r.skyGroup != nil {
		err = errors.Join(err, r.skyGroup.Close())
		r.skyGroup = nil
	}
	if r.skyCube != nil {
		err = errors.Join(err, r.skyCube.Close())
		r.skyCube = nil
	}
	if r.sky != nil {
		err = errors.Join(err, r.sky.Close())
		r.sky = nil
	}
	r.hasDrawn, r.failErr = false, nil
	return err
}

// viewport3DView is the 3D viewport's GPU view: it draws the scene's geometry with a viewport3DRenderer and shows overlay, the canvas of grid, boxes and gizmo, over it. On a target without GPU support it is a plain view holding overlay, which then draws the whole wireframe view.
func viewport3DView(cx *core.Scope, ed *Editor, overlay core.Node) core.Node {
	var r *viewport3DRenderer
	drop := func(err error) {
		_ = r.close()
		r = nil
		ed.setViewport3DGPU(false, err)
	}
	cx.Cleanup(func() { drop(nil) })
	return gpu.View(cx, gpu.ViewProps{
		TestID:      "editor-viewport-3d",
		Label:       "3D Scene Viewport",
		DepthFormat: viewport3DDepthFormat,
		Style:       core.Style{FlexGrow: 1},
		Children:    []core.Node{overlay},
		OnReady: func(dev *gpu.Device) {
			_ = r.close()
			var err error
			if r, err = newViewport3DRenderer(ed, dev); err != nil {
				drop(err)
			}
		},
		OnFrame: func(f *gpu.Frame) {
			if r == nil {
				return
			}
			w, h := f.Size()
			drew, err := r.render(f, w, h)
			if err != nil {
				before := ed.revision
				ed.setViewport3DGPU(false, err)
				if ed.revision == before+1 {
					// Reporting the error changes nothing the failed frame would draw, so it does not count as a change to try the frame again for.
					r.failed.rev = ed.revision
				}
				return
			}
			if drew {
				before := ed.revision
				ed.setViewport3DGPU(true, nil)
				if ed.revision == before+1 {
					// Telling the canvas to draw only the overlays changes nothing the frame just drawn shows.
					r.drawn.rev = ed.revision
				}
			}
		},
		OnDeviceLost: func(gpu.DeviceLostEvent) { drop(nil) },
		OnError: func(err gpu.Error) {
			if err.Code == gpu.ErrUnsupported {
				drop(nil)
				return
			}
			ed.setViewport3DGPU(ed.gpu3D, err)
		},
	})
}
