package render3d_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
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

var unlitClearColor = color.RGBA{R: 16, G: 20, B: 28, A: 255}

func unlitWantPixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, tol uint8) {
	t.Helper()
	got := img.RGBAAt(x, y)
	near := func(a, b uint8) bool { return max(a, b)-min(a, b) <= tol }
	if !near(got.R, want.R) || !near(got.G, want.G) || !near(got.B, want.B) || !near(got.A, want.A) {
		t.Errorf("pixel (%d, %d) = %v, want %v (tol %d)", x, y, got, want, tol)
	}
}

func unlitSRGBByteToLinear(b uint8) float32 {
	s := float64(b) / 255.0
	if s <= 0.04045 {
		return float32(s / 12.92)
	}
	return float32(math.Pow((s+0.055)/1.055, 2.4))
}

func unlitNRGBAToLinear(c color.NRGBA) [4]float32 {
	return [4]float32{
		unlitSRGBByteToLinear(c.R),
		unlitSRGBByteToLinear(c.G),
		unlitSRGBByteToLinear(c.B),
		float32(c.A) / 255.0,
	}
}

func unlitClipQuad(x0, y0, x1, y1 float32, colors []math3d.Vec4, reverseWinding bool) render3d.ColoredMesh {
	n := math3d.Vec3{Z: 1}
	indices := []uint32{0, 1, 2, 0, 2, 3}
	if reverseWinding {
		indices = []uint32{0, 2, 1, 0, 3, 2}
	}
	return render3d.ColoredMesh{
		Positions: []math3d.Vec3{
			{X: x0, Y: y0, Z: 0.5},
			{X: x1, Y: y0, Z: 0.5},
			{X: x1, Y: y1, Z: 0.5},
			{X: x0, Y: y1, Z: 0.5},
		},
		Normals: []math3d.Vec3{n, n, n, n},
		UVs: []math3d.Vec2{
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
		},
		Colors:  colors,
		Indices: indices,
	}
}

func unlitSolidNRGBA(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func unlitSplitAlphaImage(left, right color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, left)
	img.SetNRGBA(0, 1, left)
	img.SetNRGBA(1, 0, right)
	img.SetNRGBA(1, 1, right)
	return img
}

func unlitEncodePNGDataURI(t *testing.T, img image.Image) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

type unlitGLTFFixtureSpec struct {
	unlit           bool
	baseColorFactor [4]float32
	alphaMode       string
	alphaCutoff     *float32
	doubleSided     bool
	image           image.Image
	color0          []math3d.Vec4
	color0Vec3      bool
	reverseWinding  bool
	nearestSampler  bool
}

// unlitBuildGLTF constructs a valid self-contained glTF 2.0 JSON document with embedded data URIs for geometry and optional PNG baseColorTexture.
func unlitBuildGLTF(t *testing.T, spec unlitGLTFFixtureSpec) *gltf.Document {
	t.Helper()
	var bin bytes.Buffer
	appendBytes := func(b []byte) (offset, length int) {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		offset = bin.Len()
		bin.Write(b)
		return offset, len(b)
	}
	writeFloats := func(vals ...float32) []byte {
		b := make([]byte, len(vals)*4)
		for i, v := range vals {
			binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
		}
		return b
	}

	posBytes := writeFloats(
		-0.75, -0.75, 0.5,
		0.75, -0.75, 0.5,
		0.75, 0.75, 0.5,
		-0.75, 0.75, 0.5,
	)
	uvBytes := writeFloats(
		0, 1,
		1, 1,
		1, 0,
		0, 0,
	)
	idxVals := []uint16{0, 1, 2, 0, 2, 3}
	if spec.reverseWinding {
		idxVals = []uint16{0, 2, 1, 0, 3, 2}
	}
	idxBytes := make([]byte, len(idxVals)*2)
	for i, v := range idxVals {
		binary.LittleEndian.PutUint16(idxBytes[i*2:], v)
	}

	posOff, posLen := appendBytes(posBytes)
	uvOff, uvLen := appendBytes(uvBytes)
	idxOff, idxLen := appendBytes(idxBytes)

	bufferViews := []map[string]any{
		{"buffer": 0, "byteOffset": posOff, "byteLength": posLen},
		{"buffer": 0, "byteOffset": uvOff, "byteLength": uvLen},
		{"buffer": 0, "byteOffset": idxOff, "byteLength": idxLen},
	}
	accessors := []map[string]any{
		{
			"bufferView":    0,
			"componentType": 5126,
			"count":         4,
			"type":          "VEC3",
			"min":           []float32{-0.75, -0.75, 0.5},
			"max":           []float32{0.75, 0.75, 0.5},
		},
		{
			"bufferView":    1,
			"componentType": 5126,
			"count":         4,
			"type":          "VEC2",
		},
		{
			"bufferView":    2,
			"componentType": 5123,
			"count":         6,
			"type":          "SCALAR",
		},
	}
	attrs := map[string]any{
		"POSITION":   0,
		"TEXCOORD_0": 1,
	}

	if len(spec.color0) > 0 {
		var vals []float32
		accType := "VEC4"
		if spec.color0Vec3 {
			accType = "VEC3"
			for _, c := range spec.color0 {
				vals = append(vals, c.X, c.Y, c.Z)
			}
		} else {
			for _, c := range spec.color0 {
				vals = append(vals, c.X, c.Y, c.Z, c.W)
			}
		}
		colOff, colLen := appendBytes(writeFloats(vals...))
		bvIdx := len(bufferViews)
		bufferViews = append(bufferViews, map[string]any{"buffer": 0, "byteOffset": colOff, "byteLength": colLen})
		accIdx := len(accessors)
		accessors = append(accessors, map[string]any{
			"bufferView":    bvIdx,
			"componentType": 5126,
			"count":         len(spec.color0),
			"type":          accType,
		})
		attrs["COLOR_0"] = accIdx
	}

	pbr := map[string]any{
		"baseColorFactor": spec.baseColorFactor[:],
	}
	matObj := map[string]any{
		"name":                 "test_unlit",
		"pbrMetallicRoughness": pbr,
		"doubleSided":          spec.doubleSided,
	}
	if spec.alphaMode != "" {
		matObj["alphaMode"] = spec.alphaMode
	}
	if spec.alphaCutoff != nil {
		matObj["alphaCutoff"] = *spec.alphaCutoff
	}
	if spec.unlit {
		matObj["extensions"] = map[string]any{
			"KHR_materials_unlit": map[string]any{},
		}
	}

	root := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"buffers": []map[string]any{{
			"byteLength": bin.Len(),
			"uri":        "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin.Bytes()),
		}},
		"bufferViews": bufferViews,
		"accessors":   accessors,
		"materials":   []map[string]any{matObj},
		"meshes": []map[string]any{{
			"name": "quad",
			"primitives": []map[string]any{{
				"attributes": attrs,
				"indices":    2,
				"material":   0,
				"mode":       4,
			}},
		}},
		"nodes":  []map[string]any{{"mesh": 0}},
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"scene":  0,
	}
	if spec.unlit {
		root["extensionsUsed"] = []string{"KHR_materials_unlit"}
	}
	if spec.image != nil {
		pbr["baseColorTexture"] = map[string]any{"index": 0}
		mag, minF := 9729, 9729
		if spec.nearestSampler {
			mag, minF = 9728, 9728
		}
		root["images"] = []map[string]any{{
			"uri":      unlitEncodePNGDataURI(t, spec.image),
			"mimeType": "image/png",
		}}
		root["samplers"] = []map[string]any{{
			"magFilter": mag,
			"minFilter": minF,
			"wrapS":     33071,
			"wrapT":     33071,
		}}
		root["textures"] = []map[string]any{{
			"source":  0,
			"sampler": 0,
		}}
	}

	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatalf("json.Marshal glTF: %v", err)
	}
	doc, err := gltf.Load(raw, nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}
	return doc
}

func unlitDefaults(t testing.TB, cx *core.Scope, dev *gpu.Device, retained bool) *render3d.MaterialDefaults {
	t.Helper()
	defs, err := render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Label: "unlit-test", Retained: retained})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	return defs
}

func unlitIdentityCameraUniform() render3d.CameraUniform {
	id := math3d.Mat4Identity()
	return render3d.CameraUniform{
		View:     id,
		Proj:     id,
		ViewProj: id,
		Eye:      math3d.Vec4{Z: 1, W: 1},
	}
}

func unlitRenderClipSoftware(t *testing.T, size int, mesh render3d.ColoredMesh, mat render3d.UnlitMaterial, opts render3d.UnlitPipelineOptions) *image.RGBA {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	camBuf.Write(unlitIdentityCameraUniform())

	defs := unlitDefaults(t, nil, dev, false)
	gpuMat, err := render3d.NewGPUUnlitMaterial(nil, dev, defs, mat, render3d.MaterialUpload{Label: "test"})
	if err != nil {
		t.Fatalf("NewGPUUnlitMaterial: %v", err)
	}
	gpuMesh, err := render3d.UploadColoredMesh(nil, dev, mesh, render3d.MeshUpload{Label: "test"})
	if err != nil {
		t.Fatalf("UploadColoredMesh: %v", err)
	}

	opts.DepthFormat = gpu.FormatDepth24Plus
	pipe, err := render3d.NewUnlitPipeline(nil, dev, camBuf.Layout(), defs.UnlitLayout(), opts)
	if err != nil {
		t.Fatalf("NewUnlitPipeline: %v", err)
	}

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: core.RGB(0x10141c),
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1,
		},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	gpuMat.Bind(pass)
	gpuMesh.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return sr.Image()
}

func TestUnlitWGSLCompiles(t *testing.T) {
	mod, err := gpu.CompileWGSL(render3d.UnlitWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL(UnlitWGSL): %v", err)
	}
	if !mod.HasVertexEntry("vs_main") {
		t.Error("UnlitWGSL missing vertex entry point vs_main")
	}
	if !mod.HasFragmentEntry("fs_main") {
		t.Error("UnlitWGSL missing fragment entry point fs_main")
	}
	if !mod.HasFragmentEntry("fs_linear") {
		t.Error("UnlitWGSL missing fragment entry point fs_linear")
	}
}

func TestColoredMeshLayoutEncodingAndValidation(t *testing.T) {
	layout := render3d.ColoredVertexLayout()
	if layout.Stride != render3d.ColoredVertexStride {
		t.Fatalf("Stride = %d, want %d", layout.Stride, render3d.ColoredVertexStride)
	}
	if render3d.ColoredVertexStride != 48 {
		t.Fatalf("ColoredVertexStride = %d, want 48", render3d.ColoredVertexStride)
	}
	if layout.StepMode != gpu.StepVertex || len(layout.Attributes) != 4 {
		t.Fatalf("StepMode=%v attrs=%d", layout.StepMode, len(layout.Attributes))
	}
	wantAttrs := []gpu.VertexAttribute{
		{Format: gpu.VertexFloat32x3, Offset: 0, Location: 0},
		{Format: gpu.VertexFloat32x3, Offset: 12, Location: 1},
		{Format: gpu.VertexFloat32x2, Offset: 24, Location: 2},
		{Format: gpu.VertexFloat32x4, Offset: 32, Location: 3},
	}
	for i, w := range wantAttrs {
		if layout.Attributes[i] != w {
			t.Errorf("attr[%d] = %+v, want %+v", i, layout.Attributes[i], w)
		}
	}

	// Missing colors default to linear opaque white (1, 1, 1, 1).
	noColor := render3d.ColoredMesh{
		Positions: []math3d.Vec3{{X: 1, Y: 2, Z: 3}, {X: 4, Y: 5, Z: 6}, {X: 7, Y: 8, Z: 9}},
	}
	if err := noColor.Validate(); err != nil {
		t.Fatalf("noColor.Validate: %v", err)
	}
	if noColor.HasVertexColors() {
		t.Error("HasVertexColors = true, want false")
	}
	buf := noColor.AppendVertexData(nil)
	if len(buf) != 3*render3d.ColoredVertexStride {
		t.Fatalf("len(buf) = %d, want %d", len(buf), 3*render3d.ColoredVertexStride)
	}
	for vi := range 3 {
		off := vi*render3d.ColoredVertexStride + render3d.ColoredColorOffset
		for ch := range 4 {
			got := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+ch*4:]))
			if got != 1 {
				t.Errorf("vertex %d default color[%d] = %v, want 1", vi, ch, got)
			}
		}
	}

	// Explicit colors written verbatim; zero allocations when buffer has capacity.
	withColor := render3d.ColoredMesh{
		Positions: noColor.Positions,
		Colors: []math3d.Vec4{
			{X: 0.25, Y: 0.5, Z: 0.75, W: 0.9},
			{X: 1, Y: 0, Z: 0, W: 1},
			{X: 0, Y: 1, Z: 0, W: 0.5},
		},
		Indices: []uint32{0, 1, 2},
	}
	if err := withColor.Validate(); err != nil {
		t.Fatalf("withColor.Validate: %v", err)
	}
	if !withColor.HasVertexColors() || !withColor.Indexed() || withColor.VertexCount() != 3 || withColor.IndexFormat() != gpu.IndexUint16 {
		t.Errorf("unexpected metadata on withColor")
	}
	scratch := make([]byte, 0, withColor.VertexDataSize())
	if allocs := testing.AllocsPerRun(100, func() {
		scratch = withColor.AppendVertexData(scratch[:0])
	}); allocs != 0 {
		t.Errorf("AppendVertexData allocs = %v, want 0", allocs)
	}
	gotC0 := [4]float32{
		math.Float32frombits(binary.LittleEndian.Uint32(scratch[32:36])),
		math.Float32frombits(binary.LittleEndian.Uint32(scratch[36:40])),
		math.Float32frombits(binary.LittleEndian.Uint32(scratch[40:44])),
		math.Float32frombits(binary.LittleEndian.Uint32(scratch[44:48])),
	}
	if gotC0 != [4]float32{0.25, 0.5, 0.75, 0.9} {
		t.Errorf("vertex 0 color = %v, want [0.25 0.5 0.75 0.9]", gotC0)
	}

	// Validation rejections must wrap ErrInvalidMesh.
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	invalidCases := []struct {
		name string
		mesh render3d.ColoredMesh
	}{
		{"empty", render3d.ColoredMesh{}},
		{"color count mismatch", render3d.ColoredMesh{Positions: noColor.Positions, Colors: []math3d.Vec4{{X: 1, Y: 1, Z: 1, W: 1}}}},
		{"nan color", render3d.ColoredMesh{Positions: noColor.Positions, Colors: []math3d.Vec4{{X: nan, Y: 1, Z: 1, W: 1}, {W: 1}, {W: 1}}}},
		{"inf color alpha", render3d.ColoredMesh{Positions: noColor.Positions, Colors: []math3d.Vec4{{W: inf}, {W: 1}, {W: 1}}}},
		{"out of range index", render3d.ColoredMesh{Positions: noColor.Positions, Indices: []uint32{0, 1, 9}}},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mesh.Validate(); !errors.Is(err, render3d.ErrInvalidMesh) {
				t.Fatalf("Validate = %v, want ErrInvalidMesh", err)
			}
		})
	}
}

func TestUnlitGLTFUnlitAndColor0Fixtures(t *testing.T) {
	t.Run("opaque VEC3 COLOR_0 and baseColorFactor", func(t *testing.T) {
		vcol := math3d.Vec4{X: 0.5, Y: 0.8, Z: 0.25, W: 1}
		factor := [4]float32{0.8, 0.5, 1.0, 1.0}
		doc := unlitBuildGLTF(t, unlitGLTFFixtureSpec{
			unlit:           true,
			baseColorFactor: factor,
			color0:          []math3d.Vec4{vcol, vcol, vcol, vcol},
			color0Vec3:      true,
		})

		cm, err := render3d.ColoredMeshFromGLTF(doc, 0, 0)
		if err != nil {
			t.Fatalf("ColoredMeshFromGLTF: %v", err)
		}
		if !cm.HasVertexColors() || cm.Colors[0].W != 1 {
			t.Fatalf("VEC3 COLOR_0 did not expand alpha to 1: %+v", cm.Colors)
		}
		mat, err := render3d.UnlitMaterialFromGLTF(doc, 0, nil, nil)
		if err != nil {
			t.Fatalf("UnlitMaterialFromGLTF: %v", err)
		}

		img := unlitRenderClipSoftware(t, 32, cm, mat, render3d.UnlitPipelineOptions{
			AlphaMode:   mat.AlphaMode,
			DoubleSided: mat.DoubleSided,
		})
		want := render3d.EvaluateUnlit(mat, [4]float32{1, 1, 1, 1}, [4]float32{vcol.X, vcol.Y, vcol.Z, 1}).RGBA8()
		unlitWantPixel(t, img, 16, 16, want, 2)
		unlitWantPixel(t, img, 1, 1, unlitClearColor, 1)
	})

	t.Run("textured sRGB and VEC4 COLOR_0 multiply", func(t *testing.T) {
		// sRGB (188, 128, 64, 255) decodes to linear (~0.503, ~0.216, ~0.051, 1), multiplied by factor and vertex color in linear light, then encoded back to sRGB.
		texColor := color.NRGBA{R: 188, G: 128, B: 64, A: 255}
		vcol := math3d.Vec4{X: 0.6, Y: 0.9, Z: 0.5, W: 0.8}
		factor := [4]float32{0.9, 0.7, 1.0, 0.5}
		doc := unlitBuildGLTF(t, unlitGLTFFixtureSpec{
			unlit:           true,
			baseColorFactor: factor,
			image:           unlitSolidNRGBA(2, 2, texColor),
			color0:          []math3d.Vec4{vcol, vcol, vcol, vcol},
			nearestSampler:  true,
		})

		sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
		defer sr.Close()
		dev := sr.Device()

		camBuf, err := render3d.NewCameraBuffer(nil, dev)
		if err != nil {
			t.Fatalf("NewCameraBuffer: %v", err)
		}
		camBuf.Write(unlitIdentityCameraUniform())
		defs := unlitDefaults(t, nil, dev, false)
		gpuMat, err := render3d.UploadGLTFUnlitMaterial(nil, dev, defs, doc, 0, render3d.GLTFMaterialUpload{})
		if err != nil {
			t.Fatalf("UploadGLTFUnlitMaterial: %v", err)
		}
		cm, err := render3d.ColoredMeshFromGLTF(doc, 0, 0)
		if err != nil {
			t.Fatalf("ColoredMeshFromGLTF: %v", err)
		}
		gpuMesh, err := render3d.UploadColoredMesh(nil, dev, cm, render3d.MeshUpload{})
		if err != nil {
			t.Fatalf("UploadColoredMesh: %v", err)
		}
		pipe, err := render3d.NewUnlitPipeline(nil, dev, camBuf.Layout(), defs.UnlitLayout(), render3d.UnlitPipelineOptions{
			AlphaMode:   gpuMat.Material().AlphaMode,
			DoubleSided: gpuMat.Material().DoubleSided,
			DepthFormat: gpu.FormatDepth24Plus,
		})
		if err != nil {
			t.Fatalf("NewUnlitPipeline: %v", err)
		}
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		gpuMat.Bind(pass)
		gpuMesh.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()

		want := render3d.EvaluateUnlit(gpuMat.Material(), unlitNRGBAToLinear(texColor), [4]float32{vcol.X, vcol.Y, vcol.Z, vcol.W}).RGBA8()
		// AlphaMode is opaque, so output alpha must be 255 even though factor.a * vcol.a = 0.4.
		if want.A != 255 {
			t.Fatalf("opaque CPU reference alpha = %d, want 255", want.A)
		}
		unlitWantPixel(t, sr.Image(), 16, 16, want, 2)
	})

	t.Run("missing COLOR_0 defaults to white and UseVertexColor toggle", func(t *testing.T) {
		factor := [4]float32{0.25, 0.5, 0.75, 1.0}
		doc := unlitBuildGLTF(t, unlitGLTFFixtureSpec{
			unlit:           true,
			baseColorFactor: factor,
		})
		cm, err := render3d.ColoredMeshFromGLTF(doc, 0, 0)
		if err != nil {
			t.Fatalf("ColoredMeshFromGLTF: %v", err)
		}
		if cm.HasVertexColors() {
			t.Fatal("expected HasVertexColors == false when COLOR_0 omitted")
		}
		mat, err := render3d.UnlitMaterialFromGLTF(doc, 0, nil, nil)
		if err != nil {
			t.Fatalf("UnlitMaterialFromGLTF: %v", err)
		}
		img := unlitRenderClipSoftware(t, 32, cm, mat, render3d.UnlitPipelineOptions{})
		want := render3d.EvaluateUnlit(mat, [4]float32{1, 1, 1, 1}, [4]float32{1, 1, 1, 1}).RGBA8()
		unlitWantPixel(t, img, 16, 16, want, 2)

		// Now attach dark red vertex colors but set UseVertexColor=false: output must ignore vertex colors.
		red := math3d.Vec4{X: 0.2, Y: 0, Z: 0, W: 1}
		cmWithColors := unlitClipQuad(-0.75, -0.75, 0.75, 0.75, []math3d.Vec4{red, red, red, red}, false)
		matNoVC := mat
		matNoVC.UseVertexColor = false
		imgNoVC := unlitRenderClipSoftware(t, 32, cmWithColors, matNoVC, render3d.UnlitPipelineOptions{})
		wantNoVC := render3d.EvaluateUnlit(matNoVC, [4]float32{1, 1, 1, 1}, [4]float32{0.2, 0, 0, 1}).RGBA8()
		unlitWantPixel(t, imgNoVC, 16, 16, wantNoVC, 2)
	})

	t.Run("alpha mask discard and survive", func(t *testing.T) {
		cutoff := float32(0.5)
		leftTex := color.NRGBA{R: 255, G: 0, B: 0, A: 64}    // alpha ~0.25 < 0.5 -> discarded
		rightTex := color.NRGBA{R: 0, G: 220, B: 80, A: 200} // alpha ~0.78 >= 0.5 -> opaque output
		doc := unlitBuildGLTF(t, unlitGLTFFixtureSpec{
			unlit:           true,
			baseColorFactor: [4]float32{1, 1, 1, 1},
			alphaMode:       "MASK",
			alphaCutoff:     &cutoff,
			image:           unlitSplitAlphaImage(leftTex, rightTex),
			nearestSampler:  true,
		})

		sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
		defer sr.Close()
		dev := sr.Device()

		camBuf, _ := render3d.NewCameraBuffer(nil, dev)
		camBuf.Write(unlitIdentityCameraUniform())
		defs := unlitDefaults(t, nil, dev, false)
		gpuMat, err := render3d.UploadGLTFUnlitMaterial(nil, dev, defs, doc, 0, render3d.GLTFMaterialUpload{})
		if err != nil {
			t.Fatalf("UploadGLTFUnlitMaterial: %v", err)
		}
		if gpuMat.Material().AlphaMode != render3d.AlphaMask || gpuMat.Material().AlphaCutoff != 0.5 {
			t.Fatalf("unexpected mask material: %+v", gpuMat.Material())
		}
		cm, err := render3d.ColoredMeshFromGLTF(doc, 0, 0)
		if err != nil {
			t.Fatalf("ColoredMeshFromGLTF: %v", err)
		}
		gpuMesh, _ := render3d.UploadColoredMesh(nil, dev, cm, render3d.MeshUpload{})
		pipe, err := render3d.NewUnlitPipeline(nil, dev, camBuf.Layout(), defs.UnlitLayout(), render3d.UnlitPipelineOptions{
			AlphaMode:   render3d.AlphaMask,
			DepthFormat: gpu.FormatDepth24Plus,
		})
		if err != nil {
			t.Fatalf("NewUnlitPipeline: %v", err)
		}

		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		gpuMat.Bind(pass)
		gpuMesh.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()

		img := sr.Image()
		leftSample := render3d.EvaluateUnlit(gpuMat.Material(), unlitNRGBAToLinear(leftTex), [4]float32{1, 1, 1, 1})
		if !leftSample.Discarded {
			t.Fatal("expected left half to be discarded by EvaluateUnlit")
		}
		unlitWantPixel(t, img, 10, 16, unlitClearColor, 1)

		rightSample := render3d.EvaluateUnlit(gpuMat.Material(), unlitNRGBAToLinear(rightTex), [4]float32{1, 1, 1, 1})
		if rightSample.Discarded || rightSample.RGBA8().A != 255 {
			t.Fatalf("expected right half to survive with alpha 255: %+v", rightSample)
		}
		unlitWantPixel(t, img, 22, 16, rightSample.RGBA8(), 2)
	})

	t.Run("alpha blend composites over clear color", func(t *testing.T) {
		factor := [4]float32{1.0, 0.25, 0.0, 0.5}
		doc := unlitBuildGLTF(t, unlitGLTFFixtureSpec{
			unlit:           true,
			baseColorFactor: factor,
			alphaMode:       "BLEND",
		})
		cm, err := render3d.ColoredMeshFromGLTF(doc, 0, 0)
		if err != nil {
			t.Fatalf("ColoredMeshFromGLTF: %v", err)
		}
		mat, err := render3d.UnlitMaterialFromGLTF(doc, 0, nil, nil)
		if err != nil {
			t.Fatalf("UnlitMaterialFromGLTF: %v", err)
		}
		img := unlitRenderClipSoftware(t, 32, cm, mat, render3d.UnlitPipelineOptions{
			AlphaMode: render3d.AlphaBlend,
		})
		want := render3d.EvaluateUnlit(mat, [4]float32{1, 1, 1, 1}, [4]float32{1, 1, 1, 1}).BlendOver(unlitClearColor)
		unlitWantPixel(t, img, 16, 16, want, 3)
	})

	t.Run("double sided controls back-face culling", func(t *testing.T) {
		factor := [4]float32{0.2, 0.9, 0.4, 1.0}
		singleDoc := unlitBuildGLTF(t, unlitGLTFFixtureSpec{
			unlit:           true,
			baseColorFactor: factor,
			doubleSided:     false,
			reverseWinding:  true,
		})
		cm, err := render3d.ColoredMeshFromGLTF(singleDoc, 0, 0)
		if err != nil {
			t.Fatalf("ColoredMeshFromGLTF: %v", err)
		}
		singleMat, _ := render3d.UnlitMaterialFromGLTF(singleDoc, 0, nil, nil)
		imgSingle := unlitRenderClipSoftware(t, 32, cm, singleMat, render3d.UnlitPipelineOptions{DoubleSided: singleMat.DoubleSided})
		unlitWantPixel(t, imgSingle, 16, 16, unlitClearColor, 1)

		doubleDoc := unlitBuildGLTF(t, unlitGLTFFixtureSpec{
			unlit:           true,
			baseColorFactor: factor,
			doubleSided:     true,
			reverseWinding:  true,
		})
		doubleMat, _ := render3d.UnlitMaterialFromGLTF(doubleDoc, 0, nil, nil)
		imgDouble := unlitRenderClipSoftware(t, 32, cm, doubleMat, render3d.UnlitPipelineOptions{DoubleSided: doubleMat.DoubleSided})
		want := render3d.EvaluateUnlit(doubleMat, [4]float32{1, 1, 1, 1}, [4]float32{1, 1, 1, 1}).RGBA8()
		unlitWantPixel(t, imgDouble, 16, 16, want, 2)
	})

	t.Run("rejects a baseColorTexture without a core image source", func(t *testing.T) {
		doc := unlitBuildGLTF(t, unlitGLTFFixtureSpec{
			unlit:           true,
			baseColorFactor: [4]float32{1, 1, 1, 1},
			image:           unlitSolidNRGBA(2, 2, color.NRGBA{R: 255, A: 255}),
		})
		// A texture whose image comes only from an extension such as KHR_texture_basisu has no core source.
		doc.Textures[0].Source = -1
		sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
		defer sr.Close()
		dev := sr.Device()
		defs := unlitDefaults(t, nil, dev, false)
		if _, err := render3d.UploadGLTFUnlitMaterial(nil, dev, defs, doc, 0, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidUnlitMaterial) {
			t.Fatalf("UploadGLTFUnlitMaterial without an image source = %v, want ErrInvalidUnlitMaterial", err)
		}
	})

	t.Run("rejects non-unlit glTF material", func(t *testing.T) {
		doc := unlitBuildGLTF(t, unlitGLTFFixtureSpec{
			unlit:           false,
			baseColorFactor: [4]float32{1, 1, 1, 1},
		})
		if _, err := render3d.UnlitMaterialFromGLTF(doc, 0, nil, nil); !errors.Is(err, render3d.ErrInvalidUnlitMaterial) {
			t.Fatalf("UnlitMaterialFromGLTF on PBR material = %v, want ErrInvalidUnlitMaterial", err)
		}
	})
}

func TestUnlitKhronosGLTFTestdataSamples(t *testing.T) {
	// 1. Khronos TextureTransformMultiTest.gltf contains KHR_materials_unlit materials + KHR_texture_transform UVs.
	ttDir := filepath.Join("..", "gltf", "testdata", "khronos", "TextureTransformMultiTest", "glTF")
	ttDoc, err := gltf.LoadFS(os.DirFS(ttDir), "TextureTransformMultiTest.gltf")
	if err != nil {
		t.Fatalf("LoadFS TextureTransformMultiTest: %v", err)
	}

	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()
	defs := unlitDefaults(t, nil, dev, false)

	unlitCount := 0
	for i, m := range ttDoc.Materials {
		if !m.Unlit {
			if _, err := render3d.UnlitMaterialFromGLTF(ttDoc, i, nil, nil); !errors.Is(err, render3d.ErrInvalidUnlitMaterial) {
				t.Errorf("material %d (%s) non-unlit err = %v, want ErrInvalidUnlitMaterial", i, m.Name, err)
			}
			continue
		}
		unlitCount++
		um, err := render3d.UnlitMaterialFromGLTF(ttDoc, i, nil, nil)
		if err != nil {
			t.Fatalf("UnlitMaterialFromGLTF(%d %s): %v", i, m.Name, err)
		}
		gpuMat, err := render3d.NewGPUUnlitMaterial(nil, dev, defs, um, render3d.MaterialUpload{Label: m.Name})
		if err != nil {
			t.Fatalf("NewGPUUnlitMaterial(%d %s): %v", i, m.Name, err)
		}
		if !gpuMat.Valid() {
			t.Errorf("uploaded Khronos unlit material %d (%s) not valid", i, m.Name)
		}
		_ = gpuMat.Close()
		// TestMap.png is intentionally omitted from the testdata checkout; UploadGLTFUnlitMaterial must surface an error wrapping ErrInvalidUnlitMaterial rather than panicking.
		if m.BaseColorTexture != nil {
			if _, err := render3d.UploadGLTFUnlitMaterial(nil, dev, defs, ttDoc, i, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidUnlitMaterial) {
				t.Errorf("UploadGLTFUnlitMaterial missing external PNG (%s) = %v, want ErrInvalidUnlitMaterial", m.Name, err)
			}
		}
	}
	if unlitCount < 3 {
		t.Fatalf("expected at least 3 KHR_materials_unlit materials in TextureTransformMultiTest, got %d", unlitCount)
	}

	// Verify ColoredMeshFromGLTF extracts transformed UVs for UnlitUV0/UnlitUV1 meshes.
	for mi, mesh := range ttDoc.Meshes {
		for pi, prim := range mesh.Primitives {
			if prim.Material >= 0 && ttDoc.Materials[prim.Material].Unlit {
				cm, err := render3d.ColoredMeshFromGLTF(ttDoc, mi, pi)
				if err != nil {
					t.Fatalf("ColoredMeshFromGLTF(%s prim %d): %v", mesh.Name, pi, err)
				}
				if len(cm.UVs) != len(cm.Positions) {
					t.Errorf("mesh %s prim %d: UVs=%d positions=%d", mesh.Name, pi, len(cm.UVs), len(cm.Positions))
				}
			}
		}
	}

	// 2. Khronos MeshoptCubeTest.gltf contains COLOR_0 vertex colors.
	mcDir := filepath.Join("..", "gltf", "testdata", "khronos", "MeshoptCubeTest", "glTF")
	mcDoc, err := gltf.LoadFS(os.DirFS(mcDir), "MeshoptCubeTest.gltf")
	if err != nil {
		t.Fatalf("LoadFS MeshoptCubeTest: %v", err)
	}
	coloredPrims := 0
	for mi, mesh := range mcDoc.Meshes {
		for pi := range mesh.Primitives {
			cm, err := render3d.ColoredMeshFromGLTF(mcDoc, mi, pi)
			if err != nil {
				t.Fatalf("ColoredMeshFromGLTF(MeshoptCubeTest %d/%d): %v", mi, pi, err)
			}
			if cm.HasVertexColors() {
				coloredPrims++
			}
		}
	}
	if coloredPrims == 0 {
		t.Fatal("expected MeshoptCubeTest to contain COLOR_0 primitives")
	}
}

func unlitColoredCubeMesh(center math3d.Vec3, half float32, faceColors [6]math3d.Vec4) render3d.ColoredMesh {
	type face struct{ n, u, v math3d.Vec3 }
	faces := [...]face{
		{math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{X: -1}, math3d.Vec3{Z: 1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{Y: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}},
		{math3d.Vec3{Y: -1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: 1}},
		{math3d.Vec3{Z: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{Z: -1}, math3d.Vec3{X: -1}, math3d.Vec3{Y: 1}},
	}
	var m render3d.ColoredMesh
	for fi, f := range faces {
		base := uint32(len(m.Positions))
		c := center.Add(f.n.Scale(half))
		u, v := f.u.Scale(half), f.v.Scale(half)
		m.Positions = append(m.Positions,
			c.Sub(u).Sub(v),
			c.Add(u).Sub(v),
			c.Add(u).Add(v),
			c.Sub(u).Add(v),
		)
		m.Normals = append(m.Normals, f.n, f.n, f.n, f.n)
		m.UVs = append(m.UVs,
			math3d.Vec2{X: 0, Y: 1},
			math3d.Vec2{X: 1, Y: 1},
			math3d.Vec2{X: 1, Y: 0},
			math3d.Vec2{X: 0, Y: 0},
		)
		col := faceColors[fi]
		m.Colors = append(m.Colors, col, col, col, col)
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return m
}

func TestUnlitSceneMatchesReference(t *testing.T) {
	const (
		width  = 128
		height = 96
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defs := unlitDefaults(t, nil, dev, false)

	pipeOpaque, err := render3d.NewUnlitPipeline(nil, dev, camBuf.Layout(), defs.UnlitLayout(), render3d.UnlitPipelineOptions{
		AlphaMode:   render3d.AlphaOpaque,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewUnlitPipeline opaque: %v", err)
	}
	pipeMaskDS, err := render3d.NewUnlitPipeline(nil, dev, camBuf.Layout(), defs.UnlitLayout(), render3d.UnlitPipelineOptions{
		AlphaMode:   render3d.AlphaMask,
		DoubleSided: true,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewUnlitPipeline mask: %v", err)
	}
	pipeBlend, err := render3d.NewUnlitPipeline(nil, dev, camBuf.Layout(), defs.UnlitLayout(), render3d.UnlitPipelineOptions{
		AlphaMode:   render3d.AlphaBlend,
		DoubleSided: true,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewUnlitPipeline blend: %v", err)
	}

	// 1. Left cube: pure vertex colors (untextured fallback 1x1 white sRGB).
	leftFaceColors := [6]math3d.Vec4{
		{X: 0.9, Y: 0.2, Z: 0.15, W: 1},  // +X
		{X: 0.2, Y: 0.7, Z: 0.9, W: 1},   // -X
		{X: 0.95, Y: 0.8, Z: 0.2, W: 1},  // +Y
		{X: 0.2, Y: 0.2, Z: 0.2, W: 1},   // -Y
		{X: 0.15, Y: 0.65, Z: 0.9, W: 1}, // +Z (front)
		{X: 0.8, Y: 0.2, Z: 0.8, W: 1},   // -Z
	}
	leftMesh, err := render3d.UploadColoredMesh(nil, dev, unlitColoredCubeMesh(math3d.Vec3{X: -1.35, Y: 0.65, Z: 0.2}, 0.65, leftFaceColors), render3d.MeshUpload{Label: "left-cube"})
	if err != nil {
		t.Fatalf("UploadColoredMesh left: %v", err)
	}
	leftMatCfg := render3d.DefaultUnlitMaterial()
	leftMat, err := render3d.NewGPUUnlitMaterial(nil, dev, defs, leftMatCfg, render3d.MaterialUpload{Label: "left-mat"})
	if err != nil {
		t.Fatalf("NewGPUUnlitMaterial left: %v", err)
	}

	// 2. Right cube: sRGB checker texture multiplied by BaseColorFactor and vertex color tint.
	checker := checkerImage(4, color.NRGBA{R: 240, G: 240, B: 240, A: 255}, color.NRGBA{R: 80, G: 110, B: 220, A: 255})
	rightTex, err := render3d.UploadTexture(nil, dev, checker, render3d.TextureUpload{Label: "checker", SRGB: true})
	if err != nil {
		t.Fatalf("UploadTexture right: %v", err)
	}
	rightSamp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler right: %v", err)
	}
	tint := math3d.Vec4{X: 1.0, Y: 0.85, Z: 0.65, W: 1}
	rightMesh, err := render3d.UploadColoredMesh(nil, dev, unlitColoredCubeMesh(math3d.Vec3{X: 1.35, Y: 0.65, Z: -0.2}, 0.65, [6]math3d.Vec4{tint, tint, tint, tint, tint, tint}), render3d.MeshUpload{Label: "right-cube"})
	if err != nil {
		t.Fatalf("UploadColoredMesh right: %v", err)
	}
	rightMatCfg := render3d.DefaultUnlitMaterial()
	rightMatCfg.BaseColorFactor = [4]float32{0.95, 0.9, 0.85, 1}
	rightMatCfg.BaseColorTexture = rightTex
	rightMatCfg.BaseColorSampler = rightSamp
	rightMat, err := render3d.NewGPUUnlitMaterial(nil, dev, defs, rightMatCfg, render3d.MaterialUpload{Label: "right-mat"})
	if err != nil {
		t.Fatalf("NewGPUUnlitMaterial right: %v", err)
	}

	// 3. Center cutout card: alpha-masked double-sided quad with hollow center.
	maskImg := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			if (x == 1 || x == 2) && (y == 1 || y == 2) {
				maskImg.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 30}) // discarded
			} else {
				maskImg.SetNRGBA(x, y, color.NRGBA{R: 60, G: 235, B: 140, A: 255}) // kept
			}
		}
	}
	maskTex, _ := render3d.UploadTexture(nil, dev, maskImg, render3d.TextureUpload{Label: "mask", SRGB: true})
	maskMatCfg := render3d.DefaultUnlitMaterial()
	maskMatCfg.BaseColorTexture = maskTex
	maskMatCfg.BaseColorSampler = rightSamp
	maskMatCfg.AlphaMode = render3d.AlphaMask
	maskMatCfg.AlphaCutoff = 0.5
	maskMatCfg.DoubleSided = true
	maskMat, err := render3d.NewGPUUnlitMaterial(nil, dev, defs, maskMatCfg, render3d.MaterialUpload{Label: "mask-mat"})
	if err != nil {
		t.Fatalf("NewGPUUnlitMaterial mask: %v", err)
	}
	maskQuad := render3d.ColoredMesh{
		Positions: []math3d.Vec3{
			{X: -0.5, Y: 0.15, Z: 0.6},
			{X: 0.5, Y: 0.15, Z: 0.6},
			{X: 0.5, Y: 1.15, Z: 0.6},
			{X: -0.5, Y: 1.15, Z: 0.6},
		},
		UVs:     []math3d.Vec2{{X: 0, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 0}, {X: 0, Y: 0}},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
	maskMesh, _ := render3d.UploadColoredMesh(nil, dev, maskQuad, render3d.MeshUpload{Label: "mask-quad"})

	// 4. Translucent glass quad in front of left cube's lower edge.
	blendMatCfg := render3d.DefaultUnlitMaterial()
	blendMatCfg.BaseColorFactor = [4]float32{1.0, 0.35, 0.15, 0.45}
	blendMatCfg.AlphaMode = render3d.AlphaBlend
	blendMatCfg.DoubleSided = true
	blendMat, err := render3d.NewGPUUnlitMaterial(nil, dev, defs, blendMatCfg, render3d.MaterialUpload{Label: "blend-mat"})
	if err != nil {
		t.Fatalf("NewGPUUnlitMaterial blend: %v", err)
	}
	blendQuad := render3d.ColoredMesh{
		Positions: []math3d.Vec3{
			{X: -1.8, Y: 0.1, Z: 1.35},
			{X: -0.8, Y: 0.1, Z: 1.35},
			{X: -0.8, Y: 0.85, Z: 1.35},
			{X: -1.8, Y: 0.85, Z: 1.35},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
	blendMesh, _ := render3d.UploadColoredMesh(nil, dev, blendQuad, render3d.MeshUpload{Label: "blend-quad"})

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0.25, Y: 2.1, Z: 4.8}, math3d.DegToRad(46), float32(width)/float32(height), 0.1, 40)
	cam.LookAt(math3d.Vec3{Y: 0.6}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "unlit-scene-reference",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())

	pass.SetPipeline(pipeOpaque)
	leftMat.Bind(pass)
	leftMesh.Draw(pass)
	rightMat.Bind(pass)
	rightMesh.Draw(pass)

	pass.SetPipeline(pipeMaskDS)
	maskMat.Bind(pass)
	maskMesh.Draw(pass)

	pass.SetPipeline(pipeBlend)
	blendMat.Bind(pass)
	blendMesh.Draw(pass)

	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	assertReference(t, "unlit_vertex_colors_scene", img)

	// Analytical CPU parity checks:
	// Top face (+Y) of left cube is flat unlit (no lighting attenuation), matching EvaluateUnlit exactly.
	if s, _, ok := cam.WorldToScreen(math3d.Vec3{X: -1.35, Y: 1.3, Z: 0.2}, width, height); !ok {
		t.Fatal("left cube top face not visible")
	} else {
		wantTop := render3d.EvaluateUnlit(leftMatCfg, [4]float32{1, 1, 1, 1}, [4]float32{0.95, 0.8, 0.2, 1}).RGBA8()
		unlitWantPixel(t, img, int(s.X), int(s.Y), wantTop, 2)
	}
	// Center of cutout card is discarded -> background clear color shows through!
	if s, _, ok := cam.WorldToScreen(math3d.Vec3{X: 0, Y: 0.65, Z: 0.6}, width, height); !ok {
		t.Fatal("mask quad center not visible")
	} else {
		unlitWantPixel(t, img, int(s.X), int(s.Y), unlitClearColor, 1)
	}
}

func unlitViewApp(t *testing.T, retained bool, opts ...nsgotest.Option) (*nsgotest.App, **render3d.GPUColoredMesh, **render3d.GPUUnlitMaterial, **gpu.Device, *int) {
	t.Helper()
	const size = 64
	var (
		camBuf  *render3d.CameraBuffer
		defs    *render3d.MaterialDefaults
		tex     *render3d.GPUTexture
		samp    *render3d.GPUSampler
		gpuMat  *render3d.GPUUnlitMaterial
		gpuMesh *render3d.GPUColoredMesh
		pipe    *gpu.Pipeline
		curDev  *gpu.Device
		frames  int
	)
	texColor := color.NRGBA{R: 200, G: 150, B: 100, A: 255}
	vcol := math3d.Vec4{X: 0.75, Y: 0.5, Z: 1.0, W: 1}
	mesh := unlitClipQuad(-0.6, -0.6, 0.6, 0.6, []math3d.Vec4{vcol, vcol, vcol, vcol}, false)

	opts = append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				curDev = dev
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				camBuf.Write(unlitIdentityCameraUniform())

				if defs == nil || !retained {
					defs = unlitDefaults(t, cx, dev, retained)
				}
				if tex == nil || !retained {
					tex, err = render3d.UploadTexture(cx, dev, unlitSolidNRGBA(2, 2, texColor), render3d.TextureUpload{
						Label:    "unlit-tex",
						SRGB:     true,
						Retained: retained,
					})
					if err != nil {
						t.Fatalf("UploadTexture: %v", err)
					}
				}
				if samp == nil || !retained {
					sOpts := render3d.SamplerNearestClamp()
					sOpts.Retained = retained
					samp, err = render3d.NewGPUSampler(cx, dev, sOpts)
					if err != nil {
						t.Fatalf("NewGPUSampler: %v", err)
					}
				}
				if gpuMat == nil || !retained {
					m := render3d.DefaultUnlitMaterial()
					m.BaseColorFactor = [4]float32{0.8, 0.9, 0.7, 1.0}
					m.BaseColorTexture = tex
					m.BaseColorSampler = samp
					gpuMat, err = render3d.NewGPUUnlitMaterial(cx, dev, defs, m, render3d.MaterialUpload{
						Label:    "unlit-mat",
						Retained: retained,
					})
					if err != nil {
						t.Fatalf("NewGPUUnlitMaterial: %v", err)
					}
				}
				if gpuMesh == nil || !retained {
					gpuMesh, err = render3d.UploadColoredMesh(cx, dev, mesh, render3d.MeshUpload{
						Label:    "unlit-quad",
						Retained: retained,
					})
					if err != nil {
						t.Fatalf("UploadColoredMesh: %v", err)
					}
				}
				pipe, err = render3d.NewUnlitPipeline(cx, dev, camBuf.Layout(), defs.UnlitLayout(), render3d.UnlitPipelineOptions{
					AlphaMode:   render3d.AlphaOpaque,
					DepthFormat: gpu.FormatDepth24Plus,
					Retained:    retained,
				})
				if err != nil {
					t.Fatalf("NewUnlitPipeline: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				gpuMat.Bind(pass)
				gpuMesh.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, opts...)
	return app, &gpuMesh, &gpuMat, &curDev, &frames
}

func TestUnlitDrawsInGPUView(t *testing.T) {
	texLinear := unlitNRGBAToLinear(color.NRGBA{R: 200, G: 150, B: 100, A: 255})
	vcol := [4]float32{0.75, 0.5, 1.0, 1}

	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			app, meshp, matp, devp, frames := unlitViewApp(t, false, p.opts...)
			before := *frames
			app.Advance(16 * time.Millisecond)
			if *frames == before {
				t.Fatal("OnFrame did not run")
			}
			img, err := app.GPUReadPixels("id:viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			want := render3d.EvaluateUnlit((*matp).Material(), texLinear, vcol).RGBA8()
			unlitWantPixel(t, img, 32, 32, want, 2)
			unlitWantPixel(t, img, 2, 2, unlitClearColor, 1)

			// Live uniform update via GPUUnlitMaterial.Update.
			updated := (*matp).Material()
			updated.BaseColorFactor = [4]float32{0.25, 0.6, 0.95, 1.0}
			app.Post(func() {
				if err := (*matp).Update(*devp, updated); err != nil {
					t.Errorf("Update: %v", err)
				}
			})
			app.Settle()
			app.Advance(16 * time.Millisecond)
			img2, err := app.GPUReadPixels("id:viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels after update: %v", err)
			}
			want2 := render3d.EvaluateUnlit(updated, texLinear, vcol).RGBA8()
			unlitWantPixel(t, img2, 32, 32, want2, 2)
			if !(*meshp).Valid() || !(*matp).Valid() {
				t.Fatalf("resources not valid: mesh=%v mat=%v", (*meshp).Valid(), (*matp).Valid())
			}
		})
	}
}

func TestRetainedUnlitMaterialAndColoredMeshAfterDeviceLoss(t *testing.T) {
	texLinear := unlitNRGBAToLinear(color.NRGBA{R: 200, G: 150, B: 100, A: 255})
	vcol := [4]float32{0.75, 0.5, 1.0, 1}

	app, meshp, matp, devp, frames := unlitViewApp(t, true)
	app.Advance(16 * time.Millisecond)
	firstMesh, firstMat := *meshp, *matp
	img1, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	want1 := render3d.EvaluateUnlit(firstMat.Material(), texLinear, vcol).RGBA8()
	unlitWantPixel(t, img1, 32, 32, want1, 2)

	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if firstMesh.Valid() || firstMat.Valid() {
		t.Fatalf("resources still valid during device loss: mesh=%v mat=%v", firstMesh.Valid(), firstMat.Valid())
	}

	app.RestoreGPUDevice("id:viewport")
	if *meshp != firstMesh || !firstMesh.Valid() {
		t.Fatalf("retained colored mesh not restored: same=%v valid=%v", *meshp == firstMesh, firstMesh.Valid())
	}
	if *matp != firstMat || !firstMat.Valid() {
		t.Fatalf("retained unlit material not restored: same=%v valid=%v", *matp == firstMat, firstMat.Valid())
	}

	before := *frames
	app.Advance(16 * time.Millisecond)
	if *frames == before {
		t.Fatal("OnFrame did not run after restore")
	}
	img2, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	unlitWantPixel(t, img2, 32, 32, want1, 2)

	// Retained material remains updatable on the restored device without stale device pointers.
	updated := firstMat.Material()
	updated.BaseColorFactor = [4]float32{0.3, 0.9, 0.4, 1.0}
	app.Post(func() {
		if err := firstMat.Update(*devp, updated); err != nil {
			t.Errorf("Update after restore: %v", err)
		}
	})
	app.Settle()
	app.Advance(16 * time.Millisecond)
	img3, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after post-restore update: %v", err)
	}
	want3 := render3d.EvaluateUnlit(updated, texLinear, vcol).RGBA8()
	unlitWantPixel(t, img3, 32, 32, want3, 2)
}

func TestUnlitScopeDisposalValidationAndLifecycle(t *testing.T) {
	// 1. Scope disposal releases GPUColoredMesh and GPUUnlitMaterial.
	var (
		gpuMesh *render3d.GPUColoredMesh
		gpuMat  *render3d.GPUUnlitMaterial
		defs    *render3d.MaterialDefaults
		show    *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					var err error
					defs = unlitDefaults(t, cx, dev, false)
					gpuMat, err = render3d.NewGPUUnlitMaterial(cx, dev, defs, render3d.DefaultUnlitMaterial(), render3d.MaterialUpload{})
					if err != nil {
						t.Fatalf("NewGPUUnlitMaterial: %v", err)
					}
					gpuMesh, err = render3d.UploadColoredMesh(cx, dev, unlitClipQuad(-0.5, -0.5, 0.5, 0.5, nil, false), render3d.MeshUpload{})
					if err != nil {
						t.Fatalf("UploadColoredMesh: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())

	if !gpuMesh.Valid() || !gpuMat.Valid() {
		t.Fatal("resources must be valid while mounted")
	}
	if gpuMesh.ByteSize() != 4*render3d.ColoredVertexStride+12 {
		t.Errorf("GPUColoredMesh.ByteSize = %d, want %d", gpuMesh.ByteSize(), 4*render3d.ColoredVertexStride+12)
	}
	// The fallback texture and sampler are the shared MaterialDefaults, so only the 32 byte uniform buffer counts.
	if gpuMat.ByteSize() != render3d.UnlitUniformSize {
		t.Errorf("GPUUnlitMaterial.ByteSize = %d, want %d", gpuMat.ByteSize(), render3d.UnlitUniformSize)
	}
	if !defs.UnlitLayout().Valid() {
		t.Error("MaterialDefaults.UnlitLayout must be valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if gpuMesh.Valid() || gpuMat.Valid() || defs.UnlitLayout().Valid() {
		t.Errorf("resources still valid after scope disposal: mesh=%v mat=%v layout=%v", gpuMesh.Valid(), gpuMat.Valid(), defs.UnlitLayout().Valid())
	}

	// 2. Nil safety and double Close.
	var nilMesh *render3d.GPUColoredMesh
	if nilMesh.Valid() || nilMesh.Indexed() || nilMesh.VertexBuffer() != nil || nilMesh.IndexBuffer() != nil ||
		nilMesh.VertexCount() != 0 || nilMesh.IndexCount() != 0 || nilMesh.Triangles() != 0 ||
		nilMesh.HasVertexColors() || nilMesh.ByteSize() != 0 || nilMesh.Close() != nil {
		t.Error("nil GPUColoredMesh methods returned unexpected values")
	}
	var nilMat *render3d.GPUUnlitMaterial
	if nilMat.Valid() || nilMat.UniformBuffer() != nil || nilMat.BindGroup() != nil || nilMat.ByteSize() != 0 || nilMat.Close() != nil {
		t.Error("nil GPUUnlitMaterial methods returned unexpected values")
	}

	// 3. Validation and Update restrictions.
	nan := float32(math.NaN())
	badMats := []render3d.UnlitMaterial{
		{BaseColorFactor: [4]float32{-0.1, 1, 1, 1}},
		{BaseColorFactor: [4]float32{nan, 1, 1, 1}},
		{BaseColorFactor: [4]float32{1, 1.5, 1, 1}},
		{BaseColorFactor: [4]float32{1, 1, 1, 1}, AlphaMode: render3d.AlphaMode(9)},
		{BaseColorFactor: [4]float32{1, 1, 1, 1}, AlphaCutoff: -0.5},
		{BaseColorFactor: [4]float32{1, 1, 1, 1}, AlphaCutoff: nan},
	}
	for i, bm := range badMats {
		if err := bm.Validate(); !errors.Is(err, render3d.ErrInvalidUnlitMaterial) {
			t.Errorf("badMat[%d].Validate = %v, want ErrInvalidUnlitMaterial", i, err)
		}
	}

	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()
	defs = unlitDefaults(t, nil, dev, false)
	mat, err := render3d.NewGPUUnlitMaterial(nil, dev, defs, render3d.DefaultUnlitMaterial(), render3d.MaterialUpload{})
	if err != nil {
		t.Fatalf("NewGPUUnlitMaterial: %v", err)
	}
	otherTex, _ := render3d.UploadTexture(nil, dev, unlitSolidNRGBA(2, 2, color.NRGBA{R: 255, A: 255}), render3d.TextureUpload{SRGB: true})
	otherSamp, _ := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	swapTex := render3d.DefaultUnlitMaterial()
	swapTex.BaseColorTexture = otherTex
	if err := mat.Update(dev, swapTex); !errors.Is(err, render3d.ErrInvalidUnlitMaterial) {
		t.Errorf("Update swapping texture = %v, want ErrInvalidUnlitMaterial", err)
	}
	swapSamp := render3d.DefaultUnlitMaterial()
	swapSamp.BaseColorSampler = otherSamp
	if err := mat.Update(dev, swapSamp); !errors.Is(err, render3d.ErrInvalidUnlitMaterial) {
		t.Errorf("Update swapping sampler = %v, want ErrInvalidUnlitMaterial", err)
	}
	if err := mat.Update(nil, render3d.DefaultUnlitMaterial()); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("Update nil dev = %v, want ErrDeviceLost", err)
	}
	if err := mat.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := mat.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := mat.Update(dev, render3d.DefaultUnlitMaterial()); !errors.Is(err, render3d.ErrInvalidUnlitMaterial) {
		t.Errorf("Update closed mat = %v, want ErrInvalidUnlitMaterial", err)
	}

	// Nil device / invalid layout error checks.
	if _, err := render3d.UnlitMaterialBindGroupLayout(nil, nil); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("UnlitMaterialBindGroupLayout(nil) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewGPUUnlitMaterial(nil, nil, defs, render3d.DefaultUnlitMaterial(), render3d.MaterialUpload{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewGPUUnlitMaterial(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewGPUUnlitMaterial(nil, dev, nil, render3d.DefaultUnlitMaterial(), render3d.MaterialUpload{}); !errors.Is(err, render3d.ErrInvalidUnlitMaterial) {
		t.Errorf("NewGPUUnlitMaterial(nil defaults) = %v, want ErrInvalidUnlitMaterial", err)
	}
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	if _, err := render3d.NewUnlitPipeline(nil, nil, camBuf.Layout(), defs.UnlitLayout(), render3d.UnlitPipelineOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewUnlitPipeline(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewUnlitPipeline(nil, dev, nil, defs.UnlitLayout(), render3d.UnlitPipelineOptions{}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("NewUnlitPipeline(nil camera layout) = %v, want ErrInvalidDescriptor", err)
	}
	if _, err := render3d.NewUnlitPipeline(nil, dev, camBuf.Layout(), defs.UnlitLayout(), render3d.UnlitPipelineOptions{AlphaMode: render3d.AlphaMode(99)}); !errors.Is(err, render3d.ErrInvalidUnlitMaterial) {
		t.Errorf("NewUnlitPipeline(bad alpha) = %v, want ErrInvalidUnlitMaterial", err)
	}
	if _, err := render3d.UploadColoredMesh(nil, nil, unlitClipQuad(-0.5, -0.5, 0.5, 0.5, nil, false), render3d.MeshUpload{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("UploadColoredMesh(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.ColoredMeshFromGLTF(nil, 0, 0); !errors.Is(err, render3d.ErrInvalidMesh) {
		t.Errorf("ColoredMeshFromGLTF(nil) = %v, want ErrInvalidMesh", err)
	}
}

func TestAppendUnlitUniformsZeroAlloc(t *testing.T) {
	mat := render3d.DefaultUnlitMaterial()
	mat.BaseColorFactor = [4]float32{0.2, 0.4, 0.6, 0.8}
	mat.AlphaMode = render3d.AlphaMask
	mat.AlphaCutoff = 0.35
	dst := make([]byte, 0, render3d.UnlitUniformSize)
	if allocs := testing.AllocsPerRun(100, func() {
		dst = render3d.AppendUnlitUniforms(dst[:0], mat)
	}); allocs != 0 {
		t.Fatalf("AppendUnlitUniforms allocs = %v, want 0", allocs)
	}
	if len(dst) != render3d.UnlitUniformSize {
		t.Fatalf("len(dst) = %d, want %d", len(dst), render3d.UnlitUniformSize)
	}
}

func BenchmarkAppendUnlitUniforms(b *testing.B) {
	mat := render3d.DefaultUnlitMaterial()
	mat.BaseColorFactor = [4]float32{0.8, 0.5, 0.2, 1.0}
	mat.AlphaMode = render3d.AlphaMask
	mat.AlphaCutoff = 0.5
	dst := make([]byte, 0, render3d.UnlitUniformSize)
	b.SetBytes(render3d.UnlitUniformSize)
	b.ReportAllocs()
	for b.Loop() {
		dst = render3d.AppendUnlitUniforms(dst[:0], mat)
	}
}

func BenchmarkColoredMeshAppendVertexData(b *testing.B) {
	m := unlitColoredCubeMesh(math3d.Vec3Zero(), 1, [6]math3d.Vec4{
		{X: 1, W: 1}, {Y: 1, W: 1}, {Z: 1, W: 1},
		{X: 1, Y: 1, W: 1}, {Y: 1, Z: 1, W: 1}, {X: 1, Z: 1, W: 1},
	})
	dst := make([]byte, 0, m.VertexDataSize())
	b.SetBytes(int64(m.VertexDataSize()))
	b.ReportAllocs()
	for b.Loop() {
		dst = m.AppendVertexData(dst[:0])
	}
}
