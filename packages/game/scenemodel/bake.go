package scenemodel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

const (
	defaultLightProbeBakeFaceSize = 16
	defaultProbeBakeNear          = 0.05
	defaultProbeBakeFar           = 200
)

// ProbeBakeOptions configures [BakeReflectionProbe] and [BakeLightProbeGrid].
type ProbeBakeOptions struct {
	// Size overrides the cubemap face resolution in texels; non-positive uses the probe's
	// Resolution for reflection probes (default 64) or 16 for light probe grids.
	Size int
	// Near and Far set the 90-degree cubemap face clip planes; non-positive Near defaults to 0.05,
	// and Far <= Near defaults to Near + 200.
	Near float32
	Far  float32
	// FallbackLights supplies the direct light rig used when the built scene has no 3D lights
	// and no environment section. When nil, Built.LightRig() is always used.
	FallbackLights *render3d.LightRig
	// FallbackClearColor sets the background clear color when the scene has no solid or cubemap sky.
	FallbackClearColor core.Color
}

// BakeReflectionProbe renders b's 3D scene from probe's world position into all six faces of a
// linear HDR cubemap using render3d on an offscreen software GPU renderer, disabling existing
// baked probe contributions so rebaking captures direct lighting, shadows, sky, and emissives.
func BakeReflectionProbe(b *Built, probe *ReflectionProbe, opts ProbeBakeOptions) (*render3d.HDRCubemap, error) {
	if b == nil {
		return nil, errors.New("scenemodel: nil Built")
	}
	if probe == nil {
		return nil, errors.New("scenemodel: nil ReflectionProbe")
	}
	size := opts.Size
	if size <= 0 {
		size = probe.Doc.EffectiveResolution()
	}
	if size <= 0 || size > sceneformat.MaxReflectionProbeResolution {
		return nil, fmt.Errorf("scenemodel: reflection probe bake size %d out of range [1, %d]", size, sceneformat.MaxReflectionProbeResolution)
	}
	pos, _, _ := probe.WorldBounds()
	baker, err := newProbeSceneBaker(b, size, opts)
	if err != nil {
		return nil, err
	}
	defer baker.close()
	return baker.captureCubemap(pos)
}

// BakeLightProbeGrid renders b's 3D scene from every lattice point of grid into a small linear
// HDR cubemap using render3d on an offscreen software GPU renderer and integrates each probe's
// cosine-convolved L2 spherical harmonic irradiance coefficients.
func BakeLightProbeGrid(b *Built, grid *LightProbeGrid, opts ProbeBakeOptions) (*render3d.LightProbeGrid, error) {
	if b == nil {
		return nil, errors.New("scenemodel: nil Built")
	}
	if grid == nil {
		return nil, errors.New("scenemodel: nil LightProbeGrid")
	}
	bounds := grid.WorldBounds()
	counts := grid.Doc.EffectiveCounts()
	out, err := render3d.NewLightProbeGrid(bounds, counts[0], counts[1], counts[2])
	if err != nil {
		return nil, err
	}
	size := opts.Size
	if size <= 0 {
		size = defaultLightProbeBakeFaceSize
	}
	if size <= 0 || size > sceneformat.MaxReflectionProbeResolution {
		return nil, fmt.Errorf("scenemodel: light probe bake face size %d out of range [1, %d]", size, sceneformat.MaxReflectionProbeResolution)
	}
	baker, err := newProbeSceneBaker(b, size, opts)
	if err != nil {
		return nil, err
	}
	defer baker.close()

	for iz := range counts[2] {
		for iy := range counts[1] {
			for ix := range counts[0] {
				pos := out.ProbePosition(ix, iy, iz)
				cm, err := baker.captureCubemap(pos)
				if err != nil {
					return nil, fmt.Errorf("scenemodel: bake light probe (%d,%d,%d): %w", ix, iy, iz, err)
				}
				sh, err := render3d.ComputeIrradianceSHHDR(cm)
				if err != nil {
					return nil, fmt.Errorf("scenemodel: integrate light probe (%d,%d,%d): %w", ix, iy, iz, err)
				}
				out.Set(ix, iy, iz, sh)
			}
		}
	}
	return out, nil
}

type probeSceneBaker struct {
	built        *Built
	sr           *gpu.SoftwareRenderer
	rt           *gpu.RenderTarget
	renderer     *Renderer
	size         int
	near, far    float32
	isHDR        bool
	useFallback  bool
	fallbackRig  render3d.LightRig
	clearColor   core.Color
	readScratch  []byte
	faceIsLinear bool
}

func newProbeSceneBaker(b *Built, size int, opts ProbeBakeOptions) (*probeSceneBaker, error) {
	near := opts.Near
	if near <= 0 {
		near = defaultProbeBakeNear
	}
	far := opts.Far
	if far <= near {
		far = near + defaultProbeBakeFar
	}

	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	dev := sr.Device()

	colorFmt := gpu.FormatRGBA8Unorm
	isHDR := false
	if dev.SupportsFormat(gpu.FormatRGBA16Float) {
		colorFmt = gpu.FormatRGBA16Float
		isHDR = true
	}

	rt, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
		Label:       "scenemodel.probe_bake_rt",
		Width:       uint32(size),
		Height:      uint32(size),
		ColorFormat: colorFmt,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil && isHDR {
		// Fall back to RGBA8Unorm if half-float render target creation is unsupported.
		colorFmt = gpu.FormatRGBA8Unorm
		isHDR = false
		rt, err = dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
			Label:       "scenemodel.probe_bake_rt",
			Width:       uint32(size),
			Height:      uint32(size),
			ColorFormat: colorFmt,
			DepthFormat: gpu.FormatDepth24Plus,
		})
	}
	if err != nil {
		sr.Close()
		return nil, fmt.Errorf("scenemodel: probe bake render target: %w", err)
	}

	r, err := NewRenderer(nil, dev, b, RendererOptions{
		Label:            "scenemodel.probe_bake",
		ColorFormat:      colorFmt,
		DepthFormat:      gpu.FormatDepth24Plus,
		DisableOcclusion: true,
		LinearOutput:     isHDR,
		DisableProbes:    true,
	})
	if err != nil {
		_ = rt.Close()
		sr.Close()
		return nil, fmt.Errorf("scenemodel: probe bake renderer: %w", err)
	}

	clearColor := opts.FallbackClearColor
	if b.Environment.SkyKind == sceneformat.SkyColor {
		if isHDR {
			clearColor = srgbColorFromVec3(b.Environment.SkyColor)
		} else {
			clearColor = b.Environment.SkyClearColor
		}
	}

	useFallback := opts.FallbackLights != nil && !b.HasSceneLighting()
	var fallbackRig render3d.LightRig
	if useFallback {
		fallbackRig = *opts.FallbackLights
	}

	return &probeSceneBaker{
		built:        b,
		sr:           sr,
		rt:           rt,
		renderer:     r,
		size:         size,
		near:         near,
		far:          far,
		isHDR:        isHDR,
		useFallback:  useFallback,
		fallbackRig:  fallbackRig,
		clearColor:   clearColor,
		faceIsLinear: isHDR,
	}, nil
}

func (pb *probeSceneBaker) close() {
	if pb == nil {
		return
	}
	if pb.renderer != nil {
		_ = pb.renderer.Close()
		pb.renderer = nil
	}
	if pb.rt != nil {
		_ = pb.rt.Close()
		pb.rt = nil
	}
	if pb.sr != nil {
		pb.sr.Close()
		pb.sr = nil
	}
}

func (pb *probeSceneBaker) captureCubemap(pos math3d.Vec3) (*render3d.HDRCubemap, error) {
	cm, err := render3d.NewHDRCubemap(pb.size)
	if err != nil {
		return nil, err
	}
	dev := pb.sr.Device()
	for f := range render3d.ProbeCubeFaceCount {
		face := gpu.CubeFace(f)
		cam := probeFaceSceneCamera(pos, face, pb.near, pb.far)
		if pb.useFallback {
			if _, err := pb.renderer.Prepare(cam, pb.fallbackRig); err != nil {
				return nil, err
			}
		} else {
			if _, err := pb.renderer.PrepareScene(cam); err != nil {
				return nil, err
			}
		}
		enc := dev.BeginEncoder()
		if !pb.useFallback {
			pb.renderer.RecordShadows(enc)
		}
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			Label: "scenemodel.probe_bake_face",
			ColorAttachments: []gpu.ColorAttachment{{
				Texture:    pb.rt.Color,
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: pb.clearColor,
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				Texture:      pb.rt.Depth,
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreDiscard,
				ClearDepth:   1,
			},
		})
		pb.renderer.DrawSky(pass)
		pb.renderer.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())

		var readErr error
		pb.readScratch = pb.readScratch[:0]
		dev.ReadTexture(pb.rt.Color, gpu.TextureRegion{
			Width:  uint32(pb.size),
			Height: uint32(pb.size),
		}, func(raw []byte, err error) {
			readErr = err
			pb.readScratch = append(pb.readScratch[:0], raw...)
		})
		pb.sr.WaitIdle()
		if err := pb.sr.LastError(); err != nil {
			return nil, fmt.Errorf("scenemodel: probe bake GPU error: %w", err)
		}
		if readErr != nil {
			return nil, fmt.Errorf("scenemodel: probe bake readback: %w", readErr)
		}
		if err := decodeFaceToHDR(cm.Faces[f], pb.readScratch, pb.size, pb.faceIsLinear); err != nil {
			return nil, err
		}
	}
	return cm, nil
}

// probeFaceSceneCamera builds a 90-degree square perspective scene3d.Camera matching render3d.ProbeFaceViewProj.
func probeFaceSceneCamera(pos math3d.Vec3, face gpu.CubeFace, near, far float32) *scene3d.Camera {
	var target, up math3d.Vec3
	switch face {
	case gpu.CubeFacePosX:
		target = math3d.Vec3{X: 1}
		up = math3d.Vec3{Y: -1}
	case gpu.CubeFaceNegX:
		target = math3d.Vec3{X: -1}
		up = math3d.Vec3{Y: -1}
	case gpu.CubeFacePosY:
		target = math3d.Vec3{Y: 1}
		up = math3d.Vec3{Z: 1}
	case gpu.CubeFaceNegY:
		target = math3d.Vec3{Y: -1}
		up = math3d.Vec3{Z: -1}
	case gpu.CubeFacePosZ:
		target = math3d.Vec3{Z: 1}
		up = math3d.Vec3{Y: -1}
	case gpu.CubeFaceNegZ:
		target = math3d.Vec3{Z: -1}
		up = math3d.Vec3{Y: -1}
	default:
		target = math3d.Vec3{X: 1}
		up = math3d.Vec3{Y: -1}
	}
	cam := scene3d.NewPerspectiveCamera(pos, math.Pi/2, 1, near, far)
	cam.LookAt(pos.Add(target), up)
	return cam
}

func decodeFaceToHDR(dst [][3]float32, raw []byte, size int, isHDR16F bool) error {
	n := size * size
	if len(dst) < n {
		return fmt.Errorf("scenemodel: probe face destination too small (%d < %d)", len(dst), n)
	}
	clampHDR := func(v float32) float32 {
		if math.IsNaN(float64(v)) || v <= 0 {
			return 0
		}
		if math.IsInf(float64(v), 0) || v > 65504 {
			return 65504
		}
		return v
	}
	if isHDR16F && len(raw) >= n*8 {
		rowBytes := size * 8
		for y := range size {
			sy := size - 1 - y
			srcRow := raw[sy*rowBytes:]
			dstRow := y * size
			for x := range size {
				off := x * 8
				r := clampHDR(render3d.HalfToFloat32(binary.LittleEndian.Uint16(srcRow[off+0:])))
				g := clampHDR(render3d.HalfToFloat32(binary.LittleEndian.Uint16(srcRow[off+2:])))
				b := clampHDR(render3d.HalfToFloat32(binary.LittleEndian.Uint16(srcRow[off+4:])))
				dst[dstRow+x] = [3]float32{r, g, b}
			}
		}
		return nil
	}
	if len(raw) < n*4 {
		return fmt.Errorf("scenemodel: probe bake readback returned %d bytes, want at least %d", len(raw), n*4)
	}
	rowBytes := size * 4
	for y := range size {
		sy := size - 1 - y
		srcRow := raw[sy*rowBytes:]
		dstRow := y * size
		for x := range size {
			off := x * 4
			dst[dstRow+x] = [3]float32{
				srgbToLinear(float64(srcRow[off+0]) / 255),
				srgbToLinear(float64(srcRow[off+1]) / 255),
				srgbToLinear(float64(srcRow[off+2]) / 255),
			}
		}
	}
	return nil
}
