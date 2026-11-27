package render3d_test

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// hdrPatchWGSL draws a solid linear HDR RGBA color supplied via uniform buffer into a clip-space rectangle, letting tests write arbitrary HDR values (> 1.0) into FormatRGBA16Float targets.
const hdrPatchWGSL = `
struct PatchColor {
	rgba: vec4<f32>,
}

@group(0) @binding(0) var<uniform> patch_color: PatchColor;

struct VertexIn {
	@location(0) pos: vec3<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> @builtin(position) vec4<f32> {
	return vec4<f32>(in.pos, 1.0);
}

@fragment
fn fs_main() -> @location(0) vec4<f32> {
	return patch_color.rgba;
}
`

func TestExposureFromEV(t *testing.T) {
	cases := []struct {
		ev   float32
		want float32
	}{
		{0, 1.0},
		{1, 2.0},
		{-1, 0.5},
		{3, 8.0},
		{-3, 0.125},
		{float32(math.NaN()), 1.0},
		{float32(math.Inf(-1)), float32(math.Exp2(-126))},
		{-200, float32(math.Exp2(-126))},
	}
	for _, tc := range cases {
		got := render3d.ExposureFromEV(tc.ev)
		if math.Abs(float64(got-tc.want)) > 1e-6 || got <= 0 {
			t.Errorf("ExposureFromEV(%v) = %v, want %v", tc.ev, got, tc.want)
		}
	}
	hi := render3d.ExposureFromEV(float32(math.Inf(1)))
	if math.IsInf(float64(hi), 0) || math.IsNaN(float64(hi)) || hi <= 0 {
		t.Errorf("ExposureFromEV(+Inf) = %v, want positive finite clamp", hi)
	}
}

func TestSRGBRoundTrip(t *testing.T) {
	// Edge & out-of-range clamping.
	if got := render3d.LinearToSRGBChannel(-0.5); got != 0 {
		t.Errorf("LinearToSRGBChannel(-0.5) = %v, want 0", got)
	}
	if got := render3d.LinearToSRGBChannel(float32(math.NaN())); got != 0 {
		t.Errorf("LinearToSRGBChannel(NaN) = %v, want 0", got)
	}
	if got := render3d.LinearToSRGBChannel(2.5); got != 1 {
		t.Errorf("LinearToSRGBChannel(2.5) = %v, want 1", got)
	}
	if got := render3d.SRGBToLinearChannel(-0.5); got != 0 {
		t.Errorf("SRGBToLinearChannel(-0.5) = %v, want 0", got)
	}
	if got := render3d.SRGBToLinearChannel(float32(math.NaN())); got != 0 {
		t.Errorf("SRGBToLinearChannel(NaN) = %v, want 0", got)
	}
	if got := render3d.SRGBToLinearChannel(2.5); got != 1 {
		t.Errorf("SRGBToLinearChannel(2.5) = %v, want 1", got)
	}

	// Known IEC 61966-2-1 reference values: linear 0.5 -> sRGB ~0.735357, linear 0.18 -> sRGB ~0.461356.
	if got := render3d.LinearToSRGBChannel(0.5); math.Abs(float64(got)-0.7353569) > 1e-5 {
		t.Errorf("LinearToSRGBChannel(0.5) = %v, want ~0.735357", got)
	}
	if got := render3d.LinearToSRGBChannel(0.18); math.Abs(float64(got)-0.4613561) > 1e-5 {
		t.Errorf("LinearToSRGBChannel(0.18) = %v, want ~0.461356", got)
	}
	// Linear segment below 0.0031308.
	if got := render3d.LinearToSRGBChannel(0.002); math.Abs(float64(got)-0.02584) > 1e-6 {
		t.Errorf("LinearToSRGBChannel(0.002) = %v, want 0.02584", got)
	}

	// Full sweep round-trip.
	for i := 0; i <= 1000; i++ {
		lin := float32(i) / 1000.0
		srgb := render3d.LinearToSRGBChannel(lin)
		back := render3d.SRGBToLinearChannel(srgb)
		if math.Abs(float64(back-lin)) > 1e-5 {
			t.Fatalf("round-trip failed at %v: srgb=%v back=%v", lin, srgb, back)
		}
	}

	v := math3d.Vec3{X: 0.002, Y: 0.18, Z: 0.5}
	roundVec := render3d.SRGBToLinear(render3d.LinearToSRGB(v))
	if !roundVec.ApproxEqual(v, 1e-5) {
		t.Errorf("Vec3 sRGB round-trip = %v, want %v", roundVec, v)
	}
}

func TestToneMapOperatorMetadataAndDefaults(t *testing.T) {
	wantNames := map[render3d.ToneMapOperator]string{
		render3d.ToneMapNone:     "none",
		render3d.ToneMapReinhard: "reinhard",
		render3d.ToneMapACES:     "aces",
		render3d.ToneMapNeutral:  "neutral",
		render3d.ToneMapAgX:      "agx",
	}
	for op, name := range wantNames {
		if !op.Valid() || op.String() != name {
			t.Errorf("operator %d: valid=%v name=%q, want true %q", op, op.Valid(), op.String(), name)
		}
	}
	bad := render3d.ToneMapOperator(99)
	if bad.Valid() || bad.String() != "unknown" {
		t.Errorf("invalid operator: valid=%v name=%q", bad.Valid(), bad.String())
	}

	def := render3d.DefaultToneMapOptions()
	if def.Operator != render3d.ToneMapACES || def.Exposure != 1.0 || def.Gamma != 0 || def.LinearOutput {
		t.Errorf("DefaultToneMapOptions = %+v", def)
	}

	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	norm := (render3d.ToneMapOptions{
		Operator: bad,
		Exposure: nan,
		Gamma:    inf,
	}).Normalized()
	if norm.Operator != render3d.ToneMapNone || norm.Exposure != 1.0 || norm.Gamma != 0 {
		t.Errorf("Normalized(bad) = %+v", norm)
	}
	for _, e := range []float32{-1, inf, float32(math.Inf(-1))} {
		if got := (render3d.ToneMapOptions{Exposure: e}).Normalized().Exposure; got != 1.0 {
			t.Errorf("Normalized(Exposure %v).Exposure = %v, want 1", e, got)
		}
	}
	if got := (render3d.ToneMapOptions{Exposure: 0}).Normalized().Exposure; got != 0 {
		t.Errorf("Normalized(Exposure 0).Exposure = %v, want 0 (black)", got)
	}
}

// TestApplyToneMapExposureZeroIsBlack checks that a fade to black ending at Exposure 0 ends black instead of flashing to full brightness, and that the darkest EV stays dark.
func TestApplyToneMapExposureZeroIsBlack(t *testing.T) {
	inputs := []math3d.Vec3{{X: 0.5, Y: 2, Z: 10}, {X: float32(math.Inf(1)), Y: 65504, Z: 1e30}}
	for op := render3d.ToneMapNone; op <= render3d.ToneMapAgX; op++ {
		for _, in := range inputs {
			if got := render3d.ApplyToneMap(in, render3d.ToneMapOptions{Operator: op, Exposure: 0}); got != (math3d.Vec3{}) {
				t.Errorf("%s: ApplyToneMap(%v, Exposure 0) = %v, want black", op, in, got)
			}
			dark := render3d.ApplyToneMap(math3d.Vec3{X: 0.5, Y: 2, Z: 10}, render3d.ToneMapOptions{Operator: op, Exposure: render3d.ExposureFromEV(-200)})
			if dark.X > 0.01 || dark.Y > 0.01 || dark.Z > 0.01 {
				t.Errorf("%s: ApplyToneMap at EV -200 = %v, want near black", op, dark)
			}
		}
	}
}

func TestApplyToneMapHDRCompressionAndCurves(t *testing.T) {
	ops := []render3d.ToneMapOperator{
		render3d.ToneMapNone,
		render3d.ToneMapReinhard,
		render3d.ToneMapACES,
		render3d.ToneMapNeutral,
		render3d.ToneMapAgX,
	}

	for _, op := range ops {
		t.Run(op.String(), func(t *testing.T) {
			opts := render3d.ToneMapOptions{Operator: op, Exposure: 1.0, LinearOutput: true}

			// Black stays black.
			if black := render3d.ApplyToneMap(math3d.Vec3Zero(), opts); black != math3d.Vec3Zero() {
				t.Errorf("black = %v, want (0,0,0)", black)
			}

			// Negative, NaN, and +Inf never produce NaN/Inf or out-of-[0,1] output.
			for _, v := range []math3d.Vec3{
				{X: -5, Y: -0.1, Z: 0},
				{X: float32(math.NaN()), Y: 0.5, Z: float32(math.Inf(-1))},
				{X: float32(math.Inf(1)), Y: 1000, Z: 0.2},
			} {
				out := render3d.ApplyToneMap(v, opts)
				for _, ch := range []float32{out.X, out.Y, out.Z} {
					if math.IsNaN(float64(ch)) || math.IsInf(float64(ch), 0) || ch < 0 || ch > 1 {
						t.Errorf("input %v -> out-of-range %v", v, out)
					}
				}
			}

			// Monotonic gray ramp including HDR values > 1.0.
			ramp := []float32{0.0, 0.05, 0.18, 0.5, 1.0, 2.0, 4.0, 8.0, 15.0}
			prev := float32(-1)
			for _, lum := range ramp {
				out := render3d.ApplyToneMap(math3d.Vec3{X: lum, Y: lum, Z: lum}, opts)
				if out.X < prev-1e-6 {
					t.Errorf("non-monotonic at lum=%v: %v < %v", lum, out.X, prev)
				}
				// Neutral gray should stay close to neutral gray (R == G == B).
				if math.Abs(float64(out.X-out.Y)) > 1e-3 || math.Abs(float64(out.Y-out.Z)) > 1e-3 {
					t.Errorf("neutral gray %v skewed to %v", lum, out)
				}
				prev = out.X
			}

			// All HDR operators compress > 1.0 smoothly without flat clipping at 1.0.
			if op != render3d.ToneMapNone {
				at1 := render3d.ApplyToneMap(math3d.Vec3{X: 1, Y: 1, Z: 1}, opts).X
				at4 := render3d.ApplyToneMap(math3d.Vec3{X: 4, Y: 4, Z: 4}, opts).X
				at12 := render3d.ApplyToneMap(math3d.Vec3{X: 12, Y: 12, Z: 12}, opts).X
				if !(at1 < at4 && at4 < at12 && at12 <= 1.0) {
					t.Errorf("expected smooth HDR roll-off: f(1)=%v f(4)=%v f(12)=%v", at1, at4, at12)
				}
			}
		})
	}

	// Analytical checks.
	reinhard := render3d.ApplyToneMap(math3d.Vec3{X: 4, Y: 2, Z: 0.5}, render3d.ToneMapOptions{
		Operator: render3d.ToneMapReinhard, Exposure: 1, LinearOutput: true,
	})
	if !reinhard.ApproxEqual(math3d.Vec3{X: 0.8, Y: 2.0 / 3.0, Z: 1.0 / 3.0}, 1e-5) {
		t.Errorf("Reinhard(4,2,0.5) = %v, want (0.8, 0.6667, 0.3333)", reinhard)
	}

	// AgX desaturates intense pure-red HDR highlights rather than clipping to flat (1, 0, 0).
	agxRed := render3d.ApplyToneMap(math3d.Vec3{X: 10, Y: 0, Z: 0}, render3d.ToneMapOptions{
		Operator: render3d.ToneMapAgX, Exposure: 1, LinearOutput: true,
	})
	if agxRed.Y <= 0.01 || agxRed.Z <= 0.01 || agxRed.X <= agxRed.Y {
		t.Errorf("AgX(10,0,0) = %v, want warm highlight desaturation (R > G,B > 0)", agxRed)
	}

	// Khronos PBR Neutral desaturates highlights toward white above 0.76 while keeping midtones crisp.
	neutHigh := render3d.ApplyToneMap(math3d.Vec3{X: 10, Y: 0.5, Z: 0.5}, render3d.ToneMapOptions{
		Operator: render3d.ToneMapNeutral, Exposure: 1, LinearOutput: true,
	})
	if neutHigh.Y <= 0.4 || neutHigh.X > 1.0 {
		t.Errorf("Neutral(10,0.5,0.5) = %v, want smooth highlight desaturation", neutHigh)
	}

	// Exposure +1 EV brightens, -1 EV darkens.
	mid := math3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3}
	lo := render3d.ApplyToneMap(mid, render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: render3d.ExposureFromEV(-1)}).X
	base := render3d.ApplyToneMap(mid, render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: render3d.ExposureFromEV(0)}).X
	hi := render3d.ApplyToneMap(mid, render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: render3d.ExposureFromEV(+1)}).X
	if !(lo < base && base < hi) {
		t.Errorf("exposure ordering broken: -1EV=%v 0EV=%v +1EV=%v", lo, base, hi)
	}

	// Power-law gamma vs exact sRGB.
	lin := render3d.ApplyToneMap(mid, render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: 1, LinearOutput: true})
	pow22 := render3d.ApplyToneMap(mid, render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: 1, Gamma: 2.2})
	wantPow := float32(math.Pow(float64(lin.X), 1.0/2.2))
	if math.Abs(float64(pow22.X-wantPow)) > 1e-5 {
		t.Errorf("Gamma 2.2 = %v, want %v", pow22.X, wantPow)
	}
}

func TestAppendToneMapUniformsLayoutAndZeroAlloc(t *testing.T) {
	prefix := []byte{0xDE, 0xAD}
	opts := render3d.ToneMapOptions{
		Operator: render3d.ToneMapAgX,
		Exposure: 2.5,
		Gamma:    2.2,
	}
	got := render3d.AppendToneMapUniforms(prefix, opts)
	if len(got) != len(prefix)+render3d.ToneMapUniformSize || render3d.ToneMapUniformSize != 48 {
		t.Fatalf("len = %d, ToneMapUniformSize = %d", len(got), render3d.ToneMapUniformSize)
	}
	if got[0] != 0xDE || got[1] != 0xAD {
		t.Fatal("prefix corrupted")
	}
	f32 := func(i int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(got[len(prefix)+4*i:]))
	}
	if f32(0) != 2.5 {
		t.Errorf("exposure = %v, want 2.5", f32(0))
	}
	if math.Abs(float64(f32(1)-float32(1.0/2.2))) > 1e-6 {
		t.Errorf("inv_gamma = %v, want %v", f32(1), float32(1.0/2.2))
	}
	if f32(2) != float32(render3d.ToneMapAgX) {
		t.Errorf("operator_id = %v, want %v", f32(2), float32(render3d.ToneMapAgX))
	}
	if f32(3) != 0 {
		t.Errorf("pad = %v, want 0", f32(3))
	}
	// The whole source texture: uv_scale (1, 1), uv_min (0, 0), uv_max (1, 1), then padding.
	for i, want := range []float32{1, 1, 0, 0, 1, 1, 0, 0} {
		if f32(4+i) != want {
			t.Errorf("region float %d = %v, want %v", i, f32(4+i), want)
		}
	}

	// Exact sRGB encodes inv_gamma = 0; LinearOutput encodes inv_gamma = -1.
	buf := make([]byte, 0, render3d.ToneMapUniformSize)
	srgbBytes := render3d.AppendToneMapUniforms(buf[:0], render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: 1})
	if v := math.Float32frombits(binary.LittleEndian.Uint32(srgbBytes[4:])); v != 0 {
		t.Errorf("sRGB inv_gamma = %v, want 0", v)
	}
	linBytes := render3d.AppendToneMapUniforms(buf[:0], render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: 1, LinearOutput: true})
	if v := math.Float32frombits(binary.LittleEndian.Uint32(linBytes[4:])); v != -1 {
		t.Errorf("LinearOutput inv_gamma = %v, want -1", v)
	}

	if allocs := testing.AllocsPerRun(100, func() {
		buf = render3d.AppendToneMapUniforms(buf[:0], opts)
	}); allocs != 0 {
		t.Errorf("AppendToneMapUniforms allocated %v times, want 0", allocs)
	}
}

func TestHDRTargetLifecycleAndValidation(t *testing.T) {
	if render3d.SupportsHDR(nil) {
		t.Error("SupportsHDR(nil) = true, want false")
	}
	if got := render3d.PreferredHDRFormat(nil); got != gpu.FormatRGBA8Unorm {
		t.Errorf("PreferredHDRFormat(nil) = %v, want FormatRGBA8Unorm", got)
	}

	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	if !render3d.SupportsHDR(dev) {
		t.Fatal("software renderer should support FormatRGBA16Float")
	}
	if got := render3d.PreferredHDRFormat(dev); got != gpu.FormatRGBA16Float {
		t.Errorf("PreferredHDRFormat = %v, want FormatRGBA16Float", got)
	}

	if _, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{Width: 0, Height: 16}); !errors.Is(err, render3d.ErrInvalidHDRTarget) {
		t.Errorf("zero width err = %v, want ErrInvalidHDRTarget", err)
	}
	if _, err := render3d.NewHDRTarget(nil, nil, render3d.HDRTargetDesc{Width: 16, Height: 16}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("nil dev err = %v, want ErrDeviceLost", err)
	}

	hdr, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{
		Label:       "test-hdr",
		Width:       32,
		Height:      16,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewHDRTarget: %v", err)
	}
	if !hdr.Valid() || !hdr.IsHDR() || hdr.Format() != gpu.FormatRGBA16Float {
		t.Fatalf("hdr valid=%v isHDR=%v fmt=%v", hdr.Valid(), hdr.IsHDR(), hdr.Format())
	}
	if hdr.Width() != 32 || hdr.Height() != 16 || hdr.DepthFormat() != gpu.FormatDepth24Plus || hdr.DepthTexture() == nil {
		t.Fatalf("hdr dimensions/depth mismatch: %dx%d depth=%v", hdr.Width(), hdr.Height(), hdr.DepthFormat())
	}
	wantBytes := gpu.FormatRGBA16Float.ByteSize(32, 16) + gpu.FormatDepth24Plus.ByteSize(32, 16)
	if hdr.ByteSize() != wantBytes {
		t.Errorf("ByteSize = %d, want %d", hdr.ByteSize(), wantBytes)
	}

	// No-op resize keeps the same color texture.
	origTex := hdr.ColorTexture()
	if err := hdr.Resize(dev, 32, 16); err != nil {
		t.Fatalf("no-op Resize: %v", err)
	}
	if hdr.ColorTexture() != origTex {
		t.Error("no-op Resize unexpectedly reallocated texture")
	}

	// Real resize updates dimensions and closes the old texture.
	if err := hdr.Resize(dev, 64, 32); err != nil {
		t.Fatalf("Resize(64,32): %v", err)
	}
	if hdr.Width() != 64 || hdr.Height() != 32 || hdr.ColorTexture() == origTex || origTex.Valid() {
		t.Error("Resize(64,32) did not replace old texture cleanly")
	}

	// Close is idempotent and nil-safe.
	if err := hdr.Close(); err != nil || hdr.Valid() {
		t.Errorf("Close: err=%v valid=%v", err, hdr.Valid())
	}
	if err := hdr.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	var nilTarget *render3d.HDRTarget
	if nilTarget.Valid() || nilTarget.IsHDR() || nilTarget.ByteSize() != 0 || nilTarget.Close() != nil {
		t.Error("nil HDRTarget methods misbehaved")
	}
}

func TestHDRTargetMSAAFallbackWhenUnsupported(t *testing.T) {
	// Disable MSAA4x via software feature flags; SampleCount: 4 must fall back to 1 cleanly.
	sr := gpu.NewSoftwareRendererWithFeatures(16, 16, gpu.FormatUndefined, gpu.Features{})
	t.Cleanup(sr.Close)
	hdr, err := render3d.NewHDRTarget(nil, sr.Device(), render3d.HDRTargetDesc{
		Width:       16,
		Height:      16,
		SampleCount: 4,
	})
	if err != nil {
		t.Fatalf("NewHDRTarget(MSAA fallback): %v", err)
	}
	defer func() { _ = hdr.Close() }()
	if hdr.SampleCount() != 1 || !hdr.Valid() {
		t.Errorf("SampleCount = %d valid=%v, want 1 true", hdr.SampleCount(), hdr.Valid())
	}
}

// hdrBand describes one horizontal column of our multi-band HDR calibration target.
type hdrBand struct {
	name string
	rgb  math3d.Vec3
}

var hdrCalibrationBands = []hdrBand{
	{"dark", math3d.Vec3{X: 0.002, Y: 0.002, Z: 0.002}},
	{"midgray", math3d.Vec3{X: 0.18, Y: 0.18, Z: 0.18}},
	{"white1", math3d.Vec3{X: 1.0, Y: 1.0, Z: 1.0}},
	{"hdr2", math3d.Vec3{X: 2.0, Y: 2.0, Z: 2.0}},
	{"hdr4", math3d.Vec3{X: 4.0, Y: 4.0, Z: 4.0}},
	{"hdr10", math3d.Vec3{X: 10.0, Y: 10.0, Z: 10.0}},
	{"emissive_red", math3d.Vec3{X: 8.0, Y: 0.5, Z: 0.2}},
	{"emissive_blue", math3d.Vec3{X: 0.3, Y: 0.4, Z: 12.0}},
}

// renderHDRBandsInto draws each band in hdrCalibrationBands as a vertical strip into hdr using linear HDR floats (including values > 1.0).
func renderHDRBandsInto(t *testing.T, dev *gpu.Device, hdr *render3d.HDRTarget, enc *gpu.Encoder) {
	t.Helper()
	renderBandsInto(t, dev, hdr, enc, hdrCalibrationBands)
}

// renderBandsInto draws each band as a vertical strip into hdr.
func renderBandsInto(t *testing.T, dev *gpu.Device, hdr *render3d.HDRTarget, enc *gpu.Encoder, bands []hdrBand) {
	t.Helper()
	mod, err := gpu.CompileWGSL(hdrPatchWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL hdrPatchWGSL: %v", err)
	}
	bgl, err := dev.CreateBindGroupLayout(nil, []gpu.BindGroupLayoutEntry{
		{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
	})
	if err != nil {
		t.Fatalf("CreateBindGroupLayout: %v", err)
	}
	pipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:    "hdr-patch",
		Vertex:   gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment: gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:  []*gpu.BindGroupLayout{bgl},
		VertexBuffers: []gpu.VertexBufferLayout{{
			Stride:     3 * 4,
			Attributes: []gpu.VertexAttribute{{Location: 0, Offset: 0, Format: gpu.VertexFloat32x3}},
		}},
		Targets: []gpu.ColorTargetState{{Format: hdr.Format()}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline hdr-patch: %v", err)
	}

	n := len(bands)
	pass := enc.BeginRenderPass(hdr.RenderPassDesc(core.RGB(0x000000), 1))
	pass.SetPipeline(pipe)
	for i, band := range bands {
		x0 := -1.0 + 2.0*float32(i)/float32(n)
		x1 := -1.0 + 2.0*float32(i+1)/float32(n)
		quad := packFloat32s([]float32{
			x0, -1, 0.5,
			x1, -1, 0.5,
			x1, 1, 0.5,
			x0, -1, 0.5,
			x1, 1, 0.5,
			x0, 1, 0.5,
		})
		vb, err := dev.CreateBuffer(nil, gpu.BufferDesc{Usage: gpu.UsageVertex, Data: quad})
		if err != nil {
			t.Fatalf("CreateBuffer vb %d: %v", i, err)
		}
		ub, err := dev.CreateBuffer(nil, gpu.BufferDesc{
			Usage: gpu.UsageUniform,
			Data:  packFloat32s([]float32{band.rgb.X, band.rgb.Y, band.rgb.Z, 1.0}),
		})
		if err != nil {
			t.Fatalf("CreateBuffer ub %d: %v", i, err)
		}
		bg, err := dev.CreateBindGroup(nil, bgl, []gpu.BindGroupEntry{{Binding: 0, Buffer: ub}})
		if err != nil {
			t.Fatalf("CreateBindGroup %d: %v", i, err)
		}
		pass.SetBindGroup(0, bg)
		pass.SetVertexBuffer(0, vb, 0)
		pass.Draw(6, 0)
	}
	pass.End()
}

func quantizeByte(f float32) uint8 {
	if !(f > 0) {
		return 0
	}
	if f >= 1 {
		return 255
	}
	return uint8(math.Round(float64(f) * 255.0))
}

func TestToneMapGPUMatchesCPUReference(t *testing.T) {
	const (
		bandWidth = 8
		width     = bandWidth * 8
		height    = 16
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	hdr, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{
		Label:  "hdr-calibration",
		Width:  width,
		Height: height,
	})
	if err != nil {
		t.Fatalf("NewHDRTarget: %v", err)
	}
	t.Cleanup(func() { _ = hdr.Close() })

	// Populate the HDR target once.
	initEnc := dev.BeginEncoder()
	renderHDRBandsInto(t, dev, hdr, initEnc)
	dev.Submit(initEnc.Finish())
	sr.WaitIdle()

	tonePass, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{
		Label:        "tonemap-test",
		TargetFormat: dev.SurfaceFormat(),
	})
	if err != nil {
		t.Fatalf("NewToneMapPass: %v", err)
	}
	t.Cleanup(func() { _ = tonePass.Close() })

	configs := []struct {
		name string
		opts render3d.ToneMapOptions
	}{
		{"none-srgb", render3d.ToneMapOptions{Operator: render3d.ToneMapNone, Exposure: 1.0}},
		{"reinhard-srgb", render3d.ToneMapOptions{Operator: render3d.ToneMapReinhard, Exposure: 1.0}},
		{"aces-srgb", render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: 1.0}},
		{"neutral-srgb", render3d.ToneMapOptions{Operator: render3d.ToneMapNeutral, Exposure: 1.0}},
		{"agx-srgb", render3d.ToneMapOptions{Operator: render3d.ToneMapAgX, Exposure: 1.0}},
		{"aces-ev+1", render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: render3d.ExposureFromEV(1)}},
		{"aces-ev-1", render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: render3d.ExposureFromEV(-1)}},
		{"reinhard-gamma22", render3d.ToneMapOptions{Operator: render3d.ToneMapReinhard, Exposure: 1.0, Gamma: 2.2}},
		{"agx-linear", render3d.ToneMapOptions{Operator: render3d.ToneMapAgX, Exposure: 1.0, LinearOutput: true}},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			enc := dev.BeginEncoder()
			pass := enc.BeginRenderPass(gpu.RenderPassDesc{
				Label:            tc.name,
				ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}},
			})
			if err := tonePass.DrawTarget(pass, dev, hdr, tc.opts); err != nil {
				t.Fatalf("DrawTarget: %v", err)
			}
			pass.End()
			dev.Submit(enc.Finish())
			sr.WaitIdle()
			if err := sr.LastError(); err != nil {
				t.Fatalf("software renderer: %v", err)
			}

			img := sr.Image()
			for i, band := range hdrCalibrationBands {
				cx := i*bandWidth + bandWidth/2
				got := img.RGBAAt(cx, height/2)
				wantVec := render3d.ApplyToneMap(band.rgb, tc.opts)
				wantR, wantG, wantB := quantizeByte(wantVec.X), quantizeByte(wantVec.Y), quantizeByte(wantVec.Z)
				// Allow ±2 LSB for float16 storage quantization and float32 shader rounding.
				if absDiffU8(got.R, wantR) > 2 || absDiffU8(got.G, wantG) > 2 || absDiffU8(got.B, wantB) > 2 || got.A != 255 {
					t.Errorf("band %s (%v): GPU=%v, want CPU~{%d %d %d 255}",
						band.name, band.rgb, got, wantR, wantG, wantB)
				}
			}

			// Verify HDR > 1.0 bands (hdr2 < hdr4 < hdr10) are distinct and unclipped for all tone mappers.
			if tc.opts.Operator != render3d.ToneMapNone && tc.opts.Exposure == 1.0 && !tc.opts.LinearOutput {
				p2 := img.RGBAAt(3*bandWidth+bandWidth/2, height/2).R
				p4 := img.RGBAAt(4*bandWidth+bandWidth/2, height/2).R
				p10 := img.RGBAAt(5*bandWidth+bandWidth/2, height/2).R
				if !(p2 < p4 && p4 <= p10 && p2 < 255) {
					t.Errorf("HDR bands did not compress smoothly: 2.0->%d 4.0->%d 10.0->%d", p2, p4, p10)
				}
			}
		})
	}
}

// TestToneMapGPUOverflowAndExposureZero writes +Inf, NaN and values beyond the half-float range into the HDR target and checks the GPU pass against ApplyToneMap, including extreme and zero exposure.
func TestToneMapGPUOverflowAndExposureZero(t *testing.T) {
	const (
		bandWidth = 8
		height    = 8
	)
	inf := float32(math.Inf(1))
	bands := []hdrBand{
		{"inf", math3d.Vec3{X: inf, Y: inf, Z: inf}},
		{"nan", math3d.Vec3{X: float32(math.NaN()), Y: 0.5, Z: float32(math.NaN())}},
		{"huge", math3d.Vec3{X: 1e30, Y: 1e30, Z: 1e30}},
		{"max-half", math3d.Vec3{X: 65504, Y: 65504, Z: 65504}},
		{"mixed", math3d.Vec3{X: inf, Y: 0.25, Z: 3}},
		{"mid", math3d.Vec3{X: 0.18, Y: 0.5, Z: 2}},
	}
	width := bandWidth * len(bands)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	hdr, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{Width: uint32(width), Height: height})
	if err != nil {
		t.Fatalf("NewHDRTarget: %v", err)
	}
	t.Cleanup(func() { _ = hdr.Close() })
	if !hdr.IsHDR() {
		t.Skip("software device has no RGBA16Float target")
	}
	enc := dev.BeginEncoder()
	renderBandsInto(t, dev, hdr, enc, bands)
	dev.Submit(enc.Finish())
	sr.WaitIdle()

	tonePass, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{TargetFormat: dev.SurfaceFormat(), NearestFilter: true})
	if err != nil {
		t.Fatalf("NewToneMapPass: %v", err)
	}
	t.Cleanup(func() { _ = tonePass.Close() })

	var configs []render3d.ToneMapOptions
	for op := render3d.ToneMapNone; op <= render3d.ToneMapAgX; op++ {
		configs = append(configs,
			render3d.ToneMapOptions{Operator: op, Exposure: 1},
			render3d.ToneMapOptions{Operator: op, Exposure: render3d.ExposureFromEV(126)},
			render3d.ToneMapOptions{Operator: op, Exposure: render3d.ExposureFromEV(-200)},
			render3d.ToneMapOptions{Operator: op, Exposure: 0},
		)
	}
	for _, opts := range configs {
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
		if err := tonePass.DrawTarget(pass, dev, hdr, opts); err != nil {
			t.Fatalf("DrawTarget: %v", err)
		}
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		img := sr.Image()
		for i, band := range bands {
			got := img.RGBAAt(i*bandWidth+bandWidth/2, height/2)
			// The RGBA16Float target stores values beyond the half-float range as +Inf.
			want := render3d.ApplyToneMap(math3d.Vec3{X: storedAsHalf(band.rgb.X), Y: storedAsHalf(band.rgb.Y), Z: storedAsHalf(band.rgb.Z)}, opts)
			wr, wg, wb := quantizeByte(want.X), quantizeByte(want.Y), quantizeByte(want.Z)
			if absDiffU8(got.R, wr) > 2 || absDiffU8(got.G, wg) > 2 || absDiffU8(got.B, wb) > 2 {
				t.Errorf("%s exposure %g band %s: GPU=%v, want CPU~{%d %d %d}", opts.Operator, opts.Exposure, band.name, got, wr, wg, wb)
			}
			if opts.Exposure == 0 && (got.R|got.G|got.B) != 0 {
				t.Errorf("%s exposure 0 band %s: GPU=%v, want black", opts.Operator, band.name, got)
			}
		}
	}
}

// storedAsHalf returns v as an RGBA16Float target keeps it for the overflow test: values at or beyond the rounding threshold above 65504 become +Inf.
func storedAsHalf(v float32) float32 {
	if v >= 65520 {
		return float32(math.Inf(1))
	}
	return v
}

func TestToneMapHardwareSRGBTargetAvoidsDoubleGamma(t *testing.T) {
	// Rendering the exact same HDR target into a FormatRGBA8UnormSRGB target (where ToneMapPass automatically emits linear RGB and hardware applies sRGB) must match rendering into FormatRGBA8Unorm (where ToneMapPass applies exact sRGB in the shader).
	const (
		bandWidth = 8
		width     = bandWidth * 8
		height    = 16
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	hdr, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewHDRTarget: %v", err)
	}
	t.Cleanup(func() { _ = hdr.Close() })

	srgbDst, err := dev.CreateTexture(nil, gpu.TextureDesc{
		Label:  "srgb-dst",
		Width:  width,
		Height: height,
		Format: gpu.FormatRGBA8UnormSRGB,
		Usage:  gpu.TextureRenderAttachment | gpu.TextureCopySrc,
	})
	if err != nil {
		t.Fatalf("CreateTexture sRGB: %v", err)
	}
	t.Cleanup(func() { _ = srgbDst.Close() })

	unormPass, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{
		TargetFormat: gpu.FormatRGBA8Unorm,
	})
	if err != nil {
		t.Fatalf("NewToneMapPass unorm: %v", err)
	}
	t.Cleanup(func() { _ = unormPass.Close() })

	srgbPass, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{
		TargetFormat: gpu.FormatRGBA8UnormSRGB,
	})
	if err != nil {
		t.Fatalf("NewToneMapPass srgb: %v", err)
	}
	t.Cleanup(func() { _ = srgbPass.Close() })

	opts := render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: 1.0}
	enc := dev.BeginEncoder()
	renderHDRBandsInto(t, dev, hdr, enc)

	p0 := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}},
	})
	if err := unormPass.DrawTarget(p0, dev, hdr, opts); err != nil {
		t.Fatalf("DrawTarget unorm: %v", err)
	}
	p0.End()

	p1 := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{Texture: srgbDst, LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}},
	})
	if err := srgbPass.DrawTarget(p1, dev, hdr, opts); err != nil {
		t.Fatalf("DrawTarget srgb: %v", err)
	}
	p1.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()

	unormImg := sr.Image()
	var srgbPixels []byte
	var readErr error
	dev.ReadTexture(srgbDst, gpu.TextureRegion{Width: width, Height: height}, func(rgba []byte, err error) {
		srgbPixels = append([]byte(nil), rgba...)
		readErr = err
	})
	sr.WaitIdle()
	if readErr != nil || len(srgbPixels) != width*height*4 {
		t.Fatalf("ReadTexture sRGB: len=%d err=%v", len(srgbPixels), readErr)
	}

	for i, band := range hdrCalibrationBands {
		cx := i*bandWidth + bandWidth/2
		cy := height / 2
		u := unormImg.RGBAAt(cx, cy)
		off := (cy*width + cx) * 4
		s := color.RGBA{R: srgbPixels[off], G: srgbPixels[off+1], B: srgbPixels[off+2], A: srgbPixels[off+3]}
		if absDiffU8(u.R, s.R) > 1 || absDiffU8(u.G, s.G) > 1 || absDiffU8(u.B, s.B) > 1 {
			t.Errorf("band %s: shader-sRGB=%v vs hardware-sRGB=%v (possible double gamma!)", band.name, u, s)
		}
	}
}

// toneMapViewApp mounts a 64x64 gpu.View that draws an HDR emissive color (4.0, 2.0, 0.5) into an HDRTarget and tone-maps it onto the swapchain.
func toneMapViewApp(t *testing.T, retained bool, optsPtr *render3d.ToneMapOptions, testOpts ...nsgotest.Option) (*nsgotest.App, **render3d.HDRTarget, **render3d.ToneMapPass) {
	t.Helper()
	const size = 64
	var (
		hdr      *render3d.HDRTarget
		tonePass *render3d.ToneMapPass
		patchMod *gpu.ShaderModule
		patchBGL *gpu.BindGroupLayout
		patchBG  *gpu.BindGroup
		patchVB  *gpu.Buffer
		patchUB  *gpu.Buffer
		patchP   *gpu.Pipeline
	)
	var err error
	patchMod, err = gpu.CompileWGSL(hdrPatchWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	fullQuad := packFloat32s([]float32{
		-1, -1, 0.5,
		1, -1, 0.5,
		1, 1, 0.5,
		-1, -1, 0.5,
		1, 1, 0.5,
		-1, 1, 0.5,
	})

	allOpts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, testOpts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID: "viewport",
			Style:  core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			OnReady: func(dev *gpu.Device) {
				if hdr == nil || !retained {
					hdr, err = render3d.NewHDRTarget(cx, dev, render3d.HDRTargetDesc{
						Label:    "view-hdr",
						Width:    size,
						Height:   size,
						Retained: retained,
					})
					if err != nil {
						t.Fatalf("NewHDRTarget: %v", err)
					}
				}
				if tonePass == nil || !retained {
					tonePass, err = render3d.NewToneMapPass(cx, dev, render3d.ToneMapPassDesc{
						Label:        "view-tonemap",
						TargetFormat: dev.SurfaceFormat(),
						Retained:     retained,
					})
					if err != nil {
						t.Fatalf("NewToneMapPass: %v", err)
					}
				} else {
					tonePass.SetDevice(dev)
				}
				// Scene emitter pipeline (rebuilt on OnReady or retained).
				if patchP == nil || !retained {
					patchVB, err = dev.CreateBuffer(cx, gpu.BufferDesc{
						Usage: gpu.UsageVertex, Data: fullQuad, Retained: retained,
					})
					if err != nil {
						t.Fatalf("CreateBuffer vb: %v", err)
					}
					patchUB, err = dev.CreateBuffer(cx, gpu.BufferDesc{
						Usage: gpu.UsageUniform, Data: packFloat32s([]float32{4.0, 2.0, 0.5, 1.0}), Retained: retained,
					})
					if err != nil {
						t.Fatalf("CreateBuffer ub: %v", err)
					}
					patchBGL, err = dev.CreateBindGroupLayout(cx, []gpu.BindGroupLayoutEntry{
						{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
					})
					if err != nil {
						t.Fatalf("CreateBindGroupLayout: %v", err)
					}
					patchBG, err = dev.CreateBindGroup(cx, patchBGL, []gpu.BindGroupEntry{
						{Binding: 0, Buffer: patchUB},
					})
					if err != nil {
						t.Fatalf("CreateBindGroup: %v", err)
					}
					patchP, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
						Vertex:   gpu.ShaderStage{Module: patchMod, EntryPoint: "vs_main"},
						Fragment: gpu.ShaderStage{Module: patchMod, EntryPoint: "fs_main"},
						Layouts:  []*gpu.BindGroupLayout{patchBGL},
						VertexBuffers: []gpu.VertexBufferLayout{{
							Stride:     3 * 4,
							Attributes: []gpu.VertexAttribute{{Location: 0, Offset: 0, Format: gpu.VertexFloat32x3}},
						}},
						Targets:  []gpu.ColorTargetState{{Format: hdr.Format()}},
						Retained: retained,
					})
					if err != nil {
						t.Fatalf("CreatePipeline patch: %v", err)
					}
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				p0 := frame.BeginRenderPass(hdr.RenderPassDesc(core.RGB(0x000000), 1))
				p0.SetPipeline(patchP)
				p0.SetBindGroup(0, patchBG)
				p0.SetVertexBuffer(0, patchVB, 0)
				p0.Draw(6, 0)
				p0.End()

				p1 := frame.BeginRenderPass(gpu.RenderPassDesc{Label: "tonemap-display"})
				p1.ClearColor(core.RGB(0x000000))
				if err := tonePass.DrawTarget(p1, frame.Device(), hdr, *optsPtr); err != nil {
					t.Errorf("DrawTarget: %v", err)
				}
				p1.End()
				frame.Submit()
			},
		})
	}, allOpts...)
	return app, &hdr, &tonePass
}

func TestToneMapDrawsInGPUViewProfiles(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	hdrColor := math3d.Vec3{X: 4.0, Y: 2.0, Z: 0.5}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			activeOpts := render3d.DefaultToneMapOptions()
			app, hdrp, passp := toneMapViewApp(t, false, &activeOpts, p.opts...)
			if !(*hdrp).IsHDR() || !(*passp).Valid() {
				t.Fatalf("expected live HDR target & tone map pass on profile %s", p.name)
			}
			for _, op := range []render3d.ToneMapOperator{
				render3d.ToneMapReinhard,
				render3d.ToneMapACES,
				render3d.ToneMapNeutral,
				render3d.ToneMapAgX,
			} {
				activeOpts = render3d.ToneMapOptions{Operator: op, Exposure: 1.0}
				app.Advance(16 * time.Millisecond)
				img, err := app.GPUReadPixels("id:viewport")
				if err != nil {
					t.Fatalf("GPUReadPixels(%s): %v", op, err)
				}
				got := img.RGBAAt(32, 32)
				wantVec := render3d.ApplyToneMap(hdrColor, activeOpts)
				want := color.RGBA{
					R: quantizeByte(wantVec.X),
					G: quantizeByte(wantVec.Y),
					B: quantizeByte(wantVec.Z),
					A: 255,
				}
				if absDiffU8(got.R, want.R) > 2 || absDiffU8(got.G, want.G) > 2 || absDiffU8(got.B, want.B) > 2 || got.A != 255 {
					t.Errorf("profile=%s op=%s: got=%v want~%v", p.name, op, got, want)
				}
			}
		})
	}
}

func TestRetainedHDRAndToneMapSurviveDeviceLoss(t *testing.T) {
	hdrColor := math3d.Vec3{X: 4.0, Y: 2.0, Z: 0.5}
	activeOpts := render3d.ToneMapOptions{Operator: render3d.ToneMapAgX, Exposure: 1.0}
	app, hdrp, passp := toneMapViewApp(t, true, &activeOpts)

	app.Advance(16 * time.Millisecond)
	firstHDR, firstPass := *hdrp, *passp
	img1, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	want1 := render3d.ApplyToneMap(hdrColor, activeOpts)
	p1 := img1.RGBAAt(32, 32)
	if absDiffU8(p1.R, quantizeByte(want1.X)) > 2 || absDiffU8(p1.G, quantizeByte(want1.Y)) > 2 || absDiffU8(p1.B, quantizeByte(want1.Z)) > 2 {
		t.Fatalf("before loss pixel = %v, want ~(%d,%d,%d)", p1, quantizeByte(want1.X), quantizeByte(want1.Y), quantizeByte(want1.Z))
	}

	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if firstHDR.Valid() || firstPass.Valid() {
		t.Fatalf("resources still valid during device loss: hdr=%v pass=%v", firstHDR.Valid(), firstPass.Valid())
	}

	app.RestoreGPUDevice("id:viewport")
	if *hdrp != firstHDR || *passp != firstPass || !firstHDR.Valid() || !firstPass.Valid() {
		t.Fatalf("retained resources not restored in place: hdrSame=%v hdrValid=%v passSame=%v passValid=%v",
			*hdrp == firstHDR, firstHDR.Valid(), *passp == firstPass, firstPass.Valid())
	}

	// Switch operator after restore to prove uniform writes also work on the restored device.
	activeOpts = render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: render3d.ExposureFromEV(0.5)}
	app.Advance(16 * time.Millisecond)
	img2, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	want2 := render3d.ApplyToneMap(hdrColor, activeOpts)
	p2 := img2.RGBAAt(32, 32)
	if absDiffU8(p2.R, quantizeByte(want2.X)) > 2 || absDiffU8(p2.G, quantizeByte(want2.Y)) > 2 || absDiffU8(p2.B, quantizeByte(want2.Z)) > 2 {
		t.Fatalf("after restore pixel = %v, want ~(%d,%d,%d)", p2, quantizeByte(want2.X), quantizeByte(want2.Y), quantizeByte(want2.Z))
	}
}

func TestToneMapScopeDisposalReleasesResources(t *testing.T) {
	var (
		hdr      *render3d.HDRTarget
		tonePass *render3d.ToneMapPass
		show     *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(16), Height: core.Pt(16)},
				OnReady: func(dev *gpu.Device) {
					var err error
					hdr, err = render3d.NewHDRTarget(cx, dev, render3d.HDRTargetDesc{Width: 16, Height: 16})
					if err != nil {
						t.Fatalf("NewHDRTarget: %v", err)
					}
					tonePass, err = render3d.NewToneMapPass(cx, dev, render3d.ToneMapPassDesc{})
					if err != nil {
						t.Fatalf("NewToneMapPass: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(16, 16), nsgotest.ManualFrames())

	if !hdr.Valid() || !tonePass.Valid() {
		t.Fatal("HDRTarget or ToneMapPass not valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if hdr.Valid() || tonePass.Valid() {
		t.Errorf("resources still valid after scope disposal: hdr=%v pass=%v", hdr.Valid(), tonePass.Valid())
	}
}

// renderHDRReferenceScene draws a 128x96 scene through render3d's own lit PBR pipeline in linear HDR mode (NewLitPBRPipelineWithOptions with LinearOutput) into an HDRTarget, with a bright key light, glossy specular highlights and emissive boxes well above 1.0, and resolves it through ToneMapPass with opts.
func renderHDRReferenceScene(t *testing.T, opts render3d.ToneMapOptions) *image.RGBA {
	t.Helper()
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0.4, Y: 3.0, Z: 6.2}, math3d.DegToRad(48), float32(refWidth)/float32(refHeight), 0.1, 50)
	cam.LookAt(math3d.Vec3{Y: 0.4, Z: 0}, math3d.Vec3Up())
	rig := render3d.LightRig{
		Ambient:     render3d.AmbientLight{Color: math3d.Vec3{X: 0.5, Y: 0.6, Z: 0.8}, Intensity: 0.25},
		Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{X: -0.55, Y: -0.8, Z: -0.35}, Color: math3d.Vec3{X: 1, Y: 0.95, Z: 0.88}, Intensity: 6}},
	}
	return renderLitSceneThroughToneMap(t, refWidth, refHeight, cam, rig, core.RGB(0x0a0e18), func(dev *gpu.Device) []litSceneObject {
		box := uploadTestMesh(t, dev, unitBoxMesh())
		sphere := uploadTestMesh(t, dev, uvSphereMesh(1, 24, 16))
		ground := uploadTestMesh(t, dev, lightGroundPlaneMesh(5))
		return []litSceneObject{
			{mesh: ground, model: math3d.Mat4Identity(), mat: render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.35, Y: 0.35, Z: 0.38}, Roughness: 0.8}},
			{mesh: box, model: math3d.Translate(math3d.Vec3{X: -1.6, Y: 0.5, Z: 1.2}), mat: render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.8, Y: 0.2, Z: 0.1}, Roughness: 0.5, Emissive: math3d.Vec3{X: 3.5, Y: 0.45, Z: 0.15}}},
			{mesh: sphere, model: math3d.Translate(math3d.Vec3{X: 0, Y: 0.8, Z: -0.6}).Mul(math3d.Scale(math3d.Vec3{X: 0.8, Y: 0.8, Z: 0.8})), mat: render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.95, Y: 0.95, Z: 0.95}, Roughness: 0.15}},
			{mesh: box, model: math3d.Translate(math3d.Vec3{X: 1.7, Y: 0.5, Z: 0.9}), mat: render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.1, Y: 0.3, Z: 0.8}, Roughness: 0.5, Emissive: math3d.Vec3{X: 0.2, Y: 0.7, Z: 2.8}}},
		}
	}, opts)
}

func TestToneMapReferenceScenes(t *testing.T) {
	refs := []struct {
		name string
		opts render3d.ToneMapOptions
	}{
		{"tonemap_aces", render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: 1.0}},
		{"tonemap_agx", render3d.ToneMapOptions{Operator: render3d.ToneMapAgX, Exposure: 1.0}},
		{"tonemap_neutral", render3d.ToneMapOptions{Operator: render3d.ToneMapNeutral, Exposure: 1.0}},
	}
	rendered := make(map[string]*image.RGBA, len(refs))
	for _, r := range refs {
		t.Run(r.name, func(t *testing.T) {
			img := renderHDRReferenceScene(t, r.opts)
			rendered[r.name] = img
			assertReference(t, r.name, img)
		})
	}
	// Confirm operators produce visually distinct renders on the HDR reference scene.
	if _, diff := gametest.DiffImage(rendered["tonemap_aces"], rendered["tonemap_agx"]); diff < 100 {
		t.Errorf("ACES and AgX reference renders differ in only %d pixels, expected visibly distinct curves", diff)
	}
	if _, diff := gametest.DiffImage(rendered["tonemap_aces"], rendered["tonemap_neutral"]); diff < 100 {
		t.Errorf("ACES and Neutral reference renders differ in only %d pixels, expected visibly distinct curves", diff)
	}
}

func FuzzApplyToneMapFiniteAndBounded(f *testing.F) {
	f.Add(float32(0), float32(0), float32(0), uint8(2), float32(1), float32(0), false)
	f.Add(float32(10), float32(0.5), float32(0.1), uint8(4), float32(2), float32(2.2), false)
	f.Add(float32(-1), float32(1e6), float32(0.18), uint8(3), float32(0), float32(-1), true)
	f.Fuzz(func(t *testing.T, r, g, b float32, op uint8, exp, gamma float32, linearOut bool) {
		opts := render3d.ToneMapOptions{
			Operator:     render3d.ToneMapOperator(op),
			Exposure:     exp,
			Gamma:        gamma,
			LinearOutput: linearOut,
		}
		out := render3d.ApplyToneMap(math3d.Vec3{X: r, Y: g, Z: b}, opts)
		for _, ch := range []float32{out.X, out.Y, out.Z} {
			if math.IsNaN(float64(ch)) || math.IsInf(float64(ch), 0) || ch < 0 || ch > 1 {
				t.Fatalf("ApplyToneMap((%v,%v,%v), %+v) = %v (out of [0,1])", r, g, b, opts, out)
			}
		}
	})
}

func BenchmarkAppendToneMapUniforms(b *testing.B) {
	opts := render3d.DefaultToneMapOptions()
	var buf [render3d.ToneMapUniformSize]byte
	b.SetBytes(render3d.ToneMapUniformSize)
	b.ReportAllocs()
	for b.Loop() {
		_ = render3d.AppendToneMapUniforms(buf[:0], opts)
	}
}

func BenchmarkApplyToneMap(b *testing.B) {
	hdr := math3d.Vec3{X: 4.2, Y: 1.8, Z: 0.35}
	for _, op := range []render3d.ToneMapOperator{
		render3d.ToneMapNone,
		render3d.ToneMapReinhard,
		render3d.ToneMapACES,
		render3d.ToneMapNeutral,
		render3d.ToneMapAgX,
	} {
		b.Run(op.String(), func(b *testing.B) {
			opts := render3d.ToneMapOptions{Operator: op, Exposure: 1.0}
			b.ReportAllocs()
			var sink math3d.Vec3
			for b.Loop() {
				sink = render3d.ApplyToneMap(hdr, opts)
			}
			_ = sink
		})
	}
}
