package render3d_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func iblHelmetLoad(t *testing.T) (*gltf.Document, render3d.Mesh) {
	t.Helper()
	dir := filepath.Join(damagedHelmetDir, "glTF")
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		t.Skipf("DamagedHelmet fixture %s is not in this copy of the module", damagedHelmetDir)
	}
	doc, err := gltf.LoadFS(os.DirFS(dir), "DamagedHelmet.gltf")
	if err != nil {
		t.Fatalf("gltf.LoadFS DamagedHelmet: %v", err)
	}
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatalf("ComputeWorldMatrices: %v", err)
	}
	if len(doc.Nodes) == 0 || len(doc.Meshes) == 0 || len(doc.Meshes[0].Primitives) == 0 {
		t.Fatal("DamagedHelmet missing expected node/mesh/primitive")
	}
	return doc, transformGLTFMesh(doc.Meshes[0].Primitives[0], doc.Nodes[0].WorldMatrix)
}

// iblHelmetStudioEquirect builds a deterministic linear HDR studio panorama with:
//   - a cool sky-blue upper dome and warm studio horizon band
//   - a dark neutral studio floor
//   - a bright warm HDR key softbox (peak radiance ~10.0) in the front-upper-right
//   - a crisp cool HDR rim softbox (peak radiance ~5.0) in the rear-left
//   - a gentle overhead studio strip light (peak radiance ~3.5)
//
// Because peak radiance exceeds 1.0 by an order of magnitude, glossy metals and
// scratches on DamagedHelmet exhibit crisp HDR specular reflections that are
// measurably brighter than an LDR-clamped copy of the same environment.
func iblHelmetStudioEquirect(w, h int) *render3d.HDRImage {
	img, err := render3d.NewHDRImage(w, h)
	if err != nil {
		panic(err)
	}
	zenith := [3]float64{0.18, 0.34, 0.72}
	horizon := [3]float64{0.78, 0.70, 0.58}
	ground := [3]float64{0.07, 0.08, 0.11}

	keyDir := math3d.Vec3{X: 0.52, Y: 0.58, Z: 0.63}.Norm()
	rimDir := math3d.Vec3{X: -0.78, Y: 0.32, Z: -0.54}.Norm()
	topDir := math3d.Vec3{X: -0.10, Y: 0.96, Z: 0.26}.Norm()

	for y := range h {
		ev := (float64(y) + 0.5) / float64(h)
		theta := ev * math.Pi
		dy := math.Cos(theta)
		sinT := math.Sin(theta)
		for x := range w {
			eu := (float64(x) + 0.5) / float64(w)
			phi := (eu - 0.5) * 2.0 * math.Pi
			dx := sinT * math.Cos(phi)
			dz := sinT * math.Sin(phi)
			dir := math3d.Vec3{X: float32(dx), Y: float32(dy), Z: float32(dz)}

			var r, g, b float64
			if dy >= 0 {
				t := math.Pow(dy, 0.55)
				r = horizon[0] + (zenith[0]-horizon[0])*t
				g = horizon[1] + (zenith[1]-horizon[1])*t
				b = horizon[2] + (zenith[2]-horizon[2])*t
			} else {
				t := math.Pow(-dy, 0.45)
				r = horizon[0] + (ground[0]-horizon[0])*t
				g = horizon[1] + (ground[1]-horizon[1])*t
				b = horizon[2] + (ground[2]-horizon[2])*t
			}

			// Warm HDR key softbox (peak ~10.0).
			if kd := max(float64(dir.Dot(keyDir)), 0); kd > 0 {
				lobe := math.Pow(kd, 18.0)
				r += lobe * 10.5
				g += lobe * 9.0
				b += lobe * 6.8
			}
			// Cool HDR rim softbox (peak ~5.0).
			if rd := max(float64(dir.Dot(rimDir)), 0); rd > 0 {
				lobe := math.Pow(rd, 22.0)
				r += lobe * 2.4
				g += lobe * 4.2
				b += lobe * 6.2
			}
			// Overhead neutral softbox strip (peak ~3.5).
			if td := max(float64(dir.Dot(topDir)), 0); td > 0 {
				lobe := math.Pow(td, 14.0)
				r += lobe * 3.4
				g += lobe * 3.5
				b += lobe * 3.8
			}
			img.Set(x, y, [3]float32{float32(r), float32(g), float32(b)})
		}
	}
	return img
}

// iblHelmetStudioCubemap round-trips the procedural studio HDR panorama through Radiance .hdr (RGBE) encoding and CubemapFromHDR so the full file-to-cubemap path is exercised.
func iblHelmetStudioCubemap(t *testing.T, faceSize int) *render3d.HDRCubemap {
	t.Helper()
	pano := iblHelmetStudioEquirect(128, 64)
	var buf bytes.Buffer
	if err := render3d.EncodeHDR(&buf, pano); err != nil {
		t.Fatalf("EncodeHDR: %v", err)
	}
	cm, err := render3d.CubemapFromHDR(&buf, faceSize)
	if err != nil {
		t.Fatalf("CubemapFromHDR: %v", err)
	}
	return cm
}

// iblHelmetClampHDRToLDR returns a copy of env clamped to [0, 1] per channel to isolate the contribution of HDR radiance > 1.0.
func iblHelmetClampHDRToLDR(env *render3d.HDRCubemap) *render3d.HDRCubemap {
	out, err := render3d.NewHDRCubemap(env.Size)
	if err != nil {
		panic(err)
	}
	for face := range 6 {
		for i, c := range env.Faces[face] {
			out.Faces[face][i] = [3]float32{
				min(max(c[0], 0), 1),
				min(max(c[1], 0), 1),
				min(max(c[2], 0), 1),
			}
		}
	}
	return out
}

func iblHelmetMeanLuma(img *image.RGBA, x0, y0, x1, y1 int) float64 {
	var sum float64
	var n int
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := img.RGBAAt(x, y)
			sum += 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func TestHDRImageCodecAndEquirectangular(t *testing.T) {
	// 1. Adaptive RLE round-trip (width >= 8) across dark, mid, and bright HDR values.
	rleImg, err := render3d.NewHDRImage(16, 8)
	if err != nil {
		t.Fatalf("NewHDRImage: %v", err)
	}
	samples := [][3]float32{
		{0, 0, 0},
		{0.002, 0.004, 0.008},
		{0.5, 0.25, 0.75},
		{4.25, 2.5, 1.125},
		{32.0, 18.5, 6.25},
		{120.0, 95.0, 40.0},
	}
	for y := range rleImg.Height {
		for x := range rleImg.Width {
			if y == 0 {
				// Long identical run exercises RLE run encoding.
				rleImg.Set(x, y, [3]float32{12.5, 6.25, 3.125})
			} else {
				rleImg.Set(x, y, samples[(y*rleImg.Width+x)%len(samples)])
			}
		}
	}
	var buf bytes.Buffer
	if err := render3d.EncodeHDR(&buf, rleImg); err != nil {
		t.Fatalf("EncodeHDR RLE: %v", err)
	}
	decoded, err := render3d.DecodeHDR(&buf)
	if err != nil {
		t.Fatalf("DecodeHDR RLE: %v", err)
	}
	if decoded.Width != rleImg.Width || decoded.Height != rleImg.Height {
		t.Fatalf("decoded size = %dx%d, want %dx%d", decoded.Width, decoded.Height, rleImg.Width, rleImg.Height)
	}
	for y := range rleImg.Height {
		for x := range rleImg.Width {
			want := rleImg.At(x, y)
			got := decoded.At(x, y)
			for ch := range 3 {
				tol := max(float64(want[ch])*0.02, 1e-4)
				if math.Abs(float64(got[ch]-want[ch])) > tol {
					t.Fatalf("RLE pixel (%d,%d) ch %d = %v, want %v (tol %v)", x, y, ch, got[ch], want[ch], tol)
				}
			}
		}
	}

	// 2. Uncompressed round-trip (width < 8).
	small, err := render3d.NewHDRImage(4, 3)
	if err != nil {
		t.Fatalf("NewHDRImage small: %v", err)
	}
	small.Set(0, 0, [3]float32{8.0, 4.0, 2.0})
	small.Set(3, 2, [3]float32{0.125, 16.0, 0.5})
	buf.Reset()
	if err := render3d.EncodeHDR(&buf, small); err != nil {
		t.Fatalf("EncodeHDR small: %v", err)
	}
	smallDec, err := render3d.DecodeHDR(&buf)
	if err != nil {
		t.Fatalf("DecodeHDR small: %v", err)
	}
	for _, pt := range [][2]int{{0, 0}, {3, 2}} {
		w := small.At(pt[0], pt[1])
		g := smallDec.At(pt[0], pt[1])
		for ch := range 3 {
			tol := max(float64(w[ch])*0.02, 1e-4)
			if math.Abs(float64(g[ch]-w[ch])) > tol {
				t.Fatalf("uncompressed pixel %v ch %d = %v, want %v", pt, ch, g[ch], w[ch])
			}
		}
	}

	// 3. EXPOSURE header and +Y / -X orientation flipping.
	var expHdr bytes.Buffer
	expHdr.WriteString("#?RADIANCE\n# comment line\nEXPOSURE=2.0\nFORMAT=32-bit_rle_rgbe\n\n+Y 2 -X 2\n")
	// Pixel (raw row 0, raw col 0) -> flipped to (dstY=1, dstX=1). RGBE (128, 128, 128, 130) => 128 * 2^(130-136) = 2.0, divided by exposure 2.0 => 1.0.
	expHdr.Write([]byte{128, 128, 128, 130, 0, 0, 0, 0})
	expHdr.Write([]byte{0, 0, 0, 0, 128, 64, 32, 132}) // raw (1,1) -> dst (0,0): (8, 4, 2) / 2.0 = (4, 2, 1)
	expDec, err := render3d.DecodeHDR(&expHdr)
	if err != nil {
		t.Fatalf("DecodeHDR exposure/flipped: %v", err)
	}
	if p := expDec.At(1, 1); !math3d.ApproxEqual(p[0], 1.0, 0.02) {
		t.Errorf("flipped/exposed (1,1) = %v, want ~1.0", p)
	}
	if p := expDec.At(0, 0); !math3d.ApproxEqual(p[0], 4.0, 0.05) || !math3d.ApproxEqual(p[1], 2.0, 0.05) || !math3d.ApproxEqual(p[2], 1.0, 0.05) {
		t.Errorf("flipped/exposed (0,0) = %v, want ~(4, 2, 1)", p)
	}

	// 4. Equirectangular HDR projection preserves values > 1.0 along +X and +Y.
	pano := iblHelmetStudioEquirect(64, 32)
	var hdrStream bytes.Buffer
	if err := render3d.EncodeHDR(&hdrStream, pano); err != nil {
		t.Fatalf("EncodeHDR pano: %v", err)
	}
	hdrCM, err := render3d.CubemapFromHDR(&hdrStream, 16)
	if err != nil {
		t.Fatalf("CubemapFromHDR: %v", err)
	}
	keyDir := math3d.Vec3{X: 0.52, Y: 0.58, Z: 0.63}.Norm()
	peak := hdrCM.Sample(keyDir)
	if peak[0] < 8.0 || peak[1] < 7.0 || peak[2] < 5.0 {
		t.Errorf("HDR cubemap sample at key light = %v, want unclamped HDR radiance > (8, 7, 5)", peak)
	}

	// 5. Malformed streams return ErrInvalidCubemap.
	badInputs := []string{
		"",
		"NOT_RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 1\n",
		"#?RADIANCE\nFORMAT=32-bit_rle_xyze\n\n-Y 1 +X 1\n",
		"#?RADIANCE\n\n-Y 1 +X 1\n",
		"#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Z 1 +X 1\n",
		"#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 4 +X 4\n\x00\x00",
	}
	for i, bad := range badInputs {
		if _, err := render3d.DecodeHDR(strings.NewReader(bad)); !errors.Is(err, render3d.ErrInvalidCubemap) {
			t.Errorf("badInputs[%d]: err = %v, want ErrInvalidCubemap", i, err)
		}
	}
	if _, err := render3d.DecodeHDR(nil); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Errorf("DecodeHDR(nil) = %v, want ErrInvalidCubemap", err)
	}
	if _, err := render3d.CubemapFromHDR(strings.NewReader(""), 0); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Errorf("CubemapFromHDR(faceSize=0) = %v, want ErrInvalidCubemap", err)
	}
}

func TestHDRCubemapValidationAndBaking(t *testing.T) {
	// Validation edge cases.
	var nilCM *render3d.HDRCubemap
	if err := nilCM.Validate(); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("nil HDRCubemap Validate = %v, want ErrInvalidCubemap", err)
	}
	if _, err := render3d.NewHDRCubemap(0); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("NewHDRCubemap(0) = %v, want ErrInvalidCubemap", err)
	}
	bad, _ := render3d.NewHDRCubemap(4)
	bad.Set(gpu.CubeFacePosX, 0, 0, [3]float32{-0.1, 0, 0})
	if err := bad.Validate(); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("negative radiance Validate = %v, want ErrInvalidCubemap", err)
	}
	bad.Set(gpu.CubeFacePosX, 0, 0, [3]float32{float32(math.NaN()), 0, 0})
	if err := bad.Validate(); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("NaN radiance Validate = %v, want ErrInvalidCubemap", err)
	}

	// GradientSkyHDRCubemap preserves HDR values > 1.0 without clamping.
	sky, err := render3d.GradientSkyHDRCubemap([3]float32{2.0, 4.0, 8.0}, [3]float32{3.0, 2.0, 1.5}, [3]float32{0.2, 0.2, 0.2}, 17)
	if err != nil {
		t.Fatalf("GradientSkyHDRCubemap: %v", err)
	}
	top := sky.Sample(math3d.Vec3{Y: 1})
	if top[2] < 7.0 {
		t.Errorf("HDR gradient zenith B = %v, want ~8.0 (unclamped)", top[2])
	}

	// ComputeIrradianceSHHDR: uniform HDR radiance C -> E(n) == pi * C.
	uniformHDR, err := render3d.NewHDRCubemap(16)
	if err != nil {
		t.Fatalf("NewHDRCubemap: %v", err)
	}
	wantC := [3]float32{4.0, 2.5, 1.5}
	for face := range gpu.CubeFace(6) {
		for y := range 16 {
			for x := range 16 {
				uniformHDR.Set(face, x, y, wantC)
			}
		}
	}
	sh, err := render3d.ComputeIrradianceSHHDR(uniformHDR)
	if err != nil {
		t.Fatalf("ComputeIrradianceSHHDR: %v", err)
	}
	for _, d := range []math3d.Vec3{{X: 1}, {Y: 1}, {Z: -1}, {X: 1, Y: 2, Z: -1}} {
		got := sh.Evaluate(d)
		for ch := range 3 {
			want := float32(float64(wantC[ch]) * math.Pi)
			if !math3d.ApproxEqual(got[ch], want, 1e-2) {
				t.Errorf("uniform HDR SH Evaluate(%v) ch %d = %v, want %v", d, ch, got[ch], want)
			}
		}
	}

	// PrefilterEnvironmentHDR: uniform HDR radiance preserved across all mips; HDR sun blurs while keeping peak > 1.0.
	prefUni, err := render3d.PrefilterEnvironmentHDR(uniformHDR, render3d.PrefilterOptions{
		BaseSize:    16,
		MipLevels:   4,
		SampleCount: 32,
	})
	if err != nil {
		t.Fatalf("PrefilterEnvironmentHDR uniform: %v", err)
	}
	for lod := float32(0); lod <= 3; lod++ {
		got := prefUni.SampleLOD(math3d.Vec3{X: 0.3, Y: 0.8, Z: -0.5}, lod)
		for ch := range 3 {
			if !math3d.ApproxEqual(got[ch], wantC[ch], 0.05) {
				t.Errorf("uniform HDR prefilter lod %v ch %d = %v, want %v", lod, ch, got[ch], wantC[ch])
			}
		}
	}

	studio := iblHelmetStudioCubemap(t, 16)
	prefStudio, err := render3d.PrefilterEnvironmentHDR(studio, render3d.PrefilterOptions{
		BaseSize:    16,
		MipLevels:   5,
		SampleCount: 48,
	})
	if err != nil {
		t.Fatalf("PrefilterEnvironmentHDR studio: %v", err)
	}
	keyDir := math3d.Vec3{X: 0.52, Y: 0.58, Z: 0.63}.Norm()
	mip0Peak := prefStudio.SampleLOD(keyDir, 0)[0]
	mip4Peak := prefStudio.SampleLOD(keyDir, 4)[0]
	if !(mip0Peak > 8.0 && mip0Peak > mip4Peak*2.0) {
		t.Errorf("HDR prefilter peak mip0=%.2f mip4=%.2f, want mip0 > 8 and sharper than mip4", mip0Peak, mip4Peak)
	}

	// HDRCubemapFromCubemap promotes sRGB LDR cubemap to linear float32.
	ldr := &render3d.Cubemap{SRGB: true}
	for f := range 6 {
		ldr.Faces[f] = solidFace(4, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	}
	promoted, err := render3d.HDRCubemapFromCubemap(ldr)
	if err != nil {
		t.Fatalf("HDRCubemapFromCubemap: %v", err)
	}
	if s := promoted.Sample(math3d.Vec3{X: 1}); math.Abs(float64(s[0])-0.2158) > 0.01 {
		t.Errorf("promoted sRGB mid-gray = %v, want ~0.2158 linear", s)
	}
}

func TestUploadCubemapHDRAndSpecularBrightness(t *testing.T) {
	const size = 96
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	hdrEnv := iblHelmetStudioCubemap(t, 24)
	ldrEnv := iblHelmetClampHDRToLDR(hdrEnv)

	renderMirrorSphere := func(env *render3d.HDRCubemap, label string) (*image.RGBA, *render3d.GPUCubemap) {
		sh, err := render3d.ComputeIrradianceSHHDR(env)
		if err != nil {
			t.Fatalf("ComputeIrradianceSHHDR(%s): %v", label, err)
		}
		pref, err := render3d.PrefilterEnvironmentHDR(env, render3d.PrefilterOptions{
			BaseSize:    24,
			MipLevels:   5,
			SampleCount: 48,
		})
		if err != nil {
			t.Fatalf("PrefilterEnvironmentHDR(%s): %v", label, err)
		}
		prefGPU, err := render3d.UploadCubemapHDR(nil, dev, pref, render3d.CubemapUpload{Label: label + "-pref"})
		if err != nil {
			t.Fatalf("UploadCubemapHDR(%s): %v", label, err)
		}
		t.Cleanup(func() { _ = prefGPU.Close() })

		iblBuf, err := render3d.NewGPUIBLBuffer(nil, dev, prefGPU, render3d.NewIBLUniform(sh, prefGPU, 1.0), render3d.IBLBufferOptions{Label: label + "-ibl"})
		if err != nil {
			t.Fatalf("NewGPUIBLBuffer(%s): %v", label, err)
		}
		t.Cleanup(func() { _ = iblBuf.Close() })

		camBuf, err := render3d.NewCameraBuffer(nil, dev)
		if err != nil {
			t.Fatalf("NewCameraBuffer: %v", err)
		}
		t.Cleanup(func() { _ = camBuf.Close() })

		defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
		if err != nil {
			t.Fatalf("NewMaterialDefaults: %v", err)
		}
		t.Cleanup(func() { _ = defaults.Close() })

		chrome := render3d.DefaultMaterial()
		chrome.BaseColorFactor = [4]float32{0.95, 0.95, 0.95, 1}
		chrome.MetallicFactor = 1.0
		chrome.RoughnessFactor = 0.08
		gpuMat, err := render3d.NewGPUMaterial(nil, dev, defaults, chrome, render3d.MaterialUpload{Label: label + "-chrome"})
		if err != nil {
			t.Fatalf("NewGPUMaterial: %v", err)
		}
		t.Cleanup(func() { _ = gpuMat.Close() })

		hdrTarget, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{
			Label:       label + "-hdr",
			Width:       size,
			Height:      size,
			DepthFormat: gpu.FormatDepth24Plus,
		})
		if err != nil {
			t.Fatalf("NewHDRTarget: %v", err)
		}
		t.Cleanup(func() { _ = hdrTarget.Close() })

		pipe, err := render3d.NewMaterialIBLPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), iblBuf.Layout(), render3d.MaterialIBLPipelineOptions{
			Label:        label + "-pipe",
			ColorFormat:  hdrTarget.Format(),
			DepthFormat:  gpu.FormatDepth24Plus,
			LinearOutput: true,
		})
		if err != nil {
			t.Fatalf("NewMaterialIBLPipeline: %v", err)
		}
		tonemap, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{
			Label:        label + "-tm",
			TargetFormat: dev.SurfaceFormat(),
		})
		if err != nil {
			t.Fatalf("NewToneMapPass: %v", err)
		}
		t.Cleanup(func() { _ = tonemap.Close() })

		sphere, err := render3d.UploadMesh(nil, dev, uvSphereMesh(1.0, 28, 20), render3d.MeshUpload{Label: label + "-sphere"})
		if err != nil {
			t.Fatalf("UploadMesh: %v", err)
		}
		t.Cleanup(func() { _ = sphere.Close() })

		cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 2.8}, math3d.DegToRad(45), 1, 0.1, 20)
		cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
		camBuf.WriteCamera(cam)

		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(hdrTarget.RenderPassDesc(core.RGB(0x000000), 1))
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		gpuMat.Bind(pass)
		pass.SetBindGroup(render3d.MaterialIBLGroup, iblBuf.BindGroup())
		sphere.Draw(pass)
		pass.End()

		tmPass := enc.BeginRenderPass(gpu.RenderPassDesc{
			Label:            "tonemap",
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		})
		tmOpts := render3d.ToneMapOptions{Operator: render3d.ToneMapNeutral, Exposure: 0.45}
		if err := tonemap.Draw(tmPass, dev, hdrTarget.ColorTexture(), tmOpts); err != nil {
			t.Fatalf("tonemap.Draw: %v", err)
		}
		tmPass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return cloneRGBA(sr.Image()), prefGPU
	}

	hdrImg, prefGPU := renderMirrorSphere(hdrEnv, "hdr")
	ldrImg, _ := renderMirrorSphere(ldrEnv, "ldr")

	if prefGPU.Format() != gpu.FormatRGBA16Float {
		t.Fatalf("UploadCubemapHDR Format() = %s, want FormatRGBA16Float", prefGPU.Format())
	}
	// Verify ByteSize accounts for 8 bytes/texel across all 6 faces and 5 mip levels.
	var wantBytes uint64
	for lvl := range uint32(5) {
		mw, mh := gpu.MipSize(24, 24, lvl)
		wantBytes += 6 * uint64(mw) * uint64(mh) * 8
	}
	if prefGPU.ByteSize() != wantBytes {
		t.Errorf("GPUCubemap.ByteSize() = %d, want %d", prefGPU.ByteSize(), wantBytes)
	}

	// Compare peak specular highlight luminance on the sphere: HDR must be measurably brighter than LDR-clamped!
	peakLuma := func(img *image.RGBA) float64 {
		best := 0.0
		for y := range size {
			for x := range size {
				c := img.RGBAAt(x, y)
				l := 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
				if l > best {
					best = l
				}
			}
		}
		return best
	}
	hdrPeak := peakLuma(hdrImg)
	ldrPeak := peakLuma(ldrImg)
	if !(hdrPeak > ldrPeak+55.0) {
		t.Errorf("HDR specular peak luma = %.1f vs LDR-clamped = %.1f; expected HDR measurably brighter (> +55)", hdrPeak, ldrPeak)
	}

	// Verify UploadCubemapHDR GenerateMips and invalid format rejection.
	mipCube, err := render3d.UploadCubemapHDR(nil, dev, hdrEnv, render3d.CubemapUpload{Label: "genmips", GenerateMips: true})
	if err != nil {
		t.Fatalf("UploadCubemapHDR(GenerateMips): %v", err)
	}
	defer func() { _ = mipCube.Close() }()
	if want := gpu.MaxMipLevels(24, 24); mipCube.MipLevels() != want {
		t.Errorf("GenerateMips MipLevels() = %d, want %d", mipCube.MipLevels(), want)
	}
	if _, err := render3d.UploadCubemapHDR(nil, dev, hdrEnv, render3d.CubemapUpload{Format: gpu.FormatRGBA8UnormSRGB}); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Errorf("UploadCubemapHDR(RGBA8UnormSRGB) err = %v, want ErrInvalidCubemap", err)
	}
}

func TestMaterialIBLMatchesCPUReference(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	env, err := render3d.GradientSkyHDRCubemap([3]float32{1.2, 2.0, 3.6}, [3]float32{2.4, 1.8, 1.2}, [3]float32{0.2, 0.25, 0.3}, 16)
	if err != nil {
		t.Fatalf("GradientSkyHDRCubemap: %v", err)
	}
	sh, err := render3d.ComputeIrradianceSHHDR(env)
	if err != nil {
		t.Fatalf("ComputeIrradianceSHHDR: %v", err)
	}
	pref, err := render3d.PrefilterEnvironmentHDR(env, render3d.PrefilterOptions{BaseSize: 16, MipLevels: 4, SampleCount: 48})
	if err != nil {
		t.Fatalf("PrefilterEnvironmentHDR: %v", err)
	}
	prefGPU, err := render3d.UploadCubemapHDR(nil, dev, pref, render3d.CubemapUpload{Label: "mat-ibl-pref"})
	if err != nil {
		t.Fatalf("UploadCubemapHDR: %v", err)
	}
	t.Cleanup(func() { _ = prefGPU.Close() })

	iblBuf, err := render3d.NewGPUIBLBuffer(nil, dev, prefGPU, render3d.NewIBLUniform(sh, prefGPU, 1.0), render3d.IBLBufferOptions{Label: "mat-ibl"})
	if err != nil {
		t.Fatalf("NewGPUIBLBuffer: %v", err)
	}
	t.Cleanup(func() { _ = iblBuf.Close() })

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })

	// Create 1x1 solid textures for all 5 PBR channels (baseColor, MR, tilted normal, occlusion, emissive).
	uploadSolid := func(c color.NRGBA, srgb bool, label string) *render3d.GPUTexture {
		tex, err := render3d.UploadTexture(nil, dev, solidImage(2, 2, c), render3d.TextureUpload{Label: label, SRGB: srgb})
		if err != nil {
			t.Fatalf("UploadTexture(%s): %v", label, err)
		}
		t.Cleanup(func() { _ = tex.Close() })
		return tex
	}
	bcCol := color.NRGBA{R: 220, G: 160, B: 90, A: 255}
	mrCol := color.NRGBA{R: 0, G: 102, B: 204, A: 255}   // roughness=0.4, metallic=0.8
	nmCol := color.NRGBA{R: 153, G: 166, B: 230, A: 255} // tilted normal
	ocCol := color.NRGBA{R: 179, G: 179, B: 179, A: 255} // ao=0.7
	emCol := color.NRGBA{R: 50, G: 180, B: 210, A: 255}

	mat := render3d.DefaultMaterial()
	mat.BaseColorFactor = [4]float32{0.9, 0.95, 1.0, 1.0}
	mat.BaseColorTexture = uploadSolid(bcCol, true, "bc")
	mat.MetallicFactor = 0.9
	mat.RoughnessFactor = 0.85
	mat.MetallicRoughnessTexture = uploadSolid(mrCol, false, "mr")
	mat.NormalTexture = uploadSolid(nmCol, false, "nm")
	mat.NormalScale = 0.8
	mat.OcclusionTexture = uploadSolid(ocCol, false, "occ")
	mat.OcclusionStrength = 0.75
	mat.EmissiveFactor = [3]float32{0.3, 0.4, 0.5}
	mat.EmissiveTexture = uploadSolid(emCol, true, "em")

	gpuMat, err := render3d.NewGPUMaterial(nil, dev, defaults, mat, render3d.MaterialUpload{Label: "full-pbr-ibl"})
	if err != nil {
		t.Fatalf("NewGPUMaterial: %v", err)
	}
	t.Cleanup(func() { _ = gpuMat.Close() })

	quad, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3Zero(), 2.4, 2.4), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	pipe, err := render3d.NewMaterialIBLPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), iblBuf.Layout(), render3d.MaterialIBLPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewMaterialIBLPipeline: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(45), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	gpuMat.Bind(pass)
	pass.SetBindGroup(render3d.MaterialIBLGroup, iblBuf.BindGroup())
	quad.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	got := sr.Image().RGBAAt(size/2, size/2)
	srgb := func(u uint8) float32 { return render3d.SRGBToLinearChannel(float32(u) / 255.0) }
	eval := render3d.EvaluateMaterialIBL(render3d.MaterialEvalInput{
		Material:                   mat,
		BaseColorSample:            [4]float32{srgb(bcCol.R), srgb(bcCol.G), srgb(bcCol.B), 1},
		HasBaseColorSample:         true,
		MetallicSample:             float32(mrCol.B) / 255.0,
		RoughnessSample:            float32(mrCol.G) / 255.0,
		HasMetallicRoughnessSample: true,
		NormalSample:               [3]float32{float32(nmCol.R) / 255.0, float32(nmCol.G) / 255.0, float32(nmCol.B) / 255.0},
		HasNormalSample:            true,
		OcclusionSample:            float32(ocCol.R) / 255.0,
		HasOcclusionSample:         true,
		EmissiveSample:             [3]float32{srgb(emCol.R), srgb(emCol.G), srgb(emCol.B)},
		HasEmissiveSample:          true,
		Normal:                     math3d.Vec3{Z: 1},
		Tangent:                    math3d.Vec3{X: 1},
		Bitangent:                  math3d.Vec3{Y: 1},
		ViewDir:                    math3d.Vec3{Z: 1},
		FrontFacing:                true,
	}, sh, pref, 1.0)
	wantR := byte(math.Round(float64(eval.SRGB.X) * 255))
	wantG := byte(math.Round(float64(eval.SRGB.Y) * 255))
	wantB := byte(math.Round(float64(eval.SRGB.Z) * 255))
	if absDiffU8(got.R, wantR) > 5 || absDiffU8(got.G, wantG) > 5 || absDiffU8(got.B, wantB) > 5 {
		t.Errorf("MaterialIBLWGSL center pixel = %v, want CPU EvaluateMaterialIBL = (%d,%d,%d)", got, wantR, wantG, wantB)
	}

	// Verify SkyboxPipelineDesc.ToneMap matches Reinhard + gamma 2.2 on an HDR skybox.
	skyGPU, err := render3d.UploadCubemapHDR(nil, dev, env, render3d.CubemapUpload{Label: "tm-sky"})
	if err != nil {
		t.Fatalf("UploadCubemapHDR sky: %v", err)
	}
	t.Cleanup(func() { _ = skyGPU.Close() })
	skyPipe, err := render3d.NewSkyboxPipeline(nil, dev, render3d.SkyboxPipelineDesc{
		Label:       "tm-sky-pipe",
		ColorFormat: dev.SurfaceFormat(),
		DepthFormat: gpu.FormatDepth24Plus,
		ToneMap:     true,
	})
	if err != nil {
		t.Fatalf("NewSkyboxPipeline(ToneMap): %v", err)
	}
	t.Cleanup(func() { _ = skyPipe.Close() })
	skyGroup, err := skyPipe.BindCubemap(nil, skyGPU)
	if err != nil {
		t.Fatalf("BindCubemap: %v", err)
	}
	t.Cleanup(func() { _ = skyGroup.Close() })
	skyU := render3d.NewSkyboxUniform(cam)
	skyU.Exposure = 1.0
	skyPipe.Write(skyU)

	enc = dev.BeginEncoder()
	pass = enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	skyPipe.Draw(pass, skyGroup)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	skyCenter := sr.Image().RGBAAt(size/2, size/2)
	// Camera looks along -Z: compare GPU tone-mapped skybox against exact CPU sample of env along -Z.
	sampled := env.Sample(math3d.Vec3{Z: -1})
	tmCh := func(v float32) byte {
		c := max(float64(v), 0)
		return byte(math.Round(math.Pow(c/(c+1.0), 1.0/2.2) * 255))
	}
	wantSkyR := tmCh(sampled[0])
	wantSkyG := tmCh(sampled[1])
	wantSkyB := tmCh(sampled[2])
	if absDiffU8(skyCenter.R, wantSkyR) > 6 || absDiffU8(skyCenter.G, wantSkyG) > 6 || absDiffU8(skyCenter.B, wantSkyB) > 6 {
		t.Errorf("ToneMap skybox center = %v, want ~(%d,%d,%d)", skyCenter, wantSkyR, wantSkyG, wantSkyB)
	}
}

func TestDamagedHelmetHDRIBLReference(t *testing.T) {
	doc, helmetMesh := iblHelmetLoad(t)

	const (
		width  = 192
		height = 192
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	hdrEnv := iblHelmetStudioCubemap(t, 32)
	ldrEnv := iblHelmetClampHDRToLDR(hdrEnv)

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })
	matSet, err := render3d.UploadGLTFMaterials(nil, dev, defaults, doc, render3d.GLTFMaterialUpload{
		Label: "DamagedHelmet",
	})
	if err != nil {
		t.Fatalf("UploadGLTFMaterials: %v", err)
	}
	t.Cleanup(func() { _ = matSet.Close() })
	gpuMesh, err := render3d.UploadMesh(nil, dev, helmetMesh, render3d.MeshUpload{Label: "DamagedHelmet"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = gpuMesh.Close() })

	hdrTarget, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{
		Label:       "helmet-hdr",
		Width:       width,
		Height:      height,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewHDRTarget: %v", err)
	}
	t.Cleanup(func() { _ = hdrTarget.Close() })

	skyPipe, err := render3d.NewSkyboxPipeline(nil, dev, render3d.SkyboxPipelineDesc{
		Label:       "helmet-skybox",
		ColorFormat: hdrTarget.Format(),
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewSkyboxPipeline: %v", err)
	}
	t.Cleanup(func() { _ = skyPipe.Close() })

	tonemap, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{
		Label:        "helmet-tonemap",
		TargetFormat: dev.SurfaceFormat(),
	})
	if err != nil {
		t.Fatalf("NewToneMapPass: %v", err)
	}
	t.Cleanup(func() { _ = tonemap.Close() })

	center := gpuMesh.Bounds().Center()
	cam := scene3d.NewPerspectiveCamera(
		center.Add(math3d.Vec3{X: 0.85, Y: 0.14, Z: 3.10}),
		math3d.DegToRad(40),
		float32(width)/float32(height),
		0.1,
		50,
	)
	cam.LookAt(center, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	tmOpts := render3d.ToneMapOptions{
		Operator: render3d.ToneMapNeutral,
		Exposure: 0.72,
	}
	helmetMat := matSet.MaterialForPrimitive(doc.Meshes[0].Primitives[0], -1)

	renderWithEnv := func(env *render3d.HDRCubemap, iblIntensity float32, drawSkybox bool, label string) *image.RGBA {
		sh, err := render3d.ComputeIrradianceSHHDR(env)
		if err != nil {
			t.Fatalf("ComputeIrradianceSHHDR(%s): %v", label, err)
		}
		pref, err := render3d.PrefilterEnvironmentHDR(env, render3d.PrefilterOptions{
			BaseSize:    32,
			MipLevels:   5,
			SampleCount: 48,
		})
		if err != nil {
			t.Fatalf("PrefilterEnvironmentHDR(%s): %v", label, err)
		}
		skyGPU, err := render3d.UploadCubemapHDR(nil, dev, env, render3d.CubemapUpload{Label: label + "-sky"})
		if err != nil {
			t.Fatalf("UploadCubemapHDR sky(%s): %v", label, err)
		}
		defer func() { _ = skyGPU.Close() }()
		prefGPU, err := render3d.UploadCubemapHDR(nil, dev, pref, render3d.CubemapUpload{Label: label + "-pref"})
		if err != nil {
			t.Fatalf("UploadCubemapHDR pref(%s): %v", label, err)
		}
		defer func() { _ = prefGPU.Close() }()

		iblBuf, err := render3d.NewGPUIBLBuffer(nil, dev, prefGPU, render3d.NewIBLUniform(sh, prefGPU, iblIntensity), render3d.IBLBufferOptions{Label: label + "-ibl"})
		if err != nil {
			t.Fatalf("NewGPUIBLBuffer(%s): %v", label, err)
		}
		defer func() { _ = iblBuf.Close() }()

		skyGroup, err := skyPipe.BindCubemap(nil, skyGPU)
		if err != nil {
			t.Fatalf("BindCubemap(%s): %v", label, err)
		}
		defer func() { _ = skyGroup.Close() }()

		matIBLPipe, err := render3d.NewMaterialIBLPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), iblBuf.Layout(), render3d.MaterialIBLPipelineOptions{
			Label:        label + "-mat-ibl",
			ColorFormat:  hdrTarget.Format(),
			DepthFormat:  gpu.FormatDepth24Plus,
			LinearOutput: true,
		})
		if err != nil {
			t.Fatalf("NewMaterialIBLPipeline(%s): %v", label, err)
		}

		skyU := render3d.NewSkyboxUniform(cam)
		skyU.Exposure = 0.65
		skyPipe.Write(skyU)

		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(hdrTarget.RenderPassDesc(core.RGB(0x000000), 1))
		pass.SetPipeline(matIBLPipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		helmetMat.Bind(pass)
		pass.SetBindGroup(render3d.MaterialIBLGroup, iblBuf.BindGroup())
		gpuMesh.Draw(pass)
		if drawSkybox {
			skyPipe.Draw(pass, skyGroup)
		}
		pass.End()

		tmPass := enc.BeginRenderPass(gpu.RenderPassDesc{
			Label:            "tonemap",
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		})
		if err := tonemap.Draw(tmPass, dev, hdrTarget.ColorTexture(), tmOpts); err != nil {
			t.Fatalf("tonemap.Draw(%s): %v", label, err)
		}
		tmPass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer(%s): %v", label, err)
		}
		return cloneRGBA(sr.Image())
	}

	hdrImg := renderWithEnv(hdrEnv, 1.0, true, "hdr-studio")
	assertReferenceAt(t, filepath.Join(damagedHelmetDir, "reference_ibl.png"), hdrImg)

	// 1. Skybox covers the background behind the helmet (zero black clear-color leaks, upper sky brighter than lower studio floor).
	assertNoClearPixels(t, hdrImg)
	topSkyLuma := iblHelmetMeanLuma(hdrImg, 4, 4, 24, 24)
	botFloorLuma := iblHelmetMeanLuma(hdrImg, 4, height-24, 24, height-4)
	if !(topSkyLuma > botFloorLuma+40) {
		t.Errorf("skybox top-left luma %.1f should be brighter than bottom-left floor %.1f", topSkyLuma, botFloorLuma)
	}

	// 2. Helmet renders its glowing cyan visor HUD.
	cyanVisorPixels := 0
	for y := height / 4; y < height*3/4; y++ {
		for x := width / 4; x < width*3/4; x++ {
			c := hdrImg.RGBAAt(x, y)
			if c.G >= 120 && int(c.G) > int(c.R)+20 && int(c.B) > int(c.R)+10 {
				cyanVisorPixels++
			}
		}
	}
	if cyanVisorPixels < 50 {
		t.Errorf("DamagedHelmet glowing cyan visor HUD pixels = %d, want >= 50", cyanVisorPixels)
	}

	// 3. HDR environment produces measurably brighter specular highlights on the helmet than the same environment clamped to LDR [0, 1].
	hdrNoSky := renderWithEnv(hdrEnv, 1.0, false, "hdr-nosky")
	ldrNoSky := renderWithEnv(ldrEnv, 1.0, false, "ldr-nosky")
	var hdrBright, ldrBright int
	var hdrMaxLuma, ldrMaxLuma float64
	for y := range height {
		for x := range width {
			hc := hdrNoSky.RGBAAt(x, y)
			lc := ldrNoSky.RGBAAt(x, y)
			if hc.R == 0 && hc.G == 0 && hc.B == 0 {
				continue
			}
			hl := 0.2126*float64(hc.R) + 0.7152*float64(hc.G) + 0.0722*float64(hc.B)
			ll := 0.2126*float64(lc.R) + 0.7152*float64(lc.G) + 0.0722*float64(lc.B)
			if hl > hdrMaxLuma {
				hdrMaxLuma = hl
			}
			if ll > ldrMaxLuma {
				ldrMaxLuma = ll
			}
			if hl >= 190 {
				hdrBright++
			}
			if ll >= 190 {
				ldrBright++
			}
		}
	}
	if !(hdrMaxLuma > ldrMaxLuma+25 && hdrBright > ldrBright+40) {
		t.Errorf("HDR vs LDR helmet highlights: maxLuma hdr=%.1f ldr=%.1f, brightPixels(>=190) hdr=%d ldr=%d",
			hdrMaxLuma, ldrMaxLuma, hdrBright, ldrBright)
	}

	// 4. Turning IBL intensity to 0 leaves only emissive HUD on the helmet, proving environment lighting drives surface shading.
	zeroIBLNoSky := renderWithEnv(hdrEnv, 0.0, false, "zero-ibl")
	if diff := countDifferentPixels(hdrNoSky, zeroIBLNoSky); diff < width*height/5 {
		t.Errorf("IBL vs zero-IBL helmet differed by only %d pixels, want >= %d", diff, width*height/5)
	}

	// 5. Swapping to a warm amber HDR environment shifts the helmet's metallic reflection hue (red/blue ratio rises).
	warmEnv, err := render3d.GradientSkyHDRCubemap([3]float32{4.0, 1.5, 0.3}, [3]float32{3.0, 1.2, 0.2}, [3]float32{0.4, 0.15, 0.05}, 16)
	if err != nil {
		t.Fatalf("GradientSkyHDRCubemap warm: %v", err)
	}
	warmNoSky := renderWithEnv(warmEnv, 1.0, false, "warm-ibl")
	var hdrRB, warmRB float64
	var helmetPix int
	for y := range height {
		for x := range width {
			hc := hdrNoSky.RGBAAt(x, y)
			wc := warmNoSky.RGBAAt(x, y)
			if hc.R == 0 && hc.G == 0 && hc.B == 0 {
				continue
			}
			hdrRB += float64(hc.R) - float64(hc.B)
			warmRB += float64(wc.R) - float64(wc.B)
			helmetPix++
		}
	}
	if helmetPix == 0 || warmRB/float64(helmetPix) <= hdrRB/float64(helmetPix)+20 {
		t.Errorf("warm env R-B mean = %.1f vs studio R-B mean = %.1f; expected warm reflections markedly redder",
			warmRB/float64(max(helmetPix, 1)), hdrRB/float64(max(helmetPix, 1)))
	}
}

func TestDamagedHelmetHDRIBLInGPUViewProfilesAndDeviceLoss(t *testing.T) {
	doc, helmetMesh := iblHelmetLoad(t)
	hdrEnv := iblHelmetStudioCubemap(t, 16)
	sh, err := render3d.ComputeIrradianceSHHDR(hdrEnv)
	if err != nil {
		t.Fatalf("ComputeIrradianceSHHDR: %v", err)
	}
	pref, err := render3d.PrefilterEnvironmentHDR(hdrEnv, render3d.PrefilterOptions{
		BaseSize:    16,
		MipLevels:   4,
		SampleCount: 32,
	})
	if err != nil {
		t.Fatalf("PrefilterEnvironmentHDR: %v", err)
	}

	const size = 96
	runProfile := func(t *testing.T, retained bool, extraOpts ...nsgotest.Option) (*image.RGBA, *nsgotest.App) {
		t.Helper()
		var (
			camBuf    *render3d.CameraBuffer
			defaults  *render3d.MaterialDefaults
			matSet    *render3d.GLTFMaterialSet
			gpuMesh   *render3d.GPUMesh
			skyGPU    *render3d.GPUCubemap
			prefGPU   *render3d.GPUCubemap
			iblBuf    *render3d.GPUIBLBuffer
			skyPipe   *render3d.SkyboxPipeline
			skyGroup  *gpu.BindGroup
			hdrTarget *render3d.HDRTarget
			matPipe   *gpu.Pipeline
			tonemap   *render3d.ToneMapPass
			cam       *scene3d.Camera
		)
		opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, extraOpts...)
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				TestID:      "viewport",
				Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
				DepthFormat: gpu.FormatDepth24Plus,
				OnReady: func(dev *gpu.Device) {
					var err error
					if !retained || camBuf == nil {
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
					} else {
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer restore: %v", err)
						}
					}
					if !retained || defaults == nil {
						defaults, err = render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Label: "helmet-def", Retained: retained})
						if err != nil {
							t.Fatalf("NewMaterialDefaults: %v", err)
						}
					}
					if !retained || matSet == nil {
						matSet, err = render3d.UploadGLTFMaterials(cx, dev, defaults, doc, render3d.GLTFMaterialUpload{Label: "DamagedHelmet", Retained: retained})
						if err != nil {
							t.Fatalf("UploadGLTFMaterials: %v", err)
						}
					}
					if !retained || gpuMesh == nil {
						gpuMesh, err = render3d.UploadMesh(cx, dev, helmetMesh, render3d.MeshUpload{Label: "DamagedHelmet", Retained: retained})
						if err != nil {
							t.Fatalf("UploadMesh: %v", err)
						}
					}
					if !retained || skyGPU == nil {
						skyGPU, err = render3d.UploadCubemapHDR(cx, dev, hdrEnv, render3d.CubemapUpload{Label: "sky-hdr", Retained: retained})
						if err != nil {
							t.Fatalf("UploadCubemapHDR sky: %v", err)
						}
					}
					if !retained || prefGPU == nil {
						prefGPU, err = render3d.UploadCubemapHDR(cx, dev, pref, render3d.CubemapUpload{Label: "pref-hdr", Retained: retained})
						if err != nil {
							t.Fatalf("UploadCubemapHDR pref: %v", err)
						}
					}
					if !retained || iblBuf == nil {
						iblBuf, err = render3d.NewGPUIBLBuffer(cx, dev, prefGPU, render3d.NewIBLUniform(sh, prefGPU, 1.0), render3d.IBLBufferOptions{Label: "ibl-hdr", Retained: retained})
						if err != nil {
							t.Fatalf("NewGPUIBLBuffer: %v", err)
						}
					}
					if !retained || hdrTarget == nil {
						hdrTarget, err = render3d.NewHDRTarget(cx, dev, render3d.HDRTargetDesc{
							Label:       "hdr-rt",
							Width:       size,
							Height:      size,
							DepthFormat: gpu.FormatDepth24Plus,
							Retained:    retained,
						})
						if err != nil {
							t.Fatalf("NewHDRTarget: %v", err)
						}
					} else if err := hdrTarget.Resize(dev, size, size); err != nil {
						t.Fatalf("HDRTarget.Resize restore: %v", err)
					}
					if !retained || skyPipe == nil {
						skyPipe, err = render3d.NewSkyboxPipeline(cx, dev, render3d.SkyboxPipelineDesc{
							Label:       "sky-pipe",
							ColorFormat: hdrTarget.Format(),
							DepthFormat: gpu.FormatDepth24Plus,
							Retained:    retained,
						})
						if err != nil {
							t.Fatalf("NewSkyboxPipeline: %v", err)
						}
					} else {
						skyPipe.SetDevice(dev)
					}
					skyGroup, err = skyPipe.BindCubemap(cx, skyGPU)
					if err != nil {
						t.Fatalf("BindCubemap: %v", err)
					}
					if !retained || matPipe == nil {
						matPipe, err = render3d.NewMaterialIBLPipeline(cx, dev, camBuf.Layout(), defaults.Layout(), iblBuf.Layout(), render3d.MaterialIBLPipelineOptions{
							Label:        "mat-ibl-pipe",
							ColorFormat:  hdrTarget.Format(),
							DepthFormat:  gpu.FormatDepth24Plus,
							LinearOutput: true,
							Retained:     retained,
						})
						if err != nil {
							t.Fatalf("NewMaterialIBLPipeline: %v", err)
						}
					} else {
						// Recreate pipeline if camera layout was recreated on restore.
						matPipe, err = render3d.NewMaterialIBLPipeline(cx, dev, camBuf.Layout(), defaults.Layout(), iblBuf.Layout(), render3d.MaterialIBLPipelineOptions{
							Label:        "mat-ibl-pipe",
							ColorFormat:  hdrTarget.Format(),
							DepthFormat:  gpu.FormatDepth24Plus,
							LinearOutput: true,
							Retained:     retained,
						})
						if err != nil {
							t.Fatalf("NewMaterialIBLPipeline restore: %v", err)
						}
					}
					if !retained || tonemap == nil {
						tonemap, err = render3d.NewToneMapPass(cx, dev, render3d.ToneMapPassDesc{
							Label:        "tm-pass",
							TargetFormat: dev.SurfaceFormat(),
							Retained:     retained,
						})
						if err != nil {
							t.Fatalf("NewToneMapPass: %v", err)
						}
					}
					center := gpuMesh.Bounds().Center()
					cam = scene3d.NewPerspectiveCamera(center.Add(math3d.Vec3{X: 0.85, Y: 0.14, Z: 3.10}), math3d.DegToRad(40), 1, 0.1, 50)
					cam.LookAt(center, math3d.Vec3Up())
					camBuf.WriteCamera(cam)
					skyU := render3d.NewSkyboxUniform(cam)
					skyU.Exposure = 0.65
					skyPipe.Write(skyU)
				},
				OnFrame: func(frame *gpu.Frame) {
					pass := frame.BeginRenderPass(hdrTarget.RenderPassDesc(core.RGB(0x000000), 1))
					pass.SetPipeline(matPipe)
					pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
					matSet.MaterialForPrimitive(doc.Meshes[0].Primitives[0], -1).Bind(pass)
					pass.SetBindGroup(render3d.MaterialIBLGroup, iblBuf.BindGroup())
					gpuMesh.Draw(pass)
					skyPipe.Draw(pass, skyGroup)
					pass.End()

					tmPass := frame.BeginRenderPass(gpu.RenderPassDesc{Label: "tonemap-display"})
					tmPass.ClearColor(core.RGB(0x000000))
					if err := tonemap.DrawTarget(tmPass, frame.Device(), hdrTarget, render3d.ToneMapOptions{
						Operator: render3d.ToneMapNeutral,
						Exposure: 0.72,
					}); err != nil {
						t.Fatalf("tonemap.DrawTarget: %v", err)
					}
					tmPass.End()
					frame.Submit()
				},
			})
		}, opts...)

		app.Advance(16 * time.Millisecond)
		img, err := app.GPUReadPixels("id:viewport")
		if err != nil {
			t.Fatalf("GPUReadPixels: %v", err)
		}
		assertNoClearPixels(t, img)
		return cloneRGBA(img), app
	}

	var fullImg, glesImg *image.RGBA
	t.Run("full", func(t *testing.T) {
		fullImg, _ = runProfile(t, false)
	})
	t.Run("webgl2-gles3", func(t *testing.T) {
		glesImg, _ = runProfile(t, false, nsgotest.NoGPUCompute())
	})
	if fullImg != nil && glesImg != nil {
		if diff := countDifferentPixels(fullImg, glesImg); diff != 0 {
			t.Errorf("full vs webgl2-gles3 DamagedHelmet HDR IBL differed by %d pixels, want 0", diff)
		}
	}

	t.Run("retained-device-loss", func(t *testing.T) {
		before, app := runProfile(t, true)
		app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
		app.RestoreGPUDevice("id:viewport")
		app.Advance(16 * time.Millisecond)
		after, err := app.GPUReadPixels("id:viewport")
		if err != nil {
			t.Fatalf("GPUReadPixels after restore: %v", err)
		}
		if diff := countDifferentPixels(before, after); diff != 0 {
			t.Errorf("DamagedHelmet HDR IBL post-restore frame differed by %d pixels, want 0", diff)
		}
	})
}

func TestMaterialIBLPipelineValidation(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })
	env, _ := render3d.NewHDRCubemap(4)
	cube, err := render3d.UploadCubemapHDR(nil, dev, env, render3d.CubemapUpload{})
	if err != nil {
		t.Fatalf("UploadCubemapHDR: %v", err)
	}
	t.Cleanup(func() { _ = cube.Close() })
	iblBuf, err := render3d.NewGPUIBLBuffer(nil, dev, cube, render3d.NewIBLUniform(render3d.IrradianceSH{}, cube, 1.0), render3d.IBLBufferOptions{})
	if err != nil {
		t.Fatalf("NewGPUIBLBuffer: %v", err)
	}
	t.Cleanup(func() { _ = iblBuf.Close() })

	if _, err := render3d.NewMaterialIBLPipeline(nil, nil, camBuf.Layout(), defaults.Layout(), iblBuf.Layout(), render3d.MaterialIBLPipelineOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("nil device err = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewMaterialIBLPipeline(nil, dev, nil, defaults.Layout(), iblBuf.Layout(), render3d.MaterialIBLPipelineOptions{}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("nil camLayout err = %v, want ErrInvalidDescriptor", err)
	}
	if _, err := render3d.NewMaterialIBLPipeline(nil, dev, camBuf.Layout(), nil, iblBuf.Layout(), render3d.MaterialIBLPipelineOptions{}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("nil matLayout err = %v, want ErrInvalidDescriptor", err)
	}
	if _, err := render3d.NewMaterialIBLPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), nil, render3d.MaterialIBLPipelineOptions{}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("nil iblLayout err = %v, want ErrInvalidDescriptor", err)
	}
	if _, err := render3d.NewMaterialIBLPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), iblBuf.Layout(), render3d.MaterialIBLPipelineOptions{AlphaMode: render3d.AlphaMode(99)}); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("bad AlphaMode err = %v, want ErrInvalidMaterial", err)
	}
	// Verify AlphaMask, AlphaBlend, and DoubleSided pipelines compile cleanly.
	for _, mode := range []render3d.AlphaMode{render3d.AlphaOpaque, render3d.AlphaMask, render3d.AlphaBlend} {
		for _, ds := range []bool{false, true} {
			if _, err := render3d.NewMaterialIBLPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), iblBuf.Layout(), render3d.MaterialIBLPipelineOptions{
				Label:       fmt.Sprintf("mat-ibl-%s-ds%v", mode, ds),
				DepthFormat: gpu.FormatDepth24Plus,
				AlphaMode:   mode,
				DoubleSided: ds,
			}); err != nil {
				t.Errorf("NewMaterialIBLPipeline(mode=%v, ds=%v): %v", mode, ds, err)
			}
		}
	}
}
