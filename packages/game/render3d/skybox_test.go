package render3d_test

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// testIBLGroup is where iblSphereWGSL binds the IBL resources, exercising IBLWGSLForGroup.
const testIBLGroup = 2

// iblSphereWGSL shades a Mesh using CameraWGSL (@group(0)), a per-draw material uniform (@group(1)), and IBLWGSLForGroup(testIBLGroup).
const iblSphereWGSL = render3d.CameraWGSL + `
struct MaterialParams {
	model: mat4x4<f32>,
	base_color: vec4<f32>,
	metallic: f32,
	roughness: f32,
	ao: f32,
	_pad: f32,
}

@group(1) @binding(0) var<uniform> mat: MaterialParams;
` + "\n"

func makeIBLSphereWGSL() string {
	return iblSphereWGSL + render3d.IBLWGSLForGroup(testIBLGroup) + `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	let world = mat.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world;
	out.world_pos = world.xyz;
	out.world_normal = normalize((mat.model * vec4<f32>(in.normal, 0.0)).xyz);
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let n = normalize(in.world_normal);
	let v = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let lit = evaluate_ibl(n, v, mat.base_color.rgb, mat.metallic, mat.roughness, mat.ao);
	return vec4<f32>(clamp(lit, vec3<f32>(0.0), vec3<f32>(1.0)), 1.0);
}
`
}

func solidFace(size int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func cardinalCubemap(size int) *render3d.Cubemap {
	return &render3d.Cubemap{
		Faces: [6]*image.RGBA{
			gpu.CubeFacePosX: solidFace(size, color.RGBA{R: 240, G: 40, B: 40, A: 255}),  // +X Red
			gpu.CubeFaceNegX: solidFace(size, color.RGBA{R: 40, G: 240, B: 40, A: 255}),  // -X Green
			gpu.CubeFacePosY: solidFace(size, color.RGBA{R: 50, G: 90, B: 245, A: 255}),  // +Y Blue
			gpu.CubeFaceNegY: solidFace(size, color.RGBA{R: 235, G: 215, B: 45, A: 255}), // -Y Yellow
			gpu.CubeFacePosZ: solidFace(size, color.RGBA{R: 40, G: 225, B: 235, A: 255}), // +Z Cyan
			gpu.CubeFaceNegZ: solidFace(size, color.RGBA{R: 225, G: 55, B: 225, A: 255}), // -Z Magenta
		},
	}
}

// studioEnvironmentCubemap constructs a deterministic high-contrast studio environment with a bright warm key light in the upper-right hemisphere, a sky-blue upper dome, a subtle horizon glow, and a cool dark studio floor so reflections and diffuse irradiance clearly reveal roughness and metallicity.
func studioEnvironmentCubemap(size int) *render3d.Cubemap {
	cm, err := render3d.GradientSkyCubemap(
		[3]float32{0.18, 0.42, 0.88}, // zenith: rich blue sky
		[3]float32{0.82, 0.72, 0.58}, // horizon: warm haze
		[3]float32{0.10, 0.12, 0.16}, // ground: cool slate
		size,
	)
	if err != nil {
		panic(err)
	}
	sunDir := math3d.Vec3{X: 0.45, Y: 0.65, Z: 0.61}.Norm()
	invSize := 1.0 / float64(size)
	for face := range gpu.CubeFace(6) {
		img := cm.Faces[face]
		for py := range size {
			v := float32((float64(py) + 0.5) * invSize)
			for px := range size {
				u := float32((float64(px) + 0.5) * invSize)
				d := render3d.CubeFaceDirection(face, u, v)
				sunDot := max(float64(d.Dot(sunDir)), 0.0)
				sunGlow := math.Pow(sunDot, 24.0)
				off := img.PixOffset(px, py)
				r := min(float64(img.Pix[off+0])/255.0+sunGlow*0.95, 1.0)
				g := min(float64(img.Pix[off+1])/255.0+sunGlow*0.85, 1.0)
				b := min(float64(img.Pix[off+2])/255.0+sunGlow*0.60, 1.0)
				img.Pix[off+0] = byte(math.Round(r * 255.0))
				img.Pix[off+1] = byte(math.Round(g * 255.0))
				img.Pix[off+2] = byte(math.Round(b * 255.0))
			}
		}
	}
	return cm
}

func encodeMaterialParams(model math3d.Mat4, baseColor [3]float32, metallic, roughness, ao float32) []byte {
	var buf [96]byte
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(f))
		off += 4
	}
	for _, f := range model {
		put(f)
	}
	put(baseColor[0])
	put(baseColor[1])
	put(baseColor[2])
	put(1)
	put(metallic)
	put(roughness)
	put(ao)
	put(0)
	return buf[:]
}

func TestCubeFaceDirectionRoundTrip(t *testing.T) {
	for face := range gpu.CubeFace(6) {
		for _, v := range []float32{0.1, 0.35, 0.5, 0.72, 0.9} {
			for _, u := range []float32{0.1, 0.35, 0.5, 0.72, 0.9} {
				dir := render3d.CubeFaceDirection(face, u, v)
				if !math3d.ApproxEqual(dir.Len(), 1, 1e-5) {
					t.Fatalf("face %d (%v,%v) dir length = %v, want 1", face, u, v, dir.Len())
				}
				gotFace, gotU, gotV := render3d.CubeDirectionToFaceUV(dir)
				if gotFace != face || !math3d.ApproxEqual(gotU, u, 1e-4) || !math3d.ApproxEqual(gotV, v, 1e-4) {
					t.Fatalf("round-trip face %d (%v,%v) -> dir %v -> face %d (%v,%v)",
						face, u, v, dir, gotFace, gotU, gotV)
				}
			}
		}
	}
}

func TestCubemapFromImagesAndValidation(t *testing.T) {
	var imgs [6]image.Image
	for i := range 6 {
		imgs[i] = solidFace(8, color.RGBA{R: byte(30 * (i + 1)), G: 100, B: 200, A: 255})
	}
	cm, err := render3d.CubemapFromImages(imgs, true)
	if err != nil {
		t.Fatalf("CubemapFromImages: %v", err)
	}
	if err := cm.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cm.Size() != 8 || cm.MipLevels() != 1 || !cm.SRGB {
		t.Fatalf("got size=%d mips=%d srgb=%v, want 8/1/true", cm.Size(), cm.MipLevels(), cm.SRGB)
	}

	// Sub-image with non-zero Bounds.Min must normalize cleanly.
	big := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 4; y < 12; y++ {
		for x := 4; x < 12; x++ {
			big.SetRGBA(x, y, color.RGBA{R: 200, G: 50, B: 10, A: 255})
		}
	}
	sub := big.SubImage(image.Rect(4, 4, 12, 12))
	for i := range 6 {
		imgs[i] = sub
	}
	subCM, err := render3d.CubemapFromImages(imgs, false)
	if err != nil {
		t.Fatalf("CubemapFromImages sub-image: %v", err)
	}
	got := subCM.Sample(math3d.Vec3{X: 1})
	if !math3d.ApproxEqual(got[0], 200.0/255.0, 0.01) {
		t.Fatalf("sub-image sample = %v, want R ~ 0.784", got)
	}

	// Invalid cubemaps must wrap ErrInvalidCubemap.
	var nilCM *render3d.Cubemap
	if err := nilCM.Validate(); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("nil Validate = %v, want ErrInvalidCubemap", err)
	}
	bad := cardinalCubemap(8)
	bad.Faces[2] = nil
	if err := bad.Validate(); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("nil face Validate = %v, want ErrInvalidCubemap", err)
	}
	bad = cardinalCubemap(8)
	bad.Faces[1] = image.NewRGBA(image.Rect(0, 0, 8, 4))
	if err := bad.Validate(); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("non-square Validate = %v, want ErrInvalidCubemap", err)
	}
	bad = cardinalCubemap(8)
	bad.Faces[3] = solidFace(4, color.RGBA{A: 255})
	if err := bad.Validate(); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("mismatched size Validate = %v, want ErrInvalidCubemap", err)
	}
	bad = cardinalCubemap(4)
	bad.Mips = make([][6]*image.RGBA, 4) // 4x4 supports at most 3 mip levels (1+2)
	if err := bad.Validate(); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("excess mips Validate = %v, want ErrInvalidCubemap", err)
	}
	bad = cardinalCubemap(4)
	bad.Mips = [][6]*image.RGBA{{
		solidFace(3, color.RGBA{A: 255}), solidFace(2, color.RGBA{A: 255}),
		solidFace(2, color.RGBA{A: 255}), solidFace(2, color.RGBA{A: 255}),
		solidFace(2, color.RGBA{A: 255}), solidFace(2, color.RGBA{A: 255}),
	}}
	if err := bad.Validate(); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("wrong mip size Validate = %v, want ErrInvalidCubemap", err)
	}
}

func TestCubemapFromEquirectangular(t *testing.T) {
	// Build a 64x32 equirectangular panorama encoding:
	//   R = 255 near zenith (+Y, top rows), 0 near nadir (-Y, bottom rows)
	//   G = 255 near +X (u = 0.5), 0 near -X (u = 0 / 1)
	//   B = 255 near +Z (u = 0.75), 0 near -Z (u = 0.25)
	const w, h = 64, 32
	pano := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		ev := (float64(y) + 0.5) / float64(h)
		theta := ev * math.Pi
		dirY := math.Cos(theta)
		sinT := math.Sin(theta)
		for x := range w {
			eu := (float64(x) + 0.5) / float64(w)
			phi := (eu - 0.5) * 2 * math.Pi
			dirX := sinT * math.Cos(phi)
			dirZ := sinT * math.Sin(phi)
			pano.SetRGBA(x, y, color.RGBA{
				R: byte(math.Round(clampUnit(0.5*(dirY+1)) * 255)),
				G: byte(math.Round(clampUnit(0.5*(dirX+1)) * 255)),
				B: byte(math.Round(clampUnit(0.5*(dirZ+1)) * 255)),
				A: 255,
			})
		}
	}

	cm, err := render3d.CubemapFromEquirectangular(pano, 16, false)
	if err != nil {
		t.Fatalf("CubemapFromEquirectangular: %v", err)
	}
	cases := []struct {
		name string
		dir  math3d.Vec3
		want [3]float32
	}{
		{"+Y zenith", math3d.Vec3{Y: 1}, [3]float32{1, 0.5, 0.5}},
		{"-Y nadir", math3d.Vec3{Y: -1}, [3]float32{0, 0.5, 0.5}},
		{"+X", math3d.Vec3{X: 1}, [3]float32{0.5, 1, 0.5}},
		{"-X seam", math3d.Vec3{X: -1}, [3]float32{0.5, 0, 0.5}},
		{"+Z", math3d.Vec3{Z: 1}, [3]float32{0.5, 0.5, 1}},
		{"-Z", math3d.Vec3{Z: -1}, [3]float32{0.5, 0.5, 0}},
	}
	for _, tc := range cases {
		got := cm.Sample(tc.dir)
		for ch := range 3 {
			if math.Abs(float64(got[ch]-tc.want[ch])) > 0.06 {
				t.Errorf("%s ch %d = %.3f, want ~%.3f (got %v)", tc.name, ch, got[ch], tc.want[ch], got)
			}
		}
	}

	if _, err := render3d.CubemapFromEquirectangular(nil, 16, false); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("nil image err = %v, want ErrInvalidCubemap", err)
	}
	if _, err := render3d.CubemapFromEquirectangular(pano, 0, false); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("faceSize 0 err = %v, want ErrInvalidCubemap", err)
	}
}

func TestGradientSkyCubemap(t *testing.T) {
	zenith := [3]float32{0.1, 0.3, 0.9}
	horizon := [3]float32{0.9, 0.7, 0.5}
	ground := [3]float32{0.2, 0.15, 0.1}
	// Odd faceSize places a texel center at exact d.Y == 0 on the horizon faces.
	cm, err := render3d.GradientSkyCubemap(zenith, horizon, ground, 17)
	if err != nil {
		t.Fatalf("GradientSkyCubemap: %v", err)
	}
	check := func(name string, dir math3d.Vec3, want [3]float32) {
		got := cm.Sample(dir)
		for ch := range 3 {
			if math.Abs(float64(got[ch]-want[ch])) > 0.04 {
				t.Errorf("%s ch %d = %.3f, want %.3f", name, ch, got[ch], want[ch])
			}
		}
	}
	check("zenith", math3d.Vec3{Y: 1}, zenith)
	check("horizon +X", math3d.Vec3{X: 1}, horizon)
	check("horizon -Z", math3d.Vec3{Z: -1}, horizon)
	check("ground", math3d.Vec3{Y: -1}, ground)

	if _, err := render3d.GradientSkyCubemap(zenith, horizon, ground, 0); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("faceSize 0 err = %v, want ErrInvalidCubemap", err)
	}
	nan := float32(math.NaN())
	if _, err := render3d.GradientSkyCubemap([3]float32{nan, 0, 0}, horizon, ground, 8); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("NaN color err = %v, want ErrInvalidCubemap", err)
	}
}

func TestIrradianceSH(t *testing.T) {
	// 1. Uniform constant radiance C must yield E(n) = pi * C in every direction.
	const cR, cG, cB = 0.6, 0.4, 0.8
	uniform := &render3d.Cubemap{}
	col := color.RGBA{
		R: byte(math.Round(cR * 255)),
		G: byte(math.Round(cG * 255)),
		B: byte(math.Round(cB * 255)),
		A: 255,
	}
	for face := range 6 {
		uniform.Faces[face] = solidFace(16, col)
	}
	sh, err := render3d.ComputeIrradianceSH(uniform)
	if err != nil {
		t.Fatalf("ComputeIrradianceSH uniform: %v", err)
	}
	dirs := []math3d.Vec3{
		{X: 1}, {X: -1}, {Y: 1}, {Y: -1}, {Z: 1}, {Z: -1},
		{X: 1, Y: 1, Z: 1}, {X: -1, Y: 2, Z: -0.5},
	}
	wantR := float32(float64(col.R) / 255.0 * math.Pi)
	wantG := float32(float64(col.G) / 255.0 * math.Pi)
	wantB := float32(float64(col.B) / 255.0 * math.Pi)
	for _, d := range dirs {
		got := sh.Evaluate(d)
		if !math3d.ApproxEqual(got[0], wantR, 1e-3) ||
			!math3d.ApproxEqual(got[1], wantG, 1e-3) ||
			!math3d.ApproxEqual(got[2], wantB, 1e-3) {
			t.Fatalf("uniform SH Evaluate(%v) = %v, want (%v, %v, %v)", d, got, wantR, wantG, wantB)
		}
	}

	// 2. Upper hemisphere white (y > 0), lower black (y < 0): E(+Y) = pi, E(horizon) = pi/2, E(-Y) = 0.
	hemi := &render3d.Cubemap{}
	const sz = 16
	for face := range gpu.CubeFace(6) {
		img := image.NewRGBA(image.Rect(0, 0, sz, sz))
		for py := range sz {
			v := float32((float64(py) + 0.5) / float64(sz))
			for px := range sz {
				u := float32((float64(px) + 0.5) / float64(sz))
				d := render3d.CubeFaceDirection(face, u, v)
				if d.Y > 0 {
					img.SetRGBA(px, py, color.RGBA{R: 255, G: 255, B: 255, A: 255})
				} else {
					img.SetRGBA(px, py, color.RGBA{A: 255})
				}
			}
		}
		hemi.Faces[face] = img
	}
	hemiSH, err := render3d.ComputeIrradianceSH(hemi)
	if err != nil {
		t.Fatalf("ComputeIrradianceSH hemisphere: %v", err)
	}
	top := hemiSH.Evaluate(math3d.Vec3{Y: 1})[0]
	hor := hemiSH.Evaluate(math3d.Vec3{X: 1})[0]
	bot := hemiSH.Evaluate(math3d.Vec3{Y: -1})[0]
	if math.Abs(float64(top)-math.Pi) > 0.08 {
		t.Errorf("hemisphere E(+Y) = %.3f, want ~pi (%.3f)", top, math.Pi)
	}
	if math.Abs(float64(hor)-math.Pi/2) > 0.08 {
		t.Errorf("hemisphere E(+X) = %.3f, want ~pi/2 (%.3f)", hor, math.Pi/2)
	}
	if bot > 0.08 {
		t.Errorf("hemisphere E(-Y) = %.3f, want ~0", bot)
	}

	// 3. ToCubemap round-trips E(n)/pi into [0, 1] without clipping.
	irrCM, err := sh.ToCubemap(8)
	if err != nil {
		t.Fatalf("ToCubemap: %v", err)
	}
	s := irrCM.Sample(math3d.Vec3{Y: 1})
	if !math3d.ApproxEqual(s[0], float32(col.R)/255.0, 0.01) {
		t.Errorf("ToCubemap sample = %v, want R ~ %.3f", s, float32(col.R)/255.0)
	}

	// 4. Zero normal evaluates cleanly to zero.
	if z := sh.Evaluate(math3d.Vec3Zero()); z != ([3]float32{}) {
		t.Errorf("zero normal = %v, want zero", z)
	}

	// 5. sRGB decoding: mid-gray 128 in sRGB is ~0.216 linear, not 0.502.
	srgbCM := &render3d.Cubemap{SRGB: true}
	for face := range 6 {
		srgbCM.Faces[face] = solidFace(8, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	}
	srgbSH, err := render3d.ComputeIrradianceSH(srgbCM)
	if err != nil {
		t.Fatalf("ComputeIrradianceSH sRGB: %v", err)
	}
	gotLinear := srgbSH.Evaluate(math3d.Vec3{Y: 1})[0] / float32(math.Pi)
	if math.Abs(float64(gotLinear)-0.2158) > 0.01 {
		t.Errorf("sRGB mid-gray decoded = %.4f, want ~0.2158", gotLinear)
	}
}

func TestPrefilterEnvironment(t *testing.T) {
	// 1. Uniform environment stays constant across all mip levels.
	uni := &render3d.Cubemap{}
	for face := range 6 {
		uni.Faces[face] = solidFace(16, color.RGBA{R: 180, G: 120, B: 60, A: 255})
	}
	prefUni, err := render3d.PrefilterEnvironment(uni, render3d.PrefilterOptions{
		BaseSize:    16,
		MipLevels:   4,
		SampleCount: 32,
	})
	if err != nil {
		t.Fatalf("PrefilterEnvironment uniform: %v", err)
	}
	if prefUni.Size() != 16 || prefUni.MipLevels() != 4 {
		t.Fatalf("got size=%d mips=%d, want 16/4", prefUni.Size(), prefUni.MipLevels())
	}
	for lod := float32(0); lod <= 3; lod++ {
		got := prefUni.SampleLOD(math3d.Vec3{X: 0.4, Y: 0.7, Z: -0.5}, lod)
		if math.Abs(float64(got[0])-180.0/255.0) > 0.02 ||
			math.Abs(float64(got[1])-120.0/255.0) > 0.02 ||
			math.Abs(float64(got[2])-60.0/255.0) > 0.02 {
			t.Errorf("uniform prefilter LOD %v = %v, want (0.706, 0.471, 0.235)", lod, got)
		}
	}

	// 2. High-contrast studio environment blurs progressively with higher LOD (variance across directions strictly decreases from mip 0 to max mip) and is bit-for-bit deterministic across repeated runs.
	studio := studioEnvironmentCubemap(16)
	opts := render3d.PrefilterOptions{BaseSize: 16, MipLevels: 4, SampleCount: 32}
	prefA, err := render3d.PrefilterEnvironment(studio, opts)
	if err != nil {
		t.Fatalf("PrefilterEnvironment studio: %v", err)
	}
	prefB, err := render3d.PrefilterEnvironment(studio, opts)
	if err != nil {
		t.Fatalf("PrefilterEnvironment studio repeat: %v", err)
	}
	for face := range 6 {
		if string(prefA.Faces[face].Pix) != string(prefB.Faces[face].Pix) {
			t.Fatalf("prefilter mip 0 face %d is non-deterministic", face)
		}
		for m := range prefA.Mips {
			if string(prefA.Mips[m][face].Pix) != string(prefB.Mips[m][face].Pix) {
				t.Fatalf("prefilter mip %d face %d is non-deterministic", m+1, face)
			}
		}
	}
	sampleVariance := func(lod float32) float64 {
		dirs := []math3d.Vec3{
			{X: 0.45, Y: 0.65, Z: 0.61},
			{Y: 1}, {Y: -1}, {X: -1}, {Z: -1}, {X: 1},
		}
		var vals [6]float64
		var mean float64
		for i, d := range dirs {
			c := prefA.SampleLOD(d, lod)
			luma := 0.2126*float64(c[0]) + 0.7152*float64(c[1]) + 0.0722*float64(c[2])
			vals[i] = luma
			mean += luma
		}
		mean /= float64(len(dirs))
		var v float64
		for _, x := range vals {
			diff := x - mean
			v += diff * diff
		}
		return v / float64(len(dirs))
	}
	v0 := sampleVariance(0)
	vMax := sampleVariance(3)
	if !(vMax < v0*0.65) {
		t.Errorf("expected roughness=1 mip variance (%.4f) much lower than roughness=0 (%.4f)", vMax, v0)
	}

	// 3. Reject mip counts exceeding MaxMipLevels.
	if _, err := render3d.PrefilterEnvironment(studio, render3d.PrefilterOptions{BaseSize: 8, MipLevels: 6}); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("excess MipLevels err = %v, want ErrInvalidCubemap", err)
	}
}

func TestGenerateBRDFLUTAndEnvBRDFApprox(t *testing.T) {
	lut, err := render3d.GenerateBRDFLUTWithSamples(32, 64)
	if err != nil {
		t.Fatalf("GenerateBRDFLUTWithSamples: %v", err)
	}
	if lut.Bounds().Dx() != 32 || lut.Bounds().Dy() != 32 {
		t.Fatalf("LUT bounds = %v, want 32x32", lut.Bounds())
	}

	// Smooth surface at normal incidence (high NoV, low roughness): scale + bias ≈ 1.0 (white mirror reflects nearly 100%).
	cNormalSmooth := lut.RGBAAt(31, 0)
	sumNormalSmooth := float64(cNormalSmooth.R)/255.0 + float64(cNormalSmooth.G)/255.0
	if math.Abs(sumNormalSmooth-1.0) > 0.05 {
		t.Errorf("LUT(NoV=1, rough=0) scale+bias = %.3f, want ~1.0 (R=%d G=%d)", sumNormalSmooth, cNormalSmooth.R, cNormalSmooth.G)
	}

	// Grazing angle on smooth surface (low NoV, low roughness): Fresnel bias rises sharply.
	cGrazing := lut.RGBAAt(1, 1)
	if cGrazing.G <= cNormalSmooth.G+50 {
		t.Errorf("expected grazing Fresnel bias (%d) well above normal incidence (%d)", cGrazing.G, cNormalSmooth.G)
	}

	// Analytical EnvBRDFApprox (Karis 2014 mobile fit) agrees closely with the Monte Carlo LUT for dielectrics (f0=0.04) and tracks metallic energy attenuation (for f0=1.0, Karis's fit simplifies to 1.0 - 0.55*roughness).
	for _, py := range []int{2, 8, 16, 26} {
		rough := float32((float64(py) + 0.5) / 32.0)
		for _, px := range []int{4, 12, 24, 30} {
			nov := float32((float64(px) + 0.5) / 32.0)
			pxCol := lut.RGBAAt(px, py)
			scale := float32(pxCol.R) / 255.0
			bias := float32(pxCol.G) / 255.0

			wantDielectric := 0.04*scale + bias
			gotDielectric := render3d.EnvBRDFApprox([3]float32{0.04, 0.04, 0.04}, rough, nov)[0]
			if math.Abs(float64(gotDielectric-wantDielectric)) > 0.08 {
				t.Errorf("EnvBRDFApprox(f0=0.04, rough=%.2f, nov=%.2f) = %.3f, LUT = %.3f", rough, nov, gotDielectric, wantDielectric)
			}

			gotMetal := render3d.EnvBRDFApprox([3]float32{1, 1, 1}, rough, nov)[0]
			wantKarisMetal := 1.0 - 0.55*rough
			if !math3d.ApproxEqual(gotMetal, wantKarisMetal, 1e-4) {
				t.Errorf("EnvBRDFApprox(f0=1.0, rough=%.2f, nov=%.2f) = %.4f, want Karis %.4f", rough, nov, gotMetal, wantKarisMetal)
			}
			wantMetalLUT := scale + bias
			if math.Abs(float64(gotMetal-wantMetalLUT)) > 0.20 {
				t.Errorf("EnvBRDFApprox(f0=1.0, rough=%.2f, nov=%.2f) = %.3f too far from LUT %.3f", rough, nov, gotMetal, wantMetalLUT)
			}
		}
	}

	if _, err := render3d.GenerateBRDFLUT(0); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Fatalf("GenerateBRDFLUT(0) err = %v, want ErrInvalidCubemap", err)
	}
}

func TestSkyboxAndIBLUniformEncodeLayout(t *testing.T) {
	// Camera translation must not affect SkyboxUniform.InvViewProj.
	camA := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 0, Z: 0}, math3d.DegToRad(60), 1.5, 0.1, 100)
	camA.LookAt(math3d.Vec3{X: 1, Y: 0.2, Z: -2}, math3d.Vec3Up())
	camB := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 100, Y: -50, Z: 250}, math3d.DegToRad(60), 1.5, 0.1, 100)
	camB.LookAt(math3d.Vec3{X: 101, Y: -49.8, Z: 248}, math3d.Vec3Up())

	uA := render3d.NewSkyboxUniform(camA)
	uB := render3d.NewSkyboxUniform(camB)
	if !uA.InvViewProj.ApproxEqual(uB.InvViewProj, 1e-4) {
		t.Fatalf("camera translation shifted SkyboxUniform:\nA=%v\nB=%v", uA.InvViewProj, uB.InvViewProj)
	}
	uA.Exposure = 1.5
	uA.LOD = 2.25

	var sbBuf [render3d.SkyboxUniformSize + 8]byte
	sbOut := uA.Encode(sbBuf[:])
	if len(sbOut) != render3d.SkyboxUniformSize || render3d.SkyboxUniformSize != 80 {
		t.Fatalf("SkyboxUniform encoded %d bytes, want 80", len(sbOut))
	}
	readF32 := func(b []byte, idx int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(b[4*idx:]))
	}
	for i := range 16 {
		if readF32(sbOut, i) != uA.InvViewProj[i] {
			t.Fatalf("SkyboxUniform matrix[%d] mismatch", i)
		}
	}
	if readF32(sbOut, 16) != 1.5 || readF32(sbOut, 17) != 2.25 {
		t.Fatalf("SkyboxUniform exposure/lod = (%v, %v), want (1.5, 2.25)", readF32(sbOut, 16), readF32(sbOut, 17))
	}
	if allocs := testing.AllocsPerRun(100, func() { uA.Encode(sbBuf[:]) }); allocs != 0 {
		t.Fatalf("SkyboxUniform.Encode allocated %v times", allocs)
	}

	// Verify panic on undersized slice even with sufficient capacity.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("SkyboxUniform.Encode(len 0, cap 80) did not panic")
			}
		}()
		uA.Encode(make([]byte, 0, render3d.SkyboxUniformSize))
	}()

	// Verify IBLUniform byte layout and zero allocations.
	var iblU render3d.IBLUniform
	for k := range 9 {
		iblU.SH.Coeff[k] = [3]float32{float32(k) + 0.1, float32(k) + 0.2, float32(k) + 0.3}
	}
	iblU.Intensity = 1.25
	iblU.MaxLOD = 4.0
	var iblBuf [render3d.IBLUniformSize + 8]byte
	iblOut := iblU.Encode(iblBuf[:])
	if len(iblOut) != render3d.IBLUniformSize || render3d.IBLUniformSize != 160 {
		t.Fatalf("IBLUniform encoded %d bytes, want 160", len(iblOut))
	}
	for k := range 9 {
		for ch := range 3 {
			if got := readF32(iblOut, k*4+ch); got != iblU.SH.Coeff[k][ch] {
				t.Fatalf("IBLUniform sh[%d][%d] = %v, want %v", k, ch, got, iblU.SH.Coeff[k][ch])
			}
		}
		if pad := readF32(iblOut, k*4+3); pad != 0 {
			t.Fatalf("IBLUniform sh[%d] pad = %v, want 0", k, pad)
		}
	}
	if readF32(iblOut, 36) != 1.25 || readF32(iblOut, 37) != 4.0 {
		t.Fatalf("IBLUniform intensity/max_lod = (%v, %v), want (1.25, 4.0)", readF32(iblOut, 36), readF32(iblOut, 37))
	}
	if allocs := testing.AllocsPerRun(100, func() { iblU.Encode(iblBuf[:]) }); allocs != 0 {
		t.Fatalf("IBLUniform.Encode allocated %v times", allocs)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("IBLUniform.Encode(len 0, cap 160) did not panic")
			}
		}()
		iblU.Encode(make([]byte, 0, render3d.IBLUniformSize))
	}()
}

func TestSkyboxDrawsBehindSceneAndMatchesReference(t *testing.T) {
	const (
		w = 128
		h = 96
	)
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	// 1. Verify cardinal cubemap faces align with camera look directions and are invariant to camera translation.
	cardCube, err := render3d.UploadCubemap(nil, dev, cardinalCubemap(8), render3d.CubemapUpload{Label: "cardinal"})
	if err != nil {
		t.Fatalf("UploadCubemap cardinal: %v", err)
	}
	t.Cleanup(func() { _ = cardCube.Close() })

	skyPipe, err := render3d.NewSkyboxPipeline(nil, dev, render3d.SkyboxPipelineDesc{
		Label:       "skybox",
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewSkyboxPipeline: %v", err)
	}
	t.Cleanup(func() { _ = skyPipe.Close() })

	cardGroup, err := skyPipe.BindCubemap(nil, cardCube)
	if err != nil {
		t.Fatalf("BindCubemap cardinal: %v", err)
	}
	t.Cleanup(func() { _ = cardGroup.Close() })

	renderSkyOnly := func(pos, target, up math3d.Vec3) *image.RGBA {
		cam := scene3d.NewPerspectiveCamera(pos, math3d.DegToRad(60), float32(w)/float32(h), 0.1, 100)
		cam.LookAt(target, up)
		skyPipe.WriteCamera(cam)
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		skyPipe.Draw(pass, cardGroup)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return sr.Image()
	}

	lookChecks := []struct {
		name   string
		target math3d.Vec3
		up     math3d.Vec3
		want   color.RGBA
	}{
		{"+X Red", math3d.Vec3{X: 1}, math3d.Vec3Up(), color.RGBA{R: 240, G: 40, B: 40, A: 255}},
		{"-X Green", math3d.Vec3{X: -1}, math3d.Vec3Up(), color.RGBA{R: 40, G: 240, B: 40, A: 255}},
		{"+Y Blue", math3d.Vec3{Y: 1}, math3d.Vec3{Z: 1}, color.RGBA{R: 50, G: 90, B: 245, A: 255}},
		{"-Y Yellow", math3d.Vec3{Y: -1}, math3d.Vec3{Z: -1}, color.RGBA{R: 235, G: 215, B: 45, A: 255}},
		{"+Z Cyan", math3d.Vec3{Z: 1}, math3d.Vec3Up(), color.RGBA{R: 40, G: 225, B: 235, A: 255}},
		{"-Z Magenta", math3d.Vec3{Z: -1}, math3d.Vec3Up(), color.RGBA{R: 225, G: 55, B: 225, A: 255}},
	}
	for _, tc := range lookChecks {
		img := renderSkyOnly(math3d.Vec3Zero(), tc.target, tc.up)
		wantPixel(t, img, w/2, h/2, tc.want)
	}
	// Translating camera far away while looking in the same direction keeps the skybox identical.
	imgOrigin := renderSkyOnly(math3d.Vec3Zero(), math3d.Vec3{X: 1, Y: 0.2, Z: -0.5}, math3d.Vec3Up())
	imgShifted := renderSkyOnly(math3d.Vec3{X: 50, Y: 25, Z: -80}, math3d.Vec3{X: 51, Y: 25.2, Z: -80.5}, math3d.Vec3Up())
	if string(imgOrigin.Pix) != string(imgShifted.Pix) {
		t.Fatal("camera translation changed skybox pixels")
	}

	// 2. Render studio skybox behind an opaque foreground mesh (drawn first with depth write, then skybox with CompareLessEqual and DepthWrite: false).
	studioCM := studioEnvironmentCubemap(32)
	studioGPU, err := render3d.UploadCubemap(nil, dev, studioCM, render3d.CubemapUpload{Label: "studio"})
	if err != nil {
		t.Fatalf("UploadCubemap studio: %v", err)
	}
	t.Cleanup(func() { _ = studioGPU.Close() })

	studioGroup, err := skyPipe.BindCubemap(nil, studioGPU)
	if err != nil {
		t.Fatalf("BindCubemap studio: %v", err)
	}
	t.Cleanup(func() { _ = studioGroup.Close() })

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	fgMod, err := gpu.CompileWGSL(render3d.CameraWGSL + `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}
struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) normal: vec3<f32>,
}
@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.normal = in.normal;
	return out;
}
@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let n = normalize(in.normal);
	let shade = 0.35 + 0.65 * max(dot(n, normalize(vec3<f32>(0.5, 0.7, 0.5))), 0.0);
	return vec4<f32>(0.88 * shade, 0.24 * shade, 0.18 * shade, 1.0);
}
`)
	if err != nil {
		t.Fatalf("CompileWGSL foreground: %v", err)
	}
	fgPipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:         "fg-sphere",
		Vertex:        gpu.ShaderStage{Module: fgMod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: fgMod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{camBuf.Layout()},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
		Targets:       []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline fg: %v", err)
	}
	sphereGPU, err := render3d.UploadMesh(nil, dev, uvSphereMesh(1, 24, 16), render3d.MeshUpload{Label: "sphere"})
	if err != nil {
		t.Fatalf("UploadMesh sphere: %v", err)
	}
	t.Cleanup(func() { _ = sphereGPU.Close() })

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 0.4, Z: 4.0}, math3d.DegToRad(50), float32(w)/float32(h), 0.1, 100)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)
	skyPipe.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(fgPipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	sphereGPU.Draw(pass)
	// Draw skybox AFTER foreground geometry to prove depth test keeps it behind.
	skyPipe.Draw(pass, studioGroup)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	got := sr.Image()

	// Center pixel must be the red foreground sphere, NOT overwritten by the skybox.
	center := got.RGBAAt(w/2, h/2)
	if !(center.R > 120 && center.R > center.B+60) {
		t.Fatalf("center pixel = %v, expected red foreground sphere in front of skybox", center)
	}
	// Top-left pixel must be blue studio sky, bottom-left must be darker ground.
	topSky := got.RGBAAt(4, 4)
	botGround := got.RGBAAt(4, h-5)
	if !(topSky.B > topSky.R+25) {
		t.Errorf("top-left sky pixel = %v, want bluish studio sky", topSky)
	}
	if !(botGround.R < 100 && botGround.G < 100 && botGround.B < 100) {
		t.Errorf("bottom-left ground pixel = %v, want dark studio ground", botGround)
	}

	assertNoClearPixels(t, got)
	assertReference(t, "skybox_scene", got)
}

func TestIBLReflectiveSpheresMatchCPUAndReference(t *testing.T) {
	const (
		w = 160
		h = 112
	)
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	studioCM := studioEnvironmentCubemap(32)
	sh, err := render3d.ComputeIrradianceSH(studioCM)
	if err != nil {
		t.Fatalf("ComputeIrradianceSH: %v", err)
	}
	prefilterCM, err := render3d.PrefilterEnvironment(studioCM, render3d.PrefilterOptions{
		BaseSize:    32,
		MipLevels:   5,
		SampleCount: 48,
	})
	if err != nil {
		t.Fatalf("PrefilterEnvironment: %v", err)
	}

	skyGPU, err := render3d.UploadCubemap(nil, dev, studioCM, render3d.CubemapUpload{Label: "sky"})
	if err != nil {
		t.Fatalf("UploadCubemap sky: %v", err)
	}
	t.Cleanup(func() { _ = skyGPU.Close() })

	prefGPU, err := render3d.UploadCubemap(nil, dev, prefilterCM, render3d.CubemapUpload{Label: "prefilter"})
	if err != nil {
		t.Fatalf("UploadCubemap prefilter: %v", err)
	}
	t.Cleanup(func() { _ = prefGPU.Close() })

	iblUniform := render3d.NewIBLUniform(sh, prefGPU, 1.0)
	iblBuf, err := render3d.NewGPUIBLBuffer(nil, dev, prefGPU, iblUniform, render3d.IBLBufferOptions{Label: "studio-ibl"})
	if err != nil {
		t.Fatalf("NewGPUIBLBuffer: %v", err)
	}
	t.Cleanup(func() { _ = iblBuf.Close() })

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	skyPipe, err := render3d.NewSkyboxPipeline(nil, dev, render3d.SkyboxPipelineDesc{
		Label:       "skybox",
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewSkyboxPipeline: %v", err)
	}
	t.Cleanup(func() { _ = skyPipe.Close() })

	skyGroup, err := skyPipe.BindCubemap(nil, skyGPU)
	if err != nil {
		t.Fatalf("BindCubemap: %v", err)
	}
	t.Cleanup(func() { _ = skyGroup.Close() })

	matLayout, err := dev.CreateBindGroupLayoutDesc(nil, gpu.BindGroupLayoutDesc{
		Label: "mat-layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
		},
	})
	if err != nil {
		t.Fatalf("CreateBindGroupLayoutDesc mat: %v", err)
	}
	t.Cleanup(func() { _ = matLayout.Close() })

	mod, err := gpu.CompileWGSL(makeIBLSphereWGSL())
	if err != nil {
		t.Fatalf("CompileWGSL IBL sphere: %v", err)
	}
	layouts := make([]*gpu.BindGroupLayout, testIBLGroup+1)
	layouts[render3d.CameraGroup] = camBuf.Layout()
	layouts[1] = matLayout
	layouts[testIBLGroup] = iblBuf.Layout()

	pbrPipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:         "pbr-ibl",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
		Targets:       []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline pbr-ibl: %v", err)
	}

	sphereGPU, err := render3d.UploadMesh(nil, dev, uvSphereMesh(1, 28, 20), render3d.MeshUpload{Label: "ibl-sphere"})
	if err != nil {
		t.Fatalf("UploadMesh sphere: %v", err)
	}
	t.Cleanup(func() { _ = sphereGPU.Close() })

	// 2x2 showcase matching PBR material axes:
	//   Top-left:     polished chrome metal   (metallic=1.0, roughness=0.05)
	//   Top-right:    brushed gold metal      (metallic=1.0, roughness=0.45)
	//   Bottom-left:  glossy crimson lacquer  (metallic=0.0, roughness=0.10)
	//   Bottom-right: matte ivory ceramic     (metallic=0.0, roughness=0.90)
	type sphereSpec struct {
		name      string
		pos       math3d.Vec3
		baseColor [3]float32
		metallic  float32
		roughness float32
	}
	specs := []sphereSpec{
		{"chrome", math3d.Vec3{X: -1.35, Y: 1.05, Z: 0}, [3]float32{0.96, 0.96, 0.96}, 1.0, 0.05},
		{"brushed-gold", math3d.Vec3{X: 1.35, Y: 1.05, Z: 0}, [3]float32{0.98, 0.78, 0.34}, 1.0, 0.45},
		{"glossy-red", math3d.Vec3{X: -1.35, Y: -1.05, Z: 0}, [3]float32{0.82, 0.14, 0.12}, 0.0, 0.10},
		{"matte-ivory", math3d.Vec3{X: 1.35, Y: -1.05, Z: 0}, [3]float32{0.85, 0.82, 0.76}, 0.0, 0.90},
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 0, Z: 5.5}, math3d.DegToRad(48), float32(w)/float32(h), 0.1, 100)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)
	skyPipe.WriteCamera(cam)

	matGroups := make([]*gpu.BindGroup, len(specs))
	for i, sp := range specs {
		data := encodeMaterialParams(math3d.Translate(sp.pos).Mul(math3d.Scale(math3d.Vec3{X: 0.85, Y: 0.85, Z: 0.85})), sp.baseColor, sp.metallic, sp.roughness, 1.0)
		mb, err := dev.CreateBuffer(nil, gpu.BufferDesc{
			Label: sp.name + "-mat",
			Size:  uint64(len(data)),
			Usage: gpu.UsageUniform,
			Data:  data,
		})
		if err != nil {
			t.Fatalf("CreateBuffer %s: %v", sp.name, err)
		}
		t.Cleanup(func() { _ = mb.Close() })
		bg, err := dev.CreateBindGroupDesc(nil, gpu.BindGroupDesc{
			Label:   sp.name + "-bg",
			Layout:  matLayout,
			Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: mb, Size: uint64(len(data))}},
		})
		if err != nil {
			t.Fatalf("CreateBindGroup %s: %v", sp.name, err)
		}
		t.Cleanup(func() { _ = bg.Close() })
		matGroups[i] = bg
	}

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pbrPipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	pass.SetBindGroup(testIBLGroup, iblBuf.BindGroup())
	for _, bg := range matGroups {
		pass.SetBindGroup(1, bg)
		sphereGPU.Draw(pass)
	}
	skyPipe.Draw(pass, skyGroup)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	got := sr.Image()

	// Ensure the skybox covered every background pixel (zero pure-black clear pixels leaked from depth-precision overshoot).
	assertNoClearPixels(t, got)

	// Cross-check the exact analytical ray-hit normal at the tested pixel center of every sphere against CPU EvaluateIBL.
	const sphereRadius = float32(0.85)
	for _, sp := range specs {
		front := sp.pos.Add(math3d.Vec3{Z: sphereRadius})
		screenPos, _, visible := cam.WorldToScreen(front, w, h)
		if !visible {
			t.Fatalf("sphere %s center not visible", sp.name)
		}
		px, py := int(screenPos.X), int(screenPos.Y)
		gpuCol := got.RGBAAt(px, py)
		ray := cam.ScreenRay(float32(px)+0.5, float32(py)+0.5, w, h)
		oc := ray.Origin.Sub(sp.pos)
		bCoef := oc.Dot(ray.Dir)
		cCoef := oc.Dot(oc) - sphereRadius*sphereRadius
		disc := bCoef*bCoef - cCoef
		if disc < 0 {
			t.Fatalf("analytical ray at (%d,%d) missed sphere %s", px, py, sp.name)
		}
		hitT := -bCoef - float32(math.Sqrt(float64(disc)))
		hitPos := ray.Origin.Add(ray.Dir.Scale(hitT))
		n := hitPos.Sub(sp.pos).Norm()
		v := cam.Pos.Sub(hitPos).Norm()
		cpuLit := render3d.EvaluateIBL(sh, prefilterCM, 1.0, n, v, sp.baseColor, sp.metallic, sp.roughness, 1.0)
		wantR := byte(math.Round(clampUnit(float64(cpuLit[0])) * 255))
		wantG := byte(math.Round(clampUnit(float64(cpuLit[1])) * 255))
		wantB := byte(math.Round(clampUnit(float64(cpuLit[2])) * 255))
		if absDiffU8(gpuCol.R, wantR) > 8 || absDiffU8(gpuCol.G, wantG) > 8 || absDiffU8(gpuCol.B, wantB) > 8 {
			t.Errorf("%s GPU pixel (%d,%d)=%v differs from CPU EvaluateIBL=(%d,%d,%d)",
				sp.name, px, py, gpuCol, wantR, wantG, wantB)
		}
	}

	// Verify polished chrome reflects the bright blue/warm upper sky on its top hemisphere and the dark ground on its bottom hemisphere.
	chromeTop, _, _ := cam.WorldToScreen(specs[0].pos.Add(math3d.Vec3{Y: 0.55, Z: 0.65}), w, h)
	chromeBot, _, _ := cam.WorldToScreen(specs[0].pos.Add(math3d.Vec3{Y: -0.55, Z: 0.65}), w, h)
	topC := got.RGBAAt(int(chromeTop.X), int(chromeTop.Y))
	botC := got.RGBAAt(int(chromeBot.X), int(chromeBot.Y))
	topLuma := int(topC.R) + int(topC.G) + int(topC.B)
	botLuma := int(botC.R) + int(botC.G) + int(botC.B)
	if !(topLuma > botLuma+120) {
		t.Errorf("chrome reflection top=%v (luma %d) should be much brighter than bottom=%v (luma %d)", topC, topLuma, botC, botLuma)
	}

	// Verify brushed gold has warm golden specular reflection (R > G > B).
	goldCenter, _, _ := cam.WorldToScreen(specs[1].pos.Add(math3d.Vec3{Z: 0.85}), w, h)
	goldC := got.RGBAAt(int(goldCenter.X), int(goldCenter.Y))
	if !(goldC.R > goldC.G && goldC.G > goldC.B+20) {
		t.Errorf("brushed gold center = %v, expected warm golden R > G > B", goldC)
	}

	assertReference(t, "ibl_spheres", got)
}

func TestSkyboxAndIBLInGPUViewProfiles(t *testing.T) {
	// Verify both full and webgl2/gles3 (NoGPUCompute) profiles compile and draw the skybox + IBL-lit reflective sphere identically.
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			app, _, frames := skyboxIBLViewApp(t, false, p.opts...)
			img := advanceAndReadSkyboxIBL(t, app, frames)
			checkSkyboxIBLFrame(t, img)
		})
	}
}

func TestRetainedSkyboxAndIBLAfterDeviceLoss(t *testing.T) {
	app, handles, frames := skyboxIBLViewApp(t, true)
	beforeImg := advanceAndReadSkyboxIBL(t, app, frames)
	checkSkyboxIBLFrame(t, beforeImg)

	firstCube := handles.cube
	firstPref := handles.pref
	firstSky := handles.sky
	firstIBL := handles.ibl

	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if firstCube.Valid() || firstPref.Valid() || firstSky.Valid() || firstIBL.Valid() {
		t.Fatal("retained skybox/IBL resources still valid during device loss")
	}

	app.RestoreGPUDevice("id:viewport")
	if handles.cube != firstCube || !firstCube.Valid() ||
		handles.pref != firstPref || !firstPref.Valid() ||
		handles.sky != firstSky || !firstSky.Valid() ||
		handles.ibl != firstIBL || !firstIBL.Valid() {
		t.Fatalf("retained resources not restored: cube=%v pref=%v sky=%v ibl=%v",
			firstCube.Valid(), firstPref.Valid(), firstSky.Valid(), firstIBL.Valid())
	}

	afterImg := advanceAndReadSkyboxIBL(t, app, frames)
	checkSkyboxIBLFrame(t, afterImg)
	if string(beforeImg.Pix) != string(afterImg.Pix) {
		t.Fatal("rendered frame changed after retained device-loss recovery")
	}
}

type skyboxViewHandles struct {
	cube     *render3d.GPUCubemap
	pref     *render3d.GPUCubemap
	sky      *render3d.SkyboxPipeline
	skyGroup *gpu.BindGroup
	ibl      *render3d.GPUIBLBuffer
	camBuf   *render3d.CameraBuffer
	mesh     *render3d.GPUMesh
	pbrPipe  *gpu.Pipeline
	matBG    *gpu.BindGroup
}

func skyboxIBLViewApp(t *testing.T, retained bool, opts ...nsgotest.Option) (*nsgotest.App, *skyboxViewHandles, *int) {
	t.Helper()
	const size = 64
	studioCM := studioEnvironmentCubemap(16)
	sh, err := render3d.ComputeIrradianceSH(studioCM)
	if err != nil {
		t.Fatalf("ComputeIrradianceSH: %v", err)
	}
	prefCM, err := render3d.PrefilterEnvironment(studioCM, render3d.PrefilterOptions{
		BaseSize:    16,
		MipLevels:   4,
		SampleCount: 24,
	})
	if err != nil {
		t.Fatalf("PrefilterEnvironment: %v", err)
	}
	sphere := uvSphereMesh(1, 20, 14)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3.2}, math3d.DegToRad(50), 1, 0.1, 50)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

	h := &skyboxViewHandles{}
	var frames int
	opts = append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				var err error
				if h.cube == nil || !retained {
					h.cube, err = render3d.UploadCubemap(cx, dev, studioCM, render3d.CubemapUpload{Label: "sky", Retained: retained})
					if err != nil {
						t.Fatalf("UploadCubemap sky: %v", err)
					}
					h.pref, err = render3d.UploadCubemap(cx, dev, prefCM, render3d.CubemapUpload{Label: "pref", Retained: retained})
					if err != nil {
						t.Fatalf("UploadCubemap pref: %v", err)
					}
					h.sky, err = render3d.NewSkyboxPipeline(cx, dev, render3d.SkyboxPipelineDesc{
						Label:       "sky",
						ColorFormat: dev.SurfaceFormat(),
						DepthFormat: gpu.FormatDepth24Plus,
						Retained:    retained,
					})
					if err != nil {
						t.Fatalf("NewSkyboxPipeline: %v", err)
					}
					h.skyGroup, err = h.sky.BindCubemap(cx, h.cube)
					if err != nil {
						t.Fatalf("BindCubemap: %v", err)
					}
					h.ibl, err = render3d.NewGPUIBLBuffer(cx, dev, h.pref, render3d.NewIBLUniform(sh, h.pref, 1.0), render3d.IBLBufferOptions{Label: "ibl", Retained: retained})
					if err != nil {
						t.Fatalf("NewGPUIBLBuffer: %v", err)
					}
					h.mesh, err = render3d.UploadMesh(cx, dev, sphere, render3d.MeshUpload{Label: "sphere", Retained: retained})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				} else {
					h.sky.SetDevice(dev)
					if err := h.ibl.Update(dev, render3d.NewIBLUniform(sh, h.pref, 1.0)); err != nil {
						t.Fatalf("GPUIBLBuffer.Update after restore: %v", err)
					}
				}
				// Recreate non-retained per-device objects (CameraBuffer + material + pipeline).
				h.camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				matLayout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
					Entries: []gpu.BindGroupLayoutEntry{{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform}},
				})
				if err != nil {
					t.Fatalf("CreateBindGroupLayoutDesc: %v", err)
				}
				matData := encodeMaterialParams(math3d.Mat4Identity(), [3]float32{0.96, 0.96, 0.96}, 1.0, 0.08, 1.0)
				matBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
					Size:  uint64(len(matData)),
					Usage: gpu.UsageUniform,
					Data:  matData,
				})
				if err != nil {
					t.Fatalf("CreateBuffer mat: %v", err)
				}
				h.matBG, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
					Layout:  matLayout,
					Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: matBuf, Size: uint64(len(matData))}},
				})
				if err != nil {
					t.Fatalf("CreateBindGroupDesc mat: %v", err)
				}
				mod, err := gpu.CompileWGSL(makeIBLSphereWGSL())
				if err != nil {
					t.Fatalf("CompileWGSL: %v", err)
				}
				layouts := make([]*gpu.BindGroupLayout, testIBLGroup+1)
				layouts[render3d.CameraGroup] = h.camBuf.Layout()
				layouts[1] = matLayout
				layouts[testIBLGroup] = h.ibl.Layout()
				h.pbrPipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
					Label:         "pbr",
					Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
					Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
					Layouts:       layouts,
					VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
					Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
					DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
					Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat(), WriteMask: gpu.ColorWriteAll}},
				})
				if err != nil {
					t.Fatalf("CreatePipeline pbr: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				h.camBuf.WriteCamera(cam)
				h.sky.WriteCamera(cam)
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x000000))
				pass.ClearDepth(1)
				pass.SetPipeline(h.pbrPipe)
				pass.SetBindGroup(render3d.CameraGroup, h.camBuf.BindGroup())
				pass.SetBindGroup(1, h.matBG)
				pass.SetBindGroup(testIBLGroup, h.ibl.BindGroup())
				h.mesh.Draw(pass)
				h.sky.Draw(pass, h.skyGroup)
				pass.End()
				frame.Submit()
			},
		})
	}, opts...)
	return app, h, &frames
}

func advanceAndReadSkyboxIBL(t *testing.T, app *nsgotest.App, frames *int) *image.RGBA {
	t.Helper()
	before := *frames
	app.Advance(16 * time.Millisecond)
	if *frames == before {
		t.Fatal("OnFrame did not run")
	}
	img, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	return img
}

func checkSkyboxIBLFrame(t *testing.T, img *image.RGBA) {
	t.Helper()
	assertNoClearPixels(t, img)
	// Top-left corner is blue studio sky; bottom-left is darker ground-band sky; center is chrome sphere reflecting bright horizon/sun.
	topSky := img.RGBAAt(2, 2)
	botGround := img.RGBAAt(2, 61)
	center := img.RGBAAt(32, 32)
	if !(topSky.B > topSky.R+25) {
		t.Errorf("top-left sky = %v, want bluish sky", topSky)
	}
	if !(botGround.R < 115 && topSky.B > botGround.B+25) {
		t.Errorf("bottom-left ground = %v vs top sky %v, want darker warmer ground", botGround, topSky)
	}
	if !(int(center.R)+int(center.G)+int(center.B) > 260) {
		t.Errorf("center chrome sphere = %v, expected bright environment reflection", center)
	}
}

func assertNoClearPixels(t *testing.T, img *image.RGBA) {
	t.Helper()
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.R == 0 && c.G == 0 && c.B == 0 {
				t.Fatalf("pixel (%d, %d) is pure black clear color (skybox depth dropout)", x, y)
			}
		}
	}
}

func TestGPUCubemapAndIBLLifecycle(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	cm := cardinalCubemap(8)
	g, err := render3d.UploadCubemap(nil, dev, cm, render3d.CubemapUpload{
		Label:        "mipped",
		GenerateMips: true,
	})
	if err != nil {
		t.Fatalf("UploadCubemap GenerateMips: %v", err)
	}
	if g.Size() != 8 || g.MipLevels() != 4 || g.MaxLOD() != 3 || g.Format() != gpu.FormatRGBA8Unorm || !g.Valid() {
		t.Fatalf("unexpected GPUCubemap state: size=%d mips=%d maxLOD=%v fmt=%v valid=%v",
			g.Size(), g.MipLevels(), g.MaxLOD(), g.Format(), g.Valid())
	}
	// 6 faces * (8*8 + 4*4 + 2*2 + 1*1) * 4 bytes = 6 * 85 * 4 = 2040 bytes.
	if got := g.ByteSize(); got != 2040 {
		t.Fatalf("ByteSize = %d, want 2040", got)
	}

	ibl, err := render3d.NewGPUIBLBuffer(nil, dev, g, render3d.NewIBLUniform(render3d.IrradianceSH{}, g, 1.0), render3d.IBLBufferOptions{})
	if err != nil {
		t.Fatalf("NewGPUIBLBuffer: %v", err)
	}
	if !ibl.Valid() || ibl.Buffer() == nil || ibl.Layout() == nil || ibl.BindGroup() == nil {
		t.Fatal("GPUIBLBuffer not valid after creation")
	}
	if got := ibl.ByteSize(); got != render3d.IBLUniformSize {
		t.Fatalf("GPUIBLBuffer.ByteSize = %d, want %d", got, render3d.IBLUniformSize)
	}
	// Update uploads updated IBL parameters cleanly and refuses invalid ones.
	u := render3d.NewIBLUniform(render3d.IrradianceSH{}, g, 0.8)
	if err := ibl.Update(nil, u); err != nil {
		t.Fatalf("GPUIBLBuffer.Update: %v", err)
	}
	bad := u
	bad.Intensity = float32(math.NaN())
	if err := ibl.Update(nil, bad); !errors.Is(err, render3d.ErrInvalidIBL) {
		t.Fatalf("GPUIBLBuffer.Update(NaN intensity) = %v, want ErrInvalidIBL", err)
	}

	if err := ibl.Close(); err != nil {
		t.Fatalf("GPUIBLBuffer.Close: %v", err)
	}
	if err := ibl.Close(); err != nil {
		t.Fatalf("GPUIBLBuffer second Close: %v", err)
	}
	if ibl.Valid() {
		t.Fatal("GPUIBLBuffer still valid after Close")
	}

	if err := g.Close(); err != nil {
		t.Fatalf("GPUCubemap.Close: %v", err)
	}
	if err := g.Close(); err != nil {
		t.Fatalf("GPUCubemap second Close: %v", err)
	}
	if g.Valid() {
		t.Fatal("GPUCubemap still valid after Close")
	}

	// Nil receivers and invalid inputs fail cleanly.
	var nilCube *render3d.GPUCubemap
	var nilSky *render3d.SkyboxPipeline
	var nilIBL *render3d.GPUIBLBuffer
	if nilCube.Valid() || nilCube.Close() != nil || nilCube.ByteSize() != 0 {
		t.Error("nil GPUCubemap should be safe and invalid")
	}
	if nilSky.Valid() || nilSky.Close() != nil {
		t.Error("nil SkyboxPipeline should be safe and invalid")
	}
	if nilIBL.Valid() || nilIBL.Close() != nil || nilIBL.ByteSize() != 0 {
		t.Error("nil GPUIBLBuffer should be safe and invalid")
	}
	if _, err := render3d.UploadCubemap(nil, nil, cm, render3d.CubemapUpload{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("UploadCubemap(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.UploadCubemap(nil, dev, cm, render3d.CubemapUpload{Format: gpu.FormatDepth24Plus}); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Errorf("UploadCubemap(bad fmt) = %v, want ErrInvalidCubemap", err)
	}
	if _, err := render3d.NewSkyboxPipeline(nil, nil, render3d.SkyboxPipelineDesc{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewSkyboxPipeline(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewGPUIBLBuffer(nil, dev, nil, render3d.IBLUniform{}, render3d.IBLBufferOptions{}); !errors.Is(err, render3d.ErrInvalidCubemap) {
		t.Errorf("NewGPUIBLBuffer(nil prefilter) = %v, want ErrInvalidCubemap", err)
	}

	// Custom group index WGSL helper compiles cleanly.
	customWGSL := render3d.IBLWGSLForGroup(3) + `
@vertex fn vs(@builtin(vertex_index) i: u32) -> @builtin(position) vec4<f32> { return vec4<f32>(0.0, 0.0, 0.0, 1.0); }
@fragment fn fs() -> @location(0) vec4<f32> {
	let c = evaluate_ibl(vec3<f32>(0.0, 1.0, 0.0), vec3<f32>(0.0, 0.0, 1.0), vec3<f32>(1.0), 0.5, 0.5, 1.0);
	return vec4<f32>(c, 1.0);
}
`
	if _, err := gpu.CompileWGSL(customWGSL); err != nil {
		t.Fatalf("CompileWGSL IBLWGSLForGroup(3): %v", err)
	}
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func absDiffU8(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

// TestSkyboxKeepsGeometryNearFarPlane draws a quad a few units inside the far plane before the skybox and checks the skybox does not paint over it, at a near/far ratio where z = 0.999999 would.
func TestSkyboxKeepsGeometryNearFarPlane(t *testing.T) {
	const (
		w    = 32
		h    = 32
		near = 0.1
		far  = 1000
		dist = 995
	)
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	cube, err := render3d.UploadCubemap(nil, dev, cardinalCubemap(4), render3d.CubemapUpload{Label: "cardinal"})
	if err != nil {
		t.Fatalf("UploadCubemap: %v", err)
	}
	t.Cleanup(func() { _ = cube.Close() })
	skyPipe, err := render3d.NewSkyboxPipeline(nil, dev, render3d.SkyboxPipelineDesc{ColorFormat: gpu.FormatRGBA8Unorm, DepthFormat: gpu.FormatDepth24Plus})
	if err != nil {
		t.Fatalf("NewSkyboxPipeline: %v", err)
	}
	t.Cleanup(func() { _ = skyPipe.Close() })
	skyGroup, err := skyPipe.BindCubemap(nil, cube)
	if err != nil {
		t.Fatalf("BindCubemap: %v", err)
	}
	t.Cleanup(func() { _ = skyGroup.Close() })

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	mod, err := gpu.CompileWGSL(render3d.CameraWGSL + `
@vertex
fn vs_main(@location(0) pos: vec3<f32>, @location(1) normal: vec3<f32>, @location(2) uv: vec2<f32>) -> @builtin(position) vec4<f32> {
	return camera.view_proj * vec4<f32>(pos, 1.0);
}
@fragment
fn fs_main() -> @location(0) vec4<f32> {
	return vec4<f32>(1.0, 1.0, 1.0, 1.0);
}
`)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	pipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{camBuf.Layout()},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullNone},
		DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
		Targets:       []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	const s = 100
	quad, err := render3d.UploadMesh(nil, dev, render3d.Mesh{
		Positions: []math3d.Vec3{{X: -s, Y: -s, Z: -dist}, {X: s, Y: -s, Z: -dist}, {X: s, Y: s, Z: -dist}, {X: -s, Y: s, Z: -dist}},
		Normals:   []math3d.Vec3{{Z: 1}, {Z: 1}, {Z: 1}, {Z: 1}},
		UVs:       []math3d.Vec2{{}, {X: 1}, {X: 1, Y: 1}, {Y: 1}},
		Indices:   []uint32{0, 1, 2, 0, 2, 3},
	}, render3d.MeshUpload{Label: "far-quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), 1, near, far)
	cam.LookAt(math3d.Vec3{Z: -1}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)
	skyPipe.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	quad.Draw(pass)
	skyPipe.Draw(pass, skyGroup)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	wantPixel(t, sr.Image(), w/2, h/2, color.RGBA{R: 255, G: 255, B: 255, A: 255})
}

// TestLitPBRIBLMatchesCPUReference renders a front-facing quad through NewLitPBRIBLPipeline with a GPUIBLBuffer at IBLGroup and checks the pixels against LinearToSRGB(ShadePoint + EvaluateIBL).
func TestLitPBRIBLMatchesCPUReference(t *testing.T) {
	const w, h = 64, 64
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	env := studioEnvironmentCubemap(16)
	sh, err := render3d.ComputeIrradianceSH(env)
	if err != nil {
		t.Fatalf("ComputeIrradianceSH: %v", err)
	}
	prefCM, err := render3d.PrefilterEnvironment(env, render3d.PrefilterOptions{BaseSize: 16, MipLevels: 4, SampleCount: 24})
	if err != nil {
		t.Fatalf("PrefilterEnvironment: %v", err)
	}
	pref, err := render3d.UploadCubemap(nil, dev, prefCM, render3d.CubemapUpload{Label: "pref"})
	if err != nil {
		t.Fatalf("UploadCubemap: %v", err)
	}
	t.Cleanup(func() { _ = pref.Close() })
	const intensity = 0.9
	iblBuf, err := render3d.NewGPUIBLBuffer(nil, dev, pref, render3d.NewIBLUniform(sh, pref, intensity), render3d.IBLBufferOptions{Label: "ibl"})
	if err != nil {
		t.Fatalf("NewGPUIBLBuffer: %v", err)
	}
	t.Cleanup(func() { _ = iblBuf.Close() })

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(50), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	lightBuf, err := render3d.NewLightBuffer(nil, dev, render3d.LightRig{}, render3d.LightBufferOptions{Label: "lights"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })
	drawBuf, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), render3d.PBRMaterial{BaseColor: math3d.Vec3One()}), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer: %v", err)
	}
	t.Cleanup(func() { _ = drawBuf.Close() })

	pipe, err := render3d.NewLitPBRIBLPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), iblBuf.Layout(), gpu.FormatDepth24Plus)
	if err != nil {
		t.Fatalf("NewLitPBRIBLPipeline: %v", err)
	}
	plane, err := render3d.UploadMesh(nil, dev, render3d.Mesh{
		Positions: []math3d.Vec3{{X: -3, Y: -3}, {X: 3, Y: -3}, {X: 3, Y: 3}, {X: -3, Y: 3}},
		Normals:   []math3d.Vec3{{Z: 1}, {Z: 1}, {Z: 1}, {Z: 1}},
		UVs:       []math3d.Vec2{{}, {X: 1}, {X: 1, Y: 1}, {Y: 1}},
		Indices:   []uint32{0, 1, 2, 0, 2, 3},
	}, render3d.MeshUpload{Label: "plane"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = plane.Close() })

	cases := []struct {
		name string
		mat  render3d.PBRMaterial
		rig  render3d.LightRig
	}{
		{"rough dielectric", render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.8, Y: 0.3, Z: 0.2}, Roughness: 0.8}, render3d.LightRig{}},
		{"smooth metal", render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.95, Y: 0.8, Z: 0.4}, Metallic: 1, Roughness: 0.15}, render3d.LightRig{}},
		{"directional light plus environment", render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.5, Y: 0.55, Z: 0.6}, Metallic: 0.4, Roughness: 0.5}, render3d.LightRig{
			Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{X: -0.3, Y: -0.4, Z: -1}, Color: math3d.Vec3One(), Intensity: 1.2}},
		}},
	}
	samplePixels := [][2]int{{16, 16}, {48, 16}, {32, 32}, {16, 48}, {48, 48}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := lightBuf.Update(dev, tc.rig); err != nil {
				t.Fatalf("lightBuf.Update: %v", err)
			}
			drawBuf.Write(dev, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), tc.mat))
			enc := dev.BeginEncoder()
			pass := enc.BeginRenderPass(gpu.RenderPassDesc{
				ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
				DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
			})
			pass.SetPipeline(pipe)
			pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
			lightBuf.Bind(pass)
			pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
			iblBuf.Bind(pass)
			plane.Draw(pass)
			pass.End()
			dev.Submit(enc.Finish())
			sr.WaitIdle()
			if err := sr.LastError(); err != nil {
				t.Fatalf("software renderer: %v", err)
			}
			img := sr.Image()
			for _, px := range samplePixels {
				ray := cam.ScreenRay(float32(px[0])+0.5, float32(px[1])+0.5, w, h)
				worldPos := ray.At(-ray.Origin.Z / ray.Dir.Z)
				n := math3d.Vec3{Z: 1}
				v := cam.Pos.Sub(worldPos).Norm()
				direct := render3d.ShadePoint(worldPos, n, v, tc.mat, tc.rig)
				env := render3d.EvaluateIBL(sh, prefCM, intensity, n, v, [3]float32{tc.mat.BaseColor.X, tc.mat.BaseColor.Y, tc.mat.BaseColor.Z}, tc.mat.Metallic, tc.mat.Roughness, 1)
				srgb := render3d.LinearToSRGB(math3d.Vec3{X: direct.X + env[0], Y: direct.Y + env[1], Z: direct.Z + env[2]})
				got := img.RGBAAt(px[0], px[1])
				want := [3]uint8{uint8(math.Round(float64(srgb.X) * 255)), uint8(math.Round(float64(srgb.Y) * 255)), uint8(math.Round(float64(srgb.Z) * 255))}
				if absDiffU8(got.R, want[0]) > 6 || absDiffU8(got.G, want[1]) > 6 || absDiffU8(got.B, want[2]) > 6 {
					t.Errorf("pixel %v = %v, want about %v", px, got, want)
				}
			}
		})
	}
}

func TestIBLValidationAndNonFiniteInputs(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	valid := render3d.IBLUniform{Intensity: 1, MaxLOD: 3}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid IBLUniform.Validate = %v", err)
	}
	for name, mutate := range map[string]func(*render3d.IBLUniform){
		"NaN intensity":      func(u *render3d.IBLUniform) { u.Intensity = nan },
		"infinite intensity": func(u *render3d.IBLUniform) { u.Intensity = inf },
		"negative intensity": func(u *render3d.IBLUniform) { u.Intensity = -1 },
		"NaN max LOD":        func(u *render3d.IBLUniform) { u.MaxLOD = nan },
		"negative max LOD":   func(u *render3d.IBLUniform) { u.MaxLOD = -1 },
		"infinite SH":        func(u *render3d.IBLUniform) { u.SH.Coeff[4][1] = inf },
	} {
		u := valid
		mutate(&u)
		if err := u.Validate(); !errors.Is(err, render3d.ErrInvalidIBL) {
			t.Errorf("%s: Validate = %v, want ErrInvalidIBL", name, err)
		}
	}

	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	cube, err := render3d.UploadCubemap(nil, dev, cardinalCubemap(4), render3d.CubemapUpload{})
	if err != nil {
		t.Fatalf("UploadCubemap: %v", err)
	}
	t.Cleanup(func() { _ = cube.Close() })
	if _, err := render3d.NewGPUIBLBuffer(nil, dev, cube, render3d.IBLUniform{Intensity: nan}, render3d.IBLBufferOptions{}); !errors.Is(err, render3d.ErrInvalidIBL) {
		t.Errorf("NewGPUIBLBuffer(NaN intensity) = %v, want ErrInvalidIBL", err)
	}

	cm := cardinalCubemap(4)
	sh, err := render3d.ComputeIrradianceSH(cm)
	if err != nil {
		t.Fatalf("ComputeIrradianceSH: %v", err)
	}
	n, v := math3d.Vec3{Y: 1}, math3d.Vec3{Z: 1}
	base := [3]float32{0.5, 0.5, 0.5}
	for name, args := range map[string][4]float32{
		"NaN metallic":       {nan, 0.5, 1, 1},
		"NaN roughness":      {0.5, nan, 1, 1},
		"infinite ao":        {0.5, 0.5, inf, 1},
		"NaN intensity":      {0.5, 0.5, 1, nan},
		"negative intensity": {0.5, 0.5, 1, -2},
	} {
		got := render3d.EvaluateIBL(sh, cm, args[3], n, v, base, args[0], args[1], args[2])
		for ch, c := range got {
			if math.IsNaN(float64(c)) || math.IsInf(float64(c), 0) || c < 0 {
				t.Errorf("%s: EvaluateIBL channel %d = %v, want finite and non-negative", name, ch, c)
			}
		}
	}

	for _, bad := range [][3]float32{{-0.1, 0, 0}, {nan, 0, 0}, {0, inf, 0}} {
		if _, err := render3d.GradientSkyCubemap(bad, [3]float32{0.5, 0.5, 0.5}, [3]float32{0.1, 0.1, 0.1}, 4); !errors.Is(err, render3d.ErrInvalidCubemap) {
			t.Errorf("GradientSkyCubemap(%v) = %v, want ErrInvalidCubemap", bad, err)
		}
	}

	pano := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for _, srgb := range []bool{false, true} {
		got, err := render3d.CubemapFromEquirectangular(pano, 4, srgb)
		if err != nil {
			t.Fatalf("CubemapFromEquirectangular: %v", err)
		}
		if got.SRGB != srgb {
			t.Errorf("CubemapFromEquirectangular(srgb=%v).SRGB = %v", srgb, got.SRGB)
		}
	}
}
