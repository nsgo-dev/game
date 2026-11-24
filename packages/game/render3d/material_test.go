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

// uvSphereMesh generates a counter-clockwise UV sphere centered at center with radius r.
func uvSphereMesh(center math3d.Vec3, r float32, stacks, slices int) render3d.Mesh {
	var m render3d.Mesh
	for iy := 0; iy <= stacks; iy++ {
		v := float32(iy) / float32(stacks)
		theta := float64(v) * math.Pi
		sinT, cosT := math.Sin(theta), math.Cos(theta)
		for ix := 0; ix <= slices; ix++ {
			u := float32(ix) / float32(slices)
			phi := float64(u) * 2 * math.Pi
			sinP, cosP := math.Sin(phi), math.Cos(phi)
			// Standard right-handed outward normal; +U goes around equator, +V goes north->south.
			nx := float32(sinT * sinP)
			ny := float32(cosT)
			nz := float32(sinT * cosP)
			n := math3d.Vec3{X: nx, Y: ny, Z: nz}
			m.Positions = append(m.Positions, center.Add(n.Scale(r)))
			m.Normals = append(m.Normals, n)
			m.UVs = append(m.UVs, math3d.Vec2{X: u, Y: v})
		}
	}
	stride := uint32(slices + 1)
	for iy := 0; iy < stacks; iy++ {
		for ix := 0; ix < slices; ix++ {
			a := uint32(iy)*stride + uint32(ix)
			b := a + 1
			c := a + stride
			d := c + 1
			// CCW outward winding.
			m.Indices = append(m.Indices, a, c, b, b, c, d)
		}
	}
	return m
}

// worldQuadMesh returns a front-facing (+Z normal) quad in world space centered at center with half-extents (hx, hy).
func worldQuadMesh(center math3d.Vec3, hx, hy float32) render3d.Mesh {
	n := math3d.Vec3{Z: 1}
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: center.X - hx, Y: center.Y - hy, Z: center.Z},
			{X: center.X + hx, Y: center.Y - hy, Z: center.Z},
			{X: center.X + hx, Y: center.Y + hy, Z: center.Z},
			{X: center.X - hx, Y: center.Y + hy, Z: center.Z},
		},
		Normals: []math3d.Vec3{n, n, n, n},
		UVs: []math3d.Vec2{
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

// bumpedNormalMap generates a procedural tangent-space normal map with a grid of rounded pillow domes.
// Left slopes tilt -X (R < 128), right slopes tilt +X (R > 128), top slopes tilt +Y (G > 128), bottom tilt -Y (G < 128).
func bumpedNormalMap(size, cells int, strength float64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			u := (float64(x) + 0.5) / float64(size) * float64(cells)
			v := (float64(y) + 0.5) / float64(size) * float64(cells)
			fx := (u-math.Floor(u))*2 - 1 // [-1, 1] left to right
			fy := (v-math.Floor(v))*2 - 1 // [-1, 1] top to bottom
			// Height field h(fx, fy) = cos(fx*pi/2)*cos(fy*pi/2); dh/du = -fx*slope, dh/dv = -fy*slope.
			// Tangent-space normal: nx = -dh/du, ny = +dh/dv (since +Y normal points up = -V).
			sx := math.Sin(fx * math.Pi * 0.5)
			sy := math.Sin(fy * math.Pi * 0.5)
			nx := sx * strength
			ny := -sy * strength
			nz := 1.0
			inv := 1.0 / math.Sqrt(nx*nx+ny*ny+nz*nz)
			r := uint8(math.Round((nx*inv*0.5 + 0.5) * 255))
			g := uint8(math.Round((ny*inv*0.5 + 0.5) * 255))
			b := uint8(math.Round((nz*inv*0.5 + 0.5) * 255))
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

// metallicRoughnessRampImage builds a glTF-packed metallic-roughness texture:
// G channel = roughness (ramps left->right from 0.08 to 0.95), B channel = metallic (top half dielectric 0, bottom half metal 1).
func metallicRoughnessRampImage(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		metallic := uint8(0)
		if y >= size/2 {
			metallic = 255
		}
		for x := range size {
			t := float64(x) / float64(max(size-1, 1))
			rough := uint8(math.Round((0.08 + 0.87*t) * 255))
			img.SetNRGBA(x, y, color.NRGBA{R: 0, G: rough, B: metallic, A: 255})
		}
	}
	return img
}

// cutoutCircleImage builds a base color texture whose alpha is 255 inside a circle and 0 outside, with warm teal albedo.
func cutoutCircleImage(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	half := float64(size) * 0.5
	rad := half * 0.72
	for y := range size {
		for x := range size {
			dx := float64(x) + 0.5 - half
			dy := float64(y) + 0.5 - half
			a := uint8(0)
			if dx*dx+dy*dy <= rad*rad {
				a = 255
			}
			img.SetNRGBA(x, y, color.NRGBA{R: 60, G: 210, B: 175, A: a})
		}
	}
	return img
}

func solidNRGBA(r, g, b, a uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	return img
}

func srgbVec4ToRGBA(v math3d.Vec4) color.RGBA {
	toByte := func(f float32) uint8 {
		if f <= 0 {
			return 0
		}
		if f >= 1 {
			return 255
		}
		return uint8(math.Round(float64(f) * 255))
	}
	return color.RGBA{R: toByte(v.X), G: toByte(v.Y), B: toByte(v.Z), A: toByte(v.W)}
}

func TestMaterialValidation(t *testing.T) {
	if err := render3d.DefaultMaterial().Validate(); err != nil {
		t.Fatalf("DefaultMaterial().Validate() = %v, want nil", err)
	}
	// Type alias MetallicRoughnessMaterial is interchangeable with Material.
	var alias render3d.MetallicRoughnessMaterial = render3d.DefaultMaterial()
	if err := alias.Validate(); err != nil {
		t.Fatalf("MetallicRoughnessMaterial.Validate() = %v, want nil", err)
	}

	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	cases := []struct {
		name   string
		mutate func(*render3d.Material)
	}{
		{"base color negative", func(m *render3d.Material) { m.BaseColorFactor[0] = -0.1 }},
		{"base color > 1", func(m *render3d.Material) { m.BaseColorFactor[2] = 1.2 }},
		{"base color NaN", func(m *render3d.Material) { m.BaseColorFactor[3] = nan }},
		{"metallic negative", func(m *render3d.Material) { m.MetallicFactor = -0.01 }},
		{"metallic > 1", func(m *render3d.Material) { m.MetallicFactor = 1.01 }},
		{"metallic Inf", func(m *render3d.Material) { m.MetallicFactor = inf }},
		{"roughness negative", func(m *render3d.Material) { m.RoughnessFactor = -0.1 }},
		{"roughness > 1", func(m *render3d.Material) { m.RoughnessFactor = 1.5 }},
		{"normal scale negative", func(m *render3d.Material) { m.NormalScale = -1 }},
		{"normal scale NaN", func(m *render3d.Material) { m.NormalScale = nan }},
		{"occlusion negative", func(m *render3d.Material) { m.OcclusionStrength = -0.2 }},
		{"occlusion > 1", func(m *render3d.Material) { m.OcclusionStrength = 1.1 }},
		{"emissive negative", func(m *render3d.Material) { m.EmissiveFactor[1] = -0.5 }},
		{"emissive Inf", func(m *render3d.Material) { m.EmissiveFactor[0] = inf }},
		{"invalid alpha mode", func(m *render3d.Material) { m.AlphaMode = render3d.AlphaMode(99) }},
		{"alpha cutoff negative", func(m *render3d.Material) { m.AlphaCutoff = -0.1 }},
		{"alpha cutoff > 1", func(m *render3d.Material) { m.AlphaCutoff = 1.1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := render3d.DefaultMaterial()
			tc.mutate(&m)
			err := m.Validate()
			if !errors.Is(err, render3d.ErrInvalidMaterial) {
				t.Errorf("Validate() = %v, want wrapping ErrInvalidMaterial", err)
			}
		})
	}

	// EmissiveFactor > 1 (HDR emission) and NormalScale > 1 are valid.
	hdr := render3d.DefaultMaterial()
	hdr.EmissiveFactor = [3]float32{4, 2, 0.5}
	hdr.NormalScale = 2.5
	if err := hdr.Validate(); err != nil {
		t.Errorf("HDR emissive / NormalScale > 1 failed validation: %v", err)
	}

	// Closed GPU texture / sampler handles must be rejected.
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()
	tex, err := render3d.UploadTexture(nil, dev, solidNRGBA(255, 255, 255, 255), render3d.TextureUpload{})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	_ = tex.Close()
	samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerLinearRepeat())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	_ = samp.Close()

	badTex := render3d.DefaultMaterial()
	badTex.BaseColorTexture = tex
	if err := badTex.Validate(); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("closed BaseColorTexture: Validate() = %v, want ErrInvalidMaterial", err)
	}
	badSamp := render3d.DefaultMaterial()
	badSamp.NormalSampler = samp
	if err := badSamp.Validate(); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("closed NormalSampler: Validate() = %v, want ErrInvalidMaterial", err)
	}

	// AlphaMode helpers.
	if got := render3d.AlphaOpaque.String(); got != "opaque" {
		t.Errorf("AlphaOpaque.String() = %q, want opaque", got)
	}
	if got := render3d.AlphaMask.String(); got != "mask" {
		t.Errorf("AlphaMask.String() = %q, want mask", got)
	}
	if got := render3d.AlphaBlend.String(); got != "blend" {
		t.Errorf("AlphaBlend.String() = %q, want blend", got)
	}
	if got := render3d.AlphaMode(7).String(); got != "unknown" {
		t.Errorf("AlphaMode(7).String() = %q, want unknown", got)
	}
	if render3d.AlphaOpaque.BlendMode() != render3d.BlendOpaque ||
		render3d.AlphaMask.BlendMode() != render3d.BlendAlphaMask ||
		render3d.AlphaBlend.BlendMode() != render3d.BlendAlphaBlend {
		t.Error("AlphaMode.BlendMode mapping mismatch")
	}
}

func TestMaterialUniformEncodingAndZeroAllocs(t *testing.T) {
	u := render3d.MaterialUniforms{
		BaseColorFactor:   [4]float32{0.2, 0.4, 0.6, 0.8},
		EmissiveFactor:    [3]float32{1.5, 0.5, 0.25},
		NormalScale:       1.25,
		MetallicFactor:    0.75,
		RoughnessFactor:   0.35,
		OcclusionStrength: 0.9,
		AlphaCutoff:       0.45,
		AlphaMode:         render3d.AlphaMask,
		DoubleSided:       true,
		HasNormalMap:      true,
		HasEmissiveMap:    false,
	}

	buf := u.AppendUniformData(nil)
	if len(buf) != render3d.MaterialUniformSize {
		t.Fatalf("len(AppendUniformData) = %d, want %d", len(buf), render3d.MaterialUniformSize)
	}
	if render3d.MaterialUniformSize%16 != 0 {
		t.Fatalf("MaterialUniformSize = %d, must be 16-byte aligned", render3d.MaterialUniformSize)
	}
	want := []float32{
		0.2, 0.4, 0.6, 0.8,
		1.5, 0.5, 0.25, 1.25,
		0.75, 0.35, 0.9, 0.45,
		1, 1, 1, 0,
	}
	for i, w := range want {
		got := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
		if got != w {
			t.Errorf("float[%d] = %v, want %v", i, got, w)
		}
	}

	// Zero allocations when dst has capacity.
	dst := make([]byte, 0, render3d.MaterialUniformSize)
	allocs := testing.AllocsPerRun(200, func() {
		dst = u.AppendUniformData(dst[:0])
	})
	if allocs != 0 {
		t.Errorf("AppendUniformData allocs = %v, want 0", allocs)
	}

	// Encode panics when dst is shorter than MaterialUniformSize.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Encode with short buffer did not panic")
			}
		}()
		var short [render3d.MaterialUniformSize - 1]byte
		u.Encode(short[:])
	}()
}

func BenchmarkMaterialAppendUniformData(b *testing.B) {
	m := render3d.DefaultMaterial()
	m.BaseColorFactor = [4]float32{0.9, 0.7, 0.3, 1}
	m.MetallicFactor = 0.85
	m.RoughnessFactor = 0.25
	buf := make([]byte, 0, render3d.MaterialUniformSize)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buf = m.AppendUniformData(buf[:0])
	}
	_ = buf
}

func TestMaterialDefaultsAndLifecycle(t *testing.T) {
	if _, err := render3d.NewMaterialDefaults(nil, nil, render3d.MaterialDefaultsOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewMaterialDefaults(nil dev) = %v, want gpu.ErrDeviceLost", err)
	}
	if _, err := render3d.MaterialBindGroupLayout(nil, nil); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("MaterialBindGroupLayout(nil dev) = %v, want gpu.ErrDeviceLost", err)
	}

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{Label: "test"})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	if !defaults.Valid() {
		t.Fatal("defaults not valid after creation")
	}
	if defaults.WhiteSRGB().Format() != gpu.FormatRGBA8UnormSRGB ||
		defaults.WhiteLinear().Format() != gpu.FormatRGBA8Unorm ||
		defaults.FlatNormal().Format() != gpu.FormatRGBA8Unorm ||
		defaults.Black().Format() != gpu.FormatRGBA8UnormSRGB {
		t.Error("unexpected fallback texture formats")
	}
	// Four 1x1 RGBA8 textures = 16 bytes.
	if got := defaults.ByteSize(); got != 16 {
		t.Errorf("defaults.ByteSize() = %d, want 16", got)
	}

	gm, err := render3d.NewGPUMaterial(nil, dev, defaults, render3d.DefaultMaterial(), render3d.MaterialUpload{Label: "mat"})
	if err != nil {
		t.Fatalf("NewGPUMaterial: %v", err)
	}
	if !gm.Valid() || gm.ByteSize() != render3d.MaterialUniformSize {
		t.Errorf("GPUMaterial valid=%v byteSize=%d, want true and %d", gm.Valid(), gm.ByteSize(), render3d.MaterialUniformSize)
	}
	if gm.BindGroup() == nil || gm.UniformBuffer() == nil || gm.AlphaMode() != render3d.AlphaOpaque || gm.DoubleSided() {
		t.Error("unexpected GPUMaterial accessors")
	}

	// Explicit Close releases GPUMaterial and MaterialDefaults cleanly and is idempotent.
	if err := gm.Close(); err != nil {
		t.Fatalf("gm.Close: %v", err)
	}
	if gm.Valid() {
		t.Error("GPUMaterial still valid after Close")
	}
	if err := gm.Close(); err != nil {
		t.Errorf("second gm.Close: %v", err)
	}
	if err := defaults.Close(); err != nil {
		t.Fatalf("defaults.Close: %v", err)
	}
	if defaults.Valid() {
		t.Error("MaterialDefaults still valid after Close")
	}
	if err := defaults.Close(); err != nil {
		t.Errorf("second defaults.Close: %v", err)
	}

	// Scope disposal automatically releases both MaterialDefaults and GPUMaterial.
	var (
		scopedDefs *render3d.MaterialDefaults
		scopedMat  *render3d.GPUMaterial
		show       *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					var err error
					scopedDefs, err = render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{})
					if err != nil {
						t.Fatalf("NewMaterialDefaults: %v", err)
					}
					scopedMat, err = render3d.NewGPUMaterial(cx, dev, scopedDefs, render3d.DefaultMaterial(), render3d.MaterialUpload{})
					if err != nil {
						t.Fatalf("NewGPUMaterial: %v", err)
					}
				},
				OnFrame: func(frame *gpu.Frame) { frame.Submit() },
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())
	if !scopedDefs.Valid() || !scopedMat.Valid() {
		t.Fatal("scoped material resources not valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if scopedDefs.Valid() {
		t.Error("MaterialDefaults still valid after scope disposal")
	}
	if scopedMat.Valid() {
		t.Error("GPUMaterial still valid after scope disposal")
	}

	// Nil receivers and double Close are safe.
	var nilDefs *render3d.MaterialDefaults
	if nilDefs.Valid() || nilDefs.ByteSize() != 0 || nilDefs.Layout() != nil || nilDefs.Close() != nil {
		t.Error("nil MaterialDefaults failed safety checks")
	}
	var nilMat *render3d.GPUMaterial
	if nilMat.Valid() || nilMat.ByteSize() != 0 || nilMat.BindGroup() != nil || nilMat.Close() != nil {
		t.Error("nil GPUMaterial failed safety checks")
	}
	nilMat.Bind(nil)
}

func TestEvaluateMaterialCPUProperties(t *testing.T) {
	view := math3d.Vec3{Z: 1}
	norm := math3d.Vec3{Z: 1}

	// 1. AlphaMask discards below cutoff and keeps at/above cutoff.
	maskMat := render3d.DefaultMaterial()
	maskMat.AlphaMode = render3d.AlphaMask
	maskMat.AlphaCutoff = 0.5
	maskMat.BaseColorFactor[3] = 0.4
	if res := render3d.EvaluateMaterial(render3d.MaterialEvalInput{Material: maskMat, Normal: norm, ViewDir: view, FrontFacing: true}); !res.Discarded {
		t.Error("AlphaMask alpha=0.4 < 0.5 was not discarded")
	}
	maskMat.BaseColorFactor[3] = 0.6
	if res := render3d.EvaluateMaterial(render3d.MaterialEvalInput{Material: maskMat, Normal: norm, ViewDir: view, FrontFacing: true}); res.Discarded || res.SRGB.W != 1 {
		t.Errorf("AlphaMask alpha=0.6 discarded=%v alpha=%v, want false and 1", res.Discarded, res.SRGB.W)
	}

	// 2. Roughness clamps to >= 0.045 even when RoughnessFactor is 0.
	mirror := render3d.DefaultMaterial()
	mirror.MetallicFactor = 1
	mirror.RoughnessFactor = 0
	mRes := render3d.EvaluateMaterial(render3d.MaterialEvalInput{Material: mirror, Normal: norm, ViewDir: view, FrontFacing: true})
	if mRes.Roughness < 0.044 || mRes.Roughness > 0.046 {
		t.Errorf("mirror Roughness = %v, want ~0.045", mRes.Roughness)
	}

	// 3. DoubleSided flips geometric normal on back faces.
	ds := render3d.DefaultMaterial()
	ds.DoubleSided = true
	ds.MetallicFactor = 0
	backRes := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:    ds,
		Normal:      math3d.Vec3{Z: -1},
		ViewDir:     view,
		FrontFacing: false,
	})
	if !backRes.Normal.ApproxEqual(math3d.Vec3{Z: 1}, 1e-5) {
		t.Errorf("DoubleSided back-face normal = %v, want (0,0,1)", backRes.Normal)
	}

	// 4. Metallic surface tints specular highlight with base color (gold R > B), whereas dielectric highlight is neutral/achromatic.
	light := render3d.MaterialLight{
		Direction:   math3d.Vec3{Z: 1}, // head-on mirror reflection along V
		Color:       math3d.Vec3{X: 3, Y: 3, Z: 3},
		SkyColor:    math3d.Vec3{},
		GroundColor: math3d.Vec3{},
	}
	gold := render3d.DefaultMaterial()
	gold.BaseColorFactor = [4]float32{1.0, 0.76, 0.25, 1}
	gold.MetallicFactor = 1
	gold.RoughnessFactor = 0.25
	goldRes := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:    gold,
		Normal:      norm,
		ViewDir:     view,
		FrontFacing: true,
		Light:       light,
	})
	if goldRes.Linear.X <= goldRes.Linear.Z*2 {
		t.Errorf("metallic gold specular = %v, want strongly warm-tinted (R >> B)", goldRes.Linear)
	}

	// 5. Normal map tilting toward the light increases radiance vs tilting away from the light.
	sideLight := render3d.MaterialLight{
		Direction:   math3d.Vec3{X: 0.8, Y: 0, Z: 0.6}.Norm(),
		Color:       math3d.Vec3{X: 2.5, Y: 2.5, Z: 2.5},
		SkyColor:    math3d.Vec3{X: 0.1, Y: 0.1, Z: 0.1},
		GroundColor: math3d.Vec3{X: 0.1, Y: 0.1, Z: 0.1},
	}
	bumped := render3d.DefaultMaterial()
	bumped.MetallicFactor = 0
	bumped.RoughnessFactor = 0.5
	tiltRight := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:        bumped,
		NormalSample:    [3]float32{0.9, 0.5, 0.75}, // +X normal tilt -> faces +X light
		HasNormalSample: true,
		Normal:          norm,
		ViewDir:         view,
		FrontFacing:     true,
		Light:           sideLight,
	})
	tiltLeft := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:        bumped,
		NormalSample:    [3]float32{0.1, 0.5, 0.75}, // -X normal tilt -> turns away from +X light
		HasNormalSample: true,
		Normal:          norm,
		ViewDir:         view,
		FrontFacing:     true,
		Light:           sideLight,
	})
	if tiltRight.Linear.X <= tiltLeft.Linear.X*1.5 {
		t.Errorf("normal map tilt right (%v) did not brighten vs tilt left (%v)", tiltRight.Linear.X, tiltLeft.Linear.X)
	}
}

func TestGPUMaterialMatchesCPUReference(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	defer func() { _ = defaults.Close() }()

	pipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline: %v", err)
	}
	defer func() { _ = pipe.Close() }()

	quad, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{}, 2, 2), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	defer func() { _ = quad.Close() }()

	camPos := math3d.Vec3{Z: 3}
	cam := scene3d.NewPerspectiveCamera(camPos, math3d.DegToRad(50), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	// Textures for testing packed MR (G=roughness, B=metallic), normal map, occlusion, emissive.
	mrTex, err := render3d.UploadTexture(nil, dev, solidNRGBA(0, 64, 255, 255), render3d.TextureUpload{Label: "mr"}) // G~0.251, B=1.0
	if err != nil {
		t.Fatalf("UploadTexture mr: %v", err)
	}
	defer func() { _ = mrTex.Close() }()

	tiltRightTex, err := render3d.UploadTexture(nil, dev, solidNRGBA(220, 128, 220, 255), render3d.TextureUpload{Label: "norm-right"})
	if err != nil {
		t.Fatalf("UploadTexture norm-right: %v", err)
	}
	defer func() { _ = tiltRightTex.Close() }()

	tiltLeftTex, err := render3d.UploadTexture(nil, dev, solidNRGBA(36, 128, 220, 255), render3d.TextureUpload{Label: "norm-left"})
	if err != nil {
		t.Fatalf("UploadTexture norm-left: %v", err)
	}
	defer func() { _ = tiltLeftTex.Close() }()

	occTex, err := render3d.UploadTexture(nil, dev, solidNRGBA(64, 64, 64, 255), render3d.TextureUpload{Label: "occ"}) // R~0.251
	if err != nil {
		t.Fatalf("UploadTexture occ: %v", err)
	}
	defer func() { _ = occTex.Close() }()

	cases := []struct {
		name  string
		mat   render3d.Material
		input render3d.MaterialEvalInput
	}{
		{
			name: "matte-dielectric",
			mat: func() render3d.Material {
				m := render3d.DefaultMaterial()
				m.BaseColorFactor = [4]float32{0.82, 0.18, 0.16, 1}
				m.MetallicFactor = 0
				m.RoughnessFactor = 0.85
				return m
			}(),
		},
		{
			name: "brushed-gold-via-mr-map",
			mat: func() render3d.Material {
				m := render3d.DefaultMaterial()
				m.BaseColorFactor = [4]float32{1.0, 0.78, 0.34, 1}
				m.MetallicFactor = 1
				m.RoughnessFactor = 1
				m.MetallicRoughnessTexture = mrTex
				return m
			}(),
			input: render3d.MaterialEvalInput{
				HasMetallicRoughnessSample: true,
				RoughnessSample:            64.0 / 255.0,
				MetallicSample:             1.0,
			},
		},
		{
			name: "normal-map-tilted-right",
			mat: func() render3d.Material {
				m := render3d.DefaultMaterial()
				m.BaseColorFactor = [4]float32{0.6, 0.7, 0.85, 1}
				m.MetallicFactor = 0.1
				m.RoughnessFactor = 0.35
				m.NormalTexture = tiltRightTex
				m.NormalScale = 1.0
				return m
			}(),
			input: render3d.MaterialEvalInput{
				HasNormalSample: true,
				NormalSample:    [3]float32{220.0 / 255.0, 128.0 / 255.0, 220.0 / 255.0},
			},
		},
		{
			name: "normal-map-tilted-left",
			mat: func() render3d.Material {
				m := render3d.DefaultMaterial()
				m.BaseColorFactor = [4]float32{0.6, 0.7, 0.85, 1}
				m.MetallicFactor = 0.1
				m.RoughnessFactor = 0.35
				m.NormalTexture = tiltLeftTex
				m.NormalScale = 1.0
				return m
			}(),
			input: render3d.MaterialEvalInput{
				HasNormalSample: true,
				NormalSample:    [3]float32{36.0 / 255.0, 128.0 / 255.0, 220.0 / 255.0},
			},
		},
		{
			name: "occlusion-and-emissive",
			mat: func() render3d.Material {
				m := render3d.DefaultMaterial()
				m.BaseColorFactor = [4]float32{0.3, 0.35, 0.4, 1}
				m.MetallicFactor = 0
				m.RoughnessFactor = 0.7
				m.OcclusionTexture = occTex
				m.OcclusionStrength = 0.9
				m.EmissiveFactor = [3]float32{0.1, 0.65, 0.35}
				return m
			}(),
			input: render3d.MaterialEvalInput{
				HasOcclusionSample: true,
				OcclusionSample:    64.0 / 255.0,
			},
		},
	}

	var rightLuma, leftLuma int
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gm, err := render3d.NewGPUMaterial(nil, dev, defaults, tc.mat, render3d.MaterialUpload{Label: tc.name})
			if err != nil {
				t.Fatalf("NewGPUMaterial: %v", err)
			}
			defer func() { _ = gm.Close() }()

			enc := dev.BeginEncoder()
			pass := enc.BeginRenderPass(gpu.RenderPassDesc{
				ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
				DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
			})
			pass.SetPipeline(pipe)
			pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
			gm.Bind(pass)
			quad.Draw(pass)
			pass.End()
			dev.Submit(enc.Finish())
			sr.WaitIdle()
			if err := sr.LastError(); err != nil {
				t.Fatalf("software renderer: %v", err)
			}

			got := sr.Image().RGBAAt(size/2, size/2)
			in := tc.input
			in.Material = tc.mat
			in.Normal = math3d.Vec3{Z: 1}
			in.ViewDir = math3d.Vec3{Z: 1}
			in.FrontFacing = true
			want := srgbVec4ToRGBA(render3d.EvaluateMaterial(in).SRGB)
			near := func(a, b uint8) bool { return max(a, b)-min(a, b) <= 3 }
			if !near(got.R, want.R) || !near(got.G, want.G) || !near(got.B, want.B) || !near(got.A, want.A) {
				t.Errorf("center pixel = %v, CPU reference EvaluateMaterial = %v", got, want)
			}
			if tc.name == "normal-map-tilted-right" {
				rightLuma = int(got.R) + int(got.G) + int(got.B)
			}
			if tc.name == "normal-map-tilted-left" {
				leftLuma = int(got.R) + int(got.G) + int(got.B)
			}
		})
	}
	if rightLuma <= leftLuma+40 {
		t.Errorf("normal map tilted toward +X key light (luma %d) should be significantly brighter than tilted away (luma %d)", rightLuma, leftLuma)
	}
}

func TestMaterialAlphaModesDoubleSidedAndUpdate(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	defer func() { _ = defaults.Close() }()

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3.5}, math3d.DegToRad(50), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	opaquePipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		AlphaMode:   render3d.AlphaOpaque,
	})
	if err != nil {
		t.Fatalf("opaque pipeline: %v", err)
	}
	maskPipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		AlphaMode:   render3d.AlphaMask,
	})
	if err != nil {
		t.Fatalf("mask pipeline: %v", err)
	}
	blendPipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		AlphaMode:   render3d.AlphaBlend,
	})
	if err != nil {
		t.Fatalf("blend pipeline: %v", err)
	}
	doublePipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		DoubleSided: true,
	})
	if err != nil {
		t.Fatalf("double-sided pipeline: %v", err)
	}

	backQuad, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{Z: -0.5}, 2.2, 2.2), render3d.MeshUpload{Label: "back"})
	if err != nil {
		t.Fatalf("UploadMesh back: %v", err)
	}
	frontQuad, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{Z: 0.2}, 1.4, 1.4), render3d.MeshUpload{Label: "front"})
	if err != nil {
		t.Fatalf("UploadMesh front: %v", err)
	}

	// 1. AlphaMask: cutout circle texture discards outer corners so red backdrop shows through, while center shows teal cutout.
	redMat := render3d.DefaultMaterial()
	redMat.BaseColorFactor = [4]float32{0.9, 0.1, 0.1, 1}
	redMat.MetallicFactor = 0
	redMat.RoughnessFactor = 0.8
	redGPU, err := render3d.NewGPUMaterial(nil, dev, defaults, redMat, render3d.MaterialUpload{Label: "red"})
	if err != nil {
		t.Fatalf("NewGPUMaterial red: %v", err)
	}

	cutoutTex, err := render3d.UploadTexture(nil, dev, cutoutCircleImage(32), render3d.TextureUpload{Label: "cutout", SRGB: true})
	if err != nil {
		t.Fatalf("UploadTexture cutout: %v", err)
	}
	cutoutMat := render3d.DefaultMaterial()
	cutoutMat.BaseColorTexture = cutoutTex
	cutoutMat.MetallicFactor = 0
	cutoutMat.RoughnessFactor = 0.6
	cutoutMat.AlphaMode = render3d.AlphaMask
	cutoutMat.AlphaCutoff = 0.5
	cutoutGPU, err := render3d.NewGPUMaterial(nil, dev, defaults, cutoutMat, render3d.MaterialUpload{Label: "cutout"})
	if err != nil {
		t.Fatalf("NewGPUMaterial cutout: %v", err)
	}

	renderFrame := func(draw func(*gpu.RenderPass)) *image.RGBA {
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return sr.Image()
	}

	maskImg := renderFrame(func(pass *gpu.RenderPass) {
		pass.SetPipeline(opaquePipe)
		redGPU.Bind(pass)
		backQuad.Draw(pass)

		pass.SetPipeline(maskPipe)
		cutoutGPU.Bind(pass)
		frontQuad.Draw(pass)
	})
	center := maskImg.RGBAAt(size/2, size/2)
	cornerOfFront := maskImg.RGBAAt(7, 7) // inside frontQuad bounds (~3..61), outside cutout circle (r=0.36)
	if center.G <= center.R+40 {
		t.Errorf("AlphaMask center = %v, want teal cutout visible", center)
	}
	if cornerOfFront.R <= cornerOfFront.G+40 {
		t.Errorf("AlphaMask corner = %v, want discarded fragment revealing red backdrop", cornerOfFront)
	}

	// 2. AlphaBlend: 50% blue glass quad over red backdrop produces a blended purple pixel.
	glassMat := render3d.DefaultMaterial()
	glassMat.BaseColorFactor = [4]float32{0.1, 0.25, 0.95, 0.5}
	glassMat.MetallicFactor = 0
	glassMat.RoughnessFactor = 0.3
	glassMat.AlphaMode = render3d.AlphaBlend
	glassGPU, err := render3d.NewGPUMaterial(nil, dev, defaults, glassMat, render3d.MaterialUpload{Label: "glass"})
	if err != nil {
		t.Fatalf("NewGPUMaterial glass: %v", err)
	}
	blendImg := renderFrame(func(pass *gpu.RenderPass) {
		pass.SetPipeline(opaquePipe)
		redGPU.Bind(pass)
		backQuad.Draw(pass)

		pass.SetPipeline(blendPipe)
		glassGPU.Bind(pass)
		frontQuad.Draw(pass)
	})
	blended := blendImg.RGBAAt(size/2, size/2)
	if blended.R < 80 || blended.B < 80 {
		t.Errorf("AlphaBlend center = %v, want blended red+blue midtone", blended)
	}

	// 3. DoubleSided: reversed-winding quad is culled with opaquePipe, visible & lit with doublePipe.
	flipped := worldQuadMesh(math3d.Vec3{Z: 0.2}, 1.4, 1.4)
	flipped.Indices = []uint32{0, 2, 1, 0, 3, 2}
	for i := range flipped.Normals {
		flipped.Normals[i] = math3d.Vec3{Z: -1}
	}
	flippedGPU, err := render3d.UploadMesh(nil, dev, flipped, render3d.MeshUpload{Label: "flipped"})
	if err != nil {
		t.Fatalf("UploadMesh flipped: %v", err)
	}
	dsMat := render3d.DefaultMaterial()
	dsMat.BaseColorFactor = [4]float32{0.2, 0.85, 0.3, 1}
	dsMat.MetallicFactor = 0
	dsMat.RoughnessFactor = 0.6
	dsMat.DoubleSided = true
	dsGPU, err := render3d.NewGPUMaterial(nil, dev, defaults, dsMat, render3d.MaterialUpload{Label: "ds"})
	if err != nil {
		t.Fatalf("NewGPUMaterial ds: %v", err)
	}
	culledImg := renderFrame(func(pass *gpu.RenderPass) {
		pass.SetPipeline(opaquePipe)
		dsGPU.Bind(pass)
		flippedGPU.Draw(pass)
	})
	wantPixel(t, culledImg, size/2, size/2, meshClear)

	dsImg := renderFrame(func(pass *gpu.RenderPass) {
		pass.SetPipeline(doublePipe)
		dsGPU.Bind(pass)
		flippedGPU.Draw(pass)
	})
	if c := dsImg.RGBAAt(size/2, size/2); c.G < 120 {
		t.Errorf("DoubleSided back-face pixel = %v, want lit green surface", c)
	}

	// 4. Update dynamically changes factors without rebuilding bind group, and rejects swapping texture handles.
	updatedMat := redMat
	updatedMat.BaseColorFactor = [4]float32{0.1, 0.2, 0.95, 1}
	updatedMat.EmissiveFactor = [3]float32{0, 0.1, 0.8}
	if err := redGPU.Update(dev, updatedMat); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updImg := renderFrame(func(pass *gpu.RenderPass) {
		pass.SetPipeline(opaquePipe)
		redGPU.Bind(pass)
		backQuad.Draw(pass)
	})
	if c := updImg.RGBAAt(size/2, size/2); c.B <= c.R+60 {
		t.Errorf("after Update to blue emissive: pixel = %v, want blue-dominant", c)
	}

	swapTex := updatedMat
	swapTex.BaseColorTexture = cutoutTex
	if err := redGPU.Update(dev, swapTex); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("Update swapping texture = %v, want ErrInvalidMaterial", err)
	}
}

func TestMetallicRoughnessRampMatchesReference(t *testing.T) {
	const (
		width  = 192
		height = 128
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
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

	pipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 0.35, Z: 6.4}, math3d.DegToRad(36), float32(width)/float32(height), 0.1, 50)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0, Z: 0}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	type sphereSpec struct {
		pos       math3d.Vec3
		baseColor [4]float32
		metallic  float32
		roughness float32
	}
	// Top row: dielectric crimson ramping smooth -> rough.
	// Bottom row: metallic gold ramping mirror -> brushed -> matte.
	specs := []sphereSpec{
		{math3d.Vec3{X: -1.75, Y: 0.85}, [4]float32{0.82, 0.12, 0.14, 1}, 0.0, 0.12},
		{math3d.Vec3{X: 0.0, Y: 0.85}, [4]float32{0.82, 0.12, 0.14, 1}, 0.0, 0.45},
		{math3d.Vec3{X: 1.75, Y: 0.85}, [4]float32{0.82, 0.12, 0.14, 1}, 0.0, 0.90},
		{math3d.Vec3{X: -1.75, Y: -0.85}, [4]float32{1.0, 0.78, 0.34, 1}, 1.0, 0.08},
		{math3d.Vec3{X: 0.0, Y: -0.85}, [4]float32{1.0, 0.78, 0.34, 1}, 1.0, 0.35},
		{math3d.Vec3{X: 1.75, Y: -0.85}, [4]float32{1.0, 0.78, 0.34, 1}, 1.0, 0.80},
	}

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "pbr-metallic-roughness-ramp",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())

	for i, sp := range specs {
		gm, err := render3d.UploadMesh(nil, dev, uvSphereMesh(sp.pos, 0.68, 18, 28), render3d.MeshUpload{Label: "sphere"})
		if err != nil {
			t.Fatalf("UploadMesh %d: %v", i, err)
		}
		mat := render3d.DefaultMaterial()
		mat.BaseColorFactor = sp.baseColor
		mat.MetallicFactor = sp.metallic
		mat.RoughnessFactor = sp.roughness
		gmat, err := render3d.NewGPUMaterial(nil, dev, defaults, mat, render3d.MaterialUpload{Label: "sphere-mat"})
		if err != nil {
			t.Fatalf("NewGPUMaterial %d: %v", i, err)
		}
		gmat.Bind(pass)
		gm.Draw(pass)
	}

	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	assertReference(t, "pbr_metallic_roughness_ramp", img)

	// Analytical sanity checks:
	// 1. Background corner is clear color.
	wantPixel(t, img, 2, 2, meshClear)
	// 2. Smooth mirror gold (bottom-left) has higher peak specular intensity than rough matte gold (bottom-right).
	peakLuma := func(center math3d.Vec3) int {
		s, _, ok := cam.WorldToScreen(center, width, height)
		if !ok {
			t.Fatalf("sphere center %v not visible", center)
		}
		cx, cy := int(s.X), int(s.Y)
		best := 0
		for dy := -20; dy <= 20; dy++ {
			for dx := -20; dx <= 20; dx++ {
				c := img.RGBAAt(cx+dx, cy+dy)
				if l := int(c.R) + int(c.G) + int(c.B); l > best {
					best = l
				}
			}
		}
		return best
	}
	mirrorPeak := peakLuma(specs[3].pos)
	mattePeak := peakLuma(specs[5].pos)
	if mirrorPeak <= mattePeak+40 {
		t.Errorf("mirror gold peak luma (%d) should exceed matte gold peak luma (%d)", mirrorPeak, mattePeak)
	}
}

func TestTexturedNormalMappedPBRSceneMatchesReference(t *testing.T) {
	const (
		width  = 160
		height = 120
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
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

	opaquePipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		AlphaMode:   render3d.AlphaOpaque,
	})
	if err != nil {
		t.Fatalf("opaque pipeline: %v", err)
	}
	maskPipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		AlphaMode:   render3d.AlphaMask,
	})
	if err != nil {
		t.Fatalf("mask pipeline: %v", err)
	}
	blendPipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		AlphaMode:   render3d.AlphaBlend,
	})
	if err != nil {
		t.Fatalf("blend pipeline: %v", err)
	}

	// Ground plane: textured albedo + bumpy normal map.
	floorAlbedo, err := render3d.UploadTexture(nil, dev, checkerImage(16, color.NRGBA{R: 205, G: 198, B: 185, A: 255}, color.NRGBA{R: 72, G: 84, B: 102, A: 255}), render3d.TextureUpload{Label: "floor-albedo", SRGB: true, Mipmaps: true})
	if err != nil {
		t.Fatalf("floor albedo: %v", err)
	}
	bumpTex, err := render3d.UploadTexture(nil, dev, bumpedNormalMap(32, 4, 1.4), render3d.TextureUpload{Label: "bumps"})
	if err != nil {
		t.Fatalf("bumps: %v", err)
	}
	mrTex, err := render3d.UploadTexture(nil, dev, metallicRoughnessRampImage(32), render3d.TextureUpload{Label: "mr-ramp"})
	if err != nil {
		t.Fatalf("mr-ramp: %v", err)
	}
	cutoutTex, err := render3d.UploadTexture(nil, dev, cutoutCircleImage(32), render3d.TextureUpload{Label: "cutout", SRGB: true})
	if err != nil {
		t.Fatalf("cutout: %v", err)
	}

	floorMat := render3d.DefaultMaterial()
	floorMat.BaseColorTexture = floorAlbedo
	floorMat.MetallicFactor = 0.0
	floorMat.RoughnessFactor = 0.72
	floorMat.NormalTexture = bumpTex
	floorMat.NormalScale = 0.8
	floorGPU, err := render3d.NewGPUMaterial(nil, dev, defaults, floorMat, render3d.MaterialUpload{Label: "floor"})
	if err != nil {
		t.Fatalf("floor mat: %v", err)
	}
	floorMesh, err := render3d.UploadMesh(nil, dev, groundPlaneMesh(4.5, 3), render3d.MeshUpload{Label: "floor"})
	if err != nil {
		t.Fatalf("floor mesh: %v", err)
	}

	// Left hero sphere: textured metallic-roughness ramp + bumped normal map.
	sphereMat := render3d.DefaultMaterial()
	sphereMat.BaseColorFactor = [4]float32{0.96, 0.76, 0.36, 1}
	sphereMat.MetallicFactor = 1.0
	sphereMat.RoughnessFactor = 1.0
	sphereMat.MetallicRoughnessTexture = mrTex
	sphereMat.NormalTexture = bumpTex
	sphereMat.NormalScale = 1.2
	sphereGPU, err := render3d.NewGPUMaterial(nil, dev, defaults, sphereMat, render3d.MaterialUpload{Label: "hero-sphere"})
	if err != nil {
		t.Fatalf("sphere mat: %v", err)
	}
	sphereMesh, err := render3d.UploadMesh(nil, dev, uvSphereMesh(math3d.Vec3{X: -1.15, Y: 0.85, Z: 0.25}, 0.82, 22, 32), render3d.MeshUpload{Label: "hero-sphere"})
	if err != nil {
		t.Fatalf("sphere mesh: %v", err)
	}

	// Right cube: bumped normal map + emissive tint.
	cubeMat := render3d.DefaultMaterial()
	cubeMat.BaseColorFactor = [4]float32{0.22, 0.52, 0.88, 1}
	cubeMat.MetallicFactor = 0.75
	cubeMat.RoughnessFactor = 0.28
	cubeMat.NormalTexture = bumpTex
	cubeMat.NormalScale = 1.3
	cubeMat.EmissiveFactor = [3]float32{0.02, 0.05, 0.09}
	cubeGPU, err := render3d.NewGPUMaterial(nil, dev, defaults, cubeMat, render3d.MaterialUpload{Label: "hero-cube"})
	if err != nil {
		t.Fatalf("cube mat: %v", err)
	}
	cubeMesh, err := render3d.UploadMesh(nil, dev, texturedCubeMesh(math3d.Vec3{X: 1.15, Y: 0.65, Z: -0.25}, 0.65, 1), render3d.MeshUpload{Label: "hero-cube"})
	if err != nil {
		t.Fatalf("cube mesh: %v", err)
	}

	// Center alpha-masked medallion + foreground translucent blue glass panel.
	cutoutMat := render3d.DefaultMaterial()
	cutoutMat.BaseColorTexture = cutoutTex
	cutoutMat.MetallicFactor = 0.15
	cutoutMat.RoughnessFactor = 0.4
	cutoutMat.AlphaMode = render3d.AlphaMask
	cutoutMat.AlphaCutoff = 0.5
	cutoutGPU, err := render3d.NewGPUMaterial(nil, dev, defaults, cutoutMat, render3d.MaterialUpload{Label: "cutout"})
	if err != nil {
		t.Fatalf("cutout mat: %v", err)
	}
	cutoutMesh, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{X: 0.05, Y: 0.75, Z: 0.85}, 0.55, 0.55), render3d.MeshUpload{Label: "cutout"})
	if err != nil {
		t.Fatalf("cutout mesh: %v", err)
	}

	glassMat := render3d.DefaultMaterial()
	glassMat.BaseColorFactor = [4]float32{0.35, 0.78, 1.0, 0.42}
	glassMat.MetallicFactor = 0.05
	glassMat.RoughnessFactor = 0.12
	glassMat.AlphaMode = render3d.AlphaBlend
	glassGPU, err := render3d.NewGPUMaterial(nil, dev, defaults, glassMat, render3d.MaterialUpload{Label: "glass"})
	if err != nil {
		t.Fatalf("glass mat: %v", err)
	}
	glassMesh, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{X: 0.75, Y: 0.55, Z: 1.55}, 0.65, 0.5), render3d.MeshUpload{Label: "glass"})
	if err != nil {
		t.Fatalf("glass mesh: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0.35, Y: 2.3, Z: 4.8}, math3d.DegToRad(44), float32(width)/float32(height), 0.1, 50)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0.6, Z: 0}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "pbr-textured-normal-scene",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())

	pass.SetPipeline(opaquePipe)
	floorGPU.Bind(pass)
	floorMesh.Draw(pass)
	sphereGPU.Bind(pass)
	sphereMesh.Draw(pass)
	cubeGPU.Bind(pass)
	cubeMesh.Draw(pass)

	pass.SetPipeline(maskPipe)
	cutoutGPU.Bind(pass)
	cutoutMesh.Draw(pass)

	pass.SetPipeline(blendPipe)
	glassGPU.Bind(pass)
	glassMesh.Draw(pass)

	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	assertReference(t, "pbr_textured_normal_maps", img)
	wantPixel(t, img, 2, 2, meshClear)
}

func TestMaterialDrawsInGPUView(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			const size = 64
			var (
				camBuf   *render3d.CameraBuffer
				defaults *render3d.MaterialDefaults
				mesh     *render3d.GPUMesh
				mat      *render3d.GPUMaterial
				pipe     *gpu.Pipeline
			)
			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "viewport",
					Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						var err error
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						defaults, err = render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{})
						if err != nil {
							t.Fatalf("NewMaterialDefaults: %v", err)
						}
						pipe, err = render3d.NewMaterialPipeline(cx, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
							DepthFormat: gpu.FormatDepth24Plus,
						})
						if err != nil {
							t.Fatalf("NewMaterialPipeline: %v", err)
						}
						mesh, err = render3d.UploadMesh(cx, dev, uvSphereMesh(math3d.Vec3{}, 0.85, 14, 20), render3d.MeshUpload{Label: "sphere"})
						if err != nil {
							t.Fatalf("UploadMesh: %v", err)
						}
						bumpTex, err := render3d.UploadTexture(cx, dev, bumpedNormalMap(16, 2, 1.2), render3d.TextureUpload{Label: "bump"})
						if err != nil {
							t.Fatalf("UploadTexture bump: %v", err)
						}
						mrTex, err := render3d.UploadTexture(cx, dev, metallicRoughnessRampImage(16), render3d.TextureUpload{Label: "mr"})
						if err != nil {
							t.Fatalf("UploadTexture mr: %v", err)
						}
						m := render3d.DefaultMaterial()
						m.BaseColorFactor = [4]float32{0.95, 0.72, 0.25, 1}
						m.MetallicFactor = 1
						m.RoughnessFactor = 1
						m.MetallicRoughnessTexture = mrTex
						m.NormalTexture = bumpTex
						m.NormalScale = 1.0
						mat, err = render3d.NewGPUMaterial(cx, dev, defaults, m, render3d.MaterialUpload{Label: "gold"})
						if err != nil {
							t.Fatalf("NewGPUMaterial: %v", err)
						}
						cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(45), 1, 0.1, 20)
						cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
						camBuf.WriteCamera(cam)
					},
					OnFrame: func(frame *gpu.Frame) {
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x10141c))
						pass.ClearDepth(1)
						pass.SetPipeline(pipe)
						pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						mat.Bind(pass)
						mesh.Draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			img, err := app.GPUReadPixels("id:viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			wantPixel(t, img, 2, 2, meshClear)
			center := img.RGBAAt(size/2, size/2)
			if center == meshClear || center.R <= center.B+20 {
				t.Errorf("center pixel = %v, want warm lit metallic-roughness sphere", center)
			}
		})
	}
}

func TestRetainedMaterialSurvivesDeviceLossAndUpdates(t *testing.T) {
	const size = 64
	var (
		activeDev *gpu.Device
		camBuf    *render3d.CameraBuffer
		defaults  *render3d.MaterialDefaults
		mesh      *render3d.GPUMesh
		mat       *render3d.GPUMaterial
		pipe      *gpu.Pipeline
		baseMat   render3d.Material
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				activeDev = dev
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				if defaults == nil {
					defaults, err = render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Label: "ret", Retained: true})
					if err != nil {
						t.Fatalf("NewMaterialDefaults: %v", err)
					}
				}
				if mesh == nil {
					mesh, err = render3d.UploadMesh(cx, dev, worldQuadMesh(math3d.Vec3{}, 1.5, 1.5), render3d.MeshUpload{Label: "quad", Retained: true})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				}
				if mat == nil {
					normTex, err := render3d.UploadTexture(cx, dev, solidNRGBA(200, 128, 220, 255), render3d.TextureUpload{Label: "norm", Retained: true})
					if err != nil {
						t.Fatalf("UploadTexture: %v", err)
					}
					baseMat = render3d.DefaultMaterial()
					baseMat.BaseColorFactor = [4]float32{0.9, 0.15, 0.15, 1}
					baseMat.MetallicFactor = 0
					baseMat.RoughnessFactor = 0.5
					baseMat.NormalTexture = normTex
					mat, err = render3d.NewGPUMaterial(cx, dev, defaults, baseMat, render3d.MaterialUpload{Label: "ret-mat", Retained: true})
					if err != nil {
						t.Fatalf("NewGPUMaterial: %v", err)
					}
				}
				pipe, err = render3d.NewMaterialPipeline(cx, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
					DepthFormat: gpu.FormatDepth24Plus,
				})
				if err != nil {
					t.Fatalf("NewMaterialPipeline: %v", err)
				}
				cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(45), 1, 0.1, 20)
				cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
				camBuf.WriteCamera(cam)
			},
			OnFrame: func(frame *gpu.Frame) {
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				mat.Bind(pass)
				mesh.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)
	before, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	if c := before.RGBAAt(size/2, size/2); c.R <= c.G+50 {
		t.Fatalf("before device loss center = %v, want red surface", c)
	}

	firstMat := mat
	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if firstMat.Valid() {
		t.Fatal("GPUMaterial still valid while device is lost")
	}

	app.RestoreGPUDevice("id:viewport")
	if mat != firstMat || !firstMat.Valid() || !defaults.Valid() {
		t.Fatalf("retained material not restored: same=%v matValid=%v defsValid=%v", mat == firstMat, firstMat.Valid(), defaults.Valid())
	}

	app.Advance(16 * time.Millisecond)
	afterRestore, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	if c := afterRestore.RGBAAt(size/2, size/2); c.R <= c.G+50 {
		t.Fatalf("after restore center = %v, want restored red surface", c)
	}

	// Updating the retained material using the restored device must work seamlessly.
	nextMat := baseMat
	nextMat.BaseColorFactor = [4]float32{0.1, 0.85, 0.25, 1}
	if err := mat.Update(activeDev, nextMat); err != nil {
		t.Fatalf("Update after restore: %v", err)
	}
	app.Advance(16 * time.Millisecond)
	afterUpdate, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after update: %v", err)
	}
	if c := afterUpdate.RGBAAt(size/2, size/2); c.G <= c.R+50 {
		t.Fatalf("after post-restore Update center = %v, want green surface", c)
	}
}
